import { CircleDashed, ShieldOff, Sparkles, Wrench, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@cyber/theme'
import type {
  WebApiNotificationStatus,
  WebApiTaskListStatus,
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

const TASK_STATE_CLASS: Record<WebApiTaskListStatus, {
  className: string
  dotClassName?: string
  labelKey: string
}> = {
  // 运行中：品牌蓝底蓝字 + 呼吸圆点。
  running: { className: 'bg-primary/10 text-primary', dotClassName: 'bg-primary animate-pulse', labelKey: 'shell.statusRunning' },
  // 空闲：绿色（同“已修复”配色）。
  idle: { className: 'ws-st-fixed', labelKey: 'shell.statusIdle' },
  // 未开始：中性灰。
  not_started: { className: 'ws-st-unconfirmed', labelKey: 'shell.statusNotStarted' },
  unknown: { className: 'ws-st-unconfirmed', labelKey: 'shell.statusUnknown' },
}

/** 任务状态：圆角胶囊 + 圆点（running 品牌蓝并呼吸，idle 绿，not_started 中性灰）。 */
export function TaskStateBadge({ status, className }: {
  status: WebApiTaskListStatus
  className?: string
}) {
  const { t } = useTranslation('chat')
  const meta = TASK_STATE_CLASS[status] || TASK_STATE_CLASS.unknown
  return (
    <span className={cn('ws-st-badge', meta.className, className)}>
      <span aria-hidden className={cn('ws-st-dot', meta.dotClassName)} />
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

export function SeverityBadge({ severity, suggested, compact, className }: {
  severity: WebApiVulnSeverity | string | null | undefined
  /** AI 建议：虚线框 + Sparkles，文案“AI 建议：高危”。 */
  suggested?: boolean
  /** 窄列（列表等级列、总览 F、同资产漏洞）：只显示星标 + 等级名，完整文案放 title。 */
  compact?: boolean
  className?: string
}) {
  const { t } = useTranslation('chat')
  const key = severityKey(severity)
  const meta = SEVERITY_CLASS[key] || SEVERITY_CLASS.none
  const label = t(meta.labelKey)
  if (suggested) {
    const full = t('shell.triageAiSuggest', { level: label })
    return (
      <span className={cn('ws-sev-badge ws-sev-suggest', className)} title={compact ? full : undefined}>
        <Sparkles className="h-3 w-3" aria-hidden />
        {compact ? label : full}
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

/** 环境：test 灰 / staging 蓝 / prod 红 / 其它灰；文案原样显示（不做 i18n 映射）。 */
export function EnvBadge({ env, className }: { env: string | null | undefined; className?: string }) {
  if (!env) return null
  const key = env.trim().toLowerCase()
  const tone = key === 'prod' || key === 'production'
    ? 'ws-st-new'
    : key === 'staging' || key === 'stage'
      ? 'ws-st-pending'
      : 'ws-st-unconfirmed'
  return (
    <span className={cn('ws-st-badge', tone, className)}>
      <span aria-hidden className="ws-st-dot" />
      {env}
    </span>
  )
}

/** 企业微信集成状态：未配置灰 / 已启用绿 / 已配置未启用琥珀。 */
export function IntegrationStatusBadge({ configured, enabled, className }: {
  configured: boolean
  enabled: boolean
  className?: string
}) {
  const { t } = useTranslation('chat')
  const tone = !configured ? 'ws-st-unconfirmed' : enabled ? 'ws-st-fixed' : 'ws-st-present'
  const label = !configured
    ? t('shell.integrationsStatusUnconfigured')
    : enabled
      ? t('shell.integrationsStatusEnabled')
      : t('shell.integrationStatusConfiguredOff')
  return (
    <span className={cn('ws-st-badge', tone, className)}>
      <span aria-hidden className="ws-st-dot" />
      {label}
    </span>
  )
}

/** 节点状态：空闲绿 / 忙碌蓝 / 离线灰。 */
export function NodeStateBadge({ state, className }: {
  state: 'idle' | 'busy' | 'offline'
  className?: string
}) {
  const { t } = useTranslation('chat')
  const meta = state === 'busy'
    ? { className: 'bg-primary/10 text-primary', dotClassName: 'bg-primary animate-pulse', labelKey: 'shell.nodeBusy' }
    : state === 'idle'
      ? { className: 'ws-st-fixed', dotClassName: undefined, labelKey: 'shell.nodeIdle' }
      : { className: 'ws-st-unconfirmed', dotClassName: undefined, labelKey: 'shell.nodeOffline' }
  return (
    <span className={cn('ws-st-badge', meta.className, className)}>
      <span aria-hidden className={cn('ws-st-dot', meta.dotClassName)} />
      {t(meta.labelKey)}
    </span>
  )
}

/** 周期计划运行状态：已发起绿 / 失败红 / 已跳过灰（资产右栏运行记录时间线也用）。 */
export function RunStateBadge({ status, className }: {
  status: 'launched' | 'failed' | 'skipped'
  className?: string
}) {
  const { t } = useTranslation('chat')
  const meta = status === 'launched'
    ? { className: 'ws-st-fixed', labelKey: 'webapi.scheduleStatusLaunched' }
    : status === 'failed'
      ? { className: 'ws-st-new', labelKey: 'webapi.scheduleStatusFailed' }
      : { className: 'ws-st-unconfirmed', labelKey: 'webapi.scheduleStatusSkipped' }
  return (
    <span className={cn('ws-st-badge', meta.className, className)}>
      <span aria-hidden className="ws-st-dot" />
      {t(meta.labelKey)}
    </span>
  )
}

/** 周期计划启用状态：已启用绿 / 已停用灰。 */
export function ScheduleStateBadge({ enabled, className }: { enabled: boolean; className?: string }) {
  const { t } = useTranslation('chat')
  return (
    <span className={cn('ws-st-badge', enabled ? 'ws-st-fixed' : 'ws-st-unconfirmed', className)}>
      <span aria-hidden className="ws-st-dot" />
      {t(enabled ? 'webapi.scheduleEnabled' : 'webapi.scheduleDisabled')}
    </span>
  )
}
