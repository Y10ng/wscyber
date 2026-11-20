# 研究场景（M1 后端）

任务的“研究场景”与“完成标准”：决定研究会话系统提示词里的角色、授权边界、黑板纪律、证据标准与方法；完成标准 M3 起交给 Goal 评估器。文案依据 `docs/scenarios.md`，常量在 `webapi/scenario_prompts.go`。只由人经鉴权 API 设置；模型、目标响应、资料都没有修改途径。

## 取值

| key | 名称 | 默认完成标准 |
|---|---|---|
| `webapi_pentest`（默认） | Web/API 渗透测试 | 地图中每个端点至少被一个已结 Oracle 覆盖；高优先级 Oracle 全部结案；成立的都已形成候选。 |
| `autonomous` | 自主研究 | 模型说明已穷尽有价值的方向，且无 open 的高优先级 Oracle。 |

- `scenario`：去首尾空白；空串＝默认 `webapi_pentest`；其他值 400（区分大小写）。
- `done_criteria`：去首尾空白，≤2000 字（按 Unicode 字符）；空串＝该场景默认文本。
- Go 导出 `scenarioDoneCriteria(key)` 给 M3（未知 key 返回空串）。

## 存储

| 表 | 列 | 说明 |
|---|---|---|
| `webapi_task_scenarios` | `task_id PK, scenario, done_criteria, updated_at` | 任务当前场景；无记录＝默认（`is_default=true`） |
| `webapi_asset_scenarios` | `asset_id PK, scenario, done_criteria, updated_at` | 资产默认场景；无记录＝默认 |

资产默认场景用独立表而不是给 `webapi_assets` 加列：本项目 schema 升级（`PrepareWebAPISchema`）只补表、不 ALTER。已知旧阶段新增一档 `{33, 34}`（含黑板），升级时补这两张表。

## 视图

任务与资产响应中的 `scenario`：

```json
{"key": "webapi_pentest", "name": "Web/API 渗透测试", "done_criteria": "", "done_criteria_effective": "地图中每个端点……", "is_default": true, "max_rounds": 8}
```

`done_criteria` 为保存的自定义文本（空＝用默认），`done_criteria_effective` 为实际生效文本，`max_rounds` 见「持续探索」，`is_default`＝无保存记录（与测试策略一致：显式保存过即为 false，即使值等于默认）。

出现位置：`GET /tasks/{id}`、`GET /tasks?session_id=`、`PUT /tasks/{id}` 响应的 `task`、`GET /task-list` 每项、`GET /assets`、`GET /assets/{id}`、`PUT /assets/{id}` 响应、从资产发起的响应 `task`。

## 接口

- `GET /api/webapi/scenarios` → `{scenarios:[{key, name, default_done_criteria, is_default}]}`（新建任务/资产表单用；按展示顺序，第一项默认）。
- `PUT /api/webapi/tasks/{id}` body 可选 `scenario`、`done_criteria`：先校验（非法 400，不建任务），**仅新建时**与任务同一事务保存；未提供＝默认（不写记录）。幂等重放与 409 冲突都不改已保存场景（改场景走下一条）。
- `PUT /api/webapi/tasks/{id}/scenario` `{scenario?, done_criteria?}`：严格 JSON（未知字段 400），至少提供一个（否则 400）；未提供的字段沿用当前值（只改场景时自定义完成标准保留，要恢复默认传 `"done_criteria":""`）；任务不存在 404。随时可改，下一次 Run 生效。响应 `{task_id, scenario}`。
- `PUT /api/webapi/assets/{id}` body 可选 `scenario`、`done_criteria`：**未提供＝保留当前值**（新资产为默认，不写记录），提供则校验（非法 400，资产不保存）后保存。资产其余字段仍是整份替换。
- 从资产发起（`PUT /assets/{id}/tasks/{task_id}`）与周期计划发起（同一 `LaunchAssetTask`）：新建任务时把资产默认场景（有记录时）在建任务事务内拷贝到任务；资产无记录则任务也无记录（默认）。已存在的任务按 task_id 幂等沿用，不按资产当前值重算；之后改资产不影响已建任务。

## 审计

- `task.scenario`：场景或完成标准实际变化才登记；`detail {from, to, done_criteria_changed}`，完成标准正文不进审计；summary 如“修改任务研究场景 Web/API 渗透测试 → 自主研究；完成标准已修改”。
- `task.create` 的 detail 增加 `scenario`（建成后的 key）。
- `asset.create/update`：`changed` 可含 `scenario`、`done_criteria`，`values` 只带 `scenario`（完成标准正文不进审计）。

## 系统提示词注入

`Capability.ToolExtension()` 订阅上游 `agent/hooks.BeforeRun`（无需改 Hub 装配）。每次 Run：

1. `SessionID` 必须是某任务的研究会话（`webapi_tasks.initial_session_id`）；验证/复测/报告会话与普通对话、空会话 → 原样。
2. 读任务当前场景与测试策略（每次读库）；读失败只记日志、提示词原样，不阻断运行。
3. 把上游中性骨架身份句（`You are the agent operating inside a cyber-harness runtime. …`）替换为本项目身份段；找不到则前置身份段。已含本项目身份段（已注入）则不再处理。
4. 末尾追加 `## ` 二级标题段落：`授权与边界`、`黑板纪律`、`证据标准`、`研究场景：<名称>`（方法）、`完成标准`（“完成标准：<生效文本>” + 自查提示）。
5. 测试策略 `scope=unrestricted` 时“授权与边界”第一条改为“本任务测试策略为不限制范围：可访问任意地址，但所有请求仍被记录；仍须遵守禁止行为与审批。”，其余不变。

首条消息不变（场景由系统提示词承载；Go `webAPIFirstMessage` 与前端逐行一致的那份保持现状）。提示词只是引导；授权范围、审批与证据记录仍由平台强制。

## 持续探索（M3 后端）

研究会话的 Run 走上游 Goal 模式（`agent/evaluator.RunWithEval`）：每轮 Agent 自然结束后，独立评估调用按完成标准判 `pass/continue` 并给 `feedback`；未通过且 `continue=true` 时 feedback 作为下一轮用户输入，`max_rounds` 为硬上限。不改上游循环。代码：`webapi/explore.go`。

### 最大轮数 `max_rounds`

- 场景视图（任务、任务列表、资产、发起响应的 `scenario`）增加 `max_rounds`：**生效值**＝保存值或场景默认（`webapi_pentest`=8、`autonomous`=12）。未保存时随场景切换而变；保存后不随场景变。
- `GET /scenarios` 每项增加 `default_max_rounds`。
- `PUT /tasks/{id}/scenario` 可带 `max_rounds`（JSON 整数 1..20，否则 400，不保存）；未提供沿用当前保存值。三项至少提供一个。
- `PUT /assets/{id}` 可带 `max_rounds`（同上校验；未提供＝保留）；从资产发起/计划发起时与场景一起拷贝到新任务，已建任务不随资产变。
- 新建任务 `PUT /tasks/{id}` 不处理 `max_rounds`（字段被忽略），在 `/scenario` 上设置。
- 存储：`webapi_task_explore(task_id PK, max_rounds, updated_at)`、`webapi_asset_explore(asset_id PK, max_rounds, updated_at)`（不 ALTER 场景表）。保存最大轮数时场景记录一并 upsert（`is_default` 因而为 false）。已知旧阶段新增一档 `{35, 34}`（含研究场景），升级时补这两张表。
- 审计：`task.scenario` 在最大轮数生效值变化时登记，`detail.max_rounds {from,to}`，summary 如“最大轮数 12 → 1”；`asset.create/update` 的 `changed`/`values` 可含 `max_rounds`。

### 何时开 Goal 模式

- 服务端 `startSession`（资产手动发起、周期计划发起的研究会话）：`RunTurnRequest.Extensions` 带 `types.AgentRunOptions{EvalCriteria: done_criteria_effective, EvalRounds: "<max_rounds>"}`（发起时读库）。
- 前端新建任务后的首条研究消息：`sendChatMessage(..., {persist: true, evalCriteria, evalRounds})`，值取自任务对象 `scenario`。
- **不带**：验证/复测（含计划后续）/报告会话、用户后续手动消息；**本机受控模型节点**（`wscyber.local-model.v1`，脚本模型不能充当评估器，且 maxTurns=3 预算不变）——服务端与前端都按节点能力跳过，保持原单次 Run。

### 评估提示词追加平台黑板

上游评估器的系统/请求提示词经 prompt Resolver（`EvaluatorSystem`/`EvaluatorRequest` 目标）构建；`Evaluation.Progress` 由上游 `buildProgress` 填（轮次、上限、历次 reason/feedback）。Contribution 拿不到会话身份（ctx 无 Invocation），因此：

1. `RunEnd` 钩子（与评估在同一 goroutine、紧接评估前同步触发，带 SessionID）：研究会话正常结束的 Run 登记“待评估”`{task, 生效完成标准, 消息数}`；出错/取消的 Run 清除（上游不评估）。
2. `EvaluatorRequest` Contribution（`wscyber.webapi.explore`）：按 Trace 首行 `| Messages: N |` 与 `Criteria` 认领**唯一**一条（2 分钟内）；认领不到或不唯一则不追加，不会把别的任务黑板交给评估器。已知限制：发起后修改完成标准，本次循环的评估不再附黑板（Criteria 与库中不一致）。
3. 追加在上游请求段之后（追加不替换）：
   - `## 平台黑板`：`boardDigest(task, 2000)`；一行 `地图端点 N，未覆盖 M；Oracle open … / in_progress … / holds … / refuted … / blocked …`；一行与上一轮评估比较的增量（新增事实条数、新结论次数、连续无新增轮数；首轮写“首轮评估”）。读库失败如实写“本轮读取平台黑板失败”。
   - `## 平台评估要求`：按任务完成标准并以黑板为准判定；未达标时 feedback 必须点名下一轮要处理的 Oracle 编号或未覆盖端点；连续两轮黑板无新事实/新结论时判 `pass=false, continue=false` 并说明原因。

### 与既有判定的关系（已核实）

- 整个 Goal 循环只有一对 `TurnStarted`/`TurnEnded`（会话层 `executeRun` 包住 `RunWithEval`，各轮共用同一 turn_id），中间有 `eval_start/eval_end` 状态事件与 feedback 用户消息。因此计划后续的 `sessionEnded`、任务 running/idle、`SessionRunning` 在评估与后续轮次期间仍判为运行中，最终结束后才判结束。
- unknown 阻断（`ModelRequestPolicy` 按 session+turn 查未决 HTTP，Goal 各轮共用 turn_id）：未决请求出现后本轮模型调用被拒（`error`，上游不评估、直接结束），或工具以 `terminated` 结束本轮（上游仍会评估）；后者下一轮首个模型调用必被拒，不会绕过阻断。为使循环正常停在本轮而不是以 error 结束，评估时若本 turn 有未决 HTTP，黑板段首行要求评估器判 `pass=false, continue=false`。
- 用量：评估调用由上游直接调 Provider，不产生 `Event_Usage`，**不计入**平台用量统计（已知缺口，属上游行为）。
