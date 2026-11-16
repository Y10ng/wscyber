package httpapi

import (
	"net/http"
	"wscyber/internal/domain"
)

func (s *server) systemInfo(w http.ResponseWriter, r *http.Request) {
	info := s.cfg.System
	info.ModelContentRecordingAvailable = s.cfg.Ledger.Available()
	info.ModelManagementAvailable = s.cfg.Models.Available()
	info.ModelSource = "none"
	info.DefaultModelConfigID = ""
	if info.ModelConfigured {
		info.ModelSource = "environment"
	} else {
		info.Model = ""
	}
	id, name, usable, err := s.cfg.Models.Default(r.Context())
	if err != nil {
		fail(w, domain.ErrUnavailable)
		return
	}
	if id != "" {
		info.DefaultModelConfigID = id
		info.ModelSource = "managed"
		info.ModelConfigured = usable
		info.Model = name
	}
	writeJSON(w, http.StatusOK, info)
}
func (s *server) missionModel(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(w, domain.ErrInvalid)
		return
	}
	if s.cfg.MissionModels == nil {
		if _, err := s.repo.GetMission(r.Context(), r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		fail(w, domain.ErrUnavailable)
		return
	}
	v, err := s.cfg.MissionModels.MissionModel(r.Context(), r.PathValue("id"))
	respond(w, v, err)
}
