# PostgreSQL 迁移与多租户骨架：设计（P0）

状态：设计稿（2026-11-22，P0）。只写方案，不含实现。用户决定（11-22）：全部存储迁 PostgreSQL，迁完删 SQLite（驱动、代码、schema 阶段机制、补丁），不写兼容层；全新开始、不导入旧数据；共享库 + 每表 `tenant_id` + RLS；两级角色（管理员 / 普通用户）；连接串由外部提供，开发/演示/测试用本机 PostgreSQL 18。

上游路径相对固定版本 `cyber-harness@f4b1fb2f`（只读副本 `/root/桌面/cyber-harness`）；本项目路径相对仓库根。行号以本文写作时的 HEAD `692d5b6` 为准。

---

## 0. 调查结论（事实）

### 0.1 上游 Hub 原生存储

| 事实 | 位置 |
|---|---|
| 唯一实现是具体类型 `*SQLiteStore`，驱动 `modernc.org/sqlite`、方言 `sqlitedialect`，`SetMaxOpenConns(1)` | `pkg/web/service/store_sqlite.go:18,23,26-39` |
| 构造 `NewSQLiteStore(dbPath, modules...)`：空库按 bun 模型建表，非空库严格校验表/列完全一致（无迁移）；再校验 `PRAGMA foreign_keys` | `store_sqlite.go:32-53,68-80` |
| 方法：会话 7（Create/Get/List/ListSessionPage/Update/Delete/attachSessionScans）、事件 8（Add/Append/append/ListAOPEvents/turnTerminalCursor/HasTurnEnded/MaxAOPEventSeq/ListAOPEventPage/ListAOPEventsAfter）、请求账本 2、制品 2（Sync/archive）、Close；`scan_store.go` 另 9 个（Create/Get/List/Update/TransitionScan/Delete/LinkScanToSession/SessionScanIDs/ScanSessionIDs） | `store_sqlite.go:100-627`、`scan_store.go:61-226` |
| SQLite 专有写法：`instr(lower(title), lower(?))`（会话搜索） | `store_sqlite.go:214,216` |
| 事件游标 = 同会话 `COALESCE(MAX(cursor),0)+1`，在事务内先查后插；正确性依赖进程内 `Service.eventMu` 串行（`acceptAOPEvent` 持锁） | `store_sqlite.go:371-406`、`events.go:50-51,88` |
| 制品表 `cursor` 自增主键，`ON CONFLICT (event_id) DO NOTHING` | `store_models.go:51-56`、`store_sqlite.go:622` |
| bun 标签 `type:blob`（event_proto ×2、request_hash）——PG 无 `blob` 类型，标签必须改 | `store_models.go:33,43,53` |
| `SchemaModule{Name,Models,Indexes,Tables}`；`coreSchema` 4 表；`schemaTables` 读 `sqlite_master`、`schemaColumns` 用 `PRAGMA table_info` | `store_schema.go:15-20,40-62,146-180` |
| 存储接口只在 API 层抽象：`SessionStore`（含 `RequestLedger`）、`ArtifactStore`；可选接口 `HasTurnEnded` | `pkg/web/api/session.go:29-43,413`、`api/artifact.go:19-21` |
| 但服务层写死具体类型：`ServiceConfig.Store *SQLiteStore`、`Service.store *SQLiteStore`、`NewAgentPool(hub, store *SQLiteStore)`；且直接用未导出成员 `s.store.orm`、`appendAOPEvent`、`archiveArtifactEvents` | `service/service.go:30,54,99-101`、`agents.go:213,222`、`events.go:88,170`、`agents_mux.go:262` |
| `exts/web.Extension.Load` 内写死 `webservice.NewSQLiteStore(e.config.Database, modules...)`；`Config.Database string` 只校验非空 | `exts/web/extension.go:39,54,89-91,103` |
| `host.Serve` 不允许注入存储：`webext.New(config.Management)` 后由扩展自建；`Logger.Infof` 打印 AccessKey | `pkg/web/host/host.go:21-32,39,64` |
| 大量上游存储调用使用 `context.Background()`（事件持久化、GetSession、扫描状态） | `events.go:70,88,93,128,165,175,273`、`commands.go:150,227`、`session.go:230`、`agents_mux.go:262`、`api/session.go:243` |
| 唯一会话创建点 `api.Sessions.OpenSession → store.CreateSession(ctx, …)`（ctx 为请求或调用方 ctx） | `api/session.go:236` |
| `NewService` 内写死 `auth: NewAuth(cfg.AccessKey)`，Auth 是单访问密钥（bearer 或 cookie `cyber_session`），`/api/auth/agent-token` 把密钥发给浏览器 | `service/service.go:85`、`service/auth.go:14-133` |
| `web.Auth` 接口 4 方法；`web.NewHandler` 用 `auth.Middleware(mux)` 包住全部路由（ConnectRPC/WebSocket 都在 mux 内，Middleware 设置的 ctx 值可传到处理器） | `pkg/web/service.go:18-23`、`pkg/web/handler.go:18-53`、`connect.go:43,247,260` |

Hub 二进制依赖闭包（`go list -deps ./cmd/wscyber-hub`，1133 包）中的持久化库：
- `modernc.org/sqlite`、`bun/dialect/sqlitedialect`：只被 `pkg/web/service` 与 `wscyber/native/webapi` 导入。
- `bbolt`、`buntdb`、`goleveldb`、`pogreb`：只被 `projectdiscovery/hmap/store/disk` 导入（katana 爬虫的临时磁盘缓存，非平台数据），不迁。
- `jackc/pgx/v5/pgconn`：被 `praetorian-inc/titus/pkg/validator` 导入（密钥校验器），`pgx v5.7.2` 已在 `native/go.sum:463-464`。
- 其它文件持久化：`exts/jev/store.go:113,214`（jev 库文件）、`core/events/jsonl/jsonl.go:68-72`、`pkg/node/agent.go:268-272`（上传落临时目录）——均在节点工作目录（`model-node-*` 临时目录）内，不是 Hub 业务数据。
- 本项目 state-dir 内其它文件：`webapi-secrets.key`（凭据/Webhook 加密密钥，`webapi/asset_secret.go:21`）、`egress-ca/`（出口 CA，`webapi/egress.go:35`）、`model-config/cyber.yaml`（模型配置，`cmd/wscyber-hub/model_config.go:29-40`）、`model-node-*`（节点工作目录，`model_product.go:219`）。

Bun 事实：`bun v1.3.0` 根模块自带 `migrate` 包（`~/go/pkg/mod/github.com/uptrace/bun@v1.3.0/migrate/`，`NewMigrator`、`Discover(fs.FS)`、`Lock`、`WithTableName`）；**`bun/dialect/pgdialect` 未在模块缓存，需经代理下载**。`bun.DB.QueryContext/ExecContext` 与 `bun.Tx` 同名方法会在客户端格式化 `?` 后再交给驱动（`bun@v1.3.0/db.go:374-381,721-727`）；直接调 `*sql.DB` 则不格式化。`golang.org/x/crypto v0.57.0`（已在 go.mod）含 `argon2`、`bcrypt`。

### 0.2 本项目侧装配与补丁

| 事实 | 位置 |
|---|---|
| Hub 用 `filepath.Join(stateDir,"hub.sqlite")` 作 `Management.Database`；`--state-dir` 校验“不得含 SQLite URI 分隔符” | `native/cmd/wscyber-hub/main.go:121-122,156-161` |
| 启动前 `webapi.PrepareWebAPISchema(hub.sqlite)`，再 `webapi.NewCapability(hub.sqlite)`，Capability 经 `SchemaModules()` 把 44 张业务表交给原生建表 | `main.go:168-177`、`webapi/capability.go:327-346` |
| webapi 自开第二个 `*sql.DB`（同一文件，`_txlock=immediate`，`SetMaxOpenConns(1)`），`taskStore{db *sql.DB, orm *bun.DB}` | `webapi/store.go:13-14,150-171` |
| 阶段升级机制：`webapiTableOrder`（44 表）、`webapiSchemaStages`（19 个历史阶段）、`PrepareWebAPISchema` 逐阶段试开 `NewSQLiteStore`、`addWebAPISchemaStage` 补表 | `webapi/verification_store.go:319-395` |
| 补丁：唯一补丁给上游 `NewSQLiteStore` 加 `_txlock=immediate`；`scripts/cyber-go-overlay.py` 从模块缓存复制只读副本到 `var/cyber-sqlite-overlay/module-current/`，单行字符串替换后用 `go -overlay` + 派生 modfile 构建/测试 | `native/patches/store_sqlite.go.patch`、`native/patches/README.md`、`scripts/cyber-go-overlay.py:10-54` |

### 0.3 webapi 的 SQLite 特有写法（非测试文件，64 个）

| 类别 | 数量 | 代表样例 | 结论 |
|---|---|---|---|
| `ON CONFLICT … DO NOTHING / DO UPDATE SET x = EXCLUDED.x` | 33 处 | `asset_store.go:213`、`request_store.go:109`、`explore.go:104`、`notify_store.go:77` | PG 语法相同，均有对应唯一索引，**可直接用** |
| `INSERT OR IGNORE/REPLACE` | 0 | — | — |
| `json_extract(col,'$.a.b')` | 10 行（5 文件） | `global_lists.go:186-191`、`notify_store.go:122,125`、`task_list.go:249`、`vuln_store.go:395-399` | 改 `col::jsonb #>> '{a,b}'` |
| `json_each(col)` | 1 | `board.go:751`（`boardUntestedClause`，也拼进 `board.go:1055`） | 改 `jsonb_array_elements_text(col::jsonb)` |
| `json_array_length(col)` | 1 | `global_lists.go:390` | 改 `jsonb_array_length(col::jsonb)` |
| `datetime/strftime/julianday` | 0 | — | 时间全是 Go 端 `RFC3339Nano` 文本（50 处，如 `asset_store.go:210`） |
| `rowid` 排序 | 2 | `board.go:1017`（`at ASC, rowid ASC`）、`usage.go:209`（`created_at, rowid`） | PG 无 rowid；改为 `id` 或新增 identity 列 |
| `AUTOINCREMENT` | 0（webapi）；上游 1 | `store_models.go:51` | pgdialect 生成 identity/serial |
| `PRAGMA`、`sqlite_master` | 0（webapi）；上游 2 | `store_schema.go:147,164`、`store_sqlite.go:45` | 随上游 overlay 替换 |
| `COLLATE NOCASE`、`LIKE` | 0 | — | — |
| `instr(lower(x), ?) > 0` | 2 | `audit.go:155`、`board.go:757` | 改 `strpos(lower(x), ?) > 0` |
| `substr(…)` | 3 | `usage.go:272,277`、`audit.go:141` | PG 同名同义，**可直接用** |
| 布尔聚合 `SUM(cond)` | 2 行 | `board.go:1054,1060` | 改 `COUNT(*) FILTER (WHERE cond)` |
| 布尔列与整数比较 `archived = 0`、`enabled = 1`、`COALESCE((SELECT cs.archived = 1 …), 0)` | 5 | `global_lists.go:75`、`notify_ops.go:391`、`overview.go:191,551`、`task_list.go:77` | 改 `NOT archived` / `enabled` / `COALESCE((SELECT cs.archived …), false)` |
| 直接调 `*sql.DB`（`s.db/c.store.db.QueryContext/QueryRowContext/ExecContext`，SQL 内 `?` 占位，不经 bun 格式化） | 52 处（16 文件，overview 8、task_list 6、vuln_store 6…） | `notify_store.go:122`、`usage.go:209`、`overview.go:619` | 全部改走 `s.orm.QueryContext`（bun 格式化 `?`，pgx 不认 `?`）。另 1 处 `tx.QueryContext` 已是 `bun.Tx`，可用 |
| bun 标签 `type:blob` | 3 | `egress_store.go:41`、`store.go:47,50` | 改 `type:bytea`（或去掉，pgdialect 对 `[]byte` 默认 bytea） |
| `type:text` | 48 | `board.go:82` | 可用 |
| 布尔列（`bool`/`sql.NullBool`） | 14 | `asset_store.go:27`、`egress_store.go:44` | bun 在 PG 用 `boolean`，可用 |
| `IN (…)` 分块 `sqlInChunk = 500`（注释“远低于 SQLite 变量上限”） | 1 | `task_list.go:27-28` | 可保留；bun 内联值，不受绑定参数上限影响 |
| `MAX(number)+1` 编号 | 2 | `finding_store.go:211`（全局漏洞编号）、`board.go:888`（任务内 Oracle 编号） | 并发语义见 §5.3 |
| 错误码判断（`SQLITE_BUSY`/约束字符串） | 0（生产代码） | 只在测试 | — |
| 依赖 SQLite 单写者串行的代码与注释 | ≥8 处 | `request_store.go:72-73,108`（claim：先 INSERT 取写锁再查同指纹未决）、`finding_store.go:158`、`vuln_store.go:207`、`store.go:246-248`、`notify_ops_store.go:79`、`notify_ops.go:111,190` | **最大语义风险**：PG 并发事务互不阻塞，需显式锁（§5.3） |
| `RunInTx` 事务入口 | 27 | `request_store.go:108,168` 等 | 统一换成带身份与锁的事务助手 |
| ORM 直接调用 | `s.orm.New*` 101 处、`tx.New*` 106 处 | — | 非事务的 101 处需进入身份助手（§4.4） |
| 跨读原生表 | 4 处 | `task_list.go:76-77,323`、`usage.go:280`、`overview.go:619` | 读 `chat_aop_events`/`chat_sessions`，需同库 |

webapi 表之间**没有外键**（`rel:belongs-to`/`REFERENCES` 0 处）。

### 0.4 测试现状

- `webapi/` + `cmd/wscyber-hub/` 共 46 个测试文件、250 个 `Test*`；无 `TestMain`、无 `t.Parallel`。
- 30 个文件用 `filepath.Join(t.TempDir(),"hub.sqlite")`；27 个文件调 `webservice.NewSQLiteStore`；14 个调 `openTaskStore`；19 个文件引用阶段函数。
- **删除**：20 个 schema 升级测试（`Test*SchemaUpgradeFrom*`、`Test*LegacyPreparation`）；7 个 `notify_ops_busy_test.go` + 1 个 `store_txlock_test.go` 的 SQLITE_BUSY/IMMEDIATE 测试。
- **改写**：其余测试只换取库方式（统一助手），断言不变；新增 PG 并发测试替代 busy 测试（§7.3）。

### 0.5 会话归属与认证

- 会话→任务映射全在 webapi：`taskForSession` 依次查 `webapi_tasks.initial_session_id` → 验证 → 复测 → 子会话（`request_store.go:40-70`）；`egressTaskForSession` 再加报告会话（`egress_store.go:53-66`）；`researchTaskForSession`（`scenario.go:348`）。上游会话/事件/账本/制品表无用户或租户字段（`store_models.go:12-56`）。
- 认证：上游单访问密钥（§0.1）；前端 `runtime/auth.ts:30,45` 调 `/api/auth/session`、`/api/auth/login`（token），`api.ts:89` 取 `/api/auth/agent-token`；Hub 用 `--access-key-file`。审计 `actor` 只有 `user`（经原生鉴权的 HTTP，无用户名）/ `system`（`webapi/audit.go:22-23`）。

---

## 1. 总体结构

- **一个 PG 数据库、`public` schema**，原生表（`chat_sessions`、`chat_aop_events`、`aop_request_ledger`、`raw_artifacts`）、webapi 44 表、新增 `tenants`/`users`/`user_sessions`/`webapi_counters`/迁移表同库。Hub 不启用 Scans（`main.go:162` 注释），`scans`/`session_scans` 不进基线。
- **两个连接池、不共享事务**：上游 store 在扩展 `Load` 内自建（无法注入池，§0.1），webapi 自建。现有跨表访问只是 webapi 读原生表（§0.3 末行），无跨池事务需求。两池都用 `pgx/v5/stdlib` + `bun/dialect/pgdialect`。
- **连接配置**：`--database-url-file PATH`（P1 修订：与 D1 私密文件规则统一——属主可读、group/other 无任何权限即 0600 或 0400，拒绝符号链接与特殊文件，打开后复查同一文件；内容为单行；避免口令出现在 `ps` 参数）；未给时读环境变量 `WSCYBER_DATABASE_URL`；都没有则启动失败。不提供明文 `--database-url` 旗标。日志只打印 `pgx.ParseConfig` 得到的 host/port/database/user，口令不进日志（实现：`pgschema.DatabaseURL` 的 `String/GoString` 只含这四项，原文只经 `DSN()` 取得；解析失败的错误不回显原文）；上游 `Management.Database` 字段承载 DSN，但上游只校验非空、不打印（`extension.go:89-91`；`host.go:64` 只打印 AccessKey，已用 NopLogger）。
- **连接参数**：每池 `MaxOpenConns=8`、`MaxIdleConns=4`、`ConnMaxLifetime=30m`、`ConnMaxIdleTime=5m`；DSN 追加 `application_name=wscyber-hub-native|webapi`、`timezone=UTC`（本机集群 `timezone='America/New_York'`）。总连接 ≤16，远低于默认 `max_connections=100`。
- **单实例**：审批、调度、锁仍在进程内（`approval.go:65`、`capability.go:306-308`、`notify.go:75,84`），state-dir 仍存密钥与 CA。Hub 启动时取会话级 `pg_try_advisory_lock(<wscyber 常量>)`，失败即退出“同库已有 Hub 在运行”。多实例不在本期范围。
- state-dir 保留（密钥、出口 CA、模型配置、节点工作目录），只是不再有 `hub.sqlite`；`--state-dir` 的“SQLite URI 分隔符”校验删除。

---

## 2. 原生存储方案

### 方案 A（推荐）：overlay 整文件替换上游存储三件 + 新增一个文件

- 用 PG 版本整文件替换 `store_sqlite.go`、`store_schema.go`、`store_models.go`；新增 `store_pg_identity.go`（Go overlay 支持映射不存在的路径＝新增文件）。**保留标识符** `SQLiteStore`、`NewSQLiteStore(dsn string, modules ...SchemaModule)`、`orm` 字段、`appendAOPEvent`、`archiveArtifactEvents`、各 `*Model` 名，使 `service.go`、`agents.go`、`events.go`、`agents_mux.go`、`extension.go`、`scan_store.go` **一行不改**。类型名保留 SQLite 字样是为了补丁最小，文件头注释说明。
- 替换内容：pgx 打开、pgdialect；不再建表——只校验迁移版本已是本二进制期望的版本，并校验 `schemaUnion(modules)` 声明的每张表/列都存在于 `information_schema.columns`（允许多出 `tenant_id` 等列；bun 生成显式列清单，多列对上游透明）；`instr` → `strpos`；`type:blob` → `bytea`；`rawArtifactModel.Cursor` 用 identity；每个方法在事务内先 `set_config` 身份（§4.4）。
- 新增文件导出：`type Identity{TenantID, UserID, Role}`、`WithIdentity(ctx, Identity) context.Context`、`IdentityFrom(ctx)`、`WithSystem(ctx, tenantID)`，供 webapi 与认证共用；webapi 引用这些符号 → **普通 `go build` 编译失败**（替代现在“静默用未修复代码”的风险）。
- `cyber-go-overlay.py` 从“单行字符串替换”改为“清单驱动整文件替换”：`native/patches/cyber/manifest.json` 记录每个上游相对路径的上游源 sha256 与替换文件路径；上游源哈希不符立即失败。仍走只读副本 + 派生 modfile + `-overlay`。
- 改动量：替换三文件约 850 行（以上游 862 行为蓝本移植，SQL 改动仅 `instr` 与建表/校验部分），新增约 80 行，脚本约 +40 行。
- 升级成本：上游升级时只需对这三文件做三方 diff 并重移植；哈希校验保证不会漏。上游若改 `SessionStore`/事件逻辑，需同步——这些逻辑本就在这三文件里。
- 风险：移植时漏掉上游行为（缓解：在 `native/` 里写针对 `*service.SQLiteStore` 的契约测试，覆盖 `store_sqlite_test.go` 的会话分页/过滤、事件去重、turn 终止游标、制品同步语义）。

### 方案 B：overlay 补丁在上游加注入点，PG 实现全部放本项目

- 补丁 `service.go`（`Store` 改接口）、`agents.go`（同）、`events.go`（`s.store.orm` 改方法、`appendAOPEvent` 导出）、`agents_mux.go`（`archiveArtifactEvents` 导出）、`extension.go`（`Config.OpenStore func(...)`）共 5 文件约 10 个 hunk；本项目实现 PG store（约 700 行，含未导出语义重写）。
- 优点：PG 代码是正常本项目 Go，测试直跑。缺点：hunk 落在上游最活跃的服务核心文件，升级冲突概率高；接口需导出 `appendAOPEvent(after)` 这种内部契约；`sessionModel` 等未导出模型要在本项目复制一份，与上游 JSON 序列化逻辑（`sessionFromJSON` 剔除旧键等）双份维护。

### 方案 C：fork 上游模块（`replace` 指向自有 fork）

- 改动最自由，但违背“固定版本 + 公开模块可复现”（`native/DEPENDENCIES.md`），许可/来源记录需重做，升级变成维护 fork。不推荐。

**推荐 A**：上游其余文件零改动；补丁面集中在“存储实现”这一个职责内，哈希保护；方案 B 的接口化收益在单实现场景下不明显。

---

## 3. schema 管理

- **工具**：`bun/migrate`（已在 `bun v1.3.0` 根模块，不加新模块）。迁移 SQL 以 `embed.FS` 放 `native/pgschema/migrations/`，`Migrations.Discover(fsys)`；迁移表 `wscyber_migrations`/`wscyber_migration_locks`（`WithTableName`/`WithLocksTableName`）。P1 修订：`wscyber_migration_locks` 只是 `Migrator.Init` 顺带建出、不使用——`Migrator.Lock` 是往锁表插行，崩溃会残留；互斥改用会话级 `pg_advisory_lock`（专用连接，进程崩溃即释放）。bun 默认在迁移事务之外登记版本（崩溃会出现“已登记未执行”），运行器改为逐个调用迁移、在 `AfterMigration` 钩子里于**同一事务**内登记。
- **基线**：`0001_baseline.tx.up.sql`（P1 修订：bun 只对 `.tx.up.sql` 整体开事务，`.up.sql` 逐句独立执行；运行器拒绝非 `.tx.up.sql` 与 `.down.sql` 文件）手写全部表、索引、`tenant_id` 列、触发器、RLS（P5 前先不 `ENABLE`）。手写 DDL 而不是 bun `CreateTable`，因为需要 identity、bytea、默认值、触发器与策略。**首次生产部署前基线可原地修改**（开发库重建即可，延续上游“首发前无迁移”的做法）；首发后只增不改（`0002_…`）。
- **启动策略**：Hub 启动 → 用迁移 DSN 连接 → 角色检查 → 单实例锁 → `pg_advisory_lock`（迁移锁）→ 若库里已应用的迁移含本二进制不认识的版本（库比二进制新）→ 失败；否则应用未应用的迁移（PG DDL 事务化，每个迁移一个事务）→ 释放迁移锁 → 再打开两个运行池（P1 实现：`pgschema.Start` 按此顺序执行，运行池打开后用 `pgschema.RequireCurrent` 校验）。上游 overlay store 与 webapi 只校验版本/列，不建表。另提供 `wscyber-hub db status` 子命令只读查看版本。
- **回滚**：不写 down 迁移（生产不执行）；回滚＝升级前 `pg_dump` 备份后恢复，并回退二进制。RUNNING.md 写明“升级前备份”。开发期回滚＝删库重建。
- **删除**：`webapiTableOrder`、`webapiSchemaStages`、`webapiSchemaStage`、`legacyWebAPISchema`、`PrepareWebAPISchema`、`addWebAPISchemaStage`（`verification_store.go:319-395`）及 `main.go:168-170` 调用；上游 `createSchema/validateSchema/schemaTables/schemaColumns` 在 overlay 中被替换。`SchemaModule()`（`store.go:57`）保留，作为列存在性校验的声明来源（Capability 接口要求 `SchemaModules()`）。
- **一致性测试**：`pgschema` 测试断言 `information_schema` 列集合 ⊇ 上游 `coreSchema.Tables` ∪ `webapi.SchemaModule().Tables`，且每表都有 `tenant_id`（白名单外），防止模型与基线漂移。

---

## 4. 多租户模型

### 4.1 新表

```sql
CREATE TABLE tenants (id text PRIMARY KEY, name text NOT NULL UNIQUE, created_at text NOT NULL, archived boolean NOT NULL DEFAULT false);
CREATE TABLE users (
  id text PRIMARY KEY, tenant_id text REFERENCES tenants(id),        -- 管理员可为 NULL（不属于任何租户）
  username text NOT NULL UNIQUE, display_name text NOT NULL,
  role text NOT NULL CHECK (role IN ('admin','user')),
  password_hash text NOT NULL,                                          -- argon2id PHC 串
  disabled boolean NOT NULL DEFAULT false, created_at text NOT NULL, updated_at text NOT NULL,
  CHECK (role = 'admin' OR tenant_id IS NOT NULL));
CREATE TABLE user_sessions (token_hash bytea PRIMARY KEY, user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at text NOT NULL, expires_at text NOT NULL, last_seen_at text NOT NULL);
CREATE TABLE webapi_counters (tenant_id text NOT NULL REFERENCES tenants(id), name text NOT NULL, value bigint NOT NULL, PRIMARY KEY (tenant_id, name));
```

- 口令哈希：**argon2id**（`golang.org/x/crypto/argon2`，已在依赖内），参数 m=64MiB、t=3、p=2、16 字节盐、32 字节输出，存 PHC 串以便日后调参；比较用 `subtle.ConstantTimeCompare`。会话 token 32 字节随机，库里只存 SHA-256。
- 第一阶段（P5，无登录）：基线种子 `tenants('default','默认租户')`，不建用户；所有请求按 `Identity{TenantID:"default", Role:"admin", UserID:""}` 处理（现单访问密钥 = 管理员占位）。P6 起访问密钥只给节点/引导用，浏览器改用户名口令。

### 4.2 业务表 `tenant_id` 与归属规则

所有业务表加 `tenant_id text NOT NULL`（`users`/`tenants`/迁移表除外）。写入时取值规则：

1. **根表**：默认值 `current_setting('app.tenant_id')`（未设置即报错，失败可见）。
2. **子表**：通用触发器 `wscyber_inherit_tenant(parent_table, parent_column)` 在 BEFORE INSERT 从父行取 `tenant_id`；若会话也设了 `app.tenant_id` 且与父行不一致 → `RAISE`；父行不存在（键为空）→ 回退到规则 1。
3. 不允许 UPDATE `tenant_id`（触发器拒绝）。

P1 实现说明（`0001_baseline.tx.up.sql`）：规则 2 细化为——外键列**为空**才回退 GUC；外键列**非空但父行不存在**一律拒绝（不回退，避免挂到错误租户）；显式给出的 `tenant_id` 与父行不一致也拒绝。`app.tenant_id` 一律用 `current_setting(…, true)` 读并把空串当未设置（同一连接上 `set_config(…, true)` 用过一次后，后续事务读到的是空串而非“未定义”）。错误码：`WT001` 未设租户、`WT002` 租户不一致、`WT003` 无父行、`WT004` 改 `tenant_id`（P3 起 webapi 可按 `pgconn.PgError.Code` 判断）。触发器按参数 `(父表, 父键列, 子表外键列)` 通用实现，父行查询走父表主键索引。

| 归属 | 表 |
|---|---|
| 根（GUC） | `chat_sessions`、`aop_request_ledger`、`raw_artifacts`、`webapi_tasks`、`webapi_assets`、`webapi_notify_settings`、`webapi_notify_ops_settings`、`webapi_vuln_sla`、`webapi_audit_events`（见 4.6）、`webapi_notifications` |
| 父 `chat_sessions(id)` via `session_id` | `chat_aop_events` |
| 父 `webapi_tasks(id)` via `task_id` | `webapi_requests`、`webapi_findings`、`webapi_finding_revisions`、`webapi_verifications`、`webapi_reports`、`webapi_retests`、`webapi_task_policies`、`webapi_task_policy_events`、`webapi_asset_tasks`、`webapi_egress_flows`（`task_id` 可能为空→回退 GUC，P5 核对）、`webapi_board_endpoints`、`webapi_board_ingest`、`webapi_board_facts`、`webapi_board_oracles`、`webapi_board_oracle_events`、`webapi_task_scenarios`、`webapi_task_explore`、`webapi_replays`、`webapi_subagent_reservations`、`webapi_task_subsessions`、`webapi_subsession_usage`、`webapi_authz_compares` |
| 父 `webapi_assets(id)` via `asset_id` | `webapi_asset_credentials`、`webapi_asset_schedules`、`webapi_schedule_runs`、`webapi_schedule_options`、`webapi_schedule_run_followups`、`webapi_asset_vulns`、`webapi_asset_vuln_members`、`webapi_asset_scenarios`、`webapi_asset_explore`、`webapi_vuln_triage` |
| 其它父 | `webapi_verification_results`←`webapi_verifications(verification_id)`、`webapi_report_drafts`←`webapi_reports(report_id)`、`webapi_retest_results`←`webapi_retests(retest_id)`、`webapi_schedule_followups`←`webapi_schedule_runs(run_id)`、`webapi_notification_ops`←`webapi_notifications(notification_id)` |

原生会话/事件不改上游模型：`chat_sessions.tenant_id` 由 `CreateSession` 所在事务的 `app.tenant_id` 默认值填入（唯一创建点 `api/session.go:236`，ctx 来自请求身份或 webapi 显式 `WithSystem(ctx, task.TenantID)`）；事件由触发器从会话继承——上游 `context.Background()` 的事件写入路径无需知道租户。

单行设置表（`webapi_notify_settings`、`webapi_notify_ops_settings` 以字符串 `id` 为主键；`webapi_vuln_sla` 用 `vulnSLAID = 1`，`triage.go:33`）改为**每租户一行**：主键改 `(tenant_id)` 或 `(tenant_id, id)`。模型配置（state-dir `cyber.yaml`）为全局、仅管理员可改。

### 4.3 RLS

每张业务表：

```sql
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t FORCE ROW LEVEL SECURITY;           -- 表属主也受约束
CREATE POLICY tenant_rw ON t
  USING      (wscyber_can_see(tenant_id))
  WITH CHECK (wscyber_can_see(tenant_id));
-- wscyber_can_see(x) := current_setting('app.role', true) IN ('admin','system')
--                       OR x = NULLIF(current_setting('app.tenant_id', true), '')
```

- 未设置身份 → `app.role` 为空、`app.tenant_id` 为空 → 不可见、不可写（fail-closed）。
- `tenants`：admin/system 全可见；user 只见自己的 `id`。`users`：admin/system 全可见；user 只见本人。`user_sessions`：仅 system（登录与会话校验以 system 身份执行）。
- 应用角色要求：**非超级用户、无 `BYPASSRLS`**（超级用户无视 FORCE）。启动时查 `pg_roles` 的 `rolsuper/rolbypassrls`，任一为真即拒绝启动。

### 4.4 身份传递（每请求事务内设置）

- 身份放 ctx（§2 新增文件的 `Identity`）。所有存储访问经两个助手：`read(ctx, fn(bun.IDB))`、`write(ctx, locks, fn(bun.Tx))`；助手开事务后执行
  `SELECT set_config('app.role',$1,true), set_config('app.tenant_id',$2,true), set_config('app.user_id',$3,true)`（`is_local=true`，只在本事务有效，连接归还池后不残留）。
- 来源：HTTP 请求由认证中间件写入（P6 前固定为默认租户管理员）；后台任务用 `WithSystem(ctx, tenantID)`。**ctx 无身份时**：webapi 助手报错（编程错误）；上游 overlay store 视为 `system`（因上游后台路径一律 `context.Background()`，§0.1）——安全前提是认证中间件拒绝一切无身份的 `/api/` 请求（上游 Middleware 已如此，`auth.go:45-66`），使无身份 ctx 只可能来自非 HTTP 协程。
- 管理员跨租户：`role=admin` 且 `tenant_id` 为空＝“全部租户”只读视图；在某租户内操作时前端带所选租户（请求头 `X-WSCyber-Tenant`，仅 admin 生效），中间件设 `app.tenant_id`，于是新建行落该租户。admin 在“全部租户”视图下新建根表行会因默认值缺失而报错（预期）。

### 4.5 后台任务

| 任务 | 位置 | 身份与执行方式 |
|---|---|---|
| 周期计划调度 | `schedule.go:92,140` | `system`（无租户）查到期计划（跨租户）→ 每条计划在 `WithSystem(ctx, asset.TenantID)` 下建 run/任务/会话 |
| 推送投递 | `notify.go:110` | `system` 取待投递通知 → 逐条按通知租户读该租户 Webhook 设置并投递 |
| 运营通知登记 / 周报 | `notify_ops.go:111,358` | 审批/计划失败按事件所属任务租户；周报**逐租户循环**，每租户一份、发往该租户 Webhook |
| 遗留子会话释放 | `subagent.go:426` | `system` 全局一次 |
| 审批超时 | `approval.go` | 内存态；P5 起待审批列表按租户过滤（顺带修“待审批未按任务隔离”） |

### 4.6 唯一约束与编号

- 漏洞编号：现全局唯一（`store.go:111` `webapi_findings_number(number)`，`finding_store.go:211` `MAX(number)+1`）。改为**租户内唯一**：唯一索引 `(tenant_id, number)`，取号用 `webapi_counters`：`INSERT … ON CONFLICT (tenant_id,name) DO UPDATE SET value = webapi_counters.value + 1 RETURNING value`（行锁串行，与 RLS 无关、admin 视图也正确）。
- Oracle 编号已是任务内（`webapi_board_oracles_number(task_id, number)`），不变。
- 其它唯一索引里含 `session_id`/`task_id`/随机 `id` 的保持全局唯一（ID 随机，不会跨租户冲突）；`users.username` 全局唯一；`tenants.name` 全局唯一。
- 审计：加 `tenant_id`（全局配置类事件为 NULL，策略：NULL 行仅 admin/system 可见）、`actor_user_id`；`actor` 取值保留 `user|system`，P6 后 `user` 带用户 ID 与展示名。

### 4.7 登录片如何接上

P5 结束时：身份链路、RLS、默认租户全部就位，认证仍是单密钥＝默认租户 admin。P6 只做：overlay 整文件替换 `pkg/web/service/auth.go`（保留 `Auth` 类型与 `web.Auth` 四方法），`Authenticate` 改为查 `user_sessions`，`Middleware` 在放行时 `r.WithContext(WithIdentity(...))`；访问密钥 bearer 仍有效（节点、`agent-token`），映射为 `system`；登录接口改收 `{username,password}`。

---

## 5. 类型与 SQL 映射

### 5.1 类型

| SQLite/bun 现状 | PG |
|---|---|
| `string`（无标签）→ TEXT | pgdialect 默认 `varchar`；基线统一手写 `text` |
| `type:text` | `text` |
| `type:blob` / `[]byte` | `bytea`（标签改 `type:bytea`） |
| `bool` / `sql.NullBool` | `boolean` |
| `int64`/`int` | `bigint`/`integer`；`uint64`（`sequence`）→ `bigint`（值域足够） |
| `autoincrement` 主键 | `bigint GENERATED BY DEFAULT AS IDENTITY` |
| 时间 `RFC3339Nano` 文本 | **保持 `text`**（行为等价；`substr(created_at,1,10)` 日期过滤依赖它）。已知：`RFC3339Nano` 去尾零，同秒内文本排序可能与时间序不一致——是现状问题，不在本迁移改；如需改 `timestamptz` 另立片 |
| `*_json` 文本 | 保持 `text`，查询处 `::jsonb`；不改为 jsonb 列以免改 Go 扫描类型 |

### 5.2 SQL

| SQLite | PG |
|---|---|
| `json_extract(c,'$.payload.verdict')` | `c::jsonb #>> '{payload,verdict}'` |
| `json_each(c) AS j … j.value` | `jsonb_array_elements_text(c::jsonb) AS j(value)` |
| `json_array_length(c)` | `jsonb_array_length(c::jsonb)` |
| `instr(lower(x), ?) > 0` | `strpos(lower(x), ?) > 0` |
| `SUM(cond)` | `COUNT(*) FILTER (WHERE cond)` |
| `archived = 0` / `enabled = 1` / `x = 1`→整数 | `NOT archived` / `enabled` / 布尔表达式 |
| `ORDER BY …, rowid` | `ORDER BY …, id`（`webapi_board_oracle_events`、usage 子查询的各表都有文本 `id` 或 `session_id`；若需插入序，给该表加 identity 列 `seq`） |
| `?` 占位（经 `*sql.DB`） | 改经 `bun.DB/bun.Tx` 的 `QueryContext`（bun 格式化 `?`）；禁止使用 PG `jsonb ?` 运算符（与 bun 占位冲突），用 `jsonb_exists()` |
| `sqlite_master` / `PRAGMA table_info` | `information_schema.tables/columns`（只在 overlay store 校验处） |
| `PRAGMA foreign_keys` 校验 | 删除（PG 外键始终生效） |
| `_txlock=immediate` / busy_timeout | 删除；串行由 §5.3 显式锁保证；DSN 设 `lock_timeout=10s`、`statement_timeout` 不设 |
| `ON CONFLICT (…) DO NOTHING/DO UPDATE SET … EXCLUDED.…` | 不变 |
| `substr`、`COALESCE`、`GROUP BY`、`DISTINCT`、`COUNT(DISTINCT …)` | 不变 |
| 错误判断 | 生产代码无；新代码用 `pgconn.PgError.Code`（`23505` 唯一冲突、`40001` 序列化失败、`55P03` 锁超时） |

### 5.3 并发语义（SQLite 单写者 → PG 多连接）

现 webapi 池 `MaxOpenConns(1)` + IMMEDIATE：**所有 webapi 写事务全局串行**，代码以此为正确性前提（§0.3）。PG READ COMMITTED 下并发事务看不到彼此未提交行，`claim` 的“同指纹未决”检查、`MAX+1` 取号会被并发穿透。

方案（P3 落地，等价优先）：
1. `write(ctx, locks, fn)` 助手在事务开头按 `locks` 依次 `pg_advisory_xact_lock(hashtextextended(key, 0))`。
2. **P3 默认对每个写事务加一把全局键 `webapi:write`**，精确复现现行串行语义；读不加锁。27 个事务入口逐一迁移，不逐个分析。
3. P5 起把全局键收窄为 `webapi:write:<tenant_id>`（不同租户互不阻塞），并对已证明安全的入口（如纯插入）去锁——每处改动带并发测试。
4. 上游事件游标 `MAX(cursor)+1` 依赖进程内 `eventMu`（单实例保证，§1）；overlay 中额外加 `pg_advisory_xact_lock(hashtext('aop:'||session_id))` 作防御。

---

## 6. 表清单（基线 v1）

上游 core 4 表 + webapi 44 表（`store.go:57-148` 的 `SchemaModule()`）+ 新增 4 表（tenants、users、user_sessions、webapi_counters）+ 迁移 2 表。列与现 `Tables` 声明一致，每业务表加 `tenant_id`；索引照搬 39 条 webapi 索引与 6 条 core 索引，所有含 `tenant_id` 访问路径的索引前置 `tenant_id`（如 `(tenant_id, updated_at DESC)`），漏洞编号唯一改 `(tenant_id, number)`。不新增 webapi 外键（保持现语义；如需另议）。

P1 实际基线：48 张业务表（core 4 + webapi 44）+ `tenants`/`users`/`user_sessions`/`webapi_counters` + 迁移 2 表；索引名沿用模型声明。前置 `tenant_id` 的只有按租户列全表的路径：`idx_sessions_updated (tenant_id, updated_at DESC)`、`webapi_notifications_created (tenant_id, created_at DESC, id DESC)`、`webapi_audit_events_at (tenant_id, at, id)`、`webapi_findings_number` 唯一 `(tenant_id, number)`，另新增 `webapi_tasks_tenant_created`、`webapi_assets_tenant_updated`；以 `task_id`/`asset_id`/`session_id` 开头的索引不加（这些键已唯一归属一个租户）。原生 `chat_aop_events → chat_sessions` 外键 `ON DELETE CASCADE` 与 `(session_id, cursor)` 唯一约束、`raw_artifacts.cursor` identity 照上游模型保留。RLS 策略 `tenant_rw`（以及 `tenants`/`users`/`user_sessions` 的专用策略）已建但未 `ENABLE`。**P1 未做、留给后续片**：单行设置表（`webapi_notify_settings`、`webapi_notify_ops_settings`、`webapi_vuln_sla`）主键改每租户（P5，首发前可原地改基线）；审计 `actor_user_id` 列（P5/P6）。

---

## 7. 测试基础设施

### 7.1 取库

- 环境变量 `WSCYBER_TEST_DATABASE_URL`：指向一个有 `CREATEDB` 权限的非超级用户角色的维护库（如 `postgres://wscyber_dev:…@127.0.0.1:5432/postgres`）。**未设置时测试 `t.Fatal`/`TestMain` 退出 1 并打印设置方法，不静默跳过。**
- 包级 `TestMain`：创建模板库 `wscyber_tpl_<pid>_<rand>` 并跑全部迁移，测试结束删除。
- 每个测试 `testdb.New(t)`：`CREATE DATABASE wscyber_t_<rand> TEMPLATE <tpl>`（毫秒级），返回 DSN；`t.Cleanup` 关闭池后 `DROP DATABASE … WITH (FORCE)`。`go test -p N` 多包并行互不干扰（库名随机）。崩溃残留库名有固定前缀，提供 `scripts/pg-test-clean.py` 清理。
- 共享助手放 `native/internal/testdb/`（`webapi` 与 `cmd/wscyber-hub` 共用）；现 30 个 `hub.sqlite` 构造点与 27 个 `NewSQLiteStore` 调用统一改为 `testdb.New(t)` + `webapi.NewCapability(dsn)` / `service.NewSQLiteStore(dsn, webapi.SchemaModule())`。
- RLS 测试不需要另建角色：`FORCE ROW LEVEL SECURITY` 使属主也受策略约束，同一个非超级用户角色即可测。

### 7.2 本机 PG18（需用户/主会话执行的系统操作）

现状：PG 18 集群 `18/main` 端口 5432，`down`；`postgresql.service` 与 `postgresql@.service` 均 disabled；`pg_hba.conf` 本地 `peer`、`127.0.0.1/32` `scram-sha-256`。root 不能直接以 root 跑服务端，但 systemd 单元以 `postgres` 用户运行，root 用 `runuser -u postgres` 走 peer 认证即可管理：

```bash
# 1. 启动并开机自启
systemctl enable --now postgresql@18-main
# 2. 开发/测试角色（非超级用户，可建库）与演示库
runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c "CREATE ROLE wscyber_dev LOGIN CREATEDB PASSWORD '<随机口令>'"
runuser -u postgres -- createdb -O wscyber_dev wscyber_demo
runuser -u postgres -- createdb -O wscyber_dev wscyber_preview
# 3. 连接串写本机文件（不入库）
install -m 600 /dev/null ~/.config/wscyber/pg-dev.url
echo 'postgres://wscyber_dev:<口令>@127.0.0.1:5432/wscyber_preview?sslmode=disable' > ~/.config/wscyber/pg-dev.url
echo 'export WSCYBER_TEST_DATABASE_URL=postgres://wscyber_dev:<口令>@127.0.0.1:5432/postgres?sslmode=disable' > ~/.config/wscyber/pg-test.env
```

- `demo-stack.py up`：每次 `DROP DATABASE IF EXISTS wscyber_demo WITH (FORCE); CREATE DATABASE wscyber_demo`（以 `wscyber_dev`），把 DSN 写入演示 state 目录内 0600 文件，以 `--database-url-file` 启动 Hub；`down` 不删库（便于事后排查），下次 `up` 重建。连接串来源 `~/.config/wscyber/pg-dev.url` 的 host/口令，库名替换为 `wscyber_demo`。
- 预览 Hub：`--database-url-file ~/.config/wscyber/pg-dev.url`（库 `wscyber_preview`），全新开始；现 `var/preview/state` 只保留密钥、CA、模型配置。
- 执行会话（headless）不做系统操作；步骤 1–3 由用户或主会话在 P1 开始前执行。

### 7.3 测试改写清单

- 删除：20 个 schema 升级测试；`notify_ops_busy_test.go`（7 个）、`store_txlock_test.go`（1 个）。
- 新增：overlay store 契约测试（会话分页/过滤/搜索、事件去重与 turn 终止、制品同步、请求账本冲突）；并发测试 2 个（同指纹两 call 并发 claim 只放行一个；并发建候选编号连续不重复）；RLS 测试（两租户逐表互不可见/不可写、未设身份读写失败、admin 全见、触发器拒绝跨租户子行）；迁移测试（空库应用、重复启动幂等、库版本较新时拒绝）；角色检查（超级用户拒绝启动）。

---

## 8. 删除清单（P4）

- 代码：`webapi/store.go:13-14` 的 sqlite 导入与 `sql.Open("sqlite"…)`；`verification_store.go:319-395` 阶段机制；`main.go:121-122` URI 分隔符校验、`main.go:161,168-171` 的 `hub.sqlite`；测试中全部 `hub.sqlite`/SQLite 专用断言。
- 补丁：`native/patches/store_sqlite.go.patch`、旧 `patches/README.md` 内容（改为 PG overlay 说明）；`cyber-go-overlay.py` 中旧 `OLD/NEW` 单行替换；`var/cyber-sqlite-overlay/` 改名 `var/cyber-overlay/`。
- 脚本：`scripts/cyber-ui-http-check.py:165` 的 `/state/hub.sqlite` 探测路径（改探测 state 内其它私有文件）；`demo-stack.py` 改连 PG。
- 文档：`AGENTS.md`（§6“应用 SQLite 补丁”）、`native/RUNNING.md:5,13,34`、`native/patches/README.md`、`native/DEPENDENCIES.md`（12 处）、`docs/architecture.md`（3）、`native/ASSETS.md`、`NOTIFY.md`、`SUBAGENT.md`、`docs/STATUS.md`（主会话改）。
- 依赖：`go.mod` 去掉直接依赖 `bun/dialect/sqlitedialect`，新增 `bun/dialect/pgdialect v1.3.0`、`jackc/pgx/v5`（版本与现 `v5.7.2` 统一或升级，取 MVS 结果）。**`modernc.org/sqlite` 无法从模块图移除**：上游根模块 `go.mod` 要求它，且 `go mod tidy` 不支持 `-overlay`，会按上游原 `store_sqlite.go` 的导入保留它（变为 `// indirect`）。影响仅限 go.mod/go.sum 记录；用 overlay 构建的 Hub 不再链接它——P4 用 `cyber-go-overlay.py -- list -deps ./cmd/wscyber-hub` 验证闭包无 `modernc.org/sqlite`、`sqlitedialect`（脚本需增 `list` 子命令）。`hub-dependency-licenses.json` 相应删除 modernc/sqlitedialect、增加 pgdialect/pgx/puddle 等实际可达模块。

---

## 9. 实施拆片

| 片 | 范围 | 可写文件 | 验证 | 依赖 | 风险 | 执行 |
|---|---|---|---|---|---|---|
| **P1** PG 基础设施 | `native/pgschema`（连接配置：`--database-url-file`/`WSCYBER_DATABASE_URL`、角色检查、单实例锁、bun/migrate 运行器、`0001_baseline`：全部表+索引+tenant_id+触发器+种子默认租户，RLS 先不启用）；`native/internal/testdb`；go.mod 加 pgdialect/pgx（需代理） | `native/pgschema/**`、`native/internal/testdb/**`、`native/go.mod/sum` | `go test ./pgschema ./internal/testdb`（空库迁移、幂等、库较新拒绝、超级用户拒绝、列⊇模型声明一致性、触发器继承/拒绝） | 用户/主会话完成 §7.2 系统操作 | 基线与模型列漂移（一致性测试兜底） | 后端核心 |
| **P2** 原生存储 overlay | overlay 清单机制；PG 版 `store_sqlite.go/store_schema.go/store_models.go` + `store_pg_identity.go`；契约测试 | `native/patches/cyber/**`、`scripts/cyber-go-overlay.py`、`native/pgschema/storetest/**` | `cyber-go-overlay.py -- test ./pgschema/storetest`（会话/事件/账本/制品契约）；上游源哈希不符时脚本失败 | P1 | 移植漏行为；本片后 Hub 暂不可运行（webapi 仍 SQLite），预览/演示保持旧二进制直到 P3 | 后端核心 |
| **P3** webapi 存储迁 PG | `openTaskStore(dsn)`；§5 全部 SQL 改写；52 处 `*sql.DB` 改走 bun；`read/write` 助手（身份 GUC + 全局写锁）；删阶段机制与 28 个测试；其余测试改 `testdb`；新增 2 个并发测试；Hub `main.go` 接 `--database-url-file` 并启动迁移 | `native/webapi/**`、`native/cmd/wscyber-hub/**` | `cyber-go-overlay.py -- test ./webapi ./cmd/wscyber-hub -count=1` 全过；演示环境（PG 演示库）主会话走一遍建任务→候选→验证→报告 | P2 | 改写面最大（64 文件）；并发语义 | 后端核心 |
| **P4** 删除 SQLite 与文档 | §8 全部；`demo-stack.py` 建/删演示库；RUNNING/AGENTS/DEPENDENCIES/许可清单 | `scripts/**`、`native/*.md`、`native/patches/**`、`native/*licenses.json`、`AGENTS.md`、`native/go.mod` | `grep -ri sqlite` 仅剩说明“已移除”的文字；`list -deps` 闭包无 sqlite；`demo-stack.py up` + 浏览器全链 | P3 | 许可清单遗漏 | 后端一般 |
| **P5** 租户与 RLS | 启用 RLS/FORCE 与策略函数；单行设置表改每租户；`webapi_counters` 取号；后台任务逐租户；全局写锁收窄到租户；审计 `tenant_id/actor_user_id`；待审批按租户过滤；身份固定为默认租户 admin | `native/pgschema/**`、`native/webapi/**`、`native/patches/cyber/**` | RLS 逐表测试（两租户合成数据）、后台任务测试、`webapi` 全包；演示环境全链不回退 | P4 | 未包装查询在 RLS 下读空（fail-closed，测试可见）；遗漏后台路径 | 后端核心 |
| **P6a** 登录与角色（后端） | overlay 替换 `auth.go`；users/user_sessions 读写；argon2id；`wscyber-hub user add --admin|--tenant` 子命令；admin 租户切换头；角色校验（管理类接口仅 admin）；访问密钥映射 system | `native/patches/cyber/**`、`native/webapi/**`、`native/cmd/wscyber-hub/**` | 登录/登出/过期/禁用、普通用户越租户 403、admin 切换、节点仍可用密钥连接 | P5 | 上游前端仍发 token 登录（P6b 前演示用 CLI 建的管理员） | 后端核心 |
| **P6b** 登录界面 | 用户名口令登录页（覆盖上游 `runtime/auth.ts` 与登录组件）、当前用户/登出入口、401 处理 | `native/ui/source-overlay/**` | tsc/Vite；演示环境登录/登出/错误口令 | P6a | — | 前端 |
| **P7** 管理界面 | admin：租户列表/新建/归档、用户列表/新建/重置口令/禁用、顶部租户切换；普通用户隐藏管理入口与全局配置；审计显示操作者 | `native/webapi/**`（管理接口）、`native/ui/source-overlay/**` | 定向 Go 测试 + 演示环境两角色各走一遍 | P6b | — | 后端一般 + 前端 |

---

## 10. 风险与待用户决定

风险（实施内处理）：
- 上游 overlay 三文件随上游升级需重移植（哈希保护，不会静默）。
- PG 并发语义：P3 用全局写锁先等价，P5 收窄需逐处测试。
- 上游无身份 ctx 视为 system：依赖“所有 `/api/` 请求必须带身份”这一中间件不变量；P6 auth 替换必须保持。
- 演示/测试依赖本机 PG 运行；PG 未起时测试直接失败（按要求不跳过）。

**决定（11-22）**：第 1、2、5 条用户确认按推荐；第 3、4 条主会话按推荐定。本机 PG18 已按 §7.2 就绪（集群原缺 en_US.UTF-8 locale 无法启动，已 locale-gen 补齐；角色 `wscyber_dev` 非超级用户、无 BYPASSRLS、可建库；库 `wscyber_demo`/`wscyber_preview`；连接串在 `~/.config/wscyber/pg-dev.url`、`pg-test.env`，0600，不入库）。

原待决项（均附推荐）：
1. **编号与设置的作用域**：推荐漏洞编号**租户内**从 1 递增；企业微信 Webhook、运营通知、SLA **每租户一份**；模型（LLM）配置**全局、仅管理员**可改。
2. **用户与租户关系**：推荐**一个普通用户只属一个租户**；管理员不属任何租户、可切换进入任一租户操作。
3. **数据库角色与连接串**：推荐**只要一个连接串**，该角色是库属主但**非超级用户、无 BYPASSRLS**（启动校验），靠 `FORCE ROW LEVEL SECURITY` 约束；不强制拆“迁移角色/运行角色”两个连接串（可选 `--database-migrate-url-file`）。
4. **迁移时机**：推荐 **Hub 启动时自动迁移**（迁移锁 + 库版本较新则拒绝启动）；升级前备份由部署方 `pg_dump` 负责；不提供 down 迁移。
5. **访问密钥的去留（P6 起）**：推荐保留访问密钥，仅作节点/自动化凭据（映射 system 身份）和首个管理员的引导；浏览器只用用户名口令登录；首个管理员用 `wscyber-hub user add --admin` 命令创建。
