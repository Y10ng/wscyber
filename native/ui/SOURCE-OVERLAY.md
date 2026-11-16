# 原生 UI 源码覆盖与离线增量构建

2026-11-14最新：正式源码覆盖已用于配置→执行链及后续有界UI质量片，现16项；原始source_sha256与固定依赖锁保持，主在新的独立工作区离线fork+完整tsc/Vite及真实浏览器验证已通过目标项，仍有明确UI剩余问题。见UI质量验收（见 tag legacy-snapshot）与STATUS最新。下文初始为空/执行者未构建等为机制落地时点，不覆盖后续实际结果；纯检查初次链接测试路径错误及后续修正记录保留。

## 交付权威与固定来源

- 原始来源仍是 `sources.lock.json` 所固定的 root `f4b1fb2…` / cyber-ui `147423bd…`。原始inputs逐树校验，不从旧构建source推导来源正确。
- `dependencies.overlay.json` 和 `build-overlay/package*.json` 原字节不变；不增加依赖、不改锁、不重装旧缓存。
- **页面交付只写本目录 `source-overlay/` 及 `source-overlay.json`。** 不在 `var`、历史inputs/source或module缓存改页面；新构建副本随时可从正式覆盖重建。
- 源码清单初始为空。目录可在首个正式覆盖文件写入时创建，空目录无需`.gitkeep`；未登记文件包括`.gitkeep`均拒绝。

清单格式（以下值仅解释格式，不能直接当真实hash使用）：

```json
{
  "schema": 1,
  "sources_lock_sha256": "与sources.lock.json原字节匹配的SHA256",
  "files": {
    "src/components/ConfigPanel.tsx": {
      "source_sha256": "固定原始inputs对应文件的SHA256",
      "sha256": "source-overlay/src/components/ConfigPanel.tsx当前字节的SHA256"
    }
  }
}
```

路径相对 `web/frontend/`；原始root文件从 `inputs/root-extracted/web/frontend/` 取，`cyber-ui/…` 文件从 `inputs/ui-extracted/…` 取。接受 `src/` 或 `cyber-ui/packages/<包>/src/` 内 `.js/.jsx/.ts/.tsx/.css` 文件：固定原件必须填写真实 `source_sha256`；本项目新增组件填写 `"source_sha256": null`，可为派生源创建父目录。拒绝绝对/逃逸路径、链接、未登记覆盖、hash错位、package/lock、tsconfig和构建配置。协议生成物不属于本轮页面任务，勿改；不通过any/排除tsc掩盖错误。

每次编辑后更新该项 `sha256`，`source_sha256`不随编辑改变；不移动原始来源hash。覆盖完整文件，不引入patch解释器或通用插件框架。所有未覆盖来源继续精确校验，包括意外新增文件。安装依赖只按确切 `source/web/frontend/node_modules` 目录单独处理；不会忽略其他同名目录或整个src。Vite产物仍只写 `source/web/static`，公开时逐文件登记校验。

## 主在前后端收束后的唯一增量构建入口

在仓库根执行。`WORK`必须不存在且其父目录已存在。以下操作**本执行者未运行**；不要在开发会话/服务/浏览器旁执行，不要再套flock（脚本自身使用既有heavy.lock）。

```bash
DONOR="$PWD/var/cyber-h3a2.pba2tbpi/ui-work"
WORK="$PWD/var/native-workflow.8QkW9tkn/ui-work"
python3 -I scripts/cyber-ui-build.py fork --workspace "$WORK" --reuse-workspace "$DONOR"
# fork成功后才执行；这里只运行固定node的tsc/Vite，不执行npm或npm ci。
python3 -I scripts/cyber-ui-build.py build --workspace "$WORK"
```

### rebuild：复用既有工作区的增量构建（2026-11-14新增）

正式源码覆盖更新后，不必再fork新工作区；直接复用当前工作区与现有node_modules：

```bash
WORK="$PWD/var/native-ui-quality.ZzLkZW2L/ui-work"
python3 -I scripts/cyber-ui-build.py rebuild --workspace "$WORK" [--release-dir "$PWD/var/<新验证目录>/release"]
```

- `--release-dir`可选；默认仍写`work/release`。指定var下新派生输出时不复制workspace/node_modules，只将新输出用于下一验证；当前36667仍服务旧release。已有输出仅接受带`build.json`的既有构建release，pending/previous与输出同父目录。
- 不npm ci、不fork/复制node_modules、不做整棵依赖树hash快照、不下载。
- 在同一既有heavy.lock内：把当前正式覆盖刷新进派生source（之前移除的覆盖文件自动恢复固定原件；不改inputs/覆盖/依赖）→一次tsc→一次Vite→发布。
- 先写完整新release到临时目录，成功后一次性替换，只留一个当前release，不留历史版本；tsc/Vite或发布失败保留上一次成功release，返回非零，下一次显式rebuild可重试，不因build-started锁死。
- 前提是该工作区prepared.json对应当前固定锁/依赖覆盖；工作区被手动改动或与固定锁不符会拒绝。

`fork`完全离线：

1. 核原始inputs、固定依赖覆盖、既有build收据/工具链及现有package/lock。
2. 从已核原始inputs建立全新source，应用依赖两文件覆盖及本轮源码逐文件覆盖；**不复制旧source中的页面修改或旧release**。
3. 核现有 `node_modules/.package-lock.json` 与固定锁、已安装版本/必要工具；普通复制整个安装到新目录。不硬链接、不将新node_modules链接到旧workspace。只保留可重新定位的相对内部链接；绝对、逃逸、失效/循环链接与特殊文件拒绝。
4. 复制前后核内容/权限/链接快照，记录donor构建收据hash、安装锁hash与独立依赖树hash；旧安装不修改。此快照证明复用的一致性，不是重新下载tarball的integrity验证或供应链签名。
5. `prepared.json`记录源码覆盖摘要/构建源树摘要、依赖复用事实及`node_build_run=false`。

`build`在运行前复核上述摘要和副本依赖。复用模式不调用npm，仅校验Node版本、执行完整tsc及Vite；这不是仅做transpile或跳过类型检查。新源码覆盖与旧inputs相同不意味着可复用旧JS产物。开始后写独占`build-started.json`；失败保留现场，不自动重试，不删除该记录强行继续，须先诊断再另建目录。新source或正式覆盖在准备/构建中改变均拒绝发布。

原 `prepare`（fresh source/新安装）路径仍保留，但**不是本轮推荐入口**，且本片未运行下载/安装分支。旧schema2工作区只能作为只读donor，不会原地升级。

## 产物与宿主

- `release/public/.wscyber-ui.json`维持原schema2及原有三个固定hash；`ui.go`未改。宿主继续核真实资产字节及资源白名单，不因页面每次改动重新固定一个Go常量。
- `release/provenance/source-overlay.json`及覆盖原文、`release/build.json`中的 `source_overlay_sha256/build_source_tree_sha256/dependency_reuse`记录本轮来源；这些在公开根外。原依赖锁、通知也照旧保存。
- public清单不承诺源码签名或页面验收。主须将私有构建记录与公开资产清单关联核对；不能只拿旧release冒充新源码产物。
- 本片后端仅通过原生 `RunWebSocketWithCapabilities`添加 `wscyber.local-model.v1`，需要主最终唯一Hub构建后才生效；旧Hub不会自动公告新标志。

## 小型纯检查

```bash
python3 -I scripts/cyber-ui-source-check.py \
  --scratch-parent "$PWD/var/native-workflow.8QkW9tkn" \
  --inputs-workspace "$PWD/var/cyber-h3a2.pba2tbpi/ui-work"
```

只用Python标准库/独占临时fixture，禁子进程和下载；验证逐文件覆盖、未登记/来源hash/依赖文件拒绝、精确排除范围、链接与独立复制。可选inputs-workspace只只读核真实inputs/依赖锁/正式覆盖，**不运行旧source/Node、不是前端构建成功**。主最终只需按改动做必要构建和同一用户流程，无需重复H1–H4套餐。
