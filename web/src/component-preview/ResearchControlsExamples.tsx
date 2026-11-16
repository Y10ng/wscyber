import { useState } from 'react'
import { ResearchControlBar, SupplementEditor, SupplementList } from '../ui/ResearchControls.tsx'
import { emptySupplement } from '../ui/researchControls.ts'
import { SkillCallFacts } from '../ui/SkillRecords.tsx'
import { Example } from './examples.tsx'
export function ResearchControlsExamples() {
  const [value, change] = useState(emptySupplement())
  return <><Example title="研究控制与人工补充 · 组件fixture"><ResearchControlBar pauseRequested autoRun={false} steps={3} maxSteps={8} onCommand={() => {}} onSupplement={() => {}} /><SupplementEditor value={value} onChange={change} /></Example>
    <Example title="在途 / 预算耗尽 / 未知暂停意图"><ResearchControlBar pauseRequested={undefined} autoRun steps={8} maxSteps={8} continuationBlocked runBlocked onCommand={() => {}} onSupplement={() => {}} /><SupplementEditor value={{ source: '只读长来源'.repeat(15), content: '<script>纯文本</script>\n'.repeat(80), share_with_model: false }} disabled error="补充正文超出容量" onChange={() => {}} /></Example>
    <Example title="补充记录空态 / 错误 / 加载"><SupplementList items={[]} onOpen={() => {}} onCalls={() => {}} /><SupplementList items={[]} error="读取失败" onOpen={() => {}} onCalls={() => {}} /><SupplementList items={[]} busy onOpen={() => {}} onCalls={() => {}} /><SkillCallFacts kind="supplement" items={[{ id: 'fixture-call', model: 'loopback-fixture', outcome: 'interrupted', attempted: null, started_at: '组件fixture' }]} pager={{ onNext() {}, onPrevious() {} }} /></Example></>
}
