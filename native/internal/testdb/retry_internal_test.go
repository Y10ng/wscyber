package testdb

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// 删库遇 42501（autovacuum worker 无法被非超级用户终止）时重试，其它错误立即返回。
func TestRetryDropOnInsufficientPrivilege(t *testing.T) {
	ctx := context.Background()
	denied := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "42501", Message: "permission denied to terminate process"})

	calls := 0
	err := retryDrop(ctx, 10, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return denied
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("应在第 3 次成功: calls=%d err=%v", calls, err)
	}

	calls = 0
	err = retryDrop(ctx, 4, time.Millisecond, func() error { calls++; return denied })
	if !isInsufficientPrivilege(err) || calls != 4 {
		t.Fatalf("应尝试 4 次后返回 42501: calls=%d err=%v", calls, err)
	}

	calls = 0
	other := &pgconn.PgError{Code: "55006"}
	err = retryDrop(ctx, 10, time.Millisecond, func() error { calls++; return other })
	if !errors.Is(err, other) || calls != 1 {
		t.Fatalf("非 42501 不应重试: calls=%d err=%v", calls, err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	calls = 0
	err = retryDrop(cancelled, 10, time.Hour, func() error { calls++; return denied })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("ctx 取消应停止重试: calls=%d err=%v", calls, err)
	}
}
