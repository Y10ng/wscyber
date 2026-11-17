# 资产登记与从资产发起任务（C1 后端）

资产是服务端 `webapi_assets` 实体（与上游浏览器“资产池”无关）。路由均在原生鉴权内（`/api/webapi`）。仅临时库合成数据 Go 测试通过；前端、真实节点端到端未验。

- `GET /assets?archived=0|1`（默认 0）→ `{assets:[Asset]}`，最近更新在前；`GET /assets/{id}` → Asset，不存在 404。
- `PUT /assets/{id}`（id 由客户端生成，规则同任务 id）`{name, owner, environment, known_info, objective, origins[], forbidden, scope?, approval?, archived?}`：整份覆盖、保存即生效，新建 201、更新 200（保留 `created_at`）。校验：name 去首尾空白后必填、≤128 字；origins ≥1，规范化/去重/排序同任务；scope/approval 枚举同 `POLICY.md`，空＝`origins`/`high_risk`。未知字段忽略。
- Asset = 以上字段 + `id, created_at, updated_at, credentials:[{id,label,username,has_secret,note,updated_at}]`；**任何响应都不含 secret**。
- `PUT /assets/{id}/credentials/{cid}` `{label, username, secret?, note}`（未知字段 400）：secret 省略或 `""` 保留原值；`cid` 已属其他资产 409。`DELETE` 同路径，不存在 404。两者返回资产视图。
- 密文：AES-256-GCM（AAD 绑定 asset_id+credential_id），列存 `v1:base64(nonce‖密文)`。密钥 `<state-dir>/webapi-secrets.key`（与 hub.sqlite 同目录，64 位 hex，0600 普通文件，首次写 secret 时 `O_EXCL` 生成）。库中已有密文而密钥缺失：写新 secret 与发起任务均报“密钥文件缺失…不会生成新密钥覆盖”，原密文保留；权限非 0600 拒用。
- `PUT /assets/{id}/tasks/{task_id}` `{objective?, scope?, approval?}`（可空体）= Go `(*Capability).LaunchAssetTask(ctx, assetID, taskID, AssetLaunchOptions)`：
  1. 单事务建任务（known_info＝资产已知信息 + “环境：/负责人：”行；objective 缺省用资产；origins 用资产）+ 写策略（缺省资产默认，含变更记录）+ `webapi_asset_tasks` 关联。task_id 已被非本资产任务占用 409。
  2. 首条消息 = 前端 `webApiFirstMessage` 原文 + “测试账号”块（发起时解密 名称/用户名/密码，只进 Run 输入，不写 webapi_tasks）+ “禁止行为（提示词约束，非拦截）”块。
  3. 节点同前端：在线且公告 `wscyber.webapi-http.v1`，按 node_id 取第一个；本机受控模型节点（`wscyber.local-model.v1`）须已配置 provider/model，Run `max_turns=3`，其余 0。会话已存在则沿用原绑定节点。
  4. 原生 `Sessions.OpenSession`（session_id＝task.initial_session_id）→ `Sessions.RunTurn`，每次尝试新 requestID（上游账本会回放被拒结果）。
  - 幂等/续完：同进程按 task_id 串行；任务已存在沿用原内容（不按资产现值重算）；会话已存在不再 Open；会话事件前 500 条中已有 user 消息即视为已启动，不再 Run。
  - 响应：201（本次新建任务）/200（续完或 `idempotent:true`）`{asset_id, task, session_id, node_id, turn_id, progress:{task_created,session_opened,turn_started}, created, idempotent}`。失败 `{error, stage, progress, task, session_id, node_id}`：无可用节点/绑定节点离线/节点拒绝 UNAVAILABLE/本机模型未配置 → 409；资产不存在 404；参数 400；密钥问题 `stage=first_message` 500；会话服务异常 502/503。
- `GET /assets/{id}/tasks` → `{asset_id, tasks:[{task_id, objective, initial_session_id, created_at, finding_count}]}`，最近在前。任务 GET（含 `?session_id=` 与 PUT 响应）带 `asset:{id,name}|null`。
- 已知限制：测试账号明文进入首条用户消息，因而随原生会话事件（`chat_aop_events`）持久化并在执行视图可见；禁止行为只是提示词约束。
