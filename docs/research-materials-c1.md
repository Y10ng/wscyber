# C1：资料库、任务选用与附加安全提示词

日期2026-11-12；**下文记录本轮已接通的旧C1固定选材路径，尚非完整交付。用户经原入口提出辅助型Skill库方向后，手工选材专用路径已暂停扩展/固化；不得继续把“创建时手工选择才可用”当最终产品前提。** 新方向为自主研究循环主导、按需使用Skill并统计实际使用，影响与未冻结建议见[方向调整](skill-library-direction.md)。库管理和附加安全底座可保留；旧接口/历史材料/预览不撤销，自动发现/加载/统计尚未实现。基础前端04已交回、执行会话正常退出，web归主；当前唯一产品/docs归属以STATUS为准。原入口已转达“区分Skill与普通资料”的选择，下一短轮冻结显式Skill发现/加载及使用记录契约后实施，不再等待笼统开工。后续C2身份/业务前提和完整控制仍未实施；运行时Skill不再简单按原“运行中补材料留C2”排除。

## 1. 实际参考、组件与差异

- 2026-11-12实际只读cyber `/tasks`：200、2554×1261、无401/403或登录跳转；可见Overview/Tasks/Models/New Task，未匹配资料/Skills入口。1次页面写请求被阻止，无截图、记录正文/字段值采集、任务执行或猜路由。不能推断该站没有资料功能。证据`var/research-main._81s5um0/reference-observation.json`。
- 已取得并阅读[Open WebUI官方Knowledge文档](https://github.com/open-webui/docs/blob/main/docs/features/workspace/knowledge.mdx)：资料集合创建/命名/描述→上传→明确关联到对话；树目录、相邻预览和编辑；短资料可使用完整正文。官网首次请求超时保留，官方仓库正文200/35539字节，SHA256 `13ec45dcdd8478772b13563bba1ba4c18d677d4c1f335c99f305d73b3bcd1f56`。这是官方流程材料，不是亲自操作其登录UI。沿用文件夹、文件列表/按需查看、普通编辑保存和任务显式选择；不照搬RAG/向量库、全库自主发现、文件夹同步、远程抓取或执行工具。
- 已取得[Agent Skills格式规范](https://agentskills.io/specification)：SKILL.md为Markdown+frontmatter，附带scripts/references/assets可选，正文/引用按需加载。规范源码LICENSE取得为Apache-2.0；仅原创归纳，不迁入其代码/模板，不为文本导入引入YAML解释器或完整生态兼容声明。C1把SKILL.md及其他.md/.txt原样作为数据，frontmatter/allowed-tools不授予权限；附件必须逐个选用，不自动跟随引用、联网或执行scripts。
- 实际核对`web/src/ui/{UIProvider,AppShell,TaskModal,SurfaceControls,EvidenceBytesViewer}.tsx`、component-preview的FormExamples/coverage：现有Tree/Select/Upload只是基础用例；旧TaskConfiguration的固定版本资料是模板，不能直接作为正式组合。复用UI-1、14px紧凑、浅暗、TaskModal、GridFold/CopyButton及既有表格/树/表单状态。资料浏览/任务多选组合及有界文本导入为缺口：先提取共享受控实现、样例与覆盖，再接真实业务；不机械包装每个基础控件。Ant6.7.0 MIT LICENSE已实际核对，无新增产品依赖。

## 2. 用户链路与边界

顶层资料库：文件夹导航、当前目录分页文件列表、按需纯文本查看，新建文件夹/资料、导入一个UTF-8 .md/.txt、编辑保存、删除。删除文件夹只允许空目录；删除需明确确认，删除实时库正文不删除已经固定到任务的材料。Markdown按纯文本显示，无HTML、外链图片或脚本执行。命名/说明只描述当前资料，不显示revision/v1/v2。可编辑所属目录，后端检查环/深度。

创建探索仍两个主要输入+明确origins，高级设置里独立选择模型、资料、原文记录和测试安全。文件名/说明/大小足够选择，正文只在明确查看后载入；选中保存ID和内部token，创建时若已变更/删除返回409，用户刷新选择而非暗中使用新内容。附加安全默认关闭、文本默认空，不自造禁止清单；“提示词约束，不保证拦截”仅放标签的按需提示，不作常驻说明。关闭时草稿可留内存但不发送清单；开启则必填。库编辑、任务草稿及未知写入在应用内切换/401重认证保留内存，显式退出清空；不写local/session storage。

创建成功后任务配置按需显示当时材料来源、大小/hash和有效清单；正文按需读取。历史任务默认未选择/关闭。资料库为当前单管理员的全局可复用资料，不是假多租户/项目私库；只有明确选择才进入具体任务，任务快照接口按mission归属核对，未选/其他任务材料不可拼接。材料与既有mission背景同属持久研究数据，本轮不宣称通用加密资料库或自动识别业务秘密；无正文日志。原文开关控制完整模型调用归档，与持久任务材料独立。

## 3. 容量与不变量

- ID/token均32位小写hex（token仅内部并发身份）；名字trim后1–256 UTF-8字节，不含控制字符、`/`、`\\`，不为`.`/`..`。文件名后缀.md/.txt（不区分大小写），格式由后缀确定`markdown`/`text`，无可执行格式。
- description最多1024 UTF-8字节；文件content非空、合法UTF-8、无NUL，最多32768字节；文件夹content/format为空。所有JSON字段严格解析，重复/未知/null字段拒绝。保留正文原字节对应UTF-8字符串，不trim/规范化换行。
- 活跃条目最多2000、文件夹最多256、文件夹深度最多8（根0），活跃文件正文总量最多16MiB；同目录名字不能重复。目录树最多256项，列表25/页、上限100，稳定ID keyset，搜索仅名称/说明且最多128字节；列表不查询正文。不承诺全量一致翻页快照，编辑/删除后可刷新。
- 单任务最多16个不同文件，总正文最多65536字节，附加清单最多8000字节。C1采用显式选定短资料的完整装配，不做自动RAG/渐进取文；不装配整个资料库。最终序列化研究Snapshot仍受既有256KiB限额，超限发送前停止、不静默截断。原文与usage仍走B账本。
- 选用时同一创建事务保存来源ID/token、当时名称/目录路径/说明、格式、正文/hash/大小；库后改/移动/删除不改旧任务。任务材料/config不可变。新选择、运行中补充/修改留C2。
- 附加清单只增加任务的模型软约束，不覆盖平台system规则、origins、G0、工具资源/凭据保护或unknown不重放。资料/目标正文在低信任Snapshot，不能覆盖平台或任务清单。没有清单时不注入附加清单段。正式managed/environment两条路径均装配；读取固定配置失败零供应商调用。

## 4. API冻结（均Bearer、同源、no-store）

### 类型

`LibraryItem`：`{id,parent_id,kind:"folder"|"file",name,description,format:""|"markdown"|"text",content_sha256,byte_size,token,created_at,updated_at}`。根parent_id=""。token为不透明并发字段，不是UI版本。

`LibraryDetail`：LibraryItem所有字段 + `content:string`（文件夹为空）。

`LibraryWrite`：`{expected_token?:string,parent_id:string,kind,name,description,content:string}`。新建expected_token省略/空；修改必须为当前token，kind不可改变。格式不由客户端赋值。

`LibraryMutation`：`{key,item_id,deleted:boolean,item?:LibraryItem,committed_at}`。删除无item。成功回执只含元数据、无正文。每次写入必须使用新的随机`Idempotency-Key`，失联保留同key只GET核对；同key同请求回原回执，同key异请求409。回放优先于当前资料/目录状态检查。库写入回执单独表，不改变旧exploration operations/FK。

### 资料库

- `GET /api/library/folders` → `{items:LibraryItem[]}`，最多256活跃文件夹，无正文。
- `GET /api/library/items?parent_id=<id|empty>&all=true|false&kind=file|folder&q=...&after=<id>&limit=25` → `{items:LibraryItem[],next_cursor:string}`。默认根目录；all=true跨目录（选择器/搜索），不能与非空parent_id同用；kind省略为两类；after为上页最后ID，游标仅本次筛选下使用，不接受额外字段。排序id升序，limit+1。
- `GET /api/library/items/{id}` → LibraryDetail；不存在/已删404。
- `PUT /api/library/items/{id}` + Idempotency-Key + LibraryWrite → LibraryMutation，200；客户端先生成随机ID（与已存/墓碑ID冲突409）。
- `DELETE /api/library/items/{id}` + Idempotency-Key + `{expected_token}` → LibraryMutation，200；非空目录/并发变化409。
- `GET /api/library/operations/{key}` → LibraryMutation；404表示该时刻无已提交回执，不等于请求永远不会提交；未知写入仅继续GET核对，不提供再次PUT/DELETE入口，也不自动重发/换键。

### 创建与任务读取

`POST /api/explorations`追加可选字段：`materials:[{id,token}]`、`safety_prompt_enabled:boolean`、`safety_prompt:string`。省略/空数组/false/空字符串与旧指纹一致；materials按ID排序后hash，重复ID400。启用清单必须非空；关闭且带非空清单400。禁止额外嵌套字段/null/重复键。提交去重优先于材料存活/token/模型能力检查。refs变更是新请求身份，同键异体409，整个事务要么全部固定并启动，要么无mission/thread/receipt残留。选材过期/已删/非文件仅在提交键锁内确认无历史回执后返回409 `{error,code:"material_selection_stale"}`；锁超时/提交身份冲突/其他409不带此code。首次明确收到该code且没有unknown/401历史的客户端可返回草稿刷新选择；有不确定历史仍保留原核对身份，不把404当永远未提交证明。

`GET /api/missions/{id}/research-config` → `{mission_id,materials:MaterialReference[],safety_prompt_enabled,safety_prompt}`。
`MaterialReference`：`{id,source_token,name,path,description,format,content_sha256,byte_size}`。
`GET /api/missions/{id}/materials/{itemID}` → MaterialReference + `{content}`；属于其他任务或未选404，即使库中可见也不返回。配置/正文独立于库删除而可读取。旧任务返回空数组/false/空字符串。

研究模型内部只加载本mission固定资料，列表/常规线程Snapshot不追加正文。调用身份仍由B账本的mission/thread/attempt定位固定配置；不把重建配置当作当轮完整原文。无新默认资料、自动跨任务检索或实际请求策略设置。

## 5. 必要验证与交回

Go/domain/parser边界、真实新tmpfs PG事务/CAS/去重/历史升级与重启、固定来源防串任务、库容量/深度/同名/输入超限；正式程序loopback provider确认双协议/环境路径实际input含选择正文和启用清单，不含未选/关闭清单，库后改删和回放不污染旧任务；授权和unknown既有保护回归按影响选择，不重跑B整套。

正式React资料CRUD/导入/任务选择/固定详情、未知写入只核对、401/迟到/离页/草稿及新增原文开关×创建失联组合；共享先样例/覆盖、类型/构建/必要单测，再真实API浏览器浅暗1440/1920/390及最大化并实际看图。可操作增量及时独立展示，不替换旧59215/41409/51013。固定产物、源码manifest、实际退出结果、失败/未覆盖及准确资源归属均保留。实现/测试/展示/用户认可分别报告。
