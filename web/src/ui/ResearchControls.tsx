import { useId } from 'react'
import { Alert, Button, Checkbox, Empty, Input, Space, Spin, Tag, Tooltip } from 'antd'
import { PauseOutlined, PlayCircleOutlined, FileAddOutlined } from '@ant-design/icons'
import { controlHelp, controlLabels, type ResearchCommand, type SupplementDraft, type SupplementMeta } from './researchControls.ts'
import './researchControls.css'

/** 只展示持久事实及受控操作；不从status推断暂停意图，不执行网络。 */
export function ResearchControlBar({ pauseRequested, autoRun, steps, maxSteps, disabled, continuationBlocked, runBlocked, onCommand, onSupplement }: {
  pauseRequested: boolean | undefined; autoRun: boolean; steps: number; maxSteps?: number; disabled?: boolean; continuationBlocked?: boolean; runBlocked?: boolean
  onCommand: (command: ResearchCommand) => void; onSupplement: () => void
}) {
  return <section className="research-controls" aria-label="研究控制"><Space wrap><Tag>{pauseRequested === undefined ? '暂停意图：未知' : pauseRequested ? '已请求暂停' : '未请求暂停'}</Tag><Tag>模型研究：{autoRun ? '开启' : '关闭'}</Tag><span>累计 {steps} / {maxSteps ?? '未知'} 步</span></Space>
    <Space wrap>{(['pause', 'run', 'resume'] as const).map(command => <Tooltip key={command} title={controlHelp[command]} trigger={['hover', 'focus']}><Button icon={command === 'pause' ? <PauseOutlined /> : <PlayCircleOutlined />} disabled={disabled || (command !== 'pause' && continuationBlocked) || (command === 'run' && runBlocked)} onClick={() => onCommand(command)}>{controlLabels[command]}</Button></Tooltip>)}<Button icon={<FileAddOutlined />} onClick={onSupplement}>人工文本补充</Button></Space>
  </section>
}
export function SupplementEditor({ value, disabled, error, onChange }: { value: SupplementDraft; disabled?: boolean; error?: string; onChange: (value: SupplementDraft) => void }) {
  const id = useId()
  return <div className="supplement-editor"><Tag>用户声明 · 待核验</Tag><label htmlFor={`${id}-source`}>补充来源</label><Input id={`${id}-source`} autoFocus autoComplete="off" disabled={disabled} value={value.source} onChange={e => onChange({ ...value, source: e.target.value })} />
    <div className="supplement-label"><label htmlFor={`${id}-content`}>补充正文（非秘密文本）</label><span>{new TextEncoder().encode(value.content).length.toLocaleString('zh-CN')} / 16,384 字节</span></div><Input.TextArea id={`${id}-content`} rows={8} autoComplete="off" spellCheck={false} disabled={disabled} value={value.content} onChange={e => onChange({ ...value, content: e.target.value })} />
    <Checkbox checked={value.share_with_model} disabled={disabled} onChange={e => onChange({ ...value, share_with_model: e.target.checked })}>提供给后续研究模型</Checkbox>{error && <Alert type="error" showIcon title={error} />}
  </div>
}
export function SupplementList({ items, busy, error, onOpen, onCalls }: { items: SupplementMeta[]; busy?: boolean; error?: string; onOpen: (item: SupplementMeta) => void; onCalls: (item: SupplementMeta) => void }) {
  return <section className="material-list-pane" aria-label="人工补充记录">{error && <Alert type="error" showIcon title={error} />}<Spin spinning={Boolean(busy)}>{!items.length && !busy && !error && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无人工补充" />}{items.map(item => <div className="material-row" key={item.id}><div className="material-row-text"><strong>{item.source}</strong><span>{item.created_at} · {item.byte_size.toLocaleString('zh-CN')} 字节</span><span>用户声明 · 已保存</span></div><Space wrap><Button onClick={() => onOpen(item)}>查看原件</Button><Button onClick={() => onCalls(item)}>调用事实</Button></Space></div>)}</Spin></section>
}
