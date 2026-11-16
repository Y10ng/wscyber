package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"google.golang.org/protobuf/proto"
)

const maxVerificationPayloadBytes = 24 << 10

type verificationEvidenceInput struct {
	EvidenceID string `json:"evidence_id" jsonschema:"required,description=New webapi_http evidence ID from this verification session"`
	Relation   string `json:"relation" jsonschema:"required,enum=supporting,enum=counterevidence,enum=limitation"`
}

type verificationToolPayload struct {
	Verdict       string                      `json:"verdict" jsonschema:"required,enum=supported,enum=counterevidence,enum=inconclusive"`
	Method        string                      `json:"method" jsonschema:"required"`
	Preconditions string                      `json:"preconditions,omitempty"`
	Summary       string                      `json:"summary" jsonschema:"required"`
	Limitations   string                      `json:"limitations,omitempty"`
	Evidence      []verificationEvidenceInput `json:"evidence,omitempty"`
}

type verificationTool struct{ store *taskStore }

func (*verificationTool) Name() string { return "webapi_verification_result" }
func (*verificationTool) Description() string {
	return "Save an immutable model assessment for the current verification session using only new HTTP evidence from this session. This does not confirm the original candidate."
}
func (*verificationTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_verification_result", "保存当前独立验证的模型判读；只引用本会话新HTTP证据，支持/反证须完整响应，状态码不判资格，不修改原候选。", verificationToolPayload{})
}

// decodeStrictToolPayload 是验证/复测结果共用的strict单对象JSON解码；label为“验证”或“复测”。
func decodeStrictToolPayload(arguments string, target any, label string) error {
	if len(arguments) > maxVerificationPayloadBytes {
		return errors.New(label + "载荷超过24KiB")
	}
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("参数不是strict JSON或含未知字段")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("参数只能包含一个JSON对象")
	}
	return nil
}

// validateResultBody 校验验证/复测结果共用的正文长度与证据引用形状。
func validateResultBody(method, preconditions, summary, limitations string, evidence []verificationEvidenceInput, label string) error {
	if !validFindingText(method, 4096, true) || !validFindingText(preconditions, 4096, false) ||
		!validFindingText(summary, 4096, true) || !validFindingText(limitations, 4096, false) || len(evidence) > 16 {
		return errors.New(label + "正文或证据数量不符合限制")
	}
	seen := map[string]bool{}
	for _, item := range evidence {
		if item.EvidenceID == "" || len(item.EvidenceID) > 128 || seen[item.EvidenceID] ||
			(item.Relation != "supporting" && item.Relation != "counterevidence" && item.Relation != "limitation") {
			return errors.New(label + "证据ID重复/非法或relation非法")
		}
		seen[item.EvidenceID] = true
	}
	return nil
}

func canonicalToolPayload(args any, label string) ([]byte, error) {
	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxVerificationPayloadBytes {
		return nil, errors.New(label + "规范载荷超过24KiB")
	}
	return canonical, nil
}

func validateVerificationArguments(arguments string) (verificationToolPayload, []byte, error) {
	var args verificationToolPayload
	if err := decodeStrictToolPayload(arguments, &args, "验证"); err != nil {
		return args, nil, err
	}
	if args.Verdict != "supported" && args.Verdict != "counterevidence" && args.Verdict != "inconclusive" {
		return args, nil, errors.New("verdict仅支持supported/counterevidence/inconclusive")
	}
	if err := validateResultBody(args.Method, args.Preconditions, args.Summary, args.Limitations, args.Evidence, "验证"); err != nil {
		return args, nil, err
	}
	canonical, err := canonicalToolPayload(args, "验证")
	return args, canonical, err
}

// sessionResultEvidence 核对结果引用：只接受本session在本任务产生的新HTTP证据；
// 支持/反证须已发送、completed、有实际响应、Flow完整且未截断，否则只能作为限制。
// 返回证据行与已满足的有效relation；label为“验证”或“复测”。
func (s *taskStore) sessionResultEvidence(ctx context.Context, taskID, sessionID string, refs []verificationEvidenceInput, label string) ([]requestModel, map[string]bool, error) {
	evidence := make([]requestModel, 0, len(refs))
	qualifies := map[string]bool{}
	for _, reference := range refs {
		model, err := s.evidenceByID(ctx, taskID, reference.EvidenceID)
		if err != nil || model == nil || model.SessionID != sessionID {
			return nil, nil, errors.New("只接受本" + label + "session在本任务产生的新HTTP证据")
		}
		var flow traffic.Flow
		if proto.Unmarshal(model.FlowPB, &flow) != nil {
			return nil, nil, errors.New("HTTP原件无法核对，结果未保存")
		}
		complete := model.Status == requestCompleted && model.Sent.Valid && model.Sent.Bool && model.StatusCode.Valid && flow.GetComplete() && !model.CaptureTruncated && flow.GetResponse() != nil
		if reference.Relation == "limitation" {
			// 失败、未发送和截断仅作为限制保存；完整响应也可以作为限制引用。
		} else if !complete {
			return nil, nil, errors.New("支持/反证引用必须是已发送、收到响应且完整捕获的新HTTP原件")
		} else {
			qualifies[reference.Relation] = true
		}
		evidence = append(evidence, *model)
	}
	return evidence, qualifies, nil
}

func (t *verificationTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.TurnID == "" || inv.CallID == "" {
		return coretool.ErrorResult("缺少可信原生session/turn/call上下文，结果未保存"), nil
	}
	verification, err := t.store.verificationForSession(ctx, inv.SessionID)
	if err != nil || verification == nil {
		return coretool.ErrorResult("当前session不是已保存的独立验证，结果未保存"), nil
	}
	args, canonical, err := validateVerificationArguments(arguments)
	if err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	evidence, qualifies, err := t.store.sessionResultEvidence(ctx, verification.TaskID, verification.SessionID, args.Evidence, "验证")
	if err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	if (args.Verdict == "supported" && !qualifies["supporting"]) || (args.Verdict == "counterevidence" && !qualifies["counterevidence"]) {
		return coretool.ErrorResult("当前verdict缺少对应relation的完整新HTTP证据；HTTP状态码不决定资格"), nil
	}
	result, err := t.store.saveVerificationResult(ctx, inv, verification, canonical, args, evidence)
	if err != nil {
		if errors.Is(err, errVerificationResult) {
			return coretool.ErrorResult(errVerificationResult.Error()), nil
		}
		return coretool.ErrorResult("验证结果保存或核对失败；结果未确认，请先核对现有记录，不自动重试或重发HTTP"), nil
	}
	raw, err := json.Marshal(map[string]any{
		"verification_id": verification.ID, "verdict": args.Verdict, "source": FindingSource{SessionID: result.SessionID, TurnID: result.TurnID, CallID: result.CallID},
		"evidence": result.Evidence, "note": "仅保存本次独立验证的模型判读；不确认或覆盖原候选，状态码不是漏洞证明",
	})
	if err != nil {
		return nil, errors.New("验证结果已保存但结果编码失败")
	}
	return coretool.TextResult(string(raw)), nil
}
