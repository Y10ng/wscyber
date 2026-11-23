package webapi

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	aop "github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/uptrace/bun"
)

// UsageTotals 是一组 Usage 事件的 token 汇总。Calls 为 Event_Usage 事件数
// （上游每个模型轮次一条，轮内重试已合并），不是 HTTP 请求数。
type UsageTotals struct {
	InputTokens      uint64 `json:"input_tokens"`
	OutputTokens     uint64 `json:"output_tokens"`
	TotalTokens      uint64 `json:"total_tokens"`
	CacheReadTokens  uint64 `json:"cache_read_tokens"`
	CacheWriteTokens uint64 `json:"cache_write_tokens"`
	Calls            uint64 `json:"calls"`
}

func (u *UsageTotals) add(usage *aop.TokenUsage) {
	total := usage.GetTotalTokens()
	if total == 0 {
		total = usage.GetInputTokens() + usage.GetOutputTokens()
	}
	u.InputTokens += usage.GetInputTokens()
	u.OutputTokens += usage.GetOutputTokens()
	u.TotalTokens += total
	u.CacheReadTokens += usage.GetDetail()["cache_read"]
	u.CacheWriteTokens += usage.GetDetail()["cache_write"]
	u.Calls++
}

type UsageDay struct {
	Day string `json:"day"`
	UsageTotals
}

type UsageModel struct {
	Model string `json:"model"`
	UsageTotals
}

type UsageTask struct {
	TaskID string `json:"task_id"`
	Title  string `json:"title"`
	UsageTotals
}

type UsageSession struct {
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	FindingID string `json:"finding_id"`
	ReportID  string `json:"report_id"`
	UsageTotals
}

// UsageReport 是 GET /api/webapi/usage 响应。ByTask 仅全局查询、BySession 仅任务查询时出现。
// SkippedEvents 为无法解析的事件行数（损坏 proto 或时间），不计入任何汇总。
type UsageReport struct {
	Timezone      string          `json:"timezone"`
	Totals        UsageTotals     `json:"totals"`
	ByDay         []UsageDay      `json:"by_day"`
	ByModel       []UsageModel    `json:"by_model"`
	ByTask        *[]UsageTask    `json:"by_task,omitempty"`
	BySession     *[]UsageSession `json:"by_session,omitempty"`
	SkippedEvents int             `json:"skipped_events"`
}

// usageSessionRef 是会话到任务/角色的映射。
type usageSessionRef struct {
	taskID    string
	role      string
	findingID string
	reportID  string
}

// usageEventField 是 aop.Event 中 usage oneof 的字段号；取自生成描述符，避免硬编码。
var usageEventField = protowire.Number((&aop.Event{}).ProtoReflect().Descriptor().Fields().ByName("usage").Number())

// eventUsage 只扫描顶层字段取 Event_Usage，不完整反序列化（工具结果等大字段只跳过）。
// TurnEnded.Usage 是轮累计，不在顶层 usage 字段，天然不被计入。
func eventUsage(raw []byte) (*aop.TokenUsage, error) {
	for len(raw) > 0 {
		number, kind, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		raw = raw[n:]
		if number == usageEventField && kind == protowire.BytesType {
			value, m := protowire.ConsumeBytes(raw)
			if m < 0 {
				return nil, protowire.ParseError(m)
			}
			usage := &aop.TokenUsage{}
			if err := proto.Unmarshal(value, usage); err != nil {
				return nil, err
			}
			return usage, nil
		}
		m := protowire.ConsumeFieldValue(number, kind, raw)
		if m < 0 {
			return nil, protowire.ParseError(m)
		}
		raw = raw[m:]
	}
	return nil, nil
}

// localZoneName 返回 Hub 进程本地时区名。Go 用 /etc/localtime 时名字是 "Local"，
// 此时从符号链接目标取 IANA 名；都拿不到则回落为 UTC 偏移（如 "+08:00"）。
func localZoneName() string {
	if name := time.Local.String(); name != "" && name != "Local" {
		return name
	}
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && !strings.HasPrefix(tz, "/") {
		return tz
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			return target[i+len("zoneinfo/"):]
		}
	}
	return time.Now().Format("-07:00")
}

// usageTitle 取任务目标首个非空行（界面以 objective 作为任务标题），最长 120 字符。
func usageTitle(objective string) string {
	line := ""
	for _, candidate := range strings.Split(objective, "\n") {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			line = candidate
			break
		}
	}
	if utf8.RuneCountInString(line) > 120 {
		runes := []rune(line)
		line = string(runes[:120]) + "…"
	}
	return line
}

func validUsageTaskID(id string) bool {
	if id == "" || len(id) > 128 || strings.ContainsAny(id, "/\\?#%") {
		return false
	}
	return !strings.ContainsFunc(id, func(c rune) bool { return c < 0x20 || c == 0x7f })
}

// parseUsageDay 解析本地日期；空串返回零值。
func parseUsageDay(name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	day, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil || day.Format("2006-01-02") != value {
		return time.Time{}, fmt.Errorf("%s 必须是 YYYY-MM-DD", name)
	}
	return day, nil
}

// usageSessions 读取任务会话映射；taskID 为空时读全部任务。研究 > 验证 > 报告 > 复测 > 子 Agent，
// 同一 session 只取首个角色。
func (s *taskStore) usageSessions(ctx context.Context, taskID string) (map[string]usageSessionRef, map[string]string, error) {
	var taskIDs []string
	if taskID != "" {
		taskIDs = []string{taskID}
	}
	return s.usageSessionsForTasks(ctx, taskIDs)
}

// nil taskIDs 表示全局；任务列表传本页 ID，一次读取每类会话。
func (s *taskStore) usageSessionsForTasks(ctx context.Context, taskIDs []string) (map[string]usageSessionRef, map[string]string, error) {
	sessions := map[string]usageSessionRef{}
	titles := map[string]string{}
	filter, args := "", []any{}
	if taskIDs != nil {
		filter, args = " WHERE task_id IN (?)", []any{bun.In(taskIDs)}
	}
	taskFilter := strings.Replace(filter, "task_id", "id", 1)
	queries := []struct {
		role, query string
	}{
		{"verification", `SELECT task_id, session_id, finding_id, '' FROM webapi_verifications`},
		{"report", `SELECT task_id, session_id, '', id FROM webapi_reports`},
		{"retest", `SELECT task_id, session_id, finding_id, '' FROM webapi_retests`},
		{"subagent", `SELECT task_id, session_id, '', '' FROM webapi_task_subsessions`},
	}
	// 各表 session_id 唯一（验证/报告/复测有唯一索引，子会话为主键），同表内顺序不影响“首个角色”；
	// 使用 session_id 作次序键，保证结果确定。
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		if err := scanRows(db.QueryContext(ctx, `SELECT id, objective, initial_session_id FROM webapi_tasks`+taskFilter, args...))(func(rows *sql.Rows) error {
			var id, objective, session string
			if err := rows.Scan(&id, &objective, &session); err != nil {
				return err
			}
			titles[id] = usageTitle(objective)
			if _, seen := sessions[session]; session != "" && !seen {
				sessions[session] = usageSessionRef{taskID: id, role: "research"}
			}
			return nil
		}); err != nil {
			return err
		}
		for _, q := range queries {
			if err := scanRows(db.QueryContext(ctx, q.query+filter+` ORDER BY created_at COLLATE "C", session_id COLLATE "C"`, args...))(func(rows *sql.Rows) error {
				var ref usageSessionRef
				var session string
				if err := rows.Scan(&ref.taskID, &session, &ref.findingID, &ref.reportID); err != nil {
					return err
				}
				ref.role = q.role
				if _, seen := sessions[session]; session != "" && !seen {
					sessions[session] = ref
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return sessions, titles, nil
}

func closeRows(rows *sql.Rows) error {
	err := rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	return err
}

// Usage 汇总 chat_aop_events 中的 Event_Usage。taskID 为空统计全部会话（未映射到任务的
// 会话归 task_id=""）；from/to 为本地日期闭区间，零值表示不限。任务不存在返回 (nil, nil)。
func (s *taskStore) Usage(ctx context.Context, taskID string, from, to time.Time) (*UsageReport, error) {
	sessions, titles, err := s.usageSessions(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if taskID != "" {
		if _, ok := titles[taskID]; !ok {
			return nil, nil
		}
	}
	report := &UsageReport{Timezone: localZoneName(), ByDay: []UsageDay{}, ByModel: []UsageModel{}}

	var taskIDs []string
	if taskID != "" {
		taskIDs = []string{taskID}
		if len(sessions) == 0 {
			report.BySession = &[]UsageSession{}
			return report, nil
		}
	}
	start, end := from, time.Time{}
	if !to.IsZero() {
		end = to.AddDate(0, 0, 1)
	}
	days := map[string]*UsageTotals{}
	models := map[string]*UsageTotals{}
	tasks := map[string]*UsageTotals{}
	bySession := map[string]*UsageTotals{}
	bucket := func(m map[string]*UsageTotals, key string) *UsageTotals {
		if m[key] == nil {
			m[key] = &UsageTotals{}
		}
		return m[key]
	}
	report.SkippedEvents, err = s.scanUsage(ctx, sessions, taskIDs, start, end, nil, func(session, task string, at time.Time, usage *aop.TokenUsage) {
		at = at.In(time.Local)
		report.Totals.add(usage)
		bucket(days, at.Format("2006-01-02")).add(usage)
		bucket(models, usage.GetModel()).add(usage)
		if taskID != "" {
			bucket(bySession, session).add(usage)
		} else {
			bucket(tasks, task).add(usage)
		}
	})
	if err != nil {
		return nil, err
	}

	for day, totals := range days {
		report.ByDay = append(report.ByDay, UsageDay{Day: day, UsageTotals: *totals})
	}
	sort.Slice(report.ByDay, func(i, j int) bool { return report.ByDay[i].Day < report.ByDay[j].Day })
	for model, totals := range models {
		report.ByModel = append(report.ByModel, UsageModel{Model: model, UsageTotals: *totals})
	}
	sort.Slice(report.ByModel, func(i, j int) bool {
		return byTotalDesc(report.ByModel[i].UsageTotals, report.ByModel[j].UsageTotals, report.ByModel[i].Model, report.ByModel[j].Model)
	})
	if taskID != "" {
		list := []UsageSession{}
		for session, totals := range bySession {
			ref := sessions[session]
			list = append(list, UsageSession{SessionID: session, Role: ref.role, FindingID: ref.findingID, ReportID: ref.reportID, UsageTotals: *totals})
		}
		sort.Slice(list, func(i, j int) bool {
			return byTotalDesc(list[i].UsageTotals, list[j].UsageTotals, list[i].SessionID, list[j].SessionID)
		})
		report.BySession = &list
	} else {
		list := []UsageTask{}
		for task, totals := range tasks {
			list = append(list, UsageTask{TaskID: task, Title: titles[task], UsageTotals: *totals})
		}
		sort.Slice(list, func(i, j int) bool {
			return byTotalDesc(list[i].UsageTotals, list[j].UsageTotals, list[i].TaskID, list[j].TaskID)
		})
		report.ByTask = &list
	}
	return report, nil
}

// byTotalDesc 按 total 降序，同值按键升序保证输出确定。
func byTotalDesc(a, b UsageTotals, keyA, keyB string) bool {
	if a.TotalTokens != b.TotalTokens {
		return a.TotalTokens > b.TotalTokens
	}
	return keyA < keyB
}

// handleUsage: GET /api/webapi/usage?task_id=&from=&to=，只读汇总。
func (c *Capability) handleUsage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	taskID := query.Get("task_id") // 空串等同未传：全局统计
	if taskID != "" && !validUsageTaskID(taskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id 非法"})
		return
	}
	from, err := parseUsageDay("from", query.Get("from"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	to, err := parseUsageDay("to", query.Get("to"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from 不能晚于 to"})
		return
	}
	report, err := c.store.Usage(r.Context(), taskID, from, to)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取用量失败"})
		return
	}
	if report == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "任务不存在"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
