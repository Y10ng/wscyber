package modelprovider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInputLimitsAndCancelledContextNeverSend(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				data, err := io.ReadAll(r.Body)
				if err != nil || !json.Valid(data) || len(data) > maxRequestBytes {
					t.Error("invalid bounded request")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, success(protocol))
			}))
			defer srv.Close()
			doc := fixtureDoc(srv.URL, protocol, "none")
			for _, input := range []Input{
				{User: "x", MaxOutputTokens: 0}, {User: "x", MaxOutputTokens: 65},
				{User: "  \n ", MaxOutputTokens: 32}, {User: string([]byte{0xff}), MaxOutputTokens: 32},
				{System: string([]byte{0xff}), User: "x", MaxOutputTokens: 32},
				{System: "s", User: strings.Repeat("x", maxInputBytes), MaxOutputTokens: 32},
			} {
				got := Complete(context.Background(), doc, input)
				if got.Outcome != NotSent || got.Attempted || got.HTTPStatus != 0 || got.Text != "" {
					t.Fatal("invalid input was attempted")
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if got := Complete(ctx, doc, probe()); got.Outcome != NotSent || got.Attempted {
				t.Fatal("pre-cancelled call attempted")
			}
			if hits.Load() != 0 {
				t.Fatal("preflight sent a request")
			}
			// 输入字节限制在编码前；JSON控制字符转义的线长度也必须有界。
			for _, text := range []string{strings.Repeat("x", maxInputBytes), strings.Repeat("\x01", maxInputBytes)} {
				got := Complete(context.Background(), doc, Input{User: text, MaxOutputTokens: 32})
				if got.Outcome != ValidResponse {
					t.Fatalf("exact context limit rejected: %s", got.Outcome)
				}
			}
			if hits.Load() != 2 {
				t.Fatalf("expected 2 valid calls, got %d", hits.Load())
			}
		})
	}
}

func TestConfiguredExtensionsCannotOverrideTrustedFields(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		for _, key := range []string{"model", "messages", "system", "tools", "tool_choice", "stream", "max_tokens", "max_completion_tokens", "max_output_tokens", "functions", "function_call", "n", "mcp_servers", "container", "messages.0.content", "messages[0]", "extra_headers", "extra_body"} {
			t.Run(protocol+"/parameter/"+key, func(t *testing.T) {
				doc := fixtureDoc(srv.URL, protocol, "bearer")
				doc.Secrets.Parameters[key] = json.RawMessage(`"override"`)
				if got := Complete(context.Background(), doc, probe()); got.Outcome != NotSent || got.Attempted {
					t.Fatal("core parameter was accepted")
				}
			})
		}
		for _, key := range []string{"Authorization", "X-Api-Key", "hOsT", "Content-Type", "Cookie", "Proxy-Authorization", "Connection", "X-Stainless-Retry-Count", "Anthropic-Version"} {
			t.Run(protocol+"/header/"+key, func(t *testing.T) {
				doc := fixtureDoc(srv.URL, protocol, "bearer")
				doc.Secrets.Headers[key] = "override"
				if got := Complete(context.Background(), doc, probe()); got.Outcome != NotSent || got.Attempted {
					t.Fatal("core header was accepted")
				}
			})
		}
	}
	if hits.Load() != 0 {
		t.Fatal("reserved extension sent request")
	}
}

func TestHTTPErrorDoesNotReadOrExposeRemoteBody(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", "999999999")
				w.Header().Set("X-Should-Retry", "true")
				w.WriteHeader(http.StatusUnauthorized)
				io.WriteString(w, `{"error":{"message":"fixture-secret-body"}}`)
				w.(http.Flusher).Flush()
				<-release
			}))
			defer srv.Close()
			defer close(release)
			got := Complete(context.Background(), fixtureDoc(srv.URL, protocol, "bearer"), probe())
			encoded, err := json.Marshal(got)
			if err != nil || got.Outcome != HTTPError || got.HTTPStatus != 401 || got.Text != "" || strings.Contains(string(encoded), "fixture-secret") {
				t.Fatal("remote error exposed or body consumed")
			}
		})
	}
}
