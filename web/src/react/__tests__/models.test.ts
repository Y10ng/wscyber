import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { ApiError, StaleSessionError, invalidateSession, setMemoryToken, setOnUnauthorized } from '../api.ts'
import { fetchModelConfig, fetchModelConfigs, fetchModelTest, postModelTest, patchModelConfig } from '../modelApi.ts'
import './modelSelection.test.ts'
import { ModelTestSession, encodeModelDraft, modelDraft, modelEndpoint, modelTestPath, requireTestIdentity, testAttempt, testStatus, validateModelDraft, type ModelConfig, type ModelTest } from '../models.ts'
import { parsePathname } from '../routing.ts'
import { ModelSettingsView } from '../components/ModelSettingsView.tsx'
const id = 'a'.repeat(32), testID = 'b'.repeat(32)
const config: ModelConfig = { id, revision: 3, name: '隔离配置', protocol: 'openai_chat', base_url: 'https://example.invalid/v1', model: 'fixture', auth: 'bearer', timeout_seconds: 30, max_output_tokens: 64, enabled: true, api_key_set: true, header_names: ['X-Fixture'], parameter_keys: ['temperature'], created_at: '2026-11-12T00:00:00Z' }
const receipt: ModelTest = { id: testID, config_id: id, config_revision: 3, status: 'completed', outcome: 'valid_response', attempted: true, http_status: 200, created_at: config.created_at }
const source = readFileSync('src/react/components/ModelSettingsView.tsx', 'utf8')

describe('MC-1C 配置/秘密/测试身份契约', () => {
  it('新配置使用安全随机32位ID，编辑沿用精确版本且秘密不回显', () => {
    const a = modelDraft(), b = modelDraft()
    assert.match(a.id, /^[0-9a-f]{32}$/); assert.notEqual(a.id, b.id); assert.equal(a.expected_revision, 0)
    const d = modelDraft(config)
    assert.equal(d.id, id); assert.equal(d.expected_revision, 3)
    assert.equal(d.api_key, '')
    assert.deepEqual(d.headers, [{ id: 'header-0', name: 'X-Fixture' }])
    assert.deepEqual(d.parameters, [{ id: 'parameter-0', name: 'temperature' }])
  })
  it('直接编辑省略未输入key、完整条目清单增改删且null不是省略', () => {
    const d = modelDraft(config)
    d.headers = []; d.parameters = [{ id: 'p', name: 'temperature', value: 'null' }, { id: 'new', name: 'custom', value: '{"nested":true}' }]
    const body = JSON.parse(encodeModelDraft(d))
    assert.equal('api_key' in body, false); assert.deepEqual(body.headers, []); assert.deepEqual(body.parameters, [{ name: 'temperature', value: null }, { name: 'custom', value: { nested: true } }])
    assert.equal(body.expected_revision, 3); assert.equal('original' in body, false); assert.equal('id' in body, false)
    assert.doesNotMatch(encodeModelDraft(d), /"mode"/)
  })
  it('改变协议/端点/认证必须显式处理已有三类秘密', () => {
    for (const change of [{ protocol: 'anthropic_messages' as const }, { base_url: 'https://other.invalid' }, { auth: 'none' as const }]) {
      const d = { ...modelDraft(config), ...change }
      const errors = validateModelDraft(d)
      for (const key of ['headers', 'parameters'] as const) assert.ok(errors[key])
      if (d.auth !== 'none') assert.ok(errors.api_key)
      d.auth = 'none'; d.api_key = ''; d.headers = []; d.parameters = []
      assert.deepEqual(validateModelDraft(d), {})
      assert.equal('api_key' in JSON.parse(encodeModelDraft(d)), false)
    }
  })
  it('保留JSON原始重复键与嵌套重复键，交服务端严格拒绝', () => {
    const d = modelDraft(config)
    d.parameters = [{ id: 'p', name: 'custom', value: '{"x":1,"x":2,"nested":{"a":1,"a":2}}' }]
    const raw = encodeModelDraft(d)
    assert.ok(raw.includes(d.parameters[0].value!))
    d.headers = [{ id: 'a', name: 'X-A', value: 'one' }, { id: 'b', name: 'x-a', value: 'two' }]
    assert.ok(validateModelDraft(d).headers)
  })
  it('JSON错误不包含输入值；认证和整数范围就地校验', () => {
    const d = modelDraft(config); d.parameters = [{ id: 'p', name: 'custom', value: 'secret-non-json' }]; d.timeout_seconds = 0; d.max_output_tokens = 1.5; d.api_key = '   '
    const errors = validateModelDraft(d)
    assert.ok(errors.parameters && errors.timeout_seconds && errors.max_output_tokens && errors.api_key)
    assert.doesNotMatch(JSON.stringify(errors), /secret-non-json/)
    assert.throws(() => encodeModelDraft(d), /检查配置/)
  })
  it('不猜Base路径，OpenAI与Anthropic只追加冻结协议后缀', () => {
    assert.equal(modelEndpoint(config), 'https://example.invalid/v1/chat/completions')
    assert.equal(modelEndpoint({ ...config, base_url: 'https://example.invalid/gateway', protocol: 'anthropic_messages' }), 'https://example.invalid/gateway/v1/messages')
    assert.equal(modelEndpoint({ ...config, base_url: 'https://example.invalid' }), 'https://example.invalid/chat/completions')
  })
  it('深链只允许两个严格小写hex对象ID', () => {
    assert.deepEqual(parsePathname(modelTestPath(id, testID)), { kind: 'models', configID: id, testID })
    for (const bad of ['ABC', 'a'.repeat(31), `${id}?key=secret`, `%61${id.slice(1)}`, `${id}/x`]) {
      assert.throws(() => modelTestPath(bad, testID)); assert.deepEqual(parsePathname(`/settings/models/${bad}/tests/${testID}`), { kind: 'models', configID: null, testID: null, invalid: true })
    }
  })
  it('未决身份不被旧版本已完成回执替换，切页和404仍指向原身份', () => {
    const s = new ModelTestSession(); s.remember(id, testID)
    const old = 'c'.repeat(32); s.remember(id, old); s.accept({ ...receipt, id: old, config_revision: 1 })
    assert.equal(s.forConfig(id)?.testID, testID)
    s.accept(receipt); assert.equal(s.known.get(testID)?.receipt?.config_revision, 3)
    s.mask(); assert.equal(s.known.get(testID)?.receipt, undefined); assert.equal(s.known.get(testID)?.testID, testID)
    s.clear(); assert.equal(s.known.size, 0)
  })
  it('切页history仅存严格对象ID，刷新恢复未决身份且不带回执或秘密', () => {
    const s = new ModelTestSession(); s.remember(id, testID)
    assert.deepEqual(s.identities(), [{ configID: id, testID }])
    const restored = new ModelTestSession([...s.identities(), { configID: '../bad', testID }, { configID: id, testID: 'bad', key: 'secret' }])
    assert.equal(restored.known.size, 1); assert.equal(restored.forConfig(id)?.testID, testID)
    assert.equal(restored.forConfig(id)?.receipt, undefined)
    s.accept(receipt); assert.deepEqual(s.identities(), [])
  })
  it('202只排队/运行，unknown和缺省attempted不变成未发送', () => {
    assert.equal(testStatus(), '受理情况待核对')
    assert.equal(testStatus({ ...receipt, status: 'pending', outcome: undefined }), '排队中')
    assert.equal(testStatus({ ...receipt, status: 'running', outcome: undefined }), '运行中')
    assert.equal(testStatus({ ...receipt, outcome: 'transport_unknown' }), '效果未知')
    assert.equal(testAttempt({ ...receipt, attempted: undefined }), '发送阶段未知')
    assert.equal(testAttempt({ ...receipt, attempted: false }), '未进入传输')
    for (const outcome of ['not_sent', 'http_error', 'invalid_response', 'valid_response'] as const) assert.ok(testStatus({ ...receipt, outcome }))
  })
  it('回执必须匹配配置/测试身份，旧版本保持旧版本', () => {
    assert.equal(requireTestIdentity(receipt, id, testID).config_revision, 3)
    assert.throws(() => requireTestIdentity(receipt, 'c'.repeat(32), testID)); assert.throws(() => requireTestIdentity(receipt, id, 'c'.repeat(32)))
  })
  it('正式页面复用共享编辑器、默认API，无版本/秘密模式/usage/持久存储，不造历史列表', () => {
    const html = renderToStaticMarkup(React.createElement(ModelSettingsView, { active: true, configID: null, testID: null, session: new ModelTestSession(), navigate() {} }))
    assert.match(html, /新建配置/); assert.doesNotMatch(html, /默认模型|用于研究|费用|成功率|历史测试/)
    assert.doesNotMatch(source, /localStorage|sessionStorage|indexedDB|Radio\.Group|配置版本|基于 v|旧版本|已保存版本/)
    assert.match(source, /<SecretEntriesEditor/); assert.match(source, /await putModelSettings/); assert.match(source, /await patchModelConfig/)
    assert.equal((source.match(/await postModelTest\(/g) ?? []).length, 1)
    assert.ok(source.indexOf('navigate(modelTestPath(value.id, identity))') < source.indexOf('await postModelTest('))
    assert.match(source, /confirmLock\.current/); assert.match(source, /controller\.abort\(\)/)
  })
})

describe('MC-1C API单次写入与读取隔离', () => {
  it('PATCH内部并发令牌，原JSON不折叠；读取均GET/no-store', async () => {
    const original = globalThis.fetch; const calls: { path: string; options?: RequestInit }[] = []
    try {
      setMemoryToken('fixture-only')
      globalThis.fetch = async (path, options) => { calls.push({ path: String(path), options }); return new Response(JSON.stringify(String(path).includes('/tests/') ? receipt : config)) }
      const d = modelDraft(config); d.parameters = [{ id: 'p', name: 'custom', value: '{"x":1,"x":2}' }]; const raw = encodeModelDraft(d)
      await patchModelConfig(id, raw); await fetchModelConfigs(); await fetchModelConfig(id); await fetchModelTest(id, testID)
      assert.equal(calls[0].options?.method, 'PATCH'); assert.equal(calls[0].options?.body, raw)
      for (const call of calls.slice(1)) { assert.equal(call.options?.method, 'GET'); assert.equal(call.options?.body, undefined) }
      for (const call of calls) { assert.equal(call.options?.cache, 'no-store'); assert.doesNotMatch(call.path, /fixture-only|temperature/) }
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
  it('POST严格精确版本与确认，不含项目内容', async () => {
    const original = globalThis.fetch; const calls: RequestInit[] = []
    try {
      setMemoryToken('fixture-only')
      globalThis.fetch = async (_, options) => { calls.push(options!); return new Response(JSON.stringify({ ...receipt, status: 'pending', outcome: undefined }), { status: 202 }) }
      const result = await postModelTest(id, testID, 3)
      assert.equal(result.status, 'pending'); assert.equal(calls.length, 1)
      assert.deepEqual(JSON.parse(calls[0].body as string), { config_revision: 3, confirm_billable: true })
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
  it('CAS/503/404和断网一次调用，不重试、不打印秘密错误', async () => {
    const original = globalThis.fetch
    try {
      setMemoryToken('fixture-only')
      for (const status of [400, 409, 503, 404, 0]) {
        let calls = 0
        globalThis.fetch = async () => { calls++; if (status === 0) throw new Error('private-secret'); return new Response('{"error":"private-secret"}', { status }) }
        await assert.rejects(postModelTest(id, testID, 3), error => error instanceof ApiError && !error.message.includes('private-secret'))
        assert.equal(calls, 1)
      }
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
  it('401触发敏感状态清理；旧会话迟到响应不能恢复回执', async () => {
    const original = globalThis.fetch; let masked = false
    try {
      setMemoryToken('fixture-only'); setOnUnauthorized(() => { masked = true })
      globalThis.fetch = async () => new Response('{}', { status: 401 })
      await assert.rejects(fetchModelTest(id, testID), /401/); assert.equal(masked, true)
      setMemoryToken('fixture-only')
      let resolve!: (r: Response) => void
      globalThis.fetch = () => new Promise(r => { resolve = r })
      const p = fetchModelTest(id, testID); invalidateSession(false); setMemoryToken('new-fixture')
      resolve(new Response(JSON.stringify(receipt)))
      await assert.rejects(p, error => error instanceof StaleSessionError)
    } finally { setOnUnauthorized(null); globalThis.fetch = original; invalidateSession(false) }
  })
  it('取消核对丢弃迟到响应，不把取消当未发送', async () => {
    const original = globalThis.fetch
    try {
      setMemoryToken('fixture-only'); const controller = new AbortController()
      let resolve!: (r: Response) => void
      globalThis.fetch = () => new Promise(r => { resolve = r })
      const p = fetchModelTest(id, testID, controller.signal); controller.abort(); resolve(new Response(JSON.stringify(receipt)))
      await assert.rejects(p, /请求已中止/)
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
})
