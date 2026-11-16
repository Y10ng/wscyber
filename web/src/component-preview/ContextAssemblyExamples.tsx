import { useState } from 'react'
import { ContextAssemblyView, type ModelCallContext } from '../ui/ContextAssembly.tsx'
import { Example } from './examples.tsx'
export const contextFixture: ModelCallContext = {
  call_id: 'fixture-call', mission_id: 'fixture-mission', thread_id: 'fixture-thread', recorded: true,
  assembly: { policy: 'bounded-history-1', mission_id: 'fixture-mission', thread_id: 'fixture-thread', attempt_id: 'fixture-attempt', assembled_at: '2026-11-12T21:00:00Z', input_bytes: 36200, max_input_bytes: 262144,
    windows: ['observation', 'action', 'attempt'].map(kind => ({ kind, limit: kind === 'observation' ? 32 : 8, older_exists: kind === 'observation', cutoff_id: '00000000-0000-0000-0000-000000000001', cutoff_at: '2026-11-12T20:00:00Z' })),
    sources: Array.from({ length: 24 }, (_, i) => ({ kind: i === 0 ? 'action' : 'observation', id: `00000000-0000-0000-0000-${String(i).padStart(12, '0')}`, at: '2026-11-12T20:30:00Z', state: i === 0 ? 'outcome_unknown' : 'other', included: i !== 2, required: i === 0, reason: i === 2 ? 'item_limit' : 'selected', original_bytes: i === 2 ? 70000 : 300, included_bytes: i === 2 ? 0 : 350 })),
  },
}
export const findingsContextFixture: ModelCallContext = { ...contextFixture, assembly: { ...contextFixture.assembly!, policy: 'bounded-history-findings-1', windows: [...contextFixture.assembly!.windows, { kind: 'finding', limit: 8, older_exists: true, cutoff_id: '00000000-0000-0000-0000-000000000099', cutoff_at: '2026-11-13T01:00:00Z' }], sources: [...contextFixture.assembly!.sources.map(s => ({ ...s, content_sha256: s.included ? 'b'.repeat(64) : undefined })), { kind: 'finding', id: '00000000-0000-0000-0000-000000000099', state: 'candidate', at: '2026-11-13T01:00:00Z', original_bytes: 800, included_bytes: 1000, included: true, required: true, reason: 'selected', content_sha256: 'a'.repeat(64) }] } }
export function ContextAssemblyExamples() {
  const [page, setPage] = useState(0)
  return <><Example title="上下文装配 · 有界引用 / 未读历史 / 未知发送 · 组件fixture"><ContextAssemblyView value={contextFixture} attempted={null} page={page} onPage={setPage} /></Example><Example title="候选固定内容 / 更早候选未读 · 组件fixture"><ContextAssemblyView value={findingsContextFixture} attempted={true} page={1} onPage={() => {}} /></Example><Example title="上下文未记录 / 加载 / 错误"><ContextAssemblyView value={{ ...contextFixture, recorded: false, assembly: null }} attempted={false} onPage={() => {}} /><ContextAssemblyView attempted={null} busy onPage={() => {}} /><ContextAssemblyView attempted={null} error="装配记录读取失败" onPage={() => {}} /></Example></>
}
