package webapi

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"slices"
	"sort"
	"time"
)

// 运营总览（U2a）：GET /api/webapi/overview?days=7|30|90，只读聚合。见 native/OVERVIEW.md。

const overviewListMax = 5

type OverviewDayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type OverviewVulnDay struct {
	Date    string `json:"date"`
	New     int    `json:"new"`
	Present int    `json:"present"`
	Fixed   int    `json:"fixed"`
}

type OverviewTokenDay struct {
	Date        string `json:"date"`
	TotalTokens uint64 `json:"total_tokens"`
}

type OverviewNextRun struct {
	AssetID   string `json:"asset_id"`
	AssetName string `json:"asset_name"`
	NextRunAt string `json:"next_run_at"`
}

type OverviewRecentRun struct {
	AssetID        string  `json:"asset_id"`
	AssetName      string  `json:"asset_name"`
	ScheduledFor   string  `json:"scheduled_for"`
	Status         string  `json:"status"`
	TaskID         string  `json:"task_id"`
	FollowupsState *string `json:"followups_state"`
}

type OverviewSupported struct {
	VulnID   string        `json:"vuln_id"`
	Title    string        `json:"title"`
	Asset    *TaskAssetRef `json:"asset"`
	TaskID   string        `json:"task_id"`
	At       string        `json:"at"`
	Severity string        `json:"severity"` // OV2：triage 严重度，空串 = 未定级
}

// ---- OV2 ----

const (
	overviewCoverageDays = 30
	overviewFailedMax    = 3
)

type OverviewTodo struct {
	Unrated     int `json:"unrated"`
	PendingPush int `json:"pending_push"`
	Overdue     int `json:"overdue"`
}

type OverviewOverdueVuln struct {
	VulnKey        string        `json:"vuln_key"`
	Title          string        `json:"title"`
	Asset          *TaskAssetRef `json:"asset"`
	OwnerEffective string        `json:"owner_effective"`
	OverdueDays    int           `json:"overdue_days"`
}

type OverviewSLA struct {
	Rate         *float64              `json:"rate"`
	ClosedOnTime int                   `json:"closed_on_time"`
	ClosedTotal  int                   `json:"closed_total"`
	AvgFixDays   map[string]*float64   `json:"avg_fix_days"`
	OverdueTop   []OverviewOverdueVuln `json:"overdue_top"`
}

type OverviewStaleAsset struct {
	AssetID      string  `json:"asset_id"`
	Name         string  `json:"name"`
	LastTestedAt *string `json:"last_tested_at"`
	DaysSince    *int    `json:"days_since"`
}

type OverviewCoverage struct {
	WindowDays int                  `json:"window_days"`
	Tested     int                  `json:"tested"`
	Total      int                  `json:"total"`
	StaleTop   []OverviewStaleAsset `json:"stale_top"`
}

type OverviewRunningBoard struct {
	Endpoints        int64 `json:"endpoints"`
	CoveredEndpoints int64 `json:"covered_endpoints"`
}

type OverviewRunningTask struct {
	TaskID         string               `json:"task_id"`
	Title          string               `json:"title"`
	Asset          *TaskAssetRef        `json:"asset"`
	StartedAt      string               `json:"started_at"`
	ElapsedSeconds int64                `json:"elapsed_seconds"`
	Board          OverviewRunningBoard `json:"board"`
}

type OverviewFailedRun struct {
	AssetID      string `json:"asset_id"`
	AssetName    string `json:"asset_name"`
	ScheduledFor string `json:"scheduled_for"`
	Status       string `json:"status"`
	Error        string `json:"error"`
}

type Overview struct {
	GeneratedAt string `json:"generated_at"`
	Timezone    string `json:"timezone"`
	Days        int    `json:"days"`
	Assets      struct {
		Total            int `json:"total"`
		Archived         int `json:"archived"`
		ScheduledEnabled int `json:"scheduled_enabled"`
	} `json:"assets"`
	Tasks struct {
		Total          int                `json:"total"`
		Running        int                `json:"running"`
		CreatedInRange int                `json:"created_in_range"`
		ByDay          []OverviewDayCount `json:"by_day"`
	} `json:"tasks"`
	Vulns struct {
		Counts         map[string]int    `json:"counts"`
		OpenBySeverity map[string]int    `json:"open_by_severity"`
		Overdue        int               `json:"overdue"`
		ConfirmedByDay []OverviewVulnDay `json:"confirmed_by_day"`
	} `json:"vulns"`
	Tokens struct {
		Total uint64             `json:"total"`
		ByDay []OverviewTokenDay `json:"by_day"`
	} `json:"tokens"`
	Schedules struct {
		Next       []OverviewNextRun   `json:"next"`
		RecentRuns []OverviewRecentRun `json:"recent_runs"`
	} `json:"schedules"`
	RecentSupported []OverviewSupported `json:"recent_supported"`

	// OV2（只加字段）
	Todo             OverviewTodo          `json:"todo"`
	SLA              OverviewSLA           `json:"sla"`
	Coverage         OverviewCoverage      `json:"coverage"`
	Running          []OverviewRunningTask `json:"running"`
	RecentFailedRuns []OverviewFailedRun   `json:"recent_failed_runs"`
}

// overviewRange 返回本地日期区间 [start, end)：今天往前 days 天（含今天）及按序日期列表。
func overviewRange(now time.Time, days int) (time.Time, time.Time, []string) {
	local := now.In(time.Local)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	start := today.AddDate(0, 0, -(days - 1))
	dates := make([]string, 0, days)
	for day := 0; day < days; day++ {
		dates = append(dates, start.AddDate(0, 0, day).Format("2006-01-02"))
	}
	return start, today.AddDate(0, 0, 1), dates
}

// localDay 返回时间的本地日期；不在 [start,end) 内返回空串。
func localDay(at, start, end time.Time) string {
	if at.IsZero() || at.Before(start) || !at.Before(end) {
		return ""
	}
	return at.In(time.Local).Format("2006-01-02")
}

func (c *Capability) buildOverview(ctx context.Context, days int) (*Overview, error) {
	now := c.scheduleClock()
	start, end, dates := overviewRange(now, days)
	view := &Overview{GeneratedAt: now.UTC().Format(time.RFC3339), Timezone: localZoneName(), Days: days}

	// 资产
	err := c.store.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(archived), 0),
		(SELECT COUNT(*) FROM webapi_asset_schedules s JOIN webapi_assets a ON a.id = s.asset_id WHERE s.enabled = 1 AND a.archived = 0)
		FROM webapi_assets`).Scan(&view.Assets.Total, &view.Assets.Archived, &view.Assets.ScheduledEnabled)
	if err != nil {
		return nil, err
	}

	// 任务
	rows, err := c.store.taskListRows(ctx, "")
	if err != nil {
		return nil, err
	}
	taskDays := map[string]int{}
	view.Tasks.Total = len(rows)
	runningRows := []*taskListRow{}
	for _, row := range rows {
		if c.taskRunStatus(ctx, row) == TaskStatusRunning {
			view.Tasks.Running++
			runningRows = append(runningRows, row)
		}
		if day := localDay(row.createdAt, start, end); day != "" {
			view.Tasks.CreatedInRange++
			taskDays[day]++
		}
	}
	view.Tasks.ByDay = make([]OverviewDayCount, 0, len(dates))
	for _, date := range dates {
		view.Tasks.ByDay = append(view.Tasks.ByDay, OverviewDayCount{Date: date, Count: taskDays[date]})
	}

	// 漏洞
	vulns, err := c.globalVulns(ctx, "")
	if err != nil {
		return nil, err
	}
	triage, err := c.loadTriageContext(ctx)
	if err != nil {
		return nil, err
	}
	view.Vulns.Counts = newVulnCounts()
	view.Vulns.OpenBySeverity = newSeverityCounts()
	type vulnDay struct{ new, present, fixed map[string]bool }
	perDay := map[string]*vulnDay{}
	mark := func(date, kind, vulnID string) {
		if perDay[date] == nil {
			perDay[date] = &vulnDay{new: map[string]bool{}, present: map[string]bool{}, fixed: map[string]bool{}}
		}
		switch kind {
		case VulnNew:
			perDay[date].new[vulnID] = true
		case VulnPresent:
			perDay[date].present[vulnID] = true
		case VulnFixed:
			perDay[date].fixed[vulnID] = true
		}
	}
	supported := []OverviewSupported{}
	sla := newOverviewSLA()
	for _, vuln := range vulns {
		view.Vulns.Counts[vuln.Status]++
		// 待修复（new/present 且处置 open/fixing）的严重度分布与逾期数（V1）。
		item := triage.view(vuln.ID, vuln.assetID, vuln.Status, vuln.firstConfirmed)
		if triageOpen(vuln.Status, item.Disposition) {
			view.Vulns.OpenBySeverity[severityBucket(item.Severity)]++
			if item.Severity == "" {
				view.Todo.Unrated++
			}
			if item.Overdue {
				view.Vulns.Overdue++
			}
		}
		sla.add(vuln, item, triage, start, end)
		events := append([]vulnEvent(nil), vuln.events...)
		sort.Slice(events, func(i, j int) bool { return eventLater(events[j], events[i]) })
		firstSupported := -1
		var latestSupported *vulnEvent
		for index := range events {
			event := &events[index]
			if event.Kind == FollowupVerify && event.Value == "supported" {
				if firstSupported < 0 {
					firstSupported = index
				}
				latestSupported = event
			}
		}
		for index, event := range events {
			date := localDay(event.at, start, end)
			if date == "" {
				continue
			}
			present, fixed := decisive(event)
			switch {
			case index == firstSupported:
				mark(date, VulnNew, vuln.ID)
			case present:
				mark(date, VulnPresent, vuln.ID)
			case fixed:
				mark(date, VulnFixed, vuln.ID)
			}
		}
		if latestSupported != nil {
			supported = append(supported, OverviewSupported{VulnID: vuln.ID, Title: vuln.Title, Asset: vuln.Asset, TaskID: vuln.findingTasks[latestSupported.FindingID], At: latestSupported.At, Severity: item.Severity})
		}
	}
	view.SLA = sla.finish()
	view.Todo.Overdue = view.Vulns.Overdue
	view.Vulns.ConfirmedByDay = make([]OverviewVulnDay, 0, len(dates))
	for _, date := range dates {
		item := OverviewVulnDay{Date: date}
		if counts := perDay[date]; counts != nil {
			item.New, item.Present, item.Fixed = len(counts.new), len(counts.present), len(counts.fixed)
		}
		view.Vulns.ConfirmedByDay = append(view.Vulns.ConfirmedByDay, item)
	}
	sort.SliceStable(supported, func(i, j int) bool {
		return laterThan(parseAt(supported[i].At), supported[i].At+supported[i].VulnID, parseAt(supported[j].At), supported[j].At+supported[j].VulnID)
	})
	if len(supported) > overviewListMax {
		supported = supported[:overviewListMax]
	}
	view.RecentSupported = supported

	// token：复用 USAGE 全局汇总（本地日期闭区间）
	usage, err := c.store.Usage(ctx, "", start, end.AddDate(0, 0, -1))
	if err != nil {
		return nil, err
	}
	view.Tokens.Total = usage.Totals.TotalTokens
	tokenDays := map[string]uint64{}
	for _, day := range usage.ByDay {
		tokenDays[day.Day] = day.TotalTokens
	}
	view.Tokens.ByDay = make([]OverviewTokenDay, 0, len(dates))
	for _, date := range dates {
		view.Tokens.ByDay = append(view.Tokens.ByDay, OverviewTokenDay{Date: date, TotalTokens: tokenDays[date]})
	}

	// 计划
	schedules, err := c.store.listSchedules(ctx)
	if err != nil {
		return nil, err
	}
	view.Schedules.Next = []OverviewNextRun{}
	for _, row := range schedules {
		if row.Enabled && !row.AssetArchived && row.NextRunAt != "" && len(view.Schedules.Next) < overviewListMax {
			view.Schedules.Next = append(view.Schedules.Next, OverviewNextRun{AssetID: row.AssetID, AssetName: row.AssetName, NextRunAt: row.NextRunAt})
		}
	}
	view.Schedules.RecentRuns, err = c.recentScheduleRuns(ctx)
	if err != nil {
		return nil, err
	}

	// OV2：待推送、资产覆盖、运行中任务、最近失败运行
	if err := c.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webapi_notifications WHERE status = ?`, NotifyPendingConfirm).Scan(&view.Todo.PendingPush); err != nil {
		return nil, err
	}
	if view.Coverage, err = c.overviewCoverage(ctx, now); err != nil {
		return nil, err
	}
	if view.Running, err = c.overviewRunning(ctx, now, runningRows); err != nil {
		return nil, err
	}
	if view.RecentFailedRuns, err = c.recentFailedRuns(ctx); err != nil {
		return nil, err
	}
	return view, nil
}

// recentScheduleRuns：全部资产最近 5 次计划运行（scheduled_for 倒序），带后续状态（非 launched 为 null）。
func (c *Capability) recentScheduleRuns(ctx context.Context) ([]OverviewRecentRun, error) {
	var models []scheduleRunModel
	if err := c.store.orm.NewSelect().Model(&models).Order("scheduled_for DESC", "id DESC").Limit(overviewListMax).Scan(ctx); err != nil {
		return nil, err
	}
	names := map[string]string{}
	rows, err := c.store.db.QueryContext(ctx, `SELECT id, name FROM webapi_assets`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = name
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	out := make([]OverviewRecentRun, 0, len(models))
	for _, model := range models {
		item := OverviewRecentRun{AssetID: model.AssetID, AssetName: names[model.AssetID], ScheduledFor: model.ScheduledFor, Status: model.Status, TaskID: model.TaskID}
		summary, err := c.followupSummary(ctx, model)
		if err != nil {
			return nil, err
		}
		if summary != nil {
			state := summary.State
			item.FollowupsState = &state
		}
		out = append(out, item)
	}
	return out, nil
}

// handleOverview: GET /api/webapi/overview?days=7|30|90（默认 30）
func (c *Capability) handleOverview(w http.ResponseWriter, r *http.Request) {
	days := 30
	switch raw := r.URL.Query().Get("days"); raw {
	case "":
	case "7":
		days = 7
	case "30":
		days = 30
	case "90":
		days = 90
	default:
		writeError(w, http.StatusBadRequest, "days 只能是 7、30、90")
		return
	}
	view, err := c.buildOverview(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取总览失败")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// ---- OV2 helpers ----

var overviewFixSeverities = []string{SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow}

// overviewSLAAcc 累计区间内已修复条目（准时率、按等级平均修复天数）与当前逾期条目。
type overviewSLAAcc struct {
	onTime, total int
	fixSum        map[string]float64
	fixN          map[string]int
	overdue       []OverviewOverdueVuln
}

func newOverviewSLA() *overviewSLAAcc {
	return &overviewSLAAcc{fixSum: map[string]float64{}, fixN: map[string]int{}}
}

func (a *overviewSLAAcc) add(vuln *GlobalVuln, item *TriageView, triage *triageContext, start, end time.Time) {
	if item.Overdue && item.DueEffective != nil {
		a.overdue = append(a.overdue, OverviewOverdueVuln{VulnKey: vuln.ID, Title: vuln.Title, Asset: vuln.Asset, OwnerEffective: item.OwnerEffective,
			OverdueDays: dateDiffDays(*item.DueEffective, triage.today)})
	}
	if vuln.Status != VulnFixed || vuln.FixedAt == nil {
		return
	}
	fixedAt := parseAt(*vuln.FixedAt)
	if localDay(fixedAt, start, end) == "" {
		return
	}
	if item.DueEffective != nil {
		a.total++
		if fixedAt.In(triage.loc).Format(triageDateLayout) <= *item.DueEffective {
			a.onTime++
		}
	}
	if !vuln.firstConfirmed.IsZero() && slices.Contains(overviewFixSeverities, item.Severity) {
		a.fixSum[item.Severity] += fixedAt.Sub(vuln.firstConfirmed).Hours() / 24
		a.fixN[item.Severity]++
	}
}

func (a *overviewSLAAcc) finish() OverviewSLA {
	out := OverviewSLA{ClosedOnTime: a.onTime, ClosedTotal: a.total, AvgFixDays: map[string]*float64{}, OverdueTop: a.overdue}
	if a.total > 0 {
		rate := float64(a.onTime) / float64(a.total)
		out.Rate = &rate
	}
	for _, severity := range overviewFixSeverities {
		out.AvgFixDays[severity] = nil
		if n := a.fixN[severity]; n > 0 {
			avg := math.Round(a.fixSum[severity]/float64(n)*10) / 10
			out.AvgFixDays[severity] = &avg
		}
	}
	if out.OverdueTop == nil {
		out.OverdueTop = []OverviewOverdueVuln{}
	}
	sort.SliceStable(out.OverdueTop, func(i, j int) bool {
		if out.OverdueTop[i].OverdueDays != out.OverdueTop[j].OverdueDays {
			return out.OverdueTop[i].OverdueDays > out.OverdueTop[j].OverdueDays
		}
		return out.OverdueTop[i].VulnKey < out.OverdueTop[j].VulnKey
	})
	if len(out.OverdueTop) > overviewListMax {
		out.OverdueTop = out.OverdueTop[:overviewListMax]
	}
	return out
}

// dateDiffDays 返回两个 YYYY-MM-DD 之间的天数（to − from）；解析失败为 0。
func dateDiffDays(from, to string) int {
	a, errA := time.Parse(triageDateLayout, from)
	b, errB := time.Parse(triageDateLayout, to)
	if errA != nil || errB != nil {
		return 0
	}
	return int(math.Round(b.Sub(a).Hours() / 24))
}

// overviewCoverage：未归档资产中，近 30 个本地日（含今天）内有关联任务创建即已测；
// stale_top 为最久未测（从未测过的在前，按资产创建时间早者优先），再按最近测试时间早者优先。
func (c *Capability) overviewCoverage(ctx context.Context, now time.Time) (OverviewCoverage, error) {
	out := OverviewCoverage{WindowDays: overviewCoverageDays, StaleTop: []OverviewStaleAsset{}}
	start, _, _ := overviewRange(now, overviewCoverageDays)
	type linkAt struct {
		raw string
		at  time.Time
	}
	latest := map[string]linkAt{}
	rows, err := c.store.db.QueryContext(ctx, `SELECT asset_id, created_at FROM webapi_asset_tasks`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var assetID, createdAt string
		if err := rows.Scan(&assetID, &createdAt); err != nil {
			rows.Close()
			return out, err
		}
		at := parseAt(createdAt)
		if at.IsZero() {
			continue
		}
		if prev, ok := latest[assetID]; !ok || at.After(prev.at) {
			latest[assetID] = linkAt{raw: createdAt, at: at}
		}
	}
	if err := closeRows(rows); err != nil {
		return out, err
	}
	type assetRow struct {
		id, name  string
		createdAt time.Time
		last      *linkAt
	}
	assets := []assetRow{}
	rows, err = c.store.db.QueryContext(ctx, `SELECT id, name, created_at FROM webapi_assets WHERE archived = 0 ORDER BY id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var row assetRow
		var createdAt string
		if err := rows.Scan(&row.id, &row.name, &createdAt); err != nil {
			rows.Close()
			return out, err
		}
		row.createdAt = parseAt(createdAt)
		if last, ok := latest[row.id]; ok {
			row.last = &last
		}
		assets = append(assets, row)
	}
	if err := closeRows(rows); err != nil {
		return out, err
	}
	out.Total = len(assets)
	for _, row := range assets {
		if row.last != nil && !row.last.at.Before(start) {
			out.Tested++
		}
	}
	sort.SliceStable(assets, func(i, j int) bool {
		a, b := assets[i], assets[j]
		if (a.last == nil) != (b.last == nil) {
			return a.last == nil
		}
		if a.last == nil {
			if !a.createdAt.Equal(b.createdAt) {
				return a.createdAt.Before(b.createdAt)
			}
			return a.id < b.id
		}
		if !a.last.at.Equal(b.last.at) {
			return a.last.at.Before(b.last.at)
		}
		return a.id < b.id
	})
	today := now.In(time.Local).Format(triageDateLayout)
	for _, row := range assets {
		if len(out.StaleTop) >= overviewListMax {
			break
		}
		item := OverviewStaleAsset{AssetID: row.id, Name: row.name}
		if row.last != nil {
			raw, days := row.last.raw, dateDiffDays(row.last.at.In(time.Local).Format(triageDateLayout), today)
			item.LastTestedAt, item.DaysSince = &raw, &days
		}
		out.StaleTop = append(out.StaleTop, item)
	}
	return out, nil
}

// overviewRunning：运行中任务（同任务列表判定）按开始时间倒序前 5，开始时间 = 研究会话首个带 turn_id 事件，
// 黑板覆盖 = 端点数 − 尚无 Oracle 引用的端点数（M2 counts）。
func (c *Capability) overviewRunning(ctx context.Context, now time.Time, rows []*taskListRow) ([]OverviewRunningTask, error) {
	type running struct {
		row     *taskListRow
		raw     string
		started time.Time
	}
	items := make([]running, 0, len(rows))
	for _, row := range rows {
		item := running{row: row}
		err := c.store.db.QueryRowContext(ctx, `SELECT created_at FROM chat_aop_events WHERE session_id = ? AND turn_id != '' ORDER BY cursor LIMIT 1`,
			row.item.InitialSessionID).Scan(&item.raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		item.started = parseAt(item.raw)
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return laterThan(items[i].started, items[i].row.item.TaskID, items[j].started, items[j].row.item.TaskID)
	})
	if len(items) > overviewListMax {
		items = items[:overviewListMax]
	}
	out := make([]OverviewRunningTask, 0, len(items))
	for _, item := range items {
		task := OverviewRunningTask{TaskID: item.row.item.TaskID, Title: item.row.item.Title, Asset: item.row.item.Asset, StartedAt: item.raw}
		if !item.started.IsZero() {
			task.StartedAt = item.started.UTC().Format(time.RFC3339)
			if elapsed := now.Sub(item.started); elapsed > 0 {
				task.ElapsedSeconds = int64(elapsed / time.Second)
			}
		}
		counts, err := c.store.boardCounts(ctx, task.TaskID)
		if err != nil {
			return nil, err
		}
		task.Board = OverviewRunningBoard{Endpoints: counts.Endpoints, CoveredEndpoints: counts.Endpoints - counts.UntestedEndpoints}
		out = append(out, task)
	}
	return out, nil
}

// recentFailedRuns：运行本身 failed 或有 failed 后续的计划运行，按 scheduled_for 倒序前 3；
// error 取运行的错误，运行未失败时取首个失败后续（创建时间最早）的错误。
func (c *Capability) recentFailedRuns(ctx context.Context) ([]OverviewFailedRun, error) {
	rows, err := c.store.db.QueryContext(ctx, `SELECT r.id, r.asset_id, COALESCE(a.name, ''), r.scheduled_for, r.status, r.error
		FROM webapi_schedule_runs r LEFT JOIN webapi_assets a ON a.id = r.asset_id
		WHERE r.status = ? OR EXISTS (SELECT 1 FROM webapi_schedule_followups f WHERE f.run_id = r.id AND f.status = ?)
		ORDER BY r.scheduled_for DESC, r.id DESC LIMIT ?`, ScheduleRunFailed, FollowupFailed, overviewFailedMax)
	if err != nil {
		return nil, err
	}
	type failedRow struct {
		id  string
		run OverviewFailedRun
	}
	found := []failedRow{}
	for rows.Next() {
		var item failedRow
		if err := rows.Scan(&item.id, &item.run.AssetID, &item.run.AssetName, &item.run.ScheduledFor, &item.run.Status, &item.run.Error); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, item)
	}
	if err := closeRows(rows); err != nil {
		return nil, err
	}
	out := make([]OverviewFailedRun, 0, len(found))
	for _, item := range found {
		if item.run.Status != ScheduleRunFailed {
			err := c.store.db.QueryRowContext(ctx, `SELECT error FROM webapi_schedule_followups WHERE run_id = ? AND status = ? ORDER BY created_at, id LIMIT 1`,
				item.id, FollowupFailed).Scan(&item.run.Error)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		out = append(out, item.run)
	}
	return out, nil
}
