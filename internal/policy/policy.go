// Package policy 对平台代发的 HTTP 请求做轻量风险分流，不审批研究意图。
// 这是明确危险信号的启发式检查，不是业务风险证明，也不检查原生工具直连流量。
package policy

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"wscyber/internal/domain"
)

const Version = "request-signals-v1"

type Decision struct {
	Status string // approved（策略放行）或 pending（人工待审），沿用已有存储状态。
	Reason string
}

var (
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	destructive   = regexp.MustCompile(`(?i)(^|[^a-z])(delete|remove|destroy|drop|truncate|purge|erase|wipe|reset|revoke|shutdown|terminate)([^a-z]|$)|删除|清空|销毁|注销`)
)

// Evaluate 仅用于新请求，不能用于恢复时自动放行历史 pending。
// 调用方仍负责输入与范围校验；模型填写的 Reason 不参与放行判断。
func Evaluate(input domain.ActionInput) Decision {
	review := func(reason string) Decision {
		return Decision{Status: "pending", Reason: Version + "：待审，" + reason}
	}
	if input.Kind == "local_command" {
		return Decision{Status: "approved", Reason: Version + "：工具命令默认放行；直连流量未接入请求拦截"}
	}
	if input.Kind != "http" {
		return review("不支持的请求类型")
	}
	if strings.EqualFold(input.Method, "DELETE") {
		return review("DELETE 方法")
	}
	u, err := url.Parse(input.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return review("无法解析 HTTP 目标")
	}
	if dangerousText(u.EscapedPath()) || dangerousText(u.RawQuery) {
		return review("路径或查询包含破坏性操作信号")
	}
	for key, value := range input.Headers {
		switch strings.ToLower(key) {
		case "x-http-method-override", "x-method-override", "x-http-method":
			if dangerousText(value) {
				return review("方法覆盖头包含破坏性操作信号")
			}
		}
	}
	if dangerousText(input.Body) {
		return review("正文包含破坏性操作信号")
	}
	// 解码 JSON 字符串转义，避免只匹配原文本漏掉 delete。
	var body any
	if json.Unmarshal([]byte(input.Body), &body) == nil && dangerousJSON(body) {
		return review("JSON 正文包含破坏性操作信号")
	}
	return Decision{Status: "approved", Reason: Version + "：未命中明确危险信号，默认放行"}
}

func dangerousText(value string) bool {
	// 兼容常见 URL/form 编码；不声称覆盖任意多层编码、压缩或业务私有协议。
	for i := 0; i < 3; i++ {
		if destructive.MatchString(camelBoundary.ReplaceAllString(value, "$1 $2")) {
			return true
		}
		decoded, err := url.QueryUnescape(value)
		if err != nil || decoded == value {
			break
		}
		value = decoded
	}
	return false
}

func dangerousJSON(value any) bool {
	switch v := value.(type) {
	case string:
		return dangerousText(v)
	case []any:
		for _, item := range v {
			if dangerousJSON(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			if dangerousText(key) || dangerousJSON(item) {
				return true
			}
		}
	}
	return false
}
