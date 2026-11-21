import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Gauge, Loader2 } from 'lucide-react'
import { Button, Dialog, DialogContent } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { getWebApiUsage, type WebApiUsageReport } from '../api'
import {
  UsageCompactTable,
  UsageTotalsRow,
  compactRowFrom,
} from './WebApiUsageShared'
import { DialogFooterRow, DialogHeaderRow } from './ui-dialog'

interface Props {
  open: boolean
  onClose: () => void
  /** 点击任务行：关闭 Dialog 并打开该任务（父级负责解析 task → 会话）。 */
  onOpenTask: (taskID: string) => void
}

type RangeMode = '7d' | '30d' | 'custom'

// 浏览器时区 IANA 名（拿不到回落 UTC）。
function browserTimeZone(): string {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' } catch { return 'UTC' }
}

// 指定时区的“今天”（YYYY-MM-DD）。en-CA 的年月日格式即 ISO 日期。
function dayInZone(timeZone: string): string {
  try {
    return new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date())
  } catch {
    return new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date())
  }
}

// 纯天数位移（按 UTC 日历日加减，与时区无关）。
function shiftDays(day: string, delta: number): string {
  const date = new Date(`${day}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + delta)
  return date.toISOString().slice(0, 10)
}

// 全局用量 Dialog（任务列表标题行「用量」入口）：范围切换（近 7 天默认 / 近 30 天 / 自定义）
// → 合计一行 → 按日期（降序）→ 按任务（可点跳转）→ 按模型。只读、不轮询、无费用。
export default function WebApiUsageDialog({ open, onClose, onOpenTask }: Props) {
  const { t } = useTranslation('chat')
  const [range, setRange] = useState<RangeMode>('7d')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [report, setReport] = useState<WebApiUsageReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const seq = useRef(0)
  // 无 Trigger 的受控 Dialog：沿 ConfigPanel/ReportDialog 已验模式还原焦点。
  const returnFocus = useRef<HTMLElement | null>(null)
  const contentElement = useRef<HTMLElement | null>(null)

  // 首次响应前用浏览器时区估相对范围；响应带回 Hub 时区后如不同则以 Hub 时区重算重取一次。
  const [hubZone, setHubZone] = useState('')
  const zone = hubZone || browserTimeZone()

  const params = useMemo(() => {
    if (range === 'custom') return { from, to }
    const today = dayInZone(zone)
    const span = range === '7d' ? 6 : 29
    return { from: shiftDays(today, -span), to: today }
  }, [range, from, to, zone])

  const customInvalid = range === 'custom' && from !== '' && to !== '' && from > to
  const customIncomplete = range === 'custom' && (from === '' || to === '')

  const load = useCallback(async () => {
    const current = ++seq.current
    setLoading(true)
    setError('')
    try {
      const result = await getWebApiUsage({ from: params.from, to: params.to })
      if (current !== seq.current) return
      setReport(result)
      if (result.timezone) setHubZone(result.timezone)
    } catch (err) {
      if (current !== seq.current) return
      setReport(null)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (current === seq.current) setLoading(false)
    }
  }, [params.from, params.to])

  // 打开即重置并加载；范围参数变化重载（含 Hub 时区首次明确后的单次校正）。
  // 自定义未补全/非法时保留当前展示，等输入合法后再取。
  useEffect(() => {
    if (!open) return
    seq.current++
    setReport(null)
    setError('')
  }, [open])

  useEffect(() => {
    if (!open || customInvalid || customIncomplete) return
    void load()
  }, [open, load, customInvalid, customIncomplete])

  const daysDesc = useMemo(() => (report ? [...report.by_day].reverse() : []), [report])
  const empty = !!report && report.totals.calls === 0 && (report.by_task?.length ?? 0) === 0 && report.by_model.length === 0 && report.by_day.length === 0

  const rangeOptions: { key: RangeMode; label: string }[] = [
    { key: '7d', label: t('webapi.usageRange7') },
    { key: '30d', label: t('webapi.usageRange30') },
    { key: 'custom', label: t('webapi.usageRangeCustom') },
  ]

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
      <DialogContent
        closeLabel={t('webapi.close')}
        aria-describedby={undefined}
        onInteractOutside={(event) => event.preventDefault()}
        onOpenAutoFocus={(event) => {
          returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
          contentElement.current = event.target instanceof HTMLElement ? event.target : null
        }}
        onCloseAutoFocus={(event) => {
          const active = document.activeElement
          if (!event.defaultPrevented && returnFocus.current?.isConnected
            && (active === document.body || contentElement.current?.contains(active))) {
            event.preventDefault()
            returnFocus.current.focus()
          }
        }}
        overlayClassName="z-[70]"
        className="z-[80] flex h-[min(80dvh,48rem)] w-[calc(100vw-2rem)] max-w-[960px] flex-col gap-0 overflow-hidden p-0"
      >
        <DialogHeaderRow icon={<Gauge className="h-4 w-4" />} title={t('webapi.usageTitle')} />

        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/60 px-4 py-2">
          <div role="group" aria-label={t('webapi.usageTitle')} className="flex shrink-0 items-center gap-0.5 rounded-md border border-border/60 p-0.5">
            {rangeOptions.map((option) => (
              <button
                key={option.key}
                type="button"
                aria-pressed={range === option.key}
                onClick={() => setRange(option.key)}
                className={cn(
                  'h-6 rounded px-1.5 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                  range === option.key ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                )}
              >
                {option.label}
              </button>
            ))}
          </div>
          {range === 'custom' && (
            <>
              <input
                type="date"
                value={from}
                onChange={(event) => setFrom(event.target.value)}
                aria-label={t('webapi.usageFrom')}
                className="h-7 rounded-md border border-border/60 bg-background px-2 text-[12px] text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              />
              <span aria-hidden className="text-[12px] text-muted-foreground">–</span>
              <input
                type="date"
                value={to}
                onChange={(event) => setTo(event.target.value)}
                aria-label={t('webapi.usageTo')}
                className="h-7 rounded-md border border-border/60 bg-background px-2 text-[12px] text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              />
            </>
          )}
          {customInvalid && <span className="text-[11px] text-destructive">{t('webapi.usageBadRange')}</span>}
          {loading && <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" aria-label={t('webapi.usageLoading')} />}
          <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">{t('webapi.usageTimezone', { timezone: report?.timezone || zone })}</span>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {loading && !report ? (
            <div className="flex h-24 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : error ? (
            <div className="flex flex-col items-center gap-2 px-3 py-6 text-center">
              <span className="text-sm text-destructive">{error}</span>
              <Button type="button" variant="outline" size="xs" onClick={() => void load()}>{t('webapi.usageRetry')}</Button>
            </div>
          ) : !report ? (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">{t('webapi.usageEmpty')}</div>
          ) : empty ? (
            <div className="px-3 py-6 text-center text-sm text-muted-foreground">{t('webapi.usageEmpty')}</div>
          ) : (
            <div className="space-y-5">
              <UsageTotalsRow totals={report.totals} skippedEvents={report.skipped_events} />
              <div className="space-y-5 px-4 pb-4">
                <section aria-label={t('webapi.usageByDate')} className="space-y-1.5">
                  <h2 className="text-[14px] font-semibold text-foreground">{t('webapi.usageByDate')}</h2>
                  <UsageCompactTable
                    nameKey="webapi.usageDay"
                    rows={daysDesc.map((day) => compactRowFrom(day.day, day.day, day))}
                  />
                </section>
                <section aria-label={t('webapi.usageByTask')} className="space-y-1.5">
                  <h2 className="text-[14px] font-semibold text-foreground">{t('webapi.usageByTask')}</h2>
                  <UsageCompactTable
                    nameKey="webapi.usageTask"
                    rows={(report.by_task ?? []).map((task) =>
                      compactRowFrom(task.task_id, task.title || task.task_id, task, task.title || undefined))}
                    onRowClick={(row) => onOpenTask(row.key)}
                    nameFallback={(row) => (row.key === '' ? t('webapi.usageUnassigned') : undefined)}
                  />
                </section>
                <section aria-label={t('webapi.usageByModel')} className="space-y-1.5">
                  <h2 className="text-[14px] font-semibold text-foreground">{t('webapi.usageByModel')}</h2>
                  <UsageCompactTable
                    nameKey="webapi.usageModel"
                    rows={report.by_model.map((model) => compactRowFrom(model.model, model.model, model))}
                  />
                </section>
              </div>
            </div>
          )}
        </div>
        <DialogFooterRow>
          <Button type="button" variant="outline" onClick={onClose}>{t('webapi.close')}</Button>
        </DialogFooterRow>
      </DialogContent>
    </Dialog>
  )
}
