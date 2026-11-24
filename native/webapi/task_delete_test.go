package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"github.com/uptrace/bun"
)

// 任务删除测试：临时库 + 合成中性数据（example.test），launcher 用替身，不调用模型。

var seedSeq atomic.Int64

// seedRow 按 information_schema 给 table 插一行：values 覆盖的列用给定值，其余非空列按类型填唯一合成值
// （text 为 "<列>-<序号>"、bigint 为序号、boolean false、bytea 空），可空列留 NULL；主键冲突跳过。
func seedRow(t *testing.T, store *taskStore, table string, values map[string]any) {
	t.Helper()
	ctx := context.Background()
	type column struct {
		Name     string `bun:"column_name"`
		Type     string `bun:"data_type"`
		Nullable string `bun:"is_nullable"`
	}
	var columns []column
	if err := store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewRaw(`SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = ? ORDER BY ordinal_position`, table).Scan(ctx, &columns)
	}); err != nil || len(columns) == 0 {
		t.Fatalf("columns of %s: %v %d", table, err, len(columns))
	}
	names, args := []string{}, []any{}
	for _, col := range columns {
		if col.Name == "tenant_id" {
			continue
		}
		value, ok := values[col.Name]
		if !ok {
			if col.Nullable == "YES" {
				continue
			}
			n := seedSeq.Add(1)
			switch col.Type {
			case "text":
				value = fmt.Sprintf("%s-%d", col.Name, n)
			case "bigint", "integer":
				value = n
			case "boolean":
				value = false
			case "bytea":
				value = []byte{}
			default:
				t.Fatalf("%s.%s: 未处理类型 %s", table, col.Name, col.Type)
			}
		}
		names = append(names, col.Name)
		args = append(args, value)
	}
	for name := range values {
		if !slices.Contains(names, name) {
			t.Fatalf("%s 没有列 %s", table, name)
		}
	}
	query := `INSERT INTO ` + table + ` (` + strings.Join(names, ", ") + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", ") + `) ON CONFLICT DO NOTHING`
	if _, err := store.db().Exec(query, args...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

func countRows(t *testing.T, store *taskStore, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := store.db().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// tableCounts 是全部 webapi_ 表与原生会话表的行数快照（判定“没有任何数据被删”）。
func tableCounts(t *testing.T, store *taskStore) map[string]int64 {
	t.Helper()
	var tables []string
	if err := store.read(context.Background(), func(ctx context.Context, db bun.IDB) error {
		return db.NewRaw(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND (table_name LIKE 'webapi\_%' OR table_name IN ('chat_sessions', 'chat_aop_events')) ORDER BY 1`).Scan(ctx, &tables)
	}); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int64{}
	for _, table := range tables {
		counts[table] = countRows(t, store, `SELECT COUNT(*) FROM `+table)
	}
	return counts
}

type deleteSeed struct {
	taskID, initial                  string
	sessions                         []string // 全部任务会话（字典序）
	finding, finding2                string
	verification, report, retest     string
	subsession, scheduleRun, auditID string
	notification, approval           string
}

// seedDeleteTask 建任务（PUT）并预置一条任务的全套数据；assetID 非空时挂到资产并建计划运行与后续。
func seedDeleteTask(t *testing.T, f *assetFixture, launcher *fakeLauncher, taskID, assetID string) deleteSeed {
	t.Helper()
	status, body, raw := f.do("PUT", "/api/webapi/tasks/"+taskID, `{"known_info":"合成目标","objective":"删除测试 `+taskID+`\n第二行","authorization":{"confirmed":true,"origins":["http://example.test"]}}`)
	if status != 201 {
		t.Fatalf("create task = %d %s", status, raw)
	}
	s := deleteSeed{taskID: taskID, initial: body["task"].(map[string]any)["initial_session_id"].(string)}
	s.finding, s.finding2 = taskID+"-f1", taskID+"-f2"
	s.verification, s.report, s.retest = taskID+"-v1", taskID+"-r1", taskID+"-rt1"
	s.subsession = taskID + "-sub"
	store := f.capability.store
	verifySession, reportSession, retestSession := taskID+"-vs", taskID+"-rs", taskID+"-rts"
	s.sessions = []string{s.initial, verifySession, reportSession, retestSession, s.subsession}
	slices.Sort(s.sessions)

	byTask := map[string]any{"task_id": taskID}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{"task_id": taskID}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	seedRow(t, store, "webapi_requests", with(map[string]any{"session_id": s.initial}))
	seedRow(t, store, "webapi_findings", with(map[string]any{"id": s.finding}))
	seedRow(t, store, "webapi_findings", with(map[string]any{"id": s.finding2}))
	seedRow(t, store, "webapi_finding_revisions", with(map[string]any{"finding_id": s.finding, "session_id": s.initial}))
	seedRow(t, store, "webapi_verifications", with(map[string]any{"id": s.verification, "finding_id": s.finding, "session_id": verifySession, "created_at": "2026-11-01T00:00:01Z"}))
	seedRow(t, store, "webapi_verification_results", map[string]any{"verification_id": s.verification, "session_id": verifySession})
	seedRow(t, store, "webapi_reports", with(map[string]any{"id": s.report, "session_id": reportSession, "created_at": "2026-11-01T00:00:02Z"}))
	seedRow(t, store, "webapi_report_drafts", map[string]any{"report_id": s.report, "session_id": reportSession})
	seedRow(t, store, "webapi_retests", with(map[string]any{"id": s.retest, "finding_id": s.finding, "session_id": retestSession, "created_at": "2026-11-01T00:00:03Z"}))
	seedRow(t, store, "webapi_retest_results", map[string]any{"retest_id": s.retest, "session_id": retestSession})
	for _, table := range []string{"webapi_egress_flows", "webapi_board_endpoints", "webapi_board_facts", "webapi_board_oracles", "webapi_board_oracle_events",
		"webapi_board_ingest", "webapi_authz_compares", "webapi_replays", "webapi_task_policy_events", "webapi_subagent_reservations", "webapi_subsession_usage"} {
		seedRow(t, store, table, byTask)
	}
	// 当前策略/场景/持续探索取合法值（其它任务删后仍要能正常读取）。
	seedRow(t, store, "webapi_task_policies", with(map[string]any{"scope": ScopeOrigins, "approval": ApprovalHighRisk}))
	seedRow(t, store, "webapi_task_scenarios", with(map[string]any{"scenario": ScenarioWebAPIPentest, "done_criteria": ""}))
	seedRow(t, store, "webapi_task_explore", with(map[string]any{"max_rounds": int64(exploreMinRounds)}))
	seedRow(t, store, "webapi_task_subsessions", with(map[string]any{"session_id": s.subsession, "parent_session_id": s.initial, "status": subsessionEnded, "created_at": "2026-11-01T00:00:04Z"}))
	s.auditID = taskID + "-audit"
	seedRow(t, store, "webapi_audit_events", with(map[string]any{"id": s.auditID}))
	s.notification = taskID + "-n1"
	seedRow(t, store, "webapi_notifications", with(map[string]any{"id": s.notification, "finding_id": s.finding, "verification_id": s.verification, "status": NotifySent}))
	s.approval = taskID + "-ap"
	seedRow(t, store, "webapi_approvals", with(map[string]any{"id": s.approval, "session_id": s.initial, "status": "approved", "decided_by": "user", "decided_at": "2026-11-01T00:00:05.000Z"}))
	if assetID != "" {
		s.scheduleRun = taskID + "-run"
		seedRow(t, store, "webapi_asset_tasks", with(map[string]any{"asset_id": assetID}))
		seedRow(t, store, "webapi_schedule_runs", with(map[string]any{"id": s.scheduleRun, "asset_id": assetID}))
		seedRow(t, store, "webapi_schedule_followups", with(map[string]any{"run_id": s.scheduleRun, "finding_id": s.finding}))
		seedRow(t, store, "webapi_schedule_run_followups", with(map[string]any{"run_id": s.scheduleRun, "asset_id": assetID}))
	}
	for _, session := range s.sessions {
		launcher.sessions[session] = nil
	}
	return s
}

func deleteFixture(t *testing.T) (*assetFixture, *fakeLauncher) {
	t.Helper()
	f := newAssetFixture(t)
	launcher := newFakeLauncher(productAgent("node-a"))
	f.capability.launcher = launcher
	return f, launcher
}

func auditActions(t *testing.T, store *taskStore, action string) []auditEventModel {
	t.Helper()
	var rows []auditEventModel
	if err := store.read(context.Background(), func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&rows).Where("action = ?", action).Scan(ctx)
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestTaskDeleteRemovesTaskDataAndKeepsHistory(t *testing.T) {
	f, launcher := deleteFixture(t)
	store := f.capability.store
	f.createAsset("a1")
	target := seedDeleteTask(t, f, launcher, "del-task", "a1")
	other := seedDeleteTask(t, f, launcher, "keep-task", "a1")

	// 漏洞：人工条目只含本任务成员（应删，连同处置）；auto 条目只含本任务成员（保留）；人工条目混有其它任务成员（保留）。
	seedRow(t, store, "webapi_asset_vulns", map[string]any{"id": "vm-only", "asset_id": "a1", "endpoint_key": ""})
	seedRow(t, store, "webapi_asset_vulns", map[string]any{"id": "va-auto", "asset_id": "a1", "endpoint_key": "GET http://example.test/a"})
	seedRow(t, store, "webapi_asset_vulns", map[string]any{"id": "vm-mixed", "asset_id": "a1", "endpoint_key": ""})
	seedRow(t, store, "webapi_vuln_triage", map[string]any{"vuln_key": "vm-only", "asset_id": "a1"})
	seedRow(t, store, "webapi_vuln_triage", map[string]any{"vuln_key": "va-auto", "asset_id": "a1"})
	seedRow(t, store, "webapi_asset_vuln_members", map[string]any{"finding_id": target.finding, "vuln_id": "vm-only", "asset_id": "a1", "task_id": target.taskID})
	seedRow(t, store, "webapi_asset_vuln_members", map[string]any{"finding_id": target.finding2, "vuln_id": "va-auto", "asset_id": "a1", "task_id": target.taskID})
	seedRow(t, store, "webapi_asset_vuln_members", map[string]any{"finding_id": other.finding, "vuln_id": "vm-mixed", "asset_id": "a1", "task_id": other.taskID})
	seedRow(t, store, "webapi_asset_vuln_members", map[string]any{"finding_id": other.finding2 + "-x", "vuln_id": "vm-mixed", "asset_id": "a1", "task_id": target.taskID})
	otherBefore := map[string]int64{}
	for _, table := range []string{"webapi_requests", "webapi_findings", "webapi_verifications", "webapi_reports", "webapi_retests", "webapi_task_subsessions", "webapi_board_facts", "webapi_asset_tasks"} {
		otherBefore[table] = countRows(t, store, `SELECT COUNT(*) FROM `+table+` WHERE task_id = ?`, other.taskID)
	}

	status, body, raw := f.do("DELETE", "/api/webapi/tasks/"+target.taskID, "")
	if status != 200 {
		t.Fatalf("delete = %d %s", status, raw)
	}
	var result TaskDeleteResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"requests": 1, "findings": 2, "verifications": 1, "reports": 1, "retests": 1, "egress_flows": 1, "board_endpoints": 1,
		"board_facts": 1, "board_oracles": 1, "authz_compares": 1, "subsessions": 1, "sessions": 5}
	if result.TaskID != target.taskID || fmt.Sprint(result.Deleted) != fmt.Sprint(want) || result.OrphanVulnsRemoved != 1 || len(body) != 3 {
		t.Fatalf("result = %s", raw)
	}

	// 会话：本任务五类会话全部删除（研究/验证/报告/复测/子 Agent），其它任务会话不动。
	if !slices.Equal(launcher.deleted, target.sessions) {
		t.Fatalf("deleted sessions = %v, want %v", launcher.deleted, target.sessions)
	}
	for _, session := range other.sessions {
		if _, ok := launcher.sessions[session]; !ok {
			t.Fatalf("other task session %s deleted", session)
		}
	}

	// 删表清空。
	for _, item := range taskDeleteTables {
		where := item.where
		if where == "" {
			where = "task_id = ?"
		}
		if n := countRows(t, store, `SELECT COUNT(*) FROM `+item.table+` WHERE `+where, target.taskID); n != 0 {
			t.Fatalf("%s still has %d rows", item.table, n)
		}
	}
	for _, check := range []struct{ query, id string }{
		{`SELECT COUNT(*) FROM webapi_verification_results WHERE verification_id = ?`, target.verification},
		{`SELECT COUNT(*) FROM webapi_report_drafts WHERE report_id = ?`, target.report},
		{`SELECT COUNT(*) FROM webapi_retest_results WHERE retest_id = ?`, target.retest},
		{`SELECT COUNT(*) FROM webapi_schedule_followups WHERE run_id = ?`, target.scheduleRun},
		{`SELECT COUNT(*) FROM webapi_asset_vuln_members WHERE task_id = ?`, target.taskID},
	} {
		if n := countRows(t, store, check.query, check.id); n != 0 {
			t.Fatalf("%s(%s) = %d", check.query, check.id, n)
		}
	}

	// 保留行：仍在、task_id 置空（推送还置空 finding_id）。
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_audit_events WHERE id = ? AND task_id = ''`, target.auditID); n != 1 {
		t.Fatalf("seeded audit row = %d", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_audit_events WHERE task_id = ?`, target.taskID); n != 0 {
		t.Fatalf("audit rows still linked = %d", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_audit_events WHERE action = 'task.create' AND target_id = ? AND task_id = ''`, target.taskID); n != 1 {
		t.Fatalf("task.create audit kept = %d", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_notifications WHERE id = ? AND task_id = '' AND finding_id = '' AND status = ?`, target.notification, NotifySent); n != 1 {
		t.Fatalf("notification kept = %d", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_schedule_runs WHERE id = ? AND task_id = '' AND asset_id = 'a1'`, target.scheduleRun); n != 1 {
		t.Fatalf("schedule run kept = %d", n)
	}
	// 审批记录保留并解除关联（同审计/推送）；其它任务的审批不动。
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_approvals WHERE id = ? AND task_id = '' AND status = 'approved'`, target.approval); n != 1 {
		t.Fatalf("approval row = %d", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_approvals WHERE id = ? AND task_id = ?`, other.approval, other.taskID); n != 1 {
		t.Fatalf("other approval row = %d", n)
	}

	// 漏洞：空人工条目与其处置删除；auto 与仍有成员的人工条目保留；资产仍在。
	for id, want := range map[string]int64{"vm-only": 0, "va-auto": 1, "vm-mixed": 1} {
		if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_asset_vulns WHERE id = ?`, id); n != want {
			t.Fatalf("vuln %s = %d, want %d", id, n, want)
		}
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_vuln_triage WHERE vuln_key = 'vm-only'`); n != 0 {
		t.Fatalf("orphan triage = %d", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_vuln_triage WHERE vuln_key = 'va-auto'`); n != 1 {
		t.Fatalf("auto triage = %d", n)
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_asset_vuln_members WHERE vuln_id = 'vm-mixed'`); n != 1 {
		t.Fatalf("mixed members = %d", n)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/a1", ""); status != 200 {
		t.Fatalf("asset = %d", status)
	}

	// 审计：新增一条 task.delete（user，target task/{id}，task_id/asset_id 为空），detail 含计数。
	deletes := auditActions(t, store, "task.delete")
	if len(deletes) != 1 {
		t.Fatalf("task.delete audits = %d", len(deletes))
	}
	entry := deletes[0]
	var detail map[string]any
	_ = json.Unmarshal([]byte(entry.DetailJSON), &detail)
	if entry.Actor != auditActorUser || entry.TargetKind != "task" || entry.TargetID != target.taskID || entry.TaskID != "" || entry.AssetID != "" ||
		entry.Summary != "删除任务：删除测试 del-task" || detail["task_id"] != target.taskID || detail["title"] != "删除测试 del-task" ||
		detail["sessions"] != float64(5) || detail["requests"] != float64(1) || detail["findings"] != float64(2) || detail["orphan_vulns_removed"] != float64(1) {
		t.Fatalf("audit = %+v detail=%v", entry, detail)
	}

	// 其它任务不受影响。
	for table, n := range otherBefore {
		if got := countRows(t, store, `SELECT COUNT(*) FROM `+table+` WHERE task_id = ?`, other.taskID); got != n || n == 0 {
			t.Fatalf("other task %s = %d, want %d", table, got, n)
		}
	}
	if status, _, _ := f.do("GET", "/api/webapi/tasks/"+other.taskID, ""); status != 200 {
		t.Fatalf("other task = %d", status)
	}
	if status, _, _ := f.do("GET", "/api/webapi/tasks/"+target.taskID, ""); status != 404 {
		t.Fatalf("deleted task get = %d", status)
	}
	// 再删 404，且不重复登记。
	if status, _, raw := f.do("DELETE", "/api/webapi/tasks/"+target.taskID, ""); status != 404 {
		t.Fatalf("second delete = %d %s", status, raw)
	}
	if n := len(auditActions(t, store, "task.delete")); n != 1 {
		t.Fatalf("task.delete audits after 404 = %d", n)
	}
}

func TestTaskDeleteInFlightConflict(t *testing.T) {
	for _, tc := range []struct {
		name string
		mark func(t *testing.T, f *assetFixture, l *fakeLauncher, s deleteSeed)
	}{
		{"research session running", func(t *testing.T, f *assetFixture, l *fakeLauncher, s deleteSeed) { l.running[s.initial] = true }},
		{"verification session running", func(t *testing.T, f *assetFixture, l *fakeLauncher, s deleteSeed) { l.running[s.taskID+"-vs"] = true }},
		{"subagent running", func(t *testing.T, f *assetFixture, l *fakeLauncher, s deleteSeed) {
			if _, err := f.capability.store.db().Exec(`UPDATE webapi_task_subsessions SET status = ? WHERE session_id = ?`, subsessionRunning, s.subsession); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, launcher := deleteFixture(t)
			f.createAsset("a1")
			seed := seedDeleteTask(t, f, launcher, "busy-task", "a1")
			tc.mark(t, f, launcher, seed)
			before := tableCounts(t, f.capability.store)
			status, body, raw := f.do("DELETE", "/api/webapi/tasks/"+seed.taskID, "")
			if status != 409 || body["code"] != "in_flight" || body["error"] == "" {
				t.Fatalf("delete = %d %s", status, raw)
			}
			if after := tableCounts(t, f.capability.store); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("data changed:\nbefore %v\nafter  %v", before, after)
			}
			if len(launcher.deleted) != 0 || len(auditActions(t, f.capability.store, "task.delete")) != 0 {
				t.Fatalf("side effects: sessions %v", launcher.deleted)
			}
		})
	}
}

func TestTaskDeleteValidation(t *testing.T) {
	f, launcher := deleteFixture(t)
	if status, _, raw := f.do("DELETE", "/api/webapi/tasks/missing-task", ""); status != 404 {
		t.Fatalf("missing = %d %s", status, raw)
	}
	if status, _, raw := f.do("DELETE", "/api/webapi/tasks/bad%25id", ""); status != 400 {
		t.Fatalf("bad id = %d %s", status, raw)
	}
	seedDeleteTask(t, f, launcher, "body-task", "")
	for _, body := range []string{`{"force":true}`, `[]`, `x`, `{} {}`} {
		if status, _, raw := f.do("DELETE", "/api/webapi/tasks/body-task", body); status != 400 {
			t.Fatalf("body %q = %d %s", body, status, raw)
		}
	}
	if len(launcher.deleted) != 0 || countRows(t, f.capability.store, `SELECT COUNT(*) FROM webapi_tasks WHERE id = 'body-task'`) != 1 {
		t.Fatalf("validation failure deleted data: %v", launcher.deleted)
	}
	// 空对象与空白 body 等同空 body（同 decodeJSONBody allowEmpty 规则）。
	if status, _, raw := f.do("DELETE", "/api/webapi/tasks/body-task", ` {} `); status != 200 {
		t.Fatalf("empty object = %d %s", status, raw)
	}
	// 无资产任务：没有资产关联也能删，计划运行等无行。
	if n := countRows(t, f.capability.store, `SELECT COUNT(*) FROM webapi_requests WHERE task_id = 'body-task'`); n != 0 {
		t.Fatalf("requests left = %d", n)
	}
}

func TestTaskDeleteSessionFailureKeepsData(t *testing.T) {
	f, launcher := deleteFixture(t)
	f.createAsset("a1")
	seed := seedDeleteTask(t, f, launcher, "fail-task", "a1")
	// 第三个（字典序）会话删除失败：前两个已删，事务不开始。
	failing := seed.sessions[2]
	launcher.deleteErr = map[string]error{failing: errors.New("合成故障")}
	before := tableCounts(t, f.capability.store)
	status, body, raw := f.do("DELETE", "/api/webapi/tasks/"+seed.taskID, "")
	if status != 500 || body["code"] != "session_delete_failed" || body["sessions_deleted"] != float64(2) || !strings.Contains(body["error"].(string), "已删除 2 个会话") {
		t.Fatalf("delete = %d %s", status, raw)
	}
	if after := tableCounts(t, f.capability.store); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("webapi data changed:\nbefore %v\nafter  %v", before, after)
	}
	if len(auditActions(t, f.capability.store, "task.delete")) != 0 {
		t.Fatal("audited failed delete")
	}
	// 故障消除后重试：已删会话按“已不存在”跳过（不计数），其余删除并完成。
	launcher.deleteErr = nil
	status, _, raw = f.do("DELETE", "/api/webapi/tasks/"+seed.taskID, "")
	var result TaskDeleteResult
	_ = json.Unmarshal([]byte(raw), &result)
	if status != 200 || result.Deleted["sessions"] != 3 || len(auditActions(t, f.capability.store, "task.delete")) != 1 {
		t.Fatalf("retry = %d %s", status, raw)
	}
	if countRows(t, f.capability.store, `SELECT COUNT(*) FROM webapi_tasks WHERE id = ?`, seed.taskID) != 0 {
		t.Fatal("task not deleted on retry")
	}
}

func TestTaskDeleteRollsBackWhenSessionsAppear(t *testing.T) {
	f, launcher := deleteFixture(t)
	seed := seedDeleteTask(t, f, launcher, "race-task", "")
	// 删会话期间并发出现一个新验证会话（模拟并发发起）：写事务复核时整体回滚，不登记审计。
	launcher.deleteHook = func(string) {
		launcher.deleteHook = nil
		seedRow(t, f.capability.store, "webapi_verifications", map[string]any{"task_id": seed.taskID, "id": "race-v2", "finding_id": seed.finding, "session_id": "race-new-session"})
	}
	status, body, raw := f.do("DELETE", "/api/webapi/tasks/"+seed.taskID, "")
	if status != 500 || body["code"] != "sessions_changed" || body["sessions_deleted"] != float64(len(seed.sessions)) {
		t.Fatalf("delete = %d %s", status, raw)
	}
	if countRows(t, f.capability.store, `SELECT COUNT(*) FROM webapi_tasks WHERE id = ?`, seed.taskID) != 1 ||
		countRows(t, f.capability.store, `SELECT COUNT(*) FROM webapi_requests WHERE task_id = ?`, seed.taskID) != 1 ||
		len(auditActions(t, f.capability.store, "task.delete")) != 0 {
		t.Fatal("transaction not rolled back")
	}
	// 重试把新会话也删掉并完成。
	status, _, raw = f.do("DELETE", "/api/webapi/tasks/"+seed.taskID, "")
	if status != 200 || !slices.Contains(launcher.deleted, "race-new-session") {
		t.Fatalf("retry = %d %s deleted=%v", status, raw, launcher.deleted)
	}
}

// 真实原生 Sessions：apiLauncher.DeleteSession 删除会话并连带事件（ON DELETE CASCADE）；会话不存在返回 (false, nil)。
func TestTaskDeleteRealNativeSession(t *testing.T) {
	f := newAssetFixture(t)
	store, err := webservice.NewSQLiteStore(f.database.DSN(), SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	launcher := apiLauncher{api: webservice.NewService(webservice.ServiceConfig{Store: store}).API()}
	for _, id := range []string{"s-del", "s-keep"} {
		if err := store.CreateSession(t.Context(), &types.SessionRecord{Session: &aop.Session{Id: id, NodeId: "node-x", State: "open"}}); err != nil {
			t.Fatal(err)
		}
		event := &aop.Event{SessionId: id, TurnId: "turn-1", Id: "ev-" + id, Emitter: "cyber.web", Payload: &aop.Event_Message{Message: &aop.Message{Id: "m-" + id, Role: "user", Content: []*aop.Content{{Value: &aop.Content_Text{Text: &aop.TextContent{Text: "合成消息"}}}}}}}
		if _, _, err := store.AppendAOPEvent(t.Context(), id, event); err != nil {
			t.Fatal(err)
		}
	}
	if deleted, err := launcher.DeleteSession(t.Context(), "s-del"); err != nil || !deleted {
		t.Fatalf("delete = %v %v", deleted, err)
	}
	if deleted, err := launcher.DeleteSession(t.Context(), "s-del"); err != nil || deleted {
		t.Fatalf("delete again = %v %v", deleted, err)
	}
	if session, err := launcher.Session(t.Context(), "s-del"); err != nil || session != nil {
		t.Fatalf("session after delete = %v %v", session, err)
	}
	for id, want := range map[string]int64{"s-del": 0, "s-keep": 1} {
		if n := countRows(t, f.capability.store, `SELECT COUNT(*) FROM chat_aop_events WHERE session_id = ?`, id); n != want {
			t.Fatalf("events of %s = %d, want %d", id, n, want)
		}
	}
}

// 无资产任务：候选在全局漏洞列表以 finding:<id> 为键，删除任务连带删除本任务候选的处置；其它任务（含同前缀）处置不动。
func TestTaskDeleteStandaloneFindingTriage(t *testing.T) {
	f, launcher := deleteFixture(t)
	store := f.capability.store
	target := seedDeleteTask(t, f, launcher, "solo-del", "")
	other := seedDeleteTask(t, f, launcher, "solo-keep", "")
	for _, key := range []string{"finding:" + target.finding, "finding:" + target.finding2, "finding:" + other.finding} {
		seedRow(t, store, "webapi_vuln_triage", map[string]any{"vuln_key": key, "asset_id": ""})
	}
	status, _, raw := f.do("DELETE", "/api/webapi/tasks/"+target.taskID, "")
	var result TaskDeleteResult
	_ = json.Unmarshal([]byte(raw), &result)
	if status != 200 || result.OrphanVulnsRemoved != 0 || result.Deleted["findings"] != 2 {
		t.Fatalf("delete = %d %s", status, raw)
	}
	for key, want := range map[string]int64{"finding:" + target.finding: 0, "finding:" + target.finding2: 0, "finding:" + other.finding: 1} {
		if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_vuln_triage WHERE vuln_key = ?`, key); n != want {
			t.Fatalf("triage %s = %d, want %d", key, n, want)
		}
	}
	if n := countRows(t, store, `SELECT COUNT(*) FROM webapi_findings WHERE task_id = ?`, other.taskID); n != 2 {
		t.Fatalf("other findings = %d", n)
	}
}
