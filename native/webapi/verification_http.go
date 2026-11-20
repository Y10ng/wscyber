package webapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type saveVerificationRequest struct {
	SourceRevisionID string `json:"source_revision_id"`
}

func decodeVerificationRequest(r *http.Request) (string, error) {
	defer r.Body.Close()
	var body saveVerificationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return "", errors.New("请求体不是合法JSON或含未知字段")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errors.New("请求体只能包含一个JSON对象")
	}
	if body.SourceRevisionID == "" || len(body.SourceRevisionID) > 128 {
		return "", errors.New("source_revision_id不能为空")
	}
	return body.SourceRevisionID, nil
}

func verificationIDFromPath(r *http.Request) (string, error) {
	id := r.PathValue("verification_id")
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\?#%") || strings.ContainsFunc(id, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		return "", errors.New("verification id非法")
	}
	return id, nil
}

func (c *Capability) handleVerificationPut(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	id, err := verificationIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	sourceRevisionID, err := decodeVerificationRequest(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	created, isNew, conflict, err := c.store.saveVerification(r.Context(), id, taskID, r.PathValue("finding_id"), sourceRevisionID)
	if err != nil {
		status := 500
		message := "保存独立验证失败"
		if errors.Is(err, errFindingNotFound) {
			status, message = 404, "任务内候选不存在"
		} else if errors.Is(err, errVerificationStale) {
			status, message = 409, errVerificationStale.Error()
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	if conflict {
		writeJSON(w, 409, map[string]string{"error": errVerificationConflict.Error()})
		return
	}
	status := 200
	if isNew {
		status = 201
		c.auditSessionStart(r.Context(), "verification", taskID, created.FindingID, created.VerificationID, created.SessionID)
	}
	writeJSON(w, status, map[string]any{"verification": created, "created": isNew, "idempotent": !isNew})
}

func (c *Capability) verificationDetailResponse(w http.ResponseWriter, r *http.Request, model *verificationModel) {
	detail, err := c.store.verificationDetail(r.Context(), model)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取独立验证失败"})
		return
	}
	if detail == nil {
		writeJSON(w, 404, map[string]string{"error": "独立验证不存在"})
		return
	}
	writeJSON(w, 200, detail)
}

func (c *Capability) handleVerificationGet(w http.ResponseWriter, r *http.Request) {
	model, err := c.store.verificationByID(r.Context(), r.PathValue("id"), r.PathValue("finding_id"), r.PathValue("verification_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取独立验证失败"})
		return
	}
	c.verificationDetailResponse(w, r, model)
}

func (c *Capability) handleVerificationBySession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, 400, map[string]string{"error": "session_id不能为空"})
		return
	}
	model, err := c.store.verificationForSession(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取独立验证失败"})
		return
	}
	c.verificationDetailResponse(w, r, model)
}

func (c *Capability) handleVerificationList(w http.ResponseWriter, r *http.Request) {
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
	items, err := c.store.verificationList(r.Context(), taskID, findingID, limit, afterCreatedAt, afterID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取独立验证列表失败"})
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next any
	if hasMore {
		last := items[len(items)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt + "\n" + last.VerificationID))
	}
	writeJSON(w, 200, findingCollection(taskID, limit, items, hasMore, next))
}
