package webapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"
)

// 漏洞处置（V1）：人工设定的严重度、处置、负责人、修复期限与备注，按全局漏洞 id 存一行；
// 与验证/复测派生状态分开、互不覆盖；模型不写。负责人兜底、期限（SLA）与是否逾期读时计算。契约见 native/TRIAGE.md。

const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
	SeverityInfo     = "info"
	severityNone     = "none" // 过滤与计数里的“未定级”

	DispositionOpen          = "open"
	DispositionFixing        = "fixing"
	DispositionRiskAccepted  = "risk_accepted"
	DispositionFalsePositive = "false_positive"

	maxTriageOwnerRunes = 100
	maxTriageNoteRunes  = 2000
	vulnSLAID           = 1
	minSLADays          = 1
	maxSLADays          = 365
	triageDateLayout    = "2006-01-02"
)

var (
	severityLabels     = map[string]string{SeverityCritical: "严重", SeverityHigh: "高危", SeverityMedium: "中危", SeverityLow: "低危", SeverityInfo: "信息", "": "未定级"}
	dispositionLabels  = map[string]string{DispositionOpen: "待处理", DispositionFixing: "修复中", DispositionRiskAccepted: "接受风险", DispositionFalsePositive: "误报"}
	defaultVulnSLA     = VulnSLA{CriticalDays: 3, HighDays: 7, MediumDays: 30, LowDays: 90}
	errTriageNotFound  = errors.New("漏洞条目不存在")
	triageFieldLabels  = []struct{ key, label string }{{"severity", "严重度"}, {"disposition", "处置"}, {"owner", "负责人"}, {"due_date", "修复期限"}}
	slaFieldLabels     = []struct{ key, label string }{{"critical_days", "严重"}, {"high_days", "高危"}, {"medium_days", "中危"}, {"low_days", "低危"}}
	triageSeveritySet  = map[string]bool{SeverityCritical: true, SeverityHigh: true, SeverityMedium: true, SeverityLow: true, SeverityInfo: true}
	triageDisposition  = map[string]bool{DispositionOpen: true, DispositionFixing: true, DispositionRiskAccepted: true, DispositionFalsePositive: true}
	severityFilterKeys = map[string]bool{SeverityCritical: true, SeverityHigh: true, SeverityMedium: true, SeverityLow: true, SeverityInfo: true, severityNone: true}
)

// vulnTriageModel：键为全局漏洞 id（资产条目 vid；无资产候选 finding:<finding_id>）；asset_id 无资产为空串。
type vulnTriageModel struct {
	bun.BaseModel `bun:"table:webapi_vuln_triage,alias:vuln_triage"`

	VulnKey     string `bun:"vuln_key,pk"`
	AssetID     string `bun:"asset_id,type:text,notnull"`
	Severity    string `bun:"severity,notnull"`
	Disposition string `bun:"disposition,notnull"`
	Owner       string `bun:"owner,type:text,notnull"`
	DueDate     string `bun:"due_date,notnull"`
	Note        string `bun:"note,type:text,notnull"`
	CreatedAt   string `bun:"created_at,notnull"`
	UpdatedAt   string `bun:"updated_at,notnull"`
}

// vulnSLAModel 单行（id=1）；无行即默认 3/7/30/90。
type vulnSLAModel struct {
	bun.BaseModel `bun:"table:webapi_vuln_sla,alias:vuln_sla"`

	ID           int64  `bun:"id,pk"`
	CriticalDays int    `bun:"critical_days,notnull"`
	HighDays     int    `bun:"high_days,notnull"`
	MediumDays   int    `bun:"medium_days,notnull"`
	LowDays      int    `bun:"low_days,notnull"`
	UpdatedAt    string `bun:"updated_at,notnull"`
}

// VulnSLA 是 GET/PUT /api/webapi/vuln-sla 的视图；updated_at 未保存过为 null。
type VulnSLA struct {
	CriticalDays int     `json:"critical_days"`
	HighDays     int     `json:"high_days"`
	MediumDays   int     `json:"medium_days"`
	LowDays      int     `json:"low_days"`
	UpdatedAt    *string `json:"updated_at"`
}

func (s VulnSLA) days(severity string) (int, bool) {
	switch severity {
	case SeverityCritical:
		return s.CriticalDays, true
	case SeverityHigh:
		return s.HighDays, true
	case SeverityMedium:
		return s.MediumDays, true
	case SeverityLow:
		return s.LowDays, true
	}
	return 0, false // info 与未定级无期限
}

func (s VulnSLA) values() map[string]any {
	return map[string]any{"critical_days": s.CriticalDays, "high_days": s.HighDays, "medium_days": s.MediumDays, "low_days": s.LowDays}
}

// TriageView 是条目的处置视图（PUT 响应与列表项 triage 字段）。
type TriageView struct {
	VulnKey        string  `json:"vuln_key"`
	Severity       string  `json:"severity"`
	Disposition    string  `json:"disposition"`
	Owner          string  `json:"owner"`
	OwnerEffective string  `json:"owner_effective"`
	DueDate        string  `json:"due_date"`
	DueEffective   *string `json:"due_effective"`
	Overdue        bool    `json:"overdue"`
	Note           string  `json:"note"`
	UpdatedAt      *string `json:"updated_at"`
}

// ---- 存储 ----

func (s *taskStore) vulnSLA(ctx context.Context) (VulnSLA, error) {
	model, err := findOptional[vulnSLAModel](ctx, s.orm, "id = ?", vulnSLAID)
	if err != nil || model == nil {
		return defaultVulnSLA, err
	}
	at := model.UpdatedAt
	return VulnSLA{CriticalDays: model.CriticalDays, HighDays: model.HighDays, MediumDays: model.MediumDays, LowDays: model.LowDays, UpdatedAt: &at}, nil
}

func (s *taskStore) putVulnSLA(ctx context.Context, sla VulnSLA, at string) error {
	model := &vulnSLAModel{ID: vulnSLAID, CriticalDays: sla.CriticalDays, HighDays: sla.HighDays, MediumDays: sla.MediumDays, LowDays: sla.LowDays, UpdatedAt: at}
	_, err := s.orm.NewInsert().Model(model).On("CONFLICT (id) DO UPDATE").
		Set("critical_days = EXCLUDED.critical_days, high_days = EXCLUDED.high_days, medium_days = EXCLUDED.medium_days, low_days = EXCLUDED.low_days, updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

func (s *taskStore) triageRecord(ctx context.Context, key string) (*vulnTriageModel, error) {
	return findOptional[vulnTriageModel](ctx, s.orm, "vuln_key = ?", key)
}

// triageRecords 返回全部处置记录（按键）；assetID 非空时只取该资产。
func (s *taskStore) triageRecords(ctx context.Context, assetID string) (map[string]vulnTriageModel, error) {
	var models []vulnTriageModel
	query := s.orm.NewSelect().Model(&models)
	if assetID != "" {
		query = query.Where("asset_id = ?", assetID)
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]vulnTriageModel, len(models))
	for _, model := range models {
		out[model.VulnKey] = model
	}
	return out, nil
}

// vulnDisposition 返回条目处置（无记录为 open）。
func (s *taskStore) vulnDisposition(ctx context.Context, key string) (string, error) {
	record, err := s.triageRecord(ctx, key)
	if err != nil || record == nil {
		return DispositionOpen, err
	}
	return record.Disposition, nil
}

func (s *taskStore) putTriageRecord(ctx context.Context, model *vulnTriageModel) error {
	_, err := s.orm.NewInsert().Model(model).On("CONFLICT (vuln_key) DO UPDATE").
		Set("asset_id = EXCLUDED.asset_id, severity = EXCLUDED.severity, disposition = EXCLUDED.disposition, owner = EXCLUDED.owner, due_date = EXCLUDED.due_date, note = EXCLUDED.note, updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// mergeTriage（合并事务内）：目标无记录则继承 from 的记录，否则保留目标的；from 记录删除。
func mergeTriage(ctx context.Context, tx bun.Tx, into, from string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE webapi_vuln_triage SET vuln_key = ? WHERE vuln_key = ? AND NOT EXISTS (SELECT 1 FROM webapi_vuln_triage WHERE vuln_key = ?)`, into, from, into); err != nil {
		return err
	}
	_, err := tx.NewDelete().Model((*vulnTriageModel)(nil)).Where("vuln_key = ?", from).Exec(ctx)
	return err
}

// ---- 派生 ----

// triageContext 是一次读取用到的全部处置记录、SLA、资产负责人与 Hub 本地“今天”。
type triageContext struct {
	records map[string]vulnTriageModel
	sla     VulnSLA
	owners  map[string]string
	loc     *time.Location
	today   string
}

func (c *Capability) loadTriageContext(ctx context.Context) (*triageContext, error) {
	records, err := c.store.triageRecords(ctx, "")
	if err != nil {
		return nil, err
	}
	sla, err := c.store.vulnSLA(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.store.db.QueryContext(ctx, `SELECT id, owner FROM webapi_assets`)
	if err != nil {
		return nil, err
	}
	owners := map[string]string{}
	for rows.Next() {
		var id, owner string
		if err := rows.Scan(&id, &owner); err != nil {
			rows.Close()
			return nil, err
		}
		owners[id] = owner
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	loc := c.scheduleLocation()
	return &triageContext{records: records, sla: sla, owners: owners, loc: loc, today: c.scheduleClock().In(loc).Format(triageDateLayout)}, nil
}

// view 计算条目处置视图：firstConfirmed 为条目第一条“存在”事件时间（无为零值）。
func (t *triageContext) view(key, assetID, status string, firstConfirmed time.Time) *TriageView {
	view := &TriageView{VulnKey: key, Disposition: DispositionOpen}
	if record, ok := t.records[key]; ok {
		at := record.UpdatedAt
		view.Severity, view.Disposition, view.Owner, view.DueDate, view.Note, view.UpdatedAt = record.Severity, record.Disposition, record.Owner, record.DueDate, record.Note, &at
	}
	view.OwnerEffective = view.Owner
	if view.OwnerEffective == "" {
		view.OwnerEffective = t.owners[assetID]
	}
	due := view.DueDate
	if days, ok := t.sla.days(view.Severity); due == "" && ok && !firstConfirmed.IsZero() {
		local := firstConfirmed.In(t.loc)
		due = time.Date(local.Year(), local.Month(), local.Day()+days, 0, 0, 0, 0, t.loc).Format(triageDateLayout)
	}
	if due != "" {
		view.DueEffective = &due
		view.Overdue = triageOpen(status, view.Disposition) && t.today > due
	}
	return view
}

// triageOpen：“待修复”= 派生状态 new/present 且处置 open/fixing。
func triageOpen(status, disposition string) bool {
	return (status == VulnNew || status == VulnPresent) && (disposition == DispositionOpen || disposition == DispositionFixing)
}

func severityBucket(severity string) string {
	if severity == "" {
		return severityNone
	}
	return severity
}

func newSeverityCounts() map[string]int {
	return map[string]int{SeverityCritical: 0, SeverityHigh: 0, SeverityMedium: 0, SeverityLow: 0, SeverityInfo: 0, severityNone: 0}
}

// attachAssetTriage 给资产条目视图填 triage。
func (c *Capability) attachAssetTriage(ctx context.Context, assetID string, views ...*AssetVuln) error {
	triage, err := c.loadTriageContext(ctx)
	if err != nil {
		return err
	}
	for _, view := range views {
		view.Triage = triage.view(view.ID, assetID, view.Status, view.firstConfirmed)
	}
	return nil
}

// ---- 处置 HTTP ----

type triageTarget struct {
	assetID, title, status string
	firstConfirmed         time.Time
}

// resolveVulnKey 找到全局漏洞条目：finding:<id> 须是未关联资产任务的候选；否则为非空资产条目（含归档资产）。
// 资产条目返回时持有该资产漏洞库锁（unlock 非 nil）。
func (c *Capability) resolveVulnKey(ctx context.Context, key string) (*triageTarget, func(), error) {
	if findingID, ok := strings.CutPrefix(key, "finding:"); ok {
		items, err := c.store.standaloneFindingVulns(ctx)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range items {
			if item.ID == "finding:"+findingID {
				return &triageTarget{title: item.Title, status: item.Status, firstConfirmed: item.firstConfirmed}, func() {}, nil
			}
		}
		return nil, nil, errTriageNotFound
	}
	vuln, err := findOptional[assetVulnModel](ctx, c.store.orm, "id = ?", key)
	if err == nil && vuln == nil {
		// 条目可能尚未同步出来（auto 条目在漏洞库接口首次访问时才建）：同步全部资产后再找一次。
		if err = c.syncAllAssetVulns(ctx); err == nil {
			vuln, err = findOptional[assetVulnModel](ctx, c.store.orm, "id = ?", key)
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if vuln == nil {
		return nil, nil, errTriageNotFound
	}
	unlock := c.lockAssetVulns(vuln.AssetID)
	if err := c.store.syncAssetVulns(ctx, vuln.AssetID); err != nil {
		unlock()
		return nil, nil, err
	}
	view, err := c.store.assetVulnView(ctx, vuln.AssetID, key)
	if err != nil || view == nil {
		unlock()
		if err == nil {
			err = errTriageNotFound
		}
		return nil, nil, err
	}
	return &triageTarget{assetID: vuln.AssetID, title: view.Title, status: view.Status, firstConfirmed: view.firstConfirmed}, unlock, nil
}

// syncAllAssetVulns 按资产加锁同步全部资产（含归档）的漏洞库。
func (c *Capability) syncAllAssetVulns(ctx context.Context) error {
	rows, err := c.store.db.QueryContext(ctx, `SELECT id FROM webapi_assets ORDER BY id`)
	if err != nil {
		return err
	}
	assets := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		assets = append(assets, id)
	}
	if err := closeRows(rows); err != nil {
		return err
	}
	for _, assetID := range assets {
		unlock := c.lockAssetVulns(assetID)
		err := c.store.syncAssetVulns(ctx, assetID)
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

type triagePatch struct {
	Severity    *string `json:"severity"`
	Disposition *string `json:"disposition"`
	Owner       *string `json:"owner"`
	DueDate     *string `json:"due_date"`
	Note        *string `json:"note"`
}

// apply 校验并把部分更新套到 record 上（文本去首尾空白）。
func (p triagePatch) apply(record *vulnTriageModel) error {
	if p.Severity != nil {
		value := strings.TrimSpace(*p.Severity)
		if value != "" && !triageSeveritySet[value] {
			return errors.New("severity 只能是 critical、high、medium、low、info 或 \"\"（未定级）")
		}
		record.Severity = value
	}
	if p.Disposition != nil {
		value := strings.TrimSpace(*p.Disposition)
		if !triageDisposition[value] {
			return errors.New("disposition 只能是 open、fixing、risk_accepted、false_positive")
		}
		record.Disposition = value
	}
	if p.Owner != nil {
		value := strings.TrimSpace(*p.Owner)
		if utf8.RuneCountInString(value) > maxTriageOwnerRunes {
			return fmt.Errorf("owner 不能超过 %d 字", maxTriageOwnerRunes)
		}
		record.Owner = value
	}
	if p.DueDate != nil {
		value := strings.TrimSpace(*p.DueDate)
		if value != "" {
			if parsed, err := time.Parse(triageDateLayout, value); err != nil || parsed.Format(triageDateLayout) != value {
				return errors.New("due_date 必须是 YYYY-MM-DD 或 \"\"")
			}
		}
		record.DueDate = value
	}
	if p.Note != nil {
		value := strings.TrimSpace(*p.Note)
		if utf8.RuneCountInString(value) > maxTriageNoteRunes {
			return fmt.Errorf("note 不能超过 %d 字", maxTriageNoteRunes)
		}
		record.Note = value
	}
	return nil
}

func sameTriage(a, b vulnTriageModel) bool {
	return a.Severity == b.Severity && a.Disposition == b.Disposition && a.Owner == b.Owner && a.DueDate == b.DueDate && a.Note == b.Note
}

func triageValues(record vulnTriageModel) map[string]string {
	return map[string]string{"severity": record.Severity, "disposition": record.Disposition, "owner": record.Owner, "due_date": record.DueDate}
}

// handleVulnTriagePut: PUT /api/webapi/vulns/{key}/triage（部分更新；无变化不改 updated_at、不登记审计）。
func (c *Capability) handleVulnTriagePut(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if validPathID(key) != nil {
		writeError(w, http.StatusBadRequest, "漏洞条目 id 非法")
		return
	}
	var patch triagePatch
	if err := decodeJSONBody(r, &patch, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := patch.apply(&vulnTriageModel{}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	target, unlock, err := c.resolveVulnKey(ctx, key)
	if errors.Is(err, errTriageNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞条目失败")
		return
	}
	defer unlock()
	c.triageMu.Lock()
	defer c.triageMu.Unlock()
	existing, err := c.store.triageRecord(ctx, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞处置失败")
		return
	}
	before := vulnTriageModel{VulnKey: key, AssetID: target.assetID, Disposition: DispositionOpen}
	if existing != nil {
		before = *existing
	}
	after := before
	_ = patch.apply(&after) // 已校验
	if !sameTriage(before, after) {
		now := nowRFC3339()
		after.AssetID, after.UpdatedAt = target.assetID, now
		if existing == nil {
			after.CreatedAt = now
		}
		if err := c.store.putTriageRecord(ctx, &after); err != nil {
			writeError(w, http.StatusInternalServerError, "保存漏洞处置失败")
			return
		}
		c.auditVulnTriage(ctx, key, target, before, after)
	}
	triage, err := c.loadTriageContext(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞处置失败")
		return
	}
	writeJSON(w, http.StatusOK, triage.view(key, target.assetID, target.status, target.firstConfirmed))
}

func (c *Capability) handleVulnSLAGet(w http.ResponseWriter, r *http.Request) {
	sla, err := c.store.vulnSLA(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取修复期限失败")
		return
	}
	writeJSON(w, http.StatusOK, sla)
}

// handleVulnSLAPut: PUT {critical_days?, high_days?, medium_days?, low_days?}；省略的字段保持原值。
func (c *Capability) handleVulnSLAPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CriticalDays *int `json:"critical_days"`
		HighDays     *int `json:"high_days"`
		MediumDays   *int `json:"medium_days"`
		LowDays      *int `json:"low_days"`
	}
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, field := range []struct {
		name  string
		value *int
	}{{"critical_days", body.CriticalDays}, {"high_days", body.HighDays}, {"medium_days", body.MediumDays}, {"low_days", body.LowDays}} {
		if field.value != nil && (*field.value < minSLADays || *field.value > maxSLADays) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s 必须是 %d..%d 的整数", field.name, minSLADays, maxSLADays))
			return
		}
	}
	ctx := r.Context()
	c.triageMu.Lock()
	defer c.triageMu.Unlock()
	before, err := c.store.vulnSLA(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取修复期限失败")
		return
	}
	after := before
	for _, field := range []struct {
		value  *int
		target *int
	}{{body.CriticalDays, &after.CriticalDays}, {body.HighDays, &after.HighDays}, {body.MediumDays, &after.MediumDays}, {body.LowDays, &after.LowDays}} {
		if field.value != nil {
			*field.target = *field.value
		}
	}
	if after.CriticalDays != before.CriticalDays || after.HighDays != before.HighDays || after.MediumDays != before.MediumDays || after.LowDays != before.LowDays {
		now := nowRFC3339()
		if err := c.store.putVulnSLA(ctx, after, now); err != nil {
			writeError(w, http.StatusInternalServerError, "保存修复期限失败")
			return
		}
		after.UpdatedAt = &now
		c.auditVulnSLA(ctx, before, after)
	}
	writeJSON(w, http.StatusOK, after)
}

// ---- 审计 ----

func triageValueLabel(field, value string) string {
	switch field {
	case "severity":
		return severityLabels[value]
	case "disposition":
		return dispositionLabels[value]
	}
	if value == "" {
		return "（空）"
	}
	return value
}

// auditVulnTriage：detail 为变化字段前后值；备注只记是否修改（note_changed），不记正文。
func (c *Capability) auditVulnTriage(ctx context.Context, key string, target *triageTarget, before, after vulnTriageModel) {
	from, to := triageValues(before), triageValues(after)
	changes := map[string]any{}
	parts := []string{}
	for _, field := range triageFieldLabels {
		if from[field.key] != to[field.key] {
			changes[field.key] = map[string]string{"from": from[field.key], "to": to[field.key]}
			parts = append(parts, field.label+" "+triageValueLabel(field.key, from[field.key])+" → "+triageValueLabel(field.key, to[field.key]))
		}
	}
	noteChanged := before.Note != after.Note
	if noteChanged {
		parts = append(parts, "备注已修改")
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.triage", TargetKind: "vuln", TargetID: key, AssetID: target.assetID,
		Summary: "修改漏洞处置「" + target.title + "」：" + strings.Join(parts, "；"),
		Detail:  map[string]any{"vuln_key": key, "title": target.title, "changes": changes, "note_changed": noteChanged}})
}

func (c *Capability) auditVulnSLA(ctx context.Context, before, after VulnSLA) {
	from, to := before.values(), after.values()
	parts := []string{}
	for _, field := range slaFieldLabels {
		if from[field.key] != to[field.key] {
			parts = append(parts, fmt.Sprintf("%s %v → %v 天", field.label, from[field.key], to[field.key]))
		}
	}
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.sla", TargetKind: "vuln_sla", TargetID: "default",
		Summary: "修改修复期限 SLA：" + strings.Join(parts, "；"), Detail: map[string]any{"from": from, "to": to}})
}
