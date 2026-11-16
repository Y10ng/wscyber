import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { renderToStaticMarkup } from 'react-dom/server'
import { buildActivityItems, filterActivityItems, resolveActivityAction } from '../activity.ts'
import { ExecutionActivity } from '../components/ExecutionActivity.tsx'
import { splitHTTPURL } from '../components/HTTPRecordsWorkspace.tsx'
import type { Action, Attempt, Mission, Observation, Snapshot, Thread } from '../types.ts'

const mission: Mission = { id: 'm', title: '执行工作面', origin: 'fixture', goal: '核对输出', allowed_origins: ['http://127.0.0.1'], created_at: '2026-11-13T00:00:00Z' }
const thread: Thread = { id: 't', mission_id: mission.id, title: '接口证据', question: '当前问题', rationale: '依据', next_step: '下一步', status: 'blocked', priority: 1, auto_run: false, step_count: 2, created_at: '2026-11-13T00:00:00Z', updated_at: '2026-11-13T00:04:00Z' }
const attempt: Attempt = { id: 'attempt', thread_id: thread.id, status: 'succeeded', summary: '已保存研究输出', error: '', started_at: '2026-11-13T00:00:00Z', ended_at: '2026-11-13T00:00:01Z' }
const action: Action = { id: 'action', thread_id: thread.id, attempt_id: attempt.id, status: 'succeeded', request_hash: 'hash', decision_reason: '', error: '', created_at: '2026-11-13T00:00:01Z', updated_at: '2026-11-13T00:00:02Z', input: { kind: 'http', method: 'POST', url: 'http://127.0.0.1:8080/api/items?scope=public', headers: { Authorization: 'not-shown' }, body: '{"fixture":true}', reason: '本机受控请求' }, result: { status_code: 200, headers: { 'Content-Type': ['application/json'] }, body_base64: btoa('{"fixture":true}'), exit_code: 0, duration_ms: 13, truncated: false, effect_unknown: false } }
const observation: Observation = { id: 'observation', thread_id: thread.id, attempt_id: attempt.id, action_id: action.id, kind: 'action_result', content: '{"secret":"default-collapsed"}', created_at: '2026-11-13T00:00:02Z' }
const snapshot: Snapshot = { mission, thread, attempts: [attempt], actions: [action], observations: [observation] }

describe('Kimi前端：执行活动与HTTP证据', () => {
  it('时间流包含三类真实记录并支持本地筛选，不修改源集合', () => {
    const before = JSON.stringify(snapshot)
    const items = buildActivityItems(snapshot)
    assert.deepEqual(items.map(item => item.kind), ['observation', 'action', 'attempt'])
    assert.deepEqual(filterActivityItems(items, 'action').map(item => item.id), ['action'])
    assert.deepEqual(filterActivityItems(items, 'attempt').map(item => item.id), ['attempt'])
    assert.equal(JSON.stringify(snapshot), before)
  })
  it('证据打开只接受唯一当前项目与方向关联，拒绝重复和错归属', () => {
    const selection = { missionID: mission.id, threadID: thread.id, actionID: action.id }
    assert.equal(resolveActivityAction(snapshot, selection), action)
    assert.equal(resolveActivityAction({ ...snapshot, actions: [action, action] }, selection), null)
    assert.equal(resolveActivityAction({ ...snapshot, thread: { ...thread, id: 'other' } }, selection), null)
    assert.equal(resolveActivityAction({ ...snapshot, mission: { ...mission, id: 'other' } }, selection), null)
    assert.equal(resolveActivityAction(null, selection), null)
  })
  it('研究首屏把动作置于主要活动流，观察原文与敏感头不默认展开', () => {
    const html = renderToStaticMarkup(<ExecutionActivity mission={mission} snapshot={snapshot} rawSelection={null} onToggleRaw={() => {}} onInspect={() => {}} />)
    for (const label of ['执行活动', 'HTTP 请求', '查看证据', '已保存研究输出', 'POST http://127.0.0.1:8080/api/items?scope=public']) assert.ok(html.includes(label), label)
    assert.doesNotMatch(html, /default-collapsed|not-shown|展开观察原文|已确认漏洞|实时终端|重放请求/)
  })
  it('HTTP地址展示拆分origin与路径，非URL不丢失原字符串', () => {
    assert.deepEqual(splitHTTPURL('http://127.0.0.1:8080/api/items?scope=public'), { origin: 'http://127.0.0.1:8080', path: '/api/items?scope=public' })
    assert.deepEqual(splitHTTPURL('relative-value'), { origin: '', path: 'relative-value' })
  })
})
