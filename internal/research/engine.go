// Package research 提供由持久状态驱动的有界研究与动作执行循环。
package research

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/model"
)

// Config 分别限制研究和动作 worker；等待审批不占用任何 worker。
type Config struct {
	MaxParallel   int
	MaxSteps      int
	PollInterval  time.Duration
	ModelTimeout  time.Duration
	ActionTimeout time.Duration
}

// Engine 不持有第二份任务状态；每次派发都由 Repository 原子认领。
type Engine struct {
	repo   domain.Repository
	model  domain.Model
	exec   domain.Executor
	config Config
}

func New(repo domain.Repository, model domain.Model, exec domain.Executor, config Config) *Engine {
	if config.MaxParallel <= 0 {
		config.MaxParallel = 2
	}
	if config.MaxSteps <= 0 {
		config.MaxSteps = 12
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Second
	}
	// 防止错误配置把空队列查询变成忙循环。
	if config.PollInterval < 10*time.Millisecond {
		config.PollInterval = 10 * time.Millisecond
	}
	if config.ModelTimeout <= 0 {
		config.ModelTimeout = 60 * time.Second
	}
	if config.ActionTimeout <= 0 {
		config.ActionTimeout = 60 * time.Second
	}
	return &Engine{repo: repo, model: model, exec: exec, config: config}
}

// Run 阻塞到取消并等待在途 worker 保存回执；保存有独立的有限时限。
// 未配置模型时不认领研究，但仍执行已批准动作。Recover 由主入口负责。
func (e *Engine) Run(ctx context.Context) {
	if e.repo == nil {
		slog.Error("研究引擎未配置持久层，不派发工作")
		<-ctx.Done()
		return
	}
	var workers sync.WaitGroup
	start := func(claimAndRun func(context.Context) bool) {
		workers.Go(func() {
			for ctx.Err() == nil {
				if claimAndRun(ctx) {
					continue
				}
				timer := time.NewTimer(e.config.PollInterval)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	for range e.config.MaxParallel {
		if e.model != nil {
			start(e.claimResearch)
		}
		if e.exec != nil {
			start(e.claimAction)
		}
	}
	<-ctx.Done()
	workers.Wait()
}

func (e *Engine) claimResearch(ctx context.Context) bool {
	attempt, err := e.repo.ClaimThread(ctx, e.config.MaxSteps)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("认领研究失败")
		}
		return false
	}
	if attempt == nil {
		return false
	}
	e.runResearch(ctx, *attempt)
	return true
}

func (e *Engine) runResearch(parent context.Context, attempt domain.Attempt) {
	ctx, cancel := context.WithTimeout(parent, e.config.ModelTimeout)
	defer cancel()
	var snapshot domain.Snapshot
	var err error
	if repo, ok := e.repo.(domain.ResearchContextRepository); ok {
		snapshot, err = repo.ResearchScope(ctx, attempt.ThreadID)
	} else {
		snapshot, err = e.repo.Snapshot(ctx, attempt.ThreadID)
	}
	if err != nil {
		e.failAttempt(parent, attempt, "", "读取研究快照失败，未调用模型")
		return
	}
	decision, err := decide(ctx, e.model, snapshot, attempt.ID)
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		e.failAttempt(parent, attempt, snapshot.Thread.NextStep, "模型决策失败："+model.SafeErrorMessage(err))
		return
	}
	if err := model.ValidateDecisionForSnapshot(decision, snapshot); err != nil {
		e.failAttempt(parent, attempt, snapshot.Thread.NextStep, err.Error())
		return
	}
	persist, persistCancel := persistenceContext(parent)
	defer persistCancel()
	attemptStatus, threadStatus := "succeeded", "ready"
	switch decision.Kind {
	case "observe":
		_, err = e.repo.AddObservation(persist, domain.Observation{
			ThreadID: attempt.ThreadID, AttemptID: attempt.ID,
			Kind: "model_observation", Content: decision.Observation,
		})
	case "delegate":
		// 首期子方向保持 auto_run=false，由用户显式启用；不扩大授权。
		_, err = e.repo.CreateThread(persist, domain.Thread{
			MissionID: snapshot.Mission.ID,
			Title:     decision.Title, Question: decision.Question,
			Rationale: fmt.Sprintf("源研究方向 %s：%s", attempt.ThreadID, decision.Rationale),
			Priority:  snapshot.Thread.Priority, AutoRun: false,
		})
	case "action":
		if e.exec == nil {
			err = errors.New("动作执行能力未配置")
		} else if err = validate(e.exec, *decision.Action, snapshot.Mission.AllowedOrigins); err == nil {
			var action domain.Action
			action, err = e.repo.ProposeAction(persist, attempt.ThreadID, attempt.ID, *decision.Action)
			if err == nil && action.Status == "pending" {
				attemptStatus, threadStatus = "waiting_approval", "waiting_approval"
			}
		}
	case "verification":
		if repo, ok := e.repo.(domain.VerificationRepository); ok {
			_, err = repo.ApplyVerification(persist, attempt.ID, *decision.Verification)
		} else {
			err = domain.ErrUnavailable
		}
		if err != nil {
			if stopper, ok := e.model.(interface{ StopDispatch() }); ok {
				stopper.StopDispatch()
			}
		}
		threadStatus = "concluded"
	case "finding":
		if repo, ok := e.repo.(domain.FindingRepository); ok {
			_, err = repo.ApplyFinding(persist, attempt.ID, *decision.Finding)
		} else {
			err = domain.ErrUnavailable
		}
		if err != nil {
			if stopper, ok := e.model.(interface{ StopDispatch() }); ok {
				stopper.StopDispatch()
			}
		}
	case "skill_discover", "skill_load", "skill_release":
		if repo, ok := e.repo.(domain.SkillRepository); ok {
			_, err = repo.ApplySkillActivity(persist, attempt.ID, decision.Kind, *decision.Skill)
		} else {
			err = domain.ErrUnavailable
		}
		if err != nil {
			if stopper, ok := e.model.(interface{ StopDispatch() }); ok {
				stopper.StopDispatch()
			}
		}
	case "conclude":
		threadStatus = "concluded"
	case "block":
		threadStatus = "blocked"
	}
	if err != nil {
		e.failAttempt(parent, attempt, snapshot.Thread.NextStep, "保存研究活动或校验动作失败；未决方向保留，请检查持久层及授权参数")
		return
	}
	if err := e.repo.FinishAttempt(persist, attempt.ID, attemptStatus, threadStatus, decision.Summary, decision.NextStep); err != nil {
		// 已保存的观察/动作不回滚或重放；未确认的 attempt 留给启动恢复。
		slog.Error("未能确认研究活动状态已保存，不重复执行本轮", "attempt_id", attempt.ID)
	}
}

func (e *Engine) failAttempt(parent context.Context, attempt domain.Attempt, nextStep, reason string) {
	ctx, cancel := persistenceContext(parent)
	defer cancel()
	if nextStep == "" {
		nextStep = "保留原问题，人工检查失败原因与已有证据后恢复研究"
	}
	if err := e.repo.FinishAttempt(ctx, attempt.ID, "failed", "blocked", reason, nextStep); err != nil {
		slog.Error("未能确认研究失败状态已保存", "attempt_id", attempt.ID)
	}
}

func (e *Engine) claimAction(ctx context.Context) bool {
	action, err := e.repo.ClaimAction(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("认领动作失败")
		}
		return false
	}
	if action == nil {
		return false
	}
	e.runAction(ctx, *action)
	return true
}

func (e *Engine) runAction(parent context.Context, action domain.Action) {
	ctx, cancel := context.WithTimeout(parent, e.config.ActionTimeout)
	defer cancel()
	var result domain.ActionResult
	var runErr error
	status := "failed"
	snapshot, err := e.repo.Snapshot(ctx, action.ThreadID)
	if err != nil {
		runErr = errors.New("读取动作所属项目失败，未发送动作")
	} else if err = validate(e.exec, action.Input, snapshot.Mission.AllowedOrigins); err != nil {
		runErr = fmt.Errorf("执行前校验失败，未发送动作：%w", err)
	} else if ctx.Err() != nil {
		runErr = errors.New("执行前已取消或超时，未发送动作")
	} else {
		// ClaimAction 已持久化 running。外部执行永不由引擎自动重试。
		result, runErr = execute(ctx, e.exec, action.Input, snapshot.Mission.AllowedOrigins)
		if runErr != nil && (ctx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)) {
			result.EffectUnknown = true
		}
		if result.EffectUnknown {
			status = "outcome_unknown"
			if runErr == nil {
				runErr = errors.New("执行效果未知，禁止自动重放；需人工核对")
			}
		} else if runErr == nil {
			status = "succeeded"
		}
	}
	errText := ""
	if runErr != nil {
		errText = runErr.Error()
	}
	// 执行回执和观察先由同一个 Repository 事务保存，再由持久层释放研究。
	// 即便动作 deadline 或进程取消也尝试保存；不使用已取消的执行上下文。
	persist, persistCancel := persistenceContext(parent)
	defer persistCancel()
	if err := e.repo.FinishAction(persist, action.ID, status, result, errText); err != nil {
		slog.Error("未能确认执行回执已保存，不重放动作；重启后须按未知效果核对", "action_id", action.ID)
	}
}

func persistenceContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
}

func decide(ctx context.Context, m domain.Model, snapshot domain.Snapshot, attemptIDs ...string) (decision domain.Decision, err error) {
	defer func() {
		if recover() != nil {
			decision = domain.Decision{}
			err = errors.New("模型适配器异常，未产生有效决策")
		}
	}()
	if actual, ok := m.(domain.AttemptModel); ok && len(attemptIDs) > 0 {
		return actual.DecideAttempt(ctx, snapshot, attemptIDs[0])
	}
	return m.Decide(ctx, snapshot)
}

func validate(exec domain.Executor, input domain.ActionInput, origins []string) (err error) {
	defer func() {
		if recover() != nil {
			err = errors.New("动作校验器异常，未发送动作")
		}
	}()
	if exec.Validate(input, origins) != nil {
		return errors.New("执行器拒绝动作参数或授权范围")
	}
	return nil
}

func execute(ctx context.Context, exec domain.Executor, input domain.ActionInput, origins []string) (result domain.ActionResult, err error) {
	defer func() {
		if recover() != nil {
			result.EffectUnknown = true
			err = errors.New("执行器异常，效果未知，禁止自动重放")
		}
	}()
	result, err = exec.Execute(ctx, input, origins)
	if err != nil {
		safe := execution.SafeErrorMessage(err)
		switch {
		case errors.Is(err, context.Canceled):
			err = fmt.Errorf("%s: %w", safe, context.Canceled)
		case errors.Is(err, context.DeadlineExceeded):
			err = fmt.Errorf("%s: %w", safe, context.DeadlineExceeded)
		default:
			err = errors.New(safe)
		}
	}
	return result, err
}
