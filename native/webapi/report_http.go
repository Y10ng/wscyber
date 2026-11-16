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

const maxReportItems = 50

type saveReportRequest struct {
	Items []ReportItem `json:"items"`
}

func validReferenceID(id string) bool {
	return id != "" && len(id) <= 128 && !strings.ContainsAny(id, "/\\?#%") && !strings.ContainsFunc(id, func(c rune) bool { return c < 0x20 || c == 0x7f })
}

func decodeReportRequest(r *http.Request) ([]ReportItem, error) {
	defer r.Body.Close()
	var body saveReportRequest
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return nil, errors.New("请求体不是合法JSON或含未知字段")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("请求体只能包含一个JSON对象")
	}
	if len(body.Items) < 1 || len(body.Items) > maxReportItems {
		return nil, errors.New("items须为1至50项")
	}
	findings := map[string]bool{}
	for index := range body.Items {
		item := &body.Items[index]
		if !validReferenceID(item.FindingID) || !validReferenceID(item.RevisionID) || len(item.VerificationIDs) > 16 {
			return nil, errors.New("finding_id/revision_id非法或verification_ids超过16项")
		}
		if findings[item.FindingID] {
			return nil, errors.New("同一候选不能在报告中重复")
		}
		findings[item.FindingID] = true
		verifications := map[string]bool{}
		for _, id := range item.VerificationIDs {
			if !validReferenceID(id) || verifications[id] {
				return nil, errors.New("verification_ids重复或非法")
			}
			verifications[id] = true
		}
		if item.VerificationIDs == nil {
			item.VerificationIDs = []string{} // 缺省与空数组是同一规范items。
		}
	}
	return body.Items, nil
}

func reportIDFromPath(r *http.Request) (string, error) {
	id := r.PathValue("report_id")
	if !validReferenceID(id) {
		return "", errors.New("report id非法")
	}
	return id, nil
}

func (c *Capability) handleReportPut(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	id, err := reportIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	items, err := decodeReportRequest(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	saved, isNew, conflict, err := c.store.saveReport(r.Context(), id, taskID, items)
	if err != nil {
		status, message := 500, "保存报告失败"
		if errors.Is(err, errReportNotFound) {
			status, message = 404, errReportNotFound.Error()
		} else if errors.Is(err, errReportStale) {
			status, message = 409, errReportStale.Error()
		}
		writeJSON(w, status, map[string]string{"error": message})
		return
	}
	if conflict {
		writeJSON(w, 409, map[string]string{"error": errReportConflict.Error()})
		return
	}
	status := 200
	if isNew {
		status = 201
	}
	writeJSON(w, status, map[string]any{"report": saved, "created": isNew, "idempotent": !isNew})
}

func (c *Capability) loadReportDetail(w http.ResponseWriter, r *http.Request, model *reportModel) *ReportDetail {
	detail, err := c.store.reportDetail(r.Context(), model)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取报告失败"})
		return nil
	}
	if detail == nil {
		writeJSON(w, 404, map[string]string{"error": "报告不存在"})
		return nil
	}
	return detail
}

func (c *Capability) reportFromPath(w http.ResponseWriter, r *http.Request) *ReportDetail {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return nil
	}
	model, err := c.store.reportByID(r.Context(), taskID, r.PathValue("report_id"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取报告失败"})
		return nil
	}
	return c.loadReportDetail(w, r, model)
}

func (c *Capability) handleReportGet(w http.ResponseWriter, r *http.Request) {
	if detail := c.reportFromPath(w, r); detail != nil {
		writeJSON(w, 200, detail)
	}
}

func (c *Capability) handleReportBySession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, 400, map[string]string{"error": "session_id不能为空"})
		return
	}
	model, err := c.store.reportForSession(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取报告失败"})
		return
	}
	if detail := c.loadReportDetail(w, r, model); detail != nil {
		writeJSON(w, 200, detail)
	}
}

func (c *Capability) handleReportList(w http.ResponseWriter, r *http.Request) {
	taskID, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	limit := 25
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
	task, err := c.store.Get(r.Context(), taskID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取任务失败"})
		return
	}
	if task == nil {
		writeJSON(w, 404, map[string]string{"error": "任务不存在"})
		return
	}
	items, err := c.store.reportList(r.Context(), taskID, limit, afterCreatedAt, afterID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取报告列表失败"})
		return
	}
	hasMore := len(items) > limit
	var next any
	if hasMore {
		items = items[:limit]
		last := items[len(items)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.CreatedAt + "\n" + last.ReportID))
	}
	writeJSON(w, 200, findingCollection(taskID, limit, items, hasMore, next))
}

func (c *Capability) handleReportExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format != "markdown" && format != "json" {
		writeJSON(w, 400, map[string]string{"error": "format仅支持markdown/json"})
		return
	}
	detail := c.reportFromPath(w, r)
	if detail == nil {
		return
	}
	if detail.Draft == nil {
		writeJSON(w, 409, map[string]string{"error": "报告尚无草稿，无法导出"})
		return
	}
	if format == "markdown" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, renderReportMarkdown(detail))
		return
	}
	raw, err := renderReportJSON(detail)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "报告JSON编码失败"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
