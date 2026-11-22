package webapi

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// 仅用于旧算法对照：不要从生产 openTaskStore 继承事务模式，否则 DSN 修复后
// 无法继续验证 deferred 升级立即 BUSY / BUSY_SNAPSHOT 的根因。
func openDeferredTestDB(t *testing.T, database string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", database+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=deferred")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// 调用真实业务函数，而非重写其 SQL：另一 sql.DB 持写锁时，先读后写事务
// 应在 Begin 等待，不能在 SELECT 后升级时立即 BUSY。只使用临时库与合成行。
func TestTaskStoreTransactionsWaitForWriter(t *testing.T) {
	for _, kind := range []string{NotifyKindNew, NotifyKindManual} {
		t.Run(kind, func(t *testing.T) {
			database := filepath.Join(t.TempDir(), "hub.sqlite")
			native, err := webservice.NewSQLiteStore(database, SchemaModule())
			if err != nil {
				t.Fatal(err)
			}
			if err := native.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := openTaskStore(database)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			writer, err := openTaskStore(database)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			var journal string
			var timeout int
			if err := store.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil {
				t.Fatal(err)
			}
			if journal != "wal" || timeout != 5000 {
				t.Fatalf("sqlite parameters = %s / %d", journal, timeout)
			}
			lock, err := writer.db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback()
			// 修复前 writer 也是 deferred，显式取锁以保持前后对照条件相同。
			if _, err := lock.ExecContext(t.Context(), `UPDATE webapi_notifications SET id = id WHERE 0`); err != nil {
				t.Fatal(err)
			}
			// 普通只读查询不使用显式事务，WAL 下不应被另一连接的写锁阻塞。
			readCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			_, err = store.notification(readCtx, "synthetic-missing")
			cancel()
			if err != nil {
				t.Fatalf("ordinary read under write lock: %v", err)
			}
			model := &notificationModel{
				ID: "synthetic-notification", DedupeKey: "synthetic-key", Kind: kind,
				TaskID: "synthetic-task", FindingID: "synthetic-finding", VerificationID: "synthetic-verification",
				Status: NotifyQueued, CreatedAt: notifyTime(time.Now()),
			}
			done := make(chan error, 1)
			started := time.Now()
			go func() {
				if kind == NotifyKindNew {
					inserted, err := store.insertNotification(t.Context(), model)
					if err == nil && !inserted {
						err = fmt.Errorf("new notification was not inserted")
					}
					done <- err
					return
				}
				id, inFlight, err := store.enqueueManualNotification(t.Context(), model)
				if err == nil && (id != model.ID || inFlight != "") {
					err = fmt.Errorf("manual enqueue: id=%q inFlight=%q", id, inFlight)
				}
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("write returned before lock release after %v: %v", time.Since(started), err)
			case <-time.After(1200 * time.Millisecond):
			}
			if err := lock.Commit(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("write after lock release: %v", err)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("write did not finish after lock release")
			}
			t.Logf("WAL=%s busy_timeout=%dms; held writer for 1.2s; %s succeeded after %v", journal, timeout, kind, time.Since(started))
			saved, err := store.notification(t.Context(), model.ID)
			if err != nil || saved == nil || saved.Kind != kind || saved.Status != NotifyQueued {
				t.Fatalf("persisted notification = %+v, err=%v", saved, err)
			}
			// 等锁后仍保留原有幂等 / in_flight 语义，不额外登记行。
			if kind == NotifyKindNew {
				if inserted, err := store.insertNotification(t.Context(), model); err != nil || inserted {
					t.Fatalf("duplicate new: inserted=%v err=%v", inserted, err)
				}
			} else {
				if id, inFlight, err := store.enqueueManualNotification(t.Context(), model); err != nil || id != "" || inFlight != model.ID {
					t.Fatalf("duplicate manual: id=%q inFlight=%q err=%v", id, inFlight, err)
				}
			}
			var count int
			if err := store.db.QueryRow(`SELECT count(*) FROM webapi_notifications`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("notification count=%d err=%v", count, err)
			}
		})
	}
}
