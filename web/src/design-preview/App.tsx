import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { App as AntApp, Button, Modal, Select, Tooltip } from 'antd'
import { UIProvider } from '../ui/UIProvider.tsx'
import { ApartmentOutlined, ExperimentOutlined, FolderOpenOutlined, MenuFoldOutlined, MenuUnfoldOutlined, PlusOutlined, ReloadOutlined, SettingOutlined, SwapOutlined } from '@ant-design/icons'
import { LibraryPreview } from './TaskConfiguration.tsx'
import { INITIAL_PROGRESS } from './demoProgress.ts'
import { advancePreview, createPreviewProject, createPreviewState, createTaskDraft, supplementPreview } from './previewState.ts'
import { TrafficWorkspace } from './TrafficWorkspace.tsx'
import { FindingsWorkspace, ModelWorkspace, ResearchWorkspace, StatusLabel } from './ResearchWorkspace.tsx'
import { ConfigurationPanel, CreateTaskPanel, SupplementPanel } from './TaskPanels.tsx'
import type { DemoThread } from './types.ts'
import { TrafficDetail } from './TrafficDetail.tsx'
import { projectEvidence, type SurfaceTheme } from './surface.ts'

type Page = 'projects' | 'traffic' | 'library'
type ProjectTab = 'research' | 'findings' | 'models'
const pages = [
  { key: 'projects' as const, label: '研究项目', icon: <ExperimentOutlined /> },
  { key: 'traffic' as const, label: '流量中心', icon: <SwapOutlined /> },
  { key: 'library' as const, label: '资料库', icon: <FolderOpenOutlined /> },
]

export function App() {
  const [generation, setGeneration] = useState(0)
  const [appearance, setAppearance] = useState<SurfaceTheme>('light')
  return <UIProvider appearance={appearance}>
    <PreviewShell key={generation} onReset={() => setGeneration((value) => value + 1)} appearance={appearance} onTheme={() => setAppearance((value) => value === 'light' ? 'dark' : 'light')} />
  </UIProvider>
}

function PreviewShell({ onReset, appearance, onTheme }: { onReset: () => void; appearance: SurfaceTheme; onTheme: () => void }) {
  const { message, modal } = AntApp.useApp()
  const [state, setState] = useState(createPreviewState)
  const [page, setPage] = useState<Page>('projects')
  const [navCollapsed, setNavCollapsed] = useState(false)
  const configTrigger = useRef<HTMLButtonElement>(null)
  const pendingSupplement = useRef(false)
  const supplementTrigger = useRef<HTMLElement | null>(null)
  const reading = useRef<HTMLDivElement>(null)
  const savedScroll = useRef<{ node: HTMLElement; top: number; left: number }[]>([])
  const returnEvidenceFocus = useRef(false)
  const [projectId, setProjectId] = useState('PRJ-2026-01')
  const [tab, setTab] = useState<ProjectTab>('research')
  const [threadId, setThreadId] = useState('THR-101')
  const [findingId, setFindingId] = useState<string | null>('F-2026-001')
  const [modelId, setModelId] = useState<string | null>('LLM-501')
  const [requestId, setRequestId] = useState<string | null>('REQ-202')
  const [draft, setDraft] = useState(createTaskDraft)
  const [createOpen, setCreateOpen] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [configOpen, setConfigOpen] = useState(false)
  const [supplementOpen, setSupplementOpen] = useState(false)
  const [supplementIds, setSupplementIds] = useState<string[]>([])
  const [resumeThread, setResumeThread] = useState<DemoThread | null>(null)
  const [evidenceId, setEvidenceId] = useState<string | null>(null)
  const [streamRound, setStreamRound] = useState<number | null>(null)
  const evidenceTrigger = useRef<HTMLElement | null>(null)
  const inspectorClose = useRef<HTMLButtonElement>(null)
  const data = state.data
  const project = data.projects.find((item) => item.id === projectId)!
  const config = state.configurations[projectId]!
  const progress = state.progress[projectId] ?? INITIAL_PROGRESS
  const currentThread = data.threads.find((item) => item.projectId === projectId && item.id === threadId) ?? data.threads.find((item) => item.projectId === projectId)

  const evidence = evidenceId ? projectEvidence(data.traffic, projectId, evidenceId) : null
  useEffect(() => { if (evidenceId && page === 'projects') inspectorClose.current?.focus({ preventScroll: true }) }, [evidenceId, page])
  useLayoutEffect(() => {
    for (const { node, top, left } of savedScroll.current) { node.scrollTop = top; node.scrollLeft = left }
    if (!evidenceId && returnEvidenceFocus.current) {
      evidenceTrigger.current?.focus({ preventScroll: true })
      returnEvidenceFocus.current = false
      savedScroll.current = []
    }
  }, [evidenceId])
  function closeEvidence() {
    returnEvidenceFocus.current = true
    setEvidenceId(null)
  }
  function selectProject(id: string) {
    if (!data.projects.some((item) => item.id === id)) return
    setEvidenceId(null); setStreamRound(null); evidenceTrigger.current = null
    savedScroll.current = []; returnEvidenceFocus.current = false; pendingSupplement.current = false
    setProjectId(id)
    setThreadId(data.threads.find((item) => item.projectId === id)?.id ?? '')
    setFindingId(data.findings.find((item) => item.projectId === id)?.id ?? null)
    setModelId(data.modelCalls.filter((item) => item.projectId === id).at(-1)?.id ?? null)
    setRequestId(data.traffic.find((item) => item.projectId === id)?.id ?? null)
    setConfigOpen(false); setSupplementOpen(false); setSupplementIds([]); setResumeThread(null)
  }
  function openProjectEvidence(id: string) {
    if (!projectEvidence(data.traffic, projectId, id)) { void message.error('关联记录不属于当前项目，未打开。'); return }
    evidenceTrigger.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (!evidenceId) savedScroll.current = Array.from(reading.current?.querySelectorAll<HTMLElement>('.scroll-pane, .object-list, .model-code, .model-context') ?? []).map((node) => ({ node, top: node.scrollTop, left: node.scrollLeft }))
    setEvidenceId(id)
    inspectorClose.current?.focus({ preventScroll: true })
  }
  function jumpFinding(id: string) {
    if (!data.findings.some((item) => item.id === id && item.projectId === projectId)) { void message.error('当前项目中没有此发现记录。'); return }
    setFindingId(id); setTab('findings'); setPage('projects')
  }
  function jumpModel(id: string) {
    if (!data.modelCalls.some((item) => item.id === id && item.projectId === projectId)) { void message.error('当前项目中没有此模型记录。'); return }
    setModelId(id); setTab('models'); setPage('projects')
  }
  function jumpResearch(id: string) {
    if (!data.threads.some((item) => item.id === id && item.projectId === projectId)) return
    setThreadId(id); setTab('research'); setPage('projects')
  }
  function advance() {
    if (!currentThread) return
    const next = advancePreview(state, projectId, currentThread.id)
    if (next === state) return
    setStreamRound(next.progress[projectId]!.round)
    setState(next); setFindingId(next.progress[projectId]?.findingId ?? null)
    setModelId(next.data.modelCalls.at(-1)?.id ?? null)
    // 轮次与保存反馈已在阅读面就地显示，不重复弹出全局提示。
  }
  function changeThreadStatus(thread: DemoThread, status: DemoThread['executionStatus']) {
    setState((previous) => ({ ...previous, data: { ...previous.data, threads: previous.data.threads.map((item) => item.id === thread.id && item.projectId === projectId ? { ...item, executionStatus: status } : item) } }))
  }
  function togglePause(thread: DemoThread) {
    if (thread.executionStatus === '运行中') {
      changeThreadStatus(thread, '已暂停后续派发')
      void message.info('已暂停后续派发；不表示撤销在途请求。')
    } else if (thread.executionStatus === '已暂停后续派发') setResumeThread(thread)
  }
  function createTask() {
    const result = createPreviewProject(state, draft)
    if ('error' in result) { setCreateError(result.error); return }
    setEvidenceId(null); setStreamRound(null); evidenceTrigger.current = null
    savedScroll.current = []; returnEvidenceFocus.current = false; pendingSupplement.current = false
    setState(result.state); setProjectId(result.projectId); setThreadId(result.threadId)
    setRequestId(result.state.data.traffic.at(-1)?.id ?? null); setModelId(result.state.data.modelCalls.at(-1)?.id ?? null); setFindingId(null)
    setPage('projects'); setTab('research'); setCreateOpen(false); setCreateError(null); setDraft(createTaskDraft())
    setConfigOpen(false); setSupplementOpen(false); setSupplementIds([])
    void message.success('任务已创建。此模板只推进本地示例，不执行网络或模型请求。')
  }
  function openSupplement() {
    supplementTrigger.current = configOpen ? configTrigger.current : document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (configOpen) {
      // 等配置弹窗完全退出后再打开，避免焦点陷阱或遮罩重叠。
      pendingSupplement.current = true
      setConfigOpen(false)
    } else setSupplementOpen(true)
  }
  function configurationClosed() {
    if (pendingSupplement.current) { pendingSupplement.current = false; setSupplementOpen(true) }
  }

  return <div className={`preview-shell ${navCollapsed ? 'nav-collapsed' : ''}`}>
    <aside className="side-navigation" aria-label="应用侧栏">
      <div className="brand"><ApartmentOutlined /><strong>wscyber</strong></div>
      <nav aria-label="主导航">{pages.map((item) => <button key={item.key} aria-label={item.label} aria-current={page === item.key ? 'page' : undefined} title={navCollapsed ? item.label : undefined} className="nav-item" onClick={() => setPage(item.key)}>{item.icon}<span>{item.label}</span></button>)}</nav>
      <div className="sidebar-footer"><Button aria-label="切换浅暗主题" aria-pressed={appearance === 'dark'} onClick={onTheme}>{appearance === 'light' ? '浅色' : '暗色'}</Button><div className="preview-tools"><span className="preview-label">设计预览</span><Tooltip title="重置所有内存数据"><Button type="text" aria-label="重置设计预览" icon={<ReloadOutlined />} onClick={() => modal.confirm({ centered: true, title: '重置设计预览？', content: '所有本地修改、任务和创建草稿将清空。', okText: '重置', cancelText: '取消', onOk: onReset })} /></Tooltip></div></div>
    </aside>
    <main className="main-workspace">
      <header className="workspace-heading"><Button type="text" aria-label={navCollapsed ? '展开左侧菜单' : '收起左侧菜单'} aria-expanded={!navCollapsed} icon={navCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />} onClick={() => setNavCollapsed((value) => !value)} /><div className="project-switcher"><Select aria-label="选择项目范围" value={projectId} onChange={selectProject} options={data.projects.map((item) => ({ value: item.id, label: item.name }))} popupMatchSelectWidth={false} /></div><div className="scope-context"><span className="truncate">{project.allowedOrigins.map((origin) => new URL(origin).host).join(' / ')}</span><StatusLabel>{project.executionStatus}</StatusLabel></div><div className="workspace-actions"><Button ref={configTrigger} aria-label="查看任务配置" icon={<SettingOutlined />} onClick={() => setConfigOpen(true)}>配置</Button><Button type="primary" aria-label="创建研究任务" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>新建项目</Button></div></header>
      <section className="page-region" hidden={page !== 'traffic'}><TrafficWorkspace key={projectId} projectId={projectId} requests={data.traffic} selectedId={requestId} onSelect={setRequestId} onResearch={jumpResearch} onFinding={jumpFinding} onModel={jumpModel} /></section>
      <section className={`project-workspace ${evidence ? 'inspecting' : ''}`} hidden={page !== 'projects'} aria-label="项目研究工作区">
        <div ref={reading} className="project-reading"><div className="project-tabs" role="tablist" aria-label="项目工作面">{([{ key: 'research', label: '研究' }, { key: 'findings', label: '发现' }, { key: 'models', label: '模型记录' }] as const).map((item) => <button role="tab" id={`tab-${item.key}`} aria-controls={`panel-${item.key}`} aria-selected={tab === item.key} tabIndex={tab === item.key ? 0 : -1} className={tab === item.key ? 'active' : ''} key={item.key} onClick={() => setTab(item.key)} onKeyDown={(e) => { const keys: ProjectTab[] = ['research', 'findings', 'models']; if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') { e.preventDefault(); const next = keys[(keys.indexOf(tab) + (e.key === 'ArrowRight' ? 1 : 2)) % 3]!; setTab(next); document.getElementById(`tab-${next}`)?.focus() } }}>{item.label}</button>)}</div>
          <div id="panel-research" role="tabpanel" aria-labelledby="tab-research" className="project-panel" hidden={tab !== 'research'}><ResearchWorkspace key={projectId} data={data} projectId={projectId} selectedThreadId={threadId} onThread={setThreadId} progress={progress} streamRound={streamRound} onAdvance={advance} onPause={togglePause} onSupplement={openSupplement} onFinding={jumpFinding} onTraffic={openProjectEvidence} /></div>
          <div id="panel-findings" role="tabpanel" aria-labelledby="tab-findings" className="project-panel" hidden={tab !== 'findings'}><FindingsWorkspace data={data} projectId={projectId} selectedId={findingId} onSelect={setFindingId} onTraffic={openProjectEvidence} /></div>
          <div id="panel-models" role="tabpanel" aria-labelledby="tab-models" className="project-panel" hidden={tab !== 'models'}><ModelWorkspace calls={data.modelCalls.filter((call) => call.projectId === projectId)} selectedId={modelId} onSelect={setModelId} onTraffic={openProjectEvidence} /></div>
        </div>
        {evidence && <aside className="project-inspector" aria-label="项目内报文检查器" onKeyDown={(event) => { if (event.key === 'Escape') { event.stopPropagation(); closeEvidence() } }}><header className="inspector-heading"><div><strong>{evidence.id}</strong></div><Button aria-label="在流量中心打开此报文" onClick={() => { setRequestId(evidence.id); setPage('traffic') }}>流量中心打开</Button><Button ref={inspectorClose} aria-label="关闭项目报文检查器" onClick={closeEvidence}>关闭</Button></header><TrafficDetail key={`${projectId}-${evidence.id}`} request={evidence} onResearch={jumpResearch} onFinding={jumpFinding} onModel={jumpModel} /></aside>}
      </section>
      <section className="page-region" hidden={page !== 'library'} aria-label="版本化资料库"><LibraryPreview selected={draft.configuration.skills.map((ref) => ref.versionId)} onSelect={(ids) => setDraft({ ...draft, configuration: { ...draft.configuration, skills: ids.map((versionId) => ({ versionId, effectiveFromRound: 1 })) } })} /></section>
    </main>
    <CreateTaskPanel open={createOpen} draft={draft} onChange={setDraft} error={createError} onClose={() => setCreateOpen(false)} onCreate={createTask} />
    <ConfigurationPanel open={configOpen} afterClose={configurationClosed} restoreFocus={!pendingSupplement.current} onClose={() => setConfigOpen(false)} project={project} config={config} round={progress.round} onSupplement={openSupplement} />
    <SupplementPanel open={supplementOpen} afterClose={() => supplementTrigger.current?.focus({ preventScroll: true })} onClose={() => setSupplementOpen(false)} config={config} round={progress.round} selected={supplementIds} onSelect={setSupplementIds} onSave={() => { setState((previous) => supplementPreview(previous, projectId, supplementIds)); setSupplementOpen(false); setSupplementIds([]); void message.success(`资料从第 ${progress.round + 1} 轮生效，历史输入不变。`) }} />
    <Modal title="恢复后续派发" open={!!resumeThread} onCancel={() => setResumeThread(null)} okText="恢复" cancelText="取消" onOk={() => { if (resumeThread) changeThreadStatus(resumeThread, '运行中'); setResumeThread(null) }}>
      <p>恢复本地方向后，原已放行项将允许继续派发。本模板不发送实际请求。</p>{resumeThread?.pendingApprovedActionsOnResume.length ? <ul>{resumeThread.pendingApprovedActionsOnResume.map((action) => <li key={action}>{action}</li>)}</ul> : <p className="muted">没有已保存的待派发动作。</p>}
    </Modal>
  </div>
}
