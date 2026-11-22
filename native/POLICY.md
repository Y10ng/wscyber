# 测试策略与危险操作审批（B1）

策略按任务保存，只由人经鉴权 API 设置：`PUT /api/webapi/tasks/{id}/policy` `{"scope","approval"}`（枚举校验，写 `webapi_task_policies` 当前值 + `webapi_task_policy_events` 变更记录，保存即生效，影响之后的调用）。`GET /api/webapi/tasks/{id}`、`GET /api/webapi/tasks?session_id=`、任务 PUT 响应带 `policy:{scope,approval,is_default,updated_at}`。无记录（旧任务）＝默认 `origins + high_risk`，`is_default=true`；会话按现有映射（首会话/验证/复测/子 Agent 会话）找任务，映射不到按默认。模型、目标响应、资料、工具参数都没有改策略的途径。

- `scope`：`origins`（默认，`webapi_http` 只发授权 origins，越界记 `denied`）| `unrestricted`（任意 http/https，仍完整记证据）。同一 scope 也决定终端出口：`origins` 时终端经代理的请求越界即拒绝，`unrestricted` 时只记录，见下文「全工具出口（B2）」。这是**代理强制**，不是系统级隔离。
- `approval`：`off` | `high_risk`（默认）| `all`。
  - high_risk：`webapi_http` 命中 `policy.go requestReview`（DELETE、路径/查询/方法覆盖头/正文破坏性信号）；`bash` 命令命中 `webapi/command_rules.go` 规则表：rm -r/-f、mkfs/wipefs/shred/mkswap、dd of=/dev/…、重定向写块设备、shutdown/reboot/halt/poweroff/init 0|6/systemctl reboot…、SQL DROP/TRUNCATE/DELETE FROM、curl/wget DELETE 或破坏性信号、find -delete；穿透 sudo/env/xargs/timeout 等包装、find -exec、sh/bash -c、eval、tmux send-keys。命令名为动态展开、或高危类程序参数含动态展开（无法静态核对）也要求审批。
  - all：以上 + `webapi_http` 全部非 GET/HEAD/OPTIONS + 全部 `bash` + `write`。
  - 不审批：`read`/`ls`/`glob`、平台结果工具（`webapi_finding`、`webapi_verification_result`、`webapi_report_draft`、`webapi_retest_result`，只写平台库）、`webapi_authz_compare` 工具本身；compare 的每个变体仍逐个执行与 `webapi_http` 相同的 origin/危险请求判定，需审批的变体不发送、只记 `review_required` 并提示改用 `webapi_http` 走人工审批（见 `native/AUTHZ.md`）；`subagent` 只放行任务研究会话派出的命名子 Agent `oracle_explorer`（平台钩子 `wscyber-webapi-subagent`：须 async/sync、prompt 以 open Oracle 编号开头、同任务 ≤3 个；匿名/fork/其它名字/非研究会话一律拒绝，见 `native/SUBAGENT.md`），子会话按所属任务同样受本策略、审批与出口约束；tmux 在模型节点本就拒绝。

## 审批机制

`Capability.ApprovalExtension()` 订阅原生 `tool.before`：需审批时以 `Emitter:"guardrail"` 发 `guardrail.Review{PENDING, ExpiresAt, Decision{REVIEW, Reason}}`（带 operation 引用，入 `chat_aop_events`，上游 `GuardrailReviewCard` 可渲染）并阻塞；批准放行，拒绝/超时（`ReviewTimeout`=10 分钟）/取消→拒绝并发终态事件（`ResolutionSource="control"`）。`webapi_http` 被拒/超时/取消按 `recordUnsent` 记 `review_denied`/`review_expired`/`review_canceled`（`sent=false`，原因写明），不阻断后续模型调用；工具内只对“需审批却未经批准”的调用记 `review_required`（如未装审批扩展），已批准的不二次拒绝。

两个模型节点 Profile 的协议集绑定 `Capability.ApprovalBinding()`（guardrail 命名空间 Pending/Resolve，按 session+operation 精确匹配）；Hub 经上游 `pkg/web/service/guardrail.go` 转发。上游 guardrail 扩展保持 provider=none，不绑定其 `NewProtocol`。

终端超时：模型节点 bash 5s 超时只计进程运行；审批在 `tool.before`（进程启动前）等待，不计入（`TestModelNodeApprovalThroughNativeBash` 等 6s 后批准仍执行）。`command.before` 兜底只对注册命令生效（本固定上游版本外部程序不经过它），未批准的高危注册命令直接拒绝、不等待。

审批状态在节点内存：节点重启时挂起审批随调用取消。子 Agent 会话的挂起审批经其研究会话的 Pending/Resolve 列出与处理（见 `native/SUBAGENT.md`）。演示：`scripts/demo-stack.py approval-demo`（任务目标含“审批演示”）。

## 全工具出口（B2）

**机制。** 两个模型节点（正式 `model_product.go`、本机验收 `model_node.go`）用 `Capability.EgressExtension()` 替代 `harness.NoEgress()`：随节点 Load 启动一个上游 `tools/proxy.NewProxyHub`（监听 `127.0.0.1:随机端口`，capture+decrypt 开，直连上游），Close 停止；不用 `exts/proxy`，不注册 `proxy/mitm` 命令。扩展发布包装后的 `egress.Endpoint`，上游终端（bash 与 tmux 共用 BashTool）对每个进程调用 `Egress(ctx)`：上游生成 `cyber-route-*` 令牌放进代理 URL，终端把 `ALL_PROXY/HTTP_PROXY/HTTPS_PROXY`（含小写）与 `CURL_CA_BUNDLE/SSL_CERT_FILE/NODE_EXTRA_CA_CERTS/REQUESTS_CA_BUNDLE/GIT_SSL_CAINFO` 注入该进程；本扩展同时登记 令牌→(session, turn, call)，进程结束时上游释放令牌（等在途 flow 写完）、本扩展删除登记。会话→任务：任务首会话、验证会话、复测会话、报告会话、oracle_explorer 子会话（后四者用所属任务的策略与 origins）。fixture 节点（`fixture_node.go`、`cmd/wscyber-native`）不装 webapi 能力，保持 `NoEgress()`。

**强制（`ProxyHub.SetProxyAuth`，在转发 HTTP / 建 CONNECT 隧道之前执行，拒绝即 407、不连目标）：**
- 无令牌、令牌未登记或已释放 → 拒绝。
- 会话不属于任何 Web/API 任务（普通对话）→ 放行并记录（`task_id` 空）。
- 任务 `scope=origins`：HTTP 取请求 URL 的 origin；CONNECT 只有 host:port，443 视为 https、80 视为 http，其它端口与授权 origin 的 host:port 比较（不分 scheme）；不在授权 origins 内 → 拒绝，原因如“不在任务授权 origins 内：https://x.example.test”（CONNECT 其它端口显示 host:port）。`unrestricted` → 放行。
- 策略每个请求都读库（保存即生效，同一进程的下一次请求即按新策略）；读会话任务或策略失败按拒绝。

**记录。** 表 `webapi_egress_flows(id, task_id, session_id, turn_id, call_id, tool, method, url, host, decision allowed|denied, sent, http_status, error, flow_pb, request_bytes, response_bytes, preview_truncated, created_at, completed_at)`，索引 `(task_id, created_at)`、`(session_id)`。放行请求由上游 `FlowCompletedObserved` 写入（`flow_pb` 为 `proto.Marshal(traffic.Flow)`，与 `webapi_requests.flow_pb` 同序列化；HTTPS 记的是 MITM 解密后的内层请求，CONNECT 本身不单独记）；`sent=true` 仅当收到响应，无响应时 `sent=null`（未知，不推断）。拒绝在鉴权回调写一条：`decision=denied`、`sent=false`、只有请求行与请求头（`Proxy-Authorization` 已去除）。body 只保留每侧前 4096 字节预览（上游未落盘模式），`request_bytes/response_bytes` 为观察到的原始字节数，`preview_truncated` 如实标出，上游截断说明保留在 Flow 的 `error` 里、不计入记录的 `error`。无令牌的拒绝也记（`task_id`、`session_id` 为空，任务 API 不显示）。不发 observe AOP 事件。候选/验证暂不能引用这些流量（后续）。旧库：C3 阶段（二十二表）补表。

**API（原生鉴权，错误体 `{error}`）：**
- `GET /api/webapi/tasks/{id}/egress?session_id=&decision=allowed|denied&limit=1..200(默认50)&cursor=` → `{items:[{id, session_id, turn_id, call_id, tool, method, url, decision, sent, http_status, error, created_at, completed_at}], has_more, next_cursor}`，按 `created_at` 倒序（放行流取请求开始时间）；任务不存在 404。
- `GET /api/webapi/tasks/{id}/egress/{flow_id}` → `{summary, host, flow(protojson：请求/响应头与 body 预览，base64), flow_protobuf_base64, capture:{request_bytes, response_bytes, preview_limit:4096, preview_truncated, note}}`。

**已知可绕过方式（如实说明，非系统级隔离）：** 进程可以 `unset` 代理变量、用 `curl --noproxy '*'`/`NO_PROXY`、直接用 `nc`/原始 socket/其它不读代理变量的程序连目标；DNS 查询不经代理；已建立的 CONNECT 隧道内后续请求不再复核（连接目标仍是被授权的 host:port，但隧道内 Host 头可与之不同）；持有令牌的进程把代理 URL 交给别的进程，在令牌释放前同样生效。没有 netns/iptables/独立用户。浏览器工具未接入（模型节点未装）。

**CA 与副作用。** MITM CA 在 Hub `--state-dir/egress-ca/`（目录 0700，私钥 `mitmproxy-ca.pem` 0600，导出证书 `mitmproxy-ca-cert.pem`），不在节点工作区内，文件工具读不到；但终端进程与 Hub 同一 OS 用户，刻意读取仍可读到私钥。终端进程的 `SSL_CERT_FILE` 等被替换为只含该 CA 的文件：绕开代理直连 HTTPS 的程序会因不信任真实证书而失败；只认系统证书库、不读这些变量的程序经代理访问 HTTPS 会因不信任 MITM 证书而失败（此类连接只有 CONNECT 被鉴权，内层没有记录）。上游 mitmproxy 每次拒绝会在 Hub 标准错误打一条 logrus error 日志。

