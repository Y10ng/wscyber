import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Info, Loader2, RefreshCw, Search } from 'lucide-react'
import { fromBinary } from '@bufbuild/protobuf'
import { TrafficFlowSchema, type TrafficFlow } from '@cyber/aop'
import { aopFlowToHttpView, HttpViewPanels, type TrafficHttpView } from '@cyber/traffic'
import { cn } from '@cyber/theme'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels'
import {
  getWebApiEgress,
  getWebApiEvidence,
  listWebApiEgress,
  listWebApiEvidence,
  type WebApiEgressDetail,
  type WebApiEgressItem,
  type WebApiEvidence,
  type WebApiEvidenceDetail,
  type WebApiTask,
} from '../api'

interface Props {
  task: WebApiTask
  /** 定位并选中该 evidence（模式同候选面板的 focusFindingId）；消费后回调清空。 */
  focusEvidenceId?: string | null
  onFocusConsumed?: () => void
}

// 把后端持久化的 Flow protobuf 原件解码为 TrafficFlow，再交给原生 HttpViewPanels
// 渲染；原始 body 字节保留可下载，不把替换字符当原件、不重放。
function decodeFlow(base64: string): TrafficFlow {
  const bytes = Uint8Array.from(atob(base64), (char) => char.charCodeAt(0))
  return fromBinary(TrafficFlowSchema, bytes)
}

// 这是经过 TrafficHttpView 文本适配的「展示文本」重建，不是逐字节线上报文。
function viewToRawText(view: TrafficHttpView): string {
  const lines: string[] = []
  lines.push(`${view.request.method} ${view.request.requestTarget} ${view.request.httpVersion}`.trim())
  for (const [key, value] of view.request.headers) lines.push(`${key}: ${value}`)
  if (view.request.body) { lines.push(''); lines.push(view.request.body) }
  if (view.response) {
    lines.push('')
    lines.push(`${view.response.httpVersion} ${view.status} ${view.reason}`.trimEnd())
    for (const [key, value] of view.response.headers) lines.push(`${key}: ${value}`)
    if (view.response.body) { lines.push(''); lines.push(view.response.body) }
  }
  return lines.join('\n')
}

async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch { /* 回落 */ }
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  } catch {
    return false
  }
}

function downloadBytes(bytes: Uint8Array | undefined, filename: string) {
  if (!bytes?.length) return
  // 复制为普通 ArrayBuffer 再交给 Blob（TS5.9 lib.dom 的 BlobPart 不接受裸 Uint8Array
  // 的 ArrayBufferLike 默认类型），保持原始字节，不用 any/断言掩盖。
  const buffer = new Uint8Array(bytes).buffer
  const blob = new Blob([buffer], { type: 'application/octet-stream' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  document.body.removeChild(anchor)
  URL.revokeObjectURL(url)
}

const STATUS_LABEL_KEYS: Record<string, string> = {
  claimed: 'webapi.status_claimed',
  completed: 'webapi.status_completed',
  denied: 'webapi.status_denied',
  review_required: 'webapi.status_review_required',
  review_denied: 'webapi.status_review_denied',
  review_expired: 'webapi.status_review_expired',
  review_canceled: 'webapi.status_review_canceled',
  unknown: 'webapi.status_unknown',
  capture_failed: 'webapi.status_capture_failed',
}

function statusLabel(t: (key: string) => string, status: string): string {
  return t(STATUS_LABEL_KEYS[status] || 'webapi.status_unknown')
}

function statusTone(status: string): string {
  if (status === 'completed') return 'text-emerald-600 dark:text-emerald-400'
  if (status === 'denied' || status === 'capture_failed' || status === 'review_denied') return 'text-red-600 dark:text-red-400'
  if (status === 'review_required') return 'text-amber-600 dark:text-amber-400'
  if (status === 'review_expired' || status === 'review_canceled') return 'text-orange-600 dark:text-orange-400'
  if (status === 'unknown') return 'text-orange-600 dark:text-orange-400'
  if (status === 'claimed') return 'text-blue-600 dark:text-blue-400'
  return 'text-muted-foreground'
}

function methodTone(method: string): string {
  if (method === 'GET') return 'text-emerald-600 dark:text-emerald-400'
  if (method === 'POST') return 'text-blue-600 dark:text-blue-400'
  if (method === 'PUT' || method === 'PATCH') return 'text-amber-600 dark:text-amber-400'
  if (method === 'DELETE') return 'text-red-600 dark:text-red-400'
  return 'text-muted-foreground'
}

// 状态码按段着色：2xx 绿 / 3xx 蓝 / 4xx 琥珀 / 5xx 红（状态色只作强化，文字仍在）。
function httpStatusTone(code: number): string {
  if (code < 300) return 'text-emerald-600 dark:text-emerald-400'
  if (code < 400) return 'text-blue-600 dark:text-blue-400'
  if (code < 500) return 'text-amber-600 dark:text-amber-400'
  return 'text-red-600 dark:text-red-400'
}

function formatClock(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

// Host/路径拆分：解析失败（畸形 URL）时路径列回退整条 URL，不丢信息。
function splitUrl(url: string): { host: string; path: string } {
  try {
    const parsed = new URL(url)
    return { host: parsed.host, path: `${parsed.pathname}${parsed.search}` || '/' }
  } catch {
    return { host: '', path: url }
  }
}

// 终端出口的 CONNECT 记录 url 只有 host:port（没有 scheme）：主机列显示整条，
// 路径列显示「—」；带 scheme 的普通请求仍按 splitUrl 拆分。
function splitEgressUrl(url: string): { host: string; path: string } {
  if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(url)) return { host: url, path: '—' }
  return splitUrl(url)
}

type TrafficSegment = 'all' | '2xx' | '3xx' | '4xx' | '5xx' | 'incomplete'
type EgressSegment = 'all' | 'allowed' | 'denied'

// 分段归类：未完成 = 非 completed 或无响应（无 http_status 即无响应）。
function segmentOf(item: WebApiEvidence): TrafficSegment {
  if (item.status === 'completed' && item.http_status != null) {
    if (item.http_status < 300) return '2xx'
    if (item.http_status < 400) return '3xx'
    if (item.http_status < 500) return '4xx'
    return '5xx'
  }
  return 'incomplete'
}

export default function WebApiEvidencePanel({ task, focusEvidenceId, onFocusConsumed }: Props) {
  const { t } = useTranslation('chat')
  const [items, setItems] = useState<WebApiEvidence[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loadingList, setLoadingList] = useState(false)
  const [listError, setListError] = useState('')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<WebApiEvidenceDetail | null>(null)
  const [flow, setFlow] = useState<TrafficFlow | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState('')
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState('')
  // 筛选条：搜索（前端过滤当前已加载数据）+ 状态分段。
  const [query, setQuery] = useState('')
  const [segment, setSegment] = useState<TrafficSegment>('all')
  // 来源：webapi_http（平台工具记录，默认）与终端（B2 代理出口记录）。
  const [source, setSource] = useState<'http' | 'terminal'>('http')
  // 终端来源单独一套列表/详情状态，与 webapi_http 互不覆盖。
  const [egressItems, setEgressItems] = useState<WebApiEgressItem[]>([])
  const [egressHasMore, setEgressHasMore] = useState(false)
  const [egressNextCursor, setEgressNextCursor] = useState<string | null>(null)
  const [egressLoading, setEgressLoading] = useState(false)
  const [egressListError, setEgressListError] = useState('')
  const [egressSegment, setEgressSegment] = useState<EgressSegment>('all')
  const [egressSelectedId, setEgressSelectedId] = useState<string | null>(null)
  const [egressDetail, setEgressDetail] = useState<WebApiEgressDetail | null>(null)
  const [egressFlow, setEgressFlow] = useState<TrafficFlow | null>(null)
  const [egressDetailLoading, setEgressDetailLoading] = useState(false)
  const [egressDetailError, setEgressDetailError] = useState('')
  const listSeq = useRef(0)
  const egressListSeq = useRef(0)
  const detailSeq = useRef(0)
  const egressDetailSeq = useRef(0)
  const copySeq = useRef(0)

  const view = useMemo(() => (flow ? aopFlowToHttpView(flow) : null), [flow])
  const egressView = useMemo(() => (egressFlow ? aopFlowToHttpView(egressFlow) : null), [egressFlow])

  const loadList = useCallback(async (cursor?: string) => {
    const seq = ++listSeq.current
    setLoadingList(true)
    setListError('')
    try {
      const result = await listWebApiEvidence(task.id, 100, cursor)
      if (seq !== listSeq.current) return
      setItems((current) => (cursor ? [...current, ...result.items] : result.items))
      setHasMore(result.has_more)
      setNextCursor(result.next_cursor)
    } catch (err) {
      if (seq !== listSeq.current) return
      setListError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === listSeq.current) setLoadingList(false)
    }
  }, [task.id])

  const loadEgressList = useCallback(async (cursor?: string) => {
    const seq = ++egressListSeq.current
    setEgressLoading(true)
    setEgressListError('')
    try {
      const result = await listWebApiEgress(task.id, 100, cursor)
      if (seq !== egressListSeq.current) return
      setEgressItems((current) => (cursor ? [...current, ...result.items] : result.items))
      setEgressHasMore(result.has_more)
      setEgressNextCursor(result.next_cursor)
    } catch (err) {
      if (seq !== egressListSeq.current) return
      setEgressListError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === egressListSeq.current) setEgressLoading(false)
    }
  }, [task.id])

  // 换任务即清空旧原件与选择（含筛选态），并使 list/detail/copy seq 全失效；卸载同样失效。
  useEffect(() => {
    listSeq.current++
    egressListSeq.current++
    detailSeq.current++
    egressDetailSeq.current++
    copySeq.current++
    setItems([])
    setHasMore(false)
    setNextCursor(null)
    setSelectedId(null)
    setDetail(null)
    setFlow(null)
    setDetailError('')
    setCopied(false)
    setCopyError('')
    setQuery('')
    setSegment('all')
    setSource('http')
    setEgressItems([])
    setEgressHasMore(false)
    setEgressNextCursor(null)
    setEgressSelectedId(null)
    setEgressDetail(null)
    setEgressFlow(null)
    setEgressDetailError('')
    setEgressSegment('all')
    void loadList()
  }, [loadList])

  useEffect(() => () => {
    listSeq.current++
    egressListSeq.current++
    detailSeq.current++
    egressDetailSeq.current++
    copySeq.current++
  }, [])

  const loadDetail = useCallback(async (evidenceId: string) => {
    const seq = ++detailSeq.current
    copySeq.current++
    setDetailLoading(true)
    setDetailError('')
    setDetail(null)
    setFlow(null)
    setCopied(false)
    setCopyError('')
    try {
      const result = await getWebApiEvidence(task.id, evidenceId)
      if (seq !== detailSeq.current) return
      const decoded = decodeFlow(result.flow_protobuf_base64)
      if (seq !== detailSeq.current) return
      setDetail(result)
      setFlow(decoded)
    } catch (err) {
      if (seq !== detailSeq.current) return
      setDetail(null)
      setFlow(null)
      setDetailError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === detailSeq.current) setDetailLoading(false)
    }
  }, [task.id])

  const select = useCallback((item: WebApiEvidence) => {
    setSelectedId(item.evidence_id)
    void loadDetail(item.evidence_id)
  }, [loadDetail])

  const loadEgressDetail = useCallback(async (flowId: string) => {
    const seq = ++egressDetailSeq.current
    copySeq.current++
    setEgressDetailLoading(true)
    setEgressDetailError('')
    setEgressDetail(null)
    setEgressFlow(null)
    setCopied(false)
    setCopyError('')
    try {
      const result = await getWebApiEgress(task.id, flowId)
      if (seq !== egressDetailSeq.current) return
      const decoded = decodeFlow(result.flow_protobuf_base64)
      if (seq !== egressDetailSeq.current) return
      setEgressDetail(result)
      setEgressFlow(decoded)
    } catch (err) {
      if (seq !== egressDetailSeq.current) return
      setEgressDetail(null)
      setEgressFlow(null)
      setEgressDetailError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === egressDetailSeq.current) setEgressDetailLoading(false)
    }
  }, [task.id])

  const selectEgress = useCallback((item: WebApiEgressItem) => {
    setEgressSelectedId(item.id)
    void loadEgressDetail(item.id)
  }, [loadEgressDetail])

  // 切换来源：清空选中与详情（两个来源的详情都不保留），筛选回到全部；
  // 切到终端时重新拉取最新出口记录，切回 webapi_http 沿用已加载数据（行为不变）。
  const switchSource = useCallback((next: 'http' | 'terminal') => {
    if (next === source) return
    setSource(next)
    detailSeq.current++
    egressDetailSeq.current++
    copySeq.current++
    setSelectedId(null)
    setDetail(null)
    setFlow(null)
    setDetailError('')
    setDetailLoading(false)
    setEgressSelectedId(null)
    setEgressDetail(null)
    setEgressFlow(null)
    setEgressDetailError('')
    setEgressDetailLoading(false)
    setCopied(false)
    setCopyError('')
    setQuery('')
    setSegment('all')
    setEgressSegment('all')
    if (next === 'terminal') {
      setEgressItems([])
      setEgressHasMore(false)
      setEgressNextCursor(null)
      void loadEgressList()
    }
  }, [source, loadEgressList])

  // 「在流量中查看」定位：按 evidence_id 直接选中/加载详情，不在首页 100 条内也能到达；
  // 列表分页后仍以 ID 请求详情，找不到时 loadDetail 就地显示错误。404 不冒称已定位。
  useEffect(() => {
    if (!focusEvidenceId) return
    // 定位目标是 webapi_http 证据：若当前在终端来源，切回 webapi_http 再选中。
    setSource('http')
    egressDetailSeq.current++
    setEgressSelectedId(null)
    setEgressDetail(null)
    setEgressFlow(null)
    setEgressDetailError('')
    setEgressDetailLoading(false)
    setSelectedId(focusEvidenceId)
    void loadDetail(focusEvidenceId)
    onFocusConsumed?.()
  }, [focusEvidenceId, loadDetail, onFocusConsumed])

  const refresh = useCallback(() => {
    if (source === 'terminal') {
      egressListSeq.current++
      egressDetailSeq.current++
      copySeq.current++
      setEgressItems([])
      setEgressHasMore(false)
      setEgressNextCursor(null)
      setEgressSelectedId(null)
      setEgressDetail(null)
      setEgressFlow(null)
      setEgressDetailError('')
      setCopied(false)
      setCopyError('')
      void loadEgressList()
      return
    }
    listSeq.current++
    detailSeq.current++
    copySeq.current++
    setItems([])
    setHasMore(false)
    setNextCursor(null)
    setSelectedId(null)
    setDetail(null)
    setFlow(null)
    setDetailError('')
    setCopied(false)
    setCopyError('')
    void loadList()
  }, [loadList, loadEgressList, source])

  const copy = useCallback(async () => {
    if (!flow || !detail) return
    const text = viewToRawText(aopFlowToHttpView(flow))
    const seq = ++copySeq.current
    try {
      const ok = await writeClipboard(text)
      if (seq !== copySeq.current) return
      if (ok) {
        setCopied(true)
        setCopyError('')
        setTimeout(() => { if (seq === copySeq.current) setCopied(false) }, 2000)
      } else {
        setCopied(false)
        setCopyError(t('webapi.copyFailed'))
      }
    } catch {
      if (seq === copySeq.current) { setCopied(false); setCopyError(t('webapi.copyFailed')) }
    }
  }, [flow, detail, t])

  const copyEgress = useCallback(async () => {
    if (!egressFlow || !egressDetail) return
    const text = viewToRawText(aopFlowToHttpView(egressFlow))
    const seq = ++copySeq.current
    try {
      const ok = await writeClipboard(text)
      if (seq !== copySeq.current) return
      if (ok) {
        setCopied(true)
        setCopyError('')
        setTimeout(() => { if (seq === copySeq.current) setCopied(false) }, 2000)
      } else {
        setCopied(false)
        setCopyError(t('webapi.copyFailed'))
      }
    } catch {
      if (seq === copySeq.current) { setCopied(false); setCopyError(t('webapi.copyFailed')) }
    }
  }, [egressFlow, egressDetail, t])

  const requestBody = flow?.request?.body
  const responseBody = flow?.response?.body
  const egressRequestBody = egressFlow?.request?.body
  const egressResponseBody = egressFlow?.response?.body

  // 前端过滤：URL 匹配 + 状态分段，只作用于当前已加载数据。
  const queryTrim = query.trim().toLowerCase()
  const filtered = useMemo(() => {
    if (queryTrim === '' && segment === 'all') return items
    return items.filter((item) => {
      if (queryTrim !== '' && !item.url.toLowerCase().includes(queryTrim)) return false
      if (segment !== 'all' && segmentOf(item) !== segment) return false
      return true
    })
  }, [items, queryTrim, segment])

  const segments: { key: TrafficSegment; label: string }[] = [
    { key: 'all', label: t('webapi.trafficFilterAll') },
    { key: '2xx', label: '2xx' },
    { key: '3xx', label: '3xx' },
    { key: '4xx', label: '4xx' },
    { key: '5xx', label: '5xx' },
    { key: 'incomplete', label: t('webapi.trafficIncomplete') },
  ]

  // 终端来源：同样只过滤已加载数据，按放行/拒绝分段与 method/url 搜索。
  const egressFiltered = useMemo(() => {
    if (queryTrim === '' && egressSegment === 'all') return egressItems
    return egressItems.filter((item) => {
      if (queryTrim !== '' && !`${item.method} ${item.url}`.toLowerCase().includes(queryTrim)) return false
      if (egressSegment !== 'all' && item.decision !== egressSegment) return false
      return true
    })
  }, [egressItems, queryTrim, egressSegment])

  const egressSegments: { key: EgressSegment; label: string }[] = [
    { key: 'all', label: t('webapi.trafficFilterAll') },
    { key: 'allowed', label: t('webapi.egressAllowed') },
    { key: 'denied', label: t('webapi.egressDenied') },
  ]

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <PanelGroup direction="vertical" className="min-h-0 flex-1">
        {/* 上：筛选条 + 流量表（默认 45% 高，可拖动分割，最小 160px）。 */}
        <Panel defaultSize={45} minSize={15} className="flex min-h-0 flex-col">
          <div className="flex min-h-[160px] min-w-0 flex-1 flex-col overflow-hidden">
            <div className="flex h-10 shrink-0 items-center gap-2 border-b border-border/60 px-3">
              <div role="group" aria-label={t('webapi.egressSource')} className="flex shrink-0 items-center gap-0.5 rounded-md border border-border/60 p-0.5">
                <button
                  type="button"
                  aria-pressed={source === 'http'}
                  onClick={() => switchSource('http')}
                  className={cn(
                    'h-6 rounded px-1.5 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                    source === 'http' ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                  )}
                >
                  {t('webapi.egressSourceHttp')}
                </button>
                <button
                  type="button"
                  aria-pressed={source === 'terminal'}
                  onClick={() => switchSource('terminal')}
                  className={cn(
                    'h-6 rounded px-1.5 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                    source === 'terminal' ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                  )}
                >
                  {t('webapi.egressSourceTerminal')}
                </button>
              </div>
              <div className="relative h-7 w-full min-w-0 max-w-[260px]">
                <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <input
                  type="text"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder={t('webapi.trafficSearchPlaceholder')}
                  aria-label={t('webapi.trafficSearchPlaceholder')}
                  className="h-7 w-full rounded-md border border-border/60 bg-background pl-7 pr-2 text-[13px] text-foreground placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                />
              </div>
              {source === 'http' ? (
                <div role="group" aria-label={t('webapi.result')} className="flex shrink-0 items-center gap-0.5 rounded-md border border-border/60 p-0.5">
                  {segments.map((seg) => (
                    <button
                      key={seg.key}
                      type="button"
                      aria-pressed={segment === seg.key}
                      onClick={() => setSegment(seg.key)}
                      className={cn(
                        'h-6 rounded px-1.5 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                        segment === seg.key ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                      )}
                    >
                      {seg.label}
                    </button>
                  ))}
                </div>
              ) : (
                <div role="group" aria-label={t('webapi.result')} className="flex shrink-0 items-center gap-0.5 rounded-md border border-border/60 p-0.5">
                  {egressSegments.map((seg) => (
                    <button
                      key={seg.key}
                      type="button"
                      aria-pressed={egressSegment === seg.key}
                      onClick={() => setEgressSegment(seg.key)}
                      className={cn(
                        'h-6 rounded px-1.5 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                        egressSegment === seg.key ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                      )}
                    >
                      {seg.label}
                    </button>
                  ))}
                </div>
              )}
              <span className="ml-auto flex shrink-0 items-center gap-1">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      aria-label={t('webapi.httpRecordsHint')}
                      className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground"
                    >
                      <Info className="h-3.5 w-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">{t('webapi.httpRecordsHint')}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onClick={refresh}
                      disabled={source === 'terminal' ? egressLoading : loadingList}
                      aria-label={t('webapi.refresh')}
                      className="flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground disabled:opacity-50"
                    >
                      {(source === 'terminal' ? egressLoading : loadingList) ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                    </button>
                  </TooltipTrigger>
                  <TooltipContent>{t('webapi.refresh')}</TooltipContent>
                </Tooltip>
              </span>
            </div>
            <div className="min-h-0 flex-1 overflow-auto">
              {source === 'http' ? (
                loadingList && items.length === 0 ? (
                  <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
                ) : listError ? (
                  <div className="px-3 py-4 text-center text-sm text-destructive">{listError}</div>
                ) : items.length === 0 ? (
                  <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.evidenceEmpty')}</div>
                ) : filtered.length === 0 ? (
                  <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.trafficNoMatch')}</div>
                ) : (
                  <table className="w-full border-separate border-spacing-0 text-left">
                    <thead className="sticky top-0 z-10">
                      <tr>
                        <th scope="col" className="w-[76px] border-b border-border/60 bg-background px-3 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.time')}</th>
                        <th scope="col" className="w-[64px] border-b border-border/60 bg-background px-1 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.method')}</th>
                        <th scope="col" className="w-[190px] border-b border-border/60 bg-background px-1 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.trafficHost')}</th>
                        <th scope="col" className="border-b border-border/60 bg-background px-1 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.trafficPath')}</th>
                        <th scope="col" className="w-[110px] border-b border-border/60 bg-background px-3 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.result')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filtered.map((item) => {
                        const selected = selectedId === item.evidence_id
                        const { host, path } = splitUrl(item.url)
                        const completed = item.status === 'completed' && item.http_status != null
                        return (
                          <tr
                            key={item.evidence_id}
                            tabIndex={0}
                            onClick={() => select(item)}
                            onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); select(item) } }}
                            aria-selected={selected}
                            className={cn(
                              'h-[30px] cursor-pointer transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary/40',
                              selected && 'bg-primary/10 shadow-[inset_2px_0_0_hsl(var(--primary))]',
                            )}
                          >
                            <td className="border-b border-border/40 px-3 font-mono text-[12px] tabular-nums text-muted-foreground" title={new Date(item.created_at).toLocaleString()}>
                              {formatClock(item.created_at)}
                            </td>
                            <td className={cn('border-b border-border/40 px-1 font-mono text-[12px] font-semibold', methodTone(item.method))}>
                              {item.method || '-'}
                            </td>
                            <td className="max-w-0 truncate border-b border-border/40 px-1 font-mono text-[12px] text-foreground" title={host}>
                              {host || '—'}
                            </td>
                            <td className="max-w-0 truncate border-b border-border/40 px-1 font-mono text-[12px] text-foreground" title={item.url}>
                              {path}
                            </td>
                            <td className="border-b border-border/40 px-3">
                              {completed ? (
                                <span className={cn('font-mono text-[12px] tabular-nums', httpStatusTone(item.http_status!))}>{item.http_status}</span>
                              ) : (
                                <span className={cn('text-[12px]', statusTone(item.status))}>{statusLabel(t, item.status)}</span>
                              )}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                )
              ) : (
                egressLoading && egressItems.length === 0 ? (
                  <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
                ) : egressListError ? (
                  <div className="px-3 py-4 text-center text-sm text-destructive">{egressListError}</div>
                ) : egressItems.length === 0 ? (
                  <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.egressEmpty')}</div>
                ) : egressFiltered.length === 0 ? (
                  <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.trafficNoMatch')}</div>
                ) : (
                  <table className="w-full border-separate border-spacing-0 text-left">
                    <thead className="sticky top-0 z-10">
                      <tr>
                        <th scope="col" className="w-[76px] border-b border-border/60 bg-background px-3 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.time')}</th>
                        <th scope="col" className="w-[64px] border-b border-border/60 bg-background px-1 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.method')}</th>
                        <th scope="col" className="w-[190px] border-b border-border/60 bg-background px-1 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.trafficHost')}</th>
                        <th scope="col" className="border-b border-border/60 bg-background px-1 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.trafficPath')}</th>
                        <th scope="col" className="w-[110px] border-b border-border/60 bg-background px-3 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.result')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {egressFiltered.map((item) => {
                        const selected = egressSelectedId === item.id
                        const { host, path } = splitEgressUrl(item.url)
                        return (
                          <tr
                            key={item.id}
                            tabIndex={0}
                            onClick={() => selectEgress(item)}
                            onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); selectEgress(item) } }}
                            aria-selected={selected}
                            className={cn(
                              'h-[30px] cursor-pointer transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary/40',
                              selected && 'bg-primary/10 shadow-[inset_2px_0_0_hsl(var(--primary))]',
                            )}
                          >
                            <td className="border-b border-border/40 px-3 font-mono text-[12px] tabular-nums text-muted-foreground" title={new Date(item.created_at).toLocaleString()}>
                              {formatClock(item.created_at)}
                            </td>
                            <td className={cn('border-b border-border/40 px-1 font-mono text-[12px] font-semibold', methodTone(item.method))}>
                              {item.method || '-'}
                            </td>
                            <td className="max-w-0 truncate border-b border-border/40 px-1 font-mono text-[12px] text-foreground" title={host}>
                              {host || '—'}
                            </td>
                            <td className="max-w-0 truncate border-b border-border/40 px-1 font-mono text-[12px] text-foreground" title={item.url}>
                              {path}
                            </td>
                            <td className="border-b border-border/40 px-3">
                              {item.decision === 'denied' ? (
                                <span className="text-[12px] font-medium text-destructive">{t('webapi.egressIntercepted')}</span>
                              ) : item.http_status != null ? (
                                <span className={cn('font-mono text-[12px] tabular-nums', httpStatusTone(item.http_status))}>{item.http_status}</span>
                              ) : (
                                <span className="text-[12px] text-muted-foreground">{t('webapi.egressNoResponse')}</span>
                              )}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>
                )
              )}
            </div>
            {source === 'http' ? hasMore && (
              <div className="shrink-0 border-t border-border/50 px-3 py-1.5 text-center">
                <Button type="button" variant="ghost" size="xs" onClick={() => void loadList(nextCursor ?? undefined)} disabled={loadingList} className="text-[13px] text-muted-foreground">
                  {loadingList ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('webapi.loadMore')}
                </Button>
              </div>
            ) : egressHasMore && (
              <div className="shrink-0 border-t border-border/50 px-3 py-1.5 text-center">
                <Button type="button" variant="ghost" size="xs" onClick={() => void loadEgressList(egressNextCursor ?? undefined)} disabled={egressLoading} className="text-[13px] text-muted-foreground">
                  {egressLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('webapi.loadMore')}
                </Button>
              </div>
            )}
          </div>
        </Panel>

        <PanelResizeHandle className="relative flex h-1.5 w-full items-center justify-center bg-background transition-colors after:absolute after:inset-x-0 after:inset-y-[-3px] after:content-[''] hover:bg-primary/20 data-[resize-handle-state=drag]:bg-primary/30">
          <span aria-hidden className="h-0.5 w-10 rounded-full bg-border" />
        </PanelResizeHandle>

        {/* 下：请求/响应双栏（原生 HttpViewPanels，保持不变）。 */}
        <Panel minSize={25} className="flex min-h-0 flex-col">
          <div className="flex min-h-0 flex-1 flex-col">
            {source === 'http' ? (
            selectedId == null ? (
              <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">{t('webapi.selectEvidence')}</div>
            ) : detailLoading ? (
              <div className="flex flex-1 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
            ) : detailError ? (
              <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-destructive">
                <span>{t('webapi.evidenceLoadFailed')}</span>
                <span className="text-xs text-muted-foreground">{detailError}</span>
              </div>
            ) : (
              <>
                <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/60 px-3 py-1.5 text-xs">
                  <span className={cn('font-medium', statusTone(detail?.metadata.status || 'unknown'))}>{statusLabel(t, detail?.metadata.status || 'unknown')}</span>
                  {detail?.metadata.http_status != null && <span className="font-mono">{detail.metadata.http_status}</span>}
                  {detail?.metadata.sent === false && <span className="text-muted-foreground">{t('webapi.notSent')}</span>}
                  {detail?.metadata.sent === true && <span className="text-muted-foreground">{t('webapi.sent')}</span>}
                  {detail?.metadata.reason ? <span className="min-w-0 truncate text-muted-foreground" title={detail.metadata.reason}>{detail.metadata.reason}</span> : null}
                  {detail?.capture.capture_truncated && <span className="text-warning">{t('webapi.captureTruncated')}</span>}
                  <span className="text-muted-foreground">{t('webapi.reconstructionNote')}</span>
                  <span className="ml-auto" />
                  {requestBody?.length ? (
                    <Button type="button" variant="ghost" size="xs" onClick={() => downloadBytes(requestBody, `${detail?.metadata.evidence_id || 'request'}.request.body`)} className="text-muted-foreground">
                      {t('webapi.downloadRequestBody')}
                    </Button>
                  ) : null}
                  {responseBody?.length ? (
                    <Button type="button" variant="ghost" size="xs" onClick={() => downloadBytes(responseBody, `${detail?.metadata.evidence_id || 'response'}.response.body`)} className="text-muted-foreground">
                      {t('webapi.downloadResponseBody')}
                    </Button>
                  ) : null}
                  <Button type="button" variant="ghost" size="xs" onClick={copy} className="gap-1 text-muted-foreground">
                    {copied ? t('webapi.copied') : copyError ? t('webapi.copyFailed') : t('webapi.copyDisplay')}
                  </Button>
                </div>
                <div className="min-h-0 min-w-0 flex-1">
                  <HttpViewPanels
                    view={view}
                    requestTitle={t('webapi.request')}
                    responseTitle={t('webapi.response')}
                    emptyText={t('webapi.selectEvidence')}
                    labels={{
                      loadFailed: t('webapi.loadFailed'),
                      requestError: t('webapi.requestError'),
                      noResponse: t('webapi.noResponse'),
                      emptyBody: t('webapi.emptyBody'),
                    }}
                  />
                </div>
              </>
            )
            ) : (
            egressSelectedId == null ? (
              <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">{t('webapi.selectEvidence')}</div>
            ) : egressDetailLoading ? (
              <div className="flex flex-1 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
            ) : egressDetailError ? (
              <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-destructive">
                <span>{t('webapi.evidenceLoadFailed')}</span>
                <span className="text-xs text-muted-foreground">{egressDetailError}</span>
              </div>
            ) : (
              <>
                <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/60 px-3 py-1.5 text-xs">
                  {egressDetail?.summary.decision === 'denied' && egressDetail.summary.error ? (
                    <span className="min-w-0 truncate text-destructive" title={egressDetail.summary.error}>{egressDetail.summary.error}</span>
                  ) : null}
                  {egressDetail?.capture.preview_truncated ? (
                    <span className="text-amber-600 dark:text-amber-400">
                      {t('webapi.egressPreviewTruncated', {
                        limit: egressDetail.capture.preview_limit,
                        req: egressDetail.capture.request_bytes,
                        resp: egressDetail.capture.response_bytes,
                      })}
                    </span>
                  ) : null}
                  <span className="ml-auto" />
                  {egressRequestBody?.length ? (
                    <Button type="button" variant="ghost" size="xs" onClick={() => downloadBytes(egressRequestBody, `${egressDetail?.summary.id || 'request'}.request.body`)} className="text-muted-foreground">
                      {t('webapi.downloadRequestBody')}
                    </Button>
                  ) : null}
                  {egressResponseBody?.length ? (
                    <Button type="button" variant="ghost" size="xs" onClick={() => downloadBytes(egressResponseBody, `${egressDetail?.summary.id || 'response'}.response.body`)} className="text-muted-foreground">
                      {t('webapi.downloadResponseBody')}
                    </Button>
                  ) : null}
                  <Button type="button" variant="ghost" size="xs" onClick={copyEgress} className="gap-1 text-muted-foreground">
                    {copied ? t('webapi.copied') : copyError ? t('webapi.copyFailed') : t('webapi.copyDisplay')}
                  </Button>
                </div>
                <div className="min-h-0 min-w-0 flex-1">
                  <HttpViewPanels
                    view={egressView}
                    requestTitle={t('webapi.request')}
                    responseTitle={t('webapi.response')}
                    emptyText={t('webapi.selectEvidence')}
                    labels={{
                      loadFailed: t('webapi.loadFailed'),
                      requestError: t('webapi.requestError'),
                      noResponse: t('webapi.noResponse'),
                      emptyBody: t('webapi.emptyBody'),
                    }}
                  />
                </div>
              </>
            )
            )}
          </div>
        </Panel>
      </PanelGroup>
    </div>
  )
}
