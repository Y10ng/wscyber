import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Gauge } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { getWebApiUsage, type WebApiUsageTotals } from '../api'
import { formatTokens } from '../lib/format-tokens'
import { usePolling } from '../hooks/usePolling'

// 任务顶栏的 token 合计（U2g）：数据用任务用量接口（GET /api/webapi/usage?task_id=）。
// 10s 只读轮询只在任务运行中启用（running 由 App 按当前会话是否有进行中的轮次给出）；
// usePolling 自带页面隐藏暂停。点击切到「用量」页签。

interface Props {
  taskID: string
  running: boolean
  onOpenUsage: () => void
}

export default function WebApiTaskUsageTotal({ taskID, running, onOpenUsage }: Props) {
  const { t } = useTranslation('chat')
  const [totals, setTotals] = useState<WebApiUsageTotals | null>(null)
  const seq = useRef(0)

  const load = useCallback(async () => {
    const current = ++seq.current
    try {
      const report = await getWebApiUsage({ taskId: taskID })
      if (current !== seq.current) return
      setTotals(report.totals)
    } catch {
      // 失败保留上一次数值；下一次轮询或切任务时再试。不显示假 0。
    }
  }, [taskID])

  useEffect(() => {
    seq.current++
    setTotals(null)
    void load()
    return () => { seq.current++ }
  }, [load])

  usePolling(() => { void load() }, 10000, running)

  if (!totals) return null
  const label = `${formatTokens(totals.total_tokens)} tokens`
  const rows: Array<[string, string]> = [
    [t('webapi.usageInput'), totals.input_tokens.toLocaleString('en-US')],
    [t('webapi.usageOutput'), totals.output_tokens.toLocaleString('en-US')],
    [t('webapi.usageCacheRead'), totals.cache_read_tokens.toLocaleString('en-US')],
    [t('webapi.usageCacheWrite'), totals.cache_write_tokens.toLocaleString('en-US')],
    [t('webapi.usageTotal'), totals.total_tokens.toLocaleString('en-US')],
    [t('webapi.usageCallsFull'), totals.calls.toLocaleString('en-US')],
  ]

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={t('webapi.taskUsageTotal', { value: label })}
          onClick={onOpenUsage}
          data-task-usage-total={totals.total_tokens}
          className="flex h-6 shrink-0 items-center gap-1 rounded px-1.5 text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <Gauge className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />
          <span className="hidden whitespace-nowrap font-mono text-[11px] tabular-nums sm:inline">{label}</span>
        </button>
      </TooltipTrigger>
      <TooltipContent className="w-[220px] p-2.5">
        <p className="mb-1.5 text-[12px] font-semibold text-foreground">{t('webapi.taskUsageTitle')}</p>
        <dl className="space-y-0.5">
          {rows.map(([name, value]) => (
            <div key={name} className="flex items-baseline justify-between gap-3">
              <dt className="text-[11px] text-muted-foreground">{name}</dt>
              <dd className="font-mono text-[11px] tabular-nums text-foreground">{value}</dd>
            </div>
          ))}
        </dl>
        <p className="mt-1.5 text-[10.5px] text-muted-foreground">{t('webapi.taskUsageClickHint')}</p>
      </TooltipContent>
    </Tooltip>
  )
}