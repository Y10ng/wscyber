export interface SystemInfo {
  name: string
  version: string
  model_configured: boolean
  model: string
  sandbox_image: string
  sandbox_runtime: string
  network_mode: string
  max_parallel: number
}

export interface Mission {
  id: string
  title: string
  origin: string
  goal: string
  allowed_origins: string[]
  created_at: string
}

export interface Thread {
  id: string
  mission_id: string
  title: string
  question: string
  rationale: string
  next_step: string
  status: string
  priority: number
  auto_run: boolean
  step_count: number
  created_at: string
  updated_at: string
}

export interface Observation {
  id: string
  thread_id: string
  attempt_id?: string
  action_id?: string
  kind: string
  content: string
  created_at: string
}

export interface Attempt {
  id: string
  thread_id: string
  status: string
  summary: string
  error: string
  started_at: string
  ended_at?: string
}

export interface ActionInput {
  kind: 'http' | 'local_command'
  method?: string
  url?: string
  headers?: Record<string, string>
  body?: string
  command?: string
  reason: string
}

export interface ActionResult {
  status_code?: number
  headers?: Record<string, string[]>
  body_base64?: string
  stdout_base64?: string
  stderr_base64?: string
  exit_code: number
  truncated: boolean
  effect_unknown: boolean
  resolved_ip?: string
  duration_ms: number
}

export interface Action {
  id: string
  thread_id: string
  attempt_id?: string
  input: ActionInput
  request_hash: string
  status: string
  result?: ActionResult
  error: string
  decision_reason: string
  resolved_at?: string
  resolution_note?: string
  created_at: string
  updated_at: string
}

export interface ResearchEvent {
  id: number
  mission_id: string
  thread_id?: string
  kind: string
  message: string
  created_at: string
}

export interface Snapshot {
  mission: Mission
  thread: Thread
  observations: Observation[]
  actions: Action[]
  attempts: Attempt[]
}

const statuses: Record<string, string> = {
  ready: '可接续', running: '执行中', waiting_approval: '等待审批',
  blocked: '已阻塞', paused: '已暂停', concluded: '已结束',
  pending: '待审批', approved: '已放行 · 待执行', succeeded: '执行成功',
  failed: '执行失败', rejected: '已拒绝', outcome_unknown: '效果未知', interrupted: '已中断',
}
export const statusText = (status: string) => statuses[status] ?? status
export const statusTone = (status: string) => {
  if (['pending', 'waiting_approval', 'outcome_unknown', 'blocked', 'interrupted'].includes(status)) return 'warning'
  if (['failed', 'rejected'].includes(status)) return 'danger'
  if (['running', 'approved'].includes(status)) return 'accent'
  if (['succeeded', 'concluded'].includes(status)) return 'success'
  return 'neutral'
}
const observationKinds: Record<string, string> = {
  operator_note: '人工记录', http_response: 'HTTP 响应', action_result: '动作结果',
  model_observation: '模型观察', model_note: '模型记录', local_command: '命令结果',
}
export const observationKind = (kind: string) => observationKinds[kind] ?? `观察（${kind}）`
export function dateTime(value?: string) {
  if (!value) return '尚未结束'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  }).format(date)
}
export const actionTitle = (action: Action) => action.input.kind === 'http'
  ? `${action.input.method || 'GET'} ${action.input.url || ''}`
  : action.input.command || '本地命令'
export const needsResolution = (action: Action) => action.status === 'outcome_unknown' && !action.resolved_at
export const isUnsettled = (action: Action) => ['pending', 'approved', 'running'].includes(action.status) || needsResolution(action)
