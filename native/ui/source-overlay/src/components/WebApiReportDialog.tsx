import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FileText, Loader2 } from 'lucide-react'
import { cn } from '@cyber/theme'
import { Button, Callout, Checkbox, Dialog, DialogContent, DialogTitle, Spinner } from '@cyber/ui'
import {
  listWebApiFindings,
  listWebApiVerifications,
  type WebApiFindingSummary,
  type WebApiReportItem,
  type WebApiReportSummary,
  type WebApiTask,
  type WebApiVerificationSummary,
} from '../api'

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
}

interface CandidateState {
  revision_id: string
  verifications: WebApiVerificationSummary[]
  verificationLoading: boolean
  verificationError: string
  attached: string[]
}

// 新建报告：勾选任务候选（当前修订），勾选后列出该修订上的独立验证，可默认勾选已有
// 判读的附加。提交只固定关系（finding/revision/verification），不复制候选或验证内容。
export default function WebApiReportDialog({ open, task, onClose, onSubmit }: Props) {
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
  const verificationSeq = useRef(1)
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
      verificationSeq.current++
    }
  }, [open, loadFindings])

  const loadVerifications = useCallback(async (finding: WebApiFindingSummary) => {
    const seq = ++verificationSeq.current
    setCandidates((current) => ({ ...current, [finding.finding_id]: { ...current[finding.finding_id], verificationLoading: true, verificationError: '' } }))
    try {
      const result = await listWebApiVerifications(task.id, finding.finding_id, 50)
      if (seq !== verificationSeq.current) return
      // 只列该候选当前修订上的验证；已有判读的默认附加。
      const matched = result.items.filter((item) => item.source_revision_id === finding.revision_id)
      setCandidates((current) => ({
        ...current,
        [finding.finding_id]: {
          ...current[finding.finding_id],
          verifications: matched,
          verificationLoading: false,
          attached: matched.filter((item) => item.verdict).map((item) => item.verification_id),
        },
      }))
    } catch (err) {
      if (seq !== verificationSeq.current) return
      setCandidates((current) => ({
        ...current,
        [finding.finding_id]: { ...current[finding.finding_id], verificationLoading: false, verificationError: err instanceof Error ? err.message : String(err) },
      }))
    }
  }, [task.id])

  const toggleFinding = useCallback((finding: WebApiFindingSummary) => {
    setError('')
    setCandidates((current) => {
      const existing = current[finding.finding_id]
      if (existing) {
        const next = { ...current }
        delete next[finding.finding_id]
        return next
      }
      return { ...current, [finding.finding_id]: { revision_id: finding.revision_id, verifications: [], verificationLoading: false, verificationError: '', attached: [] } }
    })
    void loadVerifications(finding)
  }, [loadVerifications])

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

  const handleConfirm = useCallback(async () => {
    if (saving) return
    if (selectedCount === 0) { setError(t('webapi.reportSelectFindingsRequired')); return }
    if (selectedCount > 50) { setError(t('webapi.reportTooManyItems')); return }
    // items 按候选列表顺序固定；勾选后候选列表变化（加载更多）不影响既有条目顺序。
    const items: WebApiReportItem[] = findings
      .filter((finding) => candidates[finding.finding_id])
      .map((finding) => ({
        finding_id: finding.finding_id,
        revision_id: candidates[finding.finding_id].revision_id,
        verification_ids: candidates[finding.finding_id].attached,
      }))
    setSaving(true)
    setError('')
    try {
      await onSubmit({ taskID: task.id, items })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }, [saving, selectedCount, findings, candidates, onSubmit, task.id, t])

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
        className="z-[80] flex h-[min(90dvh,40rem)] w-[calc(100vw-2rem)] max-w-xl flex-col gap-0 overflow-hidden p-0"
      >
        <div className="flex shrink-0 items-center gap-2.5 border-b border-border/70 px-4 py-3 pr-12">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-primary/25 bg-primary/10 text-primary">
            <FileText className="h-4 w-4" />
          </div>
          <DialogTitle className="truncate text-sm font-semibold text-foreground">{t('webapi.newReportTitle')}</DialogTitle>
        </div>

        <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-4">
          {loadingFindings && findings.length === 0 ? (
            <div className="flex h-24 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : findingsError ? (
            <div className="px-3 py-4 text-center text-sm text-destructive">{findingsError}</div>
          ) : findings.length === 0 ? (
            <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.findingsEmpty')}</div>
          ) : (
            <div className="overflow-hidden rounded-lg border border-border/70">
              {findings.map((finding) => {
                const state = candidates[finding.finding_id]
                const checked = !!state
                return (
                  <div key={finding.finding_id} className="border-b border-border/50 last:border-b-0">
                    <label className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 transition-colors hover:bg-muted/40">
                      <Checkbox checked={checked} onCheckedChange={() => toggleFinding(finding)} disabled={saving} />
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
                                <Checkbox
                                  checked={state.attached.includes(verification.verification_id)}
                                  onCheckedChange={() => toggleVerification(finding.finding_id, verification.verification_id)}
                                  disabled={saving}
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

        <div className="flex shrink-0 flex-row justify-end gap-2 border-t border-border/60 px-4 py-3">
          <Button type="button" variant="outline" onClick={onClose} disabled={saving}>{t('webapi.close')}</Button>
          <Button type="button" onClick={handleConfirm} disabled={saving || selectedCount === 0}>
            {saving && <Spinner className="h-4 w-4" />}
            {t('webapi.reportConfirm')}（{selectedCount}）
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
