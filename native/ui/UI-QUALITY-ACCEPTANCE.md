# 原生配置与Workflow：有界UI质量片

2026-11-14，分支`feat/foundation`。**本片指定范围已落地并完成真实页面/主看图复核，仍有明确剩余UI项，不是全平台a11y或视觉认可。** 原用户入口wscyber-40保持；没有新Harness、Go修改、业务迁移、部署或commit/push。

## 1. 本片变更与参考

新可见`wangsu-ds-pro-latest`会话`wscyber-ui-quality-ds-dMC8FKIB`实施，主冻结范围/静态复核、负责整合与真实浏览器。DS已正常exit0，专属CLI/shell/terminal原身份消失。

7个源码文件与1份清单变更，覆盖总数13→16；已有覆盖从正式overlay继续，原source_sha256、来源与依赖锁未变：

- ConfigPanel由75vw抽屉改用原生Dialog，宽672px有界居中，标题与操作区固定、正文内滚动；内部路径不常驻，配置层次说明改原生details按需展开，键盘/指针可操作。保留必要单位/错误/本机边界及全部原LLM字段。
- 选中项/配置状态/profile文字使用中性文本，保留状态点/图标；Provider超时占位改“可选，填写秒数”，不编造默认数值。
- 主区main、既有品牌文字h1；文件输入复用既有上传label；Workflow卡片保留article且去不匹配group，角色说明增加正确group。
- Workflow反馈/回答标题与actor改文本token，线、边框、图标与原生控制流结构不重造。
- 主静态核固定Radix后由DS补受控Dialog无Trigger的焦点归还、明确无多余description引用，且仅settings打开时退低header层级，避免遮罩穿透视觉；没有全局调整Dialog/Select。

参考来自上一主实际只读的Cyber任务/Models空Add provider表单安全结构笔记，非私有正文复制；672px宽与居中组织沿用。本站保留更多原生配置字段，实际高704px，未伪称与参考593px高度逐像素一致。Workflow复用已验原生结构，托管站Details完整展开未观察，不编造。中文说明仍有个别重复字段帮助，本片不全站重写。

## 2. 构建与来源

- 一次全新离线fork，复用既有独立复制依赖；一次完整tsc/Vite，exit0。无npm ci/Go build/test/H1–H4/全仓。
- 正式source-overlay清单：`161e6af19b4d4021eb4a4910de271150617ed9612ff3e3ceb0309eb6dfade155`。
- public manifest：`2a3828165a7a68a1fd3faf976d32ad2b9c93647559d12ce3ab8f22a71cc20398`。
- 复用已验Hub：`b810b377132f47f2929bf60561ddafd8902ad44713a2904378603f06e6ba398f`。
- 工作区`var/native-ui-quality.ZzLkZW2L/ui-work`，build.json记录准确来源。Vite6.4.3/4684模块/13.28秒，Browserslist过期和大chunk警告保留，未为消警告升级依赖。
- 官方axe-core4.12.1（MPL-2.0）仅独立开发工具目录`var/native-ui-audit-tools.YqkS6Nf3`，固定lock/引擎hash、忽略安装脚本，不改产品依赖；普通Playwright原生WebSocket常量前后均完整，不关闭agent-browser域限制或改产品迎合其已证包装问题。

## 3. 真实交互与看图

最终运行包`var/native-ui-quality.ZzLkZW2L/flow-78hc5ixy/`；真实Hub/public和Playwright，不是API mock。

1. 原生登录、缺新key时实际中文错误显示且0 UpdateConfig；填写后只保存一次。重新打开password仍空、不回显旧值；未因保存/查看触发probe。
2. 配置帮助Enter打开、Space关闭、指针开关；Tab在modal内、Escape关闭并回焦原工具栏按钮。
3. 2554×1182下dialog 672×704；520×900下dialog x16/right504/width488、scroll/client均486，document/body均520。**对话框自身与整页横向溢出分开核，都在所测宽度通过**；窄屏底部API Key可滚动查看，固定保存/关闭仍可见。未外推全尺寸矩阵。
4. 原生节点当前provider/model且无configError，配置前后两个检查点均0模型TCP/HTTP；之后一次创建/发送、两次本机SSE、一次真实printf及同call结果消费，真正completed。
5. Workflow真实收起/展开、同工具参数/结果、End/Home/ArrowDown导航、summary Space收展通过；13持久/12持久Watch与1瞬时delta核对成立。有界列表只读搜索/清空返回真实200，写请求仍拒绝。
6. 主实际Read最终8张图：缺key、保存后配置、帮助展开、520顶/底、节点报告、Workflow收起/展开；另看2张实际截图局部裁切。居中布局较旧75vw抽屉克制，标题/状态文字与结果内容更清楚，未冒领用户认可。

本机模型fixture/usage不是供应商实际计费或研究效果。最终自动验收0 pageerror/0拦截，仍有9条原生IOA三路径404及guardrail未配置，不称零console错误。

## 4. 可访问性：修复与剩余分开

仅选择9项相关axe规则，并非全站全部规则扫描。

- **保存后配置+帮助展开**：0 violations / 0 incomplete，仅该状态/浅色桌面；不外推缺key错误、其他页签/主题。
- **Workflow展开**：main/h1/region、article角色、文件label、角色说明的已定位问题未再报对应violation；但仍有1类color-contrast、4个具体节点：`workflow-follow`当前步骤4.11，角色legend LLM/JEV/TOOL分别3.09/3.34/3.83。它们不在本片已冻结的反馈/回答标题actor修正中，**明确保留待下一涉及该组件的前端片修正，不称Workflow全a11y通过**。
- 已改的反馈卡标题/actor实际色与截图背景计算为16.057/5.174；活动回答卡因均匀叠色背景被axe列incomplete，主用实际截图像素加浏览器计算文字色复核为15.180/4.892，均≥4.5。证据manual-card-contrast.json；不是对所有渐变/主题的结论。
- 仍3类incomplete：两个TokenUsage span的ARIA命名、关闭的输入指南aria-controls关联、11处颜色/渐变或短文本候选。角色/动态控件需后续人工/辅助技术核验，不将incomplete自动当已确认缺陷或已通过。已人工复核上述本片改动的活动回答文字，其余不冒称全部处理。
- 缺key真实错误反馈已看图/验证阻止保存，但未做该错误态专门axe；模型key留空保留/LLM高级字段的转换逻辑未改，本片未重复覆盖全部多profile/异常持久化矩阵。

## 5. 两次验收测量失败保留

`flow-ws1vkmpz`与`flow-97_zotw4`都在窄屏尺寸检查停止，均0模型TCP/HTTP、0任务；各自配置只保存一次、Hub exit0/无剩余子进程，失败private原件保留。第一版混合断言缺分项诊断；第二次分项证实document/body已520，但测dialog仍672/x-76，随后截图却488/x16。

无产品API的单浏览器CSS探针精确复现：原生Dialog `duration-200`对width有200ms过渡，两个动画帧不等过渡结束；截图animations:disabled又会自动结束有限过渡。最终runner等待真实animation.finished后仍分别保留两项溢出断言，产品代码/构建未改。不因工具时序失败乱修产品、吞console或重开旧库；详细证据validation-corrections.md/resize-probe.json。

## 6. 当前预览与接续

**2026-11-14最新覆盖：34039已被系统低内存保护停止，不在线。** 执行主转交系统通知，原入口核任务输出READY后`[killed]`、四原身份消失、34039/41093无LISTEN；无最终result.json，不能声称正常排空、SQLite检查或成功清理。先前本片定向验收仍成立，失败私有目录保留。不自行重启，站点只读参考不等于本地预览重启许可。CLI898729已在确认无在途任务/不再承载预览后正常退出0，原入口wscyber-40保留。证据见运行包main-protected-stop-check.json及上级main-retirement.json。当前先依用户要求深入Cyber任务详情并整理规范，再落实一次约10%尺度与密度调整；Web/API正式实施暂停。以下为停止前历史记录，不作为当前存活或操作依据。

新已登录预览`http://127.0.0.1:34039`，窗口号171966468被系统复用，**新Chromium1061589/start12339085与旧723048不是同一身份**。后台bmknm3gas；监督器1061535/start12338996、Hub1061537/start12339014、driver1061545/start12339021。主只读核身份/标题，未再次导航或截图用户可能编辑的当前页面。准确资源/私有目录/父链见本运行包LIVE.md、ownership.json、main-live-check.json。

仅已完成fixture的配置/节点查看、Workflow回放与只读筛选；不支持展示期额外保存/任务/发送/probe，不是生产研究。无展示总时限，用户关窗后才按最终result收尾；running不是清理证明。**本主仍在预览祖先链上，交回开发写权不等可退出本主；原入口wscyber-40也保持。**

旧33901已结束，失败原件未动。无真实模型/目标、旧业务迁移、部署、commit/push/Arena。真实供应商、完整授权出口/OS隔离/恢复unknown、许可分发、业务成果/报告/复测仍未交付。下一短轮回到Web/API任务/真实流量原件→候选/验证→报告复测需求，以原生能力/扩展做最小闭环，不再无限重复底座演示；本片剩余UI项精准登记随对应组件接续。
