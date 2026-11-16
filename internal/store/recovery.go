package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
)

// Recover 仅在 Open 成功持有单实例锁、调度器尚未启动时调用。
// 此处只保存中断事实，不认领工作、不调用执行器，也不改变 pending/approved 动作。
func (s *Store) Recover(ctx context.Context) error {
	return s.mutate(ctx, func(tx pgx.Tx) error {
		// 先锁定全部待恢复方向，再写子记录与事件，保持统一锁顺序。
		threads, err := collect(ctx, tx, `SELECT `+threadColumns+` FROM threads t WHERE
			status = 'running'
			OR EXISTS (SELECT 1 FROM attempts WHERE thread_id = t.id AND status = 'running')
			OR EXISTS (SELECT 1 FROM actions WHERE thread_id = t.id AND status = 'running')
			OR (status NOT IN ('blocked', 'paused') AND EXISTS
				(SELECT 1 FROM actions WHERE thread_id = t.id AND status = 'outcome_unknown' AND resolved_at IS NULL))
			ORDER BY id FOR UPDATE OF t`, scanThread)
		if err != nil {
			return err
		}
		for _, t := range threads {
			attempts, err := collect(ctx, tx, `UPDATE attempts SET status = 'interrupted',
				error = '服务重启时执行仍在运行，已中断；研究问题保持未决',
				ended_at = clock_timestamp() WHERE thread_id = $1 AND status = 'running'
				RETURNING `+attemptColumns, scanAttempt, t.ID)
			if err != nil {
				return err
			}
			actions, err := collect(ctx, tx, `UPDATE actions SET status = 'outcome_unknown',
				error = '服务重启时动作尚无已保存回执，效果未知；禁止自动重放',
				updated_at = clock_timestamp() WHERE thread_id = $1 AND status = 'running'
				RETURNING `+actionColumns, scanAction, t.ID)
			if err != nil {
				return err
			}
			pending, err := unsettledAction(ctx, tx, t.ID)
			if err != nil {
				return err
			}
			reason := "服务重启中断了执行；已有材料保留，研究问题未被反证，请人工恢复"
			if pending != nil && pending.Status == "outcome_unknown" {
				reason = "服务重启后动作效果未知，须先人工核对并 resolve；禁止自动重放"
			}
			if _, err = tx.Exec(ctx, `UPDATE threads SET status = 'blocked', next_step = $2,
				updated_at = clock_timestamp() WHERE id = $1`, t.ID, reason); err != nil {
				return err
			}
			for _, attempt := range attempts {
				if err = appendEvent(ctx, tx, t.MissionID, t.ID, "attempt.interrupted", "启动恢复标记执行中断："+attempt.ID); err != nil {
					return err
				}
			}
			for _, action := range actions {
				if _, err = insertObservation(ctx, tx, domain.Observation{
					ThreadID: t.ID, AttemptID: action.AttemptID, ActionID: action.ID,
					Kind: "action_outcome_unknown", Content: action.Error,
				}); err != nil {
					return err
				}
				if err = appendEvent(ctx, tx, t.MissionID, t.ID, "action.outcome_unknown", "启动恢复保留未知效果："+action.ID); err != nil {
					return err
				}
			}
			if err = finishVerificationBudget(ctx, tx, t.ID); err != nil {
				return err
			}
			if err = appendEvent(ctx, tx, t.MissionID, t.ID, "thread.blocked", reason); err != nil {
				return err
			}
		}
		if err := interruptModelCalls(ctx, tx); err != nil {
			return err
		}
		return interruptModelTests(ctx, tx)
	})
}
