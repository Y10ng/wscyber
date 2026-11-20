import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ClipboardEdit, ExternalLink, Loader2, RefreshCw, Timer } from 'lucide-react'
import {
  Badge, Button, EmptyState, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Spinner,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { FilterPills, IconAction, Page, PageHeader, TableCard, type FilterPillItem } from './PageFrame'
import VulnSlaDialog from './VulnSlaDialog'
import VulnTriageSheet from './VulnTriageSheet'
import { usePolling } from '../hooks/usePolling'
import {
  listWebApiAssets,
  listWebApiGlobalVulns,
  type WebApiAsset,
  type WebApiGlobalVuln,
  type WebApiGlobalVulnCounts,
  type WebApiVulnDisposition,
  type WebApiVulnSeverity,
  type WebApiVulnStatus,
} from '../api'

interface VulnsPageProps {
  webapiEnabled: boolean
  onOpenAssetVuln: (assetId: string, vulnId: string) => void
  onOpenTaskFinding: (taskId: string, findingId: string) => void
  onOpenMenu: () => void
  /** 总览“逾期 N”跳转：置为 true 时筛出逾期条目，消费后由上层清空。 */
  overdueRequested?: boolean
  onOverdueRequestedConsumed?: () => void
}

type VulnFilter = 'all' | WebApiVulnStatus
type SeverityFilter = '' | Exclude<WebApiVulnSeverity, ''> | 'none'
type DispositionFilter = '' | WebApiVulnDisposition

const PAGE_LIMIT = 50
const ALL_ASSETS = '__all__'
const ALL_OPTION = '__all__'
const STATUS_ORDER: WebApiVulnStatus[] = ['new', 'present', 'pending', 'unconfirmed', 'fixed']

// 状态色与资产漏洞库/总览一致：new danger、present warning、pending info、
// unconfirmed muted、fixed success。
const STATUS_META: Record<WebApiVulnStatus, {
  key: string
  variant: 'danger' | 'warning' | 'info' | 'muted' | 'success'
  dot: string
}> = {
  new: { key: 'webapi.vulnStatusNew', variant: 'danger', dot: 'bg-destructive' },
  present: { key: 'webapi.vulnStatusPresent', variant: 'warning', dot: 'bg-warning' },
  pending: { key: 'webapi.vulnStatusPending', variant: 'info', dot: 'bg-info' },
  unconfirmed: { key: 'webapi.vulnStatusUnconfirmed', variant: 'muted', dot: 'bg-muted-foreground' },
  fixed: { key: 'webapi.vulnStatusFixed', variant: 'success', dot: 'bg-success' },
}

const SEVERITY_ORDER: Exclude<WebApiVulnSeverity, ''>[] = ['critical', 'high', 'medium', 'low', 'info']
const SEVERITY_META: Record<Exclude<WebApiVulnSeverity, ''> | 'none', {
  key: string
  variant: 'destructive' | 'danger' | 'warning' | 'info' | 'muted'
  className?: string
}> = {
  critical: { key: 'shell.triageSeverityCritical', variant: 'destructive' },
  high: { key: 'shell.triageSeverityHigh', variant: 'danger' },
  medium: { key: 'shell.triageSeverityMedium', variant: 'warning' },
  low: { key: 'shell.triageSeverityLow', variant: 'info' },
  info: { key: 'shell.triageSeverityInfo', variant: 'muted' },
  none: { key: 'shell.triageSeverityNone', variant: 'muted', className: 'border border-border' },
}

const DISPOSITION_ORDER: WebApiVulnDisposition[] = ['open', 'fixing', 'risk_accepted', 'false_positive']
const DISPOSITION_META: Record<WebApiVulnDisposition, {
  key: string
  variant: 'info' | 'muted'
}> = {
  open: { key: 'shell.triageDispositionOpen', variant: 'muted' },
  fixing: { key: 'shell.triageDispositionFixing', variant: 'info' },
  risk_accepted: { key: 'shell.triageDispositionRiskAccepted', variant: 'muted' },
  false_positive: { key: 'shell.triageDispositionFalsePositive', variant: 'muted' },
}

const EMPTY_COUNTS: WebApiGlobalVulnCounts = {
  new: 0, present: 0, pending: 0, unconfirmed: 0, fixed: 0, overdue: 0,
  by_severity: { critical: 0, high: 0, medium: 0, low: 0, info: 0, none: 0 },
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function severityKey(severity: WebApiVulnSeverity): Exclude<WebApiVulnSeverity, ''> | 'none' {
  return severity || 'none'
}

/** 逾期天数：Hub 本地日与到期日按日期差，至少 1 天（期限当天不算逾期）。 */
function overdueDays(due: string): number {
  const parts = due.split('-').map(Number)
  if (parts.length !== 3 || parts.some((part) => !Number.isFinite(part))) return 1
  const now = new Date()
  const dueUTC = Date.UTC(parts[0], parts[1] - 1, parts[2])
  const todayUTC = Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())
  return Math.max(1, Math.round((todayUTC - dueUTC) / 86400000))
}

export default function VulnsPage({
  webapiEnabled, onOpenAssetVuln, onOpenTaskFinding, onOpenMenu,
  overdueRequested, onOverdueRequestedConsumed,
}: VulnsPageProps) {
  const { t, i18n } = useTranslation('chat')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<VulnFilter>('all')
  const [severity, setSeverity] = useState<SeverityFilter>('')
  const [disposition, setDisposition] = useState<DispositionFilter>('')
  const [overdueOnly, setOverdueOnly] = useState(false)
  const [assetId, setAssetId] = useState('')
  const [assets, setAssets] = useState<WebApiAsset[]>([])
  const [assetError, setAssetError] = useState('')
  const [items, setItems] = useState<WebApiGlobalVuln[]>([])
  const [counts, setCounts] = useState<WebApiGlobalVulnCounts>(EMPTY_COUNTS)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [triageItem, setTriageItem] = useState<WebApiGlobalVuln | null>(null)
  const [slaOpen, setSlaOpen] = useState(false)
  const seq = useRef(0)
  const cursorRef = useRef<string | null>(null)

  // 搜索防抖 300ms：输入停顿后才传 q（同任务页）。
  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchInput.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [searchInput])

  // 总览“逾期 N”跳转：打开逾期筛选，并通知上层消费该请求。
  useEffect(() => {
    if (!overdueRequested) return
    setOverdueOnly(true)
    onOverdueRequestedConsumed?.()
  }, [overdueRequested, onOverdueRequestedConsumed])

  const loadAssets = useCallback(async () => {
    if (!webapiEnabled) return
    try {
      const next = await listWebApiAssets(false)
      setAssets(next)
      setAssetError('')
    } catch (err) {
      setAssetError(errorText(err))
    }
  }, [webapiEnabled])

  useEffect(() => { void loadAssets() }, [loadAssets])

  const loadVulns = useCallback(async (opts: { reset: boolean; silent?: boolean }) => {
    if (!webapiEnabled) return
    const nextCursor = opts.reset ? undefined : cursorRef.current || undefined
    if (!opts.reset && !nextCursor) return
    const current = ++seq.current
    if (!opts.silent) {
      if (opts.reset) setLoading(true)
      else setLoadingMore(true)
    }
    try {
      const data = await listWebApiGlobalVulns({
        status: filter === 'all' ? undefined : filter,
        severity: severity || undefined,
        disposition: disposition || undefined,
        overdue: overdueOnly || undefined,
        asset_id: assetId || undefined,
        q: query || undefined,
        limit: PAGE_LIMIT,
        cursor: nextCursor,
      })
      if (current !== seq.current) return
      cursorRef.current = data.next_cursor
      setHasMore(data.has_more)
      setCounts(data.counts)
      setItems((prev) => (opts.reset ? data.items : [...prev, ...data.items]))
      setError('')
    } catch (err) {
      if (current !== seq.current) return
      setError(errorText(err))
    } finally {
      if (current === seq.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    }
  }, [webapiEnabled, filter, severity, disposition, overdueOnly, assetId, query])

  // 条件变化时回到第一页。
  useEffect(() => {
    if (!webapiEnabled) { setLoading(false); return }
    cursorRef.current = null
    void loadVulns({ reset: true })
  }, [loadVulns, webapiEnabled])

  // 15 秒静默刷新第一页；usePolling 在标签页隐藏时暂停。
  usePolling(() => { void loadVulns({ reset: true, silent: true }) }, 15000, webapiEnabled)

  const refresh = () => {
    void loadAssets()
    cursorRef.current = null
    void loadVulns({ reset: true })
  }

  const formatCreated = (iso: string) => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return { day: iso, time: '' }
    return {
      day: date.toLocaleDateString(i18n.language, { year: 'numeric', month: '2-digit', day: '2-digit' }),
      time: date.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit', hour12: false }),
    }
  }

  const total = STATUS_ORDER.reduce((sum, status) => sum + counts[status], 0)
  const filterItems: FilterPillItem<VulnFilter>[] = [
    { key: 'all', label: t('shell.filterAll'), count: total },
    ...STATUS_ORDER.map((status) => ({
      key: status,
      label: t(STATUS_META[status].key),
      count: counts[status],
      dot: STATUS_META[status].dot,
    })),
  ]
  const filtered = filter !== 'all' || query !== '' || assetId !== '' || severity !== '' || disposition !== '' || overdueOnly

  const openVuln = (item: WebApiGlobalVuln) => {
    if (item.kind === 'asset_vuln') {
      if (item.asset) onOpenAssetVuln(item.asset.id, item.id)
      return
    }
    const findingId = item.id.startsWith('finding:') ? item.id.slice('finding:'.length) : item.id
    onOpenTaskFinding(item.task_id, findingId)
  }

  const refreshAfterSave = () => {
    // 保存后刷新列表当前页：重置到第一页取最新派生值（期限/逾期读时计算）。
    cursorRef.current = null
    void loadVulns({ reset: true, silent: true })
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page>
        <PageHeader
          title={t('shell.navVulns')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-9"
                onClick={() => setSlaOpen(true)}
              >
                <Timer className="mr-1.5 h-3.5 w-3.5" />
                {t('shell.triageSlaSettings')}
              </Button>
              <IconAction label={t('shell.refresh')} onClick={refresh} disabled={loading}>
                <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
              </IconAction>
            </>
          )}
        >
          <Input
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder={t('shell.searchVulns')}
            aria-label={t('shell.searchVulns')}
            className="h-9 w-[220px] sm:w-[320px]"
          />
          <Select value={assetId || ALL_ASSETS} onValueChange={(value) => setAssetId(value === ALL_ASSETS ? '' : value)}>
            <SelectTrigger aria-label={t('shell.vulnsAssetFilter')} className="h-9 w-[180px] shrink-0">
              <SelectValue placeholder={t('shell.vulnsAssetAll')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_ASSETS}>{t('shell.vulnsAssetAll')}</SelectItem>
              {assets.map((asset) => (
                <SelectItem key={asset.id} value={asset.id}>{asset.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={severity || ALL_OPTION}
            onValueChange={(value) => setSeverity(value === ALL_OPTION ? '' : value as SeverityFilter)}
          >
            <SelectTrigger aria-label={t('shell.triageSeverityFilter')} className="h-9 w-[120px] shrink-0">
              <SelectValue placeholder={t('shell.triageSeverityAll')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_OPTION}>{t('shell.triageSeverityAll')}</SelectItem>
              {SEVERITY_ORDER.map((value) => (
                <SelectItem key={value} value={value}>{t(SEVERITY_META[value].key)}</SelectItem>
              ))}
              <SelectItem value="none">{t('shell.triageSeverityNone')}</SelectItem>
            </SelectContent>
          </Select>
          <Select
            value={disposition || ALL_OPTION}
            onValueChange={(value) => setDisposition(value === ALL_OPTION ? '' : value as DispositionFilter)}
          >
            <SelectTrigger aria-label={t('shell.triageDispositionFilter')} className="h-9 w-[120px] shrink-0">
              <SelectValue placeholder={t('shell.triageDispositionAll')} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_OPTION}>{t('shell.triageDispositionAll')}</SelectItem>
              {DISPOSITION_ORDER.map((value) => (
                <SelectItem key={value} value={value}>{t(DISPOSITION_META[value].key)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </PageHeader>

        <div className="flex flex-wrap items-center gap-1.5">
          <FilterPills items={filterItems} value={filter} onChange={setFilter} />
          <button
            type="button"
            aria-pressed={overdueOnly}
            onClick={() => setOverdueOnly((value) => !value)}
            className={cn(
              'flex h-8 items-center gap-1.5 rounded-full px-3 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
              overdueOnly ? 'border border-border bg-accent font-medium text-foreground' : 'border border-transparent text-muted-foreground hover:bg-muted/40 hover:text-foreground',
            )}
          >
            <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-destructive" />
            <span>{t('shell.triageOverdue')}</span>
            <span className={cn('tabular-nums', overdueOnly ? 'text-foreground/70' : 'text-muted-foreground')}>{counts.overdue}</span>
          </button>
        </div>
        {(error || assetError) && (
          <p role="status" className="text-[12px] text-warning">{t('shell.loadFailed')}：{error || assetError}</p>
        )}
        <TableCard
          footer={(
            <>
              <span className="text-[12px] text-muted-foreground">{t('shell.vulnsShown', { count: items.length })}</span>
              {hasMore && (
                <Button type="button" variant="outline" size="sm" className="ml-auto h-8" disabled={loadingMore} onClick={() => void loadVulns({ reset: false })}>
                  {loadingMore && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
                  {t('shell.loadMore')}
                </Button>
              )}
            </>
          )}
        >
          {loading ? (
            <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('shell.loading')}
            </div>
          ) : items.length === 0 ? (
            <EmptyState title={filtered ? t('shell.vulnsFilterEmpty') : t('shell.vulnsEmpty')} />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[1180px] table-fixed border-collapse text-[13px] min-[1440px]:min-w-0">
                <colgroup>
                  <col className="w-[84px]" />
                  <col />
                  <col className="w-[84px]" />
                  <col className="w-[120px]" />
                  <col className="w-[110px]" />
                  <col className="w-[100px]" />
                  <col className="w-[180px]" />
                  <col className="w-[92px]" />
                  <col className="w-[92px]" />
                  <col className="w-[52px]" />
                  <col className="w-[72px]" />
                </colgroup>
                <thead>
                  <tr className="h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colStatus')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colVuln')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.triageSeverity')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colAsset')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.triageOwner')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.triageDue')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colEndpoint')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colFirstSeen')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colLastConfirmed')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colCandidates')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => {
                    const first = formatCreated(item.first_seen_at)
                    const meta = STATUS_META[item.status]
                    const triage = item.triage
                    const sev = severityKey(triage?.severity || '')
                    const sevMeta = SEVERITY_META[sev]
                    const disp = triage?.disposition || 'open'
                    const dispMeta = DISPOSITION_META[disp]
                    const falsePositive = disp === 'false_positive'
                    const ownerFromAsset = Boolean(triage && !triage.owner && triage.owner_effective)
                    const fixedAt = item.status === 'fixed' && item.fixed_at ? formatCreated(item.fixed_at) : null
                    const confirmed = fixedAt || (item.last_confirmed_at ? formatCreated(item.last_confirmed_at) : null)
                    return (
                      <tr
                        key={item.id}
                        className="h-12 cursor-pointer border-b border-border/70 transition-colors hover:bg-muted/40"
                        onClick={() => openVuln(item)}
                      >
                        <td className="px-3">
                          <Badge variant={meta.variant} size="sm">{t(meta.key)}</Badge>
                        </td>
                        <td className="min-w-0 px-3">
                          <span
                            className={cn('block truncate text-[13px] font-medium text-foreground', falsePositive && 'text-muted-foreground line-through')}
                            title={item.title}
                          >
                            {item.title}
                          </span>
                          {disp !== 'open' && (
                            <Badge variant={dispMeta.variant} size="sm" className="mt-0.5">{t(dispMeta.key)}</Badge>
                          )}
                        </td>
                        <td className="px-3">
                          <Badge variant={sevMeta.variant} size="sm" className={sevMeta.className}>{t(sevMeta.key)}</Badge>
                        </td>
                        <td className="px-3">
                          {item.asset
                            ? <span className="block truncate text-[12px] text-foreground" title={item.asset.name}>{item.asset.name}</span>
                            : <Badge variant="muted" size="sm">{t('shell.vulnsNoAsset')}</Badge>}
                        </td>
                        <td className="px-3">
                          {triage?.owner_effective
                            ? (
                              <span
                                className={cn('block truncate text-[12px]', ownerFromAsset ? 'text-muted-foreground' : 'text-foreground')}
                                title={triage.owner_effective}
                              >
                                {triage.owner_effective}
                              </span>
                            )
                            : <span className="text-[12px] text-muted-foreground">—</span>}
                        </td>
                        <td className="px-3 leading-tight">
                          {triage?.due_effective ? (
                            <>
                              <div className={cn('text-[12px]', triage.overdue ? 'text-destructive' : 'text-foreground')}>{triage.due_effective}</div>
                              {triage.overdue && (
                                <div className="text-[11px] text-destructive">
                                  {t('shell.triageOverdueDays', { count: overdueDays(triage.due_effective) })}
                                </div>
                              )}
                            </>
                          ) : <span className="text-[12px] text-muted-foreground">—</span>}
                        </td>
                        <td className="min-w-0 px-3">
                          <span className="block truncate font-mono text-[12px] text-muted-foreground" title={item.endpoint_key || undefined}>
                            {item.endpoint_key || '—'}
                          </span>
                        </td>
                        <td className="px-3 leading-tight">
                          <div className="text-[12px] text-foreground">{first.day}</div>
                          <div className="font-mono text-[11px] text-muted-foreground">{first.time}</div>
                        </td>
                        <td className="px-3 leading-tight">
                          {confirmed ? (
                            <>
                              <div className="text-[12px] text-foreground">{confirmed.day}</div>
                              <div className="font-mono text-[11px] text-muted-foreground">
                                {fixedAt ? `${t('shell.vulnsFixedAtLabel')} ${confirmed.time}` : confirmed.time}
                              </div>
                            </>
                          ) : <span className="text-[12px] text-muted-foreground">—</span>}
                        </td>
                        <td className="px-3 text-right tabular-nums text-foreground">{item.member_count}</td>
                        <td className="px-3" onClick={(event) => event.stopPropagation()}>
                          <div className="flex justify-end gap-1">
                            <IconAction label={t('shell.triageOpen')} onClick={() => setTriageItem(item)}>
                              <ClipboardEdit className="h-3.5 w-3.5" />
                            </IconAction>
                            <IconAction label={t('shell.vulnsOpen')} onClick={() => openVuln(item)}>
                              <ExternalLink className="h-3.5 w-3.5" />
                            </IconAction>
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </TableCard>
      </Page>

      <VulnTriageSheet
        open={triageItem !== null}
        item={triageItem}
        onClose={() => setTriageItem(null)}
        onSaved={refreshAfterSave}
      />
      <VulnSlaDialog
        open={slaOpen}
        onClose={() => setSlaOpen(false)}
        onSaved={refreshAfterSave}
      />
    </div>
  )
}
