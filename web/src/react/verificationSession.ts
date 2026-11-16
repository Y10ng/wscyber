import { ApiError, requestApi } from './api.ts'
import { generateOperationKey } from './idempotency.ts'
import { fetchVerification, verificationPath } from './verificationApi.ts'
interface Pending { key: string; mission: string; finding: string; source: string; maxSteps: number; body: string; path: string; uncertain: boolean }
interface Accepted { mission: string; finding: string; id: string }
interface State { pending: Pending | null; phase: 'idle' | 'writing' | 'unknown' | 'checking'; error: string; last: Accepted | null; revision: number; retryAllowed: boolean }
const initial = (): State => ({ pending: null, phase: 'idle', error: '', last: null, revision: 0, retryAllowed: false })
/** App级内存：401/关闭只遮蔽，不丢键；退出清空。未知永不自动POST。 */
export class VerificationSession {
  private state = initial(); private epoch = 0; private listeners = new Set<() => void>()
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn) } }
  snapshot = () => this.state
  private update(p: Partial<State>) { this.state = { ...this.state, ...p }; this.listeners.forEach(f => f()) }
  clear() { this.epoch++; this.state = initial(); this.listeners.forEach(f => f()) }
  mask() { this.epoch++; this.update({ last: null, error: '', retryAllowed: false, ...(this.state.pending ? { phase: 'unknown' as const, pending: { ...this.state.pending, uncertain: true } } : {}) }) }
  async start(mission: string, finding: string, source: string, maxSteps: number) {
    if (this.state.pending) return
    await this.write({ key: generateOperationKey(), mission, finding, source, maxSteps, body: JSON.stringify({ source_revision_id: source, max_steps: maxSteps }), path: verificationPath(mission, finding), uncertain: false })
  }
  private accept(p: Pending, id: string) { this.update({ pending: null, phase: 'idle', error: '', retryAllowed: false, last: { mission: p.mission, finding: p.finding, id }, revision: this.state.revision + 1 }) }
  private async write(p: Pending) {
    const epoch = this.epoch; let submitted = false; this.update({ pending: p, phase: 'writing', error: '', last: null, retryAllowed: false })
    try {
      const r = await requestApi<Record<string, unknown>>(p.path, { method: 'POST', rawBody: p.body, headers: { 'Idempotency-Key': p.key }, safeErrors: true })
      if (epoch !== this.epoch) return
      submitted = true
      if (r.key !== p.key || r.mission_id !== p.mission || r.finding_id !== p.finding || r.source_revision_id !== p.source || r.start_status !== 'accepted' || typeof r.verification_id !== 'string') throw Error('回执不匹配')
      const v = await fetchVerification(p.mission, p.finding, r.verification_id)
      if (v.source_revision_id !== p.source || v.max_steps !== p.maxSteps || v.thread_id !== r.thread_id) throw Error('验证归属不匹配')
      if (epoch === this.epoch) this.accept(p, v.id)
    } catch (e) {
      if (epoch !== this.epoch) return
      if (!submitted && !p.uncertain && e instanceof ApiError && [400, 404].includes(e.status)) { this.update({ pending: null, phase: 'idle', error: e.message }); return }
      // 即使409也可能是锁等待/另一个同键请求，不据此换键。
      this.update({ pending: { ...p, uncertain: true }, phase: 'unknown', error: e instanceof ApiError && [400, 404, 409].includes(e.status) ? `${e.message}；先核对原提交，候选可能已更新。` : '提交结果待核对，不会自动重发。' })
    }
  }
  async check() {
    const p = this.state.pending; if (!p || ['writing', 'checking'].includes(this.state.phase)) return
    const epoch = this.epoch; this.update({ phase: 'checking', error: '', retryAllowed: false }); let receipt = false
    try {
      const r = await requestApi<Record<string, unknown>>(`/operations/${p.key}`, { safeErrors: true }); receipt = true
      if (r.key !== p.key || r.method !== 'POST' || r.path !== '/api' + p.path || r.resource_type !== 'verification' || r.response_status !== 202 || typeof r.resource_id !== 'string') throw Error('回执归属不匹配')
      const v = await fetchVerification(p.mission, p.finding, r.resource_id)
      if (v.source_revision_id !== p.source || v.max_steps !== p.maxSteps) throw Error('固定输入不匹配')
      if (epoch === this.epoch) this.accept(p, v.id)
    } catch (e) { if (epoch === this.epoch) this.update({ phase: 'unknown', retryAllowed: !receipt && e instanceof ApiError && e.status === 404, error: !receipt && e instanceof ApiError && e.status === 404 ? '此刻未见回执，不证明原提交未发生。可继续核对或显式同键重试。' : '核对未完成或归属不一致，原键保留。' }) }
  }
  async retry() { const p = this.state.pending; if (p && this.state.phase === 'unknown' && this.state.retryAllowed) await this.write(p) }
}
