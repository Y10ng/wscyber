package webapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

const notProvided = "未提供"

const reportExportNote = "报告草稿由报告会话模型依据固定候选修订与验证结果撰写；候选未经人工裁定，验证判读是模型判读、不等于确认；导出不等于发布。"

// reportExportItem 按报告固定顺序合并草稿条目与固定来源。
type reportExportItem struct {
	Index         int                       `json:"index"`
	FindingID     string                    `json:"finding_id"`
	Number        string                    `json:"number"`
	Title         string                    `json:"title"`
	Revision      int64                     `json:"revision"`
	RevisionID    string                    `json:"revision_id"`
	Description   string                    `json:"description"`
	Reproduction  string                    `json:"reproduction"`
	Remediation   string                    `json:"remediation"`
	Evidence      []FindingEvidenceSnapshot `json:"evidence"`
	Verifications []ReportVerification      `json:"verifications"`
}

type reportExport struct {
	Format      string             `json:"format"`
	Note        string             `json:"note"`
	ReportID    string             `json:"report_id"`
	TaskID      string             `json:"task_id"`
	Title       string             `json:"title"`
	Overview    string             `json:"overview"`
	Items       []reportExportItem `json:"items"`
	Limitations string             `json:"limitations"`
	DraftSource FindingSource      `json:"draft_source"`
	DraftAt     string             `json:"draft_created_at"`
}

func buildReportExport(detail *ReportDetail) reportExport {
	draft := detail.Draft.Payload
	byFinding := make(map[string]reportDraftItemInput, len(draft.Items))
	for _, item := range draft.Items {
		byFinding[item.FindingID] = item
	}
	export := reportExport{
		Format: "wscyber.report.v1", Note: reportExportNote, ReportID: detail.ReportID, TaskID: detail.TaskID,
		Title: draft.Title, Overview: draft.Overview, Limitations: draft.Limitations, Items: make([]reportExportItem, 0, len(detail.Items)),
		DraftSource: FindingSource{SessionID: detail.Draft.SessionID, TurnID: detail.Draft.TurnID, CallID: detail.Draft.CallID}, DraftAt: detail.Draft.CreatedAt,
	}
	for index, source := range detail.Items {
		written := byFinding[source.FindingID]
		allowed := allowedReportEvidence(source)
		evidence := make([]FindingEvidenceSnapshot, 0, len(written.EvidenceIDs))
		for _, id := range written.EvidenceIDs {
			if snapshot, ok := allowed[id]; ok {
				evidence = append(evidence, snapshot)
			}
		}
		export.Items = append(export.Items, reportExportItem{
			Index: index + 1, FindingID: source.FindingID, Number: source.Revision.Number, Title: source.Revision.Content.Title,
			Revision: source.Revision.Revision, RevisionID: source.RevisionID, Description: written.Description,
			Reproduction: written.Reproduction, Remediation: written.Remediation, Evidence: evidence, Verifications: source.Verifications,
		})
	}
	return export
}

func renderReportJSON(detail *ReportDetail) ([]byte, error) {
	raw, err := json.MarshalIndent(buildReportExport(detail), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func singleLine(value string) string { return strings.Join(strings.Fields(value), " ") }

func codeSpan(value string) string {
	if strings.Contains(value, "`") {
		return "`` " + value + " ``"
	}
	return "`" + value + "`"
}

func orNotProvided(value string) string {
	if strings.TrimSpace(value) == "" {
		return notProvided
	}
	return value
}

func evidenceLine(snapshot FindingEvidenceSnapshot) string {
	parts := []string{singleLine(snapshot.Method) + " " + codeSpan(snapshot.URL)}
	if snapshot.HTTPStatus != nil {
		parts = append(parts, fmt.Sprintf("HTTP %d", *snapshot.HTTPStatus))
	} else {
		parts = append(parts, "无HTTP响应状态")
	}
	parts = append(parts, "记录状态 "+snapshot.Status)
	if snapshot.Sent == nil {
		parts = append(parts, "发送结果未知")
	} else if !*snapshot.Sent {
		parts = append(parts, "未发送")
	}
	if snapshot.CaptureTruncated {
		parts = append(parts, "响应捕获截断")
	}
	if snapshot.FlowSHA256 != "" {
		parts = append(parts, "原件 "+snapshot.FlowSHA256)
	}
	return "- " + codeSpan(snapshot.EvidenceID) + "：" + strings.Join(parts, "；")
}

var verdictLabels = map[string]string{"supported": "支持", "counterevidence": "反证", "inconclusive": "证据不足"}

// renderReportMarkdown 只由已存草稿与固定来源确定性生成，不含生成时间等易变字段。
func renderReportMarkdown(detail *ReportDetail) string {
	export := buildReportExport(detail)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n> %s\n\n", singleLine(export.Title), reportExportNote)
	fmt.Fprintf(&b, "- 报告：%s\n- 任务：%s\n- 草稿来源：session %s / turn %s / call %s（%s）\n\n",
		codeSpan(export.ReportID), codeSpan(export.TaskID), codeSpan(export.DraftSource.SessionID), codeSpan(export.DraftSource.TurnID), codeSpan(export.DraftSource.CallID), export.DraftAt)
	fmt.Fprintf(&b, "## 概述\n\n%s\n\n## 候选\n\n", orNotProvided(export.Overview))
	for _, item := range export.Items {
		fmt.Fprintf(&b, "### %d. %s %s\n\n修订：r%d（%s）\n\n", item.Index, item.Number, singleLine(item.Title), item.Revision, codeSpan(item.RevisionID))
		fmt.Fprintf(&b, "#### 描述\n\n%s\n\n#### 复现步骤\n\n%s\n\n证据引用：\n\n", orNotProvided(item.Description), orNotProvided(item.Reproduction))
		if len(item.Evidence) == 0 {
			b.WriteString(notProvided + "\n\n")
		} else {
			for _, snapshot := range item.Evidence {
				b.WriteString(evidenceLine(snapshot) + "\n")
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "#### 修复建议\n\n%s\n\n#### 验证判读\n\n", orNotProvided(item.Remediation))
		if len(item.Verifications) == 0 {
			b.WriteString(notProvided + "\n\n")
			continue
		}
		for _, verification := range item.Verifications {
			if verification.Result == nil {
				fmt.Fprintf(&b, "- 验证 %s：尚无判读结果\n", codeSpan(verification.VerificationID))
				continue
			}
			result := verification.Result.Payload
			fmt.Fprintf(&b, "- 验证 %s：%s（模型判读）\n  - 方法：%s\n  - 摘要：%s\n  - 局限：%s\n",
				codeSpan(verification.VerificationID), verdictLabels[result.Verdict], singleLine(result.Method), singleLine(result.Summary), singleLine(orNotProvided(result.Limitations)))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "## 局限\n\n%s\n", orNotProvided(export.Limitations))
	return b.String()
}
