package webapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/egress"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	proxytool "github.com/chainreactors/cyber/tools/proxy"
)

// 全工具出口（B2）：终端（bash/tmux）子进程、浏览器 playwright 与爬虫 katana（M5c，见 browser.go）经上游 MITM ProxyHub 出网。
// 本扩展只做三件事：按调用登记出口令牌、在代理连目标之前按任务策略放行/拒绝、
// 把放行后的完整 flow 与拒绝记录写入 webapi_egress_flows。代理本身、令牌生成、
// 进程环境注入和进程结束释放全部复用上游。只是代理强制：进程可以刻意绕开代理。

const (
	egressCADirName = "egress-ca"
	egressToolName  = "terminal" // 上游终端扩展（bash 与 tmux 共用）；浏览器/爬虫用 egressFor 显式登记 browser/katana。
	egressFlowRing  = 256        // 上游内存环只做中转；持久记录在本库。
)

var errEgressDenied = errors.New("全工具出口拒绝")

// egressRoute 是一次真实执行（一个终端进程）的归属。
type egressRoute struct {
	sessionID, turnID, callID, tool string
}

// egressEndpoint 包装上游 ProxyHub 的 egress.Endpoint：Egress 额外登记令牌→会话。
type egressEndpoint struct {
	hub   *proxytool.ProxyHub
	store *taskStore
	now   func() time.Time

	mu          sync.RWMutex
	byToken     map[string]egressRoute
	byOperation map[string]egressRoute
}

var _ egress.Endpoint = (*egressEndpoint)(nil)

func newEgressEndpoint(hub *proxytool.ProxyHub, store *taskStore) *egressEndpoint {
	return &egressEndpoint{hub: hub, store: store, now: time.Now, byToken: map[string]egressRoute{}, byOperation: map[string]egressRoute{}}
}

// ProxyURL 是不含令牌的基础地址：只作为终端基础环境的默认值，未经 Egress
// 登记直接使用它的请求没有令牌，会被拒绝。
func (e *egressEndpoint) ProxyURL() string { return e.hub.ProxyURL() }

func (e *egressEndpoint) CAPath() string { return e.hub.CAPath() }

// Egress 为一次执行取上游令牌，并按调用上下文登记归属；release 在进程真正结束时由上游调用。
func (e *egressEndpoint) Egress(ctx context.Context) (string, string, func()) {
	tool := egressToolName
	if info := operation.FromContext(ctx); info.Kind != "" && info.Kind != "process" {
		tool = info.Kind
	}
	return e.egressFor(ctx, tool)
}

// egressFor 与 Egress 相同，但由调用方给出工具名（浏览器 browser、爬虫 katana）。
func (e *egressEndpoint) egressFor(ctx context.Context, tool string) (string, string, func()) {
	proxyURL, caPath, release := e.hub.Egress(ctx)
	token := egressTokenFromURL(proxyURL)
	if token == "" {
		return proxyURL, caPath, release
	}
	info := operation.FromContext(ctx)
	route := egressRoute{sessionID: info.Invocation.SessionID, turnID: info.Invocation.TurnID, callID: info.Invocation.CallID, tool: tool}
	e.mu.Lock()
	e.byToken[token] = route
	if info.OperationID != "" {
		e.byOperation[info.OperationID] = route
	}
	e.mu.Unlock()
	var once sync.Once
	return proxyURL, caPath, func() {
		once.Do(func() {
			if release != nil {
				release() // 上游等待本令牌在途 flow 完成后才返回。
			}
			e.mu.Lock()
			delete(e.byToken, token)
			if info.OperationID != "" {
				delete(e.byOperation, info.OperationID)
			}
			e.mu.Unlock()
		})
	}
}

func egressTokenFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return ""
	}
	return u.User.Username()
}

// proxyAuthUser 与上游 mitmproxy 解析 Proxy-Authorization 的方式一致：只取 Basic 用户名。
func proxyAuthUser(header string) string {
	const prefix = "basic "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[len(prefix):]))
	if err != nil {
		return ""
	}
	user, _, _ := strings.Cut(string(decoded), ":")
	return user
}

func (e *egressEndpoint) routeForToken(token string) (egressRoute, bool) {
	if token == "" {
		return egressRoute{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	route, ok := e.byToken[token]
	return route, ok
}

// egressTarget 是代理收到的一个请求的目标描述。
type egressTarget struct {
	method, url, host string
	connect           bool
	display           string // 拒绝原因里展示的 origin / host:port
}

func egressTargetOf(req *http.Request) egressTarget {
	if strings.EqualFold(req.Method, http.MethodConnect) {
		authority := req.Host
		if authority == "" && req.URL != nil {
			authority = req.URL.Host
		}
		host, port, err := net.SplitHostPort(authority)
		if err != nil {
			host, port = authority, "443"
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		display := net.JoinHostPort(host, port)
		switch port {
		case "443":
			display = "https://" + defaultPortHost(host)
		case "80":
			display = "http://" + defaultPortHost(host)
		}
		return egressTarget{method: http.MethodConnect, url: authority, host: host, connect: true, display: display}
	}
	target := egressTarget{method: req.Method}
	if req.URL != nil {
		target.url = req.URL.String()
		target.host = strings.ToLower(req.URL.Hostname())
		if req.URL.IsAbs() {
			if origin, err := normalizeOrigin(req.URL.Scheme + "://" + req.URL.Host); err == nil {
				target.display = origin
			}
		}
	}
	if target.display == "" {
		target.display = target.url
	}
	return target
}

// egressTargetAllowed：HTTP 按 URL origin；CONNECT 只有 host:port，443→https、80→http，
// 其它端口与授权 origin 的 host:port 比较（不区分 scheme）。
func egressTargetAllowed(req *http.Request, target egressTarget, origins []string) bool {
	if !target.connect {
		return req.URL != nil && req.URL.IsAbs() && originAllowed(req.URL.String(), origins)
	}
	host, port, err := net.SplitHostPort(target.url)
	if err != nil {
		return false
	}
	switch port {
	case "443":
		return originAllowed("https://"+net.JoinHostPort(host, port), origins)
	case "80":
		return originAllowed("http://"+net.JoinHostPort(host, port), origins)
	}
	want := net.JoinHostPort(strings.ToLower(strings.Trim(host, "[]")), port)
	for _, allowed := range origins {
		u, err := url.Parse(allowed)
		if err != nil || u.Port() == "" {
			continue
		}
		if net.JoinHostPort(strings.ToLower(u.Hostname()), u.Port()) == want {
			return true
		}
	}
	return false
}

// authorize 是 ProxyHub.SetProxyAuth 回调：在转发 HTTP 或建立 CONNECT 隧道之前执行，
// 返回错误即 407、不连目标。策略每次读库（保存即生效），读失败按拒绝。
func (e *egressEndpoint) authorize(req *http.Request) error {
	ctx := context.WithoutCancel(req.Context())
	target := egressTargetOf(req)
	route, ok := e.routeForToken(proxyAuthUser(req.Header.Get("Proxy-Authorization")))
	if !ok {
		return e.deny(ctx, "", route, req, target, "无出口令牌或令牌已失效（未经终端出口登记的请求）")
	}
	task, err := e.store.egressTaskForSession(ctx, route.sessionID)
	if err != nil {
		return e.deny(ctx, "", route, req, target, "读取会话所属任务失败，按拒绝处理")
	}
	if task == nil {
		return nil // 普通对话：放行，flow 完成后记 task_id 空。
	}
	policy, err := e.store.policyForTask(ctx, task.ID)
	if err != nil {
		return e.deny(ctx, task.ID, route, req, target, "读取任务测试策略失败，按拒绝处理")
	}
	if policy.Scope == ScopeUnrestricted {
		return nil
	}
	if !egressTargetAllowed(req, target, task.Origins) {
		return e.deny(ctx, task.ID, route, req, target, "不在任务授权 origins 内："+target.display)
	}
	return nil
}

// denyUnrouted 记录一条未带令牌、由本地中继直接拒绝的请求（浏览器在两次调用之间的后台请求）。
func (e *egressEndpoint) denyUnrouted(sessionID, tool string, req *http.Request, reason string) {
	ctx := context.WithoutCancel(req.Context())
	taskID := ""
	if task, err := e.store.egressTaskForSession(ctx, sessionID); err == nil && task != nil {
		taskID = task.ID
	}
	_ = e.deny(ctx, taskID, egressRoute{sessionID: sessionID, tool: tool}, req, egressTargetOf(req), reason)
}

func (e *egressEndpoint) deny(ctx context.Context, taskID string, route egressRoute, req *http.Request, target egressTarget, reason string) error {
	headers := req.Header.Clone()
	headers.Del("Proxy-Authorization") // 令牌不入库。
	flow := &traffic.Flow{
		Error: "未发送：" + reason, Timestamp: nowTimestamp(),
		Request: &traffic.HttpRequest{Method: target.method, Url: target.url, Protocol: req.Proto, Headers: traffic.HeadersFromHTTPWithHost(headers, req.Host)},
	}
	now := e.now().UTC().Format(time.RFC3339Nano)
	model := &egressFlowModel{
		TaskID: taskID, SessionID: route.sessionID, TurnID: route.turnID, CallID: route.callID, Tool: route.tool,
		Method: target.method, URL: target.url, Host: target.host, Decision: egressDenied, Error: reason,
		CreatedAt: now, CompletedAt: sql.NullString{String: now, Valid: true}, // 拒绝即终态。
	}
	model.Sent.Valid = true // sent=false：在连目标之前拒绝。
	if err := e.store.insertEgressFlow(ctx, model, flow); err != nil {
		return fmt.Errorf("%w: %s（记录失败）", errEgressDenied, reason)
	}
	return fmt.Errorf("%w: %s", errEgressDenied, reason)
}

// 上游在未落盘模式下只保留 4KiB body 预览，并在 flow.Error 里写明观察到的原始字节数。
// 固定上游版本 f4b1fb2 的文案："<side> body truncated (<kept>/<observed> bytes retained)"。
var egressTruncationNotice = regexp.MustCompile(`(request|response) body truncated \((\d+)/(\d+) bytes retained\)( \(preview only; local storage disabled or unavailable\))?`)

// splitEgressFlowError 把截断说明从上游错误里分出来：返回真实转发错误与两侧观察字节数。
func splitEgressFlowError(flow *traffic.Flow) (rest string, requestBytes, responseBytes int64, truncated bool) {
	requestBytes = int64(len(flow.GetRequest().GetBody()))
	responseBytes = int64(len(flow.GetResponse().GetBody()))
	var parts []string
	for _, part := range strings.Split(flow.GetError(), "\n") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		match := egressTruncationNotice.FindStringSubmatch(part)
		if match == nil || match[0] != part {
			parts = append(parts, part)
			continue
		}
		observed, err := strconv.ParseInt(match[3], 10, 64)
		if err != nil {
			parts = append(parts, part)
			continue
		}
		truncated = true
		if match[1] == "request" {
			requestBytes = observed
		} else {
			responseBytes = observed
		}
	}
	return strings.Join(parts, "; "), requestBytes, responseBytes, truncated
}

// flowCompleted 写入已放行的 flow：归属取自本扩展登记的调用（按上游 operation id），
// 缺失时退回 flow 上下文里的 Invocation。
func (e *egressEndpoint) flowCompleted(ctx context.Context, event toolhooks.FlowEvent) (struct{}, error) {
	flow := event.Flow
	if flow == nil {
		return struct{}{}, nil
	}
	inv := operation.InvocationFromContext(ctx)
	route := egressRoute{sessionID: inv.SessionID, turnID: inv.TurnID, callID: inv.CallID, tool: egressToolName}
	if id := event.Operation.GetOperationId(); id != "" {
		e.mu.RLock()
		if registered, ok := e.byOperation[id]; ok {
			route = registered
		}
		e.mu.RUnlock()
	}
	ctx = context.WithoutCancel(ctx)
	taskID := ""
	if route.sessionID != "" {
		task, err := e.store.egressTaskForSession(ctx, route.sessionID)
		if err != nil {
			return struct{}{}, err
		}
		if task != nil {
			taskID = task.ID
		}
	}
	rest, requestBytes, responseBytes, truncated := splitEgressFlowError(flow)
	createdAt := e.now().UTC()
	if ts := flow.GetTimestamp(); ts != nil && ts.IsValid() {
		createdAt = ts.AsTime().UTC()
	}
	request := flow.GetRequest()
	model := &egressFlowModel{
		TaskID: taskID, SessionID: route.sessionID, TurnID: route.turnID, CallID: route.callID, Tool: route.tool,
		Method: request.GetMethod(), URL: request.GetUrl(), Decision: egressAllowed, Error: rest,
		RequestBytes: requestBytes, ResponseBytes: responseBytes, PreviewTruncated: truncated,
		CreatedAt: createdAt.Format(time.RFC3339Nano),
	}
	model.CompletedAt.String, model.CompletedAt.Valid = e.now().UTC().Format(time.RFC3339Nano), true
	if u, err := url.Parse(request.GetUrl()); err == nil {
		model.Host = strings.ToLower(u.Hostname())
	}
	if status := flow.GetResponse().GetStatusCode(); status != 0 {
		model.Sent.Bool, model.Sent.Valid = true, true
		model.HTTPStatus.Int64, model.HTTPStatus.Valid = int64(status), true
	} // 无响应：发送结果未知（sent=NULL），不推断。
	return struct{}{}, e.store.insertEgressFlow(ctx, model, flow)
}

// egressExtension 随节点 Load 启动 ProxyHub、Close 停止；发布包装后的 egress.Endpoint。
type egressExtension struct {
	store    *taskStore
	caDir    string
	resource *proxytool.Resource
	sub      *corehooks.Subscription
	endpoint *egressEndpoint
}

// EgressExtension 返回节点用的全工具出口扩展，替代 harness.NoEgress()。
// CA 放 state-dir 下 egress-ca/（0700），不在节点工作区内。
func (c *Capability) EgressExtension() extension.Extension {
	return &egressExtension{store: c.store, caDir: c.egressCADir}
}

func (x *egressExtension) Load(scope *extension.Scope) error {
	registry, err := extension.Use[*corehooks.Registry](scope)
	if err != nil {
		return err
	}
	if x.caDir == "" {
		return errors.New("全工具出口缺少 CA 目录")
	}
	if err := os.MkdirAll(x.caDir, 0o700); err != nil {
		return fmt.Errorf("创建出口 CA 目录: %w", err)
	}
	if err := os.Chmod(x.caDir, 0o700); err != nil {
		return fmt.Errorf("保护出口 CA 目录: %w", err)
	}
	x.resource = proxytool.NewProxyHub(proxytool.NewState(""), proxytool.NewFlowStore(egressFlowRing), x.caDir, true, registry)
	hub := x.resource.ProxyHub
	x.endpoint = newEgressEndpoint(hub, x.store)
	hub.SetProxyAuth(x.endpoint.authorize)
	x.sub = toolhooks.FlowCompletedObserved.On(registry, "wscyber-egress", x.endpoint.flowCompleted)
	if err := x.resource.Start(scope.Init()); err != nil {
		return err
	}
	// 上游私钥文件用默认权限创建；目录已 0700，这里再收紧文件本身。
	_ = os.Chmod(filepath.Join(x.caDir, "mitmproxy-ca.pem"), 0o600)
	return extension.Provide[egress.Endpoint](scope, x.endpoint)
}

func (x *egressExtension) Close(ctx context.Context) error {
	var err error
	if x.resource != nil {
		err = x.resource.Close(ctx)
	}
	if x.sub != nil {
		err = errors.Join(err, x.sub.Close(ctx))
	}
	return err
}

var _ extension.Extension = (*egressExtension)(nil)
