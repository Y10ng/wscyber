package storetest_test

import (
	"context"
	"reflect"
	"testing"
	"time"
	"unsafe"

	aop "github.com/chainreactors/cyber/aop"
	toolpb "github.com/chainreactors/cyber/aop/tool"
	types "github.com/chainreactors/cyber/core/types"
	service "github.com/chainreactors/cyber/pkg/web/service"
	"github.com/uptrace/bun"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"wscyber/native/internal/testdb"
)

type fixture struct {
	store *service.SQLiteStore
	raw   *bun.DB // 测试自己的连接池，用于直接核对行（不经存储层身份）
	db    testdb.Database
}

func newFixture(t *testing.T, modules ...service.SchemaModule) fixture {
	t.Helper()
	db := testdb.New(t)
	raw := db.Open(t, "storetest")
	store, err := service.NewSQLiteStore(db.DSN(), modules...)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return fixture{store: store, raw: raw, db: db}
}

// storeORM 取存储内部连接池（未导出字段 orm），只用于检查连接上的会话设置
// 是否残留；上游 events.go 也直接使用该字段。
func storeORM(t *testing.T, store *service.SQLiteStore) *bun.DB {
	t.Helper()
	field := reflect.ValueOf(store).Elem().FieldByName("orm")
	if !field.IsValid() {
		t.Fatal("SQLiteStore 没有 orm 字段")
	}
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().(*bun.DB)
}

func createSession(t *testing.T, ctx context.Context, store *service.SQLiteStore, id string) {
	t.Helper()
	if err := store.CreateSession(ctx, &types.SessionRecord{
		Session: &aop.Session{Id: id, State: service.SessionStateOpen}, CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(),
	}); err != nil {
		t.Fatalf("CreateSession(%q): %v", id, err)
	}
}

func sessionRecord(id, node, title string, updated time.Time) *types.SessionRecord {
	return &types.SessionRecord{
		Session:   &aop.Session{Id: id, NodeId: node, State: service.SessionStateOpen, Title: title},
		CreatedAt: timestamppb.New(updated), UpdatedAt: timestamppb.New(updated),
	}
}

func messageEvent(id, session, text string) *aop.Event {
	return &aop.Event{
		Id: id, EmittedAt: timestamppb.Now(), SessionId: session, Emitter: "cyber",
		Payload: &aop.Event_Message{Message: &aop.Message{Id: "m-" + id, Role: "assistant", Content: []*aop.Content{aop.Text(text)}}},
	}
}

func artifactEvent(t *testing.T, id string) *aop.Event {
	t.Helper()
	payload, err := anypb.New(&toolpb.Artifact{Tool: "gogo", Data: []byte(`{"ip":"127.0.0.1"}`), MediaType: aop.JSONMediaType})
	if err != nil {
		t.Fatal(err)
	}
	return &aop.Event{Id: id, EmittedAt: timestamppb.Now(), Payload: &aop.Event_Extension{Extension: payload}}
}

func countRows(t *testing.T, db bun.IDB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.NewRaw(query, args...).Scan(context.Background(), &n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func addTenant(t *testing.T, db *bun.DB, id string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO tenants (id, name, created_at) VALUES (?, ?, '2026-11-22T00:00:00Z')`, id, "租户 "+id); err != nil {
		t.Fatal(err)
	}
}
