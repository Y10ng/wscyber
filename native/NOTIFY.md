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

## 旧库

U2a/D1 阶段（二十三表，含 `webapi_egress_flows`）旧库由 `PrepareWebAPISchema` 单事务补 `webapi_notify_settings`、`webapi_notifications` 两表与三个索引，不改原表；更早的已知阶段一并补齐。

## 已验与未验

`-run 'Notify'` 九项临时库测试：配置读写（不回显、`""`/省略保留、`"-"` 清除、非 https/未知字段 400、AAD 绑定、密钥文件 0600）、测试推送；supported 登记 queued、非 supported/未配置/未启用不登记、回放不重复、同条目第二个候选不重复、fixed 后 regression、reproducible 不算修复、无资产 `finding:` 键、列表分页过滤；require_confirm 与 confirm/dismiss/retry 及 404/409；markdown 逐字（含 secret 替换、截断、无 base_url）、图片 base64+md5 可解码且与遮盖渲染逐字节一致；errcode 重试间隔到 failed、图片失败计次、网络错误不外露地址、45009 推迟不计次；每分钟 18 条窗口；30 行重渲与过大不发图；goroutine 投递、恢复 sending、Close 后退出；D1 阶段旧库补表与唯一约束。未向真实企业微信发送；前端未做。
