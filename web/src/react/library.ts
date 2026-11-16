import { ApiError, StaleSessionError } from './api.ts'
import { generateOperationKey } from './idempotency.ts'
import { deleteLibraryItem, fetchLibraryReceipt, putLibraryItem, type LibraryMutation, type LibraryWrite } from './libraryApi.ts'
import { validateMaterial, type MaterialDetail, type MaterialDraft } from '../ui/materials.ts'
export interface LibraryDraft { id: string; expectedToken?: string; value: MaterialDraft }
export interface LibraryPending { id: string; key: string; method: 'PUT' | 'DELETE'; body: LibraryWrite | { expected_token: string }; uncertain: boolean }
interface LibraryState { draft: LibraryDraft | null; pending: LibraryPending | null; phase: 'idle' | 'writing' | 'checking' | 'unknown'; error: string; receipt: LibraryMutation | null; revision: number }
const initial = (): LibraryState => ({ draft: null, pending: null, phase: 'idle', error: '', receipt: null, revision: 0 })
/** App级内存：离页不丢草稿或写入身份；重新认证只遮蔽，显式退出清空。 */
export class LibrarySession {
  private state = initial()
  private listeners = new Set<() => void>()
  private epoch = 0
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener) } }
  snapshot = () => this.state
  private update(patch: Partial<LibraryState>) { this.state = { ...this.state, ...patch }; this.listeners.forEach(listener => listener()) }
  newDraft(kind: 'file' | 'folder', parent_id: string) {
    if (this.state.pending || this.state.draft) return
    this.update({ draft: { id: generateOperationKey(), value: { kind, parent_id, classification: 'material', name: '', description: '', content: '' } }, error: '', receipt: null })
  }
  edit(detail: MaterialDetail) {
    if (this.state.pending || this.state.draft) return false
    const { kind, parent_id, name, description, content, classification } = detail
    this.update({ draft: { id: detail.id, expectedToken: detail.token, value: { kind, parent_id, name, description, content, classification: classification || 'material' } }, error: '', receipt: null }); return true
  }
  change(value: MaterialDraft) { if (!this.state.pending && this.state.draft) this.update({ draft: { ...this.state.draft, value }, error: '' }) }
  error(error: string) { this.update({ error }) }
  discard() { if (!this.state.pending) this.update({ draft: null, error: '' }) }
  clear() { this.epoch++; this.state = initial(); this.listeners.forEach(listener => listener()) }
  mask() {
    this.epoch++
    this.update({ receipt: null, error: this.state.pending ? '登录后请先核对原写入回执。' : '', ...(this.state.pending ? { pending: { ...this.state.pending, uncertain: true }, phase: 'unknown' as const } : {}) })
  }
  async save() {
    if (this.state.pending || !this.state.draft) return
    const draft = this.state.draft, error = validateMaterial(draft.value)
    if (error) return this.error(error)
    const pending: LibraryPending = { id: draft.id, key: generateOperationKey(), method: 'PUT', body: { ...draft.value, name: draft.value.name.trim(), ...(draft.expectedToken ? { expected_token: draft.expectedToken } : {}) }, uncertain: false }
    await this.write(pending)
  }
  async remove(detail: MaterialDetail) {
    if (this.state.pending || this.state.draft) return
    await this.write({ id: detail.id, key: generateOperationKey(), method: 'DELETE', body: { expected_token: detail.token }, uncertain: false })
  }
  private accept(receipt: LibraryMutation, pending: LibraryPending) {
    if (receipt.key !== pending.key || receipt.item_id !== pending.id || receipt.deleted !== (pending.method === 'DELETE')) throw new Error('回执归属与原写入不一致。')
    this.update({ pending: null, draft: null, phase: 'idle', error: '', receipt, revision: this.state.revision + 1 })
  }
  private async write(pending: LibraryPending) {
    const epoch = this.epoch
    this.update({ pending, phase: 'writing', error: '', receipt: null })
    try {
      const receipt = pending.method === 'PUT' ? await putLibraryItem(pending.id, pending.key, pending.body as LibraryWrite) : await deleteLibraryItem(pending.id, pending.key, pending.body.expected_token!)
      if (epoch !== this.epoch) return
      this.accept(receipt, pending)
    } catch (error) {
      if (epoch !== this.epoch) return
      // 仅首次确定拒绝可回到编辑；未知后任何响应都不能当作原请求未发送证明。
      if (!pending.uncertain && error instanceof ApiError && [400, 404, 409].includes(error.status)) {
        this.update({ pending: null, phase: 'idle', error: error.status === 409 ? `资料已变化、名称冲突或目录非空；请刷新后处理。${error.message}` : error.message })
      } else this.update({ pending: { ...pending, uncertain: true }, phase: 'unknown', error: error instanceof StaleSessionError ? '登录后请核对原写入回执。' : '写入结果待核对；不会自动重发。' })
    }
  }
  async check() {
    const pending = this.state.pending
    if (!pending || this.state.phase === 'writing' || this.state.phase === 'checking') return
    const epoch = this.epoch
    this.update({ phase: 'checking', error: '' })
    try {
      const receipt = await fetchLibraryReceipt(pending.key)
      if (epoch === this.epoch) this.accept(receipt, pending)
    } catch (error) {
      if (epoch !== this.epoch) return
      this.update({ phase: 'unknown', error: error instanceof ApiError && error.status === 404 ? '此刻未查到已提交回执，不代表原请求未发送或不会提交。请稍后继续核对，不会重发原写入。' : '未能核对回执，已保留原请求身份。' })
    }
  }
}
