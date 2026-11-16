import { useState } from 'react'
import { Select } from 'antd'
import { Example } from './examples.tsx'
import { SkillActivityDetail, SkillActivityList, SkillCallFacts, SkillUsageList, type SkillActivityRow } from '../ui/SkillRecords.tsx'
import { MaterialText } from '../ui/MaterialLibrary.tsx'
const row: SkillActivityRow = { sequence: '1', mission_id: 'fixture-project', thread_id: 'fixture-direction', attempt_id: 'fixture-attempt', kind: 'skill_load', created_at: '2026-11-12 12:00 UTC', request: { purpose: '本地组件fixture，不是真实项目加载' }, result: { status: 'loaded', load_id: 'fixture-load', material: { name: '边界检查方法.md' } } }
export function SkillExamples() {
  const [mode, setMode] = useState('default'), [selected, setSelected] = useState<SkillActivityRow | null>(null)
  const pager = { onPrevious() {}, onNext() {}, previous: true, next: true }, busy = mode === 'loading', error = mode === 'error' ? '读取失败，请重新读取。' : ''
  const items = mode === 'empty' ? [] : [row, { ...row, sequence: '2', attempt_id: 'fixture-failure', result: { status: 'stale' } }]
  return <Example title="Skill使用记录组合（本地组件fixture）"><Select aria-label="Skill组件状态" value={mode} onChange={value => { setMode(value); setSelected(null) }} options={['default', 'loading', 'empty', 'error'].map(value => ({ value, label: value }))} />
    {selected ? <SkillActivityDetail activity={selected} onClose={() => setSelected(null)}>{selected.result.status === 'loaded' && <><MaterialText detail={{ name: '边界检查方法.md', description: '仅本地样例', byte_size: 64, content_sha256: 'fixture', content: '<script>纯文本不执行</script>\n![不加载](https://invalid.example/pixel)' }} /><SkillCallFacts items={[{ id: 'fixture-call', model: 'fixture-model', outcome: '发送结果未知', attempted: true, started_at: row.created_at }]} pager={pager} /></>}</SkillActivityDetail> : <><SkillUsageList items={mode === 'empty' ? [] : [{ item_id: 'fixture', name: '边界检查方法.md', successful_loads: '1' }]} total={mode === 'empty' ? '0' : '1'} busy={busy} error={error} pager={pager} /><SkillActivityList items={items} busy={busy} error={error} pager={pager} onOpen={setSelected} /></>}
  </Example>
}
