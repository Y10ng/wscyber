import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, RefreshCw } from 'lucide-react'
import { fromBinary } from '@bufbuild/protobuf'
import { TrafficFlowSchema, type TrafficFlow } from '@cyber/aop'
import { aopFlowToHttpView, HttpViewPanels, type TrafficHttpView } from '@cyber/traffic'
import { cn } from '@cyber/theme'
import { Button } from '@cyber/ui'
import { getWebApiEvidence, listWebApiEvidence, type WebApiEvidence, type WebApiEvidenceDetail, type WebApiTask } from '../api'

interface Props {
  task: WebApiTask
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
  unknown: 'webapi.status_unknown',
  capture_failed: 'webapi.status_capture_failed',
}

function statusLabel(t: (key: string) => string, status: string): string {
  return t(STATUS_LABEL_KEYS[status] || 'webapi.status_unknown')
}

function statusTone(status: string): string {
  if (status === 'completed') return 'text-emerald-600 dark:text-emerald-400'
  if (status === 'denied' || status === 'capture_failed') return 'text-red-600 dark:text-red-400'
  if (status === 'review_required') return 'text-amber-600 dark:text-amber-400'
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

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(undefined, { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export default function WebApiEvidencePanel({ task }: Props) {
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
  const listSeq = useRef(0)
  const detailSeq = useRef(0)
  const copySeq = useRef(0)

  const view = useMemo(() => (flow ? aopFlowToHttpView(flow) : null), [flow])

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

  // 换任务即清空旧原件与选择，并使 list/detail/copy seq 全失效；卸载同样失效。
  useEffect(() => {
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
  }, [loadList])

  useEffect(() => () => { listSeq.current++; detailSeq.current++; copySeq.current++ }, [])

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

  const refresh = useCallback(() => {
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
  }, [loadList])

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

  const requestBody = flow?.request?.body
  const responseBody = flow?.response?.body

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      {/* 上方紧凑流量表 */}
      <div className="flex min-h-0 shrink-0 flex-col border-b border-border/60" style={{ maxHeight: '42%' }}>
        <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border/60 px-3 py-1.5">
          <span className="flex min-w-0 items-center gap-2">
            <span className="text-[13px] font-medium text-foreground/75">{t('webapi.httpRecordsTitle')}</span>
            <span className="truncate text-[11px] text-muted-foreground">{t('webapi.httpRecordsHint')}</span>
          </span>
          <Button type="button" variant="ghost" size="xs" onClick={refresh} disabled={loadingList} className="gap-1 text-muted-foreground">
            <RefreshCw className="h-3.5 w-3.5" />
            {t('webapi.refresh')}
          </Button>
        </div>
        <div className="flex shrink-0 items-center gap-2 border-b border-border/50 px-3 py-1 text-xs font-medium text-muted-foreground">
          <span className="w-14 shrink-0">{t('webapi.method')}</span>
          <span className="min-w-0 flex-1">{t('webapi.url')}</span>
          <span className="w-28 shrink-0">{t('webapi.result')}</span>
          <span className="w-20 shrink-0 text-right">{t('webapi.time')}</span>
        </div>
        <div className="min-h-0 flex-1 overflow-auto">
          {loadingList && items.length === 0 ? (
            <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : listError ? (
            <div className="px-3 py-4 text-center text-sm text-destructive">{listError}</div>
          ) : items.length === 0 ? (
            <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.evidenceEmpty')}</div>
          ) : (
            <div className="divide-y divide-border/40">
              {items.map((item) => (
                <button
                  key={item.evidence_id}
                  type="button"
                  onClick={() => select(item)}
                  aria-pressed={selectedId === item.evidence_id}
                  className={cn('flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40', selectedId === item.evidence_id && 'bg-primary/10')}
                >
                  <span className={cn('w-14 shrink-0 font-mono text-[12px] font-semibold', methodTone(item.method))}>{item.method || '-'}</span>
                  <span className="min-w-0 flex-1 truncate font-mono text-[13px]" title={item.url}>{item.url}</span>
                  <span className="w-28 shrink-0">
                    <span className={cn('text-[13px] font-medium', statusTone(item.status))}>{statusLabel(t, item.status)}</span>
                    {item.http_status != null && <span className="ml-1 font-mono text-[12px] text-muted-foreground">{item.http_status}</span>}
                    {item.sent === false && <span className="ml-1 text-[11px] text-muted-foreground">{t('webapi.notSent')}</span>}
                  </span>
                  <span className="w-20 shrink-0 text-right text-[11px] text-muted-foreground">{formatTime(item.created_at)}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        {hasMore && (
          <div className="shrink-0 border-t border-border/50 px-3 py-1.5 text-center">
            <Button type="button" variant="ghost" size="xs" onClick={() => void loadList(nextCursor ?? undefined)} disabled={loadingList} className="text-muted-foreground">
              {loadingList ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('webapi.loadMore')}
            </Button>
          </div>
        )}
      </div>

      {/* 下方请求/响应双栏 */}
      <div className="flex min-h-0 flex-1 flex-col">
        {selectedId == null ? (
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
        )}
      </div>
    </div>
  )
}
