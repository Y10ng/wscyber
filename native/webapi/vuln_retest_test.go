package webapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// V3 漏洞卡“申请复测”后端测试：临时库合成数据（example.test 演示资产、虚构候选），launcher 替身，不调用模型。

func (f *notifyFixture) retestRequest(key, body string) (int, map[string]any, string) {
	f.t.Helper()
	return f.do("POST", "/api/webapi/vulns/"+key+"/card/retest", body)
}

// addRevision 给候选追加一条新修订并设为当前修订（合成数据）。
func (f *notifyFixture) addRevision(taskID, findingID, title string) string {
	f.t.Helper()
	ctx := f.t.Context()
	var finding findingModel
	if err := f.capability.store.orm.NewSelect().Model(&finding).Where("id = ?", findingID).Scan(ctx); err != nil {
		f.t.Fatal(err)
	}
	content := notifyContent()
	content.Title = title
	payload, _ := json.Marshal(storedFindingPayload{Canonical: "{}", Content: content, Evidence: evidenceAt("GET", "https://api.example.test/orders/1")})
	revisionID := newFindingRevisionID()
	if _, err := f.capability.store.orm.NewInsert().Model(&findingRevisionModel{ID: revisionID, FindingID: findingID, TaskID: taskID, Number: finding.Number, Revision: finding.CurrentRevision + 1,
		SessionID: "s-research", TurnID: "turn", CallID: "call-rev2-" + findingID, PayloadJSON: string(payload), Title: title, CreatedAt: nowRFC3339()}).Exec(ctx); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.capability.store.db.ExecContext(ctx, `UPDATE webapi_findings SET current_revision = current_revision + 1, current_revision_id = ? WHERE id = ?`, revisionID, findingID); err != nil {
		f.t.Fatal(err)
	}
	return revisionID
}

func (f *notifyFixture) setRunning(sessionID string, running bool) {
	f.launcher.mu.Lock()
	defer f.launcher.mu.Unlock()
	f.launcher.running[sessionID] = running
}

func retestRequestAudits(t *testing.T, f *notifyFixture) []auditEventModel {
	return auditByAction(auditRows(t, f.capability), "vuln.retest_request")
}

func lifecycleKinds(card VulnCard, kind string) []VulnCardEvent {
	out := []VulnCardEvent{}
	for _, event := range card.Lifecycle {
		if event.Kind == kind {
			out = append(out, event)
		}
	}
	return out
}

func retestRequestFixture(t *testing.T) *notifyFixture {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"管理员","username":"admin@example.test","secret":"`+assetTestSecret+`"}`); status != 200 && status != 201 {
		t.Fatalf("credential = %d %s", status, raw)
	}
	f.launchManual("a1", "t1")
	return f
}

func TestVulnCardRetestRequestLaunch(t *testing.T) {
	f := retestRequestFixture(t)
	order := "https://api.example.test/orders/1"
	// F1 最早，supported 带最小复现（repro 来源）；F2 较新，旧 supported（无 key_steps）；F3 最新，无验证。
	f1, r1 := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", order))
	f.verify("t1", f1, r1, "supported", notifyFlow("GET", order, "", `{"owner":"user-42"}`))
	time.Sleep(2 * time.Millisecond)
	f2, r2 := f.addFinding("t1", "订单越权（复核）", notifyContent(), evidenceAt("GET", order))
	f.legacyVerify("t1", f2, r2)
	time.Sleep(2 * time.Millisecond)
	f3, _ := f.addFinding("t1", "订单越权（再次发现）", notifyContent(), evidenceAt("GET", order))
	rev2 := f.addRevision("t1", f1, "订单接口越权读取（修订）")
	list := f.vulns("a1", "")
	key := vulnOf(list, f1).ID
	if vulnOf(list, f2).ID != key || vulnOf(list, f3).ID != key {
		t.Fatalf("members not grouped: %+v", list.Vulns)
	}
	if card := f.card(key); card.RetestRequest != nil || len(lifecycleKinds(card, "retest_request")) != 0 {
		t.Fatalf("card before request = %+v", card.RetestRequest)
	}

	status, value, raw := f.retestRequest(key, "")
	if status != 201 || value["finding_id"] != f1 || value["revision_id"] != rev2 || value["task_id"] != "t1" {
		t.Fatalf("request = %d %s", status, raw)
	}
	retestID, sessionID := value["retest_id"].(string), value["session_id"].(string)
	if len(retestID) != 36 || retestID[14] != '4' || sessionID == "" {
		t.Fatalf("ids = %q %q", retestID, sessionID)
	}
	model, err := f.capability.store.retestForSession(t.Context(), sessionID)
	if err != nil || model == nil || model.ID != retestID || model.FindingID != f1 || model.SourceRevisionID != rev2 || model.TaskID != "t1" {
		t.Fatalf("retest record = %+v %v", model, err)
	}
	runs := f.runsTo(sessionID)
	if len(runs) != 1 || runs[0].request.MaxTurns != 0 || len(runs[0].request.Extensions) != 0 {
		t.Fatalf("runs = %+v", runs)
	}
	task, _ := f.capability.store.Get(t.Context(), "t1")
	source, _ := f.capability.store.findingRevision(t.Context(), "t1", f1, rev2)
	base := pinnedFirstMessage(retestMessageVariant, task.Origins, source)
	text := runText(runs[0])
	if !strings.HasPrefix(text, base+"\n\n测试账号（资产登记，仅用于上述授权范围内的测试）：\n") || !strings.HasPrefix(text, "复测：按固定修订复测以下候选是否仍可复现。") ||
		!strings.Contains(text, "候选标题：订单接口越权读取（修订）") || !strings.Contains(text, "{{cred:c1.secret}}") || strings.Contains(text, assetTestSecret) {
		t.Fatalf("first message:\n%s", text)
	}
	audits := retestRequestAudits(t, f)
	if len(audits) != 1 || audits[0].Actor != auditActorUser || audits[0].TargetKind != "vuln" || audits[0].TargetID != key || audits[0].TaskID != "t1" || audits[0].AssetID != "a1" {
		t.Fatalf("audits = %+v", audits)
	}
	var detail map[string]any
	_ = json.Unmarshal([]byte(audits[0].DetailJSON), &detail)
	if detail["vuln_key"] != key || detail["retest_id"] != retestID || detail["session_id"] != sessionID || detail["task_id"] != "t1" || detail["finding_id"] != f1 || len(detail) != 5 {
		t.Fatalf("audit detail = %v", detail)
	}
	if starts := auditByAction(auditRows(t, f.capability), "retest.start"); len(starts) != 1 || starts[0].Actor != auditActorUser || starts[0].TargetID != retestID {
		t.Fatalf("retest.start = %+v", starts)
	}

	// 会话不在运行（替身默认）→ done；运行中 → running，并挡住再次申请。
	card := f.card(key)
	if card.RetestRequest == nil || card.RetestRequest.Status != "done" || card.RetestRequest.RetestID != retestID || card.RetestRequest.SessionID != sessionID ||
		card.RetestRequest.TaskID != "t1" || card.RetestRequest.FindingID != f1 || card.RetestRequest.StartedAt != audits[0].At {
		t.Fatalf("retest_request = %+v", card.RetestRequest)
	}
	f.setRunning(sessionID, true)
	card = f.card(key)
	events := lifecycleKinds(card, "retest_request")
	if card.RetestRequest.Status != "running" || len(events) != 1 || events[0].Title != "申请复测" || events[0].Actor != auditActorUser || events[0].At != audits[0].At {
		t.Fatalf("running card = %+v %+v", card.RetestRequest, events)
	}
	if len(card.Appendix.Changes) == 0 || card.Appendix.Changes[0].Action != "vuln.retest_request" {
		t.Fatalf("changes = %+v", card.Appendix.Changes)
	}
	status, value, raw = f.retestRequest(key, "{}")
	if status != 409 || value["code"] != "in_flight" || value["retest_id"] != retestID || value["session_id"] != sessionID {
		t.Fatalf("in_flight = %d %s", status, raw)
	}
	if len(retestRequestAudits(t, f)) != 1 || len(f.runsTo(sessionID)) != 1 {
		t.Fatal("rejected request must not audit or run")
	}

	// 复测出结论（会话仍标运行中）→ in_flight 解除，卡片 done，结论走 retest 段与原有事件。
	time.Sleep(2 * time.Millisecond)
	f.addRetestResult(retestID, "fixed_in_scope")
	card = f.card(key)
	if card.RetestRequest.Status != "done" || card.Retest == nil || card.Retest.RetestID != retestID || card.Retest.Conclusion != "fixed_in_scope" || card.Header.Status != VulnFixed {
		t.Fatalf("after conclusion = %+v %+v %s", card.RetestRequest, card.Retest, card.Header.Status)
	}
	if len(lifecycleKinds(card, "retest")) != 1 || len(lifecycleKinds(card, "fixed")) != 1 {
		t.Fatalf("lifecycle = %+v", card.Lifecycle)
	}
	status, value, raw = f.retestRequest(key, "")
	if status != 201 || value["retest_id"] == retestID {
		t.Fatalf("second request = %d %s", status, raw)
	}
	card = f.card(key)
	if card.RetestRequest.RetestID != value["retest_id"] || len(lifecycleKinds(card, "retest_request")) != 2 || len(retestRequestAudits(t, f)) != 2 {
		t.Fatalf("second card = %+v", card.RetestRequest)
	}
}

func TestVulnCardRetestRequestSelection(t *testing.T) {
	f := retestRequestFixture(t)
	// 无 repro：取最近一次 supported 验证所属候选（F1 旧 supported），而不是最新成员 F2。
	url := "https://api.example.test/coupons/1"
	f1, r1 := f.addFinding("t1", "优惠券越权", notifyContent(), evidenceAt("GET", url))
	f.legacyVerify("t1", f1, r1)
	time.Sleep(2 * time.Millisecond)
	f2, r2 := f.addFinding("t1", "优惠券越权（复核）", notifyContent(), evidenceAt("GET", url))
	f.verify("t1", f2, r2, "inconclusive", notifyFlow("GET", url, "", `{}`))
	key := vulnOf(f.vulns("a1", ""), f1).ID
	if card := f.card(key); card.Repro != nil {
		t.Fatal("fixture must have no repro")
	}
	if status, value, raw := f.retestRequest(key, ""); status != 201 || value["finding_id"] != f1 || value["revision_id"] != r1 {
		t.Fatalf("supported branch = %d %s", status, raw)
	}
	// 无任何 supported：取最近成员候选。
	url = "https://api.example.test/debug"
	g1, s1 := f.addFinding("t1", "调试接口暴露", notifyContent(), evidenceAt("GET", url))
	f.verify("t1", g1, s1, "counterevidence", notifyFlow("GET", url, "", `{}`))
	time.Sleep(2 * time.Millisecond)
	g2, s2 := f.addFinding("t1", "调试接口暴露（复核）", notifyContent(), evidenceAt("GET", url))
	key = vulnOf(f.vulns("a1", ""), g1).ID
	if status, value, raw := f.retestRequest(key, ""); status != 201 || value["finding_id"] != g2 || value["revision_id"] != s2 {
		t.Fatalf("latest member branch = %d %s", status, raw)
	}
}

func TestVulnCardRetestRequestConflicts(t *testing.T) {
	f := retestRequestFixture(t)
	url := "https://api.example.test/orders/7"
	fid, rev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", url))
	f.verify("t1", fid, rev, "supported", notifyFlow("GET", url, "", `{"owner":"user-42"}`))
	key := vulnOf(f.vulns("a1", ""), fid).ID

	// 校验：未知字段/非对象 400，键非法 400，不存在 404；均不建记录、不审计。
	for _, body := range []string{`{"x":1}`, `not json`, `[]`} {
		if status, _, raw := f.retestRequest(key, body); status != 400 {
			t.Fatalf("body %s = %d %s", body, status, raw)
		}
	}
	if status, _, raw := f.do("POST", "/api/webapi/vulns/a%2Fb/card/retest", `{}`); status != 400 {
		t.Fatalf("bad key = %d %s", status, raw)
	}
	if status, _, raw := f.retestRequest("no-such", `{}`); status != 404 {
		t.Fatalf("unknown key = %d %s", status, raw)
	}

	// startSession 失败（无可用节点）→ 409 no_model，error 为 stage：原文；复测记录保留，只登记 retest.start。
	agents := f.launcher.agents
	f.launcher.agents = nil
	status, value, raw := f.retestRequest(key, "")
	if status != 409 || value["code"] != "no_model" || value["stage"] != "select_node" || !strings.HasPrefix(value["error"].(string), "select_node：当前没有已提供 webapi_http 工具的在线节点，复测已保存但未启动") {
		t.Fatalf("no_model = %d %s", status, raw)
	}
	failedID := value["retest_id"].(string)
	if model, _ := f.capability.store.retestForSession(t.Context(), value["session_id"].(string)); model == nil || model.ID != failedID {
		t.Fatalf("failed retest record = %+v", model)
	}
	if len(retestRequestAudits(t, f)) != 0 || len(auditByAction(auditRows(t, f.capability), "retest.start")) != 1 {
		t.Fatal("failed launch: vuln.retest_request must not be audited; retest.start once")
	}
	// 未注入会话服务：同样 no_model（open_session 阶段）。
	launcher := f.capability.launcher
	f.capability.launcher = nil
	if status, value, raw := f.retestRequest(key, ""); status != 409 || value["code"] != "no_model" || value["stage"] != "open_session" {
		t.Fatalf("no launcher = %d %s", status, raw)
	}
	f.capability.launcher = launcher
	f.launcher.agents = agents
	// 失败留下的记录不算进行中：恢复节点后新建复测成功。
	status, value, raw = f.retestRequest(key, "")
	if status != 201 || value["retest_id"] == failedID {
		t.Fatalf("retry = %d %s", status, raw)
	}
	sessionID := value["session_id"].(string)

	// 顺序：in_flight 先于 no_model。
	f.setRunning(sessionID, true)
	f.launcher.agents = nil
	if status, value, raw := f.retestRequest(key, ""); status != 409 || value["code"] != "in_flight" {
		t.Fatalf("in_flight before no_model = %d %s", status, raw)
	}
	f.launcher.agents = agents
	// false_positive 先于 in_flight。
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+key+"/triage", `{"disposition":"false_positive"}`); status != 200 {
		t.Fatalf("triage = %d %s", status, raw)
	}
	if status, value, raw := f.retestRequest(key, ""); status != 409 || value["code"] != "false_positive" {
		t.Fatalf("false_positive = %d %s", status, raw)
	}
	// 接受风险照常可申请（只挡误报）——in_flight 仍生效。
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+key+"/triage", `{"disposition":"risk_accepted"}`); status != 200 {
		t.Fatalf("triage = %d %s", status, raw)
	}
	if status, value, raw := f.retestRequest(key, ""); status != 409 || value["code"] != "in_flight" {
		t.Fatalf("risk_accepted in_flight = %d %s", status, raw)
	}

	// no_member：条目视图总是来自“有当前修订且任务存在”的成员（删任务/去掉当前修订后条目整体 404），
	// 故 HTTP 上只能在并发删除时出现；这里直接校验选用规则对无成员条目返回空。
	// 条目没有成员时选用规则返回空（no_member）。
	if got := retestRequestFinding(&GlobalVuln{}, nil); got != "" {
		t.Fatalf("empty members = %q", got)
	}

	// archived 最先：同时误报 + 进行中。
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+key+"/triage", `{"disposition":"false_positive"}`); status != 200 {
		t.Fatalf("triage = %d %s", status, raw)
	}
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+`,"archived":true}`); status != 200 {
		t.Fatalf("archive = %d %s", status, raw)
	}
	if status, value, raw := f.retestRequest(key, ""); status != 409 || value["code"] != "archived" {
		t.Fatalf("archived = %d %s", status, raw)
	}
	if card := f.card(key); card.RetestRequest == nil || card.RetestRequest.SessionID != sessionID || card.RetestRequest.Status != "running" {
		t.Fatalf("archived card = %+v", card.RetestRequest)
	}
	if n := len(retestRequestAudits(t, f)); n != 1 {
		t.Fatalf("vuln.retest_request audits = %d, want 1", n)
	}
}
