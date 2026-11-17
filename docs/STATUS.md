# 当前状态

只记录现在的状态和下一步；每完成一片改写本文件，不追加历史。历史记录（0.1–0.97）在 tag `legacy-snapshot` 的 `docs/STATUS.md`。

## 当前（2026-11-16）

**仓库已收敛：** 初始提交到 GitHub `Y10ng/wscyber`；旧自研实现（`cmd/ internal/ web/ deploy/`）和 30 余份过程文档已删除，保存在 tag `legacy-snapshot`。现行文档只有 AGENTS + `docs/{product,architecture,ui,roadmap,STATUS}.md` + `native/*.md`。

**已在 native 上实现并验证的用户链：**
建任务（两输入 + 授权 origin）→ 真实模型自主探索 → HTTP 原件可回看 → 候选（编号、修订、证据引用定位、宽屏主从布局、Markdown 草稿导出）→ 发起独立验证（保存/失联核对/原生 Open/Run）。

**R3 报告（已提交并在演示环境浏览器实测通过，a55aebd 修复对话框 5 个缺陷）：** 后端 a89f35f + 539dfdd（固定候选修订可附同修订验证、独立报告会话、`webapi_report_draft` 不可变草稿、确定性 Markdown/JSON 导出、摘要带草稿标题、旧库分阶段补表，见 `native/REPORT.md`），定向测试 6/6；前端：任务顶栏「报告」视图（列表 + 目录 + 限宽正文 + 就近证据 + 下载 MD/JSON + 查看执行）、新建 Dialog（选候选/附验证）、后台启动报告会话、报告会话条「返回报告」；tsc/Vite 通过，未做浏览器验收。新 Hub 首次指向 `var/preview/state` 时会为其补两张报告表。功能状态表见 [product.md §5](product.md#5-功能状态)。

**真实数据：** 一次获准的本地靶场运行（目标 `http://192.168.157.1:8000`，产品模型 `wangsu-ds-flash-latest`）：2 任务 / 11 HTTP / 3 候选 / 0 验证结果。状态库 `var/preview/state`（含两张验证表）。

**预览：** 运行中 http://127.0.0.1:42453/（11-16 含报告功能）。重开方式（`var/wscyber-hub` 于 11-16 含报告路由重建）：

```bash
cd /root/桌面/wscyber
env -i PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 var/wscyber-hub --local-hub --model-node \
  --listen 127.0.0.1:42453 --access-key-file var/preview/access.key \
  --state-dir var/preview/state --ui-dir var/ui-work/release/public
```

前端增量构建工作区：`var/ui-work`（见 `native/RUNNING.md`）。

**已知问题：**
- 真实模型独立验证尚未跑过，没有非空判读的页面验收。
- 原生终端等工具无强制出口；只有 `webapi_http` 强制 origin。
- `TestWebAPIEventPersistence*`（SQLite 写锁 5s 超时）在机器负载高时偶发失败，单独重跑通过。
- `scripts/cyber-ui-rebuild-check.py` 自检在清理前就失败（release 目录校验），未修；不影响实际 rebuild。

## 下一步

11-16~17 一个长会话补齐了首版功能链（报告 R3、复测 R4、UI 改版 A、执行时间线 R2），**全部只在演示环境（模拟模型）验过**。离首版验收（product.md §6）还差：**真实模型在靶场跑完整链**。待用户决定：

- **R1 真实链（进行中，11-17 用户授权，执行会话 Kimi 运行）**：升级预览 Hub（先备份 `var/preview/state.bak-20261117`）→ 产品模型 + 靶场 `http://192.168.157.1:8000` 跑 建任务→验证→报告→复测；`var/r1-real/result.md` 只含结构化问题清单，主会话不接触真实数据。随后按问题清单派修复。
- 之后：配套功能（用量、原文记录、资料库、测试安全、任务内对话、图谱）。

**本会话已提交：** R3 报告（a89f35f、539dfdd、743fc0d、a55aebd）、R4 复测（6c4de0c、71d297c）、UI 改版 A（19c9f4b）、R2 执行时间线（f3bd114）、演示环境（6d23c22）、URI %2F 修复（4eec51c）。

**已知小问题：** 复测区块跨断点重挂载时短暂显示「尚无复测」；时间线"新记录浮出"按钮未在实时追加场景观察过；侧栏徽标为前端反查（每会话最多 4 次 by-session 请求，有缓存）。

## 验收方式

主会话在演示环境（`python3 scripts/demo-stack.py up`，本地模拟目标 + 模拟模型 + 临时 Hub 42460，纯测试数据）用 `var/browser-tools/b.sh`（agent-browser + chromium）亲自走用户链。11-16 实测：建任务→流量→候选→独立验证→报告（草稿/阅读/导出 MD·JSON）全链通过。

## 执行会话

- `wscyber-r1-real`（`wangsu-kimi-latest`）：R1 真实链运行，交回结构化问题清单。

