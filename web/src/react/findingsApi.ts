import { ApiError, idPath, requestApi } from './api.ts'
import type { Action, Observation, Thread } from './types.ts'
import type { FindingEvidence, FindingHistoryRow, FindingRevision, FindingSummary } from '../ui/Findings.tsx'
export interface FindingsPage { mission_id: string; items: FindingSummary[]; next_cursor: string }
export interface FindingHistoryPage { mission_id: string; finding_id: string; items: FindingHistoryRow[]; next_cursor: string }
export interface FindingEvidenceContent { mission_id: string; finding_id: string; revision_id: string; index: number; reference: FindingEvidence; matches_snapshot: boolean; thread: Thread; action?: Action; observation?: Observation }
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v)
const uuid = (v: unknown): v is string => typeof v === 'string' && /^[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(v)
const hex = (v: unknown, n: number) => typeof v === 'string' && new RegExp(`^[a-f0-9]{${n}}$`).test(v)
const date = (v: unknown) => typeof v === 'string' && v.length <= 40 && Number.isFinite(Date.parse(v))
const integer = (v: unknown, min = 1, max = 2147483647): v is number => Number.isSafeInteger(v) && Number(v) >= min && Number(v) <= max
const text = (v: unknown, max: number, required = false): v is string => typeof v === 'string' && !v.includes('\0') && new TextEncoder().encode(v).length <= max && (!required || !!v.trim())
const invalid = () => new ApiError('候选记录格式或归属不一致，未显示本次结果。', 400)
const bounded = (v: unknown) => { if (new TextEncoder().encode(JSON.stringify(v)).length > 262144) throw invalid() }
const reference = (v: unknown) => object(v) && ['action', 'observation'].includes(String(v.kind)) && uuid(v.id) && ['supporting', 'counterevidence', 'limitation'].includes(String(v.relation))
export function validFindingSummary(v: unknown, mission: string, finding?: string): v is FindingSummary {
  return object(v) && uuid(v.id) && v.mission_id === mission && (!finding || v.id === finding) && uuid(v.thread_id) && text(v.title, 256, true) && typeof v.number === 'string' && /^WSC-\d{6,19}$/.test(v.number) && v.status === 'candidate' && integer(v.revision) && uuid(v.revision_id) && date(v.created_at) && date(v.updated_at)
}
export function validateFindingsPage(v: unknown, mission: string, thread = ''): FindingsPage {
  bounded(v)
  if (!object(v) || v.mission_id !== mission || !Array.isArray(v.items) || v.items.length > 50 || !text(v.next_cursor, 1024) || !v.items.every(x => validFindingSummary(x, mission) && (!thread || x.thread_id === thread)) || new Set(v.items.map(x => x.id)).size !== v.items.length) throw invalid()
  return v as unknown as FindingsPage
}
export function validateFindingRevision(v: unknown, mission: string, finding: string, revision = ''): FindingRevision {
  bounded(v)
  if (!object(v) || v.mission_id !== mission || v.finding_id !== finding || !uuid(v.id) || revision && v.id !== revision || !uuid(v.thread_id) || !uuid(v.attempt_id) || !hex(v.call_id, 32) || !text(v.event_id, 20, true) || !/^\d+$/.test(v.event_id) || !integer(v.revision) || v.status !== 'candidate' || !text(v.number, 23) || !/^WSC-\d{6,19}$/.test(v.number) || !date(v.created_at) || !date(v.call_started_at) || !date(v.call_finished_at) || !hex(v.content_sha256, 64)) throw invalid()
  const c = v.content
  if (!object(c) || !text(c.title, 256, true) || !text(c.description, 8192, true) || !text(c.remediation, 4096) || !text(c.limitations, 4096) || !Array.isArray(c.steps) || c.steps.length < 1 || c.steps.length > 16 || !Array.isArray(c.evidence) || c.evidence.length < 1 || c.evidence.length > 16 || !c.evidence.every(reference)) throw invalid()
  const refs = c.evidence as { kind: string; id: string; relation: string }[]
  if (new Set(refs.map(e => `${e.kind}:${e.id}`)).size !== refs.length || !Array.isArray(v.evidence) || v.evidence.length !== refs.length) throw invalid()
  if (!c.steps.every(step => object(step) && text(step.text, 1024, true) && Array.isArray(step.evidence_indices) && step.evidence_indices.length <= 16 && step.evidence_indices.every(n => integer(n, 0, refs.length - 1)) && new Set(step.evidence_indices).size === step.evidence_indices.length)) throw invalid()
  for (const [i, e] of v.evidence.entries()) {
    if (!object(e) || !reference(e) || e.kind !== refs[i].kind || e.id !== refs[i].id || e.relation !== refs[i].relation || e.thread_id !== v.thread_id || !(e.attempt_id === '' || uuid(e.attempt_id)) || !(e.action_id === '' || uuid(e.action_id)) || !date(e.created_at) || !date(e.captured_at) || !hex(e.content_sha256, 64) || !text(e.state, 32) || !['', 'http', 'local_command'].includes(String(e.action_kind)) || !(e.request_hash === '' || hex(e.request_hash, 64)) || !['has_result', 'truncated', 'effect_unknown'].every(k => typeof e[k] === 'boolean')) throw invalid()
  }
  return v as unknown as FindingRevision
}
export function validateFindingHistory(v: unknown, mission: string, finding: string): FindingHistoryPage {
  bounded(v)
  if (!object(v) || v.mission_id !== mission || v.finding_id !== finding || !text(v.next_cursor, 1024) || !Array.isArray(v.items) || v.items.length > 50) throw invalid()
  if (!v.items.every(x => object(x) && uuid(x.id) && x.mission_id === mission && x.finding_id === finding && uuid(x.thread_id) && uuid(x.attempt_id) && hex(x.call_id, 32) && text(x.event_id, 20, true) && /^\d+$/.test(x.event_id) && integer(x.revision) && text(x.title, 256, true) && date(x.created_at)) || new Set(v.items.map(x => x.id)).size !== v.items.length) throw invalid()
  return v as unknown as FindingHistoryPage
}
export function validateFindingEvidence(v: unknown, revision: FindingRevision, index: number): FindingEvidenceContent {
  bounded(v)
  const expected = revision.evidence[index]
  if (!expected || !object(v) || v.mission_id !== revision.mission_id || v.finding_id !== revision.finding_id || v.revision_id !== revision.id || v.index !== index || !object(v.reference) || typeof v.matches_snapshot !== 'boolean' || !object(v.thread) || v.thread.id !== revision.thread_id || v.thread.mission_id !== revision.mission_id) throw invalid()
  const actualReference = v.reference
  if (Object.keys(expected).some(k => actualReference[k] !== expected[k as keyof FindingEvidence])) throw invalid()
  if (expected.kind === 'action') {
    if (!object(v.action) || v.observation !== undefined || v.action.id !== expected.id || v.action.thread_id !== revision.thread_id || !object(v.action.input) || v.action.input.kind !== expected.action_kind || v.action.request_hash !== expected.request_hash || !['pending', 'approved', 'running', 'succeeded', 'failed', 'rejected', 'outcome_unknown'].includes(String(v.action.status))) throw invalid()
  } else if (!object(v.observation) || v.action !== undefined || v.observation.id !== expected.id || v.observation.thread_id !== revision.thread_id || !text(v.observation.content, 131072) || v.observation.action_id !== (expected.action_id || undefined) && v.observation.action_id !== expected.action_id) throw invalid()
  return v as unknown as FindingEvidenceContent
}
const base = (mission: string, finding = '') => `/missions/${idPath(mission)}/findings${finding ? '/' + idPath(finding) : ''}`
export async function fetchFindings(mission: string, thread: string, cursor: string, signal: AbortSignal) { return validateFindingsPage(await requestApi<unknown>(`${base(mission)}?${new URLSearchParams({ thread_id: thread, cursor, limit: '25' })}`, { signal, safeErrors: true }), mission, thread) }
export async function fetchFinding(mission: string, finding: string, revision: string, signal: AbortSignal) { return validateFindingRevision(await requestApi<unknown>(base(mission, finding) + (revision ? `/revisions/${idPath(revision)}` : ''), { signal, safeErrors: true }), mission, finding, revision) }
export async function fetchFindingSummary(mission: string, finding: string, signal: AbortSignal) { const v = await requestApi<unknown>(base(mission, finding) + '/summary', { signal, safeErrors: true }); if (!validFindingSummary(v, mission, finding)) throw invalid(); return v }
export async function fetchFindingHistory(mission: string, finding: string, cursor: string, signal: AbortSignal) { return validateFindingHistory(await requestApi<unknown>(base(mission, finding) + `/history?${new URLSearchParams({ cursor, limit: '25' })}`, { signal, safeErrors: true }), mission, finding) }
export async function fetchFindingEvidence(revision: FindingRevision, index: number, signal: AbortSignal) { return validateFindingEvidence(await requestApi<unknown>(base(revision.mission_id, revision.finding_id) + `/revisions/${idPath(revision.id)}/evidence/${index}`, { signal, safeErrors: true }), revision, index) }
