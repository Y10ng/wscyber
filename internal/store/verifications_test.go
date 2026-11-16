package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"wscyber/internal/domain"
	"wscyber/internal/modelprovider"
)

func d2Source(t *testing.T, s *Store, ctx context.Context) (domain.Thread, domain.FindingRevision) {
	t.Helper()
	m, e := s.CreateMission(ctx, domain.Mission{Title: "D2 fixture", Goal: "只测持久化", AllowedOrigins: []string{"http://127.0.0.1:9"}})
	if e != nil {
		t.Fatal(e)
	}
	th, e := s.CreateThread(ctx, domain.Thread{MissionID: m.ID, Title: "原研究", Question: "候选主张"})
	if e != nil {
		t.Fatal(e)
	}
	o, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: "原研究独有正文，不复制全文"})
	if e != nil {
		t.Fatal(e)
	}
	a, _, _ := findingCall(t, s, ctx, th)
	r, e := s.ApplyFinding(ctx, a.ID, findingProposal(domain.FindingReference{Kind: "observation", ID: o.ID, Relation: "supporting"}))
	if e != nil {
		t.Fatal(e)
	}
	finishFindingAttempt(t, s, ctx, a)
	return th, r
}
func d2Start(t *testing.T, s *Store, ctx context.Context, src domain.FindingRevision, steps int) domain.VerificationReceipt {
	t.Helper()
	v, e := s.StartVerification(ctx, ledgerID(t), src.MissionID, src.FindingID, domain.VerificationInput{SourceRevisionID: src.ID, MaxSteps: steps}, func(context.Context) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func d2Call(t *testing.T, s *Store, ctx context.Context, v domain.VerificationReceipt) (*domain.Attempt, domain.Snapshot) {
	t.Helper()
	th, e := s.GetThread(ctx, v.ThreadID)
	if e != nil {
		t.Fatal(e)
	}
	a := claimAttempt(t, ctx, s, v.ThreadID, 100)
	snap, e := s.AssembleResearchSnapshot(ctx, v.MissionID, v.ThreadID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	in := contextCall(t, th, a, snap)
	in.Call.Purpose = "verification"
	c, e := s.BeginModelCall(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	endLedger(t, s, ctx, c, modelprovider.MissingUsage())
	return a, snap
}
func d2Conclusion(verdict string, refs ...domain.FindingReference) domain.VerificationConclusion {
	if refs == nil {
		refs = []domain.FindingReference{}
	}
	return domain.VerificationConclusion{Verdict: verdict, Method: "新证据核对", Preconditions: "无真实身份", Summary: "VERIFICATION_BODY_有限模型判读", Limitations: "不自动确认", Evidence: refs}
}
func d2Action(t *testing.T, s *Store, ctx context.Context, v domain.VerificationReceipt, status string, r domain.ActionResult) domain.Action {
	t.Helper()
	a, e := s.ProposeAction(ctx, v.ThreadID, "", domain.ActionInput{Kind: "http", Method: "GET", URL: "http://127.0.0.1:9/fresh", Reason: "新验证请求"})
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.ClaimAction(ctx)
	if e != nil || got == nil || got.ID != a.ID {
		t.Fatal(got, e)
	}
	if e = s.FinishAction(ctx, a.ID, status, r, ""); e != nil {
		t.Fatal(e)
	}
	a, e = s.GetAction(ctx, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestD2FrozenSourceNewEvidenceAndNoReplay(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th, src := d2Source(t, s, ctx)
	before, _ := json.Marshal(src)
	v := d2Start(t, s, ctx, src, 6)
	action := d2Action(t, s, ctx, v, "succeeded", domain.ActionResult{StatusCode: 200, Body: []byte("NEW_D2_BYTES")})
	a, snap := d2Call(t, s, ctx, v)
	if snap.Verification == nil || snap.Verification.Source.ID != src.ID || len(snap.Findings) != 0 || snap.Assembly.Policy != domain.VerificationContextPolicy {
		t.Fatal("not independent", snap.Verification)
	}
	for _, o := range snap.Observations {
		if o.ThreadID != v.ThreadID || strings.Contains(o.Content, "原研究独有正文") {
			t.Fatal("copied original history")
		}
	}
	// 在途期间原候选更新，新验证仍固定原时点。
	a2, _, _ := findingCall(t, s, ctx, th)
	p := findingProposal(src.Content.Evidence[0])
	p.FindingID = src.FindingID
	p.ExpectedRevision = 1
	p.Content.Title = "后续候选更新"
	next, e := s.ApplyFinding(ctx, a2.ID, p)
	if e != nil {
		t.Fatal(e)
	}
	finishFindingAttempt(t, s, ctx, a2)
	c := d2Conclusion("supported", domain.FindingReference{Kind: "action", ID: action.ID, Relation: "supporting"})
	r, e := s.ApplyVerification(ctx, a.ID, c)
	if e != nil || r.Evidence[0].ThreadID != v.ThreadID {
		t.Fatal(r, e)
	}
	again, e := s.ApplyVerification(ctx, a.ID, c)
	if e != nil || again.CallID != r.CallID {
		t.Fatal("replay", e)
	}
	changed := c
	changed.Summary = "different"
	if _, e = s.ApplyVerification(ctx, a.ID, changed); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("changed replay", e)
	}
	got, e := s.GetVerification(ctx, src.MissionID, src.FindingID, v.VerificationID)
	if e != nil || got.SourceRevisionID != src.ID || !got.HasResult {
		t.Fatal(got, e)
	}
	current, e := s.GetFinding(ctx, src.MissionID, src.FindingID, "")
	if e != nil || current.ID != next.ID || current.Status != "candidate" {
		t.Fatal("overwrote updated candidate", e)
	}
	old, e := s.GetFinding(ctx, src.MissionID, src.FindingID, src.ID)
	raw, _ := json.Marshal(old)
	if e != nil || string(raw) != string(before) {
		t.Fatal("source drift", e)
	}
	evidence, e := s.VerificationEvidence(ctx, src.MissionID, src.FindingID, v.VerificationID, 0)
	if e != nil || !evidence.MatchesSnapshot || string(evidence.Action.Result.Body) != "NEW_D2_BYTES" {
		t.Fatal("new original", e)
	}
	// 不做FinishAttempt模拟保存后收尾失败，Recover也不能重开。
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetThreadControl(ctx, v.ThreadID, "ready", true); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("reopened saved result", e)
	}
	if _, e = s.ProposeAction(ctx, v.ThreadID, "", action.Input); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("new action after result", e)
	}
	if claimed, e := s.ClaimThread(ctx, 100); e != nil || claimed != nil {
		t.Fatal("replayed", claimed, e)
	}
	var n int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_call_contents c JOIN model_calls m ON m.id=c.call_id WHERE m.thread_id=$1`, v.ThreadID).Scan(&n); e != nil || n != 0 {
		t.Fatal("raw off bypass", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE verification_results SET verdict='inconclusive' WHERE verification_id=$1`, v.VerificationID); e == nil {
		t.Fatal("mutable result")
	}
}
func TestD2IdempotentStartConcurrencyAndOwnership(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	_, src := d2Source(t, s, ctx)
	key := ledgerID(t)
	in := domain.VerificationInput{SourceRevisionID: src.ID, MaxSteps: 4}
	var wg sync.WaitGroup
	results := make(chan domain.VerificationReceipt, 6)
	errs := make(chan error, 6)
	for range 6 {
		wg.Go(func() {
			v, e := s.StartVerification(ctx, key, src.MissionID, src.FindingID, in, func(context.Context) error { return nil })
			results <- v
			errs <- e
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	for v := range results {
		if id != "" && id != v.VerificationID {
			t.Fatal("duplicate")
		}
		id = v.VerificationID
	}
	if v, e := s.StartVerification(ctx, key, src.MissionID, src.FindingID, in, func(context.Context) error { return domain.ErrUnavailable }); e != nil || v.VerificationID != id {
		t.Fatal("replay capability", e)
	}
	in.MaxSteps++
	if _, e := s.StartVerification(ctx, key, src.MissionID, src.FindingID, in, nil); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("payload", e)
	}
	if _, e := s.StartVerification(ctx, ledgerID(t), src.MissionID, src.FindingID, in, func(context.Context) error { return nil }); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("second active", e)
	}
	receipt, e := s.GetOperation(ctx, key)
	if e != nil || receipt.ResourceType != "verification" || receipt.ResponseStatus != 202 || receipt.ResourceID != id {
		t.Fatal(receipt, e)
	}
	foreign := fixtureThread(t, ctx, s)
	if _, e = s.GetVerification(ctx, foreign.MissionID, src.FindingID, id); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("cross owner", e)
	}
	if _, e = s.StartVerification(ctx, ledgerID(t), foreign.MissionID, src.FindingID, in, nil); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("cross start", e)
	}
}
func TestD2RejectUnsupportedEvidenceAndPauseUnknown(t *testing.T) {
	for _, kind := range []string{"old", "observation", "truncated", "missing-response", "unknown", "counterevidence"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			_, src := d2Source(t, s, ctx)
			v := d2Start(t, s, ctx, src, 4)
			ref := src.Content.Evidence[0]
			if kind == "observation" {
				o, e := s.AddObservation(ctx, domain.Observation{ThreadID: v.ThreadID, Kind: "model_observation", Content: "自报成功"})
				if e != nil {
					t.Fatal(e)
				}
				ref = domain.FindingReference{Kind: "observation", ID: o.ID, Relation: "supporting"}
			} else if kind != "old" {
				r := domain.ActionResult{StatusCode: 200, Body: []byte("new")}
				status := "succeeded"
				if kind == "truncated" {
					r.Truncated = true
				}
				if kind == "missing-response" {
					r.StatusCode = 0
				}
				if kind == "unknown" {
					status = "outcome_unknown"
					r.EffectUnknown = true
				}
				a := d2Action(t, s, ctx, v, status, r)
				ref = domain.FindingReference{Kind: "action", ID: a.ID, Relation: "supporting"}
				if kind == "unknown" {
					if _, e := s.SetThreadControl(ctx, v.ThreadID, "ready", true); !errors.Is(e, domain.ErrConflict) {
						t.Fatal("unknown continuation", e)
					}
					if e := s.ResolveUnknownAction(ctx, a.ID, "受控核对，仅解除阻塞，不重放"); e != nil {
						t.Fatal(e)
					}
					if claimed, e := s.ClaimAction(ctx); e != nil || claimed != nil {
						t.Fatal("replayed unknown", e)
					}
				}
			}
			a, _ := d2Call(t, s, ctx, v)
			if _, e := s.SetThreadControl(ctx, v.ThreadID, "paused", false); e != nil {
				t.Fatal(e)
			}
			verdict := "supported"
			if kind == "counterevidence" {
				verdict = "counterevidence"
				ref.Relation = "counterevidence"
			}
			_, e := s.ApplyVerification(ctx, a.ID, d2Conclusion(verdict, ref))
			if kind == "counterevidence" {
				if e != nil {
					t.Fatal(e)
				}
			} else {
				if e == nil {
					t.Fatal("unsupported decisive result accepted")
				}
				c := d2Conclusion("inconclusive")
				if kind != "old" {
					c.Evidence = []domain.FindingReference{ref}
				}
				if _, e = s.ApplyVerification(ctx, a.ID, c); e != nil {
					t.Fatal("insufficient not saved", e)
				}
			}
			if e = s.FinishAttempt(ctx, a.ID, "succeeded", "concluded", "结果保存", ""); e != nil {
				t.Fatal(e)
			}
			got, e := s.GetVerification(ctx, src.MissionID, src.FindingID, v.VerificationID)
			if e != nil || got.Status != "concluded" || !got.HasResult {
				t.Fatal("pause discarded result", e)
			}
		})
	}
}
func TestD2BudgetPaginationAndCursor(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	_, src := d2Source(t, s, ctx)
	for range 26 {
		v := d2Start(t, s, ctx, src, 1)
		a, _ := d2Call(t, s, ctx, v)
		if _, e := s.ApplyVerification(ctx, a.ID, d2Conclusion("inconclusive")); e != nil {
			t.Fatal(e)
		}
		if e := s.FinishAttempt(ctx, a.ID, "succeeded", "concluded", "不足", ""); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.ListVerifications(ctx, src.MissionID, src.FindingID, domain.FindingQuery{})
	if e != nil || len(page.Items) != 25 || page.NextCursor == "" {
		t.Fatal("first page", e)
	}
	second, e := s.ListVerifications(ctx, src.MissionID, src.FindingID, domain.FindingQuery{Cursor: page.NextCursor})
	if e != nil || len(second.Items) != 1 || second.NextCursor != "" {
		t.Fatal("second page", e)
	}
	if _, e = s.ListVerifications(ctx, mustID(t), src.FindingID, domain.FindingQuery{Cursor: page.NextCursor}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("cursor owner", e)
	}
	v := d2Start(t, s, ctx, src, 1)
	a, _ := d2Call(t, s, ctx, v)
	if e = s.FinishAttempt(ctx, a.ID, "succeeded", "ready", "未结束", "保留未决"); e != nil {
		t.Fatal(e)
	}
	if a, e := s.ClaimThread(ctx, 100); e != nil || a != nil {
		t.Fatal("budget bypass", a, e)
	}
	got, e := s.GetVerification(ctx, src.MissionID, src.FindingID, v.VerificationID)
	if e != nil || got.Status != "blocked" || got.HasResult || got.StepCount != 1 {
		t.Fatal("budget false success", got, e)
	}
}
