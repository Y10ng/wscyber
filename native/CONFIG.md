# H4-A：原生模型配置持久化子片

2026-11-14，分支 `feat/foundation`。**主会话已完成两批定向Go验证、唯一Hub构建及真实CLI/原生Config RPC验收。** 三个新增顶层用例及一个默认空Hub回归全部通过；真实保存/空key保留/活动配置切换/探测拒绝、SIGTERM exit0与私有资源清理通过。浏览器配置交互与网络执行节点仍未验，不把此配置子片称完整产品接通。

本片是从“模型配置→网络执行节点”中拆出的配置管理子片。直接复用固定 Cyber 的 `FileConfigStore`、`SharedConfigCodec` 和 ConfigService RPC；**没有创建 Provider、执行节点、新 Harness、Config REST 或第二状态库**。网络执行仍未接通。

## 1. 显式启用与权威归属

```text
wscyber-hub --local-hub --model-config \
  --listen 127.0.0.1:0 --access-key-file <0600私有key文件> --state-dir <全新目录>
```

- 必须显式 `--model-config`，与 `--fixture-node` 互斥；默认空Hub与既有内存fixture不安装这份配置存储/拒绝路由。
- 原有 loopback字面IP、私有key、独占新0700 state-dir门禁保持；已有state一律拒绝，不迁移/重开旧库。API key不放argv。
- 原生配置的唯一权威文件为 `<state-dir>/model-config/cyber.yaml`。首次有效保存由原生存储创建0700目录、0600暂存/目标文件并提交；程序退出不自动删除操作者的配置和Hub库。
- 明确 `FileConfigStore.Explicit` 路径；完整私有 `cfg.Context.Directory/Home/Executable` 与空 `LookupEnv` 防止回退宿主配置发现/开发环境密钥。没有自定义JSON配置格式，也不从原项目 `.env`、旧产品数据库或真实HOME加载模型配置。
- `ready.model_config_management=true` 只说明配置管理模式，`execution_node=false`；`SystemStatus.llmAvailable` 保持false。原生配置视图可显示活动模型名称/API key已配置，**不表示模型可达或执行已就绪**。
- 本片保留原生 `RuntimeLLM()` 的已提交选项快照供原生状态读取；这不是构造网络Provider。没有Profile/BuildProfile/StartNode，保存、切换不会触发reload/probe。

## 2. 真实原生API与保存语义

所有路径以 `/cyber.rpc.config.ConfigService/` 开头，鉴权和protobuf/JSON wire沿用原生Connect。JSON请求使用 `Content-Type: application/json` 与 `Connect-Protocol-Version: 1`。

| 原生方法 | 本片行为 |
|---|---|
| `GetConfig` | 返回原生 `ConfigView`、固定私有路径、loaded和模型列表；首次未保存为未加载 |
| `UpdateConfig` | 原生校验→Prepare→Commit→返回视图；只允许LLM实际修改 |
| `ActivateProfile` | 通过原生相同保存路径选择已有profile；未知id失败、不改文件 |
| `TestLLM` | 鉴权后明确 `failed_precondition`，不构造/请求Provider |
| `ListModels` | 同上，不请求模型列表 |
| `TestConnection` | 同上，不启用其他扩展连接测试 |

- 原生API key规则：按相同profile id保留传入空值对应的已存key；返回 `apiKeyConfigured`，不返回API key。新profile未填key时可按原生语义保存未配置草稿，**不能称模型已配置可用**。本片没有新造秘密操作按钮。
- 供应商/模型/id/数值等格式使用原生校验；不请求端点来“验证”配置。因此错误模型地址/无效供应商凭据可能保存为配置，但不会被称作连接成功。URL、proxy、名称等本来属于原生可见字段，不应将秘密塞进这些字段；本片没有额外Headers或凭据输入通道。
- 原生 `Prepare/Commit/Discard` 拥有文件算法和空key保留；本地薄适配只限制非LLM设置并将底层文件解析/提交错误变为固定无秘密说明。没有重写原子提交、并发配置锁或原生存储。
- 空 `extensions:{}`、省略agent、`traffic:null`/`traffic:{}`等没有实际修改的形状可以保存；空agent/node消息也归一为省略，不落空章节。非空Agent/流量/节点/扩展设置明确拒绝。Agent可选timeout即使显式为0也表示设置变更，本片不接纳。
- 配置文件持久回读不等Hub历史恢复。Hub仍拒绝已有state-dir；专属库/配置由操作者保留、备份，未来恢复入口须单独实现。

### 探测拒绝为何需要一个薄适配

固定上游 Config API 的显式探测可携带独立 `base_url/proxy`，并在未传key时借用存储key，故仅约束ConfigStore不能约束这些方法。此模式通过原生Route扩展加三个精确procedure路由，其他方法仍由原生ConfigService服务前缀处理。

原生Connect鉴权interceptor没有导出；而HTTP `Auth.Middleware` **只保护 `/api/`，并不保护 `/cyber.rpc.*`**。因此拒绝handler使用现成 `connect.NewUnaryHandler` 和生成的procedure常量，借用原生extension提供的 `web.Auth.Authenticate` 校验Bearer/Cookie。没有新增token存储、Cookie算法或认证策略；错误显式Bearer不能回退到有效Cookie。

有效消息无/错key返回 `unauthenticated`；有效鉴权消息返回真实 `failed_precondition`，不是伪健康或假模型列表。JSON/protobuf由Connect解析；空/坏body可能在解析阶段即失败，不承诺全部错误同一个code。拒绝handler读取上限64KiB，配置存储RPC保留上游自己的限制。

**这不是整个Hub的网络沙箱。** 默认空Hub/旧fixture的其他原生接口行为未扩大整改；持key的本机入口不是细粒度RBAC，不能据此对外开放或授权真实目标。

## 3. 为什么本片没有网络节点

静态核查固定上游发现：

1. `agent/provider.State.Reload/Update` 会自动调用模型 `ping`，并非纯构造；直接用于“仅保存”会产生模型请求。
2. Provider私有HTTP client默认跟随重定向，没有公开client/transport注入点。校验初始loopback URL不足以约束后续重定向。
3. 原生保存广播异步，不等待节点应用回执；保存200不能当节点已生效。
4. `pkg/profile`只提供接口，完整AIScan Profile在package main中且牵连扫描/模板等发行版，不应为本片复制整个发行版。

这些是下一独立片需要解决的扩展点，不通过复制Provider、修改固定上游或另造通用代理来掩盖。本片未调用真实产品模型/目标，未新增依赖/升级版本。

## 4. 固定原生UI：只做静态核对，未做视觉验收

参考固定root `f4b1fb2` / UI `147423bd`：

- `ConfigPanel` 的真实保存调用原生UpdateConfig；表单“设为活动”先改当前表单，保存才提交；输入key为password、返回后保持空值。API另有ActivateProfile，但本片不声称已在UI实测。
- `LLMHealth` 会在mount和保存后自动调用 `TestLLM({})`。本片拒绝该请求，因此页面可能显示模型不可达；显式“测试/拉模型”也不可用。**不隐藏红提示、不返回假健康、不称用户已能在页面完成执行链。**
- 固定UI源码/构建产物没有改动或重构建。产品级“仅显式测试才请求”的前端行为留指定DS后续短片。页面实际保存/切换/展开态仍待真实浏览器验证；H3-C通过的是旧固定内存执行链，不能转作本片验收。

## 5. 主会话已完成的定向验证

主已审阅源码、完整测试和CLI脚本，核对11项交付哈希；专用开发CLI4082613/shell4082612/终端4082582正常退出且退出码0后，单执行/重型串行完成以下验证。命令为复验示例，本轮实际另使用净环境、固定缓存与`GOPROXY=off`，未下载或更新依赖，不跑全仓或上游套餐。

在项目根目录准备本轮独占目录与已有重型锁：

```bash
RUN=$(mktemp -d "$PWD/var/cyber-h4a-check.XXXXXXXX")
LOCK="$PWD/var/full-system-models.Pg2PL1Id/heavy.lock"
mkdir -m 700 "$RUN/tmp"

# 第一批：模式门禁、原生文件事务；没有Hub或模型服务。
flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local TMPDIR="$RUN/tmp" \
  go -C native test -p 1 -mod=readonly ./cmd/wscyber-hub \
  -run '^(TestModelConfigModeBoundaries|TestModelConfigStoreTransactions)$' -count=1 -timeout=45s -v

# 第二批：一个新Hub的配置RPC与拒绝；另串行核默认空Hub不变。
flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local TMPDIR="$RUN/tmp" \
  go -C native test -p 1 -mod=readonly ./cmd/wscyber-hub \
  -run '^(TestModelConfigNativeRPC|TestLocalHubNativeAPIAndShutdown)$' -count=1 -timeout=60s -v

# 唯一Hub build；不在Hub/浏览器运行时并行编译。
flock -w 20 "$LOCK" timeout 240s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local TMPDIR="$RUN/tmp" \
  go -C native build -p 1 -mod=readonly -o "$RUN/wscyber-hub" ./cmd/wscyber-hub
```

第一批覆盖私有显式配置、宿主/环境诱饵不读取、Prepare不发布、Commit/Discard、0600/0700、空key保留、取消与缺失暂存文件失败不损坏已存值、重新构造store回读、非LLM拒绝。

第二批覆盖真实生成Connect客户端/原生Config RPC、代表性ConfigPanel JSON提交、秘密不回显、原生状态不虚报Provider/节点、JSON/protobuf和Bearer/Cookie优先级、独立probe参数与空/坏body拒绝、关闭后FD/端口释放。它只启动一个**零请求哨兵HTTP服务**统计任何误发；不实现模型协议，不返回假模型结果，也不调用原生Provider。

实际CLI简短验收脚本位于本轮包 `var/cyber-h4a.pCfYx9o1/cli-smoke.py`；主审后运行：

```bash
flock -w 20 "$LOCK" timeout 75s python3 -I var/cyber-h4a.pCfYx9o1/cli-smoke.py \
  --binary "$RUN/wscyber-hub" --evidence "$RUN/cli-evidence"
```

脚本只消费该binary：全新私有key/HOME/TMPDIR/state、净环境启动 `--model-config`，只连就绪行的字面loopback Hub，原生HTTP Connect JSON保存/保密/切换/拒绝/无节点、SIGTERM与退出码、端口重绑/SQLite完整性。无浏览器/模型服务/Go子命令。服务停止后仅清理脚本新建的runtime（含测试密钥/库/配置），保留已去秘密的日志与结果；不覆盖已有证据目录，不碰旧现场。CLI测试URL是明确不运行模型的本机端口，**CLI本身不替代单测的零请求哨兵断言**。

### 本轮实际结果

证据目录：`var/cyber-h4a.pCfYx9o1/main-validation.4zxaikxg/`。

- 两批Go共4个顶层用例通过：`TestModelConfigModeBoundaries`、`TestModelConfigStoreTransactions`、`TestModelConfigNativeRPC`、`TestLocalHubNativeAPIAndShutdown`。配置RPC用例覆盖JSON/protobuf、Bearer/Cookie、空key保留、原生UI代表性请求体、三probe拒绝；本机哨兵命中数为0。没有执行Provider或模型服务。
- 唯一Hub build退出0，build日志为空；产物SHA256 `eb8dec5d47e7a2e967b835fe93bc52126a3e6333b258b3d2c52d1f71109f1f9c`，保留供后续消费。
- 实际编译CLI PID4174496/start9867029，在`127.0.0.1:45185`完成18项检查，原生HTTP Connect JSON保存/切换/拒绝成立；该历史地址已关闭，不是用户当前访问入口。脚本和Hub均exit0，输出仅ready/stopped，无key/错误日志。
- SQLite integrity为ok；主独立核验原服务身份不存在、端口可重绑、私有runtime含key/配置/库/HOME/tmp已移除、构建tmp为空、无本轮工作进程、heavy.lock空闲。开发会话已正常退出，无新常驻预览，其他用户现场未动。
- 本片没有动态失败或重跑，全量测试、旧fixture执行链、UI浏览器/构建均未运行。仅增加此配置子片，不把存储回读说成Hub历史恢复。

最终主结论见同包`main-acceptance.md`与`main-final-check.json`。失败应保留记录并停止，不自动重试/扩大测试/重启用户预览。未验：网络Provider/节点分发应用、真实UI配置交互、目标授权、恢复/业务迁移/发现报告SDL。许可UNKNOWN继续保留，未部署/分发/commit/push。
