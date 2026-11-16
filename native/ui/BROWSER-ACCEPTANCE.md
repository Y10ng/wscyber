# H3-B：原生 UI 登录与空态导航的本机浏览器验收

2026-11-13，分支`feat/foundation`。**本片限定交互链通过，不是完整UI、视觉认可或UI→节点执行验收。** 直接复用H3-A2固定Hub和11文件真实UI产物，没有修改产品页面/主题/布局或后端代码，也没有重新npm/Go构建。

## 实际入口与方法

- 同一原生`wscyber-hub --local-hub --ui-dir <真实release/public>`，未加`--fixture-node`；随机loopback端口、私有测试key与新SQLite，净环境，不读生产模型或旧业务库。
- 一次一个可见Chromium，干净临时context，1440×900、中文；通过原生表单和HttpOnly Cookie认证，不手工注入Cookie，不使用API mock、假会话或假节点。
- `/`是空会话工作台；节点列表通过顶部Agents抽屉打开，没有另造`/agents`路由。没有节点/会话，所以详情、终端、工具和任务执行不在本片。
- 页面请求限制为精确本机origin/静态资源/只读Connect方法；只额外允许本轮原生登录POST。全部WS、agent-token读取、配置写入与模型探测禁止。本线程loopback转发器仅连接该Hub，转发真实响应，并非产品路由或网络沙箱。

## 最终实际通过

| 操作 | 结果 |
|---|---|
| 缺密钥提交 | 显示“请输入访问 Token”，零登录请求；这是前端校验，不冒称后端401 |
| 错误密钥 | 真实登录401，界面显示无效Token提示 |
| 正确密钥 | 真实登录200，经自然Cookie完成后续manifest/会话/节点/状态读取 |
| 空会话列表 | 显示“暂无任务”，点击“全部任务”；数据来自真实空Hub |
| 侧栏收起/展开 | 约288px→48px→288px；全部阶段body宽1440，无整页横向溢出 |
| 节点抽屉 | 打开“Agent 控制台”，显示“无 Agent 连接”；实际抽屉1080×852，再正常关闭返回会话页 |
| 页面与网络 | 19条真实页面HTTP响应，零pageerror、零未知页面请求/WS、零requestfailed；真实JSON/protobuf内容断言通过 |
| 退出与资源 | driver/Hub均exit0，context/browser关闭；主独立核对进程消失、Hub/guard端口可重绑、私有runtime/key/SQLite/HOME/profile/短TMPDIR已删、heavy.lock空闲 |

主已实际查看全部7张最终阶段截图，不以DOM或HTTP200代替看图。测试窗口随本轮结束关闭，没有留下新常驻预览；原用户入口和在用旧预览未操作。

## 必须保留的限制与问题

- **空Hub未配置ConfigStore**：`ConfigService/GetConfig`返回400/`failed_precondition`，页面真实显示`config store is not configured`红色提示，顶部相关状态仍显示读取中。是未接入能力/体验缺口，不算正常产品验收。
- `/ioa/nodes`、`/ioa/spaces`、`/ioa/messages`真实404。最终console有6条error：错误登录401一条、配置400两条、IOA404三条，均逐项记录。**不能说console零错误、配置或IOA可用。**
- 页面没有未知网络请求，但浏览器后台有8次CONNECT尝试被本轮guard拒绝；不能称全浏览器零网络尝试，也不能把这种控制当OS强隔离/完整抓包证明。
- 首页CVE标签属于原生欢迎页装饰示例，不是本次发现或检测结果；本片没有真实安全任务或研究质量结论。
- 仅1440×900浅色空态；未验窄屏/深色/实际最大化、非空会话详情、真实节点与终端、UI→节点执行、持久恢复、授权出口、生产模型、原业务迁移、成果报告或SDL。许可UNKNOWN仍未解决，未部署/分发/commit/push。

## 真实失败与脚本修正

同片首次准备会话已正常退出后，主串行执行并保留5份记录，**不是首次即通过**：

1. `browser-evidence`：Chromium在setup退出；原错误被1800字符截断。Hub正常关闭，尚未进入页面。
2. `browser-diagnostic`：补完整有界错误后确认SIGTRAP；同净环境X11连接正常。长TMPDIR使惯用Unix socket示例路径达到137字节；缩短后浏览器能启动，但未作单变量A/B来断言唯一崩溃根因。
3. `browser-short-tmp`：浏览器启动，临时转发器直接`with HTTPConnection`的用法错误导致空响应；纯检查确认该类无上下文管理器接口，改`contextlib.closing`。
4. `browser-transport-fixed`：实际登录和空工作台已出现，但驱动在correct-key阶段到达100秒上限，不能算收展/抽屉通过。去掉冗余无界`response.finished`等待，保留状态/DOM/JSON/protobuf断言，内容读取另加8秒界限与定位字段。
5. `browser-bounded-reads`：完整本片链通过。上述改动仅验收脚本，不改产品或放宽网络/功能断言。所有失败现场及最终记录保留，各轮临时运行资源均已清理。

## 证据与接续

- 包：`var/cyber-h3b.7s7l7O0M/`；脚本`run-browser.py`、`browser.mjs`、精确`policy.json`仅本片测试资产，不是产品代理。
- 最终证据：`browser-bounded-reads/{result.json,browser-result.json,01-*.png,...,07-*.png}`。
- 主复核：`main-acceptance.md`、`main-final-check.json`、`main-final-evidence-sha256.json`；执行方原交付hash保留历史，主修订以最终hash为准。
- 独立复核：351项产品基线在状态文档更新前全部未变，11个UI资产hash未变。
- 下一片：仅接显式本机fixture-node，实测页面创建/运行一条受控会话、显示固定工具结果和原生事件；先核对真实配置/能力契约，不用假接口去遮住上述缺口。真实模型、目标、迁移和部署不随本片自动授权。
