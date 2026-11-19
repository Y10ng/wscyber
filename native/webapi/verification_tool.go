package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
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

func jsonTypeName(valueType reflect.Type) string {
	if valueType == nil {
		return "未知类型"
	}
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	switch valueType.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Array, reflect.Slice:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	default:
		return valueType.String()
	}
}

type jsonPathFrame struct {
	path         string
	object       bool
	expectingKey bool
	index        int
}

func joinJSONFieldPath(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

// jsonPathAtOffset 用类型错误的字节位置补回encoding/json未提供的数组下标。
func jsonPathAtOffset(arguments string, offset int64) string {
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.UseNumber()
	stack := []jsonPathFrame{}
	currentPath := ""
	finishValue := func() {
		if len(stack) == 0 {
			return
		}
		parent := &stack[len(stack)-1]
		if parent.object {
			parent.expectingKey = true
		} else {
			parent.index++
			currentPath = fmt.Sprintf("%s[%d]", parent.path, parent.index)
		}
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				path := currentPath
				stack = append(stack, jsonPathFrame{path: path, object: delimiter == '{', expectingKey: delimiter == '{'})
				if delimiter == '[' {
					currentPath = path + "[0]"
				}
			case '}', ']':
				if len(stack) == 0 {
					return ""
				}
				currentPath = stack[len(stack)-1].path
				stack = stack[:len(stack)-1]
				if decoder.InputOffset() >= offset {
					return currentPath
				}
				finishValue()
			}
			continue
		}
		if len(stack) > 0 {
			current := &stack[len(stack)-1]
			if current.object && current.expectingKey {
				key, ok := token.(string)
				if !ok {
					return ""
				}
				current.expectingKey = false
				currentPath = joinJSONFieldPath(current.path, key)
				continue
			}
		}
		if decoder.InputOffset() >= offset {
			return currentPath
		}
		finishValue()
	}
}

func strictToolJSONError(err error, arguments string) error {
	const unknownPrefix = "json: unknown field "
	if message, ok := strings.CutPrefix(err.Error(), unknownPrefix); ok {
		if field, unquoteErr := strconv.Unquote(message); unquoteErr == nil {
			return fmt.Errorf("参数不是strict JSON：含未知字段 %q", field)
		}
	}
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		field := jsonPathAtOffset(arguments, typeError.Offset)
		if field == "" {
			field = typeError.Field
		}
		if field == "" {
			field = "参数"
		} else {
			field = "字段 " + field
		}
		actual := typeError.Value
		if actual == "bool" {
			actual = "boolean"
		}
		return fmt.Errorf("参数不是strict JSON：%s类型不符：期望 %s，实际 %s（字节位置 %d）", field, jsonTypeName(typeError.Type), actual, typeError.Offset)
	}
	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		return fmt.Errorf("参数不是strict JSON：JSON语法错误，字节位置 %d", syntaxError.Offset)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("参数不是strict JSON：JSON语法错误，字节位置 %d（内容不完整）", len(arguments)+1)
	}
	if errors.Is(err, io.EOF) {
		return errors.New("参数不是strict JSON：JSON内容为空")
	}
	return errors.New("参数不是strict JSON：无法解码")
}

// decodeStrictToolPayload 是验证/复测结果共用的strict单对象JSON解码；label为“验证”或“复测”。
func decodeStrictToolPayload(arguments string, target any, label string) error {
	return decodeStrictToolPayloadLimit(arguments, target, label, maxVerificationPayloadBytes)
}

func decodeStrictToolPayloadLimit(arguments string, target any, label string, maximum int) error {
	if len(arguments) > maximum {
		return fmt.Errorf("%s载荷超过%dKiB", label, maximum>>10)
	}
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return strictToolJSONError(err, arguments)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return strictToolJSONError(err, arguments)
		}
		return errors.New("参数只能包含一个JSON对象")
	}
	return nil
}

// validateResultBody 校验验证/复测结果共用的正文长度与证据引用形状。
func validateResultBody(method, preconditions, summary, limitations string, evidence []verificationEvidenceInput, label string) error {
	for _, field := range []struct {
		path     string
		value    string
		required bool
	}{
		{"method", method, true},
		{"preconditions", preconditions, false},
		{"summary", summary, true},
		{"limitations", limitations, false},
	} {
		if err := findingTextError(field.path, field.value, 4096, field.required); err != nil {
			return fmt.Errorf("%s参数校验失败：%w", label, err)
		}
	}
	if len(evidence) > 16 {
		return fmt.Errorf("%s参数 evidence 数量过多：实际 %d，上限 16", label, len(evidence))
	}
	seen := map[string]bool{}
	for index, item := range evidence {
		path := fmt.Sprintf("evidence[%d]", index)
		if item.EvidenceID == "" {
			return fmt.Errorf("%s参数 %s.evidence_id 必填为空", label, path)
		}
		if len(item.EvidenceID) > 128 {
			return fmt.Errorf("%s参数 %s.evidence_id 超长：实际 %d 字节，上限 128 字节", label, path, len(item.EvidenceID))
		}
		if seen[item.EvidenceID] {
			return fmt.Errorf("%s参数 %s.evidence_id 重复", label, path)
		}
		if item.Relation != "supporting" && item.Relation != "counterevidence" && item.Relation != "limitation" {
			return fmt.Errorf("%s参数 %s.relation 仅支持supporting/counterevidence/limitation", label, path)
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
	if args.Verdict == "supported" {
		// D2：登记企业微信推送（未配置不登记；失败只记日志，不影响已保存的判读）。
		notifyVerificationSupported(ctx, t.store, verification.ID)
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
