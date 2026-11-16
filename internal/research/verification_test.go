package research

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"wscyber/internal/domain"
)

type verificationEngineRepo struct {
	*fakeRepo
	domain.VerificationRepository
	saved   int
	saveErr error
}

func (r *verificationEngineRepo) ApplyVerification(_ context.Context, id string, c domain.VerificationConclusion) (domain.VerificationResult, error) {
	r.trace = append(r.trace, "verification")
	if r.saveErr == nil {
		r.saved++
	}
	return domain.VerificationResult{}, r.saveErr
}
func (r *verificationEngineRepo) FinishAttempt(ctx context.Context, id, a, s, summary, next string) error {
	_ = r.fakeRepo.FinishAttempt(ctx, id, a, s, summary, next)
	return errors.New("fixture finish failure")
}
func TestVerificationSavedBeforeFinishNoReplay(t *testing.T) {
	for _, fail := range []bool{false, true} {
		r := &verificationEngineRepo{fakeRepo: newFakeRepo()}
		r.snapshot.Verification = &domain.VerificationContext{}
		if fail {
			r.saveErr = errors.New("save failure")
		}
		m := &findingEngineModel{d: domain.Decision{Kind: "verification", Summary: "有限结果", Verification: &domain.VerificationConclusion{Verdict: "inconclusive", Method: "实际前提不足", Summary: "无法判定", Evidence: []domain.FindingReference{}}}}
		New(r, m, nil, Config{}).runResearch(context.Background(), domain.Attempt{ID: "attempt", ThreadID: "thread"})
		if m.calls != 1 || !reflect.DeepEqual(r.trace, []string{"snapshot", "verification", "finish_attempt"}) {
			t.Fatal(m.calls, r.trace)
		}
		if fail && (!m.stopped || r.saved != 0) || !fail && r.saved != 1 {
			t.Fatal("persistence boundary", fail, r.saved, m.stopped)
		}
	}
}
