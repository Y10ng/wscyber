import type { Content, EncodedValue } from '@cyber/aop'
import type { AOPEvent } from '@/viewer'
import { anyUnpack, timestampDate } from '@bufbuild/protobuf/wkt'
import { ReviewSchema, ReviewState, type Review } from '../cyber-proto'
import { eventTime, runtimeEvents } from './jev-view'

// 执行时间线（R2）纯函数层：把已加载的原生 AOP 事件派生为「模型消息 / 工具调用」
// 两类条目，按真实 call_id 配对（toolCall ↔ toolResult 同 scope+call.id），并按
// 工具名解析业务语义（webapi_http / webapi_finding / webapi_verification_result /
// webapi_retest_result / webapi_report_draft）。不改后端、不新增接口；
// 与 cyber-ui viewer 的 aop-reducer 同一套配对/编码约定。

const decoder = new TextDecoder()
// 与 viewer aop-reducer 相同的截断上限与标记格式。
const MAX_TOOL_ARGS_CHARS = 32_000
const MAX_TOOL_RESULT_CHARS = 100_000

export interface ExecutionUsage {
  input_tokens?: number
  output_tokens?: number
  total_tokens?: number
}

export interface ExecutionHttpInfo {
  method: string
  url: string
  path: string
  /** 后端业务状态：completed/claimed/denied/review_required/unknown/capture_failed；pending 时为空串 */
  status: string
  httpStatus?: number
  sent?: boolean | null
  captureTruncated: boolean
  evidenceId?: string
  taskId?: string
}

export interface ExecutionBiz {
  kind: 'http' | 'finding' | 'verification' | 'retest' | 'report' | 'authz_compare'
  /** 结果已到达且非 error；pending 条目以 entry.pending 为准 */
  ok: boolean
  errorMessage?: string
  http?: ExecutionHttpInfo
  findingId?: string
  findingNumber?: string
  findingTitle?: string
  /** verification 的 verdict / retest 的 conclusion 原始枚举 */
  verdict?: string
  reportTitle?: string
  /** authz_compare 参数中的变体数（不解析结果表格） */
  variantCount?: number
}

export interface ExecutionEntry {
  id: string
  type: 'model' | 'tool'
  timestamp: number
  turnKey: string
  /** 稳定排序：首次出现的事件序 */
  order: number
  // 模型消息
  content?: string
  usage?: ExecutionUsage
  // 工具调用
  callId?: string
  toolName?: string
  argsText?: string
  resultText?: string
  pending?: boolean
  error?: boolean
  durationMs?: number
  biz?: ExecutionBiz
}

export interface ExecutionTurn {
  key: string
  /** 1 起始的轮次序号（按首轮条目时间排序） */
  index: number
  timestamp: number
}

export interface ExecutionSummary {
  modelCount: number
  toolCount: number
  httpCount: number
  bizCount: number
}

// B1 审批行：待审批/已批准/已拒绝/已超时（含取消）。live pending 由 App 把
// useGuardrailReviews 的 bySession 传入 pendingReviews，事件历史由 guardrail
// 扩展事件（Review / Decision）归并得出；同一 operationId 以最新状态为准。
export interface ExecutionApproval {
  /** operationId；live 待审批可能还没有事件记录，此时 eventId 取合成键。 */
  id: string
  eventId: string
  sessionId: string
  turnId: string
  state: 'pending' | 'approved' | 'rejected' | 'expired' | 'canceled'
  /** pending=true 时可批准/拒绝（App 转发 guardrails.resolve）。 */
  actionable: boolean
  /** 连接断开时 pending 不可操作。 */
  unavailable: boolean
  toolName: string
  /** 对应的工具调用 ID（与时间线工具条目 callId 配对，用于行状态覆盖）。 */
  callId?: string
  /** HTTP 方法+URL 或命令首 120 字。 */
  summary: string
  /** 后端记录的需审批原因（decision.reason）。 */
  reason?: string
  /** live Review 的到期时刻（epoch ms）。 */
  expiresAt?: number
  /** 终态事件时间。 */
  resolvedAt?: number
  /** 所属轮次键（与时间线条目同口径 `${sessionId}:${turnId || 'session'}`）。 */
  turnKey: string
  /** 并入时间线流排序的时间戳（终态事件时间；live pending 为挂起事件时间或 0）。 */
  timestamp: number
}

export interface ExecutionTimelineData {
  entries: ExecutionEntry[]
  turns: ExecutionTurn[]
  turnByKey: Record<string, number>
  summary: ExecutionSummary
  approvals: ExecutionApproval[]
  /** 仍待人工处理（pending）的审批，按时间升序；执行视图置顶渲染。 */
  pendingApprovals: ExecutionApproval[]
}

function textOfContent(parts: readonly Content[]): string {
  const values: string[] = []
  for (const part of parts) {
    if (part.value.case === 'text') values.push(part.value.value.text)
  }
  return values.filter(Boolean).join('\n')
}

// 与 viewer aop-reducer 的 encoded()+stringify() 同约定：json/protobuf+json 解码后
// 美化，其余按字节解文本；超限截断并保留同格式标记。
function encodedToText(value?: EncodedValue, limit = MAX_TOOL_ARGS_CHARS): string {
  if (!value) return ''
  let text = ''
  if (value.mediaType === 'application/json' || value.mediaType === 'application/protobuf+json') {
    try {
      text = JSON.stringify(JSON.parse(decoder.decode(value.data)), null, 2)
    } catch {
      text = decoder.decode(value.data)
    }
  } else {
    text = decoder.decode(value.data)
  }
  return text.length <= limit ? text : `${text.slice(0, limit)}\n... (${text.length - limit} characters omitted)`
}

function truncateText(text: string, limit: number): string {
  return text.length <= limit ? text : `${text.slice(0, limit)}\n... (${text.length - limit} characters omitted)`
}

function parseJson(text: string | undefined): Record<string, unknown> | undefined {
  if (!text) return undefined
  try {
    const value = JSON.parse(text)
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined
  } catch {
    return undefined
  }
}

function firstLine(text: string | undefined): string {
  const line = (text || '').split('\n').map((part) => part.trim()).find(Boolean)
  return line || ''
}

function urlPath(url: string): string {
  try {
    const parsed = new URL(url)
    return `${parsed.pathname}${parsed.search}` || '/'
  } catch {
    return url
  }
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' && value !== '' ? value : undefined
}

// ── B1 审批行（纯函数派生）─────────────────────────────────────────────────
// guardrail 扩展事件（Emitter=guardrail，payload=Any<Review>）带 operationId 与
// 状态；同一 operationId 多条事件取最新状态。live pending（useGuardrailReviews）
// 覆盖为可操作行；没有事件记录的 live pending 以合成键置顶。

function decodeReview(event: AOPEvent): Review | undefined {
  if (event.payload.case !== 'extension') return undefined
  try { return anyUnpack(event.payload.value, ReviewSchema) } catch { return undefined }
}

function reviewStateOf(review: Review): ExecutionApproval['state'] | undefined {
  switch (review.state) {
    case ReviewState.PENDING: return 'pending'
    case ReviewState.APPROVED: return 'approved'
    case ReviewState.REJECTED: return 'rejected'
    case ReviewState.EXPIRED: return 'expired'
    case ReviewState.CANCELED: return 'canceled'
    default: return undefined
  }
}

function eventMs(event: AOPEvent): number {
  return event.emittedAt ? timestampDate(event.emittedAt).getTime() : 0
}

// 审批摘要：与顶栏同约定 —— JSON 参数里 command/cmd/script 取命令首 120 字，
// 否则 method+URL。
function approvalSummary(review: Review): string {
  let value: unknown = ''
  try { value = JSON.parse(decoder.decode(review.call?.arguments?.data)) } catch {
    try { value = decoder.decode(review.call?.arguments?.data) } catch { value = '' }
  }
  const record = value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
  if (record) {
    const commandKey = ['command', 'cmd', 'script'].find((key) => typeof record[key] === 'string')
    if (commandKey) return clipCommand(record[commandKey] as string)
    if (typeof record.url === 'string') {
      const method = typeof record.method === 'string' ? record.method.toUpperCase() : ''
      return `${method ? `${method} ` : ''}${record.url}`
    }
  }
  if (typeof value === 'string' && value) return clipCommand(value)
  return review.call?.name || ''
}

function clipCommand(command: string): string {
  return command.length > 120 ? `${command.slice(0, 120)}…` : command
}

function buildApprovals(events: readonly AOPEvent[], pendingReviews: readonly Review[], connected: boolean): {
  approvals: ExecutionApproval[]
  pendingApprovals: ExecutionApproval[]
} {
  const byOperation = new Map<string, { event: AOPEvent; review: Review }>()
  for (const event of events) {
    const review = decodeReview(event)
    const id = review?.operation?.operationId
    if (!id) continue
    const previous = byOperation.get(id)
    // 最新状态优先；同状态取更晚事件。
    const previousResolved = previous ? previous.review.state !== ReviewState.PENDING : true
    const currentResolved = review.state !== ReviewState.PENDING
    if (previous && previousResolved && !currentResolved) continue
    if (previous && previousResolved === currentResolved && eventMs(previous.event) >= eventMs(event)) continue
    byOperation.set(id, { event, review })
  }
  const approvals: ExecutionApproval[] = []
  const liveKeys = new Set<string>()
  for (const review of pendingReviews) {
    const id = review.operation?.operationId
    if (!id || review.state !== ReviewState.PENDING) continue
    liveKeys.add(id)
    const existing = byOperation.get(id)
    approvals.push({
      id,
      eventId: existing ? `guardrail:${id}` : `pending:${id}`,
      sessionId: review.sessionId || existing?.event.sessionId || '',
      turnId: existing?.event.turnId || '',
      turnKey: existing ? `${existing.event.sessionId}:${existing.event.turnId || 'session'}` : '',
      timestamp: existing ? eventMs(existing.event) : 0,
      state: 'pending',
      actionable: true,
      unavailable: !connected,
      toolName: review.call?.name || existing?.review.call?.name || '',
      callId: review.call?.id || review.operation?.callId || existing?.review.call?.id || undefined,
      summary: approvalSummary(review),
      reason: review.decision?.reason || existing?.review.decision?.reason || undefined,
      expiresAt: review.expiresAt ? timestampDate(review.expiresAt).getTime() : undefined,
    })
  }
  for (const [id, { event, review }] of byOperation) {
    const state = reviewStateOf(review)
    if (!state) continue
    if (liveKeys.has(id) && state === 'pending') continue // live 行已覆盖
    if (state === 'pending' && !liveKeys.has(id)) continue // 挂起已过期/被处理，事件未到
    approvals.push({
      id,
      eventId: `guardrail:${id}`,
      sessionId: event.sessionId,
      turnId: event.turnId,
      turnKey: `${event.sessionId}:${event.turnId || 'session'}`,
      timestamp: eventMs(event),
      state,
      actionable: false,
      unavailable: false,
      toolName: review.call?.name || '',
      callId: review.call?.id || review.operation?.callId || undefined,
      summary: approvalSummary(review),
      reason: review.decision?.reason || undefined,
      resolvedAt: state === 'pending' ? undefined : eventMs(event),
    })
  }
  approvals.sort((left, right) => {
    const pendingDelta = Number(right.actionable) - Number(left.actionable)
    if (pendingDelta !== 0) return pendingDelta
    const a = left.resolvedAt ?? left.expiresAt ?? 0
    const b = right.resolvedAt ?? right.expiresAt ?? 0
    return a - b
  })
  return { approvals, pendingApprovals: approvals.filter((item) => item.state === 'pending') }
}

// 业务语义：按工具名识别，参数/结果是 JSON 字符串时解析；解析失败如实显示错误原文。
function deriveBiz(
  toolName: string,
  argsText: string,
  resultText: string | undefined,
  error: boolean,
): ExecutionBiz | undefined {
  const args = parseJson(argsText)
  const result = resultText !== undefined ? parseJson(resultText) : undefined
  const hasResult = resultText !== undefined
  const errorMessage = error ? (firstLine(resultText) || '调用失败') : undefined
  switch (toolName) {
    case 'webapi_http': {
      const method = typeof args?.method === 'string' ? args.method.toUpperCase() : ''
      const url = typeof args?.url === 'string' ? args.url : ''
      return {
        kind: 'http',
        ok: !error && hasResult,
        errorMessage,
        http: {
          method,
          url,
          path: urlPath(url),
          status: str(result?.status) || '',
          httpStatus: typeof result?.http_status === 'number' ? result.http_status : undefined,
          sent: typeof result?.sent === 'boolean' ? result.sent : null,
          captureTruncated: result?.capture_truncated === true,
          evidenceId: str(result?.evidence_id),
          taskId: str(result?.task_id),
        },
      }
    }
    case 'webapi_finding':
      return {
        kind: 'finding',
        ok: !error && hasResult,
        errorMessage,
        findingId: str(result?.finding_id) || str(args?.finding_id),
        findingNumber: str(result?.number),
        findingTitle: (() => {
          const content = args?.content
          return content && typeof content === 'object' && !Array.isArray(content)
            ? str((content as Record<string, unknown>).title)
            : undefined
        })(),
      }
    case 'webapi_verification_result':
      return { kind: 'verification', ok: !error && hasResult, errorMessage, verdict: str(result?.verdict) }
    case 'webapi_retest_result':
      return { kind: 'retest', ok: !error && hasResult, errorMessage, verdict: str(result?.conclusion) }
    case 'webapi_report_draft':
      return { kind: 'report', ok: !error && hasResult, errorMessage, reportTitle: str(result?.title) || str(args?.title) }
    case 'webapi_authz_compare': {
      const variants = Array.isArray(args?.variants) ? args.variants : []
      return { kind: 'authz_compare', ok: !error && hasResult, errorMessage, variantCount: variants.length }
    }
    default:
      return undefined
  }
}

function mergeUsage(previous: ExecutionUsage | undefined, usage: ExecutionUsage | undefined): ExecutionUsage | undefined {
  if (!usage) return previous
  // 与 reducer usageMetadata 相同的累计口径：usage 事件按增量累加。
  return {
    input_tokens: (previous?.input_tokens || 0) + (usage.input_tokens || 0),
    output_tokens: (previous?.output_tokens || 0) + (usage.output_tokens || 0),
    total_tokens: (previous?.total_tokens || 0) + (usage.total_tokens || 0),
  }
}

function usageFromProto(usage: {
  inputTokens: bigint
  outputTokens: bigint
  totalTokens: bigint
} | undefined | null): ExecutionUsage | undefined {
  if (!usage) return undefined
  return {
    input_tokens: Number(usage.inputTokens),
    output_tokens: Number(usage.outputTokens),
    total_tokens: Number(usage.totalTokens),
  }
}

// events → 条目：单趟遍历（时间有序、按 sessionId+id/seq 去重），工具调用与结果按
// `${sessionId}:${turnId}:tool:${callId}` 配对；消息 delta 与完整 message 按 messageId
// 合并（完整覆盖 delta）；usage 按 turn 累计挂到该轮最后一条模型消息。
export function buildExecutionTimeline(source: readonly AOPEvent[], pendingReviews: readonly Review[] = [], connected = true): ExecutionTimelineData {
  const events = runtimeEvents(source)
  const entries: ExecutionEntry[] = []
  const toolByKey = new Map<string, ExecutionEntry>()
  const messageByKey = new Map<string, ExecutionEntry>()
  const usageByTurn = new Map<string, ExecutionUsage>()
  const orderBase = { value: 0 }

  for (const event of events) {
    const order = orderBase.value++
    const ts = eventTime(event)
    const turnKey = `${event.sessionId}:${event.turnId || 'session'}`
    switch (event.payload.case) {
      case 'messageDelta': {
        const delta = event.payload.value
        if (delta.value.case !== 'text') break
        const key = `${event.sessionId}:${event.turnId}:msg:${delta.messageId}`
        let entry = messageByKey.get(key)
        if (!entry) {
          entry = {
            id: `model:${key}`, type: 'model', timestamp: ts, turnKey, order,
            content: '',
          }
          messageByKey.set(key, entry)
          entries.push(entry)
        }
        entry.content = `${entry.content || ''}${delta.value.value}`
        break
      }
      case 'message': {
        const message = event.payload.value
        if (message.role !== 'assistant') break // 用户/系统消息不进时间线
        const text = textOfContent(message.content)
        const key = `${event.sessionId}:${event.turnId}:msg:${message.id || `anon:${order}`}`
        const existing = messageByKey.get(key)
        if (existing) {
          // 完整消息覆盖自身 delta（与 reducer 同语义），时间保持首现时刻。
          existing.content = text
        } else {
          const entry: ExecutionEntry = {
            id: `model:${key}`, type: 'model', timestamp: ts, turnKey, order, content: text,
          }
          messageByKey.set(key, entry)
          entries.push(entry)
        }
        break
      }
      case 'toolCall': {
        const call = event.payload.value
        const key = `${event.sessionId}:${event.turnId}:tool:${call.id}`
        if (toolByKey.has(key)) break
        const entry: ExecutionEntry = {
          id: `tool:${key}`, type: 'tool', timestamp: ts, turnKey, order,
          callId: call.id, toolName: call.name,
          argsText: encodedToText(call.arguments),
          pending: true, error: false,
        }
        entry.biz = deriveBiz(call.name, entry.argsText!, undefined, false)
        toolByKey.set(key, entry)
        entries.push(entry)
        break
      }
      case 'toolResult': {
        const result = event.payload.value
        const key = `${event.sessionId}:${event.turnId}:tool:${result.callId}`
        const resultTs = ts
        const resultText = truncateText(textOfContent(result.output), MAX_TOOL_RESULT_CHARS)
        let entry = toolByKey.get(key)
        if (!entry) {
          // 历史截断等场景结果先于可见调用：仍按同 key 建条目，参数留空如实显示。
          entry = {
            id: `tool:${key}`, type: 'tool', timestamp: resultTs, turnKey, order,
            callId: result.callId, toolName: result.name || '',
            argsText: '', pending: false, error: result.isError,
          }
          toolByKey.set(key, entry)
          entries.push(entry)
        }
        entry.resultText = resultText
        entry.pending = false
        entry.error = result.isError
        const duration = Number(result.durationMs)
        entry.durationMs = duration > 0 ? duration : Math.max(0, resultTs - entry.timestamp)
        entry.biz = deriveBiz(entry.toolName || result.name || '', entry.argsText || '', resultText, result.isError)
        break
      }
      case 'usage': {
        const merged = mergeUsage(usageByTurn.get(turnKey), usageFromProto(event.payload.value))
        if (merged) usageByTurn.set(turnKey, merged)
        break
      }
      case 'turnEnded': {
        // 仅当该轮没有单独 usage 事件时才用 turn 总量兜底（与 reducer 同口径，防双计）。
        const fallback = usageFromProto(event.payload.value.usage)
        if (!usageByTurn.has(turnKey) && fallback) {
          usageByTurn.set(turnKey, fallback)
        }
        break
      }
      default:
        break
    }
  }

  // usage 挂到各轮最后一条模型消息；无模型消息的轮不计。
  const lastModelByTurn = new Map<string, ExecutionEntry>()
  for (const entry of entries) {
    if (entry.type === 'model') lastModelByTurn.set(entry.turnKey, entry)
  }
  for (const [turnKey, usage] of usageByTurn) {
    const target = lastModelByTurn.get(turnKey)
    if (target && usage) target.usage = usage
  }

  entries.sort((left, right) => left.timestamp - right.timestamp || left.order - right.order)

  // 轮次：按首轮条目时间排 1..N。
  const turnFirst = new Map<string, number>()
  for (const entry of entries) {
    const first = turnFirst.get(entry.turnKey)
    if (first === undefined || entry.timestamp < first) turnFirst.set(entry.turnKey, entry.timestamp)
  }
  const turns: ExecutionTurn[] = [...turnFirst.entries()]
    .sort((left, right) => left[1] - right[1])
    .map(([key, timestamp], index) => ({ key, timestamp, index: index + 1 }))
  const turnByKey: Record<string, number> = {}
  for (const turn of turns) turnByKey[turn.key] = turn.index

  let modelCount = 0
  let toolCount = 0
  let httpCount = 0
  let bizCount = 0
  for (const entry of entries) {
    if (entry.type === 'model') modelCount++
    else {
      toolCount++
      if (entry.biz?.kind === 'http') httpCount++
      else if (entry.biz) bizCount++
    }
  }

  return {
    entries,
    turns,
    turnByKey,
    summary: { modelCount, toolCount, httpCount, bizCount },
    ...buildApprovals(events, pendingReviews, connected),
  }
}
