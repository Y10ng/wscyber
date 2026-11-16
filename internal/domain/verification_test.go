package domain

import "testing"

func TestVerificationEvidenceIsNotHTTPStatusVerdict(t *testing.T) {
	for _, status := range []int{200, 302, 401, 403, 500} {
		a := Action{Status: "succeeded", Input: ActionInput{Kind: "http"}, Result: &ActionResult{StatusCode: status}}
		if !QualifyingVerificationAction(a) {
			t.Fatal("status used as business verdict", status)
		}
		a.Result.Truncated = true
		if QualifyingVerificationAction(a) {
			t.Fatal("truncated")
		}
		a.Result.Truncated = false
		a.Result.EffectUnknown = true
		if QualifyingVerificationAction(a) {
			t.Fatal("unknown")
		}
	}
	if QualifyingVerificationAction(Action{Status: "succeeded", Input: ActionInput{Kind: "local_command"}, Result: &ActionResult{ExitCode: 0}}) {
		t.Fatal("exit0 qualifies")
	}
}
func TestVerificationConclusionBounds(t *testing.T) {
	c := VerificationConclusion{Verdict: "inconclusive", Method: "实际前提不足", Summary: "不能确定", Evidence: []FindingReference{}}
	if ValidateVerificationConclusion(c) != nil {
		t.Fatal("inconclusive valid")
	}
	for _, verdict := range []string{"confirmed", "fixed", "false_positive", ""} {
		x := c
		x.Verdict = verdict
		if ValidateVerificationConclusion(x) == nil {
			t.Fatal(verdict)
		}
	}
	c.Method = "\x00"
	if ValidateVerificationConclusion(c) == nil {
		t.Fatal("NUL")
	}
	for _, steps := range []int{0, 9, -1} {
		if ValidateVerificationInput(VerificationInput{SourceRevisionID: "11111111-1111-4111-8111-111111111111", MaxSteps: steps}) == nil {
			t.Fatal("budget", steps)
		}
	}
}
