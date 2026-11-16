package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
	"wscyber/internal/policy"
)

// ProposeAction 同事务保存请求与放行策略；范围校验由调用方的 Executor.Validate 完成。
// 不在事务中执行目标；approved 提交后可立即被后台认领。
// 人工请求不能插入在途研究；模型请求必须绑定同方向的 running Attempt。
func (s *Store) ProposeAction(ctx context.Context, threadID, attemptID string, input domain.ActionInput) (domain.Action, error) {
	encoded, hash, err := canonicalInput(input)
	if err != nil {
		return domain.Action{}, err
	}
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.Action, error) {
		t, err := lockThread(ctx, tx, threadID)
		if err != nil {
			return domain.Action{}, err
		}
		if t.Status == "concluded" {
			return domain.Action{}, conflict("已结束方向不能提出新动作")
		}
		if done, e := verificationFinished(ctx, tx, threadID); e != nil {
			return domain.Action{}, e
		} else if done {
			return domain.Action{}, conflict("验证结果已保存，不能提出新动作")
		}
		if attemptID == "" {
			active, err := runningAttempt(ctx, tx, threadID)
			if err != nil {
				return domain.Action{}, err
			}
			if active || t.Status == "running" {
				return domain.Action{}, conflict("方向正在执行，不能插入人工动作")
			}
		} else {
			a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE id = $1`, attemptID))
			if err != nil {
				return domain.Action{}, err
			}
			if a.ThreadID != threadID {
				return domain.Action{}, invalid("执行尝试不属于该方向")
			}
			if a.Status != "running" || t.Status != "running" {
				return domain.Action{}, conflict("模型动作必须来自仍在运行的执行尝试")
			}
		}
		pending, err := unsettledAction(ctx, tx, threadID)
		if err != nil {
			return domain.Action{}, err
		}
		if pending != nil {
			return domain.Action{}, conflict("该方向已有未决动作；效果未知动作须先人工 resolve")
		}
		id, err := newID()
		if err != nil {
			return domain.Action{}, err
		}
		decision := policy.Evaluate(input)
		a, err := scanAction(tx.QueryRow(ctx, `INSERT INTO actions(id, thread_id, attempt_id, input, request_hash, status, decision_reason)
			VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7) RETURNING `+actionColumns,
			id, threadID, attemptID, encoded, hash, decision.Status, decision.Reason))
		if err != nil {
			return a, err
		}
		// 模型先提出动作，再完成 Attempt；期间保留 running，审批也不能抢先执行。
		if _, err = tx.Exec(ctx, `UPDATE threads SET
			status = CASE WHEN status IN ('running', 'paused') OR pause_requested THEN status
				WHEN $2 = 'pending' THEN 'waiting_approval' ELSE 'ready' END,
			updated_at = clock_timestamp() WHERE id = $1`, threadID, decision.Status); err != nil {
			return a, err
		}
		// 策略理由与不可变请求同事务保存；人工决定可覆盖 decision_reason，初始理由保留在事件中。
		return a, appendEvent(ctx, tx, t.MissionID, t.ID, "action.proposed", "动作已持久化："+a.ID+"；"+decision.Reason)
	})
}

func (s *Store) GetAction(ctx context.Context, id string) (domain.Action, error) {
	return scanAction(s.pool.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id = $1`, id))
}

func (s *Store) ListActions(ctx context.Context, missionID string) ([]domain.Action, error) {
	if missionID != "" {
		if _, err := getMission(ctx, s.pool, missionID); err != nil {
			return nil, err
		}
	}
	return collect(ctx, s.pool, `SELECT `+actionColumns+` FROM actions
		WHERE ($1 = '' OR thread_id IN (SELECT id FROM threads WHERE mission_id = $1)) ORDER BY created_at, id`, scanAction, missionID)
}

func (s *Store) DecideAction(ctx context.Context, id, decision, reason string) (domain.Action, error) {
	if decision != "approved" && decision != "rejected" {
		return domain.Action{}, invalid("审批只接受 approved 或 rejected")
	}
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.Action, error) {
		t, err := lockActionThread(ctx, tx, id)
		if err != nil {
			return domain.Action{}, err
		}
		a, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return a, err
		}
		if a.Status == decision {
			// 同一决定可重试，但不覆盖已生效原因，也不重复写事件。
			return a, nil
		}
		if a.Status != "pending" {
			return a, conflict("动作已被决定或开始执行，不能覆盖审批")
		}
		if t.Status == "concluded" {
			return a, conflict("方向已经结束")
		}
		a, err = scanAction(tx.QueryRow(ctx, `UPDATE actions SET status = $2, decision_reason = $3,
			updated_at = clock_timestamp() WHERE id = $1 RETURNING `+actionColumns, id, decision, reason))
		if err != nil {
			return a, err
		}
		active, err := runningAttempt(ctx, tx, t.ID)
		if err != nil {
			return a, err
		}
		if !active {
			status := "waiting_approval"
			if decision == "rejected" {
				status = "ready"
			}
			if _, err = tx.Exec(ctx, `UPDATE threads SET
				status = CASE WHEN status = 'paused' OR pause_requested THEN 'paused' ELSE $2 END,
				updated_at = clock_timestamp() WHERE id = $1`, t.ID, status); err != nil {
				return a, err
			}
		}
		if err = appendEvent(ctx, tx, t.MissionID, t.ID, "action.decided", "动作 "+id+" 审批决定："+decision+"；"+reason); err != nil {
			return a, err
		}
		return a, finishVerificationBudget(ctx, tx, t.ID)
	})
}

func (s *Store) ClaimAction(ctx context.Context) (*domain.Action, error) {
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (*domain.Action, error) {
		t, err := scanThread(tx.QueryRow(ctx, `SELECT `+threadColumns+` FROM threads t
			WHERE status NOT IN ('paused', 'concluded', 'running') AND NOT pause_requested
			AND NOT EXISTS (SELECT 1 FROM finding_verifications v JOIN verification_results r ON r.verification_id=v.id WHERE v.thread_id=t.id)
			AND NOT EXISTS (SELECT 1 FROM attempts WHERE thread_id = t.id AND status = 'running')
			AND EXISTS (SELECT 1 FROM actions WHERE thread_id = t.id AND status = 'approved')
			ORDER BY (SELECT min(created_at) FROM actions WHERE thread_id = t.id AND status = 'approved'), created_at, id
			FOR UPDATE OF t SKIP LOCKED LIMIT 1`))
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		active, err := runningAttempt(ctx, tx, t.ID)
		if err != nil {
			return nil, err
		}
		a, err := unsettledAction(ctx, tx, t.ID)
		if err != nil {
			return nil, err
		}
		if active || a == nil || a.Status != "approved" {
			return nil, nil
		}
		_, hash, err := canonicalInput(a.Input)
		if err != nil {
			return nil, err
		}
		if hash != a.RequestHash {
			return nil, conflict("动作请求与批准绑定的 hash 不一致")
		}
		claimed, err := scanAction(tx.QueryRow(ctx, `UPDATE actions SET status = 'running', updated_at = clock_timestamp()
			WHERE id = $1 RETURNING `+actionColumns, a.ID))
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, `UPDATE threads SET status = 'running', updated_at = clock_timestamp() WHERE id = $1`, t.ID); err != nil {
			return nil, err
		}
		if err = appendEvent(ctx, tx, t.MissionID, t.ID, "action.started", "动作已认领；完成前不得重复派发："+a.ID); err != nil {
			return nil, err
		}
		return &claimed, nil
	})
}

func (s *Store) FinishAction(ctx context.Context, id, status string, result domain.ActionResult, errorText string) error {
	if status != "succeeded" && status != "failed" && status != "outcome_unknown" {
		return invalid("动作完成状态无效")
	}
	// 执行器的未知效果标记优先于调用方给出的成功或失败标签。
	if result.EffectUnknown || status == "outcome_unknown" {
		status = "outcome_unknown"
		result.EffectUnknown = true
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return invalid("动作结果无法序列化")
	}
	observation, err := json.Marshal(struct {
		Status string              `json:"status"`
		Result domain.ActionResult `json:"result"`
		Error  string              `json:"error"`
	}{status, result, errorText})
	if err != nil {
		return invalid("动作观察无法序列化")
	}
	return s.mutate(ctx, func(tx pgx.Tx) error {
		t, err := lockActionThread(ctx, tx, id)
		if err != nil {
			return err
		}
		a, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if a.Status != "running" {
			return conflict("动作不在运行中；终态或未知历史不能被迟到结果覆盖")
		}
		if _, err = tx.Exec(ctx, `UPDATE actions SET status = $2, result = $3, error = $4,
			updated_at = clock_timestamp() WHERE id = $1`, id, status, encoded, errorText); err != nil {
			return err
		}
		if _, err = insertObservation(ctx, tx, domain.Observation{
			ThreadID: t.ID, AttemptID: a.AttemptID, ActionID: id, Kind: "action_result", Content: string(observation),
		}); err != nil {
			return err
		}
		pause, err := pauseRequested(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		threadStatus, nextStep := "ready", t.NextStep
		if status == "outcome_unknown" {
			threadStatus = "blocked"
			nextStep = "动作效果未知，须先人工核对并 resolve；禁止自动重放"
		} else if pause || t.Status == "paused" {
			threadStatus = "paused"
		}
		if _, err = tx.Exec(ctx, `UPDATE threads SET status = $2, next_step = $3,
			updated_at = clock_timestamp() WHERE id = $1`, t.ID, threadStatus, nextStep); err != nil {
			return err
		}
		if err = appendEvent(ctx, tx, t.MissionID, t.ID, "action.finished", "动作 "+id+" 结果与观察已保存："+status+"；"+errorText); err != nil {
			return err
		}
		return finishVerificationBudget(ctx, tx, t.ID)
	})
}

func (s *Store) ResolveUnknownAction(ctx context.Context, id, note string) error {
	if strings.TrimSpace(note) == "" {
		return invalid("必须记录人工核对说明")
	}
	return s.mutate(ctx, func(tx pgx.Tx) error {
		t, err := lockActionThread(ctx, tx, id)
		if err != nil {
			return err
		}
		a, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if a.Status != "outcome_unknown" || a.ResolvedAt != nil {
			return conflict("只有未核对的效果未知动作可以 resolve")
		}
		if _, err = tx.Exec(ctx, `UPDATE actions SET resolved_at = clock_timestamp(), resolution_note = $2,
			updated_at = clock_timestamp() WHERE id = $1`, id, note); err != nil {
			return err
		}
		// resolve 不把历史改成成功，也不自动启动模型；新工作仍须明确 run 或人工提出。
		if _, err = tx.Exec(ctx, `UPDATE threads SET
			status = CASE WHEN status = 'paused' OR pause_requested THEN 'paused' ELSE 'ready' END,
			auto_run = false, next_step = '未知动作已由人工核对；历史保留，未重放动作，自动研究未启用',
			updated_at = clock_timestamp() WHERE id = $1`, t.ID); err != nil {
			return err
		}
		if err = appendEvent(ctx, tx, t.MissionID, t.ID, "action.resolved", "人工已核对未知动作 "+id+"，保留未知历史："+note); err != nil {
			return err
		}
		return finishVerificationBudget(ctx, tx, t.ID)
	})
}
