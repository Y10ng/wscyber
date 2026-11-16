# Cyber 原生装配入口 · H1 / H2 / H3 / H4

**2026-11-15独立验证后端增量：** 固定候选修订、独立验证session、可信新HTTP证据的三类不可变判读及只读API已实现，3专项与新Hub构建通过；前端已构建并通过隔离副本的保存/失联核对/原生Open与Run/无Provider失败展示、重开返回及桌面/窄屏检查；当前用户库已在明确批准和一次恢复备份后追加两表、原业务行保全，新42453页面已展示；尚未用真实模型形成新判读，未再次调用目标/模型，不自动confirmed。其他已知旧库启动新Hub仍会事务性新增两表，运行切换不得当成普通只读检查。详见[原生独立验证](VERIFICATION.md)及STATUS最新。

**2026-11-15真实运行与接续更新：** 用户已配置并授权单origin本地靶场；原生真实模型turn已完成（持久TurnEnded=completed），11条HTTP原件与3个candidate已保存，主已在正式UI回看候选详情、关联证据展开和请求/响应双栏。候选不等于独立确认。原Hub事件并发持久化SQLITE_BUSY已用固定上游一行事务锁补丁修正，一个同库争用专项与新Hub构建通过；见[补丁构建入口](patches/README.md)，普通go build不应用补丁。历史事件缺口不补造，不声称全并发/恢复、全工具隔离或完整研究效果。本次卡死后的两项中断工作已收尾：步骤证据编号支持只读展开/定位，窄屏遮挡问题已修正并通过桌面/窄屏实际操作与看图；当前新预览保留，准确入口与未验范围以[STATUS最新](../docs/STATUS.md)为准。下文“未配置/未调用真实模型”等描述保留为各切片当时的验证边界。

**2026-11-15正式入口增量：** `--local-hub --model-node` 不传 `--model-endpoint` 已接原生正式Provider/Runtime，保存不自动探测，无旧验收3回合/4实例限制，可重开自身状态；显式端点保留旧受控分支。包内构建与2项直接相关检查通过，正式LLM配置及任务/流量/候选UI完成一次tsc/Vite构建与本机登录/配置表单/创建失败路径/真实空态检查。当前已保留可操作预览，准确地址与归属见[STATUS 0.85](../docs/STATUS.md#085-正式入口与新ui已构建并可操作真实模型研究仍待配置与授权)，启动见[RUNNING.md](RUNNING.md)。**尚未配置或调用真实产品模型，没有证明自主研究、非空候选详情/原件或完整恢复。** 下文各历史片的“生产入口未接”仅描述当时状态，不覆盖本段有限新进展。

**2026-11-14 Web/API后端已通过本机模拟模型的工程集成测试（不是自主挖掘效果验证）：** `webapi/`通过原生Web Capability、鉴权和同库SchemaModule保存两项输入、明确origins与独立初始会话ID；原生工具`webapi_http`完成授权请求与受保护Flow原件持久化。实际协议客户端已验证保存/回读→原生Open/Run/StandardLoop→GET与POST→模型消费真实结果→completed及原件正文一致，15条持久事件、两次模型fixture请求/两次目标请求、只读回读不新增发送，client/Hub正常退出并清理。**PUT保存本身仍不启动执行；完整恢复/真实模型研究、全工具出口与最终URI专项未验。新任务/流量前端源码及tsc已有，生产打包和新页面用户链仍未通过，当前无新部署或新功能可操作预览。** 精确证据与限制见[STATUS 0.80](../docs/STATUS.md#080-webapi后端原生协议链真实通过前端与生产打包仍待验)，旧技术fixture限制不作为产品永久限制。

**2026-11-14后续UI质量片：** 原生配置改为有界居中Dialog、已定位状态/Workflow标题文字与必要语义完成；16项正式覆盖、一次新UI离线构建，复用已验Hub无Go改动。真实页面与主8图复核完成目标项，仍有4处Workflow对比度/3类incomplete未冒称通过。旧33901已结束，新34039受控查看预览及父链保护、实际失败/未验见[UI质量验收](ui/UI-QUALITY-ACCEPTANCE.md)与STATUS0.62；不等完整产品、生产研究或视觉认可。

**2026-11-14最新用户流程：** 原生页面模型配置→节点当前报告→创建/单次发送→真实工具结果与持久事件已通过本机受控验收，13份正式源码覆盖、一次离线依赖复用/tsc/Vite及新Hub构建已完成；没有另造Harness/REST。主实际查看配置/节点/Workflow收展4图，另用已有工具做只读a11y审核，发现真实待修项并排除工具WebSocket包装干扰，不称完整视觉认可。当前预览及资源归属以STATUS为准，详见[配置到执行验收](ui/CONFIG-EXECUTION-ACCEPTANCE.md)。下文“UI尚未适配”是各历史切片当时边界，不覆盖本次有限用户链；生产provider/真实研究/业务迁移等缺口保持。

这是独立 Go 模块中的正式入口，直接复用固定版本 Cyber，不是旧 Snapshot→Decision 循环的适配器，也没有新增另一套 Harness。**任务输入与平台HTTP证据业务已接入并通过本机受控协议链；完整业务、生产 provider 与新页面验收仍未完成，旧系统未迁移。**

- **H1：原生 CLI 工具 fixture**，运行与限制见下文；本轮未修改 H1 源码或重跑其整轮验收。
- **H2-A：原生本机空 Web Hub**，新增 `cmd/wscyber-hub`，直接调用上游 `pkg/web/host.Serve`，复用 Web Extension/Auth/typed routes/SQLite/ServeHTTP。非空私有测试 key、loopback、新专属库；没有执行节点或静态 UI，**不能通过这个空 Hub 运行 H1 工具会话**。入口、4 项定向测试、真实 CLI/SIGTERM 验证与边界见 [HUB.md](HUB.md)。

- **H2-B：原生 Web → 本机 fixture 节点 → 工具 → Hub 回读**，在同一 Hub 命令增加显式 `--fixture-node`。直接采用 `host.StartNode`、`node.RunWebSocket`、真实注册/配置握手和原生 Session/AOP；通过薄 Profile 借用 Harness 与原生 Session 协议，只有固定内存 Provider 与无副作用 printf。真实 CLI 已验原生工具调用、14 条持久事件及关闭；默认不加该标志仍是空 Hub。详见 [NODE.md](NODE.md)。这不是 UI、生产 provider 或目标授权出口接入。

- **H3-A / H3-A2：原生UI构建与可选静态接线已完成本机验收。** Hub的`--ui-dir`直接挂原生`host.Config.Static`，默认无UI不变；原始来源锁、最小补充锁与schema2清单明确分离。H3-A的4缺包/17类型诊断通过固定依赖补充解决，原465个非根锁项不变。执行会话正常退出后，主安装437包、完整tsc及Vite通过，11个真实产物文件和24项HTTP/原生鉴权检查通过，临时服务exit0并清理。**该片只验构建/静态资源，不是产品或视觉认可；许可UNKNOWN仍在。** 见[ui/README.md](ui/README.md)。

- **H3-B：真实浏览器原生登录与空态导航已通过。** 复用H3-A2产物、不改产品源码：缺key提示/错key401/正确key200自然Cookie，真实空会话列表、侧栏收展、Agents空抽屉开关；主实际查看7阶段截图。GetConfig400及IOA3个404仍真实存在，页面有缺配置红提示，不能称完整UI通过。临时Hub/浏览器均已关闭并清理，无新常驻预览；**UI→节点执行、非空详情/终端、生产业务及许可缺口仍待后片**。失败记录、脚本修正和真实验收见[ui/BROWSER-ACCEPTANCE.md](ui/BROWSER-ACCEPTANCE.md)。

- **H3-C：Cyber原生UI→fixture-node固定工具→真实页面结果/事件链已通过。** 原生表单登录、节点就绪、UI创建/单次发送，StandardLoop实际执行固定printf；原生Workflow回放收展/参数和结果区、13持久事件与12实时事件核对通过，主实际查看5图。未改产品源码/重构建，没有假API或另一套Harness；公告工具目录空、guardrail未接及IOA404仍保留。生产模型/目标授权/恢复/业务迁移/成果报告仍未完成。详见[ui/EXECUTION-ACCEPTANCE.md](ui/EXECUTION-ACCEPTANCE.md)，最终可操作预览存活/归属以STATUS最新为准。

- **H4-A：原生模型配置持久化子片已通过。** 显式`--model-config`复用FileConfigStore与原生Get/Update/Activate，配置私有落盘、空key保留/视图不回显，三种probe明确拒绝，不创建Provider/节点。两批4个顶层Go用例、唯一Hub构建、真实CLI18项配置/鉴权/拒绝检查及正常退出/资源清理通过；默认空Hub保持，固定UI未改。网络节点、真实配置页交互和恢复仍未接通，详见[CONFIG.md](CONFIG.md)。

- **H4-B：原生网络Provider的单一连接目的地接线已通过。** 新`modeltransport`直接复用固定proxyclient.RegisterScheme与Cyber OpenAI Provider，无上游补丁/依赖变更/代理服务。3定向Go用例及独立编译客户端的本机非流/SSE各一次通过，跨目的地重定向在拨号前拒绝，资源清理已核。保证仅为精确本机IP:port连接级，同目的地路径/协议重定向不全拒绝；尚未接Hub/配置分发/执行节点，详见[MODEL-TRANSPORT.md](MODEL-TRANSPORT.md)。

- **H4-C：原生配置→本机模型节点→网络模型→工具/事件链已通过。** 新`--model-node/--model-endpoint`独立模式复用原生配置分发/节点/StandardLoop；初始和保存配置后均无隐式模型请求，实际两次SSE、一次真实工具、14持久事件与completed/会话关闭通过；新配置超限应用失败后回切已应用配置可清错。保留两次CLI失败及字段/预算修正，最终临时资源已清理。仅本机受控模式（最多3回合/4份实例等临时边界），原生UI/正式研究与业务仍未接入，不作产品交付声明，见[MODEL-NODE.md](MODEL-NODE.md)。

## H1 已落地的调用链

`cmd/wscyber-native/main.go` 负责参数与证据目录；`fixture.go` 在同一装配点直接调用：

1. `harness.New` → `Load`；`Session.Loop=nil`，由上游选用 `agent.StandardLoop`。
2. 通过 `extension.Func` 借用原生 `provider.State`，仅在 `--fixture` 路径注册确定性内存 Provider；`StartupDisabled` 不加载真实模型配置。
3. `Runtime.Observe` → `OpenSession` → `Session.Run` → `Turn.Wait`。
4. 第一次 Provider 响应声明调用原生 `bash`，固定命令为 `/usr/bin/printf 'WSCYBER_H1_NATIVE_TOOL\n'`。**工具执行完全由原生循环/工具注册表完成**，本项目不直接调用 `exec`、不实现 Loop。
5. 第二次 Provider 请求必须携带对应 `callId` 的真实工具结果；成功时返回 fixture 结束语，取消场景在此取消请求。
6. 独立、未取消的 cleanup context 关闭 Session、Harness，再排空/关闭 observer；检查原生工具管理器没有活跃条目，删除本次唯一临时工作区。

原生 `aop.Event` 以 `protojson` 原样保存为 `native-events.jsonl`。`fixture-summary.json` 是验收摘要，**不是业务执行状态、事实主库或第二套事件协议**。验收核对原生事件 ID/序号、Session/Turn/tool-call 关联、调用参数、工具输出、结束原因。资源条目直接保存上游 `proc.Info`，不转成旧 Action/Attempt。

## 本机运行（Linux，Go 1.26；已验 go1.26.4）

在仓库根目录执行；`/usr/bin/printf` 必须存在。只获取/构建本入口可达包，不执行全仓或全上游测试。

```bash
mkdir -p var
RUN=$(mktemp -d "$PWD/var/cyber-h1-manual.XXXXXXXX")
# 本工作区与其他重型任务共用此锁；不要改成各自独立锁来并行。
LOCK="$PWD/var/full-system-models.Pg2PL1Id/heavy.lock"
mkdir -p "$(dirname "$LOCK")"

flock -w 20 "$LOCK" timeout 300s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local \
  go -C native build -p 1 -mod=readonly -o "$RUN/wscyber-native" ./cmd/wscyber-native

flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local \
  go -C native test -p 1 -mod=readonly ./cmd/wscyber-native \
  -run '^(TestFixtureSuccess|TestFixtureCancelAfterTool|TestRejectUnconfiguredProduction)$' -count=1 -timeout=60s

flock -w 20 "$LOCK" timeout 45s env -i PATH=/usr/bin:/bin HOME="$RUN" LANG=C.UTF-8 \
  GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB \
  "$RUN/wscyber-native" --fixture --scenario success --work-root "$RUN" --evidence "$RUN/success"

flock -w 20 "$LOCK" timeout 45s env -i PATH=/usr/bin:/bin HOME="$RUN" LANG=C.UTF-8 \
  GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB \
  "$RUN/wscyber-native" --fixture --scenario cancel-after-tool --work-root "$RUN" --evidence "$RUN/cancel"
```

- 成功 fixture：退出 0，`tool_output=WSCYBER_H1_NATIVE_TOOL`、`stop_reason=completed`。
- 预期取消 fixture：退出 0 表示“取消验收成立”，**原生 Turn 结果仍是 canceled，不是任务成功**；`canceled=true`、`stop_reason=canceled`。
- 两者均应显示 `session_closed/observer_closed/harness_closed/workspace_removed/no_active_processes=true`。
- 不带 `--fixture`：退出 1，明确拒绝未接线的生产运行，不静默回退到假模型。
- `--evidence` 必须是全新目录，已有目录不覆盖；没有 stdin 输入/交互服务/监听端口。运行中断可用 SIGINT/SIGTERM 触发收尾，但 H1 未专项验证外部信号场景。
- 命令结束后仅证据与二进制保留于 `$RUN`，工作区由程序删除；复核后可只删除自己的二进制，证据按需要保留。勿清理用户预览或未知进程。

首次依赖获取可能需更长时间；H1 首次最小可达依赖解析 240 秒超时，检查后仅续取一次成功。它不是模块解析失败或原生运行失败。不要在超时后无限重试或关闭校验/内存保护。`GOMEMLIMIT` 只是软目标，不是硬内存上限。

## 已验范围与真实限制

2026-11-13：唯一入口 build 通过；上述 3 个定向用例通过；编译后的真实 CLI 成功与取消模式通过，分别捕获 13/11 条原生 AOP 事件；真实 CLI 默认模式退出 1。每次一个原生工具条目，命令状态 completed、退出码 0、输出 23 字节，关闭后无 active 条目。上游记录的 `shape=func` 是其 Shell 解释器工作单元，**不能把它当作有独立 PID 的 TTY 记录**；固定外部 `printf` 由原生命令执行器调用并等待。

取消只覆盖**工具已经完成、结果已回到 Provider 之后的请求取消**及未取消上下文收尾；没有证明运行中长进程杀停、强制中断恢复、发送未知动作不重放或完整资源隔离。未跑全仓/全上游/前端/完整矩阵，未启动新浏览器/PG。

`NoEgress` 只发布“无路由端点”，**不是网络沙箱、origin 强授权或不可绕过出口控制**。原生工具仍有宿主权限/默认环境继承能力；示例用 `env -i` 避免继承模型密钥等环境，但这也不构成宿主隔离。本 fixture 无动态命令/真实目标输入、不调用外部网络或付费产品模型，不能据此对一般任务宣称安全边界成立。

H1 自身尚未接入：生产模型选择/任务授权、旧业务数据、HTTP 原件捕获与授权出口、Web 执行节点/UI、持久恢复、成果编号/独立验证/报告/SDL。H2-A 是空 Web Hub；H2-B 另以原生 Web/节点协议接通固定工具 fixture，不从 HTTP 调用 H1，也不改变上述生产/业务缺口。未部署、未数据迁移，也未 commit/push。

固定版本、许可事实与分发前待办见 [DEPENDENCIES.md](DEPENDENCIES.md)。
