package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	coretool "github.com/chainreactors/cyber/core/tool"
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

// opsRegistration 只在进程内保留；SQL/密钥读取失败均重试，不把原始请求写入日志或磁盘。
// nextAttempt 用真实时钟，避免调度测试时钟或时区影响退避。
type opsRegistration struct {
	key         string
	valid       func() bool
	register    func(context.Context) error
	nextAttempt time.Time
}

func (c *Capability) opsWakeup() <-chan struct{} {
	c.notify.opsMu.Lock()
	defer c.notify.opsMu.Unlock()
	if c.notify.opsWake == nil {
		c.notify.opsWake = make(chan struct{}, 1)
	}
	return c.notify.opsWake
}

// enqueueOps 不做数据库 I/O；审批 PENDING 事件不再等待 SQLite 写锁。
func (c *Capability) enqueueOps(key string, valid func() bool, register func(context.Context) error) {
	c.notify.opsMu.Lock()
	defer c.notify.opsMu.Unlock()
	if c.notify.opsPending == nil {
		c.notify.opsPending = make(map[string]*opsRegistration)
	}
	if c.notify.opsPending[key] != nil {
		return
	}
	c.notify.opsPending[key] = &opsRegistration{key: key, valid: valid, register: register}
	if c.notify.opsWake == nil {
		c.notify.opsWake = make(chan struct{}, 1)
	}
	select {
	case c.notify.opsWake <- struct{}{}:
	default:
	}
}

// retryOpsRegistrations 与投递共用生命周期：每次尝试设置 10s 超时，失败至少间隔 10s，
// 保留到成功（或开关已关闭）；审批已决则丢弃并记日志。数据库唯一键负责最终去重。
func (c *Capability) retryOpsRegistrations(ctx context.Context) {
	c.notify.opsRunMu.Lock()
	defer c.notify.opsRunMu.Unlock()
	c.notify.opsMu.Lock()
	jobs := make([]*opsRegistration, 0, len(c.notify.opsPending))
	for _, job := range c.notify.opsPending {
		jobs = append(jobs, job)
	}
	c.notify.opsMu.Unlock()
	for _, job := range jobs {
		if ctx.Err() != nil {
			return
		}
		valid := job.valid == nil || job.valid()
		if valid && time.Now().Before(job.nextAttempt) {
			continue
		}
		var err error
		if valid {
			attempt, cancel := context.WithTimeout(ctx, opsRegisterWait)
			err = job.register(attempt)
			cancel()
			valid = job.valid == nil || job.valid()
		}
		if !valid {
			log.Printf("webapi 推送：审批已决，放弃待登记通知 %s", job.key)
		} else if err != nil {
			job.nextAttempt = time.Now().Add(notifyInterval)
			if ctx.Err() == nil {
				log.Printf("webapi 推送：登记 %s 失败，保留并重试：%v", job.key, err)
			}
			continue
		}
		c.notify.opsMu.Lock()
		delete(c.notify.opsPending, job.key)
		c.notify.opsMu.Unlock()
	}
}

func (c *Capability) logPendingOpsOnStop() {
	c.notify.opsMu.Lock()
	defer c.notify.opsMu.Unlock()
	if n := len(c.notify.opsPending); n > 0 {
		log.Printf("webapi 推送：停止时尚有 %d 项运营通知待登记（进程内队列，重启不保留）", n)
	}
}

// ---- approval_pending ----

// notifyApprovalPending 在审批挂起后只入队；由投递 goroutine 登记与重试。
func (c *Capability) notifyApprovalPending(_ context.Context, task *Task, tool, arguments string, review *guardrail.Review) {
	id := review.GetOperation().GetOperationId()
	if id == "" {
		return
	}
	key := notifyKeyApproval + id
	c.enqueueOps(key, func() bool { return c.approvalStillPending(key) }, func(ctx context.Context) error {
		// 继承审批的取消信号，解决时中断正在等待写锁的登记；不继承原请求的 deadline。
		c.approvals.mu.Lock()
		pending := c.approvals.pending[id]
		c.approvals.mu.Unlock()
		if pending == nil {
			return nil
		}
		ctx, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(pending.ctx, cancel)
		defer stop()
		defer cancel()
		return c.registerApprovalPending(ctx, task, tool, arguments, review)
	})
}

func (c *Capability) registerApprovalPending(ctx context.Context, task *Task, tool, arguments string, review *guardrail.Review) error {
	ops, err := c.opsSettings(ctx)
	if err != nil || ops == nil || !ops.NotifyApproval {
		return err
	}
	operationID := review.GetOperation().GetOperationId()
	if operationID == "" {
		return nil
	}
	sessionID := review.GetSessionId()
	if sub, err := c.store.subsessionFor(ctx, sessionID); err != nil {
		return err
	} else if sub != nil && sub.ParentSessionID != "" {
		sessionID = sub.ParentSessionID // 子 Agent 的审批经父研究会话处理
	}
	taskID, assetID, title := "", "", "未关联任务"
	resolver := &credentialResolver{byID: map[string]resolvedCredential{}}
	if task != nil {
		taskID = task.ID
		if title = usageTitle(task.Objective); title == "" {
			title = "任务 " + task.ID
		}
		if assetID, err = c.store.taskAssetID(ctx, task.ID); err != nil {
			return err
		}
		// 资产口令明文回替为占位符；读不到密钥时不登记（宁可不发，也不冒险外发明文）。
		if resolver, err = loadCredentialResolver(ctx, c.store, c.secrets, task.ID); err != nil {
			return err
		}
	}
	scrub := func(text string) string { return guardrail.RedactText(resolver.redact(text)) }
	minutes := int((c.approvals.timeout + time.Minute - 1) / time.Minute)
	payload := opsPayload{TaskTitle: scrub(title), Tool: tool, Request: opsRequestSummary(tool, arguments, scrub), Reason: scrub(review.GetDecision().GetReason()),
		TimeoutMinutes: minutes}
	if expires := review.GetExpiresAt(); expires != nil {
		payload.ExpiresAt = expires.AsTime().UTC().Format(time.RFC3339)
	}
	_, err = c.registerOps(ctx, NotifyKindApprovalPending, notifyKeyApproval+operationID, taskID,
		&notificationOpsModel{Title: payload.TaskTitle, AssetID: assetID, SessionID: sessionID}, payload)
	return err
}

// opsRequestSummary：webapi_http 为 方法+URL（去 userinfo），bash 为命令首 120 字，write 为路径；均经 scrub。
func opsRequestSummary(tool, arguments string, scrub func(string) string) string {
	switch tool {
	case "webapi_http":
		if args, err := coretool.ParseArgs[httpToolArguments](arguments); err == nil {
			return strings.ToUpper(args.Method) + " " + scrub(stripURLUserinfo(args.URL))
		}
	case "bash":
		if args, err := coretool.ParseArgs[struct {
			Command string `json:"command"`
		}](arguments); err == nil {
			return capRunes(oneLine(scrub(args.Command)), opsRequestRunes)
		}
	case "write":
		if args, err := coretool.ParseArgs[struct {
			Path string `json:"path"`
		}](arguments); err == nil {
			return scrub(args.Path)
		}
	}
	return ""
}

// notifyApprovalResolved：审批已决（批准/拒绝/超时/取消）后，仍未发出的待审批通知改为 dismissed。
func (c *Capability) notifyApprovalResolved(ctx context.Context, operationID string) {
	if operationID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), opsRegisterWait)
	defer cancel()
	key := notifyKeyApproval + operationID
	dismiss := func(ctx context.Context) error {
		return c.store.dismissUnsentNotifications(ctx, key, NotifyKindApprovalPending)
	}
	if err := dismiss(ctx); err != nil {
		log.Printf("webapi 推送：撤销已决审批的通知失败，保留并重试：%v", err)
		c.enqueueOps("dismiss:"+key, nil, dismiss)
	}
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

func (c *Capability) notifyScheduleFailed(_ context.Context, assetID, scheduledFor, cron string) {
	// 读取 run_id 本身也可能失败，所以入队键先用调度的唯一键。
	c.enqueueOps(notifyKeyScheduleRun+assetID+"/"+scheduledFor, nil, func(ctx context.Context) error {
		return c.registerScheduleFailed(ctx, assetID, scheduledFor, cron)
	})
}

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
	rows, err := c.store.db.QueryContext(ctx, `SELECT id, name FROM webapi_assets WHERE archived = 0 ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	order := []string{}
	byAsset := map[string]*digestRow{}
	for rows.Next() {
		row := &digestRow{}
		if err := rows.Scan(&row.AssetID, &row.Name); err != nil {
			rows.Close()
			return nil, err
		}
		order = append(order, row.AssetID)
		byAsset[row.AssetID] = row
	}
	if err := closeRows(rows); err != nil {
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
