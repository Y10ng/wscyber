import { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Checkbox, Input, Popover, Select, Tooltip } from 'antd'
import { FilterOutlined, SearchOutlined, SettingOutlined } from '@ant-design/icons'
import type { DemoTrafficRequest } from './types.ts'
import { TrafficDetail } from './TrafficDetail.tsx'
import { clampSplitRatio } from './surface.ts'
import { contentType, EMPTY_FILTER, filterTraffic, resolveTrafficSelection, trafficCell, type TrafficSort } from './trafficUtils.ts'

const columnOptions: { key: TrafficSort; label: string; width: number }[] = [
  { key: 'id', label: '编号', width: 116 }, { key: 'method', label: '方法', width: 90 },
  { key: 'host', label: '主机', width: 220 }, { key: 'path', label: '路径', width: 300 },
  { key: 'status', label: '状态', width: 112 }, { key: 'type', label: '类型', width: 144 },
  { key: 'time', label: '时间', width: 198 }, { key: 'source', label: '来源', width: 230 },
]

export function TrafficWorkspace({ projectId, requests, selectedId, onSelect, onResearch, onFinding, onModel }: {
  projectId: string; requests: DemoTrafficRequest[]; selectedId: string | null
  onSelect: (id: string | null) => void
  onResearch: (id: string) => void; onFinding: (id: string) => void; onModel: (id: string) => void
}) {
  const [filter, setFilter] = useState(EMPTY_FILTER)
  const [sort, setSort] = useState<TrafficSort>('id')
  const [ascending, setAscending] = useState(true)
  const [columns, setColumns] = useState<TrafficSort[]>(['id', 'method', 'host', 'path', 'status', 'type'])
  const [ratio, setRatio] = useState(28)
  const [dragging, setDragging] = useState(false)
  const stageRef = useRef<HTMLDivElement>(null)
  const tableRef = useRef<HTMLDivElement>(null)
  const rows = useMemo(() => filterTraffic(requests, { ...filter, projectId }, sort, ascending), [requests, filter, projectId, sort, ascending])
  const selected = resolveTrafficSelection(rows, selectedId)
  const visibleColumns = columnOptions.filter((column) => columns.includes(column.key))
  const filtered = filter.query !== '' || filter.method !== 'all' || filter.status !== 'all'
  useEffect(() => { if ((selected?.id ?? null) !== selectedId) onSelect(selected?.id ?? null) }, [selected?.id, selectedId, onSelect])
  useEffect(() => {
    const container = tableRef.current
    const row = container?.querySelector<HTMLElement>(`[data-record-id="${selected?.id}"]`)
    if (container && row) {
      const bounds = container.getBoundingClientRect(), item = row.getBoundingClientRect()
      // Scroll only the history pane, leaving the shell and message panes stationary.
      if (item.bottom > bounds.bottom) container.scrollTop += item.bottom - bounds.bottom
      else if (item.top < bounds.top + 44) container.scrollTop += item.top - bounds.top - 44
    }
  }, [selected?.id])
  function moveSelection(direction: number) {
    const index = rows.findIndex((row) => row.id === selected?.id)
    const next = rows[Math.min(rows.length - 1, Math.max(0, index + direction))]
    if (!next) return
    onSelect(next.id)
    const row = tableRef.current?.querySelector<HTMLElement>(`[data-record-id="${next.id}"]`)
    row?.focus({ preventScroll: true })
  }
  function resizeFromPointer(clientY: number) {
    const rect = stageRef.current?.getBoundingClientRect()
    if (!rect) return
    setRatio(clampSplitRatio((clientY - rect.top) / Math.max(1, rect.height - 8) * 100, rect.height))
  }
  return <section className="traffic-workspace" aria-label="流量工作区">
    <div className="traffic-toolbar" role="search" aria-label="流量筛选">
      <Input className="url-search" prefix={<SearchOutlined />} aria-label="搜索 URL 或路径" placeholder="搜索 URL 或路径" allowClear value={filter.query} onChange={(e) => setFilter({ ...filter, query: e.target.value })} />
      <Select aria-label="筛选请求方法" value={filter.method} onChange={(method) => setFilter({ ...filter, method })} options={['all', 'GET', 'POST', 'PUT', 'DELETE'].map((value) => ({ value, label: value === 'all' ? '所有方法' : value }))} />
      <Select aria-label="筛选响应状态" value={filter.status} onChange={(status) => setFilter({ ...filter, status })} options={[{ value: 'all', label: '所有状态' }, { value: '2', label: '2xx 成功' }, { value: '3', label: '3xx 重定向' }, { value: '4', label: '4xx 客户端错误' }, { value: '5', label: '5xx 服务端错误' }, { value: 'unknown', label: '发送效果未知' }, { value: 'unsent', label: '越界未发送' }]} />
      <Tooltip title="清除筛选"><Button aria-label="清除流量筛选" icon={<FilterOutlined />} disabled={!filtered} onClick={() => setFilter(EMPTY_FILTER)}>清除</Button></Tooltip>
      <Popover trigger="click" placement="bottomRight" title="显示列" content={<div className="column-options">{columnOptions.map((column) => <Checkbox key={column.key} checked={columns.includes(column.key)} disabled={['id', 'path'].includes(column.key)} onChange={(e) => setColumns(e.target.checked ? [...columns, column.key] : columns.filter((key) => key !== column.key))}>{column.label}</Checkbox>)}</div>}>
        <Button aria-label="流量列设置" icon={<SettingOutlined />}>列</Button>
      </Popover>
    </div>
    <div ref={stageRef} className="traffic-split" data-dragging={dragging} style={{ gridTemplateRows: `minmax(150px, ${ratio}fr) 8px minmax(380px, ${100 - ratio}fr)` }}>
      <div className="traffic-table-region" ref={tableRef}>
        <table className="traffic-table" aria-label="目标流量记录" style={{ minWidth: visibleColumns.reduce((sum, c) => sum + c.width, 0) }}>
          <colgroup>{visibleColumns.map((column) => <col key={column.key} style={{ width: column.key === 'path' ? undefined : column.width }} />)}</colgroup>
          <thead><tr>{visibleColumns.map((column) => <th key={column.key} scope="col" aria-sort={sort === column.key ? ascending ? 'ascending' : 'descending' : 'none'}><button aria-label={`按${column.label}排序`} onClick={() => { setSort(column.key); setAscending(sort === column.key ? !ascending : true) }}>{column.label}<span>{sort === column.key ? ascending ? '↑' : '↓' : ''}</span></button></th>)}</tr></thead>
          <tbody>{rows.map((request) => <tr key={request.id} data-record-id={request.id} tabIndex={selected?.id === request.id ? 0 : -1} aria-selected={selected?.id === request.id} className={selected?.id === request.id ? 'selected' : ''} onClick={() => onSelect(request.id)} onKeyDown={(e) => {
            if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); moveSelection(e.key === 'ArrowDown' ? 1 : -1) }
            if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onSelect(request.id) }
          }}>{visibleColumns.map((column) => <td key={column.key} className={column.key === 'method' ? 'method-cell' : ''}><span className={`cell-text ${column.key === 'status' && request.statusCode === null ? 'warning-text' : ''}`} title={String(trafficCell(request, column.key))}>{column.key === 'status' ? request.statusCode ?? (request.sendStatus === '越界未发送' ? '未发送' : '未知') : column.key === 'type' ? contentType(request).replace('application/', '').replace('text/', '') : trafficCell(request, column.key)}</span></td>)}</tr>)}</tbody>
        </table>
        {!rows.length && <div className="empty-state"><strong>没有匹配的流量</strong><span>{filtered ? '调整筛选条件，或恢复当前项目的全部记录。' : '当前项目尚无已保存的目标流量。'}</span>{filtered && <Button aria-label="恢复全部流量" onClick={() => setFilter(EMPTY_FILTER)}>清除筛选</Button>}</div>}
      </div>
      <div className="splitter" role="separator" aria-label="调整流量列表与报文高度" aria-orientation="horizontal" aria-valuemin={22} aria-valuemax={65} aria-valuenow={Math.round(ratio)} tabIndex={0}
        onKeyDown={(e) => { if (['ArrowUp', 'ArrowDown', 'Home', 'End'].includes(e.key)) { e.preventDefault(); setRatio(clampSplitRatio(e.key === 'Home' ? 22 : e.key === 'End' ? 65 : ratio + (e.key === 'ArrowUp' ? -5 : 5), stageRef.current?.clientHeight)) } }}
        onPointerDown={(e) => { setDragging(true); e.currentTarget.setPointerCapture(e.pointerId); resizeFromPointer(e.clientY) }}
        onPointerMove={(e) => { if (e.currentTarget.hasPointerCapture(e.pointerId)) resizeFromPointer(e.clientY) }}
        onPointerCancel={() => setDragging(false)} onLostPointerCapture={() => setDragging(false)} onPointerUp={(e) => { setDragging(false); setRatio((value) => clampSplitRatio(Math.round(value * 2) / 2, stageRef.current?.clientHeight)); if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId) }}><span /></div>
      {selected ? <TrafficDetail key={selected.id} request={selected} onResearch={onResearch} onFinding={onFinding} onModel={onModel} /> : <div className="empty-state"><strong>没有选中的报文</strong><span>选择当前项目的一条流量记录。</span></div>}
    </div>
    <footer className="workspace-status"><span>{rows.length} 条记录{filtered ? ' · 已筛选' : ''}</span><span>{selected?.id ?? '未选择'} · ↑ ↓ 切换记录</span></footer>
  </section>
}
