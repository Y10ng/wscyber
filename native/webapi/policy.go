package webapi

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

const requestPolicyVersion = "request-signals-v1"

var (
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	destructive   = regexp.MustCompile(`(?i)(^|[^a-z])(delete|remove|destroy|drop|truncate|purge|erase|wipe|reset|revoke|shutdown|terminate)([^a-z]|$)|删除|清空|销毁|注销`)
)

// requestReview 仅移植G0明确破坏信号；不宣称覆盖全部业务副作用。
func requestReview(method, rawURL string, headers map[string]string, body []byte) string {
	if strings.EqualFold(method, "DELETE") {
		return "DELETE 方法"
	}
	u, err := url.Parse(rawURL)
	if err != nil || dangerousText(u.EscapedPath()) || dangerousText(u.RawQuery) {
		return "路径或查询包含破坏性操作信号"
	}
	for key, value := range headers {
		switch strings.ToLower(key) {
		case "x-http-method-override", "x-method-override", "x-http-method":
			if dangerousText(value) {
				return "方法覆盖头包含破坏性操作信号"
			}
		}
	}
	if dangerousText(string(body)) {
		return "正文包含破坏性操作信号"
	}
	var decoded any
	if json.Unmarshal(body, &decoded) == nil && dangerousJSON(decoded) {
		return "JSON 正文包含破坏性操作信号"
	}
	return ""
}

func dangerousText(value string) bool {
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
