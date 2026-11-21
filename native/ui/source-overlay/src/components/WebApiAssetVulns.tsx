import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState, Spinner } from '@cyber/ui'
import { cn } from '@cyber/theme'
import {
  listWebApiAssetVulns,
  type WebApiVuln,
  type WebApiVulnList,
  type WebApiVulnSeverity,
  type WebApiVulnStatus,
} from '../api'
import { FilterPills, type FilterPillItem } from './PageFrame'
import { formatShortDateTime } from '../lib/time'
import { DispositionBadge, SeverityBadge, StatusBadge } from './ui-badges'

interface Props {
  assetId: string
  /** 行点击打开漏洞卡（?page=vulns&vuln=<key>）。 */
  onOpenVuln?: (key: string) => void
  onCounts: (counts: Record<WebApiVulnStatus, number>) => void
  /** 从全局漏洞页跳入时定位该条目；消费后回调清空。 */
  focusVulnId?: string | null
  onFocusConsumed?: () => void
}

type Filter = 'all' | WebApiVulnStatus

const EMPTY_COUNTS: Record<WebApiVulnStatus, number> = { new: 0, present: 0, pending: 0, unconfirmed: 0, fixed: 0 }

const STATUS_ORDER: WebApiVulnStatus[] = ['new', 'present', 'pending', 'unconfirmed', 'fixed']

const STATUS_KEYS: Record<WebApiVulnStatus, string> = {
  new: 'webapi.vulnStatusNew',
  present: 'webapi.vulnStatusPresent',
  pending: 'webapi.vulnStatusPending',
  unconfirmed: 'webapi.vulnStatusUnconfirmed',
  fixed: 'webapi.vulnStatusFixed',
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** 等级列：人工等级 > AI 建议（虚线紧凑徽标）> 未定级；后端资产漏洞列表已带 suggested_severity。 */
function SeverityCell({ vuln }: { vuln: WebApiVuln }) {
  const severity: WebApiVulnSeverity = vuln.triage?.severity || ''
  if (severity) return <SeverityBadge severity={severity} />
  const suggested: WebApiVulnSeverity = (vuln as WebApiVuln & { suggested_severity?: WebApiVulnSeverity }).suggested_severity || ''
  if (suggested) return <SeverityBadge severity={suggested} suggested compact />
  return <SeverityBadge severity="" />
}

/** 端点只显示 `METHOD /path`：去掉 origin/查询串，保留非 URL 原样。 */
function endpointDisplay(key: string | undefined): string {
  if (!key) return '—'
  if (key.startsWith('finding:')) return key
  const match = key.match(/^([A-Z]+)\s+(.+)$/)
  if (!match) return key
  const [, method, target] = match
  try {
    const url = new URL(target)
    return `${method} ${url.pathname}`
  } catch {
    return key
  }
}

/**
 * 资产详情“漏洞库”页签：状态胶囊照漏洞页 + 与漏洞页相同的表格；
 * 行点击打开漏洞卡（原展开成员候选列表去掉——漏洞卡附录已有）。
 */
export default function WebApiAssetVulns({ assetId, onOpenVuln, onCounts, focusVulnId, onFocusConsumed }: Props) {
  const { t } = useTranslation('chat')
  const [list, setList] = useState<WebApiVulnList | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const loadSeq = useRef(0)
  const containerRef = useRef<HTMLDivElement>(null)

  const load = useCallback(async () => {
    const seq = ++loadSeq.current
    setLoading(true)
    setError('')
    try {
      const next = await listWebApiAssetVulns(assetId)
      if (seq !== loadSeq.current) return
      setList(next)
      onCounts(next.counts)
    } catch (err) {
      if (seq !== loadSeq.current) return
      setError(errorText(err))
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [assetId, onCounts])

  useEffect(() => {
    setList(null)
    setFilter('all')
    void load()
  }, [load]) // eslint-disable-line react-hooks/exhaustive-deps

  // 外部定位（全局漏洞页 → 资产漏洞库）：列表就绪后滚到该行。
  useEffect(() => {
    if (!focusVulnId || !list) return
    if (list.vulns.some((item) => item.id === focusVulnId)) {
      requestAnimationFrame(() => {
        const element = containerRef.current?.querySelector<HTMLElement>(`[data-vuln-id="${CSS.escape(focusVulnId)}"]`)
        element?.scrollIntoView({ block: 'nearest' })
      })
    }
    onFocusConsumed?.()
  }, [focusVulnId, list, onFocusConsumed])

  const counts = list?.counts ?? EMPTY_COUNTS
  const total = STATUS_ORDER.reduce((sum, status) => sum + counts[status], 0)
  const visible = list ? list.vulns.filter((item) => filter === 'all' || item.status === filter) : []

  const filterItems: FilterPillItem<Filter>[] = [
    { key: 'all', label: t('webapi.vulnFilterAll'), count: total },
    ...STATUS_ORDER.map((status) => ({
      key: status as Filter,
      label: t(STATUS_KEYS[status]),
      count: counts[status],
    })),
  ]

  return (
    <div className="space-y-3" ref={containerRef}>
      <div className="flex flex-wrap items-center gap-3">
        <FilterPills items={filterItems} value={filter} onChange={setFilter} />
      </div>

      {error && <p role="status" className="text-[12px] text-warning">{t('shell.loadFailed')}：{error}</p>}

      <div className="overflow-hidden rounded-lg border border-border bg-card">
        {list === null && loading ? (
          <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
            <Spinner className="h-5 w-5" />
            {t('shell.loading')}
          </div>
        ) : !list || list.vulns.length === 0 ? (
          <EmptyState title={t('webapi.vulnEmptyTitle')} />
        ) : visible.length === 0 ? (
          <p className="px-4 py-3 text-[12px] text-muted-foreground">{t('webapi.vulnFilterEmpty')}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[980px] table-fixed border-collapse text-[13px] min-[1440px]:min-w-0">
              <colgroup>
                <col className="w-[96px]" />
                <col className="w-[80px]" />
                <col />
                <col className="w-[130px]" />
                <col className="w-[96px]" />
                <col className="w-[96px]" />
                <col className="w-[56px]" />
              </colgroup>
              <thead>
                <tr className="ws-table-head h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                  <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colStatus')}</th>
                  <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.triageSeverity')}</th>
                  <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colVuln')} / {t('shell.triageDisposition')}</th>
                  <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colEndpoint')}</th>
                  <th className="whitespace-nowrap px-3 text-left font-medium">{t('webapi.assetVulnFirstSeen')}</th>
                  <th className="whitespace-nowrap px-3 text-left font-medium">{t('webapi.assetVulnLastConfirmed')}</th>
                  <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colCandidates')}</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((vuln: WebApiVuln) => {
                  const disposition = vuln.triage?.disposition || 'open'
                  return (
                    <tr
                      key={vuln.id}
                      data-vuln-id={vuln.id}
                      className={cn(
                        'cursor-pointer border-b border-border/70 transition-colors hover:bg-muted/40',
                        disposition === 'false_positive' && 'ws-row-muted',
                      )}
                      onClick={() => onOpenVuln?.(vuln.id)}
                    >
                      <td className="px-3 py-2.5">
                        <StatusBadge status={vuln.status} />
                      </td>
                      <td className="px-3 py-2.5">
                        <SeverityCell vuln={vuln} />
                      </td>
                      <td className="min-w-0 px-3 py-2.5">
                        <span className="block truncate font-medium text-foreground" title={vuln.title}>{vuln.title}</span>
                        <span className="mt-1 flex min-w-0 items-center gap-1.5">
                          <DispositionBadge disposition={disposition} />
                          <span className="font-mono text-[11px] text-muted-foreground">{vuln.code}</span>
                        </span>
                      </td>
                      <td className="min-w-0 px-3 py-2.5">
                        <span className="block truncate font-mono text-[11px] text-secondary-foreground" title={endpointDisplay(vuln.endpoint_key)}>
                          {endpointDisplay(vuln.endpoint_key)}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[12px] tabular text-muted-foreground">
                        {formatShortDateTime(vuln.first_seen_at) || '—'}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[12px] tabular text-muted-foreground">
                        {formatShortDateTime(vuln.status === 'fixed' && vuln.fixed_at ? vuln.fixed_at : vuln.last_confirmed_at) || '—'}
                      </td>
                      <td className="px-3 py-2.5 text-right tabular text-secondary-foreground">{vuln.member_count}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}