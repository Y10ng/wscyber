package webapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// 复测HTTP语义逐条对齐验证（verification_http.go）；body解码直接复用decodeVerificationRequest。

func retestIDFromPath(r *http.Request) (string, error) {
	id := r.PathValue("retest_id")
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\?#%") || strings.ContainsFunc(id, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		return "", errors.New("retest id非法")
	}
	return id, nil
}

func (c *Capability) handleRetestPut(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	id, err := retestIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	sourceRevisionID, err := decodeVerificationRequest(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	created, isNew, conflict, err := c.store.saveRetest(r.Context(), id, taskID, r.PathValue("finding_id"), sourceRevisionID)
	if err != nil {
		status := 500
		message := "保存复测失败"
		if errors.Is(err, errFindingNotFound) {
			status, message = 404, "任务内候选不存在"
		} else if errors.Is(err, errRetestStale) {
			status, message = 409, errRetestStale.Error()
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	if conflict {
		writeJSON(w, 409, map[string]string{"error": errRetestConflict.Error()})
		return
	}
	status := 200
	if isNew {
		status = 201
		c.auditSessionStart(r.Context(), "retest", taskID, created.FindingID, created.RetestID, created.SessionID)
	}
	writeJSON(w, status, map[string]any{"retest": created, "created": isNew, "idempotent": !isNew})
}

func (c *Capability) retestDetailResponse(w http.ResponseWriter, r *http.Request, model *retestModel) {
	detail, err := c.store.retestDetail(r.Context(), model)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取复测失败"})
		return
	}
	if detail == nil {
		writeJSON(w, 404, map[string]string{"error": "复测不存在"})
		return
	}
	writeJSON(w, 200, detail)
}

func (c *Capability) handleRetestGet(w http.ResponseWriter, r *http.Request) {
	model, err := c.store.retestByID(r.Context(), r.PathValue("id"), r.PathValue("finding_id"), r.PathValue("retest_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取复测失败"})
		return
	}
	c.retestDetailResponse(w, r, model)
}

func (c *Capability) handleRetestBySession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, 400, map[string]string{"error": "session_id不能为空"})
		return
	}
	model, err := c.store.retestForSession(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取复测失败"})
		return
	}
	c.retestDetailResponse(w, r, model)
}

func (c *Capability) handleRetestList(w http.ResponseWriter, r *http.Request) {
	taskID, findingID := r.PathValue("id"), r.PathValue("finding_id")
	limit := 25
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 50 {
			writeJSON(w, 400, map[string]string{"error": "limit须为1至50"})
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
	exists, err := c.store.findingExists(r.Context(), taskID, findingID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取候选失败"})
		return
	}
	if !exists {
		writeJSON(w, 404, map[string]string{"error": "任务内候选不存在"})
		return
	}
	items, err := c.store.retestList(r.Context(), taskID, findingID, limit, afterCreatedAt, afterID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取复测列表失败"})
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next any
	if hasMore {
		last := items[len(items)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt + "\n" + last.RetestID))
	}
	writeJSON(w, 200, findingCollection(taskID, limit, items, hasMore, next))
}
