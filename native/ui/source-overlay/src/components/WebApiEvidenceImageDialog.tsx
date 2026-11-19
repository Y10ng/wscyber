import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Download, ExternalLink, Loader2 } from 'lucide-react'
import { Button, Dialog, DialogContent, DialogTitle, Switch } from '@cyber/ui'
import { cn } from '@cyber/theme'

interface Props {
  open: boolean
  onClose: () => void
  url: string
  filename: string
}

// 把后端渲染的证据 PNG 直接作为 <img> 展示：同源 Cookie 鉴权，不额外取数。
// 遮盖开关只替换 URL 的 mask 参数（服务端据此重画）；失败不自动重试。
function withMask(url: string, mask: boolean): string {
  const value = mask ? '1' : '0'
  if (/[?&]mask=/.test(url)) return url.replace(/([?&])mask=[^&]*/, `$1mask=${value}`)
  return `${url}${url.includes('?') ? '&' : '?'}mask=${value}`
}

export default function WebApiEvidenceImageDialog({ open, onClose, url, filename }: Props) {
  const { t } = useTranslation('chat')
  const [masked, setMasked] = useState(true)
  const [status, setStatus] = useState<'loading' | 'loaded' | 'error'>('loading')
  const currentUrl = withMask(url, masked)

  useEffect(() => {
    if (!open) return
    setMasked(true)
    setStatus('loading')
  }, [open, url])

  useEffect(() => {
    if (!open) return
    setStatus('loading')
  }, [open, currentUrl])

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
      <DialogContent
        closeLabel={t('webapi.close')}
        aria-describedby={undefined}
        className="flex w-[calc(100vw-2rem)] max-w-[1260px] flex-col gap-0 overflow-hidden p-0"
      >
        <div className="flex shrink-0 items-center gap-2.5 border-b border-border/70 px-4 py-3 pr-12">
          <DialogTitle className="truncate text-sm font-semibold text-foreground">{t('webapi.evidenceImageTitle')}</DialogTitle>
        </div>
        <div className="flex flex-wrap items-center gap-2 border-b border-border/60 px-4 py-2">
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <Switch checked={masked} onCheckedChange={setMasked} className="scale-75" />
            <span>{t('webapi.evidenceImageMask')}</span>
          </label>
          <span className="ml-auto" />
          <Button asChild size="xs" className="gap-1">
            <a href={currentUrl} download={filename}>
              <Download className="h-3.5 w-3.5" />
              {t('webapi.evidenceImageDownload')}
            </a>
          </Button>
          <Button asChild variant="ghost" size="xs" className="gap-1 text-muted-foreground">
            <a href={currentUrl} target="_blank" rel="noopener">
              <ExternalLink className="h-3.5 w-3.5" />
              {t('webapi.evidenceImageOpenNewTab')}
            </a>
          </Button>
        </div>
        <div className="mx-4 mb-4 mt-3 max-h-[75vh] min-h-40 overflow-auto rounded border border-border/60 bg-white">
          <div className="relative">
            {status === 'error' ? (
              <div className="flex min-h-40 items-center justify-center p-6 text-sm text-destructive">
                {t('webapi.evidenceImageFailed')}
              </div>
            ) : (
              <>
                <img
                  src={currentUrl}
                  alt=""
                  className={cn('block max-w-none', status === 'loading' && 'invisible')}
                  onLoad={() => setStatus('loaded')}
                  onError={() => setStatus('error')}
                />
                {status === 'loading' && (
                  <div className="absolute inset-0 flex items-center justify-center">
                    <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                  </div>
                )}
              </>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
