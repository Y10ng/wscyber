package store

import (
	"context"
	"testing"
	"wscyber/internal/domain"
)

// 仅postgresStore创建的随机schema回退到v10，绝不操作旧运行实例。
func dropContextFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	dropFindingsFixture(t, s, ctx)
	if _, e := s.pool.Exec(ctx, `DROP TABLE model_call_contexts; DROP FUNCTION wscyber_context_guard(); DROP INDEX observations_context_required; DROP INDEX actions_context_required; DELETE FROM wscyber_schema_migrations WHERE version=11`); e != nil {
		t.Fatal(e)
	}
}
func TestC3AUpgradeV10(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	a := contextAttempt(t, s, ctx, th)
	c, e := s.BeginModelCall(ctx, domain.BeginModelCall{Call: domain.ModelCall{ID: ledgerID(t), ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: th.MissionID, ThreadID: th.ID, AttemptID: a.ID}})
	if e != nil {
		t.Fatal(e)
	}
	dropContextFixture(t, s, ctx)
	s.Close()
	next, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	v, e := next.GetModelCallContext(ctx, c.ID, th.MissionID)
	if e != nil || v.Recorded || v.Assembly != nil {
		t.Fatal(v, e)
	}
}
