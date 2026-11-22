# 浏览器与爬虫（M5c）

研究会话（及验证、复测、报告会话）可在终端里使用上游浏览器命令 `playwright`（go-rod 驱动本机 Chromium）与爬虫命令 `katana`。它们的全部 HTTP/HTTPS 流量经全工具出口（B2，见 `POLICY.md`）并带**本次调用**的会话归属，按任务测试策略放行/拒绝，记入 `webapi_egress_flows`，`tool` 字段为 `browser` / `katana`（终端仍为 `terminal`）。不改表。实现：`webapi/browser.go`、`webapi/browser_relay.go`；两种模型节点（`model_product.go`、`model_node.go`）都装 `Capability.BrowserExtension(工作区)`，装在 `EgressExtension` 之后。fixture 节点不装。

## 为什么不用上游扩展原样装配

固定上游 f4b1fb2 的事实：

- `exts/browser/extension.go:49` 只 `playwright.New(workDir)`，从不调用 `SetProxy`；`tools/playwright/browser.go:590-616` 启动 Chrome 时只有 `proxyURL` 非空才加 `--proxy-server`，所以原样装配时**浏览器直连，不经出口**。Chrome 共享一个进程（`browser.go:574` `getOrLaunchBrowser`），所有会话共用。
- 模型可传参改变出口或接管浏览器：`open --proxy-server`（`session.go:877` → `SetProxy`，`session.go:249`）、`open --cdp`/`--headed`（`session.go:899-906` → `SetBrowserMode`，`browser.go:74`）、`attach --cdp`（`session.go:618-660`）。
- `exts/katana/extension.go:42` 把 `endpoint.ProxyURL()`（不含令牌的基础地址）作启动默认值；`tools/katana/katana.go:154` 用 `ResolveExecutionEgress(execution, c.Proxy)`，调用级令牌只来自 `execution.Env` 里的代理变量——即终端进程注入的那个（`tools/terminal/process.go:76-83`，令牌登记为 `terminal`）。katana 显式 `-proxy` 优先（`katana.go:155`），`-chrome-ws-url`/`-system-chrome-path`/`-headless-options` 可接外部浏览器或改 Chrome 参数（`katana.go` readFlags）。
- Chrome 的 `--proxy-server` 不接受 URL 里的用户名口令，无法直接带 `cyber-route-*` 令牌。

## 方案

**命令注册**：本项目自己注册 `playwright` 与 `katana` 两个命令（同名、同 Usage、同上游实现），Run 由包装层执行，不改上游。上游 `exts/browser`、`exts/katana` 不装。

**浏览器（playwright）**：
- 每个 Agent 会话一个独立 `browserSession`：独立 Chromium 进程 + 独立本机中继 + 独立上游 `playwright.Command`。不同会话之间不共享页面、Cookie、出口归属；会话结束（`SessionEnd`）时关闭。
- 中继 `egressRelay`：只监听 `127.0.0.1:随机端口` 的无认证 HTTP 代理，Chrome 以 `--proxy-server=中继` 启动；中继转发到 ProxyHub 时补 `Proxy-Authorization: Basic <本次调用令牌>`（CONNECT 原样转发后双向透传；明文 HTTP 经带令牌的 Transport 转发）。中继**不做策略判断**，放行/拒绝与记录仍全部在 ProxyHub 鉴权回调和 flow 观察里。选它而非 CDP `Fetch.authRequired`：后者要接管上游 Command 的请求拦截（与 `route`/`network` 子命令冲突），且对 CONNECT 隧道不可靠。
- 每次 `playwright` 调用：`egressFor(ctx,"browser")` 取上游令牌并登记 session/turn/call → 中继激活该令牌 → 必要时启动 Chromium（无头，本包 go-rod launcher 启动）并让上游 Command 经 `SetBrowserMode(false, 本机 CDP 地址)` 连接 → 执行上游 `Run` → 调用结束时中继关闭本令牌下的全部隧道与在途转发、上游释放令牌（等在途 flow 写完）。下一次调用里 Chrome 必须重新建连、重新鉴权，归属到新调用。
- **两次调用之间**页面发出的后台请求（定时轮询、延迟加载等）：中继没有激活令牌，直接回 403、**不发送**，按“方法+URL”在一段空闲期内去重后记一条 `denied`（`tool=browser`、归属该会话所属任务、`turn_id/call_id` 空，原因“浏览器后台请求：当前没有进行中的 playwright 调用，中继未发送”）。
- 同一会话内并发两个 `playwright` 调用时，新连接用最后激活的令牌，先结束的调用会切断自己名下的连接；归属在同会话内可能落到另一个并发调用上（任务/会话不变）。
- `kill-all` 后本包同时结束 Chromium；浏览器崩溃或被关闭时下次调用自动重启。

**爬虫（katana）**：
- 每次调用 `egressFor(ctx,"katana")`；调用参数末尾追加 `-proxy=<带令牌地址> -show-browser=false -kb-validate-secrets=false`（显式参数优先于 katana 配置文件），执行环境里的代理/CA 变量替换为本次调用的。
- 普通（standard）模式追加 `-headless=false -hybrid=false -chrome-ws-url=`；katana 的 HTTP 客户端用 `transport.Proxy`，经 ProxyHub。
- `-headless`/`-hybrid`：本包为本次调用启动一个 Chromium（同样经一个新中继），以 `-chrome-ws-url` 交给 katana，调用结束即关闭。
- katana 依赖的 goflags 会在用户配置目录写默认配置：Hub 模型节点启动时若未设 `XDG_CONFIG_HOME`，设为 `state-dir/tool-config`（0700）。

**Chromium**：`tools/headless.Discover()`：环境变量 `CYBER_BROWSER_PATH` → 系统查找（`/usr/bin/chromium` 等）；找不到时命令返回明确错误，**不交给 go-rod 下载**。启动参数：无头、`no-sandbox`、`ignore-certificate-errors`（MITM 证书）、`--proxy-server=中继`、`--proxy-bypass-list=<-loopback>`（localhost 也走代理）、`disable-quic`、`dns-prefetch-disable`、`no-pings`、`disable-background-networking`、`disable-component-update`、`disable-domain-reliability`、`disable-sync`、`disable-extensions`、`disable-field-trial-config`、WebRTC `disable_non_proxied_udp`、关闭优化提示/翻译/媒体路由/自动填充服务器/网络授时/安全浏览等功能，Google 账号/搜索/变体服务地址指向 `http://127.0.0.1:1`。

## 模型不能绕过

包装层在执行前拒绝（返回错误，“wscyber 出口策略”）：

- playwright：`--proxy-server`、`--proxy-bypass`、`--cdp`、`--cdp-url`、`--headed`（任意位置、含 `=` 形式），子命令 `attach`。
- katana：`-proxy`、`-chrome-ws-url`/`-cwu`、`-system-chrome-path`/`-scp`、`-show-browser`/`-sb`、`-headless-options`/`-ho`、`-kb-validate-secrets`（会向第三方验证密钥，未核实经出口）。

## 审批（规则在 `webapi/command_rules.go`，与 bash 同一机制）

- `approval=all`：经 bash 的任何命令都挂起，含 `playwright`/`katana`。
- `high_risk`：
  - `playwright` 动作类子命令（`open goto content screenshot pdf network tab-new click dblclick tap press type fill select-option check uncheck dispatch-event autofill set-input-files upload evaluate set-extra-headers template`）的任一参数（选择器、URL、填入值、脚本、头 JSON）命中现有破坏性信号（与 `webapi_http` 同一 `dangerousText`：delete/remove/destroy/drop/truncate/purge/erase/wipe/reset/revoke/shutdown/terminate、删除/清空/销毁/注销，含 camelCase 与 URL 编码）→ 审批。只读提取（inner-text、snapshot、url、cookie-list…）、无信号的导航/截图/点击不审批。
  - `katana` 带主动交互参数 → 审批：`-headless`/`-hl`（会点击页面按钮，含提交按钮）、`-hybrid`/`-hh`（点击 onclick 链接）、`-automatic-form-fill`/`-aff`（自动填表并提交）、`-auto-login`/`-al`。普通只读爬取不审批。
  - 两者参数含动态展开（`$VAR`、`$(…)`）时无法静态核对，也要求审批；命令边界（`command.before`，注册命令会经过）对未批准的命中再兜底拒绝。
- 子 Agent `oracle_explorer` 白名单不含终端，因而不能用这两个命令（未改）。

## 已知限制

- 仍是代理强制：浏览器页面 JS 不能改代理，但 Chromium 自身仍可能有不经代理的连接——DNS 解析（Chrome 在代理模式下通常不本地解析，但未逐项验证）、WebRTC（已设 `disable_non_proxied_udp`）、QUIC（已禁用）。WebSocket 经 CONNECT 走代理，但 ProxyHub 对 WebSocket 不记内层帧；`ws://` 升级请求本身是否单独记录以上游 mitmproxy 为准（未专门测试）。
- 已建立的 CONNECT 隧道内的请求不再复核（同 B2）；调用结束时中继会关闭隧道，所以隧道寿命不超过一次调用。
- 上游 playwright 的 `route --fulfill`、`state-load`、`set-input-files` 等读写工作区文件；路径相对节点工作区，不额外限制（与终端相同）。
- 后台联网降噪不是零后台请求保证。Chromium 145.0.7632.159 的本机静态页测试（打开页面、读取标题、等待 3 秒）原有 4 条非目标出口记录；netlog 显示其中 3 条 `www.google.com:443` 是 `is_preconnect=true` 的搜索预连接，新增 `disable-features=PreconnectToSearch` 后这 3 条消失，标题读取正常。`disable-background-networking` 原已由 go-rod 默认启用，现显式保留，不能把它算作新增修复。
- 同一测试仍有 1 条 `android.clients.google.com:443`，netlog 为 `POST https://android.clients.google.com/checkin`、traffic annotation hash `65957842`、initiator `not an origin`，符合 GCM checkin；未定位其浏览器服务层的具体触发者，也未将 hash 映射为官方 annotation 名。回归断言非目标记录 ≤1、只能是该 host 且全部 `denied`。未为压到 0 而改写推送服务端点或禁用页面推送能力；更长时间/其他 Chromium 版本的后台请求数不由此测试保证。调用内后台请求仍按任务策略处理（未授权 origin 拒绝，`unrestricted` 或普通对话可放行），调用间则由中继直接拒绝并记录。
- ProxyHub 对每次拒绝在 Hub 标准错误打一条 logrus 日志（同 B2）；浏览器的后台请求会让该日志变多。
- 浏览器会话内存在于节点进程：节点重启后会话页面丢失。
- 每个会话一个 Chromium 进程，资源随并发会话数增长；本片未加上限（会话结束即释放）。
