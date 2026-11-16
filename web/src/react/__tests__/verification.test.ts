import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { VerificationSession } from '../verificationSession.ts'
import { setMemoryToken } from '../api.ts'
import { verificationFixture, verificationResultFixture } from '../../component-preview/VerificationExamples.tsx'
import { validateVerificationDetail, validateVerificationPage, validateVerificationEvidence } from '../verificationApi.ts'
const v = verificationFixture
const detail = () => ({ ...v, execution_error: '', thread: { id: v.thread_id, mission_id: v.mission_id, status: v.status, pause_requested: v.pause_requested, auto_run: v.auto_run, step_count: v.step_count, next_step: '' }, result: structuredClone(verificationResultFixture) })
const response = (x: unknown, status = 200) => new Response(JSON.stringify(x), { status })
describe('D2独立验证与可靠提交', () => {
 it('固定归属、预算终止不是模型结论，结果引用不接受串方向', () => {
  const d = detail(); assert.equal(validateVerificationDetail(d, v.mission_id, v.finding_id, v.id).verdict, 'supported')
  assert.throws(() => validateVerificationDetail({ ...d, mission_id: 'foreign' }, v.mission_id, v.finding_id, v.id))
  assert.throws(() => validateVerificationDetail({ ...d, end_reason: 'budget_exhausted' }, v.mission_id, v.finding_id, v.id))
  d.result.evidence[0].thread_id = 'foreign'; assert.throws(() => validateVerificationDetail(d, v.mission_id, v.finding_id, v.id))
  assert.throws(() => validateVerificationPage({ mission_id: v.mission_id, finding_id: v.finding_id, items: [v, v], next_cursor: '' }, v.mission_id, v.finding_id))
 })
 it('结果原件只接受固定引用及当前验证归属', () => {
  const d = validateVerificationDetail(detail(), v.mission_id, v.finding_id, v.id), ref = d.result!.evidence[0]
  const value = { mission_id: d.mission_id, finding_id: d.finding_id, verification_id: d.id, index: 0, reference: ref, matches_snapshot: true, thread: d.thread, action: { id: ref.id, thread_id: d.thread_id, input: { kind: ref.action_kind }, request_hash: ref.request_hash } }
  assert.equal(validateVerificationEvidence(value, d, 0).matches_snapshot, true)
  assert.throws(() => validateVerificationEvidence({ ...value, verification_id: 'foreign' }, d, 0))
  assert.throws(() => validateVerificationEvidence({ ...value, reference: { ...ref, content_sha256: 'f'.repeat(64) } }, d, 0))
 })
 it('失联、409、404均不换键；只有显式同键重试会再次POST', async () => {
  const original = globalThis.fetch, session = new VerificationSession(), calls: RequestInit[] = []; let mode = 'lost'; setMemoryToken('fixture')
  globalThis.fetch = async (_u, init) => { calls.push(init!); if (mode === 'lost') throw Error('lost'); return response({}, mode === 'missing' ? 404 : 409) }
  try {
   await session.start(v.mission_id, v.finding_id, v.source_revision_id, 6); const p = session.snapshot().pending!; await session.start(v.mission_id, v.finding_id, v.source_revision_id, 1); assert.equal(calls.length, 1)
   mode = 'missing'; await session.check(); assert.equal(calls[1].method, 'GET'); assert.equal(session.snapshot().pending?.key, p.key)
   mode = 'conflict'; await session.retry(); assert.equal(calls[2].body, calls[0].body); assert.deepEqual(calls[2].headers, calls[0].headers); assert.equal(session.snapshot().phase, 'unknown')
  } finally { globalThis.fetch = original }
 })
 it('核对须匹配完整回执与固定输入；不自动POST', async () => {
  const original = globalThis.fetch, session = new VerificationSession(); let wrong = true, writes = 0; setMemoryToken('fixture')
  globalThis.fetch = async (u, init) => { if (init?.method === 'POST') { writes++; throw Error('lost') } const p = session.snapshot().pending!; if (String(u).includes('/operations/')) return response({ key: p.key, method: 'POST', path: '/api' + p.path, resource_type: 'verification', resource_id: v.id, response_status: 202 }); return response({ ...detail(), source_revision_id: wrong ? 'foreign' : v.source_revision_id }) }
  try { await session.start(v.mission_id, v.finding_id, v.source_revision_id, 6); await session.check(); assert.ok(session.snapshot().pending); wrong = false; await session.check(); assert.equal(session.snapshot().pending, null); assert.equal(writes, 1) } finally { globalThis.fetch = original }
 })
 it('成功POST后读取400不能丢键；初始输入400可更正', async () => {
  const original = globalThis.fetch, session = new VerificationSession(); let direct = true; setMemoryToken('fixture')
  globalThis.fetch = async (_u, init) => { const p = session.snapshot().pending!; return init?.method === 'POST' && !direct ? response({ key: p.key, mission_id: p.mission, finding_id: p.finding, source_revision_id: p.source, verification_id: v.id, thread_id: v.thread_id, start_status: 'accepted' }, 202) : response({}, 400) }
  try { await session.start(v.mission_id, v.finding_id, v.source_revision_id, 6); assert.equal(session.snapshot().pending, null); direct = false; await session.start(v.mission_id, v.finding_id, v.source_revision_id, 6); assert.ok(session.snapshot().pending); assert.equal(session.snapshot().phase, 'unknown') } finally { globalThis.fetch = original }
 })
 it('401遮蔽保留键，退出后的迟到响应不恢复状态', async () => {
  const original = globalThis.fetch, session = new VerificationSession(); let resolve!: (r: Response) => void; setMemoryToken('fixture'); globalThis.fetch = async () => new Promise<Response>(r => { resolve = r })
  try { const work = session.start(v.mission_id, v.finding_id, v.source_revision_id, 6), key = session.snapshot().pending?.key; session.mask(); assert.equal(session.snapshot().pending?.key, key); assert.equal(session.snapshot().phase, 'unknown'); session.clear(); resolve(response({}, 202)); await work; assert.equal(session.snapshot().pending, null); assert.equal(session.snapshot().last, null) } finally { globalThis.fetch = original }
 })
})
