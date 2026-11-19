import { useMemo, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Globe, Plus, Trash2 } from 'lucide-react'
import { newID } from '@cyber/aop'
import { Button, Callout, Checkbox, Chip, Dialog, DialogContent, DialogTitle, Field, Input, Spinner, Textarea } from '@cyber/ui'
import type { WebApiTask, WebApiTaskPolicy } from '../api'
import { DEFAULT_WEB_API_POLICY, WebApiPolicyFields } from './WebApiPolicyShared'

export interface WebApiTaskInput {
  taskID: string
  knownInfo: string
  objective: string
  origins: string[]
  // B1：提交顺序 保存任务 → 保存策略 → 启动；startWebApiTask 在保存任务之后、
  // 打开会话之前保存策略，失败即抛错（不启动）。
  policy: { scope: string; approval: string }
}

interface Props {
  open: boolean
  onClose: () => void
  onSubmit: (input: WebApiTaskInput) => Promise<{ task: WebApiTask; sessionID: string }>
}// 从两段描述文本确定性提取 http(s)://host[:port]，规范化为 origin（协议+host[:port]）。
// 只做识别建议：点击 chip 才加入，绝不自动写入 origins。
// 中文正文常把 URL 与标点连写（“地址 http://x:8080，全部为测试数据”），匹配须在
// 中文标点处截断，否则 new URL 解析失败导致漏识别；结尾 ASCII 标点也一并去掉。
export function extractOriginSuggestions(text: string): string[] {
  const found = new Set<string>()
  const re = /https?:\/\/[^\s<>"'`)\]}，。；：！？、（）【】《》「」]+/gi
  let match: RegExpExecArray | null
  while ((match = re.exec(text)) !== null) {
    try {
      const url = new URL(match[0].replace(/[.,;:!?'"，。；：！？、）】》」]+$/, ''))
      const port = url.port ? `:${url.port}` : ''
      found.add(`${url.protocol}//${url.hostname}${port}`)
    } catch { /* 非法 URL 不产生建议 */ }
  }
  return [...found]
}

// 两主输入 + 明确授权表单。正常关闭仅隐藏并保留内存草稿/未决 taskID，成功后 reset；
// 提交中同步防重并冻结本次快照，用户改字段不会静默替换启动内容。
// B1：提交顺序 保存任务 → 保存策略 → 启动；策略保存失败显示错误、不启动。
export default function WebApiTaskDialog({ open, onClose, onSubmit }: Props) {
  const { t } = useTranslation('chat')
  const [knownInfo, setKnownInfo] = useState('')
  const [objective, setObjective] = useState('')
  const [confirmed, setConfirmed] = useState(false)
  const [origins, setOrigins] = useState<string[]>([''])
  const [policy, setPolicy] = useState<WebApiTaskPolicy>({ ...DEFAULT_WEB_API_POLICY, is_default: false })
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
  const addOrigin = (value = '') => setOrigins((current) => [...current, value])
  const removeOrigin = (index: number) => setOrigins((current) => (current.length <= 1 ? current : current.filter((_, i) => i !== index)))

  // 识别建议：两段文本提取并集，排除已填 origins（trim 后比较）。
  const suggestions = useMemo(() => {
    const filled = new Set(origins.map((origin) => origin.trim().replace(/[.,;:!?'"]+$/, '')).filter(Boolean))
    return extractOriginSuggestions(`${knownInfo}\n${objective}`).filter((origin) => !filled.has(origin))
  }, [knownInfo, objective, origins])

  const reset = () => {
    setKnownInfo('')
    setObjective('')
    setConfirmed(false)
    setOrigins([''])
    setPolicy({ ...DEFAULT_WEB_API_POLICY, is_default: false })
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
    const snapshotPolicy = { scope: policy.scope, approval: policy.approval }
    const normalizedOrigins = origins.map((origin) => origin.trim()).filter(Boolean)
    if (!snapshot.knownInfo.trim()) { setError(t('webapi.requiredKnownInfo')); return }
    if (!snapshot.objective.trim()) { setError(t('webapi.requiredObjective')); return }
    if (!confirmed || normalizedOrigins.length === 0) { setError(t('webapi.requiredAuthorization')); return }

    if (!taskIDRef.current) taskIDRef.current = newID()
    submittingRef.current = true
    setSaving(true)
    setError('')
    try {
      // onSubmit（startWebApiTask）在保存任务后、开会话前保存策略并启动；
      // 策略保存失败会抛错显示在这里，任务已存、会话未开。
      await onSubmit({ taskID: taskIDRef.current, knownInfo: snapshot.knownInfo, objective: snapshot.objective, origins: normalizedOrigins, policy: snapshotPolicy })
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
                <Button type="button" variant="ghost" size="xs" onClick={() => addOrigin()} disabled={saving} className="gap-1 text-muted-foreground">
                  <Plus className="h-3.5 w-3.5" />
                  {t('webapi.addOrigin')}
                </Button>
                {suggestions.length > 0 && (
                  <div className="flex flex-wrap items-center gap-1.5 pt-1">
                    <span className="text-[11px] text-muted-foreground">{t('webapi.originSuggestions')}</span>
                    {suggestions.map((origin) => (
                      <Chip key={origin} type="button" onClick={() => addOrigin(origin)} aria-label={t('webapi.originAddSuggestion', { origin })}>
                        <Plus className="h-3 w-3" aria-hidden="true" />
                        <span className="font-mono">{origin}</span>
                      </Chip>
                    ))}
                  </div>
                )}
              </div>
            </div>

            {/* 测试策略：无卡片边框，与上方字段同一栅格。只由人设置，不进模型上下文。 */}
            <WebApiPolicyFields
              scope={policy.scope}
              approval={policy.approval}
              onScopeChange={(scope) => setPolicy((current) => ({ ...current, scope }))}
              onApprovalChange={(approval) => setPolicy((current) => ({ ...current, approval }))}
              disabled={saving}
            />

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
