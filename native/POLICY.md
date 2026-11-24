# 测试策略与危险操作审批（B1）

策略按任务保存，只由人经鉴权 API 设置：`PUT /api/webapi/tasks/{id}/policy` `{"scope","approval"}`（枚举校验，写 `webapi_task_policies` 当前值 + `webapi_task_policy_events` 变更记录，保存即生效，影响之后的调用）。`GET /api/webapi/tasks/{id}`、`GET /api/webapi/tasks?session_id=`、任务 PUT 响应带 `policy:{scope,approval,is_default,updated_at}`。无记录（旧任务）＝默认 `origins + high_risk`，`is_default=true`；会话按现有映射（首会话/验证/复测/子 Agent 会话）找任务，映射不到按默认。模型、目标响应、资料、工具参数都没有改策略的途径。

- `scope`：`origins`（默认，`webapi_http` 只发授权 origins，越界记 `denied`）| `unrestricted`（任意 http/https，仍完整记证据）。同一 scope 也决定终端、浏览器（`playwright`）与爬虫（`katana`）的出口：`origins` 时经代理的请求越界即拒绝，`unrestricted` 时只记录，见下文「全工具出口（B2）」。这是**代理强制**，不是系统级隔离。
- `approval`：`off` | `high_risk`（默认）| `all`。
  - high_risk：`webapi_http` 命中 `policy.go requestReview`（DELETE、路径/查询/方法覆盖头/正文破坏性信号）；`bash` 命令命中 `webapi/command_rules.go` 规则表：rm -r/-f、mkfs/wipefs/shred/mkswap、dd of=/dev/…、重定向写块设备、shutdown/reboot/halt/poweroff/init 0|6/systemctl reboot…、SQL DROP/TRUNCATE/DELETE FROM、curl/wget DELETE 或破坏性信号、find -delete；`playwright` 动作类子命令（open/goto/click/fill/press/evaluate/set-extra-headers 等）参数命中破坏性信号、`katana` 带 -headless/-hybrid/-aff/-al（主动点击、填表提交、自动登录）（详见 `native/BROWSER.md`）；穿透 sudo/env/xargs/timeout 等包装、find -exec、sh/bash -c、eval、tmux send-keys。命令名为动态展开、或高危类程序参数含动态展开（无法静态核对）也要求审批。
  - all：以上 + `webapi_http` 全部非 GET/HEAD/OPTIONS + 全部 `bash`（含 `playwright`、`katana`）+ `write`。
  - 不审批：`read`/`ls`/`glob`、平台结果工具（`webapi_finding`、`webapi_verification_result`、`webapi_report_draft`、`webapi_retest_result`，只写平台库）、`webapi_authz_compare` 工具本身；compare 的每个变体仍逐个执行与 `webapi_http` 相同的 origin/危险请求判定，需审批的变体不发送、只记 `review_required` 并提示改用 `webapi_http` 走人工审批（见 `native/AUTHZ.md`）；`subagent` 只放行任务研究会话派出的命名子 Agent `oracle_explorer`（平台钩子 `wscyber-webapi-subagent`：须 async/sync、prompt 以 open Oracle 编号开头、同任务 ≤3 个；匿名/fork/其它名字/非研究会话一律拒绝，见 `native/SUBAGENT.md`），子会话按所属任务同样受本策略、审批与出口约束；tmux 在模型节点本就拒绝。

## 审批机制

`Capability.ApprovalExtension()` 订阅原生 `tool.before`：需审批时以 `Emitter:"guardrail"` 发 `guardrail.Review{PENDING, ExpiresAt, Decision{REVIEW, Reason}}`（带 operation 引用，入 `chat_aop_events`，上游 `GuardrailReviewCard` 可渲染）并阻塞；批准放行，拒绝/超时（`ReviewTimeout`=10 分钟）/取消→拒绝并发终态事件（`ResolutionSource="control"`）。`webapi_http` 被拒/超时/取消按 `recordUnsent` 记 `review_denied`/`review_expired`/`review_canceled`（`sent=false`，原因写明），不阻断后续模型调用；工具内只对“需审批却未经批准”的调用记 `review_required`（如未装审批扩展），已批准的不二次拒绝。

两个模型节点 Profile 的协议集绑定 `Capability.ApprovalBinding()`（guardrail 命名空间 Pending/Resolve，按 session+operation 精确匹配）；Hub 经上游 `pkg/web/service/guardrail.go` 转发。上游 guardrail 扩展保持 provider=none，不绑定其 `NewProtocol`。

终端超时：模型节点 bash 5s 超时只计进程运行；审批在 `tool.before`（进程启动前）等待，不计入（`TestModelNodeApprovalThroughNativeBash` 等 6s 后批准仍执行）。`command.before` 兜底只对注册命令生效（本固定上游版本外部程序不经过它），未批准的高危注册命令直接拒绝、不等待。

真正挂起/放行调用的仍是节点内存运行时（模型节点随 Hub 进程）：节点或 Hub 重启时挂起的调用随之取消；每次审批另有持久记录（见下文「审批记录」），重启后可查到其去向。子 Agent 会话的挂起审批经其研究会话的 Pending/Resolve 列出与处理（见 `native/SUBAGENT.md`）。演示：`scripts/demo-stack.py approval-demo`（任务目标含“审批演示”）。

## 审批记录（R2）

表 `webapi_approvals`（迁移 `0002_approvals`，PostgreSQL）是审批的权威记录；guardrail `Pending`/`Resolve` 协议仍按会话从内存列出与处理（只有本进程挂起的调用可批准）。

**表结构**：`id`（`ap_…`，主键）、`operation_id`（guardrail operation，同一调用的 PENDING/终态事件用它关联）、`task_id`（无任务为 `""`）、`session_id`（发起调用的会话；子 Agent 为子会话）、`parent_session_id`（子 Agent 审批的父研究会话，否则 `""`）、`turn_id`、`call_id`、`tool`（`webapi_http`/`bash`/`write`）、`summary`、`reason`、`status`、`decided_by`（`""`/`user`/`system`）、`status_reason`、`hub_instance`（写入进程的随机实例 ID）、`created_at`、`expires_at`、`decided_at`（pending 为 NULL；时间均为定宽 UTC 毫秒 `2006-01-02T15:04:05.000Z`），`tenant_id` 随任务继承（无任务回退会话租户，同基线规则）。CHECK：`status ∈ {pending, approved, rejected, expired, canceled}`、`decided_by ∈ {"", user, system}`、`(status = pending) ⇔ decided_at IS NULL`。索引 `(task_id, created_at DESC, id DESC)`、`(session_id)`、`(parent_session_id)`、`(operation_id)`、`(tenant_id, status, created_at DESC, id DESC)`。

- `summary`：`webapi_http` 为 `方法 + URL`（去 userinfo）；`bash` 为命令单行前 200 字；`write` 为路径。`summary` 与 `reason` 先经任务资产测试账号回替（口令明文 → `{{cred:<id>.secret}}`），再经 guardrail `RedactText`（Bearer/Basic、`password=`/`token=` 等赋值、URL userinfo、`-u user:pass`）。请求头/正文不入库。读不到资产密钥时 `summary` 记为“（读取资产测试账号失败，摘要未保存）”，且不登记待审批运营通知。

**状态机**：

| 迁移 | 时机 | decided_by | status_reason | 审计 |
|---|---|---|---|---|
| → `pending` | PENDING 事件发出后落库（同事务插入 approval_pending 通知待登记，见 `NOTIFY.md`）；等 webapi 全局写锁不阻塞 PENDING 事件，失败每秒重试直到决定 | `""` | `""` | — |
| `pending → approved` / `rejected` | 经 Hub 控制通道批准/拒绝，工具边界拿到终态 | `user` | `""` | `approval.approve` / `approval.deny`（同事务） |
| `pending → expired` | 10 分钟未处理 | `system` | 超时未处理，视为拒绝 | `approval.expire`（同事务） |
| `pending → canceled` | 调用被取消（会话/轮次结束、节点关闭） | `system` | 调用已取消（会话或轮次结束、节点关闭） | 不登记（不是决定） |
| `pending → canceled` | Hub 启动时清理其它实例遗留的 pending | `system` | Hub 重启，调用已中断 | `approval.cancel`（system，同事务） |

- 终态写入与审计、删除该审批的待登记、把未发出的 approval_pending 通知改 `dismissed` 在**同一事务**内完成（全局写锁）。行已是终态时不再改写、不重复审计；挂起时落库一直失败的，终态写入时整行插入。
- 终态写库失败（如数据库不可用超过 10s）转入进程内补写，投递 goroutine 每轮重试。
- **重启语义**：Hub 启动（`Routes` 拿到原生管理 API，与遗留子会话清理同处）时，把 `hub_instance` 不是本进程的 pending 行改为 `canceled`（system，“Hub 重启，调用已中断”，`decided_at` 为清理时间），每行一条 `approval.cancel` 审计，删除其待登记、撤销未发出的待审批通知（不再登记、不再重试）。本进程自己的 pending 不受影响；再次执行不重复改写或审计。

**查询（原生鉴权，错误体 `{error}`）**：
- `GET /api/webapi/tasks/{id}/approvals?session_id=&status=&limit=&cursor=`（任务不存在 404）与 `GET /api/webapi/approvals?task_id=&session_id=&status=&limit=&cursor=`。
- `task_id` 精确匹配记录的 `task_id`（不串其它任务）；`session_id` 匹配 `session_id` 或 `parent_session_id`（研究会话可看到经它处理的子 Agent 审批）；`status` 为上述五值之一；`limit` 1..200（默认 50）；按 `created_at`、`id` 倒序，键游标 `base64url(created_at \n id)`。task_id/session_id 非法、status 非法、limit 越界、cursor 非法 400。
- 响应 `{items:[ApprovalView], pending_count, has_more, next_cursor}`；`pending_count` 是同一 `task_id`/`session_id` 范围内 `status=pending` 的总数（不受 `status` 过滤与分页影响），可直接作“待审批 N”计数。
- `ApprovalView = {id, operation_id, task_id, session_id, parent_session_id, review_session_id（Resolve 应使用的会话：子 Agent 为父研究会话，否则同 session_id）, turn_id, call_id, tool, summary, reason, status, decided_by, status_reason, created_at, expires_at, decided_at|null, actionable}`；`actionable` = pending 且仍在本进程挂起（可经 guardrail Resolve 处理）。

**任务删除**：`DELETE /tasks/{id}` 保留该任务的审批记录、把 `task_id` 置空（见 `native/TASK_DELETE.md`）。

**已知限制**：PENDING 事件先于落库发出，落库等待全局写锁期间列表暂时查不到该项（Pending 协议可见）；Hub 在落库成功前崩溃则该次挂起无记录。终态补写成功前进程退出时，该行在下次启动会按“Hub 重启”记为 canceled（实际结果以当时的终态事件与工具记录为准）。

## 全工具出口（B2）

**机制。** 两个模型节点（正式 `model_product.go`、本机验收 `model_node.go`）用 `Capability.EgressExtension()` 替代 `harness.NoEgress()`：随节点 Load 启动一个上游 `tools/proxy.NewProxyHub`（监听 `127.0.0.1:随机端口`，capture+decrypt 开，直连上游），Close 停止；不用 `exts/proxy`，不注册 `proxy/mitm` 命令。扩展发布包装后的 `egress.Endpoint`，上游终端（bash 与 tmux 共用 BashTool）对每个进程调用 `Egress(ctx)`：上游生成 `cyber-route-*` 令牌放进代理 URL，终端把 `ALL_PROXY/HTTP_PROXY/HTTPS_PROXY`（含小写）与 `CURL_CA_BUNDLE/SSL_CERT_FILE/NODE_EXTRA_CA_CERTS/REQUESTS_CA_BUNDLE/GIT_SSL_CAINFO` 注入该进程；本扩展同时登记 令牌→(session, turn, call)，进程结束时上游释放令牌（等在途 flow 写完）、本扩展删除登记。浏览器 `playwright` 与爬虫 `katana`（M5c，见 `native/BROWSER.md`）由本项目包装注册：每次调用以 `tool=browser`/`katana` 登记自己的令牌；浏览器每会话一个无头 Chromium + 本机中继（Chrome 代理不能带口令，由中继补令牌），调用之间的后台请求不发送、记 denied；katana 强制 `-proxy=<本次令牌>`，无头/混合模式用平台启动的 Chromium；拒绝模型传入的改代理/外部浏览器/headed 参数。会话→任务：任务首会话、验证会话、复测会话、报告会话、oracle_explorer 子会话（后四者用所属任务的策略与 origins）。fixture 节点（`fixture_node.go`、`cmd/wscyber-native`）不装 webapi 能力，保持 `NoEgress()`。

**强制（`ProxyHub.SetProxyAuth`，在转发 HTTP / 建 CONNECT 隧道之前执行，拒绝即 407、不连目标）：**
- 无令牌、令牌未登记或已释放 → 拒绝。
- 会话不属于任何 Web/API 任务（普通对话）→ 放行并记录（`task_id` 空）。
- 任务 `scope=origins`：HTTP 取请求 URL 的 origin；CONNECT 只有 host:port，443 视为 https、80 视为 http，其它端口与授权 origin 的 host:port 比较（不分 scheme）；不在授权 origins 内 → 拒绝，原因如“不在任务授权 origins 内：https://x.example.test”（CONNECT 其它端口显示 host:port）。`unrestricted` → 放行。
- 策略每个请求都读库（保存即生效，同一进程的下一次请求即按新策略）；读会话任务或策略失败按拒绝。

**记录。** 表 `webapi_egress_flows(id, task_id, session_id, turn_id, call_id, tool, method, url, host, decision allowed|denied, sent, http_status, error, flow_pb, request_bytes, response_bytes, preview_truncated, created_at, completed_at)`，索引 `(task_id, created_at)`、`(session_id)`。放行请求由上游 `FlowCompletedObserved` 写入（`flow_pb` 为 `proto.Marshal(traffic.Flow)`，与 `webapi_requests.flow_pb` 同序列化；HTTPS 记的是 MITM 解密后的内层请求，CONNECT 本身不单独记）；`sent=true` 仅当收到响应，无响应时 `sent=null`（未知，不推断）。拒绝在鉴权回调写一条：`decision=denied`、`sent=false`、只有请求行与请求头（`Proxy-Authorization` 已去除）。body 只保留每侧前 4096 字节预览（上游未落盘模式），`request_bytes/response_bytes` 为观察到的原始字节数，`preview_truncated` 如实标出，上游截断说明保留在 Flow 的 `error` 里、不计入记录的 `error`。无令牌的拒绝也记（`task_id`、`session_id` 为空，任务 API 不显示）。不发 observe AOP 事件。候选/验证暂不能引用这些流量（后续）。旧库：C3 阶段（二十二表）补表。

**API（原生鉴权，错误体 `{error}`）：**
- `GET /api/webapi/tasks/{id}/egress?session_id=&decision=allowed|denied&limit=1..200(默认50)&cursor=` → `{items:[{id, session_id, turn_id, call_id, tool, method, url, decision, sent, http_status, error, created_at, completed_at}], has_more, next_cursor}`，按 `created_at` 倒序（放行流取请求开始时间）；任务不存在 404。
- `GET /api/webapi/tasks/{id}/egress/{flow_id}` → `{summary, host, flow(protojson：请求/响应头与 body 预览，base64), flow_protobuf_base64, capture:{request_bytes, response_bytes, preview_limit:4096, preview_truncated, note}}`。

**已知可绕过方式（如实说明，非系统级隔离）：** 进程可以 `unset` 代理变量、用 `curl --noproxy '*'`/`NO_PROXY`、直接用 `nc`/原始 socket/其它不读代理变量的程序连目标；DNS 查询不经代理；已建立的 CONNECT 隧道内后续请求不再复核（连接目标仍是被授权的 host:port，但隧道内 Host 头可与之不同）；持有令牌的进程把代理 URL 交给别的进程，在令牌释放前同样生效。没有 netns/iptables/独立用户。浏览器与爬虫已接入（M5c），其额外限制（WebRTC/QUIC/DNS 旁路仅以启动参数关闭、未逐项验证，Chromium 自身少量 Google 后台请求按策略拒绝/记录，同会话并发调用归属可能互串）见 `native/BROWSER.md`「已知限制」。

**CA 与副作用。** MITM CA 在 Hub `--state-dir/egress-ca/`（目录 0700，私钥 `mitmproxy-ca.pem` 0600，导出证书 `mitmproxy-ca-cert.pem`），不在节点工作区内，文件工具读不到；但终端进程与 Hub 同一 OS 用户，刻意读取仍可读到私钥。终端进程的 `SSL_CERT_FILE` 等被替换为只含该 CA 的文件：绕开代理直连 HTTPS 的程序会因不信任真实证书而失败；只认系统证书库、不读这些变量的程序经代理访问 HTTPS 会因不信任 MITM 证书而失败（此类连接只有 CONNECT 被鉴权，内层没有记录）。上游 mitmproxy 每次拒绝会在 Hub 标准错误打一条 logrus error 日志。

