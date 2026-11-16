package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"wscyber/internal/domain"
)

func parseHTTPRecordQuery(raw string) (domain.HTTPRecordQuery, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return domain.HTTPRecordQuery{}, domain.ErrInvalid
	}
	for key, values := range values {
		switch key {
		case "limit", "cursor", "thread_id", "status", "method":
		default:
			return domain.HTTPRecordQuery{}, domain.ErrInvalid
		}
		if len(values) != 1 {
			return domain.HTTPRecordQuery{}, domain.ErrInvalid
		}
	}
	q := domain.HTTPRecordQuery{
		Limit: 50, Cursor: values.Get("cursor"), ThreadID: values.Get("thread_id"),
		Status: values.Get("status"), Method: values.Get("method"),
	}
	if rawLimit, present := values["limit"]; present {
		if rawLimit[0] == "" {
			return q, domain.ErrInvalid
		}
		for _, c := range rawLimit[0] {
			if c < '0' || c > '9' {
				return q, domain.ErrInvalid
			}
		}
		q.Limit, err = strconv.Atoi(rawLimit[0])
		if err != nil || q.Limit < 1 || q.Limit > 100 {
			return q, domain.ErrInvalid
		}
	}
	return domain.NormalizeHTTPRecordQuery(q)
}

func (s *server) listHTTPRecords(w http.ResponseWriter, r *http.Request) {
	query, err := parseHTTPRecordQuery(r.URL.RawQuery)
	if err != nil {
		fail(w, err)
		return
	}
	page, err := s.repo.ListHTTPRecords(r.Context(), r.PathValue("id"), query)
	respond(w, page, err)
}

func (s *server) httpRecord(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, domain.ErrInvalid)
		return
	}
	detail, err := s.repo.GetHTTPRecord(r.Context(), r.PathValue("id"), r.PathValue("actionID"))
	respond(w, detail, err)
}
