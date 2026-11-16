package store

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/httpapi"
	"wscyber/internal/modelconfig"
)

const modelID = "00112233445566778899aabbccddeeff"

func modelManager(t *testing.T, s *Store) *modelconfig.Manager {
	t.Helper()
	v, err := modelconfig.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{71}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return modelconfig.New(s, v)
}
func modelInput() domain.ModelConfigWrite {
	return domain.ModelConfigWrite{ModelConnection: domain.ModelConnection{Name: "隔离测试", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:9/v1", Model: "fixture-model", Auth: "bearer", TimeoutSeconds: 20, MaxOutputTokens: 512}, Enabled: true,
		APIKey: domain.SecretChange[string]{Mode: "replace", Value: "never-return-api-secret"}, Headers: domain.SecretChange[map[string]string]{Mode: "replace", Value: map[string]string{"X-Private": "never-return-header-secret"}}, Parameters: domain.SecretChange[map[string]json.RawMessage]{Mode: "replace", Value: map[string]json.RawMessage{"metadata": json.RawMessage(`{"token":"never-return-parameter-secret"}`)}}}
}
func TestPostgresModelConfigConcurrentVersions(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	m := modelManager(t, s)
	if list, err := m.List(ctx); err != nil || list == nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	settings, err := m.Settings(ctx)
	if err != nil || settings.Revision != 0 || settings.DefaultConfigID != "" {
		t.Fatal(settings, err)
	}
	const n = 5
	start := make(chan struct{})
	results := make(chan error, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { <-start; _, err := m.Save(ctx, modelID, modelInput()); results <- err })
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	conflicts := 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, domain.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != n-1 {
		t.Fatal(success, conflicts)
	}
	first, err := s.GetModelConfig(ctx, modelID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"never-return-api-secret", "never-return-header-secret", "never-return-parameter-secret"} {
		if bytes.Contains(first.Ciphertext, []byte(secret)) {
			t.Fatal("数据库出现明文秘密")
		}
	}
	settings, err = m.SetDefault(ctx, modelID, 0)
	if err != nil || settings.Revision != 1 {
		t.Fatal(settings, err)
	}
	if _, err := m.SetDefault(ctx, "", 0); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("旧设置版本不得覆盖", err)
	}
	in := modelInput()
	in.ExpectedRevision = 1
	in.Enabled = false
	in.APIKey = domain.SecretChange[string]{Mode: "keep"}
	in.Headers = domain.SecretChange[map[string]string]{Mode: "keep"}
	in.Parameters = domain.SecretChange[map[string]json.RawMessage]{Mode: "keep"}
	second, err := m.Save(ctx, modelID, in)
	if err != nil || second.Revision != 2 {
		t.Fatal(second, err)
	}
	history, err := s.GetModelConfig(ctx, modelID, 1)
	if err != nil || history.Enabled || !bytes.Equal(history.Ciphertext, first.Ciphertext) {
		t.Fatal("旧密文不可变且读取遵守当前禁用状态", err)
	}
	if _, err := m.SetDefault(ctx, modelID, 1); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("不能选禁用配置", err)
	}
	for _, sql := range []string{`UPDATE model_config_versions SET ciphertext='changed' WHERE config_id=$1`, `DELETE FROM model_config_versions WHERE config_id=$1`} {
		if _, err := s.pool.Exec(ctx, sql, modelID); err == nil {
			t.Fatal("历史版本不应可修改")
		}
	}
	s.Close()
	reopened, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	v, err := modelManager(t, reopened).Get(ctx, modelID)
	if err != nil || v.Revision != 2 || v.Enabled || !v.APIKeySet {
		t.Fatal(v, err)
	}
	var count int
	if err := reopened.pool.QueryRow(ctx, `SELECT count(*) FROM model_config_versions`).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
}

func TestPostgresModelConfigV2UpgradeAndRollback(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	start, err := s.StartExploration(ctx, startKey, startInput(), true)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(mustSnapshot(t, ctx, s, start.Thread.ID))
	// 仅本测试生成的schema：退去空v3结构，保留真实v1/v2业务和不可变提交回执。
	dropLedgerFixture(t, s, ctx)
	if _, err := s.pool.Exec(ctx, `DROP TABLE mission_models; DROP FUNCTION reject_mission_model_mutation();
		DROP TABLE model_tests; DROP FUNCTION protect_model_test();
		DROP TABLE model_settings; ALTER TABLE model_configs DROP CONSTRAINT model_configs_current_version;
		DROP TABLE model_config_versions; DROP TABLE model_configs; DROP FUNCTION reject_model_version_mutation();
		DELETE FROM wscyber_schema_migrations WHERE version>=3`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	upgraded, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(upgraded.Close)
	after, _ := json.Marshal(mustSnapshot(t, ctx, upgraded, start.Thread.ID))
	if !bytes.Equal(before, after) {
		t.Fatal("v2升级改变了旧业务")
	}
	if receipt, err := upgraded.GetOperation(ctx, startKey); err != nil || receipt.ResourceID != start.Mission.ID {
		t.Fatal(receipt, err)
	}
	m := modelManager(t, upgraded)
	if _, err := m.Save(ctx, modelID, modelInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.pool.Exec(ctx, `CREATE FUNCTION fail_model_version() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test rollback'; END $$;
		CREATE TRIGGER test_fail_model_version BEFORE INSERT ON model_config_versions FOR EACH ROW EXECUTE FUNCTION fail_model_version()`); err != nil {
		t.Fatal(err)
	}
	in := modelInput()
	in.ExpectedRevision = 1
	in.Enabled = false
	if _, err := m.Save(ctx, modelID, in); err == nil {
		t.Fatal("版本写入故障应失败")
	}
	v, err := m.Get(ctx, modelID)
	if err != nil || v.Revision != 1 || !v.Enabled {
		t.Fatal("故障不能只提交头部", v, err)
	}
}

func TestPostgresModelConfigHTTPBoundary(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	m := modelManager(t, s)
	const token = "isolated-config-api-token-not-a-secret"
	h := httpapi.New(s, execution.New(execution.Config{}), httpapi.Config{Token: token, WebDir: t.TempDir(), Models: m})
	server := httptest.NewServer(h)
	defer server.Close()
	call := func(method, path, body, auth, origin string) (int, string, http.Header) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", "Bearer "+auth)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b), resp.Header
	}
	raw, _ := json.Marshal(modelInput())
	path := "/api/model-configs/" + modelID
	if code, _, _ := call("PUT", path, string(raw), "", ""); code != 401 {
		t.Fatal(code)
	}
	if code, _, _ := call("PUT", path, string(raw), token, "https://other.invalid"); code != 403 {
		t.Fatal(code)
	}
	if code, body, headers := call("PUT", path, string(raw), token, ""); code != 201 || strings.Contains(body, "never-return") || headers.Get("Cache-Control") != "no-store" {
		t.Fatal(code, body)
	}
	for _, readPath := range []string{path, "/api/model-configs"} {
		if code, body, _ := call("GET", readPath, "", token, ""); code != 200 || strings.Contains(body, "never-return") || !strings.Contains(body, `"api_key_set":true`) {
			t.Fatal(code, body)
		}
	}
	if code, _, _ := call("PUT", path, string(raw), token, ""); code != 409 {
		t.Fatal("失联重发不应重复写版本", code)
	}
	bad := strings.Replace(string(raw), `"enabled":true`, `"enabled":true,"enabled":false`, 1)
	if code, _, _ := call("PUT", path, bad, token, ""); code != 400 {
		t.Fatal(code)
	}
	if code, _, _ := call("GET", path+"?reveal=true", "", token, ""); code != 400 {
		t.Fatal("不允许读取原秘密", code)
	}
	if code, body, _ := call("PUT", "/api/model-settings", fmt.Sprintf(`{"default_config_id":%q,"expected_revision":0}`, modelID), token, ""); code != 200 || !strings.Contains(body, modelID) {
		t.Fatal(code, body)
	}
	// 保存/选默认不启动研究，没有HTTP目标动作或模型调用实体。
	missions, err := s.ListMissions(ctx)
	if err != nil || len(missions) != 0 {
		t.Fatal(missions, err)
	}
}
