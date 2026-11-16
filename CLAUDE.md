# 开发入口

遵循 [AGENTS.md](AGENTS.md)。本项目独立于 Arena，不修改、迁入或依赖其应用代码与数据。

开始时读取 `docs/vision.md`、`docs/architecture.md`、`docs/STATUS.md`。架构已确定全部基于 Cyber Harness，见 `docs/cyber-harness-foundation.md`；新 `native/` 直接复用固定上游协议/扩展，入口见 `native/README.md`、`native/HUB.md`、`native/NODE.md`。`internal/domain` 与 `docs/api.md` 是旧业务实现的事实契约，不是要求新底座沿用旧 Harness。产品目标与已实现能力严格区分。

按需查阅：选型先看 `docs/components.md`，发现编号/报告/SDL 见 `docs/findings-and-reporting.md`；前端遵循 `docs/frontend-design.md` 与 `docs/frontend-redesign-brief.md` 的最新覆盖（原生 Cyber UI 能复用直接复用；旧 React/Ant/Vue 是历史实现，不强制混装；逐功能及展开态对齐）；分阶段范围与多 Agent 任务包见 `docs/roadmap.md`。产品不绑定黑板架构，场景 Profile 不替代授权，审批针对实际目标请求而非每步探索。不要求一次完成全部路线，当前仅文档收束时不自行启动下一阶段。
