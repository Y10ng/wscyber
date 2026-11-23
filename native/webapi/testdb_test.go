package webapi

import (
	"context"
	"database/sql"
	"testing"

	"github.com/uptrace/bun"

	"wscyber/native/internal/testdb"
)

// 本包测试都用 PostgreSQL（native/POSTGRES.md §7.1）：TestMain 建已迁移的模板库，
// 每个测试 newTestHub 克隆一个独立库，另配专属 state-dir（凭据密钥、出口 CA）。
func TestMain(m *testing.M) { testdb.Main(m) }

// testHub 是一个测试用的 Hub 数据位置：独立 PG 库 + state-dir。
type testHub struct {
	testdb.Database
	StateDir string
}

func newTestHub(t testing.TB) testHub {
	t.Helper()
	return testHub{Database: testdb.New(t), StateDir: t.TempDir()}
}

// db 是测试里直接执行 SQL 的入口（保留旧 *sql.DB 调用形状）：? 由 bun 格式化；
// 写语句在 write 事务里执行（与生产同身份与全局写锁），单行读在 read 事务里执行。
func (s *taskStore) db() testSQL { return testSQL{store: s} }

type testSQL struct{ store *taskStore }

func (d testSQL) Exec(query string, args ...any) (sql.Result, error) {
	return d.ExecContext(context.Background(), query, args...)
}

func (d testSQL) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var result sql.Result
	err := d.store.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		result, err = tx.ExecContext(ctx, query, args...)
		return err
	})
	return result, err
}

func (d testSQL) QueryRow(query string, args ...any) testRow {
	return d.QueryRowContext(context.Background(), query, args...)
}

func (d testSQL) QueryRowContext(ctx context.Context, query string, args ...any) testRow {
	return testRow{store: d.store, ctx: ctx, query: query, args: args}
}

// testRow 延迟到 Scan 时在只读事务里执行。
type testRow struct {
	store *taskStore
	ctx   context.Context
	query string
	args  []any
}

func (r testRow) Scan(dest ...any) error {
	return r.store.read(r.ctx, func(ctx context.Context, db bun.IDB) error {
		return db.QueryRowContext(ctx, r.query, r.args...).Scan(dest...)
	})
}

// databaseContains 逐表检查整行文本（bytea 列以 \x 十六进制出现，故同时找 needle 的十六进制）。
func databaseContains(ctx context.Context, store *taskStore, needle string) (bool, error) {
	found := false
	err := store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		var tables []string
		if err := db.NewRaw(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY 1`).Scan(ctx, &tables); err != nil {
			return err
		}
		for _, table := range tables {
			var hit bool
			if err := db.NewRaw(`SELECT EXISTS (SELECT 1 FROM ? AS t WHERE strpos(t::text, ?) > 0 OR strpos(t::text, encode(convert_to(?, 'UTF8'), 'hex')) > 0)`,
				bun.Ident(table), needle, needle).Scan(ctx, &hit); err != nil {
				return err
			}
			found = found || hit
		}
		return nil
	})
	return found, err
}
