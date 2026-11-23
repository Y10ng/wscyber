package storetest_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	service "github.com/chainreactors/cyber/pkg/web/service"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func tenantOf(t *testing.T, db *bun.DB, query string, args ...any) string {
	t.Helper()
	var tenant string
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(&tenant); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return tenant
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestIdentityHelpers(t *testing.T) {
	ctx := context.Background()
	if _, ok := service.IdentityFrom(ctx); ok {
		t.Fatal("空 ctx 不应有身份")
	}
	want := service.Identity{TenantID: "t1", UserID: "u1", Role: "user"}
	got, ok := service.IdentityFrom(service.WithIdentity(ctx, want))
	if !ok || got != want {
		t.Fatalf("IdentityFrom = %+v %v", got, ok)
	}
	got, ok = service.IdentityFrom(service.WithSystem(ctx, "t2"))
	if !ok || got != (service.Identity{TenantID: "t2", Role: "system"}) {
		t.Fatalf("WithSystem = %+v %v", got, ok)
	}
}

// 无身份 ctx（上游后台路径全是 context.Background()）落默认租户。
func TestWritesWithoutIdentityLandInDefaultTenant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	if err := f.store.AddAOPEvent(ctx, "s1", messageEvent("e1", "s1", "x")); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveAOPRequest(ctx, "req-1", "CancelTurn", []byte{1}, &aop.CancelTurnResponse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SyncArtifactEvents(ctx, []*aop.Event{artifactEvent(t, "a1")}, 0); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT tenant_id FROM chat_sessions WHERE id = 's1'`,
		`SELECT tenant_id FROM chat_aop_events WHERE event_id = 'e1'`,
		`SELECT tenant_id FROM aop_request_ledger WHERE request_id = 'req-1'`,
		`SELECT tenant_id FROM raw_artifacts WHERE event_id = 'a1'`,
	} {
		if tenant := tenantOf(t, f.raw, query); tenant != "default" {
			t.Errorf("%s = %q, want default", query, tenant)
		}
	}
}

func TestWithSystemTenantIsApplied(t *testing.T) {
	f := newFixture(t)
	addTenant(t, f.raw, "t2")
	ctx := service.WithSystem(context.Background(), "t2")
	createSession(t, ctx, f.store, "s2")
	if err := f.store.AddAOPEvent(ctx, "s2", messageEvent("e2", "s2", "x")); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveAOPRequest(ctx, "req-2", "CancelTurn", []byte{2}, &aop.CancelTurnResponse{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.SyncArtifactEvents(ctx, []*aop.Event{artifactEvent(t, "a2")}, 0); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT tenant_id FROM chat_sessions WHERE id = 's2'`,
		`SELECT tenant_id FROM chat_aop_events WHERE event_id = 'e2'`,
		`SELECT tenant_id FROM aop_request_ledger WHERE request_id = 'req-2'`,
		`SELECT tenant_id FROM raw_artifacts WHERE event_id = 'a2'`,
	} {
		if tenant := tenantOf(t, f.raw, query); tenant != "t2" {
			t.Errorf("%s = %q, want t2", query, tenant)
		}
	}
	// 显式普通用户身份同样生效。
	userCtx := service.WithIdentity(context.Background(), service.Identity{TenantID: "t2", UserID: "u1", Role: "user"})
	createSession(t, userCtx, f.store, "s-user")
	if tenant := tenantOf(t, f.raw, `SELECT tenant_id FROM chat_sessions WHERE id = 's-user'`); tenant != "t2" {
		t.Fatalf("user session tenant = %q", tenant)
	}
	// 跨租户 system（无租户）不能新建根表行：WT001。
	err := f.store.CreateSession(service.WithSystem(context.Background(), ""), nobodyRecord())
	if pgCode(err) != "WT001" {
		t.Fatalf("system without tenant create = %v (code %q), want WT001", err, pgCode(err))
	}
	// 当前已知限制（P5 处理）：无身份 ctx 视为默认租户，向 t2 的会话追加事件被继承触发器拒绝（WT002）。
	err = f.store.AddAOPEvent(context.Background(), "s2", messageEvent("e-bg", "s2", "x"))
	if pgCode(err) != "WT002" {
		t.Fatalf("background append to t2 session = %v (code %q), want WT002", err, pgCode(err))
	}
	// 跨租户 system 读写子表：租户从父行继承。
	if err := f.store.AddAOPEvent(service.WithSystem(context.Background(), ""), "s2", messageEvent("e-sys", "s2", "x")); err != nil {
		t.Fatal(err)
	}
	if tenant := tenantOf(t, f.raw, `SELECT tenant_id FROM chat_aop_events WHERE event_id = 'e-sys'`); tenant != "t2" {
		t.Fatalf("inherited tenant = %q", tenant)
	}
}

func nobodyRecord() *types.SessionRecord {
	return &types.SessionRecord{Session: &aop.Session{Id: "s-nobody", State: service.SessionStateOpen}, CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now()}
}

func TestInvalidIdentityIsRejected(t *testing.T) {
	f := newFixture(t)
	for name, identity := range map[string]service.Identity{
		"empty role":       {TenantID: "default"},
		"unknown role":     {TenantID: "default", Role: "root"},
		"user sans tenant": {Role: "user", UserID: "u1"},
	} {
		ctx := service.WithIdentity(context.Background(), identity)
		if err := f.store.CreateSession(ctx, nobodyRecord()); err == nil {
			t.Errorf("%s: CreateSession accepted", name)
		}
		if _, err := f.store.GetSession(ctx, "x"); err == nil || errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s: GetSession err = %v", name, err)
		}
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM chat_sessions`); n != 0 {
		t.Fatalf("rows = %d", n)
	}
}

// 读写方法在事务内用 set_config(..., true) 设置身份；事务结束（提交或回滚）后
// 连接池里的每条连接都不应残留 app.* 设置。
func TestIdentityDoesNotOutliveTransaction(t *testing.T) {
	f := newFixture(t)
	addTenant(t, f.raw, "t2")
	ctx := service.WithIdentity(context.Background(), service.Identity{TenantID: "t2", UserID: "u9", Role: "admin"})
	createSession(t, ctx, f.store, "s1")
	if err := f.store.AddAOPEvent(ctx, "s1", messageEvent("e1", "s1", "x")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.store.ListSessionPage(ctx, 0, 10, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveAOPRequest(ctx, "r1", "m", []byte{1}, &aop.CancelTurnResponse{}); err != nil {
		t.Fatal(err)
	}
	// 失败回滚的写事务。
	if err := f.store.SaveAOPRequest(ctx, "r1", "m", []byte{1}, &aop.CancelTurnResponse{}); err == nil {
		t.Fatal("duplicate save accepted")
	}

	orm := storeORM(t, f.store)
	conns := make([]*sql.Conn, 0, 8)
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	// 占满连接池（存储池上限 8），逐条检查会话级设置。
	for range 8 {
		conn, err := orm.DB.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
		var role, tenant, user string
		if err := conn.QueryRowContext(context.Background(),
			`SELECT COALESCE(current_setting('app.role', true), ''), COALESCE(current_setting('app.tenant_id', true), ''), COALESCE(current_setting('app.user_id', true), '')`).
			Scan(&role, &tenant, &user); err != nil {
			t.Fatal(err)
		}
		if role != "" || tenant != "" || user != "" {
			t.Fatalf("连接残留身份: role=%q tenant=%q user=%q", role, tenant, user)
		}
	}
	// 身份在事务内确实生效由 TestWithSystemTenantIsApplied 的租户归属证明。
}

// 存储连接参数与 pgschema.OpenDB 对齐。
func TestStoreConnectionParameters(t *testing.T) {
	f := newFixture(t)
	orm := storeORM(t, f.store)
	var app, tz, lockTimeout string
	if err := orm.QueryRowContext(context.Background(), `SELECT current_setting('application_name'), current_setting('TimeZone'), current_setting('lock_timeout')`).Scan(&app, &tz, &lockTimeout); err != nil {
		t.Fatal(err)
	}
	if app != "wscyber-hub-native" || tz != "UTC" || lockTimeout != "10s" {
		t.Fatalf("params = %q %q %q", app, tz, lockTimeout)
	}
	if stats := orm.Stats(); stats.MaxOpenConnections != 8 {
		t.Fatalf("MaxOpenConnections = %d", stats.MaxOpenConnections)
	}
	if !strings.HasPrefix(app, "wscyber-") {
		t.Fatal(app)
	}
}
