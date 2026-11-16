# 复测 · 原生实现

2026-11-16 后端片。执行路径与[独立验证](VERIFICATION.md)完全相同，区别只在记录分开、结论词汇不同。只有临时库定向测试和 Hub 构建通过；真实模型复测链、前端都还没验。

## 语义

- 复测固定同任务候选的一条不可变修订；保存时生成独立原生 session，但不启动。Open/Run 由前端走原生路径。原候选、修订、验证、报告都不会被覆盖。
- `taskForSession` 把复测 session 映射到父任务：`webapi_http` 可用，授权仍是父任务的精确 origins；父任务同指纹的 unknown 保护对复测 session 同样生效。
- 写权限隔离：复测 session 不能写 `webapi_finding`、`webapi_verification_result`、`webapi_report_draft`；验证/普通 session 不能写 `webapi_retest_result`。

## API（原生鉴权）

| 方法与路径 | 作用 |
|---|---|
| `PUT /api/webapi/tasks/{task}/findings/{finding}/retests/{retest}` | body 只有 `source_revision_id`；客户端稳定 UUID；首次 201，同 ID 同来源 200 回放（先于当前修订检查），来源不同 409，新提交用过期修订 409，归属错误 404；回包 `{retest,created,idempotent}` |
| `GET` 同上 | `RetestDetail`：固定身份、`source` 修订快照、`result` 或 null |
| `GET .../findings/{finding}/retests?limit&cursor` | 按时间倒序分页的摘要；`conclusion` 可以为空 |
| `GET /api/webapi/retests?session_id=` | 用 session 反查复测；不存在返回 404 |

## 结论工具 `webapi_retest_result`

参数：`conclusion`、`method`、`preconditions`、`summary`、`limitations`、`evidence[{evidence_id,relation}]`。归属取自原生 Invocation。

- `reproducible`（仍可复现）需要 supporting；`fixed_in_scope`（本次范围内已修复）需要 counterevidence；`inconclusive`（无法判定）可以不带证据。
- 只能引用本复测 session 的新证据。有效的支持/反证必须是已发送、completed、有实际 HTTP 响应、Flow 完整且未截断的原件。未发送、无响应（unknown）、截断只能作为 limitation。
- 登录失败、目标不可达、超时都不等于已修复，应给 `inconclusive`（工具描述里写明了）。
- 结论不可变：同一 Invocation 用同一规范载荷可以回读，其余情况拒绝。

## 表与旧库

`webapi_retests`、`webapi_retest_results`，列和语义与验证两表对齐（session 唯一索引；finding+created 分页索引）。`PrepareWebAPISchema` 新增已知阶段“含报告八表”：经原生校验后在单个事务里只补这两张表和索引，不 DROP/ALTER；半成结构拒绝。

## 代码复用

证据核对（`sessionResultEvidence`）、strict 载荷/正文校验、PUT 认领（`claimPinnedRevision`）、不可变保存（`saveImmutableResult`）、分页查询（`pinnedPage`）都从验证代码里抽成公共函数，验证改为调用这些函数，原有消息和语义不变。
