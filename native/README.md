# native：基于 Cyber Harness 的实现

独立 Go 模块 `wscyber/native`，直接依赖固定版本 Cyber（`replace` 指向 cyber-harness 固定提交），加上对原生前端的源码覆盖。

- 构建与运行：[RUNNING.md](RUNNING.md)
- 代码地图与 API：[../docs/architecture.md](../docs/architecture.md)
- 独立验证语义：[VERIFICATION.md](VERIFICATION.md)
- 固定依赖与许可：[DEPENDENCIES.md](DEPENDENCIES.md)、[ui/LICENSES.md](ui/LICENSES.md)
- 前端覆盖与构建脚本：[ui/SOURCE-OVERLAY.md](ui/SOURCE-OVERLAY.md)、[ui/README.md](ui/README.md)
- 上游补丁：[patches/README.md](patches/README.md)

`cmd/wscyber-native` 与 `--fixture-node`、`--model-config`、`--model-endpoint` 是早期接线时的本机测试入口，保留作测试；正式运行只用 `--local-hub --model-node`。
