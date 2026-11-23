// Package testdb 给测试提供隔离的 PostgreSQL 库（native/POSTGRES.md §7.1）。
//
// 用法：包内 TestMain 调 testdb.Main(m)；每个测试 testdb.New(t) 得到一个从
// 已迁移模板库克隆的新库（CREATE DATABASE … TEMPLATE），测试结束 DROP … WITH (FORCE)。
// 连接信息来自环境变量 WSCYBER_TEST_DATABASE_URL（有 CREATEDB 的非超级用户角色，
// 指向维护库如 postgres）；未设置时 Main 打印设置方法并以 1 退出，不跳过。
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"

	"wscyber/native/pgschema"
)

// EnvURL 是测试连接串环境变量。
const EnvURL = "WSCYBER_TEST_DATABASE_URL"

// 库名前缀；scripts/pg-test-clean.py 按此清理崩溃残留。
const (
	TemplatePrefix = "wscyber_tpl_"
	DatabasePrefix = "wscyber_t_"
)

const setupHelp = `testdb: 未设置 ` + EnvURL + `。
本项目测试需要本机 PostgreSQL（不跳过）。设置方法：
  source ~/.config/wscyber/pg-test.env
或导出指向维护库的连接串，角色须为非超级用户、无 BYPASSRLS、具 CREATEDB：
  export ` + EnvURL + `='postgres://<角色>:<口令>@127.0.0.1:5432/postgres?sslmode=disable'
本机准备步骤见 native/POSTGRES.md §7.2。`

var state struct {
	mu       sync.Mutex
	admin    pgschema.DatabaseURL
	template string
	ready    bool
	err      error
}

// Main 在包级 TestMain 中调用：建模板库并迁移、运行测试、删除模板库，然后退出。
func Main(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if err := setup(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	code := m.Run()
	if err := teardown(); err != nil {
		fmt.Fprintln(os.Stderr, "testdb: 删除模板库失败:", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// AdminURL 取测试维护库连接串；未设置环境变量时返回带设置方法的错误。
func AdminURL() (pgschema.DatabaseURL, error) {
	raw := strings.TrimSpace(os.Getenv(EnvURL))
	if raw == "" {
		return pgschema.DatabaseURL{}, errors.New(setupHelp)
	}
	u, err := pgschema.ParseDatabaseURL(raw)
	if err != nil {
		return pgschema.DatabaseURL{}, fmt.Errorf("testdb: %s: %w", EnvURL, err)
	}
	return u, nil
}

func setup() error {
	state.mu.Lock()
	defer state.mu.Unlock()
	admin, err := AdminURL()
	if err != nil {
		return err
	}
	state.admin = admin
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	name := fmt.Sprintf("%s%d_%s", TemplatePrefix, os.Getpid(), randomSuffix())
	if err := createDatabase(ctx, admin, name, "template0"); err != nil {
		return fmt.Errorf("testdb: 建模板库: %w", err)
	}
	state.template = name
	if err := migrateDatabase(ctx, admin, name); err != nil {
		return errors.Join(fmt.Errorf("testdb: 迁移模板库: %w", err), dropDatabase(context.Background(), admin, name))
	}
	state.ready = true
	return nil
}

func migrateDatabase(ctx context.Context, admin pgschema.DatabaseURL, name string) error {
	u, err := admin.WithDatabase(name)
	if err != nil {
		return err
	}
	db, err := pgschema.OpenDB(u, "wscyber-testdb", pgschema.PoolOptions{MaxOpenConns: 2, MaxIdleConns: 1})
	if err != nil {
		return err
	}
	defer db.Close()
	if err := pgschema.CheckRole(ctx, db); err != nil {
		return err
	}
	_, err = pgschema.Migrate(ctx, db)
	return err
}

func teardown() error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.template == "" {
		return nil
	}
	err := dropDatabase(context.Background(), state.admin, state.template)
	state.template, state.ready = "", false
	return err
}

// Database 是一个测试专用库。
type Database struct {
	Name string
	URL  pgschema.DatabaseURL
}

// DSN 返回连接串（含口令），只用于连接，不要打印。
func (d Database) DSN() string { return d.URL.DSN() }

// New 克隆已迁移的模板库，测试结束时删除。须先在 TestMain 中调用 Main。
func New(t testing.TB) Database {
	t.Helper()
	state.mu.Lock()
	admin, template, ready := state.admin, state.template, state.ready
	state.mu.Unlock()
	if !ready {
		if _, err := AdminURL(); err != nil {
			t.Fatal(err)
		}
		t.Fatal("testdb: 模板库未就绪；请在本包 TestMain 中调用 testdb.Main(m)")
	}
	return create(t, admin, template)
}

// NewEmpty 建一个未迁移的空库（迁移测试用），测试结束时删除。不需要 Main。
func NewEmpty(t testing.TB) Database {
	t.Helper()
	admin, err := AdminURL()
	if err != nil {
		t.Fatal(err)
	}
	return create(t, admin, "template0")
}

func create(t testing.TB, admin pgschema.DatabaseURL, template string) Database {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	name := DatabasePrefix + randomSuffix()
	if err := createDatabase(ctx, admin, name, template); err != nil {
		t.Fatalf("testdb: 建测试库: %v", err)
	}
	t.Cleanup(func() {
		if err := dropDatabase(context.Background(), admin, name); err != nil {
			t.Errorf("testdb: 删除测试库 %s: %v", name, err)
		}
	})
	u, err := admin.WithDatabase(name)
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	return Database{Name: name, URL: u}
}

// Open 打开连接池（Cleanup 时关闭，早于删库执行）。
func (d Database) Open(t testing.TB, application string) *bun.DB {
	t.Helper()
	db, err := pgschema.OpenDB(d.URL, application, pgschema.PoolOptions{MaxOpenConns: 4, MaxIdleConns: 2})
	if err != nil {
		t.Fatalf("testdb: 打开 %s: %v", d.Name, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Exists 报告指定库是否存在（清理测试用）。
func Exists(ctx context.Context, name string) (bool, error) {
	admin, err := AdminURL()
	if err != nil {
		return false, err
	}
	db, err := pgschema.OpenDB(admin, "wscyber-testdb", pgschema.PoolOptions{MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		return false, err
	}
	defer db.Close()
	var exists bool
	err = db.NewRaw(`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)`, name).Scan(ctx, &exists)
	return exists, err
}

func createDatabase(ctx context.Context, admin pgschema.DatabaseURL, name, template string) error {
	return withAdmin(ctx, admin, func(db *bun.DB) error {
		_, err := db.ExecContext(ctx, `CREATE DATABASE ? TEMPLATE ?`, bun.Ident(name), bun.Ident(template))
		return err
	})
}

func dropDatabase(ctx context.Context, admin pgschema.DatabaseURL, name string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return retryDrop(ctx, dropRetryAttempts, dropRetryDelay, func() error {
		return withAdmin(ctx, admin, func(db *bun.DB) error {
			_, err := db.ExecContext(ctx, `DROP DATABASE IF EXISTS ? WITH (FORCE)`, bun.Ident(name))
			return err
		})
	})
}

// 删库重试参数。PG18 起终止 autovacuum worker 需要 pg_signal_autovacuum_worker，
// 非超级用户执行 DROP DATABASE … WITH (FORCE) 时若恰有 autovacuum worker 连在
// 新库上，会报 42501（permission denied to terminate process）。worker 很快结束，
// 短暂重试即可；其它错误不重试。
const (
	dropRetryAttempts = 10
	dropRetryDelay    = 200 * time.Millisecond
)

// retryDrop 执行 drop，遇 42501 时最多共尝试 attempts 次、每次间隔 delay。
func retryDrop(ctx context.Context, attempts int, delay time.Duration, drop func() error) error {
	var err error
	for attempt := 1; ; attempt++ {
		err = drop()
		if err == nil || !isInsufficientPrivilege(err) || attempt >= attempts {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.Join(err, ctx.Err())
		case <-time.After(delay):
		}
	}
}

func isInsufficientPrivilege(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func withAdmin(ctx context.Context, admin pgschema.DatabaseURL, fn func(*bun.DB) error) error {
	db, err := pgschema.OpenDB(admin, "wscyber-testdb", pgschema.PoolOptions{MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		return err
	}
	defer db.Close()
	return fn(db)
}

func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
