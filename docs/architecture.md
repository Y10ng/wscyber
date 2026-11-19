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
| `native/webapi/` | Web Capability：任务、HTTP 工具与证据、候选、验证的存储/工具/HTTP API；`policy.go` 授权检查；`egress*.go` 全工具出口扩展与记录；`notify*.go` 企业微信推送（配置、登记、后台投递） |
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
| GET | `/tasks/{id}/egress?session_id=&decision=&limit=&cursor=`、`/tasks/{id}/egress/{flow_id}` | 全工具出口记录（终端经代理的放行/拒绝请求，倒序分页；详情含 Flow 头与 4KiB body 预览），见 `native/POLICY.md`「全工具出口（B2）」 |
| GET | `/tasks/{id}/evidence/{evidence_id}/image.png`、`/tasks/{id}/egress/{flow_id}/image.png`（`?mark=&max_lines=&mask=`） | 由 Flow 原件确定性渲染请求/响应 PNG（遮盖敏感头、mark 高亮、截断如实标注；404/400/422），包内 `renderEvidenceImage` 供推送调用；见 `native/VERIFICATION.md`「证据图片」 |
| GET | `/tasks/{id}/findings[/{fid}[/history\|/revisions/{rid}]]` | 候选、修订 |
| PUT/GET | `/tasks/{id}/findings/{fid}/verifications[/{vid}]` | 发起/读取独立验证（客户端稳定 UUID，幂等） |
| GET | `/verifications?session_id=` | 由验证会话反查上下文 |
| PUT/GET | `/tasks/{id}/reports[/{rid}]` | 保存/读取报告（固定候选修订＋同修订验证；客户端稳定 UUID，幂等；保存不启动） |
| GET | `/tasks/{id}/reports/{rid}/export?format=markdown\|json` | 由已存草稿＋固定来源确定性导出，无草稿 409 |
| GET | `/reports?session_id=` | 由报告会话反查上下文 |
| PUT/GET | `/tasks/{id}/findings/{fid}/retests[/{rtid}]` | 发起/读取复测（固定候选修订；客户端稳定 UUID，幂等；保存不启动；结论与验证分开记录） |
| GET | `/retests?session_id=` | 由复测会话反查上下文 |
| POST | `/session-kinds` | 批量会话归属（1–500 个 session_ids，去重；返回 task/verification/report/retest 的 kind、所属任务、编号/标题/created_at；只含可识别会话），见 `native/VERIFICATION.md`「会话归属批量查询」 |
| GET/PUT | `/assets?archived=0\|1`、`/assets/{id}` | 资产登记（名称、负责人、环境、已知信息、默认目标、origins、禁止行为、默认策略、归档）；保存即生效；凭据只回 `has_secret`；见 `native/ASSETS.md` |
| PUT/DELETE | `/assets/{id}/credentials/{cid}` | 测试账号（secret 写-only，AES-GCM 落库，省略/空串保留原值） |
| PUT/GET | `/assets/{id}/tasks[/{task_id}]` | 从资产一次发起任务（建任务+策略+关联→Open→Run，按 task_id 幂等、可续完；Go 层 `Capability.LaunchAssetTask`）/ 关联任务列表 |
| GET/PUT/DELETE | `/assets/{id}/schedule` | 资产周期计划 `{cron, enabled, auto_verify, auto_retest}`（5 字段 cron，按 Hub 本地时区；保存即生效并重算 `next_run_at`，响应带下 3 次预计时间；两开关省略＝true）；Hub 内单 goroutine 每 30s 到点经 `LaunchAssetTask` 发起，研究结束后自动做候选验证与已知漏洞复测，见 `native/ASSETS.md`「周期计划」「计划后续」 |
| GET | `/assets/{id}/schedule/runs?limit=&cursor=`、`/schedules` | 计划运行记录（launched/failed/skipped，按 scheduled_for 倒序分页，每项带 `followups` 摘要）/ 全部计划（资产名、cron、enabled、开关、next_run_at、最近一次 run） |
| GET/PUT/POST | `/assets/{id}/vulns[?status=]`、`/assets/{id}/vulns/{vid}`、`…/vulns/{vid}/members/{finding_id}`、`…/vulns/{vid}/merge` | 资产漏洞库：各轮候选按端点键（`METHOD origin/路径`，ID 段归一为 `{id}`）归并成条目，状态 new/present/pending/unconfirmed/fixed 由验证/复测结果读时派生；标题覆盖、移动/拆分、合并为人工归并（同步不覆盖）；计划后续复测按条目去重；见 `native/ASSETS.md`「资产漏洞库」 |
| GET | `/schedule-runs/{run_id}/followups` | 计划运行的后续明细：kind（verify/retest）、候选编号/标题、task_id、target_id、session_id、status、error、已有的 verdict/conclusion |
| GET | `/usage?task_id=&from=&to=` | 只读 token 用量汇总（按日/模型，全局按任务、任务内按会话角色）；解 `chat_aop_events` 中 `Event_Usage`，不含费用，见 `native/USAGE.md` |
| GET | `/task-list?q=&status=&asset_id=&limit=&cursor=` | 全局任务列表（标题、origins、资产、是否计划发起、状态 running/idle/not_started/unknown、最近活动、候选/验证/supported/报告/复测数、total_tokens、策略；counts 不受 status 过滤），见 `native/OVERVIEW.md` |
| GET | `/vulns?status=&asset_id=&q=&limit=&cursor=` | 全局漏洞：未归档资产的 C3 条目 + 无资产任务候选（`finding:<id>`，同一状态派生），C3 排序，counts 不受 status 过滤 |
| GET | `/report-list?q=&asset_id=&limit=&cursor=` | 全部任务的报告（任务标题、资产、草稿标题、条目数），created_at 倒序 |
| GET | `/overview?days=7\|30\|90` | 运营总览：资产/任务/漏洞/token 汇总与按本地日补 0 的趋势、即将运行的计划与最近计划运行、最近 supported |
| GET/PUT/POST | `/notify/wecom`、`/notify/wecom/test` | 企业微信群机器人配置（webhook 加密存储、只回 host+末 4 位、`""` 保留 `"-"` 清除；enabled、require_confirm、base_url）与测试推送 |
| GET/POST | `/notifications?status=&limit=&cursor=`、`/notifications/{id}/confirm\|dismiss\|retry` | 推送记录：验证 supported 后按 C3 条目（无资产按候选）去重登记（已修复后再确认为 regression），Hub 内 goroutine 每 10s 发 markdown+证据图片，重试 1/5 分钟、3 次失败、45009 推迟、全局 18 条/分钟；见 `native/NOTIFY.md` |

工具：`webapi_http`（scope=origins 时只发授权 origins，unrestricted 时任意 http/https；均保存完整 Flow）、`webapi_finding`（保存/更新候选，引用同任务证据）、`webapi_verification_result`（验证会话写判读，引用本会话新证据）、`webapi_report_draft`（报告会话写不可变草稿）、`webapi_retest_result`（复测会话写不可变结论，引用本会话新证据）。验证细节见 `native/VERIFICATION.md`，报告见 `native/REPORT.md`，复测见 `native/RETEST.md`。

## 4. 关键约束与已知缺口

- 授权：`webapi_http` 按任务测试策略强制 origin（scope=unrestricted 时不限制、仍完整记录）。
- 出口（B2）：模型节点装 `Capability.EgressExtension()`（复用上游 `tools/proxy.NewProxyHub`，CA 在 `state-dir/egress-ca/`），终端（bash/tmux）每个进程注入带令牌的代理与 CA 变量；代理在连目标前按会话所属任务策略放行/拒绝（origins 时越界 407，unrestricted 只记录，普通对话放行记录），放行与拒绝都写 `webapi_egress_flows`。是代理强制，可被刻意绕过（unset 代理变量、`--noproxy`、nc/原始 socket、DNS、已建隧道内不复核），不是 netns/iptables 隔离；浏览器工具未接入。见 `native/POLICY.md`。
- 审批：任务测试策略 approval（默认 high_risk）经原生 `tool.before` 挂起高危 `webapi_http` / 终端命令（all 时还有全部非只读 HTTP、终端与 `write`），以 `guardrail.Review` 事件出卡片，人经 Hub guardrail 命名空间 Pending/Resolve 批准；拒绝/超时（10 分钟）不执行，HTTP 记 `review_denied/review_expired` 未发送。终端规则是静态表，见 `native/POLICY.md`。
- unknown：同任务相同请求指纹的发送未知状态阻止自动重发。
- 旧库升级：新 Hub 打开已知旧结构库（四/六/八/十/十二/十五/十七/二十/二十二表阶段）时事务性补齐到完整结构（含测试策略、资产、周期计划、计划后续、漏洞库、出口记录表）；其他结构报错。没有通用迁移框架。
- 未做：出口流量被候选/验证引用、浏览器工具出口、模型报告、复测、用量/原文记录、资料库接入、图谱/时间线。

## 5. 扩展原则

先查上游是否已有（固定源码在 `var/` 的 prepare 工作区或模块缓存），再找扩展点（Web Capability、工具注册、SchemaModule、UI 覆盖/插件组合），最后才是补丁。前端优先复用原生组件（TrafficPanel、HttpViewPanels、JEVControlFlow、Markdown 等），只覆盖必须改的文件。
