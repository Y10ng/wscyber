# 当前状态

只记录现在的状态和下一步；每完成一片改写本文件，不追加历史。历史记录（0.1–0.97）在 tag `legacy-snapshot` 的 `docs/STATUS.md`。

## 当前（2026-11-16）

**仓库已收敛：** 初始提交到 GitHub `Y10ng/wscyber`；旧自研实现（`cmd/ internal/ web/ deploy/`）和 30 余份过程文档已删除，保存在 tag `legacy-snapshot`。现行文档只有 AGENTS + `docs/{product,architecture,ui,roadmap,STATUS}.md` + `native/*.md`。

**已在 native 上实现并验证的用户链：**
建任务（两输入 + 授权 origin）→ 真实模型自主探索 → HTTP 原件可回看 → 候选（编号、修订、证据引用定位、宽屏主从布局、Markdown 草稿导出）→ 发起独立验证（保存/失联核对/原生 Open/Run）。功能状态表见 [product.md §5](product.md#5-功能状态)。

**真实数据：** 一次获准的本地靶场运行（目标 `http://192.168.157.1:8000`，产品模型 `wangsu-ds-flash-latest`）：2 任务 / 11 HTTP / 3 候选 / 0 验证结果。状态库 `var/product-preview.VZ6MR7aB/state`（含两张验证表）。

**预览：** 当前没有运行中的 Hub。重开方式（新二进制已于 11-16 构建）：

```bash
cd /root/桌面/wscyber
env -i PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 var/wscyber-hub --local-hub --model-node \
  --listen 127.0.0.1:42453 --access-key-file var/product-preview.VZ6MR7aB/access.key \
  --state-dir var/product-preview.VZ6MR7aB/state --ui-dir var/findings-layout.ypAY1kni/release/public
```

前端增量构建工作区：`var/native-ui-quality.ZzLkZW2L/ui-work`（见 `native/RUNNING.md`）。

**已知问题：**
- 真实模型独立验证尚未跑过，没有非空判读的页面验收。
- 原生终端等工具无强制出口；只有 `webapi_http` 强制 origin。
- `scripts/cyber-ui-rebuild-check.py` 自检在清理前就失败（release 目录校验），未修；不影响实际 rebuild。

## 下一步（按顺序）

1. **真实验证链**（需用户确认可再次使用上述产品模型与靶场）：对现有 3 个候选各发起一次独立验证 → 得到判读 → 浏览器检查非空判读展示。发现问题由 Kimi/GLM 修。
2. **执行复盘**：按方向/会话阅读执行流（参考 V5/V6 与 ARTEX 会话页），GLM 前端为主。
3. **报告**：基于候选固定修订 + 验证结果生成报告草稿（先核 Cyber 原生报告能力，见 roadmap）。
4. **复测**：复用验证执行路径，增加独立复测结论。
5. 首版验收：靶场完整链跑通（product.md §6）。

## 执行会话

当前无执行会话在运行。
