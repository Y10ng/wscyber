# 审计记录（B3 后端）

谁在何时启动/批准/推送/改配置，可筛可查。本片只做后端：一张只追加的审计表、平台自有写操作生效后登记、只读查询接口；前端下一片做。仅临时库合成数据 Go 测试通过（`audit_test.go`），真实 Hub 数据未验。

代码：`native/webapi/audit.go`（表、登记 helper、查询）；各 handler/runtime 在业务操作成功后各调一行 helper。

## 表 `webapi_audit_events`

| 列 | 说明 |
|---|---|
| `id` TEXT PK | `au_<UTC 纳秒>_<随机>`；`schedule.run` 为确定性 `au_run_<UUIDv5(asset_id, scheduled_for)>` |
| `at` TEXT | 定宽 UTC 毫秒 `2006-01-02T15:04:05.000Z`（同 D2 `notifyTime`，字符串序即时间序） |
| `actor` | `user`（经原生鉴权的 HTTP 请求，单访问密钥，无用户名）/ `system`（调度器、计划后续、推送 goroutine、审批超时） |
| `action`、`target_kind`、`target_id` | 见下表 |
| `task_id`、`asset_id` | 关联任务/资产；无则空串（登记时按 `webapi_asset_tasks` 补资产） |
| `summary` | 一行中文，前端直接显示（≤300 字） |
| `detail_json` | JSON 对象，不含任何密文/口令/webhook/测试账号 secret |

索引：`(at, id)`、`(action)`、`(task_id)`、`(asset_id)`。

**只追加**：仅有 `INSERT ... ON CONFLICT (id) DO NOTHING`；没有更新/删除接口或代码路径。

登记规则：业务操作**实际生效后**登记；幂等重放、未变化的保存、失败（4xx/5xx）不登记；登记失败只 `log.Printf`，不影响业务响应（登记用脱离请求取消的 5s 上下文）。

## 登记点

| action | 触发（文件） | actor | detail |
|---|---|---|---|
| `task.create` | `PUT /tasks/{id}` 新建（`capability.go`）；从资产发起新建任务（`asset_launch.go`，HTTP 与调度共用） | user / 调度发起为 system | `origins`、`policy{scope,approval}`、`session_id`；从资产发起另带 `asset_id` |
| `task.policy` | `PUT /tasks/{id}/policy` 值有变化 | user | `from`/`to` `{scope, approval}`；summary 如“修改任务测试策略：仅授权 origins/仅高危 → 不限制/仅高危” |
| `verification.start` / `retest.start` | 新建验证/复测（HTTP PUT 201；计划后续新建为 system） | user / system | `finding_id`、`finding_number`、`session_id` |
| `report.start` | 新建报告（PUT 201） | user | `finding_numbers`、`session_id` |
| `approval.approve` / `approval.deny` | 审批经 Hub 控制通道批准/拒绝后，工具边界拿到终态（`approval.go admitTool`） | user | `tool`、`operation`、`session_id`；target_id = operation_id |
| `approval.expire` | 审批超时（视为拒绝） | system | 同上 |
| `asset.create` / `asset.update` / `asset.archive` / `asset.unarchive` | `PUT /assets/{id}` 有变化（归档位翻转优先记 archive/unarchive） | user | `changed`（字段名列表）、`values`（仅非敏感字段新值：name、owner、environment、origins、scope、approval、archived；不含已知信息/目标/禁止行为正文） |
| `credential.save` / `credential.delete` | 测试账号保存（有变化）/删除成功 | user | `label`、`username`、`changed`、`created`、`secret_replaced`（布尔，本次是否提交了新 secret） |
| `schedule.save` / `schedule.delete` | 计划保存（有变化）/删除成功 | user | `to`（及非首次的 `from`）`{cron, enabled, auto_verify, auto_retest}`、`created`；删除为删除前值 |
| `schedule.run` | 到点处理写入运行记录（`schedule.go` 经 `recordRun`）：launched / failed / skipped 均登记 | system | `scheduled_for`、`status`、`error`、`task_id`、`cron`、`enabled`（到期时读到的计划） |
| `vuln.rename` / `vuln.move` / `vuln.merge` | 漏洞库人工改动成功且有变化 | user | rename：`from_title`/`to_title`；move：`finding_id`、`finding_number`、`from_vuln_id`、`to_vuln_id`、`created`；merge：`from_vuln_id`、`from_title`、`into_vuln_id`、`into_title` |
| `vuln.triage` | `PUT /vulns/{key}/triage` 保存成功且有变化（`triage.go`） | user | `vuln_key`、`title`、`changes`（变化字段 severity/owner/due_date/disposition 的 `{from,to}`）、`note_changed`（不含备注正文）；target_kind = `vuln`，target_id = key，关联 asset_id |
| `vuln.sla` | `PUT /vuln-sla` 配置保存成功且有变化（`triage.go`） | user | `from`/`to` `{critical_days,high_days,medium_days,low_days}`；target_kind = `vuln_sla`，target_id = `default` |
| `vuln.curl_unmasked` | `GET /vulns/{key}/card/curl?mask=0` 成功生成未遮盖 curl（`vuln_card.go`） | user | `vuln_key`、`step`、`evidence_id`、`verification_id`（不含请求内容）；target_kind = `vuln`，target_id = key，关联验证 task_id 与条目 asset_id |
| `vuln.replay` | `POST /vulns/{key}/card/replay` 实际发送了一次请求（`replay.go`；收到响应或结果未知均登记；denied/review_required/归档/限频/进行中/同请求未决 unknown 未发送，不登记） | user | `vuln_key`、`step`、`evidence_id`（新证据）、`original_evidence_id`、`status_code`（结果未知为 null）、`verdict_hint`（结果未知为空串，另带 `outcome`: unknown/capture_failed/lost）；不含请求/响应内容；target_kind = `vuln`，target_id = key，关联证据任务与条目资产 |
| `notify.settings` | 企业微信配置保存且有变化 | user | `from`/`to` `{enabled, require_confirm, base_url}`、`webhook_changed`（提交了与原值不同的 webhook）、`webhook_cleared`（`"-"` 清除了已有 webhook） |
| `notify.test` | 测试推送实际发出（未配置 409、限频 429 不登记） | user | `ok`、`errcode`（网络错误为 -1） |
| `notification.confirm` / `.dismiss` / `.retry` | 人工处理成功（409/404 不登记） | user | `notification_id`、`title` |
| `notification.sent` / `.failed` | 投递终态：两段都成功 → sent；第 3 次失败 → failed（中间重试、45009 推迟不登记） | system | `notification_id`、`title`、`attempts`、`last_error`（D2 已去除 webhook 地址） |

审批操作摘要 `operation`：`webapi_http` 为 `方法 + URL`（去 userinfo，经上游 `guardrail.RedactText` 脱敏 `token=` 等赋值）；`bash` 为命令脱敏后前 200 字；`write` 为路径。请求头/请求体不入审计。

## 查询 `GET /api/webapi/audit?action=&actor=&task_id=&asset_id=&q=&from=&to=&limit=&cursor=`

- 按 `at` 倒序，同时间按 `id` 倒序；键游标 = base64url(`at`\n`id`)（同 OVERVIEW `task-list`）。
- `action`：精确名，或以 `.` 结尾表示前缀（如 `asset.`）；须为小写点分名、≤64 字，否则 400。
- `actor`：`user|system`，其他值 400。`task_id`、`asset_id`：路径 id 规则，非法 400、不存在为空结果。
- `from`/`to`：本地日期 `YYYY-MM-DD`，含两端，Hub 本地时区（同 USAGE `parseUsageDay`）；`from > to` 400。
- `q`：去首尾空白、≤200 字、不区分大小写子串，匹配 `summary` / `target_id`。
- `limit` 1..200（默认 50），`cursor` 非法 400；错误体 `{error}`（同 OVERVIEW 通用约定）。
- 响应 `{items:[{id,at,actor,action,target_kind,target_id,task_id,asset_id,summary,detail}], has_more, next_cursor}`；`detail` 为对象，末页 `next_cursor` 为 null。

## 旧库

持续探索阶段（三十七表）旧库补 K2 的 `webapi_replays` 表与索引（见 `native/REPLAY.md`）。

`PrepareWebAPISchema` 已知阶段新增 D2（二十五表、二十五索引）一档；B2/U2a/D1（二十三表）及更早阶段照旧单事务补到完整结构（含本表与 4 个索引），不 DROP/ALTER。

## 未覆盖（如实说明）

- 原生操作不经本能力、无法拦截，本片不登记：会话归档/取消归档/重命名/删除、原生 LLM/模型配置保存、原生 GuardrailToggle 等原生页面操作。
- 研究会话首条消息由前端经原生 Open/Run 发起（非资产任务），本片只登记 `task.create`，不登记原生 Run。
- 审批取消（会话/轮次被取消）不是人工决定，不登记；审批决定的登记点在工具边界拿到终态之后，若 Hub 在批准后、工具边界返回前退出，该次决定不登记。
- `actor` 只能区分 user/system：单访问密钥，无用户名与来源 IP。
- 无保留期/清理；数据量增大后再考虑归档策略（仍保持不改写已有行）。
