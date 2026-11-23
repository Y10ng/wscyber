package webapi

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/uptrace/bun"
)

// NotifySettingsView 是 GET/PUT /api/webapi/notify/wecom 的响应；从不包含完整 webhook。
type NotifySettingsView struct {
	Enabled        bool    `json:"enabled"`
	HasWebhook     bool    `json:"has_webhook"`
	WebhookHint    string  `json:"webhook_hint"`
	RequireConfirm bool    `json:"require_confirm"`
	BaseURL        string  `json:"base_url"`
	UpdatedAt      *string `json:"updated_at"`
	// 运营通知开关（V4，默认 false）；require_confirm 不作用于运营通知。
	NotifyApproval       bool `json:"notify_approval"`
	NotifyScheduleFailed bool `json:"notify_schedule_failed"`
	NotifyWeeklyDigest   bool `json:"notify_weekly_digest"`
}

type saveNotifySettings struct {
	Webhook        *string `json:"webhook"`
	Enabled        bool    `json:"enabled"`
	RequireConfirm bool    `json:"require_confirm"`
	BaseURL        string  `json:"base_url"`
	// 运营通知开关：省略（或 null）＝保留原值（无原值为 false），避免不认识这三项的旧客户端整份保存时把它们关掉。
	NotifyApproval       *bool `json:"notify_approval"`
	NotifyScheduleFailed *bool `json:"notify_schedule_failed"`
	NotifyWeeklyDigest   *bool `json:"notify_weekly_digest"`
}

func (c *Capability) notifySettingsView(model *notifySettingsModel, ops *notifyOpsSettingsModel) NotifySettingsView {
	view := NotifySettingsView{}
	if ops != nil {
		view.NotifyApproval, view.NotifyScheduleFailed, view.NotifyWeeklyDigest = ops.NotifyApproval, ops.NotifyScheduleFailed, ops.NotifyWeeklyDigest
	}
	if model == nil {
		return view
	}
	view.Enabled, view.HasWebhook, view.RequireConfirm, view.BaseURL, view.UpdatedAt = model.Enabled, model.WebhookCipher != "", model.RequireConfirm, model.BaseURL, &model.UpdatedAt
	if view.HasWebhook {
		if webhook, err := c.decryptWebhook(model.WebhookCipher); err == nil {
			view.WebhookHint = webhookHint(webhook)
		} else {
			view.WebhookHint = "（无法解密，请重新填写）"
		}
	}
	return view
}

// handleNotifySettingsGet: GET /api/webapi/notify/wecom
func (c *Capability) handleNotifySettingsGet(w http.ResponseWriter, r *http.Request) {
	model, err := c.store.notifySettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送配置失败")
		return
	}
	ops, err := c.store.notifyOpsSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送配置失败")
		return
	}
	writeJSON(w, http.StatusOK, c.notifySettingsView(model, ops))
}

// handleNotifySettingsPut: PUT /api/webapi/notify/wecom {webhook?, enabled, require_confirm, base_url, notify_approval?, notify_schedule_failed?, notify_weekly_digest?}
// webhook 省略或 "" 保留原值，"-" 清除；三个运营通知开关省略即保留。
func (c *Capability) handleNotifySettingsPut(w http.ResponseWriter, r *http.Request) {
	var body saveNotifySettings
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	baseURL, err := normalizeBaseURL(body.BaseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	webhook := ""
	if body.Webhook != nil && *body.Webhook != "" && *body.Webhook != "-" {
		if webhook, err = validateWebhook(*body.Webhook, c.notify.allowHTTP); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	ctx := r.Context()
	existing, err := c.store.notifySettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送配置失败")
		return
	}
	existingOps, err := c.store.notifyOpsSettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送配置失败")
		return
	}
	model := &notifySettingsModel{Enabled: body.Enabled, RequireConfirm: body.RequireConfirm, BaseURL: baseURL, UpdatedAt: nowRFC3339()}
	if existing != nil {
		model.WebhookCipher = existing.WebhookCipher
	}
	ops := &notifyOpsSettingsModel{UpdatedAt: model.UpdatedAt}
	if existingOps != nil {
		*ops = *existingOps
		ops.UpdatedAt = model.UpdatedAt
	}
	for _, item := range []struct {
		value *bool
		field *bool
	}{{body.NotifyApproval, &ops.NotifyApproval}, {body.NotifyScheduleFailed, &ops.NotifyScheduleFailed}, {body.NotifyWeeklyDigest, &ops.NotifyWeeklyDigest}} {
		if item.value != nil {
			*item.field = *item.value
		}
	}
	switch {
	case !ops.NotifyWeeklyDigest:
		ops.WeeklyDigestSince = ""
	case existingOps == nil || !existingOps.NotifyWeeklyDigest || ops.WeeklyDigestSince == "":
		ops.WeeklyDigestSince = notifyTime(c.scheduleClock()) // 由关变开：此前的周一 09:00 不补发
	}
	switch {
	case body.Webhook != nil && *body.Webhook == "-":
		model.WebhookCipher = ""
	case webhook != "":
		hasCipher, err := c.store.hasAnySecretCipher(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取密钥状态失败")
			return
		}
		aead, err := c.secrets.load(!hasCipher)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if model.WebhookCipher, err = sealWebhook(aead, webhook); err != nil {
			writeError(w, http.StatusInternalServerError, "加密 webhook 失败")
			return
		}
	}
	if err := c.store.putNotifySettingsAll(ctx, model, ops); err != nil {
		writeError(w, http.StatusInternalServerError, "保存推送配置失败")
		return
	}
	c.auditNotifySettings(ctx, existing, model, existingOps, ops, webhook, body.Webhook != nil && *body.Webhook == "-")
	writeJSON(w, http.StatusOK, c.notifySettingsView(model, ops))
}

// handleNotifyTest: POST /api/webapi/notify/wecom/test → {ok, errcode, errmsg}；未配置 webhook 409。
func (c *Capability) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	model, err := c.store.notifySettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送配置失败")
		return
	}
	if model == nil || model.WebhookCipher == "" {
		writeError(w, http.StatusConflict, "尚未配置企业微信 webhook")
		return
	}
	webhook, err := c.decryptWebhook(model.WebhookCipher)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !c.notify.reserve(c.scheduleClock(), 1) {
		writeError(w, http.StatusTooManyRequests, "已达每分钟发送上限，请稍后再试")
		return
	}
	outcome, err := sendWeCom(r.Context(), webhook, wecomMarkdown(notifyTestContent))
	if err != nil {
		c.auditNotifyTest(r.Context(), false, -1)
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "errcode": -1, "errmsg": err.Error()})
		return
	}
	c.auditNotifyTest(r.Context(), outcome.ErrCode == 0, outcome.ErrCode)
	writeJSON(w, http.StatusOK, map[string]any{"ok": outcome.ErrCode == 0, "errcode": outcome.ErrCode, "errmsg": outcome.ErrMsg})
}

// handleNotificationList: GET /api/webapi/notifications?status=&limit=1..200&cursor=
func (c *Capability) handleNotificationList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	status := query.Get("status")
	if status != "" && !notifyStatuses[status] {
		writeError(w, http.StatusBadRequest, "status 只能是 pending_confirm/queued/sending/sent/failed/dismissed")
		return
	}
	limit := 50
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "limit 须为 1 至 200")
			return
		}
		limit = parsed
	}
	offset, err := decodeOffsetCursor(query.Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := c.store.notificationViews(r.Context(), notificationFilter{Status: status}, limit+1, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送记录失败")
		return
	}
	counts, err := c.store.notificationStatusCounts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送状态计数失败")
		return
	}
	response := map[string]any{"items": items, "has_more": false, "next_cursor": nil, "status_counts": counts}
	if len(items) > limit {
		response["items"], response["has_more"], response["next_cursor"] = items[:limit], true, encodeOffsetCursor(offset+limit)
	}
	writeJSON(w, http.StatusOK, response)
}

func (c *Capability) notificationAction(action string, from []string, conflict string, set func(*bun.UpdateQuery) *bun.UpdateQuery) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		ctx := r.Context()
		exists, updated, err := c.store.transitionNotification(ctx, id, from, set)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "更新推送记录失败")
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "推送记录不存在")
			return
		}
		if !updated {
			writeError(w, http.StatusConflict, conflict)
			return
		}
		c.auditNotification(ctx, auditActorUser, action, id, nil)
		c.writeNotification(ctx, w, id)
	}
}

func (c *Capability) writeNotification(ctx context.Context, w http.ResponseWriter, id string) {
	views, err := c.store.notificationViews(ctx, notificationFilter{ID: id}, 1, 0)
	if err != nil || len(views) == 0 {
		writeError(w, http.StatusInternalServerError, "读取推送记录失败")
		return
	}
	writeJSON(w, http.StatusOK, views[0])
}

// handleNotificationConfirm: pending_confirm → queued（立即到期）。
func (c *Capability) handleNotificationConfirm(w http.ResponseWriter, r *http.Request) {
	now := sql.NullString{String: notifyTime(c.scheduleClock()), Valid: true}
	c.notificationAction("notification.confirm", []string{NotifyPendingConfirm}, "只有待确认的推送可以确认", func(q *bun.UpdateQuery) *bun.UpdateQuery {
		return q.Set("status = ?", NotifyQueued).Set("next_attempt_at = ?", now)
	})(w, r)
}

// handleNotificationDismiss: pending_confirm|failed → dismissed。
func (c *Capability) handleNotificationDismiss(w http.ResponseWriter, r *http.Request) {
	c.notificationAction("notification.dismiss", []string{NotifyPendingConfirm, NotifyFailed}, "只有待确认或失败的推送可以忽略", func(q *bun.UpdateQuery) *bun.UpdateQuery {
		return q.Set("status = ?", NotifyDismissed).Set("next_attempt_at = NULL")
	})(w, r)
}

// handleNotificationRetry: failed → queued，attempts 清零（立即到期）。
func (c *Capability) handleNotificationRetry(w http.ResponseWriter, r *http.Request) {
	now := sql.NullString{String: notifyTime(c.scheduleClock()), Valid: true}
	c.notificationAction("notification.retry", []string{NotifyFailed}, "只有失败的推送可以重试", func(q *bun.UpdateQuery) *bun.UpdateQuery {
		return q.Set("status = ?", NotifyQueued).Set("attempts = 0").Set("next_attempt_at = ?", now)
	})(w, r)
}
