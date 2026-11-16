package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"wscyber/internal/domain"
)

func (s *server) verificationRepo(w http.ResponseWriter, r *http.Request) domain.VerificationRepository {
	if len(r.URL.RawQuery) > 2048 {
		fail(w, domain.ErrInvalid)
		return nil
	}
	for _, key := range []string{"id", "findingID", "verificationID"} {
		v := r.PathValue(key)
		if v != "" && !domain.ValidSkillLoadID(v) {
			fail(w, domain.ErrInvalid)
			return nil
		}
	}
	if r.Method == "GET" && r.ContentLength != 0 {
		fail(w, domain.ErrInvalid)
		return nil
	}
	repo, ok := s.repo.(domain.VerificationRepository)
	if !ok {
		fail(w, domain.ErrUnavailable)
		return nil
	}
	return repo
}
func (s *server) startVerification(w http.ResponseWriter, r *http.Request) {
	repo := s.verificationRepo(w, r)
	if repo == nil {
		return
	}
	key, err := researchKey(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in domain.VerificationInput
	if err = decodeStrictData(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if err = domain.ValidateVerificationInput(in); err != nil {
		fail(w, err)
		return
	}
	mission, finding := r.PathValue("id"), r.PathValue("findingID")
	out, err := repo.StartVerification(r.Context(), key, mission, finding, in, func(ctx context.Context) error {
		if in.MaxSteps > s.cfg.System.MaxSteps {
			return domain.ErrInvalid
		}
		if s.cfg.MissionModels == nil {
			return domain.ErrUnavailable
		}
		return s.cfg.MissionModels.ValidateMissionModel(ctx, mission)
	})
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Location", "/api/operations/"+key)
	writeJSON(w, http.StatusAccepted, out)
}
func (s *server) listVerifications(w http.ResponseWriter, r *http.Request) {
	repo := s.verificationRepo(w, r)
	if repo == nil {
		return
	}
	q, err := parseFindingQuery(r, false)
	if err != nil {
		fail(w, err)
		return
	}
	out, err := repo.ListVerifications(r.Context(), r.PathValue("id"), r.PathValue("findingID"), q)
	respondFinding(w, out, err)
}
func (s *server) verification(w http.ResponseWriter, r *http.Request) {
	repo := s.verificationRepo(w, r)
	if repo == nil {
		return
	}
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	out, err := repo.GetVerification(r.Context(), r.PathValue("id"), r.PathValue("findingID"), r.PathValue("verificationID"))
	respondFinding(w, out, err)
}
func (s *server) verificationEvidence(w http.ResponseWriter, r *http.Request) {
	repo := s.verificationRepo(w, r)
	if repo == nil {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if r.URL.RawQuery != "" || err != nil || index < 0 || index >= 16 || strconv.Itoa(index) != r.PathValue("index") {
		fail(w, domain.ErrInvalid)
		return
	}
	out, err := repo.VerificationEvidence(r.Context(), r.PathValue("id"), r.PathValue("findingID"), r.PathValue("verificationID"), index)
	respondFinding(w, out, err)
}
