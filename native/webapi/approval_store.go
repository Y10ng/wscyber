package webapi

import (
	"context"
	"database/sql"
	"errors"

	"github.com/uptrace/bun"
)

// 审批记录（R2）与运营通知待登记表的存储。契约见 native/POLICY.md「审批记录」、native/NOTIFY.md「运营通知」。
// 所有写入走 s.write（全局写锁）；回调内只用 tx，不嵌套 s.read/s.write。

const (
	approvalStatusPending  = "pending"
	approvalStatusApproved = "approved"
	approvalStatusRejected = "rejected"
	approvalStatusExpired  = "expired"
	approvalStatusCanceled = "canceled"

	approvalRestartReason = "Hub 重启，调用已中断"
)

var approvalStatuses = map[string]bool{approvalStatusPending: true, approvalStatusApproved: true, approvalStatusRejected: true, approvalStatusExpired: true, approvalStatusCanceled: true}

// approvalModel 是一次危险操作审批。summary/reason 已按资产测试账号回替占位符并经 guardrail 遮盖。
// hub_instance 是写入进程的随机实例 ID：启动时只把其它实例遗留的 pending 判为“Hub 重启中断”。
type approvalModel struct {
	bun.BaseModel `bun:"table:webapi_approvals,alias:webapi_approval"`

	ID              string         `bun:"id,pk"`
	OperationID     string         `bun:"operation_id,notnull"`
	TaskID          string         `bun:"task_id,notnull"`
	SessionID       string         `bun:"session_id,notnull"`
	ParentSessionID string         `bun:"parent_session_id,notnull"` // 子 Agent 审批的父研究会话；否则 ""
	TurnID          string         `bun:"turn_id,notnull"`
	CallID          string         `bun:"call_id,notnull"`
	Tool            string         `bun:"tool,notnull"`
	Summary         string         `bun:"summary,type:text,notnull"`
	Reason          string         `bun:"reason,type:text,notnull"`
	Status          string         `bun:"status,notnull"`
	DecidedBy       string         `bun:"decided_by,notnull"`
	StatusReason    string         `bun:"status_reason,type:text,notnull"`
	HubInstance     string         `bun:"hub_instance,notnull"`
	CreatedAt       string         `bun:"created_at,notnull"`
	ExpiresAt       string         `bun:"expires_at,notnull"`
	DecidedAt       sql.NullString `bun:"decided_at"`
}

// notifyRegistrationModel 是一项尚未落到 webapi_notifications 的运营通知登记（取代进程内队列）。
// 存在即 register_pending；登记成功或放弃时删除。next_attempt_at 用真实时钟（定宽 UTC 毫秒）。
type notifyRegistrationModel struct {
	bun.BaseModel `bun:"table:webapi_notify_registrations,alias:webapi_notify_registration"`

	Key           string `bun:"key,pk"`
	Kind          string `bun:"kind,notnull"`
	ApprovalID    string `bun:"approval_id,notnull"` // approval_pending：webapi_approvals.id；其余 ""
	ArgsJSON      string `bun:"args_json,type:text,notnull"`
	Attempts      int    `bun:"attempts,notnull"`
	NextAttemptAt string `bun:"next_attempt_at,notnull"`
	LastError     string `bun:"last_error,type:text,notnull"`
	CreatedAt     string `bun:"created_at,notnull"`
}

func newApprovalID() string { return "ap_" + newSessionID() + newSessionID() }

// insertApproval 落 pending 行；job 非空时同一事务登记待发的 approval_pending 通知（同键已有则不重复）。
func (s *taskStore) insertApproval(ctx context.Context, model *approvalModel, job *notifyRegistrationModel) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(model).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
		if job == nil {
			return nil
		}
		_, err := tx.NewInsert().Model(job).On("CONFLICT (key) DO NOTHING").Exec(ctx)
		return err
	})
}

// finishApproval 把 pending 行改为 final 的终态（决定人、说明、时间）；行不存在（挂起时落库失败）则整行插入终态。
// 实际发生迁移时在同一事务里追加审计（audit 可为 nil）、删除该审批的待登记并撤销未发出的待审批通知。
// 行已是终态（如已被重启清理）时什么都不改，返回 false。
func (s *taskStore) finishApproval(ctx context.Context, final approvalModel, audit *auditEventModel) (bool, error) {
	changed := false
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		changed = false
		res, err := tx.NewUpdate().Model((*approvalModel)(nil)).
			Set("status = ?", final.Status).Set("decided_by = ?", final.DecidedBy).Set("status_reason = ?", final.StatusReason).Set("decided_at = ?", final.DecidedAt).
			Where("id = ? AND status = ?", final.ID, approvalStatusPending).Exec(ctx)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			exists, err := tx.NewSelect().Model((*approvalModel)(nil)).Where("id = ?", final.ID).Exists(ctx)
			if err != nil || exists {
				return err
			}
			if _, err := tx.NewInsert().Model(&final).Exec(ctx); err != nil {
				return err
			}
		}
		changed = true
		if audit != nil {
			if err := insertAuditTx(ctx, tx, audit); err != nil {
				return err
			}
		}
		return closeApprovalNotificationsTx(ctx, tx, final.ID, final.OperationID)
	})
	return changed && err == nil, err
}

// closeApprovalNotificationsTx：审批已决后不再登记、也不再发送该审批的待审批通知（已发出的保持 sent）。
func closeApprovalNotificationsTx(ctx context.Context, tx bun.Tx, approvalID, operationID string) error {
	if _, err := tx.NewDelete().Model((*notifyRegistrationModel)(nil)).Where("approval_id = ?", approvalID).Exec(ctx); err != nil {
		return err
	}
	if operationID == "" {
		return nil
	}
	_, err := tx.NewUpdate().Model((*notificationModel)(nil)).Set("status = ?", NotifyDismissed).Set("next_attempt_at = NULL").
		Where("dedupe_key = ? AND kind = ? AND status IN (?)", notifyKeyApproval+operationID, NotifyKindApprovalPending, bun.In([]string{NotifyPendingConfirm, NotifyQueued})).Exec(ctx)
	return err
}

// cancelInterruptedApprovals：Hub 启动时把其它实例遗留的 pending 行改为 canceled（system，“Hub 重启，调用已中断”），
// 同一事务里每行追加一条 approval.cancel 审计、删除其待登记、撤销未发出的待审批通知。返回被改的行。
func (s *taskStore) cancelInterruptedApprovals(ctx context.Context, instance, decidedAt string, audit func(approvalModel, string) (*auditEventModel, error)) ([]approvalModel, error) {
	var rows []approvalModel
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		rows = nil
		if err := tx.NewSelect().Model(&rows).Where("status = ? AND hub_instance <> ?", approvalStatusPending, instance).
			OrderExpr(`created_at COLLATE "C", id COLLATE "C"`).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		for i := range rows {
			row := &rows[i]
			row.Status, row.DecidedBy, row.StatusReason, row.DecidedAt = approvalStatusCanceled, auditActorSystem, approvalRestartReason, sql.NullString{String: decidedAt, Valid: true}
			if _, err := tx.NewUpdate().Model(row).Column("status", "decided_by", "status_reason", "decided_at").WherePK().Exec(ctx); err != nil {
				return err
			}
			assetID, err := taskAssetIDTx(ctx, tx, row.TaskID)
			if err != nil {
				return err
			}
			entry, err := audit(*row, assetID)
			if err != nil {
				return err
			}
			if err := insertAuditTx(ctx, tx, entry); err != nil {
				return err
			}
			if err := closeApprovalNotificationsTx(ctx, tx, row.ID, row.OperationID); err != nil {
				return err
			}
		}
		return nil
	})
	return rows, err
}

func taskAssetIDTx(ctx context.Context, db bun.IDB, taskID string) (string, error) {
	if taskID == "" {
		return "", nil
	}
	var assetID string
	err := db.QueryRowContext(ctx, `SELECT asset_id FROM webapi_asset_tasks WHERE task_id = ?`, taskID).Scan(&assetID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return assetID, err
}

func (s *taskStore) approval(ctx context.Context, id string) (*approvalModel, error) {
	return readOptional[approvalModel](ctx, s, "id = ?", id)
}

// approvalFilter：taskID 精确匹配；sessionID 匹配发起会话或父研究会话（子 Agent 审批经父会话处理）。
type approvalFilter struct {
	taskID, sessionID, status string
	afterAt, afterID          string
	limit                     int
}

func (f approvalFilter) apply(query *bun.SelectQuery) *bun.SelectQuery {
	if f.taskID != "" {
		query = query.Where("task_id = ?", f.taskID)
	}
	if f.sessionID != "" {
		query = query.Where("(session_id = ? OR parent_session_id = ?)", f.sessionID, f.sessionID)
	}
	return query
}

// approvals 按 created_at、id 倒序取一页，并返回同一任务/会话范围内的 pending 总数（不受 status 与分页影响）。
func (s *taskStore) approvals(ctx context.Context, filter approvalFilter) ([]approvalModel, int, error) {
	var rows []approvalModel
	pending := 0
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		count, err := filter.apply(db.NewSelect().Model((*approvalModel)(nil))).Where("status = ?", approvalStatusPending).Count(ctx)
		if err != nil {
			return err
		}
		pending = int(count)
		query := filter.apply(db.NewSelect().Model(&rows))
		if filter.status != "" {
			query = query.Where("status = ?", filter.status)
		}
		if filter.afterAt != "" {
			query = query.Where(`(created_at COLLATE "C" < ? OR created_at = ? AND id COLLATE "C" < ?)`, filter.afterAt, filter.afterAt, filter.afterID)
		}
		return query.OrderExpr(`created_at COLLATE "C" DESC, id COLLATE "C" DESC`).Limit(int64(filter.limit)).Scan(ctx)
	})
	return rows, pending, err
}

// ---- 运营通知待登记 ----

func enqueueRegistrationTx(ctx context.Context, tx bun.Tx, job *notifyRegistrationModel) error {
	_, err := tx.NewInsert().Model(job).On("CONFLICT (key) DO NOTHING").Exec(ctx)
	return err
}

// dueRegistrations 取 next_attempt_at ≤ now 的待登记（最早在前）。
func (s *taskStore) dueRegistrations(ctx context.Context, now string, limit int) ([]notifyRegistrationModel, error) {
	var rows []notifyRegistrationModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&rows).Where(`next_attempt_at COLLATE "C" <= ?`, now).
			OrderExpr(`next_attempt_at COLLATE "C", key COLLATE "C"`).Limit(int64(limit)).Scan(ctx)
	})
	return rows, err
}

func (s *taskStore) deleteRegistration(ctx context.Context, key string) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewDelete().Model((*notifyRegistrationModel)(nil)).Where("key = ?", key).Exec(ctx)
		return err
	})
}

// deferRegistration 记一次失败：attempts+1、推迟到 next、保存错误摘要。
func (s *taskStore) deferRegistration(ctx context.Context, key, next, lastError string) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*notifyRegistrationModel)(nil)).Set("attempts = attempts + 1").Set("next_attempt_at = ?", next).Set("last_error = ?", lastError).
			Where("key = ?", key).Exec(ctx)
		return err
	})
}

func (s *taskStore) registrationCount(ctx context.Context) (int, error) {
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) (int, error) {
		count, err := db.NewSelect().Model((*notifyRegistrationModel)(nil)).Count(ctx)
		return int(count), err
	})
}
