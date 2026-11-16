import type { Action, Attempt, Observation, Snapshot } from './types.ts'

export type ActivityKind = 'attempt' | 'action' | 'observation'
export interface ActivityItem {
  kind: ActivityKind
  id: string
  at: string
  attempt?: Attempt
  action?: Action
  observation?: Observation
}

/**
 * 仅排序和筛选服务端已有记录；不按时间邻近推断执行因果。
 * Attempt 的归属通过各自结构化 attempt_id 在活动详情中直接核对。
 */
export function buildActivityItems(snapshot: Snapshot | null): ActivityItem[] {
  if (!snapshot) return []
  const items: ActivityItem[] = [
    ...snapshot.attempts.map(attempt => ({ kind: 'attempt' as const, id: attempt.id, at: attempt.started_at, attempt })),
    ...snapshot.actions.map(action => ({ kind: 'action' as const, id: action.id, at: action.created_at, action })),
    ...snapshot.observations.map(observation => ({ kind: 'observation' as const, id: observation.id, at: observation.created_at, observation })),
  ]
  return items.sort((a, b) => {
    const at = Date.parse(a.at), bt = Date.parse(b.at)
    if (Number.isFinite(at) && Number.isFinite(bt) && at !== bt) return bt - at
    if (a.at !== b.at) return b.at.localeCompare(a.at)
    if (a.kind !== b.kind) return a.kind.localeCompare(b.kind)
    return a.id.localeCompare(b.id)
  })
}

export type ActivityFilter = 'all' | ActivityKind

export function filterActivityItems(items: ActivityItem[], filter: ActivityFilter): ActivityItem[] {
  return filter === 'all' ? items : items.filter(item => item.kind === filter)
}

/** 每次快照重新核验；重复 ID、错归属和已删除记录都不能留在证据面板。 */
export function resolveActivityAction(snapshot: Snapshot | null, selection: { missionID: string; threadID: string; actionID: string } | null): Action | null {
  if (!snapshot || !selection || snapshot.mission.id !== selection.missionID || snapshot.thread.id !== selection.threadID || snapshot.mission.id !== snapshot.thread.mission_id) return null
  const matches = snapshot.actions.filter(action => action.id === selection.actionID && action.thread_id === selection.threadID)
  return matches.length === 1 ? matches[0] : null
}
