package webapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/exts/guardrail"
)

// 运营通知登记与 webapi 全局写锁：写事务开头取全局 advisory lock 排队，验证以下
// 业务断言：持锁期间 PENDING 事件不被阻塞（审批记录等锁后落库）、登记等锁后恰好一次、
// 审批已决不迟到补登记、短尝试超时保留重试（待登记在表中）。另一写者用独立连接持有同一把锁模拟。

// holdWebAPIWriteLock 在独立连接池上开事务并持有全局写锁，返回释放函数（回滚事务）。
func holdWebAPIWriteLock(t *testing.T, hub testHub) func() {
	t.Helper()
	db := hub.Open(t, "webapi-lock-holder")
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, webapiWriteLock); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			released = true
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Cleanup(release)
	return release
}

// waitWriteLockWaiter 等到库里出现一个正在排队等待 advisory lock 的会话（登记已开始并在等锁）。
func waitWriteLockWaiter(t *testing.T, hub testHub) {
	t.Helper()
	db := hub.Open(t, "webapi-lock-observer")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := db.NewRaw(`SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(t.Context(), &waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("registration did not start waiting for the webapi write lock")
}

func waitOpsEvent(t *testing.T, h *approvalHarness) *guardrail.Review {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, review := range h.reviews {
			if review.State == guardrail.ReviewState_REVIEW_STATE_PENDING {
				h.mu.Unlock()
				return review
			}
		}
		h.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("登记写锁不得阻止 PENDING 事件超过 1s")
	return nil
}

// waitOpsPending 等到同 operation 的 PENDING 事件已发出、且审批记录（与待登记）已落库，
// fixture 才能显式推进登记。PENDING 先于落库发出，所以两者都要等。
func waitOpsPending(t *testing.T, h *approvalHarness, sessionID string) *guardrail.Review {
	t.Helper()
	pending := h.waitPending(t, sessionID)
	operationID := pending.GetOperation().GetOperationId()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		event := false
		h.mu.Lock()
		for _, review := range h.reviews {
			if review.GetOperation().GetOperationId() == operationID && review.State == guardrail.ReviewState_REVIEW_STATE_PENDING {
				event = true
			}
		}
		h.mu.Unlock()
		if event && approvalByOperation(t, h.capability, operationID) != nil {
			return pending
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("审批 PENDING 事件或审批记录未就绪")
	return nil
}

// approvalByOperation 读审批记录（不存在为 nil）。
func approvalByOperation(t *testing.T, c *Capability, operationID string) *approvalModel {
	t.Helper()
	row, err := readOptional[approvalModel](t.Context(), c.store, "operation_id = ?", operationID)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// retryOpsNow 让所有待登记与每周摘要立即到期。
func retryOpsNow(t *testing.T, c *Capability) {
	t.Helper()
	if err := c.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*notifyRegistrationModel)(nil)).Set("next_attempt_at = ''").Where("1 = 1").Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c.notify.opsMu.Lock()
	c.notify.digestNext = time.Time{}
	c.notify.opsMu.Unlock()
}

func pendingOpsCount(t *testing.T, c *Capability) int {
	t.Helper()
	n, err := c.store.registrationCount(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func registrationRow(t *testing.T, c *Capability, key string) *notifyRegistrationModel {
	t.Helper()
	row, err := readOptional[notifyRegistrationModel](t.Context(), c.store, "key = ?", key)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// 登记在写事务开头等全局写锁；锁释放后一次插入内容与通知，重复登记不再插入。
func TestNotifyOpsRegistrationWaitsForWriteLock(t *testing.T) {
	f := newNotifyFixture(t)
	f.enable(false, "")
	release := holdWebAPIWriteLock(t, f.database)
	registered := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := f.capability.registerOps(t.Context(), NotifyKindScheduleFailed, "schedule_run:lock-test", "", &notificationOpsModel{Title: "合成计划"}, opsPayload{Error: "合成失败"})
		registered <- err
	}()
	waitWriteLockWaiter(t, f.database)
	select {
	case err := <-registered:
		t.Fatalf("锁未释放登记已返回：%v", err)
	case <-time.After(300 * time.Millisecond):
	}
	release()
	if err := <-registered; err != nil {
		t.Fatal(err)
	}
	t.Logf("另一连接持全局写锁，释放后登记成功：%v", time.Since(started))
	inserted, err := f.capability.registerOps(t.Context(), NotifyKindScheduleFailed, "schedule_run:lock-test", "", &notificationOpsModel{Title: "合成计划"}, opsPayload{})
	if err != nil || inserted || len(opsNotifications(t, f.capability, NotifyKindScheduleFailed)) != 1 {
		t.Fatalf("重复登记：inserted=%v err=%v", inserted, err)
	}
}

// 持写锁期间审批 PENDING 事件不等锁；审批记录与待登记等锁后落库，后台登记恰好一次、投递一次，重复登记不新增。
func TestNotifyOpsApprovalUnderWriteLockRegistersOnce(t *testing.T) {
	f, task, h := opsApprovalFixture(t, false, `"notify_approval":true`)
	c := f.capability
	c.notify.interval = 20 * time.Millisecond
	release := holdWebAPIWriteLock(t, f.database)
	started := time.Now()
	done := startReview(t, h, t.Context(), task.InitialSessionID, "busy", "write", `{"path":"/tmp/synthetic.txt"}`)
	pending := waitOpsEvent(t, h)
	t.Logf("持写锁期间审批 PENDING 事件耗时：%v", time.Since(started))
	waitWriteLockWaiter(t, f.database)
	// 投递 goroutine 启动时的 resetSendingNotifications 同样排在锁后。
	c.startNotifier()
	time.Sleep(300 * time.Millisecond)
	if items, err := sentOpsCount(t.Context(), c); err != nil || items != 0 {
		t.Fatalf("持锁期间不应登记完成：%d %v", items, err)
	}
	if row := approvalByOperation(t, c, pending.GetOperation().GetOperationId()); row != nil {
		t.Fatalf("持锁期间审批记录不应已提交：%+v", row)
	}
	release()
	deadline := time.Now().Add(15 * time.Second)
	for {
		// webhook 收到请求不等于发送终态已落库；终态提交后再停 worker，
		// 避免测试的 stop 恰好取消响应，把本次发送恢复为 queued。
		sent, err := sentOpsCount(t.Context(), c)
		if err != nil {
			t.Fatal(err)
		}
		if sent == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("释放写锁后未提交 sent 终态")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(f.wecom.received()); got != 1 {
		t.Fatalf("投递条数 = %d", got)
	}
	c.stopNotifier()
	items := opsNotifications(t, c, NotifyKindApprovalPending)
	if len(items) != 1 || items[0].Status != NotifySent || pendingOpsCount(t, c) != 0 {
		t.Fatalf("登记 = %+v pending=%d", items, pendingOpsCount(t, c))
	}
	record := approvalByOperation(t, c, pending.GetOperation().GetOperationId())
	if record == nil || record.Status != approvalStatusPending {
		t.Fatalf("审批记录 = %+v", record)
	}
	if err := c.registerApprovalPending(t.Context(), record.ID); err != nil {
		t.Fatal(err)
	}
	if items := opsNotifications(t, c, NotifyKindApprovalPending); len(items) != 1 {
		t.Fatalf("重复登记 = %+v", items)
	}
	if !h.resolve(t, task.InitialSessionID, pending.Operation.OperationId, false) {
		t.Fatal("resolve failed")
	}
	<-done
	if record := approvalByOperation(t, c, pending.GetOperation().GetOperationId()); record.Status != approvalStatusRejected || record.DecidedBy != auditActorUser {
		t.Fatalf("拒绝后记录 = %+v", record)
	}
	t.Logf("持锁后恰好登记/投递一次，总耗时 %v", time.Since(started))
}

func sentOpsCount(ctx context.Context, c *Capability) (int64, error) {
	return readValue(ctx, c.store, func(ctx context.Context, db bun.IDB) (int64, error) {
		count, err := db.NewSelect().Model((*notificationModel)(nil)).
			Where("kind = ? AND status = ?", NotifyKindApprovalPending, NotifySent).Count(ctx)
		return int64(count), err
	})
}

// 登记在等全局写锁期间审批已决（批准/拒绝/取消/超时）：锁释放后也不能补插。
func TestNotifyOpsApprovalResolvedDuringWriteLock(t *testing.T) {
	for _, outcome := range []string{"approved", "rejected", "canceled", "expired"} {
		t.Run(outcome, func(t *testing.T) {
			f, task, h := opsApprovalFixture(t, false, `"notify_approval":true`)
			c := f.capability
			if outcome == "expired" {
				c.approvals.timeout = 1500 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := startReview(t, h, ctx, task.InitialSessionID, "resolved-busy", "write", `{"path":"/tmp/synthetic.txt"}`)
			pending := waitOpsPending(t, h, task.InitialSessionID)
			if pendingOpsCount(t, c) != 1 {
				t.Fatalf("待登记 = %d", pendingOpsCount(t, c))
			}
			release := holdWebAPIWriteLock(t, f.database)
			registerDone := make(chan struct{})
			go func() { c.retryOpsRegistrations(t.Context()); close(registerDone) }()
			// 保证尝试已开始并正在等待全局写锁。
			waitWriteLockWaiter(t, f.database)
			switch outcome {
			case "approved", "rejected":
				if err := c.approvals.Resolve(task.InitialSessionID, pending.Operation.OperationId, outcome == "approved", "test"); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			case "expired":
				time.Sleep(time.Until(pending.GetExpiresAt().AsTime()) + 100*time.Millisecond)
			}
			// 锁释放前已决，释放后也不能补插。
			release()
			<-registerDone
			<-done
			retryOpsNow(t, c)
			if rows := opsNotifications(t, c, NotifyKindApprovalPending); len(rows) != 0 {
				t.Fatalf("已决审批被补登记：%+v", rows)
			}
			f.deliver()
			if len(f.wecom.received()) != 0 || pendingOpsCount(t, c) != 0 {
				t.Fatal("已决审批仍被投递或保留待登记")
			}
			want := map[string]string{"approved": approvalStatusApproved, "rejected": approvalStatusRejected, "canceled": approvalStatusCancelled, "expired": approvalStatusExpired}[outcome]
			if record := approvalByOperation(t, c, pending.GetOperation().GetOperationId()); record == nil || record.Status != want {
				t.Fatalf("审批记录 = %+v，期望 %s", record, want)
			}
		})
	}
}

// 短尝试上下文在等锁时到期：待登记保留在表中；锁释放后重试恰好登记、投递一次。
func TestNotifyOpsScheduleAndDigestLockRetry(t *testing.T) {
	for _, kind := range []string{NotifyKindScheduleFailed, NotifyKindWeeklyDigest} {
		t.Run(kind, func(t *testing.T) {
			f := newNotifyFixture(t)
			c := f.capability
			f.configure(notifySettingsBody(f.wecom.webhook(), false, "", `"notify_schedule_failed":true,"notify_weekly_digest":true`))
			f.createAsset("a1")
			run := &scheduleRunModel{ID: "synthetic-failed-run", AssetID: "a1", ScheduledFor: "2026-11-19T07:00:00Z", Status: ScheduleRunFailed, Error: "select_node：合成失败", CreatedAt: nowRFC3339()}
			if err := c.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
				if _, err := tx.NewInsert().Model(run).Exec(ctx); err != nil {
					return err
				}
				if kind != NotifyKindScheduleFailed {
					return nil
				}
				// 同键入队两次只有一行。
				for range 2 {
					if err := enqueueRegistrationTx(ctx, tx, scheduleFailedRegistration("a1", run.ScheduledFor, "0 * * * *")); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			f.setNow("2026-11-23T01:00:00Z")
			release := holdWebAPIWriteLock(t, f.database)
			attempt, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			if kind == NotifyKindScheduleFailed {
				c.retryOpsRegistrations(attempt)
			} else {
				c.tickWeeklyDigest(attempt)
			}
			cancel()
			wantPending := map[string]int{NotifyKindScheduleFailed: 1, NotifyKindWeeklyDigest: 0}[kind]
			if pendingOpsCount(t, c) != wantPending {
				t.Fatalf("失败后待登记数 = %d", pendingOpsCount(t, c))
			}
			release()
			if items := opsNotifications(t, c, kind); kind == NotifyKindWeeklyDigest && len(items) != 0 {
				t.Fatalf("摘要不应经待登记表：%+v", items)
			}
			retryOpsNow(t, c)
			if kind == NotifyKindWeeklyDigest {
				c.tickWeeklyDigest(t.Context())
			}
			items := opsNotifications(t, c, kind)
			if len(items) != 1 || pendingOpsCount(t, c) != 0 {
				t.Fatalf("恢复登记 = %+v, pending=%d", items, pendingOpsCount(t, c))
			}
			f.deliver()
			if len(f.wecom.received()) != 1 {
				t.Fatal("恢复后未投递一次")
			}
		})
	}
}

// 停止投递 goroutine 打断进行中的登记：待登记保留在表中（不当作成功删除、不计失败次数）。
func TestNotifyOpsRegistrationStopsWithNotifier(t *testing.T) {
	f := newNotifyFixture(t)
	c := f.capability
	f.configure(notifySettingsBody(f.wecom.webhook(), false, "", `"notify_schedule_failed":true`))
	f.createAsset("a1")
	if err := c.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&scheduleRunModel{ID: "stop-run", AssetID: "a1", ScheduledFor: "2026-11-19T07:00:00Z", Status: ScheduleRunFailed, Error: "合成失败", CreatedAt: nowRFC3339()}).Exec(ctx); err != nil {
			return err
		}
		return enqueueRegistrationTx(ctx, tx, scheduleFailedRegistration("a1", "2026-11-19T07:00:00Z", "0 * * * *"))
	}); err != nil {
		t.Fatal(err)
	}
	release := holdWebAPIWriteLock(t, f.database)
	c.startNotifier()
	waitWriteLockWaiter(t, f.database)
	stopped := make(chan struct{})
	go func() { c.stopNotifier(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("registration did not stop with notifier")
	}
	release()
	job := registrationRow(t, c, notifyKeyScheduleRun+"a1/2026-11-19T07:00:00Z")
	if job == nil || job.Attempts != 0 {
		t.Fatalf("停止时不能把未登记项当作成功移除或计失败：%+v", job)
	}
}

func TestNotifyOpsRegistrationFailureIsRetainedAndBounded(t *testing.T) {
	f := newNotifyFixture(t)
	c := f.capability
	now := notifyTime(time.Now())
	if err := c.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		return enqueueRegistrationTx(ctx, tx, &notifyRegistrationModel{Key: "synthetic-error", Kind: "synthetic", ArgsJSON: "{}", NextAttemptAt: now, CreatedAt: now})
	}); err != nil {
		t.Fatal(err)
	}
	c.retryOpsRegistrations(t.Context())
	c.retryOpsRegistrations(t.Context())
	job := registrationRow(t, c, "synthetic-error")
	if job == nil || job.Attempts != 1 || !strings.Contains(job.LastError, "未知待登记类型") || job.NextAttemptAt <= now {
		t.Fatalf("未保留/未退避：%+v", job)
	}
	retryOpsNow(t, c)
	c.retryOpsRegistrations(t.Context())
	if job := registrationRow(t, c, "synthetic-error"); job == nil || job.Attempts != 2 {
		t.Fatalf("未重试：%+v", job)
	}
}
