package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// 调用方已持有方向行锁；只有真实步数耗尽且无在途/未决动作才结束执行。
// 这是不可变的程序终止回执，不伪造任何模型结论，也不清零旧预算。
func finishVerificationBudget(ctx context.Context, tx pgx.Tx, thread string) error {
	var id, mission string
	err := tx.QueryRow(ctx, `SELECT v.id,v.mission_id FROM finding_verifications v JOIN threads t ON t.id=v.thread_id
 WHERE v.thread_id=$1 AND t.step_count>=v.max_steps
 AND NOT EXISTS(SELECT 1 FROM verification_results WHERE verification_id=v.id)
 AND NOT EXISTS(SELECT 1 FROM verification_ends WHERE verification_id=v.id)
 AND NOT EXISTS(SELECT 1 FROM attempts WHERE thread_id=t.id AND status='running')
 AND NOT EXISTS(SELECT 1 FROM actions WHERE thread_id=t.id AND `+unsettledPredicate+`)`, thread).Scan(&id, &mission)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO verification_ends(verification_id,reason) VALUES($1,'budget_exhausted')`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE threads SET status='blocked',auto_run=false,updated_at=clock_timestamp() WHERE id=$1`, thread); err != nil {
		return err
	}
	return appendEvent(ctx, tx, mission, thread, "verification.budget_ended", "验证预算已结束，未形成结论；旧证据与步数保留，可另行显式发起有限验证")
}
