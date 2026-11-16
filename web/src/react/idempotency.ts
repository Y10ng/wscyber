import type { ExplorationInput } from './types.ts'
import type { MaterialItem } from '../ui/materials.ts'
import { briefIdentity, cloneResearchBrief, type ResearchBrief } from '../ui/researchBrief.ts'
const materialIdentity = (input: ExplorationInput) => JSON.stringify([...(input.materials ?? [])].sort((a, b) => a.id.localeCompare(b.id)))

export function generateOperationKey(): string {
  const bytes = new Uint8Array(16)
  globalThis.crypto.getRandomValues(bytes)
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
}

export function isValidOperationKey(key: string): boolean {
  if (key.length < 22 || key.length > 128) return false
  for (let i = 0; i < key.length; i++) {
    const c = key.charCodeAt(i)
    const ok =
      (c >= 97 && c <= 122) ||
      (c >= 65 && c <= 90) ||
      (c >= 48 && c <= 57) ||
      c === 45 ||
      c === 95
    if (!ok) return false
  }
  return true
}

export function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}

export function canonicalizeExplorationInput(
  description: string,
  goal: string,
  normalizedOrigins: string[],
): ExplorationInput {
  const sortedUnique = Array.from(new Set(normalizedOrigins)).sort()
  return {
    description,
    goal,
    allowed_origins: sortedUnique,
  }
}

export function isSameExplorationInput(a: ExplorationInput, b: ExplorationInput): boolean {
  if (a.description !== b.description || a.goal !== b.goal || (a.model_config_id || '') !== (b.model_config_id || '') || Boolean(a.record_model_content) !== Boolean(b.record_model_content)) return false
  if (materialIdentity(a) !== materialIdentity(b) || Boolean(a.safety_prompt_enabled) !== Boolean(b.safety_prompt_enabled) || (a.safety_prompt || '') !== (b.safety_prompt || '')) return false
  if (briefIdentity(a.research_brief) !== briefIdentity(b.research_brief)) return false
  if (a.allowed_origins.length !== b.allowed_origins.length) return false
  for (let i = 0; i < a.allowed_origins.length; i++) {
    if (a.allowed_origins[i] !== b.allowed_origins[i]) return false
  }
  return true
}

export interface ExplorationDraft {
  description: string
  goal: string
  originsText: string
  modelConfigID?: string
  recordModelContent?: boolean
  materials?: MaterialItem[]
  safetyPromptEnabled?: boolean
  safetyPrompt?: string
  researchBrief?: ResearchBrief
}

export type SubmissionPhase = 'idle' | 'submitting' | 'verifying' | 'unverified'

export interface ExplorationSubmissionState {
  phase: SubmissionPhase
  operationKey: string | null
  frozenPayload: ExplorationInput | null
  lastHttpStatus: number | null
  message: string
  operationsChecked404: boolean
  uncertainHistory?: boolean
}

export const initialSubmissionState: ExplorationSubmissionState = {
  phase: 'idle',
  operationKey: null,
  frozenPayload: null,
  lastHttpStatus: null,
  message: '',
  operationsChecked404: false,
}

export function isSubmissionLocked(state: ExplorationSubmissionState): boolean {
  return state.operationKey !== null || state.phase !== 'idle'
}

export function beginSubmission(
  current: ExplorationSubmissionState,
  payload: ExplorationInput,
  keyGenerator: () => string = generateOperationKey,
): ExplorationSubmissionState {
  if (current.operationKey && current.frozenPayload) {
    if (!isSameExplorationInput(current.frozenPayload, payload)) {
      throw new Error('当前提交尚未核对完成，不能修改请求内容或更换提交键新建任务。')
    }
    return {
      ...current,
      phase: 'submitting',
      message: '正在使用原提交键重试同一请求…',
    }
  }
  const key = keyGenerator()
  if (!isValidOperationKey(key)) {
    throw new Error('生成的提交键格式无效')
  }
  return {
    phase: 'submitting',
    operationKey: key,
    frozenPayload: {
      description: payload.description,
      goal: payload.goal,
      allowed_origins: [...payload.allowed_origins],
      ...(payload.model_config_id ? { model_config_id: payload.model_config_id } : {}),
      ...(payload.record_model_content ? { record_model_content: true } : {}),
      ...(payload.materials?.length ? { materials: payload.materials.map(item => ({ id: item.id, token: item.token })).sort((a, b) => a.id.localeCompare(b.id)) } : {}),
      ...(payload.safety_prompt_enabled ? { safety_prompt_enabled: true, safety_prompt: payload.safety_prompt || '' } : {}),
      ...(payload.research_brief ? { research_brief: cloneResearchBrief(payload.research_brief) } : {}),
    },
    lastHttpStatus: null,
    message: '正在提交探索任务…',
    operationsChecked404: false,
  }
}

export function handlePostFailure(
  current: ExplorationSubmissionState,
  httpStatus: number,
  errorText: string,
  code?: string,
): ExplorationSubmissionState {
  if (!current.operationKey || !current.frozenPayload) {
    return initialSubmissionState
  }
  // 仅没有未知/401历史的确定拒绝可回到草稿；普通409不能按中文错误猜测。
  if (!current.uncertainHistory && (httpStatus === 400 || (httpStatus === 409 && code === 'material_selection_stale'))) {
    return {
      phase: 'idle',
      operationKey: null,
      frozenPayload: null,
      lastHttpStatus: httpStatus,
      message: code === 'material_selection_stale' ? '选用资料已变化，请刷新选择后重新提交。' : errorText,
      operationsChecked404: false,
    }
  }
  // 401 鉴权失败：保留原键与冻结载荷，待重认证后继续核对或同键重试
  if (httpStatus === 401) {
    return {
      ...current,
      phase: 'unverified',
    uncertainHistory: true,
      lastHttpStatus: 401,
      message: '访问令牌已失效，请重新登录后先核对提交状态或使用原提交键重试。',
    }
  }
  // 0（断连/超时）、409、503、5xx 均必须保留原键与原载荷，优先核对
  const statusHint =
    httpStatus === 409
      ? `状态冲突或并发等待超时（${errorText}）。已保留原提交键，请先核对提交状态，不要新建任务。`
      : httpStatus === 503
        ? `服务暂时不可用或模型未就绪（${errorText}）。已保留原提交键，请先核对提交状态；若确认未提交再使用原键重试。`
        : httpStatus === 0
          ? `${errorText} 已保留原提交键，请先核对提交状态，不要重复新建。`
          : `提交结果未确认（${errorText}）。已保留原提交键，请先核对提交状态。`

  return {
    ...current,
    phase: 'unverified',
    uncertainHistory: true,
    lastHttpStatus: httpStatus,
    message: statusHint,
  }
}

export function handleOperationCheck404(
  current: ExplorationSubmissionState,
): ExplorationSubmissionState {
  if (!current.operationKey || !current.frozenPayload) {
    return initialSubmissionState
  }
  return {
    ...current,
    phase: 'unverified',
    uncertainHistory: true,
    lastHttpStatus: 404,
    operationsChecked404: true,
    message:
      '未查到已提交回执（不代表未提交，原请求可能仍在途）。系统不会自动重发；可稍后再次核对，或显式使用原提交键重试。',
  }
}

export function handleSessionUnauthorizedDuringSubmission(
  current: ExplorationSubmissionState,
): ExplorationSubmissionState {
  if (!current.operationKey || !current.frozenPayload) {
    return initialSubmissionState
  }
  return {
    ...current,
    phase: 'unverified',
    uncertainHistory: true,
    lastHttpStatus: 401,
    message: '会话认证已失效，本次提交结果尚未确认。重新登录后请先核对提交状态，或使用原提交键重试。',
  }
}

