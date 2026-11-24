package webapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	"github.com/uptrace/bun"
)

// K1 漏洞卡后端测试：全部为临时库合成数据（example.test 演示资产、虚构候选、合成 Flow）。

func cardFlow(rawURL, cookie, responseBody string, extraHeaders ...*traffic.Header) *traffic.Flow {
	headers := []*traffic.Header{{Name: "Host", Value: "api.example.test"}, {Name: "Cookie", Value: cookie}}
	return &traffic.Flow{
		Id: "we_card", Complete: true,
		Request: &traffic.HttpRequest{Method: "GET", Url: rawURL, Protocol: "HTTP/1.1", Headers: append(headers, extraHeaders...)},
		Response: &traffic.HttpResponse{StatusCode: 200, ReasonPhrase: "OK",
			Headers: []*traffic.Header{{Name: "Content-Type", Value: "application/json"}}, Body: []byte(responseBody)},
	}
}

func (f *notifyFixture) newVerification(taskID, findingID, revisionID string) VerificationSummary {
	f.t.Helper()
	summary, _, _, err := f.capability.store.saveVerification(f.t.Context(), "v-"+newSessionID(), taskID, findingID, revisionID)
	if err != nil {
		f.t.Fatal(err)
	}
	return *summary
}

func (f *notifyFixture) callVerification(summary VerificationSummary, payload map[string]any) (map[string]any, string) {
	f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	call := "call-" + newSessionID()
	inv := operation.Invocation{SessionID: summary.SessionID, TurnID: "turn-" + call, CallID: call}
	result, message := executeVerification(f.t, &verificationTool{store: f.capability.store}, inv, string(raw))
	time.Sleep(2 * time.Millisecond)
	return result, message
}

func reproPayload(evidenceID string) map[string]any {
	return map[string]any{
		"verdict": "supported", "method": "重放订单请求", "summary": "他人订单可读", "preconditions": "普通用户已登录",
		"evidence":  []map[string]any{{"evidence_id": evidenceID, "relation": "supporting"}},
		"impact":    "普通用户可读取他人订单",
		"key_steps": []map[string]any{{"title": "读取他人订单", "evidence_id": evidenceID}},
	}
}

// legacyVerify 走真实工具保存 supported，再去掉最小复现字段，模拟 K1 之前保存的旧结果。
func (f *notifyFixture) legacyVerify(taskID, findingID, revisionID string) VerificationSummary {
	f.t.Helper()
	summary := f.verify(taskID, findingID, revisionID, "supported", notifyFlow("GET", "https://api.example.test/legacy", "", `{"ok":true}`))
	if _, err := f.capability.store.db().ExecContext(f.t.Context(), `UPDATE webapi_verification_results SET payload_json = (payload_json::jsonb #- '{payload,impact}' #- '{payload,key_steps}')::text WHERE verification_id = ?`, summary.VerificationID); err != nil {
		f.t.Fatal(err)
	}
	return summary
}

func (f *notifyFixture) cardRetest(taskID, findingID, revisionID, conclusion string) {
	f.t.Helper()
	ctx := f.t.Context()
	id, session := "r-"+newSessionID(), newSessionID()
	if err := f.capability.store.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&retestModel{ID: id, TaskID: taskID, FindingID: findingID, SourceRevisionID: revisionID, SessionID: session, CreatedAt: nowRFC3339()}).Exec(ctx)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	payload, _ := json.Marshal(storedRetestResult{Canonical: conclusion, Payload: retestToolPayload{Conclusion: conclusion, Method: "演示", Summary: "复测 " + conclusion}})
	if err := f.capability.store.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&retestResultModel{RetestID: id, SessionID: session, TurnID: "t", CallID: "c-" + id, PayloadJSON: string(payload), CreatedAt: nowRFC3339()}).Exec(ctx)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
}

func (f *notifyFixture) card(key string) VulnCard {
	f.t.Helper()
	status, _, raw := f.do("GET", "/api/webapi/vulns/"+key+"/card", "")
	if status != 200 {
		f.t.Fatalf("card %s = %d %s", key, status, raw)
	}
	var card VulnCard
	if err := json.Unmarshal([]byte(raw), &card); err != nil {
		f.t.Fatal(err)
	}
	return card
}

func TestVerificationReproValidation(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	v := f.newVerification("t1", fid, frev)
	e1 := f.addEvidence("t1", v.SessionID, cardFlow("https://api.example.test/orders/1002", "sid=a", `{"owner":"user-42"}`))
	e2 := f.addEvidence("t1", v.SessionID, cardFlow("https://api.example.test/orders/1001", "sid=a", `{"owner":"demo-user"}`))
	other := f.addEvidence("t1", "s-research", cardFlow("https://api.example.test/orders/1003", "sid=a", `{}`))
	truncated := f.addEvidence("t1", v.SessionID, cardFlow("https://api.example.test/orders/1004", "sid=a", `{}`))
	if _, err := f.capability.store.db().ExecContext(t.Context(), `UPDATE webapi_requests SET capture_truncated = true WHERE id = ?`, truncated.ID); err != nil {
		t.Fatal(err)
	}
	marks := func(n int) []string {
		out := []string{}
		for i := 0; i < n; i++ {
			out = append(out, "片段")
		}
		return out
	}
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"缺 impact", func(p map[string]any) { delete(p, "impact") }, "验证参数 impact 必填为空"},
		{"impact 空白", func(p map[string]any) { p["impact"] = "  " }, "验证参数 impact 必填为空"},
		{"缺 key_steps", func(p map[string]any) { delete(p, "key_steps") }, "验证参数 key_steps 必填为空"},
		{"key_steps 空数组", func(p map[string]any) { p["key_steps"] = []any{} }, "验证参数 key_steps 必填为空"},
		{"步骤缺证据", func(p map[string]any) { p["key_steps"] = []map[string]any{{"title": "t"}} }, "key_steps[0].evidence_id 必填为空"},
		{"步骤证据重复", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": e1.ID}, {"title": "b", "evidence_id": e1.ID}}
		}, "key_steps[1].evidence_id 与 key_steps[0] 重复"},
		{"marks 过多", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": e1.ID, "request_marks": marks(6)}}
		}, "key_steps[0].request_marks 数量过多：实际 6，上限 5"},
		{"mark 多行", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": e1.ID, "response_marks": []string{"a\nb"}}}
		}, "key_steps[0].response_marks[0] 必须是单行文本片段"},
		{"mark 超长", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": e1.ID, "response_marks": []string{strings.Repeat("标", 101)}}}
		}, "key_steps[0].response_marks[0] 超长：实际 101 字，上限 100 字"},
		{"mark 空", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": e1.ID, "response_marks": []string{""}}}
		}, "key_steps[0].response_marks[0] 必填为空"},
		{"对照相同", func(p map[string]any) {
			p["contrast"] = map[string]any{"normal_evidence_id": e1.ID, "abnormal_evidence_id": e1.ID}
		}, "contrast.normal_evidence_id 与 contrast.abnormal_evidence_id 不能相同"},
		{"建议等级非法", func(p map[string]any) { p["suggested_severity"] = "urgent"; p["severity_reason"] = "x" }, "suggested_severity 仅支持critical/high/medium/low/info"},
		{"理由无等级", func(p map[string]any) { p["severity_reason"] = "x" }, "severity_reason 须与 suggested_severity 一起填写"},
		{"等级无理由", func(p map[string]any) { p["suggested_severity"] = "high" }, "验证参数 severity_reason 必填为空"},
		{"步骤证据跨会话", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": other.ID}}
		}, "key_steps[0].evidence_id 只接受本验证session在本任务产生的新HTTP证据"},
		{"步骤证据不存在", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": "we_missing"}}
		}, "key_steps[0].evidence_id 只接受本验证session"},
		{"步骤证据截断", func(p map[string]any) {
			p["key_steps"] = []map[string]any{{"title": "a", "evidence_id": truncated.ID}}
		}, "key_steps[0].evidence_id 必须是已发送、收到响应且完整捕获的新HTTP原件"},
		{"对照证据跨会话", func(p map[string]any) {
			p["contrast"] = map[string]any{"normal_evidence_id": other.ID, "abnormal_evidence_id": e1.ID}
		}, "contrast.normal_evidence_id 只接受本验证session"},
	}
	for _, tc := range cases {
		payload := reproPayload(e1.ID)
		tc.mutate(payload)
		if result, message := f.callVerification(v, payload); result != nil || !strings.Contains(message, tc.want) {
			t.Fatalf("%s: result=%v message=%q want %q", tc.name, result, message, tc.want)
		}
	}
	if len(f.notifications()) != 0 {
		t.Fatal("rejected results must not register notifications")
	}
	// 完整最小复现保存成功；结果视图带全部字段，旧字段语义不变。
	payload := reproPayload(e1.ID)
	payload["key_steps"] = []map[string]any{
		{"title": "以 demo-user 读取他人订单", "evidence_id": e1.ID, "request_marks": []string{"/orders/1002"}, "response_marks": []string{`"owner":"user-42"`}},
		{"title": "对照：读取本人订单", "evidence_id": e2.ID},
	}
	payload["contrast"] = map[string]any{"normal_evidence_id": e2.ID, "abnormal_evidence_id": e1.ID, "note": "仅订单号不同，均返回 200"}
	payload["remediation"] = []string{"按会话用户校验订单归属"}
	payload["retest_method"] = "重放第 1 步，期望 403"
	payload["suggested_severity"] = "high"
	payload["severity_reason"] = "可批量读取他人收货信息"
	result, message := f.callVerification(v, payload)
	if result == nil || result["verdict"] != "supported" {
		t.Fatalf("full repro rejected: %s", message)
	}
	view, err := f.capability.store.verificationResult(t.Context(), v.VerificationID)
	if err != nil || view == nil {
		t.Fatalf("result view = %v %v", view, err)
	}
	p := view.Payload
	if p.Impact != "普通用户可读取他人订单" || len(p.KeySteps) != 2 || p.KeySteps[0].RequestMarks[0] != "/orders/1002" || p.Contrast == nil || p.Contrast.NormalEvidenceID != e2.ID ||
		len(p.Remediation) != 1 || p.RetestMethod != "重放第 1 步，期望 403" || p.SuggestedSeverity != "high" || p.SeverityReason == "" || !p.hasRepro() {
		t.Fatalf("stored payload = %+v", p)
	}
	// AI 建议等级不写处置。
	if record, err := f.capability.store.triageRecords(t.Context(), ""); err != nil || len(record) != 0 {
		t.Fatalf("suggested severity must not write triage: %v %v", record, err)
	}

	// 旧结果（无最小复现字段）照常读取，规范载荷不含新字段。
	legacy := f.legacyVerify("t1", fid, frev)
	old, err := f.capability.store.verificationResult(t.Context(), legacy.VerificationID)
	if err != nil || old == nil || old.Payload.Verdict != "supported" || old.Payload.hasRepro() || old.Payload.Impact != "" {
		t.Fatalf("legacy result = %+v %v", old, err)
	}
	raw, _ := json.Marshal(old.Payload)
	if strings.Contains(string(raw), "impact") || strings.Contains(string(raw), "key_steps") || strings.Contains(string(raw), "suggested_severity") {
		t.Fatalf("legacy payload json = %s", raw)
	}
	canonical, _ := json.Marshal(verificationToolPayload{Verdict: "inconclusive", Method: "m", Summary: "s"})
	if string(canonical) != `{"verdict":"inconclusive","method":"m","summary":"s"}` {
		t.Fatalf("canonical without repro = %s", canonical)
	}
	// 放宽：非 supported 时多填的最小复现/建议等级字段（含空数组）被忽略：保存成功，且不写入结果。
	for verdict, relation := range map[string]string{"inconclusive": "limitation", "counterevidence": "counterevidence"} {
		other := f.newVerification("t1", fid, frev)
		evidence := f.addEvidence("t1", other.SessionID, cardFlow("https://api.example.test/orders/1002", "sid=a", `{"owner":"demo-user"}`))
		payload := map[string]any{"verdict": verdict, "method": "m", "summary": "s", "evidence": []map[string]any{{"evidence_id": evidence.ID, "relation": relation}},
			"impact": "x", "key_steps": []map[string]any{{"title": "a", "evidence_id": evidence.ID}}, "remediation": []string{}, "retest_method": "r",
			"contrast": map[string]any{"normal_evidence_id": evidence.ID, "abnormal_evidence_id": evidence.ID}, "suggested_severity": "low", "severity_reason": "r"}
		if result, message := f.callVerification(other, payload); result == nil || result["verdict"] != verdict {
			t.Fatalf("%s with repro fields rejected: %q", verdict, message)
		}
		stored, err := f.capability.store.verificationResult(t.Context(), other.VerificationID)
		if err != nil || stored == nil {
			t.Fatalf("%s result = %v %v", verdict, stored, err)
		}
		if p := stored.Payload; p.hasRepro() || p.Impact != "" || p.KeySteps != nil || p.Contrast != nil || p.Remediation != nil || p.RetestMethod != "" || p.SuggestedSeverity != "" || p.SeverityReason != "" {
			t.Fatalf("%s stored ignored repro fields: %+v", verdict, p)
		}
		raw, _ := json.Marshal(stored.Payload)
		if strings.Contains(string(raw), "impact") || strings.Contains(string(raw), "key_steps") || strings.Contains(string(raw), "suggested_severity") {
			t.Fatalf("%s payload json = %s", verdict, raw)
		}
	}
}

// 首条验证消息：Go 版与前端 webApiVerificationFirstMessage 的固定文本逐行一致（读前端源码做字符串比对）；复测同理。
func TestVerificationFirstMessageMatchesFrontend(t *testing.T) {
	hook, err := os.ReadFile("../ui/source-overlay/src/hooks/useChatSession.ts")
	if err != nil {
		t.Fatal(err)
	}
	retest, err := os.ReadFile("../ui/source-overlay/src/lib/retest-message.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		source  []byte
		variant pinnedMessageVariant
	}{{hook, verificationMessageVariant}, {retest, retestMessageVariant}} {
		for _, line := range []string{check.variant.head, check.variant.oldEvidence, check.variant.requirement} {
			if !bytes.Contains(check.source, []byte("'"+line+"'")) {
				t.Fatalf("frontend first message lacks line %q", line)
			}
		}
	}
	if !strings.Contains(verificationMessageVariant.requirement, "verdict=supported 时须给出最小复现与建议等级") || !strings.Contains(verificationMessageVariant.requirement, "只留能证明问题的关键请求") ||
		!strings.Contains(verificationMessageVariant.requirement, "越权类给 contrast 正常对照") {
		t.Fatalf("requirement = %s", verificationMessageVariant.requirement)
	}
	definition := (&verificationTool{}).Definition()
	if !strings.Contains(definition.Description, "最小复现") || !strings.Contains(definition.Description, "建议等级") {
		t.Fatalf("tool description = %s", definition.Description)
	}
}

func TestVulnCodeAndTitleTemplate(t *testing.T) {
	vid := assetVulnID("a1", "GET https://api.example.test/orders/{id}")
	if got := vulnCode(vid); got != "V-"+strings.ToUpper(vid[:8]) || got != vulnCode(vid) {
		t.Fatalf("code = %s for %s", got, vid)
	}
	if got := vulnCode("finding:wf_0a1b2c3d4e5f"); got != "V-0A1B2C3D" {
		t.Fatalf("finding code = %s", got)
	}
	if got := vulnCode("--"); !strings.HasPrefix(got, "V-") || len(got) != 10 || got != vulnCode("--") {
		t.Fatalf("fallback code = %s", got)
	}
	for _, tc := range []struct{ asset, key, title, want string }{
		{"演示商城", "GET https://shop.example.test/api/orders/{id}", "订单越权", "演示商城 · GET /api/orders/{id} · 订单越权"},
		{"", "POST https://api.example.test", "根路径", "POST / · 根路径"},
		{"演示商城", "finding:wf_x", "无端点", "演示商城 · 无端点"},
		{"", "", "人工条目", "人工条目"},
	} {
		if got := vulnTitleTemplate(tc.asset, tc.key, tc.title); got != tc.want {
			t.Fatalf("title_template(%q,%q) = %q want %q", tc.asset, tc.key, got, tc.want)
		}
	}
}

func TestVulnCardSections(t *testing.T) {
	f := newNotifyFixture(t)
	f.setNow(time.Now().UTC().Add(time.Hour).Format(time.RFC3339)) // 审计时间晚于本用例全部结果
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	order := "https://api.example.test/orders/1002"
	f1, r1 := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", order))
	time.Sleep(2 * time.Millisecond)
	f2, r2 := f.addFinding("t1", "订单越权（复核）", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1003"))
	fixedID, fixedRev := f.addFinding("t1", "登录接口弱口令", notifyContent(), evidenceAt("POST", "https://api.example.test/login"))
	pendingID, _ := f.addFinding("t1", "调试接口暴露", notifyContent(), evidenceAt("GET", "https://api.example.test/debug"))
	fpID, fpRev := f.addFinding("t1", "导出接口疑似泄露", notifyContent(), evidenceAt("GET", "https://api.example.test/export"))
	legacyID, legacyRev := f.addFinding("t1", "旧结果候选", notifyContent(), evidenceAt("GET", "https://api.example.test/legacy"))
	time.Sleep(2 * time.Millisecond)

	// 订单条目：F1 旧 supported（无复现），F2 supported 带完整最小复现，随后推送成功、F1 复测仍可复现、人工定级。
	f.legacyVerify("t1", f1, r1)
	v2 := f.newVerification("t1", f2, r2)
	abnormal := f.addEvidence("t1", v2.SessionID, cardFlow(order+"?expand=1", "sid=fixture-session-abcdef", `{"order":"1002","owner":"user-42"}`))
	normal := f.addEvidence("t1", v2.SessionID, cardFlow("https://api.example.test/orders/1001", "sid=fixture-session-abcdef", `{"order":"1001","owner":"demo-user"}`))
	payload := reproPayload(abnormal.ID)
	payload["key_steps"] = []map[string]any{
		{"title": "以 demo-user 读取他人订单 1002", "evidence_id": abnormal.ID, "request_marks": []string{"/orders/1002", "sid=fixture"}, "response_marks": []string{`"owner":"user-42"`, "不存在的片段"}},
		{"title": "对照：读取本人订单 1001", "evidence_id": normal.ID, "response_marks": []string{`"owner":"demo-user"`}},
	}
	payload["contrast"] = map[string]any{"normal_evidence_id": normal.ID, "abnormal_evidence_id": abnormal.ID, "note": "仅订单号不同"}
	payload["remediation"] = []string{"按会话用户校验订单归属", "订单号改为不可枚举"}
	payload["retest_method"] = "重放第 1 步，期望 403"
	payload["suggested_severity"] = "high"
	payload["severity_reason"] = "可批量读取他人收货信息"
	if result, message := f.callVerification(v2, payload); result == nil {
		t.Fatalf("repro verification rejected: %s", message)
	}
	list := f.vulns("a1", "")
	orderVuln := vulnOf(list, f1)
	if orderVuln == nil || vulnOf(list, f2).ID != orderVuln.ID {
		t.Fatalf("order vuln = %+v", orderVuln)
	}
	vid := orderVuln.ID
	if orderVuln.Code != "V-"+strings.ToUpper(vid[:8]) || orderVuln.SuggestedSeverity != "high" {
		t.Fatalf("asset vuln list code/suggested = %q %q", orderVuln.Code, orderVuln.SuggestedSeverity)
	}
	sentAt := notifyTime(time.Now())
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&notificationModel{ID: "wn_card", DedupeKey: vid, Kind: NotifyKindNew, TaskID: "t1", FindingID: f2,
			VerificationID: v2.VerificationID, VulnID: vid, Status: NotifySent, CreatedAt: sentAt, SentAt: sql.NullString{String: sentAt, Valid: true}}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	f.cardRetest("t1", f1, r1, "reproducible")
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+vid+"/triage", `{"severity":"medium","disposition":"fixing"}`); status != 200 {
		t.Fatalf("triage = %d %s", status, raw)
	}

	// 已修复、待验证、误报、旧结果条目。
	f.verify("t1", fixedID, fixedRev, "supported", notifyFlow("POST", "https://api.example.test/login", `{"u":"admin"}`, `{"token":"x"}`))
	f.cardRetest("t1", fixedID, fixedRev, "fixed_in_scope")
	f.verify("t1", fpID, fpRev, "inconclusive", notifyFlow("GET", "https://api.example.test/export", "", `{}`))
	f.legacyVerify("t1", legacyID, legacyRev)
	list = f.vulns("a1", "")
	fixedKey, pendingKey, fpKey, legacyKey := vulnOf(list, fixedID).ID, vulnOf(list, pendingID).ID, vulnOf(list, fpID).ID, vulnOf(list, legacyID).ID
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+fpKey+"/triage", `{"disposition":"false_positive"}`); status != 200 {
		t.Fatalf("fp triage = %d %s", status, raw)
	}

	// 同资产另一 supported 条目：只带 AI 建议等级，不写人工定级。
	relatedURL := "https://api.example.test/summary"
	relatedID, relatedRev := f.addFinding("t1", "演示摘要访问检查", notifyContent(), evidenceAt("GET", relatedURL))
	relatedVerification := f.newVerification("t1", relatedID, relatedRev)
	relatedEvidence := f.addEvidence("t1", relatedVerification.SessionID, cardFlow(relatedURL, "sid=demo", `{"summary":"demo"}`))
	relatedPayload := map[string]any{
		"verdict": "supported", "method": "比较演示摘要响应", "summary": "演示摘要可读",
		"impact": "可读取演示摘要", "key_steps": []map[string]any{{"title": "读取演示摘要", "evidence_id": relatedEvidence.ID}},
		"evidence":           []map[string]any{{"evidence_id": relatedEvidence.ID, "relation": "supporting"}},
		"suggested_severity": "low", "severity_reason": "仅包含演示摘要",
	}
	if result, message := f.callVerification(relatedVerification, relatedPayload); result == nil {
		t.Fatalf("related verification rejected: %s", message)
	}
	relatedKey := vulnOf(f.vulns("a1", ""), relatedID).ID

	card := f.card(vid)
	h := card.Header
	if h.Key != vid || h.Code != "V-"+strings.ToUpper(vid[:8]) || h.Title != "订单越权（复核）" || h.Status != VulnNew || h.Severity != SeverityMedium ||
		h.SuggestedSeverity != "high" || h.SeverityReason != "可批量读取他人收货信息" || h.Disposition != DispositionFixing || h.DueEffective == nil ||
		h.EndpointKey != "GET https://api.example.test/orders/{id}" || h.FirstSeenAt == "" {
		t.Fatalf("header = %+v", h)
	}
	if h.TitleTemplate != "演示库存系统 · GET /orders/{id} · 订单越权（复核）" {
		t.Fatalf("title_template = %q", h.TitleTemplate)
	}
	if h.Asset == nil || h.Asset.ID != "a1" || h.Asset.Name != "演示库存系统" || h.Asset.Env != "预发" || h.Asset.Owner != "测试组" || len(h.Asset.Origins) != 2 {
		t.Fatalf("asset = %+v", h.Asset)
	}
	if card.Triage == nil || card.Triage.Severity != SeverityMedium {
		t.Fatalf("triage = %+v", card.Triage)
	}
	repro := card.Repro
	if repro == nil || card.ReproMissingReason != nil || repro.VerificationID != v2.VerificationID || repro.TaskID != "t1" || repro.SessionID != v2.SessionID ||
		repro.Impact != "普通用户可读取他人订单" || repro.Preconditions != "普通用户已登录" || len(repro.Steps) != 2 || len(repro.Remediation) != 2 ||
		repro.RetestMethod != "重放第 1 步，期望 403" || repro.SavedAt == "" {
		t.Fatalf("repro = %+v", repro)
	}
	step := repro.Steps[0]
	if step.Title != "以 demo-user 读取他人订单 1002" || step.EvidenceID != abnormal.ID || step.Method != "GET" || step.URL != order+"?expand=1" || step.StatusCode == nil || *step.StatusCode != 200 ||
		strings.Join(step.RequestMarksFound, "|") != "/orders/1002|sid=fixture" || strings.Join(step.ResponseMarksFound, "|") != `"owner":"user-42"` || !step.MarkMissing {
		t.Fatalf("step 1 = %+v", step)
	}
	if second := repro.Steps[1]; second.MarkMissing || len(second.RequestMarksFound) != 0 || strings.Join(second.ResponseMarksFound, "|") != `"owner":"demo-user"` {
		t.Fatalf("step 2 = %+v", second)
	}
	if c := repro.Contrast; c == nil || c.NormalEvidenceID != normal.ID || c.AbnormalEvidenceID != abnormal.ID || c.Note != "仅订单号不同" ||
		c.Normal == nil || c.Normal.URL != "https://api.example.test/orders/1001" || c.Abnormal == nil || c.Abnormal.Method != "GET" {
		t.Fatalf("contrast = %+v", repro.Contrast)
	}
	if card.Retest == nil || card.Retest.Conclusion != "reproducible" || card.Retest.FindingID != f1 {
		t.Fatalf("retest = %+v", card.Retest)
	}
	kinds, actors := []string{}, []string{}
	for _, event := range card.Lifecycle {
		kinds, actors = append(kinds, event.Kind), append(actors, event.Actor)
	}
	if strings.Join(kinds, ",") != "candidate,candidate,verification,verification,notify,retest,triage" || strings.Join(actors, ",") != "ai,ai,ai,ai,system,ai,user" {
		t.Fatalf("lifecycle = %+v", card.Lifecycle)
	}
	for index := 1; index < len(card.Lifecycle); index++ {
		if parseAt(card.Lifecycle[index].At).Before(parseAt(card.Lifecycle[index-1].At)) {
			t.Fatalf("lifecycle not ascending: %+v", card.Lifecycle)
		}
	}
	if card.Lifecycle[3].Title != "独立验证：支持" || card.Lifecycle[4].Title != "企业微信推送成功" || card.Lifecycle[5].Title != "复测：仍可复现" || !strings.Contains(card.Lifecycle[6].Detail, "严重度") {
		t.Fatalf("lifecycle titles = %+v", card.Lifecycle)
	}
	app := card.Appendix
	if len(app.Verifications) != 2 || app.Verifications[0].VerificationID != v2.VerificationID || !app.Verifications[0].HasRepro || app.Verifications[1].HasRepro ||
		*app.Verifications[0].Verdict != "supported" || app.Verifications[0].FindingNumber == "" || len(app.Members) != 2 {
		t.Fatalf("appendix verifications/members = %+v %+v", app.Verifications, app.Members)
	}
	// 证据：F1 旧验证证据 + F2 验证证据 + 第 2 步/对照证据（去重；合成候选快照无 evidence_id 不计）。
	if app.EvidenceCount != 3 || len(app.Evidence) != 3 || app.Evidence[2].EvidenceID != normal.ID || app.Evidence[2].Relation != "key_step" {
		t.Fatalf("appendix evidence = %d %+v", app.EvidenceCount, app.Evidence)
	}
	if len(app.Changes) != 1 || app.Changes[0].Action != "vuln.triage" {
		t.Fatalf("changes = %+v", app.Changes)
	}
	related := map[string]bool{}
	for _, item := range card.Related {
		related[item.Key] = true
		if item.Code != vulnCode(item.Key) {
			t.Fatalf("related code = %+v", item)
		}
		wantSuggested := ""
		if item.Key == relatedKey {
			wantSuggested = "low"
		}
		if item.Severity != "" || item.SuggestedSeverity != wantSuggested {
			t.Fatalf("related severity = %+v; want suggested_severity=%q and no manual severity", item, wantSuggested)
		}
	}
	if len(card.Related) != 4 || !related[relatedKey] || !related[pendingKey] || !related[fpKey] || !related[legacyKey] || related[fixedKey] || related[vid] {
		t.Fatalf("related = %+v", card.Related)
	}
	// nav：与 GET /vulns 默认排序一致。
	var global struct {
		Items []GlobalVuln `json:"items"`
	}
	_, _, raw := f.do("GET", "/api/webapi/vulns", "")
	if err := json.Unmarshal([]byte(raw), &global); err != nil {
		t.Fatal(err)
	}
	for index, item := range global.Items {
		if item.Code != vulnCode(item.ID) {
			t.Fatalf("global code = %+v", item)
		}
		if item.ID == vid && item.SuggestedSeverity != "high" {
			t.Fatalf("global suggested = %+v", item)
		}
		for _, relatedItem := range card.Related {
			if relatedItem.Key == item.ID && relatedItem.SuggestedSeverity != item.SuggestedSeverity {
				t.Fatalf("related/global suggested mismatch: %+v / %+v", relatedItem, item)
			}
		}
		got := f.card(item.ID).Nav
		wantPrev, wantNext := "", ""
		if index > 0 {
			wantPrev = global.Items[index-1].ID
		}
		if index+1 < len(global.Items) {
			wantNext = global.Items[index+1].ID
		}
		if deref(got.PrevKey) != wantPrev || deref(got.NextKey) != wantNext {
			t.Fatalf("nav of %s = %v/%v want %s/%s", item.ID, deref(got.PrevKey), deref(got.NextKey), wantPrev, wantNext)
		}
	}

	// 已修复：状态 fixed，生命周期最后为复测结论与修复事件。
	fixed := f.card(fixedKey)
	n := len(fixed.Lifecycle)
	if fixed.Header.Status != VulnFixed || n < 2 || fixed.Lifecycle[n-2].Kind != "retest" || fixed.Lifecycle[n-1].Kind != "fixed" || fixed.Lifecycle[n-1].Actor != "system" ||
		fixed.Retest == nil || fixed.Retest.Conclusion != "fixed_in_scope" || fixed.Repro == nil {
		t.Fatalf("fixed card = %+v", fixed)
	}
	// 无复现的三种原因；误报处置。
	for key, want := range map[string]string{pendingKey: reproMissingNone, fpKey: reproMissingNoSupp, legacyKey: reproMissingNoSteps} {
		got := f.card(key)
		if got.Repro != nil || got.ReproMissingReason == nil || *got.ReproMissingReason != want {
			t.Fatalf("card %s repro = %+v reason %v want %s", key, got.Repro, got.ReproMissingReason, want)
		}
	}
	if fp := f.card(fpKey); fp.Header.Disposition != DispositionFalsePositive || fp.Header.Status != VulnUnconfirmed || fp.Header.SuggestedSeverity != "" {
		t.Fatalf("fp header = %+v", fp.Header)
	}
	// code 稳定：再读一次不变。
	if again := f.card(vid); again.Header.Code != h.Code || again.Header.TitleTemplate != h.TitleTemplate {
		t.Fatal("card header changed between reads")
	}

	// 无资产：finding:<id>，asset 为 null，标题模板省资产段，related 为空。
	f.plainTask("t9")
	standalone, standaloneRev := f.addFinding("t9", "独立任务候选", notifyContent(), evidenceAt("GET", "https://api.example.test/items/7"))
	f.verify("t9", standalone, standaloneRev, "supported", notifyFlow("GET", "https://api.example.test/items/7", "", `{"id":7}`))
	key := "finding:" + standalone
	lone := f.card(key)
	if lone.Header.Asset != nil || lone.Header.Key != key || lone.Header.Code != vulnCode(key) || lone.Header.TitleTemplate != "GET /items/{id} · 独立任务候选" ||
		len(lone.Related) != 0 || lone.Repro == nil || lone.Repro.Steps[0].Title != "复现请求" {
		t.Fatalf("standalone card = %+v", lone)
	}

	// 404/400；只读：读卡不登记审计。
	for path, want := range map[string]int{"/api/webapi/vulns/no-such/card": 404, "/api/webapi/vulns/finding:wf_missing/card": 404, "/api/webapi/vulns/bad%5Ckey/card": 400} {
		if status, _, raw := f.do("GET", path, ""); status != want {
			t.Fatalf("GET %s = %d %s", path, status, raw)
		}
	}
	var audits int
	_ = f.capability.store.db().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM webapi_audit_events WHERE action NOT IN ('vuln.triage', 'credential.save', 'asset.create', 'task.create')`).Scan(&audits)
	if audits != 0 {
		t.Fatalf("card reads wrote %d audit events", audits)
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func TestVulnCardCurl(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"普通用户","username":"demo-user","secret":"Fixture-Pass-9"}`); status != 200 {
		t.Fatalf("credential = %d %s", status, raw)
	}
	f.launchManual("a1", "t1")
	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	v := f.newVerification("t1", fid, frev)
	flow := cardFlow("https://api.example.test/orders/1002?expand=1&pw=Fixture-Pass-9", "sid=fixture-session-abcdef", `{"owner":"user-42"}`,
		&traffic.Header{Name: "X-Trace", Value: "user demo-user / Fixture-Pass-9"}, &traffic.Header{Name: "Content-Length", Value: "13"},
		&traffic.Header{Name: "Authorization", Value: "Bearer fixture-token"}, &traffic.Header{Name: ":authority", Value: "api.example.test"})
	flow.Request.Method = "post"
	flow.Request.Body = []byte(`{"q":"it's"}`)
	e1 := f.addEvidence("t1", v.SessionID, flow)
	if result, message := f.callVerification(v, reproPayload(e1.ID)); result == nil {
		t.Fatalf("verification rejected: %s", message)
	}
	vid := vulnOf(f.vulns("a1", ""), fid).ID
	get := func(query string) (int, string, string) {
		t.Helper()
		status, _, raw := f.do("GET", "/api/webapi/vulns/"+vid+"/card/curl"+query, "")
		return status, raw, ""
	}
	status, masked, _ := get("")
	want := "# 已遮盖敏感头与测试账号 secret（******），重放前请替换为有效值\n" +
		"curl -sS -X POST 'https://api.example.test/orders/1002?expand=1&pw=******' \\\n" +
		"  -H 'Host: api.example.test' \\\n" +
		"  -H 'Cookie: ******' \\\n" +
		"  -H 'X-Trace: user demo-user / ******' \\\n" +
		"  -H 'Authorization: ******' \\\n" +
		"  --data-binary '{\"q\":\"it'\\''s\"}'\n"
	if status != 200 || masked != want {
		t.Fatalf("masked curl = %d\n%s\nwant\n%s", status, masked, want)
	}
	if again, raw, _ := get("?step=1&mask=1"); again != 200 || raw != masked {
		t.Fatal("curl not deterministic")
	}
	var audits int
	countUnmasked := func() int {
		_ = f.capability.store.db().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM webapi_audit_events WHERE action = 'vuln.curl_unmasked'`).Scan(&audits)
		return audits
	}
	if countUnmasked() != 0 {
		t.Fatal("masked curl must not be audited")
	}
	status, unmasked, _ := get("?step=1&mask=0")
	wantUnmasked := "curl -sS -X POST 'https://api.example.test/orders/1002?expand=1&pw=Fixture-Pass-9' \\\n" +
		"  -H 'Host: api.example.test' \\\n" +
		"  -H 'Cookie: sid=fixture-session-abcdef' \\\n" +
		"  -H 'X-Trace: user demo-user / Fixture-Pass-9' \\\n" +
		"  -H 'Authorization: Bearer fixture-token' \\\n" +
		"  --data-binary '{\"q\":\"it'\\''s\"}'\n"
	if status != 200 || unmasked != wantUnmasked {
		t.Fatalf("unmasked curl = %d\n%s", status, unmasked)
	}
	if countUnmasked() != 1 {
		t.Fatalf("unmasked audits = %d", audits)
	}
	var detail string
	_ = f.capability.store.db().QueryRowContext(t.Context(), `SELECT detail_json FROM webapi_audit_events WHERE action = 'vuln.curl_unmasked'`).Scan(&detail)
	if !strings.Contains(detail, `"step":1`) || !strings.Contains(detail, e1.ID) || !strings.Contains(detail, v.VerificationID) || strings.Contains(detail, "Fixture-Pass-9") {
		t.Fatalf("audit detail = %s", detail)
	}
	if card := f.card(vid); len(card.Appendix.Changes) != 1 || card.Appendix.Changes[0].Action != "vuln.curl_unmasked" {
		t.Fatalf("changes = %+v", card.Appendix.Changes)
	}
	for query, want := range map[string]int{"?step=2": 400, "?step=0": 400, "?step=x": 400, "?mask=2": 400} {
		if status, raw, _ := get(query); status != want {
			t.Fatalf("curl %s = %d %s", query, status, raw)
		}
	}
	// 二进制正文不内联。
	binary := buildCurlCommand(&traffic.Flow{Request: &traffic.HttpRequest{Method: "PUT", Url: "https://api.example.test/upload", Body: []byte{0, 1, 2, 3}}}, true, nil)
	if binary != "# 请求体为二进制（4 字节），未内联\ncurl -sS -X PUT 'https://api.example.test/upload'\n" {
		t.Fatalf("binary curl = %q", binary)
	}
	// 无最小复现的条目：404。
	other, otherRev := f.addFinding("t1", "调试接口", notifyContent(), evidenceAt("GET", "https://api.example.test/debug"))
	f.legacyVerify("t1", other, otherRev)
	otherKey := vulnOf(f.vulns("a1", ""), other).ID
	if status, _, raw := f.do("GET", "/api/webapi/vulns/"+otherKey+"/card/curl", ""); status != 404 || !strings.Contains(raw, "尚无最小复现") {
		t.Fatalf("no repro curl = %d %s", status, raw)
	}
	if status, _, raw := f.do("GET", "/api/webapi/vulns/no-such/card/curl", ""); status != 404 {
		t.Fatalf("unknown curl = %d %s", status, raw)
	}
}

// 推送：未定级用 AI 建议等级“【AI 建议·高危】”，正文影响与关键复现来自最小复现，图片用第 1 步证据并按 marks 高亮；人工定级后用人工等级。
func TestVulnCardNotifyUsesRepro(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"普通用户","username":"demo-user","secret":"Fixture-Pass-9"}`); status != 200 {
		t.Fatalf("credential = %d %s", status, raw)
	}
	f.launchManual("a1", "t1")
	f.enable(false, "https://wscyber.example.test/")
	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	v := f.newVerification("t1", fid, frev)
	stepFlow := cardFlow("https://api.example.test/orders/1002?expand=1", "sid=fixture-session-abcdef", `{"order":"1002","owner":"user-42","note":"Fixture-Pass-9"}`)
	step := f.addEvidence("t1", v.SessionID, stepFlow)
	normal := f.addEvidence("t1", v.SessionID, cardFlow("https://api.example.test/orders/1001", "sid=fixture-session-abcdef", `{"owner":"demo-user"}`))
	payload := reproPayload(step.ID)
	payload["impact"] = "普通用户 demo-user（口令 Fixture-Pass-9）可读取他人订单的收货信息"
	payload["key_steps"] = []map[string]any{
		{"title": "以 demo-user 请求他人订单 1002", "evidence_id": step.ID, "request_marks": []string{"/orders/1002"}, "response_marks": []string{`"owner":"user-42"`, "/orders/1002"}},
		{"title": "对照：请求本人订单 1001", "evidence_id": normal.ID},
	}
	payload["suggested_severity"] = "high"
	payload["severity_reason"] = "可批量读取"
	if result, message := f.callVerification(v, payload); result == nil {
		t.Fatalf("verification rejected: %s", message)
	}
	var resultAt string
	_ = f.capability.store.db().QueryRowContext(t.Context(), `SELECT created_at FROM webapi_verification_results WHERE verification_id = ?`, v.VerificationID).Scan(&resultAt)
	local := parseAt(resultAt).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")
	f.deliver()
	got := f.wecom.received()
	if len(got) != 2 || got[1]["msgtype"] != "image" {
		t.Fatalf("sent = %+v", got)
	}
	want := "**【新确认漏洞】【AI 建议·高危】订单接口越权读取**\n" +
		"> 资产：演示库存系统\n" +
		"> 位置：GET https://api.example.test/orders/1002?expand=1\n" +
		"> 判读：独立验证支持（" + local + "）\n" +
		"影响：普通用户 demo-user（口令 ******）可读取他人订单的收货信息\n" +
		"关键复现：\n" +
		"1. 以 demo-user 请求他人订单 1002\n" +
		"2. 对照：请求本人订单 1001\n" +
		"[在平台查看](https://wscyber.example.test/?page=vulns&vuln=" + vulnOf(f.vulns("a1", ""), fid).ID + ")"
	if markdown := got[0]["markdown"].(map[string]any)["content"].(string); markdown != want {
		t.Fatalf("markdown =\n%s\nwant\n%s", markdown, want)
	}
	decoded, err := base64.StdEncoding.DecodeString(got[1]["image"].(map[string]any)["base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	scrubbed := proto.Clone(stepFlow).(*traffic.Flow)
	scrubFlow(scrubbed, []string{"Fixture-Pass-9"})
	expected, err := renderEvidenceImage(scrubbed, requestEvidenceImageMeta(step), evidenceImageOptions{Marks: []string{"/orders/1002", `"owner":"user-42"`}})
	if err != nil || !bytes.Equal(expected, decoded) {
		t.Fatalf("image differs from step-1 marked render (err %v)", err)
	}
	unmarked, _ := renderEvidenceImage(scrubbed, requestEvidenceImageMeta(step), evidenceImageOptions{})
	if bytes.Equal(unmarked, decoded) {
		t.Fatal("image should carry mark highlights")
	}
	// 人工定级后用人工等级。
	item := f.notifications()[0]
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+item.DedupeKey+"/triage", `{"severity":"critical"}`); status != 200 {
		t.Fatalf("triage = %d %s", status, raw)
	}
	message, err := f.capability.buildNotifyMessage(t.Context(), item, "")
	if err != nil || !strings.HasPrefix(message.markdown, "**【新确认漏洞】【严重】订单接口越权读取**") {
		t.Fatalf("triaged markdown = %v %v", message, err)
	}
}
