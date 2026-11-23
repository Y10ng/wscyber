# 报告 · 原生实现

2026-11-16 后端片。仅临时库定向测试与带补丁 Hub 构建通过；前端、真实模型写草稿、真实宿主 API 未验。

## 语义

- 报告固定同任务若干候选，每项一条不可变修订，可附该修订上的验证（`source_revision_id == revision_id`，结果可空）。只存关系，不复制研究/验证会话。
- 保存生成独立原生 session ID，不启动；前端走原生 Open/Run，首条消息由前端拼。
- 报告 session 不被 `taskForSession` 解析：`webapi_http`/`webapi_finding` 0 发送/不写；`webapi_verification_result` 不认作验证 session。
- 草稿只是报告会话模型的撰写；不确认候选、不裁定严重度、不发布。导出不调用模型。

## API（原生 Web Capability 鉴权/路由）

| 方法与路径 | 作用 |
|---|---|
| `PUT /api/webapi/tasks/{task}/reports/{report}` | body `{"items":[{"finding_id","revision_id","verification_ids":[]}]}`；1–50 项、finding 不重复、每项验证 ≤16；缺省 `verification_ids` 等同空数组 |
| `GET /api/webapi/tasks/{task}/reports?limit&cursor` | 分页摘要（固定 items、`has_draft`、`title`：草稿标题，无草稿为空串） |
| `GET /api/webapi/tasks/{task}/reports/{report}` | 详情：items 展开为修订快照＋验证摘要/结果，`draft` 或 null |
| `GET /api/webapi/tasks/{task}/reports/{report}/export?format=markdown\|json` | 由已存草稿＋固定来源确定性渲染；无草稿 409 |
| `GET /api/webapi/reports?session_id=` | 由报告 session 反查详情；不存在 404 |

PUT 回包 `{report,created,idempotent}`：首次 201；同 ID 同 items 200 回放（先于当前修订检查，候选更新后旧报告仍可回放）；同 ID 异 items 409；新建固定非当前修订 409；候选/验证不属于该任务或候选、验证来源修订不符 404。首条 SQL 即 INSERT 认领，校验失败整体回滚。

## 草稿工具 `webapi_report_draft`

参数 `title, overview, items[{finding_id, description, reproduction, remediation?, evidence_ids[]}], limitations?`，strict JSON、未知字段拒绝（不接受自报 report/session），≤128KiB。

- 归属取自原生 Invocation 的 session；非报告 session 拒绝。
- items 与报告固定 items 一一对应（同数量、每个 finding 恰一次）。
- `evidence_ids` 只能取该项固定修订的证据快照或所附验证结果的证据快照。
- 不可变：同 Invocation 同规范载荷回读；改载荷或另一 Invocation 拒绝；插入失败只核对一次回执。

### 参数错误

沿用工具错误结果返回，不改变协议或校验规则。可独立判定的错误逐行列出，最多 10 条；路径含数组下标（从 0 开始）。JSON 无法解析时先返回语法/类型错误；载荷整体错误用 `$` 标记。形状校验通过后再核对固定来源。

例如：

```text
items[2].reproduction必填为空；期望非空值
items[2].evidence_ids[0]: evidence_ids只能来自该候选固定修订的证据快照或所附验证结果的证据
```

第一条是正文形状错误；第二条是来源核对错误，不意味着两阶段必然同时返回。报告证据规则不是“本报告会话新建”，仍然是该候选固定修订与所附验证的快照集合。

## 导出

JSON `wscyber.report.v1`：按固定顺序合并草稿与来源（编号、标题、修订、证据快照、验证结果、草稿来源 session/turn/call）。Markdown：标题与声明、概述、逐候选（编号/标题/修订、描述、复现步骤及证据引用、修复建议、验证判读或"未提供"）、局限。不含生成时间等易变字段，同输入字节一致。

## 旧库

`PrepareWebAPISchema`（原 `PrepareVerificationSchema`）：空库交原生；完整结构直接通过；已知阶段（旧四表、含验证六表）经原生严格校验后在单事务内补齐其余表与索引，不 DROP/ALTER；未知/半成结构拒绝。启动新 Hub 指向已知旧库会执行补表。
