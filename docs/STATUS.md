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

## 下一步

11-17 已提交：F1 修复（faca5ff）、U1 用量后端（24a1833）+ 前端（e7d10be）、B1 测试策略/审批后端（79b6eec，契约 `native/POLICY.md`）。

1. **B1 前端（进行中，GLM，`var/b1-policy-frontend`）**：建任务策略选项 + origins 识别 chip、顶栏策略指示/修改、「待审批 N」替换原生“护栏未配置”、执行视图置顶审批卡片、流量显示审批拒绝/超时。交回后主在演示环境走：审批演示剧本 → 批准/拒绝 → 改策略为“审批关”再跑。
2. 小打磨（攒一片 GLM）：用量按会话表按角色排序（研究在前）；全局用量 Dialog 高度随内容。
3. 之后按 [roadmap](roadmap.md)：C1 资产 → C2/C3 周期任务与漏洞库 → D1/D2 证据图片与企业微信 → B2 全工具出口 → D3 SDL。

注意：`var/wscyber-hub` 曾被执行会话用含在途改动的工作区重建；更新 42453 预览前须从 HEAD 重建（`native/RUNNING.md`）。B1 审批状态只在节点内存（重启即取消挂起项）；终端规则为静态启发式，不覆盖 here-doc 与解释器脚本。

## 验收方式

主会话在演示环境（`python3 scripts/demo-stack.py up`，本地模拟目标 + 模拟模型 + 临时 Hub 42460，纯测试数据）用 `var/browser-tools/b.sh`（agent-browser + chromium）亲自走用户链。真实模型/靶场运行由执行会话在用户授权下进行，主会话只读结构化结果。

## 执行会话

- `wscyber-b1-policy-frontend`（`wangsu-glm-latest`）：B1 前端。
