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
{"key": "webapi_pentest", "name": "Web/API 渗透测试", "done_criteria": "", "done_criteria_effective": "地图中每个端点……", "is_default": true}
```

`done_criteria` 为保存的自定义文本（空＝用默认），`done_criteria_effective` 为实际生效文本，`is_default`＝无保存记录（与测试策略一致：显式保存过即为 false，即使值等于默认）。

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
