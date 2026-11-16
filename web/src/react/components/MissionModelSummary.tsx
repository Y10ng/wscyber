import { useEffect, useState } from 'react'
import { Alert, Button, Space, Spin, Tag } from 'antd'
import { ApiError, StaleSessionError } from '../api.ts'
import { fetchMissionModel } from '../modelApi.ts'
import type { MissionModel } from '../models.ts'

export function MissionModelSummary({ missionID, refreshToken }: { missionID: string; refreshToken?: unknown }) {
  const [value, setValue] = useState<MissionModel | null>(null)
  const [error, setError] = useState(''), [loading, setLoading] = useState(false), [tick, setTick] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    void fetchMissionModel(missionID, controller.signal).then(model => {
      if (!controller.signal.aborted) { setValue(model); setError('') }
    }).catch(error => {
      if (controller.signal.aborted || error instanceof StaleSessionError || (error instanceof ApiError && error.status === 401)) return
      setValue(null)
      setError(error instanceof ApiError && error.status === 404 ? '此项目暂无可读取的模型绑定' : error instanceof ApiError && error.status === 503 ? '任务模型当前不可用' : '任务模型读取失败')
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [missionID, refreshToken, tick])
  const model = value?.mission_id === missionID ? value : null
  return <div className="ws-mission-model" aria-label="实际研究模型">
    {model ? <Space wrap><span>研究模型</span><strong>{model.name || model.model}</strong>{model.name && <span>{model.model}</span>}<Tag>{model.source === 'managed' ? '任务绑定' : '环境配置'}</Tag>{!model.enabled && <Tag color="warning">已禁用</Tag>}</Space> : loading ? <Spin size="small" /> : null}
    {error && <Alert type="warning" title={error} action={<Button loading={loading} onClick={() => setTick(n => n + 1)}>重新读取</Button>} />}
  </div>
}
