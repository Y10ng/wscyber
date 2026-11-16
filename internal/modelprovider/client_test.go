package modelprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
)

func fixtureDoc(base, protocol, auth string) domain.ModelDocument {
	key := "fixture-secret"
	if auth == "none" {
		key = ""
	}
	return domain.ModelDocument{ModelConnection: domain.ModelConnection{Name: "本地协议测试", Protocol: protocol, BaseURL: base, Model: "fixture-model", Auth: auth, TimeoutSeconds: 2, MaxOutputTokens: 64}, Secrets: domain.ModelSecrets{APIKey: key, Headers: map[string]string{"X-Configured": "fixture-header"}, Parameters: map[string]json.RawMessage{"temperature": json.RawMessage(`0.25`)}}}
}
func probe() Input {
	return Input{System: "Only reply OK.", User: "Connection test.", MaxOutputTokens: 32}
}
func success(protocol string) string {
	if protocol == "anthropic_messages" {
		return `{"id":"msg_fixture","type":"message","role":"assistant","model":"fixture-model","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":1}}`
	}
	return `{"id":"chat_fixture","object":"chat.completion","model":"fixture-model","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`
}

func TestProtocolsExplicitConfiguration(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "OPENAI_ADMIN_KEY", "OPENAI_ORG_ID", "OPENAI_PROJECT_ID", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_PROFILE"} {
		t.Setenv(key, "do-not-inherit")
	}
	for _, key := range []string{"OPENAI_CUSTOM_HEADERS", "ANTHROPIC_CUSTOM_HEADERS"} {
		t.Setenv(key, "X-Environment-Secret: do-not-inherit\nAuthorization: Bearer do-not-inherit")
	}
	for _, key := range []string{"OPENAI_BASE_URL", "ANTHROPIC_BASE_URL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "http://127.0.0.1:9")
	}
	for _, tc := range []struct{ protocol, auth, path string }{{"openai_chat", "none", "/prefix/chat/completions"}, {"openai_chat", "bearer", "/prefix/chat/completions"}, {"anthropic_messages", "none", "/prefix/v1/messages"}, {"anthropic_messages", "bearer", "/prefix/v1/messages"}, {"anthropic_messages", "api_key", "/prefix/v1/messages"}} {
		t.Run(tc.protocol+"/"+tc.auth, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path != tc.path || r.Method != "POST" || r.URL.RawQuery != "" || r.ProtoMajor != 1 || !r.Close {
					t.Errorf("wrong method/path %s %s", r.Method, r.URL.Path)
				}
				if strings.Contains(fmt.Sprint(r.Header), "do-not-inherit") {
					t.Error("inherited environment header/credential")
				}
				if r.Header.Get("X-Configured") != "fixture-header" {
					t.Error("missing explicit header")
				}
				expectBearer, expectKey := "", ""
				if tc.auth == "bearer" {
					expectBearer = "Bearer fixture-secret"
				}
				if tc.auth == "api_key" {
					expectKey = "fixture-secret"
				}
				if r.Header.Get("Authorization") != expectBearer || r.Header.Get("X-Api-Key") != expectKey {
					t.Error("authentication mismatch")
				}
				if tc.protocol == "anthropic_messages" && r.Header.Get("Anthropic-Version") == "" {
					t.Error("missing SDK protocol version")
				}
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				limit := "max_completion_tokens"
				if tc.protocol == "anthropic_messages" {
					limit = "max_tokens"
				}
				if string(body[limit]) != "32" || string(body["model"]) != `"fixture-model"` || string(body["temperature"]) != "0.25" {
					t.Error("explicit payload mismatch")
				}
				var conversation []struct {
					Role    string          `json:"role"`
					Content json.RawMessage `json:"content"`
				}
				if err := json.Unmarshal(body["messages"], &conversation); err != nil {
					t.Error(err)
				}
				if tc.protocol == "openai_chat" {
					if len(conversation) != 2 || conversation[0].Role != "system" || string(conversation[0].Content) != `"Only reply OK."` || conversation[1].Role != "user" || string(conversation[1].Content) != `"Connection test."` {
						t.Error("wrong chat context")
					}
				} else {
					if len(conversation) != 1 || conversation[0].Role != "user" {
						t.Error("wrong messages roles")
					} else {
						var system, user []struct {
							Type string
							Text string
						}
						if json.Unmarshal(body["system"], &system) != nil || json.Unmarshal(conversation[0].Content, &user) != nil || len(system) != 1 || len(user) != 1 || system[0].Type != "text" || system[0].Text != probe().System || user[0].Type != "text" || user[0].Text != probe().User {
							t.Error("wrong messages context")
						}
					}
				}
				for _, field := range []string{"tools", "stream", "tool_choice"} {
					if _, exists := body[field]; exists {
						t.Errorf("unexpected field %s", field)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, success(tc.protocol))
			}))
			defer srv.Close()
			got := Complete(context.Background(), fixtureDoc(srv.URL+"/prefix", tc.protocol, tc.auth), probe())
			if got.Outcome != ValidResponse || got.Text != "OK" || got.HTTPStatus != 200 || hits.Load() != 1 {
				t.Fatalf("unexpected result %+v hits=%d", got, hits.Load())
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "OK") || strings.Contains(string(encoded), "fixture-secret") || strings.Contains(string(encoded), "usage") || strings.Contains(string(encoded), "tokens") {
				t.Fatal("result exposes body or secret")
			}
		})
	}
}

func TestFailureResponsesNeverRetryOrRedirect(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		for _, status := range []int{201, 301, 302, 307, 308, 400, 401, 429, 500, 503} {
			t.Run(fmt.Sprintf("%s/%d", protocol, status), func(t *testing.T) {
				var hits, target atomic.Int32
				redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { target.Add(1) }))
				defer redirect.Close()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					w.Header().Set("Location", redirect.URL)
					w.Header().Set("Retry-After", "0")
					w.Header().Set("X-Should-Retry", "true")
					w.WriteHeader(status)
					io.WriteString(w, `{"error":{"message":"fixture-secret"}}`)
				}))
				defer srv.Close()
				got := Complete(context.Background(), fixtureDoc(srv.URL, protocol, "bearer"), probe())
				if got.Outcome != HTTPError || got.HTTPStatus != status || hits.Load() != 1 || target.Load() != 0 || got.Text != "" {
					t.Fatalf("unexpected %+v hits=%d target=%d", got, hits.Load(), target.Load())
				}
			})
		}
	}
}

func TestInvalidResponsesAreNotSuccess(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		cases := map[string]string{"empty": "", "syntax": "not JSON", "null": "null", "missing": "{}"} // 超限未读完的响应在传输测试中要求unknown。
		good := success(protocol)
		if protocol == "openai_chat" {
			cases["length"] = strings.Replace(good, `"stop"`, `"length"`, 1)
			cases["tool"] = strings.Replace(good, `"content":"OK"`, `"content":"OK","tool_calls":[{"id":"t","type":"function","function":{"name":"run","arguments":"{}"}}]`, 1)
			cases["refusal"] = strings.Replace(good, `"content":"OK"`, `"content":"OK","refusal":"no"`, 1)
		} else {
			cases["length"] = strings.Replace(good, `"end_turn"`, `"max_tokens"`, 1)
			cases["tool"] = strings.Replace(good, `"type":"text"`, `"type":"tool_use"`, 1)
			cases["refusal"] = strings.Replace(good, `"end_turn"`, `"refusal"`, 1)
		}
		for name, body := range cases {
			t.Run(protocol+"/"+name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, body)
				}))
				defer srv.Close()
				got := Complete(context.Background(), fixtureDoc(srv.URL, protocol, "none"), probe())
				if got.Outcome != InvalidResponse || got.HTTPStatus != 200 || got.Text != "" {
					t.Fatalf("unexpected %+v", got)
				}
			})
		}
	}
}

func TestCancellationKeepsUnknownAndNoRetry(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			var hits atomic.Int32
			arrived := make(chan struct{})
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); close(arrived); <-release }))
			defer srv.Close()
			defer close(release)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan Result, 1)
			go func() { done <- Complete(ctx, fixtureDoc(srv.URL, protocol, "none"), probe()) }()
			select {
			case <-arrived:
				cancel()
			case <-time.After(3 * time.Second):
				t.Fatal("request not received")
			}
			select {
			case got := <-done:
				if got.Outcome != TransportUnknown || hits.Load() != 1 {
					t.Fatalf("unexpected %+v hits=%d", got, hits.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation not propagated")
			}
		})
	}
}

func TestInvalidConfigurationNeverSends(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	for _, mutate := range []func(*domain.ModelDocument){func(d *domain.ModelDocument) { d.Secrets.Headers["Host"] = "wrong" }, func(d *domain.ModelDocument) { d.Secrets.Parameters["messages.0.role"] = json.RawMessage(`"system"`) }, func(d *domain.ModelDocument) { d.BaseURL += "?key=secret" }, func(d *domain.ModelDocument) { d.Auth = "bearer"; d.Secrets.APIKey = "" }} {
		doc := fixtureDoc(srv.URL, "openai_chat", "none")
		mutate(&doc)
		got := Complete(context.Background(), doc, probe())
		if got.Outcome != NotSent || got.Attempted {
			t.Fatalf("unexpected %+v", got)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("invalid config sent request")
	}
}

func TestDNSAddressBoundary(t *testing.T) {
	for _, address := range []string{"0.0.0.0", "::", "169.254.169.254", "::ffff:169.254.169.254", "fe80::1", "224.0.0.1", "ff02::1"} {
		if allowedAddress(netip.MustParseAddr(address)) {
			t.Errorf("allowed %s", address)
		}
	}
	for _, address := range []string{"127.0.0.1", "::1", "10.0.0.2", "192.168.1.2", "8.8.8.8"} {
		if !allowedAddress(netip.MustParseAddr(address)) {
			t.Errorf("rejected %s", address)
		}
	}
}
