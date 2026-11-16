# D组首片：增量候选与真实证据关联

原实施契约标注2026-11-13 UTC，历史时间保留。**本片Go/PG/正式React已完成接续复核、两项定向修复与受控本机验收，交回及主入口独立整合见STATUS§0.34；最终入口36417。** 已核对35项PG race、React161、真实运行中更新/分页/证据与浏览器异常；不代表用户视觉认可、真实研究质量或长期稳定性。旧35283直接退出原因仍未完全证实，具体失败/未覆盖见交回outcome。 原实施主`wscyber-findings-main-hkanpgeh`的接权是历史记录；本轮归属见`var/finding-resume.bR2aXS/ownership-transfer.md`。不是完整D，也不是独立验证、报告或复测。

## 参考、复用与用户终点

实际只读cyber任务入口可见Vulnerabilities导航；点击后白名单仍为Tasks，不能确认发现列表/详情加载，未打开已有业务记录。实际取得并阅读[DefectDojo官方Findings](https://docs.defectdojo.com/asset_modelling/engagements_tests/os__findings/)归属、稳定ID/状态、描述/复现/修复、request-response pairs、View History及reimport限制。ZAP路径404不作依据。来源、hash、观察/未知、采用/偏离和组件复用表在`var/finding-next.hkanpgeh/reference-notes.md`。

本轮接续另补ARTEX同固定commit的任务内findings-tab源码及官方仓库findings/sessions发布截图，已实际查看，来源与差异见`var/finding-resume.bR2aXS/reference-notes.md`和[界面参考](ui-reference.md)。发布图不等于亲自运行ARTEX详情，含demo内容不证明研究质量；不复制未实现统计、严重度或管理状态。

沿用项目内列表→简洁详情→按需历史及真实来源关联，不引入平台/依赖，不采用“本轮没发现即关闭”、标题指纹自动合并或全套管理菜单。视觉遵循UI-1和既有共享组件；先补`ui/Findings.tsx`列表/三段正文/证据与历史组合、可点击样例和检查，再接正式React。复用GridFold、SkillPager、统一Ant基础用例及现有ActionEvidenceDrawer/字节查看器。不另造主题、图表/KPI或常驻说明，不增加人工编辑表单。

用户在研究未结束时可看到候选和同编号后续更新；暂停/失败后仍可读。描述、复现步骤、修复建议为主正文，候选状态在页头，限制/支持/反证/历史按需查看。支持多个候选引用同一原件；引用真实不证明主张正确。

## 决策与权威

- 新增`kind:"finding"`及唯一`finding`载荷，每次Attempt最多采纳一个候选创建或一次更新，不与其他kind载荷混用，不额外调用模型。
- 载荷`{finding_id?,expected_revision?,content:{title,description,steps:[{text,evidence_indices:[]}],remediation,limitations,evidence:[{kind:"action"|"observation",id,relation:"supporting"|"counterevidence"|"limitation"}]}}`。创建不提供ID/版本；更新必须同时给本方向已装配的候选ID和所读内部版本。平台生成稳定UUID与部署内`WSC-`数字编号，编号不回收、可有空洞，不代表确认或CVE。模型无编号/状态/严重度/验证人/归属写入权。
- 标题1–256、描述1–8192、修复建议0–4096、限制0–4096 UTF-8字节；步骤1–16、每步1–1024；证据1–16、同kind/ID不重复，步骤索引只指本载荷有效证据且不重复。整个finding载荷≤24KiB，原Decision≤32KiB；拒绝无效Unicode/NUL/坏ID/枚举/混合载荷和超限，不静默裁剪。
- 仅可信Engine传Attempt ID，Store从其归属与唯一completed/valid_response/attempted研究call取得真实调用来源，不接收模型自报call/mission/thread。引用须在该call实际装配的来源中，或来自该call已装配候选的固定证据集合；不能引用其他任务/方向、未装配材料或捏造ID。直接来源需匹配新策略的结构化来源hash；发生变化拒绝而非偷换依据。
- PG为权威；同Attempt/同载荷固定回放原结果，异载荷冲突；目标候选行锁＋expected_revision阻止迟到覆盖。编号/内容历史/来源/证据时点事实不因重开或更新改写。一次保存事务同时落身份、不可变修订与关联事件；不把模型自然语言日志当关联。
- 账本Finish成功后才采纳候选；候选事务成功后再FinishAttempt。后者失败不回滚已保存候选、不重放模型/目标；恢复保留候选并将遗留Attempt按原规则中断。候选保存未确认时停止后续模型派发，先核对，禁止以换Attempt自动补重试。

## 持久化与审计

只追加012迁移；001–011、旧原件、已有审计不改。新增findings稳定身份/当前修订指针、finding_revisions不可变正文/证据元数据、追加events的结构化finding/revision引用。数据库唯一键和约束保证一次Attempt一条修订；禁止更新/删除历史或回收身份。没有删除/合并/确认接口。

证据保存kind/ID、关系、来源方向/尝试、产生时间、引用时点、结构化原件hash，以及动作真实状态/请求hash/捕获限制等元数据；不复制请求/响应字节、不存完整prompt/provider信封或凭据充当发现历史。正文是被采纳的有限研究成果，原文开关关闭仍可保存它，不等于开启模型原文归档。列表、事件和日志不包含整篇正文/流量。

历史页展示实际创建/更新时点、来源Attempt/call与对应结构化事件；修订详情可查看当时内容和引用，按需读取真正已有原件。原件状态后续发生变化时保留历史事实并明确当前读取差异，不把当前状态回填旧版。不是完整探索图谱或独立验证时间线。

## C3-A有界输入兼容

- 新正式装配策略`bounded-history-findings-1`，保留旧`bounded-history-1`读取/验证；旧call不倒填、不升级。新策略增加finding窗口/来源；ID为不可变修订ID而非模型自报编号。
- 当前方向最近8个候选的最新修订，元数据LIMIT 9仅判断更早范围；最新候选必需，其余可选。正文同一只读快照按选中有限ID读取，纳入原64KiB单项、96KiB历史及256KiB实际system+user总预算；不额外全量读取候选。原观察32/动作8/Attempt8与显式反证/unknown必需规则不变。
- 每份候选包含有限正文、稳定ID/编号/内部版本及固定证据元数据；未读更早候选可能包含反证/限制，不声称完整。模型只能更新实际装配的候选；无自动搜索/摘要/补调用。
- 新策略直接观察/动作来源增加结构化对象hash以固定被引用的实际输入时点；不是完整prompt hash。候选来源hash绑定不可变修订，Begin事务核对同方向归属/固定hash。来源总160、单份装配JSON96KiB仍有效，普通Snapshot/API不偷偷加候选正文。

## 只读HTTP与界面

均沿用Bearer、同源、no-store和归属校验，未知/重复/非法查询拒绝。项目/对象不匹配404。所有新集合keyset分页，默认25、上限50；游标绑定项目/方向/对象和不可变顺序，纯标识不含正文/秘密；没有总数或跨页一致快照声明。

- `GET /api/missions/{id}/findings?thread_id=&limit=&cursor=`：按稳定编号倒序，仅摘要/当前修订身份和时间，不读全部正文。更新不改变列表顺序。
- `GET /api/missions/{id}/findings/{findingID}`：当前不可变修订及真实身份/来源/固定证据元数据。
- `GET /api/missions/{id}/findings/{findingID}/summary`：只投影单条摘要/当前修订身份；打开详情时仅轮询它，列表停止轮询。不因列表翻页/新记录挤出当前页而漏掉正在阅读对象的更新，也不轮询整篇正文。
- `GET /api/missions/{id}/findings/{findingID}/history?limit=&cursor=`：按内部修订倒序的有限元数据，不含正文，返回来源Attempt/call/事件ID。
- `GET /api/missions/{id}/findings/{findingID}/revisions/{revisionID}`：精确历史正文，不由用户编辑或管理版本。
- `GET /api/missions/{id}/findings/{findingID}/revisions/{revisionID}/evidence/{index}`：先校验全部归属及固定引用，再按需读取实际action/observation，单份响应≤256KiB；原件变化返回差异事实，不触发网络执行。HTTP action复用既有报文检查器，观察纯文本展示。

正式项目新增“候选发现”页签，不放独立验证/报告/复测/导出/删除占位按钮。复用现有5秒非重叠读取节奏，页面隐藏/页签离开/创建Modal暂停；单次只读当前页。打开的详情保持固定已读修订和滚动，列表发现更新只提示“查看更新”，不覆盖阅读。翻页/筛选/切项目/切候选/关闭/401失效旧响应，错误保留旧值且就地提示；400/401/403/404停止自动读取，其他失败有界退避，不密集轮询。历史/证据仅显式展开读取；不自动外链或加载远端资源，不写浏览器持久存储。

## 验收及停止点

heavy.lock串行定向Go/PG race、React/共享检查及正式构建；新隔离tmpfs PG/loopback供应商与目标fixture，environment/managed-chat/managed-messages覆盖两协议×原文开关。真实正常研究链产生候选→同编号更新→历史/证据→后续有界输入与真实审计，不用SQL灌入替代成果证明。

专项覆盖并发编号、同Attempt重试/异载荷/并发修订冲突、错归属/假引用/超限、原文关闭、保存后收尾失败/恢复、pause后读取、旧修订不变、旧迁移兼容及C2-B/C3-A/账本回归。浏览器阶段新窗口及时展示，最大化与1440/1920/390浅暗、长内容、空/加载/失败、复制/键盘/关闭/401/迟到；实际Read截图。失败/未覆盖如实留证，不冒充真实模型质量/部署/用户认可。仅做本单片，交回文件＋明确消息后停止共享写入。
