package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"wscyber/internal/domain"
)

func TestStrictMaterialJSON(t *testing.T) {
	id := strings.Repeat("a", 32)
	good := `{"description":"a","goal":"b","allowed_origins":["https://fixture.invalid"],"materials":[{"id":"` + id + `","token":"` + id + `"}],"safety_prompt_enabled":true,"safety_prompt":"不要写入😀"}`
	decode := func(body string) error {
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		var in domain.ExplorationInput
		return decodeExploration(httptest.NewRecorder(), req, &in)
	}
	if err := decode(good); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		strings.Replace(good, `"token":`, `"id":"`+id+`","token":`, 1), strings.Replace(good, `"token":`, `"Token":`, 1), strings.Replace(good, `"materials":[`, `"materials":null,"x":[`, 1), strings.Replace(good, `true`, `null`, 1), strings.Replace(good, `true`, `"true"`, 1),
		strings.Replace(good, `😀`, `\ud83d`, 1), strings.Replace(good, `😀`, `\ude00`, 1), strings.Replace(good, `😀`, string([]byte{255}), 1), good + " {}", `[]`, `null`, strings.Replace(good, `["https://fixture.invalid"]`, `[null]`, 1), strings.Replace(good, `"goal":"b"`, `"goal":"b","goal":"c"`, 1),
	} {
		if !errors.Is(decode(body), domain.ErrInvalid) {
			t.Fatal("严格解析未拒绝无效输入")
		}
	}
}
func TestLibraryQueryStrict(t *testing.T) {
	for _, raw := range []string{"limit=0", "limit=101", "all=1", "all=true&all=false", "unknown=a", "q=%FF%ZZ", "q=a;b=c"} {
		r := httptest.NewRequest("GET", "/api/library/items?"+raw, nil)
		if _, err := parseLibraryQuery(r); err == nil {
			t.Fatal(raw)
		}
	}
	q, err := parseLibraryQuery(httptest.NewRequest("GET", "/api/library/items?all=true&kind=file&limit=100", nil))
	if err != nil || !q.All || q.Limit != 100 {
		t.Fatal(q, err)
	}
}

type materialStartRepo struct {
	*explorationRepo
	domain.MissionModelRepository
}

func (r *materialStartRepo) StartExplorationWithModel(ctx context.Context, key string, in domain.ExplorationInput, _ domain.ExplorationModelOptions) (domain.ExplorationStart, error) {
	return r.StartExploration(ctx, key, in, true)
}
func TestMaterialStartAndStaleCode(t *testing.T) {
	old := &explorationRepo{}
	body := strings.TrimSuffix(explorationBody, "}") + `,"safety_prompt_enabled":true,"safety_prompt":"禁止写入"}`
	if w := explorationRequest(New(old, nil, Config{Token: testToken}), body, operationKey); w.Code != 503 || old.calls != 0 {
		t.Fatal("旧仓储不得静默丢弃新配置", w.Code)
	}
	r := &materialStartRepo{explorationRepo: &explorationRepo{}}
	h := New(r, nil, Config{Token: testToken})
	w := explorationRequest(h, body, operationKey)
	if w.Code != 202 || !r.input.SafetyPromptEnabled {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, err := range []error{domain.ErrMaterialSelectionStale, domain.ErrConflict, domain.ErrUnavailable} {
		r.err = err
		w = explorationRequest(h, body, operationKey)
		var result map[string]string
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil {
			t.Fatal(e)
		}
		if (result["code"] == "material_selection_stale") != errors.Is(err, domain.ErrMaterialSelectionStale) {
			t.Fatal(result)
		}
	}
}
