package modelprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	anthropic "github.com/anthropics/anthropic-sdk-go/v2"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/v2/option"
	openai "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

const (
	NotSent          = "not_sent"
	TransportUnknown = "transport_unknown"
	HTTPError        = "http_error"
	InvalidResponse  = "invalid_response"
	ValidResponse    = "valid_response"
	maxInputBytes    = 256 << 10
	maxTextBytes     = 32 << 10
)

// Input 由调用用途构造。连通测试只能给固定最小内容，不能混入研究快照。
type Input struct {
	System          string
	User            string
	MaxOutputTokens int
	AllowSend       func() bool // 内部持久化fail-stop准入，永不序列化。
}

// Result 不保存SDK错误对象/响应原文；Text仅供内部研究决策解析，不由测试API回显。
// 未收到有效回执时不推断未计费，也不自行重试；Response仅供显式启用的加密账本。
type Result struct {
	Outcome           string            `json:"outcome"`
	HTTPStatus        int               `json:"http_status,omitempty"`
	Attempted         bool              `json:"attempted"`
	Text              string            `json:"-"`
	Usage             domain.ModelUsage `json:"-"`
	ExtraBilling      bool              `json:"-"`
	Response          []byte            `json:"-"`
	ResponseReceived  bool              `json:"-"`
	ResponseComplete  bool              `json:"-"`
	ResponseMediaType string            `json:"-"`
}

// Complete 每次最多一次POST；没有批量、自动重试、重定向或环境认证继承。
func Complete(ctx context.Context, doc domain.ModelDocument, input Input) Result {
	out := Result{Outcome: NotSent, Usage: MissingUsage()}
	doc, err := modelconfig.ValidateForUse(doc)
	if err != nil || ctx.Err() != nil || input.MaxOutputTokens < 1 || input.MaxOutputTokens > doc.MaxOutputTokens || len(input.System)+len(input.User) > maxInputBytes || strings.TrimSpace(input.User) == "" || !utf8.ValidString(input.User) || !utf8.ValidString(input.System) {
		return out
	}
	base := strings.TrimRight(doc.BaseURL, "/") + "/"
	path := "chat/completions"
	if doc.Protocol == "anthropic_messages" {
		path = "v1/messages"
	}
	endpoint, err := url.Parse(base + path)
	if err != nil {
		return out
	}
	transport := newTransport(endpoint)
	transport.allowSend = input.AllowSend
	defer transport.transport.CloseIdleConnections()
	timeout := time.Duration(doc.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var text string
	var valid bool
	if doc.Protocol == "openai_chat" {
		text, valid, err = chat(ctx, client, doc, input, base, timeout)
	} else {
		text, valid, err = messages(ctx, client, doc, input, base, timeout)
	}
	out.Attempted, out.HTTPStatus = transport.attempted, transport.status
	out.Response, out.ResponseReceived, out.ResponseComplete, out.ResponseMediaType = transport.response, transport.received, transport.complete, transport.mediaType
	if transport.jsonResponse {
		out.Usage, out.ExtraBilling = ParseUsage(doc.Protocol, transport.response)
	} else if transport.complete && transport.mediaType == "application/json" {
		out.Usage.Status = "invalid"
	}
	switch {
	case out.HTTPStatus != 0 && out.HTTPStatus != http.StatusOK:
		out.Outcome = HTTPError
	case out.Attempted && (ctx.Err() != nil || out.HTTPStatus == 0 || transport.incomplete):
		out.Outcome = TransportUnknown
	case out.Attempted && (err != nil || !valid || strings.TrimSpace(text) == "" || len(text) > maxTextBytes || !utf8.ValidString(text)):
		out.Outcome = InvalidResponse
	case out.Attempted:
		out.Outcome, out.Text = ValidResponse, text
	}
	return out
}

func chat(ctx context.Context, client *http.Client, doc domain.ModelDocument, input Input, base string, timeout time.Duration) (string, bool, error) {
	opts := []openaioption.RequestOption{
		openaioption.WithBaseURL(base), openaioption.WithHTTPClient(client),
		openaioption.WithAPIKey(doc.Secrets.APIKey), openaioption.WithMaxRetries(0), openaioption.WithRequestTimeout(timeout),
	}
	for k, v := range doc.Secrets.Headers {
		opts = append(opts, openaioption.WithHeader(k, v))
	}
	for k, v := range doc.Secrets.Parameters {
		opts = append(opts, openaioption.WithJSONSet(k, json.RawMessage(v)))
	}
	// 公开服务构造器不读取OPENAI_*；NewClient会读取，不能仅覆盖已知几个环境字段。
	service := openai.NewChatCompletionService(opts...)
	conversation := []openai.ChatCompletionMessageParamUnion{}
	if input.System != "" {
		conversation = append(conversation, openai.SystemMessage(input.System))
	}
	conversation = append(conversation, openai.UserMessage(input.User))
	response, err := service.New(ctx, openai.ChatCompletionNewParams{
		Model: doc.Model, MaxCompletionTokens: openai.Int(int64(input.MaxOutputTokens)), Messages: conversation,
	})
	if err != nil || response == nil || !fieldKind(response.JSON.Choices, '[') || len(response.Choices) != 1 || !fieldKind(response.JSON.Object, '"') || response.Object != "chat.completion" || !absentOrNull(response.JSON.ExtraFields["error"]) {
		return "", false, err
	}
	choice := response.Choices[0]
	message := choice.Message
	valid := fieldKind(choice.JSON.FinishReason, '"') && choice.FinishReason == "stop" && choice.JSON.Index.Raw() == "0" && fieldKind(choice.JSON.Message, '{') &&
		fieldKind(message.JSON.Role, '"') && message.Role == "assistant" && fieldKind(message.JSON.Content, '"') &&
		(absentOrNull(message.JSON.Refusal) || (fieldKind(message.JSON.Refusal, '"') && message.Refusal == "")) &&
		(absentOrNull(message.JSON.ToolCalls) || (fieldKind(message.JSON.ToolCalls, '[') && len(message.ToolCalls) == 0)) &&
		absentOrNull(message.JSON.FunctionCall) && absentOrNull(message.JSON.Audio)
	return message.Content, valid, nil
}

func messages(ctx context.Context, client *http.Client, doc domain.ModelDocument, input Input, base string, timeout time.Duration) (string, bool, error) {
	opts := []anthropicoption.RequestOption{
		anthropicoption.WithoutEnvironmentDefaults(), anthropicoption.WithBaseURL(base), anthropicoption.WithHTTPClient(client),
		anthropicoption.WithMaxRetries(0), anthropicoption.WithRequestTimeout(timeout),
	}
	switch doc.Auth {
	case "bearer":
		opts = append(opts, anthropicoption.WithAuthToken(doc.Secrets.APIKey))
	case "api_key":
		// 固定v2.9.0的WithAPIKey实际生成Bearer；显式保持配置的X-Api-Key语义。
		// WithoutEnvironmentDefaults已禁用凭据链，此处不叠加第二种认证。
		opts = append(opts, anthropicoption.WithHeader("X-Api-Key", doc.Secrets.APIKey))
	}
	for k, v := range doc.Secrets.Headers {
		opts = append(opts, anthropicoption.WithHeader(k, v))
	}
	for k, v := range doc.Secrets.Parameters {
		opts = append(opts, anthropicoption.WithJSONSet(k, json.RawMessage(v)))
	}
	service := anthropic.NewClient(opts...)
	params := anthropic.MessageNewParams{
		Model: anthropic.Model(doc.Model), MaxTokens: int64(input.MaxOutputTokens),
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(input.User))},
	}
	if input.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: input.System}}
	}
	response, err := service.Messages.New(ctx, params)
	if err != nil || response == nil || !fieldKind(response.JSON.StopReason, '"') || response.StopReason != anthropic.StopReasonEndTurn ||
		!fieldKind(response.JSON.Role, '"') || response.Role != "assistant" || !fieldKind(response.JSON.Type, '"') || response.Type != "message" ||
		!fieldKind(response.JSON.Content, '[') || !absentOrNull(response.JSON.Container) || !absentOrNull(response.JSON.ExtraFields["error"]) {
		return "", false, err
	}
	var text strings.Builder
	for _, block := range response.Content {
		if !fieldKind(block.JSON.Type, '"') {
			return "", false, nil
		}
		switch block.Type {
		case "text":
			if !fieldKind(block.JSON.Text, '"') || text.Len()+len(block.Text) > maxTextBytes {
				return "", false, nil
			}
			text.WriteString(block.Text)
		case "thinking": // 允许扩展启用思考，但不把思考正文返回为决策。
			if !fieldKind(block.JSON.Thinking, '"') || !fieldKind(block.JSON.Signature, '"') {
				return "", false, nil
			}
		case "redacted_thinking":
			if !fieldKind(block.JSON.Data, '"') {
				return "", false, nil
			}
		default:
			return "", false, nil // 不接受/执行未请求的工具、容器或服务器工具回执。
		}
	}
	return text.String(), true, nil
}
