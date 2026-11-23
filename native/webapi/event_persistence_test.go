package webapi

import (
	"context"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	webservice "github.com/chainreactors/cyber/pkg/web/service"

	"github.com/uptrace/bun"
)

// PostgreSQL 下原生存储与 webapi 分池、互不共享写锁：webapi 写事务（持全局写锁、已写入
// 未提交）期间，原生事件追加不等待、立即落库；webapi 随后读得到该事件。
func TestAOPEventNotBlockedByWebAPIWriteLock(t *testing.T) {
	database := newTestHub(t)
	native, err := webservice.NewSQLiteStore(database.DSN(), SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	if err := native.CreateSession(context.Background(), &types.SessionRecord{Session: &aop.Session{Id: "synthetic-session"}}); err != nil {
		t.Fatal(err)
	}
	business, err := openTaskStore(database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer business.Close()

	locked, release, writeDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		writeDone <- business.write(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES ('lock-test', 'synthetic', 'synthetic', '[]', 'synthetic', '2026-01-01T00:00:00Z')`); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	started := time.Now()
	eventCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := native.AddAOPEvent(eventCtx, "synthetic-session", &aop.Event{
		Id: "synthetic-event", SessionId: "synthetic-session", Seq: 1,
		Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{}},
	}); err != nil {
		t.Fatalf("webapi 写事务期间原生事件应直接落库: %v", err)
	}
	t.Logf("webapi 持全局写锁期间原生事件落库耗时 %v", time.Since(started))
	var eventID string
	if err := business.db().QueryRow(`SELECT event_id FROM chat_aop_events WHERE session_id = ?`, "synthetic-session").Scan(&eventID); err != nil || eventID != "synthetic-event" {
		t.Fatalf("真实事件行读取失败: id=%q err=%v", eventID, err)
	}
	close(release)
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if task, err := business.Get(context.Background(), "lock-test"); err != nil || task == nil {
		t.Fatalf("webapi 写事务提交后任务应可见: %+v %v", task, err)
	}
}
