import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject, type UIEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, RefreshCw } from 'lucide-react'
import type { TrafficFlow } from '@cyber/aop'
import { aopFlowToHttpView, type TrafficHttpView } from '@cyber/traffic'
import { cn } from '@cyber/theme'
import { Button } from '@cyber/ui'
import {
  getWebApiEvidence,
  getWebApiFinding,
  listWebApiFindings,
  type WebApiEvidenceDetail,
  type WebApiFindingDetail,
  type WebApiFindingSummary,
  type WebApiTask,
} from '../api'
import { decodeFlow, formatTime, EvidenceReference } from './WebApiEvidenceReference'
import WebApiVerificationSection, { type StartVerification } from './WebApiVerification'
import type { VerificationLaunchView } from '../hooks/useChatSession'
import { buildFindingDraft } from '../lib/finding-draft'
import './WebApiFindingsPanel.css'

interface Props {
  task: WebApiTask
  onStartVerification: StartVerification
  onViewExecution: (sessionID: string) => void
  verificationLaunch: Record<string, VerificationLaunchView>
  onDismissVerificationLaunch: (findingKey: string) => void
  onResetVerificationLaunch: (findingKey: string) => void
  focusFindingId: string | null
  onFocusConsumed: () => void
}

// 只读候选：紧凑列表 + 有限行宽详情；证据引用按需读取本任务真实请求/响应并就近展开。
export default function WebApiFindingsPanel({ task, onStartVerification, onViewExecution, verificationLaunch, onDismissVerificationLaunch, onResetVerificationLaunch, focusFindingId, onFocusConsumed }: Props) {
  const { t } = useTranslation('chat')
  const [items, setItems] = useState<WebApiFindingSummary[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [loadingList, setLoadingList] = useState(false)
  const [listError, setListError] = useState('')

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<WebApiFindingDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState('')
  const [showDetail, setShowDetail] = useState(false)
  const listScrollRef = useRef<HTMLDivElement>(null)
  const detailScrollRef = useRef<HTMLDivElement>(null)
  const listRegionRef = useRef<HTMLDivElement>(null)
  const detailRegionRef = useRef<HTMLDivElement>(null)
  const backRef = useRef<HTMLDivElement>(null)
  const rowRefs = useRef(new Map<string, HTMLButtonElement>())
  const scrollPositions = useRef({ list: 0, detail: 0 })
  const visibleScrollPanels = useRef<{ list: HTMLDivElement | null; detail: HTMLDivElement | null }>({ list: null, detail: null })
  const pendingViewFocus = useRef<'list' | 'detail' | null>(null)

  const [expandedEvidenceId, setExpandedEvidenceId] = useState<string | null>(null)
  const [evidenceDetail, setEvidenceDetail] = useState<WebApiEvidenceDetail | null>(null)
  const [evidenceFlow, setEvidenceFlow] = useState<TrafficFlow | null>(null)
  const [evidenceLoading, setEvidenceLoading] = useState(false)
  const [evidenceError, setEvidenceError] = useState('')

  const listSeq = useRef(0)
  const detailSeq = useRef(0)
  const evidenceSeq = useRef(0)
  // 复现步骤 [n] 定位证据行：按 evidence_id 映射证据行头部 <button>，重复点击只滚动/聚焦。
  const evidenceRefs = useRef(new Map<string, HTMLButtonElement>())
  // 待定位证据 id：展开 DOM 提交后由 useLayoutEffect 消费一次，避免对旧 DOM 定位偏差。
  const pendingLocateId = useRef<string | null>(null)
  // 用户手动选择后，旧迟到 focus 不再抢回；新 focus 到来时重置。
  const manualSelectionRef = useRef(false)

  const evidenceView = useMemo(() => (evidenceFlow ? aopFlowToHttpView(evidenceFlow) : null), [evidenceFlow])

  const loadList = useCallback(async (cursor?: string) => {
    const seq = ++listSeq.current
    setLoadingList(true)
    setListError('')
    try {
      const result = await listWebApiFindings(task.id, 25, cursor)
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
  }, [task.id])

  const loadDetail = useCallback(async (findingId: string) => {
    const seq = ++detailSeq.current
    setDetailLoading(true)
    setDetailError('')
    setDetail(null)
    resetEvidenceState()
    try {
      const result = await getWebApiFinding(task.id, findingId)
      if (seq !== detailSeq.current) return
      setDetail(result)
    } catch (err) {
      if (seq !== detailSeq.current) return
      setDetail(null)
      setDetailError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === detailSeq.current) setDetailLoading(false)
    }
  }, [task.id])

  function resetEvidenceState() {
    pendingLocateId.current = null
    evidenceSeq.current++
    setExpandedEvidenceId(null)
    setEvidenceDetail(null)
    setEvidenceFlow(null)
    setEvidenceLoading(false)
    setEvidenceError('')
  }

  const loadEvidence = useCallback(async (evidenceId: string) => {
    const seq = ++evidenceSeq.current
    setEvidenceLoading(true)
    setEvidenceError('')
    setEvidenceDetail(null)
    setEvidenceFlow(null)
    try {
      const result = await getWebApiEvidence(task.id, evidenceId)
      if (seq !== evidenceSeq.current) return
      const decoded = decodeFlow(result.flow_protobuf_base64)
      if (seq !== evidenceSeq.current) return
      setEvidenceDetail(result)
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

  // 换任务即清空旧候选/原件并使所有 seq 失效；卸载同样失效。迟到响应不再写状态。
  useEffect(() => {
    listSeq.current++
    detailSeq.current++
    evidenceSeq.current++
    setItems([])
    setHasMore(false)
    setNextCursor(null)
    setSelectedId(null)
    setDetail(null)
    setDetailError('')
    setShowDetail(false)
    scrollPositions.current = { list: 0, detail: 0 }
    pendingViewFocus.current = null
    resetEvidenceState()
    void loadList()
  }, [loadList])

  useEffect(() => () => { listSeq.current++; detailSeq.current++; evidenceSeq.current++ }, [])

  // 只记录真实滚动面板；隐藏导致的归零及原件内部滚动不能覆盖阅读位置。
  const rememberScroll = useCallback((panel: 'list' | 'detail', event: UIEvent<HTMLDivElement>) => {
    const element = event.currentTarget
    if (event.target === element && visibleScrollPanels.current[panel] === element && element.getClientRects().length) {
      scrollPositions.current[panel] = element.scrollTop
    }
  }, [])

  const restoreVisiblePanels = useCallback(() => {
    for (const panel of ['list', 'detail'] as const) {
      const element = panel === 'list' ? listScrollRef.current : detailScrollRef.current
      const visible = element && element.getClientRects().length ? element : null
      if (visible && visibleScrollPanels.current[panel] !== visible) {
        visible.scrollTop = scrollPositions.current[panel]
      }
      visibleScrollPanels.current[panel] = visible
    }
  }, [])

  // CSS container query 决定显隐；观察两个区域的尺寸变化以恢复跨断点位置。
  useLayoutEffect(() => {
    const observer = new ResizeObserver(restoreVisiblePanels)
    if (listRegionRef.current) observer.observe(listRegionRef.current)
    if (detailRegionRef.current) observer.observe(detailRegionRef.current)
    restoreVisiblePanels()
    return () => observer.disconnect()
  }, [restoreVisiblePanels])

  useLayoutEffect(() => {
    restoreVisiblePanels()
    const target = pendingViewFocus.current
    pendingViewFocus.current = null
    if (target === 'detail' && backRef.current?.getClientRects().length) {
      backRef.current.querySelector('button')?.focus({ preventScroll: true })
    } else if (target === 'list') {
      const row = selectedId ? rowRefs.current.get(selectedId) : null
      ;(row ?? listRegionRef.current)?.focus({ preventScroll: true })
    }
  }, [showDetail, detail, detailLoading, detailError, restoreVisiblePanels])

  const select = useCallback((item: WebApiFindingSummary) => {
    manualSelectionRef.current = true
    const sameRevision = detail?.task_id === task.id && detail.finding_id === item.finding_id && detail.revision_id === item.revision_id
    if (!sameRevision) {
      scrollPositions.current.detail = 0
      if (detailScrollRef.current) detailScrollRef.current.scrollTop = 0
    }
    pendingViewFocus.current = 'detail'
    setShowDetail(true)
    setSelectedId(item.finding_id)
    if (!sameRevision) void loadDetail(item.finding_id)
  }, [detail, loadDetail, task.id])

  const returnToList = useCallback(() => {
    pendingViewFocus.current = 'list'
    setShowDetail(false)
  }, [])

  // “返回原候选”定位：按 ID 直接选中/加载详情，不在首屏 25 条内也能到达；
  // 404/读取错误由 loadDetail 就地显示，不冒称首屏限制已获接受。
  useEffect(() => {
    if (focusFindingId) manualSelectionRef.current = false
  }, [focusFindingId])

  useEffect(() => {
    if (!focusFindingId) return
    if (manualSelectionRef.current) return
    scrollPositions.current.detail = 0
    if (detailScrollRef.current) detailScrollRef.current.scrollTop = 0
    pendingViewFocus.current = 'detail'
    setShowDetail(true)
    setSelectedId(focusFindingId)
    void loadDetail(focusFindingId)
    onFocusConsumed()
  }, [focusFindingId, loadDetail, onFocusConsumed])

  const refresh = useCallback(() => {
    listSeq.current++
    detailSeq.current++
    evidenceSeq.current++
    setItems([])
    setHasMore(false)
    setNextCursor(null)
    setSelectedId(null)
    setDetail(null)
    setDetailError('')
    setShowDetail(false)
    scrollPositions.current = { list: 0, detail: 0 }
    pendingViewFocus.current = null
    resetEvidenceState()
    void loadList()
  }, [loadList])

  const toggleEvidence = useCallback((evidenceId: string) => {
    if (expandedEvidenceId === evidenceId) {
      resetEvidenceState()
      return
    }
    setExpandedEvidenceId(evidenceId)
    void loadEvidence(evidenceId)
  }, [expandedEvidenceId, loadEvidence])

  const locateEvidence = useCallback((evidenceId: string) => {
    const btn = evidenceRefs.current.get(evidenceId)
    if (!btn) return
    btn.scrollIntoView({ block: 'center' })
    btn.focus({ preventScroll: true })
  }, [])

  // 展开提交后定位：useLayoutEffect 在 DOM 提交、绘制前消费一次 pendingLocateId，
  // 此时旧展开项已折叠、新项已展开，几何稳定，标题可准确落在详情滚动容器可视范围内。
  useLayoutEffect(() => {
    const evidenceId = pendingLocateId.current
    if (evidenceId == null) return
    pendingLocateId.current = null
    locateEvidence(evidenceId)
  }, [expandedEvidenceId, locateEvidence])

  // 复现步骤引用按钮：已展开只定位（不收起/不重复 GET），未展开先展开再定位。
  const openEvidence = useCallback((evidenceId: string) => {
    if (expandedEvidenceId !== evidenceId) {
      pendingLocateId.current = evidenceId
      setExpandedEvidenceId(evidenceId)
      void loadEvidence(evidenceId)
      return
    }
    locateEvidence(evidenceId)
  }, [expandedEvidenceId, loadEvidence, locateEvidence])

  const registerEvidenceRef = useCallback((evidenceId: string, el: HTMLButtonElement | null) => {
    if (el) evidenceRefs.current.set(evidenceId, el)
    else evidenceRefs.current.delete(evidenceId)
  }, [])

  const findingKey = detail ? `${task.id}\u0000${detail.finding_id}` : ''
  const launchView = findingKey ? (verificationLaunch[findingKey] ?? null) : null

  return (
    <div className="findings-container min-h-0 min-w-0 flex-1">
      <div className={cn('findings-workspace', showDetail && 'findings-show-detail')}>
      {/* 候选列表与详情按工作区宽度分栏或切换。 */}
      <div ref={listRegionRef} tabIndex={-1} className="findings-list flex min-h-0 min-w-0 flex-col border-border/60">
        <div className="flex shrink-0 items-center justify-between gap-2 border-b border-border/60 px-3 py-1.5">
          <span className="flex min-w-0 items-center gap-2">
            <span className="text-[13px] font-medium text-foreground/75">{t('webapi.findingsTitle')}</span>
            <span className="truncate text-[11px] text-muted-foreground">{t('webapi.findingsHint')}</span>
          </span>
          <Button type="button" variant="ghost" size="xs" onClick={refresh} disabled={loadingList} className="gap-1 text-muted-foreground">
            <RefreshCw className="h-3.5 w-3.5" />
            {t('webapi.refresh')}
          </Button>
        </div>
        <div ref={listScrollRef} onScroll={(event) => rememberScroll('list', event)} className="min-h-0 flex-1 overflow-auto">
          {loadingList && items.length === 0 ? (
            <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
          ) : listError ? (
            <div className="px-3 py-4 text-center text-sm text-destructive">{listError}</div>
          ) : items.length === 0 ? (
            <div className="px-3 py-4 text-center text-sm text-muted-foreground">{t('webapi.findingsEmpty')}</div>
          ) : (
            <div className="divide-y divide-border/40">
              {items.map((item) => (
                <button
                  key={item.finding_id}
                  type="button"
                  ref={(el) => { if (el) rowRefs.current.set(item.finding_id, el); else rowRefs.current.delete(item.finding_id) }}
                  onClick={() => select(item)}
                  aria-pressed={selectedId === item.finding_id}
                  className={cn('flex w-full min-w-0 flex-col gap-1 px-3 py-2 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40', selectedId === item.finding_id && 'bg-primary/10 ring-1 ring-inset ring-primary/40')}
                >
                  <span className="flex w-full items-center justify-between gap-2 text-[12px] text-muted-foreground">
                    <span className="font-mono">{item.number}</span>
                    <span>{t('webapi.findingStatus_candidate')}</span>
                  </span>
                  <span className="findings-list-title text-[13px]" title={item.title}>{item.title}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        {hasMore && (
          <div className="shrink-0 border-t border-border/50 px-3 py-1.5 text-center">
            <Button type="button" variant="ghost" size="xs" onClick={() => void loadList(nextCursor ?? undefined)} disabled={loadingList} className="text-muted-foreground">
              {loadingList ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('webapi.loadMore')}
            </Button>
          </div>
        )}
      </div>

      <div ref={detailRegionRef} className="findings-detail flex min-h-0 min-w-0 flex-col overflow-hidden">
        <div ref={backRef} className="findings-back shrink-0 border-b border-border/60 px-3 py-1.5">
          <Button type="button" variant="ghost" size="xs" onClick={returnToList}>
            {t('webapi.returnToFindings')}
          </Button>
          {detail?.task_id === task.id && detail.finding_id === selectedId && (
            <span className="ml-2 font-mono text-[12px] text-muted-foreground">{detail.number}</span>
          )}
        </div>
        {selectedId == null ? (
          <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">{t('webapi.selectFinding')}</div>
        ) : detailLoading ? (
          <div className="flex flex-1 items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
        ) : detailError ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-destructive">
            <span>{t('webapi.findingLoadFailed')}</span>
            <span className="text-xs text-muted-foreground">{detailError}</span>
          </div>
        ) : detail ? (
          <FindingDetailBody
            scrollRef={detailScrollRef}
            onScroll={(event) => rememberScroll('detail', event)}
            detail={detail}
            canExport={detail.task_id === task.id && detail.finding_id === selectedId}
            evidenceView={evidenceView}
            evidenceDetail={evidenceDetail}
            expandedEvidenceId={expandedEvidenceId}
            evidenceLoading={evidenceLoading}
            evidenceError={evidenceError}
            onToggleEvidence={toggleEvidence}
            onOpenEvidence={openEvidence}
            registerEvidenceRef={registerEvidenceRef}
            task={task}
            onStartVerification={onStartVerification}
            onViewExecution={onViewExecution}
            launchView={launchView}
            onDismissLaunch={() => { if (findingKey) onDismissVerificationLaunch(findingKey) }}
            onResetLaunch={() => { if (findingKey && detail) { onResetVerificationLaunch(findingKey); void loadDetail(detail.finding_id) } }}
          />
        ) : null}
      </div>
      </div>
    </div>
  )
}

function FindingDetailBody({
  scrollRef,
  onScroll,
  detail,
  canExport,
  evidenceView,
  evidenceDetail,
  expandedEvidenceId,
  evidenceLoading,
  evidenceError,
  onToggleEvidence,
  onOpenEvidence,
  registerEvidenceRef,
  task,
  onStartVerification,
  onViewExecution,
  launchView,
  onDismissLaunch,
  onResetLaunch,
}: {
  scrollRef: RefObject<HTMLDivElement>
  onScroll: (event: UIEvent<HTMLDivElement>) => void
  detail: WebApiFindingDetail
  canExport: boolean
  evidenceView: TrafficHttpView | null
  evidenceDetail: WebApiEvidenceDetail | null
  expandedEvidenceId: string | null
  evidenceLoading: boolean
  evidenceError: string
  onToggleEvidence: (evidenceId: string) => void
  onOpenEvidence: (evidenceId: string) => void
  registerEvidenceRef: (evidenceId: string, el: HTMLButtonElement | null) => void
  task: WebApiTask
  onStartVerification: StartVerification
  onViewExecution: (sessionID: string) => void
  launchView: VerificationLaunchView | null
  onDismissLaunch: () => void
  onResetLaunch: () => void
}) {
  const { t } = useTranslation('chat')
  const [exportError, setExportError] = useState('')
  const evidence = detail.evidence ?? []
  const content = detail.content

  useEffect(() => { setExportError('') }, [detail])

  function exportDraft() {
    if (!canExport) return
    setExportError('')
    let url: string | undefined
    let anchor: HTMLAnchorElement | undefined
    try {
      // 同步捕获当前阅读的详情；不重新获取 current 或已展开的证据原件。
      const { filename, markdown } = buildFindingDraft(detail)
      url = URL.createObjectURL(new Blob([markdown], { type: 'text/markdown;charset=utf-8' }))
      anchor = document.createElement('a')
      anchor.href = url
      anchor.download = filename
      document.body.appendChild(anchor)
      anchor.click()
    } catch {
      setExportError(t('webapi.exportDraftFailed'))
    } finally {
      anchor?.remove()
      if (url) URL.revokeObjectURL(url)
    }
  }

  return (
    <div ref={scrollRef} onScroll={onScroll} className="min-h-0 min-w-0 flex-1 overflow-auto">
      <div className="finding-detail-content min-w-0 px-4 py-4">
        <header className="finding-reading-width mb-4 border-b border-border/60 pb-3">
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <span className="font-mono text-[13px] text-muted-foreground">{detail.number}</span>
            <h2 className="text-base font-semibold text-foreground">{content.title || t('webapi.untitledFinding')}</h2>
            <span className="text-[12px] text-muted-foreground">{t('webapi.findingStatus_candidate')}</span>
          </div>
          <div className="mt-1 text-[11px] text-muted-foreground">
            {t('webapi.revision')} {detail.revision} · {formatTime(detail.revision_created_at || detail.updated_at || detail.created_at || '')}
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1">
            <Button type="button" variant="outline" size="xs" onClick={exportDraft} disabled={!canExport}>
              {t('webapi.exportDraft')}
            </Button>
            <span className="text-xs text-muted-foreground">{t('webapi.exportDraftHint')}</span>
          </div>
          {exportError && <p role="alert" className="mt-1 text-xs text-destructive">{exportError}</p>}
        </header>

        <Section label={t('webapi.description')}>
          <p className="whitespace-pre-wrap text-[13px] leading-relaxed text-foreground/90">{content.description}</p>
        </Section>

        {content.steps?.length > 0 && (
          <Section label={t('webapi.steps')}>
            <ol className="list-decimal space-y-1.5 pl-5 text-[13px] leading-relaxed text-foreground/90">
              {content.steps.map((step, index) => (
                <li key={index}>
                  <span className="whitespace-pre-wrap">{step.text}</span>
                  {step.evidence_indices?.length > 0 && (
                    <span className="ml-1 inline-flex flex-wrap items-baseline gap-1 align-baseline">
                      {step.evidence_indices.map((evidenceIndex, refIndex) => {
                        const valid = Number.isInteger(evidenceIndex) && evidenceIndex >= 0 && evidenceIndex < evidence.length
                        if (!valid) {
                          // 非法/越界索引保持纯文本，不跳错到别的证据。
                          return <span key={refIndex} className="text-[11px] text-muted-foreground">[{evidenceIndex + 1}]</span>
                        }
                        const evidenceId = evidence[evidenceIndex].evidence_id
                        return (
                          <button
                            key={refIndex}
                            type="button"
                            onClick={() => onOpenEvidence(evidenceId)}
                            aria-label={t('webapi.evidenceRefLocate', { n: evidenceIndex + 1 })}
                            title={t('webapi.evidenceRefLocate', { n: evidenceIndex + 1 })}
                            className="inline-flex items-center rounded px-1 py-0.5 font-mono text-[11px] text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                          >
                            [{evidenceIndex + 1}]
                          </button>
                        )
                      })}
                    </span>
                  )}
                </li>
              ))}
            </ol>
          </Section>
        )}

        {content.remediation ? (
          <Section label={t('webapi.remediation')}>
            <p className="whitespace-pre-wrap text-[13px] leading-relaxed text-foreground/90">{content.remediation}</p>
          </Section>
        ) : null}

        {content.limitations ? (
          <Section label={t('webapi.limitations')}>
            <p className="whitespace-pre-wrap text-[13px] leading-relaxed text-foreground/90">{content.limitations}</p>
          </Section>
        ) : null}

        {evidence.length > 0 && (
          <Section label={t('webapi.evidenceRefs')} wide>
            <ol className="space-y-1.5">
              {evidence.map((item, index) => (
                <EvidenceReference
                  key={item.evidence_id}
                  index={index}
                  item={item}
                  expanded={expandedEvidenceId === item.evidence_id}
                  loading={expandedEvidenceId === item.evidence_id && evidenceLoading}
                  error={expandedEvidenceId === item.evidence_id ? evidenceError : ''}
                  view={expandedEvidenceId === item.evidence_id ? evidenceView : null}
                  detail={expandedEvidenceId === item.evidence_id ? evidenceDetail : null}
                  onToggle={() => onToggleEvidence(item.evidence_id)}
                  registerEvidenceRef={registerEvidenceRef}
                />
              ))}
            </ol>
          </Section>
        )}

        <div className="finding-verification">
        <WebApiVerificationSection
          key={detail.finding_id}
          task={task}
          finding={detail}
          onStartVerification={onStartVerification}
          onViewExecution={onViewExecution}
          launchView={launchView}
          onDismissLaunch={onDismissLaunch}
          onResetLaunch={onResetLaunch}
        />
        </div>
      </div>
    </div>
  )
}

function Section({ label, children, wide = false }: { label: string; children: ReactNode; wide?: boolean }) {
  return (
    <section className={cn('mb-4', !wide && 'finding-reading-width')}>
      <h3 className="mb-1.5 text-[12px] font-medium text-muted-foreground">{label}</h3>
      {children}
    </section>
  )
}
