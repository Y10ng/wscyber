import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'
import { Alert, Button, Popconfirm, Space, Tag } from 'antd'
import { TaskModal } from '../../ui/TaskModal.tsx'
import { CopyButton } from '../../ui/SurfaceControls.tsx'
import { VerificationList, VerificationResultView, VerificationStartFields, verificationTime, verificationStateLabels } from '../../ui/Verifications.tsx'
import type { FindingRevision } from '../../ui/Findings.tsx'
import type { Mission } from '../types.ts'
import { requestApi } from '../api.ts'
import { fetchVerification, fetchVerificationEvidence, fetchVerifications, type VerificationDetail } from '../verificationApi.ts'
import { VerificationSession } from '../verificationSession.ts'
import type { ResearchControlSession } from '../researchControls.ts'
import { controlHelp, controlLabels, type ResearchCommand } from '../../ui/researchControls.ts'
import { useFindingRead } from '../useRecordRead.ts'
import { ActionEvidenceDrawer } from './ActionEvidenceDrawer.tsx'

export const VerificationContext = createContext<{ session: VerificationSession; control: ResearchControlSession; maxSteps: number } | null>(null)
export function VerificationPending({ session }: { session: VerificationSession }) {
  const state = useSyncExternalStore(session.subscribe, session.snapshot)
  if (!state.pending) return null
  const busy = state.phase === 'writing' || state.phase === 'checking'
  return <div className="research-write-feedback"><Alert type="warning" showIcon title={busy ? '正在提交或核对独立验证' : '独立验证提交待核对'} description={state.error || undefined} /><Space wrap><Button disabled={busy} onClick={() => void session.check()}>只读核对验证提交</Button>{state.retryAllowed && <Popconfirm title="同键、同载荷再次提交验证？" onConfirm={() => void session.retry()}><Button>同键重试验证提交</Button></Popconfirm>}</Space></div>
}
export function VerificationWorkspace({ source, mission, active, canStart }: { source: FindingRevision; mission: Mission; active: boolean; canStart: boolean }) {
  const context = useContext(VerificationContext)
  return context ? <VerificationPane key={source.finding_id} {...{ source, mission, active, canStart }} {...context} /> : null
}
function VerificationPane({ source, mission, active, canStart, session, control, maxSteps }: { source: FindingRevision; mission: Mission; active: boolean; canStart: boolean; session: VerificationSession; control: ResearchControlSession; maxSteps: number }) {
  const state = useSyncExternalStore(session.subscribe, session.snapshot)
  const [start, setStart] = useState(false), [steps, setSteps] = useState(Math.min(6, maxSteps)), [selected, setSelected] = useState(''), [cursors, setCursors] = useState([''])
  const [frozen, setFrozen] = useState(source)
  const pane = useRef<HTMLElement | null>(null), returnID = useRef('')
  useLayoutEffect(() => {
    if (!selected && returnID.current) {
      Array.from(pane.current?.querySelectorAll<HTMLElement>('[data-verification-id]') ?? []).find(el => el.dataset.verificationId === returnID.current)?.focus({ preventScroll: true })
      returnID.current = ''
    }
  }, [selected])
  const cursor = cursors.at(-1)!
  const listRead = useCallback((signal: AbortSignal) => fetchVerifications(mission.id, source.finding_id, cursor, signal), [mission.id, source.finding_id, cursor])
  const list = useFindingRead(`${mission.id}:${source.finding_id}:${cursor}:${state.revision}`, active && !selected && !start, listRead, true)
  const modelRead = useCallback(async (signal: AbortSignal) => {
    const v = await requestApi<{ mission_id: string; name: string; model: string; enabled: boolean }>(`/missions/${mission.id}/model`, { signal, safeErrors: true })
    if (v.mission_id !== mission.id || typeof v.name !== 'string' || typeof v.enabled !== 'boolean') throw Error('模型归属不匹配')
    return v
  }, [mission.id])
  const model = useFindingRead(`${mission.id}:verification-model`, active && start, modelRead)
  useEffect(() => { const v = state.last; if (v?.mission === mission.id && v.finding === source.finding_id) { setSelected(v.id); setStart(false) } }, [state.revision, state.last, mission.id, source.finding_id])
  const submitting = state.phase === 'writing', permitted = canStart && !state.pending && maxSteps > 0
  if (!active) return null
  return <section ref={pane} className="finding-stack" aria-label="候选独立验证">
    <div className="workspace-toolbar"><h3>独立验证与结果</h3><Button type="primary" disabled={!permitted} onClick={() => { setFrozen(source); setSteps(Math.min(6, maxSteps)); setStart(true) }}>发起独立验证</Button></div>
    <div className="finding-stack">
      {selected ? <VerificationReader key={selected} mission={mission} finding={source.finding_id} id={selected} active={active && !start} control={control} onBack={() => { returnID.current = selected; setSelected('') }} /> : <><div><Button onClick={list.refresh} loading={list.busy}>刷新验证记录</Button></div><VerificationList items={list.value?.items ?? []} error={list.error} busy={list.busy && !list.value} onOpen={v => setSelected(v.id)} pager={{ previous: cursors.length > 1, next: !!list.value?.next_cursor && !list.error, onPrevious: () => setCursors(v => v.slice(0, -1)), onNext: () => { if (list.value?.next_cursor) setCursors(v => [...v, list.value!.next_cursor]) } }} /></>}
    </div>
    <TaskModal title="独立验证" open={start} onCancel={() => setStart(false)} destroyOnHidden footer={<Space wrap><Button onClick={() => setStart(false)}>关闭</Button><Button type="primary" disabled={!permitted || !model.value?.enabled || !!model.error || !Number.isInteger(steps) || steps < 1 || steps > Math.min(maxSteps, 8)} loading={submitting} onClick={() => void session.start(mission.id, source.finding_id, frozen.id, steps)}>开始独立验证</Button></Space>}>
      <VerificationStartFields number={frozen.number} title={frozen.content.title} sourceAt={frozen.created_at} model={model.value ? `${model.value.name} · ${model.value.model}` : '读取中'} origins={mission.allowed_origins} steps={steps} maximum={Math.min(maxSteps, 8)} onSteps={setSteps} disabled={!!state.pending} />{model.error && <Alert type="error" title={model.error} />}{!state.pending && state.error && <Alert type="error" title={state.error} />}{state.pending && <VerificationPending session={session} />}
    </TaskModal>
  </section>
}
function VerificationReader({ mission, finding, id, active, control, onBack }: { mission: Mission; finding: string; id: string; active: boolean; control: ResearchControlSession; onBack: () => void }) {
  const state = useSyncExternalStore(control.subscribe, control.snapshot)
  const [command, setCommand] = useState<ResearchCommand | null>(null), [completed, setCompleted] = useState(false)
  const heading = useRef<HTMLDivElement | null>(null)
  useEffect(() => { if (active) heading.current?.focus({ preventScroll: true }) }, [active])
  const read = useCallback((signal: AbortSignal) => fetchVerification(mission.id, finding, id, signal), [mission.id, finding, id])
  const detail = useFindingRead(`${id}:${state.revision}`, active && !command, read, !completed)
  const d = detail.value
  useEffect(() => { if (d?.has_result || d?.end_reason) setCompleted(true) }, [d?.has_result, d?.end_reason])
  useEffect(() => { if (!state.pending) setCommand(null) }, [state.revision])
  if (!active) return null
  const disabled = !!state.pending || !d || d.has_result || !!d.end_reason || d.status === 'concluded'
  return <div className="finding-stack"><div className="workspace-toolbar" ref={heading} tabIndex={-1} aria-label="本次独立验证"><Button onClick={onBack}>返回验证记录</Button><Button loading={detail.busy} onClick={detail.refresh}>刷新本次验证</Button></div>{detail.error && <Alert type="error" showIcon title={detail.error} />}
    {d && <><Space wrap><Tag>{d.end_reason === 'budget_exhausted' ? '预算结束 · 未形成结论' : verificationStateLabels[d.status]}</Tag><time>验证依据：{verificationTime(d.source_created_at)} 保存的候选</time><Tag>累计 {d.step_count} / {d.max_steps} 步</Tag>{d.pause_requested && <Tag>已请求暂停</Tag>}</Space>
      {!d.has_result && !d.end_reason && d.status !== 'concluded' && <Space wrap>{(['pause', 'run', 'resume'] as const).map(c => <Button key={c} disabled={disabled || c !== 'pause' && (d.status === 'running' || c === 'run' && d.step_count >= d.max_steps)} onClick={() => setCommand(c)}>{controlLabels[c]}</Button>)}</Space>}
      {state.pending?.thread === d.thread_id && <Alert type="warning" title={state.error || '控制提交待核对'} action={<Button disabled={state.phase === 'writing' || state.phase === 'checking'} onClick={() => void control.check()}>只读核对控制</Button>} />}
      <VerificationEvidenceView key={id} detail={d} mission={mission} active={active && !command} />
      <TaskModal title={command ? controlLabels[command] : '验证控制'} open={!!command} onCancel={() => setCommand(null)} destroyOnHidden footer={<Space><Button onClick={() => setCommand(null)}>关闭</Button><Button type="primary" disabled={disabled} onClick={() => { if (command) void control.control({ mission: mission.id, thread: d.thread_id }, command) }}>确认操作</Button></Space>}><p>{command && controlHelp[command]}</p><p>累计 {d.step_count} / {d.max_steps} 步</p>{state.error && <Alert type="error" title={state.error} />}</TaskModal>
    </>}{!d && <VerificationResultView busy error={detail.error} onEvidence={() => {}} />}
  </div>
}
function VerificationEvidenceView({ detail, mission, active }: { detail: VerificationDetail; mission: Mission; active: boolean }) {
  const [index, setIndex] = useState<number | null>(null), trigger = useRef<HTMLElement | null>(null), restore = useRef(false)
  // 不让状态轮询的对象身份变化重发原件读取；结果不可变，只以固定身份/索引作为读取键。
  const detailRef = useRef(detail); detailRef.current = detail
  const read = useCallback((signal: AbortSignal) => fetchVerificationEvidence(detailRef.current, index ?? -1, signal), [detail.id, index])
  const evidence = useFindingRead(`${detail.id}:${index}`, active && index !== null, read)
  const close = () => { restore.current = true; setIndex(null) }
  useEffect(() => { if (index === null && restore.current) { restore.current = false; if (active && trigger.current?.isConnected) trigger.current.focus({ preventScroll: true }) } }, [index, active])
  const shown = active && index !== null ? evidence.value : null
  return <><VerificationResultView result={detail.result} nextStep={detail.thread.next_step} error={detail.execution_error} onEvidence={i => { trigger.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; setIndex(i) }} />
    {active && index !== null && <div className="finding-stack"><Button onClick={close}>关闭验证材料</Button>{evidence.error && <Alert type="error" showIcon title={evidence.error} action={<Button onClick={evidence.refresh}>重读验证材料</Button>} />}{shown && !shown.matches_snapshot && <Tag color="warning">原件状态已变化，旧结果引用保留</Tag>}{shown?.observation && <><CopyButton text={shown.observation.content} label="复制验证观察" /><pre className="material-text">{shown.observation.content}</pre></>}</div>}
    <ActionEvidenceDrawer action={shown?.action ?? null} mission={mission} thread={shown?.thread} readError={evidence.error} refreshing={evidence.busy} onRefresh={evidence.refresh} onClose={close} />
  </>
}
