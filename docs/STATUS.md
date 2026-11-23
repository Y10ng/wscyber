# 当前状态

只记录现在的状态和下一步；每完成一片改写本文件，不追加历史。历史记录（0.1–0.97）在 tag `legacy-snapshot` 的 `docs/STATUS.md`。

## 当前（2026-11-20）

**首版完整链已用真实模型跑通（R1，11-17 用户授权）：** 产品模型 + 本地靶场 `http://192.168.157.1:8000`，建任务 → 自主探索（18 HTTP、4 候选）→ 3 个候选独立验证均形成判读 → 报告（4 候选 + 3 验证，草稿写入，MD/JSON 导出 200）→ 复测得出结论。各步浏览器可操作。结构化结果见 `var/r1-real/result.md`；截图/导出物在 `var/r1-real/`（主会话不读）。预览库升级前备份 `var/preview/state.bak-20261117`。

R1 发现的问题（均可自恢复，非阻断）：
- `webapi_report_draft`、`webapi_retest_result` 各被拒 1 次，错误文案合并多条规则、不指明字段 → 模型靠重试修正。
- 报告列表在草稿写入后仍显示“未命名报告”，重进才恢复（`refreshDetailQuiet` 只合并 `has_draft`）。

**长期方向（11-17 用户提出）：** 企业自动化渗透 Harness（资产登记、周期任务、审批拦截、全工具出口、企业微信/SDL 推送、审计）。分阶段计划见 [roadmap](roadmap.md)。

**预览：** 运行中 http://127.0.0.1:42453/（11-22 01:10 Hub 从 bfb861a 构建，含 M5 全部与证据口令遮盖；UI 为 01:00 构建〔与 HEAD 前端一致〕；升级前备份 `var/preview/state.bak-20261122a`，旧二进制 `var/wscyber-hub.prev`）。重开方式：

```bash
cd /root/桌面/wscyber
env -i PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 var/wscyber-hub --local-hub --model-node \
  --listen 127.0.0.1:42453 --access-key-file var/preview/access.key \
  --state-dir var/preview/state --ui-dir var/ui-work/release/public \
  --database-url-file /root/.config/wscyber/pg-dev.url
```

前端增量构建工作区：`var/ui-work`（见 `native/RUNNING.md`）。

**已知问题：**
- 原生终端等工具无强制出口；只有 `webapi_http` 强制 origin（roadmap B2）。
- 历史记录的事件持久化偶发 5s 写锁超时（原测试名已不存在）：09d559f 后整包 `-count=3` 前后均 0 失败，未复现；IMMEDIATE 不保证外部写锁超 5s 时不 BUSY。
- `scripts/cyber-ui-rebuild-check.py` 自检在清理前就失败（release 目录校验），未修；不影响实际 rebuild。
- 时间线“新记录浮出”按钮未在实时追加场景观察过；侧栏徽标为前端反查（每会话最多 4 次 by-session 请求，有缓存）。

## 进度（2026-11-20）

已提交：此前 U2a/U2b/U2c、D1、D2 后端等（见 git log）；本日 D2 前端集成页（c4ee2e4）、B3 审计后端（15cc413）、U2d 漏洞页+报告页（08f8e60）、V1 处置后端（4005fb2，契约 `native/TRIAGE.md`）。均经主会话代码+定向测试验收，前端在演示环境走过用户链。

路线（11-20 用户确认）：新增 **M 挖掘能力** 排在 V2 前——上游可复用循环/Goal 评估器/子 Agent，共享黑板（地图/事实/Oracle）须自建；场景内容自写（[scenarios](scenarios.md)）。M0 真实对比用户稍后自行建任务做。不抓取、不照抄线上 Cyber 平台的模板与提示词。

已提交（续）：V1 前端（548c57e）、M2 黑板后端（d741d40，`native/BOARD.md`）、M1 场景后端（46023e3，`native/SCENARIO.md`）、M4 黑板界面（c901021）；设计需求说明 `docs/design-brief.md`（用户找设计师出总览/漏洞卡高保真稿）；路线新增 K 漏洞卡、D3 改为 SDL 申请单入站 + 工单出站。

已提交（续 2）：M3 持续探索（3a3a260：研究会话走 Goal 评估循环，评估看黑板摘要；默认 8/12 轮；本机演示模型节点不开）、M1 场景前端（6ef1da8）；设计稿 `docs/设计稿.html` 与实现依据 `docs/ui-design.md`（含未覆盖页面统一规则，产品名 WScyber）。

**真实对比时机：** M3 后研究会话为“场景方法 + 黑板 + 持续探索”完整形态，用户可自建任务对比（M0）。已知：评估调用 token 不计入平台用量（上游行为）；评估器遵守提示词约束未经真实模型验证。

已提交（续 3）：OV2 总览后端（b85b1bf）、UI-A 设计基础（8d4fa64：Token、共享徽标、侧栏与漏洞列表按稿、品牌 WScyber）。

已提交（续 4）：K1 漏洞卡后端（9aa24b8，`native/VULN_CARD.md`）、UI-B 总览前端 + 总览 `recent_supported.suggested_severity`（b2703a4）、演示脚本分页用例带正常对照（30985e4）。UI-B 验收记下：总览“待定级 2”跳到漏洞页后列表含已修复/未支持共 4 条（筛选只按等级，未限未修复）；390 宽下 A 区四卡单列偏长。

已提交（续 5）：K1 漏洞卡前端（ffea214，经一轮 7 项纠正后验收：JSON 美化与折叠、紧凑 AI 徽标、窄屏对齐等）。验收记下：同资产其他漏洞不带 AI 建议等级（后端 related 无该字段）；总览资产覆盖“0 天未测”应显示“今天”。

已提交（续 6）：UI-C ①（163e1f5，经一轮纠正：超长单行正文折叠截断、流量详情内边距）——`lib/time.ts` 全站去 AM/PM、任务列表/计划队列按页面骨架、流量详情与候选证据引用改用 HttpPairBlock。预览 11-20 13:29 已重启加载该前端。

已提交（续 7）：后端小修 2d0afe0（漏洞卡 related 带 AI 建议、days_since 0=今天）；UI-C ②（e0ce0f2，经一轮纠正：补回漏洞人工归并入口到漏洞卡、总览“今天测过”、同资产 AI 徽标）。11-20 晚网关曾返回 407 中断执行会话，恢复后同会话续跑完成。

已提交（续 8）：UI-C ③（32c1e7a）：执行时间线撑满、HTTP 详情直接显示请求/响应、全部弹层统一。**UI-C 全站设计语言三批完成**，预览 11-21 03:00 已加载。验收记下：执行视图会显示其它任务的待审批行（待审批未按任务隔离）；处置 Sheet 日期输入为浏览器原生 mm/dd/yyyy。

已提交（续 9）：K2 一键重放后端（b43ce63，`native/REPLAY.md`：人触发、授权现读、越界/危险请求不发送、存证 source=replay、确定性对比、10s 限频、审计 vuln.replay）与前端（a6ded39）。K2 剩余：报告/推送从漏洞卡生成。

已提交（续 10）：K2 统一出口后端（65070d7，契约 `native/NOTIFY.md`、`native/VULN_CARD.md`）：推送标题/链接以漏洞卡为准；卡上人工推送 kind=manual（直接入队、终态后复用同行再推、409 not_configured/false_positive/no_supported/in_flight）与逐字预览；卡片 notify 段；按漏洞卡确定性导出单条/多条报告（MD/JSON `wscyber.vuln_report.v1`，遮盖版）。记下：manual 推过后该键不再自动登记 new；manual 与 new 并发登记可能多推一条（未加锁）。

已提交（续 11）：K2 统一出口前端（e4088c0，经一轮纠正：轮询刷新卡片会覆盖处置表单未保存输入 → 表单只在切换条目或服务端 updated_at 变化时回填）：漏洞卡“推送到企业微信”对话框（逐字预览+证据图、四种提示、409 文案）、“更多”菜单所有卡片可用（推送/导出 MD/JSON）、右栏推送卡（最近 5 条、确认/重试/忽略、未完成时 10s 刷新）；漏洞列表多选导出。主会话演示环境走过：多选导出按显示顺序、未配置只预览、待确认时禁推、忽略后人工推送入队、未保存输入经轮询保留、390/深色。**K2 完成。** 验收记下：@cyber/ui Checkbox 无半选态（部分选中显示未选）；>50 条上限未在演示触发。

已提交（续 12）：M5-0 安全小修（2a6e53a）：调研发现正式模型节点未拦上游 `subagent` 工具，子会话映射不到任务、其终端请求被出口代理按普通对话放行（不受 origins 限制）；现两种节点均拒绝，预览已重启加载。

已提交（续 13）：M5a 子 Agent 认领 Oracle 并行探索后端（30ada10，契约 `native/SUBAGENT.md`，经一轮纠正：会话轮换 /clear、/compact、恢复产生的续接会话曾被 FailClosed 拦截）：只放行 `oracle_explorer`（研究会话、async/sync、O-NNN、每任务≤3），预占→落表→释放，子会话全线归属任务，工具白名单无终端，黑板认领排他，子会话审批经父会话处理，用量计入任务，`GET /tasks/{id}/subsessions`。已知限制：子会话转录 Hub 不持久化（上游按 turn 找会话）；周期任务“上一次仍在运行”与后续自动验证就绪判定未计入子会话。`TestProductModelNativeEntryAndReopen` 基线即偶发失败（约 1/10）。

已提交（续 14）：M5a 补漏（201bae2：子 Agent 每会话≤24 次模型请求，经 ModelRequestPolicy 强制——上游 Continue Run 会覆盖 cfg.MaxTurns；计划任务“仍在运行”与后续自动验证/复测就绪计入 running 子会话）、演示剧本 `subagent-demo [--slow N]`（d1afae7）、M5a 前端（88fe67d，经一轮纠正：认领会话改显示完整 id）：黑板子 Agent 区（运行中 5s 自动刷新）、流量/候选详情/审批卡来源标签、用量“子 Agent”角色。主会话演示环境走过：慢速剧本下探索中→自动转已结束、认领会话完整 id 与说明、流量 O-NNN 标签与端点对应、任务用量按会话 1 研究 + 3 子 Agent、390 宽。**M5a 完成。** 未验：待审批卡子 Agent 标签（演示 approval=off）；候选列表不标来源（摘要接口无来源会话字段）；子会话转录 Hub 不保存（上游限制）。

已提交（续 15）：M5b 后端（d0c91d9，契约 `native/AUTHZ.md`；o-latest 执行会话读码时被安全分类器拦截 5 次〔cyber〕，用户决定改派 kimi 完成）：账号占位符 `{{cred:<id>.username|secret}}`（发送前替换、工具结果回替 secret；首条消息与计划验证/复测只给凭据 ID + 占位符，**测试账号明文进入会话事件的缺口已关**）；`webapi_authz_compare`（已发送证据换身份/匿名 1–4 变体、同一策略与存证、需审批不发送、hint 只作线索）；`GET /tasks/{id}/authz-compares`；补 M5a 旧库阶段 {41,37}。行为变化：关联资产的口令密钥读不出时该任务 `webapi_http` 全部不发送（无法替换/回替）。

已提交：M5b 演示剧本 `authz-demo`（71fb97d）、M5b 前端（20264a4，经一轮纠正：账号 ID 挤占用户名）：资产凭据 ID 与复制占位符、手动验证/复测账号占位符块（与后端逐字一致，6 组夹具核对）、流量身份对比小节与变体反查、时间线摘要。主会话演示环境走过：资产账号卡、流量“身份对比”标签、原请求小节（匿名被拒绝 401 / 用户 B 与原请求相同 200）、变体反查。**验收发现**：流量视图“遮盖敏感值”只遮敏感头，登录请求正文口令仍明文显示（既有缺口，M5b 后更显眼）→ 已修：后端 70af17f（证据详情/列表 `mask=1` 口令换占位符、省略原始 protobuf、图片默认遮盖口令）+ 前端 22a02cd（各证据视图开关接后端遮盖，显示“已遮盖口令 N 处”）；演示环境核对登录正文显示占位符、开关开时整页无口令明文。未验：hint 其余三态与 replay 标签（演示无数据）；构建脚本 Node 堆 768MB 在 Vite 阶段 OOM（执行者临时调大后还原，需另处理）。

已提交（续 16）：M5c 后端（b8afb6a，契约 `native/BROWSER.md`）：本项目注册 `playwright`/`katana`（复用上游实现），每会话独立无头 Chromium + 本机中继按调用注入出口令牌，流量经 B2 按任务策略放行/拒绝并记 `tool=browser|katana`；调用间后台请求不发送；绕过参数（--proxy-server/--cdp/--headed/attach、katana -proxy/-chrome-ws-url）拒绝；approval=all 挂起、high_risk 新规则；不运行时下载浏览器。依赖新增 107 模块（许可：MPL-2.0 3、UNKNOWN 1〔chainreactors/neutron〕），经 goproxy.cn 镜像获取、sum.golang.org 校验保留；Hub 二进制 69.8MB→124MB。已知限制：DNS/WebRTC/QUIC 只靠启动参数关闭未逐项验证、WebSocket 内层帧不记录、每会话一个 Chromium 无上限。

已提交 M5c 演示剧本 `browser-demo`（3c9ae4a）与 M5c 前端（aa13303：流量“工具出口”页签、终端/浏览器/爬虫标签与筛选、详情工具名与拒绝原文、出口说明文案）。**M5 完成**（子 Agent、多账号对比、浏览器/爬虫）。验收记下两处已修：浏览器后台降噪（1d2d9aa：netlog 定位为搜索引擎预连接，关 PreconnectToSearch 后 4→1；剩余 android.clients.google.com/checkin〔疑似 GCM〕1 条始终被拒，回归测试固定此上限）、工具出口 CONNECT 原文按 authority 形式显示（bfb861a）。构建脚本内存已修（默认 1536MB、`WSCYBER_UI_HEAP_MB` 可调、自动恢复 static/.gitkeep）。

已提交：构建脚本内存（aa994de）。V2（AI 建议等级）已在 K1 完成。

已提交（续 17）：V3 后端（5596eb8）：`POST /vulns/{key}/card/retest` 在代表候选（repro 来源→最近 supported→最近成员）原任务下按当前修订新建复测并由后端启动（与计划后续共用 `startPinnedSession`），409 archived/false_positive/no_member/in_flight（任何来源未出结论且运行中）/no_model，审计 `vuln.retest_request`（申请记录即审计，不新表），卡片 `retest_request` 段与 lifecycle。

已提交：V3 前端（b040e66）：复测卡按钮改为真实“申请复测”、更多菜单入口、进行中行与打开会话、10s 自动刷新至结论、五种 409 卡内提示。执行会话演示环境实测：fix on 后申请→进行中→自动转“本次范围内已修复”，lifecycle“申请复测→复测→已修复”；in_flight、误报提示实测。**V3 完成。** 未实测：archived/no_member/no_model 文案（代码核对）。

已提交（续 18）：V4 运营通知后端（c0b90bf，`native/NOTIFY.md`「运营通知」）：approval_pending（挂起即推、口令占位符+通用遮盖、链接研究会话、已决未发出的 dismiss）、schedule_failed、weekly_digest（周一 09:00，总览 days=7 同口径）；三开关默认关、PUT 省略即保留、不受 require_confirm；补 M5b 旧库阶段 {42,39}（此前漏登记，新增表会使 M5b 库无法升级）。

已提交：V4 前端（74aff07）：集成页“运营通知”三开关与说明、推送记录类别与筛选、待审批记录打开会话。执行会话演示环境实测：开关落库、审批演示产生“待审批”记录、打开会话跳到对应研究会话、类别筛选、深色/390。**V 阶段完成。** 未实测：计划失败与每周摘要（演示难触发，代码核对）。

**验收发现**：审批挂起时登记 approval_pending 偶发 `SQLITE_BUSY`，该条提醒静默丢失 → 已修（b3bb495）：根因是 webapi deferred 事务先读后写，在 Hub 原生连接写入时升级写锁立即 BUSY（busy_timeout 不生效）；登记先取写锁、入进程内队列重试至成功或审批已决（重启会丢未落库的登记，已写 NOTIFY.md）。

**webapi 事务改 IMMEDIATE** 已完成（09d559f）：盘点 27 个事务入口，修复前仅通知去重登记、手动推送入队两处先读后写会立即 BUSY（两连接测试复现），DSN 加 `_txlock=immediate` 后等待成功；无显式只读事务。

**11-22 用户定方向**：全部存储迁 PostgreSQL 并删除 SQLite（全新开始、不导旧数据），共享库 + tenant_id + RLS，两级角色（管理员/普通用户），外部连接串（开发用本机 PG18）。P0 设计已完成（4cc8684，`native/POSTGRES.md`：原生存储用 overlay 整文件替换为 PG 版、bun/migrate 基线、tenant_id+RLS、P1–P7 拆片）；用户确认：编号/推送/SLA 按租户、模型全局；用户单租户、管理员可切换；口令登录、密钥只给节点。本机 PG18 已启用并建 `wscyber_dev` 角色与演示/预览库。**部署与更新**设计已完成（a8240b2，`native/DEPLOY.md`）：单静态二进制（前端 embed，`serve --database-url-file` 即可运行）与 Docker 镜像同源，一条命令升级；决定：GHCR 公开（首发前须完成许可核对）、反向代理终止 TLS、默认 8080、不做自动更新、允许 `.github/workflows/`。

**P1 PostgreSQL 基础设施已完成（52ccdf9）**：`native/pgschema`（连接串解析不泄露口令、拒绝超级用户/BYPASSRLS、单实例锁、迁移运行器、启动序列）、`0001_baseline.tx.up.sql`（54 表、tenant_id 默认值/继承触发器、RLS 策略已建未启用、默认租户）、`native/internal/testdb`。（P1 首个会话因额度中断、续跑被安全分类器拦截，按用户选择新会话限定读码范围后重做完成。）记下：`go mod tidy` 因网络未跑完，go.mod 手工整理，网络通时补跑确认；P3 合并 cmd 层 `loadDatabaseURL` 与 `pgschema.ResolveDatabaseURL` 为一份。

**P2 已完成（3d2d7a2，分支 pg-migration）**：overlay 改为清单驱动整文件替换（上游源/派生副本哈希校验）；上游原生存储三文件 PG 版 + `store_pg_identity`（每事务 set_config 身份，无身份＝system+default）；事件/制品游标 advisory lock；排序 `COLLATE "C"` 等价 SQLite 字节序；契约测试 32 个。记下（P5 必须处理）：上游 `events.go` 直接用 `s.store.orm` 查游标、后台一律 `context.Background()`，RLS 启用后与非默认租户会话冲突。

**P3a 已完成（1b0aa7e，分支 pg-migration）**：webapi 存储迁 PG——read/write 助手（每事务身份 GUC、写事务全局 advisory lock 复现单写者）、SQLite 方言全部改写、99 处 `COLLATE "C"`、删阶段机制与 19 个升级测试、busy 测试改写为 PG 锁语义、新增并发测试；`./webapi` 全过（主会话复跑 90s 通过）。行为变化：读不再排在写后面；等锁上限 10s；父行不存在的子行插入被触发器拒绝（验证/复测/报告 PUT 已映射回 404）；Oracle 事件 ID 改为可排序格式代替 rowid。

**P4 已完成（5cc49f0）**：SQLite 残留清理（Hub 闭包已无 sqlite/libc，`go mod tidy` 补跑；许可清单删 8 个不可达模块，新增 `scripts/cyber-dependency-check.py` 核对 214 模块完全相等），文档改为 PG 现状。**迁 PG（P1–P4）完成。**

**平台功能与打磨阶段（进行中，并行）**：**F1 已修（1c40681）**：Profile 的 Load/Close 串行，未加载的 Profile 不计活跃资源，端到端测试 ×20 稳定；**F2 已完成（6ff2ce1）**：工具参数错误逐条指明字段路径（规则不变）、推送并发去重测试固化、审批取消状态统一为 `canceled`；**R2 已完成（d5ae782）**：迁移 0002，审批记录落库（重启遗留 pending→cancelled 并审计）、运营通知登记改表驱动（重启后继续）、按任务/会话过滤的审批记录接口；“执行视图混入其它任务待审批”确认在前端（App.tsx 把全部在线会话的 pending 传给当前任务），待 U2e 后修；**U2e 已完成（f0f4dc1）**：390 顶栏、Oracle 编号字体、用量按角色排序、待定级跳转、审计“全部动作”、报告标题静默刷新、归档计数即时、工作区高亮“任务”；验证时间线复核已撑满无需改。

**P3b 已完成（03181d8），pg-migration 已合回 main（d993f54）**：Hub 两种启动形式都经 `pgschema.Start`，无连接串即失败，单实例锁看门狗；演示环境每次 up 重建 `wscyber_demo`，五个剧本在 PG 上全部通过；主会话演示环境复核（selftest --chain、approval-demo 后总览/漏洞/审计/任务执行视图正常，数据均 tenant=default）。**预览已切到 PG**（`wscyber_preview` 库，全新开始；旧 SQLite 数据随 state 备份在 `var/preview/state.bak-sqlite-*`，旧二进制 `var/wscyber-hub.prev-sqlite`）。

**D1 单二进制运行形态已完成（4da5710）**：`wscyber-hub serve`（前端已嵌入、默认 127.0.0.1:8080、自动生成 access.key、连接串文件只解析不连接）/`version`/`healthcheck`，旧旗标形式不变，可出静态二进制（strip 后约 103MB）；部署线到此暂停（用户定）。D1 记下：正式模型节点在 ready 后、节点 Profile 未加载完就收到停止信号时以 1 退出（既有问题，未修）；P3 接 PG 时连接串文件读取/权限只保留 cmd 层 `loadDatabaseURL` 一份，未配置连接串须启动失败，`buildinfo.SchemaVersion` 接迁移最大版本。

**B3 审计页前端**已完成（deb1603）：配置组“审计”，操作者/动作类别/资产/日期/关键词筛选、分页、展开 detail 与打开关联任务/资产。预览已升级到 deb1603（state 已备份）。

U2b 验收时记下的缺口：原侧栏的会话重命名/删除、非 Web/API 普通对话会话入口未迁移；工作区顶栏未放 token 合计与待审批；侧栏任务计数在归档后要等切页/60s 才更新；验证会话时间线下方空白未撑满；进入任务会话后侧栏仍高亮原页面。

## 下一步

1. **（11-22 用户定顺序）先把平台做好，部署更新与登录放后**：P1 ∥ D1（进行中，做完即停部署线）→ P2 → P3（Hub 接 PG，演示全链）→ P4 删 SQLite → **平台功能与打磨**（U2e 走查与小打磨、U2b 遗留缺口、审批状态/运营通知队列持久化到 PG、R1 遗留问题、真实模型验证新能力〔需用户授权〕、E 阶段按需）→ 之后再做 D2–D4 部署更新与 P5–P7 租户/RLS/登录/管理界面。D3 SDL 需接口文档。
3. 小打磨（U2e 已做大部分）：scope=unrestricted 时首条消息仍写“仅限以下 origin”（前后端逐字一致，待 F3）；执行视图混入其它任务待审批（前端，待 U2f 用审批记录接口）。资产列表逾期计数、推送记录胶囊计数缺接口数据（UI-C ② 省略）。
4. D3 SDL（需接口文档）。

已知缺口：B1 审批状态只在节点内存（重启即取消挂起项）；（资产测试账号进入会话首条消息事件的问题已由 M5b 占位符关闭，历史会话不追溯清理）B2 出口为代理强制（可被刻意绕过），浏览器/爬虫已接入（M5c），新增 GPL-3.0、MPL-2.0 与许可不明依赖未做法律评估；终端规则为静态启发式；C2b 后续只在单测与替身中覆盖；无人值守的后续遇高危操作仍走审批，10 分钟超时拒绝（V4 推送待审批缓解）；审计无保留期、无用户名（单 key）。

## 验收方式

主会话在演示环境（`python3 scripts/demo-stack.py up`，本地模拟目标 + 模拟模型 + 临时 Hub 42460，纯测试数据）用 `var/browser-tools/b.sh`（agent-browser + chromium）亲自走用户链。真实模型/靶场运行由执行会话在用户授权下进行，主会话只读结构化结果。

## 执行会话

- `f3-scope`（gpt-6-astra，首条消息按策略写范围，日志 `var/f3-scope-wording/exec.log`）
- `u2f`（wangsu-ds-flash-latest，待审批按任务隔离与顶栏提示，日志 `var/u2f-approvals/exec.log`）

