package webapi

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// 计划后续（C2b）：计划研究结束后自动候选验证与已知漏洞复测。三张新表，不改 C2a 两表结构。

// scheduleOptionsModel 是计划的后续开关；无行（C2a 阶段保存的计划）= 两者均开启。
type scheduleOptionsModel struct {
	bun.BaseModel `bun:"table:webapi_schedule_options,alias:webapi_schedule_option"`

	AssetID    string `bun:"asset_id,pk"`
	AutoVerify bool   `bun:"auto_verify,notnull"`
	AutoRetest bool   `bun:"auto_retest,notnull"`
	UpdatedAt  string `bun:"updated_at,notnull"`
}

// scheduleRunFollowupModel 是运行级后续标记：研究结束并规划后续项时写入（开关取规划时的值）；
// 所有项都不再 pending 且已发起的会话都不在运行时写 completed_at。
type scheduleRunFollowupModel struct {
	bun.BaseModel `bun:"table:webapi_schedule_run_followups,alias:webapi_schedule_run_followup"`

	RunID       string `bun:"run_id,pk"`
	AssetID     string `bun:"asset_id,notnull"`
	TaskID      string `bun:"task_id,notnull"`
	AutoVerify  bool   `bun:"auto_verify,notnull"`
	AutoRetest  bool   `bun:"auto_retest,notnull"`
	PlannedAt   string `bun:"planned_at,notnull"`
	CompletedAt string `bun:"completed_at,notnull"`
}

// scheduleFollowupModel 是单个后续项；(run_id, kind, finding_id) 唯一。
// task_id 是候选所属任务（验证 = 本次计划任务，复测 = 原任务），target_id 是确定性验证/复测 ID。
type scheduleFollowupModel struct {
	bun.BaseModel `bun:"table:webapi_schedule_followups,alias:webapi_schedule_followup"`

	ID         string `bun:"id,pk"`
	RunID      string `bun:"run_id,notnull"`
	Kind       string `bun:"kind,notnull"`
	TaskID     string `bun:"task_id,notnull"`
	FindingID  string `bun:"finding_id,notnull"`
	RevisionID string `bun:"revision_id,notnull"`
	TargetID   string `bun:"target_id,notnull"`
	Status     string `bun:"status,notnull"`
	Error      string `bun:"error,type:text,notnull"`
	CreatedAt  string `bun:"created_at,notnull"`
	UpdatedAt  string `bun:"updated_at,notnull"`
}

const (
	FollowupVerify = "verify"
	FollowupRetest = "retest"

	FollowupPending  = "pending"
	FollowupLaunched = "launched"
	FollowupFailed   = "failed"
	FollowupSkipped  = "skipped"
)

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// scheduleOptions 返回计划后续开关；无行 = 均开启。
func (s *taskStore) scheduleOptions(ctx context.Context, assetID string) (autoVerify, autoRetest bool, err error) {
	model, err := readOptional[scheduleOptionsModel](ctx, s, "asset_id = ?", assetID)
	if err != nil || model == nil {
		return true, true, err
	}
	return model.AutoVerify, model.AutoRetest, nil
}

// followupRunRow 是待处理后续的计划运行（附运行级标记，未规划为 nil）。
type followupRunRow struct {
	Run    scheduleRunModel
	Marker *scheduleRunFollowupModel
}

// followupRuns 返回需要处理后续的计划运行（最早在前）：
//   - 已规划但未完成；
//   - 未规划且是该资产最近一次 launched 运行（被更新的计划运行取代的旧运行不再补规划，
//     也避免旧库升级后对历史运行批量补发）。
func (s *taskStore) followupRuns(ctx context.Context) ([]followupRunRow, error) {
	var runs []scheduleRunModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&runs).
			Join("LEFT JOIN webapi_schedule_run_followups AS m ON m.run_id = webapi_schedule_run.id").
			Where("webapi_schedule_run.status = ? AND webapi_schedule_run.task_id <> ''", ScheduleRunLaunched).
			Where(`(m.run_id IS NOT NULL AND m.completed_at = '') OR (m.run_id IS NULL AND webapi_schedule_run.scheduled_for = (SELECT MAX(r2.scheduled_for COLLATE "C") FROM webapi_schedule_runs AS r2 WHERE r2.asset_id = webapi_schedule_run.asset_id AND r2.status = ?))`, ScheduleRunLaunched).
			OrderExpr(`webapi_schedule_run.scheduled_for COLLATE "C" ASC, webapi_schedule_run.id COLLATE "C" ASC`).Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]followupRunRow, 0, len(runs))
	for _, run := range runs {
		marker, err := readOptional[scheduleRunFollowupModel](ctx, s, "run_id = ?", run.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, followupRunRow{Run: run, Marker: marker})
	}
	return out, nil
}

func (s *taskStore) runFollowups(ctx context.Context, runID string) ([]scheduleFollowupModel, error) {
	var items []scheduleFollowupModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&items).Where("run_id = ?", runID).
			OrderExpr(`CASE kind WHEN 'verify' THEN 0 ELSE 1 END, created_at COLLATE "C" ASC, id COLLATE "C" ASC`).Scan(ctx)
	})
	return items, err
}

// planFollowups 单事务写运行级标记与全部 pending 项；已规划（标记已存在）则不变。
func (s *taskStore) planFollowups(ctx context.Context, marker scheduleRunFollowupModel, items []scheduleFollowupModel) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewInsert().Model(&marker).On("CONFLICT (run_id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil || affected == 0 {
			return err
		}
		for index := range items {
			if _, err := tx.NewInsert().Model(&items[index]).On("CONFLICT (run_id, kind, finding_id) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

// setFollowupStatus 只从 pending 迁移，终态不再改写。
func (s *taskStore) setFollowupStatus(ctx context.Context, id, status, message string) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*scheduleFollowupModel)(nil)).Set("status = ?", status).Set("error = ?", message).Set("updated_at = ?", nowRFC3339()).
			Where("id = ? AND status = ?", id, FollowupPending).Exec(ctx)
		return err
	})
}

func (s *taskStore) completeFollowupRun(ctx context.Context, runID string) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*scheduleRunFollowupModel)(nil)).Set("completed_at = ?", nowRFC3339()).
			Where("run_id = ? AND completed_at = ''", runID).Exec(ctx)
		return err
	})
}

// followupCandidate 是待规划的候选（当前修订）。
type followupCandidate struct {
	TaskID, FindingID, RevisionID string
}

func scanCandidates(rows *sql.Rows, err error) ([]followupCandidate, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []followupCandidate{}
	for rows.Next() {
		var item followupCandidate
		if err := rows.Scan(&item.TaskID, &item.FindingID, &item.RevisionID); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// verifyCandidates：本任务还没有任何验证的候选（按编号）。
func (s *taskStore) verifyCandidates(ctx context.Context, taskID string) ([]followupCandidate, error) {
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) ([]followupCandidate, error) {
		return scanCandidates(db.QueryContext(ctx, `SELECT f.task_id, f.id, f.current_revision_id FROM webapi_findings f
			WHERE f.task_id = ? AND NOT EXISTS (SELECT 1 FROM webapi_verifications v WHERE v.finding_id = f.id)
			ORDER BY f.number ASC`, taskID))
	})
}

// otherVerificationExists：候选是否已有 exceptID 以外的验证（人工或其他来源）。
func (s *taskStore) otherVerificationExists(ctx context.Context, findingID, exceptID string) (bool, error) {
	var id string
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model((*verificationModel)(nil)).Column("id").Where("finding_id = ? AND id <> ?", findingID, exceptID).Limit(1).Scan(ctx, &id)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// followupTargetSession 返回后续项对应验证/复测的原生 session；尚未创建返回空串。
func (s *taskStore) followupTargetSession(ctx context.Context, item scheduleFollowupModel) (string, error) {
	if item.Kind == FollowupVerify {
		model, err := readOptional[verificationModel](ctx, s, "id = ?", item.TargetID)
		if model == nil || err != nil {
			return "", err
		}
		return model.SessionID, nil
	}
	model, err := readOptional[retestModel](ctx, s, "id = ?", item.TargetID)
	if model == nil || err != nil {
		return "", err
	}
	return model.SessionID, nil
}

func (s *taskStore) scheduleRun(ctx context.Context, id string) (*scheduleRunModel, error) {
	return readOptional[scheduleRunModel](ctx, s, "id = ?", id)
}

// followupCounts 返回运行各 kind 各状态的计数：counts[kind][status]。
func (s *taskStore) followupCounts(ctx context.Context, runID string) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	err := s.queryRows(ctx, func(rows *sql.Rows) error {
		var kind, status string
		var count int
		if err := rows.Scan(&kind, &status, &count); err != nil {
			return err
		}
		if out[kind] == nil {
			out[kind] = map[string]int{}
		}
		out[kind][status] = count
		return nil
	}, `SELECT kind, status, COUNT(*) FROM webapi_schedule_followups WHERE run_id = ? GROUP BY kind, status`, runID)
	if err != nil {
		return nil, err
	}
	return out, nil
}
