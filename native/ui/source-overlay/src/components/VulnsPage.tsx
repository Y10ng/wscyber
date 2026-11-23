import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Download, ClipboardEdit, ExternalLink, HelpCircle, Loader2, RefreshCw, Timer,
} from 'lucide-react'
import {
  Button, Checkbox, EmptyState, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Spinner,
  Tooltip, TooltipContent, TooltipTrigger,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { FilterPills, IconAction, Page, PageHeader, TableCard, type FilterPillItem } from './PageFrame'
import VulnSlaDialog from './VulnSlaDialog'
import VulnTriageSheet from './VulnTriageSheet'
import { DeadlineText, DispositionBadge, SeverityBadge, StatusBadge } from './ui-badges'
import { usePolling } from '../hooks/usePolling'
import {
  exportWebApiVulns,
  listWebApiAssets,
  listWebApiGlobalVulns,
  webApiVulnExportFilename,
  type WebApiAsset,
  type WebApiGlobalVuln,
  type WebApiGlobalVulnCounts,
  type WebApiVulnDisposition,
  type WebApiVulnSeverity,
  type WebApiVulnStatus,
} from '../api'
import { downloadText } from '../lib/download'

interface VulnsPageProps {
  webapiEnabled: boolean
  /** K1：行点击/“打开”进入漏洞卡；跳到任务/资产改由卡片顶部按钮完成。 */
  onOpenVuln: (key: string) => void
  onOpenMenu: () => void
  /** 总览“逾期 N”跳转：置为 true 时筛出逾期条目，消费后由上层清空。 */
  overdueRequested?: boolean
  onOverdueRequestedConsumed?: () => void
  /** 总览“待定级”跳转：置为 true 时筛出未定级条目，消费后由上层清空。 */
  unratedRequested?: boolean
  onUnratedRequestedConsumed?: () => void
}

type VulnFilter = 'all' | WebApiVulnStatus
type SeverityFilter = '' | Exclude<WebApiVulnSeverity, ''> | 'none'
type DispositionFilter = '' | WebApiVulnDisposition

const PAGE_LIMIT = 50
const ALL_ASSETS = '__all__'
const ALL_OPTION = '__all__'
const STATUS_ORDER: WebApiVulnStatus[] = ['new', 'present', 'pending', 'unconfirmed', 'fixed']

const SEVERITY_ORDER: Exclude<WebApiVulnSeverity, ''>[] = ['critical', 'high', 'medium', 'low', 'info']
const SEVERITY_KEYS: Record<Exclude<WebApiVulnSeverity, ''> | 'none', string> = {
  critical: 'shell.triageSeverityCritical',
  high: 'shell.triageSeverityHigh',
  medium: 'shell.triageSeverityMedium',
  low: 'shell.triageSeverityLow',
  info: 'shell.triageSeverityInfo',
  none: 'shell.triageSeverityNone',
}

const DISPOSITION_ORDER: WebApiVulnDisposition[] = ['open', 'fixing', 'risk_accepted', 'false_positive']
const DISPOSITION_KEYS: Record<WebApiVulnDisposition, string> = {
  open: 'shell.triageDispositionOpen',
  fixing: 'shell.triageDispositionFixing',
  risk_accepted: 'shell.triageDispositionRiskAccepted',
  false_positive: 'shell.triageDispositionFalsePositive',
}

const EMPTY_COUNTS: WebApiGlobalVulnCounts = {
  new: 0, present: 0, pending: 0, unconfirmed: 0, fixed: 0, overdue: 0,
  by_severity: { critical: 0, high: 0, medium: 0, low: 0, info: 0, none: 0 },
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** 端点只显示 `METHOD /path`：去掉 origin/查询串，保留非 URL 原样。 */
function endpointDisplay(key: string | undefined): string {
  if (!key) return '—'
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

export default function VulnsPage({
  webapiEnabled, onOpenVuln, onOpenMenu,
  overdueRequested, onOverdueRequestedConsumed,
  unratedRequested, onUnratedRequestedConsumed,
}: VulnsPageProps) {
  const { t } = useTranslation('chat')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<VulnFilter>('all')
  const [severity, setSeverity] = useState<SeverityFilter>('')
  const [disposition, setDisposition] = useState<DispositionFilter>('')
  const [overdueOnly, setOverdueOnly] = useState(false)
  // 总览“待定级”专用模式：后端 status 只收单值，用 severity=none + disposition=open,fixing
  // 请求，再在前端只保留 new/present（即“未修复且未定级”）。
  const [unratedOnly, setUnratedOnly] = useState(false)
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
  // K2：按 item.id 多选导出（筛选变化清空；“加载更多”保留）。
  const [selected, setSelected] = useState<Set<string>>(() => new Set())
  const [exporting, setExporting] = useState<'markdown' | 'json' | null>(null)
  const [exportError, setExportError] = useState('')
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

  // 总览“待定级”跳转：清空其他筛选，只保留“未修复且未定级”。
  useEffect(() => {
    if (!unratedRequested) return
    setFilter('all')
    setDisposition('')
    setOverdueOnly(false)
    setSeverity('none')
    setUnratedOnly(true)
    onUnratedRequestedConsumed?.()
  }, [unratedRequested, onUnratedRequestedConsumed])

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
        status: unratedOnly ? undefined : (filter === 'all' ? undefined : filter),
        severity: unratedOnly ? 'none' : (severity || undefined),
        disposition: unratedOnly ? 'open,fixing' : (disposition || undefined),
        overdue: overdueOnly || undefined,
        asset_id: assetId || undefined,
        q: query || undefined,
        limit: PAGE_LIMIT,
        cursor: nextCursor,
      })
      if (current !== seq.current) return
      // 待定级：后端 status 不支持多值，按 new/present 在客户端收窄（口径同总览）。
      const pageItems = unratedOnly
        ? data.items.filter((item) => item.status === 'new' || item.status === 'present')
        : data.items
      cursorRef.current = data.next_cursor
      setHasMore(data.has_more)
      setCounts(data.counts)
      setItems((prev) => (opts.reset ? pageItems : [...prev, ...pageItems]))
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
  }, [webapiEnabled, filter, severity, disposition, overdueOnly, unratedOnly, assetId, query])

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

  const total = STATUS_ORDER.reduce((sum, status) => sum + counts[status], 0)
  const filterItems: FilterPillItem<VulnFilter>[] = [
    { key: 'all', label: t('shell.filterAll'), count: total },
    ...STATUS_ORDER.map((status) => ({
      key: status,
      label: t(`webapi.vulnStatus${status.charAt(0).toUpperCase()}${status.slice(1)}`),
      count: counts[status],
    })),
  ]
  const filtered = filter !== 'all' || query !== '' || assetId !== '' || severity !== '' || disposition !== '' || overdueOnly || unratedOnly

  const openVuln = (item: WebApiGlobalVuln) => {
    onOpenVuln(item.id)
  }

  const refreshAfterSave = () => {
    // 保存后刷新列表当前页：重置到第一页取最新派生值（期限/逾期读时计算）。
    cursorRef.current = null
    void loadVulns({ reset: true, silent: true })
  }

  // 任一筛选/搜索条件变化都清空选择；“加载更多”不改条件，故不清空。
  useEffect(() => {
    setSelected(new Set())
    setExportError('')
  }, [filter, severity, disposition, overdueOnly, unratedOnly, assetId, query])

  const selectedItems = items.filter((item) => selected.has(item.id))
  const allSelected = items.length > 0 && items.every((item) => selected.has(item.id))
  const toggleAll = () => {
    setSelected(allSelected ? new Set() : new Set(items.map((item) => item.id)))
  }
  const toggleOne = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }
  const exportSelected = async (format: 'markdown' | 'json') => {
    if (exporting !== null || selected.size === 0 || selected.size > 50) return
    setExporting(format)
    setExportError('')
    try {
      // 按表格显示顺序导出当前已加载的选中项。
      const text = await exportWebApiVulns(selectedItems.map((item) => item.id), format)
      downloadText(text, webApiVulnExportFilename(null, format), format)
    } catch (err) {
      setExportError(errorText(err))
    } finally {
      setExporting(null)
    }
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
              <Button type="button" variant="outline" size="sm" className="h-9" onClick={refresh} disabled={loading}>
                <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')} />
                {t('shell.refresh')}
              </Button>
            </>
          )}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label={t('shell.vulnsHelp')}
                className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <HelpCircle className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-[260px] whitespace-normal text-left leading-relaxed">
              {t('shell.vulnsHelp')}
            </TooltipContent>
          </Tooltip>
        </PageHeader>

        {/* 筛选胶囊 + 右侧 [资产/等级/处置 + 搜索]（设计稿：搜索框最右） */}
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex flex-wrap items-center gap-1.5">
            <FilterPills items={filterItems} value={filter} onChange={(value) => { setUnratedOnly(false); setFilter(value) }} />
            <button
              type="button"
              aria-pressed={overdueOnly}
              onClick={() => { setUnratedOnly(false); setOverdueOnly((value) => !value) }}
              className={cn(
                'flex h-8 items-center gap-1.5 rounded-full border px-2.5 text-[12px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                overdueOnly
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border bg-card text-muted-foreground hover:text-foreground',
              )}
            >
              <span>{t('shell.triageOverdue')}</span>
              <span className={cn('tabular', overdueOnly ? 'text-primary-foreground/80' : 'text-muted-foreground')}>{counts.overdue}</span>
            </button>
            <button
              type="button"
              aria-pressed={unratedOnly}
              title={t('overview.todoUnratedHint')}
              onClick={() => {
                if (unratedOnly) {
                  setUnratedOnly(false)
                  setSeverity('')
                  return
                }
                setUnratedOnly(true)
                setFilter('all')
                setSeverity('none')
                setDisposition('')
                setOverdueOnly(false)
              }}
              className={cn(
                'flex h-8 items-center gap-1.5 rounded-full border px-2.5 text-[12px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                unratedOnly
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border bg-card text-muted-foreground hover:text-foreground',
              )}
            >
              <span>{t('overview.todoUnrated')}</span>
            </button>
          </div>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <Select value={assetId || ALL_ASSETS} onValueChange={(value) => setAssetId(value === ALL_ASSETS ? '' : value)}>
              <SelectTrigger aria-label={t('shell.vulnsAssetFilter')} className="h-9 w-[168px] shrink-0 bg-card">
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
              onValueChange={(value) => { setUnratedOnly(false); setSeverity(value === ALL_OPTION ? '' : value as SeverityFilter) }}
            >
              <SelectTrigger aria-label={t('shell.triageSeverityFilter')} className="h-9 w-[132px] shrink-0 bg-card">
                <SelectValue placeholder={t('shell.triageSeverityAll')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_OPTION}>{t('shell.triageSeverityAll')}</SelectItem>
                {SEVERITY_ORDER.map((value) => (
                  <SelectItem key={value} value={value}>{t(SEVERITY_KEYS[value])}</SelectItem>
                ))}
                <SelectItem value="none">{t('shell.triageSeverityNone')}</SelectItem>
              </SelectContent>
            </Select>
            <Select
              value={disposition || ALL_OPTION}
              onValueChange={(value) => { setUnratedOnly(false); setDisposition(value === ALL_OPTION ? '' : value as DispositionFilter) }}
            >
              <SelectTrigger aria-label={t('shell.triageDispositionFilter')} className="h-9 w-[132px] shrink-0 bg-card">
                <SelectValue placeholder={t('shell.triageDispositionAll')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_OPTION}>{t('shell.triageDispositionAll')}</SelectItem>
                {DISPOSITION_ORDER.map((value) => (
                  <SelectItem key={value} value={value}>{t(DISPOSITION_KEYS[value])}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder={t('shell.searchVulns')}
              aria-label={t('shell.searchVulns')}
              className="h-9 w-[220px] bg-card sm:w-[240px]"
            />
          </div>
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
          {selected.size > 0 && (
            <div className="flex h-11 flex-wrap items-center gap-2 border-b border-border bg-muted/30 px-4 text-[13px]">
              <span className="text-secondary-foreground">{t('shell.vulnsSelected', { count: selected.size })}</span>
              <span className="text-muted-foreground">·</span>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 gap-1"
                disabled={exporting !== null || selected.size > 50}
                onClick={() => { void exportSelected('markdown') }}
              >
                {exporting === 'markdown'
                  ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  : <Download className="h-3.5 w-3.5" />}
                {t('shell.vulnsExportMarkdown')}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-8 gap-1"
                disabled={exporting !== null || selected.size > 50}
                onClick={() => { void exportSelected('json') }}
              >
                {exporting === 'json'
                  ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  : <Download className="h-3.5 w-3.5" />}
                {t('shell.vulnsExportJson')}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-8"
                onClick={() => setSelected(new Set())}
              >
                {t('shell.vulnsClearSelection')}
              </Button>
              {selected.size > 50 && (
                <span className="text-[12px] text-warning">{t('shell.vulnsExportLimit')}</span>
              )}
              {exportError && (
                <span className="text-[12px] text-destructive">{t('shell.vulnsExportFailed', { error: exportError })}</span>
              )}
            </div>
          )}
          {loading ? (
            <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('shell.loading')}
            </div>
          ) : items.length === 0 ? (
            <EmptyState title={filtered ? t('shell.vulnsFilterEmpty') : t('shell.vulnsEmpty')} />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[1120px] table-fixed border-collapse text-[13px] min-[1440px]:min-w-0">
                <colgroup>
                  <col className="w-[40px]" />
                  <col className="w-[96px]" />
                  <col className="w-[80px]" />
                  <col />
                  <col className="w-[130px]" />
                  <col className="w-[96px]" />
                  <col className="w-[128px]" />
                  <col className="w-[180px]" />
                  <col className="w-[56px]" />
                  <col className="w-[72px]" />
                </colgroup>
                <thead>
                  <tr className="ws-table-head h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                    <th className="px-3">
                      <Checkbox
                        checked={allSelected}
                        onCheckedChange={toggleAll}
                        aria-label={t('shell.vulnsSelectAll')}
                      />
                    </th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colStatus')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.triageSeverity')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colVuln')} / {t('shell.triageDisposition')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colAsset')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.triageOwner')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.triageDue')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colEndpoint')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colCandidates')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => {
                    const triage = item.triage
                    const disp = triage?.disposition || 'open'
                    const falsePositive = disp === 'false_positive'
                    const ownerFromAsset = Boolean(triage && !triage.owner && triage.owner_effective)
                    const endpoint = endpointDisplay(item.endpoint_key)
                    return (
                      <tr
                        key={item.id}
                        className={cn(
                          'cursor-pointer border-b border-border/70 transition-colors hover:bg-muted/40',
                          falsePositive && 'ws-row-muted',
                        )}
                        onClick={() => openVuln(item)}
                      >
                        <td className="px-3 py-2.5" onClick={(event) => event.stopPropagation()}>
                          <Checkbox
                            checked={selected.has(item.id)}
                            onCheckedChange={() => toggleOne(item.id)}
                            aria-label={t('shell.vulnsSelectRow', { code: item.code })}
                          />
                        </td>
                        <td className="px-3 py-2.5">
                          <StatusBadge status={item.status} />
                        </td>
                        <td className="px-3 py-2.5">
                          {triage?.severity
                            ? <SeverityBadge severity={triage.severity} />
                            : item.suggested_severity
                              ? <SeverityBadge severity={item.suggested_severity} suggested compact />
                              : <SeverityBadge severity="" />}
                        </td>
                        <td className="min-w-0 px-3 py-2.5">
                          <span className="block truncate font-medium text-foreground" title={item.title}>
                            {item.title}
                          </span>
                          <span className="mt-1 flex min-w-0 items-center gap-1.5">
                            <DispositionBadge disposition={disp} />
                            <span className="font-mono text-[11px] text-muted-foreground">{item.code}</span>
                          </span>
                        </td>
                        <td className="px-3 py-2.5">
                          {item.asset
                            ? <span className="block truncate text-[12px] text-foreground" title={item.asset.name}>{item.asset.name}</span>
                            : <span className="text-[12px] text-muted-foreground">{t('shell.vulnsNoAsset')}</span>}
                        </td>
                        <td className="px-3 py-2.5">
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
                        <td className="whitespace-nowrap px-3 py-2.5">
                          <DeadlineText
                            due={triage?.due_effective}
                            overdue={triage?.overdue}
                            fixed={item.status === 'fixed'}
                            className="text-[12px]"
                          />
                        </td>
                        <td className="min-w-0 px-3 py-2.5">
                          <span className="block truncate font-mono text-[11px] text-secondary-foreground" title={endpoint}>
                            {endpoint}
                          </span>
                        </td>
                        <td className="px-3 py-2.5 text-right tabular text-secondary-foreground">{item.member_count}</td>
                        <td className="px-3 py-2.5" onClick={(event) => event.stopPropagation()}>
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
