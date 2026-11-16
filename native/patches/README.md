# Cyber SQLite 事务锁补丁

仅用于固定版本 `github.com/chainreactors/cyber-harness@v0.0.0-20261109041610-f4b1fb2f3315` 的 `pkg/web/service/store_sqlite.go`。

`AppendAOPEvent` 先 SELECT 后 INSERT。默认 deferred 事务在 WAL 下与另一连接写事务竞争时，读事务升级为写事务会立即返回 `SQLITE_BUSY`，`_busy_timeout` 不覆盖这种升级。`_txlock=immediate` 让 modernc SQLite 在事务起点执行 `BEGIN IMMEDIATE`，由既有 5 秒 busy handler 有界等待写锁；事件仍使用原生 SQLiteStore、schema 和持久化路径，不重放事件或请求。

普通 `go build/test` **不会**应用补丁。通过 `python3 -I scripts/cyber-go-overlay.py -- <build|test> [参数...]` 构建或测试；Go 禁止直接替换 GOMODCACHE 文件，因此脚本从固定模块缓存派生只读根模块到 `var/cyber-sqlite-overlay/module-current/`，用派生 modfile 指向该副本，再对副本应用单文件 overlay。测试与构建复用已核对目标源码及 `go.mod` 的稳定副本；不修改模块缓存、上游源码、正式 go.mod 或 go.sum，也不生成历史版本快照。源码版本或目标行变化时脚本会明确失败，不会静默使用未修复代码。共享副本、overlay 和派生 modfile 的写入及 Go 执行均在同一 heavy.lock 排他锁内。
