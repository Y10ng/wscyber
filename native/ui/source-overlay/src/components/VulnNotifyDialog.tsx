import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, Send } from 'lucide-react'
import { Button, Dialog, DialogContent, Spinner } from '@cyber/ui'
import {
  getWebApiVulnNotifyPreview, notifyWebApiVulnCard, VulnNotifyAPIError, webApiEvidenceImageURL,
  type WebApiNotification, type WebApiVulnCard, type WebApiVulnNotifyPreview,
} from '../api'
import { NotificationStatusBadge } from './ui-badges'
import { DialogFooterRow, DialogHeaderRow } from './ui-dialog'

interface VulnNotifyDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  vulnKey: string
  card: WebApiVulnCard
  /** 推送成功或失败后都调用（刷新卡片的 notify 段）。 */
  onPushed: () => void
  /** 未配置时“前往集成”：关闭对话框并跳到集成页。 */
  onOpenIntegrations: () => void
  /** 成功入队后的卡片 toast（VulnCardPage 的 notify）。 */
  onToast?: (message: string) => void
}

type NoticeKind = 'unconfigured' | 'false_positive' | 'no_supported' | 'in_flight'

const IN_FLIGHT_STATUSES = new Set<WebApiNotification['status']>(['pending_confirm', 'queued', 'sending'])

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

export default function VulnNotifyDialog({
  open, onOpenChange, vulnKey, card, onPushed, onOpenIntegrations, onToast,
}: VulnNotifyDialogProps) {
  const { t } = useTranslation('chat')
  const [loading, setLoading] = useState(false)
  const [preview, setPreview] = useState<WebApiVulnNotifyPreview | null>(null)
  const [previewError, setPreviewError] = useState('')
  const [previewCode, setPreviewCode] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setLoading(true)
    setPreview(null)
    setPreviewError('')
    setPreviewCode(null)
    setSubmitError('')
    getWebApiVulnNotifyPreview(vulnKey)
      .then((next) => {
        if (cancelled) return
        setPreview(next)
      })
      .catch((err) => {
        if (cancelled) return
        setPreviewError(errorText(err))
        setPreviewCode(err instanceof VulnNotifyAPIError && err.httpStatus === 409 ? err.body?.code || null : null)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [open, vulnKey])

  const inFlight = card.notify.items.find((item) => IN_FLIGHT_STATUSES.has(item.status)) || null
  const falsePositive = card.triage?.disposition === 'false_positive' || card.header.disposition === 'false_positive'

  // 提示条按后端语义顺序只显示第一条命中的。
  let notice: NoticeKind | null = null
  if (!card.notify.configured) notice = 'unconfigured'
  else if (falsePositive) notice = 'false_positive'
  else if (previewCode === 'no_supported') notice = 'no_supported'
  else if (inFlight) notice = 'in_flight'

  const close = () => onOpenChange(false)

  const push = async () => {
    if (submitting || notice !== null || !preview) return
    setSubmitting(true)
    setSubmitError('')
    try {
      await notifyWebApiVulnCard(vulnKey)
      close()
      onToast?.(t('vulnCard.notifyQueued'))
      onPushed()
    } catch (err) {
      let message = errorText(err)
      if (err instanceof VulnNotifyAPIError) {
        switch (err.body?.code) {
          case 'not_configured':
            message = t('vulnCard.notifyErrNotConfigured')
            break
          case 'false_positive':
            message = t('vulnCard.notifyFalsePositive')
            break
          case 'no_supported':
            message = t('vulnCard.notifyErrNoSupported')
            break
          case 'in_flight':
            message = t('vulnCard.notifyInFlight')
            break
          default:
            message = err.body?.error || err.message
        }
      }
      setSubmitError(message)
      // 失败也刷新 notify 段（可能刚进入 in_flight）。
      onPushed()
    } finally {
      setSubmitting(false)
    }
  }

  const noticeBar = () => {
    if (notice === null) return null
    const base = 'flex items-start gap-2 rounded-md border px-3 py-2 text-[12px]'
    if (notice === 'unconfigured') {
      return (
        <div className={`${base} border-warning/40 bg-warning/10 text-warning`}>
          <span className="min-w-0 flex-1">{t('vulnCard.notifyConfigUnconfigured')}</span>
          <button
            type="button"
            className="shrink-0 font-medium underline underline-offset-4 hover:no-underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            onClick={() => { onOpenIntegrations(); close() }}
          >
            {t('vulnCard.notifyGotoIntegrations')}
          </button>
        </div>
      )
    }
    return (
      <div className={`${base} border-warning/40 bg-warning/10 text-warning`}>
        {notice === 'false_positive' && <span>{t('vulnCard.notifyFalsePositive')}</span>}
        {notice === 'no_supported' && <span>{t('vulnCard.notifyNoSupported')}</span>}
        {notice === 'in_flight' && inFlight && (
          <span className="inline-flex flex-wrap items-center gap-1">
            <span>{t('vulnCard.notifyInFlightPrefix')}</span>
            <NotificationStatusBadge status={inFlight.status} />
            <span>{t('vulnCard.notifyInFlightSuffix')}</span>
          </span>
        )}
      </div>
    )
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) close() }}>
      <DialogContent
        closeLabel={t('webapi.close')}
        aria-describedby={undefined}
        className="flex max-h-[85vh] w-[calc(100vw-2rem)] max-w-[640px] flex-col gap-0 overflow-hidden p-0"
      >
        <DialogHeaderRow
          icon={<Send className="h-4 w-4" />}
          title={t('vulnCard.notifyDialogTitle')}
          description={t('vulnCard.notifyDialogDescription')}
        />

        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-5">
          {noticeBar()}

          {loading ? (
            <div className="flex h-32 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('vulnCard.notifyPreviewing')}
            </div>
          ) : preview ? (
            <>
              <div className="space-y-1.5">
                <h3 className="text-[12px] font-semibold text-foreground">{t('vulnCard.notifyMessageContent')}</h3>
                <pre className="max-h-[320px] overflow-auto whitespace-pre-wrap break-words rounded-md border border-border bg-muted/40 p-3 font-mono text-[12px] leading-5">
                  {preview.markdown}
                </pre>
              </div>
              {preview.image && (
                <div className="space-y-1.5">
                  <h3 className="text-[12px] font-semibold text-foreground">{t('vulnCard.notifyImageTitle')}</h3>
                  <img
                    src={webApiEvidenceImageURL(preview.image.task_id, preview.image.evidence_id, {
                      mask: true,
                      marks: preview.image.marks,
                    })}
                    alt={t('vulnCard.notifyImageTitle')}
                    className="max-h-[240px] w-auto max-w-full rounded border border-border object-contain"
                  />
                  {preview.image_note ? (
                    <p className="text-[12px] text-muted-foreground">{preview.image_note}</p>
                  ) : null}
                </div>
              )}
            </>
          ) : previewError && previewCode !== 'no_supported' ? (
            <p role="alert" className="rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-[12px] text-warning">
              {t('vulnCard.notifyPreviewFailed', { error: previewError })}
            </p>
          ) : null}

          {submitError && (
            <p role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-[12px] text-destructive">
              {submitError}
            </p>
          )}
        </div>

        <DialogFooterRow>
          <Button type="button" variant="outline" onClick={close} disabled={submitting}>
            {t('vulnCard.notifyCancel')}
          </Button>
          <Button
            type="button"
            onClick={() => void push()}
            disabled={submitting || loading || !preview || notice !== null}
          >
            {submitting
              ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
              : <Send className="mr-1.5 h-3.5 w-3.5" />}
            {submitting ? t('vulnCard.notifySubmitting') : t('vulnCard.notifyPush')}
          </Button>
        </DialogFooterRow>
      </DialogContent>
    </Dialog>
  )
}
