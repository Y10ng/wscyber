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
	"unicode/utf8"

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

// verificationKeyStep 是最小复现的一个关键请求（K1）：引用本验证会话的完整新证据，marks 只用于高亮。
type verificationKeyStep struct {
	Title         string   `json:"title" jsonschema:"required,description=本步做什么"`
	EvidenceID    string   `json:"evidence_id" jsonschema:"required,description=本验证会话的完整新HTTP证据ID"`
	RequestMarks  []string `json:"request_marks,omitempty" jsonschema:"description=请求原件中需高亮的单行文本片段（最多5个）"`
	ResponseMarks []string `json:"response_marks,omitempty" jsonschema:"description=响应原件中需高亮的单行文本片段（最多5个）"`
}

// verificationContrast 是正常对照（越权类应提供）：同一操作在正常权限下与异常请求的对比。
type verificationContrast struct {
	NormalEvidenceID   string `json:"normal_evidence_id" jsonschema:"required"`
	AbnormalEvidenceID string `json:"abnormal_evidence_id" jsonschema:"required"`
	Note               string `json:"note,omitempty" jsonschema:"description=差异摘要"`
}

type verificationToolPayload struct {
	Verdict       string                      `json:"verdict" jsonschema:"required,enum=supported,enum=counterevidence,enum=inconclusive"`
	Method        string                      `json:"method" jsonschema:"required"`
	Preconditions string                      `json:"preconditions,omitempty"`
	Summary       string                      `json:"summary" jsonschema:"required"`
	Limitations   string                      `json:"limitations,omitempty"`
	Evidence      []verificationEvidenceInput `json:"evidence,omitempty"`
	// 最小复现与 AI 建议等级（K1）：仅 verdict=supported 时填写，supported 时 impact 与 key_steps 必填。
	Impact            string                `json:"impact,omitempty" jsonschema:"description=仅supported：一句话影响（谁、什么条件下、能做什么）"`
	KeySteps          []verificationKeyStep `json:"key_steps,omitempty" jsonschema:"description=仅supported：最小复现的关键请求，删去非必要步骤"`
	Contrast          *verificationContrast `json:"contrast,omitempty" jsonschema:"description=仅supported：正常对照，越权类应提供"`
	Remediation       []string              `json:"remediation,omitempty" jsonschema:"description=仅supported：修复要点"`
	RetestMethod      string                `json:"retest_method,omitempty" jsonschema:"description=仅supported：复测方法（重放哪一步、期望什么结果）"`
	SuggestedSeverity string                `json:"suggested_severity,omitempty" jsonschema:"enum=critical,enum=high,enum=medium,enum=low,enum=info,description=仅supported：AI建议等级，不等于人工定级"`
	SeverityReason    string                `json:"severity_reason,omitempty" jsonschema:"description=建议等级理由"`
}

// hasRepro：结果带最小复现（supported 且 key_steps 非空）。
func (p verificationToolPayload) hasRepro() bool {
	return p.Verdict == "supported" && len(p.KeySteps) > 0
}

const maxReproMarksPerSide = evidenceMaxMarks

type verificationTool struct{ store *taskStore }

func (*verificationTool) Name() string { return "webapi_verification_result" }
func (*verificationTool) Description() string {
	return "Save an immutable model assessment for the current verification session using only new HTTP evidence from this session. This does not confirm the original candidate. When verdict=supported, also give a minimal reproduction (impact, key_steps, contrast for access-control issues) and a suggested severity."
}
func (*verificationTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_verification_result", "保存当前独立验证的模型判读；只引用本会话新HTTP证据，支持/反证须完整响应，状态码不判资格，不修改原候选。verdict=supported 时须给出最小复现（impact 一句话影响；key_steps 删去非必要步骤、只留能证明问题的请求；越权类给 contrast 正常对照）与建议等级（suggested_severity、severity_reason）。", verificationToolPayload{})
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
		return fmt.Errorf("$: 参数不是strict JSON：JSON语法错误，字节位置 %d", syntaxError.Offset)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("$: 参数不是strict JSON：JSON语法错误，字节位置 %d（内容不完整）", len(arguments)+1)
	}
	if errors.Is(err, io.EOF) {
		return errors.New("$: 参数不是strict JSON：JSON内容为空；期望 JSON 对象")
	}
	return errors.New("$: 参数不是strict JSON：无法解码；期望 JSON 对象")
}

// decodeStrictToolPayload 是验证/复测结果共用的单对象JSON解码；label为“验证”或“复测”。
func decodeStrictToolPayload(arguments string, target any, label string) error {
	return decodeStrictToolPayloadLimit(arguments, target, label, maxVerificationPayloadBytes)
}

func decodeStrictToolPayloadLimit(arguments string, target any, label string, maximum int) error {
	if len(arguments) > maximum {
		return fmt.Errorf("$: %s载荷超过%dKiB；期望不超过此字节上限的 JSON 对象", label, maximum>>10)
	}
	// 模型工具（验证/复测/报告草稿/候选/黑板）忽略未知字段；人用 HTTP 管理接口各自严格解码，不走这里。
	decoder := json.NewDecoder(strings.NewReader(arguments))
	if err := decoder.Decode(target); err != nil {
		return strictToolJSONError(err, arguments)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err != nil {
			return strictToolJSONError(err, arguments)
		}
		return errors.New("$: 参数只能包含一个JSON对象")
	}
	return nil
}

// validateResultBody 校验验证/复测结果共用的正文长度与证据引用形状。
func validateResultBody(method, preconditions, summary, limitations string, evidence []verificationEvidenceInput, label string) error {
	var problems toolValidationErrors
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
			problems.add(fmt.Errorf("%s参数校验失败：%w", label, err))
		}
	}
	seen := map[string]bool{}
	for index, item := range evidence {
		path := fmt.Sprintf("evidence[%d]", index)
		if item.EvidenceID == "" {
			problems.add(fmt.Errorf("%s参数 %s.evidence_id 必填为空；期望非空值", label, path))
		}
		if len(item.EvidenceID) > 128 {
			problems.add(fmt.Errorf("%s参数 %s.evidence_id 超长：实际 %d 字节，上限 128 字节", label, path, len(item.EvidenceID)))
		}
		if seen[item.EvidenceID] {
			problems.add(fmt.Errorf("%s参数 %s.evidence_id 重复；请删除重复项", label, path))
		}
		if item.Relation != "supporting" && item.Relation != "counterevidence" && item.Relation != "limitation" {
			problems.add(fmt.Errorf("%s参数 %s.relation 仅支持supporting/counterevidence/limitation", label, path))
		}
		seen[item.EvidenceID] = true
	}
	return problems.err()
}

func canonicalToolPayload(args any, label string) ([]byte, error) {
	canonical, err := json.Marshal(args)
	if err != nil || len(canonical) > maxVerificationPayloadBytes {
		return nil, errors.New("$: " + label + "规范载荷超过24KiB；期望规范 JSON 不超过24KiB")
	}
	return canonical, nil
}

func validateVerificationArguments(arguments string) (verificationToolPayload, []byte, error) {
	var args verificationToolPayload
	if err := decodeStrictToolPayload(arguments, &args, "验证"); err != nil {
		return args, nil, err
	}
	if args.Verdict != "supported" {
		args = args.withoutRepro()
	}
	var problems toolValidationErrors
	if args.Verdict != "supported" && args.Verdict != "counterevidence" && args.Verdict != "inconclusive" {
		problems.add(errors.New("verdict仅支持supported/counterevidence/inconclusive"))
	}
	if err := validateResultBody(args.Method, args.Preconditions, args.Summary, args.Limitations, args.Evidence, "验证"); err != nil {
		problems.add(err)
	}
	if err := validateReproFields(args); err != nil {
		problems.add(err)
	}
	if err := problems.err(); err != nil {
		return args, nil, err
	}
	canonical, err := canonicalToolPayload(args, "验证")
	return args, canonical, err
}

// reproTextError 校验最小复现文字字段：有效 UTF-8、无 NUL；required 时去空白后非空。不限字数（整体载荷字节上限兜底）。
func reproTextError(path, value string, required bool) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return fmt.Errorf("验证参数 %s 必须是有效UTF8且不含NUL", path)
	}
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("验证参数 %s 必填为空；期望非空值", path)
	}
	return nil
}

func reproEvidenceIDError(path, value string) error {
	if value == "" {
		return fmt.Errorf("验证参数 %s 必填为空；期望非空值", path)
	}
	if len(value) > 128 {
		return fmt.Errorf("验证参数 %s 超长：实际 %d 字节，上限 128 字节", path, len(value))
	}
	return nil
}

// reproMarksError：最多 5 个，每个为非空单行文本、不超过 100 字（与证据图片 mark 一致）。
func reproMarksError(path string, marks []string) error {
	var problems toolValidationErrors
	if len(marks) > maxReproMarksPerSide {
		problems.add(fmt.Errorf("验证参数 %s 数量过多：实际 %d，上限 %d", path, len(marks), maxReproMarksPerSide))
	}
	for index, mark := range marks {
		item := fmt.Sprintf("%s[%d]", path, index)
		if err := reproTextError(item, mark, true); err != nil {
			problems.add(err)
		} else if count := utf8.RuneCountInString(mark); count > evidenceMaxMarkRunes {
			problems.add(fmt.Errorf("验证参数 %s 超长：实际 %d 字，上限 %d 字", item, count, evidenceMaxMarkRunes))
		}
		if strings.ContainsAny(mark, "\r\n") {
			problems.add(fmt.Errorf("验证参数 %s 必须是单行文本片段", item))
		}
	}
	return problems.err()
}

// withoutRepro 清空最小复现与建议等级字段：非 supported 时模型多填的这些字段直接忽略，不报错、不保存。
func (p verificationToolPayload) withoutRepro() verificationToolPayload {
	p.Impact, p.KeySteps, p.Contrast, p.Remediation = "", nil, nil, nil
	p.RetestMethod, p.SuggestedSeverity, p.SeverityReason = "", "", ""
	return p
}

// validateReproFields 校验最小复现与 AI 建议等级的形状（证据归属在 Execute 中按会话核对）。
// 非 supported 时这些字段已被 withoutRepro 清空，不校验；supported 时 impact 与 key_steps 必填。
func validateReproFields(args verificationToolPayload) error {
	if args.Verdict != "supported" {
		return nil
	}
	var problems toolValidationErrors
	if err := reproTextError("impact", args.Impact, true); err != nil {
		problems.add(err)
	}
	if len(args.KeySteps) == 0 {
		problems.add(errors.New("验证参数 key_steps 必填为空：supported 须给出最小复现（只留能证明问题的请求）"))
	}
	seen := map[string]int{}
	for index, step := range args.KeySteps {
		path := fmt.Sprintf("key_steps[%d]", index)
		if err := reproTextError(path+".title", step.Title, true); err != nil {
			problems.add(err)
		}
		if err := reproEvidenceIDError(path+".evidence_id", step.EvidenceID); err != nil {
			problems.add(err)
		}
		if previous, ok := seen[step.EvidenceID]; ok {
			problems.add(fmt.Errorf("验证参数 %s.evidence_id 与 key_steps[%d] 重复；请删除重复项", path, previous))
		}
		seen[step.EvidenceID] = index
		if err := reproMarksError(path+".request_marks", step.RequestMarks); err != nil {
			problems.add(err)
		}
		if err := reproMarksError(path+".response_marks", step.ResponseMarks); err != nil {
			problems.add(err)
		}
	}
	if contrast := args.Contrast; contrast != nil {
		if err := reproEvidenceIDError("contrast.normal_evidence_id", contrast.NormalEvidenceID); err != nil {
			problems.add(err)
		}
		if err := reproEvidenceIDError("contrast.abnormal_evidence_id", contrast.AbnormalEvidenceID); err != nil {
			problems.add(err)
		}
		if contrast.NormalEvidenceID == contrast.AbnormalEvidenceID {
			problems.add(errors.New("验证参数 contrast.normal_evidence_id 与 contrast.abnormal_evidence_id 不能相同"))
		}
		if err := reproTextError("contrast.note", contrast.Note, false); err != nil {
			problems.add(err)
		}
	}
	for index, item := range args.Remediation {
		if err := reproTextError(fmt.Sprintf("remediation[%d]", index), item, true); err != nil {
			problems.add(err)
		}
	}
	if err := reproTextError("retest_method", args.RetestMethod, false); err != nil {
		problems.add(err)
	}
	if args.SuggestedSeverity != "" && !triageSeveritySet[args.SuggestedSeverity] {
		problems.add(errors.New("验证参数 suggested_severity 仅支持critical/high/medium/low/info"))
	}
	if args.SuggestedSeverity == "" && args.SeverityReason != "" {
		problems.add(errors.New("验证参数 severity_reason 须与 suggested_severity 一起填写"))
	}
	problems.add(reproTextError("severity_reason", args.SeverityReason, args.SuggestedSeverity != ""))
	return problems.err()
}

// reproEvidenceIDs 返回最小复现引用的全部证据（key_steps 与 contrast）及其参数路径。
func reproEvidenceIDs(args verificationToolPayload) [][2]string {
	refs := [][2]string{}
	for index, step := range args.KeySteps {
		refs = append(refs, [2]string{fmt.Sprintf("key_steps[%d].evidence_id", index), step.EvidenceID})
	}
	if args.Contrast != nil {
		refs = append(refs, [2]string{"contrast.normal_evidence_id", args.Contrast.NormalEvidenceID}, [2]string{"contrast.abnormal_evidence_id", args.Contrast.AbnormalEvidenceID})
	}
	return refs
}

// checkReproEvidence：最小复现引用同现有规则——只接受本验证session在本任务产生的新HTTP证据，
// 且须已发送、收到响应、完整捕获（关键请求与对照都要能展示原件）。
func (s *taskStore) checkReproEvidence(ctx context.Context, taskID, sessionID string, args verificationToolPayload) error {
	var problems toolValidationErrors
	for _, ref := range reproEvidenceIDs(args) {
		model, err := s.evidenceByID(ctx, taskID, ref[1])
		if err != nil || model == nil || model.SessionID != sessionID {
			problems.add(fmt.Errorf("验证参数 %s 只接受本验证session在本任务产生的新HTTP证据", ref[0]))
			continue
		}
		var flow traffic.Flow
		if proto.Unmarshal(model.FlowPB, &flow) != nil {
			problems.add(fmt.Errorf("验证参数 %s 的HTTP原件无法核对，结果未保存", ref[0]))
			continue
		}
		if !(model.Status == requestCompleted && model.Sent.Valid && model.Sent.Bool && model.StatusCode.Valid && flow.GetComplete() && !model.CaptureTruncated && flow.GetResponse() != nil) {
			problems.add(fmt.Errorf("验证参数 %s 必须是已发送、收到响应且完整捕获的新HTTP原件", ref[0]))
			continue
		}
	}
	return problems.err()
}

// sessionResultEvidence 核对结果引用：只接受本session在本任务产生的新HTTP证据；
// 支持/反证须已发送、completed、有实际响应、Flow完整且未截断，否则只能作为限制。
// 返回证据行与已满足的有效relation；label为“验证”或“复测”。
func (s *taskStore) sessionResultEvidence(ctx context.Context, taskID, sessionID string, refs []verificationEvidenceInput, label string) ([]requestModel, map[string]bool, error) {
	evidence := make([]requestModel, 0, len(refs))
	qualifies := map[string]bool{}
	var problems toolValidationErrors
	for index, reference := range refs {
		model, err := s.evidenceByID(ctx, taskID, reference.EvidenceID)
		if err != nil || model == nil || model.SessionID != sessionID {
			problems.add(fmt.Errorf("evidence[%d].evidence_id: 只接受本%ssession在本任务产生的新HTTP证据", index, label))
			continue
		}
		var flow traffic.Flow
		if proto.Unmarshal(model.FlowPB, &flow) != nil {
			problems.add(fmt.Errorf("evidence[%d].evidence_id: HTTP原件无法核对，结果未保存；请引用可读取的本会话新HTTP证据", index))
			continue
		}
		complete := model.Status == requestCompleted && model.Sent.Valid && model.Sent.Bool && model.StatusCode.Valid && flow.GetComplete() && !model.CaptureTruncated && flow.GetResponse() != nil
		if reference.Relation == "limitation" {
			// 失败、未发送和截断仅作为限制保存；完整响应也可以作为限制引用。
		} else if !complete {
			problems.add(fmt.Errorf("evidence[%d].evidence_id: 支持/反证引用必须是已发送、收到响应且完整捕获的新HTTP原件；不完整证据只能用 relation=limitation", index))
		} else {
			qualifies[reference.Relation] = true
		}
		evidence = append(evidence, *model)
	}
	if err := problems.err(); err != nil {
		return nil, nil, err
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
		return coretool.ErrorResult("当前是研究或其它会话，不能保存验证判读；把发现写成候选（webapi_finding）即可，独立验证由用户在任务页对候选发起，结果未保存"), nil
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
		return coretool.ErrorResult(requiredRelationError("verdict", args.Verdict, map[string]string{"supported": "supporting", "counterevidence": "counterevidence"}[args.Verdict])), nil
	}
	if err := t.store.checkReproEvidence(ctx, verification.TaskID, verification.SessionID, args); err != nil {
		return coretool.ErrorResult(err.Error()), nil
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
