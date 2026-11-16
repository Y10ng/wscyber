import { useState } from 'react'
import { Alert, Button, Space } from 'antd'
import { FindingDetail, FindingHistory, FindingList, type FindingRevision, type FindingSummary, type FindingHistoryRow, type FindingSection } from '../ui/Findings.tsx'
import { Example } from './examples.tsx'
import { WorkspaceSplit } from '../ui/WorkspaceSplit.tsx'
import { VerificationResultView } from '../ui/Verifications.tsx'
const id = '00000000-0000-4000-8000-000000000001'
export const findingFixture: FindingRevision = {
  id, finding_id: id, mission_id: id, thread_id: id, number: 'WSC-000001', revision: 1, status: 'candidate', created_at: '2026-11-13T01:00:00Z', attempt_id: id, call_id: '1'.repeat(32), event_id: '42', call_started_at: '2026-11-13T00:59:00Z', call_finished_at: '2026-11-13T01:00:00Z', content_sha256: 'a'.repeat(64),
  content: { title: '组件fixture：响应可能包含超出当前角色的字段', description: '<img src="https://invalid.example/no-request">只是文本，不请求外部资源。', steps: [{ text: '使用受控账号访问测试端点，核对保存的响应字段。', evidence_indices: [0] }], remediation: '按当前角色限制返回字段，并补充回归检查。', limitations: '尚未完成独立验证；保留对照反证和截断限制。', evidence: [{ kind: 'action', id, relation: 'supporting' }] },
  evidence: [{ kind: 'action', id, relation: 'supporting', thread_id: id, attempt_id: id, action_id: id, created_at: '2026-11-13T00:58:00Z', captured_at: '2026-11-13T00:59:00Z', content_sha256: 'b'.repeat(64), state: 'outcome_unknown', action_kind: 'http', request_hash: 'c'.repeat(64), has_result: true, truncated: true, effect_unknown: true }],
}
export const findingSummary: FindingSummary = { id, mission_id: id, thread_id: id, number: findingFixture.number, title: findingFixture.content.title, status: 'candidate', revision: 1, revision_id: id, created_at: findingFixture.created_at, updated_at: findingFixture.created_at }
export const findingHistory: FindingHistoryRow = { id, finding_id: id, mission_id: id, thread_id: id, revision: 1, title: findingFixture.content.title, created_at: findingFixture.created_at, attempt_id: id, call_id: findingFixture.call_id, event_id: '42' }
export function FindingExamples() {
  const [selected, setSelected] = useState(true), [section, setSection] = useState<FindingSection>('overview'), [evidence, setEvidence] = useState(false), [mode, setMode] = useState('正常')
  const pager = { onPrevious() {}, onNext() {} }
  return <><Example title="候选列表 / 三段详情 / 证据历史 · 组件fixture"><Space wrap>{['正常', '空', '加载', '失败', '长内容'].map(label => <Button key={label} onClick={() => setMode(label)}>{label}</Button>)}</Space>
    <WorkspaceSplit navigationLabel="候选导航样例" detailOpen={selected} navigation={<FindingList compact={selected} selected={findingSummary.id} items={mode === '空' ? [] : [{ ...findingSummary, title: mode === '长内容' ? findingSummary.title.repeat(8) : findingSummary.title }]} busy={mode === '加载'} error={mode === '失败' ? '候选读取失败' : ''} pager={pager} onOpen={() => setSelected(true)} />}>
    {selected && <FindingDetail value={mode === '加载' ? null : { ...findingFixture, content: { ...findingFixture.content, description: mode === '长内容' ? findingFixture.content.description.repeat(30) : findingFixture.content.description } }} actions={<Button onClick={() => setSelected(false)}>返回列表</Button>} error={mode === '失败' ? '详情未更新' : ''} section={section} onSectionChange={setSection} onEvidence={() => setEvidence(!evidence)} history={<FindingHistory items={[findingHistory]} pager={pager} onOpen={() => {}} />} verification={<VerificationResultView nextStep="组件样例，尚未执行独立验证" onEvidence={() => {}} />} />}
    </WorkspaceSplit>
    {evidence && <Alert type="info" title="组件样例仅回调；真实原件读取由业务入口负责" closable onClose={() => setEvidence(false)} />}
  </Example><Example title="候选空 / 加载 / 错误状态"><FindingList items={[]} pager={pager} onOpen={() => {}} /><FindingDetail busy onEvidence={() => {}} /><FindingList items={[]} error="候选读取失败" pager={pager} onOpen={() => {}} /></Example></>
}
