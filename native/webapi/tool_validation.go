package webapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
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

// unknownToolFields only formats a DisallowUnknownFields failure, never adds
// validation to tools that intentionally use permissive JSON decoding. A second
// read of the bounded payload recovers array indices omitted by encoding/json.
func unknownToolFields(arguments string, target any) error {
	var value any
	if json.Unmarshal([]byte(arguments), &value) != nil {
		return nil
	}
	var problems toolValidationErrors
	var visit func(any, reflect.Type, string)
	visit = func(value any, typ reflect.Type, path string) {
		if len(problems) >= maxToolValidationErrors {
			return
		}
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		switch typ.Kind() {
		case reflect.Slice, reflect.Array:
			if items, ok := value.([]any); ok {
				for i, item := range items {
					visit(item, typ.Elem(), fmt.Sprintf("%s[%d]", path, i))
				}
			}
		case reflect.Struct:
			object, ok := value.(map[string]any)
			if !ok {
				return
			}
			keys := make([]string, 0, len(object))
			for key := range object {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				var fieldType reflect.Type
				for i := 0; i < typ.NumField(); i++ {
					field := typ.Field(i)
					if !field.IsExported() {
						continue
					}
					name := strings.Split(field.Tag.Get("json"), ",")[0]
					if name == "-" {
						continue
					}
					if name == "" {
						name = field.Name
					}
					if strings.EqualFold(name, key) {
						fieldType = field.Type
						break
					}
				}
				fieldPath := joinJSONFieldPath(path, key)
				if fieldType == nil {
					problems.add(fmt.Errorf("%s: 参数不是strict JSON：含未知字段 %q；请删除此字段，按工具定义提供字段", fieldPath, key))
				} else {
					visit(object[key], fieldType, fieldPath)
				}
			}
		case reflect.Map:
			if object, ok := value.(map[string]any); ok {
				keys := make([]string, 0, len(object))
				for key := range object {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					visit(object[key], typ.Elem(), fmt.Sprintf("%s[%q]", path, key))
				}
			}
		}
	}
	visit(value, reflect.TypeOf(target), "")
	return problems.err()
}
