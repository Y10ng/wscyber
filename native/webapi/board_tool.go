package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// 黑板模型工具：任务内任意会话（研究/验证/复测/子 Agent）可读；写只允许该任务研究会话（initial_session_id）
// 与其 oracle_explorer 子会话（M5a：地图/事实/propose 可写，Oracle 只能结案自己认领的那一个，不能 claim 其它）。
// 每次读写前先把新流量摄取进地图。契约见 native/BOARD.md。

const (
	maxBoardPayloadBytes = 256 << 10
	boardReadDefault     = 50
	boardReadMax         = 100
)

// ---- 参数 ----

type boardReadArguments struct {
	Section  string `json:"section" jsonschema:"required,enum=summary,enum=endpoints,enum=facts,enum=oracles"`
	Status   string `json:"status,omitempty" jsonschema:"description=endpoints: untested；facts: active(默认)/retracted；oracles: open/in_progress/holds/refuted/blocked/dropped"`
	Category string `json:"category,omitempty" jsonschema:"description=facts: kind；oracles: category"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=1..100，默认50"`
}

type boardMapInput struct {
	Method string `json:"method" jsonschema:"required"`
	URL    string `json:"url" jsonschema:"required,description=绝对http(s) URL；ID段会归一为{id}"`
	Note   string `json:"note,omitempty" jsonschema:"description=来源与用途，≤500字"`
}

type boardMapArguments struct {
	Endpoints []boardMapInput `json:"endpoints" jsonschema:"required,description=1..50个从文档/JS/页面得知但尚未请求的接口"`
}

type boardFactArguments struct {
	Add     []boardFactInput `json:"add,omitempty" jsonschema:"description=≤20条"`
	Retract []string         `json:"retract,omitempty" jsonschema:"description=要撤回的事实id，≤20个"`
}

type boardOracleArguments struct {
	Action      string             `json:"action" jsonschema:"required,enum=propose,enum=claim,enum=close,enum=drop"`
	Items       []boardOracleInput `json:"items,omitempty" jsonschema:"description=propose：1..20项"`
	Number      string             `json:"number,omitempty" jsonschema:"description=claim/close/drop：Oracle编号，如O-001"`
	Status      string             `json:"status,omitempty" jsonschema:"description=close：holds/refuted/blocked"`
	Summary     string             `json:"summary,omitempty" jsonschema:"description=close/drop：结论或原因，≤2000字"`
	EvidenceIDs []string           `json:"evidence_ids,omitempty" jsonschema:"description=close：本任务已发送evidence_id，≤20个；holds/refuted至少1个"`
	FindingID   string             `json:"finding_id,omitempty" jsonschema:"description=close holds：同任务候选finding_id（webapi_finding返回）"`
}

// ---- 公共 ----

type boardToolContext struct {
	inv  operation.Invocation
	task *Task
	// subsession 非空表示调用方是本任务的 oracle_explorer 子会话。
	subsession *taskSubsessionModel
}

// boardToolSetup 取可信 Invocation 与任务；write 时只允许研究会话；随后摄取新流量。
func boardToolSetup(ctx context.Context, store *taskStore, write bool) (*boardToolContext, string) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.TurnID == "" || inv.CallID == "" {
		return nil, "缺少可信原生session/turn/call上下文，黑板未读写"
	}
	task, err := store.taskForSession(ctx, inv.SessionID)
	if err != nil {
		return nil, "无法核对session所属任务，黑板未读写"
	}
	if task == nil {
		return nil, "session无已保存任务，黑板未读写"
	}
	sub, err := store.subsessionFor(ctx, inv.SessionID)
	if err != nil {
		return nil, "无法核对session所属任务，黑板未读写"
	}
	if sub != nil && sub.TaskID != task.ID {
		sub = nil
	}
	if write && inv.SessionID != task.InitialSessionID && sub == nil {
		return nil, "只有本任务研究会话可写黑板；当前会话（验证/复测）只能用 webapi_board_read 读取，本次未写入"
	}
	if err := store.boardIngest(ctx, task.ID, time.Now()); err != nil {
		return nil, "摄取流量到黑板地图失败，黑板未读写"
	}
	return &boardToolContext{inv: inv, task: task, subsession: sub}, ""
}

func publicBoardError(err error) string {
	var input boardInputError
	if errors.As(err, &input) {
		return input.message
	}
	if errors.Is(err, errBoardOracleAbsent) {
		return err.Error()
	}
	return "黑板存储操作失败"
}

func boardJSONResult(value any) (*aop.ToolResult, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("黑板已写入但结果编码失败")
	}
	return coretool.TextResult(string(raw)), nil
}

func validateEvidenceIDs(path string, ids []string) error {
	if len(ids) > 20 {
		return fmt.Errorf("%s 数量过多：实际 %d，上限 20", path, len(ids))
	}
	seen := map[string]bool{}
	for index, id := range ids {
		if id == "" {
			return fmt.Errorf("%s[%d] 必填为空", path, index)
		}
		if len(id) > 128 {
			return fmt.Errorf("%s[%d] 超长：上限 128 字节", path, index)
		}
		if seen[id] {
			return fmt.Errorf("%s[%d] 重复", path, index)
		}
		seen[id] = true
	}
	return nil
}

func decodeBoardArguments(arguments string, target any) error {
	return decodeStrictToolPayloadLimit(arguments, target, "黑板", maxBoardPayloadBytes)
}

// ---- webapi_board_read ----

type boardReadTool struct{ store *taskStore }

func (*boardReadTool) Name() string { return "webapi_board_read" }
func (*boardReadTool) Description() string {
	return "Read the platform-held task board: summary, endpoint map, facts, or oracles (open questions still to prove). Read-only; sends no requests."
}
func (*boardReadTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_board_read", "读取平台持有的任务黑板：summary（摘要）、endpoints（接口地图，自动从已发送流量整理）、facts（有证据的事实）、oracles（待证明问题＝还未走的路）。只读，不发送请求。", boardReadArguments{})
}

func (t *boardReadTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	var args boardReadArguments
	if err := decodeBoardArguments(arguments, &args); err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	limit := boardReadDefault
	if args.Limit != 0 {
		if args.Limit < 1 || args.Limit > boardReadMax {
			return coretool.ErrorResult(fmt.Sprintf("limit 须为 1..%d：实际 %d", boardReadMax, args.Limit)), nil
		}
		limit = args.Limit
	}
	switch args.Section {
	case "summary":
		if args.Status != "" || args.Category != "" || args.Limit != 0 {
			return coretool.ErrorResult("section=summary 不接受 status/category/limit"), nil
		}
	case "endpoints":
		if args.Status != "" && args.Status != "untested" {
			return coretool.ErrorResult("section=endpoints 的 status 仅支持 untested（尚无 Oracle 引用）"), nil
		}
		if args.Category != "" {
			return coretool.ErrorResult("section=endpoints 不接受 category"), nil
		}
	case "facts":
		if args.Status != "" && args.Status != factActive && args.Status != factRetracted {
			return coretool.ErrorResult("section=facts 的 status 仅支持 active/retracted（默认 active）"), nil
		}
		if args.Category != "" && !slices.Contains(boardFactKinds, args.Category) {
			return coretool.ErrorResult("section=facts 的 category（kind）仅支持 " + strings.Join(boardFactKinds, "/")), nil
		}
	case "oracles":
		if args.Status != "" && !slices.Contains(boardOracleStatuses, args.Status) {
			return coretool.ErrorResult("section=oracles 的 status 仅支持 " + strings.Join(boardOracleStatuses, "/")), nil
		}
		if args.Category != "" && !slices.Contains(boardOracleCategory, args.Category) {
			return coretool.ErrorResult("section=oracles 的 category 仅支持 " + strings.Join(boardOracleCategory, "/")), nil
		}
	default:
		return coretool.ErrorResult("section 仅支持 summary/endpoints/facts/oracles"), nil
	}
	setup, message := boardToolSetup(ctx, t.store, false)
	if setup == nil {
		return coretool.ErrorResult(message), nil
	}
	text, err := t.read(ctx, setup.task.ID, args, limit)
	if err != nil {
		return coretool.ErrorResult("读取黑板失败"), nil
	}
	return coretool.TextResult(text), nil
}

func (t *boardReadTool) read(ctx context.Context, taskID string, args boardReadArguments, limit int) (string, error) {
	store := t.store
	switch args.Section {
	case "summary":
		return store.boardDigest(ctx, taskID, boardDigestDefaultChars)
	case "endpoints":
		counts, err := store.boardCounts(ctx, taskID)
		if err != nil {
			return "", err
		}
		items, err := store.boardEndpoints(ctx, taskID, boardEndpointFilter{untested: args.Status == "untested", limit: limit})
		if err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("端点：共 %d（尚无 Oracle 引用 %d），本次显示 %d%s", counts.Endpoints, counts.UntestedEndpoints, len(items), filterNote(args.Status))}
		for _, item := range items {
			lines = append(lines, "- "+boardEndpointLine(item))
		}
		return strings.Join(lines, "\n"), nil
	case "facts":
		status := args.Status
		if status == "" {
			status = factActive
		}
		items, err := store.boardFacts(ctx, taskID, boardFactFilter{kind: args.Category, status: status, limit: limit})
		if err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("事实（%s%s）：本次显示 %d，按时间倒序", status, kindNote(args.Category), len(items))}
		for _, item := range items {
			line := fmt.Sprintf("- %s [%s·%s] %s：%s", item.ID, item.Kind, item.Confidence, oneLine(item.Subject), oneLine(item.Statement))
			if len(item.EvidenceIDs) > 0 {
				line += "（证据 " + strings.Join(item.EvidenceIDs, ", ") + "）"
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n"), nil
	default:
		items, err := store.boardOracles(ctx, taskID, boardOracleFilter{status: args.Status, category: args.Category, limit: limit, modelOrder: true}, false)
		if err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("Oracle：本次显示 %d（进行中/待办在前，按优先级）", len(items))}
		for _, item := range items {
			lines = append(lines, "- "+boardOracleLine(item))
		}
		return strings.Join(lines, "\n"), nil
	}
}

func filterNote(status string) string {
	if status == "untested" {
		return "（仅尚无 Oracle 引用）"
	}
	return ""
}

func kindNote(kind string) string {
	if kind == "" {
		return ""
	}
	return "，kind=" + kind
}

func boardEndpointLine(item BoardEndpoint) string {
	parts := []string{item.EndpointKey}
	if item.RequestCount == 0 {
		parts = append(parts, "未请求")
	} else {
		codes := make([]string, 0, len(item.StatusCodes))
		for _, code := range item.StatusCodes {
			codes = append(codes, strconv.Itoa(code))
		}
		parts = append(parts, fmt.Sprintf("请求 %d 次 状态 %s", item.RequestCount, strings.Join(codes, ",")), "auth="+item.AuthSeen)
	}
	if len(item.Params) > 0 {
		parts = append(parts, "参数 "+strings.Join(item.Params, ","))
	}
	parts = append(parts, "来源 "+item.Source)
	if len(item.Oracles) > 0 {
		parts = append(parts, "Oracle "+strings.Join(item.Oracles, ","))
	}
	if item.Note != "" {
		parts = append(parts, "备注 "+capRunes(oneLine(item.Note), 200))
	}
	return strings.Join(parts, " | ")
}

func boardOracleLine(item BoardOracle) string {
	line := fmt.Sprintf("%s [%s·%s·%s] %s → %s", item.Number, item.Priority, item.Status, item.Category, oneLine(item.Question), boardKeysBrief(item.EndpointKeys, 5))
	if item.ResultSummary != "" {
		line += " | 结论：" + capRunes(oneLine(item.ResultSummary), 300)
	}
	if item.FindingNumber != "" {
		line += "（候选 " + item.FindingNumber + "）"
	}
	if len(item.EvidenceIDs) > 0 {
		line += "（证据 " + strings.Join(item.EvidenceIDs, ", ") + "）"
	}
	return line
}

// ---- webapi_board_map ----

type boardMapTool struct{ store *taskStore }

func (*boardMapTool) Name() string { return "webapi_board_map" }
func (*boardMapTool) Description() string {
	return "Add endpoints learned from docs/JS/pages but not yet requested to the task board map (source=model). Sends no requests and does not extend scope."
}
func (*boardMapTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_board_map", "把从文档/JS/页面得知但尚未请求的接口登记进任务黑板地图（source=model）；不发送请求，不扩大授权范围。已发送且有响应的请求会自动并入地图，无需登记。", boardMapArguments{})
}

func (t *boardMapTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	var args boardMapArguments
	if err := decodeBoardArguments(arguments, &args); err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	if len(args.Endpoints) < 1 || len(args.Endpoints) > 50 {
		return coretool.ErrorResult(fmt.Sprintf("endpoints 数量须为 1..50：实际 %d", len(args.Endpoints))), nil
	}
	keys, notes := []string{}, []string{}
	position := map[string]int{}
	for index, item := range args.Endpoints {
		path := fmt.Sprintf("endpoints[%d]", index)
		key, err := normalizeEndpoint(item.Method, item.URL)
		if err != nil {
			return coretool.ErrorResult(path + " " + err.Error()), nil
		}
		if err := boardTextError(path+".note", item.Note, 500, false); err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
		note := strings.TrimSpace(item.Note)
		if existing, ok := position[key]; ok {
			if note != "" {
				notes[existing] = note
			}
			continue
		}
		position[key] = len(keys)
		keys, notes = append(keys, key), append(notes, note)
	}
	setup, message := boardToolSetup(ctx, t.store, true)
	if setup == nil {
		return coretool.ErrorResult(message), nil
	}
	results, err := t.store.boardMapEndpoints(ctx, setup.task.ID, keys, notes, time.Now())
	if err != nil {
		return coretool.ErrorResult(publicBoardError(err)), nil
	}
	return boardJSONResult(map[string]any{"endpoints": results, "note": "仅登记地图；未发送请求"})
}

// ---- webapi_board_fact ----

type boardFactTool struct{ store *taskStore }

func (*boardFactTool) Name() string { return "webapi_board_fact" }
func (*boardFactTool) Description() string {
	return "Add objective facts to the task board (confirmed facts must cite sent evidence IDs from this task) or retract earlier facts by id."
}
func (*boardFactTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_board_fact", "向任务黑板新增客观事实（confirmed 必须引用本任务已发送的 evidence_id；inferred 为推断）或按 id 撤回旧事实。事实不是漏洞结论。", boardFactArguments{})
}

func validateFactArguments(args boardFactArguments) error {
	if len(args.Add) == 0 && len(args.Retract) == 0 {
		return errors.New("add 与 retract 至少提供一项")
	}
	if len(args.Add) > 20 {
		return fmt.Errorf("add 数量过多：实际 %d，上限 20", len(args.Add))
	}
	if len(args.Retract) > 20 {
		return fmt.Errorf("retract 数量过多：实际 %d，上限 20", len(args.Retract))
	}
	for index, item := range args.Add {
		path := fmt.Sprintf("add[%d]", index)
		if !slices.Contains(boardFactKinds, item.Kind) {
			return fmt.Errorf("%s.kind 仅支持 %s", path, strings.Join(boardFactKinds, "/"))
		}
		if err := boardTextError(path+".subject", item.Subject, 200, true); err != nil {
			return err
		}
		if err := boardTextError(path+".statement", item.Statement, 2000, true); err != nil {
			return err
		}
		if item.Confidence != factConfirmed && item.Confidence != factInferred {
			return fmt.Errorf("%s.confidence 仅支持 confirmed/inferred", path)
		}
		if err := validateEvidenceIDs(path+".evidence_ids", item.EvidenceIDs); err != nil {
			return err
		}
		if item.Confidence == factConfirmed && len(item.EvidenceIDs) == 0 {
			return fmt.Errorf("%s.evidence_ids 数量不足：confidence=confirmed 至少 1 条本任务已发送的 evidence_id；无证据请用 inferred", path)
		}
	}
	seen := map[string]bool{}
	for index, id := range args.Retract {
		if id == "" || len(id) > 128 {
			return fmt.Errorf("retract[%d] 为空或超过 128 字节", index)
		}
		if seen[id] {
			return fmt.Errorf("retract[%d] 重复", index)
		}
		seen[id] = true
	}
	return nil
}

func (t *boardFactTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	var args boardFactArguments
	if err := decodeBoardArguments(arguments, &args); err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	if err := validateFactArguments(args); err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	setup, message := boardToolSetup(ctx, t.store, true)
	if setup == nil {
		return coretool.ErrorResult(message), nil
	}
	add := make([]boardFactInput, 0, len(args.Add))
	for index, item := range args.Add {
		if err := t.store.boardCheckEvidence(ctx, setup.task.ID, fmt.Sprintf("add[%d].evidence_ids", index), item.EvidenceIDs); err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
		item.Subject, item.Statement = strings.TrimSpace(item.Subject), strings.TrimSpace(item.Statement)
		add = append(add, item)
	}
	result, err := t.store.boardWriteFacts(ctx, setup.task.ID, setup.inv.SessionID, setup.inv.TurnID, setup.inv.CallID, add, args.Retract, time.Now())
	if err != nil {
		return coretool.ErrorResult(publicBoardError(err)), nil
	}
	return boardJSONResult(result)
}

// ---- webapi_board_oracle ----

type boardOracleTool struct{ store *taskStore }

func (*boardOracleTool) Name() string { return "webapi_board_oracle" }
func (*boardOracleTool) Description() string {
	return "Manage task board oracles (concrete falsifiable questions still to prove): propose, claim, close (holds/refuted/blocked) or drop. A holding oracle must link a candidate finding; it is not a confirmed vulnerability."
}
func (*boardOracleTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_board_oracle", "管理任务黑板 Oracle（具体可证伪的待证明问题）：propose 提出（编号由平台生成，重复问题返回已有编号）、claim 认领、close 结论（holds 需同任务候选 finding_id + ≥1 证据；refuted 需 ≥1 证据；blocked 只需 summary）、drop 放弃。成立≠漏洞确认，候选仍需独立验证。", boardOracleArguments{})
}

func (args boardOracleArguments) unexpected(allowed ...string) error {
	present := map[string]bool{"items": len(args.Items) > 0, "number": args.Number != "", "status": args.Status != "", "summary": args.Summary != "",
		"evidence_ids": len(args.EvidenceIDs) > 0, "finding_id": args.FindingID != ""}
	extra := []string{}
	for _, field := range []string{"items", "number", "status", "summary", "evidence_ids", "finding_id"} {
		if present[field] && !slices.Contains(allowed, field) {
			extra = append(extra, field)
		}
	}
	if len(extra) > 0 {
		return fmt.Errorf("action=%s 不接受字段 %s", args.Action, strings.Join(extra, "/"))
	}
	return nil
}

func (t *boardOracleTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	var args boardOracleArguments
	if err := decodeBoardArguments(arguments, &args); err != nil {
		return coretool.ErrorResult(err.Error()), nil
	}
	var number int64
	switch args.Action {
	case "propose":
		if err := args.unexpected("items"); err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
		if len(args.Items) < 1 || len(args.Items) > 20 {
			return coretool.ErrorResult(fmt.Sprintf("items 数量须为 1..20：实际 %d", len(args.Items))), nil
		}
	case "claim", "close", "drop":
		allowed := map[string][]string{"claim": {"number"}, "close": {"number", "status", "summary", "evidence_ids", "finding_id"}, "drop": {"number", "summary"}}[args.Action]
		if err := args.unexpected(allowed...); err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
		value, ok := parseOracleNumber(args.Number)
		if !ok {
			return coretool.ErrorResult("number 必填，须为 Oracle 编号（如 O-001）"), nil
		}
		number = value
	default:
		return coretool.ErrorResult("action 仅支持 propose/claim/close/drop"), nil
	}
	proposals := []boardOracleProposal{}
	for index, item := range args.Items {
		proposal, err := validateOracleInput(fmt.Sprintf("items[%d]", index), item)
		if err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
		proposals = append(proposals, proposal)
	}
	if args.Action == "close" {
		if err := validateOracleClose(args); err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
	}
	if args.Action == "drop" {
		if err := boardTextError("summary", args.Summary, 2000, true); err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
	}
	setup, message := boardToolSetup(ctx, t.store, true)
	if setup == nil {
		return coretool.ErrorResult(message), nil
	}
	taskID, sessionID := setup.task.ID, setup.inv.SessionID
	if args.Action == "propose" {
		results, err := t.store.boardProposeOracles(ctx, taskID, "model", sessionID, proposals, time.Now())
		if err != nil {
			return coretool.ErrorResult(publicBoardError(err)), nil
		}
		return boardJSONResult(map[string]any{"items": results, "note": "created=false 表示同类别、同端点集合的相同问题已存在，返回已有编号"})
	}
	if args.Action == "close" {
		if err := t.store.boardCheckEvidence(ctx, taskID, "evidence_ids", args.EvidenceIDs); err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
		if args.FindingID != "" {
			findingNumber, err := t.store.boardFindingNumber(ctx, taskID, args.FindingID)
			if err != nil {
				return coretool.ErrorResult("核对候选失败，Oracle 未更新"), nil
			}
			if findingNumber == "" {
				return coretool.ErrorResult(fmt.Sprintf("finding_id=%q 不是本任务候选（用 webapi_finding 返回的 finding_id）", args.FindingID)), nil
			}
		}
	}
	label := oracleNumber(number)
	// 认领排他（M5a）：子 Agent 持有（运行中或预占中）的 Oracle 只有该子会话能 close/drop；子会话只能动自己认领的那一个。
	holder := ""
	if current, err := t.store.boardOracle(ctx, taskID, number); err != nil {
		return coretool.ErrorResult(publicBoardError(err)), nil
	} else if current != nil {
		if holder, err = t.store.oracleSubagentHolder(ctx, t.store.orm, current.ID); err != nil {
			return coretool.ErrorResult(publicBoardError(err)), nil
		}
	}
	if setup.subsession != nil && args.Action == "claim" && holder != sessionID {
		return coretool.ErrorResult(fmt.Sprintf("子 Agent 只负责认领时指定的 Oracle，不能 claim %s", label)), nil
	}
	change := boardOracleChange{actor: "model", sessionID: sessionID, apply: func(model *boardOracleModel) (string, error) {
		if oracleClosed(model.Status) {
			return "", boardInputError{fmt.Sprintf("冲突（409）：%s 已是终态 %s，不能再 %s；如需重新评估，请用户先在任务页重开（status=open）", label, model.Status, args.Action)}
		}
		if setup.subsession != nil && (model.ClaimedSessionID != sessionID || holder != sessionID) {
			return "", boardInputError{fmt.Sprintf("冲突（409）：子 Agent 只能 %s 自己认领的 Oracle，%s 不是本子会话认领的", args.Action, label)}
		}
		if setup.subsession == nil && holder != "" {
			return "", boardInputError{fmt.Sprintf("冲突（409）：%s 正由子 Agent 探索（%s），不能 %s；等子 Agent 结束或由用户在任务页重开/放弃", label, holderText(holder), args.Action)}
		}
		switch args.Action {
		case "claim":
			if model.Status == oracleOpen {
				model.Status, model.ClaimedSessionID = oracleInProgress, sessionID
			}
			return "认领", nil
		case "close":
			model.Status, model.ResultSummary = args.Status, strings.TrimSpace(args.Summary)
			model.EvidenceIDsJSON, model.FindingID = marshalStrings(args.EvidenceIDs), args.FindingID
			return model.ResultSummary, nil
		default:
			model.Status, model.ResultSummary = oracleDropped, strings.TrimSpace(args.Summary)
			return model.ResultSummary, nil
		}
	}}
	update, err := t.store.boardChangeOracle(ctx, taskID, number, change, time.Now())
	if err != nil {
		return coretool.ErrorResult(publicBoardError(err)), nil
	}
	result := map[string]any{"number": label, "status": update.after.Status, "changed": update.changed}
	if update.after.Status == oracleHolds {
		result["note"] = "Oracle 成立只记录结论并关联候选；候选仍需独立验证，不等于漏洞确认"
	}
	return boardJSONResult(result)
}

func validateOracleClose(args boardOracleArguments) error {
	if args.Status != oracleHolds && args.Status != oracleRefuted && args.Status != oracleBlocked {
		return errors.New("status 仅支持 holds/refuted/blocked")
	}
	if err := boardTextError("summary", args.Summary, 2000, true); err != nil {
		return err
	}
	if err := validateEvidenceIDs("evidence_ids", args.EvidenceIDs); err != nil {
		return err
	}
	switch args.Status {
	case oracleHolds:
		if args.FindingID == "" {
			return errors.New("status=holds 必须提供 finding_id：先用 webapi_finding 保存候选，再关联其 finding_id")
		}
		if len(args.EvidenceIDs) == 0 {
			return errors.New("status=holds 的 evidence_ids 数量不足：至少 1 条本任务已发送的 evidence_id")
		}
	case oracleRefuted:
		if len(args.EvidenceIDs) == 0 {
			return errors.New("status=refuted 的 evidence_ids 数量不足：至少 1 条本任务已发送的 evidence_id")
		}
	}
	if args.FindingID != "" && args.Status != oracleHolds {
		return errors.New("finding_id 只用于 status=holds")
	}
	if len(args.FindingID) > 128 {
		return errors.New("finding_id 超长：上限 128 字节")
	}
	return nil
}

func holderText(holder string) string {
	if holder == "reserved" {
		return "已预占、尚未启动"
	}
	return "会话 " + holder
}
