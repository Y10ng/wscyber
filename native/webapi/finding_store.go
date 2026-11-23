package webapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/core/operation"
)

type findingModel struct {
	bun.BaseModel `bun:"table:webapi_findings,alias:webapi_finding"`

	ID                string `bun:"id,pk"`
	TaskID            string `bun:"task_id,notnull"`
	Number            int64  `bun:"number,notnull"`
	Status            string `bun:"status,notnull"`
	CurrentRevision   int64  `bun:"current_revision,notnull"`
	CurrentRevisionID string `bun:"current_revision_id,notnull"`
	CreatedAt         string `bun:"created_at,notnull"`
	UpdatedAt         string `bun:"updated_at,notnull"`
}

type findingRevisionModel struct {
	bun.BaseModel `bun:"table:webapi_finding_revisions,alias:finding_revision"`

	ID          string `bun:"id,pk"`
	FindingID   string `bun:"finding_id,notnull"`
	TaskID      string `bun:"task_id,notnull"`
	Number      int64  `bun:"number,notnull"`
	Revision    int64  `bun:"revision,notnull"`
	SessionID   string `bun:"session_id,notnull"`
	TurnID      string `bun:"turn_id,notnull"`
	CallID      string `bun:"call_id,notnull"`
	PayloadJSON string `bun:"payload_json,type:text,notnull"`
	Title       string `bun:"title,type:text,notnull"`
	CreatedAt   string `bun:"created_at,notnull"`
}

type FindingSummary struct {
	FindingID  string `json:"finding_id"`
	Number     string `json:"number"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	Revision   int64  `json:"revision"`
	RevisionID string `json:"revision_id"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type FindingSource struct {
	SessionID string `json:"session_id"`
	TurnID    string `json:"turn_id"`
	CallID    string `json:"call_id"`
}

// FindingEvidenceSnapshot 只保存引用时点的真实元数据和原件hash，不复制Flow/body。
type FindingEvidenceSnapshot struct {
	EvidenceID         string `json:"evidence_id"`
	Relation           string `json:"relation"`
	SessionID          string `json:"session_id"`
	TurnID             string `json:"turn_id"`
	CallID             string `json:"call_id"`
	Method             string `json:"method"`
	URL                string `json:"url"`
	RequestFingerprint string `json:"request_fingerprint"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Sent               *bool  `json:"sent"`
	HTTPStatus         *int64 `json:"http_status,omitempty"`
	PreviewTruncated   bool   `json:"preview_truncated"`
	CaptureTruncated   bool   `json:"capture_truncated"`
	CreatedAt          string `json:"created_at"`
	CompletedAt        string `json:"completed_at,omitempty"`
	FlowSHA256         string `json:"flow_sha256,omitempty"`
}

type FindingRevisionSummary struct {
	RevisionID string        `json:"revision_id"`
	Revision   int64         `json:"revision"`
	Title      string        `json:"title"`
	CreatedAt  string        `json:"created_at"`
	Source     FindingSource `json:"source"`
}

type FindingDetail struct {
	FindingID         string                    `json:"finding_id"`
	TaskID            string                    `json:"task_id"`
	Number            string                    `json:"number"`
	Status            string                    `json:"status"`
	Revision          int64                     `json:"revision"`
	RevisionID        string                    `json:"revision_id"`
	RevisionCreatedAt string                    `json:"revision_created_at"`
	CreatedAt         string                    `json:"created_at,omitempty"`
	UpdatedAt         string                    `json:"updated_at,omitempty"`
	Content           findingContent            `json:"content"`
	Evidence          []FindingEvidenceSnapshot `json:"evidence"`
	Source            FindingSource             `json:"source"`
}

type storedFindingPayload struct {
	Canonical string                    `json:"canonical"`
	Content   findingContent            `json:"content"`
	Evidence  []FindingEvidenceSnapshot `json:"evidence"`
}

var (
	errFindingCallConflict = errors.New("同一原生call已保存不同候选载荷，拒绝冲突重投")
	errFindingNotFound     = errors.New("finding_id: 本任务候选不存在；更新只能引用本任务已保存的 finding_id")
	errFindingRevision     = errors.New("expected_revision已陈旧，未覆盖当前候选")
	errFindingEvidence     = errors.New("证据必须是当前任务已保存的真实HTTP记录且不得重复")
	errFindingSaveUnknown  = errors.New("候选保存结果未确认，请先按原生call核对；不自动重试")
)

func publicFindingError(err error) string {
	var input fieldInputError
	if errors.As(err, &input) {
		return input.Error()
	}
	for _, expected := range []error{errFindingCallConflict, errFindingNotFound, errFindingRevision, errFindingEvidence, errFindingSaveUnknown} {
		if errors.Is(err, expected) {
			return expected.Error()
		}
	}
	return "候选存储操作失败"
}

func findingNumber(number int64) string { return fmt.Sprintf("WSC-%06d", number) }
func newFindingID() string              { return "wf_" + newSessionID() + newSessionID() }
func newFindingRevisionID() string      { return "wfr_" + newSessionID() + newSessionID() }

func snapshotFindingEvidence(model requestModel, relation string) FindingEvidenceSnapshot {
	snapshot := FindingEvidenceSnapshot{
		EvidenceID: model.ID, Relation: relation, SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID,
		Method: model.Method, URL: model.URL, RequestFingerprint: model.Fingerprint, Status: model.Status, Reason: model.Reason,
		Sent: nullableBool(model.Sent), PreviewTruncated: model.PreviewTruncated, CaptureTruncated: model.CaptureTruncated,
		CreatedAt: model.CreatedAt, CompletedAt: model.CompletedAt.String,
	}
	if model.StatusCode.Valid {
		snapshot.HTTPStatus = &model.StatusCode.Int64
	}
	if len(model.FlowPB) > 0 {
		sum := sha256.Sum256(model.FlowPB)
		snapshot.FlowSHA256 = "sha256:" + hex.EncodeToString(sum[:])
	}
	return snapshot
}

func (s *taskStore) saveFinding(ctx context.Context, inv operation.Invocation, taskID string, args findingToolArguments, canonical []byte) (*FindingDetail, error) {
	evidence := make([]requestModel, 0, len(args.Content.Evidence))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var revision *findingRevisionModel
	var findingTimes findingModel
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// 同session/turn/call先认领：先INSERT（写事务持全局写锁串行），再按结果分支。
		claim := &findingRevisionModel{
			ID: newFindingRevisionID(), FindingID: newFindingID(), TaskID: taskID, Number: 0, Revision: 1,
			SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: inv.CallID, PayloadJSON: string(canonical), Title: args.Content.Title, CreatedAt: now,
		}
		insertResult, err := tx.NewInsert().Model(claim).On("CONFLICT(task_id,session_id,turn_id,call_id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		inserted, err := insertResult.RowsAffected()
		if err != nil {
			return err
		}
		if inserted == 0 {
			var existing findingRevisionModel
			if err := tx.NewSelect().Model(&existing).Where("task_id = ? AND session_id = ? AND turn_id = ? AND call_id = ?", taskID, inv.SessionID, inv.TurnID, inv.CallID).Limit(1).Scan(ctx); err != nil {
				return err
			}
			var saved storedFindingPayload
			if json.Unmarshal([]byte(existing.PayloadJSON), &saved) != nil {
				return errors.New("已保存候选载荷损坏")
			}
			if saved.Canonical != string(canonical) {
				return errFindingCallConflict
			}
			revision = &existing
			if err := tx.NewSelect().Model(&findingTimes).Column("created_at", "updated_at").Where("task_id = ? AND id = ?", taskID, existing.FindingID).Limit(1).Scan(ctx); err != nil {
				return err
			}
			return nil
		}
		var problems toolValidationErrors
		for index, reference := range args.Content.Evidence {
			var model requestModel
			if err := tx.NewSelect().Model(&model).Where("task_id = ? AND id = ?", taskID, reference.EvidenceID).Limit(1).Scan(ctx); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					problems.add(fieldInputError{errFindingEvidence, fmt.Sprintf("content.evidence[%d].evidence_id: 证据必须是当前任务已保存的真实HTTP记录且不得重复；只能引用本任务 webapi_http 返回的 evidence_id", index)})
					continue
				}
				return err
			}
			evidence = append(evidence, model)
		}
		if err := problems.err(); err != nil {
			return fieldInputError{errFindingEvidence, err.Error()}
		}
		snapshots := make([]FindingEvidenceSnapshot, len(evidence))
		for index := range evidence {
			snapshots[index] = snapshotFindingEvidence(evidence[index], args.Content.Evidence[index].Relation)
		}
		payload, err := json.Marshal(storedFindingPayload{Canonical: string(canonical), Content: args.Content, Evidence: snapshots})
		if err != nil {
			return err
		}
		claim.PayloadJSON = string(payload)
		created := args.FindingID == ""
		if created {
			var maximum int64
			if err := tx.NewSelect().Model((*findingModel)(nil)).ColumnExpr("COALESCE(MAX(number), 0)").Scan(ctx, &maximum); err != nil {
				return err
			}
			claim.Number = maximum + 1
			finding := &findingModel{ID: claim.FindingID, TaskID: taskID, Number: claim.Number, Status: "candidate", CurrentRevision: 1, CurrentRevisionID: claim.ID, CreatedAt: now, UpdatedAt: now}
			if _, err := tx.NewInsert().Model(finding).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewUpdate().Model(claim).Column("number", "payload_json").Where("id = ?", claim.ID).Exec(ctx); err != nil {
				return err
			}
			revision = claim
			findingTimes = *finding
			return nil
		}

		var current findingModel
		if err := tx.NewSelect().Model(&current).Where("task_id = ? AND id = ?", taskID, args.FindingID).Limit(1).Scan(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errFindingNotFound
			}
			return err
		}
		if current.CurrentRevision != args.ExpectedRevision {
			return errFindingRevision
		}
		claim.FindingID, claim.Number, claim.Revision = current.ID, current.Number, current.CurrentRevision+1
		if _, err := tx.NewUpdate().Model(claim).Column("finding_id", "number", "revision", "payload_json").Where("id = ?", claim.ID).Exec(ctx); err != nil {
			return err
		}
		result, err := tx.NewUpdate().Model((*findingModel)(nil)).Set("current_revision = ?, current_revision_id = ?, updated_at = ?", claim.Revision, claim.ID, now).
			Where("id = ? AND task_id = ? AND current_revision = ?", current.ID, taskID, args.ExpectedRevision).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			return errFindingRevision
		}
		revision = claim
		current.UpdatedAt = now
		findingTimes = current
		return nil
	})
	if err != nil {
		if errors.Is(err, errFindingCallConflict) || errors.Is(err, errFindingNotFound) || errors.Is(err, errFindingRevision) || errors.Is(err, errFindingEvidence) {
			return nil, err
		}
		// 事务结果未知时只做一次有界回执核对，不自动重试或另造编号。
		lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		var existing findingRevisionModel
		lookup := s.read(lookupCtx, func(ctx context.Context, db bun.IDB) error {
			return db.NewSelect().Model(&existing).Where("task_id = ? AND session_id = ? AND turn_id = ? AND call_id = ?", taskID, inv.SessionID, inv.TurnID, inv.CallID).Limit(1).Scan(ctx)
		})
		if lookup == nil {
			var saved storedFindingPayload
			if json.Unmarshal([]byte(existing.PayloadJSON), &saved) == nil && saved.Canonical == string(canonical) {
				detail, detailErr := detailFromRevision(existing)
				if detailErr != nil {
					return nil, detailErr
				}
				var current findingModel
				if getErr := s.read(lookupCtx, func(ctx context.Context, db bun.IDB) error {
					return db.NewSelect().Model(&current).Column("created_at", "updated_at").Where("task_id = ? AND id = ?", taskID, existing.FindingID).Limit(1).Scan(ctx)
				}); getErr == nil {
					detail.CreatedAt, detail.UpdatedAt = current.CreatedAt, current.UpdatedAt
				}
				return detail, nil
			}
			return nil, errFindingCallConflict
		}
		return nil, errFindingSaveUnknown
	}
	detail, err := detailFromRevision(*revision)
	if err != nil {
		return nil, err
	}
	detail.CreatedAt, detail.UpdatedAt = findingTimes.CreatedAt, findingTimes.UpdatedAt
	return detail, nil
}

func (s *taskStore) findingRevision(ctx context.Context, taskID, findingID, revisionID string) (*FindingDetail, error) {
	var model findingRevisionModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).Where("task_id = ? AND finding_id = ? AND id = ?", taskID, findingID, revisionID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return detailFromRevision(model)
}

func detailFromRevision(model findingRevisionModel) (*FindingDetail, error) {
	var payload storedFindingPayload
	if err := json.Unmarshal([]byte(model.PayloadJSON), &payload); err != nil {
		return nil, err
	}
	return &FindingDetail{
		FindingID: model.FindingID, TaskID: model.TaskID, Number: findingNumber(model.Number), Status: "candidate", Revision: model.Revision, RevisionID: model.ID, RevisionCreatedAt: model.CreatedAt,
		Content: payload.Content, Evidence: payload.Evidence,
		Source: FindingSource{SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID},
	}, nil
}

func (s *taskStore) findingCurrent(ctx context.Context, taskID, findingID string) (*FindingDetail, error) {
	var finding findingModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&finding).Where("task_id = ? AND id = ?", taskID, findingID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	detail, err := s.findingRevision(ctx, taskID, findingID, finding.CurrentRevisionID)
	if detail != nil {
		detail.CreatedAt, detail.UpdatedAt = finding.CreatedAt, finding.UpdatedAt
	}
	return detail, err
}

func (s *taskStore) findingExists(ctx context.Context, taskID, findingID string) (bool, error) {
	var id string
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model((*findingModel)(nil)).Column("id").Where("task_id = ? AND id = ?", taskID, findingID).Limit(1).Scan(ctx, &id)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

type findingListRow struct {
	findingModel `bun:",extend"`
	Title        string `bun:"title"`
}

func (s *taskStore) findingList(ctx context.Context, taskID string, limit int, afterNumber int64) ([]FindingSummary, error) {
	var rows []findingListRow
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		query := db.NewSelect().Model(&rows).
			ColumnExpr("webapi_finding.id, webapi_finding.task_id, webapi_finding.number, webapi_finding.status, webapi_finding.current_revision, webapi_finding.current_revision_id, webapi_finding.created_at, webapi_finding.updated_at, revision.title AS title").
			Join("JOIN webapi_finding_revisions AS revision ON revision.id = webapi_finding.current_revision_id").
			Where("webapi_finding.task_id = ?", taskID)
		if afterNumber > 0 {
			query = query.Where("webapi_finding.number < ?", afterNumber)
		}
		return query.Order("webapi_finding.number DESC").Limit(int64(limit + 1)).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	items := make([]FindingSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, FindingSummary{FindingID: row.ID, Number: findingNumber(row.Number), Title: row.Title, Status: row.Status, Revision: row.CurrentRevision, RevisionID: row.CurrentRevisionID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
	}
	return items, nil
}

func (s *taskStore) findingHistory(ctx context.Context, taskID, findingID string, limit int, afterRevision int64) ([]FindingRevisionSummary, error) {
	var rows []findingRevisionModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		query := db.NewSelect().Model(&rows).Column("id", "finding_id", "task_id", "number", "revision", "session_id", "turn_id", "call_id", "title", "created_at").
			Where("task_id = ? AND finding_id = ?", taskID, findingID)
		if afterRevision > 0 {
			query = query.Where("revision < ?", afterRevision)
		}
		return query.Order("revision DESC").Limit(int64(limit + 1)).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	items := make([]FindingRevisionSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, FindingRevisionSummary{RevisionID: row.ID, Revision: row.Revision, Title: row.Title, CreatedAt: row.CreatedAt, Source: FindingSource{SessionID: row.SessionID, TurnID: row.TurnID, CallID: row.CallID}})
	}
	return items, nil
}
