# wscyber

## 当前开发方向与原生入口（2026-11-13）

本项目已确定**全部基于Cyber Harness复用/扩展，不再自建另一套Harness**，见[底座决定](docs/cyber-harness-foundation.md)。当前新入口在独立`native/`模块：

- [原生CLI/H1](native/README.md)：显式本机fixture的原生会话/工具/事件链。
- [原生Web宿主/H2-A](native/HUB.md)：鉴权、原生路由及SQLite生命周期。
- [原生节点接线/H2-B](native/NODE.md)：真实Web/AOP→本机fixture节点→工具→Hub事件回读。
- [原生UI构建与静态宿主/H3-A2](native/ui/README.md)：固定源码与最小补充锁，完整tsc/Vite及真实资源HTTP/鉴权检查通过；浏览器交互与逐功能展开态尚未验收。

这些小片已完成各自的受控定向验证，**不代表生产模型、目标授权出口、原生UI或原有业务已全部迁移**。旧`cmd/`、`internal/`、`web/`保留为业务与回归资产；下方旧工程介绍/运行命令不是Cyber原生入口，过时能力清单以[STATUS](docs/STATUS.md)和实际代码为准。不要为旧引擎继续增加新运行机制，也不要直接删除历史数据。按小轮次、低并发与定向验证推进，轮后清理专属资源再新会话。

## 旧工程说明（历史背景）

独立、自托管、面向明确授权场景的通用安全研究平台。目标覆盖漏洞挖掘、代码审计、CTF、内部攻防演练，以及稳定编号的发现、模型辅助报告和内部 SDL 对接。Go 控制面、PostgreSQL 持久状态；场景 Profile 与可替换适配器扩展能力，不强制黑板或固定专家组织，优先复用成熟组件。

**目标是自主探索、对实际出站请求按风险放行/拦截，不审批每一步探索或所有工具动作。G0 已改为新命令默认放行、平台 HTTP 仅命中明确破坏信号时待审；工具默认联网，但其直连尚无请求拦截或 origin 强制。** 尚无完整流量门禁、研究协作、Finding/报告/SDL。研究方向与短暂执行分离，保存观察/失败，未知效果不自动重放；不能称为已完成的全自动渗透平台。状态与真实验证见 [STATUS](docs/STATUS.md)。

**React/TypeScript + 官方 Ant Design 的首个正式前端切片已实现并通过本地可见浏览器交互验收：可靠创建探索、提交核对及最小只读研究/证据工作面。** 构建隔离到 `web/dist-react`；旧 Vue 原型源码与 `web/dist` 保留，现有服务尚未切换。完整视觉与异常流程验收状态见 [STATUS](docs/STATUS.md)。

**当前引擎方向为稳定优先、模型自主探索：** 保留主研究、按需独立验证、成果收尾和单独复测，不用覆盖协议强制全测；具体实现和稳定性仍待验证，见[研究引擎E1](docs/research-engine-design.md)。首版新增请求风险控制/审批与真实外部对接延期；目标流量及逐轮模型输入/返回记录和读取接口在目标范围，详见[确认稿D1](docs/product-confirmation.md)。现有安全检查不因此移除。

## 文档入口

- **[当前开发规范总览](docs/development-standards.md)**：先复用项目组件库、缺项先补库；现行视觉/交互、开发流程、禁做事项和验收要求。
- **[首版完整系统交付](docs/system-delivery.md)**：当前已恢复完整系统开发，企业级视觉、功能与可靠性质量门槛不因提速降低；分组实施与真实验证以STATUS为准。
- [UI-1界面规范](docs/ui-baseline.md) / [实际组件库与覆盖](docs/ui-components.md)：项目设计系统及可复用代码，Ant Design只是底层依赖。
- **[当前产品、架构与界面确认稿](docs/product-confirmation.md)**：首版范围、布局模板、待决定事项与后续并行边界；建议尚待用户评审，不是全量开工规格。
- [产品方向](docs/vision.md)：用户任务与长期目标。
- [核心架构 A3](docs/architecture.md)：通用研究内核、场景/协作、请求级门禁与恢复边界。
- [发现与报告](docs/findings-and-reporting.md)：稳定编号、实质验证、模型草稿、版本和 SDL 适配目标。
- [接口契约](docs/api.md)：HTTP、模块与一致性约束。
- [组件选型与镜像来源](docs/components.md)：优先复用的轮子、许可证、来源 ID。
- [本地配置与模型测试](docs/local-configuration.md)：运行配置、独立私有测试配置的位置、授权边界与实际结果；不含明文凭据。
- [项目工作面与前端设计](docs/frontend-design.md)：基于项目组件库的信息架构、业务交互及迁移验收。
- [分阶段路线](docs/roadmap.md)：后续切片、多 Agent 任务包与交接，不要求一次性全部实施。
- [开发规范](AGENTS.md)：协作范围、上下文交接和交付。

最近已验收可靠启动后端：同事务保存任务、初始方向与操作回执，支持同键去重、只读核对和版本化迁移；当前推进正式前端首个切片。G0 默认工具/联网和轻量 HTTP 策略为已有基线，完整请求门禁等后续范围未实施。详细状态与验证见 STATUS。

## 当前工程基线能做什么

创建项目与研究方向 → 人工记录观察或启用已配置模型 → 提出 HTTP/命令动作 → 策略放行或命中信号待人工决定 → 执行并保存回执 → 读取证据、核对未知效果、接续方向。新命令不逐条审批；历史 pending 保留待审，不自动放行。

- `run`：显式启用模型；未配置时拒绝，不使用假模型。
- `resume`：恢复人工可接续状态，不启用模型、不清零累计步数、不重发旧请求。
- `pause`：阻止后续派发，不声称撤回在途动作。
- `outcome_unknown`：先核对实际效果，再填写核对记录；历史仍为未知，不改写成成功。
- 观察和模型结论不是已确认漏洞。尚未实现 Finding/Verification、报告/SDL、通用场景 Profile、完整研究协作、全局自动选题、SOC、浏览器或原生网络扫描工具透明代理。

## 旧工程本机运行（非Cyber原生入口）

需要 Go 1.26、Node/npm、Docker/Compose 和本机 Docker socket；构建前须已有 Go 模块缓存与前端依赖，`make image`/`make web` 不自动下载。宿主运行默认 `127.0.0.1:8790`，仅本机访问。控制面有 Docker 管理权限，因此是**受信任管理员服务**；不要将它视为无特权服务。执行容器不获 Docker socket/宿主挂载/模型凭据。

```sh
cd /root/桌面/wscyber
python3 -I scripts/init-local.py   # 只创建缺失的 .env，0600；不打印或覆盖凭据
make db                         # 独立 PostgreSQL，保留本项目持久卷
make image                      # Go + web + 已有工具层，共用 runtime:local
make run                        # 控制面仍可在宿主运行
```

打开 **http://127.0.0.1:8790**，使用本项目 `.env` 中 `WSCYBER_AUTH_TOKEN` 登录。令牌只留浏览器内存，刷新/退出后需重新输入。不使用其他平台账号或密钥。`.env` 是受信任的 shell 配置，`make run` 会加载它；不要加载来自外部的配置脚本。

`make web` 构建正式React到 `web/dist-react`，`make image` 使用同一产物；旧Vue原型只能通过 `make web-prototype` 显式构建到 `web/dist`，不进入正式镜像。新 `.env.example` 的静态目录也指向React，已有 `.env` 不自动改写；若仍显式指向旧目录，须经确认后自行切换，改配方不表示现有服务已经切换。

`make image` 仅生成 `wscyber/runtime:local`，`make toolbox` 是兼容别名，不再额外生成 toolbox 镜像。Linux/CGO=0 产物独立放在 `bin/runtime/wscyber`，不覆盖宿主二进制；`RUNTIME_ARCH` 默认 amd64，须与本地基础层一致。`.dockerignore` 仅放行配方和产物，不打包私有配置或仓库源码。

**本轮只静态检查了配方，未构建/运行新镜像。** 镜像默认入口是 Go 应用，但容器控制面执行命令仍需 Docker CLI 与标准 Docker socket 权限；本轮未确认基础层含 CLI，也未增加完整应用容器部署。PostgreSQL 继续独立持久服务，不塞进应用镜像。镜像内监听为 `0.0.0.0:8790`，未来部署只向受控本机端口发布。

历史 `wscyber/*` 镜像及来源标签未改动。全新机器须按 [components](docs/components.md) 准备/审查基础层；没有公共 registry 发行版。既有 `.env` 不自动迁移，若固定旧 toolbox 标签，需后续显式改为 runtime 并完成构建后再使用；初始化脚本不会覆盖它。

停止前台服务用 Ctrl+C；数据库可用 `docker compose stop postgres` 停止，**不要删除 volume**。后台验证服务的 PID/启动状态由开发记录说明，不使用宽泛 `pkill`。

`/healthz` 是存活探针，不代表数据库/模型/沙箱全部健康。所有 `/api/*` 要求 Bearer token。首期只支持同源本机 HTTP；远程部署前应单独设计 TLS、可信代理 origin、身份/角色与审计，不直接监听公网。静态文件可公开加载，但不能读取 API 数据。

## 模型配置

默认完全未配置，可先使用人工研究、命令与 HTTP 动作。显式设置以下三项并重启：

```dotenv
WSCYBER_MODEL_BASE_URL=https://your-approved-provider.example/v1
WSCYBER_MODEL_API_KEY=your_private_key
WSCYBER_MODEL=your_explicit_model_id
```

首个适配器使用官方 `openai-go/v3` 的 Chat Completions，当前参数包括 `max_completion_tokens=4096`。并非所有兼容端点都支持；尚未做真实供应商兼容性或发现质量评测。不把 Anthropic 原生协议假装成同一协议；后续可按 domain.Model 增加独立官方适配器。

启用模型会把该方向的完整 Snapshot（包括目标材料、动作和回执）送往你配置的服务。仅配置获准处理这些数据的端点；目标认证头/正文可能敏感，首期没有凭据保险库或自动材料分级。API key 只在控制面，不进沙箱。测试中的 fake 不会被产品入口加载。

`WSCYBER_MAX_PARALLEL=2` 分别限制研究 worker 和动作 worker，不是整个系统总共两条 goroutine；等待审批不占槽。`WSCYBER_MAX_STEPS=12` 是每方向累计决策上限，耗尽后 blocked，不是已完成结论；增加预算需显式调整配置重启，不因普通 resume 绕过限制。完整模型输入超256KiB明确阻塞，不默默删证据。

## 网络与沙箱边界

- 命令：默认 `WSCYBER_SANDBOX_NETWORK=bridge`，可显式设为 `none`；无逐场景工具名单或逐命令审批，已有工具共享同一 runtime 镜像。保留 runc、UID/GID 65534、只读 rootfs、cap-drop、no-new-privileges、CPU/内存/PID/输出/时间限制，无宿主挂载、socket 或秘密；`/tmp` 为512MiB tmpfs，允许执行以支持工具工作文件。覆盖来源入口并清空命令环境，容器内另有 `timeout`；残留容器对象仍可能需要人工核对。
- HTTP：由可信控制面代发，精确 origin（scheme/host/port），DNS 所有答案校验后固定一个 IP；禁止代理环境隐式外发、不重定向、不自动重试、正常 TLS 证书验证。禁止 metadata/link-local/multicast/unspecified；内部私网必须明确授权。Loopback 默认拒绝，仅本地 fixture 验证可显式开启 `WSCYBER_ALLOW_LOOPBACK=true`。
- 平台 HTTP 使用 `request-signals-v1`：DELETE 及 URL/path/query/body/JSON、method override 中的 delete/remove 等明确破坏信号进入 pending，其余默认 approved，未知不一律待审。这是轻量启发式，不能证明未命中请求安全；`approved` 可能来自策略或人工。请求哈希与策略理由同事务保存，初始理由保留在 `action.proposed` 事件，人工决定可覆盖 `decision_reason`；历史 pending 不重评。
- 平台代发仅 HTTP/1.1 结构化捕获，非原始线上报文；此捕获链不支持 WebSocket/CONNECT/UDP/QUIC。默认输出64KiB（服务配置），截断会标明；已保存的正文可下载，超过捕获上限的内容不是已保存原件。
- **工具直连流量未经过上述 HTTP 策略，也未强制 origins，不能把它说成全量流量门禁。** 联网能力不是任意目标授权；强制出口/MITM 仍是后续切片。gVisor 未安装，不会静默回退或宣称已具备其隔离。
- 无远程 worker/高可用切换；专用数据库 advisory lock 限制单实例。已有版本化迁移：v1固定基线，v2加入操作回执；SQL校验值与结构变更同事务，未知版本或校验不符拒绝启动。业务库升级与备份/恢复仍须单独执行，见 [迁移说明](docs/database-migrations.md)。

## 验证

```sh
make test       # 后端无数据库专项单元测试；含配置/协议/连通测试模块
make test-web   # 共享组件、正式React测试和类型检查
make web        # 正式React构建
```

以上不是完整交付验收；真实Go服务、隔离PG、受控供应商/目标、浏览器及故障恢复按本轮影响范围另外核验。

持久层与跨模块测试要求**显式指定本项目专用测试库**，不能指向其他应用/生产数据。每例使用独立临时 schema，并在结束清理该 schema。

```sh
# 本项目数据库中另建 wscyber_test（一次性执行）
docker exec wscyber-postgres-1 psql -U wscyber -d postgres -c 'CREATE DATABASE wscyber_test'
set -a; . ./.env; set +a
export WSCYBER_STORE_TEST_DATABASE_URL="postgres://wscyber:${WSCYBER_DB_PASSWORD}@127.0.0.1:15432/wscyber_test?sslmode=disable"
go test -count=1 -v ./internal/store
export WSCYBER_INTEGRATION_DATABASE_URL="$WSCYBER_STORE_TEST_DATABASE_URL"
WSCYBER_INTEGRATION_DOCKER=1 go test -count=1 -v ./internal/integration
```

不要将 shell 字符串替换用在含密码的 DSN 上。上述自动化测试只使用本地 httptest、测试模型与显式开启的本项目沙箱，不调用真实付费模型或外部渗透目标。未设置测试库时相关测试跳过，不等于集成通过。用户单独授权的真实模型轻量检查与这些测试分开记录，实际结果与未通过项见 STATUS。
