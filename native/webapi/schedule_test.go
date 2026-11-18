package webapi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// 全部为合成数据：example.test 演示资产、注入时钟与 launcher 替身。
type scheduleFixture struct {
	*assetFixture
	launcher *fakeLauncher
	now      time.Time
}

func newScheduleFixture(t *testing.T, start string) *scheduleFixture {
	t.Helper()
	f := &scheduleFixture{assetFixture: newAssetFixture(t), launcher: newFakeLauncher(productAgent("node-a"))}
	f.capability.launcher = f.launcher
	f.capability.scheduleLoc = time.UTC
	f.setNow(start)
	f.capability.scheduleNow = func() time.Time { return f.now }
	return f
}

func (f *scheduleFixture) setNow(value string) {
	f.t.Helper()
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		f.t.Fatal(err)
	}
	f.now = at
}

func (f *scheduleFixture) tick() {
	f.t.Helper()
	if err := f.capability.runDueSchedules(f.t.Context()); err != nil {
		f.t.Fatalf("runDueSchedules: %v", err)
	}
}

func (f *scheduleFixture) putSchedule(assetID, body string) map[string]any {
	f.t.Helper()
	status, value, raw := f.do("PUT", "/api/webapi/assets/"+assetID+"/schedule", body)
	if status != 201 && status != 200 {
		f.t.Fatalf("put schedule = %d %s", status, raw)
	}
	return value
}

func (f *scheduleFixture) nextRunAt(assetID string) string {
	f.t.Helper()
	model, err := f.capability.store.schedule(f.t.Context(), assetID)
	if err != nil || model == nil {
		f.t.Fatalf("schedule %s = %v %v", assetID, model, err)
	}
	return model.NextRunAt
}

func (f *scheduleFixture) runs(assetID string) []scheduleRunModel {
	f.t.Helper()
	runs, err := f.capability.store.scheduleRuns(f.t.Context(), assetID, 100, "")
	if err != nil {
		f.t.Fatal(err)
	}
	return runs
}

func (f *scheduleFixture) sessionOf(taskID string) string {
	f.t.Helper()
	task, err := f.capability.store.Get(f.t.Context(), taskID)
	if err != nil || task == nil {
		f.t.Fatalf("task %s = %v %v", taskID, task, err)
	}
	return task.InitialSessionID
}

func TestWebAPIScheduleCRUDAndValidation(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T00:00:00Z")
	f.capability.scheduleLoc = time.FixedZone("UTC+8", 8*3600)
	f.createAsset("a1")
	if status, _, _ := f.do("GET", "/api/webapi/assets/a1/schedule", ""); status != 404 {
		t.Fatalf("unset schedule = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/missing/schedule", `{"cron":"0 * * * *"}`); status != 404 {
		t.Fatalf("missing asset = %d", status)
	}
	for _, body := range []string{
		`{"cron":""}`, `{"cron":"* * * *"}`, `{"cron":"61 * * * *"}`, `{"cron":"0 0 30 2 *"}`,
		`{"cron":"0 * * * *","extra":1}`, `not json`, `{"cron":"` + strings.Repeat("1,", 70) + `1 * * * *"}`,
	} {
		if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/schedule", body); status != 400 {
			t.Fatalf("invalid %s accepted: %d %s", body, status, raw)
		}
	}
	// 按 Hub 本地时区解释：+08:00 的 09:30 = UTC 01:30；enabled 省略视为 true。
	status, value, raw := f.do("PUT", "/api/webapi/assets/a1/schedule", `{"cron":" 30 9 * * * "}`)
	if status != 201 || value["enabled"] != true || value["cron"] != "30 9 * * *" || value["next_run_at"] != "2026-11-18T01:30:00Z" || value["last_run"] != nil {
		t.Fatalf("create schedule = %d %s", status, raw)
	}
	upcoming := value["upcoming"].([]any)
	if len(upcoming) != 3 || upcoming[1] != "2026-11-19T01:30:00Z" || upcoming[2] != "2026-11-20T01:30:00Z" || !strings.Contains(value["timezone"].(string), "+08:00") {
		t.Fatalf("upcoming = %v tz=%v", upcoming, value["timezone"])
	}
	// 保存即生效：改 cron 立即重算。
	value = f.putSchedule("a1", `{"cron":"*/15 * * * *","enabled":true}`)
	if value["next_run_at"] != "2026-11-18T00:15:00Z" || value["upcoming"].([]any)[2] != "2026-11-18T00:45:00Z" {
		t.Fatalf("update schedule = %v", value)
	}
	value = f.putSchedule("a1", `{"cron":"*/15 * * * *","enabled":false}`)
	if value["enabled"] != false || value["next_run_at"] != nil || len(value["upcoming"].([]any)) != 0 {
		t.Fatalf("disabled schedule = %v", value)
	}
	f.createAsset("a2")
	f.putSchedule("a2", `{"cron":"0 * * * *"}`)
	status, value, raw = f.do("GET", "/api/webapi/schedules", "")
	items, _ := value["schedules"].([]any)
	if status != 200 || len(items) != 2 || items[0].(map[string]any)["asset_id"] != "a2" || items[0].(map[string]any)["asset_name"] != "演示库存系统" || items[1].(map[string]any)["enabled"] != false {
		t.Fatalf("schedule list = %d %s", status, raw)
	}
	if status, value, _ := f.do("DELETE", "/api/webapi/assets/a1/schedule", ""); status != 200 || value["deleted"] != true {
		t.Fatalf("delete = %d %v", status, value)
	}
	if status, _, _ := f.do("DELETE", "/api/webapi/assets/a1/schedule", ""); status != 404 {
		t.Fatalf("delete again = %d", status)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/a1/schedule/runs?limit=0", ""); status != 400 {
		t.Fatalf("bad limit = %d", status)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/a1/schedule/runs?cursor=***", ""); status != 400 {
		t.Fatalf("bad cursor = %d", status)
	}
}

// 到期发起、确定性 task_id、重复检查不重复发起、运行记录分页与列表摘要。
func TestWebAPIScheduleLaunchDeterministicNoDuplicate(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:59:00Z")
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.tick()
	if len(f.launcher.runs) != 0 || len(f.runs("a1")) != 0 {
		t.Fatal("launched before due")
	}
	f.setNow("2026-11-18T10:00:20Z")
	f.tick()
	wantTask := scheduleTaskID("a1", "2026-11-18T10:00:00Z")
	runs := f.runs("a1")
	if len(runs) != 1 || runs[0].Status != ScheduleRunLaunched || runs[0].TaskID != wantTask || runs[0].ScheduledFor != "2026-11-18T10:00:00Z" || runs[0].Error != "" {
		t.Fatalf("runs = %+v", runs)
	}
	if wantTask != scheduleTaskID("a1", "2026-11-18T10:00:00Z") || wantTask == scheduleTaskID("a2", "2026-11-18T10:00:00Z") || len(wantTask) != 36 || wantTask[14] != '5' {
		t.Fatalf("task id not deterministic UUIDv5: %s", wantTask)
	}
	if len(f.launcher.opens) != 1 || len(f.launcher.runs) != 1 || f.nextRunAt("a1") != "2026-11-18T11:00:00Z" {
		t.Fatalf("opens=%d runs=%d next=%s", len(f.launcher.opens), len(f.launcher.runs), f.nextRunAt("a1"))
	}
	// 同一时刻再查：不再发起。
	f.tick()
	// 模拟“发起后、写记录前”Hub 中断：next_run_at 回到旧值再查，同 task_id 幂等，不重复 Open/Run、不重复记录。
	if _, err := f.capability.store.db.ExecContext(t.Context(), "UPDATE webapi_asset_schedules SET next_run_at = ? WHERE asset_id = ?", "2026-11-18T10:00:00Z", "a1"); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if len(f.launcher.opens) != 1 || len(f.launcher.runs) != 1 || len(f.runs("a1")) != 1 || f.nextRunAt("a1") != "2026-11-18T11:00:00Z" {
		t.Fatalf("duplicate launch: opens=%d runs=%d records=%d", len(f.launcher.opens), len(f.launcher.runs), len(f.runs("a1")))
	}
	// 任务带资产关联与资产默认策略。
	_, value, _ := f.do("GET", "/api/webapi/tasks/"+wantTask, "")
	if value["asset"].(map[string]any)["id"] != "a1" || value["policy"].(map[string]any)["approval"] != "high_risk" {
		t.Fatalf("scheduled task = %v", value)
	}
	f.setNow("2026-11-18T11:00:05Z")
	f.tick()
	status, value, raw := f.do("GET", "/api/webapi/assets/a1/schedule/runs?limit=1", "")
	items := value["items"].([]any)
	if status != 200 || len(items) != 1 || items[0].(map[string]any)["scheduled_for"] != "2026-11-18T11:00:00Z" || value["has_more"] != true {
		t.Fatalf("runs page 1 = %d %s", status, raw)
	}
	_, value, raw = f.do("GET", "/api/webapi/assets/a1/schedule/runs?limit=1&cursor="+value["next_cursor"].(string), "")
	items = value["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["task_id"] != wantTask || value["has_more"] != false || value["next_cursor"] != nil {
		t.Fatalf("runs page 2 = %s", raw)
	}
	_, value, raw = f.do("GET", "/api/webapi/schedules", "")
	last := value["schedules"].([]any)[0].(map[string]any)["last_run"].(map[string]any)
	if last["scheduled_for"] != "2026-11-18T11:00:00Z" || last["status"] != "launched" {
		t.Fatalf("list last_run = %s", raw)
	}
	_, value, _ = f.do("GET", "/api/webapi/assets/a1/schedule", "")
	if value["last_run"].(map[string]any)["scheduled_for"] != "2026-11-18T11:00:00Z" || value["next_run_at"] != "2026-11-18T12:00:00Z" {
		t.Fatalf("schedule GET = %v", value)
	}
}

// 上一次计划任务的研究会话仍在运行 → skipped（不排队），next_run_at 照常推进。
func TestWebAPISchedulePreviousRunningSkipped(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.setNow("2026-11-18T10:00:10Z")
	f.tick()
	first := scheduleTaskID("a1", "2026-11-18T10:00:00Z")
	f.launcher.running[f.sessionOf(first)] = true
	f.setNow("2026-11-18T11:00:10Z")
	f.tick()
	runs := f.runs("a1")
	if len(runs) != 2 || runs[0].Status != ScheduleRunSkipped || runs[0].TaskID != "" || !strings.Contains(runs[0].Error, "上一次计划任务仍在运行") || !strings.Contains(runs[0].Error, first) {
		t.Fatalf("runs = %+v", runs)
	}
	if len(f.launcher.runs) != 1 || f.nextRunAt("a1") != "2026-11-18T12:00:00Z" {
		t.Fatalf("launched while previous running: runs=%d next=%s", len(f.launcher.runs), f.nextRunAt("a1"))
	}
	// 判定报错：保持到期，不记 run，下轮再试。
	f.launcher.runningErr = context.DeadlineExceeded
	f.setNow("2026-11-18T12:00:10Z")
	f.tick()
	if len(f.runs("a1")) != 2 || f.nextRunAt("a1") != "2026-11-18T12:00:00Z" {
		t.Fatalf("unverifiable running state should stay due: %+v", f.runs("a1"))
	}
	f.launcher.runningErr = nil
	delete(f.launcher.running, f.sessionOf(first))
	f.tick()
	runs = f.runs("a1")
	if len(runs) != 3 || runs[0].Status != ScheduleRunLaunched || runs[0].ScheduledFor != "2026-11-18T12:00:00Z" || len(f.launcher.runs) != 2 {
		t.Fatalf("after previous finished = %+v", runs)
	}
}

// 全局并发上限 2：超出不发起、不记 run、保持到期；名额释放后下轮发起。
func TestWebAPIScheduleConcurrencyLimit(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	for _, id := range []string{"a1", "a2", "a3"} {
		f.createAsset(id)
		f.putSchedule(id, `{"cron":"0 * * * *"}`)
	}
	f.setNow("2026-11-18T10:00:10Z")
	f.tick()
	if len(f.launcher.runs) != 2 || len(f.runs("a1")) != 1 || len(f.runs("a2")) != 1 || len(f.runs("a3")) != 0 || f.nextRunAt("a3") != "2026-11-18T10:00:00Z" {
		t.Fatalf("first tick: runs=%d a3runs=%d a3next=%s", len(f.launcher.runs), len(f.runs("a3")), f.nextRunAt("a3"))
	}
	// a1/a2 的研究会话仍在运行：a3 仍等待。
	f.launcher.runningAll = true
	f.setNow("2026-11-18T10:00:40Z")
	f.tick()
	if len(f.launcher.runs) != 2 || len(f.runs("a3")) != 0 {
		t.Fatalf("launched over limit: runs=%d", len(f.launcher.runs))
	}
	// 一个结束即释放名额。
	f.launcher.runningAll = false
	f.launcher.running[f.sessionOf(scheduleTaskID("a1", "2026-11-18T10:00:00Z"))] = true
	f.setNow("2026-11-18T10:01:10Z")
	f.tick()
	runs := f.runs("a3")
	if len(f.launcher.runs) != 3 || len(runs) != 1 || runs[0].ScheduledFor != "2026-11-18T10:00:00Z" || runs[0].Status != ScheduleRunLaunched || f.nextRunAt("a3") != "2026-11-18T11:00:00Z" {
		t.Fatalf("a3 after slot freed: runs=%+v", runs)
	}
}

// Hub 停机错过多次只补一次（scheduled_for = 最近一次应运行时间），然后按 cron 继续。
func TestWebAPIScheduleMissedRunsCatchUpOnce(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:55:00Z")
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"*/10 * * * *"}`)
	if f.nextRunAt("a1") != "2026-11-18T10:00:00Z" {
		t.Fatalf("next = %s", f.nextRunAt("a1"))
	}
	f.setNow("2026-11-18T10:47:30Z")
	f.tick()
	runs := f.runs("a1")
	if len(runs) != 1 || runs[0].ScheduledFor != "2026-11-18T10:40:00Z" || runs[0].TaskID != scheduleTaskID("a1", "2026-11-18T10:40:00Z") || len(f.launcher.runs) != 1 || f.nextRunAt("a1") != "2026-11-18T10:50:00Z" {
		t.Fatalf("catch up = %+v next=%s", runs, f.nextRunAt("a1"))
	}
	f.tick()
	f.setNow("2026-11-18T10:50:05Z")
	f.tick()
	if len(f.runs("a1")) != 2 || len(f.launcher.runs) != 2 || f.nextRunAt("a1") != "2026-11-18T11:00:00Z" {
		t.Fatalf("continue = %+v", f.runs("a1"))
	}
}

// 发起失败记 failed（含阶段与文案），不自动重试，next_run_at 照常推进；归档/停用不发起。
func TestWebAPIScheduleFailureArchivedDisabled(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	f.launcher.agents = nil
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.setNow("2026-11-18T10:00:10Z")
	f.tick()
	runs := f.runs("a1")
	if len(runs) != 1 || runs[0].Status != ScheduleRunFailed || !strings.HasPrefix(runs[0].Error, "select_node：") || !strings.Contains(runs[0].Error, "webapi_http") || runs[0].TaskID != scheduleTaskID("a1", "2026-11-18T10:00:00Z") || f.nextRunAt("a1") != "2026-11-18T11:00:00Z" {
		t.Fatalf("failed run = %+v next=%s", runs, f.nextRunAt("a1"))
	}
	f.launcher.agents = []*types.AgentView{productAgent("node-a")}
	f.setNow("2026-11-18T10:30:00Z")
	f.tick()
	if len(f.runs("a1")) != 1 || len(f.launcher.opens) != 0 {
		t.Fatalf("failed run retried: %+v", f.runs("a1"))
	}
	// 资产校验失败（无授权以外的必填缺失）也记 failed，task_id 为空（任务未建）。
	f.do("PUT", "/api/webapi/assets/a2", `{"name":"空白资产","origins":["https://empty.example.test"]}`)
	f.putSchedule("a2", `{"cron":"0 * * * *"}`)
	// 归档资产：skipped。
	f.createAsset("a3")
	f.putSchedule("a3", `{"cron":"0 * * * *"}`)
	f.do("PUT", "/api/webapi/assets/a3", `{"name":"演示库存系统","origins":["http://example.test"],"archived":true}`)
	// 停用计划：不发起、不记录。
	f.createAsset("a4")
	f.putSchedule("a4", `{"cron":"0 * * * *","enabled":false}`)
	f.setNow("2026-11-18T11:00:10Z")
	f.tick()
	if runs := f.runs("a2"); len(runs) != 1 || runs[0].Status != ScheduleRunFailed || runs[0].TaskID != "" || !strings.HasPrefix(runs[0].Error, "validate：") {
		t.Fatalf("invalid asset run = %+v", runs)
	}
	if runs := f.runs("a3"); len(runs) != 1 || runs[0].Status != ScheduleRunSkipped || runs[0].Error != "资产已归档" || f.nextRunAt("a3") != "2026-11-18T12:00:00Z" {
		t.Fatalf("archived run = %+v", runs)
	}
	if len(f.runs("a4")) != 0 || f.nextRunAt("a4") != "" {
		t.Fatalf("disabled schedule ran: %+v", f.runs("a4"))
	}
	// a1 正常发起（节点已在线），a3/a4 均未产生 Open。
	if len(f.launcher.opens) != 1 || f.launcher.opens[0].NodeId != "node-a" {
		t.Fatalf("opens = %+v", f.launcher.opens)
	}
}

// 已有资产阶段（十五表）的旧库补两张计划表；完整新结构再次 Prepare 不变。
func TestWebAPIScheduleSchemaUpgradeFromAssetStage(t *testing.T) {
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	stage, err := webservice.NewSQLiteStore(database, webapiSchemaStage(15, 15))
	if err != nil {
		t.Fatal(err)
	}
	_ = stage.Close()
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("prepare asset stage: %v", err)
	}
	complete, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatalf("full native validation after prepare: %v", err)
	}
	_ = complete.Close()
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("complete schema not accepted: %v", err)
	}
	f := openAssetFixture(t, database)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/schedule", `{"cron":"0 3 * * *"}`); status != 201 {
		t.Fatalf("schedule after upgrade = %d %s", status, raw)
	}
}

// 调度器随 Capability 生命周期：Close 停止 goroutine 并返回；重复 Close 安全。
func TestWebAPISchedulerLifecycle(t *testing.T) {
	f := newAssetFixture(t)
	f.capability.startScheduler()
	f.capability.startScheduler()
	done := make(chan error, 1)
	go func() { done <- f.capability.Close(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not stop scheduler")
	}
	f.capability.stopScheduler()
	// 未启动过的 Capability 关闭后不会再启动。
	g := newAssetFixture(t)
	g.capability.stopScheduler()
	g.capability.startScheduler()
	if g.capability.sched.cancel != nil {
		t.Fatal("scheduler started after stop")
	}
}

// 真实原生 Sessions：会话不存在/绑定节点不在线 → 不算运行中。
func TestWebAPIScheduleSessionRunningRealSessions(t *testing.T) {
	f := newAssetFixture(t)
	store, err := webservice.NewSQLiteStore(f.database, SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	launcher := apiLauncher{api: webservice.NewService(webservice.ServiceConfig{Store: store}).API()}
	if running, err := launcher.SessionRunning(t.Context(), "missing"); err != nil || running {
		t.Fatalf("missing session = %v %v", running, err)
	}
	if err := store.CreateSession(t.Context(), &types.SessionRecord{Session: &aop.Session{Id: "s1", NodeId: "node-x", State: "open"}}); err != nil {
		t.Fatal(err)
	}
	user := &aop.Event{SessionId: "s1", TurnId: "turn-1", Id: "ev-1", Emitter: "cyber.web", Payload: &aop.Event_Message{Message: &aop.Message{Id: "m1", Role: "user", Content: []*aop.Content{{Value: &aop.Content_Text{Text: &aop.TextContent{Text: "开始探索任务。"}}}}}}}
	if _, _, err := store.AppendAOPEvent(t.Context(), "s1", user); err != nil {
		t.Fatal(err)
	}
	if running, err := launcher.SessionRunning(t.Context(), "s1"); err != nil || running {
		t.Fatalf("offline node session = %v %v", running, err)
	}
	// TurnEnded（计划后续“研究已结束”）：不存在会话/只有用户消息 → false；有 TurnEnded → true。
	if ended, err := launcher.TurnEnded(t.Context(), "missing"); err != nil || ended {
		t.Fatalf("missing session ended = %v %v", ended, err)
	}
	if ended, err := launcher.TurnEnded(t.Context(), "s1"); err != nil || ended {
		t.Fatalf("open turn ended = %v %v", ended, err)
	}
	end := &aop.Event{SessionId: "s1", TurnId: "turn-1", Id: "ev-2", Emitter: "cyber.web", Payload: &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{StopReason: "end_turn"}}}
	if _, _, err := store.AppendAOPEvent(t.Context(), "s1", end); err != nil {
		t.Fatal(err)
	}
	if ended, err := launcher.TurnEnded(t.Context(), "s1"); err != nil || !ended {
		t.Fatalf("ended turn = %v %v", ended, err)
	}
}
