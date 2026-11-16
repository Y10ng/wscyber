package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
)

const testToken = "test-token-not-a-production-secret-123456"

type repoStub struct {
	domain.Repository
	calls   int
	mission domain.Mission
	state   string
	auto    bool
}

func (r *repoStub) ListMissions(context.Context) ([]domain.Mission, error) {
	r.calls++
	return []domain.Mission{}, nil
}
func (r *repoStub) CreateMission(_ context.Context, m domain.Mission) (domain.Mission, error) {
	r.calls++
	r.mission = m
	m.ID = "test"
	return m, nil
}
func (r *repoStub) SetThreadControl(_ context.Context, id, state string, auto bool) (domain.Thread, error) {
	r.calls++
	r.state = state
	r.auto = auto
	return domain.Thread{ID: id, Status: state, AutoRun: auto}, nil
}
func handler(r *repoStub, webDir string) http.Handler {
	return New(r, execution.New(execution.Config{}), Config{Token: testToken, WebDir: webDir, System: SystemInfo{Name: "wscyber"}})
}
func request(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func TestAuthAndNoCORS(t *testing.T) {
	r := &repoStub{}
	h := handler(r, t.TempDir())
	for _, token := range []string{"", "wrong"} {
		w := request(h, "GET", "/api/missions", "", token)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if r.calls != 0 {
		t.Fatal("unauthorized database access")
	}
	req := httptest.NewRequest("GET", "http://localhost/api/missions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Origin", "https://other.invalid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("cross origin accepted")
	}
	w = request(h, "GET", "/api/missions", "", testToken)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "GET", "/healthz", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), testToken) {
		t.Fatal(w.Code)
	}
}
func TestMissionValidationAndNormalization(t *testing.T) {
	r := &repoStub{}
	h := handler(r, t.TempDir())
	for _, body := range []string{
		`{"title":"a","goal":"b","allowed_origins":["https://example.test/path"]}`,
		`{"title":"a","goal":"b","allowed_origins":["https://example.test"],"auto_run":true}`,
		`{"title":"a","goal":"b","allowed_origins":["https://example.test"]} {}`,
		`{"title":"","goal":"b","allowed_origins":[]}`,
	} {
		w := request(h, "POST", "/api/missions", body, testToken)
		if w.Code != 400 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if r.calls != 0 {
		t.Fatal("invalid input persisted")
	}
	w := request(h, "POST", "/api/missions", `{"title":"a","goal":"b","allowed_origins":["https://example.test:443","https://example.test"]}`, testToken)
	if w.Code != 201 || len(r.mission.AllowedOrigins) != 1 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestControlNoImplicitModel(t *testing.T) {
	r := &repoStub{}
	h := handler(r, t.TempDir())
	w := request(h, "POST", "/api/threads/a/control", `{"command":"run"}`, testToken)
	if w.Code != 503 || r.calls != 0 {
		t.Fatal(w.Code)
	}
	w = request(h, "POST", "/api/threads/a/control", `{"command":"resume"}`, testToken)
	if w.Code != 200 || r.auto || r.state != "ready" {
		t.Fatal(w.Code)
	}
	w = request(h, "POST", "/api/threads/a/control", `{"command":"pause"}`, testToken)
	if w.Code != 200 || r.auto || r.state != "paused" {
		t.Fatal(w.Code)
	}
}
func TestStaticAndUnknownAPI(t *testing.T) {
	d := t.TempDir()
	if e := os.WriteFile(filepath.Join(d, "index.html"), []byte("<html>app</html>"), 0600); e != nil {
		t.Fatal(e)
	}
	h := handler(&repoStub{}, d)
	w := request(h, "GET", "/", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "app") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/nonexistent", "", testToken)
	var e map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if w.Code != 404 || e["error"] == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(h, "GET", "/missing.js", "", "")
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
