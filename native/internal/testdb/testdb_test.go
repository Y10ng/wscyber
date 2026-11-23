package testdb_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/uptrace/bun"

	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func TestParallelDatabasesAreIsolatedAndDropped(t *testing.T) {
	var mu sync.Mutex
	var names []string
	t.Run("group", func(t *testing.T) {
		for _, id := range []string{"task-one", "task-two"} {
			t.Run(id, func(t *testing.T) {
				t.Parallel()
				ctx := context.Background()
				database := testdb.New(t)
				mu.Lock()
				names = append(names, database.Name)
				mu.Unlock()
				if !strings.HasPrefix(database.Name, testdb.DatabasePrefix) || database.URL.Database() != database.Name {
					t.Fatalf("库名 %s / %s", database.Name, database.URL)
				}
				db := database.Open(t, "testdb-test")
				// 克隆自已迁移模板：schema 已是最新。
				if err := pgschema.RequireCurrent(ctx, db); err != nil {
					t.Fatal(err)
				}
				err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
					if _, err := tx.ExecContext(ctx, `SELECT set_config('app.tenant_id', 'default', true)`); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, `INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES (?, '', '', '[]', '', 'x')`, id)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				var ids []string
				rows, err := db.QueryContext(ctx, `SELECT id FROM webapi_tasks`)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				for rows.Next() {
					var got string
					if err := rows.Scan(&got); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, got)
				}
				if len(ids) != 1 || ids[0] != id {
					t.Fatalf("并行测试库互相可见: %v", ids)
				}
			})
		}
	})
	if len(names) != 2 || names[0] == names[1] {
		t.Fatalf("库名 %v", names)
	}
	for _, name := range names {
		exists, err := testdb.Exists(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("测试结束后库 %s 未删除", name)
		}
	}
}

func TestNewEmptyIsNotMigrated(t *testing.T) {
	ctx := context.Background()
	db := testdb.NewEmpty(t).Open(t, "testdb-empty")
	status, err := pgschema.Status(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if status.Initialized {
		t.Fatalf("空库不应有迁移表: %+v", status)
	}
}

func TestMissingEnvExplainsSetup(t *testing.T) {
	t.Setenv(testdb.EnvURL, "")
	_, err := testdb.AdminURL()
	if err == nil {
		t.Fatal("未设置时应报错")
	}
	for _, want := range []string{testdb.EnvURL, "pg-test.env", "CREATEDB", "不跳过"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("提示缺少 %q: %v", want, err)
		}
	}
}
