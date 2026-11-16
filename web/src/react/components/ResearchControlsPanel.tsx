import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { Alert, Button, Card, Popconfirm, Popover, Space, Tag, Tooltip } from 'antd'
import { FileAddOutlined, PauseOutlined } from '@ant-design/icons'
import { TaskModal } from '../../ui/TaskModal.tsx'
import { ResearchControlBar, SupplementEditor, SupplementList } from '../../ui/ResearchControls.tsx'
import { controlHelp, controlLabels, type ResearchCommand, type SupplementMeta } from '../../ui/researchControls.ts'
import { MaterialText } from '../../ui/MaterialLibrary.tsx'
import { SkillCallFacts, type SkillCallFact } from '../../ui/SkillRecords.tsx'
import { ResearchControlSession, listSupplements, readSupplement, researchPath, sameResearchOwner, type ResearchOwner } from '../researchControls.ts'
import { requestApi } from '../api.ts'
import { needsUnknownResolution, type Snapshot } from '../types.ts'

function SupplementDetail({ owner, item, mode }: { owner: ResearchOwner; item: SupplementMeta; mode: 'text' | 'calls' }) {
  const [text, setText] = useState<SupplementMeta | null>(null), [items, setItems] = useState<SkillCallFact[]>([]), [error, setError] = useState(''), [busy, setBusy] = useState(true)
  const [cursors, setCursors] = useState(['']), [next, setNext] = useState('')
  const after = cursors.at(-1)!
  useEffect(() => {
    const controller = new AbortController(); let live = true
    setBusy(true); setError(''); setText(null); setItems([]); setNext('')
    const load = async () => {
      try {
        if (mode === 'text') { const value = await readSupplement(owner, item.id, controller.signal); if (live) setText(value) }
        else {
          const v = await requestApi<{ mission_id: string; thread_id: string; supplement_id: string; items: (SkillCallFact & { mission_id: string; thread_id: string })[]; next_cursor: string }>(`${researchPath(owner)}/supplements/${item.id}/calls?limit=25${after ? '&after=' + after : ''}`, { signal: controller.signal })
          if (v.mission_id !== owner.mission || v.thread_id !== owner.thread || v.supplement_id !== item.id || !Array.isArray(v.items) || v.items.length > 25 || v.items.some(c => c.mission_id !== owner.mission || c.thread_id !== owner.thread || !/^[0-9a-f]{32}$/.test(c.id) || ![true, false, null].includes(c.attempted)) || (v.next_cursor && !/^[0-9a-f]{32}$/.test(v.next_cursor))) throw new Error('调用关联归属或结构不匹配。')
          if (live) { setItems(v.items); setNext(v.next_cursor) }
        }
      } catch (err) { if (live) setError(err instanceof Error ? err.message : '读取失败') }
      finally { if (live) setBusy(false) }
    }; void load()
    return () => { live = false; controller.abort() }
  }, [owner.mission, owner.thread, item.id, mode, after])
  if (mode === 'calls') return <SkillCallFacts kind="supplement" items={items} busy={busy} error={error} pager={{ previous: cursors.length > 1, next: Boolean(next), onPrevious: () => setCursors(v => v.slice(0, -1)), onNext: () => setCursors(v => [...v, next]) }} />
  return <>{error && <Alert type="error" title={error} />}{busy && <span role="status">正在读取原件…</span>}{text && <MaterialText detail={{ name: text.source, description: '用户声明 · 不代表已验证、授权或模型已采纳', content: text.content!, byte_size: text.byte_size, content_sha256: text.content_sha256 }} />}</>
}
export function ResearchControlsPanel({ snapshot, session, maxSteps, onRefresh, compact = false }: { snapshot: Snapshot; session: ResearchControlSession; maxSteps?: number; onRefresh: () => void; compact?: boolean }) {
  const state = useSyncExternalStore(session.subscribe, session.snapshot), owner = { mission: snapshot.mission.id, thread: snapshot.thread.id }
  const [command, setCommand] = useState<ResearchCommand | null>(null), [editor, setEditor] = useState(false), [materialsOpen, setMaterialsOpen] = useState(false)
  const [items, setItems] = useState<SupplementMeta[]>([]), [error, setError] = useState(''), [busy, setBusy] = useState(false), [refresh, setRefresh] = useState(0)
  const [selection, setSelection] = useState<{ item: SupplementMeta; mode: 'text' | 'calls' } | null>(null)
  const refreshRef = useRef(onRefresh); refreshRef.current = onRefresh
  const revision = useRef(state.revision)
  useEffect(() => { if (revision.current !== state.revision) { revision.current = state.revision; refreshRef.current(); setCommand(null); if (!state.draft) setEditor(false) } }, [state.revision, state.draft])
  useEffect(() => {
    const controller = new AbortController(); let live = true
    setBusy(true); setError(''); setItems([])
    void listSupplements(owner, controller.signal).then(value => { if (live) setItems(value) }).catch(err => { if (live) setError(err instanceof Error ? err.message : '读取失败') }).finally(() => { if (live) setBusy(false) })
    return () => { live = false; controller.abort() }
  }, [owner.mission, owner.thread, state.revision, refresh])
  const ownDraft = state.draft && sameResearchOwner(state.draft, owner) ? state.draft : null
  const ownPending = state.pending && sameResearchOwner(state.pending, owner) ? state.pending : null
  const inFlight = snapshot.attempts.some(a => a.status === 'running') || snapshot.actions.some(a => a.status === 'running') || snapshot.thread.status === 'running'
  const terminal = snapshot.thread.status === 'concluded', unknown = snapshot.actions.some(needsUnknownResolution)
  const continuationBlocked = terminal || inFlight || unknown
  const runBlocked = maxSteps === undefined || snapshot.thread.step_count >= maxSteps
  const working = state.phase === 'writing' || state.phase === 'checking'
  const canConfirm = !state.pending && !terminal && (command === 'pause' || (!continuationBlocked && (command !== 'run' || !runBlocked)))
  const compactBar = <div className="ws-research-actions">
    <Tooltip title={controlHelp.pause}><Button icon={<PauseOutlined />} disabled={Boolean(state.pending) || terminal} onClick={() => setCommand('pause')}>{controlLabels.pause}</Button></Tooltip>
    <Button icon={<FileAddOutlined />} onClick={() => setMaterialsOpen(true)}>补充{items.length ? ` ${items.length}` : ''}</Button>
    <Popover trigger="click" placement="bottomRight" title="更多控制" content={<div className="ws-more-controls">
      <div className="ws-more-controls-meta"><Tag>{snapshot.thread.pause_requested === undefined ? '暂停意图：未知' : snapshot.thread.pause_requested ? '已请求暂停' : '未请求暂停'}</Tag><Tag>模型研究：{snapshot.thread.auto_run ? '开启' : '关闭'}</Tag><span>累计 {snapshot.thread.step_count} / {maxSteps ?? '未知'} 步</span></div>
      <Space wrap>
        <Tooltip title={controlHelp.run}><Button disabled={Boolean(state.pending) || terminal || continuationBlocked || runBlocked} onClick={() => setCommand('run')}>{controlLabels.run}</Button></Tooltip>
        <Tooltip title={controlHelp.resume}><Button disabled={Boolean(state.pending) || terminal || continuationBlocked} onClick={() => setCommand('resume')}>{controlLabels.resume}</Button></Tooltip>
        <Button onClick={() => { if (session.begin(owner) || ownDraft || ownPending?.command === 'supplement') setEditor(true) }}>人工文本补充</Button>
      </Space>
    </div>}><Button>更多控制</Button></Popover>
  </div>

  const content = <>
    {compact ? compactBar : <ResearchControlBar pauseRequested={snapshot.thread.pause_requested} autoRun={snapshot.thread.auto_run} steps={snapshot.thread.step_count} maxSteps={maxSteps} disabled={Boolean(state.pending) || terminal} continuationBlocked={continuationBlocked} runBlocked={runBlocked} onCommand={setCommand} onSupplement={() => { if (session.begin(owner) || ownDraft || ownPending?.command === 'supplement') setEditor(true) }} />}
    {ownPending && <div className="research-write-feedback"><Alert type="warning" showIcon title={working ? '正在提交或核对原请求' : '原提交结果待核对'} description={state.error || '保留原键与载荷，不会自动重发。'} /><code>{ownPending.key}</code><Space wrap><Button disabled={working} onClick={() => void session.check()}>只读核对原提交</Button>{state.retryAllowed && <Popconfirm title="同键、同载荷再次提交？" description="原请求仍可能提交；不会创建新键或改变内容。" onConfirm={() => void session.retry()}><Button disabled={working}>同键重试原提交</Button></Popconfirm>}</Space></div>}
    {!state.pending && state.error && <Alert type="error" showIcon title={state.error} />}{state.notice && <Alert type="success" showIcon title={state.notice} />}
    {!compact && <SupplementList items={items} busy={busy} error={error} onOpen={item => setSelection({ item, mode: 'text' })} onCalls={item => setSelection({ item, mode: 'calls' })} />}
    <TaskModal title={command ? controlLabels[command] : '研究控制'} open={Boolean(command)} onCancel={() => setCommand(null)} destroyOnHidden footer={<Space><Button onClick={() => setCommand(null)}>关闭</Button><Button type="primary" disabled={!canConfirm} loading={state.phase === 'writing'} onClick={() => { if (command && canConfirm) void session.control(owner, command) }}>确认{command ? controlLabels[command] : ''}</Button></Space>}>
      <p>{snapshot.thread.title}</p><p>{command && controlHelp[command]}</p><p>原模型绑定保持；累计 {snapshot.thread.step_count} / {maxSteps ?? '未知'} 步。</p>{state.error && <Alert type="error" title={state.error} />}
    </TaskModal>
    <TaskModal title="人工文本补充" open={editor && Boolean(ownDraft)} onCancel={() => setEditor(false)} destroyOnHidden footer={<Space wrap><Button onClick={() => setEditor(false)}>关闭保留草稿</Button><Popconfirm title="丢弃未提交的本地草稿？" onConfirm={() => { session.discard(); setEditor(false) }}><Button disabled={Boolean(state.pending)}>丢弃草稿</Button></Popconfirm><Button type="primary" disabled={Boolean(state.pending)} loading={state.phase === 'writing'} onClick={() => void session.save()}>保存补充</Button></Space>}>
      <p>{snapshot.thread.title}</p>{ownDraft && <SupplementEditor value={ownDraft.value} disabled={Boolean(state.pending)} error={state.error} onChange={value => session.change(value)} />}
    </TaskModal>
    <TaskModal title={selection ? selection.mode === 'text' ? '人工补充原件' : '补充的模型调用事实' : '补充材料'} open={Boolean(selection) || compact && materialsOpen} onCancel={() => { setSelection(null); setMaterialsOpen(false) }} footer={<Space>{compact && selection && <Button onClick={() => setSelection(null)}>返回补充列表</Button>}<Button onClick={() => { setSelection(null); setMaterialsOpen(false) }}>关闭</Button></Space>} destroyOnHidden>{selection ? <SupplementDetail key={selection.item.id + selection.mode} owner={owner} {...selection} /> : <div className="finding-stack"><Button loading={busy} onClick={() => setRefresh(v => v + 1)}>刷新补充材料</Button><SupplementList items={items} busy={busy} error={error} onOpen={item => setSelection({ item, mode: 'text' })} onCalls={item => setSelection({ item, mode: 'calls' })} /></div>}</TaskModal>
  </>
  return compact ? <div className="ws-compact-controls">{content}</div> : <Card title="研究控制与补充" extra={<Button loading={busy} onClick={() => { setRefresh(v => v + 1); onRefresh() }}>刷新状态</Button>}>{content}</Card>
}
