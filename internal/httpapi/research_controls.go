package httpapi

import (
	"context"
	"net/http"
	"wscyber/internal/domain"
)

func (s *server) researchControls(w http.ResponseWriter, r *http.Request) domain.ResearchControlRepository {
	if !domain.ValidSkillLoadID(r.PathValue("id")) || !domain.ValidSkillLoadID(r.PathValue("threadID")) {
		fail(w, domain.ErrInvalid)
		return nil
	}
	repo, ok := s.repo.(domain.ResearchControlRepository)
	if !ok {
		fail(w, domain.ErrUnavailable)
		return nil
	}
	return repo
}
func researchKey(r *http.Request) (string, error) {
	keys := r.Header.Values("Idempotency-Key")
	if r.URL.RawQuery != "" || len(keys) != 1 || !domain.ValidOperationKey(keys[0]) {
		return "", domain.ErrInvalid
	}
	return keys[0], nil
}
func (s *server) controlResearch(w http.ResponseWriter, r *http.Request) {
	key, err := researchKey(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Command string `json:"command"`
	}
	if err = decodeStrictData(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	repo := s.researchControls(w, r)
	if repo == nil {
		return
	}
	mission := r.PathValue("id")
	out, err := repo.ControlResearch(r.Context(), key, mission, r.PathValue("threadID"), in.Command, func(ctx context.Context) error {
		if s.cfg.MissionModels != nil {
			return s.cfg.MissionModels.ValidateMissionModel(ctx, mission)
		}
		return domain.ErrUnavailable
	})
	respond(w, out, err)
}
func (s *server) addSupplement(w http.ResponseWriter, r *http.Request) {
	key, err := researchKey(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in domain.SupplementInput
	if err = decodeStrictData(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	repo := s.researchControls(w, r)
	if repo == nil {
		return
	}
	out, err := repo.AddSupplement(r.Context(), key, r.PathValue("id"), r.PathValue("threadID"), in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *server) supplements(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	repo := s.researchControls(w, r)
	if repo == nil {
		return
	}
	out, err := repo.ListSupplements(r.Context(), r.PathValue("id"), r.PathValue("threadID"))
	respond(w, out, err)
}
func (s *server) supplement(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || !domain.ValidSkillLoadID(r.PathValue("supplementID")) {
		fail(w, domain.ErrInvalid)
		return
	}
	repo := s.researchControls(w, r)
	if repo == nil {
		return
	}
	out, err := repo.GetSupplement(r.Context(), r.PathValue("id"), r.PathValue("threadID"), r.PathValue("supplementID"))
	respond(w, out, err)
}
func (s *server) supplementCalls(w http.ResponseWriter, r *http.Request) {
	after, limit, err := parseSkillPage(r)
	if err != nil || !domain.ValidSkillLoadID(r.PathValue("supplementID")) {
		fail(w, domain.ErrInvalid)
		return
	}
	repo := s.researchControls(w, r)
	if repo == nil {
		return
	}
	out, err := repo.SupplementModelCalls(r.Context(), r.PathValue("id"), r.PathValue("threadID"), r.PathValue("supplementID"), after, limit)
	respond(w, out, err)
}
