package domain

import (
	"strings"
	"testing"
)

func TestSkillRequestBoundaries(t *testing.T) {
	id := strings.Repeat("a", 32)
	load := "12345678-1234-4234-8234-123456789abc"
	for _, tt := range []struct {
		kind string
		r    SkillRequest
		ok   bool
	}{
		{"skill_discover", SkillRequest{}, true}, {"skill_discover", SkillRequest{Limit: 20, After: id, Query: "%_"}, true},
		{"skill_discover", SkillRequest{Limit: 21}, false}, {"skill_discover", SkillRequest{Query: strings.Repeat("a", 129)}, false},
		{"skill_discover", SkillRequest{Query: "x\x00"}, false}, {"skill_discover", SkillRequest{ItemID: id}, false},
		{"skill_load", SkillRequest{ItemID: id, SourceToken: id, Purpose: "说明"}, true},
		{"skill_load", SkillRequest{ItemID: id, SourceToken: id, Purpose: " "}, false},
		{"skill_load", SkillRequest{ItemID: id, SourceToken: id, Purpose: "说明", Limit: 1}, false},
		{"skill_release", SkillRequest{LoadID: load}, true}, {"skill_release", SkillRequest{LoadID: id}, false},
		{"skill_release", SkillRequest{LoadID: load, ItemID: id}, false}, {"unknown", SkillRequest{}, false},
	} {
		if err := ValidateSkillRequest(tt.kind, tt.r); (err == nil) != tt.ok {
			t.Errorf("%s %#v: %v", tt.kind, tt.r, err)
		}
	}
	for _, bad := range []string{"", id, strings.ToUpper(load), "12345678_1234-4234-8234-123456789abc", load + "a"} {
		if ValidSkillLoadID(bad) {
			t.Fatal(bad)
		}
	}
	var empty *SkillContext
	if empty.LoadIDs() != nil {
		t.Fatal("nil上下文不能携带身份")
	}
}
func TestExplicitSkillClassification(t *testing.T) {
	original := LibraryWrite{Kind: "file", Name: "SKILL.md", Content: "---\nname: method\n---\n文本"}
	if err := ValidateLibraryWrite(original); err != nil {
		t.Fatal(err)
	}
	if original.Classification != "" {
		t.Fatal("名称不能自动改变分类")
	}
	for _, tt := range []struct {
		kind, classification, description string
		ok                                bool
	}{
		{"file", "skill", "", false}, {"file", "skill", "  ", false}, {"file", "skill", "适用说明", true},
		{"file", "material", "", true}, {"file", "auto", "说明", false}, {"folder", "skill", "说明", false},
	} {
		in := original
		in.Kind = tt.kind
		in.Classification = tt.classification
		in.Description = tt.description
		if in.Kind == "folder" {
			in.Content = ""
		}
		if e := ValidateLibraryWrite(in); (e == nil) != tt.ok {
			t.Fatal(tt, e)
		}
	}
}
