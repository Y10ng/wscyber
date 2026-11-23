package webapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/exts/guardrail"
)

// 运营通知登记与 webapi 全局写锁（PostgreSQL 版）。SQLite 时代这里测的是 BUSY / BUSY_SNAPSHOT
// 与 busy_timeout；PG 下写事务开头取全局 advisory lock 排队，不会“立即失败”，所以这里保留的是
// 业务断言：持锁期间 PENDING 事件不被阻塞、登记等锁后恰好一次、审批已决不迟到补登记、
// 短尝试超时保留重试。另一写者用独立连接持有同一把锁模拟。

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

// Pending 协议在 onPending 回调前就可能读到内存审批；等同 operation 的
// PENDING 事件，确认入队回调完成后，fixture 才能显式推进异步登记。
func waitOpsPending(t *testing.T, h *approvalHarness, sessionID string) *guardrail.Review {
	t.Helper()
	pending := h.waitPending(t, sessionID)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, review := range h.reviews {
			if review.GetOperation().GetOperationId() == pending.GetOperation().GetOperationId() && review.State == guardrail.ReviewState_REVIEW_STATE_PENDING {
				h.mu.Unlock()
				return pending
			}
		}
		h.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("pending callback did not finish")
	return nil
}

func retryOpsNow(c *Capability) {
	c.notify.opsRunMu.Lock()
	defer c.notify.opsRunMu.Unlock()
	c.notify.opsMu.Lock()
	defer c.notify.opsMu.Unlock()
	for _, job := range c.notify.opsPending {
		job.nextAttempt = time.Time{}
	}
}

func pendingOpsCount(c *Capability) int {
	c.notify.opsMu.Lock()
	defer c.notify.opsMu.Unlock()
	return len(c.notify.opsPending)
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

// 持写锁期间审批 PENDING 事件不等锁；后台登记等锁后恰好登记、投递一次，重复登记不新增。
func TestNotifyOpsApprovalUnderWriteLockRegistersOnce(t *testing.T) {
	f, task, h := opsApprovalFixture(t, false, `"notify_approval":true`)
	c := f.capability
	c.notify.interval = 20 * time.Millisecond
	c.startNotifier()
	// 确保启动时的 resetSendingNotifications 已完成，再持锁。
	ready := make(chan struct{})
	c.enqueueOps("test-ready", nil, func(context.Context) error { close(ready); return nil })
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("notifier not ready")
	}
	release := holdWebAPIWriteLock(t, f.database)
	started := time.Now()
	done := startReview(t, h, t.Context(), task.InitialSessionID, "busy", "write", `{"path":"/tmp/synthetic.txt"}`)
	pending := waitOpsEvent(t, h)
	t.Logf("持写锁期间审批 PENDING 事件耗时：%v", time.Since(started))
	waitWriteLockWaiter(t, f.database)
	time.Sleep(300 * time.Millisecond)
	if items, err := sentOpsCount(t.Context(), c); err != nil || items != 0 {
		t.Fatalf("持锁期间不应登记完成：%d %v", items, err)
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
	if len(items) != 1 || items[0].Status != NotifySent {
		t.Fatalf("登记 = %+v", items)
	}
	if err := c.registerApprovalPending(t.Context(), task, "write", `{"path":"/tmp/synthetic.txt"}`, pending); err != nil {
		t.Fatal(err)
	}
	if items := opsNotifications(t, c, NotifyKindApprovalPending); len(items) != 1 {
		t.Fatalf("重复登记 = %+v", items)
	}
	if !h.resolve(t, task.InitialSessionID, pending.Operation.OperationId, false) {
		t.Fatal("resolve failed")
	}
	<-done
	t.Logf("持锁后恰好登记/投递一次，总耗时 %v", time.Since(started))
}

func sentOpsCount(ctx context.Context, c *Capability) (int64, error) {
	return readValue(ctx, c.store, func(ctx context.Context, db bun.IDB) (int64, error) {
		return db.NewSelect().Model((*notificationModel)(nil)).
			Where("kind = ? AND status = ?", NotifyKindApprovalPending, NotifySent).Count(ctx)
	})
}

// 登记在等全局写锁期间审批已决（批准/拒绝/取消/超时）：锁释放后也不能补插。
func TestNotifyOpsApprovalResolvedDuringWriteLock(t *testing.T) {
	for _, outcome := range []string{"approved", "rejected", "canceled", "expired"} {
		t.Run(outcome, func(t *testing.T) {
			f, task, h := opsApprovalFixture(t, false, `"notify_approval":true`)
			c := f.capability
			if outcome == "expired" {
				c.approvals.timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			release := holdWebAPIWriteLock(t, f.database)
			done := startReview(t, h, ctx, task.InitialSessionID, "resolved-busy", "write", `{"path":"/tmp/synthetic.txt"}`)
			pending := waitOpsEvent(t, h)
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
				time.Sleep(150 * time.Millisecond)
			}
			// 锁释放前已决，释放后也不能补插。
			release()
			<-registerDone
			<-done
			retryOpsNow(c)
			if rows := opsNotifications(t, c, NotifyKindApprovalPending); len(rows) != 0 {
				t.Fatalf("已决审批被补登记：%+v", rows)
			}
			f.deliver()
			if len(f.wecom.received()) != 0 || pendingOpsCount(c) != 0 {
				t.Fatal("已决审批仍被投递或保留队列")
			}
		})
	}
}

// 短尝试上下文在等锁时到期：失败工作保留；锁释放后重试恰好登记、投递一次。
func TestNotifyOpsScheduleAndDigestLockRetry(t *testing.T) {
	for _, kind := range []string{NotifyKindScheduleFailed, NotifyKindWeeklyDigest} {
		t.Run(kind, func(t *testing.T) {
			f := newNotifyFixture(t)
			c := f.capability
			f.configure(notifySettingsBody(f.wecom.webhook(), false, "", `"notify_schedule_failed":true,"notify_weekly_digest":true`))
			f.createAsset("a1")
			run := &scheduleRunModel{ID: "synthetic-failed-run", AssetID: "a1", ScheduledFor: "2026-11-19T07:00:00Z", Status: ScheduleRunFailed, Error: "select_node：合成失败", CreatedAt: nowRFC3339()}
			if err := c.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
				_, err := tx.NewInsert().Model(run).Exec(ctx)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			f.setNow("2026-11-23T01:00:00Z")
			release := holdWebAPIWriteLock(t, f.database)
			if kind == NotifyKindScheduleFailed {
				c.notifyScheduleFailed(t.Context(), "a1", run.ScheduledFor, "0 * * * *")
				c.notifyScheduleFailed(t.Context(), "a1", run.ScheduledFor, "0 * * * *")
			} else {
				c.enqueueOps(NotifyKindWeeklyDigest, nil, c.registerWeeklyDigest)
			}
			attempt, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			c.retryOpsRegistrations(attempt)
			cancel()
			if pendingOpsCount(c) != 1 {
				t.Fatalf("失败后待登记数 = %d", pendingOpsCount(c))
			}
			release()
			retryOpsNow(c)
			items := opsNotifications(t, c, kind)
			if len(items) != 1 || pendingOpsCount(c) != 0 {
				t.Fatalf("恢复登记 = %+v, pending=%d", items, pendingOpsCount(c))
			}
			f.deliver()
			if len(f.wecom.received()) != 1 {
				t.Fatal("恢复后未投递一次")
			}
		})
	}
}

func TestNotifyOpsRegistrationStopsWithNotifier(t *testing.T) {
	f := newNotifyFixture(t)
	c := f.capability
	started := make(chan struct{})
	c.enqueueOps("synthetic-wait", nil, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	c.startNotifier()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("registration did not start")
	}
	stopped := make(chan struct{})
	go func() { c.stopNotifier(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("registration did not stop with notifier")
	}
	if pendingOpsCount(c) != 1 {
		t.Fatal("停止时不能把未登记项当作成功移除")
	}
}

func TestNotifyOpsRegistrationFailureIsRetainedAndBounded(t *testing.T) {
	f := newNotifyFixture(t)
	c := f.capability
	attempts := 0
	c.enqueueOps("synthetic-error", nil, func(context.Context) error {
		attempts++
		return errors.New("synthetic unavailable")
	})
	c.retryOpsRegistrations(t.Context())
	c.retryOpsRegistrations(t.Context())
	if attempts != 1 || pendingOpsCount(c) != 1 {
		t.Fatalf("未保留/未退避：attempts=%d pending=%d", attempts, pendingOpsCount(c))
	}
	retryOpsNow(c)
	c.retryOpsRegistrations(t.Context())
	if attempts != 2 {
		t.Fatalf("未重试：%d", attempts)
	}
}
