# 多账号越权对比（M5b 后端）

资产可登记多个测试账号。研究、验证、复测与 `oracle_explorer` 子会话用平台占位符引用本任务关联资产的账号；`webapi_authz_compare` 把一条已存证请求换成其它身份或匿名，逐个确定性重发、存证并给出对比提示。只实现后端、契约与临时库合成数据测试；前端在下一片。

> **hint 只是对比线索，不是越权漏洞结论。** 尤其 `same_as_original` 仍须确认返回的是他人数据而不是公共数据。工具不修改候选、验证、漏洞状态或处置，也不触发推送。

## 1. 账号占位符与首条消息

`webapi_http` 的 `url`、`headers` 值、`body`，以及 `webapi_authz_compare` 的 `set_headers` 值支持：

```text
{{cred:<credential_id>.username}}
{{cred:<credential_id>.secret}}
```

- 只解析本任务经 `webapi_asset_tasks` 关联资产下的凭据。credential id 可含点号，按最后一个 `.username` / `.secret` 识别；其它占位符形态不匹配、保持原文，不猜测语义。
- 发送前在平台内替换。无关联资产、credential id 不属于该资产、secret 未设置、密钥/密文不可用，都返回工具错误且 **0 发送**。compare 会先校验全部变体并解析全部 `set_headers`，任一失败则整个调用 0 发送。
- 典型错误：`占位符 {{cred:alice.secret}} 无法解析：任务未关联资产；0发送`、`占位符 {{cred:other.secret}} 的凭据 ID 不属于本任务关联资产；0发送`、`占位符 {{cred:empty.secret}} 的凭据未设置 secret；0发送`。
- 从资产发起研究任务时，首条消息“测试账号”块改为：`- 名称：…；用户名：…；凭据 ID：<id>；口令：用占位符 {{cred:<id>.secret}}（平台发送时替换，你看不到明文）`。无 secret 的账号不写口令项；名称/用户名为空时省略对应字段。
- 仍会在组装首条消息时解密每个已存 secret 一次，以保留密钥缺失或密文损坏时 `stage=first_message` 明确失败的现有行为，但明文不再写入消息。计划验证/复测首条消息也追加同一账号块。**手动验证/复测首条消息由前端组装，本片未改，留待前端片。**
- 已有任务的历史会话事件不追溯清理。

## 2. 存证与遮盖（含模型可见文本）

- `webapi_requests.flow_pb`、URL、请求头/正文、响应头/正文保存**实际发送与接收的字节**，与此前模型自行填写口令时的存证口径一致。
- 回给模型的工具结果（含响应预览与错误文本）中，本任务资产的每个非空 secret 都替换回其 `{{cred:<id>.secret}}`；目标响应回显的 secret 同样处理。用户名可以明文。已有占位符不参与二次替换，避免短 secret 与 credential id 相同时破坏占位符。
- 原有导出、推送、漏洞卡 curl/报告遮盖逻辑继续读取该资产的**全部账号** secret 并遮盖（既有规则为 ≥4 字，`******`）；这不改变 `webapi_requests` 原件。
- `GET /api/webapi/tasks/{id}/evidence/{evidence_id}?mask=1`：读取任务关联资产的**全部非空 secret（含短口令）**，将 `flow` 的请求 URL、请求/响应头值、正文、响应原因与错误文本，以及 `metadata.url`、`metadata.reason`、`preview` 中匹配的原文字节替换为 `{{cred:<credential_id>.secret}}`。同一位置长 secret 优先；相同 secret 按占位符字典序选择一个；已有及新生成的占位符不再替换。只改本次返回副本，不回写存证。
- 遮盖详情**省略 `flow_protobuf_base64`**，避免通过原件取得明文；增加顶层整数 `masked_secrets`，计数为返回字段中的替换总次数（同一口令在 URL、metadata、正文、preview 中重复出现时分别计数），无关联资产或无命中为 0。不带 `mask` 或 `mask=0` 保留原响应字段与内容，不增加计数。
- `GET /api/webapi/tasks/{id}/evidence?mask=1`：列表不返回正文/preview，仅遮盖每项 `url`、`reason`；顶层 `masked_secrets` 为当前页替换次数，分页及游标不变。缺省和 `mask=0` 不变。
- 正文无论文本、二进制或非 UTF-8，均在 protobuf 解码后按字节替换，再按既有 protojson 规则返回（`body` 仍为 base64）。`preview` 的有效 `base64:` 编码先解码、替换再编码；其余预览按原文字节替换。不猜测 URL 编码、JSON 转义或其它变换后的口令，也不恢复已截断的片段；原有 capture/preview 截断标记保持不变。
- `GET /api/webapi/tasks/{id}/evidence/{evidence_id}/image.png` 缺省或 `mask=1`：渲染前对同一 Flow 内容与失败原因替换全部非空资产 secret 为占位符（包含 URL、正文），然后沿用敏感头遮盖、正文二进制省略等既有渲染规则；命中时页脚增加“已遮盖资产口令”。`mask=0` 仍渲染原始内容；出口记录图片接口不在此改动范围内。
- 上述遮盖接口若资产口令无法读取/解密则返回 500，不降级回传未遮盖证据；不遮盖的详情/列表及 `mask=0` 图片不依赖口令解密。
- `webapi_http` 的审批预解析与工具执行使用同一套占位符替换，因此 origin 与危险请求判定针对实际请求。

## 3. `webapi_authz_compare`

参数：

```json
{
  "evidence_id": "we_...",
  "variants": [
    {"label": "用户 B", "set_headers": {"Authorization": "Bearer ..."}},
    {"label": "匿名", "anonymous": true},
    {"label": "去掉自定义头", "remove_headers": ["X-Api-Key"]}
  ]
}
```

- `evidence_id` 必填；必须是本任务 `sent=1` 且有 HTTP 状态码的证据，Flow 中有请求与响应。普通 HTTP、replay、authz_compare 来源均可；跨任务、未发送或无状态码拒绝，0 发送。
- `variants` 1–4 个，按给定顺序逐个执行。`label` 去首尾空白后必填，≤40 字（Unicode 字符）。
- 变体从原 Flow 重建请求：方法、URL、正文不变。先应用 `remove_headers` 与匿名移除，再应用 `set_headers`；同名头大小写不敏感。`anonymous=true` 移除 `Authorization`、`Cookie`，且 `set_headers` 不得同时设置这两个头（`remove_headers` 可重复列出）。
- `set_headers` 值支持第 1 节占位符。请求头仍执行 `webapi_http` 的合法性与 Host/framing 限制；原请求按 replay 规则跳过伪头与 `Content-Length`，Host 必须与 URL authority 一致。

### 每个变体的检查、存证与来源

- 现读任务测试策略，复用 `httpPolicyDecision` 与 `requestRecorder` 的 claim→send→finish 路径：scope origins / unrestricted、`requestReview` 危险信号、同任务同指纹 unknown 保护、有界捕获都一致；每个变体只发送一次，unknown 不自动重试。
- 工具本身**不进入** `approval=all` 的整体审批。变体若需审批，则该变体 `status=review_required`、`hint=not_sent`，不发送、**不写 `webapi_requests`**、`evidence_id` 为空；结果提示“改用 webapi_http 走人工审批”。因此不会遗留 `review_required` 请求行阻断后续模型调用。
- 越界变体 `status=denied`，不发送，但按 `webapi_http` 规则写 `sent=false` 的 denied 证据。发送/读取失败或 capture_failed 为 `unknown`；认领、存储或 Flow 解析失败为 `error`。未形成可比响应时 hint 都是 `not_sent`。
- 变体证据的 `session_id` 是调用会话（含子 Agent 会话），`turn_id` 是调用 turn；`call_id = authz_compare:<原工具call_id>:<1开始的变体序号>`。证据列表/详情的 `metadata.source` 由该前缀确定为 `authz_compare`，不新增 `webapi_requests` 列。
- 每个变体处理后写一条 compare 记录。若 compare 记录保存失败，停止后续变体并明确报错；已形成的请求证据不能靠重发恢复。

### 工具结果字段与 hint

结果文本为 Markdown 表格，每个变体一行：

| 字段 | 含义 |
|---|---|
| `label` | 变体名 |
| `evidence_id` | 新/已存在的变体证据；review_required、部分 error 时为空；重复遇到同一 unknown 时指向原未知证据 |
| `status` | `sent` / `review_required` / `denied` / `unknown` / `error` |
| `http_status` | 有状态码时为整数，否则为空 |
| `body_bytes` | 已捕获响应正文字节数，无响应为 0 |
| `same_status` | 可比较时原/变体状态码相同 |
| `body_identical` | 原/变体已捕获响应正文逐字节相同 |
| `length_ratio` | 变体已捕获正文长度 ÷ 原正文长度；双方均 0 时为 1，原 0 而变体非 0 时为 0；不可比时为 0 |
| `hint` | 下列确定性提示 |

hint 优先级：

1. 没有可比较响应 → `not_sent`。
2. 401/403，或 3xx 的 `Location`（不分大小写）含 `login` / `signin` → `denied`。
3. 404 → `not_found`。
4. 状态码相同，且正文相同或长度比在闭区间 `[0.95, 1.05]` → `same_as_original`。
5. 其它 → `different`。

正文比较只针对已捕获字节；响应捕获截断仍在证据元数据中如实标注。结果末尾提示：如认为构成越权，用 `webapi_finding` 记录候选并引用原请求与变体证据；验证时用 `contrast` 引用。

## 4. 记录表与接口

表 `webapi_authz_compares`，每个变体一行：

```text
id, task_id, session_id, original_evidence_id, label, variant_evidence_id,
status, hint, http_status, created_at
```

索引：`(task_id, created_at DESC)`、`(original_evidence_id)`。`webapiSchemaStages` 先识别 M5a 的 `{41,37}` 阶段，再单事务补本表与两个索引；不改旧表。

`GET /api/webapi/tasks/{id}/authz-compares?evidence_id=`（原生鉴权内）：

- 返回 `{items:[...]}`，新到旧（同刻按 id 倒序），最多 100 条；不带 `evidence_id` 返回该任务全部，带时按 `original_evidence_id` 过滤；任务不存在 404。
- item = `{id, task_id, session_id, original_evidence_id, label, variant_evidence_id, status, hint, http_status, created_at}`；无状态码时 `http_status=null`，无证据时 `variant_evidence_id=""`。

## 5. 子 Agent 与限制

- `webapi_authz_compare` 已加入 `oracleExplorerTools`，子 Agent 提示词同步列明。子会话的占位符、origins、策略、存证归属与研究会话一致。
- 工具只负责换身份重发与比较，不代替登录、不刷新过期会话、不判断账号是否本来就有权访问，也不把响应差异自动认定为漏洞。
- 同一调用中各变体顺序执行，不并行；没有一键重放的漏洞卡限频/单飞规则（它不是漏洞卡 replay）。同指纹 unknown 仍由共享请求认领事务阻止重发。
