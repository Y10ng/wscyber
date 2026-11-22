import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Camera, CheckCircle2, ChevronDown, ChevronLeft, ChevronRight, ClipboardCopy, Download, ExternalLink,
  HelpCircle, Loader2, Menu, MoreHorizontal, Play, RotateCw, Send, Sparkles, Wrench, X, XCircle,
} from 'lucide-react'
import {
  Button, Dialog, DialogContent, DropdownMenu, DropdownMenuContent,
  DropdownMenuItem, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent,
  DropdownMenuSubTrigger, DropdownMenuTrigger, Input, Spinner, Switch, Tooltip, TooltipContent,
  TooltipTrigger, useConfirm,
} from '@cyber/ui'
import { newID } from '@cyber/aop'
import { cn } from '@cyber/theme'
import {
  actWebApiNotification, APIError, exportWebApiVulnCard,
  getWebApiVulnCard, getWebApiVulnCardCurl, listWebApiAssetVulns, mergeWebApiVulns,
  moveWebApiVulnMember, replayWebApiVulnStep, requestWebApiVulnRetest, saveWebApiVulnTitle,
  ReplayAPIError, VulnRetestAPIError, webApiEvidenceImageURL, webApiVulnExportFilename,
  type WebApiNotification, type WebApiReplayResult, type WebApiVuln, type WebApiVulnCard,
  type WebApiVulnCardContrast, type WebApiVulnCardEvent, type WebApiVulnCardEvidence,
  type WebApiVulnCardMember, type WebApiVulnCardReplay, type WebApiVulnCardRepro,
  type WebApiVulnCardStep, type WebApiVulnCardVerification, type WebApiVulnSeverity,
  type WebApiVulnStatus,
} from '../api'
import {
  DeadlineText, DispositionBadge, NotificationStatusBadge, ReplayHintBadge, SeverityBadge, StatusBadge,
  VerdictBadge,
} from './ui-badges'
import HttpPairBlock, {
  displayPath, rawRequestText, responseBodyLines, useEvidenceHttpView,
} from './HttpPairBlock'
import WebApiEvidenceImageDialog from './WebApiEvidenceImageDialog'
import VulnTriageForm from './VulnTriageForm'
import VulnNotifyDialog from './VulnNotifyDialog'
import { DialogFooterRow, DialogHeaderRow } from './ui-dialog'
import { downloadText } from '../lib/download'

interface VulnCardPageProps {
  vulnKey: string
  onBack: () => void
  onSwitch: (key: string) => void
  onOpenAssetVuln: (assetId: string, vulnId: string) => void
  onOpenTaskFinding: (taskId: string, findingId: string) => void
  /** 在任务工作区流量视图定位某条证据（一键重放结果“在流量中查看”）。 */
  onOpenTaskEvidence: (taskId: string, evidenceId: string) => void
  /** 打开复测会话（进行中行与 in_flight 错误里的链接）。 */
  onOpenSession: (sessionId: string) => void
  /** 推送对话框“前往集成”（未配置时）。 */
  onOpenIntegrations: () => void
  onOpenMenu: () => void
}

function pad(value: number): string {
  return String(value).padStart(2, '0')
}

/** YYYY-MM-DD HH:mm（本地时区，24 小时制）。 */
function formatFull(iso: string | null | undefined): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** MM-DD HH:mm（本地时区，24 小时制）。 */
function formatShort(iso: string | null | undefined): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

/** MM-DD HH:mm:ss（本地时区，24 小时制）；重放结果面板用。 */
function formatShortSeconds(iso: string | null | undefined): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

/** 千分位整数；空值显示 —。 */
function formatCount(value: number | null | undefined): string {
  if (value == null) return '—'
  return String(value).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function isPathSegment(segment: string): boolean {
  return /^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|TRACE|CONNECT)\s+\//.test(segment) && segment.includes('/')
}

function severityColor(severity: WebApiVulnSeverity): string {
  return severity ? `var(--sev-${severity}-text)` : 'var(--border-strong)'
}

const LIFECYCLE_DOT: Record<string, string> = {
  candidate: '#2563EB',
  verification: '#7C3AED',
  triage: '#64748B',
  notify: '#0891B2',
  replay: '#D97706',
  retest_request: '#D97706',
  retest: '#D97706',
  fixed: '#059669',
}

const ACTOR_KEYS: Record<string, string> = {
  ai: 'vulnCard.actorAi',
  system: 'vulnCard.actorSystem',
}

const RETEST_KEYS: Record<string, string> = {
  reproducible: 'webapi.retestReproducible',
  fixed_in_scope: 'webapi.retestFixedInScope',
  inconclusive: 'webapi.retestInconclusive',
}

const NOTIFY_KIND_KEYS: Record<WebApiNotification['kind'], string> = {
  new: 'vulnCard.notifyKindNew',
  regression: 'vulnCard.notifyKindRegression',
  manual: 'vulnCard.notifyKindManual',
  approval_pending: 'vulnCard.notifyKindApprovalPending',
  schedule_failed: 'vulnCard.notifyKindScheduleFailed',
  weekly_digest: 'vulnCard.notifyKindWeeklyDigest',
}

function retestText(t: (key: string) => string, conclusion: string | null | undefined): string {
  return conclusion ? t(RETEST_KEYS[conclusion] || 'webapi.retestInconclusive') : t('vulnCard.noRetest')
}

function sourceLabelKey(item: WebApiVulnCardEvidence): string {
  if (item.source === 'retest') return 'vulnCard.sourceRetest'
  if (item.relation === 'key_step') return 'vulnCard.relationKeyStep'
  if (item.relation === 'contrast') return 'vulnCard.relationContrast'
  if (item.source === 'candidate') return 'vulnCard.sourceCandidate'
  return 'vulnCard.sourceVerification'
}

function Card({ children, className, style }: { children: ReactNode; className?: string; style?: CSSProperties }) {
  return <section className={cn('rounded-lg border border-border bg-card p-4', className)} style={style}>{children}</section>
}

function SectionTitle({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-[12px] font-semibold text-foreground">{children}</h2>
      {actions}
    </div>
  )
}

function Skeleton() {
  return (
    <div className="space-y-4">
      <div className="h-4 w-56 animate-pulse rounded bg-muted" />
      <div className="h-[104px] animate-pulse rounded-lg border border-border bg-card" />
      <div className="flex flex-col items-start gap-5 min-[1024px]:flex-row">
        <div className="flex-1 space-y-4">
          {[150, 260, 150, 90].map((height, index) => (
            <div key={index} className="animate-pulse rounded-lg border border-border bg-card" style={{ height }} />
          ))}
        </div>
        <div className="w-full space-y-3 min-[1024px]:w-[340px] min-[1440px]:w-[376px]">
          {[220, 130, 220].map((height, index) => (
            <div key={index} className="animate-pulse rounded-lg border border-border bg-card" style={{ height }} />
          ))}
        </div>
      </div>
    </div>
  )
}

/** 一键重放结果：结论徽标 + 与原步骤的确定性对比 + 新证据原文。 */
function ReplayResultPanel({ result, masked, onClose, onOpenEvidence }: {
  result: WebApiReplayResult
  masked: boolean
  onClose: () => void
  onOpenEvidence: (taskId: string, evidenceId: string) => void
}) {
  const { t } = useTranslation('chat')
  const { view, loading, error, reload } = useEvidenceHttpView(result.task_id, result.evidence_id, masked)
  const baseline = result.original.marks_found.response
  const replayMarks = result.replayed.marks_found.response
  return (
    <div className="space-y-3 rounded-lg border border-border bg-card p-3">
      <div className="flex flex-wrap items-center gap-2">
        <ReplayHintBadge hint={result.comparison.verdict_hint} />
        <span className="text-[11px] tabular-nums text-muted-foreground">{formatShortSeconds(result.at)}</span>
        <span className="min-w-0 flex-1 text-[11px] text-muted-foreground">{result.note}</span>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className="shrink-0 text-muted-foreground"
          aria-label={t('vulnCard.replayClose')}
          onClick={onClose}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-[12px]">
          <thead>
            <tr className="border-b border-border text-left text-muted-foreground">
              <th className="py-1.5 pr-3 font-medium" />
              <th className="py-1.5 pr-3 font-medium">{t('vulnCard.replayOriginal')}</th>
              <th className="py-1.5 font-medium">{t('vulnCard.replayReplayed')}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-border">
            <tr>
              <td className="py-1.5 pr-3 text-secondary-foreground">{t('vulnCard.replayStatus')}</td>
              <td className="py-1.5 pr-3 font-mono">{result.original.status_code ?? '—'}</td>
              <td className="py-1.5 font-mono">{result.replayed.status_code ?? '—'}</td>
            </tr>
            <tr>
              <td className="py-1.5 pr-3 text-secondary-foreground">{t('vulnCard.replayBodyBytes')}</td>
              <td className="py-1.5 pr-3 font-mono">{formatCount(result.original.body_bytes)}</td>
              <td className="py-1.5 font-mono">
                {formatCount(result.replayed.body_bytes)}
                {result.replayed.capture_truncated ? <span className="text-muted-foreground">{t('vulnCard.replayCaptureTruncated')}</span> : null}
              </td>
            </tr>
            <tr>
              <td className="py-1.5 pr-3 align-top text-secondary-foreground">{t('vulnCard.replayMarks')}</td>
              <td className="py-1.5 pr-3 align-top font-mono text-[11px]">
                {baseline.length === 0 ? '—' : baseline.map((mark, index) => (
                  <div key={index} className="break-all">✓ {mark}</div>
                ))}
              </td>
              <td className="py-1.5 align-top font-mono text-[11px]">
                {baseline.length === 0 ? '—' : baseline.map((mark, index) => {
                  const present = replayMarks.includes(mark)
                  return (
                    <div key={index} className="break-all">
                      <span className={present ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'}>
                        {present ? '✓' : '✗'}
                      </span>{' '}
                      {mark}
                    </div>
                  )
                })}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div className="space-y-1.5">
        <div className="text-[11px] font-medium text-muted-foreground">{t('vulnCard.replayEvidenceTitle')}</div>
        <HttpPairBlock
          view={view}
          loading={loading}
          error={error}
          retry={reload}
          requestMarks={result.replayed.marks_found.request}
          responseMarks={result.replayed.marks_found.response}
          masked={masked}
          foldHeaders
        />
      </div>

      <button
        type="button"
        onClick={() => onOpenEvidence(result.task_id, result.evidence_id)}
        className="text-left text-[11.5px] font-medium text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        {t('vulnCard.replayViewTraffic')}
      </button>
    </div>
  )
}

/** 重放失败提示条（不弹窗），可关闭。 */
function ReplayErrorPanel({ message, onClose }: { message: string; onClose: () => void }) {
  const { t } = useTranslation('chat')
  return (
    <div
      role="alert"
      className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-[12px] text-amber-700 dark:text-amber-300"
    >
      <span className="min-w-0 break-words">{message}</span>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        className="ml-auto shrink-0 text-amber-700/80 hover:text-amber-800 dark:text-amber-300/80 dark:hover:text-amber-200"
        aria-label={t('vulnCard.replayClose')}
        onClick={onClose}
      >
        <X className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}

/** 申请复测失败提示条（不弹窗），与重放错误同为卡内琥珀提示；in_flight 可打开复测会话。 */
function RetestErrorPanel({ message, sessionId, onOpenSession, onClose }: {
  message: string
  sessionId?: string
  onOpenSession: (sessionId: string) => void
  onClose: () => void
}) {
  const { t } = useTranslation('chat')
  return (
    <div
      role="alert"
      className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-[12px] text-amber-700 dark:text-amber-300"
    >
      <span className="min-w-0 break-words">{message}</span>
      {sessionId ? (
        <Button
          type="button"
          variant="link"
          size="sm"
          className="h-auto shrink-0 p-0 text-[11.5px] text-amber-700 underline-offset-2 hover:text-amber-800 dark:text-amber-300 dark:hover:text-amber-200"
          onClick={() => onOpenSession(sessionId)}
        >
          {t('vulnCard.openRetestSession')}
        </Button>
      ) : null}
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        className="ml-auto shrink-0 text-amber-700/80 hover:text-amber-800 dark:text-amber-300/80 dark:hover:text-amber-200"
        aria-label={t('vulnCard.retestClose')}
        onClick={onClose}
      >
        <X className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}

function StepBlock({
  repro, step, index, masked, onScreenshot, onNotify, onReplay,
  replayBusy, replayBlocked, replayCooldownSeconds,
}: {
  repro: WebApiVulnCardRepro
  step: WebApiVulnCardStep
  index: number
  masked: boolean
  onScreenshot: (url: string, filename: string) => void
  onNotify: (message: string) => void
  onReplay: (step: number) => void
  replayBusy: boolean
  replayBlocked: boolean
  replayCooldownSeconds: number
}) {
  const { t } = useTranslation('chat')
  const { view, loading, error, reload } = useEvidenceHttpView(repro.task_id, step.evidence_id, masked)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(rawRequestText(view, masked))
      onNotify(t('vulnCard.requestCopied'))
    } catch {
      onNotify(t('vulnCard.copyFailed'))
    }
  }

  const screenshotURL = webApiEvidenceImageURL(repro.task_id, step.evidence_id, {
    mask: true,
    marks: [...step.request_marks_found, ...step.response_marks_found],
  })

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/15 text-[11px] font-semibold text-primary">
            {index + 1}
          </span>
          <span className="min-w-0 break-words text-[12px] font-semibold text-foreground" title={step.title}>{step.title}</span>
          <span className="w-full shrink-0 break-all pl-7 font-mono text-[11px] text-muted-foreground sm:w-auto sm:pl-0">
            {step.method} {displayPath(step.url)}
            {step.status_code != null ? ` → ${step.status_code}` : ''}
          </span>
        </div>
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="gap-1"
            disabled={replayBusy || replayBlocked || replayCooldownSeconds > 0}
            onClick={() => onReplay(index + 1)}
          >
            {replayBusy ? <Loader2 className="h-3 w-3 animate-spin" /> : <Play className="h-3 w-3" />}
            {replayBusy
              ? t('vulnCard.replaying')
              : replayCooldownSeconds > 0
                ? t('vulnCard.replayWait', { seconds: replayCooldownSeconds })
                : t('vulnCard.replayStep')}
          </Button>
          <Button type="button" variant="ghost" size="xs" className="gap-1" onClick={() => void copy()} disabled={!view}>
            <ClipboardCopy className="h-3 w-3" />
            {t('vulnCard.copy')}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="gap-1"
            onClick={() => onScreenshot(screenshotURL, `evidence-${step.evidence_id}.png`)}
          >
            <Camera className="h-3 w-3" />
            {t('vulnCard.screenshot')}
          </Button>
        </div>
      </div>
      <HttpPairBlock
        view={view}
        loading={loading}
        error={error}
        retry={reload}
        requestMarks={step.request_marks_found}
        responseMarks={step.response_marks_found}
        masked={masked}
      />
      {step.mark_missing && (
        <p className="text-[11px] text-amber-600 dark:text-amber-400">{t('vulnCard.markMissing')}</p>
      )}
    </div>
  )
}

function ContrastSide({ title, tone, method, url, status, body }: {
  title: string
  tone: 'ok' | 'danger'
  method: string
  url: string
  status: number | null
  body: string[]
}) {
  const summary = `${method} ${displayPath(url)}${status != null ? ` → ${status}` : ''}`
  // 对照区只展示前 8 行、单行最多 200 字符；绝不渲染全文，并交给自身横向滚动保持高度。
  const clip = (text: string) => {
    const chars = Array.from(text)
    return chars.length > 200 ? chars.slice(0, 200).join('') + '…' : text
  }
  return (
    <div className="min-w-0 overflow-hidden rounded-md border border-border bg-[var(--bg-code-light)]">
      <div
        className="border-b border-border px-3 py-1 text-[11px] font-medium"
        style={{
          background: tone === 'ok' ? 'var(--st-fixed-bg)' : 'var(--st-new-bg)',
          color: tone === 'ok' ? 'var(--st-fixed-text)' : 'var(--st-new-text)',
        }}
      >
        {title}
      </div>
      <div className="overflow-x-auto py-2 font-mono text-[11.5px] leading-[1.65]">
        <div
          className="flex items-baseline whitespace-pre px-[7.5px] font-medium"
          style={{
            background: tone === 'ok' ? 'var(--code-hl-ok-bg)' : 'var(--code-hl-bg)',
            borderLeft: `2.5px solid ${tone === 'ok' ? 'var(--code-hl-ok-border)' : 'var(--code-hl-border)'}`,
          }}
        >
          <span className="w-6 shrink-0 select-none text-[10.5px] opacity-65">1</span>
          <span className="min-w-0">{clip(summary)}</span>
        </div>
        {body.slice(0, 8).map((line, index) => (
          <div
            key={index}
            className={cn(
              'flex items-baseline whitespace-pre px-[7.5px]',
              tone === 'ok' && 'bg-[var(--code-hl-ok-bg)]',
            )}
            style={tone === 'danger' ? { background: 'var(--code-hl-bg)', borderLeft: '2.5px solid var(--code-hl-border)' } : undefined}
          >
            <span className="w-6 shrink-0 select-none text-[10.5px] opacity-65">{index + 2}</span>
            <span className="min-w-0">{clip(line)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function ContrastSection({ repro, masked }: { repro: WebApiVulnCardRepro; masked: boolean }) {
  const { t } = useTranslation('chat')
  const contrast = repro.contrast as WebApiVulnCardContrast
  const [mode, setMode] = useState<'side' | 'summary'>('side')
  const normal = useEvidenceHttpView(repro.task_id, contrast.normal_evidence_id, masked)
  const abnormal = useEvidenceHttpView(repro.task_id, contrast.abnormal_evidence_id, masked)
  const normalRef = contrast.normal
  const abnormalRef = contrast.abnormal
  const bodyBytes = (view: typeof normal.view) => {
    const body = view?.response?.body
    return body ? new TextEncoder().encode(body).length : 0
  }

  return (
    <Card className="space-y-3">
      <SectionTitle actions={(
        <div className="inline-flex rounded-md border border-border bg-[var(--bg-subtle)] p-0.5 text-[11px]">
          {(['side', 'summary'] as const).map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setMode(value)}
              className={cn(
                'rounded px-2 py-0.5 font-medium transition-colors',
                mode === value ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {t(value === 'side' ? 'vulnCard.contrastSide' : 'vulnCard.contrastSummary')}
            </button>
          ))}
        </div>
      )}>
        {t('vulnCard.contrastTitle')}
      </SectionTitle>

      {mode === 'side' ? (
        <div className="grid grid-cols-1 gap-3 min-[769px]:grid-cols-2">
          <ContrastSide
            title={`${t('vulnCard.contrastNormal')} · ${normalRef?.method || ''} ${displayPath(normalRef?.url || '')}`}
            tone="ok"
            method={normalRef?.method || ''}
            url={normalRef?.url || ''}
            status={normalRef?.status_code ?? null}
            body={responseBodyLines(normal.view)}
          />
          <ContrastSide
            title={`${t('vulnCard.contrastAbnormal')} · ${abnormalRef?.method || ''} ${displayPath(abnormalRef?.url || '')}`}
            tone="danger"
            method={abnormalRef?.method || ''}
            url={abnormalRef?.url || ''}
            status={abnormalRef?.status_code ?? null}
            body={responseBodyLines(abnormal.view)}
          />
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-[12px]">
            <thead>
              <tr className="border-b border-border text-left text-muted-foreground">
                <th className="py-1.5 font-medium">{t('vulnCard.contrastItem')}</th>
                <th className="py-1.5 font-medium">{t('vulnCard.contrastNormal')}</th>
                <th className="py-1.5 font-medium">{t('vulnCard.contrastAbnormal')}</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border font-mono">
              <tr>
                <td className="py-1.5 font-sans text-secondary-foreground">{t('vulnCard.contrastRequest')}</td>
                <td className="py-1.5 text-emerald-600 dark:text-emerald-400">{normalRef ? `${normalRef.method} ${displayPath(normalRef.url)}` : '—'}</td>
                <td className="py-1.5 text-rose-600 dark:text-rose-400">{abnormalRef ? `${abnormalRef.method} ${displayPath(abnormalRef.url)}` : '—'}</td>
              </tr>
              <tr>
                <td className="py-1.5 font-sans text-secondary-foreground">{t('vulnCard.contrastStatus')}</td>
                <td className="py-1.5">{normalRef?.status_code ?? '—'}</td>
                <td className="py-1.5 text-rose-600 dark:text-rose-400">{abnormalRef?.status_code ?? '—'}</td>
              </tr>
              <tr>
                <td className="py-1.5 font-sans text-secondary-foreground">{t('vulnCard.contrastBodyLength')}</td>
                <td className="py-1.5">{normal.loading ? '…' : bodyBytes(normal.view)}</td>
                <td className="py-1.5 text-rose-600 dark:text-rose-400">{abnormal.loading ? '…' : bodyBytes(abnormal.view)}</td>
              </tr>
              <tr>
                <td className="py-1.5 font-sans text-secondary-foreground">{t('vulnCard.contrastNote')}</td>
                <td colSpan={2} className="py-1.5 font-sans text-secondary-foreground">{contrast.note || '—'}</td>
              </tr>
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}

function ReplayRecordEvidence({ taskId, evidenceId, masked }: { taskId: string; evidenceId: string; masked: boolean }) {
  const { view, loading, error, reload } = useEvidenceHttpView(taskId, evidenceId, masked)
  return (
    <div className="pb-2">
      <HttpPairBlock
        view={view}
        loading={loading}
        error={error}
        retry={reload}
        requestMarks={[]}
        responseMarks={[]}
        masked={masked}
        foldHeaders
      />
    </div>
  )
}

function ReplayRecordRow({ item, taskId, masked, open, onToggle }: {
  item: WebApiVulnCardReplay
  taskId: string
  masked: boolean
  open: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation('chat')
  return (
    <div className="border-b border-border/60 last:border-0">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full flex-wrap items-center gap-2 py-1 text-left text-[11.5px] transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        <ChevronDown className={cn('h-3 w-3 shrink-0 text-muted-foreground transition-transform', !open && '-rotate-90')} />
        <span className="tabular-nums text-muted-foreground">{formatShort(item.at)}</span>
        <span className="text-muted-foreground">·</span>
        <span>{t('vulnCard.replayStepNo', { step: item.step })}</span>
        <span className="text-muted-foreground">·</span>
        <span className="font-mono">{item.status_code ?? '—'}</span>
        <ReplayHintBadge hint={item.verdict_hint} />
      </button>
      {open && (taskId
        ? <ReplayRecordEvidence taskId={taskId} evidenceId={item.evidence_id} masked={masked} />
        : <p className="pb-2 text-[11px] text-muted-foreground">{t('vulnCard.replayTaskMissing')}</p>)}
    </div>
  )
}

interface AssetVulnActions {
  targets: WebApiVuln[] | null
  targetsLoading: boolean
  acting: string | null
  loadTargets: () => void
  moveMember: (member: WebApiVulnCardMember, targetId: string | null) => void
}

function Appendix({ card, masked, onOpenTaskFinding, assetActions }: {
  card: WebApiVulnCard
  masked: boolean
  onOpenTaskFinding: (taskId: string, findingId: string) => void
  /** 仅资产漏洞条目（header.asset 非空且非 finding: 键）传入；finding: 条目为 null。 */
  assetActions: AssetVulnActions | null
}) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState<Set<string>>(new Set())
  const toggle = (key: string) => {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }
  const { verifications, members, evidence_count: evidenceCount, evidence, changes } = card.appendix
  const replays = card.replays
  const latestVerification = verifications[0]?.saved_at || verifications[0]?.created_at || null
  const latestEvidence = evidence[0]?.created_at || null
  const latestChange = changes[0]?.at || null

  return (
    <section className="overflow-hidden rounded-lg border border-border bg-card">
      <div className="flex items-center justify-between border-b border-border bg-[var(--bg-subtle)] px-4 py-2.5">
        <span className="text-[12px] font-semibold text-foreground">{t('vulnCard.appendix')}</span>
      </div>
      <div className="divide-y divide-border">
        <AppendixItem
          title={t('vulnCard.verifications')}
          meta={`${t('vulnCard.countItems', { count: verifications.length })}${latestVerification ? ` · ${formatShort(latestVerification)}` : ''}`}
          open={open.has('verifications')}
          onToggle={() => toggle('verifications')}
        >
          {verifications.length === 0 ? (
            <p className="text-[12px] text-muted-foreground">{t('vulnCard.noVerifications')}</p>
          ) : verifications.map((item) => (
            <VerificationRow key={item.verification_id} item={item} />
          ))}
        </AppendixItem>

        <AppendixItem
          title={t('vulnCard.allEvidence')}
          meta={`${t('vulnCard.countItems', { count: evidenceCount })}${latestEvidence ? ` · ${formatShort(latestEvidence)}` : ''}`}
          open={open.has('evidence')}
          onToggle={() => toggle('evidence')}
        >
          <div className="space-y-0.5">
            {evidence.map((item) => (
              <div key={`${item.task_id}:${item.evidence_id}`} className="flex flex-wrap items-center gap-2 border-b border-border/60 py-1 font-mono text-[11.5px] last:border-0">
                <span className="min-w-0 break-all">{item.method} {displayPath(item.url)}{item.status_code != null ? ` → ${item.status_code}` : ''}</span>
                <span className="rounded bg-muted px-1.5 py-px font-sans text-[10.5px] text-muted-foreground">{t(sourceLabelKey(item))}</span>
                <span className="ml-auto shrink-0 text-muted-foreground">{formatShort(item.created_at)}</span>
              </div>
            ))}
            {evidenceCount > 200 && (
              <p className="pt-1 text-[11px] text-muted-foreground">{t('vulnCard.onlyFirst200')}</p>
            )}
          </div>
        </AppendixItem>

        <AppendixItem
          title={t('vulnCard.replays')}
          meta={t('vulnCard.countItems', { count: replays.length })}
          open={open.has('replays')}
          onToggle={() => toggle('replays')}
        >
          {replays.length === 0 ? (
            <p className="text-[12px] text-muted-foreground">{t('vulnCard.noReplays')}</p>
          ) : replays.map((item) => (
            <ReplayRecordRow
              key={item.replay_id}
              item={item}
              taskId={item.task_id || card.repro?.task_id || ''}
              masked={masked}
              open={open.has(`replay:${item.replay_id}`)}
              onToggle={() => toggle(`replay:${item.replay_id}`)}
            />
          ))}
        </AppendixItem>

        <AppendixItem
          title={t('vulnCard.relatedMembers')}
          meta={t('vulnCard.countItems', { count: members.length })}
          open={open.has('members')}
          onToggle={() => toggle('members')}
        >
          {members.length === 0 ? (
            <p className="text-[12px] text-muted-foreground">—</p>
          ) : members.map((item) => (
            <div key={`${item.task_id}:${item.finding_id}`} className="flex min-w-0 items-center gap-1">
              <button
                type="button"
                onClick={() => onOpenTaskFinding(item.task_id, item.finding_id)}
                className="flex min-w-0 flex-1 items-center gap-2 rounded px-1 py-1.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <span className="shrink-0 font-mono text-[11.5px] text-foreground">{item.number}</span>
                <span className="min-w-0 flex-1 truncate text-[12px] text-secondary-foreground" title={item.title}>{item.title}</span>
                <VerdictBadge verdict={item.latest_verdict} showModelLabel={false} className="shrink-0" />
              </button>
              {assetActions && (
                <MemberActions
                  member={item}
                  memberCount={members.length}
                  actions={assetActions}
                />
              )}
            </div>
          ))}
        </AppendixItem>

        <AppendixItem
          title={t('vulnCard.changes')}
          meta={`${t('vulnCard.countItems', { count: changes.length })}${latestChange ? ` · ${formatShort(latestChange)}` : ''}`}
          open={open.has('changes')}
          onToggle={() => toggle('changes')}
        >
          {changes.length === 0 ? (
            <p className="text-[12px] text-muted-foreground">{t('vulnCard.noChanges')}</p>
          ) : changes.map((change) => (
            <div key={change.id} className="flex gap-2 border-b border-border/60 py-1 text-[11.5px] last:border-0">
              <span className="shrink-0 text-muted-foreground">{formatShort(change.at)}</span>
              <span className="shrink-0 text-muted-foreground">{change.actor}</span>
              <span className="min-w-0 break-words text-secondary-foreground">{change.summary}</span>
            </div>
          ))}
        </AppendixItem>
      </div>
    </section>
  )
}

function AppendixItem({ title, meta, open, onToggle, children }: {
  title: string
  meta: string
  open: boolean
  onToggle: () => void
  children: ReactNode
}) {
  return (
    <div>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-3 px-4 py-2.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        <span className="flex items-center gap-2 text-[12px] font-medium text-foreground">
          <ChevronDown className={cn('h-3.5 w-3.5 text-muted-foreground transition-transform', !open && '-rotate-90')} />
          {title}
        </span>
        <span className="shrink-0 text-[11px] text-muted-foreground">{meta}</span>
      </button>
      {open && <div className="space-y-2 px-4 pb-3.5 pt-0.5 text-[12px] text-secondary-foreground">{children}</div>}
    </div>
  )
}

function MemberActions({ member, memberCount, actions }: {
  member: WebApiVulnCardMember
  memberCount: number
  actions: AssetVulnActions
}) {
  const { t } = useTranslation('chat')
  const busy = actions.acting !== null
  const canSplit = memberCount > 1
  return (
    <DropdownMenu onOpenChange={(next) => { if (next && actions.targets === null && !actions.targetsLoading) actions.loadTargets() }}>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={t('webapi.vulnMemberActions')}
          disabled={busy}
          className="shrink-0 text-muted-foreground"
        >
          {actions.acting?.startsWith(`${member.finding_id}:`) ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <MoreHorizontal className="h-3.5 w-3.5" />}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuSub>
          <DropdownMenuSubTrigger disabled={actions.targetsLoading}>{t('webapi.vulnMove')}</DropdownMenuSubTrigger>
          <DropdownMenuSubContent>
            {actions.targets === null || actions.targetsLoading ? (
              <DropdownMenuItem disabled>{actions.targetsLoading ? t('shell.loading') : '—'}</DropdownMenuItem>
            ) : actions.targets.length === 0 ? (
              <DropdownMenuItem disabled>—</DropdownMenuItem>
            ) : actions.targets.map((target) => (
              <DropdownMenuItem key={target.id} onSelect={() => actions.moveMember(member, target.id)}>
                <StatusBadge status={target.status} />
                <span className="ml-2 max-w-[280px] truncate">{target.title}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuSubContent>
        </DropdownMenuSub>
        <DropdownMenuSeparator />
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="block" tabIndex={canSplit ? -1 : 0}>
              <DropdownMenuItem disabled={!canSplit || busy} onSelect={() => actions.moveMember(member, null)}>
                {t('webapi.vulnSplit')}
              </DropdownMenuItem>
            </span>
          </TooltipTrigger>
          {!canSplit && <TooltipContent>{t('webapi.vulnSplitSingle')}</TooltipContent>}
        </Tooltip>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function VerificationRow({ item }: { item: WebApiVulnCardVerification }) {
  const { t } = useTranslation('chat')
  return (
    <div className="space-y-1 border-b border-border/60 py-2 text-[11.5px] last:border-0">
      <div className="flex flex-wrap items-center gap-2">
        {item.verdict
          ? <VerdictBadge verdict={item.verdict} />
          : (
            <span className="inline-flex items-center gap-1">
              <span className="ws-st-badge ws-st-pending"><span aria-hidden className="ws-st-dot" />{t('vulnCard.verdictInProgress')}</span>
              <span className="ws-model-label">{t('webapi.verdictModelLabel')}</span>
            </span>
          )}
        {item.finding_number ? <span className="font-mono text-[10.5px] text-muted-foreground">{item.finding_number}</span> : null}
        <span className="ml-auto text-muted-foreground">{formatShort(item.saved_at || item.created_at)}</span>
      </div>
      <dl className="space-y-0.5">
        {item.method ? <div><dt className="inline font-semibold text-foreground">{t('vulnCard.verificationMethod')}：</dt><dd className="inline">{item.method}</dd></div> : null}
        {item.preconditions ? <div><dt className="inline font-semibold text-foreground">{t('vulnCard.verificationPreconditions')}：</dt><dd className="inline">{item.preconditions}</dd></div> : null}
        {item.summary ? <div><dt className="inline font-semibold text-foreground">{t('vulnCard.verificationSummary')}：</dt><dd className="inline">{item.summary}</dd></div> : null}
        {item.limitations ? <div><dt className="inline font-semibold text-foreground">{t('vulnCard.verificationLimitations')}：</dt><dd className="inline">{item.limitations}</dd></div> : null}
        {item.suggested_severity ? (
          <div className="flex flex-wrap items-center gap-1.5">
            <dt className="inline font-semibold text-foreground">{t('vulnCard.aiSuggestedSeverity')}：</dt>
            <dd className="inline-flex items-center gap-1.5">
              <SeverityBadge severity={item.suggested_severity} suggested />
              {item.severity_reason ? <span>{item.severity_reason}</span> : null}
            </dd>
          </div>
        ) : null}
      </dl>
    </div>
  )
}

function LifecycleTimeline({ events }: { events: WebApiVulnCardEvent[] }) {
  const { t } = useTranslation('chat')
  return (
    <Card className="space-y-3">
      <h3 className="text-[12px] font-semibold text-foreground">{t('vulnCard.lifecycleTitle')}</h3>
      {events.length === 0 ? (
        <p className="text-[12px] text-muted-foreground">—</p>
      ) : (
        <ol className="relative ml-2 space-y-3 border-l border-border">
          {events.map((event, index) => (
            <li key={`${event.at}-${event.kind}-${index}`} className="relative pl-4">
              <span
                aria-hidden
                className="absolute -left-[4.5px] top-1.5 h-2 w-2 rounded-full"
                style={{ background: LIFECYCLE_DOT[event.kind] || 'var(--text-muted)' }}
              />
              <div className="flex justify-between gap-2">
                <span className="text-[12px] font-medium text-foreground">{event.title}</span>
                <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{formatShort(event.at)}</span>
              </div>
              <div className="flex items-start gap-1.5">
                <span className="shrink-0 rounded bg-muted px-1 py-px text-[10px] text-muted-foreground">
                  {event.actor === 'user' ? t('vulnCard.actorUser') : t(ACTOR_KEYS[event.actor] || 'vulnCard.actorSystem')}
                </span>
                {event.detail ? (
                  <span className="line-clamp-2 min-w-0 text-[11px] text-muted-foreground" title={event.detail}>{event.detail}</span>
                ) : null}
              </div>
            </li>
          ))}
        </ol>
      )}
    </Card>
  )
}

export default function VulnCardPage({
  vulnKey, onBack, onSwitch, onOpenAssetVuln, onOpenTaskFinding, onOpenTaskEvidence, onOpenSession, onOpenIntegrations, onOpenMenu,
}: VulnCardPageProps) {
  const { t } = useTranslation('chat')
  const [card, setCard] = useState<WebApiVulnCard | null>(null)
  const [loading, setLoading] = useState(true)
  const [notFound, setNotFound] = useState(false)
  const [error, setError] = useState('')
  const [masked, setMasked] = useState(true)
  const [toast, setToast] = useState('')
  const [shot, setShot] = useState<{ url: string; filename: string } | null>(null)
  const [curlBusy, setCurlBusy] = useState(false)
  // 一键重放：按步骤保存进行中/结果/错误；429 的限频窗口对整张卡片生效。
  const [stepReplays, setStepReplays] = useState<Record<number, { busy: boolean; result: WebApiReplayResult | null; error: string }>>({})
  const [replayCooldownUntil, setReplayCooldownUntil] = useState(0)
  const [replayNow, setReplayNow] = useState(() => Date.now())
  const confirm = useConfirm()
  // 资产漏洞条目的人工归并操作（改标题 / 合并 / 移动 / 拆条）。
  const [targets, setTargets] = useState<WebApiVuln[] | null>(null)
  const [targetsLoading, setTargetsLoading] = useState(false)
  const [acting, setActing] = useState<string | null>(null)
  const [actionError, setActionError] = useState('')
  const [renameOpen, setRenameOpen] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [renameSaving, setRenameSaving] = useState(false)
  const [renameError, setRenameError] = useState('')
  // K2 统一出口：推送对话框、导出、右栏推送记录操作。
  const [notifyOpen, setNotifyOpen] = useState(false)
  const [exporting, setExporting] = useState<'markdown' | 'json' | null>(null)
  const [notifyActing, setNotifyActing] = useState<string | null>(null)
  const [notifyRowError, setNotifyRowError] = useState<{ id: string; message: string } | null>(null)
  // V3 申请复测：请求进行中与失败提示（in_flight 带会话 id）。
  const [retestBusy, setRetestBusy] = useState(false)
  const [retestError, setRetestError] = useState<{ message: string; sessionId?: string } | null>(null)
  const seq = useRef(0)
  const scrollRef = useRef<HTMLDivElement>(null)
  const toastTimer = useRef<number | null>(null)

  const notify = useCallback((message: string) => {
    setToast(message)
    if (toastTimer.current !== null) window.clearTimeout(toastTimer.current)
    toastTimer.current = window.setTimeout(() => setToast(''), 2800)
  }, [])

  useEffect(() => () => {
    if (toastTimer.current !== null) window.clearTimeout(toastTimer.current)
  }, [])

  const loadCard = useCallback(async (silent = false) => {
    const current = ++seq.current
    if (!silent) {
      setLoading(true)
      setNotFound(false)
      setError('')
    }
    try {
      const next = await getWebApiVulnCard(vulnKey)
      if (current !== seq.current) return
      setCard(next)
      setNotFound(false)
      setError('')
    } catch (err) {
      if (current !== seq.current) return
      if ((err as { status?: number }).status === 404) {
        setCard(null)
        setNotFound(true)
      } else {
        setError(errorText(err))
      }
    } finally {
      if (current === seq.current) setLoading(false)
    }
  }, [vulnKey])

  useEffect(() => {
    setCard(null)
    setMasked(true)
    setActionError('')
    setTargets(null)
    setRenameOpen(false)
    setStepReplays({})
    setReplayCooldownUntil(0)
    setNotifyOpen(false)
    setExporting(null)
    setNotifyActing(null)
    setNotifyRowError(null)
    setRetestError(null)
    void loadCard()
  }, [loadCard])

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: 0 })
  }, [vulnKey])

  const openInTask = () => {
    if (!card) return
    const key = card.header.key
    if (card.header.asset && !key.startsWith('finding:')) {
      onOpenAssetVuln(card.header.asset.id, key)
      return
    }
    const findingId = key.startsWith('finding:') ? key.slice('finding:'.length) : (card.repro?.finding_id || '')
    const member = card.appendix.members.find((item) => item.finding_id === findingId) || card.appendix.members[0]
    const taskId = card.repro?.task_id || card.retest?.task_id || member?.task_id || card.appendix.verifications[0]?.task_id || ''
    if (findingId && taskId) onOpenTaskFinding(taskId, findingId)
  }

  // V3 申请复测：后端在代表候选的原任务下新建复测并启动会话。
  const requestRetest = async () => {
    const key = card?.header.key
    if (!key || retestBusy || card?.retest_request?.status === 'running') return
    setRetestBusy(true)
    setRetestError(null)
    try {
      await requestWebApiVulnRetest(key)
      notify(t('vulnCard.retestRequested'))
    } catch (err) {
      let message = errorText(err)
      let sessionId: string | undefined
      if (err instanceof VulnRetestAPIError) {
        const body = err.body
        if (body?.code === 'archived') {
          message = t('vulnCard.retestErrArchived')
        } else if (body?.code === 'false_positive') {
          message = t('vulnCard.retestErrFalsePositive')
        } else if (body?.code === 'no_member') {
          message = t('vulnCard.retestErrNoMember')
        } else if (body?.code === 'in_flight') {
          message = t('vulnCard.retestErrInFlight')
          sessionId = body.session_id
        } else if (body?.code === 'no_model') {
          message = t('vulnCard.retestErrNoModel', { error: body.error || err.message })
        } else {
          message = body?.error || err.message
        }
      }
      setRetestError({ message, sessionId })
    } finally {
      // 成功或失败都刷新卡片：更新 retest_request、最近复测与生命周期。
      setRetestBusy(false)
      void loadCard(true)
    }
  }

  const copyCurl = async () => {
    if (!card?.repro || card.repro.steps.length === 0) return
    setCurlBusy(true)
    try {
      const text = await getWebApiVulnCardCurl(card.header.key, 1)
      await navigator.clipboard.writeText(text)
      notify(t('vulnCard.curlCopied'))
    } catch {
      notify(t('vulnCard.copyFailed'))
    } finally {
      setCurlBusy(false)
    }
  }

  // 429 倒计时：仅在有未到期的限频窗口时每秒驱动一次。
  useEffect(() => {
    if (replayCooldownUntil <= Date.now()) return
    const timer = window.setInterval(() => {
      const now = Date.now()
      setReplayNow(now)
      if (now >= replayCooldownUntil) window.clearInterval(timer)
    }, 500)
    return () => window.clearInterval(timer)
  }, [replayCooldownUntil])

  const runReplay = async (step: number) => {
    const key = card?.header.key
    if (!key) return
    if (Object.values(stepReplays).some((item) => item.busy)) return
    setStepReplays((prev) => ({ ...prev, [step]: { busy: true, result: null, error: '' } }))
    try {
      const result = await replayWebApiVulnStep(key, step)
      setStepReplays((prev) => ({ ...prev, [step]: { busy: false, result, error: '' } }))
    } catch (err) {
      let message = errorText(err)
      let retryAfter = 0
      if (err instanceof ReplayAPIError) {
        const body = err.body
        const status = body?.status
        if (err.httpStatus === 403 && status === 'denied') {
          message = t('vulnCard.replayErrDenied')
        } else if (err.httpStatus === 409 && status === 'review_required') {
          message = t('vulnCard.replayErrReview', { reason: body?.reason || '—' })
        } else if (err.httpStatus === 409 && status === 'archived') {
          message = t('vulnCard.replayErrArchived')
        } else if (err.httpStatus === 409 && status === 'replay_in_progress') {
          message = t('vulnCard.replayErrInProgress')
        } else if (err.httpStatus === 409 && status === 'unresolved') {
          message = t('vulnCard.replayErrUnresolved', { evidenceId: body?.evidence_id || '—' })
        } else if (err.httpStatus === 429) {
          retryAfter = body?.retry_after || 0
          message = t('vulnCard.replayErrRateLimited', { seconds: retryAfter })
        } else if (err.httpStatus === 502 && status === 'unknown') {
          message = t('vulnCard.replayErrUnknown', { evidenceId: body?.evidence_id || '—' })
        } else {
          message = body?.error || err.message
        }
      }
      if (retryAfter > 0) {
        setReplayNow(Date.now())
        setReplayCooldownUntil(Date.now() + retryAfter * 1000)
      }
      setStepReplays((prev) => ({ ...prev, [step]: { busy: false, result: null, error: message } }))
    } finally {
      // 成功或失败都刷新卡片：更新 replays[] 与生命周期。
      void loadCard(true)
    }
  }

  const closeReplay = (step: number) => {
    setStepReplays((prev) => {
      if (!(step in prev)) return prev
      const next = { ...prev }
      delete next[step]
      return next
    })
  }

  const anyReplayBusy = Object.values(stepReplays).some((item) => item.busy)
  const replayCooldownSeconds = replayCooldownUntil > replayNow
    ? Math.max(1, Math.ceil((replayCooldownUntil - replayNow) / 1000))
    : 0

  const isAssetVuln = Boolean(card?.header.asset && !card.header.key.startsWith('finding:'))
  const retestRequest = card?.retest_request ?? null
  const retestRunning = retestRequest?.status === 'running'

  // 推送记录里还有未完成项时每 10s 静默刷新（离开页面由 effect 清理）。
  const notifyHasPending = Boolean(card?.notify.items.some(
    (item) => item.status === 'pending_confirm' || item.status === 'queued' || item.status === 'sending',
  ))
  useEffect(() => {
    if (!notifyHasPending) return
    const timer = window.setInterval(() => { void loadCard(true) }, 10000)
    return () => window.clearInterval(timer)
  }, [notifyHasPending, loadCard])

  // 复测进行中每 10s 静默刷新，出结论后 retest_request 变 done 自动停止（卸载清理）。
  useEffect(() => {
    if (!retestRunning) return
    const timer = window.setInterval(() => { void loadCard(true) }, 10000)
    return () => window.clearInterval(timer)
  }, [retestRunning, loadCard])

  const exportCard = async (format: 'markdown' | 'json') => {
    if (!card || exporting !== null) return
    setExporting(format)
    try {
      const text = await exportWebApiVulnCard(card.header.key, format)
      downloadText(text, webApiVulnExportFilename(card.header.code, format), format)
    } catch (err) {
      notify(t('vulnCard.notifyExportFailed', { error: errorText(err) }))
    } finally {
      setExporting(null)
    }
  }

  const runNotifyAction = async (item: WebApiNotification, action: 'confirm' | 'dismiss' | 'retry') => {
    if (notifyActing !== null) return
    setNotifyActing(`${item.id}:${action}`)
    setNotifyRowError(null)
    try {
      await actWebApiNotification(item.id, action)
      await loadCard(true)
    } catch (err) {
      setNotifyRowError({ id: item.id, message: errorText(err) })
      if (err instanceof APIError && err.status === 409) await loadCard(true)
    } finally {
      setNotifyActing(null)
    }
  }

  const notifyNote = (item: WebApiNotification): string => {
    if (item.status === 'sent') return t('vulnCard.notifySentAt', { time: formatShort(item.sent_at) })
    if (item.status === 'failed' || item.last_error) return item.last_error || '—'
    if (item.status === 'pending_confirm') return t('vulnCard.notifyWaitingConfirm')
    if (item.status === 'dismissed') return t('vulnCard.notifyDismissed')
    return t('vulnCard.notifyWaiting')
  }

  const loadTargets = useCallback(async () => {
    const assetId = card?.header.asset?.id
    const selfKey = card?.header.key
    if (!assetId || !selfKey) return
    setTargetsLoading(true)
    try {
      const list = await listWebApiAssetVulns(assetId)
      setTargets(list.vulns.filter((item) => item.id !== selfKey))
      setActionError('')
    } catch (err) {
      setActionError(errorText(err))
    } finally {
      setTargetsLoading(false)
    }
  }, [card?.header.asset?.id, card?.header.key])

  const openRename = () => {
    if (!card) return
    setTitleDraft(card.header.title || '')
    setRenameError('')
    setRenameOpen(true)
  }

  const saveRename = async () => {
    const assetId = card?.header.asset?.id
    if (!assetId || !card || renameSaving) return
    setRenameSaving(true)
    setRenameError('')
    try {
      await saveWebApiVulnTitle(assetId, card.header.key, titleDraft.trim())
      setRenameOpen(false)
      await loadCard(true)
    } catch (err) {
      setRenameError(errorText(err))
    } finally {
      setRenameSaving(false)
    }
  }

  const mergeInto = async (target: WebApiVuln) => {
    const assetId = card?.header.asset?.id
    if (!assetId || !card || acting) return
    if (!(await confirm({ description: t('webapi.vulnMergeConfirm', { from: card.header.title, to: target.title }) }))) return
    setActing('merge')
    setActionError('')
    try {
      await mergeWebApiVulns(assetId, target.id, card.header.key)
      // 合并后源条目已不存在：跳到目标条目。
      onSwitch(target.id)
    } catch (err) {
      setActionError(errorText(err))
    } finally {
      setActing(null)
    }
  }

  const moveMember = async (member: WebApiVulnCardMember, targetId: string | null) => {
    const assetId = card?.header.asset?.id
    if (!assetId || !card || acting) return
    setActing(`${member.finding_id}:${targetId ?? 'split'}`)
    setActionError('')
    try {
      await moveWebApiVulnMember(assetId, targetId ?? newID(), member.finding_id)
      try {
        setCard(await getWebApiVulnCard(card.header.key))
        setTargets(null)
      } catch (err) {
        if ((err as { status?: number }).status === 404) {
          // 移走最后一个成员后当前条目已不存在：回到资产漏洞库。
          onOpenAssetVuln(assetId, card.header.key)
          return
        }
        throw err
      }
    } catch (err) {
      setActionError(errorText(err))
    } finally {
      setActing(null)
    }
  }

  const canOpenInTask = Boolean(card && (
    (card.header.asset && !card.header.key.startsWith('finding:'))
    || card.appendix.members.length > 0
    || card.repro?.task_id
    || card.retest?.task_id
  ))

  const repro = card?.repro || null
  const missingReason = card?.repro_missing_reason || 'no_verification'
  const fixedAt = card?.lifecycle.find((event) => event.kind === 'fixed')?.at || null
  // 无期限且未修复时不渲染期限与分隔点；已修复（已闭环）、逾期、有期限时照常显示。
  const showDeadline = Boolean(card && (card.header.status === 'fixed' || card.header.due_effective || card.header.overdue))

  return (
    <div ref={scrollRef} className="flex min-h-0 flex-1 flex-col overflow-auto px-4 py-5 sm:px-6">
      <div className="mx-auto w-full max-w-[1600px] space-y-4">
        {loading ? (
          <>
            <div className="flex items-center gap-2">
              <Button type="button" variant="ghost" size="sm" className="h-7 gap-1 px-1.5 text-muted-foreground" onClick={onBack}>
                <ChevronLeft className="h-3.5 w-3.5" />
                {t('vulnCard.back')}
              </Button>
            </div>
            <Skeleton />
          </>
        ) : notFound ? (
          <div className="flex min-h-[320px] flex-col items-center justify-center gap-3">
            <HelpCircle className="h-8 w-8 text-muted-foreground" />
            <p className="text-[14px] font-medium text-foreground">{t('vulnCard.notFound')}</p>
            <Button type="button" variant="outline" onClick={onBack}>{t('vulnCard.backToVulnList')}</Button>
          </div>
        ) : error || !card ? (
          <div className="flex min-h-[320px] flex-col items-center justify-center gap-3">
            <p className="text-[14px] font-medium text-foreground">{t('vulnCard.loadFailed')}</p>
            <p className="max-w-lg text-center text-[12px] text-muted-foreground">{error}</p>
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={onBack}>{t('vulnCard.backToVulnList')}</Button>
              <Button type="button" onClick={() => void loadCard()}>{t('vulnCard.retry')}</Button>
            </div>
          </div>
        ) : (
          <>
            {/* 顶部导航条 */}
            <div className="flex flex-wrap items-center justify-between gap-2 text-[12px]">
              <div className="flex min-w-0 items-center gap-2 text-muted-foreground">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  onClick={onOpenMenu}
                  aria-label={t('vulnCard.openMenu')}
                  className="-ml-1 shrink-0 md:hidden"
                >
                  <Menu className="h-4 w-4" />
                </Button>
                <button
                  type="button"
                  onClick={onBack}
                  className="inline-flex shrink-0 items-center gap-1 font-medium text-secondary-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                >
                  <ChevronLeft className="h-3.5 w-3.5" />
                  {t('vulnCard.back')}
                </button>
                {card.header.asset ? (
                  <>
                    <span className="text-muted-foreground">/</span>
                    <span className="min-w-0 truncate">{card.header.asset.name}</span>
                  </>
                ) : null}
                <span className="text-muted-foreground">/</span>
                <span className="shrink-0 font-mono font-medium text-foreground">{card.header.code}</span>
              </div>
              <div className="flex items-center gap-1.5">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-8 gap-1"
                  disabled={!card.nav.prev_key}
                  onClick={() => { if (card.nav.prev_key) onSwitch(card.nav.prev_key) }}
                >
                  <ChevronLeft className="h-3.5 w-3.5" />
                  {t('vulnCard.prev')}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="h-8 gap-1"
                  disabled={!card.nav.next_key}
                  onClick={() => { if (card.nav.next_key) onSwitch(card.nav.next_key) }}
                >
                  {t('vulnCard.next')}
                  <ChevronRight className="h-3.5 w-3.5" />
                </Button>
              </div>
            </div>

            {/* 操作失败错误条 */}
            {actionError && (
              <div role="alert" className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-[12px] text-destructive">
                <span className="min-w-0 break-words">{actionError}</span>
              </div>
            )}

            {/* 横幅 */}
            {card.header.status === 'fixed' && (
              <div
                className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border px-3 py-2 text-[12px]"
                style={{ background: 'var(--st-fixed-bg)', borderColor: 'var(--st-fixed-text)', color: 'var(--st-fixed-text)' }}
              >
                <CheckCircle2 className="h-3.5 w-3.5" />
                <span className="font-medium">
                  {card.retest
                    ? `${t('vulnCard.fixedBanner')} · ${formatFull(card.retest.at)}`
                    : `${t('vulnCard.fixedBannerFallback')}${fixedAt ? ` · ${formatFull(fixedAt)}` : ''}`}
                </span>
                {card.retest?.summary ? <span className="min-w-0 opacity-90">{card.retest.summary}</span> : null}
              </div>
            )}
            {card.header.disposition === 'false_positive' && (
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border border-border bg-muted/60 px-3 py-2 text-[12px] text-secondary-foreground">
                <XCircle className="h-3.5 w-3.5" />
                <span className="font-medium">{t('vulnCard.falsePositiveBanner')}</span>
                {card.triage?.note ? <span className="min-w-0">· {t('vulnCard.falsePositiveNote', { note: card.triage.note })}</span> : null}
              </div>
            )}

            {/* 标题卡 */}
            <Card className="space-y-3">
              <div className="flex flex-col items-start justify-between gap-3 sm:flex-row sm:flex-wrap">
                <div className="min-w-0 flex-1 space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    {card.header.severity ? (
                      <SeverityBadge severity={card.header.severity} />
                    ) : card.header.suggested_severity ? (
                      <SeverityBadge severity={card.header.suggested_severity} suggested />
                    ) : (
                      <SeverityBadge severity="" />
                    )}
                    <h1 className="min-w-0 break-words text-[16px] font-semibold leading-snug tracking-tight text-foreground sm:text-[17px]">
                      {card.header.title_template.split(' · ').map((segment, index) => (
                        <span key={`${segment}-${index}`}>
                          {index > 0 ? <span className="mx-1 text-muted-foreground">·</span> : null}
                          <span className={isPathSegment(segment) ? 'font-mono text-[15px]' : undefined}>{segment}</span>
                        </span>
                      ))}
                    </h1>
                  </div>
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-muted-foreground">
                    <StatusBadge status={card.header.status} />
                    <DispositionBadge disposition={card.header.disposition} />
                    {showDeadline ? (
                      <>
                        <DeadlineText
                          due={card.header.due_effective}
                          overdue={card.header.overdue}
                          fixed={card.header.status === 'fixed'}
                          className="text-[12px]"
                        />
                        <span>·</span>
                      </>
                    ) : null}
                    <span className="font-mono">{card.header.code}</span>
                    <span>·</span>
                    <span>{t('vulnCard.firstSeen', { time: formatFull(card.header.first_seen_at) })}</span>
                  </div>
                </div>
                <div className="flex w-full shrink-0 flex-wrap items-center gap-2 sm:w-auto">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span tabIndex={repro ? -1 : 0}>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-8 gap-1"
                          disabled={!repro || repro.steps.length === 0 || anyReplayBusy || replayCooldownSeconds > 0}
                          onClick={() => void runReplay(1)}
                        >
                          {stepReplays[1]?.busy
                            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                            : <Play className="h-3.5 w-3.5" />}
                          {stepReplays[1]?.busy
                            ? t('vulnCard.replaying')
                            : replayCooldownSeconds > 0
                              ? t('vulnCard.replayWait', { seconds: replayCooldownSeconds })
                              : t('vulnCard.replay')}
                        </Button>
                      </span>
                    </TooltipTrigger>
                    {(!repro || repro.steps.length === 0) && (
                      <TooltipContent>{t('vulnCard.replayNoRepro')}</TooltipContent>
                    )}
                  </Tooltip>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-8 gap-1"
                    disabled={!repro || repro.steps.length === 0 || curlBusy}
                    onClick={() => void copyCurl()}
                  >
                    {curlBusy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ClipboardCopy className="h-3.5 w-3.5" />}
                    {t('vulnCard.copyCurl')}
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-8 gap-1"
                    disabled={!canOpenInTask}
                    onClick={openInTask}
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                    {t('vulnCard.viewInTask')}
                  </Button>
                  <DropdownMenu onOpenChange={(next) => { if (next && isAssetVuln) void loadTargets() }}>
                    <DropdownMenuTrigger asChild>
                      <Button
                        type="button"
                        variant="outline"
                        size="icon-sm"
                        className="h-8 w-8"
                        aria-label={t('webapi.vulnMoreActions')}
                        disabled={acting !== null || exporting !== null}
                      >
                        {acting || exporting
                          ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                          : <MoreHorizontal className="h-4 w-4" />}
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem onSelect={() => setNotifyOpen(true)}>
                        <Send className="mr-2 h-4 w-4" />
                        {t('vulnCard.notifyMenu')}
                      </DropdownMenuItem>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="block">
                            <DropdownMenuItem
                              disabled={retestRunning || retestBusy}
                              className={cn(retestRunning && 'pointer-events-none')}
                              onSelect={() => { void requestRetest() }}
                            >
                              {retestBusy
                                ? <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                : <RotateCw className="mr-2 h-4 w-4" />}
                              {retestBusy ? t('vulnCard.retestRequesting') : t('vulnCard.startRetest')}
                            </DropdownMenuItem>
                          </span>
                        </TooltipTrigger>
                        {retestRunning ? <TooltipContent>{t('vulnCard.retestInFlight')}</TooltipContent> : null}
                      </Tooltip>
                      <DropdownMenuItem onSelect={() => { void exportCard('markdown') }}>
                        <Download className="mr-2 h-4 w-4" />
                        {t('vulnCard.notifyExportMarkdown')}
                      </DropdownMenuItem>
                      <DropdownMenuItem onSelect={() => { void exportCard('json') }}>
                        <Download className="mr-2 h-4 w-4" />
                        {t('vulnCard.notifyExportJson')}
                      </DropdownMenuItem>
                      {isAssetVuln && <DropdownMenuSeparator />}
                      {isAssetVuln && <DropdownMenuItem onSelect={openRename}>{t('webapi.vulnRename')}</DropdownMenuItem>}
                      {isAssetVuln && (
                        <DropdownMenuSub>
                          <DropdownMenuSubTrigger disabled={targetsLoading}>{t('webapi.vulnMergeInto')}</DropdownMenuSubTrigger>
                          <DropdownMenuSubContent>
                            {targets === null || targetsLoading ? (
                              <DropdownMenuItem disabled>
                                {targetsLoading ? t('shell.loading') : '—'}
                              </DropdownMenuItem>
                            ) : targets.length === 0 ? (
                              <DropdownMenuItem disabled>—</DropdownMenuItem>
                            ) : targets.map((target) => (
                              <DropdownMenuItem key={target.id} onSelect={() => { void mergeInto(target) }}>
                                <StatusBadge status={target.status} />
                                <span className="ml-2 max-w-[280px] truncate">{target.title}</span>
                              </DropdownMenuItem>
                            ))}
                          </DropdownMenuSubContent>
                        </DropdownMenuSub>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            </Card>

            {/* 主栏 + 右栏 */}
            <div className="flex w-full flex-col items-stretch gap-5 min-[1024px]:flex-row min-[1024px]:items-start">
              <div className={cn('w-full min-w-0 flex-1 space-y-4', card.header.disposition === 'false_positive' && 'opacity-[.68]')}>
                {repro ? (
                  <>
                    {repro.impact ? (
                      <Card
                        className="space-y-3"
                        style={{
                          borderLeftWidth: 3,
                          borderLeftStyle: 'solid',
                          borderLeftColor: severityColor(card.header.severity || card.header.suggested_severity),
                        }}
                      >
                        <div className="mb-1 flex items-center gap-1.5">
                          <span className="text-[12px] font-semibold text-foreground">{t('vulnCard.impactTitle')}</span>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <button type="button" aria-label={t('vulnCard.impactHelp')} className="text-muted-foreground/70 hover:text-foreground">
                                <HelpCircle className="h-3.5 w-3.5" />
                              </button>
                            </TooltipTrigger>
                            <TooltipContent className="max-w-[260px] whitespace-normal text-left leading-relaxed">
                              {t('vulnCard.impactHelp')}
                            </TooltipContent>
                          </Tooltip>
                        </div>
                        <p className="text-[15px] font-medium leading-relaxed text-foreground">{repro.impact}</p>
                        {repro.preconditions ? (
                          <div className="flex items-start gap-2 border-t border-border pt-2.5 text-[12px]">
                            <span className="shrink-0 font-semibold text-secondary-foreground">{t('vulnCard.preconditions')}</span>
                            <span className="min-w-0 text-foreground">{repro.preconditions}</span>
                          </div>
                        ) : null}
                      </Card>
                    ) : null}

                    <Card className="space-y-3.5">
                      <SectionTitle actions={(
                        <label className="flex select-none items-center gap-1.5 text-[12px] text-secondary-foreground">
                          <Switch checked={masked} onCheckedChange={setMasked} className="scale-75" />
                          <span>{t('vulnCard.maskSensitive')}</span>
                        </label>
                      )}>
                        {t('vulnCard.keyRequests', { count: repro.steps.length })}
                      </SectionTitle>
                      {repro.steps.map((step, index) => {
                        const state = stepReplays[index + 1]
                        return (
                          <div key={step.evidence_id} className="space-y-2">
                            <StepBlock
                              repro={repro}
                              step={step}
                              index={index}
                              masked={masked}
                              onScreenshot={(url, filename) => setShot({ url, filename })}
                              onNotify={notify}
                              onReplay={(target) => void runReplay(target)}
                              replayBusy={Boolean(state?.busy)}
                              replayBlocked={anyReplayBusy && !state?.busy}
                              replayCooldownSeconds={replayCooldownSeconds}
                            />
                            {state?.result ? (
                              <ReplayResultPanel
                                result={state.result}
                                masked={masked}
                                onClose={() => closeReplay(index + 1)}
                                onOpenEvidence={onOpenTaskEvidence}
                              />
                            ) : state?.error ? (
                              <ReplayErrorPanel message={state.error} onClose={() => closeReplay(index + 1)} />
                            ) : null}
                          </div>
                        )
                      })}
                    </Card>

                    {repro.contrast ? <ContrastSection repro={repro} masked={masked} /> : null}

                    <div className="grid grid-cols-1 gap-4 min-[1024px]:grid-cols-2">
                      <Card className="space-y-2.5">
                        <SectionTitle>
                          <span className="inline-flex items-center gap-1.5"><Wrench className="h-3.5 w-3.5 text-blue-600" />{t('vulnCard.remediation')}</span>
                        </SectionTitle>
                        {repro.remediation.length === 0 ? (
                          <p className="text-[12px] text-muted-foreground">—</p>
                        ) : (
                          <ul className="list-disc space-y-2 pl-4 text-[12px] text-secondary-foreground">
                            {repro.remediation.map((item, index) => <li key={index}>{item}</li>)}
                          </ul>
                        )}
                      </Card>
                      <Card className="flex flex-col justify-between gap-3">
                        <div className="space-y-2">
                          <SectionTitle>
                            <span className="inline-flex items-center gap-1.5"><CheckCircle2 className="h-3.5 w-3.5 text-emerald-600" />{t('vulnCard.retestMethod')}</span>
                          </SectionTitle>
                          <p className="whitespace-pre-wrap text-[12px] leading-relaxed text-secondary-foreground">{repro.retest_method || '—'}</p>
                        </div>
                        {retestRunning && retestRequest ? (
                          <div className="flex flex-wrap items-center gap-2 text-[11.5px]">
                            <span aria-hidden className="h-2 w-2 shrink-0 animate-pulse rounded-full bg-amber-500" />
                            <span className="text-amber-700 dark:text-amber-300">
                              {t('vulnCard.retestRunningAt', { time: formatShort(retestRequest.started_at) })}
                            </span>
                            <Button
                              type="button"
                              variant="link"
                              size="sm"
                              className="ml-auto h-auto p-0 text-[11.5px]"
                              onClick={() => onOpenSession(retestRequest.session_id)}
                            >
                              {t('vulnCard.openRetestSession')}
                            </Button>
                          </div>
                        ) : null}
                        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-border pt-2.5 text-[11.5px]">
                          <span>
                            <span className="text-muted-foreground">{t('vulnCard.lastRetest')}</span>
                            <span className={cn('font-medium', card.retest ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground')}>
                              {card.retest
                                ? `${retestText(t, card.retest.conclusion)} (${formatShort(card.retest.at)})`
                                : t('vulnCard.noRetest')}
                            </span>
                          </span>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="inline-flex" tabIndex={retestRunning ? 0 : -1}>
                                <Button
                                  type="button"
                                  variant="outline"
                                  size="sm"
                                  className={cn('h-7 gap-1', retestRunning && 'pointer-events-none')}
                                  disabled={retestRunning || retestBusy}
                                  onClick={() => void requestRetest()}
                                >
                                  {retestBusy
                                    ? <Loader2 className="h-3 w-3 animate-spin" />
                                    : <RotateCw className="h-3 w-3" />}
                                  {retestBusy ? t('vulnCard.retestRequesting') : t('vulnCard.startRetest')}
                                </Button>
                              </span>
                            </TooltipTrigger>
                            {retestRunning ? <TooltipContent>{t('vulnCard.retestInFlight')}</TooltipContent> : null}
                          </Tooltip>
                        </div>
                        {retestError ? (
                          <RetestErrorPanel
                            message={retestError.message}
                            sessionId={retestError.sessionId}
                            onOpenSession={onOpenSession}
                            onClose={() => setRetestError(null)}
                          />
                        ) : null}
                        {card.replays[0] ? (
                          <div className="flex flex-wrap items-center gap-1.5 border-t border-border pt-2.5 text-[11.5px]">
                            <span className="text-muted-foreground">{t('vulnCard.lastReplay')}</span>
                            <ReplayHintBadge hint={card.replays[0].verdict_hint} />
                            <span className="tabular-nums text-muted-foreground">{formatShort(card.replays[0].at)}</span>
                            <span className="text-muted-foreground">·</span>
                            <span className="text-secondary-foreground">
                              {t('vulnCard.replayStatusLabel', { status: card.replays[0].status_code ?? '—' })}
                            </span>
                          </div>
                        ) : null}
                      </Card>
                    </div>
                  </>
                ) : (
                  <Card className="space-y-3">
                    <SectionTitle>{t('vulnCard.keyRequestsTitle')}</SectionTitle>
                    {retestError ? (
                      <RetestErrorPanel
                        message={retestError.message}
                        sessionId={retestError.sessionId}
                        onOpenSession={onOpenSession}
                        onClose={() => setRetestError(null)}
                      />
                    ) : null}
                    <div className="space-y-2.5 rounded-md bg-muted/40 p-6 text-center">
                      <div className="mx-auto flex h-9 w-9 items-center justify-center rounded-full bg-card text-muted-foreground">
                        <Sparkles className="h-4 w-4" />
                      </div>
                      <div className="text-[12px] font-semibold text-foreground">{t('vulnCard.noReproTitle')}</div>
                      <p className="mx-auto max-w-md text-[11.5px] text-muted-foreground">
                        {missingReason === 'no_supported'
                          ? t('vulnCard.noReproNoSupported')
                          : missingReason === 'no_key_steps'
                            ? t('vulnCard.noReproNoKeySteps')
                            : t('vulnCard.noReproNoVerification')}
                      </p>
                      <Button type="button" size="sm" className="gap-1" onClick={openInTask} disabled={!canOpenInTask}>
                        <Sparkles className="h-3.5 w-3.5" />
                        {t('vulnCard.startVerification')}
                      </Button>
                    </div>
                    <div className="space-y-1 text-[11.5px]">
                      <div className="text-[11px] font-medium text-muted-foreground">{t('vulnCard.rawEvidence')}</div>
                      {card.appendix.evidence.slice(0, 10).map((item) => (
                        <div key={`${item.task_id}:${item.evidence_id}`} className="flex items-center justify-between gap-2 rounded bg-muted/40 px-2 py-1 font-mono">
                          <span className="min-w-0 break-all">{item.method} {displayPath(item.url)}{item.status_code != null ? ` → ${item.status_code}` : ''}</span>
                          <span className="shrink-0 text-muted-foreground">{formatShort(item.created_at)}</span>
                        </div>
                      ))}
                    </div>
                  </Card>
                )}

                <Appendix
                  card={card}
                  masked={masked}
                  onOpenTaskFinding={onOpenTaskFinding}
                  assetActions={isAssetVuln ? { targets, targetsLoading, acting, loadTargets, moveMember } : null}
                />
              </div>

              <aside className="w-full shrink-0 space-y-3.5 min-[1024px]:sticky min-[1024px]:top-4 min-[1024px]:w-[340px] min-[1440px]:w-[376px]">
                <Card className="space-y-3">
                  <h3 className="text-[12px] font-semibold text-foreground">{t('vulnCard.triageTitle')}</h3>
                  <VulnTriageForm
                    vulnKey={card.header.key}
                    triage={card.triage}
                    status={card.header.status}
                    suggestedSeverity={card.header.suggested_severity}
                    onSaved={() => { void loadCard(true) }}
                  />
                </Card>

                <Card className="space-y-2.5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-[12px] font-semibold text-foreground">{t('vulnCard.notifyTitle')}</h3>
                    <div className="flex items-center gap-1.5">
                      <span className="text-[11px] text-muted-foreground">
                        {card.notify.configured
                          ? (card.notify.require_confirm ? t('vulnCard.notifyStateRequireConfirm') : t('vulnCard.notifyStateAuto'))
                          : t('vulnCard.notifyStateUnconfigured')}
                      </span>
                      <Button
                        type="button"
                        variant="ghost"
                        size="xs"
                        className="gap-1"
                        onClick={() => setNotifyOpen(true)}
                      >
                        <Send className="h-3 w-3" />
                        {t('vulnCard.notifyPush')}
                      </Button>
                    </div>
                  </div>
                  {card.notify.items.length === 0 ? (
                    <p className="text-[12px] text-muted-foreground">{t('vulnCard.notifyEmpty')}</p>
                  ) : (
                    <div className="space-y-2">
                      {card.notify.items.slice(0, 5).map((item) => {
                        const busy = notifyActing !== null
                        const rowConflict = notifyRowError?.id === item.id ? notifyRowError.message : ''
                        return (
                          <div key={item.id} className="space-y-1 border-t border-border/60 pt-2 first:border-t-0 first:pt-0">
                            <div className="flex items-center gap-1.5">
                              <NotificationStatusBadge status={item.status} />
                              <span className="text-[12px] text-secondary-foreground">{t(NOTIFY_KIND_KEYS[item.kind])}</span>
                              <span className="ml-auto shrink-0 font-mono text-[11px] tabular text-muted-foreground">
                                {formatShort(item.created_at)}
                              </span>
                            </div>
                            <div className="flex items-center gap-1.5">
                              <p
                                className={cn(
                                  'min-w-0 flex-1 truncate text-[11px]',
                                  item.status === 'failed' ? 'text-destructive' : 'text-muted-foreground',
                                )}
                                title={notifyNote(item)}
                              >
                                {notifyNote(item)}
                              </p>
                              {item.status === 'pending_confirm' && (
                                <>
                                  <Button type="button" variant="soft" size="xs" disabled={busy} onClick={() => { void runNotifyAction(item, 'confirm') }}>
                                    {t('vulnCard.notifyConfirm')}
                                  </Button>
                                  <Button type="button" variant="ghost" size="xs" disabled={busy} onClick={() => { void runNotifyAction(item, 'dismiss') }}>
                                    {t('vulnCard.notifyDismiss')}
                                  </Button>
                                </>
                              )}
                              {item.status === 'failed' && (
                                <>
                                  <Button type="button" variant="soft" size="xs" disabled={busy} onClick={() => { void runNotifyAction(item, 'retry') }}>
                                    {t('vulnCard.notifyRetry')}
                                  </Button>
                                  <Button type="button" variant="ghost" size="xs" disabled={busy} onClick={() => { void runNotifyAction(item, 'dismiss') }}>
                                    {t('vulnCard.notifyDismiss')}
                                  </Button>
                                </>
                              )}
                            </div>
                            {rowConflict && <p className="text-[11px] text-destructive">{rowConflict}</p>}
                          </div>
                        )
                      })}
                    </div>
                  )}
                </Card>

                <Card className="space-y-2.5">
                  <h3 className="text-[12px] font-semibold text-foreground">{t('vulnCard.assetTitle')}</h3>
                  {card.header.asset ? (
                    <div className="grid grid-cols-[76px_1fr] gap-y-1.5 text-[12px]">
                      <span className="text-muted-foreground">{t('vulnCard.assetNameLabel')}</span>
                      <span className="min-w-0 break-words font-medium text-foreground">{card.header.asset.name}</span>
                      <span className="text-muted-foreground">{t('vulnCard.assetEnv')}</span>
                      <span className="text-foreground">{card.header.asset.env || '—'}</span>
                      <span className="text-muted-foreground">{t('vulnCard.assetOwner')}</span>
                      <span className="text-foreground">{card.header.asset.owner || '—'}</span>
                      <span className="text-muted-foreground">{t('vulnCard.assetOrigins')}</span>
                      <span className="min-w-0 space-y-0.5 font-mono text-[11px] text-secondary-foreground">
                        {card.header.asset.origins.length === 0
                          ? '—'
                          : card.header.asset.origins.map((origin) => <span key={origin} className="block break-all">{origin}</span>)}
                      </span>
                    </div>
                  ) : (
                    <p className="text-[12px] text-muted-foreground">{t('vulnCard.noAsset')}</p>
                  )}
                </Card>

                <LifecycleTimeline events={card.lifecycle} />

                {card.related.length > 0 && (
                  <Card className="space-y-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <h3 className="text-[12px] font-semibold text-foreground">{t('vulnCard.relatedTitle')}</h3>
                      <span className="text-[11px] text-muted-foreground">{t('vulnCard.countItems', { count: card.related.length })}</span>
                    </div>
                    <div className="space-y-1.5">
                      {card.related.map((item) => (
                        <button
                          key={item.key}
                          type="button"
                          onClick={() => onSwitch(item.key)}
                          className="flex w-full min-w-0 items-center gap-2 rounded border border-transparent p-2 text-left transition-colors hover:border-border hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                        >
                          {item.severity
                            ? <SeverityBadge severity={item.severity} compact className="shrink-0 text-[10.5px]" />
                            : item.suggested_severity
                              ? <SeverityBadge severity={item.suggested_severity} suggested compact className="shrink-0 text-[10.5px]" />
                              : <SeverityBadge severity="" compact className="shrink-0 text-[10.5px]" />}
                          <span className="min-w-0 flex-1 truncate text-[12px] text-foreground" title={item.title}>{item.title}</span>
                          <span className="shrink-0 font-mono text-[10.5px] text-muted-foreground">{item.code}</span>
                        </button>
                      ))}
                    </div>
                  </Card>
                )}
              </aside>
            </div>
          </>
        )}
      </div>

      {toast && (
        <div role="status" className="fixed bottom-5 right-5 z-50 rounded-md border border-border bg-card px-3 py-2 text-[12px] text-foreground shadow-lg">
          {toast}
        </div>
      )}

      <Dialog open={renameOpen} onOpenChange={(next) => { if (!next) setRenameOpen(false) }}>
        <DialogContent
          closeLabel={t('webapi.close')}
          aria-describedby={undefined}
          className="flex w-[calc(100vw-2rem)] max-w-[560px] flex-col gap-0 overflow-hidden p-0"
        >
          <DialogHeaderRow title={t('webapi.vulnRename')} />
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-5">
          <label className="block space-y-1.5">
            <span className="block text-[12px] text-muted-foreground">{t('webapi.vulnTitleLabel')}</span>
            <Input
              value={titleDraft}
              onChange={(event) => setTitleDraft(event.target.value)}
              maxLength={200}
              autoFocus
              disabled={renameSaving}
            />
            <span className="block text-[11px] text-muted-foreground">{t('webapi.vulnTitleAutoHint')}</span>
          </label>
          {renameError && <p role="alert" className="text-xs text-destructive">{renameError}</p>}
          </div>
          <DialogFooterRow>
            <Button type="button" variant="outline" onClick={() => setRenameOpen(false)} disabled={renameSaving}>{t('webapi.close')}</Button>
            <Button type="button" onClick={() => void saveRename()} disabled={renameSaving}>
              {renameSaving && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              {t('webapi.vulnSave')}
            </Button>
          </DialogFooterRow>
        </DialogContent>
      </Dialog>

      {card && (
        <VulnNotifyDialog
          open={notifyOpen}
          onOpenChange={setNotifyOpen}
          vulnKey={card.header.key}
          card={card}
          onPushed={() => { void loadCard(true) }}
          onOpenIntegrations={onOpenIntegrations}
          onToast={notify}
        />
      )}

      <WebApiEvidenceImageDialog
        open={shot !== null}
        onClose={() => setShot(null)}
        url={shot?.url || ''}
        filename={shot?.filename || 'evidence.png'}
      />
    </div>
  )
}
