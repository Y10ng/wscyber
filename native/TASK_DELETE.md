# 任务删除契约

任务删除是**不可恢复**的平台操作（11-23 用户定语义：**删任务本体，留审计与漏洞**）。归档仍然保留，两者并存：归档可找回，删除不可。

## 1. 接口

`DELETE /api/webapi/tasks/{id}`（鉴权内路由，实现 `webapi/task_delete.go`；空 body，或等价的空白/`{}`——同平台 `decodeJSONBody` allowEmpty 规则；带字段、非对象或多个 JSON 值 400）

- 200：`{"task_id": "...", "deleted": {...计数...}, "orphan_vulns_removed": N}`
- 404：任务不存在（已删除或从未存在）
- 409：任务仍在运行，`{"error": "...", "code": "in_flight"}`；不删任何数据
- id 非法：400（沿用 `taskIDFromPath` 规则）
- 500（均不登记审计，任务业务数据均未删除，可直接重试）：
  - 运行状态核对出错：`{error}`，未删任何数据；
  - 会话删除中途失败：`{"error": "...已删除 N 个会话...", "code": "session_delete_failed", "sessions_deleted": N}`；
  - 写事务复核发现删会话期间任务出现了新会话（并发发起）：`code: "sessions_changed"`，事务回滚；
  - 写事务失败：`code: "delete_failed"`，事务回滚。
  后三种情况下前 N 个原生会话已删除（见 §2「会话」的已知残留）。

计数形如（`deleted` 恒含下列 12 个键，无行为 0；`sessions` 为本次实际删除的原生会话数，已不存在的会话不计）：

```json
{"requests": 23, "findings": 4, "verifications": 4, "reports": 1, "retests": 2,
 "egress_flows": 12, "board_endpoints": 6, "board_facts": 5, "board_oracles": 3,
 "authz_compares": 0, "subsessions": 0, "sessions": 7}
```

### 运行中判定

任一任务会话仍在运行即拒绝：初始会话、验证会话、报告会话、复测会话、子 Agent 会话；判定复用现有 `taskRunStatus` 口径（`launcher.SessionRunning` + 运行中子会话），对任务会话集合逐个核对。launcher 为 nil（未被原生 API 注入）时只看子会话记录、按“无运行中”处理并记录日志，此时也无法删除原生会话（`sessions` 为 0，只删业务数据）。

## 2. 删除范围（单写事务、持有全局写锁）

**删除**：`webapi_tasks`、`webapi_requests`、`webapi_findings`、`webapi_finding_revisions`、`webapi_verifications` + `webapi_verification_results`、`webapi_reports` + `webapi_report_drafts`、`webapi_retests` + `webapi_retest_results`、`webapi_egress_flows`、`webapi_board_endpoints` / `_facts` / `_oracles` / `_oracle_events` / `_ingest`、`webapi_authz_compares`、`webapi_replays`、`webapi_task_policies`、`webapi_task_policy_events`、`webapi_task_scenarios`、`webapi_task_explore`、`webapi_task_subsessions`、`webapi_subagent_reservations`、`webapi_subsession_usage`、`webapi_asset_tasks`、`webapi_schedule_followups`、`webapi_schedule_run_followups`。

**保留但解除关联**（把 `task_id` 置空字符串，行本身保留）：

- `webapi_audit_events`：审计链保留，“谁删了什么”可追查；删除动作本身另登记一条 `task.delete`。
- `webapi_notifications`：推送记录保留（含已发送内容与状态）；`task_id`、`finding_id` 置空。
- `webapi_schedule_runs`：计划运行历史保留（属资产）；`task_id` 置空。
- `webapi_approvals`：审批记录（迁移 0002）保留（状态、决定人、摘要不变，审批决定另有 `approval.*` 审计）；`task_id` 置空。按 `task_id` 过滤的审批查询不再返回这些行，同 id 重建的新任务也不会串到旧审批。

**漏洞**：只删该任务在 `webapi_asset_vuln_members` 的成员行；成员被移空的人工条目（`endpoint_key = ''`）按现有 `deleteEmptyManualVuln` 规则删除（连同其处置），`auto` 条目保留（连同处置）。资产与漏洞本身不删。`orphan_vulns_removed` 只计被删除的资产漏洞条目行（`webapi_asset_vulns`）。

**无资产候选的处置**：无资产任务的候选在全局漏洞列表里以 `finding:<finding_id>` 自成一条（派生条目，无 `webapi_asset_vulns` 行），其处置记录在 `webapi_vuln_triage`（`vuln_key = 'finding:' || id`）。同一写事务内删除**本任务候选**对应的这些处置行（只此前缀、只本任务候选；资产条目处置仍按上条规则）。这些派生条目随候选一起消失（已体现在 `findings` 计数里），处置行删除**不单独计数、不计入** `orphan_vulns_removed`（派生条目没有条目行可删；也避免同一计数混入“有处置才算”的不一致口径）。

**会话**：任务会话集合与用量统计同一枚举口径（`usageSessionsTx`：`webapi_tasks.initial_session_id` + 验证/报告/复测/子 Agent 表的 `session_id`，去重去空，按字典序），通过原生 `Sessions.DeleteSession`（`sessionLauncher.DeleteSession`）逐个删除，连带 `chat_aop_events`（既有 `ON DELETE CASCADE`）；会话已不存在（上游 `NOT_FOUND`）视为已删、不计数。删会话在写事务**之前**按序执行：

- 中途失败立即返回 500 并写明已删数量，写事务不开始（不静默成功）。
- 写事务内复核会话集合，出现事务前未处理的新会话（删除期间有并发发起）即整体回滚，500 `sessions_changed`。
- **已知残留**：上述两种 500 之后，已删会话对应的业务数据（任务、验证记录等）仍在，任务在列表中仍可见但其已删会话不可打开。不做补偿（无法恢复已删会话，也不应在会话删除失败时删业务数据而留下仍存在的会话）；重试是幂等的——已删会话按“已不存在”跳过，其余继续删除并完成事务。
- 删除全程持有该任务的发起锁（与从资产/计划发起同一任务串行），写事务期间持有任务关联资产的漏洞库锁（避免并发同步把已删候选补回成员）。验证/报告/复测记录先落库、再由前端或计划后续 Open/Run，这些路径不经上述锁：删会话期间新落库的记录会被事务内复核发现并回滚；记录在枚举会话之前已存在、但其会话恰在删除之后才 Open 的，该原生会话会残留为无任务归属的孤儿会话（窗口很小；运行中判定已排除“正在运行”的情况）。写事务提交之后再落库的记录会因任务缺失被拒。

**不删**：`raw_artifacts`（全局事件归档，上游语义同样不随会话删除）。

**未发送的推送**：保留行只解除关联；排队中/待确认的通知之后生成消息会因验证已删而失败（按现有失败重试规则最终记 `failed`），不会对外发送。

## 3. 审计

成功删除登记 `task.delete`（actor user，target task/{id}，`task_id` 与 asset 为空——与被解除关联的旧审计一致，避免同 id 重建的新任务按 `task_id` 过滤时串到旧任务；按 `target_id` 可查），detail：`task_id`、`title`（objective 摘要）、各计数（含 `sessions`，与响应 `deleted` 同键）、`orphan_vulns_removed`。与业务数据删除**同一写事务**登记：事务提交即有审计，任何 500 均不登记；被拒不登记（400/404/409）。

## 4. 前端

任务列表行“更多”菜单在归档下方增加“删除任务”（destructive 样式，`Trash2`）：

- 二次确认对话框，标题“删除任务”，正文写明不可恢复，并列出将删除的内容（HTTP 记录、候选、验证、报告、复测、子 Agent）；需输入任务标题摘要或勾选确认（选最简单：勾选“我了解此操作不可恢复”）。
- 成功后静默重取第一页与计数；失败显示接口错误原文（409 时提示“任务仍在运行，请先停止”）。
- 归档筛选页同样可删。
- i18n 中英成对；390 宽不溢出；深色正常。

## 5. 不做

- 不做回收站/软删除（用户已定硬删除）。
- 不做批量删除。
- 不删审计、推送记录、资产漏洞。
- 不清理 `raw_artifacts`。
