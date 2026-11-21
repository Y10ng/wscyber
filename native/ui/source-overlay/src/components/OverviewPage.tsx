import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  AlertTriangle, ArrowRight, CheckCircle2, ChevronDown, ChevronRight, HelpCircle, Plus, Radar,
  RefreshCw, WifiOff,
} from 'lucide-react'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { Page, PageHeader } from './PageFrame'
import { SeverityBadge } from './ui-badges'
import { usePolling } from '../hooks/usePolling'
import { formatTokens } from '../lib/format-tokens'
import {
  getWebApiOverview,
  getWebApiTask,
  type WebApiOverview,
  type WebApiOverviewRecentSupported,
  type WebApiOverviewRunningTask,
  type WebApiOverviewVulnDay,
} from '../api'
import type { AppPage } from '../App'

interface OverviewPageProps {
  onNavigate: (page: AppPage) => void
  onOpenTask: (sessionId: string) => void
  onOpenAsset: (assetId: string) => void
  onOpenAssetVuln: (assetId: string, vulnId: string) => void
  onOpenTaskFinding: (taskId: string, findingId: string) => void
  /** K1：打开漏洞卡（最近确认、逾期 Top5）。 */
  onOpenVuln: (key: string) => void
  onOpenUsage: () => void
  /** 漏洞页：一次性筛出逾期条目。 */
  onOpenVulnsOverdue: () => void
  /** 漏洞页：一次性筛出未定级条目。 */
  onOpenVulnsUnrated: () => void
  /** 集成页：一次性筛出“待确认”推送。 */
  onOpenIntegrationsPending: () => void
  /** 打开侧栏现有待审批 Popover。 */
  onOpenApprovals: () => void
  pendingApprovalCount: number
  onNewTask: () => void
  onOpenMenu: () => void
}

type Days = 7 | 30 | 90

const DAYS_STORAGE_KEY = 'wscyber.overview.days'
const AI_STORAGE_KEY = 'wscyber.overview.aiResourceExpanded'
const DAYS_OPTIONS: Days[] = [7, 30, 90]

const TODO_ACCENT: Record<'unrated' | 'push' | 'approval' | 'overdue', string> = {
  unrated: 'text-destructive',
  push: 'text-primary',
  approval: 'text-warning',
  overdue: 'text-destructive',
}

const SEVERITY_META = [
  { key: 'critical', labelKey: 'shell.triageSeverityCritical', text: 'var(--sev-critical-text)' },
  { key: 'high', labelKey: 'shell.triageSeverityHigh', text: 'var(--sev-high-text)' },
  { key: 'medium', labelKey: 'shell.triageSeverityMedium', text: 'var(--sev-medium-text)' },
  { key: 'low', labelKey: 'shell.triageSeverityLow', text: 'var(--sev-low-text)' },
] as const

const TREND_SERIES = [
  { key: 'new' as const, labelKey: 'overview.trendNew', color: '#E11D48', fill: 'rgba(225, 29, 72, 0.09)' },
  { key: 'present' as const, labelKey: 'overview.trendPresent', color: '#D97706', fill: 'rgba(217, 119, 6, 0.08)' },
  { key: 'fixed' as const, labelKey: 'overview.trendFixed', color: '#059669', fill: 'rgba(5, 150, 105, 0.08)' },
]

function getInitialDays(): Days {
  if (typeof window === 'undefined') return 30
  const value = Number(window.localStorage.getItem(DAYS_STORAGE_KEY))
  return value === 7 || value === 30 || value === 90 ? value : 30
}

function getInitialAiExpanded(): boolean {
  if (typeof window === 'undefined') return true
  return window.localStorage.getItem(AI_STORAGE_KEY) !== '0'
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function pad(value: number): string {
  return String(value).padStart(2, '0')
}

/** 短时间：MM-DD HH:mm（本地时区，24 小时制）。 */
function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** 同步时间：HH:mm:ss（本地时区）。 */
function formatClock(iso: string | null | undefined): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '—'
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

function formatElapsed(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const rest = total % 60
  if (hours > 0) return `${hours}h${pad(minutes)}m`
  if (minutes > 0) return `${minutes}m${pad(rest)}s`
  return `${rest}s`
}

function shortDate(date: string): string {
  return date.length >= 10 ? date.slice(5, 10) : date
}

function CardLink({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex shrink-0 items-center gap-1 text-[11px] font-medium text-primary transition-colors hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
    >
      {label}
      <ArrowRight className="h-3 w-3" aria-hidden />
    </button>
  )
}

function HelpTip({ text, stopPropagation }: { text: string; stopPropagation?: boolean }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span
          tabIndex={0}
          role="img"
          aria-label={text}
          onClick={stopPropagation ? (event) => event.stopPropagation() : undefined}
          onKeyDown={stopPropagation ? (event) => event.stopPropagation() : undefined}
          className="inline-flex shrink-0 items-center text-muted-foreground/70 transition-colors hover:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <HelpCircle className="h-3.5 w-3.5" aria-hidden />
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-[260px] text-[12px]">{text}</TooltipContent>
    </Tooltip>
  )
}

function SectionCard({ title, actions, children, className, bodyClassName }: {
  title: string
  actions?: ReactNode
  children: ReactNode
  className?: string
  bodyClassName?: string
}) {
  return (
    <section className={cn('flex min-w-0 flex-col rounded-lg border border-border bg-card p-4', className)}>
      <div className="mb-3 flex min-w-0 items-center justify-between gap-2">
        <h2 className="min-w-0 truncate text-[12px] font-semibold text-foreground">{title}</h2>
        {actions && <div className="flex shrink-0 items-center gap-3">{actions}</div>}
      </div>
      <div className={cn('min-w-0 flex-1', bodyClassName)}>{children}</div>
    </section>
  )
}

function CardError({ text, retry, onRetry }: { text: string; retry: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 py-8 text-center">
      <WifiOff className="h-6 w-6 text-destructive" aria-hidden />
      <div className="text-xs font-medium text-foreground">{text}</div>
      <Button type="button" variant="outline" size="sm" className="h-7 gap-1.5" onClick={onRetry}>
        <RefreshCw className="h-3 w-3" />
        {retry}
      </Button>
    </div>
  )
}

function Skel({ className }: { className?: string }) {
  return <div className={cn('animate-pulse rounded bg-muted', className)} />
}

function OverviewSkeleton() {
  return (
    <div className="space-y-5">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="rounded-lg border border-border bg-card p-3.5">
            <Skel className="h-3 w-24" />
            <Skel className="mt-3 h-6 w-10" />
          </div>
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="rounded-lg border border-border bg-card p-4 lg:col-span-2">
          <Skel className="h-3.5 w-32" />
          <Skel className="mt-4 h-52 w-full" />
        </div>
        <div className="rounded-lg border border-border bg-card p-4">
          <Skel className="h-3.5 w-28" />
          <Skel className="mt-4 h-12 w-full" />
          <Skel className="mt-3 h-40 w-full" />
        </div>
      </div>
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

function TrendLegend() {
  const { t } = useTranslation('chat')
  return (
    <div className="flex flex-wrap items-center justify-end gap-3 text-[11px] text-muted-foreground">
      {TREND_SERIES.map((series) => (
        <span key={series.key} className="flex items-center gap-1.5">
          <span aria-hidden className="h-2 w-2 rounded-full" style={{ background: series.color }} />
          {t(series.labelKey)}
        </span>
      ))}
    </div>
  )
}

/**
 * 堆叠折线/面积图（设计稿 Chart.js line + stacked 的等价实现，无新依赖）：
 * 三条线为累计值（新发现 / +仍存在 / +已修复），线与线之间填半透明面积。
 */
function TrendChart({ data }: { data: WebApiOverviewVulnDay[] }) {
  const { t } = useTranslation('chat')
  const W = 720
  const H = 200
  const dates = data.map((item) => item.date)
  const levels = data.map((item) => [item.new, item.new + item.present, item.new + item.present + item.fixed])
  const max = Math.max(1, ...levels.flat())
  const point = (index: number, value: number) => {
    const x = data.length > 1 ? (index / (data.length - 1)) * W : W / 2
    const y = H - (value / max) * H
    return [x, y] as const
  }
  const linePath = (series: number) => levels.map((values, index) => {
    const [x, y] = point(index, values[series])
    return `${index === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
  }).join(' ')
  const bandPath = (upper: number, lower: number | null) => {
    const top = levels.map((values, index) => {
      const [x, y] = point(index, values[upper])
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
    }).join(' ')
    const bottom = lower === null
      ? `L${W},${H} L0,${H}`
      : levels.map((values, index) => {
        const [x, y] = point(index, values[lower])
        return `L${x.toFixed(2)},${y.toFixed(2)}`
      }).reverse().join(' ')
    return `${top} ${bottom} Z`
  }

  return (
    <div>
      <TrendLegend />
      <div className="mt-2 flex gap-2">
        <YAxis max={max} height={200} />
        <div className="relative min-w-0 flex-1" style={{ height: 200 }}>
          <GridLines />
          {data.length > 0 && (
            <svg
              viewBox={`0 0 ${W} ${H}`}
              preserveAspectRatio="none"
              aria-hidden
              className="absolute inset-0 h-full w-full overflow-visible"
            >
              {[0, 1, 2].map((index) => (
                <path
                  key={TREND_SERIES[index].key}
                  d={bandPath(index, index === 0 ? null : index - 1)}
                  fill={TREND_SERIES[index].fill}
                  stroke="none"
                />
              ))}
              {[0, 1, 2].map((index) => (
                <path
                  key={TREND_SERIES[index].key}
                  d={linePath(index)}
                  fill="none"
                  stroke={TREND_SERIES[index].color}
                  strokeWidth={2}
                  strokeLinejoin="round"
                  vectorEffect="non-scaling-stroke"
                />
              ))}
            </svg>
          )}
          {data.length > 0 && (
            <div className="absolute inset-0 flex">
              {data.map((day) => (
                <Tooltip key={day.date}>
                  <TooltipTrigger asChild>
                    <div
                      tabIndex={0}
                      aria-label={t('overview.trendTooltip', { date: day.date, new: day.new, present: day.present, fixed: day.fixed })}
                      className="min-w-0 flex-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                    />
                  </TooltipTrigger>
                  <TooltipContent side="top">
                    <div className="min-w-[150px] space-y-1 text-[12px]">
                      <div className="font-medium">{day.date}</div>
                      {TREND_SERIES.map((series) => (
                        <span key={series.key} className="flex items-center gap-1.5">
                          <span aria-hidden className="h-2 w-2 rounded-sm" style={{ background: series.color }} />
                          <span>{t(series.labelKey)}</span>
                          <span className="ml-auto tabular-nums">{day[series.key]}</span>
                        </span>
                      ))}
                    </div>
                  </TooltipContent>
                </Tooltip>
              ))}
            </div>
          )}
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

/** 单色每日柱（Token）；tooltip 显示日期与数值。 */
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
              <div className="w-full max-w-[24px] rounded-t-sm bg-primary" style={{ height: `${(item.value / max) * 100}%` }} />
            </div>
          </TooltipTrigger>
          <TooltipContent side="top" className="text-[12px]">{tooltip(item.date, item.value)}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}

export default function OverviewPage({
  onNavigate, onOpenTask, onOpenAsset, onOpenAssetVuln, onOpenTaskFinding, onOpenVuln, onOpenUsage,
  onOpenVulnsOverdue, onOpenVulnsUnrated, onOpenIntegrationsPending, onOpenApprovals,
  pendingApprovalCount, onNewTask, onOpenMenu,
}: OverviewPageProps) {
  const { t } = useTranslation('chat')
  const [days, setDays] = useState<Days>(getInitialDays)
  const [data, setData] = useState<WebApiOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [openingTask, setOpeningTask] = useState<string | null>(null)
  const [actionError, setActionError] = useState('')
  const [aiExpanded, setAiExpanded] = useState(getInitialAiExpanded)
  const seq = useRef(0)
  const hasData = useRef(false)

  const load = useCallback(async (opts: { initial?: boolean; manual?: boolean } = {}) => {
    const current = ++seq.current
    if (opts.initial && !hasData.current) {
      setLoading(true)
      setError('')
    }
    if (opts.manual) setRefreshing(true)
    try {
      const next = await getWebApiOverview(days)
      if (current !== seq.current) return
      hasData.current = true
      setData(next)
      setError('')
    } catch (err) {
      if (current !== seq.current) return
      setError(errorText(err))
    } finally {
      if (current === seq.current) {
        setLoading(false)
        setRefreshing(false)
      }
    }
  }, [days])

  // 初次进入与切换区间都走静默加载：已有数据时保留旧内容，不闪烁。
  useEffect(() => { void load({ initial: true }) }, [load])
  // 15 秒静默刷新：不显示 loading，卡片不闪烁。
  usePolling(() => { void load() }, 15000)

  const changeDays = (next: Days) => {
    if (next === days) return
    window.localStorage.setItem(DAYS_STORAGE_KEY, String(next))
    setDays(next)
  }

  const toggleAi = () => {
    setAiExpanded((value) => {
      window.localStorage.setItem(AI_STORAGE_KEY, value ? '0' : '1')
      return !value
    })
  }

  const openTaskByID = async (taskId: string) => {
    setActionError('')
    setOpeningTask(taskId)
    try {
      const task = await getWebApiTask(taskId)
      onOpenTask(task.initial_session_id)
    } catch (err) {
      setActionError(errorText(err))
    } finally {
      setOpeningTask(null)
    }
  }

  const openRecent = (item: WebApiOverviewRecentSupported) => {
    onOpenVuln(item.vuln_id)
  }

  const relativeDay = (iso: string) => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return '—'
    const now = new Date()
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    const that = new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime()
    const diff = Math.round((start - that) / 86400000)
    if (diff <= 0) return t('overview.today')
    if (diff === 1) return t('overview.yesterday')
    if (diff < 7) return t('overview.daysAgo', { count: diff })
    return formatDateTime(iso).slice(0, 5)
  }

  const retry = () => { void load({ initial: true }) }
  const refreshingNow = loading || refreshing

  const header = (
    <PageHeader
      title={t('overview.title')}
      onOpenMenu={onOpenMenu}
      menuLabel={t('shell.openMenu')}
      actions={(
        <>
          <Button type="button" variant="outline" size="sm" className="h-8 gap-1.5" onClick={() => void load({ manual: true })} disabled={refreshingNow}>
            <RefreshCw className={cn('h-3.5 w-3.5', refreshingNow && 'animate-spin')} />
            {t('overview.refresh')}
          </Button>
          <Button type="button" size="sm" className="h-8 gap-1.5" onClick={onNewTask}>
            <Plus className="h-4 w-4" />
            {t('overview.newTask')}
          </Button>
        </>
      )}
    >
      <div role="group" aria-label={t('overview.title')} className="flex h-8 shrink-0 items-center gap-0.5 rounded-md border border-border bg-muted/40 p-0.5">
        {DAYS_OPTIONS.map((option) => (
          <button
            key={option}
            type="button"
            aria-pressed={days === option}
            onClick={() => changeDays(option)}
            className={cn(
              'h-7 rounded px-2.5 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
              days === option ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {t(option === 7 ? 'overview.days7' : option === 90 ? 'overview.days90' : 'overview.days30')}
          </button>
        ))}
      </div>
      <span className="hidden text-xs text-muted-foreground sm:inline">
        {t('overview.lastSync', { time: formatClock(data?.generated_at) })}
      </span>
    </PageHeader>
  )

  if (loading && !data) {
    return (
      <Page contentClassName="max-w-[1600px] space-y-5">
        {header}
        <OverviewSkeleton />
      </Page>
    )
  }

  if (!data) {
    // 单接口失败：每张卡内显示“加载失败 · 重试”。
    const cardError = <CardError text={t('overview.loadFailed')} retry={t('overview.retry')} onRetry={retry} />
    return (
      <Page contentClassName="max-w-[1600px] space-y-5">
        {header}
        {error && (
          <div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-2 text-xs text-destructive">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
            <span className="min-w-0 truncate">{t('overview.loadFailed')}：{error}</span>
          </div>
        )}
        <div className="space-y-2">
          <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
            <span className="inline-block h-4 w-1.5 rounded-full bg-primary" />
            {t('overview.todoTitle')}
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {[t('overview.todoUnrated'), t('overview.todoPendingPush'), t('overview.todoApproval'), t('overview.todoOverdue')].map((label) => (
              <div key={label} className="rounded-lg border border-border bg-card px-3.5 py-3">
                <div className="truncate text-xs font-medium text-muted-foreground">{label}</div>
                {cardError}
              </div>
            ))}
          </div>
        </div>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <SectionCard title={t('overview.riskTitle')} className="lg:col-span-2">{cardError}</SectionCard>
          <SectionCard title={t('overview.slaTitle')}>{cardError}</SectionCard>
        </div>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          <SectionCard title={t('overview.runsTitle')}>{cardError}</SectionCard>
          <SectionCard title={t('overview.coverageTitle')}>{cardError}</SectionCard>
          <SectionCard title={t('overview.recentTitle')}>{cardError}</SectionCard>
        </div>
        <SectionCard title={t('overview.aiTitle')}>{cardError}</SectionCard>
      </Page>
    )
  }

  const todo = data.todo
  const vulnCounts = data.vulns.counts
  const vulnTotal = (['new', 'present', 'pending', 'unconfirmed', 'fixed'] as const).reduce((sum, key) => sum + (vulnCounts[key] || 0), 0)
  const todoAllZero = todo.unrated === 0 && todo.pending_push === 0 && pendingApprovalCount === 0 && todo.overdue === 0
  const emptyDeployment = data.assets.total === 0 && data.tasks.total === 0 && vulnTotal === 0
  const trendEmpty = data.vulns.confirmed_by_day.every((day) => day.new + day.present + day.fixed === 0)
  const tokensEmpty = data.tokens.by_day.every((day) => day.total_tokens === 0)
  const failed = data.recent_failed_runs[0]
  const slaRate = data.sla.rate === null ? null : `${(data.sla.rate * 100).toFixed(1)}%`
  const avgTokens = Math.round(data.tokens.total / Math.max(1, data.days))
  const coveragePct = data.coverage.total > 0 ? `${((data.coverage.tested / data.coverage.total) * 100).toFixed(1)}%` : '—'

  const todoCards = [
    {
      key: 'unrated',
      label: t('overview.todoUnrated'),
      hint: t('overview.todoUnratedHint'),
      count: todo.unrated,
      accent: TODO_ACCENT.unrated,
      onClick: onOpenVulnsUnrated,
    },
    {
      key: 'push',
      label: t('overview.todoPendingPush'),
      hint: t('overview.todoPendingPushHint'),
      count: todo.pending_push,
      accent: TODO_ACCENT.push,
      onClick: onOpenIntegrationsPending,
    },
    {
      key: 'approval',
      label: t('overview.todoApproval'),
      hint: t('overview.todoApprovalHint'),
      count: pendingApprovalCount,
      accent: TODO_ACCENT.approval,
      onClick: onOpenApprovals,
    },
    {
      key: 'overdue',
      label: t('overview.todoOverdue'),
      hint: t('overview.todoOverdueHint'),
      count: todo.overdue,
      accent: TODO_ACCENT.overdue,
      urgent: true,
      onClick: onOpenVulnsOverdue,
    },
  ]

  return (
    <Page contentClassName="max-w-[1600px] space-y-5">
      {header}
      {actionError && (
        <div className="flex items-center gap-2 rounded-lg border border-warning/40 bg-warning/10 px-4 py-2 text-xs text-warning">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span className="min-w-0 truncate">{errorText(actionError)}</span>
        </div>
      )}

      {/* A 待我处理 */}
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-2">
          <span className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
            <span className="inline-block h-4 w-1.5 rounded-full bg-primary" />
            {t('overview.todoTitle')}
          </span>
          <HelpTip text={t('overview.todoHint')} />
        </div>
        {todoAllZero ? (
          <div className="flex items-center justify-between rounded-lg border border-border bg-card px-4 py-3 text-xs">
            <span className="flex items-center gap-2 font-medium text-success">
              <CheckCircle2 className="h-4 w-4" aria-hidden />
              {t('overview.todoClear')}
            </span>
            <span className="text-muted-foreground">{t('overview.todoClearHint')}</span>
          </div>
        ) : (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {todoCards.map((card) => (
              <button
                key={card.key}
                type="button"
                onClick={card.onClick}
                className={cn(
                  'flex min-w-0 flex-col justify-between gap-2 rounded-lg border bg-card px-3.5 py-3 text-left transition-colors hover:border-primary/60 hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                  card.urgent ? 'border-destructive/40' : 'border-border',
                  card.count === 0 && 'opacity-50',
                )}
              >
                <span className="flex w-full min-w-0 items-center justify-between gap-1.5">
                  <span className="flex min-w-0 items-center gap-1.5 truncate text-xs font-medium text-muted-foreground">
                    {card.urgent && <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-destructive" aria-hidden />}
                    <span className="truncate">{card.label}</span>
                  </span>
                  <HelpTip text={card.hint} stopPropagation />
                </span>
                <span className={cn('text-2xl font-bold leading-none tabular-nums', card.accent)}>{card.count}</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {emptyDeployment ? (
        <div className="rounded-lg border border-border bg-card p-12 text-center">
          <div className="mx-auto flex h-11 w-11 items-center justify-center rounded-full bg-muted text-muted-foreground">
            <Radar className="h-5 w-5" aria-hidden />
          </div>
          <div className="mt-3 text-sm font-semibold text-foreground">{t('overview.emptyTitle')}</div>
          <p className="mx-auto mt-1 max-w-md text-xs text-muted-foreground">{t('overview.emptyHint')}</p>
          <div className="mt-3 flex flex-wrap items-center justify-center gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => onNavigate('assets')}>
              {t('overview.importAssets')}
            </Button>
            <Button type="button" size="sm" className="gap-1.5" onClick={onNewTask}>
              <Plus className="h-3.5 w-3.5" />
              {t('overview.createFirstTask')}
            </Button>
          </div>
        </div>
      ) : (
        <>
          {/* B 风险态势 + C 修复进度与 SLA */}
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
            <SectionCard
              title={t('overview.riskTitle')}
              className="lg:col-span-2"
              actions={<CardLink label={t('overview.viewAll')} onClick={() => onNavigate('vulns')} />}
              bodyClassName="flex flex-col gap-4"
            >
              <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-4">
                {SEVERITY_META.map((severity) => (
                  <div
                    key={severity.key}
                    className="rounded-md border border-border border-l-[3px] bg-muted/30 px-3 py-2.5"
                    style={{ borderLeftColor: severity.text }}
                  >
                    <div className="flex items-center gap-1.5 text-[11px] font-medium text-muted-foreground">
                      <span aria-hidden className="h-1.5 w-1.5 rounded-full" style={{ background: severity.text }} />
                      <span className="truncate">{t(severity.labelKey)}</span>
                    </div>
                    <div className="mt-0.5 text-xl font-bold tabular-nums text-foreground">
                      {data.vulns.open_by_severity?.[severity.key] || 0}
                    </div>
                  </div>
                ))}
              </div>
              {trendEmpty ? (
                <p className="py-10 text-center text-xs text-muted-foreground">{t('overview.trendEmpty')}</p>
              ) : (
                <TrendChart data={data.vulns.confirmed_by_day} />
              )}
            </SectionCard>

            <SectionCard
              title={t('overview.slaTitle')}
              actions={<CardLink label={t('overview.viewAll')} onClick={() => onNavigate('vulns')} />}
              bodyClassName="flex flex-col gap-3"
            >
              <div className="flex items-baseline justify-between gap-2">
                <div>
                  <div className="text-[11px] text-muted-foreground">{t('overview.slaRate')}</div>
                  <div className="mt-0.5 text-2xl font-bold tabular-nums text-foreground">{slaRate ?? '—'}</div>
                  {slaRate === null && <div className="text-[11px] text-muted-foreground">{t('overview.slaNoSamples')}</div>}
                </div>
              </div>
              <div className="grid grid-cols-4 gap-1.5 text-center">
                {SEVERITY_META.map((severity) => {
                  const value = data.sla.avg_fix_days?.[severity.key]
                  return (
                    <div key={severity.key} className="rounded bg-muted/50 px-1.5 py-1.5">
                      <div className="text-[10.5px] text-muted-foreground">{t(severity.labelKey)}</div>
                      <div className="mt-0.5 text-xs font-semibold tabular-nums text-foreground">
                        {value === null || value === undefined ? '—' : t('overview.avgFixValue', { value: value.toFixed(1) })}
                      </div>
                    </div>
                  )
                })}
              </div>
              <div className="space-y-1 border-t border-border pt-2">
                <div className="flex items-center justify-between text-[11px] font-medium text-muted-foreground">
                  <span>{t('overview.overdueTop')}</span>
                  <span>{t('overview.overdueDaysHeader')}</span>
                </div>
                {data.sla.overdue_top.length === 0 ? (
                  <p className="py-2 text-[11px] text-muted-foreground">{t('overview.noOverdue')}</p>
                ) : data.sla.overdue_top.map((item) => (
                  <button
                    key={item.vuln_key}
                    type="button"
                    onClick={() => onOpenVuln(item.vuln_key)}
                    className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                  >
                    <span className="min-w-0 flex-1 truncate text-xs text-foreground" title={item.title}>{item.title}</span>
                    <span className="shrink-0 text-[11px] text-muted-foreground">{item.owner_effective || '—'}</span>
                    <span className="shrink-0 text-[11px] font-semibold tabular-nums text-destructive">
                      {t('overview.overdueDays', { count: item.overdue_days })}
                    </span>
                  </button>
                ))}
              </div>
            </SectionCard>
          </div>

          {/* D 测试运行 · E 资产覆盖 · F 最近确认 */}
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            <SectionCard
              title={t('overview.runsTitle')}
              actions={<CardLink label={t('overview.viewAll')} onClick={() => onNavigate('tasks')} />}
              bodyClassName="flex flex-col gap-3"
            >
              {data.running.length === 0 ? (
                <p className="text-xs text-muted-foreground">{t('overview.noRunning')}</p>
              ) : data.running.map((task: WebApiOverviewRunningTask) => {
                const coverage = task.board.endpoints > 0 ? Math.min(100, (task.board.covered_endpoints / task.board.endpoints) * 100) : 0
                return (
                  <button
                    key={task.task_id}
                    type="button"
                    disabled={openingTask === task.task_id}
                    onClick={() => void openTaskByID(task.task_id)}
                    className="rounded-md border border-border bg-muted/30 p-2.5 text-left transition-colors hover:border-primary/50 disabled:opacity-60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                  >
                    <div className="flex items-center justify-between gap-2 text-xs">
                      <span className="flex min-w-0 items-center gap-1.5 font-medium text-foreground">
                        <span aria-hidden className="h-2 w-2 shrink-0 animate-pulse rounded-full bg-primary" />
                        <span className="truncate">{task.title}</span>
                      </span>
                      <span className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">{formatElapsed(task.elapsed_seconds)}</span>
                    </div>
                    <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-muted">
                      <div className="h-full rounded-full bg-primary" style={{ width: `${coverage}%` }} />
                    </div>
                    <div className="mt-1.5 flex items-center justify-between gap-2 text-[11px] text-muted-foreground">
                      <span className="min-w-0 truncate">{task.asset?.name || t('overview.noAsset')}</span>
                      <span className="shrink-0 tabular-nums">
                        {t('overview.boardCoverage', { covered: task.board.covered_endpoints, total: task.board.endpoints })}
                      </span>
                    </div>
                  </button>
                )
              })}

              {failed && (
                <div
                  className="flex items-start justify-between gap-2 rounded-md border p-2.5 text-xs"
                  style={{ background: 'var(--sev-critical-bg)', borderColor: 'var(--sev-critical-border)', color: 'var(--sev-critical-text)' }}
                >
                  <div className="min-w-0 space-y-0.5">
                    <div className="truncate font-semibold" title={failed.asset_name}>
                      {t('overview.recentFailed', { name: failed.asset_name })}
                    </div>
                    <div className="truncate text-[11px] opacity-85" title={failed.error}>
                      {failed.error || failed.status} · {t('overview.failedAt', { time: formatDateTime(failed.scheduled_for) })}
                    </div>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-6 shrink-0 px-2 text-[11px]"
                    onClick={() => onOpenAsset(failed.asset_id)}
                  >
                    {t('overview.failedView')}
                  </Button>
                </div>
              )}

              <div className="space-y-1 border-t border-border pt-2 text-xs">
                <div className="text-[11px] text-muted-foreground">{t('overview.nextSchedules')}</div>
                {data.schedules.next.length === 0 ? (
                  <p className="text-[11px] text-muted-foreground">{t('overview.noNextSchedules')}</p>
                ) : data.schedules.next.map((item) => (
                  <button
                    key={`${item.asset_id}-${item.next_run_at}`}
                    type="button"
                    onClick={() => onOpenAsset(item.asset_id)}
                    className="flex w-full items-center justify-between gap-2 rounded px-1 py-0.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                  >
                    <span className="min-w-0 truncate text-xs text-foreground" title={item.asset_name}>{item.asset_name}</span>
                    <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{formatDateTime(item.next_run_at)}</span>
                  </button>
                ))}
              </div>
            </SectionCard>

            <SectionCard
              title={t('overview.coverageTitle')}
              actions={<CardLink label={t('overview.viewAll')} onClick={() => onNavigate('assets')} />}
              bodyClassName="flex flex-col gap-3"
            >
              <div className="space-y-1.5">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="text-xl font-bold tabular-nums text-foreground">{coveragePct}</span>
                  <span className="text-xs tabular-nums text-muted-foreground">
                    {t('overview.coverageTested', { tested: data.coverage.tested, total: data.coverage.total })}
                  </span>
                </div>
                <div className="h-2 overflow-hidden rounded-full bg-muted">
                  <div className="h-full rounded-full bg-success" style={{ width: `${data.coverage.total > 0 ? (data.coverage.tested / data.coverage.total) * 100 : 0}%` }} />
                </div>
              </div>
              <div className="space-y-1 border-t border-border pt-2 text-xs">
                <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                  <span>{t('overview.staleTop')}</span>
                  <span>{t('overview.staleHeader')}</span>
                </div>
                {data.coverage.stale_top.length === 0 ? (
                  <p className="py-2 text-[11px] text-muted-foreground">{t('overview.noStale')}</p>
                ) : data.coverage.stale_top.map((item) => (
                  <button
                    key={item.asset_id}
                    type="button"
                    onClick={() => onOpenAsset(item.asset_id)}
                    className="flex w-full items-center justify-between gap-2 rounded px-1 py-0.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                  >
                    <span className="min-w-0 truncate text-xs text-foreground" title={item.name}>{item.name}</span>
                    <span className={cn('shrink-0 text-[11px] tabular-nums', item.days_since === 0 ? 'text-muted-foreground' : 'text-warning')}>
                      {item.days_since === null
                        ? t('overview.neverTested')
                        : item.days_since === 0
                          ? t('overview.testedToday')
                          : t('overview.daysNotTested', { count: item.days_since })}
                    </span>
                  </button>
                ))}
              </div>
            </SectionCard>

            <SectionCard
              title={t('overview.recentTitle')}
              actions={<CardLink label={t('overview.viewAll')} onClick={() => onNavigate('vulns')} />}
            >
              {data.recent_supported.length === 0 ? (
                <p className="text-xs text-muted-foreground">{t('overview.recentEmpty')}</p>
              ) : (
                <div className="divide-y divide-border">
                  {data.recent_supported.map((item) => (
                    <button
                      key={item.vuln_id}
                      type="button"
                      onClick={() => openRecent(item)}
                      className="flex w-full items-center justify-between gap-2 rounded px-1 py-2 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                    >
                      <span className="flex min-w-0 items-center gap-2">
                        {item.severity
                          ? <SeverityBadge severity={item.severity} className="shrink-0 text-[10.5px]" />
                          : item.suggested_severity
                            ? <SeverityBadge severity={item.suggested_severity} suggested compact className="shrink-0 text-[10.5px]" />
                            : <SeverityBadge severity="" className="shrink-0 text-[10.5px]" />}
                        <span className="min-w-0">
                          <span className="block truncate text-xs font-medium text-foreground" title={item.title}>{item.title}</span>
                          <span className="block truncate text-[11px] text-muted-foreground">{item.asset?.name || '—'}</span>
                        </span>
                      </span>
                      <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{relativeDay(item.at)}</span>
                    </button>
                  ))}
                </div>
              )}
            </SectionCard>
          </div>

          {/* G AI 资源（可折叠） */}
          <section className="overflow-hidden rounded-lg border border-border bg-card">
            <button
              type="button"
              aria-expanded={aiExpanded}
              onClick={toggleAi}
              className="flex w-full items-center justify-between gap-2 px-4 py-2.5 text-xs transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            >
              <span className="flex items-center gap-2 font-medium text-muted-foreground">
                {aiExpanded ? <ChevronDown className="h-3.5 w-3.5" aria-hidden /> : <ChevronRight className="h-3.5 w-3.5" aria-hidden />}
                {t('overview.aiTitle')}
              </span>
              <span className="text-xs tabular-nums text-muted-foreground">
                {t('overview.aiTotal', { count: data.days })}
                <strong className="text-foreground">{t('overview.aiTotalValue', { value: formatTokens(data.tokens.total) })}</strong>
              </span>
            </button>
            {aiExpanded && (
              <div className="grid grid-cols-1 gap-4 border-t border-border p-4 lg:grid-cols-3">
                <div className="min-w-0 lg:col-span-2">
                  {tokensEmpty ? (
                    <p className="py-10 text-center text-xs text-muted-foreground">{t('overview.usageEmpty')}</p>
                  ) : (
                    <div>
                      <div className="flex gap-2">
                        <YAxis max={Math.max(1, ...data.tokens.by_day.map((day) => day.total_tokens))} height={144} format={formatTokens} />
                        <div className="relative min-w-0 flex-1" style={{ height: 144 }}>
                          <GridLines />
                          <div className="relative h-full">
                            <DailyBars
                              height={144}
                              data={data.tokens.by_day.map((day) => ({ date: day.date, value: day.total_tokens }))}
                              tooltip={(date, value) => t('overview.tooltipTokens', { date, value: formatTokens(value) })}
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
                </div>
                <div className="flex min-w-0 flex-col gap-2 text-xs">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-muted-foreground">{t('overview.aiSum')}</span>
                    <span className="font-mono tabular-nums text-foreground">{formatTokens(data.tokens.total)}</span>
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-muted-foreground">{t('overview.aiAvg')}</span>
                    <span className="font-mono tabular-nums text-foreground">{formatTokens(avgTokens)}</span>
                  </div>
                  <div className="mt-auto flex justify-end pt-2">
                    <CardLink label={t('overview.viewUsage')} onClick={onOpenUsage} />
                  </div>
                </div>
              </div>
            )}
          </section>
        </>
      )}
    </Page>
  )
}
