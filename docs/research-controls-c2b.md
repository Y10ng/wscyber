# C2-B：研究控制与人工文本补充

2026-11-12；最小实施契约，接权见`var/research-controls-next.y4LD72b1/ownership-transfer.md`。当前实现/验证进度以STATUS为准，本文不自证交付。

## 实际参考与复用

- 本轮实际只读cyber任务入口HTTP200，无401/403/登录跳转；可见Tasks/New Task等白名单结构，拦截1次页面写请求。未打开任何已有业务任务，无截图/业务正文/字段值采集，未观察到可用的暂停/恢复/补材料同类完整流程，不推断其缺少这些功能。证据`reference-observation.json`。
- ZAP官方Active Scan Tab正文已通过普通HTTPS取得并实际阅读（内置WebFetch域名验证失败另记），URL https://www.zaproxy.org/docs/desktop/ui/tabs/ascan/ 。官方明确选中scan的工具栏分开start/stop/pause/resume；没有在途撤销/可靠回执保证。沿用对当前方向的明确控制，不照搬扫描进度、规则或stop含义，不引入ZAP依赖/代码。页面原件在本轮隔离`zap-source/`。
- UI-1、`ui/UIProvider`、`TaskModal`、`SurfaceControls`、`MaterialLibrary.MaterialText`、`SkillRecords.SkillCallFacts`和标准基础表单用例复用。缺口为控制工具栏/人工补充编辑与元数据记录：先新增共享`ResearchControls`、组件样例/coverage/SSR测试，再接正式ProjectWorkspaceView。网络/原键/草稿状态归App级会话，不放共享UI。不新增依赖或重做主题。

## 控制与可靠写入

- Thread公开PG实际`pause_requested`；`/api/system.max_steps`公开现有累计步数上限，不是可编辑额度。
- 新`POST /api/missions/{mission}/threads/{thread}/control`，`{command:pause|run|resume}`，必须有效Idempotency-Key。返回`{key,mission_id,thread_id,command,thread}`固定提交回执；实时状态另外GET原Snapshot。
- pause阻止后续认领、auto_run=false，在途真实状态保留；run启用原任务绑定模型；resume只解除暂停并允许原approved动作派发，不启用模型、不批准pending、不重放unknown。不清预算/绑定/历史，不重开concluded。旧控制端点兼容保留，复用同一事务内状态迁移函数。
- 原键锁→已有回执优先回放→归属/能力/状态校验→业务与operations同事务。回放不再检查当前模型可用性，不重新修改状态。键跨接口/对象/载荷冲突409。新GET仍复用`/api/operations/{key}`；核对key/method/path/resource_type/resource_id/status及任务方向，不能用当前状态相似代替回执。
- 失联、取消、401和迟到只转待核对，不自动写。404仅表示此刻无回执；可继续GET或用户显式同键同载荷重试。草稿、冻结请求和键仅内存，离页/关闭/重认证保留，显式退出清空；不承诺刷新恢复。原确定拒绝可编辑；unknown后的拒绝不解除不确定性。

## 人工补充与消费事实

- 新`POST .../supplements`：`{source,content,share_with_model:true}`；source是用户填写的来源说明，不当作已验证来源。界面默认未勾选“提供给后续模型研究”；须明确确认才保存此类研究材料，非秘密文本，不能暗中改变C2-A share=false。只保存、不自动启用研究、不扩大origins、不确认漏洞。
- 每方向最多16份；每份content 1–16384 UTF-8字节、source 1–256字节；正文累计65536字节，序列化上下文98304字节。拒绝非法Unicode/NUL，原文不改写/截断。无编辑删除，不覆盖历史。追加010保存原件/hash及调用关联，不改001–009。
- GET同一路径返回有界元数据列表（最多16条，不加载正文）；GET `.../supplements/{id}`显式正文；GET其`/calls?after=&limit=25`返回有界真实ModelCall事实和游标（最多100），均验证项目/方向/材料归属。普通Snapshot/列表/事件不增载材料正文。
- Routed在下一实际装配边界读取不可变补充放低信任Snapshot.user，不放system；在途已装配输入不改写。每次BeginModelCall同事务保存实际携带补充ID，核对归属、去重；失败零孤立调用/外发。保存不等于进入输入，Begin不等于已发送，attempted不保证供应商完整接收，更不保证理解/采用；not_sent、transport_unknown、interrupted沿用账本。原文归档关闭仍保留原件和最小关联事实，不保存完整提示词作为统计副本。

## 验收与停止点

定向域/模型/账本/API/研究回归，新隔离PG事务/并发/回放/归属/恢复/预算/unknown/输入关联专项；共享及正式React类型、测试、构建；新固定Go/React与loopback模型/目标做真实API及可见浏览器。浅暗最大化/1440/1920/390、长文本、键盘焦点、失联/401/导航/迟到、纯文本与无浏览器持久秘密存储。全部重型命令共用heavy.lock，失败留证。

保留41433固定产物/窗口/原入口父链；无真实目标/付费产品模型、业务库/部署、真实凭据/完整工作区、D–F、外部投递、commit/push、harness/Arena；一个主不委派。完成本片后写outcome及STATUS/system-delivery并交回停产品写入，不合称整个C完成。
