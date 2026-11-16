package webapi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// 两个真实原生 Store 连接同一临时库：业务写事务持锁后，原生事件应先等待，
// 释放锁后真实落盘；持锁超过 busy_timeout 时仍须把错误交给调用方。
func TestAOPEventWaitsForWebAPIWriteLock(t *testing.T) {
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	native, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if err := native.CreateSession(context.Background(), &types.SessionRecord{Session: &aop.Session{Id: "synthetic-session"}}); err != nil {
		t.Fatal(err)
	}
	business, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	defer business.Close()

	// 模拟 Web/API 业务写事务：取得写锁后执行真实 INSERT，再让事件进入先读
	// 后写的原生 AppendAOPEvent。deferred 会在升级时立即 SQLITE_BUSY。
	lockCtx, cancelLock := context.WithCancel(context.Background())
	defer cancelLock()
	tx, err := business.db.BeginTx(lockCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES ('lock-test', 'synthetic', 'synthetic', '[]', 'synthetic', '2026-01-01T00:00:00Z')`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}

	started := time.Now()
	t.Log("业务写锁已持有，开始写入原生事件")
	eventDone := make(chan error, 1)
	go func() {
		eventDone <- native.AddAOPEvent(context.Background(), "synthetic-session", &aop.Event{
			Id: "synthetic-event", SessionId: "synthetic-session", Seq: 1,
			Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{}},
		})
	}()
	select {
	case err := <-eventDone:
		t.Fatalf("业务写锁释放前事件调用提前结束: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Logf("业务写锁已释放，等待原生事件（已耗时 %v）", time.Since(started))
	if err := <-eventDone; err != nil {
		t.Fatalf("原生事件未在业务写锁释放后落盘（未修补的 deferred 事务此处应为 SQLITE_BUSY）: %v", err)
	}
	var eventID string
	if err := business.db.QueryRow(`SELECT event_id FROM chat_aop_events WHERE session_id = ?`, "synthetic-session").Scan(&eventID); err != nil || eventID != "synthetic-event" {
		t.Fatalf("真实事件行读取失败: id=%q err=%v", eventID, err)
	}

	// 超过同一 5 秒 busy_timeout 必须有界报错，不能吞错误或伪造成功。
	tx, err = business.db.BeginTx(lockCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES ('timeout-lock', 'synthetic', 'synthetic', '[]', 'synthetic', '2026-01-01T00:00:00Z')`); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	started = time.Now()
	err = native.AddAOPEvent(context.Background(), "synthetic-session", &aop.Event{
		Id: "timeout-event", SessionId: "synthetic-session", Seq: 2,
		Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{}},
	})
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") {
		t.Fatalf("持续写锁应返回可见 SQLITE_BUSY: %v", err)
	}
	if elapsed > 7*time.Second {
		t.Fatalf("等待未受 busy_timeout 约束: %v", elapsed)
	}
	t.Logf("持续写锁下有界返回错误: elapsed=%v err=%v", elapsed, err)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := business.db.QueryRow(`SELECT COUNT(*) FROM chat_aop_events WHERE event_id = 'timeout-event'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("失败事件不得伪造持久化: count=%d err=%v", count, err)
	}
}
