package pgschema_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"

	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
)

// withTenant 在带 app.tenant_id 的事务里执行（同 webapi 存储 inTx 的身份设置）。
func withTenant(t *testing.T, db *bun.DB, tenant string, fn func(context.Context, bun.Tx) error) error {
	t.Helper()
	return db.RunInTx(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('app.role', 'system', true), set_config('app.tenant_id', ?, true)`, tenant); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

// 0002 在“停在 0001、已有业务数据”的库上应用：旧数据不变，新表可用且租户继承规则生效；重复迁移/启动幂等。
func TestMigrate0002OnBaselineWithData(t *testing.T) {
	ctx := context.Background()
	database := testdb.NewEmpty(t)
	db := database.Open(t, "migrate-0002")
	result, err := pgschema.MigrateUpTo(ctx, db, "0001")
	if err != nil || !slices.Equal(result.Applied, []string{"0001"}) {
		t.Fatalf("应用 0001: %v %v", result.Applied, err)
	}
	status, err := pgschema.Status(ctx, db)
	if err != nil || !slices.Equal(status.Pending, []string{"0002"}) || status.Current() {
		t.Fatalf("停在 0001 的状态 %+v %v", status, err)
	}
	if err := pgschema.RequireCurrent(ctx, db); !errors.Is(err, pgschema.ErrSchemaNotCurrent) {
		t.Fatalf("停在 0001 时 RequireCurrent 应失败: %v", err)
	}
	var exists bool
	if err := db.NewRaw(`SELECT to_regclass('public.webapi_approvals') IS NOT NULL`).Scan(ctx, &exists); err != nil || exists {
		t.Fatalf("0001 不应有审批表: %v %v", exists, err)
	}
	// 0001 上的合成业务数据：第二租户 + 任务 + 一条通知。
	if _, err := db.ExecContext(ctx, `INSERT INTO tenants (id, name, created_at) VALUES ('t2', '合成租户', now())`); err != nil {
		t.Fatal(err)
	}
	if err := withTenant(t, db, "t2", func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES ('task-old', '合成', '合成目标', '[]', 'sess-old', '2026-11-20T00:00:00Z')`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO webapi_notifications (id, dedupe_key, kind, task_id, finding_id, verification_id, vuln_id, status, attempts, next_attempt_at, last_error, created_at, sent_at)
VALUES ('n-old', 'approval:op-old', 'approval_pending', 'task-old', '', '', '', 'queued', 0, '2026-11-20T00:00:00.000Z', '', '2026-11-20T00:00:00.000Z', NULL)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		result, err := pgschema.Migrate(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		want := []string(nil)
		if i == 0 {
			want = []string{"0002"}
		}
		if !slices.Equal(result.Applied, want) {
			t.Fatalf("第 %d 次迁移应用 %v，期望 %v", i+1, result.Applied, want)
		}
	}
	if err := pgschema.RequireCurrent(ctx, db); err != nil {
		t.Fatal(err)
	}
	status, err = pgschema.Status(ctx, db)
	if err != nil || len(status.Applied) != 2 || status.Applied[1].Name != "0002" || status.Applied[1].GroupID != 2 {
		t.Fatalf("迁移后状态 %+v %v", status, err)
	}
	var objective, notifyStatus string
	if err := db.NewRaw(`SELECT objective FROM webapi_tasks WHERE id = 'task-old'`).Scan(ctx, &objective); err != nil || objective != "合成目标" {
		t.Fatalf("旧任务: %q %v", objective, err)
	}
	if err := db.NewRaw(`SELECT status FROM webapi_notifications WHERE id = 'n-old'`).Scan(ctx, &notifyStatus); err != nil || notifyStatus != "queued" {
		t.Fatalf("旧通知: %q %v", notifyStatus, err)
	}

	// 新表：审批随任务继承租户（会话租户为 default 也取父行 t2 时拒绝不一致）；无任务回退会话租户；待登记随审批。
	insertApproval := func(tenant, id, taskID string) error {
		return withTenant(t, db, tenant, func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.ExecContext(ctx, `INSERT INTO webapi_approvals (id, operation_id, task_id, session_id, parent_session_id, turn_id, call_id, tool, summary, reason, status, decided_by, status_reason, hub_instance, created_at, expires_at, decided_at)
VALUES (?, 'op-'||?, ?, 'sess-old', '', 'turn', 'call', 'write', '/tmp/x', '合成', 'pending', '', '', 'hub-a', '2026-11-23T00:00:00.000Z', '2026-11-23T00:10:00.000Z', NULL)`, id, id, taskID)
			return err
		})
	}
	if err := insertApproval("t2", "ap-1", "task-old"); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if err := insertApproval("default", "ap-x", "task-old"); !errors.As(err, &pgErr) || pgErr.Code != "WT002" {
		t.Fatalf("会话租户与任务租户不一致应拒绝: %v", err)
	}
	if err := insertApproval("default", "ap-2", ""); err != nil {
		t.Fatal(err)
	}
	if err := withTenant(t, db, "t2", func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO webapi_notify_registrations (key, kind, approval_id, args_json, attempts, next_attempt_at, last_error, created_at)
VALUES ('approval:op-ap-1', 'approval_pending', 'ap-1', '{}', 0, '2026-11-23T00:00:00.000Z', '', '2026-11-23T00:00:00.000Z')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tenantOf := func(query string) string {
		var tenant string
		if err := db.NewRaw(query).Scan(ctx, &tenant); err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	if got := tenantOf(`SELECT tenant_id FROM webapi_approvals WHERE id = 'ap-1'`); got != "t2" {
		t.Fatalf("审批租户 %q", got)
	}
	if got := tenantOf(`SELECT tenant_id FROM webapi_approvals WHERE id = 'ap-2'`); got != "default" {
		t.Fatalf("无任务审批租户 %q", got)
	}
	if got := tenantOf(`SELECT tenant_id FROM webapi_notify_registrations WHERE key = 'approval:op-ap-1'`); got != "t2" {
		t.Fatalf("待登记租户 %q", got)
	}
	// 约束：终态必须带 decided_at，状态取值受限。
	if _, err := db.ExecContext(ctx, `UPDATE webapi_approvals SET status = 'approved' WHERE id = 'ap-1'`); err == nil {
		t.Fatal("终态缺 decided_at 应被拒绝")
	}
	if _, err := db.ExecContext(ctx, `UPDATE webapi_approvals SET status = 'unknown', decided_at = 'x' WHERE id = 'ap-1'`); err == nil {
		t.Fatal("非法状态应被拒绝")
	}
	if _, err := db.ExecContext(ctx, `UPDATE webapi_approvals SET tenant_id = 'default' WHERE id = 'ap-1'`); err == nil {
		t.Fatal("tenant_id 不可修改")
	}
	// 取消拼写与 guardrail 事件一致：canceled 可落库，旧拼写不再接受。
	if _, err := db.ExecContext(ctx, `UPDATE webapi_approvals SET status = 'cancelled', decided_at = '2026-11-23T00:01:00.000Z' WHERE id = 'ap-1'`); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("旧取消拼写应被 CHECK 拒绝: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE webapi_approvals SET status = 'canceled', decided_by = 'system', decided_at = '2026-11-23T00:01:00.000Z' WHERE id = 'ap-1'`); err != nil {
		t.Fatalf("canceled 应可落库: %v", err)
	}

	// 同库再走完整启动路径（单实例锁 + 迁移 + 运行池校验）两次：幂等。
	for range 2 {
		instance, err := pgschema.Start(ctx, database.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
		}
	}
	var migrations int
	if err := db.NewRaw(`SELECT count(*) FROM wscyber_migrations`).Scan(ctx, &migrations); err != nil || migrations != 2 {
		t.Fatalf("迁移表行数 %d %v", migrations, err)
	}
}
