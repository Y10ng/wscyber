import React, { useEffect, useMemo, useState } from 'react'
import { Alert, App as AntApp, Button, Space, Switch, Tag, Typography, Input, Select, Tooltip } from 'antd'
import { GridFold } from '../../ui/SurfaceControls.tsx'
import { materialSelectionError, validUTF8Text } from '../../ui/materials.ts'
import { ExplorationMaterials } from './ExplorationMaterials.tsx'
import { ResearchBriefEditor } from '../../ui/ResearchBrief.tsx'
import { hasBriefDraft, researchBriefPayload, validateResearchBrief, type BriefErrors } from '../../ui/researchBrief.ts'
import { canStartExploration, type ModelConfig } from '../models.ts'
import { CheckCircleOutlined, ClearOutlined, ReloadOutlined, RocketOutlined, SyncOutlined } from '@ant-design/icons'
import type { ExplorationDraft, ExplorationSubmissionState } from '../idempotency.ts'
import { isSubmissionLocked, utf8ByteLength } from '../idempotency.ts'
import { parseOriginInputText } from '../origin.ts'
import type { ExplorationInput, SystemInfo } from '../types.ts'

const formId = 'exploration-create-form'
type Field = 'description' | 'goal' | 'origins' | 'recording' | 'safety' | 'materials' | 'brief'

interface ExplorationCreateViewProps {
  systemInfo: SystemInfo | null
  models?: ModelConfig[] | null
  modelsLoading?: boolean
  modelsError?: string
  onRefreshModels?: () => void
  draft: ExplorationDraft
  submissionState: ExplorationSubmissionState
  onChangeDraft: (next: ExplorationDraft) => void
  onSubmitExploration: (payload: ExplorationInput) => Promise<void>
}

/** 表单只编辑 App 草稿；卸载/关闭既不重置提交，也不触发网络请求。 */
export const ExplorationCreateView: React.FC<ExplorationCreateViewProps> = ({
  systemInfo, models, modelsLoading, modelsError, onRefreshModels, draft, submissionState, onChangeDraft, onSubmitExploration,
}) => {
  const [validation, setValidation] = useState<{ field: Field; message: string } | null>(null)
  const [showKeyDetail, setShowKeyDetail] = useState(false)
  const [briefErrors, setBriefErrors] = useState<BriefErrors>({})
  const [advancedOpen, setAdvancedOpen] = useState(Boolean(draft.recordModelContent || draft.modelConfigID || draft.materials?.length || draft.safetyPromptEnabled || draft.safetyPrompt || hasBriefDraft(draft.researchBrief)))
  useEffect(() => {
    if (!validation) return
    const target = document.getElementById(`exploration-${validation.field}`)
    if (validation.field === 'brief') (target?.querySelector<HTMLElement>('[aria-invalid="true"]') ?? target)?.focus()
    else target?.focus()
  }, [validation, advancedOpen])
  const { message } = AntApp.useApp()
  const locked = isSubmissionLocked(submissionState)
  const modelConfigured = canStartExploration(systemInfo, draft.modelConfigID, models)
  const originParse = useMemo(() => parseOriginInputText(draft.originsText), [draft.originsText])
  const descBytes = utf8ByteLength(draft.description)
  const goalBytes = utf8ByteLength(draft.goal)

  const fail = (field: Field, message: string) => {
    if (['recording', 'safety', 'materials', 'brief'].includes(field)) setAdvancedOpen(true)
    setValidation({ field, message })
  }
  const handleStartSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (locked || !modelConfigured) return
    setValidation(null)
    if (!draft.description.trim()) return fail('description', '请填写“目标／已知信息”。')
    if (descBytes > 64000) return fail('description', '“目标／已知信息”不能超过 64000 字节。')
    if (!draft.goal.trim()) return fail('goal', '请填写“希望达到的目标”。')
    if (goalBytes > 32000) return fail('goal', '“希望达到的目标”不能超过 32000 字节。')
    if (originParse.errors.length > 0) return fail('origins', '授权格式待修正，请核对以下行。')
    if (originParse.origins.length === 0 || originParse.origins.length > 64) {
      return fail('origins', '请单独填写 1–64 个有效的精确 HTTP/HTTPS Origin 授权范围。')
    }
    if (draft.recordModelContent && !systemInfo?.model_content_recording_available) return fail('recording', '本机暂不能保存模型原文，请关闭记录或检查服务配置。')
    const materialError = materialSelectionError(draft.materials ?? [])
    if (materialError) return fail('materials', materialError)
    if (draft.safetyPromptEnabled && (!(draft.safetyPrompt || '').trim() || utf8ByteLength(draft.safetyPrompt || '') > 8000 || !validUTF8Text(draft.safetyPrompt || ''))) return fail('safety', '开启附加安全提示词后，请填写不超过 8,000 字节的有效文本。')
    const brief = researchBriefPayload(draft.researchBrief)
    const errors = brief ? validateResearchBrief(brief) : {}
    setBriefErrors(errors)
    if (Object.keys(errors).length) return fail('brief', '请修正身份说明与业务前提。')
    await onSubmitExploration({ description: draft.description, goal: draft.goal, allowed_origins: originParse.origins, ...(draft.modelConfigID ? { model_config_id: draft.modelConfigID } : {}), ...(draft.recordModelContent ? { record_model_content: true } : {}), ...(draft.materials?.length ? { materials: draft.materials.map(item => ({ id: item.id, token: item.token })) } : {}), ...(draft.safetyPromptEnabled ? { safety_prompt_enabled: true, safety_prompt: draft.safetyPrompt || '' } : {}), ...(brief ? { research_brief: brief } : {}) })
  }
  const fieldError = (field: Field) => validation?.field === field
    ? <Typography.Text id={`exploration-${field}-error`} type="danger" role="alert">{validation.message}</Typography.Text>
    : null

  return (
    <form id={formId} onSubmit={handleStartSubmit} className="ws-form-stack ws-create-form">
      {!modelConfigured && !locked && <Alert type={systemInfo ? 'warning' : 'info'} showIcon
        title={systemInfo ? '模型未配置，当前不可启动新的探索任务。' : '正在读取启动配置…'} />}
      {locked && <Alert type={submissionState.phase === 'unverified' ? 'warning' : 'info'} showIcon
        title={submissionState.phase === 'unverified' ? '提交结果待核对：已锁定本次请求内容与提交键' : '正在处理提交，请求内容已锁定'}
        description={submissionState.message} />}
      {!locked && submissionState.message && <Alert type="error" showIcon title={submissionState.message} />}

      <div className="ws-field-group">
        <div className="ws-label-row">
          <label htmlFor="exploration-description" className="ws-field-label">目标／已知信息</label>
          {descBytes > 64000 && <Typography.Text type="danger">已超出上限（{descBytes.toLocaleString('zh-CN')} / 64,000 字节）</Typography.Text>}
        </div>
        <Input.TextArea id="exploration-description" rows={4} disabled={locked}
          aria-invalid={validation?.field === 'description'} aria-describedby={validation?.field === 'description' ? 'exploration-description-error' : undefined}
          placeholder="输入目标系统背景、已知接口或测试线索" value={draft.description}
          onChange={(e) => { setValidation(null); onChangeDraft({ ...draft, description: e.target.value }) }} />
        {fieldError('description')}
      </div>
      <div className="ws-field-group">
        <div className="ws-label-row">
          <label htmlFor="exploration-goal" className="ws-field-label">希望达到的目标</label>
          {goalBytes > 32000 && <Typography.Text type="danger">已超出上限（{goalBytes.toLocaleString('zh-CN')} / 32,000 字节）</Typography.Text>}
        </div>
        <Input.TextArea id="exploration-goal" rows={3} disabled={locked}
          aria-invalid={validation?.field === 'goal'} aria-describedby={validation?.field === 'goal' ? 'exploration-goal-error' : undefined}
          placeholder="输入本次探索希望重点验证的业务功能或权限边界" value={draft.goal}
          onChange={(e) => { setValidation(null); onChangeDraft({ ...draft, goal: e.target.value }) }} />
        {fieldError('goal')}
      </div>
      <section className="ws-authorization-section" aria-label="明确授权范围">
        <div className="ws-field-group">
          <label htmlFor="exploration-origins" className="ws-field-label">授权范围（精确 HTTP/HTTPS Origin）</label>
          <Input.TextArea id="exploration-origins" rows={3} disabled={locked}
            aria-invalid={validation?.field === 'origins' || originParse.errors.length > 0}
            placeholder="每行一个精确 Origin，如 https://app.example.com" value={draft.originsText}
            onChange={(e) => { setValidation(null); onChangeDraft({ ...draft, originsText: e.target.value }) }} />
          {fieldError('origins')}
          {originParse.origins.length > 64 && <Typography.Text type="danger">超出 64 个上限（当前 {originParse.origins.length} 个）</Typography.Text>}
        </div>
        {originParse.origins.length > 0 && <div className="ws-field-group">
          <Typography.Text type="secondary">规范化范围预览</Typography.Text>
          <Space size={[8, 8]} wrap>{originParse.origins.map((origin) => <Tag key={origin} icon={<CheckCircleOutlined />} color="processing" className="ws-mono-tag">{origin}</Tag>)}</Space>
        </div>}
        {originParse.errors.length > 0 && <Alert type="error" showIcon title="授权格式待修正" description={
          <ul className="ws-error-list">{originParse.errors.map((item, idx) => <li key={`${item.line}-${idx}`}>第 {item.line} 行 <code>{item.raw}</code>：{item.error}</li>)}</ul>
        } />}
      </section>
      <GridFold title="高级设置" open={advancedOpen} onOpenChange={setAdvancedOpen}>
      {(systemInfo?.model_management_available || draft.modelConfigID) && <div className="ws-field-group">
        <label htmlFor="exploration-model" className="ws-field-label">研究模型</label>
        <Select id="exploration-model" value={draft.modelConfigID || ''} disabled={locked} loading={modelsLoading} showSearch optionFilterProp="label" onChange={modelConfigID => onChangeDraft({ ...draft, modelConfigID })} options={[
          { value: '', label: systemInfo?.model_configured ? `使用默认 · ${systemInfo.model}` : '使用默认（尚不可用）' },
          ...(models ?? []).filter(model => model.enabled || model.id === draft.modelConfigID).map(model => ({ value: model.id, label: `${model.name} · ${model.model}${!model.enabled ? '（已禁用）' : ''}`, disabled: !model.enabled })),
          ...(draft.modelConfigID && !models?.some(model => model.id === draft.modelConfigID) ? [{ value: draft.modelConfigID, label: '所选模型暂不可用', disabled: true }] : []),
        ]} />
        {modelsError && <Alert type="warning" title={modelsError} action={<Button onClick={onRefreshModels}>重新读取模型</Button>} />}
      </div>}
        <div id="exploration-brief" tabIndex={-1}><ResearchBriefEditor value={draft.researchBrief} disabled={locked} errors={briefErrors} onChange={researchBrief => { setBriefErrors({}); setValidation(null); onChangeDraft({ ...draft, researchBrief }) }} />{fieldError('brief')}</div>
        <div id="exploration-materials" tabIndex={-1}>{advancedOpen && <ExplorationMaterials selected={draft.materials ?? []} disabled={locked} onChange={materials => onChangeDraft({ ...draft, materials })} />}{fieldError('materials')}</div>
        <div className="ws-label-row"><label htmlFor="exploration-recording" className="ws-field-label">保存模型输入与返回</label><Switch id="exploration-recording" checked={Boolean(draft.recordModelContent)} disabled={locked} aria-invalid={validation?.field === 'recording'} aria-describedby={validation?.field === 'recording' ? 'exploration-recording-error' : undefined} onChange={checked => {
          if (checked && !systemInfo?.model_content_recording_available) { fail('recording', '本机暂不能保存模型原文，请检查服务配置。'); return }
          setValidation(null)
          onChangeDraft({ ...draft, recordModelContent: checked })
          if (checked) void message.warning('本次任务将保存可能含敏感研究材料的模型输入与返回；启动后不能切换。')
        }} /></div>
        {fieldError('recording')}
        <section className="ws-field-group" aria-label="测试安全"><div className="ws-label-row"><Tooltip title="提示词约束，不保证拦截" trigger={['hover', 'focus']}><label tabIndex={0} htmlFor="exploration-safety-enabled" className="ws-field-label">附加安全提示词</label></Tooltip><Switch id="exploration-safety-enabled" disabled={locked} checked={Boolean(draft.safetyPromptEnabled)} onChange={safetyPromptEnabled => onChangeDraft({ ...draft, safetyPromptEnabled })} /></div>
          {draft.safetyPromptEnabled && <Input.TextArea id="exploration-safety" aria-label="附加安全提示词内容" rows={4} disabled={locked} value={draft.safetyPrompt || ''} aria-invalid={validation?.field === 'safety'} onChange={event => onChangeDraft({ ...draft, safetyPrompt: event.target.value })} />}
          {fieldError('safety')}
        </section>
      </GridFold>
      {submissionState.operationKey && <div className="ws-key-disclosure">
        <Button type="link" style={{ padding: 0 }} aria-expanded={showKeyDetail} onClick={() => setShowKeyDetail((prev) => !prev)}>
          {showKeyDetail ? '收起提交技术细节' : '展开提交技术细节'}
        </Button>
        {showKeyDetail && <div style={{ marginTop: 8 }}><Typography.Text type="secondary">提交键：</Typography.Text><code className="ws-inline-code">{submissionState.operationKey}</code></div>}
      </div>}
    </form>
  )
}

/** TaskModal 的固定底栏；提交仍交由表单校验，核对与重试必须显式触发。 */
export function ExplorationCreateActions({ systemInfo, canStart, submissionState, hasDraftContent, onClearDraft, onVerifyOperation, onRetrySameKey, onClose }: {
  systemInfo: SystemInfo | null
  canStart?: boolean
  submissionState: ExplorationSubmissionState
  hasDraftContent: boolean
  onClearDraft: () => void
  onVerifyOperation: () => Promise<void>
  onRetrySameKey: () => Promise<void>
  onClose: () => void
}) {
  const locked = isSubmissionLocked(submissionState)
  const busy = submissionState.phase === 'submitting' || submissionState.phase === 'verifying'
  return <div className="modal-footer ws-create-actions">
    {!locked && <Button icon={<ClearOutlined />} disabled={!hasDraftContent} onClick={onClearDraft}>清空草稿</Button>}
    <Button onClick={onClose}>暂时关闭</Button>
    {!locked ? <Button type="primary" htmlType="submit" form={formId} icon={<RocketOutlined />} disabled={!(canStart ?? systemInfo?.model_configured)}>开始探索</Button> : <>
      <Button icon={<ReloadOutlined />} loading={submissionState.phase === 'submitting'} disabled={busy} onClick={() => void onRetrySameKey()}>使用原提交键重试</Button>
      <Button type="primary" icon={<SyncOutlined />} loading={submissionState.phase === 'verifying'} disabled={busy} onClick={() => void onVerifyOperation()}>核对提交状态</Button>
    </>}
  </div>
}
