// Package modelprobe 编排显式连通测试；不读取研究材料，不承担研究模型选择。
package modelprobe

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelledger"
	"wscyber/internal/modelprovider"
)

// RequestVersion=1 的内容固定；数据库同时保存用途与版本，升级时不得悄悄重放。
const UserMessage = "Reply with OK."

type Service struct {
	repo   domain.ModelTestRepository
	models *modelconfig.Manager
	ledger *modelledger.Service
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
	active atomic.Bool
	gate   sync.RWMutex // 退出清理必须等已开始的有界受理完成。
	err    error        // 只在done关闭后读取。
}

// New 必须在Store.Open/Recover之后调用；只启动一个受进程context管理的worker。
func New(ctx context.Context, repo domain.ModelTestRepository, models *modelconfig.Manager, ledgers ...*modelledger.Service) *Service {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{repo: repo, models: models, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1)}
	if len(ledgers) > 0 {
		s.ledger = ledgers[0]
	}
	s.active.Store(true)
	go s.run()
	return s
}

func (s *Service) Done() <-chan struct{} { return s.done }
func (s *Service) Err() error            { return s.err } // 只在Done后调用。
func (s *Service) Close(ctx context.Context) error {
	s.cancel()
	select {
	case <-s.done:
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validIdentity(configID, id string, revision int64) bool {
	return modelconfig.ValidID(configID) && modelconfig.ValidID(id) && revision >= 1 && revision <= 1<<53-1
}

func (s *Service) Get(ctx context.Context, configID, id string) (domain.ModelTest, error) {
	var zero domain.ModelTest
	if !modelconfig.ValidID(configID) || !modelconfig.ValidID(id) {
		return zero, domain.ErrInvalid
	}
	if s == nil || s.repo == nil {
		return zero, domain.ErrUnavailable
	}
	r, err := s.repo.GetModelTest(ctx, id)
	if err == nil && r.ConfigID != configID {
		return zero, domain.ErrNotFound
	}
	return r, err
}

func (s *Service) Submit(ctx context.Context, configID, id string, revision int64) (domain.ModelTest, bool, error) {
	var zero domain.ModelTest
	if !validIdentity(configID, id, revision) {
		return zero, false, domain.ErrInvalid
	}
	if s == nil || s.repo == nil {
		return zero, false, domain.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// 已提交身份优先，不依赖密钥、配置启用/当前版本、worker或队列容量。
	old, err := s.repo.GetModelTest(ctx, id)
	if err == nil {
		if old.ConfigID != configID || old.ConfigRevision != revision {
			return zero, false, domain.ErrConflict
		}
		return old, false, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return zero, false, domain.ErrUnavailable
	}
	s.gate.RLock()
	defer s.gate.RUnlock()
	if !s.active.Load() || s.ctx.Err() != nil || !s.models.Available() {
		return zero, false, domain.ErrUnavailable
	}
	// 在受理事务内再次优先核对身份，再验证被锁定的配置。
	// 不能在事务前因配置改版/禁用返回，从而遮蔽并发刚提交的同ID回执。
	r, created, err := s.repo.AcceptModelTest(ctx, configID, id, revision, s.models.ValidateRecordForUse)
	if err == nil && created {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
	return r, created, err
}

func (s *Service) run() {
	defer func() {
		s.active.Store(false)
		s.cancel()
		s.gate.Lock()
		defer s.gate.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.repo.InterruptModelTests(ctx); err != nil {
			s.err = errors.Join(s.err, persistenceFailure("退出收尾", err))
		}
		close(s.done)
	}()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if s.ctx.Err() != nil {
			return
		}
		r, err := s.repo.ClaimModelTest(s.ctx)
		if errors.Is(err, domain.ErrNotFound) {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			case <-ticker.C:
			}
			continue
		}
		if err != nil {
			if s.ctx.Err() == nil {
				s.err = persistenceFailure("领取回执", err)
			}
			return // 领取结果未知也不发送、不重领。
		}
		result := modelprovider.Result{Outcome: modelprovider.NotSent}
		doc, err := s.models.ResolveVersion(s.ctx, r.ConfigID, r.ConfigRevision)
		if err == nil && s.ctx.Err() == nil {
			result, err = s.ledger.Complete(s.ctx, modelledger.Identity{Source: "managed", ConfigID: r.ConfigID, ConfigRevision: r.ConfigRevision, ConfigName: doc.Name, TestID: r.ID}, doc, modelprovider.Input{User: UserMessage, MaxOutputTokens: min(32, doc.MaxOutputTokens)}, nil)
			if err != nil {
				s.err = persistenceFailure("保存调用账本", err)
				return
			}
		}
		// 绝不传递Text；HTTP请求取消不取消worker，进程取消也使用独立清理context写回。
		var status *int
		if result.HTTPStatus != 0 {
			status = &result.HTTPStatus
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = s.repo.CompleteModelTest(cleanup, r.ID, result.Outcome, status, &result.Attempted)
		cancel()
		if err != nil {
			s.err = persistenceFailure("保存结果", err)
			return // 落库失败保留unknown事实，不重发。
		}
	}
}

// persistenceFailure 仅保留固定阶段与错误类别，不包装可能包含连接凭据或数据的原错误。
// Err/Close 都可能进入运行日志；仍以 ErrUnavailable 对外分类，不改变停止或恢复语义。
func persistenceFailure(stage string, cause error) error {
	kind := "数据库异常"
	switch {
	case errors.Is(cause, context.DeadlineExceeded):
		kind = "超时"
	case errors.Is(cause, context.Canceled):
		kind = "取消"
	case errors.Is(cause, domain.ErrConflict):
		kind = "状态或单实例锁冲突"
	case errors.Is(cause, domain.ErrUnavailable):
		kind = "不可用"
	}
	return fmt.Errorf("%w：%s（%s）", domain.ErrUnavailable, stage, kind)
}
