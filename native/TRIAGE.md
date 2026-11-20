# 漏洞处置（V1 后端）

漏洞条目的人工处置信息：严重度、处置、负责人、修复期限（按严重度 SLA）、备注，以及读时派生的“实际负责人 / 实际期限 / 是否逾期”。**严重度、处置与验证/复测派生状态（new/present/pending/unconfirmed/fixed）分开存、互不覆盖**；处置只由人经 HTTP 设定，模型与工具不写。本片只做后端：仅临时库合成数据 Go 测试通过（`triage_test.go`），前端、真实 Hub 数据未验。

代码：`native/webapi/triage.go`（表、派生、HTTP、审计 helper）；扩展点在 `vuln_store.go`（首次存在时间、合并/移空、复测跳过）、`vuln_http.go`、`global_lists.go`、`overview.go`、`notify.go`。

## 键

全局漏洞 id：资产漏洞库条目为 `vid`（见 `ASSETS.md`「资产漏洞库」）；未关联资产任务的候选为 `finding:<finding_id>`（同 `OVERVIEW.md` `/vulns`）。

## 表

`webapi_vuln_triage(vuln_key PK, asset_id, severity, disposition, owner, due_date, note, created_at, updated_at)`：`asset_id` 无资产为空串。无记录 = 默认（未定级、`open`、无负责人/期限/备注）。

| 字段 | 取值 |
|---|---|
| `severity` | `critical` / `high` / `medium` / `low` / `info` / `""`（未定级） |
| `disposition` | `open`（待处理，默认）/ `fixing`（修复中）/ `risk_accepted`（接受风险）/ `false_positive`（误报） |
| `owner` | 去首尾空白，≤100 字；`""` = 未指定 |
| `due_date` | `YYYY-MM-DD`（人工期限，覆盖 SLA）或 `""` |
| `note` | 去首尾空白，≤2000 字 |

`webapi_vuln_sla(id=1, critical_days, high_days, medium_days, low_days, updated_at)`：无行即默认 3 / 7 / 30 / 90 天；`info` 与未定级无期限。

## 派生（读时计算，不存）

- `owner_effective` = `owner`，为空时取条目所属资产的负责人（`webapi_assets.owner`）；无资产为空串。
- `first_confirmed_at`（内部，不输出）= 条目全部成员第一条“存在”事件（验证 `supported` / 复测 `reproducible`）的结果保存时间。
- `due_effective` = `due_date`（非空时）；否则 严重度有 SLA 且有 `first_confirmed_at` 时 = 该时间的 **Hub 本地日** + SLA 天数（`YYYY-MM-DD`）；否则 `null`。
- `overdue` = 派生状态 ∈ {new, present} 且 处置 ∈ {open, fixing} 且 今天（Hub 本地日）> `due_effective`。期限当天不算逾期；已修复/待验证/未确认、接受风险、误报都不算逾期。
- “待修复” = 派生状态 ∈ {new, present} 且 处置 ∈ {open, fixing}（计数用）。

Triage View：`{vuln_key, severity, disposition, owner, owner_effective, due_date, due_effective|null, overdue, note, updated_at|null}`（无记录时 `updated_at` 为 null）。

## API（原生鉴权内 `/api/webapi`；错误体 `{error}`；未知字段 400）

- `PUT /vulns/{key}/triage` `{severity?, disposition?, owner?, due_date?, note?}` → 200 Triage View。
  - 部分更新：省略的字段保持原值。取值非法 400；请求体必须是一个 JSON 对象（空体 400）；`key` 不符合路径 id 规则 400。
  - `key` 路径段支持 URL 编码（`finding%3A<id>` 与 `finding:<id>` 等价）。
  - 404：不存在的条目；已移空的条目；`finding:<id>` 的候选不存在或其任务已关联资产（此时它的键是资产条目 `vid`）。资产条目找不到时先按资产锁同步全部资产（含归档）的漏洞库再找一次，故未访问过漏洞库的条目也能设置。归档资产的条目可以设置（`/assets/{id}/vulns` 仍展示）。
  - 无变化（含无记录时只提交默认值）：不建记录、不改 `updated_at`、不登记审计。
- `GET /vuln-sla` → `{critical_days, high_days, medium_days, low_days, updated_at|null}`。
- `PUT /vuln-sla` `{critical_days?, high_days?, medium_days?, low_days?}`：省略的保持原值；每项为 1..365 的整数（小数、字符串、越界 400）；无变化不改 `updated_at`、不登记。保存即生效（期限读时计算）。

## 扩展的现有输出（只加字段）

- `GET /vulns`：每项加 `triage`（Triage View）。新参数 `severity=`（逗号分隔多值，`critical|high|medium|low|info|none`，`none` = 未定级）、`disposition=`（逗号分隔多值）、`overdue=1`（`0`/空 = 不过滤；其他值 400）；与 `status`、`q`、`asset_id` 取交集。`counts` 增 `overdue`（逾期条目数）与 `by_severity:{critical,high,medium,low,info,none}`（只计“待修复”条目）；与原 counts 一样受 `q`/`asset_id` 影响、不受 `status`/`severity`/`disposition`/`overdue` 影响。默认排序不变。
- `GET /assets/{id}/vulns`、`GET /assets/{id}/vulns/{vid}`，以及返回条目 View 的 `PUT …/vulns/{vid}`、`PUT …/members/{finding_id}`、`POST …/merge`：每个条目加 `triage`。资产漏洞列表的 `counts` 不变。
- `GET /overview`：`vulns` 增 `open_by_severity:{critical,high,medium,low,info,none}`（待修复条目）与 `overdue`（逾期条目数），范围同 `vulns.counts`（未归档资产 + 无资产候选）。

## 归并

- `merge`（from 并入 vid）：目标无处置记录则继承 from 的记录（改键，保留原 `updated_at`），否则保留目标的；from 的记录删除。与成员移动同一事务。
- 移动/拆分：拆分出的新人工条目无记录；人工条目被移空而删除时，其处置记录一并删除；auto 条目移空保留行与处置记录（之后同端点键新候选落回该条目时处置仍在，移空期间 PUT 为 404）。

## 联动

- 企业微信推送（`NOTIFY.md`）：登记时去重键（即本键）的处置为 `false_positive` → 不登记（new 与 regression 都不登记）；`risk_accepted` 照常登记、照常推 regression。markdown 标题行在类别后加等级：`**【新确认漏洞】【高危】<标题>**`（严重/高危/中危/低危/信息；未定级不加），取**发送时**的严重度。
- 计划后续复测（C2b，`ASSETS.md`）：规划时处置为 `false_positive` 的条目不复测；`risk_accepted`、`fixing` 照常复测。自动验证（auto_verify）不受处置影响。
- 审计（`AUDIT.md` 的表与 helper）：
  - `vuln.triage`（user）：target `vuln`/`<key>`，`asset_id` 为条目资产；detail `{vuln_key, title, changes:{字段:{from,to}}（severity/disposition/owner/due_date 中变化的）, note_changed}`——备注正文不入审计。summary 如“修改漏洞处置「订单越权」：严重度 未定级 → 高危；备注已修改”。
  - `vuln.sla`（user）：target `vuln_sla`/`default`；detail `{from, to}`（四个天数）；summary 如“修改修复期限 SLA：高危 7 → 5 天”。

## 旧库

`PrepareWebAPISchema` 已知阶段新增 B3（二十六表、二十九索引）一档；B3 及更早已知阶段单事务补两表（无新索引），不 DROP/ALTER。

## 未覆盖（如实说明）

- 前端未做；处置不进报告导出。
- 期限只按“第一条存在事件”起算一次：已修复后复现（regression）不会重置期限；严重度变更后期限按新 SLA 重新计算（读时）。
- 处置不随候选在条目间移动：单个候选移入其他条目时沿用目标条目的处置。
- 无逾期提醒推送；逾期只在列表/总览读时计算。
