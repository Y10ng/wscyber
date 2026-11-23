package pgschema_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
)

func TestKnownMigrations(t *testing.T) {
	names, err := pgschema.KnownMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"0001"}) {
		t.Fatalf("内嵌迁移 %v", names)
	}
	if v := pgschema.SchemaVersion(); v != 1 {
		t.Fatalf("SchemaVersion = %d", v)
	}
}

func TestMigrateEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewEmpty(t).Open(t, "migrate-test")
	status, err := pgschema.Status(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if status.Initialized || status.Current() || !slices.Equal(status.Pending, []string{"0001"}) || status.Expected != "0001" {
		t.Fatalf("空库状态 %+v", status)
	}
	// Status 只读：不应建出迁移表。
	var exists bool
	if err := db.NewRaw(`SELECT to_regclass('public.wscyber_migrations') IS NOT NULL`).Scan(ctx, &exists); err != nil || exists {
		t.Fatalf("Status 不应建表: exists=%v err=%v", exists, err)
	}
	if err := pgschema.RequireCurrent(ctx, db); !errors.Is(err, pgschema.ErrSchemaNotCurrent) {
		t.Fatalf("空库 RequireCurrent 应失败: %v", err)
	}

	result, err := pgschema.Migrate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Applied, []string{"0001"}) {
		t.Fatalf("应用 %v", result.Applied)
	}
	status, err = pgschema.Status(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Current() || len(status.Applied) != 1 || status.Applied[0].Name != "0001" || status.Applied[0].GroupID != 1 || status.Applied[0].MigratedAt.IsZero() {
		t.Fatalf("迁移后状态 %+v", status)
	}
	if err := pgschema.RequireCurrent(ctx, db); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.NewRaw(`SELECT name FROM tenants WHERE id = 'default'`).Scan(ctx, &name); err != nil || name != "默认租户" {
		t.Fatalf("默认租户种子: %q %v", name, err)
	}

	// 幂等：再跑不应用任何迁移、不重复登记、不重复种子。
	for range 2 {
		again, err := pgschema.Migrate(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Applied) != 0 {
			t.Fatalf("重复迁移应用了 %v", again.Applied)
		}
	}
	var rows, tenants int
	if err := db.NewRaw(`SELECT count(*) FROM wscyber_migrations`).Scan(ctx, &rows); err != nil || rows != 1 {
		t.Fatalf("迁移表行数 %d %v", rows, err)
	}
	if err := db.NewRaw(`SELECT count(*) FROM tenants`).Scan(ctx, &tenants); err != nil || tenants != 1 {
		t.Fatalf("租户行数 %d %v", tenants, err)
	}
}

func TestMigrateConcurrentStartsApplyOnce(t *testing.T) {
	ctx := context.Background()
	database := testdb.NewEmpty(t)
	const n = 3
	var wg sync.WaitGroup
	results := make([]pgschema.MigrateResult, n)
	errs := make([]error, n)
	for i := range n {
		db := database.Open(t, "migrate-concurrent")
		wg.Go(func() { results[i], errs[i] = pgschema.Migrate(ctx, db) })
	}
	wg.Wait()
	applied := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("并发迁移 %d 失败: %v", i, errs[i])
		}
		applied += len(results[i].Applied)
	}
	if applied != 1 {
		t.Fatalf("并发迁移共应用 %d 次，期望 1", applied)
	}
}

func TestMigrateRejectsNewerDatabase(t *testing.T) {
	ctx := context.Background()
	database := testdb.New(t)
	db := database.Open(t, "migrate-newer")
	if err := pgschema.RequireCurrent(ctx, db); err != nil {
		t.Fatalf("模板克隆库应已是最新: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO wscyber_migrations (name, group_id) VALUES ('99990101000000', 2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pgschema.Migrate(ctx, db); !errors.Is(err, pgschema.ErrDatabaseNewer) {
		t.Fatalf("库较新时 Migrate 应拒绝: %v", err)
	}
	if err := pgschema.RequireCurrent(ctx, db); !errors.Is(err, pgschema.ErrDatabaseNewer) {
		t.Fatalf("库较新时 RequireCurrent 应拒绝: %v", err)
	}
	status, err := pgschema.Status(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(status.Unknown, []string{"99990101000000"}) || status.Current() {
		t.Fatalf("状态 %+v", status)
	}
	if instance, err := pgschema.Start(ctx, database.URL, nil); !errors.Is(err, pgschema.ErrDatabaseNewer) {
		_ = instance.Close()
		t.Fatalf("库较新时 Start 应拒绝: %v", err)
	}
}

func TestMigrateRequiresTwoConnections(t *testing.T) {
	database := testdb.NewEmpty(t)
	db, err := pgschema.OpenDB(database.URL, "one-conn", pgschema.PoolOptions{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := pgschema.Migrate(context.Background(), db); err == nil {
		t.Fatal("单连接池应拒绝迁移（否则锁连接与迁移连接互等）")
	}
}
