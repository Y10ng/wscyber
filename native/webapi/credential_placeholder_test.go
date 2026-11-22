package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

const (
	authzAliceSecret = "alice-pass-synthetic"
	authzBobSecret   = "bob-pass-synthetic"
)

type authzHit struct {
	method string
	path   string
	query  string
	header http.Header
	body   string
}

type authzTarget struct {
	server *httptest.Server
	mu     sync.Mutex
	hits   []authzHit
}

func newAuthzTarget(t *testing.T) *authzTarget {
	t.Helper()
	target := &authzTarget{}
	target.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		target.mu.Lock()
		target.hits = append(target.hits, authzHit{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: string(body)})
		target.mu.Unlock()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		switch r.URL.Path {
		case "/login/alice":
			_, _ = fmt.Fprintf(w, "user=%s password=%s body=%s", r.Header.Get("X-Username"), r.Header.Get("X-Password"), body)
		case "/disconnect":
			if r.Header.Get("Authorization") != "Bearer alice-token" {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
				}
				return
			}
			_, _ = w.Write([]byte("connected"))
		case "/resource":
			switch r.Header.Get("Authorization") {
			case "Bearer alice-token":
				_, _ = w.Write([]byte(`{"owner":"alice","data":"A"}`))
			case "Bearer bob-token", "Bearer " + authzBobSecret:
				_, _ = w.Write([]byte(`{"owner":"bob","data":"BOB-PRIVATE-LONG"}`))
			case "":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"authentication required"}`))
			default:
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
			}
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	t.Cleanup(target.server.Close)
	return target
}

func (t *authzTarget) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.hits)
}

func (t *authzTarget) last() authzHit {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hits[len(t.hits)-1]
}

type authzFixture struct {
	*assetFixture
	target   *authzTarget
	task     *Task
	launcher *fakeLauncher
	http     *httpTool
	compare  *authzCompareTool
	inv      operation.Invocation
}

func newAuthzFixture(t *testing.T) *authzFixture {
	t.Helper()
	f := newAssetFixture(t)
	target := newAuthzTarget(t)
	assetJSON := mustToolArgumentsJSON(t, map[string]any{"name": "合成越权目标", "objective": "比较多个测试账号", "origins": []string{target.server.URL}})
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1", assetJSON); status != 201 {
		t.Fatalf("asset = %d %s", status, raw)
	}
	for id, credential := range map[string]map[string]any{
		"alice": {"label": "普通用户 A", "username": "alice", "secret": authzAliceSecret},
		"bob":   {"label": "普通用户 B", "username": "bob", "secret": authzBobSecret},
		"empty": {"label": "无口令", "username": "empty"},
	} {
		if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/"+id, mustToolArgumentsJSON(t, credential)); status != 200 {
			t.Fatalf("credential %s = %d %s", id, status, raw)
		}
	}
	otherJSON := mustToolArgumentsJSON(t, map[string]any{"name": "其它资产", "origins": []string{"https://api.example.test"}})
	if status, _, raw := f.do("PUT", "/api/webapi/assets/other", otherJSON); status != 201 {
		t.Fatalf("other asset = %d %s", status, raw)
	}
	if status, _, raw := f.do("PUT", "/api/webapi/assets/other/credentials/other", `{"username":"other","secret":"other-secret"}`); status != 200 {
		t.Fatalf("other credential = %d %s", status, raw)
	}
	launcher := newFakeLauncher(productAgent("node-a"))
	f.capability.launcher = launcher
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/t1", ""); status != 201 {
		t.Fatalf("launch = %d %s", status, raw)
	}
	task, err := f.capability.store.Get(t.Context(), "t1")
	if err != nil || task == nil {
		t.Fatalf("task = %+v %v", task, err)
	}
	return &authzFixture{assetFixture: f, target: target, task: task, launcher: launcher,
		http:    &httpTool{store: f.capability.store, secrets: f.capability.secrets, approvals: f.capability.approvals},
		compare: &authzCompareTool{store: f.capability.store, secrets: f.capability.secrets},
		inv:     operation.Invocation{SessionID: task.InitialSessionID, TurnID: "turn-authz", CallID: "call-1"}}
}

func (f *authzFixture) invokeHTTP(t *testing.T, inv operation.Invocation, method, rawURL, body string, headers map[string]string) (*aop.ToolResult, toolEvidenceResult, string) {
	t.Helper()
	arguments := mustToolArgumentsJSON(t, map[string]any{"method": method, "url": rawURL, "body": body, "headers": headers})
	result, err := f.http.Execute(operation.ContextWithInvocation(t.Context(), inv), arguments)
	if err != nil {
		t.Fatal(err)
	}
	text := coretool.ResultText(result)
	var evidence toolEvidenceResult
	if strings.HasPrefix(text, "{") {
		if err := json.Unmarshal([]byte(text), &evidence); err != nil {
			t.Fatalf("HTTP tool result %q: %v", text, err)
		}
	}
	return result, evidence, text
}

func (f *authzFixture) invokeCompare(t *testing.T, inv operation.Invocation, evidenceID string, variants []authzCompareVariantInput) (*aop.ToolResult, string) {
	t.Helper()
	arguments := mustToolArgumentsJSON(t, authzCompareArguments{EvidenceID: evidenceID, Variants: variants})
	result, err := f.compare.Execute(operation.ContextWithInvocation(t.Context(), inv), arguments)
	if err != nil {
		t.Fatal(err)
	}
	return result, coretool.ResultText(result)
}

func (f *authzFixture) original(t *testing.T) toolEvidenceResult {
	t.Helper()
	result, evidence, text := f.invokeHTTP(t, f.inv, "POST", f.target.server.URL+"/resource", `{"action":"read"}`,
		map[string]string{"Authorization": "Bearer alice-token", "Cookie": "sid=alice"})
	if result.IsError || evidence.Status != requestCompleted {
		t.Fatalf("original = %s", text)
	}
	return evidence
}

func TestWebAPICredentialPlaceholdersHTTPAndEvidence(t *testing.T) {
	f := newAuthzFixture(t)
	result, evidence, text := f.invokeHTTP(t, f.inv, "POST", f.target.server.URL+"/login/{{cred:alice.username}}?from={{cred:alice.username}}",
		`{"username":"{{cred:alice.username}}","password":"{{cred:alice.secret}}","unknown":"{{cred:alice.password}}"}`,
		map[string]string{"X-Username": "{{cred:alice.username}}", "X-Password": "{{cred:alice.secret}}", "X-Unsupported": "{{cred:alice.password}}"})
	if result.IsError || evidence.Status != requestCompleted {
		t.Fatalf("placeholder request = %s", text)
	}
	hit := f.target.last()
	if hit.path != "/login/alice" || hit.query != "from=alice" || hit.header.Get("X-Username") != "alice" || hit.header.Get("X-Password") != authzAliceSecret ||
		hit.header.Get("X-Unsupported") != "{{cred:alice.password}}" || !strings.Contains(hit.body, `"password":"`+authzAliceSecret+`"`) || !strings.Contains(hit.body, "{{cred:alice.password}}") {
		t.Fatalf("target hit = %+v", hit)
	}
	// 回给模型的预览/文本不含任何资产 secret，响应里回显的口令也回替为对应占位符。
	if strings.Contains(text, authzAliceSecret) || strings.Contains(text, authzBobSecret) || !strings.Contains(text, "{{cred:alice.secret}}") {
		t.Fatalf("tool result redaction = %s", text)
	}
	model, err := f.capability.store.evidenceByID(t.Context(), f.task.ID, evidence.EvidenceID)
	if err != nil || model == nil {
		t.Fatalf("evidence = %+v %v", model, err)
	}
	flow, err := decodeEvidenceFlow(model)
	if err != nil {
		t.Fatal(err)
	}
	// 存证保留实际发送/接收字节，而不是把占位符写回原件。
	if !strings.Contains(flow.GetRequest().GetUrl(), "/login/alice?from=alice") || !strings.Contains(string(flow.GetRequest().GetBody()), authzAliceSecret) ||
		!strings.Contains(string(flow.GetResponse().GetBody()), authzAliceSecret) {
		t.Fatalf("stored flow did not retain actual bytes: %+v", flow)
	}
	foundSecretHeader := false
	for _, header := range flow.GetRequest().GetHeaders() {
		if header.GetName() == "X-Password" && header.GetValue() == authzAliceSecret {
			foundSecretHeader = true
		}
	}
	if !foundSecretHeader {
		t.Fatalf("stored headers = %+v", flow.GetRequest().GetHeaders())
	}
}

func TestWebAPICredentialPlaceholderErrorsSendNothing(t *testing.T) {
	f := newAuthzFixture(t)
	plain, err := f.capability.store.Save(t.Context(), "plain", "无资产任务", "占位符必须失败", []string{originOf(t, f.target.server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, session, placeholder, want string
	}{
		{"no asset", plain.Task.InitialSessionID, "{{cred:alice.secret}}", "任务未关联资产"},
		{"other asset", f.task.InitialSessionID, "{{cred:other.secret}}", "不属于本任务关联资产"},
		{"no secret", f.task.InitialSessionID, "{{cred:empty.secret}}", "未设置 secret"},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			before := f.target.count()
			inv := operation.Invocation{SessionID: test.session, TurnID: "turn-errors", CallID: fmt.Sprintf("bad-%d", index)}
			result, _, text := f.invokeHTTP(t, inv, "POST", f.target.server.URL+"/login/alice", "x="+test.placeholder, nil)
			if !result.IsError || !strings.Contains(text, test.placeholder) || !strings.Contains(text, test.want) || !strings.Contains(text, "0发送") || f.target.count() != before {
				t.Fatalf("error = %s, hits %d→%d", text, before, f.target.count())
			}
		})
	}
	// 错误文本回替覆盖同资产全部账号，且已有占位符不被二次破坏。
	resolver, err := loadCredentialResolver(t.Context(), f.capability.store, f.capability.secrets, f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := redactToolResult(stopTool("错误 "+authzAliceSecret+" / "+authzBobSecret+" / {{cred:alice.secret}}"), resolver)
	text := coretool.ResultText(result)
	if strings.Contains(text, authzAliceSecret) || strings.Contains(text, authzBobSecret) || !strings.Contains(text, "错误 {{cred:alice.secret}} / {{cred:bob.secret}} / {{cred:alice.secret}}") {
		t.Fatalf("error redaction = %s", text)
	}
}

func TestWebAPICredentialFirstMessages(t *testing.T) {
	f := newAuthzFixture(t)
	if len(f.launcher.runs) != 1 {
		t.Fatalf("runs = %d", len(f.launcher.runs))
	}
	research := runText(f.launcher.runs[0])
	for _, want := range []string{"凭据 ID：alice", "口令：用占位符 {{cred:alice.secret}}（平台发送时替换，你看不到明文）", "凭据 ID：bob", "凭据 ID：empty"} {
		if !strings.Contains(research, want) {
			t.Fatalf("research first message lacks %q:\n%s", want, research)
		}
	}
	if strings.Contains(research, authzAliceSecret) || strings.Contains(research, authzBobSecret) || strings.Contains(research, "口令：用占位符 {{cred:empty.secret}}") {
		t.Fatalf("research first message leaked or invented secret:\n%s", research)
	}
	source := &FindingDetail{Number: "WSC-000001", Content: findingContent{Title: "合成越权", Description: "中性描述"}}
	for _, variant := range []pinnedMessageVariant{verificationMessageVariant, retestMessageVariant} {
		base := pinnedFirstMessage(variant, f.task.Origins, source)
		message, err := f.capability.followupFirstMessage(t.Context(), f.task.ID, base)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(message, base) || !strings.Contains(message, "测试账号（资产登记") || !strings.Contains(message, "{{cred:bob.secret}}") ||
			strings.Contains(message, authzAliceSecret) || strings.Contains(message, authzBobSecret) {
			t.Fatalf("followup first message:\n%s", message)
		}
	}
}

func TestWebAPICredentialPlaceholderApprovalPlanUsesResolvedRequest(t *testing.T) {
	f := newAuthzFixture(t)
	args := mustToolArgumentsJSON(t, map[string]any{"method": "POST", "url": f.target.server.URL + "/safe", "body": `{"operation":"{{cred:alice.username}}"}`, "headers": map[string]string{"X-Password": "{{cred:alice.secret}}"}})
	plan := planHTTP(operation.ContextWithInvocation(t.Context(), f.inv), f.capability.store, f.capability.secrets, f.inv, args)
	if plan == nil || plan.req.Header.Get("X-Password") != authzAliceSecret || !strings.Contains(string(plan.body), "alice") || plan.outOfScope {
		t.Fatalf("resolved approval plan = %+v", plan)
	}
}

func TestWebAPICredentialRedactionDoesNotCorruptShortSecretPlaceholder(t *testing.T) {
	resolver := &credentialResolver{redactions: []credentialRedaction{{secret: "alice", placeholder: "{{cred:alice.secret}}"}}}
	got := resolver.redact("alice / {{cred:alice.secret}}")
	if got != "{{cred:alice.secret}} / {{cred:alice.secret}}" {
		t.Fatalf("redaction = %s", got)
	}
}

func TestWebAPICredentialFollowupWithoutAssetUnchanged(t *testing.T) {
	f := newAuthzFixture(t)
	outcome, err := f.capability.store.Save(context.Background(), "plain-followup", "无资产", "无账号块", []string{"https://api.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	message, err := f.capability.followupFirstMessage(t.Context(), outcome.Task.ID, "固定首条消息")
	if err != nil || message != "固定首条消息" {
		t.Fatalf("message = %q, %v", message, err)
	}
}
