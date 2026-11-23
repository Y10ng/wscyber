// Package storetest 是上游原生存储 PostgreSQL 版（overlay 后的
// github.com/chainreactors/cyber/pkg/web/service.SQLiteStore）的契约测试，
// 类型名为补丁最小而保留，实际存储为 PostgreSQL。语义对照上游 store_sqlite_test.go / store_event_proto_test.go（native/POSTGRES.md §2 方案 A）。
//
// 只能经 overlay 运行：
//
//	python3 -I scripts/cyber-go-overlay.py -- test ./pgschema/storetest -count=1
//
// 普通 go test 下上游包没有 Identity/WithSystem 等符号，编译失败（预期，见 §2）。
package storetest
