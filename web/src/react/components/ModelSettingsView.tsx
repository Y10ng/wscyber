import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, Descriptions, Empty, Form, Input, InputNumber, Select, Space, Spin, Switch, Tag } from 'antd'
import { SecretEntriesEditor } from '../../ui/SecretEntriesEditor.tsx'
import { GridFold } from '../../ui/SurfaceControls.tsx'
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { TaskModal } from '../../ui/TaskModal.tsx'
import { ApiError, StaleSessionError } from '../api.ts'
import { generateOperationKey } from '../idempotency.ts'
import { fetchModelConfig, fetchModelConfigs, fetchModelTest, postModelTest, patchModelConfig, fetchModelSettings, putModelSettings } from '../modelApi.ts'
import { encodeModelDraft, modelDraft, modelEndpoint, modelTestPath, modelEntryErrors, modelRecipientChanged, testAttempt, testStatus, validateModelDraft, type DraftErrors, type ModelConfig, type ModelDraft, type ModelTest, type ModelTestSession, type ModelSettings } from '../models.ts'

interface Props {
  active: boolean
  configID: string | null
  testID: string | null
  invalid?: boolean
  session: ModelTestSession
  navigate: (path: string) => void
  onModelsChanged?: () => void
}
type SaveState = { phase: 'idle' | 'saving' | 'unverified'; message: string; current?: ModelConfig }
const idleSave: SaveState = { phase: 'idle', message: '' }
const protocolName = (config: ModelConfig) => config.protocol === 'openai_chat' ? 'OpenAI Chat' : 'Anthropic Messages'
const silent = (error: unknown) => error instanceof StaleSessionError || (error instanceof ApiError && error.status === 401)
function readError(error: unknown) {
  if (error instanceof ApiError) {
    if (error.status === 404) return '暂未查到记录；仍保留原身份，可再次读取核对。'
    if (error.status === 503) return '服务暂不可用，请稍后读取核对。'
    if (error.status === 403) return '无权读取此记录。'
  }
  return '读取未完成，请检查连接后再次核对。'
}

export function ModelSettingsView({ active, configID, testID, invalid, session, navigate, onModelsChanged }: Props) {
  const [settings, setSettings] = useState<ModelSettings | null>(null)
  const [defaultError, setDefaultError] = useState('')
  const [defaultBusy, setDefaultBusy] = useState(false)
  const defaultLock = useRef(false)
  const changedRef = useRef(onModelsChanged)
  changedRef.current = onModelsChanged
  const [configs, setConfigs] = useState<ModelConfig[] | null>(null)
  const [listError, setListError] = useState('')
  const [listLoading, setListLoading] = useState(false)
  const [listTick, setListTick] = useState(0)
  const [draft, setDraft] = useState<ModelDraft | null>(null)
  const [editorOpen, setEditorOpen] = useState(false)
  const [errors, setErrors] = useState<DraftErrors>({})
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [save, setSave] = useState<SaveState>(idleSave)
  const [checkingSave, setCheckingSave] = useState(false)
  const [confirm, setConfirm] = useState<ModelConfig | null>(null)
  const [confirmError, setConfirmError] = useState('')
  const [receipt, setReceipt] = useState<ModelTest | undefined>()
  const [testError, setTestError] = useState('')
  const [testReading, setTestReading] = useState(false)
  const [testTick, setTestTick] = useState(0)
  const [pollStopped, setPollStopped] = useState(false)
  const [postBusy, setPostBusy] = useState(false)
  const [, renderSession] = useState(0)
  const generation = useRef(0)
  const saveLock = useRef(false)
  const confirmLock = useRef(false)
  const writes = useRef(new Set<AbortController>())
  const saveRead = useRef<AbortController | null>(null)
  const source = useRef<HTMLElement | null>(null)
  const newButton = useRef<React.ComponentRef<typeof Button>>(null)
  const activeRef = useRef(active)
  activeRef.current = active

  // 隐藏/切页撤销只读链及在途UI结果；服务端写入不因此被当作撤销。
  useEffect(() => {
    if (!active) { setEditorOpen(false); setConfirm(null) }
    return () => {
      generation.current++
      for (const controller of writes.current) controller.abort()
      writes.current.clear()
      saveRead.current?.abort()
      if (saveLock.current) setSave({ phase: 'unverified', message: '保存结果待核对；请按原配置 ID 读取。' })
      saveLock.current = false
      confirmLock.current = false
      setPostBusy(false)
      setCheckingSave(false)
      setDefaultBusy(false)
      defaultLock.current = false
    }
  }, [active])

  useEffect(() => {
    if (!draft) return
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = '' }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [Boolean(draft)])

  useEffect(() => {
    if (!active) return
    const controller = new AbortController()
    setListLoading(true); setListError('')
    void fetchModelConfigs(controller.signal).then(data => {
      if (!controller.signal.aborted) setConfigs(data)
    }).catch(error => {
      if (!controller.signal.aborted && !silent(error)) setListError(`列表未更新。${readError(error)}`)
    }).finally(() => { if (!controller.signal.aborted) setListLoading(false) })
    const defaultGeneration = session.defaultGeneration
    void fetchModelSettings(controller.signal).then(current => {
      if (controller.signal.aborted || !session.acceptDefaultRead(current, defaultGeneration)) return
      setSettings(previous => previous && previous.revision > current.revision ? previous : current)
      const pending = session.defaultPending
      if (!pending) setDefaultError('')
      else if (pending.rejected || current.revision > pending.expectedRevision) {
        delete session.defaultPending
        setDefaultError(current.default_config_id === pending.targetID ? '' : '当前默认模型与本次选择不同，请核对后重新选择。')
        changedRef.current?.()
      } else setDefaultError('默认设置结果待核对；不会自动重复提交。')
    }).catch(error => {
      if (!controller.signal.aborted && defaultGeneration === session.defaultGeneration && !silent(error)) setDefaultError(`默认设置未更新。${readError(error)}`)
    })
    return () => controller.abort()
  }, [active, listTick, session])

  // 挂载/深链/刷新只有GET；串行等待完成后再排下一次，没有自动POST重试。
  useEffect(() => {
    setReceipt(undefined); setTestError(''); setTestReading(false)
    if (!active || !configID || !testID) return
    const known = session.remember(configID, testID)
    setReceipt(known.receipt)
    if (postBusy || pollStopped) return
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    const read = async () => {
      setTestReading(true)
      try {
        const value = await fetchModelTest(configID, testID, controller.signal)
        if (controller.signal.aborted) return
        session.accept(value); setReceipt(value); setTestError(''); renderSession(n => n + 1)
        if (value.status !== 'completed') timer = setTimeout(() => void read(), 1200)
      } catch (error) {
        if (!controller.signal.aborted && !silent(error)) setTestError(readError(error))
      } finally { if (!controller.signal.aborted) setTestReading(false) }
    }
    void read()
    return () => { controller.abort(); if (timer) clearTimeout(timer) }
  }, [active, configID, testID, testTick, postBusy, pollStopped, session])

  const rememberSource = () => { source.current = document.activeElement instanceof HTMLElement ? document.activeElement : null }
  const restoreFocus = () => {
    if (!activeRef.current) return
    if (source.current?.isConnected && source.current.checkVisibility({ checkVisibilityCSS: true })) source.current.focus({ preventScroll: true })
    else newButton.current?.focus({ preventScroll: true })
  }
  const openEditor = (config?: ModelConfig) => {
    rememberSource()
    // 单一内存草稿；未核对写入不被另一配置覆盖。
    if (!draft) { setDraft(modelDraft(config)); setSave(idleSave); setErrors({}) }
    setEditorOpen(true)
  }
  const update = <K extends keyof ModelDraft>(key: K, value: ModelDraft[K]) => {
    setDraft(prev => prev ? { ...prev, [key]: value } : prev)
    setErrors(prev => ({ ...prev, [key]: undefined }))
  }
  const checkSave = useCallback(async (id: string) => {
    if (saveRead.current) return
    const controller = new AbortController(); saveRead.current = controller
    setCheckingSave(true)
    try {
      const current = await fetchModelConfig(id, controller.signal)
      if (controller.signal.aborted) return
      setSave({ phase: 'unverified', current, message: '已读取当前配置；秘密不回显，无法据此确认本次秘密修改。可载入当前配置后继续编辑。' })
      changedRef.current?.()
      setListTick(n => n + 1)
    } catch (error) {
      if (!controller.signal.aborted && !silent(error)) setSave({ phase: 'unverified', message: readError(error) })
    } finally {
      if (saveRead.current === controller) saveRead.current = null
      if (!controller.signal.aborted) setCheckingSave(false)
    }
  }, [])

  const submitSave = async (value: ModelDraft) => {
    if (saveLock.current || save.phase === 'unverified') return
    const validation = validateModelDraft(value); setErrors(validation)
    if (Object.keys(validation).length) {
      if (validation.headers || validation.parameters || validation.timeout_seconds || validation.max_output_tokens) setAdvancedOpen(true)
      requestAnimationFrame(() => document.querySelector<HTMLElement>('.ws-model-form [aria-invalid="true"], .ws-model-form .ant-form-item-has-error input')?.focus())
      return
    }
    saveLock.current = true
    const gen = generation.current
    const controller = new AbortController(); writes.current.add(controller)
    setSave({ phase: 'saving', message: '' })
    try {
      await patchModelConfig(value.id, encodeModelDraft(value), controller.signal)
      if (gen !== generation.current || controller.signal.aborted) return
      setDraft(null); setEditorOpen(false); setSave(idleSave); setListTick(n => n + 1)
      changedRef.current?.()
    } catch (error) {
      if (gen !== generation.current || controller.signal.aborted || silent(error)) return
      if (error instanceof ApiError && [400, 403].includes(error.status)) {
        setSave({ phase: 'idle', message: error.status === 400 ? '配置未通过校验，请检查地址、秘密 JSON 的重复键、保留字段及认证方式。' : '无权保存此配置。' })
      } else {
        setSave({ phase: 'unverified', message: error instanceof ApiError && error.status === 409 ? '配置已被其他操作更新；保留原 ID 和草稿，正在读取当前配置。' : '保存结果待核对；保留原 ID 和草稿，不会自动重发。' })
        void checkSave(value.id)
      }
    } finally {
      writes.current.delete(controller)
      if (gen === generation.current) saveLock.current = false
    }
  }
  const selectDefault = async (config: ModelConfig) => {
    if (!settings || !config.enabled || defaultLock.current || session.defaultPending) return
    defaultLock.current = true; setDefaultBusy(true); setDefaultError('')
    const gen = generation.current, controller = new AbortController()
    writes.current.add(controller)
    const defaultGeneration = session.beginDefault(settings, config.id)
    try {
      const current = await putModelSettings(settings, config.id, controller.signal)
      if (gen !== generation.current || controller.signal.aborted || !session.acceptDefaultWrite(defaultGeneration)) return
      setSettings(previous => previous && previous.revision > current.revision ? previous : current); changedRef.current?.()
    } catch (error) {
      if (gen !== generation.current || defaultGeneration !== session.defaultGeneration || controller.signal.aborted || silent(error)) return
      if (error instanceof ApiError && [400, 403].includes(error.status)) {
        delete session.defaultPending; setDefaultError('默认设置未保存，请检查配置状态和权限。')
      } else {
        if (error instanceof ApiError && error.status === 409) session.rejectDefaultWrite(defaultGeneration)
        setDefaultError('默认设置结果待核对；不会自动重复提交。'); setListTick(n => n + 1)
      }
    } finally {
      writes.current.delete(controller)
      if (gen === generation.current) { defaultLock.current = false; setDefaultBusy(false) }
    }
  }
  const toggleConfig = (config: ModelConfig) => {
    if (draft) { openEditor(); return }
    const value = { ...modelDraft(config), enabled: !config.enabled }
    rememberSource(); setDraft(value); setEditorOpen(true); setSave(idleSave); setErrors({})
    // 与编辑共用明确保存，不以开关的乐观状态冒充已落库。
  }
  const openTest = (config: ModelConfig) => {
    const previous = session.forConfig(config.id)
    if (previous && previous.receipt?.status !== 'completed') {
      setPollStopped(false); navigate(modelTestPath(config.id, previous.testID)); setTestTick(n => n + 1); return
    }
    rememberSource(); setConfirmError(''); setConfirm(config)
  }
  const confirmTest = async () => {
    if (!confirm || confirmLock.current) return
    confirmLock.current = true
    const value = confirm
    let identity: string
    try { identity = generateOperationKey() } catch { setConfirmError('无法生成安全随机测试身份，未发送。'); confirmLock.current = false; return }
    const previous = session.forConfig(value.id)
    if (previous && previous.receipt?.status !== 'completed') { confirmLock.current = false; setConfirm(null); navigate(modelTestPath(value.id, previous.testID)); return }
    const gen = generation.current
    const controller = new AbortController(); writes.current.add(controller)
    session.remember(value.id, identity)
    setConfirm(null); setPostBusy(true); setPollStopped(false)
    // 先保留非秘密身份再发送；浏览器重载只会从该路由读取。
    navigate(modelTestPath(value.id, identity))
    try {
      const result = await postModelTest(value.id, identity, value.revision, controller.signal)
      if (gen !== generation.current || controller.signal.aborted || window.location.pathname !== modelTestPath(value.id, identity)) return
      session.accept(result); renderSession(n => n + 1)
    } catch (error) {
      if (gen !== generation.current || controller.signal.aborted || silent(error)) return
      // 所有POST失败均保留身份，后续只GET；404也不证明没有受理。
    } finally {
      writes.current.delete(controller)
      if (gen === generation.current) { confirmLock.current = false; setPostBusy(false); setTestTick(n => n + 1) }
    }
  }

  const field = (key: keyof ModelDraft, label: string, input: React.ReactNode) => <Form.Item label={label} htmlFor={`model-${key}`} validateStatus={errors[key] ? 'error' : undefined} help={errors[key]}>{input}</Form.Item>
  const currentConfig = configs?.find(c => c.id === configID)
  const selectedIdentity = configID && testID ? session.known.get(testID) : undefined
  const shownReceipt = receipt?.config_id === configID && receipt.id === testID ? receipt : selectedIdentity?.configID === configID ? selectedIdentity.receipt : undefined
  return <section hidden={!active} className="ws-model-settings" aria-label="模型配置工作面">
    <div className="ws-section-row">
      <Space wrap><Button icon={<ReloadOutlined />} loading={listLoading} onClick={() => setListTick(n => n + 1)}>刷新配置</Button>{draft && <Button onClick={() => openEditor()}>继续配置草稿</Button>}</Space>
      <Button ref={newButton} type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新建配置</Button>
    </div>
    {invalid && <Alert type="error" title="测试链接中的身份无效" />}
    {listError && <Alert type="error" title={listError} />}
    {defaultError && <Alert type="warning" title={defaultError} action={<Space wrap><Button loading={listLoading} onClick={() => setListTick(n => n + 1)}>核对默认设置</Button>{session.defaultPending && session.defaultChecked && <Button disabled={defaultBusy} onClick={() => {
      const current = session.continueDefaultFromRead()
      if (!current) return
      setSettings(current); setDefaultError('已按当前设置继续；此前写入结果仍未确认，后续保存仍会检查并发变更。')
    }}>按当前设置继续</Button>}</Space>} />}
    {configs === null ? <Spin spinning={listLoading} /> : configs.length === 0 ? <Card><Empty description="暂无模型配置" /></Card> : <div className="ws-model-list">
      {configs.map(config => {
        const known = session.forConfig(config.id)
        return <Card key={config.id} size="small" className="ws-model-row" data-config-id={config.id}>
          <div className="ws-model-main"><div className="ws-model-heading"><strong>{config.name}</strong><Tag>{config.enabled ? '已启用' : '已禁用'}</Tag>{settings?.default_config_id === config.id && <Tag color="processing">默认模型</Tag>}<span>{protocolName(config)}</span></div><div className="ws-model-connection"><span>{config.model}</span><span>{config.base_url}</span></div></div>
          <Space wrap className="ws-model-actions">
            {known && <Button onClick={() => { setPollStopped(false); navigate(modelTestPath(config.id, known.testID)); setTestTick(n => n + 1) }}>{known.receipt ? `${known.receipt.config_revision !== config.revision ? '此前测试 · ' : ''}${testStatus(known.receipt)}` : '核对测试'}</Button>}
            <Button onClick={() => openEditor(config)}>编辑</Button><Button onClick={() => toggleConfig(config)}>{config.enabled ? '禁用' : '启用'}</Button>
            <Button disabled={!config.enabled || postBusy} onClick={() => openTest(config)}>连通测试</Button>
            {settings?.default_config_id !== config.id && <Button disabled={!config.enabled || !settings || defaultBusy || Boolean(session.defaultPending)} onClick={() => void selectDefault(config)}>设为默认</Button>}
          </Space>
        </Card>
      })}
    </div>}
    {configID && testID && <Card title={<Space wrap><span>连通测试</span><Tag>{postBusy ? '受理情况待核对' : testStatus(shownReceipt)}</Tag>{shownReceipt && currentConfig && shownReceipt.config_revision !== currentConfig.revision && <Tag>配置已更新，此结果来自此前测试</Tag>}</Space>} extra={<Button onClick={() => navigate('/settings/models')}>收起</Button>}>
      <div className="ws-model-result" aria-live="polite">
        <span>配置 ID <code>{configID}</code></span><span>测试 ID <code>{testID}</code></span>
        {shownReceipt && <><span>{testAttempt(shownReceipt)}</span>{shownReceipt.http_status !== undefined && <span>HTTP {shownReceipt.http_status}</span>}<span>{shownReceipt.completed_at ?? shownReceipt.started_at ?? shownReceipt.created_at}</span></>}
      </div>
      {testError && <Alert type="warning" title={testError} />}
      <Space wrap><Button loading={testReading} disabled={postBusy} onClick={() => { setPollStopped(false); setTestTick(n => n + 1) }}>读取核对</Button>{(testReading || shownReceipt?.status === 'running' || shownReceipt?.status === 'pending') && !pollStopped && <Button onClick={() => setPollStopped(true)}>停止读取</Button>}</Space>
    </Card>}
    <TaskModal open={active && editorOpen} title={draft?.expected_revision ? '编辑模型配置' : '新建模型配置'} onCancel={() => setEditorOpen(false)} afterClose={restoreFocus} focusable={{ focusTriggerAfterClose: false }} destroyOnHidden
      footer={<Space wrap>{save.phase === 'unverified' ? <><Button loading={checkingSave} onClick={() => draft && void checkSave(draft.id)}>核对已保存配置</Button>{save.current && <Button onClick={() => { setDraft(modelDraft(save.current)); setSave(idleSave); setErrors({}) }}>载入当前配置</Button>}</> : <><Button disabled={save.phase === 'saving'} onClick={() => { setDraft(null); setEditorOpen(false); setErrors({}); setSave(idleSave) }}>放弃草稿</Button><Button type="primary" loading={save.phase === 'saving'} onClick={() => draft && void submitSave(draft)}>保存配置</Button></>}<Button onClick={() => setEditorOpen(false)}>关闭</Button></Space>}>
      {draft && <Form layout="vertical" disabled={save.phase !== 'idle'} autoComplete="off" className="ws-model-form" onFinish={() => void submitSave(draft)}>
        {save.message && <Alert type={save.phase === 'unverified' ? 'warning' : 'error'} title={save.message} />}
        <fieldset disabled={save.phase !== 'idle'}>
          <div className="ws-model-form-grid">
            {field('name', '配置名称', <Input id="model-name" value={draft.name} onChange={e => update('name', e.target.value)} />)}
            {field('protocol', '协议', <Select id="model-protocol" value={draft.protocol} onChange={v => update('protocol', v)} options={[{ value: 'openai_chat', label: 'OpenAI Chat' }, { value: 'anthropic_messages', label: 'Anthropic Messages' }]} />)}
            <div className="ws-model-wide">{field('base_url', 'Base URL', <Input id="model-base_url" placeholder={draft.protocol === 'openai_chat' ? 'https://api.openai.com/v1' : 'https://api.anthropic.com'} value={draft.base_url} onChange={e => update('base_url', e.target.value)} />)}</div>
            {field('model', '模型名称', <Input id="model-model" value={draft.model} onChange={e => update('model', e.target.value)} />)}
            {field('auth', '认证方式', <Select id="model-auth" value={draft.auth} onChange={v => update('auth', v)} options={[{ value: 'none', label: '无认证' }, { value: 'bearer', label: 'Bearer' }, ...(draft.protocol === 'anthropic_messages' ? [{ value: 'api_key', label: 'API Key Header' }] : [])]} />)}
            {draft.auth !== 'none' && field('api_key', 'API Key', <Input.Password id="model-api_key" autoComplete="new-password" placeholder={draft.original?.api_key_set && !modelRecipientChanged(draft) ? '已保存' : '请输入 API Key'} value={draft.api_key} onChange={e => update('api_key', e.target.value)} />)}
          </div>
          <Form.Item label="配置状态"><Switch aria-label="配置启用状态" checked={draft.enabled} onChange={v => update('enabled', v)} checkedChildren="启用" unCheckedChildren="禁用" /></Form.Item>
          <GridFold title="高级设置" open={advancedOpen} onOpenChange={setAdvancedOpen}>
            <div className="ws-model-form-grid">
              {field('timeout_seconds', '超时（秒）', <InputNumber id="model-timeout_seconds" min={1} max={600} precision={0} value={draft.timeout_seconds} onChange={v => update('timeout_seconds', v ?? 0)} />)}
              {field('max_output_tokens', '最大输出 tokens', <InputNumber id="model-max_output_tokens" min={1} max={32768} precision={0} value={draft.max_output_tokens} onChange={v => update('max_output_tokens', v ?? 0)} />)}
            </div>
            {(['headers', 'parameters'] as const).map(key => <div key={key}>
              <SecretEntriesEditor label={key === 'headers' ? '自定义 Header' : '扩展参数'} value={draft[key]} onChange={value => update(key, value)}
                savedNames={modelRecipientChanged(draft) ? [] : key === 'headers' ? draft.original?.header_names : draft.original?.parameter_keys}
                caseInsensitive={key === 'headers'} json={key === 'parameters'} disabled={save.phase !== 'idle'} errors={errors[key] ? modelEntryErrors(draft, key) : {}} />
              {errors[key] && <Alert type="error" title={errors[key]} />}
            </div>)}
          </GridFold>
        </fieldset>
      </Form>}
    </TaskModal>
    <TaskModal open={active && Boolean(confirm)} title="确认连通测试" width={720} onCancel={() => setConfirm(null)} afterClose={restoreFocus} focusable={{ focusTriggerAfterClose: false }} destroyOnHidden footer={<Space wrap><Button onClick={() => setConfirm(null)}>取消</Button><Button type="primary" onClick={() => void confirmTest()}>确认并测试</Button></Space>}>
      {confirm && <><Descriptions column={1} size="small" items={[{ key: 'endpoint', label: '接收端', children: <span className="ws-pre-wrap">{modelEndpoint(confirm)}</span> }, { key: 'model', label: '模型', children: confirm.model }, { key: 'content', label: '固定内容', children: 'Reply with OK.' }, { key: 'limit', label: '输出上限', children: `${Math.min(32, confirm.max_output_tokens)} tokens` }]} /><p>将向此接收端发送上述固定最小内容，不含项目材料，可能产生费用。</p></>}
      {confirmError && <Alert type="error" title={confirmError} />}
    </TaskModal>
  </section>
}
