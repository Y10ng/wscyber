import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import {
  Button, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Textarea,
} from '@cyber/ui'
import { saveWebApiVulnTriage, type WebApiVulnDisposition, type WebApiVulnSeverity, type WebApiVulnStatus, type WebApiVulnTriage } from '../api'
import { SeverityBadge } from './ui-badges'

const SEVERITY_VALUES: WebApiVulnSeverity[] = ['', 'critical', 'high', 'medium', 'low', 'info']
const DISPOSITION_VALUES: WebApiVulnDisposition[] = ['open', 'fixing', 'risk_accepted', 'false_positive']

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

/**
 * 处置表单（V1 字段 + K1 的 AI 建议采纳）。漏洞列表 Sheet 与漏洞卡右栏共用；
 * 显式“保存”，保存后用后端返回的 Triage View 回填并通知调用方刷新。
 */
export default function VulnTriageForm({
  vulnKey, triage, assetName, status, suggestedSeverity, onSaved, className,
}: {
  vulnKey: string
  triage: WebApiVulnTriage | null
  assetName?: string
  status?: WebApiVulnStatus
  suggestedSeverity?: WebApiVulnSeverity
  onSaved?: (next: WebApiVulnTriage) => void
  className?: string
}) {
  const { t } = useTranslation('chat')
  const [severity, setSeverity] = useState<WebApiVulnSeverity>('')
  const [disposition, setDisposition] = useState<WebApiVulnDisposition>('open')
  const [owner, setOwner] = useState('')
  const [dueDate, setDueDate] = useState('')
  const [note, setNote] = useState('')
  const [view, setView] = useState<WebApiVulnTriage | null>(triage)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)
  const savedTimer = useRef<number | null>(null)

  // 条目切换或卡片重新拉取后用服务端视图回填。
  useEffect(() => {
    if (!triage) return
    setView(triage)
    setSeverity(triage.severity || '')
    setDisposition(triage.disposition || 'open')
    setOwner(triage.owner || '')
    setDueDate(triage.due_date || '')
    setNote(triage.note || '')
    setSaveError('')
    setSaved(false)
  }, [vulnKey, triage])

  useEffect(() => () => {
    if (savedTimer.current !== null) window.clearTimeout(savedTimer.current)
  }, [])

  const save = async () => {
    setSaving(true)
    setSaveError('')
    setSaved(false)
    try {
      const next = await saveWebApiVulnTriage(vulnKey, {
        severity,
        disposition,
        owner: owner.trim(),
        due_date: dueDate,
        note: note.trim(),
      })
      setView(next)
      setSeverity(next.severity || '')
      setDisposition(next.disposition || 'open')
      setOwner(next.owner || '')
      setDueDate(next.due_date || '')
      setNote(next.note || '')
      setSaved(true)
      if (savedTimer.current !== null) window.clearTimeout(savedTimer.current)
      savedTimer.current = window.setTimeout(() => setSaved(false), 3000)
      onSaved?.(next)
    } catch (err) {
      setSaveError(errorText(err))
    } finally {
      setSaving(false)
    }
  }

  const ownerPlaceholder = view?.owner_effective || t('shell.triageOwnerUnspecified')
  const slaDue = view?.due_effective || '—'

  return (
    <form
      className={`flex min-h-0 flex-col gap-3 ${className || ''}`}
      data-vuln-status={status || ''}
      onSubmit={(event) => { event.preventDefault(); void save() }}
    >
      <div className="min-h-0 flex-1 space-y-3">
        {assetName ? <p className="-mt-1 truncate text-[11px] text-muted-foreground" title={assetName}>{assetName}</p> : null}
        <div className="space-y-1">
          <div className="flex items-center justify-between gap-2">
            <span className="text-[12px] text-muted-foreground">{t('shell.triageSeverity')}</span>
            {suggestedSeverity ? (
              <button
                type="button"
                title={t('vulnCard.adoptAiHint')}
                onClick={() => setSeverity(suggestedSeverity)}
                className="rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <SeverityBadge severity={suggestedSeverity} suggested className="cursor-pointer text-[10.5px] hover:opacity-80" />
              </button>
            ) : null}
          </div>
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
        </div>

        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label className="block space-y-1">
            <span className="block text-[12px] text-muted-foreground">{t('shell.triageDisposition')}</span>
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

          <label className="block space-y-1">
            <span className="block text-[12px] text-muted-foreground">{t('shell.triageOwner')}</span>
            <Input
              value={owner}
              onChange={(event) => setOwner(event.target.value)}
              placeholder={ownerPlaceholder}
              maxLength={100}
              disabled={saving}
              className="h-9"
            />
          </label>
        </div>

        <label className="block space-y-1">
          <span className="block text-[12px] text-muted-foreground">{t('shell.triageDueDate')}</span>
          <Input
            type="date"
            value={dueDate}
            onChange={(event) => setDueDate(event.target.value)}
            disabled={saving}
            className="h-9"
          />
          <span className="block text-[11px] text-muted-foreground">
            {t('shell.triageDueHint', { date: slaDue })}
          </span>
        </label>

        <label className="block space-y-1">
          <span className="block text-[12px] text-muted-foreground">{t('shell.triageNote')}</span>
          <Textarea
            value={note}
            onChange={(event) => setNote(event.target.value)}
            rows={3}
            maxLength={2000}
            disabled={saving}
          />
        </label>
      </div>

      <div className="flex shrink-0 items-center justify-end gap-3 border-t border-border pt-3">
        {saveError && <span role="alert" className="mr-auto text-[11px] text-destructive">{saveError}</span>}
        {saved && <span className="mr-auto text-[12px] text-success">{t('shell.triageSaved')}</span>}
        <Button type="submit" disabled={saving}>
          {saving && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
          {t('shell.triageSave')}
        </Button>
      </div>
    </form>
  )
}
