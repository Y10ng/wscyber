package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/core/operation"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

type verificationModel struct {
	bun.BaseModel `bun:"table:webapi_verifications,alias:webapi_verification"`

	ID               string `bun:"id,pk"`
	TaskID           string `bun:"task_id,notnull"`
	FindingID        string `bun:"finding_id,notnull"`
	SourceRevisionID string `bun:"source_revision_id,notnull"`
	SessionID        string `bun:"session_id,notnull"`
	CreatedAt        string `bun:"created_at,notnull"`
}

type verificationResultModel struct {
	bun.BaseModel `bun:"table:webapi_verification_results,alias:webapi_verification_result"`

	VerificationID string `bun:"verification_id,pk"`
	SessionID      string `bun:"session_id,notnull"`
	TurnID         string `bun:"turn_id,notnull"`
	CallID         string `bun:"call_id,notnull"`
	PayloadJSON    string `bun:"payload_json,type:text,notnull"`
	CreatedAt      string `bun:"created_at,notnull"`
}

type VerificationSummary struct {
	VerificationID   string `json:"verification_id"`
	TaskID           string `json:"task_id"`
	FindingID        string `json:"finding_id"`
	SourceRevisionID string `json:"source_revision_id"`
	SessionID        string `json:"session_id"`
	CreatedAt        string `json:"created_at"`
	Verdict          string `json:"verdict,omitempty"`
}

type VerificationDetail struct {
	VerificationSummary
	Source *FindingDetail          `json:"source"`
	Result *VerificationResultView `json:"result"`
}

type VerificationResultView struct {
	SessionID string                    `json:"session_id"`
	TurnID    string                    `json:"turn_id"`
	CallID    string                    `json:"call_id"`
	Payload   verificationToolPayload   `json:"payload"`
	Evidence  []FindingEvidenceSnapshot `json:"evidence"`
	CreatedAt string                    `json:"created_at"`
}

type storedVerificationResult struct {
	Canonical string                    `json:"canonical"`
	Payload   verificationToolPayload   `json:"payload"`
	Evidence  []FindingEvidenceSnapshot `json:"evidence"`
}

var (
	errVerificationConflict = errors.New("验证ID已对应不同来源，拒绝覆盖")
	errVerificationStale    = errors.New("只能显式验证候选当前revision；请重读后发起")
	errVerificationResult   = errors.New("验证结果不可变或当前Invocation不允许覆盖")
)

func verificationSummary(model verificationModel) VerificationSummary {
	return VerificationSummary{VerificationID: model.ID, TaskID: model.TaskID, FindingID: model.FindingID, SourceRevisionID: model.SourceRevisionID, SessionID: model.SessionID, CreatedAt: model.CreatedAt}
}

// findOptional 按条件读取一行固定记录；不存在返回(nil, nil)。验证与复测共用。
func findOptional[M any](ctx context.Context, db bun.IDB, where string, args ...any) (*M, error) {
	model := new(M)
	err := db.NewSelect().Model(model).Where(where, args...).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model, nil
}

func (s *taskStore) verificationForSession(ctx context.Context, sessionID string) (*verificationModel, error) {
	return findOptional[verificationModel](ctx, s.orm, "session_id = ?", sessionID)
}

// pinnedSource 是验证/复测记录固定的候选来源。
type pinnedSource struct{ TaskID, FindingID, SourceRevisionID string }

// claimPinnedRevision 是验证与复测共用的PUT认领：事务首条SQL即INSERT认领，
// 同ID回放先于当前revision检查；异来源返回conflict，新建过期revision回滚并返回stale。
func (s *taskStore) claimPinnedRevision(ctx context.Context, want pinnedSource, insert func(context.Context, bun.Tx) (bool, error), load func(context.Context, bun.Tx) (pinnedSource, error), stale error) (created, conflict bool, err error) {
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		inserted, err := insert(ctx, tx)
		if err != nil {
			return err
		}
		saved, err := load(ctx, tx)
		if err != nil {
			return err
		}
		if saved.TaskID != want.TaskID || saved.FindingID != want.FindingID {
			return errFindingNotFound
		}
		if saved.SourceRevisionID != want.SourceRevisionID {
			conflict = true
			return nil
		}
		if !inserted {
			return nil // 同ID回放先于当前revision检查。
		}
		var current findingModel
		err = tx.NewSelect().Model(&current).Where("task_id = ? AND id = ?", want.TaskID, want.FindingID).Limit(1).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return errFindingNotFound
		}
		if err != nil {
			return err
		}
		if current.CurrentRevisionID != want.SourceRevisionID {
			return stale // 回滚刚认领的过期依据。
		}
		created = true
		return nil
	})
	return created, conflict, err
}

func (s *taskStore) saveVerification(ctx context.Context, id, taskID, findingID, sourceRevisionID string) (*VerificationSummary, bool, bool, error) {
	if len(id) < 1 || len(id) > 128 {
		return nil, false, false, fmt.Errorf("verification id非法")
	}
	model := &verificationModel{ID: id, TaskID: taskID, FindingID: findingID, SourceRevisionID: sourceRevisionID, SessionID: newSessionID(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	created, conflict, err := s.claimPinnedRevision(ctx, pinnedSource{taskID, findingID, sourceRevisionID},
		func(ctx context.Context, tx bun.Tx) (bool, error) {
			result, err := tx.NewInsert().Model(model).On("CONFLICT(id) DO NOTHING").Exec(ctx)
			if err != nil {
				return false, err
			}
			inserted, err := result.RowsAffected()
			return inserted > 0, err
		},
		func(ctx context.Context, tx bun.Tx) (pinnedSource, error) {
			var saved verificationModel
			if err := tx.NewSelect().Model(&saved).Where("id = ?", id).Limit(1).Scan(ctx); err != nil {
				return pinnedSource{}, err
			}
			if saved.TaskID == taskID && saved.FindingID == findingID && saved.SourceRevisionID == sourceRevisionID {
				*model = saved
			}
			return pinnedSource{saved.TaskID, saved.FindingID, saved.SourceRevisionID}, nil
		}, errVerificationStale)
	if err != nil {
		return nil, false, false, err
	}
	summary := verificationSummary(*model)
	return &summary, created, conflict, nil
}

func (s *taskStore) verificationByID(ctx context.Context, taskID, findingID, id string) (*verificationModel, error) {
	return findOptional[verificationModel](ctx, s.orm, "task_id = ? AND finding_id = ? AND id = ?", taskID, findingID, id)
}

func (s *taskStore) verificationDetail(ctx context.Context, model *verificationModel) (*VerificationDetail, error) {
	if model == nil {
		return nil, nil
	}
	source, err := s.findingRevision(ctx, model.TaskID, model.FindingID, model.SourceRevisionID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errors.New("验证固定候选revision不存在")
	}
	result, err := s.verificationResult(ctx, model.ID)
	if err != nil {
		return nil, err
	}
	return &VerificationDetail{VerificationSummary: verificationSummary(*model), Source: source, Result: result}, nil
}

func (s *taskStore) verificationResult(ctx context.Context, id string) (*VerificationResultView, error) {
	model, err := findOptional[verificationResultModel](ctx, s.orm, "verification_id = ?", id)
	if model == nil || err != nil {
		return nil, err
	}
	var stored storedVerificationResult
	if err := json.Unmarshal([]byte(model.PayloadJSON), &stored); err != nil {
		return nil, err
	}
	return &VerificationResultView{SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID, Payload: stored.Payload, Evidence: stored.Evidence, CreatedAt: model.CreatedAt}, nil
}

// pinnedPage 按finding内created_at/id倒序分页读取验证或复测固定记录（多读一行判断has_more）。
func pinnedPage[M any](ctx context.Context, db bun.IDB, taskID, findingID string, limit int, afterCreatedAt, afterID string) ([]M, error) {
	var models []M
	query := db.NewSelect().Model(&models).Where("task_id = ? AND finding_id = ?", taskID, findingID)
	if afterCreatedAt != "" {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", afterCreatedAt, afterCreatedAt, afterID)
	}
	if err := query.Order("created_at DESC", "id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return nil, err
	}
	return models, nil
}

func (s *taskStore) verificationList(ctx context.Context, taskID, findingID string, limit int, afterCreatedAt, afterID string) ([]VerificationSummary, error) {
	models, err := pinnedPage[verificationModel](ctx, s.orm, taskID, findingID, limit, afterCreatedAt, afterID)
	if err != nil {
		return nil, err
	}
	items := make([]VerificationSummary, 0, len(models))
	for _, model := range models {
		summary := verificationSummary(model)
		result, err := s.verificationResult(ctx, model.ID)
		if err != nil {
			return nil, err
		}
		if result != nil {
			summary.Verdict = result.Payload.Verdict
		}
		items = append(items, summary)
	}
	return items, nil
}

// resultReceipt 是不可变结果行中用于回读核对的归属与规范载荷。
type resultReceipt struct {
	SessionID, TurnID, CallID, PayloadJSON string
}

func (r *resultReceipt) matches(inv operation.Invocation, canonical []byte) bool {
	var stored struct {
		Canonical string `json:"canonical"`
	}
	return json.Unmarshal([]byte(r.PayloadJSON), &stored) == nil && r.SessionID == inv.SessionID && r.TurnID == inv.TurnID && r.CallID == inv.CallID && stored.Canonical == string(canonical)
}

// saveImmutableResult 是验证与复测结果共用的不可变保存：先按不可变记录ID读取，
// 再检查Invocation，避免另一调用抢写后事务未知时误回放；同Invocation同规范载荷回读(replayed=true)，
// 其余返回immutable。load不存在时返回(nil, nil)。
func (s *taskStore) saveImmutableResult(ctx context.Context, inv operation.Invocation, canonical []byte, load func(context.Context) (*resultReceipt, error), insert func(context.Context, bun.Tx) error, immutable error) (bool, error) {
	existing, err := load(ctx)
	if err != nil {
		return false, err
	}
	if existing != nil {
		if !existing.matches(inv, canonical) {
			return false, immutable
		}
		return true, nil
	}
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error { return insert(ctx, tx) })
	if err != nil {
		// 回执失联只核对一次，不重新生成结果或重发任何HTTP。
		existing, lookup := load(ctx)
		if lookup != nil {
			return false, errors.Join(err, lookup)
		}
		if existing == nil {
			return false, errors.Join(err, sql.ErrNoRows)
		}
		if !existing.matches(inv, canonical) {
			return false, immutable
		}
		return true, nil
	}
	return false, nil
}

func (s *taskStore) saveVerificationResult(ctx context.Context, inv operation.Invocation, verification *verificationModel, canonical []byte, args verificationToolPayload, evidence []requestModel) (*VerificationResultView, error) {
	snapshots := snapshotResultEvidence(evidence, args.Evidence)
	payload, err := json.Marshal(storedVerificationResult{Canonical: string(canonical), Payload: args, Evidence: snapshots})
	if err != nil {
		return nil, err
	}
	model := &verificationResultModel{VerificationID: verification.ID, SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: inv.CallID, PayloadJSON: string(payload), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	replayed, err := s.saveImmutableResult(ctx, inv, canonical,
		func(ctx context.Context) (*resultReceipt, error) {
			existing, err := findOptional[verificationResultModel](ctx, s.orm, "verification_id = ?", verification.ID)
			if existing == nil || err != nil {
				return nil, err
			}
			return &resultReceipt{existing.SessionID, existing.TurnID, existing.CallID, existing.PayloadJSON}, nil
		},
		func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.NewInsert().Model(model).Exec(ctx)
			return err
		}, errVerificationResult)
	if err != nil {
		return nil, err
	}
	if replayed {
		return s.verificationResult(ctx, verification.ID)
	}
	return &VerificationResultView{SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID, Payload: args, Evidence: snapshots, CreatedAt: model.CreatedAt}, nil
}

func snapshotResultEvidence(evidence []requestModel, refs []verificationEvidenceInput) []FindingEvidenceSnapshot {
	snapshots := make([]FindingEvidenceSnapshot, len(evidence))
	for index := range evidence {
		snapshots[index] = snapshotFindingEvidence(evidence[index], refs[index].Relation)
	}
	return snapshots
}

// webapiTableOrder 与SchemaModule().Models顺序一致；已知历史阶段都是其前缀。
var webapiTableOrder = []string{"webapi_tasks", "webapi_requests", "webapi_findings", "webapi_finding_revisions", "webapi_verifications", "webapi_verification_results", "webapi_reports", "webapi_report_drafts", "webapi_retests", "webapi_retest_results", "webapi_task_policies", "webapi_task_policy_events", "webapi_assets", "webapi_asset_credentials", "webapi_asset_tasks", "webapi_asset_schedules", "webapi_schedule_runs", "webapi_schedule_options", "webapi_schedule_run_followups", "webapi_schedule_followups", "webapi_asset_vulns", "webapi_asset_vuln_members", "webapi_egress_flows", "webapi_notify_settings", "webapi_notifications", "webapi_audit_events", "webapi_vuln_triage", "webapi_vuln_sla"}

// webapiSchemaStages 是已知旧结构（模型数、索引数）：旧四表、含验证六表、含报告八表、含复测十表、含测试策略十二表、含资产十五表、含周期计划十七表、含计划后续二十表、含资产漏洞库二十二表（C3）、含全工具出口二十三表（B2/U2a/D1）、含企业微信推送二十五表（D2/U2b/U2c）、含审计二十六表（B3/D2 前端）。
var webapiSchemaStages = []struct{ models, indexes int }{{4, 6}, {6, 8}, {8, 10}, {10, 12}, {12, 13}, {15, 15}, {17, 16}, {20, 17}, {22, 20}, {23, 22}, {25, 25}, {26, 29}}

func webapiSchemaStage(models, indexes int) webservice.SchemaModule {
	module := SchemaModule()
	for _, name := range webapiTableOrder[models:] {
		delete(module.Tables, name)
	}
	module.Models = module.Models[:models]
	module.Indexes = module.Indexes[:indexes]
	return module
}

// legacyWebAPISchema 是验证与报告之前的原生core+四个业务表。
func legacyWebAPISchema() webservice.SchemaModule { return webapiSchemaStage(4, 6) }

// PrepareWebAPISchema 只支持已知旧阶段补表到完整结构，单事务、不DROP/ALTER。
// 空库不操作，交原生创建完整schema；未知或半成结构由原生的严格校验拒绝。
func PrepareWebAPISchema(database string) error {
	info, err := os.Stat(database)
	if errors.Is(err, os.ErrNotExist) || err == nil && info.Size() == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	complete, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err == nil {
		return complete.Close()
	}
	failures := []error{errors.New("现有库既不是完整新结构也不是已知旧结构"), err}
	for _, stage := range webapiSchemaStages {
		known, stageErr := webservice.NewSQLiteStore(database, webapiSchemaStage(stage.models, stage.indexes))
		if stageErr != nil {
			failures = append(failures, stageErr)
			continue
		}
		if closeErr := known.Close(); closeErr != nil {
			return closeErr
		}
		return addWebAPISchemaStage(database, stage.models, stage.indexes)
	}
	return errors.Join(failures...)
}

func addWebAPISchemaStage(database string, models, indexes int) error {
	store, err := openTaskStore(database)
	if err != nil {
		return err
	}
	defer store.Close()
	full := SchemaModule()
	return store.orm.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
		for _, model := range full.Models[models:] {
			if _, err := tx.NewCreateTable().Model(model).WithForeignKeys().Exec(ctx); err != nil {
				return err
			}
		}
		for _, index := range full.Indexes[indexes:] {
			query := tx.NewCreateIndex().Model(index.Model).Index(index.Name)
			if index.Expr != "" {
				query = query.ColumnExpr(index.Expr)
			} else {
				query = query.Column(index.Columns...)
			}
			if index.Unique {
				query = query.Unique()
			}
			if _, err := query.Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}
