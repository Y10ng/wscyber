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
| `lifecycle[]` | 升序 `{at, kind, title, detail, actor}`，见下 |
| `appendix` | `{verifications[], members[], evidence_count, evidence[], changes[]}`，见下 |
| `related[]` | 同资产其他未修复（status≠fixed）条目，按全局默认排序前 5：`{key, code, title, severity, suggested_severity, status}`；`suggested_severity` 与 `GET /vulns` 对应条目同源，无建议时为空串；无资产为空 |
| `nav` | `{prev_key, next_key}`：在 `GET /vulns` 默认排序（无筛选）中的前/后一条，首尾为 null |
| `replays[]` | 该键最近 5 次已完成的一键重放（K2，见 `native/REPLAY.md`），新到旧：`{replay_id, step, evidence_id, at, status_code, verdict_hint}`；结果未知的尝试不列（只在审计与证据里）；无则 `[]` |

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
| `notify` | 该键的推送终态：sent 用 sent_at，failed 用 `notification.failed` 审计时间（无则登记时间） | system |
| `replay` | 每次已完成的一键重放（“一键重放：仍可复现/有变化/无法判定”，detail 为 `METHOD /path → 状态码`） | user |
| `retest` | 每条复测结论（“复测：仍可复现/已修复（范围内）/无法判定”） | ai |
| `fixed` | 条目派生状态为已修复时，`fixed_at` | system |

同一时刻按 candidate → verification → triage → notify → replay → retest → fixed 排。detail ≤200 字。

### appendix

- `verifications[]`：成员全部验证（含未出结果），按结果时间（无结果按创建时间）倒序：`{verification_id, task_id, finding_id, finding_number, session_id, created_at, verdict|null, method, preconditions, summary, limitations, suggested_severity, severity_reason, has_repro, evidence_count, saved_at|null}`。
- `members[]`：同资产漏洞库成员视图。
- `evidence[]`：候选当前修订声称的证据（source=candidate）、验证结果证据、关键步骤与对照证据（relation=key_step/contrast）、复测证据（source=retest），按 task+evidence_id 去重，最多列 200 条；`evidence_count` 为去重总数。
- `changes[]`：该键全部 `vuln.*` 审计（rename/move/merge/triage/curl_unmasked/replay），新到旧，同审计接口项格式。

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

## 推送联动（见 `native/NOTIFY.md`）

处置未定级时等级标签用 AI 建议（`【AI 建议·高危】`）；结果带最小复现时“影响”取 impact、“关键复现”取 step 标题，位置与图片取第 1 步证据并按其 marks 高亮。

## 未覆盖

- 总览 `recent_supported` 的 `suggested_severity` 未加（本片不改 `overview.go`，留给下一片）。
- 一键重放见 `native/REPLAY.md`（K2）；研发只读视图、工单区不在本片。
- curl 只从已保存原件生成，不校验目标可达或授权；平台内重放走 `POST /vulns/{key}/card/replay`（授权同 `webapi_http`）。
