package modelconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"wscyber/internal/domain"
)

var (
	idPattern        = regexp.MustCompile(`^[0-9a-f]{32}$`)
	parameterPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	headerPattern    = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
)

func ValidID(id string) bool       { return idPattern.MatchString(id) }
func invalid(message string) error { return fmt.Errorf("%w：%s", domain.ErrInvalid, message) }

func validText(value string, max int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= max && utf8.ValidString(value) && !hasControl(value)
}
func hasControl(value string) bool {
	for _, r := range value {
		if r < 32 || r == 127 {
			return true
		}
	}
	return false
}

// ValidateForUse 在实际调用前重新验证完整内部配置，不提供秘密读回API。
// 返回独立的规范化Header；调用方不能通过绕过管理API覆盖协议保留字段。
func ValidateForUse(doc domain.ModelDocument) (domain.ModelDocument, error) {
	if err := validateConnection(doc.ModelConnection); err != nil {
		return domain.ModelDocument{}, err
	}
	key := doc.Secrets.APIKey
	if (doc.Auth == "none" && key != "") || (doc.Auth != "none" && !validText(key, 8192)) {
		return domain.ModelDocument{}, invalid("模型认证凭据缺失或无效")
	}
	headers, err := normalizeHeaders(doc.Secrets.Headers)
	if err != nil {
		return domain.ModelDocument{}, err
	}
	if err := validateParameters(doc.Secrets.Parameters); err != nil {
		return domain.ModelDocument{}, err
	}
	doc.Secrets.Headers = headers
	return doc, nil
}

func validateConnection(c domain.ModelConnection) error {
	if !validText(c.Name, 120) || !validText(c.Model, 200) {
		return invalid("配置名称和模型标识必填且不得包含控制字符")
	}
	if c.Protocol != "openai_chat" && c.Protocol != "anthropic_messages" {
		return invalid("不支持的模型协议")
	}
	if c.Auth != "none" && c.Auth != "bearer" && c.Auth != "api_key" {
		return invalid("认证方式必须为 none、bearer 或 api_key")
	}
	if c.Protocol == "openai_chat" && c.Auth == "api_key" {
		return invalid("OpenAI Chat 协议请使用 bearer 或 none 认证")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 || c.MaxOutputTokens < 1 || c.MaxOutputTokens > 32768 {
		return invalid("模型超时须为1–600秒，输出上限须为1–32768 tokens")
	}
	return validateEndpoint(c.BaseURL)
}

func validateEndpoint(raw string) error {
	bad := func() error {
		return invalid("模型地址须为无凭据、查询和片段的 HTTPS 地址；HTTP 仅支持字面 loopback 地址")
	}
	if !validText(raw, 2048) || strings.ContainsAny(raw, "\\?#%") {
		return bad()
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return bad()
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return bad()
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return bad()
	}
	host := u.Hostname()
	ip, ipErr := netip.ParseAddr(host)
	if ipErr == nil {
		ip = ip.Unmap()
		if ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return bad()
		}
	} else {
		if len(host) > 253 || strings.Contains(host, ":") {
			return bad()
		}
		for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return bad()
			}
			for _, ch := range label {
				if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-') {
					return bad()
				}
			}
		}
	}
	if u.Scheme == "http" && (ipErr != nil || !ip.Unmap().IsLoopback()) {
		return bad()
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return bad()
		}
	}
	if strings.Contains(u.Path, "//") {
		return bad()
	}
	// net/url 允许某些非网络形式；再次检查完整主机/端口可被标准客户端解释。
	if u.Port() != "" {
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			return bad()
		}
	}
	return nil
}

var reservedHeaders = map[string]bool{
	"host": true, "authorization": true, "proxy-authorization": true, "proxy-authenticate": true,
	"cookie": true, "set-cookie": true, "x-api-key": true, "api-key": true,
	"connection": true, "keep-alive": true, "transfer-encoding": true, "te": true, "trailer": true,
	"upgrade": true, "expect": true, "accept": true, "accept-encoding": true, "origin": true,
	"referer": true, "forwarded": true, "anthropic-version": true, "idempotency-key": true,
}

func normalizeHeaders(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	if len(in) > 32 {
		return nil, invalid("自定义 Header 最多32项")
	}
	total := 0
	for name, value := range in {
		lower := strings.ToLower(name)
		canonical := http.CanonicalHeaderKey(name)
		_, duplicate := out[canonical]
		if len(name) > 128 || !headerPattern.MatchString(name) || duplicate || reservedHeaders[lower] || strings.HasPrefix(lower, "content-") || strings.HasPrefix(lower, "proxy-") || strings.HasPrefix(lower, "x-forwarded-") || strings.HasPrefix(lower, "x-stainless-") || strings.HasPrefix(lower, "sec-") || strings.HasPrefix(lower, "x-amz-") {
			return nil, invalid("自定义 Header 名称无效、重复或属于保留字段")
		}
		if len(value) > 8192 || !utf8.ValidString(value) || hasControl(value) {
			return nil, invalid("自定义 Header 值过长或含控制字符")
		}
		total += len(name) + len(value)
		out[canonical] = value
	}
	if total > 32<<10 {
		return nil, invalid("自定义 Header 总大小超过32 KiB")
	}
	return out, nil
}

var reservedParameters = map[string]bool{
	"model": true, "messages": true, "system": true, "prompt": true, "input": true, "instructions": true,
	"tools": true, "tool_choice": true, "functions": true, "function_call": true, "parallel_tool_calls": true,
	"stream": true, "stream_options": true, "n": true, "max_tokens": true, "max_completion_tokens": true, "max_output_tokens": true,
	"response_format": true, "output_config": true, "output_format": true, "stop": true, "stop_sequences": true,
	"extra_body": true, "extra_headers": true, "extra_query": true, "api_key": true, "base_url": true, "timeout": true,
	"conversation": true, "previous_response_id": true, "background": true, "store": true,
	"mcp_servers": true, "container": true, "skills": true, "plugins": true,
	"user_profile_id": true, "workspace_id": true, "context_management": true,
}

func validateParameters(parameters map[string]json.RawMessage) error {
	if len(parameters) > 32 {
		return invalid("扩展参数最多32项")
	}
	total := 0
	for name, value := range parameters {
		if !parameterPattern.MatchString(name) || reservedParameters[name] {
			return invalid("扩展参数名称无效或尝试覆盖协议保留字段")
		}
		if err := validateJSON(value); err != nil {
			return err
		}
		total += len(name) + len(value)
	}
	if total > 32<<10 {
		return invalid("扩展参数总大小超过32 KiB")
	}
	return nil
}

// validateJSON 拒绝重复键、过深和尾随内容，不把原文包含在错误中。
func validateJSON(raw []byte) error {
	if !utf8.Valid(raw) || len(raw) == 0 || len(raw) > maxDocumentBytes {
		return invalid("JSON 大小或编码无效")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 16 {
			return invalid("JSON 嵌套超过16层")
		}
		token, err := d.Token()
		if err != nil {
			return invalid("JSON 格式无效")
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				token, err := d.Token()
				if err != nil {
					return invalid("JSON 对象无效")
				}
				key, ok := token.(string)
				if !ok || seen[key] {
					return invalid("JSON 对象包含重复字段")
				}
				seen[key] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return invalid("JSON 结束符无效")
		}
		if _, err := d.Token(); err != nil {
			return invalid("JSON 不完整")
		}
		return nil
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return invalid("只能提交一个 JSON 值")
	}
	return nil
}

func DecodeDefault(raw []byte) (string, int64, error) {
	if err := validateJSON(raw); err != nil {
		return "", 0, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 2 {
		return "", 0, invalid("默认模型需要完整的ID与预期版本")
	}
	for _, name := range []string{"default_config_id", "expected_revision"} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return "", 0, invalid("默认模型字段缺失或为 null")
		}
	}
	var id string
	var revision int64
	if json.Unmarshal(fields["default_config_id"], &id) != nil || json.Unmarshal(fields["expected_revision"], &revision) != nil {
		return "", 0, invalid("默认模型字段类型无效")
	}
	return id, revision, nil
}

// DecodeWrite 除拒绝重复键外还拒绝大小写别名、缺字段和 null，避免凭据修改歧义。
func DecodeWrite(raw []byte) (domain.ModelConfigWrite, error) {
	var in domain.ModelConfigWrite
	if err := validateJSON(raw); err != nil {
		return in, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return in, invalid("模型配置必须为对象")
	}
	want := strings.Fields("name protocol base_url model auth timeout_seconds max_output_tokens expected_revision enabled api_key headers parameters")
	if len(fields) != len(want) {
		return in, invalid("模型配置字段不完整或含未知字段")
	}
	for _, name := range want {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return in, invalid("模型配置缺少必填字段")
		}
	}
	for _, name := range []string{"api_key", "headers", "parameters"} {
		var patch map[string]json.RawMessage
		if err := json.Unmarshal(fields[name], &patch); err != nil || patch == nil {
			return in, invalid("秘密修改必须为 mode/value 对象")
		}
		var mode string
		if err := json.Unmarshal(patch["mode"], &mode); err != nil {
			return in, invalid("秘密修改缺少 mode")
		}
		if mode == "replace" {
			value, ok := patch["value"]
			if len(patch) != 2 || !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return in, invalid("replace 必须提供非 null 的 value")
			}
		} else if (mode != "keep" && mode != "clear") || len(patch) != 1 {
			return in, invalid("keep/clear 不得提供 value")
		}
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, invalid("模型配置字段类型无效")
	}
	return in, nil
}
