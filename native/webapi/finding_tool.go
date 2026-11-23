package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

const maxFindingPayloadBytes = 24 << 10

type findingEvidenceInput struct {
	EvidenceID string `json:"evidence_id" jsonschema:"required,description=Stable evidence ID returned by webapi_http in this task"`
	Relation   string `json:"relation" jsonschema:"required,enum=supporting,enum=counterevidence,enum=limitation"`
}

type findingStepInput struct {
	Text            string `json:"text" jsonschema:"required"`
	EvidenceIndices []int  `json:"evidence_indices"`
}

type findingContent struct {
	Title       string                 `json:"title" jsonschema:"required"`
	Description string                 `json:"description" jsonschema:"required,description=Claim only; not a confirmed vulnerability"`
	Steps       []findingStepInput     `json:"steps" jsonschema:"required"`
	Remediation string                 `json:"remediation,omitempty"`
	Limitations string                 `json:"limitations,omitempty"`
	Evidence    []findingEvidenceInput `json:"evidence" jsonschema:"required"`
}

type findingToolArguments struct {
	FindingID        string         `json:"finding_id,omitempty"`
	ExpectedRevision int64          `json:"expected_revision,omitempty"`
	Content          findingContent `json:"content" jsonschema:"required"`
}

type findingTool struct{ store *taskStore }

func (*findingTool) Name() string { return "webapi_finding" }
func (*findingTool) Description() string {
	return "Save or update an unconfirmed candidate using real HTTP evidence IDs from the current task. This only persists claims; it never sends requests or confirms a vulnerability."
}
func (*findingTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_finding", "保存或更新候选发现；只引用本任务webapi_http返回的真实evidence_id，主张不等于确认，不发送任何请求。", findingToolArguments{})
}

func findingTextError(field, value string, maximum int, required bool) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s必须是有效UTF8", field)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s不能包含NUL", field)
	}
	if len(value) > maximum {
		return fmt.Errorf("%s超长：实际 %d 字符（%d 字节），上限 %d 字节", field, utf8.RuneCountInString(value), len(value), maximum)
	}
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s必填为空；期望非空值", field)
	}
	return nil
}

func validFindingText(value string, maximum int, required bool) bool {
	return findingTextError("", value, maximum, required) == nil
}

func validateFindingArguments(arguments string) (findingToolArguments, []byte, error) {
	var args findingToolArguments
	if err := decodeStrictToolPayloadLimit(arguments, &args, "候选", maxFindingPayloadBytes); err != nil {
		return args, nil, err
	}
	var problems toolValidationErrors
	if !utf8.ValidString(arguments) {
		problems.add(errors.New("$: 候选参数必须是有效UTF8"))
	}
	if strings.ContainsRune(arguments, 0) {
		problems.add(errors.New("$: 候选参数不能包含NUL"))
	}
	if args.ExpectedRevision < 0 {
		problems.add(errors.New("更新必须同时提供finding_id与正的expected_revision：expected_revision不能小于0"))
	}
	if args.FindingID == "" && args.ExpectedRevision != 0 {
		problems.add(errors.New("更新必须同时提供finding_id与正的expected_revision：finding_id为空"))
	}
	if args.FindingID != "" && args.ExpectedRevision == 0 {
		problems.add(errors.New("更新必须同时提供finding_id与正的expected_revision：expected_revision为0"))
	}
	if args.FindingID != "" {
		if len(args.FindingID) > 128 {
			problems.add(fmt.Errorf("finding_id超长：实际 %d 字符（%d 字节），上限 128 字节", utf8.RuneCountInString(args.FindingID), len(args.FindingID)))
		}
		if strings.ContainsAny(args.FindingID, "/\\?#%") || strings.ContainsFunc(args.FindingID, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			problems.add(errors.New("finding_id含非法字符；期望不含 /、反斜杠、?、#、%、控制字符的 ID"))
		}
	}
	content := args.Content
	for _, field := range []struct {
		path     string
		value    string
		maximum  int
		required bool
	}{
		{"content.title", content.Title, 256, true},
		{"content.description", content.Description, 8192, true},
		{"content.remediation", content.Remediation, 4096, false},
		{"content.limitations", content.Limitations, 4096, false},
	} {
		if err := findingTextError(field.path, field.value, field.maximum, field.required); err != nil {
			problems.add(err)
		}
	}
	if len(content.Steps) < 1 {
		problems.add(fmt.Errorf("content.steps数量不足：实际 %d，至少 1", len(content.Steps)))
	}
	if len(content.Steps) > 16 {
		problems.add(fmt.Errorf("content.steps数量过多：实际 %d，上限 16", len(content.Steps)))
	}
	if len(content.Evidence) < 1 {
		problems.add(fmt.Errorf("content.evidence数量不足：实际 %d，至少 1", len(content.Evidence)))
	}
	if len(content.Evidence) > 16 {
		problems.add(fmt.Errorf("content.evidence数量过多：实际 %d，上限 16", len(content.Evidence)))
	}
	seenEvidence := make(map[string]bool, len(content.Evidence))
	for index, evidence := range content.Evidence {
		path := fmt.Sprintf("content.evidence[%d]", index)
		if evidence.EvidenceID == "" {
			problems.add(fmt.Errorf("%s.evidence_id必填为空；期望非空值", path))
		}
		if len(evidence.EvidenceID) > 128 {
			problems.add(fmt.Errorf("%s.evidence_id超长：实际 %d 字符（%d 字节），上限 128 字节", path, utf8.RuneCountInString(evidence.EvidenceID), len(evidence.EvidenceID)))
		}
		if seenEvidence[evidence.EvidenceID] {
			problems.add(fmt.Errorf("%s.evidence_id重复；请删除重复项", path))
		}
		if evidence.Relation != "supporting" && evidence.Relation != "counterevidence" && evidence.Relation != "limitation" {
			problems.add(fmt.Errorf("%s.relation仅支持supporting/counterevidence/limitation", path))
		}
		seenEvidence[evidence.EvidenceID] = true
	}
	for index, step := range content.Steps {
		path := fmt.Sprintf("content.steps[%d]", index)
		if err := findingTextError(path+".text", step.Text, 1024, true); err != nil {
			problems.add(err)
		}
		if len(step.EvidenceIndices) > 16 {
			problems.add(fmt.Errorf("%s.evidence_indices数量过多：实际 %d，上限 16", path, len(step.EvidenceIndices)))
		}
		seenIndices := make(map[int]bool, len(step.EvidenceIndices))
		for evidenceIndex, index := range step.EvidenceIndices {
			indexPath := fmt.Sprintf("%s.evidence_indices[%d]", path, evidenceIndex)
			if index < 0 {
				problems.add(fmt.Errorf("%s不能小于0", indexPath))
			}
			if index >= len(content.Evidence) {
				problems.add(fmt.Errorf("%s证据索引越界：上限 %d", indexPath, len(content.Evidence)-1))
			}
			if seenIndices[index] {
				problems.add(fmt.Errorf("%s重复；请删除重复项", indexPath))
			}
			seenIndices[index] = true
		}
	}
	if err := problems.err(); err != nil {
		return args, nil, err
	}
	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxFindingPayloadBytes {
		return args, nil, errors.New("$: 候选规范载荷超过24KiB；期望规范 JSON 不超过24KiB")
	}
	return args, canonical, nil
}

func (t *findingTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.TurnID == "" || inv.CallID == "" {
		return coretool.ErrorResult("缺少可信原生session/turn/call上下文，候选未保存"), nil
	}
	args, canonical, err := validateFindingArguments(arguments)
	if err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	verification, verifyErr := t.store.verificationForSession(ctx, inv.SessionID)
	if verifyErr != nil {
		return coretool.ErrorResult("无法核对session写权限，候选未保存"), nil
	}
	if verification != nil {
		return coretool.ErrorResult("独立验证session不能创建或更新原候选"), nil
	}
	retest, retestErr := t.store.retestForSession(ctx, inv.SessionID)
	if retestErr != nil {
		return coretool.ErrorResult("无法核对session写权限，候选未保存"), nil
	}
	if retest != nil {
		return coretool.ErrorResult("复测session不能创建或更新原候选"), nil
	}
	task, err := t.store.taskForSession(ctx, inv.SessionID)
	if err != nil || task == nil {
		return coretool.ErrorResult("session无已保存任务，候选未保存"), nil
	}
	detail, err := t.store.saveFinding(ctx, inv, task.ID, args, canonical)
	if err != nil {
		return coretool.ErrorResult(publicFindingError(err)), nil
	}
	result := map[string]any{
		"finding_id": detail.FindingID, "number": detail.Number, "status": detail.Status,
		"revision": detail.Revision, "revision_id": detail.RevisionID,
		"source": detail.Source, "evidence": detail.Evidence,
		"note": "仅保存候选主张；未确认漏洞，未发送新请求",
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("候选已保存为%s但结果编码失败", detail.Number)
	}
	return coretool.TextResult(string(raw)), nil
}
