import { useTranslation } from 'react-i18next'
import { Globe, ShieldCheck } from 'lucide-react'
import { Button } from '@cyber/ui'
import type { WebApiTask } from '../api'

export type WebApiView = 'execution' | 'traffic' | 'findings' | 'reports'

interface Props {
  task: WebApiTask
  view: WebApiView
  onViewChange: (view: WebApiView) => void
}

// 真正 Web/API 任务才显示的范围摘要与「执行 / 流量」视图切换；普通会话不渲染。
export default function WebApiTaskBar({ task, view, onViewChange }: Props) {
  const { t } = useTranslation('chat')
  const summary = `${task.known_info}\n\n${task.objective}`.trim()
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/60 bg-background px-4 py-1.5">
      <div className="flex min-w-0 flex-1 items-center gap-2 text-xs">
        <Globe className="h-3.5 w-3.5 shrink-0 text-primary" />
        <span className="min-w-0 truncate text-[13px] font-medium text-foreground/80" title={summary}>{task.objective}</span>
        <span className="hidden shrink-0 items-center gap-1 text-muted-foreground sm:flex">
          <ShieldCheck className="h-3 w-3 shrink-0" />
          {task.origins.map((origin) => (
            <span key={origin} className="font-mono text-[11px]" title={origin}>{origin}</span>
          ))}
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-0.5 rounded-md border border-border/60 p-0.5">
        <Button type="button" variant="ghost" size="xs" active={view === 'execution'} onClick={() => onViewChange('execution')} className="h-7 px-2 text-[12px]">{t('webapi.execution')}</Button>
        <Button type="button" variant="ghost" size="xs" active={view === 'traffic'} onClick={() => onViewChange('traffic')} className="h-7 px-2 text-[12px]">{t('webapi.traffic')}</Button>
        <Button type="button" variant="ghost" size="xs" active={view === 'findings'} onClick={() => onViewChange('findings')} className="h-7 px-2 text-[12px]">{t('webapi.findings')}</Button>
        <Button type="button" variant="ghost" size="xs" active={view === 'reports'} onClick={() => onViewChange('reports')} className="h-7 px-2 text-[12px]">{t('webapi.reports')}</Button>
      </div>
    </div>
  )
}
