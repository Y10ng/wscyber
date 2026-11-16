import { useState } from 'react'
import { Radio } from 'antd'
import { DailyUsageChart, type DailyUsagePoint } from '../ui/DailyUsageChart.tsx'
import { Example } from './examples.tsx'
const samples: DailyUsagePoint[] = Array.from({ length: 7 }, (_, i) => ({ date: `2026-11-${String(i + 1).padStart(2, '0')}`, input: String([320, 680, 220, 510, 830, 400, 720][i]), output: String([40, 130, 90, 250, 160, 70, 320][i]), calls: String(i + 1), missingCalls: '0' }))
export function DailyUsageExamples() {
  const [state, setState] = useState('完整')
  const source = state === '93日' ? Array.from({ length: 93 }, (_, i) => ({ ...samples[i % 7], date: new Date(Date.UTC(2026, 7, i + 1)).toISOString().slice(0, 10) })) : samples
  const rows = state === '空' ? [] : source.map((r, i) => {
    if (state === '缺口' && i === 3) return { ...r, missingCalls: '2' }
    if (state === '未知') return { ...r, input: null, output: null, missingCalls: r.calls }
    if (state === '大整数') return { ...r, input: `${r.input}9007199254740993`, output: `${r.output}9007199254740993` }
    if (state === '末点接近') return { ...r, output: i === 6 ? '719' : r.output }
    if (state === '重合') return { ...r, output: r.input }
    if (state === '极端比例') return { ...r, input: '9007199254740993123', output: '1' }
    return r
  })
  return <Example title="日用量图与精确表格（组件fixture）"><Radio.Group aria-label="日用量样例状态" value={state} onChange={e => setState(e.target.value)} options={['完整', '缺口', '未知', '空', '大整数', '末点接近', '重合', '极端比例', '93日', '刷新中']} /><DailyUsageChart rows={rows} busy={state === '刷新中'} /></Example>
}
