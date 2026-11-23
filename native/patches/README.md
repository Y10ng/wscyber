# 上游 Cyber overlay（清单驱动整文件替换）

仅用于固定版本 `github.com/chainreactors/cyber-harness@v0.0.0-20261109041610-f4b1fb2f3315`（模块名 `github.com/chainreactors/cyber`）。设计见 [`../POSTGRES.md`](../POSTGRES.md) §2 方案 A。

## 内容

`cyber/manifest.json` 列出每个被替换（或新增）的上游文件：

| 字段 | 含义 |
|---|---|
| `upstream` | 上游根模块内的相对路径 |
| `sha256` | 固定版本上游源文件的 sha256；`null` 表示上游不存在、本项目新增 |
| `replacement` | 替换文件，相对 `cyber/` |

当前替换：`pkg/web/service/store_sqlite.go`、`store_schema.go`、`store_models.go` 换成 PostgreSQL 实现（保留 `SQLiteStore`、`NewSQLiteStore`、`orm`、`appendAOPEvent`、`archiveArtifactEvents`、各 `*Model` 等全部标识符，上游其它文件一行不改；名字里的 SQLite 只是为了补丁最小），新增 `store_pg_identity.go`（`Identity`、`WithIdentity`、`IdentityFrom`、`WithSystem`）。存储不建表、不迁移，只校验 `wscyber_migrations` 已有版本、声明的表/列都存在；迁移由 Hub 启动时的 `pgschema.Start` 负责。契约测试在 `native/pgschema/storetest/`。

`cyber/go.mod` 只是模块边界：让 `wscyber/native` 的 `./...` 不把替换文件当本模块的包编译。不要在 `cyber/` 里运行 go 命令。

## 使用

普通 `go build/test` **不会**应用 overlay（且引用 `Identity` 等新增符号的代码会编译失败，不会静默用上游原实现）。一律通过：

```
python3 -I scripts/cyber-go-overlay.py -- <build|test|vet|list> [go 参数...]
```

脚本流程（全程持 `var/heavy.lock` 排他锁）：

1. 读清单；`go env GOMODCACHE` 下找到固定上游模块，逐个核对 `sha256`（`null` 项要求上游不存在该文件）。**任何不符立即失败**，不应用 overlay。
2. Go 禁止 overlay 替换 GOMODCACHE 下的文件：首次把上游根模块复制为只读副本 `var/cyber-overlay/module-current/`（来源版本记在 `module-current.source`），之后每次复核副本的 `go.mod` 与清单文件哈希。
3. 写 `var/cyber-overlay/overlay.json`（副本路径 → 替换文件）与派生 modfile（正式 `go.mod` 的副本 + `replace github.com/chainreactors/cyber => 副本`），以 `-mod=readonly -modfile -overlay` 运行 go。

不修改模块缓存、上游源码、正式 `go.mod`/`go.sum`。

## 上游升级

上游三文件变化时脚本会报哈希不符：对旧上游、新上游、本目录替换文件做三方 diff，重新移植后更新清单哈希，再跑 `cyber-go-overlay.py -- test ./pgschema/storetest -count=1`。`--manifest PATH` 可指定另一份清单（演示/调试用）。
