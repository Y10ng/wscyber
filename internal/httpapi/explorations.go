package httpapi

import (
	"fmt"
	"net/http"
	"slices"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/modelconfig"
)

func (s *server) startExploration(w http.ResponseWriter, r *http.Request) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !domain.ValidOperationKey(keys[0]) {
		fail(w, fmt.Errorf("%w：需要唯一有效的 Idempotency-Key（22–128 位字母、数字、横线或下划线）", domain.ErrInvalid))
		return
	}
	var in domain.ExplorationInput
	if err := decodeExploration(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if !required(in.Description, 64000) || !required(in.Goal, 32000) || len(in.AllowedOrigins) == 0 || len(in.AllowedOrigins) > 64 {
		fail(w, fmt.Errorf("%w：请填写目标／已知信息、希望达到的目标和 1–64 个明确授权 origin", domain.ErrInvalid))
		return
	}
	for i, raw := range in.AllowedOrigins {
		normalized, err := execution.NormalizeOrigin(raw)
		if err != nil {
			// 不回显用户可能误填在 URL 中的凭据或材料。
			fail(w, fmt.Errorf("%w：授权范围必须是无路径、无凭据的精确 HTTP(S) origin", domain.ErrInvalid))
			return
		}
		in.AllowedOrigins[i] = normalized
	}
	slices.Sort(in.AllowedOrigins)
	in.AllowedOrigins = slices.Compact(in.AllowedOrigins)
	// 能力检查在去重之后：重启后未配置模型，也必须允许核对旧成功提交。
	if in.ModelConfigID != "" && !modelconfig.ValidID(in.ModelConfigID) {
		fail(w, fmt.Errorf("%w：模型配置ID必须为32位小写十六进制", domain.ErrInvalid))
		return
	}
	var started domain.ExplorationStart
	var err error
	if repo, ok := s.repo.(domain.MissionModelRepository); ok {
		started, err = repo.StartExplorationWithModel(r.Context(), keys[0], in, domain.ExplorationModelOptions{ContentRecordingAvailable: s.cfg.Ledger.Available(), EnvironmentConfigured: s.cfg.System.ModelConfigured, EnvironmentFingerprint: s.cfg.EnvironmentFingerprint, Validate: s.cfg.Models.ValidateRecordForUse})
	} else if in.ModelConfigID != "" || in.RecordModelContent || len(in.Materials) > 0 || in.SafetyPromptEnabled || in.SafetyPrompt != "" || in.ResearchBrief != nil {
		err = domain.ErrUnavailable
	} else {
		started, err = s.repo.StartExploration(r.Context(), keys[0], in, s.cfg.System.ModelConfigured)
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Location", "/api/operations/"+keys[0])
	writeJSON(w, http.StatusAccepted, started)
}

// 严格递归校验新选材与清单，保持历史省略字段兼容。
func decodeExploration(w http.ResponseWriter, r *http.Request, in *domain.ExplorationInput) error {
	if err := decodeStrictData(w, r, in); err != nil {
		return err
	}
	return domain.ValidateResearchSelection(*in)
}

func (s *server) operation(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !domain.ValidOperationKey(key) {
		fail(w, fmt.Errorf("%w：提交键格式无效", domain.ErrInvalid))
		return
	}
	receipt, err := s.repo.GetOperation(r.Context(), key)
	respond(w, receipt, err)
}
