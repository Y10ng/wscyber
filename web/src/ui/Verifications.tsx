import type { ReactNode } from 'react'
import { Alert, Button, Descriptions, Empty, InputNumber, Skeleton, Space, Table, Tag } from 'antd'
import { GridFold } from './SurfaceControls.tsx'
import { SkillPager, type SkillPagerProps } from './SkillRecords.tsx'
import type { FindingEvidence, FindingReference } from './Findings.tsx'
import './findings.css'

export type VerificationVerdict = 'supported' | 'counterevidence' | 'inconclusive'
export interface VerificationConclusion { verdict: VerificationVerdict; method: string; preconditions: string; summary: string; limitations: string; evidence: FindingReference[] }
export interface VerificationResult { attempt_id: string; call_id: string; created_at: string; conclusion: VerificationConclusion; evidence: FindingEvidence[] }
export interface VerificationSummary {
  id: string; mission_id: string; finding_id: string; source_revision_id: string; source_created_at: string; thread_id: string; max_steps: number; created_at: string
  status: string; pause_requested: boolean; auto_run: boolean; step_count: number; verdict: VerificationVerdict | ''; has_result: boolean; end_reason: '' | 'budget_exhausted'
}
export const verificationVerdicts: Record<VerificationVerdict, string> = { supported: '新证据支持主张', counterevidence: '发现反证', inconclusive: '证据不足' }
export const verificationStateLabels: Record<string, string> = { ready: '等待执行', running: '验证执行中', waiting_approval: '等待请求放行', blocked: '执行受阻', paused: '已暂停', concluded: '验证结束' }
export const verificationTime = (s: string) => new Date(s).toLocaleString('zh-CN', { hour12: false })
export function VerificationStartFields({ number, title, sourceAt, model, origins, steps, maximum, disabled, onSteps }: { number: string; title: string; sourceAt: string; model: string; origins: string[]; steps: number; maximum: number; disabled?: boolean; onSteps: (n: number) => void }) {
  return <div className="finding-stack"><Space wrap><code>{number}</code><strong>{title}</strong></Space><Descriptions size="small" column={1} items={[
    { key: 'source', label: '验证依据', children: `${verificationTime(sourceAt)} 保存的候选` },
    { key: 'model', label: '任务模型', children: model },
    { key: 'origins', label: '授权范围', children: <div className="finding-text">{origins.join('\n')}</div> },
  ]} /><label htmlFor="verification-steps">本次最多决策步数</label><InputNumber id="verification-steps" min={1} max={maximum} precision={0} value={steps} disabled={disabled} onChange={n => { if (n !== null) onSteps(n) }} /></div>
}
export function VerificationList({ items, busy, error, pager, onOpen }: { items: VerificationSummary[]; busy?: boolean; error?: string; pager: SkillPagerProps; onOpen: (v: VerificationSummary) => void }) {
  return <section className="finding-stack" aria-label="独立验证记录">{error && <Alert type="error" showIcon title={error} />}<Table<VerificationSummary> rowKey="id" size="small" pagination={false} loading={busy} dataSource={items} scroll={{ x: 560 }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={error ? '验证记录未取得' : '暂无独立验证'} /> }} columns={[
    { title: '发起 / 依据时点', render: (_, v) => <div><time>{verificationTime(v.created_at)}</time><div>依据 {verificationTime(v.source_created_at)}</div></div> },
    { title: '执行 / 结论', render: (_, v) => <Space wrap><Tag>{v.end_reason === 'budget_exhausted' ? '预算结束' : verificationStateLabels[v.status] ?? v.status}</Tag>{v.pause_requested && v.status !== 'paused' && <Tag>已请求暂停</Tag>}<span>{v.has_result && v.verdict ? verificationVerdicts[v.verdict] : '未形成结论'}</span></Space> },
    { title: '操作', width: 112, render: (_, v) => <Button data-verification-id={v.id} onClick={() => onOpen(v)}>查看验证</Button> },
  ]} /><SkillPager {...pager} busy={busy} /></section>
}
export function VerificationResultView({ result, nextStep, error, busy, controls, onEvidence }: { result?: VerificationResult | null; nextStep?: string; error?: string; busy?: boolean; controls?: ReactNode; onEvidence: (i: number) => void }) {
  if (busy && !result) return <Skeleton active />
  return <section className="finding-stack" aria-label="独立验证结果">{controls}{error && <Alert type="error" showIcon title={error} />}{!result ? <><Tag>未形成结论</Tag>{nextStep && <p className="finding-text">{nextStep}</p>}</> : <>
    <div className="verification-summary workspace-reading"><Space wrap><strong>{verificationVerdicts[result.conclusion.verdict]}</strong><Tag>模型判读 · 非自动确认</Tag><time>{verificationTime(result.created_at)}</time></Space><p className="finding-text">{result.conclusion.summary}</p></div>
    <div className="verification-facts workspace-reading"><section><h3>验证方法</h3><p className="finding-text">{result.conclusion.method}</p></section><section><h3>实际前提</h3><p className="finding-text">{result.conclusion.preconditions || '未补充'}</p></section><section><h3>限制</h3><p className="finding-text">{result.conclusion.limitations || '未补充'}</p></section><section><h3>本次新证据</h3><Space wrap>{result.evidence.map((e, i) => <Button key={`${e.kind}:${e.id}`} onClick={() => onEvidence(i)}>{e.relation === 'supporting' ? '支持材料' : e.relation === 'counterevidence' ? '反证材料' : '限制材料'} {i + 1}</Button>)}</Space></section></div>
    <GridFold title="验证结果来源"><div className="finding-source"><span>尝试 <code>{result.attempt_id}</code></span><span>调用 <code>{result.call_id}</code></span></div></GridFold>
  </>}</section>
}
