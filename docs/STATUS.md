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

## 下一步（按顺序）

1. **执行复盘（R2，进行中，GLM）**：执行时间线视图（左筛选 | 时间线 | 详情，call_id 配对，跳转流量/候选）+ 侧栏徽标语言修复（`var/r2-timeline/task.md`）。
2. **真实验证链（R1，挂起）**：需用户授权真实模型/靶场，跑通 建任务→候选→验证→报告→复测。
3. 首版验收：靶场完整链跑通（product.md §6）。

**R4 复测已完成（6c4de0c 后端 + 71d297c 前端）**：候选右栏「复测」区块、复测会话条、侧栏徽标；演示环境 fix off→仍可复现、fix on→本次范围内已修复。已知小问题：窗口跨断点时复测区块重挂载，加载完成前短暂显示「尚无复测」而非加载态。

**UI 改版 A 已提交（19c9f4b）**：任务局部导航（执行/流量/候选/报告 + 计数）、候选三栏（≥1240：列表 300 | 正文 46rem | 右栏 380 证据+验证）、流量筛选/Host+路径分列/可拖动分割、等宽字体 Fira Code 与 11/12/13/14/16 字号刻度、报告阅读列与目录断点、侧栏会话业务标题+类型图标。

## 验收方式

主会话在演示环境（`python3 scripts/demo-stack.py up`，本地模拟目标 + 模拟模型 + 临时 Hub 42460，纯测试数据）用 `var/browser-tools/b.sh`（agent-browser + chromium）亲自走用户链。11-16 实测：建任务→流量→候选→独立验证→报告（草稿/阅读/导出 MD·JSON）全链通过。

## 执行会话

- `wscyber-r2-timeline`（`wangsu-glm-latest`）：R2 执行时间线。

