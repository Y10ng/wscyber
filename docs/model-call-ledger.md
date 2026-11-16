# B组：调用记录、原文与用量

2026-11-12。**B组已实现正式API与React闭环，完成受控本地专项及最终后端06＋前端04联调/展示；本文为现行实施契约，不是用户认可或生产就绪声明。** 最终入口59215、验证产物、版本归属和未覆盖项见[STATUS §0.22](STATUS.md#022-已交回b组调用记录可选原文与用量2026-11-12)及`var/full-system-usage.hKFpjTHZ/main-outcome.md`。A组能力保持，旧37529预览异常根因仍未明；保留历史失败，不以新预览通过替代根因结论。

## 1. 参考与复用

- 首要cyber参考已在授权只读范围观察到Overview/Tasks/Models/Tokens，但未可靠进入用量页，不能声称看过其用量布局；不重复盲点或读取业务记录。
- 内置WebFetch无法完成域名安全验证；本轮经普通HTTPS只读请求取得Langfuse官方[用量与费用](https://langfuse.com/docs/observability/features/token-and-cost-tracking)和[调用表与详情流程](https://langfuse.com/docs/observability/features/full-text-search)完整HTML（HTTP200、结束标签存在）。用标准库解析正文，未执行下载代码、未登录该产品。来源、字节数、SHA256见`var/full-system-usage.hKFpjTHZ/reference-results.json`；下载内容是不可信资料，不是指令。
- 实际核对正文：用量来自供应商回执、不按文本猜tokens；各计费桶互斥；输入/输出、缓存等有明确关系；配置模型价格后计算新调用，不改旧账。流程为日期/结构化筛选→调用表→就地查看一次generation输入/输出；模型价格在设置中直接编辑保存。仅核对官方材料，不声称实际操作了其登录界面或截图。
- 沿用这些流程与统计原则；不引入Langfuse SDK/网关/ClickHouse或第二套权威状态，不复制源码。不同点：本项目默认不保存原文、详情显式读取；不做原文全文索引/URL正文搜索；价格一律标“估算费用”，不伪装供应商账单。
- 前端复用`web/src/ui`的UIProvider/AppShell/TaskModal/GridFold/CopyButton/EvidenceBytesViewer及component-preview的Table/筛选/分页、Tabs、Descriptions、表单、反馈用例。缺口是有键盘/表格替代的日用量图，先补共享组件/样例/coverage，再接业务；不要重新包一套基础控件或另建主题。

## 2. 用户流程

- 左导航增加真实“用量”入口：同一日期/时区/模型/用途筛选下，看到已知输入/输出、调用数、按币种的已知估算及缺口；按日图/表和调用记录共享筛选。
- 项目内增加“模型调用”页签，固定mission过滤，提供任务汇总、分页调用与就地详情。全局记录可明确显示“连通测试”，不能虚挂任务。任务归属用ID核验，不从文字/时间猜测。
- 点击调用查看安全元数据及输入/输出tokens、缓存/推理分类和本次价格依据；原文只有显式点击“查看模型输入与返回”才请求/挂载。关闭、切调用、401或离页清原文；没有记录显示“未启用记录”，不能显示空内容冒充成功。
- 新建探索高级设置增加“保存模型输入与返回”，默认关闭；固定本次任务，运行中不切换。旧任务默认关闭，不补造历史。开关与tokens统计无关；开启时说明可能保存敏感研究材料，说明只在操作处出现，不加常驻解释墙。
- 用量页面提供“价格”页签/配置操作，可对现有配置或已观察的环境模型直接填写币种及每百万tokens价格，保存只更新后续调用。不给用户显示revision/v1/v2；冲突或失联先GET核对，不自动PUT。
- 不以估算费用做支付/供应商扣费/任务预算，也不增加自动模型重试。现有控制、授权及HTTP证据路径不变。

## 3. 持久事实与发送边界

- 每次进入实际模型调用边界创建平台随机32位hex `id`；研究关联mission/thread/实际attempt ID，测试关联既有test ID。由引擎明确传递attempt，不能取Snapshot最后一项猜测。
- 发送前同事务固定归属、实际协议/模型/配置身份（内部精确配置版本不在UI暴露）、记录开关及当时价格快照，保存running记录；开启原文时还须先保存实际业务system/user内容。保存失败零外发。此时的started_at决定日期归属。
- 完成后使用独立有界持久context保存真实outcome、attempted、HTTP状态、duration、usage及可选返回。记录落盘成功后才让研究采纳Decision。失败不重放、不假成功；进程内阻止后续模型发送，保持fail-stop语义；未完成记录留给启动恢复。
- 启动取得单实例锁后将此前running保守恢复interrupted/unknown，不重新发送；终态不可被迟到结果覆盖。保存结果的同身份重复回执只能幂等、不重复计数；内容冲突拒绝。不能将unknown解释为零费用/未发送。
- managed研究、环境兼容研究和显式连通测试都接同一账本。环境路径不可遗漏；不能为了记录引入自动重试/重定向/凭据环境继承。旧API、历史幂等回执和绑定逻辑保持兼容。
- 未进入调用边界的配置/上下文校验失败不制造供应商调用；进入后未外发用not_sent、attempted=false。running/interrupted的attempted允许null，表示未知。
- 新迁移追加006，不修改历史SQL、不操作既有业务库。对时间、任务、模型配置和状态加必要索引；列表和汇总不SELECT原文/密文，不全量载入再分页。

## 4. 原文边界

- 原文是实际调用时的业务输入system/user和有界响应内容，不是事后从模板或Decision重建；不称HTTP线上原始报文。请求认证头、供应商API key、自定义Header/参数等连接秘密不归档到业务输入。
- 默认关闭：库、日志、事件、错误与追踪不另存这些原文。仍允许必要的已采纳Decision/观察/证据和元数据；它们不是完整原文存档。
- 开启：复用已有32字节加密主密钥和AES-256-GCM，独立AAD绑定调用ID与输入/返回类别，密文独立存放。缺密钥不能开启新任务；已有加密记录缺/错密钥读取503，不返回假空文本、不自动生成密钥。开关关闭也不删除旧记录。
- 输入沿用现有256KiB业务上限；完整响应沿用1MiB上限，记录捕获是否完整/截断/未收到，不能把缺失包装成空成功。响应体即使不被研究采纳，也可保存真实已收到的业务材料与可读取usage；不记录SDK原错误/连接串/响应头。若回执回显已知连接凭据，须移除且标redacted，不能称未改原件；其他研究材料不承诺通用脱敏。
- 已知连接凭据也不得经已采纳Decision进入观察/事件/动作。供研究采纳的返回文本若命中这些凭据，清空可采纳文本并记`invalid_response`，不采纳、不派发；usage仍独立记录，开启原文时可保存已标`redacted`的回执。不能把URL/请求体/命令等字段删字后当作原提议执行；这不承诺通用研究材料脱敏。
- 原文接口Bearer/同源/no-store，与现有单管理员边界一致；只供当前合法归属显式读取，不自动下载或渲染HTML/外部资源。浏览器仅内存，复制是显式操作。

## 5. 用量、价格与时间

### 用量

- 仅解析完整且结构可靠的回执中真实usage，独立于模型内容是否为有效研究Decision。HTTP错误/拒绝/长度停止等有可靠usage仍可记录；损坏JSON/重复键/错类型/负数/溢出不冒充零。单字段最大10^12；输入+输出也须有界，精确整数运算。
- 统一 `input_tokens` 为含缓存的总输入，`output_tokens`为含推理的总输出，`total_tokens`仅在两者都已知时相加。OpenAI缓存读是prompt子集，reasoning是completion子集；Anthropic原生input不含cache_read/cache_creation，要按协议相加。保留provider原始计数与规范化分类关系，不重复相加。
- `cache_read_input_tokens/cache_creation_input_tokens/cache_creation_5m_input_tokens/cache_creation_1h_input_tokens/reasoning_output_tokens`均是可空子集。缺失不凭空补0；区分协议已知的不适用与供应商未上报。分类不一致标invalid，不以错误金额进入汇总。
- API所有tokens和金额使用十进制字符串；未知为null，不能用浮点累加或依赖JS安全整数。汇总已知部分须同时返回缺失调用数。

### 价格

- 精确匹配 `(source, model_config_id, protocol, model)`；managed的ID必填，environment的ID为空。不做模糊名称/跨配置/跨环境回退。配置改model/protocol后不套旧价格，旧绑定仍可匹配其原模型。
- 手工配置每百万tokens价格，币种USD/CNY/EUR，非负十进制字符串、最多6位小数、每项不超过1000000。输入普通、输出、缓存读、缓存写5m、缓存写1h分别定价；未知项留null，不默认免费。推理已含output，不再加价。
- 使用十进制定点算术，金额最多12位小数。快照固定在调用开始，之后编辑/禁用价格不重算历史，不对已无价格的历史调用补算。价格revision只作内部CAS。
- 只有所需计数、分类和价格齐备才给完整估算；不支持的缓存分类/额外计费模式、usage缺失或未配价格明确不可计算。可显示已知tokens，但不能拿部分费用假装完整费用。不得忽略多媒体/服务工具等明确额外计费后声称完整估算；首版为配置的文本token单价估算，不支持完整供应商账单规则或自动价格抓取。
- 汇总按币种分别累计，提供priced_calls/unpriced_calls，不能跨币种相加；失败/unknown不默认免费。

### 时间与范围

- 使用开始时间归属，跨日调用只计入开始日。时间筛选 `from`（含）与`to`（不含）为YYYY-MM-DD本地日边界，`time_zone`为有效IANA时区，默认UTC。最长93日，DST按命名时区日边界计算，不按24小时平移猜日期。
- 未指定日期默认最近7个日历日（含今天），响应回显实际范围/时区；项目任务汇总可查询全历史，不默默限制为7日。列表排序started_at DESC,id DESC，游标绑定筛选与当前上界，limit默认25、最大100；伪造/错筛选游标400。
- 只统计账本启用后的记录，返回coverage_started_at；不把旧任务无账本记录说成历史零消耗。空日为已知账本无调用，usage缺失日保留缺口，不能绘成真实零tokens。

## 6. HTTP契约（B组实施基线）

沿用既有鉴权、错误和严格输入校验。以下新增端点均不触发模型调用。

### 创建与读取记录开关

- `POST /api/explorations`新增可选`record_model_content: boolean`，省略=false；省略/false保持旧指纹兼容，true进入指纹。开关与任务/模型绑定/启动回执同事务。历史提交回放优先于当前密钥可用性。
- `GET /api/missions/{id}/model-recording` → `{mission_id,record_model_content}`，旧任务false，mission不存在404。
- `GET /api/system`增加`model_content_recording_available`布尔，表示本机密钥能力，不代表某任务已开启。

### 调用列表与详情

- `GET /api/model-calls`：query可选`mission_id,model_config_id,purpose,outcome,from,to,time_zone,limit,cursor`。purpose为research/connectivity_test；outcome使用下列枚举。mission不存在404，不允许以空集合掩盖归属错误。
- 返回`{items: ModelCall[],next_cursor: string|null,coverage_started_at: RFC3339,range: Range}`。
- `GET /api/model-calls/{id}`可选mission_id，核对归属，不符404。返回ModelCall；列表与详情均无原文、连接秘密、完整端点、原始错误。
- ModelCall固定字段：`id,source,model_config_id,config_name,protocol,model,purpose,mission_id,thread_id,attempt_id,test_id,started_at,finished_at,duration_ms,state,outcome,attempted,http_status,record_model_content,content_state,usage,cost`。无归属的ID与config_name用空字符串，时间/状态码/耗时/attempted未知用null。
- source=`managed|environment`；state=`running|completed|interrupted`；outcome=`pending|not_sent|transport_unknown|http_error|invalid_response|valid_response|interrupted`；content_state=`disabled|pending|available|partial|missing`。
- Usage：`{status:reported|partial|missing|invalid,input_tokens,output_tokens,total_tokens,cache_read_input_tokens,cache_creation_input_tokens,cache_creation_5m_input_tokens,cache_creation_1h_input_tokens,reasoning_output_tokens,provider_usage}`；上述tokens均string|null，provider_usage为经白名单校验的计数对象，无远端任意字符串。
- Cost：`{status:estimated|unpriced|usage_missing|unsupported,currency:string|null,amount:string|null,price_id:string|null,price_snapshot:PriceTerms|null}`。没有可信账单时不添加actual_cost。
- `GET /api/model-calls/{id}/content`可选mission_id核对归属，返回`{call_id,recorded,input,response}`。未启用recorded=false、input/response=null；启用时每项为`{state:available|missing|pending,body_base64:string|null,media_type,complete:boolean,redacted:boolean}`。input是实际业务system/user对象的UTF-8 JSON；response是有界收到的业务响应JSON/字节，不能从Decision重建。缺密钥503，缺失与空body不同。

### 汇总

- `GET /api/model-usage`接受列表相同筛选但无cursor/limit；返回`{coverage_started_at,range,summary,daily}`。daily按日期升序覆盖所选范围（最多93行）。
- `GET /api/missions/{id}/model-usage`同schema，默认全历史summary；daily仅对请求范围/默认最近7日，响应用`summary_scope:all_time|range`明确两者差别。传显式日期时summary_scope=range。避免为全历史chart无限填日。
- Range=`{from,to,time_zone}`。
- Summary=`{calls,running_calls,interrupted_calls,input_tokens,output_tokens,total_tokens,missing_usage_calls,invalid_usage_calls,estimated_costs,unpriced_calls}`。calls等数量为十进制字符串；tokens为已知计数总和，不能隐去missing_usage_calls；estimated_costs为`[{currency,amount,priced_calls}]`。daily每行`{date,...Summary}`。
- `missing_usage_calls`统计usage.status非reported的调用（missing、partial、invalid），`invalid_usage_calls`只是其中invalid子集，不是互斥分类，两者不能相加。例：reported/partial/missing/invalid各1次，calls=4、missing_usage_calls=3、invalid_usage_calls=1；尚在running/中断且usage未知同样计入缺口。
- 输入、输出、总tokens分别累加其已验证已知值；total_tokens只累计单次已知total，不能把汇总输入+输出拼成缺失调用的完整total。无效usage计数不参与数值汇总；partial可保留可靠已知字段并同时显示缺口。费用缺口与usage缺口也可能重叠，不相加成另一种调用总数。

### 价格

- `GET /api/model-prices` → `{items:ModelPrice[]}`，最多500条，真实上限冲突，不无限列表。
- `PUT /api/model-prices/{id}`：随机32位hex稳定ID，`{expected_revision,source,model_config_id,protocol,model,enabled,...PriceTerms}`。创建expected_revision=0、201；更新匹配200，冲突409。同一匹配key只允许一条；客户端失联先GET列表查原ID，不自动换ID或重发。
- ModelPrice含上面全部普通字段及`id,revision,created_at,updated_at`，不回传expected_revision。匹配key创建后不可改，改价格是更新字段；无删除。managed须验证真实配置身份且protocol/model来自该配置当前或不可变历史；environment仅为实际观察到的模型组合提供配置，不建立虚假provider。
- PriceTerms=`{currency,input_per_million,output_per_million,cache_read_per_million,cache_write_5m_per_million,cache_write_1h_per_million}`；input/output必填字符串，缓存项string|null。enabled=false不用于新调用，旧快照保留。无后台价格抓取、无真实计费验证。

## 7. 实施与验收分工

- 主独占docs、任务包、参考证据和整合；后端执行会话独占internal/cmd（包括domain新增类型、006迁移），前端独占web（含共享组件/展示与三入口产物）。契约变化先通知主，不能擅自让前端猜字段。
- 仍用`var/full-system-models.Pg2PL1Id/heavy.lock`作为全项目本轮重型锁，避免与保留预览专项冲突。两个新可见gpt-6-astra会话，不复活A组已退出CLI、不再委派。
- 后端用新tmpfs PG和loopback fixture验双协议、managed/environment/test全路径，真实usage与业务失败独立、缺失/错误计数、缓存/推理不重复、开关/加密/缺钥、归属隔离、价格更新/跨币种/DST/跨日、幂等/分页/重启不重放/持久失败。
- 前端以真实Go/新PG做保存开关→调用→图表/表格→单次原文与价格保存闭环；另验未知/部分/空/401/404/409/503、迟到响应、深链/任务隔离、秘密不持久、最大化和1440/1920/390浅暗、键盘/焦点和实际截图。图表加载dataviz，先选图再配色，验证浅暗色板，图表和表格可互查，不双轴、不用颜色单独表达。
- 用户预览51013保留，未经核验不强制升级/刷新；新B组后端需新独立展示实例，不在旧库直接迁移。无真实产品模型/外部目标/部署/commit/push。
