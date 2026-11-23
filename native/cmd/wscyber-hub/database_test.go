package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"wscyber/native/internal/buildinfo"
	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
)

// 本包测试都用 PostgreSQL（native/POSTGRES.md §7.1）：TestMain 建已迁移的模板库，
// testOptions 为每个测试克隆一个独立库，另配专属 state-dir。
func TestMain(m *testing.M) { testdb.Main(m) }

// writeDatabaseURLFile 把测试库连接串写入 0600 临时文件（经 --database-url-file 传给 Hub）。
func writeDatabaseURLFile(t *testing.T, u pgschema.DatabaseURL) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "database.url")
	if err := os.WriteFile(path, []byte(u.DSN()+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// databasePassword 取连接串里的口令，用于“不泄露”断言。
func databasePassword(t *testing.T, u pgschema.DatabaseURL) string {
	t.Helper()
	cfg, err := pgx.ParseConfig(u.DSN())
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Password
}

// assertNoHubConnections 确认 Hub 停止后三个池（控制、原生、webapi）的连接都已关闭、单实例锁已释放。
func assertNoHubConnections(t *testing.T, u pgschema.DatabaseURL) {
	t.Helper()
	db, err := pgschema.OpenDB(u, "wscyber-test-probe", pgschema.PoolOptions{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var count int
		err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid() AND application_name LIKE 'wscyber-hub-%'`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Hub 停止后仍有 %d 条 Hub 连接", count)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// 没有连接串（旗标、两个环境变量都没有）即启动失败，且不创建 state-dir。
func TestHubRequiresDatabaseURL(t *testing.T) {
	t.Setenv(envDatabaseURLFile, "")
	t.Setenv(envDatabaseURL, "")
	opts, _ := testOptions(t)
	err := run(t.Context(), []string{"--local-hub", "--listen", "127.0.0.1:0", "--access-key-file", opts.keyFile, "--state-dir", opts.stateDir}, io.Discard)
	if !errors.Is(err, pgschema.ErrNoDatabaseURL) || !strings.Contains(err.Error(), "--database-url-file") || !strings.Contains(err.Error(), envDatabaseURL) {
		t.Fatalf("legacy without database: %v", err)
	}
	if _, err := os.Stat(opts.stateDir); !os.IsNotExist(err) {
		t.Fatal("legacy created state-dir without database")
	}
	for _, name := range []string{envDatabaseURLFile, envDatabaseURL} {
		if _, set := os.LookupEnv(name); set {
			t.Fatalf("%s not unset", name)
		}
	}
	oldNotices := notices
	t.Cleanup(func() { notices = oldNotices })
	notices = io.Discard
	state := filepath.Join(t.TempDir(), "serve-state")
	err = run(t.Context(), []string{"serve", "--listen", "127.0.0.1:0", "--state-dir", state}, io.Discard)
	if !errors.Is(err, pgschema.ErrNoDatabaseURL) {
		t.Fatalf("serve without database: %v", err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("serve created state-dir without database")
	}
}

// 同一库只允许一个 Hub：第二个因单实例锁启动失败（不建 state-dir）；第一个停止后可再启动。
func TestSecondHubSameDatabaseRejected(t *testing.T) {
	first, _ := testOptions(t)
	ctx, cancel := context.WithCancel(t.Context())
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- serveLocalHub(ctx, first, func(addr string) { ready <- addr }) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(15 * time.Second):
		t.Fatal("first hub not ready")
	}
	second, _ := testOptions(t)
	second.database = first.database
	err := serveLocalHub(t.Context(), second, func(string) { t.Error("second hub reported ready") })
	if !errors.Is(err, pgschema.ErrInstanceLocked) {
		t.Fatalf("second hub on same database: %v", err)
	}
	if _, err := os.Stat(second.stateDir); !os.IsNotExist(err) {
		t.Fatal("rejected second hub created state-dir")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("first hub did not stop")
	}
	assertNoHubConnections(t, first.database)
	ctx, cancel = context.WithCancel(t.Context())
	go func() { done <- serveLocalHub(ctx, second, func(addr string) { ready <- addr }) }()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("hub after release: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("hub after release not ready")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertNoHubConnections(t, first.database)
}

// 连接串与口令不出现在 stdout 事件、stderr 提示与错误里；ready 只含 host/port/database/user 与 schema。
func TestDatabaseURLNotExposed(t *testing.T) {
	oldNotices := notices
	t.Cleanup(func() { notices = oldNotices })
	var noticeText bytes.Buffer
	notices = &noticeText
	opts, _ := testOptions(t)
	dsn, password := opts.database.DSN(), databasePassword(t, opts.database)
	if password == "" {
		t.Fatal("测试连接串没有口令，无法核对泄露")
	}
	file := writeDatabaseURLFile(t, opts.database)

	reader, writer := io.Pipe()
	var stdout bytes.Buffer
	copied := make(chan struct{})
	go func() { _, _ = io.Copy(&stdout, reader); close(copied) }()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	readyLine := make(chan struct{})
	go func() {
		done <- runHub(ctx, options{localHub: true, listen: "127.0.0.1:0", keyFile: opts.keyFile, stateDir: opts.stateDir, databaseURLFile: file},
			io.MultiWriter(writer, notifyWriter{readyLine}), nil)
		writer.Close()
	}()
	select {
	case <-readyLine:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(15 * time.Second):
		t.Fatal("hub not ready")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-copied
	ready := firstEvent(t, stdout.Bytes())
	database, _ := ready["database"].(map[string]any)
	if database["host"] != opts.database.Host() || database["port"] != float64(opts.database.Port()) ||
		database["database"] != opts.database.Database() || database["user"] != opts.database.User() || len(database) != 4 {
		t.Fatalf("ready database: %v", ready["database"])
	}
	if ready["schema"] != float64(buildinfo.SchemaVersion) || buildinfo.SchemaVersion != 2 {
		t.Fatalf("ready schema: %v", ready["schema"])
	}
	if !strings.Contains(noticeText.String(), "PostgreSQL: postgres host=") {
		t.Fatalf("notices lack database location: %s", noticeText.String())
	}
	for name, text := range map[string]string{"stdout": stdout.String(), "notices": noticeText.String()} {
		if strings.Contains(text, dsn) || strings.Contains(text, password) {
			t.Fatalf("%s exposed database URL", name)
		}
	}

	// 失败路径：无法解析与无法连接的连接串，错误都不回显口令。
	const synthetic = "synthetic-db-pass-not-real"
	for _, bad := range []string{
		"postgres://wscyber:" + synthetic + "@127.0.0.1:notaport/wscyber",
		"postgres://wscyber:" + synthetic + "@127.0.0.1:1/wscyber?sslmode=disable&connect_timeout=2",
	} {
		badFile := filepath.Join(t.TempDir(), "bad.url")
		mustWrite(t, badFile, []byte(bad))
		failing, _ := testOptions(t)
		failing.database, failing.databaseURLFile = pgschema.DatabaseURL{}, badFile
		err := serveLocalHub(t.Context(), failing, func(string) { t.Error("bad database reported ready") })
		if err == nil || strings.Contains(err.Error(), synthetic) || strings.Contains(noticeText.String(), synthetic) {
			t.Fatalf("bad database accepted or leaked: %v", err)
		}
		if _, err := os.Stat(failing.stateDir); !os.IsNotExist(err) {
			t.Fatal("bad database created state-dir")
		}
	}
}

// 运行中单实例锁丢失（模拟 PG 重启：终止持锁会话）→ 记录提示、正常停止、以错误退出（无 stopped 事件）。
func TestInstanceLockLostStopsHub(t *testing.T) {
	oldInterval, oldNotices := instanceLockCheckInterval, notices
	t.Cleanup(func() { instanceLockCheckInterval, notices = oldInterval, oldNotices })
	instanceLockCheckInterval = 100 * time.Millisecond
	var noticeText lockedBuffer
	notices = &noticeText
	opts, _ := testOptions(t)
	hub := startRun(t, "--local-hub", "--listen", "127.0.0.1:0", "--access-key-file", opts.keyFile, "--state-dir", opts.stateDir,
		"--database-url-file", writeDatabaseURLFile(t, opts.database))
	if ready := hub.next(t); ready["event"] != "ready" {
		t.Fatalf("ready: %v", ready)
	}
	probe, err := pgschema.OpenDB(opts.database, "wscyber-test-probe", pgschema.PoolOptions{MaxOpenConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	var terminated int
	err = probe.DB.QueryRowContext(t.Context(), `SELECT count(pg_terminate_backend(pid)) FROM pg_locks
		WHERE locktype = 'advisory' AND granted AND classid = $1::int::oid AND objid = 1
		AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`, int32(0x77736379)).Scan(&terminated)
	if err != nil || terminated != 1 {
		t.Fatalf("terminate lock holder: %d %v", terminated, err)
	}
	var names []string
	for event := range hub.events {
		names = append(names, event["event"].(string))
	}
	select {
	case err = <-hub.done:
	case <-time.After(15 * time.Second):
		t.Fatal("hub did not stop after losing lock")
	}
	if !errors.Is(err, errInstanceLockLost) {
		t.Fatalf("exit error: %v (events %v)", err, names)
	}
	if len(names) != 0 {
		t.Fatalf("lock loss must not report stopped: %v", names)
	}
	if !strings.Contains(noticeText.String(), "单实例锁丢失") {
		t.Fatalf("notices: %s", noticeText.String())
	}
	assertNoHubConnections(t, opts.database)
}

// notifyWriter 在第一次写入（ready 事件）时关闭 ch。
type notifyWriter struct{ ch chan struct{} }

func (w notifyWriter) Write(p []byte) (int, error) {
	select {
	case <-w.ch:
	default:
		close(w.ch)
	}
	return len(p), nil
}

func firstEvent(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	line, _, _ := bytes.Cut(raw, []byte("\n"))
	var event map[string]any
	if err := json.Unmarshal(line, &event); err != nil || event["event"] != "ready" {
		t.Fatalf("first event: %s %v", line, err)
	}
	return event
}

// lockedBuffer 供后台协程（锁检查）写入提示。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
