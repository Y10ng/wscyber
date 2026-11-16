import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FileText, ListChecks, Play, Radio } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@cyber/theme'
import {
  listWebApiEvidence,
  listWebApiFindings,
  listWebApiReports,
  type WebApiTask,
} from '../api'
import type { WebApiView } from './WebApiTaskBar'

interface Props {
  task: WebApiTask
  view: WebApiView
  onViewChange: (view: WebApiView) => void
  /** 失效信号：新建验证/报告成功后 App bump，本组件重取计数。 */
  countsRevision?: number
}

/** 计数形态：数字或 “N+”（接口无 total，首页满页 + has_more）。null = 取不到不显示。 */
type CountState = string | null

interface NavItem {
  view: WebApiView
  icon: LucideIcon
  labelKey: string
  count: CountState
}

// 任务局部导航（UI 改版 B）：执行 / 流量 / 候选 / 报告，图标 + 文字 + 右侧计数。
// ≥1100px 视口渲染为 168px 竖列；<1100px 由 .webapi-task-row（垂直堆叠）+ 媒体查询
// 收为横向分段。计数取列表接口第一页（无 total 字段）：has_more 用 “N+”；
// 取不到就不显示，不显示假 0。countsRevision 变化时重取（新建报告/验证后计数即时更新）。
export default function WebApiTaskNav({ task, view, onViewChange, countsRevision = 0 }: Props) {
  const { t } = useTranslation('chat')
  const [counts, setCounts] = useState<{ traffic: CountState; findings: CountState; reports: CountState }>({
    traffic: null,
    findings: null,
    reports: null,
  })
  const seq = useRef(0)

  const loadCounts = useCallback(async () => {
    const current = ++seq.current
    const [evidence, findings, reports] = await Promise.all([
      listWebApiEvidence(task.id, 100).catch(() => null),
      listWebApiFindings(task.id, 25).catch(() => null),
      listWebApiReports(task.id, 25).catch(() => null),
    ])
    if (current !== seq.current) return
    // 每类独立落地：失败只留空（不显示数字），不影响其他类。
    setCounts({
      traffic: evidence ? `${evidence.items.length}${evidence.has_more ? '+' : ''}` : null,
      findings: findings ? `${findings.items.length}${findings.has_more ? '+' : ''}` : null,
      reports: reports ? `${reports.items.length}${reports.has_more ? '+' : ''}` : null,
    })
  }, [task.id])

  // 切任务即重算；卸载使迟到响应失效。
  useEffect(() => {
    setCounts({ traffic: null, findings: null, reports: null })
    void loadCounts()
    return () => { seq.current++ }
  }, [loadCounts])

  // 新建验证/报告成功（App bump）或切换视图后静默校准一次计数。
  useEffect(() => {
    void loadCounts()
  }, [view, countsRevision, loadCounts])

  const items: NavItem[] = [
    { view: 'execution', icon: Play, labelKey: 'webapi.execution', count: null },
    { view: 'traffic', icon: Radio, labelKey: 'webapi.traffic', count: counts.traffic },
    { view: 'findings', icon: ListChecks, labelKey: 'webapi.findings', count: counts.findings },
    { view: 'reports', icon: FileText, labelKey: 'webapi.reports', count: counts.reports },
  ]

  return (
    <nav aria-label={t('webapi.taskNav')} className="webapi-task-nav flex min-h-0 min-w-0 shrink-0 flex-col overflow-y-auto border-border/60 bg-background py-2">
      {items.map((item) => {
        const active = view === item.view
        const Icon = item.icon
        return (
          <button
            key={item.view}
            type="button"
            onClick={() => onViewChange(item.view)}
            aria-current={active ? 'page' : undefined}
            title={t(item.labelKey)}
            className={cn(
              'webapi-task-nav-item group relative flex h-[34px] shrink-0 items-center gap-2 px-3 text-left text-[13px] transition-colors',
              'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 focus-visible:ring-inset',
              active ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
            )}
          >
            <span aria-hidden className={cn('absolute inset-y-0 left-0 w-0.5 bg-primary transition-opacity', active ? 'opacity-100' : 'opacity-0')} />
            <Icon className="h-3.5 w-3.5 shrink-0" />
            <span className="webapi-task-nav-label min-w-0 flex-1 truncate">{t(item.labelKey)}</span>
            {item.count !== null && (
              <span className="font-mono text-[11px] tabular-nums">{item.count}</span>
            )}
          </button>
        )
      })}
    </nav>
  )
}
