package webapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop/traffic"
)

// K2 一键重放测试：全部为临时库合成数据（httptest 本地目标、虚构订单、合成 Flow 与测试 cookie/token）。

type replayHit struct {
	method, path, body string
	header             http.Header
}

// replayTarget 是本地测试目标：记录收到的请求，按当前配置回应；block 非空时先阻塞到关闭。
type replayTarget struct {
	*httptest.Server
	mu       sync.Mutex
	hits     []replayHit
	status   int
	body     string
	received chan struct{}
	block    chan struct{}
	hangup   bool
}

func newReplayTarget(t *testing.T) *replayTarget {
	target := &replayTarget{status: 200, body: `{"owner":"user-42","order":"1002"}`, received: make(chan struct{}, 16)}
	target.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		target.mu.Lock()
		target.hits = append(target.hits, replayHit{method: r.Method, path: r.URL.RequestURI(), body: string(raw), header: r.Header.Clone()})
		status, body, block, hangup := target.status, target.body, target.block, target.hangup
		target.mu.Unlock()
		target.received <- struct{}{}
		if block != nil {
			<-block
		}
		if hangup {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(target.Close)
	return target
}

func (r *replayTarget) set(status int, body string) {
	r.mu.Lock()
	r.status, r.body = status, body
	r.mu.Unlock()
}

func (r *replayTarget) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hits)
}

func (r *replayTarget) last() replayHit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits[len(r.hits)-1]
}

func (r *replayTarget) host() string { return strings.TrimPrefix(r.URL, "http://") }

// replayOriginalFlow 是关键步骤原件：带测试 cookie/token、同名多值头、Content-Length 与 HTTP/2 伪头。
func replayOriginalFlow(method, rawURL, host, requestBody string, status int32, responseBody string) *traffic.Flow {
	return &traffic.Flow{
		Id: "we_replay", Complete: true,
		Request: &traffic.HttpRequest{Method: method, Url: rawURL, Protocol: "HTTP/1.1", Body: []byte(requestBody), Headers: []*traffic.Header{
			{Name: ":authority", Value: host}, {Name: "Host", Value: host}, {Name: "User-Agent", Value: "fixture-agent/1"},
			{Name: "Cookie", Value: "sid=fixture-session-abcdef"}, {Name: "X-Multi", Value: "first"}, {Name: "Authorization", Value: "Bearer fixture-token"},
			{Name: "X-Multi", Value: "second"}, {Name: "Content-Length", Value: "999"}, {Name: "Content-Type", Value: "application/json"},
		}},
		Response: &traffic.HttpResponse{StatusCode: status, ReasonPhrase: http.StatusText(int(status)),
			Headers: []*traffic.Header{{Name: "Content-Type", Value: "application/json"}}, Body: []byte(responseBody)},
	}
}

type replayStep struct {
	flow          *traffic.Flow
	responseMarks []string
}

// replayVuln 在资产 a1 / 任务 t1 下建一个候选与 supported 验证，最小复现按 steps 顺序引用新证据；返回漏洞键与各步证据。
func (f *notifyFixture) replayVuln(title string, steps ...replayStep) (string, []requestModel) {
	f.t.Helper()
	first := steps[0].flow.Request
	fid, frev := f.addFinding("t1", title, notifyContent(), evidenceAt(first.Method, first.Url))
	v := f.newVerification("t1", fid, frev)
	evidence, refs, keySteps := []requestModel{}, []map[string]any{}, []map[string]any{}
	for index, step := range steps {
		model := f.addEvidence("t1", v.SessionID, step.flow)
		evidence = append(evidence, model)
		refs = append(refs, map[string]any{"evidence_id": model.ID, "relation": "supporting"})
		item := map[string]any{"title": fmt.Sprintf("第 %d 步", index+1), "evidence_id": model.ID, "request_marks": []string{"sid=fixture-session-abcdef"}}
		if len(step.responseMarks) > 0 {
			item["response_marks"] = step.responseMarks
		}
		keySteps = append(keySteps, item)
	}
	payload := map[string]any{"verdict": "supported", "method": "重放订单请求", "summary": "他人订单可读", "preconditions": "普通用户已登录",
		"evidence": refs, "impact": "普通用户可读取他人订单", "key_steps": keySteps}
	if result, message := f.callVerification(v, payload); result == nil {
		f.t.Fatalf("verification rejected: %s", message)
	}
	return vulnOf(f.vulns("a1", ""), fid).ID, evidence
}

func (f *notifyFixture) replay(key, body string) (int, map[string]any, string) {
	f.t.Helper()
	return f.do("POST", "/api/webapi/vulns/"+key+"/card/replay", body)
}

func replayFixture(t *testing.T, origins ...string) *notifyFixture {
	f := newNotifyFixture(t)
	raw, _ := json.Marshal(map[string]any{"name": "演示订单系统", "owner": "测试组", "environment": "预发", "known_info": "演示订单 API", "objective": "检查越权", "origins": origins})
	if status, _, body := f.do("PUT", "/api/webapi/assets/a1", string(raw)); status != 201 {
		t.Fatalf("create asset = %d %s", status, body)
	}
	f.launchManual("a1", "t1")
	return f
}

func replayAudits(t *testing.T, f *notifyFixture) []auditEventModel {
	return auditByAction(auditRows(t, f.capability), "vuln.replay")
}

func TestVulnCardReplaySuccessAndComparison(t *testing.T) {
	target := newReplayTarget(t)
	f := replayFixture(t, target.URL)
	host := target.host()
	key, originals := f.replayVuln("订单接口越权读取",
		replayStep{flow: replayOriginalFlow("POST", target.URL+"/orders/1002?expand=1", host, `{"order":"1002"}`, 200, `{"owner":"user-42","order":"1002"}`), responseMarks: []string{`"owner":"user-42"`}},
		replayStep{flow: replayOriginalFlow("GET", target.URL+"/orders/1002/items", host, "", 200, `{"items":[]}`)})

	// 1) 第 1 步：响应仍含 marks、状态码相同 → still_reproducible。
	status, value, raw := f.replay(key, `{"step":1}`)
	if status != 200 {
		t.Fatalf("replay = %d %s", status, raw)
	}
	hit := target.last()
	if target.count() != 1 || hit.method != "POST" || hit.path != "/orders/1002?expand=1" || hit.body != `{"order":"1002"}` {
		t.Fatalf("target got %+v", hit)
	}
	// 原值（不遮盖）发送；同名多值保持顺序；原件 User-Agent 替换默认值；Content-Length 由客户端按正文重算。
	if hit.header.Get("Cookie") != "sid=fixture-session-abcdef" || hit.header.Get("Authorization") != "Bearer fixture-token" ||
		strings.Join(hit.header.Values("X-Multi"), ",") != "first,second" || strings.Join(hit.header.Values("User-Agent"), ",") != "fixture-agent/1" ||
		hit.header.Get("Content-Type") != "application/json" || hit.header.Get("Content-Length") != "16" {
		t.Fatalf("target headers = %v", hit.header)
	}
	comparison := value["comparison"].(map[string]any)
	if comparison["verdict_hint"] != hintStillReproducible || comparison["same_status"] != true || comparison["response_marks_still_present"] != true {
		t.Fatalf("comparison = %v", comparison)
	}
	original, replayed := value["original"].(map[string]any), value["replayed"].(map[string]any)
	if original["evidence_id"] != originals[0].ID || original["status_code"] != float64(200) || original["body_bytes"] != float64(34) ||
		fmt.Sprint(original["marks_found"]) != `map[request:[sid=fixture-session-abcdef] response:["owner":"user-42"]]` {
		t.Fatalf("original = %v", original)
	}
	if replayed["status_code"] != float64(200) || replayed["capture_truncated"] != false || replayed["body_bytes"] != float64(34) ||
		fmt.Sprint(replayed["marks_found"]) != `map[request:[sid=fixture-session-abcdef] response:["owner":"user-42"]]` {
		t.Fatalf("replayed = %v", replayed)
	}
	request := value["request"].(map[string]any)
	if request["method"] != "POST" || request["url"] != target.URL+"/orders/1002?expand=1" || value["task_id"] != "t1" || value["step"] != float64(1) {
		t.Fatalf("response = %v", value)
	}
	firstEvidence := value["evidence_id"].(string)

	// 新证据：任务 t1 内、合成 session、source=replay，原件带原值请求与新响应。
	status, detail, raw := f.do("GET", "/api/webapi/tasks/t1/evidence/"+firstEvidence, "")
	metadata, _ := detail["metadata"].(map[string]any)
	if status != 200 || metadata["source"] != "replay" || metadata["session_id"] != replaySessionID || metadata["call_id"] != "step-1" ||
		metadata["turn_id"] != value["replay_id"] || metadata["status"] != requestCompleted || metadata["sent"] != true {
		t.Fatalf("evidence detail = %d %s", status, raw)
	}
	model, err := f.capability.store.evidenceByID(t.Context(), "t1", firstEvidence)
	if err != nil || model == nil {
		t.Fatal(err)
	}
	flow, err := decodeEvidenceFlow(model)
	if err != nil || string(flow.GetRequest().GetBody()) != `{"order":"1002"}` || !strings.Contains(string(flow.GetResponse().GetBody()), "user-42") {
		t.Fatalf("replay flow = %v %v", flow, err)
	}
	status, list, raw := f.do("GET", "/api/webapi/tasks/t1/evidence", "")
	sources := map[string]any{}
	for _, item := range list["items"].([]any) {
		entry := item.(map[string]any)
		sources[entry["evidence_id"].(string)] = entry["source"]
	}
	if status != 200 || sources[firstEvidence] != "replay" || sources[originals[0].ID] != nil {
		t.Fatalf("evidence list = %d %s", status, raw)
	}

	// 2) 限频：同一条目 10 秒内再点 → 429，目标未收到。
	status, value, raw = f.replay(key, `{"step":2}`)
	if status != 429 || value["retry_after"] != float64(10) || target.count() != 1 {
		t.Fatalf("rate limited = %d %s", status, raw)
	}
	f.advance(9 * time.Second)
	if status, value, _ := f.replay(key, ``); status != 429 || value["retry_after"] != float64(1) {
		t.Fatalf("rate limited after 9s = %d %v", status, value)
	}

	// 3) 第 1 步：marks 消失、状态码变化 → changed。
	f.advance(2 * time.Second)
	target.set(403, `{"error":"forbidden"}`)
	status, value, raw = f.replay(key, ``) // 空 body 默认第 1 步
	comparison = value["comparison"].(map[string]any)
	if status != 200 || value["step"] != float64(1) || comparison["verdict_hint"] != hintChanged || comparison["same_status"] != false || comparison["response_marks_still_present"] != false {
		t.Fatalf("changed replay = %d %s", status, raw)
	}

	// 4) 第 2 步：原步骤无 response marks、状态码相同 → unknown（无法判定）。
	f.advance(11 * time.Second)
	target.set(200, `{"items":[{"sku":"A"}]}`)
	status, value, raw = f.replay(key, `{"step":2}`)
	comparison = value["comparison"].(map[string]any)
	if status != 200 || comparison["verdict_hint"] != hintUnknown || comparison["same_status"] != true || comparison["response_marks_still_present"] != false ||
		target.last().method != "GET" || target.last().body != "" {
		t.Fatalf("no-marks replay = %d %s", status, raw)
	}
	if target.count() != 3 {
		t.Fatalf("target hits = %d", target.count())
	}

	// 卡片：replays[] 新到旧、lifecycle replay 升序；审计每次实际发送一条，不含请求/响应内容。
	card := f.card(key)
	if len(card.Replays) != 3 || card.Replays[0].VerdictHint != hintUnknown || card.Replays[0].Step != 2 || card.Replays[1].VerdictHint != hintChanged ||
		card.Replays[1].StatusCode != 403 || card.Replays[2].VerdictHint != hintStillReproducible || card.Replays[2].EvidenceID != firstEvidence || card.Replays[0].TaskID != "t1" {
		t.Fatalf("card replays = %+v", card.Replays)
	}
	titles := []string{}
	for _, event := range card.Lifecycle {
		if event.Kind == "replay" {
			if event.Actor != auditActorUser {
				t.Fatalf("replay actor = %+v", event)
			}
			titles = append(titles, event.Title+"|"+event.Detail)
		}
	}
	if strings.Join(titles, "\n") != "一键重放：仍可复现|POST /orders/1002 → 200\n一键重放：有变化|POST /orders/1002 → 403\n一键重放：无法判定|GET /orders/1002/items → 200" {
		t.Fatalf("lifecycle replay = %v", titles)
	}
	if !(lifecycleKindOrder["notify"] < lifecycleKindOrder["replay"] && lifecycleKindOrder["replay"] < lifecycleKindOrder["retest"]) {
		t.Fatalf("lifecycle order = %v", lifecycleKindOrder)
	}
	audits := replayAudits(t, f)
	if len(audits) != 3 || len(card.Appendix.Changes) != 3 || card.Appendix.Changes[0].Action != "vuln.replay" {
		t.Fatalf("audits = %d changes = %+v", len(audits), card.Appendix.Changes)
	}
	var auditDetail map[string]any
	if err := json.Unmarshal([]byte(audits[0].DetailJSON), &auditDetail); err != nil {
		t.Fatal(err)
	}
	if audits[0].Actor != auditActorUser || audits[0].TargetKind != "vuln" || audits[0].TargetID != key || audits[0].TaskID != "t1" || audits[0].AssetID != "a1" ||
		auditDetail["vuln_key"] != key || auditDetail["step"] != float64(1) || auditDetail["evidence_id"] != firstEvidence || auditDetail["original_evidence_id"] != originals[0].ID ||
		auditDetail["status_code"] != float64(200) || auditDetail["verdict_hint"] != hintStillReproducible {
		t.Fatalf("audit = %+v %s", audits[0], audits[0].DetailJSON)
	}
	for _, audit := range audits {
		if strings.Contains(audit.DetailJSON+audit.Summary, "fixture-session") || strings.Contains(audit.DetailJSON+audit.Summary, "fixture-token") || strings.Contains(audit.DetailJSON+audit.Summary, "user-42") {
			t.Fatalf("audit leaks content: %+v", audit)
		}
	}
	// 重放不改漏洞状态、不写处置、不登记推送。
	if card.Header.Status != VulnNew || card.Triage.UpdatedAt != nil || len(auditByAction(auditRows(t, f.capability), "vuln.triage")) != 0 || len(f.notifications()) != 0 {
		t.Fatalf("replay side effects: %+v %+v", card.Header, card.Triage)
	}
	if card.Replays[0].At == "" || len(card.Replays) > replayCardMax {
		t.Fatal("replay at missing")
	}
}

func TestVulnCardReplayCardKeepsLatestFive(t *testing.T) {
	target := newReplayTarget(t)
	f := replayFixture(t, target.URL)
	key, _ := f.replayVuln("订单接口越权读取", replayStep{flow: replayOriginalFlow("GET", target.URL+"/orders/1002", target.host(), "", 200, `{"owner":"user-42"}`), responseMarks: []string{"user-42"}})
	ids := []string{}
	for range 6 {
		status, value, raw := f.replay(key, `{"step":1}`)
		if status != 200 {
			t.Fatalf("replay = %d %s", status, raw)
		}
		ids = append(ids, value["replay_id"].(string))
		f.advance(replayRateWindow)
		time.Sleep(2 * time.Millisecond)
	}
	card := f.card(key)
	if len(card.Replays) != 5 || card.Replays[0].ReplayID != ids[5] || card.Replays[4].ReplayID != ids[1] {
		t.Fatalf("replays = %+v", card.Replays)
	}
	count := 0
	for _, event := range card.Lifecycle {
		if event.Kind == "replay" {
			count++
		}
	}
	if count != 6 {
		t.Fatalf("lifecycle replay events = %d", count)
	}
}

func TestVulnCardReplayDeniedAndReview(t *testing.T) {
	target, outsider := newReplayTarget(t), newReplayTarget(t)
	f := replayFixture(t, target.URL)
	// origin 不在任务授权内（默认 scope=origins）→ 403，不发送，留未发送记录，不登记审计。
	outKey, _ := f.replayVuln("外部接口", replayStep{flow: replayOriginalFlow("GET", outsider.URL+"/profile", outsider.host(), "", 200, `{"ok":true}`)})
	status, value, raw := f.replay(outKey, `{"step":1}`)
	if status != 403 || value["status"] != "denied" || value["error"] == nil || outsider.count() != 0 {
		t.Fatalf("denied = %d %s (outsider hits %d)", status, raw, outsider.count())
	}
	unsent, err := f.capability.store.evidenceByID(t.Context(), "t1", value["evidence_id"].(string))
	if err != nil || unsent == nil || unsent.Status != "denied" || !unsent.Sent.Valid || unsent.Sent.Bool || unsent.SessionID != replaySessionID {
		t.Fatalf("unsent record = %+v %v", unsent, err)
	}
	// 拒绝不占限频：改为 unrestricted 后可立即重放。
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1/policy", `{"scope":"unrestricted","approval":"high_risk"}`); status != 200 {
		t.Fatalf("policy = %d %s", status, raw)
	}
	if status, _, raw := f.replay(outKey, `{"step":1}`); status != 200 || outsider.count() != 1 {
		t.Fatalf("unrestricted replay = %d %s", status, raw)
	}
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1/policy", `{"scope":"origins","approval":"high_risk"}`); status != 200 {
		t.Fatalf("policy = %d %s", status, raw)
	}

	// 危险方法在 approval=high_risk 下 → 409 review_required，不发送、不留记录、不审计。
	delKey, _ := f.replayVuln("删除接口越权", replayStep{flow: replayOriginalFlow("DELETE", target.URL+"/items/7", target.host(), "", 200, `{"ok":true}`)})
	before := f.requestCount()
	status, value, raw = f.replay(delKey, `{"step":1}`)
	if status != 409 || value["status"] != "review_required" || !strings.Contains(fmt.Sprint(value["reason"]), "DELETE") || target.count() != 0 || f.requestCount() != before {
		t.Fatalf("review = %d %s", status, raw)
	}
	if audits := replayAudits(t, f); len(audits) != 1 {
		t.Fatalf("only the actual send is audited, got %d", len(audits))
	}

	// 资产归档 → 409，不发送。
	okKey, _ := f.replayVuln("订单接口越权读取", replayStep{flow: replayOriginalFlow("GET", target.URL+"/orders/1002", target.host(), "", 200, `{"owner":"user-42"}`)})
	raw2, _ := json.Marshal(map[string]any{"name": "演示订单系统", "known_info": "演示订单 API", "objective": "检查越权", "origins": []string{target.URL}, "archived": true})
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1", string(raw2)); status != 200 {
		t.Fatalf("archive = %d %s", status, raw)
	}
	if status, value, raw := f.replay(okKey, `{"step":1}`); status != 409 || value["status"] != "archived" || target.count() != 0 {
		t.Fatalf("archived = %d %s", status, raw)
	}
}

func (f *notifyFixture) requestCount() int {
	var n int
	if err := f.capability.store.db().QueryRowContext(f.t.Context(), `SELECT COUNT(*) FROM webapi_requests`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestVulnCardReplayConcurrentAndUnknown(t *testing.T) {
	target := newReplayTarget(t)
	f := replayFixture(t, target.URL)
	key, _ := f.replayVuln("订单接口越权读取",
		replayStep{flow: replayOriginalFlow("GET", target.URL+"/orders/1002", target.host(), "", 200, `{"owner":"user-42"}`), responseMarks: []string{"user-42"}},
		replayStep{flow: replayOriginalFlow("GET", target.URL+"/orders/1003", target.host(), "", 200, `{"owner":"user-43"}`)})

	// 进行中：同 {key, step} 再点 → 409 replay_in_progress（先于限频判断），目标只收到一次。
	release := make(chan struct{})
	target.mu.Lock()
	target.block = release
	target.mu.Unlock()
	done := make(chan int, 1)
	go func() {
		status, _, _ := f.replay(key, `{"step":1}`)
		done <- status
	}()
	<-target.received
	if status, value, raw := f.replay(key, `{"step":1}`); status != 409 || value["status"] != "replay_in_progress" {
		t.Fatalf("concurrent = %d %s", status, raw)
	}
	// 另一步不是同一个进行中槽位，但同条目受限频。
	if status, _, raw := f.replay(key, `{"step":2}`); status != 429 {
		t.Fatalf("other step during flight = %d %s", status, raw)
	}
	target.mu.Lock()
	target.block = nil
	target.mu.Unlock()
	close(release)
	if status := <-done; status != 200 || target.count() != 1 {
		t.Fatalf("first replay = %d hits %d", status, target.count())
	}

	// 目标断开 → 502 unknown，记 unknown 证据，审计一次，不自动重试。
	f.advance(replayRateWindow)
	target.mu.Lock()
	target.hangup = true
	target.mu.Unlock()
	status, value, raw := f.replay(key, `{"step":2}`)
	if status != 502 || value["status"] != "unknown" || value["evidence_id"] == "" || target.count() != 2 {
		t.Fatalf("hangup = %d %s hits %d", status, raw, target.count())
	}
	model, err := f.capability.store.evidenceByID(t.Context(), "t1", value["evidence_id"].(string))
	if err != nil || model == nil || model.Status != requestUnknown || model.Sent.Valid {
		t.Fatalf("unknown evidence = %+v %v", model, err)
	}
	audits := replayAudits(t, f)
	if len(audits) != 2 || !strings.Contains(audits[1].DetailJSON, `"outcome":"unknown"`) || !strings.Contains(audits[1].DetailJSON, `"status_code":null`) {
		t.Fatalf("unknown audit = %+v", audits)
	}
	// 同一请求已有未解决 unknown：再点不发送（409 unresolved），沿用 webapi_http 的“不自动重发”保护。
	f.advance(replayRateWindow)
	target.mu.Lock()
	target.hangup = false
	target.mu.Unlock()
	if status, value, raw := f.replay(key, `{"step":2}`); status != 409 || value["status"] != "unresolved" || target.count() != 2 {
		t.Fatalf("after unknown = %d %s", status, raw)
	}
	if card := f.card(key); len(card.Replays) != 1 {
		t.Fatalf("unknown must not appear in replays: %+v", card.Replays)
	}

	// 目标超时（缩短共用客户端超时）→ 502 unknown，不重试。
	saved := directClient.Timeout
	directClient.Timeout = 200 * time.Millisecond
	t.Cleanup(func() { directClient.Timeout = saved })
	slow := make(chan struct{})
	t.Cleanup(func() { close(slow) })
	target.mu.Lock()
	target.block = slow
	target.mu.Unlock()
	f.advance(replayRateWindow)
	if status, value, raw := f.replay(key, `{"step":1}`); status != 502 || value["status"] != "unknown" || target.count() != 3 {
		t.Fatalf("timeout = %d %s", status, raw)
	}
}

func TestVulnCardReplayValidation(t *testing.T) {
	target := newReplayTarget(t)
	f := replayFixture(t, target.URL)
	key, _ := f.replayVuln("订单接口越权读取", replayStep{flow: replayOriginalFlow("GET", target.URL+"/orders/1002", target.host(), "", 200, `{"owner":"user-42"}`)})
	for body, want := range map[string]int{`{"step":0}`: 400, `{"step":4}`: 400, `{"step":2}`: 400, `{"step":"1"}`: 400, `{"step":1,"x":1}`: 400, `not json`: 400} {
		if status, value, raw := f.replay(key, body); status != want || value["error"] == nil {
			t.Fatalf("replay %s = %d %s", body, status, raw)
		}
	}
	if status, _, raw := f.do("POST", "/api/webapi/vulns/a%2Fb/card/replay", `{}`); status != 400 {
		t.Fatalf("bad key = %d %s", status, raw)
	}
	if status, _, raw := f.replay("no-such", `{}`); status != 404 {
		t.Fatalf("unknown key = %d %s", status, raw)
	}
	other, otherRev := f.addFinding("t1", "调试接口", notifyContent(), evidenceAt("GET", target.URL+"/debug"))
	f.legacyVerify("t1", other, otherRev)
	if status, _, raw := f.replay(vulnOf(f.vulns("a1", ""), other).ID, `{}`); status != 404 || !strings.Contains(raw, "尚无最小复现") {
		t.Fatalf("no repro = %d %s", status, raw)
	}
	if target.count() != 0 || len(replayAudits(t, f)) != 0 {
		t.Fatal("validation errors must not send or audit")
	}
}

// Host 与 URL authority 不一致的原件不重放（URL 是唯一 Host 来源）；伪头与 Content-Length 跳过。
func TestReplayHTTPRequestRebuild(t *testing.T) {
	flow := replayOriginalFlow("POST", "https://api.example.test/orders/1002", "api.example.test", `{"a":1}`, 200, "")
	req, review, err := replayHTTPRequest(flow)
	if err != nil || req.Host != "api.example.test" || req.Header.Get("Content-Length") != "" || req.Header.Get(":authority") != "" ||
		review["X-Multi"] != "first, second" || req.Header.Get("User-Agent") != "fixture-agent/1" || req.Header.Get("Accept-Encoding") != "identity" {
		t.Fatalf("rebuild = %+v %v %v", req, review, err)
	}
	flow.Request.Headers[1].Value = "other.example.test"
	if _, _, err := replayHTTPRequest(flow); err == nil {
		t.Fatal("mismatched Host accepted")
	}
	if got := compareReplay(200, 200, nil, nil); got.VerdictHint != hintUnknown {
		t.Fatalf("no marks same status = %+v", got)
	}
	if got := compareReplay(200, 500, nil, nil); got.VerdictHint != hintChanged || got.SameStatus {
		t.Fatalf("no marks status changed = %+v", got)
	}
	if got := compareReplay(200, 500, []string{"a"}, []string{"a"}); got.VerdictHint != hintChanged || !got.ResponseMarksStillPresent {
		t.Fatalf("marks present status changed = %+v", got)
	}
}
