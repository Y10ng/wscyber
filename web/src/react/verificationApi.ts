import { ApiError, idPath, requestApi } from './api.ts'
import type { Action, Observation, Thread } from './types.ts'
import type { FindingEvidence } from '../ui/Findings.tsx'
import type { VerificationResult, VerificationSummary } from '../ui/Verifications.tsx'
export interface VerificationDetail extends VerificationSummary { thread: Thread; result: VerificationResult | null; execution_error: string }
export interface VerificationPage { mission_id: string; finding_id: string; items: VerificationSummary[]; next_cursor: string }
export interface VerificationEvidence { mission_id: string; finding_id: string; verification_id: string; index: number; reference: FindingEvidence; matches_snapshot: boolean; thread: Thread; action?: Action; observation?: Observation }
export const verificationPath = (mission: string, finding: string) => `/missions/${idPath(mission)}/findings/${idPath(finding)}/verifications`
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const uuid = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v)
const date = (v: unknown) => typeof v === 'string' && v.length <= 40 && Number.isFinite(Date.parse(v))
const text = (v: unknown, max: number): v is string => typeof v === 'string' && !v.includes('\0') && new TextEncoder().encode(v).length <= max
const hex = (v: unknown, n: number) => typeof v === 'string' && new RegExp(`^[0-9a-f]{${n}}$`).test(v)
const invalid = () => new ApiError('验证记录格式或归属不一致，未显示本次结果。', 400)
const bounded = (v: unknown) => { if (new TextEncoder().encode(JSON.stringify(v)).length > 262144) throw invalid() }
export function validVerification(v: unknown, mission: string, finding: string): v is VerificationSummary {
  return object(v) && uuid(v.id) && v.mission_id === mission && v.finding_id === finding && uuid(v.thread_id) && uuid(v.source_revision_id) && date(v.created_at) && date(v.source_created_at) && Number.isInteger(v.max_steps) && Number(v.max_steps) >= 1 && Number(v.max_steps) <= 8 && Number.isSafeInteger(v.step_count) && Number(v.step_count) >= 0 && ['ready', 'running', 'waiting_approval', 'paused', 'blocked', 'concluded'].includes(String(v.status)) && ['', 'budget_exhausted'].includes(String(v.end_reason)) && !(v.end_reason && v.has_result) && typeof v.has_result === 'boolean' && typeof v.auto_run === 'boolean' && typeof v.pause_requested === 'boolean' && (v.has_result ? ['supported', 'counterevidence', 'inconclusive'].includes(String(v.verdict)) : v.verdict === '')
}
export function validateVerificationPage(v: unknown, mission: string, finding: string): VerificationPage {
  bounded(v)
  if (!object(v) || v.mission_id !== mission || v.finding_id !== finding || !text(v.next_cursor, 1024) || !Array.isArray(v.items) || v.items.length > 50 || !v.items.every(x => validVerification(x, mission, finding)) || new Set(v.items.map(x => x.id)).size !== v.items.length) throw invalid()
  return v as unknown as VerificationPage
}
export function validateVerificationDetail(v: unknown, mission: string, finding: string, id: string): VerificationDetail {
  bounded(v)
  if (!object(v) || !validVerification(v, mission, finding) || v.id !== id || !text(v.execution_error, 131072) || !object(v.thread) || v.thread.id !== v.thread_id || v.thread.mission_id !== mission || v.thread.status !== v.status || v.thread.step_count !== v.step_count || v.thread.auto_run !== v.auto_run || v.thread.pause_requested !== v.pause_requested || !text(v.thread.next_step, 131072)) throw invalid()
  if (v.has_result) {
    const r = v.result
    if (!object(r) || !uuid(r.attempt_id) || !hex(r.call_id, 32) || !date(r.created_at) || !object(r.conclusion) || r.conclusion.verdict !== v.verdict || !['method', 'summary', 'preconditions', 'limitations'].every(k => text((r.conclusion as Record<string, unknown>)[k], 4096)) || !r.conclusion.method || !r.conclusion.summary || !Array.isArray(r.evidence) || !Array.isArray(r.conclusion.evidence) || r.evidence.length > 16 || r.evidence.length !== r.conclusion.evidence.length) throw invalid()
    for (const [i, e] of r.evidence.entries()) {
      const ref = r.conclusion.evidence[i]
      if (!object(e) || !object(ref) || !uuid(e.id) || !['action', 'observation'].includes(String(e.kind)) || !['supporting', 'counterevidence', 'limitation'].includes(String(e.relation)) || e.id !== ref.id || e.kind !== ref.kind || e.relation !== ref.relation || e.thread_id !== v.thread_id || !(e.attempt_id === '' || uuid(e.attempt_id)) || !(e.action_id === '' || uuid(e.action_id)) || !date(e.created_at) || !date(e.captured_at) || !hex(e.content_sha256, 64) || !text(e.state, 32) || !['', 'http', 'local_command'].includes(String(e.action_kind)) || !(e.request_hash === '' || hex(e.request_hash, 64)) || !['has_result', 'truncated', 'effect_unknown'].every(k => typeof e[k] === 'boolean')) throw invalid()
    }
    if (new Set(r.evidence.map(e => `${e.kind}:${e.id}`)).size !== r.evidence.length) throw invalid()
  } else if (v.result !== null) throw invalid()
  return v as unknown as VerificationDetail
}
export async function fetchVerifications(mission: string, finding: string, cursor: string, signal?: AbortSignal) { return validateVerificationPage(await requestApi<unknown>(verificationPath(mission, finding) + `?${new URLSearchParams({ cursor, limit: '25' })}`, { signal, safeErrors: true }), mission, finding) }
export async function fetchVerification(mission: string, finding: string, id: string, signal?: AbortSignal) { return validateVerificationDetail(await requestApi<unknown>(verificationPath(mission, finding) + '/' + idPath(id), { signal, safeErrors: true }), mission, finding, id) }
export function validateVerificationEvidence(v: unknown, detail: VerificationDetail, index: number): VerificationEvidence {
  bounded(v); const expected = detail.result?.evidence[index]
  if (!expected || !object(v) || v.mission_id !== detail.mission_id || v.finding_id !== detail.finding_id || v.verification_id !== detail.id || v.index !== index || !object(v.reference) || typeof v.matches_snapshot !== 'boolean' || !object(v.thread) || v.thread.id !== detail.thread_id || v.thread.mission_id !== detail.mission_id) throw invalid()
  const ref = v.reference; if (Object.keys(expected).some(k => ref[k] !== expected[k as keyof FindingEvidence])) throw invalid()
  if (expected.kind === 'action') { if (!object(v.action) || v.observation !== undefined || v.action.id !== expected.id || v.action.thread_id !== detail.thread_id || !object(v.action.input) || v.action.input.kind !== expected.action_kind || v.action.request_hash !== expected.request_hash) throw invalid() }
  else if (!object(v.observation) || v.action !== undefined || v.observation.id !== expected.id || v.observation.thread_id !== detail.thread_id || !text(v.observation.content, 131072)) throw invalid()
  return v as unknown as VerificationEvidence
}
export async function fetchVerificationEvidence(d: VerificationDetail, i: number, signal: AbortSignal) { return validateVerificationEvidence(await requestApi<unknown>(verificationPath(d.mission_id, d.finding_id) + `/${idPath(d.id)}/evidence/${i}`, { signal, safeErrors: true }), d, i) }
