package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"wscyber/internal/domain"
)

func (s *server) skillRepo(w http.ResponseWriter) domain.SkillRepository {
	repo, ok := s.repo.(domain.SkillRepository)
	if !ok {
		fail(w, domain.ErrUnavailable)
		return nil
	}
	return repo
}
func parseSkillPage(r *http.Request) (string, int, error) {
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "", 0, domain.ErrInvalid
	}
	for k, a := range v {
		if len(a) != 1 || (k != "after" && k != "limit") {
			return "", 0, domain.ErrInvalid
		}
	}
	limit := 25
	if v.Has("limit") {
		limit, err = strconv.Atoi(v.Get("limit"))
		if err != nil || limit < 1 || limit > 100 {
			return "", 0, domain.ErrInvalid
		}
	}
	return v.Get("after"), limit, nil
}
func (s *server) skillUsage(w http.ResponseWriter, r *http.Request) {
	after, limit, err := parseSkillPage(r)
	if err != nil {
		fail(w, err)
		return
	}
	repo := s.skillRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.SkillUsage(r.Context(), r.PathValue("id"), after, limit)
	respond(w, out, err)
}
func (s *server) skillActivities(w http.ResponseWriter, r *http.Request) {
	after, limit, err := parseSkillPage(r)
	if err != nil {
		fail(w, err)
		return
	}
	repo := s.skillRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.ListSkillActivities(r.Context(), r.PathValue("id"), after, limit)
	respond(w, out, err)
}
func (s *server) skillLoad(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	repo := s.skillRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.GetSkillLoad(r.Context(), r.PathValue("id"), r.PathValue("loadID"))
	respond(w, out, err)
}
func (s *server) skillCalls(w http.ResponseWriter, r *http.Request) {
	after, limit, err := parseSkillPage(r)
	if err != nil {
		fail(w, err)
		return
	}
	repo := s.skillRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.SkillModelCalls(r.Context(), r.PathValue("id"), r.PathValue("loadID"), after, limit)
	respond(w, out, err)
}
