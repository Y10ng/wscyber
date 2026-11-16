package domain

import (
	"strings"
	"testing"
)

func TestLibraryWriteBounds(t *testing.T) {
	good := LibraryWrite{Kind: "file", Name: "SKILL.md", Content: "---\nname: local\n---\n资料\r\n"}
	if err := ValidateLibraryWrite(good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*LibraryWrite){
		func(x *LibraryWrite) { x.Name = "../a.md" }, func(x *LibraryWrite) { x.Name = "a\\b.md" }, func(x *LibraryWrite) { x.Name = "a\n.md" }, func(x *LibraryWrite) { x.Name = " a.md" }, func(x *LibraryWrite) { x.Name = "a.html" },
		func(x *LibraryWrite) { x.Content = "" }, func(x *LibraryWrite) { x.Content = "a\x00b" }, func(x *LibraryWrite) { x.Content = string([]byte{255}) }, func(x *LibraryWrite) { x.Content = strings.Repeat("a", MaxMaterialBytes+1) }, func(x *LibraryWrite) { x.Description = strings.Repeat("a", 1025) }, func(x *LibraryWrite) { x.Kind = "folder" }, func(x *LibraryWrite) { x.ParentID = "../" },
	} {
		x := good
		change(&x)
		if ValidateLibraryWrite(x) == nil {
			t.Fatal("无效资料被接收")
		}
	}
	good.Content = strings.Repeat("a", MaxMaterialBytes)
	if err := ValidateLibraryWrite(good); err != nil {
		t.Fatal(err)
	}
	if MaterialFormat("SKILL.MD") != "markdown" || MaterialFormat("a.TXT") != "text" {
		t.Fatal("扩展名解析")
	}
}
func TestResearchSelectionBounds(t *testing.T) {
	id := strings.Repeat("a", 32)
	good := ExplorationInput{Materials: []MaterialSelection{{ID: id, Token: id}}, SafetyPromptEnabled: true, SafetyPrompt: "不要更改任何数据"}
	if err := ValidateResearchSelection(good); err != nil {
		t.Fatal(err)
	}
	if err := ValidateResearchSelection(ExplorationInput{}); err != nil {
		t.Fatal("默认关闭必须有效", err)
	}
	for _, x := range []ExplorationInput{
		{SafetyPromptEnabled: true}, {SafetyPromptEnabled: true, SafetyPrompt: " \n\t"}, {SafetyPrompt: "关闭时不得注入"}, {SafetyPromptEnabled: true, SafetyPrompt: strings.Repeat("a", 8001)},
		{Materials: []MaterialSelection{{ID: id, Token: id}, {ID: id, Token: id}}}, {Materials: []MaterialSelection{{ID: id, Token: "bad"}}}, {Materials: make([]MaterialSelection, 17)},
	} {
		if ValidateResearchSelection(x) == nil {
			t.Fatal("无效选择被接收")
		}
	}
	if ValidLibraryID(strings.ToUpper(id)) || ValidLibraryID(id+"a") {
		t.Fatal("非规范身份")
	}
}
