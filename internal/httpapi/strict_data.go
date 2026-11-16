package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"wscyber/internal/domain"
)

// 新增文本/配置契约拒绝重复键、null、大小写别名和非法Unicode，不改变旧API解析语义。
func decodeStrictData(w http.ResponseWriter, r *http.Request, dst any) error {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return domain.ErrInvalid
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil || !utf8.Valid(raw) || !validJSONUnicode(raw) {
		return domain.ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err = checkDataValue(dec, reflect.TypeOf(dst).Elem(), 0); err != nil {
		return err
	}
	if _, err = dec.Token(); err != io.EOF {
		return domain.ErrInvalid
	}
	dec = json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		return domain.ErrInvalid
	}
	return nil
}
func checkDataValue(d *json.Decoder, t reflect.Type, depth int) error {
	if depth > 12 {
		return domain.ErrInvalid
	}
	tok, err := d.Token()
	if err != nil || tok == nil {
		return domain.ErrInvalid
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if tok != json.Delim('{') {
			return domain.ErrInvalid
		}
		allowed := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			key := strings.Split(f.Tag.Get("json"), ",")[0]
			if key != "" && key != "-" {
				allowed[key] = f.Type
			}
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return domain.ErrInvalid
			}
			k, ok := key.(string)
			field, allowedKey := allowed[k]
			if !ok || !allowedKey || seen[k] {
				return domain.ErrInvalid
			}
			seen[k] = true
			if err = checkDataValue(d, field, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return domain.ErrInvalid
		}
	case reflect.Slice:
		if tok != json.Delim('[') {
			return domain.ErrInvalid
		}
		for d.More() {
			if err = checkDataValue(d, t.Elem(), depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return domain.ErrInvalid
		}
	case reflect.String:
		if _, ok := tok.(string); !ok {
			return domain.ErrInvalid
		}
	case reflect.Int, reflect.Int64:
		n, ok := tok.(json.Number)
		if !ok {
			return domain.ErrInvalid
		}
		v, err := strconv.ParseInt(string(n), 10, t.Bits())
		if err != nil || strconv.FormatInt(v, 10) != string(n) {
			return domain.ErrInvalid
		}
	case reflect.Bool:
		if _, ok := tok.(bool); !ok {
			return domain.ErrInvalid
		}
	default:
		return domain.ErrInvalid
	}
	return nil
}

// encoding/json会以替代字符接收未配对UTF-16转义；文本原件不能悄悄改字节。
func validJSONUnicode(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		i++
		for i < len(raw) && raw[i] != '"' {
			if raw[i] != '\\' {
				i++
				continue
			}
			i++
			if i >= len(raw) {
				return false
			}
			if raw[i] != 'u' {
				i++
				continue
			}
			if i+4 >= len(raw) {
				return false
			}
			v, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if err != nil {
				return false
			}
			i += 5
			if v >= 0xDC00 && v <= 0xDFFF {
				return false
			}
			if v >= 0xD800 && v <= 0xDBFF {
				if i+5 >= len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
					return false
				}
				low, err := strconv.ParseUint(string(raw[i+2:i+6]), 16, 16)
				if err != nil || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				i += 6
			}
		}
	}
	return true
}
