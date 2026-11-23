# 部署与更新：设计（D0）

状态：设计稿（2026-11-22）。只写方案，不含实现。用户要求（11-22）：部署、更新要清晰快捷；**Docker 镜像**（`docker pull` 即可运行，含 Chromium，附 compose）与**单个二进制**（前端 embed，给连接串即可本地运行；systemd/安装脚本只是可选便利层）两种都提供；生产服务器可访问镜像仓库与外网。数据库按 `native/POSTGRES.md`（外部连接串、启动自动迁移、库较新拒绝启动、无 down 迁移、升级前 `pg_dump`）。

上游路径相对固定版本 `cyber-harness@f4b1fb2f`（模块缓存 `~/go/pkg/mod/github.com/chainreactors/cyber-harness@v0.0.0-20261109041610-f4b1fb2f3315`）；本项目路径相对仓库根，行号以 HEAD `a2e8060` 为准。

实施补记（2026-11-23）：D1 与 P1–P4 已完成，Hub 已迁 PG，必须配置连接串；构建/存储相关段落已校正。其余部署、更新、登录条目仍为设计，当前可运行用法以 `RUNNING.md` 为准。

本文 §1 是调查事实，§2–§9 是设计，§10 是给运维看的操作手册草案（实现后随片校正），§11–§13 是衔接、拆片与待决。

---

## 1. 调查结论（事实）

### 1.1 现有构建

| 事实 | 位置 |
|---|---|
| Hub 必须经 overlay 脚本构建（应用 PostgreSQL 存储 overlay）；RUNNING 的命令带 `GOTOOLCHAIN=local GOPROXY=off`，即**假定模块缓存已热** | `native/RUNNING.md:5-13` |
| 脚本通过 `go env GOMODCACHE` 定位固定上游源（P2 已实现，支持自定义模块缓存） | `scripts/cyber-go-overlay.py:10,25-28` |
| 复制只读副本到 `var/cyber-overlay/module-current/`，持 `var/heavy.lock`，派生 modfile `-replace` 指向副本，`go build -mod=readonly -modfile -overlay` | `cyber-go-overlay.py:32-54` |
| P2 已实现“清单驱动整文件替换”，上游源和派生副本均作哈希校验 | `native/POSTGRES.md` §2 |
| Go 1.26（go.mod `go 1.26.0`，本机 go1.26.4）；模块来自公开代理并校验 `go.sum`；本机 `proxy.golang.org` 直连不可达，M5c 用 `goproxy.cn`（`GOSUMDB` 仍为 sum.golang.org） | `native/go.mod:3`、`native/DEPENDENCIES.md:20,173` |
| 现有二进制：`CGO_ENABLED=1` 动态链接 glibc（`ldd`：libc.so.6）；依赖闭包内用 cgo 的只有 `runtime/cgo`、`net`、`os/user`（`go list -deps` 实测），即 `CGO_ENABLED=0` 可得静态二进制（待实施验证 DNS/用户查询行为） | 本机 `var/wscyber-hub`、`go version -m` |
| 体积约 124 MB（未 strip，含 debug_info） | `DEPENDENCIES.md:189` |
| 前端：`cyber-ui-build.py prepare` 从 `codeload.github.com` 下载两份固定归档（root 选定路径树哈希、cyber-ui 归档 SHA256 校验），下载**显式不走代理**（`ProxyHandler({})`），禁止重定向 | `scripts/cyber-ui-build.py:126-139,316-345`、`native/ui/sources.lock.json` |
| `prepare` 的 root **公网归档分支从未实际运行过**（本机一直用 `--root-repo`） | `native/ui/README.md`“固定源与准备” |
| `build` = `npm ci --ignore-scripts` + tsc + Vite；子进程 `PATH` 固定 `/usr/bin:/bin`，registry 固定 npmjs；要求 node **恰为** `v24.16.0`、npm **恰为** `11.16.0` | `cyber-ui-build.py:80,90,600-601,657-666`、`sources.lock.json` `toolchain` |
| 工作区/输出必须在仓库 `var/` 下 | `cyber-ui-build.py:576-581,783` |
| 产物 `release/public/`（index + assets + `.wscyber-ui.json` 清单），通知/来源原件在 public 外的 `release/{notices,provenance,dependency-notices.json,build.json}` | `cyber-ui-build.py:690-772` |
| Hub 对 UI 清单做固定来源校验（root/ui commit、原锁/实际锁/覆盖哈希、逐文件 SHA256、无链接/硬链接/多余文件、key/state 不得在 UI 根内） | `native/cmd/wscyber-hub/ui.go:24-31,73-214` |
| 上游自身就把前端 `//go:embed all:static` 进二进制（`cmd/cyber-web` 用 `fs.Sub(webstatic.FS,"static")`），版本用 `var Version = "dev"` + ldflags | 上游 `web/embed.go:5`、`cmd/cyber-web/main.go:88`、`pkg/config/options.go:12`、`Makefile:53` |
| 演示栈启动 Hub 的参数全集：`--local-hub --model-node --model-endpoint <模拟模型> --listen 127.0.0.1:42460 --access-key-file --state-dir <新目录> --ui-dir`，环境 `PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 CYBER_BROWSER_PATH=/usr/bin/chromium`；就绪靠读 stdout 的 `"event":"ready"` | `scripts/demo-stack.py:1079-1095` |
| 版本：**没有任何版本号注入**；Go 自动写入 `vcs.revision/vcs.time/vcs.modified`（`go version -m`）；git 标签只有 `legacy-snapshot`；仓库无 `.github/`，无 Dockerfile | `git tag -l`、`go version -m var/wscyber-hub` |

### 1.2 Hub 运行形态

| 事实 | 位置 |
|---|---|
| 只有旗标、无子命令，拒绝位置参数：`--local-hub`（必需）、`--fixture-node`、`--model-config`、`--model-node`、`--model-endpoint`、`--listen`（默认 `127.0.0.1:0`）、`--access-key-file`、`--state-dir`、`--ui-dir` | `native/cmd/wscyber-hub/main.go:53-61,70-72` |
| 模式：`--model-node` 且无 `--model-endpoint` = **正式模型节点**（`serveProductModelNode`）；有 `--model-endpoint` = 本机受控验收分支（演示）；`--model-config` 只管理配置；`--fixture-node` 固定工具；都不带 = 空 Hub | `main.go:182-202` |
| `--listen` **只允许 loopback** | `main.go:117-120` |
| 访问密钥文件：不跟随符号链接、普通文件、**权限必须恰为 0600**、≤4096 字节 | `main.go:205-227` |
| state-dir：正式模式 `MkdirAll`+`chmod 0700` 可重开；其它模式要求新目录 | `main.go:143-153` |
| state-dir 只保存运行文件，业务数据在 PostgreSQL。文件包括：`webapi-secrets.key`（测试账号/Webhook 加密密钥，**缺失则已存密文无法解密、不会重新生成**）、`egress-ca/`（出口 MITM CA，0700，缺失时上游 ProxyHub 生成）、`model-config/cyber.yaml`（模型配置，含 API key）、`tool-config/`（katana goflags 配置，未设 `XDG_CONFIG_HOME` 时）、`model-node-*`（节点工作区，正常停止时删除，崩溃会残留） | `main.go:161`、`webapi/asset_secret.go:18-27`、`webapi/egress.go:35,380-395`、`model_config.go:29-40`、`webapi/browser.go:452-461`、`model_product.go:219-230` |
| 上游 logger 一律 `NopLogger`（上游会把 AccessKey 打到 Infof） | `main.go:154-158`、上游 `pkg/web/host/host.go:64` |
| 日志：stdout 是 JSON 行事件 `ready`（含 address、frontend、模式字段）→ 停止时 `model-node-closed` → `stopped`；错误到 stderr `wscyber-hub: …` 且 exit 1；ProxyHub 每次拒绝向 stderr 打一条 logrus | `main.go:43-46,85-104`、`native/BROWSER.md:57` |
| 健康检查：上游 `GET /health` 返回 `{"status":"ok"}`，**不需鉴权**，只表示进程在监听；`/api/auth/{session,login,logout}` 也免鉴权；其余 `/api/` 需密钥 | 上游 `pkg/web/handler.go:32-35`、`pkg/web/service/auth.go:45-66` |
| 优雅停止：SIGINT/SIGTERM → 上游 `ServeHTTP` 5 s 清理 + 必要时再 5 s → 关闭 Profile、删节点工作区 | `main.go:41`、上游 `pkg/web/http.go:23-38`、`model_product.go:260-285` |
| **上游 `host.Serve` 自己 `net.Listen`**，不能注入 listener → 内置 TLS 需上游补丁 | 上游 `host.go:49` |
| Cookie `Secure` 取决于 `r.TLS` 或 `X-Forwarded-Proto: https`；WebSocket upgrader 无 allowedOrigins 时用 gorilla 默认同源检查 → 反向代理须保留 `Host` 并传 `X-Forwarded-Proto` | 上游 `auth.go:91-98,158-163`、`service/agents.go:593-596` |
| 进程内节点用 `http://<key>@<监听地址>` 回连 Hub；监听地址取 `listener.Addr()`，绑定 `0.0.0.0` 时为 `0.0.0.0:port` | `model_product.go:238`、上游 `host.go:61,71` |
| 出口代理 ProxyHub 监听 `127.0.0.1:0`（随机端口）；浏览器中继同为 loopback 随机端口 → **无需开放/映射任何额外端口** | 上游 `tools/proxy/hub.go:241`、`BROWSER.md:20` |
| **终端子进程继承 Hub 全部环境变量**（`os.Environ()`），且与 Hub 同 uid → 能读到环境里的连接串、能读 state-dir 与连接串文件（POLICY 已注明“没有 netns/iptables/独立用户”） | 上游 `tools/terminal/interpreter.go:38`、`native/POLICY.md:37` |

### 1.3 运行依赖

| 依赖 | 事实 | 位置 |
|---|---|---|
| Chromium | `CYBER_BROWSER_PATH` → go-rod `LookPath`（`/usr/bin/chromium` 等）；找不到时 `playwright`/`katana -headless` 返回“未找到 Chromium…（平台不会自动下载浏览器）”，**其它功能正常**；启动参数已含 `no-sandbox`、`disable-dev-shm-usage`（容器内无需 root/特权）；go-rod 在 `os.TempDir()` 建 user-data-dir | `webapi/browser.go:48-60,76`、`BROWSER.md:32` |
| katana | Go 库内置（`chainreactors/katana` fork），**不是外部程序**；只需可写配置目录 | `DEPENDENCIES.md:178`、`browser.go:452-461` |
| 终端工具 | 上游 bash 是 mvdan 解释器，外部命令按 `PATH` 执行宿主程序 → 镜像里装了什么，模型终端就能用什么（curl 等） | 上游 `tools/terminal/interpreter.go:36-60` |
| 证据图 CJK 字体（可选） | `WSCYBER_EVIDENCE_FONT` → `/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf` → Noto CJK → wqy；缺失时非 ASCII 画方框 | `webapi/evidence_image.go:52-60,152-157`、`DEPENDENCIES.md:167` |
| 时区 | 用量统计按进程本地时区（`TZ`/`/etc/localtime`） | `webapi/usage.go:119-134` |
| 模型网关 | 模型配置在页面保存到 state-dir；Hub 进程直连模型网关（出网 HTTPS） | `RUNNING.md:38`、`model_config.go:29-40` |
| PostgreSQL | 外部连接串；角色非超级用户、无 BYPASSRLS（启动校验）；单实例 advisory lock | `POSTGRES.md` §1、§4.3 |
| `pg_dump`/`pg_restore` | 备份用；pg_dump **主版本须 ≥ 服务端**；本机 18.3 | 本机 `pg_dump --version` |

### 1.4 许可对分发的影响（只列事实，不下法律结论）

`native/DEPENDENCIES.md` 与 `native/*-dependency-licenses.json`、`native/ui/LICENSES.md` 已记录；**分发镜像/安装包 = 把下列组件的编译产物交给他人**，DEPENDENCIES 多处写明“分发或网络部署前须核对”。

| 类别 | 组件 | 分发时须面对的事项 |
|---|---|---|
| AGPL-3.0 文本 | Cyber、AOP（上游根 LICENSE sha256 `8d56b405…`） | AGPL 含网络交互条款；组合作品源代码提供义务未处理（`DEPENDENCIES.md:36,47`） |
| GPL-3.0 文本 | `chainreactors/proxyclient`、`proxyclient/extra`、`sagernet/sing`、`sing-vmess`、`anytls/sing-anytls` | 二进制分发通常伴随源代码/许可文本义务，未处理（`DEPENDENCIES.md:30,139-142,157`） |
| MPL-2.0（文件级） | `hashicorp/golang-lru/v2`、`hashicorp/go-version`、`projectdiscovery/retryablehttp-go` | 文件级 copyleft（`DEPENDENCIES.md:191`） |
| **UNKNOWN**（根无许可文本） | `chainreactors/logs`、`utils`、`utils/parsers`、`utils/proc`、`utils/mitmproxy`、`utils/cert`、`neutron`；cyber-ui 归档；`react-arborist` 未采得通知；carapace `LICENSE.appendix` 含义 UNKNOWN | 无法确认是否允许再分发（`DEPENDENCIES.md:40-47,119,137-138,180`、`ui/README.md`） |
| 镜像额外内容 | Debian 包：Chromium（BSD-3 系 + 大量第三方）、PostgreSQL 客户端（PostgreSQL License）、python3（PSF）、tini（MIT）、字体（Apache-2.0）等 | 由各包 `/usr/share/doc/*/copyright` 承载（Debian slim 保留 copyright 文件），镜像不得删这些文件 |

结论只到工程层：发布物**随包附带全部许可清单与通知**（§8.3）；“能否公开分发”取决于用户/法务对上表的核对，与镜像仓库可见性绑定（待决 §13-1）。

---

## 2. 总体方案

- **一个二进制，两种封装**：同一次构建产出同一个静态 `wscyber-hub`（前端已 embed）；tar 包直接装它，镜像也 `COPY` 它（不在镜像构建里再编译）。二者 sha256 相同，问题可互相复现。
- **新增子命令**，旧旗标保持不变供测试/演示：

  | 命令 | 作用 | 片 |
  |---|---|---|
  | `wscyber-hub serve [--listen ADDR] [--state-dir DIR] [--database-url-file F] [--access-key-file F] [--ui-dir DIR]` | 正式运行（= 现 `--local-hub --model-node` 正式分支），embed 前端 | D1（DB 旗标 P3 接） |
  | `wscyber-hub version [--json]` | 版本、提交、上游提交、UI 清单哈希、schema 版本、Go 版本 | D1 |
  | `wscyber-hub healthcheck [--url U]` | 访问本机 `/readyz`（P3 前 `/health`），0/1 退出码；镜像 HEALTHCHECK 用，不依赖 curl | D1 |
  | `wscyber-hub db status [--json]` / `db migrate` / `db backup [--out F]` | 只读查看库版本/服务端版本/待执行迁移；只迁移不启动；调用 `pg_dump` 备份 | D3 |
  | `wscyber-hub user add --admin --username U [--password-file F]` | 首个管理员（POSTGRES P6a） | P6a |
  | `wscyber-hub self-update [--version V]` | 单二进制原地更新（下载→校验→备份→原子替换） | D3 |

  无子命令（首参以 `-` 开头）时走现有旗标解析，`demo-stack.py`、测试、`--model-endpoint` 验收分支完全不变。
- **配置来源**：旗标 > 环境变量 > 默认值。环境变量：`WSCYBER_LISTEN`、`WSCYBER_STATE_DIR`、`WSCYBER_DATABASE_URL_FILE`、`WSCYBER_DATABASE_URL`（POSTGRES §1）、`WSCYBER_ACCESS_KEY_FILE`、`CYBER_BROWSER_PATH`、`TZ`。**Hub 读完 `WSCYBER_DATABASE_URL` 后立即 `os.Unsetenv`**（终端子进程继承环境，§1.2 末行），文档仍推荐文件方式。
- **默认值（`serve`）**：`--listen 127.0.0.1:8080`；`--state-dir` 默认 `${XDG_DATA_HOME:-$HOME/.local/share}/wscyber`（systemd/镜像显式给 `/var/lib/wscyber/state`）；`serve` 允许非 loopback 地址（旧旗标模式仍只许 loopback）。
- **访问密钥**：`serve` 未给 `--access-key-file` 时，首启在 `<state>/access.key` 生成 32 字节随机密钥（0600），之后复用；进程内节点用它回连。P6 前浏览器用它登录（ready 事件与 stderr 提示文件路径，不打印值）；P6 起浏览器改用户名口令，密钥只给节点/自动化。
- **密钥文件权限**：现要求恰为 0600（`main.go:216`）。`serve` 放宽为“属主可读且 group/other 无任何权限”（0600 或 0400），以兼容 systemd `LoadCredential`（0400）；仍拒绝符号链接与特殊文件。连接串文件同规则（P1 的解析函数按此实现或由 D1 调整，需与 P1 对齐）。
- **监听 `0.0.0.0`/`[::]` 时**，进程内节点回连地址改用 `127.0.0.1:<port>`/`[::1]:<port>`（`model_product.go:238` 处映射），不依赖“连 0.0.0.0 等于本机”的平台行为。
- **单实例**：同一数据库只允许一个 Hub（advisory lock，POSTGRES §1）；compose 不得 `scale`，systemd 只一个单元。

---

## 3. 版本号与构建信息

- **规则**：SemVer 标签 `vMAJOR.MINOR.PATCH`（预发布 `-rc.N`），从首个可部署版本 `v0.1.0` 开始（建议在 P6 登录完成后打首个正式标签；之前可打 `v0.1.0-rc.N` 供本机验证）。
  - **含数据库迁移的版本至少升 MINOR**；PATCH 版本**不得含迁移** → PATCH 之间回滚只需换二进制。
  - 发版说明写明“含迁移：是/否、schema 版本 N”。
- **注入**：新包 `native/internal/buildinfo`，`var Version, Commit, BuildTime string`，构建时 `-ldflags "-s -w -X wscyber/native/internal/buildinfo.Version=v0.1.0 -X ….Commit=<git sha> -X ….BuildTime=<SOURCE_DATE_EPOCH 对应 UTC>"`；未注入时 `Version="dev"`，Commit 回落 `debug.ReadBuildInfo()` 的 `vcs.revision`（+`-dirty`）。上游提交、UI 清单哈希（embed 后可算）、schema 版本（P1 迁移 embed 的最大版本）由代码常量/运行时计算，不靠 ldflags。
- **可见性**：
  - `wscyber-hub version`：`wscyber-hub v0.1.0 (commit abc1234, 2026-12-01T00:00:00Z, cyber f4b1fb2f, ui 147423bd, schema 2, go1.26.4)`；`--json` 同字段。
  - `ready` 事件新增 `version`、`browser`（Chromium 路径或空串）两个字段。
  - `GET /api/wscyber/version`（需鉴权，与其它 `/api/` 相同）返回同 JSON；前端在侧栏底部显示 `v0.1.0 · abc1234`（小片前端覆盖，ds-flash）。`/health`、`/readyz` 不暴露版本。
  - 镜像标签与 OCI label `org.opencontainers.image.version/revision/source/licenses`。

---

## 4. 前端 embed

- 新包 `native/internal/uiembed`：`//go:embed all:public`，`public/` 目录 git 忽略（只提交 `public/.placeholder`，使未放入前端时也能编译）。发布构建把 `release/public/` 整体复制进去后再 `go build`。
- `ui.go` 的校验逻辑改为基于 `fs.FS`：清单 schema/固定来源/锁/覆盖哈希/逐文件 SHA256/index 引用检查**原样保留**（`ui.go:128-214`）；embed.FS 天然无链接/硬链接，key/state 包含关系检查只对 `--ui-dir` 生效。
- 选择顺序：`--ui-dir`（开发预览/前端迭代仍用，`rebuild` 后不必重编 Hub）> embed 内容 > 无前端（`ready.frontend=false`，stderr 提示“本二进制未嵌入前端”）。embed 内容校验失败 = 启动失败（发布物损坏不应静默降级）。
- 不用 `go -overlay` 注入 embed 文件（overlay 对 embed 的支持不作依赖；复制进被忽略目录最直接）。

---

## 5. 健康检查与就绪

| 端点 | 鉴权 | 语义 | 片 |
|---|---|---|---|
| `GET /health`（上游） | 否 | 进程在监听 | 已有 |
| `GET /readyz`（新增，`webpkg.Route`，路径不在 `/api/` 下故免鉴权） | 否 | 数据库可 ping、迁移版本 = 本二进制期望版本 → 200 `{"status":"ready"}`；否则 503 `{"status":"not-ready","reason":"database"}`，不带细节 | D3（P3 后；P3 前只有 /health） |
| `wscyber-hub healthcheck` | — | 读 `WSCYBER_LISTEN`/`--url`，GET `/readyz`（404 时退回 `/health`），2 s 超时，0/1 | D1 |

节点是否就绪仍按原生握手核验（`main.go:89` 注释），不并入 readyz。

---

## 6. Docker 镜像

### 6.1 构建方式

镜像只做运行时封装（`native/deploy/docker/Dockerfile`，构建上下文 = `dist/<版本>/image-context/`，内含已构建的二进制、许可目录、SBOM），因此**不需要仓库根 `.dockerignore`**，也不在镜像里跑 Go/Node。

### 6.2 内容

- **基础镜像**：`debian:trixie-slim`（按 digest 固定）。理由：与本机已验证的 Debian 系 Chromium（`chromium 145.0.7632.159`）同源、apt 一条命令装齐、copyright 文件保留；distroless/alpine 装不了或不适配 Debian Chromium。
- **包**（`--no-install-recommends`）：`chromium`、`fonts-droid-fallback`（证据图 CJK 回退，匹配 `evidence_image.go` 默认路径）、`ca-certificates`、`tzdata`、`tini`、终端工具 `curl jq python3 openssl`（模型终端常用；增删不影响平台功能，随片可调）、`postgresql-client-18`（来自 PGDG apt 源；18 的 pg_dump 可备份 ≤18 的服务端。Debian trixie 官方源为 17，**实施时核实**）。
- **用户**：`wscyber`，固定 uid/gid `10001`，非 root 运行；`HOME=/var/lib/wscyber/home`。
- **路径**：二进制 `/usr/local/bin/wscyber-hub`；卷 `/var/lib/wscyber`（`state/`、`home/`）；许可 `/usr/share/wscyber/licenses/`、SBOM `/usr/share/wscyber/sbom.spdx.json`。
- **环境默认**：`WSCYBER_LISTEN=0.0.0.0:8080`、`WSCYBER_STATE_DIR=/var/lib/wscyber/state`、`CYBER_BROWSER_PATH=/usr/bin/chromium`、`TZ=UTC`。
- `ENTRYPOINT ["tini","--","wscyber-hub"]`、`CMD ["serve"]`（tini 回收 Chromium/终端孙进程）；`EXPOSE 8080`；`STOPSIGNAL SIGTERM`；`HEALTHCHECK --interval=30s --timeout=5s --start-period=60s CMD ["wscyber-hub","healthcheck"]`。
- 不默认 `read_only` 根文件系统（Chromium 写 `/tmp`、终端工具写工作区）；`/dev/shm` 不是必需（已 `disable-dev-shm-usage`），compose 仍给 `shm_size: 512m` 留余量。
- 平台：先只出 `linux/amd64`（依赖与许可只在 amd64 核验过）。

### 6.3 compose（`native/deploy/compose/compose.yaml`）

```yaml
name: wscyber
services:
  hub:
    image: ${WSCYBER_IMAGE:-<registry>/wscyber-hub}:${WSCYBER_VERSION:?在 .env 设置版本}
    restart: unless-stopped
    ports: ["${WSCYBER_PUBLISH:-127.0.0.1:8080}:8080"]
    environment:
      TZ: ${TZ:-Asia/Shanghai}
      WSCYBER_DATABASE_URL_FILE: /run/secrets/database_url
    secrets: [database_url]
    volumes:
      - state:/var/lib/wscyber
      - ./backups:/backups
    shm_size: 512m
    stop_grace_period: 30s
    logging: { driver: json-file, options: { max-size: 20m, max-file: "5" } }
    depends_on:
      postgres: { condition: service_healthy, required: false }   # 只在 trial profile 生效
  postgres:                     # 仅试用：docker compose --profile trial up -d
    profiles: ["trial"]
    image: postgres:18
    restart: unless-stopped
    environment:
      POSTGRES_PASSWORD_FILE: /run/secrets/pg_superuser_password
    secrets: [pg_superuser_password, wscyber_db_password]
    volumes:
      - pgdata:/var/lib/postgresql
      - ./initdb:/docker-entrypoint-initdb.d:ro     # 10-wscyber.sh：建非超级用户角色 wscyber 与库
    healthcheck: { test: ["CMD-SHELL","pg_isready -U postgres"], interval: 5s, retries: 20 }
secrets:
  database_url:          { file: ./secrets/database.url }
  pg_superuser_password: { file: ./secrets/pg_superuser_password }
  wscyber_db_password:   { file: ./secrets/wscyber_db_password }
volumes: { state: {}, pgdata: {} }
```

- 非 swarm 的 compose secrets 是只读绑定挂载，**保留宿主文件属主与权限**：`secrets/database.url` 须 `install -m 600 -o 10001 -g 10001`（`wscyber.sh init` 代做）。
- 生产用外部数据库：不启用 `trial`，`secrets/database.url` 写外部连接串（远程库建议 `sslmode=verify-full`）。
- 默认只发布到宿主 `127.0.0.1:8080`，由反向代理对外（§13-2）；要直接给内网访问时把 `WSCYBER_PUBLISH` 改成 `0.0.0.0:8080`。
- 随 compose 提供 `native/deploy/compose/wscyber.sh`：`init [--trial]`（生成 `.env`、`secrets/`、随机口令、正确属主）、`set-database-url`（交互写入连接串，不留 shell 历史）、`upgrade <版本>`、`rollback`、`backup`、`status`、`logs`（§9.1）。

---

## 7. 单二进制与安装包

### 7.1 最短路径（不装任何东西，首要路径）

解压即一个 `wscyber-hub`，`./wscyber-hub serve --database-url-file db.url` 就能跑（§10.2）。Chromium 只在用浏览器/爬虫命令时需要；`ready.browser` 为空时 stderr 提示一次“未找到 Chromium，playwright/katana -headless 不可用；安装系统 chromium 或设 CYBER_BROWSER_PATH”。

### 7.2 发布 tar 包 `wscyber-hub_<版本>_linux_amd64.tar.gz`

```
wscyber-hub                 # 静态二进制（CGO_ENABLED=0，-trimpath，-s -w，前端已 embed）
install.sh                  # 可选：安装为 systemd 服务
wscyber-upgrade             # 可选：版本切换式升级/回滚脚本
wscyber-hub.service         # systemd 单元模板
wscyber.env.example
release.json                # version/commit/schema_version/构建工具链/各文件 sha256
LICENSES/                   # DEPENDENCIES.md、*-dependency-licenses.json、cyber-harness LICENSE、ui notices 与 LICENSES.md
sbom.spdx.json
```

同目录另发 `SHA256SUMS`（可选 `SHA256SUMS.sig`/cosign bundle）。

### 7.3 安装布局（`install.sh`，root 执行）

```
/opt/wscyber/releases/<版本>/        # tar 包内容
/opt/wscyber/current -> releases/<版本>
/usr/local/bin/wscyber-hub -> /opt/wscyber/current/wscyber-hub
/usr/local/sbin/wscyber-upgrade -> /opt/wscyber/current/wscyber-upgrade
/etc/wscyber/wscyber.env             # 0640 root:wscyber（WSCYBER_LISTEN、TZ、CYBER_BROWSER_PATH、保留个数）
/etc/wscyber/database.url            # 0600 wscyber:wscyber
/var/lib/wscyber/state               # 0700 wscyber（state-dir）
/var/lib/wscyber/home                # 0700 wscyber（HOME）
/var/backups/wscyber/<时间>_<旧版本>_to_<新版本>/  # 0700 root
/etc/systemd/system/wscyber-hub.service
```

- 专用系统用户 `wscyber`（`useradd --system --home /var/lib/wscyber --shell /usr/sbin/nologin`）。
- Chromium：`install.sh` 只**检测**（`CYBER_BROWSER_PATH` → `chromium`/`chromium-browser`/`google-chrome-stable`），找到则写入 `wscyber.env`；没有则提示发行版命令（`apt install chromium` / `dnf install chromium`），`--install-chromium` 时才代为执行 apt/dnf。字体同理（`fonts-droid-fallback` 可选）。
- `install.sh --database-url-file F` 把连接串复制为 `/etc/wscyber/database.url`（0600 wscyber），否则提示手工写入；`install.sh uninstall [--purge]` 见 §9.6。

### 7.4 systemd 单元

```ini
[Unit]
Description=wscyber Hub
Wants=network-online.target
After=network-online.target postgresql.service

[Service]
Type=exec
User=wscyber
Group=wscyber
EnvironmentFile=/etc/wscyber/wscyber.env
Environment=HOME=/var/lib/wscyber/home
ExecStart=/opt/wscyber/current/wscyber-hub serve --state-dir /var/lib/wscyber/state --database-url-file /etc/wscyber/database.url
Restart=on-failure
RestartSec=5
KillSignal=SIGTERM
TimeoutStopSec=30
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/wscyber
UMask=0077

[Install]
WantedBy=multi-user.target
```

日志进 journald（stdout JSON 事件 + stderr 错误），不另写日志文件。Chromium 与 `ProtectSystem=strict`/`PrivateTmp` 的兼容在 D2 实测（不行就去掉 `ProtectSystem`，不为加固牺牲功能）。

---

## 8. 构建与发布流水线

### 8.1 构建入口 `scripts/release-build.py <版本>`（新）

在**固定构建环境**里执行（本机用构建镜像，CI 用同一构建镜像），步骤：

1. 校验工作树干净、`<版本>` 等于当前提交上的 git 标签（`--snapshot` 时允许 `v0.0.0-dev+<sha>`）；`SOURCE_DATE_EPOCH` = 提交时间。
2. 前端：`cyber-ui-build.py prepare --workspace var/release/<版本>/ui`（公网 codeload 归档 + SHA256/树哈希校验）→ `build`（`npm ci --ignore-scripts` → tsc → Vite）。
3. 复制 `release/public/` → `native/internal/uiembed/public/`。
4. Go：`go -C native mod download`（走 `GOPROXY`，`go.sum` 校验）→ `CGO_ENABLED=0 python3 -I scripts/cyber-go-overlay.py -- build -trimpath -buildvcs=false -ldflags "<§3>" -o dist/<版本>/wscyber-hub ./cmd/wscyber-hub`。
5. 冒烟：`dist/.../wscyber-hub version --json` 与期望一致；临时 state + 本机/服务容器 PG 起 `serve`，`healthcheck` 通过、GET `/` 返回 embed 的 index，SIGTERM 后 `stopped`。
6. 打包：§7.2 tar 包、compose 部署包 `wscyber-compose_<版本>.tar.gz`（`native/deploy/compose/` 内容，`.env` 预填该版本）、`image-context/`、`SHA256SUMS`、`release.json`；SBOM（`go version -m` 转 SPDX，或 CI 中用 syft；前端用 `dependency-notices.json`）；许可目录。
7. 镜像：`docker build -f native/deploy/docker/Dockerfile dist/<版本>/image-context -t <registry>/wscyber-hub:<版本>`；CI 再推送 `:<版本>`、`:<MAJOR.MINOR>`（不推 `latest`，避免误升级）。

### 8.2 固定构建环境 `native/deploy/builder/Dockerfile`

- `golang:1.26.4-bookworm`（digest 固定）+ Node **v24.16.0** 官方二进制包（sha256 校验）装到 `/usr/local/node` 并**软链到 `/usr/bin/node`、`/usr/bin/npm`**（`cyber-ui-build.py` 的子进程 PATH 固定 `/usr/bin:/bin`，§1.1）+ `npm install -g npm@11.16.0`（脚本要求恰为此版本）+ `python3`（≥3.11）、`git`。
- 构建环境注意事项：
  - `cyber-go-overlay.py` 已在 P2 改用 `go env GOMODCACHE`，无需再为官方 golang 镜像调整模块缓存路径。
  - 本机网络：`proxy.golang.org` 直连不可达时由调用方给 `GOPROXY=https://goproxy.cn,direct`（`GOSUMDB` 不变）；`cyber-ui-build.py` 下载/npm 一律不走代理（设计如此），本机若直连不通，`prepare` 用 `--root-repo /root/桌面/cyber-harness` + `--ui-archive <已校验缓存>`（仍校验固定哈希）。CI（GitHub 托管 runner）可直连，走公网分支——这也是 root 公网归档分支的首次实跑。
- **可复现的含义**：输入全部锁定（go.sum、sources.lock.json、实际构建 package-lock、构建镜像 digest、工具链精确版本），任何人可在同一构建镜像里重建并核对 `release.json`；**不承诺逐字节一致**（Vite/Go 输出未做逐字节复现验证）。

### 8.3 CI（`.github/workflows/release.yml`）

- 触发：推送 `v*` 标签；手动 `workflow_dispatch` 出快照（不推镜像）。
- 作业：在构建镜像内跑 `release-build.py`；`services: postgres:18` 提供冒烟库（同时可跑 P 阶段定向 Go 测试，`WSCYBER_TEST_DATABASE_URL` 指向服务容器）；`docker/login-action` + 推送镜像；产物 + `SHA256SUMS` 上传到发布渠道（§13-1）；可选 cosign keyless 签名镜像与 `SHA256SUMS`。
- 权限：`contents: write`（Release）、`packages: write`（GHCR）、`id-token: write`（cosign 可选）。
- 注：`.github/` 在仓库根，超出 AGENTS §2“代码只在 native/ 与 scripts/”，属 GitHub 强制位置，按例外处理（主会话确认）。

### 8.4 产物校验

- 每个下载物在 `SHA256SUMS` 中；`self-update`/`wscyber-upgrade`/`wscyber.sh` 强制校验 sha256，签名存在且本机有 cosign 时再校验签名（可选）。
- 镜像按 digest 记录在 `release.json`；`wscyber.sh upgrade` 拉取后比对 digest。
- 许可清单与 SBOM 随 tar 包与镜像发布（§7.2、§6.2），不放进公开静态根。

---

## 9. 更新、回滚、备份、日志、卸载

### 9.1 镜像形态：`./wscyber.sh upgrade <新版本>`（一条命令）

1. 读 `.env` 当前版本 `OLD`；`docker compose pull hub`（`WSCYBER_VERSION=<新>`），核对 digest。
2. 备份：`docker compose run --rm --no-deps hub db backup --out /backups/<时间>_<OLD>_to_<新>/db.dump`（新镜像内 pg_dump 18）+ 导出 state 卷（`tar` 除 `state/model-node-*`）到同目录；记录 `db status --json`（迁移前 schema 版本）。备份失败即中止，不动运行中的服务。
3. 写 `.env` 为新版本 → `docker compose up -d hub`（启动自动迁移）。
4. 最多等 120 s `docker inspect` 健康为 `healthy`。
5. 成功：打印新版本；保留最近 `WSCYBER_KEEP_BACKUPS`（默认 5）份备份，删除更早的本项目镜像（保留旧版本一份）。
6. 失败：打印最近 100 行日志；用旧镜像 `db status` 看 schema 版本：
   - **未变**（无迁移或迁移未执行）→ 自动改回 `OLD` 并 `up -d`，健康后提示“已回滚到 OLD，新版本启动失败原因见上”。
   - **已变** → 不自动回滚（旧二进制会因“库较新”拒绝启动），停止 hub，打印恢复命令：`./wscyber.sh rollback --restore /backups/<该目录>`（恢复数据库 + state，再起 OLD）。

### 9.2 单二进制：替换即升级

- **最简**：停进程 → `wscyber-hub db backup`（建议）→ 覆盖二进制 → 启动（自动迁移）。
- **`wscyber-hub self-update [--version V]`**：查发布渠道最新/指定版本 → 下载 tar 包与 `SHA256SUMS` 并校验（可选签名）→ 新版本 schema 版本高于当前库时自动执行 `db backup`（需能读到连接串；`--no-backup` 显式跳过）→ 解出到可执行文件同目录临时文件、fsync、`rename` 原子替换，旧版保留为 `wscyber-hub.prev` → 打印“请重启服务”。不自行重启（不知道托管方式）；可执行文件位于 `/opt/wscyber/releases/` 时拒绝并提示用 `wscyber-upgrade`。
- **`wscyber-upgrade <版本>`**（安装包形态，root）：下载 + 校验 → 解到 `/opt/wscyber/releases/<版本>` → `wscyber-hub db backup` + `tar` state 到 `/var/backups/wscyber/…` → 切 `current` 软链 → `systemctl restart wscyber-hub` → 120 s 内 `wscyber-hub healthcheck` 通过 → 成功则保留最近 `WSCYBER_KEEP_RELEASES`（默认 3）个版本目录与 5 份备份；失败按 §9.1-6 同规则：schema 未变则自动切回旧 `current` 并重启，已变则停服务并打印 `wscyber-upgrade rollback --restore <备份目录>`。

### 9.3 回滚

- PATCH 之间（无迁移）：换回旧二进制/旧镜像标签即可（`wscyber.sh rollback` / `wscyber-upgrade rollback`）。
- 跨迁移：必须恢复升级前备份（数据库 + state），再起旧版本；备份之后产生的数据会丢失，命令执行前明确提示并要求确认（`--yes` 跳过）。

### 9.4 备份与恢复

- **内容**：数据库 `pg_dump -Fc`；state-dir 中 `webapi-secrets.key`（**必须与数据库备份配对**，否则已存测试账号/Webhook 密文不可解密）、`egress-ca/`、`model-config/`、`access.key`；不含 `model-node-*`、`tool-config/`（可再生）。备份目录 0700，内含 `meta.json`（版本、schema 版本、时间、sha256）。
- **`db backup`**：Hub 生成临时 `PGPASSFILE`（0600）调用 `pg_dump`，不把口令放进 argv/环境；检查 `pg_dump` 主版本 ≥ 服务端主版本，否则报错并提示安装对应 `postgresql-client-<N>`。
- **RLS 下的备份（P5 起必须验证）**：应用角色无 BYPASSRLS 且表 `FORCE ROW LEVEL SECURITY`，pg_dump 默认 `row_security=off` 会直接报错。设计：`db backup` 以 `--enable-row-security` 并设 `PGOPTIONS=-c app.role=system` 运行，使 `wscyber_can_see` 对全部行为真（POSTGRES §4.3 策略形态）；恢复用 `pg_restore`（RLS/策略/触发器属 post-data，在数据装载之后建立——**需在 P5 后实测**）。实测不通时退路：DBA 建只读备份角色 `wscyber_backup`（`BYPASSRLS` + `pg_read_all_data`），`db backup --database-url-file <备份角色连接串>`。
- **恢复**（Hub 已停）：DBA 或库属主执行 `pg_restore --clean --if-exists --no-owner --role=wscyber -d <库> db.dump`（或 DBA 删建空库后 `pg_restore --no-owner --role=wscyber`）；解回 state 文件；起对应版本。命令封装在 `wscyber.sh rollback --restore` / `wscyber-upgrade rollback --restore`。
- 例行备份：文档给 cron/systemd timer 示例（`wscyber-hub db backup` + state tar，保留 N 份），不内置调度。

### 9.5 日志与排障

- 镜像：`docker compose logs -f hub`；安装包：`journalctl -u wscyber-hub -f`；二进制直跑：终端 stdout/stderr。
- 关键行：`{"event":"ready",…,"version":…,"browser":…}`、`{"event":"stopped"}`、`wscyber-hub: <错误>`。数据库日志只打印 host/port/db/user（POSTGRES §1）。
- 常见问题表（随片补全）：端口占用；连接串文件权限/属主（容器 uid 10001）；角色为超级用户或 BYPASSRLS → 拒绝启动；库版本较新 → 拒绝启动（说明二进制比库旧，换新版本或恢复备份）；“同库已有 Hub 在运行”（advisory lock）；Chromium 未找到；反向代理未传 `X-Forwarded-Proto`/改写 `Host` 导致 Cookie/WebSocket 失败；`webapi-secrets.key` 缺失提示。
- 诊断命令：`wscyber-hub version`、`wscyber-hub db status`、`wscyber-hub healthcheck`、`./wscyber.sh status`。

### 9.6 卸载

- 镜像：`docker compose down`（保留卷）；`docker compose down -v` 同时删 state（不可恢复）；数据库由 DBA `DROP DATABASE`。
- 安装包：`install.sh uninstall` = `systemctl disable --now` + 删单元、`/opt/wscyber`、软链；`--purge` 另删 `/etc/wscyber`、`/var/lib/wscyber`、`/var/backups/wscyber` 与用户 `wscyber`（交互确认）。数据库不自动删除。

### 9.7 安全提示（部署文档须写明）

- 模型终端与 Hub 同 uid、继承环境（§1.2）：终端能读 state-dir（模型 API key、加密密钥、CA 私钥）与连接串文件。镜像形态至少把这些限制在容器内；单二进制形态与宿主同一用户空间。**专用系统用户 `wscyber`**（不用 root/个人账号）是安装包的硬要求。终端/节点改独立 uid 或沙箱不在本期，列为后续议题。
- 首版不做公网部署（`docs/product.md` §4）：默认只监听/发布 loopback，对内网开放经反向代理 + HTTPS。

---

## 10. 操作手册草案（给运维；实现后随片校正）

### 10.0 数据库准备（两种形态共用，DBA 执行一次）

```sql
CREATE ROLE wscyber LOGIN PASSWORD '<随机口令>' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
CREATE DATABASE wscyber OWNER wscyber ENCODING 'UTF8' TEMPLATE template0;
```

连接串：`postgres://wscyber:<口令>@<主机>:5432/wscyber?sslmode=verify-full`（同机/内网测试可 `sslmode=disable`）。PostgreSQL 版本：开发与验证只在 18 上做过；其它版本未验证。

### 10.1 Docker 首次部署

1. 装 Docker（含 compose 插件），`docker login <registry>`（私有仓库时）。
2. 取部署目录：`curl -LO <发布渠道>/<版本>/wscyber-compose_<版本>.tar.gz && tar xzf … && cd wscyber`（含 compose.yaml、wscyber.sh、initdb/）。
3. `./wscyber.sh init --version <版本>`：生成 `.env` 与 `secrets/`（属主 10001、0600）。
4. 外部库：把 §10.0 的连接串写入 `secrets/database.url`（`./wscyber.sh set-database-url` 交互输入，不留 shell 历史）。**试用**：改用 `./wscyber.sh init --trial`，自带 postgres 容器与随机口令，跳过本步。
5. `docker compose up -d`（试用：`docker compose --profile trial up -d`）。
6. `docker compose ps` 显示 `healthy`；`docker compose logs hub | grep '"event":"ready"'`。
7. 建首个管理员：`docker compose exec hub wscyber-hub user add --admin --username admin`（交互输入口令；P6 前改为：`docker compose exec hub cat /var/lib/wscyber/state/access.key` 取登录 key）。
8. 浏览器打开 `http://127.0.0.1:8080/`（或反向代理地址）登录；“设置”里配置模型并“测试连接”。

### 10.2 单二进制首次部署（最短路径）

1. 下载并校验：`curl -LO <发布渠道>/<版本>/wscyber-hub_<版本>_linux_amd64.tar.gz -O <…>/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS`。
2. `tar xzf wscyber-hub_<版本>_linux_amd64.tar.gz && cd wscyber-hub_<版本>`。
3. 写连接串：`install -m 600 /dev/null db.url && $EDITOR db.url`（§10.0）。
4. 启动：`./wscyber-hub serve --database-url-file db.url`（默认 `127.0.0.1:8080`、state 在 `~/.local/share/wscyber`；看到 `"event":"ready"`）。
5. 另开终端：`./wscyber-hub user add --admin --username admin`（P6 前：登录 key 在 `~/.local/share/wscyber/access.key`）。
6. 打开 `http://127.0.0.1:8080/`。需要浏览器/爬虫命令时装系统 Chromium（`apt install chromium`）或 `export CYBER_BROWSER_PATH=…` 后重启。

作为服务长期运行（可选）：`sudo ./install.sh --database-url-file db.url && sudo systemctl enable --now wscyber-hub`，之后 `sudo -u wscyber wscyber-hub user add --admin …`（需 `--database-url-file /etc/wscyber/database.url`），`wscyber-hub healthcheck` 验证。

### 10.3 更新

- Docker：`./wscyber.sh upgrade <新版本>`。
- 安装包：`sudo wscyber-upgrade <新版本>`。
- 单二进制直跑：停进程 → `./wscyber-hub self-update --version <新版本> --database-url-file db.url` → 重新 `serve`。
- 失败时按命令输出的提示执行 `rollback`（schema 未变会自动回滚）。

### 10.4 回滚 / 备份 / 卸载

`./wscyber.sh rollback [--restore <备份目录>]`、`./wscyber.sh backup`；`sudo wscyber-upgrade rollback [--restore …]`、`wscyber-hub db backup --out …`；卸载见 §9.6。

---

## 11. 与 P 阶段的衔接

| D1 已完成 / 部署线待做 | PG 已接入后的部署工作 | P4 已清理、发布时复核 | 须等 P5/P6 |
|---|---|---|---|
| 版本注入与 `version`；`/api/wscyber/version` + 前端显示；前端 embed；`serve` 子命令（listen/默认值/环境变量/自动 access.key/权限放宽/通配地址回连）；`healthcheck`（/health）；ready 事件加字段 | `serve --database-url-file` 已接入；待做：`db status/migrate/backup`；`/readyz`；compose `trial` profile 与 initdb；`wscyber.sh`/`wscyber-upgrade`/`self-update` 的备份与 schema 判定；首次部署全流程实测 | 当前许可清单已按 PG Hub 闭包核对；发布时生成 SBOM 并复核，state-dir 只含运行文件 | RLS 下 `pg_dump/pg_restore` 实测（P5）；`user add --admin` 与口令登录写入首次部署步骤（P6a/P6b）；打 `v0.1.0` |
| 发布构建脚本、构建镜像、运行镜像、tar 包骨架、systemd 单元、install.sh、CI 工作流（用 PG 版 Hub 加独立测试库验证镜像/包能跑） | | | |

冲突约束：D1 改 `native/cmd/wscyber-hub/**`，P3 也改它 → **D1 须在 P3 开始前完成**（与 P1/P2 可并行：P1 写 `native/pgschema/**`、P2 写 `native/patches/**` 与 overlay 脚本）。D1、P2、P3 已完成；D2 复用现有 overlay 脚本，不再重复修改模块缓存定位。

---

## 12. 实施拆片

| 片 | 范围 | 可写文件 | 验证 | 依赖 | 执行 |
|---|---|---|---|---|---|
| **D1** 单二进制运行形态 | `internal/buildinfo`；`version`/`healthcheck`/`serve` 子命令（§2 全部：默认值、环境变量、`WSCYBER_DATABASE_URL` 读后 unset 的占位接口、自动 `access.key`、0600/0400 放宽、非 loopback、通配地址回连 127.0.0.1）；前端 embed（`internal/uiembed` + `ui.go` 改 fs.FS 校验，`--ui-dir` 优先）；ready 加 `version`/`browser`；`GET /api/wscyber/version`；`.gitignore` 加 embed 目录 | `native/cmd/wscyber-hub/**`、`native/internal/buildinfo/**`、`native/internal/uiembed/**`、`.gitignore`、`native/RUNNING.md` | 定向 Go 测试（子命令解析、旧旗标不变、embed 清单校验正/反例、权限、通配回连、密钥自动生成与复用、版本 JSON）；用现有 `var/ui-work/release/public` 复制进 embed 目录构建，`serve` 无 `--ui-dir` 起在临时 state：`/health`、`/`、`healthcheck`、SIGTERM→`stopped`；`demo-stack.py up/selftest` 不回退 | 无（须在 P3 前完成） | 后端核心（o-latest） |
| **D1-ui** 版本显示 | 侧栏底部显示 `/api/wscyber/version` 的版本与短提交 | `native/ui/source-overlay/**` | tsc/Vite；演示环境截图 | D1 | 前端（ds-flash） |
| **D2** 发布构建与封装 | `scripts/release-build.py`；构建镜像、运行镜像 Dockerfile；compose 骨架（无 trial）；tar 包、`install.sh`、systemd 单元、`wscyber.env.example`；`SHA256SUMS`/`release.json`/许可目录/SBOM；复用已支持 `go env GOMODCACHE` 的 overlay 脚本；`.github/workflows/release.yml`（快照模式可本地 `act` 不要求） | `scripts/release-build.py`、`scripts/cyber-go-overlay.py`（按需）、`native/deploy/**`、`.github/workflows/**` | 本机在构建镜像内出 `v0.0.0-dev+<sha>` 快照；`docker build` 运行镜像；`docker run` 起容器（PG 版，独立测试库与临时卷）→ healthy、`/` 返回 index、容器内 `chromium --version`、非 root；宿主直接跑 tar 包里的二进制；`install.sh` 在**一次性容器**（systemd 镜像）内安装/卸载，不动宿主 | D1、P3（已完成） | 后端核心（o-latest） |
| **D3** 数据库运维与升级 | `db status/migrate/backup`；`/readyz`；DB 启动等待（`--database-wait`，默认 60 s）；compose `trial` profile + `initdb/10-wscyber.sh`；`wscyber.sh`（init/upgrade/rollback/backup/status）；`wscyber-upgrade`；`self-update`（发布渠道 URL + 可选 token 文件） | `native/cmd/wscyber-hub/**`、`native/deploy/**`、`scripts/release-build.py` | 本机 docker + 本机 PG18（`wscyber_dev` 建临时库，演示数据）：compose 外部库首次部署 → 造演示数据 → 升级到第二个快照（含一个只在测试构建里的空迁移 `0002`）→ 备份目录内容核对 → 故意让新版本启动失败验证两种回滚分支 → `--restore` 恢复后数据一致；trial profile 首次部署；`self-update` 指向本地 HTTP 目录 | P3、D2 | 后端核心（o-latest） |
| **D4** 首发收尾 | P4/P5/P6 后：更新许可清单与 SBOM、RLS 下备份/恢复实测（必要时备份角色退路）、首次部署步骤接 `user add --admin`、§10 手册定稿进 `native/RUNNING.md`/本文、打 `v0.1.0-rc.1` 并在 CI 实跑一次 | `native/*.md`、`native/*licenses.json`、`native/deploy/**`、`scripts/**` | CI 实跑出镜像与包；本机按 §10.1/§10.2 从零走一遍并升级到 `rc.2` | P4、P5、P6、待决 §13-1 | 后端一般（gpt-6-astra；只用演示/合成数据） |

---

## 13. 待用户决定（各附推荐）

**决定（11-22）**：1 用户选 **GHCR 公开**（镜像 + GitHub Releases 二进制，`self-update` 免凭据）；**首次公开发布前必须完成 §1.4 许可核对**，此前 CI 只构建、不公开推送。2 **反向代理终止 TLS**（按推荐）。3 默认端口 8080、4 不做自动更新：主会话按推荐定。另：用户允许在仓库根目录加 `.github/workflows/`（AGENTS §2 的例外）；推送标签/首次发布为对外操作，执行前向用户确认。

1. **发布渠道与可见性**（镜像 + 二进制放哪、公开还是私有）。
   推荐：**先用 GHCR 私有**——镜像 `ghcr.io/<owner>/wscyber-hub`，二进制 tar 包作为同仓库的 OCI 制品（或放 CI 私有制品），服务器用只读 token 拉取；`self-update`/脚本只认“渠道 URL + 可选 token 文件”，日后改公开（GitHub Releases + 公开 GHCR）或公司仓库只改配置。理由：源码仓库公开 ≠ 已完成二进制分发的许可核对（§1.4 有 GPL/AGPL 义务与 7 个 UNKNOWN 模块，DEPENDENCIES 要求分发前核对）。若用户/法务确认可公开，则**公开 GHCR + GitHub Releases** 最省事（`self-update` 免凭据）。
2. **HTTPS 终止**。推荐：**反向代理**（nginx/Caddy/公司 LB）终止 TLS，Hub 只听 loopback/容器内网；文档给 nginx 片段（WebSocket Upgrade、保留 `Host`、`X-Forwarded-Proto https`）。内置 TLS 需要给上游 `host.Serve` 打补丁（它自己 `net.Listen`，§1.2），收益不抵补丁成本。
3. **默认端口**。推荐：**8080**（`serve` 默认 `127.0.0.1:8080`，镜像内 `0.0.0.0:8080`，compose 默认发布到宿主 `127.0.0.1:8080`）。演示 42460 保持不变。
4. **自动更新**。推荐：**不做自动更新**，只提供手动一条命令（`wscyber.sh upgrade` / `wscyber-upgrade` / `self-update`）；可选 `wscyber-hub version --check` 只提示有新版本。理由：升级伴随不可逆迁移与备份，需人工确认时机。
