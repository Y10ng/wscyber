package webapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/core/egress"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	"github.com/chainreactors/cyber/core/telemetry"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/tools/headless"
	"github.com/chainreactors/cyber/tools/katana"
	"github.com/chainreactors/cyber/tools/playwright"
	"github.com/go-rod/rod/lib/launcher"
)

// 浏览器与爬虫（M5c）：注册上游 playwright（go-rod）与 katana 命令，但由本项目包装执行，
// 让它们的全部流量经 B2 出口（ProxyHub）并带本次调用的会话归属。契约见 native/BROWSER.md。
//
//   - 不用上游 exts/browser、exts/katana：前者的 Chrome 不设代理（直连），后者把不带令牌的基础
//     代理地址作启动默认值、按调用的令牌只来自终端进程环境（会被记成 terminal）。两者都无法
//     区分工具或给 Chrome 带令牌；包装层只改启动方式与参数，不改上游代码。
//   - Chrome 不接受代理 URL 中的用户名口令：每个浏览器配一个 127.0.0.1 中继（egressRelay），
//     由中继补本次调用的令牌。Chrome 由本包启动（无头、固定参数），上游 playwright 经 CDP 连接；
//     katana 无头/混合模式用 -chrome-ws-url 连同一类实例。
//   - 拒绝模型改变出口或接管浏览器的参数；不自动下载浏览器。

const (
	egressToolBrowser = "browser"
	egressToolKatana  = "katana"
)

// unreachableURL 用于把浏览器内置的 Google 服务地址指向不可达端口（端口 1 无服务）。
const unreachableURL = "http://127.0.0.1:1"

var errBrowserMissing = errors.New("未找到 Chromium 可执行文件：请安装系统 chromium 或设置环境变量 CYBER_BROWSER_PATH 指向 Chrome/Chromium（平台不会自动下载浏览器）")

// chromiumPath 复用上游 tools/headless.Discover（CYBER_BROWSER_PATH → 系统查找，含 /usr/bin/chromium）；
// 找不到时直接报错，不交给 go-rod 的下载回退。
func chromiumPath() (string, error) {
	binary, err := headless.Discover()
	if err != nil {
		return "", fmt.Errorf("%w（%v）", errBrowserMissing, err)
	}
	if binary.Path == "" {
		return "", errBrowserMissing
	}
	return binary.Path, nil
}

// chromiumInstance 是本包启动的一个无头 Chromium，代理固定为中继地址。
type chromiumInstance struct {
	launcher   *launcher.Launcher
	controlURL string
}

func launchChromium(proxy string) (*chromiumInstance, error) {
	path, err := chromiumPath()
	if err != nil {
		return nil, err
	}
	l := launcher.New().Bin(path).Leakless(false).Headless(true).
		// 与上游 playwright 启动参数一致（ignore-certificate-errors 使 MITM 证书可用）。
		Set("disable-gpu").Set("no-sandbox").Set("disable-dev-shm-usage").Set("ignore-certificate-errors").Set("allow-insecure-localhost").
		// 出口：全部经中继；<-loopback> 取消 Chrome 对 localhost/127.0.0.1 的隐式直连。
		Set("proxy-server", proxy).Set("proxy-bypass-list", "<-loopback>").
		// 尽量关闭旁路与后台网络：QUIC、DNS 预取、超链接审计 ping、组件更新、域可靠性上报、WebRTC 非代理 UDP。
		Set("disable-quic").Set("dns-prefetch-disable").Set("no-pings").Set("disable-component-update").
		Set("disable-domain-reliability").Set("force-webrtc-ip-handling-policy", "disable_non_proxied_udp").
		Set("webrtc-ip-handling-policy", "disable_non_proxied_udp").Set("no-default-browser-check").
		// 关闭会主动联系 Google 服务的功能与后台请求（优化提示、翻译、媒体路由、自动填充服务器、网络授时、安全浏览、
		// 字段试验、扩展与组件），并把 Google 账号/搜索/变体服务地址指向不可达的本机端口。仍会出现的少量后台请求
		// 同样经出口按策略放行/拒绝并记录（见 BROWSER.md 已知限制）。
		Set("disable-features", "site-per-process", "TranslateUI", "Translate", "OptimizationHints", "OptimizationHintsFetching",
			"OptimizationGuideModelDownloading", "MediaRouter", "DialMediaRouteProvider", "AutofillServerCommunication",
			"CertificateTransparencyComponentUpdater", "InterestFeedContentSuggestions", "PrivacySandboxSettings4", "HttpsUpgrades",
			"NetworkTimeServiceQuerying", "SafeBrowsing").
		Set("disable-field-trial-config").Set("disable-sync").Set("disable-extensions").Set("disable-default-apps").
		Set("disable-search-engine-choice-screen").Set("safebrowsing-disable-auto-update").Set("no-service-autorun").
		Set("password-store", "basic").Set("google-base-url", unreachableURL).Set("gaia-url", unreachableURL).
		Set("lso-url", unreachableURL).Set("variations-server-url", unreachableURL)
	controlURL, err := l.Launch()
	if err != nil {
		l.Kill()
		return nil, fmt.Errorf("启动 Chromium 失败: %w", err)
	}
	return &chromiumInstance{launcher: l, controlURL: controlURL}, nil
}

// alive 只探测 CDP 端口是否还在（kill-all 或浏览器崩溃后需重启）。
func (c *chromiumInstance) alive() bool {
	u, err := url.Parse(c.controlURL)
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (c *chromiumInstance) close() {
	if c == nil || c.launcher == nil {
		return
	}
	c.launcher.Kill()
	done := make(chan struct{})
	go func() { c.launcher.Cleanup(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

// browserSession 是一个 Agent 会话独享的浏览器：独立 Chromium 进程 + 独立中继 + 独立上游 Command，
// 不同会话之间不共享页面、Cookie 或出口归属。
type browserSession struct {
	sessionID string
	mu        sync.Mutex
	relay     *egressRelay
	chrome    *chromiumInstance
	command   *playwright.Command
}

type browserExtension struct {
	capability *Capability
	workDir    string

	endpoint *egressEndpoint
	katana   *katana.Command
	subs     []*corehooks.Subscription

	mu       sync.Mutex
	sessions map[string]*browserSession
	closed   bool
}

// BrowserExtension 返回模型节点用的浏览器（playwright）与爬虫（katana）扩展；须在
// EgressExtension 之后装配（取其 egress.Endpoint）。workDir 是节点工作区（截图、状态文件等相对路径）。
func (c *Capability) BrowserExtension(workDir string) extension.Extension {
	return &browserExtension{capability: c, workDir: workDir, sessions: map[string]*browserSession{}}
}

func (x *browserExtension) Load(scope *extension.Scope) error {
	endpoint, err := extension.Use[egress.Endpoint](scope)
	if err != nil {
		return err
	}
	ours, ok := endpoint.(*egressEndpoint)
	if !ok {
		return errors.New("浏览器/爬虫需要 wscyber 全工具出口（EgressExtension）")
	}
	x.endpoint = ours
	logger, err := extension.Use[telemetry.Logger](scope)
	if err != nil {
		return err
	}
	stream, err := extension.Use[*events.Stream](scope)
	if err != nil {
		return err
	}
	registry, err := extension.Use[*corehooks.Registry](scope)
	if err != nil {
		return err
	}
	// 启动默认代理用不带令牌的基础地址：只有本包注入的调用令牌才能出网。
	x.katana = katana.New().WithLogger(logger).WithProxy(ours.ProxyURL()).WithEvents(stream)
	x.katana.SetWorkDir(x.workDir)
	usage := playwright.New(x.workDir)
	if err := extension.Add(scope,
		coretool.Command{Name: "playwright", Usage: usage.Usage(), DescriptionPath: "cyber://skills/runtime/playwright.md", Run: x.runPlaywright},
		coretool.Command{Name: "katana", Usage: katana.Usage(), Run: x.runKatana},
	); err != nil {
		return err
	}
	x.subs = append(x.subs, agenthooks.SessionEnd.On(registry, "wscyber-browser-session-end", func(_ context.Context, event agenthooks.SessionEvent) (struct{}, error) {
		x.dispose(event.SessionID)
		return struct{}{}, nil
	}))
	return nil
}

func (x *browserExtension) Close(ctx context.Context) error {
	x.mu.Lock()
	x.closed = true
	ids := make([]string, 0, len(x.sessions))
	for id := range x.sessions {
		ids = append(ids, id)
	}
	x.mu.Unlock()
	for _, id := range ids {
		x.dispose(id)
	}
	var err error
	for _, sub := range x.subs {
		err = errors.Join(err, sub.Close(ctx))
	}
	return err
}

func (x *browserExtension) session(sessionID string) (*browserSession, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed {
		return nil, coretool.ErrUnavailable
	}
	if s := x.sessions[sessionID]; s != nil {
		return s, nil
	}
	relay, err := newEgressRelay(x.endpoint.ProxyURL(), func(req *http.Request) {
		x.endpoint.denyUnrouted(sessionID, egressToolBrowser, req, "浏览器后台请求：当前没有进行中的 playwright 调用，中继未发送")
	})
	if err != nil {
		return nil, err
	}
	s := &browserSession{sessionID: sessionID, relay: relay}
	x.sessions[sessionID] = s
	return s, nil
}

func (x *browserExtension) dispose(sessionID string) {
	x.mu.Lock()
	s := x.sessions[sessionID]
	delete(x.sessions, sessionID)
	x.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopBrowser()
	s.mu.Unlock()
	s.relay.Close()
}

// stopBrowser 关闭上游 Command（页面与 CDP 连接）并结束本包启动的 Chromium；调用方持 s.mu。
func (s *browserSession) stopBrowser() {
	if s.command != nil {
		done := make(chan struct{})
		command := s.command
		go func() { command.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		s.command = nil
	}
	s.chrome.close()
	s.chrome = nil
}

// ensure 返回可用的上游 Command；浏览器未启动或已退出（kill-all、崩溃）时重新启动。
func (s *browserSession) ensure(workDir string) (*playwright.Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.command != nil && s.chrome != nil && s.chrome.alive() {
		return s.command, nil
	}
	s.stopBrowser()
	chrome, err := launchChromium(s.relay.URL())
	if err != nil {
		return nil, fmt.Errorf("playwright: %w", err)
	}
	command := playwright.New(workDir)
	command.SetBrowserMode(false, chrome.controlURL)
	s.chrome, s.command = chrome, command
	return command, nil
}

// playwrightDenied 拒绝改变出口或接管浏览器的参数；返回空串表示允许。
func playwrightDenied(args []string) string {
	for i, arg := range args {
		name, _, _ := strings.Cut(arg, "=")
		switch name {
		case "--proxy-server", "--proxy-bypass":
			return "不允许自定义浏览器代理（" + name + "）：浏览器流量固定经平台出口"
		case "--cdp", "--cdp-url":
			return "不允许连接外部浏览器（" + name + "）"
		case "--headed":
			return "浏览器只以无头模式运行，不允许 --headed"
		}
		if arg == "attach" && playwrightSubcommandIndex(args) == i {
			return "不允许 attach 外部浏览器"
		}
	}
	return ""
}

// playwrightSubcommandIndex 跳过全局 -s 参数，返回子命令位置（-1 表示无）。
func playwrightSubcommandIndex(args []string) int {
	for i := 0; i < len(args); i++ {
		if args[i] == "-s" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-s=") {
			continue
		}
		return i
	}
	return -1
}

func (x *browserExtension) runPlaywright(ctx context.Context, execution *coretool.Execution) (any, error) {
	if reason := playwrightDenied(execution.Args); reason != "" {
		return nil, fmt.Errorf("playwright: %s（wscyber 出口策略）", reason)
	}
	if _, err := chromiumPath(); err != nil {
		return nil, fmt.Errorf("playwright: %w", err)
	}
	inv := operation.InvocationFromContext(ctx)
	s, err := x.session(inv.SessionID)
	if err != nil {
		return nil, err
	}
	// 先激活本次调用的令牌再启动/使用浏览器：启动期间的请求也归属本次调用。
	proxyURL, _, release := x.endpoint.egressFor(ctx, egressToolBrowser)
	deactivate := s.relay.activate(proxyURL)
	defer func() {
		deactivate() // 关闭本次调用的隧道与在途转发
		release()    // 上游等在途 flow 写完后失效令牌
	}()
	command, err := s.ensure(x.workDir)
	if err != nil {
		return nil, err
	}
	result, err := command.Run(ctx, execution)
	if index := playwrightSubcommandIndex(execution.Args); index >= 0 && execution.Args[index] == "kill-all" {
		s.mu.Lock()
		s.stopBrowser()
		s.mu.Unlock()
	}
	return result, err
}

// katanaFlag 把 katana 参数规范为不带前导短横线的名字与（可选）值。
func katanaFlag(arg string) (string, string, bool) {
	if !strings.HasPrefix(arg, "-") || arg == "-" {
		return "", "", false
	}
	name, value, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	return name, value, true
}

var katanaDeniedFlags = map[string]string{
	"proxy":         "不允许自定义代理：爬虫流量固定经平台出口",
	"chrome-ws-url": "不允许连接外部浏览器", "cwu": "不允许连接外部浏览器",
	"system-chrome-path": "不允许指定浏览器程序", "scp": "不允许指定浏览器程序",
	"show-browser": "爬虫浏览器只以无头模式运行", "sb": "爬虫浏览器只以无头模式运行",
	"headless-options": "不允许追加浏览器启动参数", "ho": "不允许追加浏览器启动参数",
	"kb-validate-secrets": "不允许向第三方服务验证密钥（其出站未核实经平台出口）",
}

// katanaPlan 检查参数：返回拒绝原因，以及是否需要浏览器（-headless/-hybrid）。
func katanaPlan(args []string) (string, bool) {
	browser := false
	for _, arg := range args {
		name, value, ok := katanaFlag(arg)
		if !ok {
			continue
		}
		if reason, denied := katanaDeniedFlags[name]; denied {
			return reason + "（-" + name + "）", false
		}
		switch name {
		case "headless", "hl", "hybrid", "hh":
			if value != "false" {
				browser = true
			}
		}
	}
	return "", browser
}

func (x *browserExtension) runKatana(ctx context.Context, execution *coretool.Execution) (any, error) {
	reason, needsBrowser := katanaPlan(execution.Args)
	if reason != "" {
		return nil, fmt.Errorf("katana: %s（wscyber 出口策略）", reason)
	}
	if needsBrowser {
		if _, err := chromiumPath(); err != nil {
			return nil, fmt.Errorf("katana: %w", err)
		}
	}
	inv := operation.InvocationFromContext(ctx)
	proxyURL, caPath, release := x.endpoint.egressFor(ctx, egressToolKatana)
	defer release()
	// 显式参数优先于 katana（goflags）配置文件：出口与浏览器相关项一律由平台给值，配置文件改不了。
	args := append([]string(nil), execution.Args...)
	args = append(args, "-proxy="+proxyURL, "-show-browser=false", "-kb-validate-secrets=false")
	if needsBrowser {
		relay, err := newEgressRelay(x.endpoint.ProxyURL(), func(req *http.Request) {
			x.endpoint.denyUnrouted(inv.SessionID, egressToolKatana, req, "katana 浏览器在调用结束后的请求，中继未发送")
		})
		if err != nil {
			return nil, err
		}
		deactivate := relay.activate(proxyURL)
		chrome, err := launchChromium(relay.URL())
		if err != nil {
			deactivate()
			relay.Close()
			return nil, fmt.Errorf("katana: %w", err)
		}
		defer func() {
			deactivate()
			chrome.close()
			relay.Close()
		}()
		args = append(args, "-chrome-ws-url="+chrome.controlURL)
	} else {
		args = append(args, "-headless=false", "-hybrid=false", "-chrome-ws-url=")
	}
	execution.Args = args
	execution.Env = append(withoutProxyEnv(execution.Env), coretool.EgressEnvironment(proxyURL, caPath)...)
	return x.katana.Run(ctx, execution)
}

// withoutProxyEnv 去掉终端进程注入的代理/CA 变量（那是 terminal 的令牌），换成本次 katana 调用的。
func withoutProxyEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		switch strings.ToUpper(key) {
		case "ALL_PROXY", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "CURL_CA_BUNDLE", "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "GIT_SSL_CAINFO":
			continue
		}
		out = append(out, item)
	}
	return out
}

// ensureToolConfigHome：katana 依赖的 goflags 会在用户配置目录（$XDG_CONFIG_HOME 或 $HOME/.config）下
// 生成默认配置；HOME 不可写时 katana 报错。未设置 XDG_CONFIG_HOME 时由 Hub 指到 state-dir 下。
func EnsureToolConfigHome(stateDir string) error {
	if os.Getenv("XDG_CONFIG_HOME") != "" {
		return nil
	}
	dir := stateDir + "/tool-config"
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Setenv("XDG_CONFIG_HOME", dir)
}

var _ extension.Extension = (*browserExtension)(nil)
