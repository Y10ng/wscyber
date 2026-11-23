package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/core/operation"
)

// retestModel 与verificationModel同列同语义：固定同任务候选的一条不可变修订，
// 生成独立原生session但不启动。记录与验证分开，不覆盖原候选、修订或验证。
type retestModel struct {
	bun.BaseModel `bun:"table:webapi_retests,alias:webapi_retest"`

	ID               string `bun:"id,pk"`
	TaskID           string `bun:"task_id,notnull"`
	FindingID        string `bun:"finding_id,notnull"`
	SourceRevisionID string `bun:"source_revision_id,notnull"`
	SessionID        string `bun:"session_id,notnull"`
	CreatedAt        string `bun:"created_at,notnull"`
}

type retestResultModel struct {
	bun.BaseModel `bun:"table:webapi_retest_results,alias:webapi_retest_result"`

	RetestID    string `bun:"retest_id,pk"`
	SessionID   string `bun:"session_id,notnull"`
	TurnID      string `bun:"turn_id,notnull"`
	CallID      string `bun:"call_id,notnull"`
	PayloadJSON string `bun:"payload_json,type:text,notnull"`
	CreatedAt   string `bun:"created_at,notnull"`
}

type RetestSummary struct {
	RetestID         string `json:"retest_id"`
	TaskID           string `json:"task_id"`
	FindingID        string `json:"finding_id"`
	SourceRevisionID string `json:"source_revision_id"`
	SessionID        string `json:"session_id"`
	CreatedAt        string `json:"created_at"`
	Conclusion       string `json:"conclusion,omitempty"`
}

type RetestDetail struct {
	RetestSummary
	Source *FindingDetail    `json:"source"`
	Result *RetestResultView `json:"result"`
}

type RetestResultView struct {
	SessionID string                    `json:"session_id"`
	TurnID    string                    `json:"turn_id"`
	CallID    string                    `json:"call_id"`
	Payload   retestToolPayload         `json:"payload"`
	Evidence  []FindingEvidenceSnapshot `json:"evidence"`
	CreatedAt string                    `json:"created_at"`
}

type storedRetestResult struct {
	Canonical string                    `json:"canonical"`
	Payload   retestToolPayload         `json:"payload"`
	Evidence  []FindingEvidenceSnapshot `json:"evidence"`
}

var (
	errRetestConflict = errors.New("复测ID已对应不同来源，拒绝覆盖")
	errRetestStale    = errors.New("只能显式复测候选当前revision；请重读后发起")
	errRetestResult   = errors.New("复测结论不可变或当前Invocation不允许覆盖")
)

func retestSummary(model retestModel) RetestSummary {
	return RetestSummary{RetestID: model.ID, TaskID: model.TaskID, FindingID: model.FindingID, SourceRevisionID: model.SourceRevisionID, SessionID: model.SessionID, CreatedAt: model.CreatedAt}
}

func (s *taskStore) retestForSession(ctx context.Context, sessionID string) (*retestModel, error) {
	return readOptional[retestModel](ctx, s, "session_id = ?", sessionID)
}

func (s *taskStore) retestByID(ctx context.Context, taskID, findingID, id string) (*retestModel, error) {
	return readOptional[retestModel](ctx, s, "task_id = ? AND finding_id = ? AND id = ?", taskID, findingID, id)
}

// saveRetest 与saveVerification同一认领语义（claimPinnedRevision）。
func (s *taskStore) saveRetest(ctx context.Context, id, taskID, findingID, sourceRevisionID string) (*RetestSummary, bool, bool, error) {
	if len(id) < 1 || len(id) > 128 {
		return nil, false, false, fmt.Errorf("retest id非法")
	}
	model := &retestModel{ID: id, TaskID: taskID, FindingID: findingID, SourceRevisionID: sourceRevisionID, SessionID: newSessionID(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
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
			var saved retestModel
			if err := tx.NewSelect().Model(&saved).Where("id = ?", id).Limit(1).Scan(ctx); err != nil {
				return pinnedSource{}, err
			}
			if saved.TaskID == taskID && saved.FindingID == findingID && saved.SourceRevisionID == sourceRevisionID {
				*model = saved
			}
			return pinnedSource{saved.TaskID, saved.FindingID, saved.SourceRevisionID}, nil
		}, errRetestStale)
	if err != nil {
		return nil, false, false, err
	}
	summary := retestSummary(*model)
	return &summary, created, conflict, nil
}

func (s *taskStore) retestDetail(ctx context.Context, model *retestModel) (*RetestDetail, error) {
	if model == nil {
		return nil, nil
	}
	source, err := s.findingRevision(ctx, model.TaskID, model.FindingID, model.SourceRevisionID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errors.New("复测固定候选revision不存在")
	}
	result, err := s.retestResult(ctx, model.ID)
	if err != nil {
		return nil, err
	}
	return &RetestDetail{RetestSummary: retestSummary(*model), Source: source, Result: result}, nil
}

func (s *taskStore) retestResult(ctx context.Context, id string) (*RetestResultView, error) {
	model, err := readOptional[retestResultModel](ctx, s, "retest_id = ?", id)
	if model == nil || err != nil {
		return nil, err
	}
	var stored storedRetestResult
	if err := json.Unmarshal([]byte(model.PayloadJSON), &stored); err != nil {
		return nil, err
	}
	return &RetestResultView{SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID, Payload: stored.Payload, Evidence: stored.Evidence, CreatedAt: model.CreatedAt}, nil
}

func (s *taskStore) retestList(ctx context.Context, taskID, findingID string, limit int, afterCreatedAt, afterID string) ([]RetestSummary, error) {
	models, err := readValue(ctx, s, func(ctx context.Context, db bun.IDB) ([]retestModel, error) {
		return pinnedPage[retestModel](ctx, db, taskID, findingID, limit, afterCreatedAt, afterID)
	})
	if err != nil {
		return nil, err
	}
	items := make([]RetestSummary, 0, len(models))
	for _, model := range models {
		summary := retestSummary(model)
		result, err := s.retestResult(ctx, model.ID)
		if err != nil {
			return nil, err
		}
		if result != nil {
			summary.Conclusion = result.Payload.Conclusion
		}
		items = append(items, summary)
	}
	return items, nil
}

// saveRetestResult 与验证结果同一不可变语义（saveImmutableResult）。
func (s *taskStore) saveRetestResult(ctx context.Context, inv operation.Invocation, retest *retestModel, canonical []byte, args retestToolPayload, evidence []requestModel) (*RetestResultView, error) {
	snapshots := snapshotResultEvidence(evidence, retestEvidenceRefs(args.Evidence))
	payload, err := json.Marshal(storedRetestResult{Canonical: string(canonical), Payload: args, Evidence: snapshots})
	if err != nil {
		return nil, err
	}
	model := &retestResultModel{RetestID: retest.ID, SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: inv.CallID, PayloadJSON: string(payload), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	replayed, err := s.saveImmutableResult(ctx, inv, canonical,
		func(ctx context.Context) (*resultReceipt, error) {
			existing, err := readOptional[retestResultModel](ctx, s, "retest_id = ?", retest.ID)
			if existing == nil || err != nil {
				return nil, err
			}
			return &resultReceipt{existing.SessionID, existing.TurnID, existing.CallID, existing.PayloadJSON}, nil
		},
		func(ctx context.Context, tx bun.Tx) error {
			_, err := tx.NewInsert().Model(model).Exec(ctx)
			return err
		}, errRetestResult)
	if err != nil {
		return nil, err
	}
	if replayed {
		return s.retestResult(ctx, retest.ID)
	}
	return &RetestResultView{SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID, Payload: args, Evidence: snapshots, CreatedAt: model.CreatedAt}, nil
}
