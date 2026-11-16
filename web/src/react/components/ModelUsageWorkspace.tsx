import { useEffect, useId, useRef, useState } from 'react'
import { Alert, Button, Card, Descriptions, Empty, Form, Input, Select, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { DailyUsageChart } from '../../ui/DailyUsageChart.tsx'
import { exactDecimal } from '../../ui/exactDecimal.ts'
import { ApiError, StaleSessionError } from '../api.ts'
import { fetchModelConfigs } from '../modelApi.ts'
import type { ModelConfig } from '../models.ts'
import { formatDateTime } from '../types.ts'
import { displayCost, emptyUsageFilters, filterError, outcomeLabels, recentCalendarRange, usageLabels, usageQuery, type ModelCall, type ModelCallPage, type UsageFilters, type UsageResponse } from '../modelUsage.ts'
import { fetchModelCalls, fetchModelRecording, fetchModelUsage } from '../usageApi.ts'
import { ModelCallDetail, usageReadError } from './ModelCallDetail.tsx'

interface Props { missionID?: string; active?: boolean; selectedCallID?: string | null; onSelectCall?: (id: string | null) => void; onObservedCalls?: (calls: ModelCall[]) => void }
export function ModelUsageWorkspace({ missionID, active = true, ...props }: Props) {
  return <UsageWorkspace key={missionID ?? 'global'} missionID={missionID} active={active} {...props} />
}
function UsageWorkspace({ missionID, active, selectedCallID, onSelectCall, onObservedCalls }: Props) {
  const [draft, setDraft] = useState<UsageFilters>({ ...emptyUsageFilters }), [filters, setFilters] = useState<UsageFilters>({ ...emptyUsageFilters }), [validation, setValidation] = useState('')
  const [usage, setUsage] = useState<UsageResponse | null>(null), [page, setPage] = useState<ModelCallPage | null>(null)
  const [usageError, setUsageError] = useState(''), [pageError, setPageError] = useState(''), [busy, setBusy] = useState(true), [listBusy, setListBusy] = useState(true)
  const [refresh, setRefresh] = useState(0), [cursors, setCursors] = useState<string[]>(['']), [loadedPage, setLoadedPage] = useState(1)
  const [configs, setConfigs] = useState<ModelConfig[]>([]), [localCall, setLocalCall] = useState<string | null>(null)
  const [recording, setRecording] = useState<boolean | null>(null), [recordingError, setRecordingError] = useState('')
  const [preset, setPreset] = useState('default')
  const timeZonesID = useId()
  const listRange = useRef<ModelCallPage['range'] | null>(null)
  const query = usageQuery(filters, missionID)
  const cursor = cursors[cursors.length - 1]
  const selectCall = (id: string | null) => { setLocalCall(id); onSelectCall?.(id) }
  const visibleCall = active ? selectedCallID === undefined ? localCall : selectedCallID : null
  useEffect(() => {
    if (!active) { setLocalCall(null); return }
    const controller = new AbortController()
    void fetchModelConfigs(controller.signal).then(rows => { if (!controller.signal.aborted) setConfigs(rows) }).catch(() => { /* 环境模型仍可读账本；管理不可用不阻断用量。 */ })
    if (missionID) {
      setRecordingError('')
      void fetchModelRecording(missionID, controller.signal).then(value => { if (!controller.signal.aborted) setRecording(value) }).catch(e => {
        if (!controller.signal.aborted && !(e instanceof StaleSessionError) && !(e instanceof ApiError && e.status === 401)) setRecordingError(usageReadError(e))
      })
    }
    return () => controller.abort()
  }, [active, missionID, refresh])
  useEffect(() => {
    if (!active) return
    const controller = new AbortController()
    setBusy(true); setUsageError('')
    void fetchModelUsage(query, missionID, controller.signal).then(value => { if (!controller.signal.aborted) setUsage(value) }).catch(e => {
      if (controller.signal.aborted || e instanceof StaleSessionError || e instanceof ApiError && e.status === 401) return
      if (e instanceof ApiError && [403, 404].includes(e.status)) setUsage(null)
      setUsageError(usageReadError(e))
    }).finally(() => { if (!controller.signal.aborted) setBusy(false) })
    return () => controller.abort()
  }, [active, query, missionID, refresh])
  useEffect(() => {
    if (!active) return
    const controller = new AbortController()
    setListBusy(true); setPageError('')
    const listQuery = cursor && listRange.current ? usageQuery({ ...filters, ...listRange.current }, missionID) : query
    void fetchModelCalls(`${listQuery}&limit=25${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`, missionID, controller.signal).then(value => {
      if (!controller.signal.aborted) { listRange.current = value.range; setPage(value); setLoadedPage(cursors.length); onObservedCalls?.(value.items) }
    }).catch(e => {
      if (controller.signal.aborted || e instanceof StaleSessionError || e instanceof ApiError && e.status === 401) return
      if (e instanceof ApiError && [403, 404].includes(e.status)) setPage(null)
      setPageError(usageReadError(e))
    }).finally(() => { if (!controller.signal.aborted) setListBusy(false) })
    return () => controller.abort()
  }, [active, query, cursor, missionID, refresh])
  const apply = () => {
    const error = filterError(draft); setValidation(error)
    if (error) return
    selectCall(null); listRange.current = null; setUsage(null); setPage(null); setFilters({ ...draft }); setCursors(['']); setRefresh(n => n + 1)
  }
  const change = <K extends keyof UsageFilters>(key: K, value: UsageFilters[K]) => {
    if (key === 'from' || key === 'to') setPreset('custom')
    setDraft(d => {
      const next = { ...d, [key]: value }
      if (key === 'time_zone' && /^\d+$/.test(preset)) {
        try { Object.assign(next, recentCalendarRange(Number(preset), value)) } catch { /* 校验由查询处理。 */ }
      }
      return next
    })
    setValidation('')
  }
  const summary = usage?.summary
  return <section className="ws-usage-stack" aria-label={missionID ? '项目模型调用' : '全局模型用量'}>
    <Form layout="vertical" className="ws-usage-filters" onFinish={apply}>
      <Form.Item label="日期范围"><Select aria-label="日期范围快捷选择" value={preset} onChange={value => {
        setPreset(value)
        if (value === 'default') setDraft(d => ({ ...d, from: '', to: '' }))
        else if (value !== 'custom') { try { setDraft(d => ({ ...d, ...recentCalendarRange(Number(value), d.time_zone) })) } catch { setValidation('请先选择有效时区。') } }
      }} options={[{ value: 'default', label: missionID ? '任务全历史汇总 / 近 7 日日图' : '最近 7 个日历日' }, { value: '1', label: '今天' }, { value: '7', label: '最近 7 日' }, { value: '30', label: '最近 30 日' }, { value: '90', label: '最近 90 日' }, { value: 'custom', label: '自定义' }]} /></Form.Item>
      <Form.Item label="开始日期"><Input aria-label="开始日期" type="date" value={draft.from} onChange={e => change('from', e.target.value)} /></Form.Item>
      <Form.Item label="结束日期（不含）"><Input aria-label="结束日期（不含）" type="date" value={draft.to} onChange={e => change('to', e.target.value)} /></Form.Item>
      <Form.Item label="时区"><Input aria-label="时区" list={timeZonesID} value={draft.time_zone} onChange={e => change('time_zone', e.target.value)} /><datalist id={timeZonesID}><option value="UTC" /><option value="Asia/Shanghai" /><option value="America/New_York" /><option value="Europe/Berlin" /></datalist></Form.Item>
      <Form.Item label="模型配置"><Select aria-label="筛选模型配置" value={draft.model_config_id} showSearch optionFilterProp="label" onChange={value => change('model_config_id', value)} options={[{ value: '', label: '全部模型' }, ...configs.map(c => ({ value: c.id, label: `${c.name} · ${c.model}` }))]} /></Form.Item>
      <Form.Item label="用途"><Select aria-label="调用用途" value={draft.purpose} onChange={value => change('purpose', value)} options={[{ value: '', label: '全部用途' }, { value: 'research', label: '研究调用' }, { value: 'verification', label: '独立验证' }, { value: 'connectivity_test', label: '连通测试' }]} /></Form.Item>
      <Form.Item label="结果"><Select aria-label="调用结果" value={draft.outcome} onChange={value => change('outcome', value)} options={[{ value: '', label: '全部结果' }, ...Object.entries(outcomeLabels).map(([value, label]) => ({ value, label }))]} /></Form.Item>
      <Button htmlType="submit" type="primary" loading={busy || listBusy}>查询</Button>
    </Form>
    {validation && <Alert type="error" showIcon title={validation} />}
    {missionID && <Space wrap><Tag>模型原文：{recording === null ? '未读取' : recording ? '已开启' : '未启用记录'}</Tag>{recordingError && <Typography.Text type="danger">{recordingError}</Typography.Text>}</Space>}
    {usageError && <Alert type="error" showIcon title={`${usage ? '汇总未更新：' : ''}${usageError}`} action={<Button onClick={() => setRefresh(n => n + 1)}>重新读取</Button>} />}
    {!usage ? busy ? <Skeleton active paragraph={{ rows: 3 }} /> : null : <>
      <Card size="small" title={usage.summary_scope === 'all_time' ? '任务全历史 · 已知用量' : '所选日范围 · 已知用量'} loading={false} className={busy ? 'ws-usage-loading' : ''}>
        <Descriptions column={{ xs: 1, sm: 2, md: 3, lg: 4, xl: 4 }} size="small" items={[
          { key: 'calls', label: '调用数', children: exactDecimal(summary!.calls) },
          { key: 'input', label: '输入 tokens', children: exactDecimal(summary!.input_tokens) },
          { key: 'output', label: '输出 tokens', children: exactDecimal(summary!.output_tokens) },
          { key: 'total', label: '总 tokens', children: exactDecimal(summary!.total_tokens) },
          { key: 'running', label: '执行中 / 中断', children: `${exactDecimal(summary!.running_calls)} / ${exactDecimal(summary!.interrupted_calls)}` },
          { key: 'missing', label: '用量缺口 / 其中无效', children: `${exactDecimal(summary!.missing_usage_calls)} / ${exactDecimal(summary!.invalid_usage_calls)}` },
          { key: 'unpriced', label: '无完整估算（调用）', children: exactDecimal(summary!.unpriced_calls) },
          { key: 'cost', label: '文本 tokens 估算', children: summary!.estimated_costs.length ? <Space orientation="vertical" size={2}>{summary!.estimated_costs.map(c => <span key={c.currency}>{c.currency} {exactDecimal(c.amount)} · {exactDecimal(c.priced_calls)} 次</span>)}</Space> : '暂无可计算费用' },
        ]} />
      </Card>
      <div className="ws-section-row"><span>日图范围：{usage.range.from} 至 {usage.range.to}（不含） · {usage.range.time_zone}</span><Tag title={usage.coverage_started_at}>账本起始 {formatDateTime(usage.coverage_started_at, usage.range.time_zone)}</Tag></div>
      <DailyUsageChart busy={busy} rows={usage.daily.map(d => ({ date: d.date, calls: d.calls, input: d.input_tokens, output: d.output_tokens, missingCalls: d.missing_usage_calls }))} />
    </>}
    <Card size="small" title="调用记录" extra={<Button loading={listBusy} onClick={() => { selectCall(null); setCursors(['']); setRefresh(n => n + 1) }}>刷新记录</Button>}>
      {pageError && <Alert type="error" showIcon title={`${page ? '列表未更新，保留上次读取：' : ''}${pageError}`} />}
      {page && <div className="ws-section-row"><span>{page.range.from} 至 {page.range.to}（不含） · {page.range.time_zone}</span><span>第 {loadedPage} 页</span></div>}
      {!page && listBusy ? <Skeleton active /> : <Table<ModelCall> size="small" rowKey="id" loading={listBusy} dataSource={page?.items ?? []} pagination={false} scroll={{ x: 1100 }} locale={{ emptyText: <Empty description={pageError ? '记录暂不可用' : '此范围暂无调用记录'} /> }} columns={[
        { title: `开始时间（${page?.range.time_zone ?? filters.time_zone}）`, dataIndex: 'started_at', width: 200, render: value => <time dateTime={value} title={value}>{formatDateTime(value, page?.range.time_zone ?? filters.time_zone)}</time> },
        { title: '模型', width: 230, render: (_, call) => <div className="ws-usage-model"><strong>{call.config_name || (call.source === 'environment' ? '环境模型' : call.model_config_id)}</strong><span>{call.model}</span></div> },
        { title: '用途 / 结果', width: 190, render: (_, call) => <Space wrap size={4}><Tag>{call.purpose === 'connectivity_test' ? '连通测试' : call.purpose === 'verification' ? '独立验证' : '研究调用'}</Tag><span>{outcomeLabels[call.outcome]}</span></Space> },
        { title: '输入 / 输出 tokens', width: 190, render: (_, call) => <div>{exactDecimal(call.usage.input_tokens)} / {exactDecimal(call.usage.output_tokens)}<div>{usageLabels[call.usage.status]}</div></div> },
        { title: '估算费用', width: 180, render: (_, call) => displayCost(call.cost) },
        { title: '操作', width: 100, render: (_, call) => <Button onClick={() => selectCall(call.id)} aria-label={`查看调用 ${call.id}`}>查看详情</Button> },
      ]} />}
      <div className="ws-usage-pagination"><Button disabled={listBusy || cursors.length < 2} onClick={() => { selectCall(null); setCursors(c => c.slice(0, -1)) }}>上一页</Button><Button disabled={listBusy || Boolean(pageError) || !page?.next_cursor} onClick={() => { selectCall(null); if (page?.next_cursor) setCursors(c => [...c, page.next_cursor!]) }}>下一页</Button></div>
    </Card>
    <ModelCallDetail callID={visibleCall ?? null} missionID={missionID} timeZone={page?.range.time_zone ?? filters.time_zone} onClose={() => selectCall(null)} />
  </section>
}
