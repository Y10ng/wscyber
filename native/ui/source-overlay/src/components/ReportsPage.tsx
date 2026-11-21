import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Braces, ExternalLink, FileDown, HelpCircle, Loader2, RefreshCw } from 'lucide-react'
import {
  Button, EmptyState, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
  Spinner, Tooltip, TooltipContent, TooltipTrigger,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { Page, PageHeader, TableCard } from './PageFrame'
import { formatShortDateTime } from '../lib/time'
import {
  exportWebApiReport,
  listWebApiAssets,
  listWebApiGlobalReports,
  webApiReportExportFilename,
  type WebApiAsset,
  type WebApiGlobalReport,
} from '../api'

interface ReportsPageProps {
  webapiEnabled: boolean
  onOpenTaskReport: (taskId: string, reportId: string) => void
  onOpenMenu: () => void
}

const PAGE_LIMIT = 50
const ALL_ASSETS = '__all__'

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export default function ReportsPage({ webapiEnabled, onOpenTaskReport, onOpenMenu }: ReportsPageProps) {
  const { t } = useTranslation('chat')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [assetId, setAssetId] = useState('')
  const [assets, setAssets] = useState<WebApiAsset[]>([])
  const [assetError, setAssetError] = useState('')
  const [items, setItems] = useState<WebApiGlobalReport[]>([])
  const [total, setTotal] = useState(0)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [exporting, setExporting] = useState<string | null>(null)
  const [exportError, setExportError] = useState('')
  const seq = useRef(0)
  const cursorRef = useRef<string | null>(null)

  // 搜索防抖 300ms（同任务页）。
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

  const loadReports = useCallback(async (opts: { reset: boolean }) => {
    if (!webapiEnabled) return
    const nextCursor = opts.reset ? undefined : cursorRef.current || undefined
    if (!opts.reset && !nextCursor) return
    const current = ++seq.current
    if (opts.reset) setLoading(true)
    else setLoadingMore(true)
    try {
      const data = await listWebApiGlobalReports({
        q: query || undefined,
        asset_id: assetId || undefined,
        limit: PAGE_LIMIT,
        cursor: nextCursor,
      })
      if (current !== seq.current) return
      cursorRef.current = data.next_cursor
      setHasMore(data.has_more)
      setTotal(data.total)
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
  }, [webapiEnabled, query, assetId])

  useEffect(() => {
    if (!webapiEnabled) { setLoading(false); return }
    cursorRef.current = null
    void loadReports({ reset: true })
  }, [loadReports, webapiEnabled])

  const refresh = () => {
    void loadAssets()
    cursorRef.current = null
    void loadReports({ reset: true })
  }

  // 导出由已存草稿确定性渲染；无草稿时后端 409，前端先禁用并说明。
  const handleExport = async (item: WebApiGlobalReport, format: 'markdown' | 'json') => {
    if (!item.has_draft || exporting) return
    const key = `${item.report_id}:${format}`
    setExporting(key)
    setExportError('')
    let url: string | undefined
    let anchor: HTMLAnchorElement | undefined
    try {
      const text = await exportWebApiReport(item.task_id, item.report_id, format)
      url = URL.createObjectURL(new Blob([text], { type: format === 'markdown' ? 'text/markdown;charset=utf-8' : 'application/json' }))
      anchor = document.createElement('a')
      anchor.href = url
      anchor.download = webApiReportExportFilename(item.report_id, format)
      document.body.appendChild(anchor)
      anchor.click()
    } catch (err) {
      setExportError(errorText(err))
    } finally {
      anchor?.remove()
      if (url) URL.revokeObjectURL(url)
      setExporting(null)
    }
  }

  const exportButton = (item: WebApiGlobalReport, format: 'markdown' | 'json', label: string, icon: ReactNode) => {
    const busy = exporting === `${item.report_id}:${format}`
    const disabled = !item.has_draft || (exporting !== null && !busy)
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span tabIndex={disabled ? 0 : -1} className="inline-flex">
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={label}
              disabled={disabled}
              onClick={(event) => { event.stopPropagation(); void handleExport(item, format) }}
              className="text-muted-foreground"
            >
              {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : icon}
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent>{item.has_draft ? label : t('shell.reportsExportDisabled')}</TooltipContent>
      </Tooltip>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page>
        <PageHeader
          title={t('shell.navReports')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <Button type="button" variant="outline" size="sm" className="h-9" onClick={refresh} disabled={loading}>
              <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')} />
              {t('shell.refresh')}
            </Button>
          )}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label={t('shell.reportsHelp')}
                className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <HelpCircle className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px] whitespace-normal text-left leading-relaxed">
              {t('shell.reportsHelp')}
            </TooltipContent>
          </Tooltip>
        </PageHeader>

        {/* 筛选行：左侧留空，右侧资产下拉 + 搜索（同漏洞页）。 */}
        <div className="flex flex-wrap items-center gap-2">
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <Select value={assetId || ALL_ASSETS} onValueChange={(value) => setAssetId(value === ALL_ASSETS ? '' : value)}>
              <SelectTrigger aria-label={t('shell.reportsAssetFilter')} className="h-9 w-[168px] shrink-0 bg-card">
                <SelectValue placeholder={t('shell.reportsAssetAll')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_ASSETS}>{t('shell.reportsAssetAll')}</SelectItem>
                {assets.map((asset) => (
                  <SelectItem key={asset.id} value={asset.id}>{asset.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder={t('shell.searchReports')}
              aria-label={t('shell.searchReports')}
              className="h-9 w-[240px] bg-card"
            />
          </div>
        </div>

        {(error || assetError || exportError) && (
          <p role="status" className="text-[12px] text-warning">{t('shell.loadFailed')}：{error || assetError || exportError}</p>
        )}
        <TableCard
          footer={(
            <>
              <span className="text-[12px] text-muted-foreground">{t('shell.reportsTotal', { total })}</span>
              {hasMore && (
                <Button type="button" variant="outline" size="sm" className="ml-auto h-8" disabled={loadingMore} onClick={() => void loadReports({ reset: false })}>
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
            <EmptyState title={t('shell.reportsEmpty')} />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[1000px] table-fixed border-collapse text-[13px] min-[1440px]:min-w-0">
                <colgroup>
                  <col />
                  <col className="w-[240px]" />
                  <col className="w-[140px]" />
                  <col className="w-[72px]" />
                  <col className="w-[116px]" />
                  <col className="w-[116px]" />
                  <col className="w-[112px]" />
                </colgroup>
                <thead>
                  <tr className="ws-table-head h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colReport')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colTask')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colAsset')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colItems')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colCreated')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colUpdated')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => (
                    <tr
                      key={item.report_id}
                      className="cursor-pointer border-b border-border/70 transition-colors hover:bg-muted/40"
                      onClick={() => onOpenTaskReport(item.task_id, item.report_id)}
                    >
                      <td className="min-w-0 px-3 py-2.5">
                        {item.title
                          ? <span className="block truncate text-[13px] font-medium text-foreground" title={item.title}>{item.title}</span>
                          : <span className="block truncate text-[12px] text-muted-foreground">{t('shell.reportsNoDraft')}</span>}
                        <span className="mt-0.5 block truncate font-mono text-[11px] text-muted-foreground">{item.report_id}</span>
                      </td>
                      <td className="min-w-0 px-3 py-2.5">
                        <span className="block truncate text-[12px] text-foreground" title={item.task_title}>{item.task_title || '—'}</span>
                      </td>
                      <td className="min-w-0 px-3 py-2.5">
                        {item.asset
                          ? <span className="block truncate text-[12px] text-foreground" title={item.asset.name}>{item.asset.name}</span>
                          : <span className="text-[12px] text-muted-foreground">{t('shell.reportsNoAsset')}</span>}
                      </td>
                      <td className="px-3 py-2.5 text-right tabular text-secondary-foreground">{item.item_count}</td>
                      <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[12px] tabular text-muted-foreground">{formatShortDateTime(item.created_at) || '—'}</td>
                      <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[12px] tabular text-muted-foreground">{formatShortDateTime(item.updated_at) || '—'}</td>
                      <td className="px-3 py-2.5" onClick={(event) => event.stopPropagation()}>
                        <div className="flex items-center justify-end gap-0.5">
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-xs"
                                aria-label={t('shell.reportsOpen')}
                                onClick={() => onOpenTaskReport(item.task_id, item.report_id)}
                                className="text-muted-foreground"
                              >
                                <ExternalLink className="h-3.5 w-3.5" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>{t('shell.reportsOpen')}</TooltipContent>
                          </Tooltip>
                          {exportButton(item, 'markdown', t('shell.reportsExportMd'), <FileDown className="h-3.5 w-3.5" />)}
                          {exportButton(item, 'json', t('shell.reportsExportJson'), <Braces className="h-3.5 w-3.5" />)}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </TableCard>
      </Page>
    </div>
  )
}
