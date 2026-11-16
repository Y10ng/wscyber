package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

type verificationHTTPRepo struct {
	domain.Repository
	domain.VerificationRepository
	calls int
	err   error
}

func (r *verificationHTTPRepo) StartVerification(_ context.Context, k, m, f string, in domain.VerificationInput, _ func(context.Context) error) (domain.VerificationReceipt, error) {
	r.calls++
	return domain.VerificationReceipt{Key: k, MissionID: m, FindingID: f, SourceRevisionID: in.SourceRevisionID, VerificationID: in.SourceRevisionID, StartStatus: "accepted"}, r.err
}
func (r *verificationHTTPRepo) ListVerifications(_ context.Context, m, f string, q domain.FindingQuery) (domain.VerificationPage, error) {
	r.calls++
	return domain.VerificationPage{MissionID: m, FindingID: f, Items: []domain.VerificationSummary{}}, r.err
}
func (r *verificationHTTPRepo) GetVerification(_ context.Context, m, f, id string) (domain.VerificationDetail, error) {
	r.calls++
	return domain.VerificationDetail{}, r.err
}
func (r *verificationHTTPRepo) VerificationEvidence(_ context.Context, m, f, id string, i int) (domain.VerificationEvidenceContent, error) {
	r.calls++
	return domain.VerificationEvidenceContent{}, r.err
}
func TestD2HTTPStrictIntegerSubmitAndReadBoundaries(t *testing.T) {
	r := &verificationHTTPRepo{}
	h := New(r, nil, Config{Token: testToken, System: SystemInfo{MaxSteps: 8}})
	id := "11111111-1111-4111-8111-111111111111"
	base := "/api/missions/" + id + "/findings/" + id + "/verifications"
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", base, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Idempotency-Key", strings.Repeat("a", 32))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	body := `{"source_revision_id":"` + id + `","max_steps":6}`
	w := post(body)
	if w.Code != 202 || r.calls != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("real numeric JSON must submit", w.Code, w.Body.String())
	}
	for _, n := range []string{`"6"`, `6.0`, `6e0`, `null`, `0`, `9`, `-1`, `999999999999999999999`} {
		before := r.calls
		w = post(`{"source_revision_id":"` + id + `","max_steps":` + n + `}`)
		if w.Code != 400 || r.calls != before {
			t.Fatal(n, w.Code)
		}
	}
	for _, bad := range []string{`{"source_revision_id":"` + id + `","max_steps":6,"max_steps":3}`, `{"source_revision_id":"` + id + `","max_steps":6,"unknown":true}`} {
		if w = post(bad); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	for _, suffix := range []string{"", "/" + id, "/" + id + "/evidence/0"} {
		if w = request(h, "GET", base+suffix, "", ""); w.Code != 401 {
			t.Fatal("auth", w.Code)
		}
		if w = request(h, "GET", base+suffix, "", testToken); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code)
		}
		for _, q := range []string{"x=1", "limit=51", "limit=1&limit=2", "thread_id=" + id} {
			if w = request(h, "GET", base+suffix+"?"+q, "", testToken); w.Code != 400 {
				t.Fatal(q, w.Code)
			}
		}
		r.err = domain.ErrNotFound
		if w = request(h, "GET", base+suffix, "", testToken); w.Code != 404 {
			t.Fatal(w.Code)
		}
		r.err = nil
		req := httptest.NewRequest("GET", base+suffix, nil)
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Origin", "https://foreign.invalid")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("origin", w.Code)
		}
	}
	for _, index := range []string{"01", "-1", "16"} {
		if w = request(h, "GET", base+"/"+id+"/evidence/"+index, "", testToken); w.Code != 400 {
			t.Fatal(index, w.Code)
		}
	}
}
