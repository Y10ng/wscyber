package webapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// 漏洞卡上的企业微信推送（K2）：人工推送（kind=manual）、推送预览与卡片 notify 段。
// 消息内容与自动推送同一构建函数（buildNotifyMessage）；契约见 native/NOTIFY.md「漏洞卡人工推送」。

const (
	vulnCardNotifyMax = 5

	notifyConflictNotConfigured = "not_configured"
	notifyConflictFalsePositive = "false_positive"
	notifyConflictNoSupported   = "no_supported"
	notifyConflictInFlight      = "in_flight"
)

// NotifyPreviewImage 是将随消息发送的证据图片：前端用 GET /tasks/{task_id}/evidence/{evidence_id}/image.png?mark=… 显示。
type NotifyPreviewImage struct {
	TaskID     string   `json:"task_id"`
	EvidenceID string   `json:"evidence_id"`
	Marks      []string `json:"marks"`
}

// NotifyPreview 是 GET /vulns/{key}/card/notify/preview 的响应。
type NotifyPreview struct {
	VerificationID string              `json:"verification_id"`
	Markdown       string              `json:"markdown"`
	Image          *NotifyPreviewImage `json:"image"`
	ImageNote      string              `json:"image_note"`
}

// VulnCardNotify 是卡片 notify 段：推送配置状态与该键最近 5 条通知（任何 kind/状态，新到旧）。
type VulnCardNotify struct {
	Configured     bool               `json:"configured"`
	RequireConfirm bool               `json:"require_confirm"`
	Items          []NotificationView `json:"items"`
}

func (c *Capability) cardNotify(ctx context.Context, key string) (VulnCardNotify, error) {
	out := VulnCardNotify{Items: []NotificationView{}}
	settings, err := c.store.notifySettings(ctx)
	if err != nil {
		return out, err
	}
	if settings != nil {
		out.Configured = settings.WebhookCipher != "" && settings.Enabled
		out.RequireConfirm = settings.RequireConfirm
	}
	items, err := c.store.notificationViews(ctx, notificationFilter{DedupeKey: key}, vulnCardNotifyMax, 0)
	if err != nil {
		return out, err
	}
	out.Items = items
	return out, nil
}

// manualNotifyVerification 选用人工推送的验证：与卡片 repro 同一条（最新 supported 且带 key_steps），
// 无则成员最新 supported 验证；都没有返回 nil。verifications 须按结果时间倒序（cardVerifications）。
func manualNotifyVerification(verifications []cardVerification) *cardVerification {
	if chosen, _ := pickRepro(verifications); chosen != nil {
		return chosen
	}
	for index := range verifications {
		if item := &verifications[index]; item.stored != nil && item.stored.Payload.Verdict == "supported" {
			return item
		}
	}
	return nil
}

func writeNotifyConflict(w http.ResponseWriter, code, message string, extra map[string]any) {
	body := map[string]any{"error": message, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, http.StatusConflict, body)
}

// manualNotifyTarget 定位条目与选用验证；失败时已写响应（400/404/409 no_supported/500）。
func (c *Capability) manualNotifyTarget(w http.ResponseWriter, r *http.Request) (*GlobalVuln, *cardVerification, bool) {
	key, ok := vulnCardKey(w, r)
	if !ok {
		return nil, nil, false
	}
	ctx := r.Context()
	item, _, err := c.cardTarget(ctx, key)
	if errors.Is(err, errTriageNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return nil, nil, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞条目失败")
		return nil, nil, false
	}
	verifications, err := c.store.cardVerifications(ctx, memberFindingIDs(item))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取验证结果失败")
		return nil, nil, false
	}
	return item, manualNotifyVerification(verifications), true
}

// manualNotificationModel 是人工推送将登记/预览所用的通知行（直接 queued、立即到期）。
func (c *Capability) manualNotificationModel(item *GlobalVuln, chosen *cardVerification) *notificationModel {
	now := notifyTime(c.scheduleClock())
	vulnID := ""
	if item.assetID != "" {
		vulnID = item.ID
	}
	return &notificationModel{ID: newNotificationID(), DedupeKey: item.ID, Kind: NotifyKindManual, TaskID: chosen.model.TaskID, FindingID: chosen.model.FindingID,
		VerificationID: chosen.model.ID, VulnID: vulnID, Status: NotifyQueued, NextAttemptAt: sql.NullString{String: now, Valid: true}, CreatedAt: now}
}

// handleVulnCardNotify: POST /api/webapi/vulns/{key}/card/notify（空 body 或 {}）→ 201 NotificationView。
// 人点按钮即确认：直接 queued、立即到期，不受 require_confirm 影响；由投递 goroutine 发送。
func (c *Capability) handleVulnCardNotify(w http.ResponseWriter, r *http.Request) {
	if _, ok := vulnCardKey(w, r); !ok {
		return
	}
	var input struct{}
	if err := decodeJSONBody(r, &input, true, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, chosen, ok := c.manualNotifyTarget(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	settings, err := c.store.notifySettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送配置失败")
		return
	}
	if settings == nil || settings.WebhookCipher == "" || !settings.Enabled {
		writeNotifyConflict(w, notifyConflictNotConfigured, "企业微信推送未配置或未启用", nil)
		return
	}
	if item.Triage != nil && item.Triage.Disposition == DispositionFalsePositive {
		writeNotifyConflict(w, notifyConflictFalsePositive, "该漏洞已处置为误报，不推送", nil)
		return
	}
	if chosen == nil {
		writeNotifyConflict(w, notifyConflictNoSupported, "该漏洞没有独立验证支持的判读，无法推送", nil)
		return
	}
	model := c.manualNotificationModel(item, chosen)
	id, inFlight, err := c.store.enqueueManualNotification(ctx, model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "登记推送失败")
		return
	}
	if inFlight != "" {
		writeNotifyConflict(w, notifyConflictInFlight, "该漏洞已有待确认、排队或发送中的推送", map[string]any{"notification_id": inFlight})
		return
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.notify", TargetKind: "vuln", TargetID: item.ID, TaskID: chosen.model.TaskID, AssetID: item.assetID,
		Summary: fmt.Sprintf("人工推送漏洞「%s」到企业微信", item.Title),
		Detail:  map[string]any{"vuln_key": item.ID, "notification_id": id, "verification_id": chosen.model.ID, "kind": NotifyKindManual}})
	views, err := c.store.notificationViews(ctx, notificationFilter{ID: id}, 1, 0)
	if err != nil || len(views) == 0 {
		writeError(w, http.StatusInternalServerError, "读取推送记录失败")
		return
	}
	writeJSON(w, http.StatusCreated, views[0])
}

// handleVulnCardNotifyPreview: GET /api/webapi/vulns/{key}/card/notify/preview（只读，不登记审计；未配置也可预览）。
// markdown 与人工推送实际发送的逐字相同（同一 buildNotifyMessage，按当前 base_url）。
func (c *Capability) handleVulnCardNotifyPreview(w http.ResponseWriter, r *http.Request) {
	item, chosen, ok := c.manualNotifyTarget(w, r)
	if !ok {
		return
	}
	if chosen == nil {
		writeNotifyConflict(w, notifyConflictNoSupported, "该漏洞没有独立验证支持的判读，无法推送", nil)
		return
	}
	ctx := r.Context()
	settings, err := c.store.notifySettings(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取推送配置失败")
		return
	}
	baseURL := ""
	if settings != nil {
		baseURL = settings.BaseURL
	}
	message, err := c.buildNotifyMessage(ctx, *c.manualNotificationModel(item, chosen), baseURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成推送预览失败："+strings.TrimSpace(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, NotifyPreview{VerificationID: chosen.model.ID, Markdown: message.markdown, Image: message.imageSource, ImageNote: message.note})
}
