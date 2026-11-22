package webapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/egress"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	"github.com/chainreactors/cyber/core/telemetry"
	coretool "github.com/chainreactors/cyber/core/tool"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
)

func TestBrowserArgumentGuards(t *testing.T) {
	denied := [][]string{
		{"open", "http://a.example.test", "--proxy-server", "http://127.0.0.1:1"},
		{"open", "http://a.example.test", "--proxy-server=socks5://x"},
		{"goto", "http://a.example.test", "--proxy-bypass", "*"},
		{"open", "http://a.example.test", "--cdp", "ws://127.0.0.1:9222/x"},
		{"open", "http://a.example.test", "--cdp-url=ws://x"},
		{"open", "http://a.example.test", "--headed"},
		{"attach", "--cdp", "ws://127.0.0.1:9222/x"},
		{"-s", "s1", "attach", "ws://x"},
	}
	for _, args := range denied {
		if playwrightDenied(args) == "" {
			t.Errorf("playwright args admitted: %v", args)
		}
	}
	for _, args := range [][]string{{"goto", "http://a.example.test"}, {"-s=s1", "click", "s1", "#attach"}, {"open", "http://a.example.test", "--session", "attach"}} {
		if reason := playwrightDenied(args); reason != "" {
			t.Errorf("playwright args denied %v: %s", args, reason)
		}
	}
	for _, args := range [][]string{{"-u", "x", "-proxy", "http://p"}, {"-u", "x", "--proxy=http://p"}, {"-u", "x", "-cwu", "ws://x"}, {"-u", "x", "-chrome-ws-url=ws://x"},
		{"-u", "x", "-scp", "/bin/sh"}, {"-u", "x", "-sb"}, {"-u", "x", "-ho", "--proxy-server=x"}, {"-u", "x", "-kb-validate-secrets"}} {
		if reason, _ := katanaPlan(args); reason == "" {
			t.Errorf("katana args admitted: %v", args)
		}
	}
	if reason, browser := katanaPlan([]string{"-u", "x", "-d", "2", "-jc"}); reason != "" || browser {
		t.Fatalf("plain katana = %q %v", reason, browser)
	}
	for _, flag := range []string{"-headless", "-hl", "--hybrid", "-hh", "-headless=true"} {
		if reason, browser := katanaPlan([]string{"-u", "x", flag}); reason != "" || !browser {
			t.Fatalf("katana %s = %q %v", flag, reason, browser)
		}
	}
	if _, browser := katanaPlan([]string{"-u", "x", "-headless=false"}); browser {
		t.Fatal("headless=false needs browser")
	}
	env := withoutProxyEnv([]string{"HTTP_PROXY=http://t@h", "https_proxy=x", "SSL_CERT_FILE=/a", "PATH=/bin", "no_proxy=*"})
	if strings.Join(env, ",") != "PATH=/bin" {
		t.Fatalf("env = %v", env)
	}
}

func TestBrowserCommandRules(t *testing.T) {
	risky := []string{
		`playwright click s1 "#delete-account"`, `playwright -s s1 click "button[name=removeUser]"`,
		`playwright fill s1 "#confirm" "DELETE"`, `playwright goto http://a.example.test/admin/users/1/delete`,
		`playwright evaluate s1 "fetch('/api/items/1',{method:'DELETE'})"`, `playwright set-extra-headers s1 '{"X-HTTP-Method-Override":"DELETE"}'`,
		`playwright open "http://a.example.test/reset?all=1" --session s2`,
		"katana -u http://a.example.test -headless", "katana -u http://a.example.test -hl", "katana -u http://a.example.test --hybrid",
		"katana -u http://a.example.test -aff", "katana -u http://a.example.test -automatic-form-fill", "katana -u http://a.example.test -al user:pass",
		`bash -c "playwright click s1 '#drop-table'"`, "playwright click s1 $SEL", "katana $ARGS",
	}
	for _, command := range risky {
		if commandReviewReason(command) == "" {
			t.Errorf("risky browser command not matched: %s", command)
		}
	}
	safe := []string{
		"playwright goto http://a.example.test/items", "playwright open http://a.example.test --session s1", "playwright click s1 '#next-page'",
		"playwright screenshot s1", `playwright inner-text s1 "#delete-help"`, "playwright snapshot s1 --json", "playwright close s1",
		"katana -u http://a.example.test -d 2 -jc", "katana -u http://a.example.test -headless=false", "katana -list urls.txt -silent",
	}
	for _, command := range safe {
		if reason := commandReviewReason(command); reason != "" {
			t.Errorf("safe browser command matched %q: %s", reason, command)
		}
	}
}

// 审批：approval=all 时 playwright/katana（经 bash）都挂起；high_risk 按规则表；命令边界兜底同一规则。
func TestBrowserApprovalPolicy(t *testing.T) {
	capability, _, task, inv, _, _ := newHTTPToolFixture(t, http.NotFoundHandler())
	h := newApprovalHarness(t, capability)
	run := func(context.Context, string) (*aop.ToolResult, error) { return coretool.TextResult("ok"), nil }
	exec := func(command, callID string) error {
		inv.CallID = callID
		_, err := h.execute(t.Context(), inv, "bash", fmt.Sprintf(`{"command":%q}`, command), run)
		return err
	}
	for i, command := range []string{"playwright goto http://a.example.test/items", "katana -u http://a.example.test -d 2"} {
		if err := exec(command, fmt.Sprintf("free-%d", i)); err != nil {
			t.Fatalf("high_risk gated %q: %v", command, err)
		}
	}
	for i, command := range []string{`playwright click s1 "#delete-account"`, "katana -u http://a.example.test -aff"} {
		done := make(chan error, 1)
		go func() { done <- exec(command, fmt.Sprintf("risky-%d", i)) }()
		pending := h.waitPending(t, task.InitialSessionID)
		if !strings.Contains(pending.GetDecision().GetReason(), "高危终端命令") {
			t.Fatalf("reason = %s", pending.GetDecision().GetReason())
		}
		h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), false)
		if err := <-done; err == nil {
			t.Fatalf("rejected %q executed", command)
		}
	}
	ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: "dynamic"})
	if admission, _ := capability.admitCommand(ctx, corehooksCommand("playwright", "click", "s1", "#delete-all")); admission.Deny == nil {
		t.Fatal("expanded destructive playwright admitted without approval")
	}
	if admission, _ := capability.admitCommand(ctx, corehooksCommand("katana", "-u", "http://a.example.test", "-d", "1")); admission.Deny != nil {
		t.Fatal("plain katana denied at command boundary")
	}
	if _, err := capability.store.setPolicy(t.Context(), task.ID, ScopeOrigins, ApprovalAll); err != nil {
		t.Fatal(err)
	}
	for i, command := range []string{"playwright goto http://a.example.test/items", "katana -u http://a.example.test -d 2"} {
		done := make(chan error, 1)
		go func() { done <- exec(command, fmt.Sprintf("all-%d", i)) }()
		pending := h.waitPending(t, task.InitialSessionID)
		if !strings.Contains(pending.GetDecision().GetReason(), "全部人工审批") {
			t.Fatalf("all reason = %s", pending.GetDecision().GetReason())
		}
		h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), true)
		if err := <-done; err != nil {
			t.Fatalf("approved %q failed: %v", command, err)
		}
	}
}

// browserFixture 装配与模型节点相同的出口 + 命令注册表 + 浏览器/爬虫扩展（不经模型）。
type browserFixture struct {
	*egressFixture
	commands coretool.CommandExecutor
}

func newBrowserFixture(t *testing.T, needChromium bool) *browserFixture {
	t.Helper()
	if needChromium {
		if _, err := chromiumPath(); err != nil {
			t.Skip("本机无 Chromium：" + err.Error())
		}
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := newEgressFixture(t) // 复用目标、任务与库；其自身的出口集合不用于本测试
	f := &browserFixture{egressFixture: base}
	var err error
	f.set, err = extension.New(
		extension.Provided[*corehooks.Registry](corehooks.New()),
		extension.Provided[telemetry.Logger](telemetry.NopLogger()),
		extension.Provided[*events.Stream](events.New()),
		coretool.NewCommandRegistry(),
		base.capability.EgressExtension(),
		base.capability.BrowserExtension(t.TempDir()),
		extension.Func{LoadFunc: func(scope *extension.Scope) error {
			if f.endpoint, err = extension.Use[egress.Endpoint](scope); err != nil {
				return err
			}
			f.commands, err = extension.Use[coretool.CommandExecutor](scope)
			return err
		}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.set.Close(context.Background()) })
	return f
}

// run 像终端解释器那样调用已注册命令（同一 command.before 边界）。
func (f *browserFixture) run(t *testing.T, inv operation.Invocation, argv ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(operation.ContextWithInvocation(context.Background(), inv), 90*time.Second)
	defer cancel()
	ctx, finish := operation.Begin(ctx, "process", "bash")
	defer finish(nil)
	var out bytes.Buffer
	_, err := f.commands.Run(ctx, argv, &coretool.Execution{ID: "exec-" + inv.CallID, Dir: t.TempDir(), Stdout: &out, Stderr: io.Discard})
	return out.String(), err
}

func (f *browserFixture) waitRows(t *testing.T, want int, where string, args ...any) []egressFlowModel {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := f.rows(t, where, args...)
		if len(rows) >= want || time.Now().After(deadline) {
			return rows
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestBrowserEgressAttributionScopeAndBypass(t *testing.T) {
	f := newBrowserFixture(t, true)
	inv := operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "turn-b", CallID: "pw-inside"}
	out, err := f.run(t, inv, "playwright", "goto", f.inside.url("/page"))
	if err != nil || !strings.Contains(out, "inside ok") {
		t.Fatalf("goto inside = %q %v", out, err)
	}
	rows := f.waitRows(t, 1, "call_id = ? AND url = ?", "pw-inside", f.inside.url("/page"))
	if len(rows) != 1 || rows[0].Tool != "browser" || rows[0].TaskID != f.task.ID || rows[0].SessionID != inv.SessionID || rows[0].TurnID != "turn-b" ||
		rows[0].Decision != "allowed" || rows[0].HTTPStatus.Int64 != 200 {
		t.Fatalf("inside rows = %+v", rows)
	}
	// HTTPS（CONNECT + MITM，浏览器忽略 MITM 证书校验）。
	inv.CallID = "pw-tls"
	if out, err := f.run(t, inv, "playwright", "goto", f.tlsInside.url("/tls")); err != nil || !strings.Contains(out, "tls inside ok") {
		t.Fatalf("goto tls inside = %q %v", out, err)
	}
	if rows := f.waitRows(t, 1, "call_id = ? AND url = ?", "pw-tls", f.tlsInside.url("/tls")); len(rows) != 1 || rows[0].Tool != "browser" || rows[0].Decision != "allowed" {
		t.Fatalf("tls rows = %+v", rows)
	}
	// 越界 origin：代理拒绝并记录，页面拿不到内容，目标未被连接。
	inv.CallID = "pw-outside"
	out, _ = f.run(t, inv, "playwright", "goto", f.outside.url("/secret"))
	if strings.Contains(out, "outside reached") || f.outside.conns.Load() != 0 {
		t.Fatalf("outside reached: %q conns=%d", out, f.outside.conns.Load())
	}
	rows = f.waitRows(t, 1, "call_id = ? AND url = ?", "pw-outside", f.outside.url("/secret"))
	if len(rows) == 0 || rows[0].Tool != "browser" || rows[0].Decision != "denied" || rows[0].TaskID != f.task.ID || !strings.Contains(rows[0].Error, "不在任务授权 origins 内") {
		t.Fatalf("outside rows = %+v", rows)
	}
	// 模型参数改代理/接管浏览器：拒绝，不执行。
	for i, argv := range [][]string{
		{"playwright", "open", f.outside.url("/x"), "--proxy-server", "http://127.0.0.1:9"},
		{"playwright", "open", f.outside.url("/x"), "--cdp", "ws://127.0.0.1:9/devtools/browser/x"},
		{"playwright", "open", f.outside.url("/x"), "--headed"},
		{"playwright", "attach", "--cdp", "ws://127.0.0.1:9/devtools/browser/x"},
		{"katana", "-u", f.outside.url("/"), "-proxy", "http://127.0.0.1:9"},
		{"katana", "-u", f.outside.url("/"), "-hl", "-chrome-ws-url", "ws://127.0.0.1:9/x"},
	} {
		inv.CallID = fmt.Sprintf("bypass-%d", i)
		if _, err := f.run(t, inv, argv...); err == nil || !strings.Contains(err.Error(), "出口策略") {
			t.Fatalf("bypass %v = %v", argv, err)
		}
	}
	// 会话页面：open 后在下一次调用里读取；两次调用各自归属。
	inv.CallID = "pw-open"
	if _, err := f.run(t, inv, "playwright", "open", f.inside.url("/session"), "--session", "s1"); err != nil {
		t.Fatal(err)
	}
	inv.CallID = "pw-read"
	if out, err := f.run(t, inv, "playwright", "inner-text", "s1", "body"); err != nil || !strings.Contains(out, "inside ok") {
		t.Fatalf("session read = %q %v", out, err)
	}
	if rows := f.waitRows(t, 1, "call_id = ? AND url = ?", "pw-open", f.inside.url("/session")); len(rows) != 1 || rows[0].Tool != "browser" {
		t.Fatalf("open rows = %+v", rows)
	}
	// 不限制：越界放行并记录。
	if _, err := f.capability.store.setPolicy(t.Context(), f.task.ID, ScopeUnrestricted, ApprovalHighRisk); err != nil {
		t.Fatal(err)
	}
	inv.CallID = "pw-unrestricted"
	if out, err := f.run(t, inv, "playwright", "goto", f.outside.url("/free")); err != nil || !strings.Contains(out, "outside reached") {
		t.Fatalf("unrestricted = %q %v", out, err)
	}
	if rows := f.waitRows(t, 1, "call_id = ? AND url = ?", "pw-unrestricted", f.outside.url("/free")); len(rows) != 1 || rows[0].Decision != "allowed" || rows[0].Tool != "browser" {
		t.Fatalf("unrestricted rows = %+v", rows)
	}
}

func TestBrowserConcurrentSessionsKeepAttribution(t *testing.T) {
	f := newBrowserFixture(t, true)
	other, err := f.capability.store.Save(t.Context(), "task-browser-b", "第二个任务", "并发浏览器", []string{originOf(t, f.outside.url(""))})
	if err != nil {
		t.Fatal(err)
	}
	type job struct {
		session, call, url, task string
	}
	jobs := []job{
		{f.task.InitialSessionID, "a-1", f.inside.url("/a"), f.task.ID},
		{other.Task.InitialSessionID, "b-1", f.outside.url("/b"), other.Task.ID},
	}
	// 两个会话并发（同一会话内的调用按顺序，见 BROWSER.md 同会话并发说明）。
	errs := make(chan error, len(jobs)*2)
	for round := 0; round < 2; round++ {
		var wg sync.WaitGroup
		for _, j := range jobs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				inv := operation.Invocation{SessionID: j.session, TurnID: "t", CallID: fmt.Sprintf("%s-%d", j.call, round)}
				out, err := f.run(t, inv, "playwright", "goto", fmt.Sprintf("%s?round=%d", j.url, round))
				if err != nil || !strings.Contains(out, "ok") && !strings.Contains(out, "reached") {
					errs <- fmt.Errorf("%s: %q %v", inv.CallID, out, err)
				}
			}()
		}
		wg.Wait()
	}
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for _, j := range jobs {
		for round := 0; round < 2; round++ {
			call := fmt.Sprintf("%s-%d", j.call, round)
			rows := f.waitRows(t, 1, "call_id = ?", call)
			for _, row := range rows {
				if row.SessionID != j.session || row.TaskID != j.task || row.Tool != "browser" {
					t.Fatalf("cross attribution %s: %+v", call, row)
				}
			}
			found := false
			for _, row := range rows {
				found = found || row.URL == fmt.Sprintf("%s?round=%d", j.url, round) && row.Decision == "allowed"
			}
			if !found {
				t.Fatalf("%s rows = %+v", call, rows)
			}
		}
	}
}

func TestKatanaEgressInsideAndOutside(t *testing.T) {
	f := newBrowserFixture(t, false)
	// 授权内首页链接到越界目标；katana 只按 host 判 scope（两者同为 127.0.0.1），因此会尝试越界链接。
	page := newEgressTestTarget(t, false, fmt.Sprintf(`<html><body><a href="/next">next</a><a href="%s">away</a></body></html>`, f.outside.url("/away")))
	saved, err := f.capability.store.Save(t.Context(), "task-katana", "爬虫fixture", "katana 出口", []string{originOf(t, page.url(""))})
	if err != nil {
		t.Fatal(err)
	}
	task := saved.Task
	inv := operation.Invocation{SessionID: task.InitialSessionID, TurnID: "turn-k", CallID: "katana-1"}
	out, err := f.run(t, inv, "katana", "-u", page.url("/"), "-d", "2", "-timeout", "5")
	if f.outside.conns.Load() != 0 || f.outside.requests.Load() != 0 {
		t.Fatalf("katana reached outside: %q %v", out, err)
	}
	rows := f.waitRows(t, 2, "call_id = ?", "katana-1")
	var allowed, denied bool
	for _, row := range rows {
		if row.Tool != "katana" || row.SessionID != inv.SessionID || row.TaskID != task.ID {
			t.Fatalf("katana row = %+v", row)
		}
		allowed = allowed || row.Decision == "allowed" && row.URL == page.url("/")
		denied = denied || row.Decision == "denied" && strings.HasPrefix(row.URL, f.outside.url("/away"))
	}
	if !allowed || !denied {
		t.Fatalf("katana rows (out=%q err=%v) = %+v", out, err, rows)
	}
}

// corehooksCommand 构造命令边界事件。
func corehooksCommand(name string, args ...string) toolhooks.CommandEvent {
	return toolhooks.CommandEvent{Name: name, Args: args}
}

func TestKatanaHeadlessUsesPlatformBrowser(t *testing.T) {
	f := newBrowserFixture(t, true)
	page := newEgressTestTarget(t, false, `<html><body><a href="/next">next</a></body></html>`)
	saved, err := f.capability.store.Save(t.Context(), "task-katana-hl", "无头爬虫fixture", "katana 无头出口", []string{originOf(t, page.url(""))})
	if err != nil {
		t.Fatal(err)
	}
	inv := operation.Invocation{SessionID: saved.Task.InitialSessionID, TurnID: "turn-kh", CallID: "katana-hl"}
	out, err := f.run(t, inv, "katana", "-u", page.url("/"), "-hl", "-d", "1", "-timeout", "10", "-ct", "20s")
	rows := f.waitRows(t, 1, "call_id = ? AND url = ?", "katana-hl", page.url("/"))
	if len(rows) == 0 || rows[0].Tool != "katana" || rows[0].TaskID != saved.Task.ID || rows[0].Decision != "allowed" {
		t.Fatalf("katana headless rows (out=%q err=%v) = %+v", out, err, rows)
	}
}

func TestEgressRelayIdleDeniesAndDedupes(t *testing.T) {
	var idle []string
	var mu sync.Mutex
	relay, err := newEgressRelay("http://127.0.0.1:9", func(req *http.Request) {
		mu.Lock()
		idle = append(idle, req.Method+" "+req.URL.String())
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer relay.Close()
	proxy, _ := http.NewRequest("GET", relay.URL(), nil)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy.URL)}, Timeout: 5 * time.Second}
	for i := 0; i < 3; i++ {
		response, err := client.Get("http://a.example.test/poll")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("idle status = %d", response.StatusCode)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(idle) != 1 {
		t.Fatalf("idle records = %v", idle)
	}
}
