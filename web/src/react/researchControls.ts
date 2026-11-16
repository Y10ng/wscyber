import { ApiError, requestApi } from './api.ts'
import { generateOperationKey } from './idempotency.ts'
import type { OperationReceipt, Snapshot, Thread } from './types.ts'
import { emptySupplement, supplementError, type ResearchCommand, type SupplementDraft, type SupplementMeta } from '../ui/researchControls.ts'
export interface ResearchOwner { mission: string; thread: string }
export interface ResearchPending extends ResearchOwner { key: string; command: ResearchCommand | 'supplement'; body: string; path: string; uncertain: boolean }
export interface ResearchReceipt { key: string; mission_id: string; thread_id: string; command: string; thread?: Thread; supplement?: SupplementMeta }
interface State { draft: (ResearchOwner & { value: SupplementDraft }) | null; pending: ResearchPending | null; phase: 'idle' | 'writing' | 'checking' | 'unknown'; error: string; notice: string; revision: number; retryAllowed: boolean }
const initial = (): State => ({ draft: null, pending: null, phase: 'idle', error: '', notice: '', revision: 0, retryAllowed: false })
export const researchPath = (owner: ResearchOwner) => `/missions/${encodeURIComponent(owner.mission)}/threads/${encodeURIComponent(owner.thread)}`
export const sameResearchOwner = (a: ResearchOwner | null, b: ResearchOwner) => a?.mission === b.mission && a.thread === b.thread
const isSupplement = (v: SupplementMeta | undefined, owner: ResearchOwner): v is SupplementMeta => Boolean(v && v.mission_id === owner.mission && v.thread_id === owner.thread && /^[0-9a-f-]{36}$/.test(v.id) && v.kind === 'operator_statement' && typeof v.source === 'string' && typeof v.created_at === 'string' && Number.isInteger(v.byte_size) && v.byte_size > 0 && v.byte_size <= 16384 && /^[0-9a-f]{64}$/.test(v.content_sha256))
export async function readSupplement(owner: ResearchOwner, id: string, signal?: AbortSignal) {
  const v = await requestApi<SupplementMeta>(`${researchPath(owner)}/supplements/${id}`, { signal })
  if (!isSupplement(v, owner) || v.id !== id || typeof v.content !== 'string') throw new Error('补充原件归属不匹配。')
  return v
}
export async function listSupplements(owner: ResearchOwner, signal?: AbortSignal) {
  const v = await requestApi<{ mission_id: string; thread_id: string; items: SupplementMeta[] }>(`${researchPath(owner)}/supplements`, { signal })
  if (v.mission_id !== owner.mission || v.thread_id !== owner.thread || !Array.isArray(v.items) || v.items.length > 16 || v.items.some(i => !isSupplement(i, owner) || i.content !== undefined) || new Set(v.items.map(i => i.id)).size !== v.items.length) throw new Error('补充列表归属或结构不匹配。')
  return v.items
}
/** 单App内存队列；冻结原身份/原载荷，关闭与401不丢弃，显式退出才清空。 */
export class ResearchControlSession {
  private state = initial()
  private epoch = 0
  private listeners = new Set<() => void>()
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn) } }
  snapshot = () => this.state
  private update(patch: Partial<State>) { this.state = { ...this.state, ...patch }; this.listeners.forEach(fn => fn()) }
  clear() { this.epoch++; this.state = initial(); this.listeners.forEach(fn => fn()) }
  mask() { this.epoch++; this.update({ notice: '', error: '', retryAllowed: false, ...(this.state.pending ? { pending: { ...this.state.pending, uncertain: true }, phase: 'unknown' as const } : {}) }) }
  begin(owner: ResearchOwner) {
    if (this.state.pending || (this.state.draft && !sameResearchOwner(this.state.draft, owner))) return false
    if (!this.state.draft) this.update({ draft: { ...owner, value: emptySupplement() }, error: '', notice: '' })
    return true
  }
  change(value: SupplementDraft) { if (this.state.draft && !this.state.pending) this.update({ draft: { ...this.state.draft, value: { ...value } }, error: '' }) }
  discard() { if (!this.state.pending) this.update({ draft: null, error: '' }) }
  async save() {
    const draft = this.state.draft
    if (!draft || this.state.pending) return
    const error = supplementError(draft.value); if (error) { this.update({ error }); return }
    await this.start(draft, 'supplement', draft.value)
  }
  async control(owner: ResearchOwner, command: ResearchCommand) { if (!this.state.pending) await this.start(owner, command, { command }) }
  private async start(owner: ResearchOwner, command: ResearchPending['command'], body: object) {
    await this.write({ mission: owner.mission, thread: owner.thread, command, body: JSON.stringify(body), key: generateOperationKey(), path: researchPath(owner) + (command === 'supplement' ? '/supplements' : '/control'), uncertain: false })
  }
  private accepted(p: ResearchPending) {
    this.update({ pending: null, ...(p.command === 'supplement' ? { draft: null } : {}), phase: 'idle', error: '', retryAllowed: false, notice: p.command === 'supplement' ? '补充已保存；是否进入模型调用请查看调用事实。' : '控制已提交；下方显示最新持久状态，不以旧回执覆盖。', revision: this.state.revision + 1 })
  }
  private async write(p: ResearchPending) {
    const epoch = this.epoch
    this.update({ pending: p, phase: 'writing', error: '', notice: '', retryAllowed: false })
    try {
      const v = await requestApi<ResearchReceipt>(p.path, { method: 'POST', headers: { 'Idempotency-Key': p.key }, rawBody: p.body })
      if (epoch !== this.epoch) return
      if (v.key !== p.key || v.mission_id !== p.mission || v.thread_id !== p.thread || v.command !== p.command || (p.command === 'supplement' ? !isSupplement(v.supplement, p) : v.thread?.id !== p.thread || v.thread.mission_id !== p.mission)) throw new Error('回执归属不匹配')
      this.accepted(p)
    } catch (err) {
      if (epoch !== this.epoch) return
      if (!p.uncertain && err instanceof ApiError && [400, 404, 409].includes(err.status)) this.update({ pending: null, phase: 'idle', error: err.message })
      else this.update({ pending: { ...p, uncertain: true }, phase: 'unknown', error: '提交结果待核对，已保留原键和冻结载荷；不会自动重发。' })
    }
  }
  async check() {
    const p = this.state.pending
    if (!p || ['writing', 'checking'].includes(this.state.phase)) return
    const epoch = this.epoch
    this.update({ phase: 'checking', error: '', retryAllowed: false })
    let receiptSeen = false
    try {
      const r = await requestApi<OperationReceipt>(`/operations/${p.key}`)
      receiptSeen = true
      if (epoch !== this.epoch) return
      const supplement = p.command === 'supplement'
      if (r.key !== p.key || r.method !== 'POST' || r.path !== '/api' + p.path || r.resource_type !== (supplement ? 'research_supplement' : 'thread') || r.response_status !== (supplement ? 201 : 200)) throw new Error('回执不属于原提交')
      if (supplement) await readSupplement(p, r.resource_id)
      else {
        if (r.resource_id !== p.thread) throw new Error('方向归属不匹配')
        const snap = await requestApi<Snapshot>(`/threads/${p.thread}`)
        if (snap.mission.id !== p.mission || snap.thread.id !== p.thread || snap.thread.mission_id !== p.mission) throw new Error('方向归属不匹配')
      }
      if (epoch === this.epoch) this.accepted(p)
    } catch (err) {
      if (epoch !== this.epoch) return
      // 404仅开放用户显式同键重试，不声称原请求未发生。
      this.update({ phase: 'unknown', retryAllowed: !receiptSeen && err instanceof ApiError && err.status === 404, error: err instanceof ApiError && err.status === 404 ? '此刻未查到回执或原对象；不证明原请求未提交或不会提交。可继续只读核对。' : '核对未完成或归属不匹配，原提交继续保留。' })
    }
  }
  async retry() { const p = this.state.pending; if (p && this.state.phase === 'unknown' && this.state.retryAllowed) await this.write({ ...p, uncertain: true }) }
}
