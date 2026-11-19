package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/uptrace/bun"
)

// sessionKindsMaxIDs 限制一次批量归属查询的会话数（去重前）。
const sessionKindsMaxIDs = 500

type sessionKindsRequest struct {
	SessionIDs []string `json:"session_ids"`
}

// SessionKindItem 是一个会话的归属：任务本身或任务下的验证/报告/复测。
type SessionKindItem struct {
	Kind          string `json:"kind"`
	TaskID        string `json:"task_id"`
	TaskSessionID string `json:"task_session_id"`
	TaskObjective string `json:"task_objective"`
	Number        string `json:"number"`
	Title         string `json:"title"`
	CreatedAt     string `json:"created_at"`
}

func decodeSessionKindsRequest(r *http.Request) ([]string, error) {
	defer r.Body.Close()
	var body sessionKindsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return nil, errors.New("请求体不是合法JSON或含未知字段")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("请求体只能包含一个JSON对象")
	}
	if len(body.SessionIDs) == 0 || len(body.SessionIDs) > sessionKindsMaxIDs {
		return nil, errors.New("session_ids须为1至500个会话")
	}
	seen := make(map[string]struct{}, len(body.SessionIDs))
	ids := make([]string, 0, len(body.SessionIDs))
	for _, id := range body.SessionIDs {
		if id == "" || len(id) > 256 {
			return nil, errors.New("session_id非法")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// sessionKindPin 是验证/复测共用的固定候选来源列。
type sessionKindPin struct {
	Kind             string
	SessionID        string
	TaskID           string
	SourceRevisionID string
	CreatedAt        string
}

func (s *taskStore) sessionKinds(ctx context.Context, ids []string) (map[string]SessionKindItem, error) {
	items := make(map[string]SessionKindItem)
	var tasks []taskModel
	if err := s.orm.NewSelect().Model(&tasks).Where("initial_session_id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	byTaskID := make(map[string]taskModel, len(tasks))
	for _, task := range tasks {
		byTaskID[task.ID] = task
		items[task.InitialSessionID] = SessionKindItem{Kind: "task", TaskID: task.ID, TaskSessionID: task.InitialSessionID, TaskObjective: task.Objective, Title: task.Objective, CreatedAt: task.CreatedAt}
	}

	pins := make(map[string]sessionKindPin)
	var verifications []verificationModel
	if err := s.orm.NewSelect().Model(&verifications).Where("session_id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, model := range verifications {
		pins[model.SessionID] = sessionKindPin{Kind: "verification", SessionID: model.SessionID, TaskID: model.TaskID, SourceRevisionID: model.SourceRevisionID, CreatedAt: model.CreatedAt}
	}
	var retests []retestModel
	if err := s.orm.NewSelect().Model(&retests).Where("session_id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, model := range retests {
		pins[model.SessionID] = sessionKindPin{Kind: "retest", SessionID: model.SessionID, TaskID: model.TaskID, SourceRevisionID: model.SourceRevisionID, CreatedAt: model.CreatedAt}
	}

	revisionIDs := make([]string, 0, len(pins))
	for _, pin := range pins {
		revisionIDs = append(revisionIDs, pin.SourceRevisionID)
	}
	type revisionLabel struct {
		ID     string `bun:"id"`
		Number int64  `bun:"number"`
		Title  string `bun:"title"`
	}
	labels := make(map[string]revisionLabel)
	if len(revisionIDs) > 0 {
		var rows []revisionLabel
		if err := s.orm.NewSelect().Model((*findingRevisionModel)(nil)).Column("id", "number", "title").Where("id IN (?)", bun.In(revisionIDs)).Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			labels[row.ID] = row
		}
	}
	for _, pin := range pins {
		task, ok := byTaskID[pin.TaskID]
		if !ok {
			loaded, err := findOptional[taskModel](ctx, s.orm, "id = ?", pin.TaskID)
			if err != nil {
				return nil, err
			}
			if loaded == nil {
				continue
			}
			byTaskID[pin.TaskID] = *loaded
			task = *loaded
		}
		label := labels[pin.SourceRevisionID]
		items[pin.SessionID] = SessionKindItem{Kind: pin.Kind, TaskID: task.ID, TaskSessionID: task.InitialSessionID, TaskObjective: task.Objective, Number: findingNumber(label.Number), Title: label.Title, CreatedAt: pin.CreatedAt}
	}

	var reports []reportModel
	if err := s.orm.NewSelect().Model(&reports).Where("session_id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	reportIDs := make([]string, 0, len(reports))
	for _, report := range reports {
		reportIDs = append(reportIDs, report.ID)
	}
	type draftTitle struct {
		ReportID    string `bun:"report_id"`
		PayloadJSON string `bun:"payload_json"`
	}
	titles := make(map[string]string)
	if len(reportIDs) > 0 {
		var rows []draftTitle
		if err := s.orm.NewSelect().Model((*reportDraftModel)(nil)).Column("report_id", "payload_json").Where("report_id IN (?)", bun.In(reportIDs)).Scan(ctx, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			var stored storedReportDraft
			if err := json.Unmarshal([]byte(row.PayloadJSON), &stored); err != nil {
				return nil, err
			}
			titles[row.ReportID] = stored.Payload.Title
		}
	}
	for _, report := range reports {
		task, ok := byTaskID[report.TaskID]
		if !ok {
			loaded, err := findOptional[taskModel](ctx, s.orm, "id = ?", report.TaskID)
			if err != nil {
				return nil, err
			}
			if loaded == nil {
				continue
			}
			byTaskID[report.TaskID] = *loaded
			task = *loaded
		}
		items[report.SessionID] = SessionKindItem{Kind: "report", TaskID: task.ID, TaskSessionID: task.InitialSessionID, TaskObjective: task.Objective, Title: titles[report.ID], CreatedAt: report.CreatedAt}
	}
	return items, nil
}

func (c *Capability) handleSessionKinds(w http.ResponseWriter, r *http.Request) {
	ids, err := decodeSessionKindsRequest(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	items, err := c.store.sessionKinds(r.Context(), ids)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 500, map[string]string{"error": "读取会话归属失败"})
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
