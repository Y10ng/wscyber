package store

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"wscyber/internal/domain"
)

func TestD2Non2xxEvidenceKeepsLimitationsAndCandidate(t *testing.T) {
	for _, status := range []int{200, 302, 401, 403, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			_, src := d2Source(t, s, ctx)
			v := d2Start(t, s, ctx, src, 6)
			a := d2Action(t, s, ctx, v, "succeeded", domain.ActionResult{StatusCode: status, Headers: map[string][]string{"Location": {"https://redirect.invalid/controlled"}}, Body: []byte("受控响应证据：SQL syntax fixture，非真实目标")})
			failed := d2Action(t, s, ctx, v, "failed", domain.ActionResult{Truncated: true, Body: []byte("部分捕获，仅作限制")})
			attempt, _ := d2Call(t, s, ctx, v)
			r, e := s.ApplyVerification(ctx, attempt.ID, d2Conclusion("supported", domain.FindingReference{Kind: "action", ID: a.ID, Relation: "supporting"}, domain.FindingReference{Kind: "action", ID: failed.ID, Relation: "limitation"}))
			if e != nil || len(r.Evidence) != 2 || !r.Evidence[1].Truncated || r.Evidence[1].State != "failed" {
				t.Fatal("lost limitations or status shortcut", r, e)
			}
			f, e := s.GetFinding(ctx, src.MissionID, src.FindingID, "")
			if e != nil || f.Status != "candidate" {
				t.Fatal("HTTP status confirmed vulnerability", e)
			}
		})
	}
}
func TestD2BudgetEndReleasesOnlyExplicitNewVerification(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	_, src := d2Source(t, s, ctx)
	v := d2Start(t, s, ctx, src, 1)
	a, _ := d2Call(t, s, ctx, v)
	action, e := s.ProposeAction(ctx, v.ThreadID, a.ID, domain.ActionInput{Kind: "http", Method: "GET", URL: "http://127.0.0.1:9/fresh", Reason: "有限预算的新动作"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.FinishAttempt(ctx, a.ID, "succeeded", "ready", "等待动作", ""); e != nil {
		t.Fatal(e)
	}
	rejectNew := func() {
		t.Helper()
		if _, e := s.StartVerification(ctx, ledgerID(t), src.MissionID, src.FindingID, domain.VerificationInput{SourceRevisionID: src.ID, MaxSteps: 3}, func(context.Context) error { return nil }); !errors.Is(e, domain.ErrConflict) {
			t.Fatal("escaped unfinished execution", e)
		}
	}
	rejectNew() // approved不得借耗尽预算逃逸。
	claimed, e := s.ClaimAction(ctx)
	if e != nil || claimed == nil || claimed.ID != action.ID {
		t.Fatal(e)
	}
	rejectNew() // running不得逃逸。
	if e = s.FinishAction(ctx, action.ID, "outcome_unknown", domain.ActionResult{EffectUnknown: true}, "执行效果未知"); e != nil {
		t.Fatal(e)
	}
	rejectNew()
	before, e := s.GetVerification(ctx, src.MissionID, src.FindingID, v.VerificationID)
	if e != nil || before.EndReason != "" || before.Result != nil {
		t.Fatal("unknown incorrectly ended", e)
	}
	if e = s.ResolveUnknownAction(ctx, action.ID, "明确核对但不重放原请求"); e != nil {
		t.Fatal(e)
	}
	ended, e := s.GetVerification(ctx, src.MissionID, src.FindingID, v.VerificationID)
	if e != nil || ended.EndReason != "budget_exhausted" || ended.Result != nil || ended.StepCount != 1 {
		t.Fatal("missing program end", ended, e)
	}
	// 不伪造inconclusive，不清零旧预算；并发新发起与旧方向run交错。
	var wg sync.WaitGroup
	success := make(chan domain.VerificationReceipt, 2)
	errs := make(chan error, 2)
	for range 2 {
		key := ledgerID(t)
		wg.Go(func() {
			out, e := s.StartVerification(ctx, key, src.MissionID, src.FindingID, domain.VerificationInput{SourceRevisionID: src.ID, MaxSteps: 3}, func(context.Context) error { return nil })
			if e == nil {
				success <- out
			} else {
				errs <- e
			}
		})
	}
	wg.Go(func() {
		if _, e := s.SetThreadControl(ctx, v.ThreadID, "ready", true); !errors.Is(e, domain.ErrConflict) {
			t.Error("old run accepted", e)
		}
	})
	wg.Wait()
	close(success)
	close(errs)
	if len(success) != 1 || len(errs) != 1 {
		t.Fatal("concurrent new verification", len(success), len(errs))
	}
	for e := range errs {
		if !errors.Is(e, domain.ErrConflict) {
			t.Fatal(e)
		}
	}
	got, e := s.GetAction(ctx, action.ID)
	if e != nil || got.Status != "outcome_unknown" {
		t.Fatal("unknown history rewritten", e)
	}
	if got, e := s.ClaimAction(ctx); e != nil || got != nil {
		t.Fatal("replayed old target", got, e)
	}
}
func TestD2PendingAndPausedDoNotReleaseBudget(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	_, src := d2Source(t, s, ctx)
	v := d2Start(t, s, ctx, src, 1)
	a, _ := d2Call(t, s, ctx, v)
	action, e := s.ProposeAction(ctx, v.ThreadID, a.ID, fixtureAction())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.FinishAttempt(ctx, a.ID, "waiting_approval", "waiting_approval", "待审批", ""); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetThreadControl(ctx, v.ThreadID, "paused", false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.StartVerification(ctx, ledgerID(t), src.MissionID, src.FindingID, domain.VerificationInput{SourceRevisionID: src.ID, MaxSteps: 3}, func(context.Context) error { return nil }); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("escaped paused pending", e)
	}
	if _, e = s.DecideAction(ctx, action.ID, "rejected", "不发送"); e != nil {
		t.Fatal(e)
	}
	ended, e := s.GetVerification(ctx, src.MissionID, src.FindingID, v.VerificationID)
	if e != nil || ended.EndReason != "budget_exhausted" || ended.Result != nil {
		t.Fatal("rejected did not release only after settled", e)
	}
	_ = d2Start(t, s, ctx, src, 3)
}
