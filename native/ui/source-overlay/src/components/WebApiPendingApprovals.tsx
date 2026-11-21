import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { Check, ChevronRight, Clock3, Loader2, ShieldAlert, X } from 'lucide-react'
import { Button, Popover, PopoverContent, PopoverTrigger } from '@cyber/ui'
import type { Review } from '../cyber-proto'

// B1 顶栏「待审批 N」：替换原生 GuardrailToggle（JEV 全局开关，与本平台
// 任务策略无关、会误导）。N=0 不渲染；N>0 琥珀色。列表数据由 App 的
// useGuardrailReviews 聚合（bySession + unavailable），本组件只做展示与
// 批准/拒绝转发，不自行发请求。

export interface PendingItem {
  sessionId: string
  review: Review
  sessionName: string
  unavailable: boolean
}

// 关键参数：HTTP 方法+URL，或命令首 120 字（与 guardrailArguments 同约定：
// JSON 参数里找 command/cmd/script，其余键值对列出）。
function reviewKeyParams(review: Review): { text: string; title: string } {
  let value: unknown = ''
  try { value = JSON.parse(new TextDecoder().decode(review.call?.arguments?.data)) } catch {
    try { value = new TextDecoder().decode(review.call?.arguments?.data) } catch { value = '' }
  }
  const record = value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
  if (record) {
    const commandKey = ['command', 'cmd', 'script'].find((key) => typeof record[key] === 'string')
    const command = commandKey ? record[commandKey] as string : null
    if (command !== null) {
      const text = command.length > 120 ? `${command.slice(0, 120)}…` : command
      return { text, title: command }
    }
    if (typeof record.url === 'string') {
      const method = typeof record.method === 'string' ? record.method.toUpperCase() : ''
      const text = `${method ? `${method} ` : ''}${record.url}`
      return { text, title: text }
    }
  }
  if (typeof value === 'string' && value) {
    const text = value.length > 120 ? `${value.slice(0, 120)}…` : value
    return { text, title: value }
  }
  return { text: '', title: '' }
}

function useRemainingTime(expiresAt: number | undefined, active: boolean): string {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    if (!active || expiresAt === undefined) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [expiresAt, active])
  return useMemo(() => {
    if (expiresAt === undefined) return ''
    const remaining = Math.max(0, expiresAt - now)
    const minutes = Math.floor(remaining / 60_000)
    const seconds = Math.floor((remaining % 60_000) / 1000)
    return minutes > 0 ? `${minutes}m ${String(seconds).padStart(2, '0')}s` : `${seconds}s`
  }, [expiresAt, now])
}

function PendingReviewRow({ item, onResolve, onView, resolving }: {
  item: PendingItem
  onResolve: (item: PendingItem, approve: boolean) => Promise<void>
  onView: (sessionId: string) => void
  resolving: 'approve' | 'reject' | null
}) {
  const { t } = useTranslation('chat')
  const { t: ta } = useTranslation('app')
  const expiresAt = item.review.expiresAt ? timestampDate(item.review.expiresAt).getTime() : undefined
  const remaining = useRemainingTime(expiresAt, resolving === null)
  const params = reviewKeyParams(item.review)
  const toolName = item.review.call?.name || ''
  return (
    <div className="min-w-0 px-3 py-2.5">
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
        <span className="min-w-0 max-w-full truncate font-medium text-foreground" title={item.sessionName}>{item.sessionName}</span>
        <span className="shrink-0 rounded bg-muted px-1 py-0.5 font-mono text-[10px] text-muted-foreground">{toolName}</span>
        {remaining && <span className="flex shrink-0 items-center gap-0.5 text-muted-foreground"><Clock3 className="h-3 w-3" aria-hidden="true" />{t('webapi.pendingApprovalsRemaining', { time: remaining })}</span>}
      </div>
      {(params.text || item.review.decision?.reason) && (
        <div className="mt-1 min-w-0 space-y-0.5">
          {params.text && <p className="break-all font-mono text-[11px] text-foreground" title={params.title}>{params.text}</p>}
          {item.review.decision?.reason && <p className="break-all text-[11px] text-muted-foreground" title={item.review.decision.reason}>{item.review.decision.reason}</p>}
        </div>
      )}
      {item.unavailable && <p role="status" className="mt-1 text-[11px] text-warning">{ta('guardrailDisconnected')}</p>}
      <div className="mt-1.5 flex items-center gap-1.5">
        <Button
          type="button"
          size="xs"
          variant="outline"
          className="h-8 gap-1 px-2.5 text-[12px] text-destructive hover:text-destructive"
          disabled={resolving !== null || item.unavailable}
          onClick={() => { void onResolve(item, false) }}
        >
          {resolving === 'reject' ? <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" /> : <X className="h-3 w-3" aria-hidden="true" />}
          {t('webapi.pendingApprovalsReject')}
        </Button>
        <Button
          type="button"
          size="xs"
          className="h-8 gap-1 px-2.5 text-[12px]"
          disabled={resolving !== null || item.unavailable}
          onClick={() => { void onResolve(item, true) }}
        >
          {resolving === 'approve' ? <Loader2 className="h-3 w-3 animate-spin" aria-hidden="true" /> : <Check className="h-3 w-3" aria-hidden="true" />}
          {t('webapi.pendingApprovalsApprove')}
        </Button>
        <button
          type="button"
          className="ml-auto flex shrink-0 items-center gap-0.5 rounded px-1 text-[11px] text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
          onClick={() => onView(item.sessionId)}
          title={t('webapi.pendingApprovalsViewSession')}
        >
          {t('webapi.pendingApprovalsView')}
          <ChevronRight className="h-3 w-3" aria-hidden="true" />
        </button>
      </div>
    </div>
  )
}

interface Props {
  items: PendingItem[]
  onResolve: (item: PendingItem, approve: boolean) => Promise<void>
  onViewSession: (sessionId: string) => void
  /** 外部（总览待审批卡）一次性请求打开 Popover；消费后由上层清空。 */
  openRequested?: boolean
  onOpenRequestedConsumed?: () => void
}

// 顶栏按钮：琥珀色「盾牌 + 待审批 N」。N=0 不渲染。Popover 列出全部待审批，
// 每条可批准/拒绝或跳到该会话；不可用时（连接断开）显示提示、按钮禁用。
export function PendingApprovalsButton({ items, onResolve, onViewSession, openRequested, onOpenRequestedConsumed }: Props) {
  const { t } = useTranslation('chat')
  const [open, setOpen] = useState(false)
  const [resolving, setResolving] = useState<{ key: string; action: 'approve' | 'reject' } | null>(null)
  // 外部一次性请求：有可审批项时展开 Popover；无项也消费请求，避免后续误开。
  useEffect(() => {
    if (!openRequested) return
    if (items.length > 0) setOpen(true)
    onOpenRequestedConsumed?.()
  }, [openRequested, items.length, onOpenRequestedConsumed])
  const count = items.length
  if (count === 0) return null

  const resolve = async (item: PendingItem, approve: boolean) => {
    const key = `${item.sessionId}:${item.review.operation?.operationId || ''}`
    if (resolving) return
    setResolving({ key, action: approve ? 'approve' : 'reject' })
    try { await onResolve(item, approve) } finally { setResolving(null) }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="soft"
          size="xs"
          aria-label={t('webapi.pendingApprovalsControl', { count })}
          data-pending-approvals={count}
          className="h-auto w-full justify-between gap-2 rounded-md border px-2.5 py-2 text-[12px] font-semibold"
          style={{
            background: 'var(--sev-high-bg)',
            color: 'var(--sev-high-text)',
            borderColor: 'var(--sev-high-border)',
          }}
        >
          <span className="flex min-w-0 items-center gap-2">
            <ShieldAlert className="h-4 w-4 shrink-0" aria-hidden="true" />
            <span className="truncate">{t('shell.pendingApprovalsCard')}</span>
          </span>
          <span className="shrink-0 rounded bg-orange-600 px-1.5 py-0.5 text-[10.5px] font-semibold tabular text-white">
            {count}
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(22.5rem,calc(100vw-2rem))] rounded-lg border border-border p-0 shadow-md">
        <div className="border-b border-border px-3 py-2.5 text-[14px] font-semibold text-foreground">
          {t('webapi.pendingApprovalsTitle', { count })}
        </div>
        <div className="max-h-[60vh] divide-y divide-border overflow-y-auto">
          {items.map((item) => {
            const key = `${item.sessionId}:${item.review.operation?.operationId || ''}`
            return <PendingReviewRow
              key={key}
              item={item}
              onResolve={resolve}
              onView={(sessionId) => { setOpen(false); onViewSession(sessionId) }}
              resolving={resolving?.key === key ? resolving.action : null}
            />
          })}
        </div>
      </PopoverContent>
    </Popover>
  )
}

// 会话名解析：App 传入已解析好的名称（会话标题或业务徽标），此处只做展示。
