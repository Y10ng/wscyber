package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func briefFixture() *ResearchBrief {
	share := false
	return &ResearchBrief{Identities: []ResearchIdentity{{Label: " 成员A ", Role: "普通成员", Ownership: "本人记录"}}, BusinessRules: "待核验\r\n规则\t第二行\n", ShareWithModel: &share}
}
func TestResearchBriefValidation(t *testing.T) {
	if ValidateResearchBrief(nil) != nil {
		t.Fatal("省略必须兼容")
	}
	good := briefFixture()
	if err := ValidateResearchBrief(good); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*ResearchBrief){
		"missing_choice": func(b *ResearchBrief) { b.ShareWithModel = nil },
		"empty":          func(b *ResearchBrief) { b.Identities = nil; b.BusinessRules = " \n\t" },
		"count":          func(b *ResearchBrief) { b.Identities = make([]ResearchIdentity, 9) },
		"duplicate": func(b *ResearchBrief) {
			b.Identities = append(b.Identities, ResearchIdentity{Label: "成员A", Role: "另一角色"})
		},
		"label_required":      func(b *ResearchBrief) { b.Identities[0].Label = "　" },
		"role_required":       func(b *ResearchBrief) { b.Identities[0].Role = "" },
		"label_bytes":         func(b *ResearchBrief) { b.Identities[0].Label = strings.Repeat("中", 43) },
		"role_bytes":          func(b *ResearchBrief) { b.Identities[0].Role = strings.Repeat("a", 513) },
		"ownership_bytes":     func(b *ResearchBrief) { b.Identities[0].Ownership = strings.Repeat("a", 1025) },
		"rules_bytes":         func(b *ResearchBrief) { b.BusinessRules = strings.Repeat("a", 8193) },
		"preconditions_bytes": func(b *ResearchBrief) { b.Preconditions = strings.Repeat("a", 4097) },
		"invalid_utf8":        func(b *ResearchBrief) { b.BusinessRules = string([]byte{255}) },
		"nul":                 func(b *ResearchBrief) { b.Preconditions = "a\x00b" },
		"c1_control":          func(b *ResearchBrief) { b.Preconditions = "a\u0085b" },
		"bare_cr":             func(b *ResearchBrief) { b.Preconditions = "a\rb" },
		"linebreak":           func(b *ResearchBrief) { b.Identities[0].Label = "a\nb" },
		"unicode_linebreak":   func(b *ResearchBrief) { b.Identities[0].Ownership = "a b" },
		"aggregate": func(b *ResearchBrief) {
			b.BusinessRules = strings.Repeat("a", 8192)
			b.Preconditions = strings.Repeat("b", 4096)
			b.Identities = nil
			for _, l := range []string{"a", "b", "c"} {
				b.Identities = append(b.Identities, ResearchIdentity{Label: l, Role: strings.Repeat("r", 512), Ownership: strings.Repeat("o", 1024)})
			}
		},
		"escaped_budget": func(b *ResearchBrief) { b.BusinessRules = strings.Repeat("<", 6000) },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			b := good.Clone()
			change(b)
			err := ValidateResearchBrief(b)
			if !errors.Is(err, ErrInvalid) {
				t.Fatal("须拒绝无效说明", err)
			}
		})
	}
	for _, text := range []string{"a\r\nb\nc\td", "😀", string(rune(0xFEFF)), strings.Repeat("a", 8192)} {
		b := good.Clone()
		b.BusinessRules = text
		if err := ValidateResearchBrief(b); err != nil {
			t.Fatal(err)
		}
	}
	b := good.Clone()
	b.Identities[0].Label = strings.Repeat("a", 128)
	b.Identities[0].Role = strings.Repeat("r", 512)
	b.Identities[0].Ownership = strings.Repeat("o", 1024)
	b.Preconditions = strings.Repeat("p", 4096)
	if err := ValidateResearchBrief(b); err != nil {
		t.Fatal(err)
	}
	b = good.Clone()
	b.Identities[0].Label = "changed"
	*b.ShareWithModel = true
	if good.Shared() || good.Identities[0].Label != " 成员A " {
		t.Fatal("克隆必须独立")
	}
}
func TestResearchBriefOldEncodingAndIdentity(t *testing.T) {
	old := ExplorationInput{Description: "a", Goal: "b", AllowedOrigins: []string{"https://fixture.invalid"}}
	raw, _ := json.Marshal(old)
	if string(raw) != `{"description":"a","goal":"b","allowed_origins":["https://fixture.invalid"]}` {
		t.Fatal("省略字段改变旧指纹")
	}
	old.ResearchBrief = briefFixture()
	raw, _ = json.Marshal(old)
	if !strings.Contains(string(raw), `"share_with_model":false`) || !strings.Contains(string(raw), `"label":" 成员A "`) {
		t.Fatal("不得丢失显式false或改写文本")
	}
}
