# 子 Agent 认领 Oracle 并行探索（M5a 后端）

研究会话在黑板上有多个独立 open Oracle 时，可用上游 `subagent` 工具派出**命名子 Agent `oracle_explorer`**，每个认领一个 Oracle 并行探索。子 Agent 发出的请求、出口流量、候选都照常归属任务，受同一测试策略 / 审批 / 出口约束并存证；子会话结束后 Oracle 有结论，或自动退回 `open`；父会话收到上游完成通知（async）或工具结果（sync）。

本片只做后端：临时库合成数据 Go 测试通过（`webapi/subagent_test.go`、`webapi/subagent_harness_test.go` 用真实上游 Harness/Session/subagent 工具 + 脚本化 fake provider 走完整链），前端、真实模型未验。

代码：`native/webapi/subagent.go`（准入、预占、生命周期、子 Agent 定义、接口）、`scenario_prompts.go`（提示词常量）、`approval.go`（钩子装配与审批经父会话聚合）、`request_store.go`（`taskForSession` 识别子会话）、`board_tool.go`（认领排他）。不改上游。

## 装配

- `Capability.ApprovalExtension()` 内订阅：`tool.before` `wscyber-webapi-subagent`（准入，替换 M5-0 的一刀切拒绝）、`tool.completed` `wscyber-webapi-subagent-release`（未启动预占清理）、`session_start` `wscyber-webapi-subsession-start`（FailClosed）、`session_end` `wscyber-webapi-subsession-end`，`ModelRequestPolicy` `wscyber-webapi-subsession-model-limit`（子会话模型调用上限），以及事件流观察（子会话用量）。
- `Capability.SubagentExtension()` 向上游 Subagent Point 注册 `oracle_explorer`（`DefaultMode: async`）；两种模型节点（`model_product.go`、`model_node.go`）都装。本机验收节点的 `wscyber-model-node-subagent` 只拦**非** `oracle_explorer` 的 create（`catalog/list/kill` 与 oracle_explorer 交给平台准入）。

## 准入（全部满足才放行，否则拒绝并给出原因；拒绝不改任何状态）

| 条件 | 拒绝文案（均以 `已拒绝 subagent：` 开头） |
|---|---|
| `action` 为空/`create`；`catalog`/`list` 直接放行；`kill` 只能 kill 本任务的子会话（调用方须为该任务研究会话） | `session_id="…" 不是本任务的子 Agent 会话` / `只有 Web/API 任务的研究会话可 kill 本任务的子 Agent` / `未知 action …` |
| `name == "oracle_explorer"` | `只开放命名子 Agent oracle_explorer（name="oracle_explorer"），匿名或其它名字不放行` |
| `mode` 为空 / `async` / `sync` | `mode=fork 不放行（fork 会带入父会话历史）；用 async（默认）或 sync` |
| 有可信 session/call | `缺少可信原生 session/call 上下文` |
| 调用方是任务**研究首会话**（`initial_session_id`）；验证/复测/报告、普通对话、子会话本身都拒绝（不递归） | `只有 Web/API 任务的研究会话可派出 oracle_explorer；…` |
| `prompt` 首个词为 `O-NNN` | `prompt 须以本任务 Oracle 编号开头（如 "O-001 先读黑板……"）` / `prompt 首个词 "…" 不是 Oracle 编号；…` |
| Oracle 存在 | `本任务不存在 O-099` |
| 状态 `open`，或 `in_progress` 且 `claimed_session_id` 为空/调用方首会话 | `O-003 已被会话 X 认领，不能再派子 Agent` / `O-002 已是终态 refuted，不能派子 Agent；…请用户先在任务页重开` |
| 该 Oracle 没有运行中/预占中的子 Agent | `O-001 已有子 Agent 在探索，不能重复派出` |
| 任务运行中子会话 + 预占行 < `maxOracleExplorers = 3`（资源约束） | `本任务进行中的子 Agent 已达上限 3 个，等其结束后再派` |

审批策略 `approval` 不影响准入（子会话的审批挂起可经父会话看到并处理，见下）。

## 预占 → 落表 → 释放

1. **预占**（准入放行时，同一写事务）：Oracle 置 `in_progress`、`claimed_session_id=''`，写 `webapi_board_oracle_events`（actor `ai`，summary `子 Agent 认领`，session 为父会话），插入预占行（键：父会话 + 调用 `CallID`）。同一调用重复准入幂等。
2. **Prepare**（上游 `Registry.Start` 调用）：按（父会话, CallID）读预占；无预占则返回错误，子会话不启动。
3. **SessionStart**（FailClosed）：`ParentID + ParentToolCallID` 匹配预占且 `Delegation.AgentType == oracle_explorer` → 删预占、写 `webapi_task_subsessions(status=running)`、Oracle `claimed_session_id` = 子会话 id。父会话属于任务但无预占、或类型不符 → 返回错误，子会话不启动。父会话不属于任何任务（普通对话）不处理。**只对委派子会话（有 `ParentToolCallID`，上游 `sessionexec` 总会设置）生效**：会话轮换（`/clear`、`/compact`、恢复）以 `ParentSessionID` 开出的续接会话 `ParentToolCallID` 为空，不受影响（不拦截、不落表）；SessionEnd 同样只处理委派子会话。
4. **工具结束**（`tool.completed`）：本调用的预占若仍在（子会话没启动：Prepare/打开失败）→ 删除并把 Oracle 退回 `open`（summary `子 Agent 未启动，退回 open`）。
5. **SessionEnd**：行置 `ended`，记 `stop_reason`（上游 StopReason：completed/canceled/error/…）与 `error`（≤2000 字）；Oracle 仍 `in_progress` 且 `claimed_session_id` 为该子会话 → 退回 `open`、清认领，事件 summary `子 Agent 结束未给结论（<stop>）`，有 error 时追加 `：<error>`（含模型调用上限原因）。已结案的不动。
6. **Hub 重启**：`Capability.Routes`（拿到原生管理 API，即 Hub 启动时；模型节点随 Hub 进程启动，旧子会话已不存在）把遗留 `running` 行按 `stop_reason=interrupted`、`error="Hub 重启时子会话仍在运行，按已中断结束"` 结束并同样释放 Oracle，遗留预占行清除并释放。

## 表

| 表 | 列 | 说明 |
|---|---|---|
| `webapi_task_subsessions` | `session_id PK, task_id, parent_session_id, parent_call_id, oracle_id, agent_name, status running\|ended, stop_reason, error, created_at, ended_at` | 索引 `(task_id, created_at DESC)` |
| `webapi_subagent_reservations` | `parent_session_id, parent_call_id`（联合 PK）`, task_id, oracle_id, created_at` | 准入到 SessionStart 之间的预占；正常为空 |
| `webapi_subsession_usage` | `id PK(事件 id), session_id, task_id, model, input_tokens, output_tokens, total_tokens, cache_read_tokens, cache_write_tokens, created_at` | 节点侧观察到的子会话 `Event_Usage`，索引 `(task_id, created_at)` |

时间为定宽 UTC 毫秒（与黑板一致），用量 `created_at` 为事件时间 RFC3339Nano。旧库：含一键重放三十八表阶段经 `PrepareWebAPISchema` 单事务补三表（不 DROP/ALTER）。

## 识别范围（子会话 → 所属任务）

`taskForSession` 在首会话/验证/复测之后识别子会话，以下各处随之统一：
- `webapi_http`：同一授权 origins / 策略 / 审批；存证 `session_id` 为子会话；unknown 阻断按子会话的 turn。
- 出口代理 `egressTaskForSession`：按任务策略放行/拒绝，`webapi_egress_flows.session_id` 为子会话。
- 审批 `policyForSession` / `admitTool` / `admitCommand`：按任务测试策略；审批原因追加 `（子 Agent oracle_explorer 会话 <id>）`。
- `webapi_finding`：可新建/更新候选，来源会话记子会话。
- 黑板：读全部；`webapi_board_map`、`webapi_board_fact` 可写；`webapi_board_oracle` 可 `propose` 新 Oracle，可 `close`/`drop` **自己认领的那一个**，不能 `claim` 其它 Oracle、不能动别的 Oracle（`冲突（409）：子 Agent 只能 close 自己认领的 Oracle，…`）。认领排他：子 Agent 持有（运行中或预占中）的 Oracle，研究会话的 claim/close/drop 返回 `冲突（409）：O-001 正由子 Agent 探索（…），不能 …；等子 Agent 结束或由用户在任务页重开/放弃`。用户经 HTTP 重开/放弃不受限（人工操作优先；之后子 Agent 再结案会因 `claimed_session_id` 已清空被拒）。
- 验证/复测/报告逻辑不变；`researchTaskForSession`（场景注入、Goal 评估）不认子会话。

## 命名子 Agent `oracle_explorer`

- 工具白名单：`webapi_http`、`webapi_authz_compare`、`webapi_finding`、`webapi_board_read|map|fact|oracle`（包装执行器：定义只列白名单，其它名字执行即报错；bash/终端/subagent/文件工具都不可用）。多账号占位符与对比存证见 `native/AUTHZ.md`。
- 系统提示词 = 上游 `main.system` 骨架（经 `PromptResolver`）→ `injectScenarioPrompt`（与研究会话同一构建函数：本项目身份段 + 授权与边界〔按 scope〕+ 黑板纪律 + 证据标准 + 场景方法 + 完成标准）+ `oracleExplorerPromptTemplate`（`## 子 Agent：oracle_explorer`：只负责 O-NNN、先读黑板、最少请求证实/证伪、成立先 `webapi_finding` 再 close holds、结束前必须 close 或 drop 并写 summary、最终回复一句结论）。
- 首条输入 = `请探索 Oracle O-NNN（你只负责这一个）。` + 问题、类别、优先级、端点 + `研究会话补充说明：<prompt 去掉编号后的余下文本>`。
- 不开 Goal 模式（上游子会话 SingleTask，单次 Run）。资源约束：每个子会话最多 `maxOracleExplorerModelCalls = 24` 次模型请求；`ModelRequestPolicy` 从第 25 次起 Deny，文案为“子 Agent 模型调用已达上限 24 次，停止；Oracle 未结案将退回 open”。计数在进程内按 session 保存，SessionEnd 清除；不使用会被上游 Continue Run 覆盖的 `cfg.MaxTurns`。拒绝按上游语义结束子会话（stop_reason=error），照常释放未结案 Oracle；研究/验证/复测/报告会话不受此计数影响。
- 研究会话场景提示词黑板纪律段末句：存在多个独立 open Oracle 时可用 `subagent`（name=oracle_explorer，prompt 以 Oracle 编号开头）并行探索，最多 3 个。

## 接口

`GET /api/webapi/tasks/{id}/subsessions` → `{items:[{session_id, oracle:{id, number, question, status}|null, status running|ended, stop_reason, error, created_at, ended_at|null, request_count, finding_count}]}`，按 `created_at` 新到旧，最多 50 条；`request_count` = `webapi_requests` 中该子会话行数（含拒绝/未发送），`finding_count` = 该子会话写过修订的不同候选数；任务不存在 404。

任务视图运行态：有 `running` 子会话时任务列表/总览 `status=running`。计划任务“上一次仍在运行”也包含该任务的 `running` 子会话；后续自动验证/复测仅在研究会话结束且该任务没有 `running` 子会话时规划。

## 可见性调查结论与已知限制

- **子会话事件 Hub 不持久化**：节点把所有事件转给 Hub（`pkg/node/proto_connection.go:196` Observe → `pkg/node/tool/calls.go:122` Forward），Hub `forwardAOPFrame`（`pkg/web/service/agents_mux.go:229-258`）按 `turn_id` 查 `TaskSession` 找会话；子会话 turn 不是 Hub 派发的（`session.go:118 registerSessionTask` 只登记 Hub 发起的 Run），查不到后只接受 SessionStarted/SessionEnded 或该节点 `openSessions` 中的会话；子会话不在其中，工具调用/消息/usage/审批事件被丢弃。SessionStarted/Ended 虽被接受，但该会话没有 `chat_sessions` 行，`chat_aop_events` 插入违反外键，`acceptAOPEvent` 查不到会话即静默丢弃（`events.go:50-100`、`store_sqlite.go:353`）。因此**子会话转录不可在 Hub 按会话读取**；平台侧可追溯的是 `webapi_requests` / `webapi_egress_flows` / 候选修订 / 黑板事件（均带子会话 id）与 `/subsessions` 列表。父会话的 `subagent` 工具结果 / 完成通知（含子 Agent 最终一句结论）在父会话事件中。
- **审批挂起可看到并处理**：上游 Hub `forwardGuardrail`（`pkg/web/service/guardrail.go:16-40`）按请求的 session 查 Hub 会话行取节点，子会话无会话行无法直接查询。本项目在节点侧补齐：对研究会话的 `Pending` 一并返回其运行中子会话的待审批（`Review.session_id` 仍为子会话），`Resolve` 以研究会话 id 提交时，若该 operation 属于其运行中子会话则代为处理（其它会话不能代批）。前端现有待审批聚合（`useGuardrailReviews` 按会话轮询 Pending，`App.tsx` 以轮询到它的会话 id 调 Resolve）已轮询研究会话，因此子 Agent 的待审批会出现在研究会话名下并可批准/拒绝（后端测试覆盖，浏览器未验；显示上不区分子会话，留给前端片）。
- **用量**：子会话 `Event_Usage` 不进 `chat_aop_events`，本项目在节点事件流上观察并写 `webapi_subsession_usage`，用量接口按任务计入（角色 `subagent`），见 `USAGE.md`。仅在模型节点与 Hub 同进程（本项目两种节点均如此）时有效。
- 审批状态与子会话都在节点内存：节点/Hub 重启时子会话随之终止，按上文第 6 步释放。
- `sync` 模式会阻塞研究会话本次工具调用直到子会话结束；`timeout` 参数按上游语义只用于 sync。
