import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type UIEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { FileDown, FileJson, Loader2, Plus, RefreshCw } from 'lucide-react'
import type { TrafficFlow } from '@cyber/aop'
import { aopFlowToHttpView, type TrafficHttpView } from '@cyber/traffic'
import { MarkdownContent } from '@/markdown'
import { cn } from '@cyber/theme'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import {
  exportWebApiReport,
  getWebApiEvidence,
  getWebApiReport,
  listWebApiReports,
  webApiReportExportFilename,
  type WebApiFindingEvidenceSnapshot,
  type WebApiEvidenceDetail,
  type WebApiReportDetail,
  type WebApiReportDraftItem,
  type WebApiReportDraftPayload,
  type WebApiReportItem,
  type WebApiReportItemDetail,
  type WebApiReportSummary,
  type WebApiTask,
} from '../api'
import type { ReportLaunchView } from '../hooks/useChatSession'
import { usePolling } from '../hooks/usePolling'
import { decodeFlow, EvidenceReference, formatTime } from './WebApiEvidenceReference'
import WebApiReportDialog, { type StartReport } from './WebApiReportDialog'
import './WebApiReportsPanel.css'

export type { StartReport }

interface Props {
  task: WebApiTask
  onStartReport: StartReport
  onViewExecution: (sessionID: string) => void
  reportLaunch: ReportLaunchView | null
  onDismissReportLaunch: () => void
  onResetReportLaunch: () => void
  onRetryReportLaunch: (items: WebApiReportItem[]) => void
  focusReportId: string | null
  onFocusConsumed: () => void
  /** 新建报告成功后通知 App bump 任务导航计数（fix-1 #5）。 */
  onReportStarted?: () => void
}

// 判读标签与验证工作面一致：支持/反证/证据不足 + “模型判读”；判读不确认漏洞。
function verdictKey(verdict: string | undefined): string {
  if (verdict === 'supported') return 'webapi.verdict_supported'
  if (verdict === 'counterevidence') return 'webapi.verdict_counterevidence'
  if (verdict === 'inconclusive') return 'webapi.verdict_inconclusive'
  return 'webapi.verdictPending'
}

function verdictTone(verdict: string | undefined): string {
  if (verdict === 'supported') return 'text-emerald-600 dark:text-emerald-400'
  if (verdict === 'counterevidence') return 'text-red-600 dark:text-red-400'
  if (verdict === 'inconclusive') return 'text-amber-600 dark:text-amber-400'
  return 'text-muted-foreground'
}

// 只读报告视图：左窄列表（时间倒序）+ 阅读区（目录 + 限行宽正文 + 导出）。
// 报告固定若干候选修订与所附验证；草稿由报告会话模型写入后只读展示，缺失按未提供处理。
export default function WebApiReportsPanel({ task, onStartReport, onViewExecution, reportLaunch, onDismissReportLaunch, onResetReportLaunch, onRetryReportLaunch, focusReportId, onFocusConsumed, onReportStarted }: Props) {
  const { t } = useTranslation('chat')
  const [items, setItems] = useState<WebApiReportSummary[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loadingList, setLoadingList] = useState(false)
  const [listError, setListError] = useState('')

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<WebApiReportDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState('')
  const [showDetail, setShowDetail] = useState(false)
  const [dialogOpen, setDialogOpen] = useState(false)
  // 追加7：Dialog「保存并启动」成功后回传新报告 ID，父级刷新列表并选中它。
  const pendingFocusReportId = useRef<string | null>(null)

  const [exporting, setExporting] = useState<'markdown' | 'json' | null>(null)
  const [exportError, setExportError] = useState('')
  const [launching, setLaunching] = useState(false)
  const [launchError, setLaunchError] = useState('')

  const listSeq = useRef(0)
  const detailSeq = useRef(0)
  const listScrollRef = useRef<HTMLDivElement>(null)
  const detailScrollRef = useRef<HTMLDivElement>(null)
  const listRegionRef = useRef<HTMLDivElement>(null)
  const detailRegionRef = useRef<HTMLDivElement>(null)
  const scrollPositions = useRef({ list: 0, detail: 0 })
  const visibleScrollPanels = useRef<{ list: HTMLDivElement | null; detail: HTMLDivElement | null }>({ list: null, detail: null })
  const pendingViewFocus = useRef<'list' | 'detail' | null>(null)

  const loadList = useCallback(async (cursor?: string) => {
    const seq = ++listSeq.current
    setLoadingList(true)
    setListError('')
    try {
      const result = await listWebApiReports(task.id, 25, cursor)
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

  const loadDetail = useCallback(async (reportId: string) => {
    const seq = ++detailSeq.current
    setDetailLoading(true)
    setDetailError('')
    setDetail(null)
    setExportError('')
    try {
      const result = await getWebApiReport(task.id, reportId)
      if (seq !== detailSeq.current) return
      setDetail(result)
    } catch (err) {
      if (seq !== detailSeq.current) return
      setDetail(null)
      setDetailError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === detailSeq.current) setDetailLoading(false)
    }
  }, [task.id])

  // 无草稿时静默轮询详情，直到 has_draft；轮询只读，不覆盖用户已选的其他报告。
  const refreshDetailQuiet = useCallback(async () => {
    const current = detail
    if (!current || current.has_draft || current.task_id !== task.id) return
    try {
      const fresh = await getWebApiReport(task.id, current.report_id)
      if (fresh.report_id !== current.report_id || fresh.task_id !== task.id) return
      setDetail((prev) => (prev && prev.report_id === fresh.report_id ? fresh : prev))
      setItems((prev) => prev.map((item) => (item.report_id === fresh.report_id ? { ...item, has_draft: fresh.has_draft } : item)))
    } catch { /* 轮询失败不覆盖已有；下一次再试 */ }
  }, [detail, task.id])
  usePolling(refreshDetailQuiet, 8000, !!detail && !detail.has_draft)

  // 切任务/卸载使 seq 失效并清空，迟到响应不再写状态。
  useEffect(() => {
    listSeq.current++
    detailSeq.current++
    setItems([])
    setHasMore(false)
    setNextCursor(null)
    setSelectedId(null)
    setDetail(null)
    setDetailError('')
    setShowDetail(false)
    scrollPositions.current = { list: 0, detail: 0 }
    pendingViewFocus.current = null
    void loadList()
  }, [loadList])
  useEffect(() => () => { listSeq.current++; detailSeq.current++ }, [])

  const rememberScroll = useCallback((panel: 'list' | 'detail', event: UIEvent<HTMLDivElement>) => {
    const element = event.currentTarget
    if (event.target === element && visibleScrollPanels.current[panel] === element && element.getClientRects().length) {
      scrollPositions.current[panel] = element.scrollTop
    }
  }, [])

  const restoreVisiblePanels = useCallback(() => {
    for (const panel of ['list', 'detail'] as const) {
      const element = panel === 'list' ? listScrollRef.current : detailScrollRef.current
      const visible = element && element.getClientRects().length ? element : null
      if (visible && visibleScrollPanels.current[panel] !== visible) {
        visible.scrollTop = scrollPositions.current[panel]
      }
      visibleScrollPanels.current[panel] = visible
    }
  }, [])

  useLayoutEffect(() => {
    const observer = new ResizeObserver(restoreVisiblePanels)
    if (listRegionRef.current) observer.observe(listRegionRef.current)
    if (detailRegionRef.current) observer.observe(detailRegionRef.current)
    restoreVisiblePanels()
    return () => observer.disconnect()
  }, [restoreVisiblePanels])

  useLayoutEffect(() => {
    restoreVisiblePanels()
    const target = pendingViewFocus.current
    pendingViewFocus.current = null
    if (target === 'list') listRegionRef.current?.focus({ preventScroll: true })
  }, [showDetail, detail, detailLoading, detailError, restoreVisiblePanels])

  const select = useCallback((item: WebApiReportSummary) => {
    const same = detail?.task_id === task.id && detail.report_id === item.report_id
    if (!same) {
      scrollPositions.current.detail = 0
      if (detailScrollRef.current) detailScrollRef.current.scrollTop = 0
    }
    pendingViewFocus.current = 'detail'
    setShowDetail(true)
    setSelectedId(item.report_id)
    if (!same) void loadDetail(item.report_id)
  }, [detail, loadDetail, task.id])

  const returnToList = useCallback(() => {
    pendingViewFocus.current = 'list'
    setShowDetail(false)
  }, [])

  // 「返回报告」定位：按 ID 直接选中/加载；404/错误由 loadDetail 就地显示。
  useEffect(() => {
    if (!focusReportId) return
    scrollPositions.current.detail = 0
    if (detailScrollRef.current) detailScrollRef.current.scrollTop = 0
    pendingViewFocus.current = 'detail'
    setShowDetail(true)
    setSelectedId(focusReportId)
    void loadDetail(focusReportId)
    onFocusConsumed()
  }, [focusReportId, loadDetail, onFocusConsumed])

  const handleExport = useCallback(async (format: 'markdown' | 'json') => {
    if (!detail?.has_draft || exporting) return
    setExporting(format)
    setExportError('')
    let url: string | undefined
    let anchor: HTMLAnchorElement | undefined
    try {
      const text = await exportWebApiReport(task.id, detail.report_id, format)
      url = URL.createObjectURL(new Blob([text], { type: format === 'markdown' ? 'text/markdown;charset=utf-8' : 'application/json' }))
      anchor = document.createElement('a')
      anchor.href = url
      anchor.download = webApiReportExportFilename(detail.report_id, format)
      document.body.appendChild(anchor)
      anchor.click()
    } catch (err) {
      setExportError(err instanceof Error ? err.message : String(err))
    } finally {
      anchor?.remove()
      if (url) URL.revokeObjectURL(url)
      setExporting(null)
    }
  }, [detail, exporting, task.id])

  const refresh = useCallback(() => {
    listSeq.current++
    detailSeq.current++
    setItems([])
    setHasMore(false)
    setNextCursor(null)
    setSelectedId(null)
    setDetail(null)
    setDetailError('')
    setShowDetail(false)
    scrollPositions.current = { list: 0, detail: 0 }
    pendingViewFocus.current = null
    void loadList()
  }, [loadList])

  // 追加7：Dialog 提交成功 → 关闭（Dialog 自己 onClose）→ 刷新列表并选中新报告。
  // 新报告无草稿时走既有等待态（PendingDraftBody + 8s 只读轮询）。
  const handleReportStarted = useCallback(async (report: WebApiReportSummary) => {
    pendingFocusReportId.current = report.report_id
    // 先重载列表（结果非空页），等 items 就绪后由 effect 选中并加载详情。
    listSeq.current++
    detailSeq.current++
    setShowDetail(false)
    setSelectedId(null)
    setDetail(null)
    setDetailError('')
    scrollPositions.current = { list: 0, detail: 0 }
    await loadList()
    onReportStarted?.()
    if (pendingFocusReportId.current) {
      const id = pendingFocusReportId.current
      pendingFocusReportId.current = null
      scrollPositions.current.detail = 0
      pendingViewFocus.current = 'detail'
      setShowDetail(true)
      setSelectedId(id)
      void loadDetail(id)
    }
  }, [loadList, loadDetail, onReportStarted])

  const canExport = !!detail?.has_draft

  // 未清除的冻结发起身份期间不得另起新建（Dialog 的新选择会被 hook 拒绝）；只有
  // canStartFresh（原 ID 确认未保存且未进入 Open/Run）才允许「重新开始」清除后新建。
  const launchPending = reportLaunch?.status === 'pending'
  const newReportDisabled = launchPending || (!!reportLaunch && !reportLaunch.canStartFresh)
  const retryLaunch = useCallback(() => {
    if (!reportLaunch || launching) return
    setLaunching(true)
    setLaunchError('')
    void (async () => {
      try {
        await onStartReport({ taskID: task.id, items: reportLaunch.items })
      } catch (err) {
        setLaunchError(err instanceof Error ? err.message : String(err))
      } finally {
        setLaunching(false)
      }
    })()
  }, [reportLaunch, launching, onStartReport, task.id])

  return (
    <div className="reports-container min-h-0 min-w-0 flex-1">
      <div className={cn('reports-workspace', showDetail && 'reports-show-detail')}>
        <div ref={listRegionRef} tabIndex={-1} className="reports-list flex min-h-0 min-w-0 flex-col border-border/60">
          <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border/60 px-3">
            <span className="flex min-w-0 items-center gap-2 text-[13px] font-medium text-foreground">
              {t('webapi.reports')}
              <span className="font-mono text-[11px] tabular-nums text-muted-foreground">{items.length}{hasMore ? '+' : ''}</span>
            </span>
            <span className="flex shrink-0 items-center gap-1">
            <Button
              type="button"
              size="xs"
              onClick={() => {
                // canStartFresh：先清除冻结身份再开 Dialog，否则提交会被 hook 的
                // 「items 不同」守卫拒绝；其它未决状态按钮本身禁用，不会走到这里。
                if (reportLaunch?.status === 'error' && reportLaunch.canStartFresh) onResetReportLaunch()
                setDialogOpen(true)
              }}
              disabled={newReportDisabled}
              className="gap-1 text-[12px]"
            >
              <Plus className="h-3.5 w-3.5" />
              {reportLaunch?.status === 'error' && reportLaunch.canStartFresh ? t('webapi.reportStartFresh') : t('webapi.newReport')}
            </Button>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button type="button" variant="ghost" size="icon-xs" onClick={refresh} disabled={loadingList} aria-label={t('webapi.refresh')} className="text-muted-foreground">
                  {loadingList ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                </Button>
              </TooltipTrigger>
              <TooltipContent>{t('webapi.refresh')}</TooltipContent>
            </Tooltip>
            </span>
          </div>

          {launchError && !reportLaunch && (
            <div role="alert" className="border-b border-border/60 px-3 py-1.5 text-xs text-destructive">{t('webapi.reportLaunchFailed')}：{launchError}</div>
          )}

          {reportLaunch && !reportLaunch.dismissed && (
            <div className="flex items-start justify-between gap-2 border-b border-border/60 px-3 py-2 text-xs">
              {reportLaunch.status === 'pending' ? (
                <span className="flex min-w-0 items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" />
                  {t('webapi.reportLaunching')}
                </span>
              ) : (
                <span className="min-w-0 text-destructive">{t('webapi.reportLaunchFailed')}：{reportLaunch.error || launchError}</span>
              )}
              <span className="flex shrink-0 items-center gap-1">
                {reportLaunch.status === 'error' && !reportLaunch.canStartFresh && (
                  <Button type="button" variant="ghost" size="xs" onClick={retryLaunch} disabled={launching} className="text-muted-foreground">{t('webapi.reportRetry')}</Button>
                )}
                {reportLaunch.status === 'error' && reportLaunch.canStartFresh && (
                  <Button type="button" variant="ghost" size="xs" onClick={onResetReportLaunch} className="text-muted-foreground">{t('webapi.reportRestartFresh')}</Button>
                )}
                {/* error 且非 canStartFresh：无重试出口时不得隐藏错误条（新建仍禁用），
                    不提供「关闭」；dismissed 只用于 pending 或 canStartFresh 的情况。 */}
                {!(reportLaunch.status === 'error' && !reportLaunch.canStartFresh) && (
                  <Button type="button" variant="ghost" size="xs" onClick={onDismissReportLaunch} className="text-muted-foreground">{t('webapi.close')}</Button>
                )}
              </span>
            </div>
          )}

          <div ref={listScrollRef} onScroll={(event) => rememberScroll('list', event)} className="min-h-0 flex-1 overflow-auto">
            {loadingList && items.length === 0 ? (
              <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
            ) : listError ? (
              <div className="px-3 py-4 text-center text-sm text-destructive">{listError}</div>
            ) : items.length === 0 ? (
              <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.reportsEmpty')}</div>
            ) : (
              <div className="divide-y divide-border/40">
                {items.map((item) => (
                  <button
                    key={item.report_id}
                    type="button"
                    onClick={() => select(item)}
                    aria-pressed={selectedId === item.report_id}
                    className={cn('flex w-full min-w-0 flex-col gap-1 px-3 py-2 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40', selectedId === item.report_id && 'bg-primary/10 ring-1 ring-inset ring-primary/40')}
                  >
                    <span className="reports-list-title text-[13px] leading-[1.4] text-foreground">
                      {item.title?.trim() || (item.has_draft ? `${t('webapi.reportUntitled')} ${item.report_id.slice(0, 8)}` : t('webapi.reportDraftPending'))}
                    </span>
                    <span className="flex w-full items-center justify-between gap-2 text-[12px] text-muted-foreground">
                      <span>{t('webapi.reportItemsCount', { count: item.items.length })}</span>
                      <span>{formatTime(item.created_at)}</span>
                    </span>
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

        <div ref={detailRegionRef} className="reports-detail flex min-h-0 min-w-0 flex-col overflow-hidden">
          <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border/60 px-3 py-1.5">
            <Button type="button" variant="ghost" size="xs" onClick={returnToList} className="reports-back shrink-0">
              {t('webapi.returnToReports')}
            </Button>
            <div className="flex min-w-0 items-center gap-1">
              <Button type="button" variant="outline" size="xs" disabled={!canExport || !!exporting} onClick={() => void handleExport('markdown')} className="gap-1 text-[12px]">
                <FileDown className="h-3.5 w-3.5" />
                {exporting === 'markdown' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                {t('webapi.reportDownloadMarkdown')}
              </Button>
              <Button type="button" variant="outline" size="xs" disabled={!canExport || !!exporting} onClick={() => void handleExport('json')} className="gap-1 text-[12px]">
                <FileJson className="h-3.5 w-3.5" />
                {exporting === 'json' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                {t('webapi.reportDownloadJson')}
              </Button>
              {detail && (
                <Button type="button" variant="outline" size="xs" onClick={() => onViewExecution(detail.session_id)} className="gap-1 text-[12px]">
                  {t('webapi.verificationViewExecution')}
                </Button>
              )}
            </div>
          </div>
          {exportError && (
            <div role="alert" className="shrink-0 border-b border-border/60 px-3 py-1.5 text-xs text-destructive">{t('webapi.reportExportFailed')}：{exportError}</div>
          )}
          {selectedId == null ? (
            <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">{t('webapi.selectReport')}</div>
          ) : detailLoading ? (
            <div className="flex flex-1 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : detailError ? (
            <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-destructive">
              <span>{t('webapi.reportLoadFailed')}</span>
              <span className="text-xs text-muted-foreground">{detailError}</span>
            </div>
          ) : detail ? (
            <ReportReading key={detail.report_id} detail={detail} origins={task.origins} />
          ) : null}
        </div>
      </div>

      <WebApiReportDialog
        open={dialogOpen}
        task={task}
        onClose={() => setDialogOpen(false)}
        onSubmit={onStartReport}
        onStarted={handleReportStarted}
      />
    </div>
  )
}

// 阅读区：目录（概述/各候选/局限）+ 限行宽正文。无草稿时显示固定来源与等待提示。
function ReportReading({ detail, origins }: { detail: WebApiReportDetail; origins?: string[] }) {
  const { t } = useTranslation('chat')
  const draft = detail.draft?.payload
  const sectionRefs = useRef(new Map<string, HTMLElement>())

  const registerSection = useCallback((id: string, el: HTMLElement | null) => {
    if (el) sectionRefs.current.set(id, el)
    else sectionRefs.current.delete(id)
  }, [])

  const jump = useCallback((id: string) => {
    sectionRefs.current.get(id)?.scrollIntoView({ block: 'start' })
  }, [])

  const tocItems = useMemo(() => detail.items
    .map((item, index) => ({
      id: `item-${index}`,
      label: `${item.revision?.number || t('webapi.untitledFinding')} ${item.revision?.content?.title || ''}`.trim(),
    })), [detail.items, t])

  return (
    <div className="reports-reading">
      <nav aria-label={t('webapi.reportToc')} className="reports-toc min-h-0 flex-col gap-0.5 overflow-auto p-3">
        <span className="mb-1 text-[12px] font-medium text-muted-foreground">{t('webapi.reportToc')}</span>
        <TocButton label={t('webapi.reportOverview')} onClick={() => jump('overview')} />
        {tocItems.map((item) => (
          <TocButton key={item.id} label={item.label} onClick={() => jump(item.id)} />
        ))}
        <TocButton label={t('webapi.limitations')} onClick={() => jump('limitations')} />
      </nav>
      <div className="min-h-0 min-w-0 overflow-auto">
        <div className="reports-reading-column px-4 py-4">
          <header className="reports-reading-width mb-4 border-b border-border/60 pb-3">
            <h2 className="text-[16px] font-semibold leading-[1.4] text-foreground">{draft?.title || t('webapi.reportUntitled')}</h2>
            <div className="mt-1 font-mono text-[12px] tabular-nums text-muted-foreground">
              {detail.report_id} · {formatTime(detail.created_at)} · {t('webapi.reportItemsCount', { count: detail.items.length })}
            </div>
          </header>

          {draft ? (
            <DraftBody detail={detail} draft={draft} registerSection={registerSection} origins={origins} />
          ) : (
            <PendingDraftBody detail={detail} registerSection={registerSection} origins={origins} />
          )}
        </div>
      </div>
    </div>
  )
}

function TocButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="truncate rounded px-1.5 py-1 text-left text-[12px] text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      title={label}
    >
      {label}
    </button>
  )
}
function Section({ id, label, children, registerSection, wide = false }: { id?: string; label?: string; children: ReactNode; registerSection?: (id: string, el: HTMLElement | null) => void; wide?: boolean }) {
  return (
    <section
      ref={id && registerSection ? (el) => registerSection(id, el) : undefined}
      className={cn('mb-5 scroll-mt-4', !wide && 'reports-reading-width')}
    >
      {label && <h3 className="mb-1.5 text-[12px] font-medium text-muted-foreground">{label}</h3>}
      {children}
    </section>
  )
}

function MissingText() {
  const { t } = useTranslation('chat')
  return <p className="text-[14px] italic text-muted-foreground">{t('webapi.reportMissing')}</p>
}

// 逐候选正文：编号/标题/修订号来自固定修订；描述/复现/修复/证据引用来自草稿（缺失
// 写“未提供”）；验证判读来自所附验证。证据展开读取本任务真实原件，复用 EvidenceReference。
function DraftItemBody({ taskID, draftItem, item, origins }: { taskID: string; draftItem?: WebApiReportDraftItem; item: WebApiReportItemDetail; origins?: string[] }) {
  const { t } = useTranslation('chat')
  const revision = item.revision
  const verifications = item.verifications ?? []

  const [expandedEvidenceId, setExpandedEvidenceId] = useState<string | null>(null)
  const [evidenceDetail, setEvidenceDetail] = useState<WebApiEvidenceDetail | null>(null)
  const [evidenceFlow, setEvidenceFlow] = useState<TrafficFlow | null>(null)
  const [evidenceLoading, setEvidenceLoading] = useState(false)
  const [evidenceError, setEvidenceError] = useState('')
  const evidenceSeq = useRef(0)
  const evidenceView = useMemo(() => (evidenceFlow ? aopFlowToHttpView(evidenceFlow) : null), [evidenceFlow])

  useEffect(() => () => { evidenceSeq.current++ }, [])

  const toggleEvidence = useCallback((evidenceId: string) => {
    if (expandedEvidenceId === evidenceId) {
      evidenceSeq.current++
      setExpandedEvidenceId(null)
      setEvidenceDetail(null)
      setEvidenceFlow(null)
      setEvidenceLoading(false)
      setEvidenceError('')
      return
    }
    const seq = ++evidenceSeq.current
    setExpandedEvidenceId(evidenceId)
    setEvidenceLoading(true)
    setEvidenceError('')
    setEvidenceDetail(null)
    setEvidenceFlow(null)
    void (async () => {
      try {
        const loaded = await getWebApiEvidence(taskID, evidenceId)
        if (seq !== evidenceSeq.current) return
        const decoded = decodeFlow(loaded.flow_protobuf_base64)
        if (seq !== evidenceSeq.current) return
        setEvidenceDetail(loaded)
        setEvidenceFlow(decoded)
      } catch (err) {
        if (seq !== evidenceSeq.current) return
        setEvidenceDetail(null)
        setEvidenceFlow(null)
        setEvidenceError(err instanceof Error ? err.message : String(err))
      } finally {
        if (seq === evidenceSeq.current) setEvidenceLoading(false)
      }
    })()
  }, [expandedEvidenceId, taskID])

  // 草稿 evidence_ids 只能指向固定修订或所附验证的证据快照；按快照解析后就近展示，
  // 解析不到的 ID 如实列出，不冒充原件。
  const evidenceMap = useMemo(() => {
    const map = new Map<string, WebApiFindingEvidenceSnapshot>()
    for (const snapshot of revision?.evidence ?? []) map.set(snapshot.evidence_id, snapshot)
    for (const verification of verifications) {
      for (const snapshot of verification.result?.evidence ?? []) map.set(snapshot.evidence_id, snapshot)
    }
    return map
  }, [revision, verifications])

  const description = draftItem?.description?.trim()
  const reproduction = draftItem?.reproduction?.trim()
  const remediation = draftItem?.remediation?.trim()
  const evidenceIds = useMemo(
    () => [...new Set((draftItem?.evidence_ids ?? []).filter((id) => typeof id === 'string' && id))],
    [draftItem],
  )

  return (
    <div>
      {/* 追加8：编号 + 标题 + 版本徽章同一行（不再小号重复一行）。 */}
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="font-mono text-[12px] tabular-nums text-muted-foreground">{revision?.number || '—'}</span>
        <h4 className="text-[16px] font-semibold leading-[1.4] text-foreground">{revision?.content?.title || t('webapi.untitledFinding')}</h4>
        <span className="rounded border border-border/60 px-1.5 py-px text-[12px] text-muted-foreground">
          {t('webapi.revision')} {revision ? revision.revision : '—'}{draftItem ? '' : ` · ${t('webapi.reportDraftItemMissing')}`}
        </span>
      </div>

      <Section label={t('webapi.description')}>
        {description ? <MarkdownContent content={description} className="text-[14px]" /> : <MissingText />}
      </Section>

      <Section label={t('webapi.steps')}>
        {reproduction ? <MarkdownContent content={reproduction} className="text-[14px]" /> : <MissingText />}
      </Section>

      <Section label={t('webapi.evidenceRefs')} wide>
        {evidenceIds.length === 0 ? (
          <MissingText />
        ) : (
          <ol className="space-y-1.5">
            {evidenceIds.map((evidenceId, index) => {
              const snapshot = evidenceMap.get(evidenceId)
              if (!snapshot) {
                return (
                  <li key={evidenceId} className="rounded-md border border-border/60 px-2.5 py-1.5 text-[12px] text-muted-foreground">
                    {t('webapi.reportEvidenceUnresolved')}：{evidenceId}
                  </li>
                )
              }
              const expanded = expandedEvidenceId === evidenceId
              return (
                <EvidenceReference
                  key={evidenceId}
                  index={index}
                  item={snapshot}
                  expanded={expanded}
                  loading={expanded && evidenceLoading}
                  error={expanded ? evidenceError : ''}
                  view={expanded ? evidenceView : null}
                  detail={expanded ? evidenceDetail : null}
                  onToggle={() => toggleEvidence(evidenceId)}
                  registerEvidenceRef={() => {}}
                  origins={origins}
                />
              )
            })}
          </ol>
        )}
      </Section>

      <Section label={t('webapi.remediation')}>
        {remediation ? <MarkdownContent content={remediation} className="text-[14px]" /> : <MissingText />}
      </Section>

      <Section label={t('webapi.verification')} wide>
        {verifications.length === 0 ? (
          <MissingText />
        ) : (
          <div className="space-y-2">
            {verifications.map((verification) => (
              <div key={verification.verification_id} className="rounded-md border border-border/60 px-2.5 py-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className={cn('text-[13px] font-medium', verdictTone(verification.verdict))}>{t(verdictKey(verification.verdict))}</span>
                  <span className="text-[12px] text-muted-foreground">{t('webapi.verdictModelLabel')}</span>
                  <span className="ml-auto text-[12px] text-muted-foreground">{formatTime(verification.created_at)}</span>
                </div>
                {verification.result ? (
                  <div className="mt-1.5 space-y-1">
                    {verification.result.payload.summary && <p className="whitespace-pre-wrap text-[14px] leading-[1.65] text-foreground">{verification.result.payload.summary}</p>}
                    {verification.result.payload.limitations && <p className="whitespace-pre-wrap text-[12px] text-muted-foreground">{t('webapi.verificationLimitations')}：{verification.result.payload.limitations}</p>}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        )}
      </Section>
    </div>
  )
}

// 有草稿：概述 + 逐候选（草稿文本用 MarkdownContent 渲染）+ 局限。
function DraftBody({ detail, draft, registerSection, origins }: { detail: WebApiReportDetail; draft: WebApiReportDraftPayload; registerSection: (id: string, el: HTMLElement | null) => void; origins?: string[] }) {
  const { t } = useTranslation('chat')
  const draftByFinding = useMemo(() => {
    const map = new Map<string, WebApiReportDraftItem>()
    for (const item of draft.items ?? []) if (item?.finding_id) map.set(item.finding_id, item)
    return map
  }, [draft])

  return (
    <article>
      <Section id="overview" label={t('webapi.reportOverview')} registerSection={registerSection}>
        {draft.overview?.trim() ? <MarkdownContent content={draft.overview} className="text-[14px]" /> : <MissingText />}
      </Section>

      {detail.items.map((item, index) => (
        <Section
          key={item.finding_id}
          id={`item-${index}`}
          registerSection={registerSection}
        >
          <DraftItemBody taskID={detail.task_id} draftItem={draftByFinding.get(item.finding_id)} item={item} origins={origins} />
        </Section>
      ))}

      <Section id="limitations" label={t('webapi.limitations')} registerSection={registerSection}>
        {draft.limitations?.trim() ? <MarkdownContent content={draft.limitations} className="text-[14px]" /> : <MissingText />}
      </Section>
    </article>
  )
}

// 无草稿：固定来源（各候选修订 + 所附验证判读）+ 等待提示；导出按钮在上方禁用。
function PendingDraftBody({ detail, registerSection, origins }: { detail: WebApiReportDetail; registerSection: (id: string, el: HTMLElement | null) => void; origins?: string[] }) {
  const { t } = useTranslation('chat')
  return (
    <article>
      <div className="reports-reading-width mb-5 rounded-md border border-border/60 px-3 py-2 text-sm text-muted-foreground">
        {t('webapi.reportWaitingDraft')}
      </div>
      <Section id="overview" label={t('webapi.reportOverview')} registerSection={registerSection}>
        <MissingText />
      </Section>
      {detail.items.map((item, index) => (
        <Section
          key={item.finding_id}
          id={`item-${index}`}
          registerSection={registerSection}
        >
          <DraftItemBody taskID={detail.task_id} item={item} origins={origins} />
        </Section>
      ))}
      <Section id="limitations" label={t('webapi.limitations')} registerSection={registerSection}>
        <MissingText />
      </Section>
    </article>
  )
}
