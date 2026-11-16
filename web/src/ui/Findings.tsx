import type { ReactNode } from 'react'
import { Alert, Button, Empty, Skeleton, Space, Table, Tabs, Tag } from 'antd'
import { GridFold } from './SurfaceControls.tsx'
import { SkillPager, type SkillPagerProps } from './SkillRecords.tsx'
import './findings.css'

export interface FindingReference { kind: 'action' | 'observation'; id: string; relation: 'supporting' | 'counterevidence' | 'limitation' }
export interface FindingContent { title: string; description: string; steps: { text: string; evidence_indices: number[] }[]; remediation: string; limitations: string; evidence: FindingReference[] }
export interface FindingEvidence extends FindingReference {
  thread_id: string; attempt_id: string; action_id: string; created_at: string; captured_at: string; content_sha256: string
  state: string; action_kind: string; request_hash: string; has_result: boolean; truncated: boolean; effect_unknown: boolean
}
export interface FindingSummary { id: string; mission_id: string; thread_id: string; number: string; title: string; status: 'candidate'; revision: number; revision_id: string; created_at: string; updated_at: string }
export interface FindingRevision {
  id: string; finding_id: string; mission_id: string; thread_id: string; number: string; revision: number; status: 'candidate'; created_at: string
  attempt_id: string; call_id: string; event_id: string; call_started_at: string; call_finished_at: string; content_sha256: string
  content: FindingContent; evidence: FindingEvidence[]
}
export interface FindingHistoryRow { id: string; finding_id: string; mission_id: string; thread_id: string; revision: number; title: string; created_at: string; attempt_id: string; call_id: string; event_id: string }
export const findingRelationLabel = { supporting: '支持材料', counterevidence: '反证', limitation: '限制材料' }
const evidenceState: Record<string, string> = { pending: '待审批 · 未发送', approved: '已放行 · 未发送', running: '执行中', succeeded: '动作成功', failed: '动作失败', rejected: '已拒绝 · 未发送', outcome_unknown: '效果未知', model_observation: '模型观察', action_result: '动作结果观察', operator_note: '人工观察', counterevidence: '反证观察', contradiction: '矛盾观察', refutation: '反驳观察', other: '观察' }
const dateText = (value: string) => { const d = new Date(value); return Number.isFinite(d.valueOf()) ? d.toLocaleString('zh-CN', { hour12: false }) : value }

// 纯受控展示；请求、归属、刷新和认证由正式入口负责。没有执行/确认/编辑操作。
export function FindingList({ items, busy, error, pager, onOpen, compact = false, selected }: { items: FindingSummary[]; busy?: boolean; error?: string; pager: SkillPagerProps; onOpen: (row: FindingSummary) => void; compact?: boolean; selected?: string }) {
  const empty = <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={error ? '尚未取得候选记录' : '暂无候选发现'} />
  return <section className="finding-stack" aria-label="候选发现列表" aria-busy={busy}>
    {error && <Alert type="error" showIcon title={error} />}
    {compact ? <nav className="finding-navigation" aria-label="选择候选">{busy && !items.length ? <Skeleton active /> : !items.length ? empty : items.map(row => <button type="button" key={row.id} data-finding-id={row.id} aria-current={selected === row.id ? 'true' : undefined} onClick={() => onOpen(row)}>
      <code>{row.number}</code><strong>{row.title}</strong><span>候选 · 未确认</span><time>{dateText(row.updated_at)}</time>
    </button>)}</nav> : <Table<FindingSummary> size="small" rowKey="id" dataSource={items} loading={busy} pagination={false} scroll={{ x: 600 }} tableLayout="fixed" locale={{ emptyText: empty }} columns={[
      { title: '编号 / 标题', render: (_, row) => <div className="finding-title"><code>{row.number}</code><Button type="link" data-finding-id={row.id} className="finding-open" onClick={() => onOpen(row)}>{row.title}</Button></div> },
      { title: '状态', width: 130, render: () => <Tag>候选 · 未确认</Tag> },
      { title: '最近保存', width: 180, render: (_, row) => <time>{dateText(row.updated_at)}</time> },
    ]} />}
    {!compact && <SkillPager {...pager} busy={busy} />}
  </section>
}

export type FindingSection = 'overview' | 'verification' | 'evidence' | 'history'
export function FindingDetail({ value, busy, error, history, section = 'overview', onSectionChange, onEvidence, actions, verification, historical = false }: { value?: FindingRevision | null; busy?: boolean; error?: string; history?: ReactNode; section?: FindingSection; onSectionChange?: (section: FindingSection) => void; onEvidence: (index: number) => void; actions?: ReactNode; verification?: ReactNode; historical?: boolean }) {
  if (!value) return error ? <Alert type="error" showIcon title={error} /> : <Skeleton active />
  const content = value.content
  const evidenceButton = (index: number) => <Button size="small" key={index} onClick={() => onEvidence(index)}>{value.evidence[index]?.kind === 'action' && value.evidence[index]?.action_kind === 'http' ? '查看流量' : '查看材料'} {index + 1}</Button>
  const overview = <div className="finding-document workspace-reading">
    <section className="finding-section"><h3>漏洞描述</h3><p>{content.description}</p></section>
    <section className="finding-section"><h3>复现步骤</h3><ol className="finding-steps">{content.steps.map((step, index) => <li key={index}><p>{step.text}</p><Space wrap size={[6, 4]}>{step.evidence_indices.map(evidenceButton)}</Space></li>)}</ol></section>
    <section className="finding-section"><h3>修复建议</h3><p>{content.remediation || '尚未提出修复建议'}</p></section>
    {content.limitations && <aside className="finding-limitations"><h3>前提与未验证限制</h3><p>{content.limitations}</p></aside>}
  </div>
  const evidence = <section className="finding-stack workspace-reading" aria-label="候选引用证据">{content.limitations && <p className="finding-text">{content.limitations}</p>}{value.evidence.map((e, i) => <div className="finding-evidence" key={`${e.kind}:${e.id}`}><div className="finding-heading"><Space wrap><Tag color={e.relation === 'counterevidence' ? 'warning' : undefined}>{findingRelationLabel[e.relation]}</Tag><span>{evidenceState[e.state] ?? '观察'}</span>{e.truncated && <Tag>捕获不完整</Tag>}{e.effect_unknown && <Tag color="warning">效果未知</Tag>}</Space>{evidenceButton(i)}</div><time>材料产生：{dateText(e.created_at)} · 引用时点：{dateText(e.captured_at)}</time><code>{e.id}</code></div>)}</section>
  const historyPanel = <div className="finding-document workspace-reading"><section className="finding-stack"><h3>发现时间线与历史</h3>{history}</section><section className="finding-stack"><h3>当前阅读内容的形成来源</h3><div className="finding-source"><span>执行尝试 <code>{value.attempt_id}</code></span><span>模型调用 <code>{value.call_id}</code></span><time>{dateText(value.call_started_at)} → {dateText(value.call_finished_at)}</time><span>关联事件 <code>{value.event_id}</code></span><span>内容 SHA256 <code>{value.content_sha256}</code></span></div></section></div>
  return <article className="finding-stack" aria-label="候选发现详情" aria-busy={busy}>
    <header className="finding-heading finding-detail-heading"><div className="finding-title"><Space wrap><code>{value.number}</code><Tag>候选 · 未确认</Tag>{historical && <Tag>历史记录</Tag>}</Space><h2>{content.title}</h2><time>保存于 {dateText(value.created_at)}</time></div><Space wrap>{actions}</Space></header>
    {error && <Alert type="error" showIcon title={error} />}
    <Tabs aria-label="候选内容" activeKey={section} onChange={key => onSectionChange?.(key as FindingSection)} destroyOnHidden items={[
      { key: 'overview', label: '漏洞详情', children: overview },
      ...(verification ? [{ key: 'verification', label: '独立验证', children: verification }] : []),
      { key: 'evidence', label: `证据 (${value.evidence.length})`, children: evidence },
      { key: 'history', label: '历史与来源', children: historyPanel },
    ]} />
  </article>
}

export function FindingHistory({ items, busy, error, pager, selected, onOpen }: { items: FindingHistoryRow[]; busy?: boolean; error?: string; pager: SkillPagerProps; selected?: string; onOpen: (item: FindingHistoryRow) => void }) {
  return <section className="finding-stack" aria-label="发现保存历史">{error && <Alert type="error" showIcon title={error} />}{busy && !items.length ? <Skeleton active /> : !items.length && !error ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无历史记录" /> : <ol className="finding-history">{items.map(row => <li key={row.id}><div className="finding-heading"><Space wrap><time>{dateText(row.created_at)}</time><Tag>{row.revision === 1 ? '形成候选' : '更新候选'}</Tag></Space><Button disabled={selected === row.id} onClick={() => onOpen(row)}>{selected === row.id ? '正在阅读' : '查看当时内容'}</Button></div><p>{row.title}</p><GridFold title="关联研究记录"><div className="finding-source"><span>尝试 <code>{row.attempt_id}</code></span><span>调用 <code>{row.call_id}</code></span><span>事件 <code>{row.event_id}</code></span></div></GridFold></li>)}</ol>}<SkillPager {...pager} busy={busy} /></section>
}
