# C2-A：身份说明与业务前提

2026-11-12；已实现并完成受控本机测试、正式运行和浏览器展示，不等于用户认可或完整C完成。依据 `var/c2-identity-prep.gz4jssgu/entry-decision.md`；原实施归属及基线：`var/research-brief-main.3apc0k4k/`；接续独立验收：`var/c2a-resume.BtptOqcM/outcome.md`，最新入口与未覆盖见STATUS§0.26。实际参考与必要差异保留在准备包 `packet.md`：cyber仅观察了空白创建表单结构；ZAP Users官方正文用于区分说明、认证、会话和凭据，未观察身份工作流、不引入认证执行。

## 范围与接口

仅创建时固定的**非秘密用户声明**，不是已认证身份、已验证规则或授权。实际目标身份/会话配置仍待完成。无密码、Cookie、Token、Header契约、自动登录、凭据注入或运行中PATCH。

`POST /api/explorations` 可追加：

```json
{"research_brief":{"identities":[{"label":"成员A","role":"普通成员","ownership":"本人组织内自建记录"}],"business_rules":"普通成员仅可读取本人记录（用户声明，待核验）。","preconditions":"测试数据由获授权人员创建。","share_with_model":false}}
```

- 整个对象可省略，省略保持旧序列化/操作指纹，不回填默认值。对象出现时必须显式布尔 `share_with_model`；缺省、null、字符串均拒绝。UI默认false，未填写说明内容时不提交对象。
- `identities`可省略，最多8项；`label`、`role`必填且去首尾空白后非空，分别最多128、512 UTF-8字节；可选`ownership`最多1024字节。身份数组顺序属于提交身份，不排序；label仅比较去首尾Unicode White_Space后的精确值，不改变保存文本、不忽略大小写。
- 可选`business_rules`最多8192字节，`preconditions`最多4096字节。所有文本原字节合计最多16384，对象按Go `encoding/json`规范化序列化（包括HTML转义）最多32768字节。不截断、不规范化换行、不修复Unicode。
- 至少存在一条有效身份，或非空白的业务规则/复现前提。空对象及只有分享标记拒绝。空字符串与省略的可选文本按omitempty等价，空身份数组与省略等价。
- 有效Unicode：拒绝无效UTF-8和未配对UTF-16转义。拒绝NUL和所有Unicode Cc控制字符；规则/前提例外允许TAB、LF和CRLF（不允许孤立CR）。身份单行字段还拒绝U+2028/U+2029。空白判定采用Unicode White_Space集合（含U+0085、不含U+FEFF），前后端一致。文本原件不trim。
- 严格递归拒绝未知字段、重复键、null和大小写别名；凭据类非契约字段不例外。错误不回显正文。提示不是通用秘密检测，无法保证用户不会误贴秘密。

## 固定、读取与发送

- 追加009 `mission_research_briefs`，mission_id主键、规范化JSON文本及SHA256；不可变触发器禁止UPDATE/DELETE。与任务、方向、选材、安全清单、模型绑定、启动事件及回执同事务。历史无行表示未配置，不补造分享意图。
- 现有 `GET /api/missions/{id}/research-config` 可返回 `research_brief`，继承Bearer/no-store、任务归属及显式读取。普通项目列表、事件、Snapshot API、创建响应及操作回执不增加正文。
- 每次模型装配从本任务读取并校验固定原件，即使不分享也检查完整性；失败在发送前终止，不降级为空。仅显式true才装配到低信任 `research_context.research_brief`；仅brief、无材料/安全清单也能装配。
- false整组不进入环境/managed双协议的system/user/原文归档，不拼入description/goal，不受Skill选择、原文开关或重新登录影响。true也不成为系统指令、目标授权或工具参数。保留一次Attempt最多一次模型调用、S1真实加载与调用关联和256KiB实际输入限制。
- “提供给研究模型”只表示固定的配置意图，不证明已派发、已阅读、已理解或已验证；不增加虚构使用次数。

## 前端与验收边界

共享受控ResearchBriefEditor、纯文本ResearchBriefSummary及样例/coverage先行；无API、鉴权或提交逻辑。沿用UI-1、TaskModal、GridFold与14px主题。非秘密说明不用密码字段；按需提示字段用途与勿填凭据，不设常驻说明墙。正式创建高级区保留两主输入和授权布局；现有研究配置弹窗回看固定内容。

提交前深复制说明对象、身份数组和分享选择；在途草稿变化不改变原载荷。unknown/401仍只核对或显式同键重试，退出清内存、不存浏览器持久存储。

专项以准备包§6和核定书§验收为准：严格输入/旧指纹、新隔离PG原子性/幂等/不可变/恢复、三模型路径×分享×原文开关、only-brief/跨项目/读取失败/容量、共享与正式UI故障生命周期及实际尺寸主题。只用本项目新tmpfs PG、loopback确定性fixture，不动旧预览/窗口/草稿，不用真实模型或外部目标。测试、构建、实际展示、原入口复核和用户认可分别记录。原入口已在新隔离实例重新构建/运行，完成三模型路径×分享×原文12组合、九项PG race、147项React测试及可见/六组异常浏览器检查；失败驱动、警告和未覆盖仍保留，见STATUS§0.26。不把这些工程验证当真实研究效果或用户验收。
