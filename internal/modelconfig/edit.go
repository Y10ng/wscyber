package modelconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"wscyber/internal/domain"
)

// ModelConfigEdit 使用完整条目清单；nil Value 表示省略，而 JSON null 是参数实际值。
type ModelConfigEdit struct {
	domain.ModelConnection
	ExpectedRevision int64            `json:"expected_revision"`
	Enabled          bool             `json:"enabled"`
	APIKey           *string          `json:"api_key,omitempty"`
	Headers          []HeaderEntry    `json:"headers"`
	Parameters       []ParameterEntry `json:"parameters"`
}
type HeaderEntry struct {
	Name  string  `json:"name"`
	Value *string `json:"value,omitempty"`
}
type ParameterEntry struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value,omitempty"`
}

// DecodeEdit 直接检查原始 JSON，拒绝重复字段、未知字段和歧义 null；不先反序列化秘密参数。
func DecodeEdit(raw []byte) (ModelConfigEdit, error) {
	var in ModelConfigEdit
	if err := validateJSON(raw); err != nil {
		return in, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return in, invalid("模型配置必须为对象")
	}
	required := []string{"name", "protocol", "base_url", "model", "auth", "timeout_seconds", "max_output_tokens", "expected_revision", "enabled", "headers", "parameters"}
	allowed := map[string]bool{"api_key": true}
	for _, name := range required {
		allowed[name] = true
		if v, ok := fields[name]; !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return in, invalid("模型配置缺少必填字段")
		}
	}
	for name, v := range fields {
		if !allowed[name] || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return in, invalid("模型配置含未知字段或无效 null")
		}
	}
	for _, name := range []string{"headers", "parameters"} {
		var entries []map[string]json.RawMessage
		if json.Unmarshal(fields[name], &entries) != nil {
			return in, invalid("条目清单必须为数组")
		}
		for _, entry := range entries {
			var key string
			if entry == nil || json.Unmarshal(entry["name"], &key) != nil || key == "" {
				return in, invalid("条目名称必填")
			}
			for field, value := range entry {
				if field != "name" && field != "value" {
					return in, invalid("条目含未知字段")
				}
				if field == "value" && name == "headers" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
					return in, invalid("Header 值必须为字符串")
				}
			}
		}
	}
	if json.Unmarshal(raw, &in) != nil {
		return in, invalid("模型配置字段类型无效")
	}
	return in, nil
}

func (m *Manager) Edit(ctx context.Context, id string, in ModelConfigEdit) (domain.ModelConfigView, error) {
	var zero domain.ModelConfigView
	if err := m.check(); err != nil {
		return zero, err
	}
	if !ValidID(id) || in.ExpectedRevision < 0 || in.ExpectedRevision >= 1<<53-1 {
		return zero, invalid("配置ID或预期版本无效")
	}
	if err := validateConnection(in.ModelConnection); err != nil {
		return zero, err
	}
	var previous domain.ModelDocument
	if in.ExpectedRevision > 0 {
		r, err := m.repo.GetModelConfig(ctx, id, 0)
		if err != nil {
			return zero, err
		}
		if r.Revision != in.ExpectedRevision {
			return zero, fmt.Errorf("%w：配置已变化，请重新读取", domain.ErrConflict)
		}
		previous, err = m.document(r)
		if err != nil {
			return zero, err
		}
	}
	changed := in.ExpectedRevision > 0 && (previous.BaseURL != in.BaseURL || previous.Protocol != in.Protocol || previous.Auth != in.Auth)
	key := previous.Secrets.APIKey
	if in.APIKey != nil {
		if !validText(*in.APIKey, 8192) {
			return zero, invalid("API key 不得为空白或含控制字符")
		}
		key = *in.APIKey
	} else if changed && key != "" && in.Auth != "none" {
		return zero, invalid("接收端变化后必须重新填写 API key")
	}
	if in.Auth == "none" {
		key = ""
	} else if key == "" {
		return zero, invalid("该认证方式必须提供 API key")
	}
	if len(in.Headers) > 32 || len(in.Parameters) > 32 {
		return zero, invalid("Header 和参数各最多32项")
	}
	headers := map[string]string{}
	for _, entry := range in.Headers {
		name := http.CanonicalHeaderKey(entry.Name)
		if _, exists := headers[name]; exists {
			return zero, invalid("Header 名称重复")
		}
		if entry.Value != nil {
			headers[name] = *entry.Value
		} else {
			old, exists := previous.Secrets.Headers[name]
			if !exists || changed {
				return zero, invalid("新增 Header 或接收端变化时必须填写值")
			}
			headers[name] = old
		}
	}
	parameters := map[string]json.RawMessage{}
	for _, entry := range in.Parameters {
		if _, exists := parameters[entry.Name]; exists {
			return zero, invalid("参数名称重复")
		}
		if entry.Value != nil {
			parameters[entry.Name] = entry.Value
		} else {
			old, exists := previous.Secrets.Parameters[entry.Name]
			if !exists || changed {
				return zero, invalid("新增参数或接收端变化时必须填写值")
			}
			parameters[entry.Name] = old
		}
	}
	// 复用旧保存路径的加密、完整值校验与最终CAS，不在这里发送供应商请求。
	keyChange := domain.SecretChange[string]{Mode: "replace", Value: key}
	if in.Auth == "none" {
		keyChange.Mode = "clear"
	}
	return m.Save(ctx, id, domain.ModelConfigWrite{ModelConnection: in.ModelConnection, ExpectedRevision: in.ExpectedRevision, Enabled: in.Enabled,
		APIKey: keyChange, Headers: domain.SecretChange[map[string]string]{Mode: "replace", Value: headers}, Parameters: domain.SecretChange[map[string]json.RawMessage]{Mode: "replace", Value: parameters}})
}
