package research

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"wscyber/internal/domain"
)

type findingEngineRepo struct {
	*fakeRepo
	domain.FindingRepository
	saved              int
	saveErr, finishErr error
}

func (r *findingEngineRepo) ApplyFinding(_ context.Context, id string, p domain.FindingProposal) (domain.FindingRevision, error) {
	r.trace = append(r.trace, "finding")
	if id != "attempt" {
		return domain.FindingRevision{}, errors.New("wrong attempt")
	}
	if r.saveErr == nil {
		r.saved++
	}
	return domain.FindingRevision{}, r.saveErr
}
func (r *findingEngineRepo) FinishAttempt(ctx context.Context, id, a, s, summary, next string) error {
	_ = r.fakeRepo.FinishAttempt(ctx, id, a, s, summary, next)
	return r.finishErr
}

type findingEngineModel struct {
	calls   int
	stopped bool
	d       domain.Decision
}

func (m *findingEngineModel) Name() string { return "fixture" }
func (m *findingEngineModel) Decide(context.Context, domain.Snapshot) (domain.Decision, error) {
	m.calls++
	return m.d, nil
}
func (m *findingEngineModel) StopDispatch() { m.stopped = true }
func TestFindingPersistBeforeFinishAndNoReplay(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish-fails", true: "save-fails"}[failSave], func(t *testing.T) {
			repo := &findingEngineRepo{fakeRepo: newFakeRepo(), finishErr: errors.New("fixture finish failure")}
			if failSave {
				repo.saveErr = errors.New("fixture save failure")
			}
			m := &findingEngineModel{d: domain.Decision{Kind: "finding", Summary: "保存候选", NextStep: "继续", Finding: &domain.FindingProposal{Content: domain.FindingContent{Title: "未验证", Description: "有依据主张", Steps: []domain.FindingStep{{Text: "核对", EvidenceIndices: []int{}}}, Evidence: []domain.FindingReference{{Kind: "observation", ID: "11111111-1111-4111-8111-111111111111", Relation: "limitation"}}}}}}
			engine := New(repo, m, nil, Config{})
			engine.runResearch(context.Background(), domain.Attempt{ID: "attempt", ThreadID: "thread"})
			if m.calls != 1 || !reflect.DeepEqual(repo.trace, []string{"snapshot", "finding", "finish_attempt"}) {
				t.Fatal("replayed or wrong order", m.calls, repo.trace)
			}
			if failSave {
				if repo.saved != 0 || !m.stopped {
					t.Fatal("persistence failure did not stop dispatch")
				}
			} else if repo.saved != 1 {
				t.Fatal("saved candidate lost on finish failure")
			}
		})
	}
}
