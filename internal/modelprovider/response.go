package modelprovider

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SDK以兼容性为先，会把数字转为字符串、忽略部分字段错误。边界层先拒绝
// 重复键、过深与损坏的Unicode，再依据公开Raw/Valid元数据检查所消费字段。
func validResponseJSON(data []byte) bool {
	if !utf8.Valid(data) || !json.Valid(data) || !validUnicodeEscapes(data) {
		return false
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, container := token.(json.Delim)
		if !container {
			return true
		}
		seen := map[string]bool{}
		for d.More() {
			if delim == '{' {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
			}
			if !walk(depth + 1) {
				return false
			}
		}
		_, err = d.Token()
		return err == nil // json.Valid已验证括号配对。
	}
	if !walk(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}

// encoding/json会把孤立UTF-16代理项替换为U+FFFD；不能把被修补的文本当原文成功。
// 调用前已json.Valid，故转义形式和四位十六进制均合法；跳过双反斜杠等转义。
func validUnicodeEscapes(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if data[i] != 'u' {
			continue
		}
		value, _ := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		i += 4
		switch {
		case value >= 0xdc00 && value <= 0xdfff:
			return false
		case value >= 0xd800 && value <= 0xdbff:
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

// 两个官方SDK的公开字段元数据接口相同，不导入其internal包。
type responseField interface {
	Valid() bool
	Raw() string
}

func fieldKind(field responseField, first byte) bool {
	raw := strings.TrimSpace(field.Raw())
	return field.Valid() && len(raw) > 0 && raw[0] == first
}

func absentOrNull(field responseField) bool {
	raw := strings.TrimSpace(field.Raw())
	return raw == "" || raw == "null"
}
