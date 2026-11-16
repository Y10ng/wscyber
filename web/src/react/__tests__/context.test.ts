import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { validateCallContext, fetchCallContext } from '../contextApi.ts'
import { ApiError, StaleSessionError, invalidateSession, setMemoryToken, setOnUnauthorized, hasToken } from '../api.ts'
const id = '11111111-1111-1111-1111-111111111111'
const call = { id: 'a'.repeat(32), mission_id: id, thread_id: id, attempt_id: id }
const fixture = () => ({ call_id: call.id, mission_id: id, thread_id: id, recorded: true, assembly: { policy: 'bounded-history-1', mission_id: id, thread_id: id, attempt_id: id, assembled_at: '2026-11-12T21:00:00Z', input_bytes: 1000, max_input_bytes: 262144, sources: [{ kind: 'attempt', id, at: '2026-11-12T21:00:00Z', included: true, required: true, reason: 'selected', original_bytes: 0, included_bytes: 200, state: 'running' }], windows: ['observation', 'action', 'attempt'].map(kind => ({ kind, limit: kind === 'observation' ? 32 : 8, older_exists: false })) } })
const response = (x: unknown, status = 200) => new Response(JSON.stringify(x), { status })
describe('C3-A真实装配读取契约', () => {
  it('旧调用明确未记录，拒绝伪造归属/重复引用/无界或矛盾字段', () => {
    assert.equal(validateCallContext(fixture(), call).recorded, true)
    assert.equal(validateCallContext({ ...fixture(), recorded: false, assembly: null }, call).recorded, false)
    assert.throws(() => validateCallContext({ ...fixture(), mission_id: 'foreign' }, call), /归属/)
    for (const mutate of [(x: ReturnType<typeof fixture>) => { x.assembly.sources.push(x.assembly.sources[0]) }, (x: ReturnType<typeof fixture>) => { x.assembly.sources[0].included = false }, (x: ReturnType<typeof fixture>) => { x.assembly.input_bytes = 262145 }, (x: ReturnType<typeof fixture>) => { x.assembly.windows[0].older_exists = true }, (x: ReturnType<typeof fixture>) => { x.assembly.attempt_id = 'foreign' }]) { const x = fixture(); mutate(x); assert.throws(() => validateCallContext(x, call), /无效/) }
  })
  it('唯一显式GET有归属/no-store，不读原文或访问来源', async () => {
    const original = globalThis.fetch, seen: { url: string; init?: RequestInit }[] = []; setMemoryToken('fixture')
    globalThis.fetch = async (input, init) => { seen.push({ url: String(input), init }); return response(fixture()) }
    try { await fetchCallContext(call, new AbortController().signal); assert.equal(seen.length, 1); assert.ok(seen[0].url.endsWith(`/context?mission_id=${id}`)); assert.equal(seen[0].init?.cache, 'no-store'); assert.equal(seen[0].init?.method, 'GET') } finally { globalThis.fetch = original }
  })
  it('关闭/过期会话/旧401不能恢复装配内容，当前401清会话', async () => {
    const original = globalThis.fetch; let resolve!: (r: Response) => void; let unauthorized = 0
    globalThis.fetch = async () => new Promise<Response>(done => { resolve = done }); setOnUnauthorized(() => { unauthorized++ })
    try {
      setMemoryToken('fixture'); const controller = new AbortController(); const closed = fetchCallContext(call, controller.signal); controller.abort(); resolve(response(fixture())); await assert.rejects(closed, e => e instanceof ApiError)
      const old = fetchCallContext(call, new AbortController().signal); invalidateSession(false); setMemoryToken('new'); resolve(response({}, 401)); await assert.rejects(old, e => e instanceof StaleSessionError); assert.equal(unauthorized, 0); assert.equal(hasToken(), true)
      const current = fetchCallContext(call, new AbortController().signal); resolve(response({}, 401)); await assert.rejects(current, e => e instanceof ApiError && e.status === 401); assert.equal(unauthorized, 1); assert.equal(hasToken(), false)
    } finally { globalThis.fetch = original; setOnUnauthorized(null) }
  })
})
