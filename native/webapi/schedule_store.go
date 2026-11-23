package webapi

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// 周期计划（C2a）：每资产一个 cron 计划；到点由调度器经 LaunchAssetTask 发起。
type assetScheduleModel struct {
	bun.BaseModel `bun:"table:webapi_asset_schedules,alias:webapi_asset_schedule"`

	AssetID string `bun:"asset_id,pk"`
	Cron    string `bun:"cron,notnull"`
	Enabled bool   `bun:"enabled,notnull"`
	// NextRunAt 为 UTC scheduleTimeLayout；停用或 cron 无后续触发时为空串。
	NextRunAt string `bun:"next_run_at,notnull"`
	UpdatedAt string `bun:"updated_at,notnull"`
}

// scheduleRunModel 记录每次到期的处理结果；(asset_id, scheduled_for) 唯一，重复检查不重复记录。
type scheduleRunModel struct {
	bun.BaseModel `bun:"table:webapi_schedule_runs,alias:webapi_schedule_run"`

	ID           string `bun:"id,pk"`
	AssetID      string `bun:"asset_id,notnull"`
	ScheduledFor string `bun:"scheduled_for,notnull"`
	TaskID       string `bun:"task_id,notnull"`
	Status       string `bun:"status,notnull"`
	Error        string `bun:"error,type:text,notnull"`
	CreatedAt    string `bun:"created_at,notnull"`
}

const (
	ScheduleRunLaunched = "launched"
	ScheduleRunFailed   = "failed"
	ScheduleRunSkipped  = "skipped"
)

// scheduleTimeLayout 固定宽度 UTC（触发时间按分钟对齐），字典序即时间序。
const scheduleTimeLayout = "2006-01-02T15:04:05Z"

func formatScheduleTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(scheduleTimeLayout)
}

func parseScheduleTime(raw string) (time.Time, error) {
	return time.Parse(scheduleTimeLayout, raw)
}

func (s *taskStore) schedule(ctx context.Context, assetID string) (*assetScheduleModel, error) {
	var model assetScheduleModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).Where("asset_id = ?", assetID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// putSchedule 整份覆盖（保存即生效，计划与后续开关同一事务）；返回是否新建。
func (s *taskStore) putSchedule(ctx context.Context, model assetScheduleModel, options scheduleOptionsModel) (bool, error) {
	created := false
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&options).On("CONFLICT (asset_id) DO UPDATE").
			Set("auto_verify = EXCLUDED.auto_verify, auto_retest = EXCLUDED.auto_retest, updated_at = EXCLUDED.updated_at").Exec(ctx); err != nil {
			return err
		}
		res, err := tx.NewInsert().Model(&model).On("CONFLICT (asset_id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil || affected > 0 {
			created = err == nil
			return err
		}
		_, err = tx.NewUpdate().Model(&model).Column("cron", "enabled", "next_run_at", "updated_at").WherePK().Exec(ctx)
		return err
	})
	return created, err
}

// deleteSchedule 删除计划与其后续开关（运行记录与后续明细保留）。
func (s *taskStore) deleteSchedule(ctx context.Context, assetID string) (bool, error) {
	deleted := false
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewDelete().Model((*assetScheduleModel)(nil)).Where("asset_id = ?", assetID).Exec(ctx)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		deleted = affected > 0
		_, err = tx.NewDelete().Model((*scheduleOptionsModel)(nil)).Where("asset_id = ?", assetID).Exec(ctx)
		return err
	})
	return deleted, err
}

// dueSchedules 返回启用且 next_run_at ≤ now 的计划，最早到期在前。
func (s *taskStore) dueSchedules(ctx context.Context, now time.Time) ([]assetScheduleModel, error) {
	var models []assetScheduleModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&models).
			Where(`enabled = ? AND next_run_at <> '' AND next_run_at COLLATE "C" <= ?`, true, formatScheduleTime(now)).
			OrderExpr(`next_run_at COLLATE "C" ASC, asset_id COLLATE "C" ASC`).Scan(ctx)
	})
	return models, err
}

// recordScheduleRun 在一个事务内写运行记录（同 asset+scheduled_for 已有则不重复）并推进
// next_run_at；推进只在计划仍是本次读取时的 next_run_at 时生效，不覆盖期间人为保存的新计划。
func (s *taskStore) recordScheduleRun(ctx context.Context, run scheduleRunModel, expectedNext, newNext string) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&run).On("CONFLICT (asset_id, scheduled_for) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewUpdate().Model((*assetScheduleModel)(nil)).Set("next_run_at = ?", newNext).
			Where("asset_id = ? AND next_run_at = ?", run.AssetID, expectedNext).Exec(ctx)
		return err
	})
}

// scheduleRuns 按 scheduled_for 倒序分页；before 为空表示从最新开始。多取一条判断 has_more。
func (s *taskStore) scheduleRuns(ctx context.Context, assetID string, limit int, before string) ([]scheduleRunModel, error) {
	var models []scheduleRunModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		query := db.NewSelect().Model(&models).Where("asset_id = ?", assetID).OrderExpr(`scheduled_for COLLATE "C" DESC`).Limit(int64(limit + 1))
		if before != "" {
			query = query.Where(`scheduled_for COLLATE "C" < ?`, before)
		}
		return query.Scan(ctx)
	})
	return models, err
}

// latestLaunchedRun 返回资产最近一次已发起的计划运行（排除 exclude 这个 scheduled_for）。
func (s *taskStore) latestLaunchedRun(ctx context.Context, assetID, exclude string) (*scheduleRunModel, error) {
	var model scheduleRunModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).
			Where("asset_id = ? AND status = ? AND scheduled_for <> ?", assetID, ScheduleRunLaunched, exclude).
			OrderExpr(`scheduled_for COLLATE "C" DESC`).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// latestLaunchedRuns 返回每个资产最近一次已发起的计划运行（用于全局并发计数）。
func (s *taskStore) latestLaunchedRuns(ctx context.Context) ([]scheduleRunModel, error) {
	var models []scheduleRunModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&models).
			Where("status = ?", ScheduleRunLaunched).
			Where(`scheduled_for = (SELECT MAX(r2.scheduled_for COLLATE "C") FROM webapi_schedule_runs AS r2 WHERE r2.asset_id = webapi_schedule_run.asset_id AND r2.status = ?)`, ScheduleRunLaunched).
			OrderExpr(`asset_id COLLATE "C" ASC`).Scan(ctx)
	})
	return models, err
}

// latestRuns 返回给定资产各自最近一次运行（任意状态），供计划列表摘要。
func (s *taskStore) latestRuns(ctx context.Context) (map[string]scheduleRunModel, error) {
	var models []scheduleRunModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&models).
			Where(`scheduled_for = (SELECT MAX(r2.scheduled_for COLLATE "C") FROM webapi_schedule_runs AS r2 WHERE r2.asset_id = webapi_schedule_run.asset_id)`).
			Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]scheduleRunModel, len(models))
	for _, model := range models {
		out[model.AssetID] = model
	}
	return out, nil
}

// scheduleListRow 是计划列表行（计划 + 资产名/归档）。
type scheduleListRow struct {
	assetScheduleModel
	AssetName     string
	AssetArchived bool
	AutoVerify    bool
	AutoRetest    bool
}

func (s *taskStore) listSchedules(ctx context.Context) ([]scheduleListRow, error) {
	out := []scheduleListRow{}
	err := s.queryRows(ctx, func(rows *sql.Rows) error {
		var row scheduleListRow
		if err := rows.Scan(&row.AssetID, &row.Cron, &row.Enabled, &row.NextRunAt, &row.UpdatedAt, &row.AssetName, &row.AssetArchived, &row.AutoVerify, &row.AutoRetest); err != nil {
			return err
		}
		out = append(out, row)
		return nil
	}, `SELECT s.asset_id, s.cron, s.enabled, s.next_run_at, s.updated_at, a.name, a.archived,
		COALESCE(o.auto_verify, true), COALESCE(o.auto_retest, true)
		FROM webapi_asset_schedules s JOIN webapi_assets a ON a.id = s.asset_id
		LEFT JOIN webapi_schedule_options o ON o.asset_id = s.asset_id
		ORDER BY s.enabled DESC, CASE WHEN s.next_run_at = '' THEN 1 ELSE 0 END, s.next_run_at COLLATE "C" ASC, a.name COLLATE "C" ASC, s.asset_id COLLATE "C" ASC`)
	if err != nil {
		return nil, err
	}
	return out, nil
}
