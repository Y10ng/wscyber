import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Form, Input, Select, Space, Switch, Table, Tag } from 'antd'
import { TaskModal } from '../../ui/TaskModal.tsx'
import { GridFold } from '../../ui/SurfaceControls.tsx'
import { exactDecimal } from '../../ui/exactDecimal.ts'
import { ApiError, StaleSessionError, getSessionEpoch } from '../api.ts'
import { generateOperationKey } from '../idempotency.ts'
import { fetchModelConfigs } from '../modelApi.ts'
import type { ModelConfig } from '../models.ts'
import { knownPriceTargets, matchesPriceWrite, priceFieldLabels, priceFields, priceKey, validPrice, type ModelCall, type ModelPrice, type PriceDraft, type PriceEditSession, type PriceWrite } from '../modelUsage.ts'
import { fetchModelCalls, fetchModelPrices, putModelPrice } from '../usageApi.ts'
import { usageReadError } from './ModelCallDetail.tsx'

function draftForPrice(price?: ModelPrice): PriceDraft {
  return { id: price?.id ?? generateOperationKey(), original: price ?? null, target: price ? { source: price.source, model_config_id: price.model_config_id, protocol: price.protocol, model: price.model } : null, currency: price?.currency ?? 'USD', enabled: price?.enabled ?? true, input_per_million: price?.input_per_million ?? '', output_per_million: price?.output_per_million ?? '', cache_read_per_million: price?.cache_read_per_million ?? '', cache_write_5m_per_million: price?.cache_write_5m_per_million ?? '', cache_write_1h_per_million: price?.cache_write_1h_per_million ?? '' }
}
export function ModelPricesView({ active, session }: { active: boolean; session: PriceEditSession }) {
  const [prices, setPrices] = useState<ModelPrice[]>([]), [configs, setConfigs] = useState<ModelConfig[]>([]), [calls, setCalls] = useState<ModelCall[]>([])
  const [draft, setDraft] = useState<PriceDraft | null>(session.draft), [open, setOpen] = useState(false), [busy, setBusy] = useState(false), [reading, setReading] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState(''), [reload, setReload] = useState(0)
  const [checked, setChecked] = useState<{ current: ModelPrice | null; conflict: boolean } | null>(null)
  const [fields, setFields] = useState<Record<string, string>>({})
  const [cacheOpen, setCacheOpen] = useState(false)
  useEffect(() => {
    const first = Object.keys(fields)[0]
    if (first) document.getElementById(`price-${first}`)?.focus()
  }, [fields, cacheOpen])
  const generation = useRef(0), live = useRef(true)
  useEffect(() => { live.current = true; return () => { live.current = false; generation.current++ } }, [])
  const updateDraft = (next: PriceDraft | null) => { session.draft = next; setDraft(next); setFields({}) }
  const targets = knownPriceTargets(configs, calls, prices)
  useEffect(() => {
    if (!active) { setOpen(false); setBusy(false); setReading(false); setChecked(null); generation.current++; return }
    const controller = new AbortController()
    setReading(true); setError('')
    void fetchModelPrices(controller.signal).then(result => { if (!controller.signal.aborted) setPrices(result.items) }).catch(e => { if (!controller.signal.aborted && !(e instanceof StaleSessionError) && !(e instanceof ApiError && e.status === 401)) setError(usageReadError(e)) }).finally(() => { if (!controller.signal.aborted) setReading(false) })
    void fetchModelConfigs(controller.signal).then(result => { if (!controller.signal.aborted) setConfigs(result) }).catch(() => {})
    void fetchModelCalls('limit=100', undefined, controller.signal).then(result => { if (!controller.signal.aborted) setCalls(result.items) }).catch(() => {})
    return () => controller.abort()
  }, [active, reload])
  useEffect(() => {
    if (!draft && !session.pending) return
    const before = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = '' }
    window.addEventListener('beforeunload', before)
    return () => window.removeEventListener('beforeunload', before)
  }, [draft, session])
  const reconcile = async () => {
    const pending = session.pending
    if (!pending || busy) return
    const gen = ++generation.current, epoch = getSessionEpoch()
    setBusy(true); setError(''); setChecked(null)
    try {
      const result = await fetchModelPrices()
      if (!live.current || gen !== generation.current || epoch !== getSessionEpoch()) return
      setPrices(result.items)
      const current = result.items.find(p => p.id === pending.id) ?? null
      if (current && matchesPriceWrite(current, pending.body)) {
        session.pending = null; updateDraft(null); setOpen(false); setNotice('已核对：价格已保存，后续调用使用新价格。')
      } else {
        setChecked({ current, conflict: result.items.some(p => p.id !== pending.id && priceKey(p) === priceKey(pending.body)) })
        setNotice(current ? '已读取当前价格，与本次提交不同；尚未再次保存。' : '当前未查到本次价格记录；这不证明原请求未在途。')
      }
    } catch (e) { if (live.current && gen === generation.current && !(e instanceof StaleSessionError) && !(e instanceof ApiError && e.status === 401)) setError(usageReadError(e)) }
    finally { if (live.current && gen === generation.current) setBusy(false) }
  }
  const save = async () => {
    if (!draft || busy || session.pending) return
    const errors: Record<string, string> = {}
    if (!draft.target) errors.target = '请选择已配置或实际观察到的模型。'
    for (const field of priceFields) if ((field === 'input_per_million' || field === 'output_per_million' || draft[field] !== '') && !validPrice(draft[field])) errors[field] = '填写 0–1,000,000，最多 6 位小数；留空不代表免费。'
    if (Object.keys(errors).length) {
      if (priceFields.slice(2).some(field => errors[field])) setCacheOpen(true)
      setFields(errors); return
    }
    const body: PriceWrite = { ...draft.target!, expected_revision: draft.original?.revision ?? 0, enabled: draft.enabled, currency: draft.currency, input_per_million: draft.input_per_million, output_per_million: draft.output_per_million, cache_read_per_million: draft.cache_read_per_million || null, cache_write_5m_per_million: draft.cache_write_5m_per_million || null, cache_write_1h_per_million: draft.cache_write_1h_per_million || null }
    session.pending = { id: draft.id, body }
    setBusy(true); setError(''); setNotice(''); setChecked(null)
    const gen = ++generation.current, epoch = getSessionEpoch()
    try {
      const price = await putModelPrice(draft.id, body)
      if (!live.current || gen !== generation.current || epoch !== getSessionEpoch()) return
      if (price.id !== draft.id || !matchesPriceWrite(price, body)) throw new Error('回执身份不一致')
      session.pending = null; updateDraft(null); setOpen(false); setNotice('价格已保存，后续调用使用新价格。'); setReload(n => n + 1)
    } catch (e) {
      if (!live.current || gen !== generation.current || e instanceof StaleSessionError || e instanceof ApiError && e.status === 401) return
      if (e instanceof ApiError && e.status === 400) { session.pending = null; setError('价格输入未被接受，请检查字段。') }
      else setError(e instanceof ApiError && e.status === 409 ? '价格已发生变化，请先核对当前保存结果。' : '保存结果待核对；保留原记录身份，不自动再次保存。')
    } finally { if (live.current && gen === generation.current) setBusy(false) }
  }
  const begin = (price?: ModelPrice) => {
    const next = session.draft ?? draftForPrice(price)
    setCacheOpen(priceFields.slice(2).some(field => Boolean(next[field])))
    if (session.draft) { setOpen(true); return }
    updateDraft(next); setNotice(''); setError(''); setChecked(null); setOpen(true)
  }
  const locked = busy || Boolean(session.pending)
  return <section className="ws-usage-stack" hidden={!active}>
    <div className="ws-section-row"><h2>模型价格</h2><Space wrap><Button loading={reading} onClick={() => setReload(n => n + 1)}>刷新价格</Button><Button type="primary" onClick={() => begin()}>{draft ? session.pending ? '核对价格保存' : '继续价格草稿' : '配置价格'}</Button></Space></div>
    {notice && <Alert type="info" showIcon title={notice} />}
    {error && !open && <Alert type="error" showIcon title={error} />}
    <Table<ModelPrice> size="small" rowKey="id" dataSource={prices} loading={reading} pagination={false} scroll={{ x: 900 }} columns={[
      { title: '模型', render: (_, p) => <div className="ws-usage-model"><strong>{configs.find(c => c.id === p.model_config_id)?.name || (p.source === 'environment' ? '环境模型' : p.model_config_id)}</strong><span>{p.model}</span><span>{p.protocol === 'openai_chat' ? 'OpenAI Chat' : 'Anthropic Messages'}</span></div> },
      { title: '币种', dataIndex: 'currency', width: 90 },
      { title: '输入 / 百万 tokens', dataIndex: 'input_per_million', render: exactDecimal, width: 190 },
      { title: '输出 / 百万 tokens', dataIndex: 'output_per_million', render: exactDecimal, width: 190 },
      { title: '状态', width: 100, render: (_, p) => <Tag>{p.enabled ? '启用' : '已停用'}</Tag> },
      { title: '操作', width: 100, render: (_, p) => <Button disabled={Boolean(draft && draft.id !== p.id)} onClick={() => begin(p)}>编辑价格</Button> },
    ]} />
    <TaskModal title="配置模型价格" open={active && open} onCancel={() => setOpen(false)} destroyOnHidden footer={<div className="modal-footer">
      {!session.pending && <Button disabled={busy} onClick={() => { updateDraft(null); setOpen(false); setError('') }}>放弃草稿</Button>}
      <Button onClick={() => setOpen(false)}>暂时关闭</Button>
      {session.pending ? <Button type="primary" loading={busy} onClick={() => void reconcile()}>核对保存结果</Button> : <Button type="primary" htmlType="submit" form="model-price-form" loading={busy}>保存价格</Button>}
    </div>}>
      {draft && <Form id="model-price-form" layout="vertical" onFinish={() => void save()} className="ws-model-form">
        {error && <Alert type="error" showIcon title={error} />}
        {session.pending && <Alert type="warning" showIcon title="价格保存待核对" description={checked ? notice : '只读取原记录，不自动再次提交。'} />}
        {checked && <div className="ws-usage-stack">
          {checked.conflict ? <Alert type="error" title="该模型已有另一条价格，请关闭本次草稿后从列表编辑已有价格。" /> : <Button disabled={busy} onClick={() => {
            if (!session.pending) return
            const savedID = session.pending.id
            session.pending = null
            updateDraft(checked.current ? draftForPrice(checked.current) : { ...draft, id: savedID, original: null })
            setChecked(null); setNotice(''); setError('')
          }}>按当前读取继续编辑</Button>}
          {checked.conflict && <Button onClick={() => { session.clear(); setDraft(null); setOpen(false); setChecked(null) }}>关闭本次草稿</Button>}
        </div>}
        <Form.Item label="模型" required validateStatus={fields.target ? 'error' : undefined} help={fields.target}><Select id="price-target" aria-label="价格模型" disabled={locked || Boolean(draft.original)} value={draft.target ? priceKey(draft.target) : undefined} showSearch optionFilterProp="label" options={targets.map(t => ({ value: t.value, label: t.label, disabled: !draft.original && prices.some(p => priceKey(p) === t.value) }))} onChange={value => updateDraft({ ...draft, target: targets.find(t => t.value === value)!.target })} /></Form.Item>
        <div className="ws-model-form-grid">
          <Form.Item label="币种"><Select aria-label="价格币种" disabled={locked} value={draft.currency} onChange={currency => updateDraft({ ...draft, currency })} options={['USD', 'CNY', 'EUR'].map(value => ({ value, label: value }))} /></Form.Item>
          <Form.Item label="用于后续调用"><Switch aria-label="启用模型价格" disabled={locked} checked={draft.enabled} onChange={enabled => updateDraft({ ...draft, enabled })} /></Form.Item>
          {priceFields.slice(0, 2).map(field => <Form.Item key={field} label={`${priceFieldLabels[field]} / 百万 tokens`} required validateStatus={fields[field] ? 'error' : undefined} help={fields[field]}><Input id={`price-${field}`} inputMode="decimal" disabled={locked} value={draft[field]} onChange={e => updateDraft({ ...draft, [field]: e.target.value })} /></Form.Item>)}
        </div>
        <GridFold title="缓存单价（可选）" open={cacheOpen} onOpenChange={setCacheOpen}>{priceFields.slice(2).map(field => <Form.Item key={field} label={`${priceFieldLabels[field]} / 百万 tokens`} validateStatus={fields[field] ? 'error' : undefined} help={fields[field]}><Input id={`price-${field}`} inputMode="decimal" disabled={locked} placeholder="未知" value={draft[field]} onChange={e => updateDraft({ ...draft, [field]: e.target.value })} /></Form.Item>)}</GridFold>
      </Form>}
    </TaskModal>
  </section>
}
