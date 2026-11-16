package modelprobe

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"

	"wscyber/internal/domain"
)

// DecodeConfirmation 只接受完整的显式确认，不接受提示词/项目材料或客户端状态。
func DecodeConfirmation(raw []byte) (int64, error) {
	if len(raw) > 1024 || !utf8.Valid(raw) {
		return 0, domain.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return 0, domain.ErrInvalid
	}
	seen := map[string]bool{}
	var revision int64
	var confirm bool
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return 0, domain.ErrInvalid
		}
		seen[key] = true
		switch key {
		case "config_revision":
			err = d.Decode(&revision)
		case "confirm_billable":
			err = d.Decode(&confirm)
		default:
			return 0, domain.ErrInvalid
		}
		if err != nil {
			return 0, domain.ErrInvalid
		}
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') || len(seen) != 2 || !confirm || revision < 1 || revision > 1<<53-1 {
		return 0, domain.ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return 0, domain.ErrInvalid
	}
	return revision, nil
}
