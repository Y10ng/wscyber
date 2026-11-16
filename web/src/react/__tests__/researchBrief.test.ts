import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { beginSubmission, handlePostFailure, handleSessionUnauthorizedDuringSubmission, initialSubmissionState, isSameExplorationInput } from '../idempotency.ts'
import { emptyResearchBrief, hasBriefDraft, researchBriefPayload, validateResearchBrief, isResearchBrief, type ResearchBrief } from '../../ui/researchBrief.ts'
import type { ExplorationInput } from '../types.ts'
import { fetchResearchConfig } from '../libraryApi.ts'
import { invalidateSession, setMemoryToken } from '../api.ts'

const brief = (): ResearchBrief => ({ identities: [{ label: '成员A', role: '普通成员', ownership: '本人记录' }], business_rules: '用户声明', share_with_model: false })
const input = (): ExplorationInput => ({ description: '本机fixture', goal: '核对配置', allowed_origins: ['https://fixture.invalid'], research_brief: brief() })
const key = '1234567890abcdef1234567890abcdef'
describe('C2-A身份说明与业务前提', () => {
  it('默认关闭且无说明内容时省略，单独开关也不产生空对象', () => {
    assert.equal(emptyResearchBrief().share_with_model, false)
    assert.equal(researchBriefPayload(emptyResearchBrief()), undefined)
    assert.equal(researchBriefPayload({ share_with_model: true }), undefined)
    assert.equal(hasBriefDraft({ share_with_model: true }), true)
    assert.equal(hasBriefDraft(undefined), false)
    assert.ok(validateResearchBrief({ business_rules: '　', share_with_model: false }).group)
  })
  it('深复制身份数组/条目，保留false、原文本和顺序；未配置旧载荷不增字段', () => {
    const payload = input(), state = beginSubmission(initialSubmissionState, payload, () => key)
    payload.research_brief!.identities![0]!.label = '后改'
    payload.research_brief!.identities!.push({ label: '另一个', role: '管理员' })
    payload.research_brief!.business_rules = '后改规则'; payload.research_brief!.share_with_model = true
    assert.deepEqual(state.frozenPayload!.research_brief, brief())
    const old = input(); delete old.research_brief
    assert.equal('research_brief' in beginSubmission(initialSubmissionState, old, () => key).frozenPayload!, false)
  })
  it('内容/分享/身份顺序改变必须拒绝同键；省略与空可选字段等价', () => {
    const payload = input(), state = beginSubmission(initialSubmissionState, payload, () => key)
    for (const changed of [{ ...brief(), share_with_model: true }, { ...brief(), preconditions: '变更' }, { ...brief(), identities: [{ label: '另一个', role: '普通成员' }] }]) {
      assert.throws(() => beginSubmission(state, { ...payload, research_brief: changed }, () => 'never'))
    }
    const a = input(); a.research_brief!.identities!.push({ label: 'B', role: 'member' })
    const b = input(); b.research_brief!.identities = [...a.research_brief!.identities!].reverse()
    assert.equal(isSameExplorationInput(a, b), false)
    assert.equal(isSameExplorationInput(input(), { ...input(), research_brief: { ...brief(), preconditions: '' } }), true)
  })
  it('401/断连与重认证保留同键与原分享选择，不自动发送', () => {
    const state = beginSubmission(initialSubmissionState, input(), () => key)
    for (const next of [handlePostFailure(state, 0, '失联'), handlePostFailure(state, 401, '认证失效'), handleSessionUnauthorizedDuringSubmission(state)]) {
      assert.equal(next.operationKey, key); assert.deepEqual(next.frozenPayload!.research_brief, brief())
      const retried = beginSubmission(next, next.frozenPayload!, () => { throw Error('不应换键') })
      assert.equal(retried.frozenPayload!.research_brief!.share_with_model, false)
    }
  })
  it('数量、各字节边界、聚合、编码扩张、多行与非法Unicode校验', () => {
    assert.deepEqual(validateResearchBrief({ identities: [{ label: 'a'.repeat(128), role: 'r'.repeat(512), ownership: 'o'.repeat(1024) }], business_rules: 'b'.repeat(8192), preconditions: 'p'.repeat(4096), share_with_model: false }), {})
    for (const invalid of [
      { ...brief(), identities: [{ label: '中'.repeat(43), role: 'r' }] },
      { ...brief(), identities: [{ label: 'a', role: 'r'.repeat(513) }] },
      { ...brief(), identities: [{ label: 'a', role: 'r', ownership: 'o'.repeat(1025) }] },
      { ...brief(), business_rules: 'b'.repeat(8193) }, { ...brief(), preconditions: 'p'.repeat(4097) },
      { ...brief(), identities: Array.from({ length: 9 }, (_, i) => ({ label: String(i), role: 'r' })) },
      { ...brief(), business_rules: '<'.repeat(6000) }, { ...brief(), business_rules: String.fromCharCode(0xd800) },
      { ...brief(), business_rules: 'a\rb' }, { ...brief(), business_rules: 'a\x00b' }, { ...brief(), preconditions: String.fromCharCode(0x85) },
      { ...brief(), business_rules: 'b'.repeat(8192), preconditions: 'p'.repeat(4096), identities: ['a','b','c'].map(label => ({ label, role: 'r'.repeat(512), ownership: 'o'.repeat(1024) })) },
    ]) assert.ok(Object.keys(validateResearchBrief(invalid)).length)
    assert.deepEqual(validateResearchBrief({ ...brief(), business_rules: 'a\r\nb\nc\td' }), {})
    assert.deepEqual(validateResearchBrief({ ...brief(), identities: Array.from({ length: 8 }, (_, i) => ({ label: String(i), role: 'r' })) }), {})
  })
  it('只读响应不接受缺省/null分享、凭据字段、未知字段或损坏文本', () => {
    assert.equal(isResearchBrief(brief()), true)
    for (const bad of [null, [], {}, { ...brief(), share_with_model: null }, { ...brief(), share_with_model: undefined }, { ...brief(), password: 'private' }, { ...brief(), identities: null }, { ...brief(), identities: [{ label: 'a', role: 'r', headers: {} }] }]) assert.equal(isResearchBrief(bad), false)
  })
  it('研究配置API核对任务归属与说明结构，不向错误消息回显正文', async () => {
    const original = globalThis.fetch
    setMemoryToken('brief-fixture-token')
    try {
      const good = { mission_id: 'mission-a', materials: [], safety_prompt_enabled: false, safety_prompt: '', research_brief: brief() }
      globalThis.fetch = async () => new Response(JSON.stringify(good), { status: 200 })
      assert.deepEqual((await fetchResearchConfig('mission-a')).research_brief, brief())
      await assert.rejects(fetchResearchConfig('mission-b'), /归属/)
      globalThis.fetch = async () => new Response(JSON.stringify({ ...good, research_brief: { ...brief(), share_with_model: null, token: 'PRIVATE_SENTINEL' } }), { status: 200 })
      await assert.rejects(fetchResearchConfig('mission-a'), e => e instanceof Error && !e.message.includes('PRIVATE_SENTINEL'))
    } finally { globalThis.fetch = original; invalidateSession(false) }
  })
})
