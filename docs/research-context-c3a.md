# C3-A：有界研究上下文与实际装配记录

2026-11-12，本片已接通Go/PG/正式React并完成受控本机验证；实际结果、失败与未覆盖以STATUS§0.29及`var/context-next.oxe6f3cw/outcome.md`为准，不代表真实供应商研究质量、用户认可或完整C。

## 实际参考与复用

- 本轮实际只读访问cyber任务入口，HTTP200、无401/403/登录跳转；白名单可见Overview/Tasks/Models，拦截1次页面写请求。未打开已有任务、读取值/业务正文或截图，因此未观察到上下文详情流程，也不能推断该功能不存在。证据在`var/context-next.oxe6f3cw/reference-observation.json`。
- 实际取得并阅读Langfuse官方trace explorer正文：按实际请求定位调用、输入/输出和元数据/关系；沿用“调用→按需详情”，不接入外部观测服务、不复制源码/品牌、不发送项目材料。
- 实际取得并阅读LangChain官方short-term-memory正文：调用前保留有限历史，trim与summarize是不同策略；官方明确裁剪可能丢失信息。沿用确定性有界选择，不采用会新增模型调用的摘要、删除持久历史或框架第二状态。本轮不新增依赖。公开Cookbook返回403、LangChain首次直连无网络的失败保留；授权进程代理一次获取成功，不关闭TLS。下载各在新空目录，未执行下载内容。
- UI复用`ModelCallDetail`的实际调用详情/取消机制、`SkillPager`、统一UIProvider、GridFold和纯文本；新增共享`ContextAssemblyView`及标准样例/SSR覆盖后接业务。无新主题、图表、KPI或常驻说明墙。内部策略标识不成为用户版本选择。

## 调用链与兼容

旧Engine研究路径先调用无LIMIT的Snapshot，Routed再加载配置。正式新路径先仅读取Mission/Thread作用域，Routed在PG同一RepeatableRead事务按当前任务/方向/正在运行Attempt重新装配全部来源，清掉调用者预填。环境模型、managed Chat及managed Messages共用同一装配及账本；一次Attempt仍最多一次模型调用。

普通`GET /api/threads/{id}`和动作执行旧Snapshot保留完整历史语义，本片不冒称已修复其分页/容量。新模型路径不通过旧Snapshot读取历史，不全表读取后截断。

## 选择规则与上限

- Mission/Thread可变文本合计先检查64KiB，不读取超限正文。目标、授权、当前问题/下一步、已固定且获准的任务材料/附加清单/分享身份说明、当前活跃Skill/最近Skill活动、已保存人工补充保持既有规则，全量各自有界读取。普通未选材料和share=false说明不装配；不自动跟随引用。
- 历史候选按`created_at,id`（Attempt用`started_at,id`）降序：观察最近32、动作最近8、尝试最近8。每类多读1条**元数据**只判断是否存在更早范围，不扫描全历史算总数；记录窗口截止与`older_exists`，未读总数为未知，不编造计数。最终内容仍按时间升序供模型消费。
- 另外优先保留最多16条明确类型`counterevidence/contradiction/refutation`的观察，以及最多16条pending/approved/running/outcome_unknown动作（含已核对unknown），各用部分索引有界读取。超过上限停止本次装配/零模型调用；最新观察、最新动作和当前Attempt也是必需项。自由文本不自动被分类为反证；窗口外可能包含未分类反证，模型和用户均看到“更早历史未读取”，不得声称完整研究结论。
- 单项候选先读ID/时间/状态/原始字段字节数；原始字段超过64KiB不读取正文。必需项过大明确失败不调用；非必需项保留`item_limit`未纳入引用。内容读取只按已选有限ID且同事务进行；序列化历史累计96KiB。必需项优先、余项从新到旧整条放入，超出记`history_budget`，不截断条内字节、不删原件。整个模型输入（system+user）仍256KiB；固定资料组合或JSON转义导致不足则明确失败，未调用，不自动丢材料或另调摘要。
- 只读事务设5秒时限；历史查询成本为索引范围定位+有限候选，单项/总字节受限。数据库内部对有限候选的长度检查不等于零存储IO；不声称恒定延迟或已验证长期容量。

## 实际装配与发送分层

装配记录只含来源类型/ID、观察到的状态、既有原件hash（仅已有hash的材料）、时间、字节数、纳入/未纳入理由、历史窗口范围和输入字节总数，无业务正文、摘要或完整prompt hash。它绑定实际装配内容，经同一个Begin事务与call、Skill/Supplement关联落盘；Begin失败零供应商调用。保存后新增补充或Skill释放不追认或改写该记录，调用关联只取当次已装配集合。

每个call最多一份不可变装配记录。原文关闭仍只保存上述最小事实；不旁路记录完整输入。发送事实仍以账本attempted/outcome为准：已装配不等于已发送，attempted不等于完整接收/理解；interrupted/transport_unknown不自动重放。装配前失败没有call，方向保留明确失败原因，不伪造调用。

旧调用不补造装配记录，返回`recorded=false`；连通测试不适用。追加迁移011，001–010不变，不迁业务库。

## 读取与正式界面

`GET /api/model-calls/{id}/context?mission_id=...`按原call鉴权/归属/no-store，返回call_id、mission_id、thread_id、recorded及assembly（未记录为null）。单份有界（最多160来源、96KiB JSON），并非历史正文接口；详情列表每页20项本地分页，基于不可变单份记录稳定翻页，不跨call积累。普通call列表不加载装配内容。界面从模型调用详情显式打开，展示真实来源引用、纳入/未纳入原因、更早未读范围与发送状态，不重建旧prompt。

历史正文使用已有显式原件路径，不在新详情默认请求；本文不承诺为每类历史新增单条正文API。关闭/换call/项目取消并卸载，401遮蔽，迟到和错归属拒绝，503显式重读，纯文本不执行/自动外链，退出无持久存储。

## 本轮验收范围

定向Go/PG/React，三模型路径两协议×原文开关，长历史/超大单条/必需反证与unknown、预算、归属、装配后变化、Begin/Finish失败和恢复；实际供应商fixture收到的来源与账本一致，旧原件不变。正式Go/隔离PG/React联调和浅暗桌面/390/键盘/401/迟到按实际执行记证据。不扩真实身份/工作区/D–F或真实模型研究质量。
