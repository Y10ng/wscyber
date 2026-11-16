import { useEffect, useId, useRef, useState } from 'react'
import { Empty, Radio, Table } from 'antd'
import { compactInteger, exactDecimal, integerValue, plotRatio } from './exactDecimal.ts'
import './dailyUsage.css'

export interface DailyUsagePoint { date: string; input: string | null; output: string | null; calls: string; missingCalls: string }
export function usagePlotMaximum(rows: DailyUsagePoint[]): bigint {
  let max = 0n
  for (const row of rows) for (const value of [row.input, row.output]) {
    const n = integerValue(value)
    if (n !== null && n > max) max = n
  }
  if (max === 0n) return 4n
  const step = 10n ** BigInt(Math.max(0, max.toString().length - 1))
  return ((max + 4n * step - 1n) / (4n * step)) * 4n * step
}
export const hasUsageGap = (row: DailyUsagePoint) => integerValue(row.missingCalls) !== 0n

/** 直标贴近各自末点；两个标签碰撞时只做最小纵向分离。 */
export function usageEndLabels(input: number | null, output: number | null, top = 24, bottom = 208): [number | null, number | null] {
  if (input === null || output === null || Math.abs(input - output) >= 24) return [input, output]
  const upper = Math.max(top, Math.min(bottom - 24, (input + output) / 2 - 12))
  return input <= output ? [upper, upper + 24] : [upper + 24, upper]
}

/** 最多93个日桶。只负责图/表表现，不发请求、不猜用量。 */
export function DailyUsageChart({ rows, label = '每日已知 tokens', busy = false }: { rows: DailyUsagePoint[]; label?: string; busy?: boolean }) {
  const [view, setView] = useState('chart'), [active, setActive] = useState<number | null>(null)
  const [viewportWidth, setViewportWidth] = useState(800), [scrollLeft, setScrollLeft] = useState(0)
  const width = Math.max(620, viewportWidth)
  const container = useRef<HTMLDivElement>(null)
  const titleID = useId(), readoutID = useId()
  useEffect(() => {
    if (!container.current) return
    setScrollLeft(container.current.scrollLeft)
    const observer = new ResizeObserver(([entry]) => setViewportWidth(Math.floor(entry.contentRect.width)))
    observer.observe(container.current)
    return () => observer.disconnect()
  }, [view, rows.length === 0])
  // 调用方刷新其他面板时可传入同值新数组；不能因此吞掉键盘选中日。
  useEffect(() => setActive(index => index === null || rows.length === 0 ? null : Math.min(index, rows.length - 1)), [rows.length])
  useEffect(() => setActive(null), [view])
  const maximum = usagePlotMaximum(rows)
  const left = 76, right = width - 96, top = 24, bottom = 208
  const x = (i: number) => left + (rows.length < 2 ? (right - left) / 2 : i * (right - left) / (rows.length - 1))
  const y = (value: string | null) => { const ratio = plotRatio(value, maximum); return ratio === null ? null : bottom - ratio * (bottom - top) }
  const series = [{ key: 'input' as const, name: '输入', shape: '●' }, { key: 'output' as const, name: '输出', shape: '◆' }]
  const selected = active === null ? null : rows[active]
  const ends = series.map(s => {
    let index = -1
    rows.forEach((row, i) => { if (!hasUsageGap(row) && y(row[s.key]) !== null) index = i })
    return { index, y: index < 0 ? null : y(rows[index][s.key]) }
  })
  const labelY = usageEndLabels(ends[0].y, ends[1].y, top, bottom)
  const tooltipWidth = Math.max(0, Math.min(260, viewportWidth - 16))
  return <section className="daily-usage" aria-label={label} aria-busy={busy}>
    <div className="daily-usage-heading"><h3 id={titleID}>{label}</h3><Radio.Group aria-label="日用量视图" value={view} onChange={e => setView(e.target.value)} optionType="button" options={[{ label: '趋势图', value: 'chart' }, { label: '精确表格', value: 'table' }]} /></div>
    {rows.length === 0 ? <Empty description="暂无日用量记录" /> : view === 'table' ? <Table size="small" rowKey="date" dataSource={rows} pagination={false} scroll={{ x: 600, y: 360 }} columns={[
      { title: '日期', dataIndex: 'date', width: 140 },
      { title: '调用', dataIndex: 'calls', render: exactDecimal, align: 'right' },
      { title: '已知输入', dataIndex: 'input', render: exactDecimal, align: 'right' },
      { title: '已知输出', dataIndex: 'output', render: exactDecimal, align: 'right' },
      { title: '用量缺口（调用）', dataIndex: 'missingCalls', render: exactDecimal, align: 'right' },
    ]} /> : <>
      <div className="daily-usage-legend">{series.map(s => <span key={s.key}><span aria-hidden="true" className={`usage-key usage-${s.key}`}>{s.shape}</span>{s.name}</span>)}{rows.some(hasUsageGap) && <span>× 用量不完整，曲线留空</span>}</div>
      <div ref={container} className="daily-usage-scroll" onScroll={e => setScrollLeft(e.currentTarget.scrollLeft)}>
        <div className="daily-usage-plot" role="group" tabIndex={0} aria-labelledby={titleID} aria-describedby={readoutID} style={{ width }}
          onFocus={() => { setActive(0); container.current?.scrollTo({ left: 0 }) }} onBlur={() => setActive(null)}
          onKeyDown={e => {
            if (e.key === 'Escape') { setActive(null); return }
            if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return
            e.preventDefault()
            const next = e.key === 'Home' ? 0 : e.key === 'End' ? rows.length - 1 : Math.max(0, Math.min(rows.length - 1, (active ?? 0) + (e.key === 'ArrowRight' ? 1 : -1)))
            setActive(next)
            if (container.current) container.current.scrollTo({ left: Math.max(0, x(next) - container.current.clientWidth / 2) })
          }}
          onPointerMove={e => { const pos = e.clientX - e.currentTarget.getBoundingClientRect().left; setActive(Math.max(0, Math.min(rows.length - 1, Math.round((pos - left) / (right - left) * (rows.length - 1))))) }}
          onPointerLeave={e => { if (document.activeElement !== e.currentTarget) setActive(null) }}>
          <svg width={width} height={252} role="img" aria-label={`${label}；左右方向键逐日查看，或切换精确表格`}>
            {[0, 1, 2, 3, 4].map(i => <g key={i}><line className="usage-grid" x1={left} x2={right} y1={bottom - i * (bottom - top) / 4} y2={bottom - i * (bottom - top) / 4} /><text x={left - 12} y={bottom - i * (bottom - top) / 4 + 5} textAnchor="end">{compactInteger(maximum * BigInt(i) / 4n)}</text></g>)}
            {rows.map((row, i) => (i === 0 || i === rows.length - 1 || (i % Math.max(1, Math.ceil(rows.length / 6)) === 0 && x(rows.length - 1) - x(i) >= 60)) && <text key={row.date} x={x(i)} y={236} textAnchor="middle">{row.date.slice(5)}</text>)}
            {rows.map((row, i) => hasUsageGap(row) && <text key={row.date} x={x(i)} y={bottom - 8} textAnchor="middle">×</text>)}
            {series.map((s, seriesIndex) => {
              let connected = false
              const path = rows.map((row, i) => { const py = hasUsageGap(row) ? null : y(row[s.key]); if (py === null) { connected = false; return '' }; const part = `${connected ? 'L' : 'M'}${x(i)},${py}`; connected = true; return part }).join(' ')
              const { index: end, y: endY } = ends[seriesIndex]
              const textY = labelY[seriesIndex]
              return <g key={s.key} className={`usage-${s.key}`}><path className="usage-line" d={path} />{rows.map((row, i) => {
                const py = hasUsageGap(row) ? null : y(row[s.key]); if (py === null || (rows.length > 14 && i !== active && i !== end)) return null
                return s.key === 'input' ? <circle key={row.date} className="usage-mark" cx={x(i)} cy={py} r={4} /> : <path key={row.date} className="usage-mark" d={`M${x(i)},${py - 5} l5,5 -5,5 -5,-5 Z`} />
              })}{endY !== null && textY !== null && <><path className="usage-leader" d={`M${x(end) + 6},${endY} L${right + 14},${textY}`} /><text className="usage-end-label" x={right + 20} y={textY + 5}>{s.name}</text></>}</g>
            })}
            {active !== null && <line className="usage-crosshair" x1={x(active)} x2={x(active)} y1={top} y2={bottom} />}
          </svg>
          {selected && <div className="daily-usage-tooltip" style={{ width: tooltipWidth, left: Math.max(scrollLeft + 8, Math.min(scrollLeft + viewportWidth - tooltipWidth - 8, x(active!) - tooltipWidth / 2)) }}>
            <span>{selected.date}{hasUsageGap(selected) ? ' · 部分已知' : ''}</span>{series.map(s => <div key={s.key}><span className={`usage-key usage-${s.key}`} aria-hidden="true">{s.shape}</span><strong>{exactDecimal(selected[s.key])}</strong><span>{s.name}</span></div>)}<span>{exactDecimal(selected.calls)} 次调用 · {exactDecimal(selected.missingCalls)} 次用量缺口</span>
          </div>}
        </div>
      </div>
      <span id={readoutID} className="sr-only" aria-live="polite">{selected ? `${selected.date}，已知输入${exactDecimal(selected.input)}，已知输出${exactDecimal(selected.output)}，用量缺口${exactDecimal(selected.missingCalls)}次` : '聚焦后使用左右方向键逐日查看；Home/End到首尾；精确表格提供全部数值。'}</span>
    </>}
  </section>
}
