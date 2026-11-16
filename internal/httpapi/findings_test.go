package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

type findingHTTPRepo struct {
	domain.Repository
	domain.FindingRepository
	calls int
	err   error
}

func (r *findingHTTPRepo) ListFindings(_ context.Context, m string, q domain.FindingQuery) (domain.FindingsPage, error) {
	r.calls++
	return domain.FindingsPage{MissionID: m, Items: []domain.FindingSummary{}}, r.err
}
func (r *findingHTTPRepo) GetFinding(_ context.Context, m, f, id string) (domain.FindingRevision, error) {
	r.calls++
	return domain.FindingRevision{ID: id, FindingID: f, MissionID: m}, r.err
}
func (r *findingHTTPRepo) GetFindingSummary(_ context.Context, m, f string) (domain.FindingSummary, error) {
	r.calls++
	return domain.FindingSummary{ID: f, MissionID: m}, r.err
}
func (r *findingHTTPRepo) FindingHistory(_ context.Context, m, f string, q domain.FindingQuery) (domain.FindingHistoryPage, error) {
	r.calls++
	return domain.FindingHistoryPage{MissionID: m, FindingID: f, Items: []domain.FindingHistoryRow{}}, r.err
}
func (r *findingHTTPRepo) FindingEvidenceContent(_ context.Context, m, f, id string, index int) (domain.FindingEvidenceContent, error) {
	r.calls++
	return domain.FindingEvidenceContent{MissionID: m, FindingID: f, RevisionID: id, Index: index}, r.err
}
func TestFindingReadRoutesAuthenticationBoundsAndNoWrites(t *testing.T) {
	repo := &findingHTTPRepo{}
	h := New(repo, nil, Config{Token: testToken})
	id := "11111111-1111-4111-8111-111111111111"
	base := "/api/missions/" + id + "/findings"
	for _, suffix := range []string{"", "/" + id, "/" + id + "/summary", "/" + id + "/history", "/" + id + "/revisions/" + id, "/" + id + "/revisions/" + id + "/evidence/0"} {
		t.Run(suffix, func(t *testing.T) {
			before := repo.calls
			w := request(h, "GET", base+suffix, "", "")
			if w.Code != 401 || repo.calls != before {
				t.Fatal("auth", w.Code)
			}
			w = request(h, "GET", base+suffix, "", testToken)
			if w.Code != 200 || repo.calls != before+1 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, q := range []string{"limit=51", "limit=0", "limit=01", "limit=%2B1", "limit=1&limit=2", "cursor=a&cursor=b", "unknown=1", "q=%ZZ", "cursor=" + strings.Repeat("x", 1025)} {
				before = repo.calls
				w = request(h, "GET", base+suffix+"?"+q, "", testToken)
				if w.Code != 400 || repo.calls != before {
					t.Fatal(q, w.Code)
				}
			}
			before = repo.calls
			w = request(h, "GET", base+suffix, `{"unused":"body"}`, testToken)
			if w.Code != 400 || repo.calls != before {
				t.Fatal("GET body")
			}
			repo.err = domain.ErrNotFound
			w = request(h, "GET", base+suffix, "", testToken)
			repo.err = nil
			if w.Code != 404 {
				t.Fatal("owner miss", w.Code)
			}
			req := httptest.NewRequest("GET", base+suffix, nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("Origin", "https://foreign.invalid")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 403 {
				t.Fatal("origin", rec.Code)
			}
		})
	}
	for _, index := range []string{"-1", "16", "01", "x"} {
		before := repo.calls
		w := request(h, "GET", base+"/"+id+"/revisions/"+id+"/evidence/"+index, "", testToken)
		if w.Code != 400 || repo.calls != before {
			t.Fatal("index", index, w.Code)
		}
	}
	w := request(h, "POST", base, `{}`, testToken)
	if w.Code != 404 && w.Code != 405 {
		t.Fatal("unexpected write endpoint", w.Code)
	}
	rec := httptest.NewRecorder()
	respondFinding(rec, map[string]string{"body": strings.Repeat("x", domain.MaxFindingEvidenceBytes)}, nil)
	if rec.Code != 503 {
		t.Fatal("response byte bound")
	}
}
