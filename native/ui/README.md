# H3-A2：固定原生 UI 最小依赖覆盖与静态宿主

**H3-C后续更新（2026-11-14）：** 复用此固定构建，已真实通过UI登录/节点就绪/单次创建运行→固定工具→原生Workflow参数/结果及持久事件回读，5阶段图已由主查看。未改产品/构建，原生实际显示分支与失败修正、护栏/IOA/工具公告等限制详见[EXECUTION-ACCEPTANCE.md](EXECUTION-ACCEPTANCE.md)。不等生产研究、完整业务或视觉认可；最终统一预览的当前存活状态见STATUS。

**H3-B后续更新：** 真实浏览器原生登录、空会话列表/侧栏收展、Agents空抽屉开关已完成本机验收，7阶段截图已由主实际查看。空Hub仍有GetConfig400红提示和IOA3个404，完整业务/非空详情/终端及UI→节点执行未验；临时运行资源已清理，无常驻新预览。详见[BROWSER-ACCEPTANCE.md](BROWSER-ACCEPTANCE.md)。下文“浏览器未验”是H3-A2当时范围，不覆盖此后有限验收，也不能据后片称完整UI已认可。

2026-11-13。**H3-A2已完成本机真实构建与静态资源HTTP验收。** 执行会话正常退出后，主串行安装437包、完整TypeScript检查和Vite构建通过；真实Hub的24项HTTP检查通过，服务exit0、私有runtime清理及端口释放已核验。**浏览器、逐功能交互/展开态和完整业务仍未验收**，静态可提供不等于这些能力已完成。

前轮H3-A的512MiB堆失败及17条类型诊断保留在历史记录。本轮只补固定依赖并使用768MiB进程堆，不改源码类型/页面、不跳过tsc；此前17条诊断在实际完整类型检查中已消除。Go定向测试仍仅验证合成边界，真实资产验收使用另行构建的原生UI。

不改页面/布局/主题/图表，不混装旧 Ant/React19，不建立第二 router/Harness。沿用 Cyber 原生 React18/Radix/Tailwind/Vite6、原生插件组合 `src/plugins/composition.ts` 和 `host.Config.Static`。这是未改上游页面的技术基线，不是最终品牌、全部业务能力或视觉验收。

## 固定源与准备

- root：`f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf`。
- cyber-ui：gitlink `147423bd42b665aab361409c57258d9370f75bb0`，不跟随 main。
- **原始来源锁**：[sources.lock.json](sources.lock.json)，保持H3-A原字节不变。**显式构建覆盖**：[dependencies.overlay.json](dependencies.overlay.json)及[build-overlay/package.json](build-overlay/package.json)、[build-overlay/package-lock.json](build-overlay/package-lock.json)。不把覆盖后的package/lock称为上游原件。
- 许可事实/UNKNOWN：[LICENSES.md](LICENSES.md)。本片未新增 Go 模块，原生 Go 模块图不变。
- 脚本：仓库根 `scripts/cyber-ui-build.py`，Python ≥3.11，**必须 `python3 -I`**。只用标准库，不将归档目录加入 Python 模块搜索路径。Node 固定本机已装 `v24.16.0`、npm `11.16.0`；脚本不会替换工具链或改全局配置。

在仓库根运行（`WORK` 必须不存在，不能先 mkdir 它）：

```bash
mkdir -p var
WORK="$PWD/var/cyber-ui-manual"
# fresh checkout：通过两个固定公开来源取源，验证采用文件树/归档/lock。
# 可选 --root-repo /某只读git仓库，仍只导出固定提交，不依赖其工作区/HEAD。
# 可选 --ui-archive /某缓存归档，仍验证固定SHA256，不当它是可信目录。
timeout 180s env -i PATH=/usr/bin:/bin HOME="$PWD/var" LANG=C.UTF-8 \
  python3 -I "$PWD/scripts/cyber-ui-build.py" prepare --workspace "$WORK"
```

`prepare` 只下载/解包/核验/复制，并在**构建副本**显式应用两个覆盖文件；不执行 npm、源码脚本或构建，不重新解析锁。每份下载与解包独占新目录，拒绝路径逃逸、符号/硬链接、特殊/重复文件，限制下载/展开数量和总量。分别验证原始inputs树与仅两文件变化的实际构建树、固定来源/构建锁、真实 Wasm magic 与无 LFS 指针；任何其他源码/tsconfig/子模块变化都拒绝。Git 只读导出在净环境运行，归档内容不执行。

目录结构：

- `inputs/{root,ui}-download/`：各自的归档；`{root,ui}-extracted/`：各自解包结果。
- `source/`：独立构建副本，仅 `web/frontend/package.json` 与 `package-lock.json` 应用上述固定覆盖，其余页面/tsconfig/UI内容不改；原样来源在 `inputs/*-extracted/` 保留。Vite 的 outDir **及 closeBundle `.gitkeep` hook** 都只写 `source/web/static`，不会写原上游/模块缓存。
- `home/`、`tmp/`、`npm-cache/`：本轮进程环境/缓存；不挂到静态根。
- `prepared.json` schema2：准备结果、原始/实际构建锁及覆盖哈希、应用覆盖后的构建树摘要，明确 `node_build_run=false`（准备收据不是构建状态记录）。H3-A旧schema1工作区拒绝作为本片输入；不原地修改失败现场或复制其node_modules。本轮也不复用/写入旧npm缓存，主将使用新workspace缓存。

本轮实际使用只读本地 Git + 已下载且校验的 UI 归档准备成功；**fresh-checkout 的 root 公网归档下载分支没有实际运行**。两分支对所采用文件做同一树摘要检查；不把代码支持写成空缓存新机器复现已通过。

## H3-A2最小闭包与锁语义

固定UI `pnpm-lock.yaml` 的file-manager importer给出：`@radix-ui/react-checkbox@1.3.8`、`react-arborist@3.15.1`、`react-hotkeys-hook@4.6.2`、`react-resizable-panels@2.1.9`；traffic也引用最后一项。版本满足子包声明，源码有真实导入。只采用这4项精确版本，不搬入该pnpm workspace的React19/Tailwind4/devDependencies。该UI自己的npm锁仅8项且不含4缺项，不能作为版本依据。

一次有界 `npm install --package-lock-only --ignore-scripts --no-audit --no-fund --include=optional` 成功：原466条目→488，原465个非根条目**完整JSON不变**，根包只加4依赖；新增4直接+18传递，全部沿这4项的依赖可达，全部版本/integrity匹配固定pnpm锁。checkbox需要的5个较新Radix内部包嵌套在checkbox下，**没有升级根目录已有Radix版本**；Redux4/5并存是DND与arborist各自明确依赖，不能扁平强制替换。React/ReactDOM均仍18.3.1，无新增生命周期脚本，无force/legacy-peer-deps。

- 原始root npm锁 SHA256：`23da6a44f3f816986cf2768c454ea9071b0078654a688274797825c10bf48ea0`。
- 实际构建npm锁 SHA256：`996a5ab5c64b6ac622f2d7eb170d7422646d25f7bbbdd5bf9b48535214890416`。
- 覆盖审计 SHA256：`b4f3a409e4b98d81b6cfff7a63414080f4e9b8eb09352c07b4f6be2c592c2528`。

此处“覆盖”是复制两个已审文件，不是npm overrides配置。正式审计含逐项必要来源、integrity与许可证声明。补充锁不等同于安装/类型检查通过；锁生成工具与过程记录在本轮 `lock-generation.json`，没有运行包生命周期或生成node_modules。

## 构建：只由主在执行模型退出后串行运行

```bash
# fresh checkout时创建同一个锁位置；本轮已经存在，不创建另一把锁并行。
mkdir -p var/full-system-models.Pg2PL1Id
touch var/full-system-models.Pg2PL1Id/heavy.lock
# 脚本内部独占上面的同一heavy.lock，锁忙立即失败，不重复套flock自锁。
env -i PATH=/usr/bin:/bin HOME="$WORK/home" LANG=C.UTF-8 \
  python3 -I "$PWD/scripts/cyber-ui-build.py" build --workspace "$WORK"
```

构建阶段先重新校验来源与工具版本，然后只执行：

1. `npm ci --ignore-scripts --no-audit --no-fund --include=optional`（600 秒边界）；
2. 用 Node 直接调用固定安装的 `typescript/bin/tsc`（240 秒）；
3. 用 Node 直接调用 `vite/bin/vite.js build`（360 秒），等价于固定 root 的 build，而不执行 npm pre/postbuild。

使用全新 HOME、空 user/global npm 配置、固定公共 registry、独立缓存、禁浏览器下载；不继承 key/Cookie/代理/NODE_OPTIONS/用户 npmrc。仅进程级 `--max-old-space-size=768`、`UV_THREADPOOL_SIZE=2`、npm maxsockets=2；**软目标不是硬内存沙箱**。前台子进程组用于超时/中断精准收尾，不另开 CLI 或后台重型工作。包脚本/tsc/Vite 本身仍是执行第三方代码，不承诺强隔离。

本片覆盖组合已在上述固定工具链/768MiB堆下完成一次真实安装与完整构建；前轮原锁失败证据仍保留。失败后保留workspace/日志，不自动重试、不解除ignore-scripts、不换版本/补假生成物。已有node_modules/release的workspace不会被脚本隐式清空；需经主审查后另准备新目录。fresh-checkout公网取源分支仍未单独验证，不将本机成功等同所有环境已通过。

成功后预期产物：

- `$WORK/release/public/`：**仅**真实 Vite index、assets 和无秘密的 `.wscyber-ui.json` 清单。脚本不复制 key/state、HOME、源码、node_modules、source map 或下载目录。
- `$WORK/release/build.json` schema2：构建/工具链/清单与原始锁/实际锁/覆盖哈希，HTTP/browser 均仍为未验。
- `$WORK/release/provenance/`：公开根之外保存原始/实际package及lock、来源锁与覆盖审计原件；不作为静态资产公开。
- `$WORK/release/{notices/,dependency-notices.json}`：许可证原件/来源说明与已安装包元数据，位于公开根外；存在这些文件不表示许可 UNKNOWN 已解决。

## Go 宿主

新增 **`--ui-dir "$WORK/release/public"`**；保留已有 `--local-hub`、loopback、非空私有 key、全新 state-dir 门禁。key 与 state 必须在 public 之外；不把整个 WORK/项目根/私有状态目录当静态根。

- 未配置 `--ui-dir`：保持 H2-A 空 Hub / H2-B 显式 fixture-node 的默认语义，根路径仍 404。
- 配置后：清单只接受schema2，固定核对 `source_package_lock_sha256`、`build_package_lock_sha256`、`dependency_overlay_sha256`；旧schema1、缺字段、原锁冒充实际锁、错误覆盖均拒绝。H3-A旧Hub二进制不兼容新清单，本片必须消费重新编译的Hub。再校验每个普通文件 SHA256、index 引用的真实 JS 条目、资源种类及总体大小；拒绝缺失/多余文件、链接、特殊文件、key/state 包含关系（包括父目录符号链接别名）。清单是完整性记录，**不是签名或恶意本机管理员隔离，也不是“看见清单就证明UI验收”**。
- 启动时验证的白名单 FS 直接赋给原生 `host.Config.Static`；`os.Root` 防路径逃逸，只开放登记文件，清单/目录/后来添加的私有文件不公开。运行时不得原地更改资源；更新应停止本轮服务并显式选择新构建目录。
- 原生 SPAFileServer 本身对缺失路径返回 index。通过原生 `extension.Add(web.Route)` 添加两个低优先级 fallback（`/{service}`、`/{service}/{rest...}`），仅把未匹配 `cyber.rpc.*`、IOA、缺失 asset/文件、隐藏路径和非 GET/HEAD fallback 变为 404，其他 SPA 路径仍委托**同一原生 SPAFileServer**。无第二 mux/鉴权层。具体已注册 API/Connect 路由更优先，仍使用原生鉴权。
- `ready.frontend=true` 仅指已校验并挂载静态资源，不表示浏览器/页面业务功能就绪。原生 UI 内尚无服务支持的扫描/配置/IOA等功能不因静态可访问而变成已实现；不补假 API。

运行命令沿用 [../HUB.md](../HUB.md)，只额外加 `--ui-dir`。不要将 key 原值放入 argv，不打开真实 provider/目标/部署。

## 主的真实资产 HTTP 接续入口（本执行者未运行）

```bash
# Hub二进制先前已独立最小构建，不在HTTP服务旁再次编译。
env -i PATH=/usr/bin:/bin HOME="$WORK/home" LANG=C.UTF-8 \
  python3 -I "$PWD/scripts/cyber-ui-http-check.py" \
  --binary /本轮独立产物/wscyber-hub \
  --ui-dir "$WORK/release/public" \
  --evidence "$WORK/http-check"
```

脚本内持同一 heavy.lock；独占新证据目录和私有 runtime，净环境启动一次**无 fixture-node**的真实编译 Hub。串行读取 index/deep-link/全部登记 assets（核对字节哈希、类型/缓存）、原生 manifest/SessionService 鉴权，以及未实现 RPC/IOA/缺失 asset 的非 HTML 404。随后 SIGTERM、等待退出并删除本脚本自己的 key/state/HOME/tmp，保留结果与无秘密日志。**它不启动任务/模型，不打开浏览器，也不证明页面交互或 H2-B 执行链通过。** 若脚本/Hub失败不能记成功；主须审阅结果/清理事实。

## H3-A2执行阶段实际验证

- 唯一lock-only 9秒成功，未实际安装；22新增项的固定pnpm版本/integrity、依赖可达性及465个旧非根条目完整不变通过。
- 新独立prepare成功：本地固定Git只读导出+前轮固定UI归档校验复用；前轮source/node_modules/npm缓存不修改。
- 30项Python纯检查通过：原始/应用覆盖树、仅2文件变化、收据、Go/构建/HTTP脚本固定值一致，旧/错/缺来源字段拒绝，原始/构建锁篡改及源码/tsconfig/额外文件拒绝，净环境及临时目录删除。没有调用HTTP driver。
- 仅2个Go顶层用例：`TestUIAssetsBoundary`（20子例，含schema1、原锁冒充实际锁、错误/缺失覆盖拒绝）和 `TestUIStaticNativeRoutes`（既有22条合成静态/原生鉴权HTTP断言），全部PASS。临时Hub/库/key随用例清理。这不是原生UI真实资产HTTP。
- 因Go清单有变化，唯一 `go build -p 1 -mod=readonly` 生成本轮新Hub成功；同heavy.lock、净环境、GOPROXY=off，未新增Go依赖；不复用旧binary。Go临时目录为空后删除。
- 未运行npm ci/tsc/Vite/publish/真实资产HTTP/浏览器/PG，不重跑H1/H2整链或默认无UI专项。主后续入口 `var/cyber-h3a2.pba2tbpi/main-next.py` 在旧CLI身份仍存在时拒绝启动；主正常退出执行会话后，一次实际安装/编译，成功才HTTP，任一步失败即停。

## H3-A2主接续实际验收与清理

执行CLI2932508和终端2932477均正常exit0后，主独占执行一次新闭包安装/编译/HTTP（任务bxyoo4si4，exit0）：

- npm ci禁生命周期安装437包（50秒），完整tsc通过；没有再次加堆、改页面/类型或跳过检查。
- Vite6.4.3转换4684模块，14.50秒完成；生成index+10个JS/CSS/WASM资源，均是真实原生构建产物，来源清单schema2与原锁/实际锁/覆盖一致。
- 编译Hub PID2988400/start7065930在`127.0.0.1:36155`运行，无fixture-node/生产模型。24条HTTP检查通过：index/deep-link、全部10个资源的类型/哈希/缓存，原生manifest/SessionService的401/200，未实现RPC/IOA及缺失资源非HTML404；私有清单/key/state不可公开。
- SIGTERM exit0；主独立核验精确服务进程消失、端口可重绑、私有runtime/key/SQLite/HOME/tmp不存在、重型锁空闲。没有常驻新服务；保留binary/真实release/固定源/依赖缓存作为后续材料，旧入口与在用预览保持。
- `release/build.json`中的`http_verified=false`是构建时点收据，不回写历史；后续HTTP结果以同清单hash的`http-check/result.json`为准。主最终核验为`var/cyber-h3a2.pba2tbpi/main-final-check.json`。
- 实际通知清单487条：437已安装、50跨平台可选未安装；421个已安装包采得根部通知，新增22包中21有根部通知。`react-arborist`未采得根部通知，虽锁声明MIT仍需补核；不等于整个上游无许可或已解决分发义务。
- 非阻塞警告保留：Node url.parse弃用、Recharts2停维护、Browserslist数据较旧，以及主JS压缩前约2.13MB超过900kB提示。未为了消除警告升级固定依赖或提高阈值；无浏览器性能结论。

浏览器登录/导航/详情/展开态及UI→节点完整执行链仍未测；本片不是视觉批准或完整业务迁移。没有部署、旧库迁移、真实目标或付费产品模型请求。

## H3-A前轮执行阶段历史验证

- 固定 UI 有界下载/安全解包；固定 root 只读 Git 导出；`prepare` 成功。
- Python 语法、准备源树复核、净环境字段白名单、路径逃逸/绝对路径/符号链接/硬链接归档拒绝检查通过；未执行构建/HTTP driver。
- 唯一 Hub `go build -p 1 -mod=readonly` 成功；同 heavy.lock / GOMAXPROCS=2 / GOGC=30 / GOMEMLIMIT=384MiB、净环境、复用共享 Go 缓存且 GOPROXY=off。
- 只跑 3 个直接相关 Go 用例：`TestUIAssetsBoundary`（13 子例）、`TestUIStaticNativeRoutes`（合成资源+真实临时 loopback Hub，22 条HTTP断言）、`TestLocalHubNativeAPIAndShutdown`（默认无UI回归）。全部 PASS。临时 Hub/SQLite/key/测试目录随用例关闭清理。
- 上述执行会话未运行npm/前端构建；主接续结果见下节。全程未运行浏览器、PG、全仓/整链回归、生产provider、真实目标、部署、迁移、commit/push。

## 主接续实测：构建未通过，H3-A按阻塞收束

执行CLI/终端均正常退出0后，主在同heavy.lock下完成一次`npm ci --ignore-scripts`，安装415包。首次tsc在512MiB堆上限SIGABRT；主核对没有残留构建进程，单次诊断后只将进程堆改为768MiB，重新校验700个原始源码文件并复用依赖，续编一次。该次tsc没有再报堆错误，但退出2：9条TS2307与8条TS7006，共17条诊断。

固定root package/lock缺少以下由固定cyber-ui子包声明并实际导入的依赖：`react-hotkeys-hook`、`react-arborist`、`@radix-ui/react-checkbox`、`react-resizable-panels`。相关回调也报隐式any；是否全部随依赖补齐消失尚未验证。不使用any占位声明或跳过tsc来伪造成功。

两次失败日志保存在`var/cyber-h3a.67eXzJEB/{node-build.log,node-compile-768.log}`。Vite、publish、真实资产HTTP和浏览器均未执行，无release/HTTP runtime。固定来源与上游未改，保留源/依赖/cache及已核验Hub二进制供下一依赖兼容短轮消费，非遗留服务。下一片需将最小依赖补充与版本依据形成独立可审锁/覆盖记录，不移动上游commit或偷偷改原锁。
