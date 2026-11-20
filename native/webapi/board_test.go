package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/proto"
)

// 任务黑板（M2）测试：全部为临时库合成数据（example.test 虚构接口与字段），不发送任何真实请求。

type boardFixture struct {
	t     *testing.T
	cap   *Capability
	mux   *http.ServeMux
	task  *Task
	other *Task
	inv   operation.Invocation
	call  int
	base  time.Time
}

func newBoardFixture(t *testing.T) *boardFixture {
	t.Helper()
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	native, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = native.Close() })
	return openBoardFixture(t, database)
}

func openBoardFixture(t *testing.T, database string) *boardFixture {
	t.Helper()
	capability, err := NewCapability(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close(context.Background()) })
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	task, err := capability.store.Save(t.Context(), "task-board", "演示商城 API", "检查越权", []string{"https://api.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := capability.store.Save(t.Context(), "task-other", "另一个演示", "其他任务", []string{"https://other.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := time.Parse(time.RFC3339, "2026-11-20T01:00:00Z")
	return &boardFixture{t: t, cap: capability, mux: mux, task: task.Task, other: other.Task,
		inv: operation.Invocation{SessionID: task.Task.InitialSessionID, TurnID: "turn-1"}, base: base}
}

func (f *boardFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.cap.store.db.Exec(query, args...); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
}

// request 写一条合成证据行。status=0 表示无响应；completed=false 表示仍在认领中。
func (f *boardFixture) request(taskID, id, method, rawURL string, headers map[string]string, body string, status int, sent, completed bool, created time.Time) {
	f.t.Helper()
	req, err := http.NewRequest(method, rawURL, strings.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	var resp *http.Response
	if status != 0 {
		resp = &http.Response{StatusCode: status, Status: fmt.Sprintf("%d X", status), Header: http.Header{}}
	}
	flow := traffic.FlowFromHTTP(req, resp, []byte(body), nil)
	flow.Id = id
	flowPB, err := proto.Marshal(flow)
	if err != nil {
		f.t.Fatal(err)
	}
	model := requestModel{ID: id, TaskID: taskID, SessionID: "s", TurnID: "t", CallID: "c-" + id, Fingerprint: "fp-" + id, Method: method, URL: rawURL,
		Status: requestCompleted, Preview: []byte("演示"), FlowPB: flowPB, CreatedAt: created.UTC().Format(time.RFC3339Nano)}
	if !sent {
		model.Status, model.Sent = "denied", sql.NullBool{Bool: false, Valid: true}
	} else if status != 0 {
		model.Sent, model.StatusCode = sql.NullBool{Bool: true, Valid: true}, sql.NullInt64{Int64: int64(status), Valid: true}
	} else {
		model.Status = requestUnknown
	}
	if completed {
		model.CompletedAt = sql.NullString{String: created.Add(time.Second).UTC().Format(time.RFC3339Nano), Valid: true}
	} else {
		model.Status, model.Sent, model.StatusCode = "claimed", sql.NullBool{}, sql.NullInt64{}
	}
	if _, err := f.cap.store.orm.NewInsert().Model(&model).Exec(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
}

func (f *boardFixture) at(minutes int) time.Time {
	return f.base.Add(time.Duration(minutes) * time.Minute)
}

func (f *boardFixture) ingest(now time.Time) {
	f.t.Helper()
	if err := f.cap.store.boardIngest(f.t.Context(), f.task.ID, now); err != nil {
		f.t.Fatal(err)
	}
}

func (f *boardFixture) endpoints(untested bool) map[string]BoardEndpoint {
	f.t.Helper()
	items, err := f.cap.store.boardEndpoints(f.t.Context(), f.task.ID, boardEndpointFilter{untested: untested, limit: 500})
	if err != nil {
		f.t.Fatal(err)
	}
	out := map[string]BoardEndpoint{}
	for _, item := range items {
		out[item.EndpointKey] = item
	}
	return out
}

// tool 经真实工具 Execute 路径执行；每次新的 call_id。
func (f *boardFixture) tool(tool coretool.Tool, inv operation.Invocation, arguments string) (string, bool) {
	f.t.Helper()
	f.call++
	if inv.CallID == "" {
		inv.CallID = fmt.Sprintf("board-call-%d", f.call)
	}
	result, err := tool.Execute(operation.ContextWithInvocation(f.t.Context(), inv), arguments)
	if err != nil {
		f.t.Fatal(err)
	}
	return coretool.ResultText(result), result.IsError
}

func (f *boardFixture) mustTool(tool coretool.Tool, arguments string) map[string]any {
	f.t.Helper()
	text, isError := f.tool(tool, f.inv, arguments)
	if isError {
		f.t.Fatalf("%s %s: %s", tool.Name(), arguments, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		f.t.Fatalf("decode %s: %v", text, err)
	}
	return out
}

func (f *boardFixture) toolError(tool coretool.Tool, inv operation.Invocation, arguments, want string) {
	f.t.Helper()
	text, isError := f.tool(tool, inv, arguments)
	if !isError || !strings.Contains(text, want) {
		f.t.Fatalf("%s %s = %v %q, want error containing %q", tool.Name(), arguments, isError, text, want)
	}
}

func (f *boardFixture) do(method, path, body string) (int, map[string]any) {
	f.t.Helper()
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &out)
	return recorder.Code, out
}

func (f *boardFixture) oracle(number int64) *BoardOracle {
	f.t.Helper()
	oracle, err := f.cap.store.boardOracle(f.t.Context(), f.task.ID, number)
	if err != nil || oracle == nil {
		f.t.Fatalf("oracle %d: %+v %v", number, oracle, err)
	}
	return oracle
}

func (f *boardFixture) finding(id, taskID string, number int64) {
	f.exec(`INSERT INTO webapi_findings (id, task_id, number, status, current_revision, current_revision_id, created_at, updated_at) VALUES (?, ?, ?, 'candidate', 1, ?, '2026-11-20T00:00:00Z', '2026-11-20T00:00:00Z')`,
		id, taskID, number, "r-"+id)
}

func eventPath(oracle *BoardOracle) string {
	parts := []string{}
	for _, event := range oracle.Events {
		parts = append(parts, event.From+">"+event.To+"@"+event.Actor)
	}
	return strings.Join(parts, ",")
}

var (
	boardUsersKey  = "GET https://api.example.test/api/users/{id}"
	boardOrdersKey = "POST https://api.example.test/api/orders"
	boardLoginKey  = "POST https://api.example.test/api/login"
)

// seedTraffic：用户详情两次（带/不带 Authorization，200/403）、下单 JSON、登录表单、拒绝与无响应各一。
func (f *boardFixture) seedTraffic() {
	f.request(f.task.ID, "we_users1", "GET", "https://api.example.test/api/users/123?page=1&size=2", map[string]string{"Authorization": "Bearer demo"}, "", 200, true, true, f.at(1))
	f.request(f.task.ID, "we_users2", "GET", "https://api.example.test/api/users/0f1e2d3c4b5a69788796a5b4c3d2e1f0/?sort=name", nil, "", 403, true, true, f.at(2))
	f.request(f.task.ID, "we_orders", "POST", "https://api.example.test/api/orders", map[string]string{"Content-Type": "application/json", "Cookie": "sid=demo"}, `{"item_id":1,"qty":2}`, 201, true, true, f.at(3))
	f.request(f.task.ID, "we_login", "POST", "https://api.example.test/api/login", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, "username=a&password=b", 302, true, true, f.at(4))
	f.request(f.task.ID, "we_denied", "GET", "https://api.example.test/api/denied", nil, "", 0, false, true, f.at(5))
	f.request(f.task.ID, "we_unknown", "GET", "https://api.example.test/api/unknown", nil, "", 0, true, true, f.at(6))
	f.request(f.other.ID, "we_other", "GET", "https://other.example.test/api/x", nil, "", 200, true, true, f.at(1))
}

func TestBoardIngestTrafficIncrementalIdempotent(t *testing.T) {
	f := newBoardFixture(t)
	f.seedTraffic()
	f.ingest(f.at(30))
	got := f.endpoints(false)
	if len(got) != 3 {
		t.Fatalf("endpoints = %+v", got)
	}
	users := got[boardUsersKey]
	if users.Path != "/api/users/{id}" || users.Origin != "https://api.example.test" || users.Method != "GET" || users.RequestCount != 2 ||
		strings.Join(users.Params, ",") != "page,size,sort" || fmt.Sprint(users.StatusCodes) != "[200 403]" || users.AuthSeen != boardAuthBoth ||
		users.Source != boardSourceTraffic || users.FirstSeenAt != "2026-11-20T01:01:00.000Z" || users.LastSeenAt != "2026-11-20T01:02:00.000Z" {
		t.Fatalf("users = %+v", users)
	}
	if users.ID != boardEndpointID(f.task.ID, boardUsersKey) {
		t.Fatalf("endpoint id not UUIDv5(task,key): %s", users.ID)
	}
	if orders := got[boardOrdersKey]; strings.Join(orders.Params, ",") != "item_id,qty" || orders.AuthSeen != boardAuthWith || fmt.Sprint(orders.StatusCodes) != "[201]" {
		t.Fatalf("orders = %+v", orders)
	}
	if login := got[boardLoginKey]; strings.Join(login.Params, ",") != "password,username" || login.AuthSeen != boardAuthNone {
		t.Fatalf("login = %+v", login)
	}
	// 再次摄取幂等。
	f.ingest(f.at(31))
	if again := f.endpoints(false); again[boardUsersKey].RequestCount != 2 || len(again) != 3 {
		t.Fatalf("not idempotent: %+v", again)
	}

	// 认领中且新近的请求挡住游标；其后已完成的请求等它完成后一起并入。
	f.request(f.task.ID, "we_inflight", "GET", "https://api.example.test/api/slow", nil, "", 0, true, false, f.at(40))
	f.request(f.task.ID, "we_after", "GET", "https://api.example.test/api/users/9", nil, "", 500, true, true, f.at(41))
	f.ingest(f.at(41))
	if again := f.endpoints(false); again[boardUsersKey].RequestCount != 2 {
		t.Fatalf("passed in-flight row: %+v", again[boardUsersKey])
	}
	f.exec(`UPDATE webapi_requests SET status = 'completed', sent = 1, status_code = 200, completed_at = ? WHERE id = 'we_inflight'`, f.at(42).Format(time.RFC3339Nano))
	f.ingest(f.at(42))
	again := f.endpoints(false)
	if again[boardUsersKey].RequestCount != 3 || fmt.Sprint(again[boardUsersKey].StatusCodes) != "[200 403 500]" || again["GET https://api.example.test/api/slow"].RequestCount != 1 {
		t.Fatalf("after completion: %+v", again)
	}
	// 早已卡住（进程中断遗留）的认领行被跳过，不挡后续。
	f.request(f.task.ID, "we_stuck", "GET", "https://api.example.test/api/stuck", nil, "", 0, true, false, f.at(50))
	f.request(f.task.ID, "we_later", "GET", "https://api.example.test/api/later", nil, "", 200, true, true, f.at(51))
	f.ingest(f.at(70))
	again = f.endpoints(false)
	if _, ok := again["GET https://api.example.test/api/later"]; !ok {
		t.Fatalf("stuck row blocked ingest: %+v", again)
	}
	if _, ok := again["GET https://api.example.test/api/stuck"]; ok {
		t.Fatalf("stuck row ingested")
	}
	for _, key := range []string{"GET https://api.example.test/api/denied", "GET https://api.example.test/api/unknown", "GET https://other.example.test/api/x"} {
		if _, ok := again[key]; ok {
			t.Fatalf("%s should not be in map", key)
		}
	}
	var cursor boardIngestModel
	if err := f.cap.store.orm.NewSelect().Model(&cursor).Where("task_id = ?", f.task.ID).Scan(t.Context()); err != nil || cursor.LastID != "we_later" {
		t.Fatalf("cursor = %+v %v", cursor, err)
	}
}

func TestBoardToolsPermissionsMapAndFacts(t *testing.T) {
	f := newBoardFixture(t)
	f.seedTraffic()
	read, mapTool, factTool, oracleTool := &boardReadTool{store: f.cap.store}, &boardMapTool{store: f.cap.store}, &boardFactTool{store: f.cap.store}, &boardOracleTool{store: f.cap.store}
	for _, tool := range []coretool.Tool{read, mapTool, factTool, oracleTool} {
		if definition := tool.Definition(); definition == nil || definition.Name != tool.Name() {
			t.Fatalf("definition %s = %+v", tool.Name(), definition)
		}
	}
	// 验证会话：可读不可写。
	f.exec(`INSERT INTO webapi_verifications (id, task_id, finding_id, source_revision_id, session_id, created_at) VALUES ('v1', ?, 'wf_x', 'wfr_x', 's-verify', '2026-11-20T00:00:00Z')`, f.task.ID)
	verifyInv := operation.Invocation{SessionID: "s-verify", TurnID: "vt"}
	if text, isError := f.tool(read, verifyInv, `{"section":"endpoints"}`); isError || !strings.Contains(text, boardUsersKey) || !strings.Contains(text, "auth=both") {
		t.Fatalf("verify read = %v %s", isError, text)
	}
	f.toolError(mapTool, verifyInv, `{"endpoints":[{"method":"GET","url":"https://api.example.test/x"}]}`, "只有本任务研究会话可写黑板")
	f.toolError(factTool, verifyInv, `{"add":[{"kind":"tech","subject":"框架","statement":"响应头显示演示框架","confidence":"inferred"}]}`, "只有本任务研究会话可写黑板")
	f.toolError(oracleTool, verifyInv, `{"action":"propose","items":[{"question":"q","category":"authz","endpoint_keys":[],"priority":"low"}]}`, "只有本任务研究会话可写黑板")
	f.toolError(read, operation.Invocation{SessionID: "s-unknown", TurnID: "t"}, `{"section":"summary"}`, "session无已保存任务")
	f.toolError(read, operation.Invocation{}, `{"section":"summary"}`, "缺少可信原生session/turn/call上下文")
	f.toolError(read, f.inv, `{"section":"endpoints","status":"open"}`, "status 仅支持 untested")
	f.toolError(read, f.inv, `{"section":"oracles","limit":101}`, "limit 须为 1..100")
	f.toolError(read, f.inv, `{"section":"facts","extra":1}`, `含未知字段 "extra"`)

	// 地图：模型补充未请求接口；ID 段归一，已存在的键不重复。
	mapped := f.mustTool(mapTool, `{"endpoints":[{"method":"get","url":"https://api.example.test/api/admin/export?fmt=csv","note":"main.js 中出现"},{"method":"GET","url":"https://api.example.test/api/users/77"}]}`)
	results := mapped["endpoints"].([]any)
	if first := results[0].(map[string]any); first["endpoint_key"] != "GET https://api.example.test/api/admin/export" || first["created"] != true || first["source"] != "model" {
		t.Fatalf("map = %+v", mapped)
	}
	if second := results[1].(map[string]any); second["endpoint_key"] != boardUsersKey || second["created"] != false || second["source"] != "traffic" {
		t.Fatalf("map existing = %+v", mapped)
	}
	export := f.endpoints(false)["GET https://api.example.test/api/admin/export"]
	if export.RequestCount != 0 || export.AuthSeen != "" || export.Note != "main.js 中出现" || export.Source != boardSourceModel {
		t.Fatalf("model endpoint = %+v", export)
	}
	f.toolError(mapTool, f.inv, `{"endpoints":[{"method":"GET","url":"ftp://api.example.test/x"}]}`, "endpoints[0] url 须为无 userinfo 的绝对 http(s) URL")
	f.toolError(mapTool, f.inv, `{"endpoints":[]}`, "endpoints 数量须为 1..50")
	// 之后被实际请求的模型端点转为流量来源。
	f.request(f.task.ID, "we_export", "GET", "https://api.example.test/api/admin/export", nil, "", 401, true, true, f.at(20))
	f.mustTool(mapTool, `{"endpoints":[{"method":"GET","url":"https://api.example.test/api/admin/export"}]}`)
	if export = f.endpoints(false)["GET https://api.example.test/api/admin/export"]; export.Source != boardSourceTraffic || export.RequestCount != 1 || export.Note != "main.js 中出现" || export.AuthSeen != boardAuthNone {
		t.Fatalf("model endpoint after traffic = %+v", export)
	}

	// 事实：confirmed 无证据、跨任务证据、未发送证据均拒；整批不落库。
	f.toolError(factTool, f.inv, `{"add":[{"kind":"auth","subject":"会话","statement":"使用 Bearer token","confidence":"confirmed"}]}`, "add[0].evidence_ids 数量不足")
	f.toolError(factTool, f.inv, `{"add":[{"kind":"auth","subject":"会话","statement":"使用 Bearer token","confidence":"confirmed","evidence_ids":["we_other"]}]}`, `add[0].evidence_ids[0]="we_other" 不是本任务的证据`)
	f.toolError(factTool, f.inv, `{"add":[{"kind":"auth","subject":"会话","statement":"使用 Bearer token","confidence":"confirmed","evidence_ids":["we_denied"]}]}`, "未实际发送")
	f.toolError(factTool, f.inv, `{"add":[{"kind":"secret","subject":"x","statement":"y","confidence":"inferred"}]}`, "add[0].kind 仅支持")
	f.toolError(factTool, f.inv, `{"add":[{"kind":"tech","subject":"`+strings.Repeat("长", 201)+`","statement":"y","confidence":"inferred"}]}`, "add[0].subject 超长：实际 201 字，上限 200 字")
	f.toolError(factTool, f.inv, `{}`, "add 与 retract 至少提供一项")
	f.toolError(factTool, f.inv, `{"add":[{"kind":"tech","subject":"x","statement":"y","confidence":"inferred"}],"retract":["bf_missing"]}`, `retract[0]="bf_missing" 不是本任务的事实 id`)
	if facts, _ := f.cap.store.boardFacts(t.Context(), f.task.ID, boardFactFilter{limit: 100}); len(facts) != 0 {
		t.Fatalf("failed batch persisted facts: %+v", facts)
	}
	factInv := f.inv
	factInv.CallID = "fact-call-1"
	text, isError := f.tool(factTool, factInv, `{"add":[{"kind":"auth","subject":"会话","statement":"API 使用 Bearer token","confidence":"confirmed","evidence_ids":["we_users1"]},{"kind":"tech","subject":"框架","statement":"可能是演示框架","confidence":"inferred"}]}`)
	var written boardFactWrite
	if isError || json.Unmarshal([]byte(text), &written) != nil || len(written.Added) != 2 {
		t.Fatalf("facts = %v %s", isError, text)
	}
	// 同一 call 重放不重复新增。
	if text, isError = f.tool(factTool, factInv, `{"add":[{"kind":"auth","subject":"会话","statement":"API 使用 Bearer token","confidence":"confirmed","evidence_ids":["we_users1"]},{"kind":"tech","subject":"框架","statement":"可能是演示框架","confidence":"inferred"}]}`); isError {
		t.Fatalf("replay = %s", text)
	}
	if facts, _ := f.cap.store.boardFacts(t.Context(), f.task.ID, boardFactFilter{limit: 100}); len(facts) != 2 || facts[0].SessionID != f.inv.SessionID || facts[0].CallID != "fact-call-1" {
		t.Fatalf("facts after replay = %+v", facts)
	}
	retracted := f.mustTool(factTool, `{"retract":["`+written.Added[1]+`"]}`)
	if fmt.Sprint(retracted["retracted"]) != "["+written.Added[1]+"]" {
		t.Fatalf("retract = %+v", retracted)
	}
	if again := f.mustTool(factTool, `{"retract":["`+written.Added[1]+`"]}`); fmt.Sprint(again["already_retracted"]) != "["+written.Added[1]+"]" {
		t.Fatalf("retract again = %+v", again)
	}
	text, _ = f.tool(read, verifyInv, `{"section":"facts"}`)
	if !strings.Contains(text, "API 使用 Bearer token") || strings.Contains(text, "可能是演示框架") || !strings.Contains(text, "we_users1") {
		t.Fatalf("read facts = %s", text)
	}
	if text, _ = f.tool(read, verifyInv, `{"section":"facts","status":"retracted","category":"tech"}`); !strings.Contains(text, "可能是演示框架") {
		t.Fatalf("read retracted = %s", text)
	}
}

func TestBoardOracleLifecycleAndDigest(t *testing.T) {
	f := newBoardFixture(t)
	f.seedTraffic()
	f.finding("wf_mine", f.task.ID, 1)
	f.finding("wf_theirs", f.other.ID, 2)
	oracleTool, read := &boardOracleTool{store: f.cap.store}, &boardReadTool{store: f.cap.store}
	proposed := f.mustTool(oracleTool, `{"action":"propose","items":[
		{"question":"用户A的token能否读取用户B的资料？","category":"authz","endpoint_keys":["`+boardUsersKey+`","DELETE https://api.example.test/api/users/789"],"priority":"high"},
		{"question":"登录接口是否可被 SQL 注入绕过？","category":"injection","endpoint_keys":["`+boardLoginKey+`"],"priority":"medium"},
		{"question":"下单是否需要第二个账号才能验证？","category":"logic","endpoint_keys":[],"priority":"low"},
		{"question":"导出接口是否存在？","category":"info_disclosure","endpoint_keys":["GET https://api.example.test/api/export"],"priority":"low"}]}`)
	items := proposed["items"].([]any)
	for index, want := range []string{"O-001", "O-002", "O-003", "O-004"} {
		if item := items[index].(map[string]any); item["number"] != want || item["created"] != true || item["status"] != oracleOpen {
			t.Fatalf("propose = %+v", proposed)
		}
	}
	first := f.oracle(1)
	if strings.Join(first.EndpointKeys, ",") != "DELETE https://api.example.test/api/users/{id},"+boardUsersKey || first.Origin != "model" {
		t.Fatalf("O-001 = %+v", first)
	}
	if deleteEndpoint := f.endpoints(false)["DELETE https://api.example.test/api/users/{id}"]; deleteEndpoint.Source != boardSourceModel {
		t.Fatalf("referenced endpoint not added to map: %+v", deleteEndpoint)
	}
	// 去重：大小写/空白/标点不同、端点换序与非规范写法 → 返回已有编号。
	duplicate := f.mustTool(oracleTool, `{"action":"propose","items":[{"question":"  用户a的Token 能否读取用户B的资料 ","category":"authz","endpoint_keys":["DELETE https://API.example.test:443/api/users/1/","GET https://api.example.test/api/users/{id}"],"priority":"low"}]}`)
	if item := duplicate["items"].([]any)[0].(map[string]any); item["number"] != "O-001" || item["created"] != false {
		t.Fatalf("dedupe = %+v", duplicate)
	}
	if other := f.mustTool(oracleTool, `{"action":"propose","items":[{"question":"用户A的token能否读取用户B的资料？","category":"authn","endpoint_keys":["`+boardUsersKey+`","DELETE https://api.example.test/api/users/789"],"priority":"high"}]}`); other["items"].([]any)[0].(map[string]any)["number"] != "O-005" {
		t.Fatalf("different category deduped: %+v", other)
	}
	f.toolError(oracleTool, f.inv, `{"action":"propose","items":[{"question":"x","category":"rce","endpoint_keys":[],"priority":"low"}]}`, "items[0].category 仅支持")
	f.toolError(oracleTool, f.inv, `{"action":"propose","items":[{"question":"x","category":"authz","endpoint_keys":["/api/users"],"priority":"low"}]}`, "items[0].endpoint_keys[0] 须为 `METHOD URL` 形式")
	f.toolError(oracleTool, f.inv, `{"action":"propose","items":[{"question":"","category":"authz","endpoint_keys":[],"priority":"low"}]}`, "items[0].question 必填为空")
	f.toolError(oracleTool, f.inv, `{"action":"claim","number":"O-001","items":[{"question":"x","category":"authz","endpoint_keys":[],"priority":"low"}]}`, "action=claim 不接受字段 items")
	f.toolError(oracleTool, f.inv, `{"action":"claim","number":"O-099"}`, "Oracle 不存在")
	f.toolError(oracleTool, f.inv, `{"action":"claim"}`, "number 必填")

	if claimed := f.mustTool(oracleTool, `{"action":"claim","number":"O-001"}`); claimed["status"] != oracleInProgress || claimed["changed"] != true {
		t.Fatalf("claim = %+v", claimed)
	}
	if again := f.mustTool(oracleTool, `{"action":"claim","number":"o-1"}`); again["status"] != oracleInProgress || again["changed"] != false {
		t.Fatalf("claim again = %+v", again)
	}
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-001","status":"holds","summary":"B 的资料可读","evidence_ids":["we_users1"]}`, "status=holds 必须提供 finding_id")
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-001","status":"holds","summary":"B 的资料可读","finding_id":"wf_mine"}`, "status=holds 的 evidence_ids 数量不足")
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-001","status":"holds","summary":"B 的资料可读","evidence_ids":["we_users1"],"finding_id":"wf_theirs"}`, `finding_id="wf_theirs" 不是本任务候选`)
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-001","status":"holds","summary":"B 的资料可读","evidence_ids":["we_other"],"finding_id":"wf_mine"}`, "不是本任务的证据")
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-001","status":"open","summary":"x"}`, "status 仅支持 holds/refuted/blocked")
	closed := f.mustTool(oracleTool, `{"action":"close","number":"O-001","status":"holds","summary":"用户A token 读取到用户B资料","evidence_ids":["we_users1","we_users2"],"finding_id":"wf_mine"}`)
	if closed["status"] != oracleHolds || !strings.Contains(fmt.Sprint(closed["note"]), "仍需独立验证") {
		t.Fatalf("close holds = %+v", closed)
	}
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-001","status":"refuted","summary":"x","evidence_ids":["we_users1"]}`, "冲突（409）：O-001 已是终态 holds")
	f.toolError(oracleTool, f.inv, `{"action":"claim","number":"O-001"}`, "冲突（409）")
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-002","status":"refuted","summary":"注入无效"}`, "status=refuted 的 evidence_ids 数量不足")
	f.toolError(oracleTool, f.inv, `{"action":"close","number":"O-002","status":"refuted","summary":"注入无效","evidence_ids":["we_login"],"finding_id":"wf_mine"}`, "finding_id 只用于 status=holds")
	f.mustTool(oracleTool, `{"action":"close","number":"O-002","status":"refuted","summary":"单引号与布尔载荷均返回 302 到登录页","evidence_ids":["we_login"]}`)
	f.mustTool(oracleTool, `{"action":"close","number":"O-003","status":"blocked","summary":"需要第二个测试账号"}`)
	f.toolError(oracleTool, f.inv, `{"action":"drop","number":"O-004"}`, "summary 必填为空")
	f.mustTool(oracleTool, `{"action":"drop","number":"O-004","summary":"文档已标注下线"}`)

	holds := f.oracle(1)
	if eventPath(holds) != ">open@model,open>in_progress@model,in_progress>holds@model" || holds.FindingNumber != "WSC-000001" || holds.ClaimedSessionID != f.inv.SessionID ||
		strings.Join(holds.EvidenceIDs, ",") != "we_users1,we_users2" || holds.Events[2].Summary != "用户A token 读取到用户B资料" {
		t.Fatalf("O-001 = %+v", holds)
	}
	if blocked := f.oracle(3); blocked.Status != oracleBlocked || eventPath(blocked) != ">open@model,open>blocked@model" {
		t.Fatalf("O-003 = %+v", blocked)
	}

	// 只读与摘要。
	text, isError := f.tool(read, f.inv, `{"section":"oracles","status":"holds"}`)
	if isError || !strings.Contains(text, "O-001 [high·holds·authz]") || !strings.Contains(text, "WSC-000001") || strings.Contains(text, "O-002") {
		t.Fatalf("read oracles = %s", text)
	}
	f.mustTool(&boardFactTool{store: f.cap.store}, `{"add":[{"kind":"role","subject":"普通用户","statement":"注册用户可下单","confidence":"confirmed","evidence_ids":["we_orders"]}]}`)
	digest, err := f.cap.store.boardDigest(t.Context(), f.task.ID, 2000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"地图：5 个端点（流量 3 / 模型补充 2），其中 1 个尚无任何 Oracle 引用",
		"- " + boardOrdersKey + "（请求 1 次；状态 201）",
		"Oracle：open 1 / in_progress 0 / holds 1 / refuted 1 / blocked 1 / dropped 1",
		"- O-005 [high·open·authn]",
		"O-001 成立（候选 WSC-000001，待独立验证）",
		"O-002 不成立",
		"事实：1 条有效（confirmed 1 / inferred 0）",
		"- [role] 普通用户：注册用户可下单",
	} {
		if !strings.Contains(digest, want) {
			t.Fatalf("digest missing %q:\n%s", want, digest)
		}
	}
	if summary, _ := f.tool(read, f.inv, `{"section":"summary"}`); summary != digest {
		t.Fatalf("read summary differs from digest:\n%s\n---\n%s", summary, digest)
	}
}

func TestBoardDigestTruncation(t *testing.T) {
	f := newBoardFixture(t)
	for index := range 40 {
		f.request(f.task.ID, fmt.Sprintf("we_%02d", index), "GET", fmt.Sprintf("https://api.example.test/api/module%02d/items", index), nil, "", 200, true, true, f.at(index))
	}
	proposals := []boardOracleProposal{}
	for index := range 30 {
		proposals = append(proposals, boardOracleProposal{question: fmt.Sprintf("问题%02d：", index) + strings.Repeat("能否越权访问其他租户的数据", 30), category: "authz", priority: "high"})
	}
	if _, err := f.cap.store.boardProposeOracles(t.Context(), f.task.ID, "model", f.inv.SessionID, proposals, f.at(60)); err != nil {
		t.Fatal(err)
	}
	digest, err := f.cap.store.boardDigest(t.Context(), f.task.ID, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if count := utf8.RuneCountInString(digest); count > 2000 || !strings.HasSuffix(digest, boardDigestTruncated) || !strings.HasPrefix(digest, "地图：40 个端点") {
		t.Fatalf("digest %d runes:\n%s", count, digest)
	}
	if untested := strings.Count(digest, "（请求 1 次；状态 200）"); untested != 15 {
		t.Fatalf("untested endpoints listed = %d", untested)
	}
	if small, _ := f.cap.store.boardDigest(t.Context(), f.task.ID, 10); utf8.RuneCountInString(small) > 10 {
		t.Fatalf("tiny digest = %q", small)
	}
}

func TestBoardHTTPListsUserOraclesAndAudit(t *testing.T) {
	f := newBoardFixture(t)
	f.seedTraffic()
	f.finding("wf_mine", f.task.ID, 1)
	base := "/api/webapi/tasks/" + f.task.ID + "/board"
	// 用户提出（摄取在 HTTP 读写前执行）。
	status, created := f.do("POST", base+"/oracles", `{"question":"普通用户能否访问订单管理？","category":"authz","endpoint_keys":["`+boardOrdersKey+`"],"priority":"medium"}`)
	oracle, _ := created["oracle"].(map[string]any)
	if status != 201 || created["created"] != true || oracle["number"] != "O-001" || oracle["origin"] != "user" || len(oracle["events"].([]any)) != 1 {
		t.Fatalf("POST = %d %+v", status, created)
	}
	if status, again := f.do("POST", base+"/oracles", `{"question":"普通用户能否访问订单管理","category":"authz","endpoint_keys":["`+boardOrdersKey+`"],"priority":"high"}`); status != 200 || again["created"] != false {
		t.Fatalf("POST duplicate = %d %+v", status, again)
	}
	row := requireAudit(t, f.cap, "board.oracle.create", 1)
	if row.Actor != auditActorUser || row.TaskID != f.task.ID || row.TargetKind != "board_oracle" || !strings.Contains(row.Summary, "O-001") {
		t.Fatalf("audit create = %+v", row)
	}
	for _, body := range []string{`{"question":"x","category":"rce","endpoint_keys":[],"priority":"low"}`, `{"question":"x","category":"authz","endpoint_keys":[],"priority":"low","status":"open"}`, `{"question":"","category":"authz","endpoint_keys":[],"priority":"low"}`} {
		if status, value := f.do("POST", base+"/oracles", body); status != 400 {
			t.Fatalf("POST %s = %d %+v", body, status, value)
		}
	}
	if status, _ := f.do("POST", "/api/webapi/tasks/missing/board/oracles", `{"question":"x","category":"authz","endpoint_keys":[],"priority":"low"}`); status != 404 {
		t.Fatalf("POST missing task = %d", status)
	}
	f.mustTool(&boardOracleTool{store: f.cap.store}, `{"action":"propose","items":[{"question":"登录能否爆破？","category":"authn","endpoint_keys":["`+boardLoginKey+`"],"priority":"low"}]}`)
	f.mustTool(&boardOracleTool{store: f.cap.store}, `{"action":"close","number":"O-002","status":"holds","summary":"无锁定","evidence_ids":["we_login"],"finding_id":"wf_mine"}`)

	// 总览。
	status, board := f.do("GET", base, "")
	counts, _ := board["counts"].(map[string]any)
	if status != 200 || counts["endpoints"] != float64(3) || counts["untested_endpoints"] != float64(1) || counts["oracles"].(map[string]any)["open"] != float64(1) ||
		counts["oracles"].(map[string]any)["holds"] != float64(1) || !strings.Contains(board["digest"].(string), "地图：3 个端点") {
		t.Fatalf("GET board = %d %+v", status, board)
	}
	// 端点：游标、untested、q。
	status, page := f.do("GET", base+"/endpoints?limit=2", "")
	if status != 200 || page["has_more"] != true || len(page["items"].([]any)) != 2 {
		t.Fatalf("endpoints page1 = %d %+v", status, page)
	}
	status, page2 := f.do("GET", base+"/endpoints?limit=2&cursor="+page["next_cursor"].(string), "")
	if status != 200 || page2["has_more"] != false || page2["next_cursor"] != nil || page2["items"].([]any)[0].(map[string]any)["endpoint_key"] != boardOrdersKey {
		t.Fatalf("endpoints page2 = %d %+v", status, page2)
	}
	if login := page["items"].([]any)[1].(map[string]any); login["endpoint_key"] != boardLoginKey || fmt.Sprint(login["oracles"]) != "[O-002]" {
		t.Fatalf("login endpoint = %+v", login)
	}
	if status, untested := f.do("GET", base+"/endpoints?untested=1", ""); status != 200 || len(untested["items"].([]any)) != 1 || untested["items"].([]any)[0].(map[string]any)["endpoint_key"] != boardUsersKey {
		t.Fatalf("untested = %d %+v", status, untested)
	}
	if status, found := f.do("GET", base+"/endpoints?q=LOGIN", ""); status != 200 || len(found["items"].([]any)) != 1 {
		t.Fatalf("q = %d %+v", status, found)
	}
	for _, path := range []string{base + "/endpoints?untested=2", base + "/endpoints?limit=0", base + "/endpoints?cursor=!!!", base + "/facts?kind=secret", base + "/facts?status=open",
		base + "/facts?cursor=bad", base + "/oracles?status=closed", base + "/oracles?category=rce", base + "/oracles?cursor=bad", base + "/oracles?limit=201"} {
		if status, value := f.do("GET", path, ""); status != 400 {
			t.Fatalf("GET %s = %d %+v", path, status, value)
		}
	}
	for _, path := range []string{"/api/webapi/tasks/missing/board", "/api/webapi/tasks/missing/board/endpoints", "/api/webapi/tasks/missing/board/facts", "/api/webapi/tasks/missing/board/oracles"} {
		if status, _ := f.do("GET", path, ""); status != 404 {
			t.Fatalf("GET %s = %d", path, status)
		}
	}
	// 事实列表。
	f.mustTool(&boardFactTool{store: f.cap.store}, `{"add":[{"kind":"auth","subject":"a","statement":"s1","confidence":"confirmed","evidence_ids":["we_users1"]},{"kind":"tech","subject":"b","statement":"s2","confidence":"inferred"},{"kind":"tech","subject":"c","statement":"s3","confidence":"inferred"}]}`)
	status, facts := f.do("GET", base+"/facts?kind=tech&limit=1", "")
	if status != 200 || facts["has_more"] != true || len(facts["items"].([]any)) != 1 {
		t.Fatalf("facts = %d %+v", status, facts)
	}
	status, facts2 := f.do("GET", base+"/facts?kind=tech&limit=1&cursor="+facts["next_cursor"].(string), "")
	if status != 200 || facts2["has_more"] != false || facts2["items"].([]any)[0].(map[string]any)["kind"] != "tech" ||
		facts2["items"].([]any)[0].(map[string]any)["id"] == facts["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatalf("facts page2 = %d %+v", status, facts2)
	}
	if status, active := f.do("GET", base+"/facts?status=active", ""); status != 200 || len(active["items"].([]any)) != 3 {
		t.Fatalf("facts active = %d %+v", status, active)
	}
	// Oracle 列表（含 events）、过滤、游标。
	status, oracles := f.do("GET", base+"/oracles?limit=1", "")
	if status != 200 || oracles["has_more"] != true || oracles["items"].([]any)[0].(map[string]any)["number"] != "O-001" {
		t.Fatalf("oracles = %d %+v", status, oracles)
	}
	status, oracles2 := f.do("GET", base+"/oracles?limit=1&cursor="+oracles["next_cursor"].(string), "")
	second := oracles2["items"].([]any)[0].(map[string]any)
	if status != 200 || oracles2["has_more"] != false || second["number"] != "O-002" || second["finding_number"] != "WSC-000001" || len(second["events"].([]any)) != 2 {
		t.Fatalf("oracles page2 = %d %+v", status, oracles2)
	}
	if status, filtered := f.do("GET", base+"/oracles?status=open&category=authz", ""); status != 200 || len(filtered["items"].([]any)) != 1 {
		t.Fatalf("oracles filter = %d %+v", status, filtered)
	}

	// 用户调整：优先级、放弃、重开（清除原结论），模型此后可再结论。
	status, updated := f.do("PUT", base+"/oracles/O-001", `{"priority":"high"}`)
	if status != 200 || updated["changed"] != true || updated["oracle"].(map[string]any)["priority"] != "high" || len(updated["oracle"].(map[string]any)["events"].([]any)) != 1 {
		t.Fatalf("PUT priority = %d %+v", status, updated)
	}
	if status, same := f.do("PUT", base+"/oracles/O-001", `{"priority":"high","status":"open"}`); status != 200 || same["changed"] != false {
		t.Fatalf("PUT no-op = %d %+v", status, same)
	}
	requireAudit(t, f.cap, "board.oracle.update", 1)
	if status, dropped := f.do("PUT", base+"/oracles/1", `{"status":"dropped"}`); status != 200 || dropped["oracle"].(map[string]any)["status"] != oracleDropped {
		t.Fatalf("PUT dropped = %d %+v", status, dropped)
	}
	status, reopened := f.do("PUT", base+"/oracles/O-002", `{"status":"open"}`)
	reopenedOracle := reopened["oracle"].(map[string]any)
	if status != 200 || reopenedOracle["status"] != oracleOpen || reopenedOracle["finding_id"] != "" || reopenedOracle["result_summary"] != "" {
		t.Fatalf("PUT reopen = %d %+v", status, reopened)
	}
	row = requireAudit(t, f.cap, "board.oracle.update", 3)
	if detail := auditDetail(t, row); fmt.Sprint(detail["changes"]) != "map[status:map[from:holds to:open]]" {
		t.Fatalf("audit update detail = %+v", detail)
	}
	reopenedView := f.oracle(2)
	if eventPath(reopenedView) != ">open@model,open>holds@model,holds>open@user" || !strings.Contains(reopenedView.Events[2].Summary, "原结论：无锁定") {
		t.Fatalf("reopen events = %+v", reopenedView.Events)
	}
	f.mustTool(&boardOracleTool{store: f.cap.store}, `{"action":"close","number":"O-002","status":"refuted","summary":"复查后有锁定","evidence_ids":["we_login"]}`)
	for _, body := range []string{`{}`, `{"status":"holds"}`, `{"priority":"urgent"}`, `{"priority":"low","extra":1}`} {
		if status, value := f.do("PUT", base+"/oracles/O-001", body); status != 400 {
			t.Fatalf("PUT %s = %d %+v", body, status, value)
		}
	}
	if status, _ := f.do("PUT", base+"/oracles/O-abc", `{"priority":"low"}`); status != 400 {
		t.Fatalf("PUT bad number = %d", status)
	}
	if status, _ := f.do("PUT", base+"/oracles/O-099", `{"priority":"low"}`); status != 404 {
		t.Fatalf("PUT missing oracle = %d", status)
	}
	requireAudit(t, f.cap, "board.oracle.update", 3)
}

func TestBoardSchemaUpgradeFromKnownStages(t *testing.T) {
	for _, stage := range []struct{ models, indexes int }{{28, 29}, {26, 29}} {
		database := filepath.Join(t.TempDir(), "hub.sqlite")
		old, err := webservice.NewSQLiteStore(database, webapiSchemaStage(stage.models, stage.indexes))
		if err != nil {
			t.Fatal(err)
		}
		_ = old.Close()
		if _, err := webservice.NewSQLiteStore(database, SchemaModule()); err == nil {
			t.Fatalf("stage %v already complete", stage)
		}
		if err := PrepareWebAPISchema(database); err != nil {
			t.Fatalf("prepare stage %v: %v", stage, err)
		}
		complete, err := webservice.NewSQLiteStore(database, SchemaModule())
		if err != nil {
			t.Fatalf("full validation after stage %v: %v", stage, err)
		}
		_ = complete.Close()
		f := openBoardFixture(t, database)
		f.seedTraffic()
		if status, value := f.do("POST", "/api/webapi/tasks/"+f.task.ID+"/board/oracles", `{"question":"升级后可用？","category":"other","endpoint_keys":["`+boardUsersKey+`"],"priority":"low"}`); status != 201 {
			t.Fatalf("board after upgrade = %d %+v", status, value)
		}
		if status, value := f.do("GET", "/api/webapi/tasks/"+f.task.ID+"/board", ""); status != 200 || value["counts"].(map[string]any)["endpoints"] != float64(3) {
			t.Fatalf("board GET after upgrade = %d %+v", status, value)
		}
		if err := PrepareWebAPISchema(database); err != nil {
			t.Fatalf("prepare complete again: %v", err)
		}
	}
}
