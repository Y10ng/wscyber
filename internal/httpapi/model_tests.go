package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelprobe"
)

func modelTestRequest(r *http.Request) bool {
	return r.URL.RawQuery == "" && !r.URL.ForceQuery && modelconfig.ValidID(r.PathValue("id")) && modelconfig.ValidID(r.PathValue("testID"))
}

// 此端点只返回固定错误，不让持久层/配置/供应商的原文成为错误正文。
func failModelTest(w http.ResponseWriter, err error) {
	code, msg := 503, "连通测试服务暂不可用，请保留测试ID并读取核对"
	switch {
	case errors.Is(err, domain.ErrInvalid):
		code, msg = 400, "需要合法测试ID、精确配置版本及显式计费确认，不接受其他字段或查询参数"
	case errors.Is(err, domain.ErrNotFound):
		code, msg = 404, "配置或测试回执不存在"
	case errors.Is(err, domain.ErrConflict):
		code, msg = 409, "测试ID身份冲突，或配置版本已变化/已禁用，请重新读取核对"
	}
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *server) getModelTest(w http.ResponseWriter, r *http.Request) {
	if !modelTestRequest(r) {
		failModelTest(w, domain.ErrInvalid)
		return
	}
	v, err := s.cfg.Probes.Get(r.Context(), r.PathValue("id"), r.PathValue("testID"))
	if err != nil {
		failModelTest(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) createModelTest(w http.ResponseWriter, r *http.Request) {
	if !modelTestRequest(r) || strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		failModelTest(w, domain.ErrInvalid)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		failModelTest(w, domain.ErrInvalid)
		return
	}
	defer clear(body)
	revision, err := modelprobe.DecodeConfirmation(body)
	if err != nil {
		failModelTest(w, err)
		return
	}
	v, created, err := s.cfg.Probes.Submit(r.Context(), r.PathValue("id"), r.PathValue("testID"), revision)
	if err != nil {
		failModelTest(w, err)
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusAccepted
	}
	writeJSON(w, code, v)
}
