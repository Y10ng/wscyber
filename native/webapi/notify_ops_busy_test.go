package webapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/exts/guardrail"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// 全部使用 fixture 临时库和本地 httptest webhook；第二个连接模拟 Hub 写事务。
func holdOpsWriteLock(t *testing.T, f *notifyFixture) *sql.Tx {
	t.Helper()
	writer, err := openTaskStore(f.database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	tx, err := writer.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(t.Context(), `UPDATE webapi_notify_settings SET enabled = enabled WHERE id = 'wecom'`); err != nil {
		t.Fatal(err)
	}
	return tx
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

// 原算法在另一个连接仅持锁数毫秒时即 BUSY，绝非必须持锁超过 busy_timeout。
func TestNotifyOpsDeferredUpgradeBusy(t *testing.T) {
	f := newNotifyFixture(t)
	f.enable(false, "")
	var journal string
	var timeout int
	if err := f.capability.store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if err := f.capability.store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if journal != "wal" || timeout != 5000 {
		t.Fatalf("sqlite parameters = %s / %d", journal, timeout)
	}
	lock := holdOpsWriteLock(t, f)
	deferred := openDeferredTestDB(t, f.database)
	tx, err := deferred.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM webapi_notifications`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = tx.Exec(`UPDATE webapi_notifications SET id = id WHERE 0`)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") || elapsed > time.Second {
		t.Fatalf("deferred upgrade = %v after %v", err, elapsed)
	}
	t.Logf("两个连接；WAL=%s busy_timeout=%dms；先读后写升级在 %v 即失败：%v", journal, timeout, elapsed, err)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// 生产登记通过 IMMEDIATE 在 Begin 等待写锁；锁释放后一次插入内容与通知。
	registered := make(chan error, 1)
	started = time.Now()
	go func() {
		_, err := f.capability.registerOps(t.Context(), NotifyKindScheduleFailed, "schedule_run:lock-test", "", &notificationOpsModel{Title: "合成计划"}, opsPayload{Error: "合成失败"})
		registered <- err
	}()
	select {
	case err := <-registered:
		t.Fatalf("锁未释放登记已返回：%v", err)
	case <-time.After(2 * time.Second):
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-registered; err != nil {
		t.Fatal(err)
	}
	t.Logf("修复后持锁 2s，释放后登记成功：%v", time.Since(started))
	inserted, err := f.capability.registerOps(t.Context(), NotifyKindScheduleFailed, "schedule_run:lock-test", "", &notificationOpsModel{Title: "合成计划"}, opsPayload{})
	if err != nil || inserted || len(opsNotifications(t, f.capability, NotifyKindScheduleFailed)) != 1 {
		t.Fatalf("重复登记：inserted=%v err=%v", inserted, err)
	}
}

// 真实 Hub AppendAOPEvent 在去重 SELECT 和 INSERT 之间提交一次即可使旧快照失效。
// 不需要持锁 5s，也没有审批调用链里的长事务或嵌套借连接。
func TestNotifyOpsNativeEventInvalidatesDeferredSnapshot(t *testing.T) {
	f := newNotifyFixture(t)
	native, err := webservice.NewSQLiteStore(f.database, SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if err := native.CreateSession(t.Context(), &types.SessionRecord{Session: &aop.Session{Id: "synthetic-ops-session"}}); err != nil {
		t.Fatal(err)
	}
	deferred := openDeferredTestDB(t, f.database)
	tx, err := deferred.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRow(`SELECT count(*) FROM webapi_notifications`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := native.AddAOPEvent(t.Context(), "synthetic-ops-session", &aop.Event{
		Id: "synthetic-ops-event", SessionId: "synthetic-ops-session", Seq: 1,
		Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{}},
	}); err != nil {
		t.Fatal(err)
	}
	eventElapsed := time.Since(started)
	started = time.Now()
	_, err = tx.Exec(`UPDATE webapi_notifications SET id = id WHERE 0`)
	var sqliteError interface{ Code() int }
	if !errors.As(err, &sqliteError) || sqliteError.Code() != 517 {
		t.Fatalf("want SQLITE_BUSY_SNAPSHOT (517), got %v", err)
	}
	t.Logf("真实 Hub AppendAOPEvent 全调用（含写事务）耗时 %v；已提交后旧登记快照升级在 %v 失败：%v", eventElapsed, time.Since(started), err)
}

func TestNotifyOpsApprovalBusyEventuallyRegistersOnce(t *testing.T) {
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
	lock := holdOpsWriteLock(t, f)
	started := time.Now()
	done := startReview(t, h, t.Context(), task.InitialSessionID, "busy", "write", `{"path":"/tmp/synthetic.txt"}`)
	pending := waitOpsEvent(t, h)
	t.Logf("持写锁期间审批 PENDING 事件耗时：%v", time.Since(started))
	// 真实超过 5s busy_timeout，让第一次登记失败并保留队列；不缩短生产退避。
	time.Sleep(time.Until(started.Add(6 * time.Second)))
	c.notify.opsRunMu.Lock()
	c.notify.opsMu.Lock()
	job := c.notify.opsPending[notifyKeyApproval+pending.Operation.OperationId]
	retried := job != nil && !job.nextAttempt.IsZero()
	c.notify.opsMu.Unlock()
	c.notify.opsRunMu.Unlock()
	if !retried {
		t.Fatal("锁持续 6s 后登记失败没有保留到重试队列")
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		// webhook 收到请求不等于发送终态已落库；终态提交后再停 worker，
		// 避免测试的 stop 恰好取消响应，把本次发送恢复为 queued。
		sent, err := c.store.orm.NewSelect().Model((*notificationModel)(nil)).
			Where("kind = ? AND status = ?", NotifyKindApprovalPending, NotifySent).Count(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if sent == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("重试后未提交 sent 终态")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := len(f.wecom.received()); got != 1 {
		t.Fatalf("重试后投递条数 = %d", got)
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
	t.Logf("持锁 6s，真实 10s 退避后恰好登记/投递一次，总耗时 %v", time.Since(started))
}

func TestNotifyOpsApprovalResolvedDuringBusy(t *testing.T) {
	for _, outcome := range []string{"approved", "rejected", "canceled", "expired"} {
		t.Run(outcome, func(t *testing.T) {
			f, task, h := opsApprovalFixture(t, false, `"notify_approval":true`)
			c := f.capability
			if outcome == "expired" {
				c.approvals.timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			lock := holdOpsWriteLock(t, f)
			done := startReview(t, h, ctx, task.InitialSessionID, "resolved-busy", "write", `{"path":"/tmp/synthetic.txt"}`)
			pending := waitOpsEvent(t, h)
			registerDone := make(chan struct{})
			go func() { c.retryOpsRegistrations(t.Context()); close(registerDone) }()
			// 保证尝试已借出单连接并正在等待第二连接的写锁。
			deadline := time.Now().Add(time.Second)
			for c.store.db.Stats().InUse == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if c.store.db.Stats().InUse != 1 {
				t.Fatal("registration did not start")
			}
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
			// SQLite busy handler 未必立即响应取消；锁释放前已决，释放后也不能补插。
			if err := lock.Rollback(); err != nil {
				t.Fatal(err)
			}
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

func TestNotifyOpsScheduleAndDigestBusyRetry(t *testing.T) {
	for _, kind := range []string{NotifyKindScheduleFailed, NotifyKindWeeklyDigest} {
		t.Run(kind, func(t *testing.T) {
			f := newNotifyFixture(t)
			c := f.capability
			f.configure(notifySettingsBody(f.wecom.webhook(), false, "", `"notify_schedule_failed":true,"notify_weekly_digest":true`))
			f.createAsset("a1")
			run := &scheduleRunModel{ID: "synthetic-failed-run", AssetID: "a1", ScheduledFor: "2026-11-19T07:00:00Z", Status: ScheduleRunFailed, Error: "select_node：合成失败", CreatedAt: nowRFC3339()}
			if _, err := c.store.orm.NewInsert().Model(run).Exec(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.setNow("2026-11-23T01:00:00Z")
			lock := holdOpsWriteLock(t, f)
			if kind == NotifyKindScheduleFailed {
				c.notifyScheduleFailed(t.Context(), "a1", run.ScheduledFor, "0 * * * *")
				c.notifyScheduleFailed(t.Context(), "a1", run.ScheduledFor, "0 * * * *")
			} else {
				c.enqueueOps(NotifyKindWeeklyDigest, nil, c.registerWeeklyDigest)
			}
			// 短 attempt 上下文不会删除失败工作；SQLite 的真实 busy_timeout 仍保留 5s。
			attempt, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			c.retryOpsRegistrations(attempt)
			cancel()
			if pendingOpsCount(c) != 1 {
				t.Fatalf("失败后待登记数 = %d", pendingOpsCount(c))
			}
			if err := lock.Rollback(); err != nil {
				t.Fatal(err)
			}
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
