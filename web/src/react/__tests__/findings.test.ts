import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { findingFixture, findingSummary, findingHistory } from '../../component-preview/FindingExamples.tsx'
import { fetchFinding, fetchFindings, validateFindingEvidence, validateFindingHistory, validateFindingRevision, validateFindingsPage } from '../findingsApi.ts'
import { ApiError, StaleSessionError, invalidateSession, setMemoryToken, setOnUnauthorized, hasToken } from '../api.ts'
import { findingReadDelay } from '../components/FindingsWorkspace.tsx'
import { validateCallContext } from '../contextApi.ts'
const clone = () => structuredClone(findingFixture)
const id = findingFixture.finding_id, mission = findingFixture.mission_id
const response = (x: unknown, status = 200) => new Response(JSON.stringify(x), { status })
describe('D首片候选读取与事实边界', () => {
  it('只接受正确归属和候选状态，列表有界且不重复身份', () => {
    assert.equal(validateFindingsPage({ mission_id: mission, items: [findingSummary], next_cursor: '' }, mission).items.length, 1)
    for (const items of [[{ ...findingSummary, status: 'confirmed' }], [findingSummary, findingSummary], [{ ...findingSummary, mission_id: 'foreign' }], Array(51).fill(findingSummary)]) assert.throws(() => validateFindingsPage({ mission_id: mission, items, next_cursor: '' }, mission), /归属/)
  })
  it('正文与固定证据一一匹配，拒绝错版本/错方向/重复证据/超限/坏索引', () => {
    assert.equal(validateFindingRevision(clone(), mission, id).status, 'candidate')
    const mutations = [(v: ReturnType<typeof clone>) => { v.mission_id = 'foreign' }, (v: ReturnType<typeof clone>) => { v.evidence[0].thread_id = 'foreign' }, (v: ReturnType<typeof clone>) => { v.evidence[0].relation = 'counterevidence' }, (v: ReturnType<typeof clone>) => { v.content.description = 'x'.repeat(8193) }, (v: ReturnType<typeof clone>) => { v.content.steps[0].evidence_indices = [1] }, (v: ReturnType<typeof clone>) => { v.content.evidence.push(v.content.evidence[0]); v.evidence.push(v.evidence[0]) }]
    for (const mutate of mutations) { const v = clone(); mutate(v); assert.throws(() => validateFindingRevision(v, mission, id), /归属/) }
    assert.throws(() => validateFindingRevision(clone(), mission, id, 'foreign'))
  })
  it('历史只有结构化来源，拒绝跨候选或错调用ID', () => {
    const v = { mission_id: mission, finding_id: id, items: [findingHistory], next_cursor: '' }
    assert.equal(validateFindingHistory(v, mission, id).items[0].event_id, '42')
    assert.throws(() => validateFindingHistory({ ...v, items: [{ ...findingHistory, call_id: 'invented' }] }, mission, id))
    assert.throws(() => validateFindingHistory(v, mission, 'foreign'))
  })
  it('原件必须对应当前修订固定引用，不接受错action或伪关联', () => {
    const v = { mission_id: mission, finding_id: id, revision_id: findingFixture.id, index: 0, reference: findingFixture.evidence[0], matches_snapshot: true, thread: { id: findingFixture.thread_id, mission_id: mission }, action: { id: findingFixture.evidence[0].id, thread_id: findingFixture.thread_id, input: { kind: 'http' }, request_hash: findingFixture.evidence[0].request_hash, status: 'outcome_unknown' } }
    assert.equal(validateFindingEvidence(v, findingFixture, 0).matches_snapshot, true)
    assert.throws(() => validateFindingEvidence({ ...v, action: { ...v.action, id: 'foreign' } }, findingFixture, 0))
    assert.throws(() => validateFindingEvidence({ ...v, reference: { ...v.reference, content_sha256: 'a'.repeat(64) } }, findingFixture, 0))
    assert.throws(() => validateFindingEvidence(v, findingFixture, 1))
  })
  it('GET/no-store/同源读取无写入，取消后的数据不能回填', async () => {
    const original = globalThis.fetch, seen: RequestInit[] = []; let resolve!: (r: Response) => void
    setMemoryToken('fixture'); globalThis.fetch = async (_url, init) => { seen.push(init!); return new Promise<Response>(r => { resolve = r }) }
    try {
      const controller = new AbortController(), p = fetchFinding(mission, id, findingFixture.id, controller.signal); controller.abort(); resolve(response(findingFixture)); await assert.rejects(p, e => e instanceof ApiError)
      assert.equal(seen[0].method, 'GET'); assert.equal(seen[0].cache, 'no-store'); assert.equal(seen[0].redirect, 'error')
    } finally { globalThis.fetch = original }
  })
  it('当前401遮蔽会话；旧401不得使新登录失效', async () => {
    const original = globalThis.fetch; let resolve!: (r: Response) => void; let unauthorized = 0
    globalThis.fetch = async () => new Promise<Response>(done => { resolve = done }); setOnUnauthorized(() => { unauthorized++ })
    try {
      setMemoryToken('old'); const old = fetchFindings(mission, '', '', new AbortController().signal); invalidateSession(false); setMemoryToken('new'); resolve(response({}, 401)); await assert.rejects(old, e => e instanceof StaleSessionError); assert.equal(unauthorized, 0); assert.equal(hasToken(), true)
      const current = fetchFindings(mission, '', '', new AbortController().signal); resolve(response({}, 401)); await assert.rejects(current, e => e instanceof ApiError && e.status === 401); assert.equal(unauthorized, 1); assert.equal(hasToken(), false)
    } finally { globalThis.fetch = original; setOnUnauthorized(null) }
  })
  it('读取失败有限退避，不以密集无界重试掩盖问题', () => {
    assert.deepEqual([0, 1, 2, 3, 4].map(findingReadDelay), [5000, 10000, 20000, 30000, null])
  })
  it('新候选装配与旧策略分开，旧调用不追认新窗口', () => {
    const call = { id: 'a'.repeat(32), mission_id: mission, thread_id: id, attempt_id: id }
    const source = { kind: 'finding', id, state: 'candidate', at: findingFixture.created_at, original_bytes: 100, included_bytes: 100, included: true, required: true, reason: 'selected', content_sha256: 'a'.repeat(64) }
    const v = { call_id: call.id, mission_id: mission, thread_id: id, recorded: true, assembly: { policy: 'bounded-history-findings-1', mission_id: mission, thread_id: id, attempt_id: id, assembled_at: findingFixture.created_at, input_bytes: 1000, max_input_bytes: 262144, sources: [source], windows: ['observation', 'action', 'attempt', 'finding'].map(kind => ({ kind, limit: kind === 'observation' ? 32 : 8, older_exists: false })) } }
    assert.equal(validateCallContext(v, call).recorded, true)
    assert.throws(() => validateCallContext({ ...v, assembly: { ...v.assembly, policy: 'bounded-history-1' } }, call))
    assert.throws(() => validateCallContext({ ...v, assembly: { ...v.assembly, sources: [{ ...source, content_sha256: '' }] } }, call))
  })
})
