import type { Action, Mission, Observation, Snapshot, Thread } from './types.ts'

export interface WorkspaceRouteTarget {
  missionId: string
  threadId: string | null
}

export class WorkspaceRequestTracker {
  private generation = 0
  private activeMissionId: string | null = null
  private activeThreadId: string | null = null

  public begin(missionId: string, threadId: string | null): number {
    this.generation += 1
    this.activeMissionId = missionId
    this.activeThreadId = threadId
    return this.generation
  }

  public invalidate(): number {
    this.generation += 1
    this.activeMissionId = null
    this.activeThreadId = null
    return this.generation
  }

  public isCurrent(
    gen: number,
    missionId: string,
    threadId: string | null,
  ): boolean {
    return (
      gen === this.generation &&
      this.activeMissionId === missionId &&
      this.activeThreadId === threadId
    )
  }

  public get currentGeneration(): number {
    return this.generation
  }
}

export function resolveWorkspaceThreadTarget(
  requestedThreadId: string | null,
  threads: Thread[],
): { targetThreadId: string | null; isExplicit: boolean } {
  if (requestedThreadId !== null) {
    // 显式指定 threadId 时禁止静默回退到其他研究方向
    return { targetThreadId: requestedThreadId, isExplicit: true }
  }
  return {
    targetThreadId: threads[0]?.id ?? null,
    isExplicit: false,
  }
}

export function selectMatchedSnapshot(
  activeSnapshot: Snapshot | null,
  missionId: string,
  effectiveThreadId: string | null,
): Snapshot | null {
  if (!activeSnapshot || !effectiveThreadId) {
    return null
  }
  if (activeSnapshot.mission.id !== missionId) {
    return null
  }
  if (activeSnapshot.thread.id !== effectiveThreadId) {
    return null
  }
  return activeSnapshot
}

export interface InspectingActionSelection {
  missionId: string
  threadId: string
  actionId: string
  // 观察入口额外绑定来源；刷新只能更新同一关联的回执，不能自动换到另一动作。
  observationId?: string
}

export function createObservationActionSelection(
  observation: Observation,
  mission: Mission | null,
  selectedThreadId: string | null,
  snapshot: Snapshot | null,
): InspectingActionSelection | null {
  // 只接受本次快照里的观察对象；旧闭包不能仅凭相同 ID 借用新观察。
  if (!observation.id || !observation.action_id || !snapshot?.observations.includes(observation)) {
    return null
  }
  const selection: InspectingActionSelection = {
    missionId: snapshot.mission.id,
    threadId: observation.thread_id,
    actionId: observation.action_id,
    observationId: observation.id,
  }
  return resolveInspectingAction(selection, mission, selectedThreadId, snapshot) ? selection : null
}

export function resolveInspectingAction(
  selection: InspectingActionSelection | null,
  mission: Mission | null,
  selectedThreadId: string | null,
  snapshot: Snapshot | null,
): Action | null {
  if (!selection || !mission || !selectedThreadId || !snapshot) {
    return null
  }
  if (
    selection.missionId !== mission.id ||
    selection.threadId !== selectedThreadId ||
    snapshot.mission.id !== mission.id ||
    snapshot.thread.id !== selectedThreadId ||
    snapshot.thread.mission_id !== mission.id ||
    !selection.actionId
  ) {
    return null
  }
  if (selection.observationId !== undefined) {
    const observations = snapshot.observations.filter((obs) => obs.id === selection.observationId)
    if (
      !selection.observationId ||
      observations.length !== 1 ||
      observations[0]!.thread_id !== selectedThreadId ||
      observations[0]!.action_id !== selection.actionId
    ) {
      return null
    }
  }
  // 不先按归属过滤：重复 ID 中即使只有一条归属正确，也不能任意选第一条。
  const actions = snapshot.actions.filter((act) => act.id === selection.actionId)
  return actions.length === 1 && actions[0]!.thread_id === selectedThreadId ? actions[0]! : null
}

export interface WorkspacePollState {
  authenticated: boolean
  isWorkspaceRoute: boolean
  documentHidden: boolean
  pollCount: number
  maxPollCount?: number
  terminalErrorStatus: number | null
  consecutiveFailures: number
  snapshot: Snapshot | null
  threads: Thread[]
}

export const DEFAULT_MAX_WORKSPACE_POLLS = 120
export const ACTIVE_POLL_INTERVAL_MS = 1200
export const IDLE_POLL_INTERVAL_MS = 5000

export function isThreadActivelyProgressing(
  snapshot: Snapshot | null,
  threads: Thread[],
): boolean {
  if (snapshot) {
    const { thread, actions, attempts } = snapshot
    if (thread.status === 'running') return true
    if (thread.status === 'ready' && thread.auto_run) return true
    if (attempts.some((att) => att.status === 'running')) return true
    if (
      actions.some(
        (act) =>
          act.status === 'running' ||
          (act.status === 'approved' && thread.status !== 'paused'),
      )
    ) {
      return true
    }
    return false
  }
  return threads.some(
    (t) => t.status === 'running' || (t.status === 'ready' && t.auto_run),
  )
}

export function computeWorkspacePollDelayMs(
  state: WorkspacePollState,
): number | null {
  const maxPollCount = state.maxPollCount ?? DEFAULT_MAX_WORKSPACE_POLLS
  if (!state.authenticated || !state.isWorkspaceRoute || state.documentHidden) {
    return null
  }
  if (
    state.terminalErrorStatus === 401 ||
    state.terminalErrorStatus === 403 ||
    state.terminalErrorStatus === 404
  ) {
    return null
  }
  if (state.pollCount >= maxPollCount) {
    return null
  }
  if (state.consecutiveFailures >= 5) {
    return null
  }

  if (isThreadActivelyProgressing(state.snapshot, state.threads)) {
    const backoffFactor = Math.min(state.consecutiveFailures, 3)
    return ACTIVE_POLL_INTERVAL_MS * (backoffFactor + 1)
  }

  const thread = state.snapshot?.thread
  if (!thread) {
    // 刚进入项目且方向尚未出现在列表中时，短轮询最多 10 次
    return state.pollCount < 10 ? ACTIVE_POLL_INTERVAL_MS : null
  }

  if (thread.status === 'concluded' || thread.status === 'completed') {
    // 方向已结束且无在途动作/尝试时立即停止轮询
    return null
  }

  // paused / blocked / 非自动 ready 状态降频轮询（最多 20 次）
  if (state.pollCount < 20) {
    return IDLE_POLL_INTERVAL_MS
  }

  return null
}

const SENSITIVE_HEADER_NAMES = new Set([
  'authorization',
  'proxy-authorization',
  'cookie',
  'set-cookie',
  'x-api-key',
  'x-auth-token',
  'api-key',
])

export function isSensitiveHeaderName(name: string): boolean {
  return SENSITIVE_HEADER_NAMES.has(name.trim().toLowerCase())
}

export function formatHeadersForDisplay(
  headers?: Record<string, string | string[]>,
  revealSensitive = false,
): { text: string; hasSensitive: boolean } {
  if (!headers) {
    return { text: '', hasSensitive: false }
  }
  let hasSensitive = false
  const lines = Object.entries(headers).flatMap(([key, val]) => {
    const sensitive = isSensitiveHeaderName(key)
    if (sensitive) {
      hasSensitive = true
    }
    const items = Array.isArray(val) ? val : [val]
    return items.map((item) =>
      sensitive && !revealSensitive ? `${key}: [已隐藏]` : `${key}: ${item}`,
    )
  })
  return {
    text: lines.join('\n'),
    hasSensitive,
  }
}

