# 候选独立验证 · 原生实现

2026-11-15。后端3项专项/Hub构建与前端tsc/Vite通过；不带模型配置的隔离数据库副本已通过真实保存失联核对、原生Open/Run、未配置Provider的失败展示、再次显式新验证、会话重开/返回候选及桌面/窄屏检查。没有伪造模型成功，**尚未验证真实模型形成判读或再次访问靶场**。随后用户明确批准原预览升级，已先做一次恢复副本并给原库加两表，原业务行逐项保全；新版42453入口已打开，未点击保存启动，两新表均0行。准确范围/可操作入口见[STATUS](../docs/STATUS.md)与本机`var/verification-local-check.EUWFUzeg/result.md`。

## 语义与原生复用

- 验证固定同任务候选的一条不可变修订，保留原编号、正文及历史；仅存关系，不复制全部原研究聊天或原件。
- 保存验证生成独立原生session ID，但不启动执行。后续仍由原生SessionService Open/Run及其请求账本驱动，不新增Harness/Provider/调度器。
- HTTP授权仍来自父Task精确origins；新HTTP记录带独立验证session。父task相同请求指纹的unknown保护不因换session失效。旧HTTP调用键回读还核对session，不能串用其它验证的回执。
- 验证session不能通过`webapi_finding`改原候选。结果工具从原生Invocation取得可信session/turn/call，不接受模型自报归属。

## API

均复用原生Web Capability鉴权/路由装配，验证保存只写本地状态。

| 方法与路径 | 作用 |
|---|---|
| `PUT /api/webapi/tasks/{task}/findings/{finding}/verifications/{verification}` | body仅`source_revision_id`；客户端稳定提交ID，UI使用UUID；首次201，同ID同来源200回读，异来源409、错归属404、新提交过期来源409 |
| `GET` 同上 | `VerificationDetail`：固定身份、来源修订`source`及`result`或null |
| `GET /api/webapi/tasks/{task}/findings/{finding}/verifications?limit&cursor` | 分页摘要、可空verdict；结果空不等于执行失败 |
| `GET /api/webapi/verifications?session_id=...` | 只读找回验证上下文；不存在404 |

PUT回包为`{verification,created,idempotent}`。创建的身份/current revision检查在同一先写事务内；已存在提交的回放先于当前revision检查。保存、Open、Run接受、执行结束与形成判读是不同事实，UI必须沿用原request ID核对，不能因失联换ID盲发。

## 结果与证据

`webapi_verification_result`参数为`verdict`、`method`、`preconditions`、`summary`、`limitations`及`evidence[{evidence_id,relation}]`。

- `supported`：本次新证据支持；`counterevidence`：本次实证反证；`inconclusive`：证据不足。这些是**模型判读**，不自动确认漏洞或认定修复。
- 引用须同task且来自本验证session。支持/反证至少有相应关系、已发送、completed、有实际HTTP响应、Flow完整且未捕获截断的原件；200/302/500本身都不是漏洞或资格判据。
- 未决/失败/截断不能作为有效支持或反证，仅可作为限制；inconclusive可无证据。工具既有unknown处理不被结果保存解除。
- 一个验证的结果不可变；同Invocation同规范载荷回读，变载荷/另一Invocation覆盖拒绝。保存错误与确定的覆盖冲突区分，不能用重发目标请求恢复数据库回执。

## 两表与已知旧结构

新增`webapi_verifications`、`webapi_verification_results`。`PrepareVerificationSchema`在宿主能力装配前运行：空库交原生建完整结构；完整新结构沿原生校验；已知旧core＋四业务表经原生校验后，在一个事务内只新增两表和索引。未知或半成结构拒绝，不DROP/ALTER旧表、不补造历史行。随后仍保留原生完整结构校验；没有新增通用迁移框架或上游schema补丁。

**启动新Hub指向已知旧库会执行这次加表。** 已先通过临时库和隔离副本检查，随后于2026-11-15在用户明确批准、原Hub正常退出并保存恢复副本后，应用到当前预览库；原任务/HTTP/候选及修订逐行保全，新验证表为空。其他旧库的升级/切换仍须单独按实际范围安排，不能把构建成功当作已升级。构建仍须使用包含事件锁修补的[`cyber-go-overlay.py`](../scripts/cyber-go-overlay.py)，见[运行说明](RUNNING.md)。

## 已验与未验

仅`^TestWebAPIVerification`三个临时库专项最终通过（0.128s）：固定来源/提交回放与冲突、HTTP归属与候选写保护、同时间分页、跨session调用冲突零新增发送、三种判读实际保存、伪造/跨session/原候选证据拒绝、HTTP500有效证据、截断限制、不可变结果，以及已知旧结构少量合成行保全/未知或半成结构拒绝。Hub独立编译通过。

未实际构造unknown/claimed资格矩阵；未跑旧suite或全仓。随后真实宿主新API鉴权与隔离前端的保存/失联核对/Open/Run失败/重开返回/窄屏路径已验证，原预览也已按授权完成本地加表与只读展示。真实模型完整验证链、非空判读浏览器链、完整恢复/研究质量及全工具出口仍未验。没有真实模型/目标重跑、公开部署或commit/push。

## 会话归属批量查询

`POST /api/webapi/session-kinds`，体 `{"session_ids":[...]}`（1–500 个，去重；空数组、超过 500 或未知字段 400）。响应 `{"items":{session_id: Item}}`，只含能识别的会话。
Item 字段：`kind`（task/verification/report/retest）、`task_id`、`task_session_id`（任务 `initial_session_id`）、`task_objective`、`number`、`title`、`created_at`（该业务记录时间）。
- task：number 空，title=objective；verification/retest：number/title 取固定候选修订（`source_revision_id`），与 by-session 接口 `source` 一致；report：number 空，title=最新草稿标题（无草稿为空）。
- 每类一次 `IN (...)` 批量查询（共 4–5 条 SQL），供前端侧栏会话归组，替代每会话最多 4 次 by-session 反查。只读，不改任何已有接口行为。
