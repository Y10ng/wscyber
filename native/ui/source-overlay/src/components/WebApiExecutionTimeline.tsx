import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import {
  Check,
  Copy,
  FileText,
  Globe,
  ListChecks,
  Loader2,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Wrench,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@cyber/theme'
import { Button } from '@cyber/ui'
import { MarkdownContent } from '@/markdown'
import type {
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
  onViewEvidence: (taskId: string, evidenceId: string) => void
  onViewFinding: (findingId: string) => void
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
  return date.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function clockMinuteOf(ts: number): string {
  const date = new Date(ts)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit' })
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

const HTTP_STATUS_LABEL_KEYS: Record<string, string> = {
  claimed: 'webapi.status_claimed',
  completed: 'webapi.status_completed',
  denied: 'webapi.status_denied',
  review_required: 'webapi.status_review_required',
  unknown: 'webapi.status_unknown',
  capture_failed: 'webapi.status_capture_failed',
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

// HTTP 摘要里的结果段：`→ 200`；completed 无响应码 → 无响应；其他业务状态 → 本地化标签。
function httpResultPart(entry: ExecutionEntry, t: TFunction<'chat'>): { text: string; tone: string } | null {
  const http = entry.biz?.kind === 'http' ? entry.biz.http : undefined
  if (!http) return null
  if (entry.pending) return { text: '…', tone: 'text-muted-foreground' }
  if (entry.error) return { text: t('webapi.status_capture_failed'), tone: 'text-red-600 dark:text-red-400' }
  if (http.httpStatus != null) return { text: `→ ${http.httpStatus}`, tone: httpStatusTone(http.httpStatus) }
  if (http.status === 'completed') return { text: t('webapi.noResponse'), tone: 'text-orange-600 dark:text-orange-400' }
  const key = HTTP_STATUS_LABEL_KEYS[http.status]
  const label = key ? t(key) : t('webapi.status_unknown')
  return { text: `→ ${label}`, tone: http.status === 'denied' || http.status === 'capture_failed' ? 'text-red-600 dark:text-red-400' : 'text-muted-foreground' }
}

// 状态列：进行中 / 完成 / 错误（HTTP 行的状态码由 httpPart 在摘要尾渲染，避免重复）。
function entryStatus(entry: ExecutionEntry, t: TFunction<'chat'>): { text: string; tone: string; pending: boolean } {
  if (entry.type === 'model') return { text: '', tone: '', pending: false }
  if (entry.pending) return { text: t('webapi.execRunning'), tone: 'text-muted-foreground', pending: true }
  if (entry.error) return { text: t('webapi.execFailed'), tone: 'text-red-600 dark:text-red-400', pending: false }
  const http = entry.biz?.kind === 'http' ? entry.biz.http : undefined
  if (http) {
    if (http.httpStatus != null) return { text: String(http.httpStatus), tone: httpStatusTone(http.httpStatus), pending: false }
    if (http.status === 'completed') return { text: t('webapi.noResponse'), tone: 'text-orange-600 dark:text-orange-400', pending: false }
    const key = HTTP_STATUS_LABEL_KEYS[http.status]
    if (key) {
      const tone = http.status === 'denied' || http.status === 'capture_failed'
        ? 'text-red-600 dark:text-red-400'
        : http.status === 'claimed' ? 'text-blue-600 dark:text-blue-400' : 'text-amber-600 dark:text-amber-400'
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
                ? 'flex h-[34px] items-center gap-2 px-3'
                : 'flex h-7 items-center gap-1.5 rounded px-2',
              active
                ? layout === 'side' ? 'bg-primary/10 text-primary' : 'bg-primary/10 font-medium text-primary'
                : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
            )}
          >
            {layout === 'side' && (
              <span aria-hidden className={cn('absolute inset-y-0 left-0 w-0.5 bg-primary transition-opacity', active ? 'opacity-100' : 'opacity-0')} />
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

function JsonSection({ title, text, empty }: { title: string; text: string; empty?: string }) {
  return (
    <section className="min-w-0">
      <div className="flex h-7 items-center justify-between gap-2">
        <h4 className="text-[12px] font-medium text-foreground">{title}</h4>
        {text && <CopyButton text={text} />}
      </div>
      {text ? (
        <pre className="whitespace-pre-wrap break-all rounded-md border border-border/60 bg-muted/30 px-2 py-1.5 font-mono text-[13px] leading-[1.45] text-foreground" data-testid="execution-json">
          {text}
        </pre>
      ) : (
        <div className="rounded-md border border-dashed border-border/60 px-2 py-1.5 text-[12px] text-muted-foreground">{empty}</div>
      )}
    </section>
  )
}

function EntryDetail({ entry, onViewEvidence, onViewFinding }: {
  entry: ExecutionEntry
  onViewEvidence: (taskId: string, evidenceId: string) => void
  onViewFinding: (findingId: string) => void
}) {
  const { t } = useTranslation('chat')
  const biz: ExecutionBiz | undefined = entry.biz
  const http = biz?.kind === 'http' ? biz.http : undefined

  if (entry.type === 'model') {
    const usage = entry.usage
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-3">
        <div className="flex h-7 shrink-0 items-center gap-2 text-[12px] text-muted-foreground">
          <span className="font-medium text-foreground">{t('webapi.execModelMessage')}</span>
          <span className="font-mono tabular-nums">{clockOf(entry.timestamp)}</span>
        </div>
        {usage && (
          <div className="shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground" title={`${t('tokenInput', { count: usage.input_tokens || 0 })} · ${t('tokenOutput', { count: usage.output_tokens || 0 })}`}>
            ↓ {(usage.input_tokens || 0).toLocaleString()} ↑ {(usage.output_tokens || 0).toLocaleString()} tokens
          </div>
        )}
        <div className="min-w-0 text-[13px] leading-relaxed">
          {(entry.content || '').trim()
            ? <MarkdownContent content={entry.content || ''} />
            : <span className="text-muted-foreground">—</span>}
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-3">
      <div className="flex min-h-7 shrink-0 flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-muted-foreground">
        <span className="font-medium text-foreground">{t('webapi.execToolCall')}</span>
        {entry.toolName && <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground">{entry.toolName}</span>}
        <span className="font-mono tabular-nums">{clockOf(entry.timestamp)}</span>
        {entry.durationMs != null && <span className="font-mono tabular-nums">{formatDuration(entry.durationMs)}</span>}
        {entry.pending && <span className="inline-flex items-center gap-1 text-muted-foreground"><Loader2 className="h-3 w-3 animate-spin" />{t('webapi.execRunning')}</span>}
        {entry.error && <span className="text-red-600 dark:text-red-400">{t('webapi.execFailed')}</span>}
      </div>

      {http && (
        <div className="flex shrink-0 flex-col gap-1 rounded-md border border-border/60 px-2 py-1.5 text-[12px]">
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
        </div>
      )}

      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {biz?.kind === 'http' && http?.evidenceId && http.taskId && (
          <Button type="button" variant="outline" size="xs" onClick={() => onViewEvidence(http.taskId!, http.evidenceId!)}>
            {t('webapi.execViewInTraffic')}
          </Button>
        )}
        {biz?.kind === 'finding' && entry.error && <span className="min-w-0 break-all text-[12px] text-red-600 dark:text-red-400">{biz.errorMessage}</span>}
        {biz?.kind === 'finding' && !entry.pending && biz.findingId && (
          <Button type="button" variant="outline" size="xs" onClick={() => onViewFinding(biz.findingId!)}>
            {t('webapi.execViewFinding')}
          </Button>
        )}
      </div>

      <JsonSection title={t('toolCard.arguments')} text={prettyJson(entry.argsText)} empty={entry.pending ? t('webapi.execRunning') : '—'} />
      {entry.pending ? (
        <div className="rounded-md border border-dashed border-border/60 px-2 py-1.5 text-[12px] text-muted-foreground">{t('webapi.execRunning')}…</div>
      ) : (
        <JsonSection title={t('toolCard.result')} text={prettyJson(entry.resultText)} empty="—" />
      )}
    </div>
  )
}

// ── 主组件：三栏 / 堆叠 ────────────────────────────────────────────────────

export default function WebApiExecutionTimeline({ data, onViewEvidence, onViewFinding }: TimelineProps) {
  const { t } = useTranslation('chat')
  const [filter, setFilter] = useState<FilterKey>('all')
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [newCount, setNewCount] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(true)
  const prevCountRef = useRef(data.entries.length)
  const turnRefs = useRef(new Map<string, HTMLDivElement>())

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

  // 行序列：按 turnKey 变化插入轮次头；轮头时间取该轮首条可见条目时间，
  // 工具计数为该轮当前筛选下的工具条目数。
  const rows = useMemo(() => {
    const out: Array<{ kind: 'turn'; turn: ExecutionTurn; timestamp: number; toolCount: number } | { kind: 'entry'; entry: ExecutionEntry }> = []
    const toolCountByTurn = new Map<string, number>()
    for (const entry of visible) {
      if (entry.type === 'tool') toolCountByTurn.set(entry.turnKey, (toolCountByTurn.get(entry.turnKey) || 0) + 1)
    }
    let lastTurn = ''
    for (const entry of visible) {
      if (entry.turnKey !== lastTurn) {
        lastTurn = entry.turnKey
        out.push({
          kind: 'turn',
          turn: { key: entry.turnKey, index: data.turnByKey[entry.turnKey] || 0, timestamp: entry.timestamp },
          timestamp: entry.timestamp,
          toolCount: toolCountByTurn.get(entry.turnKey) || 0,
        })
      }
      out.push({ kind: 'entry', entry })
    }
    return out
  }, [visible, data.turnByKey])

  const selected = useMemo(() => data.entries.find((entry) => entry.id === selectedId) || null, [data.entries, selectedId])

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
          key={`turn:${item.turn.key}`}
          ref={(el) => { if (el) turnRefs.current.set(item.turn.key, el); else turnRefs.current.delete(item.turn.key) }}
          className="flex h-7 items-center gap-2 border-b border-border/40 bg-muted/40 px-3 text-[11px] text-muted-foreground"
        >
          <span className="font-medium text-foreground">{t('webapi.execTurn', { index: item.turn.index })}</span>
          <span className="font-mono tabular-nums">{clockOf(item.timestamp)}</span>
          <span aria-hidden>·</span>
          <span className="font-mono tabular-nums">{t('webapi.execTurnTools', { count: item.toolCount })}</span>
        </div>
      )
    }
    const entry = item.entry
    const icon = entryIcon(entry)
    const Icon = icon
    const status = entryStatus(entry, t)
    const selectedEntry = selectedId === entry.id
    const httpPart = entry.biz?.kind === 'http' ? httpResultPart(entry, t) : null
    const summary = entrySummary(entry, t)
    return (
      <div key={entry.id}>
        <button
          type="button"
          role="row"
          aria-selected={selectedEntry}
          onClick={() => setSelectedId(selectedEntry ? null : entry.id)}
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
            {entry.biz?.kind === 'http' && entry.biz.http?.sent === false && (
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
              <EntryDetail entry={entry} onViewEvidence={onViewEvidence} onViewFinding={onViewFinding} />
            </div>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="webapi-exec flex min-h-0 flex-1 flex-col overflow-hidden">
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
            <EntryDetail entry={selected} onViewEvidence={onViewEvidence} onViewFinding={onViewFinding} />
          ) : (
            <div className="flex flex-1 items-center justify-center px-4 text-center text-sm text-muted-foreground">{t('webapi.execSelectEntry')}</div>
          )}
        </aside>
      </div>
    </div>
  )
}
