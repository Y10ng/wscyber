package webapi

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func findingQuery(r *http.Request) (int, int64, error) {
	limit := 25
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 50 {
			return 0, 0, fmt.Errorf("limit须为1至50")
		}
	}
	var after int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) == 0 || len(decoded) > 32 {
			return 0, 0, fmt.Errorf("cursor非法")
		}
		after, err = strconv.ParseInt(string(decoded), 10, 64)
		if err != nil || after < 1 {
			return 0, 0, fmt.Errorf("cursor非法")
		}
	}
	return limit, after, nil
}

func findingCursor(value int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(value, 10)))
}

func findingCollection(taskID string, limit int, items any, hasMore bool, nextCursor any) map[string]any {
	return map[string]any{"task_id": taskID, "items": items, "limit": limit, "has_more": hasMore, "next_cursor": nextCursor}
}

func (c *Capability) handleFindingList(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	limit, after, err := findingQuery(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	task, err := c.store.Get(r.Context(), taskID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取任务失败"})
		return
	}
	if task == nil {
		writeJSON(w, 404, map[string]string{"error": "任务不存在"})
		return
	}
	items, err := c.store.findingList(r.Context(), taskID, limit, after)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取候选列表失败"})
		return
	}
	hasMore := len(items) > limit
	var next any
	if hasMore {
		items = items[:limit]
		lastNumber, _ := strconv.ParseInt(strings.TrimPrefix(items[len(items)-1].Number, "WSC-"), 10, 64)
		next = findingCursor(lastNumber)
	}
	writeJSON(w, 200, findingCollection(taskID, limit, items, hasMore, next))
}

func (c *Capability) handleFindingCurrent(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	detail, err := c.store.findingCurrent(r.Context(), taskID, r.PathValue("finding_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取候选失败"})
		return
	}
	if detail == nil {
		writeJSON(w, 404, map[string]string{"error": "任务内候选不存在"})
		return
	}
	writeJSON(w, 200, detail)
}

func (c *Capability) handleFindingHistory(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	limit, after, err := findingQuery(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	findingID := r.PathValue("finding_id")
	exists, err := c.store.findingExists(r.Context(), taskID, findingID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取候选失败"})
		return
	}
	if !exists {
		writeJSON(w, 404, map[string]string{"error": "任务内候选不存在"})
		return
	}
	items, err := c.store.findingHistory(r.Context(), taskID, findingID, limit, after)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取候选历史失败"})
		return
	}
	hasMore := len(items) > limit
	var next any
	if hasMore {
		items = items[:limit]
		next = findingCursor(items[len(items)-1].Revision)
	}
	writeJSON(w, 200, findingCollection(taskID, limit, items, hasMore, next))
}

func (c *Capability) handleFindingRevision(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	detail, err := c.store.findingRevision(r.Context(), taskID, r.PathValue("finding_id"), r.PathValue("revision_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取候选revision失败"})
		return
	}
	if detail == nil {
		writeJSON(w, 404, map[string]string{"error": "任务内候选revision不存在"})
		return
	}
	writeJSON(w, 200, detail)
}
