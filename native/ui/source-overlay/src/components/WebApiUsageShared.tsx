import { useTranslation } from 'react-i18next'
import { Info, Loader2, RefreshCw } from 'lucide-react'
import { cn } from '@cyber/theme'
import { Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import type { WebApiUsageTotals } from '../api'

// 用量视图共享件（任务视图 + 全局 Dialog 同一刻度）：
// 合计一行、按模型/按日期紧凑表、角色标签与配色、千分位格式化。
// 数值列一律右对齐 + 等宽 tabular-nums，与 WebApiEvidencePanel 表格密度对齐。

export function formatUsageTokens(value: number): string {
  return value.toLocaleString('en-US')
}

const ROLE_LABEL_KEYS: Record<string, string> = {
  research: 'webapi.usageRoleResearch',
  verification: 'webapi.usageRoleVerification',
  report: 'webapi.usageRoleReport',
  retest: 'webapi.usageRoleRetest',
}

export function usageRoleLabelKey(role: string): string {
  return ROLE_LABEL_KEYS[role] || role
}

// 状态色配文字（同 evidence 面板 tone 写法）：研究蓝 / 验证绿 / 报告琥珀 / 复测紫。
export function usageRoleTone(role: string): string {
  if (role === 'research') return 'text-blue-600 dark:text-blue-400'
  if (role === 'verification') return 'text-emerald-600 dark:text-emerald-400'
  if (role === 'report') return 'text-amber-600 dark:text-amber-400'
  if (role === 'retest') return 'text-purple-600 dark:text-purple-400'
  return 'text-muted-foreground'
}

function UsageMetric({ label, value }: { label: string; value: number }) {
  return (
    <span className="flex shrink-0 items-baseline gap-1.5">
      <span className="text-[12px] font-normal text-muted-foreground">{label}</span>
      <span className="font-mono text-[13px] tabular-nums text-foreground">{formatUsageTokens(value)}</span>
    </span>
  )
}

interface TotalsRowProps {
  totals: WebApiUsageTotals
  skippedEvents: number
  onRefresh?: () => void
  refreshing?: boolean
}

// 合计一行（非卡片）：输入 · 输出 · 缓存读 · 缓存写 · 总计 · 调用次数 + skipped 警示
// + info tooltip + 右侧刷新按钮（提供 onRefresh 时）。窄屏自动换行。
export function UsageTotalsRow({ totals, skippedEvents, onRefresh, refreshing }: TotalsRowProps) {
  const { t } = useTranslation('chat')
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-border/60 px-3 py-2">
      <UsageMetric label={t('webapi.usageInput')} value={totals.input_tokens} />
      <UsageMetric label={t('webapi.usageOutput')} value={totals.output_tokens} />
      <UsageMetric label={t('webapi.usageCacheRead')} value={totals.cache_read_tokens} />
      <UsageMetric label={t('webapi.usageCacheWrite')} value={totals.cache_write_tokens} />
      <UsageMetric label={t('webapi.usageTotal')} value={totals.total_tokens} />
      <UsageMetric label={t('webapi.usageCallsFull')} value={totals.calls} />
      {skippedEvents > 0 && (
        <span className="shrink-0 text-[11px] text-warning" title={t('webapi.usageSkipped', { count: skippedEvents })}>
          {t('webapi.usageSkipped', { count: skippedEvents })}
        </span>
      )}
      <span className={cn('flex shrink-0 items-center gap-1', !onRefresh && 'ml-auto')}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span
              role="img"
              aria-label={t('webapi.usageNote')}
              className="flex h-5 w-5 cursor-help items-center justify-center rounded text-muted-foreground"
            >
              <Info className="h-3.5 w-3.5" />
            </span>
          </TooltipTrigger>
          <TooltipContent className="max-w-xs">{t('webapi.usageNote')}</TooltipContent>
        </Tooltip>
      </span>
      {onRefresh && (
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              onClick={onRefresh}
              disabled={refreshing}
              aria-label={t('webapi.refresh')}
              className="ml-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground disabled:opacity-50"
            >
              {refreshing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
            </button>
          </TooltipTrigger>
          <TooltipContent>{t('webapi.refresh')}</TooltipContent>
        </Tooltip>
      )}
    </div>
  )
}

export interface UsageCompactRow {
  key: string
  name: string
  title?: string
  input: number
  output: number
  total: number
  calls: number
}

interface CompactTableProps {
  /** 首列标题键（webapi.usageModel / webapi.usageDay）。 */
  nameKey: string
  rows: UsageCompactRow[]
  /** 行点击（按任务表跳转用）；缺省不可点。 */
  onRowClick?: (row: UsageCompactRow) => void
  /** 不可点行（task_id 为空）在 name 单元格渲染的降级文案。 */
  nameFallback?: (row: UsageCompactRow) => string | undefined
}

// 「按模型」「按日期」「按任务」共用的紧凑表：首列 + 输入/输出/总计/调用（调用列较窄）。
export function UsageCompactTable({ nameKey, rows, onRowClick, nameFallback }: CompactTableProps) {
  const { t } = useTranslation('chat')
  return (
    <table className="w-full border-separate border-spacing-0 text-left">
      <thead>
        <tr>
          <th scope="col" className="border-b border-border/60 bg-background px-2 py-1 text-[12px] font-medium text-muted-foreground">{t(nameKey)}</th>
          <th scope="col" className="w-[90px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageInput')}</th>
          <th scope="col" className="w-[90px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageOutput')}</th>
          <th scope="col" className="w-[100px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageTotal')}</th>
          <th scope="col" className="w-[70px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageCalls')}</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => {
          const fallback = nameFallback?.(row)
          const clickable = !!onRowClick && !fallback
          return (
            <tr
              key={row.key}
              tabIndex={clickable ? 0 : undefined}
              onClick={clickable ? () => onRowClick!(row) : undefined}
              onKeyDown={clickable ? (event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onRowClick!(row) } } : undefined}
              className={cn(
                'h-[30px] transition-colors',
                clickable && 'cursor-pointer hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary/40',
              )}
            >
              <td className="max-w-0 truncate border-b border-border/40 px-2 text-[12px] text-foreground" title={row.title ?? row.name}>
                {fallback ?? row.name}
              </td>
              <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-foreground">{formatUsageTokens(row.input)}</td>
              <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-foreground">{formatUsageTokens(row.output)}</td>
              <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-foreground">{formatUsageTokens(row.total)}</td>
              <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-muted-foreground">{formatUsageTokens(row.calls)}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

export function compactRowFrom(key: string, name: string, u: WebApiUsageTotals, title?: string): UsageCompactRow {
  return { key, name, title, input: u.input_tokens, output: u.output_tokens, total: u.total_tokens, calls: u.calls }
}
