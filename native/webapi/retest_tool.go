package webapi

import (
	"context"
	"encoding/json"
	"errors"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// retestEvidenceInput 与verificationEvidenceInput同字段（仅schema描述不同），可逐项转换后复用验证证据核对。
type retestEvidenceInput struct {
	EvidenceID string `json:"evidence_id" jsonschema:"required,description=New webapi_http evidence ID from this retest session"`
	Relation   string `json:"relation" jsonschema:"required,enum=supporting,enum=counterevidence,enum=limitation"`
}

type retestToolPayload struct {
	Conclusion    string                `json:"conclusion" jsonschema:"required,enum=reproducible,enum=fixed_in_scope,enum=inconclusive"`
	Method        string                `json:"method" jsonschema:"required"`
	Preconditions string                `json:"preconditions,omitempty"`
	Summary       string                `json:"summary" jsonschema:"required"`
	Limitations   string                `json:"limitations,omitempty"`
	Evidence      []retestEvidenceInput `json:"evidence,omitempty"`
}

func retestEvidenceRefs(items []retestEvidenceInput) []verificationEvidenceInput {
	refs := make([]verificationEvidenceInput, len(items))
	for index, item := range items {
		refs[index] = verificationEvidenceInput(item)
	}
	return refs
}

type retestTool struct{ store *taskStore }

func (*retestTool) Name() string { return "webapi_retest_result" }
func (*retestTool) Description() string {
	return "Save the immutable conclusion for the current retest session using only new HTTP evidence from this session: reproducible (needs complete supporting evidence), fixed_in_scope (needs complete counterevidence showing the issue no longer occurs within the tested scope), or inconclusive. Login failure, unreachable target, timeouts, missing responses or truncated captures do NOT mean fixed; use inconclusive and record them as limitations. This does not modify the original candidate, its revisions or verifications."
}
func (*retestTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_retest_result", "保存当前复测的不可变结论；只引用本复测会话新HTTP证据。reproducible需完整支持证据，fixed_in_scope需完整反证（本次范围内不再出现）。登录失败、目标不可达、超时、无响应或截断不等于已修复，应给inconclusive并记为限制。状态码不判资格，不修改原候选、修订或验证。", retestToolPayload{})
}

func validateRetestArguments(arguments string) (retestToolPayload, []byte, error) {
	var args retestToolPayload
	if err := decodeStrictToolPayload(arguments, &args, "复测"); err != nil {
		return args, nil, err
	}
	if args.Conclusion != "reproducible" && args.Conclusion != "fixed_in_scope" && args.Conclusion != "inconclusive" {
		return args, nil, errors.New("conclusion仅支持reproducible/fixed_in_scope/inconclusive")
	}
	if err := validateResultBody(args.Method, args.Preconditions, args.Summary, args.Limitations, retestEvidenceRefs(args.Evidence), "复测"); err != nil {
		return args, nil, err
	}
	canonical, err := canonicalToolPayload(args, "复测")
	return args, canonical, err
}

func (t *retestTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.TurnID == "" || inv.CallID == "" {
		return coretool.ErrorResult("缺少可信原生session/turn/call上下文，结论未保存"), nil
	}
	retest, err := t.store.retestForSession(ctx, inv.SessionID)
	if err != nil || retest == nil {
		return coretool.ErrorResult("当前session不是已保存的复测，结论未保存"), nil
	}
	args, canonical, err := validateRetestArguments(arguments)
	if err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	evidence, qualifies, err := t.store.sessionResultEvidence(ctx, retest.TaskID, retest.SessionID, retestEvidenceRefs(args.Evidence), "复测")
	if err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	if (args.Conclusion == "reproducible" && !qualifies["supporting"]) || (args.Conclusion == "fixed_in_scope" && !qualifies["counterevidence"]) {
		return coretool.ErrorResult("当前conclusion缺少对应relation的完整新HTTP证据；登录失败、不可达、超时不等于已修复，应给inconclusive"), nil
	}
	result, err := t.store.saveRetestResult(ctx, inv, retest, canonical, args, evidence)
	if err != nil {
		if errors.Is(err, errRetestResult) {
			return coretool.ErrorResult(errRetestResult.Error()), nil
		}
		return coretool.ErrorResult("复测结论保存或核对失败；结果未确认，请先核对现有记录，不自动重试或重发HTTP"), nil
	}
	raw, err := json.Marshal(map[string]any{
		"retest_id": retest.ID, "conclusion": args.Conclusion, "source": FindingSource{SessionID: result.SessionID, TurnID: result.TurnID, CallID: result.CallID},
		"evidence": result.Evidence, "note": "仅保存本次复测的模型结论；不覆盖原候选、修订或验证，状态码不是漏洞或修复证明",
	})
	if err != nil {
		return nil, errors.New("复测结论已保存但结果编码失败")
	}
	return coretool.TextResult(string(raw)), nil
}
