package webapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/uptrace/bun"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	egressAllowed = "allowed"
	egressDenied  = "denied"
)

// egressFlowModel 是终端出口的被动记录：一次调用可有多条；不参与 claimed/unknown/防重放语义。
type egressFlowModel struct {
	bun.BaseModel `bun:"table:webapi_egress_flows,alias:webapi_egress_flow"`

	ID               string         `bun:"id,pk"`
	TaskID           string         `bun:"task_id,notnull"`
	SessionID        string         `bun:"session_id,notnull"`
	TurnID           string         `bun:"turn_id,notnull"`
	CallID           string         `bun:"call_id,notnull"`
	Tool             string         `bun:"tool,notnull"`
	Method           string         `bun:"method,notnull"`
	URL              string         `bun:"url,notnull"`
	Host             string         `bun:"host,notnull"`
	Decision         string         `bun:"decision,notnull"`
	Sent             sql.NullBool   `bun:"sent"`
	HTTPStatus       sql.NullInt64  `bun:"http_status"`
	Error            string         `bun:"error,type:text,notnull"`
	FlowPB           []byte         `bun:"flow_pb,type:bytea,notnull"`
	RequestBytes     int64          `bun:"request_bytes,notnull"`
	ResponseBytes    int64          `bun:"response_bytes,notnull"`
	PreviewTruncated bool           `bun:"preview_truncated,notnull"`
	CreatedAt        string         `bun:"created_at,notnull"`
	CompletedAt      sql.NullString `bun:"completed_at"`
}

func newEgressFlowID() string { return "wx_" + newSessionID() + newSessionID() }

// egressTaskForSession：任务首会话 / 验证 / 复测（taskForSession）之外，再认报告会话；
// 各自都按所属任务的策略与 origins。
func (s *taskStore) egressTaskForSession(ctx context.Context, sessionID string) (*Task, error) {
	if sessionID == "" {
		return nil, nil
	}
	task, err := s.taskForSession(ctx, sessionID)
	if err != nil || task != nil {
		return task, err
	}
	report, err := s.reportForSession(ctx, sessionID)
	if err != nil || report == nil {
		return nil, err
	}
	return s.Get(ctx, report.TaskID)
}

// insertEgressFlow 序列化方式与 webapi_requests.flow_pb 相同（proto.Marshal(traffic.Flow)）。
func (s *taskStore) insertEgressFlow(ctx context.Context, model *egressFlowModel, flow *traffic.Flow) error {
	flowPB, err := proto.Marshal(flow)
	if err != nil {
		return err
	}
	if flowPB == nil {
		flowPB = []byte{}
	}
	model.ID, model.FlowPB = newEgressFlowID(), flowPB
	err = s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(model).Exec(ctx)
		return err
	})
	return err
}

var egressSummaryColumns = []string{"id", "task_id", "session_id", "turn_id", "call_id", "tool", "method", "url", "host", "decision", "sent", "http_status", "error", "request_bytes", "response_bytes", "preview_truncated", "created_at", "completed_at"}

func (s *taskStore) egressFlowList(ctx context.Context, taskID, sessionID, decision string, limit int, afterCreatedAt, afterID string) ([]egressFlowModel, error) {
	var models []egressFlowModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		query := db.NewSelect().Model(&models).Column(egressSummaryColumns...).Where("task_id = ?", taskID)
		if sessionID != "" {
			query = query.Where("session_id = ?", sessionID)
		}
		if decision != "" {
			query = query.Where("decision = ?", decision)
		}
		if afterCreatedAt != "" {
			query = query.Where(`(created_at COLLATE "C" < ? OR (created_at = ? AND id COLLATE "C" < ?))`, afterCreatedAt, afterCreatedAt, afterID)
		}
		return query.OrderExpr(`created_at COLLATE "C" DESC, id COLLATE "C" DESC`).Limit(int64(limit + 1)).Scan(ctx)
	})
	return models, err
}

func (s *taskStore) egressFlowByID(ctx context.Context, taskID, flowID string) (*egressFlowModel, error) {
	return readOptional[egressFlowModel](ctx, s, "task_id = ? AND id = ?", taskID, flowID)
}

// EgressFlowSummary 是列表与详情共用的摘要。
type EgressFlowSummary struct {
	ID          string  `json:"id"`
	SessionID   string  `json:"session_id"`
	TurnID      string  `json:"turn_id"`
	CallID      string  `json:"call_id"`
	Tool        string  `json:"tool"`
	Method      string  `json:"method"`
	URL         string  `json:"url"`
	Decision    string  `json:"decision"`
	Sent        *bool   `json:"sent"`
	HTTPStatus  *int64  `json:"http_status"`
	Error       string  `json:"error"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`
}

func egressFlowSummary(model egressFlowModel) EgressFlowSummary {
	summary := EgressFlowSummary{ID: model.ID, SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID, Tool: model.Tool,
		Method: model.Method, URL: model.URL, Decision: model.Decision, Sent: nullableBool(model.Sent), Error: model.Error, CreatedAt: model.CreatedAt}
	if model.HTTPStatus.Valid {
		status := model.HTTPStatus.Int64
		summary.HTTPStatus = &status
	}
	if model.CompletedAt.Valid {
		completed := model.CompletedAt.String
		summary.CompletedAt = &completed
	}
	return summary
}

// flowDetailJSON 是 webapi_requests 详情与出口详情共用的 Flow 展开（protojson）。
func flowDetailJSON(flowPB []byte) (json.RawMessage, error) {
	var flow traffic.Flow
	if err := proto.Unmarshal(flowPB, &flow); err != nil {
		return nil, errors.New("Flow原件损坏")
	}
	raw, err := protojson.Marshal(&flow)
	if err != nil {
		return nil, errors.New("Flow protojson编码失败")
	}
	return raw, nil
}

func (c *Capability) egressTaskFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return "", false
	}
	task, err := c.store.Get(r.Context(), taskID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取任务失败"})
		return "", false
	}
	if task == nil {
		writeJSON(w, 404, map[string]string{"error": "任务不存在"})
		return "", false
	}
	return taskID, true
}

// handleEgressList：GET /api/webapi/tasks/{id}/egress?session_id=&decision=&limit=&cursor=，按时间倒序。
func (c *Capability) handleEgressList(w http.ResponseWriter, r *http.Request) {
	taskID, ok := c.egressTaskFromPath(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	limit := 50
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeJSON(w, 400, map[string]string{"error": "limit须为1至200"})
			return
		}
		limit = parsed
	}
	decision := query.Get("decision")
	if decision != "" && decision != egressAllowed && decision != egressDenied {
		writeJSON(w, 400, map[string]string{"error": "decision只能是allowed或denied"})
		return
	}
	sessionID := query.Get("session_id")
	if len(sessionID) > 256 {
		writeJSON(w, 400, map[string]string{"error": "session_id非法"})
		return
	}
	afterCreatedAt, afterID := "", ""
	if encoded := query.Get("cursor"); encoded != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		parts := strings.Split(string(decoded), "\n")
		if err != nil || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			writeJSON(w, 400, map[string]string{"error": "cursor非法"})
			return
		}
		afterCreatedAt, afterID = parts[0], parts[1]
	}
	models, err := c.store.egressFlowList(r.Context(), taskID, sessionID, decision, limit, afterCreatedAt, afterID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取出口记录失败"})
		return
	}
	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit]
	}
	items := make([]EgressFlowSummary, 0, len(models))
	for _, model := range models {
		items = append(items, egressFlowSummary(model))
	}
	response := map[string]any{"items": items, "has_more": hasMore, "next_cursor": nil}
	if hasMore {
		last := models[len(models)-1]
		response["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt + "\n" + last.ID))
	}
	writeJSON(w, 200, response)
}

// handleEgressGet：GET /api/webapi/tasks/{id}/egress/{flow_id} → 摘要 + Flow（请求/响应头与 body 预览）。
func (c *Capability) handleEgressGet(w http.ResponseWriter, r *http.Request) {
	taskID, ok := c.egressTaskFromPath(w, r)
	if !ok {
		return
	}
	model, err := c.store.egressFlowByID(r.Context(), taskID, r.PathValue("flow_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取出口记录失败"})
		return
	}
	if model == nil {
		writeJSON(w, 404, map[string]string{"error": "任务内出口记录不存在"})
		return
	}
	flowJSON, err := flowDetailJSON(model.FlowPB)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	capture := map[string]any{
		"request_bytes": model.RequestBytes, "response_bytes": model.ResponseBytes,
		"preview_limit": egressPreviewLimit, "preview_truncated": model.PreviewTruncated,
		"note": "经终端出口代理观察到的请求/响应；body 只保留前 4096 字节预览，request_bytes/response_bytes 为观察到的原始字节数",
	}
	if model.Decision == egressDenied {
		capture["note"] = "代理在连接目标之前拒绝，未发送；只有请求行与请求头，没有 body 与响应"
	}
	writeJSON(w, 200, map[string]any{
		"summary": egressFlowSummary(*model), "host": model.Host,
		"flow": flowJSON, "flow_protobuf_base64": base64Encode(model.FlowPB), "capture": capture,
	})
}

// egressPreviewLimit 是上游 ProxyHub 未落盘模式下每侧 body 预览上限（固定上游 maxBodySnip）。
const egressPreviewLimit = 4096
