# 当前状态

只记录现在的状态和下一步；每完成一片改写本文件，不追加历史。历史记录（0.1–0.97）在 tag `legacy-snapshot` 的 `docs/STATUS.md`。

## 当前（2026-11-17）

**首版完整链已用真实模型跑通（R1，11-17 用户授权）：** 产品模型 + 本地靶场 `http://192.168.157.1:8000`，建任务 → 自主探索（18 HTTP、4 候选）→ 3 个候选独立验证均形成判读 → 报告（4 候选 + 3 验证，草稿写入，MD/JSON 导出 200）→ 复测得出结论。各步浏览器可操作。结构化结果见 `var/r1-real/result.md`；截图/导出物在 `var/r1-real/`（主会话不读）。预览库升级前备份 `var/preview/state.bak-20261117`。

R1 发现的问题（均可自恢复，非阻断）：
- `webapi_report_draft`、`webapi_retest_result` 各被拒 1 次，错误文案合并多条规则、不指明字段 → 模型靠重试修正。
- 报告列表在草稿写入后仍显示“未命名报告”，重进才恢复（`refreshDetailQuiet` 只合并 `has_draft`）。

**长期方向（11-17 用户提出）：** 企业自动化渗透 Harness（资产登记、周期任务、审批拦截、全工具出口、企业微信/SDL 推送、审计）。分阶段计划见 [roadmap](roadmap.md)。

**预览：** 运行中 http://127.0.0.1:42453/（11-17 HEAD 构建，含报告/复测/执行时间线）。重开方式：

```bash
cd /root/桌面/wscyber
env -i PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 var/wscyber-hub --local-hub --model-node \
  --listen 127.0.0.1:42453 --access-key-file var/preview/access.key \
  --state-dir var/preview/state --ui-dir var/ui-work/release/public
```

前端增量构建工作区：`var/ui-work`（见 `native/RUNNING.md`）。

**已知问题：**
- 原生终端等工具无强制出口；只有 `webapi_http` 强制 origin（roadmap B2）。
- `TestWebAPIEventPersistence*`（SQLite 写锁 5s 超时）在机器负载高时偶发失败，单独重跑通过。
- `scripts/cyber-ui-rebuild-check.py` 自检在清理前就失败（release 目录校验），未修；不影响实际 rebuild。
- 时间线“新记录浮出”按钮未在实时追加场景观察过；侧栏徽标为前端反查（每会话最多 4 次 by-session 请求，有缓存）。

## 断点（2026-11-17 14:06，按用户设定的 2 小时时限暂停；等用户下发继续指令）

本轮已提交：F1 修复（faca5ff）、U1 用量后端（24a1833）+ 前端（e7d10be）、B1 测试策略/审批后端（79b6eec，`native/POLICY.md`）、C1 资产后端（5025b38，`native/ASSETS.md`）。所有执行会话、演示环境已停止；42453 预览 Hub 保持运行（仍是 11-17 上午的二进制，不含本轮功能）。

工作区未提交改动属于下面两片，**均未验收，续跑完成并验收后再提交**：

1. **B1 前端**（`wangsu-glm-latest`，会话 `aa384b7d-31f5-4115-9c07-d480553d5b83`，`var/b1-policy-frontend`）
   - 已做：建任务策略选项与“不限制”风险提示、顶栏策略指示/Popover、待审批组件（`WebApiPendingApprovals.tsx`）、执行时间线与流量的审批状态、i18n；演示环境截图 7 张（批准/拒绝的执行与流量视图、策略 Popover、不限制提示）。中断前正在修 `WebApiPolicySaver` 保存按钮永不可用的缺陷，并登记 `source-overlay.json` 哈希。
   - 未做/待查：result.md 未写；origins 识别 chip、顶栏「待审批 N」替换“护栏未配置”、侧栏徽标是否完成需续跑后确认；构建结果未知。
   - 改动文件：`native/ui/source-overlay.json`、`native/ui/source-overlay/src/{App.tsx,api.ts,hooks/useChatSession.ts,lib/execution-timeline.ts,i18n/locales/{zh,en}/chat.ts}`、`components/{SessionList,WebApiEvidencePanel,WebApiExecutionTimeline,WebApiTaskBar,WebApiTaskDialog}.tsx`，新增 `components/{WebApiPendingApprovals,WebApiPolicyShared}.tsx`。
   - 续跑：`scripts/claude-exec-loop.py wangsu-glm-latest b1-policy-frontend var/b1-policy-frontend --session aa384b7d-31f5-4115-9c07-d480553d5b83`（需要时先 `python3 scripts/demo-stack.py up --hub <含 B1 的 Hub>`，演示 Hub 须从 HEAD 重建）。
2. **C2a 资产周期调度后端**（`wangsu-o-latest`，会话 `dac585b3-b323-4aeb-bb8a-f0e15b67343d`，`var/c2a-schedule-backend`）
   - 已做：`native/webapi/schedule.go`（调度器）、`schedule_store.go`（两表）、`schedule_http.go`（API），并改 `asset_launch.go`/`asset_test.go`/`capability.go`/`store.go`/`verification_store.go`。中断前网关多次断连。
   - 未做：定向测试（无 `schedule_test.go`）、`native/ASSETS.md` 周期计划段、`docs/architecture.md`、result.md。
   - 续跑：`scripts/claude-exec-loop.py wangsu-o-latest c2a-schedule-backend var/c2a-schedule-backend --session dac585b3-b323-4aeb-bb8a-f0e15b67343d`
3. **已备好未派发**：C1 资产前端任务包 `var/c1-assets-frontend/task.md`（须等 B1 前端提交后派，二者改同一批文件）。

续跑顺序建议：B1 前端与 C2a 后端可同时续跑 → 主验收提交 B1 前端 → 派 C1 前端 → C2a 验收提交 → C2a 计划 UI 与 C2b（候选自动验证/已知漏洞自动复测）→ C3 跨轮去重。

## 下一步（续跑之后）

1. 小打磨（攒一片 GLM）：用量按会话表按角色排序（研究在前）；全局用量 Dialog 高度随内容；scope=unrestricted 时首条消息仍写“仅限以下 origin”（前端拼装与 `webAPIFirstMessage` 需同步改）。
2. 之后按 [roadmap](roadmap.md)：C2b/C3 → D1/D2 证据图片与企业微信 → B2 全工具出口 → D3 SDL。
3. 更新 42453 预览前须从 HEAD 重建 `var/wscyber-hub`（该文件曾被执行会话用在途工作区重建）。

已知缺口：B1 审批状态只在节点内存（重启即取消挂起项）；终端规则为静态启发式，不覆盖 here-doc 与解释器脚本；资产测试账号会出现在会话首条消息事件中。

## 验收方式

主会话在演示环境（`python3 scripts/demo-stack.py up`，本地模拟目标 + 模拟模型 + 临时 Hub 42460，纯测试数据）用 `var/browser-tools/b.sh`（agent-browser + chromium）亲自走用户链。真实模型/靶场运行由执行会话在用户授权下进行，主会话只读结构化结果。

## 执行会话

无运行中的执行会话（14:06 已全部停止，断点见上）。
