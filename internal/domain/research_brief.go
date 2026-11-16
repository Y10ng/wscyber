package domain

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxBriefIdentities = 8
	MaxBriefTextBytes  = 16384
	MaxBriefJSONBytes  = 32768
)

// ResearchBrief 是创建时固定的非秘密用户声明，不是认证状态、已验证事实或授权。
// 指针区分必填布尔的缺省/null与显式false；只有明确true可以进入模型上下文。
type ResearchBrief struct {
	Identities     []ResearchIdentity `json:"identities,omitempty"`
	BusinessRules  string             `json:"business_rules,omitempty"`
	Preconditions  string             `json:"preconditions,omitempty"`
	ShareWithModel *bool              `json:"share_with_model"`
}
type ResearchIdentity struct {
	Label     string `json:"label"`
	Role      string `json:"role"`
	Ownership string `json:"ownership,omitempty"`
}

func (b *ResearchBrief) Shared() bool {
	return b != nil && b.ShareWithModel != nil && *b.ShareWithModel
}
func (b *ResearchBrief) Clone() *ResearchBrief {
	if b == nil {
		return nil
	}
	out := *b
	out.Identities = slices.Clone(b.Identities)
	if b.ShareWithModel != nil {
		share := *b.ShareWithModel
		out.ShareWithModel = &share
	}
	return &out
}

func ValidateResearchBrief(b *ResearchBrief) error {
	if b == nil {
		return nil
	}
	bad := func(message string) error { return fmt.Errorf("%w：%s", ErrInvalid, message) }
	if b.ShareWithModel == nil {
		return bad("身份说明须明确选择是否提供给研究模型")
	}
	if len(b.Identities) > MaxBriefIdentities {
		return bad("身份说明最多8项")
	}
	total := len(b.BusinessRules) + len(b.Preconditions)
	seen := map[string]bool{}
	for _, item := range b.Identities {
		label := strings.TrimSpace(item.Label)
		if label == "" || strings.TrimSpace(item.Role) == "" || !briefText(item.Label, 128, false) || !briefText(item.Role, 512, false) || !briefText(item.Ownership, 1024, false) {
			return bad("身份别名和角色必填；单行UTF-8文本上限分别为128、512、1024字节，不含控制字符")
		}
		if seen[label] {
			return bad("身份别名去首尾空白后不能重复")
		}
		seen[label] = true
		total += len(item.Label) + len(item.Role) + len(item.Ownership)
	}
	if !briefText(b.BusinessRules, 8192, true) || !briefText(b.Preconditions, 4096, true) {
		return bad("业务规则和复现前提须为UTF-8文本，上限分别为8192、4096字节；控制字符仅允许TAB、LF和CRLF")
	}
	if len(b.Identities) == 0 && strings.TrimSpace(b.BusinessRules) == "" && strings.TrimSpace(b.Preconditions) == "" {
		return bad("身份说明与业务前提不能全空")
	}
	if total > MaxBriefTextBytes {
		return bad("身份说明与业务前提合计最多16384字节")
	}
	raw, err := json.Marshal(b)
	if err != nil || len(raw) > MaxBriefJSONBytes {
		return bad("身份说明编码后最多32768字节")
	}
	return nil
}

func briefText(s string, max int, multiline bool) bool {
	if !utf8.ValidString(s) || len(s) > max {
		return false
	}
	for i, r := range s {
		if !multiline && (r == 0x2028 || r == 0x2029) {
			return false
		}
		if !unicode.IsControl(r) {
			continue
		}
		if multiline && (r == '\t' || r == '\n' || r == '\r' && i+1 < len(s) && s[i+1] == '\n') {
			continue
		}
		return false
	}
	return true
}
