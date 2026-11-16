package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func validFindingText(value string, maximum int, required bool) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len(value) <= maximum && (!required || strings.TrimSpace(value) != "")
}

func validateFindingArguments(arguments string) (findingToolArguments, []byte, error) {
	var args findingToolArguments
	if len(arguments) > maxFindingPayloadBytes {
		return args, nil, errors.New("候选载荷超过24KiB")
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
	if !utf8.ValidString(arguments) || strings.ContainsRune(arguments, 0) {
		return args, nil, errors.New("候选文本必须是有效UTF8且不含NUL")
	}
	if (args.FindingID == "") != (args.ExpectedRevision == 0) || args.ExpectedRevision < 0 {
		return args, nil, errors.New("更新必须同时提供finding_id与正的expected_revision；创建两者均不提供")
	}
	if args.FindingID != "" {
		if len(args.FindingID) > 128 || strings.ContainsAny(args.FindingID, "/\\?#%") || strings.ContainsFunc(args.FindingID, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return args, nil, errors.New("finding_id非法")
		}
	}
	content := args.Content
	if !validFindingText(content.Title, 256, true) || !validFindingText(content.Description, 8192, true) ||
		!validFindingText(content.Remediation, 4096, false) || !validFindingText(content.Limitations, 4096, false) ||
		len(content.Steps) < 1 || len(content.Steps) > 16 || len(content.Evidence) < 1 || len(content.Evidence) > 16 {
		return args, nil, errors.New("候选正文或步骤/证据数量不符合限制")
	}
	seenEvidence := make(map[string]bool, len(content.Evidence))
	for _, evidence := range content.Evidence {
		if evidence.EvidenceID == "" || len(evidence.EvidenceID) > 128 || seenEvidence[evidence.EvidenceID] ||
			(evidence.Relation != "supporting" && evidence.Relation != "counterevidence" && evidence.Relation != "limitation") {
			return args, nil, errors.New("证据ID重复/非法或relation非法")
		}
		seenEvidence[evidence.EvidenceID] = true
	}
	for _, step := range content.Steps {
		if !validFindingText(step.Text, 1024, true) || len(step.EvidenceIndices) > 16 {
			return args, nil, errors.New("步骤文本或索引数量不符合限制")
		}
		seenIndices := make(map[int]bool, len(step.EvidenceIndices))
		for _, index := range step.EvidenceIndices {
			if index < 0 || index >= len(content.Evidence) || seenIndices[index] {
				return args, nil, errors.New("步骤证据索引越界或重复")
			}
			seenIndices[index] = true
		}
	}
	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxFindingPayloadBytes {
		return args, nil, errors.New("候选规范载荷超过24KiB")
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
