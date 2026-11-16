package modelprovider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMalformedResponseFields(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		good := success(protocol)
		cases := map[string]string{
			"duplicate_root":         strings.Replace(good, `"model":"fixture-model"`, `"model":"other","model":"fixture-model"`, 1),
			"invalid_utf8":           strings.Replace(good, "OK", string([]byte{0xff}), 1),
			"unpaired_surrogate":     strings.Replace(good, "OK", `\ud800`, 1),
			"unpaired_low_surrogate": strings.Replace(good, "OK", `\udc00`, 1),
			"missing_role":           strings.Replace(good, `"role":"assistant",`, "", 1),
			"null_role":              strings.Replace(good, `"role":"assistant"`, `"role":null`, 1),
			"wrong_role":             strings.Replace(good, `"role":"assistant"`, `"role":"user"`, 1),
			"blank_text":             strings.Replace(good, "OK", "  ", 1),
			"large_text":             strings.Replace(good, "OK", strings.Repeat("x", maxTextBytes+1), 1),
			"large_utf8_text":        strings.Replace(good, "OK", strings.Repeat("字", maxTextBytes/3+1), 1),
			"deep_json":              strings.Replace(good, `"id":`, `"extra":`+strings.Repeat("[", 65)+"0"+strings.Repeat("]", 65)+`,"id":`, 1),
			"trailing_json":          good + `{}`,
		}
		if protocol == "openai_chat" {
			for name, replacement := range map[string]string{
				"number_text":    `"content":42`,
				"null_text":      `"content":null`,
				"duplicate_text": `"content":"wrong","content":"OK"`, "invalid_refusal": `"content":"OK","refusal":{}`,
				"invalid_tool_calls": `"content":"OK","tool_calls":"not-an-array"`,
				"empty_function":     `"content":"OK","function_call":{}`,
				"invalid_function":   `"content":"OK","function_call":[]`,
				"audio":              `"content":"OK","audio":{"id":"audio"}`,
			} {
				cases[name] = strings.Replace(good, `"content":"OK"`, replacement, 1)
			}
			cases["missing_finish"] = strings.Replace(good, `,"finish_reason":"stop"`, "", 1)
			cases["wrong_object"] = strings.Replace(good, `"chat.completion"`, `"chat.completion.chunk"`, 1)
			cases["missing_object"] = strings.Replace(good, `"object":"chat.completion",`, "", 1)
			cases["invalid_choice_index"] = strings.Replace(good, `"index":0`, `"index":1`, 1)
		} else {
			cases["missing_type"] = strings.Replace(good, `"type":"message",`, "", 1)
			cases["null_type"] = strings.Replace(good, `"type":"message"`, `"type":null`, 1)
			cases["duplicate_text"] = strings.Replace(good, `"text":"OK"`, `"text":"wrong","text":"OK"`, 1)
			cases["number_text"] = strings.Replace(good, `"text":"OK"`, `"text":42`, 1)
			cases["mixed_missing_text"] = strings.Replace(good, `"content":[`, `"content":[{"type":"text"},`, 1)
			cases["mixed_null_block"] = strings.Replace(good, `"content":[`, `"content":[null,`, 1)
			cases["malformed_thinking"] = strings.Replace(good, `"content":[`, `"content":[{"type":"thinking"},`, 1)
			cases["container"] = strings.Replace(good, `"content":`, `"container":{"id":"container"},"content":`, 1)
			cases["pause"] = strings.Replace(good, `"end_turn"`, `"pause_turn"`, 1)
			cases["missing_finish"] = strings.Replace(good, `"stop_reason":"end_turn",`, "", 1)
			for _, kind := range []string{"server_tool_use", "web_search_tool_result", "container_upload", "unknown"} {
				cases[kind] = strings.Replace(good, `"type":"text"`, fmt.Sprintf(`"type":%q`, kind), 1)
			}
		}
		for name, body := range cases {
			t.Run(protocol+"/"+name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, body)
				}))
				defer srv.Close()
				got := Complete(context.Background(), fixtureDoc(srv.URL, protocol, "none"), probe())
				if got.Outcome != InvalidResponse || got.HTTPStatus != 200 || got.Text != "" || !got.Attempted {
					t.Fatalf("malformed response accepted: outcome=%s status=%d attempted=%v", got.Outcome, got.HTTPStatus, got.Attempted)
				}
			})
		}
	}
}

func TestValidResponseVariants(t *testing.T) {
	for _, tc := range []struct{ name, protocol, body, text string }{
		{"chat_empty_optional", "openai_chat", strings.Replace(success("openai_chat"), `"content":"OK"`, `"content":"OK","refusal":null,"tool_calls":[],"function_call":null,"audio":null`, 1), "OK"},
		{"chat_unicode_pair", "openai_chat", strings.Replace(success("openai_chat"), "OK", "\\"+"ud83d"+"\\"+"ude00", 1), "😀"},
		{"messages_multi_text", "anthropic_messages", strings.Replace(success("anthropic_messages"), `"content":[`, `"content":[{"type":"text","text":"first "},`, 1), "first OK"},
		{"messages_thinking", "anthropic_messages", strings.Replace(success("anthropic_messages"), `"content":[`, `"content":[{"type":"thinking","thinking":"not-returned","signature":"sig"},{"type":"redacted_thinking","data":"opaque"},`, 1), "OK"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			got := Complete(context.Background(), fixtureDoc(srv.URL, tc.protocol, "none"), probe())
			if got.Outcome != ValidResponse || got.Text != tc.text {
				t.Fatalf("unexpected outcome=%s", got.Outcome)
			}
		})
	}
}

func TestValidTextBoundariesWithoutInventingUsage(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		for _, text := range []string{"OK", strings.Repeat("x", maxTextBytes), "emoji 😀 / 字 / \\ud800"} {
			t.Run(fmt.Sprintf("%s/%d", protocol, len(text)), func(t *testing.T) {
				body := strings.Split(success(protocol), `,"usage":`)[0] + "}"
				// 引用后的文本保持原字节；合法代理对也应接受，usage缺失不补零。
				body = strings.Replace(body, `"OK"`, fmt.Sprintf("%q", text), 1)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, body)
				}))
				defer srv.Close()
				got := Complete(context.Background(), fixtureDoc(srv.URL, protocol, "none"), probe())
				if got.Outcome != ValidResponse || got.Text != text {
					t.Fatalf("valid text rejected: outcome=%s", got.Outcome)
				}
			})
		}
	}
}
