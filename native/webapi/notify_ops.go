package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chainreactors/cyber/exts/guardrail"
)

// 运营通知（V4）：登记、内容与 markdown。投递沿用 D2（deliverNotifications）。契约见 native/NOTIFY.md「运营通知」。

const (
	opsRequestRunes   = 120 // bash 命令摘要首 120 字
	opsTitleRunes     = 120
	opsDigestRowRunes = 40
	opsRegisterWait   = 10 * time.Second
	digestHour        = 9
)

// opsPayload 是登记时冻结的运营通知内容（按 kind 只填相应字段）。
type opsPayload struct {
	// approval_pending
	TaskTitle      string `json:"task_title,omitempty"`
	Tool           string `json:"tool,omitempty"`
	Request        string `json:"request,omitempty"` // 请求摘要，已回替资产口令并经 guardrail 遮盖
	Reason         string `json:"reason,omitempty"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	TimeoutMinutes int    `json:"timeout_minutes,omitempty"`
	// schedule_failed
	AssetName    string `json:"asset_name,omitempty"`
	Cron         string `json:"cron,omitempty"`
	ScheduledFor string `json:"scheduled_for,omitempty"`
	Stage        string `json:"stage,omitempty"`
	Error        string `json:"error,omitempty"`
	// weekly_digest
	Week        string      `json:"week,omitempty"`
	RangeStart  string      `json:"range_start,omitempty"`
	RangeEnd    string      `json:"range_end,omitempty"`
	Assets      []digestRow `json:"assets,omitempty"`
	QuietAssets int         `json:"quiet_assets,omitempty"`
	Total       *digestStat `json:"total,omitempty"`
}

// digestStat：新增 / 已修复 = 区间内总览趋势（confirmed_by_day）的 new / fixed 之和；逾期 / 待定级 = 当前值（总览 vulns.overdue / todo.unrated 口径）。
type digestStat struct {
	New     int `json:"new"`
	Fixed   int `json:"fixed"`
	Overdue int `json:"overdue"`
	Unrated int `json:"unrated"`
}

func (s digestStat) zero() bool { return s == digestStat{} }

type digestRow struct {
	AssetID string     `json:"asset_id"` // 未关联资产的独立候选为 ""
	Name    string     `json:"name"`
	Stat    digestStat `json:"stat"`
}

// opsSettings 返回运营通知可登记时的开关（需 webhook 已配置且 enabled=true）；不可登记返回 nil。
func (c *Capability) opsSettings(ctx context.Context) (*notifyOpsSettingsModel, error) {
	settings, err := c.store.notifySettings(ctx)
	if err != nil || settings == nil || settings.WebhookCipher == "" || !settings.Enabled {
		return nil, err
	}
	return c.store.notifyOpsSettings(ctx)
}

func (c *Capability) registerOps(ctx context.Context, kind, key, taskID string, ops *notificationOpsModel, payload opsPayload) (bool, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	ops.PayloadJSON = string(raw)
	now := notifyTime(c.scheduleClock())
	// require_confirm 不作用于运营通知（时效性）：一律直接 queued、立即到期。
	model := &notificationModel{ID: newNotificationID(), DedupeKey: key, Kind: kind, TaskID: taskID, Status: NotifyQueued, NextAttemptAt: nullNow(now), CreatedAt: now}
	var valid func() bool
	if kind == NotifyKindApprovalPending {
		valid = func() bool { return c.approvalStillPending(key) }
	}
	return c.store.insertOpsNotificationIf(ctx, model, ops, valid)
}

// ---- 待登记（webapi_notify_registrations，R2 起取代进程内队列） ----
//
// 审批挂起与计划运行失败各在自己的业务写事务里插一行待登记（不在 PENDING 事件路径上额外等锁）；
// 投递 goroutine 每轮（及被唤醒时）取到期行登记：成功或放弃即删除，失败 attempts+1、至少 10s 后重试，
// 单次尝试 10s 超时。重启后从表继续；数据库唯一键与登记前的去重检查负责最终“恰好一次”。

// wakeOps 唤醒投递 goroutine 立即处理待登记（非阻塞）。
func (c *Capability) wakeOps() {
	wake := c.opsWakeup()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (c *Capability) opsWakeup() chan struct{} {
	c.notify.opsMu.Lock()
	defer c.notify.opsMu.Unlock()
	if c.notify.opsWake == nil {
		c.notify.opsWake = make(chan struct{}, 1)
	}
	return c.notify.opsWake
}

// scheduleFailedArgs 是 schedule_failed 待登记的参数（不含任何口令）。
type scheduleFailedArgs struct {
	AssetID      string `json:"asset_id"`
	ScheduledFor string `json:"scheduled_for"`
	Cron         string `json:"cron"`
}

// scheduleFailedRegistration：键用调度唯一键（run_id 在登记时按 (asset, scheduled_for) 取）。
func scheduleFailedRegistration(assetID, scheduledFor, cron string) *notifyRegistrationModel {
	raw, _ := json.Marshal(scheduleFailedArgs{AssetID: assetID, ScheduledFor: scheduledFor, Cron: cron})
	now := notifyTime(time.Now())
	return &notifyRegistrationModel{Key: notifyKeyScheduleRun + assetID + "/" + scheduledFor, Kind: NotifyKindScheduleFailed, ArgsJSON: string(raw), NextAttemptAt: now, CreatedAt: now}
}

// retryOpsRegistrations 处理到期待登记（及此前写库失败的审批终态）；与投递共用生命周期。
func (c *Capability) retryOpsRegistrations(ctx context.Context) {
	c.notify.opsRunMu.Lock()
	defer c.notify.opsRunMu.Unlock()
	c.retryApprovalBacklog(ctx)
	jobs, err := c.store.dueRegistrations(ctx, notifyTime(time.Now()), notifyBatch)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("webapi 推送：读取待登记失败：%v", err)
		}
		return
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, opsRegisterWait)
		err := c.runRegistration(attempt, job)
		cancel()
		if ctx.Err() != nil {
			return // 停止打断：保留、不计次
		}
		if err == nil {
			if err := c.store.deleteRegistration(ctx, job.Key); err != nil && ctx.Err() == nil {
				log.Printf("webapi 推送：删除已完成的待登记 %s 失败（重试时按去重键跳过）：%v", job.Key, err)
			}
			continue
		}
		log.Printf("webapi 推送：登记 %s 失败，保留并重试：%v", job.Key, err)
		if err := c.store.deferRegistration(ctx, job.Key, notifyTime(time.Now().Add(notifyInterval)), capRunes(oneLine(err.Error()), 300)); err != nil && ctx.Err() == nil {
			log.Printf("webapi 推送：记录待登记 %s 失败次数失败：%v", job.Key, err)
		}
	}
}

func (c *Capability) runRegistration(ctx context.Context, job notifyRegistrationModel) error {
	switch job.Kind {
	case NotifyKindApprovalPending:
		return c.registerApprovalPending(ctx, job.ApprovalID)
	case NotifyKindScheduleFailed:
		var args scheduleFailedArgs
		if err := json.Unmarshal([]byte(job.ArgsJSON), &args); err != nil {
			return fmt.Errorf("待登记参数无法解析：%w", err)
		}
		return c.registerScheduleFailed(ctx, args.AssetID, args.ScheduledFor, args.Cron)
	default:
		return fmt.Errorf("未知待登记类型 %q", job.Kind)
	}
}

// tickWeeklyDigest：每周摘要按“最近一期未登记即登记”从库状态重算，不需要待登记行；失败至少间隔 10s 再试。
func (c *Capability) tickWeeklyDigest(ctx context.Context) {
	c.notify.opsMu.Lock()
	wait := time.Now().Before(c.notify.digestNext)
	c.notify.opsMu.Unlock()
	if wait {
		return
	}
	attempt, cancel := context.WithTimeout(ctx, opsRegisterWait)
	err := c.registerWeeklyDigest(attempt)
	cancel()
	next := time.Time{}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Printf("webapi 推送：登记每周摘要失败，稍后重试：%v", err)
		next = time.Now().Add(notifyInterval)
	}
	c.notify.opsMu.Lock()
	c.notify.digestNext = next
	c.notify.opsMu.Unlock()
}

// ---- approval_pending ----

// registerApprovalPending 按审批记录登记待审批通知；审批已决/过期/取消（或已不在本进程挂起）则放弃。
func (c *Capability) registerApprovalPending(ctx context.Context, approvalID string) error {
	record, err := c.store.approval(ctx, approvalID)
	if err != nil {
		return err
	}
	if record == nil {
		log.Printf("webapi 推送：审批记录 %s 不存在，放弃待登记通知", approvalID)
		return nil
	}
	key := notifyKeyApproval + record.OperationID
	if record.Status != approvalStatusPending || !c.approvalStillPending(key) {
		log.Printf("webapi 推送：审批已决，放弃待登记通知 %s", key)
		return nil
	}
	// 继承审批的取消信号，解决时中断正在等待写锁的登记；不继承原请求的 deadline。
	c.approvals.mu.Lock()
	pending := c.approvals.pending[record.OperationID]
	c.approvals.mu.Unlock()
	if pending != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		stop := context.AfterFunc(pending.ctx, cancel)
		defer stop()
		defer cancel()
	}
	ops, err := c.opsSettings(ctx)
	if err != nil || ops == nil || !ops.NotifyApproval {
		return err
	}
	sessionID := record.SessionID
	if record.ParentSessionID != "" {
		sessionID = record.ParentSessionID // 子 Agent 的审批经父研究会话处理
	}
	assetID, title := "", "未关联任务"
	resolver := &credentialResolver{byID: map[string]resolvedCredential{}}
	if record.TaskID != "" {
		task, err := c.store.Get(ctx, record.TaskID)
		if err != nil {
			return err
		}
		if title = "任务 " + record.TaskID; task != nil {
			if t := usageTitle(task.Objective); t != "" {
				title = t
			}
		}
		if assetID, err = c.store.taskAssetID(ctx, record.TaskID); err != nil {
			return err
		}
		// 资产口令明文回替为占位符；读不到密钥时不登记（宁可不发，也不冒险外发明文）。
		if resolver, err = loadCredentialResolver(ctx, c.store, c.secrets, record.TaskID); err != nil {
			return err
		}
	}
	scrub := func(text string) string { return guardrail.RedactText(resolver.redact(text)) }
	request := record.Summary
	if record.Tool == "bash" {
		request = capRunes(request, opsRequestRunes)
	}
	minutes := int((c.approvals.timeout + time.Minute - 1) / time.Minute)
	payload := opsPayload{TaskTitle: scrub(title), Tool: record.Tool, Request: request, Reason: record.Reason, TimeoutMinutes: minutes}
	if expires, err := time.Parse(notifyTimeLayout, record.ExpiresAt); err == nil {
		payload.ExpiresAt = expires.UTC().Format(time.RFC3339)
	}
	_, err = c.registerOps(ctx, NotifyKindApprovalPending, key, record.TaskID,
		&notificationOpsModel{Title: payload.TaskTitle, AssetID: assetID, SessionID: sessionID}, payload)
	return err
}

// approvalStillPending：去重键对应的审批仍在本进程挂起。Hub 重启后挂起丢失 → false。
func (c *Capability) approvalStillPending(key string) bool {
	operationID := opsKeySuffix(key, notifyKeyApproval)
	c.approvals.mu.Lock()
	defer c.approvals.mu.Unlock()
	p := c.approvals.pending[operationID]
	return p != nil && p.ctx.Err() == nil && time.Now().Before(p.review.ExpiresAt.AsTime())
}

// ---- schedule_failed ----

// launchStagePattern：LaunchAssetTask 失败文案“阶段：文案”中的阶段标识（如 select_node）。
var launchStagePattern = regexp.MustCompile(`^([a-z_]+)：`)

func (c *Capability) registerScheduleFailed(ctx context.Context, assetID, scheduledFor, cron string) error {
	ops, err := c.opsSettings(ctx)
	if err != nil || ops == nil || !ops.NotifyScheduleFailed {
		return err
	}
	// 记录冲突未插入时按 (asset, scheduled_for) 取已有那条，去重键用其 id。
	run, err := c.store.scheduleRunAt(ctx, assetID, scheduledFor)
	if err != nil || run == nil || run.Status != ScheduleRunFailed {
		return err
	}
	key := notifyKeyScheduleRun + run.ID
	if exists, err := c.store.notificationKeyExists(ctx, key); err != nil || exists {
		return err
	}
	secrets, err := c.assetSecrets(ctx, assetID)
	if err != nil {
		secrets = nil // 资产已删除或读不到账号时仍登记；失败文案本身不含账号
	}
	name := c.assetName(ctx, assetID)
	if name == "" {
		name = "资产 " + assetID
	}
	stage := "调度检查"
	if match := launchStagePattern.FindStringSubmatch(run.Error); match != nil {
		stage = match[1]
	}
	payload := opsPayload{AssetName: name, Cron: cron, ScheduledFor: run.ScheduledFor, Stage: stage, Error: scrubText(run.Error, secrets)}
	_, err = c.registerOps(ctx, NotifyKindScheduleFailed, key, run.TaskID, &notificationOpsModel{Title: name, AssetID: assetID}, payload)
	return err
}

// ---- weekly_digest ----

// digestSlot 返回 at（Hub 本地时区）之前（含）最近一次周一 09:00。
func digestSlot(at time.Time, loc *time.Location) time.Time {
	local := at.In(loc)
	slot := time.Date(local.Year(), local.Month(), local.Day(), digestHour, 0, 0, 0, loc)
	for slot.Weekday() != time.Monday || slot.After(local) {
		slot = time.Date(slot.Year(), slot.Month(), slot.Day()-1, digestHour, 0, 0, 0, loc)
	}
	return slot
}

func digestKey(slot time.Time) string {
	year, week := slot.ISOWeek()
	return fmt.Sprintf("%s%04d-W%02d", notifyKeyDigest, year, week)
}

// registerWeeklyDigest 由推送 goroutine 每轮调用：最近一次周一 09:00 尚未登记即登记一条（停机错过多周只补最近一次）。
// 开关由关变开之前的时间点不补发。
func (c *Capability) registerWeeklyDigest(ctx context.Context) error {
	ops, err := c.opsSettings(ctx)
	if err != nil || ops == nil || !ops.NotifyWeeklyDigest {
		return err
	}
	slot := digestSlot(c.scheduleClock(), c.scheduleLocation())
	if since, err := time.Parse(notifyTimeLayout, ops.WeeklyDigestSince); err == nil && slot.Before(since) {
		return nil
	}
	key := digestKey(slot)
	if exists, err := c.store.notificationKeyExists(ctx, key); err != nil || exists {
		return err
	}
	payload, err := c.weeklyDigestPayload(ctx, slot)
	if err != nil {
		return err
	}
	_, err = c.registerOps(ctx, NotifyKindWeeklyDigest, key, "", &notificationOpsModel{Title: "每周摘要 " + payload.Week}, *payload)
	return err
}

// weeklyDigestPayload 按资产汇总：区间 = 总览 days=7 的区间（slot 所在本地日往前 7 个本地日，含当日）；
// 新增/已修复用 vulnPeriodMarks（总览趋势同一函数），逾期/待定级用 vulnTodo（总览待办同一函数，当前值）。
func (c *Capability) weeklyDigestPayload(ctx context.Context, slot time.Time) (*opsPayload, error) {
	start, end, dates := overviewRange(slot, 7)
	vulns, err := c.globalVulns(ctx, "")
	if err != nil {
		return nil, err
	}
	triage, err := c.loadTriageContext(ctx)
	if err != nil {
		return nil, err
	}
	order := []string{}
	byAsset := map[string]*digestRow{}
	if err := c.store.queryRows(ctx, func(rows *sql.Rows) error {
		row := &digestRow{}
		if err := rows.Scan(&row.AssetID, &row.Name); err != nil {
			return err
		}
		order = append(order, row.AssetID)
		byAsset[row.AssetID] = row
		return nil
	}, `SELECT id, name FROM webapi_assets WHERE NOT archived ORDER BY name COLLATE "C", id COLLATE "C"`); err != nil {
		return nil, err
	}
	standalone := &digestRow{Name: "未关联资产"}
	total := digestStat{}
	for _, vuln := range vulns {
		row := standalone
		if vuln.Asset != nil && byAsset[vuln.Asset.ID] != nil {
			row = byAsset[vuln.Asset.ID]
		}
		marked := map[string]bool{}
		vulnPeriodMarks(vuln, start, end, func(date, kind string) {
			if kind != VulnNew && kind != VulnFixed || marked[date+kind] {
				return
			}
			marked[date+kind] = true
			if kind == VulnNew {
				row.Stat.New++
				total.New++
			} else {
				row.Stat.Fixed++
				total.Fixed++
			}
		})
		item := triage.view(vuln.ID, vuln.assetID, vuln.Status, vuln.firstConfirmed)
		if _, unrated, overdue := vulnTodo(vuln.Status, item); unrated || overdue {
			if unrated {
				row.Stat.Unrated++
				total.Unrated++
			}
			if overdue {
				row.Stat.Overdue++
				total.Overdue++
			}
		}
	}
	year, week := slot.ISOWeek()
	payload := &opsPayload{Week: fmt.Sprintf("%04d-W%02d", year, week), RangeStart: dates[0], RangeEnd: dates[len(dates)-1], Total: &total, Assets: []digestRow{}}
	for _, id := range order {
		if row := byAsset[id]; !row.Stat.zero() {
			payload.Assets = append(payload.Assets, *row)
		} else {
			payload.QuietAssets++
		}
	}
	if !standalone.Stat.zero() {
		payload.Assets = append(payload.Assets, *standalone)
	}
	return payload, nil
}

// ---- markdown ----

func (c *Capability) buildOpsMessage(ctx context.Context, item notificationModel, baseURL string) (*notifyMessage, error) {
	ops, err := c.store.notificationOps(ctx, item.ID)
	if err != nil || ops == nil {
		return nil, errors.Join(errors.New("运营通知内容不存在"), err)
	}
	var payload opsPayload
	if err := json.Unmarshal([]byte(ops.PayloadJSON), &payload); err != nil {
		return nil, errors.New("运营通知内容无法解析")
	}
	link := func(path string) string {
		if baseURL == "" {
			return ""
		}
		return baseURL + path
	}
	var markdown string
	switch item.Kind {
	case NotifyKindApprovalPending:
		label := map[string]string{"bash": "命令：", "write": "文件："}[payload.Tool]
		if label == "" {
			label = "请求："
		}
		remain := fmt.Sprintf("剩余时间：%d 分钟内未处理将自动拒绝", payload.TimeoutMinutes)
		if payload.ExpiresAt != "" {
			remain += "（" + c.notifyLocalTime(payload.ExpiresAt) + " 截止）"
		}
		tail := []string{}
		if href := link("/sessions/" + ops.SessionID); href != "" && ops.SessionID != "" {
			tail = append(tail, "[去平台审批]("+href+")")
		}
		markdown = composeOpsMarkdown("**【待审批】"+capRunes(oneLine(payload.TaskTitle), opsTitleRunes)+"**",
			[]opsField{{"> 工具：", payload.Tool}, {"> " + label, payload.Request}, {"> 原因：", payload.Reason}, {"> ", remain}}, tail)
	case NotifyKindScheduleFailed:
		tail := []string{}
		if href := link("/?page=assets"); href != "" {
			tail = append(tail, "[在平台查看]("+href+")")
		}
		scheduled := payload.ScheduledFor
		if at, err := parseScheduleTime(scheduled); err == nil {
			scheduled = at.In(c.scheduleLocation()).Format("2006-01-02 15:04")
		}
		markdown = composeOpsMarkdown("**【计划运行失败】"+capRunes(oneLine(payload.AssetName), opsTitleRunes)+"**",
			[]opsField{{"> 计划：", payload.Cron + "（Hub 本地时区）"}, {"> 应运行时间：", scheduled}, {"> 失败阶段：", payload.Stage}, {"> 原因：", payload.Error}}, tail)
	case NotifyKindWeeklyDigest:
		markdown = composeDigestMarkdown(payload, link("/?page=overview"))
	default:
		return nil, errors.New("未知运营通知类型")
	}
	return &notifyMessage{markdown: markdown}, nil
}

type opsField struct{ label, value string }

// composeOpsMarkdown：标题行 + 字段行 + 尾行（链接）；超过 4096 字节时对字段值统一取最大可行字数上限截断（同漏洞通知规则），标题与尾行保留。
func composeOpsMarkdown(title string, fields []opsField, tail []string) string {
	render := func(limit int) string {
		lines := []string{title}
		for _, field := range fields {
			if value := capRunes(oneLine(field.value), limit); value != "" {
				lines = append(lines, field.label+value)
			}
		}
		return strings.Join(append(lines, tail...), "\n")
	}
	content := render(-1)
	if len(content) <= wecomMarkdownLimit {
		return content
	}
	low, high := 0, 0
	for _, field := range fields {
		high = max(high, utf8.RuneCountInString(oneLine(field.value)))
	}
	for low < high {
		mid := (low + high + 1) / 2
		if len(render(mid)) <= wecomMarkdownLimit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return capBytes(render(low))
}

func digestLine(stat digestStat) string {
	return fmt.Sprintf("新增 %d · 已修复 %d · 当前逾期 %d · 待定级 %d", stat.New, stat.Fixed, stat.Overdue, stat.Unrated)
}

// composeDigestMarkdown：超过 4096 字节时从末尾起省略资产行，并注明未列出的资产数。
func composeDigestMarkdown(payload opsPayload, link string) string {
	total := digestStat{}
	if payload.Total != nil {
		total = *payload.Total
	}
	render := func(rows int) string {
		lines := []string{
			"**【每周摘要】" + payload.Week + "**",
			"> 统计区间：" + payload.RangeStart + " ~ " + payload.RangeEnd + "（Hub 本地日期；逾期/待定级为当前值）",
			"> 合计：" + digestLine(total),
		}
		for _, row := range payload.Assets[:rows] {
			lines = append(lines, "- "+capRunes(oneLine(row.Name), opsDigestRowRunes)+"："+digestLine(row.Stat))
		}
		if omitted := len(payload.Assets) - rows; omitted > 0 {
			lines = append(lines, fmt.Sprintf("另有 %d 个资产有变化未列出", omitted))
		}
		if payload.QuietAssets > 0 {
			lines = append(lines, fmt.Sprintf("其余 %d 个资产均为 0", payload.QuietAssets))
		}
		if len(payload.Assets) == 0 && payload.QuietAssets == 0 {
			lines = append(lines, "暂无资产")
		}
		if link != "" {
			lines = append(lines, "[在平台查看]("+link+")")
		}
		return strings.Join(lines, "\n")
	}
	rows := len(payload.Assets)
	content := render(rows)
	for len(content) > wecomMarkdownLimit && rows > 0 {
		rows--
		content = render(rows)
	}
	return capBytes(content)
}

// capBytes 兜底：仍超 4096 字节时按字节截断并以 … 结尾（实际不会发生）。
func capBytes(content string) string {
	if len(content) <= wecomMarkdownLimit {
		return content
	}
	cut := wecomMarkdownLimit - len("…")
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut] + "…"
}
