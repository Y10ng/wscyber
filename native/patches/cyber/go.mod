// 模块边界：本目录是上游 github.com/chainreactors/cyber 的 overlay 替换文件，
// 只经 scripts/cyber-go-overlay.py 以 go -overlay 编入上游包；独立 go.mod 使
// wscyber/native 的 ./... 不把它们当成本模块的包编译。不要在这里运行 go build。
module wscyber/native/patches/cyber

go 1.26.0
