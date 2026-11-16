# 组件选型、公开调研与复用记录

更新：2026-11-11。**优先复用成熟轮子，但不以框架名称、stars 或 README 成绩证明发现能力。** 已使用的依赖以实现/锁文件为准，候选不等于已安装。第1节记录本轮正式 React 前端的实际依赖核验；其余多数候选仍为历史索引调研，ARTEX 固定版本源码研究见第4节。G0 为历史实现轮次，新 runtime 镜像仍未构建运行；当前已获准逐轮实施模型配置/连通测试/研究接入及后续用量；均限受控本地验收，不调用产品真实模型/外部目标/SDL，不操作既有服务与业务数据。

## 2026-11-14：前端审核工具核验，优先用好已有工具

用户要求不仅依靠脚本，应核验成熟开源工具/插件，精准辅助功能、布局与美观审核。已复核本地Playwright Core1.65.0和agent-browser0.42.0，后者实际CLI具备元素引用、box/styles、标注截图、截图/结构diff与基于axe-core的a11y命令；只核命令/许可，本轮新页面尚未实跑审核。另按固定commit取得官方Playwright视觉比较、axe-core、Playwright CLI/MCP、Chrome DevTools MCP文档与许可证，记录隐私/环境/可访问性限制。**优先现有工具，不重复装多个浏览器控制器；自动检查/截图差异不能代替参考对照、实际看图和用户认可。** 未安装新候选/MCP/插件，未增加全量测试。完整取舍、来源和逐功能审核方法见[前端审核工具](frontend-review-tooling.md)。随后已实际用内置axe-core4.12.1只读审核当前工作流/设置，发现待修项；同时确认agent-browser0.42域限制包装丢失WebSocket静态常量，会误导AOP功能判断，已排除该工具干扰，未为迎合工具修改产品或关闭限制。详见该文最新实际使用节，不以命令存在替代本机行为验证。

## 2026-11-13：Cyber原生底座已开始直接复用（H1）

架构决定见[cyber-harness-foundation](cyber-harness-foundation.md)。独立`native/`模块已固定Cyber和AOP到`v0.0.0-20261109041610-f4b1fb2f3315`（完整commit `f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf`），通过公开版本replace处理仓库名cyber-harness与module名cyber的差异；不是绝对本机路径依赖。H1直接使用原生StandardLoop/Session/工具/事件，已通过最小build、3项定向用例及两次真实本机CLI验证，尚不是生产模型/平台业务/原生UI接线或旧系统迁移。

固定来源、go.sum、61项实际可达外部模块与许可核验记录在`native/DEPENDENCIES.md`和`native/dependency-licenses.json`：Cyber/AOP附AGPL文本，proxyclient附GPL文本，4个固定Go模块根未找到许可声明记UNKNOWN；不等于上游无许可证，不声称许可全清或可部署。两个上游扫描fork replace已显式声明，但H1入口无其可达包；不据go.mod列项声称启用扫描发行版。UI/templates子模块尚未初始化。此节优先于下面历史“当前自研薄循环”的选型方向，旧代码事实仍按STATUS/API说明。

### H2-A增量：直接复用原生Web宿主

已新增`native/cmd/wscyber-hub`，直接调用同固定提交的`pkg/web/host.Serve`，由上游拥有Web Extension/Auth/typed routes/SQLite/ServeHTTP，不自建会话REST或第二状态库。最小build、4项定向用例及真实编译CLI的本机HTTP/Connect JSON/SIGTERM关闭通过；空Hub尚无执行node/UI/真实模型。新增20项可达依赖（总81，原H1 61项版本未变）及许可增量记录在`native/hub-dependency-licenses.json`，UNKNOWN与组合义务保留。上游host日志会格式化AccessKey，本入口固定NopLogger并验证不回显，不可随意改接控制台日志器；见native/HUB.md。

### H2-B增量：直接复用原生节点/AOP回流

显式fixture-node已用原生host.StartNode、node.RunWebSocket、Harness与session.NewProtocol的薄Profile适配，完成真实Web协议派发固定本地工具、原生事件进入Hub并回读的一条链；无自建Loop/Session REST/事件表。4项直接影响用例、实际CLI及有限地址校验通过，生产UI/真实研究/完整恢复仍未接。可达模块93项（H2-A原81版本未变+12项原生console/readline/补全依赖），记录在`native/node-dependency-licenses.json`；carapace除根许可还有附加声明，含义/适用范围UNKNOWN，既有许可缺口不消除。详见native/NODE.md与STATUS。

### H3-A / H3-A2增量：原生UI固定构建与host.Static

同root提交的原生React18/Radix/Tailwind/Vite6前端，cyber-ui gitlink固定`147423bd42b665aab361409c57258d9370f75bb0`。原root锁缺4个子包所需依赖；新增可审构建覆盖，4直接+18传递版本/integrity均匹配固定UI pnpm锁，原465个非根条目完整不变，不搬入React19/Tailwind4或升级旧依赖。原始源锁、实际构建锁及覆盖摘要明确区分，详见`native/ui/{sources.lock.json,dependencies.overlay.json,build-overlay/}`。

主在执行会话退出后串行npm ci禁生命周期安装437包、完整tsc/Vite通过；11个真实产物文件经原生host.Static提供，24项HTTP/鉴权/404检查通过，临时服务正常退出清理。没有重画页面/新router/Harness，也未测浏览器/完整UI执行链。cyber-ui固定归档许可UNKNOWN保留，新增react-arborist仅见锁MIT声明、未采得npm包根部通知，通知采集不等法律全审；来源/许可与真实状态见`native/ui/README.md`、`LICENSES.md`及STATUS。大JS chunk、过期Browserslist和Recharts停维护警告如实保留，不为消警告扩大升级。

## 1. 已选组件与历史工程记录

| 组件 | 用途 | 版本/许可证 | 取舍 |
|---|---|---|---|
| Go 标准库 | API、并发、HTTP 客户端、配置 | Go 1.26，BSD-3-Clause | 当前自研薄循环，无额外 Web/任务框架 |
| [pgx](https://github.com/jackc/pgx) | PostgreSQL 驱动/连接池 | v5.12.0，MIT | 事务维护研究与执行状态；无双数据库后端 |
| [openai-go](https://github.com/openai/openai-go) | 首个模型协议适配器 | v3.98.0，Apache-2.0 | 官方SDK；显式端点/模型/凭据，兼容性须单独验证 |
| PostgreSQL | 持久状态与事务事件 | 16-alpine 本地镜像，PostgreSQL License | 单实例应用、独立卷，不把外部报告平台当权威数据库 |
| Docker/runc | OCI 工具沙箱，G0 默认 bridge，可配置 none | 历史宿主 Docker 28/runc，主要 Apache-2.0 | 保留资源/权限限制；直连未做请求拦截或 origin 强制，不冒称全协议隔离 |
| Vue/Vite/TypeScript/lucide-vue-next | 现有工程原型，非正式前端 | web/package-lock.json；MIT/Apache-2.0/ISC | 正式迁移React/antd；第三方资产单独按web内许可 |

Go依赖在前轮通过模块服务查询/下载，**不是上一设计轮次重新核验版本或构建**。临时下载代理不成为目标执行出口；不导入其他项目业务代码、数据库或凭据。

### 2026-11-11 正式前端首个切片（隔离构建，未切换服务）

正式前端已安装 React + TypeScript + Vite + 官方 `antd` / `@ant-design/icons`，用于任务入口、可靠提交核对与最小只读研究工作面。旧 Vue 源码和 `web/dist` 暂保留用于未切换的现有入口；React 产物单独输出到 `web/dist-react`。不引入 Pro/Pro Components/Ant Design X、图表库或重型状态框架。规范见 [frontend-design](frontend-design.md)，验收结果以 [STATUS](STATUS.md) 为准。

| 新增直接依赖 | 固定版本 | 用途与许可证 |
|---|---|---|
| [React](https://github.com/facebook/react) / react-dom | 19.3.1 / 19.3.1 | 正式界面与DOM渲染，MIT |
| [Ant Design](https://github.com/ant-design/ant-design) | 6.7.0 | 官方 React UI 组件与中文主题，MIT |
| [Ant Design Icons](https://github.com/ant-design/ant-design-icons) | 6.3.4 | 官方图标，MIT |
| [Vite React 插件](https://github.com/vitejs/vite-plugin-react) | 5.2.0 | 复用现有 Vite 的独立 React 构建，MIT |
| [React 类型](https://github.com/DefinitelyTyped/DefinitelyTyped) | @types/react 19.3.0、@types/react-dom 19.3.0 | TypeScript 开发类型，MIT |
| [esbuild](https://github.com/evanw/esbuild) | 0.28.2 | 测试入口打包的直接开发依赖；构建成功后才执行测试，MIT |

前端会话通过 npm 官方元数据核验版本与依赖约束；主会话核对上述实际安装包的 `package.json` 和 LICENSE 文件。实际 `antd@6.7.0` 要求 React/react-dom >=18，react-dom 19.3.1 要求 React ^19.3.1。Vite 锁定版本为7.3.8；插件5.2.0支持Vite7，候选插件6.3.0要求Vite8，故不采用后者、不强制忽略peer冲突。新增直接版本固定在 `web/package.json` 与 lock；安装关闭生命周期脚本，公开依赖下载仅临时使用进程代理，不改变运行时联网策略。这不等于完整传递依赖安全审计或已部署。

主会话另在被忽略的 `var/react-acceptance` 安装 `playwright-core@1.65.0`（Apache-2.0，Node>=20）复用系统 Chromium，仅作本轮浏览器验收工具，不进入产品依赖或执行镜像。

用户随后授权安装精准定位工具并要求可见浏览器验收。本轮固定安装 [Vercel Labs agent-browser](https://github.com/vercel-labs/agent-browser) `0.42.0`（Apache-2.0，Node>=22）于 `var/browser-tools`；核对 npm 元数据、安装包 README/许可证/启动及 postinstall 脚本，关闭生命周期脚本，仅为已有 Linux 原生二进制补执行权限。已验证 CLI 版本，以独立 namespace/session、项目内 HOME 和空配置启动系统 Chromium 可见最大化窗口，用 accessibility snapshot 元素引用完成本地登录/创建并保存截图。未接管已有浏览器、启用状态持久化/云端服务/额外模型，未修改 harness 配置。元素引用仍须在页面变化后重新获取，不能代替视觉检查。[Microsoft Playwright MCP](https://github.com/microsoft/playwright-mcp) 为本次官方索引调研的备选，未安装；没有声称穷尽或比较所有市面方案。

### 2026-11-11 MC-1配置与协议适配选型

- A已复用Go标准库`crypto/aes/cipher/rand`的AES-256-GCM保护完整配置文档，AAD绑定ID/版本；没有自制加密算法、外部秘密服务或新增依赖。pgx继续维护配置CAS与不可变版本。A单元、独立PG及正式程序启动/重启验收见[STATUS](STATUS.md)。
- B1于2026-11-12完成本地协议组件验证，继续固定`openai-go/v3@v3.98.0`。实际阅读模块`client.go/chatcompletion.go/internal/requestconfig`确认`NewClient`读取环境自定义Header，而公开`NewChatCompletionService`只应用传入选项；采用后者加显式HTTP client，不导入SDK internal包、修改进程全局环境或harness。传输禁止代理继承、重定向、自动重试与连接复用，限制请求/响应并严格校验所消费的响应字段；具体限制见[模型配置契约](model-configuration-and-usage.md)。
- 已采用固定[官方Anthropic Go SDK v2.9.0](https://github.com/anthropics/anthropic-sdk-go/tree/v2.9.0)，此前完整阅读Apache-2.0 LICENSE、go.mod（Go1.25/toolchain1.25.8）、client及option关键源码；现为直接依赖，未升级其他版本。`WithoutEnvironmentDefaults`禁止环境/凭据链继承；该固定版本`WithAPIKey`实际生成Bearer，因此显式api_key模式用公开`WithHeader("X-Api-Key", ...)`，本地fixture核验认证互斥与环境污染。SDK宽松解码由严格JSON及公开Raw/Valid字段元数据补齐，不修改SDK内部。不接Bedrock/Vertex/Managed Agents；模块声明的云/MCP依赖不等于采用这些能力。
- 两包协议/配置22个顶层测试、专项race、旧模型/研究17个顶层回归与正式入口编译通过；主核对关键实现、日志和产物SHA，未重复整轮测试。`go mod verify`通过仅证明缓存模块校验，不是依赖安全审计。B1仍是内部组件，B2持久连通测试和C任务/界面接入另行交付；本地fixture不证明全部真实供应商兼容性。

## 2. 上一设计轮次调研方法与证据强度

上一设计轮次通过 WebSearch 的官方仓库/文档索引检索；研究子任务尝试 WebFetch 获取原文时被访问验证限制阻断，未绕过。**没有完整取得各候选当前 LICENSE/go.mod/部署配置，也没有做运行验证。** 下表许可证是索引中的上游声明或明确未核验，不是最终许可证审查；版本/维护活跃度/兼容性仍需选定 release 后核验。

历史参考与上一设计轮次检索分开，不能把搜索摘要写成已深读源码或独立效果评测。当前没有新增依赖，所有采用决定都需要 P0 的窄范围验证和当次授权。

## 3. Go 研究运行时：先评估 Eino，ADK Go 备选

| 候选 | 类型与可复用能力 | 边界、代价与许可证核验 |
|---|---|---|
| [Eino](https://github.com/cloudwego/eino/blob/main/README.md) | Go框架；模型/工具接口、图编排、按需Agent组合、中断与checkpoint | 上游声明Apache-2.0；具体release/Go门槛未完整核验。checkpoint可接自定义存储，不把示例Redis当必需服务。不能替代领域状态、授权、风险出口或unknown不重放 |
| [Google ADK Go](https://github.com/google/adk-go) | Go框架；agent/tool/runner/session/artifact抽象与组合 | 上游声明Apache-2.0，部分httprr为BSD-3-Clause；索引显示/v2迁移和Go补丁版本要求，须核对实际go.mod，不能仅凭“Go1.26”判兼容。模型/部署可替换，不默认需要Google Cloud |

**评估排序，不是采用决定：优先 Eino，ADK Go 作为备选。** 选择一种或有证据地保留薄循环，不同时叠加两套调度器。框架 session/checkpoint 是运行时数据，业务授权、发送事实、研究/证据仍由平台权威记录。

采用前要用受控 fixture 证明：有界调用、结构化输出、等待可持久化、恢复不会重放可能已发送请求、总预算不因派生放大、工具实际出站仍经门禁。**框架的 tool-call 审批不等于请求级流量控制。** 不为了复用而接受默认自动重试外部副作用。

官方补充来源：[Eino checkpoint/interrupt](https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/checkpoint_interrupt/)、[ADK v2说明](https://github.com/google/adk-go/blob/main/README-v2.md)、[ADK httprr许可证](https://github.com/google/adk-go/blob/main/internal/httprr/LICENSE)。

## 4. 安全研究产品与轻量 harness 思想来源

| 项目 | 上一设计轮次/历史状态与可借鉴点 | 不直接迁入的原因 |
|---|---|---|
| [ARTEX公共fork](https://github.com/BaaRai/ARTEX/blob/main/README.md) | 上一设计轮次索引复查：持久计划、跨worker轨迹检索、目标/意图/事实/发现关联 | 应用级产品，不是SDK；上游声明AGPL-3.0，完整upstream及派生关系未核实。借鉴机制不等于复制业务代码，也不能据流量存储推断出口不可绕过 |
| [Shannon](https://github.com/KeygraphHQ/shannon/blob/main/README.md) | 上一设计轮次索引复查：有源码Web/API验证、持久workspace、接续时核对原目标 | 非Go库；文档涉及Node.js22+/Docker，源码部署涉及Temporal；声明AGPLv3及商业许可，需选版本复核。不能当全场景引擎或发现质量证明 |
| [CyberStrikeAI](https://github.com/Ed1s0nZ/CyberStrikeAI) | 历史参考，上一设计轮次未重新核验：工具任务独立ID、有界等待/取消、预览与原件分开 | 当前项目尚无完整在途取消；不因为参考过就声称已移植 |
| [Pi SDK原记录](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/sdk.md) | 历史参考：轻量会话/事件/上下文；上一设计轮次原链接未直接核实 | 上一设计轮次检索另命中[earendil-works/pi](https://github.com/earendil-works/pi)的TS/Node/Bun SDK及MIT声明，迁移归属未独立证实；不是Go原生，不作为基座首选 |

Shannon补充来源：[workspace/恢复](https://github.com/KeygraphHQ/shannon/blob/main/docs/workspaces.md)、[部署声明](https://github.com/KeygraphHQ/shannon/blob/main/docker-compose.yml)。README结果未用作独立能力证据。未完成X.com专项核验，不声称已调研。

### 2026-11-10 补充：ARTEX 任务启动与提示词原文核验

本次已通过公开 GitHub API 读取 BaaRai/ARTEX 固定 commit `923c4a813ad2ae6cb91b0799f8b9104e2437175b` 的相关源码，校验 blob SHA 并记录 SHA256；与上表“上一设计轮次仅索引”的历史状态分开。核验了 description/goal 表单与 handler、启动准入/目标拆解、planner/worker 提示词和装配、调度回流，以及报告/复测提示词；不是全仓审计或运行效果验证。

源码表明普通任务靠角色规则、动态上下文、真实工具写回与事件调度推进，而非单一万能提示词。建议学习目标与执行步骤分开、有限方向、先消费已有流量、观察/推断分离、及时保存和反证；不照搬双图、跨任务共享资产、固定多角色或长报告格式。用户要求的两个主要输入及采用边界见 [research-start-and-prompts](research-start-and-prompts.md)，内含固定版本原文位置。

该版本 LICENSE 为 AGPL-3.0；README 自述为 Autumn-27 ARTEX 的英文翻译 fork，完整派生/版权关系未审计。`go.mod` 使用 Norma 并 replace 到 BaaRai/Norma；未读取完整依赖许可、未安装/执行，不能据此选择本项目运行时。源码快照仅作临时只读研究材料，不属于产品依赖或拟提交应用代码；本项目文档以原创归纳记录机制。

### 2026-11-11 补充：挖掘引擎机制核验

用户明确要求先回答Agent架构如何实现质量、效率、深度和覆盖。本次补读Strix固定commit `685bbc8138d35fc9fe378ac37ec0d9198b9c46a3` 的root职责、覆盖记录工具及覆盖警告测试源码；Shannon固定commit `804b80636d5022f06dfd6242744610e7521c8f21` 的工作流相关片段及覆盖说明。GitHub API取得tree/blob、校验blob SHA并记录SHA256，只读且未执行/安装。具体来源、采用与不采用机制见[研究引擎E1](research-engine-design.md#2-优秀项目实际学到了什么)；用户已明确稳定优先、模型自主探索，不采用强制覆盖账本/深度门槛作为首版前置。不把Agent自报覆盖、上游评测宣传或固定分类流水线视为效果保证；目前没有本项目真实研究质量对照结果。

本轮范围仍为设计确认与独立模板；用户已将新增请求风险控制/审批、真实外部对接延期，以下出口候选保留长期参考，不是当前首版强制前置。没有选择新运行时、复制上游应用实现或新增依赖。

## 5. 出口与隔离：优先复用，不混淆职责

| 候选 | 适合复用 | 使用前必须解决 |
|---|---|---|
| [goproxy](https://github.com/elazarl/goproxy) | Go HTTP/HTTPS proxy、请求/响应hook与MITM；索引声明BSD-3-Clause | 不是现成不可绕过出口；持久等待/恢复、TLS上下文、强制路由、策略与证据自己对接 |
| [mitmproxy](https://docs.mitmproxy.org/stable/mitmproxytutorial-interceptrequests/) | 成熟拦截/修改/丢弃请求与协议解析，候选独立流量组件 | 非Go嵌入库，增加进程/服务边界；完整许可证上一设计轮次未取得。其协议能力不等于本平台已支持，也不等于业务风险分类已完成 |
| [gVisor](https://gvisor.dev/docs/) | 后续增强系统调用隔离 | 历史记录宿主未装runsc，上一设计轮次未探测；不是网络审批器，须明确安装验证，不静默回退 |
| [OpenSandbox](https://github.com/alibaba/OpenSandbox) | 历史候选：生命周期/多节点及Go SDK | 上一设计轮次未重新核验；单机需求未证明不足时不引入额外服务栈 |

G0 已默认开放工具 bridge 网络；这与已完成强制出口不是一回事，直连尚未被平台 HTTP 的 `request-signals-v1` 或 origins 约束。G1/E1 仍须冻结并验证强制 egress、DNS、TLS、待审 continuation、超时与 unknown 不重放。仅设置 HTTP_PROXY 不算强制；未来支持/拒绝的协议边界要明确，不能把当前直连写成已检查流量。

[mitmproxy协议说明](https://docs.mitmproxy.org/stable/concepts/protocols/)与[证书说明](https://docs.mitmproxy.org/stable/concepts/certificates/)表明协议/模式和证书信任均有条件。pinning/mTLS/QUIC等必须单独声明支持边界；其对未支持连接的跳过能力不能在本平台变成绕过检查的默认配置。

## 6. 场景工具：工具、规则与研究结论分开

| 候选 | 适合的窄场景 | 许可证与能力边界 |
|---|---|---|
| [Semgrep CE](https://semgrep.dev/products/community-edition/) | 离线代码模式分析，作为审计线索/可复用工具适配器 | 引擎声明LGPL2.1；官方维护规则另有Semgrep Rules License，不能把引擎许可证套给规则库。规则商业分发/服务化限制须逐项核验；CE不等于商业跨文件分析能力，结果不直接确认漏洞 |
| [Nuclei](https://github.com/projectdiscovery/nuclei) | 明确授权范围内的模板化验证工具候选 | 引擎及[模板库许可](https://github.com/projectdiscovery/nuclei-templates/blob/main/LICENSE.md)索引声明MIT；仍需锁定引擎/模板版本、审核具体行为与网络需求。不能默认全量模板运行、模板自更新或放过其多协议流量 |

Semgrep官方来源：[引擎/规则许可区别](https://semgrep.dev/blog/2024/important-updates-to-semgrep-oss)、[规则许可](https://semgrep.dev/legal/rules-license/)。上一设计轮次没有执行任何扫描器，不能据工具镜像中可能存在某工具就宣称集成完成。

代码审计先使用固定commit和授权工作区，分析/构建与材料读取分离；CTF按题型引入已有工具而非新造求解器；内部演练只在清楚的授权/时窗/停止条件下扩展能力。工具输入、输出、网络需求和证据解析使用同一适配器契约，不为场景复制平台内核。

## 7. 发现、报告与 SDL 轮子

| 候选/标准 | 推荐复用方式 | 边界与核验状态 |
|---|---|---|
| [DefectDojo API v2](https://docs.defectdojo.com/automation/api/api-v2-docs/) | 可选外部漏洞管理连接器/导入去重参考 | 不默认部署、不假设内部SDL就是它；完整许可证上一设计轮次未取得。导入/重导入并非本地稳定编号的替代 |
| [SysReptor](https://docs.sysreptor.com/designer/designer) | 评估报告模板/HTML-CSS/PDF服务，避免自研排版引擎 | 非Go库；有Community/Professional能力区分。索引为自定义Community License且版本出现1.0/1.1差异，当前全文未确认；不能按MIT/Apache随意嵌入或分发 |
| [SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/sarif-v2.1.0-os.html) | 代码分析结果交换，做扫描器导入/导出 | 标准不是完整渗透报告/SDL工作流；需保留工具/规则/位置/指纹语义，不强塞所有动态证据 |
| [CWE](https://cwe.mitre.org/about/) / [CVSS 4.0](https://www.first.org/cvss/v4.0/specification-document) | 弱点分类与可追溯严重性表达 | 不提供本地漏洞实例编号，不自动生成CVE；记录版本、向量、分数和依据，CVSS不等于组织业务风险 |

**重要集成陷阱：** DefectDojo的[reimport](https://docs.defectdojo.com/import_data/import_intro/reimport/)可以关闭本次未出现的旧发现。自主研究通常是增量/局部材料，连接器必须显式配置避免把“本次没观察到”变成“已修复”。其[去重配置](https://docs.defectdojo.com/triage_findings/finding_deduplication/os__deduplication_tuning/)受解析器/设置影响，不能替代本地不可变ID与历史。

SysReptor来源：[API/版本FAQ](https://docs.sysreptor.com/faq/application)、[许可资料](https://github.com/Syslifters/sysreptor-docs/blob/gh-pages/license.md)；CVSS解释见[用户指南](https://www.first.org/cvss/v4.0/user-guide.html)。没有测试API、渲染实际报告或传输项目材料。

本地保留最小Finding/Verification/Report/Delivery权威，先结构化JSON/Markdown，后按需要接成熟渲染器和真实SDL协议。详见 [findings-and-reporting](findings-and-reporting.md)。

## 8. 本地镜像复用与当前共享配方

| 新标签 | 已核实来源 | 本地 image ID | 用途 |
|---|---|---|---|
| wscyber/toolbox-base:local | arena-task-executor:local | sha256:e9e8686b2b1ee45cee807229bbb2e6075187cce9fe5f82820b27e327152b8f65 | 只复用工具层，不运行来源应用入口 |
| wscyber/toolbox:local | 历史 deploy/toolbox/Dockerfile，从 toolbox-base 派生 | sha256:f06b53fe7e6d3c02e46badedcf14b88f36b4aae149424fd56c0c8c8b10bad8c9 | 历史无网络命令验收；当前不再生成此标签 |
| wscyber/runtime:local | 当前 deploy/toolbox/Dockerfile，toolbox-base + 本项目 Go/web 产物 | 本轮未构建，无新 ID | G0 默认共享镜像；不是已运行能力证明 |
| wscyber/postgres:16 | postgres:16-alpine | sha256:de3a4eab8fdfa507ea92aac488b916b08089e515db49b055fe71dfa271ba3a28 | 独立项目数据库 |
| wscyber/node:22 | node:22-bookworm-slim | sha256:fc8cd9deea7389d01d9a70cc83a5d09465c2050f2ae322d67300a9794433edad | 可选构建基础层，前轮使用本机Node |
| wscyber/traffic-base:local | arena-traffic-gateway-dependencies:s0 | sha256:b048a9b418c9590f30274650b5834ce0c7f5561d80b7f93da8f3b830dc2dd23e | 后续候选，上一设计轮次未运行 |

表中已记录 ID 沿用历史本机 image ID，**不是 registry manifest digest**；不将其当成本轮新构建结果。来源标签保留；不运行来源应用入口。基础层可能含旧应用文件，不等于迁入源码；公开分发前须制作独立可审计镜像并逐工具核验许可证，本次不发布镜像。

当前 `make image`/兼容别名 `make toolbox` 仅生成一个自定义 `wscyber/runtime:local`，不按场景拆镜像；使用已有缓存构建 Linux/CGO=0 Go 程序与 web，再 COPY 到工具层，重置入口、默认命令和健康检查。PG 继续单独持久服务，Node 镜像不作为当前构建必需项。`.dockerignore` 收窄上下文；本轮只做配方静态/dry-run 检查。容器控制面执行命令仍需 Docker CLI 与标准 socket 权限，基础层是否有 CLI 未核验，宿主 `make run` 保留。

## 9. 采用前检查表与停止点

- 选定release后核验源码/许可证全文、依赖/Go版本、维护与安全更新、引擎/规则/模板的不同条款。
- 定义复用边界、数据流、秘密和对外请求、可替换接口，不因引入框架让它接管授权和事实权威。
- 用受控实验验证中断/恢复/副作用、数据格式和真实资源开销，再锁定依赖；不凭搜索索引直接安装。
- “现成轮子”不意味着把完整产品堆在一起；新增服务应解决已出现的需求，不预建消息/多节点/记忆平台。
- 上一设计轮次到公开调研记录与建议排序为止；G0 仍未集成框架/代理/报告组件，后续 P0/G1/E1 范围见 [roadmap](roadmap.md)。
