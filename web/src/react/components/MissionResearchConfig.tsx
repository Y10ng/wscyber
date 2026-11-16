import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Descriptions, Empty, Spin, Tag, Tabs } from 'antd'
import { MissionSkillUsage } from './MissionSkillUsage.tsx'
import { TaskModal } from '../../ui/TaskModal.tsx'
import { MaterialText } from '../../ui/MaterialLibrary.tsx'
import { ResearchBriefSummary } from '../../ui/ResearchBrief.tsx'
import { ApiError, StaleSessionError } from '../api.ts'
import { fetchMissionMaterial, fetchResearchConfig, type MaterialReference, type ResearchConfig } from '../libraryApi.ts'
export function MissionResearchConfig({ missionID }: { missionID: string }) {
  const [open, setOpen] = useState(false), [config, setConfig] = useState<ResearchConfig | null>(null), [loading, setLoading] = useState(false), [error, setError] = useState(''), [refresh, setRefresh] = useState(0)
  const [detail, setDetail] = useState<(MaterialReference & { content: string }) | null>(null), [reading, setReading] = useState(false), [readError, setReadError] = useState('')
  const ticket = useRef(0)
  useEffect(() => {
    ticket.current++; setDetail(null); setReading(false); setReadError(''); setConfig(null); setError('')
    if (!open) return
    const controller = new AbortController(); setLoading(true)
    void fetchResearchConfig(missionID, controller.signal).then(value => { if (!controller.signal.aborted) { if (value.mission_id !== missionID) throw new Error('配置不属于当前任务。'); setConfig(value) } }).catch(reason => { if (!controller.signal.aborted && !(reason instanceof StaleSessionError) && !(reason instanceof ApiError && reason.status === 401)) setError(reason instanceof Error ? reason.message : '读取任务配置失败。') }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => { controller.abort(); ticket.current++ }
  }, [missionID, open, refresh])
  const read = async (id: string) => {
    const current = ++ticket.current; setDetail(null); setReadError(''); setReading(true)
    try { const value = await fetchMissionMaterial(missionID, id); if (current === ticket.current) { if (value.id !== id || !config?.materials.some(item => item.id === id && item.content_sha256 === value.content_sha256)) throw new Error('资料不属于当前任务配置。'); setDetail(value) } }
    catch (reason) { if (current === ticket.current && !(reason instanceof StaleSessionError) && !(reason instanceof ApiError && reason.status === 401)) setReadError(reason instanceof Error ? reason.message : '读取固定资料失败。') }
    finally { if (current === ticket.current) setReading(false) }
  }
  return <><Button onClick={() => setOpen(true)}>研究配置</Button><TaskModal title="任务研究配置" open={open} onCancel={() => setOpen(false)} destroyOnHidden footer={<Button onClick={() => setOpen(false)}>关闭</Button>}>
    {open && <Tabs destroyOnHidden items={[{ key: 'materials', label: '任务资料与安全', children: <>
    {loading && <Spin aria-label="读取任务研究配置" />}{error && <Alert type="error" title={error} action={<Button onClick={() => setRefresh(value => value + 1)}>重新读取</Button>} />}
    {config?.mission_id === missionID && <div className="material-list-pane"><ResearchBriefSummary value={config.research_brief} /><h3>任务资料</h3>{!config.materials.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未选用资料" />}{config.materials.map(item => <div className="material-row" key={item.id}><span className="material-row-text"><strong>{item.name}</strong><span>{item.path}</span>{item.description && <span>{item.description}</span>}</span><span>{item.byte_size.toLocaleString('zh-CN')} 字节</span><Button onClick={() => void read(item.id)}>查看 {item.name}</Button></div>)}
      {reading && <Spin aria-label="读取任务固定正文" />}{readError && <Alert type="error" title={readError} />}{detail && <MaterialText detail={detail} actions={<Button onClick={() => { ticket.current++; setDetail(null) }}>关闭正文</Button>} />}
      <h3>测试安全</h3><Descriptions size="small" column={1} items={[{ key: 'enabled', label: '附加安全提示词', children: <Tag>{config.safety_prompt_enabled ? '已开启' : '已关闭'}</Tag> }]} />{config.safety_prompt_enabled && <pre className="material-text">{config.safety_prompt}</pre>}
    </div>}
    </> }, { key: 'skills', label: 'Skill使用', children: <MissionSkillUsage key={missionID} missionID={missionID} /> }]} />}
  </TaskModal></>
}
