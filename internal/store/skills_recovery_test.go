package store

import (
	"errors"
	"reflect"
	"testing"

	"wscyber/internal/domain"
)

func TestPostgresSkillsUnfinishedActivityRecovery(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	item := saveMaterial(t, s, ctx, 1, skillInput("recovery.md", "持久成功原件"))
	attempt := claimAttempt(t, ctx, s, thread.ID, 100)
	saved, err := s.ApplySkillActivity(ctx, attempt.ID, "skill_load", loadRequest(item))
	if err != nil {
		t.Fatal(err)
	}
	// 活动事务已提交，但未调用FinishAttempt；重开和恢复不重新执行加载。
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ApplySkillActivity(ctx, attempt.ID, "skill_load", loadRequest(item))
	if err != nil || !reflect.DeepEqual(saved, replay) {
		t.Fatal(replay, err)
	}
	usage, err := s.SkillUsage(ctx, thread.MissionID, "", 25)
	if err != nil || usage.TotalSuccessfulLoads != "1" {
		t.Fatal(usage, err)
	}
	current, err := s.GetSkillContext(ctx, thread.MissionID, thread.ID)
	if err != nil || len(current.Active) != 1 || current.Active[0].Content != "持久成功原件" {
		t.Fatal(current, err)
	}
	snap := mustSnapshot(t, ctx, s, thread.ID)
	if snap.Thread.Status != "blocked" {
		t.Fatal("恢复后应阻塞方向", snap.Thread)
	}
	// 既有恢复保留auto_run偏好；blocked状态才是派发门禁。
	if next, err := s.ClaimThread(ctx, 100); err != nil || next != nil {
		t.Fatal("恢复后不应自动认领/重放", next, err)
	}
	calls, err := s.SkillModelCalls(ctx, thread.MissionID, attempt.ID, "", 25)
	if err != nil || len(calls.Items) != 0 {
		t.Fatal("恢复补造了模型调用", calls, err)
	}
}
func TestPostgresSkillsCallRejectsOtherThreadAndMission(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	source := fixtureThread(t, ctx, s)
	item := saveMaterial(t, s, ctx, 1, skillInput("isolation.md", "固定原件"))
	load := skillActivity(t, s, ctx, source, "skill_load", loadRequest(item))
	sibling, err := s.CreateThread(ctx, domain.Thread{MissionID: source.MissionID, Title: "其他方向", Question: "隔离检查"})
	if err != nil {
		t.Fatal(err)
	}
	other := fixtureThread(t, ctx, s)
	for _, thread := range []domain.Thread{sibling, other} {
		a := claimAttempt(t, ctx, s, thread.ID, 100)
		call := domain.ModelCall{ID: ledgerID(t), ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: thread.MissionID, ThreadID: thread.ID, AttemptID: a.ID}
		if _, err = s.BeginModelCall(ctx, domain.BeginModelCall{Call: call, SkillLoadIDs: []string{load.Result.LoadID}}); err == nil {
			t.Fatal("跨方向/项目关联被接受")
		}
		if _, err = s.GetModelCall(ctx, call.ID, thread.MissionID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("错误关联留下孤立Begin", err)
		}
		if err = s.FinishAttempt(ctx, a.ID, "failed", "paused", "拒绝错归属", "人工检查"); err != nil {
			t.Fatal(err)
		}
	}
	calls, err := s.SkillModelCalls(ctx, source.MissionID, load.Result.LoadID, "", 25)
	if err != nil || len(calls.Items) != 0 {
		t.Fatal(calls, err)
	}
}
