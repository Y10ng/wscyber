package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	"github.com/uptrace/bun"
)

// 运营通知（V4）测试：临时库 + httptest 模拟企业微信，数据全部为 example.test 合成内容，不向任何真实地址发送。

func opsNotifications(t *testing.T, c *Capability, kind string) []notificationModel {
	t.Helper()
	c.retryOpsRegistrations(t.Context()) // fixture 不启动后台投递，显式推进登记队列。
	var items []notificationModel
	if err := c.store.orm.NewSelect().Model(&items).Where("kind = ?", kind).OrderExpr("created_at ASC, id ASC").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	return items
}

func wecomMarkdowns(f *fakeWeCom) []string {
	out := []string{}
	for _, body := range f.received() {
		if body["msgtype"] == "markdown" {
			out = append(out, body["markdown"].(map[string]any)["content"].(string))
		}
	}
	return out
}

func notifySettingsBody(webhook string, requireConfirm bool, baseURL string, extra string) string {
	body := fmt.Sprintf(`{"webhook":%q,"enabled":true,"require_confirm":%v,"base_url":%q`, webhook, requireConfirm, baseURL)
	if extra != "" {
		body += "," + extra
	}
	return body + "}"
}

func TestNotifyOpsSettingsAPI(t *testing.T) {
	f := newNotifyFixture(t)
	if value := f.configure(`{"enabled":false,"require_confirm":false,"base_url":""}`); value["notify_approval"] != false || value["notify_schedule_failed"] != false || value["notify_weekly_digest"] != false {
		t.Fatalf("defaults = %v", value)
	}
	value := f.configure(notifySettingsBody(f.wecom.webhook(), true, "https://hub.example.test", `"notify_approval":true,"notify_schedule_failed":true,"notify_weekly_digest":true`))
	if value["notify_approval"] != true || value["notify_schedule_failed"] != true || value["notify_weekly_digest"] != true || value["require_confirm"] != true {
		t.Fatalf("saved = %v", value)
	}
	ops, _ := f.capability.store.notifyOpsSettings(t.Context())
	if ops == nil || ops.WeeklyDigestSince != notifyTime(f.now) {
		t.Fatalf("since = %+v", ops)
	}
	// 旧客户端整份保存但不带三项：保留原值（since 也不变）。
	f.advance(time.Hour)
	value = f.configure(`{"enabled":true,"require_confirm":false,"base_url":"https://hub.example.test"}`)
	if value["notify_approval"] != true || value["notify_schedule_failed"] != true || value["notify_weekly_digest"] != true || value["require_confirm"] != false {
		t.Fatalf("omitted keeps = %v", value)
	}
	if again, _ := f.capability.store.notifyOpsSettings(t.Context()); again.WeeklyDigestSince != ops.WeeklyDigestSince {
		t.Fatalf("since changed on keep: %s → %s", ops.WeeklyDigestSince, again.WeeklyDigestSince)
	}
	// 显式 false 关闭；关闭后 since 清空，再开重新记时间。
	value = f.configure(`{"enabled":true,"require_confirm":false,"base_url":"","notify_approval":false,"notify_weekly_digest":false}`)
	if value["notify_approval"] != false || value["notify_schedule_failed"] != true || value["notify_weekly_digest"] != false {
		t.Fatalf("explicit false = %v", value)
	}
	if again, _ := f.capability.store.notifyOpsSettings(t.Context()); again.WeeklyDigestSince != "" {
		t.Fatalf("since not cleared: %+v", again)
	}
	if status, _, raw := f.do("PUT", "/api/webapi/notify/wecom", `{"enabled":true,"require_confirm":false,"base_url":"","notify_unknown":true}`); status != 400 {
		t.Fatalf("unknown field = %d %s", status, raw)
	}
	if status, value, _ := f.do("GET", "/api/webapi/notify/wecom", ""); status != 200 || value["notify_schedule_failed"] != true || value["has_webhook"] != true {
		t.Fatalf("GET = %d %v", status, value)
	}
	// 审计 notify.settings 带三项开关变化。
	var audits []auditEventModel
	if err := f.capability.store.orm.NewSelect().Model(&audits).Where("action = ?", "notify.settings").OrderExpr("at ASC, id ASC").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	last := audits[len(audits)-1]
	if !strings.Contains(last.Summary, "待审批通知启用 → 停用") || !strings.Contains(last.Summary, "每周摘要启用 → 停用") || !strings.Contains(last.DetailJSON, `"notify_approval":false`) {
		t.Fatalf("audit = %+v", last)
	}
}

// opsApprovalFixture：资产 a1（测试账号 alice，口令合成）发起任务 t1，审批 all，推送已启用并开待审批通知。
func opsApprovalFixture(t *testing.T, requireConfirm bool, extra string) (*notifyFixture, *Task, *approvalHarness) {
	t.Helper()
	f := newNotifyFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/alice", `{"label":"普通用户","username":"alice","secret":"alice-pass-synthetic"}`); status != 200 {
		t.Fatalf("credential = %d %s", status, raw)
	}
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/t1", ""); status != 201 {
		t.Fatalf("launch = %d %s", status, raw)
	}
	task, err := f.capability.store.Get(t.Context(), "t1")
	if err != nil || task == nil {
		t.Fatalf("task = %v %v", task, err)
	}
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1/policy", `{"scope":"origins","approval":"all"}`); status != 200 {
		t.Fatalf("policy = %d %s", status, raw)
	}
	f.configure(notifySettingsBody(f.wecom.webhook(), requireConfirm, "https://hub.example.test", extra))
	return f, task, newApprovalHarness(t, f.capability)
}

func opsByKey(t *testing.T, c *Capability, key string) notificationModel {
	t.Helper()
	c.retryOpsRegistrations(t.Context())
	var item notificationModel
	if err := c.store.orm.NewSelect().Model(&item).Where("dedupe_key = ?", key).Limit(1).Scan(t.Context()); err != nil {
		t.Fatalf("notification %s: %v", key, err)
	}
	return item
}

func startReview(t *testing.T, h *approvalHarness, ctx context.Context, sessionID, callID, tool, args string) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := h.execute(ctx, operation.Invocation{SessionID: sessionID, TurnID: "ops-turn", CallID: callID}, tool, args, func(context.Context, string) (*aop.ToolResult, error) {
			return &aop.ToolResult{}, nil
		})
		done <- err
	}()
	return done
}

func TestNotifyOpsApprovalPending(t *testing.T) {
	f, task, h := opsApprovalFixture(t, true, `"notify_approval":true`)
	// webapi_http：URL 里出现口令明文 → 回替为占位符；require_confirm=true 也直接 queued。
	done := startReview(t, h, t.Context(), task.InitialSessionID, "http-1", "webapi_http",
		`{"method":"post","url":"https://api.example.test/orders?q=alice-pass-synthetic&x=1"}`)
	pending := waitOpsPending(t, h, task.InitialSessionID)
	items := opsNotifications(t, f.capability, NotifyKindApprovalPending)
	opID := pending.GetOperation().GetOperationId()
	if len(items) != 1 || items[0].DedupeKey != "approval:"+opID || items[0].Status != NotifyQueued || items[0].TaskID != "t1" || items[0].VerificationID != "" {
		t.Fatalf("registered = %+v", items)
	}
	f.deliver()
	markdowns := wecomMarkdowns(f.wecom)
	if len(markdowns) != 1 {
		t.Fatalf("sent = %v", markdowns)
	}
	md := markdowns[0]
	for _, want := range []string{"**【待审批】检查越权与输入校验**", "> 工具：webapi_http", "> 请求：POST https://api.example.test/orders?q={{cred:alice.secret}}&x=1",
		"> 原因：全部人工审批：非只读方法 POST", "10 分钟内未处理将自动拒绝", "[去平台审批](https://hub.example.test/sessions/" + task.InitialSessionID + ")"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "alice-pass-synthetic") || strings.Contains(md, "【新确认漏洞】") {
		t.Fatalf("markdown leaked:\n%s", md)
	}
	// View：kind、title、asset、session_id；漏洞字段为空。
	views, err := f.capability.store.notificationViews(t.Context(), notificationFilter{ID: items[0].ID}, 1, 0)
	if err != nil || len(views) != 1 {
		t.Fatal(err)
	}
	view := views[0]
	if view.Kind != NotifyKindApprovalPending || view.Status != NotifySent || view.Title != "检查越权与输入校验" || view.Asset == nil || view.Asset.ID != "a1" ||
		view.SessionID != task.InitialSessionID || view.VulnID != nil || view.FindingID != "" || view.FindingNumber != "" || view.VerificationID != "" {
		t.Fatalf("view = %+v", view)
	}
	// confirm 对 queued/sent 的运营通知 409。
	if status, _, _ := f.do("POST", "/api/webapi/notifications/"+items[0].ID+"/confirm", ""); status != 409 {
		t.Fatalf("confirm ops = %d", status)
	}
	h.resolve(t, task.InitialSessionID, opID, false)
	<-done
	if got := f.notificationByID(items[0].ID); got.Status != NotifySent {
		t.Fatalf("sent should stay sent: %+v", got)
	}

	// bash：命令首 120 字，经 guardrail 遮盖；已决（拒绝）前未发出 → dismissed。
	f.wecom.reset()
	long := "curl -u admin:hunter2 https://api.example.test/x --data password=alice-pass-synthetic " + strings.Repeat("a", 200)
	done = startReview(t, h, t.Context(), task.InitialSessionID, "bash-1", "bash", mustToolArgumentsJSON(t, map[string]any{"command": long}))
	pending = waitOpsPending(t, h, task.InitialSessionID)
	bashKey := "approval:" + pending.GetOperation().GetOperationId()
	ops, _ := f.capability.store.notificationOps(t.Context(), opsByKey(t, f.capability, bashKey).ID)
	var payload opsPayload
	_ = json.Unmarshal([]byte(ops.PayloadJSON), &payload)
	if utf8.RuneCountInString(payload.Request) != opsRequestRunes || strings.Contains(payload.Request, "hunter2") || strings.Contains(payload.Request, "alice-pass-synthetic") || !strings.HasPrefix(payload.Request, "curl -u [REDACTED]") {
		t.Fatalf("bash summary = %q", payload.Request)
	}
	h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), false)
	<-done
	second := opsByKey(t, f.capability, bashKey)
	if second.Status != NotifyDismissed {
		t.Fatalf("resolved unsent = %+v", second)
	}
	f.deliver()
	if len(f.wecom.received()) != 0 {
		t.Fatalf("dismissed sent: %v", f.wecom.received())
	}

	// 超时：同样 dismissed。
	f.capability.approvals.timeout = 500 * time.Millisecond
	done = startReview(t, h, t.Context(), task.InitialSessionID, "expire-1", "write", `{"path":"/tmp/x.txt"}`)
	waitOpsPending(t, h, task.InitialSessionID)
	f.capability.retryOpsRegistrations(t.Context())
	if err := <-done; err == nil {
		t.Fatal("expired write executed")
	}
	f.capability.approvals.timeout = ReviewTimeout
	all := opsNotifications(t, f.capability, NotifyKindApprovalPending)
	if len(all) != 3 {
		t.Fatalf("approval rows = %+v", all)
	}
	third := all[0]
	for _, item := range all {
		if item.DedupeKey != bashKey && item.ID != items[0].ID {
			third = item
		}
	}
	if third.Status != NotifyDismissed {
		t.Fatalf("expired = %+v", third)
	}

	// 投递前发现挂起已不存在（如 Hub 重启）：queued 直接 dismissed。
	stale := &notificationModel{ID: newNotificationID(), DedupeKey: "approval:gone", Kind: NotifyKindApprovalPending, Status: NotifyQueued, NextAttemptAt: nullNow(notifyTime(f.now)), CreatedAt: notifyTime(f.now)}
	if _, err := f.capability.store.insertOpsNotification(t.Context(), stale, &notificationOpsModel{Title: "旧审批", PayloadJSON: "{}"}); err != nil {
		t.Fatal(err)
	}
	f.deliver()
	if got := f.notificationByID(stale.ID); got.Status != NotifyDismissed || len(f.wecom.received()) != 0 {
		t.Fatalf("stale = %+v sent=%v", got, f.wecom.received())
	}
}

func TestNotifyOpsApprovalSubagentAndSwitchOff(t *testing.T) {
	f, task, h := opsApprovalFixture(t, false, `"notify_approval":true`)
	child := &taskSubsessionModel{SessionID: "child-ops", TaskID: task.ID, ParentSessionID: task.InitialSessionID, ParentCallID: "parent-call",
		OracleID: "synthetic", AgentName: OracleExplorerName, Status: subsessionRunning, CreatedAt: nowRFC3339()}
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(child).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	done := startReview(t, h, t.Context(), child.SessionID, "child-1", "webapi_http", `{"method":"DELETE","url":"https://api.example.test/items/7"}`)
	pending := waitOpsPending(t, h, task.InitialSessionID)
	f.deliver()
	md := wecomMarkdowns(f.wecom)
	if len(md) != 1 || !strings.Contains(md[0], "(https://hub.example.test/sessions/"+task.InitialSessionID+")") || strings.Contains(md[0], "/sessions/child-ops") {
		t.Fatalf("child link = %v", md)
	}
	if views, _ := f.capability.store.notificationViews(t.Context(), notificationFilter{}, 10, 0); len(views) != 1 || views[0].SessionID != task.InitialSessionID {
		t.Fatalf("child view = %+v", views)
	}
	h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), true)
	<-done

	// 开关关闭 / 推送未启用：不登记。
	f.configure(`{"enabled":true,"require_confirm":false,"base_url":"","notify_approval":false}`)
	done = startReview(t, h, t.Context(), task.InitialSessionID, "off-1", "write", `{"path":"/tmp/a"}`)
	pending = waitOpsPending(t, h, task.InitialSessionID)
	h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), false)
	<-done
	f.configure(`{"enabled":false,"require_confirm":false,"base_url":"","notify_approval":true}`)
	done = startReview(t, h, t.Context(), task.InitialSessionID, "off-2", "write", `{"path":"/tmp/b"}`)
	pending = waitOpsPending(t, h, task.InitialSessionID)
	h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), false)
	<-done
	if items := opsNotifications(t, f.capability, NotifyKindApprovalPending); len(items) != 1 {
		t.Fatalf("switch off registered: %+v", items)
	}
}

func TestNotifyOpsScheduleFailed(t *testing.T) {
	f := newNotifyFixture(t)
	f.configure(notifySettingsBody(f.wecom.webhook(), true, "https://hub.example.test", `"notify_schedule_failed":true`))
	f.launcher.agents = nil
	f.createAsset("a1") // 发起失败：select_node（任务已建）
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.do("PUT", "/api/webapi/assets/a2", `{"name":"空白资产","origins":["https://empty.example.test"]}`) // validate 失败
	f.putSchedule("a2", `{"cron":"0 * * * *"}`)
	f.createAsset("a3") // cron 非法（库中损坏）
	f.putSchedule("a3", `{"cron":"0 * * * *"}`)
	f.createAsset("a4") // next_run_at 损坏
	f.putSchedule("a4", `{"cron":"0 * * * *"}`)
	f.createAsset("a5") // 归档 → skipped，不通知
	f.putSchedule("a5", `{"cron":"0 * * * *"}`)
	f.do("PUT", "/api/webapi/assets/a5", `{"name":"演示库存系统","origins":["http://example.test"],"archived":true}`)
	if _, err := f.capability.store.db().ExecContext(t.Context(), `UPDATE webapi_asset_schedules SET cron = 'bad cron' WHERE asset_id = 'a3'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.capability.store.db().ExecContext(t.Context(), `UPDATE webapi_asset_schedules SET next_run_at = '0000-broken' WHERE asset_id = 'a4'`); err != nil {
		t.Fatal(err)
	}
	f.setNow("2026-11-19T07:00:10Z")
	f.tick()
	items := opsNotifications(t, f.capability, NotifyKindScheduleFailed)
	if len(items) != 4 {
		t.Fatalf("schedule_failed = %+v", items)
	}
	byAsset := map[string]notificationModel{}
	for _, item := range items {
		ops, _ := f.capability.store.notificationOps(t.Context(), item.ID)
		run, _ := f.capability.store.scheduleRun(t.Context(), opsKeySuffix(item.DedupeKey, notifyKeyScheduleRun))
		if run == nil || run.Status != ScheduleRunFailed || item.Status != NotifyQueued || run.AssetID != ops.AssetID {
			t.Fatalf("item = %+v run = %+v", item, run)
		}
		byAsset[ops.AssetID] = item
	}
	if byAsset["a1"].TaskID != scheduleTaskID("a1", "2026-11-19T07:00:00Z") || byAsset["a2"].TaskID != "" {
		t.Fatalf("task ids = %+v", byAsset)
	}
	// 去重：同一 run 再登记、下一轮检查都不重复。
	if err := f.capability.registerScheduleFailed(t.Context(), "a1", "2026-11-19T07:00:00Z", "0 * * * *"); err != nil {
		t.Fatal(err)
	}
	f.setNow("2026-11-19T07:30:00Z")
	f.tick()
	if again := opsNotifications(t, f.capability, NotifyKindScheduleFailed); len(again) != 4 {
		t.Fatalf("dedupe = %d", len(again))
	}
	f.deliver()
	md := strings.Join(wecomMarkdowns(f.wecom), "\n=====\n")
	for _, want := range []string{"**【计划运行失败】演示库存系统**", "> 计划：0 * * * *（Hub 本地时区）", "> 应运行时间：2026-11-19 15:00", "> 失败阶段：select_node",
		"> 原因：select_node：", "> 失败阶段：validate", "**【计划运行失败】空白资产**", "> 原因：cron 非法：", "> 失败阶段：调度检查", "next_run_at 损坏，已按当前时间重算",
		"[在平台查看](https://hub.example.test/?page=assets)"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
	views, _ := f.capability.store.notificationViews(t.Context(), notificationFilter{ID: byAsset["a2"].ID}, 1, 0)
	if len(views) != 1 || views[0].Kind != NotifyKindScheduleFailed || views[0].Title != "空白资产" || views[0].Asset == nil || views[0].Asset.ID != "a2" || views[0].SessionID != "" || views[0].Status != NotifySent {
		t.Fatalf("view = %+v", views)
	}
	// 开关关闭不登记。
	f.configure(`{"enabled":true,"require_confirm":false,"base_url":"","notify_schedule_failed":false}`)
	f.setNow("2026-11-19T08:00:10Z")
	f.tick()
	if again := opsNotifications(t, f.capability, NotifyKindScheduleFailed); len(again) != 4 {
		t.Fatalf("switch off registered: %d", len(again))
	}
}

func TestNotifyOpsWeeklyDigest(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()
	f.mustTriage(ordersKey, `{"severity":"critical","due_date":"2026-11-18"}`) // 订单条目逾期
	wecom := newFakeWeCom(t)
	f.cap.notify.allowHTTP = true
	now := time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC) // 本地周四 11-19 12:00
	f.cap.scheduleNow = func() time.Time { return now }
	if status, raw := f.do("PUT", "/api/webapi/notify/wecom", notifySettingsBody(wecom.webhook(), true, "https://hub.example.test", `"notify_weekly_digest":true`)); status != 200 {
		t.Fatalf("configure = %d %s", status, raw)
	}
	register := func() {
		t.Helper()
		if err := f.cap.registerWeeklyDigest(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	// 启用时刻之前的周一 09:00（11-16）不补发；周一 08:59 仍未到点。
	register()
	now = time.Date(2026, 11, 23, 0, 59, 0, 0, time.UTC)
	register()
	if items := opsNotifications(t, f.cap, NotifyKindWeeklyDigest); len(items) != 0 {
		t.Fatalf("early digest = %+v", items)
	}
	// 周一 09:00（本地）登记 digest:2026-W48；再调用不重复。
	now = time.Date(2026, 11, 23, 1, 0, 0, 0, time.UTC)
	register()
	register()
	items := opsNotifications(t, f.cap, NotifyKindWeeklyDigest)
	if len(items) != 1 || items[0].DedupeKey != "digest:2026-W48" || items[0].Status != NotifyQueued || items[0].TaskID != "" {
		t.Fatalf("digest = %+v", items)
	}
	// 汇总与总览 days=7（同一时刻）口径一致。
	view, _ := f.overview("?days=7")
	ops, _ := f.cap.store.notificationOps(t.Context(), items[0].ID)
	var payload opsPayload
	if err := json.Unmarshal([]byte(ops.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	want := digestStat{Overdue: view.Vulns.Overdue, Unrated: view.Todo.Unrated}
	for _, day := range view.Vulns.ConfirmedByDay {
		want.New += day.New
		want.Fixed += day.Fixed
	}
	if *payload.Total != want || want.New == 0 || want.Fixed == 0 || want.Overdue == 0 || want.Unrated == 0 || payload.RangeStart != "2026-11-17" || payload.RangeEnd != "2026-11-23" {
		t.Fatalf("total = %+v want %+v (%s~%s)", payload.Total, want, payload.RangeStart, payload.RangeEnd)
	}
	sum := digestStat{}
	names := []string{}
	for _, row := range payload.Assets {
		sum.New, sum.Fixed, sum.Overdue, sum.Unrated = sum.New+row.Stat.New, sum.Fixed+row.Stat.Fixed, sum.Overdue+row.Stat.Overdue, sum.Unrated+row.Stat.Unrated
		names = append(names, row.Name)
	}
	if sum != want || strings.Join(names, ",") != "演示商城,未关联资产" {
		t.Fatalf("rows = %+v", payload.Assets)
	}
	// 投递：require_confirm=true 也直接发送。
	if err := f.cap.deliverNotifications(t.Context()); err != nil {
		t.Fatal(err)
	}
	md := wecomMarkdowns(wecom)
	if len(md) != 1 {
		t.Fatalf("sent = %v", md)
	}
	for _, line := range []string{"**【每周摘要】2026-W48**", "> 统计区间：2026-11-17 ~ 2026-11-23", "> 合计：" + digestLine(want), "- 演示商城：", "- 未关联资产：",
		"[在平台查看](https://hub.example.test/?page=overview)"} {
		if !strings.Contains(md[0], line) {
			t.Fatalf("markdown lacks %q:\n%s", line, md[0])
		}
	}
	if views, _ := f.cap.store.notificationViews(t.Context(), notificationFilter{ID: items[0].ID}, 1, 0); len(views) != 1 || views[0].Title != "每周摘要 2026-W48" || views[0].Asset != nil || views[0].Kind != NotifyKindWeeklyDigest {
		t.Fatalf("view = %+v", views)
	}
	// 停机错过 W49、W50：12-15（周二）只补最近一次 W51。
	now = time.Date(2026, 12, 15, 2, 0, 0, 0, time.UTC)
	register()
	keys := []string{}
	for _, item := range opsNotifications(t, f.cap, NotifyKindWeeklyDigest) {
		keys = append(keys, item.DedupeKey)
	}
	if strings.Join(keys, ",") != "digest:2026-W48,digest:2026-W51" {
		t.Fatalf("catch-up = %v", keys)
	}
}

func TestNotifyOpsWeeklyDigestNoAssets(t *testing.T) {
	f := newNotifyFixture(t)
	f.configure(notifySettingsBody(f.wecom.webhook(), false, "", `"notify_weekly_digest":true`))
	f.setNow("2026-11-23T01:00:00Z") // CST 周一 09:00
	if err := f.capability.registerWeeklyDigest(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.deliver()
	md := wecomMarkdowns(f.wecom)
	if len(md) != 1 || !strings.Contains(md[0], "> 合计：新增 0 · 已修复 0 · 当前逾期 0 · 待定级 0") || !strings.Contains(md[0], "暂无资产") || strings.Contains(md[0], "[在平台查看]") {
		t.Fatalf("empty digest = %v", md)
	}
}

func TestNotifyOpsDigestSlotAndTruncation(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	for at, want := range map[string]string{
		"2026-11-23T09:00:00+08:00": "2026-11-23 09:00", "2026-11-23T08:59:59+08:00": "2026-11-16 09:00",
		"2026-11-22T23:00:00+08:00": "2026-11-16 09:00", "2026-11-29T10:00:00+08:00": "2026-11-23 09:00",
	} {
		parsed, _ := time.Parse(time.RFC3339, at)
		if got := digestSlot(parsed, loc).Format("2006-01-02 15:04"); got != want {
			t.Fatalf("slot(%s) = %s want %s", at, got, want)
		}
	}
	if got := digestKey(time.Date(2027, 1, 4, 9, 0, 0, 0, loc)); got != "digest:2027-W01" {
		t.Fatalf("iso key = %s", got)
	}
	// 待审批/计划失败：字段统一截断，标题与链接保留。
	long := strings.Repeat("长", 3000)
	md := composeOpsMarkdown("**【计划运行失败】资产**", []opsField{{"> 原因：", long}, {"> 计划：", "0 * * * *"}}, []string{"[在平台查看](https://hub.example.test/?page=assets)"})
	if len(md) > wecomMarkdownLimit || !strings.HasPrefix(md, "**【计划运行失败】资产**") || !strings.HasSuffix(md, "[在平台查看](https://hub.example.test/?page=assets)") ||
		!strings.Contains(md, "…") || !strings.Contains(md, "> 计划：0 * * * *") {
		t.Fatalf("ops truncation len=%d:\n%s", len(md), md)
	}
	// 每周摘要：资产行过多时从末尾省略并注明。
	payload := opsPayload{Week: "2026-W48", RangeStart: "2026-11-17", RangeEnd: "2026-11-23", Total: &digestStat{New: 1}}
	for index := range 300 {
		payload.Assets = append(payload.Assets, digestRow{AssetID: fmt.Sprint(index), Name: fmt.Sprintf("演示资产%03d", index), Stat: digestStat{New: 1}})
	}
	md = composeDigestMarkdown(payload, "https://hub.example.test/?page=overview")
	if len(md) > wecomMarkdownLimit || !strings.Contains(md, "个资产有变化未列出") || !strings.HasSuffix(md, "[在平台查看](https://hub.example.test/?page=overview)") || !strings.Contains(md, "- 演示资产000：") {
		t.Fatalf("digest truncation len=%d", len(md))
	}
}

func TestNotifyOpsRetryDismiss(t *testing.T) {
	f := newNotifyFixture(t)
	f.configure(notifySettingsBody(f.wecom.webhook(), false, "", `"notify_schedule_failed":true`))
	f.launcher.agents = nil
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.setNow("2026-11-19T07:00:10Z")
	f.tick()
	item := opsNotifications(t, f.capability, NotifyKindScheduleFailed)[0]
	f.wecom.respond(93000, 93000, 93000)
	f.deliver()
	f.advance(time.Minute)
	f.deliver()
	f.advance(5 * time.Minute)
	f.deliver()
	if got := f.notificationByID(item.ID); got.Status != NotifyFailed || got.Attempts != 3 || !strings.HasPrefix(got.LastError, "markdown：errcode 93000") {
		t.Fatalf("failed = %+v", got)
	}
	if status, _, _ := f.do("POST", "/api/webapi/notifications/"+item.ID+"/confirm", ""); status != 409 {
		t.Fatalf("confirm = %d", status)
	}
	if status, value, raw := f.do("POST", "/api/webapi/notifications/"+item.ID+"/retry", ""); status != 200 || value["status"] != NotifyQueued || value["kind"] != NotifyKindScheduleFailed || value["title"] != "演示库存系统" {
		t.Fatalf("retry = %d %s", status, raw)
	}
	f.deliver()
	if got := f.notificationByID(item.ID); got.Status != NotifySent {
		t.Fatalf("retried = %+v", got)
	}
	var audits []auditEventModel
	if err := f.capability.store.orm.NewSelect().Model(&audits).Where("target_id = ?", item.ID).OrderExpr("at ASC, id ASC").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	actions := []string{}
	for _, audit := range audits {
		actions = append(actions, audit.Action+"|"+audit.Summary)
	}
	if strings.Join(actions, ",") != "notification.failed|推送失败：演示库存系统,notification.retry|重试推送：演示库存系统,notification.sent|推送成功：演示库存系统" {
		t.Fatalf("audits = %v", actions)
	}
	// failed → dismiss。
	f.setNow("2026-11-19T08:00:10Z")
	f.tick()
	next := opsNotifications(t, f.capability, NotifyKindScheduleFailed)[1]
	f.wecom.respond(93000, 93000, 93000)
	for _, d := range []time.Duration{0, time.Minute, 5 * time.Minute} {
		f.advance(d)
		f.deliver()
	}
	if status, value, _ := f.do("POST", "/api/webapi/notifications/"+next.ID+"/dismiss", ""); status != 200 || value["status"] != NotifyDismissed {
		t.Fatalf("dismiss = %d %v", status, value)
	}
}
