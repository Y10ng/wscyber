export interface SystemInfo {
  name: string
  version: string
  model_configured: boolean
  model: string
  model_management_available?: boolean
  model_content_recording_available?: boolean
  model_source?: 'managed' | 'environment' | 'none'
  default_model_config_id?: string
  sandbox_image: string
  sandbox_runtime: string
  network_mode: string
  max_parallel: number
  max_steps?: number
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
  pause_requested?: boolean
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

// 平台 HTTP 动作的只读投影；不是全工具流量或独立请求模型。
export interface HTTPRecordSummary {
  id: string
  mission_id: string
  thread_id: string
  thread_title: string
  method: string
  url: string
  status: string
  has_result: boolean
  status_code?: number
  truncated: boolean
  effect_unknown: boolean
  resolved_at?: string
  created_at: string
  updated_at: string
}

export interface HTTPRecordPage {
  items: HTTPRecordSummary[]
  next_cursor: string
}

export interface HTTPRecordDetail {
  mission_id: string
  thread: Thread
  action: Action
}

export interface Snapshot {
  mission: Mission
  thread: Thread
  observations: Observation[]
  actions: Action[]
  attempts: Attempt[]
}

export interface ExplorationInput {
  research_brief?: import('../ui/researchBrief.ts').ResearchBrief
  materials?: { id: string; token: string }[]
  safety_prompt_enabled?: boolean
  safety_prompt?: string
  record_model_content?: boolean
  model_config_id?: string
  description: string
  goal: string
  allowed_origins: string[]
}

export interface ExplorationStart {
  mission: Mission
  thread: Thread
  start_status: string
}

export interface OperationReceipt {
  key: string
  method: string
  path: string
  resource_type: string
  resource_id: string
  response_status: number
  committed_at: string
}

const statuses: Record<string, string> = {
  ready: '可接续',
  running: '执行中',
  waiting_approval: '等待审批',
  blocked: '已阻塞',
  paused: '已暂停',
  concluded: '已结束',
  pending: '待审批',
  approved: '已放行 · 待执行',
  succeeded: '执行成功',
  failed: '执行失败',
  rejected: '已拒绝',
  outcome_unknown: '效果未知',
  interrupted: '已中断',
}

export const statusText = (status: string): string => statuses[status] ?? status

export type BadgeColor = 'default' | 'processing' | 'success' | 'warning' | 'error'

export const statusBadgeColor = (status: string): BadgeColor => {
  if (['pending', 'waiting_approval', 'outcome_unknown', 'blocked', 'interrupted'].includes(status)) {
    return 'warning'
  }
  if (['failed', 'rejected'].includes(status)) {
    return 'error'
  }
  if (['running', 'approved'].includes(status)) {
    return 'processing'
  }
  if (['succeeded', 'concluded'].includes(status)) {
    return 'success'
  }
  return 'default'
}

const observationKinds: Record<string, string> = {
  operator_note: '人工记录',
  http_response: 'HTTP 响应',
  action_result: '动作结果',
  model_observation: '模型观察',
  model_note: '模型记录',
  local_command: '命令结果',
}

export const observationKindText = (kind: string): string =>
  observationKinds[kind] ?? `观察（${kind}）`

export function formatDateTime(value?: string, timeZone?: string): string {
  if (!value) return '尚未结束'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
    ...(timeZone ? { timeZone } : {}),
  }).format(date)
}

export const actionSummaryTitle = (action: Action): string =>
  action.input.kind === 'http'
    ? `${action.input.method || 'GET'} ${action.input.url || ''}`
    : action.input.command || '本地命令'

export const needsUnknownResolution = (action: Action): boolean =>
  action.status === 'outcome_unknown' && !action.resolved_at
