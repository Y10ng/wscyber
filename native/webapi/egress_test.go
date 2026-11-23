package webapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/core/egress"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	terminaltool "github.com/chainreactors/cyber/tools/terminal"
	"github.com/uptrace/bun"
)

// egressTarget 是计数目标：requests 统计收到的请求，conns 统计建立的 TCP 连接
// （拒绝必须发生在连目标之前，所以被拒目标两者都应为 0）。
type egressTestTarget struct {
	server   *httptest.Server
	requests atomic.Int32
	conns    atomic.Int32
}

func newEgressTestTarget(t *testing.T, useTLS bool, body string) *egressTestTarget {
	t.Helper()
	target := &egressTestTarget{}
	target.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target.requests.Add(1)
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("出口令牌泄露到目标")
		}
		_, _ = io.WriteString(w, body)
	}))
	target.server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			target.conns.Add(1)
		}
	}
	if useTLS {
		target.server.StartTLS()
	} else {
		target.server.Start()
	}
	t.Cleanup(target.server.Close)
	return target
}

// url 返回目标地址；TLS 目标用 localhost（MITM 证书按主机名签发）。
func (target *egressTestTarget) url(path string) string {
	return strings.Replace(target.server.URL, "127.0.0.1", "localhost", 1) + path
}

type egressFixture struct {
	capability *Capability
	task       *Task
	endpoint   egress.Endpoint
	set        *extension.Set
	mux        *http.ServeMux
	database   testHub

	inside, outside, tlsInside, tlsOutside, large *egressTestTarget
}

func newEgressFixture(t *testing.T) *egressFixture {
	t.Helper()
	capability, _, _, _, _, database := newHTTPToolFixture(t, http.NotFoundHandler())
	f := &egressFixture{capability: capability, database: database}
	f.inside = newEgressTestTarget(t, false, "inside ok")
	f.outside = newEgressTestTarget(t, false, "outside reached")
	f.tlsInside = newEgressTestTarget(t, true, "tls inside ok")
	f.tlsOutside = newEgressTestTarget(t, true, "tls outside reached")
	f.large = newEgressTestTarget(t, false, strings.Repeat("x", 10000))
	outcome, err := capability.store.Save(t.Context(), "task-egress", "本机出口fixture", "终端出口策略", []string{
		originOf(t, f.inside.url("")), originOf(t, f.tlsInside.url("")), originOf(t, f.large.url("")),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.task = outcome.Task
	f.set, err = extension.New(
		extension.Provided[*corehooks.Registry](corehooks.New()),
		capability.EgressExtension(),
		extension.Func{LoadFunc: func(scope *extension.Scope) error {
			endpoint, err := extension.Use[egress.Endpoint](scope)
			f.endpoint = endpoint
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
	f.mux = http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		f.mux.Handle(route.Pattern, route.Handler)
	}
	return f
}

// call 模拟终端一次进程：在调用上下文里取出口（与终端 Start 相同的 process 操作），
// 用注入的代理与 CA 起客户端；返回的 done 等同进程结束（释放令牌，等在途 flow 写完）。
type egressCall struct {
	client     *http.Client
	proxyURL   string
	caPath     string
	connectRes atomic.Int32
	done       func()
}

func (f *egressFixture) call(t *testing.T, inv operation.Invocation) *egressCall {
	t.Helper()
	ctx, cancel := operation.Begin(operation.ContextWithInvocation(context.Background(), inv), "process", "fixture command")
	proxyURL, caPath, release := f.endpoint.Egress(ctx)
	if !strings.Contains(proxyURL, "@127.0.0.1:") || caPath == "" {
		t.Fatalf("egress route = %q ca=%q", proxyURL, caPath)
	}
	call := &egressCall{proxyURL: proxyURL, caPath: caPath, done: func() { release(); cancel(nil) }}
	call.client = egressClient(t, proxyURL, caPath, &call.connectRes)
	return call
}

func egressClient(t *testing.T, proxyURL, caPath string, connectStatus *atomic.Int32) *http.Client {
	t.Helper()
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pem, err := os.ReadFile(caPath)
	if err != nil || !pool.AppendCertsFromPEM(pem) {
		t.Fatalf("read egress CA: %v", err)
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(parsed), TLSClientConfig: &tls.Config{RootCAs: pool}, DisableKeepAlives: true,
		OnProxyConnectResponse: func(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
			connectStatus.Store(int32(response.StatusCode))
			return nil
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

// get 返回（HTTP 状态或 CONNECT 拒绝状态，body）。
func (c *egressCall) get(t *testing.T, rawURL string) (int, string) {
	t.Helper()
	c.connectRes.Store(0)
	response, err := c.client.Get(rawURL)
	if err != nil {
		if status := c.connectRes.Load(); status != 0 {
			return int(status), ""
		}
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func (f *egressFixture) rows(t *testing.T, where string, args ...any) []egressFlowModel {
	t.Helper()
	var models []egressFlowModel
	if err := f.capability.store.orm.NewSelect().Model(&models).Where(where, args...).Order("created_at ASC", "id ASC").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	return models
}

func (f *egressFixture) api(t *testing.T, path string) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
	var value map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("%s: %d %s", path, recorder.Code, recorder.Body.String())
	}
	return recorder.Code, value
}

func TestEgressScopeOriginsEnforcesBeforeDialAndRecords(t *testing.T) {
	f := newEgressFixture(t)
	inv := operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "turn-1", CallID: "bash-1"}
	call := f.call(t, inv)
	if status, body := call.get(t, f.inside.url("/a?q=1")); status != 200 || body != "inside ok" {
		t.Fatalf("authorized HTTP = %d %q", status, body)
	}
	if status, _ := call.get(t, f.outside.url("/secret")); status != http.StatusProxyAuthRequired {
		t.Fatalf("outside HTTP status = %d", status)
	}
	if status, body := call.get(t, f.tlsInside.url("/tls")); status != 200 || body != "tls inside ok" {
		t.Fatalf("authorized CONNECT = %d %q", status, body)
	}
	if status, _ := call.get(t, f.tlsOutside.url("/tls")); status != http.StatusProxyAuthRequired {
		t.Fatalf("outside CONNECT status = %d", status)
	}
	call.done()
	if f.outside.requests.Load() != 0 || f.outside.conns.Load() != 0 || f.tlsOutside.requests.Load() != 0 || f.tlsOutside.conns.Load() != 0 {
		t.Fatalf("denied targets were contacted: http=%d/%d tls=%d/%d", f.outside.requests.Load(), f.outside.conns.Load(), f.tlsOutside.requests.Load(), f.tlsOutside.conns.Load())
	}
	if f.inside.requests.Load() != 1 || f.tlsInside.requests.Load() != 1 {
		t.Fatalf("authorized targets = %d %d", f.inside.requests.Load(), f.tlsInside.requests.Load())
	}
	rows := f.rows(t, "task_id = ?", f.task.ID)
	if len(rows) != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	byURL := map[string]egressFlowModel{}
	for _, row := range rows {
		if row.SessionID != inv.SessionID || row.TurnID != "turn-1" || row.CallID != "bash-1" || row.Tool != "terminal" {
			t.Fatalf("attribution = %+v", row)
		}
		byURL[row.URL] = row
	}
	allowed := byURL[f.inside.url("/a?q=1")]
	if allowed.Decision != "allowed" || !allowed.Sent.Valid || !allowed.Sent.Bool || allowed.HTTPStatus.Int64 != 200 || allowed.Method != "GET" ||
		allowed.ResponseBytes != int64(len("inside ok")) || allowed.PreviewTruncated || allowed.Error != "" || !allowed.CompletedAt.Valid || len(allowed.FlowPB) == 0 {
		t.Fatalf("allowed HTTP row = %+v", allowed)
	}
	outsideOrigin := originOf(t, f.outside.url(""))
	denied := byURL[f.outside.url("/secret")]
	if denied.Decision != "denied" || !denied.Sent.Valid || denied.Sent.Bool || denied.HTTPStatus.Valid || denied.Error != "不在任务授权 origins 内："+outsideOrigin {
		t.Fatalf("denied HTTP row = %+v", denied)
	}
	tlsAllowed := byURL[f.tlsInside.url("/tls")]
	if tlsAllowed.Decision != "allowed" || tlsAllowed.HTTPStatus.Int64 != 200 || tlsAllowed.Host != "localhost" {
		t.Fatalf("allowed CONNECT inner row = %+v", tlsAllowed)
	}
	tlsHostPort := strings.TrimPrefix(f.tlsOutside.url(""), "https://")
	connectDenied := byURL[tlsHostPort]
	if connectDenied.Decision != "denied" || connectDenied.Method != "CONNECT" || connectDenied.Sent.Bool || connectDenied.Error != "不在任务授权 origins 内："+tlsHostPort {
		t.Fatalf("denied CONNECT row = %+v (all %+v)", connectDenied, rows)
	}

	// 详情：摘要 + Flow（请求/响应头与 body 预览）。
	status, detail := f.api(t, "/api/webapi/tasks/"+f.task.ID+"/egress/"+allowed.ID)
	flow, _ := detail["flow"].(map[string]any)
	request, _ := flow["request"].(map[string]any)
	response, _ := flow["response"].(map[string]any)
	summary, _ := detail["summary"].(map[string]any)
	if status != 200 || summary["decision"] != "allowed" || summary["sent"] != true || summary["http_status"] != float64(200) ||
		request["url"] != f.inside.url("/a?q=1") || response["statusCode"] != float64(200) || response["body"] == nil || detail["flow_protobuf_base64"] == "" {
		t.Fatalf("allowed detail = %d %v", status, detail)
	}
	status, detail = f.api(t, "/api/webapi/tasks/"+f.task.ID+"/egress/"+denied.ID)
	summary, _ = detail["summary"].(map[string]any)
	flow, _ = detail["flow"].(map[string]any)
	if status != 200 || summary["sent"] != false || summary["http_status"] != nil || summary["completed_at"] == nil || flow["response"] != nil ||
		strings.Contains(fmt.Sprint(flow), "cyber-route-") {
		t.Fatalf("denied detail = %d %v", status, detail)
	}
}

func TestEgressSessionAttributionAndNoToken(t *testing.T) {
	f := newEgressFixture(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&verificationModel{ID: "verify-1", TaskID: f.task.ID, FindingID: "f-1", SourceRevisionID: "r-1", SessionID: "verify-session", CreatedAt: now}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&reportModel{ID: "report-1", TaskID: f.task.ID, SessionID: "report-session", ItemsJSON: "[]", CreatedAt: now}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"verify-session", "report-session"} {
		call := f.call(t, operation.Invocation{SessionID: session, TurnID: "t", CallID: "c"})
		if status, _ := call.get(t, f.outside.url("/x")); status != http.StatusProxyAuthRequired {
			t.Fatalf("%s outside = %d", session, status)
		}
		if status, _ := call.get(t, f.inside.url("/x")); status != 200 {
			t.Fatalf("%s inside = %d", session, status)
		}
		call.done()
		rows := f.rows(t, "session_id = ?", session)
		if len(rows) != 2 || rows[0].TaskID != f.task.ID || rows[1].TaskID != f.task.ID {
			t.Fatalf("%s rows = %+v", session, rows)
		}
	}
	// 普通对话：放行并记录，task_id 空。
	plain := f.call(t, operation.Invocation{SessionID: "plain-chat", TurnID: "t", CallID: "c"})
	if status, body := plain.get(t, f.outside.url("/free")); status != 200 || body != "outside reached" {
		t.Fatalf("plain session = %d %q", status, body)
	}
	plain.done()
	if rows := f.rows(t, "session_id = ?", "plain-chat"); len(rows) != 1 || rows[0].TaskID != "" || rows[0].Decision != "allowed" || rows[0].HTTPStatus.Int64 != 200 {
		t.Fatalf("plain rows = %+v", rows)
	}
	if f.outside.requests.Load() != 1 {
		t.Fatalf("outside requests = %d", f.outside.requests.Load())
	}
	// 无令牌（基础代理地址）与伪造令牌：拒绝，不连目标。
	var connectStatus atomic.Int32
	for _, proxyURL := range []string{f.endpoint.ProxyURL(), strings.Replace(f.endpoint.ProxyURL(), "http://", "http://cyber-route-FORGED@", 1)} {
		client := egressClient(t, proxyURL, f.endpoint.CAPath(), &connectStatus)
		response, err := client.Get(f.inside.url("/no-token"))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusProxyAuthRequired {
			t.Fatalf("tokenless %s = %d", proxyURL, response.StatusCode)
		}
	}
	if rows := f.rows(t, "url = ?", f.inside.url("/no-token")); len(rows) != 2 || rows[0].Decision != "denied" || rows[0].SessionID != "" || rows[0].TaskID != "" || !strings.Contains(rows[0].Error, "无出口令牌") {
		t.Fatalf("tokenless rows = %+v", rows)
	}
	// 进程结束后令牌失效。
	expired := f.call(t, operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "t", CallID: "expired"})
	expired.done()
	if status, _ := expired.get(t, f.inside.url("/expired")); status != http.StatusProxyAuthRequired {
		t.Fatalf("expired token = %d", status)
	}
	if f.inside.requests.Load() != 2 {
		t.Fatalf("inside requests = %d", f.inside.requests.Load())
	}
}

func TestEgressPolicySaveTakesEffectAndReadFailureDenies(t *testing.T) {
	f := newEgressFixture(t)
	call := f.call(t, operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "t", CallID: "c"})
	defer call.done()
	if status, _ := call.get(t, f.outside.url("/before")); status != http.StatusProxyAuthRequired {
		t.Fatalf("before = %d", status)
	}
	// 同一进程（同一令牌）下一次请求即按新策略。
	if _, err := f.capability.store.setPolicy(t.Context(), f.task.ID, ScopeUnrestricted, ApprovalHighRisk); err != nil {
		t.Fatal(err)
	}
	if status, body := call.get(t, f.outside.url("/after")); status != 200 || body != "outside reached" {
		t.Fatalf("unrestricted = %d %q", status, body)
	}
	if status, _ := call.get(t, f.tlsOutside.url("/after")); status != 200 {
		t.Fatalf("unrestricted CONNECT = %d", status)
	}
	if _, err := f.capability.store.setPolicy(t.Context(), f.task.ID, ScopeOrigins, ApprovalHighRisk); err != nil {
		t.Fatal(err)
	}
	if status, _ := call.get(t, f.outside.url("/again")); status != http.StatusProxyAuthRequired {
		t.Fatalf("restored origins = %d", status)
	}
	// 已保存策略读不出（非法值）→ 按拒绝，授权内也不放行。
	if _, err := f.capability.store.db().ExecContext(t.Context(), "UPDATE webapi_task_policies SET scope = 'bogus' WHERE task_id = ?", f.task.ID); err != nil {
		t.Fatal(err)
	}
	if status, _ := call.get(t, f.inside.url("/broken")); status != http.StatusProxyAuthRequired {
		t.Fatalf("broken policy = %d", status)
	}
	if f.outside.requests.Load() != 1 || f.tlsOutside.requests.Load() != 1 || f.inside.requests.Load() != 0 {
		t.Fatalf("requests outside=%d tls=%d inside=%d", f.outside.requests.Load(), f.tlsOutside.requests.Load(), f.inside.requests.Load())
	}
	call.done()
	rows := f.rows(t, "task_id = ?", f.task.ID)
	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.Decision)
	}
	if strings.Join(got, ",") != "denied,allowed,allowed,denied,denied" || !strings.Contains(rows[4].Error, "读取任务测试策略失败") {
		t.Fatalf("decisions = %v rows=%+v", got, rows)
	}
}

func TestEgressTruncatedBodyAndCurlSubprocess(t *testing.T) {
	f := newEgressFixture(t)
	call := f.call(t, operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "t", CallID: "large"})
	if status, body := call.get(t, f.large.url("/big")); status != 200 || len(body) != 10000 {
		t.Fatalf("large = %d %d", status, len(body))
	}
	call.done()
	rows := f.rows(t, "call_id = ?", "large")
	if len(rows) != 1 || rows[0].ResponseBytes != 10000 || !rows[0].PreviewTruncated || rows[0].Error != "" || rows[0].HTTPStatus.Int64 != 200 {
		t.Fatalf("large rows = %+v", rows)
	}
	status, detail := f.api(t, "/api/webapi/tasks/"+f.task.ID+"/egress/"+rows[0].ID)
	capture, _ := detail["capture"].(map[string]any)
	flow, _ := detail["flow"].(map[string]any)
	if status != 200 || capture["preview_truncated"] != true || capture["response_bytes"] != float64(10000) || capture["preview_limit"] != float64(4096) ||
		!strings.Contains(fmt.Sprint(flow["error"]), "response body truncated (4096/10000 bytes retained)") {
		t.Fatalf("large detail = %d %v", status, detail)
	}

	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("无 curl")
	}
	sub := f.call(t, operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "t", CallID: "curl"})
	defer sub.done()
	// 与终端相同的注入：ALL/HTTP/HTTPS_PROXY 与 CA 变量，其余环境清空。
	env := append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, coretool.EgressEnvironment(sub.proxyURL, sub.caPath)...)
	run := func(target string) (string, error) {
		command := exec.Command(curl, "-sS", "--max-time", "10", "-o", "-", "-w", "|%{http_code}", target)
		command.Env = env
		output, err := command.CombinedOutput()
		return string(output), err
	}
	if output, err := run(f.inside.url("/curl")); err != nil || output != "inside ok|200" {
		t.Fatalf("curl inside = %q %v", output, err)
	}
	if output, err := run(f.tlsInside.url("/curl")); err != nil || output != "tls inside ok|200" {
		t.Fatalf("curl https inside = %q %v", output, err)
	}
	if output, _ := run(f.outside.url("/curl")); !strings.HasSuffix(output, "|407") {
		t.Fatalf("curl outside = %q", output)
	}
	if output, err := run(f.tlsOutside.url("/curl")); err == nil || f.tlsOutside.conns.Load() != 0 {
		t.Fatalf("curl https outside = %q %v conns=%d", output, err, f.tlsOutside.conns.Load())
	}
	if f.outside.requests.Load() != 0 || f.outside.conns.Load() != 0 {
		t.Fatal("curl reached outside target")
	}
	sub.done()
	if rows := f.rows(t, "call_id = ?", "curl"); len(rows) != 4 {
		t.Fatalf("curl rows = %+v", rows)
	}
}

func TestEgressAPIListFilterCursorAndErrors(t *testing.T) {
	f := newEgressFixture(t)
	call := f.call(t, operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "t", CallID: "c"})
	for i := 0; i < 3; i++ {
		call.get(t, f.inside.url(fmt.Sprintf("/in/%d", i)))
		call.get(t, f.outside.url(fmt.Sprintf("/out/%d", i)))
	}
	call.done()
	verify := f.call(t, operation.Invocation{SessionID: "verify-session", TurnID: "t", CallID: "c"})
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&verificationModel{ID: "verify-1", TaskID: f.task.ID, FindingID: "f-1", SourceRevisionID: "r-1", SessionID: "verify-session", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	verify.get(t, f.outside.url("/verify"))
	verify.done()

	base := "/api/webapi/tasks/" + f.task.ID + "/egress"
	status, value := f.api(t, base)
	items, _ := value["items"].([]any)
	if status != 200 || len(items) != 7 || value["has_more"] != false || value["next_cursor"] != nil {
		t.Fatalf("default list = %d %v", status, value)
	}
	first, _ := items[0].(map[string]any)
	if first["session_id"] != "verify-session" || first["decision"] != "denied" {
		t.Fatalf("not newest first: %v", first)
	}
	for _, key := range []string{"id", "session_id", "turn_id", "call_id", "tool", "method", "url", "decision", "sent", "http_status", "error", "created_at", "completed_at"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("summary lacks %s: %v", key, first)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		path := base + "?limit=3"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		status, value = f.api(t, path)
		items, _ = value["items"].([]any)
		if status != 200 || len(items) == 0 || len(items) > 3 {
			t.Fatalf("page %d = %d %v", page, status, value)
		}
		for _, item := range items {
			id := item.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatalf("duplicate %s", id)
			}
			seen[id] = true
		}
		if value["has_more"] != true {
			break
		}
		cursor, _ = value["next_cursor"].(string)
	}
	if len(seen) != 7 {
		t.Fatalf("paged %d items", len(seen))
	}
	if _, value = f.api(t, base+"?decision=denied"); len(value["items"].([]any)) != 4 {
		t.Fatalf("denied filter = %v", value)
	}
	if _, value = f.api(t, base+"?decision=allowed&session_id="+f.task.InitialSessionID); len(value["items"].([]any)) != 3 {
		t.Fatalf("allowed+session filter = %v", value)
	}
	if _, value = f.api(t, base+"?session_id=verify-session"); len(value["items"].([]any)) != 1 {
		t.Fatalf("session filter = %v", value)
	}
	for path, want := range map[string]int{
		base + "?limit=0": 400, base + "?limit=201": 400, base + "?decision=maybe": 400, base + "?cursor=bm90LXZhbGlk": 400,
		"/api/webapi/tasks/missing/egress": 404, base + "/wx_missing": 404, "/api/webapi/tasks/missing/egress/wx_1": 404,
	} {
		if status, value := f.api(t, path); status != want || value["error"] == nil {
			t.Fatalf("%s = %d %v", path, status, value)
		}
	}
}

func TestEgressCADirAndClose(t *testing.T) {
	f := newEgressFixture(t)
	caDir := filepath.Join(f.database.StateDir, "egress-ca")
	if !strings.HasPrefix(f.endpoint.CAPath(), caDir+string(os.PathSeparator)) {
		t.Fatalf("CA path %q not under %q", f.endpoint.CAPath(), caDir)
	}
	for path, want := range map[string]os.FileMode{caDir: 0o700, filepath.Join(caDir, "mitmproxy-ca.pem"): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Fatalf("%s mode = %v %v", path, info, err)
		}
	}
	address := strings.TrimPrefix(f.endpoint.ProxyURL(), "http://")
	if err := f.set.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.DialTimeout("tcp", address, time.Second); err == nil {
		conn.Close()
		t.Fatal("proxy still listening after Close")
	}
}

func TestEgressTargetMatching(t *testing.T) {
	origins := []string{"http://plain.example.test", "https://secure.example.test", "https://alt.example.test:8443"}
	cases := []struct {
		method, target string
		want           bool
		display        string
	}{
		{"GET", "http://plain.example.test/a", true, "http://plain.example.test"},
		{"GET", "http://PLAIN.example.test:80/a", true, "http://plain.example.test"},
		{"GET", "http://secure.example.test/a", false, "http://secure.example.test"},
		{"CONNECT", "secure.example.test:443", true, "https://secure.example.test"},
		{"CONNECT", "plain.example.test:443", false, "https://plain.example.test"},
		{"CONNECT", "plain.example.test:80", true, "http://plain.example.test"},
		{"CONNECT", "alt.example.test:8443", true, "alt.example.test:8443"},
		{"CONNECT", "alt.example.test:9443", false, "alt.example.test:9443"},
		{"CONNECT", "other.example.test:443", false, "https://other.example.test"},
	}
	for _, tc := range cases {
		var req *http.Request
		if tc.method == "CONNECT" {
			req = &http.Request{Method: "CONNECT", Host: tc.target, URL: &url.URL{Host: tc.target}, Header: http.Header{}}
		} else {
			req = httptest.NewRequest(tc.method, tc.target, nil)
		}
		target := egressTargetOf(req)
		if got := egressTargetAllowed(req, target, origins); got != tc.want || target.display != tc.display {
			t.Errorf("%s %s = %v %q, want %v %q", tc.method, tc.target, got, target.display, tc.want, tc.display)
		}
	}
}

// 真实上游终端工具：resolver 即本扩展的 Egress，代理/CA 变量由终端按进程注入，进程结束释放令牌。
func TestEgressRealTerminalBashTool(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("无 curl")
	}
	f := newEgressFixture(t)
	bash := terminaltool.NewBashTool(t.TempDir(), 10, nil).WithEgressProxy(f.endpoint.ProxyURL()).WithEgressProxyCA(f.endpoint.CAPath())
	bash.SetEgressResolver(f.endpoint.Egress)
	defer bash.Close()
	inv := operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "turn-bash", CallID: "call-bash"}
	command := fmt.Sprintf(`curl -sS -w '|%%{http_code}\n' %s; curl -sS -w '|%%{http_code}\n' %s; curl -sS -o /dev/null -w '|%%{http_code}\n' %s`,
		f.inside.url("/bash"), f.tlsInside.url("/bash"), f.outside.url("/bash"))
	arguments, _ := json.Marshal(map[string]string{"command": command})
	result, err := bash.Execute(operation.ContextWithInvocation(t.Context(), inv), string(arguments))
	if err != nil {
		t.Fatal(err)
	}
	text := coretool.ResultText(result)
	if !strings.Contains(text, "inside ok|200") || !strings.Contains(text, "tls inside ok|200") || !strings.Contains(text, "|407") {
		t.Fatalf("bash output = %q", text)
	}
	if f.outside.conns.Load() != 0 {
		t.Fatal("bash reached outside target")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows := f.rows(t, "call_id = ?", "call-bash")
		if len(rows) == 3 {
			for _, row := range rows {
				if row.TaskID != f.task.ID || row.SessionID != inv.SessionID || row.TurnID != "turn-bash" || row.Tool != "terminal" {
					t.Fatalf("bash row = %+v", row)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bash rows = %+v", rows)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
