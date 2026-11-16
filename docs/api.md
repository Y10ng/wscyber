# API 现状与演进契约草案

日期：2026-11-12。**第1–4节记录当前实现；完整交付A、B组、C组的S1、C2-A、C2-B、C3-A及D组候选首片已完成受控本地验证和正式React展示，版本、验证范围与残余缺口见STATUS，不代表用户认可或生产就绪。第5–7节除明确标注的首轮子集外仍为设计建议。** 权威结构为 `internal/domain`，HTTP 入口为 `internal/httpapi`。可靠开始探索入口已接入React最小工作面；TF-1项目级HTTP记录列表/详情已接入并通过本地验收，均未部署；旧创建/控制接口语义不变。G0 已实现默认放行与平台 HTTP 明确信号待审，不等于完整原生流量门禁。完成状态见 [STATUS](STATUS.md)。

产品已纠正为通用授权安全研究和请求级风险控制。新工具命令默认放行，平台 HTTP 仅命中明确危险信号时待审；现有类型化授权仍只有 HTTP origins，工具直连尚未强制执行该范围。不能把尚未实现的Profile、多Agent协作、独立流量请求、自动确认/报告/SDL字段写进现有响应。D首片候选Finding为已新增的限定契约，见下节；其他新增契约按最新实施范围评审，早期P0/G1路线不代替[当前交付顺序](system-delivery.md)。

**首版边界（2026-11-12）：** 新增请求风险控制/审批与外部对接仍延期，现有G0与授权检查不变；下文G1/SDL保留未来契约，不作为首版流量记录的强制前置。B组已实现逐轮调用账本、可选实际输入/返回和受控读取，取代[确认稿§6.4](product-confirmation.md#64-模型逐轮记录与读取契约建议)中的早期候选接口；不是无条件保存原文。系统理解/覆盖/动态分工等挖掘引擎契约仍待[架构评审](research-engine-design.md)。

## B组已实现契约

调用账本、可选原文、价格和每日/任务用量的请求/响应见[model-call-ledger.md §6](model-call-ledger.md#6-http契约b组实施基线)，权威类型位于`internal/domain/model_call.go`。正式API和React已接通：后端最终06完成14组正式运行/故障及新PG race，前端04专项使用后端02，主另完成06＋04实际组合联调和展示；验证/未覆盖边界见STATUS §0.22。原文仅显式读取，默认关闭并固定到任务；全局范围以range表达，summary_scope仅任务汇总要求。既有A组与授权、unknown不重放语义保持。

## C1资料库与任务固定材料（当前代码；手工选材路径暂停固化）

**S1已在C1底座上实现：** 普通资料与显式Skill共用资料库，只有显式Skill参与自动发现/按需加载，普通资料仍须显式提供。下面保留C1固定材料接口，自动Skill的新增契约见下一节及[automatic-skills.md](automatic-skills.md)。旧请求/回执/固定原件保持，不将历史选材倒算为Skill成功加载，也不将创建前手选作为自动Skill的前置。

完整字段与容量见[research-materials-c1.md §4](research-materials-c1.md#4-api冻结均bearer同源no-store)，权威类型为`internal/domain/research_materials.go`。追加007迁移，旧迁移不变；资料库是单管理员全局库，不是多租户私库，skill按文本处理，不执行或跟随链接。

- 资料库：`GET /api/library/folders`、`GET /api/library/items`（目录/搜索/类型及有界keyset分页）、`GET /api/library/items/{id}`（显式正文）、`PUT/DELETE /api/library/items/{id}`（Idempotency-Key与内部token CAS）、`GET /api/library/operations/{key}`（独立元数据回执）。列表不读取正文；未知写入仅GET核对，不自动重发/换键。
- `POST /api/explorations`追加`materials:[{id,token}]`、`safety_prompt_enabled`、`safety_prompt`，同时沿用B的`record_model_content`。省略新字段保持旧指纹，选材按ID canonical；历史回放先于当前资料检查。单事务固定来源/原件/hash/清单，库后改删不影响旧任务。
- 选材过期仅在提交键锁内确认无历史回执后返回409及稳定`code:"material_selection_stale"`。普通409（含锁超时/同键异体）无此code；首次明确stale且无unknown/401历史才可返回草稿刷新选择，其余保留原提交身份。不能依赖中文错误文字分类。
- `GET /api/missions/{id}/research-config`按需读取有效配置/材料元数据，`GET /api/missions/{id}/materials/{itemID}`只返回该任务固定原件，未选/跨任务404。普通线程Snapshot不附正文；研究发送边界仅装配本任务固定资料。
- 附加清单默认关闭，启用必填、关闭不接受非空清单；开启时只增加模型软限制，不关闭HTTP origins、G0、凭据保护或unknown不重放。正式environment及managed两协议已由本机fixture确认实际输入；不是安全拦截保证或真实模型效果验证。

C1历史产物/验证见STATUS§0.23；S1当前实现、固定预览及新增专项见§0.24。旧03手选链证据与04基础专项分开，不冒充当前S1完整回归，C2-A/C2-B限定子片见后文，完整C组仍未交付。

## S1辅助Skill（已实现，受控本机验证）

权威类型为`internal/domain/skills.go`；追加008迁移，不修改001–007。共用Bearer、同源、no-store和项目归属校验。正式React入口为资料库分类编辑、项目“研究配置 → Skill使用”。

- `LibraryItem.classification`为`material|skill`；旧数据默认material，folder只能material。PUT可选classification：新建省略为material，修改省略保留当前值。显式skill必须为文本文件且description非空。省略字段不改变旧输入序列化指纹；历史回执可能没有该字段，前端只有显式skill才显示Skill。不依赖SKILL.md或frontmatter自动分类。
- Decision新增`skill_discover`、`skill_load`、`skill_release`及`skill`对象，严格拒绝不适用字段。discover接收`query?`（最多128 UTF-8字节）、`after?`（资料ID）、`limit?`（默认10，上限20），只搜索Skill名称/说明并返回元数据；load接收`item_id,source_token,purpose`（目的必填、最多1024字节）；release接收`load_id`。它们各消耗一次正常Attempt，不产生目标Action、不执行脚本/链接，不强制使用Skill。
- load_id采用触发Attempt的**36字符小写UUID文本**；资料ID/token和model call ID仍为32字符小写hex。不要将两种身份混用。一次Attempt仍至多一次模型调用，活动结果供下一正常轮消费。
- 每方向最多4个活跃加载、正文合计65536字节、序列化活跃集98304字节。超限返回capacity，不静默截断或淘汰；最终模型输入仍受原256KiB边界。状态为`discovered|loaded|released|unavailable|stale|capacity|not_active`。正文完整读取校验且固定原件/活动/活跃引用同事务成功后才计loaded。

| GET路径 | 查询 | 返回与约束 |
|---|---|---|
| `/api/missions/{id}/skill-usage` | `after=<itemID>&limit=25` | `{mission_id,total_successful_loads:string,items:[{item_id,name,successful_loads:string}],next_cursor}`；按成功原件聚合，名称取最近一次加载快照 |
| `/api/missions/{id}/skill-activities` | `after=<decimal-sequence>&limit=25` | `{mission_id,items:SkillActivity[],next_cursor}`；序号以字符串返回，但按数据库数字升序分页；无正文 |
| `/api/missions/{id}/skill-loads/{loadID}` | 无，拒绝查询参数 | 固定来源/path/hash/正文及mission/thread/attempt/purpose/time；不存在或跨项目404 |
| `/api/missions/{id}/skill-loads/{loadID}/calls` | `after=<callID>&limit=25` | `{mission_id,load_id,items:ModelCall[],next_cursor}`；来自实际Begin事务关联，不从当前活跃集倒推 |

分页默认25、最多100，拒绝重复/未知查询参数。目录曝光、失败、释放、重复携带、GET和同Attempt回放不增加成功次数；新Attempt再次成功加载可新增。Begin关联不代表已发送，`attempted=true`不保证供应商完整收到；not_sent/transport_unknown/interrupted沿用B账本。原文关闭仍保留Skill固定原件与最小事实，但不归档整个提示词/返回。旧显式选材可选Skill作为固定任务材料，这一路径不冒充自动加载或额外计数。

## C2-A身份说明与业务前提（已实现，受控本机验证）

权威类型为`internal/domain/research_brief.go`；追加009迁移，不修改001–008。完整容量、字符和读取契约见[research-brief-c2a.md](research-brief-c2a.md)，接续验证见STATUS§0.26。

- `POST /api/explorations`可追加`research_brief:{identities?:[{label,role,ownership?}],business_rules?,preconditions?,share_with_model:boolean}`。整个对象省略保持旧输入指纹；对象存在时分享标记必须显式布尔，UI默认false。至少一条有效身份或非空白规则/前提。身份顺序、原文本与分享值参与同键比较。
- 身份最多8项；label/role/ownership分别最多128/512/1024 UTF-8字节，规则/前提8192/4096字节；全部文本16384、Go规范JSON32768字节。严格拒绝未知/重复/null字段、无效Unicode和控制字符；多行仅额外允许TAB、LF、CRLF。label去Unicode首尾空白后精确唯一，保存文本不trim。
- 同创建事务固定JSON原件/hash，数据库不可变。`GET /api/missions/{id}/research-config`按需返回可选`research_brief`，沿用Bearer、no-store与任务归属；历史无配置不补默认值。普通列表/事件/创建回执/线程Snapshot不新增正文。
- false不进入模型输入或原文归档；true仅进入本任务低信任`research_context.research_brief`，不是系统指令、目标授权或已发送事实。读取失败/原件损坏发送前拒绝，only-brief可独立装配；256KiB总输入和单Attempt至多一次调用保持。
- 本片只承载非秘密用户声明，不提供密码/Cookie/Token/Header、登录/续期/凭据注入或运行中PATCH；真实目标身份/会话接入仍待后续。

## C2-B研究控制与人工文本补充（已接通，验证范围见STATUS）

权威类型为`internal/domain/research_controls.go`；追加010迁移，不修改001–009。复用原研究状态机及全局`operations`键域，不新增调度器。完整约束见[research-controls-c2b.md](research-controls-c2b.md)。下列路由均校验mission/thread归属，沿用Bearer、同源和no-store。

| 方法与路径 | 输入／响应 |
|---|---|
| POST `/api/missions/{id}/threads/{threadID}/control` | `Idempotency-Key`＋`{command:"pause"\|"run"\|"resume"}`；200 `{key,mission_id,thread_id,command,thread}` |
| POST `/api/missions/{id}/threads/{threadID}/supplements` | `Idempotency-Key`＋`{source,content,share_with_model:true}`；201 `{key,mission_id,thread_id,command:"supplement",supplement}`，仅补充元数据 |
| GET `/api/missions/{id}/threads/{threadID}/supplements` | `{mission_id,thread_id,items:Supplement[]}`；最多16条元数据，无正文，无查询参数 |
| GET `/api/missions/{id}/threads/{threadID}/supplements/{supplementID}` | 显式原件；Supplement元数据及`content`，不存在／跨归属404，无查询参数 |
| GET `/api/missions/{id}/threads/{threadID}/supplements/{supplementID}/calls` | `after=<32hex-callID>&limit=25`；`{mission_id,thread_id,supplement_id,items:ModelCall[],next_cursor}`；默认25、最多100，有界keyset，拒绝未知／重复查询 |

- `Thread.pause_requested`是持久暂停意图；pause关闭auto_run并阻止后续认领，在途仍running并保存真实结果，不是撤销。run明确开启模型研究；resume仅恢复既有approved动作，auto_run=false，不批准pending或调用模型。二者均不清步数、不换原任务模型、不重开concluded，在途或未核对unknown拒绝接续；run先验证原绑定本地可用性，不额外探测供应商。预算由原worker强制执行。
- 同键同method/path/规范输入固定回放旧响应，回放先于当前能力／状态检查；不同请求409。业务与回执同事务；`GET /api/operations/{key}`不含补充正文，新增resource_type为thread或research_supplement，成功状态分别200／201；旧mission回执保持。回执不是实时状态，控制成功后另读Snapshot。
- 写失联保留原键与冻结载荷，只读核对完整回执和原件／方向归属；404不证明原请求未提交或不会提交，只有用户明确确认才可同键同载荷重试。正式React的App级内存草稿在关闭、离页和401后保留，显式退出清空，不写浏览器持久存储。
- Supplement元数据：`id,mission_id,thread_id,source,kind:"operator_statement",byte_size,content_sha256,created_at`。原件不可变，属于用户声明、待核验，不是授权或已验证事实。来源1–256、正文1–16384 UTF-8字节，拒绝空白、NUL、无效Unicode；保留原文而非trim。每方向最多16份、正文累计65536字节、序列化集合98304字节；超限事务回滚，不截断。UI分享初始false，本片保存必须显式true；不改变旧C2-A分享标记。
- 后续模型装配从持久仓库读取，清除调用者预填补充，不修改已装配／已发在途输入；仍受总输入256KiB及单Attempt最多一次调用限制。Begin同事务关联实际携带的补充IDs；关联、已尝试发送、not_sent、transport_unknown、interrupted各保留，不把关联说成已理解／采用。原文归档关闭仍留不可变补充及最小关联，不绕道归档整个提示词。普通Snapshot／事件／回执不附补充正文；原件与关联按需读取。
- 旧`/api/threads/{id}/control`与operator_note接口保持原语义，没有新增可靠提交保证。真实凭据／工作区、完整C组及D–F不在本片。

## C3-A有界上下文与实际装配（已实现，受控本机验证）

权威类型为`internal/domain/context_assembly.go`，完整选择规则见[research-context-c3a.md](research-context-c3a.md)。追加011迁移及不可变`model_call_contexts`，001–010不变；不是完整调用原文归档，也不替代B组发送账本。**下列policy/source/window描述为C3-A基线；D首片另增`bounded-history-findings-1`及finding窗口/来源，见下一节，两策略历史读取兼容，不倒填旧call。**

- `GET /api/model-calls/{id}/context?mission_id=...`：call ID为32hex，查询仅可选单个非空mission_id（至多128字节），拒绝未知/重复查询参数。沿用Bearer、同源、no-store和call归属；不存在或跨任务404。普通call列表不加载装配内容。
- 响应`{call_id,mission_id,thread_id,recorded,assembly}`；旧调用无记录为`recorded:false,assembly:null`，不事后补造。连通测试无研究装配，正式UI不提供其上下文按钮。
- assembly：`policy:"bounded-history-1"`、`mission_id,thread_id,attempt_id,assembled_at,input_bytes,max_input_bytes`、`windows[]`、`sources[]`。input_bytes是实际system+user的UTF-8字节，不是token估算；max_input_bytes=262144。单份JSON至多96KiB，来源至多160项；UI基于这份不可变记录每页20项本地分页。
- window：`kind:"observation"|"action"|"attempt",limit,older_exists,cutoff_at?,cutoff_id?`。窗口32/8/8，稳定按时间/ID取最近项；多取一条元数据判断更早历史，未知总数不虚构。额外必需项可能来自窗口外，不将窗口外全部内容说成已读。
- source：`kind,id,state?,at,original_bytes,included_bytes,included,required,reason,content_sha256?`；kind为mission/thread/observation/action/attempt/material/research_brief/safety_prompt/skill/skill_activity/supplement。只保存安全枚举状态、身份与容量事实、已有固定原件hash，无正文/摘要/完整prompt hash。固定配置没有持久来源时间时，at为装配读取时间；original_bytes是历史原字段字节或固定对象规范序列化字节，不冒充磁盘原文件大小。
- reason仅`selected|item_limit|history_budget`。历史单项正文读取前先检查64KiB，整条序列化历史预算96KiB；必需项超限/过多或总输入不足明确失败，不静默截断、不调用隐藏摘要。最多16条显式反证和16条未决/效果未知动作额外保留；最新观察/动作、当前Attempt也必需。自由文本未分类反证可能在未读范围，不保证语义检索完整。
- 正式Engine研究路径只先读Mission/Thread作用域；Routed在同一PG只读快照有界装配，清除调用者预填。普通`GET /api/threads/{id}`和动作执行Snapshot仍完整读取，不因本片静默限条，也未解决其长期容量。固定资料/分享说明/活跃Skill/人工补充沿用原授权和上限。
- 实际装配与call/Skill/Supplement关联同Begin事务成功后才派发；每Attempt最多一次模型调用。装配前失败无call，Begin失败不发，Finish失败不为补审计重发；装配记录与attempted/outcome分层，不证明供应商完整接收、理解或采用。原文关闭不新增完整prompt副本。

新入口与实际三路径两协议×原文开关、PG/race、React和故障浏览器证据见STATUS§0.29；未实现RAG/自动摘要/真实身份或有限文件执行工作区。

## D组首片：增量候选与真实证据关联（已完成受控本机验收）

权威类型`internal/domain/findings.go`、HTTP入口`internal/httpapi/findings.go`，完整规则见[findings-incremental-d1.md](findings-incremental-d1.md)。追加012迁移；本轮已完成定向测试、真实研究/正式React受控验收、交回及主入口整合，见STATUS§0.34，最终入口36417。用户认可/长期稳定性/真实研究质量仍分开；旧35283直接退出原因未证实，不据本轮修复追认。该D1片提供候选创建/更新、不可变历史与已有证据读取；后续D2已新增下节独立验证，仍没有自动确认、人工编辑/删除、报告或修复后复测写接口。

以下GET沿用Bearer、同源、no-store；mission/finding/revision等身份为严格小写UUID。不存在/错归属404，未知/重复/非法查询400。列表默认25、最多50，keyset游标绑定所属范围；单次序列化响应（含换行）不超过256KiB。详情/summary/证据不接受查询参数。

| GET路径 | 查询／返回 |
|---|---|
| `/api/missions/{id}/findings` | 可选`thread_id,limit,cursor`；`{mission_id,items:FindingSummary[],next_cursor}`，按稳定编号倒序，不在列表读取全文 |
| `/api/missions/{id}/findings/{findingID}` | 当前`FindingRevision`，正文和引用为固定历史时点 |
| `/api/missions/{id}/findings/{findingID}/summary` | 单条`FindingSummary`，供打开详情时有界检查更新，不拉取整篇正文 |
| `/api/missions/{id}/findings/{findingID}/history` | 可选`limit,cursor`；`{mission_id,finding_id,items:FindingHistoryRow[],next_cursor}`，仅历史元数据 |
| `/api/missions/{id}/findings/{findingID}/revisions/{revisionID}` | 指定不可变`FindingRevision`，不把当前内容回填历史 |
| `/api/missions/{id}/findings/{findingID}/revisions/{revisionID}/evidence/{index}` | `FindingEvidenceContent`；0–15规范十进制索引，先核对固定引用归属，再读取已有Action或Observation，不重新执行 |

- Summary：`id,mission_id,thread_id,number,title,status,revision,revision_id,created_at,updated_at`。平台生成部署内`WSC-`数字编号、不回收；当前status仅`candidate`，编号不代表确认或CVE。
- Revision：`id,finding_id,mission_id,thread_id,number,revision,status,created_at,attempt_id,call_id,event_id,call_started_at,call_finished_at,content_sha256,content,evidence[]`。内部revision用于冲突与保真，不要求用户管理版本。历史行只含身份/标题/时间/Attempt/call/event，不含整篇正文。
- content：`title,description,steps:[{text,evidence_indices:[]}],remediation,limitations,evidence:[{kind,id,relation}]`；引用kind为`action|observation`，relation为`supporting|counterevidence|limitation`。这些关系是模型提出的研究主张，不是验证通过的程序结论。正文/步数/引用/载荷容量严格按领域校验，详见实施契约。
- Revision.evidence另含原件所属方向/尝试、产生与引用时间、hash和真实动作状态/结果/截断/unknown等时点元数据，不复制报文字节。EvidenceContent返回`mission_id,finding_id,revision_id,index,reference,matches_snapshot,thread`及实际`action`或`observation`；原件后来改变时显式给出不匹配，不偷换旧版依据。
- 创建/更新来自正常研究`Decision.kind:"finding"`和`FindingProposal`，不是公开POST。创建省略finding_id/expected_revision，更新同时提供已有ID和所读内部revision。一次Attempt最多一次候选变更；平台从真实Attempt及已完成有效模型call确定来源，同Attempt同载荷幂等回放、异载荷/过期更新冲突。引用须来自该call实际装配的原件或已装配候选的固定引用，不信任模型自报归属或call ID。
- 新装配策略`bounded-history-findings-1`兼容旧`bounded-history-1`记录：增加当前方向最近8个候选最新修订窗口/来源，最新候选必需，仍受C3-A单项/历史/总输入预算与真实装配审计约束。旧调用不倒填、不改旧策略；新策略的观察/动作结构化hash固定引用时点。
- 账本完成、候选事务持久化、Attempt收尾分别处理；候选保存后收尾失败不丢已保存成果，不自动重放目标动作或另建候选。原文关闭仍保存有限被采纳成果及引用，但不保存完整prompt/provider信封作为发现历史。
- 正式项目“候选发现”页签复用共享组件，列表/单条summary有界更新，当前已读详情保持原修订，通过“查看更新”显式切换；证据/历史按需加载。界面可用性和故障验证以本轮实际结果为准。

## D2：候选独立验证首片（工程闭环；当前布局未通过用户验收）

权威类型`internal/domain/verification.go`，新增013迁移，001–012不改；[完整契约](finding-verification-d2.md)与STATUS§0.36记录实际验收及边界。复用原Thread/Attempt/Action/模型账本，无第二调度器；来源固定候选修订，模型绑定、origins、原文与分享策略沿原任务。

| 方法与路径 | 输入／返回 |
|---|---|
| POST `/api/missions/{id}/findings/{findingID}/verifications` | 必需Idempotency-Key；`{source_revision_id,max_steps}`；max_steps为1–8且≤当前部署上限；202 `{key,mission_id,finding_id,verification_id,thread_id,source_revision_id,start_status:"accepted"}` |
| GET 同路径 | `limit,cursor`，默认25/最多50，游标绑定mission/finding；`{mission_id,finding_id,items:VerificationSummary[],next_cursor}`，列表不加载正文 |
| GET `.../verifications/{verificationID}` | `VerificationDetail`，含摘要、真实Thread、`result`或null、最近尝试`execution_error` |
| GET `.../verifications/{verificationID}/evidence/{index}` | index 0–15；固定结果引用对应的真实Action/Observation，含`matches_snapshot`；不触发执行 |

- 摘要含`id,mission_id,finding_id,source_revision_id,source_created_at,thread_id,max_steps,created_at,status,pause_requested,auto_run,step_count,verdict,has_result,end_reason`。status是原Thread真实状态；end_reason仅空或`budget_exhausted`。程序预算终止不生成模型result；候选保持candidate，严重度/修复状态不改。
- Result含`attempt_id,call_id,created_at,conclusion,evidence[]`。conclusion为`{verdict:"supported"|"counterevidence"|"inconclusive",method,preconditions,summary,limitations,evidence:[{kind,id,relation}]}`；四段正文各≤4096字节、证据0–16、结果载荷≤24KiB。它是新证据支撑的模型判读，**不自动confirmed、不判修复**。HTTP状态码不直接判成立；500/302可为材料，200也不能证明漏洞。失败/截断材料可保留为限制，但不能单独满足主结论的新完整证据要求；未决unknown阻塞且不重放。
- 同键同载荷固定回放202，异请求409；回放先于当前能力/来源检查，旧回执不当实时状态。首次发起必须绑定当时当前修订，过时409要求明确重读；在途候选更新不改旧验证。operations新增resource_type=`verification`；失联只读核对，不自动重发/换键。
- 同候选最多32份记录、一次未结束验证；启动事务锁定原验证方向并核对在途/未决事实。pause/run/resume沿C2-B可靠控制。步数耗尽且无在途/未决动作时写不可变预算终止回执，旧方向不重开、步数/证据保留，允许用户另行显式发起；pending/approved/running/未核对unknown不能靠换键逃逸。没有强制终止在途动作端点。
- 每Attempt最多一次模型调用，新用途`verification`并入既有调用/费用/可选原文API，purpose筛选可用；实际装配策略`bounded-verification-1`新增唯一必需`verification_source`，绑定固定候选及引用。原方向全文/补充/活跃Skill不隐式复制，false资料不外发；单项/历史/总输入上限保持，原文关闭不旁路保存完整prompt/provider信封。
- 全部端点Bearer、同源、no-store、严格UUID/查询、错归属404，读取单响应≤256KiB。正式React使用共享验证组合/TaskModal/分页/证据检查器；未确认写入键仅App内存，401遮蔽、退出清空，取消读取不撤销服务端执行。完整自动确认、报告/复测仍未实现。

## 1. 当前 HTTP 契约

响应不包装 data；普通集合空时返回 `[]`，TF-1 分页列表空时返回 `{ "items": [], "next_cursor": "" }`。错误 `{ "error": "中文原因" }`：400 无效输入、401 鉴权失败、403 来源不允许、404 不存在、409 状态冲突、503 能力未配置；500 为内部错误，不返回原数据库错误。错误字符串不是前端可依赖的结构化状态码；C1选材明确过期额外返回稳定`code:"material_selection_stale"`，语义见上文。

所有 `/api/*` 需 `Authorization: Bearer <WSCYBER_AUTH_TOKEN>`；令牌只留浏览器内存，不写 URL、localStorage、sessionStorage、日志或导出。无登录会话端点，可用受保护的 `/api/system` 验证令牌。`/healthz` 无凭据只给健康；静态界面可加载，API 不公开。默认127.0.0.1:8790、同源前端，禁止跨域。

| 方法与路径 | 输入 | 当前返回与行为 |
|---|---|---|
| GET `/api/system` | — | `{name,version,model_configured,model,model_management_available,model_source,default_model_config_id,sandbox_image,sandbox_runtime,network_mode,max_parallel,max_steps}`；A组动态模型能力见下文，无密钥 |
| POST `/api/explorations` | `{description,goal,allowed_origins:string[],model_config_id?,record_model_content?,materials?,safety_prompt_enabled?,safety_prompt?,research_brief?}`；必需 `Idempotency-Key` | `{mission,thread,start_status:"accepted"}`，202；原子保存、绑定模型并启用一个初始方向，不在 handler 内调用模型/目标 |
| GET `/api/operations/{key}` | 提交键 | OperationReceipt，200；只查询已提交回执，404 不证明没有在途提交 |
| GET `/api/missions` | — | Mission[]，完整集合 |
| POST `/api/missions` | `{title,origin,goal,allowed_origins:string[]}` | Mission，201；不请求目标、不启动研究 |
| GET `/api/missions/{id}` | — | Mission |
| GET `/api/missions/{id}/threads` | — | Thread[]，完整集合 |
| GET `/api/missions/{id}/http-records` | 可选 `limit`、`cursor`、`thread_id`、`status`、`method` | HTTPRecordPage：`{items,next_cursor}`，仅当前项目的平台 HTTP Action 摘要 |
| GET `/api/missions/{id}/http-records/{actionID}` | 无查询参数 | HTTPRecordDetail：`{mission_id,thread,action}`，按需读取原 HTTP Action |
| POST `/api/missions/{id}/threads` | `{title,question,rationale,priority}` | Thread，201；ready、auto_run=false |
| GET `/api/threads/{id}` | — | Snapshot：mission/thread/observations/actions/attempts |
| POST `/api/threads/{id}/control` | `{command:"run"\|"resume"\|"pause"}` | Thread；详见状态语义 |
| POST `/api/threads/{id}/observations` | `{content}` | Observation，201；kind=operator_note，不自动视作已验证事实 |
| POST `/api/threads/{id}/actions` | ActionInput | Action，201；校验并持久保存策略结果，approved 后后台可立即执行；不是纯草稿接口 |
| GET `/api/actions` | 可选 `mission_id` | Action[]；省略筛选为全局，完整集合 |
| POST `/api/actions/{id}/decision` | `{decision:"approved"\|"rejected",reason:string}` | Action；批准后后台异步执行，不等待执行结果 |
| POST `/api/actions/{id}/resolve` | `{note:string}` | Action；核对未知效果，不改为成功、不重放 |
| GET `/api/events` | 可选 `mission_id`、`after`（默认0） | Event[]；`id > after` 按ID增序，**游标后最早最多200条** |

`network_mode` 当前返回配置的 `bridge` 或 `none`，并说明“HTTP 动作风险分流，工具直连未拦截”；version 当前为 `0.1.0-dev`，不是正式发布声明。`model_configured` 在A组改为当前默认启动路径能否完成本地配置校验，不表示健康或效果验证，也不能代替某个已有任务的绑定可用性；`max_parallel` 分别用于研究与动作两组 worker，不是全系统总上限。C2-B新增 `max_steps` 为每方向累计步数上限；`/api/system`不返回费用/token用量，B组有独立查询。

事件 `after=0` 从历史开头读，不是“最近200条”。取末条 ID 继续，按 ID 去重；无 total/has_more，满批不能断言已经结束。省略 mission_id 为全局。除下述项目 HTTP 记录列表外，其他集合及 Snapshot 没有分页，不能静默限条后做完整集合筛选。

### 首轮：可靠开始探索（已实现后端）

- `description` 对应“目标／已知信息”，必填、至多64000字节；`goal` 对应“希望达到的目标”，必填、至多32000字节。文本完整保存到 Mission.origin/goal，不冒充观察或证据；自动名称取目标前50个字符，完整目标不截断。`allowed_origins` 单独明确，1–64项；不从自由文本推断授权。
- 新接口必须有且只有一个 `Idempotency-Key`，格式为22–128个 ASCII 字母/数字/`-`/`_`，建议用 `crypto.getRandomValues` 生成16个随机字节再编码成32位hex；客户端负责至少128位随机性。新接口缺键400；旧 POST 不获得幂等保证。
- 输入指纹基于解析后的字段、规范化且排序/去重后的 origins；JSON 键顺序与授权集合顺序不影响身份，文本中的任何变化仍算不同输入。键在单管理员部署内唯一，不随 Bearer 轮换失效；键不是访问凭据。
- 同一数据库事务保存 Mission、一个 `ready/auto_run=true/step_count=0` 的初始 Thread、三个事件及成功回执。`exploration.start_requested` 与 auto_run 表达持久启动意图；不另建第二套队列。已有 worker 通过 ClaimThread 原子认领后才出现实际 Attempt/running。HTTP handler 不调用模型或目标。
- 202 和 `start_status:"accepted"` 只表示保存与启动意图已提交，不表示模型健康、已经运行或必然成功。模型未配置时新请求503且不留任务/回执；已提交同键请求优先回放，即使重启后模型未配置也可核对。后续模型调用失败保留 failed Attempt/blocked Thread，不假装仍在运行。
- 同键同输入返回原202和原响应，`Location: /api/operations/{key}`；不新增事件、不重开暂停/结束/失败的方向、不刷新预算、不重放未知动作。**回放中的 Thread 是提交时快照**，须再读 `GET /api/threads/{id}` 获取实时状态、观察、动作和尝试。
- 同键不同输入409；并发由事务提交键锁与主键裁定，锁等待至多2秒，启动事务上下文至多10秒。409等待超时、503未确认、断连均应保留原键核对，不换键重建；这些响应不证明其他在途提交失败。
- `GET /api/operations/{key}` 返回 `{key,method,path,resource_type,resource_id,response_status,committed_at}`；开始探索的 resource_type 为 mission；C2-B控制／补充分别为thread／research_supplement。它不包含原输入、指纹或原响应。`committed_at` 是成功事务内写回执的时间，不是 PostgreSQL 精确 commit 时钟；只有提交成功后可见。404也可能表示原提交仍在途，不能据此自动新建。
- 回执首版不自动过期，数据库拒绝更新/删除；没有清理接口。成功原响应含用户材料，与业务数据同等级保护；所有新端点仍受 Bearer、同源限制与 no-store 约束，不记录键/正文/凭据日志。
- 旧观察、控制、审批、未知核对等 POST 没有新增幂等；不承诺目标端恰好一次执行。正式 React 已接开始探索与提交核对；C2-B可靠控制使用新的项目／方向归属路由。

### TF-1：项目级平台 HTTP 记录（已实现）

仅查询现有 `Action.input.kind=http`，跨当前项目的研究方向；包含 pending/rejected 等未发送动作，不能把每条记录都称为已发出的流量。不是全工具抓包、原生出口、独立 OutboundRequest 或线上原始报文，不改变策略/审批/执行/恢复行为。

**列表 `GET /api/missions/{id}/http-records`：**

| 查询参数 | 当前约束 |
|---|---|
| `limit` | 缺省50；显式值须为十进制数字且在1–100，空值无效；正式 React 使用25 |
| `cursor` | 缺省或空为最新页；至多2048字节，使用上次返回的 `next_cursor` |
| `thread_id` | 缺省或空为全部方向；至多128字节、不可含NUL；必须属于当前项目，否则404 |
| `status` | 缺省或空不筛选；仅 `pending/approved/running/succeeded/failed/rejected/outcome_unknown` |
| `method` | 缺省或空不筛选；非空为1–32字节 HTTP token，不接受空白；筛选规范化为大写并忽略原方法大小写，返回仍保留原保存值 |

- 仅允许上述五个参数；未知、重复、非法编码或值均400。项目不存在404；合法筛选没有匹配记录为200空页，数据库/网络错误不能冒充空页。
- 响应为 `{items: HTTPRecordSummary[], next_cursor: string}`，空 `items=[]`，末页 `next_cursor=""`，无 total。按 `created_at DESC, id DESC` 排序；SQL 使用白名单标量投影、条件筛选及 `LIMIT limit+1`，不在应用层先加载全部 Action/正文再分页。
- `HTTPRecordSummary` 固定字段：`id, mission_id, thread_id, thread_title, method, url, status, has_result, truncated, effect_unknown, created_at, updated_at`，以及可选 `status_code, resolved_at`。`id` 是原 Action ID；时间沿用原记录。无请求/响应头、正文、错误、理由或请求哈希。**URL 原样保留，可能含敏感 query；摘要不是通用脱敏。**
- `has_result` 只表示已保存非空结果对象；`status_code` 非零时才返回。无结果、结果未含状态码与实际 HTTP 状态码分别展示。`truncated/effect_unknown` 来自原结果，缺结果时为false，不能用false反推请求已发送或效果已知；仍须结合 Action.status。`resolved_at` 不会把 unknown 改为成功。
- 游标是无填充 base64url JSON v1，精确字段为 `version, mission_id, thread_id, status, method, created_at, id`；绑定项目、规范化筛选和末条创建顺序，不绑定limit。不接受缺失/重复/未知/大小写别名/null字段、尾随JSON、非法版本/ID/时间或筛选归属不一致。时间采用UTC RFC3339Nano且不超PG微秒精度。客户端应原样回传，不自行拼接。
- 游标没有URL/正文/凭据，不是加密内容或访问凭据，也不是跨页一致快照、总数或状态增量流。每次请求各自使用当前只读快照；新记录回最新页查看，旧记录状态变化需重读当前页/详情，变更筛选须清游标。

**详情 `GET /api/missions/{id}/http-records/{actionID}`：**

- 不接受非空查询串。返回 `{mission_id:string, thread:Thread, action:Action}`，复用原 Thread/Action、完整已保存结果与字节，不复制证据或创建第二份请求身份。
- 在同一 `RepeatableRead + ReadOnly` 事务内先限定项目/HTTP类型/动作，再校验方向归属；项目/动作缺失、跨项目或非HTTP统一404，不先加载其他项目正文。此端点不是通用 `GET /api/actions/{id}`。
- 两个新端点沿用 Bearer、同源检查与 `Cache-Control: no-store`，不写事件、不请求目标、不审批/重放/控制。详情只由用户选择后读取；既有 Snapshot 契约不变。
- 正式 React 入口为“项目 → HTTP 记录”，支持当前页/选中详情刷新及串行可取消轮询；分页、筛选、切项目/记录/页签、创建Modal及401使旧读取失效。读取错误保留已读材料并说明未更新；400/401/403/404停止该链自动轮询，显式刷新或重新认证后可恢复。详情复用原证据Drawer、遮蔽、复制与字节下载，不新增写操作。

### MC-1A：配置管理后端（完整模型功能仍在实施）

本小节API只保存/读取配置，不调用供应商，不启动研究。显式连通测试由下节B2独立提供。当前完整交付A组已在源码接入直接编辑与任务绑定，尚在验收；其默认和研究语义见后续A组小节，不再使用“保存默认永不影响研究”的旧阶段描述。保存配置仍不自动测试或启动任务。

| 方法与路径 | 输入与结果 |
|---|---|
| GET `/api/model-configs` | 完整配置列表（最多100项）；空为`[]` |
| GET `/api/model-configs/{id}` | 当前版本安全视图；无原秘密读取参数 |
| PUT `/api/model-configs/{id}` | 完整配置及`expected_revision`；创建0返回201，编辑匹配当前版本返回200，旧版本409 |
| GET `/api/model-settings` | `{revision,default_config_id}`，初始`{0,""}` |
| PUT `/api/model-settings` | `{expected_revision,default_config_id}`；CAS更新默认选择，空ID清空；只接受已存在且启用配置 |

- ID由客户端以16随机字节编码为32位小写hex；请求严格拒绝未知/重复/大小写别名/null/缺失字段及查询串。配置JSON上限96KiB。API key、Header、参数各自用显式keep/replace/clear；完整字段和校验见[配置契约§6](model-configuration-and-usage.md#6-mc-1冻结增量a已写代码完整链路仍在实施)。
- 安全视图含`id/revision/enabled/created_at/name/protocol/base_url/model/auth/timeout_seconds/max_output_tokens/api_key_set/header_names/parameter_keys`，无key/Header值/参数值。created_at是当前版本写入时间，不代表最后连通时间。protocol和配置已保存不证明已适配或支持该供应商。
- 加密主密钥未配置时503；错误密钥/密文失败不伪造空列表或忽略配置。API沿用Bearer/同源/no-store，所有保存不写包含秘密的日志或业务事件。只读不会触发连接。
- 写入失联先按ID读取版本核对；该接口使用CAS，不复用`operations`回执，不承诺原响应回放。旧版本重发409，没有自动重试。默认选择有单独版本；保存不自动设默认，禁用不删除记录。
- v3只追加配置表，v1/v2 SQL不变；历史密文版本不可更新/删除，清除只影响新版本。没有在线主密钥轮换或历史秘密删除API；备份边界见[迁移约定](database-migrations.md)。

### 完整交付A组：直接编辑与真实任务选模（实现中，整合验收未完成）

- `PATCH /api/model-configs/{id}`：完整普通连接字段、`expected_revision`、`enabled`、`headers:[{name,value?:string}]`、`parameters:[{name,value?:JSON}]`，以及可选`api_key:string`。创建revision=0返回201，匹配更新返回200；旧PUT保留兼容。revision仅内部CAS，不变成用户版本流程。
- API key省略表示保留，提供时必须非空白；需要认证的新配置/已有值缺失必须填写，禁用不绕过此编辑接口的必填要求。`auth=none`清除本次有效key。完整条目清单决定最终集合：已有同名省略value保留，新项必须值，移除即不再使用；参数的JSON null是真实值而非省略。Header大小写不造成重复身份，改名不能隐式搬移原秘密。
- 接收端`base_url/protocol/auth`变化后，不静默沿用原key/Header/参数；需本次填写新值或移除。仍执行96KiB、重复JSON键、嵌套、控制字符、保留字段等校验。原始参数JSON直接校验，不先parse/stringify消除重复键；秘密不回显、不进日志或浏览器持久存储。
- 创建探索可选`model_config_id`（32位小写hex）；省略/空表示受理事务内读取当前默认。显式选择纳入幂等指纹，省略字段保持历史指纹兼容。历史回放先于默认/启用/密钥检查；新任务模型绑定与Mission/Thread/操作回执同事务落库，配置或默认后续变更不切换已有任务。
- 没有默认且没有显式选择时，仅明确配置的环境模型可作为兼容路径。已有默认不可用或显式选择失效不得回退环境；旧未绑定任务仍按原环境模型路径，新环境绑定会核对内部连接指纹，环境改变后不静默换接收方。每次managed决策再次核对当前enabled并解密绑定配置；禁用不撤回已在途请求。
- `GET /api/system`新增`model_management_available:boolean`、`model_source:"managed"|"environment"|"none"`、`default_model_config_id:string`。默认失效时仍保留managed来源与ID，`model_configured=false`，不是把它当无默认而退回环境。启动后新增配置不要求重启即可用于新任务。
- `GET /api/missions/{id}/model`：`{mission_id,source:"managed"|"environment",config_id?:string,name,model,protocol,enabled}`；无秘密、配置版本或环境指纹。读取真实绑定，不根据当前默认伪造旧任务来源；缺任务404、绑定无法读取或缺少可用环境事实503，不用空名伪造成功。
- `run`按方向所属Mission的实际模型绑定校验，不依赖当前全局默认；`resume`仍不启用模型。已有状态/在途/未知/预算限制保留，handler不调用供应商。managed研究复用两协议provider及严格Decision解析，不新增自动重试或跨协议回退。
- managed研究等待同时受调用上下文、部署级`Engine.ModelTimeout`、绑定配置`timeout_seconds`约束，实际为最短剩余期限；部署级时限还覆盖快照读取与本地解析。配置600秒不保证能等待600秒，B2测试另用独立预算。
- 新增v5任务模型绑定迁移，不改旧SQL、不删除历史密文。A组后端六步Go/PG/集成/race/构建及13组正式程序受控验证已交回，证据见STATUS；前端22组独立浏览器检查已交回，主补1440/1920/390浅暗六组弹窗边界与末项可达检查；旧预览曾异常退出，根因仍未确认，当前新预览持续可用与故障留证不构成长期可靠性证明。测试API仍用B2的内部精确版本身份，前端不暴露版本管理，旧结果以“配置已更新，此结果来自此前测试”表示来源差异。

### MC-1B2：显式持久连通测试（已完成本地验证）

`POST /api/model-configs/{id}/tests/{testID}`受理，`GET`同一路径读取。两个ID均为32位小写hex，testID全局唯一；沿用Bearer、同源与no-store，拒绝查询串。POST为`application/json`，正文至多1024字节：

```json
{"config_revision":1,"confirm_billable":true}
```

版本为1..9007199254740991的JSON整数；拒绝浮点/指数/字符串、缺失/null/false、未知/重复/大小写别名字段与尾随JSON。不接收提示词、项目材料、状态或时间。

| 情况 | HTTP与行为 |
|---|---|
| 新身份事务提交 | 202＋持久回执，不等待供应商响应 |
| 同ID、同配置/版本 | 200＋原回执；pending/running/completed均不重发 |
| 同ID不同配置或版本 | 409 |
| GET匹配归属 | 200；不存在或跨配置404 |
| 新测试配置缺失 | 404 |
| 新测试版本已变/配置禁用 | 409 |
| 非法输入、路径、query | 400；鉴权/Origin失败仍为401/403 |
| 缺主密钥、服务/数据库不可用、未完成容量满 | 503；保留原ID读取核对，不据此断言绝未受理 |

回执含`id/config_id/config_revision/status/created_at`，可选`started_at/completed_at/outcome/http_status/attempted`。status为`pending/running/completed`；仅completed有终态`not_sent/transport_unknown/http_error/invalid_response/valid_response`。HTTP状态保存实际100..999值；attempted缺省表示未知，不能转换为false。时间由数据库生成；无供应商正文/原始错误、URL、秘密、usage或假零费用。

- 鉴权与语法检查后，历史身份优先于配置改版/禁用、密钥缺失或容量。受理事务持有与配置保存同序的`model_settings → model_configs`锁，再次先核对历史身份，再校验精确当前版本、enabled、本地解密、容量与INSERT；提交后才可发送。
- 容量8为pending＋running总数，包含正在运行的1项；全局单worker。POST受理至多5秒、事务锁等待2秒，供应商等待不占HTTP请求。HTTP断开不取消已受理执行。
- 固定持久用途`connectivity_test`、request_version=1；只发`Reply with OK.`，输出上限`min(32,配置上限)`，不含system/项目材料。发起前解密回执绑定的精确旧版本并检查最新enabled，不改用后来替换的端点/密钥。禁用阻止尚未发起的请求，不撤销在途请求。
- 服务取消后用独立有界context保存并等待清理，再释放单实例锁。启动恢复旧pending/running一律completed/transport_unknown，保留既有started_at、清空发送阶段信息，不重发。领取/写回故障及已检测失锁停止执行；无法落库留待下次恢复，终态不可覆盖。
- 前端每次明确新测试生成一个ID；断线/超时/503保留该ID，通过GET或同身份POST核对，不能自动换ID重试，GET暂时404也不排除原POST稍后提交。202不等于成功；旧版本成功不能标成新版本成功。unknown不表示未发送/未计费，valid_response只证明最小请求得到有效文本。

### 字段与前端展示

| 对象 | 关键字段与真实限制 |
|---|---|
| Mission | `origin` 是自由背景；`allowed_origins` 才是授权。只有 created_at，没有 updated_at、项目状态、审批人或角色 |
| Thread | question、rationale、next_step、priority、status、auto_run、step_count、created_at、updated_at；无 summary、公开 pause_requested、可编辑预算 |
| Attempt | summary、error、status、started_at、ended_at；摘要不是 Thread 字段 |
| Observation | kind、content、attempt_id、action_id、created_at；人工记录不是 Claim |
| Action | thread_id、input、request_hash、status、result、error、decision_reason、resolved_at、resolution_note、时间；无直接 mission_id、审批人身份 |
| Event | id、mission_id、thread_id、kind、message、created_at；无结构化 action_id，不从 message 猜 ID |

当前**没有**通用 `GET /api/actions/{id}`、独立证据下载、项目/授权编辑、已批准动作撤销、在途取消、方向人工结束/重开、上传、报告或通用幂等接口。TF-1 已提供项目限定的单条 HTTP Action 详情；旧原型独立动作页面仍可从全局列表定位再读取所属方向 Snapshot，通用单动作读取仍为第5节目标。

### ActionInput 与证据

HTTP：`{kind:"http",method:"GET",url:"http://fixture:port/path",headers:{},body:"",reason:"验证目的"}`。

工具命令：`{kind:"local_command",command:"...",reason:"目的"}`。默认不逐条审批，容器网络默认 bridge；部署可显式设置 `WSCYBER_SANDBOX_NETWORK=none`。不支持 host 模式；无工具名称白名单，不表示镜像已安装所有场景工具或支持特权操作。

### G0 策略与旧状态兼容

- 仅对**新** Action 使用 `request-signals-v1`：工具命令以及未命中明确危险信号的 HTTP 直接保存为 `approved`，命中则 `pending`。DELETE 方法、路径/查询、正文/JSON（包括字符串转义）、方法覆盖头中的 delete/remove/reset 等信号待审；常见 URL/form 编码与 camelCase 有基础处理。不根据模型填写的 reason 放行。
- 这是固定启发式规则，可能误报/漏报，不覆盖任意编码、压缩、业务私有协议或所有副作用；没有可配置业务规则引擎。普通 POST/PATCH 不一律待审，无法证明无风险不等于默认禁止。
- `decision_reason` 初始保存策略版本与理由；`action.proposed` 事务事件也保留初始理由。人工决定可改写 pending 的 decision_reason，原策略理由仍在事件中；此文本不是审批人身份或独立 PolicyDecision 实体。
- `approved` 现在表示**策略或人工放行**，不能全部显示成“人工已批准”。仍先持久化，再异步认领；暂停与未决唯一约束不变。研究 Attempt 仅在 pending 时 waiting_approval；自动放行后 Attempt succeeded、方向 ready，但未决动作阻止下一次模型决策。
- 不修改 schema，不重新评估历史 pending，不把已有待审命令/普通 HTTP 批量改为 approved。旧 pending 继续人工决定；旧 approved 及 unknown 恢复语义不变。
- 该片只对一条 HTTP Action 对应的一次平台代发请求分流，不将一条工具命令等同于其所有网络请求。工具直连不经过该策略、HTTP origin 或地址过滤；完整一对多请求、强制出口与策略有效期属于 G1/E1。

ActionResult 的 `body_base64/stdout_base64/stderr_base64` 为保存的原字节 base64，可前端本地解码下载；不是另一个网络下载端点。字段还包括 status_code、headers、exit_code、truncated、effect_unknown、resolved_ip、duration_ms，按动作类型与实际存在情况展示。

`truncated=true` 表示捕获不完整，目前没有各流的独立截断计数或未捕获总字节数。HTTP 记录是结构化捕获，不标为线上原始报文；响应不等于漏洞确认。展示预览长度由前端管理，不能改变已保存内容或冒充捕获截断。

### origins 规范化

创建时规范化并去重：scheme/host 小写，去默认端口及域名末尾点，规范化 IP。拒绝首尾空白、路径（包括尾部 `/`）、query、fragment、userinfo、通配符和非 HTTP(S)；国际化域名使用 punycode。

创建仅确认语法与保存范围，不证明可连通或实际可执行。动作校验及执行仍拒绝 metadata/link-local/multicast/unspecified 等地址；loopback 只有显式开发开关才允许。明确授权的内部私网可用；不跟随重定向，不从材料中扩展 origins。

## 2. 当前控制与恢复语义

“未决动作”包含 pending、approved、running，以及尚未 resolve 的 outcome_unknown。人工观察不受方向状态限制，concluded 也可以补充记录。

| 条件 | run / resume | pause | 人工提出动作 |
|---|---|---|---|
| ready 或普通 blocked；无在途/未知 | 可用，设置 ready；auto_run 分别 true/false | paused | 无未决动作才可用 |
| waiting_approval；仅 pending/approved | 可用但不清除旧动作 | paused | 不可用 |
| paused；无在途/未知 | 可用；可能放行原 approved | 保持暂停 | 无未决动作才可用；仍暂停 |
| 运行 Attempt/Action 或 status=running | 409，不创建并行执行 | auto_run=false，记内部暂停标记，保留真实状态 | 不可用 |
| 未核对 outcome_unknown | 409，先核对 | 可暂停，但不解除未知限制 | 不可用 |
| concluded | 409，不隐式重开 | 409 | 不可用 |

- run 还要求该任务的模型绑定可用（旧未绑定任务为明确环境模型），否则按实际缺失/不可用返回错误；不因当前默认变化切换任务，也不只检查启动时环境模型。不清零累计步数，预算耗尽后 run 不能获得新额度。A组该能力检查已通过真实PG与正式程序pause→重启→run绑定回归；不代表所有生产故障已验证。
- resume 不启用模型，不批准或重放未知动作；**解除暂停后，原已批准尚未执行动作可被后台认领**。
- pause 不撤销在途工作。内部 `pause_requested` 当前未序列化；`auto_run=false` 不能独自证明已请求暂停。
- pending 在暂停期间仍可批准/拒绝；批准仍等待恢复。approved 不能改拒绝或撤销。重复同决定只在动作仍处于该决定状态时接受；已经 running 后再批准仍是409。
- 动作派发不依赖 auto_run/模型；ready、waiting_approval、blocked 均可能派发 approved，只要无在途且未暂停。因此不能把 blocked 当成全局停止标记。
- resolve 仅接受未核对 unknown，note 必填；保留 unknown 状态，写入 resolved_at/resolution_note，关闭 auto_run。方向通常 ready，有暂停标记则 paused。不能强制再 resume 一次，也不能把 resolve 用于普通补充说明。
- 正常执行成功/失败后尊重暂停；未知结果优先阻塞。模型若在途已经作出 conclude，结束结果仍可成立，暂停不伪造取消。
- 启动恢复将遗留 running Attempt 记 interrupted、running Action 记 unknown、方向阻塞；pending/approved 保留，符合派发条件的 approved 仍可执行。

## 3. 当前模块接口

- store：`Open(ctx context.Context, databaseURL string) (*Store,error)`，实现 domain.Repository；取得 PostgreSQL advisory 单实例锁后执行版本化迁移（当前v1/v2/v3/v4），失败整体回滚。Recover 由主入口调用，Close 释放。已提供 `StartExploration(ctx,key,input,modelConfigured)`、`GetOperation(ctx,key)`，TF-1 新增 `ListHTTPRecords(ctx,missionID,HTTPRecordQuery)` 与 `GetHTTPRecord(ctx,missionID,actionID)`（不改schema/迁移）；升级/备份边界见 [database-migrations](database-migrations.md)。
- execution：`Config{Image,Runtime,Network string; Timeout time.Duration; MaxOutputBytes int64; AllowLoopback bool}`，`New(Config) *Executor`，实现 domain.Executor；`NormalizeOrigin(string)(string,error)`。平台代发 HTTP 不继承代理环境；命令容器默认 bridge 直连。
- model：`Config{BaseURL,APIKey,Model string; Timeout time.Duration}`，`New(Config)(domain.Model,error)`；显式配置，缺失时不创建模型，不隐式导入其他项目或会话凭据。
- research：`Config{MaxParallel,MaxSteps int; PollInterval,ModelTimeout,ActionTimeout time.Duration}`；`New(repo domain.Repository, model domain.Model, exec domain.Executor, config Config) *Engine`；`Run(ctx context.Context)` 阻塞到取消。nil model 仍执行符合条件的 approved，不认领模型研究；审批等待不占 slot。

## 4. 不可弱化的持久约束

1. ProposeAction 计算 canonical JSON SHA256，请求不可更新；方向行锁及未决约束防并发插入。模型来源须匹配运行 Attempt。
2. ClaimThread 原子认领并创建 Attempt、递增 step_count；达到 maxSteps 记 blocked，不假称 concluded。
3. ClaimAction 先持久认领再执行；FinishAction 的结果与观察同事务。无论成功失败保留材料/错误；未知效果阻塞。
4. 跨对象 ID 校验归属；相反审批不互相覆盖；终态不可被迟到回执覆盖。
5. 恢复不重执行 running 动作；unknown 只有显式人工核对才能解除对应限制，历史状态不变。
6. Snapshot 为一致只读快照；过程事件与业务同事务，事件游标有提交顺序保障。当前列表有固定排序。
7. API 写请求超时、中止或失联不表示未提交；也不等于外部动作 unknown。当前没有通用幂等键，客户端只能先刷新核对，不得自动重发。

## 5. C1 首版目标增量（仅开始探索子集已实施）

首轮仅将提交去重/核对用于 `POST /api/explorations`，实现 `GET /api/operations/{key}` 和v1/v2迁移；具体当前行为以第1节为准。以下对旧业务 POST 的全量幂等及 C1-a 字段仍是建议，不是已有能力。C1 可独立补当前事实字段，但不能预先把旧 Action 状态机定为新流量请求的最终模型；G1 的请求/策略/放行关系及兼容迁移另见第7节。新增持久结构前先完成安全升级设计。

### C1-a：独立动作定位与控制事实

- `GET /api/actions/{id}` → Action。404 不存在；同样要求鉴权。详情通过 action.thread_id 读取 Snapshot，核对归属与授权，不依赖全量列表扫描。
- Thread 新增 `pause_requested:boolean`，返回持久层的实际标记；覆盖列表、Snapshot、控制返回等全部 Thread 序列化路径。不能由 status/auto_run 推导。
- `/api/system` 新增 `max_steps:number`，返回控制面实际配置；与 step_count 结合展示累计上限。不是费用余额，不允许前端修改或清零。

### C1-b：控制面写入去重与提交核对

目的仅是解决浏览器回执丢失时“是否已保存”的问题，**不提供目标动作重新执行接口，也不宣称外部动作恰好一次**。

1. 正式前端对所有业务 POST 使用 `Idempotency-Key`（不可预测、至少128位随机值，编码长度至多128字符）；同一次提交及其显式重试复用键，不为一次网络重试生成新键。
2. 键在当前单管理员部署内唯一，不按明文 Bearer 值分区；仍先鉴权及校验对象归属，不能拿键代替访问权限。将来多身份时再增加明确主体域。
3. 服务端将键、方法/规范路径、规范化输入指纹、关联对象、原成功响应及提交时间与业务变更放在**同一事务**；不存 Authorization 或重复原始请求正文到回执元数据，不把键/敏感响应写入日志。原响应按业务材料同等级保护。
4. 同键同请求若已提交，返回原成功 HTTP 状态与原响应，不再次修改状态/追加观察/创建事件或派发动作；客户端再 GET 当前对象，因为原响应只表示当时的提交，不保证仍是当前状态。同键不同请求返回409且不发生变更。
5. 同键并发必须在数据库唯一约束与事务中串行裁定；等待超时可返回409并要求核对，不允许第二份业务变更。事务失败不能留下“已提交”回执；成功不能只提交业务而丢回执。
6. 新增 `GET /api/operations/{key}`，返回 `{key,method,path,resource_type,resource_id,response_status,committed_at}`，只表示已提交回执，不包含秘密请求/响应。404 表示尚未查到提交记录，可能仍在途，**不证明未提交**。前端可继续核对，或由用户明确选择同键同请求重新提交；不自动重试写入。
7. 键及去重记录首版不自动过期/回收，至少保留至关联研究记录的生命周期结束；本版没有删除研究记录的界面。后续若引入清理，必须定义墓碑或拒绝过期键机制，不能让旧键失效后重新产生业务副作用。
8. 为兼容旧原型，无键请求仍按旧契约处理且不享有去重保证；正式 React 全部业务写入必须带键。无键预览不得宣称通过可靠提交验收。

覆盖现有 POST：创建项目/方向、记录观察、提出动作、控制、审批、未知核对；未来请求决定、发现/报告/投递等写入也应遵循同样的提交核对原则，但不在 C1 一次创建全部新实体。幂等重放只复用原提交；例如已执行动作的旧批准请求只能得到原回执，不能让动作再次进入 approved。用户改参数属于新请求，需新键与重新评估；历史 pending 保持人工待审，新 Action 已使用 G0 默认放行策略；完整新流量模型只有 review 项需人工决定。

前端键与草稿只保留于当前内存会话。刷新/崩溃后不保证恢复键，也不猜测新键就是旧请求；先读取对象和历史，无法核对时保留不确定性，不自动重建动作。请求哈希不能替代提交键。

### 数据与升级前提

pause_requested 已在持久层存在，但未暴露为领域 JSON。首轮已经新增提交回执结构与版本化迁移，仅在隔离测试库验证，未升级业务库；当前升级约定见 [database-migrations](database-migrations.md)。C1 其余增量仍需各自迁移与兼容验收，不批量放行旧 pending。

## 6. 缺口分级与契约验收

| 分级 | 内容 | 安排 |
|---|---|---|
| 首个新语义闭环 | C1可靠持久基础、G1请求级策略、准确状态/冲突/未知与证据语义 | 先P0/G1冻结兼容边界，再单片实施；不把全审批原型当新正式版 |
| 现有组合可用 | 跨项目旧动作列表、关联 Snapshot、浏览器字节下载、事件增量 | 如实表达旧数据；不假造 Policy/Request 字段 |
| 通用研究扩展 | Profile、类型化授权、共享证据、按需分工/预算 | R1/R2逐片设计与验证，不以提示词代替权限 |
| 明确成果目标 | Finding编号/验证、报告版本与生成、SDL映射/投递 | V1/V2/S1；契约建议见第7节及成果文档 |
| 按实际规模/运营 | 分页/检索、独立证据存储、身份/角色、费用、授权编辑 | 单独设计与授权，不静默截断、不占位实现 |

C1 验收场景：单动作深链与404/归属；在途暂停事实刷新后仍准确；实际步数上限；同键串行/并发重复、同键异请求、响应丢失、事务回滚、进程重启后查回执、旧响应与当前对象状态差异；原批准/核对重放不追加副作用；旧无键客户端仍兼容且不获得虚假保证。先用本项目受控 fixture，不调用真实模型或外部测试目标。

第1–4节的前轮静态核对来源：`internal/domain/model.go`、`internal/httpapi/server.go`、`internal/store/actions.go`、`threads.go`、`records.go`、`recovery.go`、`internal/execution/origin.go`。事件批次、恢复可能放行 approved、resolve 的 ready/paused 分支及真实预算边界继续保留，本轮没有修改这些实现。

## 7. 通用研究、流量与成果契约草案

以下是待拆分的领域职责，**不是已存在的 Go interface、JSON schema、HTTP 路由或数据库表**。P0 先冻结下一片必须的字段与状态迁移，再按任务包实现；不一次新增所有抽象。

| 契约职责（暂名） | 最小语义 | 权威/约束 |
|---|---|---|
| ScenarioProfile / Capability | 模式与版本、提示词/工具schema、上下文与证据要求、模板 | 可信配置；有效能力不超项目/部署授权，不能凭模型文本加载工具 |
| Scope / ExecutionContext | HTTP origins及后续代码仓库/commit/文件、身份、时窗 | 资源类型单独校验；当前只支持HTTP origins，背景不是范围 |
| WorkItem / ContextView | 来源问题、依赖/交回、输入证据引用、预算/资源标签 | 不等于当前delegate文本；预算不随分工扩增，跨项目默认隔离 |
| ToolAction / OutboundRequest | 工具任务与实际请求一对多、不可变请求/身份上下文引用 | 现有Action一动作一次执行，不能直接当通用多请求容器 |
| PolicyDecision / ReleaseDecision | allow/review/deny、命中理由、策略/范围版本、单请求人工决定与有效性 | allow自动过、review才人工、deny不可越权批准；实际发送前复核 |
| EvidenceRef | 原件位置/内容摘要、来源与版本、捕获限制、授权标签 | 摘要/脱敏/展示预览为派生视图；原件不被替换 |
| Finding / Verification | 稳定内部ID/编号、revision、支持/反证、验证与修复状态 | 编号由服务端原子生成，模型草稿不自动确认 |
| Report | 模板/profile版本、固定发现/证据清单、草稿/发布版本 | 生成与发布分开，引用可核验，缺材料不编造 |
| Delivery / ExternalMapping | SDL目标/远端ID、字段映射、投递版本、去重键/回执/未知 | 不猜内部协议，超时先核对，不把远端关闭强制改写为本地不成立 |

### G1 需要先决定的兼容边界

1. 旧 pending/approved/running/unknown 的解释与恢复不变；不得自动把旧 pending 变为策略已允许，或迁移时重放旧动作。
2. G0 已对平台代发 HTTP 保存请求和策略来源，并取消新工具命令逐条审批、默认启用 bridge；保留执行容器与控制面隔离。G1/E1 才增加完整原生请求模型和出口，不能把开放直连伪装为已强制检查。
3. 人工放行只对待审请求，决定绑定不可变输入与有效上下文；重复决定、过期、参数/身份/策略变化、暂停与恢复的状态迁移需专项验收。
4. 策略回执与目标响应分开；发送前等待/拒绝不伪造目标状态码。业务状态保存失败时停止发送，不能先发后补许可。
5. 一个工具多请求、待审 continuation、工具超时与错误恢复先有明确契约；原生网络工具不在本片默默开放。
6. 前端读取单请求及策略/决定记录；旧GET actions继续表示旧Action，不靠字段猜测“已智能判定风险”。新端点命名、版本和迁移策略在实施前冻结。

### 后续成果契约

D首片已提供候选身份/更新历史/证据及上述只读Finding API，并完成本轮受控本机验收；不能继续称所有成果接口均不存在。D2已新增固定候选的显式独立有限验证及新证据判读，不自动确认；报告模板/导出、修复后复测和SDL同步仍按 [findings-and-reporting](findings-and-reporting.md) 后续冻结，当前没有Report/SDL API。本文不预设内部SDL的接口、认证方式或字段；未来写入应沿用可靠提交与权限检查，跨系统/目标外部动作不承诺恰好一次。

