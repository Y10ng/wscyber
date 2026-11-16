import { buildRawHttpRequestText, buildRawHttpResponseText, formatDemoHeaders, type DemoTrafficRequest } from './types.ts'

export interface TrafficFilter { projectId: string; query: string; method: string; status: string }
export type TrafficSort = 'id' | 'method' | 'host' | 'path' | 'status' | 'type' | 'time' | 'source'
export type MessageMode = 'Raw' | '格式化' | 'Headers'
export const EMPTY_FILTER = { query: '', method: 'all', status: 'all' }

export function contentType(request: DemoTrafficRequest) {
  return Object.entries(request.responseHeaders ?? {}).find(([key]) => key.toLowerCase() === 'content-type')?.[1].split(';')[0] ?? '—'
}
export function trafficCell(request: DemoTrafficRequest, key: TrafficSort): string | number {
  const url = new URL(request.url)
  switch (key) {
    case 'id': return request.id
    case 'method': return request.method
    case 'host': return url.host
    case 'path': return url.pathname + url.search
    case 'status': return request.statusCode ?? -1
    case 'type': return contentType(request)
    case 'time': return request.capturedAt
    case 'source': return request.captureSource
  }
}
export function filterTraffic(requests: DemoTrafficRequest[], filter: TrafficFilter, sort: TrafficSort = 'id', ascending = true) {
  return requests.filter((request) => request.projectId === filter.projectId
    && request.url.toLowerCase().includes(filter.query.trim().toLowerCase())
    && (filter.method === 'all' || request.method === filter.method)
    && (filter.status === 'all' || (filter.status === 'unknown' ? request.sendStatus === '发送效果未知'
      : filter.status === 'unsent' ? request.sendStatus === '越界未发送'
      : String(request.statusCode).startsWith(filter.status))))
    .sort((a, b) => {
      const x = trafficCell(a, sort), y = trafficCell(b, sort)
      return (typeof x === 'number' && typeof y === 'number' ? x - y : String(x).localeCompare(String(y), 'zh-CN', { numeric: true })) * (ascending ? 1 : -1)
    })
}
// Selection is resolved against the visible, project-scoped set BEFORE rendering details.
export function resolveTrafficSelection(rows: DemoTrafficRequest[], selectedId: string | null) {
  return rows.find((request) => request.id === selectedId) ?? rows[0] ?? null
}
export function messageText(request: DemoTrafficRequest, side: 'request' | 'response', mode: MessageMode, reveal: boolean) {
  const response = side === 'response'
  if (response && (request.statusCode === null || !request.responseHeaders)) {
    return request.sendStatus === '越界未发送' ? '越界未发送。没有目标 HTTP 响应。' : '发送效果未知。未保存目标 HTTP 响应，禁止自动重放。'
  }
  if (mode === 'Raw') return response ? buildRawHttpResponseText(request, reveal) : buildRawHttpRequestText(request, reveal)
  const headers = formatDemoHeaders(response ? request.responseHeaders : request.requestHeaders, reveal).lines.join('\n')
  if (mode === 'Headers') return headers || '没有保存的请求头'
  const body = (response ? request.responseBody : request.requestBody) ?? ''
  if (!body) return headers + '\n\n（正文为空）'
  let formatted = body
  try { formatted = JSON.stringify(JSON.parse(body), null, 2) } catch { /* Non-JSON is plain text, never HTML. */ }
  return headers + '\n\n' + formatted
}
export function searchParts(text: string, query: string): { text: string; match: number | null }[] {
  if (!query) return [{ text, match: null }]
  const lower = text.toLowerCase(), term = query.toLowerCase()
  const parts: { text: string; match: number | null }[] = []
  let cursor = 0, count = 0, index = lower.indexOf(term)
  while (index !== -1) {
    if (index > cursor) parts.push({ text: text.slice(cursor, index), match: null })
    parts.push({ text: text.slice(index, index + query.length), match: count++ })
    cursor = index + query.length
    index = lower.indexOf(term, cursor)
  }
  parts.push({ text: text.slice(cursor), match: null })
  return parts
}
export async function copyLocalText(text: string): Promise<string> {
  try {
    if (!globalThis.navigator?.clipboard?.writeText) return '复制失败：浏览器未开放剪贴板，请手动选择文本。'
    await navigator.clipboard.writeText(text)
    return '已复制当前视图'
  } catch { return '复制失败：剪贴板权限不可用，请手动选择文本。' }
}
