package webapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// 黑板 HTTP（原生鉴权内，/api/webapi/tasks/{id}/board…）：读取 + 用户提出/调整 Oracle。契约见 native/BOARD.md。

// boardTaskFromPath 校验任务存在并摄取新流量；失败时已写错误响应。
func (c *Capability) boardTaskFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	task, err := c.store.Get(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return "", false
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "任务不存在")
		return "", false
	}
	if err := c.store.boardIngest(r.Context(), taskID, c.scheduleClock()); err != nil {
		writeError(w, http.StatusInternalServerError, "摄取流量到黑板地图失败")
		return "", false
	}
	return taskID, true
}

// handleBoardGet: GET /tasks/{id}/board → {digest, counts}
func (c *Capability) handleBoardGet(w http.ResponseWriter, r *http.Request) {
	taskID, ok := c.boardTaskFromPath(w, r)
	if !ok {
		return
	}
	digest, err := c.store.boardDigest(r.Context(), taskID, boardDigestDefaultChars)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成黑板摘要失败")
		return
	}
	counts, err := c.store.boardCounts(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑板计数失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "digest": digest, "counts": counts})
}

func boardListPage[T any](w http.ResponseWriter, items []T, limit int, cursor func(T) string, extra map[string]any) {
	var next any
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
		next = cursor(items[len(items)-1])
	}
	body := map[string]any{"items": items, "has_more": hasMore, "next_cursor": next}
	for key, value := range extra {
		body[key] = value
	}
	writeJSON(w, http.StatusOK, body)
}

// handleBoardEndpoints: GET /tasks/{id}/board/endpoints?q=&untested=1&limit=&cursor=（按 endpoint_key 升序）
func (c *Capability) handleBoardEndpoints(w http.ResponseWriter, r *http.Request) {
	limit, needle, _, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := r.URL.Query()
	untested := query.Get("untested")
	if untested != "" && untested != "0" && untested != "1" {
		writeError(w, http.StatusBadRequest, "untested 只能是 0 或 1")
		return
	}
	afterKey := ""
	if raw := query.Get("cursor"); raw != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(raw)
		if decodeErr != nil || len(decoded) == 0 {
			writeError(w, http.StatusBadRequest, "cursor非法")
			return
		}
		afterKey = string(decoded)
	}
	taskID, ok := c.boardTaskFromPath(w, r)
	if !ok {
		return
	}
	items, err := c.store.boardEndpoints(r.Context(), taskID, boardEndpointFilter{needle: needle, untested: untested == "1", afterKey: afterKey, limit: limit + 1})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑板端点失败")
		return
	}
	boardListPage(w, items, limit, func(item BoardEndpoint) string {
		return base64.RawURLEncoding.EncodeToString([]byte(item.EndpointKey))
	}, nil)
}

// handleBoardFacts: GET /tasks/{id}/board/facts?kind=&status=&limit=&cursor=（按 created_at,id 倒序）
func (c *Capability) handleBoardFacts(w http.ResponseWriter, r *http.Request) {
	limit, _, _, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := r.URL.Query()
	filter := boardFactFilter{kind: query.Get("kind"), status: query.Get("status")}
	if filter.kind != "" && !slices.Contains(boardFactKinds, filter.kind) {
		writeError(w, http.StatusBadRequest, "kind 只能是 "+strings.Join(boardFactKinds, "、"))
		return
	}
	if filter.status != "" && filter.status != factActive && filter.status != factRetracted {
		writeError(w, http.StatusBadRequest, "status 只能是 active 或 retracted")
		return
	}
	afterAt, afterID, hasCursor, err := decodeKeyCursor(query.Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if hasCursor {
		filter.afterAt, filter.afterID = notifyTime(afterAt), afterID
	}
	taskID, ok := c.boardTaskFromPath(w, r)
	if !ok {
		return
	}
	filter.limit = limit + 1
	items, err := c.store.boardFacts(r.Context(), taskID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑板事实失败")
		return
	}
	boardListPage(w, items, limit, func(item BoardFact) string { return encodeKeyCursor(item.CreatedAt, item.ID) }, nil)
}

// handleBoardOracles: GET /tasks/{id}/board/oracles?status=&category=&limit=&cursor=（按编号升序，项含 events）
func (c *Capability) handleBoardOracles(w http.ResponseWriter, r *http.Request) {
	limit, _, _, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := r.URL.Query()
	filter := boardOracleFilter{status: query.Get("status"), category: query.Get("category")}
	if filter.status != "" && !slices.Contains(boardOracleStatuses, filter.status) {
		writeError(w, http.StatusBadRequest, "status 只能是 "+strings.Join(boardOracleStatuses, "、"))
		return
	}
	if filter.category != "" && !slices.Contains(boardOracleCategory, filter.category) {
		writeError(w, http.StatusBadRequest, "category 只能是 "+strings.Join(boardOracleCategory, "、"))
		return
	}
	if raw := query.Get("cursor"); raw != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(raw)
		value, convErr := strconv.ParseInt(string(decoded), 10, 64)
		if decodeErr != nil || convErr != nil || value < 1 {
			writeError(w, http.StatusBadRequest, "cursor非法")
			return
		}
		filter.afterNumber = value
	}
	taskID, ok := c.boardTaskFromPath(w, r)
	if !ok {
		return
	}
	filter.limit = limit + 1
	items, err := c.store.boardOracles(r.Context(), taskID, filter, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取黑板 Oracle 失败")
		return
	}
	boardListPage(w, items, limit, func(item BoardOracle) string {
		number, _ := parseOracleNumber(item.Number)
		return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(number, 10)))
	}, nil)
}

// handleBoardOracleCreate: POST /tasks/{id}/board/oracles，body 同 propose 单项；origin=user。
// 新建 201 {oracle, created:true}；去重命中 200 {oracle, created:false}。
func (c *Capability) handleBoardOracleCreate(w http.ResponseWriter, r *http.Request) {
	if _, err := taskIDFromPath(r); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body boardOracleInput
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	proposal, err := validateOracleInput("body", body)
	if err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "body."))
		return
	}
	taskID, ok := c.boardTaskFromPath(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	results, err := c.store.boardProposeOracles(ctx, taskID, auditActorUser, "", []boardOracleProposal{proposal}, c.scheduleClock())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Oracle 失败")
		return
	}
	number, _ := parseOracleNumber(results[0].Number)
	oracle, err := c.store.boardOracle(ctx, taskID, number)
	if err != nil || oracle == nil {
		writeError(w, http.StatusInternalServerError, "读取 Oracle 失败")
		return
	}
	status := http.StatusOK
	if results[0].Created {
		status = http.StatusCreated
		c.auditBoardOracle(ctx, taskID, "board.oracle.create", oracle, "提出 Oracle "+oracle.Number+"："+oracle.Question,
			map[string]any{"number": oracle.Number, "category": oracle.Category, "priority": oracle.Priority, "endpoint_keys": oracle.EndpointKeys})
	}
	writeJSON(w, status, map[string]any{"oracle": oracle, "created": results[0].Created})
}

// handleBoardOracleUpdate: PUT /tasks/{id}/board/oracles/{number}，body {priority?, status?: open|dropped}。
func (c *Capability) handleBoardOracleUpdate(w http.ResponseWriter, r *http.Request) {
	if _, err := taskIDFromPath(r); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	number, ok := parseOracleNumber(r.PathValue("number"))
	if !ok {
		writeError(w, http.StatusBadRequest, "Oracle 编号非法（如 O-001）")
		return
	}
	var body struct {
		Priority *string `json:"priority"`
		Status   *string `json:"status"`
	}
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Priority == nil && body.Status == nil {
		writeError(w, http.StatusBadRequest, "priority 与 status 至少提供一项")
		return
	}
	if body.Priority != nil && !slices.Contains(boardOraclePriority, *body.Priority) {
		writeError(w, http.StatusBadRequest, "priority 只能是 high、medium、low")
		return
	}
	if body.Status != nil && *body.Status != oracleOpen && *body.Status != oracleDropped {
		writeError(w, http.StatusBadRequest, "status 只能是 open（重开）或 dropped（放弃）")
		return
	}
	taskID, ok := c.boardTaskFromPath(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	change := boardOracleChange{actor: auditActorUser, apply: func(model *boardOracleModel) (string, error) {
		if body.Priority != nil {
			model.Priority = *body.Priority
		}
		if body.Status == nil || *body.Status == model.Status {
			return "", nil
		}
		previous := model.Status
		model.Status = *body.Status
		if model.Status == oracleDropped {
			return "用户放弃", nil
		}
		summary := "用户重开（原状态 " + previous + "）"
		if model.ResultSummary != "" {
			summary += "；原结论：" + model.ResultSummary
		}
		model.ClaimedSessionID, model.ResultSummary, model.EvidenceIDsJSON, model.FindingID = "", "", "[]", ""
		return summary, nil
	}}
	update, err := c.store.boardChangeOracle(ctx, taskID, number, change, c.scheduleClock())
	if errors.Is(err, errBoardOracleAbsent) {
		writeError(w, http.StatusNotFound, "Oracle 不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Oracle 失败")
		return
	}
	oracle, err := c.store.boardOracle(ctx, taskID, number)
	if err != nil || oracle == nil {
		writeError(w, http.StatusInternalServerError, "读取 Oracle 失败")
		return
	}
	if update.changed {
		changes := map[string]any{}
		parts := []string{}
		if update.before.Priority != update.after.Priority {
			changes["priority"] = map[string]string{"from": update.before.Priority, "to": update.after.Priority}
			parts = append(parts, "优先级 "+update.before.Priority+" → "+update.after.Priority)
		}
		if update.before.Status != update.after.Status {
			changes["status"] = map[string]string{"from": update.before.Status, "to": update.after.Status}
			parts = append(parts, "状态 "+update.before.Status+" → "+update.after.Status)
		}
		c.auditBoardOracle(ctx, taskID, "board.oracle.update", oracle, fmt.Sprintf("调整 Oracle %s：%s", oracle.Number, strings.Join(parts, "；")),
			map[string]any{"number": oracle.Number, "changes": changes})
	}
	writeJSON(w, http.StatusOK, map[string]any{"oracle": oracle, "changed": update.changed})
}

func (c *Capability) auditBoardOracle(ctx context.Context, taskID, action string, oracle *BoardOracle, summary string, detail map[string]any) {
	assetID, _ := c.store.taskAssetID(ctx, taskID)
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: action, TargetKind: "board_oracle", TargetID: oracle.ID, TaskID: taskID, AssetID: assetID,
		Summary: summary, Detail: detail})
}
