import type { HTTPRecordDetail, HTTPRecordPage, HTTPRecordSummary } from './types.ts'

export const HTTP_RECORD_PAGE_SIZE = 25
export const HTTP_RECORD_STATUSES = ['pending', 'approved', 'running', 'succeeded', 'failed', 'rejected', 'outcome_unknown'] as const
export interface HTTPRecordFilters { thread_id: string; method: string; status: string }
export const emptyHTTPRecordFilters: HTTPRecordFilters = { thread_id: '', method: '', status: '' }

export function httpRecordQuery(filters: HTTPRecordFilters, cursor = ''): string {
  const query = new URLSearchParams({ limit: String(HTTP_RECORD_PAGE_SIZE) })
  for (const field of ['thread_id', 'method', 'status'] as const) {
    if (filters[field]) query.set(field, filters[field])
  }
  if (cursor) query.set('cursor', cursor)
  return query.toString()
}

// 只接受本次作用域的记录，整页拒绝重复身份/错误归属，不默默丢行后冒充完整页。
export function validHTTPRecordPage(page: HTTPRecordPage, missionId: string, filters: HTTPRecordFilters): boolean {
  if (!page || !Array.isArray(page.items) || page.items.length > HTTP_RECORD_PAGE_SIZE || typeof page.next_cursor !== 'string') return false
  const seen = new Set<string>()
  return page.items.every((item) => {
    if (!item || typeof item.id !== 'string' || !item.id || seen.has(item.id) || item.mission_id !== missionId || !item.thread_id) return false
    seen.add(item.id)
    return (!filters.thread_id || item.thread_id === filters.thread_id) &&
      (!filters.method || (typeof item.method === 'string' && item.method.toUpperCase() === filters.method)) && (!filters.status || item.status === filters.status) &&
      typeof item.url === 'string' && typeof item.thread_title === 'string' && typeof item.method === 'string' &&
      HTTP_RECORD_STATUSES.some((status) => status === item.status) &&
      typeof item.has_result === 'boolean' && typeof item.truncated === 'boolean' && typeof item.effect_unknown === 'boolean' &&
      typeof item.created_at === 'string' && typeof item.updated_at === 'string' &&
      (item.status_code === undefined || (Number.isInteger(item.status_code) && item.status_code >= 100 && item.status_code <= 999))
  })
}

export function validHTTPRecordDetail(detail: HTTPRecordDetail, missionId: string, selected: Pick<HTTPRecordSummary, 'id' | 'thread_id'>): boolean {
  return Boolean(detail && detail.mission_id === missionId && detail.thread?.mission_id === missionId &&
    detail.thread.id === selected.thread_id && detail.action?.id === selected.id &&
    detail.action.thread_id === selected.thread_id && detail.action.input?.kind === 'http')
}

export function responseStatusText(record: Pick<HTTPRecordSummary, 'status_code' | 'has_result'>): string {
  return record.status_code ? `HTTP ${record.status_code}` : record.has_result ? '未保存响应状态码' : '尚无执行回执'
}
