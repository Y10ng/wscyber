import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Archive, ArchiveRestore, ArrowRight, Loader2, MoreHorizontal, Plus, RefreshCw } from 'lucide-react'
import {
  Badge, Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
  EmptyState, Input, Spinner, useConfirm,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { FilterPills, IconAction, Page, PageHeader, TableCard } from './PageFrame'
import { usePolling } from '../hooks/usePolling'
import { formatTokens } from '../lib/format-tokens'
import {
  listWebApiAssets,
  listWebApiSchedules,
  listWebApiTaskList,
  type WebApiSchedule,
  type WebApiTaskListCounts,
  type WebApiTaskListItem,
  type WebApiTaskListStatus,
} from '../api'

interface TasksPageProps {
  webapiEnabled: boolean
  onOpenTask: (sessionID: string) => void
  onCreateTask: () => void
  onOpenAsset: (assetId: string) => void
  onOpenMenu: () => void
  onArchive: (sessionId: string, archived: boolean) => Promise<void>
}

type TaskFilter = 'all' | Exclude<WebApiTaskListStatus, 'unknown'> | 'archived'
type PageMode = 'tasks' | 'schedules'

const PAGE_LIMIT = 50

function statusBadgeVariant(status: WebApiTaskListStatus): 'info' | 'success' | 'muted' {
  if (status === 'running') return 'info'
  if (status === 'idle') return 'success'
  return 'muted'
}

export default function TasksPage({ webapiEnabled, onOpenTask, onCreateTask, onOpenAsset, onOpenMenu, onArchive }: TasksPageProps) {
  const { t, i18n } = useTranslation('chat')
  const confirm = useConfirm()
  const [archivingId, setArchivingId] = useState<string | null>(null)
  const [mode, setMode] = useState<PageMode>('tasks')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<TaskFilter>('all')
  const [items, setItems] = useState<WebApiTaskListItem[]>([])
  const [counts, setCounts] = useState<WebApiTaskListCounts | null>(null)
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [schedules, setSchedules] = useState<WebApiSchedule[] | null>(null)
  const [scheduleError, setScheduleError] = useState('')
  const [assetNames, setAssetNames] = useState<Record<string, string>>({})
  const [loadingSchedules, setLoadingSchedules] = useState(false)
  const seq = useRef(0)
  const cursorRef = useRef<string | null>(null)

  // 搜索防抖 300ms：输入停顿后才传 q。
  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchInput.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [searchInput])

  const loadTasks = useCallback(async (opts: { reset: boolean; silent?: boolean }) => {
    if (!webapiEnabled) return
    const nextCursor = opts.reset ? undefined : cursorRef.current || undefined
    if (!opts.reset && !nextCursor) return
    const current = ++seq.current
    if (!opts.silent) {
      if (opts.reset) setLoading(true)
      else setLoadingMore(true)
    }
    try {
      const data = await listWebApiTaskList({
        q: query || undefined,
        status: filter === 'all' || filter === 'archived' ? undefined : filter,
        archived: filter === 'archived' || undefined,
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
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (current === seq.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    }
  }, [webapiEnabled, query, filter])

  // 条件变化时回到第一页。
  useEffect(() => {
    if (!webapiEnabled) { setLoading(false); return }
    if (mode !== 'tasks') return
    cursorRef.current = null
    void loadTasks({ reset: true })
  }, [mode, loadTasks, webapiEnabled])

  // 每 15 秒静默刷新第一页（不显示 loading，列表不闪烁）。
  usePolling(() => { void loadTasks({ reset: true, silent: true }) }, 15000, webapiEnabled && mode === 'tasks')

  const loadSchedules = useCallback(async () => {
    if (!webapiEnabled) return
    setLoadingSchedules(true)
    setScheduleError('')
    try {
      const [nextSchedules, assets] = await Promise.all([listWebApiSchedules(), listWebApiAssets(false)])
      setSchedules(nextSchedules)
      setAssetNames(Object.fromEntries(assets.map((asset) => [asset.id, asset.name])))
    } catch (err) {
      setScheduleError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoadingSchedules(false)
    }
  }, [webapiEnabled])

  useEffect(() => {
    if (mode === 'schedules') void loadSchedules()
  }, [mode, loadSchedules])

  const refresh = () => {
    if (mode === 'tasks') {
      cursorRef.current = null
      void loadTasks({ reset: true })
    } else {
      void loadSchedules()
    }
  }

  const now = Date.now()
  const formatCreated = (iso: string) => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return { day: iso, time: '' }
    return {
      day: date.toLocaleDateString(i18n.language, { year: 'numeric', month: '2-digit', day: '2-digit' }),
      time: date.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit', hour12: false }),
    }
  }
  const formatDateTime = (iso: string | null | undefined) => {
    if (!iso) return '—'
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    return date.toLocaleString(i18n.language, {
      year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
    })
  }
  const relativeTime = (iso: string) => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    const diff = now - date.getTime()
    if (diff < 60_000) return t('shell.relJustNow')
    if (diff < 3_600_000) return t('shell.relMinutes', { count: Math.floor(diff / 60_000) })
    if (diff < 86_400_000) return t('shell.relHours', { count: Math.floor(diff / 3_600_000) })
    return date.toLocaleDateString(i18n.language, { year: 'numeric', month: '2-digit', day: '2-digit' })
  }
  const statusLabel = (status: WebApiTaskListStatus) => t(`shell.status${status === 'not_started' ? 'NotStarted' : status === 'running' ? 'Running' : status === 'idle' ? 'Idle' : 'Unknown'}`)

  const filterItems = [
    { key: 'all' as const, label: t('shell.filterAll'), count: counts?.all, dot: undefined },
    { key: 'running' as const, label: t('shell.filterRunning'), count: counts?.running, dot: 'bg-info' },
    { key: 'idle' as const, label: t('shell.filterIdle'), count: counts?.idle, dot: 'bg-success' },
    { key: 'not_started' as const, label: t('shell.filterNotStarted'), count: counts?.not_started, dot: 'bg-muted-foreground' },
    { key: 'archived' as const, label: t('shell.archiveFilter'), count: counts?.archived, dot: undefined },
  ]

  // 归档/取消归档：确认后调用父级，完成后重取第一页与计数（静默，不闪列表）。
  const toggleArchive = async (item: WebApiTaskListItem) => {
    const next = !item.archived
    // 归档前确认（可找回）；取消归档无需确认。
    if (next && !(await confirm({
      title: t('shell.archiveTask'),
      description: t('shell.archiveConfirm'),
      confirmLabel: t('shell.archiveTask'),
    }))) return
    setArchivingId(item.task_id)
    try {
      await onArchive(item.initial_session_id, next)
      cursorRef.current = null
      await loadTasks({ reset: true, silent: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setArchivingId(null)
    }
  }

  const total = counts?.all ?? items.length
  const rangeText = t('shell.tableRange', { from: items.length === 0 ? 0 : 1, to: items.length, total })

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page>
        <PageHeader
          title={t('shell.navTasks')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <>
              <IconAction label={t('shell.refresh')} onClick={refresh} disabled={loading || loadingSchedules}>
                <RefreshCw className={cn('h-3.5 w-3.5', (loading || loadingSchedules) && 'animate-spin')} />
              </IconAction>
              {webapiEnabled && (
                <Button type="button" size="sm" className="h-9 gap-1.5" onClick={onCreateTask}>
                  <Plus className="h-4 w-4" />
                  {t('shell.newTask')}
                </Button>
              )}
            </>
          )}
        >
          <Input
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder={t('shell.searchTasks')}
            aria-label={t('shell.searchTasks')}
            className="h-9 w-[220px] sm:w-[320px]"
          />
          <div role="group" aria-label={t('shell.navTasks')} className="flex h-9 shrink-0 items-center gap-0.5 rounded-md border border-border p-0.5">
            {(['tasks', 'schedules'] as const).map((key) => (
              <button
                key={key}
                type="button"
                aria-pressed={mode === key}
                onClick={() => setMode(key)}
                className={cn(
                  'h-8 rounded px-3 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                  mode === key ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                )}
              >
                {key === 'tasks' ? t('shell.segmentTasks') : t('shell.segmentSchedules')}
              </button>
            ))}
          </div>
        </PageHeader>

        {mode === 'tasks' ? (
          <>
            <FilterPills items={filterItems} value={filter} onChange={setFilter} />
            {error && <p role="status" className="text-[12px] text-warning">{t('shell.loadFailed')}：{error}</p>}
            <TableCard
              footer={(
                <>
                  <span className="text-[12px] text-muted-foreground">{rangeText}</span>
                  {hasMore && (
                    <Button type="button" variant="outline" size="sm" className="ml-auto h-8" disabled={loadingMore} onClick={() => void loadTasks({ reset: false })}>
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
                <EmptyState
                  title={t('shell.emptyTasks')}
                  action={<Button type="button" size="sm" className="gap-1.5" onClick={onCreateTask}><Plus className="h-4 w-4" />{t('shell.newTask')}</Button>}
                />
              ) : (
                <div className="overflow-x-auto">
                <table className="w-full min-w-[860px] table-fixed border-collapse text-[13px]">
                  <colgroup>
                    <col className="w-10" />
                    <col className="w-[120px]" />
                    <col />
                    <col className="w-[100px]" />
                    <col className="w-[110px]" />
                    <col className="w-[90px]" />
                    <col className="w-[110px]" />
                    <col className="w-20" />
                  </colgroup>
                  <thead>
                    <tr className="h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                      <th className="whitespace-nowrap px-3 text-left font-medium">#</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colCreated')}</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colTask')}</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colStatus')}</th>
                      <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colFindings')}</th>
                      <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colTokens')}</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colLastActivity')}</th>
                      <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colActions')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {items.map((item, index) => {
                      const created = formatCreated(item.created_at)
                      return (
                        <tr
                          key={item.task_id}
                          className="h-12 cursor-pointer border-b border-border/70 transition-colors hover:bg-muted/40"
                          onClick={() => onOpenTask(item.initial_session_id)}
                        >
                          <td className="px-3 tabular-nums text-muted-foreground">{index + 1}</td>
                          <td className="px-3 leading-tight">
                            <div className="text-[12px] text-foreground">{created.day}</div>
                            <div className="font-mono text-[11px] text-muted-foreground">{created.time}</div>
                          </td>
                          <td className="min-w-0 px-3">
                            <div className="flex min-w-0 items-center gap-1.5">
                              {item.asset && <Badge variant="muted" size="sm" className="shrink-0 max-w-[8rem] truncate">{item.asset.name}</Badge>}
                              {item.scheduled && <Badge variant="outline" size="sm" className="shrink-0">{t('shell.scheduled')}</Badge>}
                              <span className="min-w-0 truncate text-[13px] font-medium text-foreground" title={item.title}>{item.title || item.task_id.slice(0, 8)}</span>
                            </div>
                            <div className="flex min-w-0 items-center gap-1.5 truncate font-mono text-[11px] text-muted-foreground" title={item.origins.join(', ')}>
                              <span className="truncate">{item.origins.join(', ')}</span>
                              {item.scenario && item.scenario.key !== 'webapi_pentest' && (
                                <span className="shrink-0 font-sans text-muted-foreground">{item.scenario.name}</span>
                              )}
                            </div>
                          </td>
                          <td className="px-3">
                            <Badge variant={statusBadgeVariant(item.status)} size="sm">{statusLabel(item.status)}</Badge>
                          </td>
                          <td className="px-3 text-right tabular-nums text-foreground">{item.finding_count} / {item.supported_count}</td>
                          <td className="px-3 text-right tabular-nums text-muted-foreground">{formatTokens(item.total_tokens)}</td>
                          <td className="px-3 text-[12px] text-muted-foreground">{relativeTime(item.last_activity_at)}</td>
                          <td className="px-3">
                            <div className="flex items-center justify-end gap-1">
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-xs"
                                aria-label={t('shell.openTask')}
                                title={t('shell.openTask')}
                                onClick={(event) => { event.stopPropagation(); onOpenTask(item.initial_session_id) }}
                                className="text-muted-foreground"
                              >
                                <ArrowRight className="h-3.5 w-3.5" />
                              </Button>
                              <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon-xs"
                                    aria-label={t('shell.colActions')}
                                    title={t('shell.colActions')}
                                    disabled={archivingId === item.task_id}
                                    onClick={(event) => event.stopPropagation()}
                                    className="text-muted-foreground"
                                  >
                                    <MoreHorizontal className="h-3.5 w-3.5" />
                                  </Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="end" onClick={(event) => event.stopPropagation()}>
                                  <DropdownMenuItem disabled={archivingId === item.task_id} onSelect={() => void toggleArchive(item)}>
                                    {item.archived
                                      ? <ArchiveRestore className="mr-2 h-3.5 w-3.5" />
                                      : <Archive className="mr-2 h-3.5 w-3.5" />}
                                    {t(item.archived ? 'shell.unarchiveTask' : 'shell.archiveTask')}
                                  </DropdownMenuItem>
                                </DropdownMenuContent>
                              </DropdownMenu>
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
          </>
        ) : (
          <>
            {scheduleError && <p role="status" className="text-[12px] text-warning">{t('shell.loadFailed')}：{scheduleError}</p>}
            <TableCard>
              {loadingSchedules && schedules === null ? (
                <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
                  <Spinner className="h-5 w-5" />
                  {t('shell.loading')}
                </div>
              ) : !schedules || schedules.length === 0 ? (
                <EmptyState title={t('shell.scheduleEmpty')} />
              ) : (
                <div className="overflow-x-auto">
                <table className="w-full min-w-[780px] table-fixed border-collapse text-[13px]">
                  <colgroup>
                    <col />
                    <col className="w-[160px]" />
                    <col className="w-[80px]" />
                    <col className="w-[160px]" />
                    <col className="w-[220px]" />
                    <col className="w-16" />
                  </colgroup>
                  <thead>
                    <tr className="h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.scheduleAsset')}</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.scheduleCron')}</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.scheduleStatus')}</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.scheduleNext')}</th>
                      <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.scheduleLast')}</th>
                      <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colActions')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {schedules.map((schedule) => {
                      const last = schedule.last_run
                      return (
                        <tr key={schedule.asset_id} className="h-12 border-b border-border/70 hover:bg-muted/40">
                          <td className="truncate px-3 text-foreground" title={assetNames[schedule.asset_id] || schedule.asset_id}>
                            {assetNames[schedule.asset_id] || schedule.asset_id}
                          </td>
                          <td className="px-3 font-mono text-[12px] text-foreground">{schedule.cron}</td>
                          <td className="px-3">
                            <Badge variant={schedule.enabled ? 'success' : 'muted'} size="sm">
                              {schedule.enabled ? t('shell.scheduleEnabled') : t('shell.scheduleDisabled')}
                            </Badge>
                          </td>
                          <td className="px-3 text-[12px] text-muted-foreground">{formatDateTime(schedule.next_run_at)}</td>
                          <td className="px-3">
                            {last ? (
                              <span className="flex items-center gap-1.5">
                                <Badge variant={last.status === 'launched' ? 'success' : last.status === 'failed' ? 'danger' : 'muted'} size="sm">
                                  {t(`shell.run${last.status === 'launched' ? 'Launched' : last.status === 'failed' ? 'Failed' : 'Skipped'}`)}
                                </Badge>
                                <span className="text-[12px] text-muted-foreground">{formatDateTime(last.scheduled_for)}</span>
                              </span>
                            ) : <span className="text-[12px] text-muted-foreground">—</span>}
                          </td>
                          <td className="px-3 text-right">
                            <Button
                              type="button"
                              variant="ghost"
                              size="icon-xs"
                              aria-label={t('shell.openAsset')}
                              title={t('shell.openAsset')}
                              onClick={() => onOpenAsset(schedule.asset_id)}
                              className="text-muted-foreground"
                            >
                              <ArrowRight className="h-3.5 w-3.5" />
                            </Button>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
                </div>
              )}
            </TableCard>
          </>
        )}
      </Page>
    </div>
  )
}