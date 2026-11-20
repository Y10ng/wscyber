# 全局导航只读接口（U2a 后端）

前端“左侧固定菜单”（总览 / 任务 / 资产 / 漏洞 / 报告）所需的全局列表与总览聚合。全部只读、不新增表与索引、不改已有接口行为。仅临时库合成数据 Go 测试通过；前端、真实 Hub 数据未验。

通用：路由在原生鉴权内（`/api/webapi`）；错误体 `{error}`；时间为 UTC RFC3339。列表参数 `limit` 1..200（默认 50，非法 400）；`q` 去首尾空白、≤200 字、不区分大小写子串；`asset_id` 规则同路径 id（非法 400，不存在则为空结果）；`cursor` 为上页 `next_cursor`（不透明，非法 400），无下一页时 `next_cursor` 为 null。

## `GET /task-list?q=&status=&asset_id=&archived=&limit=&cursor=`

按 `created_at` 倒序（同时间按 task_id 倒序），游标为 `created_at+task_id` 键。`q` 匹配 objective / known_info / origins。`asset_id` 只取该资产关联任务。`archived=0|1`（缺省 `0`，其他值 400）：初始会话（`initial_session_id`）在原生 `chat_sessions.archived=1` 即为已归档，会话行不存在视为未归档；`0` 只返回未归档，`1` 只返回已归档。

项：`{task_id, title, origins[], asset:{id,name}|null, scheduled, created_at, initial_session_id, status, archived, last_activity_at, finding_count, verification_count, supported_count, report_count, retest_count, total_tokens, policy:{scope,approval}}`

- `title` = objective 首个非空行（≤120 字，同 USAGE）。`scheduled` = 存在指向该任务的计划运行（`webapi_schedule_runs.task_id`）。`policy` 无记录时为默认 `origins/high_risk`。
- `status`：研究会话（`initial_session_id`）持久化事件里没有任何带 turn_id 的事件 → `not_started`；否则调 C2a 的 `SessionRunning`：true → `running`，false → `idle`，出错 → `unknown`。`status=` 过滤同取值（其他值 400）。
- `last_activity_at` = 任务全部业务会话（研究/验证/复测/报告）`chat_aop_events` 最新事件时间；无事件为 `created_at`。
- `supported_count` = 每个候选取最新保存的验证判读（同 C3 `latest_verdict`），为 `supported` 的候选数。
- `total_tokens` = 任务全部会话 `Event_Usage` 汇总（解析同 `USAGE.md`，损坏行跳过）。
- 响应 `{items, has_more, next_cursor, counts:{all, running, idle, not_started, unknown, archived}}`；状态计数受 q/asset_id/archived 影响、不受 status 影响。`archived` 为同一 q/asset_id 下已归档任务总数（与 archived 参数无关），供前端“已归档 N”胶囊。

## `GET /vulns?status=&asset_id=&q=&limit=&cursor=`

全局漏洞 = 所有**未归档**资产的 C3 条目（每个资产先按 C3 加锁同步，再用 `assetVulns` 派生）＋ 未关联资产任务里的候选（每个候选一条，`id = finding:<finding_id>`，状态用 C3 同一 `buildAssetVuln` 对单成员计算；该成员所在任务视为“最近一次任务”，故存在类结论为 `new`）。

项：`{id, kind: asset_vuln|task_finding, title, status, endpoint_key, asset:{id,name}|null, task_id, first_seen_at, last_confirmed_at, fixed_at, last_event, member_count, task_count}`；字段语义同 `ASSETS.md`「资产漏洞库」View。`task_id`：task_finding 为候选任务，asset_vuln 为最新成员（候选创建时间最新）的任务。`endpoint_key` 对 task_finding 也按端点键规则计算（仅展示）。

- 排序同 C3：new, present, pending, unconfirmed, fixed；同状态 `last_event.at`（无事件用最新成员创建时间）倒序，再按 id。
- `q` 匹配标题 / 端点键 / 资产名。`asset_id` 只返回该资产条目（不含无资产候选；归档资产为空）。`status` 其他值 400。
- 响应 `{items, has_more, next_cursor, counts:{new,present,pending,unconfirmed,fixed}}`；counts 受 q/asset_id 影响、不受 status 影响。分页为内存排序后按偏移切片（游标编码偏移）；翻页期间数据变化可能重复/漏项。
- 副作用：与资产漏洞接口相同，会把未归档资产尚未入库的候选同步进 `webapi_asset_vulns`/`_members`（幂等）。
- V1 起每项另有 `triage`，并支持 `severity=`/`disposition=`/`overdue=1` 过滤、`counts.overdue`/`counts.by_severity`；`/overview` 的 `vulns` 另有 `open_by_severity`、`overdue`。见 `TRIAGE.md`。

## `GET /report-list?q=&asset_id=&limit=&cursor=`

全部任务的报告，按 `created_at` 倒序（同时间按 report_id 倒序），游标为 `created_at+report_id` 键。`q` 匹配草稿标题 / 任务 objective / 资产名。

项：`{report_id, task_id, task_title, asset:{id,name}|null, session_id, title, has_draft, item_count, created_at, updated_at}`；`title` 为草稿标题（无草稿为空串）；`updated_at` 为草稿保存时间（无草稿 = created_at）。响应 `{items, has_more, next_cursor, total}`（total = 过滤后总数）。

## `GET /overview?days=7|30|90`（默认 30，其他值 400）

```
{ generated_at, timezone, days,
  assets: {total, archived, scheduled_enabled},
  tasks: {total, running, created_in_range, by_day:[{date,count}]},
  vulns: {counts:{new,present,pending,unconfirmed,fixed}, confirmed_by_day:[{date,new,present,fixed}]},
  tokens: {total, by_day:[{date,total_tokens}]},
  schedules: {next:[{asset_id,asset_name,next_run_at}]≤5, recent_runs:[{asset_id,asset_name,scheduled_for,status,task_id,followups_state}]≤5},
  recent_supported:[{vuln_id,title,asset:{id,name}|null,task_id,at,severity,suggested_severity}]≤5,
  todo: {unrated, pending_push, overdue},
  sla: {rate|null, closed_on_time, closed_total, avg_fix_days:{critical,high,medium,low}(数值|null),
        overdue_top:[{vuln_key,title,asset:{id,name}|null,owner_effective,overdue_days}]≤5},
  coverage: {window_days:30, tested, total, stale_top:[{asset_id,name,last_tested_at|null,days_since|null}]≤5},
  running: [{task_id,title,asset:{id,name}|null,started_at,elapsed_seconds,board:{endpoints,covered_endpoints}}]≤5,
  recent_failed_runs: [{asset_id,asset_name,scheduled_for,status,error}]≤3 }
```

- 区间 = 今天（Hub 本地时区）往前 `days` 个本地日（含今天）；`by_day`/`confirmed_by_day` 覆盖每一天，无数据补 0，日期 `YYYY-MM-DD`；`timezone` 同 USAGE。
- `assets.scheduled_enabled` = 未归档且计划启用的资产数。`tasks.running` 用任务列表同一状态判定。
- `vulns.counts` = 全局漏洞列表的 counts（无过滤）。`confirmed_by_day` 基于全局漏洞条目全部成员的结果事件（按保存时间）：条目历史上**第一条** `supported` 判读所在日计 `new`；其后的 `supported` 与任何 `reproducible` 计 `present`；`fixed_in_scope` 计 `fixed`。同一条目同一天同类只计一次（同一天可同时计 new 与 present）。
- `tokens` = `Usage` 全局汇总在该本地日期区间的结果（同 `/usage?from=&to=`）。
- `schedules.next` = 启用、未归档、有 next_run_at 的计划，最早在前；`recent_runs` = 全部资产计划运行按 `scheduled_for` 倒序前 5，`followups_state` 同 C2b 摘要 `state`（非 launched 为 null）。
- `recent_supported` = 有 `supported` 判读的全局条目，按其最新 `supported` 时间倒序前 5；`task_id` 为该判读所属候选的任务。
- `recent_supported[].suggested_severity`（K1）：成员中最新一条带建议等级的 `supported` 验证的 AI 建议等级，与全局漏洞列表同源，无则空串；不写入人工 `severity`，人工定级后仍保留，不参与 SLA/逾期/计数。

OV2 新增（只加字段，旧字段不变；处置/期限派生见 `TRIAGE.md`，“本地日”= Hub 本地时区）：

- `recent_supported[].severity`：条目 triage 严重度，空串 = 未定级。
- `todo`：`unrated` = 全局条目中“待修复”（status ∈ {new, present} 且 disposition ∈ {open, fixing}）且未定级的数；`pending_push` = 通知 `pending_confirm` 数（`NOTIFY.md`）；`overdue` = `/vulns` 无过滤时的 `counts.overdue`（同 `vulns.overdue`）。待审批数不在此处（前端已有来源）。
- `sla`：样本 = 全局条目中 status=fixed、`fixed_at` 的本地日落在 `days` 区间内、且有 `due_effective` 的条目。`closed_on_time` = `fixed_at` 本地日 ≤ `due_effective`（期限当天算准时）；`rate` = closed_on_time / closed_total（无样本 null，原始比值不取整）。`avg_fix_days` = 区间内已修复条目（不要求有期限）按**当前**严重度 critical/high/medium/low 分组，`first_confirmed_at → fixed_at` 天数（小时/24）的平均，保留 1 位小数，无样本 null；info/未定级、无首次存在时间的条目不计。`overdue_top` = 当前逾期条目，`overdue_days` = 今天本地日 − `due_effective`，按逾期天数倒序、同天数按 `vuln_key`，前 5。样本不按处置排除（误报/接受风险的已修复条目同样计入）。
- `coverage`：固定 30 个本地日窗口（含今天），与 `days` 无关。范围 = 未归档资产；某资产在窗口内有关联任务创建（`webapi_asset_tasks.created_at`）即 `tested`。`stale_top` = 全部未归档资产按“最久未测”排序前 5：从未测过的在前（资产创建时间早者优先，再按 id），其余按最近一次关联任务创建时间早者优先（再按 id）。`last_tested_at` = 最近一次关联任务创建时间（存储原值），`days_since` = 今天本地日 − 其本地日；从未测过均为 null。
- `running`：任务列表状态为 `running` 的任务（同 task-list 判定，含已归档任务），按 `started_at` 倒序（同时间按 task_id）前 5。`started_at` = 研究会话首个带 turn_id 的持久化事件时间（按 cursor 最早，UTC RFC3339），`elapsed_seconds` = 生成时刻 − started_at（向下取整秒，≥0；中间空闲时间也计入）。`board.endpoints` = 黑板端点数，`covered_endpoints` = endpoints − untested_endpoints（`BOARD.md` counts）。
- `recent_failed_runs`：全部资产的计划运行中，运行本身 `failed` 或其后续（`webapi_schedule_followups`）有 `failed` 项的，按 `scheduled_for` 倒序（再按 id）前 3。`status` 为运行状态；`error` = 运行失败时取运行的错误，否则取首个失败后续（创建时间最早，再按 id）的错误。`asset_name` 含已归档资产（同 `recent_runs`）。

## 性能说明

- 任务列表：全部任务一条 SQL（含 `EXISTS` 走 `chat_aop_events (session_id, cursor)` 唯一索引判断有无轮次）＋计划运行集合一条；有轮次的任务逐个调 `SessionRunning`（生产实现先看节点 Busy，空闲时不读事件），以便计算 counts。计数、supported、会话集合与事件扫描只对**本页**任务批量 `IN (...)`（每块 500）：约 4+1+3+1 条 SQL；事件扫描按 session_id 走索引，读取这些会话的全部事件行。
- 全局漏洞：每个未归档资产一次同步＋C3 派生（约 5 条 SQL），无资产候选 3 条；内存排序。适合数千条目量级。
- 总览：在上述两者之上加一次 `Usage`（与 `/usage` 相同，按 UTC 日期前缀粗筛后扫描 `chat_aop_events` 区间内全部行）。
- 总览 OV2 字段：复用同一次已加载的全局漏洞条目与 triage 上下文（不重复同步、不重读处置），另加通知计数 1 条、资产与关联任务各 1 条、失败运行 1 条（＋每条非失败运行 1 条取后续错误，≤3）、每个运行中任务 1 条首轮次事件查询（走 `(session_id, cursor)` 索引）＋前 5 个各 3 条黑板计数。
- 未新增索引；数据量增大后可考虑给 `webapi_verifications/reports/retests (task_id)` 与 `webapi_schedule_runs (task_id)` 补索引。
