import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { renderToStaticMarkup } from 'react-dom/server'
import React from 'react'
import { canStartExploration, encodeModelDraft, modelDraft, ModelTestSession, validateModelDraft, type ModelConfig } from '../models.ts'
import { beginSubmission, initialSubmissionState, isSameExplorationInput } from '../idempotency.ts'
import { ApiError, invalidateSession, setMemoryToken, StaleSessionError } from '../api.ts'
import { fetchMissionModel, fetchModelSettings, patchModelConfig, putModelSettings } from '../modelApi.ts'
import { ExplorationCreateActions, ExplorationCreateView } from '../components/ExplorationCreateView.tsx'
import type { ExplorationInput, SystemInfo } from '../types.ts'
const id = 'a'.repeat(32)
const config: ModelConfig = { id, revision: 3, name: '隔离配置', protocol: 'openai_chat', base_url: 'https://example.invalid/v1', model: 'fixture', auth: 'bearer', timeout_seconds: 30, max_output_tokens: 64, enabled: true, api_key_set: true, header_names: ['X-Fixture'], parameter_keys: ['temperature'], created_at: '2026-11-12T00:00:00Z' }
const sys: SystemInfo = { name: 'fixture', version: '', model_configured: false, model: '', model_management_available: true, model_source: 'none', sandbox_image: '', sandbox_runtime: '', network_mode: '', max_parallel: 1 }

describe('A组模型编辑与任务选择', () => {
  it('认证key新建和禁用均必填，已有key普通编辑不强迫重填', () => {
    for (const enabled of [true, false]) {
      const d = modelDraft({ ...config, api_key_set: false, enabled })
      assert.ok(validateModelDraft(d).api_key)
      d.api_key = 'test-only'; assert.equal(validateModelDraft(d).api_key, undefined)
      assert.equal(JSON.parse(encodeModelDraft(d)).api_key, 'test-only')
    }
    assert.deepEqual(validateModelDraft(modelDraft(config)), {})
  })
  it('Header大小写匹配允许省略，但改名不得移动旧秘密', () => {
    const d = modelDraft(config); d.headers[0].name = 'x-fixture'
    assert.deepEqual(validateModelDraft(d), {})
    assert.deepEqual(JSON.parse(encodeModelDraft(d)).headers, [{ name: 'x-fixture' }])
    d.headers[0].name = 'X-New'; assert.ok(validateModelDraft(d).headers)
    d.headers[0].value = ''; assert.deepEqual(validateModelDraft(d), {})
    assert.deepEqual(JSON.parse(encodeModelDraft(d)).headers, [{ name: 'X-New', value: '' }])
  })
  it('接收端变化每项重新输入，不经trim或parse重写用户值', () => {
    const d = modelDraft(config); d.base_url = 'https://new.invalid/v1'
    assert.ok(validateModelDraft(d).api_key && validateModelDraft(d).headers && validateModelDraft(d).parameters)
    d.api_key = 'test-only'; d.headers[0].value = ' exact value '
    d.parameters[0].value = ' {"a":1,"a":2} '
    assert.deepEqual(validateModelDraft(d), {})
    const raw = encodeModelDraft(d); assert.ok(raw.includes(' {"a":1,"a":2} '))
    assert.equal(JSON.parse(raw).headers[0].value, ' exact value ')
  })
  it('任务指纹包括显式模型，省略与空兼容旧指纹，默认变化不污染已冻结请求', () => {
    const input: ExplorationInput = { description: '目标', goal: '目的', allowed_origins: ['http://127.0.0.1'] }
    assert.ok(isSameExplorationInput(input, { ...input, model_config_id: '' }))
    assert.equal(isSameExplorationInput(input, { ...input, model_config_id: id }), false)
    const selected = { ...input, model_config_id: id }
    const state = beginSubmission(initialSubmissionState, selected)
    assert.equal(state.frozenPayload?.model_config_id, id)
    assert.throws(() => beginSubmission(state, { ...selected, model_config_id: 'b'.repeat(32) }))
    assert.equal(beginSubmission(state, selected).operationKey, state.operationKey)
    assert.equal('model_config_id' in beginSubmission(initialSubmissionState, input).frozenPayload!, false)
  })
  it('没有默认仍可显式选择启用模型；禁用/管理不可用不静默回退', () => {
    assert.equal(canStartExploration(sys), false)
    assert.equal(canStartExploration(sys, id, [config]), true)
    assert.equal(canStartExploration({ ...sys, model_configured: true }, id, [{ ...config, enabled: false }]), false)
    assert.equal(canStartExploration({ ...sys, model_configured: true, model_management_available: false }, id, [config]), false)
    assert.equal(canStartExploration({ ...sys, model_configured: true }), true)
  })
  it('创建表单与底栏显示真实选择，旧环境入口无管理依赖', () => {
    const html = renderToStaticMarkup(React.createElement(ExplorationCreateView, { systemInfo: sys, models: [config], draft: { description: '', goal: '', originsText: '', modelConfigID: id }, submissionState: initialSubmissionState, onChangeDraft() {}, async onSubmitExploration() {} }))
    assert.match(html, /研究模型/); assert.match(html, /隔离配置/); assert.doesNotMatch(html, /当前不可启动/)
    const actions = renderToStaticMarkup(React.createElement(ExplorationCreateActions, { systemInfo: sys, canStart: true, submissionState: initialSubmissionState, hasDraftContent: true, onClearDraft() {}, async onVerifyOperation() {}, async onRetrySameKey() {}, onClose() {} }))
    assert.doesNotMatch(actions, /disabled=""/)
    const old = renderToStaticMarkup(React.createElement(ExplorationCreateView, { systemInfo: { ...sys, model_management_available: undefined, model_configured: true }, draft: { description: '', goal: '', originsText: '' }, submissionState: initialSubmissionState, onChangeDraft() {}, async onSubmitExploration() {} }))
    // 旧环境入口不依赖模型管理选择器；研究说明的分享开关仍可用于环境模型。
    assert.doesNotMatch(old, /(?:id|for)="exploration-model"|当前不可启动/)
    assert.match(old, /提供给研究模型/)
    assert.match(html, /(?:id|for)="exploration-model"/)
  })
  it('默认PUT未落库后同令牌GET可显式继续，旧GET/PUT不能覆盖新选择', () => {
    const session = new ModelTestSession(), original = { revision: 2, default_config_id: '' }
    const generation = session.beginDefault(original, id)
    assert.equal(session.continueDefaultFromRead(), null)
    assert.ok(session.acceptDefaultRead(original, generation))
    assert.equal(session.defaultPending?.targetID, id)
    assert.deepEqual(session.continueDefaultFromRead(), original)
    const nextGeneration = session.beginDefault(original, 'b'.repeat(32))
    assert.equal(session.acceptDefaultRead({ revision: 3, default_config_id: id }, generation), false)
    assert.equal(session.acceptDefaultWrite(generation), false)
    assert.equal(session.rejectDefaultWrite(generation), false)
    assert.equal(session.defaultPending?.targetID, 'b'.repeat(32))
    assert.equal(session.defaultPending?.rejected, undefined)
    assert.ok(session.rejectDefaultWrite(nextGeneration))
    assert.equal(session.defaultPending?.rejected, true)
    assert.ok(session.acceptDefaultWrite(nextGeneration))
    assert.equal(session.defaultPending, undefined)
  })
  it('默认写入未决状态跨401遮蔽保留，显式退出清空', () => {
    const session = new ModelTestSession()
    session.defaultPending = { targetID: id, expectedRevision: 1 }
    session.mask(); assert.equal(session.defaultPending.targetID, id)
    assert.deepEqual(session.identities(), [])
    session.clear(); assert.equal(session.defaultPending, undefined)
  })
})

describe('A组真实接口客户端契约', () => {
  it('默认GET/PUT仅内部CAS，任务模型GET验证所属且不返回其他任务', async () => {
    const original = globalThis.fetch, calls: { path: string; options?: RequestInit }[] = []
    try {
      setMemoryToken('local-only')
      globalThis.fetch = async (path, options) => { calls.push({ path: String(path), options }); return new Response(JSON.stringify(String(path).includes('/missions/') ? { mission_id: 'm1', source: 'managed', config_id: id, model: 'fixture', enabled: true } : { revision: 2, default_config_id: id })) }
      await fetchModelSettings(); await putModelSettings({ revision: 1, default_config_id: '' }, id); await fetchMissionModel('m1')
      assert.deepEqual(calls.map(c => [c.path, c.options?.method]), [['/api/model-settings', 'GET'], ['/api/model-settings', 'PUT'], ['/api/missions/m1/model', 'GET']])
      assert.deepEqual(JSON.parse(calls[1].options?.body as string), { expected_revision: 1, default_config_id: id })
      await assert.rejects(fetchMissionModel('m2'), /归属不匹配/)
      for (const call of calls) assert.equal(call.options?.cache, 'no-store')
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
  it('PATCH与默认PUT在409/503/断网均单次调用且不泄漏错误正文', async () => {
    const original = globalThis.fetch
    try {
      setMemoryToken('local-only')
      for (const operation of [() => patchModelConfig(id, encodeModelDraft(modelDraft(config))), () => putModelSettings({ revision: 1, default_config_id: '' }, id)]) {
        for (const status of [409, 503, 0]) {
          let calls = 0
          globalThis.fetch = async () => { calls++; if (!status) throw new Error('private-secret'); return new Response('{"error":"private-secret"}', { status }) }
          await assert.rejects(operation(), error => error instanceof ApiError && !error.message.includes('private-secret'))
          assert.equal(calls, 1)
        }
      }
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
  it('默认读取与任务模型迟到响应不穿越重新登录', async () => {
    const original = globalThis.fetch
    try {
      for (const operation of [() => fetchModelSettings(), () => fetchMissionModel('m1')]) {
        setMemoryToken('local-only')
        let finish!: (response: Response) => void
        globalThis.fetch = () => new Promise(resolve => { finish = resolve })
        const pending = operation(); invalidateSession(false); setMemoryToken('new-session')
        finish(new Response('{"mission_id":"m1","source":"managed","revision":1,"default_config_id":""}'))
        await assert.rejects(pending, error => error instanceof StaleSessionError)
      }
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
})
