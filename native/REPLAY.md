# 一键重放（K2 后端）

漏洞卡上由**人触发**、对最小复现某一步关键请求在**任务授权范围内**原样重放一次，新响应留存为新证据，并与原步骤做确定性对比。只做后端；前端下一片做。实现 `native/webapi/replay.go`（发送与存证复用 `http_tool.go` 的 `requestRecorder`，与 `webapi_http` 同一条 claim→send→finish 路径）。仅临时库合成数据 Go 测试通过（`replay_test.go`，httptest 本地目标），真实 Hub 数据未验。

> **重放结果是平台对比提示，不是复测结论。** 它不修改漏洞状态、不写处置（triage）、不触发企业微信推送、不进入复测记录。正式复测仍走复测会话（`native/RETEST.md`）。

## `POST /api/webapi/vulns/{key}/card/replay`

请求体 `{"step": N}`（1..3，默认 1；空请求体＝默认；未知字段/非整数 400）。原生鉴权；错误体 `{error, …}`。

### 定位与重建

- 与 `GET /vulns/{key}/card/curl` 同一定位（`locateReproStep`）：条目的 repro（最新 supported 且带 key_steps 的验证）第 N 步证据原件 Flow。
- 请求**原样**重建，不遮盖（遮盖只用于展示）：方法、URL、原始头、正文。跳过 HTTP/2 伪头（`:` 开头）与 `Content-Length`（客户端按正文重算）。`Host` 头必须与 URL authority 一致（URL 是唯一 Host 来源，与 `webapi_http` 相同），一致则交给客户端从 URL 生成，不一致 422。其余头与 `webapi_http` 同一校验（不允许 `Transfer-Encoding`/`Connection`/`Proxy-Authorization`，名/值须合法），不合格 422。原件带的 `User-Agent`/`Accept-Encoding` 原值发送，原件没有时同 `webapi_http` 默认值（`wscyber-webapi/1`、`identity`）。
- **头顺序**：同名多值按原件顺序发送；不同名头之间的线上顺序由 Go `net/http` 客户端规范化（按名排序，名规范大小写），与 `webapi_http` 一致——共用 `directClient`，不另写传输层。
- 原件无法解析或无请求 422；条目不存在、无最小复现（`尚无最小复现`）、证据不存在 404；step 超出步数 400；键非法 400。

### 授权（每次现读，与 `webapi_http` 同一判定 `httpPolicyDecision`）

| 情况 | 响应 | 发送 | 记录 | 审计 |
|---|---|---|---|---|
| 证据所属任务不存在 | 404 | 否 | 无 | 否 |
| 任务所属资产已归档 | 409 `{error, status:"archived"}` | 否 | 无 | 否 |
| `scope=origins` 且 URL origin 不在任务授权 origins 内 | 403 `{error, status:"denied", evidence_id}` | 否 | 按 `recordUnsent` 留 `status=denied, sent=false` 记录 | 否 |
| `httpReviewReason(policy.Approval, …)` 非空（DELETE、破坏性信号、`approval=all` 的非只读方法） | 409 `{error, status:"review_required", reason}` | 否 | 无（不是挂起，没有待审项） | 否 |

本片不做重放的审批挂起流程：人点按钮本身是人工动作，但危险请求仍不允许一键发出。审批判定用的头视图把同名多值以 `", "` 合并（方法覆盖头检查与 `webapi_http` 同口径）。

### 并发、限频与不重试

- 同一 `{key, step}` 同时只允许一个进行中：409 `{error, status:"replay_in_progress"}`（先于限频判断）。
- 同一 `{key}` 每 10 秒最多放行 1 次发送：429 `{error, retry_after}`（整数秒，同时带 `Retry-After` 头）。只有通过授权判定、真正进入发送的请求占用窗口；403/409/404/400 不占。
- 占位与窗口在 Hub 进程内存（`Capability.replays`），重启清空。限频时钟用能力时钟（测试可注入）。
- 不自动重试。发送失败、超时、读响应失败 → 502 `{error, status:"unknown", evidence_id}`，证据记 `unknown`（同 `webapi_http`：“目标结果未知，不自动重试”）；保存失败记 `capture_failed`，也返回 502 unknown。
- 同任务同指纹已有未决 `claimed/unknown/capture_failed`（例如上一次重放结果未知）时，认领事务的现有保护拒绝发送：409 `{error, status:"unresolved", evidence_id}`（指向那条未决证据）。这条保护与模型调用共享：一次结果未知的重放也会阻止同任务内模型对完全相同请求的 `webapi_http`，反之亦然。
- 人工点击后发送与存证不随页面断开而取消（`directClient` 30s 超时兜底），保证记录走完状态机。

### 存证归属

- 新证据写 `webapi_requests`：`task_id` = 该步证据所属任务；`session_id="replay"`、`turn_id=<replay_id>`（`rp_` + 32 位十六进制）、`call_id="step-N"`。这些不是模型会话：`taskForSession` 映射不到，因此 `unresolvedTurn` 等模型会话逻辑不受影响；认领走 `claimRequest`（由 handler 先核对任务与授权，跳过 session→任务映射，其余同 `claim`）。
- 未新增 `webapi_requests` 列：`source=replay` 由 `session_id="replay"` 表达。`GET /tasks/{id}/evidence` 与 `/evidence/{evidence_id}` 对这类记录正常返回，`metadata.source = "replay"`（模型调用的记录不带该字段）。图片接口照常可用。
- 黑板地图摄取（M2）照常把已发送且有响应的重放记录并入接口地图（与其它请求一视同仁）。
- 表 `webapi_replays(id, vuln_key, step, task_id, evidence_id, original_evidence_id, method, url, status_code, verdict_hint, created_at)`，索引 `(vuln_key, created_at DESC, id DESC)`：每次**收到响应**的重放一行（结果未知不写），作为漏洞键 → 重放的索引与对比结论；请求/响应本体仍只在 `webapi_requests`。`url` 去 userinfo。比任务说明多 `method/url` 两列，供生命周期 detail 用，免于再解码原件。

### 返回 200

```
{
  replay_id, step, evidence_id（新证据）, task_id, at,
  request: {method, url（去 userinfo）},
  original: {evidence_id, status_code, body_bytes, marks_found: {request:[], response:[]}},
  replayed: {status_code, body_bytes, capture_truncated, marks_found: {request:[], response:[]}},
  comparison: {same_status, response_marks_still_present, verdict_hint},
  note: "重放结果是平台对比提示，不是复测结论；不修改漏洞状态、不写处置、不触发推送"
}
```

- `body_bytes`：已捕获响应体字节数（重放侧上限 64KiB，超出 `capture_truncated=true`）。
- `marks_found`：该步 `request_marks`/`response_marks` 分别在原件与新证据中逐行子串查找（同 K1 `marksIn`：请求行/状态行、原始头、正文逐行，未遮盖，二进制正文不查），找到的按原顺序去重。

### 对比规则（确定性）

基准 = 原步骤原件中**找到的** response marks（即卡片上实际高亮的片段）。

| 原步骤基准 | 条件 | `verdict_hint` |
|---|---|---|
| 有 | 基准全部出现在新响应 **且** 状态码相同 | `still_reproducible`（仍可复现） |
| 有 | 否则 | `changed`（有变化） |
| 无 | 状态码相同 | `unknown`（无法判定） |
| 无 | 状态码不同 | `changed` |

`same_status` = 原/新状态码相等；`response_marks_still_present` = 有基准时基准是否全部仍出现，无基准时 `false`。发送失败/结果未知不返回 200（见上）。

## 漏洞卡联动（`native/VULN_CARD.md`）

- `GET /vulns/{key}/card` 增加 `replays[]`：最近 5 次 `{replay_id, step, evidence_id, at, status_code, verdict_hint}`，新到旧。
- `lifecycle` 增加 kind `replay`（actor=user，title“一键重放：仍可复现/有变化/无法判定”，detail `METHOD /path → 状态码`），同一时刻排在 notify 之后、retest 之前。全部已完成重放都进生命周期（不限 5 条）。
- 审计 `vuln.replay`（`native/AUDIT.md`）：每次实际发送登记一条（含结果未知，detail 另带 `outcome`）；未发送不登记。也出现在卡片 `appendix.changes[]`。

## 旧库

`PrepareWebAPISchema` 新增已知阶段“含持续探索三十七表（M3）”：单事务只补 `webapi_replays` 与其索引，不 DROP/ALTER；更早阶段照旧一次补到完整结构。

## 未覆盖（如实说明）

- 无重放审批挂起：危险请求一律 409，不能经审批后一键发出（需走研究/复测会话）。
- 跨名头的线上顺序不保留（见上）；重放请求在 Flow 里的头按名排序记录（同 `webapi_http`）。
- 进行中占位与限频不持久、不跨进程；Hub 重启后窗口清零。
- 结果未知的重放没有“人工核对/解除”接口（与 `webapi_http` 的 unknown 相同现状）：同一请求在该任务内此后不再发送。
- 不刷新测试账号会话：原件中的 Cookie/Authorization 过期时重放会得到 401 等，对比为 `changed`，需人工判断。
- 前端（按钮、结果面板、证据列表 source 标签）在下一片。
