import { ApiError, idPath, requestApi } from './api.ts'
import { contextKindLabels, type ModelCallContext } from '../ui/ContextAssembly.tsx'
import type { ModelCall } from './modelUsage.ts'
const object = (x: unknown): x is Record<string, unknown> => x !== null && typeof x === 'object' && !Array.isArray(x)
const uuid = (x: unknown): x is string => typeof x === 'string' && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(x)
const hex = (x: unknown, n: number) => typeof x === 'string' && new RegExp(`^[0-9a-f]{${n}}$`).test(x)
const integer = (x: unknown, max: number): x is number => Number.isSafeInteger(x) && (x as number) >= 0 && (x as number) <= max
const date = (x: unknown) => typeof x === 'string' && x.length <= 40 && Number.isFinite(Date.parse(x))
export function validateCallContext(value: unknown, call: Pick<ModelCall, 'id' | 'mission_id' | 'thread_id' | 'attempt_id'>): ModelCallContext {
  const invalid = () => new ApiError('上下文装配记录无效，已拒绝显示。', 0)
  if (!object(value)) throw invalid()
  if (value.call_id !== call.id || value.mission_id !== call.mission_id || value.thread_id !== call.thread_id) throw new ApiError('装配记录归属不匹配，已拒绝显示。', 404)
  if (value.recorded === false && value.assembly === null) return value as unknown as ModelCallContext
  const a = value.assembly
  const withVerification = object(a) && a.policy === 'bounded-verification-1'
  const withFindings = object(a) && (a.policy === 'bounded-history-findings-1' || withVerification)
  if (value.recorded !== true || !object(a) || (a.policy !== 'bounded-history-1' && !withFindings) || a.mission_id !== call.mission_id || a.thread_id !== call.thread_id || a.attempt_id !== call.attempt_id || !uuid(a.mission_id) || !uuid(a.thread_id) || !uuid(a.attempt_id) || !date(a.assembled_at) || !integer(a.input_bytes, 262144) || a.input_bytes === 0 || a.max_input_bytes !== 262144 || !Array.isArray(a.sources) || a.sources.length > 160 || !Array.isArray(a.windows) || a.windows.length !== (withFindings ? 4 : 3)) throw invalid()
  const keys = new Set<string>(), windows = new Set<string>()
  for (const row of a.sources) {
    if (!object(row) || typeof row.kind !== 'string' || !Object.hasOwn(contextKindLabels, row.kind) || !(row.kind === 'material' ? hex(row.id, 32) : uuid(row.id)) || !date(row.at) || !integer(row.original_bytes, 1073741824) || !integer(row.included_bytes, 262144) || typeof row.included !== 'boolean' || typeof row.required !== 'boolean' || (row.state !== undefined && (typeof row.state !== 'string' || row.state.length > 64)) || (row.content_sha256 !== undefined && !hex(row.content_sha256, 64))) throw invalid()
    if (row.included ? row.reason !== 'selected' || row.included_bytes < 1 : row.required || row.included_bytes !== 0 || !['item_limit', 'history_budget'].includes(String(row.reason))) throw invalid()
    if (row.kind === 'verification_source' && (!withVerification || row.state !== 'candidate' || !row.included || !row.required || !hex(row.content_sha256,64))) throw invalid()
    if (row.kind === 'finding' && (!withFindings || row.state !== 'candidate' || !hex(row.content_sha256, 64))) throw invalid()
    if (withFindings && row.included && ['observation', 'action'].includes(row.kind) && !hex(row.content_sha256, 64)) throw invalid()
    const key = `${row.kind}:${row.id}`; if (keys.has(key)) throw invalid(); keys.add(key)
  }
  if (withVerification && a.sources.filter(s => s.kind === 'verification_source').length !== 1) throw invalid()
  for (const w of a.windows) {
    if (!object(w) || typeof w.kind !== 'string' || !(withFindings ? ['observation', 'action', 'attempt', 'finding'] : ['observation', 'action', 'attempt']).includes(w.kind) || windows.has(w.kind) || w.limit !== (w.kind === 'observation' ? 32 : 8) || typeof w.older_exists !== 'boolean' || ((w.cutoff_id === undefined) !== (w.cutoff_at === undefined)) || (w.cutoff_id !== undefined && (!uuid(w.cutoff_id) || !date(w.cutoff_at))) || (w.older_exists && w.cutoff_id === undefined)) throw invalid()
    windows.add(w.kind)
  }
  return value as unknown as ModelCallContext
}
export async function fetchCallContext(call: Pick<ModelCall, 'id' | 'mission_id' | 'thread_id' | 'attempt_id'>, signal: AbortSignal) {
  const value = await requestApi<unknown>(`/model-calls/${idPath(call.id)}/context?mission_id=${idPath(call.mission_id)}`, { signal, safeErrors: true })
  return validateCallContext(value, call)
}
