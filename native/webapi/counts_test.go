package webapi

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

type countQueries struct{ n int }

func (q *countQueries) BeforeQuery(ctx context.Context, e *bun.QueryEvent) context.Context {
	q.n++
	return ctx
}
func (q *countQueries) AfterQuery(context.Context, *bun.QueryEvent) {}

func TestTaskListUsageCounts(t *testing.T) {
	f := newGlobalFixture(t)
	at := "2026-11-18T04:00:00Z"
	for _, task := range []string{"one", "two"} {
		f.task(task, "", task, "", "https://example.test", task+"-research", at)
		f.finding(task, task, task, "GET", "https://example.test/demo", at)
		f.verify(task+"-verify", task, task, task+"-verification", "supported", at)
		f.event(task+"-research", at, "turn", usageEvent("demo", 100, 20, 0, 0, 0))
		f.event(task+"-verification", at, "turn", usageEvent("demo", 50, 10, 80, 0, 0))
		f.session(task + "-sub")
		f.exec(`INSERT INTO webapi_task_subsessions (session_id, task_id, parent_session_id, parent_call_id, oracle_id, agent_name, status, stop_reason, error, created_at) VALUES (?, ?, ?, 'call', 'O-001', 'oracle_explorer', 'ended', '', '', ?)`, task+"-sub", task, task+"-research", at)
		f.exec(`INSERT INTO webapi_subsession_usage (id, session_id, task_id, model, input_tokens, output_tokens, total_tokens, cache_read_tokens, cache_write_tokens, created_at) VALUES (?, ?, ?, 'demo', 200, 30, 0, 0, 0, ?)`, task+"-usage", task+"-sub", task, at)
		// 子会话的 Hub 事件即使存在也不能重复计入。
		f.event(task+"-sub", at, "turn", usageEvent("demo", 9000, 0, 9000, 0, 0))
		f.event(task+"-research", at, "bad-time", usageEvent("demo", 9000, 0, 9000, 0, 0))
		f.exec(`UPDATE chat_aop_events SET created_at = 'broken' WHERE session_id = ? AND turn_id = 'bad-time'`, task+"-research")
		f.event(task+"-research", at, "bad-proto", usageEvent("demo", 9000, 0, 9000, 0, 0))
		f.exec(`UPDATE chat_aop_events SET event_proto = ? WHERE session_id = ? AND turn_id = 'bad-proto'`, []byte{0xff}, task+"-research")
		f.exec(`INSERT INTO webapi_subsession_usage (id, session_id, task_id, model, input_tokens, output_tokens, total_tokens, cache_read_tokens, cache_write_tokens, created_at) VALUES (?, ?, ?, 'demo', 9000, 0, 9000, 0, 0, 'broken')`, task+"-bad-sub", task+"-sub", task)
	}
	var list taskListResponse
	f.getJSON("/api/webapi/task-list", &list)
	if len(list.Items) != 2 {
		t.Fatalf("items = %+v", list)
	}
	for _, item := range list.Items {
		var usage UsageReport
		f.getJSON("/api/webapi/usage?task_id="+item.TaskID, &usage)
		if item.TotalTokens != 430 || item.TotalTokens != usage.Totals.TotalTokens || usage.SkippedEvents != 3 {
			t.Fatalf("list=%+v usage=%+v", item, usage)
		}
	}
	rows, err := f.cap.store.taskListRows(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	hook := &countQueries{}
	f.cap.store.orm.AddQueryHook(hook)
	if err := f.cap.store.fillTaskListPage(context.Background(), rows[:1]); err != nil {
		t.Fatal(err)
	}
	one := hook.n
	hook.n = 0
	if err := f.cap.store.fillTaskListPage(context.Background(), rows); err != nil {
		t.Fatal(err)
	}
	if hook.n != one {
		t.Fatalf("page queries grew: one=%d two=%d", one, hook.n)
	}
}

func TestAssetOverdueCounts(t *testing.T) {
	f := newGlobalFixture(t)
	// UTC 11-18、本地 11-19：昨天到期已逾期，今天到期不逾期。
	f.cap.scheduleNow = func() time.Time { return time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC) }
	at := "2026-10-01T04:00:00Z"
	for _, id := range []string{"zero", "one", "many", "archived"} {
		status, raw := f.do("PUT", "/api/webapi/assets/"+id, fmt.Sprintf(`{"name":%q,"origins":["https://example.test"],"archived":%t}`, id, id == "archived"))
		if status != 201 {
			t.Fatalf("asset: %d %s", status, raw)
		}
		f.task(id, id, id, "", "https://example.test", id+"-research", at)
	}
	add := func(name, task, disposition, due string, fixed bool) {
		t.Helper()
		endpoint := "https://example.test/" + name
		f.finding(name, task, name, "GET", endpoint, at)
		f.verify(name+"-v", task, name, name+"-vs", "supported", at)
		key := assetVulnID(task, "GET "+endpoint)
		if task == "standalone" {
			key = "finding:wf_" + name
		}
		if err := f.cap.store.putTriageRecord(context.Background(), &vulnTriageModel{VulnKey: key, Severity: SeverityHigh, Disposition: disposition, DueDate: due, UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
		if fixed {
			f.retest(name+"-r", task, name, name+"-rs", "fixed_in_scope", "2026-11-01T00:00:00Z")
		}
	}
	add("single", "one", DispositionOpen, "2026-11-18", false)
	add("multi-a", "many", DispositionOpen, "2026-11-18", false)
	add("multi-b", "many", DispositionFixing, "", false) // 默认 high SLA，从最早存在事件起算
	add("fixed", "many", DispositionOpen, "2026-11-18", true)
	add("false-positive", "many", DispositionFalsePositive, "2026-11-18", false)
	add("future", "many", DispositionOpen, "2026-11-20", false)
	add("today", "many", DispositionOpen, "2026-11-19", false)
	add("accepted", "many", DispositionRiskAccepted, "2026-11-18", false)
	getAssets := func(archived bool) (map[string]int, int) {
		var response struct {
			Assets []struct {
				ID      string `json:"id"`
				Overdue *int   `json:"overdue_vulns"`
			} `json:"assets"`
		}
		path := "/api/webapi/assets"
		if archived {
			path += "?archived=1"
		}
		f.getJSON(path, &response)
		counts, total := map[string]int{}, 0
		for _, item := range response.Assets {
			if item.Overdue == nil {
				t.Fatal("missing overdue_vulns")
			}
			counts[item.ID] = *item.Overdue
			total += *item.Overdue
		}
		return counts, total
	}
	counts, sum := getAssets(false) // 总览尚未同步入库，列表也须给出正确结果。
	if !reflect.DeepEqual(counts, map[string]int{"zero": 0, "one": 1, "many": 2}) {
		t.Fatalf("counts=%v", counts)
	}
	var overview Overview
	f.getJSON("/api/webapi/overview", &overview)
	if sum != overview.Vulns.Overdue {
		t.Fatalf("sum=%d overview=%d", sum, overview.Vulns.Overdue)
	}
	// 同端点第二成员仍是一条漏洞（含未同步成员）。
	f.finding("duplicate", "one", "duplicate", "GET", "https://example.test/single", at)
	f.verify("duplicate-v", "one", "duplicate", "duplicate-vs", "supported", at)
	if _, got := getAssets(false); got != sum {
		t.Fatalf("duplicate counted: %d", got)
	}
	// 第二组：归档自身计数，无资产漏洞只进入总览。
	add("archived-vuln", "archived", DispositionOpen, "2026-11-18", false)
	f.task("standalone", "", "standalone", "", "https://example.test", "standalone-research", at)
	add("standalone-vuln", "standalone", DispositionOpen, "2026-11-18", false)
	archived, archivedSum := getAssets(true)
	if archivedSum != 1 || archived["archived"] != 1 {
		t.Fatalf("archived=%v", archived)
	}
	_, sum = getAssets(false)
	f.getJSON("/api/webapi/overview", &overview)
	if overview.Vulns.Overdue != sum+1 {
		t.Fatalf("active=%d standalone=1 overview=%d", sum, overview.Vulns.Overdue)
	}
	// 常数查询：1 个资产和多个资产的计数查询数相同。
	assets, err := f.cap.store.listAssets(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	hook := &countQueries{}
	f.cap.store.orm.AddQueryHook(hook)
	if _, err := f.cap.assetOverdueCounts(context.Background(), assets[:1]); err != nil {
		t.Fatal(err)
	}
	one := hook.n
	hook.n = 0
	if _, err := f.cap.assetOverdueCounts(context.Background(), assets); err != nil {
		t.Fatal(err)
	}
	if hook.n != one {
		t.Fatalf("asset queries grew: %d -> %d", one, hook.n)
	}
}

func TestNotificationStatusCounts(t *testing.T) {
	f := newGlobalFixture(t)
	type response struct {
		Items  []NotificationView `json:"items"`
		Counts map[string]int     `json:"status_counts"`
		Next   string             `json:"next_cursor"`
	}
	var empty response
	f.getJSON("/api/webapi/notifications", &empty)
	if len(empty.Counts) != 7 {
		t.Fatalf("missing zero keys: %v", empty.Counts)
	}
	for _, n := range empty.Counts {
		if n != 0 {
			t.Fatal(empty.Counts)
		}
	}
	want := map[string]int{"all": 7}
	for status := range notifyStatuses {
		n := 1
		if status == NotifySent {
			n = 2
		}
		want[status] = n
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("%s-%d", status, i)
			f.exec(`INSERT INTO webapi_notifications (id,dedupe_key,kind,task_id,finding_id,verification_id,vuln_id,status,attempts,last_error,created_at) VALUES (?,?,'weekly_digest','','','','',?,0,'','2026-11-18T00:00:00.000Z')`, id, id, status)
		}
	}
	var first, second response
	f.getJSON("/api/webapi/notifications?status=sent&limit=1", &first)
	if len(first.Items) != 1 || first.Items[0].Status != NotifySent || first.Next == "" {
		t.Fatalf("first=%+v", first)
	}
	f.getJSON("/api/webapi/notifications?status=sent&limit=1&cursor="+url.QueryEscape(first.Next), &second)
	for _, r := range []response{first, second} {
		if !reflect.DeepEqual(r.Counts, want) {
			t.Fatalf("counts=%v want=%v", r.Counts, want)
		}
	}
	if len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second=%+v", second)
	}
	var full response
	f.getJSON("/api/webapi/notifications", &full)
	if len(full.Items) != 7 || !reflect.DeepEqual(full.Counts, want) {
		t.Fatalf("full=%+v", full)
	}
}
