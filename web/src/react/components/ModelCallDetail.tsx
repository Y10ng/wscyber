import { useEffect, useState } from 'react'
import { Alert, Button, Descriptions, Drawer, Empty, Skeleton, Space, Tag } from 'antd'
import { CopyButton, GridFold } from '../../ui/SurfaceControls.tsx'
import { EvidenceBytesViewer } from '../../ui/EvidenceBytesViewer.tsx'
import { exactDecimal } from '../../ui/exactDecimal.ts'
import { ApiError, StaleSessionError } from '../api.ts'
import { contentLabels, displayCost, outcomeLabels, priceFieldLabels, priceFields, usageLabels, type ContentPart, type ModelCall, type ModelCallContent } from '../modelUsage.ts'
import { fetchModelCall, fetchModelCallContent } from '../usageApi.ts'
import { formatDateTime } from '../types.ts'
import { ContextAssemblyView, type ModelCallContext } from '../../ui/ContextAssembly.tsx'
import { fetchCallContext } from '../contextApi.ts'

export function usageReadError(error: unknown): string {
  if (error instanceof ApiError) return error.status === 404 ? '未找到该记录，或记录不属于当前项目。' : error.status === 503 ? '服务暂不可用，请稍后重新读取。' : error.status === 403 ? '无权读取该记录。' : error.status === 400 ? '筛选或请求参数无效。' : error.message
  return '读取失败，请检查连接后重新读取。'
}
export function ModelCallDetail({ callID, missionID, timeZone = 'UTC', onClose }: { callID: string | null; missionID?: string; timeZone?: string; onClose: () => void }) {
  return <Drawer title="模型调用详情" placement="right" size="large" open={Boolean(callID)} onClose={onClose} destroyOnHidden>
    {callID && <CallMetadata key={`${missionID ?? ''}:${callID}`} callID={callID} missionID={missionID} timeZone={timeZone} />}
  </Drawer>
}
function CallMetadata({ callID, missionID, timeZone }: { callID: string; missionID?: string; timeZone: string }) {
  const [call, setCall] = useState<ModelCall | null>(null), [error, setError] = useState(''), [busy, setBusy] = useState(true), [reload, setReload] = useState(0), [raw, setRaw] = useState(false), [context, setContext] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    setBusy(true); setError(''); setRaw(false); setContext(false)
    void fetchModelCall(callID, missionID, controller.signal).then(value => { if (!controller.signal.aborted) setCall(value) }).catch(e => {
      if (controller.signal.aborted || e instanceof StaleSessionError || e instanceof ApiError && e.status === 401) return
      if (e instanceof ApiError && [403, 404].includes(e.status)) setCall(null)
      setError(usageReadError(e))
    }).finally(() => { if (!controller.signal.aborted) setBusy(false) })
    return () => controller.abort()
  }, [callID, missionID, reload])
  return <div className="ws-usage-stack">
    <div className="ws-section-row"><code className="ws-inline-code">{callID}</code><Button loading={busy} onClick={() => setReload(n => n + 1)}>刷新详情</Button></div>
    {error && <Alert type="error" showIcon title={call ? `详情未更新：${error}` : error} />}
    {!call ? busy ? <Skeleton active /> : <Empty description="调用详情不可用" /> : <>
      <Space wrap><Tag>{outcomeLabels[call.outcome] ?? '未知状态'}</Tag><Tag>{call.purpose === 'connectivity_test' ? '连通测试' : '研究调用'}</Tag><Tag>{usageLabels[call.usage.status] ?? '未知用量'}</Tag></Space>
      <Descriptions column={1} size="small" items={[
        { key: 'config', label: '模型配置', children: call.source === 'environment' ? '环境模型' : call.config_name || call.model_config_id },
        { key: 'model', label: '模型', children: call.model },
        { key: 'protocol', label: '协议', children: call.protocol === 'openai_chat' ? 'OpenAI Chat' : 'Anthropic Messages' },
        { key: 'mission', label: '所属项目', children: call.mission_id || '无（连通测试）' },
        { key: 'started', label: `开始时间 · ${timeZone}`, children: <time dateTime={call.started_at} title={call.started_at}>{formatDateTime(call.started_at, timeZone)}</time> },
        { key: 'finished', label: `完成时间 · ${timeZone}`, children: call.finished_at ? <time dateTime={call.finished_at} title={call.finished_at}>{formatDateTime(call.finished_at, timeZone)}</time> : '尚无记录' },
        { key: 'duration', label: '耗时', children: call.duration_ms === null ? '未知' : `${call.duration_ms} ms` },
        { key: 'attempted', label: '已尝试发送', children: call.attempted === null ? '未知' : call.attempted ? '是' : '否' },
        { key: 'http', label: 'HTTP 状态', children: call.http_status ?? '未知' },
        { key: 'input', label: '输入 tokens（含缓存）', children: exactDecimal(call.usage.input_tokens) },
        { key: 'output', label: '输出 tokens（含推理）', children: exactDecimal(call.usage.output_tokens) },
        { key: 'total', label: '总 tokens', children: exactDecimal(call.usage.total_tokens) },
        { key: 'cost', label: '文本 tokens 估算费用', children: displayCost(call.cost) },
        { key: 'content', label: '原文记录', children: contentLabels[call.content_state] ?? '未知' },
      ]} />
      <GridFold title="用量子集与价格依据"><div className="ws-usage-stack"><Descriptions column={1} size="small" items={[
        ...([['cache_read_input_tokens', '缓存读（输入子集）'], ['cache_creation_input_tokens', '缓存写（输入子集）'], ['cache_creation_5m_input_tokens', '5 分钟缓存写'], ['cache_creation_1h_input_tokens', '1 小时缓存写'], ['reasoning_output_tokens', '推理（输出子集）']] as const).map(([key, label]) => ({ key, label, children: exactDecimal(call.usage[key]) })),
        { key: 'attempt', label: '执行尝试', children: call.attempt_id || '无' },
        { key: 'thread', label: '研究方向', children: call.thread_id || '无' },
        { key: 'test', label: '连通测试', children: call.test_id || '无' },
      ]} />
      {call.cost.price_snapshot ? <Descriptions title={`本次单价 · ${call.cost.price_snapshot.currency} / 百万 tokens`} column={1} size="small" items={priceFields.map(key => ({ key, label: priceFieldLabels[key], children: exactDecimal(call.cost.price_snapshot![key]) }))} /> : <Tag>本次未匹配价格</Tag>}
      <pre className="ws-code-pre" aria-label="供应商已校验计数">{JSON.stringify(call.usage.provider_usage, null, 2)}</pre>
      <CopyButton text={call.id} label="复制调用 ID" /></div></GridFold>
      {(call.purpose === 'research' || call.purpose === 'verification') && <><Button aria-expanded={context} onClick={() => setContext(v => !v)}>{context ? '收起上下文装配' : '查看上下文装配'}</Button>{context && <CallContext key={call.id} call={call} />}</>}
      {call.record_model_content ? <>
        <Button onClick={() => setRaw(v => !v)} aria-expanded={raw}>{raw ? '收起模型输入与返回' : '查看模型输入与返回'}</Button>
        {raw && <CallRawContent key={call.id} callID={call.id} missionID={missionID} />}
      </> : <Tag>未启用记录</Tag>}
    </>}
  </div>
}
function CallContext({ call }: { call: ModelCall }) {
  const [value, setValue] = useState<ModelCallContext | null>(null), [error, setError] = useState(''), [page, setPage] = useState(0), [reload, setReload] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setValue(null); setError(''); setPage(0)
    void fetchCallContext(call, controller.signal).then(v => { if (!controller.signal.aborted) setValue(v) }).catch(e => {
      if (!controller.signal.aborted && !(e instanceof StaleSessionError) && !(e instanceof ApiError && e.status === 401)) setError(usageReadError(e))
    })
    return () => controller.abort()
  }, [call.id, call.mission_id, call.thread_id, call.attempt_id, reload])
  return <><ContextAssemblyView value={value} attempted={call.attempted} page={page} onPage={setPage} error={error} />{error && <Button onClick={() => setReload(n => n + 1)}>重新读取装配记录</Button>}</>
}
function CallRawContent({ callID, missionID }: { callID: string; missionID?: string }) {
  const [content, setContent] = useState<ModelCallContent | null>(null), [error, setError] = useState(''), [reload, setReload] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setContent(null); setError('')
    void fetchModelCallContent(callID, missionID, controller.signal).then(value => { if (!controller.signal.aborted) setContent(value) }).catch(e => {
      if (!controller.signal.aborted && !(e instanceof StaleSessionError) && !(e instanceof ApiError && e.status === 401)) setError(usageReadError(e))
    })
    return () => controller.abort()
  }, [callID, missionID, reload])
  return error ? <Alert type="error" showIcon title={error} action={<Button onClick={() => setReload(n => n + 1)}>重新读取原文</Button>} /> : !content ? <Skeleton active /> : !content.recorded ? <Tag>未启用记录</Tag> : <div className="ws-usage-stack">
    <RawPart part={content.input} label="实际模型输入" sourceKey={`${callID}:input`} />
    <RawPart part={content.response} label="模型返回" sourceKey={`${callID}:response`} />
  </div>
}
function RawPart({ part, label, sourceKey }: { part: ContentPart | null; label: string; sourceKey: string }) {
  if (!part || part.state !== 'available' || part.body_base64 === null) return <div><strong>{label}</strong> <Tag>{part?.state === 'pending' ? '尚在执行' : '内容缺失'}</Tag></div>
  return <section className="ws-usage-stack">
    <Space wrap><Tag>{part.media_type || '未提供媒体类型'}</Tag>{!part.complete && <Tag color="warning">捕获已截断或不完整</Tag>}{part.redacted && <Tag color="warning">已移除已知连接凭据（非未改原件）</Tag>}</Space>
    <EvidenceBytesViewer sourceKey={sourceKey} base64Value={part.body_base64} label={label} filename={`${sourceKey.replace(':', '-')}.bin`} />
  </section>
}
