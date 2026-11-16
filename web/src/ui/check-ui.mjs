// 共享库契约检查；只使用现有esbuild/React依赖，不启动服务或访问网络。
import { buildSync } from 'esbuild'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { readFileSync } from 'node:fs'
import assert from 'node:assert/strict'
const root = fileURLToPath(new URL('../', import.meta.url))
const output = buildSync({
  stdin: { contents: `
    import assert from 'node:assert/strict'
    import React from 'react'
    import { renderToStaticMarkup } from 'react-dom/server'
    import { UIProvider } from './ui/UIProvider.tsx'
    import { AppShell } from './ui/AppShell.tsx'
    import { GridFold, StreamText } from './ui/SurfaceControls.tsx'
    import { surfaceTokens, createCopyLifecycle, COPY_HOLD_MS } from './ui/surface.ts'
    import { copyText } from './ui/clipboard.ts'
    import { coverage } from './component-preview/coverage.ts'
    import { EvidenceBytesViewer } from './ui/EvidenceBytesViewer.tsx'
    import { EvidenceExamples } from './component-preview/EvidenceExamples.tsx'
    import { TaskModal } from './ui/TaskModal.tsx'
    import { SecretEntriesEditor } from './ui/SecretEntriesEditor.tsx'
    import { validateSecretEntries, encodeSecretEntries } from './ui/secretEntries.ts'
    import { SecretEntriesExamples } from './component-preview/SecretEntriesExamples.tsx'
    import { DailyUsageChart } from './ui/DailyUsageChart.tsx'
    import { DailyUsageExamples } from './component-preview/DailyUsageExamples.tsx'
    import { SkillExamples } from './component-preview/SkillExamples.tsx'
    import { MaterialEditor } from './ui/MaterialLibrary.tsx'
    import { SkillCallFacts } from './ui/SkillRecords.tsx'
    import { validateMaterial } from './ui/materials.ts'
    import { ResearchBriefEditor, ResearchBriefSummary } from './ui/ResearchBrief.tsx'
    import { emptyResearchBrief, validateResearchBrief, isResearchBrief, trimBriefSpace } from './ui/researchBrief.ts'
    import { ResearchBriefExamples } from './component-preview/ResearchBriefExamples.tsx'
    import { ResearchControlsExamples } from './component-preview/ResearchControlsExamples.tsx'
    import { ContextAssemblyExamples, contextFixture } from './component-preview/ContextAssemblyExamples.tsx'
    import { ContextAssemblyView } from './ui/ContextAssembly.tsx'
    import { VerificationExamples } from './component-preview/VerificationExamples.tsx'
    for (const appearance of ['light', 'dark']) {
      const html = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(VerificationExamples)))
      for (const label of ['独立验证', '新证据支持主张', '模型判读 · 非自动确认', '暂无独立验证', '未形成结论', '验证读取失败']) assert.ok(html.includes(label), label)
      assert.ok(!html.includes('<img src='))
    }
    assert.equal(coverage.find(x => x.name === '独立验证 / 结果 / 新证据')?.status, '可复用')
    console.log('PASS D2共享验证组合：执行/结论分离、浅暗、纯文本、空错误')
    import { FindingDetail, FindingHistory } from './ui/Findings.tsx'
    import { FindingExamples, findingFixture, findingHistory } from './component-preview/FindingExamples.tsx'
    for (const appearance of ['light', 'dark']) {
      const html = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(FindingExamples)))
      for (const label of ['候选 · 未确认', '漏洞描述', '复现步骤', '修复建议', '查看流量', '暂无候选发现', '候选读取失败']) assert.ok(html.includes(label), label)
      assert.ok(!html.includes('<img src='))
      // 新布局明确分区；不再要求敏感证据/历史在默认正文同时挂载。
      const detail = renderToStaticMarkup(React.createElement(FindingDetail, { value: findingFixture, section: 'history', onEvidence() {}, history: React.createElement(FindingHistory, { items: [findingHistory], pager: { onPrevious() {}, onNext() {} }, onOpen() {} }) }))
      for (const label of ['形成候选', '发现时间线与历史', '关联事件']) assert.ok(detail.includes(label), label)
      const evidence = renderToStaticMarkup(React.createElement(FindingDetail, { value: findingFixture, section: 'evidence', onEvidence() {} }))
      for (const label of ['效果未知', '捕获不完整', '支持材料']) assert.ok(evidence.includes(label), label)
      const overview = renderToStaticMarkup(React.createElement(FindingDetail, { value: findingFixture, onEvidence() {} }))
      assert.ok(overview.includes('前提与未验证限制') && !overview.includes(findingFixture.call_id))
      assert.ok(!evidence.includes('<img src='))
      assert.ok(!detail.includes('确认漏洞') && !detail.includes('发起复测') && !detail.includes('生成报告'))
    }
    assert.equal(coverage.find(x => x.name === '发现三段正文')?.status, '可复用')
    console.log('PASS D首片共享组合：浅暗/三段正文/证据时点/保存历史/纯文本/空加载错误')
    for (const appearance of ['light', 'dark']) {
      const html = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(ContextAssemblyExamples)))
      for (const label of ['已装配', '发送阶段未知', '模型理解未证实', '单项超限', '第 1 / 2 页', '本次未记录上下文装配', '装配记录读取失败']) assert.ok(html.includes(label), label)
      const next = renderToStaticMarkup(React.createElement(ContextAssemblyView, { value: contextFixture, attempted: false, page: 1, onPage() {} }))
      assert.ok(next.includes('第 2 / 2 页') && !next.includes('00000000-0000-0000-0000-000000000000'))
    }
    assert.equal(coverage.find(x => x.name === '上下文装配 / 未读范围 / 来源引用')?.status, '可复用')
    console.log('PASS C3-A共享组合：浅暗/有界分页/未记录/加载/错误/装配与发送区分')
    import { emptySupplement, supplementError } from './ui/researchControls.ts'
    assert.equal(emptySupplement().share_with_model, false)
    assert.ok(supplementError({ source: 'a', content: 'b', share_with_model: false }))
    assert.equal(supplementError({ source: 'a', content: 'b', share_with_model: true }), '')
    assert.ok(supplementError({ source: 'a', content: '中'.repeat(5462), share_with_model: true }))
    for (const appearance of ['light', 'dark']) {
      const html = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(ResearchControlsExamples)))
      for (const label of ['暂停研究', '启用模型研究', '仅恢复已批准动作', '补充来源', '读取失败', '发送阶段未知', '暂停意图：未知']) assert.ok(html.includes(label))
      assert.ok(!html.includes('<script>纯文本'))
      assert.ok(!html.includes('supplement-note') && !html.includes('不可变原件；不修改在途输入'))
      assert.ok(html.includes('补充正文（非秘密文本）') && html.includes('提供给后续研究模型'))
    }
    assert.equal(coverage.find(x => x.name === '研究控制 / 人工补充 / 调用事实')?.status, '可复用')
    console.log('PASS C2-B共享组合：三控制/默认不分享/容量/浅暗/纯文本/未知调用')
    assert.equal(emptyResearchBrief().share_with_model, false)
    const brief = { identities: [{ label: '成员A', role: 'member' }], business_rules: '<script>fixture</script>', share_with_model: false }
    assert.deepEqual(validateResearchBrief(brief), {})
    assert.equal(isResearchBrief({ ...brief, token: 'private' }), false)
    assert.equal(trimBriefSpace(String.fromCharCode(0x85) + 'a' + String.fromCharCode(0x85)), 'a')
    assert.equal(trimBriefSpace(String.fromCharCode(0xfeff)), String.fromCharCode(0xfeff))
    assert.ok(validateResearchBrief({ ...brief, identities: [{ label: 'a' + String.fromCharCode(0x2028), role: 'r' }] })['0-label'])
    assert.deepEqual(validateResearchBrief({ ...brief, business_rules: 'a' + String.fromCharCode(13, 10, 9) + 'b' }), {})
    assert.ok(validateResearchBrief({ ...brief, business_rules: '<'.repeat(6000) }).group)
    assert.ok(validateResearchBrief({ ...brief, identities: [{ label: 'A', role: 'r' }, { label: ' A ', role: 'r' }] })['1-label'])
    for (const appearance of ['light', 'dark']) {
      const editor = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(ResearchBriefEditor, { value: brief, onChange() {}, disabled: true, errors: { '0-label': '测试错误' } })))
      assert.ok(editor.includes('业务规则') && editor.includes('复现前提') && editor.includes('测试错误') && editor.includes('aria-checked="false"'))
      assert.ok(!editor.includes('type="password"') && !editor.includes('<script>fixture'))
      const summary = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(ResearchBriefSummary, { value: brief })))
      assert.ok(summary.includes('配置：关闭') && summary.includes('待核验') && summary.includes('&lt;script&gt;'))
      assert.ok(!summary.includes('已认证') && !summary.includes('<script>'))
    }
    assert.ok(renderToStaticMarkup(React.createElement(ResearchBriefExamples)).includes('八身份长文本'))
    assert.equal(coverage.find(x => x.name === '身份说明与业务前提 / 固定分享意图')?.status, '可复用')
    console.log('PASS C2-A共享组合：默认不分享/纯文本/逐项错误/禁用/浅暗/Unicode与容量/样例覆盖')
    const skillDraft = { kind: 'file', classification: 'skill', parent_id: '', name: 'SKILL.md', content: 'method', description: '' }
    assert.ok(validateMaterial(skillDraft)); assert.equal(validateMaterial({ ...skillDraft, description: '适用范围' }), '')
    assert.equal(validateMaterial({ ...skillDraft, classification: undefined }), '')
    for (const appearance of ['light', 'dark']) {
      const skillUI = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(SkillExamples)))
      assert.ok(skillUI.includes('成功加载记录') && skillUI.includes('来源已变化') && skillUI.includes('组件fixture'))
      const editor = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(MaterialEditor, { value: skillDraft, folders: [], onChange() {}, onImportError() {} })))
      assert.ok(editor.includes('适用说明（必填）') && editor.includes('分类'))
      const facts = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(SkillCallFacts, { items: [{ id: '<script>', model: 'fixture', outcome: '中断', attempted: null, started_at: '' }], pager: { onPrevious() {}, onNext() {} } })))
      assert.ok(facts.includes('发送阶段未知') && !facts.includes('<script>'))
    }
    console.log('PASS S1共享组件：显式分类、浅暗SSR、活动失败、调用未知、纯文本与真实样例')
    const chartRows = [{ date: '2026-11-12', input: '0', output: null, calls: '1', missingCalls: '1' }]
    for (const appearance of ['light', 'dark']) {
      const chart = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(DailyUsageChart, { rows: chartRows })))
      assert.ok(chart.includes('精确表格') && chart.includes('趋势图') && chart.includes('用量不完整'))
      assert.ok(chart.includes('tabindex="0"') && chart.includes('aria-live="polite"'))
    }
    assert.ok(renderToStaticMarkup(React.createElement(DailyUsageExamples)).includes('组件fixture'))
    assert.equal(coverage.find(x => x.name === '日用量趋势 / 精确表格')?.status, '可复用')
    console.log('PASS B组共享日图：浅暗SSR/形状/键盘入口/缺口/表格入口/组件样例')
    const storedEntries = [{ id: 'stored', name: 'X-Secret' }]
    assert.deepEqual(validateSecretEntries(storedEntries, { savedNames: ['x-secret'], caseInsensitive: true }), {})
    assert.ok(validateSecretEntries(storedEntries, { savedNames: ['x-secret'], caseInsensitive: true, requireValues: true }).stored.value)
    assert.ok(validateSecretEntries([{ id: 'a', name: 'x' }, { id: 'b', name: 'X' }], { caseInsensitive: true }).b.name)
    const rawJSON = '{"nested":{"a":1,"a":2}}'
    assert.ok(encodeSecretEntries([{ id: 'json', name: 'custom', value: rawJSON }], true).includes(rawJSON))
    assert.equal(encodeSecretEntries([{ id: 'empty', name: 'x', value: '' }]), '[{"name":"x","value":""}]')
    assert.equal(encodeSecretEntries(storedEntries), '[{"name":"X-Secret"}]')
    assert.ok(validateSecretEntries([{ id: 'bad', name: 'p', value: 'private-invalid' }], { json: true }).bad.value)
    for (const appearance of ['light', 'dark']) {
      const editor = renderToStaticMarkup(React.createElement(UIProvider, { appearance }, React.createElement(SecretEntriesEditor, { label: 'Header', value: storedEntries, savedNames: ['X-Secret'], onChange() {} })))
      assert.ok(editor.includes('type="password"') && editor.includes('placeholder="已保存"') && editor.includes('value=""'))
      assert.ok(editor.includes('删除Header条目 1') && !editor.includes('keep') && !editor.includes('replace'))
    }
    const entryExamples = renderToStaticMarkup(React.createElement(SecretEntriesExamples))
    for (const label of ['秘密键值条目编辑器', '长值样例', '错误样例', '禁用条目编辑器']) assert.ok(entryExamples.includes(label))
    console.log('PASS A组共享条目编辑器：浅暗SSR/空与省略/原始JSON/重复名称/接收端必填/实际样例')
    const render = (node) => renderToStaticMarkup(React.createElement(UIProvider, { appearance: 'light' }, node))
    const shell = render(React.createElement(AppShell, { items: [{key: 'one', label: '唯一导航', icon: null}], activeKey: 'one', title: '组件', onNavigate() {} }, '实际内容'))
    assert.ok(shell.includes('唯一导航') && shell.includes('aria-current="page"') && shell.includes('实际内容'))
    assert.ok(!shell.includes('流量中心') && !shell.includes('demoData'))
    console.log('PASS 1/10外壳由调用方注入导航/内容，无业务假入口')
    for (const key of ['regular', 'medium', 'strong', 'heading']) assert.equal(surfaceTokens.light[key] - surfaceTokens.dark[key], 40)
    assert.equal(surfaceTokens.light.base, '#f4f6f9'); assert.equal(surfaceTokens.dark.base, '#10131a')
    console.log('PASS 2/10UI-1浅暗token与语义字重不漂移')
    const fold = render(React.createElement(GridFold, {title: '选项'}, React.createElement('button', null, '内部')))
    assert.ok(fold.includes('aria-hidden="true"') && fold.includes('inert=""'))
    assert.ok(/<button[^>]*type="button"[^>]*class="fold-trigger"/.test(fold))
    const openedFold = render(React.createElement(GridFold, { title: '错误字段', open: true }, React.createElement('input')))
    assert.ok(openedFold.includes('aria-hidden="false"') && !openedFold.includes('inert=""'))
    const stream = render(React.createElement(StreamText, {text: '<img src=x>'}))
    assert.ok(!stream.includes('<img src=x>')); assert.ok(stream.includes('aria-hidden="true"'))
    console.log('PASS 3/10折叠焦点隔离与不可信短文字按文本转义')
    assert.equal(new Set(coverage.map(x => x.key)).size, coverage.length)
    assert.equal(new Set(coverage.map(x => x.category)).size, 6)
    for (const x of coverage) { assert.ok(x.path && x.boundary); assert.ok(['可复用','仅展示','待补','本期不适用'].includes(x.status)) }
    assert.equal(coverage.find(x => x.name === '已保存字节查看 / 当前预览复制')?.status, '可复用')
    for (const name of ['请求响应 / 遮蔽 / 搜索 / 复制', '模型记录 / 未记录态', '验证 / 历史 / 复测记录']) assert.equal(coverage.find(x => x.name === name)?.status, '仅展示')
    console.log('PASS 4/10六类矩阵无重复，未提取业务组合不冒充可复用')
    const evidence = render(React.createElement(EvidenceBytesViewer, { sourceKey: 'fixture:body', label: '本地字节', filename: 'fixture.bin', base64Value: 'PGltZyBzcmM9eD4=' }))
    assert.ok(evidence.includes('&lt;img src=x&gt;') && !evidence.includes('<img src=x>'))
    assert.ok(evidence.includes('aria-label="复制当前文本预览"') && evidence.includes('下载已保存字节'))
    assert.ok(evidence.includes('evidence-preview') && !evidence.includes('ws-code-pre'))
    console.log('PASS 7/10 共享字节组件直接渲染，文本/复制语义与样式不依赖正式页面')
    const examples = render(React.createElement(EvidenceExamples))
    assert.ok(examples.includes('字节样例') && examples.includes('同内容换记录') && examples.includes('本地样例'))
    console.log('PASS 8/10 有限字节样例使用真实共享组件和可切换记录身份')
    // 这里只验证共享包装器契约；真实关闭、焦点和草稿由浏览器验收覆盖。
    const onCancel = () => {}, afterClose = () => {}, afterOpenChange = () => {}
    const focusable = { focusTriggerAfterClose: false }
    const draftChild = React.createElement('input', { defaultValue: '仅内存草稿' })
    for (const open of [true, false]) {
      for (const motionProps of [{}, { transitionName: 'fixture-motion', maskTransitionName: 'fixture-mask' }]) {
        const modal = TaskModal({ ...motionProps, open, width: 960, className: 'fixture-modal', keyboard: true,
          destroyOnHidden: true, onCancel, afterClose, afterOpenChange, focusable, children: draftChild })
        assert.equal(modal.props.transitionName, '')
        assert.equal(modal.props.maskTransitionName, '')
        assert.equal(modal.props.open, open)
        assert.equal(modal.props.width, 960)
        assert.equal(modal.props.centered, true)
        assert.equal(modal.props.className, 'task-modal fixture-modal')
        assert.equal(modal.props.keyboard, true)
        assert.equal(modal.props.destroyOnHidden, true)
        assert.equal(modal.props.onCancel, onCancel)
        assert.equal(modal.props.afterClose, afterClose)
        assert.equal(modal.props.afterOpenChange, afterOpenChange)
        assert.equal(modal.props.focusable, focusable)
        assert.equal(modal.props.children, draftChild)
        const wrapped = modal.props.modalRender(draftChild)
        assert.equal(wrapped.props.children, draftChild)
        assert.equal(wrapped.props.style.display, 'contents')
        assert.equal(typeof wrapped.props.onKeyDownCapture, 'function')
      }
    }
    const defaultModal = TaskModal({})
    assert.equal(defaultModal.props.width, 880)
    assert.equal(defaultModal.props.centered, true)
    assert.equal(defaultModal.props.transitionName, '')
    assert.equal(defaultModal.props.maskTransitionName, '')
    console.log('PASS 10/10 任务Modal固定无进出场动画，布局/内容/键盘/关闭回调接口保留')
    ;(async () => {
      const states=[], timers=new Map(); let id=0
      const lifecycle=createCopyLifecycle(x=>states.push(x),(fn,ms)=>{assert.equal(ms,COPY_HOLD_MS);timers.set(++id,fn);return id}, n=>timers.delete(n))
      await lifecycle.run(async()=>true);assert.equal(states.at(-1),'success')
      await lifecycle.run(async()=>false);assert.equal(states.at(-1),'error');assert.equal(timers.size,1)
      timers.get(id)();timers.delete(id);assert.equal(states.at(-1),'idle')
      let finish;const pending=lifecycle.run(()=>new Promise(resolve=>{finish=resolve}))
      lifecycle.reset();const count=states.length;finish(true);await pending;assert.equal(states.length,count)
      await lifecycle.run(async()=>{throw Error('denied')});assert.equal(states.at(-1),'error');lifecycle.reset();assert.equal(timers.size,0)
      assert.equal(await copyText('no browser clipboard'),false)
      console.log('PASS 5/10复制计时、失败、重复与记录变化/卸载异步失效')
    })().catch(e=>{console.error(e);process.exitCode=1})
  `, resolveDir: root, sourcefile: 'ui-check.tsx', loader: 'tsx' },
  bundle: true, write: false, platform: 'node', format: 'cjs', jsx: 'automatic', packages: 'external', loader: { '.css': 'empty' }, metafile: true,
})
for (const input of Object.keys(output.metafile.inputs)) assert.ok(!/design-preview|\/react\/|demoData|previewState|demoProgress/.test(input), input)
console.log('PASS 6/10共享库依赖图不含正式业务/设计fixture')
// 补充检查复制入口绑定的是与pre相同的预览，不以大段材料作为React key。
const viewerSource = readFileSync(new URL('./EvidenceBytesViewer.tsx', import.meta.url), 'utf8')
assert.match(viewerSource, /key=\{props\.sourceKey\}/)
assert.match(viewerSource, /source\.base64Value !== base64Value \|\| source\.charset !== charset/)
assert.match(viewerSource, /key=\{`\$\{source\.revision\}:\$\{mode\}`\} text=\{preview\.text\}/)
assert.match(viewerSource, /<pre[^>]*>\{preview\.text\}<\/pre>/)
assert.ok(!viewerSource.includes('createCopyLifecycle') && !viewerSource.includes('navigator.clipboard'))
const evidenceCss = readFileSync(new URL('./evidence.css', import.meta.url), 'utf8')
assert.ok(!evidenceCss.includes('.ws-') && evidenceCss.includes('flex-wrap: wrap') && evidenceCss.includes('overflow: auto'))
assert.ok(!/font-size:\s*(?:[0-9]|1[0-3])px/.test(evidenceCss))
assert.ok(evidenceCss.includes('font-size: 14px'))
const drawerSource = readFileSync(new URL('../react/components/ActionEvidenceDrawer.tsx', import.meta.url), 'utf8')
assert.match(drawerSource, /\{action && <ActionEvidenceContent key=\{sourceKey\}/)
assert.match(drawerSource, /JSON\.stringify\(\[mission\?\.id.*thread\?\.id.*action\?\.id/)
assert.match(drawerSource, /key=\{JSON\.stringify\(\[sourceKey, mode\]\)\}/)
for (const field of ['reqHeadersDisplay.text', 'respHeadersDisplay.text', 'action.input.body']) assert.ok(drawerSource.includes(`text={${field}}`))
const workspaceSource = readFileSync(new URL('../react/components/ProjectWorkspaceView.tsx', import.meta.url), 'utf8')
assert.ok(workspaceSource.includes('!creationOpen && <ActionEvidenceDrawer'))
console.log('PASS 9/10 复制同源、材料/模式代次、对象身份与创建弹窗隔离边界可检查')
// UI-D1：同一Provider控制正式/设计/组件入口，避免旧16px硬底线回流。
const providerSource = readFileSync(new URL('./UIProvider.tsx', import.meta.url), 'utf8')
assert.match(providerSource, /fontSize: 14, fontSizeSM: 14/)
assert.match(providerSource, /controlHeight: 34, controlHeightSM: 30/)
for (const filename of ['./styles.css', './evidence.css']) {
  const css = readFileSync(new URL(filename, import.meta.url), 'utf8')
  assert.ok(!/font-size:\s*(?:[0-9]|1[0-3])px/.test(css), filename)
}
const appSource = readFileSync(new URL('../react/App.tsx', import.meta.url), 'utf8')
const listSource = readFileSync(new URL('../react/components/ProjectListView.tsx', import.meta.url), 'utf8')
assert.ok(appSource.includes('新建探索') && listSource.includes('ws-page-toolbar') && !listSource.includes('ws-list-toolbar'))
const httpSource = readFileSync(new URL('../react/components/HTTPRecordsWorkspace.tsx', import.meta.url), 'utf8')
assert.ok(httpSource.includes('ws-traffic-surface') && httpSource.includes('<ActionEvidencePanel'))
console.log('PASS UI-D1 14px共享基线、紧凑控件与同排操作静态契约（另需真实布局验收）')
const module = { exports: {} }
new Function('require', 'module', 'exports', output.outputFiles[0].text)(createRequire(import.meta.url), module, module.exports)
