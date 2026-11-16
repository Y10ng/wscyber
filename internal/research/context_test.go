package research

import (
	"context"
	"testing"
	"wscyber/internal/domain"
)

type scopedRepo struct {
	*fakeRepo
	domain.ResearchContextRepository
	read bool
}

func (r *scopedRepo) ResearchScope(context.Context, string) (domain.Snapshot, error) {
	r.read = true
	return domain.Snapshot{Mission: r.snapshot.Mission, Thread: r.snapshot.Thread}, nil
}
func (r *scopedRepo) Snapshot(context.Context, string) (domain.Snapshot, error) {
	panic("formal research must not read full Snapshot")
}
func TestC3AResearchUsesScopeWithoutHistory(t *testing.T) {
	repo := &scopedRepo{fakeRepo: newFakeRepo()}
	calls := 0
	m := modelFunc(func(_ context.Context, s domain.Snapshot) (domain.Decision, error) {
		calls++
		if len(s.Observations) != 0 || len(s.Actions) != 0 {
			t.Fatal("unbounded history before router")
		}
		return domain.Decision{Kind: "block", Summary: "fixture"}, nil
	})
	New(repo, m, nil, Config{}).runResearch(context.Background(), domain.Attempt{ID: "attempt", ThreadID: "thread"})
	if !repo.read || calls != 1 {
		t.Fatal(repo.read, calls)
	}
}
