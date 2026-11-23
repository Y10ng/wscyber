package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/exts/guardrail"
)

// 审计记录（B3）测试：全部为临时库合成数据（example.test 演示资产、虚构账号与 webhook key）。

func auditRows(t *testing.T, capability *Capability) []auditEventModel {
	t.Helper()
	var rows []auditEventModel
	if err := capability.store.orm.NewSelect().Model(&rows).OrderExpr("at ASC, id ASC").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	return rows
}

func auditByAction(rows []auditEventModel, action string) []auditEventModel {
	out := []auditEventModel{}
	for _, row := range rows {
		if row.Action == action {
			out = append(out, row)
		}
	}
	return out
}

// requireAudit 断言某 action 恰好 n 条并返回最后一条。
func requireAudit(t *testing.T, capability *Capability, action string, n int) auditEventModel {
	t.Helper()
	rows := auditByAction(auditRows(t, capability), action)
	if len(rows) != n {
		t.Fatalf("%s 登记 %d 条，期望 %d：%+v", action, len(rows), n, rows)
	}
	if n == 0 {
		return auditEventModel{}
	}
	return rows[n-1]
}

func auditDetail(t *testing.T, row auditEventModel) map[string]any {
	t.Helper()
	var detail map[string]any
	if err := json.Unmarshal([]byte(row.DetailJSON), &detail); err != nil {
		t.Fatalf("detail %q: %v", row.DetailJSON, err)
	}
	return detail
}

// assertNoSecrets 逐条扫描全部审计行（summary 与 detail）不含给定敏感串。
func assertNoSecrets(t *testing.T, capability *Capability, secrets ...string) {
	t.Helper()
	for _, row := range auditRows(t, capability) {
		for _, secret := range secrets {
			if secret != "" && (strings.Contains(row.DetailJSON, secret) || strings.Contains(row.Summary, secret) || strings.Contains(row.TargetID, secret)) {
				t.Fatalf("审计行 %s %s 含敏感串 %q：%s %s", row.ID, row.Action, secret, row.Summary, row.DetailJSON)
			}
		}
	}
}

func TestAuditTaskPolicyAndSessionStarts(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-19T06:00:00Z")
	body := `{"known_info":"演示接口","objective":"检查越权\n第二行","authorization":{"confirmed":true,"origins":["https://api.example.test"]}}`
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1", body); status != 201 {
		t.Fatalf("create task = %d %s", status, raw)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/tasks/t1", body); status != 200 {
		t.Fatalf("replay = %d", status)
	}
	row := requireAudit(t, f.capability, "task.create", 1)
	detail := auditDetail(t, row)
	if row.Actor != auditActorUser || row.TaskID != "t1" || row.AssetID != "" || row.Summary != "新建任务：检查越权" || row.At != "2026-11-19T06:00:00.000Z" ||
		detail["policy"].(map[string]any)["scope"] != ScopeOrigins || len(detail["origins"].([]any)) != 1 {
		t.Fatalf("task.create = %+v %v", row, detail)
	}

	// 策略修改登记 from/to；同值保存不登记。
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1/policy", `{"scope":"unrestricted","approval":"high_risk"}`); status != 200 {
		t.Fatalf("policy = %d %s", status, raw)
	}
	f.do("PUT", "/api/webapi/tasks/t1/policy", `{"scope":"unrestricted","approval":"high_risk"}`)
	row = requireAudit(t, f.capability, "task.policy", 1)
	detail = auditDetail(t, row)
	if row.Summary != "修改任务测试策略：仅授权 origins/仅高危 → 不限制/仅高危" || detail["from"].(map[string]any)["scope"] != ScopeOrigins || detail["to"].(map[string]any)["scope"] != ScopeUnrestricted {
		t.Fatalf("task.policy = %+v", row)
	}

	// 验证/复测/报告会话：新建登记一次，幂等重放不登记。
	findingID, revisionID := f.addFinding("t1", "演示越权", simpleContent(), nil)
	number := f.capability.findingNumberOf(t.Context(), findingID)
	source := `{"source_revision_id":"` + revisionID + `"}`
	for range 2 {
		f.do("PUT", "/api/webapi/tasks/t1/findings/"+findingID+"/verifications/v1", source)
		f.do("PUT", "/api/webapi/tasks/t1/findings/"+findingID+"/retests/r1", source)
		f.do("PUT", "/api/webapi/tasks/t1/reports/rp1", `{"items":[{"finding_id":"`+findingID+`","revision_id":"`+revisionID+`"}]}`)
	}
	for _, kind := range []string{"verification", "retest"} {
		row = requireAudit(t, f.capability, kind+".start", 1)
		detail = auditDetail(t, row)
		if row.Actor != auditActorUser || row.TaskID != "t1" || detail["finding_number"] != number || detail["session_id"] == "" || !strings.Contains(row.Summary, number) {
			t.Fatalf("%s.start = %+v", kind, row)
		}
	}
	row = requireAudit(t, f.capability, "report.start", 1)
	if row.TargetID != "rp1" || !strings.Contains(row.Summary, number) || auditDetail(t, row)["session_id"] == "" {
		t.Fatalf("report.start = %+v", row)
	}
}

func TestAuditAssetCredentialScheduleAndLaunch(t *testing.T) {
	const secondSecret = "demo-Secret-second-9b"
	f := newScheduleFixture(t, "2026-11-18T09:59:00Z")
	f.createAsset("a1")
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1", assetBody); status != 200 {
		t.Fatal("same save failed")
	}
	row := requireAudit(t, f.capability, "asset.create", 1)
	values := auditDetail(t, row)["values"].(map[string]any)
	if row.Summary != "新建资产：演示库存系统" || values["name"] != "演示库存系统" || values["owner"] != "测试组" || values["known_info"] != nil || values["forbidden"] != nil {
		t.Fatalf("asset.create = %+v", row)
	}
	requireAudit(t, f.capability, "asset.update", 0) // 未变化的保存不登记

	renamed := strings.Replace(assetBody, `"name":"演示库存系统"`, `"name":"演示库存系统二"`, 1)
	f.do("PUT", "/api/webapi/assets/a1", renamed)
	row = requireAudit(t, f.capability, "asset.update", 1)
	if changed := auditDetail(t, row)["changed"].([]any); len(changed) != 1 || changed[0] != "name" || !strings.Contains(row.Summary, "名称") {
		t.Fatalf("asset.update = %+v", row)
	}
	archived := strings.TrimSuffix(renamed, "}") + `,"archived":true}`
	f.do("PUT", "/api/webapi/assets/a1", archived)
	f.do("PUT", "/api/webapi/assets/a1", renamed)
	requireAudit(t, f.capability, "asset.archive", 1)
	requireAudit(t, f.capability, "asset.unarchive", 1)

	// 测试账号：secret 只记是否更换；未变化不登记；删除登记 label/username。
	cred := `{"label":"管理员","username":"admin@example.test","secret":"` + assetTestSecret + `"}`
	f.do("PUT", "/api/webapi/assets/a1/credentials/c1", cred)
	f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"管理员","username":"admin@example.test"}`)
	f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"管理员","username":"admin@example.test","secret":"`+secondSecret+`"}`)
	rows := auditByAction(auditRows(t, f.capability), "credential.save")
	if len(rows) != 2 {
		t.Fatalf("credential.save = %+v", rows)
	}
	first, second := auditDetail(t, rows[0]), auditDetail(t, rows[1])
	if first["created"] != true || first["secret_replaced"] != true || first["username"] != "admin@example.test" ||
		second["created"] != false || second["secret_replaced"] != true || len(second["changed"].([]any)) != 1 {
		t.Fatalf("credential details = %v / %v", first, second)
	}
	if status, _, _ := f.do("DELETE", "/api/webapi/assets/a1/credentials/c1", ""); status != 200 {
		t.Fatal("delete credential failed")
	}
	f.do("DELETE", "/api/webapi/assets/a1/credentials/c1", "") // 404 不登记
	if row = requireAudit(t, f.capability, "credential.delete", 1); auditDetail(t, row)["label"] != "管理员" {
		t.Fatalf("credential.delete = %+v", row)
	}

	// 计划配置：保存、未变化不登记、删除。
	f.putSchedule("a1", `{"cron":"30 * * * *"}`)
	f.putSchedule("a1", `{"cron":"30 * * * *"}`)
	f.putSchedule("a1", `{"cron":"30 * * * *","enabled":false}`)
	rows = auditByAction(auditRows(t, f.capability), "schedule.save")
	if len(rows) != 2 || auditDetail(t, rows[0])["created"] != true || auditDetail(t, rows[1])["from"].(map[string]any)["enabled"] != true {
		t.Fatalf("schedule.save = %+v", rows)
	}
	f.do("DELETE", "/api/webapi/assets/a1/schedule", "")
	if row = requireAudit(t, f.capability, "schedule.delete", 1); auditDetail(t, row)["cron"] != "30 * * * *" {
		t.Fatalf("schedule.delete = %+v", row)
	}

	// 从资产手动发起：user，带 asset_id；重放不重复。
	f.launchManual("a1", "t-manual")
	f.do("PUT", "/api/webapi/assets/a1/tasks/t-manual", "")
	row = requireAudit(t, f.capability, "task.create", 1)
	if row.Actor != auditActorUser || row.AssetID != "a1" || auditDetail(t, row)["asset_id"] != "a1" || !strings.HasPrefix(row.Summary, "从资产「演示库存系统二」发起任务") {
		t.Fatalf("asset launch = %+v", row)
	}

	// 到点发起：schedule.run 与 task.create 均为 system；同一时刻再查/中断重放不重复。
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.setNow("2026-11-18T10:00:20Z")
	f.tick()
	f.tick()
	if _, err := f.capability.store.db().ExecContext(t.Context(), "UPDATE webapi_asset_schedules SET next_run_at = ? WHERE asset_id = ?", "2026-11-18T10:00:00Z", "a1"); err != nil {
		t.Fatal(err)
	}
	f.tick()
	wantTask := scheduleTaskID("a1", "2026-11-18T10:00:00Z")
	row = requireAudit(t, f.capability, "schedule.run", 1)
	detail := auditDetail(t, row)
	if row.Actor != auditActorSystem || row.TaskID != wantTask || detail["status"] != ScheduleRunLaunched || detail["cron"] != "0 * * * *" {
		t.Fatalf("schedule.run = %+v", row)
	}
	created := auditByAction(auditRows(t, f.capability), "task.create")
	if len(created) != 2 || created[1].Actor != auditActorSystem || created[1].TaskID != wantTask {
		t.Fatalf("scheduled task.create = %+v", created)
	}

	// 发起失败：system，带失败原因。
	f.launcher.mu.Lock()
	f.launcher.agents = nil
	f.launcher.mu.Unlock()
	f.setNow("2026-11-18T11:00:20Z")
	f.tick()
	rows = auditByAction(auditRows(t, f.capability), "schedule.run")
	if len(rows) != 2 || auditDetail(t, rows[1])["status"] != ScheduleRunFailed || !strings.Contains(rows[1].Summary, "计划运行失败") || auditDetail(t, rows[1])["error"] == "" {
		t.Fatalf("failed schedule.run = %+v", rows)
	}
	assertNoSecrets(t, f.capability, assetTestSecret, secondSecret)
}

func TestAuditVulnManualOps(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	f1, _ := f.addFinding("t1", "列表接口越权", simpleContent(), evidenceAt("GET", "https://api.example.test/items/1"))
	f2, _ := f.addFinding("t1", "列表接口越权（另一参数）", simpleContent(), evidenceAt("GET", "https://api.example.test/items/2"))
	f3, _ := f.addFinding("t1", "导出接口信息泄露", simpleContent(), evidenceAt("GET", "https://api.example.test/export"))
	list := f.vulns("a1", "")
	auto, exportVuln := vulnOf(list, f1).ID, vulnOf(list, f3).ID
	before := vulnOf(list, f1).Title

	f.do("PUT", "/api/webapi/assets/a1/vulns/"+auto, `{"title":"物品接口越权"}`)
	f.do("PUT", "/api/webapi/assets/a1/vulns/"+auto, `{"title":"物品接口越权"}`) // 同标题不登记
	row := requireAudit(t, f.capability, "vuln.rename", 1)
	if detail := auditDetail(t, row); detail["from_title"] != before || detail["to_title"] != "物品接口越权" || row.AssetID != "a1" {
		t.Fatalf("vuln.rename = %+v", row)
	}

	f.do("PUT", "/api/webapi/assets/a1/vulns/manual-1/members/"+f2, "")
	f.do("PUT", "/api/webapi/assets/a1/vulns/manual-1/members/"+f2, "") // 重放不登记
	row = requireAudit(t, f.capability, "vuln.move", 1)
	if detail := auditDetail(t, row); detail["from_vuln_id"] != auto || detail["to_vuln_id"] != "manual-1" || detail["created"] != true {
		t.Fatalf("vuln.move = %+v", row)
	}

	if status, _, raw := f.do("POST", "/api/webapi/assets/a1/vulns/manual-1/merge", `{"from":"`+exportVuln+`"}`); status != 200 {
		t.Fatalf("merge = %d %s", status, raw)
	}
	f.do("POST", "/api/webapi/assets/a1/vulns/manual-1/merge", `{"from":"nope"}`) // 404 不登记
	row = requireAudit(t, f.capability, "vuln.merge", 1)
	if detail := auditDetail(t, row); detail["from_vuln_id"] != exportVuln || detail["from_title"] != "导出接口信息泄露" || detail["into_vuln_id"] != "manual-1" {
		t.Fatalf("vuln.merge = %+v", row)
	}
}

func TestAuditNotifySettingsTestAndNotifications(t *testing.T) {
	f := newNotifyFixture(t)
	f.enable(true, "https://hub.example.test")
	f.enable(true, "https://hub.example.test") // 同 webhook 同配置：不登记
	row := requireAudit(t, f.capability, "notify.settings", 1)
	detail := auditDetail(t, row)
	if detail["webhook_changed"] != true || detail["webhook_cleared"] != false || detail["to"].(map[string]any)["base_url"] != "https://hub.example.test" || detail["from"].(map[string]any)["enabled"] != false {
		t.Fatalf("notify.settings = %+v", row)
	}

	f.do("POST", "/api/webapi/notify/wecom/test", "")
	f.wecom.respond(93000)
	f.do("POST", "/api/webapi/notify/wecom/test", "")
	rows := auditByAction(auditRows(t, f.capability), "notify.test")
	if len(rows) != 2 || auditDetail(t, rows[0])["ok"] != true || auditDetail(t, rows[1])["ok"] != false || auditDetail(t, rows[1])["errcode"] != float64(93000) {
		t.Fatalf("notify.test = %+v", rows)
	}

	// 人工确认/忽略；确认后投递成功登记 notification.sent（system）。
	f.plainTask("t1")
	flow := notifyFlow("GET", "https://api.example.test/orders/1002", "", "{}")
	a, arev := f.addFinding("t1", "候选甲", notifyContent(), nil)
	b, brev := f.addFinding("t1", "候选乙", notifyContent(), nil)
	f.verify("t1", a, arev, "supported", flow)
	f.verify("t1", b, brev, "supported", flow)
	items := f.notifications()
	f.do("POST", "/api/webapi/notifications/"+items[0].ID+"/confirm", "")
	f.do("POST", "/api/webapi/notifications/"+items[0].ID+"/confirm", "") // 409 不登记
	f.do("POST", "/api/webapi/notifications/"+items[1].ID+"/dismiss", "")
	row = requireAudit(t, f.capability, "notification.confirm", 1)
	if row.Actor != auditActorUser || row.TaskID != "t1" || auditDetail(t, row)["title"] != "候选甲" || row.Summary != "确认推送：候选甲" {
		t.Fatalf("confirm = %+v", row)
	}
	requireAudit(t, f.capability, "notification.dismiss", 1)
	f.deliver()
	row = requireAudit(t, f.capability, "notification.sent", 1)
	if row.Actor != auditActorSystem || row.TargetID != items[0].ID || auditDetail(t, row)["attempts"] != float64(0) {
		t.Fatalf("sent = %+v", row)
	}

	// 改为无需确认（webhook 省略＝保留）；第三条连续失败 3 次 → notification.failed，再人工重试。
	f.configure(`{"enabled":true,"require_confirm":false,"base_url":"https://hub.example.test"}`)
	row = requireAudit(t, f.capability, "notify.settings", 2)
	if detail := auditDetail(t, row); detail["webhook_changed"] != false || detail["to"].(map[string]any)["require_confirm"] != false {
		t.Fatalf("settings change = %+v", row)
	}
	c, crev := f.addFinding("t1", "候选丙", notifyContent(), nil)
	f.verify("t1", c, crev, "supported", flow)
	third := f.notifications()[2].ID
	f.wecom.respond(93000, 93000, 93000)
	for _, step := range []time.Duration{0, time.Minute, 5 * time.Minute} {
		f.advance(step)
		f.deliver()
	}
	row = requireAudit(t, f.capability, "notification.failed", 1)
	if detail := auditDetail(t, row); row.Actor != auditActorSystem || row.TargetID != third || detail["attempts"] != float64(3) || !strings.Contains(detail["last_error"].(string), "93000") {
		t.Fatalf("failed = %+v", row)
	}
	requireAudit(t, f.capability, "notification.sent", 1)
	f.do("POST", "/api/webapi/notifications/"+third+"/retry", "")
	requireAudit(t, f.capability, "notification.retry", 1)

	// 清除 webhook。
	f.configure(`{"webhook":"-","enabled":false,"require_confirm":false,"base_url":"https://hub.example.test"}`)
	row = requireAudit(t, f.capability, "notify.settings", 3)
	if detail := auditDetail(t, row); detail["webhook_cleared"] != true || detail["webhook_changed"] != false {
		t.Fatalf("clear = %+v", row)
	}
	assertNoSecrets(t, f.capability, "fixture-key-abcd", f.wecom.server.URL, "/cgi-bin/webhook")
}

func TestAuditApprovalDecisions(t *testing.T) {
	capability, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	tool.approvals = capability.approvals
	h := newApprovalHarness(t, capability)
	target := task.Origins[0] + "/api/items/7?token=fixture-token-77aa"
	args := `{"method":"DELETE","url":"` + target + `","headers":{"Authorization":"Bearer fixture-bearer-55cc"}}`
	run := func(ctx context.Context, callID string, decide func(operationID string)) {
		inv.CallID = callID
		done := make(chan struct{})
		local := inv
		go func() {
			_, _ = h.execute(ctx, local, "webapi_http", args, tool.Execute)
			close(done)
		}()
		if decide != nil {
			decide(h.waitPending(t, task.InitialSessionID).GetOperation().GetOperationId())
		}
		<-done
	}
	run(t.Context(), "approve-call", func(id string) { h.resolve(t, task.InitialSessionID, id, true) })
	run(t.Context(), "deny-call", func(id string) { h.resolve(t, task.InitialSessionID, id, false) })
	capability.approvals.timeout = 50 * time.Millisecond
	run(t.Context(), "expire-call", nil)
	capability.approvals.timeout = ReviewTimeout
	ctx, cancel := context.WithCancel(t.Context())
	run(ctx, "cancel-call", func(string) { cancel() }) // 取消不是决定，不登记

	for _, want := range []struct{ action, actor string }{{"approval.approve", auditActorUser}, {"approval.deny", auditActorUser}, {"approval.expire", auditActorSystem}} {
		row := requireAudit(t, capability, want.action, 1)
		detail := auditDetail(t, row)
		operationText, _ := detail["operation"].(string)
		if row.Actor != want.actor || row.TaskID != task.ID || detail["tool"] != "webapi_http" || detail["session_id"] != task.InitialSessionID ||
			!strings.HasPrefix(operationText, "DELETE "+task.Origins[0]+"/api/items/7") || row.TargetID == "" {
			t.Fatalf("%s = %+v %v", want.action, row, detail)
		}
	}
	if rows := auditRows(t, capability); len(rows) != 3 {
		t.Fatalf("approval rows = %+v", rows)
	}
	assertNoSecrets(t, capability, "fixture-token-77aa", "fixture-bearer-55cc")

	// 终端命令摘要截前 200 字并脱敏。
	long := "curl -X DELETE http://example.test/x --user admin:fixture-pass-11 " + strings.Repeat("a", 400)
	raw, _ := json.Marshal(map[string]string{"command": long})
	operation := approvalSummary("bash", string(raw), guardrail.RedactText)
	if len([]rune(operation)) != approvalSummaryRunes || strings.Contains(operation, "fixture-pass-11") {
		t.Fatalf("bash operation = %q", operation)
	}
	if _, err := coretool.ParseArgs[httpToolArguments](args); err != nil {
		t.Fatal(err)
	}
}

func TestAuditQueryFiltersPagingAndValidation(t *testing.T) {
	f := newAssetFixture(t)
	local := func(day, clock string) time.Time {
		at, err := time.ParseInLocation("2006-01-02 15:04:05.000", "2026-11-"+day+" "+clock, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return at
	}
	events := []struct {
		entry auditEntry
		at    time.Time
	}{
		{auditEntry{ID: "au_q1", Actor: "user", Action: "asset.create", TargetKind: "asset", TargetID: "a1", AssetID: "a1", Summary: "新建资产：演示库存"}, local("10", "10:00:00.000")},
		{auditEntry{ID: "au_q2", Actor: "user", Action: "asset.update", TargetKind: "asset", TargetID: "a1", AssetID: "a1", Summary: "修改资产「演示库存」：名称"}, local("11", "10:00:00.000")},
		{auditEntry{ID: "au_q3", Actor: "system", Action: "task.create", TargetKind: "task", TargetID: "t1", TaskID: "t1", AssetID: "a1", Summary: "从资产发起任务：Check IDOR"}, local("12", "10:00:00.000")},
		{auditEntry{ID: "au_q4", Actor: "system", Action: "approval.expire", TargetKind: "operation", TargetID: "op-77", TaskID: "t1", Summary: "审批超时"}, local("12", "10:00:00.000")},
		{auditEntry{ID: "au_q5", Actor: "user", Action: "notify.test", TargetKind: "notify", TargetID: "wecom", Summary: "测试企业微信推送：成功"}, local("13", "00:00:00.000")},
		{auditEntry{ID: "au_q6", Actor: "user", Action: "credential.save", TargetKind: "credential", TargetID: "c1", AssetID: "a1", Summary: "新增测试账号：管理员"}, local("13", "23:59:59.999")},
	}
	for _, event := range events {
		if err := f.capability.store.appendAudit(t.Context(), event.entry, event.at); err != nil {
			t.Fatal(err)
		}
	}
	ids := func(query string) []string {
		t.Helper()
		status, value, raw := f.do("GET", "/api/webapi/audit?"+query, "")
		if status != 200 {
			t.Fatalf("query %s = %d %s", query, status, raw)
		}
		out := []string{}
		for _, item := range value["items"].([]any) {
			out = append(out, item.(map[string]any)["id"].(string))
		}
		return out
	}
	cases := map[string]string{
		"":                                     "au_q6,au_q5,au_q4,au_q3,au_q2,au_q1",
		"action=asset.":                        "au_q2,au_q1",
		"action=asset":                         "",
		"action=asset.create":                  "au_q1",
		"actor=system":                         "au_q4,au_q3",
		"task_id=t1":                           "au_q4,au_q3",
		"asset_id=a1":                          "au_q6,au_q3,au_q2,au_q1",
		"asset_id=missing":                     "",
		"from=2026-11-13&to=2026-11-13":        "au_q6,au_q5",
		"to=2026-11-12":                        "au_q4,au_q3,au_q2,au_q1",
		"from=2026-11-11&to=2026-11-12":        "au_q4,au_q3,au_q2",
		"q=" + url.QueryEscape("check idor"):   "au_q3",
		"q=op-77":                              "au_q4",
		"actor=user&asset_id=a1&action=asset.": "au_q2,au_q1",
	}
	for query, want := range cases {
		if got := strings.Join(ids(query), ","); got != want {
			t.Errorf("query %q = %s, want %s", query, got, want)
		}
	}

	// 游标分页：不重不漏，末页 next_cursor 为 null。
	seen, cursor := []string{}, ""
	for page := 0; page < 10; page++ {
		status, value, raw := f.do("GET", "/api/webapi/audit?limit=4&cursor="+url.QueryEscape(cursor), "")
		if status != 200 {
			t.Fatalf("page = %d %s", status, raw)
		}
		for _, item := range value["items"].([]any) {
			entry := item.(map[string]any)
			if _, ok := entry["detail"].(map[string]any); !ok {
				t.Fatalf("detail not object: %v", entry)
			}
			seen = append(seen, entry["id"].(string))
		}
		if value["has_more"] != true {
			if value["next_cursor"] != nil {
				t.Fatalf("last page cursor = %v", value["next_cursor"])
			}
			break
		}
		cursor = value["next_cursor"].(string)
	}
	if got := strings.Join(seen, ","); got != "au_q6,au_q5,au_q4,au_q3,au_q2,au_q1" {
		t.Fatalf("paged = %s", got)
	}
	if got := ids("limit=1&cursor=" + url.QueryEscape(encodeKeyCursor(notifyTime(local("12", "10:00:00.000")), "au_q4"))); strings.Join(got, ",") != "au_q3" {
		t.Fatalf("tie cursor = %v", got)
	}

	for _, query := range []string{"limit=0", "limit=201", "limit=x", "actor=admin", "action=Asset", "action=asset..create", "action=" + strings.Repeat("a", 65),
		"task_id=a%2Fb", "asset_id=a%2Fb", "from=2026-13-01", "to=2026-11-1", "from=2026-11-13&to=2026-11-12", "cursor=bad", "q=" + strings.Repeat("长", 201)} {
		if status, value, raw := f.do("GET", "/api/webapi/audit?"+query, ""); status != 400 || value["error"] == nil {
			t.Errorf("query %s = %d %s, want 400 {error}", query, status, raw)
		}
	}
}
