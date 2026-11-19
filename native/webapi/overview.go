package webapi

import (
	"context"
	"net/http"
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
	VulnID string        `json:"vuln_id"`
	Title  string        `json:"title"`
	Asset  *TaskAssetRef `json:"asset"`
	TaskID string        `json:"task_id"`
	At     string        `json:"at"`
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
	for _, row := range rows {
		if c.taskRunStatus(ctx, row) == TaskStatusRunning {
			view.Tasks.Running++
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
	view.Vulns.Counts = newVulnCounts()
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
	for _, vuln := range vulns {
		view.Vulns.Counts[vuln.Status]++
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
			supported = append(supported, OverviewSupported{VulnID: vuln.ID, Title: vuln.Title, Asset: vuln.Asset, TaskID: vuln.findingTasks[latestSupported.FindingID], At: latestSupported.At})
		}
	}
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
