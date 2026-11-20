# 当前状态

只记录现在的状态和下一步；每完成一片改写本文件，不追加历史。历史记录（0.1–0.97）在 tag `legacy-snapshot` 的 `docs/STATUS.md`。

## 当前（2026-11-20）

**首版完整链已用真实模型跑通（R1，11-17 用户授权）：** 产品模型 + 本地靶场 `http://192.168.157.1:8000`，建任务 → 自主探索（18 HTTP、4 候选）→ 3 个候选独立验证均形成判读 → 报告（4 候选 + 3 验证，草稿写入，MD/JSON 导出 200）→ 复测得出结论。各步浏览器可操作。结构化结果见 `var/r1-real/result.md`；截图/导出物在 `var/r1-real/`（主会话不读）。预览库升级前备份 `var/preview/state.bak-20261117`。

R1 发现的问题（均可自恢复，非阻断）：
- `webapi_report_draft`、`webapi_retest_result` 各被拒 1 次，错误文案合并多条规则、不指明字段 → 模型靠重试修正。
- 报告列表在草稿写入后仍显示“未命名报告”，重进才恢复（`refreshDetailQuiet` 只合并 `has_draft`）。

**长期方向（11-17 用户提出）：** 企业自动化渗透 Harness（资产登记、周期任务、审批拦截、全工具出口、企业微信/SDL 推送、审计）。分阶段计划见 [roadmap](roadmap.md)。

**预览：** 运行中 http://127.0.0.1:42453/（11-19 08:10 从 a9eb363 构建，含 U2a/D2/归档过滤后端与 U2c 前端；升级前备份 `var/preview/state.bak-20261119b`，旧二进制 `var/wscyber-hub.prev`）。重开方式：

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

## 进度（2026-11-20）

已提交：此前 U2a/U2b/U2c、D1、D2 后端等（见 git log）；本日 **D2 前端集成页**（c4ee2e4，演示环境主会话验收：webhook 不回显、待确认→确认发送→排队、跳转验证会话）、**B3 审计后端**（15cc413，契约 `native/AUDIT.md`）。

路线新增 **V 漏洞运营闭环**（[roadmap](roadmap.md)）：V1 处置（严重度/处置/负责人/SLA 逾期）→ V2 模型建议等级 → V3 申请复测 → V4 运营通知（待审批/运行失败/周报推企业微信）。

进行中：**U2d 漏洞页 + 报告页**（ds-flash，`var/u2d-vulns-reports/task.md`，用演示环境）、**V1 处置后端**（o-latest，`var/v1-vuln-triage-backend/task.md`，只做 Go 测试）。

U2b 验收时记下的缺口：原侧栏的会话重命名/删除、非 Web/API 普通对话会话入口未迁移；工作区顶栏未放 token 合计与待审批；侧栏任务计数在归档后要等切页/60s 才更新；验证会话时间线下方空白未撑满；进入任务会话后侧栏仍高亮原页面。

## 下一步

1. U2d 验收 → V1 验收 → V1 前端（漏洞页加等级/处置/负责人/期限列与处置面板、总览逾期数）→ B3 审计页前端（配置下“审计”）。
2. V2 → V3 → V4；U2e 分隔线与按钮全量走查、资产页套规范。
3. 小打磨：用量按会话表按角色排序；scope=unrestricted 时首条消息仍写“仅限以下 origin”；总览 `tasks` 统计是否排除已归档任务待定。
4. D3 SDL（需接口文档）。

已知缺口：B1 审批状态只在节点内存（重启即取消挂起项）；B2 出口为代理强制（可被刻意绕过），浏览器工具未接入，新增 GPL-3.0 与许可不明依赖未做法律评估；终端规则为静态启发式；资产测试账号会出现在会话首条消息事件中；C2b 后续只在单测与替身中覆盖；无人值守的后续遇高危操作仍走审批，10 分钟超时拒绝（V4 推送待审批缓解）；审计无保留期、无用户名（单 key）。

## 验收方式

主会话在演示环境（`python3 scripts/demo-stack.py up`，本地模拟目标 + 模拟模型 + 临时 Hub 42460，纯测试数据）用 `var/browser-tools/b.sh`（agent-browser + chromium）亲自走用户链。真实模型/靶场运行由执行会话在用户授权下进行，主会话只读结构化结果。

## 执行会话

U2d 前端（ds-flash）与 V1 处置后端（o-latest）运行中。
