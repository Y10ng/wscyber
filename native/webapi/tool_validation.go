package webapi

import (
	"errors"
	"strings"
)

// toolValidationErrors keeps independent input failures in field order. Nested
// validators share the same ten-line budget; callers must not persist or send
// requests when err is non-nil.
type toolValidationErrors []error

const maxToolValidationErrors = 10

func (v *toolValidationErrors) add(err error) {
	if err == nil || len(*v) >= maxToolValidationErrors {
		return
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			v.add(child)
		}
		return
	}
	*v = append(*v, err)
}

func (v toolValidationErrors) err() error { return errors.Join(v...) }

// fieldInputError exposes only a deliberately constructed validation message,
// while retaining the sentinel used for transaction rollback/error matching.
type fieldInputError struct {
	cause   error
	message string
}

func (e fieldInputError) Error() string { return e.message }
func (e fieldInputError) Unwrap() error { return e.cause }

func requiredRelationError(field, value, relation string) string {
	return "evidence: 当前" + field + "=" + value + "缺少对应relation的完整新HTTP证据；至少提供一项 relation=" + relation + " 的本会话新证据（已发送、收到响应、完整且未截断）；登录失败、不可达、超时不等于已修复，不能判定时使用 inconclusive"
}

// endpointFieldError preserves normalizeEndpoint's rule while attaching the
// exact method/url field rather than only the enclosing array element.
func endpointFieldError(path string, err error) error {
	field, rule, ok := strings.Cut(err.Error(), " ")
	if ok && (field == "method" || field == "url") {
		return errors.New(path + "." + field + ": " + rule)
	}
	return errors.New(path + ": " + err.Error())
}
