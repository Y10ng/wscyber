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
  vulnKey, triage, assetName, status, suggestedSeverity, onSaved, className, padded = false,
}: {
  vulnKey: string
  triage: WebApiVulnTriage | null
  assetName?: string
  status?: WebApiVulnStatus
  suggestedSeverity?: WebApiVulnSeverity
  onSaved?: (next: WebApiVulnTriage) => void
  className?: string
  /** Sheet 模式：内容区自身滚动，底部保存行固定在 Sheet 底部。 */
  padded?: boolean
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

  // 派生值（due_effective/overdue/owner_effective 等）跟随每次卡片刷新。
  useEffect(() => {
    setView(triage)
  }, [triage])

  // 只有切换条目或服务端处置真正被保存（updated_at 变化）时才回填可编辑字段，
  // 避免后台轮询刷新卡片时覆盖用户尚未保存的输入。
  useEffect(() => {
    if (!triage) return
    setSeverity(triage.severity || '')
    setDisposition(triage.disposition || 'open')
    setOwner(triage.owner || '')
    setDueDate(triage.due_date || '')
    setNote(triage.note || '')
    setSaveError('')
    setSaved(false)
  }, [vulnKey, triage?.updated_at])

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
      className={`flex min-h-0 flex-col ${padded ? 'gap-0' : 'gap-3'} ${className || ''}`}
      data-vuln-status={status || ''}
      onSubmit={(event) => { event.preventDefault(); void save() }}
    >
      <div className={padded ? 'min-h-0 flex-1 space-y-3 overflow-y-auto p-5' : 'min-h-0 flex-1 space-y-3'}>
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

      <div className={padded ? 'flex shrink-0 items-center justify-end gap-3 border-t border-border px-5 py-3' : 'flex shrink-0 items-center justify-end gap-3 border-t border-border pt-3'}>
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
