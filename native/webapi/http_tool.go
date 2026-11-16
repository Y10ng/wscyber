package webapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maxCaptureBytes = 64 * 1024
	maxPreviewBytes = 4 * 1024
	maxRequestBody  = 1 << 20
	httpTimeout     = 30 * time.Second
)

type httpToolArguments struct {
	Method  string            `json:"method" jsonschema:"required,description=HTTP method"`
	URL     string            `json:"url" jsonschema:"required,description=Absolute HTTP(S) URL within the saved task authorization"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty" jsonschema:"description=Literal request body bytes as UTF-8 text"`
}

type httpTool struct{ store *taskStore }

// ToolExtension 在原生Harness工具集中注册，复用Capability同一store。
func (c *Capability) ToolExtension() extension.Extension {
	var subscriptions []*corehooks.Subscription
	return extension.Func{
		LoadFunc: func(scope *extension.Scope) error {
			if err := extension.Add[coretool.Tool](scope, &httpTool{store: c.store}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &findingTool{store: c.store}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &verificationTool{store: c.store}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &reportDraftTool{store: c.store}); err != nil {
				return err
			}
			if err := extension.Add[coretool.Tool](scope, &retestTool{store: c.store}); err != nil {
				return err
			}
			registry, err := extension.Use[*corehooks.Registry](scope)
			if err != nil {
				return err
			}
			subscriptions = append(subscriptions, agenthooks.ModelRequestPolicy.On(registry, "wscyber-webapi-unresolved", func(ctx context.Context, event agenthooks.ModelRequestEvent) (agenthooks.ModelPolicy, error) {
				blocked, err := c.store.unresolvedTurn(ctx, event.SessionID, event.TurnID)
				if err != nil {
					return agenthooks.ModelPolicy{Deny: errors.New("无法核对当前turn未决HTTP，停止下一次模型调用")}, nil
				}
				if blocked {
					return agenthooks.ModelPolicy{Deny: errors.New("当前turn已有claimed/unknown/capture_failed/review_required HTTP；先核对，不继续模型调用")}, nil
				}
				return agenthooks.ModelPolicy{}, nil
			}))
			return nil
		},
		CloseFunc: func(ctx context.Context) error {
			var result error
			for _, subscription := range subscriptions {
				result = errors.Join(result, subscription.Close(ctx))
			}
			return result
		},
	}
}

func (*httpTool) Name() string { return "webapi_http" }
func (*httpTool) Description() string {
	return "Send an actual HTTP(S) request within the current saved task's authorized origins and retain bounded request/response evidence. Never use it for unauthorized hosts."
}
func (*httpTool) Definition() *aop.ToolDefinition {
	return coretool.Def("webapi_http", "在已保存任务的明确origin授权内发送一次具体HTTP请求；禁止自报task/session/授权，结果提供有界预览与证据ID。", httpToolArguments{})
}

func buildHTTPRequest(method, rawURL string, headers map[string]string, body []byte) (*http.Request, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" || strings.ContainsAny(method, " \t\r\n") {
		return nil, errors.New("method 非法")
	}
	if len(body) > maxRequestBody {
		return nil, errors.New("body 超过1MiB")
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Fragment != "" {
		return nil, errors.New("url 必须是无userinfo/fragment的绝对HTTP(S) URL")
	}
	origin, err := normalizeOrigin(u.Scheme + "://" + u.Host)
	if err != nil {
		return nil, fmt.Errorf("url origin非法: %w", err)
	}
	u.Host = strings.TrimPrefix(origin, u.Scheme+"://")
	// 大小写scheme经url.Parse会丢RawPath，先单独恢复保留原转义的路径。
	original := strings.TrimSpace(rawURL)
	pathStart := strings.Index(original, "://") + 3
	if pathStart < 3 {
		return nil, errors.New("url缺少scheme")
	}
	if index := strings.IndexAny(original[pathStart:], "/?#"); index >= 0 {
		pathStart += index
	} else {
		pathStart = len(original)
	}
	pathEnd := len(original)
	if index := strings.IndexAny(original[pathStart:], "?#"); index >= 0 {
		pathEnd = pathStart + index
	}
	rawPath := original[pathStart:pathEnd]
	if rawPath == "" {
		rawPath = "/"
	}
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return nil, errors.New("url path转义非法")
	}
	u.Path, u.RawPath = decodedPath, rawPath
	req, err := http.NewRequest(method, u.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Host = u.Host // 与URL authority完全相同，仅让Flow保留实际Host。
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "wscyber-webapi/1")
	}
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
	// URL authority 是唯一Host来源；请求头不能改Host或传输跳转策略。
	for key, value := range headers {
		canonical := http.CanonicalHeaderKey(key)
		if strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") || strings.EqualFold(key, "Connection") || strings.EqualFold(key, "Proxy-Authorization") {
			return nil, fmt.Errorf("header %s 不允许覆盖传输目标/ framing", key)
		}
		if httpgutsValidHeader(key, value) {
			req.Header.Set(canonical, value)
		} else {
			return nil, fmt.Errorf("header %s 非法", key)
		}
	}
	return req, nil
}

func httpgutsValidHeader(name, value string) bool {
	return name != "" && !strings.ContainsAny(name, "()<>@,;:\\\"/[]?={} \t\r\n") && !strings.ContainsAny(value, "\r\n") && !strings.ContainsFunc(name, func(r rune) bool { return r < 0x21 || r > 0x7e })
}

func fingerprintRequest(req *http.Request, body []byte) string {
	encoded, err := json.Marshal(struct {
		Method  string              `json:"method"`
		URL     string              `json:"url"`
		Headers map[string][]string `json:"headers"`
		Body    []byte              `json:"body"`
	}{Method: req.Method, URL: req.URL.String(), Headers: canonicalHeaders(req.Header), Body: body})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func canonicalHeaders(header http.Header) map[string][]string {
	result := make(map[string][]string, len(header))
	for name, values := range header {
		result[strings.ToLower(name)] = append([]string(nil), values...)
	}
	return result
}

func originAllowed(rawURL string, origins []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	origin, err := normalizeOrigin(u.Scheme + "://" + u.Host)
	if err != nil {
		return false
	}
	for _, allowed := range origins {
		if origin == allowed {
			return true
		}
	}
	return false
}

var directTransport = &http.Transport{
	Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false, DisableCompression: true,
	DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
}

var directClient = &http.Client{
	Transport:     directTransport,
	Timeout:       httpTimeout,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

func (t *httpTool) Execute(ctx context.Context, arguments string) (*aop.ToolResult, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.SessionID == "" || inv.CallID == "" || inv.TurnID == "" {
		return stopTool("缺少可信原生 session/turn/call 上下文；0发送"), nil
	}
	args, err := coretool.ParseArgs[httpToolArguments](arguments)
	if err != nil {
		return stopTool("参数不是合法请求JSON；0发送"), nil
	}
	// task/session/origins/approved即使伪造在JSON里也不进入ParseArgs结构或授权。
	task, err := t.store.taskForSession(ctx, inv.SessionID)
	if err != nil || task == nil {
		return stopTool("session无已保存任务授权；0发送"), nil
	}
	body := []byte(args.Body)
	req, err := buildHTTPRequest(args.Method, args.URL, args.Headers, body)
	if err != nil {
		return stopTool(err.Error() + "；0发送"), nil
	}
	if !originAllowed(req.URL.String(), task.Origins) {
		return t.recordUnsent(ctx, inv, task.ID, req, body, "denied", "实际origin不在持久授权内")
	}
	if reason := requestReview(req.Method, req.URL.String(), args.Headers, body); reason != "" {
		return t.recordUnsent(ctx, inv, task.ID, req, body, "review_required", requestPolicyVersion+"："+reason)
	}
	id := newRequestID()
	flow := traffic.FlowFromHTTP(req, nil, body, nil)
	flow.Id, flow.Timestamp = id, nowTimestamp()
	spec := requestSpec{method: req.Method, url: req.URL.String(), fingerprint: fingerprintRequest(req, body), body: body, flow: flow}
	claimed, fresh, err := t.store.claim(ctx, inv, task.ID, spec, "claimed", "发送前已认领")
	if err != nil {
		if claimed != nil {
			return stopTool(formatEvidence(claimed, err.Error())), nil
		}
		return stopTool("发送前认领失败，0发送: " + publicStoreError(err)), nil
	}
	if !fresh {
		if claimed.Status == requestCompleted {
			return coretool.TextResult(formatEvidence(claimed, "同调用已完成，返回已保存结果；未再次发送")), nil
		}
		return stopTool(formatEvidence(claimed, "同调用已有未解决结果，不再次发送")), nil
	}

	resp, sendErr := directClient.Do(req.WithContext(ctx))
	if sendErr != nil {
		return t.finishUnknown(ctx, inv, task.ID, claimed, flow, "发送或接收失败，目标结果未知；不自动重试", nil)
	}
	defer resp.Body.Close()
	captured, readErr := io.ReadAll(io.LimitReader(resp.Body, maxCaptureBytes+1))
	captureTruncated := len(captured) > maxCaptureBytes
	if captureTruncated {
		captured = captured[:maxCaptureBytes]
	}
	flow = traffic.FlowFromHTTP(req, resp, body, captured)
	flow.Id, flow.Timestamp = id, nowTimestamp()
	flow.Complete = !captureTruncated && readErr == nil
	statusCode := resp.StatusCode
	if readErr != nil {
		flow.Complete = false
		return t.finishUnknown(ctx, inv, task.ID, claimed, flow, "响应体读取失败，目标结果未知；不自动重试", &statusCode)
	}
	preview, previewTruncated := responsePreview(captured, resp.Header.Get("Content-Type"))
	model, err := t.store.finish(ctx, claimed.ID, requestCompleted, "已收到并保存有界响应", preview, previewTruncated, captureTruncated, &statusCode, flow)
	if err != nil {
		return t.saveFailure(ctx, claimed, flow, &statusCode, preview, previewTruncated, captureTruncated)
	}
	return coretool.TextResult(formatEvidence(model, "HTTP请求完成；仅代表目标响应，不是漏洞结论")), nil
}

func responsePreview(captured []byte, contentType string) ([]byte, bool) {
	textual := utf8.Valid(captured) && (strings.HasPrefix(strings.ToLower(contentType), "text/") || strings.Contains(strings.ToLower(contentType), "json") || contentType == "")
	if !textual {
		const prefix = "base64:"
		// 保留一个原字节余量，使base64长度恰为4的倍数且含前缀不超4KiB。
		limit := (maxPreviewBytes-len(prefix))/4*3 - 1
		truncated := len(captured) > limit
		if truncated {
			captured = captured[:limit]
		}
		return []byte(prefix + base64.StdEncoding.EncodeToString(captured)), truncated
	}
	truncated := len(captured) > maxPreviewBytes
	if truncated {
		captured = captured[:maxPreviewBytes]
		for len(captured) > 0 && !utf8.Valid(captured) {
			captured = captured[:len(captured)-1]
		}
	}
	return captured, truncated
}

// 发送取消/进程中断留下的 claimed 也永不自动重发，无需借新CallID规避。

func nowTimestamp() *timestamppb.Timestamp { return timestamppb.Now() }

func stopTool(message string) *aop.ToolResult {
	result := coretool.ErrorResult(message)
	result.Terminate = true // StandardLoop实测支持：停止当前自动回合，不造暂停调度器。
	return result
}

func publicStoreError(err error) string {
	switch {
	case errors.Is(err, errUnknownSession), errors.Is(err, errCallConflict), errors.Is(err, errUnknownReplay):
		return err.Error()
	default:
		return "证据存储操作失败"
	}
}

func (t *httpTool) recordUnsent(ctx context.Context, inv operation.Invocation, taskID string, req *http.Request, body []byte, status, reason string) (*aop.ToolResult, error) {
	id := newRequestID()
	flow := traffic.FlowFromHTTP(req, nil, body, nil)
	flow.Id, flow.Timestamp = id, nowTimestamp()
	model, fresh, err := t.store.claim(ctx, inv, taskID, requestSpec{method: req.Method, url: req.URL.String(), fingerprint: fingerprintRequest(req, body), body: body, flow: flow}, status, reason)
	if err != nil && model == nil {
		return stopTool("保存拒绝/待审记录失败；0发送"), nil
	}
	if !fresh && model != nil {
		reason = "已有同调用记录；0发送"
	}
	return stopTool(formatEvidence(model, reason+"；0发送")), nil
}

func (t *httpTool) finishUnknown(ctx context.Context, inv operation.Invocation, taskID string, claimed *requestModel, flow *traffic.Flow, reason string, statusCode *int) (*aop.ToolResult, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	model, err := t.store.finish(cleanup, claimed.ID, requestUnknown, reason, []byte("目标响应未知"), false, false, statusCode, flow)
	if err != nil {
		return t.saveFailure(ctx, claimed, flow, statusCode, nil, false, false)
	}
	return stopTool(formatEvidence(model, reason)), nil
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func (t *httpTool) saveFailure(ctx context.Context, claimed *requestModel, flow *traffic.Flow, statusCode *int, preview []byte, previewTruncated, captureTruncated bool) (*aop.ToolResult, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	model, err := t.store.finish(cleanup, claimed.ID, requestCaptureFailed, "已发送但结果保存失败；不自动重试", preview, previewTruncated, captureTruncated, statusCode, flow)
	if err != nil {
		return stopTool("证据ID " + claimed.ID + "：已发送但capture_failed保存也失败；保持未知，不自动重试"), nil
	}
	return stopTool(formatEvidence(model, "已发送但结果保存失败；不自动重试")), nil
}

func formatEvidence(model *requestModel, note string) string {
	if model == nil {
		return note
	}
	value := map[string]any{
		"evidence_id": model.ID, "task_id": model.TaskID, "session_id": model.SessionID,
		"turn_id": model.TurnID, "call_id": model.CallID, "status": model.Status,
		"sent": nullableBool(model.Sent), "preview": string(model.Preview), "preview_truncated": model.PreviewTruncated,
		"capture_truncated": model.CaptureTruncated, "note": note,
	}
	if model.StatusCode.Valid {
		value["http_status"] = model.StatusCode.Int64
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func nullableBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}

// Evidence 是列表/工具摘要共用的元数据；不含Flow protobuf原件。
type Evidence struct {
	ID               string `json:"evidence_id"`
	TaskID           string `json:"task_id"`
	SessionID        string `json:"session_id"`
	TurnID           string `json:"turn_id"`
	CallID           string `json:"call_id"`
	Fingerprint      string `json:"request_fingerprint"`
	Method           string `json:"method"`
	URL              string `json:"url"`
	Status           string `json:"status"`
	Reason           string `json:"reason"`
	Sent             *bool  `json:"sent"`
	HTTPStatus       *int64 `json:"http_status,omitempty"`
	PreviewTruncated bool   `json:"preview_truncated"`
	CaptureTruncated bool   `json:"capture_truncated"`
	CreatedAt        string `json:"created_at"`
	CompletedAt      string `json:"completed_at,omitempty"`
}

func evidenceMetadata(model requestModel) Evidence {
	e := Evidence{ID: model.ID, TaskID: model.TaskID, SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID,
		Fingerprint: model.Fingerprint, Method: model.Method, URL: model.URL, Status: model.Status, Reason: model.Reason,
		Sent: nullableBool(model.Sent), PreviewTruncated: model.PreviewTruncated, CaptureTruncated: model.CaptureTruncated, CreatedAt: model.CreatedAt, CompletedAt: model.CompletedAt.String}
	if model.StatusCode.Valid {
		e.HTTPStatus = &model.StatusCode.Int64
	}
	return e
}

func (c *Capability) handleEvidenceList(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			writeJSON(w, 400, map[string]string{"error": "limit须为1至200"})
			return
		}
	}
	afterCreatedAt, afterID := "", ""
	if encoded := r.URL.Query().Get("cursor"); encoded != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
		parts := strings.Split(string(decoded), "\n")
		if decodeErr != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			writeJSON(w, 400, map[string]string{"error": "cursor非法"})
			return
		}
		afterCreatedAt, afterID = parts[0], parts[1]
	}
	models, err := c.store.evidenceList(r.Context(), taskID, limit, afterCreatedAt, afterID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取证据元数据失败"})
		return
	}
	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit]
	}
	items := make([]Evidence, 0, len(models))
	for _, model := range models {
		items = append(items, evidenceMetadata(model))
	}
	response := map[string]any{"task_id": taskID, "items": items, "limit": limit, "has_more": hasMore, "next_cursor": nil}
	if hasMore {
		last := models[len(models)-1]
		response["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt + "\n" + last.ID))
	}
	writeJSON(w, 200, response)
}

func (c *Capability) handleEvidenceGet(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	model, err := c.store.evidenceByID(r.Context(), taskID, r.PathValue("evidence_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取证据失败"})
		return
	}
	if model == nil {
		writeJSON(w, 404, map[string]string{"error": "任务内证据不存在"})
		return
	}
	var flow traffic.Flow
	if err := proto.Unmarshal(model.FlowPB, &flow); err != nil {
		writeJSON(w, 500, map[string]string{"error": "Flow原件损坏"})
		return
	}
	flowJSON, err := protojson.Marshal(&flow)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Flow protojson编码失败"})
		return
	}
	writeJSON(w, 200, map[string]any{
		"metadata": evidenceMetadata(*model), "preview": string(model.Preview),
		"flow_protobuf_base64": base64Encode(model.FlowPB),
		"flow":                 json.RawMessage(flowJSON),
		"capture":              map[string]any{"request": "结构化重建，含完整请求body；非逐字节线上报文", "response_body_limit": maxCaptureBytes, "preview_limit": maxPreviewBytes, "capture_truncated": model.CaptureTruncated, "preview_truncated": model.PreviewTruncated, "auto_decompressed": false},
	})
}

func base64Encode(value []byte) string { return base64.StdEncoding.EncodeToString(value) }
