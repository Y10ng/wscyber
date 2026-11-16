package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	extension2 "github.com/chainreactors/cyber/core/extension"
	webext "github.com/chainreactors/cyber/exts/web"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

const testAccessKey = "webapi-test-access-key"

// startTestHub 用真实 httptest + 临时 SQLite 装配原生 Web 扩展与鉴权中间件，
// 返回关闭函数与数据库路径（用于关闭后重开验证）。
func startTestHub(t *testing.T) (srv *httptest.Server, database string, closeFn func()) {
	t.Helper()
	database = filepath.Join(t.TempDir(), "hub.sqlite")
	capability, err := NewCapability(database)
	if err != nil {
		t.Fatalf("NewCapability: %v", err)
	}
	extension := webext.New(webext.Config{
		Product:      "wscyber-webapi-test",
		Database:     database,
		AccessKey:    testAccessKey,
		Capabilities: []webservice.Capability{capability},
	})
	set, err := extension2.New(extension)
	if err != nil {
		t.Fatalf("extension.New: %v", err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatalf("set.Load: %v", err)
	}
	service, ok := extension.Service().(interface{ Auth() webpkg.Auth })
	if !ok {
		t.Fatalf("web service lacks Auth()")
	}
	handler, err := webpkg.NewHandler(service.Auth(), nil, extension.Routes()...)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	srv = httptest.NewServer(handler)
	closeFn = func() {
		srv.Close()
		_ = set.Close(context.Background())
		// Capability 自有的第二条连接由 Capability.Close 负责关闭。
		_ = capability.Close(context.Background())
	}
	return srv, database, closeFn
}

func putTask(t *testing.T, client *http.Client, baseURL, id, body, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/webapi/tasks/"+id, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func getTask(t *testing.T, client *http.Client, baseURL, id, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/webapi/tasks/"+id, nil)
	if err != nil {
		t.Fatalf("new GET request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func validBody(known, objective, origins string) string {
	return fmt.Sprintf(`{"known_info":%q,"objective":%q,"authorization":{"confirmed":true,"origins":[%s]}}`, known, objective, origins)
}

// TestSaveReadTask 覆盖：保存→读取；独立 initial_session_id 与 task id 不同；
// origins 规范化；404。
func TestSaveReadTask(t *testing.T) {
	srv, _, closeFn := startTestHub(t)
	defer closeFn()
	client := srv.Client()

	body := validBody("目标已开放 8080 端口", "确认登录入口是否存在默认凭据", `"http://127.0.0.1:8080", "https://example.com:443"`)
	status, raw := putTask(t, client, srv.URL, "task-1", body, testAccessKey)
	if status != http.StatusCreated {
		t.Fatalf("PUT status = %d, want 201; body: %s", status, raw)
	}
	var created struct {
		Task     *Task `json:"task"`
		Created  bool  `json:"created"`
		Conflict bool  `json:"conflict"`
		Idem     bool  `json:"idempotent"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	task := created.Task
	if task == nil || !created.Created || created.Conflict || created.Idem {
		t.Fatalf("unexpected PUT result: %+v", created)
	}
	if task.ID != "task-1" {
		t.Fatalf("task id = %q", task.ID)
	}
	if task.InitialSessionID == "" || task.InitialSessionID == task.ID {
		t.Fatalf("initial_session_id 必须非空且与 task id 不同: %q", task.InitialSessionID)
	}
	if task.CreatedAt == "" {
		t.Fatalf("created_at 不能为空")
	}
	// origins 规范化：去重、默认端口省略、排序（":" > ".", http://127.0.0.1:8080 在前）。
	wantOrigins := []string{"http://127.0.0.1:8080", "https://example.com"}
	if fmt.Sprint(task.Origins) != fmt.Sprint(wantOrigins) {
		t.Fatalf("origins = %v, want %v", task.Origins, wantOrigins)
	}
	if task.KnownInfo != "目标已开放 8080 端口" || task.Objective != "确认登录入口是否存在默认凭据" {
		t.Fatalf("两输入回读不一致: %+v", task)
	}

	// GET 回读真实保存字段。
	status, raw = getTask(t, client, srv.URL, "task-1", testAccessKey)
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body: %s", status, raw)
	}
	var loaded Task
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	gotRaw, _ := json.Marshal(loaded)
	wantRaw, _ := json.Marshal(*task)
	if string(gotRaw) != string(wantRaw) {
		t.Fatalf("GET 回读与 PUT 保存不一致: got %s want %s", gotRaw, wantRaw)
	}

	// 404
	status, _ = getTask(t, client, srv.URL, "task-missing", testAccessKey)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing status = %d, want 404", status)
	}
}

// TestIdempotentAndConflict 覆盖：同 ID 同规范请求返回同一任务；不同内容 409；
// 并发同 ID 只有一条记录。
func TestIdempotentAndConflict(t *testing.T) {
	srv, database, closeFn := startTestHub(t)
	defer closeFn()
	client := srv.Client()

	bodyA := validBody("已知A", "目标A", `"http://127.0.0.1:9000"`)
	if status, raw := putTask(t, client, srv.URL, "task-idem", bodyA, testAccessKey); status != http.StatusCreated {
		t.Fatalf("first PUT = %d, body %s", status, raw)
	}
	// 同规范请求（重复 + 默认端口显式写出 + 带空路径 + 换序）→ 同一任务 200。
	bodyASame := validBody("已知A", "目标A", `"http://127.0.0.1:9000/", "http://127.0.0.1:9000"`)
	status, raw := putTask(t, client, srv.URL, "task-idem", bodyASame, testAccessKey)
	if status != http.StatusOK {
		t.Fatalf("idempotent PUT = %d, want 200; body %s", status, raw)
	}
	var idem struct {
		Task     *Task `json:"task"`
		Created  bool  `json:"created"`
		Conflict bool  `json:"conflict"`
		Idem     bool  `json:"idempotent"`
	}
	if err := json.Unmarshal(raw, &idem); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !idem.Idem || idem.Conflict || idem.Created || idem.Task == nil || idem.Task.KnownInfo != "已知A" {
		t.Fatalf("idempotent result wrong: %+v", idem)
	}
	// 换序（排序后同集合）也必须是同一规范请求。
	bodyAReordered := validBody("已知A", "目标A", `"http://127.0.0.1:9000", "http://127.0.0.1:9000/"`)
	status, _ = putTask(t, client, srv.URL, "task-idem", bodyAReordered, testAccessKey)
	if status != http.StatusOK {
		t.Fatalf("reordered idempotent PUT = %d, want 200", status)
	}

	// 不同内容 → 409，且不覆盖原授权。
	bodyB := validBody("已知B", "目标B", `"http://127.0.0.1:9001"`)
	status, raw = putTask(t, client, srv.URL, "task-idem", bodyB, testAccessKey)
	if status != http.StatusConflict {
		t.Fatalf("conflict PUT = %d, want 409; body %s", status, raw)
	}
	status, raw = getTask(t, client, srv.URL, "task-idem", testAccessKey)
	if status != http.StatusOK {
		t.Fatalf("GET after conflict = %d", status)
	}
	var after Task
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if after.KnownInfo != "已知A" || after.Objective != "目标A" || fmt.Sprint(after.Origins) != "[http://127.0.0.1:9000]" {
		t.Fatalf("409 后原授权被改动: %+v", after)
	}

	// 并发同 ID 同内容：全部成功且只有一条记录。
	const workers = 8
	var wg sync.WaitGroup
	statuses := make([]int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], _ = putTask(t, client, srv.URL, "task-concurrent", bodyA, testAccessKey)
		}(i)
	}
	wg.Wait()
	created, idemOK := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			idemOK++
		default:
			t.Fatalf("concurrent PUT status = %d", s)
		}
	}
	if created != 1 || idemOK != workers-1 {
		t.Fatalf("并发结果 created=%d idempotent=%d, want 1/%d", created, idemOK, workers-1)
	}
	store, err := openTaskStore(database)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	task, err := store.Get(context.Background(), "task-concurrent")
	if err != nil || task == nil || task.KnownInfo != "已知A" {
		t.Fatalf("并发后记录异常: task=%+v err=%v", task, err)
	}
}

// TestInvalidRequests 覆盖：非法授权/空输入/缺鉴权一律 400/401 且不写库。
func TestInvalidRequests(t *testing.T) {
	srv, database, closeFn := startTestHub(t)
	defer closeFn()
	client := srv.Client()

	cases := []struct {
		name string
		body string
	}{
		{"empty-known", `{"known_info":"  ","objective":"目标","authorization":{"confirmed":true,"origins":["http://127.0.0.1:1"]}}`},
		{"empty-objective", `{"known_info":"已知","objective":"","authorization":{"confirmed":true,"origins":["http://127.0.0.1:1"]}}`},
		{"not-confirmed", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":false,"origins":["http://127.0.0.1:1"]}}`},
		{"nil-authorization", `{"known_info":"已知","objective":"目标"}`},
		{"userinfo-origin", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":["http://user:pass@127.0.0.1:1"]}}`},
		{"path-origin", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":["http://127.0.0.1:1/admin"]}}`},
		{"query-origin", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":["http://127.0.0.1:1?x=1"]}}`},
		{"fragment-origin", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":["http://127.0.0.1:1/#a"]}}`},
		{"non-http-scheme", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":["ftp://127.0.0.1:1"]}}`},
		{"empty-origin", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":[""]}}`},
		{"missing-origins", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true}}`},
		{"empty-origins-list", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":[]}}`},
		{"bad-port-origin", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":["http://127.0.0.1:70000"]}}`},
		{"trailing-json", `{"known_info":"已知","objective":"目标","authorization":{"confirmed":true,"origins":["http://127.0.0.1:1"]}}]`},
		{"bad-json", `{"known_info":`},
	}
	for _, tc := range cases {
		status, _ := putTask(t, client, srv.URL, "bad-"+tc.name, tc.body, testAccessKey)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", tc.name, status)
		}
	}
	// 非法路径 ID：含控制字符（%0A 保留原始字节，不被 ServeMux 规范化）。
	status, _ := putTask(t, client, srv.URL, "bad%0Aid", validBody("已知", "目标", `"http://127.0.0.1:1"`), testAccessKey)
	if status != http.StatusBadRequest {
		t.Fatalf("bad id status = %d, want 400", status)
	}
	// 无鉴权：401 且不写库。
	status, _ = putTask(t, client, srv.URL, "no-auth", validBody("已知", "目标", `"http://127.0.0.1:1"`), "")
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated PUT = %d, want 401", status)
	}
	status, _ = getTask(t, client, srv.URL, "no-auth", "")
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d, want 401", status)
	}

	// 确认非法请求一条都没写入。
	store, err := openTaskStore(database)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	for _, tc := range cases {
		task, err := store.Get(context.Background(), "bad-"+tc.name)
		if err != nil || task != nil {
			t.Fatalf("非法请求已写库: %s task=%+v err=%v", tc.name, task, err)
		}
	}
	task, _ := store.Get(context.Background(), "no-auth")
	if task != nil {
		t.Fatalf("未鉴权请求已写库")
	}
}

// TestPersistenceAcrossRestart 覆盖：Hub 关闭后重开，同一 hub.sqlite 任务仍在。
func TestPersistenceAcrossRestart(t *testing.T) {
	srv, database, closeFn := startTestHub(t)
	client := srv.Client()
	body := validBody("已知", "目标", `"http://127.0.0.1:8080"`)
	if status, raw := putTask(t, client, srv.URL, "persist-1", body, testAccessKey); status != http.StatusCreated {
		t.Fatalf("PUT = %d, body %s", status, raw)
	}
	closeFn()

	// 重开：模拟新 Capability + 新扩展进程读同一库。
	capability, err := NewCapability(database)
	if err != nil {
		t.Fatalf("reopen capability: %v", err)
	}
	defer capability.Close(context.Background())
	task, err := capability.store.Get(context.Background(), "persist-1")
	if err != nil || task == nil {
		t.Fatalf("重开后任务丢失: task=%+v err=%v", task, err)
	}
	if task.KnownInfo != "已知" || task.InitialSessionID == "" {
		t.Fatalf("重读后字段异常: %+v", task)
	}
}

// TestNormalizeOrigin 覆盖 origin 重建正确性：IPv6 方括号、默认端口省略、
// 非法端口范围、userinfo/路径/query/fragment 拒绝。
func TestNormalizeOrigin(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"HTTP://Example.COM:80/", "http://example.com"},
		{"https://example.com:443", "https://example.com"},
		{"http://[::1]:8080", "http://[::1]:8080"},
		{"http://[2001:db8::1]", "http://[2001:db8::1]"},
		{"http://[::1]:80", "http://[::1]"},
		{"http://[::1]", "http://[::1]"},
	}
	for _, tc := range cases {
		got, err := normalizeOrigin(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("normalizeOrigin(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
	bad := []string{
		"http://127.0.0.1:0", "http://127.0.0.1:65536", "http://user@127.0.0.1:1",
		"http://127.0.0.1:1/a", "http://127.0.0.1:1?q=1", "http://127.0.0.1:1#f",
		"ftp://127.0.0.1:1", "",
	}
	for _, raw := range bad {
		if got, err := normalizeOrigin(raw); err == nil {
			t.Fatalf("normalizeOrigin(%q) = %q, want error", raw, got)
		}
	}
}

// TestStoreDirectly 覆盖 store 层保存/回读与独立 session id 不重复。
// 表结构由 SchemaModule 声明，这里直接建在临时库中以聚焦 store 行为。
func TestStoreDirectly(t *testing.T) {
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	store, err := openTaskStore(database)
	if err != nil {
		t.Fatalf("openTaskStore: %v", err)
	}
	defer store.Close()
	if _, err := store.orm.NewCreateTable().Model((*taskModel)(nil)).Exec(context.Background()); err != nil {
		t.Fatalf("create webapi_tasks: %v", err)
	}
	outcome, err := store.Save(context.Background(), "s-1", "已知", "目标", []string{"http://127.0.0.1:1"})
	if err != nil || !outcome.Created || outcome.Task == nil {
		t.Fatalf("Save: %+v err=%v", outcome, err)
	}
	outcome2, err := store.Save(context.Background(), "s-2", "已知", "目标", []string{"http://127.0.0.1:1"})
	if err != nil || !outcome2.Created {
		t.Fatalf("Save s-2: %+v err=%v", outcome2, err)
	}
	if outcome.Task.InitialSessionID == outcome2.Task.InitialSessionID {
		t.Fatalf("不同任务的 initial_session_id 重复: %q", outcome.Task.InitialSessionID)
	}
	loaded, err := store.Get(context.Background(), "s-1")
	if err != nil || loaded == nil {
		t.Fatalf("Get: %+v err=%v", loaded, err)
	}
	if loaded.InitialSessionID != outcome.Task.InitialSessionID {
		t.Fatalf("session id 回读不一致")
	}
}
