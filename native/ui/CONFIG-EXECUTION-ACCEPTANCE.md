# 原生页面配置到执行：受控用户流程验收

2026-11-14，`feat/foundation`。**配置→节点当前报告→创建/发送→真实工具结果这一条本机用户流程已通过；完整产品、真实研究质量及最终UI质量未完成。** 本轮不重跑H1–H4或全仓测试，也没有旧业务迁移、部署或Git提交。

## 本轮实际改变

- 后端仅通过原生RunWebSocketWithCapabilities添加`wscyber.local-model.v1`模式标志，原生配置/会话/工具循环/事件协议不重造。
- 新增可追溯逐文件UI源码覆盖，离线复制既有依赖到新构建目录，不重装依赖、不改固定原始来源/锁。
- 13个现有前端源文件适配本机配置、节点事实、执行预算与不支持入口。静态复核后修正已有LLM字段丢失、主状态误称已应用、懒创建旧闭包节点绑定、必填key及显式continue保护/非LLM页签残留。原生Workflow结果直接复用。
- 源码覆盖清单SHA256：`7fcff10e5069dc8bedb2ce7a0fc265303804f543893eacd3868a8339335d7ca9`。

## 实际构建与一条用户流程

三个专属执行会话均已正常exit0、原身份消失后，主串行执行唯一Hub build、离线fork、完整tsc及Vite，均exit0。没有npm ci或全量测试。构建证据：`var/native-workflow.8QkW9tkn/main-build.lxJxXrKz/`；真实新public为同包`ui-work/release/public`。

- Hub SHA256：`b810b377132f47f2929bf60561ddafd8902ad44713a2904378603f06e6ba398f`。
- UI public manifest SHA256：`cb0b479d87c45963e4b965fb169c20c7931e41238ae03bddf2db4e48e24c9c3a`。
- Vite6.4.3、4684模块，13.48秒；既有Browserslist过期和约2.13MB主chunk警告保留，未为消警告升级。

主按`/run`要求启动实际新Hub与真实浏览器，不以import/测试替代运行：

1. 原生表单登录，真实节点握手含模式标志；初始0模型TCP/HTTP。
2. 页面填写本机fixture端点/模型/密码并保存一次；原生ConfigService真实落盘，节点报告provider/model且无configError；此时仍0模型TCP/HTTP，无隐式probe。
3. 页面创建一个会话并发送一次；原生RunTurn预算3、无eval/continue扩展。
4. 恰两次模型SSE，一次真实printf工具，第二次请求消费同call的真实结果；真正completed，不以Accepted当成功。
5. 原生Workflow收起/展开、参数和结果对应同tool节点；同session持久回读13事件，12条持久实时事件完整对象/ID/cursor相符，另1条非持久delta。未关闭会话用于预览，不把13与旧含SessionEnded的14混同。
6. 主实际查看4张2554×1182受控截图：配置、节点报告、结果收起及展开。无API mock、无假业务数据；这里只是显式本机模型协议fixture，usage也是fixture返回，不是供应商计费。

真实运行包：`var/native-flow-check.WNIqotGR/flow-jl0ib935/`。`running.json`表示验收已通过且预览保留，**不是最终资源清理结果**。真实console保留9条IOA三路径404，原生guardrail未配置；不能称零console错误/完整相关能力。

## UI审核并非随工程通过

主用已有agent-browser0.42.0及内置axe-core4.12.1在另一个独占只读浏览器中读取现有fixture，不创建/发送/保存/测试；用户预览不动。

- 工作流：工具标出6类violations、3类incomplete。需区分实际对比度/标签/landmark问题、best-practice和待人工判断，不把类别数当全部已确认漏洞。
- 配置面板：续审完成，2类violations（5处对比度、重复banner landmark），无incomplete。看到已保存配置和“LLM configured”，不把初始“未配置”截图误判为保存后错误。
- 第一次配置审计停止是主对CLI eval表达式用法误判：传箭头函数未调用，返回{}；保留原失败，仅续设置面板，未重跑模型执行。
- 独立工具出现的历史页`.send`异常未算产品缺陷：实测该工具域限制注入后的WebSocket.OPEN/CONNECTING为undefined、prototype.OPEN仍为1，constructor为检查URL后new OrigWS的包装函数。原生AOP的`socket?.readyState === WebSocket.OPEN`会受此破坏。没有关闭域限制或修改产品迎合工具；该工具模式不能直接证明AOP功能失败。
- 两审计浏览器正常关闭、对应runtime删除，原daemon父PID也已消失；工具环境探针已关闭清理。其一次清理匹配把自身shell误认成候选，未发送信号，改按真实comm核验后完成。

具体待修项与工具干扰证据见运行包`main-ui-review.md`、`ui-audit*`和[审核工具取舍](../../docs/frontend-review-tooling.md)。配置面板比例/常驻技术说明仍与用户要求存在差距，不能称全站或视觉认可。

## 用户预览与边界

本轮曾保留同一真实窗口`http://127.0.0.1:33901`，现已结束，不再在线。最终driver因preview的ListSessions围栏拦截及console error退出1；先前UI/执行验收仍passed。page_close_observed/context_closed为true，不推断谁关闭或拦截与关闭因果。Hub exit0、节点排空、四个原身份消失、端口可重绑；失败私有原件保留。准确事实见运行包`result.json`、`main-ended-check.json`、`LIVE.md`和STATUS最新，不用旧running/main-live-check快照声称仍在线。

仅当前fixture记录可展开回放/查看配置与节点，额外任务/保存/探测由本轮验收围栏拒绝；不是可反复研究或真实供应商入口。就绪后没有自动导航/刷新/重发或展示总时限；用户关闭专属窗口后才由监督器收尾，并以最终result.json核清理。

原有Web/API业务任务/授权/真实流量、候选/验证、报告/复测、模型用量/原文/资料Skill等尚未完成Cyber接入。真实目标、生产provider、工具隔离/出口、完整恢复/unknown、许可分发和部署仍未交付；当前临时限制不是最终产品需求削减。
