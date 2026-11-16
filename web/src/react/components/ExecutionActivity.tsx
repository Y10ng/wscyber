import { useMemo, useRef, useState } from 'react'
import { Button, Empty, Segmented, Tag } from 'antd'
import { DownOutlined, EyeOutlined, RightOutlined } from '@ant-design/icons'
import type { Action, Attempt, Mission, Observation, Snapshot } from '../types.ts'
import { actionSummaryTitle, formatDateTime, observationKindText, statusBadgeColor, statusText } from '../types.ts'
import { buildActivityItems, filterActivityItems, type ActivityFilter } from '../activity.ts'
import type { InspectingActionSelection } from '../workspace.ts'
import type { RawObservationSelection } from '../observations.ts'
import { ActionEvidencePanel } from './ActionEvidenceDrawer.tsx'
import { LinkedActionObservation, ObservationList } from './ObservationReader.tsx'

export function ExecutionActivity({ mission, snapshot, rawSelection, onToggleRaw, onInspect }: {
  mission: Mission
  snapshot: Snapshot
  rawSelection: RawObservationSelection | null
  onToggleRaw: (selection: RawObservationSelection) => void
  onInspect: (selection: InspectingActionSelection, sourceSnapshot: Snapshot) => void
}) {
  const [filter, setFilter] = useState<ActivityFilter>('all')
  const [expanded, setExpanded] = useState<{ kind: 'attempt' | 'action' | 'observation'; id: string } | null>(null)
  const evidenceTrigger = useRef<HTMLElement | null>(null)
  const items = useMemo(() => buildActivityItems(snapshot), [snapshot])
  const visibleItems = filterActivityItems(items, filter)
  const counts = { all: items.length, attempt: snapshot.attempts.length, action: snapshot.actions.length, observation: snapshot.observations.length }

  const toggle = (kind: 'attempt' | 'action' | 'observation', id: string) => {
    if (expanded?.id === id && expanded.kind === kind) {
      setExpanded(null)
      queueMicrotask(() => { if (evidenceTrigger.current?.isConnected) evidenceTrigger.current.focus({ preventScroll: true }) })
      return
    }
    evidenceTrigger.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    setExpanded({ kind, id })
  }
  const inspect = (selection: InspectingActionSelection, source: Snapshot) => {
    setExpanded(null)
    onInspect(selection, source)
  }

  const renderRow = (item: (typeof items)[number]) => {
    const expandedHere = expanded?.kind === item.kind && expanded.id === item.id

    if (item.kind === 'attempt' && item.attempt) {
      const attempt: Attempt = item.attempt
      return <div key={`attempt:${item.id}`}>
        <div className="ws-activity-tr" role="row">
          <div className="ws-activity-td ws-activity-time" role="cell"><time dateTime={attempt.started_at}>{formatDateTime(attempt.started_at)}</time></div>
          <div className="ws-activity-td ws-activity-type" role="cell"><span className="ws-activity-type-label">执行尝试</span></div>
          <div className="ws-activity-td ws-activity-summary" role="cell">{attempt.summary && <span className="ws-activity-reason">{attempt.summary}</span>}{attempt.error && <span className="ws-activity-error">执行尝试失败；展开查看记录。</span>}</div>
          <div className="ws-activity-td ws-activity-status" role="cell"><Tag color={statusBadgeColor(attempt.status)}>{statusText(attempt.status)}</Tag></div>
          <div className="ws-activity-td ws-activity-op" role="cell"><Button type="text" className="ws-activity-expand" aria-expanded={expandedHere} icon={expandedHere ? <DownOutlined /> : <RightOutlined />} aria-label={expandedHere ? '收起尝试详情' : '展开尝试详情'} onClick={() => toggle('attempt', attempt.id)} /></div>
        </div>
        {expandedHere && <div className="ws-activity-expand-row">
          <div className="ws-activity-detail">
            <div className="ws-activity-meta">开始于 {formatDateTime(attempt.started_at)} · {attempt.ended_at ? `结束于 ${formatDateTime(attempt.ended_at)}` : '尚未结束'}</div>
            {attempt.error && <p className="ws-activity-error ws-pre-wrap">{attempt.error}</p>}
            <div className="ws-activity-meta">Attempt <code>{attempt.id}</code></div>
          </div>
        </div>}
      </div>
    }

    if (item.kind === 'action' && item.action) {
      const action: Action = item.action
      return <div key={`action:${item.id}`}>
        <div className="ws-activity-tr" role="row">
          <div className="ws-activity-td ws-activity-time" role="cell"><time dateTime={action.created_at}>{formatDateTime(action.created_at)}</time></div>
          <div className="ws-activity-td ws-activity-type" role="cell"><span className="ws-activity-type-label">{action.input.kind === 'http' ? 'HTTP 请求' : '工具命令'}</span></div>
          <div className="ws-activity-td ws-activity-summary" role="cell">
            <code className="ws-activity-command">{actionSummaryTitle(action)}</code>
            {action.decision_reason && <span className="ws-activity-reason">决策说明：{action.decision_reason}</span>}
            {action.error && <span className="ws-activity-error">执行说明：{action.error}</span>}
          </div>
          <div className="ws-activity-td ws-activity-status" role="cell">
            <div className="ws-activity-status-cell"><Tag color={statusBadgeColor(action.status)}>{statusText(action.status)}</Tag>{action.result?.status_code ? <code className="ws-http-status">{action.result.status_code}</code> : null}{action.result ? <span className="ws-muted">{action.result.duration_ms} ms</span> : null}</div>
          </div>
          <div className="ws-activity-td ws-activity-op" role="cell"><Button className="ws-activity-expand" aria-expanded={expandedHere} icon={expandedHere ? <DownOutlined /> : <RightOutlined />} onClick={() => toggle('action', action.id)}>{expandedHere ? '收起证据' : '查看证据'}</Button></div>
        </div>
        {expandedHere && <div className="ws-activity-expand-row">
          <LinkedActionObservation action={action} mission={mission} snapshot={snapshot} rawSelection={rawSelection} onToggleRaw={onToggleRaw} onInspect={inspect} />
          <ActionEvidencePanel action={action} mission={mission} thread={snapshot.thread} onClose={() => toggle('action', action.id)} />
        </div>}
      </div>
    }

    const observation: Observation = item.observation!
    return <div key={`observation:${item.id}`}>
      <div className="ws-activity-tr" role="row">
        <div className="ws-activity-td ws-activity-time" role="cell"><time dateTime={observation.created_at}>{formatDateTime(observation.created_at)}</time></div>
        <div className="ws-activity-td ws-activity-type" role="cell"><span className="ws-activity-type-label">{observationKindText(observation.kind)}</span></div>
        <div className="ws-activity-td ws-activity-summary" role="cell">{observation.kind === 'action_result' ? <span className="ws-activity-reason">动作结果已保存，原文按需展开。</span> : <span className="ws-activity-reason ws-pre-wrap">{observation.content}</span>}</div>
        <div className="ws-activity-td ws-activity-status" role="cell"><span className="ws-muted">观察记录</span></div>
        <div className="ws-activity-td ws-activity-op" role="cell"><Button type="text" className="ws-activity-expand" aria-expanded={expandedHere} icon={expandedHere ? <DownOutlined /> : <EyeOutlined />} aria-label={expandedHere ? '收起观察详情' : '查看观察详情'} onClick={() => toggle('observation', observation.id)} /></div>
      </div>
      {expandedHere && <div className="ws-activity-expand-row"><ObservationList mission={mission} snapshot={snapshot} observations={[observation]} rawSelection={rawSelection} onToggleRaw={onToggleRaw} onInspect={inspect} /></div>}
    </div>
  }

  // 关联结果在动作内展示，避免同一证据在活动流中重复；观察筛选保留全部独立记录。
  const renderedKeys = new Set<string>()
  return <section className="ws-execution-activity" aria-label="执行活动">
    <div className="ws-activity-toolbar">
      <div className="ws-activity-heading"><h2>执行活动</h2><span>{counts.all}</span></div>
      <Segmented<ActivityFilter> aria-label="筛选执行活动" value={filter} onChange={value => { setFilter(value); setExpanded(null) }} options={[
        { value: 'all', label: `全部 ${counts.all}` },
        { value: 'action', label: `动作 ${counts.action}` },
        { value: 'observation', label: `观察 ${counts.observation}` },
        { value: 'attempt', label: `尝试 ${counts.attempt}` },
      ]} />
    </div>
    {!visibleItems.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚无执行活动" /> : <div className="ws-activity-table" role="table" aria-label="执行活动记录">
      <div className="ws-activity-head-row" role="row">
        <div role="columnheader" className="ws-activity-col-time">时间</div>
        <div role="columnheader" className="ws-activity-col-type">类型</div>
        <div role="columnheader" className="ws-activity-col-summary">内容</div>
        <div role="columnheader" className="ws-activity-col-status">状态</div>
        <div role="columnheader" className="ws-activity-col-op">操作</div>
      </div>
      {visibleItems.map(item => {
        if (filter === 'all' && item.kind === 'observation' && item.observation?.kind === 'action_result' && item.observation.action_id && snapshot.actions.some(action => action.id === item.observation?.action_id)) return null
        const key = `${item.kind}:${item.id}`
        if (renderedKeys.has(key)) return null
        renderedKeys.add(key)
        return renderRow(item)
      })}
    </div>}
  </section>
}
