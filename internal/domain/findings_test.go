package domain

import (
	"strings"
	"testing"
)

func findingProposalFixture() FindingProposal {
	return FindingProposal{Content: FindingContent{Title: "候选", Description: "未验证主张", Steps: []FindingStep{{Text: "核对实际材料", EvidenceIndices: []int{0}}}, Evidence: []FindingReference{{Kind: "observation", ID: "11111111-1111-4111-8111-111111111111", Relation: "supporting"}}}}
}
func TestFindingProposalBoundsAndIdentity(t *testing.T) {
	if e := ValidateFindingProposal(findingProposalFixture()); e != nil {
		t.Fatal(e)
	}
	tests := []struct {
		name   string
		change func(*FindingProposal)
	}{
		{"self id without revision", func(p *FindingProposal) { p.FindingID = p.Content.Evidence[0].ID }},
		{"revision without id", func(p *FindingProposal) { p.ExpectedRevision = 1 }},
		{"bad kind", func(p *FindingProposal) { p.Content.Evidence[0].Kind = "url" }},
		{"bad relation", func(p *FindingProposal) { p.Content.Evidence[0].Relation = "confirmed" }},
		{"duplicate", func(p *FindingProposal) { p.Content.Evidence = append(p.Content.Evidence, p.Content.Evidence[0]) }},
		{"bad index", func(p *FindingProposal) { p.Content.Steps[0].EvidenceIndices = []int{1} }},
		{"duplicate index", func(p *FindingProposal) { p.Content.Steps[0].EvidenceIndices = []int{0, 0} }},
		{"oversize utf8", func(p *FindingProposal) { p.Content.Title = strings.Repeat("中", 86) }},
		{"invalid utf8", func(p *FindingProposal) { p.Content.Description = string([]byte{255}) }},
		{"nul", func(p *FindingProposal) { p.Content.Remediation = "a\x00b" }},
		{"no evidence", func(p *FindingProposal) { p.Content.Evidence = nil }},
		{"no steps", func(p *FindingProposal) { p.Content.Steps = nil }},
		{"total budget", func(p *FindingProposal) {
			p.Content.Description = strings.Repeat("x", 8192)
			p.Content.Remediation = strings.Repeat("x", 4096)
			p.Content.Limitations = strings.Repeat("x", 4096)
			for i := 0; i < 15; i++ {
				p.Content.Steps = append(p.Content.Steps, FindingStep{Text: strings.Repeat("x", 1024), EvidenceIndices: []int{}})
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := findingProposalFixture()
			tt.change(&p)
			if ValidateFindingProposal(p) == nil {
				t.Fatal("accepted invalid proposal")
			}
		})
	}
	p := findingProposalFixture()
	p.FindingID = p.Content.Evidence[0].ID
	p.ExpectedRevision = 1
	if e := ValidateFindingProposal(p); e != nil {
		t.Fatal(e)
	}
}
