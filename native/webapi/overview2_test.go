package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// OV2 总览新增字段测试：全部为临时库合成数据（example.test 演示资产、虚构候选/负责人/错误文案）。
// globalFixture：进程时区 UTC+8，时钟固定 2026-11-19T04:00Z（本地 11-19 12:00）。

func (f *globalFixture) overview(days string) (Overview, map[string]json.RawMessage) {
	f.t.Helper()
	status, raw := f.do("GET", "/api/webapi/overview"+days, "")
	if status != 200 {
		f.t.Fatalf("overview = %d %s", status, raw)
	}
	var view Overview
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		f.t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		f.t.Fatal(err)
	}
	return view, fields
}

func (f *globalFixture) insertModel(model any) {
	f.t.Helper()
	if _, err := f.cap.store.orm.NewInsert().Model(model).Exec(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func avgText(values map[string]*float64) string {
	parts := []string{}
	for _, severity := range overviewFixSeverities {
		if value := values[severity]; value != nil {
			parts = append(parts, fmt.Sprintf("%s=%.1f", severity, *value))
		} else {
			parts = append(parts, severity+"=null")
		}
	}
	return strings.Join(parts, ",")
}

func TestOverviewTodoAndSLA(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()
	// 额外无资产候选：F5 误报、F6 接受风险（均未定级 new）；F7 在区间内修复但超期；F8 修复在 7 天区间外、30 天区间内。
	f.finding("F5", "T3", "演示误报", "GET", "https://api.example.test/a", "2026-11-18T18:40:00Z")
	f.finding("F6", "T3", "演示接受风险", "GET", "https://api.example.test/b", "2026-11-18T18:41:00Z")
	f.finding("F7", "T3", "演示超期修复", "GET", "https://api.example.test/c", "2026-11-09T00:00:00Z")
	f.finding("F8", "T3", "演示早期修复", "GET", "https://api.example.test/d", "2026-10-31T00:00:00Z")
	f.verify("v5", "T3", "F5", "s-v5", "supported", "2026-11-10T00:00:00Z")
	f.verify("v6", "T3", "F6", "s-v6", "supported", "2026-11-10T00:30:00Z")
	f.verify("v7", "T3", "F7", "s-v7", "supported", "2026-11-10T00:00:00Z")
	f.retest("r7", "T3", "F7", "s-r7", "fixed_in_scope", "2026-11-18T00:00:00Z")
	f.verify("v8", "T3", "F8", "s-v8", "supported", "2026-11-01T00:00:00Z")
	f.retest("r8", "T3", "F8", "s-r8", "fixed_in_scope", "2026-11-05T00:00:00Z")

	// 基线：未定级待修复 = orders、F3、F5、F6；无期限样本 → rate null、avg 全 null。
	view, fields := f.overview("?days=7")
	if view.Todo != (OverviewTodo{Unrated: 4}) {
		t.Fatalf("baseline todo = %+v", view.Todo)
	}
	if view.SLA.Rate != nil || view.SLA.ClosedTotal != 0 || avgText(view.SLA.AvgFixDays) != "critical=null,high=null,medium=null,low=null" || len(view.SLA.OverdueTop) != 0 {
		t.Fatalf("baseline sla = %+v", view.SLA)
	}
	var rawSLA map[string]json.RawMessage
	_ = json.Unmarshal(fields["sla"], &rawSLA)
	if string(rawSLA["rate"]) != "null" || string(rawSLA["overdue_top"]) != "[]" {
		t.Fatalf("baseline sla json = %s", fields["sla"])
	}
	for _, key := range []string{"vulns", "tasks", "assets", "tokens", "schedules", "recent_supported", "todo", "coverage", "running", "recent_failed_runs"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %s", key)
		}
	}

	f.mustTriage("finding:wf_F5", `{"disposition":"false_positive"}`)
	f.mustTriage("finding:wf_F6", `{"disposition":"risk_accepted"}`)
	f.mustTriage(ordersKey, `{"severity":"critical","due_date":"2026-11-10","owner":"安全组"}`)
	f.mustTriage("finding:wf_F3", `{"severity":"medium","due_date":"2026-11-15","disposition":"fixing"}`)
	f.mustTriage(loginKey, `{"severity":"high"}`)            // 首次存在本地 11-12 → 期限 11-19，本地 11-19 修复 → 准时
	f.mustTriage("finding:wf_F7", `{"severity":"critical"}`) // 首次存在本地 11-10 → 期限 11-13，本地 11-18 修复 → 超期
	f.mustTriage("finding:wf_F8", `{"severity":"high"}`)     // 本地 11-05 修复：7 天区间外
	for index := 0; index < 3; index++ {
		status := NotifyPendingConfirm
		if index == 2 {
			status = NotifyQueued
		}
		f.insertModel(&notificationModel{ID: fmt.Sprintf("wn_demo_%d", index), DedupeKey: fmt.Sprintf("finding:demo-%d", index), Kind: NotifyKindNew,
			TaskID: "T3", FindingID: "wf_F3", VerificationID: "v3", Status: status, CreatedAt: "2026-11-19T01:00:00Z"})
	}

	view, _ = f.overview("?days=7")
	list := f.globalVulnList("")
	if view.Todo != (OverviewTodo{Unrated: 0, PendingPush: 2, Overdue: 2}) || fmt.Sprint(list.Counts["overdue"]) != "2" || view.Vulns.Overdue != 2 {
		t.Fatalf("todo = %+v counts = %v", view.Todo, list.Counts)
	}
	sla := view.SLA
	if sla.Rate == nil || *sla.Rate != 0.5 || sla.ClosedOnTime != 1 || sla.ClosedTotal != 2 {
		t.Fatalf("sla = %+v", sla)
	}
	// login：11-12T00 → 11-19T02 = 7.08 天；F7：11-10T00 → 11-18T00 = 8 天。
	if got := avgText(sla.AvgFixDays); got != "critical=8.0,high=7.1,medium=null,low=null" {
		t.Fatalf("avg = %s", got)
	}
	top := sla.OverdueTop
	if len(top) != 2 || top[0].VulnKey != ordersKey || top[0].OverdueDays != 9 || top[0].OwnerEffective != "安全组" || top[0].Asset == nil || top[0].Asset.Name != "演示商城" ||
		top[1].VulnKey != "finding:wf_F3" || top[1].OverdueDays != 4 || top[1].Asset != nil || top[1].Title != "用户资料越权" {
		t.Fatalf("overdue_top = %+v", top)
	}
	severities := map[string]string{}
	for _, item := range view.RecentSupported {
		severities[item.VulnID] = item.Severity
	}
	if severities["finding:wf_F3"] != "medium" || severities[ordersKey] != "critical" || severities[loginKey] != "high" {
		t.Fatalf("recent_supported severity = %v", severities)
	}

	// 30 天：F8（本地 11-01 首次存在 → 期限 11-08，本地 11-05 修复）进入样本 → 准时 2/3；high 平均 (7.08+4)/2 = 5.5。
	month, _ := f.overview("")
	if month.SLA.ClosedTotal != 3 || month.SLA.ClosedOnTime != 2 || avgText(month.SLA.AvgFixDays) != "critical=8.0,high=5.5,medium=null,low=null" {
		t.Fatalf("30d sla = %+v %s", month.SLA, avgText(month.SLA.AvgFixDays))
	}
	// 修复日期等于期限当天算准时；改人工期限早于修复日 → 超期。
	f.mustTriage(loginKey, `{"due_date":"2026-11-18"}`)
	view, _ = f.overview("?days=7")
	if view.SLA.Rate == nil || *view.SLA.Rate != 0 || view.SLA.ClosedOnTime != 0 || view.SLA.ClosedTotal != 2 {
		t.Fatalf("late sla = %+v", view.SLA)
	}
	// 未定级的已修复条目无期限：不进准时率样本，也不进 avg 分组。
	f.mustTriage(loginKey, `{"severity":"","due_date":""}`)
	f.mustTriage("finding:wf_F7", `{"severity":"info"}`)
	view, _ = f.overview("?days=7")
	if view.SLA.Rate != nil || view.SLA.ClosedTotal != 0 || avgText(view.SLA.AvgFixDays) != "critical=null,high=null,medium=null,low=null" {
		t.Fatalf("no-sample sla = %+v", view.SLA)
	}
}

func TestOverviewCoverage(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()
	for _, id := range []string{"A3", "A4", "A5", "A6"} {
		if status, raw := f.do("PUT", "/api/webapi/assets/"+id, `{"name":"演示`+id+`","origins":["https://`+strings.ToLower(id)+`.example.test"]}`); status != 201 {
			t.Fatalf("asset %s = %d %s", id, status, raw)
		}
	}
	f.exec(`UPDATE webapi_assets SET created_at = '2026-02-01T00:00:00Z' WHERE id = 'A3'`)
	f.exec(`UPDATE webapi_assets SET created_at = '2026-03-01T00:00:00Z' WHERE id = 'A4'`)
	// 30 天窗口起点 = 本地 10-21 00:00 = UTC 10-20 16:00。A5 最后一次在窗口前 1 分钟；A6 正好落在起点。
	f.task("T5a", "A5", "演示旧巡检", "", "https://a5.example.test", "s-t5a", "2026-09-01T00:00:00Z")
	f.task("T5b", "A5", "演示巡检", "", "https://a5.example.test", "s-t5b", "2026-10-20T15:59:00Z")
	f.task("T6", "A6", "演示巡检", "", "https://a6.example.test", "s-t6", "2026-10-20T16:00:00Z")

	view, _ := f.overview("?days=7")
	coverage := view.Coverage
	// 未归档：A1、A3、A4、A5、A6（A2 归档不计）；已测 A1（11-10）、A6。
	if coverage.WindowDays != 30 || coverage.Total != 5 || coverage.Tested != 2 {
		t.Fatalf("coverage = %+v", coverage)
	}
	got := []string{}
	for _, item := range coverage.StaleTop {
		last, days := "null", "null"
		if item.LastTestedAt != nil {
			last = *item.LastTestedAt
		}
		if item.DaysSince != nil {
			days = fmt.Sprint(*item.DaysSince)
		}
		got = append(got, item.AssetID+"|"+item.Name+"|"+last+"|"+days)
	}
	want := []string{"A3|演示A3|null|null", "A4|演示A4|null|null", "A5|演示A5|2026-10-20T15:59:00Z|30", "A6|演示A6|2026-10-20T16:00:00Z|29", "A1|演示商城|2026-11-10T00:00:00Z|9"}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("stale_top = %v", got)
	}
}

func TestOverviewRunningAndFailedRuns(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()
	// T1（seed 运行中，首个轮次事件 11-10T00:00:01Z）黑板：3 个端点，1 个 Oracle 引用其中 2 个。
	for index, key := range []string{"GET https://shop.example.test/api/orders/{id}", "GET https://shop.example.test/debug", "POST https://shop.example.test/api/login"} {
		f.insertModel(&boardEndpointModel{ID: fmt.Sprintf("be-%d", index), TaskID: "T1", EndpointKey: key, Method: "GET", Origin: "https://shop.example.test", Path: "/",
			ParamsJSON: "[]", StatusCodesJSON: "[]", Source: "traffic", FirstSeenAt: "2026-11-10T00:00:00Z", LastSeenAt: "2026-11-10T00:00:00Z"})
	}
	f.insertModel(&boardOracleModel{ID: "bo-1", TaskID: "T1", Number: 1, Question: "演示问题", Category: "authz",
		EndpointKeysJSON: `["GET https://shop.example.test/api/orders/{id}","GET https://shop.example.test/debug"]`, Priority: "high", Status: "open",
		Origin: "model", EvidenceIDsJSON: "[]", DedupeKey: "d1", CreatedAt: "2026-11-10T00:00:00Z", UpdatedAt: "2026-11-10T00:00:00Z"})
	// T3：先有无轮次事件，首个带 turn_id 事件 11-19T03:00Z；运行中。T2 有轮次但空闲，不列。
	f.event("s-t3", "2026-11-19T02:00:00Z", "", userMessage())
	f.event("s-t3", "2026-11-19T03:00:00Z", "turn-3", userMessage())
	f.launcher.running["s-t3"] = true

	// 计划运行：run-0（seed，失败）、run-1（seed，launched，补两条失败后续与一条已启动）、run-3 失败、run-2 无失败后续、run-4 更早失败。
	f.exec(`INSERT INTO webapi_schedule_runs (id, asset_id, scheduled_for, task_id, status, error, created_at) VALUES
		('run-2', 'A1', '2026-11-14T00:00:00Z', 'T1', 'launched', '', '2026-11-14T00:00:00Z'),
		('run-3', 'A1', '2026-11-12T00:00:00Z', '', 'failed', '演示：节点离线', '2026-11-12T00:00:00Z'),
		('run-4', 'A1', '2026-11-01T00:00:00Z', '', 'failed', '演示：更早失败', '2026-11-01T00:00:00Z')`)
	for _, item := range []struct{ id, run, finding, status, errText, at string }{
		{"fu-1", "run-1", "wf_F1a", FollowupFailed, "演示：第二个失败", "2026-11-10T02:00:00Z"},
		{"fu-2", "run-1", "wf_F1b", FollowupFailed, "演示：首个失败", "2026-11-10T01:00:00Z"},
		{"fu-3", "run-1", "wf_F1c", FollowupLaunched, "", "2026-11-10T00:30:00Z"},
		{"fu-4", "run-2", "wf_F1a", FollowupLaunched, "", "2026-11-14T01:00:00Z"},
	} {
		f.insertModel(&scheduleFollowupModel{ID: item.id, RunID: item.run, Kind: FollowupVerify, TaskID: "T1", FindingID: item.finding, RevisionID: "wfr_x",
			Status: item.status, Error: item.errText, CreatedAt: item.at, UpdatedAt: item.at})
	}

	view, _ := f.overview("?days=7")
	running := view.Running
	if len(running) != 2 || view.Tasks.Running != 2 {
		t.Fatalf("running = %+v", running)
	}
	if running[0].TaskID != "T3" || running[0].Asset != nil || running[0].Title != "检查 Example 越权" || running[0].StartedAt != "2026-11-19T03:00:00Z" ||
		running[0].ElapsedSeconds != 3600 || running[0].Board != (OverviewRunningBoard{}) {
		t.Fatalf("running[0] = %+v", running[0])
	}
	if running[1].TaskID != "T1" || running[1].Asset == nil || running[1].Asset.Name != "演示商城" || running[1].StartedAt != "2026-11-10T00:00:01Z" ||
		running[1].ElapsedSeconds != int64((9*24+4)*3600-1) || running[1].Board != (OverviewRunningBoard{Endpoints: 3, CoveredEndpoints: 2}) {
		t.Fatalf("running[1] = %+v", running[1])
	}

	got := []string{}
	for _, run := range view.RecentFailedRuns {
		got = append(got, strings.Join([]string{run.AssetID, run.AssetName, run.ScheduledFor, run.Status, run.Error}, "|"))
	}
	want := []string{
		"A1|演示商城|2026-11-12T00:00:00Z|failed|演示：节点离线",
		"A1|演示商城|2026-11-10T00:00:00Z|launched|演示：首个失败",
		"A2|旧系统|2026-11-09T00:00:00Z|failed|select_node：无节点",
	}
	if strings.Join(got, ";") != strings.Join(want, ";") {
		t.Fatalf("recent_failed_runs = %v", got)
	}
}
