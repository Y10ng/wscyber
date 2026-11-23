package buildinfo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInjectedAndFallback(t *testing.T) {
	oldVersion, oldCommit, oldTime := Version, Commit, BuildTime
	t.Cleanup(func() { Version, Commit, BuildTime = oldVersion, oldCommit, oldTime })
	Version, Commit, BuildTime = "v0.1.0", "abcdef0123456789", "2026-12-01T00:00:00Z"
	info := Get("147423bd42b665aab361409c57258d9370f75bb0", strings.Repeat("a", 64))
	want := "wscyber-hub v0.1.0 (commit abcdef01, 2026-12-01T00:00:00Z, cyber f4b1fb2f, ui 147423bd manifest aaaaaaaa, schema 2, "
	if !strings.HasPrefix(info.String(), want) || !strings.HasSuffix(info.String(), info.Go+")") {
		t.Fatalf("text: %s", info.String())
	}
	raw, _ := json.Marshal(info)
	for _, field := range []string{`"version":"v0.1.0"`, `"commit":"abcdef0123456789"`, `"build_time":`, `"cyber_commit":"f4b1fb2f`, `"ui_commit":`, `"ui_manifest_sha256":"aaaa`, `"schema":2`, `"go":"go`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("json lacks %s: %s", field, raw)
		}
	}
	Version, Commit, BuildTime = "", "", ""
	info = Get("", "")
	if info.Version != "dev" || info.BuildTime != "" || !strings.Contains(info.String(), "(未嵌入)") {
		t.Fatalf("fallback: %+v", info)
	}
	if short("0123456789-dirty") != "01234567-dirty" {
		t.Fatal("dirty suffix lost")
	}
}
