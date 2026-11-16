import { useId, useRef, type ReactNode } from 'react'
import { Button, Empty, Input, Switch, Tag, Tooltip } from 'antd'
import { DeleteOutlined, PlusOutlined, QuestionCircleOutlined } from '@ant-design/icons'
import { emptyResearchBrief, type ResearchBrief, type ResearchIdentity, type BriefErrors } from './researchBrief.ts'
import './researchBrief.css'

/** 受控非秘密说明，无请求/认证/业务提交；分享只表达用户选择。 */
export function ResearchBriefEditor({ value = emptyResearchBrief(), onChange, disabled = false, errors = {} }: {
  value?: ResearchBrief; onChange: (value: ResearchBrief) => void; disabled?: boolean; errors?: BriefErrors
}) {
  const prefix = useId(), root = useRef<HTMLElement>(null), identities = value.identities ?? []
  const focus = (key: string) => requestAnimationFrame(() => document.getElementById(`${prefix}-${key}`)?.focus())
  const update = (index: number, patch: Partial<ResearchIdentity>) => onChange({ ...value, identities: identities.map((i, n) => n === index ? { ...i, ...patch } : i) })
  const error = (key: string) => errors[key] ? <span id={`${prefix}-${key}-error`} role="alert" className="brief-error">{errors[key]}</span> : null
  const field = (key: string, label: string, text: string, change: (text: string) => void, multiline = false) => {
    const props = { id: `${prefix}-${key}`, value: text, disabled, autoComplete: 'off', spellCheck: false, 'aria-invalid': Boolean(errors[key]), 'aria-describedby': errors[key] ? `${prefix}-${key}-error` : undefined, status: errors[key] ? 'error' as const : undefined, onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => change(e.target.value) }
    return <div className="brief-field"><label htmlFor={props.id}>{label}</label>{multiline ? <Input.TextArea {...props} rows={3} /> : <Input {...props} />}{error(key)}</div>
  }
  return <section ref={root} className="research-brief" aria-label="身份说明与业务前提">
    <div className="brief-heading"><h3>身份说明与业务前提</h3><Tooltip title="仅填写非秘密用户声明，不代表已认证或已验证；请勿填写密码、Cookie、Token或请求头。" trigger={['hover', 'focus']}><Button type="text" size="small" icon={<QuestionCircleOutlined />} aria-label="身份说明填写提示" /></Tooltip><Button id={`${prefix}-add`} disabled={disabled || identities.length >= 8} icon={<PlusOutlined />} onClick={() => { onChange({ ...value, identities: [...identities, { label: '', role: '' }] }); focus(`${identities.length}-label`) }}>添加身份说明</Button></div>
    {identities.map((item, index) => <div className="brief-identity" role="group" aria-label={`身份说明 ${index + 1}`} key={index}>
      {field(`${index}-label`, `身份别名 ${index + 1}`, item.label, label => update(index, { label }))}
      {field(`${index}-role`, `角色 ${index + 1}`, item.role, role => update(index, { role }))}
      <Button className="brief-remove" disabled={disabled} icon={<DeleteOutlined />} aria-label={`删除身份说明 ${index + 1}`} onClick={() => { onChange({ ...value, identities: identities.filter((_, n) => n !== index) }); focus(identities.length > 1 ? `${Math.min(index, identities.length - 2)}-label` : 'add') }} />
      <div className="brief-ownership">{field(`${index}-ownership`, `资源归属 ${index + 1}`, item.ownership || '', ownership => update(index, { ownership }))}</div>
    </div>)}
    {field('rules', '业务规则', value.business_rules || '', business_rules => onChange({ ...value, business_rules }), true)}
    {field('preconditions', '复现前提', value.preconditions || '', preconditions => onChange({ ...value, preconditions }), true)}
    <div className="brief-sharing"><Tooltip title="开启后，此组内容将在研究调用时提供给本任务绑定模型；这不是已发送或已阅读状态。勿填凭据。" trigger={['hover', 'focus']}><label tabIndex={0} htmlFor={`${prefix}-share`}>提供给研究模型</label></Tooltip><Switch id={`${prefix}-share`} disabled={disabled} checked={value.share_with_model} onChange={share_with_model => onChange({ ...value, share_with_model })} /></div>
    {error('share')}{error('group')}
  </section>
}

/** 固定配置纯文本查看；不渲染HTML/链接，不伪造认证或发送事实。 */
export function ResearchBriefSummary({ value }: { value?: ResearchBrief }) {
  if (!value) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="未配置身份说明与业务前提" />
  const row = (label: string, body: ReactNode) => <div className="brief-summary-row"><strong>{label}</strong><div>{body}</div></div>
  return <section className="research-brief brief-summary" aria-label="固定身份说明与业务前提">
    <div className="brief-heading"><h3>身份说明与业务前提</h3><Tag>用户声明 · 待核验</Tag></div>
    {row('提供给研究模型', <Tooltip title="创建时固定的分享选择，不表示已经发送、阅读或验证。" trigger={['hover', 'focus']}><Tag tabIndex={0}>{value.share_with_model ? '配置：开启' : '配置：关闭'}</Tag></Tooltip>)}
    {value.identities?.map((item, index) => <div className="brief-summary-identity" key={index}>{row('身份别名', item.label)}{row('角色', item.role)}{item.ownership && row('资源归属', item.ownership)}</div>)}
    {value.business_rules && row('业务规则', value.business_rules)}{value.preconditions && row('复现前提', value.preconditions)}
  </section>
}
