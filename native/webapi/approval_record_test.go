package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

// 审批记录（R2）测试：临时库 + 合成数据（example.test、虚构口令），不调用真实模型、不向真实地址发送。

// restart 模拟 Hub 重启：关闭当前 Capability（连接池），用同库同 state-dir 新建一个，
// 执行 Hub 启动时的遗留审批清理。旧 Capability 内存里的挂起调用不再可见（真实重启时随进程消失）。
func (f *notifyFixture) restart() {
	f.t.Helper()
	old := f.capability
	_ = old.Close(context.Background())
	next, err := NewCapability(f.database.URL, f.database.StateDir)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = next.Close(context.Background()) })
	next.launcher = f.launcher
	next.notify.allowHTTP = true
	next.scheduleLoc, next.scheduleNow = old.scheduleLoc, old.scheduleNow
	mux := http.NewServeMux()
	for _, route := range next.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	if err := next.recoverInterruptedApprovals(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	f.capability, f.mux = next, mux
}

func approvalRowsOf(t *testing.T, c *Capability) []approvalModel {
	t.Helper()
	rows, err := readValue(t.Context(), c.store, func(ctx context.Context, db bun.IDB) ([]approvalModel, error) {
		var rows []approvalModel
		err := db.NewSelect().Model(&rows).OrderExpr("created_at ASC, id ASC").Scan(ctx)
		return rows, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

type approvalListBody struct {
	Items        []ApprovalView `json:"items"`
	PendingCount int            `json:"pending_count"`
	HasMore      bool           `json:"has_more"`
	NextCursor   *string        `json:"next_cursor"`
}

func (f *notifyFixture) approvalList(path string) (int, approvalListBody) {
	f.t.Helper()
	status, _, raw := f.do("GET", path, "")
	var body approvalListBody
	if status == 200 {
		if err := json.Unmarshal([]byte(raw), &body); err != nil {
			f.t.Fatalf("%s: %v %s", path, err, raw)
		}
	}
	return status, body
}

// 挂起 → pending 行；批准/拒绝/超时/取消各写终态与决定人；批准/拒绝/超时各一条审计（同事务）、取消不登记；
// 摘要按资产测试账号回替占位符并遮盖；列表接口标出可操作项。
func TestApprovalRecordLifecycle(t *testing.T) {
	f, task, h := opsApprovalFixture(t, false, `"notify_approval":false`)
	c := f.capability
	command := "curl -u admin:hunter2 https://api.example.test/x?pw=alice-pass-synthetic " + strings.Repeat("b", 300)
	args := mustToolArgumentsJSON(t, map[string]any{"command": command})

	before := time.Now().UTC()
	done := startReview(t, h, t.Context(), task.InitialSessionID, "approve-1", "bash", args)
	pending := waitOpsPending(t, h, task.InitialSessionID)
	opID := pending.GetOperation().GetOperationId()
	row := approvalByOperation(t, c, opID)
	if row.Status != approvalStatusPending || row.DecidedBy != "" || row.DecidedAt.Valid || row.StatusReason != "" || row.TaskID != "t1" ||
		row.SessionID != task.InitialSessionID || row.ParentSessionID != "" || row.TurnID != "ops-turn" || row.CallID != "approve-1" || row.Tool != "bash" ||
		row.HubInstance != c.instance || !strings.Contains(row.Reason, "全部人工审批：终端命令") {
		t.Fatalf("pending row = %+v", row)
	}
	if strings.Contains(row.Summary, "hunter2") || strings.Contains(row.Summary, "alice-pass-synthetic") || !strings.Contains(row.Summary, "{{cred:alice.secret}}") ||
		!strings.HasPrefix(row.Summary, "curl -u [REDACTED]") || len([]rune(row.Summary)) != approvalSummaryRunes {
		t.Fatalf("summary = %q", row.Summary)
	}
	created, err1 := time.Parse(notifyTimeLayout, row.CreatedAt)
	expires, err2 := time.Parse(notifyTimeLayout, row.ExpiresAt)
	if err1 != nil || err2 != nil || created.Before(before.Truncate(time.Millisecond)) || expires.Sub(created) < ReviewTimeout-time.Second || expires.Sub(created) > ReviewTimeout+time.Second {
		t.Fatalf("times created=%s expires=%s", row.CreatedAt, row.ExpiresAt)
	}
	if status, body := f.approvalList("/api/webapi/tasks/t1/approvals"); status != 200 || body.PendingCount != 1 || len(body.Items) != 1 ||
		!body.Items[0].Actionable || body.Items[0].ReviewSessionID != task.InitialSessionID || body.Items[0].DecidedAt != nil || body.Items[0].OperationID != opID {
		t.Fatalf("list = %d %+v", status, body)
	}
	h.resolve(t, task.InitialSessionID, opID, true)
	<-done

	decide := func(callID string, act func(opID string, cancel context.CancelFunc)) string {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := startReview(t, h, ctx, task.InitialSessionID, callID, "write", `{"path":"/tmp/synthetic.txt"}`)
		id := waitOpsPending(t, h, task.InitialSessionID).GetOperation().GetOperationId()
		act(id, cancel)
		<-done
		return id
	}
	rejectID := decide("reject-1", func(id string, _ context.CancelFunc) { h.resolve(t, task.InitialSessionID, id, false) })
	c.approvals.timeout = 400 * time.Millisecond
	expireID := decide("expire-1", func(string, context.CancelFunc) {})
	c.approvals.timeout = ReviewTimeout
	cancelID := decide("cancel-1", func(_ string, cancel context.CancelFunc) { cancel() })

	for _, want := range []struct{ id, status, by, reason, action string }{
		{opID, approvalStatusApproved, auditActorUser, "", "approval.approve"},
		{rejectID, approvalStatusRejected, auditActorUser, "", "approval.deny"},
		{expireID, approvalStatusExpired, auditActorSystem, approvalExpiredReason, "approval.expire"},
		{cancelID, approvalStatusCanceled, auditActorSystem, approvalCanceledReason, ""},
	} {
		row := approvalByOperation(t, c, want.id)
		if row.Status != want.status || row.DecidedBy != want.by || row.StatusReason != want.reason || !row.DecidedAt.Valid || row.DecidedAt.String < row.CreatedAt {
			t.Fatalf("%s row = %+v", want.status, row)
		}
		if want.action == "" {
			continue
		}
		audit := requireAudit(t, c, want.action, 1)
		detail := auditDetail(t, audit)
		if audit.TargetID != want.id || audit.TaskID != "t1" || audit.AssetID != "a1" || detail["approval_id"] != row.ID || detail["operation"] != row.Summary || audit.Actor != want.by {
			t.Fatalf("%s audit = %+v %v", want.action, audit, detail)
		}
	}
	approvalAudits := 0
	for _, row := range auditRows(t, c) {
		if strings.HasPrefix(row.Action, "approval.") {
			approvalAudits++
		}
	}
	if approvalAudits != 3 {
		t.Fatalf("approval audits = %d", approvalAudits)
	}
	assertNoSecrets(t, c, "hunter2", "alice-pass-synthetic")
	if found, err := databaseContains(t.Context(), c.store, "hunter2"); err != nil || found {
		t.Fatalf("库中出现命令口令：%v %v", found, err)
	}
	status, body := f.approvalList("/api/webapi/tasks/t1/approvals?status=" + approvalStatusCanceled)
	if status != 200 || body.PendingCount != 0 || len(body.Items) != 1 || body.Items[0].Status != "canceled" || body.Items[0].Actionable || body.Items[0].DecidedBy != auditActorSystem || body.Items[0].DecidedAt == nil {
		t.Fatalf("canceled list = %d %+v", status, body)
	}
}

// 挂起时落库失败（无 pending 行）的终态仍整行写入；已是终态的行不被覆盖、不重复审计。
func TestApprovalFinishUpsertsAndKeepsTerminal(t *testing.T) {
	f := newNotifyFixture(t)
	c := f.capability
	f.plainTask("t9")
	at := notifyTime(time.Now())
	final := approvalModel{ID: newApprovalID(), OperationID: "op-lost", TaskID: "t9", SessionID: "s9", Tool: "write", Summary: "/tmp/x", Reason: "合成",
		Status: approvalStatusApproved, DecidedBy: auditActorUser, HubInstance: c.instance, CreatedAt: at, ExpiresAt: at, DecidedAt: sql.NullString{String: at, Valid: true}}
	audit, err := newAuditModel(*reviewAuditEntry(&final, ""), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := c.store.finishApproval(t.Context(), final, audit); err != nil || !changed {
		t.Fatalf("upsert = %v %v", changed, err)
	}
	again := final
	again.Status, again.DecidedBy = approvalStatusCanceled, auditActorSystem
	if changed, err := c.store.finishApproval(t.Context(), again, audit); err != nil || changed {
		t.Fatalf("terminal overwritten = %v %v", changed, err)
	}
	rows := approvalRowsOf(t, c)
	if len(rows) != 1 || rows[0].Status != approvalStatusApproved || rows[0].TaskID != "t9" {
		t.Fatalf("rows = %+v", rows)
	}
	requireAudit(t, c, "approval.approve", 1)
}

// 模拟重启：上次进程遗留的 pending → canceled（system，“Hub 重启，调用已中断”）并写 approval.cancel 审计；
// 未登记的待登记与已登记未发出的 approval_pending 通知都不再投递；本进程新挂起的审批不受清理影响；清理幂等。
func TestApprovalRecordRestartCancelsAndStopsNotifications(t *testing.T) {
	f, task, h := opsApprovalFixture(t, false, `"notify_approval":true`)
	ctxA, cancelA := context.WithCancel(t.Context())
	defer cancelA()
	doneA := startReview(t, h, ctxA, task.InitialSessionID, "restart-a", "write", `{"path":"/tmp/a.txt"}`)
	opA := waitOpsPending(t, h, task.InitialSessionID).GetOperation().GetOperationId()
	// B：已登记为 queued（未发出）；A：只有待登记行。
	ctxB, cancelB := context.WithCancel(t.Context())
	defer cancelB()
	doneB := startReview(t, h, ctxB, task.InitialSessionID, "restart-b", "write", `{"path":"/tmp/b.txt"}`)
	var opB string
	deadline := time.Now().Add(5 * time.Second)
	for opB == "" && time.Now().Before(deadline) {
		for _, row := range approvalRowsOf(t, f.capability) {
			if row.OperationID != opA {
				opB = row.OperationID
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if opB == "" {
		t.Fatal("B 未落库")
	}
	if err := f.capability.registerApprovalPending(t.Context(), approvalByOperation(t, f.capability, opB).ID); err != nil {
		t.Fatal(err)
	}
	if err := f.capability.store.deleteRegistration(t.Context(), notifyKeyApproval+opB); err != nil {
		t.Fatal(err)
	}
	if items := f.notifications(); len(items) != 1 || items[0].DedupeKey != notifyKeyApproval+opB || items[0].Status != NotifyQueued {
		t.Fatalf("restart 前通知 = %+v", items)
	}
	if job := registrationRow(t, f.capability, notifyKeyApproval+opA); job == nil || pendingOpsCount(t, f.capability) != 1 {
		t.Fatalf("restart 前待登记 = %+v / %d", job, pendingOpsCount(t, f.capability))
	}

	oldInstance := f.capability.instance
	f.restart()
	c := f.capability
	// 旧进程的调用随“进程退出”结束（其终态写库因连接池已关闭失败，不影响新进程的记录）。
	cancelA()
	cancelB()
	<-doneA
	<-doneB

	rows := approvalRowsOf(t, c)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, row := range rows {
		if row.Status != approvalStatusCanceled || row.DecidedBy != auditActorSystem || row.StatusReason != approvalRestartReason || !row.DecidedAt.Valid || row.HubInstance != oldInstance {
			t.Fatalf("restart row = %+v", row)
		}
	}
	cancels := auditByAction(auditRows(t, c), "approval.cancel")
	if len(cancels) != 2 {
		t.Fatalf("cancel audits = %+v", cancels)
	}
	for _, audit := range cancels {
		if audit.Actor != auditActorSystem || audit.TaskID != "t1" || audit.AssetID != "a1" || !strings.Contains(audit.Summary, approvalRestartReason) {
			t.Fatalf("cancel audit = %+v", audit)
		}
	}
	if pendingOpsCount(t, c) != 0 {
		t.Fatalf("重启后仍有待登记 %d", pendingOpsCount(t, c))
	}
	retryOpsNow(t, c)
	f.deliver()
	if len(f.wecom.received()) != 0 {
		t.Fatalf("重启后仍投递了待审批通知：%v", f.wecom.received())
	}
	items := opsNotifications(t, c, NotifyKindApprovalPending)
	if len(items) != 1 || items[0].DedupeKey != notifyKeyApproval+opB || items[0].Status != NotifyDismissed {
		t.Fatalf("notifications = %+v", items)
	}
	if status, body := f.approvalList("/api/webapi/approvals?task_id=t1"); status != 200 || body.PendingCount != 0 || len(body.Items) != 2 || body.Items[0].Actionable {
		t.Fatalf("list after restart = %d %+v", status, body)
	}

	// 新进程自己的挂起不被再次清理；清理幂等（不重复审计）。
	h2 := newApprovalHarness(t, c)
	doneC := startReview(t, h2, t.Context(), task.InitialSessionID, "restart-c", "write", `{"path":"/tmp/c.txt"}`)
	opC := waitOpsPending(t, h2, task.InitialSessionID).GetOperation().GetOperationId()
	if err := c.recoverInterruptedApprovals(t.Context()); err != nil {
		t.Fatal(err)
	}
	if row := approvalByOperation(t, c, opC); row.Status != approvalStatusPending {
		t.Fatalf("current instance row canceled: %+v", row)
	}
	if n := len(auditByAction(auditRows(t, c), "approval.cancel")); n != 2 {
		t.Fatalf("cancel audits after second recovery = %d", n)
	}
	f.deliver()
	if md := wecomMarkdowns(f.wecom); len(md) != 1 || !strings.Contains(md[0], "/tmp/c.txt") {
		t.Fatalf("新挂起应正常投递一次：%v", md)
	}
	h2.resolve(t, task.InitialSessionID, opC, true)
	<-doneC
	if row := approvalByOperation(t, c, opC); row.Status != approvalStatusApproved {
		t.Fatalf("C = %+v", row)
	}
}

// 列表/计数按任务精确过滤（不串任务）；session_id 含经父会话处理的子 Agent 审批；分页、状态过滤与参数校验。
func TestApprovalListFiltersByTask(t *testing.T) {
	f, task, h := opsApprovalFixture(t, false, `"notify_approval":false`)
	c := f.capability
	f.plainTask("t2")
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t2/policy", `{"scope":"origins","approval":"all"}`); status != 200 {
		t.Fatalf("policy = %d %s", status, raw)
	}
	other, err := c.store.Get(t.Context(), "t2")
	if err != nil || other == nil {
		t.Fatal(err)
	}
	child := &taskSubsessionModel{SessionID: "child-list", TaskID: task.ID, ParentSessionID: task.InitialSessionID, ParentCallID: "parent-call",
		OracleID: "synthetic", AgentName: OracleExplorerName, Status: subsessionRunning, CreatedAt: nowRFC3339()}
	if err := c.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(child).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	start := func(sessionID, callID string) (string, chan error) {
		done := startReview(t, h, t.Context(), sessionID, callID, "write", `{"path":"/tmp/`+callID+`"}`)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, row := range approvalRowsOf(t, c) {
				if row.CallID == callID {
					return row.OperationID, done
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("%s 未落库", callID)
		return "", nil
	}
	op1, done1 := start(task.InitialSessionID, "t1-main")
	op2, done2 := start(other.InitialSessionID, "t2-main")
	op3, done3 := start(child.SessionID, "t1-child")

	ids := func(body approvalListBody) map[string]bool {
		out := map[string]bool{}
		for _, item := range body.Items {
			out[item.OperationID] = true
		}
		return out
	}
	check := func(path string, pending int, want ...string) approvalListBody {
		t.Helper()
		status, body := f.approvalList(path)
		got := ids(body)
		if status != 200 || body.PendingCount != pending || len(got) != len(want) {
			t.Fatalf("%s = %d pending=%d items=%v want %v", path, status, body.PendingCount, got, want)
		}
		for _, id := range want {
			if !got[id] {
				t.Fatalf("%s 缺 %s：%v", path, id, got)
			}
		}
		return body
	}
	check("/api/webapi/tasks/t1/approvals", 2, op1, op3)
	check("/api/webapi/tasks/t2/approvals", 1, op2)
	check("/api/webapi/approvals?task_id=t2", 1, op2)
	body := check("/api/webapi/approvals?session_id="+task.InitialSessionID, 2, op1, op3)
	for _, item := range body.Items {
		if item.OperationID == op3 && (item.SessionID != child.SessionID || item.ParentSessionID != task.InitialSessionID || item.ReviewSessionID != task.InitialSessionID || item.TaskID != "t1") {
			t.Fatalf("child view = %+v", item)
		}
	}
	check("/api/webapi/approvals?session_id="+child.SessionID, 1, op3)
	check("/api/webapi/tasks/t2/approvals?session_id="+task.InitialSessionID, 0)
	check("/api/webapi/approvals", 3, op1, op2, op3)

	h.resolve(t, other.InitialSessionID, op2, false)
	<-done2
	check("/api/webapi/tasks/t2/approvals?status=pending", 0)
	check("/api/webapi/tasks/t2/approvals?status=rejected", 0, op2)
	check("/api/webapi/approvals", 2, op1, op2, op3)

	// 分页：limit=1 逐页取完，倒序且不重复。
	seen := map[string]bool{}
	path := "/api/webapi/approvals?limit=1"
	for page := 0; page < 5; page++ {
		status, body := f.approvalList(path)
		if status != 200 || len(body.Items) != 1 || body.PendingCount != 2 {
			t.Fatalf("page %d = %d %+v", page, status, body)
		}
		if seen[body.Items[0].ID] {
			t.Fatalf("重复项 %s", body.Items[0].ID)
		}
		seen[body.Items[0].ID] = true
		if !body.HasMore {
			break
		}
		path = "/api/webapi/approvals?limit=1&cursor=" + *body.NextCursor
	}
	if len(seen) != 3 {
		t.Fatalf("分页取到 %d 项", len(seen))
	}

	for _, bad := range []string{"/api/webapi/approvals?status=cancelled", "/api/webapi/approvals?task_id=a/b", "/api/webapi/approvals?session_id=%20x",
		"/api/webapi/approvals?cursor=***", "/api/webapi/approvals?limit=0", "/api/webapi/approvals?limit=201"} {
		if status, _, raw := f.do("GET", bad, ""); status != 400 {
			t.Fatalf("%s = %d %s", bad, status, raw)
		}
	}
	if status, _, _ := f.do("GET", "/api/webapi/tasks/missing/approvals", ""); status != 404 {
		t.Fatalf("missing task = %d", status)
	}
	h.resolve(t, task.InitialSessionID, op1, true)
	h.resolve(t, task.InitialSessionID, op3, true)
	<-done1
	<-done3
	check("/api/webapi/tasks/t1/approvals?status=approved", 0, op1, op3)
}

// 运营登记：计划失败的待登记在“登记失败 → 重启 → 恢复”后恰好登记、投递一次。
func TestNotifyOpsRegistrationSurvivesRestart(t *testing.T) {
	f := newNotifyFixture(t)
	f.configure(notifySettingsBody(f.wecom.webhook(), false, "", `"notify_schedule_failed":true`))
	f.launcher.agents = nil
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.setNow("2026-11-19T07:00:10Z")
	f.tick() // 计划运行失败：运行记录与待登记同事务落库
	if pendingOpsCount(t, f.capability) != 1 {
		t.Fatalf("待登记 = %d", pendingOpsCount(t, f.capability))
	}
	// 登记尝试在等写锁时超时 → 失败、保留。
	release := holdWebAPIWriteLock(t, f.database)
	attempt, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	f.capability.retryOpsRegistrations(attempt)
	cancel()
	release()
	if pendingOpsCount(t, f.capability) != 1 {
		t.Fatal("失败的待登记丢失")
	}
	f.restart()
	c := f.capability
	if pendingOpsCount(t, c) != 1 {
		t.Fatal("重启后待登记丢失")
	}
	// 新进程的投递 goroutine 启动即处理一轮：登记并投递。
	c.notify.interval = 20 * time.Millisecond
	c.startNotifier()
	deadline := time.Now().Add(10 * time.Second)
	for {
		items := opsNotifications(t, c, NotifyKindScheduleFailed)
		if len(items) == 1 && items[0].Status == NotifySent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("重启后未投递：%+v", items)
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond) // 再跑几轮，确认不重复
	c.stopNotifier()
	if got := len(f.wecom.received()); got != 1 || pendingOpsCount(t, c) != 0 {
		t.Fatalf("投递 %d 次，待登记 %d", got, pendingOpsCount(t, c))
	}
	if md := wecomMarkdowns(f.wecom); !strings.Contains(md[0], "【计划运行失败】") {
		t.Fatalf("markdown = %v", md)
	}
}
