package webapi

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// 企业微信推送（D2）的配置与通知队列表。契约见 native/NOTIFY.md。

const (
	notifyChannelWeCom = "wecom"

	NotifyKindNew        = "new"
	NotifyKindRegression = "regression"
	// NotifyKindManual：漏洞卡上人工推送（K2）；每个 (去重键, 验证) 一行，终态后再推复用该行重新入队。
	NotifyKindManual = "manual"

	NotifyPendingConfirm = "pending_confirm"
	NotifyQueued         = "queued"
	NotifySending        = "sending"
	NotifySent           = "sent"
	NotifyFailed         = "failed"
	NotifyDismissed      = "dismissed"

	// notifyTimeLayout 定宽 UTC 时间，保证字符串比较与时间顺序一致（next_attempt_at 到期查询用）。
	notifyTimeLayout = "2006-01-02T15:04:05.000Z"
)

var notifyStatuses = map[string]bool{NotifyPendingConfirm: true, NotifyQueued: true, NotifySending: true, NotifySent: true, NotifyFailed: true, NotifyDismissed: true}

// notifySettingsModel 单行（id 固定 wecom）。webhook_cipher 为空 = 未配置。
type notifySettingsModel struct {
	bun.BaseModel `bun:"table:webapi_notify_settings,alias:webapi_notify_setting"`

	ID             string `bun:"id,pk"`
	WebhookCipher  string `bun:"webhook_cipher,type:text,notnull"`
	Enabled        bool   `bun:"enabled,notnull"`
	RequireConfirm bool   `bun:"require_confirm,notnull"`
	BaseURL        string `bun:"base_url,type:text,notnull"`
	UpdatedAt      string `bun:"updated_at,notnull"`
}

// notificationModel 是一条待推送/已推送通知；(dedupe_key, kind, verification_id) 唯一。
type notificationModel struct {
	bun.BaseModel `bun:"table:webapi_notifications,alias:webapi_notification"`

	ID             string         `bun:"id,pk"`
	DedupeKey      string         `bun:"dedupe_key,notnull"`
	Kind           string         `bun:"kind,notnull"`
	TaskID         string         `bun:"task_id,notnull"`
	FindingID      string         `bun:"finding_id,notnull"`
	VerificationID string         `bun:"verification_id,notnull"`
	VulnID         string         `bun:"vuln_id,notnull"`
	Status         string         `bun:"status,notnull"`
	Attempts       int            `bun:"attempts,notnull"`
	NextAttemptAt  sql.NullString `bun:"next_attempt_at"`
	LastError      string         `bun:"last_error,type:text,notnull"`
	CreatedAt      string         `bun:"created_at,notnull"`
	SentAt         sql.NullString `bun:"sent_at"`
}

func notifyTime(at time.Time) string { return at.UTC().Format(notifyTimeLayout) }

func newNotificationID() string { return "wn_" + newSessionID() + newSessionID() }

func (s *taskStore) notifySettings(ctx context.Context) (*notifySettingsModel, error) {
	return readOptional[notifySettingsModel](ctx, s, "id = ?", notifyChannelWeCom)
}

func (s *taskStore) putNotifySettings(ctx context.Context, model *notifySettingsModel) error {
	model.ID = notifyChannelWeCom
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(model).
			On("CONFLICT (id) DO UPDATE").
			Set("webhook_cipher = EXCLUDED.webhook_cipher, enabled = EXCLUDED.enabled, require_confirm = EXCLUDED.require_confirm, base_url = EXCLUDED.base_url, updated_at = EXCLUDED.updated_at").
			Exec(ctx)
		return err
	})
}

// hasAnySecretCipher：库里是否已有任何用资产密钥加密的密文（资产测试账号或 webhook）。
func (s *taskStore) hasAnySecretCipher(ctx context.Context) (bool, error) {
	if found, err := s.hasStoredSecrets(ctx); err != nil || found {
		return found, err
	}
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) (bool, error) {
		return db.NewSelect().Model((*notifySettingsModel)(nil)).Where("webhook_cipher <> ''").Exists(ctx)
	})
}

// taskAssetID 返回任务关联的资产（无关联为空）。
func (s *taskStore) taskAssetID(ctx context.Context, taskID string) (string, error) {
	var assetID string
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.QueryRowContext(ctx, `SELECT asset_id FROM webapi_asset_tasks WHERE task_id = ?`, taskID).Scan(&assetID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return assetID, err
}

func (s *taskStore) vulnOfFinding(ctx context.Context, findingID string) (string, error) {
	var vulnID string
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.QueryRowContext(ctx, `SELECT vuln_id FROM webapi_asset_vuln_members WHERE finding_id = ?`, findingID).Scan(&vulnID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return vulnID, err
}

// notifyEvent 是去重键下全部成员的一条验证判读/复测结论（结果保存时间）。
type notifyEvent struct {
	Kind, ID, Value, At string
	at                  time.Time
}

// notifyEvents：vulnID 非空时取该条目全部成员的结果，否则取单个候选的结果。
func (s *taskStore) notifyEvents(ctx context.Context, vulnID, findingID string) ([]notifyEvent, error) {
	scope, arg := `= ?`, findingID
	if vulnID != "" {
		scope, arg = `IN (SELECT finding_id FROM webapi_asset_vuln_members WHERE vuln_id = ?)`, vulnID
	}
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) ([]notifyEvent, error) {
		rows, err := db.QueryContext(ctx, `SELECT 'verify', v.id, COALESCE(r.payload_json::jsonb #>> '{payload,verdict}', ''), r.created_at
			FROM webapi_verifications v JOIN webapi_verification_results r ON r.verification_id = v.id WHERE v.finding_id `+scope+`
			UNION ALL
			SELECT 'retest', t.id, COALESCE(r.payload_json::jsonb #>> '{payload,conclusion}', ''), r.created_at
			FROM webapi_retests t JOIN webapi_retest_results r ON r.retest_id = t.id WHERE t.finding_id `+scope, arg, arg)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []notifyEvent{}
		for rows.Next() {
			var event notifyEvent
			if err := rows.Scan(&event.Kind, &event.ID, &event.Value, &event.At); err != nil {
				return nil, err
			}
			event.at = parseAt(event.At)
			out = append(out, event)
		}
		return out, rows.Err()
	})
}

// insertNotification 单事务登记：kind=new 时同一去重键已有任何通知则不登记；
// (dedupe_key, kind, verification_id) 冲突不重复。返回是否新插入。
func (s *taskStore) insertNotification(ctx context.Context, model *notificationModel) (bool, error) {
	inserted := false
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if model.Kind == NotifyKindNew {
			exists, err := tx.NewSelect().Model((*notificationModel)(nil)).Where("dedupe_key = ?", model.DedupeKey).Exists(ctx)
			if err != nil || exists {
				return err
			}
		}
		res, err := tx.NewInsert().Model(model).On("CONFLICT (dedupe_key, kind, verification_id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		inserted = affected > 0
		return err
	})
	return inserted, err
}

func (s *taskStore) notification(ctx context.Context, id string) (*notificationModel, error) {
	return readOptional[notificationModel](ctx, s, "id = ?", id)
}

// dueNotifications 取到期 queued（next_attempt_at ≤ now），最早到期在前。
func (s *taskStore) dueNotifications(ctx context.Context, now time.Time, limit int) ([]notificationModel, error) {
	var items []notificationModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&items).
			Where(`status = ? AND next_attempt_at IS NOT NULL AND next_attempt_at COLLATE "C" <= ?`, NotifyQueued, notifyTime(now)).
			OrderExpr(`next_attempt_at COLLATE "C" ASC, created_at COLLATE "C" ASC, id COLLATE "C" ASC`).Limit(int64(limit)).Scan(ctx)
	})
	return items, err
}

// claimNotification queued → sending（只在仍为 queued 时）。
func (s *taskStore) claimNotification(ctx context.Context, id string) (bool, error) {
	var affected int64
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model((*notificationModel)(nil)).Set("status = ?", NotifySending).
			Where("id = ? AND status = ?", id, NotifyQueued).Exec(ctx)
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	return affected > 0, err
}

// finishNotification 写投递结果（只更新仍为 sending 的行）。
func (s *taskStore) finishNotification(ctx context.Context, id, status string, attempts int, next sql.NullString, lastError string, sentAt sql.NullString) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*notificationModel)(nil)).
			Set("status = ?", status).Set("attempts = ?", attempts).Set("next_attempt_at = ?", next).
			Set("last_error = ?", lastError).Set("sent_at = ?", sentAt).
			Where("id = ? AND status = ?", id, NotifySending).Exec(ctx)
		return err
	})
}

// resetSendingNotifications：Hub 重启后上次中断在 sending 的通知回到 queued（不计次，可能重复推送）。
func (s *taskStore) resetSendingNotifications(ctx context.Context) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*notificationModel)(nil)).Set("status = ?", NotifyQueued).
			Where("status = ?", NotifySending).Exec(ctx)
		return err
	})
}

// transitionNotification 管理操作：当前状态在 from 中才更新；返回 (存在, 已更新)。
func (s *taskStore) transitionNotification(ctx context.Context, id string, from []string, set func(*bun.UpdateQuery) *bun.UpdateQuery) (bool, bool, error) {
	var exists, updated bool
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := set(tx.NewUpdate().Model((*notificationModel)(nil))).Where("id = ? AND status IN (?)", id, bun.In(from)).Exec(ctx)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected > 0 {
			exists, updated = true, true
			return nil
		}
		exists, err = tx.NewSelect().Model((*notificationModel)(nil)).Where("id = ?", id).Exists(ctx)
		return err
	})
	if err != nil {
		return false, false, err
	}
	return exists, updated, nil
}

// NotificationView 是管理列表项。
type NotificationView struct {
	ID                    string        `json:"id"`
	Kind                  string        `json:"kind"`
	Status                string        `json:"status"`
	DedupeKey             string        `json:"dedupe_key"`
	TaskID                string        `json:"task_id"`
	FindingID             string        `json:"finding_id"`
	FindingNumber         string        `json:"finding_number"`
	VerificationID        string        `json:"verification_id"`
	VerificationSessionID string        `json:"verification_session_id"`
	VulnID                *string       `json:"vuln_id"`
	Title                 string        `json:"title"`
	Asset                 *TaskAssetRef `json:"asset"`
	Attempts              int           `json:"attempts"`
	LastError             string        `json:"last_error"`
	NextAttemptAt         *string       `json:"next_attempt_at"`
	CreatedAt             string        `json:"created_at"`
	SentAt                *string       `json:"sent_at"`
	// SessionID：approval_pending 为消息链接指向的会话（子 Agent 审批为其父研究会话），其他 kind 为 ""。
	SessionID string `json:"session_id"`
}

// 运营通知（V4）的标题/资产/会话取 webapi_notification_ops；资产优先按任务关联，否则按登记时的资产 id（仍存在时）。
const notificationViewSQL = `SELECT n.id, n.kind, n.status, n.dedupe_key, n.task_id, n.finding_id, COALESCE(f.number, 0), n.verification_id,
	COALESCE(v.session_id, ''), n.vuln_id, COALESCE(o.title, r.title, ''), COALESCE(a.id, a2.id, ''), COALESCE(a.name, a2.name, ''), n.attempts, n.last_error,
	n.next_attempt_at, n.created_at, n.sent_at, COALESCE(o.session_id, '')
	FROM webapi_notifications n
	LEFT JOIN webapi_verifications v ON v.id = n.verification_id
	LEFT JOIN webapi_finding_revisions r ON r.id = v.source_revision_id
	LEFT JOIN webapi_findings f ON f.id = n.finding_id
	LEFT JOIN webapi_asset_tasks l ON l.task_id = n.task_id
	LEFT JOIN webapi_assets a ON a.id = l.asset_id
	LEFT JOIN webapi_notification_ops o ON o.notification_id = n.id
	LEFT JOIN webapi_assets a2 ON a2.id = o.asset_id AND o.asset_id <> ''`

// notificationFilter 是 notificationViews 的筛选；空字段不过滤。
type notificationFilter struct {
	Status, ID, DedupeKey string
}

// notificationViews 按 created_at 倒序。调用方按需多取一条以判断 has_more。
func (s *taskStore) notificationViews(ctx context.Context, filter notificationFilter, limit, offset int) ([]NotificationView, error) {
	where := ` WHERE (? = '' OR n.status = ?) AND (? = '' OR n.id = ?) AND (? = '' OR n.dedupe_key = ?)`
	args := []any{filter.Status, filter.Status, filter.ID, filter.ID, filter.DedupeKey, filter.DedupeKey}
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) ([]NotificationView, error) {
		return scanNotificationViews(db.QueryContext(ctx, notificationViewSQL+where+` ORDER BY n.created_at COLLATE "C" DESC, n.id COLLATE "C" DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...))
	})
}

// notificationStatusCounts 不接收列表过滤或游标，一次 GROUP BY 返回全部状态。
func (s *taskStore) notificationStatusCounts(ctx context.Context) (map[string]int, error) {
	counts := map[string]int{"all": 0}
	for status := range notifyStatuses {
		counts[status] = 0
	}
	err := s.queryRows(ctx, func(rows *sql.Rows) error {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
		counts[status] = count
		counts["all"] += count
		return nil
	}, `SELECT status, COUNT(*) FROM webapi_notifications GROUP BY status`)
	return counts, err
}

func scanNotificationViews(rows *sql.Rows, err error) ([]NotificationView, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NotificationView{}
	for rows.Next() {
		var view NotificationView
		var number int64
		var vulnID, assetID, assetName string
		var next, sent sql.NullString
		if err := rows.Scan(&view.ID, &view.Kind, &view.Status, &view.DedupeKey, &view.TaskID, &view.FindingID, &number, &view.VerificationID,
			&view.VerificationSessionID, &vulnID, &view.Title, &assetID, &assetName, &view.Attempts, &view.LastError, &next, &view.CreatedAt, &sent, &view.SessionID); err != nil {
			return nil, err
		}
		if number > 0 {
			view.FindingNumber = findingNumber(number)
		}
		if vulnID != "" {
			view.VulnID = &vulnID
		}
		if assetID != "" {
			view.Asset = &TaskAssetRef{ID: assetID, Name: assetName}
		}
		if next.Valid {
			view.NextAttemptAt = &next.String
		}
		if sent.Valid {
			view.SentAt = &sent.String
		}
		out = append(out, view)
	}
	return out, rows.Err()
}

// notifyInFlightStatuses：尚未到终态的通知（人工推送遇到即 409 in_flight）。
var notifyInFlightStatuses = []string{NotifyPendingConfirm, NotifyQueued, NotifySending}

// enqueueManualNotification 单事务登记一次人工推送（kind=manual，直接 queued、立即到期）：
// 该键有任一未到终态的通知 → 返回其 id（inFlight）不登记；同 (键, manual, 验证) 已有终态行 → 复用该行重新入队
// （attempts/last_error/sent_at 清空，created_at 更新为本次）；否则插入新行。不影响 new/regression 的去重。
func (s *taskStore) enqueueManualNotification(ctx context.Context, model *notificationModel) (id string, inFlight string, err error) {
	err = s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var busy []notificationModel
		if err := tx.NewSelect().Model(&busy).Where("dedupe_key = ? AND status IN (?)", model.DedupeKey, bun.In(notifyInFlightStatuses)).
			OrderExpr(`created_at COLLATE "C" DESC, id COLLATE "C" DESC`).Limit(1).Scan(ctx); err != nil {
			return err
		}
		if len(busy) > 0 {
			inFlight = busy[0].ID
			return nil
		}
		var existing []notificationModel
		if err := tx.NewSelect().Model(&existing).Where("dedupe_key = ? AND kind = ? AND verification_id = ?", model.DedupeKey, NotifyKindManual, model.VerificationID).
			Limit(1).Scan(ctx); err != nil {
			return err
		}
		if len(existing) > 0 {
			id = existing[0].ID
			_, err := tx.NewUpdate().Model((*notificationModel)(nil)).
				Set("status = ?", NotifyQueued).Set("attempts = 0").Set("next_attempt_at = ?", model.NextAttemptAt).
				Set("last_error = ''").Set("sent_at = NULL").Set("created_at = ?", model.CreatedAt).
				Where("id = ?", id).Exec(ctx)
			return err
		}
		id = model.ID
		_, err := tx.NewInsert().Model(model).Exec(ctx)
		return err
	})
	return id, inFlight, err
}
