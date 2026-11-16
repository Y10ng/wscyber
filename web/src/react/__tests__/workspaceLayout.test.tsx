import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { renderToStaticMarkup } from 'react-dom/server'
import { FindingDetail, FindingList } from '../../ui/Findings.tsx'
import { WorkspaceSplit } from '../../ui/WorkspaceSplit.tsx'
import { VerificationResultView } from '../../ui/Verifications.tsx'
import { findingFixture, findingSummary } from '../../component-preview/FindingExamples.tsx'
import { verificationResultFixture } from '../../component-preview/VerificationExamples.tsx'

const pager = { onPrevious() {}, onNext() {} }
describe('UI重构：对象导航与按需内容', () => {
  it('主从布局只渲染提供的导航和已选择详情，不造占位功能', () => {
    const list = renderToStaticMarkup(<WorkspaceSplit navigationLabel="候选导航" navigation="实际列表" detailOpen={false}>不应出现的详情</WorkspaceSplit>)
    assert.ok(list.includes('实际列表') && list.includes('data-detail-open="false"'))
    assert.ok(!list.includes('不应出现的详情') && !list.includes('生成报告'))
    const detail = renderToStaticMarkup(<WorkspaceSplit navigationLabel="研究方向" navigation="方向A" mobileNavigation={<button>选择方向</button>}>实际问题</WorkspaceSplit>)
    assert.ok(detail.includes('实际问题') && detail.includes('选择方向') && detail.includes('data-mobile-selector="true"'))
  })
  it('候选导航保留身份/当前选择，紧凑模式不显示不可用翻页', () => {
    const html = renderToStaticMarkup(<FindingList compact selected={findingSummary.id} items={[findingSummary]} pager={pager} onOpen={() => {}} />)
    assert.ok(html.includes('aria-current="true"') && html.includes(`data-finding-id="${findingSummary.id}"`))
    assert.ok(html.includes('候选 · 未确认') && !html.includes('上一页'))
  })
  it('正文保留真实限制和证据入口，默认不挂载历史与验证正文', () => {
    const html = renderToStaticMarkup(<FindingDetail value={findingFixture} history="历史正文标记" verification="验证正文标记" onEvidence={() => {}} />)
    for (const label of ['漏洞描述', '复现步骤', '修复建议', '前提与未验证限制', '查看流量 1']) assert.ok(html.includes(label), label)
    assert.ok(!html.includes('历史正文标记') && !html.includes('验证正文标记') && !html.includes('<img src='))
    assert.ok(html.includes('独立验证') && html.includes('历史与来源'))
  })
  it('证据区保留截断/unknown/来源，不从状态码推断确认', () => {
    const html = renderToStaticMarkup(<FindingDetail value={findingFixture} section="evidence" onEvidence={() => {}} />)
    for (const label of ['捕获不完整', '效果未知', '支持材料', '引用时点']) assert.ok(html.includes(label))
    assert.ok(!html.includes('确认漏洞') && !html.includes('已修复'))
  })
  it('历史和验证内容只在对应分区挂载', () => {
    const history = renderToStaticMarkup(<FindingDetail value={findingFixture} section="history" history="不可变历史正文" verification="独立验证正文" onEvidence={() => {}} />)
    assert.ok(history.includes('不可变历史正文') && history.includes(findingFixture.call_id) && !history.includes('独立验证正文'))
    const verification = renderToStaticMarkup(<FindingDetail value={findingFixture} section="verification" history="不可变历史正文" verification="独立验证正文" onEvidence={() => {}} />)
    assert.ok(verification.includes('独立验证正文') && !verification.includes('不可变历史正文'))
  })
  it('验证结果保留模型判读、实际前提、限制和新证据，正文仍纯文本', () => {
    const html = renderToStaticMarkup(<VerificationResultView result={verificationResultFixture} onEvidence={() => {}} />)
    for (const label of ['模型判读 · 非自动确认', '验证方法', '实际前提', '限制', '本次新证据', '支持材料 1']) assert.ok(html.includes(label))
    assert.ok(!html.includes('<img src=') && !html.includes('已修复'))
  })
})
