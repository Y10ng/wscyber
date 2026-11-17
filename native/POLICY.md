# 测试策略与危险操作审批（B1）

策略按任务保存，只由人经鉴权 API 设置：`PUT /api/webapi/tasks/{id}/policy` `{"scope","approval"}`（枚举校验，写 `webapi_task_policies` 当前值 + `webapi_task_policy_events` 变更记录，保存即生效，影响之后的调用）。`GET /api/webapi/tasks/{id}`、`GET /api/webapi/tasks?session_id=`、任务 PUT 响应带 `policy:{scope,approval,is_default,updated_at}`。无记录（旧任务）＝默认 `origins + high_risk`，`is_default=true`；会话按现有映射（首会话/验证/复测）找任务，映射不到按默认。模型、目标响应、资料、工具参数都没有改策略的途径。

- `scope`：`origins`（默认，`webapi_http` 只发授权 origins，越界记 `denied`）| `unrestricted`（任意 http/https，仍完整记证据）。只约束 `webapi_http`；终端等原生工具**没有**强制出口。
- `approval`：`off` | `high_risk`（默认）| `all`。
  - high_risk：`webapi_http` 命中 `policy.go requestReview`（DELETE、路径/查询/方法覆盖头/正文破坏性信号）；`bash` 命令命中 `webapi/command_rules.go` 规则表：rm -r/-f、mkfs/wipefs/shred/mkswap、dd of=/dev/…、重定向写块设备、shutdown/reboot/halt/poweroff/init 0|6/systemctl reboot…、SQL DROP/TRUNCATE/DELETE FROM、curl/wget DELETE 或破坏性信号、find -delete；穿透 sudo/env/xargs/timeout 等包装、find -exec、sh/bash -c、eval、tmux send-keys。命令名为动态展开、或高危类程序参数含动态展开（无法静态核对）也要求审批。
  - all：以上 + `webapi_http` 全部非 GET/HEAD/OPTIONS + 全部 `bash` + `write`。
  - 不审批：`read`/`ls`/`glob`、平台结果工具（`webapi_finding`、`webapi_verification_result`、`webapi_report_draft`、`webapi_retest_result`，只写平台库）；`subagent`、tmux 在模型节点本就拒绝。

## 审批机制

`Capability.ApprovalExtension()` 订阅原生 `tool.before`：需审批时以 `Emitter:"guardrail"` 发 `guardrail.Review{PENDING, ExpiresAt, Decision{REVIEW, Reason}}`（带 operation 引用，入 `chat_aop_events`，上游 `GuardrailReviewCard` 可渲染）并阻塞；批准放行，拒绝/超时（`ReviewTimeout`=10 分钟）/取消→拒绝并发终态事件（`ResolutionSource="control"`）。`webapi_http` 被拒/超时/取消按 `recordUnsent` 记 `review_denied`/`review_expired`/`review_canceled`（`sent=false`，原因写明），不阻断后续模型调用；工具内只对“需审批却未经批准”的调用记 `review_required`（如未装审批扩展），已批准的不二次拒绝。

两个模型节点 Profile 的协议集绑定 `Capability.ApprovalBinding()`（guardrail 命名空间 Pending/Resolve，按 session+operation 精确匹配）；Hub 经上游 `pkg/web/service/guardrail.go` 转发。上游 guardrail 扩展保持 provider=none，不绑定其 `NewProtocol`。

终端超时：模型节点 bash 5s 超时只计进程运行；审批在 `tool.before`（进程启动前）等待，不计入（`TestModelNodeApprovalThroughNativeBash` 等 6s 后批准仍执行）。`command.before` 兜底只对注册命令生效（本固定上游版本外部程序不经过它），未批准的高危注册命令直接拒绝、不等待。

审批状态在节点内存：节点重启时挂起审批随调用取消。演示：`scripts/demo-stack.py approval-demo`（任务目标含“审批演示”）。
