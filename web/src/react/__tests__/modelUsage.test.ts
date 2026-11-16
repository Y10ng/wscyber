import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { compactInteger, exactDecimal, integerValue, plotRatio } from '../../ui/exactDecimal.ts'
import { DailyUsageChart, usagePlotMaximum, hasUsageGap, usageEndLabels } from '../../ui/DailyUsageChart.tsx'
import { beginSubmission, initialSubmissionState, isSameExplorationInput } from '../idempotency.ts'
import { emptyUsageFilters, filterError, recentCalendarRange, usageQuery, validPrice, matchesPriceWrite, priceKey, PriceEditSession, type ModelPrice, type PriceWrite } from '../modelUsage.ts'
import { ApiError, StaleSessionError, setMemoryToken, setOnUnauthorized, invalidateSession } from '../api.ts'
import { fetchModelCall, fetchModelCallContent, fetchModelCalls, fetchModelRecording, fetchModelUsage, putModelPrice } from '../usageApi.ts'
import { parsePathname } from '../routing.ts'
const id = 'a'.repeat(32), mission = 'b'.repeat(32)
const price: ModelPrice = { id, revision: 2, source: 'managed', model_config_id: id, protocol: 'openai_chat', model: 'fixture', enabled: true, currency: 'USD', input_per_million: '2', output_per_million: '8', cache_read_per_million: null, cache_write_5m_per_million: '0', cache_write_1h_per_million: null, created_at: '', updated_at: '' }
const write: PriceWrite = { ...price, expected_revision: 1 }

describe('B组精确用量、日历、价格与原文提交身份', () => {
  it('金额与大整数保留全部位数，未知不变成零', () => {
    assert.equal(exactDecimal('9007199254740993123.000001'), '9,007,199,254,740,993,123.000001')
    for (const v of [null, undefined, '-1', 'NaN', '1e8']) assert.equal(exactDecimal(v), '未知')
    assert.equal(exactDecimal('0'), '0'); assert.equal(integerValue('0.5'), null)
    assert.equal(plotRatio('9007199254740993123', 9007199254740993123n), 1)
    assert.equal(plotRatio(null, 1n), null); assert.equal(plotRatio('2', 1n), null)
  })
  it('缺口日留空，零值与空集合分别呈现；共享图不依赖业务', () => {
    const row = { date: '2026-11-12', calls: '1', input: '9007199254740993123', output: '5', missingCalls: '1' }
    assert.ok(usagePlotMaximum([row]) >= BigInt(row.input)); assert.equal(usagePlotMaximum([]), 4n)
    assert.equal(hasUsageGap(row), true); assert.equal(hasUsageGap({ ...row, missingCalls: '0' }), false)
    const html = renderToStaticMarkup(React.createElement(DailyUsageChart, { rows: [row] }))
    assert.ok(html.includes('用量不完整') && html.includes('精确表格') && html.includes('tabindex="0"'))
    assert.doesNotMatch(html, /class="usage-line" d="M/)
    assert.ok(renderToStaticMarkup(React.createElement(DailyUsageChart, { rows: [] })).includes('暂无日用量记录'))
  })
  it('直标邻近末点且碰撞时不越界；极大数轴刻度有界而原数保真', () => {
    assert.deepEqual(usageEndLabels(80, 190), [80, 190])
    assert.deepEqual(usageEndLabels(null, 190), [null, 190])
    assert.deepEqual(usageEndLabels(100, 100), [88, 112])
    assert.deepEqual(usageEndLabels(24, 24), [24, 48])
    assert.deepEqual(usageEndLabels(208, 208), [184, 208])
    assert.deepEqual(usageEndLabels(202, 200), [208, 184])
    assert.equal(compactInteger(9007199254740993123n), '9.0e18')
    assert.equal(compactInteger(10n ** 300n), '1.0e300')
    assert.equal(compactInteger(10000n), '10K')
    assert.equal(compactInteger(0n), '0')
  })
  it('日期/IANA时区/93日约束与DST只处理日历标签', () => {
    assert.equal(filterError(emptyUsageFilters), '')
    assert.ok(filterError({ ...emptyUsageFilters, time_zone: 'not/a-zone' }))
    assert.ok(filterError({ ...emptyUsageFilters, from: '2026-02-30', to: '2026-03-03' }))
    assert.ok(filterError({ ...emptyUsageFilters, from: '2026-01-01', to: '' }))
    assert.equal(filterError({ ...emptyUsageFilters, from: '2026-01-01', to: '2026-04-04' }), '')
    assert.ok(filterError({ ...emptyUsageFilters, from: '2026-01-01', to: '2026-04-05' }))
    assert.deepEqual(recentCalendarRange(7, 'America/New_York', new Date('2026-03-09T02:00:00Z')), { from: '2026-03-02', to: '2026-03-09' })
    const q = new URLSearchParams(usageQuery({ ...emptyUsageFilters, purpose: 'research' }, mission, 'a+/='))
    assert.equal(q.get('mission_id'), mission); assert.equal(q.get('cursor'), 'a+/='); assert.equal(q.has('from'), false)
  })
  it('价格不接受指数/负值/超精度，空缓存不等于免费；精确匹配组合', () => {
    for (const v of ['0', '0.000001', '1000000', '1000000.000000']) assert.equal(validPrice(v), true)
    for (const v of ['', '-1', '1e3', '01', '1.0000001', '1000000.000001']) assert.equal(validPrice(v), false)
    assert.equal(matchesPriceWrite(price, { ...write, input_per_million: '2.000000' }), true)
    assert.equal(matchesPriceWrite(price, { ...write, cache_read_per_million: '0' }), false)
    assert.notEqual(priceKey(price), priceKey({ ...price, protocol: 'anthropic_messages' }))
    assert.notEqual(priceKey(price), priceKey({ ...price, model: 'Fixture' }))
    const s = new PriceEditSession(); s.pending = { id, body: write }; assert.equal(s.pending.id, id); s.clear(); assert.equal(s.pending, null)
  })
  it('原文true冻结进提交身份，false与省略仍兼容旧键', () => {
    const input = { description: 'fixture', goal: 'fixture', allowed_origins: ['http://127.0.0.1:9'] }
    assert.equal(isSameExplorationInput(input, { ...input, record_model_content: false }), true)
    assert.equal(isSameExplorationInput(input, { ...input, record_model_content: true }), false)
    const state = beginSubmission(initialSubmissionState, { ...input, record_model_content: true }, () => id)
    assert.equal(state.frozenPayload?.record_model_content, true)
    assert.throws(() => beginSubmission(state, input), /不能修改/)
    assert.equal('record_model_content' in beginSubmission(initialSubmissionState, { ...input, record_model_content: false }, () => id).frozenPayload!, false)
  })
  it('全局/任务深链固定调用ID，原文仅显式子树挂载', () => {
    assert.deepEqual(parsePathname(`/usage/calls/${id}`), { kind: 'usage', tab: 'calls', callID: id })
    assert.deepEqual(parsePathname(`/projects/${mission}/model-calls/${id}`), { kind: 'workspace', missionId: mission, threadId: null, modelCalls: true, callID: id })
    const detail = readFileSync('src/react/components/ModelCallDetail.tsx', 'utf8')
    assert.ok(detail.includes('{raw && <CallRawContent'))
    assert.ok(detail.includes('return () => controller.abort()'))
    assert.doesNotMatch(detail, /localStorage|sessionStorage|dangerouslySetInnerHTML/)
    assert.doesNotMatch(readFileSync('src/react/components/ModelUsageWorkspace.tsx', 'utf8'), /fetchModelCallContent/)
  })
})

describe('B组真实API客户端边界（传输故障注入）', () => {
  it('列表/详情/原文/记录设置分别核验身份，不回退全局', async () => {
    const original = globalThis.fetch
    setMemoryToken('fixture'); setOnUnauthorized(null)
    const seen: string[] = []
    try {
      globalThis.fetch = async (url, init) => {
        seen.push(String(url)); assert.equal(init?.cache, 'no-store'); assert.equal(init?.method, 'GET')
        return new Response(JSON.stringify(String(url).endsWith('/model-recording') ? { mission_id: id, record_model_content: true } : String(url).includes('/content') ? { call_id: mission, recorded: false } : String(url).includes('/model-calls?') ? { items: [{ id, mission_id: id }] } : { id, mission_id: id }))
      }
      for (const read of [() => fetchModelCalls(`mission_id=${mission}`, mission), () => fetchModelCall(id, mission), () => fetchModelCallContent(id, mission), () => fetchModelRecording(mission)]) await assert.rejects(read, (e: unknown) => e instanceof ApiError && e.status === 404)
      assert.equal(seen.length, 4)
      globalThis.fetch = async url => { seen.push(String(url)); return new Response('{}', { status: 503 }) }
      await assert.rejects(() => fetchModelUsage('time_zone=UTC', mission), (e: unknown) => e instanceof ApiError && e.status === 503)
      assert.equal(seen.length, 5); assert.ok(seen[4].includes(`/missions/${mission}/model-usage`))
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
  it('价格409/503/失联每次只有一个PUT且错误不泄漏正文', async () => {
    const original = globalThis.fetch; setMemoryToken('fixture')
    try {
      for (const status of [409, 503, 0]) {
        let count = 0
        globalThis.fetch = async (url, init) => { count++; assert.equal(String(url), `/api/model-prices/${id}`); assert.equal(init?.method, 'PUT'); assert.equal(JSON.parse(String(init?.body)).expected_revision, 1); if (!status) throw new Error('private'); return new Response('{"error":"private"}', { status }) }
        await assert.rejects(() => putModelPrice(id, write), (e: unknown) => e instanceof ApiError && e.status === status && !e.message.includes('private'))
        assert.equal(count, 1)
      }
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
  it('原文迟到结果不可穿越取消或重新登录', async () => {
    const original = globalThis.fetch; setMemoryToken('first')
    try {
      let finish!: (response: Response) => void
      globalThis.fetch = () => new Promise(resolve => { finish = resolve })
      const controller = new AbortController(), p = fetchModelCallContent(id, mission, controller.signal)
      controller.abort(); finish(new Response(JSON.stringify({ call_id: id, recorded: true })))
      await assert.rejects(p, (e: unknown) => e instanceof ApiError && e.status === 0)
      const late = fetchModelCallContent(id, mission); setMemoryToken('second'); finish(new Response(JSON.stringify({ call_id: id, recorded: true })))
      await assert.rejects(late, (error: unknown) => error instanceof StaleSessionError)
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
})
