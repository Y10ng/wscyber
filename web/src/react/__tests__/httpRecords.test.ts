import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { renderToStaticMarkup } from 'react-dom/server'
import React from 'react'
import { emptyHTTPRecordFilters, httpRecordQuery, responseStatusText, validHTTPRecordDetail, validHTTPRecordPage } from '../httpRecords.ts'
import type { HTTPRecordDetail, HTTPRecordSummary, Mission } from '../types.ts'
import { fetchHTTPRecordDetail, fetchHTTPRecords, requestApi, setMemoryToken, invalidateSession, hasToken } from '../api.ts'
import { HTTPRecordsWorkspace } from '../components/HTTPRecordsWorkspace.tsx'

const record: HTTPRecordSummary = {
  id: 'action-1', mission_id: 'mission-1', thread_id: 'thread-1', thread_title: '方向', method: 'GET', url: 'http://example.test/path',
  status: 'succeeded', has_result: true, status_code: 200, truncated: false, effect_unknown: false,
  created_at: '2026-11-11T00:00:00Z', updated_at: '2026-11-11T00:00:00Z',
}
const mission: Mission = { id: 'mission-1', title: '项目', goal: '目标', origin: '', allowed_origins: ['http://example.test'], created_at: record.created_at }
const detail = {
  mission_id: mission.id,
  thread: { id: record.thread_id, mission_id: mission.id },
  action: { id: record.id, thread_id: record.thread_id, input: { kind: 'http' } },
} as HTTPRecordDetail

describe('TF-1 HTTP记录契约', () => {
  it('固定25条并编码筛选和游标，不将内容加入URL', () => {
    const query = new URLSearchParams(httpRecordQuery({ thread_id: '方向/&', status: 'failed', method: 'M-SEARCH' }, 'cursor_-'))
    assert.deepEqual(Object.fromEntries(query), { limit: '25', thread_id: '方向/&', status: 'failed', method: 'M-SEARCH', cursor: 'cursor_-' })
    assert.equal(httpRecordQuery(emptyHTTPRecordFilters), 'limit=25')
  })
  it('接受空页与合法的已保存HTTP摘要，不制造响应', () => {
    assert.equal(validHTTPRecordPage({ items: [], next_cursor: '' }, mission.id, emptyHTTPRecordFilters), true)
    for (const status_code of [undefined, 100, 200, 599, 700, 999]) {
      assert.equal(validHTTPRecordPage({ items: [{ ...record, status_code }], next_cursor: '' }, mission.id, emptyHTTPRecordFilters), true)
    }
    assert.equal(responseStatusText({ has_result: false }), '尚无执行回执')
    assert.equal(responseStatusText({ has_result: true }), '未保存响应状态码')
    assert.equal(responseStatusText(record), 'HTTP 200')
  })
  it('拒绝跨项目、重复ID、超长页与非法结构，不静默丢行', () => {
    for (const items of [[{ ...record, mission_id: 'other' }], [record, record], Array.from({ length: 26 }, (_, i) => ({ ...record, id: String(i) })), [{ ...record, status: 'invented' }], [{ ...record, status_code: 0 }]]) {
      assert.equal(validHTTPRecordPage({ items, next_cursor: '' }, mission.id, emptyHTTPRecordFilters), false)
    }
    assert.equal(validHTTPRecordPage({ items: null, next_cursor: '' } as never, mission.id, emptyHTTPRecordFilters), false)
  })
  it('核对筛选归属且不把原method大小写当错误', () => {
    const filters = { thread_id: record.thread_id, status: record.status, method: 'GET' }
    assert.equal(validHTTPRecordPage({ items: [{ ...record, method: 'get' }], next_cursor: '' }, mission.id, filters), true)
    for (const change of [{ thread_id: 'other' }, { status: 'failed' }, { method: 'POST' }]) {
      assert.equal(validHTTPRecordPage({ items: [{ ...record, ...change }], next_cursor: '' }, mission.id, filters), false)
    }
  })
  it('详情必须同时满足项目、方向、动作与HTTP类型', () => {
    assert.equal(validHTTPRecordDetail(detail, mission.id, record), true)
    assert.equal(validHTTPRecordDetail({ ...detail, mission_id: 'other' }, mission.id, record), false)
    assert.equal(validHTTPRecordDetail({ ...detail, thread: { ...detail.thread, mission_id: 'other' } }, mission.id, record), false)
    assert.equal(validHTTPRecordDetail({ ...detail, thread: { ...detail.thread, id: 'other' } }, mission.id, record), false)
    assert.equal(validHTTPRecordDetail({ ...detail, action: { ...detail.action, id: 'other' } }, mission.id, record), false)
    assert.equal(validHTTPRecordDetail({ ...detail, action: { ...detail.action, thread_id: 'other' } }, mission.id, record), false)
    assert.equal(validHTTPRecordDetail({ ...detail, action: { ...detail.action, input: { kind: 'local_command', reason: '' } } }, mission.id, record), false)
  })
  it('正式组件无模拟记录、总数、重放或审批操作', () => {
    const html = renderToStaticMarkup(React.createElement(HTTPRecordsWorkspace, { mission, threads: [], active: false }))
    assert.doesNotMatch(html, /仅平台 HTTP 动作|不含工具直连流量/)
    assert.match(html, /刷新记录/)
    assert.match(html, /回到最新/)
    assert.doesNotMatch(html, /example\.test|action-1|重放请求|批准请求|共\s*\d+\s*条/)
  })
  it('两个读取路径使用同源GET、no-store及编码后的身份', async () => {
    const originalFetch = globalThis.fetch
    const requests: Array<{ path: string; options?: RequestInit }> = []
    try {
      setMemoryToken('local-test')
      globalThis.fetch = async (path, options) => { requests.push({ path: String(path), options }); return new Response('{}', { status: 200 }) }
      await fetchHTTPRecords('project/a', 'limit=25')
      await fetchHTTPRecordDetail('project/a', 'action/b')
      assert.deepEqual(requests.map((r) => r.path), ['/api/missions/project%2Fa/http-records?limit=25', '/api/missions/project%2Fa/http-records/action%2Fb'])
      for (const r of requests) { assert.equal(r.options?.method, 'GET'); assert.equal(r.options?.cache, 'no-store'); assert.equal(r.options?.body, undefined) }
    } finally { globalThis.fetch = originalFetch; invalidateSession(false) }
  })
  it('取消传播到fetch，迟到401不能注销当前会话', async () => {
    const originalFetch = globalThis.fetch
    let resolve!: (response: Response) => void
    let signal: AbortSignal | null | undefined
    try {
      setMemoryToken('local-test')
      globalThis.fetch = async (_, options) => { signal = options?.signal; return new Promise<Response>((done) => { resolve = done }) }
      const owner = new AbortController()
      const pending = requestApi('/test-cancel', { signal: owner.signal })
      owner.abort()
      assert.equal(signal?.aborted, true)
      resolve(new Response('{"error":"old request"}', { status: 401 }))
      await assert.rejects(pending, /请求已中止/)
      assert.equal(hasToken(), true)
    } finally { globalThis.fetch = originalFetch; invalidateSession(false) }
  })
})
