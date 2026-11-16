import { useTranslation } from 'react-i18next'
import { Globe, ShieldCheck } from 'lucide-react'
import type { WebApiTask } from '../api'

export type WebApiView = 'execution' | 'traffic' | 'findings' | 'reports'

interface Props {
  task: WebApiTask
}

// 真正 Web/API 任务才显示的范围摘要栏；普通会话不渲染。
// 视图切换已上移到任务局部导航（WebApiTaskNav），这里只保留目标与授权 origin。
export default function WebApiTaskBar({ task }: Props) {
  const summary = `${task.known_info}\n\n${task.objective}`.trim()
  return (
    <div className="flex shrink-0 items-center gap-2 border-b border-border/60 bg-background px-4 py-1.5">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Globe className="h-3.5 w-3.5 shrink-0 text-primary" />
        <span className="min-w-0 truncate text-[13px] font-medium text-foreground" title={summary}>{task.objective}</span>
        <span className="hidden shrink-0 items-center gap-1 text-muted-foreground sm:flex">
          <ShieldCheck className="h-3 w-3 shrink-0" />
          {task.origins.map((origin) => (
            <span key={origin} className="font-mono text-[12px]" title={origin}>{origin}</span>
          ))}
        </span>
      </div>
    </div>
  )
}
