// 纯内存契约与服务端渲染检查；不启动服务、浏览器或写入构建文件。
import { buildSync } from 'esbuild'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

const output = buildSync({
  stdin: {
    contents: `
      import assert from 'node:assert/strict'
      import React from 'react'
      import { renderToStaticMarkup } from 'react-dom/server'
      import { App } from './App.tsx'
      import { createInitialDemoDataset } from './demoData.ts'
      import { createDemoConfiguration } from './TaskConfiguration.tsx'
      import { createDemoTurn, parseDemoOrigins } from './demoProgress.ts'
      import { createPreviewState, advancePreview, supplementPreview, createPreviewProject, createTaskDraft } from './previewState.ts'
      import { EMPTY_FILTER, filterTraffic, resolveTrafficSelection, messageText, searchParts } from './trafficUtils.ts'
      import { ResearchWorkspace, FindingsWorkspace, ModelWorkspace } from './ResearchWorkspace.tsx'
      import { MessageViewer } from './MessageViewer.tsx'
      import { LibraryPreview } from './TaskConfiguration.tsx'
      import { formatDemoHeaders } from './types.ts'
      import { readFileSync } from 'node:fs'
      import { surfaceTokens, STREAM, SPRINGS, springPosition, springCurve, clampSplitRatio, projectEvidence, COPY_HOLD_MS, createCopyLifecycle } from './surface.ts'
      import { GridFold, StreamText } from './SurfaceControls.tsx'
      import { TrafficDetail } from './TrafficDetail.tsx'
      const sourceRoot = ${JSON.stringify(fileURLToPath(new URL('.', import.meta.url)))}
      const source = (name) => readFileSync(sourceRoot + name, 'utf8')

      const data = createInitialDemoDataset()
      const project = data.projects[0]
      const thread = data.threads[0]
      const off = createDemoConfiguration()
      const on = { ...off, saveModelIo: true }
      const offTurn = createDemoTurn(project, thread, off, 2, 1)
      assert.equal(offTurn.call.recordingEnabled, false)
      assert.equal(offTurn.call.actualInputText, null)
      assert.equal(offTurn.call.actualOutputText, null)
      assert.equal(offTurn.request.statusCode, 200)
      assert.equal(offTurn.call.projectId, project.id)
      assert.ok(offTurn.call.linkedRequestIds.includes(offTurn.request.id))
      for (const call of data.modelCalls.filter((call) => !call.recordingEnabled)) {
        assert.equal(call.actualInputText, null)
        assert.equal(call.actualOutputText, null)
      }
      console.log('PASS 1/19关闭模型原文仍保留元数据与目标流量')

      const selected = { ...on, skills: [{ versionId: 'tenant-check@1', effectiveFromRound: 3 }] }
      const before = createDemoTurn(project, thread, selected, 2, 1)
      const after = createDemoTurn(project, thread, selected, 3, 2)
      assert.deepEqual(JSON.parse(before.call.actualInputText).selected_skill_versions, [])
      assert.equal(JSON.parse(after.call.actualInputText).selected_skill_versions[0].versionId, 'tenant-check@1')
      selected.skills.push({ versionId: 'tenant-check@2', effectiveFromRound: 4 })
      assert.equal(JSON.parse(after.call.actualInputText).selected_skill_versions.length, 1)
      console.log('PASS 2/19资料固定版本、后续生效与历史输入不变')

      const withoutPrompt = createDemoTurn(project, thread, { ...on, prohibitedPromptEnabled: false }, 3, 2)
      assert.equal('prohibited_behaviors' in JSON.parse(withoutPrompt.call.actualInputText), false)
      assert.equal(JSON.parse(after.call.actualInputText).prohibited_behaviors, on.prohibitedBehaviors)
      assert.deepEqual(JSON.parse(withoutPrompt.call.actualInputText).scope, project.allowedOrigins)
      console.log('PASS 3/19禁止行为开关不改变目标授权')

      const revisions = [1, 2, 3].map((stage) => createDemoTurn(project, thread, on, stage + 1, stage))
      assert.deepEqual(revisions.map((turn) => turn.finding.verificationStatus), ['待验证', '待验证', '验证成立'])
      assert.equal(new Set(revisions.map((turn) => turn.finding.id)).size, 1)
      assert.equal(new Set(revisions.map((turn) => turn.finding.descriptionPrincipleAndHarm)).size, 3)
      assert.equal(new Set(revisions.map((turn) => turn.request.id)).size, 3)
      assert.equal(new Set(revisions.map((turn) => turn.event.id)).size, 3)
      assert.equal(project.executionStatus, '运行中')
      console.log('PASS 4/19候选、证据正文、验证三步演进，研究不结束')

      assert.deepEqual(parseDemoOrigins('https://api.example.com,https://api.example.com/'), ['https://api.example.com'])
      for (const invalid of ['', 'https://example.com.evil.invalid', 'https://u:p@example.com', 'https://example.com/path', 'https://example.com?q=1', 'javascript:alert(1)']) {
        assert.equal(parseDemoOrigins(invalid), null)
      }
      const other = createDemoTurn(data.projects[1], data.threads.find((item) => item.projectId === data.projects[1].id), off, 2, 1)
      assert.notEqual(other.finding.id, revisions[0].finding.id)
      assert.ok(other.request.url.startsWith(data.projects[1].allowedOrigins[0] + '/'))
      console.log('PASS 5/19明确测试域校验与跨项目演示引用隔离')

      const html = renderToStaticMarkup(React.createElement(App))
      assert.ok(html.includes('设计预览'))
      assert.ok(html.includes('资料库'))
      assert.ok(html.includes('aria-label="流量工作区"'))
      assert.ok(html.includes('aria-label="请求报文查看器"'))
      assert.ok(html.includes('aria-label="响应报文查看器"'))
      assert.ok(html.includes('aria-label="调整流量列表与报文高度"'))
      assert.ok(html.includes('根据保存字段重建'))
      assert.ok(html.includes('aria-label="选择项目范围"'))
      assert.ok(html.includes('REQ-202'))
      assert.ok(!html.includes('tenantB_buyer_fiction'))
      assert.ok(!html.includes('每轮 LLM 输入输出已保存'))
      assert.ok(html.includes('aria-label="研究项目" aria-current="page"'))
      assert.ok(html.includes('aria-label="项目研究工作区"'))
      console.log('PASS 6/19 项目默认首屏与保留挂载的全局流量 SSR：报文/分隔/项目/遮蔽契约')

      const scoped = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id })
      assert.ok(scoped.length > 0)
      assert.ok(scoped.every((request) => request.projectId === project.id))
      const selectedRequest = scoped.find((request) => request.id === 'REQ-202')
      assert.equal(resolveTrafficSelection(scoped, 'REQ-202'), selectedRequest)
      const secondProject = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: data.projects[1].id })
      assert.ok(secondProject.length > 0)
      assert.equal(resolveTrafficSelection(secondProject, 'REQ-202').projectId, data.projects[1].id)
      assert.equal(resolveTrafficSelection([], 'REQ-202'), null)
      const noRows = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id, query: 'not-a-real-path-9472' })
      assert.equal(noRows.length, 0)
      assert.equal(resolveTrafficSelection(noRows, 'REQ-202'), null)
      assert.ok(filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id, query: 'ORD-90021', method: 'GET' }).every((request) => request.method === 'GET' && request.url.includes('ORD-90021')))
      const errors = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id, status: '5' })
      assert.ok(errors.length > 0)
      assert.ok(errors.every((request) => request.statusCode >= 500 && request.statusCode < 600))
      const unknownRows = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id, status: 'unknown' })
      assert.ok(unknownRows.length > 0)
      assert.ok(unknownRows.every((request) => request.sendStatus === '发送效果未知'))
      const unsentRows = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id, status: 'unsent' })
      assert.ok(unsentRows.length > 0)
      const asc = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id }, 'id', true)
      const desc = filterTraffic(data.traffic, { ...EMPTY_FILTER, projectId: project.id }, 'id', false)
      assert.deepEqual(asc.map((row) => row.id), desc.map((row) => row.id).reverse())
      console.log('PASS 7/19 URL/方法/状态筛选、排序、空结果与跨项目选择隔离')

      for (const mode of ['Raw', '格式化', 'Headers']) {
        assert.ok(!messageText(unknownRows[0], 'response', mode, false).includes('HTTP/1.1'))
        assert.ok(messageText(unknownRows[0], 'response', mode, false).includes('禁止自动重放'))
        assert.ok(messageText(unsentRows[0], 'response', mode, false).includes('越界未发送'))
        assert.ok(!messageText(selectedRequest, 'request', mode, false).includes('tenantB_buyer_fiction'))
        assert.ok(messageText(selectedRequest, 'request', mode, true).includes('tenantB_buyer_fiction'))
      }
      const secretHeaders = formatDemoHeaders({ AUTHORIZATION: 'secret', 'Set-Cookie': 'session=secret', 'X-API-Key': 'secret', Accept: 'application/json' }, false)
      assert.equal(secretHeaders.hasSensitive, true)
      assert.ok(!secretHeaders.lines.join(' ').includes('secret'))
      assert.ok(secretHeaders.lines.join(' ').includes('application/json'))
      const literal = searchParts('GET /a?x=[a] and [A]', '[a]')
      assert.equal(literal.filter((part) => part.match !== null).length, 2)
      assert.equal(literal.map((part) => part.text).join(''), 'GET /a?x=[a] and [A]')
      assert.equal(searchParts('abc', '').length, 1)
      const hostile = { ...selectedRequest, responseBody: '<img src="https://external.invalid/a" onerror="alert(1)">' }
      const safeHtml = renderToStaticMarkup(React.createElement(MessageViewer, { request: hostile, side: 'response' }))
      assert.ok(!safeHtml.includes('<img'))
      assert.ok(safeHtml.includes('&lt;img'))
      console.log('PASS 8/19 无响应真实性、敏感头显隐、字面搜索与不可信 HTML 仅作文本')

      const originalState = createPreviewState()
      let evolving = originalState
      const snapshots = []
      for (let i = 0; i < 3; i++) {
        evolving = advancePreview(evolving, project.id, thread.id)
        snapshots.push(evolving)
      }
      const evolvedId = evolving.progress[project.id].findingId
      const evolved = evolving.data.findings.find((finding) => finding.id === evolvedId)
      assert.equal(evolved.verificationStatus, '验证成立')
      assert.equal(evolved.reproductionSteps.length, 3)
      assert.equal(evolved.verificationRecords[0].evidenceRequestIds.length, 3)
      assert.equal(snapshots[0].data.findings.find((finding) => finding.id === evolvedId).verificationStatus, '待验证')
      assert.equal(evolving.data.timelineEvents.filter((event) => event.findingId === evolvedId).length, 3)
      assert.equal(evolving.data.traffic.length, originalState.data.traffic.length + 3)
      assert.equal(evolving.data.modelCalls.length, originalState.data.modelCalls.length + 3)
      assert.equal(evolving.data.projects.find((item) => item.id === project.id).executionStatus, '运行中')
      assert.equal(originalState.data.findings.some((finding) => finding.id === evolvedId), false)
      const paused = { ...evolving, data: { ...evolving.data, threads: evolving.data.threads.map((item) => item.id === thread.id ? { ...item, executionStatus: '已暂停后续派发' } : item) } }
      assert.equal(advancePreview(paused, project.id, thread.id), paused)
      assert.equal(advancePreview(evolving, data.projects[1].id, thread.id), evolving)
      const fourth = advancePreview(evolving, project.id, thread.id)
      assert.equal(fourth.data.timelineEvents.length, evolving.data.timelineEvents.length)
      assert.deepEqual(fourth.data.findings, evolving.data.findings)
      assert.equal(fourth.data.traffic.length, evolving.data.traffic.length + 1)
      assert.equal(fourth.data.graphNodes, originalState.data.graphNodes)
      assert.equal(fourth.data.reports, originalState.data.reports)
      console.log('PASS 9/19 实际状态推进：三版历史/证据保留、暂停门禁、继续研究与旧图谱数据保留')

      const historyBefore = evolving.data.modelCalls.map((call) => call.actualInputText)
      const supplemented = supplementPreview(evolving, project.id, ['tenant-check@2', 'tenant-check@2'])
      assert.equal(supplemented.configurations[project.id].skills.length, 1)
      assert.equal(supplemented.configurations[project.id].skills[0].effectiveFromRound, 5)
      assert.deepEqual(supplemented.data.modelCalls.map((call) => call.actualInputText), historyBefore)
      const supplementedTurn = advancePreview(supplemented, project.id, thread.id)
      assert.equal(JSON.parse(supplementedTurn.data.modelCalls.at(-1).actualInputText).selected_skill_versions[0].versionId, 'tenant-check@2')
      assert.deepEqual(evolving.data.modelCalls.map((call) => call.actualInputText), historyBefore)
      console.log('PASS 10/19 运行中补充固定资料版本：下一轮生效、不回写历史输入')

      const draft = { ...createTaskDraft(), target: '测试订单接口', objective: '核验租户权限', origins: 'https://orders.example.com' }
      assert.ok('error' in createPreviewProject(originalState, draft))
      assert.ok('error' in createPreviewProject(originalState, { ...draft, authorized: true, origins: 'https://real.invalid' }))
      const created = createPreviewProject(originalState, { ...draft, authorized: true, configuration: { ...draft.configuration, prohibitedPromptEnabled: false, skills: [{ versionId: 'tenant-check@1', effectiveFromRound: 99 }] } })
      assert.ok(!('error' in created))
      assert.equal(created.state.data.modelCalls.at(-1).actualInputText, null)
      assert.equal(created.state.data.modelCalls.at(-1).actualOutputText, null)
      assert.equal(created.state.configurations[created.projectId].prohibitedBehaviors, draft.configuration.prohibitedBehaviors)
      assert.equal(created.state.configurations[created.projectId].skills[0].effectiveFromRound, 1)
      assert.deepEqual(created.state.data.projects[0].allowedOrigins, ['https://orders.example.com'])
      const afterCreate = advancePreview(created.state, created.projectId, created.threadId)
      assert.equal(afterCreate.data.modelCalls.at(-1).actualInputText, null)
      assert.equal(afterCreate.data.modelCalls.at(-1).actualOutputText, null)
      assert.equal(afterCreate.data.traffic.length, created.state.data.traffic.length + 1)
      console.log('PASS 11/19 创建需要两输入与明确授权、固定配置、原文关闭实际为 null')

      const noop = () => {}
      const researchHtml = renderToStaticMarkup(React.createElement(ResearchWorkspace, { data: evolving.data, projectId: project.id, selectedThreadId: thread.id, onThread: noop, progress: evolving.progress[project.id], onAdvance: noop, onPause: noop, onSupplement: noop, onFinding: noop, onTraffic: noop }))
      assert.ok(researchHtml.includes('aria-label="推进一轮"'))
      assert.ok(researchHtml.includes('aria-label="候选与证据"'))
      assert.ok(researchHtml.includes('aria-label="补充研究资料"'))
      const findingHtml = renderToStaticMarkup(React.createElement(FindingsWorkspace, { data: evolving.data, projectId: project.id, selectedId: evolvedId, onSelect: noop, onTraffic: noop }))
      for (const label of ['漏洞描述', '复现步骤', '修复建议', '证据与历史']) assert.ok(findingHtml.includes(label))
      assert.ok(findingHtml.includes(evolved.reproductionSteps[0].linkedRequestId))
      const modelHtml = renderToStaticMarkup(React.createElement(ModelWorkspace, { calls: created.state.data.modelCalls.filter((call) => call.projectId === created.projectId), selectedId: null, onSelect: noop, onTraffic: noop }))
      assert.ok(modelHtml.includes('未记录模型原文'))
      assert.ok(modelHtml.includes('aria-label="模型运行列表"'))
      const libraryHtml = renderToStaticMarkup(React.createElement(LibraryPreview, { selected: ['tenant-check@1'], onSelect: noop }))
      assert.ok(libraryHtml.includes('资料版本目录'))
      assert.ok(libraryHtml.includes('移出创建草稿'))
      assert.ok(libraryHtml.includes('固定版本'))
      console.log('PASS 12/19 研究/发现/模型记录/资料库 SSR 契约（不是浏览器与视觉验收）')

      const appSource = source('App.tsx'), css = source('styles.css') + source('../ui/styles.css'), researchSource = source('ResearchWorkspace.tsx')
      assert.equal(projectEvidence(data.traffic, project.id, selectedRequest.id), selectedRequest)
      assert.equal(projectEvidence(data.traffic, data.projects[1].id, selectedRequest.id), null)
      assert.equal(projectEvidence(data.traffic, project.id, 'missing'), null)
      const openHandler = appSource.split('function openProjectEvidence(id: string) {')[1].split('function jumpFinding')[0]
      assert.ok(openHandler.includes('projectEvidence(data.traffic, projectId, id)'))
      assert.ok(openHandler.includes('setEvidenceId(id)'))
      assert.ok(!openHandler.includes('setPage'))
      assert.equal(appSource.split('onTraffic={openProjectEvidence}').length - 1, 3)
      for (const entry of ['查看流量', '查看证据', '查看验证流量', '查看历史流量', '查看复测证据', '查看模型关联流量', '查看未知请求']) assert.ok(researchSource.includes(entry))
      assert.ok(appSource.includes("hidden={page !== 'projects'}"))
      assert.ok(appSource.includes('evidenceTrigger.current?.focus({ preventScroll: true })'))
      assert.ok(appSource.includes('setEvidenceId(null); setStreamRound(null); evidenceTrigger.current = null'))
      assert.ok(source('TrafficWorkspace.tsx').includes('<TrafficDetail'))
      assert.ok(appSource.includes('<TrafficDetail'))
      const detailHtml = renderToStaticMarkup(React.createElement(TrafficDetail, { request: selectedRequest }))
      assert.ok(detailHtml.includes('请求报文查看器') && detailHtml.includes('响应报文查看器'))
      assert.ok(!detailHtml.includes('tenantB_buyer_fiction'))
      console.log('PASS 13/19 项目归属纯函数与所有证据入口静态契约：不切主导航、持久挂载、共享报文、焦点回收')

      for (const key of ['regular', 'medium', 'strong', 'heading']) assert.equal(surfaceTokens.light[key] - surfaceTokens.dark[key], 40)
      assert.equal(surfaceTokens.light.base, '#f5f5f3')
      assert.equal(surfaceTokens.light.plane, '#fcfcfb')
      assert.deepEqual(['base', 'plane', 'inset', 'raised'].map((key) => surfaceTokens.dark[key]), ['#0b0b0a', '#151515', '#1a1a19', '#20201e'])
      for (const palette of Object.values(surfaceTokens)) for (const key of ['base', 'plane', 'inset', 'raised', 'ink', 'muted', 'accent', 'selected']) assert.ok(source('../ui/surface.ts').toLowerCase().includes(palette[key].toLowerCase()))
      assert.ok(appSource.includes('<UIProvider appearance={appearance}>'))
      assert.ok(source('../ui/UIProvider.tsx').includes('document.documentElement.dataset.theme = appearance'))
      assert.ok(source('../ui/UIProvider.tsx').includes('antTheme.darkAlgorithm'))
      assert.ok(html.includes('aria-label="切换浅暗主题"'))
      for (const rule of ['font-variant-numeric: tabular-nums', 'vertical-align: -0.125em', 'text-wrap: pretty', 'var(--focus-inner)', 'var(--focus-outer)', 'button:focus-visible', 'prefers-reduced-motion']) assert.ok(css.includes(rule))
      assert.ok(!css.includes('#000') && !css.includes(': black'))
      assert.ok(!css.includes('1px solid') && !css.includes('inset 0 0'))
      console.log('PASS 14/19 浅暗色/全语义字重差40、portal主题及排版/焦点/无常驻描边静态契约')

      const closedFold = renderToStaticMarkup(React.createElement(GridFold, { title: '折叠测试' }, React.createElement('button', null, '内部按钮')))
      const openFold = renderToStaticMarkup(React.createElement(GridFold, { title: '折叠测试', defaultOpen: true }, '证据'))
      assert.ok(closedFold.includes('aria-expanded="false"') && closedFold.includes('aria-hidden="true"') && closedFold.includes('inert=""'))
      assert.ok(openFold.includes('aria-expanded="true"') && !openFold.includes('inert=""'))
      assert.ok(css.includes('grid-template-rows: 0fr') && css.includes('grid-template-rows: 1fr'))
      assert.ok(css.includes('.fold-clip { min-height: 0; overflow: hidden; }'))
      assert.ok(source('TaskConfiguration.tsx').includes('<GridFold title="高级设置">'))
      assert.ok(researchSource.includes('<GridFold title="候选与证据"'))
      assert.ok(!source('SurfaceControls.tsx').includes('scrollHeight'))
      console.log('PASS 15/19 Grid折叠 SSR语义、inert焦点隔离与高级配置/证据实际落点')

      assert.deepEqual(SPRINGS.drag, { stiffness: 1200, damping: 80, mass: 1 })
      assert.deepEqual(SPRINGS.check, { stiffness: 380, damping: 18, mass: 1 })
      const drag = Array.from({ length: 601 }, (_, i) => springPosition(i / 1000, SPRINGS.drag))
      assert.equal(drag[0], 0)
      assert.ok(drag.every((x, i) => x >= 0 && x <= 1 && (i === 0 || x >= drag[i - 1])))
      assert.ok(drag.at(-1) > 0.9999)
      const check = Array.from({ length: 801 }, (_, i) => springPosition(i / 1000, SPRINGS.check))
      assert.ok(Math.max(...check) > 1.1 && Math.max(...check) < 1.25)
      assert.ok(Math.abs(check.at(-1) - 1) < 0.002)
      for (const kind of ['drag', 'check']) { assert.ok(springCurve(kind).startsWith('linear(')); assert.ok(springCurve(kind).endsWith('1.00000)')) }
      for (const height of [540, 644, 824]) for (const value of [-100, 22, 36, 65, 200]) {
        const clamped = clampSplitRatio(value, height)
        assert.ok(clamped >= 22 && clamped <= 65)
        assert.ok((height - 8) * clamped / 100 >= 150 - 0.01)
        assert.ok((height - 8) * (100 - clamped) / 100 >= 380 - 0.01)
      }
      assert.ok(css.includes('grid-template-rows 600ms var(--spring-drag'))
      assert.ok(css.includes('check-arrive 800ms var(--spring-check'))
      assert.ok(source('TrafficWorkspace.tsx').includes('Math.round(value * 2) / 2'))
      console.log('PASS 16/19 1200/80单调零回弹、380/18微弹边界、mass=1与拖拽边界/释放收敛')

      assert.deepEqual(STREAM, { duration: 90, stagger: 30, blur: 6, easing: 'cubic-bezier(.215,.61,.355,1)' })
      const streamHtml = renderToStaticMarkup(React.createElement(StreamText, { text: '本地示例' }))
      assert.equal(streamHtml.split('class="stream-character"').length - 1, 4)
      assert.ok(streamHtml.includes('animation-delay:90ms'))
      assert.ok(streamHtml.includes('aria-hidden="true"'))
      assert.ok(!source('MessageViewer.tsx').includes('StreamText'))
      assert.ok(researchSource.includes('streamRound !== null'))
      assert.ok(css.includes('animation-delay: 0s !important') && css.includes('filter: none; opacity: 1'))
      console.log('PASS 17/19 明确新本地示例的逐字90ms/30ms/blur6及减弱动效静态契约')

      ;(async () => {
        const published = [], timers = new Map(); let serial = 0
        const lifecycle = createCopyLifecycle((value) => published.push(value), (fn, ms) => { assert.equal(ms, COPY_HOLD_MS); timers.set(++serial, fn); return serial }, (id) => timers.delete(id))
        assert.equal(COPY_HOLD_MS, 2500)
        await lifecycle.run(async () => true)
        assert.equal(published.at(-1), 'success'); assert.equal(timers.size, 1)
        const first = serial
        await lifecycle.run(async () => true)
        assert.ok(!timers.has(first)); assert.equal(timers.size, 1)
        timers.get(serial)(); timers.clear(); assert.equal(published.at(-1), 'idle')
        await lifecycle.run(async () => false)
        assert.equal(published.at(-1), 'error')
        lifecycle.reset(); assert.equal(timers.size, 0)
        let complete
        const pending = lifecycle.run(() => new Promise((resolve) => { complete = resolve }))
        lifecycle.reset(); const count = published.length; complete(true); await pending
        assert.equal(published.length, count); assert.equal(timers.size, 0)
        const controls = source('../ui/SurfaceControls.tsx')
        assert.ok(controls.includes('return lifecycle.reset'))
        assert.ok(controls.includes('[text, lifecycle]'))
        assert.ok(source('TrafficDetail.tsx').includes('<CopyButton label="复制完整 URL"'))
        assert.ok(source('MessageViewer.tsx').includes('<CopyButton'))
        assert.ok(researchSource.includes('<CopyButton'))
        console.log('PASS 18/19 复制成功/失败、2500ms恢复、重复重置、卸载与旧异步完成隔离（可测计时器）')

        const panels = source('TaskPanels.tsx')
        assert.equal(panels.split('<TaskModal title=').length - 1, 3)
        assert.ok(source('../ui/TaskModal.tsx').includes('centered'))
        assert.equal(panels.split('footer={<div className="modal-footer">').length - 1, 3)
        assert.ok(!panels.includes('Drawer'))
        for (const width of [800, 880, 920]) assert.ok(panels.includes('width={' + width + '}'))
        assert.ok(panels.includes('focusable={{ focusTriggerAfterClose: restoreFocus }}'))
        assert.ok(appSource.includes('afterClose={configurationClosed}'))
        assert.ok(appSource.includes('pendingSupplement.current = true'))
        assert.ok(appSource.includes('supplementTrigger.current?.focus({ preventScroll: true })'))
        assert.ok(appSource.includes('if (!evidenceId) savedScroll.current'))
        assert.ok(html.includes('class="side-navigation"') && html.includes('aria-label="收起左侧菜单"'))
        assert.ok(!html.includes('问题与证据'))
        assert.ok(css.includes('grid-template-columns: 224px minmax(0, 1fr)'))
        assert.ok(css.includes('.research-grid.has-activity') && css.includes('.finding-body'))
        assert.ok(!css.includes('1040px'))
        assert.ok(css.includes('max-height: calc(100dvh - 48px)'))
        assert.ok(css.includes('.task-modal .ant-modal-body { flex: 1 1 auto; min-height: 0; overflow: auto;'))
        assert.ok(css.includes('.task-modal .ant-modal-footer { flex: 0 0 auto;'))
        assert.equal(surfaceTokens.light.accent, '#2f6fed')
        assert.equal(surfaceTokens.dark.accent, '#6f9bff')
        assert.ok(!/font-size:\\s*(?:[0-9]|1[0-3])px/.test(css))
        console.log('PASS 19/19 左侧菜单、内容网格、蓝色主操作与三种居中Modal静态契约（不替代浏览器验收）')
      })().catch((error) => { console.error(error); process.exitCode = 1 })
    `,
    resolveDir: fileURLToPath(new URL('.', import.meta.url)),
    sourcefile: 'check-demo-entry.tsx',
    loader: 'tsx',
  },
  bundle: true,
  write: false,
  platform: 'node',
  format: 'cjs',
  jsx: 'automatic',
  loader: { '.css': 'empty' },
  packages: 'external',
  logLevel: 'silent',
}).outputFiles[0].text

const result = { exports: {} }
new Function('require', 'module', 'exports', output)(createRequire(import.meta.url), result, result.exports)
