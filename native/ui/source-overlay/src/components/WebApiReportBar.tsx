import { useTranslation } from 'react-i18next'
import { ArrowLeft, FileText } from 'lucide-react'
import { Button } from '@cyber/ui'
import type { WebApiReportDetail } from '../api'

// 原生报告 session 的紧凑上下文栏：只读展示报告固定条目与草稿状态，并提供「返回报告」。
// 报告不确认漏洞、不裁定严重度，草稿只是报告会话模型的撰写。
export default function WebApiReportBar({ report, onReturnToReport, onBack }: { report: WebApiReportDetail; onReturnToReport: () => void; onBack: () => void }) {
  const { t } = useTranslation('chat')
  const title = report.draft?.payload.title?.trim()
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/60 bg-background px-3 py-1.5">
      <Button type="button" variant="ghost" size="icon-sm" onClick={onBack} aria-label={t('shell.backToTasks')} title={t('shell.backToTasks')} className="shrink-0 text-muted-foreground">
        <ArrowLeft className="h-4 w-4" />
      </Button>
      <div className="flex min-w-0 flex-1 items-center gap-2 text-xs">
        <FileText className="h-3.5 w-3.5 shrink-0 text-primary" />
        <span className="min-w-0 truncate text-[13px] font-medium text-foreground/80" title={title || report.report_id}>
          {t('webapi.reports')} · {title || `${t('webapi.reportUntitled')}（${report.report_id.slice(0, 8)}）`}
        </span>
        <span className="shrink-0 text-muted-foreground">{t('webapi.reportItemsCount', { count: report.items.length })}</span>
        {report.has_draft ? (
          <span className="shrink-0 text-emerald-600 dark:text-emerald-400">{t('webapi.reportHasDraft')}</span>
        ) : (
          <span className="shrink-0 text-muted-foreground">{t('webapi.reportNoDraft')}</span>
        )}
      </div>
      <Button type="button" variant="outline" onClick={onReturnToReport} className="h-8 shrink-0 gap-1 text-[12px]">
        <ArrowLeft className="h-3.5 w-3.5" />
        {t('webapi.reportReturnToReports')}
      </Button>
    </div>
  )
}
