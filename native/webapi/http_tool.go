package webapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxCaptureBytes = 64 * 1024
	maxPreviewBytes = 4 * 1024
	maxRequestBody  = 1 << 20
	httpTimeout     = 30 * time.Second
)

type httpToolArguments struct {
	Method  string            `json:"method" jsonschema:"required,description=HTTP method"`
	URL     string            `json:"url" jsonschema:"required,description=Absolute HTTP(S) URL within the saved task authorization"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty" jsonschema:"description=Literal request body bytes as UTF-8 text"`
}

type httpTool struct {
	store     *taskStore
	secrets   *secretBox
	approvals *approvalRuntime // 为空时视为无人批准：需审批的请求一律记 review_required，0发送。
}

// ToolExtension 在原生Harness工具集中注册，复用Capability同一store。
func (c *Capability) ToolExtension() extension.Extension {
	var subscriptions []*corehooks.Subscription
	return extension.Func{
		LoadFunc: func(scope *extension.Scope) error {
			if err := extension.Add[coretool.Tool](scope, &httpTool{store: c.store, secrets: c.secrets, approvals: c.approvals}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &authzCompareTool{store: c.store, secrets: c.secrets}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &findingTool{store: c.store}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &verificationTool{store: c.store}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &reportDraftTool{store: c.store}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &retestTool{store: c.store}); err != nil {
				return err
			}
			for _, tool := range []coretool.Tool{&boardReadTool{store: c.store}, &boardMapTool{store: c.store}, &boardFactTool{store: c.store}, &boardOracleTool{store: c.store}} {
				if err := extension.Add[coretool.Tool](scope, tool); err != nil {
					return err
				}
			}
			registry, err := extension.Use[*corehooks.Registry](scope)
			if err != nil {
				return err
			}
			subscriptions = append(subscriptions, agenthooks.ModelRequestPolicy.On(registry, "wscyber-webapi-unresolved", func(ctx context.Context, event agenthooks.ModelRequestEvent) (agenthooks.ModelPolicy, error) {
				blocked, err := c.store.unresolvedTurn(ctx, event.SessionID, event.TurnID)
				if err != nil {
					return agenthooks.ModelPolicy{Deny: errors.New("无法核对当前turn未决HTTP，停止下一次模型调用")}, nil
				}
				if blocked {
					return agenthooks.ModelPolicy{Deny: errors.New("当前turn已有claimed/unknown/capture_failed/review_required HTTP；先核对，不继续模型调用")}, nil
				}
				return agenthooks.ModelPolicy{}, nil
			}))
			// 研究会话按任务场景注入身份/边界/黑板纪律/证据标准/方法/完成标准（M1，见 native/SCENARIO.md）。
			subscriptions = append(subscriptions, agenthooks.BeforeRun.On(registry, "wscyber-webapi-scenario", c.scenarioBeforeRun))
			// 持续探索：研究会话的 Goal 评估追加平台黑板段（M3，见 explore.go）。
			explore, err := c.registerExplore(scope, registry)
			if err != nil {
				return err
			}
			subscriptions = append(subscriptions, explore)
			return nil
		},
		CloseFunc: func(ctx context.Context) error {
			var result error
			for _, subscription := range subscriptions {
				result = errors.Join(result, subscription.Close(ctx))
			}
			return result
		},
	}
}

func (*httpTool) Name() string { return "webapi_http" }
func (*httpTool) Description() string {
	return "Send an actual HTTP(S) request within the current saved task's authorized origins and retain bounded request/response evidence. URL, header values and body support {{cred:<credential_id>.username}} and {{cred:<credential_id>.secret}} for credentials of the task asset. Never use it for unauthorized hosts."
}
func (*httpTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_http", "在已保存任务的明确origin授权内发送一次具体HTTP请求；url、headers值、body可用本任务资产账号占位符 {{cred:<id>.username}} 与 {{cred:<id>.secret}}，平台发送前替换，工具结果回替 secret；禁止自报task/session/授权，结果提供有界预览与证据ID。", httpToolArguments{})
}

func buildHTTPRequest(method, rawURL string, headers map[string]string, body []byte) (*http.Request, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" || strings.ContainsAny(method, " \t\r\n") {
		return nil, errors.New("method 非法")
	}
	if len(body) > maxRequestBody {
		return nil, errors.New("body 超过1MiB")
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
		return nil, errors.New("url 必须是无userinfo/fragment的绝对HTTP(S) URL")
	}
	origin, err := normalizeOrigin(u.Scheme + "://" + u.Host)
	if err != nil {
		return nil, fmt.Errorf("url origin非法: %w", err)
	}
	u.Host = strings.TrimPrefix(origin, u.Scheme+"://")
	// 大小写scheme经url.Parse会丢RawPath，先单独恢复保留原转义的路径。
	original := strings.TrimSpace(rawURL)
	pathStart := strings.Index(original, "://") + 3
	if pathStart < 3 {
		return nil, errors.New("url缺少scheme")
	}
	if index := strings.IndexAny(original[pathStart:], "/?#"); index >= 0 {
		pathStart += index
	} else {
		pathStart = len(original)
	}
	pathEnd := len(original)
	if index := strings.IndexAny(original[pathStart:], "?#"); index >= 0 {
		pathEnd = pathStart + index
	}
	rawPath := original[pathStart:pathEnd]
	if rawPath == "" {
		rawPath = "/"
	}
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return nil, errors.New("url path转义非法")
	}
	u.Path = decodedPath
	// RawPath 必须是合法的全 ASCII 转义形式；保留已有 %XX，将其余需转义字节（含非 ASCII）按 UTF-8 编码。
	u.RawPath = escapeRawPath(rawPath)
	req, err := http.NewRequest(method, u.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Host = u.Host // 与URL authority完全相同，仅让Flow保留实际Host。
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "wscyber-webapi/1")
	}
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
	// URL authority 是唯一Host来源；请求头不能改Host或传输跳转策略。
	for key, value := range headers {
		if err := checkRequestHeader(key, value); err != nil {
			return nil, err
		}
		req.Header.Set(http.CanonicalHeaderKey(key), value)
	}
	return req, nil
}

// checkRequestHeader：URL authority 是唯一 Host 来源，请求头不能改 Host、framing 或代理鉴权；名/值须合法。
func checkRequestHeader(key, value string) error {
	if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") || strings.EqualFold(key, "Connection") || strings.EqualFold(key, "Proxy-Authorization") {
		return fmt.Errorf("header %s 不允许覆盖传输目标/ framing", key)
	}
	if !httpgutsValidHeader(key, value) {
		return fmt.Errorf("header %s 非法", key)
	}
	return nil
}

func escapeRawPath(rawPath string) string {
	const upperhex = "0123456789ABCDEF"
	var escaped strings.Builder
	escaped.Grow(len(rawPath))
	for i := 0; i < len(rawPath); i++ {
		c := rawPath[i]
		alphanumeric := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if alphanumeric || strings.IndexByte("-_.~/!$&'()*+,;=:@[]%", c) >= 0 {
			escaped.WriteByte(c)
			continue
		}
		escaped.WriteByte('%')
		escaped.WriteByte(upperhex[c>>4])
		escaped.WriteByte(upperhex[c&15])
	}
	return escaped.String()
}

func httpgutsValidHeader(name, value string) bool {
	return name != "" && !strings.ContainsAny(name, "()<>@,;:\\\"/[]?={} \t\r\n") && !strings.ContainsAny(value, "\r\n") && !strings.ContainsFunc(name, func(r rune) bool { return r < 0x21 || r > 0x7e })
}

func fingerprintRequest(req *http.Request, body []byte) string {
	encoded, err := json.Marshal(struct {
		Method  string              `json:"method"`
		URL     string              `json:"url"`
		Headers map[string][]string `json:"headers"`
		Body    []byte              `json:"body"`
	}{Method: req.Method, URL: req.URL.String(), Headers: canonicalHeaders(req.Header), Body: body})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func canonicalHeaders(header http.Header) map[string][]string {
	result := make(map[string][]string, len(header))
	for name, values := range header {
		result[strings.ToLower(name)] = append([]string(nil), values...)
	}
	return result
}

func originAllowed(rawURL string, origins []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	origin, err := normalizeOrigin(u.Scheme + "://" + u.Host)
	if err != nil {
		return false
	}
	for _, allowed := range origins {
		if origin == allowed {
			return true
		}
	}
	return false
}

var directTransport = &http.Transport{
	Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false, DisableCompression: true,
	DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
}

var directClient = &http.Client{
	Transport:     directTransport,
	Timeout:       httpTimeout,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

func (t *httpTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.CallID == "" || inv.TurnID == "" {
		return stopTool("缺少可信原生 session/turn/call 上下文；0发送"), nil
	}
	args, err := coretool.ParseArgs[httpToolArguments](arguments)
	if err != nil {
		return stopTool("参数不是合法请求JSON；0发送"), nil
	}
	// task/session/origins/approved即使伪造在JSON里也不进入ParseArgs结构或授权。
	task, err := t.store.taskForSession(ctx, inv.SessionID)
	if err != nil || task == nil {
		return stopTool("session无已保存任务授权；0发送"), nil
	}
	resolver, err := loadCredentialResolver(ctx, t.store, t.secrets, task.ID)
	if err != nil {
		return stopTool("读取测试账号或密钥失败，无法解析占位符或回替工具结果；0发送"), nil
	}
	finish := func(result *aop.ToolResult) (*aop.ToolResult, error) {
		return redactToolResult(result, resolver), nil
	}
	args, err = resolver.resolveHTTPArguments(args)
	if err != nil {
		return finish(stopTool(err.Error() + "；0发送"))
	}
	// 测试策略每次调用现读，保存即生效；只由人经鉴权 API 设置。
	policy, err := t.store.policyForTask(ctx, task.ID)
	if err != nil {
		return finish(stopTool("无法读取任务测试策略；0发送"))
	}
	body := []byte(args.Body)
	req, err := buildHTTPRequest(args.Method, args.URL, args.Headers, body)
	if err != nil {
		return finish(stopTool(err.Error() + "；0发送"))
	}
	switch decision, reason := httpPolicyDecision(policy, task.Origins, req, args.Headers, body); decision {
	case "denied":
		result, _ := t.recordUnsent(ctx, inv, task.ID, req, body, decision, reason)
		return finish(result)
	case "review_required":
		// 审批由原生 tool.before 挂起处理；这里只拒绝未经人工批准却需审批的调用（如无审批扩展的装配）。
		if !t.approvals.isApproved(inv) {
			result, _ := t.recordUnsent(ctx, inv, task.ID, req, body, decision, reason+"；未经人工审批")
			return finish(result)
		}
	}
	return finish(httpSendResult(requestRecorder{store: t.store, inv: inv, taskID: task.ID}.send(ctx, req, body)))
}

// httpPolicyDecision 是 webapi_http 与一键重放共用的发送前判定（策略由调用方每次现读）：
// scope=origins 且 origin 不在任务授权内 → "denied"；httpReviewReason 非空 → "review_required"；否则放行（""）。
func httpPolicyDecision(policy TaskPolicy, origins []string, req *http.Request, headers map[string]string, body []byte) (string, string) {
	if policy.Scope != ScopeUnrestricted && !originAllowed(req.URL.String(), origins) {
		return "denied", "实际origin不在持久授权内（测试策略 scope=origins）"
	}
	if reason := httpReviewReason(policy.Approval, req.Method, req.URL.String(), headers, body); reason != "" {
		return "review_required", reason
	}
	return "", ""
}

// httpSendResult 把共用发送路径的结果映射成 webapi_http 工具结果（文案与旧实现一致）。
func httpSendResult(res sendResult) *aop.ToolResult {
	switch res.state {
	case sendClaimFailed:
		if res.model != nil {
			return stopTool(formatEvidence(res.model, res.err.Error()))
		}
		return stopTool("发送前认领失败，0发送: " + publicStoreError(res.err))
	case sendExisting:
		if res.model.Status == requestCompleted {
			return coretool.TextResult(formatEvidence(res.model, "同调用已完成，返回已保存结果；未再次发送"))
		}
		return stopTool(formatEvidence(res.model, "同调用已有未解决结果，不再次发送"))
	case sendLost:
		return stopTool("证据ID " + res.model.ID + "：已发送但capture_failed保存也失败；保持未知，不自动重试")
	case requestCompleted:
		return coretool.TextResult(formatEvidence(res.model, "HTTP请求完成；仅代表目标响应，不是漏洞结论"))
	default: // unknown / capture_failed
		return stopTool(formatEvidence(res.model, res.note))
	}
}

// ---- 共用发送与存证（webapi_http 与一键重放 replay.go） ----

const (
	sendClaimFailed = "claim_failed" // 认领失败，0发送（含同指纹未决 errUnknownReplay）
	sendExisting    = "existing"     // 同 task/turn/call 已有记录，未再次发送
	sendLost        = "lost"         // 已发送但 capture_failed 也保存失败，保持认领行
)

// requestRecorder 是 claim→send→finish 状态机的归属：模型调用经 claim 核对 session→任务；
// trusted=true（一键重放的合成 session）由调用方先核对任务，认领走 claimRequest。
type requestRecorder struct {
	store   *taskStore
	inv     operation.Invocation
	taskID  string
	trusted bool
}

// sendResult：state 为 sendClaimFailed/sendExisting/sendLost 或最终记录状态（completed/unknown/capture_failed）。
type sendResult struct {
	state string
	model *requestModel
	note  string
	err   error
}

func (r requestRecorder) claim(ctx context.Context, spec requestSpec, status, reason string) (*requestModel, bool, error) {
	if r.trusted {
		return r.store.claimRequest(ctx, r.inv, r.taskID, spec, status, reason)
	}
	return r.store.claim(ctx, r.inv, r.taskID, spec, status, reason)
}

func requestSpecFor(id string, req *http.Request, body []byte) (requestSpec, *traffic.Flow) {
	flow := traffic.FlowFromHTTP(req, nil, body, nil)
	flow.Id, flow.Timestamp = id, nowTimestamp()
	return requestSpec{method: req.Method, url: req.URL.String(), fingerprint: fingerprintRequest(req, body), body: body, flow: flow}, flow
}

// recordUnsent 留一条发送前即终止的记录（denied/review_*，sent=false）。
func (r requestRecorder) recordUnsent(ctx context.Context, req *http.Request, body []byte, status, reason string) (*requestModel, bool, error) {
	spec, _ := requestSpecFor(newRequestID(), req, body)
	return r.claim(ctx, spec, status, reason)
}

// send：认领（claimed）→ directClient 发送一次 → 有界捕获 → finish。发送/读取失败记 unknown，
// 保存失败记 capture_failed；任何情况都不自动重试。
func (r requestRecorder) send(ctx context.Context, req *http.Request, body []byte) sendResult {
	id := newRequestID()
	spec, flow := requestSpecFor(id, req, body)
	claimed, fresh, err := r.claim(ctx, spec, "claimed", "发送前已认领")
	if err != nil {
		return sendResult{state: sendClaimFailed, model: claimed, err: err}
	}
	if !fresh {
		return sendResult{state: sendExisting, model: claimed}
	}

	resp, sendErr := directClient.Do(req.WithContext(ctx))
	if sendErr != nil {
		return r.finishUnknown(ctx, claimed, flow, "发送或接收失败，目标结果未知；不自动重试", nil)
	}
	defer resp.Body.Close()
	captured, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCaptureBytes+1))
	captureTruncated := len(captured) > maxCaptureBytes
	if captureTruncated {
		captured = captured[:maxCaptureBytes]
	}
	flow = traffic.FlowFromHTTP(req, resp, body, captured)
	flow.Id, flow.Timestamp = id, nowTimestamp()
	flow.Complete = !captureTruncated && readErr == nil
	statusCode := resp.StatusCode
	if readErr != nil {
		flow.Complete = false
		return r.finishUnknown(ctx, claimed, flow, "响应体读取失败，目标结果未知；不自动重试", &statusCode)
	}
	preview, previewTruncated := responsePreview(captured, resp.Header.Get("Content-Type"))
	model, err := r.store.finish(ctx, claimed.ID, requestCompleted, "已收到并保存有界响应", preview, previewTruncated, captureTruncated, &statusCode, flow)
	if err != nil {
		return r.saveFailure(ctx, claimed, flow, &statusCode, preview, previewTruncated, captureTruncated)
	}
	return sendResult{state: requestCompleted, model: model}
}

func (r requestRecorder) finishUnknown(ctx context.Context, claimed *requestModel, flow *traffic.Flow, reason string, statusCode *int) sendResult {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	model, err := r.store.finish(cleanup, claimed.ID, requestUnknown, reason, []byte("目标响应未知"), false, false, statusCode, flow)
	if err != nil {
		return r.saveFailure(ctx, claimed, flow, statusCode, nil, false, false)
	}
	return sendResult{state: requestUnknown, model: model, note: reason}
}

func (r requestRecorder) saveFailure(ctx context.Context, claimed *requestModel, flow *traffic.Flow, statusCode *int, preview []byte, previewTruncated, captureTruncated bool) sendResult {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	const note = "已发送但结果保存失败；不自动重试"
	model, err := r.store.finish(cleanup, claimed.ID, requestCaptureFailed, note, preview, previewTruncated, captureTruncated, statusCode, flow)
	if err != nil {
		return sendResult{state: sendLost, model: claimed, note: "已发送但capture_failed保存也失败；保持未知，不自动重试"}
	}
	return sendResult{state: requestCaptureFailed, model: model, note: note}
}

func responsePreview(captured []byte, contentType string) ([]byte, bool) {
	textual := utf8.Valid(captured) && (strings.HasPrefix(strings.ToLower(contentType), "text/") || strings.Contains(strings.ToLower(contentType), "json") || contentType == "")
	if !textual {
		const prefix = "base64:"
		// 保留一个原字节余量，使base64长度恰为4的倍数且含前缀不超4KiB。
		limit := (maxPreviewBytes-len(prefix))/4*3 - 1
		truncated := len(captured) > limit
		if truncated {
			captured = captured[:limit]
		}
		return []byte(prefix + base64.StdEncoding.EncodeToString(captured)), truncated
	}
	truncated := len(captured) > maxPreviewBytes
	if truncated {
		captured = captured[:maxPreviewBytes]
		for len(captured) > 0 && !utf8.Valid(captured) {
			captured = captured[:len(captured)-1]
		}
	}
	return captured, truncated
}

// 发送取消/进程中断留下的 claimed 也永不自动重发，无需借新CallID规避。

func nowTimestamp() *timestamppb.Timestamp { return timestamppb.Now() }

func stopTool(message string) *aop.ToolResult {
	result := coretool.ErrorResult(message)
	result.Terminate = true // StandardLoop实测支持：停止当前自动回合，不造暂停调度器。
	return result
}

func publicStoreError(err error) string {
	switch {
	case errors.Is(err, errUnknownSession), errors.Is(err, errCallConflict), errors.Is(err, errUnknownReplay):
		return err.Error()
	default:
		return "证据存储操作失败"
	}
}

func (t *httpTool) recordUnsent(ctx context.Context, inv operation.Invocation, taskID string, req *http.Request, body []byte, status, reason string) (*aop.ToolResult, error) {
	model, fresh, err := requestRecorder{store: t.store, inv: inv, taskID: taskID}.recordUnsent(ctx, req, body, status, reason)
	if err != nil && model == nil {
		return stopTool("保存拒绝/待审记录失败；0发送"), nil
	}
	if !fresh && model != nil {
		reason = "已有同调用记录；0发送"
	}
	return stopTool(formatEvidence(model, reason+"；0发送")), nil
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func formatEvidence(model *requestModel, note string) string {
	if model == nil {
		return note
	}
	value := map[string]any{
		"evidence_id": model.ID, "task_id": model.TaskID, "session_id": model.SessionID,
		"turn_id": model.TurnID, "call_id": model.CallID, "status": model.Status,
		"sent": nullableBool(model.Sent), "preview": string(model.Preview), "preview_truncated": model.PreviewTruncated,
		"capture_truncated": model.CaptureTruncated, "note": note,
	}
	if model.StatusCode.Valid {
		value["http_status"] = model.StatusCode.Int64
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func nullableBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}

// Evidence 是列表/工具摘要共用的元数据；不含Flow protobuf原件。
type Evidence struct {
	ID               string `json:"evidence_id"`
	TaskID           string `json:"task_id"`
	SessionID        string `json:"session_id"`
	TurnID           string `json:"turn_id"`
	CallID           string `json:"call_id"`
	Fingerprint      string `json:"request_fingerprint"`
	Method           string `json:"method"`
	URL              string `json:"url"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	Sent             *bool  `json:"sent"`
	HTTPStatus       *int64 `json:"http_status,omitempty"`
	PreviewTruncated bool   `json:"preview_truncated"`
	CaptureTruncated bool   `json:"capture_truncated"`
	CreatedAt        string `json:"created_at"`
	CompletedAt      string `json:"completed_at,omitempty"`
	// Source：一键重放为 replay（合成 session），越权对比为 authz_compare（call_id 前缀）；普通模型调用省略。
	Source string `json:"source,omitempty"`
}

func evidenceMetadata(model requestModel) Evidence {
	e := Evidence{ID: model.ID, TaskID: model.TaskID, SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID,
		Fingerprint: model.Fingerprint, Method: model.Method, URL: model.URL, Status: model.Status, Reason: model.Reason,
		Sent: nullableBool(model.Sent), PreviewTruncated: model.PreviewTruncated, CaptureTruncated: model.CaptureTruncated, CreatedAt: model.CreatedAt, CompletedAt: model.CompletedAt.String}
	if model.StatusCode.Valid {
		e.HTTPStatus = &model.StatusCode.Int64
	}
	if model.SessionID == replaySessionID {
		e.Source = evidenceSourceReplay
	}
	if strings.HasPrefix(model.CallID, authzCompareCallPrefix) {
		e.Source = evidenceSourceAuthz
	}
	return e
}

func (c *Capability) handleEvidenceList(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			writeJSON(w, 400, map[string]string{"error": "limit须为1至200"})
			return
		}
	}
	afterCreatedAt, afterID := "", ""
	if encoded := r.URL.Query().Get("cursor"); encoded != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		parts := strings.Split(string(decoded), "\n")
		if decodeErr != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			writeJSON(w, 400, map[string]string{"error": "cursor非法"})
			return
		}
		afterCreatedAt, afterID = parts[0], parts[1]
	}
	models, err := c.store.evidenceList(r.Context(), taskID, limit, afterCreatedAt, afterID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取证据元数据失败"})
		return
	}
	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit]
	}
	items := make([]Evidence, 0, len(models))
	for _, model := range models {
		items = append(items, evidenceMetadata(model))
	}
	response := map[string]any{"task_id": taskID, "items": items, "limit": limit, "has_more": hasMore, "next_cursor": nil}
	if hasMore {
		last := models[len(models)-1]
		response["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt + "\n" + last.ID))
	}
	if r.URL.Query().Get("mask") == "1" {
		resolver, err := loadCredentialResolver(r.Context(), c.store, c.secrets, taskID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "读取资产口令失败，无法遮盖证据"})
			return
		}
		count := 0
		for i := range items {
			count += redactEvidenceMetadata(&items[i], resolver)
		}
		response["masked_secrets"] = count
	}
	writeJSON(w, 200, response)
}

func redactEvidenceMetadata(meta *Evidence, resolver *credentialResolver) int {
	var urlCount, reasonCount int
	meta.URL, urlCount = resolver.redactCount(meta.URL)
	meta.Reason, reasonCount = resolver.redactCount(meta.Reason)
	return urlCount + reasonCount
}

// 二进制预览沿用 base64: 编码；先解码遮盖，再编码，避免编码绕过。
func redactEvidencePreview(preview string, resolver *credentialResolver) (string, int) {
	if strings.HasPrefix(preview, "base64:") {
		if data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(preview, "base64:")); err == nil {
			text, count := resolver.redactCount(string(data))
			return "base64:" + base64Encode([]byte(text)), count
		}
	}
	return resolver.redactCount(preview)
}

func (c *Capability) handleEvidenceGet(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	model, err := c.store.evidenceByID(r.Context(), taskID, r.PathValue("evidence_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取证据失败"})
		return
	}
	if model == nil {
		writeJSON(w, 404, map[string]string{"error": "任务内证据不存在"})
		return
	}
	meta, preview, flowPB := evidenceMetadata(*model), string(model.Preview), model.FlowPB
	mask, count := r.URL.Query().Get("mask") == "1", 0
	if mask {
		resolver, loadErr := loadCredentialResolver(r.Context(), c.store, c.secrets, taskID)
		if loadErr != nil {
			writeJSON(w, 500, map[string]string{"error": "读取资产口令失败，无法遮盖证据"})
			return
		}
		flowPB, count, err = resolver.redactEvidenceFlow(flowPB)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		count += redactEvidenceMetadata(&meta, resolver)
		var previewCount int
		preview, previewCount = redactEvidencePreview(preview, resolver)
		count += previewCount
	}
	flowJSON, err := flowDetailJSON(flowPB)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	response := map[string]any{
		"metadata": meta, "preview": preview,
		"flow_protobuf_base64": base64Encode(model.FlowPB),
		"flow":                 flowJSON,
		"capture":              map[string]any{"request": "结构化重建，含完整请求body；非逐字节线上报文", "response_body_limit": maxCaptureBytes, "preview_limit": maxPreviewBytes, "capture_truncated": model.CaptureTruncated, "preview_truncated": model.PreviewTruncated, "auto_decompressed": false},
	}
	if mask {
		delete(response, "flow_protobuf_base64")
		response["masked_secrets"] = count
	}
	writeJSON(w, 200, response)
}

func base64Encode(value []byte) string { return base64.StdEncoding.EncodeToString(value) }
