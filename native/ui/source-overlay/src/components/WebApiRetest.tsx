import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Loader2, RefreshCw } from 'lucide-react'
import type { TrafficFlow } from '@cyber/aop'
import { aopFlowToHttpView, type TrafficHttpView } from '@cyber/traffic'
import { cn } from '@cyber/theme'
import { Button, Callout, Dialog, DialogContent, DialogTitle, Spinner } from '@cyber/ui'
import {
  getWebApiEvidence,
  getWebApiRetest,
  listWebApiRetests,
  type WebApiEvidenceDetail,
  type WebApiFindingDetail,
  type WebApiRetestDetail,
  type WebApiRetestSummary,
  type WebApiTask,
} from '../api'
import type { RetestLaunchView } from '../hooks/useChatSession'
import { usePolling } from '../hooks/usePolling'
import { decodeFlow, formatTime, EvidenceReference } from './WebApiEvidenceReference'

export type StartRetest = (input: {
  taskID: string
  findingID: string
  origins: string[]
  source: WebApiFindingDetail
}) => Promise<{ retest: WebApiRetestSummary; sessionID: string }>

interface Props {
  task: WebApiTask
  finding: WebApiFindingDetail
  onStartRetest: StartRetest
  onViewExecution: (sessionID: string) => void
  launchView: RetestLaunchView | null
  onDismissLaunch: () => void
  onResetLaunch: () => void
  /** 发起成功后通知上层刷新任务导航计数。 */
  onRetestStarted?: () => void
}

// 结论文案与色：reproducible=仍可复现（红/destructive 系）、fixed_in_scope=本次范围内
// 已修复（绿/success 系）、inconclusive=无法判定（灰/muted）。状态色必须配文字。
function conclusionKey(conclusion: string | undefined): string {
  if (conclusion === 'reproducible') return 'webapi.retestReproducible'
  if (conclusion === 'fixed_in_scope') return 'webapi.retestFixedInScope'
  if (conclusion === 'inconclusive') return 'webapi.retestInconclusive'
  return 'webapi.retestPending'
}

function conclusionTone(conclusion: string | undefined): string {
  if (conclusion === 'reproducible') return 'text-red-600 dark:text-red-400'
  if (conclusion === 'fixed_in_scope') return 'text-emerald-600 dark:text-emerald-400'
  if (conclusion === 'inconclusive') return 'text-muted-foreground'
  return 'text-muted-foreground'
}

// 候选详情内的复测区：按需发起（固定当前修订）、历史列表（时间倒序）、三类模型判读
// 与本复测新 HTTP 原件。它只读原生执行入口（chat.selectSession），绝不因查看而 Run；
// 复测不确认修复、不覆盖原候选。
export default function WebApiRetestSection({ task, finding, onStartRetest, onViewExecution, launchView, onDismissLaunch, onResetLaunch, onRetestStarted }: Props) {
  const { t } = useTranslation('chat')
  const [items, setItems] = useState<WebApiRetestSummary[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  // 初始即“加载中”：挂载（含跨断点重挂载）到首帧之间 effect 尚未跑 loadList，
  // 若初值 false 会先闪“尚无复测”空态；首帧直接走既有加载样式，加载完成且为空才显示空态。
  const [loadingList, setLoadingList] = useState(true)
  const [listError, setListError] = useState('')

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<WebApiRetestDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState('')

  const [dialogOpen, setDialogOpen] = useState(false)
  const [launching, setLaunching] = useState(false)
  const [launchError, setLaunchError] = useState('')

  const listSeq = useRef(0)
  const detailSeq = useRef(0)
  const pollSeq = useRef(0)

  const loadList = useCallback(async (cursor?: string) => {
    const seq = ++listSeq.current
    setLoadingList(true)
    setListError('')
    try {
      const result = await listWebApiRetests(task.id, finding.finding_id, 25, cursor)
      if (seq !== listSeq.current) return
      setItems((current) => (cursor ? [...current, ...result.items] : result.items))
      setHasMore(result.has_more)
      setNextCursor(result.next_cursor)
    } catch (err) {
      if (seq !== listSeq.current) return
      setListError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === listSeq.current) setLoadingList(false)
    }
  }, [task.id, finding.finding_id])

  const loadDetail = useCallback(async (retestId: string) => {
    const seq = ++detailSeq.current
    setDetailLoading(true)
    setDetailError('')
    setDetail(null)
    try {
      const result = await getWebApiRetest(task.id, finding.finding_id, retestId)
      if (seq !== detailSeq.current) return
      setDetail(result)
    } catch (err) {
      if (seq !== detailSeq.current) return
      setDetail(null)
      setDetailError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === detailSeq.current) setDetailLoading(false)
    }
  }, [task.id, finding.finding_id])

  const select = useCallback((item: WebApiRetestSummary) => {
    setSelectedId(item.retest_id)
    void loadDetail(item.retest_id)
  }, [loadDetail])

  // 静默刷新：按 ID 更新已有项（保留已加载历史/选择），仅当当前选中的复测刚从
  // “无结论”过渡到“有结论”时才补拉详情。串行/隐藏暂停由 usePolling 与 pollSeq 负责。
  const refreshListQuiet = useCallback(async () => {
    const seq = ++pollSeq.current
    try {
      const result = await listWebApiRetests(task.id, finding.finding_id, 25)
      if (seq !== pollSeq.current) return
      const fresh = new Map(result.items.map((item) => [item.retest_id, item]))
      setItems((current) => {
        let changed = false
        const next = current.map((item) => {
          const update = fresh.get(item.retest_id)
          if (update && update.conclusion !== item.conclusion) { changed = true; return update }
          return item
        })
        return changed ? next : current
      })
      const selectedSummary = result.items.find((item) => item.retest_id === selectedId)
      if (selectedId && selectedSummary?.conclusion && detail && !detail.result) {
        void loadDetail(selectedId)
      }
    } catch { /* 轮询失败不覆盖已有错误或清空列表 */ }
  }, [task.id, finding.finding_id, selectedId, detail, loadDetail])

  // 只要还有未形成结论的复测就保持轮询；全部有结论后停止，不空转。
  const hasPending = items.some((item) => !item.conclusion)
  usePolling(refreshListQuiet, 10000, hasPending)

  // 切 finding/task 或卸载时使 list/detail/poll seq 失效，旧响应不再写状态。
  useEffect(() => {
    return () => {
      listSeq.current++
      detailSeq.current++
      pollSeq.current++
    }
  }, [finding.finding_id, task.id])

  useEffect(() => {
    void loadList()
  }, [loadList])

  // 发起成功后刷新列表：新复测行（尚无结论）会出现并触发轮询；有候选行未收起时
  // 顺带刷新其详情。这与验证区块的发起路径一致（发起即见新行，不等轮询）。
  const handleLaunch = useCallback(async () => {
    if (launching) return
    setLaunching(true)
    setLaunchError('')
    try {
      await onStartRetest({ taskID: task.id, findingID: finding.finding_id, origins: task.origins, source: finding })
      setDialogOpen(false)
      onRetestStarted?.()
      await loadList()
    } catch (err) {
      setLaunchError(err instanceof Error ? err.message : String(err))
    } finally {
      setLaunching(false)
    }
  }, [launching, onStartRetest, task.id, task.origins, finding, onRetestStarted, loadList])

  return (
    <section className="mb-4">
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-baseline gap-2">
          <h3 className="text-[12px] font-medium text-muted-foreground">{t('webapi.retest')}</h3>
        </div>
        <Button type="button" variant="soft" size="xs" onClick={() => { setLaunchError(''); setDialogOpen(true) }} disabled={launchView?.status === 'pending'} className="shrink-0 gap-1 text-[12px]">
          <RefreshCw className="h-3.5 w-3.5" />
          {t(launchView ? 'webapi.retestRetry' : 'webapi.retestStart')}
        </Button>
      </div>

      {launchView && !launchView.dismissed && (
        <div className="mb-1.5 flex items-start justify-between gap-2 rounded-md border border-border/60 px-3 py-2 text-xs">
          {launchView.status === 'pending' ? (
            <span className="flex min-w-0 items-center gap-2 text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin" />
              {t('webapi.retestLaunching')}
            </span>
          ) : (
            <span className="min-w-0 text-destructive">{t('webapi.retestLaunchFailed')}：{launchView.error}</span>
          )}
          <Button type="button" variant="ghost" size="xs" onClick={onDismissLaunch} className="shrink-0 text-muted-foreground">{t('webapi.close')}</Button>
        </div>
      )}

      <div className="rounded-md border border-border/60">
        {loadingList && items.length === 0 ? (
          <div className="flex h-24 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
        ) : listError ? (
          <div className="px-3 py-4 text-center text-sm text-destructive">{listError}</div>
        ) : items.length === 0 ? (
          <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.retestEmpty')}</div>
        ) : (
          <div className="divide-y divide-border/40">
            {items.map((item) => {
              const selected = selectedId === item.retest_id
              return (
                <div key={item.retest_id}>
                  <button
                    type="button"
                    onClick={() => select(item)}
                    aria-pressed={selected}
                    className={cn('flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40', selected && 'bg-primary/10')}
                  >
                    <span className={cn('shrink-0 text-[12px] font-medium', conclusionTone(item.conclusion))}>{t(conclusionKey(item.conclusion))}</span>
                    <span className="min-w-0 flex-1 truncate text-[12px] text-muted-foreground">{formatTime(item.created_at)}</span>
                    <span className="shrink-0 text-[12px] text-muted-foreground">{t('webapi.verdictModelLabel')}</span>
                  </button>
                  {selected && (
                    <div className="border-t border-border/50">
                      {detailLoading ? (
                        <div className="flex h-32 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
                      ) : detailError ? (
                        <div className="px-3 py-3 text-sm text-destructive">{t('webapi.retestDetailFailed')}：{detailError}</div>
                      ) : detail ? (
                        <RetestDetailBody key={detail.retest_id} detail={detail} task={task} onViewExecution={onViewExecution} />
                      ) : null}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        )}
        {hasMore && (
          <div className="border-t border-border/50 px-3 py-1.5 text-center">
            <Button type="button" variant="ghost" size="xs" onClick={() => void loadList(nextCursor ?? undefined)} disabled={loadingList} className="text-muted-foreground">
              {loadingList ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('webapi.loadMore')}
            </Button>
          </div>
        )}
      </div>

      <RetestLaunchDialog
        open={dialogOpen}
        origins={launchView?.origins ?? task.origins}
        finding={launchView?.source ?? finding}
        retrying={!!launchView}
        canStartFresh={!!launchView?.canStartFresh}
        launching={launching}
        error={launchError || (launchView?.status === 'error' ? launchView.error : '')}
        onClose={() => { if (!launching) setDialogOpen(false) }}
        onReset={onResetLaunch}
        onConfirm={handleLaunch}
      />
    </section>
  )
}

function RetestDetailBody({
  detail,
  task,
  onViewExecution,
}: {
  detail: WebApiRetestDetail
  task: WebApiTask
  onViewExecution: (sessionID: string) => void
}) {
  const { t } = useTranslation('chat')
  const result = detail.result

  const [expandedEvidenceId, setExpandedEvidenceId] = useState<string | null>(null)
  const [evidenceDetail, setEvidenceDetail] = useState<WebApiEvidenceDetail | null>(null)
  const [evidenceFlow, setEvidenceFlow] = useState<TrafficFlow | null>(null)
  const [evidenceLoading, setEvidenceLoading] = useState(false)
  const [evidenceError, setEvidenceError] = useState('')
  const evidenceSeq = useRef(0)
  const evidenceView = useMemo<TrafficHttpView | null>(() => (evidenceFlow ? aopFlowToHttpView(evidenceFlow) : null), [evidenceFlow])

  // 卸载/切换复测时使证据读取 seq 失效，旧响应不再写状态。
  useEffect(() => () => { evidenceSeq.current++ }, [])

  const loadEvidence = useCallback(async (evidenceId: string) => {
    const seq = ++evidenceSeq.current
    setEvidenceLoading(true)
    setEvidenceError('')
    setEvidenceDetail(null)
    setEvidenceFlow(null)
    try {
      const loaded = await getWebApiEvidence(task.id, evidenceId)
      if (seq !== evidenceSeq.current) return
      const decoded = decodeFlow(loaded.flow_protobuf_base64)
      if (seq !== evidenceSeq.current) return
      setEvidenceDetail(loaded)
      setEvidenceFlow(decoded)
    } catch (err) {
      if (seq !== evidenceSeq.current) return
      setEvidenceDetail(null)
      setEvidenceFlow(null)
      setEvidenceError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === evidenceSeq.current) setEvidenceLoading(false)
    }
  }, [task.id])

  const toggleEvidence = useCallback((evidenceId: string) => {
    if (expandedEvidenceId === evidenceId) {
      evidenceSeq.current++
      setExpandedEvidenceId(null)
      setEvidenceDetail(null)
      setEvidenceFlow(null)
      setEvidenceLoading(false)
      setEvidenceError('')
      return
    }
    setExpandedEvidenceId(evidenceId)
    void loadEvidence(evidenceId)
  }, [expandedEvidenceId, loadEvidence])

  const registerEvidenceRef = useCallback(() => {}, [])

  return (
    <div className="px-3 py-3">
      <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
        <span className="text-muted-foreground">
          {t('webapi.retestFixedSource')}：{t('webapi.revision')} {detail.source.revision} · {formatTime(detail.source.revision_created_at)}
        </span>
        <span className="text-muted-foreground">{t('webapi.retestSavedAt')}：{formatTime(detail.created_at)}</span>
        <Button type="button" variant="ghost" size="xs" onClick={() => onViewExecution(detail.session_id)} className="ml-auto gap-1 text-muted-foreground">
          {t('webapi.retestViewExecution')}
        </Button>
      </div>

      {!result ? (
        <div className="px-1 py-3 text-sm text-muted-foreground">{t('webapi.retestNoResult')}</div>
      ) : (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className={cn('text-[13px] font-medium', conclusionTone(result.payload.conclusion))}>{t(conclusionKey(result.payload.conclusion))}</span>
            <span className="text-[12px] text-muted-foreground">{t('webapi.verdictModelLabel')}</span>
          </div>
          {result.payload.method ? <PayloadRow label={t('webapi.retestMethod')} text={result.payload.method} /> : null}
          {result.payload.preconditions ? <PayloadRow label={t('webapi.retestPreconditions')} text={result.payload.preconditions} /> : null}
          {result.payload.summary ? <PayloadRow label={t('webapi.retestSummary')} text={result.payload.summary} /> : null}
          {result.payload.limitations ? <PayloadRow label={t('webapi.retestLimitations')} text={result.payload.limitations} /> : null}
          {result.evidence?.length ? (
            <div>
              <div className="mb-1 text-[12px] font-medium text-muted-foreground">{t('webapi.retestEvidence')}</div>
              <ol className="space-y-1.5">
                {result.evidence.map((item, index) => (
                  <EvidenceReference
                    key={item.evidence_id}
                    index={index}
                    item={item}
                    expanded={expandedEvidenceId === item.evidence_id}
                    loading={expandedEvidenceId === item.evidence_id && evidenceLoading}
                    error={expandedEvidenceId === item.evidence_id ? evidenceError : ''}
                    view={expandedEvidenceId === item.evidence_id ? evidenceView : null}
                    detail={expandedEvidenceId === item.evidence_id ? evidenceDetail : null}
                    onToggle={() => toggleEvidence(item.evidence_id)}
                    registerEvidenceRef={registerEvidenceRef}
                    origins={task.origins}
                  />
                ))}
              </ol>
            </div>
          ) : null}
        </div>
      )}
    </div>
  )
}

function PayloadRow({ label, text }: { label: string; text: string }) {
  return (
    <div>
      <div className="text-[12px] text-muted-foreground">{label}</div>
      <p className="whitespace-pre-wrap text-[14px] leading-[1.65] text-foreground">{text}</p>
    </div>
  )
}

function RetestLaunchDialog({
  open,
  origins,
  finding,
  retrying,
  canStartFresh,
  launching,
  error,
  onClose,
  onReset,
  onConfirm,
}: {
  open: boolean
  origins: string[]
  finding: WebApiFindingDetail
  retrying: boolean
  canStartFresh: boolean
  launching: boolean
  error: string
  onClose: () => void
  onReset: () => void
  onConfirm: () => void
}) {
  const { t } = useTranslation('chat')
  // 无 Trigger 的受控 Dialog：沿 WebApiTaskDialog 已验模式还原焦点。
  const returnFocus = useRef<HTMLElement | null>(null)
  const contentElement = useRef<HTMLElement | null>(null)
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !launching) onClose() }}>
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
        className="z-[80] flex max-h-[min(80dvh,32rem)] w-[calc(100vw-2rem)] max-w-lg flex-col gap-0 overflow-hidden p-0"
      >
        <div className="flex shrink-0 items-center gap-2.5 border-b border-border/70 px-4 py-3 pr-12">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-primary/25 bg-primary/10 text-primary">
            <RefreshCw className="h-4 w-4" />
          </div>
          <DialogTitle className="truncate text-sm font-semibold text-foreground">{t(retrying ? 'webapi.retestRetry' : 'webapi.retestStartTitle')}</DialogTitle>
        </div>

        <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
          <div className="space-y-1">
            <div className="text-[13px] font-medium text-foreground">{finding.number} · {finding.content.title || t('webapi.untitledFinding')}</div>
            <div className="text-xs text-muted-foreground">
              {t('webapi.retestPinnedRevision')}：{t('webapi.revision')} {finding.revision} · {formatTime(finding.revision_created_at)}
            </div>
          </div>

          <div className="rounded-lg border border-border/70 bg-muted/15 p-2.5">
            <div className="text-[13px] font-medium text-foreground">{t('webapi.retestAuthorization')}</div>
            <div className="mt-1 space-y-0.5">
              {origins.map((origin) => (
                <div key={origin} className="font-mono text-[12px] text-muted-foreground" title={origin}>{origin}</div>
              ))}
            </div>
            <div className="mt-1 text-xs text-muted-foreground">{t('webapi.retestLaunchHint')}</div>
          </div>

          {error && <Callout tone="destructive">{error}</Callout>}
        </div>

        <div className="flex shrink-0 flex-row justify-end gap-2 border-t border-border/60 px-4 py-3">
          {canStartFresh && <Button type="button" variant="outline" onClick={onReset} disabled={launching}>{t('webapi.retestReloadSource')}</Button>}
          <Button type="button" variant="outline" onClick={onClose} disabled={launching}>{t('webapi.close')}</Button>
          <Button type="button" onClick={onConfirm} disabled={launching}>{launching && <Spinner className="h-4 w-4" />}{t(retrying ? 'webapi.retestRetryConfirm' : 'webapi.retestConfirm')}</Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// 原生复测 session 的紧凑上下文栏：只读展示固定来源与结论，并提供“返回原候选”。
export function WebApiRetestBar({ retest, onReturnToFinding }: { retest: WebApiRetestDetail; onReturnToFinding: () => void }) {
  const { t } = useTranslation('chat')
  const result = retest.result
  return (
    <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/60 bg-background px-4 py-1.5">
      <div className="flex min-w-0 flex-1 items-center gap-2 text-xs">
        <RefreshCw className="h-3.5 w-3.5 shrink-0 text-primary" />
        <span className="min-w-0 truncate text-[13px] font-medium text-foreground" title={retest.source.content.title}>
          {t('webapi.retestTitle')} · {retest.source.number} {retest.source.content.title || t('webapi.untitledFinding')}
        </span>
        <span className="shrink-0 text-muted-foreground">{t('webapi.revision')} {retest.source.revision}</span>
        {result ? (
          <span className={cn('shrink-0 font-medium', conclusionTone(result.payload.conclusion))}>{t(conclusionKey(result.payload.conclusion))} · {t('webapi.verdictModelLabel')}</span>
        ) : (
          <span className="shrink-0 text-muted-foreground">{t('webapi.retestNoResult')}</span>
        )}
      </div>
      <Button type="button" variant="outline" size="xs" onClick={onReturnToFinding} className="shrink-0 gap-1 text-[12px]">
        <ArrowLeft className="h-3.5 w-3.5" />
        {t('webapi.retestReturnToFinding')}
      </Button>
    </div>
  )
}
