package webapi

import (
	"context"
	"database/sql"
	"strings"

	"github.com/uptrace/bun"
)

// 运营通知（V4）：待审批 / 计划运行失败 / 每周摘要。与漏洞通知共用 webapi_notifications 与投递；
// 三个开关与每条运营通知的内容各存一张新表（旧库按补表方式升级，不 ALTER 原表）。契约见 native/NOTIFY.md「运营通知」。

const (
	NotifyKindApprovalPending = "approval_pending"
	NotifyKindScheduleFailed  = "schedule_failed"
	NotifyKindWeeklyDigest    = "weekly_digest"

	notifyKeyApproval    = "approval:"
	notifyKeyScheduleRun = "schedule_run:"
	notifyKeyDigest      = "digest:"
)

// notifyOpsKinds：运营通知 kind（不关联候选/验证；内容存 webapi_notification_ops）。
var notifyOpsKinds = map[string]bool{NotifyKindApprovalPending: true, NotifyKindScheduleFailed: true, NotifyKindWeeklyDigest: true}

// notifyOpsSettingsModel 单行（id 固定 wecom），逻辑上属于企业微信推送配置。
// weekly_digest_since：每周摘要开关最近一次由关变开的时间（notifyTime；关闭时为空）——更早的时间点不补发。
type notifyOpsSettingsModel struct {
	bun.BaseModel `bun:"table:webapi_notify_ops_settings,alias:webapi_notify_ops_setting"`

	ID                   string `bun:"id,pk"`
	NotifyApproval       bool   `bun:"notify_approval,notnull"`
	NotifyScheduleFailed bool   `bun:"notify_schedule_failed,notnull"`
	NotifyWeeklyDigest   bool   `bun:"notify_weekly_digest,notnull"`
	WeeklyDigestSince    string `bun:"weekly_digest_since,notnull"`
	UpdatedAt            string `bun:"updated_at,notnull"`
}

// notificationOpsModel 是一条运营通知登记时冻结的内容；markdown 在投递时按当前 base_url 生成。
type notificationOpsModel struct {
	bun.BaseModel `bun:"table:webapi_notification_ops,alias:webapi_notification_op"`

	NotificationID string `bun:"notification_id,pk"`
	Title          string `bun:"title,type:text,notnull"`
	AssetID        string `bun:"asset_id,notnull"`
	SessionID      string `bun:"session_id,notnull"`
	PayloadJSON    string `bun:"payload_json,type:text,notnull"`
}

func (s *taskStore) notifyOpsSettings(ctx context.Context) (*notifyOpsSettingsModel, error) {
	return findOptional[notifyOpsSettingsModel](ctx, s.orm, "id = ?", notifyChannelWeCom)
}

// putNotifySettingsAll 单事务写推送配置与运营通知开关。
func (s *taskStore) putNotifySettingsAll(ctx context.Context, model *notifySettingsModel, ops *notifyOpsSettingsModel) error {
	model.ID, ops.ID = notifyChannelWeCom, notifyChannelWeCom
	return s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(model).On("CONFLICT (id) DO UPDATE").
			Set("webhook_cipher = EXCLUDED.webhook_cipher, enabled = EXCLUDED.enabled, require_confirm = EXCLUDED.require_confirm, base_url = EXCLUDED.base_url, updated_at = EXCLUDED.updated_at").
			Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(ops).On("CONFLICT (id) DO UPDATE").
			Set("notify_approval = EXCLUDED.notify_approval, notify_schedule_failed = EXCLUDED.notify_schedule_failed, notify_weekly_digest = EXCLUDED.notify_weekly_digest, weekly_digest_since = EXCLUDED.weekly_digest_since, updated_at = EXCLUDED.updated_at").
			Exec(ctx)
		return err
	})
}

// insertOpsNotification 单事务登记一条运营通知：同一去重键已有任何通知（任何状态）则不登记。返回是否新插入。
func (s *taskStore) insertOpsNotification(ctx context.Context, model *notificationModel, ops *notificationOpsModel) (bool, error) {
	return s.insertOpsNotificationIf(ctx, model, ops, nil)
}

func (s *taskStore) insertOpsNotificationIf(ctx context.Context, model *notificationModel, ops *notificationOpsModel, valid func() bool) (bool, error) {
	inserted := false
	err := s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// IMMEDIATE 事务已在 Begin 取得写锁；等待期间审批可能已决，不能迟到补登记。
		if valid != nil && !valid() {
			return nil
		}
		exists, err := tx.NewSelect().Model((*notificationModel)(nil)).Where("dedupe_key = ?", model.DedupeKey).Exists(ctx)
		if err != nil || exists {
			return err
		}
		if _, err := tx.NewInsert().Model(model).Exec(ctx); err != nil {
			return err
		}
		ops.NotificationID = model.ID
		if _, err := tx.NewInsert().Model(ops).Exec(ctx); err != nil {
			return err
		}
		inserted = true
		return nil
	})
	return inserted && err == nil, err
}

func (s *taskStore) notificationOps(ctx context.Context, id string) (*notificationOpsModel, error) {
	return findOptional[notificationOpsModel](ctx, s.orm, "notification_id = ?", id)
}

func (s *taskStore) notificationKeyExists(ctx context.Context, key string) (bool, error) {
	return s.orm.NewSelect().Model((*notificationModel)(nil)).Where("dedupe_key = ?", key).Exists(ctx)
}

// dismissUnsentNotifications：去重键下仍在 pending_confirm/queued 的通知改为 dismissed（已决的审批不再发送）。
func (s *taskStore) dismissUnsentNotifications(ctx context.Context, key, kind string) error {
	_, err := s.orm.NewUpdate().Model((*notificationModel)(nil)).Set("status = ?", NotifyDismissed).Set("next_attempt_at = NULL").
		Where("dedupe_key = ? AND kind = ? AND status IN (?)", key, kind, bun.In([]string{NotifyPendingConfirm, NotifyQueued})).Exec(ctx)
	return err
}

// dismissQueuedNotification queued → dismissed（投递前发现审批已决）；返回是否更新。
func (s *taskStore) dismissQueuedNotification(ctx context.Context, id string) (bool, error) {
	res, err := s.orm.NewUpdate().Model((*notificationModel)(nil)).Set("status = ?", NotifyDismissed).Set("next_attempt_at = NULL").
		Where("id = ? AND status = ?", id, NotifyQueued).Exec(ctx)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// scheduleRunAt 按 (asset_id, scheduled_for) 取运行记录（recordScheduleRun 冲突不插入时取已有那条）。
func (s *taskStore) scheduleRunAt(ctx context.Context, assetID, scheduledFor string) (*scheduleRunModel, error) {
	return findOptional[scheduleRunModel](ctx, s.orm, "asset_id = ? AND scheduled_for = ?", assetID, scheduledFor)
}

func opsKeySuffix(key, prefix string) string {
	if !strings.HasPrefix(key, prefix) {
		return ""
	}
	return strings.TrimPrefix(key, prefix)
}

func nullNow(at string) sql.NullString { return sql.NullString{String: at, Valid: true} }
