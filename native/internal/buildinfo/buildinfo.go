// Package buildinfo 汇总本二进制的版本与构建来源；只读，不含任何秘密。
//
// 发布构建以 -ldflags 注入：
//
//	-X wscyber/native/internal/buildinfo.Version=v0.1.0
//	-X wscyber/native/internal/buildinfo.Commit=<git sha>
//	-X wscyber/native/internal/buildinfo.BuildTime=<UTC RFC3339>
//
// 未注入时 Version 为 "dev"，Commit 回落到 Go 自动写入的 vcs.revision（修改过的工作区加 "-dirty"）。
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"

	"wscyber/native/pgschema"
)

// 由 -ldflags -X 注入；-X 只能改写包级字符串变量，故不得改为常量或函数调用初始化。
var (
	Version   = "dev"
	Commit    = ""
	BuildTime = ""
)

// CyberCommit 是固定的上游 cyber-harness 提交（native/DEPENDENCIES.md）。
const CyberCommit = "f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf"

// SchemaVersion 是本二进制期望的数据库迁移版本＝内嵌迁移的最大版本（native/pgschema）。
var SchemaVersion = pgschema.SchemaVersion()

// Info 是 `wscyber-hub version --json` 与 GET /api/wscyber/version 的共同结构。
type Info struct {
	Version     string `json:"version"`
	Commit      string `json:"commit"`
	BuildTime   string `json:"build_time"`
	CyberCommit string `json:"cyber_commit"`
	UICommit    string `json:"ui_commit"`
	// UIManifestSHA256 为嵌入前端清单（.wscyber-ui.json）的 SHA256；未嵌入前端时为空串。
	UIManifestSHA256 string `json:"ui_manifest_sha256"`
	Schema           int    `json:"schema"`
	Go               string `json:"go"`
}

// Get 返回构建信息；uiCommit/uiManifest 由调用方（持有 UI 校验逻辑的入口）提供。
func Get(uiCommit, uiManifest string) Info {
	commit := strings.TrimSpace(Commit)
	if commit == "" {
		commit = vcsCommit()
	}
	version := strings.TrimSpace(Version)
	if version == "" {
		version = "dev"
	}
	return Info{
		Version: version, Commit: commit, BuildTime: strings.TrimSpace(BuildTime),
		CyberCommit: CyberCommit, UICommit: uiCommit, UIManifestSHA256: uiManifest,
		Schema: SchemaVersion, Go: runtime.Version(),
	}
}

func vcsCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision != "" && modified {
		revision += "-dirty"
	}
	return revision
}

// String 是 `wscyber-hub version` 的单行文本。
func (i Info) String() string {
	parts := []string{"commit " + orDash(short(i.Commit))}
	if i.BuildTime != "" {
		parts = append(parts, i.BuildTime)
	}
	ui := "ui " + orDash(short(i.UICommit))
	if i.UIManifestSHA256 != "" {
		ui += " manifest " + short(i.UIManifestSHA256)
	} else {
		ui += " (未嵌入)"
	}
	parts = append(parts, "cyber "+short(i.CyberCommit), ui, fmt.Sprintf("schema %d", i.Schema), i.Go)
	return fmt.Sprintf("wscyber-hub %s (%s)", i.Version, strings.Join(parts, ", "))
}

func short(value string) string {
	dirty := strings.HasSuffix(value, "-dirty")
	value = strings.TrimSuffix(value, "-dirty")
	if len(value) > 8 {
		value = value[:8]
	}
	if dirty {
		value += "-dirty"
	}
	return value
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
