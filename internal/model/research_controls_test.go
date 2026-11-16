package model

import (
	"context"
	"testing"
	"wscyber/internal/domain"
)

type supplementRouteRepo struct {
	*routeRepo
	domain.ResearchControlRepository
	items []domain.Supplement
	err   error
}

func (r *supplementRouteRepo) GetSupplementContext(_ context.Context, mission, thread string) ([]domain.Supplement, error) {
	if mission != "m" || thread != "t" {
		return nil, domain.ErrNotFound
	}
	return r.items, r.err
}

type supplementCapture struct {
	snapshot domain.Snapshot
	calls    int
}

func (c *supplementCapture) Name() string { return "fixture" }
func (c *supplementCapture) Decide(_ context.Context, s domain.Snapshot) (domain.Decision, error) {
	c.calls++
	c.snapshot = s
	return domain.Decision{Kind: "block", Summary: "fixture", NextStep: "wait"}, nil
}
func TestC2BRouterUsesOnlyPersistedSupplements(t *testing.T) {
	repo := &supplementRouteRepo{routeRepo: &routeRepo{}, items: []domain.Supplement{{ID: "saved", Content: "actual-user-statement"}}}
	env := &supplementCapture{}
	router := NewRouted(repo, nil, env, "")
	snapshot := domain.Snapshot{Mission: domain.Mission{ID: "m"}, Thread: domain.Thread{ID: "t"}, Supplements: []domain.Supplement{{ID: "caller-injected", Content: "not-saved"}}}
	if _, err := router.Decide(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	if env.calls != 1 || len(env.snapshot.Supplements) != 1 || env.snapshot.Supplements[0].ID != "saved" {
		t.Fatal(env)
	}
	repo.err = domain.ErrUnavailable
	if _, err := router.Decide(context.Background(), snapshot); err == nil || env.calls != 1 {
		t.Fatal("read failure sent model", err)
	}
	plain := NewRouted(&routeRepo{}, nil, env, "")
	if _, err := plain.Decide(context.Background(), snapshot); err != nil || len(env.snapshot.Supplements) != 0 {
		t.Fatal("injection", err)
	}
}
