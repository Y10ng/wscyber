package store

import (
	"strings"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/policy"
)

func TestPostgresAutomaticReleaseRespectsAttemptPauseAndUnknown(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	attempt := claimAttempt(t, ctx, s, thread.ID, 3)
	action, err := s.ProposeAction(ctx, thread.ID, attempt.ID, domain.ActionInput{Kind: "local_command", Command: "true", Reason: "只验证存储，不执行"})
	if err != nil || action.Status != "approved" || !strings.Contains(action.DecisionReason, policy.Version) {
		t.Fatal("新命令必须带策略依据自动放行", action, err)
	}
	if got, err := s.ClaimAction(ctx); err != nil || got != nil {
		t.Fatal("自动放行不能抢在研究尝试结束前执行", got, err)
	}
	if err = s.FinishAttempt(ctx, attempt.ID, "succeeded", "ready", "动作已保存", "消费回执"); err != nil {
		t.Fatal(err)
	}
	if got := mustSnapshot(t, ctx, s, thread.ID); got.Thread.Status != "ready" || got.Attempts[0].Status != "succeeded" {
		t.Fatal("自动放行不应标记等待人工", got)
	}
	if got, err := s.ClaimThread(ctx, 3); err != nil || got != nil {
		t.Fatal("动作未完成不能再次调用模型", got, err)
	}
	if _, err = s.SetThreadControl(ctx, thread.ID, "paused", false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClaimAction(ctx); err != nil || got != nil {
		t.Fatal("暂停必须阻止自动放行的动作", got, err)
	}
	if _, err = s.SetThreadControl(ctx, thread.ID, "ready", false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClaimAction(ctx); err != nil || got == nil || got.ID != action.ID {
		t.Fatal("恢复后应可认领自动放行动作", got, err)
	}
	if err = s.FinishAction(ctx, action.ID, "outcome_unknown", domain.ActionResult{EffectUnknown: true}, "测试未知效果"); err != nil {
		t.Fatal(err)
	}
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClaimAction(ctx); err != nil || got != nil {
		t.Fatal("自动放行的动作也不能重放未知效果", got, err)
	}
}

func TestPostgresLegacyPendingIsNotAutomaticallyReleased(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	input := domain.ActionInput{Kind: "http", Method: "GET", URL: "https://fixture.invalid/status", Reason: "旧版本普通请求"}
	encoded, hash, err := canonicalInput(input)
	if err != nil {
		t.Fatal(err)
	}
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	// 模拟升级前留下的 pending；恢复不能重新套新策略将它悄悄放行。
	if _, err = s.pool.Exec(ctx, `INSERT INTO actions(id, thread_id, input, request_hash) VALUES ($1,$2,$3,$4)`, id, thread.ID, encoded, hash); err != nil {
		t.Fatal(err)
	}
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetAction(ctx, id); err != nil || got.Status != "pending" || got.DecisionReason != "" {
		t.Fatal("历史待审语义被改写", got, err)
	}
	if got, err := s.ClaimAction(ctx); err != nil || got != nil {
		t.Fatal("历史待审不得自动认领", got, err)
	}
}
