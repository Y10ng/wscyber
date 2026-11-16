package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/httpapi"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelprobe"
)

const probeID = "11223344556677889900aabbccddeeff"
const otherModelID = "ffeeddccbbaa00998877665544332211"

type probeRequest struct {
	path   string
	header http.Header
	body   map[string]any
}

type probeFixture struct {
	server   *httptest.Server
	requests chan probeRequest
	hits     atomic.Int32
	release  chan struct{}
}

func newProbeFixture(t *testing.T, slow bool) *probeFixture {
	t.Helper()
	f := &probeFixture{requests: make(chan probeRequest, 32), release: make(chan struct{})}
	if !slow {
		close(f.release)
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		f.requests <- probeRequest{path: r.URL.Path, header: r.Header.Clone(), body: body}
		select {
		case <-f.release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/nonstandard/"):
			w.WriteHeader(700)
		case strings.Contains(r.URL.Path, "/error/"):
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":"private-provider-error"}`)
		case strings.Contains(r.URL.Path, "/invalid/"):
			_, _ = io.WriteString(w, `{"private":"private-provider-text"}`)
		case strings.Contains(r.URL.Path, "/disconnect/"):
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case strings.HasSuffix(r.URL.Path, "/v1/messages"):
			_, _ = io.WriteString(w, `{"id":"fixture","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":"private-provider-text"}],"stop_reason":"end_turn"}`)
		default:
			_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"private-provider-text"},"finish_reason":"stop"}]}`)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func startProbe(t *testing.T, ctx context.Context, s domain.ModelTestRepository, m *modelconfig.Manager) *modelprobe.Service {
	t.Helper()
	p := modelprobe.New(ctx, s, m)
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		_ = p.Close(c)
	})
	return p
}
func waitProbe(t *testing.T, s *Store, id, status string) domain.ModelTest {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for {
		r, err := s.GetModelTest(ctx, id)
		if err == nil && r.Status == status {
			return r
		}
		select {
		case <-ctx.Done():
			t.Fatalf("测试未达到%s: %+v %v", status, r, err)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func receiveProbe(t *testing.T, f *probeFixture) probeRequest {
	t.Helper()
	select {
	case r := <-f.requests:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("未收到本地测试请求")
		return probeRequest{}
	}
}
func saveProbeConfig(t *testing.T, ctx context.Context, m *modelconfig.Manager, id, base, protocol string, max int) domain.ModelConfigWrite {
	t.Helper()
	in := modelInput()
	in.BaseURL = base
	in.Protocol = protocol
	in.MaxOutputTokens = max
	in.Parameters.Value = map[string]json.RawMessage{"temperature": json.RawMessage(`0`)}
	if protocol == "anthropic_messages" {
		in.Auth = "api_key"
	}
	if _, err := m.Save(ctx, id, in); err != nil {
		t.Fatal(err)
	}
	return in
}
func assertProbeInput(t *testing.T, r probeRequest, protocol string, max int) {
	t.Helper()
	tokenField := "max_completion_tokens"
	if protocol == "anthropic_messages" {
		tokenField = "max_tokens"
	}
	if r.body["model"] != "fixture-model" || r.body[tokenField] != float64(max) || r.body["temperature"] != float64(0) || r.header.Get("X-Private") != "never-return-header-secret" {
		t.Fatalf("配置未精确传递: %+v", r)
	}
	messages, ok := r.body["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("混入其他消息: %+v", r.body)
	}
	msg := messages[0].(map[string]any)
	content := msg["content"]
	if protocol == "anthropic_messages" {
		parts, ok := content.([]any)
		if !ok || len(parts) != 1 || parts[0].(map[string]any)["text"] != modelprobe.UserMessage {
			t.Fatal(content)
		}
		if r.header.Get("X-Api-Key") != "never-return-api-secret" || r.header.Get("Authorization") != "" {
			t.Fatal("认证模式错误")
		}
	} else {
		if content != modelprobe.UserMessage {
			t.Fatal(content)
		}
		if r.header.Get("Authorization") != "Bearer never-return-api-secret" || r.header.Get("X-Api-Key") != "" {
			t.Fatal("认证模式错误")
		}
	}
	if msg["role"] != "user" || r.body["system"] != nil || r.body["tools"] != nil {
		t.Fatal("不应携带研究上下文")
	}
}

type probeAPI struct {
	server *httptest.Server
	token  string
}

func newProbeAPI(t *testing.T, s *Store, m *modelconfig.Manager, p *modelprobe.Service) probeAPI {
	t.Helper()
	a := probeAPI{token: "local-model-test-api-fixture-token"}
	a.server = httptest.NewServer(httpapi.New(s, nil, httpapi.Config{Token: a.token, Models: m, Probes: p, WebDir: t.TempDir()}))
	t.Cleanup(a.server.Close)
	return a
}
func (a probeAPI) call(t *testing.T, method, path, body, auth, origin string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, a.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if auth != "" {
		r.Header.Set("Authorization", "Bearer "+auth)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	resp, err := a.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("回执必须no-store")
	}
	for _, secret := range []string{"never-return", "private-provider", "http://127.0.0.1", "\"text\"", "usage"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("回执泄漏: %s", b)
		}
	}
	return resp.StatusCode, b
}
func testPath(configID, id string) string { return "/api/model-configs/" + configID + "/tests/" + id }

const confirmation = `{"config_revision":1,"confirm_billable":true}`

func TestPostgresModelProbeProtocolsAndConcurrentReplay(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			f := newProbeFixture(t, true)
			m := modelManager(t, s)
			max := 32
			if protocol == "anthropic_messages" {
				max = 7
			}
			saveProbeConfig(t, ctx, m, modelID, f.server.URL+"/saved", protocol, max)
			p := startProbe(t, ctx, s, m)
			api := newProbeAPI(t, s, m, p)
			const n = 8
			var wg sync.WaitGroup
			codes := make(chan int, n)
			start := make(chan struct{})
			for range n {
				wg.Go(func() {
					<-start
					code, _ := api.call(t, "POST", testPath(modelID, probeID), confirmation, api.token, "")
					codes <- code
				})
			}
			close(start)
			wg.Wait()
			close(codes)
			accepted := 0
			for code := range codes {
				if code == 202 {
					accepted++
				} else if code != 200 {
					t.Fatal(code)
				}
			}
			if accepted != 1 {
				t.Fatal(accepted)
			}
			req := receiveProbe(t, f)
			assertProbeInput(t, req, protocol, max)
			want := "/saved/chat/completions"
			if protocol == "anthropic_messages" {
				want = "/saved/v1/messages"
			}
			if req.path != want {
				t.Fatal(req.path)
			}
			r := waitProbe(t, s, probeID, "running")
			if r.Attempted != nil || r.Outcome != "" || r.StartedAt == nil {
				t.Fatal(r)
			}
			for range 3 {
				if code, _ := api.call(t, "POST", testPath(modelID, probeID), confirmation, api.token, ""); code != 200 {
					t.Fatal(code)
				}
			}
			if code, _ := api.call(t, "POST", testPath(otherModelID, probeID), confirmation, api.token, ""); code != 409 {
				t.Fatal(code)
			}
			if code, _ := api.call(t, "GET", testPath(otherModelID, probeID), "", api.token, ""); code != 404 {
				t.Fatal(code)
			}
			if code, _ := api.call(t, "POST", testPath(modelID, probeID), `{"config_revision":2,"confirm_billable":true}`, api.token, ""); code != 409 {
				t.Fatal(code)
			}
			// 已发送期间禁用不伪称撤回，原请求仍可得到确定成功。
			in := modelInput()
			in.Protocol = protocol
			in.BaseURL = f.server.URL + "/saved"
			in.MaxOutputTokens = max
			in.ExpectedRevision = 1
			in.Enabled = false
			if protocol == "anthropic_messages" {
				in.Auth = "api_key"
			}
			if _, err := m.Save(ctx, modelID, in); err != nil {
				t.Fatal(err)
			}
			close(f.release)
			r = waitProbe(t, s, probeID, "completed")
			if r.Outcome != "valid_response" || r.Attempted == nil || !*r.Attempted || r.HTTPStatus == nil || *r.HTTPStatus != 200 {
				t.Fatal(r)
			}
			if f.hits.Load() != 1 {
				t.Fatal(f.hits.Load())
			}
			if code, _ := api.call(t, "POST", testPath(modelID, probeID), confirmation, api.token, ""); code != 200 {
				t.Fatal(code)
			}
			if code, _ := api.call(t, "GET", testPath(modelID, probeID), "", api.token, ""); code != 200 {
				t.Fatal(code)
			}
			// 无主密钥甚至执行服务已停止，仍可核对持久身份。
			if err := p.Close(ctx); err != nil {
				t.Fatal(err)
			}
			noKey := modelconfig.New(s, nil)
			p2 := startProbe(t, ctx, s, noKey)
			api2 := newProbeAPI(t, s, noKey, p2)
			if code, _ := api2.call(t, "POST", testPath(modelID, probeID), confirmation, api2.token, ""); code != 200 {
				t.Fatal(code)
			}
			if code, _ := api2.call(t, "POST", testPath(modelID, fmt.Sprintf("%032x", 99)), confirmation, api2.token, ""); code != 503 {
				t.Fatal(code)
			}
		})
	}
}

func TestPostgresModelProbeHTTPInputZeroSend(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	f := newProbeFixture(t, false)
	m := modelManager(t, s)
	in := saveProbeConfig(t, ctx, m, modelID, f.server.URL, "openai_chat", 64)
	p := startProbe(t, ctx, s, m)
	api := newProbeAPI(t, s, m, p)
	path := testPath(modelID, probeID)
	for _, tc := range []struct {
		path, body, auth, origin string
		code                     int
	}{
		{path, confirmation, "", "", 401}, {path, confirmation, api.token, "https://other.invalid", 403},
		{path + "?", confirmation, api.token, "", 400}, {path + "?reveal=true", confirmation, api.token, "", 400},
		{testPath(modelID, "INVALID"), confirmation, api.token, "", 400},
		{path, `{"config_revision":1,"confirm_billable":false}`, api.token, "", 400},
		{path, `{"config_revision":1,"confirm_billable":true,"prompt":"project-secret"}`, api.token, "", 400},
		{path, `{"config_revision":1,"confirm_billable":true,"config_revision":1}`, api.token, "", 400},
		{path, `{"Config_revision":1,"confirm_billable":true}`, api.token, "", 400},
		{path, `{"config_revision":1,"confirm_billable":null}`, api.token, "", 400},
		{path, confirmation + `{}`, api.token, "", 400}, {path, strings.Repeat(" ", 1025), api.token, "", 400},
		{path, `{"config_revision":2,"confirm_billable":true}`, api.token, "", 409},
		{testPath(otherModelID, probeID), confirmation, api.token, "", 404},
	} {
		code, _ := api.call(t, "POST", tc.path, tc.body, tc.auth, tc.origin)
		if code != tc.code {
			t.Fatalf("%s: %d != %d", tc.body, code, tc.code)
		}
	}
	in.ExpectedRevision = 1
	in.Enabled = false
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	if code, _ := api.call(t, "POST", path, `{"config_revision":2,"confirm_billable":true}`, api.token, ""); code != 409 {
		t.Fatal(code)
	}
	if code, _ := api.call(t, "GET", path, "", api.token, ""); code != 404 {
		t.Fatal(code)
	}
	if f.hits.Load() != 0 {
		t.Fatal("拒绝请求不应外发")
	}
}

func TestPostgresModelProbeCapacityExactVersionAndDisable(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	old := newProbeFixture(t, true)
	newer := newProbeFixture(t, false)
	m := modelManager(t, s)
	in := saveProbeConfig(t, ctx, m, modelID, old.server.URL, "openai_chat", 32)
	other := saveProbeConfig(t, ctx, m, otherModelID, old.server.URL, "openai_chat", 32)
	p := startProbe(t, ctx, s, m)
	if _, created, err := p.Submit(ctx, modelID, probeID, 1); err != nil || !created {
		t.Fatal(created, err)
	}
	receiveProbe(t, old)
	for i := 1; i < domain.ModelTestCapacity; i++ {
		id := fmt.Sprintf("%032x", i)
		configID := modelID
		if i == 2 {
			configID = otherModelID
		}
		if _, created, err := p.Submit(ctx, configID, id, 1); err != nil || !created {
			t.Fatal(created, err)
		}
	}
	if _, _, err := p.Submit(ctx, modelID, fmt.Sprintf("%032x", 100), 1); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("容量未限制", err)
	}
	if _, created, err := p.Submit(ctx, modelID, probeID, 1); err != nil || created {
		t.Fatal("容量满必须回放", created, err)
	}
	in.ExpectedRevision = 1
	in.BaseURL = newer.server.URL
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	other.ExpectedRevision = 1
	other.Enabled = false
	if _, err := m.Save(ctx, otherModelID, other); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Submit(ctx, modelID, fmt.Sprintf("%032x", 101), 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	close(old.release)
	for i := 1; i < domain.ModelTestCapacity; i++ {
		r := waitProbe(t, s, fmt.Sprintf("%032x", i), "completed")
		if i == 2 {
			if r.Outcome != "not_sent" || r.Attempted == nil || *r.Attempted {
				t.Fatal(r)
			}
		} else if r.Outcome != "valid_response" {
			t.Fatal(r)
		}
	}
	waitProbe(t, s, probeID, "completed")
	if old.hits.Load() != 7 || newer.hits.Load() != 0 {
		t.Fatal("不能把旧确认静默发送到新版", old.hits.Load(), newer.hits.Load())
	}
}

func TestPostgresModelProbePersistFailures(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	f := newProbeFixture(t, false)
	m := modelManager(t, s)
	saveProbeConfig(t, ctx, m, modelID, f.server.URL, "openai_chat", 32)
	// INSERT故障只影响受理，绝不能在事务提交前发送。
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION fail_probe_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private-db-error'; END $$;
		CREATE TRIGGER test_fail_probe_insert BEFORE INSERT ON model_tests FOR EACH ROW EXECUTE FUNCTION fail_probe_insert()`); err != nil {
		t.Fatal(err)
	}
	p := startProbe(t, ctx, s, m)
	api := newProbeAPI(t, s, m, p)
	if code, b := api.call(t, "POST", testPath(modelID, probeID), confirmation, api.token, ""); code != 503 || bytes.Contains(b, []byte("private-db-error")) {
		t.Fatal(code, string(b))
	}
	if _, err := s.GetModelTest(ctx, probeID); !errors.Is(err, domain.ErrNotFound) || f.hits.Load() != 0 {
		t.Fatal(err, f.hits.Load())
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER test_fail_probe_insert ON model_tests; DROP FUNCTION fail_probe_insert();
		CREATE FUNCTION fail_probe_finish() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.outcome='valid_response' THEN RAISE EXCEPTION 'private-db-error'; END IF; RETURN NEW; END $$;
		CREATE TRIGGER test_fail_probe_finish BEFORE UPDATE ON model_tests FOR EACH ROW EXECUTE FUNCTION fail_probe_finish()`); err != nil {
		t.Fatal(err)
	}
	if _, created, err := p.Submit(ctx, modelID, probeID, 1); err != nil || !created {
		t.Fatal(created, err)
	}
	select {
	case <-p.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("写失败必须停止执行服务")
	}
	if p.Err() == nil || f.hits.Load() != 1 {
		t.Fatal(p.Err(), f.hits.Load())
	}
	r := waitProbe(t, s, probeID, "completed")
	if r.Outcome != "transport_unknown" || r.Attempted != nil {
		t.Fatal(r)
	}
	if _, created, err := p.Submit(ctx, modelID, probeID, 1); err != nil || created {
		t.Fatal(created, err)
	}
	if _, _, err := p.Submit(ctx, modelID, fmt.Sprintf("%032x", 100), 1); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestPostgresModelProbeCancelAndOutcomes(t *testing.T) {
	for _, kind := range []string{"error", "invalid", "disconnect", "cancel", "nonstandard"} {
		t.Run(kind, func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			f := newProbeFixture(t, kind == "cancel")
			m := modelManager(t, s)
			saveProbeConfig(t, ctx, m, modelID, f.server.URL+"/"+kind, "openai_chat", 32)
			p := startProbe(t, ctx, s, m)
			reqCtx, cancel := context.WithCancel(ctx)
			if _, _, err := p.Submit(reqCtx, modelID, probeID, 1); err != nil {
				t.Fatal(err)
			}
			cancel()
			receiveProbe(t, f) // HTTP请求context取消不取消服务管理的实际调用。
			want := map[string]string{"error": "http_error", "invalid": "invalid_response", "disconnect": "transport_unknown", "cancel": "transport_unknown", "nonstandard": "http_error"}[kind]
			if kind == "cancel" {
				if err := p.Close(ctx); err != nil {
					t.Fatal(err)
				}
			}
			r := waitProbe(t, s, probeID, "completed")
			if r.Outcome != want || r.Attempted == nil || !*r.Attempted || f.hits.Load() != 1 {
				t.Fatal(r, f.hits.Load())
			}
			if kind == "nonstandard" && (r.HTTPStatus == nil || *r.HTTPStatus != 700) {
				t.Fatal("不能丢失实际非标准HTTP状态", r)
			}
		})
	}
}

func TestPostgresModelProbeV3UpgradeRecoveryAndLateResult(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	m := modelManager(t, s)
	saveProbeConfig(t, ctx, m, modelID, "http://127.0.0.1:9/v1", "openai_chat", 32)
	start, err := s.StartExploration(ctx, startKey, startInput(), true)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(mustSnapshot(t, ctx, s, start.Thread.ID))
	configBefore, err := s.GetModelConfig(ctx, modelID, 1)
	if err != nil {
		t.Fatal(err)
	}
	dropLedgerFixture(t, s, ctx)
	if _, err := s.pool.Exec(ctx, `DROP TABLE mission_models; DROP FUNCTION reject_mission_model_mutation(); DROP TABLE model_tests; DROP FUNCTION protect_model_test(); DELETE FROM wscyber_schema_migrations WHERE version>=4`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	after, _ := json.Marshal(mustSnapshot(t, ctx, s, start.Thread.ID))
	if !bytes.Equal(before, after) {
		t.Fatal("v3→v4改变研究")
	}
	configAfter, err := s.GetModelConfig(ctx, modelID, 1)
	if err != nil || !bytes.Equal(configBefore.Ciphertext, configAfter.Ciphertext) {
		t.Fatal(err)
	}
	if r, err := s.GetOperation(ctx, startKey); err != nil || r.ResourceID != start.Mission.ID {
		t.Fatal(r, err)
	}
	for _, id := range []string{probeID, fmt.Sprintf("%032x", 1)} {
		if _, created, err := s.AcceptModelTest(ctx, modelID, id, 1, m.ValidateRecordForUse); err != nil || !created {
			t.Fatal(created, err)
		}
	}
	claimed, err := s.ClaimModelTest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{probeID, fmt.Sprintf("%032x", 1)} {
		r := waitProbe(t, s, id, "completed")
		if r.Outcome != "transport_unknown" || r.Attempted != nil || r.HTTPStatus != nil {
			t.Fatal(r)
		}
		if id == claimed.ID && r.StartedAt == nil {
			t.Fatal("恢复不能丢失开始时间")
		}
		if id != claimed.ID && r.StartedAt != nil {
			t.Fatal("恢复不能捏造开始时间")
		}
	}
	yes := true
	code := 200
	if err := s.CompleteModelTest(ctx, claimed.ID, "valid_response", &code, &yes); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("迟到结果不得覆盖恢复终态", err)
	}
	if _, err := s.ClaimModelTest(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("恢复不可重新领取", err)
	}
	for _, sql := range []string{`UPDATE model_tests SET outcome='valid_response' WHERE id=$1`, `DELETE FROM model_tests WHERE id=$1`} {
		if _, err := s.pool.Exec(ctx, sql, probeID); err == nil {
			t.Fatal("终态不可变")
		}
	}
	if err := s.Recover(ctx); err != nil {
		t.Fatal("恢复需可重复", err)
	}
}

func TestPostgresModelProbeAdmissionSerializedWithDisable(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	m := modelManager(t, s)
	in := saveProbeConfig(t, ctx, m, modelID, "http://127.0.0.1:9", "openai_chat", 32)
	// 持有与保存相同的行锁，受理必须等待，而非信任事务前读到的enabled。
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT singleton FROM model_settings WHERE singleton FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, _, e := s.AcceptModelTest(ctx, modelID, probeID, 1, m.ValidateRecordForUse); result <- e }()
	select {
	case e := <-result:
		t.Fatal("受理未等待锁", e)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := tx.Exec(ctx, `UPDATE model_configs SET enabled=false WHERE id=$1`, modelID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	// 反向顺序：受理已提交后禁用，只阻止尚未发起的执行。
	in.ExpectedRevision = 1
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AcceptModelTest(ctx, modelID, probeID, 2, m.ValidateRecordForUse); err != nil {
		t.Fatal(err)
	}
	in.ExpectedRevision = 2
	in.Enabled = false
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	if _, created, err := s.AcceptModelTest(ctx, modelID, probeID, 2, nil); err != nil || created {
		t.Fatal("事务内回放也必须优先于禁用/版本及解密检查", created, err)
	}
	startProbe(t, ctx, s, m)
	r := waitProbe(t, s, probeID, "completed")
	if r.Outcome != "not_sent" || r.Attempted == nil || *r.Attempted {
		t.Fatal(r)
	}
}

func TestPostgresModelProbeLostLockDoesNotResend(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	f := newProbeFixture(t, true)
	m := modelManager(t, s)
	saveProbeConfig(t, ctx, m, modelID, f.server.URL, "openai_chat", 32)
	p := startProbe(t, ctx, s, m)
	if _, _, err := p.Submit(ctx, modelID, probeID, 1); err != nil {
		t.Fatal(err)
	}
	receiveProbe(t, f)
	s.lockMu.Lock()
	err := s.lock.Close(ctx)
	s.lockMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	close(f.release)
	select {
	case <-p.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("失锁应停止")
	}
	r, err := s.GetModelTest(ctx, probeID)
	if err != nil || r.Status != "running" || p.Err() == nil || f.hits.Load() != 1 {
		t.Fatal(r, err, p.Err(), f.hits.Load())
	}
	if _, created, err := p.Submit(ctx, modelID, probeID, 1); err != nil || created {
		t.Fatal(created, err)
	}
}
