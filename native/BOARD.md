# 任务黑板（M2 后端）

平台持有的任务级黑板：**地图**（接口清单，自动从流量整理 + 模型补充）、**事实**（有证据的客观信息）、**Oracle**（具体可证伪的待证明问题＝还未走的路）。给模型工具读写、给前端只读与少量人工操作接口。M3 的 Goal 评估器读 `boardDigest` 摘要，M4 做界面。

不变量：**Oracle 成立 ≠ 漏洞确认**——`holds` 必须关联一个同任务候选（`webapi_finding`），之后照旧走独立验证；黑板不发送请求、不扩大授权范围。本片只做后端：仅临时库合成数据 Go 测试通过（`board_test.go`），前端、真实 Hub 数据未验。

代码：`native/webapi/board.go`（表、摄取、存储、计数与摘要）、`board_tool.go`（四个模型工具）、`board_http.go`（HTTP 与审计）。

## 表（均带 `task_id`，id 由平台生成）

| 表 | 列 | 说明 |
|---|---|---|
| `webapi_board_endpoints` | `id, task_id, endpoint_key, method, origin, path, params_json, status_codes_json, auth_seen, request_count, first_seen_at, last_seen_at, source, note` | `id`=UUIDv5(task_id\nendpoint_key)；`endpoint_key` 用 `endpointKeyOf` 规则（`METHOD origin/路径`，去 query/fragment，纯数字/UUID/≥16 位 hex 段→`{id}`，去尾 `/`）；索引 `(task_id, endpoint_key)` 唯一 |
| `webapi_board_ingest` | `task_id PK, last_created_at, last_id` | 摄取游标 |
| `webapi_board_facts` | `id, task_id, kind, subject, statement, confidence, evidence_ids_json, status, session_id, turn_id, call_id, created_at, updated_at` | 索引 `(task_id, created_at DESC, id DESC)` |
| `webapi_board_oracles` | `id, task_id, number, question, category, endpoint_keys_json, priority, status, origin, claimed_session_id, result_summary, evidence_ids_json, finding_id, dedupe_key, created_at, updated_at` | `number` 存整数、对外呈现 `O-001`（任务内递增，>999 时自然变 4 位）；`dedupe_key` 为内部去重键；索引 `(task_id, number)`、`(task_id, dedupe_key)` 均唯一 |
| `webapi_board_oracle_events` | `id, oracle_id, task_id, from_status, to_status, actor（model\|user\|ai）, session_id, at, summary` | 只追加状态变化（含创建：`from_status` 为空串）；按 `at, rowid` 排序 |

时间均为定宽 UTC 毫秒（`2006-01-02T15:04:05.000Z`）。已知旧库阶段（审计 26 表、漏洞处置 28 表）经 `PrepareWebAPISchema` 单事务补表到 33 表。

### 端点字段

- `params_json`：见过的 query 参数名 + JSON 对象顶层字段名（`Content-Type` 含 `json`，或无 `Content-Type` 且体以 `{` 开头）+ 表单字段名（`application/x-www-form-urlencoded`）；去重排序，≤100 个（满后不再加入新名），单个名 ≤100 字节、跳过非 UTF8/控制字符。
- `status_codes_json`：见过的状态码集合（升序，≤50 个）。
- `auth_seen`：请求是否带非空 `Authorization` 或 `Cookie`：`none` / `with` / 两者都见过为 `both`；**从未被请求的模型补充端点为空串**。
- `request_count`、`first_seen_at`、`last_seen_at`：按请求 `created_at`；模型补充端点为登记时间、计数 0。
- `source`：`traffic`（来自流量）/ `model`（模型补充且尚未被请求）；模型补充端点之后被实际请求即转为 `traffic`（`note` 保留）。

### 摄取

`webapi_requests` 中 **已发送（`sent=1`）且有状态码** 的记录按 `(created_at, id)` 游标增量并入地图，幂等；拒绝、待审批、无响应的 unknown 不入。在任何黑板读写（工具或 HTTP、`boardDigest`）前执行，不挂钩 `webapi_http`。
- 仍在认领中（`completed_at` 为空）且创建不足 2 分钟的请求挡住游标，等它完成后与其后的请求一并并入；超过 2 分钟仍未完成（进程中断遗留）的跳过。
- 已知边界：游标依赖 `created_at` 字符串顺序（与证据列表一致）；若某请求在算出 `created_at` 后、写入前停顿超过其后一次完整请求与一次黑板读取，该请求会被跳过（毫秒级窗口）。

## Oracle 状态

`open` →（claim）`in_progress` →（close）`holds` / `refuted` / `blocked`；`open`/`in_progress` →（drop）`dropped`。`holds/refuted/blocked/dropped` 为终态：模型再 claim/close/drop 返回“冲突（409）……请用户先重开”；用户可经 HTTP 把任意状态重开为 `open` 或放弃为 `dropped`。重开时清空 `claimed_session_id/result_summary/evidence_ids/finding_id`，原结论写进该事件 `summary`。

去重：同任务 + 同 `category` + 同端点集合（归一、去重、排序）+ 规范化问题（小写、去空白与标点）已存在（**任意状态**）时返回已有项、不新建。`endpoint_keys` 每项须为 `METHOD URL`（地图键本身也是该形式），按 `endpointKeyOf` 归一；地图中不存在的键自动补入（`source=model`）。

“尚无 Oracle 引用的端点”（untested）= 没有任何 Oracle（任意状态）的 `endpoint_keys` 包含该端点。

## 模型工具（注册在 `ToolExtension`）

任务内任意会话（研究 / 验证 / 复测 / 子 Agent）可读；**写只允许该任务研究会话（`initial_session_id`）及其 `oracle_explorer` 子会话**，其他会话返回“只有本任务研究会话可写黑板……”。

认领排他与子会话（M5a，见 `native/SUBAGENT.md`）：子 Agent 派出时平台把 Oracle 置 `in_progress`（事件 actor `ai`、summary“子 Agent 认领”），启动后 `claimed_session_id` 为子会话 id；子会话可写地图/事实、可 propose 新 Oracle，只能 close/drop 自己认领的那一个、不能 claim 其它；子 Agent 持有（运行中或预占中）的 Oracle 研究会话不能 claim/close/drop（409）。子会话结束未结案 → 平台退回 `open`（summary“子 Agent 结束未给结论（<stop>）”）。事件 actor 新增 `ai`（平台代子 Agent 生命周期写入）。session/turn/call 只取自可信 Invocation。参数为单个 JSON 对象，未知字段与本操作用不到的字段忽略、不报错；字段值非法逐字段说明（如 `items[0].category 仅支持 …`），校验失败整批不写。

| 工具 | 参数 | 结果 |
|---|---|---|
| `webapi_board_read` | `{section: summary\|endpoints\|facts\|oracles, status?, category?, limit?(1..100，默认 50)}`；summary 忽略 status/category/limit（limit 取值仍校验），endpoints 忽略 category；endpoints 的 `status` 只支持 `untested`；facts 的 `status` 为 `active`（默认）/`retracted`、`category` 即 kind；oracles 的 `status`/`category` 为对应枚举 | 紧凑文本。summary＝`boardDigest(…, 2000)`；oracles 按 in_progress、open、blocked、holds、refuted、dropped，再按优先级、编号 |
| `webapi_board_map` | `{endpoints:[{method, url, note?(≤500 字)}]}`（1..50） | `{endpoints:[{endpoint_key, created, source}]}`；已存在的键不重复，`note` 非空时覆盖 |
| `webapi_board_fact` | `{add?:[{kind, subject(≤200 字), statement(≤2000 字), confidence, evidence_ids?(≤20)}](≤20), retract?:[id](≤20)}` | `{added, retracted, already_retracted}`。kind：`endpoint\|auth\|role\|tech\|config\|data\|other`；confidence：`confirmed`（须 ≥1 条证据）/`inferred`。证据须为同任务已发送的 evidence_id。事实 id = `bf_`+UUIDv5(task, session, turn, call, 序号)，同一 call 重放不重复新增；撤回不存在的 id 整批拒绝 |
| `webapi_board_oracle` | `action: propose` `{items:[{question(≤500 字), category, endpoint_keys(0..20), priority}](1..20)}`；`claim {number}`；`close {number, status: holds\|refuted\|blocked, summary(≤2000 字), evidence_ids?(≤20), finding_id?}`；`drop {number, summary}` | propose → `{items:[{number, created, status, id}]}`（`created=false` 为去重命中）；其他 → `{number, status, changed}`。holds 需 `finding_id`（同任务候选）+ ≥1 证据；refuted 需 ≥1 证据；blocked 只需 summary；`finding_id` 只用于 holds，其它结论带了忽略、不写入；与 action 无关的字段忽略。number 接受 `O-001`/`o-1`/`1` |

category：`authz|authn|injection|ssrf|file|logic|info_disclosure|config|other`；priority：`high|medium|low`。

## `boardDigest(ctx, taskID, maxChars)`（M3 复用，签名稳定）

`(*taskStore).boardDigest(ctx context.Context, taskID string, maxChars int) (string, error)`；`maxChars<=0` 取 2000。先摄取，再按行输出：
1. `地图：N 个端点（流量 a / 模型补充 b），其中 U 个尚无任何 Oracle 引用`，及未被引用端点前 15 条（已请求的在前，按键）。
2. `Oracle：open … / in_progress … / holds … / refuted … / blocked … / dropped …`，及 open/in_progress 中按优先级（再按编号）前 10 条：编号、优先级、状态、类别、问题（≤160 字）、端点（≤3 个，余数 `+N`）。
3. 最近 5 条成立/不成立结论（按 `updated_at` 倒序）：成立显示关联候选编号并注明“待独立验证”。
4. `事实：N 条有效（confirmed c / inferred i）`，及最近 10 条 confirmed 事实。

总长（按字）超过 `maxChars` 时整行截断，以 `…（摘要已截断，用 webapi_board_read 分区查看）` 结尾。

## HTTP（`/api/webapi`，原生鉴权内）

列表约定同 `OVERVIEW.md`：`limit` 1..200（默认 50），`q` ≤200 字不区分大小写，`cursor` 为上页 `next_cursor`（不透明，非法 400），末页 `next_cursor` 为 null；错误体 `{error}`；任务不存在 404。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/tasks/{id}/board` | `{task_id, digest, counts:{endpoints, traffic_endpoints, model_endpoints, untested_endpoints, facts(有效), confirmed_facts, oracles:{open,in_progress,holds,refuted,blocked,dropped}}}` |
| GET | `/tasks/{id}/board/endpoints?q=&untested=0\|1&limit=&cursor=` | 按 `endpoint_key` 升序；`q` 匹配端点键与备注；项 `{id, endpoint_key, method, origin, path, params, status_codes, auth_seen, request_count, first_seen_at, last_seen_at, source, note, oracles:["O-001"…]}` |
| GET | `/tasks/{id}/board/facts?kind=&status=active\|retracted&limit=&cursor=` | 按 `created_at, id` 倒序；`status` 缺省返回全部；项 `{id, kind, subject, statement, confidence, evidence_ids, status, session_id, turn_id, call_id, created_at, updated_at}` |
| GET | `/tasks/{id}/board/oracles?status=&category=&limit=&cursor=` | 按编号升序；项 `{id, number, question, category, endpoint_keys, priority, status, origin, claimed_session_id, result_summary, evidence_ids, finding_id, finding_number, created_at, updated_at, events:[{from, to, actor, session_id, at, summary}]}` |
| POST | `/tasks/{id}/board/oracles` | 用户提出，body 同 propose 单项（strict）；`origin=user`；新建 201 `{oracle, created:true}`，去重命中 200 `{oracle, created:false}` |
| PUT | `/tasks/{id}/board/oracles/{number}` | `{priority?, status?: open\|dropped}`（至少一项，strict）；`{number}` 接受 `O-001`/`1`；不存在 404；无变化 200 `changed:false` |

审计（actor `user`，`target_kind=board_oracle`，`target_id`=Oracle id，带 task_id 与任务所属资产）：`board.oracle.create`（detail `{number, category, priority, endpoint_keys}`；去重命中不登记）、`board.oracle.update`（detail `{number, changes:{priority|status:{from,to}}}`；无变化不登记）。模型工具写入不登记审计（经会话事件可追溯）。
