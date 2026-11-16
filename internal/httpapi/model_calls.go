package httpapi

import (
	"net/http"
	"net/url"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelledger"
)

func (s *server) ledgerRepo(w http.ResponseWriter) (domain.ModelCallRepository, bool) {
	repo, ok := s.repo.(domain.ModelCallRepository)
	if !ok {
		fail(w, domain.ErrUnavailable)
	}
	return repo, ok
}
func ledgerQuery(r *http.Request) (url.Values, error) { return url.ParseQuery(r.URL.RawQuery) }
func (s *server) listModelCalls(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.ledgerRepo(w)
	if !ok {
		return
	}
	q, err := ledgerQuery(r)
	if err != nil {
		fail(w, domain.ErrInvalid)
		return
	}
	f, err := modelledger.ParseFilter(q, "", false, time.Now())
	if err != nil {
		fail(w, err)
		return
	}
	v, err := repo.ListModelCalls(r.Context(), f)
	respond(w, v, err)
}
func callIdentity(r *http.Request) (string, string, error) {
	id := r.PathValue("id")
	if !modelconfig.ValidID(id) {
		return "", "", domain.ErrInvalid
	}
	q, err := ledgerQuery(r)
	if err != nil {
		return "", "", domain.ErrInvalid
	}
	for k, v := range q {
		if k != "mission_id" || len(v) != 1 || len(v[0]) == 0 || len(v[0]) > 128 {
			return "", "", domain.ErrInvalid
		}
	}
	return id, q.Get("mission_id"), nil
}
func (s *server) getModelCall(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.ledgerRepo(w)
	if !ok {
		return
	}
	id, mission, err := callIdentity(r)
	if err != nil {
		fail(w, err)
		return
	}
	v, err := repo.GetModelCall(r.Context(), id, mission)
	respond(w, v, err)
}
func (s *server) getModelCallContext(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.repo.(domain.ResearchContextRepository)
	if !ok {
		fail(w, domain.ErrUnavailable)
		return
	}
	id, mission, err := callIdentity(r)
	if err != nil {
		fail(w, err)
		return
	}
	v, err := repo.GetModelCallContext(r.Context(), id, mission)
	respond(w, v, err)
}
func (s *server) getModelCallContent(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Ledger == nil {
		fail(w, domain.ErrUnavailable)
		return
	}
	id, mission, err := callIdentity(r)
	if err != nil {
		fail(w, err)
		return
	}
	v, err := s.cfg.Ledger.Content(r.Context(), id, mission)
	respond(w, v, err)
}
func (s *server) getModelUsage(w http.ResponseWriter, r *http.Request) {
	repo, ok := s.ledgerRepo(w)
	if !ok {
		return
	}
	q, err := ledgerQuery(r)
	if err != nil {
		fail(w, domain.ErrInvalid)
		return
	}
	f, err := modelledger.ParseFilter(q, r.PathValue("id"), true, time.Now())
	if err != nil {
		fail(w, err)
		return
	}
	v, err := repo.ModelUsage(r.Context(), f)
	respond(w, v, err)
}
func (s *server) getModelRecording(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	repo, ok := s.ledgerRepo(w)
	if !ok {
		return
	}
	v, err := repo.GetModelRecording(r.Context(), r.PathValue("id"))
	respond(w, v, err)
}
func (s *server) listModelPrices(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	repo, ok := s.ledgerRepo(w)
	if !ok {
		return
	}
	v, err := repo.ListModelPrices(r.Context())
	if v == nil {
		v = []domain.ModelPrice{}
	}
	respond(w, map[string]any{"items": v}, err)
}
func (s *server) saveModelPrice(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	if s.cfg.Ledger == nil {
		fail(w, domain.ErrUnavailable)
		return
	}
	raw, err := readModelInput(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	in, err := modelledger.DecodePrice(raw)
	if err != nil {
		fail(w, err)
		return
	}
	v, created, err := s.cfg.Ledger.SavePrice(r.Context(), r.PathValue("id"), in)
	if err != nil {
		fail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, v)
}
