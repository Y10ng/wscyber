import { useTranslation } from 'react-i18next'
import { Sheet, SheetContent, SheetTitle } from '@cyber/ui'
import { DeadlineText, DispositionBadge, SeverityBadge, StatusBadge } from './ui-badges'
import VulnTriageForm from './VulnTriageForm'
import type { WebApiGlobalVuln } from '../api'

interface Props {
  open: boolean
  item: WebApiGlobalVuln | null
  onClose: () => void
  /** 保存成功后刷新列表当前页。 */
  onSaved: () => void
}

// 列表快速处置抽屉：条目摘要 + 共用处置表单（VulnTriageForm）。
export default function VulnTriageSheet({ open, item, onClose, onSaved }: Props) {
  const { t } = useTranslation('chat')
  const assetName = item?.asset?.name || ''
  const status = item?.status || 'new'

  return (
    <Sheet open={open} onOpenChange={(next) => { if (!next) onClose() }}>
      <SheetContent
        side="right"
        closeLabel={t('webapi.close')}
        aria-describedby={undefined}
        className="flex w-full flex-col gap-0 p-0 sm:max-w-[420px]"
      >
        <div className="shrink-0 border-b border-border px-5 py-4 pr-12">
          <SheetTitle className="truncate text-[15px] font-semibold text-foreground" title={item?.title}>
            {item?.title || ''}
          </SheetTitle>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <StatusBadge status={status} />
            <SeverityBadge severity={item?.triage?.severity} />
            <DispositionBadge disposition={item?.triage?.disposition} />
            <DeadlineText
              due={item?.triage?.due_effective}
              overdue={item?.triage?.overdue}
              fixed={status === 'fixed'}
              className="text-[12px]"
            />
          </div>
          {assetName && (
            <div className="mt-1.5 truncate text-[12px] text-muted-foreground" title={assetName}>{assetName}</div>
          )}
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-5">
          {open && item ? (
            <VulnTriageForm
              vulnKey={item.id}
              triage={item.triage}
              status={status}
              suggestedSeverity={item.suggested_severity}
              onSaved={() => onSaved()}
              className="h-full"
            />
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}
