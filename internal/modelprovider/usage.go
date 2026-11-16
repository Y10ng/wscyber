package modelprovider

import (
	"encoding/json"
	"strconv"

	"wscyber/internal/domain"
)

const MaxUsageTokens int64 = 1_000_000_000_000

func MissingUsage() domain.ModelUsage {
	return domain.ModelUsage{Status: "missing", ProviderUsage: map[string]string{}}
}

// ParseUsage 不依赖 SDK 的缺失字段零值，也不要求业务 Decision 有效。
// 额外计费信号单独返回，不能借助文本token价格冒充完整估算。
func ParseUsage(protocol string, body []byte) (domain.ModelUsage, bool) {
	u := MissingUsage()
	if !validResponseJSON(body) {
		u.Status = "invalid"
		return u, false
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		u.Status = "invalid"
		return u, false
	}
	raw, ok := root["usage"]
	if !ok || string(raw) == "null" {
		return u, false
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(raw, &usage) != nil || usage == nil {
		u.Status = "invalid"
		return u, false
	}
	invalid, unsupported := false, responseExtraBilling(protocol, root)
	read := func(obj map[string]json.RawMessage, key, path string) *int64 {
		v, exists := obj[key]
		if !exists {
			return nil
		}
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil || n < 0 || n > MaxUsageTokens {
			invalid = true
			return nil
		}
		u.ProviderUsage[path] = strconv.FormatInt(n, 10)
		return &n
	}
	object := func(obj map[string]json.RawMessage, key string) map[string]json.RawMessage {
		v, exists := obj[key]
		if !exists {
			return nil
		}
		var out map[string]json.RawMessage
		if json.Unmarshal(v, &out) != nil || out == nil {
			invalid = true
		}
		return out
	}
	checkKeys := func(obj map[string]json.RawMessage, known ...string) {
		for key := range obj {
			found := false
			for _, k := range known {
				if key == k {
					found = true
					break
				}
			}
			if !found {
				unsupported = true
			}
		}
	}
	str := func(n *int64) *string {
		if n == nil {
			return nil
		}
		s := strconv.FormatInt(*n, 10)
		return &s
	}
	var input, output, cache, creation, five, hour, reasoning *int64
	if protocol == "openai_chat" {
		input = read(usage, "prompt_tokens", "prompt_tokens")
		output = read(usage, "completion_tokens", "completion_tokens")
		total := read(usage, "total_tokens", "total_tokens")
		prompt := object(usage, "prompt_tokens_details")
		completion := object(usage, "completion_tokens_details")
		cache = read(prompt, "cached_tokens", "prompt_tokens_details.cached_tokens")
		reasoning = read(completion, "reasoning_tokens", "completion_tokens_details.reasoning_tokens")
		for _, bucket := range []struct{ value, parent *int64 }{
			{read(prompt, "audio_tokens", "prompt_tokens_details.audio_tokens"), input},
			{read(completion, "audio_tokens", "completion_tokens_details.audio_tokens"), output},
			{read(completion, "accepted_prediction_tokens", "completion_tokens_details.accepted_prediction_tokens"), output},
			{read(completion, "rejected_prediction_tokens", "completion_tokens_details.rejected_prediction_tokens"), output},
		} {
			if bucket.value != nil && *bucket.value > 0 {
				unsupported = true
			}
			if bucket.value != nil && bucket.parent != nil && *bucket.value > *bucket.parent {
				invalid = true
			}
		}
		if total != nil && ((input != nil && *total < *input) || (output != nil && *total < *output)) {
			invalid = true
		}
		if total != nil && input != nil && output != nil && *total != *input+*output {
			invalid = true
		}
		if cache != nil && input != nil && *cache > *input {
			invalid = true
		}
		if reasoning != nil && output != nil && *reasoning > *output {
			invalid = true
		}
		checkKeys(usage, "prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details", "completion_tokens_details")
		checkKeys(prompt, "cached_tokens", "audio_tokens")
		checkKeys(completion, "reasoning_tokens", "audio_tokens", "accepted_prediction_tokens", "rejected_prediction_tokens")
	} else if protocol == "anthropic_messages" {
		native := read(usage, "input_tokens", "input_tokens")
		output = read(usage, "output_tokens", "output_tokens")
		cache = read(usage, "cache_read_input_tokens", "cache_read_input_tokens")
		creation = read(usage, "cache_creation_input_tokens", "cache_creation_input_tokens")
		c := object(usage, "cache_creation")
		five = read(c, "ephemeral_5m_input_tokens", "cache_creation.ephemeral_5m_input_tokens")
		hour = read(c, "ephemeral_1h_input_tokens", "cache_creation.ephemeral_1h_input_tokens")
		if native != nil && cache != nil && creation != nil {
			n := *native + *cache + *creation
			if n > MaxUsageTokens {
				invalid = true
			} else {
				input = &n
			}
		}
		if five != nil && creation != nil && *five > *creation {
			invalid = true
		}
		if hour != nil && creation != nil && *hour > *creation {
			invalid = true
		}
		if five != nil && hour != nil && creation != nil && *five+*hour != *creation {
			invalid = true
		}
		tools := object(usage, "server_tool_use")
		for _, key := range []string{"web_search_requests", "web_fetch_requests"} {
			v := read(tools, key, "server_tool_use."+key)
			if v != nil && *v > 0 {
				unsupported = true
			}
		}
		checkKeys(usage, "input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "cache_creation", "server_tool_use", "service_tier", "inference_geo")
		checkKeys(c, "ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens")
		checkKeys(tools, "web_search_requests", "web_fetch_requests")
		for _, key := range []string{"service_tier", "inference_geo"} {
			if v, ok := usage[key]; ok && string(v) != "null" {
				var s string
				if json.Unmarshal(v, &s) != nil {
					invalid = true
				} else if (key == "service_tier" && s != "standard") || (key == "inference_geo" && s != "not_available") {
					unsupported = true
				}
			}
		}
	} else {
		invalid = true
	}
	if invalid {
		u = MissingUsage()
		u.Status = "invalid"
		return u, unsupported
	}
	u.InputTokens = str(input)
	u.OutputTokens = str(output)
	u.CacheReadInputTokens = str(cache)
	u.CacheCreationInputTokens = str(creation)
	u.CacheCreation5mInputTokens = str(five)
	u.CacheCreation1hInputTokens = str(hour)
	u.ReasoningOutputTokens = str(reasoning)
	u.Status = "partial"
	if input != nil && output != nil {
		total := *input + *output
		if total > MaxUsageTokens {
			u = MissingUsage()
			u.Status = "invalid"
			return u, unsupported
		}
		u.TotalTokens = str(&total)
		u.Status = "reported"
	}
	return u, unsupported
}

// 不能只看usage：音频、服务器工具或不同服务档位也可能在响应信封里明确报告。
func responseExtraBilling(protocol string, root map[string]json.RawMessage) bool {
	present := func(v json.RawMessage) bool { return len(v) > 0 && string(v) != "null" }
	if protocol == "openai_chat" {
		if raw := root["service_tier"]; present(raw) {
			var tier string
			if json.Unmarshal(raw, &tier) != nil || (tier != "default" && tier != "standard") {
				return true
			}
		}
		var choices []struct {
			Message map[string]json.RawMessage `json:"message"`
		}
		if json.Unmarshal(root["choices"], &choices) == nil {
			for _, choice := range choices {
				if present(choice.Message["audio"]) {
					return true
				}
			}
		}
	}
	if protocol == "anthropic_messages" {
		if present(root["container"]) {
			return true
		}
		var blocks []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(root["content"], &blocks) == nil {
			for _, block := range blocks {
				switch block.Type {
				case "text", "thinking", "redacted_thinking":
				default:
					return true
				}
			}
		}
	}
	return false
}
