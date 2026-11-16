package research

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"wscyber/internal/domain"
)

type skillEngineRepo struct {
	*fakeRepo
	domain.SkillRepository
	applyErr, finishErr error
	applied             int
	request             domain.SkillRequest
}

func (r *skillEngineRepo) ApplySkillActivity(ctx context.Context, attempt, kind string, request domain.SkillRequest) (domain.SkillActivity, error) {
	r.record("skill_activity")
	r.applied++
	r.request = request
	if ctx.Err() != nil {
		return domain.SkillActivity{}, ctx.Err()
	}
	return domain.SkillActivity{AttemptID: attempt, Kind: kind, Result: domain.SkillResult{Status: "loaded"}}, r.applyErr
}
func (r *skillEngineRepo) FinishAttempt(ctx context.Context, id, a, s, summary, next string) error {
	_ = r.fakeRepo.FinishAttempt(ctx, id, a, s, summary, next)
	return r.finishErr
}

type skillEngineModel struct {
	decision     domain.Decision
	calls, stops int
	trace        func(string)
}

func (m *skillEngineModel) Name() string { return "local-skill-fixture" }
func (m *skillEngineModel) Decide(context.Context, domain.Snapshot) (domain.Decision, error) {
	m.calls++
	return m.decision, nil
}
func (m *skillEngineModel) StopDispatch() { m.stops++; m.trace("stop_dispatch") }

func TestSkillActivityPersistenceAndFailStop(t *testing.T) {
	for _, stage := range []string{"ok", "activity_failure", "finish_failure"} {
		t.Run(stage, func(t *testing.T) {
			repo := &skillEngineRepo{fakeRepo: newFakeRepo()}
			if stage == "activity_failure" {
				repo.applyErr = errors.New("private SQL detail")
			}
			if stage == "finish_failure" {
				repo.finishErr = errors.New("private SQL detail")
			}
			request := domain.SkillRequest{ItemID: "11111111111111111111111111111111", SourceToken: "22222222222222222222222222222222", Purpose: "按需辅助"}
			m := &skillEngineModel{decision: domain.Decision{Kind: "skill_load", Summary: "加载方法", NextStep: "消费已存结果", Skill: &request}, trace: repo.record}
			engine := New(repo, m, fakeExecutor{execute: func(context.Context, domain.ActionInput, []string) (domain.ActionResult, error) {
				t.Fatal("Skill不应触发目标动作")
				return domain.ActionResult{}, nil
			}}, Config{})
			engine.runResearch(context.Background(), domain.Attempt{ID: "11111111-1111-1111-1111-111111111111", ThreadID: "thread"})
			if repo.applied != 1 || m.calls != 1 || !reflect.DeepEqual(repo.request, request) || len(repo.proposals) != 0 || len(repo.attemptReceipts) != 1 {
				t.Fatal("发生重复活动/模型/目标动作", repo.applied, m.calls, repo.trace)
			}
			want := []string{"snapshot", "skill_activity", "finish_attempt"}
			receipt := repo.attemptReceipts[0]
			if stage == "activity_failure" {
				want = []string{"snapshot", "skill_activity", "stop_dispatch", "finish_attempt"}
				if m.stops != 1 || receipt.attemptStatus != "failed" || receipt.threadStatus != "blocked" || receipt.nextStep != repo.snapshot.Thread.NextStep {
					t.Fatal("持久失败未停止派发或丢失未决方向", receipt, m.stops)
				}
			} else if m.stops != 0 || receipt.attemptStatus != "succeeded" || receipt.threadStatus != "ready" {
				t.Fatal(receipt, m.stops)
			}
			if !reflect.DeepEqual(repo.trace, want) {
				t.Fatal(repo.trace, want)
			}
		})
	}
}
