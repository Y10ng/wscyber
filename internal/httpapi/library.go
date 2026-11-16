package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"wscyber/internal/domain"
)

func (s *server) materialRepo(w http.ResponseWriter) domain.ResearchMaterialRepository {
	repo, ok := s.repo.(domain.ResearchMaterialRepository)
	if !ok {
		fail(w, domain.ErrUnavailable)
		return nil
	}
	return repo
}
func (s *server) libraryFolders(w http.ResponseWriter, r *http.Request) {
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	items, err := repo.ListLibraryFolders(r.Context())
	respond(w, struct {
		Items []domain.LibraryItem `json:"items"`
	}{items}, err)
}
func (s *server) libraryItems(w http.ResponseWriter, r *http.Request) {
	q, err := parseLibraryQuery(r)
	if err != nil {
		fail(w, err)
		return
	}
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.ListLibraryItems(r.Context(), q)
	respond(w, out, err)
}
func parseLibraryQuery(r *http.Request) (domain.LibraryQuery, error) {
	q := domain.LibraryQuery{Limit: 25}
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, domain.ErrInvalid
	}
	for key, values := range v {
		if len(values) != 1 {
			return q, domain.ErrInvalid
		}
		switch key {
		case "parent_id", "all", "kind", "q", "after", "limit":
		default:
			return q, domain.ErrInvalid
		}
	}
	q.ParentID = v.Get("parent_id")
	q.Kind = v.Get("kind")
	q.Search = v.Get("q")
	q.After = v.Get("after")
	if v.Has("all") {
		switch v.Get("all") {
		case "true":
			q.All = true
		case "false":
		default:
			return q, domain.ErrInvalid
		}
	}
	if v.Has("limit") {
		n, err := strconv.Atoi(v.Get("limit"))
		if err != nil || n < 1 || n > 100 {
			return q, domain.ErrInvalid
		}
		q.Limit = n
	}
	return q, nil
}
func (s *server) libraryItem(w http.ResponseWriter, r *http.Request) {
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.GetLibraryItem(r.Context(), r.PathValue("itemID"))
	respond(w, out, err)
}
func libraryWriteKey(r *http.Request) (string, error) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !domain.ValidOperationKey(keys[0]) {
		return "", domain.ErrInvalid
	}
	return keys[0], nil
}
func (s *server) saveLibraryItem(w http.ResponseWriter, r *http.Request) {
	key, err := libraryWriteKey(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in domain.LibraryWrite
	if err = decodeStrictData(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.SaveLibraryItem(r.Context(), key, r.PathValue("itemID"), in)
	respond(w, out, err)
}
func (s *server) deleteLibraryItem(w http.ResponseWriter, r *http.Request) {
	key, err := libraryWriteKey(r)
	if err != nil {
		fail(w, err)
		return
	}
	var in domain.LibraryDelete
	if err = decodeStrictData(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.DeleteLibraryItem(r.Context(), key, r.PathValue("itemID"), in)
	respond(w, out, err)
}
func (s *server) libraryOperation(w http.ResponseWriter, r *http.Request) {
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.GetLibraryOperation(r.Context(), r.PathValue("key"))
	respond(w, out, err)
}
func (s *server) researchConfiguration(w http.ResponseWriter, r *http.Request) {
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.GetResearchConfiguration(r.Context(), r.PathValue("id"))
	respond(w, out, err)
}
func (s *server) taskMaterial(w http.ResponseWriter, r *http.Request) {
	repo := s.materialRepo(w)
	if repo == nil {
		return
	}
	out, err := repo.GetTaskMaterial(r.Context(), r.PathValue("id"), r.PathValue("itemID"))
	respond(w, out, err)
}
