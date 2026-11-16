import { useEffect, useState } from 'react'
import { Alert, Button, Descriptions, Space, Spin, Tabs } from 'antd'
import { MaterialText } from '../../ui/MaterialLibrary.tsx'
import { SkillActivityDetail, SkillActivityList, SkillCallFacts, SkillUsageList, type SkillActivityRow } from '../../ui/SkillRecords.tsx'
import { ApiError, StaleSessionError } from '../api.ts'
import { outcomeLabels } from '../modelUsage.ts'
import { fetchSkillActivities, fetchSkillCalls, fetchSkillLoad, fetchSkillUsage, type SkillLoad } from '../skillsApi.ts'

const readError = (reason: unknown) => reason instanceof StaleSessionError || reason instanceof ApiError && reason.status === 401 ? '' : reason instanceof Error ? reason.message : '读取Skill记录失败。'
// 每页替换，不无限累积正文或项目历史；身份改变立即遮蔽上一页。
function useSkillPage<T extends { next_cursor: string }>(scope: string, read: (after: string, signal: AbortSignal) => Promise<T>) {
  const [cursors, setCursors] = useState(['']), [reload, setReload] = useState(0)
  const after = cursors[cursors.length - 1], identity = `${scope}:${after}:${reload}`
  const [state, setState] = useState<{ identity: string; data?: T; error: string; busy: boolean }>({ identity: '', error: '', busy: true })
  useEffect(() => {
    const controller = new AbortController(); setState({ identity, busy: true, error: '' })
    void read(after, controller.signal).then(data => { if (!controller.signal.aborted) setState({ identity, data, busy: false, error: '' }) }).catch(reason => { if (!controller.signal.aborted) setState({ identity, busy: false, error: readError(reason) }) })
    return () => controller.abort()
    // read闭包只依赖scope；调用方必须在scope包含全部请求身份。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [identity])
  const current = state.identity === identity ? state : { busy: true, error: '', data: undefined }
  return { ...current, refresh: () => { setCursors(['']); setReload(value => value + 1) }, pager: {
    previous: cursors.length > 1, next: Boolean(current.data?.next_cursor),
    onPrevious: () => setCursors(values => values.length > 1 ? values.slice(0, -1) : values),
    onNext: () => { if (current.data?.next_cursor) setCursors(values => [...values, current.data!.next_cursor]) },
  } }
}
function SkillTotals({ missionID }: { missionID: string }) {
  const page = useSkillPage(missionID, (after, signal) => fetchSkillUsage(missionID, after, signal))
  return <><Button onClick={page.refresh} loading={page.busy}>刷新加载记录</Button><SkillUsageList items={page.data?.items ?? []} total={page.data?.total_successful_loads} busy={page.busy} error={page.error} pager={page.pager} /></>
}
function SkillActivities({ missionID, onOpen }: { missionID: string; onOpen: (row: SkillActivityRow) => void }) {
  const page = useSkillPage(missionID, (after, signal) => fetchSkillActivities(missionID, after, signal))
  return <><Button onClick={page.refresh} loading={page.busy}>刷新活动记录</Button><SkillActivityList items={page.data?.items ?? []} busy={page.busy} error={page.error} pager={page.pager} onOpen={onOpen} /></>
}
function SkillLoadedDetail({ missionID, activity }: { missionID: string; activity: SkillActivityRow }) {
  const loadID = activity.result.load_id!
  const [detail, setDetail] = useState<SkillLoad | null>(null), [error, setError] = useState(''), [busy, setBusy] = useState(true), [reload, setReload] = useState(0)
  const calls = useSkillPage(`${missionID}:${loadID}`, (after, signal) => fetchSkillCalls(missionID, loadID, after, signal))
  useEffect(() => {
    const controller = new AbortController(); setDetail(null); setError(''); setBusy(true)
    void fetchSkillLoad(missionID, loadID, controller.signal).then(value => {
      if (controller.signal.aborted) return
      if (value.attempt_id !== activity.attempt_id || value.thread_id !== activity.thread_id || value.id !== activity.request.item_id || value.source_token !== activity.request.source_token) throw new Error('固定原件与当前活动身份不一致。')
      setDetail(value)
    }).catch(reason => { if (!controller.signal.aborted) setError(readError(reason)) }).finally(() => { if (!controller.signal.aborted) setBusy(false) })
    return () => controller.abort()
  }, [missionID, loadID, activity, reload])
  return <>{busy && <Spin aria-label="读取固定Skill原件" />}{error && <Alert type="error" showIcon title={error} action={<Button onClick={() => setReload(value => value + 1)}>重新读取</Button>} />}
    {detail && <><Descriptions size="small" column={1} items={[{ key: 'path', label: '固定来源', children: detail.path }, { key: 'token', label: '来源身份', children: detail.source_token }]} /><MaterialText detail={detail} /></>}
    <Space wrap><Button onClick={calls.refresh} loading={calls.busy}>刷新调用关联</Button></Space><SkillCallFacts items={(calls.data?.items ?? []).map(call => ({ ...call, outcome: outcomeLabels[call.outcome] || '未知结果' }))} busy={calls.busy} error={calls.error} pager={calls.pager} />
  </>
}
export function MissionSkillUsage({ missionID }: { missionID: string }) {
  const [tab, setTab] = useState('totals'), [selected, setSelected] = useState<SkillActivityRow | null>(null)
  return selected ? <SkillActivityDetail activity={selected} onClose={() => setSelected(null)}>{selected.result.status === 'loaded' && selected.result.load_id && <SkillLoadedDetail key={selected.attempt_id} missionID={missionID} activity={selected} />}</SkillActivityDetail> : <Tabs activeKey={tab} onChange={setTab} destroyOnHidden items={[
    { key: 'totals', label: '按Skill统计', children: <SkillTotals missionID={missionID} /> },
    { key: 'activities', label: '活动明细', children: <SkillActivities missionID={missionID} onOpen={setSelected} /> },
  ]} />
}
