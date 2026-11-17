# 用量 · 原生实现

2026-11-17 后端片（只读，不新增表/列）。仅临时库合成事件测试通过；前端、真实 Hub 数据未验。

`GET /api/webapi/usage?task_id=&from=YYYY-MM-DD&to=YYYY-MM-DD`（原生鉴权内）

- 数据源：`chat_aop_events.event_proto` 顶层 `Event_Usage`（上游 agent 每轮一条，轮内重试已合并）。`TurnEnded.Usage` 为轮累计，不计。损坏行计入 `skipped_events`，不进汇总。
- 会话归属：`webapi_tasks.initial_session_id`=research、`webapi_verifications`=verification、`webapi_reports`=report、`webapi_retests`=retest。
- `task_id` 有值：只统计该任务会话，返回 `by_session[{session_id,role,finding_id,report_id,…U}]`；任务不存在 404。空/缺省：统计全部会话，返回 `by_task[{task_id,title,…U}]`，不属于任务的会话归 `task_id=""`；`title` 为 objective 首个非空行（≤120 字）。
- 日期按 Hub 进程本地时区（`timezone` 字段；`/etc/localtime` 场景从链接取 IANA 名），`from/to` 可选闭区间；格式非法或 from>to 400。
- U = `input_tokens, output_tokens, total_tokens, cache_read_tokens, cache_write_tokens, calls`；total 缺失（0）时用 input+output；cache 取 `detail["cache_read"/"cache_write"]`；calls = Usage 事件数。
- 排序：`by_day` 升序；`by_model/by_task/by_session` 按 total 降序，同值按键升序。数组无数据时为 `[]`。
- 不做费用（上游无价格配置）。
- 未计入：上下文压缩摘要调用、recap 扩展、JEV（声明/编译/反射）模型调用均不发 `Event_Usage`；子代理（subagent）在独立 session 中运行，不属于任何任务会话，任务视图不含、全局视图归入 `""`。
