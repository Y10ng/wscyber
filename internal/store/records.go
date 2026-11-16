package store

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
)

func (s *Store) CreateMission(ctx context.Context, input domain.Mission) (domain.Mission, error) {
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Goal) == "" {
		return domain.Mission{}, invalid("项目标题和目标不能为空")
	}
	if input.AllowedOrigins == nil {
		input.AllowedOrigins = []string{}
	}
	origins, err := json.Marshal(input.AllowedOrigins)
	if err != nil {
		return domain.Mission{}, err
	}
	id, err := newID()
	if err != nil {
		return domain.Mission{}, err
	}
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.Mission, error) {
		m, err := scanMission(tx.QueryRow(ctx, `INSERT INTO missions(id, title, origin, goal, allowed_origins)
			VALUES ($1, $2, $3, $4, $5) RETURNING `+missionColumns,
			id, input.Title, input.Origin, input.Goal, origins))
		if err != nil {
			return m, err
		}
		err = appendEvent(ctx, tx, m.ID, "", "mission.created", "研究项目已创建")
		return m, err
	})
}

func (s *Store) ListMissions(ctx context.Context) ([]domain.Mission, error) {
	return collect(ctx, s.pool, `SELECT `+missionColumns+` FROM missions ORDER BY created_at, id`, scanMission)
}

func (s *Store) GetMission(ctx context.Context, id string) (domain.Mission, error) {
	return getMission(ctx, s.pool, id)
}

func (s *Store) CreateThread(ctx context.Context, input domain.Thread) (domain.Thread, error) {
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Question) == "" || input.MissionID == "" {
		return domain.Thread{}, invalid("方向必须有项目、标题和研究问题")
	}
	id, err := newID()
	if err != nil {
		return domain.Thread{}, err
	}
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.Thread, error) {
		if _, err := getMission(ctx, tx, input.MissionID); err != nil {
			return domain.Thread{}, err
		}
		t, err := scanThread(tx.QueryRow(ctx, `INSERT INTO threads(id, mission_id, title, question, rationale, priority)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+threadColumns,
			id, input.MissionID, input.Title, input.Question, input.Rationale, input.Priority))
		if err != nil {
			return t, err
		}
		err = appendEvent(ctx, tx, t.MissionID, t.ID, "thread.created", "研究方向已创建，自动研究未启用")
		return t, err
	})
}

func (s *Store) ListThreads(ctx context.Context, missionID string) ([]domain.Thread, error) {
	if _, err := getMission(ctx, s.pool, missionID); err != nil {
		return nil, err
	}
	return collect(ctx, s.pool, `SELECT `+threadColumns+` FROM threads WHERE mission_id = $1
		ORDER BY priority DESC, created_at, id`, scanThread, missionID)
}

func (s *Store) GetThread(ctx context.Context, id string) (domain.Thread, error) {
	return getThread(ctx, s.pool, id)
}

// Snapshot 使用同一只读快照，避免把动作提交前的方向与提交后的观察拼接在一起。
func (s *Store) Snapshot(ctx context.Context, id string) (domain.Snapshot, error) {
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.Snapshot, error) {
		var snapshot domain.Snapshot
		var err error
		if snapshot.Thread, err = getThread(ctx, tx, id); err != nil {
			return snapshot, err
		}
		if snapshot.Mission, err = getMission(ctx, tx, snapshot.Thread.MissionID); err != nil {
			return snapshot, err
		}
		if snapshot.Observations, err = collect(ctx, tx, `SELECT `+observationColumns+` FROM observations WHERE thread_id = $1 ORDER BY created_at, id`, scanObservation, id); err != nil {
			return snapshot, err
		}
		if snapshot.Actions, err = collect(ctx, tx, `SELECT `+actionColumns+` FROM actions WHERE thread_id = $1 ORDER BY created_at, id`, scanAction, id); err != nil {
			return snapshot, err
		}
		snapshot.Attempts, err = collect(ctx, tx, `SELECT `+attemptColumns+` FROM attempts WHERE thread_id = $1 ORDER BY started_at, id`, scanAttempt, id)
		return snapshot, err
	})
}

func (s *Store) AddObservation(ctx context.Context, input domain.Observation) (domain.Observation, error) {
	if input.ThreadID == "" || strings.TrimSpace(input.Kind) == "" || strings.TrimSpace(input.Content) == "" {
		return domain.Observation{}, invalid("观察必须有方向、类型和内容")
	}
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.Observation, error) {
		t, err := lockThread(ctx, tx, input.ThreadID)
		if err != nil {
			return domain.Observation{}, err
		}
		if input.AttemptID != "" {
			a, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE id = $1`, input.AttemptID))
			if err != nil {
				return domain.Observation{}, err
			}
			if a.ThreadID != t.ID {
				return domain.Observation{}, invalid("观察的执行尝试不属于该方向")
			}
			if a.Status != "running" {
				return domain.Observation{}, conflict("执行尝试已结束，不能补写模型观察")
			}
		}
		if input.ActionID != "" {
			a, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id = $1`, input.ActionID))
			if err != nil {
				return domain.Observation{}, err
			}
			if a.ThreadID != t.ID || (input.AttemptID != "" && a.AttemptID != input.AttemptID) {
				return domain.Observation{}, invalid("观察的动作与方向或执行尝试不匹配")
			}
		}
		o, err := insertObservation(ctx, tx, input)
		if err != nil {
			return o, err
		}
		return o, appendEvent(ctx, tx, t.MissionID, t.ID, "observation.added", "已保存观察："+o.Kind)
	})
}

// 调用者已锁定方向并校验来源；FinishAction 也通过此函数在同一事务保存证据。
func insertObservation(ctx context.Context, tx pgx.Tx, input domain.Observation) (domain.Observation, error) {
	id, err := newID()
	if err != nil {
		return domain.Observation{}, err
	}
	return scanObservation(tx.QueryRow(ctx, `INSERT INTO observations(id, thread_id, attempt_id, action_id, kind, content)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6) RETURNING `+observationColumns,
		id, input.ThreadID, input.AttemptID, input.ActionID, input.Kind, input.Content))
}

func (s *Store) ListEvents(ctx context.Context, missionID string, after int64) ([]domain.Event, error) {
	if after < 0 {
		return nil, invalid("事件游标不能为负数")
	}
	if missionID != "" {
		if _, err := getMission(ctx, s.pool, missionID); err != nil {
			return nil, err
		}
	}
	return collect(ctx, s.pool, `SELECT id, mission_id, COALESCE(thread_id, ''), kind, message, created_at, COALESCE(finding_id,''), COALESCE(finding_revision_id,'')
		FROM events WHERE ($1 = '' OR mission_id = $1) AND id > $2 ORDER BY id LIMIT 200`,
		func(row scanner) (domain.Event, error) {
			var event domain.Event
			err := row.Scan(&event.ID, &event.MissionID, &event.ThreadID, &event.Kind, &event.Message, &event.CreatedAt, &event.FindingID, &event.FindingRevisionID)
			return event, databaseError(err)
		}, missionID, after)
}
