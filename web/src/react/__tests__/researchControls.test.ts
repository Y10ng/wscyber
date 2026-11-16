import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { ResearchControlSession } from '../researchControls.ts'
import { setMemoryToken } from '../api.ts'
const owner = { mission: '11111111-1111-4111-8111-111111111111', thread: '22222222-2222-4222-8222-222222222222' }
const draft = { source: '本机用户声明', content: '不可变\r\n<script>文本</script>', share_with_model: true }
describe('C2-B可靠控制与人工补充', () => {
  it('失联只核对，404保留冻结键/载荷；显式同键重试且未知后的冲突不解锁', async () => {
    setMemoryToken('fixture'); const original = globalThis.fetch, calls: { method: string; key?: string; body: unknown }[] = []; let mode = 'lost'
    const session = new ResearchControlSession()
    globalThis.fetch = async (_url, init) => { calls.push({ method: init?.method || 'GET', key: (init?.headers as Record<string, string>)?.['Idempotency-Key'], body: init?.body }); if (mode === 'lost') throw new Error('offline'); return new Response('{}', { status: mode === 'missing' ? 404 : 409 }) }
    try {
      session.begin(owner); session.change(draft); await session.save(); const p = session.snapshot().pending!
      assert.deepEqual(JSON.parse(calls[0].body as string), draft)
      session.change({ ...draft, content: 'tamper' }); assert.equal(session.snapshot().draft?.value.content, draft.content)
      mode = 'missing'; await session.check(); assert.equal(calls[1].method, 'GET'); assert.equal(session.snapshot().pending?.key, p.key)
      assert.equal(calls.filter(c => c.method === 'POST').length, 1); mode = 'conflict'; await session.retry()
      assert.equal(calls[2].body, calls[0].body); assert.equal(calls[2].key, calls[0].key); assert.equal(session.snapshot().phase, 'unknown'); assert.equal(session.snapshot().pending?.key, p.key)
    } finally { globalThis.fetch = original }
  })
  it('核对检查完整方法/路径/类型/状态/资源及真实方向归属，不做自动POST', async () => {
    setMemoryToken('fixture'); const original = globalThis.fetch, session = new ResearchControlSession(); let mode = 'lost', writes = 0
    globalThis.fetch = async (url, init) => {
      if (init?.method === 'POST') { writes++; throw new Error('lost') }
      const p = session.snapshot().pending!
      if (String(url).includes('/operations/')) return new Response(JSON.stringify({ key: p.key, method: 'POST', path: '/api' + p.path, resource_type: 'thread', resource_id: owner.thread, response_status: mode === 'wrong-status' ? 201 : 200 }))
      return new Response(JSON.stringify({ mission: { id: mode === 'wrong-owner' ? 'foreign' : owner.mission }, thread: { id: owner.thread, mission_id: owner.mission } }))
    }
    try {
      await session.control(owner, 'pause'); mode = 'wrong-status'; await session.check(); assert.ok(session.snapshot().pending)
      mode = 'wrong-owner'; await session.check(); assert.ok(session.snapshot().pending)
      mode = 'ok'; await session.check(); assert.equal(session.snapshot().pending, null); assert.equal(writes, 1)
    } finally { globalThis.fetch = original }
  })
  it('401遮蔽保留草稿/提交身份，跨方向拒绝占用，退出后迟到成功失效', async () => {
    setMemoryToken('fixture'); const original = globalThis.fetch, session = new ResearchControlSession(); let resolve!: (r: Response) => void
    globalThis.fetch = async () => new Promise<Response>(done => { resolve = done })
    try {
      session.begin(owner); session.change(draft); const pending = session.save(), p = session.snapshot().pending!
      session.mask(); assert.equal(session.snapshot().phase, 'unknown'); assert.equal(session.snapshot().pending?.key, p.key); assert.equal(session.snapshot().draft?.value.content, draft.content)
      assert.equal(session.begin({ ...owner, thread: 'foreign' }), false)
      session.clear(); session.begin(owner)
      resolve(new Response(JSON.stringify({ key: p.key, mission_id: p.mission, thread_id: p.thread, command: 'supplement', supplement: {} }))); await pending
      assert.equal(session.snapshot().pending, null); assert.equal(session.snapshot().draft?.value.content, '')
    } finally { globalThis.fetch = original }
  })
})
