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

1. **F1 修复（进行中）**：后端结果工具错误文案逐字段说清（Kimi，`var/f1-tool-errors`）；前端报告列表标题刷新 + 复测区块跨断点闪现空态（GLM，`var/f1-ui-fixes`）。
2. **用量**：每任务/每日 tokens（先核 Cyber 原生 usage 记录与展示）。
3. 之后按 [roadmap](roadmap.md)：B1 审批拦截 → C1 资产 → C2/C3 周期任务与漏洞库 → D1/D2 证据图片与企业微信 → B2 全工具出口 → D3 SDL。

## 验收方式

主会话在演示环境（`python3 scripts/demo-stack.py up`，本地模拟目标 + 模拟模型 + 临时 Hub 42460，纯测试数据）用 `var/browser-tools/b.sh`（agent-browser + chromium）亲自走用户链。真实模型/靶场运行由执行会话在用户授权下进行，主会话只读结构化结果。

## 执行会话

- `wscyber-f1-tool-errors`（`wangsu-kimi-latest`）：结果工具错误文案。
- `wscyber-f1-ui-fixes`（`wangsu-glm-latest`）：报告列表标题、复测区块加载态。
