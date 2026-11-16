package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"wscyber/internal/domain"
)

func (s *server) findingRepo(w http.ResponseWriter, r *http.Request) domain.FindingRepository {
	if r.ContentLength != 0 || len(r.URL.RawQuery) > 2048 {
		fail(w, domain.ErrInvalid)
		return nil
	}
	for _, key := range []string{"id", "findingID", "revisionID"} {
		v := r.PathValue(key)
		if v != "" && !domain.ValidSkillLoadID(v) {
			fail(w, domain.ErrInvalid)
			return nil
		}
	}
	repo, ok := s.repo.(domain.FindingRepository)
	if !ok {
		fail(w, domain.ErrUnavailable)
		return nil
	}
	return repo
}
func parseFindingQuery(r *http.Request, filter bool) (domain.FindingQuery, error) {
	var q domain.FindingQuery
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, domain.ErrInvalid
	}
	for key, v := range values {
		if len(v) != 1 || (key != "limit" && key != "cursor" && !(filter && key == "thread_id")) {
			return q, domain.ErrInvalid
		}
	}
	q.Cursor = values.Get("cursor")
	q.ThreadID = values.Get("thread_id")
	if values.Has("limit") {
		q.Limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil || q.Limit < 1 || strconv.Itoa(q.Limit) != values.Get("limit") {
			return q, domain.ErrInvalid
		}
	}
	return domain.NormalizeFindingQuery(q)
}
func respondFinding(w http.ResponseWriter, value any, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	raw, e := json.Marshal(value)
	if e != nil || len(raw)+1 > domain.MaxFindingEvidenceBytes {
		fail(w, domain.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) listFindings(w http.ResponseWriter, r *http.Request) {
	repo := s.findingRepo(w, r)
	if repo == nil {
		return
	}
	q, err := parseFindingQuery(r, true)
	if err != nil {
		fail(w, err)
		return
	}
	v, err := repo.ListFindings(r.Context(), r.PathValue("id"), q)
	respondFinding(w, v, err)
}
func (s *server) finding(w http.ResponseWriter, r *http.Request) {
	repo := s.findingRepo(w, r)
	if repo == nil {
		return
	}
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	v, err := repo.GetFinding(r.Context(), r.PathValue("id"), r.PathValue("findingID"), r.PathValue("revisionID"))
	respondFinding(w, v, err)
}
func (s *server) findingSummary(w http.ResponseWriter, r *http.Request) {
	repo := s.findingRepo(w, r)
	if repo == nil {
		return
	}
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	v, err := repo.GetFindingSummary(r.Context(), r.PathValue("id"), r.PathValue("findingID"))
	respondFinding(w, v, err)
}
func (s *server) findingHistory(w http.ResponseWriter, r *http.Request) {
	repo := s.findingRepo(w, r)
	if repo == nil {
		return
	}
	q, err := parseFindingQuery(r, false)
	if err != nil {
		fail(w, err)
		return
	}
	v, err := repo.FindingHistory(r.Context(), r.PathValue("id"), r.PathValue("findingID"), q)
	respondFinding(w, v, err)
}
func (s *server) findingEvidence(w http.ResponseWriter, r *http.Request) {
	repo := s.findingRepo(w, r)
	if repo == nil {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if r.URL.RawQuery != "" || err != nil || index < 0 || index >= 16 || strconv.Itoa(index) != r.PathValue("index") {
		fail(w, domain.ErrInvalid)
		return
	}
	v, err := repo.FindingEvidenceContent(r.Context(), r.PathValue("id"), r.PathValue("findingID"), r.PathValue("revisionID"), index)
	respondFinding(w, v, err)
}
