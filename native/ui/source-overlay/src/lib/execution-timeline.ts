import type { Content, EncodedValue } from '@cyber/aop'
import type { AOPEvent } from '@/viewer'
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
  kind: 'http' | 'finding' | 'verification' | 'retest' | 'report'
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

export interface ExecutionTimelineData {
  entries: ExecutionEntry[]
  turns: ExecutionTurn[]
  turnByKey: Record<string, number>
  summary: ExecutionSummary
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
export function buildExecutionTimeline(source: readonly AOPEvent[]): ExecutionTimelineData {
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
  }
}
