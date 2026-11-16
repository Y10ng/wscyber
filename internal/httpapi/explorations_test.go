package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wscyber/internal/domain"
)

const operationKey = "00112233-4455-4677-8899-aabbccddeeff"
const explorationBody = `{"description":"目标及已知材料","goal":"验证访问控制","allowed_origins":["HTTPS://EXAMPLE.test:443","https://a.test","https://example.test"]}`

type explorationRepo struct {
	domain.Repository
	calls      int
	input      domain.ExplorationInput
	configured bool
	err        error
}

func (r *explorationRepo) StartExploration(_ context.Context, key string, in domain.ExplorationInput, configured bool) (domain.ExplorationStart, error) {
	r.calls++
	r.input, r.configured = in, configured
	return domain.ExplorationStart{StartStatus: "accepted"}, r.err
}
func (r *explorationRepo) GetOperation(_ context.Context, key string) (domain.OperationReceipt, error) {
	r.calls++
	return domain.OperationReceipt{Key: key, Method: "POST", Path: "/api/explorations", ResourceType: "mission", ResourceID: "m", ResponseStatus: 202}, r.err
}

func explorationRequest(h http.Handler, body string, keys ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/explorations", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	for _, key := range keys {
		req.Header.Add("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestExplorationValidation(t *testing.T) {
	r := &explorationRepo{}
	h := New(r, nil, Config{Token: testToken, System: SystemInfo{ModelConfigured: true}})
	for _, keys := range [][]string{nil, {"short"}, {strings.Repeat("a", 129)}, {operationKey, operationKey}, {operationKey + "/"}, {operationKey + ","}} {
		if w := explorationRequest(h, explorationBody, keys...); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, body := range []string{
		`{"description":"a","goal":"b","allowed_origins":[]}`,
		`{"description":"a","goal":"b","allowed_origins":["https://test.invalid/secret"]}`,
		`{"description":"a","goal":"b","allowed_origins":["https://user:private@test.invalid"]}`,
		`{"description":"a","goal":"b","allowed_origins":["https://test.invalid"],"auto_run":true}`,
		`{"description":" ","goal":"b","allowed_origins":["https://test.invalid"]}`,
		explorationBody + " {}",
	} {
		w := explorationRequest(h, body, operationKey)
		if w.Code != 400 || strings.Contains(w.Body.String(), "private") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if r.calls != 0 {
		t.Fatal("无效输入不应调用持久层", r.calls)
	}
	w := explorationRequest(h, explorationBody, operationKey)
	if w.Code != 202 || !r.configured || len(r.input.AllowedOrigins) != 2 || r.input.AllowedOrigins[0] != "https://a.test" || r.input.AllowedOrigins[1] != "https://example.test" || w.Header().Get("Location") != "/api/operations/"+operationKey {
		t.Fatal(w.Code, w.Body.String(), r.input)
	}
}

func TestExplorationAuthAndReceipts(t *testing.T) {
	r := &explorationRepo{}
	h := New(r, nil, Config{Token: testToken})
	for _, path := range []string{"/api/explorations", "/api/operations/" + operationKey} {
		method := "GET"
		if path == "/api/explorations" {
			method = "POST"
		}
		if w := request(h, method, path, explorationBody, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(explorationBody))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	if r.calls != 0 {
		t.Fatal("未授权请求访问了持久层")
	}
	w := explorationRequest(h, explorationBody, operationKey)
	if w.Code != 202 || r.configured {
		t.Fatal("未配置模型也须将去重核对交给事务", w.Code)
	}
	r.err = domain.ErrUnavailable
	if w := explorationRequest(h, explorationBody, operationKey); w.Code != 503 {
		t.Fatal(w.Code)
	}
	r.err = domain.ErrConflict
	if w := explorationRequest(h, explorationBody, operationKey); w.Code != 409 {
		t.Fatal(w.Code)
	}
	r.err = domain.ErrNotFound
	if w := request(h, "GET", "/api/operations/"+operationKey, "", testToken); w.Code != 404 {
		t.Fatal(w.Code)
	}
	r.err = nil
	w = request(h, "GET", "/api/operations/"+operationKey, "", testToken)
	var receipt map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(receipt) != 7 || receipt["resource_id"] != "m" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, receipt)
	}
}
