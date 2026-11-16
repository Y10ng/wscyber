package store

import (
	"encoding/json"
	"errors"
	"testing"
	"wscyber/internal/domain"
)

// 已引用原件后续变化不能改写旧历史，也不能把当前字节冒充旧时点材料。
func TestD1OriginalChangeAndStaleUpdate(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	o, err := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: "原时点材料"})
	if err != nil {
		t.Fatal(err)
	}
	a, _, _ := findingCall(t, s, ctx, th)
	p := findingProposal(domain.FindingReference{Kind: "observation", ID: o.ID, Relation: "supporting"})
	first, err := s.ApplyFinding(ctx, a.ID, p)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(first)
	finishFindingAttempt(t, s, ctx, a)
	// 只在随机测试schema模拟非API原件变化，产品没有修改原件接口。
	if _, err = s.pool.Exec(ctx, `UPDATE observations SET content='后续材料' WHERE id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	evidence, err := s.FindingEvidenceContent(ctx, th.MissionID, first.FindingID, first.ID, 0)
	if err != nil || evidence.MatchesSnapshot || evidence.Observation == nil || evidence.Observation.Content != "后续材料" || evidence.Reference.ContentSHA256 != first.Evidence[0].ContentSHA256 {
		t.Fatal("未明确区分当前原件与历史时点", err)
	}
	a2, _, _ := findingCall(t, s, ctx, th)
	p.FindingID = first.FindingID
	p.ExpectedRevision = 1
	p.Content.Description = "基于新原件更新"
	second, err := s.ApplyFinding(ctx, a2.ID, p)
	if err != nil || second.Revision != 2 || second.Evidence[0].ContentSHA256 == first.Evidence[0].ContentSHA256 {
		t.Fatal("新修订没有绑定新原件", err)
	}
	finishFindingAttempt(t, s, ctx, a2)
	a3, _, _ := findingCall(t, s, ctx, th)
	if _, err = s.ApplyFinding(ctx, a3.ID, p); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("接受过期expected_revision", err)
	}
	p.ExpectedRevision = 2
	third, err := s.ApplyFinding(ctx, a3.ID, p)
	if err != nil || third.Revision != 3 {
		t.Fatal("拒绝过期后无法保存正确修订", err)
	}
	old, err := s.GetFinding(ctx, th.MissionID, first.FindingID, first.ID)
	after, _ := json.Marshal(old)
	if err != nil || string(before) != string(after) {
		t.Fatal("旧历史被原件变化或更新覆盖", err)
	}
}

func TestD1ResolvedUnknownEvidenceRemainsUnknown(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	action, err := s.ProposeAction(ctx, th.ID, "", domain.ActionInput{Kind: "http", Method: "GET", URL: "http://127.0.0.1:9/fixture", Reason: "只验证持久化，不发送"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimAction(ctx)
	if err != nil || claimed == nil || claimed.ID != action.ID {
		t.Fatal(err)
	}
	if err = s.FinishAction(ctx, action.ID, "outcome_unknown", domain.ActionResult{EffectUnknown: true}, "合成未确认回执"); err != nil {
		t.Fatal(err)
	}
	if err = s.ResolveUnknownAction(ctx, action.ID, "人工仅核对，不证明成功"); err != nil {
		t.Fatal(err)
	}
	a, _, _ := findingCall(t, s, ctx, th)
	saved, err := s.ApplyFinding(ctx, a.ID, findingProposal(domain.FindingReference{Kind: "action", ID: action.ID, Relation: "limitation"}))
	if err != nil || saved.Status != "candidate" || !saved.Evidence[0].EffectUnknown || saved.Evidence[0].State != "outcome_unknown" {
		t.Fatal("核对被当作已成功", err)
	}
	if next, err := s.ClaimAction(ctx); err != nil || next != nil {
		t.Fatal("核对后重放原动作", err)
	}
}
