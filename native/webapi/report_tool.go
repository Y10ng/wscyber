package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

const maxReportDraftBytes = 128 << 10

type reportDraftItemInput struct {
	FindingID    string   `json:"finding_id" jsonschema:"required,description=finding_id of one item pinned by this report"`
	Description  string   `json:"description" jsonschema:"required"`
	Reproduction string   `json:"reproduction" jsonschema:"required"`
	Remediation  string   `json:"remediation,omitempty"`
	EvidenceIDs  []string `json:"evidence_ids,omitempty" jsonschema:"description=Evidence IDs from the pinned revision snapshot or attached verification results"`
}

type reportDraftPayload struct {
	Title       string                 `json:"title" jsonschema:"required"`
	Overview    string                 `json:"overview" jsonschema:"required"`
	Items       []reportDraftItemInput `json:"items" jsonschema:"required,description=Exactly one entry per pinned report item"`
	Limitations string                 `json:"limitations,omitempty"`
}

type reportDraftTool struct{ store *taskStore }

func (*reportDraftTool) Name() string { return "webapi_report_draft" }
func (*reportDraftTool) Description() string {
	return "Save the immutable report draft for the current report session, covering each pinned candidate revision exactly once and citing only evidence from those revisions or attached verification results. This does not confirm or publish anything."
}
func (*reportDraftTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_report_draft", "保存当前报告会话的不可变报告草稿；逐一覆盖报告固定的候选修订，只引用该修订或所附验证结果中的证据，不确认漏洞、不发布、不发送请求。", reportDraftPayload{})
}

func validateReportDraftArguments(arguments string) (reportDraftPayload, []byte, error) {
	var args reportDraftPayload
	if len(arguments) > maxReportDraftBytes {
		return args, nil, errors.New("报告草稿载荷超过128KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return args, nil, errors.New("参数不是strict JSON或含未知字段")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return args, nil, errors.New("参数只能包含一个JSON对象")
	}
	if !validFindingText(args.Title, 256, true) || !validFindingText(args.Overview, 8192, true) ||
		!validFindingText(args.Limitations, 8192, false) || len(args.Items) < 1 || len(args.Items) > maxReportItems {
		return args, nil, errors.New("报告标题/概述/局限或条目数量不符合限制")
	}
	for _, item := range args.Items {
		if !validReferenceID(item.FindingID) || !validFindingText(item.Description, 8192, true) ||
			!validFindingText(item.Reproduction, 8192, true) || !validFindingText(item.Remediation, 4096, false) || len(item.EvidenceIDs) > 16 {
			return args, nil, errors.New("报告条目正文或证据数量不符合限制")
		}
		seen := map[string]bool{}
		for _, evidenceID := range item.EvidenceIDs {
			if !validReferenceID(evidenceID) || seen[evidenceID] {
				return args, nil, errors.New("报告条目证据ID重复或非法")
			}
			seen[evidenceID] = true
		}
	}
	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxReportDraftBytes {
		return args, nil, errors.New("报告草稿规范载荷超过128KiB")
	}
	return args, canonical, nil
}

// allowedReportEvidence 是一项可引用的证据：固定修订快照 ∪ 所附验证结果证据。
func allowedReportEvidence(item ReportItemDetail) map[string]FindingEvidenceSnapshot {
	allowed := map[string]FindingEvidenceSnapshot{}
	for _, snapshot := range item.Revision.Evidence {
		allowed[snapshot.EvidenceID] = snapshot
	}
	for _, verification := range item.Verifications {
		if verification.Result == nil {
			continue
		}
		for _, snapshot := range verification.Result.Evidence {
			if _, exists := allowed[snapshot.EvidenceID]; !exists {
				allowed[snapshot.EvidenceID] = snapshot
			}
		}
	}
	return allowed
}

func checkDraftAgainstReport(args reportDraftPayload, detail *ReportDetail) error {
	if len(args.Items) != len(detail.Items) {
		return errors.New("草稿items必须与报告固定items一一对应")
	}
	pinned := make(map[string]ReportItemDetail, len(detail.Items))
	for _, item := range detail.Items {
		pinned[item.FindingID] = item
	}
	seen := map[string]bool{}
	for _, item := range args.Items {
		source, ok := pinned[item.FindingID]
		if !ok || seen[item.FindingID] {
			return errors.New("草稿items必须与报告固定items一一对应")
		}
		seen[item.FindingID] = true
		allowed := allowedReportEvidence(source)
		for _, evidenceID := range item.EvidenceIDs {
			if _, ok := allowed[evidenceID]; !ok {
				return errors.New("evidence_ids只能来自该候选固定修订的证据快照或所附验证结果的证据")
			}
		}
	}
	return nil
}

func (t *reportDraftTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.TurnID == "" || inv.CallID == "" {
		return coretool.ErrorResult("缺少可信原生session/turn/call上下文，草稿未保存"), nil
	}
	report, err := t.store.reportForSession(ctx, inv.SessionID)
	if err != nil || report == nil {
		return coretool.ErrorResult("当前session不是已保存的报告会话，草稿未保存"), nil
	}
	args, canonical, err := validateReportDraftArguments(arguments)
	if err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	detail, err := t.store.reportDetail(ctx, report)
	if err != nil {
		return coretool.ErrorResult("无法读取报告固定来源，草稿未保存"), nil
	}
	if err := checkDraftAgainstReport(args, detail); err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	draft, err := t.store.saveReportDraft(ctx, inv, report, canonical, args)
	if err != nil {
		if errors.Is(err, errReportDraft) {
			return coretool.ErrorResult(errReportDraft.Error()), nil
		}
		return coretool.ErrorResult("报告草稿保存或核对失败；结果未确认，请先核对现有记录，不自动重试"), nil
	}
	raw, err := json.Marshal(map[string]any{
		"report_id": report.ID, "title": draft.Payload.Title, "item_count": len(draft.Payload.Items),
		"source": FindingSource{SessionID: draft.SessionID, TurnID: draft.TurnID, CallID: draft.CallID},
		"note":   "仅保存报告草稿；不确认候选、不发布，导出由平台按固定来源确定性渲染",
	})
	if err != nil {
		return nil, errors.New("报告草稿已保存但结果编码失败")
	}
	return coretool.TextResult(string(raw)), nil
}
