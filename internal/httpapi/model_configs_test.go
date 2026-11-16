package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelConfigUnavailableStillRequiresAuthAndSameOrigin(t *testing.T) {
	h := handler(&repoStub{}, t.TempDir())
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/api/model-configs", ""},
		{"GET", "/api/model-configs/00112233445566778899aabbccddeeff", ""},
		{"PUT", "/api/model-configs/00112233445566778899aabbccddeeff", `{"api_key":"must-not-echo-secret"}`},
		{"GET", "/api/model-settings", ""},
		{"PUT", "/api/model-settings", `{"default_config_id":""}`},
	} {
		if w := request(h, endpoint.method, endpoint.path, endpoint.body, ""); w.Code != 401 {
			t.Fatal(w.Code)
		}
		w := request(h, endpoint.method, endpoint.path, endpoint.body, testToken)
		if w.Code != 503 || strings.Contains(w.Body.String(), "must-not-echo-secret") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		req := httptest.NewRequest(endpoint.method, "http://localhost"+endpoint.path, strings.NewReader(endpoint.body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Origin", "https://elsewhere.invalid")
		recorder := httptest.NewRecorder()
		h.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Fatal(recorder.Code)
		}
	}
}
