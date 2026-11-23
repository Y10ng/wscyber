# 构建与运行

在仓库根目录执行。Go 需 1.26、Node v24.16.0（本机已装）。

## 0. 前提：PostgreSQL

Hub 的全部数据（原生会话/事件、webapi 任务等）都在 PostgreSQL，没有连接串即启动失败。本机 PG18、开发角色 `wscyber_dev`（非超级用户、无 BYPASSRLS、可建库）与连接串文件 `~/.config/wscyber/pg-dev.url`、测试用 `~/.config/wscyber/pg-test.env` 的准备步骤见 `native/POSTGRES.md` §7.2。Hub 启动时自动迁移 schema；同一个库只能有一个 Hub（单实例锁，第二个启动即失败；运行中锁丢失则停止并以非 0 退出）。升级前先 `pg_dump` 备份。

## 1. 构建 Hub（应用 overlay 替换文件）

```bash
env GOMAXPROCS=2 GOGC=30 GOTOOLCHAIN=local GOPROXY=off \
  python3 -I scripts/cyber-go-overlay.py -- build -p 1 \
  -o "$PWD/var/wscyber-hub" ./cmd/wscyber-hub
```

overlay 由清单驱动，派生文件只放在 `var/cyber-overlay/`；原生存储为 PostgreSQL（见 `native/patches/README.md`）。

普通 `go build` 不会应用 `native/patches/` 的替换文件（会编译失败），正式运行必须用上面的入口。定向测试同理，且需要测试库：`source ~/.config/wscyber/pg-test.env` 后 `python3 -I scripts/cyber-go-overlay.py -- test -p 1 ./webapi -run '^TestWebAPIVerification' -count=1`。

## 2. 构建前端

复用已准备好的工作区增量构建（刷新 `native/ui/source-overlay` → tsc → Vite）：

```bash
python3 -I scripts/cyber-ui-build.py rebuild --workspace "$PWD/var/ui-work"
# 产物：该工作区下 release/public
```

Node heap 默认 1536MB，可用环境变量 `WSCYBER_UI_HEAP_MB` 调整（仅接受 256–8192 的整数，不继承调用方 `NODE_OPTIONS`）。

工作区丢失时用 `prepare --workspace <新目录>` 重新准备（下载固定来源，见 `native/ui/README.md`）。

## 3. 启动

```bash
env -i PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 \
  "$PWD/var/wscyber-hub" --local-hub --model-node \
  --listen 127.0.0.1:PORT --access-key-file KEY_FILE \
  --state-dir STATE_DIR --ui-dir UI_DIR \
  --database-url-file DB_URL_FILE
```

- `DB_URL_FILE`：属主私有（0600/0400）普通文件，单行 PostgreSQL 连接串（如 `~/.config/wscyber/pg-dev.url`，前提见 §0）。也可用环境变量 `WSCYBER_DATABASE_URL_FILE`（文件路径）或 `WSCYBER_DATABASE_URL`（连接串，读后即从进程环境删除）；优先级旗标 > 前者 > 后者，三者都没有则启动失败。连接串与口令不进日志/ready，ready 事件只给 `database` 的 host/port/database/user 与 `schema` 版本。
- `KEY_FILE`：0600 普通文件，内容为页面登录 key；`STATE_DIR` 首次须不存在，之后用同一目录重开（state-dir 只放凭据密钥、出口 CA、模型配置等文件，数据在库里；重开时库与 state-dir 要配套）。key 不放在 state/UI 目录内。
- 不传 `--model-endpoint` 即正式模型节点；模型在页面“设置”中配置，保存不自动探测，“测试连接”才联网。
- 停止：对 Hub PID 发 SIGTERM，等待 `stopped` 输出。不要两个进程共用一个 state 目录；同一个库第二个 Hub 会因单实例锁启动失败。

## 3.1 单二进制运行（`serve`，前端已嵌入）

构建（把前端产物放进被 git 忽略的 embed 目录，再注入版本；`CGO_ENABLED=0` 可得静态二进制）：

```bash
find native/internal/uiembed/public -mindepth 1 ! -name .placeholder -delete
cp -a var/ui-work/release/public/. native/internal/uiembed/public/
env CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off python3 -I scripts/cyber-go-overlay.py -- build -p 1 \
  -ldflags "-s -w -X wscyber/native/internal/buildinfo.Version=v0.0.0-dev -X wscyber/native/internal/buildinfo.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o "$PWD/var/wscyber-hub" ./cmd/wscyber-hub
```

未放入前端时照常可编译，`serve` 只提供 API 并在 stderr 提示。嵌入内容按 `.wscyber-ui.json` 做与 `--ui-dir` 相同的固定来源/逐文件哈希校验，不符即启动失败。

最短运行：`./wscyber-hub serve --database-url-file DB_URL_FILE`（PG 前提见 §0），看到 `"event":"ready"` 后打开 `http://127.0.0.1:8080/`，登录 key 为 `access.key` 文件内容（路径见 ready 事件 `access_key_file`）。

| 项 | 旗标 | 环境变量 | 默认 |
|---|---|---|---|
| 监听 | `--listen` | `WSCYBER_LISTEN` | `127.0.0.1:8080`；须为 IP:port，可 `0.0.0.0:8080`（对内网开放时经反向代理终止 TLS，保留 `Host`、传 `X-Forwarded-Proto`） |
| 状态目录 | `--state-dir` | `WSCYBER_STATE_DIR` | `${XDG_DATA_HOME:-$HOME/.local/share}/wscyber`（0700，可重开） |
| 访问密钥 | `--access-key-file` | `WSCYBER_ACCESS_KEY_FILE` | `<state>/access.key`，不存在时生成 32 字节随机 key（0600），之后复用 |
| 数据库连接串 | `--database-url-file` | `WSCYBER_DATABASE_URL_FILE`，或 `WSCYBER_DATABASE_URL`（读后即从进程环境删除） | 无；**必填**，三者都没有则启动失败（前提见 §0、`native/POSTGRES.md` §7.2） |
| 前端目录 | `--ui-dir` | — | 无；给出时优先于嵌入前端（前端迭代用） |
| Chromium | — | `CYBER_BROWSER_PATH` | 系统查找；找不到时 stderr 提示，浏览器/爬虫命令不可用，其余正常 |

优先级：旗标 > 环境变量 > 默认值。密钥/连接串文件须为非符号链接的普通文件，权限 0600 或 0400（旧旗标形式仍只接受 0600）。监听通配地址时进程内节点改连 `127.0.0.1:<port>`。

其它子命令：

- `wscyber-hub version [--json]`：版本、提交、构建时间、上游提交、UI 提交与嵌入清单哈希、schema（内嵌迁移最大版本，当前 1）、Go 版本。登录后 `GET /api/wscyber/version` 返回同 JSON（两种启动形式都有）。
- `wscyber-hub healthcheck [--url http://127.0.0.1:8080]`：GET `/readyz`（404 时退回 `/health`），2 s 超时，健康退出 0，否则 1；默认地址取 `WSCYBER_LISTEN` 或 `127.0.0.1:8080`。

无子命令（首参以 `-` 开头）时仍是上面 §3 的旧旗标形式，行为不变（不使用嵌入前端、只许 loopback）；演示栈与测试照旧。

## 4. 演示环境（全部测试数据）

`python3 scripts/demo-stack.py up`：启动三个本地进程——模拟目标“演示库存 API” `http://127.0.0.1:18080`、按脚本回复的模拟模型 `http://127.0.0.1:18081/v1`，以及独立临时 Hub `http://127.0.0.1:42460/`（走 `--model-endpoint` 受控分支；每次 `up` 新建 `var/demo/state-<时间戳>`，并经 Hub API 自动保存模型配置）。数据库：`up` 用 `~/.config/wscyber/pg-dev.url` 的主机/角色/口令，以该角色重建库 `wscyber_demo`（`DROP … WITH (FORCE)` + `CREATE`，psql 凭据经 `PG*` 环境变量），演示连接串写 `var/demo/database.url`（0600）并以 `--database-url-file` 启动 Hub；`down` 不删库，下次 `up` 重建。登录 key 在 `var/demo/access.key`（0600）。在页面新建任务时 origin 填 `http://127.0.0.1:18080`。

- Hub 二进制默认用 `var/demo/wscyber-hub`，不存在时用 `var/wscyber-hub`，也可用 `--hub` 指定。日志在 `var/demo/{target,model,hub}.log`，pid 在 `var/demo/pids.json`。
- `status`；`fix on|off` 切换目标“已修复”模式（`/api/admin/config` 返回 401 或 200，供复测得出两种结论）；`down` 停止全部进程并释放端口。
- `selftest [--chain]`：经 Hub API 跑一遍研究，加 `--chain` 时再跑验证、报告和两次复测，只输出数量和状态。
- `approval-demo`：经 Hub API 跑“审批演示”剧本（模拟模型并行发一条测试 DELETE 与一条命中高危规则的无害命令），一轮全部批准、一轮全部拒绝，输出审批与请求状态。页面上首条目标文本含“审批演示”即触发同一剧本。
- 模拟模型按首条消息识别会话类型（研究/独立验证/报告/复测，复测首行须含“复测”），在原生 3 回合上限内推进：同一阶段的工具调用并行发出。不读写 `var/preview`，不连接真实模型。

## 5. 浏览器依赖（M5c）

模型节点的 `playwright`/`katana -headless` 需要本机 Chrome/Chromium（系统包，不随仓库分发，**不自动下载**）。路径：环境变量 `CYBER_BROWSER_PATH`（显式优先，须可执行）→ 系统查找（go-rod `launcher.LookPath`，含 `/usr/bin/chromium`、`chromium-browser`、`google-chrome` 等）。找不到时命令返回“未找到 Chromium 可执行文件…”。本机：Debian `chromium 145.0.7632.159-1`（`chromium --version`）。上面 `env -i` 启动方式需要时加 `CYBER_BROWSER_PATH=/usr/bin/chromium`。katana 的配置目录：未设 `XDG_CONFIG_HOME` 时 Hub 用 `STATE_DIR/tool-config`。见 `native/BROWSER.md`。

## 6. TLS 缺链兼容

`webapi_http` 及共用发送路径在证书无法链到系统信任根时，会按 AIA caIssuers 自动获取公网 HTTP 证书并跟随 HTTP 跳转；无需额外配置或逐次确认（用户确认的证书验证出网例外，不受任务 origins 限制）。补取材料只作中间证书，仍严格验证系统根、目标域名/IP、有效期、用途与签名链，不安装新信任根。

单次握手最多补取 4 次，每次最多 5 秒/64KB，URL 缓存最多 64 项；仅支持 DER/PEM，不支持 PKCS#7、HTTPS/LDAP AIA 或私网发布点。补链也使用直连；无法获取或仍不可信时继续返回 `tls_certificate`，不重发目标请求。辅助补链请求当前不记录为任务 HTTP 证据或审计，不等于普通目标请求获准越界。

## 7. 限制

- `webapi_http` 按任务测试策略：scope=origins（默认）只发授权 origins，unrestricted 不限；危险操作审批见 `native/POLICY.md`。终端、浏览器、爬虫经全工具出口代理强制（非系统级隔离，见 `native/POLICY.md`、`native/BROWSER.md`）。
- 正式入口不自动恢复/重放历史任务。
