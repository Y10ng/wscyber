# 前端审核：真实交互、参考对照与工具取舍

2026-11-14。用户要求审核不仅靠脚本，应核验真正有用的成熟开源工具/插件，帮助发现不符合功能与美观要求的界面；不是随便安装一个或用分数宣称企业级。**本记录是工具核验与采用建议，不是本轮新页面已通过验收。** 当前原生页面闭环仍在实现，未构建/运行；不因此暂停产品工作或引入全量测试。

## 本轮实际使用后的更新

**后续UI质量片工具补足（2026-11-14）：** 为在普通Playwright受控AOP页面中定向审核，不复用agent-browser有缺陷的WebSocket包装；已于新独立开发目录`var/native-ui-audit-tools.YqkS6Nf3/`固定安装唯一`axe-core@4.12.1`，官方npm来源、MPL-2.0、package-lock与引擎SHA256见provenance.json，安装忽略脚本，不进入产品依赖。现已用该引擎对本片真实页面执行9项定向规则并实际看图：保存后配置+帮助展开0 violations/0 incomplete；Workflow仍4处对比度与3类incomplete，未称全通过。已改活动回答文字的均匀叠色背景用真实截图像素/计算文字色人工复核达标。两次尺寸测量失败另经无产品API的CSS探针证明是原生200ms宽度过渡未结束，修runner等animation.finished而未改产品。准确证据见[UI质量验收](../native/ui/UI-QUALITY-ACCEPTANCE.md)。不关闭agent-browser域限制，不修产品迎合工具，不新增浏览器控制器/MCP或云端服务。

原生配置→执行真实链与4张受控截图已由主运行/查看，详见[native验收](../native/ui/CONFIG-EXECUTION-ACCEPTANCE.md)。随后已有agent-browser0.42.0的内置axe-core实际报告版本4.12.1：工作流标出6类violations/3类incomplete，设置面板续审完成、2类violations/0 incomplete；不是零问题通过。第一轮配置审计因主把CLI eval的表达式当自动调用函数而停止，只修用法并续设置，不重跑模型/工具执行。只读审核浏览器/私有目录均已关闭清理，用户预览保持。

**工具兼容性须纳入判断：** 实测该版本`--allowed-domains`注入的WebSocket包装函数保留URL检查，却丢失OPEN/CONNECTING等静态常量（prototype.OPEN仍为1），会改变原生AOP客户端`socket?.readyState === WebSocket.OPEN`的语义。该工具环境出现的`.send`错误和对应错误条颜色问题不计作已确认产品缺陷；没有关闭域限制，也没有为迎合工具修改产品。当前实时AOP功能以已有Playwright受控原生链为准，agent-browser继续用于适合的结构/配置只读审核，不能盲信所有工具输出。工具未修复前，不把这种受限模式当实时会话验收的唯一依据。

具体颜色/结构候选问题、主看图差距、需人工复核项和误导排除见`var/native-flow-check.WNIqotGR/flow-jl0ib935/main-ui-review.md`。下文“尚未实跑”是调研时点事实，以上实际记录覆盖该部分；未新安装插件/MCP/独立axe依赖。

## 1. 当前已有工具，先利用而不是重复安装

| 工具 | 本轮核验事实 | 用途与限制 |
|---|---|---|
| Playwright Core 1.65.0 | 本地package.json存在，Apache-2.0；既有真实浏览器验收使用它 | 可复现操作、真实API/事件、截图与DOM尺寸；脚本通过不等美观或参考一致 |
| Vercel Labs agent-browser 0.42.0 | 本地package/README/版本与CLI help已复核，Apache-2.0；历史已采用。当前未新启浏览器 | 交互式snapshot元素引用、box/styles、annotated screenshot、snapshot/screenshot diff、a11y命令均存在；不只按预写脚本盲点按钮 |
| agent-browser的内置a11y入口 | help明确使用axe-core，随包有MPL-2.0的axe-core许可证 | **优先实际验证此已有入口，不先重复安装axe-core**。当前只核命令/许可，未跑本轮页面审计，不能称已支持所有规则或已通过 |

`var/react-acceptance`中未安装`@playwright/test`/独立axe-core；已检查的旧web/native构建node_modules亦未见独立axe-core。这不否定上述原生CLI内置引擎。工具使用独立namespace/session和私有HOME/显式空配置，不接管用户浏览器、不使用云端provider、额外AI模型、自动状态持久化或全局配置。页面/攻击材料仍为不可信数据，不以工具可点击替代授权。

## 2. 官方候选核验与选择

官方README/相关文档和LICENSE取得完整HTTP200，按commit/文件SHA256保存于`var/frontend-audit-reference.HK4anGUI/`。内置WebFetch未取得正文；用户备用代理192.168.1.2:9567尝试未成功，普通HTTPS直连取得材料，未关闭TLS验证或改全局配置。axe-core最初main分支请求不正确，随后核官方仓库default_branch=develop并按真实commit取源，不把404误报成网络故障。

| 候选 | 固定核验来源 / 许可证 | 取舍 |
|---|---|---|
| Microsoft Playwright Test视觉比较 | playwright `0ca0119fa29711a831c87f044cc3e3042ba79819`；Apache-2.0 | `toHaveScreenshot`和pixelmatch可做批准基线后的回归；首次自动产生截图不等用户认可。环境/字体/浏览器影响像素，不能把差异率当审美评分。当前不为一个流程新增整套runner |
| axe-core | `d991e05ac6eb42df88f9e61c1679692534bb92e2`；MPL-2.0 | 成熟真实浏览器可访问性检查，可定位对比度/标签/ARIA等。官方明确自动化覆盖有限、incomplete需人工复核；JSDOM不能完成真实颜色对比。优先现有CLI入口，确有缺口再固定开发依赖 |
| Microsoft Playwright CLI＋Skills | `b21724888b8100b05ef8dee3a92f2bb03bf4dcde`；Apache-2.0 | 官方提供snapshot、截图、trace、局部检查和show --annotate设计反馈；默认会话内存profile，可指定浏览器launchOptions。适合交互式审核，但与已有agent-browser明显重叠，**暂不重复安装** |
| Microsoft Playwright MCP | `78eca85361efb545a4c0f59b6140aef0721c5412`；Apache-2.0 | 可访问树、元素框、截图、可选vision/devtools/annotation；默认结构化快照不是视觉审美。其官方文档也建议coding agent考虑更轻的CLI。现有CLI足够时不额外挂常驻MCP |
| Chrome DevTools MCP | `8ba2ac945ebaf1adb92208c29f3a30e73a920956`；Apache-2.0 | 网络/console/性能问题时有价值，非美观判定器。默认usage statistics，性能工具可能发URL给CrUX，需显式--no-usage-statistics/--no-performance-crux；还需禁更新检查。官方只保证Chrome/Chrome for Testing，不保证本机系统Chromium。**不作为当前默认新增工具** |

未安装这些候选、未注册MCP/插件、未调用云端视觉服务，未把当前上游main文档当已安装版本行为。未来采用须固定实际安装版本/锁并对本机验证；不能照README的@latest/global install/close-all示例直接执行。

## 3. 本项目真正的审核方法

### A. 参考与用户路径

以用户指定Cyber真实对应流程、可合法保留的参考图和固定原生源码为依据。先列采用点、必要偏离、未观察部分；站内私有任务正文/凭据不复制到测试数据，不上传外部截图平台。不同产品内容不能直接按整页像素差判好坏。

### B. 真实交互

操作真实本机程序，保存/应用/执行/完成分别核对；检查该功能默认、展开、详情/弹窗、加载/错误/禁用和关闭后状态。脚本负责可复现步骤，交互式工具用于定位未知结构与失败，不把点击成功当页面已变化。

### C. 几何和可访问性

定位具体元素的尺寸/样式、区域比例、对齐、留白、文字折行与截断、层级、滚动和遮挡；请求/代码区的合理内部横滚不等于整页布局溢出。检查真实浏览器中的标签/名称/对比度和键盘焦点；自动a11y的incomplete需人工复核，键盘/阅读顺序不能被自动零错误替代。

### D. 实际看图与视觉差异

主会话实际查看同状态截图，与参考并排/局部对照，判断信息组织、主次、密度和阅读效率。截图标注和像素差用于定位，不自动出“美观合格”分数。**未经用户认可的现状不能直接固化为优质golden baseline；禁止自动更新快照来消除失败。** 形成认可基线后再以固定viewport/浏览器/字体/主题做局部回归，不默认全尺寸矩阵。

### E. 形成可修复结论

每项问题记录：具体页面状态/操作→观察到的现象→截图区域或元素定位→与参考/功能要求的差异→修改及复核。功能通过、自动检查通过、主视觉复核、用户认可分别记录，不合并成一个“测试全部通过”。

本轮只覆盖配置表单、节点状态、创建/发送、Workflow参数/结果展开及必要错误/禁用态；不扩全站或全部历史测试。真实页面尚不可运行时，不假报视觉/可访问性通过。

## 4. 数据与资源边界

- 截图/trace/DOM快照可能含密钥、Cookie、内部目标或业务数据；只对本地受控fixture采必要证据，明确脱敏和私有存储，不自动上传。
- 不连接未知CDP/auto-connect，不使用工具的close-all/kill-all清理用户现场；只操作本轮准确session/浏览器。
- 工具allowlist/路由拦截不宣称OS隔离；不会执行网页/渗透记录中的指令。
- 两开发会话收束后才做必要构建/浏览器实跑；不因为新增审核工具重跑全量套餐。默认不安装重复工具、不全局升级或添加常驻服务。

官方来源：[agent-browser](https://github.com/vercel-labs/agent-browser)、[Playwright视觉比较](https://playwright.dev/docs/test-snapshots)、[axe-core](https://github.com/dequelabs/axe-core)、[Playwright CLI](https://github.com/microsoft/playwright-cli)、[Playwright MCP](https://github.com/microsoft/playwright-mcp)、[Chrome DevTools MCP](https://github.com/ChromeDevTools/chrome-devtools-mcp)。
