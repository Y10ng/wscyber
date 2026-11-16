import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { fetchSkillActivities, fetchSkillCalls, fetchSkillLoad, fetchSkillUsage } from '../skillsApi.ts'
import { ApiError, StaleSessionError, hasToken, invalidateSession, setMemoryToken, setOnUnauthorized } from '../api.ts'
import { LibrarySession } from '../library.ts'
const mission = '11111111-1111-1111-1111-111111111111', load = '22222222-2222-2222-2222-222222222222'
const response = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status })
describe('S1 Skill读取与显式分类边界', () => {
  it('只读分页不读正文，保留大整数字符串、同源与no-store', async () => {
    const original = globalThis.fetch, seen: { url: string; init?: RequestInit }[] = []; setMemoryToken('fixture')
    globalThis.fetch = async (input, init) => { seen.push({ url: String(input), init }); return response({ mission_id: mission, total_successful_loads: '9007199254740993', items: [], next_cursor: '' }) }
    try {
      const value = await fetchSkillUsage(mission, 'a'.repeat(32), new AbortController().signal)
      assert.equal(value.total_successful_loads, '9007199254740993'); assert.equal(seen.length, 1)
      assert.ok(seen[0].url.includes('/skill-usage?after=')); assert.ok(seen[0].url.endsWith('&limit=25'))
      assert.equal(seen[0].init?.method, 'GET'); assert.equal(seen[0].init?.cache, 'no-store'); assert.equal(seen[0].init?.credentials, 'same-origin')
    } finally { globalThis.fetch = original }
  })
  it('项目/活动/加载/调用归属错配均拒绝，不把别的项目显示到当前详情', async () => {
    const original = globalThis.fetch; setMemoryToken('fixture'); let payload: unknown
    globalThis.fetch = async () => response(payload)
    const signal = new AbortController().signal
    try {
      payload = { mission_id: 'other', items: [], next_cursor: '' }; await assert.rejects(fetchSkillUsage(mission, '', signal), /当前项目/)
      payload = { mission_id: mission, items: [{ mission_id: 'other' }], next_cursor: '' }; await assert.rejects(fetchSkillActivities(mission, '', signal), /当前项目/)
      payload = { mission_id: mission, load_id: 'other' }; await assert.rejects(fetchSkillLoad(mission, load, signal), /当前加载/)
      payload = { mission_id: mission, load_id: load, items: [{ mission_id: 'other' }], next_cursor: '' }; await assert.rejects(fetchSkillCalls(mission, load, '', signal), /当前项目或加载/)
    } finally { globalThis.fetch = original }
  })
  it('关闭取消与logout迟到响应丢弃；旧401不能注销新会话', async () => {
    const original = globalThis.fetch; let resolve!: (value: Response) => void; let masked = 0
    globalThis.fetch = async () => new Promise<Response>(done => { resolve = done }); setOnUnauthorized(() => { masked++ })
    try {
      setMemoryToken('fixture'); const controller = new AbortController()
      const closed = fetchSkillLoad(mission, load, controller.signal); controller.abort()
      resolve(response({ mission_id: mission, load_id: load, content: '迟到正文' })); await assert.rejects(closed, error => error instanceof ApiError && error.status === 0)
      const old = fetchSkillLoad(mission, load, new AbortController().signal)
      invalidateSession(false); setMemoryToken('new-fixture'); resolve(response({}, 401))
      await assert.rejects(old, error => error instanceof StaleSessionError); assert.equal(masked, 0); assert.equal(hasToken(), true)
      const current = fetchSkillUsage(mission, '', new AbortController().signal); resolve(response({}, 401))
      await assert.rejects(current, error => error instanceof ApiError && error.status === 401); assert.equal(masked, 1); assert.equal(hasToken(), false)
    } finally { globalThis.fetch = original; setOnUnauthorized(null) }
  })
  it('新建默认为普通资料；显式Skill失联后原分类和请求体固定，只GET核对', async () => {
    const original = globalThis.fetch; setMemoryToken('fixture'); const calls: { method: string; body?: string }[] = []
    globalThis.fetch = async (_input, init) => { calls.push({ method: init?.method || 'GET', body: init?.body as string | undefined }); if (init?.method === 'PUT') throw new Error('lost'); return response({}, 404) }
    try {
      const session = new LibrarySession(); session.newDraft('file', ''); assert.equal(session.snapshot().draft?.value.classification, 'material')
      session.change({ parent_id: '', kind: 'file', name: 'SKILL.md', description: '适用本机检查', content: '只读方法', classification: 'skill' })
      await session.save(); const pending = session.snapshot().pending; assert.ok(pending)
      session.mask(); await session.check(); assert.equal(session.snapshot().pending?.key, pending.key)
      assert.equal(session.snapshot().draft?.value.classification, 'skill'); assert.deepEqual(calls.map(c => c.method), ['PUT', 'GET'])
      assert.equal(JSON.parse(calls[0].body!).classification, 'skill')
    } finally { globalThis.fetch = original }
  })
})
