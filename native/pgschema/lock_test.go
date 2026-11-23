package pgschema_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"wscyber/native/internal/testdb"
	"wscyber/native/pgschema"
)

func TestInstanceLockSingleHolder(t *testing.T) {
	ctx := context.Background()
	database := testdb.New(t)
	first := database.Open(t, "lock-a")
	second := database.Open(t, "lock-b")

	lock, err := pgschema.AcquireInstanceLock(ctx, first.DB)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Check(ctx); err != nil {
		t.Fatal(err)
	}
	// 另一个池（另一个“进程”）与同一个池里的另一条连接都拿不到。
	if _, err := pgschema.AcquireInstanceLock(ctx, second.DB); !errors.Is(err, pgschema.ErrInstanceLocked) {
		t.Fatalf("第二个持有者应失败: %v", err)
	}
	if _, err := pgschema.AcquireInstanceLock(ctx, first.DB); !errors.Is(err, pgschema.ErrInstanceLocked) {
		t.Fatalf("同池第二条连接应失败: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal("重复 Release 应无害")
	}
	if err := lock.Check(ctx); err == nil {
		t.Fatal("释放后 Check 应报错")
	}
	again, err := pgschema.AcquireInstanceLock(ctx, second.DB)
	if err != nil {
		t.Fatalf("释放后应可再取: %v", err)
	}
	defer again.Release()

	// 锁按库隔离：另一个库不受影响。
	other := testdb.New(t).Open(t, "lock-other")
	otherLock, err := pgschema.AcquireInstanceLock(ctx, other.DB)
	if err != nil {
		t.Fatalf("不同库的锁应互不影响: %v", err)
	}
	_ = otherLock.Release()
}

func TestStartSequence(t *testing.T) {
	ctx := context.Background()
	database := testdb.NewEmpty(t)
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	instance, err := pgschema.Start(ctx, database.URL, logf)
	if err != nil {
		t.Fatal(err)
	}
	if err := pgschema.RequireCurrent(ctx, instance.DB()); err != nil {
		t.Fatal(err)
	}
	if err := instance.Lock().Check(ctx); err != nil {
		t.Fatal(err)
	}
	// 控制池只用 2 条（单实例锁连接兼作迁移锁连接 + 迁移连接）；空库迁移不自锁。
	if got := instance.DB().Stats().MaxOpenConnections; got != 2 {
		t.Fatalf("控制池上限 %d", got)
	}
	// 迁移锁已释放：另一个池可立即取得。
	probe, err := database.Open(t, "migrate-lock-probe").DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gotMigrateLock bool
	if err := probe.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`, int32(0x77736379), int32(2)).Scan(&gotMigrateLock); err != nil || !gotMigrateLock {
		t.Fatalf("Start 后迁移锁应已释放: %v %v", gotMigrateLock, err)
	}
	if _, err := probe.ExecContext(ctx, `SELECT pg_advisory_unlock($1, $2)`, int32(0x77736379), int32(2)); err != nil {
		t.Fatal(err)
	}
	_ = probe.Close()
	if _, err := pgschema.Start(ctx, database.URL, nil); !errors.Is(err, pgschema.ErrInstanceLocked) {
		t.Fatalf("同库第二个 Hub 应因单实例锁失败: %v", err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	if err := instance.Close(); err != nil {
		t.Fatal("重复 Close 应无害")
	}
	restarted, err := pgschema.Start(ctx, database.URL, nil)
	if err != nil {
		t.Fatalf("前一个关闭后应可再启动: %v", err)
	}
	defer restarted.Close()
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "PostgreSQL") {
		t.Fatalf("日志 %q", joined)
	}
	if strings.Contains(joined, database.DSN()) {
		t.Fatal("日志不得包含连接串")
	}
}
