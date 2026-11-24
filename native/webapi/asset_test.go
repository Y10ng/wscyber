package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// 全部为合成数据：example.test 演示环境、虚构账号。
const assetTestSecret = "demo-Secret-7f3a"

type assetFixture struct {
	t          *testing.T
	capability *Capability
	database   testHub
	mux        *http.ServeMux
}

func newAssetFixture(t *testing.T) *assetFixture {
	t.Helper()
	database := newTestHub(t)
	// NewSQLiteStore 经 overlay 实际连接 PostgreSQL；保留上游名字以缩小补丁。
	schema, err := webservice.NewSQLiteStore(database.DSN(), SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	_ = schema.Close()
	return openAssetFixture(t, database)
}

func openAssetFixture(t *testing.T, database testHub) *assetFixture {
	t.Helper()
	capability, err := NewCapability(database.URL, database.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close(context.Background()) })
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	return &assetFixture{t: t, capability: capability, database: database, mux: mux}
}

func (f *assetFixture) do(method, path, body string) (int, map[string]any, string) {
	f.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, request)
	var value map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &value)
	return recorder.Code, value, recorder.Body.String()
}

const assetBody = `{"name":"演示库存系统","owner":"测试组","environment":"预发","known_info":"演示库存 API，文档 /docs","objective":"检查越权与输入校验","origins":["HTTP://Example.test:80","https://api.example.test"],"forbidden":"不要删除库存数据"}`

func (f *assetFixture) createAsset(id string) {
	f.t.Helper()
	if status, _, raw := f.do("PUT", "/api/webapi/assets/"+id, assetBody); status != 201 {
		f.t.Fatalf("create asset = %d %s", status, raw)
	}
}

func TestWebAPIAssetCRUDAndValidation(t *testing.T) {
	f := newAssetFixture(t)
	for _, body := range []string{
		`{"name":"","origins":["http://example.test"]}`,
		`{"name":"` + strings.Repeat("名", 129) + `","origins":["http://example.test"]}`,
		`{"name":"a","origins":[]}`,
		`{"name":"a","origins":["ftp://example.test"]}`,
		`{"name":"a","origins":["http://example.test/path"]}`,
		`{"name":"a","origins":["http://example.test"],"scope":"any"}`,
		`{"name":"a","origins":["http://example.test"],"approval":"maybe"}`,
		`not json`,
	} {
		if status, _, raw := f.do("PUT", "/api/webapi/assets/a1", body); status != 400 {
			t.Fatalf("invalid %s accepted: %d %s", body, status, raw)
		}
	}
	status, value, raw := f.do("PUT", "/api/webapi/assets/a1", assetBody)
	if status != 201 || value["scope"] != "origins" || value["approval"] != "high_risk" || value["archived"] != false {
		t.Fatalf("create = %d %s", status, raw)
	}
	origins, _ := value["origins"].([]any)
	if len(origins) != 2 || origins[0] != "http://example.test" || origins[1] != "https://api.example.test" {
		t.Fatalf("origins not normalized: %v", origins)
	}
	if credentials, _ := value["credentials"].([]any); credentials == nil || len(credentials) != 0 {
		t.Fatalf("credentials should be []: %s", raw)
	}
	createdAt := value["created_at"]
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1", `{"name":" 改名 ","origins":["http://example.test"],"scope":"unrestricted","approval":"all"}`)
	if status != 200 || value["name"] != "改名" || value["scope"] != "unrestricted" || value["approval"] != "all" || value["created_at"] != createdAt || value["owner"] != "" {
		t.Fatalf("update = %d %s", status, raw)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/missing", ""); status != 404 {
		t.Fatalf("missing asset = %d", status)
	}
	if status, value, _ := f.do("GET", "/api/webapi/assets", ""); status != 200 || len(value["assets"].([]any)) != 1 {
		t.Fatalf("list = %d %v", status, value)
	}
	f.do("PUT", "/api/webapi/assets/a1", `{"name":"改名","origins":["http://example.test"],"archived":true}`)
	if _, value, _ := f.do("GET", "/api/webapi/assets?archived=0", ""); len(value["assets"].([]any)) != 0 {
		t.Fatalf("archived listed as active: %v", value)
	}
	if _, value, _ := f.do("GET", "/api/webapi/assets?archived=1", ""); len(value["assets"].([]any)) != 1 {
		t.Fatalf("archived list: %v", value)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets?archived=2", ""); status != 400 {
		t.Fatalf("bad archived filter = %d", status)
	}
}

func TestWebAPIAssetCredentialsWriteOnly(t *testing.T) {
	f := newAssetFixture(t)
	f.createAsset("a1")
	f.createAsset("a2")
	status, value, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"管理员","username":"admin@example.test","secret":"`+assetTestSecret+`","note":"演示账号"}`)
	if status != 200 || strings.Contains(raw, assetTestSecret) || strings.Contains(raw, "secret_cipher") {
		t.Fatalf("put credential = %d %s", status, raw)
	}
	credential := value["credentials"].([]any)[0].(map[string]any)
	if credential["has_secret"] != true || credential["username"] != "admin@example.test" || credential["id"] != "c1" {
		t.Fatalf("credential view = %v", credential)
	}
	info, err := os.Stat(filepath.Join(f.database.StateDir, secretKeyFileName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("key file = %v %v", info, err)
	}
	cipherOf := func() string {
		var model assetCredentialModel
		if err := f.capability.store.orm.NewSelect().Model(&model).Where("id = ?", "c1").Scan(t.Context()); err != nil {
			t.Fatal(err)
		}
		return model.SecretCipher
	}
	first := cipherOf()
	if !strings.HasPrefix(first, secretCipherV1) || strings.Contains(first, assetTestSecret) {
		t.Fatalf("secret not encrypted: %q", first)
	}
	// 整库（全部表的全部行，含 bytea 列的十六进制形式）中也找不到明文。
	if found, err := databaseContains(t.Context(), f.capability.store, assetTestSecret); err != nil || found {
		t.Fatalf("plaintext in database (found=%v err=%v)", found, err)
	}
	// GET 不含 secret。
	if _, _, raw := f.do("GET", "/api/webapi/assets/a1", ""); strings.Contains(raw, assetTestSecret) {
		t.Fatalf("GET leaked secret: %s", raw)
	}
	if _, _, raw := f.do("GET", "/api/webapi/assets", ""); strings.Contains(raw, assetTestSecret) {
		t.Fatalf("list leaked secret: %s", raw)
	}
	// 省略 / 空串 = 保留原值，其它字段照常更新。
	for _, body := range []string{`{"label":"管理员2","username":"admin2@example.test","note":""}`, `{"label":"管理员3","username":"admin3@example.test","secret":"","note":""}`} {
		status, value, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", body)
		credential := value["credentials"].([]any)[0].(map[string]any)
		if status != 200 || credential["has_secret"] != true || cipherOf() != first {
			t.Fatalf("preserve secret failed for %s: %d %s", body, status, raw)
		}
	}
	// 新值替换并可解密。
	f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"管理员","username":"admin@example.test","secret":"new-demo-pass"}`)
	aead, err := f.capability.secrets.load(false)
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := openSecret(aead, "a1", "c1", cipherOf()); err != nil || plain != "new-demo-pass" {
		t.Fatalf("decrypt = %q %v", plain, err)
	}
	// 密文绑定行：换 credential id 解不开。
	if _, err := openSecret(aead, "a1", "c2", cipherOf()); err == nil {
		t.Fatal("cipher not bound to row")
	}
	// 无 secret 的新凭据。
	_, value, _ = f.do("PUT", "/api/webapi/assets/a1/credentials/c2", `{"label":"只读","username":"viewer"}`)
	if credentials := value["credentials"].([]any); len(credentials) != 2 || credentials[1].(map[string]any)["has_secret"] != false {
		t.Fatalf("no-secret credential = %v", value)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a2/credentials/c1", `{"label":"x"}`); status != 409 {
		t.Fatalf("cross-asset credential id = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/credentials/c3", `{"label":"x","password":"y"}`); status != 400 {
		t.Fatalf("unknown field accepted = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/missing/credentials/c9", `{"label":"x"}`); status != 404 {
		t.Fatalf("missing asset credential = %d", status)
	}
	status, value, _ = f.do("DELETE", "/api/webapi/assets/a1/credentials/c1", "")
	if status != 200 || len(value["credentials"].([]any)) != 1 {
		t.Fatalf("delete = %d %v", status, value)
	}
	if status, _, _ := f.do("DELETE", "/api/webapi/assets/a1/credentials/c1", ""); status != 404 {
		t.Fatalf("second delete = %d", status)
	}
}

func TestWebAPIAssetSecretKeyMissingAndInvalid(t *testing.T) {
	f := newAssetFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"管理员","secret":"`+assetTestSecret+`"}`); status != 200 {
		t.Fatalf("put = %d %s", status, raw)
	}
	keyPath := filepath.Join(f.database.StateDir, secretKeyFileName)
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	_ = f.capability.Close(context.Background())
	g := openAssetFixture(t, f.database)
	// 已有密文 + 密钥缺失：不生成新密钥，写新 secret 明确报错。
	status, value, _ := g.do("PUT", "/api/webapi/assets/a1/credentials/c2", `{"label":"x","secret":"another"}`)
	if status != 500 || !strings.Contains(value["error"].(string), "webapi-secrets.key 缺失") {
		t.Fatalf("missing key write = %d %v", status, value)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("key silently regenerated: %v", err)
	}
	// 不含 secret 的编辑不受影响，密文仍在（不静默丢弃）。
	if status, _, _ := g.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"改名"}`); status != 200 {
		t.Fatalf("non-secret edit = %d", status)
	}
	if has, err := g.capability.store.hasStoredSecrets(t.Context()); err != nil || !has {
		t.Fatalf("cipher dropped: %v %v", has, err)
	}
	// 发起任务在组装首条消息阶段报错，任务已建但不开会话。
	launcher := newFakeLauncher(productAgent("node-a"))
	g.capability.launcher = launcher
	status, value, _ = g.do("PUT", "/api/webapi/assets/a1/tasks/t-key", "")
	if status != 500 || value["stage"] != "first_message" || !strings.Contains(value["error"].(string), "缺失") || len(launcher.opens) != 0 {
		t.Fatalf("launch without key = %d %v", status, value)
	}
	// 权限不对的密钥文件拒用。
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("ab", 32)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := newSecretBox(keyPath).load(true); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("0644 key accepted: %v", err)
	}
}

type fakeRun struct {
	requestID string
	request   *aop.RunTurnRequest
}

type fakeLauncher struct {
	mu        sync.Mutex
	agents    []*types.AgentView
	sessions  map[string]*aop.Session
	userTurns map[string]string
	opens     []*aop.OpenSessionRequest
	runs      []fakeRun
	runReject *aop.Rejection
	// running 标记仍在运行的会话（SessionRunning 替身）；runningErr 非空时报错。
	running    map[string]bool
	runningAll bool
	runningErr error
	// ended 标记事件里已有 TurnEnded 的会话（TurnEnded 替身）。
	ended map[string]bool
	// deleted 记录 DeleteSession 调用顺序；deleteErr 中的会话删除时报错（任务删除失败路径）。
	deleted   []string
	deleteErr map[string]error
	// deleteHook 在一次 DeleteSession 记录后（释放锁后）调用，模拟删除期间的并发写入。
	deleteHook func(sessionID string)
}

func newFakeLauncher(agents ...*types.AgentView) *fakeLauncher {
	return &fakeLauncher{agents: agents, sessions: map[string]*aop.Session{}, userTurns: map[string]string{}, running: map[string]bool{}, ended: map[string]bool{}}
}

func productAgent(id string) *types.AgentView {
	return &types.AgentView{Hello: &aop.AgentHello{NodeId: id, Capabilities: []string{"wscyber.model.v1", webapiHTTPCapability}}}
}

func (l *fakeLauncher) Agents() []*types.AgentView {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]*types.AgentView(nil), l.agents...)
}

func (l *fakeLauncher) Session(_ context.Context, id string) (*aop.Session, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sessions[id], nil
}

func (l *fakeLauncher) OpenSession(_ context.Context, requestID string, request *aop.OpenSessionRequest) (*aop.OpenSessionResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opens = append(l.opens, request)
	session := &aop.Session{Id: request.SessionId, NodeId: request.NodeId, State: "open"}
	l.sessions[request.SessionId] = session
	return &aop.OpenSessionResponse{Outcome: &aop.OpenSessionResponse_Accepted{Accepted: session}}, nil
}

func (l *fakeLauncher) RunTurn(_ context.Context, requestID string, request *aop.RunTurnRequest) (*aop.RunTurnResponse, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runs = append(l.runs, fakeRun{requestID: requestID, request: request})
	if l.runReject != nil {
		return &aop.RunTurnResponse{Outcome: &aop.RunTurnResponse_Rejected{Rejected: l.runReject}}, nil
	}
	l.userTurns[request.SessionId] = request.TurnId
	return &aop.RunTurnResponse{Outcome: &aop.RunTurnResponse_Accepted{Accepted: &aop.TurnReceipt{SessionId: request.SessionId, TurnId: request.TurnId, State: "running"}}}, nil
}

func (l *fakeLauncher) UserTurn(_ context.Context, sessionID string) (string, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	turn, ok := l.userTurns[sessionID]
	return turn, ok, nil
}

func (l *fakeLauncher) SessionRunning(_ context.Context, sessionID string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running[sessionID] || l.runningAll && l.sessions[sessionID] != nil, l.runningErr
}

func (l *fakeLauncher) TurnEnded(_ context.Context, sessionID string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ended[sessionID], nil
}

func (l *fakeLauncher) DeleteSession(_ context.Context, sessionID string) (bool, error) {
	l.mu.Lock()
	if err := l.deleteErr[sessionID]; err != nil {
		l.mu.Unlock()
		return false, err
	}
	l.deleted = append(l.deleted, sessionID)
	_, existed := l.sessions[sessionID]
	delete(l.sessions, sessionID)
	hook := l.deleteHook
	l.mu.Unlock()
	if hook != nil {
		hook(sessionID)
	}
	return existed, nil
}

func runText(run fakeRun) string {
	return run.request.GetInput().GetContent()[0].GetText().GetText()
}

func TestWebAPIAssetLaunchTask(t *testing.T) {
	f := newAssetFixture(t)
	f.createAsset("a1")
	f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"管理员","username":"admin@example.test","secret":"`+assetTestSecret+`"}`)
	f.do("PUT", "/api/webapi/assets/a1/credentials/c2", `{"label":"只读","username":"viewer@example.test"}`)
	launcher := newFakeLauncher(productAgent("node-b"), productAgent("node-a"), &types.AgentView{Hello: &aop.AgentHello{NodeId: "node-0"}})
	f.capability.launcher = launcher

	status, value, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	if status != 201 || value["created"] != true || value["idempotent"] != false || value["node_id"] != "node-a" {
		t.Fatalf("launch = %d %s", status, raw)
	}
	progress := value["progress"].(map[string]any)
	if progress["task_created"] != true || progress["session_opened"] != true || progress["turn_started"] != true {
		t.Fatalf("progress = %v", progress)
	}
	task := value["task"].(map[string]any)
	sessionID := task["initial_session_id"].(string)
	if value["session_id"] != sessionID || len(launcher.opens) != 1 || launcher.opens[0].SessionId != sessionID || launcher.opens[0].NodeId != "node-a" {
		t.Fatalf("open = %+v", launcher.opens)
	}
	knownInfo := task["known_info"].(string)
	if knownInfo != "演示库存 API，文档 /docs\n\n环境：预发\n负责人：测试组" || task["objective"] != "检查越权与输入校验" {
		t.Fatalf("task fields = %v", task)
	}
	if strings.Contains(raw, assetTestSecret) {
		t.Fatalf("launch response leaked secret: %s", raw)
	}
	if len(launcher.runs) != 1 || launcher.runs[0].request.MaxTurns != 0 || launcher.runs[0].request.SessionId != sessionID {
		t.Fatalf("runs = %+v", launcher.runs)
	}
	text := runText(launcher.runs[0])
	want := strings.Join([]string{
		"开始探索任务。", "",
		"目标／已知信息：演示库存 API，文档 /docs", "", "环境：预发", "负责人：测试组",
		"希望达到的目标：检查越权与输入校验", "",
		"HTTP(S) 授权范围（仅限以下 origin，出站请求不得超出）：", "- http://example.test", "- https://api.example.test", "",
		"提示：需要发送 HTTP 请求时，优先使用 webapi_http 工具以留存请求/响应证据（此提示不构成额外授权）。", "",
		"测试账号（资产登记，仅用于上述授权范围内的测试）：",
		"- 名称：管理员；用户名：admin@example.test；凭据 ID：c1；口令：用占位符 {{cred:c1.secret}}（平台发送时替换，你看不到明文）",
		"- 名称：只读；用户名：viewer@example.test；凭据 ID：c2", "",
		"禁止行为（提示词约束，非拦截）：", "不要删除库存数据",
	}, "\n")
	if text != want {
		t.Fatalf("first message:\n%s\n--- want ---\n%s", text, want)
	}
	// 任务库行不含 secret。
	var stored taskModel
	if err := f.capability.store.orm.NewSelect().Model(&stored).Where("id = ?", "t1").Scan(t.Context()); err != nil || strings.Contains(stored.KnownInfo+stored.Objective, assetTestSecret) {
		t.Fatalf("task row = %+v %v", stored, err)
	}
	// 任务 GET 带 asset 与资产默认策略。
	_, value, _ = f.do("GET", "/api/webapi/tasks/t1", "")
	asset, _ := value["asset"].(map[string]any)
	policy := value["policy"].(map[string]any)
	if asset["id"] != "a1" || asset["name"] != "演示库存系统" || policy["scope"] != "origins" || policy["approval"] != "high_risk" || policy["is_default"] != false {
		t.Fatalf("task GET = %v", value)
	}
	_, value, _ = f.do("GET", "/api/webapi/tasks?session_id="+sessionID, "")
	if value["asset"].(map[string]any)["id"] != "a1" {
		t.Fatalf("task by session lacks asset: %v", value)
	}
	// 幂等：重复调用不再 Open/Run。
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/t1", `{"objective":"另一个目标"}`)
	if status != 200 || value["idempotent"] != true || value["created"] != false || len(launcher.opens) != 1 || len(launcher.runs) != 1 || value["task"].(map[string]any)["objective"] != "检查越权与输入校验" {
		t.Fatalf("repeat = %d %s", status, raw)
	}
	// 覆盖 objective/scope/approval；本机受控模型节点 maxTurns=3。
	launcher.agents = []*types.AgentView{{Hello: &aop.AgentHello{NodeId: "local", Capabilities: []string{localModelCapability, webapiHTTPCapability}}, Status: &aop.AgentStatus{Provider: "openai", Model: "demo"}}}
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/t2", `{"objective":"只看登录接口","scope":"unrestricted","approval":"off"}`)
	if status != 201 || launcher.runs[1].request.MaxTurns != 3 || !strings.Contains(runText(launcher.runs[1]), "希望达到的目标：只看登录接口") {
		t.Fatalf("override launch = %d %s", status, raw)
	}
	_, value, _ = f.do("GET", "/api/webapi/tasks/t2", "")
	if policy := value["policy"].(map[string]any); policy["scope"] != "unrestricted" || policy["approval"] != "off" {
		t.Fatalf("override policy = %v", value)
	}
	// 关联任务列表。
	status, value, raw = f.do("GET", "/api/webapi/assets/a1/tasks", "")
	tasks, _ := value["tasks"].([]any)
	if status != 200 || len(tasks) != 2 || tasks[0].(map[string]any)["task_id"] != "t2" || tasks[1].(map[string]any)["finding_count"] != float64(0) || tasks[1].(map[string]any)["initial_session_id"] != sessionID {
		t.Fatalf("asset tasks = %d %s", status, raw)
	}
	// 校验：非法策略、不存在资产、占用他用的 task id。
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/tasks/t3", `{"scope":"any"}`); status != 400 {
		t.Fatalf("bad scope = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/tasks/t3", `{"extra":1}`); status != 400 {
		t.Fatalf("unknown field = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/missing/tasks/t3", ""); status != 404 {
		t.Fatalf("missing asset = %d", status)
	}
	if _, err := f.capability.store.Save(t.Context(), "plain", "手建任务", "目标", []string{"http://example.test"}); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/tasks/plain", ""); status != 409 {
		t.Fatalf("plain task id reused = %d", status)
	}
	f.createAsset("a2")
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a2/tasks/t1", ""); status != 409 {
		t.Fatalf("other asset task id reused = %d", status)
	}
	if _, value, _ := f.do("GET", "/api/webapi/tasks/plain", ""); value["asset"] != nil {
		t.Fatalf("plain task asset = %v", value["asset"])
	}
}

func TestWebAPIAssetLaunchWithoutOptionalKnownInfo(t *testing.T) {
	f := newAssetFixture(t)
	status, _, raw := f.do("PUT", "/api/webapi/assets/a-empty", `{"name":"仅名称资产","objective":"完成演示检查","origins":["http://example.test"]}`)
	if status != 201 {
		t.Fatalf("create asset = %d %s", status, raw)
	}
	f.capability.launcher = newFakeLauncher(productAgent("node-a"))

	result, err := f.capability.LaunchAssetTask(t.Context(), "a-empty", "t-empty", AssetLaunchOptions{})
	if err != nil {
		t.Fatalf("launch = %v", err)
	}
	if !result.Created || result.Task == nil || result.Task.KnownInfo != "资产：仅名称资产" {
		t.Fatalf("launch result = %+v", result)
	}
	task, err := f.capability.store.Get(t.Context(), "t-empty")
	if err != nil || task == nil || task.KnownInfo != "资产：仅名称资产" {
		t.Fatalf("stored task = %+v, %v", task, err)
	}
}

func TestWebAPIAssetLaunchPartialFailureAndResume(t *testing.T) {
	f := newAssetFixture(t)
	f.createAsset("a1")
	launcher := newFakeLauncher()
	f.capability.launcher = launcher

	// 没有在线 webapi_http 节点：任务已建，409 明确文案。
	status, value, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	progress, _ := value["progress"].(map[string]any)
	if status != 409 || value["stage"] != "select_node" || !strings.Contains(value["error"].(string), "没有已提供 webapi_http 工具的在线节点") || progress["task_created"] != true || progress["session_opened"] != false {
		t.Fatalf("offline = %d %s", status, raw)
	}
	// 节点上线后重试续完：不重复建任务。
	launcher.agents = []*types.AgentView{productAgent("node-a")}
	launcher.runReject = &aop.Rejection{Code: "UNAVAILABLE", Message: "node is not connected"}
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	progress, _ = value["progress"].(map[string]any)
	if status != 409 || value["stage"] != "run_turn" || progress["session_opened"] != true || progress["turn_started"] != false {
		t.Fatalf("run rejected = %d %s", status, raw)
	}
	// 会话绑定节点离线：不换节点。
	launcher.runReject = nil
	launcher.agents = []*types.AgentView{productAgent("node-z")}
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	if status != 409 || value["stage"] != "run_turn" || !strings.Contains(value["error"].(string), "node-a") {
		t.Fatalf("bound node offline = %d %s", status, raw)
	}
	launcher.agents = []*types.AgentView{productAgent("node-a")}
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	if status != 200 || value["created"] != false || value["idempotent"] != false || value["progress"].(map[string]any)["turn_started"] != true {
		t.Fatalf("resume = %d %s", status, raw)
	}
	if len(launcher.opens) != 1 || len(launcher.runs) != 2 || launcher.runs[0].requestID == launcher.runs[1].requestID {
		t.Fatalf("opens=%d runs=%+v", len(launcher.opens), launcher.runs)
	}
	var links int
	if err := f.capability.store.db().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM webapi_asset_tasks").Scan(&links); err != nil || links != 1 {
		t.Fatalf("links = %d %v", links, err)
	}
	// 本机受控模型节点未配置模型：409。
	launcher.agents = []*types.AgentView{{Hello: &aop.AgentHello{NodeId: "local", Capabilities: []string{localModelCapability, webapiHTTPCapability}}}}
	if status, value, _ := f.do("PUT", "/api/webapi/assets/a1/tasks/t2", ""); status != 409 || !strings.Contains(value["error"].(string), "尚未配置模型") {
		t.Fatalf("unconfigured local model = %d %v", status, value)
	}
}

// 真实原生管理 API（Routes 注入）：无在线节点时 409，任务已建，会话未开。
func TestWebAPIAssetLaunchRealAPINodeOffline(t *testing.T) {
	srv, _, closeFn := startTestHub(t)
	defer closeFn()
	send := func(method, path, body string) (int, map[string]any) {
		request, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+testAccessKey)
		request.Header.Set("Content-Type", "application/json")
		response, err := srv.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var value map[string]any
		_ = json.NewDecoder(response.Body).Decode(&value)
		return response.StatusCode, value
	}
	if status, _ := send("PUT", "/api/webapi/assets/a1", assetBody); status != 201 {
		t.Fatalf("create asset = %d", status)
	}
	status, value := send("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	if status != 409 || value["stage"] != "select_node" || value["progress"].(map[string]any)["task_created"] != true {
		t.Fatalf("real API offline = %d %v", status, value)
	}
	// 未鉴权拒绝。
	request, _ := http.NewRequest("GET", srv.URL+"/api/webapi/assets", nil)
	response, err := srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", response.StatusCode)
	}
}

// 真实原生 Sessions/存储上的续完判定：会话已存在则不再 Open、绑定节点离线 409；
// 已持久化用户首条消息则视为已启动，幂等返回，不盲发新 Run。
func TestWebAPIAssetLaunchRealSessionsResume(t *testing.T) {
	f := newAssetFixture(t)
	f.createAsset("a1")
	store, err := webservice.NewSQLiteStore(f.database.DSN(), SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := webservice.NewService(webservice.ServiceConfig{Store: store})
	f.capability.launcher = apiLauncher{api: service.API()}

	status, value, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	if status != 409 || value["stage"] != "select_node" {
		t.Fatalf("no agents = %d %s", status, raw)
	}
	sessionID := value["session_id"].(string)
	if err := store.CreateSession(t.Context(), &types.SessionRecord{Session: &aop.Session{Id: sessionID, NodeId: "node-x", State: "open"}}); err != nil {
		t.Fatal(err)
	}
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	progress, _ := value["progress"].(map[string]any)
	if status != 409 || value["stage"] != "run_turn" || progress["session_opened"] != true || !strings.Contains(value["error"].(string), "node-x") {
		t.Fatalf("bound node offline = %d %s", status, raw)
	}
	user := &aop.Event{SessionId: sessionID, TurnId: "turn-1", Id: "ev-1", Emitter: "cyber.web", Payload: &aop.Event_Message{Message: &aop.Message{Id: "m1", Role: "user", Content: []*aop.Content{{Value: &aop.Content_Text{Text: &aop.TextContent{Text: "开始探索任务。"}}}}}}}
	if _, _, err := store.AppendAOPEvent(t.Context(), sessionID, user); err != nil {
		t.Fatal(err)
	}
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/t1", "")
	if status != 200 || value["idempotent"] != true || value["turn_id"] != "turn-1" || value["node_id"] != "node-x" {
		t.Fatalf("already started = %d %s", status, raw)
	}
}
