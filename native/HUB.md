# H2-A：原生本机 Web Hub

**H3-C后续更新（2026-11-14）：** 同一固定binary在显式fixture-node+真实UI下，已实际通过页面创建/单次运行→固定工具→原生Workflow结果和持久事件链。GetConfig200仅fixture只读配置，不改变默认空Hub400；护栏/IOA/公告目录限制保留。详见[ui/EXECUTION-ACCEPTANCE.md](ui/EXECUTION-ACCEPTANCE.md)，默认空Hub及各历史切片未验描述不能代替最新有限验收。

**H3-B更新：** 已使用同一空Hub和真实UI产物完成原生表单/Cookie登录、空会话侧栏收展、顶部Agents空抽屉的实际浏览器验收。GetConfig未配置400及IOA3个404仍存在；不是完整UI或UI→节点执行通过，临时服务/浏览器已清理。见[ui/BROWSER-ACCEPTANCE.md](ui/BROWSER-ACCEPTANCE.md)。下文未验描述保留其历史切片范围。

**H3-A 更新：** 本文“无静态 UI”仍是未配置参数时的默认语义。新增可选 `--ui-dir` 直接挂载固定来源的真实构建产物，key/state 必须在公开根外；具体校验/原生 fallback 与构建入口见 [ui/README.md](ui/README.md)。H3-A2已完成真实原生UI构建及24项资产HTTP/鉴权检查，临时服务正常退出；**浏览器交互、逐功能展开态及UI执行链仍未验收**。`ready.frontend` 仅反映是否配置并校验挂载，不代表页面业务功能就绪。

**H2-B 更新：** 本文记录 H2-A 空 Hub 模式及其历史验收；不加 `--fixture-node` 仍保持这些语义。新增显式本机 fixture 节点模式及真实 Web→工具→Hub 验收见 [NODE.md](NODE.md)，不能把本文“没有 node”理解为新模式也没有节点。

2026-11-13，Linux/amd64，Go 1.26（本轮 go1.26.4）。**这是明确隔离的本机空 Hub 技术小片，不是已接通 H1 工具任务的 Web 产品。** 没有执行 node、生产模型/任务授权、静态 UI、扫描控制台或旧数据迁移。

## 直接复用什么

`cmd/wscyber-hub/main.go` 只做参数/私有 key 校验、独占新 state-dir、信号绑定和无秘密的就绪/停止输出，然后直接调用固定上游的 `pkg/web/host.Serve`。

上游自己拥有以下完整装配和生命周期，不在 wscyber 复制另一份：

`host.Serve → exts/web.New → extension.Set.Load → SQLiteStore + Service + AgentPool + Auth + typed ManagementRoutes → NewHandler → ServeHTTP → Set.Close`

- `Static / StartNode / InitialProfile / ConfigStore / RuntimeLLM / Scans` 均不启用；没有自建 Profile wrapper、Session REST、状态库或 Harness。
- 原生 Service 没有 ConfigStore/Profile/RuntimeLLM 时不发现或读取宿主 Cyber 模型配置。原生 `GetStatus` 显示未加载模型/配置，节点与会话列表为空。
- `ServeHTTP` 使用独立、未取消的 cleanup context；包括启动失败收尾在内，直接沿用原生关闭机制。
- **日志集成限制：** 固定上游 `pkg/web/host/host.go` 会将 AccessKey 传给 `Logger.Infof`。本入口显式传原生 `telemetry.NopLogger()`，已核实它使用 `io.Discard`，没有退回 stderr；不得改接控制台/全局 logger。单测与真实 CLI 均验证测试 key 未出现在 stdout/stderr，失败日志也不回显 key。没有扩展为上游全审计。

## 构建与运行

仓库根目录执行；只构建此入口，所有重型操作复用已有同一锁。测试 key 仅本机新建，不复用真实模型 key、站点 Cookie 或旧产品 token，不将 key 原值放在 argv。

```bash
mkdir -p var
RUN=$(mktemp -d "$PWD/var/cyber-h2a-manual.XXXXXXXX")
LOCK="$PWD/var/full-system-models.Pg2PL1Id/heavy.lock"
mkdir -p "$(dirname "$LOCK")"

python3 -I - "$RUN/test.key" <<'PY'
import os, secrets, sys
fd = os.open(sys.argv[1], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, "w") as f:
    f.write(secrets.token_urlsafe(32) + "\n")
PY

flock -w 20 "$LOCK" timeout 360s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local \
  go -C native build -p 1 -mod=readonly -o "$RUN/wscyber-hub" ./cmd/wscyber-hub

flock -w 20 "$LOCK" env -i PATH=/usr/bin:/bin HOME="$RUN" LANG=C.UTF-8 \
  GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB \
  "$RUN/wscyber-hub" --local-hub --listen 127.0.0.1:0 \
  --access-key-file "$RUN/test.key" --state-dir "$RUN/state"
```

- stdout 第一行形如 `{"event":"ready","address":"127.0.0.1:<随机端口>","mode":"local-hub","execution_node":false,"frontend":false}`；不是业务事件协议或执行状态。
- `--local-hub` 必须显式给出；默认启动退出 1，不回退假模型。
- `--listen` 只接受明确 loopback IP:port，不接受 hostname/通配地址；默认 `127.0.0.1:0`。本轮实际监听验证为 IPv4 loopback，未专项验 IPv6。
- key 必须是非空、无内部空白/控制字符、不超过 4096 字节的 0600 普通文件；拒绝符号链接。不支持空 key 关闭鉴权。
- state-dir 必须尚不存在，父目录须已存在；程序以 0700 独占创建，其下只有原生 `hub.sqlite` 及可能的 SQLite 辅助文件。已有目录一律拒绝，不重开旧库、不删除或迁移已有状态。
- 停止可在运行终端按 Ctrl-C，或向**准确的本轮服务 PID**发 SIGTERM；本轮实际验 SIGTERM 正常退出 0，最后输出 `{"event":"stopped"}`。不要按宽泛进程名清理。
- 程序停止后保留该专属 state-dir/key/二进制供复核，**不自动删除用户文件**。复核后仅清理本次 `$RUN` 下这些专属临时资源；不要清理用户预览或整个 `var/`。本轮验收工作目录、key、SQLite 和二进制已清理，证据保留。

### 原生 API，不是旧 `/api/missions`

| 请求 | 本轮结果 |
|---|---|
| `GET /api/manifest` 无 key / 错 key | 401 |
| 同上，`Authorization: Bearer <私有测试key>` | 200，原生 `product/capabilities/api_routes/ui_contributions` |
| `POST /cyber.rpc.chat.SessionService/ListSessions` 无 key | 401，Connect `unauthenticated` |
| 同上，授权、JSON `{}` | 200，原生 protobuf JSON `{}`（空 repeated 字段省略） |
| `POST /cyber.rpc.agent.AgentService/ListAgents`，授权、JSON `{}` | 200，空节点 `{}` |
| `POST /cyber.rpc.system.SystemService/GetStatus`，授权、JSON `{}` | 200，`status.version=dev`、`serverUrl=/`，模型/配置缺省字段为空/false |
| `GET /` | 404，未提供前端 |
| `POST /cyber.rpc.scan.ScanService/ListScans` | 404，未挂扫描 API |
| `GET /health` | 200；这是原生公开健康端点，不表示执行可用 |

原生 Connect JSON 请求使用 `Content-Type: application/json`、`Connect-Protocol-Version: 1`。鉴权由上游 Auth/Connect interceptor 完成，不自造 API 转发层。

原生 manifest 的 `core.ui_contributions` 固定列出 sessions/chat/agents/settings，**仅是原生贡献声明，不表示本入口部署了这些 UI**。原生 Config/节点/AOP 等路由仍由上游选择；当前空配置写入/激活不可用，不要将路由存在等同业务已接通。某些原生显式模型探测请求可以接受调用方参数，这个本机 key 不是细粒度权限/出口沙箱；本轮未调用这些方法，不授权真实目标/模型请求，也不对外监听。

## 定向验证与证据

```bash
flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local \
  go -C native test -p 1 -mod=readonly ./cmd/wscyber-hub \
  -run '^(TestRejectUnsafeConfig|TestLocalHubNativeAPIAndShutdown|TestListenFailureClosesStore|TestEmptyHubUnavailable)$' \
  -count=1 -timeout=60s -v
```

实际结果：唯一入口可达依赖解析、唯一入口 build、上述 4 项测试均 exit 0；未跑 `go test ./...` 或全上游。测试覆盖：

1. 默认/非法监听/key/旧 state 拒绝（14 个输入子例）；旧目录哨兵不变。
2. 真实 loopback HTTP 与上游生成的 Connect 客户端，鉴权/manifest/空会话与节点/未加载配置、根/扫描不伪造；取消后监听可重新绑定，`/proc/self/fd` 无 SQLite 路径，使用**原生 SQLiteStore**重开空库并关闭；捕获真实 stdout/stderr 检查 key 不回显。
3. 监听被占用时的原生启动失败收尾，无遗留 SQLite FD。
4. 直接调用原生 Service 的缺节点操作，返回原生 `UNAVAILABLE`，没有创建可执行 node 或发目标请求。

另按 run 技能实际启动编译后的 CLI，使用最小净环境（不继承秘密）和全新临时目录，独立通过 HTTP/原生 Connect JSON wire 请求以上 API，SIGTERM 后 exit 0、PID 消失、端口可重绑、同专属 SQLite `integrity_check=ok`，测试 key 从不入日志。**生成 Connect 客户端是在 Go 定向测试中使用；编译 CLI 验收使用 HTTP Connect JSON，不混称两者。**

本轮证据：`var/cyber-h2a.1f8w3Id8/{tests.log,cli-validation.json,cli.stdout.log,cli.stderr.log,cli-smoke.log,binary-build-info.txt}`；其中 `cli-validation.json` 保存服务 PID/start ticks、准确监听地址、HTTP 状态与原生响应结构、退出/清理布尔值，不保存 key。

## 尚未证明的能力

- 空库初始化/重开不是执行历史持久恢复；入口刻意拒绝复用已有 state-dir。
- 没有 H1 Session/Turn/Web 运行接线，没有远端执行节点或 WebSocket/gRPC streaming 验证。
- 无浏览器/UI/PG/扫描模板初始化，无全量测试、真实模型/目标、部署/投递、数据迁移、commit/push。
- loopback + 共享 key 不是多用户 RBAC、TLS、强隔离或目标授权出口；保留原生鉴权边界，不声称它满足全部业务安全需求。
- 固定提交与许可增量见 [DEPENDENCIES.md](DEPENDENCIES.md) 和 [hub-dependency-licenses.json](hub-dependency-licenses.json)。技术通过不表示许可缺口已解决或可部署分发。
