package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	if err := decodeStrictToolPayloadLimit(arguments, &args, "报告草稿", maxReportDraftBytes); err != nil {
		return args, nil, err
	}
	var problems toolValidationErrors
	for _, field := range []struct {
		path     string
		value    string
		maximum  int
		required bool
	}{
		{"title", args.Title, 256, true},
		{"overview", args.Overview, 8192, true},
		{"limitations", args.Limitations, 8192, false},
	} {
		if err := findingTextError(field.path, field.value, field.maximum, field.required); err != nil {
			problems.add(err)
		}
	}
	if len(args.Items) < 1 {
		problems.add(fmt.Errorf("items数量不足：实际 %d，至少 1", len(args.Items)))
	}
	if len(args.Items) > maxReportItems {
		problems.add(fmt.Errorf("items数量过多：实际 %d，上限 %d", len(args.Items), maxReportItems))
	}
	for index, item := range args.Items {
		path := fmt.Sprintf("items[%d]", index)
		if item.FindingID == "" {
			problems.add(fmt.Errorf("%s.finding_id必填为空；期望非空值", path))
		}
		if !validReferenceID(item.FindingID) {
			problems.add(fmt.Errorf("%s.finding_id非法：需为有效引用ID（非空、最多128字节、不含 /、反斜杠、?、#、%%、控制字符）", path))
		}
		for _, field := range []struct {
			name     string
			value    string
			maximum  int
			required bool
		}{
			{"description", item.Description, 8192, true},
			{"reproduction", item.Reproduction, 8192, true},
			{"remediation", item.Remediation, 4096, false},
		} {
			if err := findingTextError(path+"."+field.name, field.value, field.maximum, field.required); err != nil {
				problems.add(err)
			}
		}
		seen := map[string]bool{}
		for evidenceIndex, evidenceID := range item.EvidenceIDs {
			evidencePath := fmt.Sprintf("%s.evidence_ids[%d]", path, evidenceIndex)
			if evidenceID == "" {
				problems.add(fmt.Errorf("%s必填为空；期望非空值", evidencePath))
			}
			if !validReferenceID(evidenceID) {
				problems.add(fmt.Errorf("%s非法：需为有效引用ID（非空、最多128字节、不含 /、反斜杠、?、#、%%、控制字符）", evidencePath))
			}
			if seen[evidenceID] {
				problems.add(fmt.Errorf("%s重复；请删除重复项", evidencePath))
			}
			seen[evidenceID] = true
		}
	}
	if err := problems.err(); err != nil {
		return args, nil, err
	}
	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxReportDraftBytes {
		return args, nil, errors.New("$: 报告草稿规范载荷超过128KiB；期望规范 JSON 不超过128KiB")
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
	var problems toolValidationErrors
	if len(args.Items) != len(detail.Items) {
		problems.add(fmt.Errorf("items: 草稿items必须与报告固定items一一对应；实际 %d 项，期望 %d 项", len(args.Items), len(detail.Items)))
	}
	pinned := make(map[string]ReportItemDetail, len(detail.Items))
	for _, item := range detail.Items {
		pinned[item.FindingID] = item
	}
	seen := map[string]bool{}
	for index, item := range args.Items {
		source, ok := pinned[item.FindingID]
		if !ok {
			problems.add(fmt.Errorf("items[%d].finding_id: 不是报告固定候选；草稿items必须与报告固定items一一对应", index))
			continue
		}
		if seen[item.FindingID] {
			problems.add(fmt.Errorf("items[%d].finding_id: 重复；草稿items必须与报告固定items一一对应，每个 finding_id 只能出现一次", index))
		}
		seen[item.FindingID] = true
		allowed := allowedReportEvidence(source)
		for evidenceIndex, evidenceID := range item.EvidenceIDs {
			if _, ok := allowed[evidenceID]; !ok {
				problems.add(fmt.Errorf("items[%d].evidence_ids[%d]: evidence_ids只能来自该候选固定修订的证据快照或所附验证结果的证据", index, evidenceIndex))
			}
		}
	}
	return problems.err()
}

func (t *reportDraftTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.TurnID == "" || inv.CallID == "" {
		return coretool.ErrorResult("缺少可信原生session/turn/call上下文，草稿未保存"), nil
	}
	report, err := t.store.reportForSession(ctx, inv.SessionID)
	if err != nil || report == nil {
		return coretool.ErrorResult("当前不是报告会话，不能保存报告草稿；报告由用户在任务页固定候选后发起，草稿未保存"), nil
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
