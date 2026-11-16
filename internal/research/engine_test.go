package research

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
)

type modelFunc func(context.Context, domain.Snapshot) (domain.Decision, error)

func (f modelFunc) Name() string { return "local-fake" }
func (f modelFunc) Decide(ctx context.Context, snapshot domain.Snapshot) (domain.Decision, error) {
	return f(ctx, snapshot)
}

type fakeExecutor struct {
	validate func(domain.ActionInput, []string) error
	execute  func(context.Context, domain.ActionInput, []string) (domain.ActionResult, error)
}

func (e fakeExecutor) Validate(input domain.ActionInput, origins []string) error {
	if e.validate != nil {
		return e.validate(input, origins)
	}
	return nil
}
func (e fakeExecutor) Execute(ctx context.Context, input domain.ActionInput, origins []string) (domain.ActionResult, error) {
	if e.execute != nil {
		return e.execute(ctx, input, origins)
	}
	return domain.ActionResult{}, nil
}

type attemptReceipt struct {
	id, attemptStatus, threadStatus, summary, nextStep string
}
type actionReceipt struct {
	id, status, message string
	result              domain.ActionResult
}

type fakeRepo struct {
	domain.Repository // 未涉及接口若意外被调用，会使测试明确失败。
	mu                sync.Mutex
	snapshot          domain.Snapshot
	attemptQueue      []domain.Attempt
	actionQueue       []domain.Action
	attemptReceipts   []attemptReceipt
	actionReceipts    []actionReceipt
	observations      []domain.Observation
	children          []domain.Thread
	proposalStatus    string
	proposals         []domain.ActionInput
	trace             []string
	claimThreads      int
	claimActions      int
	maxSteps          int
	snapshotErr       error
	observationErr    error
	finishActionErr   error
	canceledPersist   bool
	finished          chan string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		snapshot: domain.Snapshot{
			Mission:      domain.Mission{ID: "mission", AllowedOrigins: []string{"https://authorized.test"}},
			Thread:       domain.Thread{ID: "thread", MissionID: "mission", NextStep: "原有未决方向", Priority: 7},
			Actions:      []domain.Action{{ID: "past", Status: "failed", Error: "已记录失败"}},
			Observations: []domain.Observation{{Content: "已有观察"}},
		},
		finished: make(chan string, 100),
	}
}

func (r *fakeRepo) Snapshot(ctx context.Context, id string) (domain.Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace = append(r.trace, "snapshot")
	return r.snapshot, r.snapshotErr
}
func (r *fakeRepo) ClaimThread(ctx context.Context, maxSteps int) (*domain.Attempt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimThreads++
	r.maxSteps = maxSteps
	if len(r.attemptQueue) == 0 {
		return nil, nil
	}
	attempt := r.attemptQueue[0]
	r.attemptQueue = r.attemptQueue[1:]
	return &attempt, nil
}
func (r *fakeRepo) ClaimAction(ctx context.Context) (*domain.Action, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimActions++
	if len(r.actionQueue) == 0 {
		return nil, nil
	}
	action := r.actionQueue[0]
	r.actionQueue = r.actionQueue[1:]
	action.Status = "running"
	return &action, nil
}
func (r *fakeRepo) AddObservation(ctx context.Context, observation domain.Observation) (domain.Observation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace = append(r.trace, "observation")
	if r.observationErr != nil {
		return domain.Observation{}, r.observationErr
	}
	r.observations = append(r.observations, observation)
	return observation, nil
}
func (r *fakeRepo) CreateThread(ctx context.Context, thread domain.Thread) (domain.Thread, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace = append(r.trace, "child")
	r.children = append(r.children, thread)
	return thread, nil
}
func (r *fakeRepo) ProposeAction(ctx context.Context, threadID, attemptID string, input domain.ActionInput) (domain.Action, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace = append(r.trace, "proposal")
	r.proposals = append(r.proposals, input)
	status := r.proposalStatus
	if status == "" {
		status = "pending"
	}
	return domain.Action{ID: "proposed", Status: status, Input: input}, nil
}
func (r *fakeRepo) FinishAttempt(ctx context.Context, id, attemptStatus, threadStatus, summary, nextStep string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.canceledPersist = r.canceledPersist || ctx.Err() != nil
	r.trace = append(r.trace, "finish_attempt")
	r.attemptReceipts = append(r.attemptReceipts, attemptReceipt{id, attemptStatus, threadStatus, summary, nextStep})
	r.finished <- "attempt"
	return nil
}
func (r *fakeRepo) FinishAction(ctx context.Context, id, status string, result domain.ActionResult, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.canceledPersist = r.canceledPersist || ctx.Err() != nil
	r.trace = append(r.trace, "finish_action")
	r.actionReceipts = append(r.actionReceipts, actionReceipt{id, status, message, result})
	r.finished <- "action"
	return r.finishActionErr
}
func (r *fakeRepo) record(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace = append(r.trace, step)
}

func TestResearchDecisionsPersistBeforeFinishing(t *testing.T) {
	cases := []struct {
		kind, persistenceStep, attemptStatus, threadStatus string
	}{
		{"observe", "observation", "succeeded", "ready"},
		{"delegate", "child", "succeeded", "ready"},
		{"action", "proposal", "waiting_approval", "waiting_approval"},
		{"conclude", "", "succeeded", "concluded"},
		{"block", "", "succeeded", "blocked"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			repo := newFakeRepo()
			decision := domain.Decision{Kind: tc.kind, Summary: "本轮依据", NextStep: "新的明确下一步"}
			switch tc.kind {
			case "observe":
				decision.Observation = "未验证的有用观察"
			case "delegate":
				decision.Title, decision.Question, decision.Rationale = "独立子问题", "另一个问题", "不共享业务状态"
			case "action":
				decision.Action = &domain.ActionInput{Kind: "http", Method: "GET", URL: "https://authorized.test/path", Reason: "核对现象"}
			}
			m := modelFunc(func(ctx context.Context, snapshot domain.Snapshot) (domain.Decision, error) {
				if len(snapshot.Actions) != 1 || len(snapshot.Observations) != 1 {
					t.Error("恢复未消费已有动作和观察")
				}
				return decision, nil
			})
			exec := fakeExecutor{execute: func(context.Context, domain.ActionInput, []string) (domain.ActionResult, error) {
				t.Error("提出动作时不得执行")
				return domain.ActionResult{}, nil
			}}
			engine := New(repo, m, exec, Config{})
			engine.runResearch(context.Background(), domain.Attempt{ID: "attempt", ThreadID: "thread"})
			if len(repo.attemptReceipts) != 1 {
				t.Fatalf("receipts=%#v", repo.attemptReceipts)
			}
			receipt := repo.attemptReceipts[0]
			if receipt.attemptStatus != tc.attemptStatus || receipt.threadStatus != tc.threadStatus || receipt.summary != decision.Summary || receipt.nextStep != decision.NextStep {
				t.Fatalf("receipt=%#v", receipt)
			}
			wantTrace := []string{"snapshot"}
			if tc.persistenceStep != "" {
				wantTrace = append(wantTrace, tc.persistenceStep)
			}
			wantTrace = append(wantTrace, "finish_attempt")
			if !reflect.DeepEqual(repo.trace, wantTrace) {
				t.Fatalf("trace=%v, want=%v", repo.trace, wantTrace)
			}
			if tc.kind == "delegate" && (repo.children[0].AutoRun || repo.children[0].MissionID != "mission") {
				t.Fatal("子问题扩大自动运行或丢失所属项目")
			}
			if tc.kind == "observe" && repo.observations[0].Kind != "model_observation" {
				t.Fatal("模型自报被升级为事实")
			}
		})
	}
}

func TestAutomaticallyAllowedActionDoesNotWaitForApproval(t *testing.T) {
	repo := newFakeRepo()
	repo.proposalStatus = "approved"
	model := modelFunc(func(context.Context, domain.Snapshot) (domain.Decision, error) {
		return domain.Decision{Kind: "action", Summary: "检查", NextStep: "消费执行回执", Action: &domain.ActionInput{Kind: "local_command", Command: "true", Reason: "验证"}}, nil
	})
	New(repo, model, fakeExecutor{}, Config{}).runResearch(context.Background(), domain.Attempt{ID: "attempt", ThreadID: "thread"})
	if len(repo.attemptReceipts) != 1 || repo.attemptReceipts[0].attemptStatus != "succeeded" || repo.attemptReceipts[0].threadStatus != "ready" {
		t.Fatalf("默认放行动作不应等待人工：%+v", repo.attemptReceipts)
	}
}

func TestResearchFailureKeepsDirectionBlocked(t *testing.T) {
	for _, scenario := range []string{"model_error", "invalid_decision", "model_panic", "save_failure", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newFakeRepo()
			if scenario == "save_failure" {
				repo.observationErr = errors.New("写入失败")
			}
			m := modelFunc(func(ctx context.Context, snapshot domain.Snapshot) (domain.Decision, error) {
				switch scenario {
				case "model_error":
					return domain.Decision{}, errors.New("显式上下文阻塞")
				case "invalid_decision":
					return domain.Decision{Kind: "confirmed_vulnerability"}, nil
				case "model_panic":
					panic("private adapter internals")
				case "timeout":
					<-ctx.Done()
					return domain.Decision{}, ctx.Err()
				}
				return domain.Decision{Kind: "observe", Summary: "观察", NextStep: "继续", Observation: "未保存的观察"}, nil
			})
			engine := New(repo, m, fakeExecutor{}, Config{ModelTimeout: 10 * time.Millisecond})
			engine.runResearch(context.Background(), domain.Attempt{ID: "attempt", ThreadID: "thread"})
			if len(repo.attemptReceipts) != 1 {
				t.Fatalf("receipts=%#v", repo.attemptReceipts)
			}
			receipt := repo.attemptReceipts[0]
			if receipt.attemptStatus != "failed" || receipt.threadStatus != "blocked" || receipt.nextStep != "原有未决方向" || receipt.summary == "" {
				t.Fatalf("失败丢失方向或被假称成功: %#v", receipt)
			}
			if repo.canceledPersist || strings.Contains(receipt.summary, "private adapter") {
				t.Fatal("保存使用了失效 context 或泄露 panic 内容")
			}
		})
	}
}

func TestActionValidationExecutionAndReceiptOrder(t *testing.T) {
	repo := newFakeRepo()
	exec := fakeExecutor{
		validate: func(input domain.ActionInput, origins []string) error {
			repo.record("validate")
			if !reflect.DeepEqual(origins, repo.snapshot.Mission.AllowedOrigins) {
				t.Error("授权范围不是来自持久项目")
			}
			return nil
		},
		execute: func(context.Context, domain.ActionInput, []string) (domain.ActionResult, error) {
			repo.record("execute")
			return domain.ActionResult{Body: []byte{0, 255}, StatusCode: 200}, nil
		},
	}
	New(repo, nil, exec, Config{}).runAction(context.Background(), domain.Action{ID: "action", ThreadID: "thread"})
	if !reflect.DeepEqual(repo.trace, []string{"snapshot", "validate", "execute", "finish_action"}) {
		t.Fatalf("顺序错误: %v", repo.trace)
	}
	if len(repo.actionReceipts) != 1 || repo.actionReceipts[0].status != "succeeded" || !reflect.DeepEqual(repo.actionReceipts[0].result.Body, []byte{0, 255}) {
		t.Fatalf("回执丢失: %#v", repo.actionReceipts)
	}
}

func TestActionFailureClassification(t *testing.T) {
	for _, scenario := range []string{"scope", "unknown", "timeout", "panic", "known_failure", "validate_panic", "snapshot_failure"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newFakeRepo()
			calls := 0
			if scenario == "snapshot_failure" {
				repo.snapshotErr = errors.New("数据库暂不可用")
			}
			exec := fakeExecutor{
				validate: func(domain.ActionInput, []string) error {
					if scenario == "scope" {
						return errors.New("越界")
					}
					if scenario == "validate_panic" {
						panic("validator")
					}
					return nil
				},
				execute: func(ctx context.Context, _ domain.ActionInput, _ []string) (domain.ActionResult, error) {
					calls++
					switch scenario {
					case "unknown":
						return domain.ActionResult{EffectUnknown: true, Stdout: []byte("partial")}, nil
					case "timeout":
						<-ctx.Done()
						return domain.ActionResult{Stdout: []byte("partial")}, ctx.Err()
					case "panic":
						panic("executor")
					default:
						return domain.ActionResult{ExitCode: 1, Stderr: []byte("known error")}, errors.New("已知执行失败")
					}
				},
			}
			New(repo, nil, exec, Config{ActionTimeout: 10 * time.Millisecond}).runAction(context.Background(), domain.Action{ID: "action", ThreadID: "thread"})
			receipt := repo.actionReceipts[0]
			wantStatus := "failed"
			if scenario == "unknown" || scenario == "timeout" || scenario == "panic" {
				wantStatus = "outcome_unknown"
			}
			if receipt.status != wantStatus || receipt.message == "" || repo.canceledPersist {
				t.Fatalf("receipt=%#v canceled=%v", receipt, repo.canceledPersist)
			}
			if (scenario == "scope" || scenario == "validate_panic" || scenario == "snapshot_failure") && calls != 0 {
				t.Fatal("校验或快照失败后仍发送动作")
			}
			if calls > 1 {
				t.Fatal("引擎重试外部动作")
			}
			if (scenario == "unknown" || scenario == "timeout") && string(receipt.result.Stdout) != "partial" {
				t.Fatal("失败的部分结果未保存")
			}
		})
	}
}

func TestErrorsDoNotPersistCredentials(t *testing.T) {
	privateErr := errors.New("Authorization: Bearer private-provider-key; HTTP headers={Cookie: private-session}")
	repo := newFakeRepo()
	m := modelFunc(func(context.Context, domain.Snapshot) (domain.Decision, error) {
		return domain.Decision{}, privateErr
	})
	New(repo, m, nil, Config{}).runResearch(context.Background(), domain.Attempt{ID: "attempt", ThreadID: "thread"})
	for _, validationFailure := range []bool{false, true} {
		exec := fakeExecutor{
			validate: func(domain.ActionInput, []string) error {
				if validationFailure {
					return privateErr
				}
				return nil
			},
			execute: func(context.Context, domain.ActionInput, []string) (domain.ActionResult, error) {
				return domain.ActionResult{}, privateErr
			},
		}
		New(repo, nil, exec, Config{}).runAction(context.Background(), domain.Action{ID: "action", ThreadID: "thread"})
	}
	messages := []string{repo.attemptReceipts[0].summary, repo.actionReceipts[0].message, repo.actionReceipts[1].message}
	for _, message := range messages {
		if strings.Contains(message, "private-") || strings.Contains(message, "Authorization") || strings.Contains(message, "Cookie") {
			t.Fatalf("凭据出现在公开状态错误中: %q", message)
		}
	}
}

func TestShutdownPersistsPartialReceiptWithFreshContext(t *testing.T) {
	repo := newFakeRepo()
	repo.actionQueue = []domain.Action{{ID: "action", ThreadID: "thread", Status: "approved"}}
	started := make(chan struct{})
	exec := fakeExecutor{execute: func(ctx context.Context, _ domain.ActionInput, _ []string) (domain.ActionResult, error) {
		close(started)
		<-ctx.Done()
		return domain.ActionResult{Stdout: []byte("partial-before-shutdown")}, ctx.Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { New(repo, nil, exec, Config{MaxParallel: 1}).Run(ctx); close(done) }()
	await(t, started)
	cancel()
	await(t, done)
	if repo.canceledPersist || len(repo.actionReceipts) != 1 || repo.actionReceipts[0].status != "outcome_unknown" || string(repo.actionReceipts[0].result.Stdout) != "partial-before-shutdown" {
		t.Fatalf("取消后回执未保全: receipts=%#v canceled=%v", repo.actionReceipts, repo.canceledPersist)
	}
}

func TestRunWithoutModelExecutesApprovedActionOnlyOnce(t *testing.T) {
	repo := newFakeRepo()
	repo.actionQueue = []domain.Action{{ID: "action", ThreadID: "thread", Status: "approved"}}
	repo.finishActionErr = errors.New("不能确认保存")
	var executed atomic.Int32
	exec := fakeExecutor{execute: func(context.Context, domain.ActionInput, []string) (domain.ActionResult, error) {
		executed.Add(1)
		return domain.ActionResult{EffectUnknown: true}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { New(repo, nil, exec, Config{MaxParallel: 2}).Run(ctx); close(done) }()
	await(t, repo.finished)
	cancel()
	await(t, done)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.claimThreads != 0 || executed.Load() != 1 || repo.actionReceipts[0].status != "outcome_unknown" {
		t.Fatalf("threadClaims=%d executions=%d receipts=%#v", repo.claimThreads, executed.Load(), repo.actionReceipts)
	}
}

func TestWaitingApprovalReleasesResearchWorker(t *testing.T) {
	repo := newFakeRepo()
	repo.attemptQueue = []domain.Attempt{{ID: "first", ThreadID: "thread"}, {ID: "second", ThreadID: "other"}}
	var calls atomic.Int32
	m := modelFunc(func(context.Context, domain.Snapshot) (domain.Decision, error) {
		calls.Add(1)
		return domain.Decision{Kind: "action", Summary: "提出动作", NextStep: "等待审批", Action: &domain.ActionInput{Kind: "http", Method: "GET", URL: "https://authorized.test", Reason: "核对"}}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { New(repo, m, fakeExecutor{}, Config{MaxParallel: 1, MaxSteps: 9}).Run(ctx); close(done) }()
	await(t, repo.finished)
	await(t, repo.finished)
	cancel()
	await(t, done)
	if calls.Load() != 2 || len(repo.proposals) != 2 || repo.maxSteps != 9 {
		t.Fatalf("calls=%d proposals=%d maxSteps=%d", calls.Load(), len(repo.proposals), repo.maxSteps)
	}
}

func TestRunBoundsResearchAndActionWorkers(t *testing.T) {
	repo := newFakeRepo()
	for i := 0; i < 4; i++ {
		repo.attemptQueue = append(repo.attemptQueue, domain.Attempt{ID: "attempt", ThreadID: "thread"})
		repo.actionQueue = append(repo.actionQueue, domain.Action{ID: "action", ThreadID: "thread"})
	}
	var activeModels, activeActions, maxModels, maxActions atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	block := func(ctx context.Context, active, maximum *atomic.Int32) {
		current := active.Add(1)
		for previous := maximum.Load(); current > previous; previous = maximum.Load() {
			if maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		active.Add(-1)
	}
	m := modelFunc(func(ctx context.Context, _ domain.Snapshot) (domain.Decision, error) {
		block(ctx, &activeModels, &maxModels)
		return domain.Decision{Kind: "conclude", Summary: "仅结束当前问题"}, nil
	})
	exec := fakeExecutor{execute: func(ctx context.Context, _ domain.ActionInput, _ []string) (domain.ActionResult, error) {
		block(ctx, &activeActions, &maxActions)
		return domain.ActionResult{}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { New(repo, m, exec, Config{MaxParallel: 2}).Run(ctx); close(done) }()
	for i := 0; i < 4; i++ {
		await(t, entered)
	}
	if activeModels.Load() != 2 || activeActions.Load() != 2 {
		t.Fatalf("models=%d actions=%d", activeModels.Load(), activeActions.Load())
	}
	close(release)
	for i := 0; i < 8; i++ {
		await(t, repo.finished)
	}
	cancel()
	await(t, done)
	if maxModels.Load() > 2 || maxActions.Load() > 2 {
		t.Fatalf("max models=%d actions=%d", maxModels.Load(), maxActions.Load())
	}
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("等待本地 fake worker 超时")
		var zero T
		return zero
	}
}
