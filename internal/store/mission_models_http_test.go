package store

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/httpapi"
	"wscyber/internal/model"
	"wscyber/internal/modelconfig"
)

func TestPostgresMissionModelHTTPDynamicControl(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	m := modelManager(t, s)
	router := model.NewRouted(s, m, nil, "")
	const token = "isolated-managed-http-fixture-token"
	api := httptest.NewServer(httpapi.New(s, nil, httpapi.Config{Token: token, Models: m, MissionModels: router}))
	defer api.Close()
	call := func(method, path string, input any, status int, key string) []byte {
		t.Helper()
		var body string
		if raw, ok := input.(string); ok {
			body = raw
		} else if input != nil {
			b, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			body = string(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, api.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		resp, err := api.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != status {
			t.Fatalf("%s %s: %d %s %v", method, path, resp.StatusCode, raw, err)
		}
		if strings.Contains(string(raw), "never-return") {
			t.Fatal("秘密回显")
		}
		return raw
	}
	system := func(configured bool, source, modelName string) {
		t.Helper()
		raw := call("GET", "/api/system", nil, 200, "")
		var info httpapi.SystemInfo
		if json.Unmarshal(raw, &info) != nil || info.ModelConfigured != configured || info.ModelSource != source || info.Model != modelName || !info.ModelManagementAvailable {
			t.Fatal(string(raw))
		}
	}
	system(false, "none", "")
	key := "never-return-key"
	edit := modelconfig.ModelConfigEdit{ModelConnection: modelInput().ModelConnection, Enabled: true, APIKey: &key, Headers: []modelconfig.HeaderEntry{}, Parameters: []modelconfig.ParameterEntry{}}
	path := "/api/model-configs/" + modelID
	call("PATCH", path, edit, 201, "")
	call("PUT", "/api/model-settings", `{"expected_revision":0,"default_config_id":"`+modelID+`"}`, 200, "")
	system(true, "managed", "fixture-model")
	raw := call("POST", "/api/explorations", startInput(), 202, startKey)
	var accepted domain.ExplorationStart
	if json.Unmarshal(raw, &accepted) != nil {
		t.Fatal(string(raw))
	}
	control := "/api/threads/" + accepted.Thread.ID + "/control"
	call("POST", control, `{"command":"pause"}`, 200, "")
	call("PUT", "/api/model-settings", `{"expected_revision":1,"default_config_id":""}`, 200, "")
	system(false, "none", "")
	call("POST", control, `{"command":"run"}`, 200, "")
	if thread, err := s.GetThread(ctx, accepted.Thread.ID); err != nil || !thread.AutoRun || thread.Status != "ready" {
		t.Fatal(thread, err)
	}
	edit.ExpectedRevision = 1
	edit.Name = "已编辑"
	edit.Model = "edited-model"
	edit.APIKey = nil
	call("PATCH", path, edit, 200, "")
	var view domain.MissionModelView
	raw = call("GET", "/api/missions/"+accepted.Mission.ID+"/model", nil, 200, "")
	if json.Unmarshal(raw, &view) != nil || view.Name != "隔离测试" || view.Model != "fixture-model" || !view.Enabled {
		t.Fatal(string(raw))
	}
	if strings.Contains(string(raw), "revision") {
		t.Fatal("安全绑定视图不应包含版本")
	}
	edit.ExpectedRevision = 2
	edit.Enabled = false
	call("PATCH", path, edit, 200, "")
	call("POST", control, `{"command":"pause"}`, 200, "")
	call("POST", control, `{"command":"run"}`, 409, "")
	call("POST", control, `{"command":"resume"}`, 200, "")
	if thread, err := s.GetThread(ctx, accepted.Thread.ID); err != nil || thread.AutoRun {
		t.Fatal("resume不得启用模型", thread, err)
	}
	call("POST", "/api/explorations", startInput(), 202, startKey)
	call("POST", "/api/explorations", startInput(), 503, "missing-default-fixture-key001")
	explicit := startInput()
	explicit.ModelConfigID = strings.ToUpper(modelID)
	call("POST", "/api/explorations", explicit, 400, "invalid-model-fixture-key-0001")
	call("GET", "/api/missions/nonexistent/model", nil, 404, "")
	legacy, err := s.CreateMission(ctx, domain.Mission{Title: "旧任务", Goal: "无环境模型"})
	if err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/missions/"+legacy.ID+"/model", nil, 503, "")
}
