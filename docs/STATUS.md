# 当前状态与接续

## 0.97 候选分栏与滚动收尾已验、预览已更新；按用户要求暂停

2026-11-15。用户最新要求“执行完这轮先暂停，快速收尾”。本轮只完成候选布局及最后滚动修补，**不启动执行复盘下一轮**，后续须等用户恢复指令；0.96的连续推进安排暂止于此，不将三列推广为全站规范。

- 最后窄修仅候选组件及覆盖清单；tsc/Vite通过（18.30s，既有Browserslist/chunk警告保留）。隔离副本定向实测：纯DOM加长列表的120px位置跨宽窄后仍120；窄屏列表再滚80后回宽屏仍80；展开原件的详情400→返回列表→键盘重入仍400，编号/焦点及520 CSS无横向溢出通过。原件内部只做了子滚动事件探针（该短原件本身无可滚距离），不冒称长报文滚动全矩阵。首次引用按钮检查因无障碍名称与可见“[1]”不同而超时，改实际按钮选择器后通过；无产品改动。0业务写/0外部请求/0pageerror，主已看宽屏、窄屏及现用预览图。
- 隔离Hub1100164、Chromium1100922正常退出0。原Hub848341已停止，日志确认无活跃进程；复用原binary/state/模型，只切本轮UI，无加表/重编后端/重放研究。**现用入口 http://127.0.0.1:42453 ，Hub1112460/start2598965（后台`bz360d0jl`），UI=`var/findings-layout.ypAY1kni/release/public`。** 用户Chromium93373（`bf7iwxyh7`）保留原3页及草稿，另开第4页展示候选三列；2043 CSS实测列表352、详情1403、正文960，标题top110.4。
- 原库仍2任务/11HTTP/3候选/49事件/0验证/0验证结果；没有新真实模型或目标请求、commit/push。验收及归属在`var/findings-layout-scroll.r3kqhq93/{scroll-check.json,live-browser-result.json,ownership.json}`。唯一执行会话Terra1040563已停写空闲，但可见终端1040554含用户未发送文字，故未清空/关闭该窗口；无后续派发或后台开发。保留可操作预览。

## 0.96 用户批准参考建议继续：先候选布局，后独立处理执行复盘

2026-11-15。用户在布局分析后明确“先参考建议继续，执行后面的轮次”。本轮已获准做候选宽屏主从布局试片，但不是把三列改为全站规则；执行复盘将在候选验完后另成一片。保留原生组件与业务数据/授权/提交/证据/导出语义，不再累加字号放大。

唯一新可见Terra+xhigh`wscyber-terra-findings-layout`（CLI950607/终端950588，后台`bah38b1dz`，包`var/findings-layout.ypAY1kni/`）已启动，主已核实际型号/effort。写权仅候选组件、必要新CSS/翻译/清单：根据实际可用宽度将内部“候选列表｜详情证据”横向组织，连同既有任务导航构成宽屏三列；正文在详情栏左对齐适当限宽、原件区可展宽；窄屏列表/详情切换，保留同对象阅读位置与展开，换对象从页头读，键盘焦点与现有功能保持。无API/Go/新依赖或执行复盘写权。首版已静态交回并正常退出0，tsc/Vite通过（24.15s）；主在既有隔离副本实核1440/2043 CSS分栏、520列表/详情返回445px阅读位置及证据展开保留/同修订零重取、键盘焦点、换候选归顶、证据[n]定位、Dialog只开关及导出入口。0业务写/0外部请求/0pageerror，主已看宽屏/窄屏3图。同2043×987 CSS只读基线：旧正文容器768、标题top268.9；新阅读区960、标题top111，原件区可用更宽空间。

另一次**纯DOM临时加长滚动空间**检查（无新增业务候选、探针已移除）发现宽屏列表scrollTop120→缩窄返回后0；不能把普通主路径通过当全部滚动通过。隔离Hub1003275/Chromium1005291均已正常退出0；唯一新Terra+xhigh短包`var/findings-layout-scroll.r3kqhq93/`（后台`b1f7b8j0k`）仅补滚动缓存/恢复及窄屏当前编号，尚待交回。旧42453 Hub848341/用户浏览器93373/当前release及原库仍保持不动，未应用本轮新布局。

## 0.95 候选草稿导出已验并展示；布局建议仅分析，未开工

2026-11-15。固定修订的纯前端Markdown草稿已完成：动态文本围栏、来源时点/证据索引、sent三态与两种截断、原件字段排除；一组纯fixture通过，恶意HTML/图片/链接/反引号在Markdown AST中无活动节点。Terra+xhigh交回并正常退出后，tsc/Vite通过（14.78s）。复用已有隔离副本、未配置模型，实际下载WSC-000001-r1-draft.md（6503字节）与WSC-000002-r1-draft.md（3680字节）；导出A后快速切B仍各自匹配所读ID/revision/time。0原件GET/0业务写入/0外部请求/0pageerror，桌面/520 CSS窄屏已主看图。仅本地确定性候选草稿，不是新LLM报告生成、持久报告状态或正式发布；候选原文仍可能含敏感内容，不承诺全面脱敏。

隔离Hub824068/Chromium826638均正常退出0。核现用预览仍无新执行后仅切前端产物：旧Hub718122正常退出，复用同一binary/原state/模型配置，没有再加表、重编后端或例行复制历史。**当前42453 Hub848341/start1977811（后台`b3f4cdwfk`），UI=`var/finding-draft-ui.Ed1pdHq2/release/public`**；Chromium93373（后台`bf7iwxyh7`）保留旧2页，另开第3页展示“导出候选草稿”，主已看live图，未在用户页额外下载。原计数仍2任务/11HTTP/3候选/49事件/0验证/0结果。交付/下载文件/截图与准确归属见`var/finding-draft-ui.Ed1pdHq2/{result.md,pure-check.json,check/result.json,live/ownership.json}`。无新模型/目标调用或commit/push。

**用户最新布局反馈：** 正文两侧留白和执行复盘有效占比偏小，提出可考虑“任务—列表—正文”三列，但明确只是建议，并非批准统一改规范或以后强制采用。本主仅对照当前候选/执行复盘截图和限宽样式分析，未改UI代码、未派发重构。下一步布局决定应按对象选择/正文阅读/流程观察分别判断，不能把本建议当全站三列要求或已获视觉认可。

## 0.94 连续推进下一小片：Sol xhigh只读核对原生报告/导出复用

2026-11-15。0.93升级和保全核对完成后，按用户继续逐轮要求接下一小片，不停等视觉认可、不重验独立验证底座。唯一新可见`wscyber-sol-report-reuse`（CLI747620/终端747601，后台`b9katyhkn`，包`var/native-report-reuse.BdmGKwJG/`）已实际读包复述；主核实`gpt-6.1-sol --effort xhigh`，沿已有GPT开发host、不改全局或产品模型/凭据。

本轮先只读定位固定Cyber报告/Markdown/导出入口与本项目固定Finding修订/证据读取，明确哪些会启动模型、哪些可作本地确定性草稿导出；目标是最少可交付的固定候选草稿，不把下载当作完整模型报告生成/发布/SDL。Sol已交一页discovery，主核固定原生RenderEvents只接AOP事件、原生startReportSession会触发Run，接受最小方案为纯前端导出正在阅读的Finding固定修订及证据快照，无新API/Go/schema/模型调用/报告状态。Sol正常退出0/原身份消失后，唯一新Terra+xhigh`wscyber-terra-finding-draft`已启动（`var/finding-draft-ui.Ed1pdHq2/`，后台`b1m5mkot6`），只写自包含纯格式化函数、候选详情导出按钮、翻译及覆盖清单。按惰性文本输出防HTML/图片载荷，默认不含Flow/body/认证头/完整URL，不声称全面脱敏或正式报告发布；Terra已静态交回并正常退出0。主核纯函数与下载调用点后，仅运行一组现有TypeScript/remark依赖的纯fixture：固定时点/编号/引用、缺失值、sent三态与两种截断、无副作用与无原件字段、恶意HTML/图片/链接/反引号内容仍完整但Markdown AST无活动节点均通过；不是浏览器下载通过。当前`b24a3lqu0`串行构建新UI，之后才做一次隔离页面下载与切候选核对，不新增后端/模型/状态层。用户42453新Hub718122/浏览器93373与当前产物/已升级state保持，不新增真实模型/目标请求。

## 0.93 已获准升级原预览：独立验证入口上线本地，原数据保全

2026-11-15。用户在独立升级确认中明确选择“升级当前预览”：允许备份原数据库、事务性加两张验证表并切换新Hub/UI，**只展示，不调用真实模型或目标**。主核原Hub身份及2任务/11HTTP/3候选/49事件，精确SIGTERM旧Hub126083并正常exit0，保留用户浏览器/旧页面；建立本次唯一0600恢复副本`var/verification-upgrade.goqgrflx/before.sqlite`，SQLite quick_check及旧计数通过后，才以新Hub重开原state。

**当前可操作入口仍 http://127.0.0.1:42453 。** 新Hub718122/start1675142（后台`besun9itl`），binary=`var/native-verification.GSCklaKc/wscyber-hub`，UI=`var/verification-terra-resume.UQ5ANPBs/release/public`；原私有state仍`var/product-preview.VZ6MR7aB/state`、访问key及产品模型配置未改。Chromium93373/start213227（后台`bf7iwxyh7`）保持，原1个标签页未刷新/关闭，另开新版标签页并保留候选详情的“发起独立验证”入口；主实际打开/关闭确认框并看最终两图，未点保存启动。0页面异常、0业务PUT/PATCH/DELETE，后台和数据库均无新增验证。

**升级后实核：** 原webapi_tasks/requests/findings/finding_revisions逐行与恢复副本一致，原计数仍2/11/3/49，两新表均0行。准确进程/产物/原件保全结果、浏览器结果与截图见`var/verification-upgrade.goqgrflx/{result.json,browser-result.json,01-live-verification-dialog.png,02-live-verification-entry.png}`。不能将隔离副本的2条测试验证混入原库或称真实判读已形成；没有重跑原靶场、真实产品模型调用、公开部署或commit/push。原事件缺口不补造，非空验证结果浏览器链/真实研究效果仍未验；后续继续小片，不重复已过专项。

## 0.92 独立验证前端构建与隔离操作链通过，真实模型结论仍未验

2026-11-15。Terra+xhigh接续DS额度中断，完成关闭只隐藏/保留未决身份及冻结依据、跨重试保存事实、回读归属/来源校验、组件props和候选ID直达；主核关键分支、实际进程型号/effort及可见任务后正常退出0。一次tsc/Vite成功（4690模块，27.87s），新UI在`var/verification-terra-resume.UQ5ANPBs/release/public`，旧Browserslist/chunk警告保留，不等全站验收。

**实际隔离检查：** `var/verification-local-check.EUWFUzeg/`只通过SQLite只读backup复制原库，不复制模型配置/运行凭据/开发连接；新本机40183/私有访问key/净环境/独立无头浏览器。真实PUT保存201后丢回执并使一次GET核对失败→关闭Dialog及提示→重开仍“核对并继续原验证”→恢复网络后同UUID PUT200核对→原生Open/Run；再次显式发起得到新UUID/session。验证会话任务查询200时Bar出现、重载找回固定来源、返回原候选、520 CSS窄屏/无所测横向溢出/Escape关闭、无认证新GET401均实际通过，主看图。关键路径没有伪造API/模型成功回复。

副本无Provider，两个原生执行均真实报`agent provider is nil`，所以只是保存/核对/Open/Run及失败展示链通过，**不是独立验证研究成功或非空结果浏览器验收**。副本2任务/11HTTP/3候选/57事件/2验证/0结果；原库前后仍2/11/3/49。Hub587194和Chromium590199均精确正常退出0，profiles2全关闭、clients_created=0、no_active_processes=true。证据/准确边界见该包result.md及JSON/截图；当前42453/原Hub126083/用户浏览器93373保持不动。

看图另发现null判读被标“待执行”、Dialog大块留白与遮罩低于header；唯一新Terra+xhigh短包`var/verification-ui-polish.xnmI5Qk6/`已仅修三个展示文件及hash，正常退出0。最终增量构建`bozqdlu3x`退出0（16.41s）。只读复查已通过：两条无结果历史均“尚无判读”，Dialog由大块空白改为内容高度，z70/z80遮罩真实盖住header；桌面/520与390 CSS窄屏实际查看，390重开computed width358px无所测横向溢出、Escape可关，0新验证写入/0 pageerror。主已看最终3图，没有重跑已过启动链。复查Hub632607/Chromium634192也均正常退出0，profiles2关闭、clients_created=0；原库仍2/11/3/49，用户42453预览保持旧已验版本。当前代码/构建及隔离链已收束；应用新功能到42453将给现有库加两表并切新Hub/UI，需要一次原库升级/切换决定，尚未执行。没有新增真实产品模型/目标请求、部署或commit/push。

## 0.91 开发网关额度耗尽，按用户指定切GPT后备且均xhigh

2026-11-15。用户明确开发网关`<内部模型网关>`额度耗尽后，Kimi非前端代码改`gpt-6.1-sol`、DS Pro前端改`gpt-6.1-terra`，两者均`xhigh`；astra继续仅主分析/契约/派发/关键验收。主已同步AGENTS、会话规范与长期路由；不降低业务底线，不接受自报完成，不换任意其他型号。

本轮DS短修实际报API400“今日使用额度已达上限”，并非完成或卡死：`var/verification-ui-finish.lecY6HiL/idle-check.png`已实看，无result交回。部分App会话200分支/刷新、hook关闭与清理拆分、候选ID直达已落盘，组件props/冻结来源/安全重置与hash仍未收完。主停止仅用于等待的任务，正常退出DS，CLI498799/终端498767退出0，保留全部源码；前端未构建，不以部分修改当通过。

唯一新可见Terra `wscyber-terra-verification`（CLI551915/终端551894，后台`buxe13s12`，包`var/verification-terra-resume.UQ5ANPBs/`）已实际读包复述并开始修改；主核进程参数为`--model gpt-6.1-terra --effort xhigh`，开发host沿主现有GPT连接`<内部开发网关>`，未带旧Wangsu settings/复制凭据或改全局、产品模型配置。CLI帮助确认支持xhigh。任务仅三个已定位收尾块，不能重做整轮或越权改Go/用户库；尚待交回、串行构建及隔离本地副本检查。当前42453及原Hub126083/浏览器93373/已验产物仍保持，无新增真实模型/目标请求、数据升级或部署提交。后续非前端执行用sol+xhigh，当前未额外启动Sol会话。

## 0.90 用户要求继续逐轮，接原生候选独立验证

2026-11-15。用户再次明确“继续逐轮完成”。主`wscyber-6d`接下一实质业务片：候选固定时点→独立Cyber会话→新HTTP证据→支持/反证/不足判读回看；不自动confirmed，不做修复后复测/报告/SDL，不复用旧Decision循环或旧模型绑定/固定次数限制。正式主链及0.89已通过项不重跑。

唯一新可见Kimi`wscyber-kimi-native-verification`（CLI215936/终端215906，后台`bs94floo1`，包`var/native-verification.GSCklaKc/`）已实际读包并正确复述。已交回`discovery.md`。主核固定上游schema与当前写权入口后，已在同包`implementation.md`冻结并授实际后端写权：稳定UUID的PUT验证记录、固定候选修订、新session的HTTP授权映射、Invocation绑定的不可变三类结果及只读API；扩展HTTP映射不能顺带让验证session写原候选。不增加上游Schema迁移钩子，在本项目装配点仅验证已知旧结构后事务性加两表，再保留原生完整结构校验；只用临时库验证保全，不授权升级当前用户库。执行者首版已交回：两表、PUT/详情/列表/session反查、HTTP验证session归属、候选写保护和结果工具已落盘；仅3项新专项最终通过（0.117s），未构建Hub。主实际读代码/测试后要求同轮补正：来源revision检查与创建同事务、同时间分页参数、旧HTTP调用键跨session回执校验、保存错误真实性；同时补真正counterevidence/空证据inconclusive断言，去除重复半成schema测试并纠正覆盖表述。补正已交回且经主关键源码核对，末次3专项退出0（0.128s）：三种判读均有实际成功保存断言，unknown/claimed资格矩阵未实构造。Kimi已正常退出0、原CLI/终端身份消失；之后主只做一次补丁入口新Hub构建，后台`b2qcxwm3y`退出0，产物同包`wscyber-hub`，未启动/未应用用户库。后端事实与边界见`native/VERIFICATION.md`。唯一新DS`wscyber-ds-native-verification`已启动（`var/native-verification-ui.0kK5R0uA/`，后台`bh7mt8mo2`），接发起/原生执行/历史判读/新证据的前端路径；首版已静态交回（API/原生启动hook/验证区/共享证据组件/翻译），未构建。主核代码后正同轮补齐：成功后的显式新验证不能复用旧ID/session；待核对提交不能随props版本变化换ID且各launch保留Open请求身份；启动Dialog/错误不随会话切换卸载丢失；必须接按session找回验证上下文及返回原候选；静默刷新不得清掉已加载历史分页，切上下文令迟到请求失效。未将这些原包要求当额外新功能。长DS轮次曾等待器超时，主实核仍有代码更新、无可见连接/内存报错，没有重开并行会话。补正交回后主再核发现任务查询对验证session实际200而App仅在404反查、关闭提示会清掉未知提交身份、返回原候选依赖第一页，未接受为完成或“可接受边界”。现有成果全部保留；原DS正常退出0/身份消失后，唯一新同型号`wscyber-ds-verification-finish`（`var/verification-ui-finish.lecY6HiL/`，后台`blbltibun`）仅定点修这些缺口/判读刷新，不从头重做。前端仍未构建，原生Open/Run和当前用户库升级未验，不能称完整验证功能已交付。主独占共享docs，未并行启动DS/构建/浏览器检查。42453及0.89准确Hub/浏览器/已验产物保持不动，无原靶场/真实产品模型调用、迁移/部署/commit/push。

## 0.89 两项中断工作收尾：事件补丁通过，证据引用可定位，预览保留

2026-11-15。按用户卡死后要求，全程单个开发执行会话，Kimi→DS→构建/页面检查串行；三个短执行会话均正常退出0，未全仓测试。事件锁补丁与唯一专项、新Hub已通过，准确范围见0.88和`var/event-resume.iGFF7Gim/result.md`；普通go build不应用补丁，正式入口`native/RUNNING.md`已改用内部持有heavy.lock的`scripts/cyber-go-overlay.py`。不补造历史事件缺口、不宣称完整并发/恢复保证。

**证据引用已验：** 首版步骤[n]关联与重复点击正确，但主看图发现窄屏标题被固定候选列表遮挡。DS仅再修两文件：对展开提交后的证据头部button定位并防止focus二次滚动，候选/任务切换清空待定位。最终tsc/Vite增量通过（14.03s，旧Browserslist/chunk警告保留）；真实指针、Enter/Space、重复点击零额外GET、切候选清空旧证据通过。按详情滚动容器边界/实际命中/焦点核验，实测桌面2043×946 CSS及窄屏416×720 CSS，主实际看最终两图；窄屏标题不再被遮挡、无所测横向溢出，不外推全主题/全矩阵认可。最终0 pageerror，原生IOA三个404保留。最初语言选择器和视口测量误读均已纠正，不当产品缺陷或全通过依据。

**当前可操作入口：http://127.0.0.1:42453 。** 最终Hub126083/start286783（后台`bw7zn2pjy`），本轮独立可见Chromium93373/start213227（后台`bf7iwxyh7`），主CLI6995承载；准确归属/截图/结果见`var/evidence-preview.hDBDnRKq/{ownership.json,result.md,final-interaction-result.json}`。binary为`var/event-resume.iGFF7Gim/wscyber-hub`，UI为`var/evidence-scroll-fix.9mpvtpm1/release/public`，原私有状态仍`var/product-preview.VZ6MR7aB/state`。只正常停止过本轮用于首版检查的Hub92187以切换最终静态路径，旧public未覆盖、浏览器未关闭；保留当前窗口/服务及其父链，不自动刷新/重放。

前后只读计数仍**2任务/11请求/3candidate/49持久事件**，请求全部completed。没有新增靶场请求/真实产品模型调用，没有独立确认漏洞、报告/复测/SDL或完整恢复的新验收；未迁移/部署/commit/push。本轮完成的是上述两项中断工作，不是整个产品已完成。后续继续原生业务短片，维持单执行与必要定向验证，不再重跑已通过的本片。

## 0.88 卡死后单执行恢复：先收尾事件补丁，再接证据定位

2026-11-15。用户要求接续并降低并发。新主`wscyber-6d`核对无其他开发会话，旧Hub/浏览器及42453监听均不在；没有自行清理用户窗口，也不推断本次卡死的唯一根因。上轮补丁、定向测试和构建脚本已落盘但无结果交回；候选步骤编号仍是纯文本，前端片未完成。保留原研究数据，不重放靶场/产品模型请求。

恢复期只开一个额外执行会话，后端→前端→构建→浏览器分段串行，不全量重测。Kimi`wscyber-kimi-event-resume`已交回：一行`_txlock=immediate`补丁、稳定派生根模块＋overlay构建入口及唯一争用专项完成。专项退出0（5.248s）：写锁释放前等待、释放后真实落盘、持续占锁约5.017s报SQLITE_BUSY且无伪造事件。测试自身辅助连接/事务设置曾失败并修正，准确记录在`var/event-resume.iGFF7Gim/result.md`；不是所有并发/恢复保证。新Hub同包构建退出0；主核关键代码并收束稳定副本路径和覆盖共享文件写入的heavy.lock，正式RUNNING文档改用补丁构建入口，普通go build仍不应用补丁。未补造旧历史事件。

Kimi已正常`/exit`，CLI18725/终端18697均退出0且身份消失。之后唯一DS`wscyber-ds-evidence-resume`（`var/evidence-links-resume.RvLg0CRK/`）完成步骤[n]只读证据定位，主核代码后正常退出0；再串行一次tsc/Vite通过（4688模块/15.62s），无依赖安装/全量测试。Browserslist过期/大chunk提示仍在。用新Hub及原私有状态恢复本轮检查入口42453（Hub后台`b8kkng7ik`、浏览器`bf7iwxyh7`，归属在`var/evidence-preview.hDBDnRKq/`）；原库启动前只读计数仍2任务/11请求/3候选/49事件。

**实际页面尚有一处待修：** 正确原件关联、重复点击零额外GET、Enter/Space路径已操作；主看图发现关闭侧栏后的窄屏指针路径虽打开正确原件，标题仍部分被固定候选列表遮住。根因是openEvidence在React提交展开前定位旧DOM，不能以仅视口内坐标冒称滚动容器内可见。唯一新DS`wscyber-ds-evidence-scroll`（`var/evidence-scroll-fix.9mpvtpm1/`，后台`b8iask34c`）只窄修提交后的头部定位与清单，主暂停浏览器操作/构建待交回，不并行再开模型。前一驱动语言按钮选择器曾超时，实际已登录后按真实aria-label续做，未重复登录/Run；窄屏实测416 CSS而非请求参数520，首次键盘检查有侧栏覆盖，随后真实关闭并指针核验，不把前者当完整用户路径。未新发靶场/产品模型请求，未部署迁移或commit/push。

## 0.87 接续原测试：真实turn已完成，流量/候选可回看；修复事件持久化争用

2026-11-15。用户要求本主继续逐轮推进。主`wscyber-41`只读核对原任务，没有重跑靶场：11条HTTP记录全部completed/sent=true、3个candidate；固定原生descriptor解码最后持久事件，原turn的`TurnEnded.stopReason=completed`（7次模型请求/49418总tokens）。这不是3个已独立确认漏洞，也不表示第二套靶场路径或完整研究效果已验。

**接续验收：** 旧42453/Hub/浏览器均已退出，原日志包含正常model-node-closed/stopped。复用既有binary/UI及原私有状态恢复 **http://127.0.0.1:42453**，没有Run或目标请求；本轮独立可见浏览器完成登录、中文、原任务3条候选、WSC-000001详情/证据展开及11条流量列表/请求响应双栏回看，主看图。回看前后任务/请求/候选/持久事件计数仍2/11/3/49。请求展示明确为结构化重建，候选仍candidate；未操作剪贴板/下载。重开执行页可回放且最终总结可见；主最初错误地等待“回复完成”可见标记，后核`ChatPanel.tsx:746`确认它是仅实时变化的sr-only无障碍播报，不是历史页持久状态标签，不将该驱动超时当产品缺陷。持久终态已核实。

**本轮实际缺陷：** 原Hub有9次`persist AOP event ... SQLITE_BUSY`，不能把实时展示称无缺失持久历史。Kimi可见会话`wscyber-kimi-event-persistence`（`var/native-event-persistence.e3SROc8m/`，后台`bswj8xetr`）已通过单一临时库测试复现未修复原生事件写入失败；根因是固定上游事件事务先SELECT后INSERT，在WAL并发下读事务升级失败。主批准固定版本原生SQLite的一行`_txlock=immediate`修补，不自建Store/Harness、不改模块缓存或重放历史。Go拒绝overlay模块缓存文件，改为一次派生固定根模块工作副本及临时modfile，仅构建时替换该依赖；目前实现/修补测试待交回，未称修复通过。

当前Hub后台`b7b5g34zf`、浏览器`bnxza0yhx`由本主承载；PID/日志/截图/结果在`var/product-readback.hlfgEUX1/`，状态仍`var/product-preview.VZ6MR7aB/state`。保留用户窗口，不自动刷新/重放；运行中当前仍旧binary。Kimi仅拥有本轮所列数据库/定向测试/补丁构建入口；主独占共享docs。原测试只读回看后，另开DS短片`wscyber-ds-evidence-links`（`var/native-evidence-links.XPLUxEQi/`，后台`bu5bwwv2t`），仅将候选步骤纯文本[n]接到现有证据的只读展开/定位，写组件/必要翻译/覆盖清单，与Go无重叠；尚未交回/构建/浏览器验收。重型验证和构建串行。未新增靶场请求、产品模型调用、部署迁移或commit/push。

## 0.86 用户已授权实际产品模型与单端口靶场，真实探索已启动

2026-11-15。用户明确提供产品模型 `wangsu-ds-flash-latest`、网关 `https://<内部模型网关>/devops` 及私有API key，并授权测试本人局域网两个靶场，**唯一目标origin为 `http://192.168.157.1:8000`，不得扩到其它端口/主机**。密钥未写入源码/共享文档或回显；已通过实际页面保存到本次预览的私有原生模型配置，临时输入副本已删除，不复用开发连接。

已核固定OpenAI Provider只追加 `/chat/completions`，将配置Base URL设为 `https://<内部模型网关>/devops/v1`。只点击一次原生“测试连接”，真实页面返回“连接成功 · 耗时3541ms”。主最初响应等待器误匹配RPC路径而超时，之后只读现有结果、没有重复计费测试或猜改网关；不是模型请求超时。正式配置已加载/节点可用。

已从新建任务页面创建并启动真实研究：task=`6d20aa82-2ca0-416c-95bc-6627427b6079`，session=`6fe0fbea38079d79`，本机归属/入口仍见0.85。此轮仅低影响探索，提示目标联网只用webapi_http，最多12个HTTP请求用于本轮链路验证，不做端口扫描/口令爆破/破坏性写入；明确不执行目标内容指令、不跨origin、unknown不重放，有真实证据才保存candidate。这是单次运行预算，不是产品永久限制。只读终态观察后台 `buivh20t8`，超本轮预算/unknown时通过当前任务暂停按钮停止，不自动重跑；结果尚待通知，不能提前称有发现或全链通过。原生其它工具仍无同等强制网络边界，不将提示词当不可绕过隔离保证。

## 0.85 正式入口与新UI已构建并可操作；真实模型研究仍待配置与授权

2026-11-15。Kimi与DS均交回且正常/exit，CLI/终端均0。主核正式Provider/Runtime/关闭与UI三态配置分支，没有重跑已通过Go专项。仅一次 `cyber-ui-build.py rebuild` 复用既有工作区/依赖，tsc及Vite成功（4688模块，打包15.07s），产物 `var/native-product-ui/release/public`；保留Browserslist过期/大chunk警告，没有npm安装/全量测试/新增模拟runner。

**当前可操作新入口： http://127.0.0.1:42453 。** 独立目录 `var/product-preview.VZ6MR7aB/`，新正式Hub PID311250（后台b05291038），独立可见Chromium PID312539（后台b5nndw9dk），均由当前主CLI199896承载；访问key在该目录私有access.key，不回显、不复用开发凭据。Hub用 `var/native-product-entry/wscyber-hub --local-hub --model-node`，未传验收model-endpoint，已公告正式节点。保留该窗口/服务/当前release，不自动刷新/重启/重放；不是恢复旧36667/34039。

**实际页面检查：** 原生登录、切中文、正式设置仅LLM页，供应商/HTTPS地址/代理/模型参数可编辑、显式测试入口可见；两输入/明确授权表单可操作。创建了唯一明确标为“本地界面核对”的任务（只填127.0.0.1:9，指令不访问目标/不调用工具），未配置Provider所以原生执行真实报 `agent provider is nil`，不是研究成功。任务保存与会话可见、执行失败记录、流量和候选的真实空态/页签切换均已操作，主查看设置/流量/候选截图；只读核库tasks=1、requests=0、findings=0。最后保留已登录的正式模型配置页，未填/保存任何真实产品模型凭据、未点连接测试。最初截图等待因页面同时有h1/h2 Cyber而选择器不唯一失败，只修读取位置，没有重交任务/改产品。

**未验证必须保留：** 真实模型自主探索→HTTP→候选全链、非空候选详情/原件展开、HTTPS/代理实际联网、窄屏/全展开视觉、完整恢复/全工具授权出口/业务迁移仍未通过。当前不是完整产品或研究效果验收；下一步实际模型运行需要用户明确产品模型配置和测试目标授权，不能默用开发连接。无部署迁移、外部目标请求或commit/push。

## 0.84 继续正式运行主链：停止扩展模拟验收，前后端分文件实施

2026-11-15。用户明确“继续完成，不必要的测试不要做了，否则太慢了”。主只做必要编译、直接受影响专项与关键页面操作，不再增加模拟runner或重复H1–H4/HTTP全套。只读定位报告已实际交回 `var/native-runtime-path/result.md`；开工核无其他开发会话，旧36667当前无监听，未重启旧预览、未推断其退出原因。

本轮直接实施：`wscyber-kimi-product-entry`（包 `var/native-product-entry/`，终端后台 `bbe1afha1`，已真实读包复述）独占 `native/cmd/wscyber-hub/`，将不带model-endpoint的model-node接正式原生Provider/会话，保留显式本机验收分支，取消正式路径的3回合/4实例等实验限制并支持自身状态目录重开；不隐式模型探测、不重放旧任务。`wscyber-ds-product-ui`（包 `var/native-product-ui/`，后台 `bs49w387i`）独占正式UI覆盖及清单，接正式LLM配置、已有任务/流量和候选只读详情/证据展开。正式节点能力冻结为 `wscyber.model.v1` + `wscyber.webapi-http.v1`，不与旧 `wscyber.local-model.v1` 混同。两执行者代码不重叠，DS仅静态、Go必要验证与之后UI构建串行。新功能尚未交回/编译/页面验收，不提前称完成；主会话 `wscyber-35` 负责接续。

**后端已交回并经主源码核对：** `model_product.go` 复用原生Provider/Runtime/命名空间与State.Set，正式无model-endpoint分支和自身状态目录重开已实现，原验收分支不变。执行者报告命令构建及仅两个 `TestProductModel` 专项通过（保存/启动零隐式模型请求、同库重开、显式本机模型列表一次、供应商构造/原生协议与借入client排空关闭）；主已读取对应代码/测试，不重复运行。产物 `var/native-product-entry/wscyber-hub`，操作说明 `native/RUNNING.md`。未验真实模型对话/continue/HTTP工具集成、HTTPS/代理或研究效果；不能把构造与协议检查称真实研究通过。

**前端已静态交回：** 新候选列表/详情/按需原件展开、正式能力标志和任务局部视图已落盘。主实读发现正式ConfigPanel仍会显示/发送后端不支持的非LLM字段，DS同轮改为llmOnly与localModel分离：正式只提交LLM但不锁供应商/代理/探测，旧受控模式仍保持原边界；主核关键分支。DS已停写，主核专属终端空输入后正常/exit，CLI与终端均0。尚未tsc/Vite/页面验收；Kimi后端已交回，待其正常退出后主串行一次UI构建，不以源码交回当可操作交付。

开发连接保持原型号/原连接，未改全局或产品模型凭据。首个终端启动因当前qterminal不支持--separate退出1，已改用该版本支持的-e启动，不是模型连接失败。无真实产品模型/目标调用、部署迁移或提交；已有历史与用户窗口不动。

## 0.83 交付纠偏：停止未接受的模拟集成草稿，回到正式运行主链

2026-11-14。用户询问当前进度/是否偏离目标，并追问fixture是否属于Cyber。主已明确：已通过的HTTP链是**本机模拟模型的工程集成测试**，Cyber循环/工具/HTTP/存储真实，但模型决策和目标响应是预设；不等于真实模型自主挖掘效果或完整产品。此前在验收驱动/断言/资源修补上投入过多、可操作界面滞后，主承认交付节奏偏移；当前不再继续铺报告/复测等模块，先回到正式模型运行入口和“新建任务→探索→HTTP原件→候选”的可操作主链。真实产品模型调用/目标测试仍须相应授权，不复用开发凭据。

`wscyber-kimi-findings-flow`交回的模拟候选集成稿**未被接受**：主实际读取发现未定义变量/返回值数量、重复独占写出等阻断；执行者所称工具回执不含source错误，正式`native/webapi/finding_tool.go:136`明确返回source，测试补造来源或要求其为空均不接受。未编译/运行该稿，不把语法格式化当链路通过；`var/native-findings-flow/done.md`已纠正为未接受草稿。主明确停止此稿的后续修补，收到停写确认且核空输入后精确SIGTERM结束CLI3989451；未改正式候选源码或运行服务。

**保留事实：** HTTP模拟模型后端链0.80已通过；正式候选4个新增专项0.82已通过。候选模型集成、新UI、生产打包、正式模型运行接入与研究效果均未交付，旧库自动升级未做。当前var中的候选场景runner/client是未接受草稿，不能直接使用其命令或据其报告声明通过；这不撤销历史成功产物的实际结果。旧36667与用户主入口保持保护，无部署迁移/提交。该执行者CLI退出143、终端退出0已核，不是正常CLI退出。

当前只读执行者`wscyber-kimi-runtime-path`已启动（`var/native-runtime-path/task.md`，后台`b0uiexxqz`）：只定位固定Cyber正式模型构造/重载/生命周期入口、当前验收专用限制和前端相关分支，输出一页带源码行号的事实；无产品写权、无新fixture、无构建/应用/模型调用。主据此决定最少正式接线，不继续凭猜测叠加验收框架。

## 0.82 候选4项新增专项通过，接真实原生工具链

2026-11-14。主确认候选执行者交回/空输入后精确SIGTERM结束CLI3869673（143/终端0），仅运行`go -C native test -p 1 -mod=readonly ./webapi -run '^TestWebAPIFinding' -count=1 -timeout=60s -v`。`bs4qnlph1`退出0，4项全部通过（0.129s）：生命周期/快照/只读分页与历史、假及跨task引用/未知权威字段拒绝、8并发编号唯一、未授权本机origin零发送及sent=false/http_status空快照。未执行旧HTTP/URI/full-suite，不把这组直接工具/存储测试称原生模型链或UI通过。

下一唯一执行者`wscyber-kimi-findings-flow`已启动（`var/native-findings-flow/task.md`，后台`bxnn072jc`），只扩既有Go协议client与本机runner的候选场景：同一原生session两次Run，先生成真实HTTP证据，后由模型fixture调用webapi_finding保存一个candidate并消费返回，再核鉴权API和数据库归属；模型共4请求/目标仍2，两个turn均需completed。两Run只是本机验收安排，不是产品固定工作流或新Harness。当前执行者仅静态/语法，主交回退出后编译新Hub/client实跑；正式业务源码无此轮写权。旧库升级、候选UI/独立验证/生产打包仍未交付，36667和用户入口保护保持。

## 0.81 接续原生候选发现最小后端片，前端缺口单独保留

2026-11-14。在0.80真实HTTP协议链通过后，按持续逐轮授权接候选保存/更新、稳定编号和真实证据引用后端，不以资源受阻的前端打包作为所有独立后端工作的强绑定门槛，也不把UI未验改写为完成。主已核旧D1业务契约和`internal/domain/findings.go`、编号格式`WSC-%06d`；旧PG/Decision/Attempt账本不迁入新Harness。

唯一执行者`wscyber-kimi-native-findings`已实际接收并交首版（CLI3869673，任务`var/native-findings/task.md`，后台`brznyymd1`）：`webapi_finding`保存工具、两表/稳定编号、原生Invocation归属、同调用回读/冲突、内部revision历史、同task HTTP真实状态/原件hash快照、四类只读API与新测试均已落盘，仅gofmt/静态。主实读发现的同call冲突nil解引用、提交后回读误称未保存、未知task列表200、精确历史时间为空、未发送测试用授权origin等问题已同轮补正，另清理新测试未用变量。主核代码/空输入后精确SIGTERM结束CLI3869673；当前`bs4qnlph1`仅执行4个新增`TestWebAPIFinding`专项，隔离新库/本机fixture、单包低并发，不运行旧HTTP/URI/full-suite。结果待通知，静态交回不等运行通过。原生已有库精确schema核验不支持本次自动加表，保留为迁移未交付项；只在新隔离库验证，不清库/重建旧数据。始终candidate，不自动确认/评分/报告/复测/外部投递，不复制原件或另造运行循环。交回后主仅做新候选功能定向验证。旧36667、用户入口、已停Go测试与前端重型重试边界保持，无部署迁移/提交。

## 0.80 Web/API后端原生协议链真实通过；前端与生产打包仍待验

2026-11-14。节点列表契约修正后，主核空输入并精确SIGTERM结束CLI3823189（143/终端0），离线重编自有协议client，`b21k7z4f2`实际运行退出0。证据`var/native-webapi-flow/flow-du5ellef/{result.json,protocol-result.json,protocol-evidence.json,native-events.jsonl,native-watch.jsonl}`已由主读取核对；这是`protocol-api-not-browser`，没有浏览器、假UI或真实产品模型/外部目标。

**实际通过：** 新Hub/节点→原生Config RPC与应用状态（未运行前模型/目标0请求）→两输入和精确origin任务PUT/回读，task与initial_session_id独立→一次原生Open/Run/StandardLoop→本机GET /inspect、POST /echo→第二次SSE请求消费两条真实工具结果→completed、15条持久事件→按session定位任务/按任务读取两条原件，Flow完整/200/sent=true、请求/响应原始body与fixture一致→只读回读仍2证据、模型2请求/目标2请求→原生会话关闭。全局Artifact归档0，HTTP原件未进入全局同步。主另核watch导出16封事件包中15封有deliveryCursor，1封无cursor；客户端旧字段`watched_durable_events=16`不是16条持久事件，准确计数以此为准。

**收尾事实：** client0、Hub0、profiles2全部关闭、唯一模型client在排空后关闭闲置连接；无残留子进程或监听，本次private与短runtime均已删除（主核路径不存在）。目标端口有2条TIME_WAIT，不是残留监听。成功仅此本机允许请求链，不证明完整授权/unknown/恢复、URI最终专项、真实研究效果或全工具网络隔离；被系统停止的Go测试未重启。

**仍未完成：** 新任务/流量页面只有源码与tsc，生产Vite打包和可见/无头真实页面链均未通过，不能把本次后端成功改写成UI交付。旧36667仍按原保护要求保留，未刷新/覆盖；它不是新HTTP用户链。未部署/迁移/commit/push。下一轮沿已授权需求继续小片，不重复本轮成功链或失败的重型打包/浏览器尝试。

## 0.79 协议client已编译并运行；节点列表验收误读已定位

2026-11-14。协议客户端补正收束后，主精确SIGTERM结束CLI3733404（143/终端0），`bodk5s4q5`成功编译新client并实际运行协议链。证据`var/native-webapi-flow/flow-2sknx139/`：首次节点列表检查报`webapi_http absent from actual tool report`，尚未配置/打开会话，模型/目标0请求；client退出1、Hub正常排空0，无残余子进程/监听，失败目录保留。

主已核固定上游`pkg/web/service/agents.go:165–180`：`remoteAgent.view()`重建公开Hello仅含NodeId/Name/Capabilities/Runtime，**ListAgents按设计不返回Tools**。工具扩展的Add方式与上游files/terminal一致，不能据该列表断言工具缺失，也不改产品去迎合错误客户端。Kimi `wscyber-kimi-list-contract`（`var/webapi-list-contract/`）已完成Go协议client及浏览器wire/等待处三项修正：列表核真实能力/状态，不伪填advertisedTools，实际工具可用性仍由模型fixture的tools及真实调用结果证明。gofmt/Node语法及省略Tools的合成节点检查通过，主核关键修改/空输入后精确SIGTERM退出CLI3823189。当前`b21k7z4f2`正在重编client并接续全新协议fixture，不重建Hub/打包/浏览器或旧suite；结果待通知。新HTTP集成、UI与生产构建仍未称通过；旧36667不动。

## 0.78 暂停重复浏览器重试，推进轻量真实原生协议HTTP集成

2026-11-14。无头尝试`be1e1r2ig`同样在首次100秒检查点超时，证据`var/native-webapi-flow/flow-z94k3950/`；driver/Vite由本轮runner精确强制收尾（-9），Hub正常排空0，最终无残余子进程/监听，失败私有目录保留。模型/目标均0，未进入实际任务链，不能宣称页面已验或预览已保留。生产打包及可见/无头两种加载路径均未完成，本阶段不再重复这些重型尝试；不关闭旧36667或用户窗口、不关闭资源保护。

下一唯一Kimi `wscyber-kimi-webapi-api`（`var/native-webapi-api/task.md`，后台`bkjnh6scq`）只写轻量Go原生协议客户端，并在已有runner增加明确protocol-client模式，复用同一新Hub和本机SSE/HTTP fixture验证保存→原生Open/Run→两次真实HTTP→持久原件回读。无Vite/Chromium、不改正式业务源码、不新造Harness/协议或复制整套runner；首版client.go与runner协议模式已交回，仅gofmt/Python语法通过；主实读后要求同轮补正协议模式不能引用空UI路径、checkpoint JSON统一、结果在database通知前落盘/最终真实更新、metadata指针改值比较及准确client日志。上述六项已实际补正，主核关键代码；另恢复原浏览器keep-open无总展示时限，protocol只等待20秒收尾。静态/少量合成检查通过。主核空输入后精确SIGTERM结束CLI3733404，已启动新小客户端离线单包编译并接续真实协议链（`bodk5s4q5`）；当前结果待通知，不与模型会话或浏览器叠加。这是后端集成，不算UI/生产打包验收，也不代跑被系统停止的webapi测试或URI专项。无真实目标/产品模型/部署迁移/commit/push。

## 0.77 新链已到真实前端模块加载；仅修开发入口与启动取消分类

2026-11-14。Vite冗余配置断言已移除，窄修会话CLI3688654精确SIGTERM退出143/终端0。第二次真实链`b8b1o9xyd`在`var/native-webapi-flow/flow-63wnj9md/`：Hub、Vite及浏览器均启动，根页/React refresh/Vite client/实际应用模块200；登录前多个依赖版本与页面重载中出现静态模块ERR_ABORTED，驱动立即失败，未进入AuthGate后用户流程。日志同时有Tailwind content配置missing，Vite当前cwd为脚本包而非前端目录。**模型0/目标0、Open0/Run0/保存0；不是业务功能已通过。** Hub正常排空0、Vite143、browser driver1，剩余子进程为空、各监听消失；失败私有目录保留，不能称Vite正常退出或已清空目录。

Kimi `wscyber-kimi-dev-load-fix`（`var/webapi-dev-load-fix/`）已完成三处驱动窄修：Vite cwd改真实frontend、去掉额外依赖扫描/预转换覆盖以复用原生默认；仅对dev登录前、首次登录尚未发送的同源允许GET静态模块ERR_ABORTED单独记录，不忽略API/任务/其它或登录后错误。语法及1正8反分类检查通过，主核代码与空输入后精确SIGTERM结束CLI3701586，终端`b8ld3555y`退出0。新独立fixture`bgsi3k7nm`实际失败，证据`var/native-webapi-flow/flow-3ulwtp5n/`：首次浏览器检查点100秒超时，driver/Vite最终被runner精确强制收尾（均-9），Hub正常排空0，模型/目标仍0请求；曾见本轮Chromium子进程D状态，最终remaining_children为空、各监听消失，失败目录保留。不能说正常清理/页面通过。主不再增加代码或堆参数，仅用现有runner省略keep-open，进行一次无头真实浏览器链（`be1e1r2ig`，日志`var/webapi-dev-load-fix/headless.log`）降低可见展示开销；即使通过也不冒称保留了可操作预览。仍不重打包、不重放unknown、不碰36667及已停Go任务。

## 0.76 首次新链启动到Hub，Vite被驱动冗余断言阻断；零请求后已收尾

2026-11-14。开发驱动原生依赖/client、HMR与AOP分流、React refresh资源和manifest200预期已实际补正。主核空输入后精确SIGTERM结束CLI3554888（143/终端0），启动真实新链`b9144udqs`；就绪等待器`bleo4jw7p`收到失败后正常结束。

证据`var/native-webapi-flow/flow-2gyd6y3z/result.json`：新Hub成功监听，Vite在`vite-dev.mjs:45`的冗余“resolved native Vite development options”相等断言失败，未启动浏览器，**模型0/目标0请求、无新任务**。Hub正常退出0、profiles2全部排空、clients0、无剩余子进程、已建监听消失；失败私有运行目录保留供诊断，不宣称目录已删除。这不是页面/API功能失败，也不算新链通过。

单Kimi窄修`wscyber-kimi-dev-start-fix`（`var/webapi-dev-start-fix/`）已仅删除该冗余解析结果断言，保留原生Vite实参、fs/本机/代理边界，node语法检查通过；未增加替代校验。主核空输入后精确SIGTERM结束CLI3688654，后台`b0k7n3d0r`终端退出0。新独立fixture已启动（`b8b1o9xyd`，日志`var/webapi-dev-start-fix/run.log`，一次性就绪/失败等待器`bc4p1fqnc`），结果待通知；无unknown重放、不重打包、不重启已停Go测试。旧36667及用户入口不动，仍无生产release或真实新页面验收通过。

## 0.75 停止重复打包，接原生Vite按需开发预览；生产构建仍未通过

2026-11-14。关闭minify的`bvp6j2nqr`同样在4687模块转换后的rendering chunks阶段180秒超时（RSS840400KiB、4521次major page faults），退出1，没有新发布包。此后不继续试堆值、延长重跑或换打包器。新Hub已编译成功，tsc已通过，当前阻塞准确限定在生产前端打包/真实页面链未验。

下一单Kimi `wscyber-kimi-webapi-dev`（任务`var/native-webapi-dev/task.md`，后台`baaka10y1`）只给已有`var/native-webapi-flow/`驱动增加Vite原生按需开发服务来源：复用当前同一份正式overlay派生源码和依赖、新Hub/真实原生Loop/HTTP证据，不修改正式Go/UI/锁或再造应用。首版已交回、语法/纯准入检查通过；主实际审阅后要求同轮恢复必要的原生依赖按需优化与CSS使用的Vite client，将Vite内部连接和AOP分开，dev模式简化掉旧静态relay并保持本次精确同源范围，修正module query不能单独绕过路径条件。执行者仍只做源码/轻量检查；主审阅且其退出后串行启动。开发预览若通过也不算生产构建通过，真实结果尚待运行。旧36667不刷新/关闭/覆盖，其私有数据不复用；停止的Go测试仍不重启，无外部目标/产品模型/部署迁移/提交。

## 0.74 Vite已定位到分块阶段资源阻塞；先串行编译独立新Hub

2026-11-14。704MiB/8MiB单步Vite首次因前次OOM清空派生`web/static/.gitkeep`而未启动（`bmaawty5c`）；主核固定原件为换行后仅恢复该派生占位文件。实际尝试`baq1e0ow3`完成4687模块转换并进入rendering chunks，180秒超时；子进程峰值RSS833188KiB、5319次major page faults、filesystem_inputs23282176。没有新release，不把转换完成称构建通过，不再继续试堆值；旧public/binary未动。

独立新Hub应用构建`b546rp9c9`已退出0，产物`var/native-webapi-flow/wscyber-hub`已核为真实ELF；固定离线模块、单包低并发，临时目录也在本包。它不是重启已被系统停止的`go test ./webapi`，不代跑该suite/URI专项。下一步针对分块阶段，保留704/8MiB与全部功能源码，仅使用Vite原生`--minify false`生成本地验收包（`bvp6j2nqr`）；若成功也不外推常规生产压缩构建通过。真实新页面链仍未运行。无外部目标、产品模型、部署迁移或提交。

## 0.73 验收驱动静态补正已收束；512MiB不足以完成tsc，恢复已验堆值后接续

2026-11-14。`var/native-webapi-flow/`的关键补正和证据行选择器已核，纯策略/SQLite合成归属、真实协议合成检查及语法检查通过；真实用户链仍未运行。执行者已精确SIGTERM退出（CLI3397507退出143/终端0），未碰用户入口或预览。

第二次rebuild（`be58gbxx6`）在tsc出现明确V8 heap OOM，Node SIGABRT/整次退出1，未进入Vite、未发布新产物。512MiB是本轮降低的进程堆值，不能把该失败归因系统停止或类型错误。单行任务`wscyber-kimi-build-heap`（包`var/ui-build-heap/`，后台`bwsmnezkw`）已恢复先前已通过tsc的768MiB，保留esbuild的GOMAXPROCS2/GOGC30；主将误放scripts的交回说明归位本包，核空输入后精确SIGTERM结束CLI3506137。第三次构建`bnen0ockr`的tsc再次通过，但Vite仍在transforming阶段360秒超时，未发布。主不再重跑整套，按未变源码及已通过tsc单独诊断Vite。首次`b7vdrwbiw`因系统缺`/usr/bin/time`未启动Vite；改用Python资源统计的`bgp3hul1v`真实完成4687模块转换后触及640MiB堆上限，42.83秒、子进程峰值RSS812788KiB，SIGABRT退出，无新发布包。随后仅Vite以704MiB old-space/8MiB semi-space作最后一次180秒有界尝试（`bmaawty5c`），不改正式参数、不重跑tsc、不发布。被系统停止的Go任务仍不重启，旧36667保持。

## 0.72 新前端类型检查通过，Vite超时；单Kimi接低资源构建与HTTP用户链

2026-11-14。自有组件/独立release适配已实际交回，10项纯Python检查通过。主确认原Kimi空输入且已停写后精确SIGTERM结束：CLI3261261退出143、终端3261214退出0，原身份消失；不是CLI正常退出。

主串行运行`rebuild --workspace var/native-ui-quality.ZzLkZW2L/ui-work --release-dir var/native-webapi-ui/release`：**tsc通过，Vite6.4.3在transforming阶段360秒超时，整次退出1，无新发布包**。已检查没有遗留本次node/esbuild；当次内存与I/O压力明显，不能据快照断定唯一原因。未改正在服务36667的旧release或binary，未重启被系统停止的Go任务bdp7zx4bi；URI最终运行验证仍缺。

下一新单执行包`var/native-webapi-flow/task.md`已由`wscyber-kimi-webapi-flow`实际读取并正确复述接收（CLI3397507，终端3397494/start15979880，后台`b7pp7i5ac`）：仅将既有构建净环境降为Node512MiB/esbuild两核及GOGC30，并复用旧驱动编写新的真实UI→原生HTTP→持久原件本机链。只做源码/轻量语法检查，实际构建/浏览器由主在其退出后串行执行；不把旧printf成功当新链通过，不改前端/Go，不新增配置框架。构建净环境3项修改已落盘；新驱动首版仅语法检查通过。主实际审阅发现JSON策略字符串误当RegExp、PUT/201与预期404处理、旧relay策略缺业务路径、自动Run时序及SQL列名等脚本阻断，已在同轮补正；纯Python策略/SQLite合成归属与Node真实协议合成检查通过，主已核实际关键代码。最后证据行选择器已进一步限定本轮目标URL title span，排除main内激活页签。主确认交回/空输入后，GUI快捷键未使CLI退出，已精确SIGTERM结束CLI3397507；随后启动低资源独立输出rebuild（后台`be58gbxx6`），结果待通知，不叠加新执行模型或Go/浏览器。尚未启动新链，不将这些预检问题说成产品运行失败，也不称新页面已可操作。保留旧预览、用户入口与原开发连接，无真实目标/产品模型/部署迁移/commit/push。

## 0.71 前端源码已收束，单Kimi接自有组件与安全发布位置的轻量适配

2026-11-14。主已核前端最后实际修改：提交ref、原生lookup正确解包SessionRecord.session且仅NotFound视为缺失、accepted不被列表刷新失败改成启动失败、maxTurns重试快照、普通ArrayBuffer下载。未运行新UI构建或浏览器链，不能将源码交回当可操作交付。DS已结束：GUI退出输入发生丢字，主确认输入仅为本轮自输文本且无在途工作后，对准确CLI2997841发送SIGTERM；CLI143、终端0、原CLI消失，未碰用户入口/预览。退出执行者后实测MemAvailable约1.1GiB、无swap，仅当次恢复快照，不保证后续重型任务安全。

新唯一执行者`wscyber-kimi-ui-sources`已真实读包接收（CLI3261261，终端3261214/start15634759，包`var/ui-owned-sources/`，后台`b12yxl3mr`）。自有UI文件null来源标记、节点`wscyber.webapi-http.v1`声明和8项纯Python首版已交；主实际读代码后正同轮补：摘要真正移至tsc前、mkdir前归属校验、只删除明确旧owned派生文件，并提供rebuild可选release-dir让新构建不覆盖正在使用的旧release。不复制workspace/node_modules、不造版本管理、不运行Go/Node/服务/浏览器。被保护停止的bdp7zx4bi仍未重启，URI最终运行检查仍未完成。

主只读核2725461/2725469仍是36667预览原Hub/driver身份，当前已验public/binary继续保护。下一步是适配通过后以单执行、独立新输出处理前端构建/必要用户链；不要求用户每轮确认，也不把内存事件转成整个开发暂停。无真实目标/产品模型/部署/迁移/commit/push。

## 0.70 资源保护停止最终Go链接；保留实现、降为DS单执行继续补正

2026-11-14。Kimi最后三处HTTP修正与测试代码已落盘；混合批次原生Loop（1模型/2目标，下一模型被hook拒）及二进制预览边界专项已实际通过。最后`bdp7zx4bi`的全webapi检查仍在链接阶段，被执行会话收到的系统critical low memory通知停止；主实际核go3014797/link3015049归属并随后确认link消失。**不是命令自身失败归因，也不是最终测试通过；URI最终RawPath修正未完成运行验证。** 不原样重启该任务、不由另一会话代跑、不关闭保护。代码与未验记录保留，后端专用会话已正常退出0（bx9bsdrdk），不再占用执行名额。

实际诊断当时可用内存约545MiB、无swap；未清理旧浏览器、用户窗口或未知进程，不据快照归因具体占用者。当前只保留DS继续轻量静态补正，重型验证与新的构建会话不叠加。

DS的任务表单/原生启动/流量双栏首版已落地（3新组件、api/hook/App/翻译、null清单），主已实读源码及可见终端。大部分静态补正已落盘：保留关闭草稿/ID、首条消息用已保存数据；切会话关联兜底、迟到响应失效；原件/摘要截断分开、原始body下载/真实复制反馈；焦点/遮罩/控件标签及字号。最后静态窄修已实际落盘并由主核对：提交ref/禁用提交中控件，lookup只在原生NotFound返回null且正确解包SessionRecord.session，accepted与列表刷新错误分开，重试maxTurns沿快照；Blob用普通ArrayBuffer复制兼容固定TS5.9.3。源码轮收束，不把静态复核称构建或浏览器通过。DS已交回停写并由主向确认空输入的专用终端提交正常/exit，退出结果待通知。平台HTTP记录标题已明确只捕获webapi_http，不冒称全工具流量。已明确节点需实际提供`wscyber.webapi-http.v1`能力，不把Hub有API等同所有外接节点都能执行；下一Kimi小片只在实际装配的本机节点公告此标志。

下一纯构建适配任务仍在`var/ui-owned-sources/task.md`，待DS交回退出后单执行启动；不恢复被停止的Go任务。当前36667已验预览的源/public/binary及父链未由执行者操作；未新构建/切换到在改代码，未声明新任务/流量页面已经可操作。仍无部署/迁移/commit/push或真实目标/产品模型。

## 0.69 HTTP接口形状已稳定，DS已接续任务/流量工作面；内部窄修并行

2026-11-14。主已核HTTP补正：请求ctx/30秒超时、同事务指纹认领、已完成回读、sent三态、protojson/游标/session查询已落盘，执行者记录仅webapi的16项测试及vet通过。尚有三处明确窄修仍由同Kimi完成：URL Path不应重新赋EscapedPath导致二次编码；base64预览应按最终4KiB与实际截断标志计；混合工具批次用原生ModelRequestPolicy.Deny停止本turn后续模型调用，不能仅将未实现写成限制。接口路径/DTO不因此变化，未宣布整个后端最终通过。

新可见DS `wscyber-ds-webapi-ui`已启动（包`var/native-webapi-ui/`，终端2997766/start14969466，后台`b18uz1doh`），已订阅idle/交回；实际接收待核。直接实现两输入/明确授权→持久任务→按服务端initial_session_id原生Open/Run→流量上列表/下双栏→按需读取原件，按playbook/V2及原生组件复用。DS独占UI覆盖/清单，Kimi独占Go，不抢写；不再等用户开工或每轮视觉确认。

允许DS新增真正的本项目组件，以source_sha256:null明示自有源码，不为旧“仅能覆盖原文件”限制挤进巨型组件。下一独立Kimi小片`var/ui-owned-sources/task.md`已准备，只适配这项必要构建能力（及源码摘要前置），未启动，不新增第三会话。当前DS不运行旧构建脚本；待两边交回后主一次构建/真实用户链。36667仍保护当前已验binary/public及窗口，源文件更改不等它已更新。无外部目标/真实产品模型/部署/提交。

## 0.68 原生HTTP/受保护Flow后端首版已落地，主复核后同轮补正

2026-11-14。Kimi `wscyber-kimi-http-evidence`已交HTTP ToolExtension、G0纯策略、请求认领/Flow同库行、证据API与模型节点装配，报告真实本机多请求、原生StandardLoop和相关包测试通过。主已读取实现及相关测试，**尚未将首版认定为可接前端的最终契约**，继续同会话修正，不另起设计轮：

- 请求接原生ctx并有界超时，避免慢响应挂住/取消失效；终态有界落盘。
- 同调用与同指纹未决判断放到同一个写事务，结构化指纹消除拼接歧义并规范已知URL等价；已completed回读不能变IsError/Terminate。
- sent改可空事实、claimed无completed_at、无响应不造HTTP0；截断Flow.Complete与捕获状态一致，二进制/UTF8预览明确。
- 原件JSON兼容原生protobuf消费者；列表加cursor/has_more/next_cursor；按session定位任务使用`GET /api/webapi/tasks?session_id=...`返回Task或404，前端不猜绑定。此前`tasks/by-session/{session_id}`与证据路由有Go ServeMux歧义，主已改为查询接口；不改原证据精确路径、不引入尾斜杠子树分发或旧别名。
- 原生Loop只有全部工具结果Terminate才终止批次，补真实unknown/混合批次专项；必要时仅用原生hook停止当前turn后续模型调用，不锁整个任务/不复制Loop。

已重新订阅本次补正的idle/完成通知；idle后核实际代码与done，不因一次断线或旧通知停住。只补受影响专项，不再重复整个Hub/旧模型套餐。API正确后直接由DS接任务创建与流量视图，暂不让前后端对未完成接口抢写。当前36667预览仍是上一已验binary/public，不载入这批在改的HTTP代码；保持用户窗口及其父链，不重启/刷新/重放。无真实目标/产品模型/外部投递或部署提交。

## 0.67 UI增量构建与新预览通过；已获准后续连续逐轮推进

2026-11-14。用户再次要求后续由主持续逐轮接续，不每轮前端构建后停等确认；按既有Cyber界面规范自行实施/核验。Kimi临时断线或内存中断需核实际完成情况并有界尝试原任务续接，不把idle或漏交回消息当整体暂停，不静默换型。开发重试与目标unknown不重放分开。

**本轮已实际完成：** DS的6个表现源码/19项覆盖经主复核；主运行新`rebuild`对既有`var/native-ui-quality.ZzLkZW2L/ui-work`原位刷新，完整tsc及Vite成功（4684模块/15.74s），无npm安装/依赖复制。既有Browserslist过期与大chunk提示保留。当前`prepared`源码摘要仍在tsc/Vite后写入（执行方曾误报已前移），本次构建期间无源码执行者或并行修改，不把它说成已实现构建中并发变更保护。新Hub也已按当前native代码编译，含已通过6项功能+1项Hub专项的任务输入能力；仍未接HTTP研究/成果链。

**用户已明确批准新本地预览，不是恢复旧34039：** http://127.0.0.1:36667，后台`b1y5ckeye`，包`var/native-ui-quality.ZzLkZW2L/flow-ippamai2/`。新独立fixture/私有状态；监督器2725429→Hub2725461/start14351535、browser driver2725469/start14351540，完整归属在该包ownership/running.json。只查看本机已完成工具结果/配置/执行流，非真实目标或可重复研究入口。用户正在看的窗口/此父链/public/binary保持，不自动刷新、关闭、重放或在展示期间覆盖此release；用户关窗后由原runner收尾，未退出不称清理完成。

主复用既有真实页面驱动，完成配置、520窄屏、节点/原生执行流收展及结果，core_path_passed=true；主实际查看新4图并对比0.62同视口配置/展开2图。520宽Dialog为488/x16，所测无横向溢出。定向axe：配置与Workflow均0 violations；Workflow仍有3类incomplete（2/1/11个节点）未冒称全通过，9条已知IOA404保持；暗色只做源码/静态色值核对，未完整暗色浏览器矩阵，不等用户视觉认可。

构建Kimi已正常退出0，原源码写权回主。下一包`var/native-http-evidence/task.md`已冻结为实际后端实现：原生HTTP工具、同任务多请求、按任务读取真实原件；复用原生Flow类型但敏感原件保存在同库受保护业务行，避免全局Artifact同步自动进入IndexedDB，不封禁整个原生RPC或另造Harness。新可见`wscyber-kimi-http-evidence`已启动（终端2774277/start14463172，后台`bx9bsdrdk`），已订阅完成/idle通知以核实真实交回或断线，不能仅凭idle认定完成。已收到CLI2774307的真实读包及范围复述，正直接实施HTTP/受保护Flow证据；实现/测试结果仍待交回。不再停在重复设计轮。当前无部署/迁移/commit/push/真实产品模型或目标。

## 0.66 继续实际代码：DS可读性调整与Kimi任务输入后端

2026-11-14。用户追问为什么不继续，主纠正把执行者退出误当整体暂停的节奏：已有逐轮继续授权，不再因交回/退出停住或反复等开工确认。直接任务包即窄写权，不另设设计/accepted/交权文档关卡。

- **DS**：`wscyber-ds-readability-f2owtdim`，包`var/ui-readability.f2owtdim/`，只改正式原生UI表现层覆盖及清单，实现以0.62为基准一次约10%可读性/有效密度调整及已知4处文字对比度。初次窗口实际出现API400扫码登录提示，随后已收到真实读取任务包/组件的接收消息；不推断恢复原因，当前不是仅PID启动。前端模型保持DS，未换型。首版两份Workflow CSS经主审发现暗色对比度回归及范围遗漏，DS已同轮补正交回：共6源码+1清单（19项覆盖），补配置正文/字段label、主输入及compact消息正文；跟随按钮恢复暗色primary，reflex小字也用文本色。主实际读全部diff，未改功能语义；字号是约10%的分级取整，边角未全部调整，不称全UI完成。DS专用会话已正常退出0。尚未构建/新页面视觉验收。
- **Kimi**：包`var/webapi-task-input.l_0_ao3w/`、新可见`wscyber-kimi-task-input-l0ao3w`已启动，已收到真实读包及正确范围复述（CLI2426801），正在直接编码。直接实现原生Web Capability下的两输入＋明确授权保存/回读、一张业务表、独立task与initial_session_id，不加新模式开关/模型绑定、不执行任务。可写native/webapi、Hub入口与必要专项测试，与DS无重叠。已完成并经主源码复核：新增capability/store/test三文件，main.go挂载及失败路径Close，两个直接受影响旧测试补SchemaModule。空origins、IPv6、顶层JSON EOF与授权集合顺序已修正；Save改为INSERT ON CONFLICT后一次回读，删去重复分支。执行者最终记录webapi 6项测试及TestLocalHubNativeAPIAndShutdown均通过；主未重复全套。仅表持久化/回读与原生Hub接线，不是完整进程恢复或研究闭环。会话已交回停写并正常/exit，后台任务badklcoz5最终exit0；开发模型使用原连接，未新增凭据副本。

DS退出后启动独立新Kimi `wscyber-kimi-ui-rebuild`（包`var/ui-rebuild-current/`）。主已实际查看可见终端的代码/纯检查活动并读取交回，新增rebuild、纯fixture及SOURCE-OVERLAY用法已落盘；未真实Node构建。主发现刷新会误恢复依赖package/lock、cyber-ui恢复路径多前缀，另有发布rename失败未恢复旧入口及收据取值时机问题，已交同轮最小补正，不另起设计。目标仍为复用工作区/依赖、不每轮fork或整树快照，失败保留上一成功release、成功替换不留历史副本。已明确允许刷新停止预览所用`var/native-ui-quality.ZzLkZW2L/ui-work`的派生源码/产物，不触固定inputs/依赖/其它历史包。主在脚本交回后才串行一次真实UI构建。最多两个独立执行会话，当前为后端Kimi与构建Kimi；复用原开发连接不新增凭据副本。主负责接续审阅和必要串行构建，34039仍不自行重启，未有新预览/完整视觉验收。无真实产品模型/目标、部署迁移、commit/push。完成轮次后接后续已授权工作，不等待用户再次催促。

## 0.65 模型策略调整：astra仅主分析调度，代码执行改Kimi；准备恢复短轮

**随后用户新增简化要求：** 不要过多限制、配置修改校验、验证流程、功能绑定和历史版本保留操作。已同步当前Kimi：优先直接复用、只补当前必需代码，不将实验次数/固定关联变正式功能，不默认unknown锁死无依赖整任务；保留必要授权/秘密/证据与不自动重放。后续不机械复制源码/构建/配置历史或生成多份hash/交接验收材料，旧材料不擅自删除。Kimi按旧包的四文档静态准备交回后，已按纠正收缩为一份简短契约；主已要求准备片到此停写，不再循环改版/补交接/验证。草案仍未获实施批准：未知动作不能按origin+path简单判同一，task/session不为省字段强绑，已知全局原件同步不能仅靠“不触发”的声明消失；这些在下一最小实施包中明确，不再另开大设计轮。未构建测试或启动产品。此用户要求不等于删除真实证据、复测历史或撤销授权边界。

2026-11-14。用户要求核对整体进度，并因astra额度不足明确：仅主会话`gpt-6-astra`负责分析、架构/契约决策、精准任务下发与关键验收；前端保持`wangsu-ds-pro-latest`；其余代码开发/集成/测试改之前的`wangsu-kimi-latest`。已按历史启动脚本核实准确型号与既有连接文件存在，未回显凭据、不改全局或产品配置、不静默换型。已同步AGENTS、开发会话规范、底座说明及长期偏好。

**当前进度不变：** 原生底座与页面配置→受控执行链已验，正式Web/API自主研究/HTTP原件仍未实施；UI规范首稿、详情参考未完整、一次10%调整未实施，0.64的34039停止及不自行重启约束保持。剩余按用户结果归并7个环节，见[roadmap最新交付视图](roadmap.md)；不是宣称还需7轮或固定完成比例。

开工ListAgents无其他执行者；当前主`wscyber-df`独占共享文档/正式源码。新包`var/kimi-webapi-contract.wiu40e0j/`仅一个新可见Kimi做既有Web/API草案的有界修订准备：复用此前源码发现，纠正每task永久一次HTTP/固定单turn的实验简化，明确多请求的身份、防重放、未知状态与原件边界。先只写本包四份指定输出，不授正式后端/前端写权，不构建/测试/开服务或浏览器，不重新勘察整个仓库。主审查后才另行冻结实施范围；这不越过UI优先级或把准备当交付。

**新会话已实际启动并回应：** `wscyber-kimi-webapi-wiu40e0j`，QTerminal2316794/start13411048→shell2316812/start13411093→CLI2316813/start13411093；显式型号`wangsu-kimi-latest`、正确cwd、可见窗口171966471均实核。主已Read启动截图，看到指定型号、真实中文接收回应及读取任务入口的工具活动；不是只有PID或最小文字探测。随后已收到并读取结构化accepted及跨会话接收消息，目标、禁写范围与停止点复述正确；其中pid误填shell，主已按startup-check明确要求区分terminal/shell/CLI，不重做启动。契约交回仍待完成，尚未完成准备/实现或验收。后台启动归属`brwazy8rk`。本轮最终已交回停写，专用会话通过`/exit`正常退出0，原终端/shell/CLI身份均消失；仅本轮临时凭据设置副本已删，既有连接和用户窗口未动。全局/产品配置未改，不自动重试或换型。无产品代码实现/测试/新预览/部署/迁移/commit/push。

## 0.64 用户要求先深入Cyber详情并形成规范；本地预览已被资源保护停止

2026-11-14。最新优先级：只读观察Cyber真实任务详情的功能/布局/文字与展开态，形成有依据的后续实施规范；随后落实以0.62为基准的一次约10%视觉尺度及有效信息密度调整，不加无关内容填空。当前未改UI源码、未开新DS；不把规范准备当作已完成视觉调整。Web/API勘察暂停，`var/native-webapi.KUJS8uve/contract-closure.md`保留供恢复消费，正式后端尚未获写权/实施；每task永久一次HTTP/固定单turn等草案简化未获准作为正式自主研究契约。

**34039已停止，不在线：** UI执行主收到系统低内存保护停止bmknm3gas的通知；原入口已实际读取任务输出READY后`[killed]`，核四原PID身份消失、34039/41093无LISTEN、无最终result.json。先前browser core_path_passed仍是验收证据，不证明正常排空/SQLite检查/清理；private-0nm9265y及var/uf-x87t9mhc按失败保留。不把系统保护归因于该命令自身，也不自行重启；新只读参考站观察不等于恢复该本地预览。见运行包main-protected-stop-check.json及LIVE.md。

Web/API研究CLI1172652与不再承载预览的守护CLI898729均已核精确身份后正常`/exit`；各CLI/终端exit0，原三身份消失，无在途任务。原入口wscyber-40保持并独占共享docs/源码写权。

**规范首稿已落盘，在线详情未完成：** [Cyber界面学习与实施规范](cyber-ui-playbook.md)汇总本轮逐张实际查看的用户六图、此前有限在线观察、当前外壳计算样式和固定原生组件，区分V/O/S/R/U；包含功能覆盖矩阵、区域组织/文字与密度/展开态、复用机制及逐功能任务包验收清单，已接入AGENTS、frontend-redesign-brief和ui-reference。包`var/cyber-ui-spec.kmapjwg0/`曾误把download命名的同源JS当效果路径，已仅修静态资源判断；后续仍见Failed to load及未完整加载状态，没有可进入任务记录，无401/403，不能推断Cookie失效或确定故障原因。错误摘要展开超时、GUI诊断未取得安全标记，均不作成功；未声称已观察Details/完整工具展开。全部参考浏览器已关闭、各自runtime删除，未放开写操作/跨域/效果路径，未保存站内截图/正文/ID/凭据/HAR或执行攻击材料。公开前端两份JS只下载分析未执行；不是私有后台或站点部署版本证明。未改正式UI/Go、未开DS，10%调整仍待实施；无构建/测试/部署/commit/push。

## 0.63 回到Web/API任务与HTTP原件：原生能力核对已开始，尚未实现

2026-11-14。原入口wscyber-40已接回0.62开发/共享docs写权，读交付/验收及最终核对记录，独立核16覆盖文件hash、public清单与34039四个当前进程身份/新窗口owner1061589，并实际看配置局部、520窄屏及Workflow局部3图；未重构建/重跑。确认0.62的目标项通过与剩余4处对比度/3类incomplete同时保留，不外推为全UI或用户认可。

下一同一业务链目标：目标／已知信息＋希望达到的目标＋明确授权→Cyber原生会话/执行→实际HTTP请求/响应证据可读。先核固定原生任务/HTTP/traffic/artifact的真实能力与扩展，不再重复printf/底座演示，不沿旧Decision循环或另造Harness。Scope/来源/状态归属与最小文件范围先冻结，额外业务接口仅补真实缺口；前端随后由指定DS接入，未前后端实测不称产品交付，原有候选/验证/报告复测/用量原文Skill等需求不削减。

新包`var/native-webapi.KUJS8uve/`、新可见gpt-6-astra `wscyber-native-webapi-KUJS8uve`已真实接收并只读定位：QTerminal1172643/start12592189→shell1172651/start12592218→CLI1172652/start12592219，主已看启动截图。当前只写accepted/discovery，未交正式后端写权、未构建测试或开新服务。已交短discovery：拟复用原生Web/Tool扩展、Flow/operation.Ref及Artifact归档承接本机受控HTTP；主独立核artifact.go与SQLite去重代码，确认通用SyncArtifacts只校验结构、event_id冲突DO NOTHING，不能直接把命名空间当可信授权/并发幂等。已要求有界收口可信写入与原子核对、保存/Open/Run失联边界、模型可见能力及结果视图、全局原件同步限制；不另造Harness，也不为强行同库复用牺牲业务正确性。方案尚未冻结，未把此受限实验模式当正式Web/API全能力。旧CLI898729只保留34039预览父链，不再写源码；新Astra占第二额外名额，不再派CLI/DS，交回退出后由原入口串行换DS，不为名额关用户窗口或转托管。34039及旧失败原件保护，实核状态以0.62/LIVE为准。无真实模型/外部目标/迁移/部署/commit/push。

## 0.62 原生UI质量有界片已交付：配置与Workflow目标项通过；剩余问题明确保留

2026-11-14。新可见`wangsu-ds-pro-latest`执行者完成7源码+1清单及主静态核对后的局部补正，已正常exit0/三原身份消失。ConfigPanel改原生有界居中Dialog，说明按需展开、超时语义正确、配置状态文字改中性；补受控Dialog焦点归还/遮罩层级、main/h1、文件label与Workflow局部语义，反馈/回答标题actor改文本token。原Harness/配置/节点/会话运行逻辑、来源hash/依赖锁不变。主只做一次新离线fork+完整tsc/Vite，exit0，无npm ci/Go build/test/全仓；Browserslist/chunk警告保留。16项覆盖清单`161e6af19b4d4021eb4a4910de271150617ed9612ff3e3ceb0309eb6dfade155`，新public清单`2a3828165a7a68a1fd3faf976d32ad2b9c93647559d12ce3ab8f22a71cc20398`；复用0.61已验Hub。

**真实定向验收与看图：** `var/native-ui-quality.ZzLkZW2L/flow-78hc5ixy/`。缺新key有错误且0写、保存一次/秘密不回显/无隐式probe、帮助键盘与指针、Tab/Escape/焦点归还通过；2554桌面dialog672×704，520窄屏dialog488/x16且自身scroll=client486、document/body均520，无所测横向溢出，底部字段滚动与固定操作通过。一次必要本机模型/真实工具链completed、13持久/12持久Watch一致，Workflow收展/参数结果/键盘与只读搜索通过。主实际看最终8图+2局部裁切；0pageerror/0拦截但保留9条IOA404，不等用户视觉认可。

**审核不冒称全通过：** 官方axe-core4.12.1仅独立开发工具（固定锁/MPL-2.0），普通Playwright保持WebSocket常量完整。仅9项相关规则，保存后配置+帮助展开0 violations/0 incomplete；Workflow仍4处未修对比度（当前步骤4.11、角色legend3.09/3.34/3.83），另3类incomplete待人工/辅助技术核验。已改反馈标题/actor实算16.057/5.174，活动回答叠色背景实算15.180/4.892；不是全主题/错误态结论。两次前置验收因未等原生200ms宽度过渡而误判，均0模型TCP/HTTP/0任务，失败保留；独立CSS探针核实后只修runner等待真实过渡，未重构建/修产品迎合工具。详见[本片验收与剩余项](../native/ui/UI-QUALITY-ACCEPTANCE.md)。

**历史展示记录（现已被系统资源保护停止，见0.64，不可按此重启）：** http://127.0.0.1:34039，后台`bmknm3gas`；监督器1061535/start12338996→Hub1061537/start12339014、driver1061545/start12339021→新Chromium1061589/start12339085。窗口171966468由系统复用，绝不是旧33901的723048身份；准确归属在新包LIVE/ownership/main-live-check。仅当前fixture配置/节点查看、Workflow回放/只读筛选，不允许展示期额外任务/保存/发送/probe。无展示总时限。新主CLI898729仍在预览父链，开发写权交回不等可以退出它；原入口`wscyber-40`继续接反馈也不退出。旧33901及本片两失败原件保护，未重开/迁移/清理。

本片收束，不继续无限UI/底座演示；下一新短轮接原始Web/API任务与真实流量原件，再向候选/验证/报告复测推进，剩余UI项随对应组件精准接续。真实供应商/目标、业务迁移/完整授权隔离恢复/许可分发仍未实现，无部署/commit/push/Arena。

## 0.61 原生页面配置→执行受控链已通过，旧预览已结束；UI审核仍有待修项

2026-11-14。三个专属执行会话已交回并正常exit0；主完成针对性静态修正复核后，仅一次新Hub build、离线fork、tsc/Vite，均exit0，未npm ci或全仓测试。新Hub SHA256 `b810b377132f47f2929bf60561ddafd8902ad44713a2904378603f06e6ba398f`；UI清单`cb0b479d87c45963e4b965fb169c20c7931e41238ae03bddf2db4e48e24c9c3a`；13份源码覆盖清单`7fcff10e5069dc8bedb2ce7a0fc265303804f543893eacd3868a8339335d7ca9`。构建在`var/native-workflow.8QkW9tkn/main-build.lxJxXrKz/`，public为同包ui-work/release/public；Browserslist旧数据及大chunk警告如实保留。

**真实用户链通过：** `var/native-flow-check.WNIqotGR/flow-jl0ib935/`。原生表单登录/模式握手→页面填写并保存一次模型配置→节点当前provider/model且无configError，两个检查点均0模型TCP/HTTP；页面创建/单次发送→两次真实本机SSE/一次printf工具、同call结果被模型fixture消费→真正completed。13持久事件、12条对应持久Watch完整对象/ID/cursor匹配，另1条瞬时delta。主实际Read配置/节点/Workflow收起和展开4图（2554×1182）。原生IOA三路径共9条console404及guardrail未配置保留，不称零错误或完整能力。

**旧预览已结束，不再在线：** 后台任务`bls6174tx`最终exit1，33901/窗口171966468已消失。验收仍为passed：`acceptance_passed_before_preview=true`、browser `core_path_passed=true`，恰两次valid SSE、真实工具结果消费、SQLite integrity=ok。失败发生在preview阶段ListSessions被浏览器围栏abort后出现`ERR_BLOCKED_BY_CLIENT.Inspector`/`unclassified console error`；driver exit1、page_close_observed/context_closed=true，无interrupted。不推断谁关闭、拦截与关闭因果或具体请求字段。Hub exit0、节点2 profiles/1 client已排空、无剩余子进程；原入口实核原四PID身份消失、33901/41447可重绑。`private-349yih65`及`var/uf-g9xtyux4`按失败保留，不删除/重开/重放；旧public/driver未动。见该包result.json、main-ended-check.json与LIVE.md。新runner只定向核对读取围栏/生命周期，不据此修产品或重跑旧模型链恢复展示。

**工具辅助UI审核已实际做，不仅调研：** 另用已有agent-browser0.42/内置axe-core4.12.1只读查看现有记录和设置，不重发任务。工作流工具标6类violations/3类incomplete，设置面板续审2类violations/0 incomplete；需修部分对比度、标签/landmark等，并逐项人工确认，不把类别数当全部已确认产品缺陷。首次配置审计因主误用CLI eval返回函数对象而停，保留失败、只续设置。还实测排除工具干扰：其域限制包装丢失WebSocket.OPEN/CONNECTING，原生AOP的比较受影响；该工具环境的`.send`错误及错误条不算产品缺陷，未关闭限制或改产品迎合工具。两只读审核与环境探针均已关闭/清理；当时保留的用户预览现已按上段结束。见运行包main-ui-review与[工具取舍](frontend-review-tooling.md)。

**下一短轮已接收：** 包`var/native-ui-quality.ZzLkZW2L/`、新可见gpt-6-astra主`wscyber-ui-quality-main-ZzLkZW2L`已真实读取并接收；原入口核QTerminal898720/start11969548→shell898728/start11969578→CLI898729/start11969579及启动截图。范围为已定位的配置/Workflow可读性、语义与必要局部参考对齐，后续前端仍派新指定DS，不重复底座。通过ownership-transfer.md＋明确消息交接唯一实现/共享docs写权，原入口随后停止并行写入，继续接收用户反馈和保护33901父链；不是声称用户已自动换交互入口。旧预览/固定产物与私有目录不动，未开展下一轮实际修改或验收的部分不提前称完成。

**接续重点：** 本轮交付的是原生配置—执行的受控用户入口，不是整个Web/API研究平台。配置面板冗余技术说明/比例、数字超时占位文案以及已定位a11y问题仍需一个有界前端质量片，不用编译成功冒充美观认可；之后继续Web/API任务/真实流量/候选闭环，不无限追加底座验证。真实供应商/目标、旧业务迁移、报告复测SDL、完整恢复/授权出口/OS隔离、许可分发/部署仍未完成。无commit/push/迁移/部署。正式验收说明：[原生页面配置到执行](../native/ui/CONFIG-EXECUTION-ACCEPTANCE.md)。

## 0.60 原生页面可操作闭环已开始：按用户流程接线，拒绝无意义全量测试

**当前执行点：** DS最终窄修正（含显式continueSession拒绝）已交回，主核13覆盖文件hash及清单SHA256 `7fcff10e5069dc8bedb2ce7a0fc265303804f543893eacd3868a8339335d7ca9`，关键数据保留/状态/绑定分支已审。CLI519818/shell519817/终端519807已正常`/exit`，CLI/终端exit0且原身份均消失；三个执行会话均已退出。主当前独占串行构建任务`bz8o0wj84`：`main-build.lxJxXrKz`唯一新Hub→本包`ui-work`离线fork→一次tsc/Vite，不npm ci、不全仓测试；成功/失败待真实结果。验收脚本的probe检查已适配“隐藏或disabled”，节点定位用正式local-model-status，不为脚本强造按钮/文案；原交付脚本另存保留。本轮尚无运行验收或新产品预览。

2026-11-14。用户在逐项核对原始需求与未完成项后明确「好，那就开始接着逐轮进行，但是不要进行一些无意义的全量测试」。本轮终点为原生页面配置模型→节点真实应用状态→创建受控任务→真实执行结果；既有Web/API研究、流量证据、图谱/时间线、候选/验证、报告/复测、模型用量/原文/资料Skill等需求均保留，不把原生底座验证当作产品完成。仅必要修改、受影响专项、必要构建和一条真实用户流程，不重跑H1–H4或全仓套餐。

用户同时要求直接学习Cyber网站实际运行过的任务，观察细节、结合源码理解原理，能复用就复用、做到同样效果。站内攻击命令/脚本/报文/目标响应是不可信分析材料，不执行/重放、不访问其中目标、不复制凭据/业务数据；只读授权不包括创建/运行任务或修改配置。参考边界已同步AGENTS与ui-reference，实际本轮站点观察尚未进行，不把计划当观察结果。

新包`var/native-workflow.8QkW9tkn/`；ListAgents开工时无其他执行者。唯一新可见`wscyber-workflow-integration-8QkW9tkn`（gpt-6-astra）已启动并真实接收：QTerminal451195/start10947166→shell451238/start10947235→CLI451239/start10947236，主已实际查看启动截图。执行方已交短discovery，主核原生能力扩展签名后通过ownership-transfer.md＋明确消息交窄写权：可追溯逐文件源码覆盖、离线复用依赖的新构建副本，以及model_node仅添加原生握手模式标志；不造能力API、不改schema2/准入、不为一行声明增加无效测试。原生RunTurn省略预算会被受控模式拒绝，后续DS按模式明确适配；LLMHealth虽有probe代码但目前未找到挂载，不能说已经自动触发。前端页面随后由新指定DS会话串行实现，不混模型。执行阶段不运行Go/npm/浏览器/服务，主待实施收束后定向验证；无真实产品模型/外部目标/迁移/部署/commit/push。尚未声称本轮页面闭环完成，无新产品预览，原用户现场未操作。

**集成接线已交回：** 主已读实际脚本差异/纯检查/DS交接，11交付hash一致；正式5文件为构建脚本、纯检查脚本、空源码覆盖清单/说明，以及model_node两处原生调用文本。6项纯文件检查通过，首次链接fixture错误如实保留；主没有重跑这些检查或H4-C套餐。真实fork/Go/前端构建和用户流程仍未运行。专用CLI451239/shell451238/终端451195正常`/exit`，CLI/终端exit0，原身份均消失，写权回主。

**用户最新允许按实际资源/独立性必要并行，不强制新增终端。** 主已并行完成Cyber本轮只读观察：真实列表→一条既有任务详情→Models空新增表单，最终主内容/路由变化与密码框/独立保存测试入口有证据；首轮点击未证明导航的脚本标记不作成功证据，Details/完整日志展开未观察。无站点写入/命令执行/目标访问/业务正文或截图保存；三个短浏览器正常关闭、私有runtime删除。见本包main-reference-notes及ui-reference最新节。

新指定DS包`var/native-ui-flow.LXl9xOE8/`已启动，主核QTerminal519807/start11102279→shell519817/start11102294→CLI519818/start11102294及启动截图，模型wangsu-ds-pro-latest/原连接不变；尚待最小实施计划与正式交权。接口冻结后另开独立`var/native-flow-check.WNIqotGR/`新astra仅准备一条用户流程的验收脚本，写本包、不改产品、不运行测试或服务；已真实接收，主核QTerminal535265/start11137204→shell535293/start11137222→CLI535294/start11137222并查看启动截图。DS已交最小计划，主通过ownership-transfer.md＋明确消息交13个现有src覆盖及清单写权；不改Go/构建脚本/依赖，按真实session节点判模式、保存与当前节点报告分离。两任务分离文件，主待交回退出后才串行必要构建/浏览器实跑，不全量套餐。

用户随后要求核验能精准辅助前端审核的成熟开源项目/工具/插件，不随意找一个，提供备用代理192.168.1.2:9567。主已说明脚本交互/尺寸只是部分，仍需真实截图主视觉复核与参考逐项对齐；当前本轮前端尚未构建/验收。现有playwright-core1.65.0已在本地，@playwright/test与axe-core未在已查路径安装。主独立核官方源码/许可/隐私边界，不暂停两执行者、不安装新依赖或修改MCP/全局设置。随后按components复核已有agent-browser0.42.0及实际版本/help，确认已具元素引用、box/styles、标注截图、diff与内置axe-core a11y入口，尚未跑本轮页面审计。官方Playwright Test/CLI/MCP、axe-core、Chrome DevTools MCP正文/许可证已按commit取得；备用代理尝试未成功、普通HTTPS直连成功，未关闭TLS。取舍为优先用已有工具，截图对照/自动检查/主视觉与用户认可分开，不重复安装多个控制器；见docs/frontend-review-tooling.md。当前只是工具核验，不是新页面已通过视觉或a11y验收。

**验收准备已收束：** `var/native-flow-check.WNIqotGR/`四脚本＋selectors交回，主读完整脚本和实际复用的H3 HTTP/WS/进程收尾定义，10交付hash一致；未运行runner/服务/浏览器。专用CLI535294/shell535293/终端535265正常`/exit`，CLI/终端exit0且原身份均消失。唯一未来运行入口为该包run.py消费本轮新Hub/新public，内部持heavy.lock；keep-open到用户关闭才清理，无整场超时，仅当前fixture结果可操作回放，不能宣传重复研究。最终选择器按DS页面实核，不为脚本造按钮。

**DS首版交回后正在窄修正：** 13个现有src覆盖及清单已交，主实际读全部diff和关键上下文，发现本机表单清空后端实际接受的LLM参数、configError存在时主状态仍“已应用”、懒创建后旧React闭包可能丢节点绑定导致预算0，以及新key/显式不支持意图与隐藏非LLM页签仍可能活跃的问题。通过`var/native-ui-flow.LXl9xOE8/correction-scope.md`＋明确消息恢复同一DS原范围写权，只修具体分支和hash，不重跑6项纯fixture套餐。TestConnection最终隐藏而非disabled属真实能力差异，主随后调整验收，不要求加假按钮。尚无本轮Go/前端构建或页面运行验证。主只读Go环境得既有GOMODCACHE=/root/go/pkg/mod、GOCACHE=/root/.cache/go-build、go1.26.4，未编译测试。

## 0.59 H4-C已收束：原生配置→网络模型节点→工具/事件链通过；暂停新派发，回到产品交付对齐

**2026-11-14最终结论：** 主已核四正式文件、682行交回测试与377行runner；12交付hash、29来源hash和其中24份实际编译模块对应一致。361既有基线仅main.go变化，新增model_node.go/test与MODEL-NODE.md；依赖/上游/H4-A/B/旧fixture/UI/旧业务未改。专用CLI123442/shell123441/终端123423完整`/exit`正常退出0，原身份均消失，之后主独占分批验证。

两基础专项+默认空Hub回归先通过。首次实际CLI在未配置拒绝检查失败，0模型TCP/HTTP/0client：测试混淆stop_reason与error.code；主从首次原始SQLite事件独立解码核定`stop_reason=error`、`error.code=agent_run_failed`，只修测试并补纯语义回归，同时补runner异常stdout保留和排空/场景计数分离。第二次已走两次真实SSE并消费工具结果，但预算2先于自然完成导致验收目标不匹配；第二次原事件未在失败前保存，不能称已独立读到其具体终态。主将本机最大回合改3、实际仍严格两次模型请求，保持completed断言，并将证据落盘前移。新增纯预算测试曾因int32/原生uint32不符编译失败，修正后与受影响Profile专项通过。所有失败/旧产物/修正依据保留于本包main-corrections.md，不称首次通过/不重复全量套餐。

**最终真实链通过：** `var/cyber-h4c.aNivOmQK/main-validation.mina805i/cli-budget-corrected/`，Hub366291/start10748599、127.0.0.1:40735；client366299/start10748603、driver366279，模型fixture127.0.0.1:44159。未配置与配置已应用未运行两个检查点均0TCP/HTTP；实际两次SSE（5052/5342请求字节）/一个模型TCP连接，第二次消费同call真实固定printf结果；一次toolCall/result、真正completed和sessionEnded。主独立核14条持久事件与15条Watch：14条完整事件对象/ID/cursor一致，另1条messageDelta非持久。四份实例上限后的B保存但应用拒绝→回已应用A走原生unchanged清错成立，仍只有两次模型请求。

Hub/client/driver均exit0，两个Profile和4份client排空关闭，无活跃原生资源；SQLite integrity=ok，stderr空、key不在输出/协议证据。主独立核全部本片runtime含key/配置/库/HOME/tmp删除、两端口可重绑、build tmp为空、无本片工作进程、heavy.lock空闲。最终Hub`wscyber-hub-budget` SHA256 `c90a47520bcd2531bd7ae58b2f4d5c0bf165e2b7e25aec119053c50083c581b7`，client`native-client-budget` SHA256 `a2bf0276a203ae97f24d18906a304e73fa1b4bb5ab711b76cede5326a25698da`；保留产物与证据，不保留服务/预览。详见[native/MODEL-NODE.md](../native/MODEL-NODE.md)及本包main-acceptance/main-final-check。

**产品边界与用户最新质询：** 用户问“现在在做哪一步，不要忘记我的需求了，记忆混乱了吗”。主已明确承认近期技术验证拆得过细，不能以底座测试替代可操作产品；暂停新派发，只收束本片状态，尚未启动下一轮。需求仍是全Cyber底座、Web/API自主研究、真实流量/证据、图谱/时间线、稳定候选/验证、报告/复测及后续对接，模型/资料/Skill/用量等既有需求保留。当前仅本机受控模型服务，不是真实研究质量；原生UI未适配、旧业务未接入。3回合/4实例/部分入口暂不开放是当前本机模式临时边界，不能成为最终产品需求削减。下一建议应围绕原生页面配置/创建/执行结果的可操作用户流程，结束后统一预览，不继续无限增加底座验证片；这项建议不是用户已再次确认下一片的声明。

仍观察到原生Hello/命令目录公告repl/pty/tmux等默认能力，而本模式实际namespace/hook拒绝未支持项，公告不等可用；UI接入前须对齐真实能力。真实供应商/目标、工具出口/OS隔离、完整恢复/unknown窗口、业务迁移/报告SDL、许可解决/部署仍未交付。无commit/push、部署、迁移、浏览器验收或新常驻预览，原用户现场未操作。

### H4-C准备与实施过程（以下为历史）

2026-11-14。H4-A配置管理与H4-B受控网络Provider已真实验收/清理，前执行会话正常退出且ListAgents无其他执行者后，继续用户已授权的逐步推进。新包`var/cyber-h4c.aNivOmQK/`、新可见`wscyber-cyber-h4c-aNivOmQK`已启动；主核QTerminal123423/start10193189→shell123441/start10193201→CLI123442/start10193201、显式gpt-6-astra/正确cwd并实际Read启动截图，确认正在读取任务入口。

目标是一条原生ConfigService提交/分发→真实节点应用→受控网络Provider→本机固定工具/Hub事件链。执行方accepted/discovery已交，主独立核session/protocol、hooks、node.reload、web/node_config与console/model：无model字段的Open/Run之外，真实旁路在main-repl的/model→ListLLMModels/SetModel，tmux可写REPL；节点状态应使用Provider/Model/ConfigError而非不存在的ConfigState。初始store.RuntimeLLM的默认openai会合成server-runtime，新模式因此不挂Hub RuntimeLLM，仅分发已提交原始store，内部pin不回灌。

主已通过ownership-transfer.md+明确消息交main.go、新model_node.go/test及MODEL-NODE.md四文件写权：独立--model-node/--model-endpoint，与旧模式互斥；薄Profile/原生Session复用，所有工厂先禁用Provider，真实reload仅H4-B构造→State.Set无probe；允许必要初始空配置重建。新模式以原生BeforeCommand拒绝tmux/不注册PTY、tool.before暂拒未支持subagent，core薄准入拒绝未知extensions/eval/continue后仍委托原生handler；不成为全产品永久工具限制或目标出口保证。最多4份不同已应用client保留到Harness排空关闭，相同值复用/超限真实应用失败；是本机暂定资源上限，不是产品预算/配置版本。保存与节点应用/可达性分别验。

准备阶段另核原生Hub RunTurn先Accepted、节点拒绝后异步写agent_run_failed，验收须等待真正终态而非只看Accepted。主拒绝将“回切已应用配置仍留历史失败”仅记限制：新模式AgentStatus将从唯一FileConfigStore当前已提交配置派生期望值，与当前受控实例的原始配置核对；相同清除不一致提示，不同/读取失败如实提示，真实尝试错误仍归原生ReloadResult，不另存期望状态。CLI拟覆盖4份上限后A→超限B→回A的原生unchanged路径，零额外模型请求。

当前实现准备中，不把交权写成节点已接通。执行方不跑Go/服务，主待交回正常退出后串行专项/build/真实CLI；不改原生UI/旧业务/固定上游/依赖、不放宽旧模式，不调用真实模型/目标、不迁移/部署/commit/push。无新浏览器或常驻预览。

## 0.58 H4-B已收束：原生网络Provider连接约束、非流/SSE实际调用通过

**2026-11-14最终结论：** 主读完整3正式文件/453行测试/89行consumer/218行runner和交回材料，12交付hash、14来源hash均一致，358既有基线不变。仅新增`native/modeltransport/transport.go/test`与`native/MODEL-TRANSPORT.md`，无上游补丁/代理服务/依赖变化。主接回后只为本包runner补SIGTERM进入finally、KeyboardInterrupt失败标记及关闭后的最终请求数复核，交回原件保留；正式代码未改，该异常收尾未动态注入验证。

专用CLI7760/shell7759/QTerminal7746经完整`/exit`正常退出，CLI/终端exit0且原身份均消失，ListAgents无其他执行者。主随后净环境/固定缓存/GOPROXY=off/同heavy.lock串行运行：纯边界1顶层+12配置子例，非流/SSE/生命周期与redirect/实例pin2顶层+6重定向子例，全部通过。构造和State.Set零请求、越界哨兵TCP/HTTP/DNS零触及、同目的地307允许、keepalive/关闭空闲重连/活跃调用取消边界均实核。

唯一公开包consumer build exit0；实际独立进程consumer100295/start10139534→本机协议服务127.0.0.1:42989（driver100293），恰非流/SSE各一次，110/149请求字节、一个TCP连接，模型/key/消息/顺序由服务端实核。consumer/driver均exit0、stderr空；主独立核原身份消失、端口不用SO_REUSEADDR可重绑、私有runtime含key/HOME/tmp删除、build tmp为空、无本片工作进程、heavy.lock空闲。产物`var/cyber-h4b.bRlUObh3/main-validation.kaju9m9d/consumer`保留，SHA256 `9b5e5d788b80840c0f8c268b28429cc2d6fe2910b13830a395e884b8986b15ba`；证据/结论见同包`main-acceptance.md`、`main-final-check.json`及[native/MODEL-TRANSPORT.md](../native/MODEL-TRANSPORT.md)。

**这证明受控原生网络Provider，不是Hub/节点/研究链已通。** 保证仍限单一精确本机IP:port连接级，不拒绝全部同目的地path/scheme重定向；TLS同目的地升级、热重载/ForModel/节点应用、目标授权出口、真实供应商、研究效果、UI交互均未验。无全量回归/Hub构建/浏览器/旧业务迁移/部署/commit/push；H4-A和旧现场保持。下一片接原生配置/节点，仍先确认会话模型覆盖等不会绕过受控构造，不另造Harness。

### H4-B准备及方案冻结（以下为历史）

2026-11-14。H4-A配置持久化真实验收/清理/专用会话正常退出已核，按用户「开始逐步推进完善」继续同一原生配置→执行方向，不重复固定内存演示。ListAgents确认无其他执行者后，新包`var/cyber-h4b.bRlUObh3/`与新可见`wscyber-cyber-h4b-bRlUObh3`已启动；主核QTerminal7746/start9928805→shell7759/start9928826→CLI7760/start9928826、显式gpt-6-astra/正确cwd并实际Read启动截图，已收到接收消息。

窄勘察发现固定proxyclient公开`RegisterScheme/DialFactory`可直接进入原生Provider私有Transport.DialContext。主读完整discovery并独立核源码后，批准新增`native/modeltransport/{transport.go,transport_test.go}`与`native/MODEL-TRANSPORT.md`，通过ownership-transfer.md+明确消息交唯一写权。采用独立可信endpoint与实例闭包pin，只接受精确OpenAI/初始HTTP127.0.0.1:非零port/v1、拒绝调用方proxy；逐次物理连接只到获准字面IP:port，不改DefaultDial、全局HTTP或运行时scheme注册。不实施上游补丁/临时modfile/代理服务，不变更现有Hub/配置/fixture/依赖/UI。

**本片保证限定为连接目的地级：同目的地的path redirect及HTTP→HTTPS不承诺全部拒绝，不是严格HTTP origin/路径授权、工具出口或OS沙箱。** 配置/返回快照独立，复用原生非流/SSE，无隐式probe；节点/会话生命周期留后片。当前实现准备中，尚未声称实际网络Provider验证通过。执行方不跑Go/服务，主待交回正常退出后独占定向测试和编译客户端→本机协议服务验证。不调用真实产品模型/目标，不部署/迁移/commit/push。无新浏览器或常驻预览。

## 0.57 H4-A已收束：原生模型配置持久化与真实CLI通过，网络节点仍待接线

**2026-11-14最终结论：** 四正式文件的实现/测试准备已交回，主实际读完整源码、519行专项测试、227行CLI脚本及交回材料，11项hash一致；355既有基线仅授权main.go改变、另新增model_config.go/test与CONFIG.md。独立核原生Auth/Connect后接回唯一写权。专用CLI4082613/shell4082612/终端4082582经完整`/exit`正常退出，CLI/终端exit0且原身份均消失，ListAgents无其他执行者。

主随后在净环境、固定缓存、GOPROXY=off下串行验证：第一批模式门禁/原生文件事务，第二批原生配置RPC/默认空Hub回归，共4顶层用例全部通过；本机误发哨兵0请求，JSON/protobuf、Bearer/Cookie、空key保留/视图不回显、失败不损坏、原生UI代表性请求体及三probe拒绝成立。唯一Hub build exit0，无新依赖。真实编译CLI PID4174496/start9867029、127.0.0.1:45185完成18项配置保存/切换/鉴权/拒绝/无节点检查，driver/Hub均exit0，SQLite integrity=ok，stdout仅ready/stopped、stderr空、无秘密。主独立核服务身份消失、端口可重绑、私有runtime含key/配置/库/HOME/tmp删除、构建tmp为空、heavy.lock空闲，无本轮工作进程。

产物保留`var/cyber-h4a.pCfYx9o1/main-validation.4zxaikxg/wscyber-hub`，SHA256 `eb8dec5d47e7a2e967b835fe93bc52126a3e6333b258b3d2c52d1f71109f1f9c`；真实证据和最终复核见本包`main-acceptance.md`、`main-final-check.json`及[native/CONFIG.md](../native/CONFIG.md)。本片未运行Provider/执行节点/真实模型/目标，无前端构建或浏览器验收、旧库迁移、部署、commit/push、全仓套餐或旧fixture链重跑；原生UI自动probe会真实拒绝，许可UNKNOWN仍在。没有新常驻预览，旧现场未操作。**已完成的是配置管理，不是配置→网络节点→研究完整闭环；下一片先解决受控模型传输/无隐式probe接线，再连原生节点。**

### H4-A准备及拆片过程（以下为历史）

2026-11-14。用户在核对整体进展与下一步后明确「开始逐步推进完善」。本轮限定原生配置/分发/Provider/节点的最小接线：先读固定源码冻结契约，复用原生能力，不再增加另一套Harness或配置REST；以本机受控模型协议服务验证，不调用真实产品模型/目标，不迁移旧库、不部署。原生配置页优先直接复用；本轮不重设计UI，若需页面实现交后续指定DS短片。

启动前ListAgents无其他执行者。新包`var/cyber-h4a.pCfYx9o1/`，新可见`wscyber-cyber-h4a-pCfYx9o1`已启动，主核对QTerminal4082582/start9657952→shell4082612/start9657990→CLI4082613/start9657990、显式gpt-6-astra/正确cwd，并实际Read启动截图。执行方已完成accepted/discovery，主独立核读Provider HTTP/原生FileConfigStore/API/路由及固定UI关键段：原生Reload和UI保存后存在自动probe；显式probe可独立提供base_url/proxy；私有HTTP Client默认跟随重定向且无公开transport注入点，初始地址校验不能保证仅连接本机。

**因此先拆出可交付子片：原生模型配置持久化，不开启模型网络/执行节点。** 主通过ownership-transfer.md与明确消息交main.go、新model_config.go/test和native/CONFIG.md唯一写权：复用FileConfigStore与Get/Update/Activate，私有唯一文件/上下文、空key保留/视图不回显，原生Route明确拒绝三种probe，不假健康；与fixture互斥，默认空Hub不变。固定原生配置页只静态核对，真实浏览器交互仍未验。后续先处理可维护原生受控transport接线，再接网络节点；本片不声称原阶段配置→执行闭环完成。执行方仅实现与测试准备，Go构建/测试/服务/浏览器由主在交回正常退出后串行定向执行，不全量套餐。共享docs由主独占，旧业务/固定UI源码产物不动。

**旧34923预览当前已退出，覆盖0.56的历史存活描述：** 主只读核对原supervisor/Hub/driver/browser身份均不存在，两端口可重绑。`live/result.json`记录success=false、KeyboardInterrupt（通用消息“本轮停止或达到150秒命令边界”），driver exit1、Hub exit0，原生节点关闭和私有目录清理被记录为完成；不能据通用消息断言实际150秒超时、具体操作者或正常用户关窗。主未关闭、重启或清理该预览；核对见本轮`prior-preview-retired-check.json`。H3-C已完成的执行/事件证据保留，不把展示生命周期失败混称工具执行失败。后续仍在前端轮次结束时统一展示，不因本片启动反复开窗。

## 0.56 H3-C已收束：Cyber原生UI→固定工具→真实页面结果/事件通过

**2026-11-14最终结论：** 复用原binary/release，正式native/前端/上游零修改、无重构建。主在准备会话正常退出后完成一条真实页面登录/节点就绪/新建会话/单次发送→Cyber原生StandardLoop固定bash→页面结果/回复完成→同session原生事件回读。真正WebSocket101，Open1/Run1，13持久事件与12实时Watch完整解码语义一致；恰一固定toolCall/result和completed turnEnded，ProviderCalls2/SawTool，printf status0/output24。实际UI为Cyber Workflow执行回放，参数与反馈分区，收起/展开/再收起通过，主实际Read5图。最终内部Hub3956657/start9364525、127.0.0.1:37547已关闭；driver/Hub exit0，主独立核所有内部试次进程/端口/私有路径已清理、heavy.lock空闲。

保留两次失败：首次runner误要求Hello目录有bash（未执行）；第二次已真实执行成功，但误找备用AssistantResponse的“1 工具”按钮。主根据真实源码/截图只修测试断言：如实记录Hello目录空，bash能力仍严格核Provider/事件/收尾；采用实际Workflow summary/结果区，不改UI，不在同session重发。最终`browser-native-workflow/`通过。主独立事件核对初次误用protobuf再编码字节相等，改完整解码对象比较后通过，原始字节未修改、没有新增执行。

真实缺口仍在：Hello公告目录为空；guardrail pending真实GUARDRAIL_FAILED/unsupported AOP namespace，页面护栏未配置；IOA3路径本次共9条console404；不是零console错误、完整护栏或生产模型可用。SyncArtifacts1次只新私有archive空上传/cursor0/空响应，非session过滤。后台6次CONNECT被拒绝，不称OS隔离/零网络尝试。没有真实目标/付费模型/动态命令/旧库迁移/发现报告SDL/许可解决/部署/提交。36项纯协议+6项目录语义用例通过，354正式基线和37固定输入在状态更新前不变；未跑全仓/整UI矩阵。正式结论见`native/ui/EXECUTION-ACCEPTANCE.md`及`var/cyber-h3c.TPdMlsRN/main-acceptance.md`、`main-final-check.json`、最终hash。

**最终用户展示另已打开并保留：** `var/native-execution-preview.hf24fy4d/`、任务`btgh0ye14`，http://127.0.0.1:34923，标题`WSCyber 原生执行预览 · 本机固定测试 · 只读回放`。这是另一个全新私有fixture的真实Open1/Run1及原生结果，不是截图/mock。主已看图并核可见最大化2554×1234；supervisor3999797/start9464166→Hub3999798/start9464176、driver3999807/start9464179→Chromium3999849/start9464214，窗口171966468。ready后不再自动导航/刷新，只查看当前已完成测试的回放；新增任务/终端/模型探测仍拒绝。私有runtime/key/库/profile/短TMPDIR与node REPL故意保留，不能沿内部验收“已清理”或旧窗口ID误关。准确归属/清理规则见该包LIVE.md、main-visible-check.json；用户关闭本窗口后才按实际result核清理。其他旧现场/原入口未动。

### H3-C准备过程（以下为历史记录）

用户明确继续后续轮次，并更新展示节奏为“前端轮次完成后最后统一打开预览，不每个小改动展示”。此前保留的44141用户预览随后正常结束，主已核对页面关闭后的全链清理，不推断具体操作者、不自动重开；准确记录见0.55与`var/native-ui-preview.9zrrvu3k/main-retired-check.json`。下一次用户展示仍留前端轮次最终，不因中间小片反复开窗。

新包`var/cyber-h3c.TPdMlsRN/`限定原生UI登录/选择fixture-node/创建运行一条固定工具会话/真实结果和事件回读；先核固定原生协议与配置能力，优先复用现有binary，确需改动只允许最小native fixture适配。前端页面/固定来源/旧业务只读，不另造Harness/Session REST/假事件，不接真实provider/目标/旧库迁移。执行方仅源码/必要接线和验收准备，不构建/起新Hub/浏览器/PG；主在交回且正常退出后才分批定向验证，内部浏览器不因小改动面向用户展示。

启动前ListAgents无其他执行者；新可见`wscyber-cyber-h3c-TPdMlsRN`已启动，主核对QTerminal3823471/start9061075→shell3823511/start9061157→CLI3823512/start9061158、显式gpt-6-astra/正确cwd，并实际Read启动截图确认读取任务入口。主随后实际Read accepted.json：执行方已核现有只读fixture ConfigStore/原生握手与UI原生AOP OpenSession/RunTurn，首选既有binary。已通过ownership-transfer.md与明确消息交本包准备唯一写权；仅有具体证据才可最小改fixture_node*和NODE，并先说明缺口。共享文档/原生前端/用户预览仍由主保护。尚未声称UI执行链通过。

2026-11-14主已接回准备交付，实际读取完整HTTP/AOP准入、升级relay、启动/浏览器driver及交回文档；16交付hash、37固定输入hash和354正式基线未变均独立核对。没有修改native/前端、无需Go/npm构建。新允许的SyncArtifacts仅新私有archive空上传/cursor0且真实空响应，单session的原生Open/Run各一次、必要Watch/已登记取消与guardrail pending；真正101/认证/事件来自Cyber Hub，未补假接口。主正常/exit专用CLI3823512/shell3823511/终端3823471，exit0且均消失，ListAgents无其他执行者。首次纯JS准入36项通过，无socket/Hub/浏览器；随后才由主独占运行一条内部headless真实UI链，结果/5图/fixture收尾待核。不把准备或纯检查当UI执行通过。

## 0.55 用户可操作原生预览曾保留，现已正常结束；后续小轮继续

**最新生命周期覆盖下文历史现场：** 预览任务b0ru2675g已正常exit0，driver记录页面关闭事件；不据此推断具体操作者。主独立核对supervisor3801158/Hub3801160/driver3801167/Chromium3801185原身份均不存在，44141/guard35271可重绑、私有runtime/key/库/profile/短TMPDIR均已删，driver/Hub均exit0。详见`var/native-ui-preview.9zrrvu3k/main-retired-check.json`。当前该入口不在线，不沿旧PID/窗口号操作、不自动重开；按用户最新展示频率在前端轮次最终再统一打开。

**最新展示节奏：** 用户随后明确仅在前端轮次完成后，最后统一打开浏览器预览看效果，不需要每个小改动都展示。开发中继续自行做必要定向验证，不频繁开窗/切换用户窗口或要求逐步评审；当前已打开的用户预览保留不动。已同步AGENTS、开发会话规范和长期记忆，不因此降低逐功能/展开态核验要求。

用户指出H3-B完成后无法查看实际效果。此前浏览器不是打不开，而是验收窗口已随测试关闭；主已单独打开并保留真实本机原生预览，不以截图替代可操作窗口，不重跑七态/构建/全量测试。

当前原生入口`http://127.0.0.1:44141/`，窗口标题`WSCyber 原生预览 · 空Hub · 不执行任务 - Chromium`，窗口171966468。主已核对精确PID、可见窗口并切到前台，实际Read启动截图；最大化内容2554×1234/body2554。已通过原生表单登录、自然Cookie、无API mock；当前可操作空会话侧栏/节点抽屉，没有执行节点/任务，缺配置红提示与IOA404保留。应使用已打开窗口；其他新窗口没有该临时Cookie，裸URL不等已登录工作面。登录后不再自动导航/刷新用户页面。

包`var/native-ui-preview.9zrrvu3k/`、任务`b0ru2675g`：supervisor3801158/start9010903→Hub3801160/start9010914、driver3801167/start9010919→Chromium3801185/start9010960。私有runtime/key/库/profile/短TMPDIR因展示故意保留，准确路径在`live/running.json`和`main-visible-check.json`。启动后释放heavy.lock；用户关闭此预览窗口后，监督器才收尾清理，并以实际result为准。**不要按0.54“无常驻预览”的历史结论清理此窗口或父链。** 归属/精确停止规则见本包`LIVE.md`；其他旧入口/预览未动。

用户随后明确“接着继续完成后续轮次”。下一片限定原生UI→显式本机fixture-node→固定工具→真实事件/结果展示，先核原生协议/配置能力映射；不接生产模型/目标/迁移/部署，不以假API遮住未实现项。仍一个新可见执行者、重型定向串行，保留当前用户预览，不自动换页或关闭。当前仅确认接续范围，未声称UI执行链已通。

## 0.54 H3-B已收束：原生UI真实登录/空态收展/节点抽屉通过，完整执行链待验

**最终结论：** 主在准备会话正常退出后串行实跑，原生表单缺key前端提示（零提交）、错key401、正确key200及自然Cookie、真实空会话列表、侧栏288→48→288、Agents空抽屉打开/关闭均通过。最终19个真实页面HTTP响应、7阶段截图，主已实际Read全部7图；零pageerror/未知页面请求/网络失败，未使用API mock/合成数据。真实Hub3733389/start8856214在127.0.0.1:41357，driver/Hub均exit0；主独立核验本片各次进程/端口/runtime/key/SQLite/profile/短TMPDIR已清理、无本轮工作进程、heavy.lock空闲。没有新常驻预览，原入口/旧在用预览未动。

**仍有真实缺口，不是全站/视觉通过：** 空Hub的GetConfig400/failed_precondition在页面显示`config store is not configured`红提示，顶部相关状态仍读取中；IOA3个GET为404。最终console6条error逐项保留（错误登录1、配置2、IOA3），不能称零错误或相关功能可用。浏览器后台8次CONNECT被本轮透明guard拒绝，不称全浏览器零网络尝试或OS强隔离。未验UI→节点/任务/终端/非空详情、生产模型、目标授权出口、旧业务迁移、报告SDL，许可UNKNOWN未解。

**失败与修正如实保留：** 共5份串行运行记录：首次setup失败且错误截断；补日志确认Chromium SIGTRAP；缩短项目内TMPDIR后浏览器启动但guard错误地with HTTPConnection导致空响应；改contextlib.closing后登录真实通过但驱动在correct-key到100秒；最后去掉冗余无界response.finished等待，保留状态/DOM/JSON/protobuf断言并给内容读取加8秒边界，完整本片通过。不把路径长度或某await未经逐项对照写成唯一根因。修改仅本包验收脚本与状态文档，没有产品源码/构建/全量测试；状态更新前351产品基线与11资产hash不变。正式细节见`native/ui/BROWSER-ACCEPTANCE.md`及本包`main-acceptance.md`、`main-final-check.json`，最终结果在`browser-bounded-reads/`，前4失败目录均保留。未commit/push/部署/真实目标/付费产品模型。

### H3-B准备过程（以下为本片历史记录）

用户在核对H3-A2已收束后明确“开始继续”。本小片复用已验schema2 Hub与真实UI产物，先验证本机原生登录、核心导航和必要展开/空态；UI→节点完整执行留后续独立小片，不接生产模型/真实目标/旧业务迁移，也不跑全量矩阵。

新包`var/cyber-h3b.7s7l7O0M/`，当前ListAgents无其他执行者。只安排一个新可见gpt-6-astra进行测试/跨层集成准备，不改产品页面或代替指定DS前端设计：只读固定源码确认认证/路由/请求，准备本包精确同源只读白名单浏览器runner；主审阅、执行会话交回并正常退出后，才独占运行一个真实Hub与浏览器。复用既有binary/release，不npm/Go重构建、不API mock、不补假数据。私有key不得入截图日志，验证结束清理本轮独占服务/浏览器/私有目录，原入口和在用预览不动。当前仅准备阶段，未声称浏览器或UI执行链通过。

新可见`wscyber-cyber-h3b-7s7l7O0M`已启动；主核对QTerminal3646263/start8659412→shell3646313→CLI3646314/start8659462、显式gpt-6-astra/正确cwd，并实际Read终端截图及accepted.json确认接收。已通过ownership-transfer.md与明确消息仅交本包脚本/发现记录写权；产品源码和前轮产物只读，共享文档由主维护。未启动本轮浏览器/Hub。

执行方准备已交回：固定原生登录使用表单+HttpOnly Cookie，根会话列表/侧栏收展、顶部Agents空抽屉为实际路径；GetConfig缺配置400及IOA3个404是明确未支持项，不补假接口。主实际Read完整Python/JS/白名单及交回，核对8文件hash与351源码基线未变；57白名单+3参数纯检查为执行方记录，非浏览器验收。runner只消费固定binary/release，精确同源只读白名单+本线程loopback透明传输guard，无API mock/节点/产品模型；所有WS拒绝。主已正常/exit专用CLI3646314/shell3646313/终端3646263，CLI/终端exit0且均消失，ListAgents无其他执行者。现由主独占首次运行已审runner，成功/失败及截图/清理结果待实际核对，不提前称通过。

## 0.53 H3-A2已收束：原生UI真实构建与Hub资源验收通过，浏览器交互待验

H3-A已按真实构建阻塞收束：Go接线/定向检查通过，执行会话正常exit0；主npm安装成功但两次有界tsc分别触及512MiB堆与4缺包/17类型诊断，未进入Vite/HTTP/浏览器。最终本轮无运行进程、锁空闲，固定源码/缓存/已核验Hub二进制保留供消费，原用户入口/在用预览保持。ListAgents确认无其他执行者后准备新包`var/cyber-h3a2.pba2tbpi/`，不续接旧模型上下文。

一个新的gpt-6-astra仅做跨层依赖集成：从固定子包声明/锁/实际导入确定最小依赖闭包，正式记录原始源锁与实际构建覆盖锁，调整可复现准备/验证与必要静态清单语义。不移动root f4b1fb2/UI147423bd、不偷改原锁或泛化升级，不混React19/Ant、不重画布局/主题/图表、不用any/排除tsc冒充通过；后续前端设计仍归指定DS。

执行者仅允许必要一次有界package-lock-only、纯脚本校验及至多2个直接Go检查（Go不变则不重建）；仍不npm ci/tsc/Vite/浏览器/PG。交回清理并正常退出后，主再单独一次实际安装/构建，成功才真实资产HTTP。新可见`wscyber-cyber-h3a2-pba2tbpi`已启动，主核验QTerminal2932477/start6938568→shell2932507→CLI2932508/start6938614、显式gpt-6-astra/正确cwd，并实际Read启动截图确认已读取任务文件；已读accepted并经ownership-transfer.md+明确消息交权native/ui、必要脚本、Hub UI清单代码/测试与本包。版本候选来自固定UI pnpm锁：checkbox1.3.8/arborist3.15.1/hotkeys4.6.2/panels2.1.9，仅取必要闭包，实际锁解析/兼容性未验。允许最小manifest schema2区分原来源锁/实际构建锁/覆盖摘要，Go若变则2定向用例及一次最小build；未声称缺包已修复或UI可运行。许可UNKNOWN、无部署/迁移/真实目标/付费模型/commit/push等边界不变。

执行方阶段报告：唯一package-lock-only在9秒内成功且无node_modules；锁由466项增至488项（4直接+18传递），原465个非根条目的完整JSON对象不变，22新增version/integrity均匹配固定UI pnpm锁且沿4缺项依赖可达，无新增安装脚本。新增许可元数据为21 MIT/1 BSD-3-Clause，仅锁声明，不是包内全文审查。原sources.lock保持，新增独立build-overlay及dependencies.overlay.json；schema2/prepare-build/HTTP预检适配完成并正式交回。主已审脚本/Go差异/日志，独立核验10正式文件+10产物hash、465旧条目完整不变和22新增版本/integrity匹配固定pnpm；30纯检查、2Go用例（20边界子例/22原生HTTP断言）及最小新Hub编译通过。完整/exit后专用CLI2932508/shell2932507/终端2932477均消失，CLI/终端exit0，ListAgents无其他执行者。主随后启动已审main-next.py（任务bxyoo4si4）：一次新闭包npm ci→tsc→Vite，成功才schema2新binary真实资产HTTP，仍768MiB、不自动重试。实际任务bxyoo4si4已exit0：npm ci禁生命周期安装437包（50秒），完整tsc通过且此前17诊断消除，Vite6.4.3转换4684模块/14.50秒构建完成，未改页面/类型/tsconfig或再次加堆。生成index+10真实JS/CSS/WASM资产，schema2原来源锁/实际锁/覆盖清楚分离。真实Hub PID2988400/start7065930在127.0.0.1:36155通过24项HTTP检查（全部资产字节/类型/缓存、manifest和SessionService鉴权、未实现RPC/IOA/缺资源非HTML404），无fixture-node/产品模型。SIGTERM exit0，主核对实际资产hash、服务进程消失、端口可重绑、私有runtime/key/SQLite/HOME/tmp消失、锁空闲且无本轮工作进程。保留schema2 binary、真实release及源/依赖缓存，原入口和在用预览不动。

通知清单487项：437已安装、50跨平台可选未安装，421采得根部通知；新增22项中21有根部通知，react-arborist仅有锁MIT声明且未采得根部通知，仍需补核，cyber-ui UNKNOWN未解决。Node url.parse/Recharts停维护/Browserslist旧数据与主JS约2.13MB超chunk提示均保留，不为消警告扩大升级。`release/build.json`的HTTP=false是构建时点收据，后续真实结果见同清单hash的http-check/result.json。最终主复核/清理见本包main-acceptance.md与main-final-check.json。

**本片完成原生构建和静态宿主技术验收，不是完整产品/视觉认可。** 浏览器登录/导航/详情/展开态及UI→节点执行链仍待下一定向小片；生产模型、目标授权出口、原业务迁移和许可分发边界仍未完成。未commit/push/部署/真实目标/付费产品模型；本轮已收束，不保留新常驻预览。

## 0.52 H3-A已按阻塞收束：Go静态接线通过，真实UI构建缺少固定子包依赖

H2-B最终16文件/14原生事件/私有清理/CLI与终端exit0已由主核验，当前无其他执行会话。用户原有低并发、每轮清理再新会话与逐功能展开态对齐继续有效。

主只读核验固定root f4b1fb2的web/frontend package/lock/Vite插件组合与gitlink：cyber-ui子模块固定147423bd42b665aab361409c57258d9370f75bb0，公开固定codeload归档HEAD200（未据此声称源码/许可/构建已核验）；原上游web/static只有.gitkeep。原生前端React18/Radix/Tailwind/Vite6，不混入旧React19/Ant。Vite写../static及.gitkeep、插件组合扩展点已记录，原上游保持只读。

新包`var/cyber-h3a.67eXzJEB/`由一个gpt-6-astra做**跨层集成而非前端重设计**：固定源/许可/lock、项目内可复现准备/构建脚本、原生host.Static的可选资产参数与必要Go边界。页面/布局/主题/图表不改，后续功能视觉适配仍由DS。原生UI未完成构建/浏览器验收，不用假index或HTTP200冒充。

为降低峰值，执行会话阶段不跑npm ci/tsc/Vite大构建、不新浏览器/PG；只做源核验、代码/脚本及少量Go检查，清理临时运行资源交回退出。主审脚本并确认模型会话退出后，再独占跑一次真实Node安装/构建和资产HTTP验收。两段同属H3-A，主验证结束前不新开下一模型轮。保留给主消费的二进制/固定源/产物有路径哈希，不把它们误当未清理进程。原用户入口/在用旧预览不动。

已同步CLAUDE/README/frontend-design入口，明确旧业务契约和Ant实现是历史，不要求新底座重用旧Harness。新可见`wscyber-cyber-h3a-67eXzJEB`已接包：主核验QTerminal2809628/start6656719→shell2809636→CLI2809637/start6656741、显式gpt-6-astra/正确cwd，实际读取启动截图和accepted.json；已通过ownership-transfer.md与明确消息交付native/必要scripts/cyber-ui-*及本包唯一写权。可选参数定为`--ui-dir`，直接挂原生host.Static；允许用原生Route扩展对未匹配RPC/IOA与缺失资源作最小404兜底，其他页面仍委托原生SPAFileServer，须定向证明既有API/鉴权优先且默认无UI不变。不新造router/认证层。尚未完成真实UI构建/HTTP验收，未声称原生UI已接通。

执行阶段实现已落盘：`--ui-dir`接原生host.Static与Route扩展，固定源准备/构建脚本及真实资产HTTP检查脚本已提供。主已只读审阅Go改动、两份脚本、来源锁/许可说明和测试日志；3个定向顶层Go用例PASS（资产边界13子例、静态/原生API路由22条HTTP断言、默认无UI回归），未新加Go依赖。执行方最小Hub构建与prepare完成后正式交回停写。主核验23正式文件/5保留产物哈希、340基线仅4既有native变化、测试临时目录为空与重型锁空闲；完整/exit正常退出专用CLI2809637/shell2809636/终端2809628，CLI/终端exit0，ListAgents无其他执行者。随后由主启动已审`main-next.py`（后台任务bd5a4kmri），单独一次npm安装→tsc→Vite，成功才真实资产HTTP检查，任一步失败即停。首次构建已按失败停止：npm ci禁生命周期成功安装415包，tsc在512MiB进程堆上限处SIGABRT，Vite/真实HTTP未执行；不是收到系统内存回收通知。主保存日志并一次性诊断：无残留构建进程、重型锁空闲、可用内存约1.26GiB且无swap。只调整进程堆为768MiB并记录到构建收据；通过独立一次性`continue-compile.py`重新核对700个源码文件及工具版本、复用已安装依赖，定向续编一次（任务b61my7mze），不重装/跳过类型检查/反复加码/关闭保护。续编已失败停止：tsc正常给出exit2，9条TS2307+8条TS7006，共17诊断；固定root lock与实际node_modules均缺少`react-hotkeys-hook`、`react-arborist`、`@radix-ui/react-checkbox`、`react-resizable-panels`，这些确由固定cyber-ui子包声明/导入。主未再次加堆/跳过tsc/换源；Vite、publish、真实HTTP、浏览器均未执行。最终无本轮运行进程，重型锁空闲，无release/HTTP runtime；保留来源/已安装依赖/cache和核验过的Hub二进制供后续消费，非遗留进程。最终23文件哈希/两次失败/清理与未验项见本包main-acceptance.md、main-final-check.json；4份状态文档与构建脚本由主准确收束。**原生UI未接通，本轮按依赖阻塞收束，下一短片仅修固定组合依赖闭包与补充锁。**

执行方阶段报告：固定cyber-ui归档1,737,387字节、SHA256 `c5cede262e9d48089cc771cd591436b454dec2b5c74e7a91a4afcf6a5afab78a`，安全解包545项、未见LFS指针且cstx wasm存在；最终来源记录待主复核。该固定归档根/子目录未发现LICENSE/NOTICE，根与@cyber/ui包亦无license字段，许可记UNKNOWN，不推断整个上游无许可，也不继承Cyber根AGPL。继续仅限获准本机技术准备；可构建不等可部署/分发。

## 0.51 H2-B已收束：真实Web→原生节点→工具→Hub事件链通过，清理并正常退出

H2-A最终交回/文档窄补正/12文件哈希/专属清理均已核验，开发CLI2541962与终端2541917正常退出exit0后，ListAgents无执行者。主只读定位原生`host.Config.StartNode`、`pkg/node.RunWebSocketWithCapabilities`、Profile接口和AIScan的原生接线参考，不加载整发行版或重写机制。

新包`var/cyber-h2b.nRd1jnEH/`只做一条原生链：显式fixture-node模式下，真实原生Web/会话协议→一个本机node→StandardLoop固定本地工具→原生AOP进入Hub并回读，验证身份/结果/资源关闭。不是HTTP里直接跑H1再伪造事件，不自建Session REST/第二状态库；不同时接UI/报告/真实provider/目标或旧库。具体Profile/配置握手按上游真实接口确认，不能手动mark ready绕过协议。H2-A默认空Hub语义保留。

版本/许可、loopback/私有key/NopLogger/净环境及NoEgress非隔离等边界保持。只一个新gpt-6-astra可见执行者，少量定向用例与实际CLI串行，不全量测试/浏览器/PG；轮后清理专属服务/工具/端口/临时库/key/二进制，保留证据后交回、正常退出才新轮。新可见`wscyber-cyber-h2b-nRd1jnEH`已启动；主核验QTerminal2643292/start6276417→shell2643310→CLI2643311/start6276482、显式gpt-6-astra/正确cwd，并实际Read终端截图和accepted.json。通过ownership-transfer.md和明确消息已交唯一native/本包写权。接收后进一步只读核验采用上游registry+exts/session.NewProtocol的薄Profile适配；为真实Config.Distribute握手，仅fixture-node模式增加只读fixture ConfigStore及受限ReloadProvider hook，不手工mark ready，空Hub默认语义不变。未声称Web执行已接通。用户入口及在用预览不动。

### H2-B最终验证、主复核与退出

主已实际Readfixture-node实现/完整测试/首次失败/最终日志及真实CLI/flow/cleanup/NODE文档。原生注册与配置reload后，经生成Connect和Application AOP WS真实OpenSession/RunTurn，固定Bash输出`WSCYBER_H2B_NATIVE_TOOL`，一个call/result，14条Hub持久EventDelivery；WatchEvents与ListEvents身份/cursor/内容一致，原生CloseSession与事件落盘成立。4项直接影响用例通过；第一次20秒握手失败因Option.Timeout 0/上游默认3600图差异，修正保留日志，不手工ready；实际Terminal/工具时限仍5秒。

真实CLI PID2676906/start6350617在127.0.0.1:38767通过外部编译原生协议客户端；SIGTERM exit0，node/profile/runtime及REPL/工具均停，无active/子进程，端口重绑、SQLite integrity通过；REPL为原生func killed/canceled，不是OS PID杀停证明。主要求的验收客户端DNS输入边界已改字面loopback/无Zone/非零端口，8项纯地址断言通过，不重跑整链。16文件最终哈希及14事件行数已由主核对，准确服务身份、runtime/key/库目录、两二进制已确认不存在。

之后主确认专用CLI无子进程/输入空闲，完整/exit正常退出：CLI2643311、shell2643310、QTerminal2643292均消失，CLI/终端exit0，任务buj8d9zgx正常完成。清理先于新轮，原入口/用户预览保持。未commit/push/部署/真实目标/付费模型；未知许可及carapace appendix含义保留，93=原81版本不变+12新增。仍无原生UI/业务迁移/完整恢复和授权出口，本片仅受控固定链。

## 0.50 H2-A已收束：原生空Web宿主通过真实CLI验收，清理和正常退出已核验

用户已授权完成一轮清理退出后新会话继续。H1最终8文件与清理/exit0已核验，旧执行者不存在，ListAgents无其他执行者。主仅进一步只读检查上游exts/web、pkg/web原生Handler/ManagementRoutes/ServeHTTP、Service/Capability/Profile接口，核定H2-A为更窄的原生Web宿主小片，不一轮接节点+完整UI+旧业务迁移。

新包`var/cyber-h2a.1f8w3Id8/`：直接使用原生Web Extension/Auth/typed routes/SQLite/ServeHTTP，在native内新增本机受控入口；仅loopback、非空私有测试key、专属新SQLite，不读真实模型配置/旧业务库。验原生manifest/一个只读管理API、鉴权拒绝、关闭后端口和库资源释放；空Hub没有执行node时不称已能运行H1会话，不自写Session REST/事件状态库。不构建前端/初始化UI或templates子模块、不新浏览器/PG、不全量测试。新增可达依赖版本/许可按实记录。

启动前已保存H1收束记录。新可见`wscyber-cyber-h2a-1f8w3Id8`已启动：主核验QTerminal2541917/start6040673→shell2541961→CLI2541962/start6040756、显式gpt-6-astra/正确cwd，并实际Read终端截图和accepted.json；通过ownership-transfer.md及明确消息已交native/本包唯一写入权。执行方只读发现更直接的原生`pkg/web/host.Serve`，主同意复用其完整宿主装配而非重新拼相同机制；已要求核验nil/Nop logger不回退泄漏AccessKey并在小片内定向断言不回显，记录上游日志限制、不扩大审计。未提前称H2完成。仅一个gpt-6-astra可见执行者、重型串行；主独占共享文档，不并行改native。轮后清理本包临时服务/端口/库/二进制、保留证据再正常退出，用户入口/预览及未知旧资源保持。

### H2-A最终交回与正常退出

主已实际Read最终outcome/ownership-return、CLI验收JSON、cleanup/scope与HUB说明；12个正式文件更新后的交付哈希全部核对通过。HUB示例补var和锁父目录两行仅文档，不重跑构建。真实CLI PID2575446/start6118571在127.0.0.1:36501：manifest无/错key401、授权200，HTTP Connect JSON会话无key401/授权200，空agents/status200、根/scan404；SIGTERM exit0、key不入日志、端口可重绑、空SQLite integrity/reopen通过。生成Connect客户端是在Go定向测试中使用，CLI实际是HTTP Connect JSON，未混称。

主独立核验该服务身份不存在、专属工作区/key/SQLite与二进制已删除；H1源码与原许可JSON、旧范围324文件未变。Hub入口依赖81项为H1原61版本不变+20新增，许可UNKNOWN及组合义务保留，不宣称可部署。然后核验开发CLI无子进程/输入空闲、确认完整/exit并正常退出：CLI2541962、shell2541961、QTerminal2541917均消失，CLI/终端exit0，任务b22em4x4j正常完成。原入口/用户预览保持，未提前并行下一执行者，无commit/push/部署。

本片仍是空Hub，尚未连接执行node或UI，不代表H1已通过Web执行、执行历史恢复或完整授权出口。接续只在前轮清理/退出后另开短轮。

### H2-A阶段验证的只读复核记录

执行方报告依赖解析、唯一Hub build与四个定向顶层用例exit0；主已实际ReadHub main/test源码、tests.log，并对照上游host.Serve，确认没有自造原生宿主/Session REST。配置门禁为显式local-hub、loopback IP、0600非符号链接普通key文件、全新state-dir；实际测试含原生HTTP/生成Connect客户端鉴权、manifest/空session/无模型配置/空节点、根与扫描404、取消后端口和同进程SQLite句柄释放/原生空库重开、监听失败清理、空Hub unavailable。

主确认上游host.go确会把AccessKey交给Logger.Infof，本入口使用NopLogger；不把它改接控制台logger。测试已对stdout/stderr检查不含测试key；真实CLI启动/HTTP/SIGTERM/日志无key及清理正在执行，**尚未最终交权，不提前称H2-A完成**。主只读复核见本包main-review.md，不并行跑构建/测试/服务。空库重开不等执行恢复，空Hub不等已连接H1执行；节点/UI/生产模型/旧业务迁移仍不在本片。

## 0.49 H1已收束：原生CLI受控运行通过，清理及专用会话正常退出已核验

用户已明确“然后直接开始开发”，要求功能/执行细节可登录Cyber只读学习，深入理解原理并复用；低并发、合理小轮次，每轮完成清理后再开新会话。架构不再选型，也不再自建Harness。前端各功能/展开态对齐与astra后端、指定DS前端分工保持。

主已实际读取本地固定commit的`pkg/harness/{harness.go,base.go,harness_test.go,shared_test.go}`及`examples/session/main.go`：New/Load/Close、默认StandardLoop、原生OpenSession/Run/Wait/Observe及通过Extension注入无网络确定性Provider均有真实入口。Go1.26.4可用；上游根模块及AOP/两个fork的replace需要在依赖方明确处理，不继承臆定。UI/templates子模块本片不用，不全量初始化。

本片任务包`var/cyber-h1.6ns2j4wQ/`：默认隔离`native/`产品装配模块，直接固定Cyber/AOP依赖，不引入旧internal/research，不以绝对本机replace冒充可复现。交付真实原生Session/Turn→受控本地工具→输出/原生事件→清理的窄链；确定性Provider只作显式fixture，不是真实模型研究质量。先必要小包编译、少数定向用例，再实际CLI运行，不go test ./...、不启动新前端/PG/全量矩阵。共享旧根模块/业务库/前端不改。新可见`wscyber-cyber-h1-6ns2j4wQ`已启动，主核验QTerminal2453575/start5835211→shell2453613→CLI2453614/start5835284、显式gpt-6-astra/正确cwd，并实际Read终端截图与accepted.json确认接收。通过ownership-transfer.md及明确消息已交native/必要bootstrap唯一写入权；主不并行重型工作或改native，不提前称接入完成。

只有一个新gpt-6-astra可见执行者；主不并行重型检查或改native。轮次完成先交回并核验清理本轮专属临时资源，再正常退出，之后才新轮；用户入口、仍在用的预览和未知旧资源保持。此次启动授权不扩大真实目标/付费产品模型/部署/数据迁移/外部投递权限。

### H1最终交回与正常退出

最终outcome/ownership-return/cleanup-check均已由主读取，native下8个正式交付文件sha256全部核对通过。主再次核验本包无工具工作区、专属二进制已删除，开发CLI无子进程；保留日志/原生JSONL/构建信息及共享Go缓存，不清理用户预览或未知进程。核验空输入区并确认完整/exit后正常退出专用CLI2453614、shell2453613、QTerminal2453575，CLI/终端exit0，任务bclwi027w正常完成。无第二执行者提前并行。源码与说明边界见native/README及DEPENDENCIES；无commit/push或部署。

### H1定向运行验证通过的只读复核记录

执行方一次最小依赖续取exit0、唯一入口build -p1 exit0；三个定向用例成功/工具完成后取消/拒绝默认fixture均PASS。实际编译CLI的success与cancel-after-tool均exit0，分别产生13/11条原生AOP事件。主已实际读取main/fixture/test源码、测试日志、两份CLI摘要和success原生JSONL，并只读核对事件行数、两个新临时工作区确实不存在、baseline覆盖旧cmd/internal/web/根模块等无改动；未重复编译或跑测试。

已确认H1直接用原生StandardLoop/Session/Turn调用已有bash，实际输出`WSCYBER_H1_NATIVE_TOOL`，Provider第二次调用确实消费工具结果，原生session/turn/call关联成立；manager报告各一个completed/status0/output_bytes23且无active，Session/Observer/Harness已关闭。**取消只覆盖工具已完成后的请求取消，不是长进程运行中杀停/持久恢复/完整unknown保护。** 默认非fixture启动拒绝，NoEgress不等隔离，没有真实目标/付费产品模型/生产Web/UI接线或数据迁移。

主的范围内复核见本包main-review.md和main-evidence-check.json；暂未发现阻碍H1既定范围交回的问题。执行方正补native许可来源、README、清理和outcome，尚未最终交权。许可记录区分Cyber/AOP的AGPL文本、proxyclient的GPL文本与部分固定Go模块包内未发现许可声明的UNKNOWN；不把包内缺声明等同上游无许可证，不把本机技术通过当可分发/部署。收尾交回并核验清理后才正常退出专用会话、开新短轮。

### H1前期依赖记录：正式源码与固定公开模块已落盘，当时尚未完成构建/运行验收

主已只读确认`native/cmd/wscyber-native/{main.go,fixture.go}`与native/go.mod存在；Cyber及AOP均固定`v0.0.0-20261109041610-f4b1fb2f3315`，模块声明cyber通过公开版本replace指向实际仓库cyber-harness及其aop，不依赖绝对本机路径。执行方报告两主模块download成功并有go.sum；依赖方明确列出上游两个fork replace，是否实际可达仍以入口构建为准。

执行方报告首次仅入口`go list -deps -p1`在240s时限exit124，54个可达模块有下载进展、未见版本/解析错误且进程已结束；不是已证实的代码/运行失败。主仅允许复用缓存再作一次有界最小续取，完成后才串行最小build/定向test/实际CLI；再次超时或停滞则保留缺失清单交回，不循环重试、不降版本或关校验。原生proc生命周期无活跃进程断言仍在补，不提前声称H1验收完成。

## 0.48 历史架构决定：全部基于cyber-harness，原有需求在其底座上扩展实现

用户明确要求：**全部基于cyber-harness的Harness架构实现之前的功能设计，不再使用自己新建的Harness；直接复用现成能力，在其基础上实现额外需求。** 这是已决定的技术路线，覆盖此前“候选首选/是否采用/仅worker接入旧引擎”等待选表述。Cyber是主体底座，不是旧架构上的可选插件；不继续维护旧Decision循环去逐步指挥Cyber，不另建同职责Harness。

原有产品需求、授权/证据/候选/验证/unknown等规则保持；旧表结构、Ant组合、路由和代码工作量不构成必须保留的设计限制。下一步直接做“既有需求→原生能力→实际扩展点→新增业务功能→最小验证片”的落地设计，不重新选型，也不在旧内核继续加新模块。每个功能及展开态的真实效果要求、单执行/重型串行/定向测试继续有效。

本次将决定写入`docs/cyber-harness-foundation.md`、AGENTS、architecture/vision顶部及长期记忆，**只固化决定，没有直接迁移产品代码/数据、初始化上游子模块、构建/运行上游或部署**。上游版本/许可/原生UI可构建性/扩展接口与业务安全缺口仍须按真实模块核验；这些核验不再影响“使用Cyber而非自建”的既定方向。现有前端及预览保持，不宣称旧系统已经换底座。

## 0.47 历史收束：用户暂保留当前UI，后续逐功能/展开态对齐；下一步先架构评审

用户表示“先这样吧”，要求后续开发细化到每个功能，各种展开后的效果都要对齐，并询问下一步。**这是当前UI阶段暂收束，不是完整视觉验收/用户认可全部功能。** 当前不继续布局微调、不开新执行会话、不补跑全量检查；保留源码与预览，不自动迁移/部署。

已将逐功能操作路径及展开/收起/详情/弹窗/页签/原文/必要异常状态的真实效果对照写入AGENTS与frontend-redesign-brief，继续小片串行而非全量矩阵。

按用户此前提出的cyber-harness方向，下一建议是先冻结执行与原生交互底座的复用方案，再做小型受控接入验证，不继续扩写旧Decision循环上的报告/图谱/对话。主本次仅读取既有`var/cyber-harness-assessment.xGAkA5Qw/assessment.md`并只读核验本地上游：仍为`f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf`且工作区无变更，tools/templates和web/frontend/cyber-ui子模块仍未初始化。已有结论仍仅静态评估，不能称原生UI已完整核验或接入成功；许可/子模块/UI宿主与状态权威边界需先明确。此次未下载/构建/运行上游，未启动迁移或新实现。

## 0.46 历史定向交付：1440项目列表修复已收束，DS正常退出，单一可见预览恢复

用户明确“继续，注意不要太高并发，也不要一次性全量测试”。本次不恢复整套回归矩阵，不同时开模型/测试浏览器/展示浏览器/Go/PG。只有一个执行会话上限，重型严格串行，按实际失败和影响范围分小片；该要求已写入会话规范。

主确认旧DS及展示浏览器不存在、无其他可达执行者。一次压力后资源快照可用约1.1GiB，无swap，不持续轮询。静态8793/projects本轮200，原serve PID2179265/start5145962仍在；裸/api仍非业务后端。旧stage-4仅有login后`.ws-table-frame`20s超时，没有成功回归证据。

**主先在不启动DS时做了一个单页检查**：新包`var/ds-ui-focused.tpMRHoHn/probe-list.mjs`，1440×900、严格本地只读API mock，登录→列表通过，5项目/document与API200、无pageerror/外部请求，临时浏览器已关闭；旧超时未复现，不宣称根因已修。主实际Read probe-list.png发现真实视觉缺陷：长内容挤压创建时间列至逐字符换行，普通任务行约320px，授权范围也不必要断行；body宽仍1440，证明“无整页溢出”不足以验布局。

因此新DS小片仅修ProjectListView/必要CSS的列分配与长内容，再对**同一1440页面**定向验证列宽、普通行高与焦点。不跑177全量、全浏览器矩阵、新PG/Go；不删长fixture掩盖缺陷、不改API。检查后关闭本轮临时浏览器，交回并退出DS后主再恢复唯一可见预览，降低同时驻留量。新可见`wscyber-ds-focused-tpMRHoHn`已启动；主核验QTerminal2319392/start5474283→shell2319440→CLI2319441/start5474333、显式模型/正确cwd/可见窗口，并实际Read终端截图确认真实读文件，主已实际Read本包accepted.json，通过ownership-transfer.md及明确消息交唯一项目列表小片写入权；仅ProjectListView/相关CSS和定向验证，主不并行改前端或跑重型检查。旧布局与中间成果保留，不回滚、不重做全轮。剩余窄屏/深浅/实际最大化/证据/真实API列为后续串行小片，不能说全通过。

### 0.46定向交回与一项验收补正

执行方第一次交回outcome/ownership-return；主确认源码相对本包baseline只变ProjectListView.tsx和styles.css，并实际读取修后截图与verify-list-fix.json。固定表格布局后日期列180px单行、授权240px、普通行74px、长标题行95px、5条fixture保留；类型/构建exit0为执行方报告，单页几何JSON通过，未跑177/全矩阵/Go·PG。**主看图又发现短标题居中而摘要/长标题左对齐**，尚未最终接收；已通过ownership-amendment及明确消息交同会话仅补标题text-align:left与同页有效文字对齐断言，不重复tsc或全量测试。首次截图/结果保留review-first-fix/。

主仅准备了本包display.mjs（单headed、真实最大化、明确fixture标题、严格同origin只读mock、不再自动导航）；**尚未运行**，计划在此DS完成交回并正常退出后再恢复唯一可见窗口，降低同时驻留量。旧静态服务继续复用，不新增Go/PG。

### 0.46已接收：列宽与标题对齐修复，DS正常退出后恢复单个可见预览

主实际Read最终fix-check截图/JSON与有效文字左边缘断言：日期180px单行、授权240px、普通行74px、长标题行95px、5条含长数据保留；短标题与摘要首字left差1px，已消除居中回归。源码只变ProjectListView.tsx/styles.css。本小片接收，不代表完整UI/用户认可。末次脚本曾因web cwd相对路径错误exit1，改绝对路径后通过；工具完成后DS最终回复连接中断，旧outcome未补写，主以实际最终源码/截图/JSON补记main-acceptance.md，不重试模型。未跑177全量/完整矩阵/Go·PG。

主核验专用CLI无子进程且输入空闲，确认完整/exit后正常退出；CLI2319441、shell2319440与QTerminal2319392均消失，CLI/终端exit0，任务br0xwkkyv正常完成，原入口保持。**先退出DS，再恢复一个主负责可见预览**：display.mjs/任务b0ejq9vdv，driver2370863/start5640110→headed Chromium2370913/start5640142，复用静态2179265，不新增Go/PG。真实最大化inner2554×1234/body2554，标题“本机合成数据 · 只读UI布局预览”；主已Read项目列表截图并核验可见窗口。显示后不再自动导航用户页。

仍仅同origin只读API mock，裸8793不可用，不是真实后端联调；关闭该窗口/driver则mock结束。静态服务读web/dist-react而非固定快照，后续构建须保护用户正在查看的窗口/操作。尚未验390/768/深浅/其他模块/本片证据链/真实API；最大化仅查看项目列表，不能说全回归通过。下一步保持串行、小片验证，不重复全量套餐。

## 0.45 历史中断：DS接续再次被低内存保护停止，预览窗口也已退出

系统通知：后台任务`bm8bawb35`（打开DS可见接续会话）在主会话idle时因系统临界低内存停止。这不证明命令/模型或其自身内存使用故障；**不得自行再次启动，需用户明确要求后再恢复，不关闭保护、不用脱离监督绕过回收。** 本次主只核对存活与磁盘成果，未启动模型/浏览器/后端，未继续改前端。

精确核对：DS QTerminal2153673/start5088153与CLI2153744/start5088263均不存在；阶段浏览器driver2257993/start5324859、Chromium2258056/start5324933也不存在，无该浏览器可见窗口。静态serve.py2179265/start5145962仍存活，但/api本就404，失去带mock的浏览器后**8793不能作为可操作预览**。不是主主动关闭用户预览；不继续沿用§0.44“窗口可见”的历史时点。

磁盘修改与阶段2/3截图、视觉复核记录均保留。相比本接续包baseline另有8个文件变化（包含新增的core测试、ActionEvidenceDrawer、ObservationReader、ResearchControlsPanel及既有HTTP/ProjectWorkspace/styles/shell），未回滚。preview包已有regression.mjs，但尚未收到最终回归/真实API联调交回，不把脚本存在算通过。中断清单在`var/ds-ui-resume.YCvRQjQn/memory-stop-2.json`。真实后端仍未启动；旧60859现场和其他旧窗口/服务均未主动处理。原用户入口继续承接反馈。

用户再要求恢复时，先读取当前源码/最新回归产物，核对会话与资源，保留阶段成果；仍用指定wangsu-ds-pro-latest新可见会话。建议先由用户关闭明确不用的旧预览以减少再次中断，不能自动清理未知或仍在用的资源。

## 0.44 历史接续：用户明确继续，保留DS九文件中间成果推进

用户在§0.43低内存停止通知后明确“继续”。主已核对ListAgents无其他执行者，旧DS专用CLI/终端不存在；本轮仅一次资源诊断可用约1.2GiB/总约6.6GiB、无swap，不按固定阈值停工、不持续轮询、不关闭系统保护。仅开一个指定`wangsu-ds-pro-latest`新可见会话，沿用原base URL/API key、不改全局/产品配置。

- 旧包9个前端文件保留，未回滚。主已实际运行`npm --prefix web run typecheck:react`，exit0，日志`var/ds-ui-resume.YCvRQjQn/typecheck-entry.log`；本轮单元/共享/构建/浏览器尚未核验，不把旧Kimi通过算DS结果。
- 旧Cyber观察JSON的安全字段确认/tasks应用导航已渲染、无password输入、1914×1000、aside248px、body13px、无pageerror，阻止1个请求；只访问任务页，**未实测任务详情/HTTP/报告交互**。旧design-plan的“实测”混合主六图转述，接续须区分证据。主审阅旧脚本发现Cookie/路由允许子域且在线截图未遮蔽，新包明确禁止直接复跑，要求精确origin/host-only Cookie、阻止WebSocket/副作用请求、不再保存原始在线截图或业务正文；现有两图已收紧0600、未公开或转发。
- **旧60859当前离线**：health连接失败HTTP000，API1889421不存在，监督器1888880/start4404701仍在。主已读其3行api.log：12:08:04连通测试持久执行停止，领取回执/退出收尾超时；不据此归因于内存保护或前端代码。未重启/清理旧现场；新产物检查后用一套本轮隔离预览展示，不以新端口掩盖旧故障。
- 新包`var/ds-ui-resume.YCvRQjQn/`已写接续任务与当前源码快照。新可见`wscyber-ds-resume-YCvRQjQn`已启动：主核验QTerminal2153673/start5088153→shell2153743→CLI2153744/start5088263、cwd/显式wangsu-ds-pro-latest/独占窗口，实际Read截图确认真实文件读取；主已实际读取本包accepted.json，执行方明确接收当前九文件成果/六图文字依据/安全观察修正；ownership-transfer.md加明确消息已交唯一前端写入权，主停前端共享写入并负责后续实际截图视觉复核。不把接收/交权当开发完成。重申布局排版对齐Cyber、DS图像不可用由主视觉复核、串行检查与及时展示；本轮仅前端，不启动cyber-harness迁移或旧架构扩展。

### 0.44阶段1：四图已由主实际复核，视觉尚未达到参考，继续同轮修正

执行方提供`var/ds-ui-preview.YCvRQjQn/browser/shots/`的research、research-evidence、traffic-evidence、projects四图（1920×1080）。主均已实际Read：项目列表宽表与248px全局侧栏有改善，但HTTP缺一致的任务内窄对象导航；研究表从y约404才开始、顶部控制/风险/折叠区层叠；HTTP行高约64px、报文工具/字段堆叠使正文落到屏底；动作证据前有大“暂无观察记录”插画。主已通过明确消息要求结构性修正，保留风险/授权/原件行为，不以删信息凑整洁；完整结论见接续包visual-review-stage1.md。未作视觉通过或用户认可声明。

**验证范围更正：** 执行方报告类型/177单测/共享/构建exit0，主未重跑这组；主已读取browser-ready.json与驱动。8793仅服务web/dist-react静态文件，/api与/healthz返回404；browser-stage是headless+Playwright API mock且finally关闭浏览器，**裸URL不是可直接使用的业务入口，也没有该脚本保留的可见交互窗口**。四图是正式前端在合成数据下的阶段截图，不是实后端联调。external=[]只记了/api非GET/HEAD，未证明全部外部请求为零；已要求改为全请求白名单/观测并纠正交回措辞。后续基本修正后应提供唯一headed fixture或真实API受控预览，明确mock与真实契约验证边界，不并发添多套常驻资源。已要求修正fixture中含路径的无效origin。

### 0.44阶段2：二次四图已看，结构改善，定点收敛后立即展示

主已实际Read执行方12:47更新的四图，并复制至接续包visual-stage2-shots/含哈希留证：184px任务内对象导航已统一，HTTP行高收紧，大观察空态消失，响应正文进入首屏。**仍未作最终视觉通过/用户认可声明。** 请求侧仍有大灰色空白、HTTP关键状态码被列裁切、研究控制区占高；左上具体方向与HTTP全部方向及任务头状态的作用域需明确。主已给出定点修正，不要求重开布局方案；修后必要检查即开唯一headed fixture窗口供用户操作，不再仅交无头截图，不因展示延后真实契约/关键回归边界。详细见visual-review-stage2.md。

执行方已纠正8793及无外部请求的旧措辞：仍只是headless API mock，不是普通浏览器可用入口，也无保留可见窗口；其新全请求origin/非GET观测为执行方报告，主尚未核对新驱动。后续截图要求独立stage目录，不覆盖旧图。源码仍由同一个DS会话独占，主负责视觉和状态。

### 0.44阶段3：四图定点改善，可见fixture窗口已由主独立核验

主已实际Read`var/ds-ui-preview.YCvRQjQn/stage-3/shots/`四图：请求灰填充移除、HTTP状态码可读、对象导航和方向作用域收敛、更多控制减少常驻按钮。允许进入阶段展示及专项回归，不再为同一1920图重开设计，**不等于最终UI通过或用户认可**。详细见接续包visual-review-stage3.md。

主已核验静态服务2179265/start5145962，driver2257993/start5324859→headed Chromium2258056/start5324933、唯一窗口188743684，并实际Read桌面截图确认项目列表可见。它只在该Playwright context下拦截/api，裸8793的/api、/healthz仍404；不是普通新窗口可用入口/真实后端联调。已要求fixture标题明确、关闭本轮翻译弹框、不再自动导航展示窗口。实测屏幕2554×1356，当前浏览器外窗1928×1165；1920图非实际最大化，已要求独立上下文补真实尺寸。保留窗口及driver依赖，不因交回关闭。

DS继续前端专项回归；主astra随后安排真实Go/API验证，当前仅审阅旧受控运行器、未启动新后端，不与前端重型浏览器并发添多套资源。模型/Go/授权边界不变。

## 0.43 历史中断：DS前端会话被系统低内存保护停止，等待用户要求恢复

系统通知：后台启动任务`bppq03oug`在主会话idle时因系统临界低内存被停止；这不证明启动命令、DS模型或其自身内存使用存在故障。**不得自行重新启动，不关闭内存保护，需用户明确要求后再恢复。** 主入口只做只读存活/文件状态核对，未重启、未接着改前端、未清理旧预览或未知资源。

本轮精确启动器2069450、QTerminal2069461/start4816136、shell2069523及CLI2069524/start4816281均已不存在。相对本包baseline，9个前端文件已有磁盘改动：`react/App.tsx`、`components/{ExecutionActivity,HTTPRecordsWorkspace,ProjectListView,ProjectWorkspaceView}.tsx`、`react/styles.css`及`ui/{AppShell.tsx,check-ui.mjs,shell.css}`；路径均在`web/src/`。保留这些中间成果，不回滚或称已完成。包内已有design-plan.md、observe-cyber.mjs，但主尚未核验浏览器观察/测试/构建结果，未收到最终outcome/ownership-return，**不能把脚本存在说成站点访问成功或UI验收通过**。

中断核验在`var/ds-ui-redesign.2YLodZu4/memory-stop-check.json`；完整任务、逐图布局依据、初始源码快照及私有连接仍保留。原用户入口继续承接反馈。用户要求恢复后，先检查当前磁盘成果与真实断点，再用指定模型新开可见会话，不盲目重复启动原launch或从基线覆盖中途改动。

## 0.42 历史接收：前端换为wangsu-ds-pro-latest，新一轮重新设计

2026-11-13用户明确上一轮UI仍有很多问题，要求Kimi换为`wangsu-ds-pro-latest`，base URL/API key保持不变，这一轮重新来过。覆盖此前Kimi/Gemini模型规则及§0.41仅收尾的限制。用户随后明确要求用好插件/浏览器工具检查实际效果，**连布局排版也要做出与参考图一样的效果，不是在旧布局上抄外观**。完整设计要求和Cyber六图是直接目标；已有代码仅作功能/行为依据，不作为已认可布局，不盲目回滚。主已实际读取六图，核验本机Chromium与既有Playwright可用路径（未提供chromium-cli/浏览器MCP，不冒称插件存在）。仅前端换型，架构/后端/复杂集成仍归astra。本UI轮次后再评审cyber-harness方向，不自动新增旧架构功能。

- 旧Kimi收尾已在`var/kimi-ui-finish.CN4yC0Xo/outcome.md`和ownership-return.md交回：仅加一行grid列约束修390横溢；记录177/177与局部headless通过，未覆盖暗色/候选验证/设置等、未展示新构建。这是执行方历史材料，不是本轮主入口重跑或用户认可。ListAgents无其他执行者，旧CLI1925363与QTerminal1925341均不存在。
- 新包`var/ds-ui-redesign.2YLodZu4/`。已从上一轮私有连接生成会话级配置，只改模型字段并核对base URL/API key完全相同，不回显秘密，不改全局/产品配置。指定模型最小请求HTTP200/READY，网关返回model=`deepseek-v4-pro-0813`；请求仍为`wangsu-ds-pro-latest`，无回退。连接通过不等于实际CLI工具、图片或开发能力通过。
- 已保存当前前端源码快照与354项文件哈希；更新AGENTS/会话规范/完整任务入口。新可见会话`wscyber-ds-ui-2YLodZu4`：主已核验QTerminal2069461/start4816136→shell2069523→CLI2069524/start4816281、正确cwd/显式模型/可见窗口，并实际Read终端截图确认文件读取与工具活动，已读取accepted.json。
- **指定DS接入不能处理图像：** 执行方对六张有效PNG的Read均返回Unsupported Image，文本/工具正常；不换模型、不声称DS已看图。主已逐张实际Read六图，将布局/区域比例/层级/密度写入本包visual-reference.md；由DS进行DOM/交互学习与前端实现，主负责新产物实际截图视觉复核和复杂契约。已通过ownership-transfer.md和明确消息交唯一前端写入权，主不并行改web；共享规范/STATUS仍归主。实现尚未交回，不把接收算完成。
- 用户新提供Cyber登录态，明确截图不足时可只读登录学习具体交互。凭据仅本包私有文件0600/目录0700且gitignored；无凭据入文档/消息。已交DS按限定只读浏览、拦截写请求及效果性操作，不复制业务数据；**本轮新登录有效性尚待执行方实测**，不能把历史200当本轮已登录。
- 本轮只读`http://127.0.0.1:60859/healthz`为200；该旧固定构建不含最后窄屏修复，旧用户窗口是否存活未确认。全部旧预览/PG/现场保留，不强刷或清理。无前端产品代码改动、commit/push、部署或真实目标/产品模型评测。

以下§0.41及更早内容为历史时点，模型选择、当前任务与归属以上为准。

## 0.41 历史接续：旧Kimi被内存压力回收；磁盘已有更新成果，短轮仅收尾

用户在系统低内存停止后台启动任务后明确“继续”。本轮ListAgents无其他会话，/proc核对旧CLI1046755和QTerminal1046482均不存在，仅原入口继续；不沿用旧窗口号操作，也不关闭内存压力保护。一次资源快照MemAvailable约1.8GB/总约6.6GB、无swap，不设固定2GB停工门槛，不持续轮询内存。

**08:26故障快照已不是当前源码。** 磁盘上已有16个web文件变化（含core.test.ts、styles.css和新活动测试），保留这些后续改动，不从`kimi-interruption.r9dobvjp`回滚。旧包`var/kimi-ui-return.etj1Q5g7/`的react-final.log为**177/177、fail0**，shared-final.log通过，build-final.log正式类型/构建通过；旧Descriptions span与Vite大chunk告警保留。这是已读取的执行方日志，不称主入口重跑。

新固定工程预览 **http://127.0.0.1:60859/projects**，目录`var/kimi-ui-preview.pfmyzh2e/`，本轮health200。主已实际Read该目录browser.stage的研究与流量截图：方向/统一执行活动、动作内联原件、HTTP上表下请求响应。browser-ready明确stage=true，2554×1261；不代表完整UI验收/用户认可，也不证明用户浏览器仍存活。首轮browser-checks/failed.json在1440-light定位“查看证据”超时，browser-checks-final目录空，**无最终浏览器通过证据/最终交回**。

已核对当前该API1889421/start4404919与监督器1888880/start4404701仍在，监督器父为PID1；旧preview监督器1819028仍保留，无本包浏览器stage/checks Node进程。没有清理任何旧服务/PG/窗口。新短包`var/kimi-ui-finish.CN4yC0Xo/`仅接续必要浏览器验证、局部修正、展示和交回；保持Kimi仅前端，高难度架构/后端由astra。当前UI轮次完成后停止，先评审cyber-harness架构方向，不自动新增旧架构功能。新可见`wscyber-kimi-finish-CN4yC0Xo`已显式wangsu-kimi-latest启动；主核对CLI1925363/start4490759→shell1925362→QTerminal1925341/start4490714、cwd和独占可见窗口，实际Read截图确认真实回复与文件读取。接收/独占写入按本包accepted、ownership-transfer及明确消息，不把启动算作收尾完成。

## 0.40 最新：用户切回原来的Kimi，Gemini已正常退出，兼容转发取消

2026-11-13用户最新要求“算了，还是切回原来的kimi吧”，前端恢复`wangsu-kimi-latest`，每轮新可见会话，后续模型调整等用户通知。设计原提示、六张Cyber截图目标、旧规范可突破及业务保护不变。

- Gemini Flash最小文本成功但实际CLI两轮空返回，无assistant记录/工具调用/accepted；355项源码/文档与交权基线完全一致，未实施前端。有界合成工具SSE探测确有tool_use但重复message_start，尚未证明是CLI空返回唯一根因。
- 用户曾选择会话级兼容处理，随即撤销并切回Kimi。转发服务未启动，`var/gemini-sse-session.sTypCzpu/capture-probe.py`仅写好未执行，不继续诊断或调用Gemini。
- 主按精确PID/starttime/空输入区正常`/exit`退出专用CLI972350/shell972349/QTerminal972288；CLI/终端exit0且均消失，未kill父链或动用户入口/预览。首次按窗口键入未显示，二次活跃窗口键入经截图确认完整/exit后才提交。
- 原Kimi网关/别名本轮最小请求再次200/READY，返回model=kimi-k3.1-pro。新包`var/kimi-ui-return.etj1Q5g7/`保存新会话任务、连接和退出核验；新可见`wscyber-kimi-return-etj1Q5g7`已显式wangsu-kimi-latest启动。主核对CLI1046755/start2449255→shell1046754→QTerminal1046482/start2448570及单独窗口，实际Read截图确认真实回复并调用工具读取项目文件/Cyber PNG。新会话已写accepted并明确回报：完整原提示/项目文档和6张Cyber图已读，交权前仅本包写入、未改产品。主已实际Read接收文件；独占写入按ownership-transfer及随后明确消息生效。不是复用旧会话，不把启动/读图当实现完成。
- 原用户入口、旧所有窗口/PG/固定产物/失败现场保持；旧API健康只沿§0.38时点，不声称全部在线。无产品改动、commit/push或部署。

## 0.39 最新：用户指定Gemini Flash，最小连接通过，新可见前端会话已启动

2026-11-13用户明确改用`gemini-3.8-flash-high`，“后面要调整模型再跟你说”。保持这一前端模型，后续每轮新开可见会话，不自行切回Argon/Kimi/GPT。原完整设计提示、六张Cyber截图目标及业务保护保持。

同一用户指定网关的最小`/v1/messages`请求已返回200和READY，响应模型标识为`gemini-3.8-flash-n`；请求仍为用户指定的`gemini-3.8-flash-high`，未试其他型号。本次未向该探测发送项目内容；连接成功不等于前端实现或工具调用已完成。新包`var/gemini-flash-ui.s70sWU5c/`，新可见`wscyber-gemini-ui-s70sWU5c`已启动。主核对CLI972350/start2259386→shell972349→QTerminal972288/start2259272、cwd/显式模型与单独可见窗口，并实际Read初始终端截图；截图时初始请求处理中，尚不声称已完成材料读取或设计。唯一写入按该包ownership-transfer与明确消息生效，实际接收以accepted/回信为准。原入口继续承接反馈，不与执行者并行写前端。

此前Kimi已经交回并正常退出，仅看图/盘点、未改产品；Argon失败记录保留，不把Flash成功写成Argon路由已修。旧预览在线状态仍按§0.38最近时点，不重启或清理失败现场。

## 0.38 最新：用户改用Gemini；Kimi已正常退出，指定网关生成路由阻塞

2026-11-13用户明确“不要用kimi了，换成gemini”，指定`gemini-4-argon-high`，后续前端仍每轮新开可见会话；覆盖§0.37的Kimi选择。完整设计提示与Cyber六张截图目标不变，不默认GPT、不静默换型号。当前主入口已接回唯一写入权，前端实现暂停。

- Kimi已交回`var/kimi-ui-redesign.OVhO6Kkf/ownership-return.md`及outcome.md：只完成真实功能盘点及6/6 Cyber截图阅读，未进入产品实现，无测试/构建/新运行。主入口独立对比355项源码/文档全部与交权基线一致。核对精确PID/starttime/唯一窗口和空输入区后正常`/exit`，CLI/shell/QTerminal均消失，CLI和终端退出码0；未kill父链或关闭用户入口。
- 指定新网关的最小生成探测：`/v1/messages` HTTP400/model_not_found；只读`/v1/models` HTTP200且**明确列有gemini-4-argon-high**；同一模型`/v1/chat/completions`仍HTTP400，`unknown provider for model gemini-4-argon-high`。因此不是简单按模型列表即可认为可用，需确认网关供应商路由。未试其他模型、未无限重试、未发送项目代码；**新Gemini开发会话尚未启动**，不宣称已用Gemini设计。凭据只在本次私有文件，不改全局或产品模型配置。
- 接续包`var/gemini-ui-redesign.rozqF32s/`保存连接结果、Kimi正常退出核验及交接；解除网关阻塞后再新开指定Gemini可见会话接权。
- 退出核验时39129/37307健康200，**48983与36417连接失败**。只读日志均记录原时间03:28:16运行失败：48983连通测试领取回执超时；36417领取/收尾状态或单实例锁冲突。48983监督器记录phase=failed/api_exit=1；未归因为退出Kimi，底层原因未查明，不重启、不清理、不用新端口掩盖。窗口/PG/产物现场保留，不再把48983/36417称当前在线。

以下§0.37及更早描述保留历史时点，不代表当前模型归属/在线状态：

## 0.37 最新：UI阶段调整封存；用户仍不满意，改由指定Kimi重设计

2026-11-13。用户明确要求当前步骤暂停，新开可见终端使用`wangsu-kimi-latest`，按其完整产品设计/前端提示重新设计现有前端，**不被旧UI-1、组件库或本轮主从布局约束**。当前前端仍未获认可；核心是agent执行输出、日志、结果与HTTP细节，不是视觉炫技。详见[新的前端设计任务](frontend-redesign-brief.md)。

本轮15个web文件实现工作台/候选/验证信息组织调整，Go/迁移/依赖未改。最终类型、React173/173、共享类型/SSR和构建通过；两次类型失败已修正且日志保留，Descriptions/Vite告警未消除。真实可见阶段链已在独立39129及最终48983通过任务→候选→证据→独立验证→新结果；两个窗口停止自动导航并保留。**完整六尺寸/异常矩阵尚未运行，browser-checks.mjs只是脚本，不是验收证据。** 工程预览<http://127.0.0.1:48983/projects>，时点health200，不代表布局获认可或长期稳定性已解决。

本轮封存`var/ui-rebuild-next.ejUIbC/outcome.md`及stopped-source/runtime.json。新设计包`var/kimi-ui-redesign.OVhO6Kkf/`；新可见`wscyber-kimi-ui-OVhO6Kkf`已以显式`wangsu-kimi-latest`启动，主独立核对CLI844788→shell844787→QTerminal844678及唯一窗口，并实际Read终端截图确认真实回复/读包。最小开发连接请求200，网关返回model=`kimi-k3.1-pro`；不是产品模型评测，也未改全局或产品模型配置。唯一写入按该包ownership-transfer.md及明确消息交权；实际接收以accepted/消息为准，不把启动当设计完成。原用户入口、39129/48983/37307/36417及所有旧现场不关闭、不强刷。无commit/push、部署、真实目标或产品付费模型调用。

以下为此前历史状态（其中“最新”“当前”沿当时语境）：

**2026-11-13最新：D2仅以技术实现/受控验证材料封存，当前布局已被用户否定，暂停追加开发并交回，见§0.36。下一优先项为学习优秀项目重构工作台/候选/验证展示。封存时37307与保留的36417健康；本轮59423等三个旧API已退出，窗口/PG/现场均保留。以下旧段落保留历史语境，不代表所有旧URL持续在线或用户认可。**

更新：本轮接续核对；历史记录含2026-11-13 UTC标注，原证据时间不改写。分支 `feat/foundation`；仓库 `/root/桌面/wscyber`。**尚无 Git 提交，未推送。A/B、C1/S1、C2-A/C2-B及C3-A已完成此前受控本机验证/正式React展示；D组候选首片已完成本轮受控本机验收、交回及主入口整合核对，见§0.34。旧五个预览端口在接续检查时均连接失败；本轮35283曾通过基本可见展示及主入口只读核对，**随后API异常退出、35283当前不可用**，旧现场保留、其直接根因未证实。已确定性修复请求取消伤及持锁连接的一般缺陷，当前最终入口 **http://127.0.0.1:36417/projects** 已通过本轮候选子片验收及主入口核对，具体边界见§0.34；41893仅为中间版本。** 完整C及D–F未完成；新增方向执行记录与任务内对话仅登记待设计，不自动开工。历史检查点/会话数量不是持续存活证明；不清理旧窗口、资源或父链，不恢复旧撤销任务，不把本机展示写成用户认可。

## 0.36 D2技术材料封存；当前布局被用户否定，停止追加开发

- **最新用户反馈与停止点：当前布局太简陋，逻辑/实用/可读/美观不达成熟系统要求，已被明确否定。** 工程证据≠视觉认可，不将本轮称为UI验收或完整D交付。用户允许学习Cyber/ARTEX等优秀实际设计并改进原组件库/UI-1，不把旧约束当不可突破布局；本轮不改全局视觉规范、不自行重设计，按原入口明确指示仅封存交回。后续优先重构任务工作台、候选与验证展示，报告/修复后复测、TI-1/TI-2仍未实施。
- 本轮新可见`gpt-6-astra`主按双重交权独立完成参考/契约/共享先行与Go/隔离PG/正式React工程闭环，无Agent/Workflow/其他执行CLI。固定候选修订、独立验证身份/有限Thread、原模型/授权/分享/账本、新证据引用及结果回流已接通；只追加013，旧迁移/依赖未改。详见[finding-verification-d2](finding-verification-d2.md)。结果是模型有据判读，候选仍candidate，不自动confirmed/判修复。
- 原入口代码核对指出并已修正两处：HTTP状态码不当业务证据判据（500/302可作为材料，200不证明漏洞），限制/截断失败材料可以保留；预算耗尽且无在途/未决动作写不可变程序结束回执、result保持null，旧记录/预算不变，允许用户另行显式新验证。启动事务锁原方向并检查未决事实，pending/approved/running/未核对unknown不能换键逃逸。原读取取消持锁保护和证据关闭passive焦点归还未回退。
- 实际测试：**43顶层PG race**（8个D2及旧D1/取消锁/C2-B/C3-A/账本/迁移恢复）、五包Go race、共享类型/SSR及正式构建通过。最终React **167/167**，明确新增测试最初未加入core入口、当时161不算新专项；补入口后单独类型/167项实际通过。首轮旧版本测试因013依赖导致模拟回退失败，补隔离schema空D2结构回退助手及真实v12→13升级后通过，未改旧迁移。Descriptions span与Vite大chunk警告保留。
- 真实运行独立API验收15任务：三路径两协议×原文开关与分享、支持/反证/身份不足、500受控错误/302重定向、保留截断限制、503失败、真实目标超时unknown不重放、在途暂停/原候选更新/迟到结果固定来源。正式浏览器完成预算1→真实动作→无结论终止→显式新验证、双击单POST/丢响应后GET回执核对、复制/焦点、六尺寸主题长文、错归属/关闭迟到/401；补验390浅暗操作/表单、26条真实预算结束记录25+1分页、503旧列表保留及实际切项目迟到。截图已实际查看，但**不替代用户已作出的布局否定**。
- **封存时当前工程预览：http://127.0.0.1:37307/projects**，`var/verification-preview.4zv5c3z4/`，可见`browser.64ydesch`，实际1914×792；health200、实际exe/index与固定产物一致，窗口停止自动导航。20任务、20候选/21修订、45验证、16判读结果/27程序预算结束、1未核对unknown；无在途Attempt/call/action、原文关闭零归档。账本141调用（62研究/77验证/2连通测试）；fixture解析记录139个研究/验证请求、138个决策与1个503、80目标请求；另2个连通测试transport_unknown不混称成功研究调用，来源未进一步核对。
- **新增稳定性未解决项：57567/41045/59423三个阶段API封存时均已退出1，不再在线。** 最早可见日志原时间分别2026/11/13 00:38:19、00:38:20、00:38:22：连通测试领取回执超时；57567/59423收尾还报状态或单实例锁冲突。监督器仍在且phase=failed，PG均running/OOMKilled=false，退出诊断PG日志未见对应崩溃事件；更底层原因未证实，不归因为已修复的读取取消，也不以37307成功证明旧故障消失。停止要求到达后只读封存，未重启或修产品。
- 57567初版、41045整数解析400现场/窗口、59423阶段展示/窗口、37307工程预览及此前36417和所有旧现场均保持；36417精确PID/starttime未变、封存health200，原用户入口13638保持。所有新监督器/可见driver独立父链及完整容器ID/label/tmpfs、退出边界在`final-runtime.json`；不关闭用户入口或因交权清理。
- 失败、命令/退出、未覆盖、源码/产物/证据清单和唯一归属交回见`var/verification-next.hQ12lW/outcome.md`及`ownership-return.md`。未覆盖长期稳定性/上述退出底层根因、真实供应商研究质量、完整自动确认、生产升级/部署、全部隐藏/取消/状态全排列、跨浏览器/a11y及大规模计划。无真实目标/付费产品模型、业务库、部署、外部投递、commit/push或Arena操作。

## 0.35 历史接收：候选独立验证与结果回流

- 依用户“开始继续完成”及既有完整系统接续授权，D候选首片交回后开启新可见`gpt-6-astra`主`wscyber-verification-main-hQ12lW`。其已实际读包、写accepted并明确消息接收；原入口独立核对CLI239484/start541642→shell239483/start541642→QTerminal239453/start541603、cwd/exe/显式模型与唯一可见窗口，并Read终端截图。窗口号被系统复用，依据新PID重新核验，不沿用旧退出操作。迟到的前执行会话idle通知不改变其已正常退出事实，不对旧地址/PID再做操作。
- 接权前329项产品/文档基线逐项匹配；原入口本次仅补本节及system-delivery接续状态。新包`var/verification-next.hQ12lW/handoff.md`及`ownership-transfer.md`与原入口明确消息双条件生效后，新主独占本轮产品/测试/docs和新验证运行，原入口停止共享写入、继续真人反馈和独立核验，不假称用户交互入口已迁移。只有两个可见CLI，不启动Agent/Workflow/执行者。
- 本片用户终点：从固定候选修订显式发起独立有限验证，保存新的真实证据、支持/反证/不足与执行状态，结果回流候选详情，原发现/历史不被覆盖。先实际核对Cyber/ARTEX同类参考、共享组件复用/缺口和必要契约，再实施；接收不表示设计已冻结或代码完成。首片不由模型单字段自动把候选改为confirmed，完整确认流转仍须后续明确设计，不以结果文本冒充验证执行。
- 36417最终D1、35283失败现场、41893/54933中间实例及全部用户窗口/父链保留，不导航强刷或换产物。TI-1/TI-2仍待设计，报告/修复后复测/真实凭据与工作区不在本片。只新隔离PG与loopback模型/目标；无真实目标/付费产品模型、业务库/部署、投递、Git提交或Arena操作。最终完成状态由新主按实写回，不因接权提前标成功。

## 0.34 D首片已交回并完成原入口整合核对

- **当前最终入口：http://127.0.0.1:36417/projects**，`var/finding-preview.516iqj1m/`及可见`browser.0ywfrx7a/`，2554×1234最大化。候选稳定编号、研究中新增/同编号更新、固定历史与真实证据、正式React详情/分页/按需读取已完成本轮受控本机验收；不是完整D、真实模型质量、长期稳定性、生产部署或用户视觉认可。执行交付见`var/finding-resume.bR2aXS/outcome.md`。
- 执行者已写ownership-return并明确消息停写，主入口实际读交回文件后接回产品/测试唯一写入。主入口独立重算**297源码、10固定产物、147证据**全部匹配，outcome哈希匹配；与本轮起点相比只改`internal/store/store.go`、`web/src/react/components/FindingsWorkspace.tsx`并新增两个store测试文件，无迁移/依赖/共享Drawer最终改动。`entry-source-delta.json`独立确认相同四文件范围。
- 两项确认修复：读取取消不再连带关闭专用持锁连接，独立5秒有界检查、真实断连/失锁继续fail-stop；证据面板关闭后在提交与焦点锁清理完成时归还来源焦点。首次Drawer afterOpenChange方案实际仍失败，已撤回留证；最终不改布局和共享Drawer。35283现场最早断连原因仍无法直接证明，不把确定性一般缺陷修复追认为该次退出的唯一根因。
- 已核对最终 **35顶层PG race**、五包Go race、**React161/161**、共享类型/SSR及正式Go/React构建成功；不是全仓测试。最后Go/PG后仅前端焦点调整，`verify-finalfocus.json/log`重新覆盖对应最终前端/构建。Ant Descriptions span与Vite大chunk警告保留。
- 最终真实运行：执行方`final-runtime.json`封存9个隔离任务、68次本机模型请求且68响应、17次本机目标请求、34候选/42修订，无在途Attempt/call/test、原文关闭零归档。六原组合30调用/12目标，其余为增量、26候选分页和长文fixture。成果经正常研究产生，不是SQL灌入；主入口没有把自己的只读核验称为新增模型/目标执行。
- `browser-accept.37zi434k`3组主流程通过：运行中空→新增、同编号更新不覆盖当前旧详情、显式查看更新、历史/证据按需、真实剪贴板/键盘/Escape/来源焦点、26条25+1分页及往返、1440/1920/390浅暗六组长文。`browser-faults.2zjrhflv`7组通过：503显式重读、错归属拒显、关闭/切对象/切项目迟到、summary退避、401/退出遮蔽；页面错误/外域/故障浏览器写入均0。首个退避实测约10秒，后续20/30秒及停止仅单元覆盖，不冒称全部浏览器壁钟验证。
- 主入口`entry-final-verification.json`另以**61次真实只读GET**核对最终实际exe/index/JS/CSS、精确PG隔离/归属及六原任务编号/固定历史/证据/装配/鉴权；不读模型原文、不操作用户浏览器。已实际Read最终最大化、六尺寸主题长文、增量提示、390暗步骤、401遮蔽共10张，不称逐张审阅所有截图。读取最终关键修复代码和失败/通过日志，不冒称重跑执行者全套测试。
- 35283失败现场、41893中间前端、54933首次焦点方案失败实例及其用户窗口均保留；36417为最终前端。原入口独立重核保留资源19条进程链记录，supervisor/可见driver均PPID1，服务和浏览器不依赖执行CLI，不因交权关闭。精准资源/清理边界见final-runtime/outcome；tmpfs库不是持久生产库，未经授权不删除。
- 未覆盖：35283直接根因、长时运行/真实供应商效果/生产升级备份/大规模查询计划、所有取消与隐藏/Modal/状态码组合、25+历史修订浏览器分页、完整跨浏览器/a11y、整进程崩溃/网络分区全排列。故障测试失败和驱动定位失败逐项保留在outcome，不以最后成功抹去。本轮无真实目标、付费产品模型、业务库、部署、投递、commit/push或Arena操作。TI-1/TI-2保持待设计；后续独立验证另由新短轮接续。
- 原入口完成上述核对后，以已授权的正常`/exit`退出本轮专用CLI57218/shell57217/QTerminal57197，CLI/终端退出码均0、三进程已不存在。保留的19条资源链身份不变、原用户入口13638保持、最终health200，见`entry-retirement-result.json`；首次键入仅xit，实际截图发现未提交，清本次残缺命令后第二次Read确认完整/exit再提交，没有kill。主入口结论在`entry-outcome.md`；下一短轮准备包`var/verification-next.hQ12lW/handoff.md`聚焦候选独立验证，准备不等于已接收/已交权。

## 0.33 历史：D首片接续过程、服务退出与定向修复

- 本轮继续已有候选发现首片的实质复核、必要修正、最终测试和真实Go/PG/React验收，不从零重写；TI-1执行记录/TI-2任务对话仍仅待设计，不自行插入。现已完成基本阶段展示，完整验收与交回尚未完成。
- **阶段入口35283随后退出，当前不可用。** `api.log`在22:50:58记录认领动作/研究失败、连通测试领取与退出收尾的“状态或单实例锁冲突”，API退出1。`postgres-api-exit.json`中PG仍running、OOMKilled=false；`browser-network-diagnostic.json`确认首个浏览器专项POST连接失败、任务仍为原6个。主入口另做一次限时healthz GET确认curl7/HTTP000，已告知用户并撤下在线声明。服务supervisor、PG及可见窗口保留，不自动重启、重放或清理；执行者正在有界定位，不能以新实例起来冒称根因已修。此问题先于剩余浏览器专项收束。
- **随后确认并修复一般取消缺陷，但未追认35283的直接根因。** `pg-cancellationBefore.log`在旧实现用新隔离PG、50ms调用者超时＋200ms锁查询延迟，确定性复现“取消只读请求关闭了专用单实例锁连接”。仅产品`internal/store/store.go`改为锁内再查调用者取消、专用锁查询使用独立5秒有界context、完成后再返回调用者取消；真实断连/失锁仍fail-stop，不重连/重获锁。新增`instance_lock_cancel_test.go`覆盖取消后8并发读取、真实unlock仍停止、内部5秒超时和Close有界；另补D1原件变化/过期更新、已核对unknown仍未知两项测试。
- 主入口实际读取该修复关键代码/新测试及before/final日志。`pg-final.log`为新隔离PG **35个顶层通过**（含新增5项及既有D/C/账本回归）；`verify-final.json`的五包Go race、React类型/测试、共享类型/SSR、Go/React正式构建退出均0，执行方报告React161项。此为修复后的专项/构建，不等于完整浏览器验收或长期稳定性。35283缺少底层首次断连证据，仍不能直接证明其退出就是该机制。执行者已用最终产物另起新隔离实例，不重启/重放或替换旧现场。
- **修复后阶段入口：http://127.0.0.1:41893/projects**，`var/finding-preview.kujs75xy/`，固定binary SHA256 `9115123cccd0b9b9b54d5fd19b32e0b8145f92bea22b44d84ff10995fca0e32e`。六任务重新通过30次本机模型/12次本机目标的基本链；可见`browser.foroo7zo`2554×1234最大化，ready仍明确stage=true，主入口已实际看visible-ready截图。`entry-fixed-stage-verification.json`另以61次只读GET核对实际产物、隔离PG、六任务编号/固定历史/证据/装配及鉴权边界通过，不读模型原文、不写任务、不动用户页面。新服务/浏览器与35283旧现场均保留，余下专项另用隔离context；不是完整验收或长期稳定性证明。
- **以下是退出前的阶段成功记录，不是当前可用性：** 当时入口http://127.0.0.1:35283/projects，服务`var/finding-preview.p4i1q51q/`，新的固定Go/React、tmpfs PG与loopback确定性供应商/目标。可见浏览器`browser.aa0w8_3x`以2293×1234最大化完成候选详情、按需HTTP证据和Escape，ready明确stage=true，错误/外域/写请求均0；主入口已实际查看visible-ready与stage-evidence两张。此后不自动导航该窗口，分页/增量/异常另用隔离context；不是生产部署、真实研究质量或用户认可。
- 主入口读取`verify-first.json`确认五包Go race、React类型/测试、共享类型/SSR、Go/React正式构建命令退出均0；执行方报告React161项通过，最终日志与后续专项待交回复核。新六任务三模型路径两协议×原文开关的30次本机模型/12次本机目标请求检查已交；不把阶段结果扩为完整浏览器验收。
- 主入口`entry-stage-verification-02.json` **61次真实只读GET通过**：实际运行exe及HTTP index/JS/CSS对应固定产物，精确PG标签/tmpfs/loopback与资源上限、六任务编号/当前与旧历史/真实证据/call与装配关联、鉴权/no-store/跨项目404/非法查询400。没有模型原文读取、研究写入或用户页面操作。首次核验在身份步骤即失败、零HTTP；原因是owner保存的短launcher父PID已退出，supervisor/driver由PID1接管，主入口重核同PID/start/cwd/exe与精确脚本/service参数、API/Chromium子父关系后通过，变化记录stage-process-recheck.json，失败版保留。不是跳过归属验证或持续可靠性保证。
- 一个新可见`gpt-6-astra`执行会话`wscyber-findings-resume-bR2aXS`已实际读包、写accepted并消息确认。主入口独立核对CLI57218/start138935→shell57217/start138935→QTerminal57197/start138905、cwd/exe/显式模型和唯一可见窗口，并实际Read终端截图。新包`var/finding-resume.bR2aXS/`的`ownership-transfer.md`及主入口明确消息已双重交权：执行者独占internal/web及新验证产物，主入口独占docs/参考与整合；不增开Agent/Workflow/其他执行者，不关闭用户原入口。
- 主入口补ARTEX同固定commit的任务内findings-tab源码，并实际查看官方仓库findings/sessions两张发布截图。前者是跨任务列表、后者含demo，不能当作真实研究结果或亲自运行上游详情。沿用紧凑列表、详情、就地证据/按需历史；不照搬未实现统计、严重度/全状态管理、跨模块跳转或无界刷新。证据与必要差异见本包reference-notes/manifest；既有Cyber详情观察限制保留。
- 295项产品/测试源基线已保存。首次进程核验已匹配，但枚举清单因Python子进程找不到shell内rg中止；改用Path.rglob完成，不改产品。当前旧预览/数据库/窗口未动；后续使用新隔离tmpfs PG与loopback供应商/目标，heavy.lock串行，阶段可见入口一旦就绪及时展示。旧端口失败不作为清理授权。

## 0.32 本轮接续核对：D首片已有阶段成果；新增两项仅待设计

- 用户询问中断位置及能否接续，重申前端优先参考Cyber与ARTEX实际展示/交互，不得自行随意搭建；另要求方向执行记录（含自动下发提示词）和任务内状态问答/方向调整先设计、登记，不直接实施。本轮仅恢复事实、补参考与文档，没有修改产品源码、运行迁移、重跑测试/构建或启动新开发会话。
- **状态记录滞后于磁盘成果。** `var/finding-next.hkanpgeh/`已存在Go/正式React构建日志、012迁移和候选发现领域/存储/HTTP/共享组件/正式页面实现；已实际读取[实施契约](findings-incremental-d1.md)、`build-first.log`相关结果、`pg-second.log`及预览JSON，不能继续把D首片写成“只有接收、尚未实现”。本轮未完成产品源码审查或对当前文件重新测试，不将历史日志外推为当前最终源码已验收。
- 历史`build-first.log`记录五包Go测试、React类型与**161/161测试**、共享检查及正式构建通过，Ant Descriptions与大chunk告警保留；`pg-first.log`有六项旧升级测试失败，第二轮`pg-second.log`为**30个顶层通过，含6个D1专项**。本轮仅核对日志/计数，不重跑、不覆盖首次失败，也不由末次通过补造失败原因。
- `var/finding-preview.5i9snoqk/api-checks.json`记录三模型路径、两协议×原文开关共6任务、30次本机供应商调用、12次受控目标请求，候选同编号更新/旧历史/证据和实际输入审计检查通过。`browser.7m79sk3u/browser-ready.json`明确`stage:true`：2554×1234正式React候选详情、按需HTTP证据和Escape，零页面错误/外域请求；本轮实际查看`visible-ready.png`一张。**这只是当时阶段展示，不是完整浏览器异常、视觉验收、用户认可或最终交回。** 未发现本包最终outcome/交回证据，接续点是核对已有实现并补齐余下验收，不从零重写。
- 当前`ListAgents`未发现其他可达开发会话，另以`/proc`只读核对仅发现本项目当前CLI。对历史50123/52743/55935/60295/41433的loopback `/healthz`各做一次限时GET，**全部连接失败（curl 7/HTTP 000）**。不能说旧主仍在运行或URL仍可操作；停止原因未确认，没有重启、清理、替换旧产物、导航或关闭用户窗口。
- 新增[待设计TI-1/TI-2](task-interaction-backlog.md)：按方向的真实下发说明/执行流/按需提示词原文，以及任务内只读问答/受控方向调整。明确原文开关、历史不补造、查询不触发研究、调整回执与生效时点、授权/预算/分享边界。只登记待设计，不并入当前D首片或宣布新API/聊天能力已存在。
- 参考补充：只读下载ARTEX固定commit的任务详情、SessionsTab、Transcript，逐文件核对Git blob/SHA256，重核已有MainAgent源码。已读取方向首条intent、会话选择、调用/结果按ID配对、展开读取、历史跟随和主对话输入；manifest为`var/task-interaction-reference.0k_ycktj/manifest.json`。没有运行上游、复用其源码或新访问Cyber详情；静态依据不冒充视觉效果验收。规范和路线已交叉链接此需求。
- 当前分支仍`feat/foundation`、无提交。仅本项目文档及持久偏好更新、公开只读参考；无真实目标/付费产品模型、业务库/部署、外部投递、commit/push、harness或Arena操作。后续先恢复D首片可验证入口并完成原有验收；这两项新功能先补实际界面参考和必要设计，再另排实施。

## 0.31 C3-A专用会话正常退出；接续D组候选发现首片（2026-11-13 UTC）

- 用户在明确说明未提交输入与保留范围的问题组件中选择“关闭专用会话并换新”。C3-A主实际回复已准备正常退出；原入口重新核对PID/starttime/cwd/父链、显式模型/名称及唯一可见窗口后，正常提交`/exit`。CLI、shell与独占QTerminal已消失，CLI/终端退出码均0，原launcher完成；没有kill或清理上层父链。原入口仍承接真人反馈，C2-B会话及所有保留预览不关闭。证据`var/finding-next.hkanpgeh/retirement-notes.md`及`retirement-result.json`。
- 首次严格身份断言因运行中旧exe路径变为`(deleted)`失败，未直接绕过；重新核对其余身份、argv和窗口。首次带窗口参数的按键未实际退出，后续截图确认真实`/exit`输入再回车；失败与成功证据分开保留。01:30:34 UTC核验原入口/C2-B及52743 API/监督器/可见浏览器父链未变，52743/55935/60295/41433健康检查均200，不等于全功能或持续可用性证明。
- 下一短轮范围为**D组首片：增量候选发现与真实证据关联**，任务包`var/finding-next.hkanpgeh/handoff.md`。用户终点是研究中及时看稳定编号候选及后续更新、不可变历史和实际流量引用；模型提出不等于确认。不扩独立验证/确认、报告/复测、图谱、真实身份或文件工作区，不合称完整D。接权后先实际参考、共享组件复用/缺口及必要契约，再实现真实Go/PG/正式React，不能只交模板或接口。
- 新可见主`wscyber-findings-main-hkanpgeh`已实际读取任务包、写accepted并发送明确接收消息。原入口01:39:24 UTC独立核对CLI3096925/start7150567→shell3096921/start7150566→QTerminal3096892/start7150458、cwd/exe/显式gpt-6-astra及唯一可见窗口，并实际查看启动与接收两张截图；窗口号虽被系统复用，但此次依据新PID重新核验，未复用旧退出操作。接收不是实现或验收通过。
- 唯一产品/测试/共享组件/docs写入按本包`ownership-transfer.md`与原入口明确交权消息双条件生效；原入口此后停共享写入、继续承接用户反馈和独立核验，不自称用户交互入口已迁移。接续前324项基线完全一致，交权前仅两状态文档发生预期变化；检查点另存新包、不覆盖旧封包。accepted中的effort标签转录需以原启动截图的high为准，显式模型gpt-6-astra已独立核实，不因此修改设置。仍最多三个CLI，不开Agent/Workflow/执行者；重型验证共用原heavy.lock。

## 0.30 C3-A原入口有限独立复核完成（2026-11-12）

- 原入口实际读交回文件＋明确消息后接回唯一产品/测试/共享组件/docs写入权；实施主确认停写。独立重算324项源码、10项固定产物、70项实施证据全部一致，随后核对研究入口、有界装配、同事务审计、HTTP读取、正式React及关键专项测试；未发现需要修改产品代码的确认缺陷。本次只补验证与状态文档，不改Go/React实现、迁移或依赖；结果和失败版在`var/context-entry-review.r_78x3yk/outcome.md`。
- **可操作入口仍为 <http://127.0.0.1:52743/projects>**。本轮重新通过最终源码React类型、**153/153测试**及共享类型/SSR检查，补齐实施轮最后标签改动后的测试缺口；Ant Descriptions告警保留。已实际审阅实施轮Go五包race和PG首轮失败/第二轮21顶层＋5容量子项通过日志，未冒称本轮重跑Go/PG或重新构建。
- `runtime-check.json`于22:02:29 UTC通过83个真实只读HTTP GET及精确进程/隔离PG核验：API/监督器/可见浏览器/实施CLI的PID-starttime-cwd-exe与父链、PG label/tmpfs/loopback/资源上限、运行exe与HTTP index/JS/CSS哈希相符。六任务仍各1000观察、两调用、零目标动作；12call/12assembly逐项对应实际供应商system+user字节与选择/窗口，记录开启的输入匹配、关闭的input/response为空且数据库无原文行，跨项目404/无鉴权401/非法查询400/no-store通过。不POST、不新增模型调用，不操作保留窗口；不是持续可用性承诺。
- 新隔离headless浏览器在1440/390两尺寸×浅暗四组真实UI检查通过：默认不读取装配/原文、显式读取、38来源两页本地翻页、哈希展开、Tab/Escape、零写入/原文请求/外部请求/pageErrors、浏览器持久存储为空。实际查看新增六图：桌面哈希完整可见；窄屏须使用表格内横向滚动，左右两图覆盖完整哈希，不能说单屏全部显示。另实际看实施轮最大化、1440浅色、1920暗色及390浅暗五张。独立补验不连接、导航或替换原可见浏览器，不等于用户视觉认可/完整a11y。
- 核验脚本首轮把“历史尝试出现”误当本次调用唯一匹配，因后轮快照含前轮尝试而断言失败；改为匹配装配时running尝试后通过，未改产品。两轮动态缩窄截图的定位断言失败，脚本/日志/截图均保留；之后改用各自固定viewport的新context完成上述四组，不把它外推为展开后跨断点调整尺寸的滚动稳定性通过。
- 证据计数纠正：实施主`browser-ready.json`的checks数组实际为**四组**，此前交回摘要写五组不准确；§0.29对应计数已更正，原封包不覆盖。该纠正不改变已记录交互或测试结果。
- 原入口保留唯一写入权。会话列表暂不可达，但实际/proc核对仍有三个本项目CLI，不能将不可达当退出、增开第四个或为腾位关闭旧主/用户入口；所有成功预览和父链保留。下一片尚未冻结或启动。完整C、真实身份/会话、有限文件执行工作区及D–F仍未完成；真实供应商质量、生产升级、长期容量、全部并发/崩溃及§0.29组合缺口仍未验证。本轮无真实目标、付费产品模型、业务库迁移、部署、外部投递或commit/push。

## 0.29 C3-A有界研究上下文与实际装配记录完成受控闭环（2026-11-12）

- 本主`wscyber-context-main-oxe6f3cw`完成交权文件＋明确消息双条件后，实际只读cyber入口和Langfuse/LangChain官方同类材料，冻结[必要契约](research-context-c3a.md)，独立完成Go/PG/正式React；未启Agent/Workflow/执行者或第四CLI。原入口继续承接反馈。完整交付、失败、哈希和未覆盖见`var/context-next.oxe6f3cw/outcome.md`，交回以该包`ownership-return.md`及明确消息为准。
- **可操作入口：<http://127.0.0.1:52743/projects>**，`var/context-preview.2h27mogj/`，新固定Go/React、新tmpfs PG与loopback确定性供应商。可见窗口停留“模型调用详情→上下文装配”，最终2554×1261，自动导航停止并保留。不是部署、真实供应商研究质量或用户认可；旧55935/60295/41433未换产物、强刷或关闭。
- 正式研究链改为Engine仅读Mission/Thread、Routed同一PG只读快照装配；不先读全部历史再截断。recent窗口观察32/动作8/Attempt8，各仅多取一条元数据判更早范围；另保留各最多16条显式反证/未决或效果未知动作，以及最新观察/动作和当前Attempt。自由文本未分类反证可能仍在未读范围，模型和UI均明确未知，不伪称完整。
- Mission/Thread文本64KiB预检，历史候选64KiB预检、整条序列化历史预算96KiB、实际system+user总输入256KiB；可选超大项/预算不足记录未纳入原因，必需项超限或过多明确失败、零模型调用，无摘要/隐藏调用。原件保留；普通工作台/API和动作执行Snapshot仍完整读取，未解决其分页或长期容量。
- 装配记录只含安全状态枚举、来源ID/已有原件hash/时间/字节/选择原因/未读范围，不含正文、摘要或完整prompt hash。与call、Skill/Supplement同Begin事务；失败不派发。追加011不可变表/索引，001–010未改；旧调用明确未记录、不倒填。已保存、已装配、attempted/发送未知、理解未证实各自分层，原文关闭不借审计留完整prompt。
- 共享`ContextAssemblyView`、样例/coverage/浅暗SSR先行，正式`ModelCallDetail`接真实按需GET；每页20条本地稳定分页，无默认正文读取/自动外链/浏览器持久存储，关闭/换call/401/迟到/错归属及503显式重读接通。未引入依赖、主题、图表或执行型工作区。
- `build-first.log`：Go六包定向测试、React类型和**153/153测试**、正式Go/React构建通过；随后仅补后端校验和来源时间标签，`build-final.log`重新通过Go五包race、共享类型/SSR及正式Go/React构建。React153项未在最后标签改动后再跑，不把初轮冒称最终源码全套测试。既有Ant Descriptions与Vite大chunk告警保留。
- `pg-second.log`：新隔离PG **21个顶层＋5个容量子项race通过**，包括5个C3-A顶层：2000原件/老反证/resolved unknown、有界容量/空历史、错关联Begin回滚/跨归属、装配后补充不追认/重开后记录不变、旧调用不补造及v10→v11升级。pause/resume/预算/恢复、S1调用关联和C2-B专项同轮通过；两个专项临时PG已精准清理。
- 真服务六任务（三路径、两协议×原文开关）共**12次**本机供应商请求；每任务两次调用、1000观察原件仍在、零目标动作。实际system/user字节及选择/窗口与审计完全对应，老反证/超大可选项/补充均符合策略；未选普通材料和share=false不外发，原文开启内容匹配，关闭无content记录。API鉴权/no-store/跨项目404/无效查询400通过。
- `browser.ejhy4ces/browser-ready.json`四组检查通过（按实际checks数组纠正，见§0.30）：显式读取/默认零context与content请求、本地翻页、最大化与1440/1920/390浅暗/长UUID/展开hash DOM/键盘，及浏览器注入503/错归属/关闭跨call迟到/当前401。pageErrors/external/写请求均0、持久存储空。已实际看六尺寸主题、最终最大化、未选页、401遮蔽及窄屏展开过程共10张；hash长文展开已DOM断言，但该截图未完整取到hash，不冒称完整视觉验收。不是独立跨浏览器或a11y认证。
- 失败留证：第一轮PG四个旧升级夹具未退011而失败，补清理测试schema的依赖链后上述第二轮全通过，未改旧迁移；首个预览驱动误将资料PUT成功码期望为201（实际200），只修fixture，新实例通过。失败实例`context-preview.w6i51aej`已核对PID/starttime/容器label后清理，原证据不覆盖。
- `final-verification.json`于21:37:36 UTC只读核对API/监督器/浏览器/本CLI父链、精确PG label/tmpfs/loopback/上限、运行exe与HTTP index/两资产、12call/12审计/同Attempt零重复/关闭原文零content、合成标记不进audit/API日志，以及旧三预览29项清单产物未改。Go SHA256 `3e9f8b90719f527ce7739ae4074a9fecefd96946bfa5ee1a2e99481bdcb44bde`；index `758de05974a6acaa301fc88cbf759fd0543466566fb419819c19b5a22edbf880`。这是本主时点核验，非独立人员复核或持续可靠性保证。
- 未覆盖真实供应商质量/生产升级备份/长期容量与查询计划、全部并发和整进程崩溃组合；C3-A装配后Skill释放专属故障测试、固定材料＋分享brief＋Skill满容量真实运行、带新assembly的Begin/Finish真实服务故障注入仍未补齐（既有ledger fail-stop单元与PG关联回归通过不替代这些组合）。普通Snapshot仍无界，完整上下文/文件工作区、真实身份/会话及D–F待后续明确接权短轮；本片不续开下一片。交回后停产品/测试/docs写入，保留所有服务/窗口和原入口父链。

## 0.28 C2-B原入口复核与界面修正完成；接续有界上下文（2026-11-12）

- 原入口实际接回唯一写入权后完成有限关键源码/测试/运行证据复核，交回305项源码/测试/文档与10项产物清单重算一致。纠正共享SupplementEditor的13px常驻说明，仅修改共享组件、CSS和SSR测试三文件，未改Go、依赖或001–010迁移。详细结果、改前/差异、失败及未覆盖见`var/controls-entry-review.ria6z8kd/outcome.md`。
- **修正版可操作入口：<http://127.0.0.1:55935/projects>**；新固定React、原已核验Go binary、新tmpfs PG和loopback确定性供应商，`var/controls-preview.13ztxdm2/`。旧60295已做独立只读终核；41433/60295产物、旧用户窗口与服务父链不替换、不强刷、不关闭。不是部署、真实模型质量或用户认可。
- 本轮重新执行正式React类型及**150/150测试**、共享类型/SSR、正式React构建，全部通过。审阅交回Go五包及16项PG/race日志，未冒称本轮重跑Go/PG race；精确race临时容器再次确认不存在。Ant Descriptions与Vite大chunk告警保留。
- 新实例三模型路径、两协议×原文开关六任务真实API四组通过；新可见浏览器六组重验pause/resume/run、默认未分享、草稿跨页/关闭/Escape、正反Tab、201后丢响应只GET、保存零关联→下一模型调用、POST401重认证和原键同载荷显式重试、退出清空、纯文本及浅暗长文。pageErrors/external=0；新增断言常驻说明不存在、正文14px。
- 实际查看修正版最终最大化、补充表单、390暗长文及1440/1920/390六种浅暗共9张；最终2554×1261可操作，自动导航已停止，不代替用户视觉认可。旧headless四组迟到/错归属/503/GET401证据已审阅，本次未重跑，不计作新增专项。
- `runtime-old.json`、`runtime-new.json`分别独立只读核验真实PID/starttime/PPID/cwd/exe、精确PG label/tmpfs/loopback/上限、HTTP index与两资产、鉴权/no-store、原件hash/模型调用归属/原文开关/零目标动作。旧实例时点17次、新实例时点14次本机调用，不混用计数。新index SHA256 `edc1f589f34060347f1482de8d6a2f0688d30b6cfca6e6efa7779cf5a09ab034`；Go沿用§0.27 binary。首轮核验脚本误写output而非response字段，仅修脚本，失败版留存。
- C2-B本片接受为受控本机交付，不合称整个C。下一短轮为C3-A有界研究上下文与实际装配记录，任务包`var/context-next.oxe6f3cw/handoff.md`；先实际参考/组件复用和必要契约，接权后实质实现。当前未声称该片已实现；真实身份/会话、有限工作区及D–F仍待完成。新可见主`wscyber-context-main-oxe6f3cw`已实际读包、写accepted并消息确认；原入口核对CLI2362181/start5416224→shell2362180/start5416224→QTerminal2362150/start5416175、cwd和显式gpt-6-astra，并实际查看可见交互终端截图。唯一产品/测试/共享组件/docs写入按该包`ownership-transfer.md`与原入口明确消息双重生效，原入口随后只承接用户反馈和核验；不把接收算作实现。当前三个可见CLI，不增开执行者/Agent/Workflow；旧主不可达不等于退出，原入口和所有保留预览父链不关。本轮三状态文档before/after及delta、305项源码和固定产物检查点另存`var/controls-entry-review.ria6z8kd/`，不覆盖旧清单。

## 0.27 C2-B研究控制与人工文本补充完成受控闭环（2026-11-12）

- 本主`wscyber-controls-main-y4LD72b1`收到原入口交权文件和明确消息后独立实施，无Agent/Workflow/执行者。实际身份、已读范围、只读cyber/ZAP参考与接权在`var/research-controls-next.y4LD72b1/`；未观察到参考站完整控制/补充流程，不编造对标结论。契约[research-controls-c2b.md](research-controls-c2b.md)，交付与残余边界见该包`outcome.md`。
- **可操作入口：<http://127.0.0.1:60295/projects>**，固定Go/正式React、新tmpfs PG与loopback确定性供应商，归属`var/controls-preview.j0ee4Kdg/`。可见窗口停在已有合成任务的研究控制/补充区，最终2554×1261最大化，不再自动导航。不是部署或真实研究质量；41433固定产物、旧窗口与原用户入口未强刷、替换或关闭，本轮未重新证明41433当前存活。
- 三操作分开：pause持久暂停意图、关闭auto_run但不伪造在途结束；run使用原任务绑定明确启用模型；resume仅允许原approved动作继续，不启模型、不批准pending、不重放unknown。原累计预算、concluded/在途/未核对unknown保护保留；公开真实`pause_requested`与`max_steps`，旧接口语义不变。
- 新项目/方向归属控制及补充写入复用全局`operations`键域，原响应与业务同事务、旧回放先于能力/状态检查。补充原件/hash不可变，单份/累计/序列化/份数有界；来源是用户声明，显式分享才保存，不改变C2-A share=false。列表/普通Snapshot/事件/核对回执不新增正文，原件显式读。
- 后续实际上下文装配从PG读取补充，清掉调用者预填，不修改已装配/已发的在途输入；Begin同事务关联实际携带ID。保存、装配关联、attempted及not_sent/transport_unknown/interrupted分别显示，不称已理解/采纳。原文归档关闭仍保存补充原件与最小事实，不绕道归档完整提示词。仅追加010，001–009未改。
- 共享`ResearchControls`、样例/coverage/SSR先行，正式React复用TaskModal、MaterialText和SkillCallFacts。App级草稿/原键/冻结载荷在关闭、离页和401后保留，退出清空；失联只GET核对，404不证明未提交，显式确认才同键重试；原件/调用事实取消、归属与迟到保护接通。无新依赖或另一套主题。
- `build-tests.log`：Go五包定向回归、正式React类型及**150/150测试**、共享类型/SSR和Go/React构建通过；不是全仓测试。`pg-tests.log`新隔离PG16顶层通过；后补强化跨归属测试，避免被同Attempt唯一约束提前拦截，再于`var/c2b-pg-race.8ZZxRXnT/`对同16顶层**race通过**，11.497秒，精确临时容器已清理。所有重型命令共用heavy.lock；Ant Descriptions及Vite大chunk告警保留。
- 真服务`api-checks.json`四组通过：environment、managed chat、managed messages三路径×原文开关6任务；在途pause、resume不调用、run后消费补充、旧false说明不外发、Begin关联、实际原文开关、回放/冲突/显式分享校验、鉴权/no-store/归属。setup为12次本机供应商请求；浏览器后终核为17次，六任务均零目标动作。不是三种协议或当前持续总数保证。
- `browser.6f9o6ubb/browser-ready.json`六组通过：三控制真实提交、分享默认false/错误零写、关闭/Escape/跨页草稿、正反Tab、真实201后响应丢失仅GET核对、保存零关联→后续调用、原件纯文本、POST401遮蔽/重认证/原键同载荷显式重试、退出清空，及浅暗1440/1920/390、长文。pageErrors/external均0、持久存储空；故障为受控传输注入，不冒称真实服务故障。已实际查看最终最大化、六种尺寸/主题、390长文及调用事实共9张；未逐张审核全部13图，不代替用户视觉认可。
- 独立headless `boundaries.DB1olSOV/result.json`四组通过：关闭后迟到不复活、错项目原件不显示、503局部错误且显式重读、GET401遮蔽且重认证不自动打开原件。pageErrors/external/writes均0；已实际看其401遮蔽图。
- 失败留证：首轮PG暴露operations仍限mission及旧迁移测试退版本遗漏010，已只在010扩展资源约束并补退版本夹具；首轮log/result保留。三次可见驱动分别因图标名、双汉字确认间隔、刷新按钮精确角色名定位超时而中止；截图/结果保留，只调整驱动，不改产品迎合测试，最终通过并核验刷新按钮不在aria-hidden/inert树。仅停止本轮失败驱动，未动旧用户窗口。
- `final-verification.json`在20:10:59 UTC核对实际API/监督器/浏览器父链的PID-starttime-PPID/cwd、精确PG label/tmpfs/loopback、运行中exe与HTTP index/两项资产哈希、健康/鉴权/no-store、原件hash、普通读无唯一正文标记及操作回执字段白名单。后端SHA256 `208208a991ba889f735d35f446aa43e4a58a69a518fe2d76e2c83d04da6d9004`；index `7d71e77e89bed2fd7d88427ba53cd4bd089765470c8fcbf6a56824ef7dcb968d`。setup的含换行原文泄漏断言可能被JSON转义弱化，终核用无换行唯一标记补验，不改旧证据。
- 未覆盖真实供应商质量、生产升级/备份、多租户/长期容量、全部并发/整进程崩溃组合、25+调用的浏览器分页、完整跨浏览器/a11y认证；页面多个读取不是一致性快照，短暂刷新差异不当实时全局事务视图。可见测试不证明所有错误的焦点引导。仅合成既有任务用作fixture，不提供真实研究服务。
- 本片交回原入口后停止产品/测试/docs写入；保留本主及原入口服务/窗口父链，不以idle为由退出。无真实目标/付费产品模型、业务库/部署、外部投递、commit/push、harness或Arena操作。真实身份/会话、完整上下文/工作区及其余C/D–F仍待后续，不合称整个C完成。

## 0.26 C2-A接续验收完成；继续研究控制与补充材料（2026-11-12）

- 用户要求“现在进展如何，接着继续完成”。原入口`wscyber-db`重新核对时只有当前开发CLI可达，多个历史API已不运行；未关闭旧用户窗口或清理未知资源。接续只对此前C2-A源码重新测试/构建/运行，不改产品源码、依赖或009迁移；结果见`var/c2a-resume.BtptOqcM/outcome.md`。非秘密说明不是目标凭据接入，默认false分享及低信任边界保持。
- **新可操作入口：<http://127.0.0.1:41433/projects>**，真实Go/React、新tmpfs PG与loopback确定性供应商，归属`var/brief-preview.XjYvj2El/`。旧59215/41409/51013/52903/50197仅保留历史验证，不承诺目前可用；本轮未重启或替换旧实例，旧服务退出原因未确认。
- 五包`domain/model/modelledger/research/httpapi`定向race通过；新隔离PG九顶层Brief/Research/S1恢复与关联专项race通过；正式React类型、**147/147测试**、共享类型/SSR契约及正式Go/React构建通过。对应本轮`backend-race.log`、`frontend-checks.log`、`pg-race/`、`build.log`。Ant Descriptions及Vite约1.27MB JS大chunk告警保留，不是全仓/零告警验收。
- 真程序`api-checks.json`四组通过：三模型路径×分享×原文开关12任务、only-brief/固定GET/同键回放与冲突/鉴权/no-store/跨任务隔离/普通读无正文，以及S1 discover→load→下一调用与brief组合。setup16次本机供应商请求、零目标动作；`provider-evidence.json`只是setup时点，浏览器后续新任务不计入这个16，不能当实例当前总数或真实模型效果。
- 可见`browser.jha02b3c/browser-ready.json`四组通过：0/1/8身份、错误零提交及焦点、长文/内存草稿、真实202/在途锁定/默认false固定GET、纯文本及浅暗1440/1920/390。页面/API/外部请求错误均0，最终2554×1261最大化。已实际看最大化、1440/1920浅暗、390暗色固定说明和390浅色8身份截图；未逐张审核全部10图，不代替用户认可。
- 独立headless `boundaries.vebqbado/result.json`六组通过：迟到GET/503显式重读/GET401遮蔽、真实202失联只GET核对、POST401重认证保留原键原文且仅显式重试、退出清草稿和分享。pageErrors/external均0，writes3，持久存储空；已读结果/日志并看401遮蔽图。这是传输故障注入，不冒称服务端真实故障。
- 首轮异常驱动在重认证异步恢复弹窗前抢点背景按钮而超时，`boundaries.4469p8m5/`留证；核对实际App恢复逻辑后只改驱动等待，未改产品迎合测试。终核首次argv后缀误判Chromium亦留失败版本，改用内核exe辨认后通过。
- `final-verification.json`只读重核250项源码未变、API/监督器/可见浏览器PID-starttime-父链、精确PG label/运行/tmpfs、运行中exe与实际HTTP index/两项链接资产、真实健康/鉴权GET和浏览器创建任务false/no-store均一致。后端SHA256 `42b0542dbf9ce6432b68648618c707d877afacb0b38b0e7da10267e388b09829`；index `819686a0a281b0f8075119dda2ac7853f4276cb4047ac489d9dd0b51369fd2e7`；专项PG临时容器确认已不存在。这是时点核验，不是持续可靠性承诺。
- 原入口及41433服务/可见窗口父链保留，不自动导航、强刷、换产物或退出。下一短包`var/research-controls-next.y4LD72b1/handoff.md`为C2-B真实暂停/明确接续与人工文本补充；先参考/组件复用与必要契约，再做可验收业务片。新可见主`wscyber-controls-main-y4LD72b1`已实际读包、写accepted并消息确认；原入口核对CLI2131336/start4894621→shell2131335/start4894620→QTerminal2131297/start4894591、cwd、显式gpt-6-astra及真实可见画面。共享产品/测试/docs唯一写入以该目录`ownership-transfer.md`及明确消息转交，原入口随后只承接反馈/核验，不并行修改；接收不等于C2-B已实现。当前两可见开发会话，不开执行者/Agent/Workflow，不能退出原用户入口。
- 未覆盖真实供应商研究质量、生产升级/备份、多租户/长期容量、所有迟到/并发/整进程崩溃组合、完整跨浏览器/a11y认证。目标身份/会话、完整上下文/有限工作区及D–F仍待完成。仅本项目/隔离fixture，无真实目标/付费产品模型、业务库、部署、外部投递、commit/push、harness或Arena操作。

## 0.25 历史：C2-A身份说明与业务前提实施过程（2026-11-12）

- 原入口已核定准备包两项选择并明确本主唯一产品/测试/契约/docs写入归属，见`var/c2-identity-prep.gz4jssgu/entry-decision.md`；接收及新基线保存在`var/research-brief-main.3apc0k4k/`，不覆盖S1清单或后续三文档delta。§0.24末“待核对”是此前准备状态，不再是当前阻塞。
- 本片为创建时固定的非秘密身份说明、业务规则与复现前提；分享默认false，显式开启才进入本任务绑定模型的低信任输入。真实目标身份/会话接入、完整C2和D–F仍待完成。正式契约见[research-brief-c2a.md](research-brief-c2a.md)。
- 后端类型/严格验证/追加009不可变存储/创建事务/显式GET/模型装配、共享编辑与只读组合及正式React接入已落地。`go-unit-02.log`定向单元、`shared-01.log`共享类型/SSR、`react-02.log`正式类型与147/147测试、`pg-01/tests.log`新隔离PG九项事务/兼容/恢复测试和`build-01.log`独立Go/React构建已通过；尚在运行与异常验收，不是用户验收。Ant Descriptions及Vite大chunk告警保留。
- 失败证据保留：首次Go测试源码误放U+FEFF，改为显式码点后通过；React首轮146/147，旧断言过宽匹配新分享标签，改为精确核验模型选择器不存在（环境模型仍可分享说明），未删产品功能。首个新预览fixture误读Mission.description（实际字段origin）导致供应商断连/等待超时；仅修fixture，失败实例按精确PID/starttime/label清理，不改产品迎合测试，不把cleanup exit0说成业务通过。
- 三个会话、50197及全部旧预览/窗口/草稿/服务父链保留，不开Agent/Workflow/执行者；只用新隔离PG和loopback fixture，完成该片交回停写。

## 0.24 S1辅助Skill已实现并完成本机展示（2026-11-12）

### 实现与范围

- `wscyber-skill-main-ikg6S3xp`已按`var/skill-main.ikg6S3xp/ownership-transfer.md`及明确消息接收产品源码/测试/docs/web唯一写入权，独立冻结并实施S1，没有启动执行者、Agent或Workflow。原入口及旧主继续保留服务父链，不因交权或idle退出。本轮收束材料在该目录`outcome-main.md`，源码/测试/文档哈希在`source-manifest.sha256`；交付结果不等于另一次写入权转移。
- **普通资料与显式Skill共用一个库**：只有显式分类为Skill的文件参与自主发现/按需加载，且适用说明必填；旧条目默认普通，文件名/frontmatter不授予分类或权限。创建省略classification为普通、修改省略保留原分类；保留旧请求指纹、回执和任务固定材料。旧手动选用Skill不倒算为自动加载次数。
- 增加严格`skill_discover`、`skill_load`、`skill_release` Decision与追加迁移008：有界元数据发现、CAS加载、不可变活动/成功原件、当前方向活跃引用，以及实际模型调用关联。活动不产生目标Action，不进入目标审批；每Attempt仍最多一次模型调用，由下一正常Attempt消费活动结果，不强制使用Skill或全库注入。
- 每方向最多4个活跃加载，正文合计65536字节、序列化活跃集98304字节，原256KiB模型输入限制保留。成功加载计数从不可变明细派生；发现/失败/释放/GET/同Attempt回放/重复携带不增加。Begin与已尝试发送、not_sent、transport_unknown、interrupted分别保留；原文归档关闭仍保存Skill固定原件，不借统计归档完整提示词。
- 正式React复用项目共享编辑器、TaskModal及主题，先补共享`SkillRecords`/样例/覆盖，再接分类保存和项目“研究配置 → Skill使用”。可查成功次数、活动、按需固定原件及关联调用；分页替换、关闭卸载、401遮蔽和迟到取消保留，无新依赖或常驻说明墙。契约见[automatic-skills.md](automatic-skills.md)、[api.md](api.md)。

### 已运行与核验

- **正式独立入口：<http://127.0.0.1:50197/library>**，`var/skill-preview.n10kggBV/`；固定后端02 SHA256 `6c9b5fcc7ef0f5e5dd9397d28737fe3e884e5b8552cd08468e587c5deb7c77bc`，React index SHA256 `78e57adbabd1a61d6a20d36127983d3b6dc46cbc0056050799565f8e5bd20a6c`。使用新隔离tmpfs PG和loopback确定性供应商，不是真实模型研究效果或用户验收。
- `api-checks.json`通过：environment/managed openai_chat/managed anthropic_messages三路径×原文开关共6个Skill任务，每任务6次模型调用、5个活动、2次成功加载；另无Skill任务2次调用，共38次本机供应商请求。核对真实上下文、调用关联、普通正文不外发、归属/鉴权/分页边界、零目标动作。`main-final-verification.json`再核对精确API/监督器PID与starttime/PPID、PG容器label与运行状态、运行中exe及实际服务index/两项链接资产哈希；六任务仍各2次加载。该时点只读核验不等于持续可靠性承诺。
- Go `domain/model/modelledger/research/httpapi`五包定向回归通过；`pg-race-02/tests.log`为新隔离PG的6个Skill顶层测试及容量3子项race通过，覆盖分类兼容、数字序号分页、并发同Attempt去重、固定历史/释放、容量、调用关联与恢复。另`pg-recovery-04/tests.log`两项race通过：已保存活动未Finish时重开/Recover不重计、不自动认领，以及跨方向/跨项目Begin拒绝且无孤立调用。
- `final-targeted-03.log`：`research/modelledger/httpapi`三包race通过，覆盖活动持久失败门闩、Finish失败不重做、Begin/Finish失败携带真实Skill身份和HTTP只读边界；正式React类型检查及**140/140测试通过**。其中既有C1材料7项与新增S1四项现已注册到正式测试入口，不把前序129项说成已包含它们。`check-02.log`此前三入口类型检查、共享SSR/契约和正式Vite构建通过；之后仅补测试/文档，未将旧构建冒称重新构建。Ant Descriptions及大chunk告警仍保留。
- `browser-02.uwvr6bvr/browser-ready.json`三组真实页面检查通过：分类实际保存/说明必填/纯文本、项目2次加载及按需正文/调用事实、浅暗与1440/1920/390无整页横溢出，page_errors/api_errors为0。已实际查看浅色详情、暗色390及最终桌面截图，未逐张验收全部9张。原ready的最终视口仍390；另经`desktop-restoration.json`和`visible-desktop-fixed.png`确认最终2554×1261，窗口停止自动导航并保留。
- 独立只读headless `browser-boundaries.e3Kokp/result.json`三项通过：关闭后迟到原件不复活、503局部失败且仅显式重新读取恢复、当前401取消在途并遮蔽；page_errors和写请求均0，local/session storage均空。已实际查看401遮蔽截图。这里是传输故障注入，不冒充真实服务故障或用户页面操作。

### 失败保留与未覆盖

- 首个新预览`var/skill-preview.gHH5UOoN/`真实暴露`seq::text`别名使ORDER BY按文本跨9→10排序错误；已改为`skill_activities.seq`数值排序，并补12活动/3条分页回归及新实例API验证。失败证据保留，终核确认其精确容器已不存在，不把清理exit0写成业务通过。另修load/Attempt为36字符UUID、区别于32hex资料身份的校验。
- 首个本轮浏览器因Ant隐藏辅助option定位超时而失败；新驱动只点可见选项，旧失败窗口/草稿仍留。第二窗口首次恢复仅扩窗口未恢复390页面视口，随后精确恢复自有驱动Page到桌面尺寸并实际看图，原失败/中间截图不改写。`pg-recovery-03`因测试错误要求auto_run=false失败，核对既有恢复契约后改为blocked且ClaimThread=nil，在全新PG的04通过；未为迎合测试修改产品Recover。
- 未覆盖真实供应商/模型研究质量、生产升级/备份、多租户、长期容量、全部分类/删除/加载并发与整进程崩溃组合；没有完整跨浏览器/a11y认证、25+活动真实浏览器分页及所有跨项目迟到组合。`skillsApi`已有归属核对但尚非完整响应结构/重复ID验证；007旧二进制实际生成回执的互操作未完整验证。单元故障注入不等于整进程可靠性验收。
- **旧59215/41409/51013/52903、全部用户窗口/草稿及原入口/旧主父链均未重启、替换或关闭。52903仍固定React03且含已知当前目录删除缺陷，旧浏览器失败仍不是ready。** 本轮50197服务及两个可见浏览器父链继续保留；不退出承载它们的当前会话。仅本项目/新隔离PG/本机fixture，无真实产品模型、外部目标、业务库、部署、commit/push、harness或Arena改动。C2和D–F没有在本轮展开。

### 原入口复核与后续文档补记（2026-11-12）

- 原入口已完成限定范围源码/证据复核，记录见[独立复核结果](../var/skill-entry-review.64a56g0w/outcome.md)：重算43项源码/9项产物一致，核对普通资料隔离、成功计数、实际调用关联及既有日志，未发现阻断本轮工程接收的问题。原入口未重跑测试/构建、未终核运行实例、未操作用户窗口；不把实施方既有运行证据说成原入口新增验证，也不是用户认可或生产就绪。
- 原入口明确本主独占本次`STATUS.md`、`system-delivery.md`、`research-configuration.md`补记，纠正后者尚未实现/手选必经等过期表述；旧S1 manifest保留为原交付检查点，后续三文档的before/after与delta哈希另存`var/c2-identity-prep.gz4jssgu/`，不覆盖旧证据。
- 随后仅准备C2“身份与业务前提”有界短包：核对已确认需求、授权只读参考、共享UI与安全边界；**暂不改internal/web，待原入口核对短包并明确产品写入范围**。非秘密身份说明不等于登录凭据已接通，目标秘密不自动外发、授权不扩张；涉及新外发决策或重大产品变化先交回具体分歧。当前三个会话/全部预览与草稿继续保留，不增开Agent/Workflow/执行者，本次补记不新增产品测试或运行结果。

## 0.23 历史：C1底座已接通；辅助Skill方向调整，手工选材路径暂停固化（2026-11-12）

> 以下保留C1交回与S1接权前事实；“尚未开发/下一短轮”描述当时状态，当前S1实现与验证以§0.24为准。

### 当前方向与归属

- 用户在原入口提出：资料库类似Skill库，Agent根据研究状态和探索方向自动按需调用，Skill只辅助而不驱动流程，按项目统计实际使用。原入口转达后，主/前端均已暂停手工选材专用路径的扩展和最终固化；不撤销代码、固定材料或预览，不把新方向写成已实现。原入口随后转达用户已选择**“区分Skill与普通资料”**：共用一个库，只有明确设为可供Agent使用的Skill参与自动发现/按需加载，普通资料仍显式提供给任务，旧条目不自动转换。产品范围已收敛，下一短轮冻结必要技术契约并实施，不再等待笼统开工。
- 源码影响与未冻结建议见[辅助Skill方向调整](skill-library-direction.md)：复用薄循环，增加有界只读发现/加载活动，下一正常轮消费；保留每Attempt唯一模型调用，不进入目标动作审批。推荐统计成功加载次数，逻辑身份去重；目录曝光/失败/自动重复携带不计，模型调用关联和发送未知另列。不声称加载即执行全部指导。自动加载/统计尚未开发，无新增schema或接口。
- 本基础轮主`wscyber-research-main-81s5um0`（CLI1197305/start2761239）已完成整合；前端`wscyber-materials-ui-81s5um0`以`frontend-outcome-04.md`交回，web归主且执行会话已正常退出。阶段报告`var/research-main._81s5um0/outcome-main.md`。原入口wscyber-85继续用户沟通和旧三预览，本轮主保留52903/浏览器/新主的监督父链；二者都不因交权或idle关闭。
- 下一可见主`wscyber-skill-main-ikg6S3xp`已实际读包、写`var/skill-main.ikg6S3xp/accepted.json`并消息确认；主独立核对CLI1448802/start3318154→shell1448801/start3318154→QTerminal1448752/start3318118、cwd、显式gpt-6-astra及真实可见画面。精确地址`uds:/run/user/0/cc-socks/1448802.sock`；短交接为该目录`handoff.md`，共享源码/docs/web唯一写入以`ownership-transfer.md`及明确交权消息生效。新主接权后独立冻结并实施自动Skill，不再等待库定位；旧主停止产品/docs写入。原入口＋本轮保留主＋新主已达会话上限，不再开执行者/后台Agent/workflow。窗口ID可被重用，不将旧前端退出窗口号用于新主清理。

### 已实现与本轮验证

- 已实际只读cyber入口200，无401/403，未找到匹配资料库可见入口；拦截1次页面写请求，无业务正文/截图采集。已取得并阅读Open WebUI官方资料管理流程和Agent Skills规范，核对共享UI/用例及Ant MIT许可证。旧C1参考/复用/容量/接口见[本轮契约](research-materials-c1.md)，现已标注方向调整。官网超时保留，官方仓库正文取得不冒充登录UI观察。
- 后端/正式React已接通文件夹、UTF-8单文件导入与CRUD、元数据分页/显式正文、CAS与独立只读回执；旧路径创建时同事务固定材料原件/hash及默认关闭的附加安全清单。库后改删不改旧任务，unknown/401只核对、不自动重发。追加007迁移，不改历史迁移，不新增产品依赖。旧固定选材路径是当前代码事实，不能作为新自动Skill的完成声明。
- Go四包单元回归通过；新隔离PG的15项定向事务/升级测试通过（`pg-01/tests.log`）；C1库/研究7项顶层及容量3子测试race通过（`pg-race-02/tests.log`）。后续`domain/httpapi/model/execution/research`五包影响范围race回归通过（`backend-boundary-race-03.log`），不是全仓或沙箱完整重验。正式后端01 SHA256 `449a85a85f4f379f911e5e57490d54555a311f87072ea35beb783add12cafac4`。
- 新独立预览 **<http://127.0.0.1:52903/library>** 使用后端01＋固定React03（index SHA256 `cc6f1e9776b47ff5033fc29d9c437c1f2a780c91ef9118ff04311dafc0e66875`）、新tmpfs PG及本机供应商fixture。`var/research-preview.BnPYdISl/api-checks.json`五组通过：environment/managed两协议的实际材料与清单开关输入、未选正文不发、库改删/历史回放/API重启保持固定原件、超预算零对应派发和日志边界。六次模型输入验证将选材/清单/原文记录同时开关，**未覆盖全部独立开关交叉组合**；固定conclude响应不证明真实模型遵守或研究效果，无目标动作。
- 主第二轮可见浏览器已完成真实创建PUT/显式正文GET、HTML按原文展示、浅暗及旧任务已删除Skill原件按需读取；主已看暗色库和任务固定原件截图。驱动随后在Modal“关闭”定位失败，`browser-02/browser-failure.json`保留，pageErrors/apiErrors均0，**没有整轮ready/成功声明**。第一轮标题定位失败亦保留。两窗口停止自动导航并保留，不为完成旧路径另开第三轮或影响用户页面。
- 前端旧路径`frontend-runtime-06/browser-result.json`九组真实API浏览器通过，使用**固定React03**；覆盖CRUD/UTF8 BOM与CRLF、草稿离页/401、stale、写入失联只GET、创建资料/原文/安全与迟到401组合、六组尺寸浅暗及删除后固定原件，errors/external为空。主核对结果/清理并查看代表性截图，未逐张验收全部15张，不冒充用户认可。前序构建/驱动失败证据保留。
- 前端最终04已交回：主独立核对`frontend-source-manifest-04.sha256`全部24项及04 index SHA256 `6a00a4b8e584e3add1b24ccb6970ba3454c4d97b91dba8166aff38b17960b84f`一致；核对`frontend-checks-04.log`的三入口类型检查、共享检查、129项既有＋7项资料测试与构建通过。Ant Descriptions及Vite大chunk告警保留，不称零告警。
- `frontend-library-edge-04/browser-result.json`七组基础专项通过，errors/external为空：导入异常零写与32KiB边界、390长名称/正文浅暗和键盘焦点、实际移动、迟到PUT×401与错归属回执只GET、迟到/删除正文保护、退出清空内存草稿、删除当前目录后退根。主核对结果并实际查看两张390浅暗截图；04相较03唯一产品修正是`LibraryView`按实时folders识别失效parent并退根。**04未重跑已暂停的手选整链；03证据不冒充04全路径回归。52903仍为固定03，仍含当前目录删除缺陷**，不以新版源码冒充当前预览已修复。
- 主逐一核对10次前端自有运行的ownership/cleanup及当前精确容器ID、API PID/starttime，10个容器及原API身份均已不存在（`main-frontend-resource-check-04.json`）。确认执行者停写、独占父链/无子任务及真实空闲画面后，正常`/exit`；CLI1223638、shell1223637、QTerminal1223596及其独占窗口均已结束，CLI/终端/监督器退出码均0（`main-frontend-exit-final-04.json`）。首次按窗口注入只送达`it`、未退出，修正为核对焦点和完整`/exit`画面后提交；失败记录保留。没有关闭用户入口或保留预览。

### 保留边界与接续

- 59215/41409/51013以及52903固定产物、旧窗口和草稿保持，不就地换二进制/迁库/强刷；服务和浏览器分别保留。新预览归属为`var/research-preview.BnPYdISl/ownership.json`，具体身份按记录并在操作前重核，不复用过期PID做清理。
- 不用后台Agent/workflow，重型命令共用既有heavy.lock。仅新隔离PG/loopback fixture，不改业务库/部署/真实产品模型/外部目标/commit/push/harness/Arena。基础库管理和安全清单本轮已收束；自动Skill的可发现范围已收敛，下一可见主短轮冻结最小契约并实质实现。C2身份/业务前提/完整控制及D–F未完成，不因本次方向调整自行展开执行型Skill、脚本安装或全库正文注入。

## 0.22 已交回：B组调用记录、可选原文与用量（2026-11-12）

### 当前结论与接续

- **B组已实现并完成受控本地专项、最终组合联调及实际展示；不等于用户认可、生产就绪或完整系统完成。** 原文默认关闭并固定到任务，usage独立记录；未知不按零、估算不冒充账单、历史价格不重算。正式契约见[调用账本](model-call-ledger.md)，主整合记录为`var/full-system-usage.hKFpjTHZ/main-outcome.md`。
- 后端最终固定产物为`wscyber-b-candidate-06`，SHA256 `51a24a1c254c497dae7a6ebcea9c4e3f9a9e1be086109b73d0cd775d08d1d1c8`。主核对108项源码/fixture及二进制哈希，14组正式运行/故障检查通过；新隔离PG上全仓race为153个顶层、544条含子测试PASS，无FAIL/race报告。唯一SKIP为未启用Docker专项的`TestSandboxIsolationAndPersistedEvidence`，不写成通过。研究Decision回显已知连接凭据时整次拒绝、usage保留、零动作派发，不删改动作后执行。证据：`backend-outcome.md`、`runtime-06/`、`main-backend-final-verification.json`。
- 前端最终为`frontend-checks-01/react-snapshot-04`，index SHA256 `318a5d930b568491d98a177f935dc40fb4c16e64a8dec3ce69fd8c28c28e848f`。主核对26项源码/驱动哈希；129/129测试、共享契约、三入口类型/构建通过。最终业务runtime08十组、图表runtime11三组通过，主已实际查看窄屏tooltip及高对比度/打印等截图。**执行者浏览器专项使用后端02；不能写成已用06重跑全部专项。** Descriptions及大chunk警告保留。
- **最终组合展示入口：<http://127.0.0.1:59215/usage>**。`var/usage-preview.9oaya1ES/`使用后端06＋前端04、新tmpfs PG及双协议loopback fixture。主实际完成五组浏览器流程、最大化浅暗展示，并核对运行中exe/全部资产、进程身份、PG归属及真实鉴权API：4调用、560输入/100输出/660总tokens、USD0.001724，真实目标fixture2次。`sustained-ready.json`记录180秒持续API读取通过，不是长期可靠性证明。
- 主另对Apia整日跳过、圣保罗午夜gap、纽约23/25小时日做全局及任务历史窗口只读核验，均有界返回且日标签/任务summary_scope正确；空账本不能证明当时真实消耗为零。首次主驱动误将任务专用summary_scope要求套到全局而失败，保留`main-check-attempt-01.json`；按契约修正驱动后`main-ready-check.json`通过，未改产品迎合错误断言。
- 旧B入口41409已无刷新增量发布前端04，保留旧hash资源和用户草稿；后端仍是02，**不是最终后端、安全或历史日期修正版的验收入口**。`var/usage-preview.aRWP2445/web-04-independent.json`确认实际资产、真实API、直标/键盘读取通过，零API写入/原文读取，未动用户页面。A组51013及旧窗口仍保留；不自动替换二进制、迁库或重启预览。37529旧退出根因仍未明。41409外部Monitor已到期，不称持续监控。
- 两个B执行会话均已明确交回停写。主核对临时容器/端口清理、精确PID/starttime/独占空闲终端及无子进程后正常`/exit`，退出码均0，CLI857034/857035及各自shell/QTerminal均已结束；`backend-exit-check.json`、`frontend-exit-check.json`留证。源码归属回主，不复用旧PID或延迟idle通知；用户主入口和保留预览没有关闭。
- **未覆盖仍保留：** 新原文开关×创建401/迟到回执的组合浏览器专项、全部筛选组合与列表筛选迟到竞态未逐一执行；没有完整可访问性认证、长期容量/跨浏览器、真实供应商账单或研究效果、生产库升级/备份演练。单测复用不冒充新增组合浏览器验证。前序失败和警告均保留，不把最后通过改写为首次成功。
- **下一轮为C研究配置与控制**：短交接在`var/research-main._81s5um0/handoff.md`，先由新可见主`wscyber-research-main-81s5um0`（gpt-6-astra）实际参考并冻结C1资料库/任务选用/附加安全提示词，再接真实API和正式页面；身份/业务前提及完整控制接续后续短轮。原主wscyber-85结束B产品/docs写入，保留用户入口、启动核验和反馈转发；新主必须写accepted并通知后才记录已接收，不把此准备状态当作启动成功。原入口保留期间新主最多再开一个可见执行者，遵守最多额外两个。旧撤销任务不恢复，真实目标/付费产品模型/业务库/部署/外部投递/commit/push边界不变。

### 早期过程（历史记录，不代表上述最终产物或当前会话状态）

- 用户明确“好，继续推进下一步”。继续B组真实账本/原文开关/每日与任务tokens/估算费用，前端每完成可操作部分即展示；不是只写契约，也不表示全平台已获用户验收。
- 已补Langfuse官方两篇完整HTML并实际核对流程/计费原则，证据`var/full-system-usage.hKFpjTHZ/reference-results.json`。cyber只读观察限制保留；没有假称进入用量UI或实际操作Langfuse登录界面。沿用筛选→调用表→就地详情与直接价格编辑，视觉仍用本项目组件库/UI-1。采用/必要偏离和前后端契约见[调用账本](model-call-ledger.md)。
- 实施包`var/full-system-usage.hKFpjTHZ/`。主wscyber-85独占docs/契约/整合，准备两个新可见gpt-6-astra短轮：后端internal/cmd（含新domain/006），前端web（含共享图表先补库/样例）。所有重型命令继续使用A组heavy.lock。A组执行会话均已退出，不复用其PID或延迟通知；新会话接收/源码/验证需另记，不能将准备或启动写成完成。
- 两个新可见会话均已写accepted并消息确认开始实质实现，主已读取接收记录并独立核对PID/starttime/cwd/父链：后端CLI857034/shell857033/终端857004，前端CLI857035/shell857032/终端857007，显式gpt-6-astra。证据execution-identity.json；源码/测试尚未交回，不把接收当交付。主提供两协议tokens/定点费用及DST的独立fixture预期，不作为产品测试通过证明。
- 后端首个正式B产物`var/full-system-usage.hKFpjTHZ/wscyber-b-preview-01`已交，主核对SHA256为`65e52ee3893f282598d67c7a35022f0320fc8d592c00f3de93755f0db5dfec4b`，并读取`runtime-01/result.json`：6组真实程序基本检查、7次loopback调用、failures为空，覆盖managed/environment/test同账本、双协议usage、可选加密原文、价格快照、归属与缺钥回放；专项PG/边界/race仍在执行，尚非最终验收。前端已实现用量/价格/项目调用入口及共享日图，正进行首次类型与生命周期检查，未交正式展示产物。
- 首个正式React快照`frontend-checks-01/react-snapshot`已交，主核对index哈希和成功构建日志，保留约1.23MB JS包警告；执行者报告类型与既有119项React测试通过，新B专项继续。主在`var/usage-preview.44W3wfjc/`运行新PG/双协议loopback：4次模型、2次目标请求，560输入/100输出/660总tokens、USD0.001724及任务归属/原文开关全部符合独立预期。浏览器在“日图键盘与精确表格”复合检查阶段超时，未标展示成功；实际截图显示真实数据、页面/API错误均0，但不足以确定具体失败点。保留失败日志/截图与清理前PG State/log，API正常退出0、浏览器1、容器精确清理。新驱动`var/usage-preview.aM7vVKbS/`改点击可见Radio标签并细分定位，保留原断言，尚未启动。
- 主边界审读指出原文脱敏对重复JSON键可能保留被后一个键覆盖的已知凭据，已交后端在其归属内修复/回归，尚未验证。前端另修日图表格切回后的ResizeObserver重订阅，以及同值新数组不应清掉键盘选中态；这些源码修正与首轮超时原因分开记录，待自有浏览器验证。展示只用本地fixture，不使用真实连接凭据；展示不代替专项或用户认可。
- 修正版`frontend-checks-01/react-snapshot-02`已交，主核对index SHA256为`ae6501ee4cc78122dc995083c4501211c432f765ab2b5d6723dda2a63b95c325`及`build-react-03.log`成功结果。新增128项测试由前端报告通过；随后测试类型声明兼容问题导致02构建失败，修改谓词后03成功，失败日志保留。主另读取`frontend-runtime-02/browser-result.json`并实际查看1440浅色、390暗色与价格模态截图：真实四条算术、日图键盘/切换重排、六组浅暗尺寸、显式原文等检查通过，但整个run为passed=false，因模态定位只取得2/3个框而停在价格保存前（pricePUT=0）。前端正改用实际role=dialog保留实框/底栏/最后字段可达断言，未视作完整验收。其runtime-01在API初始化阶段退出，未进入浏览器，现有日志不足以定根因；不能用02成功反证。主下一次可见展示等待已确认脱敏缺陷修复后的正式后端，不交付已知缺陷版本；现有A组入口不动。
- 后端已交重复JSON键脱敏修正版`wscyber-b-preview-02`，主核对SHA256为`3e60ab2b8a5d35bdbfe989b8978ebf33f9230043cb1bc460f086092d80ad72a6`及`runtime-02/result.json`：7组真实程序检查、8次loopback请求、failures为空；异常重复键回显凭据被移除且业务标记保留，临时容器已清理。专项PG/日期边界/race仍继续，非最终交回。
- 主第二轮`var/usage-preview.aM7vVKbS/`已用修正版后端及React02运行：API算术/归属/原文通过；浏览器执行完日图、按需原文、实际单价2→3保存与新GET核对旧价仍2/总费用不变、项目用量后，在最终汇总读取完整导航前保存的Playwright Response时失败（`browser.mjs:151`）。页面/API错误均0；未写ready，不宣称可操作入口交付。主已查看全局及项目截图，核对API正常退出0、驱动1、精确容器清理。新目录`var/usage-preview.aRWP2445/`改在返回全局时立即解析新响应，保留全部业务断言及前两轮失败证据，准备第三轮展示。
- **第三轮阶段展示已就绪：`http://127.0.0.1:41409/usage`**。`var/usage-preview.aRWP2445/`使用后端02及React03固定快照（index SHA256 `9feb22bdfbcb599d1dc2b98b0672380a6786003d1c0cee12026d33e035b4cc07`），主核对成功构建日志、全部静态资产/二进制哈希、三进程身份、精确PG容器label及真实鉴权GET；浏览器五组基本流程通过，page_errors/failed_apis均0，实际最大化2554×1261无整页横溢出，主已查看最终截图。输入560/输出100/总660、USD0.001724均符合独立fixture。READY后停止自动导航，保留用户窗口及旧A入口。该产物只作当前UTC阶段展示：后端另确认02存在Apia整日跳过/午夜DST历史日期边界问题，修正及最终产物仍在专项中；不冒充完整B验收或真实供应商账单。
- 收尾专项仍在进行：后端candidate-05已通过13组正式程序/故障检查及带独立PG的`go test ./...`；主读取`runtime-05/result.json`并核对PG日志151个顶层/538条含子测试PASS、无FAIL，唯一SKIP为需单独Docker opt-in的既有沙箱专项。旧升级夹具三项首轮失败保留，修夹具后通过；特殊时区日期已改有界日历遍历。主指出长重复凭据脱敏热点后，后端改KMP并增加合法8192字节凭据/1MiB响应专项，主核对记录耗时28.44ms（非容量/SLA承诺）。正式故障覆盖begin/finish持久失败、SIGKILL、失锁、错钥、恢复不重放及明文sentinel检查，精确容器已清理。后端另发现有效Decision可把回显连接凭据持久化到语义记录；已共同收敛为拒绝整次Decision并保留usage，不删改动作后继续派发，契约已补，后续固定产物及零派发/race回归尚未最终交回。前端`frontend-runtime-06`（React03/后端02）九组通过，主已读取结果/清理记录并实际查看390价格模态和长名称/大整数截图：真实价格保存、UI创建原文任务、29条25+4游标往返；另以明确故障注入验证原文迟到/401、价格409/失联后按原ID只GET核对、跨项目404/局部503，以传输fixture验证缺失/截断/HTML只读字节/多币种与精度。pageErrors/consoleErrors为空，local/session storage为空，API退出0、精确容器已清理。03/04/05驱动失败仍保留，不归因产品。共享图直标固定顶部问题经主截图发现、前端确认；端点邻近/防碰撞修正及新增图表/迟到PUT专项待最终固定产物交回，不自动替换当前预览二进制。
- 前端04固定快照index SHA256 `318a5d930b568491d98a177f935dc40fb4c16e64a8dec3ce69fd8c28c28e848f`，主已核对`final-builds-01.log`中129/129测试（0失败/0跳过）、共享契约及三入口类型/构建成功，保留Descriptions和大包警告。`frontend-runtime-08`以04/后端02完成十组，主核对结果/清理，新增真实PUT已成功但离页后才收到回执时按原ID显式GET核对、无重发。图专项07因驱动未及时await响应而未写完整结果；09在极大值阶段把轴最大值预期误写1.0e19而失败，实际1.2e19符合有界向上取整，主已看失败图；均不视作整轮通过。修正驱动后的10仍在运行，04尚未发布到用户预览。
- 原文默认关闭，固定任务；usage独立记录，未知不按零。价格为明确手工单价的估算，调用开始固定快照，历史不重算、多币种不相加。持久失败阻止外发/后续派发，unknown不重放，兼容managed/environment/test三条路径。
- 保留A组51013预览及用户草稿，不就地迁业务库、不强刷新或关闭旧窗口。旧37529故障根因仍未明；B组只新临时PG/loopback验证，无真实产品模型/外部目标/部署/commit/push。

## 0.21 已交回：A组模型设置与实际研究接入（2026-11-12）

- 用户在整体设计缺口盘点后明确要求“继续继续，正确快速完成整个完整系统构建，不只是停留在最小契约”。本次恢复覆盖旧暂停，目标为已确认Web/API首版完整链路；完整清单见system-delivery，不只交接口/模板，也不将延期SDL/全工具强制出口/多租户等擅自塞入首版。
- 主入口`wscyber-85`保留，主独占共享docs/实施契约与整合验收。A组任务目录`var/full-system-models.Pg2PL1Id/`；后端独占internal/cmd，前端独占web（含共享组件和展示）。均使用可见`gpt-6-astra`CLI，最多两个；重型命令用目录内heavy.lock串行。
- A组计划实现直接字段编辑/条目增改删、真正默认与任务模型选择、原子模型绑定、动态能力、两协议研究适配；不再暴露配置版本或秘密模式。参考沿用ui-reference中实际观察证据；先补共享键值编辑组件/用例/coverage，再接业务。
- 两个可见执行会话均已消息确认并写accepted，主已读接收记录：后端CLI590929/父shell590926/终端590854，前端CLI590928/父shell590927/终端590855，均报告显式gpt-6-astra并开始实质实现；进程身份为接收记录，尚非主独立进程核验。源码/验证尚未交回，不把启动当完成。旧草案仍撤销，旧用户窗口/预览不关闭或重置。
- 用户进一步强调不能为了快交付低质系统，前端外观、视觉和功能均须企业级。已同步两执行者并写入system-delivery质量门槛：真实业务闭环、组件一致性与状态/可访问性、授权/秘密/并发/恢复、有界规模、可运行产物及主独立整合验收；不以编译/单测代替端到端、展示或用户认可。主并行维护独立构建配方与运行入口，不写执行者独占产品路径、不切现有服务。
- 后端已完成并交回`backend-outcome.md`，停写后归属转回主。主已核对最终六步Go/真实PG/集成/race/构建结果与正式程序13组受控端到端结果、成功二进制哈希；后者17次loopback供应商/8次目标请求、7次正式进程启动，failures=[]。覆盖双协议研究→HTTP证据→收尾、默认/显式绑定、在途改配置/默认后pause→重启→run仍用原绑定、禁用/缺密钥不回退、unknown/SIGKILL不重放、429及旧环境兼容。主发现的run静态能力回归已修并实测；这不代替前端/全平台/真实模型效果验收。首次超时fixture和首次runtime误读body字段的失败均保留，修fixture后重跑通过；临时PG/应用清理结果已核对。managed超时为调用上下文、部署Engine上限、绑定配置TimeoutSeconds中最短期限。
- 前端已先实现共享SecretEntriesEditor/样例/coverage，再接PATCH、默认选择和任务绑定。主发现默认写入未抵达服务时永久锁在待核对，已补用户显式按当前读取继续与代次隔离。02轮正式类型发生TS2339，失败日志保留；修正后03轮类型、119项React测试（0失败/0skip）、正式构建成功，主已核对日志/计数。共享测试、两展示入口类型和19项设计静态契约已报告通过，完整浏览器验收仍未交回。构建存在JS单包约1196kB/gzip384kB警告，未以调高阈值掩盖；后续须检查实际性能并处理必要拆包。
- 用户新增要求前端每完成一部分就及时浏览器更新展示，不等整组/全系统全部结束。主已用03轮新正式构建＋真实Go＋新tmpfs PG＋loopback供应商/目标打开独立最大化可见预览：`http://127.0.0.1:37529/settings/models`。归属与证据在`var/full-system-preview.8yb77p6e/`；主已实际查看编辑表单/创建选模截图，完成真实PATCH普通保存（秘密不重填）、浅暗切换/导航，并通过真实研究→一次HTTP→证据→收尾基本检查。1914×792无横向溢出，page_errors/failed_apis为0。保留用户页面供查看，完整专项在独立context进行，不强刷草稿、不关旧窗口；展示不等于完整验收/用户认可。首次展示驱动因含图标按钮exact选择器失败，证据保留于`var/full-system-preview.yDOpI8Sx/`，其资源已清理；仅修正驱动后新实例成功，不将失败掩盖。
- **上述37529预览随后异常停止，目前不可用。** `api.log`记录05:21:18连通测试服务关闭失败，API exit1先于监督器清理浏览器/PG；不能归因于用户关窗。原监督器未在清理前保存PG诊断，现有证据不足以确认根因。主正诊断实际退出路径、增加故障留证并分离浏览器与服务生命周期；初次展示成功不等于持续运行可靠。前端已转自有独立浏览器/fixture专项，旧CDP不再使用；未操作用户其他窗口。
- 主已调整Makefile、.dockerignore、Dockerfile、.env.example，使正式构建/打包/新配置指向dist-react，旧Vue仅显式web-prototype。新增test-web入口并覆盖已有模型单元包。15项静态构建链核对通过，实际构建/镜像运行/服务切换未执行；证据packaging-check.json，同时独立核对两CLI的项目cwd、PID/starttime与父链。接收记录不再是唯一进程证据。
- 前端最终`frontend-outcome.md`已交回停写，主核对30项源码/产物哈希全部一致。独立真实API浏览器9＋共享消费者3＋恢复6＋边界4，共22组通过；主核对结果摘要/清理，实际查看1920浅色、390暗色表单及共享窄屏错误截图。故障注入与真实服务结果严格分开；1920/390是headless视口，不冒充物理最大化。前端自身API/监督器正常结束，6次loopback模型/2次目标调用，容器精确清理；主请求PG留证抵达时已删除，未取得该证据，不补造。主核对空闲画面、PID/starttime/独占窗口后正常`/exit`，退出0，590855/590927/590928均已结束；web归属回主，用户主入口保留。
- 新独立展示已恢复：**`http://127.0.0.1:51013/settings/models`**，归属`var/full-system-preview.i5AGy4ZF/`。保持A组已验收Go/React快照、新PG/本机fixture；真实保存、证据链、2554×1261物理最大化基本交互及180秒持续API读取通过，主已查看本轮截图。服务不因关浏览器退出；API异常先保存PG日志/非秘密State，保留资源、不自动重启或重放。**旧预览退出根因仍未确认，新实例通过不等于已修复根因或长期可靠性验收。** 主已补脱敏失败阶段、保留首个故障和退出收尾错误、避免重复输出同一错误；modelprobe/httpapi专项race及正式构建通过。新隔离PG中真实终止单实例锁连接，正式程序按原fail-stop退出1并保留领取/收尾阶段；随后重启/正常关闭退出0，凭据不入日志、无供应商/目标调用，资源精确清理。这是诊断能力验证，不是旧故障的根因复现。预览不自动升级二进制或刷新用户草稿；最新只读复核已持续636秒，进程身份匹配、API200、PG运行中，不能据此排除旧故障。主另补1440/1920/390浅暗六组弹窗真实框/固定底栏/最后参数可达，无缺框/横溢出/页面错误，已查看1440暗色截图。所有补充证据在`recovery-diagnostics/`。
- 受控本地fixture/隔离临时PG用于工程验证；不访问真实目标、不追加真实供应商、不操作业务库/部署/外部投递/commit/push。A组交付后继续账本/研究配置/发现验证/报告复测/整体验收，不重复笼统开工询问。

## 0.20 历史：开发暂停，整理项目组件库优先的现行规范（2026-11-12）

- 用户在进度汇总后要求开工，随后明确质疑“已有组件库为什么又按Ant Design”，要求停止并查看当前开发规范。**本轮只整理文档；模型页、研究绑定、usage及后续功能均不继续实施。**
- 新任务草案`var/session-round.model-settings-correction.tm4h2ToY/task.md`从未派发、未启动执行CLI，已在文件顶部撤销。其新增PATCH、Header/参数条目编辑、兼容和存储方案没有实施，也不是已批准契约；不得凭草案恢复。主此前只核对代码/参考并写草案，没有产品修改、构建、测试或新预览。旧执行CLI270874及其shell270873/终端270814经此次核对不存在；旧预览在线状态未核验，未关闭或重置用户窗口。
- 问题在于任务没有先列现有组件库的复用/缺口及补库计划，却先规定页面专用方案并禁止修改共享UI；这与先补库再用于业务的规则冲突。不能把Ant底层依赖当作重新拼页面的设计依据，也不能把“统一主题”当成所有复杂组合都已复用。
- 已新增用户审阅入口[当前开发规范总览](development-standards.md)，同步AGENTS、UI-1、组件库、前端设计、会话规范和README入口：**项目组件库/视觉基线负责一致性，cyber等参考用户流程，Ant文档只负责底层API。** 基础控件可沿已有标准用例直接使用；缺少组合先补共享实现、展示和覆盖，再接业务。
- 只读核对确认共享库实际包含UIProvider/token、AppShell、TaskModal、复制/折叠/短文字及字节查看器；覆盖矩阵六类41项不等于41个共享业务组件全完成。完整业务组合仍有仅展示/待补，模板旧版本交互不能直接沿用；不宣称此次已完成全项目UI审查或用户视觉认可。
- 本次只有文档/撤销标记与静态核对，不运行产品测试、浏览器、模型、数据库或服务；不修改组件实现/构建产物，不部署、业务库、commit/push。等待用户审阅规范及明确恢复后，重新确定开发切片，不能沿历史自动接续授权开工。

## 0.19 历史：用户否定旧交互，MC-1C-UI暂停；全项目先参考再设计（2026-11-12）

- **最新要求覆盖所有功能，不仅模型设置：优先实际参考、模仿已有优秀设计，以用户指定的 <https://cyber.chainreactors.cn/tasks> 为首要功能/流程/字段组织参考，不能只取字号和布局后自行猜交互。** 无对应或无法观察时核对成熟项目同类设计，任务包记录观察范围、沿用点和必要偏离，重大差异先沟通；技术测试不能替代设计依据或用户认可。规则已同步AGENTS、前端规范和会话分派要求，观察见[界面参考](ui-reference.md)。
- 用户明确否定配置v1/v2和“保留/替换/清除”选项：保存就是更新，API key按必填密码字段处理；已保存值不回显，改其他字段不要求重填。此要求不是仅隐藏标签，还需审视后台历史版本/秘密更新协议的实际必要性。有限源码初查还定位到资料选择/资料库、发现与报告模板中的固定版本/revision交互，均纳入先参考再设计，不只修模型页；模板不等于真实功能已实现。**现有后端契约仍是代码事实，不是新版设计已获认可；尚未修改存储或实施新方案，也未完成全项目交互验收。**
- 执行者`wscyber-model-settings-u6q0g4hy`已确认停写：收到暂停时仅只读核对，无在途测试/写入；此后不改源码、不新增测试/截图、不驱动用户可见浏览器，不退出CLI或清理预览。继续保留用户主入口6658及执行CLI270874/starttime640460、shell270873、QTerminal270814；身份为启动核验记录，不冒称本刻重新检查。共享docs仍由主维护；不恢复旧task.md或自动进入研究绑定/usage/发现。
- 目录`var/session-round.model-settings-ui.u6q0g4hy/`中的旧任务按A/B2契约实现了正式React设置页，但未先实际核对同类设置交互，主的任务设计有误。执行者报告改动限于正式React十个源码/测试路径及`web/dist-react`，未改Go/依赖/共享UI/docs/业务配置。**当前代码未最终交付、未主核验、未获用户认可。**
- 阶段验证仅为执行者报告：107/107后又新增history身份恢复测试，未重跑，不称108通过；typecheck/build03成功。browser07前10组通过后因两个“关闭”按钮选择器冲突停止，修正驱动后browser08接续5组通过，不称一轮完整最终验收。pending/running/not_sent及显式logout专项未补测；最终源码SHA/outcome/results/完整进程归属汇总未完成。早期静态断言误匹配CSS类已修正，既有chunk/Descriptions警告保留。
- 保留旧设计的隔离预览 <http://127.0.0.1:49549/settings/models>，不是修正后的交付。有效归属为`preview-02/preview-ownership.json`，主已读取；根目录同名文件是第一预览的过期记录，不能用于清理当前资源。记录supervisor357566/starttime843460/PPID1、API358506/starttime844485、browser_driver358539/starttime844552；端口API/provider/PG/CDP为49549/39359/32770/52289，容器label为`wscyber.preview=model-settings-u6q0g4hy-preview-02`、tmpfs且无卷挂载。**归属文件记录初始桌面viewport，但执行者最后截图仍390px，未恢复桌面viewport；当前Chromium父链/实时资源状态尚未由主独立核验。** 关闭预览浏览器会触发监督器清理，暂不关闭或重置用户页面。
- 预览复用正式B2二进制、正式React快照、新tmpfs PG和loopback供应商；执行者记录6次本地最小fixture请求，无真实网关调用、无研究worker模型能力。不读取用户真实key或追加网关调用；未部署、连接业务库、commit/push。
- 本次参考核对在`var/model-config-design-review.ewnqvnjo/`：代理首次未加载，直连200；初次观察只到Models入口，后续通过可见导航实际进入模型页，再打开Add Provider居中弹窗，取得Provider/Name/API Key密码框/Add Model/测试及Save/Cancel的安全控件结构；不再仅有密度参考。新增按钮匹配曾因大小写差异失败，修正后成功，非认证失败；未见401/403或登录跳转。未填写、保存、测试、打开已有记录或读取秘密值；非GET/HEAD等请求全部拦截，无参考截图。不能从本次未见版本/秘密模式推断后台没有历史机制，也未验证其必填/保存规则。Open WebUI官方兼容连接文档部分正文已取得并解析，含URL/API Key/模型ID、显式Verify Connection、Save和启停；下载截断，Dify仍仅搜索摘要。实际沿用点、必要差异和未观察范围已写[界面参考](ui-reference.md)，新方案未实施，不沿旧设计扩功能。

## 0.18 已交付：MC-1B2持久连通测试（2026-11-12）

- `POST/GET /api/model-configs/{id}/tests/{testID}`已接入正式程序，追加v4持久身份与精确配置版本。显式确认可能计费才受理；同ID同身份回原记录，不重发。事务内先核对历史身份，再检查配置当前版本/启用/本地解密及容量；队列容量8包含运行中1项，单worker，固定最小内容与至多32输出。启动恢复未完成为unknown，不自动重放；终态不被迟到结果覆盖。输入/回执/错误及前端注意事项见[API](api.md)。
- **最终单元3包16个顶层测试、PG17个顶层测试（含B2九项）、四包专项race通过；最终正式程序11组验收通过。** 八并发同ID为1个202＋7个200，仅一次供应商请求；31.27秒供应商等待时POST约5.9ms返回。正常退出、SIGKILL恢复、缺密钥历史回放、事务/最终写回失败、禁用与受理并发均有受控fixture验证。确定性channel回归验证首次查无记录后并发受理/改版禁用仍优先回原回执，不靠时序碰巧通过。
- 主审读执行服务、事务、HTTP输入/回执、迁移、恢复和正式生命周期；独立核对16产品文件、78未改边界文件、最终日志计数及产物SHA。最终二进制`c8336ad58af646c632645d0b8028521f17cdea65c4a18212e75d9ee3a4d8c1c1`；未重复整套测试。首轮文件命名/fixture错误、最终准备时Docker端口核验错误及修正日志保留；首轮完整源码SHA未采集，不补造。主首次manifest核对误比外层对象，改取`changed_product_files`后匹配，无源码变更。证据在`var/session-round.model-probe.Jvlr7a99/`的`outcome.md/results.json/main-review.json`。
- 执行者交回停写后，主重新核对CLI124013/starttime297487、无子进程、专用QTerminal123972及真实空闲窗口，正常`/exit`。退出码0，CLI/shell124012/QTerminal123972均不存在，后台启动任务亦exit0；见`executor-exit-check.json`。本轮精确tmpfs容器、13个记录程序和17端口已清理，主独立检查本轮label无容器、记录身份不存活及端口无监听。用户主入口`wscyber-35`/6658保持，主独占共享docs。
- 未改B1、旧研究默认、前端或依赖，未部署/操作业务库/commit/push。**研究任务绑定、正式设置页、usage/费用未交付。** 按已有自动接续授权，下一短轮先将A/B2接入真实模型设置页供用户操作，再做任务绑定；没有任务绑定前不开放“研究默认已生效”的假控件。只用本地fixture，不追加真实供应商调用。
- 用户提供真实网关后，主先进行§0.17所述一次请求，返回404。用户进一步明确所给地址是Base URL、应自行追加协议后缀；主据此补全OpenAI标准`/v1/chat/completions`做一次复测，**HTTP200/valid_response，约6.6秒**。共两次明确触发的最小请求，无自动重试/跨协议回退，不含项目材料；本次成功仅证明指定配置得到有效文本，不证明研究效果或完整B2链路。两次临时凭据文件均已删除，最新非秘密配置/结果留在`var/model-live-v1.rnejhu02/`，不把密钥写入源码/docs。执行者仍只用本地fixture，不读取凭据或追加真实调用；新建资源限定本轮精确归属，不操作业务服务/数据、旧预览、其他项目，不部署或commit/push。

## 0.17 已交付：B1协议组件与中断恢复（2026-11-12）

- 用户报告此前中断、疑似卡死并要求继续。确认旧开发CLI1881792不存在，系统于2026-11-12重启；与本轮时段对应的内核日志未检出OOM/lockup，**中断原因仍未确认**。从磁盘MC-1A与未验收的B1代码恢复，不复用旧PID、不自动重放外部动作、不例行监控内存。
- B1完成8个产品路径：`internal/modelprovider`的协议、传输、严格响应检查及专项测试，`go.mod`将既有Anthropic v2.9.0归为直接依赖；OpenAI仍v3.98.0，go.sum不变。显式认证、不继承SDK环境凭据/Header/代理，固定端点、无重定向/自动重试，HTTP/1.1单次新连接；256KiB上下文/2MiB序列化请求体发送前拒绝，1MiB响应读中断保守unknown，32KiB返回文本/响应头限制。SDK宽松解析由严格JSON及公开字段元数据补齐，不接收工具结果为正常文本。
- **provider17＋config5顶层测试通过（含子测试212条PASS），两包专项race通过；旧model/research17顶层回归通过（含子测试48条PASS），正式cmd编译、go mod verify通过。** 最终日志无FAIL/SKIP，主审读关键实现并独立核对8文件/产物SHA、日志计数与范围，无越界产品改动；未重复整轮测试。基线Anthropic/api_key失败、22个畸形响应误判成功及两项取消测试同步假设失败均保留原日志，修正后复验。未跑全仓/PG/前端套餐，正式二进制仅编译未启动；本地HTTPS fixture不代表真实供应商兼容性。
- 交付记录`var/recovery-mc1b.pSo8Zdkx/`。执行CLI34178/starttime85746交回停写后，主核验实际空闲、无子进程及专用窗口，正常`/exit`，退出码0；CLI/shell34177/QTerminal34168均不存在。用户入口`wscyber-35`保持，未被退出；主继续独占共享docs。下一短轮B2契约与任务包在`var/session-round.model-probe.Jvlr7a99/`，仅持久测试身份/显式确认API/有界执行与恢复，后续才C任务绑定和设置页。
- 用户随后提供指定网关/模型及凭据，明确授权连通测试。主用B1组件单次发送固定最小内容，20秒超时/32输出上限，实际约1.7秒收到**HTTP404**；未自动补路径或重试，不能确认认证/模型可用，也不推断未计费。该一次性适配器验证不是尚未实现的B2产品回执，结果仅存本项目受限临时目录，不在文档/源码保存密钥或复制远端正文。此授权不扩为真实研究/任意供应商测试。
- **B2显式测试API、任务版本绑定、设置页及usage/费用仍未完成。** 历史预览35983/45649重启后尚未核验在线，不能称仍在展示；未重启业务服务、连接业务库、访问外部安全目标、部署或commit/push。正式设置页完成后再实际展示，不以本轮后端验证冒充界面交付。

## 0.16 历史后端增量：MC-1A配置持久化与秘密边界（2026-11-11）

- 已新增模型配置领域类型、整体AES-256-GCM加密、显式keep/replace/clear、严格连接/Header/参数校验、v3不可变配置版本迁移、比较交换保存与默认选择，以及5个受保护管理API。主密钥仅由显式环境配置提供；缺少则管理不可用，不导入开发CLI或其他应用凭据。保存和选择不调用供应商。
- 专项`modelconfig/config/httpapi/model/research`单元回归通过，正式主入口编译通过；独立tmpfs PostgreSQL的9项可靠启动/迁移/模型配置回归通过，覆盖5并发保存仅1成功、历史不可改、v2→v3保留研究及回执、注入版本写失败回滚与真实TCP socket管理API。该HTTP用例由httptest启动；随后重新构建正式cmd二进制，在另一独立tmpfs PG启动并实测鉴权、保存/脱敏读取、CAS冲突、默认选择、错误主密钥拒绝启动及正确密钥重启读取，8项检查通过，两次正常退出，模型fixture命中0。后加HTTP不可用测试所在包也已单独重跑通过；日志和结果见同轮`runtime-results.json/httpapi-final.log`，临时容器已精确清理。
- **MC-1整体未完成**：配置管理是后端增量，没有设置页、显式连通测试或新配置到研究任务的版本绑定；新默认选择暂不改变现有启动环境模型，`/api/system`仍反映旧路径。下一步继续官方协议适配/本地连通fixture→任务绑定/正式设置页，不开放假导航。usage/费用/发现仍未实现。
- 当前可见开发负责人1881792继续唯一写源码/docs，用户交互/协调入口保持；已收束UI小清理，不再布局微调、空交接或例行查内存。保留35983/45649预览。契约与验证目录`var/session-round.model-config.n4hyjnhd/`；不调用真实供应商/外部安全目标、不部署/业务库/commit/push。

## 0.15 历史：UI-D1展示后最小清理，继续实质模型功能（2026-11-11）

- **用户最新反馈**：“先这样继续下一步”，因功能较少目前难以充分评价；明确不要常驻小字辅助说明，也不得换字号/换位置继续呈现。UI暂收束，优先MC-1实质功能，不再连续布局微调，不能将“先这样”写成完整UI认可。已删除列表的冗余已知信息行、HTTP列表说明及项目/创建的常驻HTTP/bridge段；真实背景在项目详情保留、无背景不补占位句，授权范围输入和实际校验错误保留。只在新预览已核验tmpfs PG清空一条开发占位Mission.origin，未修改证据或授权数据。能力事实仍在开发/API文档。
- 清理后的React **91/91**、共享原10组＋UI-D1、正式类型与构建通过；最终JS为`react-DhHOpV5f.js`（1,135.42kB），CSS仍`react-TK23GfIU.css`，大chunk警告保留。新预览先加入hash资产后原子更新index，保留旧hash避免在途页面缺资源；8个当前产物SHA与dist一致。清理后独立浏览器5项通过：列表/研究/创建/HTTP无指定辅助说明、非法Origin实际触发错误；pageerror为空，页面写请求/外发0，目标命中保持33。已查看创建、HTTP及用户可见原生窗口截图，后者已登录并正在查看证据，不重置其页面。该次未采集完整console流。下文7组/18布局是此前密度验收，不冒充清理后全量重跑；旧`browser.mjs`为历史驱动，最终清理以`copy-cleanup-check.mjs`为准。

- 用户在MC-1核验期间新增UI反馈：实际最大化浏览器、字号缩小、减少纵向留白、短状态/时间/操作同行；同时指定`https://cyber.chainreactors.cn/tasks`为参考并授权登录态只读观察。MC-1无在途产品写入，主安全插入UI-D1并串行完成，无执行子会话；模型配置尚未实现，不能把本UI切片算模型功能交付。
- 已实际观察参考任务列表：1914×765视口、248px左导航、主区占剩余宽度、紧凑任务行及短元信息同行。独立落实到本项目正式React/官方Ant中，采用14px而非照搬参考13px；不复制品牌/业务数据，不新增假入口。参考详情尚未可靠观察，后续页面未就绪不能断言Cookie过期；登录态边界、失败处理与观察限制见[界面参考](ui-reference.md)。
- 单一共享Provider更新正文/表格/表单/状态/提示14px，标题16/20/22px，桌面控件36/32px、触屏主要按钮40px；正式侧栏208/64px、顶栏52px，间距和Card/Form/Table/Tabs收紧。列表刷新合入顶栏、HTTP状态/响应横排、方向更新时间与观察原文操作同行。规范、演示兼容样式和旧字号静态断言同步；历史16px验收保留历史身份。未改业务API、数据归属、提交/授权/unknown或敏感证据语义。
- **React90/90、共享原10组＋UI-D1新静态契约、设计19组通过**；三个入口类型检查及正式构建通过。产物`react-TK23GfIU.css / react-BveQaGY6.js`（JS 1,136.20kB），保留>500kB chunk警告。未新增依赖、改lock/go.mod/go.sum或重建后端；组件/设计产物未重新构建，也未重跑其全浏览器矩阵。
- **真实Go API/新tmpfs PG/loopback HTTP＋可见Chromium7组通过**：1440/1920/390浅暗HTTP/Drawer/Modal18次布局，无全页横溢出或小于14px文字；草稿保留/清空、正反Tab、Escape/来源焦点及观察原文展开收起实际操作。页面写入/外发/持久存储0，本地目标命中保持33，验收runtime/console错误警告及异常响应为空。无研究worker或真实产品模型，不据此证明研究质量；未重跑TF-1完整矩阵/Go/PG测试套餐。
- 同1440视口，项目行高115.19→89px、列表起点150→80px，HTTP行高93.59→56px；旧窗口宽1448而屏幕宽1914且固定1440viewport，确有窗口外右侧空位。新预览使用`viewport:null`并实际最大化：窗口1914×852、viewport1914×765，工作面右侧仅12px正常边距。已Read列表/研究/创建/HTTP及390px浅色Modal、暗色Drawer截图。**尚无用户视觉认可。**
- 新入口 **`http://127.0.0.1:45649`** 保留供评审；自动验收结束停在项目列表，随后真实窗口截图已显示HTTP页，不重置用户查看状态。旧35983未关闭/重载。目录`var/session-round.ui-density.1o8tpcvf/`保存契约、基线、测试日志、布局结果、截图及快照8文件SHA；supervisor2023652/starttime38050770/PPID1、API2024114、Node2024240、Chromium2024287，目标44277、PG32806。容器`wscyber-ui-density-1o8tpcvf`，精确ID/label/父链/清理规则见`preview-ownership.json`；tmpfs且无持久挂载，不依赖主CLI生命周期。
- 首次预览PG仅Unix socket临时就绪，API连接被重置；改TCP就绪后解决。第二次浏览器图标按钮精确名称断言失败，按实际可访问名称修正驱动后完整通过；失败日志保留于`launch-01/02`，两次临时容器已精确清理。没有修改产品逻辑或降低旧测试来迎合验收。
- 当前唯一共享写入责任仍属`wscyber-model-main-62sdpd6z`（CLI1881792），在现主继续MC-1：成熟组件/最小契约→全局配置/显式测试/真实研究接入，随后才usage/费用和发现，不做空交接。用户最新要求“不要求时刻监控内存，没出问题就接着运行”，覆盖旧例行检查及静态2GiB/1GiB暂停门槛；保留最多额外两个可见会话、避免不必要重型并行，实际异常再诊断。
- 旧CLI1688828曾按旧换轮约定经最终可见空闲、精确身份/焦点核验后正常`/exit`，共享402204/402187/未知402485均保留；但**未可靠转交用户交互入口，退出导致用户中断，是主判断错误**，不是合格的交互接管。用户已在原入口恢复新CLI2052763/`wscyber-59`继续反馈；对原入口所有自动退出安排取消，不复用旧窗口号。原入口保留、源码仍由1881792独占，最新流程已修正于[开发会话约定](development-sessions.md)。
- 未部署、连接业务库、调用真实产品模型/外部安全目标、修改harness/Arena或commit/push。只读参考站访问不扩大为安全测试、任务执行、付费模型或写配置授权。

## 0.14 历史授权：已展示正式前端，获准自动逐轮推进模型配置与用量（2026-11-11）

- TF-1交付后，用户明确要求“做完一轮自动接着下一轮”，并要求把已实现前端展示出来评审。接续方向为全局模型配置/显式连通测试/研究接入 → 调用与usage账本 → 每日/任务用量与费用 → 发现与验证。在已确认方向内逐轮冻结、实现、验收并用新可见会话交接，不再每轮重复笼统开工确认；不是同时展开全路线，也不授权真实产品模型/外部目标、部署、业务库升级、外部投递或commit/push。
- 已用**正式React产物快照 + 真实Go API + 一次性PG + loopback HTTP**打开可见Chromium供用户操作，入口 `http://127.0.0.1:35983`。实际打开项目列表、研究、新建探索、HTTP记录和证据Drawer，保存五张截图，主已Read研究/创建/HTTP/证据四张；当前停在HTTP记录，保留窗口。数据明确为隔离fixture，无模型/自动研究worker；不是设计模板或真实研究。尚无用户视觉认可，不将展示当认可。
- 展示目录 `var/presentation-and-handoff.62sdpd6z/`：正式dist复制前核对8文件SHA一致，独立`web-snapshot`避免后续构建影响正在看的页面；Go fixture二进制复用TF-1已验收版本，本阶段未重建。`presentation.log`读取时仅READY，未采集到pageerror/console error或warning；本阶段不是重复TF-1全矩阵验收。
- 保留资源归属在`preview-ownership.json`：supervisor1854911（starttime37671331，实际PPID=1）、API1855468、Node1855513、Chromium1855546；API/目标/PG端口35983/32791/32803。PG容器`wscyber-ui-preview-62sdpd6z`，精确ID与label见记录，tmpfs且无持久挂载。用户关闭浏览器后由supervisor自己的trap清理；交接不能关闭用户正在看的窗口，结束/迁移前重核精确身份，不操作未知进程或业务资源。
- 新主`wscyber-model-main-62sdpd6z`已实际只读接收、写`next-main-accepted.md`并消息确认；CLI1881792/starttime37732797 → shell1881786 → 独立QTerminal1881749 → PID1，显式模型`gpt-6-astra`。旧主独立核对身份/父链、实际Read启动与接收交互截图；新主也独立核验预览归属。共享工作区/文档与保留预览的唯一责任以同目录`ownership-transfer.md`及明确交权消息生效，旧主此后停止写入，新主推进MC-1组件/最小契约与实质实现；**交接时模型配置产品代码尚未开始**。
- 旧主CLI1688828仍待新主收到最终空闲、核验无在途工作后正常退出，不将接收说成退出完成。其所在shell402204/终端402187还含未知停止态402485，不能整体关闭；新可见主窗口也不自动替换用户原聊天入口。后续开发交互使用`wscyber-model-main-62sdpd6z`窗口，用户前端预览继续保留。

## 0.13 历史：TF-1 平台 HTTP 记录已验收；模型配置/用量需求已登记（2026-11-11）

- 用户授权继续后，按`var/session-round.traffic.hhDXBPbR/contract.md`完成一个业务切片：**项目 → HTTP 记录**，跨研究方向浏览现有平台HTTP Action，支持方向/状态/方法筛选、25条游标分页、按需详情，以及既有证据预览/复制/原字节下载。只读现有记录，包含未发送动作；不是全工具抓包、独立OutboundRequest或线上原始报文，不新增扫描/审批/重放。
- 后端新增项目级列表/单条详情两个GET，契约见[API](api.md)。SQL白名单摘要投影、keyset及limit+1；严格v1游标绑定项目/筛选。详情同一只读快照核对项目/方向/动作及HTTP类型，不加载其他项目正文，不复制证据。未改schema/迁移、策略、执行器、研究循环或依赖。
- 正式React复用统一主题和原证据Drawer；新读取链串行、可取消，切记录/筛选/分页/项目/页签、创建Modal及401撤销旧材料。错误保留已读内容并说明未更新，403/404停止自动轮询；详情错误在Drawer内显示。研究选择/内存草稿、敏感头复位、来源焦点及原字节语义保持。URL/正文/错误仍可能含敏感值，不宣称通用脱敏。
- 一个真实可见`gpt-6-astra`执行子会话交回后端7文件，主独立整合、审读并串行验收；交回SHA一致。执行CLI1715907及所属shell/独占终端已核对最终空闲和精确身份后正常`/exit`，退出码0且三者均不存在；未知旧会话及共享主终端未操作。记录见`executor-outcome.md`与`executor-exit-check.json`。
- **React 90/90、正式类型检查和构建通过**（保留原82项，新增8项）。正式资产为`react-Bx2ZylPz.css / react-DDIQ4ORy.js`，JS 1,135.86kB；既有SSR Descriptions列跨度、测试bundle及>500kB正式chunk警告保留。首次89/90暴露Drawer子树结构回归，错误提示移入原keyed材料子树后通过；没有降低旧断言。
- **一次性PostgreSQL真实回归24个顶层入口通过，无SKIP/FAIL**：httpapi 5、store 14、integration 5，覆盖TF-1及必要的可靠创建、归属、恢复/unknown、原子完成与HTTP执行兼容。此前无PG阶段4个用例显式SKIP，随后已在隔离库实跑，不把跳过当成功；未跑全仓/竞态或Docker执行集成套餐。
- **本轮重新构建的真实Go API + 一次性PG + loopback HTTP + 可见Chromium，12组通过**：跨方向33条及25+8分页、组合筛选/空态、按需详情、原字节下载/真实剪贴板、状态变更刷新、错误/迟到响应、跨项目、创建草稿/页签、401与键盘焦点。1440/1920/390浅暗列表/Drawer共12次布局与>=16px检查，已Read最终桌面/窄屏浅暗4张截图。首轮4组后因图标按钮名称精确定位超时，修正测试选择器后完整通过，失败证据保留。
- 页面业务写入/页面外发均为0，本地目标命中始终33，原Action材料未被读取改写；仅测试API客户端明确拒绝一条pending，事件增加1条且未发送目标。最终pageerror/console warning为空，HTTP异常仅注入404/403/401，另注入断线；未采集完整console error流，不称其全部为空。无真实产品模型调用；集成测试使用受控模型，浏览器fixture无研究worker，不能据此证明研究质量。
- 临时API1771752及runner已结束，测试容器按精确ID/label清理，44049/35409/32802均无监听，本轮label无残留；浏览器脚本已正常await关闭。见`cleanup-check.json`，未操作业务服务/卷/来源镜像。源码/文档开工SHA基线163→172，23条新增/修改路径均在范围；忽略的dist/var/下载参考源不属于该基线，正式dist另记录指纹。主报告`outcome-main.md`汇总范围、验证与停止点。
- 用户新增后续核心需求已记入[全局模型配置、用量与计费](model-configuration-and-usage.md)：多供应商/多模型、自定义接口/Header/参数、显式连通测试，以及每日/每任务tokens与费用仪表盘。**仅需求登记，未实现，未插入TF-1。** 未知用量不按零，估算不冒充账单，原文开关不关闭usage统计；秘密、价格版本和任务/日期口径实施前另冻结。
- **本轮收束停止**。未部署/切服务、连接业务库、调用真实产品模型/外部目标或commit/push；正式产物在`web/dist-react`，旧Vue保留。未重跑共享/设计全矩阵、全浏览器/完整读屏、大数据性能或真实发现能力；完整流量捕获、可选模型原文、资料库、Finding/验证、报告/复测等仍未实现，不自动启动下一片。

## 0.12 历史：DR-1后交接完成，有限核对后停止（2026-11-11）

- 新主`wscyber-next-main-iik5seby`（CLI938716→shell938710→qterminal938703→PID1，gpt-6-astra）已实际读包、记录身份并获879569明确交回唯一共享写入。接收阶段只读及写交接目录，没有源码/docs写入、执行派发或重测试。
- 等旧主因晚到通知完成最后短回复后，Read最终完整空闲画面并独立核对PID/starttime/cwd、无其他后代及独占父链；精确窗口/焦点核验后正常`/exit`。879569/879563/879555及窗口均不存在，旧启动exit.txt=0；退出核验时自动idle尚未收到，不以消息投递当空闲。未知402485/402204/402187未操作。证据在`var/main-handoff.after-dr1.iik5seby/accepted.md`及`new-main-exit-check.json`。
- 旧主关闭后，有限静态核对现有domain、api第1–6节相关边界、正式API客户端与研究工作面；未取得足以冻结一个必要小片的具体问题证据，**本轮停止，不开下一片**。不为继续开发润色文案、增加阅读控件或扩展后端/依赖/控制/审批/完整流量等范围；不把有限核对当全仓无缺陷结论。记录见同目录`new-main-scope-decision.md`。
- 本轮仅更新本状态及交接证据，产品源码、产物与配置未改；交权记录8条变化路径SHA/删除状态独立核对相符。未重复UI-1/EC-1/OL-1/OS-1/AF-1/DR-1实现或验收，未启动执行会话、测试、构建、浏览器、服务、Go/PG或容器；未部署、切服务、调用产品真实模型/外部目标、commit/push。
- DR-1与AF-1浏览器仍仅明确内存API fixture，不是真Go/PG端到端；OS-1不是通用脱敏，其他观察及Drawer正文/URL/错误、Attempt.error与decision_reason边界不变。下一片没有冻结，不再自动启动空白开发轮。

## 0.11 历史：DR-1动作决策说明标签已验证，收束交接（2026-11-11）

- 主`wscyber-next-main-62qrbwxs`（CLI879569→shell879563→qterminal879555→PID1，gpt-6-astra）已实际接收并获旧主823114明确交权。等待其补充回复结束，Read完整空闲画面并核对准确身份/无后代/独占父链后正常`/exit`；823114/823108/823100均不存在、旧启动目录exit.txt为0，退出核验时自动idle尚未收到。未知402485/402204/402187未操作。证据`var/main-handoff.after-af1.62qrbwxs/`。
- 交接退出后有限核对现有domain/API与正式工作面，发现`decision_reason`也保存待审或人工拒绝理由，固定称“放行依据”误导。只将列表字段、Drawer字段及卡片标题三处改为中性“决策说明”；不解析理由或推断放行来源/审批人，不改变状态、原字符串/空值条件、控制或执行行为。理由仍是未脱敏纯文本。
- DR-1任务与证据`var/session-round.decision-reason.MHHQDXCE/`，主直接完成，无执行子会话。只改两处正式组件、针对性测试、正式dist及本状态/ui-components。源码反向标签替换后与开工前SHA一致，原测试文件完整保留，仅新增一组6项；不重复实现UI-1/EC-1/OL-1/OS-1/AF-1。
- 新6项回归在旧标签上**82项中77通过、5失败**；三处标签修正后**82/82 React测试、正式类型检查和隔离构建通过**。JS为`react-DDjv3ah0.js`（1,099.05kB），CSS仍`react-CwScG4JM.css`。既有SSR Descriptions列跨度、测试bundle11.4MB及正式>500kB chunk警告保留，不屏蔽或越界调整。
- 正式dist + **明确内存只读API fixture** + 可见Chromium首次**6组通过**：pending/rejected/approved及运行/成功/失败/未知状态在列表/Drawer准确展示、原理由/空值/HTML文本保留、刷新无旧理由、键盘关闭归还来源焦点；零页面写请求/外发/持久存储，runtime/console错误警告及异常响应为空。1440/1920浅暗及390共12次列表/Drawer布局和>=16px检查，实际Read桌面/窄屏各浅暗共4张。最终目录`browser-6p1K56/`，不是真Go/PG/模型端到端。
- 一次性Node912156、对应Chromium与45419端口已独立核实结束，见`cleanup-check.json`；未启动数据库/容器/业务服务。公开文件SHA基线512→512、8条变化路径，无越界；5文档24处本地链接无缺失，围栏/尾空白检查通过。主报告`outcome-main.md`记录范围、命令退出、失败及未验证项。本轮停止，不追加第二功能；换新主以短交接实际接收及明确交权为准，下一片尚未冻结。
- 新主`wscyber-next-main-iik5seby`（CLI938716→shell938710→qterminal938703→PID1，gpt-6-astra）已实际读包、写accepted并消息确认；旧主独立核对模型参数、父链并Read真实可见交互截图。交接目录`var/main-handoff.after-dr1.iik5seby/`；唯一共享写入以最终`ownership-transfer.md`及明确消息交回为准，旧主879569/879563/879555仍待新主核验最终完整空闲后正常退出，不把接收说成旧主已关闭。
- **边界**：OS-1不是通用脱敏，其他观察与Drawer正文/URL/错误边界保持；AF-1本轮仅保留其11项回归，不声称重跑其完整浏览器矩阵。未改后端/domain/API/schema、依赖/锁、共享UI/样式、Vue或设计/组件入口与产物；不扩完整流量/控制/审批/重放/资料库/模型原文/发现/报告/复测。未重跑Go/PG/race、共享/设计全矩阵、完整读屏/全浏览器或研究质量；未部署、切服务、业务库/真实产品模型/外部目标、commit或push。

## 0.10 历史：AF-1最近一次尝试失败提示已验证，收束交接（2026-11-11）

- 主`wscyber-next-main-ddn4_hfw`（CLI823114→shell823108→qterminal823100→PID1，gpt-6-astra）已实际接收并获旧主741939明确交权。最终补充回复后的完整空闲画面、精确身份/无后代/独占父链核实后正常`/exit`，741939/741933/741924均不存在、退出码0；当时自动idle尚未收到。未知402485/402204/402187未操作。交权及退出证据在`var/main-handoff.after-os1.ddn4_hfw/`。
- 修正正式工作面将升序Snapshot.attempts第一条失败误称最近一次的问题：现在只判断最后一次尝试，blocked且该次failed并有error才显示“最近一次尝试的失败记录”。不回退更早失败，不解析next_step或推断当前阻塞原因；完整历史仍留在执行尝试页签。error仍为未脱敏纯文本，不是秘密隐藏。
- 任务包及证据`var/session-round.attempt-failure.PIiCE9tX/`，主报告`outcome-main.md`。局部修正由本主直接完成，无执行子会话；只改工作面、针对性测试及正式dist，文档仅本状态及ui-components。不加控件/API、后端/依赖或控制写入，不重做UI-1/EC-1/OL-1/OS-1。
- 先新增11项回归：原实现76项中68通过、8失败；修正后**76/76 React测试、类型检查、正式隔离构建通过**，原65项保留。JS为`react-DgQGwFPe.js`（1,099.05kB），CSS仍`react-CwScG4JM.css`；保留>500kB chunk警告。整页SSR新覆盖暴露既有Ant Descriptions列跨度警告，未屏蔽或越界改布局；不称所有日志零警告。
- 正式dist + **明确的内存API fixture** + 可见Chromium **7组通过**：最新失败与历史保留、成功后模型block/预算场景撤旧提示、空error/中断不回退、纯文本HTML、慢旧方向响应隔离、401重认证、零页面写请求/外发/持久存储。1440/1920浅暗与390共6次布局及>=16px检查，已实际查看桌面浅暗与窄屏浅暗截图。正常浏览器路径无运行异常/警告，唯一401是明确注入。最终目录`browser-vvKSxp/`；这是展示验收，不是真Go/PG/模型端到端。
- 一次性Node854803、对应Chromium及33377端口已独立核验结束，见`cleanup-check.json`；未启动数据库、容器或业务服务。公开文件SHA基线512→512、7条变化路径，无越界；5文档21处本地链接无缺失，围栏/尾空白检查通过。本轮停止，不追加第二功能，下一片尚未冻结。
- 新主`wscyber-next-main-62qrbwxs`（CLI879569→shell879563→qterminal879555→PID1，gpt-6-astra）已实际读包、写accepted并消息确认；旧主独立核对模型参数、父链及真实可见交互画面。交接目录`var/main-handoff.after-af1.62qrbwxs/`；共享写入以最终`ownership-transfer.md`及明确交权消息生效，旧主823114/823108/823100仍待新主核验最终完整空闲后正常退出，不把接收写成旧主已关闭。
- **边界**：OS-1默认收起不是通用脱敏，其他观察与Drawer正文/URL/错误边界不变。不扩后端/domain/API/schema、依赖、完整流量/重放/控制/审批、资料库/模型原文/发现/报告/复测；未重跑全仓Go/PG/race、共享/设计全矩阵、全浏览器/完整读屏或真实研究质量。未部署、切服务、连接业务库/真实产品模型/外部目标、commit或push。

## 0.9 历史：OS-1动作结果观察默认摘要与显式原文已验收（2026-11-11）

- 主`wscyber-next-main-nDcvE3YT`（CLI741939→shell741933→qterminal741924→PID1，gpt-6-astra）已接收旧主659031明确交权；精确身份、最终空闲及无后代核对后正常`/exit`，659031/659025/659015均不存在。自动idle在退出后延迟到达，已补记`var/main-handoff.after-ol1.nDcvE3YT/accepted.md`。未知402485/402204/402187始终保留。
- 仅精确`action_result`默认不挂载未脱敏content，以合法关联Action的固定白名单显示“关联回执摘要”；不解析content、不透传未知状态、错误或额外字段。原文必须显式展开，逐字符纯文本保真；收起即卸载。原文归属独立核验，缺关联动作不阻止读取合法观察，跨归属/重复观察则拒绝。上下文、内容、关联或记录失效、切页签、Drawer、创建Modal及401使展开失效，恢复旧数据不自动复开。
- 单个可见astra执行759387交回后停写；主审查并核对511→512文件SHA-256范围，无越界。执行与主分别串行通过**65项React测试（新增16、保留OL-1的13项）、正式类型检查和隔离构建**；11项源码/产物SHA完全一致。JS为`react-Cl0qwO75.js`，CSS仍`react-CwScG4JM.css`；保留>500kB chunk警告，JS 1,099.03kB。只改三处正式React源码/测试、正式dist与两份主文档，未改Drawer/共享UI/样式、Vue、设计/组件入口及产物、后端/API或依赖。
- 主可见Chromium + 真Go/一次性PG/loopback HTTP/确定性脚本模型最终**11组通过**：含cookie的真实保存结果默认无原文DOM、键盘显式展开/收起及精确文本、同身份刷新保持、Drawer敏感头与来源焦点/页签、两观察不串位与原字节下载、非JSON/额外字段、缺失/重复/跨归属、刷新正文/关联/消失、慢旧方向响应、实际跨项目、Modal草稿重开和401复位；零额外页面写请求/重放/外发/持久存储，后端原件不变。1440/1920浅暗与390底线共12次摘要/原文布局与>=16px检查，已实际查看桌面/窄屏截图。
- 浏览器首次10组通过后，主发现“草稿保持”缺少重开后的值断言；保留首轮脚本/结果，补实际草稿断言和跨项目一组，再次11组通过。不是失败后删断言，产品代码未改。两次均无运行异常或警告，控制台仅保留明确注入401。证据`var/session-round.observation-summary.eqm124dx/`，主报告`outcome-main.md`，最终浏览器`browser-mr044cme/`。
- 两次临时API与数据库均按精确PID/ID/label核对已结束，浏览器无残留。执行CLI759387→shell759379→qterminal759371最终空闲画面和无后代已核验，正常`/exit`后均不存在；退出核验时自动idle未收到，不以消息投递当空闲。详见`cleanup-check.json`、`executor-final-check.json`、`executor-exit-check.json`。本轮结束，不追加第二功能。新主`wscyber-next-main-ddn4_hfw`（CLI823114→shell823108→qterminal823100→PID1，gpt-6-astra）已实际读包、写accepted.md并消息确认，旧主独立核对身份/模型/可见画面；交接目录`var/main-handoff.after-os1.ddn4_hfw/`。共享写入以最终ownership-transfer.md及明确交权消息生效；旧主741939及独占链仍待新主核验最终空闲后正常退出，不将接收说成已关闭旧主。
- **边界**：默认收起不是通用脱敏、访问控制、后端删除或模型上下文脱敏。API原始content不变；其他kind和Drawer正文/URL/错误仍可能含敏感值。UI-1/EC-1/OL-1不重复实现；TaskModal无进出场动画修正保持。不扩完整流量/重放/控制/审批/资料库/模型原文/发现/报告/复测；未重跑全仓Go/PG/race、共享/设计全矩阵、全浏览器/完整读屏或真实研究质量。复用已审阅EC-1 fixture二进制，不声称重建后端；未部署、切服务、连接业务库/真实产品模型/外部目标、commit或push。

## 0.8 历史：OL-1观察就地查看关联回执已验收（2026-11-11）

- 主 `wscyber-next-main-u2CVoaFb`（CLI659031→shell659025→qterminal659015→PID1，gpt-6-astra）已实际接收并获旧主498568明确交权。核对最终空闲画面、精确身份及无子进程后正常`/exit`，498568/498561/498535均不存在；自动idle通知在退出核验后才延迟投递，已补记原交接目录`accepted.md`，不改写当时未收到的事实。未知402485/402204/402187始终保留。
- 正式观察记录已按现有`Observation.action_id`在当前完整Snapshot内就地打开原`ActionEvidenceDrawer`，不切页签/路由、不新增API或业务写入。核验项目/方向/观察/动作归属和ID唯一性；缺失或不一致只提示“关联回执不可用”，无关联不加按钮或解释。刷新只允许同一关联更新，关联改变/消失后清选择，不自行重开旧材料；旧快照事件不可借用新上下文。
- 单个可见astra执行676836交回后停写，主独立核对三处React源码及511文件SHA-256基线，范围无越界。执行与主分别串行通过**49项React测试（本片新增13）、正式React类型检查与隔离构建**。JS为`react-BodSHQ57.js`，CSS仍`react-CwScG4JM.css`；保留>500kB chunk警告，JS 1,096.11kB。未修改Drawer/共享UI/样式、设计或组件入口/产物、Vue、依赖/锁或后端。
- 主可见Chromium + 真Go/一次性PG/loopback HTTP/确定性脚本模型**9组通过**：键盘打开/关闭、来源DOM/焦点/阅读滚动及页签保持；同方向两条材料与原字节下载；明确注入缺失/跨方向/重复ID拒绝关联；在途刷新变更关联失效；慢旧方向响应隔离；创建Modal互斥及草稿；401清理和敏感头复位；零额外页面写请求/重放/外发及空持久存储。1440/1920浅暗与390底线共12次布局/字号测量，实际查看桌面和窄屏截图。正常路径无运行异常或警告，控制台仅保留明确注入的401。
- 浏览器前三次未通过分别为带图标按钮名称定位、关闭过渡后焦点断言时序、临时材料准备POST409。保留失败截图/日志；修正定位、等待真实来源焦点、改为独立API轮询并确认动作succeeded且方向ready后，第四次完整通过；未改产品或删核心断言，也未把409未经证实地归因于后端缺陷。证据在`var/session-round.observation-link.xfPHQSeC/`，主报告`outcome-main.md`，最终浏览器目录`browser-krs6jr6n/`。
- 四次临时API/数据库均已结束并按精确ID/label核验清理，浏览器无残留。执行最终空闲画面及PID/starttime/cwd/无后代核对后正常`/exit`；676836/676830/676822均不存在，自动idle通知当时尚未投递。详见`cleanup-check.json`、`executor-exit-request.json`。本轮验收结束，不在旧主追加第二功能。新主`wscyber-next-main-nDcvE3YT`（CLI741939→shell741933→qterminal741924→PID1，gpt-6-astra）已实际读包、写`accepted.md`并消息确认；旧主独立核对模型参数、父链和可见画面。交接目录`var/main-handoff.after-ol1.nDcvE3YT/`，共享写入以最终`ownership-transfer.md`和明确交权消息生效；旧主659031及独占链仍待新主核对最终空闲后退出，不将启动或接收说成已关闭旧主。
- **现有边界**：敏感头遮蔽仅是Drawer的字段展示能力；原有观察`content`仍原样按文本显示，动作结果正文可能含头原值，本片未新增观察正文脱敏。HTML不执行不等于秘密已遮蔽。跨项目/关联消失等分支有纯函数测试，本轮未单独重跑它们的全部浏览器组合；不把历史EC-1完整矩阵当本轮结果。
- 不扩后端/domain/API/schema、依赖、完整流量/重放/控制/审批、资料库/模型原文/发现/报告/复测。未重跑全仓Go/PG/race、共享/设计全矩阵、全浏览器/完整读屏或真实研究质量；未部署、切服务、连接业务库/真实产品模型/外部目标、commit或push。复用已审阅EC-1 Go fixture二进制，不声称重新构建后端。

## 0.7 历史：EC-1已有证据查看与明确复制已验收（2026-11-11）

- 主 `wscyber-evidence-main-Yi5W42er`（CLI498568，gpt-6-astra）已实际接收上一轮；旧主404448明确交回共享写入后，经空闲通知与无子进程核对，以可见`/exit`正常退出。当前主终端498535已由PID1接管。未知停止态402485及其shell402204/终端402187均保留，不能整体关闭。
- 在现有Action/API字段内新增共享`EvidenceBytesViewer`：复制当前文本/hex/Base64预览、当前可见请求/响应头及请求正文；下载始终来自完整已保存字节。未提供字段不虚构0字节，显式空与非法数据分开；16,384预览限制与捕获截断分开，结构化HTTP不称线上原始报文。对象/材料/模式/关闭/401使旧复制反馈失效，敏感头默认遮蔽；不承诺撤销浏览器已接受的剪贴板写入。
- 单个可见执行 `wscyber-evidence-exec-2n3jwkxe`（CLI514136、终端514120、gpt-6-astra）完成冻结范围并明确交回停写。共享查看器已接正式HTTP/stdout/stderr；组件展示含10个内存用例，六类矩阵共41项，完整流量组合仍仅展示。未重做已验收UI-1或可靠创建。
- 主验收两次复现动态减少动效后TaskModal透明但离场DOM残留，按最小追加范围将共享任务弹窗/遮罩固定无进出场动画；保留草稿、键盘、焦点、afterClose与业务状态，不改全局CSS/UIProvider/依赖。测试脚本另修正JSX构建模式、可见控件定位、下载监听、Tooltip顶层Escape、portal收尾等待及按钮可访问名；失败记录保留，未删核心断言。
- 主最终独立通过：**36项React、10组共享、19组设计契约；三个入口类型检查与隔离构建；哈希范围核对**。真实Go/一次性PG/loopback HTTP/确定性脚本模型 + 可见Chromium **13组通过**；共享字节展示 **5组通过**；组件/设计Modal专项 **6组通过**（24次动态媒体开关、12次浅暗尺寸组合及afterClose串行）。正式26次、字节展示6次布局/字号测量，桌面1440/1920浅暗、1024/390基本适配；已实际查看最终证据、字节及Modal截图。
- 真保存结果覆盖二进制、Latin1、未知charset、HTML、65,536字节捕获截断及omitempty缺失；真实剪贴板与下载字节比较通过，复制/下载零产品写请求。正式控制台仅有明确注入的断线/404/503/401，静态资源无404、警告和页面外发为空；展示/专项错误与外发均为空。三个构建仍有>500kB chunk警告。
- 证据：`var/session-round.evidence-copy.2n3jwkxe/`，主结论`outcome-main.md`；最终目录`browser-tyzfrv4n/`、`components-5Wch1B/`、`modal-gZ018k/`。六次临时API PID均不存在、六个测试容器按精确ID核验已清理、本轮label无残留；浏览器及本仓测试Node/fixture进程已结束。执行最终确认停写后，主核对PID/starttime/父链、无子进程及可见空闲画面，聚焦准确窗口正常`/exit`；CLI514136、shell514130、独占终端514120均不存在。未知402485/402204/402187仍保留。新主交接目录`var/main-handoff.after-ec1.u2CVoaFb/`；新主`wscyber-next-main-u2CVoaFb`（CLI659031→shell659025→qterminal659015→PID1，gpt-6-astra）已实际读包、写`accepted.md`并消息确认，旧主已独立核对模型参数、父链和可见画面。第一次QTerminal不支持`--separate`未启动CLI，按本机参数列表删除该参数后成功，未替换模型或覆盖失败记录。共享写入在最终检查后的明确交权记录/消息生效；旧主498568及独占终端由新主等最终空闲后精准正常退出，本条不宣称它们已经关闭。
- 执行早期一次环境诊断误回显认证变量，已披露并停止该读法；未将值复制进项目文档/交接，未擅自修改认证配置或CLI历史。若该工具回显需对外共享，应先处理凭据暴露；本轮无凭据轮换授权或执行记录。
- 不改后端/domain/API/schema、依赖/锁、Vue、设计模板源码、业务库或运行配置；设计dist仅因共享Modal修正重建。不扩完整流量/重放/审批/资料库/模型原文/发现/报告/复测。未重跑全仓Go/PG/race、共享库全部历史浏览器矩阵、全浏览器/完整读屏或真实研究质量；未部署、切换服务、调用产品真实模型/外部目标、commit或push。下一片仍须新主在现有API内核对冻结，不自动启动全部路线。

## 0.6 历史：正式React接入UI-1已验收，新主已接收（2026-11-11）

- 用户再次明确按此前可见多终端/GPT模型设计逐轮开发，一轮完成后交接新主继续。主 `wscyber-52`（CLI404448）负责契约、文档与整合；单个可见执行 `wscyber-ui-exec-YRaQMVQv`（CLI425499，gpt-6-astra）独占正式React源码/产物，已交回验收。未同时铺开后端或全部路线。
- 正式App已使用单一共享UIProvider、浅暗主题、AppShell和路由驱动TaskModal。仅开放项目列表、已有研究/证据；两个主要输入和精确origins保留。创建来源工作面保持挂载、关闭保留内存草稿/提交键、历史前后退及深链可用、明确退出清空；创建与证据portal不叠加焦点陷阱。未迁入模板fixture。
- 保留真实API/可靠提交契约：202不是已经运行；失联先核对、404不证明无在途提交；显式重试复用同键同内容；401遮蔽并可重新登录核对；旧方向响应不覆盖新选择；目标效果unknown不自动重放，敏感头/原字节语义不变。
- 执行类型检查、23项React测试和隔离构建通过；主独立重跑23项React、6组共享测试及受控fixture构建通过。最终正式资产`react-B-R74518.css / react-BEyMSMzQ.js`；构建仍有>500kB chunk警告。源码/产物哈希范围核对无越界：后端/domain/API/schema、Vue与旧dist、共享UI、设计/组件产物、依赖/锁均未改。
- 主真实Go/一次性PG/loopback HTTP/确定性脚本模型 + 可见Chromium **10组通过**：共享主题/Modal焦点/草稿/历史、可靠创建与证据、断线/404、503同键重试、在途关闭重开/401、过期方向响应、敏感头复位/HTML不加载、深链/退出/空持久存储、静态资源/异常/外发。首项目1个HTTP动作、2次尝试，正文与本地响应字节一致；4次创建均无重复。1440/1920浅暗及1024/390基本适配共21次字号/尺寸测量，已查看最终Modal、工作面、证据和窄屏截图。
- 首次CLI启动因`--mcp-config`可变参数误吞提示失败，新增`--`分隔后成功，示例已修正；首次TS检查的ButtonRef导出问题改用React组件ref类型；首次证据Drawer视口断言在进入动画中失败，测试等待真实动画完成后原断言通过。没有通过改产品/降断言掩盖失败。浏览器控制台只有明确注入的断线/404/503/401记录，无正常路径运行异常、警告或页面外发。
- 本轮证据`var/session-round.react-ui-resume.YRaQMVQv/`，最终浏览器目录`browser-2kyvdruo/`，主结论见`outcome-main.md`。两次临时API/数据库均已准确清理，浏览器已关闭。执行确认无在途写入/服务/子会话后，经可见`/exit`正常退出，CLI425499、shell425493与独占终端425432均已结束；第一次非聚焦窗口投递未生效，核实画面后激活终端退出，未盲杀。
- 未重跑共享展示/设计全部浏览器矩阵、全仓Go/PG/race、全浏览器/完整读屏、正式长表单全部异常组合或真实研究质量；没有新增完整流量、资料库、模型记录、Finding、报告、复测能力。保留旧Vue入口，未部署、切换服务、调用产品真实模型/外部目标、commit或push。
- 已核实旧主380724/313244和旧预览进程不在；4175/4176/8790恢复检查无监听，历史记录不证明在线。当前主终端402187及shell402204仍挂着归属未核实的停止态CLI402485，**不能整体关闭该终端或盲杀未知旧会话**。新主先实际接收短交接，再转交共享文件；仅能安全退出当前CLI时不声称整个窗口已关闭。下一短片建议在现有API内补齐可复用证据查看/明确复制，不冒充完整流量模型；范围由新主核对冻结，不自动扩大为G1/资料库/发现全路线。新主包`var/main-handoff.evidence-ui.Yi5W42er/handoff.md`；新主`wscyber-evidence-main-Yi5W42er`（CLI498568，shell498561，独立可见终端498535，gpt-6-astra）已实际读包、写`accepted.md`并消息确认。启动等待器结束后已复核终端PPID=1、SID=498535，无旧主祖先。旧主完成最终文件核对后明确交出唯一写入权并停止，新主订阅旧主最终空闲后仅精准退出CLI404448；本条不表示旧主CLI/共用终端已经关闭。

## 0.5 历史：UI-1共享组件库首轮已验收，接续正式React（2026-11-11）

- 当前新主实际CLI PID **313244**（父313238），已读交接并写 `var/main-handoff.ui1.VbBqFX/accepted.md`；旧 `interactive.pid=308971` 为失效历史记录，不能据此判定未启动。旧主 `wscyber-aa` 已明确确认接收并停止共享文档/应用文件写入，本轮新主独占写入。`bgkq0vmy3`浏览器已退出，不当作现存窗口。
- 用户最新授权：组件库写好后直接按此前设计、可见多会话协同继续短轮开发；已完成的主/执行会话经交回、无在途写入和服务归属核实后关闭其终端窗口，避免历史窗口堆积。最多额外两个，内存紧张串行；继续下一片使用新主和短任务包，不把此授权扩大为部署/真实模型/外部目标或全路线并发。
- 已新增 `web/src/ui/` 共享主题/基础CSS、CopyButton/GridFold/StreamText、TaskModal、AppShell。设计模板已共用主题、原语与三种任务Modal；外壳首先在组件展示使用，不迁入demo状态机。主题颜色/字重唯一来源为共享surface。正式React本轮只兼容验证，尚未接入新库。
- 新 `web/src/component-preview/` 实际可点击展示、独立HTML/tsconfig/Vite脚本与 `dist-components`；基础控件直接复用官方Ant，表单/数据/反馈有实际状态用例。六类40项覆盖矩阵区分“可复用/仅展示/待补/本期不适用”，见 [组件库与覆盖](ui-components.md)。业务复杂组合仍在设计入口，仅展示，不宣称穷尽未来组件。
- 本轮直接串行实现，未开子会话；复用现有依赖，锁文件未改。未改后端/domain/API/schema/Vue、业务库/生产配置或正式React源码，未调用产品模型/外部目标、部署、commit或push。
- 实际通过：components/design/react三个类型检查与隔离构建；共享6组、设计19组、正式React16项契约。可见Chromium组件8组、设计行为10组、布局4组；1440/1920浅暗、390组件表单/矩阵底线、至少16px、Modal正反Tab/Escape/草稿/焦点/长表单、敏感头复位与项目归属、复制计时/失败/异步失效、减少动效、Upload零外发、文本安全与空持久存储。已查看实际截图；浏览器正常路径错误/警告/外发均为空。
- 未重跑正式Go/PG/API完整浏览器链路；提交失联/401/过期响应本轮只回归正式React已有单元/SSR契约，展示页手动“待核对”不是真实回执核对。未验证完整读屏、所有控件所有状态/尺寸、全浏览器或研究质量。三个构建仍有>500kB chunk警告。
- 初次检查出现Vite请求类型、假计时器、SSR CSS加载配置问题，已修正后通过；浏览器初次在Ant虚拟Select/隐藏Radio/分页定位与焦点过渡等待失败，保留失败记录并修正测试定位后通过，没有降低业务断言。
- 本轮证据：`var/session-round.ui-library.5uz4jdjc/`；设计回归单独放`design-regression/`，未覆盖compact历史产物。最终当前组件资产`component-preview-D7MPmBs2.css / component-preview-wZ8rxGr5.js`，设计资产`design-preview-CwMOKbMT.css / design-preview-4rq5ctBj.js`；正式React构建资产与开工前一致。
- 交接前服务快照：4175设计预览PID3295559仍属旧主父链，保留，不结束旧主父进程带走服务；4176组件预览PID342165属本轮后台任务`bgfb1iai4`。本轮验收浏览器均已关闭，无本轮临时API/数据库。预览的最终交接/清理和新主启动结果见本轮短交接，不把计划当已完成。
- 下一获准短片：正式React接入共享主题/外壳与可靠创建Modal，保持既有API、草稿/幂等键/401/unknown/过期响应与证据语义；主会话负责共享契约与整合，按独立写入范围分派可见执行会话。缺后端的流量/资料库/模型记录/发现/报告/复测不做假入口，不把模板fixture迁入正式入口。新主交接包为 `var/main-handoff.react-ui.5EPOwGlS/handoff.md`；启动与接收需独立确认，本条不证明已经启动。
- 终端清理额外核实：旧主2444779已确认无在途写入，但其父shell726721/终端726599还挂着归属未核实的claude2213874（Tl），**不能整体关闭旧终端**。4175虽独立SID仍可能被旧CLI退出清理，不假称安全脱离。当前313244所在终端313199是本轮可见新主窗口；先交接其4176服务及新主接收，再精准关闭当前会话/终端，未知历史进程不盲杀。

## 0.4 历史：compact设计获认可、UI-1交接准备（2026-11-11；启动情况以0.5为准）

- 用户明确：“设计认可，固定好规范，然后开新的主会话开始后面的逐轮开发，后面开发添加功能可以按照适合的需求布局调整”。生效规范为 [UI-1](ui-baseline.md)；固定视觉/组件/交互与真实性边界，不冻结页面像素。正式共享组件库尚未提取完成。
- 认可模板为 `web/src/design-preview/` 与 `web/dist-design/`：左导航、充分使用展示宽度、新建/配置/补资料居中Modal、项目内就地查证。仅内存fixture，不等于正式React已接入这些功能，更不等于后端已实现资料库、模型原文持久化、完整流量、Finding/报告/复测。
- compact轮实际通过：设计类型检查、构建、19组契约、10组浏览器行为、4组布局专项；1440/1920浅暗、三种任务Modal正反Tab/焦点、敏感材料复位、动态减少动效等。修正短通知离场DOM残留、局部长文本溢出与发现历史网格空白；构建仍有>500kB chunk警告。没有重跑后端/正式React验收，也没有完整读屏/全浏览器或真实模型质量结论。
- 最终资产 `design-preview-BfZfNkNw.css` / `design-preview-BTq50oLt.js`。详细证据在 `var/session-round.compact.bEgdFUTq/outcome-main.md`、`final-checks.log`、`browser-result.json`、`layout-result.json`；web哈希范围核对unexpected为空，不是全仓审计。报告里的“等待认可”为用户随后批准前的历史状态。
- 执行会话 `wscyber-compact-bEgdFUTq` 已正常退出，PID4184478不存在。用户预览 `http://127.0.0.1:4175/` 和原可见浏览器继续保留，已强刷查看。旧主 `wscyber-aa` 只收束规范和交接，不继续扩展业务；新主启动/接收结果见后续交接记录，不把计划当已启动。
- 用户随后补充：“最好写好组件库，作为固定的参考，覆盖所可能用到的组件”。新主首轮优先建立实际共享组件库、可点击展示页和覆盖矩阵，覆盖规划中的基础/表单/布局/数据/反馈/研究业务组合，见UI-1 §5.1；不把文档目录或占位页当已实现库。官方基础组件统一主题和用例复用，业务示例与正式入口隔离。之后再增量接入正式React，不迁入演示状态机。已获逐轮开发授权，无需重复请求模板认可；重大未定契约/安全范围仍需确认，不一次启动全路线。
- 开发使用已有GPT：关键/复杂astra，简单非关键sol/terra，前端不固定Gemini。主/执行均可见交互，不设整轮强制超时，最多额外两个、优先一个。`wscyber-b8`仍消息失败，未确认退出或在途写入，不强杀、不把失败视为同意；共享文档仅精确更新，遇并发变化停下核对。
- 规范/调度共8份文档、60处本地链接及UI-1引用组件路径、围栏/空白静态检查通过；仅文档阶段未重复应用测试。短交接与可见终端脚本在 `var/main-handoff.ui1.VbBqFX/`。尝试打开新主时内存保护测得2013MiB<2048MiB，脚本在启动CLI之前退出；无新主PID或接收确认。终端启动器返回0不代表模型会话启动成功。**尚未完成换主**，释放明确可关闭的会话资源后才可执行该脚本；不盲杀未知进程、不结束旧主父链带走预览/用户浏览器。
- 本阶段仅规范/状态与交接更新，未部署、切换正式入口、调用产品真实模型/外部目标、commit或push。下面0.3及更早章节均为历史，不恢复其中Gemini/后台print/模板待认可等过期指令。

## 0.3 历史调度：模板轮次曾受Gemini配额阻塞（2026-11-11，已被后续轮次替代）

- 用户授权主会话直接识别进展并派发已确认范围内的短轮次；执行标签页和主会话均默认每轮交回后退出，下一轮新开 `claude --dangerously-skip-permissions`，不用长历史默认续跑。已写入 [开发会话约定](development-sessions.md)、AGENTS及本地配置说明，包括模型路由、按需代理 `http://192.168.43.190:9567`、内存阈值与精准退出；不改全局/harness配置或业务运行时。
- 主会话 `wscyber-02` 使用 `gpt-6-astra`。旧前端 `wscyber-0f` 已明确交回且无在途写入/服务/子会话；核对PID 1419726为本项目claude后发送TERM，随后确认该PID不存在。旧主 `wscyber-b8` 消息投递失败，**未确认其退出，不强杀**。
- 在本终端派发了一个独立Gemini CLI会话 `wscyber-design-config-round`，显式指定 `gemini-4-argon-high`、`--dangerously-skip-permissions`、print完成退出、12分钟时限、禁用不必要MCP、限定工具。后台任务 `b07vw88o9` **退出码1，失败**：日志先有 `claude-code:unrecognized_model`，随后网关返回429，说明provider antigravity该模型所有凭据冷却，最后错误为 `RESOURCE_EXHAUSTED`。没有取得模型有效交回或前端验证结果；不能判断准确恢复时间，不用代理切换解决配额问题，不静默换模型、不立即或循环重试。
- 本轮前端任务仍未完成：仅在 `web/src/design-preview/` 与构建产物 `web/dist-design/` 补模型原文开关、文件夹式skill资料库/任务选用、测试安全提示词配置、运行中候选增量演示。原D1模板状态见0.2。下一次执行仍须新开指定Gemini会话，保持纯内存假数据，不改正式React/Vue/依赖/后端，不调用产品模型或目标、不留预览服务。
- 任务包、失败日志和退出码位于被忽略的 `var/session-round.hjgnef/`；主会话短交接见该目录 `handoff.md`。无需等待旧任务，**该任务已结束**；不要因失败重做已验收的正式前端或越过设计评审启动业务开发。
- 已确认本轮CLI PID 2424921不存在，内存观察任务 `bz0eo3by1` 随CLI退出结束。结束快照可用内存约2504MiB、无swap；本轮仅新增一个执行会话，未并行运行构建/浏览器。观察不是硬内存隔离或绝不卡死保证，未操作其他项目进程/业务服务。
- 调度文档已做本地链接、围栏与空白静态检查；仓库仍全未跟踪，不能用空Git diff代替内容检查。本轮未取得前端类型检查/构建/浏览器结果，无提交/推送。主会话本轮收束并提供全新主会话启动提示，**当前用户可见终端尚未自动换主**；不能将后台子进程当作已完成交互控制交接。

## 0.2 产品/架构确认稿与独立模板（2026-11-11，前轮记录）

用户在最小前端验收后明确要求：先整体明确功能需求、核心架构和前端布局，通过可查看模板共同确认，再统一组织分模块并行；随后授权“开始下一步”。本轮只做设计收束与独立模板，**不是新增业务能力或全路线开工授权**。

- 模板最新交回（2026-11-11）：原Gemini会话 `wscyber-0f` 已确认完成旧D1范围的独立模板并暂停，报告始终使用 `gemini-4-argon-high`、没有启动后台服务。源码在 `web/src/design-preview/`，构建产物为 `web/dist-design/`，新增design入口/配置及npm脚本；该会话报告设计/正式React类型检查及设计构建通过，未做浏览器截图。主会话已独立重跑 `typecheck:design` 通过、确认构建目录存在，尚未重跑构建或浏览器验收。新增要求仍未完整合入：运行中发现已有静态展示但无增量演进演示；模型原文开关、skill资料库和禁止行为/测试安全配置尚缺，旧“模型原文必记”文案待修正。已接收交回并要求保持暂停，未派发下一开发轮次。以下“尚待模板交回”为此前状态，不再作为当前结论。
- 最新补充确认（2026-11-11）：用户接受架构补齐方向并要求优先寻找优秀组件复用/学习；目标流量必须记录，模型输入/返回原文记录改为可开关。新增文件夹式 skill 资料库和运行时选用；新增可开关的禁止行为提示词，与未来流量审批同属测试安全。提示词软约束关闭不扩大授权或绕过现有G0/程序保护；完整新增审批仍延期。详细开关/版本/加载边界见[研究配置补充](research-configuration.md)，已同步产品/引擎/架构/愿景/前端/路线；默认值和组件仍为建议，未改应用代码、未验证运行能力、未启动业务开发或部署。下列此前“模型原文必记”的历史要求以本条为准。
- 用户补充确认（2026-11-11）：漏洞发现需在研究运行过程中持续可见和更新，不等任务结束后统一整理才展示。候选及时持久保存并标明待验证，证据/正文/验证结论随进展更新且保留历史；成果收尾只是汇总，不是首次展示门槛。已同步产品确认稿、研究引擎设计与发现验收要求；本次仅补充文档，未实现该能力或启动业务开发。
- 主入口：[产品、架构与界面确认稿](product-confirmation.md)，已更新D1：用户明确将新增请求风险控制/审批、外部平台实际对接移出首版；目标流量记录与逐轮LLM实际输入/返回保存及读取接口纳入首版。保留现有origin/DNS/G0保护，独立验证/复测上下文为建议而非已批准拓扑。新增接口尚未实现。
- 用户在要求明确挖掘架构后进一步决定：**稳定运行为主，保留关键阶段，让模型自主覆盖/探索，不用协议强制全覆盖。** [研究引擎E1](research-engine-design.md)替代E0的强制覆盖/深度账本、专门覆盖审计及结束门槛。建议主研究Agent自主规划/探索，候选按需独立验证，成果收尾，复测另行触发；默认串行、复用同一执行基础，具体实现尚待评审。仅保留授权/预算/持久事实/未知不重放与成果真实性等必要边界；先正常链路和故障恢复，再小规模效果改进。不估造故障次数或稳定性成绩，本轮未改业务代码、未跑真实模型。
- 公开源码研究：沿用ARTEX固定版本记录；本次通过GitHub API补读Strix `685bbc8138d35fc9fe378ac37ec0d9198b9c46a3` 与Shannon `804b80636d5022f06dfd6242744610e7521c8f21`的角色/覆盖/调度相关源码，校验blob SHA并记录SHA256。仅静态读取所列文件/片段，未执行上游代码或效果评测，未引入依赖。已通知Gemini会话调整目标流量/模型调用模板，且模板不能锁定未确认的引擎方案；投递成功不代表已接收完成。
- 主会话独占共享文档；已向原Gemini前端会话交接独立design-preview模板任务，仅新增隔离入口/源码/配置，复用现有依赖，不改正式React/Vue源码和构建、后端或业务库。模板必须纯演示数据，无API/模型/目标/外部资源调用，明确标识设计预览；尚待会话确认接收与完成。
- 已收束vision/architecture/roadmap/frontend-design中当前接续的“React未接入”等过期表述；历史验收保留，不把模板当成正式能力。README增加主确认稿入口。7份文档、61处本地路径、F01–F09与D01–D10编号完整性检查通过，未检查远程链接或图渲染；独立静态预览脚本已准备但尚未启动。用户审阅之前不启动后续业务批次或部署。

## 0.1 正式前端短轮次（2026-11-11，本地交互验收通过，未部署）

用户已授权“开始下一轮”，并指定使用新标签页的 Gemini 会话开展前端。该会话已确认模型为 `gemini-4-argon-high`；主会话负责契约、共享文档与整合验收，前端会话独占 `web/` 写入，不再委派。

- 范围：React/TypeScript + 官方 Ant Design 的内存令牌登录、项目列表、两个主要输入与明确 origins 的可靠开始探索、提交核对、项目/方向及已有证据的最小只读详情。不扩展研究控制/审批写入、完整流量面板、图谱、漏洞、报告或复测。
- 保留旧 Vue 源码与 `web/dist`；新构建隔离到 `web/dist-react`，不切换现有服务，不修改业务库或私有配置。新增前端依赖由前端会话核验兼容/许可证并固定版本；主会话测试工具仅放被忽略的 `var/`。
- 验收准备：系统 Chromium 与本项目 PostgreSQL 镜像已确认存在；仅本轮使用的 Go API/真实 PG/本地 HTTP fixture/确定性脚本模型验收入口已编译并实际运行。浏览器测试工具 `playwright-core@1.65.0`（Apache-2.0）仅安装在 `var/react-acceptance`，不是产品依赖。公开 npm 直连超时后，通过文档记录的进程级临时代理取得依赖；未修改全局/运行时代理配置。
- 指定开发模型的 CLI 最小检查曾报告 unrecognized_model 并在45秒超时，未循环重试；随后按用户指示改用其新开的指定模型会话。开发模型分工不代表产品真实模型验证。
- 前端已首次交回 React 实现、9项单元/SSR测试及隔离构建；主会话 HTTP API 测试通过。真实浏览器首屏渲染、1440/1024/390字号与基本宽度检查通过，但控制台静态资源404导致该轮验收失败；没有将其报告为通过。首次验收入口还遇到一次性PG初始化临时socket就绪早于TCP可用，已修正测试编排为TCP就绪检查；各次临时API/PG均结束并按精确归属清理。
- 主会话已查看桌面截图并交回修正：删减常驻技术说明、突出两个输入和授权；补研究只读串行刷新、路由代次防旧响应覆盖、401中断在途提交后的可核对状态、Drawer归属与敏感头遮蔽、测试构建错误退出码。用户明确以桌面大窗口美观为主，已写入前端设计规范。
- 修正构建已交回（前端会话报告16项测试、类型检查与构建通过）。用户授权安装可精确定位元素的浏览器工具，主会话已安装验证 `agent-browser@0.42.0`，打开独立可见最大化 Chromium，在一次性 Go/PG/本地HTTP环境通过元素引用实际登录、创建探索，看到1条HTTP动作和2次执行尝试，并检查创建页与工作区截图。CLI refs随页面变化失效时重新读取，不盲点旧编号。
- 复核发现轮询终态误写 `completed`（实际为 `concluded`）及方向GET 403未立即停止，前端已修正并交回16项测试/类型检查/构建通过；主会话核对了修正代码。完整验收曾因测试定位“项目列表”同时命中导航与返回按钮而失败，已限定导航定位，不修改产品规避测试。
- 主会话实际可见 Chromium + 真实 Go/PG/本地HTTP验收最终8组通过：页面创建/键盘/草稿导航/自动证据回流、原文字节展示与Drawer关闭焦点返回、真实提交后断线及404不重发、503后原键原内容显式重试、并行GET 401中断POST后的重认证核对、同项目切方向丢弃慢旧响应、1024/390基本适配与存储检查、静态资源与运行异常检查。最终4个任务无重复；首任务1条HTTP动作、2次执行尝试，正文与本地响应字节一致。未验证真实模型研究质量、生产部署、全浏览器兼容或所有异常组合。
- 以用户可见最大化窗口（宿主1914×914）实际操作，并保存1440×900/1920×1080视口截图；主会话查看创建页、研究工作面、证据Drawer等截图。桌面双栏/层级/留白可用，各主页面可见文本>=16px；不以自动检查代替用户最终审美确认。最终产物位于被忽略的 `var/react-acceptance/run-8m6x18qv`；其中404/503/401与断线错误为明确注入，不是静态资源或正常流程错误。
- 各次临时浏览器、API、数据库均已结束，数据库按精确ID/label清理；agent-browser演示会话也已关闭。旧Vue三份构建文件SHA256与开工前一致；没有切换服务、触碰业务库或私有配置、调用真实模型/外部目标、提交或推送Git。后端上一轮结果如下。

## 0. 最新授权与当前短轮次（后端已验收，交回）

2026-11-10 用户明确授权“按照这个首轮开始，注意进程不要卡死”。本轮仅实施：可靠创建任务、持久启动意图、自动初始方向、提交去重/核对及版本化迁移，接现有研究循环；正式 React 入口留下一短轮次。主会话负责关键实现，暂不开额外会话。测试/构建限制并行并设置超时，不重启现有服务、不连接业务库、不调用真实模型或外部目标。下列开工前讨论是历史背景，不撤销本次明确授权。

### 本轮已实现

- 新 `POST /api/explorations` 接收 description/goal 和明确 allowed_origins，强制提交键；同事务保存 Mission、一个初始 Thread、启动事件和不可变操作回执。返回202/accepted，ready/auto_run=true；实际 Attempt/running 由原 worker 认领产生，handler 不调用模型/目标。
- `GET /api/operations/{key}` 可核对已提交操作。同键同请求只回放原响应，不重启、清零预算或更改暂停/unknown；同键异请求409。回执与实时状态分开，404不证明没有在途提交。缺模型的新请求503且不留半个任务；已提交回放不受后来缺模型影响。
- 新接口规范化并排序/去重 origins，背景不是授权或观察；旧 Mission/Thread 创建语义不变。没有给全部旧 POST 增加幂等，也没有新增公开 pause_requested/max_steps 或独立 GET action。
- v1固定已有 schema，v2新增operations；迁移版本与SQL校验值、结构变更同事务提交，未知版本/校验不符拒绝启动。保留原单实例锁、事件顺序和动作状态约束；迁移说明见 [database-migrations](database-migrations.md)。未新增依赖。
- 关键后端由主会话实施，无额外开发会话。未修改 web/模型提示词/执行器/研究循环或现有私有配置，未部署、重启服务或升级业务库。正式 React/Ant Design 任务入口留下一短轮次，仍按 Gemini 分工；其他产品路线不自动展开。

### 本轮真实验收（2026-11-11）

- 真实 PostgreSQL：16项PG测试及 canonical 单元通过；新增覆盖并发同键、异请求冲突、最后一步回执故障整体回滚、锁等待上限、在途404、重启核对、暂停/unknown/预算不重启、无版本旧基线升级、原始字节/旧待审/暂停/累计步数保留、失败迁移回滚和校验不符拒绝启动。
- HTTP/研究跨模块：6项顶层用例通过（另含子用例）；新增两输入自动启动、本地请求真实证据回流、已完成后同键回放、缺模型与模型失败、真正断开HTTP连接丢失回执后核对；原批准/越界/待审不重复调用等测试继续通过。真实命令沙箱1项显式跳过。
- 在本轮一次性容器的两个新空库完成旧基线 fixture `pg_dump -Fc` / `pg_restore --exit-on-error`，逐表核对记录、原字节、暂停/步数/unknown保持。它与迁移测试分开，不代表业务库生产恢复验收。
- 测试进程均有超时，Go为低并行与低GC内存配置，两个PG测试包使用 `-p 1` 串行。只使用本项目 `wscyber/postgres:16` 一次性容器，384MiB/1CPU、tmpfs与随机loopback端口，按精确ID和本轮label清理；未操作既有库/持久卷。测试辅助脚本、纯fixture备份和构建产物在被忽略的 var/，不是产品依赖。
- 已如实修正两类验收问题：首次新增测试编译发现未使用 import，删除后重跑通过；额外备份演练fixture漏填Thread.rationale触发非空约束，补齐测试数据后通过，没有放宽数据库约束。失败尝试也已定向清理。
- httpapi/research 单元及 race 均通过；domain 编译通过（无独立测试）。domain/httpapi/store/integration/research/cmd 的 go vet 通过；Linux CGO=0 后端构建通过，产物 `var/startup-build.ByRQNY/wscyber` 未运行，未覆盖既有运行文件。未重跑 store/integration race。
- 最终8份修改文档、48处本地链接/锚点、围栏/空白/末尾换行与全部本轮 Go 文件格式检查通过；未检查远程链接或渲染图。分支仍为 feat/foundation，尚无提交/推送。
- 本轮测试与构建进程均已结束，各次临时数据库容器均已核对归属并删除；没有新服务留在后台。未运行真实模型、外部目标、前端浏览器、runtime镜像或强制出口效果验证。

### 开工前产品确认记录（历史，不是当前停止点）

- 用户已确认：**首版聚焦 Web/API 漏洞挖掘**，不同时展开代码审计、CTF 和内部攻防演练；这些仍保留为长期产品目标。
- 用户新增确认的产品功能：①类 Burp 的流量记录面板，查看请求/响应；②探索过程图谱；③逐漏洞的发现时间线；④漏洞详情关联具体流量并可直接复制，主正文仅保留简短的漏洞描述（原理/危害）、复现步骤和修复建议；⑤每个漏洞可复测并保留每次复测报告，后续支持按外部规定格式提交结果，也支持外部平台提交漏洞信息创建复测任务。不要求第一步全部做出；不是全部功能同时开工或已经实现。
- 开始正式开发前，先与用户沟通确定架构、功能范围、组件选型和设计规范。已确认主场景及上述功能目标，但完整首版交付边界、验收标准与下一实现切片尚未冻结；不得将功能确认视为开工授权。登录态/多身份方案仍为建议，用户尚未确认；外部复测接口协议和字段尚未提供。
- 设计建议：流量、图谱、时间线、漏洞与复测共用可追溯的执行/证据关联；图谱不是新的事实数据库。复测新增任务、证据和报告，不覆盖原发现；外部任务不能扩大授权，结果投递重试不能再次测试目标。职责与边界见 findings-and-reporting、frontend-design、architecture；分片建议见 roadmap，均未新增 domain/API/schema。
- 开发协作：主 Agent 负责整体协调，最多额外两个会话，每个子会话只完成一个小轮次后交回并结束。关键任务与关键实现使用 `gpt-6-astra`；简单、非关键开发使用 `gpt-6.1-sol` 或 `gpt-6.1-terra`；前端使用 `gemini-4-argon-high`。这些为任务分配要求，不代表模型连接或终端启动通道已经验证。
- 用户补充任务入口：填写目标或已知信息、希望达到的结果后开始自动探索；界面收敛为“目标／已知信息”和“希望达到的目标”两个主要输入，不要求手工建方向。“开始探索”协调可靠保存与显式启动，当前 Mission 创建不启动研究的 API 仍未改变；名称/预算/授权呈现等细节仍为建议。
- ARTEX 研究已从历史搜索索引推进到公开固定版本源码静态核验：BaaRai/ARTEX commit `923c4a813ad2ae6cb91b0799f8b9104e2437175b`，核对表单/启动链、goals/planner/worker 提示词及工具/上下文装配、调度回流与复测提示词。结论见 [research-start-and-prompts](research-start-and-prompts.md)，许可证与组件边界见 [components](components.md)。未执行上游、未安装 Norma、不移植其代码；这不是运行效果或完整安全审计。
- 本次只更新产品设计/状态文档与公开源码研究，不改应用代码、不启动开发子会话、不运行模型或目标验证。临时只读源码快照 `.artex-source.jb0igo9v/` 尚保留，不是产品依赖，不应提交。下文为最近 G0 实现及历史验证，不代表已获准推进后续阶段。
- 本次真实验证：8份设计/状态 Markdown、46处本地链接/锚点、代码围栏、行尾空白和文件末尾换行检查通过；未检查外部链接可达性、未渲染图、未运行应用测试。Git 确认为 `feat/foundation`、尚无提交；未推送。

## 1. 最近实现任务与停止点（G0）

G0 轮次用户要求：减少镜像，尽量共用一个；场景工具和网络默认允许，不加过多严格限制，开始下一步。此前“仅文档收束”的任务已结束，G0 在该轮授权范围内实施；这不代替当前开工前的设计确认。

本轮落地 **G0：默认工具/联网 + 平台 HTTP 明确信号待审 + 共享运行镜像配方**。没有推进完整 G1/E1、框架替换、React 迁移、Finding/报告或 SDL。工具直连暂未接入请求拦截，这是明确缺口，不是已实现的全量流量门禁。

没有读取 `.env`/`.env.model-test`、调用真实模型/外部渗透目标/SDL、操作既有应用服务或业务数据库、删除来源镜像标签或数据卷。验证仅使用本地 fixture，以及新建并定向清理的一次性 PostgreSQL 测试容器。未读取或修改 Arena 应用代码/数据。

## 2. 已改实现与兼容边界

### 默认放行与 HTTP 风险信号

- 新 `local_command` 默认保存为 `approved`，不逐命令审批，不设工具名称白名单；不是声称镜像已安装所有工具或支持任意特权操作。
- 新平台 HTTP 未命中明确危险信号默认 `approved`；DELETE 方法、路径/查询、正文/JSON、方法覆盖头中的 delete/remove/reset 等信号保存为 `pending`，人工放行前不派发。普通 POST/PATCH 不一律待审。
- `internal/policy` 的 `request-signals-v1` 是轻量启发式，支持基础 camelCase、常见 URL/form 编码和 JSON 字符串转义；不是完整业务风险识别，会有误报/漏报，不保证识别任意编码/压缩或业务副作用。模型填写的 reason 不作为放行依据。
- 请求 hash、初始状态和策略理由同事务保存；`action.proposed` 事件保留初始策略理由。人工决定可覆盖 pending 的 `decision_reason`，但不覆盖初始事件。没有新 PolicyDecision 实体或审批人身份字段。
- `approved` 表示策略或人工放行，前端不再统一称为“人工已批准”。动作先持久认领再执行，保存真实回执/观察；unknown 不自动重放。
- 研究 Attempt 仅在新动作 pending 时 waiting_approval；自动放行动作完成提出后 Attempt succeeded、方向 ready，未决动作仍阻止下一轮模型调用。暂停、单未决动作和单实例锁保留。
- **无 schema 变更或历史批量重判。** 旧 pending 继续人工决定，不自动放行旧普通 HTTP/命令；旧 approved 恢复派发、pause 不撤回在途、resolve 保留 unknown 历史等语义不变。

### 默认联网与宿主隔离

- `WSCYBER_SANDBOX_NETWORK` 默认 `bridge`，可显式设 `none`；配置与执行器都拒绝不支持的值（如 host）。实际设置经主入口传给 Docker 参数，`/api/system.network_mode` 同步说明覆盖边界。
- `/tmp` 保留 nosuid/nodev，取消 noexec，扩为512MiB，可运行临时编译产物。非root、只读根、无宿主挂载/socket/模型凭据、清空命令环境及现有资源/输出/时间预算保留。
- **工具直连未经过平台 HTTP 策略、origin 或地址校验。** 默认 bridge 是开放联网模式；不称其为强制出口。平台代发 HTTP 仍有原 origin/DNS/IP 校验、不重定向、不自动重试、正常 TLS 校验；loopback 仍需显式 fixture 开关。
- 模型提示词与 Vue 原型表单/状态/详情已同步，提交普通请求或工具命令可能立即异步执行；不再提示“所有动作必须先人工批准”。没有进行 React/antd 迁移或新页面扩展。

### 单一自定义运行镜像配方

- `make image` 与兼容别名 `make toolbox` 都只生成 `wscyber/runtime:local`；默认执行镜像也改为此名称。不按场景拆镜像，不额外生成旧 toolbox 标签。
- `deploy/toolbox/Dockerfile` 复用已确认工具基础层，加入本项目 Linux CGO=0 Go 程序与前端；覆盖来源 ENTRYPOINT/CMD/HEALTHCHECK。`.dockerignore` 仅放行所需产物/配方，排除私有配置、密钥、依赖和源映射。
- `make build-runtime` 使用独立 `bin/runtime/wscyber`，不覆盖宿主 `bin/wscyber`；离线模块缓存、单包编译、默认 GOGC=30 降低编译内存，允许环境覆盖。`make web` 只构建已装依赖，不隐式 npm ci。
- PostgreSQL 继续独立持久服务；未把数据库塞进应用镜像，未改 compose。宿主 `make run` 保留。
- **尚未构建或启动新 runtime 镜像。** 容器内控制面还需 Docker CLI 与标准 socket 权限，基础层是否有 CLI 未核验；没有完整容器部署验收。既有私有 `.env` 固定旧标签时不会自动更改。

## 3. G0 轮次真实验证（历史记录，本次未重跑）

| 验证 | 实际结果 |
|---|---|
| Go 单元 | config、policy、execution、model、research、httpapi 通过；不使用真实模型或外部目标 |
| Race | policy、config、execution 三包通过；未重跑 model/research race |
| 真实 PostgreSQL Store | canonical 单元 + 10项PG测试通过，含新自动放行/尝试互斥/暂停/unknown与历史pending保持 |
| HTTP/研究跨模块 | 普通HTTP自动执行、脚本模型自动放行后消费证据、危险HTTP审批前/拒绝/越界零发送、待审时不重复模型调用通过 |
| Docker 执行器 | 替身验证默认bridge、显式none、非法网络零创建、tmpfs可执行及既有清理/unknown流程通过；真实命令容器专项本轮跳过 |
| 前端 | `vue-tsc --noEmit` 与 Vite build 通过，使用现有依赖；未做浏览器视觉/交互验收 |
| Go 静态检查 | cmd、policy、store、research、config、execution、httpapi 的 go vet 通过 |
| 静态运行程序 | 首次编译 openai-go 被 `signal: killed` 终止；降低编译并行度及 GOGC 后重试成功，生成 Linux CGO=0 的 bin/runtime/wscyber；未构建镜像 |
| 打包静态 | make image/toolbox/run 与 arm64参数 dry-run、shell语法、Dockerfile/.dockerignore静态检查通过；不等于Docker构建通过 |
| 文档 | 最终12份Markdown、48处本地链接/锚点、围栏/空白检查通过；Go格式检查无输出；架构图仅改节点文字，未渲染 |

测试库只使用已确认的 `wscyber/postgres:16` 新建一次性容器，数据为 tmpfs、仅随机 loopback 端口、独立测试密码。未读取业务凭据或连接现有数据库。完成后按准确容器ID与本轮label核对并清理，已确认不存在；没有删除持久卷。

首次将 Store 与 integration 两包并行跑在同一临时数据库，触发数据库级单实例锁，部分 Store 测试失败；不是通过结果。随后以 `go test -p 1 -count=1 -v ./internal/store ./internal/integration` 串行重跑全部通过，未放宽产品锁约束。复用同一测试数据库时应串行运行两包，或为两包使用不同数据库。

本轮真实 Docker 沙箱验收未开启；既有隔离测试已显式选择 Network=none，取消命令人工审批断言。默认 bridge 的真实联网与新镜像实际可用性还未验证，CLI 参数测试不能代替端到端验收。

## 4. 当前架构仍是什么

Go 模块化单体、PostgreSQL 权威状态、自写薄研究循环、官方 openai-go/v3 模型适配器；不是已接入 Eino/ADK runner。研究/动作 worker 分别受 MaxParallel 约束，Thread 长寿命、Attempt 短寿命，Snapshot 仍只有当前方向材料。

`delegate` 仍只新建同项目 Thread、auto_run=false，父ID在文本中；没有结构化父子汇合、自动任务交回、跨方向证据检索或业务资源冲突控制。共享状态不等于完整黑板，不强制固定专家组织。

完整一工具多请求、原生代理/MITM、策略有效期/执行上下文绑定、版本化Profile、Finding编号/Verification、LLM报告和SDL连接器均未实现。类型化授权仍只有HTTP origins；当前网络开放不代表其他类型授权已实现。

## 5. 设计与公开调研历史

上一设计轮次形成 vision V2、architecture A3、findings-and-reporting、frontend-design、api/roadmap、components 等文档。目标覆盖漏洞挖掘、代码审计、CTF、内部演练，稳定编号发现、证据驱动报告和后续SDL；这些仍不能当作当前能力。

Eino优先评估、ADK Go备选；goproxy/mitmproxy为出口候选；Semgrep/Nuclei为工具候选；DefectDojo/SysReptor为可选集成参考，SARIF/CWE/CVSS为标准。仅公开搜索索引调研，原文访问曾受验证限制；没有完整LICENSE/go.mod/部署核验、安装或运行证明。许可及复用边界见 [components](components.md)。本轮没有新增依赖。

上一轮12份文档/57处链接、非Markdown摘要一致性检查只证明当时文档收束，没有证明本轮代码未变。旧“全部动作人工批准/network=none”现在仅为历史基线，不是当前默认。

## 6. 历史服务与验证（不能当作当前运行情况）

- 旧应用入口 `http://127.0.0.1:8790`，使用本项目.env；本轮未请求healthz/登录，未重启应用，不应因续接重复启动。
- 旧PG为wscyber-postgres-1，仅127.0.0.1:15432，主库wscyber、测试库wscyber_test、卷wscyber_postgres-data；本轮未连接。新临时测试库与它无关。
- 最后记录主服务未配置模型、loopback关闭；本轮不读进程环境或秘密值。前端构建产物已更新，后端新行为尚未部署；不得把构建通过描述为在线服务已更新。
- 旧工程轮次曾通过无网络Docker、浏览器观察/审批/回执、500px布局及重启持久化；不证明本轮bridge或新UI的端到端行为。390px、完整可访问性与遗留404/连接拒绝问题未重新验证。
- 指定真实模型曾仅做一次极小检查，连接/解析失败，无有效决策；历史许可不允许自动重试。旧model/research race曾在SDK编译时被signal:killed。本轮不隐藏这些未完成项。
- 私有配置位置与边界见 [local-configuration](local-configuration.md)，镜像来源见components；来源标签保留。不宽泛pkill、不删卷、不碰其他应用。

## 7. 修改范围与下一步

本轮修改配置/主入口、policy、store/research/execution/model及针对性测试；Vue原型仅同步实际语义；修改共享镜像配方、Makefile、.dockerignore与公开配置示例；同步AGENTS、README和相关设计/API文档。没有新增数据库结构或外部依赖，未提交/推送。

G0 以上修改范围是历史记录；当前已完成第0节的可靠启动后端首轮，不再停在开工前确认。下一短轮次接正式 React/Ant Design 创建入口与真实研究状态；开始前读取已落地 API，不重新定义后端或一次展开全部路线。共享镜像/bridge、完整C1、G1/E1等仍为独立后续任务；登录态/浏览器范围、外部复测和 SDL 真实协议尚待确定。续接见 [roadmap](roadmap.md#6-下一会话与续接)。
