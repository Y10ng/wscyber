package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

func contextAttempt(t *testing.T, s *Store, ctx context.Context, th domain.Thread) *domain.Attempt {
	t.Helper()
	if _, e := s.SetThreadControl(ctx, th.ID, "ready", true); e != nil {
		t.Fatal(e)
	}
	a, e := s.ClaimThread(ctx, 8)
	if e != nil || a == nil {
		t.Fatal(a, e)
	}
	return a
}
func contextCall(t *testing.T, th domain.Thread, a *domain.Attempt, snap domain.Snapshot) domain.BeginModelCall {
	t.Helper()
	snap.Assembly.InputBytes = 10000
	return domain.BeginModelCall{Call: domain.ModelCall{ID: ledgerID(t), ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: th.MissionID, ThreadID: th.ID, AttemptID: a.ID}, Assembly: snap.Assembly, SkillLoadIDs: snap.SkillContext.LoadIDs(), SupplementIDs: snap.SupplementIDs()}
}
func TestC3ABoundedHistoryAndImmutableAudit(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	a := contextAttempt(t, s, ctx, th)
	// 两千条原件：最早反证必须保留，普通早期历史只报告未读范围。
	_, e := s.pool.Exec(ctx, `INSERT INTO observations(id,thread_id,kind,content,created_at) SELECT (md5('c3a-obs-'||n))::uuid::text,$1,CASE WHEN n=2000 THEN 'counterevidence' ELSE 'operator_note' END,'ORIGINAL_BODY_'||n,clock_timestamp()-n*interval '1 second' FROM generate_series(1,2000) n`, th.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.pool.Exec(ctx, `INSERT INTO actions(id,thread_id,input,request_hash,status,result,resolved_at,resolution_note,created_at) VALUES($1,$2,'{"kind":"http"}',$3,'outcome_unknown','{}',clock_timestamp(),'人工核对，不改未知历史',clock_timestamp()-interval '1 day')`, mustID(t), th.ID, strings.Repeat("a", 64))
	if e != nil {
		t.Fatal(e)
	}
	scope, e := s.ResearchScope(ctx, th.ID)
	if e != nil || len(scope.Observations) != 0 || scope.ResearchContext != nil {
		t.Fatal(scope, e)
	}
	snap, e := s.AssembleResearchSnapshot(ctx, th.MissionID, th.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(snap.Observations) != 33 || len(snap.Actions) != 1 || snap.Actions[0].Status != "outcome_unknown" || !snap.ContextSelection.Windows[0].OlderExists {
		t.Fatal("bounded/pinned selection", len(snap.Observations), snap.ContextSelection)
	}
	if snap.Observations[0].Kind != "counterevidence" {
		t.Fatal("old counterevidence lost")
	}
	in := contextCall(t, th, a, snap)
	call, e := s.BeginModelCall(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.GetModelCallContext(ctx, call.ID, th.MissionID)
	if e != nil || !v.Recorded {
		t.Fatal(v, e)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), "ORIGINAL_BODY") || strings.Contains(string(raw), "人工核对") {
		t.Fatal("audit copied raw bodies")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE model_call_contexts SET assembly=assembly WHERE call_id=$1`, call.ID); e == nil {
		t.Fatal("mutable audit")
	}
	if _, e = s.pool.Exec(ctx, `DELETE FROM model_call_contexts WHERE call_id=$1`, call.ID); e == nil {
		t.Fatal("deletable audit")
	}
	in.Call.ID = ledgerID(t)
	if _, e = s.BeginModelCall(ctx, in); e == nil {
		t.Fatal("second call in same Attempt")
	}
	other := fixtureThread(t, ctx, s)
	if _, e = s.GetModelCallContext(ctx, call.ID, other.MissionID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("foreign owner", e)
	}
	if _, e = s.AssembleResearchSnapshot(ctx, other.MissionID, th.ID, a.ID); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
	original, e := s.Snapshot(ctx, th.ID)
	if e != nil || len(original.Observations) != 2000 {
		t.Fatal("ordinary history changed", e)
	}
}
func mustID(t *testing.T) string {
	t.Helper()
	id, e := newID()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestC3ACapacityAndUnreadItems(t *testing.T) {
	for _, test := range []string{"optional-oversize", "required-oversize", "pinned-overflow", "history-budget", "empty"} {
		t.Run(test, func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			th := fixtureThread(t, ctx, s)
			a := contextAttempt(t, s, ctx, th)
			if test != "empty" {
				n, bytes := 2, domain.MaxContextItemBytes+1
				kind := "operator_note"
				if test == "pinned-overflow" {
					n = 17
					bytes = 1
					kind = "counterevidence"
				}
				if test == "history-budget" {
					n = 8
					bytes = 20000
				}
				for i := 0; i < n; i++ {
					body := strings.Repeat("x", bytes)
					if test == "optional-oversize" && i == n-1 {
						body = "latest"
					}
					if _, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: kind, Content: body}); e != nil {
						t.Fatal(e)
					}
				}
			}
			snap, e := s.AssembleResearchSnapshot(ctx, th.MissionID, th.ID, a.ID)
			if test == "required-oversize" || test == "pinned-overflow" {
				if e == nil {
					t.Fatal("must stop before model")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if test == "optional-oversize" || test == "history-budget" {
				reason := "item_limit"
				if test == "history-budget" {
					reason = "history_budget"
				}
				found := false
				for _, v := range snap.Assembly.Sources {
					if !v.Included && v.Reason == reason {
						found = true
					}
				}
				if !found {
					t.Fatal("missing omitted reference", reason)
				}
			}
			snap.Assembly.InputBytes = 10000
			if e = domain.ValidateContextAssembly(snap.Assembly); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestC3AAuditAtomicityAndLateSupplement(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	a := contextAttempt(t, s, ctx, th)
	snap, e := s.AssembleResearchSnapshot(ctx, th.MissionID, th.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	late, e := s.AddSupplement(ctx, controlKey(300), th.MissionID, th.ID, domain.SupplementInput{Source: "fixture", Content: "LATE_PRIVATE", ShareWithModel: true})
	if e != nil {
		t.Fatal(e)
	}
	in := contextCall(t, th, a, snap)
	// 注入关联集合不一致：Begin整笔回滚。
	in.SupplementIDs = []string{late.Supplement.ID}
	if _, e = s.BeginModelCall(ctx, in); e == nil {
		t.Fatal("association mismatch accepted")
	}
	var count int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_calls WHERE id=$1`, in.Call.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial Begin", count, e)
	}
	in.SupplementIDs = nil
	foreign := fixtureThread(t, ctx, s)
	obs, e := s.AddObservation(ctx, domain.Observation{ThreadID: foreign.ID, Kind: "operator_note", Content: "foreign"})
	if e != nil {
		t.Fatal(e)
	}
	in.Assembly.Sources = append(in.Assembly.Sources, domain.ContextSource{Kind: "observation", ID: obs.ID, At: obs.CreatedAt, OriginalBytes: 7, IncludedBytes: 10, Included: true, Reason: "selected", State: "other"})
	if _, e = s.BeginModelCall(ctx, in); e == nil {
		t.Fatal("foreign source accepted")
	}
	in.Assembly.Sources = in.Assembly.Sources[:len(in.Assembly.Sources)-1]
	call, e := s.BeginModelCall(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	expected, _ := json.Marshal(in.Assembly)
	s.Close()
	reopened, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	v, e := reopened.GetModelCallContext(ctx, call.ID, th.MissionID)
	if e != nil {
		t.Fatal(e)
	}
	actual, _ := json.Marshal(v.Assembly)
	if string(actual) != string(expected) {
		t.Fatal("restart reconstructed audit")
	}
	if strings.Contains(string(actual), late.Supplement.ID) {
		t.Fatal("late supplement rewrote past call")
	}
}
func TestC3ALegacyCallsNotBackfilled(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	a := contextAttempt(t, s, ctx, th)
	call, e := s.BeginModelCall(ctx, domain.BeginModelCall{Call: domain.ModelCall{ID: ledgerID(t), ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: th.MissionID, ThreadID: th.ID, AttemptID: a.ID}})
	if e != nil {
		t.Fatal(e)
	}
	v, e := s.GetModelCallContext(ctx, call.ID, th.MissionID)
	if e != nil || v.Recorded || v.Assembly != nil {
		t.Fatal(fmt.Sprint(v), e)
	}
}
