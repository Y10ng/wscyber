import { useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Globe, Plus, Trash2 } from 'lucide-react'
import { newID } from '@cyber/aop'
import { Button, Callout, Checkbox, Dialog, DialogContent, DialogTitle, Field, Input, Spinner, Textarea } from '@cyber/ui'
import type { WebApiTask } from '../api'

export interface WebApiTaskInput {
  taskID: string
  knownInfo: string
  objective: string
  origins: string[]
}

interface Props {
  open: boolean
  onClose: () => void
  onSubmit: (input: WebApiTaskInput) => Promise<{ task: WebApiTask; sessionID: string }>
}

// 两主输入 + 明确授权表单。正常关闭仅隐藏并保留内存草稿/未决 taskID，成功后 reset；
// 提交中同步防重并冻结本次快照，用户改字段不会静默替换启动内容。
export default function WebApiTaskDialog({ open, onClose, onSubmit }: Props) {
  const { t } = useTranslation('chat')
  const [knownInfo, setKnownInfo] = useState('')
  const [objective, setObjective] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [origins, setOrigins] = useState<string[]>([''])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  // 客户端只生成一次保存 ID：首次提交生成，重试复用；正常关闭不丢，成功后 reset。
  const taskIDRef = useRef<string>('')
  const submittingRef = useRef(false)
  // 无 Trigger 的受控 Dialog：沿 ConfigPanel 已验模式还原焦点。
  const returnFocus = useRef<HTMLElement | null>(null)
  const contentElement = useRef<HTMLElement | null>(null)

  const patchOrigin = (index: number, value: string) => {
    setOrigins((current) => current.map((item, i) => (i === index ? value : item)))
  }
  const addOrigin = () => setOrigins((current) => [...current, ''])
  const removeOrigin = (index: number) => setOrigins((current) => (current.length <= 1 ? current : current.filter((_, i) => i !== index)))

  const reset = () => {
    setKnownInfo('')
    setObjective('')
    setConfirmed(false)
    setOrigins([''])
    setError('')
    taskIDRef.current = ''
  }

  const handleClose = () => {
    if (saving) return
    onClose()
  }

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    // 同步防重：ref 在首个 await 前占用，React state(saving) 挡不住同一 tick 的重复提交。
    if (saving || submittingRef.current) return

    // 冻结本次提交快照：只 Trim 判空，传原文；提交中编辑不改变本次启动内容。
    const snapshot = { knownInfo, objective }
    const normalizedOrigins = origins.map((origin) => origin.trim()).filter(Boolean)
    if (!snapshot.knownInfo.trim()) { setError(t('webapi.requiredKnownInfo')); return }
    if (!snapshot.objective.trim()) { setError(t('webapi.requiredObjective')); return }
    if (!confirmed || normalizedOrigins.length === 0) { setError(t('webapi.requiredAuthorization')); return }

    if (!taskIDRef.current) taskIDRef.current = newID()
    submittingRef.current = true
    setSaving(true)
    setError('')
    try {
      await onSubmit({ taskID: taskIDRef.current, knownInfo: snapshot.knownInfo, objective: snapshot.objective, origins: normalizedOrigins })
      reset()
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      submittingRef.current = false
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next) handleClose() }}>
      <DialogContent
        closeLabel={t('webapi.close')}
        aria-describedby={undefined}
        onInteractOutside={(event) => event.preventDefault()}
        onOpenAutoFocus={(event) => {
          returnFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
          contentElement.current = event.target instanceof HTMLElement ? event.target : null
        }}
        onCloseAutoFocus={(event) => {
          const active = document.activeElement
          if (!event.defaultPrevented && returnFocus.current?.isConnected
            && (active === document.body || contentElement.current?.contains(active))) {
            event.preventDefault()
            returnFocus.current.focus()
          }
        }}
        className="flex h-[min(90dvh,40rem)] w-[calc(100vw-2rem)] max-w-xl flex-col gap-0 overflow-hidden p-0"
      >
        <div className="flex shrink-0 items-center gap-2.5 border-b border-border/70 px-4 py-3 pr-12">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-primary/25 bg-primary/10 text-primary">
            <Globe className="h-4 w-4" />
          </div>
          <DialogTitle className="truncate text-sm font-semibold text-foreground">{t('webapi.newTaskTitle')}</DialogTitle>
        </div>

        <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
            <Field label={t('webapi.knownInfo')} hint={t('webapi.knownInfoHint')}>
              <Textarea value={knownInfo} onChange={(e) => setKnownInfo(e.target.value)} placeholder={t('webapi.knownInfoPlaceholder')} disabled={saving} />
            </Field>
            <Field label={t('webapi.objective')} hint={t('webapi.objectiveHint')}>
              <Textarea value={objective} onChange={(e) => setObjective(e.target.value)} placeholder={t('webapi.objectivePlaceholder')} disabled={saving} />
            </Field>

            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Checkbox checked={confirmed} onCheckedChange={setConfirmed} disabled={saving} aria-label={t('webapi.authorizationConfirmed')} />
                <button type="button" onClick={() => setConfirmed((value) => !value)} disabled={saving} className="text-left text-[13px] font-medium text-foreground/75 hover:text-foreground disabled:opacity-50">
                  {t('webapi.authorizationConfirmed')}
                </button>
              </div>
              <div className="space-y-1.5 rounded-lg border border-border/70 bg-muted/15 p-2.5">
                <span className="block text-[13px] font-medium text-foreground/75">{t('webapi.authorization')}</span>
                <span className="block text-xs text-muted-foreground">{t('webapi.authorizationHint')}</span>
                {origins.map((origin, index) => (
                  <div key={index} className="flex items-center gap-1.5">
                    <Input value={origin} onChange={(e) => patchOrigin(index, e.target.value)} placeholder={t('webapi.originPlaceholder')} disabled={saving} className="h-9 flex-1" />
                    <Button type="button" variant="ghost" size="icon-xs" onClick={() => removeOrigin(index)} disabled={origins.length <= 1 || saving} aria-label={t('webapi.removeOrigin')} className="shrink-0 text-muted-foreground hover:text-destructive">
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}
                <Button type="button" variant="ghost" size="xs" onClick={addOrigin} disabled={saving} className="gap-1 text-muted-foreground">
                  <Plus className="h-3.5 w-3.5" />
                  {t('webapi.addOrigin')}
                </Button>
              </div>
            </div>

            {error && <Callout tone="destructive">{error}</Callout>}
          </div>

          <div className="flex shrink-0 flex-row justify-end gap-2 border-t border-border/60 px-4 py-3">
            <Button type="button" variant="outline" onClick={handleClose} disabled={saving}>{t('webapi.close')}</Button>
            <Button type="submit" disabled={saving}>{saving && <Spinner className="h-4 w-4" />}{t('webapi.start')}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
