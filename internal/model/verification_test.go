package model

import (
	"strings"
	"testing"
	"wscyber/internal/domain"
)

func TestVerificationDecisionModeAndStrictDecode(t *testing.T) {
	raw := `{"kind":"verification","summary":"有限判读","next_step":"","verification":{"verdict":"inconclusive","method":"检查前提","preconditions":"缺有效身份","summary":"本次不足","limitations":"不判修复","evidence":[]}}`
	d, e := decodeDecision(raw)
	if e != nil {
		t.Fatal(e)
	}
	if ValidateDecisionForSnapshot(d, domain.Snapshot{}) == nil {
		t.Fatal("ordinary accepted verification")
	}
	snapshot := domain.Snapshot{Verification: &domain.VerificationContext{}}
	if ValidateDecisionForSnapshot(d, snapshot) != nil {
		t.Fatal("verification rejected")
	}
	if ValidateDecisionForSnapshot(domain.Decision{Kind: "conclude", Summary: "完成"}, snapshot) == nil {
		t.Fatal("unstructured conclude")
	}
	for _, bad := range []string{strings.Replace(raw, `"inconclusive"`, `"confirmed"`, 1), strings.Replace(raw, `"method":"检查前提"`, `"method":"a","method":"b"`, 1), strings.Replace(raw, `"method":"检查前提"`, `"method":"\ud800"`, 1)} {
		if _, e = decodeDecision(bad); e == nil {
			t.Fatal("bad JSON admitted")
		}
	}
	prompt := taskSystemPrompt(snapshot)
	if !strings.Contains(prompt, "500错误或302重定向") || !strings.Contains(prompt, "不能删掉限制材料") {
		t.Fatal("missing evidence boundary")
	}
}
