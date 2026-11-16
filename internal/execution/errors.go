package execution

import (
	"errors"
	"net/url"
	"strings"

	"wscyber/internal/domain"
)

// 只有本包构造的诊断可持久化展示；未知执行适配器错误不得直接回显。
// 不记录请求正文、命令或凭据；保留阶段、退出码和本次容器标识供核对。
type diagnosticError struct {
	message string
	cause   error
}

func (e *diagnosticError) Error() string { return e.message }
func (e *diagnosticError) Unwrap() error { return e.cause }

func diagnostic(err error, input domain.ActionInput) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	redact := func(v string) {
		if v != "" {
			text = strings.ReplaceAll(text, v, "[请求内容已隐藏]")
		}
	}
	redact(input.Command)
	redact(input.Body)
	redact(input.URL)
	if u, e := url.Parse(input.URL); e == nil {
		redact(u.String())
		if u.RawQuery != "" {
			redact(u.RawQuery)
		}
	}
	for _, v := range input.Headers {
		if len(v) >= 4 {
			redact(v)
		}
	}
	if len(text) > 4096 {
		text = text[:4096] + " [诊断已截断]"
	}
	return &diagnosticError{message: text, cause: err}
}
func SafeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var safe *diagnosticError
	if errors.As(err, &safe) {
		return safe.message
	}
	return "执行失败；已保留执行器返回的结果和部分证据，不能视为问题被反证"
}
