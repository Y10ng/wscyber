package modelprobe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
)

func TestPersistenceFailureSanitizes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		kind  string
	}{
		{"timeout", context.DeadlineExceeded, "超时"},
		{"cancel", context.Canceled, "取消"},
		{"conflict", domain.ErrConflict, "状态或单实例锁冲突"},
		{"unavailable", domain.ErrUnavailable, "不可用"},
		{"database", errors.New("private-record"), "数据库异常"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := fmt.Errorf("postgres://user:private-password@host/db private-record: %w", tc.cause)
			err := persistenceFailure("领取回执", cause)
			if !errors.Is(err, domain.ErrUnavailable) || !strings.Contains(err.Error(), "领取回执（"+tc.kind+"）") {
				t.Fatalf("错误分类或阶段丢失: %v", err)
			}
			if strings.Contains(err.Error(), "private-") || strings.Contains(err.Error(), "postgres://") || errors.Is(err, cause) {
				t.Fatal("运行诊断不可包含或包装原始数据库错误")
			}
		})
	}
}

type failureRepository struct {
	domain.ModelTestRepository // 未使用受理/读取；若意外调用应让测试失败。
	claim                      func(context.Context) (domain.ModelTest, error)
	completeErr                error
	interruptErr               error
	claims                     atomic.Int32
	completions                atomic.Int32
	interrupts                 atomic.Int32
}

func (r *failureRepository) ClaimModelTest(ctx context.Context) (domain.ModelTest, error) {
	r.claims.Add(1)
	return r.claim(ctx)
}
func (r *failureRepository) CompleteModelTest(context.Context, string, string, *int, *bool) error {
	r.completions.Add(1)
	return r.completeErr
}
func (r *failureRepository) InterruptModelTests(context.Context) error {
	r.interrupts.Add(1)
	return r.interruptErr
}

func waitService(t *testing.T, s *Service) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("持久失败应停止worker，不重试认领或发送")
	}
}

func TestServicePreservesFailureStagesWithoutRetry(t *testing.T) {
	for _, stage := range []string{"领取回执", "保存结果"} {
		t.Run(stage, func(t *testing.T) {
			r := &failureRepository{
				completeErr:  errors.New("private-result"),
				interruptErr: fmt.Errorf("private-lock: %w", domain.ErrConflict),
			}
			r.claim = func(context.Context) (domain.ModelTest, error) {
				if stage == "领取回执" {
					return domain.ModelTest{}, fmt.Errorf("private-database: %w", context.DeadlineExceeded)
				}
				return domain.ModelTest{ID: "0123456789abcdef0123456789abcdef", ConfigID: "fedcba9876543210fedcba9876543210", ConfigRevision: 1}, nil
			}
			// nil管理器不具备凭据，始终不外发；这里验证执行服务的持久失败处理。
			s := New(context.Background(), r, nil)
			t.Cleanup(func() { s.cancel() })
			waitService(t, s)
			err := s.Err()
			if !errors.Is(err, domain.ErrUnavailable) || !strings.Contains(err.Error(), stage) || !strings.Contains(err.Error(), "退出收尾") || strings.Contains(err.Error(), "private-") {
				t.Fatalf("首个失败与退出失败应同时保留安全阶段: %v", err)
			}
			if r.claims.Load() != 1 || r.interrupts.Load() != 1 {
				t.Fatal("失败后发生重复认领或重复收尾", r.claims.Load(), r.interrupts.Load())
			}
			wantComplete := int32(0)
			if stage == "保存结果" {
				wantComplete = 1
			}
			if r.completions.Load() != wantComplete {
				t.Fatal("结果保存次数不符", r.completions.Load())
			}
			if closeErr := s.Close(context.Background()); closeErr == nil || closeErr.Error() != err.Error() {
				t.Fatal("Close必须保持此前worker的失败事实", closeErr)
			}
		})
	}
}

func TestServiceCancellationDoesNotInventClaimFailure(t *testing.T) {
	for _, failCleanup := range []bool{false, true} {
		t.Run(fmt.Sprint(failCleanup), func(t *testing.T) {
			entered := make(chan struct{})
			r := &failureRepository{claim: func(ctx context.Context) (domain.ModelTest, error) {
				close(entered)
				<-ctx.Done()
				return domain.ModelTest{}, ctx.Err()
			}}
			if failCleanup {
				r.interruptErr = errors.New("private-cleanup")
			}
			s := New(context.Background(), r, nil)
			t.Cleanup(func() { s.cancel() })
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("worker未进入领取")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := s.Close(ctx)
			if !failCleanup {
				if err != nil {
					t.Fatal("正常取消不应报告持久故障", err)
				}
				return
			}
			if !errors.Is(err, domain.ErrUnavailable) || !strings.Contains(err.Error(), "退出收尾") || strings.Contains(err.Error(), "领取回执") || strings.Contains(err.Error(), "private-") {
				t.Fatal("只应报告真实退出收尾故障", err)
			}
		})
	}
}
