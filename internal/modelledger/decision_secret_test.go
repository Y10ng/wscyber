package modelledger

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelprovider"
)

func TestLedgerRejectsCredentialEchoInsteadOfChangingDecision(t *testing.T) {
	const key = "fixture-provider-key"
	bs := string(rune(92))
	for _, decision := range []string{
		`{"kind":"conclude","summary":"fixture-provider-key"}`,
		`{"kind":"action","summary":"test","next_step":"await","action":{"kind":"http","method":"GET","url":"http://127.0.0.1:9/?token=fixture-provider-key","reason":"test"}}`,
		`{"kind":"action","summary":"test","next_step":"await","action":{"kind":"http","method":"POST","url":"http://127.0.0.1:9/","body":"fixture-provider-key","reason":"test"}}`,
		`{"kind":"conclude","summary":"fixture-` + bs + `u0070rovider-key"}`,
	} {
		t.Run(decision, func(t *testing.T) {
			if !json.Valid([]byte(decision)) {
				t.Fatal("测试必须为合法Decision JSON")
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"object": "chat.completion", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": decision}}}, "usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1}})
			}))
			defer provider.Close()
			vault, e := modelconfig.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
			if e != nil {
				t.Fatal(e)
			}
			repo := &callRepo{recording: true}
			service := New(repo, vault, nil)
			doc := domain.ModelDocument{ModelConnection: domain.ModelConnection{Name: "fixture", Protocol: "openai_chat", BaseURL: provider.URL, Model: "fake", Auth: "bearer", TimeoutSeconds: 2, MaxOutputTokens: 128}, Secrets: domain.ModelSecrets{APIKey: key}}
			validated := false
			r, e := service.Complete(context.Background(), Identity{Source: "environment", MissionID: "mission", ThreadID: "thread", AttemptID: "attempt"}, doc, modelprovider.Input{User: "business input", MaxOutputTokens: 128}, func(string) bool { validated = true; return true })
			if e != nil || r.Outcome != "invalid_response" || r.Text != "" || r.Usage.TotalTokens == nil || *r.Usage.TotalTokens != "3" || validated {
				t.Fatal(r, e, validated)
			}
			if repo.finish.Outcome != "invalid_response" || repo.finish.Response == nil || !repo.finish.Response.Redacted {
				t.Fatal(repo.finish)
			}
			raw, e := vault.OpenContent(repo.begin.Call.ID, "response", repo.finish.Response.Ciphertext)
			if e != nil || bytes.Contains(raw, []byte(key)) {
				t.Fatal(e, string(raw))
			}
			var decoded struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if e = json.Unmarshal(raw, &decoded); e != nil || len(decoded.Choices) != 1 || decoded.Choices[0].Message.Content != "" {
				t.Fatal("再次转义的被拒绝Decision也不得归档回显", e, string(raw))
			}
		})
	}
}
func TestArchiveRedactsJSONWrappedSpecialCredential(t *testing.T) {
	key := "fixture-" + string(rune(92)) + `"secret`
	snapshot, _ := json.Marshal(map[string]string{"material": key})
	envelope, _ := json.Marshal(map[string]string{"user": string(snapshot)})
	got, changed := redact(envelope, domain.ModelSecrets{APIKey: key})
	if !changed || strings.Contains(string(got), "secret") {
		t.Fatal(string(got), changed)
	}
}
