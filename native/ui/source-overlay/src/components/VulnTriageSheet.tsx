import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import {
  Badge, Button, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
  Sheet, SheetContent, SheetTitle, Textarea,
} from '@cyber/ui'
import {
  saveWebApiVulnTriage,
  type WebApiGlobalVuln,
  type WebApiVulnDisposition,
  type WebApiVulnSeverity,
  type WebApiVulnTriage,
} from '../api'

interface Props {
  open: boolean
  item: WebApiGlobalVuln | null
  onClose: () => void
  /** 保存成功后刷新列表当前页。 */
  onSaved: () => void
}

const SEVERITY_VALUES: WebApiVulnSeverity[] = ['', 'critical', 'high', 'medium', 'low', 'info']
const DISPOSITION_VALUES: WebApiVulnDisposition[] = ['open', 'fixing', 'risk_accepted', 'false_positive']

const STATUS_KEYS: Record<string, string> = {
  new: 'webapi.vulnStatusNew',
  present: 'webapi.vulnStatusPresent',
  pending: 'webapi.vulnStatusPending',
  unconfirmed: 'webapi.vulnStatusUnconfirmed',
  fixed: 'webapi.vulnStatusFixed',
}

const STATUS_VARIANTS: Record<string, 'danger' | 'warning' | 'info' | 'muted' | 'success'> = {
  new: 'danger',
  present: 'warning',
  pending: 'info',
  unconfirmed: 'muted',
  fixed: 'success',
}

const SEVERITY_KEYS: Record<WebApiVulnSeverity, string> = {
  '': 'shell.triageSeverityNone',
  critical: 'shell.triageSeverityCritical',
  high: 'shell.triageSeverityHigh',
  medium: 'shell.triageSeverityMedium',
  low: 'shell.triageSeverityLow',
  info: 'shell.triageSeverityInfo',
}

const DISPOSITION_KEYS: Record<WebApiVulnDisposition, string> = {
  open: 'shell.triageDispositionOpen',
  fixing: 'shell.triageDispositionFixing',
  risk_accepted: 'shell.triageDispositionRiskAccepted',
  false_positive: 'shell.triageDispositionFalsePositive',
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export default function VulnTriageSheet({ open, item, onClose, onSaved }: Props) {
  const { t } = useTranslation('chat')
  const triage: WebApiVulnTriage | null = item?.triage ?? null

  const [severity, setSeverity] = useState<WebApiVulnSeverity>('')
  const [disposition, setDisposition] = useState<WebApiVulnDisposition>('open')
  const [owner, setOwner] = useState('')
  const [dueDate, setDueDate] = useState('')
  const [note, setNote] = useState('')
  // 最近一次打开/保存的 Triage View：表单回填与派生提示（SLA 期限、资产负责人）同源。
  const [view, setView] = useState<WebApiVulnTriage | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)
  const savedTimer = useRef<number | null>(null)

  // 打开条目时用当前 Triage View 回填表单；保存后返回的新 view 也走这里。
  useEffect(() => {
    if (!open || !triage) return
    setView(triage)
    setSeverity(triage.severity || '')
    setDisposition(triage.disposition || 'open')
    setOwner(triage.owner || '')
    setDueDate(triage.due_date || '')
    setNote(triage.note || '')
    setSaveError('')
    setSaved(false)
  }, [open, item?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => () => {
    if (savedTimer.current !== null) window.clearTimeout(savedTimer.current)
  }, [])

  const save = async () => {
    if (!item) return
    setSaving(true)
    setSaveError('')
    setSaved(false)
    try {
      const next = await saveWebApiVulnTriage(item.id, {
        severity,
        disposition,
        owner: owner.trim(),
        due_date: dueDate,
        note: note.trim(),
      })
      // 返回值回填：同步表单里被后端规范化的值（如去空白）。
      setView(next)
      setSeverity(next.severity || '')
      setDisposition(next.disposition || 'open')
      setOwner(next.owner || '')
      setDueDate(next.due_date || '')
      setNote(next.note || '')
      setSaved(true)
      if (savedTimer.current !== null) window.clearTimeout(savedTimer.current)
      savedTimer.current = window.setTimeout(() => setSaved(false), 3000)
      onSaved()
    } catch (err) {
      // 直接显示后端文案（非法取值、条目不存在等）。
      setSaveError(errorText(err))
    } finally {
      setSaving(false)
    }
  }

  const assetName = item?.asset?.name || ''
  const status = item?.status || 'new'
  const ownerPlaceholder = view?.owner_effective || t('shell.triageOwnerUnspecified')
  const slaDue = view?.due_effective || '—'

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
          <div className="mt-2 flex items-center gap-2">
            <Badge variant={STATUS_VARIANTS[status] || 'muted'} size="sm">{t(STATUS_KEYS[status] || 'webapi.vulnStatusNew')}</Badge>
            {assetName
              ? <span className="min-w-0 truncate text-[12px] text-muted-foreground" title={assetName}>{assetName}</span>
              : <Badge variant="muted" size="sm">{t('shell.vulnsNoAsset')}</Badge>}
          </div>
        </div>

        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => { event.preventDefault(); void save() }}
        >
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
            <label className="block space-y-1.5">
              <span className="block text-[13px] font-medium text-foreground/75">{t('shell.triageSeverity')}</span>
              <Select value={severity || 'none'} onValueChange={(value) => setSeverity(value === 'none' ? '' : value as WebApiVulnSeverity)}>
                <SelectTrigger aria-label={t('shell.triageSeverity')} className="h-9 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {SEVERITY_VALUES.map((value) => (
                    <SelectItem key={value || 'none'} value={value || 'none'}>{t(SEVERITY_KEYS[value])}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>

            <label className="block space-y-1.5">
              <span className="block text-[13px] font-medium text-foreground/75">{t('shell.triageDisposition')}</span>
              <Select value={disposition} onValueChange={(value) => setDisposition(value as WebApiVulnDisposition)}>
                <SelectTrigger aria-label={t('shell.triageDisposition')} className="h-9 w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DISPOSITION_VALUES.map((value) => (
                    <SelectItem key={value} value={value}>{t(DISPOSITION_KEYS[value])}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </label>

            <label className="block space-y-1.5">
              <span className="block text-[13px] font-medium text-foreground/75">{t('shell.triageOwner')}</span>
              <Input
                value={owner}
                onChange={(event) => setOwner(event.target.value)}
                placeholder={ownerPlaceholder}
                maxLength={100}
                disabled={saving}
              />
            </label>

            <label className="block space-y-1.5">
              <span className="block text-[13px] font-medium text-foreground/75">{t('shell.triageDueDate')}</span>
              <Input
                type="date"
                value={dueDate}
                onChange={(event) => setDueDate(event.target.value)}
                disabled={saving}
              />
              <span className="block text-xs text-muted-foreground">
                {t('shell.triageDueHint', { date: slaDue })}
              </span>
            </label>

            <label className="block space-y-1.5">
              <span className="block text-[13px] font-medium text-foreground/75">{t('shell.triageNote')}</span>
              <Textarea
                value={note}
                onChange={(event) => setNote(event.target.value)}
                rows={4}
                maxLength={2000}
                disabled={saving}
              />
            </label>

            {saveError && <p role="alert" className="text-xs text-destructive">{saveError}</p>}
          </div>

          <div className="flex shrink-0 items-center justify-end gap-3 border-t border-border px-5 py-3">
            {saved && <span className="mr-auto text-[12px] text-success">{t('shell.triageSaved')}</span>}
            <Button type="submit" disabled={saving}>
              {saving && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
              {t('shell.triageSave')}
            </Button>
          </div>
        </form>
      </SheetContent>
    </Sheet>
  )
}
