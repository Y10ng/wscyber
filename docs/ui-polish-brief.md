# 界面打磨交接说明（给外部模型）

本文给参与界面打磨的模型看。**只改前端，不碰后端。** 打磨完的改动由本项目主会话审阅、在演示环境里验收后合入，所以请按第 5 节的方式交回。

## 1. 项目是什么

WScyber：公司内部、明确授权的 Web/API 安全测试平台。用户登记资产与授权范围，Agent 自主探索，平台保存真实 HTTP 证据，形成候选漏洞 → 独立验证 → 报告 → 复测，并有漏洞卡、处置、推送、审计。产品需求见 `docs/product.md`，界面规范见 `docs/ui.md`、`docs/ui-design.md`，设计稿 `docs/设计稿.html`（浏览器直接打开，用户已确认满意；实现以它为准）。

前端基于上游 Cyber 原生前端（React 18 + Radix + Tailwind + Vite），本项目以**覆盖文件**方式改它，不 fork 整个前端。

## 2. 能改哪里

- **只改** `native/ui/source-overlay/src/**`（覆盖到上游前端的源码）和 `native/ui/source-overlay.json`（覆盖清单，记录每个覆盖文件的 sha256）。
- 新增文件也放在 `native/ui/source-overlay/src/` 下，并在 `source-overlay.json` 登记。
- 文案改动中英两套成对改：`src/i18n/locales/zh/*.ts`、`src/i18n/locales/en/*.ts`。

## 3. 不能碰

- `native/webapi/**`、`native/cmd/**`、`native/pgschema/**`、`native/patches/**` 及一切 Go 代码、数据库、接口。
- 接口调用约定：`src/api.ts` 里的请求路径、参数、返回类型**不能改语义**（可以重构调用方式，但请求与响应必须与现在一致）。
- 不加新的 npm 依赖（构建是固定依赖锁）。
- 不删任何功能入口；不用假数据、占位页冒充没有的能力（产品原则：未实现就不显示入口）。
- 下列产品约束在界面上要守住：候选 ≠ 已确认（模型判读要标明“模型判读/AI 建议”）；截断、未发送、发送结果未知等状态要如实显示；测试策略（范围/审批）要如实展示；敏感值遮盖开关要保留。
- `scripts/`、`docs/`（除你写的交回说明）、`AGENTS.md`、`CLAUDE.md` 不改。

## 4. 怎么跑起来看

需要 Go 1.26、Node v24、本机 PostgreSQL（见 `native/RUNNING.md` §0）。

```bash
# 准备前端工作区（首次，需联网）
python3 -I scripts/cyber-ui-build.py prepare --workspace "$PWD/var/ui-work"
# 每次改完覆盖文件后构建（会做 tsc 与 Vite）
python3 -I scripts/cyber-ui-build.py rebuild --workspace "$PWD/var/ui-work"
# 构建 Hub（只需一次）
python3 -I scripts/cyber-go-overlay.py -- build -p 1 -o "$PWD/var/wscyber-hub" ./cmd/wscyber-hub
# 演示环境：本地模拟目标 + 模拟模型 + 临时 Hub，全部测试数据
python3 scripts/demo-stack.py up
python3 scripts/demo-stack.py selftest --chain     # 生成一条完整链的数据
python3 scripts/demo-stack.py authz-demo           # 多账号对比数据
python3 scripts/demo-stack.py approval-demo        # 审批数据
# 浏览器打开 http://127.0.0.1:42460/ ，登录 key 在 var/demo/access.key
python3 scripts/demo-stack.py down
```

`rebuild` 会校验 `source-overlay.json` 的哈希；改了覆盖文件就要更新对应 sha256，否则构建会拒绝。Hub 启动时缓存前端，改完需 `down` 再 `up` 才能看到。

## 5. 交回方式（请照做，主会话按此审阅）

1. **在一个新分支上改**，分支名 `ui-polish/<简短主题>`，从 `main` 拉出。不要直接推 `main`。
2. **按页面或主题分多个小提交**，每个提交只做一件事，提交信息写清“哪个页面、改了什么、为什么”。不要一个提交改全站。
3. 在仓库根新建 `docs/ui-polish-notes.md`，逐条列出：
   - 改动清单：页面 → 改了什么 → 理由（对照 `docs/设计稿.html` 或可用性问题）；
   - 每条对应的截图（放 `docs/ui-polish-shots/`，PNG，浅色与深色、1440 宽与 390 宽各至少一张关键页）；
   - 有意没改、或改不了的地方（例如需要后端字段的，写清需要什么字段，不要自己改后端）；
   - 自查结果：`rebuild`（tsc + Vite）是否通过、演示环境里走过哪些用户链。
4. 不要提交 `var/`、构建产物、node_modules、截图以外的二进制文件。

## 6. 值得打磨的方向（参考，不是限制）

- 对照设计稿检查全站一致性：间距、字号层级、徽标、表格密度、空/加载/错误态。
- 任务工作区（时间线、流量、候选、黑板、用量）信息密度大，可读性与查找效率优先。
- 窄屏（390）与深色主题的细节。
- 交互反馈：操作成功/失败提示、按钮禁用原因提示。
