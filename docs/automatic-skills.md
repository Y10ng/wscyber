# S1：辅助Skill自动发现、加载与事实关联

2026-11-12，最小技术契约冻结；实现/验证状态以STATUS为准。仅文本辅助，不是执行型插件或固定研究流水线。

## 参考与共享复用

实际复核C1已取得的Agent Skills规范正文（`skill-spec-readable.txt:571–579`）：名称/说明先行、正文按激活加载、资源按需。本轮必要差异：显式分类代替文件名/frontmatter授权；全量启动元数据改成模型自主请求的有界分页，不执行附件/脚本。沿用C1实际cyber只读观察（未找到匹配入口）和Open WebUI官方知识库流程，不假称本轮重新访问登录UI。

已核对共享`MaterialLibrary.tsx`、`materials.ts`、`TaskModal`、component-preview的MaterialExamples/coverage以及正式LibrarySession/调用账本。先补共享分类编辑与使用记录组合、样例和覆盖，再接业务；复用14px浅暗主题、分页、纯文本、受控弹窗及原有401/unknown状态机，无新依赖、图表或假KPI。

## 分类兼容

`LibraryItem.classification`为`material|skill`；folder恒material。`LibraryWrite.classification`可选：创建省略为material，修改省略保留原分类；显式skill须非空description（适用说明，仍最多1024字节）。新字段omitempty不改变旧省略请求的序列化指纹和历史回执，旧数据默认material；SKILL.md/frontmatter不自动转换。旧任务材料及其请求身份不变，普通资料仍显式选用。

## 只读研究活动

Decision新增`kind=skill_discover|skill_load|skill_release`，统一`skill`载荷但严格拒绝不适用字段：
- discover：`{query?:string,after?:id,limit?:number}`，默认10、最多20；query UTF-8最多128字节，无NUL；id升序keyset，查询只匹配Skill名称/说明，不读取正文、不返回普通资料/目录元数据。
- load：`{item_id,source_token,purpose}`，稳定ID+完整CAS身份；purpose非空UTF-8最多1024字节。平台以触发Attempt ID（现有36字符UUID文本）作逻辑加载ID，区别于32字符hex的资料ID/token，不信模型自报成功。
- release：`{load_id}`，仅本方向活跃加载退出上下文，保留历史。

三者消耗现有一次Attempt/研究步数，先一次模型Decision，再持久只读活动，下次正常Attempt消费。没有内层模型调用、自动重试或强制先读Skill。不产生Action、不进入目标审批、不执行脚本/依赖/引用链接。Skill和目录都在低信任Snapshot，不能扩权。

## 持久原子边界与容量

追加008，不改001–007。`skill_activities`按attempt唯一保存请求hash和不可变结果；同attempt同请求回放优先于running检查，不同请求冲突。`skill_loads`固定成功来源/路径/说明/正文/hash、项目/方向/Attempt/目的/时间，不FK实时库；与活动和活跃引用同事务。分类/编辑/删除/加载共用原库锁，方向锁串行活跃集变更。

结果状态：discovered、loaded、released、unavailable（不存在/普通/已删均不泄漏）、stale、capacity、not_active。数据库/完整性失败是错误而非伪造正常失败结果。完整正文读取并校验后才计loaded。当前方向最多4个活跃加载、原始正文合计65536字节、序列化活跃集最多98304字节，超限明确capacity，不暗中淘汰/截断；释放后可继续加载。重复携带不计，新Attempt明确再次加载可产生新记录。

运行时只读取当前方向活跃原件和最近一次活动结果（有界目录），历史不全部注入；普通Snapshot API不带Skill正文。原有任务材料/清单独立保留。最终Snapshot仍最多256KiB，实际system+user仍最多256KiB；超限发送前停止。加载成功仅表示已持久提供给方向上下文，不保证后续请求发送、模型理解或执行指导。

## 模型调用事实

路由装配实际active load IDs，并传入B的BeginModelCall；同事务核验加载归属/原件身份并写`model_call_skills(call_id,load_id)`。无成功Begin不得外发；关联不从事后活跃集猜测。一次Attempt仍最多一条模型调用。原文关闭仍保留Skill固定原件/最低使用事实，不保存整个组装提示词/返回。

not_sent、attempted、transport_unknown、interrupted沿用B真实账本；Begin不是已发送，attempted不证明供应商完整接收。恢复不重放活动/调用、不补造计数。Skill活动事务异常接入账本门闩并停止后续模型派发；已保存活动即使FinishAttempt失败也保持，不在该轮重试。启动恢复标记旧Attempt为interrupted、方向为blocked，保留auto_run偏好但不可自动认领，不重放加载。

## 正式只读API（同源Bearer/no-store）

- `GET /api/missions/{id}/skill-usage?after=<itemID>&limit=25`：`{mission_id,total_successful_loads:string,items:[{item_id,name,successful_loads:string}],next_cursor}`，group来自不可变成功明细，名称取最近加载快照；最多100/页。
- `GET /api/missions/{id}/skill-activities?after=<decimal-sequence>&limit=25`：`{mission_id,items:SkillActivity[],next_cursor}`，seq升序、最多100；包含发现/加载成功失败/释放，结果无正文。
- `GET /api/missions/{id}/skill-loads/{loadID}`：固定加载详情+content，必须所属项目；跨项目/不存在404，校验hash/字节。
- `GET /api/missions/{id}/skill-loads/{loadID}/calls?after=<callID>&limit=25`：`{mission_id,load_id,items:ModelCall[],next_cursor}`；有界且检查加载和调用同属项目。

项目“研究配置”内“Skill使用”页签按需打开真实按Skill成功次数与活动明细，就地看固定原文、方向/目的/来源以及实际调用状态。配置列表不是使用史，浏览器GET无计数副作用。异步按项目/对象身份隔离、401遮蔽、关闭后迟到正文不复活；不叠加弹窗。分类/适用说明在共享编辑器维护，旧草稿及提交回执保持。

## 验证边界

只新隔离tmpfsPG与loopback确定性供应商，按影响验证兼容迁移、普通资料隔离、失效/并发/去重、活跃上限/释放、历史保真、原文开关、call_id和not_sent/unknown、持久失败/恢复。正式Go/React新独立预览，旧四端口/父链/用户草稿不动。fixture行为不是研究效果证明，失败证据保留，不把单测/构建/展示写成用户认可。
