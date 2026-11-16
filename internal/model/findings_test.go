package model

import (
	"encoding/json"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

func TestFindingDecisionStrictProtocol(t *testing.T) {
	p := &domain.FindingProposal{Content: domain.FindingContent{Title: "候选", Description: "未验证", Steps: []domain.FindingStep{{Text: "核对", EvidenceIndices: []int{0}}}, Evidence: []domain.FindingReference{{Kind: "action", ID: "11111111-1111-4111-8111-111111111111", Relation: "supporting"}}}}
	d := domain.Decision{Kind: "finding", Summary: "保存", NextStep: "继续研究", Finding: p}
	raw, _ := json.Marshal(d)
	if _, e := decodeDecision(string(raw)); e != nil {
		t.Fatal(e)
	}
	for _, invalid := range []string{strings.Replace(string(raw), `"title":`, `"status":"confirmed","title":`, 1), strings.Replace(string(raw), `"title":`, `"title":"重复","title":`, 1), strings.Replace(string(raw), `"finding":`, `"observation":"混用","finding":`, 1), strings.Replace(string(raw), `"候选"`, `"\ud800"`, 1), strings.Replace(string(raw), `"候选"`, `"\udfff"`, 1)} {
		if _, e := decodeDecision(invalid); e == nil {
			t.Fatal("accepted invalid decision")
		}
	}
	d.Kind = "observe"
	d.Observation = "观察"
	if ValidateDecision(d) == nil {
		t.Fatal("mixed payload")
	}
	if !validJSONUnicode(`{"x":"\ud83d\ude00","escaped":"\\ud800"}`) || validJSONUnicode("{\"x\":\""+string([]byte{255})+"\"}") {
		t.Fatal("unicode validation")
	}
	if !strings.Contains(systemPrompt, "finding") || !strings.Contains(systemPrompt, "未经独立验证") {
		t.Fatal("missing candidate prompt")
	}
}
