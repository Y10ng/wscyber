package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"wscyber/internal/domain"
)

func TestExplorationRecordingStrictAndLegacyFingerprint(t *testing.T) {
	base := `{"description":"fixture","goal":"end","allowed_origins":["http://127.0.0.1"]`
	decode := func(raw string) (domain.ExplorationInput, error) {
		r := httptest.NewRequest("POST", "/api/explorations", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		var input domain.ExplorationInput
		e := decodeExploration(httptest.NewRecorder(), r, &input)
		return input, e
	}
	omitted, e := decode(base + `}`)
	if e != nil {
		t.Fatal(e)
	}
	disabled, e := decode(base + `,"record_model_content":false}`)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(omitted)
	b, _ := json.Marshal(disabled)
	if string(a) != string(b) || strings.Contains(string(a), "record_model_content") {
		t.Fatal("关闭必须保留旧指纹", string(a), string(b))
	}
	enabled, e := decode(base + `,"record_model_content":true}`)
	if e != nil || !enabled.RecordModelContent {
		t.Fatal(enabled, e)
	}
	for _, suffix := range []string{`,"record_model_content":null}`, `,"record_model_content":"false"}`, `,"record_model_content":1}`, `,"record_model_content":true,"record_model_content":false}`, `,"RecordModelContent":true}`, `,"unknown":false}`, `} {}`} {
		if _, e := decode(base + suffix); e == nil {
			t.Fatal(suffix)
		}
	}
}
