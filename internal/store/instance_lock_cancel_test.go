package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"wscyber/internal/domain"
)

// 锁查询被确定性延迟时，取消一个只读HTTP请求不能终结全服务持锁会话。
func TestInstanceLockSurvivesReadCancellation(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	// 仅专用连接的临时视图；不修改业务表，也不改变pg_catalog。
	_, err := s.lock.Exec(ctx, `CREATE TEMP VIEW pg_locks AS SELECT l.* FROM pg_catalog.pg_locks l CROSS JOIN pg_sleep(0.2)`)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = s.ListFindings(cancelled, th.MissionID, domain.FindingQuery{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected caller timeout, got %v", err)
	}
	if s.lock.IsClosed() {
		t.Fatal("取消只读请求关闭了专用单实例锁连接")
	}
	if _, err = s.lock.Exec(ctx, `DROP VIEW pg_temp.pg_locks`); err != nil {
		t.Fatal(err)
	}
	if err = s.ensureInstanceLock(ctx); err != nil {
		t.Fatalf("请求取消后持锁状态丢失: %v", err)
	}
	if _, err = s.ListFindings(ctx, th.MissionID, domain.FindingQuery{}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.ListFindings(ctx, th.MissionID, domain.FindingQuery{}); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
}

func TestInstanceLockLossStillFailsClosed(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	if _, err := s.lock.Exec(ctx, `SELECT pg_advisory_unlock_all()`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.ensureInstanceLock(ctx); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("真实失锁必须停止、不重获锁: %v", err)
		}
	}
}

func TestInstanceLockInternalTimeoutStillBounded(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	if _, err := s.lock.Exec(ctx, `CREATE TEMP VIEW pg_locks AS SELECT l.* FROM pg_catalog.pg_locks l CROSS JOIN pg_sleep(6)`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := s.ensureInstanceLock(ctx); err == nil {
		t.Fatal("未执行内部超时")
	}
	if elapsed := time.Since(started); elapsed < 4*time.Second || elapsed > 6*time.Second {
		t.Fatalf("锁检查超时不受界限约束: %v", elapsed)
	}
	if err := s.ensureInstanceLock(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("内部超时后连接失效未停止: %v", err)
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Store关闭未有界结束")
	}
}
