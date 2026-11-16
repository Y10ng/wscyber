package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

type researchControlsStub struct {
	domain.Repository
	domain.ResearchControlRepository
	calls int
}

func (r *researchControlsStub) ControlResearch(_ context.Context, key, m, t, c string, _ func(context.Context) error) (domain.ResearchWriteReceipt, error) {
	r.calls++
	if c != "pause" && c != "run" && c != "resume" {
		return domain.ResearchWriteReceipt{}, domain.ErrInvalid
	}
	return domain.ResearchWriteReceipt{Key: key, MissionID: m, ThreadID: t, Command: c}, nil
}
func (r *researchControlsStub) AddSupplement(_ context.Context, key, m, t string, in domain.SupplementInput) (domain.ResearchWriteReceipt, error) {
	if err := domain.ValidateSupplement(in); err != nil {
		return domain.ResearchWriteReceipt{}, err
	}
	r.calls++
	return domain.ResearchWriteReceipt{Key: key, MissionID: m, ThreadID: t, Command: "supplement"}, nil
}
func TestC2BStrictHTTP(t *testing.T) {
	repo := &researchControlsStub{}
	h := New(repo, nil, Config{Token: testToken})
	base := "/api/missions/11111111-1111-4111-8111-111111111111/threads/22222222-2222-4222-8222-222222222222/"
	do := func(path, body, key, token string) int {
		r := httptest.NewRequest("POST", base+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store")
		}
		return w.Code
	}
	if got := do("control", `{"command":"pause"}`, operationKey, testToken); got != 200 {
		t.Fatal(got)
	}
	for _, body := range []string{`{"command":"pause","command":"run"}`, `{"Command":"pause"}`, `{"command":null}`, `{"command":"pause","extra":true}`, `{"command":"\ud800"}`} {
		if got := do("control", body, operationKey, testToken); got != 400 {
			t.Fatal(got, body)
		}
	}
	if got := do("control", `{"command":"pause"}`, "", testToken); got != 400 {
		t.Fatal(got)
	}
	if got := do("control?x=1", `{"command":"pause"}`, operationKey, testToken); got != 400 {
		t.Fatal(got)
	}
	if got := do("control", `{"command":"pause"}`, operationKey, ""); got != 401 {
		t.Fatal(got)
	}
	if got := do("supplements", `{"source":"operator","content":"text","share_with_model":true}`, operationKey, testToken); got != 201 {
		t.Fatal(got)
	}
	if got := do("supplements", `{"source":"operator","content":"text","share_with_model":false}`, operationKey, testToken); got != 400 {
		t.Fatal(got)
	}
	if repo.calls != 2 {
		t.Fatal(repo.calls)
	}
}
