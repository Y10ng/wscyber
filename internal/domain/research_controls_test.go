package domain

import (
	"strings"
	"testing"
)

func TestSupplementValidation(t *testing.T) {
	good := SupplementInput{Source: "人工本机说明", Content: "a\r\nb\t<script>text</script>", ShareWithModel: true}
	if err := ValidateSupplement(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []SupplementInput{{Source: "a", Content: "b"}, {Source: "a", Content: "\x00", ShareWithModel: true}, {Source: "a", Content: "\xff", ShareWithModel: true}, {Source: " ", Content: "b", ShareWithModel: true}, {Source: "a", Content: strings.Repeat("中", 5462), ShareWithModel: true}, {Source: strings.Repeat("a", 257), Content: "b", ShareWithModel: true}} {
		if ValidateSupplement(bad) == nil {
			t.Fatalf("accepted bad input %#v", bad)
		}
	}
	if ValidateSupplement(SupplementInput{Source: "a", Content: strings.Repeat("x", 16384), ShareWithModel: true}) != nil {
		t.Fatal("boundary")
	}
}
