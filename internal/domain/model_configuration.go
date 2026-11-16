package domain

import (
	"context"
	"encoding/json"
	"time"
)

// ModelConnection 是模型协议配置，不授予目标访问权限。
// Key/Header/参数原值只用于内部解密及实际请求，禁止直接作为 API 响应。
type ModelConnection struct {
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	Auth            string `json:"auth"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	MaxOutputTokens int    `json:"max_output_tokens"`
}

type ModelSecrets struct {
	APIKey     string                     `json:"api_key"`
	Headers    map[string]string          `json:"headers"`
	Parameters map[string]json.RawMessage `json:"parameters"`
}

type ModelDocument struct {
	ModelConnection
	Secrets ModelSecrets `json:"secrets"`
}

// SecretChange 中 mode 必须是 keep/replace/clear；非 replace 不得携带 value。
type SecretChange[T any] struct {
	Mode  string `json:"mode"`
	Value T      `json:"value,omitempty"`
}

type ModelConfigWrite struct {
	ModelConnection
	ExpectedRevision int64                                    `json:"expected_revision"`
	Enabled          bool                                     `json:"enabled"`
	APIKey           SecretChange[string]                     `json:"api_key"`
	Headers          SecretChange[map[string]string]          `json:"headers"`
	Parameters       SecretChange[map[string]json.RawMessage] `json:"parameters"`
}

// ModelConfigView 永不包含 API key、Header 值或扩展参数值。
type ModelConfigView struct {
	ModelConnection
	ID            string    `json:"id"`
	Revision      int64     `json:"revision"`
	Enabled       bool      `json:"enabled"`
	APIKeySet     bool      `json:"api_key_set"`
	HeaderNames   []string  `json:"header_names"`
	ParameterKeys []string  `json:"parameter_keys"`
	CreatedAt     time.Time `json:"created_at"`
}

// SealedModelConfig 仅在配置管理器与持久层之间传递，不对 HTTP 暴露。
type SealedModelConfig struct {
	ID         string
	Revision   int64
	Enabled    bool // 配置级当前状态，即使读取历史版本也遵守最新禁用状态。
	Ciphertext []byte
	CreatedAt  time.Time
}

type ModelSettings struct {
	Revision        int64  `json:"revision"`
	DefaultConfigID string `json:"default_config_id"`
}

// ModelConfigRepository 与研究状态接口分离，已有研究/执行适配器无需获得密文访问权。
type ModelConfigRepository interface {
	ListModelConfigs(context.Context) ([]SealedModelConfig, error)
	GetModelConfig(context.Context, string, int64) (SealedModelConfig, error) // revision=0 为最新版本。
	SaveModelConfig(context.Context, SealedModelConfig, int64) (SealedModelConfig, error)
	GetModelSettings(context.Context) (ModelSettings, error)
	SetModelDefault(context.Context, string, int64) (ModelSettings, error)
}
