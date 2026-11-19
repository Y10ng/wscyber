package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// U2a 全局列表与总览测试：全部为合成数据（example.test 演示资产、虚构候选与用量），
// 进程时区固定 UTC+8，时钟与 SessionRunning 用替身。

type globalFixture struct {
	t        *testing.T
	native   *webservice.SQLiteStore
	cap      *Capability
	mux      *http.ServeMux
	launcher *fakeLauncher
	seq      int
	number   int64
	ids      map[string]string // 名称 → 生成的 finding/revision/verification id
}

func newGlobalFixture(t *testing.T) *globalFixture {
	t.Helper()
	previous := time.Local
	time.Local = time.FixedZone("UTC+8", 8*3600)
	t.Cleanup(func() { time.Local = previous })
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	native, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = native.Close() })
	capability, err := NewCapability(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close(context.Background()) })
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	launcher := newFakeLauncher(productAgent("node-a"))
	capability.launcher = launcher
	now, _ := time.Parse(time.RFC3339, "2026-11-19T04:00:00Z") // 本地 11-19 12:00
	capability.scheduleNow = func() time.Time { return now }
	return &globalFixture{t: t, native: native, cap: capability, mux: mux, launcher: launcher, number: 500, ids: map[string]string{}}
}

func (f *globalFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.cap.store.db.Exec(query, args...); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
}

func (f *globalFixture) do(method, path, body string) (int, string) {
	f.t.Helper()
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	return recorder.Code, recorder.Body.String()
}

func (f *globalFixture) getJSON(path string, out any) {
	f.t.Helper()
	status, raw := f.do("GET", path, "")
	if status != 200 {
		f.t.Fatalf("GET %s = %d %s", path, status, raw)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		f.t.Fatalf("decode %s: %v", raw, err)
	}
}

func (f *globalFixture) session(id string) {
	f.t.Helper()
	if err := f.native.CreateSession(context.Background(), &types.SessionRecord{Session: &aop.Session{Id: id}}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *globalFixture) event(session, at, turn string, payload *aop.Event) {
	f.t.Helper()
	f.seq++
	emitted, err := time.Parse(time.RFC3339, at)
	if err != nil {
		f.t.Fatal(err)
	}
	payload.Id = fmt.Sprintf("%s-evt-%d", session, f.seq)
	payload.SessionId = session
	payload.TurnId = turn
	payload.Seq = uint64(f.seq)
	payload.EmittedAt = timestamppb.New(emitted)
	if err := f.native.AddAOPEvent(context.Background(), session, payload); err != nil {
		f.t.Fatal(err)
	}
}

func userMessage() *aop.Event {
	return &aop.Event{Payload: &aop.Event_Message{Message: &aop.Message{Role: "user"}}}
}

func (f *globalFixture) task(id, assetID, objective, knownInfo, origin, session, createdAt string) {
	f.t.Helper()
	f.session(session)
	f.exec(`INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, knownInfo, objective, `["`+origin+`"]`, session, createdAt)
	if assetID != "" {
		f.exec(`INSERT INTO webapi_asset_tasks (task_id, asset_id, created_at) VALUES (?, ?, ?)`, id, assetID, createdAt)
	}
}

// finding 写候选与当前修订，证据为一条 supporting（method/url）。
func (f *globalFixture) finding(name, taskID, title, method, url, createdAt string) {
	f.t.Helper()
	f.number++
	findingID, revisionID := "wf_"+name, "wfr_"+name
	payload, _ := json.Marshal(storedFindingPayload{Canonical: "{}", Content: findingContent{Title: title, Description: "演示", Steps: []findingStepInput{}}, Evidence: evidenceAt(method, url)})
	f.exec(`INSERT INTO webapi_findings (id, task_id, number, status, current_revision, current_revision_id, created_at, updated_at) VALUES (?, ?, ?, 'candidate', 1, ?, ?, ?)`,
		findingID, taskID, f.number, revisionID, createdAt, createdAt)
	f.exec(`INSERT INTO webapi_finding_revisions (id, finding_id, task_id, number, revision, session_id, turn_id, call_id, payload_json, title, created_at) VALUES (?, ?, ?, ?, 1, 's', 't', ?, ?, ?, ?)`,
		revisionID, findingID, taskID, f.number, "c-"+name, string(payload), title, createdAt)
	f.ids[name] = findingID
}

// verify 写一条验证（会话 = sessionID，已建原生会话）与可选判读（时间 at）。
func (f *globalFixture) verify(name, taskID, finding, sessionID, verdict, at string) {
	f.t.Helper()
	f.session(sessionID)
	f.exec(`INSERT INTO webapi_verifications (id, task_id, finding_id, source_revision_id, session_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		name, taskID, "wf_"+finding, "wfr_"+finding, sessionID, at)
	if verdict != "" {
		payload, _ := json.Marshal(storedVerificationResult{Canonical: verdict, Payload: verificationToolPayload{Verdict: verdict, Method: "演示", Summary: "演示"}})
		f.exec(`INSERT INTO webapi_verification_results (verification_id, session_id, turn_id, call_id, payload_json, created_at) VALUES (?, ?, 't', ?, ?, ?)`,
			name, sessionID, "c-"+name, string(payload), at)
	}
}

func (f *globalFixture) retest(name, taskID, finding, sessionID, conclusion, at string) {
	f.t.Helper()
	f.session(sessionID)
	f.exec(`INSERT INTO webapi_retests (id, task_id, finding_id, source_revision_id, session_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		name, taskID, "wf_"+finding, "wfr_"+finding, sessionID, at)
	payload, _ := json.Marshal(storedRetestResult{Canonical: conclusion, Payload: retestToolPayload{Conclusion: conclusion, Method: "演示", Summary: "演示"}})
	f.exec(`INSERT INTO webapi_retest_results (retest_id, session_id, turn_id, call_id, payload_json, created_at) VALUES (?, ?, 't', ?, ?, ?)`,
		name, sessionID, "c-"+name, string(payload), at)
}

func (f *globalFixture) report(id, taskID, sessionID, finding, createdAt, draftTitle, draftAt string) {
	f.t.Helper()
	f.session(sessionID)
	items := fmt.Sprintf(`[{"finding_id":"wf_%s","revision_id":"wfr_%s","verification_ids":[]}]`, finding, finding)
	f.exec(`INSERT INTO webapi_reports (id, task_id, session_id, items_json, created_at) VALUES (?, ?, ?, ?, ?)`, id, taskID, sessionID, items, createdAt)
	if draftTitle != "" {
		payload, _ := json.Marshal(storedReportDraft{Canonical: "{}", Payload: reportDraftPayload{Title: draftTitle, Overview: "演示"}})
		f.exec(`INSERT INTO webapi_report_drafts (report_id, session_id, turn_id, call_id, payload_json, created_at) VALUES (?, ?, 't', ?, ?, ?)`, id, sessionID, "c-"+id, string(payload), draftAt)
	}
}

// seed：资产 A1（启用计划）+ A2（归档，也有计划）；任务 T1(A1, 计划运行, 研究运行中)、T2(A2, 已结束)、
// T3(无资产, 未开始)；候选/验证/复测/报告与跨本地日的用量事件。
func (f *globalFixture) seed() {
	t := f.t
	for _, body := range []struct{ id, json string }{
		{"A1", `{"name":"演示商城","origins":["https://shop.example.test"]}`},
		{"A2", `{"name":"旧系统","origins":["https://old.example.test"]}`},
	} {
		if status, raw := f.do("PUT", "/api/webapi/assets/"+body.id, body.json); status != 201 {
			t.Fatalf("asset = %d %s", status, raw)
		}
		if status, raw := f.do("PUT", "/api/webapi/assets/"+body.id+"/schedule", `{"cron":"0 3 * * *"}`); status != 201 {
			t.Fatalf("schedule = %d %s", status, raw)
		}
	}
	if status, raw := f.do("PUT", "/api/webapi/assets/A2", `{"name":"旧系统","origins":["https://old.example.test"],"archived":true}`); status != 200 {
		t.Fatalf("archive = %d %s", status, raw)
	}

	f.task("T1", "A1", "商城越权检查", "商城 API", "https://shop.example.test", "s-t1", "2026-11-10T00:00:00Z")
	f.task("T2", "A2", "旧系统巡检", "旧系统", "https://old.example.test", "s-t2", "2026-11-15T00:00:00Z")
	// T3：UTC 11-18 17:00 = 本地 11-19 01:00。
	f.task("T3", "", "\n  检查 Example 越权\n第二行", "包含 KeyWord 的已知信息", "https://api.example.test", "s-t3", "2026-11-18T17:00:00Z")
	f.exec(`INSERT INTO webapi_task_policies (task_id, scope, approval, updated_at) VALUES ('T3', 'unrestricted', 'off', '2026-11-18T17:00:00Z')`)
	f.exec(`INSERT INTO webapi_schedule_runs (id, asset_id, scheduled_for, task_id, status, error, created_at) VALUES
		('run-1', 'A1', '2026-11-10T00:00:00Z', 'T1', 'launched', '', '2026-11-10T00:00:00Z'),
		('run-0', 'A2', '2026-11-09T00:00:00Z', '', 'failed', 'select_node：无节点', '2026-11-09T00:00:00Z')`)

	// T1 候选：F1a/F1b 同端点（订单）→ 一个条目；F1c 无结果 → pending；F1d 登录 → 后来修复。
	f.finding("F1a", "T1", "订单越权", "GET", "https://shop.example.test/api/orders/123", "2026-11-10T01:00:00Z")
	f.finding("F1b", "T1", "订单越权（另一编号）", "GET", "https://shop.example.test/api/orders/456", "2026-11-10T02:00:00Z")
	f.finding("F1c", "T1", "调试信息泄露", "GET", "https://shop.example.test/debug", "2026-11-10T03:00:00Z")
	f.finding("F1d", "T1", "登录爆破", "POST", "https://shop.example.test/api/login", "2026-11-10T04:00:00Z")
	f.verify("v1a", "T1", "F1a", "s-v1a", "supported", "2026-11-17T02:00:00Z")
	f.retest("r1b", "T1", "F1b", "s-r1b", "reproducible", "2026-11-18T02:00:00Z")
	f.verify("v1d", "T1", "F1d", "s-v1d", "supported", "2026-11-12T00:00:00Z")
	f.retest("r1d", "T1", "F1d", "s-r1d", "fixed_in_scope", "2026-11-19T02:00:00Z")
	// T2（归档资产）候选：不进全局漏洞。
	f.finding("F2", "T2", "旧系统注入", "GET", "https://old.example.test/search", "2026-11-15T01:00:00Z")
	f.verify("v2", "T2", "F2", "s-v2", "supported", "2026-11-16T00:00:00Z")
	// T3（无资产）候选：F3 supported → new；F4 counterevidence → unconfirmed。
	f.finding("F3", "T3", "用户资料越权", "GET", "https://api.example.test/users/1", "2026-11-18T18:00:00Z")
	f.finding("F4", "T3", "疑似 SSRF", "POST", "https://api.example.test/fetch", "2026-11-18T18:30:00Z")
	f.verify("v3", "T3", "F3", "s-v3", "supported", "2026-11-19T01:00:00Z")
	f.verify("v4", "T3", "F4", "s-v4", "counterevidence", "2026-11-19T01:30:00Z")

	f.report("rep1", "T1", "s-rep1", "F1a", "2026-11-17T03:00:00Z", "商城越权报告", "2026-11-17T04:00:00Z")
	f.report("rep3", "T3", "s-rep3", "F3", "2026-11-19T02:00:00Z", "", "")

	// 事件：T1 研究有轮次；UTC 15:59 = 本地 11-18 23:59，UTC 16:01 = 本地 11-19 00:01。
	f.event("s-t1", "2026-11-10T00:00:01Z", "turn-1", userMessage())
	f.event("s-t1", "2026-11-18T15:59:00Z", "turn-1", usageEvent("model-a", 90, 10, 100, 0, 0))
	f.event("s-v1a", "2026-11-18T16:01:00Z", "turn-v", usageEvent("model-a", 40, 10, 50, 0, 0))
	f.event("s-t2", "2026-11-15T00:00:01Z", "turn-2", userMessage())
	f.event("s-t2", "2026-11-16T00:00:00Z", "turn-2", usageEvent("model-b", 5, 2, 7, 0, 0))
	f.event("s-t2", "2026-11-16T00:00:05Z", "turn-2", &aop.Event{Payload: &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{StopReason: "end_turn"}}})
	f.launcher.running["s-t1"] = true
}

type taskListResponse struct {
	Items      []TaskListItem `json:"items"`
	HasMore    bool           `json:"has_more"`
	NextCursor *string        `json:"next_cursor"`
	Counts     map[string]int `json:"counts"`
}

func taskIDs(items []TaskListItem) string {
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.TaskID)
	}
	return strings.Join(ids, ",")
}

func TestTaskListFieldsFiltersAndPaging(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()

	var all taskListResponse
	f.getJSON("/api/webapi/task-list", &all)
	if got := taskIDs(all.Items); got != "T3,T2,T1" {
		t.Fatalf("order = %s", got)
	}
	wantCounts := map[string]int{"all": 3, "running": 1, "idle": 1, "not_started": 1, "unknown": 0}
	for key, want := range wantCounts {
		if all.Counts[key] != want {
			t.Fatalf("counts = %v", all.Counts)
		}
	}
	t1, t2, t3 := all.Items[2], all.Items[1], all.Items[0]
	if t1.Status != "running" || !t1.Scheduled || t1.Asset == nil || t1.Asset.Name != "演示商城" || t1.Title != "商城越权检查" {
		t.Fatalf("T1 = %+v", t1)
	}
	if t1.FindingCount != 4 || t1.VerificationCount != 2 || t1.SupportedCount != 2 || t1.RetestCount != 2 || t1.ReportCount != 1 {
		t.Fatalf("T1 counts = %+v", t1)
	}
	if t1.TotalTokens != 150 || t1.LastActivityAt != "2026-11-18T16:01:00Z" || t1.Policy.Scope != "origins" || t1.Policy.Approval != "high_risk" {
		t.Fatalf("T1 tokens/activity/policy = %+v", t1)
	}
	if t2.Status != "idle" || t2.Scheduled || t2.Asset == nil || t2.Asset.ID != "A2" || t2.TotalTokens != 7 || t2.SupportedCount != 1 {
		t.Fatalf("T2 = %+v", t2)
	}
	if t3.Status != "not_started" || t3.Asset != nil || t3.Title != "检查 Example 越权" || t3.LastActivityAt != "2026-11-18T17:00:00Z" ||
		t3.Policy.Scope != "unrestricted" || t3.Policy.Approval != "off" || t3.SupportedCount != 1 || t3.VerificationCount != 2 || t3.ReportCount != 1 ||
		len(t3.Origins) != 1 || t3.Origins[0] != "https://api.example.test" || t3.InitialSessionID != "s-t3" {
		t.Fatalf("T3 = %+v", t3)
	}

	// 分页：limit=2 → T3,T2；游标 → T1。
	var page taskListResponse
	f.getJSON("/api/webapi/task-list?limit=2", &page)
	if taskIDs(page.Items) != "T3,T2" || !page.HasMore || page.NextCursor == nil {
		t.Fatalf("page1 = %+v", page)
	}
	var page2 taskListResponse
	f.getJSON("/api/webapi/task-list?limit=2&cursor="+*page.NextCursor, &page2)
	if taskIDs(page2.Items) != "T1" || page2.HasMore || page2.NextCursor != nil || page2.Counts["all"] != 3 {
		t.Fatalf("page2 = %+v", page2)
	}

	// q：known_info（不区分大小写）、origins、objective；counts 受 q 影响。
	for query, want := range map[string]string{"keyword": "T3", "SHOP.example": "T1", "巡检": "T2", "nothing-here": ""} {
		var filtered taskListResponse
		f.getJSON("/api/webapi/task-list?q="+query, &filtered)
		if taskIDs(filtered.Items) != want || filtered.Counts["all"] != len(filtered.Items) {
			t.Fatalf("q=%s → %+v", query, filtered)
		}
	}
	// status 过滤不影响 counts；asset_id 过滤影响 counts。
	var notStarted taskListResponse
	f.getJSON("/api/webapi/task-list?status=not_started", &notStarted)
	if taskIDs(notStarted.Items) != "T3" || notStarted.Counts["all"] != 3 || notStarted.Counts["running"] != 1 {
		t.Fatalf("status filter = %+v", notStarted)
	}
	var byAsset taskListResponse
	f.getJSON("/api/webapi/task-list?asset_id=A1", &byAsset)
	if taskIDs(byAsset.Items) != "T1" || byAsset.Counts["all"] != 1 {
		t.Fatalf("asset filter = %+v", byAsset)
	}

	// SessionRunning 无法核对 → unknown（无轮次的仍是 not_started）。
	f.launcher.runningErr = fmt.Errorf("节点状态不可读")
	var unknown taskListResponse
	f.getJSON("/api/webapi/task-list?status=unknown", &unknown)
	if taskIDs(unknown.Items) != "T2,T1" || unknown.Counts["unknown"] != 2 || unknown.Counts["not_started"] != 1 {
		t.Fatalf("unknown = %+v", unknown)
	}

	for _, bad := range []string{"?limit=0", "?limit=201", "?status=done", "?cursor=@@", "?asset_id=a%2Fb"} {
		if status, raw := f.do("GET", "/api/webapi/task-list"+bad, ""); status != 400 || !strings.Contains(raw, "error") {
			t.Fatalf("%s = %d %s", bad, status, raw)
		}
	}
}

type globalVulnResponse struct {
	Items      []GlobalVuln   `json:"items"`
	HasMore    bool           `json:"has_more"`
	NextCursor *string        `json:"next_cursor"`
	Counts     map[string]int `json:"counts"`
}

func vulnSummary(items []GlobalVuln) string {
	parts := []string{}
	for _, item := range items {
		parts = append(parts, item.Status+":"+item.Title)
	}
	return strings.Join(parts, ",")
}

func TestGlobalListVulns(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()

	var all globalVulnResponse
	f.getJSON("/api/webapi/vulns", &all)
	// 归档 A2 的“旧系统注入”不出现；同状态按 last_event 倒序。
	want := "new:用户资料越权,new:订单越权（另一编号）,pending:调试信息泄露,unconfirmed:疑似 SSRF,fixed:登录爆破"
	if got := vulnSummary(all.Items); got != want {
		t.Fatalf("items = %s", got)
	}
	if fmt.Sprint(all.Counts) != fmt.Sprint(map[string]int{"new": 2, "present": 0, "pending": 1, "unconfirmed": 1, "fixed": 1}) {
		t.Fatalf("counts = %v", all.Counts)
	}
	f3, order := all.Items[0], all.Items[1]
	if f3.ID != "finding:wf_F3" || f3.Kind != "task_finding" || f3.Asset != nil || f3.TaskID != "T3" || f3.MemberCount != 1 || f3.TaskCount != 1 ||
		f3.EndpointKey != "GET https://api.example.test/users/{id}" || f3.LastConfirmedAt == nil || *f3.LastConfirmedAt != "2026-11-19T01:00:00Z" ||
		f3.FirstSeenAt != "2026-11-18T18:00:00Z" || f3.LastEvent == nil || f3.LastEvent.FindingID != "wf_F3" {
		t.Fatalf("F3 = %+v", f3)
	}
	if order.Kind != "asset_vuln" || order.Asset == nil || order.Asset.ID != "A1" || order.MemberCount != 2 || order.TaskID != "T1" ||
		order.EndpointKey != "GET https://shop.example.test/api/orders/{id}" || order.LastEvent == nil || order.LastEvent.Value != "reproducible" ||
		order.ID != assetVulnID("A1", order.EndpointKey) {
		t.Fatalf("order vuln = %+v", order)
	}
	ssrf, login := all.Items[3], all.Items[4]
	if ssrf.ID != "finding:wf_F4" || ssrf.Status != "unconfirmed" {
		t.Fatalf("F4 = %+v", ssrf)
	}
	if login.FixedAt == nil || *login.FixedAt != "2026-11-19T02:00:00Z" {
		t.Fatalf("login = %+v", login)
	}

	// 过滤：status 不影响 counts；asset_id=A1 只含资产条目；归档 A2 为空；q 匹配标题/端点/资产名。
	var fixed globalVulnResponse
	f.getJSON("/api/webapi/vulns?status=fixed", &fixed)
	if vulnSummary(fixed.Items) != "fixed:登录爆破" || fixed.Counts["new"] != 2 {
		t.Fatalf("status filter = %+v", fixed)
	}
	var a1 globalVulnResponse
	f.getJSON("/api/webapi/vulns?asset_id=A1", &a1)
	if len(a1.Items) != 3 || a1.Counts["new"] != 1 || a1.Counts["unconfirmed"] != 0 {
		t.Fatalf("asset filter = %+v", a1)
	}
	var a2 globalVulnResponse
	f.getJSON("/api/webapi/vulns?asset_id=A2", &a2)
	if len(a2.Items) != 0 || a2.Counts["new"] != 0 {
		t.Fatalf("archived asset = %+v", a2)
	}
	for query, want := range map[string]int{"ssrf": 1, "API.EXAMPLE.TEST": 2, "演示商城": 3} {
		var filtered globalVulnResponse
		f.getJSON("/api/webapi/vulns?q="+query, &filtered)
		if len(filtered.Items) != want {
			t.Fatalf("q=%s → %s", query, vulnSummary(filtered.Items))
		}
	}

	// 分页（偏移游标）。
	var page globalVulnResponse
	f.getJSON("/api/webapi/vulns?limit=2", &page)
	if len(page.Items) != 2 || !page.HasMore || page.NextCursor == nil {
		t.Fatalf("page1 = %+v", page)
	}
	var rest globalVulnResponse
	f.getJSON("/api/webapi/vulns?limit=10&cursor="+*page.NextCursor, &rest)
	if vulnSummary(rest.Items) != "pending:调试信息泄露,unconfirmed:疑似 SSRF,fixed:登录爆破" || rest.HasMore || rest.NextCursor != nil {
		t.Fatalf("page2 = %+v", rest)
	}

	// 同步已写入 A1 漏洞库（与资产接口一致），A2 未同步（归档不处理）。
	var members int
	_ = f.cap.store.db.QueryRow(`SELECT COUNT(*) FROM webapi_asset_vuln_members WHERE asset_id = 'A2'`).Scan(&members)
	if members != 0 {
		t.Fatalf("archived asset synced: %d", members)
	}
	for _, bad := range []string{"?status=open", "?cursor=bad", "?limit=abc"} {
		if status, _ := f.do("GET", "/api/webapi/vulns"+bad, ""); status != 400 {
			t.Fatalf("%s = %d", bad, status)
		}
	}
}

func TestGlobalListReports(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()
	var response struct {
		Items      []GlobalReport `json:"items"`
		HasMore    bool           `json:"has_more"`
		NextCursor *string        `json:"next_cursor"`
		Total      int            `json:"total"`
	}
	f.getJSON("/api/webapi/report-list", &response)
	if len(response.Items) != 2 || response.Total != 2 {
		t.Fatalf("reports = %+v", response)
	}
	rep3, rep1 := response.Items[0], response.Items[1]
	if rep3.ReportID != "rep3" || rep3.HasDraft || rep3.Title != "" || rep3.Asset != nil || rep3.TaskTitle != "检查 Example 越权" || rep3.ItemCount != 1 || rep3.UpdatedAt != rep3.CreatedAt {
		t.Fatalf("rep3 = %+v", rep3)
	}
	if rep1.ReportID != "rep1" || !rep1.HasDraft || rep1.Title != "商城越权报告" || rep1.Asset == nil || rep1.Asset.Name != "演示商城" ||
		rep1.SessionID != "s-rep1" || rep1.TaskID != "T1" || rep1.UpdatedAt != "2026-11-17T04:00:00Z" || rep1.CreatedAt != "2026-11-17T03:00:00Z" {
		t.Fatalf("rep1 = %+v", rep1)
	}
	f.getJSON("/api/webapi/report-list?limit=1", &response)
	if len(response.Items) != 1 || response.Items[0].ReportID != "rep3" || !response.HasMore || response.NextCursor == nil {
		t.Fatalf("page1 = %+v", response)
	}
	cursor := *response.NextCursor
	response.NextCursor = nil
	f.getJSON("/api/webapi/report-list?limit=1&cursor="+cursor, &response)
	if len(response.Items) != 1 || response.Items[0].ReportID != "rep1" || response.HasMore || response.NextCursor != nil {
		t.Fatalf("page2 = %+v", response)
	}
	f.getJSON("/api/webapi/report-list?q=越权报告", &response)
	if len(response.Items) != 1 || response.Items[0].ReportID != "rep1" {
		t.Fatalf("q = %+v", response)
	}
}

func TestOverviewAggregates(t *testing.T) {
	f := newGlobalFixture(t)
	f.seed()
	var view Overview
	f.getJSON("/api/webapi/overview?days=7", &view)
	if view.Days != 7 || view.GeneratedAt != "2026-11-19T04:00:00Z" || view.Timezone != "UTC+8" {
		t.Fatalf("header = %+v", view)
	}
	if view.Assets.Total != 2 || view.Assets.Archived != 1 || view.Assets.ScheduledEnabled != 1 {
		t.Fatalf("assets = %+v", view.Assets)
	}
	if view.Tasks.Total != 3 || view.Tasks.Running != 1 || view.Tasks.CreatedInRange != 2 || len(view.Tasks.ByDay) != 7 {
		t.Fatalf("tasks = %+v", view.Tasks)
	}
	if view.Tasks.ByDay[0].Date != "2026-11-13" || view.Tasks.ByDay[6].Date != "2026-11-19" {
		t.Fatalf("task days = %+v", view.Tasks.ByDay)
	}
	taskDays := map[string]int{}
	for _, day := range view.Tasks.ByDay {
		taskDays[day.Date] = day.Count
	}
	// T3 UTC 11-18 17:00 按本地时区落在 11-19；其余日期补 0。
	if taskDays["2026-11-15"] != 1 || taskDays["2026-11-19"] != 1 || taskDays["2026-11-18"] != 0 || taskDays["2026-11-13"] != 0 {
		t.Fatalf("task by_day = %v", taskDays)
	}

	if fmt.Sprint(view.Vulns.Counts) != fmt.Sprint(map[string]int{"new": 2, "present": 0, "pending": 1, "unconfirmed": 1, "fixed": 1}) {
		t.Fatalf("vuln counts = %v", view.Vulns.Counts)
	}
	vulnDays := map[string]OverviewVulnDay{}
	for _, day := range view.Vulns.ConfirmedByDay {
		vulnDays[day.Date] = day
	}
	// 11-17：订单首次 supported → new；11-18：复测 reproducible → present；11-19（本地）：F3 首次 supported → new，登录 fixed。
	// 登录 11-12 的首次 supported 在区间外；归档资产 11-16 的 supported 不计。
	if len(view.Vulns.ConfirmedByDay) != 7 || vulnDays["2026-11-17"] != (OverviewVulnDay{Date: "2026-11-17", New: 1}) ||
		vulnDays["2026-11-18"] != (OverviewVulnDay{Date: "2026-11-18", Present: 1}) || vulnDays["2026-11-19"] != (OverviewVulnDay{Date: "2026-11-19", New: 1, Fixed: 1}) ||
		vulnDays["2026-11-16"] != (OverviewVulnDay{Date: "2026-11-16"}) {
		t.Fatalf("confirmed_by_day = %+v", view.Vulns.ConfirmedByDay)
	}

	tokenDays := map[string]uint64{}
	for _, day := range view.Tokens.ByDay {
		tokenDays[day.Date] = day.TotalTokens
	}
	if view.Tokens.Total != 157 || len(view.Tokens.ByDay) != 7 || tokenDays["2026-11-18"] != 100 || tokenDays["2026-11-19"] != 50 || tokenDays["2026-11-16"] != 7 || tokenDays["2026-11-14"] != 0 {
		t.Fatalf("tokens = %+v", view.Tokens)
	}

	// 计划：A1 下一次本地 11-20 03:00 = UTC 11-19 19:00；归档 A2 不列。最近运行按 scheduled_for 倒序。
	if len(view.Schedules.Next) != 1 || view.Schedules.Next[0] != (OverviewNextRun{AssetID: "A1", AssetName: "演示商城", NextRunAt: "2026-11-19T19:00:00Z"}) {
		t.Fatalf("next = %+v", view.Schedules.Next)
	}
	runs := view.Schedules.RecentRuns
	if len(runs) != 2 || runs[0].AssetID != "A1" || runs[0].Status != "launched" || runs[0].TaskID != "T1" || runs[0].FollowupsState == nil || *runs[0].FollowupsState != "waiting" ||
		runs[1].AssetName != "旧系统" || runs[1].Status != "failed" || runs[1].FollowupsState != nil {
		t.Fatalf("recent runs = %+v", runs)
	}

	recent := view.RecentSupported
	if len(recent) != 3 || recent[0].VulnID != "finding:wf_F3" || recent[0].TaskID != "T3" || recent[0].Asset != nil || recent[0].At != "2026-11-19T01:00:00Z" ||
		recent[1].Title != "订单越权（另一编号）" || recent[1].Asset == nil || recent[1].At != "2026-11-17T02:00:00Z" || recent[2].Title != "登录爆破" {
		t.Fatalf("recent_supported = %+v", recent)
	}

	// 默认 30 天；非法 days 400。
	var month Overview
	f.getJSON("/api/webapi/overview", &month)
	if month.Days != 30 || len(month.Tasks.ByDay) != 30 || month.Tasks.CreatedInRange != 3 || month.Tasks.ByDay[29].Date != "2026-11-19" {
		t.Fatalf("30 days = %+v", month.Tasks)
	}
	if status, _ := f.do("GET", "/api/webapi/overview?days=14", ""); status != 400 {
		t.Fatalf("days=14 = %d", status)
	}
}
