import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowRight, RefreshCw } from 'lucide-react'
import { Badge, Button, Callout, Spinner, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { IconAction, Page, PageHeader } from './PageFrame'
import { usePolling } from '../hooks/usePolling'
import { formatTokens } from '../lib/format-tokens'
import {
  getWebApiOverview,
  getWebApiTask,
  type WebApiOverview,
  type WebApiOverviewRecentSupported,
  type WebApiOverviewVulnDay,
  type WebApiVulnStatus,
} from '../api'
import type { AppPage } from '../App'

interface OverviewPageProps {
  onNavigate: (page: AppPage) => void
  onOpenTask: (sessionId: string) => void
  onOpenAsset: (assetId: string) => void
  onOpenUsage: () => void
  onOpenMenu: () => void
}

type Days = 7 | 30 | 90

const DAYS_STORAGE_KEY = 'wscyber.overview.days'
const DAYS_OPTIONS: Days[] = [7, 30, 90]
const VULN_STATUS_ORDER: WebApiVulnStatus[] = ['new', 'present', 'pending', 'unconfirmed', 'fixed']

const VULN_STATUS_META: Record<WebApiVulnStatus, { key: string; dot: string; bar: string }> = {
  new: { key: 'webapi.vulnStatusNew', dot: 'bg-destructive', bar: 'bg-destructive' },
  present: { key: 'webapi.vulnStatusPresent', dot: 'bg-warning', bar: 'bg-warning' },
  pending: { key: 'webapi.vulnStatusPending', dot: 'bg-info', bar: 'bg-info' },
  unconfirmed: { key: 'webapi.vulnStatusUnconfirmed', dot: 'bg-muted-foreground', bar: 'bg-muted-foreground' },
  fixed: { key: 'webapi.vulnStatusFixed', dot: 'bg-success', bar: 'bg-success' },
}

const TREND_LEGEND: Array<{ key: 'new' | 'present' | 'fixed'; label: string; color: string }> = [
  { key: 'new', label: 'webapi.vulnStatusNew', color: 'bg-destructive' },
  { key: 'present', label: 'webapi.vulnStatusPresent', color: 'bg-warning' },
  { key: 'fixed', label: 'webapi.vulnStatusFixed', color: 'bg-success' },
]

function getInitialDays(): Days {
  if (typeof window === 'undefined') return 30
  const value = Number(window.localStorage.getItem(DAYS_STORAGE_KEY))
  return value === 7 || value === 30 || value === 90 ? value : 30
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function shortDate(date: string): string {
  return date.length >= 10 ? date.slice(5, 10) : date
}

function Card({ title, actions, children, className, bodyClassName }: {
  title: string
  actions?: ReactNode
  children: ReactNode
  className?: string
  bodyClassName?: string
}) {
  return (
    <section className={cn('flex min-w-0 flex-col rounded-lg border border-border bg-card', className)}>
      <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-5">
        <h2 className="min-w-0 truncate text-[13px] font-medium text-foreground">{title}</h2>
        {actions && <div className="ml-auto flex shrink-0 items-center">{actions}</div>}
      </div>
      <div className={cn('min-w-0 flex-1 p-5', bodyClassName)}>{children}</div>
    </section>
  )
}

function CardLink({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-1 text-[12px] text-primary transition-colors hover:text-primary/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
    >
      {label}
      <ArrowRight className="h-3 w-3" />
    </button>
  )
}

function MetricCard({ label, value, sub, valueClassName, onClick }: {
  label: string
  value: string
  sub: string
  valueClassName?: string
  onClick?: () => void
}) {
  const interactive = Boolean(onClick)
  return (
    <div
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      onClick={onClick}
      onKeyDown={interactive ? (event) => {
        if (event.key === 'Enter') {
          event.preventDefault()
          onClick?.()
        }
      } : undefined}
      className={cn(
        'h-24 rounded-lg border border-border bg-card px-5 py-4',
        interactive && 'cursor-pointer transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
      )}
    >
      <div className="text-[12px] leading-4 text-muted-foreground">{label}</div>
      <div className={cn('mt-0.5 text-[28px] font-semibold leading-7 tabular-nums text-foreground', valueClassName)}>{value}</div>
      <div className="truncate text-[12px] leading-4 text-muted-foreground">{sub}</div>
    </div>
  )
}

/** 三线 y 轴（0 / 一半 / 最大值）；高度固定，数字在轴线左侧。 */
function YAxis({ max, height, format }: { max: number; height: number; format?: (value: number) => string }) {
  const fmt = format || ((value: number) => String(value))
  return (
    <div className="relative w-10 shrink-0 text-right text-[11px] tabular-nums text-muted-foreground" style={{ height }}>
      <span className="absolute right-0 top-0 -translate-y-1/2">{fmt(max)}</span>
      <span className="absolute right-0 top-1/2 -translate-y-1/2">{fmt(Math.ceil(max / 2))}</span>
      <span className="absolute bottom-0 right-0 translate-y-1/2">0</span>
    </div>
  )
}

/** 图区三条横虚线（0 / 一半 / 最大）；绝对定位铺满绘图区。 */
function GridLines() {
  return (
    <>
      {[0, 50, 100].map((top) => (
        <div key={top} aria-hidden className="absolute inset-x-0 border-t border-dashed border-border" style={{ top: `${top}%` }} />
      ))}
    </>
  )
}

function XAxisLabels({ dates }: { dates: string[] }) {
  if (dates.length === 0) return null
  const middle = dates[Math.floor((dates.length - 1) / 2)]
  return (
    <div className="flex justify-between text-[11px] tabular-nums text-muted-foreground">
      <span>{shortDate(dates[0])}</span>
      <span>{shortDate(middle)}</span>
      <span>{shortDate(dates[dates.length - 1])}</span>
    </div>
  )
}

function TooltipRow({ color, label, value }: { color: string; label: string; value: number }) {
  return (
    <span className="flex items-center gap-1.5">
      <span aria-hidden className={cn('h-2 w-2 rounded-sm', color)} />
      <span>{label}</span>
      <span className="ml-auto tabular-nums">{value}</span>
    </span>
  )
}

function VulnTrendBars({ data }: { data: WebApiOverviewVulnDay[] }) {
  const { t } = useTranslation('chat')
  const max = Math.max(1, ...data.map((item) => item.new + item.present + item.fixed))
  const dates = data.map((item) => item.date)
  if (data.length === 0) return null
  return (
    <div>
      <div className="flex gap-2">
        <YAxis max={max} height={180} />
        <div className="relative min-w-0 flex-1" style={{ height: 180 }}>
          <GridLines />
          <div className="relative flex h-full items-end gap-[2px]">
            {data.map((day) => {
              const total = day.new + day.present + day.fixed
              return (
                <Tooltip key={day.date}>
                  <TooltipTrigger asChild>
                    <div
                      tabIndex={0}
                      aria-label={`${day.date}: ${t('webapi.vulnStatusNew')} ${day.new}, ${t('webapi.vulnStatusPresent')} ${day.present}, ${t('webapi.vulnStatusFixed')} ${day.fixed}`}
                      className="flex h-full min-w-0 flex-1 justify-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                    >
                      <div className="flex w-full max-w-[24px] flex-col-reverse" style={{ height: `${(total / max) * 100}%` }}>
                        {total > 0 && (
                          <>
                            {day.new > 0 && <div className="bg-destructive" style={{ height: `${(day.new / total) * 100}%` }} />}
                            {day.present > 0 && <div className="bg-warning" style={{ height: `${(day.present / total) * 100}%` }} />}
                            {day.fixed > 0 && <div className="bg-success" style={{ height: `${(day.fixed / total) * 100}%` }} />}
                          </>
                        )}
                      </div>
                    </div>
                  </TooltipTrigger>
                  <TooltipContent side="top">
                    <div className="min-w-[150px] space-y-1 text-[12px]">
                      <div className="font-medium">{day.date}</div>
                      <TooltipRow color="bg-destructive" label={t('webapi.vulnStatusNew')} value={day.new} />
                      <TooltipRow color="bg-warning" label={t('webapi.vulnStatusPresent')} value={day.present} />
                      <TooltipRow color="bg-success" label={t('webapi.vulnStatusFixed')} value={day.fixed} />
                    </div>
                  </TooltipContent>
                </Tooltip>
              )
            })}
          </div>
        </div>
      </div>
      <div className="mt-1 flex gap-2">
        <div className="w-10 shrink-0" />
        <div className="min-w-0 flex-1">
          <XAxisLabels dates={dates} />
        </div>
      </div>
    </div>
  )
}

/** 单色每日柱（任务新建 / Token）；无 y 轴，tooltip 显示日期与数值。 */
function DailyBars({ data, height, tooltip }: {
  data: Array<{ date: string; value: number }>
  height: number
  tooltip: (date: string, value: number) => string
}) {
  const max = Math.max(1, ...data.map((item) => item.value))
  return (
    <div className="relative flex items-end gap-[2px]" style={{ height }}>
      {data.map((item) => (
        <Tooltip key={item.date}>
          <TooltipTrigger asChild>
            <div tabIndex={0} aria-label={tooltip(item.date, item.value)} className="flex h-full min-w-0 flex-1 items-end justify-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40">
              <div className="w-full max-w-[24px] bg-primary/70" style={{ height: `${(item.value / max) * 100}%` }} />
            </div>
          </TooltipTrigger>
          <TooltipContent side="top" className="text-[12px]">{tooltip(item.date, item.value)}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}

function RatioBar({ segments }: { segments: Array<{ value: number; color: string }> }) {
  const total = segments.reduce((sum, item) => sum + item.value, 0)
  return (
    <div className="flex h-2 overflow-hidden rounded-full bg-muted">
      {total > 0 && segments.map((item, index) => (
        item.value > 0
          ? <div key={index} className={cn('h-full', item.color)} style={{ width: `${(item.value / total) * 100}%` }} />
          : null
      ))}
    </div>
  )
}

function scheduleRunBadge(status: string): { label: string; variant: 'success' | 'danger' | 'muted' } {
  if (status === 'launched') return { label: 'shell.runLaunched', variant: 'success' }
  if (status === 'failed') return { label: 'shell.runFailed', variant: 'danger' }
  return { label: 'shell.runSkipped', variant: 'muted' }
}

export default function OverviewPage({ onNavigate, onOpenTask, onOpenAsset, onOpenUsage, onOpenMenu }: OverviewPageProps) {
  const { t, i18n } = useTranslation('chat')
  const [days, setDays] = useState<Days>(getInitialDays)
  const [data, setData] = useState<WebApiOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [openingRecent, setOpeningRecent] = useState<string | null>(null)
  const [actionError, setActionError] = useState('')
  const seq = useRef(0)

  const load = useCallback(async (silent: boolean) => {
    const current = ++seq.current
    if (!silent) {
      setLoading(true)
      setData(null)
      setError('')
    }
    try {
      const next = await getWebApiOverview(days)
      if (current !== seq.current) return
      setData(next)
      setError('')
    } catch (err) {
      if (current !== seq.current) return
      setError(errorText(err))
    } finally {
      if (current === seq.current) setLoading(false)
    }
  }, [days])

  useEffect(() => { void load(false) }, [load])
  // 60 秒静默轮询：不显示 loading，卡片不闪烁。
  usePolling(() => { void load(true) }, 60000, true)

  const changeDays = (next: Days) => {
    if (next === days) return
    window.localStorage.setItem(DAYS_STORAGE_KEY, String(next))
    setDays(next)
  }

  const formatDateTime = (iso: string | null | undefined) => {
    if (!iso) return '—'
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    return date.toLocaleString(i18n.language, {
      month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
    })
  }

  const openRecent = async (item: WebApiOverviewRecentSupported) => {
    setActionError('')
    setOpeningRecent(item.vuln_id)
    try {
      const task = await getWebApiTask(item.task_id)
      onOpenTask(task.initial_session_id)
    } catch (err) {
      setActionError(errorText(err))
    } finally {
      setOpeningRecent(null)
    }
  }

  const vulnCounts = data?.vulns.counts
  const vulnTotal = vulnCounts ? VULN_STATUS_ORDER.reduce((sum, status) => sum + (vulnCounts[status] || 0), 0) : 0
  const unfixedCount = vulnCounts ? (vulnCounts.new || 0) + (vulnCounts.present || 0) : 0
  const trendTotals = data ? TREND_LEGEND.reduce<Record<string, number>>((acc, item) => {
    acc[item.key] = data.vulns.confirmed_by_day.reduce((sum, day) => sum + (day[item.key] || 0), 0)
    return acc
  }, {}) : {}
  const trendEmpty = data ? data.vulns.confirmed_by_day.every((day) => day.new + day.present + day.fixed === 0) : false
  const tokensEmpty = data ? data.tokens.by_day.every((day) => day.total_tokens === 0) : false
  const schedulesEmpty = data ? data.schedules.next.length === 0 && data.schedules.recent_runs.length === 0 : false
  const taskOther = data ? Math.max(0, data.tasks.total - data.tasks.running) : 0
  const avgTokens = data ? Math.round(data.tokens.total / Math.max(1, data.days)) : 0

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page>
        <PageHeader
          title={t('shell.navOverview')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <IconAction label={t('shell.refresh')} onClick={() => void load(false)} disabled={loading}>
              <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
            </IconAction>
          )}
        >
          <div role="group" aria-label={t('shell.navOverview')} className="flex h-9 shrink-0 items-center gap-0.5 rounded-md border border-border p-0.5">
            {DAYS_OPTIONS.map((option) => (
              <button
                key={option}
                type="button"
                aria-pressed={days === option}
                onClick={() => changeDays(option)}
                className={cn(
                  'h-8 rounded px-3 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                  days === option ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                )}
              >
                {t(option === 7 ? 'shell.overviewDays7' : option === 90 ? 'shell.overviewDays90' : 'shell.overviewDays30')}
              </button>
            ))}
          </div>
        </PageHeader>

        {actionError && (
          <Callout tone="warning" onDismiss={() => setActionError('')}>{t('shell.loadFailed')}：{actionError}</Callout>
        )}

        {error && !loading ? (
          <Callout tone="destructive" icon={<span aria-hidden>!</span>}>
            <div className="flex flex-wrap items-center gap-3">
              <span>{t('shell.loadFailed')}：{error}</span>
              <Button type="button" variant="outline" size="sm" className="h-7" onClick={() => void load(false)}>
                {t('shell.overviewRetry')}
              </Button>
            </div>
          </Callout>
        ) : loading || !data ? (
          <div className="flex h-60 items-center justify-center gap-2 text-[13px] text-muted-foreground">
            <Spinner className="h-5 w-5" />
            {t('shell.loading')}
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
            <div className="col-span-full grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
              <MetricCard
                label={t('shell.overviewMetricAssets')}
                value={String(Math.max(0, data.assets.total - data.assets.archived))}
                sub={t('shell.overviewAssetsSub', { count: data.assets.scheduled_enabled })}
                onClick={() => onNavigate('assets')}
              />
              <MetricCard
                label={t('shell.overviewMetricTasks')}
                value={String(data.tasks.total)}
                sub={t('shell.overviewTasksSub', { running: data.tasks.running, created: data.tasks.created_in_range })}
                onClick={() => onNavigate('tasks')}
              />
              <MetricCard
                label={t('shell.overviewMetricVulns')}
                value={String(unfixedCount)}
                valueClassName={unfixedCount > 0 ? 'text-destructive' : undefined}
                sub={t('shell.overviewVulnsSub', { fixed: vulnCounts?.fixed || 0, pending: vulnCounts?.pending || 0 })}
              />
              <MetricCard
                label={t('shell.overviewMetricTokens')}
                value={formatTokens(data.tokens.total)}
                sub={t('shell.overviewTokensSub', { value: formatTokens(avgTokens) })}
                onClick={onOpenUsage}
              />
            </div>

            <Card
              title={t('shell.overviewTrendTitle')}
              className="col-span-full xl:col-span-2"
              actions={(
                <div className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
                  {TREND_LEGEND.map((item) => (
                    <span key={item.key} className="flex items-center gap-1.5">
                      <span aria-hidden className={cn('h-2 w-2 rounded-sm', item.color)} />
                      <span>{t(item.label)}</span>
                      <span className="tabular-nums text-foreground">{trendTotals[item.key] || 0}</span>
                    </span>
                  ))}
                </div>
              )}
            >
              {trendEmpty ? (
                <p className="text-[12px] text-muted-foreground">{t('shell.overviewNoVulnTrend')}</p>
              ) : (
                <VulnTrendBars data={data.vulns.confirmed_by_day} />
              )}
            </Card>

            <Card title={t('shell.overviewStatusTitle')}>
              {vulnTotal === 0 ? (
                <p className="text-[12px] text-muted-foreground">{t('shell.overviewNoVulns')}</p>
              ) : (
                <div className="space-y-4">
                  <RatioBar segments={VULN_STATUS_ORDER.map((status) => ({ value: vulnCounts?.[status] || 0, color: VULN_STATUS_META[status].bar }))} />
                  <div className="space-y-2">
                    {VULN_STATUS_ORDER.map((status) => {
                      const count = vulnCounts?.[status] || 0
                      const pct = vulnTotal > 0 ? (count / vulnTotal) * 100 : 0
                      return (
                        <div key={status} className="flex items-center gap-2 text-[12px]">
                          <span aria-hidden className={cn('h-2 w-2 shrink-0 rounded-full', VULN_STATUS_META[status].dot)} />
                          <span className="min-w-0 flex-1 truncate text-foreground">{t(VULN_STATUS_META[status].key)}</span>
                          <span className="w-10 text-right tabular-nums text-foreground">{count}</span>
                          <span className="w-12 text-right tabular-nums text-muted-foreground">{pct.toFixed(1)}%</span>
                        </div>
                      )
                    })}
                  </div>
                </div>
              )}
            </Card>

            <Card title={t('shell.overviewTaskTitle')} actions={<CardLink label={t('shell.overviewViewAll')} onClick={() => onNavigate('tasks')} />}>
              <div className="space-y-4">
                <div className="space-y-2">
                  <RatioBar segments={[
                    { value: data.tasks.running, color: 'bg-info' },
                    { value: taskOther, color: 'bg-muted-foreground' },
                  ]} />
                  <div className="flex items-center justify-between text-[12px] text-muted-foreground">
                    <span className="flex items-center gap-1.5"><span aria-hidden className="h-2 w-2 rounded-full bg-info" />{t('shell.overviewTasksRunning')} <span className="tabular-nums text-foreground">{data.tasks.running}</span></span>
                    <span className="flex items-center gap-1.5"><span aria-hidden className="h-2 w-2 rounded-full bg-muted-foreground" />{t('shell.overviewTasksOther')} <span className="tabular-nums text-foreground">{taskOther}</span></span>
                  </div>
                </div>
                <DailyBars
                  height={64}
                  data={data.tasks.by_day.map((day) => ({ date: day.date, value: day.count }))}
                  tooltip={(date, value) => t('shell.overviewTooltipTasks', { date, count: value })}
                />
              </div>
            </Card>

            <Card title={t('shell.overviewSchedulesTitle')}>
              {schedulesEmpty ? (
                <p className="text-[12px] text-muted-foreground">{t('shell.overviewNoSchedules')}</p>
              ) : (
                <div className="space-y-4">
                  <div>
                    <div className="mb-2 text-[12px] text-muted-foreground">{t('shell.overviewSchedulesNext')}</div>
                    {data.schedules.next.length === 0 ? (
                      <p className="text-[12px] text-muted-foreground">—</p>
                    ) : data.schedules.next.map((item) => (
                      <button
                        key={`${item.asset_id}-${item.next_run_at}`}
                        type="button"
                        onClick={() => onOpenAsset(item.asset_id)}
                        className="flex h-9 w-full items-center gap-2 rounded px-1 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                      >
                        <span className="min-w-0 flex-1 truncate text-[12px] text-foreground" title={item.asset_name}>{item.asset_name}</span>
                        <span className="shrink-0 text-[12px] text-muted-foreground">{formatDateTime(item.next_run_at)}</span>
                      </button>
                    ))}
                  </div>
                  <div>
                    <div className="mb-2 text-[12px] text-muted-foreground">{t('shell.overviewSchedulesRecent')}</div>
                    {data.schedules.recent_runs.length === 0 ? (
                      <p className="text-[12px] text-muted-foreground">—</p>
                    ) : data.schedules.recent_runs.map((item) => {
                      const badge = scheduleRunBadge(item.status)
                      return (
                        <button
                          key={`${item.asset_id}-${item.scheduled_for}`}
                          type="button"
                          onClick={() => onOpenAsset(item.asset_id)}
                          className="flex h-9 w-full items-center gap-2 rounded px-1 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                        >
                          <span className="min-w-0 flex-1 truncate text-[12px] text-foreground" title={item.asset_name}>{item.asset_name}</span>
                          <Badge variant={badge.variant} size="sm" className="shrink-0">{t(badge.label)}</Badge>
                          <span className="shrink-0 text-[12px] text-muted-foreground">{formatDateTime(item.scheduled_for)}</span>
                        </button>
                      )
                    })}
                  </div>
                </div>
              )}
            </Card>

            <Card title={t('shell.overviewRecentTitle')}>
              {data.recent_supported.length === 0 ? (
                <p className="text-[12px] text-muted-foreground">{t('shell.overviewNoRecent')}</p>
              ) : (
                <div className="space-y-1">
                  {data.recent_supported.map((item) => (
                    <button
                      key={item.vuln_id}
                      type="button"
                      disabled={openingRecent === item.vuln_id}
                      onClick={() => void openRecent(item)}
                      className="flex w-full flex-col rounded px-1 py-1.5 text-left transition-colors hover:bg-muted/40 disabled:opacity-60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                    >
                      <span className="w-full truncate text-[12px] text-foreground" title={item.title}>{item.title}</span>
                      <span className="w-full truncate text-[11px] text-muted-foreground">
                        {item.asset ? item.asset.name : t('shell.overviewNoAsset')} · {formatDateTime(item.at)}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </Card>

            <Card
              title={t('shell.overviewTokensTitle')}
              className="col-span-full"
              actions={<CardLink label={t('shell.overviewViewUsage')} onClick={onOpenUsage} />}
            >
              {tokensEmpty ? (
                <p className="text-[12px] text-muted-foreground">{t('shell.overviewNoUsage')}</p>
              ) : (
                <div>
                  <div className="flex gap-2">
                    <YAxis max={Math.max(1, ...data.tokens.by_day.map((day) => day.total_tokens))} height={140} format={formatTokens} />
                    <div className="relative min-w-0 flex-1" style={{ height: 140 }}>
                      <GridLines />
                      <div className="relative h-full">
                        <DailyBars
                          height={140}
                          data={data.tokens.by_day.map((day) => ({ date: day.date, value: day.total_tokens }))}
                          tooltip={(date, value) => t('shell.overviewTooltipTokens', { date, value: formatTokens(value) })}
                        />
                      </div>
                    </div>
                  </div>
                  <div className="mt-1 flex gap-2">
                    <div className="w-10 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <XAxisLabels dates={data.tokens.by_day.map((day) => day.date)} />
                    </div>
                  </div>
                </div>
              )}
            </Card>
          </div>
        )}
      </Page>
    </div>
  )
}
