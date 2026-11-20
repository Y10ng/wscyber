# 资产登记与从资产发起任务（C1 后端）

资产是服务端 `webapi_assets` 实体（与上游浏览器“资产池”无关）。路由均在原生鉴权内（`/api/webapi`）。仅临时库合成数据 Go 测试通过；前端、真实节点端到端未验。

- `GET /assets?archived=0|1`（默认 0）→ `{assets:[Asset]}`，最近更新在前；`GET /assets/{id}` → Asset，不存在 404。
- `PUT /assets/{id}`（id 由客户端生成，规则同任务 id）`{name, owner, environment, known_info, objective, origins[], forbidden, scope?, approval?, archived?}`：整份覆盖、保存即生效，新建 201、更新 200（保留 `created_at`）。校验：name 去首尾空白后必填、≤128 字；origins ≥1，规范化/去重/排序同任务；scope/approval 枚举同 `POLICY.md`，空＝`origins`/`high_risk`。未知字段忽略。
- Asset = 以上字段 + `id, created_at, updated_at, credentials:[{id,label,username,has_secret,note,updated_at}]`；**任何响应都不含 secret**。
- `PUT /assets/{id}/credentials/{cid}` `{label, username, secret?, note}`（未知字段 400）：secret 省略或 `""` 保留原值；`cid` 已属其他资产 409。`DELETE` 同路径，不存在 404。两者返回资产视图。
- 密文：AES-256-GCM（AAD 绑定 asset_id+credential_id），列存 `v1:base64(nonce‖密文)`。密钥 `<state-dir>/webapi-secrets.key`（与 hub.sqlite 同目录，64 位 hex，0600 普通文件，首次写 secret 时 `O_EXCL` 生成）。库中已有密文而密钥缺失：写新 secret 与发起任务均报“密钥文件缺失…不会生成新密钥覆盖”，原密文保留；权限非 0600 拒用。
- `PUT /assets/{id}/tasks/{task_id}` `{objective?, scope?, approval?}`（可空体）= Go `(*Capability).LaunchAssetTask(ctx, assetID, taskID, AssetLaunchOptions)`：
  1. 单事务建任务（known_info＝资产已知信息 + “环境：/负责人：”行，三者都为空时为 `资产：<名称>`；objective 缺省用资产；origins 用资产）+ 写策略（缺省资产默认，含变更记录）+ `webapi_asset_tasks` 关联。task_id 已被非本资产任务占用 409。
  2. 首条消息 = 前端 `webApiFirstMessage` 原文 + “测试账号”块（发起时解密 名称/用户名/密码，只进 Run 输入，不写 webapi_tasks）+ “禁止行为（提示词约束，非拦截）”块。
  3. 节点同前端：在线且公告 `wscyber.webapi-http.v1`，按 node_id 取第一个；本机受控模型节点（`wscyber.local-model.v1`）须已配置 provider/model，Run `max_turns=3`，其余 0。会话已存在则沿用原绑定节点。
  4. 原生 `Sessions.OpenSession`（session_id＝task.initial_session_id）→ `Sessions.RunTurn`，每次尝试新 requestID（上游账本会回放被拒结果）。
  - 幂等/续完：同进程按 task_id 串行；任务已存在沿用原内容（不按资产现值重算）；会话已存在不再 Open；会话事件前 500 条中已有 user 消息即视为已启动，不再 Run。
  - 响应：201（本次新建任务）/200（续完或 `idempotent:true`）`{asset_id, task, session_id, node_id, turn_id, progress:{task_created,session_opened,turn_started}, created, idempotent}`。失败 `{error, stage, progress, task, session_id, node_id}`：无可用节点/绑定节点离线/节点拒绝 UNAVAILABLE/本机模型未配置 → 409；资产不存在 404；参数 400；密钥问题 `stage=first_message` 500；会话服务异常 502/503。
- `GET /assets/{id}/tasks` → `{asset_id, tasks:[{task_id, objective, initial_session_id, created_at, finding_count}]}`，最近在前。任务 GET（含 `?session_id=` 与 PUT 响应）带 `asset:{id,name}|null`。
- 已知限制：测试账号明文进入首条用户消息，因而随原生会话事件（`chat_aop_events`）持久化并在执行视图可见；禁止行为只是提示词约束。

## 周期计划（C2a 后端）

每资产一个计划（`webapi_asset_schedules`），到点由 Hub 自动从资产发起任务（复用 `LaunchAssetTask`，资产默认 objective/scope/approval）。仅临时库合成数据 Go 测试通过；前端、真实节点端到端未验。

- `PUT /assets/{id}/schedule` `{cron, enabled?, auto_verify?, auto_retest?}`（未知字段 400；三者省略＝true，整份覆盖，开关存 `webapi_schedule_options`，无行＝开启）：cron 用上游 `agent.ParseCron`（5 字段：分 时 日 月 周，支持 `*`、`*/n`、`a-b[/n]`、列表），按 **Hub 本地时区**解释，4 年内无触发（如 `0 0 30 2 *`）400。保存即生效：按当前时间重算 `next_run_at`（停用为 null）。新建 201、覆盖 200。资产不存在 404。
- 响应/`GET` = `{asset_id, cron, enabled, auto_verify, auto_retest, next_run_at, upcoming[≤3], timezone, updated_at, last_run}`；时间均为 UTC RFC3339（`...Z`），`timezone` 如 `Local (+08:00)`。未设置计划 GET 404。`DELETE` 删除计划与开关（运行记录与后续明细保留），不存在 404。
- `GET /assets/{id}/schedule/runs?limit=1..200(默认50)&cursor=` → `{items:[{id, asset_id, scheduled_for, task_id, status, error, created_at, followups}], has_more, next_cursor}`（`followups` 见下节；`last_run` 同形），按 `scheduled_for` 倒序。`GET /schedules` → `{schedules:[计划视图 + asset_name, asset_archived], timezone}`，启用且最早到期在前。
- 运行记录 `webapi_schedule_runs`，`(asset_id, scheduled_for)` 唯一：
  - `launched`：任务已建并 Run 被接受；`task_id` 为该任务。
  - `failed`：`LaunchAssetTask` 失败，`error` = `阶段：文案`（如 `select_node：当前没有…在线节点…`）；任务已建时带 `task_id`（可在资产任务列表里查看/手动经 `PUT /assets/{id}/tasks/{task_id}` 续完），校验阶段失败为空。**不自动重试**，`next_run_at` 照常推进。
  - `skipped`：资产已归档 / 上一次计划任务仍在运行（error 带其 task_id）/ 计划在检查过程中被停用。不排队。
- 调度器：`Routes` 拿到原生管理 API 时启动、`Capability.Close` 停止的单 goroutine，每 30s 检查 `enabled 且 next_run_at ≤ now` 的计划（Hub 启动后第一次检查在 30s 后）：
  - `task_id` = UUIDv5(固定命名空间, `asset_id + "\n" + scheduled_for`)：重复检查、发起后写记录前中断等情况下同一应运行时间只对应一个任务，依赖 `LaunchAssetTask` 按 task_id 幂等/续完；记录插入 `ON CONFLICT DO NOTHING`。
  - Hub 停机错过多次：只补最近一次应运行时间（`scheduled_for` 记该时间），然后 `next_run_at` = cron 在当前时间之后的下一次。
  - “上一次仍在运行”判定（`apiLauncher.SessionRunning`，查原生会话状态）：上一次 `launched` 任务的研究会话（`initial_session_id`）存在 **且** 其绑定节点在线且原生 `AgentView.Busy`（该连接有已派发未结束的轮次）**且** 会话持久化事件里有已开始（用户消息/TurnStarted）却无 `TurnEnded` 的轮次。Busy 条件用来排除 Hub 重启后遗留的无终态旧轮次。Busy 是节点级：同节点另有会话在跑时，本会话只要事件里也有未结束轮次才会算运行中。无法核对（读取出错）时本轮不发起、保持到期、不记 run。
  - 全局并发上限 2：统计各资产最近一次 `launched` 计划任务中仍在运行的数量（无法核对按运行中计）；达到上限本轮不发起、保持到期、不记 run，下轮再试。只计计划任务，不含手动发起的任务。
  - 归档资产记 skipped；`enabled=false` 不会到期（`next_run_at` 为空），不发起不记录。期间人为重新保存计划时，按新的 `next_run_at` 处理，不覆盖。
  - Hub 关闭打断进行中的发起：不记 failed，`next_run_at` 不推进，重启后同一 `scheduled_for`（若期间未跨过下一次触发）用同 task_id 续完。
- 计划任务的审批沿用任务策略（资产默认）；无人值守时待审批 10 分钟超时拒绝（B1 现状）。

## 计划后续（C2b 后端）

计划研究结束后，调度器（同一 goroutine、同一次 30s 检查）无人值守接着做“候选验证 → 已知漏洞复测”。仅临时库合成数据 Go 测试通过；前端、真实节点端到端未验。

- 触发：扫描 `launched` 且后续未完成的计划运行——已规划未完成的，或未规划且是该资产**最近一次** launched 运行（被更新的计划运行取代时不再补规划；旧库升级后也只会规划各资产最近一次）。研究会话 `SessionRunning=false` 且事件里至少一个 `TurnEnded`（launcher `TurnEnded`）才规划；判定出错本轮跳过。手动从资产发起的任务没有 run，不触发。
- 规划（单事务写运行级标记 `webapi_schedule_run_followups` 与全部 pending 项，开关取当时值快照）：
  - `auto_verify`：本次任务中**还没有任何验证**的候选（当前修订）→ 验证项，`task_id`＝本次任务。
  - `auto_retest`（C3 起按漏洞条目去重）：先同步资产漏洞库（见下节），对状态 `new`/`present` 且**本次任务没有成员**的条目（本轮重新发现的由本轮验证覆盖），取一个代表成员复测——最新决定性“存在”事件（验证 supported / 复测 reproducible）所属的候选，要求其任务在本次任务之前关联（`webapi_asset_tasks.created_at` 更早，含手动发起）；在其原任务下按其**当前修订**复测，`task_id`＝原任务（授权沿用原任务 origins）。`fixed`/`pending`/`unconfirmed` 条目不复测；同条目其余候选不复测。
  - 项表 `webapi_schedule_followups(id, run_id, kind, task_id, finding_id, revision_id, target_id, status, error, created_at, updated_at)`，`(run_id, kind, finding_id)` 唯一；`target_id` = UUIDv5(固定命名空间, `run_id\nfinding_id\nrevision_id\nverify|retest`)。
- 发起（每个 pending 项）：先查跳过——资产不存在/已归档、计划已删除、对应开关已关闭 → `skipped`（文案即原因）；验证项若候选已有本项以外的验证 → `skipped`“候选已有验证”。再用 `target_id` 走现有验证/复测认领（`saveVerification`/`saveRetest`，同 ID 回放），然后与 `LaunchAssetTask` 共用的 `startSession`：会话已存在不再 Open、已有用户消息不再 Run，节点选择同资产发起。首条消息由 Go `pinnedFirstMessage` 生成，与前端 `webApiVerificationFirstMessage`（`hooks/useChatSession.ts`）/`buildWebApiRetestFirstMessage`（`lib/retest-message.ts`）逐行一致。
  - 成功 → `launched`；确定失败 → `failed`，`error`＝`阶段：文案`（`create`/`select_node`/`open_session`/`run_turn` 等，文案里对象为“验证/复测已保存…”），**不自动重试**。存储异常或 Hub 关闭 → 保持 pending，下轮/重启后以同 ID 续完，不重复建或 Run。
- 并发：全局上限 `scheduleMaxConcurrent`(2) = 运行中的计划研究 + 运行中的后续会话（未完成运行里已发起项，`SessionRunning`，无法核对按运行中计）。每次检查先处理到期计划（研究优先），剩余名额再发后续；满了保持 pending。
- 完成：没有 pending 项、本轮没有刚发起的项、已发起的后续会话都不在运行 → 写 `completed_at`，此后不再扫描。
- 摘要 `followups`（仅 launched 运行，其余为 null）：`{state: waiting|not_planned|in_progress|completed, completed, auto_verify, auto_retest, planned_at, completed_at, verify:{pending,launched,failed,skipped}, retest:{…}}`；未规划时开关/时间为 null。
- `GET /api/webapi/schedule-runs/{run_id}/followups` → `{run: 运行视图(含 followups), items:[{id, kind, task_id, finding_id, finding_number, finding_title, revision_id, target_id, session_id, status, error, verdict, conclusion, created_at, updated_at}]}`（验证项在前；`session_id`/`verdict`/`conclusion` 尚无为 null）；运行不存在 404。
- 旧库：C2a 阶段（十七表）补 `webapi_schedule_options`、`webapi_schedule_run_followups`、`webapi_schedule_followups` 三表与唯一索引，不改原表。
- 已知限制：研究会话因 Hub 重启缺 `TurnEnded` 时该运行停在 waiting，直到下一次计划运行将其取代（not_planned）；同资产下一次计划研究不会等待上一次的后续会话结束（仍受全局上限约束）。

## 资产漏洞库（C3 后端）

把同一资产各轮任务的候选归并成“漏洞条目”，资产层面看“新发现 / 仍存在 / 已修复”。状态读时派生、不存储；归并关系可人工修改。仅临时库合成数据 Go 测试通过；前端、真实节点端到端未验。

- 表：`webapi_asset_vulns(id, asset_id, endpoint_key, title_override, created_at, updated_at)`、`webapi_asset_vuln_members(finding_id PK, vuln_id, asset_id, task_id, link auto|manual, created_at, updated_at)`；一个候选最多属于一个条目。旧库：C2b 阶段（二十表）补这两表与三个索引，不改原表。
- **端点键**：候选当前修订证据快照里第一条 `supporting`（无则第一条任意关系）的 `METHOD origin/规范化路径`——METHOD 大写；origin 同任务 origin 规范化（小写、去默认端口）；去 query/fragment；纯数字、UUID、≥16 位 hex 的路径段替换为 `{id}`；去尾 `/`（根路径保留 `/`）。无证据或 URL 不能解析为 http(s) 的候选为 `finding:<finding_id>`（自成一条）。
- **同步**（按资产串行、单事务、幂等）：资产关联任务（`webapi_asset_tasks`）中尚未入库的候选按端点键加入 auto 条目，id = UUIDv5(固定命名空间, `asset_id\nendpoint_key`)，不存在则建，`link=auto`。已入库成员**不再重新计算**（候选改修订也不挪动）。触发：漏洞库各 API 处理前、C2b 规划复测前。
- **人工操作**写 `link=manual`，同步永不覆盖。人工条目 id 由客户端生成（规则同任务 id），`endpoint_key` 为空。成员被移空：人工条目删除；auto 条目保留行但不返回（GET 404），之后同键新候选仍落入该条目。
- **状态**：收集条目全部成员的决定性事件——验证判读 `supported`（存在）、复测 `reproducible`（存在）、复测 `fixed_in_scope`（已修复），时间为结果保存时间（按时间解析比较）。有决定性事件取最新一条：已修复 → `fixed`；存在 → 条目最早成员（候选创建时间最早）所属任务是该资产最近一次任务（`webapi_asset_tasks` created_at 最新，含手动）时 `new`，否则 `present`。无决定性事件：有任何已出结果的验证 → `unconfirmed`，否则 `pending`（只有 inconclusive 复测也是 pending）。
- API（原生鉴权内；资产不存在 404；未知字段 400；错误体 `{error}`）：
  - `GET /assets/{id}/vulns?status=new|present|pending|unconfirmed|fixed`（其他值 400）→ `{asset_id, latest_task_id|null, counts:{new,present,pending,unconfirmed,fixed}（全量，不受过滤影响）, vulns:[View]}`；排序 new, present, pending, unconfirmed, fixed，同状态按 `last_event.at`（无事件用最新成员创建时间）倒序。
  - View = `{id, title, status, endpoint_key, first_seen_at, last_confirmed_at, fixed_at, last_event:{kind verify|retest, value, at, task_id, finding_id}|null, member_count, task_count, members:[{task_id, finding_id, number, title, link, latest_verdict, latest_retest, created_at, initial_session_id}]}`：title = `title_override` 或最新成员当前修订标题；`first_seen_at` = 最早成员候选创建时间；`last_confirmed_at` = 最新“存在”事件时间；`fixed_at` 仅 fixed 时为最新已修复事件时间；`last_event` = 最新一条结果事件（**含非决定性**，如 counterevidence、inconclusive）；成员按候选创建时间倒序，`created_at` 为候选创建时间，`latest_verdict`/`latest_retest` 为该候选最新保存的判读/结论（无为 null）。
  - `GET /assets/{id}/vulns/{vid}` → View（不存在/已移空 404）；`PUT` 同路径 `{title}`（必填，去首尾空白，≤200 字，`""` 清除覆盖）→ View。
  - `PUT /assets/{id}/vulns/{vid}/members/{finding_id}`（空体或 `{}`）→ 把候选移入该条目；`vid` 不存在则新建人工条目（拆分，201），否则 200（已在该条目不变）。候选不存在 404；候选不属于该资产的任务 409；`vid` 属于其他资产 409。返回目标条目 View。
  - `POST /assets/{id}/vulns/{vid}/merge` `{from}` → from 全部成员移入 vid（`link=manual`），from 的标题覆盖丢弃；同 id 400；任一不存在（或已移空、属其他资产）404；返回 View。
- 人工处置（严重度/处置/负责人/修复期限，V1）按条目 `vid` 另存，与上述派生状态分开；条目 View 带 `triage`，合并时目标无记录则继承 from 的记录，计划后续复测跳过处置为误报的条目。见 `TRIAGE.md`。
- 已知限制：端点键只看一条证据，同端点不同问题会归入同一条目、同问题不同端点会分开，需人工拆分/合并；归并不跨资产；人工条目 id 与将来 auto 条目 id 碰撞的可能性按 UUID 忽略。
