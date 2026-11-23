package webapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"google.golang.org/protobuf/proto"

	"github.com/chainreactors/cyber/aop/traffic"
)

// 漏洞卡（K1 后端）：按全局漏洞键（资产条目 vid 或 finding:<id>）只读聚合标题区、最小复现、复测、
// 生命周期、附录、同资产其他漏洞与上一条/下一条；curl 由关键步骤证据原件确定性生成。契约见 native/VULN_CARD.md。

const (
	vulnCardRelatedMax   = 5
	vulnCardDetailRunes  = 200
	vulnCardEvidenceMax  = 200
	vulnCurlMaskValue    = "******"
	reproMissingNone     = "no_verification" // 成员没有任何已保存的验证判读
	reproMissingNoSupp   = "no_supported"    // 有判读但没有 supported
	reproMissingNoSteps  = "no_key_steps"    // 有 supported，但都不带最小复现（旧结果）
	vulnCardCodeLen      = 8
	vulnCardTitleDivider = " · "
)

var (
	conclusionLabels = map[string]string{"reproducible": "仍可复现", "fixed_in_scope": "已修复（范围内）", "inconclusive": "无法判定"}
	// lifecycleKindOrder：同一时刻的事件按业务先后排列。
	lifecycleKindOrder = map[string]int{"candidate": 0, "verification": 1, "triage": 2, "notify": 3, "replay": 4, "retest_request": 5, "retest": 6, "fixed": 7}
)

// vulnCode 是平台短编号：V- + 条目 id 去前缀（finding:、wf_）与非字母数字后的前 8 位大写；稳定、只由 id 决定。
func vulnCode(id string) string {
	raw := strings.TrimPrefix(strings.TrimPrefix(id, "finding:"), "wf_")
	var b strings.Builder
	for _, r := range raw {
		if b.Len() == vulnCardCodeLen {
			break
		}
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		}
	}
	if b.Len() == 0 {
		sum := sha256.Sum256([]byte(id))
		return "V-" + strings.ToUpper(hex.EncodeToString(sum[:vulnCardCodeLen/2]))
	}
	return "V-" + b.String()
}

// endpointMethodPath 把端点键 `METHOD origin/path` 变成 `METHOD /path`；finding:<id> 或空键返回 ""。
func endpointMethodPath(endpointKey string) string {
	method, rest, ok := strings.Cut(endpointKey, " ")
	if !ok || method == "" {
		return ""
	}
	scheme := strings.Index(rest, "://")
	if scheme < 0 {
		return ""
	}
	path := "/"
	if slash := strings.Index(rest[scheme+3:], "/"); slash >= 0 {
		path = rest[scheme+3+slash:]
	}
	return method + " " + path
}

// vulnTitleTemplate：“资产名 · METHOD 路径 · 漏洞标题”，无资产或无端点时省略对应段。
func vulnTitleTemplate(assetName, endpointKey, title string) string {
	parts := []string{}
	if assetName != "" {
		parts = append(parts, assetName)
	}
	if path := endpointMethodPath(endpointKey); path != "" {
		parts = append(parts, path)
	}
	return strings.Join(append(parts, title), vulnCardTitleDivider)
}

// ---- 视图 ----

type VulnCardAsset struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Env     string   `json:"env"`
	Owner   string   `json:"owner"`
	Origins []string `json:"origins"`
}

type VulnCardHeader struct {
	Key               string         `json:"key"`
	Code              string         `json:"code"`
	TitleTemplate     string         `json:"title_template"`
	Title             string         `json:"title"`
	Status            string         `json:"status"`
	Severity          string         `json:"severity"`
	SuggestedSeverity string         `json:"suggested_severity"`
	SeverityReason    string         `json:"severity_reason"`
	Disposition       string         `json:"disposition"`
	DueEffective      *string        `json:"due_effective"`
	Overdue           bool           `json:"overdue"`
	FirstSeenAt       string         `json:"first_seen_at"`
	Asset             *VulnCardAsset `json:"asset"`
	EndpointKey       string         `json:"endpoint_key"`
}

type VulnCardEvidenceRef struct {
	EvidenceID string `json:"evidence_id"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	StatusCode *int64 `json:"status_code"`
}

type VulnCardStep struct {
	Title              string   `json:"title"`
	EvidenceID         string   `json:"evidence_id"`
	Method             string   `json:"method"`
	URL                string   `json:"url"`
	StatusCode         *int64   `json:"status_code"`
	RequestMarksFound  []string `json:"request_marks_found"`
	ResponseMarksFound []string `json:"response_marks_found"`
	MarkMissing        bool     `json:"mark_missing"`
}

type VulnCardContrast struct {
	NormalEvidenceID   string               `json:"normal_evidence_id"`
	AbnormalEvidenceID string               `json:"abnormal_evidence_id"`
	Note               string               `json:"note"`
	Normal             *VulnCardEvidenceRef `json:"normal"`
	Abnormal           *VulnCardEvidenceRef `json:"abnormal"`
}

type VulnCardRepro struct {
	VerificationID string            `json:"verification_id"`
	TaskID         string            `json:"task_id"`
	FindingID      string            `json:"finding_id"`
	SessionID      string            `json:"session_id"`
	Impact         string            `json:"impact"`
	Preconditions  string            `json:"preconditions"`
	Steps          []VulnCardStep    `json:"steps"`
	Contrast       *VulnCardContrast `json:"contrast"`
	Remediation    []string          `json:"remediation"`
	RetestMethod   string            `json:"retest_method"`
	SavedAt        string            `json:"saved_at"`
}

type VulnCardRetest struct {
	RetestID   string `json:"retest_id"`
	TaskID     string `json:"task_id"`
	FindingID  string `json:"finding_id"`
	SessionID  string `json:"session_id"`
	Conclusion string `json:"conclusion"`
	Summary    string `json:"summary"`
	At         string `json:"at"`
}

type VulnCardEvent struct {
	At     string `json:"at"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Actor  string `json:"actor"` // ai | user | system

	at time.Time
}

type VulnCardVerification struct {
	VerificationID    string  `json:"verification_id"`
	TaskID            string  `json:"task_id"`
	FindingID         string  `json:"finding_id"`
	FindingNumber     string  `json:"finding_number"`
	SessionID         string  `json:"session_id"`
	CreatedAt         string  `json:"created_at"`
	Verdict           *string `json:"verdict"`
	Method            string  `json:"method"`
	Preconditions     string  `json:"preconditions"`
	Summary           string  `json:"summary"`
	Limitations       string  `json:"limitations"`
	SuggestedSeverity string  `json:"suggested_severity"`
	SeverityReason    string  `json:"severity_reason"`
	HasRepro          bool    `json:"has_repro"`
	EvidenceCount     int     `json:"evidence_count"`
	SavedAt           *string `json:"saved_at"`
}

type VulnCardEvidence struct {
	EvidenceID string `json:"evidence_id"`
	TaskID     string `json:"task_id"`
	Source     string `json:"source"` // candidate | verification | retest
	Relation   string `json:"relation"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	StatusCode *int64 `json:"status_code"`
	CreatedAt  string `json:"created_at"`
}

type VulnCardAppendix struct {
	Verifications []VulnCardVerification `json:"verifications"`
	Members       []AssetVulnMember      `json:"members"`
	EvidenceCount int                    `json:"evidence_count"`
	Evidence      []VulnCardEvidence     `json:"evidence"`
	Changes       []AuditEvent           `json:"changes"`
}

type VulnCardRelated struct {
	Key               string `json:"key"`
	Code              string `json:"code"`
	Title             string `json:"title"`
	Severity          string `json:"severity"`
	SuggestedSeverity string `json:"suggested_severity"`
	Status            string `json:"status"`
}

type VulnCardNav struct {
	PrevKey *string `json:"prev_key"`
	NextKey *string `json:"next_key"`
}

type VulnCard struct {
	Header             VulnCardHeader  `json:"header"`
	Triage             *TriageView     `json:"triage"`
	Repro              *VulnCardRepro  `json:"repro"`
	ReproMissingReason *string         `json:"repro_missing_reason"`
	Retest             *VulnCardRetest `json:"retest"`
	// RetestRequest：最近一次漏洞卡“申请复测”（V3）；无则 null。结论仍在 Retest。
	RetestRequest *VulnCardRetestRequest `json:"retest_request"`
	Lifecycle     []VulnCardEvent        `json:"lifecycle"`
	Appendix      VulnCardAppendix       `json:"appendix"`
	Related       []VulnCardRelated      `json:"related"`
	Nav           VulnCardNav            `json:"nav"`
	Replays       []VulnCardReplay       `json:"replays"`
	Notify        VulnCardNotify         `json:"notify"`
}

// ---- 定位 ----

// cardTarget 找到条目视图（含处置）与全局默认排序列表（未归档资产条目 + 无资产候选）；
// 归档资产条目也可打开，但不在排序列表中（nav 为空）。不存在返回 errTriageNotFound。
func (c *Capability) cardTarget(ctx context.Context, key string) (*GlobalVuln, []*GlobalVuln, error) {
	index, err := c.loadCardIndex(ctx)
	if err != nil {
		return nil, nil, err
	}
	return c.cardTargetIn(ctx, index, key)
}

// cardIndex 是全局默认排序列表（含处置视图）；批量导出复用同一份，避免每键重算。
type cardIndex struct {
	all    []*GlobalVuln
	triage *triageContext
}

func (c *Capability) loadCardIndex(ctx context.Context) (*cardIndex, error) {
	all, err := c.globalVulns(ctx, "")
	if err != nil {
		return nil, err
	}
	triage, err := c.loadTriageContext(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range all {
		item.Triage = triage.view(item.ID, item.assetID, item.Status, item.firstConfirmed)
	}
	sortGlobalVulns(all)
	return &cardIndex{all: all, triage: triage}, nil
}

func (c *Capability) cardTargetIn(ctx context.Context, index *cardIndex, key string) (*GlobalVuln, []*GlobalVuln, error) {
	all, triage := index.all, index.triage
	for _, item := range all {
		if item.ID == key {
			return item, all, nil
		}
	}
	if strings.HasPrefix(key, "finding:") {
		return nil, nil, errTriageNotFound
	}
	vuln, err := readOptional[assetVulnModel](ctx, c.store, "id = ?", key)
	if err == nil && vuln == nil {
		if err = c.syncAllAssetVulns(ctx); err == nil {
			vuln, err = readOptional[assetVulnModel](ctx, c.store, "id = ?", key)
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if vuln == nil {
		return nil, nil, errTriageNotFound
	}
	asset, err := c.store.assetModel(ctx, vuln.AssetID)
	if err != nil {
		return nil, nil, err
	}
	if asset == nil {
		return nil, nil, errTriageNotFound
	}
	views, events, err := c.syncedAssetVulns(ctx, vuln.AssetID)
	if err != nil {
		return nil, nil, err
	}
	for _, view := range views {
		if view.ID == key {
			item := globalVulnFrom(view, VulnKindAsset, &TaskAssetRef{ID: asset.ID, Name: asset.Name}, events)
			item.Triage = triage.view(item.ID, item.assetID, item.Status, item.firstConfirmed)
			return item, all, nil
		}
	}
	return nil, nil, errTriageNotFound
}

func memberFindingIDs(item *GlobalVuln) []string {
	ids := make([]string, 0, len(item.members))
	for _, member := range item.members {
		ids = append(ids, member.FindingID)
	}
	return ids
}

// ---- 验证与复测 ----

type cardVerification struct {
	model  verificationModel
	result *verificationResultModel
	stored *storedVerificationResult
	at     time.Time
}

// cardVerifications 读取成员全部验证（含未出结果的），按结果时间（无结果按创建时间）倒序。
func (s *taskStore) cardVerifications(ctx context.Context, findingIDs []string) ([]cardVerification, error) {
	if len(findingIDs) == 0 {
		return []cardVerification{}, nil
	}
	var models []verificationModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&models).Where("finding_id IN (?)", bun.In(findingIDs)).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	results := map[string]verificationResultModel{}
	if len(models) > 0 {
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		var rows []verificationResultModel
		if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
			return db.NewSelect().Model(&rows).Where("verification_id IN (?)", bun.In(ids)).Scan(ctx)
		}); err != nil {
			return nil, err
		}
		for _, row := range rows {
			results[row.VerificationID] = row
		}
	}
	out := make([]cardVerification, 0, len(models))
	for _, model := range models {
		item := cardVerification{model: model, at: parseAt(model.CreatedAt)}
		if row, ok := results[model.ID]; ok {
			var stored storedVerificationResult
			if err := json.Unmarshal([]byte(row.PayloadJSON), &stored); err != nil {
				return nil, err
			}
			item.result, item.stored, item.at = &row, &stored, parseAt(row.CreatedAt)
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return laterThan(out[i].at, out[i].model.ID, out[j].at, out[j].model.ID)
	})
	return out, nil
}

type cardRetest struct {
	model  retestModel
	result retestResultModel
	stored storedRetestResult
	at     time.Time
}

// cardRetests 读取成员已出结论的复测，按结论时间倒序。
func (s *taskStore) cardRetests(ctx context.Context, findingIDs []string) ([]cardRetest, error) {
	if len(findingIDs) == 0 {
		return []cardRetest{}, nil
	}
	var models []retestModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&models).Where("finding_id IN (?)", bun.In(findingIDs)).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	out := []cardRetest{}
	for _, model := range models {
		row, err := readOptional[retestResultModel](ctx, s, "retest_id = ?", model.ID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			continue
		}
		item := cardRetest{model: model, result: *row, at: parseAt(row.CreatedAt)}
		if err := json.Unmarshal([]byte(row.PayloadJSON), &item.stored); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return laterThan(out[i].at, out[i].model.ID, out[j].at, out[j].model.ID) })
	return out, nil
}

// pickRepro：最新 supported 且带 key_steps 的验证；没有时给出原因。
func pickRepro(verifications []cardVerification) (*cardVerification, string) {
	anyResult, anySupported := false, false
	for index := range verifications {
		item := &verifications[index]
		if item.stored == nil {
			continue
		}
		anyResult = true
		if item.stored.Payload.Verdict != "supported" {
			continue
		}
		anySupported = true
		if item.stored.Payload.hasRepro() {
			return item, ""
		}
	}
	switch {
	case anySupported:
		return nil, reproMissingNoSteps
	case anyResult:
		return nil, reproMissingNoSupp
	}
	return nil, reproMissingNone
}

// suggestedOf：最新一条带建议等级的 supported 验证（与列表 suggested_severity 一致）。
func suggestedOf(verifications []cardVerification) (string, string) {
	for _, item := range verifications {
		if item.stored != nil && item.stored.Payload.Verdict == "supported" && item.stored.Payload.SuggestedSeverity != "" {
			return item.stored.Payload.SuggestedSeverity, item.stored.Payload.SeverityReason
		}
	}
	return "", ""
}

// ---- 证据原件与 marks ----

func decodeEvidenceFlow(model *requestModel) (*traffic.Flow, error) {
	var flow traffic.Flow
	if err := proto.Unmarshal(model.FlowPB, &flow); err != nil {
		return nil, err
	}
	return &flow, nil
}

func bodyTextLines(body []byte) []string {
	if len(body) == 0 || evidenceBodyIsBinary(body) {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines
}

// flowRequestLines / flowResponseLines：与证据图片同一行模型（请求行/状态行、原始头、正文逐行），未遮盖。
func flowRequestLines(flow *traffic.Flow) []string {
	request := flow.GetRequest()
	if request == nil {
		return nil
	}
	lines := []string{requestLineText(request)}
	for _, header := range request.GetHeaders() {
		lines = append(lines, header.GetName()+": "+header.GetValue())
	}
	return append(lines, bodyTextLines(request.GetBody())...)
}

func flowResponseLines(flow *traffic.Flow) []string {
	response := flow.GetResponse()
	if response == nil || response.GetStatusCode() == 0 {
		return nil
	}
	lines := []string{statusLineText(flow.GetRequest(), response)}
	for _, header := range response.GetHeaders() {
		lines = append(lines, header.GetName()+": "+header.GetValue())
	}
	return append(lines, bodyTextLines(response.GetBody())...)
}

// marksIn 返回在某一行中出现的片段（保持原顺序、去重）与是否有片段找不到。
func marksIn(lines []string, marks []string) ([]string, bool) {
	found, missing, seen := []string{}, false, map[string]bool{}
	for _, mark := range marks {
		if seen[mark] {
			continue
		}
		seen[mark] = true
		hit := false
		for _, line := range lines {
			if strings.Contains(line, mark) {
				hit = true
				break
			}
		}
		if hit {
			found = append(found, mark)
		} else {
			missing = true
		}
	}
	return found, missing
}

func evidenceRef(model *requestModel, id string) *VulnCardEvidenceRef {
	ref := &VulnCardEvidenceRef{EvidenceID: id}
	if model != nil {
		ref.Method, ref.URL = strings.ToUpper(model.Method), stripURLUserinfo(model.URL)
		if model.StatusCode.Valid {
			code := model.StatusCode.Int64
			ref.StatusCode = &code
		}
	}
	return ref
}

func (c *Capability) cardRepro(ctx context.Context, chosen *cardVerification) (*VulnCardRepro, error) {
	payload := chosen.stored.Payload
	repro := &VulnCardRepro{VerificationID: chosen.model.ID, TaskID: chosen.model.TaskID, FindingID: chosen.model.FindingID, SessionID: chosen.model.SessionID,
		Impact: payload.Impact, Preconditions: payload.Preconditions, Steps: make([]VulnCardStep, 0, len(payload.KeySteps)),
		Remediation: append([]string{}, payload.Remediation...), RetestMethod: payload.RetestMethod, SavedAt: chosen.result.CreatedAt}
	for _, step := range payload.KeySteps {
		model, err := c.store.evidenceByID(ctx, chosen.model.TaskID, step.EvidenceID)
		if err != nil {
			return nil, err
		}
		ref := evidenceRef(model, step.EvidenceID)
		view := VulnCardStep{Title: step.Title, EvidenceID: step.EvidenceID, Method: ref.Method, URL: ref.URL, StatusCode: ref.StatusCode,
			RequestMarksFound: []string{}, ResponseMarksFound: []string{}}
		var requestLines, responseLines []string
		if model != nil {
			if flow, err := decodeEvidenceFlow(model); err == nil {
				requestLines, responseLines = flowRequestLines(flow), flowResponseLines(flow)
			}
		}
		var requestMissing, responseMissing bool
		view.RequestMarksFound, requestMissing = marksIn(requestLines, step.RequestMarks)
		view.ResponseMarksFound, responseMissing = marksIn(responseLines, step.ResponseMarks)
		view.MarkMissing = requestMissing || responseMissing
		repro.Steps = append(repro.Steps, view)
	}
	if contrast := payload.Contrast; contrast != nil {
		view := &VulnCardContrast{NormalEvidenceID: contrast.NormalEvidenceID, AbnormalEvidenceID: contrast.AbnormalEvidenceID, Note: contrast.Note}
		for _, side := range []struct {
			id     string
			target **VulnCardEvidenceRef
		}{{contrast.NormalEvidenceID, &view.Normal}, {contrast.AbnormalEvidenceID, &view.Abnormal}} {
			model, err := c.store.evidenceByID(ctx, chosen.model.TaskID, side.id)
			if err != nil {
				return nil, err
			}
			*side.target = evidenceRef(model, side.id)
		}
		repro.Contrast = view
	}
	return repro, nil
}

// ---- 审计与推送 ----

func scanAuditRows(rows interface {
	Next() bool
	Scan(...any) error
}) ([]AuditEvent, error) {
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
	return out, nil
}

const auditColumns = `id, at, actor, action, target_kind, target_id, task_id, asset_id, summary, detail_json`

// vulnChanges：该条目的全部 vuln.* 审计（新到旧）。
func (s *taskStore) vulnChanges(ctx context.Context, key string) ([]AuditEvent, error) {
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) ([]AuditEvent, error) {
		rows, err := db.QueryContext(ctx, `SELECT `+auditColumns+` FROM webapi_audit_events
			WHERE target_kind = 'vuln' AND target_id = ? AND substr(action, 1, 5) = 'vuln.' ORDER BY at COLLATE "C" DESC, id COLLATE "C" DESC`, key)
		if err != nil {
			return nil, err
		}
		items, err := scanAuditRows(rows)
		if closeErr := closeRows(rows); err == nil {
			err = closeErr
		}
		return items, err
	})
}

// vulnNotifyEvents：该条目（去重键）的推送终态：sent 用 sent_at，failed 用审计时间（无则登记时间）；
// 人工推送（manual）见 manualNotifyEvents。
func (s *taskStore) vulnNotifyEvents(ctx context.Context, key string) ([]VulnCardEvent, error) {
	var items []notificationModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&items).Where("dedupe_key = ? AND (status IN (?) OR kind = ?)", key, bun.In([]string{NotifySent, NotifyFailed}), NotifyKindManual).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	out := []VulnCardEvent{}
	for _, item := range items {
		if item.Kind == NotifyKindManual {
			events, err := s.manualNotifyEvents(ctx, item)
			if err != nil {
				return nil, err
			}
			out = append(out, events...)
			continue
		}
		label := map[string]string{NotifyKindNew: "新确认漏洞", NotifyKindRegression: "漏洞复现"}[item.Kind]
		event := VulnCardEvent{Kind: "notify", Actor: auditActorSystem}
		if item.Status == NotifySent {
			event.At, event.Title, event.Detail = item.SentAt.String, "企业微信推送成功", label
		} else {
			event.At, event.Title, event.Detail = item.CreatedAt, "企业微信推送失败", capRunes(oneLine(item.LastError), vulnCardDetailRunes)
			var at string
			if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
				return db.QueryRowContext(ctx, `SELECT at FROM webapi_audit_events WHERE action = 'notification.failed' AND target_id = ? ORDER BY at COLLATE "C" DESC LIMIT 1`, item.ID).Scan(&at)
			}); err == nil {
				event.At = at
			}
		}
		if event.At == "" {
			event.At = item.CreatedAt
		}
		out = append(out, event)
	}
	return out, nil
}

// manualNotifyEvents：人工推送行在终态后可重新入队（复用同一行），故每次终态取投递审计
// notification.sent / notification.failed；没有审计时退回该行当前终态。
func (s *taskStore) manualNotifyEvents(ctx context.Context, item notificationModel) ([]VulnCardEvent, error) {
	audits, err := readValue(ctx, s, func(ctx context.Context, db bun.IDB) ([]AuditEvent, error) {
		rows, err := db.QueryContext(ctx, `SELECT `+auditColumns+` FROM webapi_audit_events
			WHERE target_kind = 'notification' AND target_id = ? AND action IN ('notification.sent', 'notification.failed') ORDER BY at COLLATE "C" ASC, id COLLATE "C" ASC`, item.ID)
		if err != nil {
			return nil, err
		}
		audits, err := scanAuditRows(rows)
		if closeErr := closeRows(rows); err == nil {
			err = closeErr
		}
		return audits, err
	})
	if err != nil {
		return nil, err
	}
	out := []VulnCardEvent{}
	for _, audit := range audits {
		event := VulnCardEvent{At: audit.At, Kind: "notify", Actor: auditActorSystem, Title: "企业微信人工推送成功", Detail: "漏洞通报"}
		if audit.Action == "notification.failed" {
			var detail struct {
				LastError string `json:"last_error"`
			}
			_ = json.Unmarshal(audit.Detail, &detail)
			event.Title, event.Detail = "企业微信人工推送失败", capRunes(oneLine(detail.LastError), vulnCardDetailRunes)
		}
		out = append(out, event)
	}
	if len(out) == 0 && (item.Status == NotifySent || item.Status == NotifyFailed) {
		event := VulnCardEvent{At: item.CreatedAt, Kind: "notify", Actor: auditActorSystem, Title: "企业微信人工推送成功", Detail: "漏洞通报"}
		if item.Status == NotifySent && item.SentAt.Valid {
			event.At = item.SentAt.String
		} else if item.Status == NotifyFailed {
			event.Title, event.Detail = "企业微信人工推送失败", capRunes(oneLine(item.LastError), vulnCardDetailRunes)
		}
		out = append(out, event)
	}
	return out, nil
}

// ---- 证据汇总 ----

type cardEvidenceSet struct {
	items []VulnCardEvidence
	seen  map[string]bool
}

func (set *cardEvidenceSet) add(taskID, source string, snapshot FindingEvidenceSnapshot) {
	key := taskID + "\x00" + snapshot.EvidenceID
	if snapshot.EvidenceID == "" || set.seen[key] {
		return
	}
	set.seen[key] = true
	set.items = append(set.items, VulnCardEvidence{EvidenceID: snapshot.EvidenceID, TaskID: taskID, Source: source, Relation: snapshot.Relation,
		Method: strings.ToUpper(snapshot.Method), URL: stripURLUserinfo(snapshot.URL), StatusCode: snapshot.HTTPStatus, CreatedAt: snapshot.CreatedAt})
}

func (c *Capability) cardEvidence(ctx context.Context, findingIDs []string, verifications []cardVerification, retests []cardRetest, repro *VulnCardRepro) (*cardEvidenceSet, error) {
	set := &cardEvidenceSet{items: []VulnCardEvidence{}, seen: map[string]bool{}}
	if len(findingIDs) > 0 {
		if err := c.store.queryRows(ctx, func(rows *sql.Rows) error {
			var taskID, payload string
			if err := rows.Scan(&taskID, &payload); err != nil {
				return err
			}
			var stored storedFindingPayload
			if json.Unmarshal([]byte(payload), &stored) == nil {
				for _, snapshot := range stored.Evidence {
					set.add(taskID, "candidate", snapshot)
				}
			}
			return nil
		}, `SELECT f.task_id, r.payload_json FROM webapi_findings f
			JOIN webapi_finding_revisions r ON r.id = f.current_revision_id WHERE f.id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(findingIDs)), ",")+`) ORDER BY f.number ASC`, stringArgs(findingIDs)...); err != nil {
			return nil, err
		}
	}
	for _, item := range verifications {
		if item.stored != nil {
			for _, snapshot := range item.stored.Evidence {
				set.add(item.model.TaskID, "verification", snapshot)
			}
		}
	}
	if repro != nil {
		for _, step := range repro.Steps {
			set.add(repro.TaskID, "verification", FindingEvidenceSnapshot{EvidenceID: step.EvidenceID, Relation: "key_step", Method: step.Method, URL: step.URL, HTTPStatus: step.StatusCode})
		}
		if contrast := repro.Contrast; contrast != nil {
			for _, ref := range []*VulnCardEvidenceRef{contrast.Normal, contrast.Abnormal} {
				set.add(repro.TaskID, "verification", FindingEvidenceSnapshot{EvidenceID: ref.EvidenceID, Relation: "contrast", Method: ref.Method, URL: ref.URL, HTTPStatus: ref.StatusCode})
			}
		}
	}
	for _, item := range retests {
		for _, snapshot := range item.stored.Evidence {
			set.add(item.model.TaskID, "retest", snapshot)
		}
	}
	return set, nil
}

func stringArgs(values []string) []any {
	out := make([]any, len(values))
	for index, value := range values {
		out[index] = value
	}
	return out
}

// ---- 组装 ----

func (c *Capability) buildVulnCard(ctx context.Context, key string) (*VulnCard, error) {
	item, ordered, err := c.cardTarget(ctx, key)
	if err != nil {
		return nil, err
	}
	return c.buildVulnCardFor(ctx, item, ordered)
}

func (c *Capability) buildVulnCardFor(ctx context.Context, item *GlobalVuln, ordered []*GlobalVuln) (*VulnCard, error) {
	findingIDs := memberFindingIDs(item)
	verifications, err := c.store.cardVerifications(ctx, findingIDs)
	if err != nil {
		return nil, err
	}
	retests, err := c.store.cardRetests(ctx, findingIDs)
	if err != nil {
		return nil, err
	}
	card := &VulnCard{Triage: item.Triage, Lifecycle: []VulnCardEvent{}, Related: []VulnCardRelated{}}

	// 标题区
	header := VulnCardHeader{Key: item.ID, Code: vulnCode(item.ID), Title: item.Title, Status: item.Status, Severity: item.Triage.Severity,
		Disposition: item.Triage.Disposition, DueEffective: item.Triage.DueEffective, Overdue: item.Triage.Overdue, FirstSeenAt: item.FirstSeenAt, EndpointKey: item.EndpointKey}
	header.SuggestedSeverity, header.SeverityReason = suggestedOf(verifications)
	assetName := ""
	if item.assetID != "" {
		asset, err := c.store.assetModel(ctx, item.assetID)
		if err != nil {
			return nil, err
		}
		if asset != nil {
			origins, _ := decodeOrigins(asset.OriginsJSON)
			if origins == nil {
				origins = []string{}
			}
			header.Asset = &VulnCardAsset{ID: asset.ID, Name: asset.Name, Env: asset.Environment, Owner: asset.Owner, Origins: origins}
			assetName = asset.Name
		}
	}
	header.TitleTemplate = vulnTitleTemplate(assetName, item.EndpointKey, item.Title)
	card.Header = header

	// 最小复现
	if chosen, reason := pickRepro(verifications); chosen != nil {
		if card.Repro, err = c.cardRepro(ctx, chosen); err != nil {
			return nil, err
		}
	} else {
		card.ReproMissingReason = &reason
	}

	// 复测
	if len(retests) > 0 {
		latest := retests[0]
		card.Retest = &VulnCardRetest{RetestID: latest.model.ID, TaskID: latest.model.TaskID, FindingID: latest.model.FindingID, SessionID: latest.model.SessionID,
			Conclusion: latest.stored.Payload.Conclusion, Summary: latest.stored.Payload.Summary, At: latest.result.CreatedAt}
	}

	// 附录与生命周期
	numbers := map[string]string{}
	for _, member := range item.members {
		numbers[member.FindingID] = member.Number
		card.Lifecycle = append(card.Lifecycle, VulnCardEvent{At: member.CreatedAt, Kind: "candidate", Title: "AI 发现候选 " + member.Number,
			Detail: capRunes(oneLine(member.Title), vulnCardDetailRunes), Actor: "ai"})
	}
	summaries := make([]VulnCardVerification, 0, len(verifications))
	for _, v := range verifications {
		summary := VulnCardVerification{VerificationID: v.model.ID, TaskID: v.model.TaskID, FindingID: v.model.FindingID, FindingNumber: numbers[v.model.FindingID],
			SessionID: v.model.SessionID, CreatedAt: v.model.CreatedAt}
		if v.stored != nil {
			payload := v.stored.Payload
			verdict, savedAt := payload.Verdict, v.result.CreatedAt
			summary.Verdict, summary.SavedAt = &verdict, &savedAt
			summary.Method, summary.Preconditions, summary.Summary, summary.Limitations = payload.Method, payload.Preconditions, payload.Summary, payload.Limitations
			summary.SuggestedSeverity, summary.SeverityReason, summary.HasRepro, summary.EvidenceCount = payload.SuggestedSeverity, payload.SeverityReason, payload.hasRepro(), len(v.stored.Evidence)
			card.Lifecycle = append(card.Lifecycle, VulnCardEvent{At: savedAt, Kind: "verification", Title: "独立验证：" + labelOr(verdictLabels, verdict),
				Detail: capRunes(oneLine(payload.Summary), vulnCardDetailRunes), Actor: "ai"})
		}
		summaries = append(summaries, summary)
	}
	for _, r := range retests {
		card.Lifecycle = append(card.Lifecycle, VulnCardEvent{At: r.result.CreatedAt, Kind: "retest", Title: "复测：" + labelOr(conclusionLabels, r.stored.Payload.Conclusion),
			Detail: capRunes(oneLine(r.stored.Payload.Summary), vulnCardDetailRunes), Actor: "ai"})
	}
	changes, err := c.store.vulnChanges(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		if change.Action == "vuln.triage" {
			card.Lifecycle = append(card.Lifecycle, VulnCardEvent{At: change.At, Kind: "triage", Title: "人工定级/处置", Detail: capRunes(change.Summary, vulnCardDetailRunes), Actor: change.Actor})
		}
	}
	requestEvents := []VulnCardEvent{}
	if card.RetestRequest, requestEvents, err = c.cardRetestRequest(ctx, changes); err != nil {
		return nil, err
	}
	card.Lifecycle = append(card.Lifecycle, requestEvents...)
	notifyEvents, err := c.store.vulnNotifyEvents(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	card.Lifecycle = append(card.Lifecycle, notifyEvents...)
	replays, replayEvents, err := c.cardReplays(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	card.Replays = replays
	card.Lifecycle = append(card.Lifecycle, replayEvents...)
	if card.Notify, err = c.cardNotify(ctx, item.ID); err != nil {
		return nil, err
	}
	if item.FixedAt != nil {
		card.Lifecycle = append(card.Lifecycle, VulnCardEvent{At: *item.FixedAt, Kind: "fixed", Title: "已修复", Detail: "复测结论为范围内已修复", Actor: auditActorSystem})
	}
	for index := range card.Lifecycle {
		card.Lifecycle[index].at = parseAt(card.Lifecycle[index].At)
	}
	sort.SliceStable(card.Lifecycle, func(i, j int) bool {
		a, b := card.Lifecycle[i], card.Lifecycle[j]
		if !a.at.Equal(b.at) {
			return a.at.Before(b.at)
		}
		return lifecycleKindOrder[a.Kind] < lifecycleKindOrder[b.Kind]
	})

	evidence, err := c.cardEvidence(ctx, findingIDs, verifications, retests, card.Repro)
	if err != nil {
		return nil, err
	}
	members := item.members
	if members == nil {
		members = []AssetVulnMember{}
	}
	listed := evidence.items
	if len(listed) > vulnCardEvidenceMax {
		listed = listed[:vulnCardEvidenceMax]
	}
	card.Appendix = VulnCardAppendix{Verifications: summaries, Members: members, EvidenceCount: len(evidence.items), Evidence: listed, Changes: changes}

	// 同资产其他未修复条目与上一条/下一条（全局默认排序）
	for index, other := range ordered {
		if other.ID == item.ID {
			if index > 0 {
				prev := ordered[index-1].ID
				card.Nav.PrevKey = &prev
			}
			if index+1 < len(ordered) {
				next := ordered[index+1].ID
				card.Nav.NextKey = &next
			}
			continue
		}
		if item.assetID != "" && other.assetID == item.assetID && other.Status != VulnFixed && len(card.Related) < vulnCardRelatedMax {
			severity := ""
			if other.Triage != nil {
				severity = other.Triage.Severity
			}
			card.Related = append(card.Related, VulnCardRelated{Key: other.ID, Code: vulnCode(other.ID), Title: other.Title, Severity: severity, SuggestedSeverity: other.SuggestedSeverity, Status: other.Status})
		}
	}
	return card, nil
}

func labelOr(labels map[string]string, value string) string {
	if label, ok := labels[value]; ok {
		return label
	}
	return value
}

// ---- HTTP ----

func vulnCardKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := r.PathValue("key")
	if validPathID(key) != nil {
		writeError(w, http.StatusBadRequest, "漏洞条目 id 非法")
		return "", false
	}
	return key, true
}

// handleVulnCard: GET /api/webapi/vulns/{key}/card（只读）。
func (c *Capability) handleVulnCard(w http.ResponseWriter, r *http.Request) {
	key, ok := vulnCardKey(w, r)
	if !ok {
		return
	}
	card, err := c.buildVulnCard(r.Context(), key)
	if errors.Is(err, errTriageNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞卡失败")
		return
	}
	writeJSON(w, http.StatusOK, card)
}

// handleVulnCardCurl: GET /api/webapi/vulns/{key}/card/curl?step=N&mask=0|1 → text/plain。
// step 从 1 开始（默认 1）；默认遮盖敏感头与资产测试账号 secret，mask=0 不遮盖并登记审计 vuln.curl_unmasked。
func (c *Capability) handleVulnCardCurl(w http.ResponseWriter, r *http.Request) {
	key, ok := vulnCardKey(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	step := 1
	if raw := query.Get("step"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxReproSteps {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("step 必须是 1..%d 的整数", maxReproSteps))
			return
		}
		step = parsed
	}
	mask := true
	switch query.Get("mask") {
	case "", "1":
	case "0":
		mask = false
	default:
		writeError(w, http.StatusBadRequest, "mask 只能是 0 或 1")
		return
	}
	ctx := r.Context()
	target, code, message := c.locateReproStep(ctx, key, step)
	if target == nil {
		writeError(w, code, message)
		return
	}
	item, chosen, keyStep, flow := target.item, target.chosen, target.keyStep, target.flow
	var secrets []string
	if mask {
		var err error
		if _, secrets, err = c.notifyAssetContext(ctx, chosen.model.TaskID); err != nil {
			writeError(w, http.StatusInternalServerError, "读取测试账号失败，无法遮盖")
			return
		}
	}
	text := buildCurlCommand(flow, mask, secrets)
	if !mask {
		c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.curl_unmasked", TargetKind: "vuln", TargetID: key, TaskID: chosen.model.TaskID, AssetID: item.assetID,
			Summary: fmt.Sprintf("导出未遮盖的复现 curl：「%s」第 %d 步", item.Title, step),
			Detail:  map[string]any{"vuln_key": key, "step": step, "evidence_id": keyStep.EvidenceID, "verification_id": chosen.model.ID}})
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(text))
}

// reproStepTarget 是 repro 第 N 步证据原件（card/curl 与 card/replay 共用定位）。
type reproStepTarget struct {
	item    *GlobalVuln
	chosen  *cardVerification
	keyStep verificationKeyStep
	model   *requestModel
	flow    *traffic.Flow
}

// locateReproStep：条目不存在、无最小复现、证据不存在 → 404；step 超出步数 → 400；原件无法解析 → 422；其余 500。
func (c *Capability) locateReproStep(ctx context.Context, key string, step int) (*reproStepTarget, int, string) {
	item, _, err := c.cardTarget(ctx, key)
	if errors.Is(err, errTriageNotFound) {
		return nil, http.StatusNotFound, err.Error()
	}
	if err != nil {
		return nil, http.StatusInternalServerError, "读取漏洞条目失败"
	}
	verifications, err := c.store.cardVerifications(ctx, memberFindingIDs(item))
	if err != nil {
		return nil, http.StatusInternalServerError, "读取验证结果失败"
	}
	chosen, _ := pickRepro(verifications)
	if chosen == nil {
		return nil, http.StatusNotFound, "该漏洞尚无最小复现"
	}
	steps := chosen.stored.Payload.KeySteps
	if step > len(steps) {
		return nil, http.StatusBadRequest, fmt.Sprintf("step 超出范围：最小复现共 %d 步", len(steps))
	}
	keyStep := steps[step-1]
	model, err := c.store.evidenceByID(ctx, chosen.model.TaskID, keyStep.EvidenceID)
	if err != nil {
		return nil, http.StatusInternalServerError, "读取证据失败"
	}
	if model == nil {
		return nil, http.StatusNotFound, "关键步骤证据不存在"
	}
	flow, err := decodeEvidenceFlow(model)
	if err != nil || flow.GetRequest() == nil {
		return nil, http.StatusUnprocessableEntity, "证据原件无法解析"
	}
	return &reproStepTarget{item: item, chosen: chosen, keyStep: keyStep, model: model, flow: flow}, 0, ""
}

// ---- curl ----

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func curlMethodToken(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "GET"
	}
	for _, r := range method {
		if (r < 'A' || r > 'Z') && r != '-' && r != '_' {
			return shellQuote(method)
		}
	}
	return method
}

// buildCurlCommand 由证据 Flow 请求确定性生成 curl：`curl -sS -X METHOD 'URL'`，按原始顺序 -H（跳过 HTTP/2 伪头与
// Content-Length，由 curl 计算），正文用 --data-binary（二进制正文不内联，改为注释说明）。
// mask=true 时敏感头（Authorization/Cookie/Set-Cookie 等，与证据图片同一集合）值替换为 ******，
// URL 去 userinfo，URL/头/正文中的资产测试账号 secret 替换为 ******。
func buildCurlCommand(flow *traffic.Flow, mask bool, secrets []string) string {
	request := flow.GetRequest()
	rawURL, masked := request.GetUrl(), false
	if mask {
		scrubbed := scrubText(stripURLUserinfo(rawURL), secrets)
		masked, rawURL = scrubbed != rawURL, scrubbed
	}
	comments := []string{}
	parts := []string{"curl -sS -X " + curlMethodToken(request.GetMethod()) + " " + shellQuote(rawURL)}
	for _, header := range request.GetHeaders() {
		name, value := header.GetName(), header.GetValue()
		if name == "" || strings.HasPrefix(name, ":") || strings.EqualFold(name, "Content-Length") {
			continue
		}
		if mask {
			original := value
			if evidenceMaskedHeaders[strings.ToLower(name)] {
				value = vulnCurlMaskValue
			}
			value = scrubText(value, secrets)
			masked = masked || value != original
		}
		parts = append(parts, "  -H "+shellQuote(name+": "+value))
	}
	if body := request.GetBody(); len(body) > 0 {
		if evidenceBodyIsBinary(body) {
			comments = append(comments, fmt.Sprintf("# 请求体为二进制（%d 字节），未内联", len(body)))
		} else {
			if mask {
				scrubbed := scrubBytes(body, secrets)
				masked = masked || string(scrubbed) != string(body)
				body = scrubbed
			}
			parts = append(parts, "  --data-binary "+shellQuote(string(body)))
		}
	}
	if masked {
		comments = append(comments, "# 已遮盖敏感头与测试账号 secret（******），重放前请替换为有效值")
	}
	out := strings.Join(parts, " \\\n") + "\n"
	if len(comments) > 0 {
		out = strings.Join(comments, "\n") + "\n" + out
	}
	return out
}
