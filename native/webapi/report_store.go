package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/core/operation"
)

type reportModel struct {
	bun.BaseModel `bun:"table:webapi_reports,alias:webapi_report"`

	ID        string `bun:"id,pk"`
	TaskID    string `bun:"task_id,notnull"`
	SessionID string `bun:"session_id,notnull"`
	ItemsJSON string `bun:"items_json,type:text,notnull"`
	CreatedAt string `bun:"created_at,notnull"`
}

type reportDraftModel struct {
	bun.BaseModel `bun:"table:webapi_report_drafts,alias:webapi_report_draft"`

	ReportID    string `bun:"report_id,pk"`
	SessionID   string `bun:"session_id,notnull"`
	TurnID      string `bun:"turn_id,notnull"`
	CallID      string `bun:"call_id,notnull"`
	PayloadJSON string `bun:"payload_json,type:text,notnull"`
	CreatedAt   string `bun:"created_at,notnull"`
}

// ReportItem 是报告固定的一项：候选的一条不可变修订及可选的同修订验证。
type ReportItem struct {
	FindingID       string   `json:"finding_id"`
	RevisionID      string   `json:"revision_id"`
	VerificationIDs []string `json:"verification_ids"`
}

type ReportSummary struct {
	ReportID  string       `json:"report_id"`
	TaskID    string       `json:"task_id"`
	SessionID string       `json:"session_id"`
	CreatedAt string       `json:"created_at"`
	Items     []ReportItem `json:"items"`
	HasDraft  bool         `json:"has_draft"`
	Title     string       `json:"title"` // 草稿标题；无草稿为空串。
}

type ReportVerification struct {
	VerificationSummary
	Result *VerificationResultView `json:"result"`
}

type ReportItemDetail struct {
	ReportItem
	Revision      *FindingDetail       `json:"revision"`
	Verifications []ReportVerification `json:"verifications"`
}

type ReportDetail struct {
	ReportID  string             `json:"report_id"`
	TaskID    string             `json:"task_id"`
	SessionID string             `json:"session_id"`
	CreatedAt string             `json:"created_at"`
	HasDraft  bool               `json:"has_draft"`
	Items     []ReportItemDetail `json:"items"`
	Draft     *ReportDraftView   `json:"draft"`
}

type ReportDraftView struct {
	SessionID string             `json:"session_id"`
	TurnID    string             `json:"turn_id"`
	CallID    string             `json:"call_id"`
	Payload   reportDraftPayload `json:"payload"`
	CreatedAt string             `json:"created_at"`
}

type storedReportDraft struct {
	Canonical string             `json:"canonical"`
	Payload   reportDraftPayload `json:"payload"`
}

var (
	errReportNotFound = errors.New("任务内候选、修订或验证不属于本报告")
	errReportConflict = errors.New("报告ID已对应不同条目，拒绝覆盖")
	errReportStale    = errors.New("新报告只能固定候选当前revision；请重读后保存")
	errReportDraft    = errors.New("报告草稿不可变或当前Invocation不允许覆盖")
)

func reportSummary(model reportModel) (ReportSummary, error) {
	var items []ReportItem
	if err := json.Unmarshal([]byte(model.ItemsJSON), &items); err != nil {
		return ReportSummary{}, err
	}
	return ReportSummary{ReportID: model.ID, TaskID: model.TaskID, SessionID: model.SessionID, CreatedAt: model.CreatedAt, Items: items}, nil
}

func (s *taskStore) reportForSession(ctx context.Context, sessionID string) (*reportModel, error) {
	var model reportModel
	err := s.orm.NewSelect().Model(&model).Where("session_id = ?", sessionID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (s *taskStore) reportByID(ctx context.Context, taskID, id string) (*reportModel, error) {
	var model reportModel
	err := s.orm.NewSelect().Model(&model).Where("task_id = ? AND id = ?", taskID, id).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// saveReport 与saveVerification同模式：首条SQL即INSERT认领，同ID回放先于当前revision检查。
func (s *taskStore) saveReport(ctx context.Context, id, taskID string, items []ReportItem) (*ReportSummary, bool, bool, error) {
	if len(id) < 1 || len(id) > 128 {
		return nil, false, false, fmt.Errorf("report id非法")
	}
	canonical, err := json.Marshal(items)
	if err != nil {
		return nil, false, false, err
	}
	model := &reportModel{ID: id, TaskID: taskID, SessionID: newSessionID(), ItemsJSON: string(canonical), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	created, conflict := false, false
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewInsert().Model(model).On("CONFLICT(id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		var saved reportModel
		if err := tx.NewSelect().Model(&saved).Where("id = ?", id).Limit(1).Scan(ctx); err != nil {
			return err
		}
		if saved.TaskID != taskID {
			return errReportNotFound
		}
		if saved.ItemsJSON != string(canonical) {
			conflict = true
			return nil
		}
		if inserted == 0 {
			*model = saved
			return nil
		}
		for _, item := range items {
			var current findingModel
			err := tx.NewSelect().Model(&current).Where("task_id = ? AND id = ?", taskID, item.FindingID).Limit(1).Scan(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				return errReportNotFound
			}
			if err != nil {
				return err
			}
			if current.CurrentRevisionID != item.RevisionID {
				return errReportStale // 回滚刚认领的过期依据。
			}
			for _, verificationID := range item.VerificationIDs {
				var verification verificationModel
				err := tx.NewSelect().Model(&verification).Where("task_id = ? AND finding_id = ? AND id = ?", taskID, item.FindingID, verificationID).Limit(1).Scan(ctx)
				if errors.Is(err, sql.ErrNoRows) {
					return errReportNotFound
				}
				if err != nil {
					return err
				}
				if verification.SourceRevisionID != item.RevisionID {
					return errReportNotFound
				}
			}
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, false, err
	}
	summary, err := reportSummary(*model)
	if err != nil {
		return nil, false, false, err
	}
	if !created && !conflict {
		if err := s.fillReportDraftSummary(ctx, &summary); err != nil {
			return nil, false, false, err
		}
	}
	return &summary, created, conflict, nil
}

// fillReportDraftSummary 只取草稿payload一次，填has_draft与title。
func (s *taskStore) fillReportDraftSummary(ctx context.Context, summary *ReportSummary) error {
	var payloadJSON string
	err := s.orm.NewSelect().Model((*reportDraftModel)(nil)).Column("payload_json").Where("report_id = ?", summary.ReportID).Limit(1).Scan(ctx, &payloadJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var stored storedReportDraft
	if err := json.Unmarshal([]byte(payloadJSON), &stored); err != nil {
		return err
	}
	summary.HasDraft, summary.Title = true, stored.Payload.Title
	return nil
}

func (s *taskStore) reportDraft(ctx context.Context, reportID string) (*ReportDraftView, error) {
	var model reportDraftModel
	err := s.orm.NewSelect().Model(&model).Where("report_id = ?", reportID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stored storedReportDraft
	if err := json.Unmarshal([]byte(model.PayloadJSON), &stored); err != nil {
		return nil, err
	}
	return &ReportDraftView{SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID, Payload: stored.Payload, CreatedAt: model.CreatedAt}, nil
}

// reportDetail 把固定items展开为修订快照与验证结果；来源均不可变，不读Flow原件。
func (s *taskStore) reportDetail(ctx context.Context, model *reportModel) (*ReportDetail, error) {
	if model == nil {
		return nil, nil
	}
	summary, err := reportSummary(*model)
	if err != nil {
		return nil, err
	}
	detail := &ReportDetail{ReportID: model.ID, TaskID: model.TaskID, SessionID: model.SessionID, CreatedAt: model.CreatedAt, Items: make([]ReportItemDetail, 0, len(summary.Items))}
	for _, item := range summary.Items {
		revision, err := s.findingRevision(ctx, model.TaskID, item.FindingID, item.RevisionID)
		if err != nil {
			return nil, err
		}
		if revision == nil {
			return nil, errors.New("报告固定候选revision不存在")
		}
		expanded := ReportItemDetail{ReportItem: item, Revision: revision, Verifications: make([]ReportVerification, 0, len(item.VerificationIDs))}
		for _, verificationID := range item.VerificationIDs {
			verification, err := s.verificationByID(ctx, model.TaskID, item.FindingID, verificationID)
			if err != nil {
				return nil, err
			}
			if verification == nil {
				return nil, errors.New("报告所附验证不存在")
			}
			result, err := s.verificationResult(ctx, verification.ID)
			if err != nil {
				return nil, err
			}
			view := ReportVerification{VerificationSummary: verificationSummary(*verification), Result: result}
			if result != nil {
				view.Verdict = result.Payload.Verdict
			}
			expanded.Verifications = append(expanded.Verifications, view)
		}
		detail.Items = append(detail.Items, expanded)
	}
	detail.Draft, err = s.reportDraft(ctx, model.ID)
	if err != nil {
		return nil, err
	}
	detail.HasDraft = detail.Draft != nil
	return detail, nil
}

func (s *taskStore) reportList(ctx context.Context, taskID string, limit int, afterCreatedAt, afterID string) ([]ReportSummary, error) {
	var models []reportModel
	query := s.orm.NewSelect().Model(&models).Where("task_id = ?", taskID)
	if afterCreatedAt != "" {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", afterCreatedAt, afterCreatedAt, afterID)
	}
	if err := query.Order("created_at DESC", "id DESC").Limit(int64(limit + 1)).Scan(ctx); err != nil {
		return nil, err
	}
	items := make([]ReportSummary, 0, len(models))
	for _, model := range models {
		summary, err := reportSummary(model)
		if err != nil {
			return nil, err
		}
		if err := s.fillReportDraftSummary(ctx, &summary); err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

func sameDraftInvocation(existing reportDraftModel, inv operation.Invocation, canonical []byte) bool {
	var stored storedReportDraft
	return json.Unmarshal([]byte(existing.PayloadJSON), &stored) == nil && existing.SessionID == inv.SessionID &&
		existing.TurnID == inv.TurnID && existing.CallID == inv.CallID && stored.Canonical == string(canonical)
}

// saveReportDraft 与saveVerificationResult同语义：草稿不可变，同Invocation同规范载荷回读。
func (s *taskStore) saveReportDraft(ctx context.Context, inv operation.Invocation, report *reportModel, canonical []byte, args reportDraftPayload) (*ReportDraftView, error) {
	payload, err := json.Marshal(storedReportDraft{Canonical: string(canonical), Payload: args})
	if err != nil {
		return nil, err
	}
	model := &reportDraftModel{ReportID: report.ID, SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: inv.CallID, PayloadJSON: string(payload), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	var existing reportDraftModel
	err = s.orm.NewSelect().Model(&existing).Where("report_id = ?", report.ID).Limit(1).Scan(ctx)
	if err == nil {
		if !sameDraftInvocation(existing, inv, canonical) {
			return nil, errReportDraft
		}
		return s.reportDraft(ctx, report.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(model).Exec(ctx)
		return err
	})
	if err != nil {
		// 回执失联只核对一次，不重新生成草稿。
		lookup := s.orm.NewSelect().Model(&existing).Where("report_id = ?", report.ID).Limit(1).Scan(ctx)
		if lookup != nil {
			return nil, errors.Join(err, lookup)
		}
		if !sameDraftInvocation(existing, inv, canonical) {
			return nil, errReportDraft
		}
		return s.reportDraft(ctx, report.ID)
	}
	return &ReportDraftView{SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID, Payload: args, CreatedAt: model.CreatedAt}, nil
}
