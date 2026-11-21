import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { Button, Dialog, DialogContent, Input, Spinner } from '@cyber/ui'
import { getWebApiVulnSLA, saveWebApiVulnSLA, type WebApiVulnSLA } from '../api'
import { DialogFooterRow, DialogHeaderRow } from './ui-dialog'

interface Props {
  open: boolean
  onClose: () => void
  /** 保存成功后刷新列表（期限为读时计算，列表需重新取数）。 */
  onSaved: () => void
}

type FieldKey = 'critical_days' | 'high_days' | 'medium_days' | 'low_days'

const FIELDS: Array<{ key: FieldKey; labelKey: string }> = [
  { key: 'critical_days', labelKey: 'shell.triageSeverityCritical' },
  { key: 'high_days', labelKey: 'shell.triageSeverityHigh' },
  { key: 'medium_days', labelKey: 'shell.triageSeverityMedium' },
  { key: 'low_days', labelKey: 'shell.triageSeverityLow' },
]

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export default function VulnSlaDialog({ open, onClose, onSaved }: Props) {
  const { t } = useTranslation('chat')
  const [values, setValues] = useState<Record<FieldKey, string>>({
    critical_days: '3', high_days: '7', medium_days: '30', low_days: '90',
  })
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setLoadError('')
    setSaveError('')
    getWebApiVulnSLA()
      .then((sla: WebApiVulnSLA) => {
        if (cancelled) return
        setValues({
          critical_days: String(sla.critical_days),
          high_days: String(sla.high_days),
          medium_days: String(sla.medium_days),
          low_days: String(sla.low_days),
        })
      })
      .catch((err) => {
        if (cancelled) return
        setLoadError(errorText(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [open])

  const parse = (): Partial<Record<FieldKey, number>> | null => {
    const result: Partial<Record<FieldKey, number>> = {}
    for (const field of FIELDS) {
      const raw = values[field.key].trim()
      const days = Number(raw)
      if (!/^\d+$/.test(raw) || !Number.isInteger(days) || days < 1 || days > 365) return null
      result[field.key] = days
    }
    return result
  }

  const save = async () => {
    const body = parse()
    if (!body) {
      setSaveError(t('shell.triageSlaInvalid'))
      return
    }
    setSaving(true)
    setSaveError('')
    try {
      await saveWebApiVulnSLA(body)
      onSaved()
      onClose()
    } catch (err) {
      setSaveError(errorText(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
      <DialogContent closeLabel={t('webapi.close')} aria-describedby={undefined} className="flex w-[calc(100vw-2rem)] max-w-[560px] flex-col gap-0 overflow-hidden p-0">
        <DialogHeaderRow title={t('shell.triageSlaTitle')} description={t('shell.triageSlaHint')} />

        <div className="min-h-0 flex-1 overflow-y-auto p-5">
          {loading ? (
            <div className="flex h-24 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('shell.loading')}
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-4">
              {FIELDS.map((field) => (
                <label key={field.key} className="block space-y-1.5">
                  <span className="block text-[12px] text-muted-foreground">{t(field.labelKey)}</span>
                  <span className="relative block">
                    <Input
                      type="number"
                      min={1}
                      max={365}
                      step={1}
                      value={values[field.key]}
                      onChange={(event) => setValues((current) => ({ ...current, [field.key]: event.target.value }))}
                      disabled={saving}
                      className="h-9 pr-9"
                    />
                    <span className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-[12px] text-muted-foreground">
                      {t('shell.triageSlaDays')}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          )}

          {(loadError || saveError) && (
            <p role="alert" className="mt-3 text-xs text-destructive">{loadError || saveError}</p>
          )}
        </div>

        <DialogFooterRow>
          <Button type="button" variant="outline" onClick={onClose} disabled={saving}>{t('webapi.close')}</Button>
          <Button type="button" onClick={() => void save()} disabled={saving || loading}>
            {saving && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            {t('shell.triageSave')}
          </Button>
        </DialogFooterRow>
      </DialogContent>
    </Dialog>
  )
}
