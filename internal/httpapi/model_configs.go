package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

func (s *server) modelRequest(w http.ResponseWriter, r *http.Request) bool {
	if !s.cfg.Models.Available() {
		fail(w, fmt.Errorf("%w：模型配置加密密钥未配置", domain.ErrUnavailable))
		return false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		fail(w, fmt.Errorf("%w：模型配置端点不接受查询参数", domain.ErrInvalid))
		return false
	}
	return true
}
func (s *server) listModelConfigs(w http.ResponseWriter, r *http.Request) {
	if !s.modelRequest(w, r) {
		return
	}
	v, err := s.cfg.Models.List(r.Context())
	respond(w, v, err)
}
func (s *server) getModelConfig(w http.ResponseWriter, r *http.Request) {
	if !s.modelRequest(w, r) {
		return
	}
	v, err := s.cfg.Models.Get(r.Context(), r.PathValue("id"))
	respond(w, v, err)
}
func readModelInput(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return nil, fmt.Errorf("%w：需要 application/json", domain.ErrInvalid)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 96<<10)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("%w：模型配置请求超过大小限制或无法读取", domain.ErrInvalid)
	}
	return body, nil
}
func (s *server) saveModelConfig(w http.ResponseWriter, r *http.Request) {
	if !s.modelRequest(w, r) {
		return
	}
	body, err := readModelInput(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	defer clear(body)
	var v domain.ModelConfigView
	var expected int64
	if r.Method == http.MethodPatch {
		in, decodeErr := modelconfig.DecodeEdit(body)
		err = decodeErr
		expected = in.ExpectedRevision
		if err == nil {
			v, err = s.cfg.Models.Edit(r.Context(), r.PathValue("id"), in)
		}
	} else {
		in, decodeErr := modelconfig.DecodeWrite(body)
		err = decodeErr
		expected = in.ExpectedRevision
		if err == nil {
			v, err = s.cfg.Models.Save(r.Context(), r.PathValue("id"), in)
		}
	}
	if err != nil {
		fail(w, err)
		return
	}
	status := http.StatusOK
	if expected == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, v)
}
func (s *server) getModelSettings(w http.ResponseWriter, r *http.Request) {
	if !s.modelRequest(w, r) {
		return
	}
	v, err := s.cfg.Models.Settings(r.Context())
	respond(w, v, err)
}
func (s *server) setModelDefault(w http.ResponseWriter, r *http.Request) {
	if !s.modelRequest(w, r) {
		return
	}
	body, err := readModelInput(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	id, expected, err := modelconfig.DecodeDefault(body)
	if err != nil {
		fail(w, err)
		return
	}
	v, err := s.cfg.Models.SetDefault(r.Context(), id, expected)
	respond(w, v, err)
}
