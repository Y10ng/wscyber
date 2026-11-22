package webapi

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	"github.com/chainreactors/cyber/aop/traffic"
)

// 企业微信群机器人推送（D2）：经独立验证判读为 supported 的漏洞登记一条通知，
// Hub 内单 goroutine 按到期发送 markdown + 证据图片。契约见 native/NOTIFY.md。

const (
	notifyInterval        = 10 * time.Second
	notifyBatch           = 50
	notifyMaxAttempts     = 3
	notifyRateLimit       = 18 // 全局每分钟发送条数上限（企业微信每机器人 20 条/分钟）。
	notifyRateWindow      = time.Minute
	notifyThrottleDelay   = 60 * time.Second
	notifySendTimeout     = 15 * time.Second
	wecomMarkdownLimit    = 4096
	wecomImageLimit       = 2 << 20
	wecomErrRateLimited   = 45009
	notifyImageRetryLines = 30
	notifyMaxWebhook      = 2048
	notifyMaxBaseURL      = 512
	notifyDangerRunes     = 120
	notifyStepRunes       = 80
	notifyMaxSteps        = 3
	notifyMinSecretRunes  = 4
	notifySecretMask      = "******"
	notifyImageTooLarge   = "证据图片过大未发送"
	notifyImageFailed     = "证据图片生成失败未发送"
	notifyTestContent     = "wscyber 推送测试"
	notifyWebhookAAD      = "notify:wecom"
)

// notifyRetryDelays[attempts-1]：第 1、2 次失败后分别 1 分钟、5 分钟重试；第 3 次失败 → failed。
var notifyRetryDelays = []time.Duration{time.Minute, 5 * time.Minute}

var wecomHTTPClient = &http.Client{
	Timeout: notifySendTimeout,
	// 不跟随重定向：webhook 只发往配置的地址。
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// notifyCapabilities：taskStore → Capability。验证结果工具只持有 store，借此找到所属能力登记通知。
var notifyCapabilities sync.Map

// notifyRuntime 是推送 goroutine 与发送频率窗口；allowHTTP/interval/imageLimit 仅测试设置。
type notifyRuntime struct {
	once   sync.Once
	cancel context.CancelFunc
	done   chan struct{}

	runMu sync.Mutex // 串行投递（goroutine 与测试直接调用）
	mu    sync.Mutex
	sent  []time.Time

	allowHTTP  bool          // 允许 http:// webhook（仅测试注入 httptest 地址）
	interval   time.Duration // 0 = notifyInterval
	imageLimit int           // 0 = wecomImageLimit
}

// reserve 在全局每分钟窗口内预留 n 条发送名额；不足返回 false（本轮不再发送）。
func (r *notifyRuntime) reserve(now time.Time, n int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := now.Add(-notifyRateWindow)
	kept := r.sent[:0]
	for _, at := range r.sent {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	r.sent = kept
	if len(r.sent)+n > notifyRateLimit {
		return false
	}
	for range n {
		r.sent = append(r.sent, now)
	}
	return true
}

func (c *Capability) startNotifier() {
	c.notify.once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		c.notify.cancel, c.notify.done = cancel, make(chan struct{})
		interval := c.notify.interval
		if interval <= 0 {
			interval = notifyInterval
		}
		go func() {
			defer close(c.notify.done)
			if err := c.store.resetSendingNotifications(ctx); err != nil && ctx.Err() == nil {
				log.Printf("webapi 推送：恢复中断的发送失败：%v", err)
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := c.registerWeeklyDigest(ctx); err != nil && ctx.Err() == nil {
						log.Printf("webapi 推送：登记每周摘要失败：%v", err)
					}
					if err := c.deliverNotifications(ctx); err != nil && ctx.Err() == nil {
						log.Printf("webapi 推送：%v", err)
					}
				}
			}
		}()
	})
}

func (c *Capability) stopNotifier() {
	c.notify.once.Do(func() {})
	if c.notify.cancel != nil {
		c.notify.cancel()
		<-c.notify.done
	}
}

// ---- webhook 加密 ----

func sealWebhook(aead cipher.AEAD, plain string) (string, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), []byte(notifyWebhookAAD))
	return secretCipherV1 + base64.StdEncoding.EncodeToString(sealed), nil
}

func openWebhook(aead cipher.AEAD, stored string) (string, error) {
	errOpen := errors.New("webhook 密文无法用当前密钥解密（密钥文件可能被替换）")
	if !strings.HasPrefix(stored, secretCipherV1) {
		return "", errOpen
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, secretCipherV1))
	if err != nil || len(raw) < aead.NonceSize() {
		return "", errOpen
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(notifyWebhookAAD))
	if err != nil {
		return "", errOpen
	}
	return string(plain), nil
}

func (c *Capability) decryptWebhook(stored string) (string, error) {
	aead, err := c.secrets.load(false)
	if err != nil {
		return "", err
	}
	return openWebhook(aead, stored)
}

// validateWebhook：https URL（测试可放开 http），有 host、无 userinfo/fragment。
func validateWebhook(raw string, allowHTTP bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > notifyMaxWebhook {
		return "", fmt.Errorf("webhook 不能超过 %d 字节", notifyMaxWebhook)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", errors.New("webhook 必须是 https:// 开头的完整地址")
	}
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return "", errors.New("webhook 必须是 https:// 开头的完整地址")
	}
	if u.User != nil || u.Fragment != "" {
		return "", errors.New("webhook 不能包含用户信息或 #片段")
	}
	return raw, nil
}

// webhookHint 只给出 host 与 key（无 key 参数时为整个地址）末 4 位。
func webhookHint(webhook string) string {
	u, err := url.Parse(webhook)
	if err != nil {
		return ""
	}
	key := u.Query().Get("key")
	if key == "" {
		key = strings.TrimRight(u.Path, "/")
	}
	runes := []rune(key)
	if len(runes) > 4 {
		runes = runes[len(runes)-4:]
	}
	if len(runes) == 0 {
		return u.Hostname()
	}
	return u.Hostname() + " …" + string(runes)
}

// normalizeBaseURL：空或 http(s) 绝对地址（去尾 /）。
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	if len(raw) > notifyMaxBaseURL {
		return "", fmt.Errorf("base_url 不能超过 %d 字节", notifyMaxBaseURL)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		strings.ContainsAny(raw, " ()\r\n\t") {
		return "", errors.New("base_url 必须是不含 query/#片段/空白/括号的 http(s) 地址")
	}
	return raw, nil
}

// ---- 触发登记 ----

// notifyVerificationSupported 由验证结果工具在 supported 结果保存成功后调用；失败只记日志。
func notifyVerificationSupported(ctx context.Context, store *taskStore, verificationID string) {
	value, ok := notifyCapabilities.Load(store)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := value.(*Capability).registerVerificationNotification(ctx, verificationID); err != nil {
		log.Printf("webapi 推送：登记验证 %s 的通知失败：%v", verificationID, err)
	}
}

// registerVerificationNotification：未配置 webhook 或未启用不登记；条目处置为误报不登记（V1）；去重键为 C3 条目 id（任务关联资产时，先同步）
// 或 finding:<id>。同键只登记一次；本次之前最新决定性事件为已修复时登记 regression。
func (c *Capability) registerVerificationNotification(ctx context.Context, verificationID string) error {
	settings, err := c.store.notifySettings(ctx)
	if err != nil {
		return err
	}
	if settings == nil || settings.WebhookCipher == "" || !settings.Enabled {
		return nil
	}
	verification, err := findOptional[verificationModel](ctx, c.store.orm, "id = ?", verificationID)
	if err != nil || verification == nil {
		return errors.Join(errors.New("验证不存在"), err)
	}
	result, err := findOptional[verificationResultModel](ctx, c.store.orm, "verification_id = ?", verificationID)
	if err != nil || result == nil {
		return errors.Join(errors.New("验证结果不存在"), err)
	}
	var stored storedVerificationResult
	if err := json.Unmarshal([]byte(result.PayloadJSON), &stored); err != nil {
		return err
	}
	if stored.Payload.Verdict != "supported" {
		return nil
	}
	assetID, err := c.store.taskAssetID(ctx, verification.TaskID)
	if err != nil {
		return err
	}
	vulnID := ""
	if assetID != "" {
		unlock := c.lockAssetVulns(assetID)
		err = c.store.syncAssetVulns(ctx, assetID)
		if err == nil {
			vulnID, err = c.store.vulnOfFinding(ctx, verification.FindingID)
		}
		unlock()
		if err != nil {
			return err
		}
	}
	dedupeKey := "finding:" + verification.FindingID
	if vulnID != "" {
		dedupeKey = vulnID
	}
	// V1：条目处置为误报时不再登记（接受风险照常登记，含 regression）。
	if disposition, err := c.store.vulnDisposition(ctx, dedupeKey); err != nil {
		return err
	} else if disposition == DispositionFalsePositive {
		return nil
	}
	events, err := c.store.notifyEvents(ctx, vulnID, verification.FindingID)
	if err != nil {
		return err
	}
	kind := NotifyKindNew
	if latestDecisiveBefore(events, verificationID, result.CreatedAt) == VulnFixed {
		kind = NotifyKindRegression
	}
	now := c.scheduleClock()
	model := &notificationModel{ID: newNotificationID(), DedupeKey: dedupeKey, Kind: kind, TaskID: verification.TaskID, FindingID: verification.FindingID,
		VerificationID: verificationID, VulnID: vulnID, Status: NotifyQueued, NextAttemptAt: sql.NullString{String: notifyTime(now), Valid: true}, CreatedAt: notifyTime(now)}
	if settings.RequireConfirm {
		model.Status, model.NextAttemptAt = NotifyPendingConfirm, sql.NullString{}
	}
	_, err = c.store.insertNotification(ctx, model)
	return err
}

// latestDecisiveBefore 返回本次验证结果之前（不含本次）最新决定性事件：VulnFixed / VulnPresent / ""。
func latestDecisiveBefore(events []notifyEvent, verificationID, resultAt string) string {
	current := notifyEvent{Kind: FollowupVerify, ID: verificationID, At: resultAt, at: parseAt(resultAt)}
	var latest *notifyEvent
	state := ""
	for index := range events {
		event := events[index]
		if event.Kind == FollowupVerify && event.ID == verificationID {
			continue
		}
		if notifyEventLater(event, current) {
			continue
		}
		present, fixed := decisive(vulnEvent{Kind: event.Kind, Value: event.Value})
		if !present && !fixed {
			continue
		}
		if latest == nil || notifyEventLater(event, *latest) {
			latest = &events[index]
			state = VulnPresent
			if fixed {
				state = VulnFixed
			}
		}
	}
	return state
}

func notifyEventLater(a, b notifyEvent) bool {
	return laterThan(a.at, a.At+"\x00"+a.Kind+"\x00"+a.ID, b.at, b.At+"\x00"+b.Kind+"\x00"+b.ID)
}

// ---- 投递 ----

type wecomOutcome struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// sendWeCom POST 一条消息；err 为网络/HTTP/响应格式错误（不含 webhook 地址）。
func sendWeCom(ctx context.Context, webhook string, payload any) (wecomOutcome, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return wecomOutcome{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return wecomOutcome{}, errors.New("webhook 地址无效")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := wecomHTTPClient.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // url.Error 会带完整 webhook（含 key），不外露。
		}
		return wecomOutcome{}, fmt.Errorf("网络错误：%v", err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return wecomOutcome{}, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	var outcome wecomOutcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return wecomOutcome{}, errors.New("响应不是企业微信 JSON")
	}
	return outcome, nil
}

func wecomMarkdown(content string) map[string]any {
	return map[string]any{"msgtype": "markdown", "markdown": map[string]string{"content": content}}
}

func wecomImage(png []byte) map[string]any {
	sum := md5.Sum(png)
	return map[string]any{"msgtype": "image", "image": map[string]string{"base64": base64.StdEncoding.EncodeToString(png), "md5": hex.EncodeToString(sum[:])}}
}

// deliverNotifications 处理一轮到期 queued：每条先 markdown 再图片，两条都成功 → sent。
// 未配置/未启用时不发送（保持 queued）。频率窗口用尽或遇 45009 时本轮停止。
func (c *Capability) deliverNotifications(ctx context.Context) error {
	c.notify.runMu.Lock()
	defer c.notify.runMu.Unlock()
	settings, err := c.store.notifySettings(ctx)
	if err != nil || settings == nil || settings.WebhookCipher == "" || !settings.Enabled {
		return err
	}
	now := c.scheduleClock()
	items, err := c.store.dueNotifications(ctx, now, notifyBatch)
	if err != nil || len(items) == 0 {
		return err
	}
	webhook, err := c.decryptWebhook(settings.WebhookCipher)
	if err != nil {
		return err
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return nil
		}
		if item.Kind == NotifyKindApprovalPending && !c.approvalStillPending(item.DedupeKey) {
			// 审批已决（或 Hub 重启后挂起已不存在）：不再发送。
			if _, err := c.store.dismissQueuedNotification(ctx, item.ID); err != nil {
				return err
			}
			continue
		}
		message, buildErr := c.buildNotifyMessage(ctx, item, settings.BaseURL)
		if buildErr == nil {
			need := 1
			if message.image != nil {
				need = 2
			}
			if !c.notify.reserve(c.scheduleClock(), need) {
				return nil
			}
		}
		claimed, err := c.store.claimNotification(ctx, item.ID)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if buildErr != nil {
			if err := c.failNotification(ctx, item, "生成消息失败："+buildErr.Error()); err != nil {
				return err
			}
			continue
		}
		stop, err := c.sendNotification(ctx, item, webhook, message)
		if err != nil {
			return err
		}
		if stop {
			return nil
		}
	}
	return nil
}

// sendNotification 发送一条已认领（sending）的通知；返回 stop=true 表示本轮停止（频率超限/Hub 关闭）。
func (c *Capability) sendNotification(ctx context.Context, item notificationModel, webhook string, message *notifyMessage) (bool, error) {
	parts := []struct {
		label   string
		payload any
	}{{"markdown", wecomMarkdown(message.markdown)}}
	if message.image != nil {
		parts = append(parts, struct {
			label   string
			payload any
		}{"图片", wecomImage(message.image)})
	}
	for _, part := range parts {
		outcome, err := sendWeCom(ctx, webhook, part.payload)
		store := context.WithoutCancel(ctx)
		switch {
		case ctx.Err() != nil:
			// Hub 关闭打断：回到 queued，不计次。
			return true, c.store.finishNotification(store, item.ID, NotifyQueued, item.Attempts, item.NextAttemptAt, item.LastError, sql.NullString{})
		case err == nil && outcome.ErrCode == wecomErrRateLimited:
			next := sql.NullString{String: notifyTime(c.scheduleClock().Add(notifyThrottleDelay)), Valid: true}
			lastError := fmt.Sprintf("%s：errcode %d %s（频率超限，60 秒后重试，不计次）", part.label, outcome.ErrCode, outcome.ErrMsg)
			return true, c.store.finishNotification(store, item.ID, NotifyQueued, item.Attempts, next, lastError, sql.NullString{})
		case err != nil:
			return false, c.failNotification(store, item, part.label+"："+err.Error())
		case outcome.ErrCode != 0:
			return false, c.failNotification(store, item, fmt.Sprintf("%s：errcode %d %s", part.label, outcome.ErrCode, outcome.ErrMsg))
		}
	}
	sentAt := sql.NullString{String: notifyTime(c.scheduleClock()), Valid: true}
	if err := c.store.finishNotification(context.WithoutCancel(ctx), item.ID, NotifySent, item.Attempts, sql.NullString{}, "", sentAt); err != nil {
		return false, err
	}
	c.auditNotification(ctx, auditActorSystem, "notification.sent", item.ID, map[string]any{"attempts": item.Attempts, "last_error": ""})
	return false, nil
}

// failNotification 计一次失败：未满 3 次按 1 分钟/5 分钟后重试，第 3 次 → failed。
func (c *Capability) failNotification(ctx context.Context, item notificationModel, lastError string) error {
	attempts := item.Attempts + 1
	if attempts >= notifyMaxAttempts {
		if err := c.store.finishNotification(ctx, item.ID, NotifyFailed, attempts, sql.NullString{}, lastError, sql.NullString{}); err != nil {
			return err
		}
		c.auditNotification(ctx, auditActorSystem, "notification.failed", item.ID, map[string]any{"attempts": attempts, "last_error": lastError})
		return nil
	}
	next := sql.NullString{String: notifyTime(c.scheduleClock().Add(notifyRetryDelays[attempts-1])), Valid: true}
	return c.store.finishNotification(ctx, item.ID, NotifyQueued, attempts, next, lastError, sql.NullString{})
}

// ---- 消息内容 ----

type notifyMessage struct {
	markdown string
	image    []byte
	// imageSource：图片渲染所用证据与高亮（人工推送预览用）；无图片时为 nil。
	imageSource *NotifyPreviewImage
	note        string // 附在 markdown 末行的图片说明（无则空串）
}

// notifyFields 是 markdown 的全部字段（只来自已保存的候选与验证文字）。
type notifyFields struct {
	Regression bool
	Manual     bool   // 漏洞卡人工推送（K2）：标题前缀【漏洞通报】
	Severity   string // 处置严重度中文等级（严重/高危/…）；未定级时为 AI 建议等级“AI 建议·高危”（K1），都没有为空
	Title      string
	Asset      string
	Location   string
	VerifiedAt string
	Danger     string
	Steps      []string
	Link       string
	Note       string
	// Repro：危害与步骤来自验证结果的最小复现（K1），行标题为“影响”“关键复现”。
	Repro bool
}

func (c *Capability) buildNotifyMessage(ctx context.Context, item notificationModel, baseURL string) (*notifyMessage, error) {
	if notifyOpsKinds[item.Kind] {
		return c.buildOpsMessage(ctx, item, baseURL)
	}
	verification, err := findOptional[verificationModel](ctx, c.store.orm, "id = ?", item.VerificationID)
	if err != nil || verification == nil {
		return nil, errors.Join(errors.New("验证不存在"), err)
	}
	resultModel, err := findOptional[verificationResultModel](ctx, c.store.orm, "verification_id = ?", item.VerificationID)
	if err != nil || resultModel == nil {
		return nil, errors.Join(errors.New("验证结果不存在"), err)
	}
	var result storedVerificationResult
	if err := json.Unmarshal([]byte(resultModel.PayloadJSON), &result); err != nil {
		return nil, errors.New("验证结果无法解析")
	}
	revision, err := findOptional[findingRevisionModel](ctx, c.store.orm, "id = ?", verification.SourceRevisionID)
	if err != nil || revision == nil {
		return nil, errors.Join(errors.New("候选修订不存在"), err)
	}
	var source storedFindingPayload
	if err := json.Unmarshal([]byte(revision.PayloadJSON), &source); err != nil {
		return nil, errors.New("候选修订无法解析")
	}
	assetName, secrets, err := c.notifyAssetContext(ctx, verification.TaskID)
	if err != nil {
		return nil, err
	}
	triage, err := c.store.triageRecord(ctx, item.DedupeKey)
	if err != nil {
		return nil, err
	}
	fields := notifyFields{
		Regression: item.Kind == NotifyKindRegression,
		Manual:     item.Kind == NotifyKindManual,
		Title:      c.notifyVulnTitle(ctx, item.DedupeKey),
		Asset:      assetName,
		Location:   "未记录",
		VerifiedAt: c.notifyLocalTime(resultModel.CreatedAt),
		Danger:     capRunes(oneLine(firstSentence(source.Content.Description)), notifyDangerRunes),
	}
	if fields.Title == "" { // 漏洞卡标题取不到（条目已并入/移空等）时退回验证固定修订标题。
		fields.Title = revision.Title
	}
	if fields.Title == "" {
		fields.Title = source.Content.Title
	}
	if triage != nil && triage.Severity != "" {
		fields.Severity = severityLabels[triage.Severity]
	} else if suggested := result.Payload.SuggestedSeverity; triageSeveritySet[suggested] {
		fields.Severity = "AI 建议·" + severityLabels[suggested]
	}
	stepTexts := []string{}
	for _, step := range source.Content.Steps {
		stepTexts = append(stepTexts, step.Text)
	}
	if result.Payload.hasRepro() {
		// K1：优先用最小复现的一句话影响与关键步骤标题。
		fields.Repro = true
		fields.Danger = capRunes(oneLine(result.Payload.Impact), notifyDangerRunes)
		stepTexts = stepTexts[:0]
		for _, step := range result.Payload.KeySteps {
			stepTexts = append(stepTexts, step.Title)
		}
	}
	for _, text := range stepTexts {
		if len(fields.Steps) == notifyMaxSteps {
			break
		}
		if text := capRunes(oneLine(text), notifyStepRunes); text != "" {
			fields.Steps = append(fields.Steps, text)
		}
	}
	if baseURL != "" {
		fields.Link = notifyCardLink(baseURL, item.DedupeKey)
	}
	message := &notifyMessage{}
	if step, marks := c.notifyReproEvidence(ctx, verification.TaskID, result.Payload); step != nil {
		// K1：图片与位置优先用最小复现第 1 步证据，并按其 marks 高亮。
		fields.Location = strings.TrimSpace(strings.ToUpper(step.Method) + " " + stripURLUserinfo(step.URL))
		image, note := c.notifyEvidenceImage(*step, secrets, marks)
		message.image, fields.Note = image, note
		if image != nil {
			message.imageSource = &NotifyPreviewImage{TaskID: step.TaskID, EvidenceID: step.ID, Marks: marks}
		}
	} else {
		evidence, model := c.notifyEvidence(ctx, verification.TaskID, result.Evidence, source.Evidence)
		if evidence != nil {
			fields.Location = strings.TrimSpace(strings.ToUpper(evidence.Method) + " " + stripURLUserinfo(evidence.URL))
		}
		switch {
		case model != nil:
			image, note := c.notifyEvidenceImage(*model, secrets, nil)
			message.image, fields.Note = image, note
			if image != nil {
				message.imageSource = &NotifyPreviewImage{TaskID: model.TaskID, EvidenceID: model.ID, Marks: []string{}}
			}
		case evidence != nil: // 引用的证据行读不到
			fields.Note = notifyImageFailed
		}
	}
	fields = scrubNotifyFields(fields, secrets)
	message.markdown, message.note = composeNotifyMarkdown(fields), fields.Note
	return message, nil
}

// notifyCardLink 是消息里的平台链接：漏洞卡页（前端支持 ?page=vulns&vuln=<key> 直接打开）。
func notifyCardLink(baseURL, key string) string {
	return baseURL + "/?page=vulns&vuln=" + url.QueryEscape(key)
}

// notifyVulnTitle 返回漏洞卡 header.title（人工改名优先，否则最新成员修订标题）；取不到返回 ""。
func (c *Capability) notifyVulnTitle(ctx context.Context, key string) string {
	target, unlock, err := c.resolveVulnKey(ctx, key)
	if err != nil {
		return ""
	}
	unlock()
	return target.title
}

// notifyAssetContext 返回资产名（无关联为空）与该资产测试账号的 secret 明文（用于从消息与图片中遮盖）。
func (c *Capability) notifyAssetContext(ctx context.Context, taskID string) (string, []string, error) {
	assetID, err := c.store.taskAssetID(ctx, taskID)
	if err != nil || assetID == "" {
		return "", nil, err
	}
	asset, err := findOptional[assetModel](ctx, c.store.orm, "id = ?", assetID)
	if err != nil || asset == nil {
		return "", nil, err
	}
	secrets, err := c.assetSecrets(ctx, assetID)
	if err != nil {
		return "", nil, err
	}
	return asset.Name, secrets, nil
}

// assetSecrets 返回资产测试账号 secret 明文（≥4 字，用于遮盖）。
func (c *Capability) assetSecrets(ctx context.Context, assetID string) ([]string, error) {
	credentials, err := c.store.assetCredentials(ctx, assetID)
	if err != nil {
		return nil, err
	}
	secrets := []string{}
	for _, item := range credentials[assetID] {
		if item.SecretCipher == "" {
			continue
		}
		aead, err := c.secrets.load(false)
		if err != nil {
			return nil, err
		}
		secret, err := openSecret(aead, assetID, item.ID, item.SecretCipher)
		if err != nil {
			return nil, err
		}
		if utf8.RuneCountInString(secret) >= notifyMinSecretRunes {
			secrets = append(secrets, secret)
		}
	}
	return secrets, nil
}

// notifyEvidence：验证结果第一条 supporting 证据，没有则候选（验证固定的修订）第一条 supporting 证据。
// 返回快照（位置）与可读取的证据行（图片；读不到时为 nil）。
func (c *Capability) notifyEvidence(ctx context.Context, taskID string, groups ...[]FindingEvidenceSnapshot) (*FindingEvidenceSnapshot, *requestModel) {
	for _, group := range groups {
		for index := range group {
			if group[index].Relation != "supporting" {
				continue
			}
			model, err := c.store.evidenceByID(ctx, taskID, group[index].EvidenceID)
			if err != nil {
				model = nil
			}
			return &group[index], model
		}
	}
	return nil, nil
}

// notifyReproEvidence：结果带最小复现时返回第 1 步证据行与高亮片段（请求在前、去重、最多 5 个）；读不到返回 nil。
func (c *Capability) notifyReproEvidence(ctx context.Context, taskID string, payload verificationToolPayload) (*requestModel, []string) {
	if !payload.hasRepro() {
		return nil, nil
	}
	step := payload.KeySteps[0]
	model, err := c.store.evidenceByID(ctx, taskID, step.EvidenceID)
	if err != nil || model == nil {
		return nil, nil
	}
	marks, seen := []string{}, map[string]bool{}
	for _, mark := range append(append([]string{}, step.RequestMarks...), step.ResponseMarks...) {
		if len(marks) == evidenceMaxMarks {
			break
		}
		if !seen[mark] {
			seen[mark] = true
			marks = append(marks, mark)
		}
	}
	return model, marks
}

// notifyEvidenceImage 渲染（mask 开、测试账号 secret 替换、可选 mark 高亮）；超过上限时降到 30 行重渲，仍超不发图。
func (c *Capability) notifyEvidenceImage(model requestModel, secrets []string, marks []string) ([]byte, string) {
	var flow traffic.Flow
	if err := proto.Unmarshal(model.FlowPB, &flow); err != nil {
		return nil, notifyImageFailed
	}
	scrubFlow(&flow, secrets)
	limit := c.notify.imageLimit
	if limit <= 0 {
		limit = wecomImageLimit
	}
	meta := requestEvidenceImageMeta(model)
	for _, lines := range []int{0, notifyImageRetryLines} {
		png, err := renderEvidenceImage(&flow, meta, evidenceImageOptions{MaxLines: lines, Marks: marks})
		if err != nil {
			return nil, notifyImageFailed
		}
		if len(png) <= limit {
			return png, ""
		}
	}
	return nil, notifyImageTooLarge
}

func scrubFlow(flow *traffic.Flow, secrets []string) {
	if len(secrets) == 0 {
		return
	}
	scrubHeaders := func(headers []*traffic.Header) {
		for _, header := range headers {
			header.Value = scrubText(header.Value, secrets)
		}
	}
	if request := flow.GetRequest(); request != nil {
		request.Url = scrubText(request.Url, secrets)
		scrubHeaders(request.Headers)
		request.Body = scrubBytes(request.Body, secrets)
	}
	if response := flow.GetResponse(); response != nil {
		scrubHeaders(response.Headers)
		response.Body = scrubBytes(response.Body, secrets)
	}
}

func scrubText(text string, secrets []string) string {
	for _, secret := range secrets {
		text = strings.ReplaceAll(text, secret, notifySecretMask)
	}
	return text
}

func scrubBytes(body []byte, secrets []string) []byte {
	for _, secret := range secrets {
		body = bytes.ReplaceAll(body, []byte(secret), []byte(notifySecretMask))
	}
	return body
}

func scrubNotifyFields(fields notifyFields, secrets []string) notifyFields {
	if len(secrets) == 0 {
		return fields
	}
	for _, field := range []*string{&fields.Title, &fields.Asset, &fields.Location, &fields.Danger} {
		*field = scrubText(*field, secrets)
	}
	for index := range fields.Steps {
		fields.Steps[index] = scrubText(fields.Steps[index], secrets)
	}
	return fields
}

func stripURLUserinfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

func (c *Capability) notifyLocalTime(raw string) string {
	at := parseAt(raw)
	if at.IsZero() {
		return raw
	}
	return at.In(c.scheduleLocation()).Format("2006-01-02 15:04")
}

// firstSentence 取首句（含句末标点；换行处截止）。
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	for index, r := range text {
		switch r {
		case '\n', '\r':
			return strings.TrimSpace(text[:index])
		case '。', '！', '？', '!', '?':
			return text[:index+utf8.RuneLen(r)]
		case '.':
			rest := text[index+1:]
			if rest == "" || rest[0] == ' ' || rest[0] == '\t' || rest[0] == '\n' || rest[0] == '\r' {
				return text[:index+1]
			}
		}
	}
	return text
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// capRunes 截到最多 limit 字（含末尾 “…”）。
func capRunes(text string, limit int) string {
	if limit < 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	if limit == 0 {
		return ""
	}
	runes := []rune(text)
	return string(runes[:limit-1]) + "…"
}

// composeNotifyMarkdown 生成企业微信 markdown；超过 4096 字节时对标题与链接以外的字段统一按字数上限截断。
func composeNotifyMarkdown(fields notifyFields) string {
	content := renderNotifyMarkdown(fields, -1)
	if len(content) <= wecomMarkdownLimit {
		return content
	}
	low, high := 0, 0
	for _, value := range append([]string{fields.Asset, fields.Location, fields.Danger}, fields.Steps...) {
		high = max(high, utf8.RuneCountInString(value))
	}
	for low < high { // 最大的满足长度限制的上限
		mid := (low + high + 1) / 2
		if len(renderNotifyMarkdown(fields, mid)) <= wecomMarkdownLimit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	content = renderNotifyMarkdown(fields, low)
	if len(content) > wecomMarkdownLimit { // 只有标题/链接也超限时兜底（实际不会发生：标题 ≤256 字节、base_url ≤512 字节）
		cut := wecomMarkdownLimit - len("…")
		for cut > 0 && !utf8.RuneStart(content[cut]) {
			cut--
		}
		content = content[:cut] + "…"
	}
	return content
}

func renderNotifyMarkdown(fields notifyFields, limit int) string {
	label := "【新确认漏洞】"
	switch {
	case fields.Manual:
		label = "【漏洞通报】"
	case fields.Regression:
		label = "【漏洞复现】"
	}
	if fields.Severity != "" {
		label += "【" + fields.Severity + "】"
	}
	asset := fields.Asset
	if asset == "" {
		asset = "未关联资产"
	}
	lines := []string{
		"**" + label + oneLine(fields.Title) + "**",
		"> 资产：" + capRunes(oneLine(asset), limit),
		"> 位置：" + capRunes(oneLine(fields.Location), limit),
		"> 判读：独立验证支持（" + fields.VerifiedAt + "）",
	}
	dangerLabel, stepsLabel := "危害：", "复现要点："
	if fields.Repro {
		dangerLabel, stepsLabel = "影响：", "关键复现："
	}
	if danger := capRunes(fields.Danger, limit); danger != "" {
		lines = append(lines, dangerLabel+danger)
	}
	if len(fields.Steps) > 0 {
		lines = append(lines, stepsLabel)
		for index, step := range fields.Steps {
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, capRunes(step, limit)))
		}
	}
	if fields.Link != "" {
		lines = append(lines, "[在平台查看]("+fields.Link+")")
	}
	if fields.Note != "" {
		lines = append(lines, fields.Note)
	}
	return strings.Join(lines, "\n")
}
