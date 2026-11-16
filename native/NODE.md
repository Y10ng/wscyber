# H2-B：原生 Web 派发、本机 fixture 节点与 Hub 事件回读

**H3-C后续更新（2026-11-14）：** 本文同一fixture节点和固定binary未修改，已进一步通过真实Cyber UI单次创建/发送、固定bash输出、原生Workflow回放收展、13条持久事件/12条实时事件回证。详见[ui/EXECUTION-ACCEPTANCE.md](ui/EXECUTION-ACCEPTANCE.md)。下文未做UI是H2-B当时范围；后续有限UI链不等生产模型/目标、护栏、完整恢复或全部界面已验。

2026-11-13，Linux/amd64、Go 1.26.4。**仅一条固定本机技术链已验收，不是生产安全任务或 UI 上线。** Cyber/AOP 继续固定 `f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf`，未改上游。旧 H1 源码与 H2-A 空 Hub 模式保留。

## 实际参考与接线

参考固定上游 `pkg/web/host/host.go`、`pkg/node/{agent,proto_connection}.go`、`pkg/node/reload_test.go`、`pkg/profile/profile.go`、`exts/session/protocol_extension.go`、`pkg/web/{connect.go,service/application_dispatch.go,api/session.go}`。没有加载完整 AIScan 发行版/扫描模板，也没有创建新 Harness/Loop/Session REST/事件表。

1. `wscyber-hub --local-hub --fixture-node` → 原生 `host.Serve`。
2. `Config.StartNode` → 原生 `node.RunWebSocket`；原生先以 `StartupDisabled` 建立 Profile，启动它要求的持久 `main-repl`，进行 AgentHello/AgentAccepted 与配置分发/回复。没有手工标 ready。
3. 薄 `fixtureProfile` 嵌入现成 `harness.Harness`，借用 Runtime/Providers/Events/Progress/Processes；只补 Namespace/Console/Status。协议通过原生 registry + `exts/session.NewProtocol()` 绑定，不复制 handler。
4. Hub 的**只读 fixture ConfigStore**只返回一个常量配置，写入/提交拒绝；默认空 Hub 不安装它。原生 `Config.Distribute` 发给节点，`ReloadProvider` 严格比较完整固定配置后才 `State.Set` 内存 Provider；未知配置拒绝，不调用真实模型。
5. 外部验收客户端通过生成 Connect `ListAgents` 确认节点 ID/配置模型，再经 `/api/aop/application/ws` 的原生 `OpenSessionRequest → WatchEventsRequest → RunTurnRequest` 派发。
6. 原生 `StandardLoop` 调用原生 `bash`，固定命令：`/usr/bin/printf 'WSCYBER_H2B_NATIVE_TOOL\n'`。内存 Provider 第二次请求必须实际包含相同 `callId` 的成功工具结果；全进程只允许这两个模型边界请求，不接受额外工具回合。
7. 节点原生事件经 AOP 回到 Hub，由上游持久化。通过同一原生 Application 通道 `ListEventsRequest` 读取，核对 watch 与持久记录的 ID、cursor、session/turn/call、参数、输出及 completed 终态。再原生 `CloseSessionRequest`、回读 SessionEnded、取消订阅并关闭 WebSocket。
8. SIGTERM/宿主 context 取消后，由 `host.Serve` 取消并等待节点；原生节点停止持久 REPL、协议与 Harness；之后关闭 Hub/SQLite。薄 Profile 只读取原生 Runtime/Manager 的收尾事实，程序移除自己刚创建的节点工作区。

`fixture-closed` JSON 是关闭后的诊断摘要，不是运行协议、事件主库或业务状态。没有 HTTP handler 调用 H1 `runFixture`、直接运行命令或补写 SQLite 事件。

## 显式模式与私有边界

- 保留 `--local-hub`、loopback IP、0600 非符号链接 key、独占新 state-dir 门禁。**`--fixture-node` 不代替 `--local-hub`**；默认生产入口仍拒绝。
- `ready.execution_node=false` 是保守的“此时未认证执行就绪”声明，`fixture_node_configured=true` 仅表配置意图；`ready` 只保证 Hub 已绑定监听，执行可用必须看原生节点状态并实际派发。
- 节点新工作区位于新 state-dir 中；`cfg.Context` 明确指定私有 Home/Directory/Executable 与空 `LookupEnv`，不发现真实配置。真实 CLI 同时使用私有 HOME/TMPDIR 与净环境，不继承代理、开发认证、站点 Cookie、产品 token 或模型配置。
- 配置 schema 的 `provider=openai` **不是使用 OpenAI/付费模型**：显式固定内存 Provider；`base_url=http://127.0.0.1:1/h2b-no-http` 仅拒绝误配的标记，不启动/访问该端口。常量 `fixture-not-a-credential` 不是凭据。
- 原生 agent Option 没有独立 token 字段，故仅在内存以 `url.User(key)` 装配有效 loopback URL；上游 `aopws.DialURL` 去除 userinfo 后转为 Authorization header。URL 不入 argv/证据；错误返回固定无秘密说明。宿主、Profile、node 始终 NopLogger，保留 H2-A 的上游 AccessKey 日志限制。
- `Option.Timeout=3600` 仅对齐上游配置默认值，以免首次 reload 被误判为图重建；没有把此值当固定工具时限。工具参数 `timeout=5`、Terminal Timeout=5、MaximumTimeout=5s；验收协议 context=20s、读 deadline=15s，CLI 驱动外层时限75s。**没有据此验证超时杀停能力。**
- 原生 REPL 是 node 机制必需的内部资源，不提供用户前端/终端 UI；本片不附着 PTY/执行交互命令。真实 CLI 私有 HOME 下生成的 carapace 样式文件也归本轮目录并已清理。
- `NoEgress`、loopback/key、WebSocket Origin 不是工具出口沙箱或业务目标授权。持 key 的原生 API 不是细粒度 RBAC；本 fixture 不对外监听、不授权真实目标或额外原生命令。工具仍具宿主权限，不宣称强隔离。

## 构建与使用

复用 [HUB.md](HUB.md) 的 key/全新目录准备步骤。所有 Go 构建使用同一 `var/full-system-models.Pg2PL1Id/heavy.lock`，`-p 1`、`GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB`；每次一个重型命令，不跑全仓。

```bash
# 仓库根目录，RUN/LOCK 按 HUB.md 准备。先构建，不在已有服务旁并行构建。
flock -w 20 "$LOCK" timeout 240s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local \
  go -C native build -p 1 -mod=readonly -o "$RUN/wscyber-hub" ./cmd/wscyber-hub

mkdir -m 700 "$RUN/home" "$RUN/tmp"
flock -w 20 "$LOCK" env -i PATH=/usr/bin:/bin HOME="$RUN/home" TMPDIR="$RUN/tmp" LANG=C.UTF-8 TERM=dumb \
  GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB \
  "$RUN/wscyber-hub" --local-hub --fixture-node --listen 127.0.0.1:0 \
  --access-key-file "$RUN/test.key" --state-dir "$RUN/state"
```

运行不自动派发任务；需真实原生客户端连接。Ctrl-C/SIGTERM 对准确服务 PID 发信号关闭，节点临时工作区自动移除；key/Hub DB/二进制是操作者文件，复核后只清理本轮独占目录，不删用户预览或未知资源。

验收客户端在 `fixture_node_test.go`：不是另一服务器；`TestFixtureNodeExternal` 只在显式 `WSCYBER_H2B_ADDRESS / WSCYBER_H2B_KEY_FILE / WSCYBER_H2B_EVIDENCE` 参数下连接一个 loopback Hub。可先以 `go test -c -p 1 -mod=readonly` 编译，再在服务运行期间以净环境运行该**唯一外部用例**，不启动第二 Hub；证据目录须新建且为空，不用 shell 参数传 key 原值。不要在运行链旁再次构建。

## 本轮实际验证

| 层级 | 真实结果 |
|---|---|
| 首次最小测试 | 未知配置拒绝通过；链路等待原生配置就绪20s超时。初始 Option.Timeout=0 与分发解析默认3600造成图差异，原生请求重建被 fixture 明确拒绝；保留失败日志，没有绕过握手。对齐默认值后单链用例通过。 |
| 最终4项定向测试 | `TestFixtureProfileRejectsUnknownConfig`、`TestFixtureNodeNativeFlowAndShutdown`、`TestRejectUnsafeConfig`、`TestLocalHubNativeAPIAndShutdown` 全部 PASS。没有机械重跑全 H1/H2-A。 |
| 主复核后窄补正 | 外部验收地址改用 `netip.ParseAddrPort` + loopback/无Zone/非零端口校验，不用 DNS。仅补跑 `TestFixtureExternalAddress` 的8项纯解析断言：IPv4/IPv6 loopback接受，hostname/远端/通配/Zone/零端口拒绝，全部PASS。未重跑链路/服务，原真实CLI的明确IP地址符合新旧校验。 |
| 同进程链路关闭 | 原生 Profile/Runtime inactive，原生 Manager 仅一个 REPL+一个 printf 且都非 active；监听可重绑、`/proc/self/fd` 无本库句柄；原生 SQLiteStore 重开并只读核对事件后关闭。**不是执行恢复验证。** |
| 真实编译 CLI | 私有净环境启动 PID2676906/start6350617，127.0.0.1:38767；外部编译 Go 客户端经真实生成 Connect 与 AOP WebSocket 完成链路。没有通过 import 代替实际启动。 |
| 原生输出/关联 | node=`wscyber-h2b-fixture-node`，session=`h2b-session`，turn=`h2b-turn`，call=`h2b-bash-1`；一个原生 tool-call/result，实际输出 `WSCYBER_H2B_NATIVE_TOOL`，Turn completed，14条持久 EventDelivery，watch工具/结果/终态与回读一致。 |
| 拒绝/秘密 | Application WS 无 key/错 key 均401，缺失 node 原生 UNAVAILABLE；未知 Provider 配置拒绝、未激活 Provider。无多余工具执行；真实 stdout/stderr/保存证据不含私有 key。未单独覆盖“已连接但配置尚未就绪”的派发竞态。 |
| CLI收尾 | SIGTERM exit0；节点/Profile/Runtime已停，printf completed/status0/24字节，原生持久 REPL 的 func 条目 killed/reason=canceled，无活跃工具/子进程；PID消失、端口重绑、SQLite integrity_check=ok。专属工作区/HOME样式文件/tmp/key/库/二进制清理，证据保留。 |

真实 CLI 的 REPL `shape=func` 不是被遗留或杀掉的独立 OS PID；只证明此固定链完成后的关闭路径，没有长进程执行中强停/断网重连/未知发送窗口/幂等恢复的完整验收。

证据：`var/cyber-h2b.nRd1jnEH/{tests-first.log,tests-chain-second.log,tests.log,tests-address.log,cli-client.log,cli.stdout.log,cli.stderr.log,cli-validation.json,cli-evidence/,binary-build-info.txt,cleanup.json}`。`cli-evidence/hub-events.jsonl` 是上游原生 EventDelivery 回读，`watch-envelopes.jsonl` 是原生实时 Envelope，`flow-summary.json` 仅验收摘要。原生临时 delta 不被冒称持久记录。

未做：UI/浏览器/PG/扫描/报告/真实 provider/目标/授权出口/旧业务库/完整持久恢复/全量测试/部署/迁移/投递/commit/push。新增12项可达依赖及 carapace 附加许可未知边界见 [DEPENDENCIES.md](DEPENDENCIES.md)。
