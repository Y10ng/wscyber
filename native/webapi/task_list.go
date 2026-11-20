package webapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// 全局任务列表（U2a）：只读，跨任务汇总状态、计数与 token。见 native/OVERVIEW.md。

const (
	TaskStatusRunning    = "running"
	TaskStatusIdle       = "idle"
	TaskStatusNotStarted = "not_started"
	TaskStatusUnknown    = "unknown"

	globalListDefaultLimit = 50
	globalListMaxLimit     = 200
	globalQueryMaxRunes    = 200
	// sqlInChunk 是批量 IN (...) 的分块大小，远低于 SQLite 变量上限。
	sqlInChunk = 500
)

var taskStatusOrder = []string{TaskStatusRunning, TaskStatusIdle, TaskStatusNotStarted, TaskStatusUnknown}

type TaskListPolicy struct {
	Scope    string `json:"scope"`
	Approval string `json:"approval"`
}

// TaskListItem 是 GET /api/webapi/task-list 的一项。
type TaskListItem struct {
	TaskID            string         `json:"task_id"`
	Title             string         `json:"title"`
	Origins           []string       `json:"origins"`
	Asset             *TaskAssetRef  `json:"asset"`
	Scheduled         bool           `json:"scheduled"`
	CreatedAt         string         `json:"created_at"`
	InitialSessionID  string         `json:"initial_session_id"`
	Status            string         `json:"status"`
	LastActivityAt    string         `json:"last_activity_at"`
	FindingCount      int            `json:"finding_count"`
	VerificationCount int            `json:"verification_count"`
	SupportedCount    int            `json:"supported_count"`
	ReportCount       int            `json:"report_count"`
	RetestCount       int            `json:"retest_count"`
	TotalTokens       uint64         `json:"total_tokens"`
	Policy            TaskListPolicy `json:"policy"`
	Scenario          TaskScenario   `json:"scenario"`
	Archived          bool           `json:"archived"`
}

// taskListRow 是全部任务的轻量行（过滤、状态与计数用），不含逐任务聚合。
type taskListRow struct {
	item      TaskListItem
	objective string
	knownInfo string
	createdAt time.Time
	hasTurn   bool
}

// taskListRows 读取全部任务（assetID 非空时只取该资产关联任务），一次 SQL + 一次计划运行集合查询。
// hasTurn = 研究会话持久化事件里有任何带 turn_id 的事件（走 chat_aop_events(session_id, cursor) 唯一索引）。
// archived = 初始会话原生行 archived=1；会话行不存在视为未归档。
func (s *taskStore) taskListRows(ctx context.Context, assetID string) ([]*taskListRow, error) {
	query := `SELECT t.id, t.objective, t.known_info, t.origins_json, t.initial_session_id, t.created_at,
		COALESCE(a.id, ''), COALESCE(a.name, ''), COALESCE(p.scope, ''), COALESCE(p.approval, ''),
		COALESCE(sc.scenario, ''), COALESCE(sc.done_criteria, ''), COALESCE(ex.max_rounds, 0),
		EXISTS (SELECT 1 FROM chat_aop_events e WHERE e.session_id = t.initial_session_id AND e.turn_id != ''),
		COALESCE((SELECT cs.archived = 1 FROM chat_sessions cs WHERE cs.id = t.initial_session_id), 0)
		FROM webapi_tasks t
		LEFT JOIN webapi_asset_tasks l ON l.task_id = t.id
		LEFT JOIN webapi_assets a ON a.id = l.asset_id
		LEFT JOIN webapi_task_policies p ON p.task_id = t.id
		LEFT JOIN webapi_task_scenarios sc ON sc.task_id = t.id
		LEFT JOIN webapi_task_explore ex ON ex.task_id = t.id`
	args := []any{}
	if assetID != "" {
		query += ` WHERE l.asset_id = ?`
		args = append(args, assetID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []*taskListRow{}
	for rows.Next() {
		row := &taskListRow{}
		var originsJSON, assetRefID, assetName, scope, approval, scenarioKey, doneCriteria string
		var maxRounds int
		if err := rows.Scan(&row.item.TaskID, &row.objective, &row.knownInfo, &originsJSON, &row.item.InitialSessionID, &row.item.CreatedAt,
			&assetRefID, &assetName, &scope, &approval, &scenarioKey, &doneCriteria, &maxRounds, &row.hasTurn, &row.item.Archived); err != nil {
			rows.Close()
			return nil, err
		}
		origins, decodeErr := decodeOrigins(originsJSON)
		if decodeErr != nil || origins == nil {
			origins = []string{}
		}
		row.item.Origins = origins
		row.item.Title = usageTitle(row.objective)
		if assetRefID != "" {
			row.item.Asset = &TaskAssetRef{ID: assetRefID, Name: assetName}
		}
		policy := defaultTaskPolicy()
		if scope != "" {
			policy.Scope, policy.Approval = scope, approval
		}
		row.item.Policy = TaskListPolicy{Scope: policy.Scope, Approval: policy.Approval}
		row.item.Scenario = defaultTaskScenario()
		if scenarioKey != "" {
			row.item.Scenario = newTaskScenario(scenarioKey, doneCriteria, false).withMaxRounds(maxRounds)
		}
		row.createdAt = parseAt(row.item.CreatedAt)
		out = append(out, row)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	scheduled := map[string]bool{}
	runRows, err := s.db.QueryContext(ctx, `SELECT DISTINCT task_id FROM webapi_schedule_runs WHERE task_id != ''`)
	if err != nil {
		return nil, err
	}
	for runRows.Next() {
		var taskID string
		if err := runRows.Scan(&taskID); err != nil {
			runRows.Close()
			return nil, err
		}
		scheduled[taskID] = true
	}
	if err := closeRows(runRows); err != nil {
		return nil, err
	}
	for _, row := range out {
		row.item.Scheduled = scheduled[row.item.TaskID]
	}
	return out, nil
}

// taskRunStatus：无任何轮次 not_started；否则按原生 SessionRunning（研究会话）判定 running/idle，
// 判定出错为 unknown。未注入原生管理 API（launcher 为空，仅测试/无会话服务）时按不在运行处理。
func (c *Capability) taskRunStatus(ctx context.Context, row *taskListRow) string {
	if !row.hasTurn {
		return TaskStatusNotStarted
	}
	if c.launcher == nil {
		return TaskStatusIdle
	}
	running, err := c.launcher.SessionRunning(ctx, row.item.InitialSessionID)
	switch {
	case err != nil:
		return TaskStatusUnknown
	case running:
		return TaskStatusRunning
	}
	return TaskStatusIdle
}

// containsFold 不区分大小写子串匹配（Unicode 小写）；needle 已小写。
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), needle)
}

func (row *taskListRow) matches(needle string) bool {
	if needle == "" {
		return true
	}
	if containsFold(row.objective, needle) || containsFold(row.knownInfo, needle) {
		return true
	}
	for _, origin := range row.item.Origins {
		if containsFold(origin, needle) {
			return true
		}
	}
	return false
}

// createdDescLess：created_at（按时间解析）倒序，同时间按 id 倒序。
func createdDescLess(aAt time.Time, aID string, bAt time.Time, bID string) bool {
	if !aAt.Equal(bAt) {
		return aAt.After(bAt)
	}
	return aID > bID
}

// ---- 批量聚合（只对本页任务） ----

// inChunks 把 ids 分块执行 fn(placeholders, args)。
func inChunks(ids []string, fn func(placeholders string, args []any) error) error {
	for start := 0; start < len(ids); start += sqlInChunk {
		end := min(start+sqlInChunk, len(ids))
		args := make([]any, 0, end-start)
		for _, id := range ids[start:end] {
			args = append(args, id)
		}
		if err := fn(strings.TrimSuffix(strings.Repeat("?,", end-start), ","), args); err != nil {
			return err
		}
	}
	return nil
}

// countByTask 对 table 按 task_id 计数（本页任务）。
func (s *taskStore) countByTask(ctx context.Context, table string, taskIDs []string) (map[string]int, error) {
	out := map[string]int{}
	err := inChunks(taskIDs, func(placeholders string, args []any) error {
		rows, err := s.db.QueryContext(ctx, `SELECT task_id, COUNT(*) FROM `+table+` WHERE task_id IN (`+placeholders+`) GROUP BY task_id`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var taskID string
			var count int
			if err := rows.Scan(&taskID, &count); err != nil {
				rows.Close()
				return err
			}
			out[taskID] = count
		}
		return closeRows(rows)
	})
	return out, err
}

// supportedByTask：每个候选取最新保存的验证判读（结果保存时间，同 C3 latest_verdict），统计为 supported 的候选数。
func (s *taskStore) supportedByTask(ctx context.Context, taskIDs []string) (map[string]int, error) {
	type latest struct {
		taskID, verdict, key string
		at                   time.Time
	}
	byFinding := map[string]*latest{}
	err := inChunks(taskIDs, func(placeholders string, args []any) error {
		rows, err := s.db.QueryContext(ctx, `SELECT v.task_id, v.finding_id, v.id, COALESCE(json_extract(r.payload_json, '$.payload.verdict'), ''), r.created_at
			FROM webapi_verifications v JOIN webapi_verification_results r ON r.verification_id = v.id
			WHERE v.task_id IN (`+placeholders+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var taskID, findingID, verificationID, verdict, createdAt string
			if err := rows.Scan(&taskID, &findingID, &verificationID, &verdict, &createdAt); err != nil {
				rows.Close()
				return err
			}
			at, key := parseAt(createdAt), createdAt+"\x00"+verificationID
			if current := byFinding[findingID]; current == nil || laterThan(at, key, current.at, current.key) {
				byFinding[findingID] = &latest{taskID: taskID, verdict: verdict, key: key, at: at}
			}
		}
		return closeRows(rows)
	})
	out := map[string]int{}
	for _, entry := range byFinding {
		if entry.verdict == "supported" {
			out[entry.taskID]++
		}
	}
	return out, err
}

// taskSessions 返回本页任务全部业务会话（研究/验证/报告/复测）到任务的映射；同一会话取首个。
func (s *taskStore) taskSessions(ctx context.Context, rows []*taskListRow) (map[string]string, error) {
	sessions := map[string]string{}
	taskIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		taskIDs = append(taskIDs, row.item.TaskID)
		if session := row.item.InitialSessionID; session != "" {
			sessions[session] = row.item.TaskID
		}
	}
	for _, table := range []string{"webapi_verifications", "webapi_reports", "webapi_retests"} {
		err := inChunks(taskIDs, func(placeholders string, args []any) error {
			result, err := s.db.QueryContext(ctx, `SELECT task_id, session_id FROM `+table+` WHERE task_id IN (`+placeholders+`)`, args...)
			if err != nil {
				return err
			}
			for result.Next() {
				var taskID, session string
				if err := result.Scan(&taskID, &session); err != nil {
					result.Close()
					return err
				}
				if _, seen := sessions[session]; session != "" && !seen {
					sessions[session] = taskID
				}
			}
			return closeRows(result)
		})
		if err != nil {
			return nil, err
		}
	}
	return sessions, nil
}

// sessionActivity 对给定会话一次扫描 chat_aop_events：最新事件时间与 Event_Usage token 汇总（解析同 usage.go）。
// 损坏行跳过（同 USAGE 的 skipped_events，不计入）。
func (s *taskStore) sessionActivity(ctx context.Context, sessions map[string]string) (map[string]time.Time, map[string]*UsageTotals, error) {
	ids := make([]string, 0, len(sessions))
	for session := range sessions {
		ids = append(ids, session)
	}
	sort.Strings(ids)
	lastAt := map[string]time.Time{}
	tokens := map[string]*UsageTotals{}
	err := inChunks(ids, func(placeholders string, args []any) error {
		rows, err := s.db.QueryContext(ctx, `SELECT session_id, created_at, event_proto FROM chat_aop_events WHERE session_id IN (`+placeholders+`)`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var session, createdAt string
			var raw []byte
			if err := rows.Scan(&session, &createdAt, &raw); err != nil {
				rows.Close()
				return err
			}
			taskID := sessions[session]
			if at := parseAt(createdAt); !at.IsZero() && at.After(lastAt[taskID]) {
				lastAt[taskID] = at
			}
			usage, decodeErr := eventUsage(raw)
			if decodeErr != nil || usage == nil {
				continue
			}
			if tokens[taskID] == nil {
				tokens[taskID] = &UsageTotals{}
			}
			tokens[taskID].add(usage)
		}
		return closeRows(rows)
	})
	return lastAt, tokens, err
}

// fillTaskListPage 为本页任务批量填计数、最新活动时间与 token（每类一次 IN 查询）。
func (s *taskStore) fillTaskListPage(ctx context.Context, rows []*taskListRow) error {
	if len(rows) == 0 {
		return nil
	}
	taskIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		taskIDs = append(taskIDs, row.item.TaskID)
	}
	counts := map[string]map[string]int{}
	for _, table := range []string{"webapi_findings", "webapi_verifications", "webapi_reports", "webapi_retests"} {
		values, err := s.countByTask(ctx, table, taskIDs)
		if err != nil {
			return err
		}
		counts[table] = values
	}
	supported, err := s.supportedByTask(ctx, taskIDs)
	if err != nil {
		return err
	}
	sessions, err := s.taskSessions(ctx, rows)
	if err != nil {
		return err
	}
	lastAt, tokens, err := s.sessionActivity(ctx, sessions)
	if err != nil {
		return err
	}
	for _, row := range rows {
		id := row.item.TaskID
		row.item.FindingCount = counts["webapi_findings"][id]
		row.item.VerificationCount = counts["webapi_verifications"][id]
		row.item.ReportCount = counts["webapi_reports"][id]
		row.item.RetestCount = counts["webapi_retests"][id]
		row.item.SupportedCount = supported[id]
		row.item.LastActivityAt = row.item.CreatedAt
		if at, ok := lastAt[id]; ok {
			row.item.LastActivityAt = at.UTC().Format(time.RFC3339Nano)
		}
		if totals := tokens[id]; totals != nil {
			row.item.TotalTokens = totals.TotalTokens
		}
	}
	return nil
}

// ---- HTTP ----

// listParams 解析 limit（1..200，默认 50）、q（去首尾空白，≤200 字，小写）与可选 asset_id。
func listParams(r *http.Request) (limit int, needle, assetID string, err error) {
	query := r.URL.Query()
	limit = globalListDefaultLimit
	if raw := query.Get("limit"); raw != "" {
		value, convErr := strconv.Atoi(raw)
		if convErr != nil || value < 1 || value > globalListMaxLimit {
			return 0, "", "", fmt.Errorf("limit须为1至%d", globalListMaxLimit)
		}
		limit = value
	}
	needle = strings.TrimSpace(query.Get("q"))
	if utf8.RuneCountInString(needle) > globalQueryMaxRunes {
		return 0, "", "", fmt.Errorf("q 不能超过 %d 字", globalQueryMaxRunes)
	}
	needle = strings.ToLower(needle)
	if assetID = query.Get("asset_id"); assetID != "" && validPathID(assetID) != nil {
		return 0, "", "", fmt.Errorf("asset_id 非法")
	}
	return limit, needle, assetID, nil
}

// decodeKeyCursor 解析 base64url(created_at \n id) 游标；空串返回 ok=false。
func decodeKeyCursor(encoded string) (at time.Time, id string, present bool, err error) {
	if encoded == "" {
		return time.Time{}, "", false, nil
	}
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(encoded)
	parts := strings.Split(string(decoded), "\n")
	if decodeErr != nil || len(parts) != 2 || parts[1] == "" {
		return time.Time{}, "", false, fmt.Errorf("cursor非法")
	}
	at, parseErr := time.Parse(time.RFC3339Nano, parts[0])
	if parseErr != nil {
		return time.Time{}, "", false, fmt.Errorf("cursor非法")
	}
	return at, parts[1], true, nil
}

func encodeKeyCursor(createdAt, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt + "\n" + id))
}

// handleTaskList: GET /api/webapi/task-list?q=&status=&asset_id=&archived=&limit=&cursor=
func (c *Capability) handleTaskList(w http.ResponseWriter, r *http.Request) {
	limit, needle, assetID, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := r.URL.Query().Get("status")
	if status != "" && !slices.Contains(taskStatusOrder, status) {
		writeError(w, http.StatusBadRequest, "status 只能是 running、idle、not_started、unknown")
		return
	}
	archived := r.URL.Query().Get("archived")
	if archived == "" {
		archived = "0"
	}
	if archived != "0" && archived != "1" {
		writeError(w, http.StatusBadRequest, "archived 只能是 0 或 1")
		return
	}
	wantArchived := archived == "1"
	afterAt, afterID, hasCursor, err := decodeKeyCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := c.store.taskListRows(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务列表失败")
		return
	}
	counts := map[string]int{"all": 0, "archived": 0}
	for _, key := range taskStatusOrder {
		counts[key] = 0
	}
	filtered := make([]*taskListRow, 0, len(rows))
	for _, row := range rows {
		if !row.matches(needle) {
			continue
		}
		if row.item.Archived {
			counts["archived"]++
		}
		if row.item.Archived != wantArchived {
			continue
		}
		row.item.Status = c.taskRunStatus(r.Context(), row)
		counts["all"]++
		counts[row.item.Status]++
		if status == "" || row.item.Status == status {
			filtered = append(filtered, row)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return createdDescLess(filtered[i].createdAt, filtered[i].item.TaskID, filtered[j].createdAt, filtered[j].item.TaskID)
	})
	start := 0
	if hasCursor {
		start = sort.Search(len(filtered), func(i int) bool {
			return createdDescLess(afterAt, afterID, filtered[i].createdAt, filtered[i].item.TaskID)
		})
	}
	page := filtered[start:]
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	if err := c.store.fillTaskListPage(r.Context(), page); err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务统计失败")
		return
	}
	items := make([]TaskListItem, 0, len(page))
	for _, row := range page {
		items = append(items, row.item)
	}
	var next any
	if hasMore {
		last := page[len(page)-1]
		next = encodeKeyCursor(last.createdAt.UTC().Format(time.RFC3339Nano), last.item.TaskID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": hasMore, "next_cursor": next, "counts": counts})
}
