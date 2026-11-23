// Package uiembed 把发布构建放入 public/ 的原生前端产物嵌入二进制。
//
// public/ 只提交 .placeholder（git 忽略其余内容），因此未放入前端时仍可编译，
// 此时 FS 只含占位文件，入口视为“未嵌入前端”。发布构建把
// cyber-ui-build.py 的 release/public/ 整体复制进 public/ 后再 go build；
// 清单与逐文件哈希校验由 Hub 入口负责（见 cmd/wscyber-hub/ui.go）。
package uiembed

import (
	"embed"
	"io/fs"
)

// Placeholder 是 public/ 中唯一受版本控制的文件，校验时忽略。
const Placeholder = ".placeholder"

//go:embed all:public
var files embed.FS

// FS 返回以 public/ 为根的只读文件系统。
func FS() fs.FS {
	sub, err := fs.Sub(files, "public")
	if err != nil {
		panic(err) // "public" 是编译期固定的有效路径，不会失败。
	}
	return sub
}
