import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { renderToStaticMarkup } from 'react-dom/server'
import { importMaterialText, materialBytes, materialSelectionError, validateMaterial, type MaterialDraft, type MaterialItem } from '../../ui/materials.ts'
import { MaterialText, MaterialSelection } from '../../ui/MaterialLibrary.tsx'
import { MaterialExamples } from '../../component-preview/MaterialExamples.tsx'
import { UIProvider } from '../../ui/UIProvider.tsx'
import { LibrarySession } from '../library.ts'
import { setMemoryToken } from '../api.ts'
import { beginSubmission, handleOperationCheck404, handlePostFailure, handleSessionUnauthorizedDuringSubmission, initialSubmissionState, isSameExplorationInput } from '../idempotency.ts'
const draft: MaterialDraft = { parent_id: '', kind: 'file', name: 'test.md', description: '', content: '原文\r\n' }
const item: MaterialItem = { ...draft, id: 'a'.repeat(32), token: 'b'.repeat(32), format: 'markdown', content_sha256: 'hash', byte_size: 8, created_at: '', updated_at: '' }
describe('C1共享资料组合与可靠状态', () => {
  it('选材/安全/原文一起冻结；关闭清单不发送；过期材料code只释放首次确定拒绝', () => {
    const payload = { description: 'x', goal: 'y', allowed_origins: ['http://127.0.0.1:9'], materials: [{ id: item.id, token: item.token }], safety_prompt_enabled: true, safety_prompt: '附加约束', record_model_content: true }
    const state = beginSubmission(initialSubmissionState, payload)
    payload.materials[0].token = 'c'.repeat(32)
    assert.equal(state.frozenPayload?.materials?.[0].token, item.token)
    assert.equal(state.frozenPayload?.record_model_content, true)
    assert.equal(isSameExplorationInput(state.frozenPayload!, payload), false)
    assert.equal(handlePostFailure(state, 409, 'stale', 'material_selection_stale').operationKey, null)
    for (const uncertain of [handlePostFailure(state, 0, '断线'), handleSessionUnauthorizedDuringSubmission(state), handleOperationCheck404(state)]) {
      const kept = handlePostFailure(uncertain, 409, 'stale', 'material_selection_stale')
      assert.equal(kept.operationKey, state.operationKey); assert.equal(kept.frozenPayload?.safety_prompt, '附加约束'); assert.equal(kept.frozenPayload?.record_model_content, true)
      assert.equal(handlePostFailure(uncertain, 400, '输入错误').operationKey, state.operationKey)
    }
    const off = beginSubmission(initialSubmissionState, { ...payload, safety_prompt_enabled: false, safety_prompt: '' })
    assert.equal(off.frozenPayload?.safety_prompt, undefined)
  })
  it('资料失联仅显式GET核对，404保留身份不重发；历史回执不混为实时详情', async () => {
    const original = globalThis.fetch, calls: { method: string; key: string | undefined; body: string | undefined }[] = []
    let mode = 'lost'
    setMemoryToken('fixture')
    globalThis.fetch = async (_input, init) => {
      calls.push({ method: init?.method || 'GET', key: (init?.headers as Record<string, string>)?.['Idempotency-Key'], body: init?.body as string })
      if (mode === 'lost') throw new Error('offline')
      if (mode === 'missing') return new Response('{}', { status: 404 })
      const pending = session.snapshot().pending!
      return new Response(JSON.stringify({ key: pending.key, item_id: pending.id, deleted: false, committed_at: '2026-11-12T00:00:00Z', item }), { status: 200 })
    }
    const session = new LibrarySession()
    try {
      session.newDraft('file', ''); session.change(draft); await session.save()
      const key = session.snapshot().pending?.key; assert.ok(key); assert.equal(session.snapshot().phase, 'unknown')
      mode = 'missing'; await session.check(); assert.equal(calls.length, 2); assert.equal(calls[1].method, 'GET'); assert.equal(session.snapshot().pending?.key, key)
      mode = 'ok'; await session.check(); assert.equal(calls[2].method, 'GET'); assert.equal(calls.filter(call => call.method === 'PUT').length, 1); assert.equal(session.snapshot().pending, null); assert.equal(session.snapshot().draft, null)
      assert.ok(session.snapshot().receipt); assert.equal('content' in session.snapshot().receipt!, false)
    } finally { globalThis.fetch = original }
  })
  it('401遮蔽/保留草稿及身份，迟到写入不能覆盖logout后的新草稿', async () => {
    const original = globalThis.fetch; setMemoryToken('fixture')
    let resolve!: (value: Response) => void
    globalThis.fetch = async () => new Promise<Response>(done => { resolve = done })
    const session = new LibrarySession()
    try {
      session.newDraft('file', ''); session.change(draft); const writing = session.save(); const pending = session.snapshot().pending!
      session.mask(); assert.equal(session.snapshot().phase, 'unknown'); assert.equal(session.snapshot().pending?.key, pending.key); assert.equal(session.snapshot().draft?.value.content, draft.content)
      session.clear(); session.newDraft('folder', ''); const fresh = session.snapshot().draft?.id
      resolve(new Response(JSON.stringify({ key: pending.key, item_id: pending.id, deleted: false, committed_at: '', item }))); await writing
      assert.equal(session.snapshot().draft?.id, fresh); assert.equal(session.snapshot().receipt, null)
    } finally { globalThis.fetch = original }
  })
  it('名称/UTF8字节/NUL/孤立代理项/格式与边界', () => {
    assert.equal(validateMaterial(draft), '')
    for (const name of ['../x.md', 'a\\x.md', '.', 'x.js', '\0.md', 'a'.repeat(254) + '.md']) assert.ok(validateMaterial({ ...draft, name }))
    assert.equal(validateMaterial({ ...draft, content: 'a'.repeat(32768) }), '')
    for (const content of ['', '\0', '\ud800', 'a'.repeat(32769)]) assert.ok(validateMaterial({ ...draft, content }))
    assert.equal(materialBytes('中文'), 6)
  })
  it('导入保持BOM/CRLF/空白原字节，拒绝非法UTF8及超限且不先读大文件', async () => {
    const content = '﻿# title\r\n \r\n', bytes = new TextEncoder().encode(content)
    assert.deepEqual(await importMaterialText({ name: 'SKILL.MD', size: bytes.length, arrayBuffer: async () => bytes.buffer }), { name: 'SKILL.MD', content })
    await assert.rejects(importMaterialText({ name: 'x.txt', size: 1, arrayBuffer: async () => new Uint8Array([255]).buffer }), /UTF-8/)
    let reads = 0
    await assert.rejects(importMaterialText({ name: 'x.md', size: 32769, arrayBuffer: async () => { reads++; return new ArrayBuffer(0) } }))
    assert.equal(reads, 0)
  })
  it('选择数量/合计/去重与仅文件', () => {
    assert.equal(materialSelectionError([item]), '')
    assert.ok(materialSelectionError([item, item]))
    assert.ok(materialSelectionError([{ ...item, kind: 'folder' }]))
    assert.ok(materialSelectionError([{ ...item, byte_size: 65537 }]))
    assert.ok(materialSelectionError(Array.from({ length: 17 }, (_, i) => ({ ...item, id: String(i) }))))
  })
  it('浅暗纯文本/长内容/选择/空/禁用及样例真实消费共享组合', () => {
    for (const appearance of ['light', 'dark'] as const) {
      const html = renderToStaticMarkup(<UIProvider appearance={appearance}><MaterialText detail={{ ...item, content: '<img src="https://invalid.example">\n<script>alert(1)</script>' }} /><MaterialSelection items={[item]} selected={[item]} query="" disabled onQuery={() => {}} onSearch={() => {}} onNext={() => {}} onPrevious={() => {}} onToggle={() => {}} onView={() => {}} onRefresh={() => {}} /></UIProvider>)
      assert.ok(html.includes('&lt;script&gt;')); assert.ok(!html.includes('<img ')); assert.ok(html.includes('复制资料正文')); assert.ok(html.includes('已选资料')); assert.ok(html.includes('disabled'))
    }
    assert.ok(renderToStaticMarkup(<MaterialExamples />).includes('导入本地文本资料'))
  })
})
