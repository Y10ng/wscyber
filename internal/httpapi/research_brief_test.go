package httpapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

func TestStrictResearchBriefJSON(t *testing.T) {
	decode := func(brief string) error {
		body := strings.TrimSuffix(explorationBody, "}") + `,"research_brief":` + brief + `}`
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var in domain.ExplorationInput
		return decodeExploration(httptest.NewRecorder(), r, &in)
	}
	for _, b := range []string{`{"business_rules":"a\r\nb\t😀","share_with_model":false}`, `{"identities":[{"label":"A","role":"member"}],"share_with_model":true}`} {
		if err := decode(b); err != nil {
			t.Fatal(err)
		}
	}
	for _, b := range []string{
		`null`, `{}`, `[]`, `{"business_rules":"a"}`, `{"business_rules":"a","share_with_model":null}`, `{"business_rules":"a","share_with_model":"false"}`, `{"business_rules":"a","share_with_model":false,"share_with_model":true}`, `{"business_rules":"a","share_with_model":false,"token":"private"}`, `{"business_rules":"a","Share_with_model":false}`, `{"business_rules":"a","share_with_model":false,"identities":null}`, `{"business_rules":null,"share_with_model":false}`,
		`{"identities":[{"label":"a","role":"member","label":"b"}],"share_with_model":false}`,
		`{"identities":[{"label":"a","role":"member","password":"private"}],"share_with_model":false}`,
		`{"identities":[{"label":"a","role":null}],"share_with_model":false}`,
		`{"business_rules":"\ud83d","share_with_model":false}`, `{"business_rules":"\ude00","share_with_model":false}`, `{"business_rules":"\u0000","share_with_model":false}`,
	} {
		if err := decode(b); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("严格解析未拒绝", err)
		}
	}
}
func TestResearchBriefStartCannotFallBack(t *testing.T) {
	body := strings.TrimSuffix(explorationBody, "}") + `,"research_brief":{"business_rules":"BRIEF_SENTINEL","share_with_model":false}}`
	old := &explorationRepo{}
	if w := explorationRequest(New(old, nil, Config{Token: testToken}), body, operationKey); w.Code != 503 || old.calls != 0 {
		t.Fatal("旧仓储不得丢弃说明", w.Code)
	}
	r := &materialStartRepo{explorationRepo: &explorationRepo{}}
	w := explorationRequest(New(r, nil, Config{Token: testToken}), body, operationKey)
	if w.Code != 202 || r.input.ResearchBrief == nil || r.input.ResearchBrief.Shared() || strings.Contains(w.Body.String(), "BRIEF_SENTINEL") {
		t.Fatal("分享意图/回执边界", w.Code)
	}
}
