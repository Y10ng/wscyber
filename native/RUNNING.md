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
python3 -I scripts/cyber-ui-build.py rebuild --workspace "$PWD/var/native-ui-quality.ZzLkZW2L/ui-work"
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

## 4. 限制

- `webapi_http` 只发任务授权 origins；原生终端等工具无强制出口。
- 正式入口不自动恢复/重放历史任务。
