package modelprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wscyber/internal/domain"
)

func TestUsageProtocolBuckets(t *testing.T) {
	cases := []struct{ protocol, raw, input, output, total, cache, creation, five, hour, reason string }{
		{"openai_chat", `{"usage":{"prompt_tokens":120,"completion_tokens":20,"total_tokens":140,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":5}}}`, "120", "20", "140", "40", "", "", "", "5"},
		{"anthropic_messages", `{"usage":{"input_tokens":100,"output_tokens":30,"cache_read_input_tokens":40,"cache_creation_input_tokens":20,"cache_creation":{"ephemeral_5m_input_tokens":12,"ephemeral_1h_input_tokens":8}}}`, "160", "30", "190", "40", "20", "12", "8", ""},
	}
	for _, c := range cases {
		t.Run(c.protocol, func(t *testing.T) {
			u, extra := ParseUsage(c.protocol, []byte(c.raw))
			if extra || u.Status != "reported" {
				t.Fatal(u, extra)
			}
			for i, p := range []*string{u.InputTokens, u.OutputTokens, u.TotalTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens, u.CacheCreation5mInputTokens, u.CacheCreation1hInputTokens, u.ReasoningOutputTokens} {
				want := []string{c.input, c.output, c.total, c.cache, c.creation, c.five, c.hour, c.reason}[i]
				if want == "" {
					if p != nil {
						t.Fatal("不适用不能伪造零", u)
					}
				} else if p == nil || *p != want {
					t.Fatal(u, want)
				}
			}
		})
	}
}
func TestUsageInvalidNeverFabricatesZero(t *testing.T) {
	for _, raw := range []string{
		`{"usage":{"prompt_tokens":1,"prompt_tokens":2}}`, `{"usage":{"prompt_tokens":"1"}}`, `{"usage":{"prompt_tokens":-1}}`, `{"usage":{"prompt_tokens":null}}`, `{"usage":{"prompt_tokens":true}}`, `{"usage":{"prompt_tokens":1.5}}`, `{"usage":{"prompt_tokens":1e3}}`, `{"usage":{"prompt_tokens":1000000000001}}`, `{"usage":{"prompt_tokens":9223372036854775808}}`, `{"usage":{"prompt_tokens":1000000000000,"completion_tokens":1}}`, `{"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":9}}`, `{"usage":{"prompt_tokens":2,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":3}}}`, `{"usage":{"prompt_tokens":2,"completion_tokens":1,"completion_tokens_details":{"reasoning_tokens":2}}}`, `{"usage":[]}`, `{"usage":{"prompt_tokens_details":null}}`, `{"usage":{"completion_tokens_details":{"reasoning_tokens":"1"}}}`, `{"usage":{"prompt_tokens":1}`, `{"usage":{},"usage":{}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			u, _ := ParseUsage("openai_chat", []byte(raw))
			if u.Status != "invalid" || u.InputTokens != nil || u.TotalTokens != nil || len(u.ProviderUsage) != 0 {
				t.Fatal(u)
			}
		})
	}
	for _, raw := range []string{
		`{"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":1,"cache_creation_input_tokens":3,"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":2}}}`,
		`{"usage":{"input_tokens":999999999999,"cache_read_input_tokens":1,"cache_creation_input_tokens":1}}`,
	} {
		u, _ := ParseUsage("anthropic_messages", []byte(raw))
		if u.Status != "invalid" {
			t.Fatal(u)
		}
	}
}
func TestUsageMissingPartialAndExtraBilling(t *testing.T) {
	for _, raw := range []string{`{}`, `{"usage":null}`} {
		u, _ := ParseUsage("openai_chat", []byte(raw))
		if u.Status != "missing" {
			t.Fatal(u)
		}
	}
	u, _ := ParseUsage("openai_chat", []byte(`{"usage":{"prompt_tokens":7}}`))
	if u.Status != "partial" || u.InputTokens == nil || *u.InputTokens != "7" || u.OutputTokens != nil || u.TotalTokens != nil {
		t.Fatal(u)
	}
	u, _ = ParseUsage("anthropic_messages", []byte(`{"usage":{"input_tokens":7,"output_tokens":2}}`))
	if u.Status != "partial" || u.InputTokens != nil || u.TotalTokens != nil || u.OutputTokens == nil {
		t.Fatal(u)
	}
	for _, raw := range []string{`{"usage":{"prompt_tokens":2,"completion_tokens":1,"prompt_tokens_details":{"audio_tokens":1}}}`, `{"usage":{"prompt_tokens":2,"completion_tokens":1,"custom_billing":1}}`} {
		u, extra := ParseUsage("openai_chat", []byte(raw))
		if !extra || u.Status != "reported" {
			t.Fatal(u, extra)
		}
	}
	u, extra := ParseUsage("anthropic_messages", []byte(`{"usage":{"input_tokens":2,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"server_tool_use":{"web_search_requests":1}}}`))
	if !extra || u.Status != "reported" {
		t.Fatal(u, extra)
	}
}

func TestUsageCapturedIndependentlyOfOutcome(t *testing.T) {
	for _, status := range []int{200, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"private-response"},"usage":{"prompt_tokens":120,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40}}}`)
			}))
			defer server.Close()
			doc := domain.ModelDocument{ModelConnection: domain.ModelConnection{Name: "fixture", Protocol: "openai_chat", BaseURL: server.URL, Model: "fake", Auth: "none", TimeoutSeconds: 2, MaxOutputTokens: 32}}
			r := Complete(context.Background(), doc, Input{User: "fixed", MaxOutputTokens: 32})
			if r.Outcome == ValidResponse || r.Usage.TotalTokens == nil || *r.Usage.TotalTokens != "140" || !r.ResponseComplete || !strings.Contains(string(r.Response), "private-response") {
				t.Fatal(r)
			}
		})
	}
}
