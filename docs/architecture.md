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
| PUT/GET | `/tasks/{id}`、`/tasks` | 保存/读取任务（两输入 + origins + 初始 session）；保存不启动，启动走原生 Open/Run |
| GET | `/tasks/{id}/evidence[/{evidence_id}]` | HTTP 证据列表与原件 |
| GET | `/tasks/{id}/findings[/{fid}[/history\|/revisions/{rid}]]` | 候选、修订 |
| PUT/GET | `/tasks/{id}/findings/{fid}/verifications[/{vid}]` | 发起/读取独立验证（客户端稳定 UUID，幂等） |
| GET | `/verifications?session_id=` | 由验证会话反查上下文 |
| PUT/GET | `/tasks/{id}/reports[/{rid}]` | 保存/读取报告（固定候选修订＋同修订验证；客户端稳定 UUID，幂等；保存不启动） |
| GET | `/tasks/{id}/reports/{rid}/export?format=markdown\|json` | 由已存草稿＋固定来源确定性导出，无草稿 409 |
| GET | `/reports?session_id=` | 由报告会话反查上下文 |

工具：`webapi_http`（只发授权 origins，保存完整 Flow）、`webapi_finding`（保存/更新候选，引用同任务证据）、`webapi_verification_result`（验证会话写判读，引用本会话新证据）、`webapi_report_draft`（报告会话写不可变草稿）。验证细节见 `native/VERIFICATION.md`，报告见 `native/REPORT.md`。

## 4. 关键约束与已知缺口

- 授权：`webapi_http` 强制 origin；原生终端等工具**没有**强制出口。
- unknown：同任务相同请求指纹的发送未知状态阻止自动重发。
- 旧库升级：新 Hub 打开已知旧结构库时事务性新增验证两表；其他结构报错。没有通用迁移框架。
- 未做：全工具流量捕获、模型报告、复测、用量/原文记录、资料库接入、图谱/时间线。

## 5. 扩展原则

先查上游是否已有（固定源码在 `var/` 的 prepare 工作区或模块缓存），再找扩展点（Web Capability、工具注册、SchemaModule、UI 覆盖/插件组合），最后才是补丁。前端优先复用原生组件（TrafficPanel、HttpViewPanels、JEVControlFlow、Markdown 等），只覆盖必须改的文件。
