import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Boxes, Check, Minus } from 'lucide-react'
import { newID } from '@cyber/aop'
import { Button, Callout, Dialog, DialogContent, DialogTitle, Field, Spinner, Textarea } from '@cyber/ui'
import { AssetLaunchError, launchWebApiAssetTask, type WebApiAsset, type WebApiLaunchProgress } from '../api'
import { normalizeWebApiPolicy, WebApiPolicyFields } from './WebApiPolicyShared'

interface Props {
  open: boolean
  asset: WebApiAsset
  onClose: () => void
  onLaunched: (sessionID: string) => void
}

// 从资产发起测试：目标与策略预填资产默认值。失败不关 Dialog，显示后端原文与
// 三步进度；重试复用第一次提交的 taskId（关闭重开才生成新 ID）。
export default function WebApiAssetLaunchDialog({ open, asset, onClose, onLaunched }: Props) {
  const { t } = useTranslation('chat')
  const [objective, setObjective] = useState('')
  const [scope, setScope] = useState('origins')
  const [approval, setApproval] = useState('high_risk')
  const [taskId, setTaskId] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [progress, setProgress] = useState<WebApiLaunchProgress | null>(null)

  useEffect(() => {
    if (!open) return
    const policy = normalizeWebApiPolicy({ scope: asset.scope, approval: asset.approval, is_default: !asset.scope && !asset.approval })
    setObjective(asset.objective || '')
    setScope(policy.scope)
    setApproval(policy.approval)
    setTaskId('')
    setError('')
    setProgress(null)
    setSaving(false)
    // 只在打开或切换资产时重置；资产其余字段变化不应覆盖用户编辑。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, asset.id])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (saving) return
    const id = taskId || newID()
    if (!taskId) setTaskId(id)
    setSaving(true)
    setError('')
    try {
      const result = await launchWebApiAssetTask(asset.id, id, { objective, scope, approval })
      onLaunched(result.session_id)
    } catch (err) {
      if (err instanceof AssetLaunchError) {
        setError(err.message)
        setProgress(err.progress)
      } else {
        setError(err instanceof Error ? err.message : String(err))
        setProgress(null)
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !saving) onClose() }}>
      <DialogContent
        closeLabel={t('webapi.close')}
        aria-describedby={undefined}
        onInteractOutside={(event) => event.preventDefault()}
        className="flex w-[calc(100vw-2rem)] max-w-lg flex-col gap-0 overflow-hidden p-0"
      >
        <div className="flex shrink-0 items-center gap-2.5 border-b border-border/70 px-4 py-3 pr-12">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-primary/25 bg-primary/10 text-primary">
            <Boxes className="h-4 w-4" />
          </div>
          <DialogTitle className="truncate text-sm font-semibold text-foreground">{t('webapi.assetLaunchTitle')}</DialogTitle>
        </div>

        <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
            <Field label={t('webapi.assetLaunchObjective')}>
              <Textarea value={objective} onChange={(event) => setObjective(event.target.value)} placeholder={t('webapi.objectivePlaceholder')} disabled={saving} />
            </Field>
            <WebApiPolicyFields
              scope={scope}
              approval={approval}
              onScopeChange={setScope}
              onApprovalChange={setApproval}
              disabled={saving}
              compact
            />
            {error && (
              <Callout tone="destructive">
                <div className="space-y-1.5">
                  <p className="whitespace-pre-wrap">{error}</p>
                  <div className="space-y-0.5 font-normal">
                    <ProgressLine label={t('webapi.assetLaunchTaskCreated')} done={progress?.task_created === true} />
                    <ProgressLine label={t('webapi.assetLaunchSessionOpened')} done={progress?.session_opened === true} />
                    <ProgressLine label={t('webapi.assetLaunchTurnStarted')} done={progress?.turn_started === true} />
                  </div>
                </div>
              </Callout>
            )}
          </div>

          <div className="flex shrink-0 flex-row justify-end gap-2 border-t border-border/60 px-4 py-3">
            <Button type="button" variant="outline" onClick={onClose} disabled={saving}>{t('webapi.close')}</Button>
            <Button type="submit" disabled={saving}>
              {saving && <Spinner className="h-4 w-4" />}
              {error ? t('webapi.assetLaunchRetry') : t('webapi.assetLaunchStart')}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ProgressLine({ label, done }: { label: string; done: boolean }) {
  return (
    <div className="flex items-center gap-1.5 text-[12px]">
      {done ? <Check className="h-3.5 w-3.5 shrink-0" aria-hidden="true" /> : <Minus className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />}
      <span>{label}</span>
    </div>
  )
}
