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

func validateVerificationArguments(arguments string) (verificationToolPayload, []byte, error) {
	var args verificationToolPayload
	if len(arguments) > maxVerificationPayloadBytes {
		return args, nil, errors.New("验证载荷超过24KiB")
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
	if args.Verdict != "supported" && args.Verdict != "counterevidence" && args.Verdict != "inconclusive" {
		return args, nil, errors.New("verdict仅支持supported/counterevidence/inconclusive")
	}
	if !validFindingText(args.Method, 4096, true) || !validFindingText(args.Preconditions, 4096, false) ||
		!validFindingText(args.Summary, 4096, true) || !validFindingText(args.Limitations, 4096, false) || len(args.Evidence) > 16 {
		return args, nil, errors.New("验证正文或证据数量不符合限制")
	}
	seen := map[string]bool{}
	for _, evidence := range args.Evidence {
		if evidence.EvidenceID == "" || len(evidence.EvidenceID) > 128 || seen[evidence.EvidenceID] ||
			(evidence.Relation != "supporting" && evidence.Relation != "counterevidence" && evidence.Relation != "limitation") {
			return args, nil, errors.New("验证证据ID重复/非法或relation非法")
		}
		seen[evidence.EvidenceID] = true
	}
	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxVerificationPayloadBytes {
		return args, nil, errors.New("验证规范载荷超过24KiB")
	}
	return args, canonical, nil
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
	evidence := make([]requestModel, 0, len(args.Evidence))
	qualifies := map[string]bool{}
	for _, reference := range args.Evidence {
		model, err := t.store.evidenceByID(ctx, verification.TaskID, reference.EvidenceID)
		if err != nil || model == nil || model.SessionID != verification.SessionID {
			return coretool.ErrorResult("只接受本验证session在本任务产生的新HTTP证据"), nil
		}
		var flow traffic.Flow
		if proto.Unmarshal(model.FlowPB, &flow) != nil {
			return coretool.ErrorResult("HTTP原件无法核对，结果未保存"), nil
		}
		complete := model.Status == requestCompleted && model.Sent.Valid && model.Sent.Bool && model.StatusCode.Valid && flow.GetComplete() && !model.CaptureTruncated && flow.GetResponse() != nil
		if reference.Relation == "limitation" {
			// 失败、未发送和截断仅作为限制保存；完整响应也可以作为限制引用。
		} else if !complete {
			return coretool.ErrorResult("支持/反证引用必须是已发送、收到响应且完整捕获的新HTTP原件"), nil
		} else {
			qualifies[reference.Relation] = true
		}
		evidence = append(evidence, *model)
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
