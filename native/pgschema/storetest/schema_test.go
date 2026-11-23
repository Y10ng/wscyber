package storetest_test

import (
	"context"
	"strings"
	"testing"

	service "github.com/chainreactors/cyber/pkg/web/service"

	"wscyber/native/internal/testdb"
	"wscyber/native/webapi"
)

// Hub 的实际组合：核心表 + webapi 44 表，基线已全部建好。
func TestOpenWithWebAPIModule(t *testing.T) {
	f := newFixture(t, webapi.SchemaModule())
	createSession(t, context.Background(), f.store, "s1")
	if _, err := f.store.GetSession(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
}

// 存储不建表也不改表：声明的列缺失时报错，库保持原样。
func TestOpenRejectsMissingColumnWithoutModification(t *testing.T) {
	db := testdb.New(t)
	raw := db.Open(t, "storetest")
	ctx := context.Background()
	if _, err := raw.ExecContext(ctx, `ALTER TABLE chat_aop_events RENAME COLUMN event_proto TO event_json`); err != nil {
		t.Fatal(err)
	}
	_, err := service.NewSQLiteStore(db.DSN())
	if err == nil || !strings.Contains(err.Error(), "chat_aop_events columns") || !strings.Contains(err.Error(), "event_proto") {
		t.Fatalf("NewSQLiteStore = %v", err)
	}
	if _, err := raw.ExecContext(ctx, `SELECT event_json FROM chat_aop_events`); err != nil {
		t.Fatalf("store changed the rejected schema: %v", err)
	}
}

func TestOpenRejectsMissingModuleTables(t *testing.T) {
	db := testdb.New(t)
	// 基线不含 scans/session_scans（Hub 不启用扫描台）。
	_, err := service.NewSQLiteStore(db.DSN(), service.ScanSchema)
	if err == nil || !strings.Contains(err.Error(), "scans") {
		t.Fatalf("NewSQLiteStore(ScanSchema) = %v", err)
	}
	raw := db.Open(t, "storetest")
	if _, err := raw.ExecContext(context.Background(), `DROP TABLE raw_artifacts`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.NewSQLiteStore(db.DSN()); err == nil || !strings.Contains(err.Error(), "raw_artifacts") {
		t.Fatalf("missing core table = %v", err)
	}
}

// 多出的列与表（tenant_id、tenants、迁移表、未声明的扩展表）是允许的。
func TestOpenAllowsExtraColumnsAndTables(t *testing.T) {
	db := testdb.New(t)
	raw := db.Open(t, "storetest")
	if _, err := raw.ExecContext(context.Background(), `ALTER TABLE chat_sessions ADD COLUMN future_note text; CREATE TABLE future_table (id text)`); err != nil {
		t.Fatal(err)
	}
	store, err := service.NewSQLiteStore(db.DSN())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createSession(t, context.Background(), store, "s1")
}

func TestOpenRejectsUnmigratedDatabase(t *testing.T) {
	db := testdb.NewEmpty(t)
	_, err := service.NewSQLiteStore(db.DSN())
	if err == nil || !strings.Contains(err.Error(), "not migrated") {
		t.Fatalf("empty database = %v", err)
	}
	raw := db.Open(t, "storetest")
	ctx := context.Background()
	// 迁移表存在但没有已应用版本。
	if _, err := raw.ExecContext(ctx, `CREATE TABLE wscyber_migrations (id bigserial PRIMARY KEY, name text NOT NULL, group_id bigint NOT NULL, migrated_at timestamptz NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	_, err = service.NewSQLiteStore(db.DSN())
	if err == nil || !strings.Contains(err.Error(), "no applied migrations") {
		t.Fatalf("empty migrations table = %v", err)
	}
	var tables int
	if err := raw.NewRaw(`SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public'`).Scan(ctx, &tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Fatalf("store created tables in an unmigrated database: %d", tables)
	}
}

func TestOpenErrorsDoNotEchoConnectionString(t *testing.T) {
	for _, dsn := range []string{"", "postgres://user:s3cr3t-pw@[::1", "host=127.0.0.1 port=notaport password=s3cr3t-pw"} {
		_, err := service.NewSQLiteStore(dsn)
		if err == nil {
			t.Fatalf("NewSQLiteStore(len=%d) succeeded", len(dsn))
		}
		if strings.Contains(err.Error(), "s3cr3t-pw") {
			t.Fatalf("error echoes the password: %v", err)
		}
	}
	// 可解析但连不上：错误也不含口令。
	_, err := service.NewSQLiteStore("postgres://user:s3cr3t-pw@127.0.0.1:1/x?sslmode=disable&connect_timeout=2")
	if err == nil || strings.Contains(err.Error(), "s3cr3t-pw") {
		t.Fatalf("unreachable = %v", err)
	}
}
