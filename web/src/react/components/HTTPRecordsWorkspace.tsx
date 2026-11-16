import { useCallback, useLayoutEffect, useRef, useState } from 'react'
import { Alert, Button, Empty, Form, Input, Select, Space, Table, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { ApiError, fetchHTTPRecordDetail, fetchHTTPRecords } from '../api.ts'
import type { HTTPRecordDetail, HTTPRecordPage, HTTPRecordSummary, Mission, Thread } from '../types.ts'
import { formatDateTime, statusBadgeColor, statusText } from '../types.ts'
import { emptyHTTPRecordFilters, HTTP_RECORD_STATUSES, httpRecordQuery, validHTTPRecordDetail, validHTTPRecordPage } from '../httpRecords.ts'
import type { HTTPRecordFilters } from '../httpRecords.ts'
import { useFindingRead as useRecordRead } from '../useRecordRead.ts'
import { ActionEvidencePanel } from './ActionEvidenceDrawer.tsx'

export function splitHTTPURL(value: string): { origin: string; path: string } {
  try {
    const url = new URL(value)
    return { origin: url.origin, path: `${url.pathname}${url.search}${url.hash}` || '/' }
  } catch {
    return { origin: '', path: value }
  }
}

export function HTTPRecordsWorkspace({ mission, threads, active }: { mission: Mission; threads: Thread[]; active: boolean }) {
  const [filters, setFilters] = useState<HTTPRecordFilters>(emptyHTTPRecordFilters)
  const [cursors, setCursors] = useState([''])
  const [pageIndex, setPageIndex] = useState(0)
  const query = httpRecordQuery(filters, cursors[pageIndex])
  const filter = (next: HTTPRecordFilters) => { setFilters(next); setCursors(['']); setPageIndex(0) }
  return <div className="ws-http-workspace">
    <Form layout="inline" className="ws-http-filters" onFinish={(values: { method?: string }) => filter({ ...filters, method: (values.method ?? '').trim().toUpperCase() })}>
      <Form.Item name="method" rules={[{ validator: (_, value: string | undefined) => !value?.trim() || /^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,32}$/.test(value.trim()) ? Promise.resolve() : Promise.reject(new Error('请输入 1–32 位 HTTP 方法标记')) }]}><Input aria-label="筛选HTTP方法" placeholder="方法（如 GET/POST）" maxLength={32} allowClear style={{ width: 150 }} /></Form.Item>
      <Form.Item><Select aria-label="筛选研究方向" value={filters.thread_id} onChange={thread_id => filter({ ...filters, thread_id })} options={[{ value: '', label: '全部方向' }, ...threads.filter(thread => thread.mission_id === mission.id).map(thread => ({ value: thread.id, label: thread.title }))]} style={{ width: 190 }} /></Form.Item>
      <Form.Item><Select aria-label="筛选动作状态" value={filters.status} onChange={status => filter({ ...filters, status })} options={[{ value: '', label: '全部状态' }, ...HTTP_RECORD_STATUSES.map(value => ({ value, label: statusText(value) }))]} style={{ width: 150 }} /></Form.Item>
      <Form.Item><Button htmlType="submit">应用筛选</Button></Form.Item>
    </Form>
    <HTTPRecordsPage key={query} mission={mission} filters={filters} query={query} active={active} pageIndex={pageIndex}
      onPrevious={() => setPageIndex(n => Math.max(0, n - 1))}
      onLatest={() => { setCursors(['']); setPageIndex(0) }}
      onNext={cursor => { setCursors(old => [...old.slice(0, pageIndex + 1), cursor]); setPageIndex(pageIndex + 1) }} />
  </div>
}

function HTTPRecordsPage({ mission, filters, query, active, pageIndex, onPrevious, onNext, onLatest }: {
  mission: Mission; filters: HTTPRecordFilters; query: string; active: boolean; pageIndex: number
  onPrevious: () => void; onNext: (cursor: string) => void; onLatest: () => void
}) {
  const read = useCallback(async (signal: AbortSignal): Promise<HTTPRecordPage> => {
    const result = await fetchHTTPRecords(mission.id, query, signal)
    if (!validHTTPRecordPage(result, mission.id, filters)) throw new ApiError('记录归属或格式不一致，未显示本次结果。', 400)
    return result
  }, [mission.id, query, filters])
  const list = useRecordRead(query, active, read)
  const [selected, setSelected] = useState<HTTPRecordSummary | null>(null)
  const visibleSelection = active && selected && list.value?.items.some(item => item.id === selected.id && item.thread_id === selected.thread_id) ? selected : null
  useLayoutEffect(() => { if (selected && !visibleSelection) setSelected(null) }, [selected, visibleSelection])
  const selectionRef = useRef(visibleSelection)
  useLayoutEffect(() => { selectionRef.current = visibleSelection; return () => { selectionRef.current = null } }, [visibleSelection])
  const detailRead = useCallback(async (signal: AbortSignal): Promise<HTTPRecordDetail> => {
    if (!visibleSelection) throw new ApiError('未选择记录', 400)
    const detail = await fetchHTTPRecordDetail(mission.id, visibleSelection.id, signal)
    if (!validHTTPRecordDetail(detail, mission.id, visibleSelection)) throw new ApiError('详情归属不一致，未显示本次结果。', 400)
    return detail
  }, [mission.id, visibleSelection])
  const detail = useRecordRead(visibleSelection?.id ?? '', Boolean(visibleSelection), detailRead)
  const context = useRef<HTTPRecordPage | null>(null)
  useLayoutEffect(() => { context.current = active ? list.value : null; return () => { context.current = null } }, [active, list.value])
  const inspect = (record: HTTPRecordSummary) => {
    if (context.current !== list.value || !context.current?.items.includes(record)) return
    setSelected(record)
  }
  const validDetail = visibleSelection && detail.value && validHTTPRecordDetail(detail.value, mission.id, visibleSelection) ? detail.value : null
  return <section className="ws-traffic-surface">
    <div className="ws-traffic-toolbar"><div><h2>HTTP 记录</h2><span>{list.value ? `本页 ${list.value.items.length} 条` : '读取中'}</span></div><Button icon={<ReloadOutlined />} loading={list.busy} onClick={() => { list.refresh(); if (selectionRef.current) detail.refresh() }}>刷新记录</Button></div>
    {list.error && <Alert type="error" showIcon title={`记录未更新：${list.error}${list.value ? '（保留上次读取内容）' : ''}`} />}
    {detail.error && <Alert type="error" showIcon title={`详情未更新：${detail.error}${detail.value ? '（保留上次读取详情）' : ''}`} action={<Button onClick={detail.refresh}>重试详情</Button>} />}
    <Table<HTTPRecordSummary> rowKey="id" dataSource={list.value?.items ?? []} pagination={false} loading={list.busy && !list.value}
      onRow={row => ({ onClick: () => inspect(row), 'aria-selected': visibleSelection?.id === row.id })}
      rowClassName={row => `ws-traffic-row${visibleSelection?.id === row.id ? ' is-selected' : ''}`}
      scroll={{ x: 1080 }} tableLayout="fixed" className="ws-http-table"
      locale={{ emptyText: list.error ? '尚未取得记录' : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无匹配的 HTTP 记录" /> }}
      columns={[
        { title: '时间', key: 'created_at', width: 150, render: (_, row) => <time dateTime={row.created_at} className="ws-time-cell">{formatDateTime(row.created_at)}</time> },
        { title: '方法', key: 'method', width: 70, render: (_, row) => <code className="ws-method">{row.method}</code> },
        { title: 'HOST', key: 'host', width: 210, render: (_, row) => <span className="ws-host" title={splitHTTPURL(row.url).origin}>{splitHTTPURL(row.url).origin || '—'}</span> },
        { title: '路径', key: 'path', render: (_, row) => <code className="ws-url-path" title={row.url}>{splitHTTPURL(row.url).path}</code> },
        { title: '状态', key: 'status', width: 150, render: (_, row) => <span className="ws-http-status-cell"><Tag color={statusBadgeColor(row.status)}>{statusText(row.status)}</Tag>{row.has_result && row.status_code !== undefined && <code className={row.status_code < 400 ? 'ws-response-success' : 'ws-response-failed'}>{row.status_code}</code>}</span> },
        { title: '方向', dataIndex: 'thread_title', width: 170, render: (value: string) => <span className="ws-host" title={value}>{value}</span> },
        { title: '标记', key: 'flags', width: 120, render: (_, row) => <span className="ws-http-flags">{row.truncated && <Tag>截断</Tag>}{row.effect_unknown && <Tag color="warning">未知</Tag>}{row.resolved_at && <Tag>已核对</Tag>}</span> },
      ]} />
    {validDetail && <ActionEvidencePanel action={validDetail.action} mission={mission} thread={validDetail.thread} readError={detail.error} refreshing={detail.busy} onRefresh={detail.refresh} onClose={() => setSelected(null)} />}
    <div className="ws-section-row ws-traffic-pagination">
      <Typography.Text type="secondary">第 {pageIndex + 1} 页 · 本页 {list.value?.items.length ?? '—'} 条</Typography.Text>
      <Space wrap><Button disabled={pageIndex === 0} onClick={onLatest}>回到最新</Button><Button disabled={pageIndex === 0} onClick={onPrevious}>上一页</Button><Button disabled={!list.value?.next_cursor || Boolean(list.error)} onClick={() => list.value?.next_cursor && onNext(list.value.next_cursor)}>下一页</Button></Space>
    </div>
  </section>
}
