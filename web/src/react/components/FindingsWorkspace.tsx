import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Alert, Button, Select, Space, Tag } from 'antd'
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons'
import { fetchFinding, fetchFindingEvidence, fetchFindingHistory, fetchFindings, fetchFindingSummary } from '../findingsApi.ts'
import type { Mission, Thread } from '../types.ts'
import { FindingDetail, FindingHistory, FindingList, type FindingRevision, type FindingSummary, type FindingSection } from '../../ui/Findings.tsx'
import { WorkspaceSplit } from '../../ui/WorkspaceSplit.tsx'
import { CopyButton } from '../../ui/SurfaceControls.tsx'
import { ActionEvidenceDrawer } from './ActionEvidenceDrawer.tsx'
import { useFindingRead } from '../useRecordRead.ts'
export { findingReadDelay } from '../useRecordRead.ts'
import { VerificationWorkspace } from './VerificationWorkspace.tsx'

export function FindingsWorkspace({ mission, threads, active }: { mission: Mission; threads: Thread[]; active: boolean }) {
  const [thread, setThread] = useState(''), [cursors, setCursors] = useState(['']), [page, setPage] = useState(0), [selected, setSelected] = useState<FindingSummary | null>(null)
  const navigation = useRef<HTMLDivElement | null>(null), returnID = useRef('')
  const cursor = cursors[page]
  const read = useCallback((signal: AbortSignal) => fetchFindings(mission.id, thread, cursor, signal), [mission.id, thread, cursor])
  // 阅读详情时固定当前候选集合；更新只提示，不覆盖用户已读正文。
  const list = useFindingRead(`${mission.id}:${thread}:${cursor}`, active && !selected, read, true)
  useLayoutEffect(() => {
    if (!selected && returnID.current) {
      const target = Array.from(navigation.current?.querySelectorAll<HTMLElement>('[data-finding-id]') ?? []).find(el => el.dataset.findingId === returnID.current)
      target?.focus({ preventScroll: true }); returnID.current = ''
    }
  }, [selected])
  const open = (row: FindingSummary) => { if (active && list.value?.items.includes(row)) setSelected(row) }
  if (!active) return null
  const rail = <div ref={navigation} className="finding-stack">
    <div className="workspace-nav-heading"><h2>候选发现</h2>{!selected && <Button icon={<ReloadOutlined />} loading={list.busy} onClick={list.refresh}>刷新候选</Button>}</div>
    <div className="finding-list-filter" hidden={!!selected}><Select aria-label="候选研究方向" value={thread} disabled={!!selected} options={[{ value: '', label: '全部方向' }, ...threads.filter(t => t.mission_id === mission.id).map(t => ({ value: t.id, label: t.title }))]} onChange={v => { setThread(v); setCursors(['']); setPage(0) }} /><span>第 {page + 1} 页</span>{page > 0 && !selected && <Button onClick={() => { setCursors(['']); setPage(0) }}>回到最新</Button>}</div>
    <FindingList compact={!!selected} selected={selected?.id} items={list.value?.items ?? []} busy={list.busy && !list.value} error={list.error ? `候选未更新：${list.error}` : ''} onOpen={open} pager={{ previous: !selected && page > 0, next: !selected && !!list.value?.next_cursor && !list.error, onPrevious: () => setPage(n => n - 1), onNext: () => { if (!selected && list.value?.next_cursor) { setCursors(old => [...old.slice(0, page + 1), list.value!.next_cursor]); setPage(page + 1) } } }} />
  </div>
  return <div className="finding-workspace"><WorkspaceSplit navigation={rail} navigationLabel="候选导航" detailOpen={!!selected}>
    {selected && <FindingReader key={`${mission.id}:${selected.id}`} mission={mission} selection={selected} active={active} onBack={() => { returnID.current = selected.id; setSelected(null) }} />}
  </WorkspaceSplit></div>
}
function FindingReader({ mission, selection, active, onBack }: { mission: Mission; selection: FindingSummary; active: boolean; onBack: () => void }) {
  const [revisionID, setRevisionID] = useState(selection.revision_id)
  const latestRead = useCallback((signal: AbortSignal) => fetchFindingSummary(mission.id, selection.id, signal), [mission.id, selection.id])
  const latest = useFindingRead(`${mission.id}:${selection.id}:summary`, active, latestRead, true)
  const read = useCallback((signal: AbortSignal) => fetchFinding(mission.id, selection.id, revisionID, signal), [mission.id, selection.id, revisionID])
  const detail = useFindingRead(`${mission.id}:${selection.id}:${revisionID}`, active, read)
  const current = latest.value ?? selection
  const heading = useRef<HTMLDivElement | null>(null)
  useEffect(() => { heading.current?.focus({ preventScroll: true }) }, [])
  return <div className="finding-stack">
    <div className="workspace-toolbar" ref={heading} tabIndex={-1} aria-label={`正在阅读 ${selection.number}`}><Button type="text" icon={<ArrowLeftOutlined />} onClick={onBack}>返回候选列表</Button><Space wrap><Button icon={<ReloadOutlined />} loading={detail.busy} onClick={() => { detail.refresh(); latest.refresh() }}>重新读取</Button>{current.revision_id !== revisionID && <Button type="primary" onClick={() => setRevisionID(current.revision_id)}>查看更新</Button>}</Space></div>
    {latest.error && <Alert type="error" showIcon title={`更新状态未取得：${latest.error}`} action={<Button onClick={latest.refresh}>重读状态</Button>} />}
    {!detail.value && <FindingDetail busy={detail.busy} error={detail.error} onEvidence={() => {}} />}
    {detail.value && detail.error && <Alert type="error" showIcon title={`详情未更新：${detail.error}`} />}
    {detail.value && <FindingContentWithEvidence key={detail.value.id} value={detail.value} mission={mission} active={active} onRevision={setRevisionID} historical={current.revision_id !== revisionID} />}
  </div>
}
function FindingContentWithEvidence({ value, mission, active, onRevision, historical }: { value: FindingRevision; mission: Mission; active: boolean; onRevision: (id: string) => void; historical: boolean }) {
  const [section, setSection] = useState<FindingSection>('overview')
  const [index, setIndex] = useState<number | null>(null), evidenceTrigger = useRef<HTMLElement | null>(null)
  const read = useCallback((signal: AbortSignal) => fetchFindingEvidence(value, index ?? -1, signal), [value, index])
  const evidence = useFindingRead(`${value.id}:${index}`, active && index !== null, read)
  const shown = active && index !== null ? evidence.value : null
  const restoreEvidenceFocus = useRef(false)
  const close = () => { restoreEvidenceFocus.current = true; setIndex(null) }
  // 关闭提交及抽屉焦点锁的passive清理完成后，归还原来源焦点。
  useEffect(() => {
    if (index === null && restoreEvidenceFocus.current) {
      restoreEvidenceFocus.current = false
      if (active && evidenceTrigger.current?.isConnected) evidenceTrigger.current.focus({ preventScroll: true })
    }
  }, [index, active])
  useLayoutEffect(() => { if (!active) { restoreEvidenceFocus.current = false; setIndex(null) } }, [active])
  return <><FindingDetail value={value} historical={historical} section={section} onSectionChange={next => { setIndex(null); setSection(next) }}
    onEvidence={i => { evidenceTrigger.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; setIndex(i) }}
    history={<FindingHistoryReader value={value} active={active && section === 'history'} onRevision={onRevision} />}
    verification={<VerificationWorkspace source={value} mission={mission} active={active && section === 'verification' && index === null} canStart={!historical} />} />
    {index !== null && <div className="finding-stack" aria-label="关联材料读取状态"><Space wrap><Button onClick={close}>关闭关联材料</Button>{evidence.busy && <Tag>读取材料中</Tag>}</Space>{evidence.error && <Alert type="error" showIcon title={evidence.error} action={<Button onClick={evidence.refresh}>重读材料</Button>} />}{shown && !shown.matches_snapshot && <Tag color="warning">原件状态已变化；历史引用时点保持</Tag>}
      {shown?.observation && <section className="finding-stack" aria-label="关联观察原件"><Space wrap><Tag>观察原件</Tag><CopyButton text={shown.observation.content} label="复制观察原文" /></Space><pre className="material-text">{shown.observation.content}</pre></section>}
    </div>}
    <ActionEvidenceDrawer action={shown?.action ?? null} mission={mission} thread={shown?.thread} readError={evidence.error} refreshing={evidence.busy} onRefresh={evidence.refresh} onClose={close} />
  </>
}
function FindingHistoryReader({ value, active, onRevision }: { value: FindingRevision; active: boolean; onRevision: (id: string) => void }) {
  const [cursors, setCursors] = useState(['']), [page, setPage] = useState(0), cursor = cursors[page]
  const read = useCallback((signal: AbortSignal) => fetchFindingHistory(value.mission_id, value.finding_id, cursor, signal), [value.mission_id, value.finding_id, cursor])
  const history = useFindingRead(`${value.finding_id}:${cursor}`, active, read)
  return <div className="finding-stack"><Button onClick={history.refresh} loading={history.busy}>刷新保存历史</Button><FindingHistory items={history.value?.items ?? []} busy={history.busy} error={history.error} selected={value.id} onOpen={row => onRevision(row.id)} pager={{ previous: page > 0, next: !!history.value?.next_cursor && !history.error, onPrevious: () => setPage(p => p - 1), onNext: () => { if (history.value?.next_cursor) { setCursors(old => [...old.slice(0, page + 1), history.value!.next_cursor]); setPage(page + 1) } } }} /></div>
}
