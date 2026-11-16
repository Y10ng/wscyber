import type { Action, Mission, Observation, Snapshot } from './types.ts'

// 仅输出固定标签或经过数值校验的字段，不使用可回退任意原值的 statusText。
const receiptStatuses = new Map<string, string>([
  ['pending', '待审批'],
  ['approved', '已放行 · 待执行'],
  ['running', '执行中'],
  ['succeeded', '执行成功'],
  ['failed', '执行失败'],
  ['rejected', '已拒绝'],
  ['outcome_unknown', '效果未知'],
])

export function actionReceiptSummary(action: Action): string[] {
  const fields: string[] = []
  const status = receiptStatuses.get(action.status)
  if (status) fields.push(status)
  const kind = action.input?.kind
  if (kind === 'http') fields.push('受控 HTTP')
  if (kind === 'local_command') fields.push('工具命令')

  const result = action.result
  const code = result?.status_code
  if (kind === 'http' && typeof code === 'number' && Number.isInteger(code) && code >= 100 && code <= 599) {
    fields.push(`HTTP ${code}`)
  }
  const exitCode = result?.exit_code
  if (kind === 'local_command' && typeof exitCode === 'number' && Number.isSafeInteger(exitCode)) {
    fields.push(`退出码 ${exitCode}`)
  }
  const duration = result?.duration_ms
  if (typeof duration === 'number' && Number.isSafeInteger(duration) && duration >= 0) {
    fields.push(`耗时 ${duration} ms`)
  }
  if (result?.truncated === true) fields.push('已截断')
  if (result?.effect_unknown === true && action.status !== 'outcome_unknown') fields.push('效果未知')
  return fields
}

export interface RawObservationSelection {
  missionId: string
  threadId: string
  observationId: string
  kind: 'action_result'
  actionId: string | undefined
  // 仅内存中比较观察版本，绝不用于 DOM 属性、key、URL 或日志。
  content: string
}

export function createRawObservationSelection(
  observation: Observation,
  mission: Mission | null,
  selectedThreadId: string | null,
  snapshot: Snapshot | null,
): RawObservationSelection | null {
  if (observation.kind !== 'action_result' || !snapshot?.observations.includes(observation)) return null
  const selection: RawObservationSelection = {
    missionId: snapshot.mission.id,
    threadId: observation.thread_id,
    observationId: observation.id,
    kind: observation.kind,
    actionId: observation.action_id,
    content: observation.content,
  }
  return resolveRawObservation(selection, mission, selectedThreadId, snapshot) ? selection : null
}

export function resolveRawObservation(
  selection: RawObservationSelection | null,
  mission: Mission | null,
  selectedThreadId: string | null,
  snapshot: Snapshot | null,
): Observation | null {
  if (!selection || !mission || !selectedThreadId || !snapshot || !selection.observationId) return null
  if (
    selection.missionId !== mission.id ||
    selection.threadId !== selectedThreadId ||
    snapshot.mission.id !== mission.id ||
    snapshot.thread.id !== selectedThreadId ||
    snapshot.thread.mission_id !== mission.id
  ) return null

  // 原文归属与关联动作是否可读取独立，但同样拒绝跨归属及重复观察 ID。
  const records = snapshot.observations.filter((observation) => observation.id === selection.observationId)
  if (records.length !== 1) return null
  const observation = records[0]!
  return observation.thread_id === selectedThreadId &&
    selection.kind === 'action_result' && observation.kind === selection.kind &&
    observation.action_id === selection.actionId &&
    typeof observation.content === 'string' && observation.content === selection.content
    ? observation : null
}

export interface RawObservationContext {
  mission: Mission | null
  selectedThreadId: string | null
  snapshot: Snapshot | null
  available: boolean
}

// 上下文对象身份是事件代次；普通刷新可保留已展开的同一原文，但不能接收旧快照点击。
export function isCurrentRawObservationEvent(
  selection: RawObservationSelection,
  source: RawObservationContext,
  current: RawObservationContext | null,
): boolean {
  return source === current && source.available &&
    resolveRawObservation(selection, source.mission, source.selectedThreadId, source.snapshot) !== null
}
