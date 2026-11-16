package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
)

// SetThreadControl 的 ready,false 是人工恢复，不清零步数，也不重新批准任何动作。
// pause_requested 区分“人工模式”与“在途期间请求暂停”；该内部标记不伪造运行状态。
func (s *Store) SetThreadControl(ctx context.Context, id, status string, autoRun bool) (domain.Thread, error) {
	if status != "ready" && status != "paused" {
		return domain.Thread{}, invalid("方向控制只接受 ready 或 paused")
	}
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.Thread, error) {
		return setThreadControl(ctx, tx, id, status, autoRun)
	})
}

// 与可靠控制复用同一迁移；调用者在同一事务追加回执。
func setThreadControl(ctx context.Context, tx pgx.Tx, id, status string, autoRun bool) (domain.Thread, error) {
	t, err := lockThread(ctx, tx, id)
	if err != nil {
		return t, err
	}
	if t.Status == "concluded" {
		return t, conflict("已结束方向不能隐式重开")
	}
	if done, err := verificationFinished(ctx, tx, id); err != nil {
		return t, err
	} else if done {
		return t, conflict("验证结果已保存，不能重开或再次派发")
	}
	active, err := runningAttempt(ctx, tx, id)
	if err != nil {
		return t, err
	}
	action, err := unsettledAction(ctx, tx, id)
	if err != nil {
		return t, err
	}
	inFlight := active || (action != nil && action.Status == "running") || t.Status == "running"
	newStatus, pause := status, status == "paused"
	if pause {
		autoRun = false
		if inFlight {
			newStatus = t.Status
		}
	} else {
		if inFlight {
			return t, conflict("已有在途执行；恢复不能创建并行执行")
		}
		if action != nil && action.Status == "outcome_unknown" {
			return t, conflict("须先人工核对并 resolve 效果未知的动作")
		}
	}
	t, err = scanThread(tx.QueryRow(ctx, `UPDATE threads SET status = $2, auto_run = $3,
			pause_requested = $4, updated_at = clock_timestamp() WHERE id = $1 RETURNING `+threadColumns,
		id, newStatus, autoRun, pause))
	if err != nil {
		return t, err
	}
	message := "研究方向已恢复；累计步数和动作历史保持不变"
	if pause {
		message = "已停止后续派发；在途执行仍将保存真实结果"
	} else if autoRun {
		message = "已启用自动研究；累计步数和动作历史保持不变"
	}
	return t, appendEvent(ctx, tx, t.MissionID, t.ID, "thread.controlled", message)
}

// ClaimThread 每次仅锁一个候选方向，SKIP LOCKED 支持同实例有界 worker。
// 预算耗尽不是方向结论；提交 blocked 后继续寻找其他可执行方向。
func (s *Store) ClaimThread(ctx context.Context, maxSteps int) (*domain.Attempt, error) {
	if maxSteps <= 0 {
		return nil, invalid("研究步数预算必须大于零")
	}
	for {
		type claim struct {
			attempt *domain.Attempt
			blocked bool
		}
		result, err := transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (claim, error) {
			t, err := scanThread(tx.QueryRow(ctx, `SELECT `+threadColumns+` FROM threads t
				WHERE status = 'ready' AND auto_run AND NOT pause_requested
				AND NOT EXISTS (SELECT 1 FROM finding_verifications v JOIN verification_results r ON r.verification_id=v.id WHERE v.thread_id=t.id)
				AND NOT EXISTS (SELECT 1 FROM attempts WHERE thread_id = t.id AND status = 'running')
				AND NOT EXISTS (SELECT 1 FROM actions WHERE thread_id = t.id AND `+unsettledPredicate+`)
				ORDER BY priority DESC, created_at, id FOR UPDATE OF t SKIP LOCKED LIMIT 1`))
			if errors.Is(err, domain.ErrNotFound) {
				return claim{}, nil
			}
			if err != nil {
				return claim{}, err
			}
			// 行锁取得后再次检查子记录，避免语句快照早于竞争事务提交。
			active, err := runningAttempt(ctx, tx, t.ID)
			if err != nil {
				return claim{}, err
			}
			action, err := unsettledAction(ctx, tx, t.ID)
			if err != nil {
				return claim{}, err
			}
			if active || action != nil {
				return claim{}, nil
			}
			budget := maxSteps
			var ownBudget int
			err = tx.QueryRow(ctx, `SELECT max_steps FROM finding_verifications WHERE thread_id=$1`, t.ID).Scan(&ownBudget)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return claim{}, err
			}
			if ownBudget > 0 {
				budget = min(budget, ownBudget)
			}
			if t.StepCount >= budget {
				reason := fmt.Sprintf("已达到研究步数预算（%d/%d）；问题保持未决。人工恢复不重置累计步数。", t.StepCount, budget)
				if _, err = tx.Exec(ctx, `UPDATE threads SET status = 'blocked', next_step = $2,
					updated_at = clock_timestamp() WHERE id = $1`, t.ID, reason); err != nil {
					return claim{}, err
				}
				if err = appendEvent(ctx, tx, t.MissionID, t.ID, "thread.budget_blocked", reason); err != nil {
					return claim{}, err
				}
				return claim{blocked: true}, finishVerificationBudget(ctx, tx, t.ID)
			}
			id, err := newID()
			if err != nil {
				return claim{}, err
			}
			a, err := scanAttempt(tx.QueryRow(ctx, `INSERT INTO attempts(id, thread_id, status)
				VALUES ($1, $2, 'running') RETURNING `+attemptColumns, id, t.ID))
			if err != nil {
				return claim{}, err
			}
			if _, err = tx.Exec(ctx, `UPDATE threads SET status = 'running', step_count = step_count + 1,
				updated_at = clock_timestamp() WHERE id = $1`, t.ID); err != nil {
				return claim{}, err
			}
			return claim{attempt: &a}, appendEvent(ctx, tx, t.MissionID, t.ID, "attempt.started", "已认领研究执行："+a.ID)
		})
		if err != nil {
			return nil, err
		}
		if !result.blocked {
			return result.attempt, nil
		}
	}
}

func (s *Store) FinishAttempt(ctx context.Context, id, attemptStatus, threadStatus, summary, nextStep string) error {
	switch attemptStatus {
	case "succeeded", "failed", "interrupted", "waiting_approval":
	default:
		return invalid("执行尝试的完成状态无效")
	}
	switch threadStatus {
	case "ready", "blocked", "paused", "concluded", "waiting_approval":
	default:
		return invalid("执行尝试不能迁移到该方向状态")
	}
	return s.mutate(ctx, func(tx pgx.Tx) error {
		t, err := lockAttemptThread(ctx, tx, id)
		if err != nil {
			return err
		}
		a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if a.Status != "running" || t.Status == "concluded" {
			return conflict("执行尝试已结束，不能用迟到结果覆盖")
		}
		pending, err := unsettledAction(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		pause, err := pauseRequested(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		if pending != nil && (pending.Status == "running" || pending.AttemptID != id) {
			return conflict("方向存在不属于本次执行的未决动作")
		}
		if (attemptStatus == "waiting_approval" || threadStatus == "waiting_approval") &&
			(pending == nil || (pending.Status != "pending" && pending.Status != "approved")) {
			// 人工可能在 ProposeAction 与本次 FinishAttempt 之间已拒绝动作。
			// 此时不能重新制造等待，也不能把已保存的模型执行永久留在 running。
			var rejected bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM actions
				WHERE thread_id = $1 AND attempt_id = $2 AND status = 'rejected')`, t.ID, id).Scan(&rejected); err != nil {
				return err
			}
			if !rejected || pending != nil {
				return conflict("没有本次执行提出的可等待审批动作")
			}
			if threadStatus == "waiting_approval" {
				threadStatus = "ready"
			}
		}
		if pending != nil && threadStatus == "concluded" {
			return conflict("存在未决动作，不能结束研究方向")
		}
		errorText := ""
		if attemptStatus == "failed" || attemptStatus == "interrupted" {
			// Repository 没有独立错误参数，调用方传入的失败 summary 同时保存为 Error。
			errorText = summary
			threadStatus = "blocked"
			if nextStep == "" {
				nextStep = summary
			}
		} else if pending != nil {
			threadStatus = "ready" // approved 等待执行，不是等待人工；ClaimThread 仍排除未决动作。
			if pending.Status == "pending" {
				threadStatus = "waiting_approval"
			}
		}
		if pending != nil && pending.Status == "outcome_unknown" {
			threadStatus = "blocked"
			nextStep = "动作效果未知，须先人工核对并 resolve；禁止自动重放"
		} else if pause && threadStatus != "concluded" {
			threadStatus = "paused"
		}
		if _, err = tx.Exec(ctx, `UPDATE attempts SET status = $2, summary = $3, error = $4,
			ended_at = clock_timestamp() WHERE id = $1`, id, attemptStatus, summary, errorText); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE threads SET status = $2, next_step = $3,
			auto_run = CASE WHEN $2 IN ('paused', 'concluded') THEN false ELSE auto_run END,
			pause_requested = CASE WHEN $2 = 'paused' THEN true ELSE pause_requested END,
			updated_at = clock_timestamp() WHERE id = $1`, t.ID, threadStatus, nextStep); err != nil {
			return err
		}
		if err = appendEvent(ctx, tx, t.MissionID, t.ID, "attempt.finished", "研究执行 "+id+" 已保存："+attemptStatus+"；"+summary); err != nil {
			return err
		}
		return finishVerificationBudget(ctx, tx, t.ID)
	})
}
