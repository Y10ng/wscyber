package webapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"google.golang.org/protobuf/proto"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
)

const (
	requestCompleted     = "completed"
	requestUnknown       = "unknown"
	requestCaptureFailed = "capture_failed"
)

type requestSpec struct {
	method, url, fingerprint string
	body                     []byte
	flow                     *traffic.Flow
}

// unsentRequestStatus 是发送前即终止的状态：sent=false，记录即完成。
func unsentRequestStatus(status string) bool {
	switch status {
	case "denied", "review_required", requestReviewDenied, requestReviewExpired, requestReviewCanceled:
		return true
	}
	return false
}

func newRequestID() string { return "we_" + newSessionID() + newSessionID() }

func (s *taskStore) taskForSession(ctx context.Context, sessionID string) (*Task, error) {
	var model taskModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).Where("initial_session_id = ?", sessionID).Limit(1).Scan(ctx)
	})
	if err == nil {
		return model.toTask()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	verification, verifyErr := s.verificationForSession(ctx, sessionID)
	if verifyErr != nil {
		return nil, verifyErr
	}
	if verification != nil {
		return s.Get(ctx, verification.TaskID)
	}
	// 复测session同验证：只映射到父任务，授权仍取父任务origins，unknown保护按父任务指纹生效。
	retest, retestErr := s.retestForSession(ctx, sessionID)
	if retestErr != nil {
		return nil, retestErr
	}
	if retest != nil {
		return s.Get(ctx, retest.TaskID)
	}
	// 子 Agent 会话（M5a）：映射到所属任务，授权/策略/审批/unknown 保护都按该任务，存证 session_id 为子会话。
	sub, subErr := s.subsessionFor(ctx, sessionID)
	if subErr != nil || sub == nil {
		return nil, subErr
	}
	return s.Get(ctx, sub.TaskID)
}

// claim 在持全局写锁的写事务里先 INSERT、再回读同call并检查同指纹未决项。
// 并发新call只能串行通过此判断，不依赖进程mutex或整任务锁。
func (s *taskStore) claim(ctx context.Context, inv operation.Invocation, taskID string, spec requestSpec, status, reason string) (*requestModel, bool, error) {
	authorized, err := s.taskForSession(ctx, inv.SessionID)
	if err != nil {
		return nil, false, err
	}
	if authorized == nil || authorized.ID != taskID {
		return nil, false, errUnknownSession
	}
	return s.claimRequest(ctx, inv, taskID, spec, status, reason)
}

// claimRequest 是 claim 去掉 session→任务核对后的认领事务；一键重放（合成 session，见 replay.go）在
// HTTP handler 里先核对任务与授权后直接调用。同 call 回读、同指纹未决保护与 claim 完全相同。
func (s *taskStore) claimRequest(ctx context.Context, inv operation.Invocation, taskID string, spec requestSpec, status, reason string) (*requestModel, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	flowPB, err := proto.Marshal(spec.flow)
	if err != nil {
		return nil, false, err
	}
	sent := sql.NullBool{}
	unsent := unsentRequestStatus(status)
	if unsent {
		sent = sql.NullBool{Bool: false, Valid: true}
	}
	model := &requestModel{
		ID: spec.flow.GetId(), TaskID: taskID, SessionID: inv.SessionID, TurnID: inv.TurnID,
		CallID: inv.CallID, Fingerprint: spec.fingerprint, Method: spec.method, URL: spec.url,
		Status: status, Reason: reason, Sent: sent, Preview: []byte("请求尚未获得目标响应"),
		FlowPB: flowPB, CreatedAt: now,
	}
	if unsent {
		model.CompletedAt = sql.NullString{String: now, Valid: true}
	}
	var inserted int64
	err = s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewInsert().Model(model).On("CONFLICT(task_id,turn_id,call_id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		inserted, err = res.RowsAffected()
		if err != nil {
			return err
		}
		var existing requestModel
		if err := tx.NewSelect().Model(&existing).Where("task_id = ? AND turn_id = ? AND call_id = ?", taskID, inv.TurnID, inv.CallID).Limit(1).Scan(ctx); err != nil {
			return err
		}
		if existing.SessionID != inv.SessionID || existing.Fingerprint != spec.fingerprint {
			return errCallConflict
		}
		if inserted == 0 {
			model = &existing
			return nil
		}
		var unresolved requestModel
		err = tx.NewSelect().Model(&unresolved).Where("task_id = ? AND fingerprint = ? AND id != ? AND status IN (?, ?, ?)", taskID, spec.fingerprint, model.ID, "claimed", requestUnknown, requestCaptureFailed).OrderExpr(`created_at COLLATE "C" ASC`).Limit(1).Scan(ctx)
		if err == nil {
			model = &unresolved
			return errUnknownReplay // 回滚刚插入的候选行。
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errUnknownReplay) {
			return model, false, err
		}
		if errors.Is(err, errCallConflict) {
			var existing requestModel
			if getErr := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
				return db.NewSelect().Model(&existing).Where("task_id = ? AND turn_id = ? AND call_id = ?", taskID, inv.TurnID, inv.CallID).Limit(1).Scan(ctx)
			}); getErr == nil {
				return &existing, false, errCallConflict
			}
		}
		return nil, false, err
	}
	return model, inserted == 1, nil
}

var (
	errUnknownSession = errors.New("原生 session 未对应已保存任务的首会话、独立验证或复测会话")
	errCallConflict   = errors.New("同一 task/turn/call 已认领不同请求，拒绝重放")
	errUnknownReplay  = errors.New("相同具体请求已有未解决 unknown，不自动重发")
)

// finish 原子更新事实、Flow 与结果；不把响应写入全局Artifact。
func (s *taskStore) finish(ctx context.Context, id, status, reason string, preview []byte, previewTruncated, captureTruncated bool, statusCode *int, flow *traffic.Flow) (*requestModel, error) {
	flowPB, err := proto.Marshal(flow)
	if err != nil {
		return nil, err
	}
	var model requestModel
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model(&requestModel{
			Status: status, Reason: reason, Sent: sql.NullBool{Bool: statusCode != nil, Valid: statusCode != nil}, Preview: preview,
			PreviewTruncated: previewTruncated, CaptureTruncated: captureTruncated,
			StatusCode: sql.NullInt64{Int64: int64(valueOrZero(statusCode)), Valid: statusCode != nil},
			FlowPB:     flowPB, CompletedAt: sql.NullString{String: now, Valid: true},
		}).Column("status", "reason", "sent", "preview", "preview_truncated", "capture_truncated", "status_code", "flow_pb", "completed_at").Where("id = ?", id).Exec(ctx)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			return fmt.Errorf("请求证据行更新失败")
		}
		return tx.NewSelect().Model(&model).Where("id = ?", id).Scan(ctx)
	})
	return &model, err
}

func (s *taskStore) unresolvedTurn(ctx context.Context, sessionID, turnID string) (bool, error) {
	var id string
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model((*requestModel)(nil)).Column("id").
			Where("session_id = ? AND turn_id = ? AND status IN (?, ?, ?, ?)", sessionID, turnID, "claimed", requestUnknown, requestCaptureFailed, "review_required").
			Limit(1).Scan(ctx, &id)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *taskStore) evidenceByID(ctx context.Context, taskID, evidenceID string) (*requestModel, error) {
	var model requestModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).Where("task_id = ? AND id = ?", taskID, evidenceID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &model, err
}

func (s *taskStore) evidenceList(ctx context.Context, taskID string, limit int, afterCreatedAt, afterID string) ([]requestModel, error) {
	var models []requestModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		// 列表不返回 preview（详情接口才给），不读该列，避免每行多读最多 64KiB。
		query := db.NewSelect().Model(&models).Column("id", "task_id", "session_id", "turn_id", "call_id", "fingerprint", "method", "url", "status", "reason", "sent", "status_code", "preview_truncated", "capture_truncated", "created_at", "completed_at").
			Where("task_id = ?", taskID)
		if afterCreatedAt != "" {
			query = query.Where(`(created_at COLLATE "C" < ? OR (created_at = ? AND id COLLATE "C" < ?))`, afterCreatedAt, afterCreatedAt, afterID)
		}
		return query.OrderExpr(`created_at COLLATE "C" DESC, id COLLATE "C" DESC`).Limit(int64(limit + 1)).Scan(ctx)
	})
	return models, err
}
