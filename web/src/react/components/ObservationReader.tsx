import { Button, Empty, Space, Tag, Typography } from 'antd'
import { EyeOutlined } from '@ant-design/icons'
import type { Action, Mission, Observation, Snapshot } from '../types.ts'
import { formatDateTime, observationKindText } from '../types.ts'
import { actionReceiptSummary, createRawObservationSelection, resolveRawObservation } from '../observations.ts'
import type { RawObservationSelection } from '../observations.ts'
import type { InspectingActionSelection } from '../workspace.ts'
import { createObservationActionSelection, resolveInspectingAction } from '../workspace.ts'

export function ObservationList({ mission, snapshot, observations, rawSelection, onToggleRaw, onInspect }: {
  mission: Mission
  snapshot: Snapshot
  observations: Observation[]
  rawSelection: RawObservationSelection | null
  onToggleRaw: (selection: RawObservationSelection) => void
  onInspect: (selection: InspectingActionSelection, sourceSnapshot: Snapshot) => void
}) {
  if (observations.length === 0) return <Empty description="尚无观察记录" />
  const opened = resolveRawObservation(rawSelection, mission, snapshot.thread.id, snapshot)
  const idCounts = new Map<string, number>()
  for (const obs of snapshot.observations) idCounts.set(obs.id, (idCounts.get(obs.id) ?? 0) + 1)
  return <div className="ws-record-list">{observations.map((observation, index) => {
    const link = createObservationActionSelection(observation, mission, snapshot.thread.id, snapshot)
    const linkedAction = resolveInspectingAction(link, mission, snapshot.thread.id, snapshot)
    const raw = createRawObservationSelection(observation, mission, snapshot.thread.id, snapshot)
    const expanded = opened === observation
    return <div key={JSON.stringify([observation.id, idCounts.get(observation.id) === 1 ? null : index])} className="ws-record-card">
      <div className="ws-record-header">
        <Space size="small" wrap><Tag>{observationKindText(observation.kind)}</Tag><Typography.Text type="secondary">{formatDateTime(observation.created_at)}</Typography.Text></Space>
        {link ? <Button icon={<EyeOutlined />} onClick={() => onInspect(link, snapshot)}>查看关联回执</Button> : observation.action_id ? <Typography.Text type="secondary">关联回执不可用</Typography.Text> : null}
      </div>
      {observation.kind === 'action_result' ? <div className="ws-evidence-block">
        <div className="ws-record-summary">
          {linkedAction && <Space size="small" wrap><Typography.Text type="secondary">关联回执摘要</Typography.Text>{actionReceiptSummary(linkedAction).map(field => <Tag key={field}>{field}</Tag>)}</Space>}
          {raw && <Button aria-expanded={expanded} aria-label={expanded ? '收起观察原文' : '展开观察原文（可能含敏感值）'} style={{ whiteSpace: 'normal', height: 'auto', minHeight: 32, maxWidth: '100%' }} onClick={() => onToggleRaw(raw)}>{expanded ? '收起观察原文' : '展开观察原文（可能含敏感值）'}</Button>}
        </div>
        {expanded && <><span className="ws-observation-raw-label">未脱敏观察原文；与关联回执独立。</span><pre className="ws-code-pre">{observation.content}</pre></>}
      </div> : <pre className="ws-code-pre">{observation.content}</pre>}
    </div>
  })}</div>
}

export function LinkedActionObservation({ action, mission, snapshot, rawSelection, onToggleRaw, onInspect }: {
  action: Action
  mission: Mission
  snapshot: Snapshot
  rawSelection: RawObservationSelection | null
  onToggleRaw: (selection: RawObservationSelection) => void
  onInspect: (selection: InspectingActionSelection, sourceSnapshot: Snapshot) => void
}) {
  const observations = snapshot.observations.filter(observation => observation.kind === 'action_result' && observation.action_id === action.id && observation.thread_id === snapshot.thread.id)
  if (observations.length === 0) return null
  return <ObservationList mission={mission} snapshot={snapshot} observations={observations} rawSelection={rawSelection} onToggleRaw={onToggleRaw} onInspect={onInspect} />
}
