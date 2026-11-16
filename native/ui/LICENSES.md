# 固定原生 UI：来源与许可边界

核验日期：2026-11-13。**这是获准的本机技术准备，不是可部署/分发许可结论。** 原生页面、主题、图表未改写；未使用线上业务材料、登录态、截图或私有站点资产。

## 固定来源

| 来源 | 固定版本 / 核验 |
|---|---|
| [Cyber Harness](https://github.com/chainreactors/cyber-harness) | `f4b1fb2f33158a1a3c382de9c1cb63807a6b55cf`；只读本地 Git 的该提交导出 `LICENSE`、`.gitmodules`、`web/frontend`、`web/static/.gitkeep`，不使用移动 HEAD/工作区修改。公开 fresh-checkout 路径由固定 codeload 提供，选定文件树必须具有相同 SHA256。 |
| [cyber-ui](https://github.com/chainreactors/cyber-ui) | `147423bd42b665aab361409c57258d9370f75bb0`，对应上述 root 的真实 gitlink；不是 `.gitmodules` 中的移动 main。实际下载固定归档 1,737,387 字节，545 个归档条目。 |
| npm原锁 | root 的原生 `package-lock.json` v3，466 项（含根包），SHA保持不变。Vite alias 直接消费固定 `cyber-ui/packages/*/src`。所有非根项具有 registry.npmjs.org 固定 resolved 和 integrity。 |
| H3-A2显式构建覆盖 | `build-overlay/package{,-lock}.json`，488项。4缺失直接依赖及18传递项；原465个非根项完整不变。版本依据是固定cyber-ui的pnpm importer/包integrity，**不运行pnpm/turbo、不搬其React19/dev环境**。覆盖及逐项来源记录在 `dependencies.overlay.json`；不把它称作原样上游。 |

版本、完整 SHA256、树摘要算法的输入目录、安装脚本标记和工具版本见 [sources.lock.json](sources.lock.json)。树摘要为每个普通文件按相对 POSIX 路径排序，将 `path + NUL + sha256 + LF` 的 UTF-8 字节再做 SHA256；不依赖文件时间/权限或归档压缩字节。root 公网归档压缩哈希不是固定值：实际采用文件树才是校验边界；cyber-ui 同时固定归档与文件树。

## 实际核验到的许可证与未解决项

- Cyber 根固定 `LICENSE` 为 GNU AGPL Version 3 全文，SHA256 `8d56b405468aad11f87ab5763f901e276e08d9646ff5c8481b1762b6b789e9ed`。准备副本原样保留；构建完成时另复制至 `release/notices/cyber-harness-LICENSE`。不凭许可文本推定所有文件 only/or-later、商标权或组合义务已经处理。
- **cyber-ui UNKNOWN：** 已在该固定归档内检索根及子目录 LICENSE/LICENCE/COPYING/NOTICE/COPYRIGHT 文件、根与 16 个子包 `package.json` 的许可字段，以及关键源码许可/版权声明；未发现授权声明。不能推断成 MIT，不能把另一个 root 仓库的 AGPL 自动施加给它，也不能外推为整个上游从来没有许可。保持原始文件/现有注释不变；对外分发或网络部署前必须取得适用于固定版本的授权依据。
- 采用的 alias 子包为 ui/theme/markdown/terminal/aop/cstx/cstx-easm/viewer/file-manager/traffic/ioa；归档还包含其他包，但没有运行它们自己的 build/dev/typecheck/turbo/pnpm 脚本。此表是源采用范围，不是已完成每个界面功能验证。
- 固定归档含真实 `packages/cstx/src/wasm/cstx_wasm_bg.wasm`：3,684,685 字节，magic/version 为真实 Wasm，SHA256 `a49fd04df8d2f827e422ec03efa0079e146f8542e143a97b9c0276dbeb5ad163`。未发现 LFS 指针；没有制造空 wasm/占位 index。**只固定已有生成物，没有证明能从原始 Rust/编译器完全复编译它，其许可归属仍随 cyber-ui UNKNOWN。**
- H3-A原始npm lock及失败轮次的历史记录：声明统计为MIT 412、Apache-2.0 13、ISC 29、BSD-3-Clause 4；其余包括 Apache/BSD 组合、CC-BY-4.0、CC0-1.0、0BSD、MIT AND ISC。2 个缺字段项是根包与 `format@0.2.2`。这些只是 lock 元数据，**不是已经查看每个下载包许可证全文，也不是实际浏览器包完整 SBOM**。执行会话没有安装npm；它正常退出后，主用禁生命周期的npm ci实际安装415包成功。随后tsc失败，未到Vite/publish阶段，因此最终依赖通知清单尚未生成，不能据安装成功宣称许可审查完成。
- 构建脚本在安装/构建成功后收集实际安装包根部可取得的 LICENSE/LICENCE/COPYING/NOTICE/COPYRIGHT 原件、SHA256 和缺项到 `release/notices/` 与 `release/dependency-notices.json`，保留但不公开挂到静态根。未安装的跨平台可选包会明确标记 `installed=false`。不把这一步当嵌套字体/代码/二进制资产与组合许可完整审查。
- 原生 Cyber 品牌/文案仍在未改的源码中，仅作技术构建基线，不声称是最终 wscyber 品牌、商标授权或完整产品。原生 UI 的导航/扫描/模型等入口不代表当前 Hub 已支持这些能力；本片不通过假接口补齐。
- 既有 Go 侧 AGPL/GPL、缺根文本及 carapace 附加声明等限制继续存在，见 [../DEPENDENCIES.md](../DEPENDENCIES.md)。本片无新 Go 模块。

## H3-A2补充依赖：用途、固定来源与许可证声明

正式[覆盖审计](dependencies.overlay.json)逐项含npm resolved、SHA512 integrity、固定pnpm包版本依据及required_by；实际锁在[build-overlay/package-lock.json](build-overlay/package-lock.json)。唯一lock-only禁生命周期解析成功，全部22新增项与固定UI pnpm中的版本和integrity一致；没有更改旧非根条目，也无新hasInstallScript标记。

| 新增包/版本 | 用途与必要性 | npm锁声明（非全文审查） |
|---|---|---|
| `@radix-ui/react-checkbox@1.3.8` | file-manager权限复选控件，源码已有导入 | MIT |
| `react-arborist@3.15.1` | file-manager树组件及Tree/Node类型 | MIT |
| `react-hotkeys-hook@4.6.2` | file-manager快捷键及事件类型 | MIT |
| `react-resizable-panels@2.1.9` | file-manager和traffic已有面板 | MIT |
| checkbox下嵌套 `@radix-ui/primitive@1.1.6`、`react-context@1.2.0`、`react-presence@1.1.8`、`react-primitive@2.1.7`、`react-use-controllable-state@1.2.4`（均`@radix-ui`作用域） | checkbox固定内部依赖；保留旧根目录Radix版本，不升级替换 | MIT（5项） |
| `react-dnd@14.0.5`、`react-dnd-html5-backend@14.1.0`、`dnd-core@14.0.1` | arborist拖放依赖 | MIT（3项） |
| `@react-dnd/asap@4.0.1`、`@react-dnd/invariant@2.0.0`、`@react-dnd/shallowequal@2.0.0`、`fast-deep-equal@3.1.3` | DND调度/校验/比较 | MIT（4项） |
| `hoist-non-react-statics@3.3.2` | react-dnd静态属性保留 | BSD-3-Clause |
| 其下 `react-is@16.13.1` | 满足hoist的^16.7.0；不是引入React16运行时 | MIT |
| `react-window@1.8.11`、`memoize-one@5.2.1` | arborist虚拟树及记忆化 | MIT（2项） |
| `redux@5.0.1`、dnd-core下`redux@4.2.1` | 分别满足arborist与旧DND依赖范围，均是新增 | MIT（2项） |

新增共21 MIT / 1 BSD-3-Clause为锁元数据声明。执行会话退出后，主已实际安装437包并完成tsc/Vite/资源HTTP；通知收集消费覆盖后的实际锁，487条记录中437已安装、50跨平台可选未安装，421个已安装包采得根部通知。新增22包中21采得根部通知，`react-arborist`未采得根部通知（锁声明MIT），仍需补核授权文本来源；这不是整个上游无许可的结论。采集原件/哈希不等逐项完整法律审查，cyber-ui UNKNOWN和Wasm/嵌套资产边界保持，不能称可部署/分发。原始/实际锁及覆盖原件已保存release/provenance，通知位于release/notices，均不公开挂载。

## 安装生命周期审源范围

固定 root `package.json` 的 build 为 `tsc && vite build`；没有 prebuild/postbuild。本片核对 Vite 配置：alias 指向同副本子模块，输出 `../static`，`closeBundle` 写同目录 `.gitkeep`；**两个位置都留在独立构建副本内，不改 outDir 后让 hook 写回上游。**

固定 npm lock 仅标记 `esbuild@0.25.12`、两份 macOS `fsevents@2.3.3/2.3.2` 有安装脚本。未取得这些包后盲跑其脚本：所有生命周期均通过 `npm ci --ignore-scripts` 禁止。主H3-A2实际构建证明：该Linux工具链在禁生命周期安装后可完成tsc/Vite，未额外开放postinstall或下载替代二进制；不外推到其他平台/版本。失败仍须保留日志交回，不自动打开生命周期。Playwright 浏览器下载禁止，未运行任何 e2e/图表/页面测试。
