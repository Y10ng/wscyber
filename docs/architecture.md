# 架构与代码地图

## 1. 总体

```
浏览器 ── Cyber 原生 UI（固定 cyber-ui + 本项目 source-overlay）
   │  原生 AOP/WebSocket + /api/webapi/* (本项目 Web Capability)
Hub（cmd/wscyber-hub，调用上游 pkg/web/host.Serve）── SQLite（原生表 + webapi_* 业务表）
   │  原生节点协议
模型节点（同进程 --model-node）── 原生 Provider / Runtime / StandardLoop
   └─ 工具：原生工具 + webapi_http / webapi_finding / webapi_verification_result
```

- **底座**：Cyber Harness 固定提交 `f4b1fb2f…`（Go 模块 `github.com/chainreactors/cyber`，`replace` 指向 cyber-harness），cyber-ui 固定 `147423bd…`。版本与许可见 `native/DEPENDENCIES.md`、`native/ui/LICENSES.md`。
- **原生负责**：Agent 循环、会话/turn、工具调度、AOP 事件持久化、模型配置与 Provider、原生页面（聊天、Workflow、配置）。
- **本项目负责**：任务授权与两输入、HTTP 证据、候选与编号、独立验证、以及这些对象的页面。业务数据由 `native/webapi` 通过原生 `SchemaModule` 存同一 SQLite。
- 原生运行事实只由原生记录；业务对象引用原生 session/turn/call，不双写执行状态。

## 2. 目录

| 路径 | 内容 |
|---|---|
| `native/cmd/wscyber-hub/` | 入口与装配：Hub、正式模型节点（`model_product.go`）、模型配置、UI 静态挂载。`--fixture-node`/`--model-endpoint` 为本机测试分支 |
| `native/cmd/wscyber-native/` | 早期 CLI fixture（原生会话/工具最小链），仅测试参考 |
| `native/webapi/` | Web Capability：任务、HTTP 工具与证据、候选、验证的存储/工具/HTTP API；`policy.go` 授权检查 |
| `native/modeltransport/` | 本机测试用的模型连接目的地约束 |
| `native/patches/` | 上游 SQLite 事务锁一行补丁（`_txlock=immediate`），经 `scripts/cyber-go-overlay.py` 构建时应用 |
| `native/ui/source-overlay/` | 对原生前端的覆盖文件（App、任务/候选/证据/验证面板、配置、i18n）；清单 `source-overlay.json` |
| `native/ui/` 其他 | 固定来源锁、依赖覆盖、构建说明 |
| `scripts/` | `cyber-go-overlay.py`（带补丁构建 Go）、`cyber-ui-build.py`（prepare/rebuild 前端）及其自检 |

## 3. 业务 API（`/api/webapi`，原生鉴权）

| 方法 | 路径 | 作用 |
|---|---|---|
| PUT/GET | `/tasks/{id}`、`/tasks` | 保存/读取任务（两输入 + origins + 初始 session）；保存不启动，启动走原生 Open/Run；响应带 `policy:{scope,approval,is_default,updated_at}` 与 `asset:{id,name}\|null` |
| PUT | `/tasks/{id}/policy` | 设置测试策略 `{scope: origins\|unrestricted, approval: off\|high_risk\|all}`，写当前值＋变更记录，保存即生效；见 `native/POLICY.md` |
| GET | `/tasks/{id}/evidence[/{evidence_id}]` | HTTP 证据列表与原件 |
| GET | `/tasks/{id}/findings[/{fid}[/history\|/revisions/{rid}]]` | 候选、修订 |
| PUT/GET | `/tasks/{id}/findings/{fid}/verifications[/{vid}]` | 发起/读取独立验证（客户端稳定 UUID，幂等） |
| GET | `/verifications?session_id=` | 由验证会话反查上下文 |
| PUT/GET | `/tasks/{id}/reports[/{rid}]` | 保存/读取报告（固定候选修订＋同修订验证；客户端稳定 UUID，幂等；保存不启动） |
| GET | `/tasks/{id}/reports/{rid}/export?format=markdown\|json` | 由已存草稿＋固定来源确定性导出，无草稿 409 |
| GET | `/reports?session_id=` | 由报告会话反查上下文 |
| PUT/GET | `/tasks/{id}/findings/{fid}/retests[/{rtid}]` | 发起/读取复测（固定候选修订；客户端稳定 UUID，幂等；保存不启动；结论与验证分开记录） |
| GET | `/retests?session_id=` | 由复测会话反查上下文 |
| GET/PUT | `/assets?archived=0\|1`、`/assets/{id}` | 资产登记（名称、负责人、环境、已知信息、默认目标、origins、禁止行为、默认策略、归档）；保存即生效；凭据只回 `has_secret`；见 `native/ASSETS.md` |
| PUT/DELETE | `/assets/{id}/credentials/{cid}` | 测试账号（secret 写-only，AES-GCM 落库，省略/空串保留原值） |
| PUT/GET | `/assets/{id}/tasks[/{task_id}]` | 从资产一次发起任务（建任务+策略+关联→Open→Run，按 task_id 幂等、可续完；Go 层 `Capability.LaunchAssetTask`）/ 关联任务列表 |
| GET/PUT/DELETE | `/assets/{id}/schedule` | 资产周期计划 `{cron, enabled}`（5 字段 cron，按 Hub 本地时区；保存即生效并重算 `next_run_at`，响应带下 3 次预计时间）；Hub 内单 goroutine 每 30s 到点经 `LaunchAssetTask` 发起，见 `native/ASSETS.md`「周期计划」 |
| GET | `/assets/{id}/schedule/runs?limit=&cursor=`、`/schedules` | 计划运行记录（launched/failed/skipped，按 scheduled_for 倒序分页）/ 全部计划（资产名、cron、enabled、next_run_at、最近一次 run） |
| GET | `/usage?task_id=&from=&to=` | 只读 token 用量汇总（按日/模型，全局按任务、任务内按会话角色）；解 `chat_aop_events` 中 `Event_Usage`，不含费用，见 `native/USAGE.md` |

工具：`webapi_http`（scope=origins 时只发授权 origins，unrestricted 时任意 http/https；均保存完整 Flow）、`webapi_finding`（保存/更新候选，引用同任务证据）、`webapi_verification_result`（验证会话写判读，引用本会话新证据）、`webapi_report_draft`（报告会话写不可变草稿）、`webapi_retest_result`（复测会话写不可变结论，引用本会话新证据）。验证细节见 `native/VERIFICATION.md`，报告见 `native/REPORT.md`，复测见 `native/RETEST.md`。

## 4. 关键约束与已知缺口

- 授权：`webapi_http` 按任务测试策略强制 origin（scope=unrestricted 时不限制、仍完整记录）；原生终端等工具**没有**强制出口。
- 审批：任务测试策略 approval（默认 high_risk）经原生 `tool.before` 挂起高危 `webapi_http` / 终端命令（all 时还有全部非只读 HTTP、终端与 `write`），以 `guardrail.Review` 事件出卡片，人经 Hub guardrail 命名空间 Pending/Resolve 批准；拒绝/超时（10 分钟）不执行，HTTP 记 `review_denied/review_expired` 未发送。终端规则是静态表，见 `native/POLICY.md`。
- unknown：同任务相同请求指纹的发送未知状态阻止自动重发。
- 旧库升级：新 Hub 打开已知旧结构库（四/六/八/十/十二/十五表阶段）时事务性补齐到完整结构（含测试策略、资产、周期计划表）；其他结构报错。没有通用迁移框架。
- 未做：全工具流量捕获、模型报告、复测、用量/原文记录、资料库接入、图谱/时间线。

## 5. 扩展原则

先查上游是否已有（固定源码在 `var/` 的 prepare 工作区或模块缓存），再找扩展点（Web Capability、工具注册、SchemaModule、UI 覆盖/插件组合），最后才是补丁。前端优先复用原生组件（TrafficPanel、HttpViewPanels、JEVControlFlow、Markdown 等），只覆盖必须改的文件。
