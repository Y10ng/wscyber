# H3-C：Cyber 原生 UI 到本机固定工具执行链

2026-11-14，分支`feat/foundation`。**已通过一条真实原生 UI→Hub→fixture-node→固定工具→页面结果/持久事件的端到端链。** 直接复用H3-A2固定binary/release，产品native/前端/上游均未修改，没有新建Harness、旧Decision循环或假API，也没有重新Go/npm构建。

## 实际验证

主在唯一准备会话正常退出后，以单个内部headless浏览器、1440×900、独立私有key/SQLite/HOME/短TMPDIR启动现有`--local-hub --fixture-node --ui-dir`。它不是生产模型/任意目标入口。

1. 原生表单登录与自然Cookie；从真实ListAgents确认唯一fixture节点及固定内存模型/config reload，不手工ready。
2. 在节点旁点击原生“新建任务”，UI自行生成session ID并通过原生AOP创建；同session历史与事件订阅就绪。
3. composer发送一次固定fixture文字；**OpenSession一次、RunTurn一次**，不刷新/重发同一尝试。
4. Cyber原生StandardLoop调用bash执行固定`/usr/bin/printf 'WSCYBER_H2B_NATIVE_TOOL\n'`，内存Provider第二次确实消费同call的工具结果。没有外部模型请求或动态命令。
5. 页面显示原生完成答复和“回复完成”；实际呈现是**Cyber Workflow执行回放**，不是备用AssistantResponse两级工具卡。实际点击其summary收起→展开→再收起；参数和反馈分别位于原生区域，输出只从`workflow-tool-result`内的`tool-result`断言，并核对属于同一tool node。
6. 原生Connect二进制ListEvents回读13条持久事件，与12条实时Watch事件按cursor/ID/session/turn/call及完整解码结构核对；sessionStarted在订阅前从历史读取，故13与12不同。恰一toolCall、一个非错误toolResult、一个completed turnEnded；关键事件来自fixture节点。
7. 关闭浏览器/Hub，原生fixture报告reload1、ProviderCalls2、SawTool=true；printf completed/status0/24字节，REPL关闭，无active，Profile/Runtime关闭。主独立核验进程消失、端口释放、私有runtime及短TMPDIR删除。

最终内部运行：Hub3956657/start9364525，历史地址127.0.0.1:37547；已关闭，**不可当当前访问入口**。driver/Hub均exit0，真正原生WebSocket升级101。主已实际查看5张阶段图；本片不构成完整视觉认可、窄屏/深色/全功能验收。

## 真实边界与仍未解决项

- **这是内存fixture，不是安全研究质量验收。** 原生工具实际执行，模型边界是预先限定的两次内存响应；没有真实目标、收费模型、任意命令或漏洞发现结论。
- 本fixture模式已有只读ConfigStore，GetConfig200，之前空Hub的缺配置红提示在此模式不出现；默认空Hub语义没有改变。
- AgentHello公告工具目录本次实际为空，但真实session工具调用成立。目录与运行能力的一致性仍待查，不能拿公告当授权或把目录缺口写成已修。
- guardrail pending返回真实`GUARDRAIL_FAILED/unsupported AOP namespace`，页面显示护栏未配置；没有伪造空待审或成功。完整授权出口/审批仍未接通。
- IOA三个GET仍404，本次随原生刷新出现9条console error，逐项保留；没有pageerror、未知页面请求或requestfailed。不能称console零错误/IOA可用。
- SyncArtifacts只允许新私有archive空上传/cursor0，真实空返回；它不是session过滤，也不是任意同步授权。本次一次，本地IndexedDB仅此临时context。
- 有6次浏览器后台CONNECT被精确目的地围栏拒绝；真正应用WS只连接本轮Hub，Open/Run各一次。测试HTTP/WS围栏不是产品出口控制、OS沙箱或全协议捕获证明。
- 未验生产Provider配置、任务授权/身份/业务前提、工具长进程强停、持久恢复/unknown不重放、旧业务迁移、发现/复测/报告/SDL；许可UNKNOWN未解决，未部署/分发/commit/push。

## 失败记录与仅测试脚本修正

本片保存3次内部记录，非首次即通过：

- `browser-evidence`：login阶段测试误要求Hello目录包含bash，Open/Run/ProviderCalls均0。改为如实保存公告目录，实际bash能力仍由Provider/原生工具事件/关闭报告严格证明；没有改产品来伪造目录。
- `browser-roster-semantics`：真实单次执行已完成且结果已显示，脚本却寻找不适用于该原生渲染分支的“1 工具”按钮。主看图并核对withWorkflows/Workflow/WorkflowToolContent后，改为实际执行回放收展及独立结果区断言；没有改UI或在同session重发。
- `browser-native-workflow`：完整链通过，13持久/12实时事件及5图留存。每次都用全新私有fixture；前次已完成的本地printf结果已知，不属于unknown外部动作重放。

准备方只做纯检查/语法，主实际运行36条纯协议准入和6条目录语义断言后才分别执行受影响链。未重跑全仓/上游/类型/构建套餐。354项正式基线、37固定输入在最终状态文档更新前均未变。

## 证据和最终展示

- 包：`var/cyber-h3c.TPdMlsRN/`，最终内部证据`browser-native-workflow/`。
- 原件：`native-list-events-response.pb`、`native-watch-frames.json`（base64容器保存未修改原生Envelope字节）；索引/截图不是事件主库。
- 主复核：`main-native-event-check.json`、`main-final-check.json`、`main-final-evidence-sha256.json`及`main-acceptance.md`。
- 用户要求前端轮次最后统一可操作展示；内部验收完成即关闭，不能将5张截图说成用户正在操作。最终展示另用独立包/新本机fixture，只生成一条已验证固定结果后保留给用户查看，不自动操作其页面；具体存活地址/归属以STATUS最新为准。
