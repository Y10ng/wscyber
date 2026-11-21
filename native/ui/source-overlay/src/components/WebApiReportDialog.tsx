import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, FileText, Loader2 } from 'lucide-react'
import { cn } from '@cyber/theme'
import { Button, Callout, Dialog, DialogContent, Spinner } from '@cyber/ui'
import {
  listWebApiFindings,
  listWebApiVerifications,
  type WebApiFindingSummary,
  type WebApiReportItem,
  type WebApiReportSummary,
  type WebApiTask,
  type WebApiVerificationSummary,
} from '../api'
import { DialogFooterRow, DialogHeaderRow } from './ui-dialog'

export interface StartReportInput {
  taskID: string
  items: WebApiReportItem[]
}

export type StartReport = (input: StartReportInput) => Promise<{ report: WebApiReportSummary; sessionID: string }>

interface Props {
  open: boolean
  task: WebApiTask
  onClose: () => void
  onSubmit: StartReport
  /** 追加7：提交成功后父级回调（刷新列表并选中新报告）；Dialog 在此之前自行关闭。 */
  onStarted?: (report: WebApiReportSummary) => void | Promise<void>
}

interface CandidateState {
  revision_id: string
  verifications: WebApiVerificationSummary[]
  verificationLoading: boolean
  verificationError: string
  attached: string[]
}

// 行内唯一勾选控件：真实 checkbox 承载键盘空格与 label 整行点击，指示器纯展示。
// 不把带交互处理的角色控件再包进 label——那会形成两层 checkbox，一次点击切换两次。
function RowCheckbox({ checked, disabled, onChange, label }: {
  checked: boolean
  disabled?: boolean
  onChange: () => void
  label: string
}) {
  return (
    <>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={onChange}
        aria-label={label}
        className="peer sr-only"
      />
      <span
        aria-hidden
        className={cn(
          'inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-sm border border-primary transition-colors',
          'peer-focus-visible:outline-none peer-focus-visible:ring-2 peer-focus-visible:ring-ring peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-background',
          checked ? 'bg-primary text-primary-foreground' : 'bg-background',
          disabled && 'cursor-not-allowed opacity-50',
        )}
      >
        {checked && <Check className="h-3 w-3" />}
      </span>
    </>
  )
}

// 新建报告：勾选任务候选（当前修订），勾选后列出该修订上的独立验证，可默认勾选已有
// 判读的附加。提交只固定关系（finding/revision/verification），不复制候选或验证内容。
export default function WebApiReportDialog({ open, task, onClose, onSubmit, onStarted }: Props) {
  const { t } = useTranslation('chat')
  const [findings, setFindings] = useState<WebApiFindingSummary[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loadingFindings, setLoadingFindings] = useState(false)
  const [findingsError, setFindingsError] = useState('')
  const [candidates, setCandidates] = useState<Record<string, CandidateState>>({})
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const findingsSeq = useRef(0)
  // 按 finding 维度的请求序号：多个候选并行加载验证时互不抢占，早完成的不再被丢弃。
  const verificationSeq = useRef<Record<string, number>>({})
  const returnFocus = useRef<HTMLElement | null>(null)
  const contentElement = useRef<HTMLElement | null>(null)

  const loadFindings = useCallback(async (cursor?: string) => {
    const seq = ++findingsSeq.current
    setLoadingFindings(true)
    setFindingsError('')
    try {
      const result = await listWebApiFindings(task.id, 50, cursor)
      if (seq !== findingsSeq.current) return
      setFindings((current) => (cursor ? [...current, ...result.items] : result.items))
      setHasMore(result.has_more)
      setNextCursor(result.next_cursor)
    } catch (err) {
      if (seq !== findingsSeq.current) return
      setFindingsError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === findingsSeq.current) setLoadingFindings(false)
    }
  }, [task.id])

  // 每次打开重新读候选：报告固定的是当前修订，旧勾选不复用，避免把过期 revision 写进 items。
  useEffect(() => {
    if (!open) return
    setCandidates({})
    setError('')
    void loadFindings()
    return () => {
      findingsSeq.current++
      verificationSeq.current = {}
    }
  }, [open, loadFindings])

  const loadVerifications = useCallback(async (finding: WebApiFindingSummary) => {
    const seq = (verificationSeq.current[finding.finding_id] ?? 0) + 1
    verificationSeq.current[finding.finding_id] = seq
    setCandidates((current) => {
      const entry = current[finding.finding_id]
      if (!entry) return current
      return { ...current, [finding.finding_id]: { ...entry, verificationLoading: true, verificationError: '' } }
    })
    try {
      const result = await listWebApiVerifications(task.id, finding.finding_id, 50)
      if (seq !== verificationSeq.current[finding.finding_id]) return
      // 只列该候选当前修订上的验证；已有判读的默认附加。
      const matched = result.items.filter((item) => item.source_revision_id === finding.revision_id)
      setCandidates((current) => {
        const entry = current[finding.finding_id]
        if (!entry) return current
        return {
          ...current,
          [finding.finding_id]: {
            ...entry,
            verifications: matched,
            verificationLoading: false,
            attached: matched.filter((item) => item.verdict).map((item) => item.verification_id),
          },
        }
      })
    } catch (err) {
      if (seq !== verificationSeq.current[finding.finding_id]) return
      setCandidates((current) => {
        const entry = current[finding.finding_id]
        if (!entry) return current
        return { ...current, [finding.finding_id]: { ...entry, verificationLoading: false, verificationError: err instanceof Error ? err.message : String(err) } }
      })
    }
  }, [task.id])

  const toggleFinding = useCallback((finding: WebApiFindingSummary) => {
    setError('')
    if (candidates[finding.finding_id]) {
      setCandidates((current) => {
        if (!current[finding.finding_id]) return current
        const next = { ...current }
        delete next[finding.finding_id]
        return next
      })
      return
    }
    setCandidates((current) => {
      if (current[finding.finding_id]) return current
      return { ...current, [finding.finding_id]: { revision_id: finding.revision_id, verifications: [], verificationLoading: false, verificationError: '', attached: [] } }
    })
    // 只在勾选时加载验证：取消勾选不发请求，也不会为已删条目造出残缺状态。
    void loadVerifications(finding)
  }, [candidates, loadVerifications])

  const toggleVerification = useCallback((findingID: string, verificationID: string) => {
    setCandidates((current) => {
      const entry = current[findingID]
      if (!entry) return current
      const attached = entry.attached.includes(verificationID)
        ? entry.attached.filter((id) => id !== verificationID)
        : [...entry.attached, verificationID]
      return { ...current, [findingID]: { ...entry, attached } }
    })
  }, [])

  const selectedCount = Object.keys(candidates).length
  const allSelected = findings.length > 0 && findings.every((finding) => candidates[finding.finding_id])

  const handleToggleAll = useCallback(() => {
    setError('')
    if (findings.length === 0) return
    if (findings.every((finding) => candidates[finding.finding_id])) {
      setCandidates({})
      return
    }
    setCandidates((current) => {
      const next = { ...current }
      for (const finding of findings) {
        if (!next[finding.finding_id]) {
          next[finding.finding_id] = { revision_id: finding.revision_id, verifications: [], verificationLoading: false, verificationError: '', attached: [] }
        }
      }
      return next
    })
    for (const finding of findings) {
      if (!candidates[finding.finding_id]) void loadVerifications(finding)
    }
  }, [findings, candidates, loadVerifications])

  const handleConfirm = useCallback(async () => {
    if (saving) return
    if (selectedCount === 0) { setError(t('webapi.reportSelectFindingsRequired')); return }
    if (selectedCount > 50) { setError(t('webapi.reportTooManyItems')); return }
    const selected = findings.filter((finding) => candidates[finding.finding_id])
    // 防御：勾选条目必须有当前修订标识，缺失则不提交（后端会整体拒绝且前端状态不完整）。
    if (selected.some((finding) => !candidates[finding.finding_id].revision_id)) {
      setError(t('webapi.reportMissingRevision'))
      return
    }
    // items 按候选列表顺序固定；勾选后候选列表变化（加载更多）不影响既有条目顺序。
    const items: WebApiReportItem[] = selected.map((finding) => ({
      finding_id: finding.finding_id,
      revision_id: candidates[finding.finding_id].revision_id,
      verification_ids: candidates[finding.finding_id].attached,
    }))
    setSaving(true)
    setError('')
    try {
      const { report } = await onSubmit({ taskID: task.id, items })
      // 追加7：成功即关对话框并通知父级刷新列表、选中新报告；失败留在原处可重试。
      onClose()
      await onStarted?.(report)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [saving, selectedCount, findings, candidates, onSubmit, onStarted, onClose, task.id, t])

  const verdictKey = (verdict: string | undefined) => {
    if (verdict === 'supported') return 'webapi.verdict_supported'
    if (verdict === 'counterevidence') return 'webapi.verdict_counterevidence'
    if (verdict === 'inconclusive') return 'webapi.verdict_inconclusive'
    return 'webapi.verdictPending'
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !saving) onClose() }}>
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
        overlayClassName="z-[70]"
        className="z-[80] flex max-h-[min(90dvh,44rem)] w-[calc(100vw-2rem)] max-w-[640px] flex-col gap-0 overflow-hidden p-0"
      >
        <DialogHeaderRow icon={<FileText className="h-4 w-4" />} title={t('webapi.newReportTitle')} />

        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-5">
          {loadingFindings && findings.length === 0 ? (
            <div className="flex h-24 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : findingsError ? (
            <div className="px-3 py-4 text-center text-sm text-destructive">{findingsError}</div>
          ) : findings.length === 0 ? (
            <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.findingsEmpty')}</div>
          ) : (
            <div className="overflow-hidden rounded-lg border border-border/70">
              <div className="flex shrink-0 items-center justify-between border-b border-border/50 bg-muted/15 px-3 py-1.5">
                <span className="text-[11px] text-muted-foreground">{selectedCount}/{findings.length}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  onClick={handleToggleAll}
                  disabled={saving || findings.length === 0}
                  className="text-muted-foreground"
                >
                  {allSelected ? t('webapi.reportClearAll') : t('webapi.reportSelectAll')}
                </Button>
              </div>
              {findings.map((finding) => {
                const state = candidates[finding.finding_id]
                const checked = !!state
                return (
                  <div key={finding.finding_id} className="border-b border-border/50 last:border-b-0">
                    <label className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 transition-colors hover:bg-muted/40">
                      <RowCheckbox
                        checked={checked}
                        disabled={saving}
                        onChange={() => toggleFinding(finding)}
                        label={`${finding.number} ${finding.title}`}
                      />
                      <span className="font-mono text-[12px] text-muted-foreground">{finding.number}</span>
                      <span className="min-w-0 flex-1 truncate text-[13px] text-foreground/90" title={finding.title}>{finding.title}</span>
                      <span className="shrink-0 text-[11px] text-muted-foreground">{t('webapi.revision')} {finding.revision}</span>
                    </label>
                    {checked && (
                      <div className="border-t border-border/40 bg-muted/15 px-3 py-2 pl-9">
                        {state.verificationLoading ? (
                          <div className="flex items-center gap-2 text-xs text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" />{t('webapi.reportLoadingVerifications')}</div>
                        ) : state.verificationError ? (
                          <div className="text-xs text-destructive">{state.verificationError}</div>
                        ) : state.verifications.length === 0 ? (
                          <div className="text-xs text-muted-foreground">{t('webapi.reportNoVerifications')}</div>
                        ) : (
                          <div className="space-y-1.5">
                            {state.verifications.map((verification) => (
                              <label key={verification.verification_id} className="flex w-full cursor-pointer items-center gap-2">
                                <RowCheckbox
                                  checked={state.attached.includes(verification.verification_id)}
                                  disabled={saving}
                                  onChange={() => toggleVerification(finding.finding_id, verification.verification_id)}
                                  label={t(verdictKey(verification.verdict))}
                                />
                                <span className={cn('text-[12px]', !verification.verdict && 'text-muted-foreground')}>{t(verdictKey(verification.verdict))}</span>
                                {verification.verdict && <span className="text-[11px] text-muted-foreground">{t('webapi.verdictModelLabel')}</span>}
                              </label>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                )
              })}
            </div>
          )}
          {hasMore && (
            <div className="text-center">
              <Button type="button" variant="ghost" size="xs" onClick={() => void loadFindings(nextCursor ?? undefined)} disabled={loadingFindings} className="text-muted-foreground">
                {loadingFindings ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('webapi.loadMore')}
              </Button>
            </div>
          )}

          {error && <Callout tone="destructive">{error}</Callout>}
        </div>

        <DialogFooterRow>
          <Button type="button" variant="outline" onClick={onClose} disabled={saving}>{t('webapi.close')}</Button>
          <Button type="button" onClick={handleConfirm} disabled={saving || selectedCount === 0}>
            {saving && <Spinner className="h-4 w-4" />}
            {t('webapi.reportConfirm')}（{selectedCount}）
          </Button>
        </DialogFooterRow>
      </DialogContent>
    </Dialog>
  )
}
