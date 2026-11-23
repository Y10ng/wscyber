package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/exts/guardrail"
	"github.com/uptrace/bun"
)

// 审计记录（B3）：平台自己的写操作实际生效后追加一行；只追加，无更新/删除代码路径。契约见 native/AUDIT.md。

const (
	auditActorUser   = "user"   // 经原生鉴权的 HTTP 请求（单访问密钥，无用户名）
	auditActorSystem = "system" // 调度器、推送 goroutine、审批超时

	auditCommandRunes = 200
	auditSummaryRunes = 300
	auditActionMax    = 64
)

type auditEventModel struct {
	bun.BaseModel `bun:"table:webapi_audit_events,alias:webapi_audit_event"`

	ID         string `bun:"id,pk"`
	At         string `bun:"at,notnull"` // 定宽 UTC 毫秒（notifyTimeLayout）
	Actor      string `bun:"actor,notnull"`
	Action     string `bun:"action,notnull"`
	TargetKind string `bun:"target_kind,notnull"`
	TargetID   string `bun:"target_id,notnull"`
	TaskID     string `bun:"task_id,notnull"`
	AssetID    string `bun:"asset_id,notnull"`
	Summary    string `bun:"summary,type:text,notnull"`
	DetailJSON string `bun:"detail_json,type:text,notnull"`
}

type auditActorKey struct{}

// withAuditActor 标记后续登记的操作者（调度器、计划后续用 system）；未标记为 user（HTTP 请求）。
func withAuditActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, auditActorKey{}, actor)
}

func auditActorOf(ctx context.Context) string {
	if actor, ok := ctx.Value(auditActorKey{}).(string); ok && actor != "" {
		return actor
	}
	return auditActorUser
}

// auditEntry 是一次登记；ID 为空时生成（同一时间内按生成先后递增）。
type auditEntry struct {
	ID         string
	Actor      string
	Action     string
	TargetKind string
	TargetID   string
	TaskID     string
	AssetID    string
	Summary    string
	Detail     map[string]any
}

func newAuditID() string {
	return "au_" + time.Now().UTC().Format("20060102T150405.000000000") + "_" + newSessionID()[:8]
}

// appendAudit 只做 INSERT（同 id 已存在则不重复）。
func (s *taskStore) appendAudit(ctx context.Context, entry auditEntry, at time.Time) error {
	if entry.Detail == nil {
		entry.Detail = map[string]any{}
	}
	raw, err := json.Marshal(entry.Detail)
	if err != nil {
		return err
	}
	if entry.ID == "" {
		entry.ID = newAuditID()
	}
	model := auditEventModel{ID: entry.ID, At: notifyTime(at), Actor: entry.Actor, Action: entry.Action, TargetKind: entry.TargetKind, TargetID: entry.TargetID,
		TaskID: entry.TaskID, AssetID: entry.AssetID, Summary: capRunes(oneLine(entry.Summary), auditSummaryRunes), DetailJSON: string(raw)}
	err = s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&model).On("CONFLICT (id) DO NOTHING").Exec(ctx)
		return err
	})
	return err
}

// audit 登记失败只记日志，不影响业务响应；请求取消不丢登记。
func (c *Capability) audit(ctx context.Context, entry auditEntry) {
	if c == nil || c.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := c.store.appendAudit(ctx, entry, c.scheduleClock()); err != nil {
		log.Printf("webapi 审计：登记 %s 失败：%v", entry.Action, err)
	}
}

// ---- 查询 ----

// AuditEvent 是 GET /api/webapi/audit 的项。
type AuditEvent struct {
	ID         string          `json:"id"`
	At         string          `json:"at"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	TargetKind string          `json:"target_kind"`
	TargetID   string          `json:"target_id"`
	TaskID     string          `json:"task_id"`
	AssetID    string          `json:"asset_id"`
	Summary    string          `json:"summary"`
	Detail     json.RawMessage `json:"detail"`
}

type auditFilter struct {
	action           string
	prefix           bool
	actor            string
	taskID, assetID  string
	needle           string
	from, to         string // [from, to) UTC 定宽
	afterAt, afterID string
	limit            int
}

func (s *taskStore) auditEvents(ctx context.Context, filter auditFilter) ([]AuditEvent, error) {
	where, args := []string{"1 = 1"}, []any{}
	add := func(clause string, values ...any) {
		where = append(where, clause)
		args = append(args, values...)
	}
	switch {
	case filter.action != "" && filter.prefix:
		add("substr(action, 1, ?) = ?", len(filter.action), filter.action)
	case filter.action != "":
		add("action = ?", filter.action)
	}
	if filter.actor != "" {
		add("actor = ?", filter.actor)
	}
	if filter.taskID != "" {
		add("task_id = ?", filter.taskID)
	}
	if filter.assetID != "" {
		add("asset_id = ?", filter.assetID)
	}
	if filter.needle != "" {
		add("(strpos(lower(summary), ?) > 0 OR strpos(lower(target_id), ?) > 0)", filter.needle, filter.needle)
	}
	if filter.from != "" {
		add(`at COLLATE "C" >= ?`, filter.from)
	}
	if filter.to != "" {
		add(`at COLLATE "C" < ?`, filter.to)
	}
	if filter.afterAt != "" {
		add(`(at COLLATE "C" < ? OR at = ? AND id COLLATE "C" < ?)`, filter.afterAt, filter.afterAt, filter.afterID)
	}
	query := `SELECT id, at, actor, action, target_kind, target_id, task_id, asset_id, summary, detail_json FROM webapi_audit_events WHERE ` +
		strings.Join(where, " AND ") + ` ORDER BY at COLLATE "C" DESC, id COLLATE "C" DESC LIMIT ?`
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) ([]AuditEvent, error) {
		return scanAuditEvents(db.QueryContext(ctx, query, append(args, filter.limit)...))
	})
}

func scanAuditEvents(rows *sql.Rows, err error) ([]AuditEvent, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var item AuditEvent
		var detail string
		if err := rows.Scan(&item.ID, &item.At, &item.Actor, &item.Action, &item.TargetKind, &item.TargetID, &item.TaskID, &item.AssetID, &item.Summary, &detail); err != nil {
			return nil, err
		}
		item.Detail = json.RawMessage(detail)
		if !json.Valid(item.Detail) {
			item.Detail = json.RawMessage("null")
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

var auditActionPattern = regexp.MustCompile(`^[a-z][a-z_]*(\.[a-z][a-z_]*)*\.?$`)

// handleAuditList: GET /api/webapi/audit?action=&actor=&task_id=&asset_id=&q=&from=&to=&limit=&cursor=
func (c *Capability) handleAuditList(w http.ResponseWriter, r *http.Request) {
	limit, needle, assetID, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := r.URL.Query()
	filter := auditFilter{limit: limit + 1, needle: needle, assetID: assetID, action: query.Get("action"), actor: query.Get("actor"), taskID: query.Get("task_id")}
	if filter.action != "" && (len(filter.action) > auditActionMax || !auditActionPattern.MatchString(filter.action)) {
		writeError(w, http.StatusBadRequest, "action 非法（精确名或以 . 结尾的前缀）")
		return
	}
	filter.prefix = strings.HasSuffix(filter.action, ".")
	if filter.actor != "" && filter.actor != auditActorUser && filter.actor != auditActorSystem {
		writeError(w, http.StatusBadRequest, "actor 只能是 user 或 system")
		return
	}
	if filter.taskID != "" && validPathID(filter.taskID) != nil {
		writeError(w, http.StatusBadRequest, "task_id 非法")
		return
	}
	from, err := parseUsageDay("from", query.Get("from"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := parseUsageDay("to", query.Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		writeError(w, http.StatusBadRequest, "from 不能晚于 to")
		return
	}
	if !from.IsZero() {
		filter.from = notifyTime(from)
	}
	if !to.IsZero() {
		filter.to = notifyTime(to.AddDate(0, 0, 1))
	}
	afterAt, afterID, hasCursor, err := decodeKeyCursor(query.Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if hasCursor {
		filter.afterAt, filter.afterID = notifyTime(afterAt), afterID
	}
	items, err := c.store.auditEvents(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审计记录失败")
		return
	}
	var next any
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
		last := items[len(items)-1]
		next = encodeKeyCursor(last.At, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": hasMore, "next_cursor": next})
}

// ---- 文案辅助 ----

func scopeLabel(scope string) string {
	switch scope {
	case ScopeOrigins:
		return "仅授权 origins"
	case ScopeUnrestricted:
		return "不限制"
	}
	return "未知"
}

func approvalLabel(approval string) string {
	switch approval {
	case ApprovalOff:
		return "不审批"
	case ApprovalHighRisk:
		return "仅高危"
	case ApprovalAll:
		return "全部人工"
	}
	return "未知"
}

func policyLabel(scope, approval string) string {
	return scopeLabel(scope) + "/" + approvalLabel(approval)
}

func policyDetail(scope, approval string) map[string]any {
	return map[string]any{"scope": scope, "approval": approval}
}

func (c *Capability) assetName(ctx context.Context, assetID string) string {
	if assetID == "" {
		return ""
	}
	if asset, err := c.store.assetModel(ctx, assetID); err == nil && asset != nil {
		return asset.Name
	}
	return assetID
}

func (c *Capability) findingNumberOf(ctx context.Context, findingID string) string {
	var number int64
	if err := c.store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.QueryRowContext(ctx, `SELECT number FROM webapi_findings WHERE id = ?`, findingID).Scan(&number)
	}); err != nil {
		return ""
	}
	return findingNumber(number)
}

// ---- 任务、策略、会话 ----

func (c *Capability) auditTaskCreate(ctx context.Context, task *Task, assetID, scope, approval string) {
	if task == nil {
		return
	}
	actor := auditActorOf(ctx)
	detail := map[string]any{"origins": task.Origins, "policy": policyDetail(scope, approval), "session_id": task.InitialSessionID}
	if task.Scenario != nil {
		detail["scenario"] = task.Scenario.Key
	} else if scenario, err := c.store.scenarioForTask(ctx, task.ID); err == nil {
		detail["scenario"] = scenario.Key
	}
	summary := "新建任务：" + usageTitle(task.Objective)
	if assetID != "" {
		detail["asset_id"] = assetID
		summary = "从资产「" + c.assetName(ctx, assetID) + "」发起任务：" + usageTitle(task.Objective)
	}
	c.audit(ctx, auditEntry{Actor: actor, Action: "task.create", TargetKind: "task", TargetID: task.ID, TaskID: task.ID, AssetID: assetID, Summary: summary, Detail: detail})
}

func (c *Capability) auditTaskPolicy(ctx context.Context, taskID string, from, to TaskPolicy) {
	if from.Scope == to.Scope && from.Approval == to.Approval {
		return
	}
	assetID, _ := c.store.taskAssetID(ctx, taskID)
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "task.policy", TargetKind: "task", TargetID: taskID, TaskID: taskID, AssetID: assetID,
		Summary: "修改任务测试策略：" + policyLabel(from.Scope, from.Approval) + " → " + policyLabel(to.Scope, to.Approval),
		Detail:  map[string]any{"from": policyDetail(from.Scope, from.Approval), "to": policyDetail(to.Scope, to.Approval)}})
}

// auditSessionStart 登记新建验证/复测会话（kind = verification | retest）。
func (c *Capability) auditSessionStart(ctx context.Context, kind, taskID, findingID, targetID, sessionID string) {
	number := c.findingNumberOf(ctx, findingID)
	label := map[string]string{"verification": "发起独立验证：", "retest": "发起复测："}[kind]
	assetID, _ := c.store.taskAssetID(ctx, taskID)
	c.audit(ctx, auditEntry{Actor: auditActorOf(ctx), Action: kind + ".start", TargetKind: kind, TargetID: targetID, TaskID: taskID, AssetID: assetID,
		Summary: label + number, Detail: map[string]any{"finding_id": findingID, "finding_number": number, "session_id": sessionID}})
}

func (c *Capability) auditReportStart(ctx context.Context, report *ReportSummary) {
	if report == nil {
		return
	}
	numbers := []string{}
	for _, item := range report.Items {
		numbers = append(numbers, c.findingNumberOf(ctx, item.FindingID))
	}
	assetID, _ := c.store.taskAssetID(ctx, report.TaskID)
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "report.start", TargetKind: "report", TargetID: report.ReportID, TaskID: report.TaskID, AssetID: assetID,
		Summary: fmt.Sprintf("发起报告：%d 个候选（%s）", len(numbers), strings.Join(numbers, "、")),
		Detail:  map[string]any{"finding_numbers": numbers, "session_id": report.SessionID}})
}

// ---- 审批 ----

// reviewOperation 是审批对象摘要：HTTP 为方法+URL（去 userinfo、脱敏），终端为命令前 200 字（脱敏），写文件为路径。
func reviewOperation(name, arguments string) string {
	switch name {
	case "webapi_http":
		if args, err := coretool.ParseArgs[httpToolArguments](arguments); err == nil {
			return strings.ToUpper(args.Method) + " " + guardrail.RedactText(stripURLUserinfo(args.URL))
		}
	case "bash":
		if args, err := coretool.ParseArgs[struct {
			Command string `json:"command"`
		}](arguments); err == nil {
			return capRunes(guardrail.RedactText(args.Command), auditCommandRunes)
		}
	case "write":
		if args, err := coretool.ParseArgs[struct {
			Path string `json:"path"`
		}](arguments); err == nil {
			return guardrail.RedactText(args.Path)
		}
	}
	return ""
}

// auditReview 登记审批决定：批准/拒绝（user）、超时（system）；取消不是决定，不登记。
func (c *Capability) auditReview(ctx context.Context, sessionID, operationID string, task *Task, name, arguments string, state guardrail.ReviewState) {
	action, actor, label := "", auditActorUser, ""
	switch state {
	case guardrail.ReviewState_REVIEW_STATE_APPROVED:
		action, label = "approval.approve", "批准危险操作："
	case guardrail.ReviewState_REVIEW_STATE_REJECTED:
		action, label = "approval.deny", "拒绝危险操作："
	case guardrail.ReviewState_REVIEW_STATE_EXPIRED:
		action, actor, label = "approval.expire", auditActorSystem, "审批超时（视为拒绝）："
	default:
		return
	}
	taskID, assetID := "", ""
	if task != nil {
		taskID = task.ID
		assetID, _ = c.store.taskAssetID(ctx, taskID)
	}
	op := reviewOperation(name, arguments)
	c.audit(ctx, auditEntry{Actor: actor, Action: action, TargetKind: "operation", TargetID: operationID, TaskID: taskID, AssetID: assetID,
		Summary: label + name + " " + op, Detail: map[string]any{"tool": name, "operation": op, "session_id": sessionID}})
}

// ---- 资产与测试账号 ----

var assetFieldLabels = []struct{ key, label string }{
	{"name", "名称"}, {"owner", "负责人"}, {"environment", "环境"}, {"known_info", "已知信息"}, {"objective", "默认目标"},
	{"origins", "origins"}, {"forbidden", "禁止行为"}, {"scope", "范围"}, {"approval", "审批"}, {"archived", "归档"},
	{"scenario", "研究场景"}, {"done_criteria", "完成标准"}, {"max_rounds", "最大轮数"},
}

func assetFieldValues(model *assetModel) map[string]any {
	if model == nil {
		return map[string]any{}
	}
	origins, _ := decodeOrigins(model.OriginsJSON)
	return map[string]any{"name": model.Name, "owner": model.Owner, "environment": model.Environment, "known_info": model.KnownInfo, "objective": model.Objective,
		"origins": origins, "forbidden": model.Forbidden, "scope": model.Scope, "approval": model.Approval, "archived": model.Archived,
		"scenario": model.ScenarioKey, "done_criteria": model.DoneCriteria, "max_rounds": model.MaxRounds}
}

// auditAssetSave：before 为保存前的资产（新建为 nil）；无变化不登记。
func (c *Capability) auditAssetSave(ctx context.Context, before *assetModel, after assetModel) {
	old, current := assetFieldValues(before), assetFieldValues(&after)
	changed, labels, values := []string{}, []string{}, map[string]any{}
	for _, field := range assetFieldLabels {
		oldRaw, _ := json.Marshal(old[field.key])
		newRaw, _ := json.Marshal(current[field.key])
		if before != nil && string(oldRaw) == string(newRaw) {
			continue
		}
		changed, labels = append(changed, field.key), append(labels, field.label)
		if slices.Contains([]string{"name", "owner", "environment", "origins", "scope", "approval", "archived", "scenario", "max_rounds"}, field.key) {
			values[field.key] = current[field.key]
		}
	}
	if len(changed) == 0 {
		return
	}
	action, summary := "asset.update", "修改资产「"+after.Name+"」："+strings.Join(labels, "、")
	switch {
	case before == nil:
		action, summary = "asset.create", "新建资产："+after.Name
	case !before.Archived && after.Archived:
		action, summary = "asset.archive", "归档资产："+after.Name
	case before.Archived && !after.Archived:
		action, summary = "asset.unarchive", "取消归档资产："+after.Name
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: action, TargetKind: "asset", TargetID: after.ID, AssetID: after.ID, Summary: summary,
		Detail: map[string]any{"changed": changed, "values": values}})
}

func (c *Capability) credentialBefore(ctx context.Context, assetID, credentialID string) *assetCredentialModel {
	model, err := readOptional[assetCredentialModel](ctx, c.store, "id = ? AND asset_id = ?", credentialID, assetID)
	if err != nil {
		return nil
	}
	return model
}

func credentialLabel(label, username string) string {
	if label != "" {
		return label
	}
	return username
}

// auditCredentialSave：secret 只记是否本次更换；无变化不登记。
func (c *Capability) auditCredentialSave(ctx context.Context, assetID, credentialID string, before *assetCredentialModel, body saveCredential, secretReplaced bool) {
	changed := []string{}
	if before == nil || before.Label != body.Label {
		changed = append(changed, "label")
	}
	if before == nil || before.Username != body.Username {
		changed = append(changed, "username")
	}
	if before == nil || before.Note != body.Note {
		changed = append(changed, "note")
	}
	if secretReplaced {
		changed = append(changed, "secret")
	}
	if before != nil && len(changed) == 0 {
		return
	}
	verb := "修改测试账号："
	if before == nil {
		verb = "新增测试账号："
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "credential.save", TargetKind: "credential", TargetID: credentialID, AssetID: assetID,
		Summary: verb + credentialLabel(body.Label, body.Username) + "（资产「" + c.assetName(ctx, assetID) + "」）",
		Detail:  map[string]any{"label": body.Label, "username": body.Username, "changed": changed, "secret_replaced": secretReplaced, "created": before == nil}})
}

func (c *Capability) auditCredentialDelete(ctx context.Context, assetID, credentialID string, before *assetCredentialModel) {
	label, username := "", ""
	if before != nil {
		label, username = before.Label, before.Username
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "credential.delete", TargetKind: "credential", TargetID: credentialID, AssetID: assetID,
		Summary: "删除测试账号：" + credentialLabel(label, username) + "（资产「" + c.assetName(ctx, assetID) + "」）",
		Detail:  map[string]any{"label": label, "username": username}})
}

// ---- 周期计划 ----

type scheduleAuditState struct {
	exists                 bool
	cron                   string
	enabled                bool
	autoVerify, autoRetest bool
}

func (c *Capability) scheduleSnapshot(ctx context.Context, assetID string) scheduleAuditState {
	model, err := c.store.schedule(ctx, assetID)
	if err != nil || model == nil {
		return scheduleAuditState{}
	}
	verify, retest, _ := c.store.scheduleOptions(ctx, assetID)
	return scheduleAuditState{exists: true, cron: model.Cron, enabled: model.Enabled, autoVerify: verify, autoRetest: retest}
}

func (s scheduleAuditState) detail() map[string]any {
	return map[string]any{"cron": s.cron, "enabled": s.enabled, "auto_verify": s.autoVerify, "auto_retest": s.autoRetest}
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "启用"
	}
	return "停用"
}

func (c *Capability) auditScheduleSave(ctx context.Context, assetID string, before scheduleAuditState, model assetScheduleModel, options scheduleOptionsModel) {
	after := scheduleAuditState{exists: true, cron: model.Cron, enabled: model.Enabled, autoVerify: options.AutoVerify, autoRetest: options.AutoRetest}
	if before == after {
		return
	}
	verb := "修改周期计划"
	detail := map[string]any{"to": after.detail(), "created": !before.exists}
	if before.exists {
		detail["from"] = before.detail()
	} else {
		verb = "新建周期计划"
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "schedule.save", TargetKind: "schedule", TargetID: assetID, AssetID: assetID,
		Summary: verb + "：资产「" + c.assetName(ctx, assetID) + "」 cron " + model.Cron + "（" + enabledLabel(model.Enabled) + "）", Detail: detail})
}

func (c *Capability) auditScheduleDelete(ctx context.Context, assetID string, before scheduleAuditState) {
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "schedule.delete", TargetKind: "schedule", TargetID: assetID, AssetID: assetID,
		Summary: "删除周期计划：资产「" + c.assetName(ctx, assetID) + "」 cron " + before.cron, Detail: before.detail()})
}

// recordRun 写计划运行记录并登记 schedule.run（system）；同 asset+scheduled_for 只登记一次。
func (c *Capability) recordRun(ctx context.Context, run scheduleRunModel, expectedNext, newNext, cron string, enabled bool) error {
	if err := c.store.recordScheduleRun(ctx, run, expectedNext, newNext); err != nil {
		return err
	}
	if run.Status == ScheduleRunFailed {
		c.notifyScheduleFailed(ctx, run.AssetID, run.ScheduledFor, cron)
	}
	name := c.assetName(ctx, run.AssetID)
	summary := "计划发起任务：资产「" + name + "」"
	switch run.Status {
	case ScheduleRunFailed:
		summary = "计划运行失败：资产「" + name + "」：" + run.Error
	case ScheduleRunSkipped:
		summary = "计划跳过：资产「" + name + "」：" + run.Error
	}
	c.audit(ctx, auditEntry{ID: "au_run_" + scheduleUUID("audit", run.AssetID, run.ScheduledFor), Actor: auditActorSystem, Action: "schedule.run",
		TargetKind: "schedule", TargetID: run.AssetID, TaskID: run.TaskID, AssetID: run.AssetID, Summary: summary,
		Detail: map[string]any{"scheduled_for": run.ScheduledFor, "status": run.Status, "error": run.Error, "task_id": run.TaskID, "cron": cron, "enabled": enabled}})
	return nil
}

// ---- 漏洞库人工改动 ----

func (c *Capability) vulnTitle(ctx context.Context, assetID, vulnID string) string {
	if view, err := c.store.assetVulnView(ctx, assetID, vulnID); err == nil && view != nil {
		return view.Title
	}
	return ""
}

func (c *Capability) auditVulnRename(ctx context.Context, assetID, vulnID, beforeTitle string) {
	after := c.vulnTitle(ctx, assetID, vulnID)
	if after == beforeTitle {
		return
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.rename", TargetKind: "vuln", TargetID: vulnID, AssetID: assetID,
		Summary: "重命名漏洞条目：" + beforeTitle + " → " + after, Detail: map[string]any{"from_title": beforeTitle, "to_title": after}})
}

func (c *Capability) vulnMemberBefore(ctx context.Context, findingID string) string {
	member, err := readOptional[assetVulnMemberModel](ctx, c.store, "finding_id = ?", findingID)
	if err != nil || member == nil {
		return ""
	}
	return member.VulnID
}

func (c *Capability) auditVulnMove(ctx context.Context, assetID, vulnID, findingID, fromVuln string, created bool) {
	if fromVuln == vulnID {
		return
	}
	number := c.findingNumberOf(ctx, findingID)
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.move", TargetKind: "vuln", TargetID: vulnID, AssetID: assetID,
		Summary: "移动候选 " + number + " 到漏洞条目「" + c.vulnTitle(ctx, assetID, vulnID) + "」",
		Detail:  map[string]any{"finding_id": findingID, "finding_number": number, "from_vuln_id": fromVuln, "to_vuln_id": vulnID, "created": created}})
}

func (c *Capability) auditVulnMerge(ctx context.Context, assetID, into, from, fromTitle string) {
	intoTitle := c.vulnTitle(ctx, assetID, into)
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.merge", TargetKind: "vuln", TargetID: into, AssetID: assetID,
		Summary: "合并漏洞条目：「" + fromTitle + "」并入「" + intoTitle + "」",
		Detail:  map[string]any{"from_vuln_id": from, "from_title": fromTitle, "into_vuln_id": into, "into_title": intoTitle}})
}

// ---- 企业微信推送 ----

// auditNotifySettings：webhook 只记 changed/cleared 布尔；无变化不登记。
func (c *Capability) auditNotifySettings(ctx context.Context, before *notifySettingsModel, after *notifySettingsModel, beforeOps, afterOps *notifyOpsSettingsModel, newWebhook string, clear bool) {
	old := notifySettingsModel{}
	if before != nil {
		old = *before
	}
	changed := newWebhook != ""
	if changed && old.WebhookCipher != "" {
		if previous, err := c.decryptWebhook(old.WebhookCipher); err == nil && previous == newWebhook {
			changed = false
		}
	}
	cleared := clear && old.WebhookCipher != ""
	parts := []string{}
	if old.Enabled != after.Enabled {
		parts = append(parts, "推送"+enabledLabel(old.Enabled)+" → "+enabledLabel(after.Enabled))
	}
	if old.RequireConfirm != after.RequireConfirm {
		parts = append(parts, fmt.Sprintf("人工确认 %v → %v", old.RequireConfirm, after.RequireConfirm))
	}
	if old.BaseURL != after.BaseURL {
		parts = append(parts, "平台地址 "+old.BaseURL+" → "+after.BaseURL)
	}
	if changed {
		parts = append(parts, "更换 webhook")
	}
	if cleared {
		parts = append(parts, "清除 webhook")
	}
	oldOps := notifyOpsSettingsModel{}
	if beforeOps != nil {
		oldOps = *beforeOps
	}
	for _, item := range []struct {
		label    string
		from, to bool
	}{{"待审批通知", oldOps.NotifyApproval, afterOps.NotifyApproval}, {"计划失败通知", oldOps.NotifyScheduleFailed, afterOps.NotifyScheduleFailed}, {"每周摘要", oldOps.NotifyWeeklyDigest, afterOps.NotifyWeeklyDigest}} {
		if item.from != item.to {
			parts = append(parts, item.label+enabledLabel(item.from)+" → "+enabledLabel(item.to))
		}
	}
	if len(parts) == 0 {
		return
	}
	opsDetail := func(ops notifyOpsSettingsModel) map[string]any {
		return map[string]any{"notify_approval": ops.NotifyApproval, "notify_schedule_failed": ops.NotifyScheduleFailed, "notify_weekly_digest": ops.NotifyWeeklyDigest}
	}
	from, to := opsDetail(oldOps), opsDetail(*afterOps)
	from["enabled"], from["require_confirm"], from["base_url"] = old.Enabled, old.RequireConfirm, old.BaseURL
	to["enabled"], to["require_confirm"], to["base_url"] = after.Enabled, after.RequireConfirm, after.BaseURL
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "notify.settings", TargetKind: "notify", TargetID: notifyChannelWeCom,
		Summary: "修改企业微信推送配置：" + strings.Join(parts, "；"),
		Detail: map[string]any{
			"from": from, "to": to, "webhook_changed": changed, "webhook_cleared": cleared,
		}})
}

func (c *Capability) auditNotifyTest(ctx context.Context, ok bool, errcode int) {
	summary := "测试企业微信推送：成功"
	if !ok {
		summary = fmt.Sprintf("测试企业微信推送：失败（errcode %d）", errcode)
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "notify.test", TargetKind: "notify", TargetID: notifyChannelWeCom, Summary: summary,
		Detail: map[string]any{"ok": ok, "errcode": errcode}})
}

// auditNotification 登记一条通知的人工处理或投递终态。
func (c *Capability) auditNotification(ctx context.Context, actor, action, id string, extra map[string]any) {
	labels := map[string]string{"notification.confirm": "确认推送：", "notification.dismiss": "忽略推送：", "notification.retry": "重试推送：",
		"notification.sent": "推送成功：", "notification.failed": "推送失败："}
	ctx = context.WithoutCancel(ctx)
	detail := map[string]any{"notification_id": id}
	for key, value := range extra {
		detail[key] = value
	}
	title, taskID, assetID := "", "", ""
	if views, err := c.store.notificationViews(ctx, notificationFilter{ID: id}, 1, 0); err == nil && len(views) > 0 {
		title, taskID = views[0].Title, views[0].TaskID
		if views[0].Asset != nil {
			assetID = views[0].Asset.ID
		}
	}
	detail["title"] = title
	c.audit(ctx, auditEntry{Actor: actor, Action: action, TargetKind: "notification", TargetID: id, TaskID: taskID, AssetID: assetID, Summary: labels[action] + title, Detail: detail})
}
