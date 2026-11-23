package webapi

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/uptrace/bun"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

const (
	authzCompareToolName   = "webapi_authz_compare"
	authzCompareCallPrefix = "authz_compare:"
	evidenceSourceAuthz    = "authz_compare"
	authzCompareListLimit  = 100

	authzHintSame      = "same_as_original"
	authzHintDenied    = "denied"
	authzHintNotFound  = "not_found"
	authzHintDifferent = "different"
	authzHintNotSent   = "not_sent"
)

type authzCompareVariantInput struct {
	Label         string            `json:"label" jsonschema:"required,description=变体名称，例如用户 B 或匿名，不超过40字"`
	SetHeaders    map[string]string `json:"set_headers,omitempty" jsonschema:"description=覆盖请求头；值支持账号占位符"`
	RemoveHeaders []string          `json:"remove_headers,omitempty" jsonschema:"description=要移除的请求头名"`
	Anonymous     bool              `json:"anonymous,omitempty" jsonschema:"description=移除 Authorization 与 Cookie；不得同时 set 这两个头"`
}

type authzCompareArguments struct {
	EvidenceID string                     `json:"evidence_id" jsonschema:"required,description=本任务已发送且有HTTP响应的原证据ID"`
	Variants   []authzCompareVariantInput `json:"variants" jsonschema:"required,description=按顺序执行的1到4个身份变体"`
}

type authzCompareTool struct {
	store   *taskStore
	secrets *secretBox
}

func (*authzCompareTool) Name() string { return authzCompareToolName }
func (*authzCompareTool) Description() string {
	return "Replay one saved HTTP request with 1-4 deterministic identity variants, retain evidence, and report comparison hints without deciding whether an authorization vulnerability exists."
}
func (*authzCompareTool) Definition() *aop.ToolDefinition {
	return coretool.Def(authzCompareToolName, "把本任务一条已发送且有响应的请求换成其它身份或匿名逐个重发并存证；方法、URL、正文不变，仅按变体改头。hint 只是确定性对比线索，不是越权结论。", authzCompareArguments{})
}

// authzCompareModel 每个变体一行；请求/响应本体仍在 webapi_requests。
type authzCompareModel struct {
	bun.BaseModel `bun:"table:webapi_authz_compares,alias:webapi_authz_compare"`

	ID                 string        `bun:"id,pk"`
	TaskID             string        `bun:"task_id,notnull"`
	SessionID          string        `bun:"session_id,notnull"`
	OriginalEvidenceID string        `bun:"original_evidence_id,notnull"`
	Label              string        `bun:"label,notnull"`
	VariantEvidenceID  string        `bun:"variant_evidence_id,notnull"`
	Status             string        `bun:"status,notnull"`
	Hint               string        `bun:"hint,notnull"`
	HTTPStatus         sql.NullInt64 `bun:"http_status"`
	CreatedAt          string        `bun:"created_at,notnull"`
}

// AuthzCompareItem 是只读列表项；variant_evidence_id 在 review_required 等未形成证据时为空。
type AuthzCompareItem struct {
	ID                 string `json:"id"`
	TaskID             string `json:"task_id"`
	SessionID          string `json:"session_id"`
	OriginalEvidenceID string `json:"original_evidence_id"`
	Label              string `json:"label"`
	VariantEvidenceID  string `json:"variant_evidence_id"`
	Status             string `json:"status"`
	Hint               string `json:"hint"`
	HTTPStatus         *int64 `json:"http_status"`
	CreatedAt          string `json:"created_at"`
}

func authzCompareItem(model authzCompareModel) AuthzCompareItem {
	item := AuthzCompareItem{ID: model.ID, TaskID: model.TaskID, SessionID: model.SessionID, OriginalEvidenceID: model.OriginalEvidenceID,
		Label: model.Label, VariantEvidenceID: model.VariantEvidenceID, Status: model.Status, Hint: model.Hint, CreatedAt: model.CreatedAt}
	if model.HTTPStatus.Valid {
		value := model.HTTPStatus.Int64
		item.HTTPStatus = &value
	}
	return item
}

func (s *taskStore) listAuthzCompares(ctx context.Context, taskID, originalEvidenceID string) ([]AuthzCompareItem, error) {
	var rows []authzCompareModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		query := db.NewSelect().Model(&rows).Where("task_id = ?", taskID)
		if originalEvidenceID != "" {
			query = query.Where("original_evidence_id = ?", originalEvidenceID)
		}
		return query.OrderExpr(`created_at COLLATE "C" DESC, id COLLATE "C" DESC`).Limit(authzCompareListLimit).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	items := make([]AuthzCompareItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, authzCompareItem(row))
	}
	return items, nil
}

func (c *Capability) handleAuthzCompareList(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	task, err := c.store.Get(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	items, err := c.store.listAuthzCompares(r.Context(), taskID, r.URL.Query().Get("evidence_id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取越权对比记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// AuthzCompareVariant 是工具结果中一个变体的确定性对比；detail 只在表格后补充错误/未发送原因。
type AuthzCompareVariant struct {
	Label         string  `json:"label"`
	EvidenceID    string  `json:"evidence_id"`
	Status        string  `json:"status"`
	HTTPStatus    *int64  `json:"http_status"`
	BodyBytes     int     `json:"body_bytes"`
	SameStatus    bool    `json:"same_status"`
	BodyIdentical bool    `json:"body_identical"`
	LengthRatio   float64 `json:"length_ratio"`
	Hint          string  `json:"hint"`

	detail string
}

type preparedAuthzVariant struct {
	input  authzCompareVariantInput
	req    *http.Request
	body   []byte
	review map[string]string
}

func (t *authzCompareTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.CallID == "" || inv.TurnID == "" {
		return stopTool("缺少可信原生 session/turn/call 上下文；0发送"), nil
	}
	args, err := coretool.ParseArgs[authzCompareArguments](arguments)
	if err != nil {
		return stopTool("参数不是合法请求JSON；0发送"), nil
	}
	task, err := t.store.taskForSession(ctx, inv.SessionID)
	if err != nil || task == nil {
		return stopTool("session无已保存任务授权；0发送"), nil
	}
	resolver, err := loadCredentialResolver(ctx, t.store, t.secrets, task.ID)
	if err != nil {
		return redactToolResult(stopTool("读取测试账号或密钥失败；0发送"), resolver), nil
	}
	fail := func(message string) (*aop.ToolResult, error) {
		return redactToolResult(stopTool(message), resolver), nil
	}
	args.EvidenceID = strings.TrimSpace(args.EvidenceID)
	if args.EvidenceID == "" {
		return fail("evidence_id 必填；0发送")
	}
	if len(args.Variants) < 1 || len(args.Variants) > 4 {
		return fail("variants 须为 1 至 4 个；0发送")
	}
	for index := range args.Variants {
		variant := &args.Variants[index]
		variant.Label = strings.TrimSpace(variant.Label)
		if variant.Label == "" || utf8.RuneCountInString(variant.Label) > 40 {
			return fail(fmt.Sprintf("variants[%d].label 必填且不超过 40 字；0发送", index))
		}
		for _, name := range sortedHeaderNames(variant.SetHeaders) {
			if anonymousAuthHeader(name) && variant.Anonymous {
				return fail(fmt.Sprintf("variants[%d] anonymous=true 时 set_headers 不得同时设置 %s；0发送", index, name))
			}
			if err := checkRequestHeader(name, variant.SetHeaders[name]); err != nil {
				return fail(fmt.Sprintf("variants[%d].set_headers：%v；0发送", index, err))
			}
			value, err := resolver.replace(variant.SetHeaders[name])
			if err != nil {
				return fail(fmt.Sprintf("variants[%d].set_headers[%s]：%v；0发送", index, name, err))
			}
			variant.SetHeaders[name] = value
		}
		for _, name := range variant.RemoveHeaders {
			if !httpgutsValidHeader(name, "") || strings.EqualFold(name, "Host") {
				return fail(fmt.Sprintf("variants[%d].remove_headers 含非法头名 %q；0发送", index, name))
			}
		}
	}
	original, err := t.store.evidenceByID(ctx, task.ID, args.EvidenceID)
	if err != nil {
		return fail("读取原证据失败；0发送")
	}
	if original == nil {
		return fail("原证据不存在于本任务；0发送")
	}
	if !original.Sent.Valid || !original.Sent.Bool || !original.StatusCode.Valid {
		return fail("原证据必须是本任务已发送且有 HTTP 状态码的记录（sent=1）；0发送")
	}
	originalFlow, err := decodeEvidenceFlow(original)
	if err != nil || originalFlow.GetRequest() == nil || originalFlow.GetResponse() == nil {
		return fail("原证据 Flow 缺少可比较的请求或响应；0发送")
	}
	policy, err := t.store.policyForTask(ctx, task.ID)
	if err != nil {
		return fail("无法读取任务测试策略；0发送")
	}
	prepared := make([]preparedAuthzVariant, 0, len(args.Variants))
	for _, variant := range args.Variants {
		req, _, err := replayHTTPRequest(originalFlow)
		if err != nil {
			return fail("原证据请求无法原样重建：" + err.Error() + "；0发送")
		}
		for _, name := range variant.RemoveHeaders {
			req.Header.Del(http.CanonicalHeaderKey(name))
		}
		if variant.Anonymous {
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
		}
		for _, name := range sortedHeaderNames(variant.SetHeaders) {
			value := variant.SetHeaders[name]
			if err := checkRequestHeader(name, value); err != nil {
				return fail(fmt.Sprintf("变体 %s 的 header %s 非法；0发送", variant.Label, name))
			}
			req.Header.Set(http.CanonicalHeaderKey(name), value)
		}
		prepared = append(prepared, preparedAuthzVariant{input: variant, req: req, body: originalFlow.GetRequest().GetBody(), review: joinedHTTPHeaders(req.Header)})
	}

	results := make([]AuthzCompareVariant, 0, len(prepared))
	for index, variant := range prepared {
		result := t.executeVariant(ctx, inv, task.ID, policy, task.Origins, index, variant, original, originalFlow)
		row := &authzCompareModel{ID: "wac_" + newSessionID() + newSessionID(), TaskID: task.ID, SessionID: inv.SessionID,
			OriginalEvidenceID: original.ID, Label: result.Label, VariantEvidenceID: result.EvidenceID, Status: result.Status,
			Hint: result.Hint, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if result.HTTPStatus != nil {
			row.HTTPStatus = sql.NullInt64{Int64: *result.HTTPStatus, Valid: true}
		}
		if err := t.store.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error { _, err := tx.NewInsert().Model(row).Exec(ctx); return err }); err != nil {
			message := fmt.Sprintf("变体 %s 已处理（证据 %s），但越权对比记录保存失败；停止后续变体，不要重发已形成证据的请求", result.Label, result.EvidenceID)
			return fail(message)
		}
		results = append(results, result)
	}
	return redactToolResult(coretool.TextResult(formatAuthzCompareResults(original.ID, results)), resolver), nil
}

func anonymousAuthHeader(name string) bool {
	return strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Cookie")
}

func joinedHTTPHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for name, values := range headers {
		out[name] = strings.Join(values, ", ")
	}
	return out
}

func (t *authzCompareTool) executeVariant(ctx context.Context, inv operation.Invocation, taskID string, policy TaskPolicy, origins []string, index int, variant preparedAuthzVariant, original *requestModel, originalFlow *traffic.Flow) AuthzCompareVariant {
	result := AuthzCompareVariant{Label: variant.input.Label, Status: "error", Hint: authzHintNotSent}
	variantInv := inv
	variantInv.CallID = fmt.Sprintf("%s%s:%d", authzCompareCallPrefix, inv.CallID, index+1)
	recorder := requestRecorder{store: t.store, inv: variantInv, taskID: taskID}
	decision, reason := httpPolicyDecision(policy, origins, variant.req, variant.review, variant.body)
	if decision == "review_required" {
		result.Status = "review_required"
		result.detail = "需人工审批，未发送；改用 webapi_http 走人工审批：" + reason
		return result
	}
	if decision == "denied" {
		model, _, err := recorder.recordUnsent(ctx, variant.req, variant.body, decision, reason+"（多账号越权对比）")
		if err != nil {
			if errors.Is(err, errUnknownReplay) && model != nil {
				result.Status, result.EvidenceID, result.detail = "unknown", model.ID, "相同请求已有未知结果，不自动重试"
				return result
			}
			result.detail = "保存 denied 证据失败，未发送"
			return result
		}
		result.Status = "denied"
		if model != nil {
			result.EvidenceID = model.ID
		}
		result.detail = reason + "；0发送"
		return result
	}

	res := recorder.send(ctx, variant.req, variant.body)
	var model *requestModel
	switch res.state {
	case requestCompleted:
		result.Status = "sent"
		model = res.model
	case sendExisting:
		model = res.model
		if model != nil && model.Status == requestCompleted {
			result.Status = "sent"
		} else {
			result.Status = "unknown"
			result.detail = "同调用已有未解决结果，不再次发送"
		}
	case sendClaimFailed:
		if errors.Is(res.err, errUnknownReplay) && res.model != nil {
			result.Status, result.EvidenceID, result.detail = "unknown", res.model.ID, "相同请求已有未知结果，不自动重试"
		} else {
			result.detail = "发送前认领失败，0发送：" + publicStoreError(res.err)
		}
		return result
	case sendLost, requestUnknown, requestCaptureFailed:
		result.Status = "unknown"
		model = res.model
		result.detail = res.note + "；不自动重试"
	default:
		result.detail = "发送结果无法判定；不自动重试"
		return result
	}
	if model != nil {
		result.EvidenceID = model.ID
		if model.StatusCode.Valid {
			value := model.StatusCode.Int64
			result.HTTPStatus = &value
		}
	}
	if result.Status != "sent" {
		if model != nil {
			if flow, err := decodeEvidenceFlow(model); err == nil {
				result.BodyBytes = len(flow.GetResponse().GetBody())
			}
		}
		return result
	}
	variantFlow, err := decodeEvidenceFlow(model)
	if err != nil || variantFlow.GetResponse() == nil {
		result.Status = "error"
		result.Hint = authzHintNotSent
		result.detail = "变体已发送并存证，但响应 Flow 无法解析，未做对比"
		return result
	}
	result.BodyBytes = len(variantFlow.GetResponse().GetBody())
	fillAuthzComparison(&result, original.StatusCode.Int64, originalFlow, variantFlow)
	return result
}

// fillAuthzComparison 只比较已捕获字节；长度比在原响应为 0 且双方均为 0 时定义为 1，原响应为 0 而变体非 0 时为 0。
func fillAuthzComparison(result *AuthzCompareVariant, originalStatus int64, original, variant *traffic.Flow) {
	originalBody, variantBody := original.GetResponse().GetBody(), variant.GetResponse().GetBody()
	result.SameStatus = result.HTTPStatus != nil && originalStatus == *result.HTTPStatus
	result.BodyIdentical = bytes.Equal(originalBody, variantBody)
	switch {
	case len(originalBody) == 0 && len(variantBody) == 0:
		result.LengthRatio = 1
	case len(originalBody) == 0:
		result.LengthRatio = 0
	default:
		result.LengthRatio = float64(len(variantBody)) / float64(len(originalBody))
	}
	result.Hint = authzComparisonHint(result.SameStatus, result.BodyIdentical, result.LengthRatio, result.HTTPStatus, variant.GetResponse())
}

func authzComparisonHint(sameStatus, identical bool, ratio float64, status *int64, response *traffic.HttpResponse) string {
	if status == nil {
		return authzHintNotSent
	}
	if *status == 401 || *status == 403 || *status >= 300 && *status < 400 && loginRedirect(response) {
		return authzHintDenied
	}
	if *status == 404 {
		return authzHintNotFound
	}
	if sameStatus && (identical || ratio >= 0.95 && ratio <= 1.05) {
		return authzHintSame
	}
	return authzHintDifferent
}

func loginRedirect(response *traffic.HttpResponse) bool {
	for _, header := range response.GetHeaders() {
		if strings.EqualFold(header.GetName(), "Location") {
			location := strings.ToLower(header.GetValue())
			return strings.Contains(location, "login") || strings.Contains(location, "signin")
		}
	}
	return false
}

// formatAuthzCompareResults 是给模型的表格化结果；hint 不下漏洞结论，并给出候选与验证的后续引用方式。
func formatAuthzCompareResults(originalEvidenceID string, results []AuthzCompareVariant) string {
	var b strings.Builder
	fmt.Fprintf(&b, "原请求证据：%s\n\n", originalEvidenceID)
	b.WriteString("| label | evidence_id | status | http_status | body_bytes | same_status | body_identical | length_ratio | hint |\n")
	b.WriteString("|---|---|---|---|---:|---|---|---:|---|\n")
	for _, result := range results {
		status := ""
		if result.HTTPStatus != nil {
			status = strconv.FormatInt(*result.HTTPStatus, 10)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %t | %t | %.4f | %s |\n",
			tableCell(result.Label), result.EvidenceID, result.Status, status, result.BodyBytes, result.SameStatus, result.BodyIdentical, result.LengthRatio, result.Hint)
	}
	for _, result := range results {
		if result.detail != "" {
			fmt.Fprintf(&b, "\n- %s：%s", result.Label, result.detail)
		}
	}
	b.WriteString("\n\n以上只是确定性对比提示，不是越权漏洞结论；same_as_original 也必须确认返回的是他人数据而非公共数据。\n")
	b.WriteString("如认为构成越权，用 webapi_finding 记录候选并在证据中引用原请求与变体证据；验证时用 contrast 引用。\n")
	b.WriteString("review_required 的变体未发送且不生成请求证据；请改用 webapi_http 走人工审批。")
	return b.String()
}
