# 构建与运行

在仓库根目录执行。Go 需 1.26、Node v24.16.0（本机已装）。

## 1. 构建 Hub（应用 SQLite 补丁）

```bash
env GOMAXPROCS=2 GOGC=30 GOTOOLCHAIN=local GOPROXY=off \
  python3 -I scripts/cyber-go-overlay.py -- build -p 1 \
  -o "$PWD/var/wscyber-hub" ./cmd/wscyber-hub
```

普通 `go build` 不会应用 `native/patches/` 的补丁，正式运行必须用上面的入口。定向测试同理：`python3 -I scripts/cyber-go-overlay.py -- test -p 1 ./webapi -run '^TestWebAPIVerification' -count=1`。

## 2. 构建前端

复用已准备好的工作区增量构建（刷新 `native/ui/source-overlay` → tsc → Vite）：

```bash
python3 -I scripts/cyber-ui-build.py rebuild --workspace "$PWD/var/ui-work"
# 产物：该工作区下 release/public
```

工作区丢失时用 `prepare --workspace <新目录>` 重新准备（下载固定来源，见 `native/ui/README.md`）。

## 3. 启动

```bash
env -i PATH=/usr/bin:/bin HOME=/nonexistent LANG=C.UTF-8 \
  "$PWD/var/wscyber-hub" --local-hub --model-node \
  --listen 127.0.0.1:PORT --access-key-file KEY_FILE \
  --state-dir STATE_DIR --ui-dir UI_DIR
```

- `KEY_FILE`：0600 普通文件，内容为页面登录 key；`STATE_DIR` 首次须不存在，之后用同一目录重开。key 不放在 state/UI 目录内。
- 不传 `--model-endpoint` 即正式模型节点；模型在页面“设置”中配置，保存不自动探测，“测试连接”才联网。
- 停止：对 Hub PID 发 SIGTERM，等待 `stopped` 输出。不要两个进程共用一个 state 目录。
- 新 Hub 打开已知旧结构库会事务性新增验证两表（见 `VERIFICATION.md`）。

## 4. 演示环境（全部测试数据）

`python3 scripts/demo-stack.py up`：启动三个本地进程——模拟目标“演示库存 API” `http://127.0.0.1:18080`、按脚本回复的模拟模型 `http://127.0.0.1:18081/v1`，以及独立临时 Hub `http://127.0.0.1:42460/`（走 `--model-endpoint` 受控分支；每次 `up` 新建 `var/demo/state-<时间戳>`，并经 Hub API 自动保存模型配置）。登录 key 在 `var/demo/access.key`（0600）。在页面新建任务时 origin 填 `http://127.0.0.1:18080`。

- Hub 二进制默认用 `var/demo/wscyber-hub`，不存在时用 `var/wscyber-hub`，也可用 `--hub` 指定。日志在 `var/demo/{target,model,hub}.log`，pid 在 `var/demo/pids.json`。
- `status`；`fix on|off` 切换目标“已修复”模式（`/api/admin/config` 返回 401 或 200，供复测得出两种结论）；`down` 停止全部进程并释放端口。
- `selftest [--chain]`：经 Hub API 跑一遍研究，加 `--chain` 时再跑验证、报告和两次复测，只输出数量和状态。
- `approval-demo`：经 Hub API 跑“审批演示”剧本（模拟模型并行发一条测试 DELETE 与一条命中高危规则的无害命令），一轮全部批准、一轮全部拒绝，输出审批与请求状态。页面上首条目标文本含“审批演示”即触发同一剧本。
- 模拟模型按首条消息识别会话类型（研究/独立验证/报告/复测，复测首行须含“复测”），在原生 3 回合上限内推进：同一阶段的工具调用并行发出。不读写 `var/preview`，不连接真实模型。

## 5. 限制

- `webapi_http` 按任务测试策略：scope=origins（默认）只发授权 origins，unrestricted 不限；危险操作审批见 `native/POLICY.md`。原生终端等工具无强制出口（审批只按命令静态规则挂起）。
- 正式入口不自动恢复/重放历史任务。
