# 漏洞卡（K1 后端）

漏洞的本体是**最小复现**：一句话影响 + 前置条件 + 1–3 个关键请求（平台原件，关键片段高亮）+ 正常对照 + 修复要点 + 复测方法；AI 长推理入附录。最小复现由独立验证工具 `webapi_verification_result` 在 supported 时产出（字段与校验见 `native/VERIFICATION.md`「最小复现与 AI 建议等级」）。本接口只读聚合，不新增表。实现 `native/webapi/vuln_card.go`。

## 键与编号

- 键同 TRIAGE：资产漏洞条目 `vid`，或无资产任务候选 `finding:<finding_id>`。归档资产的条目也能打开（不在全局排序中，nav 为空）。
- `code` = `V-` + 键去掉 `finding:`、`wf_` 前缀与非字母数字后的前 8 位大写（资产条目即 vid 前 8 位十六进制）；只由键决定、稳定。全为符号时退化为 sha256 前 8 位。`GET /vulns` 每项与资产漏洞列表每项同样带 `code`。
- `title_template` = `资产名 · METHOD 路径 · 标题`；路径取端点键（已归一 `{id}`）去 origin；无资产省资产段，端点键为 `finding:` 或空时省路径段。`title` 同列表（标题覆盖优先，否则最新成员修订标题）。

## `GET /api/webapi/vulns/{key}/card`

键非法 400，不存在 404，其余 500；错误体 `{error}`。只读，不登记审计。

| 段 | 内容 |
|---|---|
| `header` | `key, code, title_template, title, status, severity（人工，未定级 ""）, suggested_severity, severity_reason, disposition, due_effective, overdue, first_seen_at, asset{id,name,env,owner,origins}\|null, endpoint_key` |
| `triage` | 完整处置视图（同 `/vulns` 项的 `triage`） |
| `repro` | 成员中**最新 supported 且带 key_steps** 的验证：`{verification_id, task_id, finding_id, session_id, impact, preconditions, steps[], contrast\|null, remediation[], retest_method, saved_at}`；无则 null |
| `repro_missing_reason` | repro 为 null 时：`no_verification`（无任何判读）/ `no_supported`（有判读无 supported）/ `no_key_steps`（只有 K1 前的旧 supported）；有 repro 时 null |
| `retest` | 成员最新复测结论 `{retest_id, task_id, finding_id, session_id, conclusion, summary, at}` 或 null |
| `retest_request` | V3：该键最近一次成功的“申请复测” `{retest_id, session_id, task_id, finding_id, started_at, status}` 或 null；`started_at` 为审计 `vuln.retest_request` 时间；`status`：该复测无结论且会话正在运行（`SessionRunning`，无法核对按运行中）→ `running`，否则（已出结论、会话已结束或未注入会话服务）→ `done`。结论仍看 `retest` 段 |
| `lifecycle[]` | 升序 `{at, kind, title, detail, actor}`，见下 |
| `appendix` | `{verifications[], members[], evidence_count, evidence[], changes[]}`，见下 |
| `related[]` | 同资产其他未修复（status≠fixed）条目，按全局默认排序前 5：`{key, code, title, severity, suggested_severity, status}`；`suggested_severity` 与 `GET /vulns` 对应条目同源，无建议时为空串；无资产为空 |
| `nav` | `{prev_key, next_key}`：在 `GET /vulns` 默认排序（无筛选）中的前/后一条，首尾为 null |
| `notify` | K2：`{configured, require_confirm, items: NotificationView[]}`，该键最近 5 条推送（任何 kind/状态，新到旧）；见 `native/NOTIFY.md`「漏洞卡人工推送」 |
| `replays[]` | 该键最近 5 次已完成的一键重放（K2，见 `native/REPLAY.md`），新到旧：`{replay_id, step, task_id, evidence_id, at, status_code, verdict_hint}`；结果未知的尝试不列（只在审计与证据里）；无则 `[]` |

- `suggested_severity` / `severity_reason`：成员中最新一条带建议等级的 supported 验证；列表项 `suggested_severity` 同一规则。它只是 AI 建议，不写处置、不参与 SLA/逾期/计数。
- `steps[]`：`{title, evidence_id, method, url（去 userinfo）, status_code, request_marks_found[], response_marks_found[], mark_missing}`。marks 在该步证据原件中逐行查找（请求侧：请求行、原始头、正文；响应侧：状态行、原始头、正文；未遮盖；二进制正文不查），找到的按原顺序去重返回，任一找不到则 `mark_missing=true`。
- `contrast`：`{normal_evidence_id, abnormal_evidence_id, note, normal{evidence_id,method,url,status_code}, abnormal{…}}`。
- 前端取原件与截图沿用 `GET /tasks/{repro.task_id}/evidence/{evidence_id}`（及 `image.png?mark=`，把 `*_marks_found` 作为 mark 传入）。

### lifecycle

| kind | 来源 | actor |
|---|---|---|
| `candidate` | 每个成员候选创建（“AI 发现候选 WSC-…”，detail 为标题） | ai |
| `verification` | 每条已保存判读（结果保存时间；“独立验证：支持/反证/证据不足”，detail 为 summary） | ai |
| `triage` | 审计 `vuln.triage`（detail 为审计摘要） | 审计 actor（user） |
| `notify` | 该键的推送终态：sent 用 sent_at，failed 用 `notification.failed` 审计时间（无则登记时间）；人工推送（manual）每次投递终态各一条（“企业微信人工推送成功/失败”，取 `notification.sent/failed` 审计时间） | system |
| `replay` | 每次已完成的一键重放（“一键重放：仍可复现/有变化/无法判定”，detail 为 `METHOD /path → 状态码`） | user |
| `retest_request` | 每次成功的申请复测（审计 `vuln.retest_request`；“申请复测”，detail 为审计摘要“申请复测漏洞「标题」：候选 WSC-…”） | user |
| `retest` | 每条复测结论（“复测：仍可复现/已修复（范围内）/无法判定”） | ai |
| `fixed` | 条目派生状态为已修复时，`fixed_at` | system |

同一时刻按 candidate → verification → triage → notify → replay → retest_request → retest → fixed 排。detail ≤200 字。

### appendix

- `verifications[]`：成员全部验证（含未出结果），按结果时间（无结果按创建时间）倒序：`{verification_id, task_id, finding_id, finding_number, session_id, created_at, verdict|null, method, preconditions, summary, limitations, suggested_severity, severity_reason, has_repro, evidence_count, saved_at|null}`。
- `members[]`：同资产漏洞库成员视图。
- `evidence[]`：候选当前修订声称的证据（source=candidate）、验证结果证据、关键步骤与对照证据（relation=key_step/contrast）、复测证据（source=retest），按 task+evidence_id 去重，最多列 200 条；`evidence_count` 为去重总数。
- `changes[]`：该键全部 `vuln.*` 审计（rename/move/merge/triage/curl_unmasked/replay/notify/retest_request），新到旧，同审计接口项格式。

## `GET /api/webapi/vulns/{key}/card/curl?step=N&mask=0|1`

`text/plain; charset=utf-8`，`Cache-Control: no-store`。由 repro 第 N 步（1 起，默认 1）证据的 Flow 请求确定性生成：

```
# 已遮盖敏感头与测试账号 secret（******），重放前请替换为有效值     （有遮盖时）
curl -sS -X METHOD 'URL' \
  -H 'Name: value' \          （按原始头顺序；跳过 HTTP/2 伪头与 Content-Length）
  --data-binary 'body'        （有正文时；二进制正文改为首行注释说明，不内联）
```

- 单引号 shell 转义；方法大写，非常规字符时整体加引号。
- 默认遮盖：敏感头（Authorization、Proxy-Authorization、Cookie、Set-Cookie、X-API-Key、X-Auth-Token，同证据图片）值替换为 `******`；URL 去 userinfo；URL、头、正文中该任务资产测试账号 secret（≥4 字）替换为 `******`。
- `mask=0` 输出原值，并登记审计 `vuln.curl_unmasked`（user；target 为该键；detail `vuln_key, step, evidence_id, verification_id`，不含请求内容）。
- 错误：键非法、`step` 非 1..3 或超出步数、`mask` 非 0/1 → 400；条目不存在、无最小复现、证据不存在 → 404；原件无法解析 → 422。

## 申请复测（V3 后端）

研发说“已修复”后，人在漏洞卡点“申请复测”：后端直接在代表候选的原任务下新建复测并启动会话（无需打开原任务）；复测结论照现有规则（`webapi_retest_result`）回写派生状态（`fixed_in_scope` → 已修复，`reproducible` → 仍存在）。不新增表：申请记录即审计 `vuln.retest_request`。实现 `native/webapi/vuln_retest.go`。

`POST /api/webapi/vulns/{key}/card/retest`，body 为空或 `{}`（未知字段、非对象 400）→ 201 `{retest_id, session_id, task_id, finding_id, revision_id}`。键非法 400，条目不存在 404，其余 500（`{error}`）。

**选用候选**（须为条目成员）：
1. 卡片 `repro` 的来源候选（成员中最新 supported 且带 key_steps 的验证所属候选）；
2. 无 repro：成员中最近一次 supported 验证所属候选（同卡上人工推送的选用规则）；
3. 都没有：最近成员候选（候选创建时间最新）。

复测固定该候选的**当前修订**（不是验证当时的修订），`task_id` 为候选原任务，授权沿用原任务 origins。

**409** `{error, code}`，按以下顺序检查：

| code | 条件 |
|---|---|
| `archived` | 条目所属资产已归档 |
| `false_positive` | 处置为误报（接受风险、修复中照常可申请） |
| `no_member` | 无可用候选（无成员，或选中候选的任务/当前修订已不存在） |
| `in_flight` | 成员下已有**未出结论且会话正在运行**的复测（任何来源：申请复测、计划后续、手动 PUT 后前端 Run）；另带 `retest_id`、`session_id`。已保存但未启动（手动未 Run、发起失败）或会话已结束仍无结论的不算；会话状态无法核对按运行中计（同计划后续）；复测出结论后即解除 |
| `no_model` | `startSession` 的任何阶段性失败（无在线 webapi_http 节点、本机模型未配置、绑定节点离线、未注入会话服务、Open/Run 被拒或失败、首条消息组装失败等）；`error` = `阶段：原文`（同计划后续 `failed` 文案），另带 `stage`、`retest_id`、`session_id` |

**发起**：同键进程内串行（防连点）；`saveRetest` 以平台生成的随机 UUID 新建复测记录（登记 `retest.start`，actor user）→ `startPinnedSession`（与计划后续共用：首条消息 = `pinnedFirstMessage(retestMessageVariant, 原任务 origins, 当前修订)` + 测试账号占位符块，经 `startSession` 选节点 → Open → Run，不带持续探索设置）。请求处理不随页面断开取消。

**失败时的复测记录**：与计划后续一致——保留不删、不自动重试；响应 409 `no_model` 带其 `retest_id`。这条记录没有用户消息，不算 `in_flight`；再次申请会新建一条复测记录（新 UUID），不复用旧记录。只登记 `retest.start`，不登记 `vuln.retest_request`，卡片 `retest_request` 与 lifecycle 不出现。

**审计** `vuln.retest_request`（仅成功）：见 `native/AUDIT.md`。被拒（400/404/409）不登记。

## 推送联动（见 `native/NOTIFY.md`）

处置未定级时等级标签用 AI 建议（`【AI 建议·高危】`）；结果带最小复现时“影响”取 impact、“关键复现”取 step 标题，位置与图片取第 1 步证据并按其 marks 高亮。K2：标题行用 `header.title`，链接为 `<base_url>/?page=vulns&vuln=<键>`；卡上人工推送 `POST /vulns/{key}/card/notify` 与预览 `GET /vulns/{key}/card/notify/preview`。

## 漏洞报告导出（K2）

由漏洞卡确定性生成，不调用模型；只读、不登记审计；只导出遮盖版（不提供 `mask=0`）。实现 `native/webapi/vuln_export.go`。

- 单条 `GET /api/webapi/vulns/{key}/card/export?format=markdown|json`：键非法或 format 缺失/非法 400，条目不存在 404。
- 多条 `POST /api/webapi/vulns/export` `{keys:[…], format}`：1–50 个、不重复、每个符合路径 id 规则，未知字段 400；任一键不存在 404，`error` 为“漏洞条目不存在：<键>”；按请求顺序输出；归档资产条目可导出。
- 响应头：Markdown `text/markdown; charset=utf-8`，JSON `application/json`；`Cache-Control: no-store`；`Content-Disposition: attachment; filename="vuln-<code>.md|json"`（单条）/ `"vulns-report.md|json"`（多条）。不含生成时间等易变字段，同输入字节一致。
- 遮盖：敏感头（同 curl：Authorization、Proxy-Authorization、Cookie、Set-Cookie、X-API-Key、X-Auth-Token）值整体为 `******`（请求与响应都遮）；条目所属资产测试账号 secret（≥4 字）在所有文字字段、URL、原文、curl、高亮片段中替换为 `******`；高亮片段与敏感头值互相包含时整体为 `******`；URL 去 userinfo。curl 为 `card/curl` 默认遮盖版。

### Markdown

```
> 由 WScyber 依据漏洞卡生成；AI 判读与建议等级不等于人工确认；原文已遮盖敏感头与测试账号口令
## 汇总（N 条）                                   （仅多条）
| 编号 | 标题 | 资产 | 等级 | 状态 | 处置 | 期限 |
## V-XXXX <title_template>
- 资产 / 位置（端点键，无则“未记录”）/ 等级 / 状态 / 处置 / 负责人（owner_effective，无则“未指定”）/ 修复期限（due_effective，逾期加“（已逾期）”，无则“无”）/ 首次发现（Hub 本地时区 YYYY-MM-DD HH:MM）
### 影响 / ### 前置条件 / ### 复现步骤（#### 第 N 步：标题）/ ### 正常对照 / ### 修复建议 / ### 复测方法
### 最近复测结论 / ### 最近重放（≤5）/ ### 验证记录
```

- 等级：人工严重度中文名；未定级时“未定级（AI 建议：高危）”或“未定级”。状态：新发现/仍存在/待验证/未支持/已修复；处置：待处理/修复中/接受风险/误报。
- 每步：`` `METHOD URL` → 状态码（证据 id）``；关键片段列表（请求/响应各自 `*_marks_found`，`mark_missing` 时加“部分片段在原件中未找到”）；请求原文、响应原文各一个 `http` 代码块（首行、原始头、空行、正文），围栏为 max(3, 原文最长连续反引号 + 1) 个反引号；遮盖版 curl 一个 `bash` 代码块。如实标注：`capture_truncated`（“原件捕获已截断：响应正文只保存了前 N 字节”）、正文（遮盖后）超过 16 KiB 截到 16 KiB（UTF-8 边界）并写“已截断（原 N 字节）”、二进制正文不内联（“为二进制（N 字节），未内联”）、证据行不存在/原件无法解析（不出原文）。
- 正常对照：note，再 `正常/异常：`METHOD URL` → 状态码`；无对照写“未提供”。
- 无最小复现：不出影响至复测方法各段，写“最小复现：未提供（原因）”，原因：`no_verification` 还没有独立验证判读 / `no_supported` 独立验证未得出“支持”判读 / `no_key_steps` 该判读产生于最小复现功能之前；复测、重放、验证记录照常。
- 验证记录：附录中已出结果的判读（新到旧）：`候选编号 · 判读（时间）：summary`，下列方法、局限。

### JSON `wscyber.vuln_report.v1`

`{schema, items:[Item]}`，缩进 2 空格、不转义 HTML、末尾换行。

- Item：`key, code, title, title_template, asset{id,name,env,owner}|null, endpoint_key, location, severity, suggested_severity, severity_reason, status, disposition, owner（owner_effective）, due_effective|null, overdue, first_seen_at, repro|null, repro_missing_reason|null, retest|null, replays[], verifications[]`。
- repro：`verification_id, task_id, impact, preconditions, steps[], contrast|null, remediation[], retest_method, saved_at`。
- step：`index, title, evidence_id, evidence_status（ok|missing|unparsable）, method, url, status_code|null, request_marks_found[], response_marks_found[], mark_missing, request_raw, request_body_bytes, request_body_binary, request_body_truncated, response_raw, response_body_bytes, response_body_binary, response_body_truncated, capture_truncated, curl`（`*_raw` 为遮盖后原文，二进制时只到头部，`*_body_bytes` 为原件正文字节数；无响应时 `response_raw` 为空串；非 ok 时原文与 curl 为空串）。
- contrast：`note, normal{evidence_id,method,url,status_code}, abnormal{…}`；retest：`retest_id, conclusion, summary, at`；replays：`replay_id, step, evidence_id, at, status_code, verdict_hint`；verifications：`verification_id, finding_number, verdict, method, summary, limitations, suggested_severity, saved_at`。

## 未覆盖

- 总览 `recent_supported` 的 `suggested_severity` 未加（本片不改 `overview.go`，留给下一片）。
- 一键重放见 `native/REPLAY.md`（K2）；研发只读视图、工单区不在本片。
- curl 只从已保存原件生成，不校验目标可达或授权；平台内重放走 `POST /vulns/{key}/card/replay`（授权同 `webapi_http`）。
