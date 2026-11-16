# D2：候选独立验证与结果回流首片

2026-11-12冻结必要契约，2026-11-13有界封存交回。**工程实现及受控证据已有；当前布局被用户明确否定，不算视觉验收或产品交付完成。已停止追加开发/界面验收，后续应优先学习优秀项目重构任务工作台、候选及验证展示；不自行在本轮重设计。实际状态、退出故障和未覆盖见STATUS§0.36及本轮outcome。**依据本轮双重交权及`var/verification-next.hQ12lW/reference-notes.md`的实际Cyber观察、ARTEX发布图/固定源码、DefectDojo官方流程及共享缺口。不是报告、修复后复测、TI-1/TI-2或完整D；不新增依赖/调度器。

## 用户终点与事实边界

候选详情显式“独立验证”→核对所读候选时点、原任务模型/授权和有限步数→保存并启动独立方向→新动作证据→保存支持/反证/不足判读→就地按需查看记录/新流量。原候选三段正文、编号及历史保持。并非重新跑原研究或把父ID拼进文本。

Verification具有UUID、mission/finding/source_revision/thread关联，固定不可变候选修订及其真实证据元数据；模型每轮取得同一份候选内容（不自动拉原研究全部聊天、流量/历史）。当前用户在途更新候选不改输入或旧结论。来源时间展示“验证依据：某时保存内容”，内部revision不作为用户配置版本管理。结果永不覆盖候选。

**本片不自动升级confirmed。** `supported`是独立执行后、通过来源和完成性校验的新证据所支持的模型判读，不是程序证明全部漏洞语义；`counterevidence`表达本次实证反证，不等于普遍不成立；`inconclusive`为不足。执行成功、结论、严重度/风险分开；无风险分级或修复状态写入。完整D的自动确认/人工复核流转仍为后续目标，没有永久删去。

## 执行、预算与可靠提交

复用同一Go研究Engine/Thread/Attempt/Action，独立方向是执行上下文，不是第二调度器/Agent服务。继承原Mission精确origins、不可变模型绑定、原文策略和材料分享；false资料不外发，不把候选文字当登录凭据。仅候选内容和引用时点＋该验证自己的有界历史/按需资料，无原方向补充/活跃Skill隐式拷贝。新动作经原Validate/策略/认领/执行保护；unknown不重放。

显式提交`{source_revision_id,max_steps}`，max_steps为1–8且不超过部署max_steps；UI默认最多6。每候选至多32次显式验证（容量拒绝不删历史），同一候选最多一个未结束的验证；未耗尽预算的受阻/暂停验证须先在原方向接续，不以换键新建重置预算。达到本次有限预算且无running Attempt、pending/approved/running/未核对unknown动作时，保存不可变`verification_ends(reason=budget_exhausted)`，这是执行终止回执而非模型结论；原结果仍为null。旧方向不可重开/追加动作，原证据和步数保持，用户可以显式发起新的有限验证。启动事务锁定原验证方向、再次检查在途/未决事实并串行裁定同一候选新发起，不靠UI解锁；暂停/未决unknown不能逃逸。原模型调用每Attempt最多一次；没有额外抽取/总结调用。原系统只有每方向步数上限，不宣称已实现项目tokens/费用总预算。每次新验证是用户显式分配独立有限预算，旧方向步数不重置。

POST与operations回执同事务，原键同规范载荷固定回放202，异载荷409，能力检查在回放之后。错归属404。要求source为创建时当前修订，过时409让用户显式重读，避免无意验证过时主张。提交成功后后续候选变更不影响本次。

暂停/run/resume沿C2-B：pause只阻止后续派发，不撤销在途；run不清步数、不重开已有结论；resume只释放原approved、不启用模型。浏览器关闭/取消读取不取消已提交执行，失联只GET核对原键，不自动重试/换键；本片无强行终止在途动作/撤销approved端点。失败/中断/预算耗尽保留独立记录和真实错误，未形成结论不是支持/反证；用户可在有限剩余预算内显式继续。恢复不重放可能已发送动作。已保存结果后Attempt收尾失败，结果仍可读且验证不能再次派发/重开。

## 模型结果与来源校验

新增Decision.kind=`verification`，载荷`verification:{verdict,method,preconditions,summary,limitations,evidence:[{kind,id,relation}]}`。verdict仅supported/counterevidence/inconclusive；method/summary必需≤4096字节，preconditions/limitations≤4096，证据0–16，整体≤24KiB；Decision≤32KiB。所有字段严格结构/Unicode/容量检查，不接受confirmed/fixed/自报来源身份。验证方向不能delegate/finding/conclude（结束用结构化verification），普通研究不能提交verification。

Store从可信Attempt找到唯一completed/valid_response/attempted且purpose=verification的call及实际装配；只接受本验证方向实际选入且hash匹配的新原件，不接受原候选引用/其他方向/捏造ID。supported/counterevidence必须至少一个对应关系的新完成且完整捕获的HTTP动作（真实响应、非unknown/截断），单个exit0或模型观察不能满足。**HTTP状态码不是漏洞或证据资格判据：500可含报错型证据、302可含重定向证据，200也不证明漏洞。** 程序只校验来源、归属、装配与捕获事实，业务支持/反证仍是有依据的模型判读；实际登录失效、不可达或前提不足要求inconclusive，但不仅凭401/403猜前提。未决unknown继续阻塞，不自动重放。已知失败/不完整尝试可作为limitation保留，不要求为了保存supported删掉限制或反证；这些材料不能单独满足主结论的完整新证据条件。引用真实不证明模型业务判读正确，界面明确“模型判读”。记录方法/实际前提/限制与所有支持反证关系，不靠单枚举确认。

新不可变result固定Attempt/call、判读和引用时点；同Attempt同载荷回放、不同载荷冲突。先账本Finish→结果持久化→Attempt收尾。保存失败停止新模型派发、不自动重做目标。原文关闭仍保存有限被采纳结果和来源，不旁路存完整prompt/返回信封。

## 持久化与API

仅追加013迁移，不改001–012。新增verification身份/不可变result，新增账本purpose=verification与上下文策略`bounded-verification-1`/固定来源；不改旧审计记录。验证沿原历史窗口/64KiB单项/96KiB历史/256KiB实际输入上限，固定候选也计入容量；Begin同事务校验身份/审计。列表不读取正文或原件。

- POST `/api/missions/{id}/findings/{findingID}/verifications`，必需Idempotency-Key，202 `{key,mission_id,finding_id,verification_id,thread_id,source_revision_id,start_status:"accepted"}`。
- GET 同路径：`limit,cursor`有界keyset，默认25/最大50；摘要含固定身份、来源时间、真实Thread状态/暂停/累计步数、end_reason（空或budget_exhausted）、是否有结论与verdict，不含完整判读。
- GET `.../verifications/{verificationID}`：按需读取独立元数据/固定候选修订身份、Thread、已保存result或null、最近尝试的execution_error，错误/下一步为真实运行事实。
- GET `.../verifications/{verificationID}/evidence/{index}`：核对完整归属及固定结果引用后读原件；相对于引用时点的变化明确标记，≤256KiB，不触发执行。
- 已有GET operations核对，resource_type=verification，202；已有项目/方向可靠control用于pause/run/resume。调用账本用途标独立验证，仍可按任务读取可选原文/实际装配。

所有接口Bearer、同源、no-store、严格未知/重复查询拒绝、错归属404；游标绑定mission/finding与稳定顺序，无总数/跨页一致快照保证。

## 前端与验收

先共享Verification组合/用例/覆盖，再接正式FindingsWorkspace；发起Modal复用TaskModal，列表与结果按需折叠，沿既有5秒串行有界读取/隐藏暂停/错误退避，阅读原候选不跳动。待核对键保存在App级内存（关闭/切页/401保留、退出清理），401/失活/切任务遮蔽敏感原文，关闭证据passive归还焦点。无新浏览器持久缓存、无日志/聊天壳或统计。

heavy.lock串行定向Go/domain/model/ledger/research/httpapi及隔离PG race、旧D1/取消锁/C2-B/C3-A关键回归；共享/React类型测试及正式构建。新隔离tmpfs PG和loopback确定性供应商/目标，经正式React显式发起→实际研究/新动作→保存结论→读取与独立核对；覆盖支持/反证/不足/失败、幂等失联、候选更新迟到、分享/原文/归属、暂停/unknown不重放，最大化及1440/1920/390浅暗和键盘/焦点。未验证和失败如实保留，不称真实模型质量/用户认可/部署。阶段新窗口展示后不导航强刷，专项另context。完成单片交回停写。
