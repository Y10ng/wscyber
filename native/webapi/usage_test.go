package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type usageFixture struct {
	t      *testing.T
	native *webservice.SQLiteStore
	cap    *Capability
	mux    *http.ServeMux
	seq    int
}

// newUsageFixture 用临时库：原生 Store 建 schema 与会话/事件，业务行直接 SQL 写入。
// 进程时区固定为 UTC+8，便于构造跨日边界。
func newUsageFixture(t *testing.T) *usageFixture {
	t.Helper()
	previous := time.Local
	time.Local = time.FixedZone("UTC+8", 8*3600)
	t.Cleanup(func() { time.Local = previous })
	database := newTestHub(t)
	native, err := webservice.NewSQLiteStore(database.DSN(), SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = native.Close() })
	capability, err := NewCapability(database.URL, database.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close(context.Background()) })
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	return &usageFixture{t: t, native: native, cap: capability, mux: mux}
}

func (f *usageFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.cap.store.db().Exec(query, args...); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
}

func (f *usageFixture) session(id string) {
	f.t.Helper()
	if err := f.native.CreateSession(context.Background(), &types.SessionRecord{Session: &aop.Session{Id: id}}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *usageFixture) event(session, at string, payload *aop.Event) {
	f.t.Helper()
	f.seq++
	emitted, err := time.Parse(time.RFC3339, at)
	if err != nil {
		f.t.Fatal(err)
	}
	payload.Id = fmt.Sprintf("%s-evt-%d", session, f.seq)
	payload.SessionId = session
	payload.Seq = uint64(f.seq)
	payload.EmittedAt = timestamppb.New(emitted)
	if err := f.native.AddAOPEvent(context.Background(), session, payload); err != nil {
		f.t.Fatal(err)
	}
}

func usageEvent(model string, input, output, total, cacheRead, cacheWrite uint64) *aop.Event {
	detail := map[string]uint64{}
	if cacheRead > 0 {
		detail["cache_read"] = cacheRead
	}
	if cacheWrite > 0 {
		detail["cache_write"] = cacheWrite
	}
	return &aop.Event{Payload: &aop.Event_Usage{Usage: &aop.TokenUsage{InputTokens: input, OutputTokens: output, TotalTokens: total, Model: model, Detail: detail}}}
}

// usageResponse 是测试侧解码视图（响应里条件数组为普通切片）。
type usageResponse struct {
	Timezone  string         `json:"timezone"`
	Totals    UsageTotals    `json:"totals"`
	ByDay     []UsageDay     `json:"by_day"`
	ByModel   []UsageModel   `json:"by_model"`
	ByTask    []UsageTask    `json:"by_task"`
	BySession []UsageSession `json:"by_session"`
}

func (f *usageFixture) get(path string) (int, usageResponse, map[string]any) {
	f.t.Helper()
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	var report usageResponse
	var raw map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &report)
	_ = json.Unmarshal(recorder.Body.Bytes(), &raw)
	return recorder.Code, report, raw
}

// seed：任务 T1 四种角色会话 + 任务 T2 研究会话 + 一个不属于任务的会话。
func (f *usageFixture) seed() {
	for _, s := range []string{"s-research", "s-verify", "s-report", "s-retest", "s-t2", "s-free"} {
		f.session(s)
	}
	f.exec(`INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES
		('T1', 'k', ?, '["https://a.test"]', 's-research', '2026-11-16T00:00:00Z'),
		('T2', 'k', '任务二', '["https://b.test"]', 's-t2', '2026-11-16T00:00:00Z')`, "\n  任务一目标\n第二行")
	f.exec(`INSERT INTO webapi_verifications (id, task_id, finding_id, source_revision_id, session_id, created_at) VALUES ('v1', 'T1', 'F1', 'R1', 's-verify', '2026-11-16T00:00:00Z')`)
	f.exec(`INSERT INTO webapi_reports (id, task_id, session_id, items_json, created_at) VALUES ('rep1', 'T1', 's-report', '[]', '2026-11-16T00:00:00Z')`)
	f.exec(`INSERT INTO webapi_retests (id, task_id, finding_id, source_revision_id, session_id, created_at) VALUES ('rt1', 'T1', 'F1', 'R1', 's-retest', '2026-11-16T00:00:00Z')`)

	// 研究：两次调用跨本地日（UTC 15:59 = 本地 23:59 11-16；UTC 16:01 = 本地 00:01 11-17）。
	f.event("s-research", "2026-11-16T15:59:00Z", usageEvent("model-a", 100, 10, 110, 40, 5))
	f.event("s-research", "2026-11-16T16:01:00Z", usageEvent("model-a", 200, 20, 0, 0, 0)) // total 缺失 → 220
	// TurnEnded 累计 usage 不得重复计入；普通消息事件忽略。
	f.event("s-research", "2026-11-16T16:02:00Z", &aop.Event{Payload: &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{StopReason: "end_turn", Usage: &aop.TokenUsage{InputTokens: 300, OutputTokens: 30, TotalTokens: 330}}}})
	f.event("s-research", "2026-11-16T16:03:00Z", &aop.Event{Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{}}})
	f.event("s-verify", "2026-11-17T01:00:00Z", usageEvent("model-b", 50, 5, 55, 0, 0))
	f.event("s-report", "2026-11-17T02:00:00Z", usageEvent("model-b", 1000, 100, 1100, 0, 0))
	f.event("s-retest", "2026-11-17T03:00:00Z", usageEvent("model-a", 30, 3, 33, 0, 0))
	f.event("s-t2", "2026-11-17T04:00:00Z", usageEvent("model-a", 7, 1, 8, 0, 0))
	f.event("s-free", "2026-11-17T05:00:00Z", usageEvent("model-c", 2, 1, 3, 0, 0))
}

func TestUsageByTaskRolesDaysAndNoTurnEndedDoubleCount(t *testing.T) {
	f := newUsageFixture(t)
	f.seed()
	status, report, raw := f.get("/api/webapi/usage?task_id=T1")
	if status != 200 {
		t.Fatalf("status=%d %v", status, raw)
	}
	if report.Timezone != "UTC+8" {
		t.Fatalf("timezone=%q", report.Timezone)
	}
	want := UsageTotals{InputTokens: 1380, OutputTokens: 138, TotalTokens: 1518, CacheReadTokens: 40, CacheWriteTokens: 5, Calls: 5}
	if report.Totals != want {
		t.Fatalf("totals=%+v want %+v", report.Totals, want)
	}
	if _, ok := raw["by_task"]; ok {
		t.Fatalf("任务查询不应返回 by_task: %v", raw)
	}
	if len(report.ByDay) != 2 || report.ByDay[0].Day != "2026-11-16" || report.ByDay[0].TotalTokens != 110 ||
		report.ByDay[1].Day != "2026-11-17" || report.ByDay[1].TotalTokens != 1408 || report.ByDay[1].Calls != 4 {
		t.Fatalf("by_day=%+v", report.ByDay)
	}
	if len(report.ByModel) != 2 || report.ByModel[0].Model != "model-b" || report.ByModel[0].TotalTokens != 1155 || report.ByModel[1].TotalTokens != 363 {
		t.Fatalf("by_model=%+v", report.ByModel)
	}
	got := map[string]UsageSession{}
	for _, s := range report.BySession {
		got[s.SessionID] = s
	}
	if len(report.BySession) != 4 || report.BySession[0].SessionID != "s-report" {
		t.Fatalf("by_session 排序/数量=%+v", report.BySession)
	}
	checks := []struct {
		session, role, finding, reportID string
		total                            uint64
	}{
		{"s-research", "research", "", "", 330},
		{"s-verify", "verification", "F1", "", 55},
		{"s-report", "report", "", "rep1", 1100},
		{"s-retest", "retest", "F1", "", 33},
	}
	for _, c := range checks {
		s := got[c.session]
		if s.Role != c.role || s.FindingID != c.finding || s.ReportID != c.reportID || s.TotalTokens != c.total {
			t.Fatalf("%s=%+v", c.session, s)
		}
	}

	// 日期闭区间：只取本地 11-16。
	status, report, _ = f.get("/api/webapi/usage?task_id=T1&from=2026-11-16&to=2026-11-16")
	if status != 200 || report.Totals.TotalTokens != 110 || report.Totals.Calls != 1 || len(report.BySession) != 1 {
		t.Fatalf("单日=%d %+v", status, report)
	}
	status, report, _ = f.get("/api/webapi/usage?task_id=T1&from=2026-11-17")
	if status != 200 || report.Totals.TotalTokens != 1408 {
		t.Fatalf("from=%d %+v", status, report.Totals)
	}
}

func TestUsageGlobalByTask(t *testing.T) {
	f := newUsageFixture(t)
	f.seed()
	status, report, raw := f.get("/api/webapi/usage")
	if status != 200 {
		t.Fatalf("status=%d %v", status, raw)
	}
	if _, ok := raw["by_session"]; ok {
		t.Fatalf("全局查询不应返回 by_session")
	}
	if report.Totals.TotalTokens != 1529 || report.Totals.Calls != 7 {
		t.Fatalf("totals=%+v", report.Totals)
	}
	if len(report.ByTask) != 3 {
		t.Fatalf("by_task=%+v", report.ByTask)
	}
	if report.ByTask[0].TaskID != "T1" || report.ByTask[0].Title != "任务一目标" || report.ByTask[0].TotalTokens != 1518 ||
		report.ByTask[1].TaskID != "T2" || report.ByTask[1].Title != "任务二" || report.ByTask[1].TotalTokens != 8 ||
		report.ByTask[2].TaskID != "" || report.ByTask[2].TotalTokens != 3 {
		t.Fatalf("by_task=%+v", report.ByTask)
	}
	if len(report.ByModel) != 3 || report.ByModel[2].Model != "model-c" {
		t.Fatalf("by_model=%+v", report.ByModel)
	}
	// 空 task_id 等同全局。
	if status, again, _ := f.get("/api/webapi/usage?task_id="); status != 200 || again.Totals != report.Totals {
		t.Fatalf("空 task_id=%d %+v", status, again.Totals)
	}
}

func TestUsageErrorsAndEmpty(t *testing.T) {
	f := newUsageFixture(t)
	f.seed()
	if status, _, _ := f.get("/api/webapi/usage?task_id=missing"); status != 404 {
		t.Fatalf("未知任务=%d", status)
	}
	for _, path := range []string{
		"/api/webapi/usage?from=2026-13-01",
		"/api/webapi/usage?to=2026/11/17",
		"/api/webapi/usage?from=2026-11-1",
		"/api/webapi/usage?from=2026-11-18&to=2026-11-17",
		"/api/webapi/usage?task_id=a%2Fb",
	} {
		if status, _, raw := f.get(path); status != 400 || raw["error"] == "" {
			t.Fatalf("%s=%d %v", path, status, raw)
		}
	}
	// 区间无数据：数组非 null。
	status, _, raw := f.get("/api/webapi/usage?task_id=T2&from=2020-01-01&to=2020-01-02")
	if status != 200 {
		t.Fatalf("空区间=%d", status)
	}
	for _, key := range []string{"by_day", "by_model", "by_session"} {
		if list, ok := raw[key].([]any); !ok || len(list) != 0 {
			t.Fatalf("%s=%v", key, raw[key])
		}
	}
}

func TestUsageEventDecodeSkipsTurnEnded(t *testing.T) {
	turnEnded := &aop.Event{Id: "x", Payload: &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{Usage: &aop.TokenUsage{InputTokens: 1}}}}
	raw, err := proto.Marshal(turnEnded)
	if err != nil {
		t.Fatal(err)
	}
	if usage, err := eventUsage(raw); err != nil || usage != nil {
		t.Fatalf("TurnEnded 应不计: %v %v", usage, err)
	}
	if _, err := eventUsage([]byte{0xff}); err == nil {
		t.Fatalf("损坏 proto 应报错")
	}
}
