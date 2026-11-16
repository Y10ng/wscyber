package modelprovider

import "testing"

func TestEnvelopeExtraBillingSignals(t *testing.T) {
	for _, c := range []struct{ protocol, raw string }{
		{"openai_chat", `{"usage":{"prompt_tokens":2,"completion_tokens":1},"service_tier":"priority"}`},
		{"openai_chat", `{"usage":{"prompt_tokens":2,"completion_tokens":1},"choices":[{"message":{"audio":{"id":"audio"}}}]}`},
		{"anthropic_messages", `{"usage":{"input_tokens":2,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"content":[{"type":"server_tool_use"}]}`},
		{"anthropic_messages", `{"usage":{"input_tokens":2,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0},"container":{"id":"fixture"}}`},
	} {
		u, extra := ParseUsage(c.protocol, []byte(c.raw))
		if !extra || u.Status != "reported" {
			t.Fatal(u, extra)
		}
	}
	for _, raw := range []string{`{"usage":{"prompt_tokens":2,"total_tokens":1}}`, `{"usage":{"prompt_tokens":2,"prompt_tokens_details":{"audio_tokens":3}}}`} {
		u, _ := ParseUsage("openai_chat", []byte(raw))
		if u.Status != "invalid" {
			t.Fatal(u)
		}
	}
}
