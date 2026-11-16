# H4-C：原生配置 → 本机受控模型节点

2026-11-14，`feat/foundation`。**主已完成定向Go验证及真实原生配置→节点→网络Provider→工具→事件链，最终通过。** 空节点和配置已应用未运行两个检查点均零TCP/HTTP；实际两次SSE、一次固定工具、模型消费真实结果、completed/会话关闭与配置失败后回切均成立。首次回执断言错误、第二次预算终态不匹配及后续修正全部保留；不是首次即通过。原生UI尚未适配/验收，不能据本片称可操作页面已交付。

## 1. 仅新增一个显式模式

```text
wscyber-hub --local-hub --model-node \
  --model-endpoint http://127.0.0.1:<独立允许的非零端口>/v1 \
  --listen 127.0.0.1:0 --access-key-file <0600私有key文件> --state-dir <全新目录>
```

- `--model-node` 与 `--fixture-node`、`--model-config` 互斥；`--model-endpoint` 只属于新模式。默认空Hub、H4-A配置管理、H2内存fixture的语义不变。
- 端点是可信CLI组合根独立给出的允许值，不从模型配置、提示词或模型响应自动推导。仅规范 `http://127.0.0.1:非零port/v1`；逐次连接仍由 [modeltransport](MODEL-TRANSPORT.md) 的实例pin约束。
- 保留私有key、新0700 state-dir与原生SQLite。模型key只通过原生Config RPC存入私有配置，不放argv。验收用净环境/私有HOME与TMPDIR，不继承开发模型凭据或代理。
- `ready.execution_node=false` 仍只表监听已开始，`model_node_configured=true` 是模式意图，**不是节点已应用配置或模型可达**。
- 原生工具仍具宿主权限，本模式不是工具网络沙箱、目标硬授权或生产安全研究入口。不授权任意真实目标/真实供应商请求。

## 2. 全部运行与协议直接复用

组合根：`host.Serve → StartNode → node.RunWebSocket → 薄modelNodeProfile嵌入Harness → 原生Session/StandardLoop`。

首次构造明确 `StartupDisabled`；原生必要的一次空配置禁用Profile重建也允许。工厂拒绝其他Provider启动模式和额外图重建，不把 `StartupOptional` 交给原生联网构造。没有复制AIScan发行版、运行循环、Config REST、Session/AOP或模型协议。

`Profile.ReloadProvider` 是原生节点已有扩展点。仅LLM改变由该钩子调用 `modeltransport.NewLoopbackOpenAI(独立允许值, 原始配置)`，然后 `State.Set(client, client.Config())`。构造与发布不调用 `Reload/Update/Probe`，不发送ping；`HealthConfigured`不等于`HealthReady`。原生空配置可清空当前发布，不回收仍被旧turn引用的client。

原生 namespace registry 注册薄core准入，接受后仍调用同一个 `Runtime.HandleCoreNamespace`；事件、消息、工具执行与Hub持久化均归原生实现。正式 `model_node.go` 没有固定printf、测试模型回复或直接exec。

## 3. 保存、应用、可达性分别核对

- 继续用H4-A `FileConfigStore`：`<state-dir>/model-config/cyber.yaml` 是原始用户配置唯一权威，文件0600、目录0700。保存与活动项选择沿用原生ConfigService，不另造配置格式。
- 新模式只接受LLM；所有保存的profile须显式 `provider=openai`、同一独立允许的BaseURL、空用户Proxy。原始代理/别名/地址在归一化前拒绝；key非空在原生同profile空key保留后校验。新profile空key拒绝，编辑已有profile不强迫重填。空LLM保留真实未配置语义。
- **新模式不安装Hub RuntimeLLM/InitialProfile/BuildProfile**。分发直接来自原始已提交存储。否则上游默认 `provider=openai` 会在初始空配置时合成server-runtime，或将含内部pin的运行时Proxy复制入节点分发。H4-A旧模式不改。
- `client.Config()` 含秘密与内部pin，仅给节点 `State.Set`，不回写用户文件、不交Hub、不入日志或应用摘要。
- 原生保存200只证明Commit。广播异步，经节点 `ReloadResult` 返回后，在原生 `AgentView.Status.Provider/Model/ConfigError` 查看应用事实；该固定版本没有新增的ConfigState字段。
- `RunTurn`在Hub层也先返回Accepted，再异步派发。节点拒绝时，Hub真实写 `turnEnded.stopReason=error`、`turnEnded.error.code=agent_run_failed`。停止原因与错误码不能混同；验收要读终态，而不是要求Hub提前伪造同步拒绝。
- 请求确实到本机协议fixture、模型发出工具调用、原生工具返回、模型实际消费结果、completed及Watch/ListEvents一致，才是本片完整链成功。

### 配置变化与有界实例所有权

Profile最多保留4份**不同已应用配置**的受控client；同值原始配置（含Images指向的值）重用。输入、原始持有快照、运行时快照相互独立。有效更新发布新client，旧turn继续持有旧快照；未安装fallback。

第5份新配置可由原生存储成功保存，但节点明确应用失败，旧有效实例不变。4是这一本机短片为保留旧依赖设置的临时资源上限，不是产品配置版本、计费预算或长期限制；没有造引用计数、热淘汰或通用更新管理器。

Reload与开始Close共用发布锁；关闭标记设定后拒绝新发布。只有原生Harness成功排空Session/工具后才逐个 `CloseIdleConnections`。取消/失败的Close不视为排空、不关旧client，可继续Close。空闲关闭并非永久关闭或活跃调用取消；取消仍归原生context机制。

真正尝试错误仍由原生ReloadResult返回，不将历史失败塞入当前Provider健康。持续的ConfigError只从**唯一FileConfigStore当前已提交期望与当前受控client原始配置**派生：状态读取先在Profile锁外读文件、用同一原生ResolveDistributedRuntime解析独立快照，再锁内比对；不一致提示“已保存尚未应用”，读取/解析失败提示“无法核对”，不当作已应用。A→超限B→保存回A时，即使原生unchanged短路而未调用ReloadProvider，状态也会清除不再适用的错误，旧有效实例不被替换或阻断。该状态采样不新建期望配置权威、不增加上游缺少的逐次revision回执，也不更改在途旧turn结果。

## 4. 模型覆盖与暂不支持的入口

固定源码路径的静态核对结论：

| 入口 | 新模式行为 |
|---|---|
| OpenSession | 使用原生ID/父关系；拒绝extensions及未知字段，正常请求委托原生handler |
| RunTurn | 使用当前已应用受控client；只接受显式1–3回合，无extensions/未知字段，不支持eval、continue；未配置时节点拒绝且零模型请求。原生循环先判断回合上限再判断自然完成，因此两次模型交互的测试使用上限3，实际仍严格只有两次模型请求 |
| 会话command namespace | 节点明确FAILED_PRECONDITION，不开放`/model`、`/eval`、`/compact`、定时循环或直接shell命令；Hub自己的只读help/agents仍可能由原生Hub处理 |
| 持久main-repl/PTY | 原生node内部仍需要main-repl，但不注册PTY协议，不能经该namespace附着 |
| bash内置tmux | **真实BeforeCommand**检查展开后的Name/Args并拒绝；不能经`tmux send-keys`写main-repl后触发`/model` |
| subagent工具 | 原生tool.before明确拒绝；本节点暂未接多Agent预算/模型选择，不是删除产品未来协作需求 |
| 显式TestLLM/ListModels/TestConnection | 复用H4-A精确原生Route拒绝，不返回假健康，不创建Provider |
| fallback | 未配置，不调用原生FallbackProviderConfigs |

固定 `Session.SetModel → State.ForModel` 会直接构造原生Provider，不走H4-B包装；固定代码唯一内置调用者是console的 `/model`，而其前面还会主动 `ListLLMModels`。因此本片封真实console通路，而不是只隐藏按钮或让测试恰好不覆盖。

Go公开 `SessionOptions.Config`、`Session.SetModel`、Providers等仍是同进程可信API，不宣称抵御同进程恶意代码。上述门禁不拦截任意宿主二进制/脚本、直接工具联网或进程内存攻击；正常原生bash仍可执行，**没有将本模式做成“所有安全命令已授权”或通用工具白名单**。固定测试模型只能产生一次无副作用printf和结束语；真实产品模型/目标未获本片授权。

## 5. 主会话已完成的串行验证

执行会话正常退出后，主先跑两个新专项和一个受影响的默认空Hub回归，再构建Hub/外部客户端并实际运行。初次失败后追加两条纯契约专项、仅重跑受影响Profile并有界续验，没有重跑H4-A/B/全量套餐。

主要用例均在 `model_node_test.go`：

1. `TestModelNodeModesAndStore`：拒绝模式/端点、不触监听，原生空存储/秘密保留、用户代理与端点限制，不产生模型请求。
2. `TestModelNodeProfileAdmissionAndOwnership`：实际原生namespace准入、无PTY、shell展开后的tmux与subagent拒绝；构造/发布/拒绝零请求，4份上限/同值Images重用、旧实例保留；唯一store期望回切无需Reload即清错，坏配置显示无法核对；一个悬挂的本机模型请求随原生Session关闭取消，取消Close不提前释放，最终幂等排空。
3. `TestModelNodeExternal`：只在本包runner给定环境时连接实际编译CLI，不启动额外Hub。复用既有wireProbe/原生编解码；初始空节点、原生Config RPC、明确失败终态、两次请求/固定工具及原生事件、保存成功但第5份应用失败，以及回第4份A的原生unchanged路径清错/不新增模型请求。测试client通过两个无秘密stdin/stdout检查点，让driver在未配置、已配置但未运行时核**零TCP/零HTTP**。

本轮主已审阅runner，执行者正常退出后才按下列命令主体串行执行。使用既有固定Go module/build缓存、净环境、GOPROXY=off/GOTOOLCHAIN=local；未下载依赖、未跑全仓，不与开发会话/浏览器并行。初始命令与后续小范围修正的具体日志均保留在验收包：

```bash
RUN=$(mktemp -d "$PWD/var/cyber-h4c-check.XXXXXXXX")
LOCK="$PWD/var/full-system-models.Pg2PL1Id/heavy.lock"
mkdir -m 700 "$RUN/tmp" "$RUN/home"

# 第一批纯模式/存储；第二批才本机Profile/一个挂起模型请求。
flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off TMPDIR="$RUN/tmp" \
  go -C native test -p 1 -mod=readonly ./cmd/wscyber-hub -run '^TestModelNodeModesAndStore$' -count=1 -timeout=30s -v
flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off TMPDIR="$RUN/tmp" \
  go -C native test -p 1 -mod=readonly ./cmd/wscyber-hub -run '^TestModelNodeProfileAdmissionAndOwnership$' -count=1 -timeout=35s -v

# 唯一Hub build；外部协议客户端编译后运行期间不再启动Go编译器。
flock -w 20 "$LOCK" timeout 240s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off TMPDIR="$RUN/tmp" \
  go -C native build -p 1 -mod=readonly -o "$RUN/wscyber-hub" ./cmd/wscyber-hub
flock -w 20 "$LOCK" timeout 240s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off TMPDIR="$RUN/tmp" \
  go -C native test -c -p 1 -mod=readonly -o "$RUN/native-client" ./cmd/wscyber-hub
flock -w 20 "$LOCK" timeout 90s python3 -I var/cyber-h4c.aNivOmQK/cli-smoke.py \
  --binary "$RUN/wscyber-hub" --client "$RUN/native-client" --evidence "$RUN/cli-evidence"
```

失败即停，保留首份结果，不在效果未知的旧session重放。动态检查未运行前，不把gofmt/AST成功写成编译或链路成功。旧模式仅按实际main.go变更必要时挑一个已有用例，不重跑H4-A/B或旧fixture套餐。

runner仅一个本机HTTP协议fixture、一个编译Hub、一个原生协议测试client；只新私有key/HOME/tmp/state。SIGTERM/KeyboardInterrupt进入finally，精确Popen收尾、服务器线程/端口复核、SQLite只读完整性与秘密检查。若强杀/节点排空未证实，保留准确私有runtime而非假称已清理；正常成功才删本次runtime、保留去秘密事件/日志与binary。**外部信号异常收尾尚未动态注入验证。**

### 本轮真实结果、失败与修正

证据包：`var/cyber-h4c.aNivOmQK/`，动态目录`main-validation.mina805i/`，最终成功目录`cli-budget-corrected/`。

- 主核12项交付hash、29项来源hash，并比对其中24份本地上游源码与实际Go模块缓存一致；361既有基线仅main.go改变，执行方新增3正式文件。专用CLI123442/shell123441/终端123423正常`/exit`、退出码0且原身份消失后，主才独占测试。
- 两基础专项、受影响的默认空Hub回归，以及后补`TestModelNodeRejectedTurnSemantics`、`TestModelNodeTurnBudgetBoundary`均通过；预算修正后只补跑受影响Profile/预算小批。主新增预算测试曾将MaxTurns写成int32导致编译失败，改为原生uint32后通过，失败日志保留。
- **首次CLI失败**：0模型请求/0模型连接/0模型client。测试错把`agent_run_failed`放在stop_reason断言，主从首次SQLite原生事件独立解码证实真实字段是`error`与`error.code=agent_run_failed`。只修测试断言；另外修runner异常时丢stdout和把4client场景成功混作排空判断的问题。原脚本/测试/失败库和回执均保留，确认profiles已排空后才清理私有runtime。
- **第二次CLI失败**：两个零请求检查点通过，随后两次SSE均valid且模型已消费真实工具结果，但完成状态断言失败。原生循环先判回合上限再判自然完成，2回合预算与本片目标冲突；第二次事件在断言前尚未保存，不能声称主已独立读取其具体stop_reason。主将本机上限改3、仍严格只允许两次实际模型请求，保持真正completed断言；同时先落原生事件再断言，避免失败丢证据。
- **最终CLI通过**：Hub366291/start10748599（历史127.0.0.1:40735）、client366299/start10748603、driver366279及模型fixture127.0.0.1:44159。未配置/已配置未运行均零TCP/HTTP；实际两次SSE为5052/5342请求字节、一个模型TCP连接，第二次确实消费同call的真实固定printf结果。一次toolCall/result、真正completed及sessionEnded，四份client上限后的B应用拒绝和回A原生unchanged清错全部通过，无额外模型请求。
- 14条持久事件与15条实时Watch由主独立核对：14条持久事件完整对象、ID和cursor全部匹配，额外1条为无持久cursor的messageDelta，不把瞬时delta冒充落库。原始用户配置不含内部pin；随机key不在输出/协议证据。
- Hub/client/driver均已退出，两个Profile与4份client排空关闭，无活跃原生资源；SQLite integrity=ok，stdout正常收尾、stderr空。主独立核两端口可重绑、所有本片runtime含key/配置/库/HOME/tmp删除、build tmp为空、无本片工作进程、heavy.lock空闲。没有常驻预览。
- 最终Hub SHA256 `c90a47520bcd2531bd7ae58b2f4d5c0bf165e2b7e25aec119053c50083c581b7`；最终外部client SHA256 `a2bf0276a203ae97f24d18906a304e73fa1b4bb5ab711b76cede5326a25698da`。旧失败产物/日志保留，不覆盖失败记录；没有重放未知外部动作。

还观察到原生Hello/命令目录仍公告repl/pty/tmux等默认能力与菜单，虽然本模式实际namespace/hook拒绝这些未支持入口；**公告不等于可用或授权**。本片不据目录称终端已开放，原生UI接入前须对齐真实能力展示。正式结论见同包`main-acceptance.md`、`main-final-check.json`及`main-corrections.md`。

## 6. 尚未交付

未做真实供应商/产品模型、真实目标、目标出口/工具沙箱、完整恢复/unknown重放窗口、原生配置页及UI执行交互、业务迁移、报告/SDL、部署或分发。本片UI未改：默认RunTurn回合值/自动probe等原生页面行为并未适配新模式，不能称UI已可使用它。固定Cyber/AOP、固定UI、Go依赖与H4-B transport未修改，许可UNKNOWN与原组合义务保持。未commit/push/再委派。
