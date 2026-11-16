# H4-B：原生 OpenAI Provider 的连接目的地接线

2026-11-14，分支 `feat/foundation`。**主会话已完成3个定向Go顶层用例、唯一公开包消费者构建及真实本机非流/SSE各一次协议调用，全部通过。** 开发会话正常退出后才串行验证，消费者/协议fixture与私有runtime已清理。本片没有连接Hub、配置分发、节点或UI；真实网络Provider调用不等于配置→节点→研究闭环。

## 1. 复用与实际范围

`modeltransport` 通过固定 `proxyclient.RegisterScheme` 提供原生 `DialFactory`，由 Cyber `ProviderConfig.Proxy → newHTTPClient → Transport.DialContext` 装配。没有代理服务、Provider协议副本、第二Harness、上游补丁、临时modfile或模块缓存修改。

```go
// allowed来自可信组合根独立确认，不能从用户/模型配置自行推导为授权。
client, err := modeltransport.NewLoopbackOpenAI(allowed, config)
if err != nil { /* 不发布 */ }
// 构造和Set均不请求模型；不调用State.Reload/Update/Probe。
state.Set(client, client.Config())
// 按原生Provider/StreamingProvider接口显式请求。
// 所有依赖调用/流/旧turn排空后，拥有者才关闭空闲连接。
client.CloseIdleConnections()
```

这只是模型传输的薄装配，不拥有运行循环、任务状态或配置文件。`Config()` 是含秘密、含内部Proxy的**运行时独立快照**，供可信组合根使用；不记录、不回显、不写回H4-A的用户配置。构造输入、内部Provider、返回快照的 `Images` 指针也分别拷贝，外部修改快照不修改既有实例的key/地址。

### 初始配置支持/拒绝

- 仅精确 `provider=openai`，不做供应商推断或别名回退；其他协议/供应商名拒绝。
- 独立 `allowedEndpoint` 必须是规范的 `http://127.0.0.1:<非零端口>/v1`，且 `config.BaseURL` 完全相等。每实例只允许一个明确端口；不接受整个loopback网段。
- hostname、IPv6、其它IP、初始HTTPS、端口0/省略端口/前导零、userinfo、query（含空`?`）、fragment（含空`#`）、其它路径或尾斜杠拒绝。
- 调用方 `Proxy` 必须为空，包括 `direct` 和伪造内部scheme。内部 `wscyber-model-pin://127.0.0.1:<port>` 仅由装配生成，不是可填写的网络代理。
- model/key非空，其余数值及缺省超时沿用原生 `Resolve`。不发现HOME/环境中的配置，不装fallback。
- 只委托 `Name`、`ChatCompletion`、`ChatCompletionStream`、`CloseIdleConnections`；不匿名嵌入原生Provider，不额外暴露 `ListModels`/`WebSearch`、底层client或可变配置指针。没有重写协议/SSE/响应解析/重试。

## 2. 保证是TCP目的地，不是通用HTTP授权

工厂闭包持有不可变 `netip.AddrPort`，每次新Dial先校验network为tcp/tcp4及规范字面IP:port，才使用本实例自己的 `net.Dialer` 连接固定IPv4。hostname在任何DNS解析前拒绝；不调用proxyclient传入的 `upstreamDial` 或全局 `DefaultDial`。所有实例各自持pin，不靠可变全局目的地。

因此，通过该装配的原生Provider新连接（包括重定向产生的连接）只能到一个获准目的地；**同一IP:port上的路径重定向允许，同一目的地的HTTP→HTTPS也不保证拒绝**。Dial看不到HTTP路径/完整origin，不可声称“拒绝所有重定向”“HTTP严格同源”或“只发送HTTP字节”。授权服务本身可能再代理请求；本片不控制对端的后续行为。

固定实现与当前工具链的静态依据：

- Cyber私有 `http.Transport` 的 `Proxy=nil`，不是默认Transport。`cfg.Proxy`只设置DialContext，不开启HTTP CONNECT或环境代理。
- Go1.26（当前已安装1.26.4）HTTP/1 keepalive key包含scheme和targetAddr；只有真正HTTP代理时才可能去掉targetAddr，而本路径没有这种代理。相同目标复用已建立连接，跨authority需要新Dial。
- 自定义DialContext、`ForceAttemptHTTP2=false`，当前原生Transport未自动启用HTTP2/跨authority coalescing，也未注册其它协议。HTTPS重定向仍先经过同一DialContext，未设置另一个DialTLS旁路。
- OpenAI非流/SSE使用同一私有client。原生图片URL只编码成模型请求，并不在Provider里下载；返回链接不在此抓取。独立JEV client不在该构造分支。
- `RegisterScheme`全局map没有运行时锁。本包只在 `init` 注册一次唯一scheme，不覆盖内建scheme，不在请求/构造期间注册。不防御同进程可信代码主动覆盖注册表或另建未受控Provider；后续集成不得新增这种运行时写入。

这是固定依赖路径的保证，升级Cyber/proxyclient/Go时要重核这些事实。不是工具出口、目标授权、整个Hub、其它Provider或OS网络隔离。H4-A默认/fixture/三probe拒绝没有改动；当前Hub尚未引用本包，保存配置不会自动启动网络Provider。

## 3. 生命周期与未支持集成

构造只Resolve/装配，`State.Set`仅将健康标为configured，不能称ready或模型可达。没有调用 `State.Reload/Update/Probe`，没有自动模型请求。

本片生命周期限定为显式创建 → 显式调用 → 排空调用和SSE → `CloseIdleConnections`。关闭空闲连接不取消活跃调用，也不是永久关闭；之后再次请求会新连接且再次检查pin。取消通过调用context及原生机制传播。

下一组合根若替换Provider，必须持有旧实例直到其turn/session排空，再关闭旧空闲连接。本片没有实现热更新所有权管理、State.ForModel、节点分发应用回执、持久恢复、unknown重放控制或完整任务链。

原生请求对象可能被修改，沿用原生调用约定，不并发复用同一个请求。原生错误/响应原文可能包含对端内容，调用方必须按自己的日志/证据策略处理；NopLogger或本包不打印日志不等于所有错误都可安全展示。

## 4. 已完成的定向验证

`modeltransport/transport_test.go` 的3个顶层用例已由主分两批串行运行，全部通过，无 `t.Parallel`：

1. `TestConfigurationAndDialBoundaries`：纯参数/factory/注册地址、快照克隆、未公开附加能力、network/字面地址拒绝；stub证明拒绝时底层dial零调用。没有监听或真实拨号。
2. `TestNativeHTTPAndLifecycle`：一个本机协议服务；构造+State.Set零连接/零请求、真实非流/SSE wire、key/model快照不变、keepalive、关闭空闲后再连接、活跃请求不被IdleClose取消及显式context取消。
3. `TestNativeRedirectAndInstancePins`：本机两个服务；预热keepalive后跨端口/hostname/其它loopback IP/HTTPS其它端口/流重定向拒绝，第二目的地TCP和HTTP零命中、DNSStart零次；同目的地307允许；第二实例独立pin，第一个重新连接不能借第二实例放宽；传入upstreamDial不被使用。环境代理指向本机哨兵，但不应命中。

TLS同目的地升级没有动态测试；只按源码说明不拒绝、不声称TLS功能完成。测试是局部受控模型HTTP边界，不证明所有网络/所有协议零外发。

### 主会话串行执行入口

以下为复验命令主体。本轮实际使用既有固定缓存、净环境、`GOPROXY=off`、`GOTOOLCHAIN=local`和同一heavy.lock，精确命令/日志在本轮验收包。每批失败即停，不扩成全仓或重跑H1/H2/H3/H4-A整套。`RUN`须是本轮新独占目录，`TMPDIR`/HOME在其中，cache路径由主使用既有值。

```bash
REPO="$PWD"
RUN=$(mktemp -d "$REPO/var/cyber-h4b-check.XXXXXXXX")
LOCK="$REPO/var/full-system-models.Pg2PL1Id/heavy.lock"
mkdir -m 700 "$RUN/home" "$RUN/tmp"

# 第一批：纯参数/factory；第二批才打开本机模型协议fixture。
flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off TMPDIR="$RUN/tmp" \
  go -C native test -p 1 -mod=readonly ./modeltransport -run '^TestConfigurationAndDialBoundaries$' -count=1 -timeout=30s -v
flock -w 20 "$LOCK" timeout 180s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off TMPDIR="$RUN/tmp" \
  go -C native test -p 1 -mod=readonly ./modeltransport -run '^(TestNativeHTTPAndLifecycle|TestNativeRedirectAndInstancePins)$' -count=1 -timeout=45s -v

# 唯一公开包消费者build，不构建/启动Hub。
flock -w 20 "$LOCK" timeout 240s env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off TMPDIR="$RUN/tmp" \
  go -C native build -p 1 -mod=readonly -o "$RUN/consumer" "$REPO/var/cyber-h4b.bRlUObh3/consumer.go"
flock -w 20 "$LOCK" timeout 60s python3 -I var/cyber-h4b.bRlUObh3/cli-smoke.py \
  --binary "$RUN/consumer" --evidence "$RUN/cli-evidence"
```

CLI脚本不运行Go：一个Python本机HTTP协议fixture + 一个实际编译后的公开包消费者，以原生Provider发一次非流和一次SSE。仅新私有runtime、随机测试key、净环境，不读真实供应商配置。日志/结果不留key正文；消费者只输出固定错误说明。结束回收自己Popen持有的进程、关闭线程/监听、检查端口可重绑并删除自己新建runtime，保留无秘密证据和binary。不是Hub/节点/研究/工具执行验收。

### 本轮实际结果与收尾

证据：`var/cyber-h4b.bRlUObh3/main-validation.kaju9m9d/`。主先核12交付hash/14来源hash和358既有基线不变，读完整实现/测试/脚本；专用CLI7760/shell7759/终端7746正常`/exit`、退出码0且全部消失后才执行。

- 第一批纯边界用例及12配置子例通过；第二批非流/SSE/生命周期和redirect/实例pin两用例及6重定向子例通过。构造/State.Set零请求、被拒绝目的地TCP/HTTP/DNS零触及、同目的地307允许、连接复用/空闲关闭/显式取消等断言成立。
- 唯一公开包consumer构建exit0；实际consumer PID100295/start10139534经原生网络Provider连接本机协议服务`127.0.0.1:42989`（driver PID100293），非流false/110请求字节、SSE true/149请求字节，恰两次有效请求、一个TCP连接，模型/随机key/消息/顺序由fixture实际检查。两个进程已退出，此地址不是常驻入口。
- driver/consumer均exit0，stderr空；消费者只输出公开成功摘要。主独立核原身份不存在、端口不用SO_REUSEADDR也可重绑、私有runtime含key/HOME/tmp删除、build tmp为空、无本片工作进程、heavy.lock空闲。
- 保留consumer SHA256 `9b5e5d788b80840c0f8c268b28429cc2d6fe2910b13830a395e884b8986b15ba`及去秘密证据；没有Hub构建/运行、浏览器、旧fixture或全量回归。
- 主仅在交回后为本包CLI runner补SIGTERM进入finally、KeyboardInterrupt失败标记及关闭后的最终计数核查，原交回脚本保留；正常调用一次通过，**没有动态注入停止信号验证该补正路径**。正式transport与test源码保持交付hash不变。

详见同包`main-acceptance.md`与`main-final-check.json`。TLS同目的地升级、节点/会话热重载、真实供应商及所有未列协议仍未验证。

## 5. 固定来源、许可与变更边界

Cyber/AOP仍固定 `v0.0.0-20261109041610-f4b1fb2f3315`，proxyclient仍为现有可达版本 `v1.1.1-0.20260728110701-74504679dc47`。没有新增或升级Go模块，`go.mod/go.sum`不改。Cyber根AGPLv3、proxyclient根GPLv3以及既有UNKNOWN/组合义务见 [DEPENDENCIES.md](DEPENDENCIES.md)，不因本片可构造/可测试而认定可部署分发。

直接源码参考：Cyber `agent/provider/{http,provider,openai,state}.go`；proxyclient `client.go/proxy.go/utils.go`；本机Go `src/net/http/transport.go`。本包交回保留这些输入和新增源码/脚本SHA256，固定模块缓存只读。

本片无前端/浏览器、真实产品模型/目标、系统网络调整、旧业务/库迁移、部署、commit/push或再委派。产品目标与本片实际验证结果严格区分。
