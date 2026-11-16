// Package integration_test 仅验证有限、确定的本地工程链路，不衡量泛化安全发现能力。
// PostgreSQL 测试须显式设置 WSCYBER_INTEGRATION_DATABASE_URL；测试不启动数据库。
package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/httpapi"
	"wscyber/internal/research"
	"wscyber/internal/store"
)

const testPollInterval = 20 * time.Millisecond

func requireIntegrationDatabase(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("WSCYBER_INTEGRATION_DATABASE_URL")
	if dsn == "" {
		t.Skip("未配置 WSCYBER_INTEGRATION_DATABASE_URL，跳过跨模块 PostgreSQL 验收")
	}
	return dsn
}

// 每例使用新 schema；清理只删除本例创建的 schema，不删除数据库或 public 中的记录。
// 各测试不调用 Parallel，因为单实例 advisory lock 是数据库级锁。
func isolatedStore(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	dsn := requireIntegrationDatabase(t)
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("WSCYBER_INTEGRATION_DATABASE_URL 必须是 PostgreSQL URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = admin.Close(cleanup)
	})
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	schemaName := "integration_test_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{schemaName}.Sanitize()
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, `DROP SCHEMA `+quoted+` CASCADE`); err != nil {
			t.Errorf("清理本例 schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	repo, err := store.Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(repo.Close)
	if err = repo.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	return repo, ctx
}

type harness struct {
	ctx    context.Context
	api    *httptest.Server
	client *http.Client
	token  string
}

func newHarness(t *testing.T, model domain.Model, config execution.Config) *harness {
	t.Helper()
	repo, ctx := isolatedStore(t)
	config.AllowLoopback = true // 仅测试显式允许本机 httptest origin。
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if config.MaxOutputBytes == 0 {
		config.MaxOutputBytes = 64 << 10
	}
	exec := execution.New(config)
	modelName := ""
	if model != nil {
		modelName = model.Name()
	}
	token := "wscyber-integration-only-token"
	api := httptest.NewServer(httpapi.New(repo, exec, httpapi.Config{
		Token: token,
		System: httpapi.SystemInfo{
			Name: "wscyber", Version: "integration-test", ModelConfigured: model != nil,
			Model: modelName, NetworkMode: "none + brokered HTTP", MaxParallel: 1,
		},
	}))
	transport := &http.Transport{Proxy: nil}
	h := &harness{
		ctx: ctx, api: api, token: token,
		client: &http.Client{
			Transport: transport, Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	engine := research.New(repo, model, exec, research.Config{
		MaxParallel: 1, MaxSteps: 4, PollInterval: testPollInterval,
		ModelTimeout: 5 * time.Second, ActionTimeout: config.Timeout + 10*time.Second,
	})
	engineCtx, stopEngine := context.WithCancel(ctx)
	engineDone := make(chan struct{})
	go func() {
		defer close(engineDone)
		engine.Run(engineCtx)
	}()
	// Cleanup 后进先出：必须等 worker 保存结果，再运行 isolatedStore 注册的 Close。
	t.Cleanup(func() {
		stopEngine()
		<-engineDone
		transport.CloseIdleConnections()
		api.Close()
	})
	return h
}

func (h *harness) request(t *testing.T, method, path, token string, input any, want int, keys ...string) []byte {
	t.Helper()
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(h.ctx, method, h.api.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, key := range keys {
		req.Header.Add("Idempotency-Key", key)
	}
	res, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s 返回 %d，期望 %d：%s", method, path, res.StatusCode, want, encoded)
	}
	return encoded
}

func (h *harness) call(t *testing.T, method, path string, input any, want int, output any) {
	t.Helper()
	encoded := h.request(t, method, path, h.token, input, want)
	if output != nil {
		if err := json.Unmarshal(encoded, output); err != nil {
			t.Fatalf("解析 %s 响应: %v；%s", path, err, encoded)
		}
	}
}

func (h *harness) createThread(t *testing.T, origin string) (domain.Mission, domain.Thread) {
	t.Helper()
	var mission domain.Mission
	h.call(t, "POST", "/api/missions", map[string]any{
		"title": "本地工程验收", "origin": "仅受控 httptest fixture", "goal": "验证审批、真实执行与证据持久化，不验证漏洞发现质量",
		"allowed_origins": []string{origin},
	}, http.StatusCreated, &mission)
	var thread domain.Thread
	h.call(t, "POST", "/api/missions/"+mission.ID+"/threads", map[string]any{
		"title": "有限测试方向", "question": "本次明确批准的动作能否保存真实结果？", "rationale": "只运行本地确定性测试", "priority": 50,
	}, http.StatusCreated, &thread)
	if thread.MissionID != mission.ID || thread.Status != "ready" || thread.AutoRun || thread.StepCount != 0 {
		t.Fatal("新方向必须保持 ready、未自动运行", thread)
	}
	return mission, thread
}

func (h *harness) snapshot(t *testing.T, id string) domain.Snapshot {
	t.Helper()
	var snapshot domain.Snapshot
	h.call(t, "GET", "/api/threads/"+id, nil, http.StatusOK, &snapshot)
	return snapshot
}

func (h *harness) waitSnapshot(t *testing.T, id, description string, ready func(domain.Snapshot) bool) domain.Snapshot {
	t.Helper()
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(testPollInterval)
	defer ticker.Stop()
	for {
		snapshot := h.snapshot(t, id)
		if ready(snapshot) {
			return snapshot
		}
		select {
		case <-h.ctx.Done():
			t.Fatalf("等待%s时测试上下文结束：%v", description, h.ctx.Err())
		case <-deadline.C:
			t.Fatalf("等待%s超时；最新快照：%+v", description, snapshot)
		case <-ticker.C:
		}
	}
}

// 连续观察多个调度周期，负面断言不只依赖一次瞬时读取。
func assertStable(t *testing.T, duration time.Duration, check func()) {
	t.Helper()
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(testPollInterval)
	defer ticker.Stop()
	for {
		check()
		select {
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
}

func finishedAction(snapshot domain.Snapshot, id string) bool {
	for _, action := range snapshot.Actions {
		if action.ID == id {
			return action.Status == "succeeded" || action.Status == "failed" || action.Status == "outcome_unknown"
		}
	}
	return false
}

func findAction(t *testing.T, snapshot domain.Snapshot, id string) domain.Action {
	t.Helper()
	for _, action := range snapshot.Actions {
		if action.ID == id {
			return action
		}
	}
	t.Fatalf("快照中缺少动作 %s", id)
	return domain.Action{}
}

func persistedResult(snapshot domain.Snapshot, actionID string) (domain.ActionResult, bool) {
	for _, observation := range snapshot.Observations {
		if observation.ActionID != actionID || observation.Kind != "action_result" {
			continue
		}
		var evidence struct {
			Status string              `json:"status"`
			Result domain.ActionResult `json:"result"`
			Error  string              `json:"error"`
		}
		if err := json.Unmarshal([]byte(observation.Content), &evidence); err == nil && evidence.Status == "succeeded" && evidence.Error == "" {
			return evidence.Result, true
		}
	}
	return domain.ActionResult{}, false
}
