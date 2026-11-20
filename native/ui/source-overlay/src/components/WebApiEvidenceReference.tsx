import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, Image as ImageIcon, Loader2 } from 'lucide-react'
import { fromBinary } from '@bufbuild/protobuf'
import { TrafficFlowSchema, type TrafficFlow } from '@cyber/aop'
import type { TrafficHttpView } from '@cyber/traffic'
import { cn } from '@cyber/theme'
import { Button } from '@cyber/ui'
import { webApiEvidenceImageURL, type WebApiEvidenceDetail, type WebApiFindingEvidenceSnapshot } from '../api'
import { formatShortDateTime } from '../lib/time'
import HttpPairBlock from './HttpPairBlock'
import WebApiEvidenceImageDialog from './WebApiEvidenceImageDialog'

// 把后端持久化的 Flow protobuf 原件解码为 TrafficFlow，再交给共享 HttpPairBlock
// 渲染；与 WebApiEvidencePanel 同一解码路径，不把替换字符当原件、不重放。
export function decodeFlow(base64: string): TrafficFlow {
  const bytes = Uint8Array.from(atob(base64), (char) => char.charCodeAt(0))
  return fromBinary(TrafficFlowSchema, bytes)
}

export function formatTime(value: string): string {
  return formatShortDateTime(value)
}

export function relationLabelKey(relation: string): string {
  if (relation === 'counterevidence') return 'webapi.relation_counterevidence'
  if (relation === 'limitation') return 'webapi.relation_limitation'
  return 'webapi.relation_supporting'
}

export function statusTone(status: string): string {
  if (status === 'completed') return 'text-emerald-600 dark:text-emerald-400'
  if (status === 'denied' || status === 'capture_failed') return 'text-red-600 dark:text-red-400'
  if (status === 'review_required') return 'text-amber-600 dark:text-amber-400'
  if (status === 'unknown') return 'text-orange-600 dark:text-orange-400'
  if (status === 'claimed') return 'text-blue-600 dark:text-blue-400'
  return 'text-muted-foreground'
}

export function methodTone(method: string): string {
  if (method === 'GET') return 'text-emerald-600 dark:text-emerald-400'
  if (method === 'POST') return 'text-blue-600 dark:text-blue-400'
  if (method === 'PUT' || method === 'PATCH') return 'text-amber-600 dark:text-amber-400'
  if (method === 'DELETE') return 'text-red-600 dark:text-red-400'
  return 'text-muted-foreground'
}

export const STATUS_KEYS: Record<string, string> = {
  claimed: 'webapi.status_claimed',
  completed: 'webapi.status_completed',
  denied: 'webapi.status_denied',
  review_required: 'webapi.status_review_required',
  unknown: 'webapi.status_unknown',
  capture_failed: 'webapi.status_capture_failed',
}

// URL 与任一授权 origin 同源（scheme+host+port 一致）→ 只显示路径；否则返回 null，
// 由调用方按非授权目标显示完整 URL。
function sameOriginPath(url: string, origins: string[]): string | null {
  let parsed: URL
  try { parsed = new URL(url) } catch { return null }
  for (const origin of origins) {
    try {
      const o = new URL(origin)
      if (o.protocol === parsed.protocol && o.host === parsed.host) {
        return `${parsed.pathname}${parsed.search}${parsed.hash}` || '/'
      }
    } catch { continue }
  }
  return null
}

// 复用的证据引用条目：元数据摘要 + 按需展开共享 HTTP 并排代码块。它只负责展示，
// 取数/解码/展开态由调用方维护，便于候选证据与独立验证新证据共用同一渲染器。
// origins：任务授权 origin 列表；与授权同源的 URL 只显示路径（title 保留完整 URL），
// 非授权目标显示完整 URL 并允许换行（fix-2 #3）。
export function EvidenceReference({
  index,
  item,
  expanded,
  loading,
  error,
  view,
  detail,
  onToggle,
  registerEvidenceRef,
  origins,
  taskId,
}: {
  index: number
  item: WebApiFindingEvidenceSnapshot
  expanded: boolean
  loading: boolean
  error: string
  view: TrafficHttpView | null
  detail: WebApiEvidenceDetail | null
  onToggle: () => void
  registerEvidenceRef: (evidenceId: string, el: HTMLButtonElement | null) => void
  origins?: string[]
  /** 任务 id：缺失时不显示“图片”入口（证据图片接口按任务鉴权）。 */
  taskId?: string
}) {
  const { t } = useTranslation('chat')
  const [imageOpen, setImageOpen] = useState(false)
  const inScopePath = origins && origins.length > 0 ? sameOriginPath(item.url, origins) : null
  return (
    <li className="rounded-md border border-border/60">
      <button
        ref={(el) => registerEvidenceRef(item.evidence_id, el)}
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        {expanded ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
        <span className="w-6 shrink-0 font-mono text-[12px] tabular-nums text-muted-foreground">{index + 1}</span>
        <span className="shrink-0 text-[12px] text-muted-foreground">{t(relationLabelKey(item.relation))}</span>
        <span className={cn('shrink-0 font-mono text-[12px] font-semibold', methodTone(item.method))}>{item.method || '-'}</span>
        {inScopePath !== null ? (
          <span className="min-w-0 flex-1 truncate font-mono text-[12px]" title={item.url}>{inScopePath}</span>
        ) : (
          <span className="min-w-0 flex-1 break-all font-mono text-[12px]" title={item.url}>{item.url}</span>
        )}
        {item.http_status != null && <span className="shrink-0 font-mono text-[12px] text-muted-foreground">{item.http_status}</span>}
      </button>
      {expanded && (
        <div className="border-t border-border/50">
          {loading ? (
            <div className="flex h-40 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : error ? (
            <div className="px-3 py-3 text-sm text-destructive">{t('webapi.evidenceLoadFailed')}：{error}</div>
          ) : view ? (
            <>
              <div className="flex flex-wrap items-center gap-2 border-b border-border/50 px-2.5 py-1.5 text-xs">
                <span className={cn('font-medium', statusTone(detail?.metadata.status || 'unknown'))}>{t(detail?.metadata.status ? (STATUS_KEYS[detail.metadata.status] || 'webapi.status_unknown') : 'webapi.status_unknown')}</span>
                {detail?.metadata.sent === false && <span className="text-muted-foreground">{t('webapi.notSent')}</span>}
                {detail?.metadata.reason ? <span className="min-w-0 truncate text-muted-foreground" title={detail.metadata.reason}>{detail.metadata.reason}</span> : null}
                {detail?.capture.capture_truncated && <span className="text-warning">{t('webapi.captureTruncated')}</span>}
                <span className="text-muted-foreground">{t('webapi.reconstructionNote')}</span>
                {taskId ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    className="ml-auto gap-1 text-muted-foreground"
                    onClick={() => setImageOpen(true)}
                  >
                    <ImageIcon className="h-3.5 w-3.5" />
                    {t('webapi.evidenceImage')}
                  </Button>
                ) : null}
              </div>
              <div className="min-w-0 p-2.5">
                <HttpPairBlock
                  view={view}
                  loading={false}
                  error=""
                  retry={() => {}}
                  requestMarks={[]}
                  responseMarks={[]}
                  masked
                  foldHeaders
                  stacked
                />
              </div>
            </>
          ) : null}
        </div>
      )}
      {taskId ? (
        <WebApiEvidenceImageDialog
          open={imageOpen}
          onClose={() => setImageOpen(false)}
          url={webApiEvidenceImageURL(taskId, item.evidence_id)}
          filename={`evidence-${item.evidence_id}.png`}
        />
      ) : null}
    </li>
  )
}
