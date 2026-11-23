package pgschema_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"

	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
)

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// asTenant 在事务内设置 app.tenant_id（is_local=true，与 §4.4 的助手一致）后执行 fn。
func asTenant(ctx context.Context, db *bun.DB, tenant string, fn func(tx bun.Tx) error) error {
	return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if tenant != "" {
			if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', ?, true)`, tenant); err != nil {
				return err
			}
		}
		return fn(tx)
	})
}

func insertTask(ctx context.Context, conn bun.IConn, id string) error {
	_, err := conn.ExecContext(ctx, `INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES (?, '', '', '[]', '', '2026-11-22T00:00:00Z')`, id)
	return err
}

func insertPolicy(ctx context.Context, conn bun.IConn, taskID string) error {
	_, err := conn.ExecContext(ctx, `INSERT INTO webapi_task_policies (task_id, scope, approval, updated_at) VALUES (?, '{}', '{}', '2026-11-22T00:00:00Z')`, taskID)
	return err
}

func tenantOf(t *testing.T, db *bun.DB, query string, args ...any) string {
	t.Helper()
	var tenant string
	if err := db.NewRaw(query, args...).Scan(context.Background(), &tenant); err != nil {
		t.Fatal(err)
	}
	return tenant
}

func TestTenantDefaultsAndInheritance(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t).Open(t, "tenant-test")
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, created_at) VALUES ('t2', '租户二', '2026-11-22T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	// 根表：没有 app.tenant_id 时插入失败（失败可见，不落默认租户）。
	if err := insertTask(ctx, db, "task-none"); pgCode(err) != "WT001" {
		t.Fatalf("未设租户插根表应 WT001: %v", err)
	}
	// 根表：取 app.tenant_id。
	if err := asTenant(ctx, db, "default", func(tx bun.Tx) error { return insertTask(ctx, tx, "task-a") }); err != nil {
		t.Fatal(err)
	}
	if err := asTenant(ctx, db, "t2", func(tx bun.Tx) error { return insertTask(ctx, tx, "task-b") }); err != nil {
		t.Fatal(err)
	}
	if got := tenantOf(t, db, `SELECT tenant_id FROM webapi_tasks WHERE id = 'task-b'`); got != "t2" {
		t.Fatalf("task-b 租户 %q", got)
	}

	// 子表：无会话租户时从父行继承（上游 context.Background() 写事件即此路径）。
	if err := insertPolicy(ctx, db, "task-b"); err != nil {
		t.Fatal(err)
	}
	if got := tenantOf(t, db, `SELECT tenant_id FROM webapi_task_policies WHERE task_id = 'task-b'`); got != "t2" {
		t.Fatalf("子表应继承父行租户，得到 %q", got)
	}
	// 子表：会话租户与父行一致时允许。
	if err := asTenant(ctx, db, "default", func(tx bun.Tx) error { return insertPolicy(ctx, tx, "task-a") }); err != nil {
		t.Fatal(err)
	}
	// 子表：父行不存在 → 拒绝。
	if err := insertPolicy(ctx, db, "task-missing"); pgCode(err) != "WT003" {
		t.Fatalf("无父行应 WT003: %v", err)
	}
	if err := asTenant(ctx, db, "default", func(tx bun.Tx) error { return insertPolicy(ctx, tx, "task-missing") }); pgCode(err) != "WT003" {
		t.Fatalf("设了租户也不能绕过父行检查: %v", err)
	}
	// 子表：会话租户与父行不一致 → 拒绝（防止跨租户挂子行）。
	if _, err := db.ExecContext(ctx, `DELETE FROM webapi_task_policies`); err != nil {
		t.Fatal(err)
	}
	if err := asTenant(ctx, db, "default", func(tx bun.Tx) error { return insertPolicy(ctx, tx, "task-b") }); pgCode(err) != "WT002" {
		t.Fatalf("会话租户与父行不一致应 WT002: %v", err)
	}
	// 子表：显式 tenant_id 与父行不一致 → 拒绝。
	_, err := db.ExecContext(ctx, `INSERT INTO webapi_task_policies (task_id, scope, approval, updated_at, tenant_id) VALUES ('task-b', '{}', '{}', 'x', 'default')`)
	if pgCode(err) != "WT002" {
		t.Fatalf("显式租户不一致应 WT002: %v", err)
	}

	// 多级：verification_results ← verifications ← tasks。
	if _, err := db.ExecContext(ctx, `INSERT INTO webapi_verifications (id, task_id, finding_id, source_revision_id, session_id, created_at) VALUES ('ver-b', 'task-b', 'f', 'r', 'sess-ver-b', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO webapi_verification_results (verification_id, session_id, turn_id, call_id, payload_json, created_at) VALUES ('ver-b', 's', 't', 'c', '{}', 'x')`); err != nil {
		t.Fatal(err)
	}
	if got := tenantOf(t, db, `SELECT tenant_id FROM webapi_verification_results WHERE verification_id = 'ver-b'`); got != "t2" {
		t.Fatalf("孙表租户 %q", got)
	}

	// 外键列为空（如出口流量 task_id 未知）→ 回退 app.tenant_id；都没有则失败。
	insertFlow := func(conn bun.IConn, id string) error {
		_, err := conn.ExecContext(ctx, `INSERT INTO webapi_egress_flows (id, task_id, session_id, turn_id, call_id, tool, method, url, host, decision, error, flow_pb, request_bytes, response_bytes, preview_truncated, created_at)
VALUES (?, '', '', '', '', 'bash', 'GET', 'http://x/', 'x', 'allow', '', '\x00'::bytea, 0, 0, false, 'x')`, id)
		return err
	}
	if err := insertFlow(db, "flow-none"); pgCode(err) != "WT001" {
		t.Fatalf("空外键且未设租户应 WT001: %v", err)
	}
	if err := asTenant(ctx, db, "t2", func(tx bun.Tx) error { return insertFlow(tx, "flow-t2") }); err != nil {
		t.Fatal(err)
	}
	if got := tenantOf(t, db, `SELECT tenant_id FROM webapi_egress_flows WHERE id = 'flow-t2'`); got != "t2" {
		t.Fatalf("回退会话租户，得到 %q", got)
	}

	// 原生事件从会话继承。
	if err := asTenant(ctx, db, "t2", func(tx bun.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO chat_sessions (id, node_id, status, archived, title, agent_name, session_json, created_at, updated_at) VALUES ('sess-1', 'n', 'idle', false, '', '', '{}', 'x', 'x')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO chat_aop_events (id, session_id, event_id, cursor, turn_id, emitter, sequence, event_proto, created_at) VALUES ('e1', 'sess-1', 'ev1', 1, 't', 'x', 1, '\x00'::bytea, 'x')`); err != nil {
		t.Fatal(err)
	}
	if got := tenantOf(t, db, `SELECT tenant_id FROM chat_aop_events WHERE id = 'e1'`); got != "t2" {
		t.Fatalf("事件应继承会话租户，得到 %q", got)
	}
	// raw_artifacts 的 identity 游标。
	if err := asTenant(ctx, db, "t2", func(tx bun.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO raw_artifacts (event_id, event_proto, created_at) VALUES ('a1', '\x00'::bytea, 'x'), ('a2', '\x00'::bytea, 'x')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var maxCursor int64
	if err := db.NewRaw(`SELECT max(cursor) FROM raw_artifacts`).Scan(ctx, &maxCursor); err != nil || maxCursor != 2 {
		t.Fatalf("identity 游标 %d %v", maxCursor, err)
	}

	// 审计：未设租户允许 NULL（全局配置事件）。
	if _, err := db.ExecContext(ctx, `INSERT INTO webapi_audit_events (id, at, actor, action, target_kind, target_id, task_id, asset_id, summary, detail_json) VALUES ('au1', 'x', 'system', 'a', 'k', '', '', '', '', '{}')`); err != nil {
		t.Fatal(err)
	}
	var auditTenant *string
	if err := db.NewRaw(`SELECT tenant_id FROM webapi_audit_events WHERE id = 'au1'`).Scan(ctx, &auditTenant); err != nil || auditTenant != nil {
		t.Fatalf("审计租户应为 NULL: %v %v", auditTenant, err)
	}

	// 不允许改 tenant_id。
	if _, err := db.ExecContext(ctx, `UPDATE webapi_tasks SET tenant_id = 'default' WHERE id = 'task-b'`); pgCode(err) != "WT004" {
		t.Fatalf("改 tenant_id 应 WT004: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE webapi_tasks SET objective = 'changed' WHERE id = 'task-b'`); err != nil {
		t.Fatalf("改其它列应允许: %v", err)
	}
}

// set_config(..., true) 用过后，同一连接的下一个事务里 app.tenant_id 是空串而不是未定义；
// 必须仍按“未设置”处理，否则会落到空租户或沿用上个请求的租户。
func TestTenantSettingDoesNotLeakAcrossTransactions(t *testing.T) {
	ctx := context.Background()
	database := testdb.New(t)
	db, err := pgschema.OpenDB(database.URL, "tenant-leak", pgschema.PoolOptions{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := asTenant(ctx, db, "default", func(tx bun.Tx) error { return insertTask(ctx, tx, "task-1") }); err != nil {
		t.Fatal(err)
	}
	if err := insertTask(ctx, db, "task-2"); pgCode(err) != "WT001" {
		t.Fatalf("上一事务的租户不应残留: %v", err)
	}
}

func TestCountersAndTenantScopedFindingNumber(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t).Open(t, "tenant-counter")
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, created_at) VALUES ('t2', '租户二', 'x')`); err != nil {
		t.Fatal(err)
	}
	next := func(tenant string) int64 {
		var value int64
		err := db.NewRaw(`INSERT INTO webapi_counters (tenant_id, name, value) VALUES (?, 'finding_number', 1)
ON CONFLICT (tenant_id, name) DO UPDATE SET value = webapi_counters.value + 1 RETURNING value`, tenant).Scan(ctx, &value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	if a, b, c := next("default"), next("default"), next("t2"); a != 1 || b != 2 || c != 1 {
		t.Fatalf("计数器 %d %d %d", a, b, c)
	}
	// 漏洞编号租户内唯一：两个租户可各有 1 号，同租户重复则冲突。
	for _, task := range []struct{ id, tenant string }{{"ta", "default"}, {"tb", "t2"}} {
		if err := asTenant(ctx, db, task.tenant, func(tx bun.Tx) error { return insertTask(ctx, tx, task.id) }); err != nil {
			t.Fatal(err)
		}
	}
	insertFinding := func(id, task string) error {
		_, err := db.ExecContext(ctx, `INSERT INTO webapi_findings (id, task_id, number, status, current_revision, current_revision_id, created_at, updated_at) VALUES (?, ?, 1, 'candidate', 1, 'r', 'x', 'x')`, id, task)
		return err
	}
	if err := insertFinding("f1", "ta"); err != nil {
		t.Fatal(err)
	}
	if err := insertFinding("f2", "tb"); err != nil {
		t.Fatalf("不同租户同编号应允许: %v", err)
	}
	if err := insertFinding("f3", "ta"); pgCode(err) != "23505" {
		t.Fatalf("同租户同编号应唯一冲突: %v", err)
	}
}
