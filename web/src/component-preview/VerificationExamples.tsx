import { useState } from 'react'
import { Button, Space } from 'antd'
import { Example } from './examples.tsx'
import { TaskModal } from '../ui/TaskModal.tsx'
import { VerificationList, VerificationStartFields, VerificationResultView, type VerificationResult, type VerificationSummary } from '../ui/Verifications.tsx'
import { findingFixture } from './FindingExamples.tsx'
const f = findingFixture
export const verificationFixture: VerificationSummary = { id: f.id, mission_id: f.mission_id, finding_id: f.finding_id, source_revision_id: f.id, source_created_at: f.created_at, thread_id: f.thread_id, created_at: f.created_at, max_steps: 6, status: 'concluded', pause_requested: false, auto_run: false, step_count: 3, verdict: 'supported', has_result: true, end_reason: '' }
export const verificationResultFixture: VerificationResult = { attempt_id: f.attempt_id, call_id: f.call_id, created_at: f.created_at, conclusion: { verdict: 'supported', summary: '<img src="https://invalid.example/">仅为组件文本；新响应支持候选主张。', method: '独立获取响应并与正常行为比较。', preconditions: '本机公开fixture，无登录身份。', limitations: '不代表完整确认或真实模型质量。', evidence: f.content.evidence }, evidence: f.evidence }
export function VerificationExamples() {
  const [open, setOpen] = useState(false), [steps, setSteps] = useState(6), [mode, setMode] = useState('正常')
  const pager = { onPrevious() {}, onNext() {} }
  return <Example title="独立验证 · 纯组件fixture"><Space wrap>{['正常', '空', '加载', '失败', '长文'].map(v => <Button key={v} onClick={() => setMode(v)}>{v}</Button>)}<Button onClick={() => setOpen(true)}>独立验证</Button></Space>
    <VerificationList items={mode === '空' ? [] : [verificationFixture]} busy={mode === '加载'} error={mode === '失败' ? '验证读取失败' : ''} pager={pager} onOpen={() => setMode('正常')} />
    <VerificationResultView result={mode === '空' || mode === '失败' ? null : { ...verificationResultFixture, conclusion: { ...verificationResultFixture.conclusion, summary: verificationResultFixture.conclusion.summary.repeat(mode === '长文' ? 20 : 1) } }} nextStep="执行失败，已有证据保留" onEvidence={() => setMode('空')} />
    <VerificationList items={[]} pager={pager} onOpen={() => {}} /><VerificationResultView nextStep="目标不可达，未形成结论" onEvidence={() => {}} error="验证读取失败" />
    <TaskModal title="独立验证" open={open} onCancel={() => setOpen(false)} footer={<Button onClick={() => setOpen(false)}>关闭样例</Button>}><VerificationStartFields number={f.number} title={f.content.title} sourceAt={f.created_at} model="组件样例（不调用）" origins={['http://127.0.0.1:1']} steps={steps} maximum={8} onSteps={setSteps} /></TaskModal>
  </Example>
}
