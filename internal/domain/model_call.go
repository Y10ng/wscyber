package domain

import (
	"context"
	"time"
)

// ModelUsage 只接受供应商经白名单验证的整数计数；nil 不等于零。
type ModelUsage struct {
	Status                     string            `json:"status"`
	InputTokens                *string           `json:"input_tokens"`
	OutputTokens               *string           `json:"output_tokens"`
	TotalTokens                *string           `json:"total_tokens"`
	CacheReadInputTokens       *string           `json:"cache_read_input_tokens"`
	CacheCreationInputTokens   *string           `json:"cache_creation_input_tokens"`
	CacheCreation5mInputTokens *string           `json:"cache_creation_5m_input_tokens"`
	CacheCreation1hInputTokens *string           `json:"cache_creation_1h_input_tokens"`
	ReasoningOutputTokens      *string           `json:"reasoning_output_tokens"`
	ProviderUsage              map[string]string `json:"provider_usage"`
}

type PriceTerms struct {
	Currency               string  `json:"currency"`
	InputPerMillion        string  `json:"input_per_million"`
	OutputPerMillion       string  `json:"output_per_million"`
	CacheReadPerMillion    *string `json:"cache_read_per_million"`
	CacheWrite5mPerMillion *string `json:"cache_write_5m_per_million"`
	CacheWrite1hPerMillion *string `json:"cache_write_1h_per_million"`
}

type ModelPriceKey struct {
	Source        string `json:"source"`
	ModelConfigID string `json:"model_config_id"`
	Protocol      string `json:"protocol"`
	Model         string `json:"model"`
}
type ModelPrice struct {
	ModelPriceKey
	PriceTerms
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
type ModelPriceWrite struct {
	ModelPriceKey
	PriceTerms
	ExpectedRevision int64 `json:"expected_revision"`
	Enabled          bool  `json:"enabled"`
}

type ModelCost struct {
	Status        string      `json:"status"`
	Currency      *string     `json:"currency"`
	Amount        *string     `json:"amount"`
	PriceID       *string     `json:"price_id"`
	PriceSnapshot *PriceTerms `json:"price_snapshot"`
}

type ModelCall struct {
	ModelPriceKey
	ID                 string     `json:"id"`
	ConfigName         string     `json:"config_name"`
	Purpose            string     `json:"purpose"`
	MissionID          string     `json:"mission_id"`
	ThreadID           string     `json:"thread_id"`
	AttemptID          string     `json:"attempt_id"`
	TestID             string     `json:"test_id"`
	StartedAt          time.Time  `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	DurationMS         *int64     `json:"duration_ms"`
	State              string     `json:"state"`
	Outcome            string     `json:"outcome"`
	Attempted          *bool      `json:"attempted"`
	HTTPStatus         *int       `json:"http_status"`
	RecordModelContent bool       `json:"record_model_content"`
	ContentState       string     `json:"content_state"`
	Usage              ModelUsage `json:"usage"`
	Cost               ModelCost  `json:"cost"`
}

type ModelRecording struct {
	MissionID          string `json:"mission_id"`
	RecordModelContent bool   `json:"record_model_content"`
}
type ModelContentPart struct {
	State      string  `json:"state"`
	BodyBase64 *string `json:"body_base64"`
	MediaType  string  `json:"media_type"`
	Complete   bool    `json:"complete"`
	Redacted   bool    `json:"redacted"`
}
type ModelCallContent struct {
	CallID   string            `json:"call_id"`
	Recorded bool              `json:"recorded"`
	Input    *ModelContentPart `json:"input"`
	Response *ModelContentPart `json:"response"`
}

// SealedCallContent 不可作为列表/详情响应；AAD 由调用ID和类别共同决定。
type SealedCallContent struct {
	Kind       string
	Ciphertext []byte
	MediaType  string
	Complete   bool
	Redacted   bool
}

type BeginModelCall struct {
	Call           ModelCall
	ConfigRevision int64
	Input          *SealedCallContent
	SkillLoadIDs   []string // 实际进入本次模型输入的固定加载身份。
	SupplementIDs  []string // 实际装配的人工补充；不是已发送/已理解的断言。
	Assembly       *ContextAssembly
}
type FinishModelCall struct {
	ID         string
	Outcome    string
	Attempted  bool
	HTTPStatus *int
	FinishedAt time.Time
	DurationMS int64
	Usage      ModelUsage
	Cost       ModelCost
	Response   *SealedCallContent
}

type ModelCallRange struct {
	From     string `json:"from"`
	To       string `json:"to"`
	TimeZone string `json:"time_zone"`
}
type ModelCallFilter struct {
	MissionID     string
	ModelConfigID string
	Purpose       string
	Outcome       string
	Range         ModelCallRange
	From          time.Time
	To            time.Time
	Limit         int
	Cursor        string
	AllTime       bool
}
type ModelCallsPage struct {
	Items             []ModelCall    `json:"items"`
	NextCursor        *string        `json:"next_cursor"`
	CoverageStartedAt time.Time      `json:"coverage_started_at"`
	Range             ModelCallRange `json:"range"`
}
type EstimatedCostTotal struct {
	Currency    string `json:"currency"`
	Amount      string `json:"amount"`
	PricedCalls string `json:"priced_calls"`
}
type ModelUsageSummary struct {
	Calls             string               `json:"calls"`
	RunningCalls      string               `json:"running_calls"`
	InterruptedCalls  string               `json:"interrupted_calls"`
	InputTokens       string               `json:"input_tokens"`
	OutputTokens      string               `json:"output_tokens"`
	TotalTokens       string               `json:"total_tokens"`
	MissingUsageCalls string               `json:"missing_usage_calls"`
	InvalidUsageCalls string               `json:"invalid_usage_calls"`
	EstimatedCosts    []EstimatedCostTotal `json:"estimated_costs"`
	UnpricedCalls     string               `json:"unpriced_calls"`
}
type ModelUsageDay struct {
	Date string `json:"date"`
	ModelUsageSummary
}
type ModelUsageReport struct {
	CoverageStartedAt time.Time         `json:"coverage_started_at"`
	Range             ModelCallRange    `json:"range"`
	SummaryScope      string            `json:"summary_scope,omitempty"`
	Summary           ModelUsageSummary `json:"summary"`
	Daily             []ModelUsageDay   `json:"daily"`
}

type ModelCallRepository interface {
	GetModelRecording(context.Context, string) (ModelRecording, error)
	BeginModelCall(context.Context, BeginModelCall) (ModelCall, error)
	FinishModelCall(context.Context, FinishModelCall) error
	GetModelCall(context.Context, string, string) (ModelCall, error)
	GetSealedCallContent(context.Context, string) ([]SealedCallContent, error)
	ListModelCalls(context.Context, ModelCallFilter) (ModelCallsPage, error)
	ModelUsage(context.Context, ModelCallFilter) (ModelUsageReport, error)
	ListModelPrices(context.Context) ([]ModelPrice, error)
	SaveModelPrice(context.Context, string, ModelPriceWrite, func(SealedModelConfig) bool) (ModelPrice, bool, error)
}

// AttemptModel 由研究引擎明确传入本次认领，不从 Snapshot 的顺序推断身份。
type AttemptModel interface {
	DecideAttempt(context.Context, Snapshot, string) (Decision, error)
}
