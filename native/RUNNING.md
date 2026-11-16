# 正式模型入口

## 启动

```bash
# 在仓库根目录；构建与启动分开。脚本内部获取既有 heavy.lock，勿再套同一锁。
# 固定 Cyber SQLite 事务锁补丁须通过该入口应用；普通 go build 不会应用它。
mkdir -p "$PWD/var/full-system-models.Pg2PL1Id"
env GOMAXPROCS=2 GOGC=30 GOMEMLIMIT=384MiB GOTOOLCHAIN=local GOPROXY=off \
  python3 -I scripts/cyber-go-overlay.py -- build -p 1 \
  -o "$PWD/var/wscyber-hub" ./cmd/wscyber-hub

# KEY_FILE 为 0600 普通文件；STATE_DIR 与 UI_DIR 分开，key 不放在二者内。
env -i PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 \
  "$PWD/var/wscyber-hub" --local-hub --model-node \
  --listen 127.0.0.1:PORT --access-key-file KEY_FILE \
  --state-dir STATE_DIR --ui-dir UI_DIR
```

正式分支为 `--model-node` 且**不传** `--model-endpoint`。`ready` 只表示 Hub 已开始监听；模型可用以节点登记、配置分发及原生 Agent 状态为准。退出用 SIGINT/SIGTERM，等待 `stopped`；保留 `STATE_DIR`，下次用同一入口重开。不要同时启动两个进程共用同一状态目录。

## 当前能力与限制

- 页面保存真实 LLM 供应商、HTTPS/自定义地址、代理及模型参数；保存与切换不隐式探测。`configured` 不是连接成功，连接测试、模型列表或实际 Run 才联网。保存成功不等于节点已应用，失败通过原生 `configError` 展示。
- 使用原生 `provider.Resolve/NewProviderFromResolved`、`State.Set`、Profile、Runtime 和 StandardLoop；OpenAI 兼容及 Anthropic 协议按固定上游支持范围。没有三回合、四实例或禁 continue/命令的验收限制。
- 节点公告 `wscyber.model.v1`、`wscyber.webapi-http.v1`，不再用 `wscyber.local-model.v1` 表示正式模式。显式非空 `--model-endpoint` 仍进入原有本机受控验收分支，不属于正式运行。
- 现有配置页仅开放 LLM，Agent、流量、节点配置未实现；密钥保留、视图不回显及 loopback 访问鉴权不变。配置与运行节点使用私有工作区和净环境，不读取开发模型凭据。
- 正式入口可重开自己保存的状态，不自动恢复/重放历史任务，不清库或迁入旧业务系统的数据。新增独立验证后端的二进制启动时，会对**已知旧原生结构**事务性新增两张验证表及索引，保留旧行；未知/半成结构明确报错，没有通用迁移框架。当前用户预览库已于2026-11-15经明确批准完成这次加表并保全原数据；其他旧库的实际切换仍须按范围安排，不能把新binary指向旧库当成只读操作；见[独立验证说明](VERIFICATION.md)。
- `webapi_http` 仍只对任务明确授权的 origins 发请求并保存证据，`webapi_finding` 仍是候选记录；这不是全工具网络授权、全协议流量捕获或自动复现。原生终端/文件等工具按原生 Harness 提供，不等于任意目标已获授权。
- 已有一次获准的局域网真实模型探索，持久终态completed、11条HTTP记录及3个candidate可回看；不是独立确认漏洞或完整研究效果验收。之后的事件争用/证据定位修复只做离线专项和既有结果回看，不自动重跑原任务。最新可操作入口、页面验证及未验范围见[STATUS](../docs/STATUS.md)，未生产部署。
