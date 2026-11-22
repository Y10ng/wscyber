# 企业微信推送（D2 后端）

经独立验证判读为 `supported` 的漏洞，精简推送到企业微信群机器人（markdown 文字 + 证据图片 + 平台链接）。未配置 webhook 时不推送、不登记。仅临时库合成数据 Go 测试（httptest 模拟企业微信接口）通过；前端、真实企业微信、真实节点端到端未验。

## 配置

- 表 `webapi_notify_settings`（单行，`id='wecom'`）：`webhook_cipher`、`enabled`、`require_confirm`、`base_url`、`updated_at`。webhook 用资产测试账号同一密钥文件（`<state-dir>/webapi-secrets.key`，见 `ASSETS.md`）AES-256-GCM 加密，AAD 固定 `notify:wecom`，列存 `v1:base64(nonce‖密文)`；库中已有任何密文（测试账号或 webhook）而密钥缺失时报错，不生成新密钥覆盖。
- `GET /api/webapi/notify/wecom` → `{enabled, has_webhook, webhook_hint, require_confirm, base_url, updated_at|null}`。`webhook_hint` = host + ` …` + `key` 参数末 4 位（无 key 参数时取路径末 4 位），如 `qyapi.weixin.qq.com …abcd`；**任何响应都不含完整 webhook**；密文无法解密时 hint 为“（无法解密，请重新填写）”。未保存过时全为默认值。
- `PUT` 同路径 `{webhook?, enabled, require_confirm, base_url}`（未知字段 400；整份覆盖，保存即生效）→ 同 GET 形状：
  - `webhook` 省略或 `""` 保留原值；`"-"` 清除；其他值须为 `https://` 完整地址（有 host，无 userinfo/#片段，≤2048 字节），否则 400。`Capability.notify.allowHTTP` 是仅测试设置的开关，允许 httptest 的 `http://` 地址。
  - `base_url`：空或 `http(s)://` 地址（无 query/#片段/空白/括号，≤512 字节），去尾 `/`；用于消息里的平台链接。
- `POST /api/webapi/notify/wecom/test` → 发一条 markdown `wscyber 推送测试`，`200 {ok, errcode, errmsg}`（网络/HTTP 错误为 `errcode:-1`，errmsg 不含 webhook 地址）；未配置 webhook 409；全局每分钟名额用尽 429。不要求 `enabled`。

## 触发与去重

- 触发：验证结果工具 `webapi_verification_result` 保存（含同 Invocation 回放）`verdict=supported` 成功后，同一流程里登记；不改判读保存语义，登记失败只记 Hub 日志。未配置 webhook 或 `enabled=false` → 不登记（之后再启用也不补登记以前的判读）。
- 去重键：候选所属任务关联资产时 = C3 漏洞条目 id（登记前先按资产锁同步漏洞库）；否则 = `finding:<finding_id>`。
- 同一去重键已有任何通知（任何状态，含 dismissed/failed）→ 不再登记 `new`。例外：该键全部成员的决定性事件（验证 supported、复测 reproducible = 存在；复测 fixed_in_scope = 已修复；按结果保存时间）中，本次结果之前最新的一条为“已修复” → 登记 `kind=regression`。
- 处置（V1，`TRIAGE.md`）：去重键对应条目处置为误报时不登记；接受风险照常登记（含 regression）；有严重度时标题行在类别后加“【高危】”等等级。
- 表 `webapi_notifications(id, dedupe_key, kind new|regression|manual, task_id, finding_id, verification_id, vuln_id, status, attempts, next_attempt_at, last_error, created_at, sent_at)`，`(dedupe_key, kind, verification_id)` 唯一；时间为定宽 UTC `2006-01-02T15:04:05.000Z`。`require_confirm=true` 时初始 `pending_confirm`（`next_attempt_at` 空），否则 `queued`（立即到期）。

## 投递

- Hub 内单 goroutine：`Routes` 拿到原生管理 API 时启动（先把上次中断在 `sending` 的通知改回 `queued`），`Capability.Close` 停止；每 10s 取到期 `queued`（`next_attempt_at ≤ now`，最早在前，每轮最多 50 条）。未配置 webhook 或未启用时不发送、保持 queued。
- 每条：认领 `queued → sending`，先发 markdown，再发图片；都成功 → `sent`（写 `sent_at`，清 `last_error`）。
- 图片：D1 `renderEvidenceImage`，证据取验证结果第一条 `supporting`，没有则取验证固定的候选修订第一条 `supporting`；`mask` 开，另把该资产测试账号的 secret 明文（≥4 字）在 Flow 中替换为 `******`。PNG >2MB 时 `max_lines=30` 重渲，仍超则不发图、markdown 末行“证据图片过大未发送”；证据行读不到/渲染失败 → 末行“证据图片生成失败未发送”。无 supporting 证据时只发 markdown。图片消息 `{"msgtype":"image","image":{"base64","md5"}}`。
- 失败（网络、HTTP 非 2xx、响应非 JSON、`errcode≠0`）：`attempts+1`，第 1、2 次失败后分别 1 分钟、5 分钟后重试，第 3 次 → `failed`。`last_error` 形如 `markdown：errcode 93000 …` / `图片：网络错误：…`，不含 webhook 地址。
- `errcode=45009`（频率超限）：不计 attempts，`next_attempt_at` 推迟 60s，本轮停止。
- 全局每分钟最多发送 18 条消息（markdown、图片各算一条，测试推送也计入）：名额不足时本轮停止，剩余下轮再发。
- Hub 关闭打断发送：回到 `queued`、不计次。markdown 已成功而图片失败/超限/中断时，重试会重发 markdown（可能重复一条文字）。

## markdown 内容

只用候选（验证固定的修订）与验证中已保存的文字，不经模型：

```
**【新确认漏洞】<标题>**            （regression 时为【漏洞复现】，manual 时为【漏洞通报】）
> 资产：<资产名 或 “未关联资产”>
> 位置：<METHOD URL（上述第一条 supporting 证据；无则“未记录”；去 userinfo）>
> 判读：独立验证支持（<结果保存时间，Hub 本地时区 YYYY-MM-DD HH:MM>）
危害：<description 首句（到 。！？!? 或“. ”或换行），≤120 字>
复现要点：
1. <steps[i].text，空白合并为单行，≤80 字>  （最多 3 条）
[在平台查看](<base_url>/?page=vulns&vuln=<url.QueryEscape(去重键)>)   （漏洞卡；base_url 空时省略）
<证据图片说明（如有）>
```

- 标题（K2）：漏洞卡 `header.title`（人工改名优先，否则最新成员修订标题），对所有 kind（含自动推送）生效；按去重键取不到漏洞卡条目（如已并入其他条目）时退回验证固定的修订标题。
- 链接（K2）：漏洞卡 URL，键即去重键（`finding:<id>` 的冒号转义为 `%3A`）；前端支持该 URL 直接打开漏洞卡。
- 超过字数上限的字段截断并以 `…` 结尾（计入上限）。description 为空则省略“危害”行，无步骤省略“复现要点”。
- 总长 >4096 字节时，对标题与链接以外的字段（资产、位置、危害、各步骤）统一取最大可行字数上限截断，标题、链接、说明行保留。
- 测试账号 secret 明文出现在任一字段中时替换为 `******`；用户名不替换。Cookie/Authorization 只出现在图片里且被遮盖。

## 漏洞卡人工推送（K2）

- `POST /api/webapi/vulns/{key}/card/notify`（空 body 或 `{}`；未知字段 400）→ 201 View。
  - 选用验证：与卡片 `repro` 同一条（成员中最新 supported 且带 key_steps）；无则成员最新 supported 验证。
  - 登记 `kind=manual`：直接 `queued`、立即到期（人点按钮即确认，不受 `require_confirm` 影响），由投递 goroutine 按上文规则发送（重试、45009、频率窗口、图片均相同）；标题前缀【漏洞通报】。
  - 可重复推送：该键所有通知都已到终态（sent/failed/dismissed）即可再推。同一 `(去重键, manual, 验证)` 只有一行：再推时**复用该行**重新入队（`status=queued`、`attempts=0`、`last_error=""`、`sent_at=null`、`created_at`/`next_attempt_at` 为本次时间）；选用验证变化时插入新行。未改表；`(dedupe_key, kind, verification_id)` 唯一语义不变，manual 行不影响 new 的“同键已有任何通知不再登记”（new 只在同键无任何通知时登记，manual 行算“已有通知”）与 regression 判定。
  - 409 `{error, code}`：`not_configured`（无 webhook 或未启用）、`false_positive`（处置为误报）、`no_supported`（成员无 supported 验证）、`in_flight`（该键有 pending_confirm/queued/sending 的任一通知，任何 kind；另带 `notification_id` 为其中最新一条）。检查顺序同上。键非法 400，条目不存在 404。
  - 审计 `vuln.notify`（见 `AUDIT.md`）；拒绝不登记。
- `GET /api/webapi/vulns/{key}/card/notify/preview` → `{verification_id, markdown, image: {task_id, evidence_id, marks[]} | null, image_note}`：用与投递同一构建函数（`buildNotifyMessage`，按当前 `base_url`）生成人工推送将发送的逐字相同 markdown；`image` 为将渲染的证据与高亮（前端用 `GET /tasks/{task_id}/evidence/{evidence_id}/image.png?mark=…` 显示；不发图时为 null），`image_note` 为附在 markdown 末行的说明（“证据图片过大未发送”等，无则空串）。只读、不登记审计；未配置 webhook 也可预览。错误：`no_supported` 409，键非法 400，不存在 404。
- 卡片 `GET /vulns/{key}/card` 的 `notify` 段：`{configured（有 webhook 且已启用）, require_confirm, items: View[]}`，该键最近 5 条通知（任何 kind/状态，`created_at` 新到旧）。前端用现有 confirm/dismiss/retry 接口操作。
- 生命周期 `notify`：manual 行按每次投递终态审计（`notification.sent`/`notification.failed`）各出一条，标题“企业微信人工推送成功/失败”；new/regression 规则不变。

## 管理 API

- `GET /api/webapi/notifications?status=&limit=1..200(默认50)&cursor=` → `{items:[View], has_more, next_cursor}`，按 `created_at` 倒序（偏移游标，同全局列表）；status 非法、limit 越界、cursor 非法 400。
- View = `{id, kind（new/regression/manual）, status, dedupe_key, task_id, finding_id, finding_number, verification_id, verification_session_id, vuln_id|null, title（验证固定修订标题）, asset:{id,name}|null, attempts, last_error, next_attempt_at|null, created_at, sent_at|null}`。
- `POST /api/webapi/notifications/{id}/confirm`：pending_confirm → queued（立即到期）；`…/dismiss`：pending_confirm|failed → dismissed；`…/retry`：failed → queued，attempts 清零、立即到期。成功 200 返回 View；不存在 404；状态不符 409。

## 运营通知（V4 后端）

无人值守时让人及时知道“有危险操作在等审批 / 计划运行失败 / 本周情况”。与漏洞通知共用 `webapi_notifications` 表与上文投递（队列、重试 1/5 分钟、3 次 failed、45009 推迟、每分钟 18 条窗口、未配置/未启用不发送保持 queued）。只发 markdown，不发图片。仅临时库 + httptest 模拟企业微信的 Go 测试通过；前端（集成页开关与记录展示）下一片做，未向真实企业微信发送。

### 设置

- `GET/PUT /api/webapi/notify/wecom` 视图增加三个布尔 `notify_approval`、`notify_schedule_failed`、`notify_weekly_digest`（默认 false）。
- PUT 请求体同名三项为**可选**：**省略（或 null）＝保留原值**（无原值为 false）；其余字段仍整份覆盖、未知字段 400。选“省略即保留”而非“省略按 false”：不认识这三项的旧客户端整份保存时不会把已开的运营通知悄悄关掉。要关闭须显式传 `false`。
- 三类都要求 `enabled=true` 且已配置 webhook 才登记（之后再启用不补登记以前的事件；每周摘要见下）。
- **`require_confirm` 不作用于运营通知**（时效性）：一律登记为 `queued`、立即到期。
- 存储：开关在新表 `webapi_notify_ops_settings`（单行 `id='wecom'`：三开关、`weekly_digest_since`、`updated_at`），与 `webapi_notify_settings` 同事务写；`weekly_digest_since` = 每周摘要开关最近一次由关变开的时间（关闭时清空），其前的周一 09:00 不补发。
- 审计沿用 `notify.settings`（摘要如“待审批通知停用 → 启用”，detail `from`/`to` 带三项）。

### 三类触发与去重

| kind | 触发 | 去重键 |
|---|---|---|
| `approval_pending` | 审批挂起时（`approval.go` review 登记挂起后、发 PENDING 事件前同步登记；每个 operation 一条） | `approval:<operation_id>` |
| `schedule_failed` | 计划运行记为 `failed` 时（`recordRun`：发起失败 `阶段：文案`、cron 非法、next_run_at 损坏，全部来源） | `schedule_run:<run_id>`（`(asset, scheduled_for)` 冲突未插入时取已有那条的 id） |
| `weekly_digest` | 推送 goroutine 每轮（10s）检查：Hub 本地时区最近一次“周一 09:00”尚未登记即登记；停机错过多周只补最近一次 | `digest:<ISO 年>-W<周>`，如 `digest:2026-W48` |

- 同一去重键已有任何通知（任何状态）即不再登记。审批/计划失败先进入进程内待登记队列，不等待数据库；推送 goroutine 登记失败（包括 SQLITE_BUSY）会记日志并保留，单次尝试设置 10s 超时、失败至少间隔 10s，直至成功或当前开关关闭；周报同用此队列，每轮只检查最近一期。审批已决/过期/取消则放弃补登记并记日志，取得写锁后也会再核对；撤销未发出通知失败同样入队重试。队列不持久化，进程退出会记录剩余数，重启不恢复待登记项（周报仍按最近一期规则补发），已落库通知继续沿用 D2 投递重试。
- 审批已决（批准/拒绝/超时/取消）后，该键仍在 `pending_confirm/queued` 的通知改为 `dismissed`（不审计）；投递前若发现挂起已不在本进程（如 Hub 重启后）同样改 `dismissed` 不发。已发出的保持 `sent`。
- 表结构：`webapi_notifications` 必填列不改，运营通知 `finding_id`/`verification_id`/`vuln_id` 为空串（唯一约束 `(dedupe_key, kind, verification_id)` 不受影响），`task_id` 为审批所属任务 / 失败运行已建的任务（无则空）/ 摘要为空。登记时冻结的内容存新表 `webapi_notification_ops(notification_id pk, title, asset_id, session_id, payload_json)`；markdown 在投递时按当时 `base_url` 生成。

### markdown 版式

```
**【待审批】<任务标题（objective 首个非空行，≤120 字）>**
> 工具：webapi_http | bash | write
> 请求：<METHOD URL>            （bash 为“> 命令：<命令首 120 字>”，write 为“> 文件：<路径>”）
> 原因：<审批原因原文>
> 剩余时间：10 分钟内未处理将自动拒绝（<截止时间，Hub 本地 YYYY-MM-DD HH:MM> 截止）
[去平台审批](<base_url>/sessions/<研究会话 id>)
```

- 遮盖：请求摘要/原因/标题先经该任务资产的 credential resolver 回替（口令明文 → `{{cred:<id>.secret}}`），再经 guardrail `RedactText`（Bearer/Basic、`password=`/`token=`/`cookie:` 等赋值、URL userinfo、`-u user:pass`）；URL 另去 userinfo。读不到资产密钥时不登记（不冒险外发明文）。用户名不替换。
- 会话链接：审批来自子 Agent（`oracle_explorer`）时指向其父研究会话；验证/复测会话指向该会话本身。View `session_id` 即链接所用会话。

```
**【计划运行失败】<资产名>**
> 计划：<cron>（Hub 本地时区）
> 应运行时间：<scheduled_for，Hub 本地 YYYY-MM-DD HH:MM>
> 失败阶段：<LaunchAssetTask 阶段，如 select_node / validate；cron 非法与 next_run_at 损坏为“调度检查”>
> 原因：<运行记录 error 原文（资产测试账号口令替换为 ******）>
[在平台查看](<base_url>/?page=assets)
```

- 前端目前没有资产深链参数，链接用 `?page=assets`。

```
**【每周摘要】2026-W48**
> 统计区间：2026-11-17 ~ 2026-11-23（Hub 本地日期；逾期/待定级为当前值）
> 合计：新增 N · 已修复 N · 当前逾期 N · 待定级 N
- <资产名>：新增 … · 已修复 … · 当前逾期 … · 待定级 …     （有非 0 数字的未归档资产，按名称；末行“未关联资产”= 独立任务候选）
其余 N 个资产均为 0
[在平台查看](<base_url>/?page=overview)
```

- 口径（复用总览函数，不新写口径）：区间 = 登记时点（周一 09:00）的总览 `days=7` 区间（该本地日往前 7 个本地日，含当日）；新增/已修复 = 区间内 `vulns.confirmed_by_day` 的 `new`/`fixed` 之和（同一函数 `vulnPeriodMarks`）；当前逾期 = `vulns.overdue`、待定级 = `todo.unrated`（同一函数 `vulnTodo`，当前值）。全局合计 = 各行之和 = 同一时刻总览 `days=7` 的对应数字。数据范围同全局漏洞列表（不含归档资产）。无任何资产时仍发合计行与“暂无资产”。
- 4096 字节：待审批/计划失败对标题与链接以外的字段统一取最大可行字数上限截断（同漏洞通知规则，`…` 结尾）；每周摘要从末尾起省略资产行并加“另有 N 个资产有变化未列出”。无 `base_url` 省略链接行。

### 记录（`GET /notifications` View）

- `kind` 取值增加 `approval_pending`/`schedule_failed`/`weekly_digest`；View 新增 `session_id`（仅 `approval_pending` 有值，其余 `""`）。

| 字段 | approval_pending | schedule_failed | weekly_digest |
|---|---|---|---|
| `title` | 任务标题 | 资产名 | `每周摘要 <年-W周>` |
| `asset` | 任务关联资产或 null | 该资产（已删除为 null） | null |
| `task_id` | 所属任务 | 已建任务或 `""` | `""` |
| `session_id` | 链接会话 | `""` | `""` |
| `finding_id`/`finding_number`/`verification_id`/`verification_session_id` | `""` | `""` | `""` |
| `vuln_id` | null | null | null |

- confirm/dismiss/retry 同样可用：运营通知不会处于 `pending_confirm`，confirm 恒为 409（状态不符）；failed 可 dismiss/retry。投递终态审计 `notification.sent/failed` 同规则。漏洞卡 `notify` 段、人工推送 `in_flight` 判定都按漏洞去重键，运营通知不影响。

## 旧库

U2a/D1 阶段（二十三表，含 `webapi_egress_flows`）旧库由 `PrepareWebAPISchema` 单事务补 `webapi_notify_settings`、`webapi_notifications` 两表与三个索引，不改原表；更早的已知阶段一并补齐。V4 新增 `webapi_notify_ops_settings`、`webapi_notification_ops` 两表：M5b（四十二表）及更早已知阶段由 `PrepareWebAPISchema` 补表（不 ALTER 原表），原推送配置与记录保留，三开关为 false。

## 已验与未验

`-run 'Notify'` 九项临时库测试：配置读写（不回显、`""`/省略保留、`"-"` 清除、非 https/未知字段 400、AAD 绑定、密钥文件 0600）、测试推送；supported 登记 queued、非 supported/未配置/未启用不登记、回放不重复、同条目第二个候选不重复、fixed 后 regression、reproducible 不算修复、无资产 `finding:` 键、列表分页过滤；require_confirm 与 confirm/dismiss/retry 及 404/409；markdown 逐字（含 secret 替换、截断、无 base_url）、图片 base64+md5 可解码且与遮盖渲染逐字节一致；errcode 重试间隔到 failed、图片失败计次、网络错误不外露地址、45009 推迟不计次；每分钟 18 条窗口；30 行重渲与过大不发图；goroutine 投递、恢复 sending、Close 后退出；D1 阶段旧库补表与唯一约束。未向真实企业微信发送；前端未做。
