# H1 / H2-A / H2-B 固定依赖、来源及许可边界

核验日期：2026-11-13。仅 Linux/amd64 原生 CLI 与本机空 Web Hub 小片；不是完整 Cyber 扫描发行版或 Web UI 许可审查。完整模块约束和校验和在 `go.mod` / `go.sum`。H1 的 61 个可达外部模块及模块根许可文件 SHA-256 在 [dependency-licenses.json](dependency-licenses.json)；H2-A 的 81 个可达模块（原 61 个版本不变 + 新增 20 个）见 [hub-dependency-licenses.json](hub-dependency-licenses.json)，增量说明见本文末节。许可分类只依据核对过的固定模块根文本/补充通知，不替代完整嵌套文件/字体/二进制资产和组合审查。

## Cyber / AOP：同一固定提交，公开 Go 模块可复现

- 官方仓库：<https://github.com/chainreactors/cyber-harness>
- 固定提交：`f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf`
- 提交时间：`2026-11-09T04:16:10Z`
- 固定伪版本：`v0.0.0-20261109041610-f4b1fb2f3315`
- 上游声明模块名 `github.com/chainreactors/cyber`，仓库名为 `cyber-harness`。依赖方通过**版本化** `replace` 映射，不改上游 module 声明。
- AOP 是 `aop/` 独立 Go 子模块，原上游 `replace => ./aop` 不会被消费方继承。因此单独映射 `github.com/chainreactors/cyber/aop` 到 `github.com/chainreactors/cyber-harness/aop` 的**同一伪版本**。
- 已实际运行 Go 模块下载；两个 `.info` 的 `Origin.Hash` 均为上述完整提交，AOP 的 `Origin.Subdir=aop`。不依赖 `/root/桌面/cyber-harness`，不手写模块缓存、不从本地源码目录编译、不关闭公共模块校验；无需额外 bootstrap/私有源码种子。

| 发布模块 | Go 内容校验和 | go.mod 校验和 |
|---|---|---|
| cyber-harness | `h1:7Vkag5ofh2pim5LRBJ23rf2SgeBzoyR+B44lPvi4zvw=` | `h1:gUO44roGW3N6FKfaNIRb6LHveAmxNj4Mtbw64g8By+Q=` |
| cyber-harness/aop | `h1:ZE1apyRPZRLdpOVAF/4Cx3TVZC5CK+C1nFShITwedeE=` | `h1:NUCgOvzxm7pEr9MqV2XYahrtV2DIjXbh9QkOT/ec54Q=` |

fresh checkout 路径：安装符合 `go.mod` 的 Go（本轮 go1.26.4），按 README 从 `native/` 构建唯一入口，Go 从公开代理/源仓库取固定模块并验证 `go.sum`。本轮确实通过公开模块取得源码并编译，**未另做完全空缓存/全新主机重跑**。不使用绝对或本地目录 `replace`。

### 直接用途与许可

| 依赖 | 固定版本 | 用途 | 核验事实 |
|---|---|---|---|
| Cyber + AOP | 同上 | Harness/StandardLoop/Session/Turn、工具与扩展、原生事件 | 两个模块均携带 GNU AGPL Version 3 文本 |
| `github.com/chainreactors/utils/proc` | `v0.0.0-20261002195803-fc9e07c4b4fd` | 原生工具资源记录及关闭状态读取；不新建管理器 | 模块根许可文件缺失，UNKNOWN，见下节 |
| `google.golang.org/protobuf` | `v1.36.12` | 原生 AOP protojson 序列化和只读复制 | BSD-3-Clause 根文本 |
| `mvdan.cc/sh/v3`（传递） | `v3.14.1` | 原生 Bash 解释器 | BSD-3-Clause 根文本 |
| `github.com/chainreactors/proxyclient`（传递） | `v1.1.1-0.20260728110701-74504679dc47` | 原生工具出口相关库的可达依赖；本 fixture 不调用代理/网络 | GNU GPL Version 3 根文本 |

Cyber 根、AOP 模块及只读本地固定上游的 `LICENSE` 字节哈希一致：

`8d56b405468aad11f87ab5763f901e276e08d9646ff5c8481b1762b6b789e9ed`

原始许可可从固定源码获取：<https://github.com/chainreactors/cyber-harness/blob/f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf/LICENSE>。Go 发布的 AOP 模块包含同一根许可。文本存在不自动确定所有文件的 only/or-later 授权，也不表示组合/网络服务源代码提供义务已处理。

### 明确未解决的许可问题

以下**实际可达**模块在本次固定 Go 模块根中未包含 LICENSE/LICENCE/COPYING/NOTICE/COPYRIGHT，亦无根 README 许可补充；不能推断为 MIT，也不等于已确认整个仓库“无许可证”：

- `github.com/chainreactors/logs v0.0.0-20260624034259-9aaea4aa52cc`
- `github.com/chainreactors/utils v0.0.0-20260711153742-f3d210a5fa9d`
- `github.com/chainreactors/utils/parsers v0.0.3`
- `github.com/chainreactors/utils/proc v0.0.0-20261002195803-fc9e07c4b4fd`

H1 只进行获准的本机技术验证，未部署/对外发布。**后续分发或网络部署前须补齐这些固定版本的授权来源，核对 AGPL/GPL 组合及对应源代码/通知义务；本片不作“法律问题全部解决”声明。** 模块内嵌资产、跨平台实现（例如 Windows 相关文件）、传递子目录的额外许可均未完整审查。

### 上游两个 fork replace

消费方显式记录上游固定映射：

- `github.com/wasilibs/go-re2 => github.com/chainreactors/native v1.11.1-native.1`
- `github.com/projectdiscovery/katana => github.com/chainreactors/katana v1.7.0-aiscan.1`

它们保留上游模块图的兼容约束，但本次 `go list -deps ./cmd/wscyber-native` **没有可达包使用它们**，未下载/编译对应扫描能力，不冒称相关扫描引擎/模板的许可已验证。后续扩大实际采用范围须重新核对，不能仅凭已有 replace 推断许可或功能就绪。`tools/templates` 与 `web/frontend/cyber-ui` 子模块未初始化/采用。

## 核验范围

- 实际参考：固定上游 `pkg/harness/{harness,base,harness_test,shared_test}.go`、`examples/session/main.go`，以及实现所需 Provider、Session、terminal、eventbus、proc 源码。
- 复用：上游拥有运行循环、工具调度、进程管理、原生事件与关闭语义；本项目仅做入口配置、明确隔离的 fixture Provider、证据序列化与验收。
- 新增自定义运行类型只有 fixture 验收摘要，不提供通用 Harness wrapper/第二套调度/持久状态权威。
- H1 校验基于唯一入口 build、3 个定向测试和真实 CLI；未跑上游全量测试、未用真实模型/目标，也未验证完整业务迁移。

## H2-A：本机 Web Hub 的许可增量

H2-A 继续使用相同 Cyber/AOP 固定提交/公开模块，不修改上游。实际参考并直接复用 `pkg/web/host/host.go`（而非重拼宿主）、`exts/web`、`core/extension`、`pkg/web/{handler,connect,http}.go`、`pkg/web/service/{auth,service,store_sqlite,node_config}.go`。不调用宿主配置发现/加载函数，不构造 `FileConfigStore`，不使用 node/Static/Scans/Profile。

真实 `go list -deps -json ./cmd/wscyber-hub`：81 个可达外部模块；H1 原 61 个模块版本全部不变，下列新增 20 个固定于 go.mod/go.sum。没有执行全上游/全模块图下载或测试；两个 fork replace 仍无可达包，未初始化 UI/templates 子模块。

| 新增实际可达模块 | 固定版本 | 用途 / 根许可文本 |
|---|---|---|
| `connectrpc.com/connect` | `v1.21.1` | 原生 typed routes / 生成测试客户端；Apache-2.0 |
| `github.com/uptrace/bun` | `v1.3.0` | 原生 SQLiteStore ORM；BSD-2-Clause |
| `github.com/uptrace/bun/dialect/sqlitedialect` | `v1.3.0` | 原生 SQLite 方言；BSD-2-Clause |
| `modernc.org/sqlite` | `v1.60.1` | 原生纯 Go SQLite driver；BSD-3-Clause，另有 SQLite public-domain、sqlite-vec MIT 与第三方通知 |
| `modernc.org/libc` | `v1.77.1` | SQLite 传递依赖；BSD-3-Clause，另有第三方通知（含 musl MIT 等） |
| `modernc.org/mathutil` | `v1.7.1` | SQLite 传递依赖；BSD-3-Clause |
| `modernc.org/memory` | `v1.12.1` | SQLite 传递依赖；BSD-3-Clause，多个根通知及仅归属链接的 logo 文件 |
| `github.com/dustin/go-humanize` | `v1.0.1` | SQLite 传递依赖；MIT |
| `github.com/google/uuid` | `v1.6.0` | SQLite 传递依赖；BSD-3-Clause |
| `github.com/remyoudompheng/bigfft` | `v0.0.0-20230129092748-24d4a6f8daec` | SQLite 传递依赖；BSD-3-Clause |
| `github.com/jinzhu/inflection` | `v1.0.0` | Bun 传递依赖；MIT |
| `github.com/puzpuzpuz/xsync/v3` | `v3.5.1` | Bun 传递依赖；Apache-2.0 |
| `github.com/tmthrgd/go-hex` | `v0.0.0-20190904060850-447a3041c3bc` | Bun 传递依赖；BSD-3-Clause |
| `github.com/vmihailenco/msgpack/v5` | `v5.4.1` | Bun 传递依赖；BSD-2-Clause |
| `github.com/vmihailenco/tagparser/v2` | `v2.0.0` | Bun 传递依赖；BSD-2-Clause |
| `github.com/gorilla/websocket` | `v1.5.3` | 原生 AOP/AgentPool WebSocket；BSD-2-Clause，本轮未连接 node |
| `github.com/dop251/goja` | `v0.0.0-20260930195847-0f92c903ca4a` | Web Service 经 exts/jev 可达；MIT，本轮未执行 JEV |
| `github.com/go-sourcemap/sourcemap` | `v2.1.3+incompatible` | Goja 传递依赖；BSD-2-Clause |
| `github.com/google/pprof` | `v0.0.0-20260802141513-ef3492d7dac3` | Goja 引用 profile 包；根 Apache-2.0，不是新增 profiling 服务 |
| `github.com/santhosh-tekuri/jsonschema/v6` | `v6.0.3` | exts/jev JSON Schema；Apache-2.0 |

来源和根许可全文哈希在增量 JSON 中，公开模块来源链接使用 `pkg.go.dev/<module>@<固定版本>`。SQLite/libc 的 `LICENSE-3RD-PARTY.md` 已核对汇总并记录哈希，**不把它们的全模块图清单误当本入口全部链接内容，也不称逐文件组合许可审查完成**。`modernc.org/memory/LICENSE-LOGO` 只有外部归属链接，明确保留 UNKNOWN；本轮未使用/发布 logo。sqlite-vec/vfs 未被此入口导入，不据打包许可推断功能已采用。

H1 四个模块的缺根许可文本 UNKNOWN、AGPL/GPL 组合及对应源代码/通知义务仍然存在；H2-A 不消除这些缺口，未部署或分发。固定新模块根许可证存在不等于所有嵌套代码/资产均已审查；后续网络部署/分发仍须完成相应核验。

H2-A 的 build、4 项定向测试、编译 CLI 的 HTTP/原生 JSON API 与 SIGTERM/关闭验证见 HUB.md（见 tag legacy-snapshot）。空库重开不等于执行历史恢复，Web 空 Hub 不等于 H1 工具执行已接入。

## H2-B：原生节点/持久控制台的可达依赖增量

同一固定 Cyber/AOP 版本，直接复用 `pkg/node.RunWebSocket`；其原生持久 REPL 使 console/readline/补全相关模块实际可达。唯一 Hub 入口 `go list -deps -json` 得到 **93 个模块：原81个版本全不变，新增12个**。未改上游、未改 fork replace、未采用扫描/UI子模块。版本、模块公开来源和根文本SHA-256见 [node-dependency-licenses.json](node-dependency-licenses.json)。

| 新增可达模块 | 固定版本 | 模块根实际文本 |
|---|---|---|
| `github.com/chainreactors/tui/console` | `v0.0.0-20260712082522-2ba36ad7841f` | Apache-2.0 |
| `github.com/chainreactors/tui/readline` | `v0.0.0-20261010062516-bb32632d1a4e` | Apache-2.0 |
| `github.com/carapace-sh/carapace` | `v1.16.3` | Apache-2.0根文本 **及额外 LICENSE.appendix，见下文** |
| `github.com/carapace-sh/carapace-shlex` | `v1.1.1` | Apache-2.0 |
| `github.com/atotto/clipboard` | `v0.1.4` | BSD-3-Clause；可达不等于本片调用剪贴板 |
| `github.com/charmbracelet/bubbles` | `v1.0.0` | MIT |
| `github.com/charmbracelet/bubbletea` | `v1.3.10` | MIT |
| `github.com/kballard/go-shellquote` | `v0.0.0-20180428030007-95032a82bc51` | MIT |
| `github.com/muesli/ansi` | `v0.0.0-20230316100256-276c6243b2f6` | MIT |
| `github.com/muesli/cancelreader` | `v0.2.2` | MIT |
| `github.com/sahilm/fuzzy` | `v0.1.1` | MIT |
| `golang.org/x/exp` | `v0.0.0-20260529124908-c761662dc8c9` | BSD-3-Clause |

**新增待核实事项：** carapace `LICENSE.appendix` 原文为 “For sustainability reasons, the features `Uid`, `Query`, and `Trace` are reserved for Tabdance™.” 已保存此文件哈希，附加声明的含义/适用范围保持 UNKNOWN；不能只看 LICENSE.txt 就宣称整个模块纯 Apache-2.0、分发/部署许可已解决。本片只做获准本机验证，没有主动启用这些特性；未逐文件核实其内部可达性、声明适用范围或完整组合义务。

已有四个缺根文本 UNKNOWN、Cyber/AOP AGPL、proxyclient GPL、源代码与通知义务继续存在；本增量不作法律结论或可部署声明。新模块根文本存在也不代表嵌套代码/资产/商标全审查完成。

真实链路/净环境CLI/14条原生持久事件/关闭与有限验收见 NODE.md（见 tag legacy-snapshot）。原生SQLite事件重开仅证明记录落盘可读，不是完整执行恢复；原生WebSocket回流不是生产UI验收。

## H3-A / H3-A2：固定原生UI与补充依赖（已验本机构建/资源HTTP）

本片没有新增/变更 Go 模块；静态边界只使用标准库及既有原生 host/extension/web。固定 root 同上，原生 cyber-ui gitlink 为 `147423bd42b665aab361409c57258d9370f75bb0`。实际固定归档/树/lock/已有 Wasm 哈希见 [ui/sources.lock.json](ui/sources.lock.json)，使用方式和未验证阶段见 [ui/README.md](ui/README.md)。

**cyber-ui 固定归档内未找到许可证声明，保持 UNKNOWN，不自动继承 Cyber 根 AGPL，不外推为整个上游无许可。** H3-A的4项缺依赖由H3-A2明确补充锁解决：4直接+18传递依赖匹配固定UI pnpm版本/integrity，原465非根项完整不变，实际锁488项。主在执行会话正常退出后安装437包、完整tsc/Vite及24项真实资源HTTP通过，原始锁/实际锁/覆盖分别记录。通知清单已生成，新增22包中21采得根部文本，react-arborist虽锁声明MIT但未采得根部通知，仍需补核。lock字段或通知采集均不等完整法律审查，不解决cyber-ui UNKNOWN或组合许可问题。详细来源、审源范围与部署/分发前待办见 [ui/LICENSES.md](ui/LICENSES.md)。所有既有 Go 许可缺口继续保留。

## B2：全工具出口（上游 tools/proxy）的可达依赖增量

核验日期：2026-11-18。同一固定 Cyber/AOP 版本；wscyber 自有出口扩展直接复用上游 `tools/proxy.NewProxyHub`（不使用 `exts/proxy`，不注册 `proxy/mitm` 命令），用户已同意引入此依赖。唯一 Hub 入口 `go list -deps` 得到 **112 个模块：原 93 个版本全不变，新增 19 个**；新增模块版本与 `go.sum` 校验和逐行与上游 f4b1fb2 的 `go.sum` 一致。版本、公开来源与根文本 SHA-256 见 [egress-dependency-licenses.json](egress-dependency-licenses.json)。模块获取：指定代理 `192.168.1.2:9567` 本次不可达（no route to host），改为直连公共 Go 模块代理并经 sum.golang.org 校验；未改上游、未改 fork replace。

| 新增可达模块 | 固定版本 | 用途 / 模块根实际文本 |
|---|---|---|
| `github.com/chainreactors/utils/mitmproxy` | `v0.0.0-20261002195803-fc9e07c4b4fd` | MITM 代理引擎（监听、CONNECT/MITM、入口鉴权）；**未找到许可文件，UNKNOWN** |
| `github.com/chainreactors/utils/cert` | `v0.0.0-20260722180147-5b1816060721` | mitmproxy 自签 CA/证书签发；**未找到许可文件，UNKNOWN** |
| `github.com/chainreactors/proxyclient/extra` | `v0.0.0-20260527160727-36cf133952c3` | 上游链/订阅协议（clash/anytls/hysteria2/trojan/vmess）可达；B2 用直连上游；GNU GPL v3 |
| `github.com/sagernet/sing` | `v0.7.6` | proxyclient/extra 传递；GNU GPL v3 或更新版 |
| `github.com/sagernet/sing-vmess` | `v0.2.7` | proxyclient/extra 传递；GNU GPL v3 或更新版 |
| `github.com/anytls/sing-anytls` | `v0.0.11` | proxyclient/extra 传递；GNU GPL v3 或更新版 |
| `github.com/apernet/hysteria/core/v2` | `v2.9.2` | proxyclient/extra 传递；MIT |
| `github.com/apernet/quic-go` | `v0.59.1-0.20260425001925-6c6cc9bcb716` | hysteria 传递；MIT |
| `github.com/quic-go/qpack` | `v0.6.0` | quic-go 传递；MIT |
| `github.com/sirupsen/logrus` | `v1.9.4` | mitmproxy 日志；MIT |
| `github.com/klauspost/compress` | `v1.18.6` | mitmproxy 解码；BSD-3-Clause 根文本，附 Apache-2.0/MIT 段 |
| `github.com/andybalholm/brotli` | `v1.2.1` | mitmproxy 解码；MIT |
| `github.com/satori/go.uuid` | `v1.2.0` | mitmproxy flow id；MIT |
| `github.com/gofrs/uuid/v5` | `v5.3.2` | 传递；MIT |
| `github.com/golang/groupcache` | `v0.0.0-20241129210726-2c02b8208cf8` | 传递；Apache-2.0 |
| `go.uber.org/atomic` | `v1.11.0` | 传递；MIT |
| `github.com/stretchr/testify` | `v1.12.1` | hysteria client 导入 testify/mock 而可达；MIT |
| `github.com/stretchr/objx` | `v0.5.3` | testify/mock 传递；MIT |
| `go.yaml.in/yaml/v3` | `v3.0.5` | testify 传递；MIT + Apache-2.0（另有 NOTICE） |

**新增待核实事项：** mitmproxy 与 cert 两个模块根无许可文本，保持 UNKNOWN，不按其可能的派生来源推断。新增四个 GPL-3.0 文本模块与既有 Cyber AGPL、proxyclient GPL 的组合及源代码/通知义务未处理；本增量不作法律结论或可部署声明，部署/分发前须核对。模块根文本存在不代表嵌套代码/资产已审查。

## D1：证据图片（golang.org/x/image）的可达依赖增量

核验日期：2026-11-19。同一固定 Cyber/AOP 版本；`webapi/evidence_image.go` 在服务端把已保存的 traffic.Flow 渲染成 PNG（不经模型）。唯一 Hub 入口 `go list -deps` 得到 **113 个模块：原 112 个版本全不变，新增 1 个**。版本、公开来源与根文本 SHA-256 见 [evidence-image-dependency-licenses.json](evidence-image-dependency-licenses.json)。固定 Cyber 根模块 `go.mod` 已要求 `golang.org/x/image v0.47.0`，最小版本选择据此取 v0.47.0（本机缓存原有 v0.38.0 不能被选中），经直连 proxy.golang.org 获取并由 `go.sum` 校验；未改上游、未改 fork replace。

| 新增可达模块 | 固定版本 | 用途 / 模块根实际文本 |
|---|---|---|
| `golang.org/x/image` | `v0.47.0` | `font/opentype`、`font/sfnt` 字体解析，内嵌 `font/gofont/gomono`、`gomonobold` 等宽字体，`math/fixed`；BSD-3-Clause 根文本 + Go PATENTS；Go 字体 README 为同款 BSD 式许可 |

CJK 回退字体只在运行时从系统读取（`WSCYBER_EVIDENCE_FONT` → `/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf` → `/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc` → `/usr/share/fonts/truetype/wqy/wqy-microhei.ttc`），不打包进仓库或二进制；本机为 Debian `fonts-droid-fallback`（Apache-2.0）。部署机器的字体由其系统包授权；没有可用回退字体时非 ASCII 字符画方框并在图片页脚说明。既有许可缺口不因本增量改变。
