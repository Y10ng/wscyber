import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import {
  Check,
  ChevronRight,
  Copy,
  FileText,
  Globe,
  ListChecks,
  Loader2,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  ShieldX,
  ShieldOff,
  Sparkles,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@cyber/theme'
import { Button, Sheet, SheetContent, SheetTitle, Switch } from '@cyber/ui'
import { MarkdownContent } from '@/markdown'
import { formatClock, formatShortDateTime } from '../lib/time'
import HttpPairBlock, { useEvidenceHttpView } from './HttpPairBlock'
import type {
  ExecutionApproval,
  ExecutionBiz,
  ExecutionEntry,
  ExecutionSummary,
  ExecutionTimelineData,
  ExecutionTurn,
} from '../lib/execution-timeline'
import './WebApiExecutionTimeline.css'

// ── 执行时间线（R2）─────────────────────────────────────────────────────────
// Web/API 任务/验证/报告/复测会话「执行」视图的默认视图。数据由 App 用
// lib/execution-timeline.buildExecutionTimeline 从已加载 AOP 事件派生（纯函数）。
// 版式：主内容宽 ≥1240 三栏（筛选 200 | 时间线 | 详情 420，容器查询驱动）；
// <1240 筛选收为上方分段行、详情在所选行下方原地展开（<860 同，单列）。
// 新事件追加：用户在底部自动跟随；上滚阅读不强制滚底，底部浮出「有 n 条新记录」。

export type ExecutionViewMode = 'timeline' | 'flow'

type FilterKey = 'all' | 'model' | 'tool' | 'http' | 'biz'

interface TimelineProps {
  data: ExecutionTimelineData
  /** 证据取号所属任务：验证/复测/报告会话的 http 条目可能不带 taskId。 */
  taskId?: string
  onViewEvidence: (taskId: string, evidenceId: string) => void
  onViewFinding: (findingId: string) => void
  /** 本任务各会话待审批审批行（App 由 useGuardrailReviews 聚合传入）。 */
  onResolveApproval: (approval: ExecutionApproval, approve: boolean) => Promise<void>
}

interface HeaderProps {
  mode: ExecutionViewMode
  onModeChange: (mode: ExecutionViewMode) => void
  summary: ExecutionSummary | null
}

// ── 展示辅助 ────────────────────────────────────────────────────────────────

function clockOf(ts: number): string {
  const date = new Date(ts)
  if (Number.isNaN(date.getTime())) return ''
  return formatClock(date.toISOString())
}

function clockMinuteOf(ts: number): string {
  const date = new Date(ts)
  if (Number.isNaN(date.getTime())) return ''
  return formatShortDateTime(date.toISOString())
}

function formatDuration(ms?: number): string {
  if (ms == null) return ''
  if (ms < 1000) return `${Math.max(1, Math.round(ms))}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  const minutes = Math.floor(ms / 60_000)
  return `${minutes}m${Math.round((ms % 60_000) / 1000)}s`
}

function methodTone(method: string): string {
  if (method === 'GET') return 'text-emerald-600 dark:text-emerald-400'
  if (method === 'POST') return 'text-blue-600 dark:text-blue-400'
  if (method === 'PUT' || method === 'PATCH') return 'text-amber-600 dark:text-amber-400'
  if (method === 'DELETE') return 'text-red-600 dark:text-red-400'
  return 'text-muted-foreground'
}

function httpStatusTone(code: number): string {
  if (code < 300) return 'text-emerald-600 dark:text-emerald-400'
  if (code < 400) return 'text-blue-600 dark:text-blue-400'
  if (code < 500) return 'text-amber-600 dark:text-amber-400'
  return 'text-red-600 dark:text-red-400'
}

const VERDICT_LABEL_KEYS: Record<string, string> = {
  supported: 'webapi.verdict_supported',
  counterevidence: 'webapi.verdict_counterevidence',
  inconclusive: 'webapi.verdict_inconclusive',
}

const RETEST_LABEL_KEYS: Record<string, string> = {
  reproducible: 'webapi.retestReproducible',
  fixed_in_scope: 'webapi.retestFixedInScope',
  inconclusive: 'webapi.retestInconclusive',
}

// 流量视图未发送原因：审批拒绝/超时的记录在状态列显示「审批拒绝」「审批超时」。
const HTTP_STATUS_LABEL_KEYS: Record<string, string> = {
  claimed: 'webapi.status_claimed',
  completed: 'webapi.status_completed',
  denied: 'webapi.status_denied',
  review_required: 'webapi.status_review_required',
  review_denied: 'webapi.status_review_denied',
  review_expired: 'webapi.status_review_expired',
  review_canceled: 'webapi.status_review_canceled',
  unknown: 'webapi.status_unknown',
  capture_failed: 'webapi.status_capture_failed',
}

// HTTP 摘要里的审批终态着色：拒绝红、超时/取消橙、待审琥珀。
function reviewStatusTone(status: string): string | null {
  if (status === 'review_denied') return 'text-red-600 dark:text-red-400'
  if (status === 'review_expired' || status === 'review_canceled') return 'text-orange-600 dark:text-orange-400'
  if (status === 'review_required') return 'text-amber-600 dark:text-amber-400'
  return null
}

// 执行视图审批行（待审批/已批准/已拒绝/已超时/已取消）。
const APPROVAL_STATE_KEYS: Record<ExecutionApproval['state'], string> = {
  pending: 'webapi.execApprovalPending',
  approved: 'webapi.execApprovalApproved',
  rejected: 'webapi.execApprovalRejected',
  expired: 'webapi.execApprovalExpired',
  canceled: 'webapi.execApprovalCanceled',
}

function approvalIcon(state: ExecutionApproval['state']): LucideIcon {
  if (state === 'pending') return ShieldAlert
  if (state === 'approved') return ShieldCheck
  if (state === 'rejected') return ShieldX
  return ShieldOff
}

function approvalTone(state: ExecutionApproval['state']): string {
  if (state === 'pending') return 'text-warning'
  if (state === 'approved') return 'text-emerald-600 dark:text-emerald-400'
  if (state === 'rejected') return 'text-red-600 dark:text-red-400'
  return 'text-orange-600 dark:text-orange-400'
}

function entryIcon(entry: ExecutionEntry): LucideIcon {
  if (entry.type === 'model') return Sparkles
  const biz = entry.biz
  if (biz?.kind === 'http') return Globe
  if (biz?.kind === 'finding') return ListChecks
  if (biz?.kind === 'verification') return ShieldCheck
  if (biz?.kind === 'retest') return RefreshCw
  if (biz?.kind === 'report') return FileText
  return Wrench
}

// 摘要（单行截断，title 全文）。业务语义按 task.md 指定格式组装，语言在渲染期解析。
function entrySummary(entry: ExecutionEntry, t: TFunction<'chat'>): string {
  if (entry.type === 'model') {
    const line = (entry.content || '').split('\n').map((part) => part.trim()).find(Boolean)
    return line || t('webapi.execModelMessage')
  }
  const biz = entry.biz
  if (biz?.kind === 'http' && biz.http) {
    const { method, path } = biz.http
    return `${method || '?'} ${path || biz.http.url}`
  }
  if (biz?.kind === 'finding') {
    if (entry.pending) return t('webapi.execFindingPending')
    if (entry.error) return biz.errorMessage || t('webapi.execFindingPending')
    const parts = [biz.findingNumber, biz.findingTitle].filter(Boolean).join(' ')
    return parts ? `${t('webapi.execFindingSaved')} ${parts}` : t('webapi.execFindingSaved')
  }
  if (biz?.kind === 'verification') {
    if (entry.pending) return t('webapi.execVerificationPending')
    if (entry.error) return biz.errorMessage || t('webapi.execVerificationPending')
    const label = biz.verdict ? t(VERDICT_LABEL_KEYS[biz.verdict] || 'webapi.status_unknown') : t('webapi.verdictPending')
    return `${t('webapi.execVerificationNote')}：${label}`
  }
  if (biz?.kind === 'retest') {
    if (entry.pending) return t('webapi.execRetestPending')
    if (entry.error) return biz.errorMessage || t('webapi.execRetestPending')
    const label = biz.verdict ? t(RETEST_LABEL_KEYS[biz.verdict] || 'webapi.status_unknown') : t('webapi.retestPending')
    return `${t('webapi.execRetestNote')}：${label}`
  }
  if (biz?.kind === 'report') {
    if (entry.pending) return t('webapi.execReportPending')
    if (entry.error) return biz.errorMessage || t('webapi.execReportPending')
    return `${t('webapi.execReportDraft')}：${biz.reportTitle || t('webapi.reportUntitled')}`
  }
  const args = (entry.argsText || '').replace(/\s+/g, ' ').trim()
  return `${entry.toolName || '?'} ${args.slice(0, 80)}`.trimEnd()
}

// 审批终态（拒绝/超时/取消）：请求未发送，状态文字优先于工具错误/捕获失败。
// 与流量视图的映射一致（review_denied → 审批拒绝 …），并标注「未发送」。
const TERMINAL_REVIEW_STATUSES = ['review_denied', 'review_expired', 'review_canceled'] as const

function terminalReviewPart(http: { status: string; sent?: boolean | null }, t: TFunction<'chat'>): { text: string; tone: string } | null {
  if (!(TERMINAL_REVIEW_STATUSES as readonly string[]).includes(http.status)) return null
  const key = HTTP_STATUS_LABEL_KEYS[http.status]
  if (!key) return null
  return { text: `→ ${t(key)} · ${t('webapi.notSent')}`, tone: reviewStatusTone(http.status) || 'text-red-600 dark:text-red-400' }
}

// HTTP 摘要里的结果段：`→ 200`；completed 无响应码 → 无响应；其他业务状态 → 本地化标签。
function httpResultPart(entry: ExecutionEntry, t: TFunction<'chat'>): { text: string; tone: string } | null {
  const http = entry.biz?.kind === 'http' ? entry.biz.http : undefined
  if (!http) return null
  if (entry.pending) return { text: '…', tone: 'text-muted-foreground' }
  // 审批拒绝/超时/取消的请求按未发送展示，不能被“捕获失败”（工具错误）覆盖。
  const review = terminalReviewPart(http, t)
  if (review) return review
  if (entry.error) return { text: t('webapi.status_capture_failed'), tone: 'text-red-600 dark:text-red-400' }
  if (http.httpStatus != null) return { text: `→ ${http.httpStatus}`, tone: httpStatusTone(http.httpStatus) }
  if (http.status === 'completed') return { text: t('webapi.noResponse'), tone: 'text-orange-600 dark:text-orange-400' }
  const key = HTTP_STATUS_LABEL_KEYS[http.status]
  const label = key ? t(key) : t('webapi.status_unknown')
  const reviewTone = reviewStatusTone(http.status)
  return { text: `→ ${label}`, tone: reviewTone || (http.status === 'denied' || http.status === 'capture_failed' ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground') }
}

// 状态列：进行中 / 完成 / 错误（HTTP 行的状态码由 httpPart 在摘要尾渲染，避免重复）。
function entryStatus(entry: ExecutionEntry, t: TFunction<'chat'>): { text: string; tone: string; pending: boolean } {
  if (entry.type === 'model') return { text: '', tone: '', pending: false }
  if (entry.pending) return { text: t('webapi.execRunning'), tone: 'text-muted-foreground', pending: true }
  const http = entry.biz?.kind === 'http' ? entry.biz.http : undefined
  const review = http ? terminalReviewPart(http, t) : null
  if (review) return { text: review.text.replace(/^→ /, ''), tone: review.tone, pending: false }
  if (entry.error) return { text: t('webapi.execFailed'), tone: 'text-red-600 dark:text-red-400', pending: false }
  if (http) {
    if (http.httpStatus != null) return { text: String(http.httpStatus), tone: httpStatusTone(http.httpStatus), pending: false }
    if (http.status === 'completed') return { text: t('webapi.noResponse'), tone: 'text-orange-600 dark:text-orange-400', pending: false }
    const key = HTTP_STATUS_LABEL_KEYS[http.status]
    if (key) {
      const reviewTone = reviewStatusTone(http.status)
      const tone = reviewTone || (http.status === 'denied' || http.status === 'capture_failed'
        ? 'text-red-600 dark:text-red-400'
        : http.status === 'claimed' ? 'text-blue-600 dark:text-blue-400' : 'text-amber-600 dark:text-amber-400')
      return { text: t(key), tone, pending: false }
    }
  }
  return { text: t('webapi.execDone'), tone: 'text-emerald-600 dark:text-emerald-400', pending: false }
}

// ── 顶部一行：时间线 | 控制流 切换 + 计数 ──────────────────────────────────

export function ExecutionViewHeader({ mode, onModeChange, summary }: HeaderProps) {
  const { t } = useTranslation('chat')
  const modes: { key: ExecutionViewMode; label: string }[] = [
    { key: 'timeline', label: t('webapi.timeline') },
    { key: 'flow', label: t('webapi.flow') },
  ]
  return (
    <div className="flex h-9 shrink-0 items-center gap-2 border-b border-border/60 bg-background px-3">
      <div role="group" aria-label={t('webapi.execution')} className="flex shrink-0 items-center gap-0.5 rounded-md border border-border/60 p-0.5">
        {modes.map((item) => (
          <button
            key={item.key}
            type="button"
            aria-pressed={mode === item.key}
            onClick={() => onModeChange(item.key)}
            className={cn(
              'h-6 rounded px-2 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
              mode === item.key ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
            )}
          >
            {item.label}
          </button>
        ))}
      </div>
      {summary && (
        <span className="ml-auto hidden min-w-0 truncate font-mono text-[11px] tabular-nums text-muted-foreground sm:inline" data-testid="execution-counts">
          {t('webapi.countModel')} {summary.modelCount} · {t('webapi.countTools')} {summary.toolCount} · HTTP {summary.httpCount} · {t('webapi.countBiz')} {summary.bizCount}
        </span>
      )}
    </div>
  )
}

// ── 筛选（宽屏左侧栏 / 窄屏顶部分段共用） ─────────────────────────────────

const FILTER_KEYS: FilterKey[] = ['all', 'model', 'tool', 'http', 'biz']

function filterCount(key: FilterKey, data: ExecutionTimelineData): number {
  if (key === 'all') return data.entries.length
  if (key === 'model') return data.summary.modelCount
  if (key === 'tool') return data.summary.toolCount
  if (key === 'http') return data.summary.httpCount
  return data.summary.bizCount
}

function entryMatchesFilter(entry: ExecutionEntry, key: FilterKey): boolean {
  if (key === 'all') return true
  if (key === 'model') return entry.type === 'model'
  if (key === 'tool') return entry.type === 'tool'
  if (key === 'http') return entry.biz?.kind === 'http'
  return !!entry.biz && entry.biz.kind !== 'http'
}

function FilterButtons({ filter, onFilter, data, layout }: {
  filter: FilterKey
  onFilter: (key: FilterKey) => void
  data: ExecutionTimelineData
  layout: 'side' | 'top'
}) {
  const { t } = useTranslation('chat')
  const labels: Record<FilterKey, string> = {
    all: t('webapi.trafficFilterAll'),
    model: t('webapi.countModel'),
    tool: t('webapi.countTools'),
    http: 'HTTP',
    biz: t('webapi.countBiz'),
  }
  return (
    <>
      {FILTER_KEYS.map((key) => {
        const active = filter === key
        return (
          <button
            key={key}
            type="button"
            aria-pressed={active}
            onClick={() => onFilter(key)}
            className={cn(
              'relative shrink-0 text-left text-[13px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-inset',
              layout === 'side'
                ? 'flex h-[34px] items-center gap-2 rounded-md px-3'
                : 'flex h-7 items-center gap-1.5 rounded-md px-2',
              active
                ? 'bg-[var(--brand-soft)] font-medium text-[var(--brand-primary)]'
                : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
            )}
          >
            {layout === 'side' && (
              <span aria-hidden className={cn('absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-[var(--brand-primary)] transition-opacity', active ? 'opacity-100' : 'opacity-0')} />
            )}
            <span className="min-w-0 truncate">{labels[key]}</span>
            <span className="ml-auto font-mono text-[11px] tabular-nums opacity-80">{filterCount(key, data)}</span>
          </button>
        )
      })}
    </>
  )
}

// ── 详情（宽屏右栏 / 窄屏行内展开共用同一内容） ───────────────────────────

async function writeClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch { /* 回落 */ }
  try {
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.opacity = '0'
    document.body.appendChild(textarea)
    textarea.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(textarea)
    return ok
  } catch {
    return false
  }
}

function CopyButton({ text }: { text: string }) {
  const { t } = useTranslation('chat')
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      aria-label={t('webapi.execCopy')}
      onClick={async () => {
        const ok = await writeClipboard(text)
        if (!ok) return
        setCopied(true)
        setTimeout(() => setCopied(false), 2000)
      }}
      className="flex h-6 shrink-0 items-center gap-1 rounded px-1.5 text-[11px] text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground"
    >
      {copied ? <Check className="h-3 w-3 text-emerald-600 dark:text-emerald-400" /> : <Copy className="h-3 w-3" />}
      {copied ? t('webapi.copied') : t('webapi.execCopy')}
    </button>
  )
}

function prettyJson(text: string | undefined): string {
  if (!text) return ''
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return text
  }
}

// 参数/结果：默认折叠，标题行可点开；JSON 等宽 12px、最大高度 320px 自身滚动。
function JsonCard({ title, text, empty }: { title: string; text: string; empty: string }) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState(false)
  return (
    <section className="rounded-lg border border-border bg-card p-3">
      <div className="flex min-w-0 items-center gap-1">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
          className="flex min-w-0 items-center gap-1.5 rounded text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <ChevronRight className={cn('h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden />
          <span className="truncate text-[12px] font-semibold text-foreground">{title}</span>
        </button>
        {text && <span className="ml-auto shrink-0">{open ? <CopyButton text={text} /> : null}</span>}
      </div>
      {open && (
        text ? (
          <pre className="mt-2 max-h-[320px] overflow-auto whitespace-pre-wrap break-all rounded-md border border-border/60 bg-[var(--bg-code-light)] px-2 py-1.5 font-mono text-[12px] leading-[1.5] text-foreground" data-testid="execution-json">
            {text}
          </pre>
        ) : (
          <div className="mt-2 rounded-md border border-dashed border-border/60 px-2 py-1.5 text-[12px] text-muted-foreground">{empty}</div>
        )
      )}
    </section>
  )
}

// HTTP 条目的请求/响应原文：与漏洞卡同一组件与取数（证据 → HTTP 视图）。
function HttpExchangeCard({ taskId, evidenceId }: { taskId: string; evidenceId: string }) {
  const { t } = useTranslation('chat')
  const { view, loading, error, reload } = useEvidenceHttpView(taskId, evidenceId)
  const [masked, setMasked] = useState(true)
  return (
    <section className="rounded-lg border border-border bg-card p-3">
      <div className="mb-2 flex min-w-0 items-center justify-between gap-2">
        <h4 className="text-[12px] font-semibold text-foreground">{t('webapi.execRequestResponse')}</h4>
        <label className="flex shrink-0 items-center gap-1.5 text-[11px] text-muted-foreground">
          <Switch checked={masked} onCheckedChange={setMasked} className="scale-75" />
          <span>{t('vulnCard.maskSensitive')}</span>
        </label>
      </div>
      <HttpPairBlock
        view={view}
        loading={loading}
        error={error}
        retry={reload}
        requestMarks={[]}
        responseMarks={[]}
        masked={masked}
        foldHeaders
        stacked
      />
    </section>
  )
}

function DetailHeader({ label, toolName, timestamp, durationMs, pending, error }: {
  label: string
  toolName?: string
  timestamp: number
  durationMs?: number
  pending: boolean
  error: boolean
}) {
  const { t } = useTranslation('chat')
  return (
    <div className="flex min-h-7 shrink-0 flex-wrap items-center gap-x-2 gap-y-1">
      <span className="text-[14px] font-semibold text-foreground">{label}</span>
      {toolName && <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground">{toolName}</span>}
      <span className="font-mono text-[12px] tabular-nums text-muted-foreground">{clockOf(timestamp)}</span>
      {durationMs != null && <span className="font-mono text-[12px] tabular-nums text-muted-foreground">{formatDuration(durationMs)}</span>}
      {pending && <span className="inline-flex items-center gap-1 text-[12px] text-muted-foreground"><Loader2 className="h-3 w-3 animate-spin" />{t('webapi.execRunning')}</span>}
      {error && <span className="text-[12px] text-red-600 dark:text-red-400">{t('webapi.execFailed')}</span>}
    </div>
  )
}

function EntryDetail({ entry, taskId, onViewEvidence, onViewFinding }: {
  entry: ExecutionEntry
  taskId?: string
  onViewEvidence: (taskId: string, evidenceId: string) => void
  onViewFinding: (findingId: string) => void
}) {
  const { t } = useTranslation('chat')
  const biz: ExecutionBiz | undefined = entry.biz
  const http = biz?.kind === 'http' ? biz.http : undefined

  if (entry.type === 'model') {
    const usage = entry.usage
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
        <div className="flex min-h-7 shrink-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-[14px] font-semibold text-foreground">{t('webapi.execModelMessage')}</span>
          <span className="ml-auto font-mono text-[12px] tabular-nums text-muted-foreground">{clockOf(entry.timestamp)}</span>
          {usage && (
            <span className="font-mono text-[11px] tabular-nums text-muted-foreground" title={`${t('tokenInput', { count: usage.input_tokens || 0 })} · ${t('tokenOutput', { count: usage.output_tokens || 0 })}`}>
              ↓ {(usage.input_tokens || 0).toLocaleString()} ↑ {(usage.output_tokens || 0).toLocaleString()} tokens
            </span>
          )}
        </div>
        <section className="rounded-lg border border-border bg-card p-3">
          <div className="min-w-0 text-[14px] leading-[1.6]">
            {(entry.content || '').trim()
              ? <MarkdownContent content={entry.content || ''} />
              : <span className="text-muted-foreground">—</span>}
          </div>
        </section>
      </div>
    )
  }

  const evidenceTaskId = http?.taskId || taskId || ''
  const evidenceId = http?.evidenceId || ''

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
      <DetailHeader
        label={t('webapi.execToolCall')}
        toolName={entry.toolName}
        timestamp={entry.timestamp}
        durationMs={entry.durationMs}
        pending={!!entry.pending}
        error={!!entry.error}
      />

      {biz && biz.kind !== 'http' && (
        <section className="rounded-lg border border-border bg-card p-3">
          <p className="min-w-0 break-words text-[14px] leading-[1.6] text-foreground">{entrySummary(entry, t)}</p>
          {biz.kind === 'finding' && entry.error && (
            <p className="mt-1 min-w-0 break-all text-[12px] text-red-600 dark:text-red-400">{biz.errorMessage}</p>
          )}
        </section>
      )}

      {http && (
        <section className="flex flex-col gap-1 rounded-lg border border-border bg-card p-3 text-[12px]">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono">
            <span className={cn('font-semibold', methodTone(http.method))}>{http.method || '?'}</span>
            <span className="min-w-0 break-all text-foreground" title={http.url}>{http.url}</span>
          </div>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-muted-foreground">
            {http.evidenceId && <span className="font-mono" title={http.evidenceId}>evidence {http.evidenceId}</span>}
            {http.captureTruncated && <span className="text-amber-600 dark:text-amber-400">{t('webapi.captureTruncated')}</span>}
            {http.sent === false && <span>{t('webapi.notSent')}</span>}
            {http.sent === true && <span>{t('webapi.sent')}</span>}
          </div>
        </section>
      )}

      {http && evidenceId && evidenceTaskId && (
        <HttpExchangeCard taskId={evidenceTaskId} evidenceId={evidenceId} />
      )}

      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {biz?.kind === 'http' && evidenceId && http?.taskId && (
          <Button type="button" variant="outline" size="xs" className="h-8" onClick={() => onViewEvidence(http.taskId!, evidenceId)}>
            {t('webapi.execViewInTraffic')}
          </Button>
        )}
        {biz?.kind === 'finding' && !entry.pending && biz.findingId && (
          <Button type="button" variant="outline" size="xs" className="h-8" onClick={() => onViewFinding(biz.findingId!)}>
            {t('webapi.execViewFinding')}
          </Button>
        )}
      </div>

      <JsonCard title={t('toolCard.arguments')} text={prettyJson(entry.argsText)} empty={entry.pending ? t('webapi.execRunning') : '—'} />
      {entry.pending ? (
        <div className="rounded-lg border border-dashed border-border px-3 py-2 text-[12px] text-muted-foreground">{t('webapi.execRunning')}…</div>
      ) : (
        <JsonCard title={t('toolCard.result')} text={prettyJson(entry.resultText)} empty="—" />
      )}
    </div>
  )
}

// ── 审批行：置顶待审批卡（可批准/拒绝）+ 时间线内审批事件行 ────────────────

function ApprovalRemaining({ expiresAt }: { expiresAt: number }) {
  const { t } = useTranslation('chat')
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const remaining = Math.max(0, expiresAt - now)
  const minutes = Math.floor(remaining / 60_000)
  const seconds = Math.floor((remaining % 60_000) / 1000)
  return <span className="shrink-0 font-mono tabular-nums text-muted-foreground">{t('webapi.execApprovalRemaining', { time: minutes > 0 ? `${minutes}m ${String(seconds).padStart(2, '0')}s` : `${seconds}s` })}</span>
}

// 置顶待审批卡：live pending 行（actionable），直接批准/拒绝。
function PendingApprovalCard({ approval, onResolve, busy }: {
  approval: ExecutionApproval
  onResolve: (approval: ExecutionApproval, approve: boolean) => Promise<void>
  busy: 'approve' | 'reject' | null
}) {
  const { t } = useTranslation('chat')
  const [error, setError] = useState('')
  const submitting = useRef(false)
  const resolve = async (approve: boolean) => {
    if (submitting.current) return
    submitting.current = true
    setError('')
    try { await onResolve(approval, approve) } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) } finally { submitting.current = false }
  }
  return (
    <div data-approval-id={approval.id} data-approval-state="pending" className="shrink-0 border-b border-warning/30 bg-warning/5 px-3 py-2">
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px]">
        <ShieldAlert className="h-3.5 w-3.5 shrink-0 text-warning" aria-hidden="true" />
        <span className="shrink-0 font-medium text-warning">{t('webapi.execApprovalPendingCard')}</span>
        <span className="shrink-0 rounded bg-muted px-1 py-0.5 font-mono text-[11px] text-muted-foreground">{approval.toolName}</span>
        {approval.reason?.includes('子 Agent oracle_explorer 会话') && (
          <span className="ws-st-badge shrink-0 bg-slate-100 text-[10.5px] text-slate-600 dark:bg-slate-800 dark:text-slate-300">
            {t('webapi.execSubagentBadge')}
          </span>
        )}
        {approval.expiresAt !== undefined && <ApprovalRemaining expiresAt={approval.expiresAt} />}
      </div>
      <p className="mt-1 break-all font-mono text-[12px] text-foreground" title={approval.summary}>{approval.summary}</p>
      {approval.reason && <p className="mt-1 break-all text-[11px] text-muted-foreground" title={approval.reason}>{approval.reason}</p>}
      {approval.unavailable && <p role="status" className="mt-1 text-[11px] text-warning">{t('webapi.execApprovalUnavailable')}</p>}
      {error && <p role="alert" className="mt-1 text-[11px] text-destructive">{error}</p>}
      <div className="mt-1.5 flex items-center gap-2">
        <Button type="button" size="xs" variant="outline" className="h-6 gap-1 px-2 text-[11px] text-destructive hover:text-destructive" disabled={busy !== null || approval.unavailable} onClick={() => { void resolve(false) }}>
          {busy === 'reject' ? <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" /> : <ShieldX className="h-3 w-3" aria-hidden="true" />}
          {t('webapi.execApprovalReject')}
        </Button>
        <Button type="button" size="xs" className="h-6 gap-1 px-2 text-[11px]" disabled={busy !== null || approval.unavailable} onClick={() => { void resolve(true) }}>
          {busy === 'approve' ? <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" /> : <ShieldCheck className="h-3 w-3" aria-hidden="true" />}
          {t('webapi.execApprovalApprove')}
        </Button>
      </div>
    </div>
  )
}

// 时间线内审批事件行：与其它行同一栅格（时间列 → 耗时列 → 图标列 → 左对齐
// 内容 → 右侧状态列）。状态文字带颜色，工具名小标签，参数单行截断。
function ApprovalRow({ approval }: { approval: ExecutionApproval }) {
  const { t } = useTranslation('chat')
  const Icon = approvalIcon(approval.state)
  const time = approval.resolvedAt ?? approval.timestamp
  return (
    <div
      data-approval-id={approval.id}
      data-approval-state={approval.state}
      className="webapi-exec-row h-[34px] w-full items-center gap-2 px-3 text-left"
    >
      <span className="font-mono text-[12px] tabular-nums text-muted-foreground">{time ? clockOf(time) : ''}</span>
      <span className="webapi-exec-dur-col text-right font-mono text-[12px] tabular-nums text-muted-foreground" />
      <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      <span className="min-w-0 truncate text-[13px] text-foreground">
        <span className={cn('font-medium', approvalTone(approval.state))}>{t(APPROVAL_STATE_KEYS[approval.state])}</span>
        {approval.toolName && <span className="ml-1.5 rounded bg-muted px-1 py-0.5 font-mono text-[10px] text-muted-foreground">{approval.toolName}</span>}
        {approval.summary && <span className="ml-1.5 font-mono text-muted-foreground" title={approval.summary}>{approval.summary}</span>}
      </span>
      <span className="flex min-w-0 items-center justify-end gap-1.5 text-[12px]" />
    </div>
  )
}

// ── 主组件：三栏 / 堆叠 ────────────────────────────────────────────────────

export default function WebApiExecutionTimeline({ data, taskId, onViewEvidence, onViewFinding, onResolveApproval }: TimelineProps) {
  const { t } = useTranslation('chat')
  const [filter, setFilter] = useState<FilterKey>('all')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  // <1024：详情改为右侧滑出 Sheet（与行内展开互斥）；1024–1279 行内展开。
  const [narrow, setNarrow] = useState(() => typeof window !== 'undefined' && window.matchMedia('(max-width: 1023px)').matches)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [newCount, setNewCount] = useState(0)
  const [approvalBusy, setApprovalBusy] = useState<{ id: string; action: 'approve' | 'reject' } | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(true)
  const prevCountRef = useRef(data.entries.length)
  const turnRefs = useRef(new Map<string, HTMLDivElement>())

  // 视口 <1024 用 Sheet，≥1024 用右栏/行内详情；跨断点时关闭 Sheet。
  useEffect(() => {
    const media = window.matchMedia('(max-width: 1023px)')
    const update = () => { setNarrow(media.matches); if (!media.matches) setSheetOpen(false) }
    update()
    media.addEventListener('change', update)
    return () => media.removeEventListener('change', update)
  }, [])

  // 待审批解析：每条 live pending 找回 useGuardrailReviews 的原 Review 对象，
  // 批准/拒绝经 App 的 guardrails.resolve 走（不在本组件另造请求路径）。
  const resolveApproval = useCallback(async (approval: ExecutionApproval, approve: boolean) => {
    if (approvalBusy) return
    setApprovalBusy({ id: approval.id, action: approve ? 'approve' : 'reject' })
    try { await onResolveApproval(approval, approve) } finally { setApprovalBusy(null) }
  }, [approvalBusy, onResolveApproval])

  // 新事件追加：用户在底部 → 自动跟随滚底；上滚阅读 → 计数并浮出按钮。
  useEffect(() => {
    const previous = prevCountRef.current
    prevCountRef.current = data.entries.length
    if (data.entries.length <= previous) {
      // 会话切换/整体重算：重置跟随与计数。
      pinnedRef.current = true
      setNewCount(0)
      const el = scrollRef.current
      if (el) el.scrollTop = el.scrollHeight
      return
    }
    if (pinnedRef.current) {
      requestAnimationFrame(() => {
        const el = scrollRef.current
        if (el && pinnedRef.current) el.scrollTop = el.scrollHeight
      })
    } else {
      setNewCount((current) => current + (data.entries.length - previous))
    }
  }, [data.entries.length])

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 40
    pinnedRef.current = atBottom
    if (atBottom) setNewCount(0)
  }, [])

  const jumpToBottom = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    pinnedRef.current = true
    setNewCount(0)
    el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  }, [])

  const visible = useMemo(
    () => data.entries.filter((entry) => entryMatchesFilter(entry, filter)),
    [data.entries, filter],
  )

  // 行序列：工具/模型条目与已终态的审批事件按时间合并（审批行属于其事件所在
  // 轮次，随该轮排序，不再固定显示在时间线最上方）；按 turnKey 变化插入轮次头。
  // 轮头时间取该轮首条可见行时间；工具计数只算工具条目（审批行不计）。
  const rows = useMemo(() => {
    type RowItem =
      | { kind: 'turn'; key: string; timestamp: number; toolCount: number }
      | { kind: 'entry'; entry: ExecutionEntry }
      | { kind: 'approval'; approval: ExecutionApproval }
    const items: Array<{ kind: 'entry'; entry: ExecutionEntry } | { kind: 'approval'; approval: ExecutionApproval }>
      = visible.map((entry) => ({ kind: 'entry' as const, entry }))
    if (filter === 'all' || filter === 'tool') {
      for (const approval of data.approvals) {
        if (approval.state !== 'pending') items.push({ kind: 'approval' as const, approval })
      }
    }
    const timestampOf = (item: (typeof items)[number]) => item.kind === 'entry' ? item.entry.timestamp : item.approval.timestamp
    const turnKeyOf = (item: (typeof items)[number]) => item.kind === 'entry' ? item.entry.turnKey : item.approval.turnKey
    items.sort((left, right) => timestampOf(left) - timestampOf(right))
    const toolCountByTurn = new Map<string, number>()
    for (const item of items) {
      if (item.kind === 'entry' && item.entry.type === 'tool') {
        toolCountByTurn.set(item.entry.turnKey, (toolCountByTurn.get(item.entry.turnKey) || 0) + 1)
      }
    }
    const out: RowItem[] = []
    let lastTurn = ''
    for (const item of items) {
      const turnKey = turnKeyOf(item)
      if (turnKey !== lastTurn) {
        lastTurn = turnKey
        out.push({
          kind: 'turn',
          key: turnKey,
          timestamp: timestampOf(item),
          toolCount: toolCountByTurn.get(turnKey) || 0,
        })
      }
      out.push(item)
    }
    return out
  }, [visible, data.approvals, data.turnByKey, filter])

  const selected = useMemo(() => data.entries.find((entry) => entry.id === selectedId) || null, [data.entries, selectedId])

  // 工具调用 → 审批终态：被拒绝/超时/取消的调用行按“审批结果 · 未发送”展示，
  // 优先于工具错误（被拒调用不会执行，结果为 deny 错误而非捕获失败）。
  // 状态文字与流量视图同词（审批拒绝/审批超时/审批取消）。
  const REVIEW_OUTCOME_KEYS: Record<string, string> = {
    rejected: 'webapi.status_review_denied',
    expired: 'webapi.status_review_expired',
    canceled: 'webapi.status_review_canceled',
  }
  const terminalApprovalByCall = useMemo(() => {
    const map = new Map<string, ExecutionApproval>()
    for (const approval of data.approvals) {
      if (approval.callId && approval.state !== 'pending') map.set(approval.callId, approval)
    }
    return map
  }, [data.approvals])

  function approvalOutcomePart(approval: ExecutionApproval, t: TFunction<'chat'>): { text: string; tone: string } {
    return { text: t(REVIEW_OUTCOME_KEYS[approval.state] || APPROVAL_STATE_KEYS[approval.state]), tone: approvalTone(approval.state) }
  }

  const scrollToTurn = useCallback((turnKey: string) => {
    turnRefs.current.get(turnKey)?.scrollIntoView({ block: 'start' })
  }, [])

  const turnsList = useMemo(
    () => data.turns
      .map((turn) => {
        const first = data.entries.find((entry) => entry.turnKey === turn.key)
        return first ? { turn, timestamp: first.timestamp } : null
      })
      .filter((item): item is { turn: ExecutionTurn; timestamp: number } => item !== null),
    [data.turns, data.entries],
  )

  const renderRow = (item: (typeof rows)[number]) => {
    if (item.kind === 'turn') {
      return (
        <div
          key={`turn:${item.key}`}
          ref={(el) => { if (el) turnRefs.current.set(item.key, el); else turnRefs.current.delete(item.key) }}
          className="flex h-7 items-center gap-2 border-b border-border/40 bg-muted/40 px-3 text-[11px] text-muted-foreground"
        >
          <span className="font-medium text-foreground">{t('webapi.execTurn', { index: data.turnByKey[item.key] || 0 })}</span>
          <span className="font-mono tabular-nums">{clockOf(item.timestamp)}</span>
          <span aria-hidden>·</span>
          <span className="font-mono tabular-nums">{t('webapi.execTurnTools', { count: item.toolCount })}</span>
        </div>
      )
    }
    if (item.kind === 'approval') {
      return <ApprovalRow key={`approval:${item.approval.eventId}`} approval={item.approval} />
    }
    const entry = item.entry
    const icon = entryIcon(entry)
    const Icon = icon
    const approvalOutcome = entry.callId ? terminalApprovalByCall.get(entry.callId) : undefined
    const status = approvalOutcome && entry.biz?.kind === 'http'
      ? { ...approvalOutcomePart(approvalOutcome, t), pending: false }
      : entryStatus(entry, t)
    const selectedEntry = selectedId === entry.id
    const httpPart = entry.biz?.kind === 'http'
      ? (approvalOutcome
        ? { ...approvalOutcomePart(approvalOutcome, t), text: `${approvalOutcomePart(approvalOutcome, t).text} · ${t('webapi.notSent')}` }
        : httpResultPart(entry, t))
      : null
    const summary = entrySummary(entry, t)
    return (
      <div key={entry.id}>
        <button
          type="button"
          role="row"
          aria-selected={selectedEntry}
          onClick={() => {
            if (narrow) {
              setSelectedId(entry.id)
              setSheetOpen(true)
            } else {
              setSelectedId(selectedEntry ? null : entry.id)
            }
          }}
          title={summary}
          className={cn(
            'webapi-exec-row h-[34px] w-full items-center gap-2 px-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-inset',
            selectedEntry ? 'bg-primary/10 shadow-[inset_2px_0_0_hsl(var(--primary))]' : 'hover:bg-muted/40',
          )}
        >
          <span className="font-mono text-[12px] tabular-nums text-muted-foreground">{clockOf(entry.timestamp)}</span>
          <span className="webapi-exec-dur-col text-right font-mono text-[12px] tabular-nums text-muted-foreground">{formatDuration(entry.durationMs)}</span>
          <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="min-w-0 truncate text-[13px] text-foreground">
            {entry.biz?.kind === 'http' && entry.biz.http && (
              <>
                <span className={cn('font-mono font-semibold', methodTone(entry.biz.http.method))}>{entry.biz.http.method || '?'}</span>{' '}
                <span className="font-mono">{entry.biz.http.path || entry.biz.http.url}</span>
              </>
            )}
            {(!httpPart || !entry.biz?.http) && summary}
          </span>
          <span className="flex min-w-0 items-center justify-end gap-1.5 text-[12px]">
            {httpPart && <span className={cn('font-mono font-semibold tabular-nums', httpPart.tone)}>{httpPart.text}</span>}
            {entry.biz?.kind === 'http' && entry.biz.http?.captureTruncated && (
              <span className="shrink-0 text-[11px] text-amber-600 dark:text-amber-400">{t('webapi.execTruncated')}</span>
            )}
            {/* 审批拒绝/超时/取消的 httpPart 已含「未发送」，不再重复渲染未发送徽标。 */}
            {entry.biz?.kind === 'http' && entry.biz.http?.sent === false && !approvalOutcome && (
              <span className="shrink-0 text-[11px] text-muted-foreground">{t('webapi.notSent')}</span>
            )}
            {status.pending && <Loader2 className="h-3 w-3 shrink-0 animate-spin text-muted-foreground" aria-hidden />}
            {/* HTTP 行状态码已并入 httpPart（→ 200），不重复渲染；其余行显示 完成/错误/进行中。 */}
            {status.text && !httpPart && <span className={cn('shrink-0 tabular-nums', status.tone)}>{status.text}</span>}
          </span>
        </button>
        {selectedEntry && (
          <div className="webapi-exec-detail-inline border-b border-border/60 bg-muted/20" data-testid="execution-detail-inline">
            <div className="flex max-h-[60vh] min-h-0 flex-col overflow-hidden">
              <EntryDetail entry={entry} taskId={taskId} onViewEvidence={onViewEvidence} onViewFinding={onViewFinding} />
            </div>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="webapi-exec flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* 置顶待审批卡：本任务各会话的 live pending，可直接批准/拒绝。 */}
      {data.pendingApprovals.length > 0 && (
        <div className="flex shrink-0 flex-col">
          {data.pendingApprovals.map((approval) => (
            <PendingApprovalCard
              key={approval.eventId}
              approval={approval}
              onResolve={resolveApproval}
              busy={approvalBusy?.id === approval.id ? approvalBusy.action : null}
            />
          ))}
        </div>
      )}
      {/* 窄屏：筛选收为上方一行分段（宽屏由容器查询隐藏）。 */}
      <div className="webapi-exec-filters-top flex shrink-0 items-center gap-1 overflow-x-auto border-b border-border/60 px-2 py-1" role="group" aria-label={t('webapi.execution')}>
        <FilterButtons filter={filter} onFilter={setFilter} data={data} layout="top" />
      </div>

      <div className="webapi-exec-layout flex min-h-0 flex-1 overflow-hidden">
        {/* 宽屏左筛选栏。 */}
        <aside className="webapi-exec-filters-side min-h-0 flex-col overflow-y-auto border-r border-border/60 bg-background" aria-label={t('webapi.execution')}>
          <div className="flex flex-col py-1" role="group">
            <FilterButtons filter={filter} onFilter={setFilter} data={data} layout="side" />
          </div>
          <div className="mt-2 border-t border-border/60 px-3 pb-1 pt-2 text-[11px] font-medium text-muted-foreground">{t('webapi.execTurns')}</div>
          <div className="flex flex-col pb-2">
            {turnsList.map(({ turn, timestamp }) => (
              <button
                key={turn.key}
                type="button"
                onClick={() => scrollToTurn(turn.key)}
                title={t('webapi.execJumpTurn', { index: turn.index })}
                className="flex h-7 items-center gap-2 px-3 text-left text-[12px] text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-inset"
              >
                <span className="truncate">{t('webapi.execTurn', { index: turn.index })}</span>
                <span className="ml-auto shrink-0 font-mono text-[11px] tabular-nums">{clockMinuteOf(timestamp)}</span>
              </button>
            ))}
          </div>
        </aside>

        {/* 中间时间线。 */}
        <div className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          <div ref={scrollRef} onScroll={handleScroll} className="min-h-0 flex-1 overflow-auto overscroll-contain" data-testid="execution-timeline">
            {rows.length === 0 ? (
              <div className="flex h-full items-center justify-center px-4 text-center text-sm text-muted-foreground">{t('webapi.execNoEntries')}</div>
            ) : rows.map(renderRow)}
          </div>
          {newCount > 0 && (
            <button
              type="button"
              onClick={jumpToBottom}
              className="absolute bottom-4 left-1/2 z-10 -translate-x-1/2 rounded-full border border-border bg-background px-3 py-1 text-[12px] text-foreground shadow-elevated transition-colors hover:bg-muted/60"
            >
              {t('webapi.execNewEntries', { count: newCount })}
            </button>
          )}
        </div>

        {/* 宽屏右详情栏。 */}
        <aside className="webapi-exec-detail-side min-h-0 min-w-0 flex-col overflow-hidden border-l border-border/60 bg-background" aria-label={t('webapi.execSelectEntry')}>
          {selected ? (
            <EntryDetail entry={selected} taskId={taskId} onViewEvidence={onViewEvidence} onViewFinding={onViewFinding} />
          ) : (
            <div className="flex flex-1 items-center justify-center px-4 text-center text-sm text-muted-foreground">{t('webapi.execSelectEntry')}</div>
          )}
        </aside>
      </div>

      {/* <1024：点击条目后从右侧滑出的详情 Sheet。 */}
      <Sheet open={narrow && sheetOpen} onOpenChange={setSheetOpen}>
        <SheetContent
          side="right"
          closeLabel={t('webapi.close')}
          aria-describedby={undefined}
          className="flex w-[calc(100vw-32px)] max-w-[440px] flex-col gap-0 p-0 sm:max-w-[440px]"
        >
          <SheetTitle className="sr-only">{t('webapi.execSelectEntry')}</SheetTitle>
          {selected && <EntryDetail entry={selected} taskId={taskId} onViewEvidence={onViewEvidence} onViewFinding={onViewFinding} />}
        </SheetContent>
      </Sheet>
    </div>
  )
}
