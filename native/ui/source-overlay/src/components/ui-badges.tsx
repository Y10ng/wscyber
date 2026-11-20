import { CircleDashed, ShieldOff, Sparkles, Wrench, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@cyber/theme'
import type {
  WebApiNotificationStatus,
  WebApiVulnDisposition,
  WebApiVulnSeverity,
  WebApiVulnStatus,
} from '../api'

// ── 七类徽标（docs/设计稿.html“设计规范”第 1 节）────────────────────────
// 形状/颜色由 index.css 的 .ws-* 类统一给出，本文件只负责枚举映射、文案与图标。
// 状态＝圆角胶囊+圆点；等级＝微底方角（AI 建议为虚线星标）；处置＝4px 锐角+
// 语义图标（误报删除线）；期限＝纯文字（正常/即将到期/逾期/已闭环）。

const STATUS_CLASS: Record<WebApiVulnStatus, { className: string; labelKey: string }> = {
  new: { className: 'ws-st-new', labelKey: 'webapi.vulnStatusNew' },
  present: { className: 'ws-st-present', labelKey: 'webapi.vulnStatusPresent' },
  pending: { className: 'ws-st-pending', labelKey: 'webapi.vulnStatusPending' },
  unconfirmed: { className: 'ws-st-unconfirmed', labelKey: 'webapi.vulnStatusUnconfirmed' },
  fixed: { className: 'ws-st-fixed', labelKey: 'webapi.vulnStatusFixed' },
}

export function StatusBadge({ status, className }: { status: WebApiVulnStatus; className?: string }) {
  const { t } = useTranslation('chat')
  const meta = STATUS_CLASS[status] || STATUS_CLASS.new
  return (
    <span className={cn('ws-st-badge', meta.className, className)}>
      <span aria-hidden className="ws-st-dot" />
      {t(meta.labelKey)}
    </span>
  )
}

type SeverityKey = Exclude<WebApiVulnSeverity, ''> | 'none'

const SEVERITY_CLASS: Record<SeverityKey, { className: string; labelKey: string }> = {
  critical: { className: 'ws-sev-critical', labelKey: 'shell.triageSeverityCritical' },
  high: { className: 'ws-sev-high', labelKey: 'shell.triageSeverityHigh' },
  medium: { className: 'ws-sev-medium', labelKey: 'shell.triageSeverityMedium' },
  low: { className: 'ws-sev-low', labelKey: 'shell.triageSeverityLow' },
  info: { className: 'ws-sev-info', labelKey: 'shell.triageSeverityInfo' },
  none: { className: 'ws-sev-none', labelKey: 'shell.triageSeverityNone' },
}

export function severityKey(severity: WebApiVulnSeverity | string | null | undefined): SeverityKey {
  return (severity || 'none') as SeverityKey
}

export function SeverityBadge({ severity, suggested, className }: {
  severity: WebApiVulnSeverity | string | null | undefined
  /** AI 建议：虚线框 + Sparkles，文案“AI 建议：高危”。 */
  suggested?: boolean
  className?: string
}) {
  const { t } = useTranslation('chat')
  const key = severityKey(severity)
  const meta = SEVERITY_CLASS[key] || SEVERITY_CLASS.none
  const label = t(meta.labelKey)
  if (suggested) {
    return (
      <span className={cn('ws-sev-badge ws-sev-suggest', className)}>
        <Sparkles className="h-3 w-3" aria-hidden />
        {t('shell.triageAiSuggest', { level: label })}
      </span>
    )
  }
  return <span className={cn('ws-sev-badge', meta.className, className)}>{label}</span>
}

const DISPOSITION_META: Record<WebApiVulnDisposition, {
  className: string
  labelKey: string
  Icon: typeof CircleDashed
}> = {
  open: { className: 'ws-disp-open', labelKey: 'shell.triageDispositionOpen', Icon: CircleDashed },
  fixing: { className: 'ws-disp-fixing', labelKey: 'shell.triageDispositionFixing', Icon: Wrench },
  risk_accepted: { className: 'ws-disp-accepted', labelKey: 'shell.triageDispositionRiskAccepted', Icon: ShieldOff },
  false_positive: { className: 'ws-disp-false', labelKey: 'shell.triageDispositionFalsePositive', Icon: XCircle },
}

export function DispositionBadge({ disposition, className }: {
  disposition: WebApiVulnDisposition | null | undefined
  className?: string
}) {
  const { t } = useTranslation('chat')
  const meta = DISPOSITION_META[disposition || 'open'] || DISPOSITION_META.open
  const { Icon } = meta
  return (
    <span className={cn('ws-disp-tag', meta.className, className)}>
      <Icon className="h-3 w-3" aria-hidden />
      {t(meta.labelKey)}
    </span>
  )
}

function dateUTC(value: string): number | null {
  const parts = value.split('-').map(Number)
  if (parts.length !== 3 || parts.some((part) => !Number.isFinite(part))) return null
  return Date.UTC(parts[0], parts[1] - 1, parts[2])
}

function todayUTC(): number {
  const now = new Date()
  return Date.UTC(now.getFullYear(), now.getMonth(), now.getDate())
}

/**
 * 期限：正常（剩 N 天）/ 即将到期（N 天，≤2 天黄）/ 逾期 N 天（红）/ —；
 * 已修复显示“已闭环”。期限当天不算逾期（与后端 SLA 一致）。
 */
export function DeadlineText({ due, overdue, fixed, className }: {
  due?: string | null
  overdue?: boolean
  /** 条目已修复（状态 fixed）。 */
  fixed?: boolean
  className?: string
}) {
  const { t } = useTranslation('chat')
  if (fixed) return <span className={cn('ws-deadline-closed', className)}>{t('shell.deadlineClosed')}</span>
  if (!due) return <span className={cn('text-muted-foreground', className)}>—</span>
  const dueUTC = dateUTC(due)
  if (dueUTC === null) return <span className={cn('ws-deadline-normal', className)}>{due}</span>
  const diff = Math.round((dueUTC - todayUTC()) / 86400000)
  if (overdue || diff < 0) {
    const days = Math.max(1, -diff)
    return <span className={cn('ws-deadline-overdue', className)}>{t('shell.triageOverdueDays', { count: days })}</span>
  }
  if (diff <= 2) return <span className={cn('ws-deadline-soon', className)}>{t('shell.deadlineSoon', { count: diff })}</span>
  return <span className={cn('ws-deadline-normal', className)}>{t('shell.deadlineNormal', { count: diff })}</span>
}

const VERDICT_META: Record<string, { className: string; labelKey: string }> = {
  supported: { className: 'ws-st-fixed', labelKey: 'webapi.verdict_supported' },
  counterevidence: { className: 'ws-st-new', labelKey: 'webapi.verdict_counterevidence' },
  inconclusive: { className: 'ws-st-present', labelKey: 'webapi.verdict_inconclusive' },
}

/** 验证判读徽标 + “模型判读”弱标签。无判读时显示 —。 */
export function VerdictBadge({ verdict, showModelLabel = true, className }: {
  verdict?: string | null
  showModelLabel?: boolean
  className?: string
}) {
  const { t } = useTranslation('chat')
  if (!verdict) return <span className={cn('text-muted-foreground', className)}>—</span>
  const meta = VERDICT_META[verdict]
  return (
    <span className={cn('inline-flex items-center gap-1', className)}>
      <span className={cn('ws-st-badge', meta?.className || 'ws-st-unconfirmed')}>
        {meta ? t(meta.labelKey) : verdict}
      </span>
      {showModelLabel && <span className="ws-model-label">{t('webapi.verdictModelLabel')}</span>}
    </span>
  )
}

const RETEST_META: Record<string, { className: string; labelKey: string }> = {
  reproducible: { className: 'ws-st-new', labelKey: 'webapi.retestReproducible' },
  fixed_in_scope: { className: 'ws-st-fixed', labelKey: 'webapi.retestFixedInScope' },
  inconclusive: { className: 'ws-st-unconfirmed', labelKey: 'webapi.retestInconclusive' },
}

export function RetestBadge({ conclusion, className }: {
  conclusion?: string | null
  className?: string
}) {
  const { t } = useTranslation('chat')
  if (!conclusion) return <span className={cn('text-muted-foreground', className)}>—</span>
  const meta = RETEST_META[conclusion]
  return (
    <span className={cn('ws-st-badge', meta?.className || 'ws-st-unconfirmed', className)}>
      {meta ? t(meta.labelKey) : conclusion}
    </span>
  )
}

const NOTIFICATION_META: Record<WebApiNotificationStatus, { className: string; labelKey: string }> = {
  pending_confirm: { className: 'ws-st-present', labelKey: 'shell.integrationsStatusPending' },
  queued: { className: 'ws-st-pending', labelKey: 'shell.integrationsStatusQueued' },
  sending: { className: 'ws-st-pending', labelKey: 'shell.integrationsStatusSending' },
  sent: { className: 'ws-st-fixed', labelKey: 'shell.integrationsStatusSent' },
  failed: { className: 'ws-st-new', labelKey: 'shell.integrationsStatusFailed' },
  dismissed: { className: 'ws-st-unconfirmed', labelKey: 'shell.integrationsStatusDismissed' },
}

/** 推送记录状态（待确认/排队/发送中/已发送/失败/已忽略）同一胶囊语言。 */
export function NotificationStatusBadge({ status, className }: {
  status: WebApiNotificationStatus
  className?: string
}) {
  const { t } = useTranslation('chat')
  const meta = NOTIFICATION_META[status] || NOTIFICATION_META.dismissed
  return (
    <span className={cn('ws-st-badge', meta.className, className)}>
      <span aria-hidden className="ws-st-dot" />
      {t(meta.labelKey)}
    </span>
  )
}
