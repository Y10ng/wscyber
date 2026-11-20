import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ExternalLink, Loader2, RefreshCw } from 'lucide-react'
import {
  Badge, Button, EmptyState, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Spinner,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { FilterPills, IconAction, Page, PageHeader, TableCard, type FilterPillItem } from './PageFrame'
import { usePolling } from '../hooks/usePolling'
import {
  listWebApiAssets,
  listWebApiGlobalVulns,
  type WebApiAsset,
  type WebApiGlobalVuln,
  type WebApiGlobalVulnCounts,
  type WebApiVulnStatus,
} from '../api'

interface VulnsPageProps {
  webapiEnabled: boolean
  onOpenAssetVuln: (assetId: string, vulnId: string) => void
  onOpenTaskFinding: (taskId: string, findingId: string) => void
  onOpenMenu: () => void
}

type VulnFilter = 'all' | WebApiVulnStatus

const PAGE_LIMIT = 50
const ALL_ASSETS = '__all__'
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

const EMPTY_COUNTS: WebApiGlobalVulnCounts = { new: 0, present: 0, pending: 0, unconfirmed: 0, fixed: 0 }

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export default function VulnsPage({ webapiEnabled, onOpenAssetVuln, onOpenTaskFinding, onOpenMenu }: VulnsPageProps) {
  const { t, i18n } = useTranslation('chat')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<VulnFilter>('all')
  const [assetId, setAssetId] = useState('')
  const [assets, setAssets] = useState<WebApiAsset[]>([])
  const [assetError, setAssetError] = useState('')
  const [items, setItems] = useState<WebApiGlobalVuln[]>([])
  const [counts, setCounts] = useState<WebApiGlobalVulnCounts>(EMPTY_COUNTS)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const seq = useRef(0)
  const cursorRef = useRef<string | null>(null)

  // 搜索防抖 300ms：输入停顿后才传 q（同任务页）。
  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchInput.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [searchInput])

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
  }, [webapiEnabled, filter, assetId, query])

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
  const filtered = filter !== 'all' || query !== '' || assetId !== ''

  const openVuln = (item: WebApiGlobalVuln) => {
    if (item.kind === 'asset_vuln') {
      if (item.asset) onOpenAssetVuln(item.asset.id, item.id)
      return
    }
    const findingId = item.id.startsWith('finding:') ? item.id.slice('finding:'.length) : item.id
    onOpenTaskFinding(item.task_id, findingId)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page>
        <PageHeader
          title={t('shell.navVulns')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <IconAction label={t('shell.refresh')} onClick={refresh} disabled={loading}>
              <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
            </IconAction>
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
        </PageHeader>

        <FilterPills items={filterItems} value={filter} onChange={setFilter} />
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
              <table className="w-full min-w-[960px] table-fixed border-collapse text-[13px]">
                <colgroup>
                  <col className="w-[96px]" />
                  <col />
                  <col className="w-[160px]" />
                  <col className="w-[240px]" />
                  <col className="w-[110px]" />
                  <col className="w-[110px]" />
                  <col className="w-16" />
                  <col className="w-14" />
                </colgroup>
                <thead>
                  <tr className="h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colStatus')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colVuln')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colAsset')}</th>
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
                          <span className="block truncate text-[13px] font-medium text-foreground" title={item.title}>{item.title}</span>
                        </td>
                        <td className="px-3">
                          {item.asset
                            ? <span className="block truncate text-[12px] text-foreground" title={item.asset.name}>{item.asset.name}</span>
                            : <Badge variant="muted" size="sm">{t('shell.vulnsNoAsset')}</Badge>}
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
                          <div className="flex justify-end">
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
    </div>
  )
}
