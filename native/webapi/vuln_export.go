package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/chainreactors/cyber/aop/traffic"
)

// 漏洞报告导出（K2）：由漏洞卡确定性生成单条/多条报告（Markdown 或 JSON），不调用模型、不含生成时间等易变字段，
// 只导出遮盖版（敏感头与测试账号 secret 遮盖规则同 card/curl 默认）。契约见 native/VULN_CARD.md「漏洞报告导出」。

const (
	vulnExportSchema       = "wscyber.vuln_report.v1"
	vulnExportMaxKeys      = 50
	vulnExportBodyLimit    = 16 << 10
	vulnExportMaxReplays   = replayCardMax
	vulnExportDeclaration  = "由 WScyber 依据漏洞卡生成；AI 判读与建议等级不等于人工确认；原文已遮盖敏感头与测试账号口令"
	vulnExportMarkMissing  = "部分片段在原件中未找到"
	vulnExportNotProvided  = "未提供"
	evidenceStatusOK       = "ok"
	evidenceStatusMissing  = "missing"
	evidenceStatusBadFlow  = "unparsable"
	vulnExportMaskedMarkup = "******"
)

var (
	vulnStatusLabels   = map[string]string{VulnNew: "新发现", VulnPresent: "仍存在", VulnPending: "待验证", VulnUnconfirmed: "未支持", VulnFixed: "已修复"}
	reproMissingLabels = map[string]string{reproMissingNone: "还没有独立验证判读", reproMissingNoSupp: "独立验证未得出“支持”判读", reproMissingNoSteps: "该判读产生于最小复现功能之前"}
)

// ---- JSON 结构（wscyber.vuln_report.v1） ----

type VulnExport struct {
	Schema string           `json:"schema"`
	Items  []VulnExportItem `json:"items"`
}

type VulnExportAsset struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Env   string `json:"env"`
	Owner string `json:"owner"`
}

type VulnExportItem struct {
	Key                string                  `json:"key"`
	Code               string                  `json:"code"`
	Title              string                  `json:"title"`
	TitleTemplate      string                  `json:"title_template"`
	Asset              *VulnExportAsset        `json:"asset"`
	EndpointKey        string                  `json:"endpoint_key"`
	Location           string                  `json:"location"`
	Severity           string                  `json:"severity"`
	SuggestedSeverity  string                  `json:"suggested_severity"`
	SeverityReason     string                  `json:"severity_reason"`
	Status             string                  `json:"status"`
	Disposition        string                  `json:"disposition"`
	Owner              string                  `json:"owner"`
	DueEffective       *string                 `json:"due_effective"`
	Overdue            bool                    `json:"overdue"`
	FirstSeenAt        string                  `json:"first_seen_at"`
	Repro              *VulnExportRepro        `json:"repro"`
	ReproMissingReason *string                 `json:"repro_missing_reason"`
	Retest             *VulnExportRetest       `json:"retest"`
	Replays            []VulnExportReplay      `json:"replays"`
	Verifications      []VulnExportVerdictItem `json:"verifications"`
}

type VulnExportRepro struct {
	VerificationID string              `json:"verification_id"`
	TaskID         string              `json:"task_id"`
	Impact         string              `json:"impact"`
	Preconditions  string              `json:"preconditions"`
	Steps          []VulnExportStep    `json:"steps"`
	Contrast       *VulnExportContrast `json:"contrast"`
	Remediation    []string            `json:"remediation"`
	RetestMethod   string              `json:"retest_method"`
	SavedAt        string              `json:"saved_at"`
}

type VulnExportStep struct {
	Index                 int      `json:"index"`
	Title                 string   `json:"title"`
	EvidenceID            string   `json:"evidence_id"`
	EvidenceStatus        string   `json:"evidence_status"` // ok | missing | unparsable
	Method                string   `json:"method"`
	URL                   string   `json:"url"`
	StatusCode            *int64   `json:"status_code"`
	RequestMarksFound     []string `json:"request_marks_found"`
	ResponseMarksFound    []string `json:"response_marks_found"`
	MarkMissing           bool     `json:"mark_missing"`
	RequestRaw            string   `json:"request_raw"`
	RequestBodyBytes      int      `json:"request_body_bytes"`
	RequestBodyBinary     bool     `json:"request_body_binary"`
	RequestBodyTruncated  bool     `json:"request_body_truncated"`
	ResponseRaw           string   `json:"response_raw"`
	ResponseBodyBytes     int      `json:"response_body_bytes"`
	ResponseBodyBinary    bool     `json:"response_body_binary"`
	ResponseBodyTruncated bool     `json:"response_body_truncated"`
	CaptureTruncated      bool     `json:"capture_truncated"`
	Curl                  string   `json:"curl"`
}

type VulnExportEndpoint struct {
	EvidenceID string `json:"evidence_id"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	StatusCode *int64 `json:"status_code"`
}

type VulnExportContrast struct {
	Note     string              `json:"note"`
	Normal   *VulnExportEndpoint `json:"normal"`
	Abnormal *VulnExportEndpoint `json:"abnormal"`
}

type VulnExportRetest struct {
	RetestID   string `json:"retest_id"`
	Conclusion string `json:"conclusion"`
	Summary    string `json:"summary"`
	At         string `json:"at"`
}

type VulnExportReplay struct {
	ReplayID    string `json:"replay_id"`
	Step        int64  `json:"step"`
	EvidenceID  string `json:"evidence_id"`
	At          string `json:"at"`
	StatusCode  int64  `json:"status_code"`
	VerdictHint string `json:"verdict_hint"`
}

type VulnExportVerdictItem struct {
	VerificationID    string `json:"verification_id"`
	FindingNumber     string `json:"finding_number"`
	Verdict           string `json:"verdict"`
	Method            string `json:"method"`
	Summary           string `json:"summary"`
	Limitations       string `json:"limitations"`
	SuggestedSeverity string `json:"suggested_severity"`
	SavedAt           string `json:"saved_at"`
}

// ---- 组装 ----

// exportMasker 遮盖导出文本：测试账号 secret（≥4 字）→ ******。
type exportMasker struct{ secrets []string }

func (m exportMasker) text(value string) string { return scrubText(value, m.secrets) }

func (m exportMasker) list(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, m.text(value))
	}
	return out
}

// buildVulnExportItem 由漏洞卡生成一条导出项（全部文字字段已遮盖测试账号 secret）。
func (c *Capability) buildVulnExportItem(ctx context.Context, card *VulnCard) (*VulnExportItem, error) {
	header := card.Header
	masker := exportMasker{}
	if header.Asset != nil {
		secrets, err := c.assetSecrets(ctx, header.Asset.ID)
		if err != nil {
			return nil, err
		}
		masker.secrets = secrets
	}
	item := &VulnExportItem{Key: header.Key, Code: header.Code, Title: masker.text(header.Title), TitleTemplate: masker.text(header.TitleTemplate),
		EndpointKey: masker.text(header.EndpointKey), Location: masker.text(exportLocation(header.EndpointKey)), Severity: header.Severity,
		SuggestedSeverity: header.SuggestedSeverity, SeverityReason: masker.text(header.SeverityReason), Status: header.Status, Disposition: header.Disposition,
		DueEffective: header.DueEffective, Overdue: header.Overdue, FirstSeenAt: header.FirstSeenAt, ReproMissingReason: card.ReproMissingReason,
		Replays: []VulnExportReplay{}, Verifications: []VulnExportVerdictItem{}}
	if card.Triage != nil {
		item.Owner = card.Triage.OwnerEffective
	}
	if asset := header.Asset; asset != nil {
		item.Asset = &VulnExportAsset{ID: asset.ID, Name: asset.Name, Env: asset.Env, Owner: asset.Owner}
	}
	if repro := card.Repro; repro != nil {
		out := &VulnExportRepro{VerificationID: repro.VerificationID, TaskID: repro.TaskID, Impact: masker.text(repro.Impact), Preconditions: masker.text(repro.Preconditions),
			Steps: make([]VulnExportStep, 0, len(repro.Steps)), Remediation: masker.list(repro.Remediation), RetestMethod: masker.text(repro.RetestMethod), SavedAt: repro.SavedAt}
		for index, step := range repro.Steps {
			exported, err := c.exportStep(ctx, repro.TaskID, index+1, step, masker)
			if err != nil {
				return nil, err
			}
			out.Steps = append(out.Steps, exported)
		}
		if contrast := repro.Contrast; contrast != nil {
			out.Contrast = &VulnExportContrast{Note: masker.text(contrast.Note), Normal: exportEndpoint(contrast.Normal, masker), Abnormal: exportEndpoint(contrast.Abnormal, masker)}
		}
		item.Repro = out
	}
	if retest := card.Retest; retest != nil {
		item.Retest = &VulnExportRetest{RetestID: retest.RetestID, Conclusion: retest.Conclusion, Summary: masker.text(retest.Summary), At: retest.At}
	}
	for _, replay := range card.Replays {
		if len(item.Replays) == vulnExportMaxReplays {
			break
		}
		item.Replays = append(item.Replays, VulnExportReplay{ReplayID: replay.ReplayID, Step: replay.Step, EvidenceID: replay.EvidenceID, At: replay.At, StatusCode: replay.StatusCode, VerdictHint: replay.VerdictHint})
	}
	for _, v := range card.Appendix.Verifications {
		if v.Verdict == nil {
			continue // 未出结果的验证不是判读，不导出
		}
		item.Verifications = append(item.Verifications, VulnExportVerdictItem{VerificationID: v.VerificationID, FindingNumber: v.FindingNumber, Verdict: *v.Verdict,
			Method: masker.text(v.Method), Summary: masker.text(v.Summary), Limitations: masker.text(v.Limitations), SuggestedSeverity: v.SuggestedSeverity, SavedAt: stringOr(v.SavedAt)})
	}
	return item, nil
}

func stringOr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// exportLocation：端点键（`METHOD origin/规范化路径`）；无端点（finding: 或空）为空串。
func exportLocation(endpointKey string) string {
	if endpointKey == "" || strings.HasPrefix(endpointKey, "finding:") {
		return ""
	}
	return endpointKey
}

func exportEndpoint(ref *VulnCardEvidenceRef, masker exportMasker) *VulnExportEndpoint {
	if ref == nil {
		return nil
	}
	return &VulnExportEndpoint{EvidenceID: ref.EvidenceID, Method: ref.Method, URL: masker.text(ref.URL), StatusCode: ref.StatusCode}
}

// exportStep 读该步证据原件，生成遮盖后的请求/响应原文、正文截断与二进制标志、遮盖版 curl。
func (c *Capability) exportStep(ctx context.Context, taskID string, index int, step VulnCardStep, masker exportMasker) (VulnExportStep, error) {
	out := VulnExportStep{Index: index, Title: masker.text(step.Title), EvidenceID: step.EvidenceID, EvidenceStatus: evidenceStatusMissing, Method: step.Method,
		URL: masker.text(step.URL), StatusCode: step.StatusCode, RequestMarksFound: []string{}, ResponseMarksFound: []string{}, MarkMissing: step.MarkMissing}
	model, err := c.store.evidenceByID(ctx, taskID, step.EvidenceID)
	if err != nil {
		return out, err
	}
	var flow *traffic.Flow
	if model != nil {
		out.EvidenceStatus, out.CaptureTruncated = evidenceStatusBadFlow, model.CaptureTruncated
		if decoded, err := decodeEvidenceFlow(model); err == nil && decoded.GetRequest() != nil {
			flow, out.EvidenceStatus = decoded, evidenceStatusOK
		}
	}
	sensitive := sensitiveHeaderValues(flow)
	out.RequestMarksFound = exportMarks(step.RequestMarksFound, sensitive, masker)
	out.ResponseMarksFound = exportMarks(step.ResponseMarksFound, sensitive, masker)
	if flow == nil {
		return out, nil
	}
	request := flow.GetRequest()
	out.RequestRaw, out.RequestBodyBytes, out.RequestBodyBinary, out.RequestBodyTruncated = exportRaw(
		masker.text(requestLineText(request)), request.GetHeaders(), request.GetBody(), masker)
	if response := flow.GetResponse(); response != nil && response.GetStatusCode() != 0 {
		out.ResponseRaw, out.ResponseBodyBytes, out.ResponseBodyBinary, out.ResponseBodyTruncated = exportRaw(
			statusLineText(request, response), response.GetHeaders(), response.GetBody(), masker)
	}
	out.Curl = buildCurlCommand(flow, true, masker.secrets)
	return out, nil
}

// exportRaw 生成遮盖后的 HTTP 原文：首行、原始头（敏感头值 ******，测试账号 secret ******）、空行、正文。
// 二进制正文不内联（raw 只到空行）；正文（遮盖后）超过 16 KiB 截到 16 KiB（UTF-8 边界）。返回原件正文字节数与标志。
func exportRaw(first string, headers []*traffic.Header, body []byte, masker exportMasker) (string, int, bool, bool) {
	lines := []string{first}
	for _, header := range headers {
		value := header.GetValue()
		if evidenceMaskedHeaders[strings.ToLower(header.GetName())] {
			value = vulnExportMaskedMarkup
		}
		lines = append(lines, header.GetName()+": "+masker.text(value))
	}
	raw := strings.Join(lines, "\n") + "\n"
	size := len(body)
	if size == 0 {
		return raw, 0, false, false
	}
	if evidenceBodyIsBinary(body) {
		return raw, size, true, false
	}
	text := masker.text(string(body))
	truncated := false
	if len(text) > vulnExportBodyLimit {
		cut := vulnExportBodyLimit
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text, truncated = text[:cut], true
	}
	return raw + "\n" + text, size, false, truncated
}

// sensitiveHeaderValues：原件中敏感头（请求与响应）的非空值；落在其中的高亮片段导出时遮盖。
func sensitiveHeaderValues(flow *traffic.Flow) []string {
	if flow == nil {
		return nil
	}
	out := []string{}
	for _, headers := range [][]*traffic.Header{flow.GetRequest().GetHeaders(), flow.GetResponse().GetHeaders()} {
		for _, header := range headers {
			if value := header.GetValue(); value != "" && evidenceMaskedHeaders[strings.ToLower(header.GetName())] {
				out = append(out, value)
			}
		}
	}
	return out
}

// exportMarks：片段与敏感头值互相包含时整体替换为 ******（避免经高亮列表泄露 Cookie/Authorization），再遮盖测试账号 secret。
func exportMarks(marks []string, sensitive []string, masker exportMasker) []string {
	out := make([]string, 0, len(marks))
	for _, mark := range marks {
		for _, value := range sensitive {
			if strings.Contains(value, mark) || strings.Contains(mark, value) {
				mark = vulnExportMaskedMarkup
				break
			}
		}
		out = append(out, masker.text(mark))
	}
	return out
}

// ---- Markdown ----

func (c *Capability) renderVulnExportMarkdown(items []*VulnExportItem) string {
	var b strings.Builder
	b.WriteString("> " + vulnExportDeclaration + "\n")
	if len(items) > 1 {
		fmt.Fprintf(&b, "\n## 汇总（%d 条）\n\n| 编号 | 标题 | 资产 | 等级 | 状态 | 处置 | 期限 |\n|---|---|---|---|---|---|---|\n", len(items))
		for _, item := range items {
			cells := []string{item.Code, item.Title, exportAssetName(item), exportSeverity(item), labelOr(vulnStatusLabels, item.Status), labelOr(dispositionLabels, item.Disposition), exportDue(item)}
			for index := range cells {
				cells[index] = tableCell(cells[index])
			}
			b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
		}
	}
	for _, item := range items {
		b.WriteString("\n")
		c.renderVulnExportItem(&b, item)
	}
	return b.String()
}

func tableCell(value string) string {
	return strings.ReplaceAll(oneLine(value), "|", `\|`)
}

func exportAssetName(item *VulnExportItem) string {
	if item.Asset == nil {
		return "未关联资产"
	}
	return item.Asset.Name
}

func exportSeverity(item *VulnExportItem) string {
	if item.Severity != "" {
		return labelOr(severityLabels, item.Severity)
	}
	if item.SuggestedSeverity != "" {
		return "未定级（AI 建议：" + labelOr(severityLabels, item.SuggestedSeverity) + "）"
	}
	return "未定级"
}

func exportDue(item *VulnExportItem) string {
	if item.DueEffective == nil {
		return "无"
	}
	if item.Overdue {
		return *item.DueEffective + "（已逾期）"
	}
	return *item.DueEffective
}

func orText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// codeFence：围栏长度大于原文中最长连续反引号（至少 3）。
func codeFence(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

func writeCodeBlock(b *strings.Builder, lang, text string) {
	fence := codeFence(text)
	b.WriteString(fence + lang + "\n" + text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(fence + "\n")
}

func statusText(code *int64) string {
	if code == nil {
		return "无状态码"
	}
	return fmt.Sprint(*code)
}

func (c *Capability) renderVulnExportItem(b *strings.Builder, item *VulnExportItem) {
	fmt.Fprintf(b, "## %s %s\n\n", item.Code, oneLine(item.TitleTemplate))
	fmt.Fprintf(b, "- 资产：%s\n- 位置：%s\n- 等级：%s\n- 状态：%s\n- 处置：%s\n- 负责人：%s\n- 修复期限：%s\n- 首次发现：%s\n",
		exportAssetName(item), orText(item.Location, "未记录"), exportSeverity(item), labelOr(vulnStatusLabels, item.Status), labelOr(dispositionLabels, item.Disposition),
		orText(item.Owner, "未指定"), exportDue(item), c.notifyLocalTime(item.FirstSeenAt))
	if repro := item.Repro; repro != nil {
		fmt.Fprintf(b, "\n### 影响\n\n%s\n", orText(repro.Impact, vulnExportNotProvided))
		fmt.Fprintf(b, "\n### 前置条件\n\n%s\n", orText(repro.Preconditions, "无"))
		b.WriteString("\n### 复现步骤\n")
		for _, step := range repro.Steps {
			renderExportStep(b, step)
		}
		b.WriteString("\n### 正常对照\n\n")
		if contrast := repro.Contrast; contrast != nil {
			b.WriteString(orText(contrast.Note, "（无说明）") + "\n\n")
			for _, side := range []struct {
				label string
				ref   *VulnExportEndpoint
			}{{"正常", contrast.Normal}, {"异常", contrast.Abnormal}} {
				if side.ref == nil {
					continue
				}
				if side.ref.Method == "" {
					fmt.Fprintf(b, "- %s：证据 %s 不存在\n", side.label, side.ref.EvidenceID)
					continue
				}
				fmt.Fprintf(b, "- %s：%s → %s\n", side.label, codeSpan(side.ref.Method+" "+side.ref.URL), statusText(side.ref.StatusCode))
			}
		} else {
			b.WriteString(vulnExportNotProvided + "\n")
		}
		b.WriteString("\n### 修复建议\n\n")
		if len(repro.Remediation) == 0 {
			b.WriteString(vulnExportNotProvided + "\n")
		}
		for index, text := range repro.Remediation {
			fmt.Fprintf(b, "%d. %s\n", index+1, oneLine(text))
		}
		fmt.Fprintf(b, "\n### 复测方法\n\n%s\n", orText(repro.RetestMethod, vulnExportNotProvided))
	} else {
		reason := ""
		if item.ReproMissingReason != nil {
			reason = labelOr(reproMissingLabels, *item.ReproMissingReason)
		}
		fmt.Fprintf(b, "\n最小复现：未提供（%s）\n", orText(reason, "原因未知"))
	}
	b.WriteString("\n### 最近复测结论\n\n")
	if retest := item.Retest; retest != nil {
		fmt.Fprintf(b, "%s（%s）：%s\n", labelOr(conclusionLabels, retest.Conclusion), c.notifyLocalTime(retest.At), orText(oneLine(retest.Summary), "无摘要"))
	} else {
		b.WriteString("暂无复测\n")
	}
	b.WriteString("\n### 最近重放\n\n")
	if len(item.Replays) == 0 {
		b.WriteString("暂无重放\n")
	}
	for _, replay := range item.Replays {
		fmt.Fprintf(b, "- %s 第 %d 步 → %d，%s（证据 %s）\n", c.notifyLocalTime(replay.At), replay.Step, replay.StatusCode, labelOr(replayHintLabels, replay.VerdictHint), replay.EvidenceID)
	}
	b.WriteString("\n### 验证记录\n\n")
	if len(item.Verifications) == 0 {
		b.WriteString("暂无判读\n")
	}
	for _, v := range item.Verifications {
		number := ""
		if v.FindingNumber != "" {
			number = v.FindingNumber + " · "
		}
		fmt.Fprintf(b, "- %s%s（%s）：%s\n  - 方法：%s\n  - 局限：%s\n", number, labelOr(verdictLabels, v.Verdict), c.notifyLocalTime(v.SavedAt),
			orText(oneLine(v.Summary), "无摘要"), orText(oneLine(v.Method), vulnExportNotProvided), orText(oneLine(v.Limitations), "无"))
	}
}

func renderExportStep(b *strings.Builder, step VulnExportStep) {
	fmt.Fprintf(b, "\n#### 第 %d 步：%s\n\n", step.Index, oneLine(step.Title))
	if step.Method != "" {
		fmt.Fprintf(b, "%s → %s（证据 %s）\n\n", codeSpan(step.Method+" "+step.URL), statusText(step.StatusCode), step.EvidenceID)
	}
	marks := append(append([]string{}, step.RequestMarksFound...), step.ResponseMarksFound...)
	if len(marks) > 0 || step.MarkMissing {
		b.WriteString("关键片段：\n\n")
		for _, mark := range step.RequestMarksFound {
			b.WriteString("- 请求：" + codeSpan(mark) + "\n")
		}
		for _, mark := range step.ResponseMarksFound {
			b.WriteString("- 响应：" + codeSpan(mark) + "\n")
		}
		if step.MarkMissing {
			b.WriteString("- " + vulnExportMarkMissing + "\n")
		}
		b.WriteString("\n")
	}
	switch step.EvidenceStatus {
	case evidenceStatusMissing:
		fmt.Fprintf(b, "证据原件不存在（%s）。\n", step.EvidenceID)
		return
	case evidenceStatusBadFlow:
		fmt.Fprintf(b, "证据原件无法解析（%s）。\n", step.EvidenceID)
		return
	}
	b.WriteString("请求原文：\n\n")
	writeCodeBlock(b, "http", step.RequestRaw)
	writeBodyNotes(b, "请求", step.RequestBodyBytes, step.RequestBodyBinary, step.RequestBodyTruncated, false)
	b.WriteString("\n响应原文：\n\n")
	if step.ResponseRaw == "" {
		b.WriteString("（原件无响应）\n")
	} else {
		writeCodeBlock(b, "http", step.ResponseRaw)
		writeBodyNotes(b, "响应", step.ResponseBodyBytes, step.ResponseBodyBinary, step.ResponseBodyTruncated, step.CaptureTruncated)
	}
	b.WriteString("\n遮盖版 curl：\n\n")
	writeCodeBlock(b, "bash", step.Curl)
}

func writeBodyNotes(b *strings.Builder, side string, size int, binary, truncated, capture bool) {
	if capture {
		fmt.Fprintf(b, "\n> 原件捕获已截断：%s正文只保存了前 %d 字节。\n", side, size)
	}
	if binary {
		fmt.Fprintf(b, "\n> %s正文为二进制（%d 字节），未内联。\n", side, size)
	}
	if truncated {
		fmt.Fprintf(b, "\n> %s正文超过 16 KiB，已截断（原 %d 字节）。\n", side, size)
	}
}

// ---- JSON ----

func renderVulnExportJSON(items []*VulnExportItem) ([]byte, error) {
	out := VulnExport{Schema: vulnExportSchema, Items: make([]VulnExportItem, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, *item)
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- HTTP ----

func exportFormat(format string) bool { return format == "markdown" || format == "json" }

// exportItems 按给定顺序生成导出项；任一键不存在返回 (key, errTriageNotFound)。
func (c *Capability) exportItems(ctx context.Context, keys []string) ([]*VulnExportItem, string, error) {
	index, err := c.loadCardIndex(ctx)
	if err != nil {
		return nil, "", err
	}
	out := make([]*VulnExportItem, 0, len(keys))
	for _, key := range keys {
		item, ordered, err := c.cardTargetIn(ctx, index, key)
		if err != nil {
			return nil, key, err
		}
		card, err := c.buildVulnCardFor(ctx, item, ordered)
		if err != nil {
			return nil, key, err
		}
		exported, err := c.buildVulnExportItem(ctx, card)
		if err != nil {
			return nil, key, err
		}
		out = append(out, exported)
	}
	return out, "", nil
}

func (c *Capability) writeVulnExport(w http.ResponseWriter, r *http.Request, keys []string, format, filename string) {
	items, failedKey, err := c.exportItems(r.Context(), keys)
	if errors.Is(err, errTriageNotFound) {
		writeError(w, http.StatusNotFound, "漏洞条目不存在："+failedKey)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成漏洞报告失败")
		return
	}
	if filename == "" {
		filename = "vuln-" + items[0].Code
	}
	var body []byte
	if format == "markdown" {
		body = []byte(c.renderVulnExportMarkdown(items))
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		filename += ".md"
	} else {
		if body, err = renderVulnExportJSON(items); err != nil {
			writeError(w, http.StatusInternalServerError, "漏洞报告 JSON 编码失败")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		filename += ".json"
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// handleVulnCardExport: GET /api/webapi/vulns/{key}/card/export?format=markdown|json（只读，不登记审计）。
func (c *Capability) handleVulnCardExport(w http.ResponseWriter, r *http.Request) {
	key, ok := vulnCardKey(w, r)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	if !exportFormat(format) {
		writeError(w, http.StatusBadRequest, "format 只能是 markdown 或 json")
		return
	}
	c.writeVulnExport(w, r, []string{key}, format, "")
}

// handleVulnExport: POST /api/webapi/vulns/export {keys:[…], format}；1–50 个不重复键，按给定顺序。
func (c *Capability) handleVulnExport(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Keys   []string `json:"keys"`
		Format string   `json:"format"`
	}
	if err := decodeJSONBody(r, &input, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !exportFormat(input.Format) {
		writeError(w, http.StatusBadRequest, "format 只能是 markdown 或 json")
		return
	}
	if len(input.Keys) < 1 || len(input.Keys) > vulnExportMaxKeys {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("keys 须为 1 至 %d 个", vulnExportMaxKeys))
		return
	}
	seen := map[string]bool{}
	for _, key := range input.Keys {
		if validPathID(key) != nil {
			writeError(w, http.StatusBadRequest, "漏洞条目 id 非法："+key)
			return
		}
		if seen[key] {
			writeError(w, http.StatusBadRequest, "keys 不能重复："+key)
			return
		}
		seen[key] = true
	}
	c.writeVulnExport(w, r, input.Keys, input.Format, "vulns-report")
}
