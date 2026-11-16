package model

import (
	"context"
	"strings"
	"testing"
	"time"
	"wscyber/internal/domain"
)

type contextRouteRepo struct {
	*routeRepo
	domain.ResearchContextRepository
	actual domain.Snapshot
	err    error
	calls  int
}

func (r *contextRouteRepo) AssembleResearchSnapshot(_ context.Context, m, t, a string) (domain.Snapshot, error) {
	r.calls++
	return r.actual, r.err
}

type contextEnvironment struct {
	snapshot domain.Snapshot
	calls    int
}

func (m *contextEnvironment) Name() string { return "fixture" }
func (m *contextEnvironment) Decide(_ context.Context, s domain.Snapshot) (domain.Decision, error) {
	m.calls++
	m.snapshot = s
	return domain.Decision{Kind: "block", Summary: "fixture"}, nil
}
func TestC3ARouterDiscardsInjectedHistoryAndFacts(t *testing.T) {
	env := &contextEnvironment{}
	r := &contextRouteRepo{routeRepo: &routeRepo{}, actual: domain.Snapshot{Mission: domain.Mission{ID: "real"}, Observations: []domain.Observation{{Content: "stored"}}}}
	m := NewRouted(r, nil, env, "")
	in := domain.Snapshot{Mission: domain.Mission{ID: "caller"}, Observations: []domain.Observation{{Content: "injected"}}, Assembly: &domain.ContextAssembly{}, Supplements: []domain.Supplement{{Content: "injected"}}}
	if _, e := m.DecideAttempt(context.Background(), in, "attempt"); e != nil {
		t.Fatal(e)
	}
	if env.calls != 1 || env.snapshot.Mission.ID != "real" || env.snapshot.Observations[0].Content != "stored" || len(env.snapshot.Supplements) != 0 {
		t.Fatal("caller facts survived")
	}
	r.err = domain.ErrUnavailable
	if _, e := m.DecideAttempt(context.Background(), in, "attempt"); e == nil || env.calls != 1 {
		t.Fatal("assembly failure called model")
	}
}
func TestC3AActualPromptBytesAndTotalLimit(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	now := time.Now().UTC()
	a := &domain.ContextAssembly{ContextSelection: domain.ContextSelection{Policy: domain.ContextPolicy, Windows: []domain.ContextWindow{{Kind: "observation", Limit: 32}, {Kind: "action", Limit: 8}, {Kind: "attempt", Limit: 8}}}, MissionID: id, ThreadID: id, AttemptID: id, AssembledAt: now, MaxInputBytes: 256 << 10}
	for _, k := range []string{"mission", "thread", "attempt"} {
		state := ""
		if k != "mission" {
			state = "running"
		}
		a.Sources = append(a.Sources, domain.ContextSource{Kind: k, ID: id, At: now, State: state, Included: true, Required: true, IncludedBytes: 1, Reason: "selected"})
	}
	s := domain.Snapshot{Assembly: a, ContextSelection: &a.ContextSelection}
	user, e := snapshotPrompt(s)
	if e != nil {
		t.Fatal(e)
	}
	if a.InputBytes != len(user)+len(taskSystemPrompt(s)) || !strings.Contains(user, "未分类反证") {
		t.Fatal("actual bytes or unread warning")
	}
	s.ResearchContext = &domain.ResearchContext{SafetyPromptEnabled: true, SafetyPrompt: strings.Repeat("<", 46000)}
	if _, e = snapshotPrompt(s); e == nil {
		t.Fatal("escaped system and user total should fail")
	}
}
