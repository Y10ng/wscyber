package domain

import "time"

// ExplorationInput 的两个文本输入不是授权；AllowedOrigins 必须单独明确并由 API 规范化。
type ExplorationInput struct {
	Description    string   `json:"description"`
	Goal           string   `json:"goal"`
	AllowedOrigins []string `json:"allowed_origins"`
	// omitempty 保持历史未选择模型的操作指纹不变。
	ModelConfigID       string              `json:"model_config_id,omitempty"`
	RecordModelContent  bool                `json:"record_model_content,omitempty"`
	Materials           []MaterialSelection `json:"materials,omitempty"`
	SafetyPromptEnabled bool                `json:"safety_prompt_enabled,omitempty"`
	SafetyPrompt        string              `json:"safety_prompt,omitempty"`
	ResearchBrief       *ResearchBrief      `json:"research_brief,omitempty"`
}

// ExplorationStart 是提交时的固定回执，不是当前运行状态。实时状态读取 Thread Snapshot。
type ExplorationStart struct {
	Mission     Mission `json:"mission"`
	Thread      Thread  `json:"thread"`
	StartStatus string  `json:"start_status"`
}

// OperationReceipt 只公开提交元数据，不公开输入指纹、原响应或授权令牌。
type OperationReceipt struct {
	Key            string    `json:"key"`
	Method         string    `json:"method"`
	Path           string    `json:"path"`
	ResourceType   string    `json:"resource_type"`
	ResourceID     string    `json:"resource_id"`
	ResponseStatus int       `json:"response_status"`
	CommittedAt    time.Time `json:"committed_at"`
}

// ValidOperationKey 检查传输格式；随机性必须由客户端保证，不能从字符串证明熵。
// 支持 UUID、hex 和无填充 base64url；要求客户端使用至少 128 位随机值。
func ValidOperationKey(key string) bool {
	if len(key) < 22 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
