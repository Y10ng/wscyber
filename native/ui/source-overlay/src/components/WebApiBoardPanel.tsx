import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronLeft, Loader2, Plus, PlusCircle, RefreshCw, Search, X } from 'lucide-react'
import { cn } from '@cyber/theme'
import {
  Badge,
  Button,
  Callout,
  Dialog,
  DialogContent,
  Field,
  Input,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  Switch,
  Textarea,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@cyber/ui'
import {
  createWebApiBoardOracle,
  listWebApiBoardEndpoints,
  listWebApiBoardFacts,
  listWebApiBoardOracles,
  getWebApiBoard,
  updateWebApiBoardOracle,
  type WebApiBoardCategory,
  type WebApiBoardDigest,
  type WebApiBoardEndpoint,
  type WebApiBoardFact,
  type WebApiBoardFactKind,
  type WebApiBoardOracle,
  type WebApiBoardOracleStatus,
  type WebApiBoardPriority,
  type WebApiTask,
} from '../api'
import { usePolling } from '../hooks/usePolling'
import { FilterPills, type FilterPillItem } from './PageFrame'
import { formatTime } from './WebApiEvidenceReference'
import { DialogFooterRow, DialogHeaderRow } from './ui-dialog'
import './WebApiBoardPanel.css'

type BoardSection = 'oracles' | 'map' | 'facts'
type OracleFilter = 'all' | WebApiBoardOracleStatus

const ORACLE_FILTERS: OracleFilter[] = ['all', 'open', 'in_progress', 'holds', 'refuted', 'blocked', 'dropped']
const STATUS_LABEL_KEYS: Record<WebApiBoardOracleStatus, string> = {
  open: 'board.statusOpen',
  in_progress: 'board.statusInProgress',
  holds: 'board.statusHolds',
  refuted: 'board.statusRefuted',
  blocked: 'board.statusBlocked',
  dropped: 'board.statusDropped',
}
const STATUS_DOT: Record<WebApiBoardOracleStatus, string> = {
  open: 'bg-muted-foreground',
  in_progress: 'bg-info',
  holds: 'bg-destructive',
  refuted: 'bg-success',
  blocked: 'bg-warning',
  dropped: 'bg-muted-foreground',
}
const PRIORITY_LABEL_KEYS: Record<WebApiBoardPriority, string> = {
  high: 'board.priorityHigh',
  medium: 'board.priorityMedium',
  low: 'board.priorityLow',
}
const PRIORITY_DOT: Record<WebApiBoardPriority, string> = {
  high: 'bg-destructive',
  medium: 'bg-warning',
  low: 'bg-muted-foreground',
}
const CATEGORY_LABEL_KEYS: Record<WebApiBoardCategory, string> = {
  authz: 'board.categoryAuthz',
  authn: 'board.categoryAuthn',
  injection: 'board.categoryInjection',
  ssrf: 'board.categorySsrf',
  file: 'board.categoryFile',
  logic: 'board.categoryLogic',
  info_disclosure: 'board.categoryInfoDisclosure',
  config: 'board.categoryConfig',
  other: 'board.categoryOther',
}
const CATEGORIES: WebApiBoardCategory[] = ['authz', 'authn', 'injection', 'ssrf', 'file', 'logic', 'info_disclosure', 'config', 'other']
const FACT_KIND_LABEL_KEYS: Record<WebApiBoardFactKind, string> = {
  endpoint: 'board.factKindEndpoint',
  auth: 'board.factKindAuth',
  role: 'board.factKindRole',
  tech: 'board.factKindTech',
  config: 'board.factKindConfig',
  data: 'board.factKindData',
  other: 'board.factKindOther',
}
const FACT_KINDS: WebApiBoardFactKind[] = ['endpoint', 'auth', 'role', 'tech', 'config', 'data', 'other']
const METHOD_TONE: Record<string, string> = {
  GET: 'text-success',
  POST: 'text-info',
  PUT: 'text-warning',
  PATCH: 'text-caution',
  DELETE: 'text-destructive',
  HEAD: 'text-muted-foreground',
  OPTIONS: 'text-muted-foreground',
}

function statusVariant(status: WebApiBoardOracleStatus): 'muted' | 'info' | 'danger' | 'success' | 'warning' {
  switch (status) {
    case 'in_progress': return 'info'
    case 'holds': return 'danger'
    case 'refuted': return 'success'
    case 'blocked': return 'warning'
    default: return 'muted'
  }
}

function authLabelKey(authSeen: string): string {
  if (authSeen === 'none') return 'board.authNone'
  if (authSeen === 'with') return 'board.authWith'
  if (authSeen === 'both') return 'board.authBoth'
  return 'board.authUnknown'
}

function sourceLabelKey(source: string): string {
  return source === 'model' ? 'board.sourceModel' : 'board.sourceTraffic'
}

function mergeOracles(current: WebApiBoardOracle[], page: WebApiBoardOracle[]): WebApiBoardOracle[] {
  const byNumber = new Map(current.map((item) => [item.number, item]))
  for (const item of page) byNumber.set(item.number, item)
  return [...byNumber.values()].sort((a, b) => Number(a.number.replace(/\D/g, '')) - Number(b.number.replace(/\D/g, '')))
}

function mergeEndpoints(current: WebApiBoardEndpoint[], page: WebApiBoardEndpoint[]): WebApiBoardEndpoint[] {
  const byId = new Map(current.map((item) => [item.id, item]))
  for (const item of page) byId.set(item.id, item)
  return [...byId.values()].sort((a, b) => a.endpoint_key.localeCompare(b.endpoint_key))
}

function mergeFacts(current: WebApiBoardFact[], page: WebApiBoardFact[]): WebApiBoardFact[] {
  const pageIds = new Set(page.map((item) => item.id))
  return [...page, ...current.filter((item) => !pageIds.has(item.id))]
}

interface Props {
  task: WebApiTask
  onViewFinding: (findingId: string) => void
  onViewEvidence: (evidenceId: string) => void
  onOpenSession: (sessionId: string) => void
  /** 用户提出/调整 Oracle 后通知 App 刷新工作区页签计数。 */
  onCountsChanged?: () => void
}

/**
 * 任务黑板：指标条 + Oracle / 接口地图 / 事实三段。
 * 只做后端 M2 已冻结的读取与人工 Oracle 操作；成立不等于漏洞确认。
 */
export default function WebApiBoardPanel({ task, onViewFinding, onViewEvidence, onOpenSession, onCountsChanged }: Props) {
  const { t } = useTranslation('chat')
  const [section, setSection] = useState<BoardSection>('oracles')
  const [board, setBoard] = useState<WebApiBoardDigest | null>(null)
  const [boardLoading, setBoardLoading] = useState(true)
  const [boardError, setBoardError] = useState('')

  const [proposeOpen, setProposeOpen] = useState(false)
  const [proposePrefill, setProposePrefill] = useState<string[]>([])

  const [oracleStatus, setOracleStatus] = useState<OracleFilter>('all')
  const [oracleCategory, setOracleCategory] = useState<WebApiBoardCategory | ''>('')
  const [oracles, setOracles] = useState<WebApiBoardOracle[]>([])
  const [oraclesLoading, setOraclesLoading] = useState(false)
  const [oraclesLoadingMore, setOraclesLoadingMore] = useState(false)
  const [oraclesError, setOraclesError] = useState('')
  const [oracleHasMore, setOracleHasMore] = useState(false)
  const [oracleNextCursor, setOracleNextCursor] = useState<string | null>(null)
  const [selectedNumber, setSelectedNumber] = useState<string | null>(null)
  const [showDetail, setShowDetail] = useState(false)
  const [actionBusy, setActionBusy] = useState(false)
  const [actionError, setActionError] = useState('')
  const selectedNumberRef = useRef<string | null>(null)
  selectedNumberRef.current = selectedNumber

  const [mapQuery, setMapQuery] = useState('')
  const [mapUntested, setMapUntested] = useState(false)
  const [endpoints, setEndpoints] = useState<WebApiBoardEndpoint[]>([])
  const [endpointsLoading, setEndpointsLoading] = useState(false)
  const [endpointsLoadingMore, setEndpointsLoadingMore] = useState(false)
  const [endpointsError, setEndpointsError] = useState('')
  const [endpointHasMore, setEndpointHasMore] = useState(false)
  const [endpointNextCursor, setEndpointNextCursor] = useState<string | null>(null)

  const [factKind, setFactKind] = useState<WebApiBoardFactKind | ''>('')
  const [showRetracted, setShowRetracted] = useState(false)
  const [facts, setFacts] = useState<WebApiBoardFact[]>([])
  const [factsLoading, setFactsLoading] = useState(false)
  const [factsLoadingMore, setFactsLoadingMore] = useState(false)
  const [factsError, setFactsError] = useState('')
  const [factHasMore, setFactHasMore] = useState(false)
  const [factNextCursor, setFactNextCursor] = useState<string | null>(null)

  const boardSeq = useRef(0)
  const oracleSeq = useRef(0)
  const endpointSeq = useRef(0)
  const factSeq = useRef(0)

  const loadBoard = useCallback(async (silent = false) => {
    const seq = ++boardSeq.current
    if (!silent) setBoardLoading(true)
    try {
      const result = await getWebApiBoard(task.id)
      if (seq !== boardSeq.current) return
      setBoard(result)
      setBoardError('')
    } catch (err) {
      if (seq !== boardSeq.current) return
      if (!silent) setBoardError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === boardSeq.current && !silent) setBoardLoading(false)
    }
  }, [task.id])

  const loadOracles = useCallback(async (cursor?: string, silent = false) => {
    const seq = ++oracleSeq.current
    const reset = !cursor
    if (!silent) {
      if (reset) setOraclesLoading(true)
      else setOraclesLoadingMore(true)
    }
    try {
      const result = await listWebApiBoardOracles(task.id, {
        status: oracleStatus === 'all' ? undefined : oracleStatus,
        category: oracleCategory || undefined,
        limit: 50,
        cursor,
      })
      if (seq !== oracleSeq.current) return
      setOracles((current) => {
        if (cursor) return mergeOracles(current, result.items)
        // 静默轮询与筛选重载都保留当前选中项：新编号可能不在第一页，但详情不能消失。
        const selected = current.find((item) => item.number === selectedNumberRef.current)
        const next = result.items.some((item) => item.number === selected?.number)
          ? result.items
          : selected ? mergeOracles(result.items, [selected]) : result.items
        return next
      })
      setOracleHasMore(result.has_more)
      setOracleNextCursor(result.next_cursor)
      setOraclesError('')
    } catch (err) {
      if (seq !== oracleSeq.current) return
      if (!silent) setOraclesError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === oracleSeq.current && !silent) {
        setOraclesLoading(false)
        setOraclesLoadingMore(false)
      }
    }
  }, [task.id, oracleStatus, oracleCategory])

  const loadEndpoints = useCallback(async (cursor?: string, silent = false) => {
    const seq = ++endpointSeq.current
    const reset = !cursor
    if (!silent) {
      if (reset) setEndpointsLoading(true)
      else setEndpointsLoadingMore(true)
    }
    try {
      const result = await listWebApiBoardEndpoints(task.id, {
        q: mapQuery.trim() || undefined,
        untested: mapUntested,
        limit: 50,
        cursor,
      })
      if (seq !== endpointSeq.current) return
      setEndpoints((current) => (cursor ? mergeEndpoints(current, result.items) : silent ? mergeEndpoints(current, result.items) : result.items))
      setEndpointHasMore(result.has_more)
      setEndpointNextCursor(result.next_cursor)
      setEndpointsError('')
    } catch (err) {
      if (seq !== endpointSeq.current) return
      if (!silent) setEndpointsError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === endpointSeq.current && !silent) {
        setEndpointsLoading(false)
        setEndpointsLoadingMore(false)
      }
    }
  }, [task.id, mapQuery, mapUntested])

  const loadFacts = useCallback(async (cursor?: string, silent = false) => {
    const seq = ++factSeq.current
    const reset = !cursor
    if (!silent) {
      if (reset) setFactsLoading(true)
      else setFactsLoadingMore(true)
    }
    try {
      const result = await listWebApiBoardFacts(task.id, {
        kind: factKind || undefined,
        status: showRetracted ? undefined : 'active',
        limit: 50,
        cursor,
      })
      if (seq !== factSeq.current) return
      setFacts((current) => (cursor ? mergeFacts(current, result.items) : silent ? mergeFacts(current, result.items) : result.items))
      setFactHasMore(result.has_more)
      setFactNextCursor(result.next_cursor)
      setFactsError('')
    } catch (err) {
      if (seq !== factSeq.current) return
      if (!silent) setFactsError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === factSeq.current && !silent) {
        setFactsLoading(false)
        setFactsLoadingMore(false)
      }
    }
  }, [task.id, factKind, showRetracted])

  // 换任务：清空全部派生状态并使在途请求失效。
  useEffect(() => {
    boardSeq.current++
    oracleSeq.current++
    endpointSeq.current++
    factSeq.current++
    setBoard(null)
    setBoardLoading(true)
    setBoardError('')
    setSection('oracles')
    setOracleStatus('all')
    setOracleCategory('')
    setOracles([])
    setOraclesError('')
    setOracleHasMore(false)
    setOracleNextCursor(null)
    setSelectedNumber(null)
    setShowDetail(false)
    setMapQuery('')
    setMapUntested(false)
    setEndpoints([])
    setEndpointsError('')
    setEndpointHasMore(false)
    setEndpointNextCursor(null)
    setFactKind('')
    setShowRetracted(false)
    setFacts([])
    setFactsError('')
    setFactHasMore(false)
    setFactNextCursor(null)
    void loadBoard()
    // Oracle 首屏由 loadOracles 的筛选 effect 触发；这里不重复发请求。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.id])

  // 筛选变化：重新取第一页（保留当前选中项由 loadOracles 处理）。
  useEffect(() => { void loadOracles() }, [loadOracles])
  // 搜索防抖，避免每次按键都发请求。
  useEffect(() => {
    const timer = setTimeout(() => { void loadEndpoints() }, 250)
    return () => clearTimeout(timer)
  }, [loadEndpoints])
  useEffect(() => { void loadFacts() }, [loadFacts])

  // 视图可见时 10 秒静默轮询：计数 + 当前段第一页；不打断选中项与已加载分页。
  usePolling(() => {
    void loadBoard(true)
    if (section === 'oracles') void loadOracles(undefined, true)
    else if (section === 'map') void loadEndpoints(undefined, true)
    else void loadFacts(undefined, true)
  }, 10000)

  const counts = board?.counts
  const oracleCount = useCallback((status: WebApiBoardOracleStatus) => counts?.oracles?.[status] ?? 0, [counts])
  const totalOracles = ORACLE_FILTERS.reduce((total, key) => total + (key === 'all' ? 0 : oracleCount(key)), 0)
  const concluded = oracleCount('holds') + oracleCount('refuted') + oracleCount('blocked')
  const pendingOracles = oracleCount('open') + oracleCount('in_progress')
  const selectedOracle = useMemo(
    () => oracles.find((item) => item.number === selectedNumber) ?? null,
    [oracles, selectedNumber],
  )
  const emptyBoard = !!counts && counts.endpoints === 0 && totalOracles === 0

  const refreshAll = useCallback(() => {
    void loadBoard()
    if (section === 'oracles') void loadOracles()
    else if (section === 'map') void loadEndpoints()
    else void loadFacts()
  }, [section, loadBoard, loadOracles, loadEndpoints, loadFacts])

  const selectOracle = (number: string) => {
    setSelectedNumber(number)
    setShowDetail(true)
    setActionError('')
  }

  // 地图 / 提出结果 → Oracle 段并选中；编号不在第一页时用大页补齐详情。
  const revealOracle = useCallback((number: string) => {
    setSection('oracles')
    setOracleStatus('all')
    setOracleCategory('')
    setSelectedNumber(number)
    setShowDetail(true)
    setActionError('')
    void listWebApiBoardOracles(task.id, { limit: 200 })
      .then((result) => {
        setOracles((current) => {
          const selected = current.find((item) => item.number === number)
          return mergeOracles(result.items, selected ? [selected] : [])
        })
        setOracleHasMore(result.has_more)
        setOracleNextCursor(result.next_cursor)
      })
      .catch(() => { /* 列表错误由当前段自行显示；详情保持选中 */ })
  }, [task.id])

  const focusEndpoint = useCallback((endpointKey: string) => {
    setSection('map')
    setMapUntested(false)
    setMapQuery(endpointKey)
  }, [])

  const openPropose = (endpointKeys: string[] = []) => {
    setProposePrefill(endpointKeys)
    setProposeOpen(true)
  }

  const applyOracleUpdate = useCallback(async (patch: { priority?: WebApiBoardPriority; status?: 'open' | 'dropped' }) => {
    if (!selectedOracle || actionBusy) return
    setActionBusy(true)
    setActionError('')
    try {
      const result = await updateWebApiBoardOracle(task.id, selectedOracle.number, patch)
      setOracles((current) => current.map((item) => (item.number === result.oracle.number ? result.oracle : item)))
      void loadBoard(true)
      onCountsChanged?.()
    } catch (err) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setActionBusy(false)
    }
  }, [task.id, selectedOracle, actionBusy, loadBoard, onCountsChanged])

  const handleOracleProposed = (oracle: WebApiBoardOracle, created: boolean) => {
    revealOracle(oracle.number)
    if (created) {
      void loadBoard(true)
      onCountsChanged?.()
    }
  }

  const filterItems: FilterPillItem<OracleFilter>[] = ORACLE_FILTERS.map((key) => ({
    key,
    label: key === 'all' ? t('board.filterAll') : t(STATUS_LABEL_KEYS[key]),
    count: key === 'all' ? totalOracles : oracleCount(key),
    dot: key === 'all' ? undefined : STATUS_DOT[key],
  }))

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      {/* 指标条：接口 / 未覆盖 / 待走 / 已结论 / 事实。 */}
      <div className="flex h-14 shrink-0 items-center gap-6 overflow-x-auto border-b border-border px-5">
        <div className="flex shrink-0 items-baseline gap-1.5">
          <span className="text-[18px] font-semibold tabular-nums text-foreground">{counts?.endpoints ?? '—'}</span>
          <span className="text-[12px] text-muted-foreground">{t('board.metricsEndpoints')}</span>
          {counts && (
            <span className="text-[11px] text-muted-foreground">
              {t('board.metricsTrafficModel', { traffic: counts.traffic_endpoints, model: counts.model_endpoints })}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-baseline gap-1.5">
          <span className={cn('text-[18px] font-semibold tabular-nums', (counts?.untested_endpoints ?? 0) > 0 ? 'text-warning' : 'text-foreground')}>
            {counts?.untested_endpoints ?? '—'}
          </span>
          <span className="text-[12px] text-muted-foreground">{t('board.metricsUntested')}</span>
        </div>
        <div className="flex shrink-0 items-baseline gap-1.5">
          <span className="text-[18px] font-semibold tabular-nums text-foreground">{counts ? pendingOracles : '—'}</span>
          <span className="text-[12px] text-muted-foreground">{t('board.metricsPending')}</span>
        </div>
        <div className="flex shrink-0 items-baseline gap-1.5">
          <span className="text-[18px] font-semibold tabular-nums text-foreground">{counts ? concluded : '—'}</span>
          <span className="text-[12px] text-muted-foreground">{t('board.metricsConcluded')}</span>
          {counts && (
            <span className="text-[11px] font-medium tabular-nums text-destructive">
              {t('board.metricsHolds', { count: oracleCount('holds') })}
            </span>
          )}
        </div>
        <div className="flex shrink-0 items-baseline gap-1.5">
          <span className="text-[18px] font-semibold tabular-nums text-foreground">{counts?.facts ?? '—'}</span>
          <span className="text-[12px] text-muted-foreground">{t('board.metricsFacts')}</span>
          {counts && <span className="text-[11px] text-muted-foreground">{t('board.metricsConfirmedFacts', { count: counts.confirmed_facts })}</span>}
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button type="button" variant="ghost" size="icon-sm" onClick={refreshAll} disabled={boardLoading} aria-label={t('board.refresh')} className="text-muted-foreground">
                {boardLoading ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t('board.refresh')}</TooltipContent>
          </Tooltip>
          <Button type="button" size="sm" onClick={() => openPropose()} className="gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            {t('board.propose')}
          </Button>
        </div>
      </div>

      {boardError && <div className="shrink-0 px-4 py-1.5"><Callout tone="destructive">{boardError}</Callout></div>}

      <div className="flex shrink-0 items-center gap-3 border-b border-border/70 px-4 py-2">
        <FilterPills
          items={[
            { key: 'oracles', label: t('board.segmentOracles') },
            { key: 'map', label: t('board.segmentMap') },
            { key: 'facts', label: t('board.segmentFacts') },
          ]}
          value={section}
          onChange={setSection}
        />
      </div>

      {section === 'oracles' ? (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border/70 px-4 py-2">
            <FilterPills
              items={filterItems}
              value={oracleStatus}
              onChange={(next) => { setSelectedNumber(null); setShowDetail(false); setOracleStatus(next) }}
            />
            <Select
              value={oracleCategory || 'all'}
              onValueChange={(value) => { setSelectedNumber(null); setShowDetail(false); setOracleCategory(value === 'all' ? '' : value as WebApiBoardCategory) }}
            >
              <SelectTrigger aria-label={t('board.category')} className="ml-auto h-8 w-[150px] shrink-0 text-[12px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('board.categoryAll')}</SelectItem>
                {CATEGORIES.map((category) => (
                  <SelectItem key={category} value={category}>{t(CATEGORY_LABEL_KEYS[category])}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="board-container min-h-0 min-w-0 flex-1">
            <div className={cn('board-workspace', showDetail && 'board-show-detail')}>
              <div className="board-list flex min-h-0 min-w-0 flex-col">
                <div className="flex h-10 shrink-0 items-center justify-between gap-2 border-b border-border/60 px-4">
                  <span className="flex min-w-0 items-center gap-2 text-[13px] font-medium text-foreground">
                    {t('board.oracleListTitle')}
                    <span className="font-mono text-[11px] tabular-nums text-muted-foreground">{oracles.length}{oracleHasMore ? '+' : ''}</span>
                  </span>
                  <span className="text-[11px] text-muted-foreground">{t('board.oracleStatusCounts', { pending: pendingOracles, total: totalOracles })}</span>
                </div>
                <div className="min-h-0 flex-1 overflow-auto">
                  {oraclesLoading && oracles.length === 0 ? (
                    <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
                  ) : oraclesError ? (
                    <div className="px-4 py-5 text-center text-sm text-destructive">{oraclesError}</div>
                  ) : oracles.length === 0 ? (
                    <div className="px-4 py-5 text-center text-[13px] leading-relaxed text-muted-foreground">
                      {emptyBoard ? t('board.emptyBoard') : t('board.oracleEmpty')}
                    </div>
                  ) : (
                    <div>
                      {oracles.map((oracle) => (
                        <button
                          key={oracle.number}
                          type="button"
                          onClick={() => selectOracle(oracle.number)}
                          aria-pressed={selectedNumber === oracle.number}
                          className={cn(
                            'flex min-h-14 w-full min-w-0 flex-col gap-1 border-b border-border/70 px-4 py-2 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                            selectedNumber === oracle.number && 'bg-primary/10 ring-1 ring-inset ring-primary/40',
                          )}
                        >
                          <span className="flex w-full min-w-0 items-center gap-2">
                            <span className="shrink-0 font-mono text-[12px] tabular-nums text-muted-foreground">{oracle.number}</span>
                            <span aria-hidden className={cn('h-2 w-2 shrink-0 rounded-full', PRIORITY_DOT[oracle.priority])} title={t(PRIORITY_LABEL_KEYS[oracle.priority])} />
                            <span className="board-question-clamp min-w-0 flex-1 text-[13px] leading-[1.4] text-foreground" title={oracle.question}>{oracle.question}</span>
                          </span>
                          <span className="flex w-full min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
                            <span className="truncate">{t(CATEGORY_LABEL_KEYS[oracle.category] ?? 'board.categoryOther')}</span>
                            <span aria-hidden>·</span>
                            <span>{t('board.endpointCount', { count: oracle.endpoint_keys.length })}</span>
                            <span aria-hidden>·</span>
                            <span>{oracle.origin === 'user' ? t('board.originUser') : t('board.originModel')}</span>
                            <span className="ml-auto shrink-0">
                              <Badge size="sm" variant={statusVariant(oracle.status)} className={cn(oracle.status === 'dropped' && 'line-through decoration-1')}>
                                {t(STATUS_LABEL_KEYS[oracle.status] ?? 'board.statusOpen')}
                              </Badge>
                            </span>
                          </span>
                        </button>
                      ))}
                    </div>
                  )}
                </div>
                {oracleHasMore && (
                  <div className="shrink-0 border-t border-border/50 px-4 py-1.5 text-center">
                    <Button type="button" variant="ghost" size="xs" onClick={() => void loadOracles(oracleNextCursor ?? undefined)} disabled={oraclesLoadingMore} className="text-muted-foreground">
                      {oraclesLoadingMore ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('board.loadMore')}
                    </Button>
                  </div>
                )}
              </div>

              <div className="board-detail min-h-0 min-w-0 flex-col">
                <div className="flex h-10 shrink-0 items-center gap-2 border-b border-border/60 px-3">
                  <Button type="button" variant="ghost" size="xs" onClick={() => setShowDetail(false)} className="board-back -ml-1 shrink-0 text-muted-foreground">
                    <ChevronLeft className="h-3.5 w-3.5" />
                    {t('board.backToList')}
                  </Button>
                  {selectedOracle && <span className="font-mono text-[12px] font-semibold tabular-nums text-foreground">{selectedOracle.number}</span>}
                  {selectedOracle && (
                    <div className="ml-auto flex shrink-0 items-center gap-1.5">
                      <Select
                        value={selectedOracle.priority}
                        onValueChange={(value) => void applyOracleUpdate({ priority: value as WebApiBoardPriority })}
                        disabled={actionBusy}
                      >
                        <SelectTrigger aria-label={t('board.fieldPriority')} className="h-7 w-[88px] px-2 text-[12px]">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {(['high', 'medium', 'low'] as WebApiBoardPriority[]).map((priority) => (
                            <SelectItem key={priority} value={priority}>{t(PRIORITY_LABEL_KEYS[priority])}</SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                      {selectedOracle.status !== 'open' && (
                        <Button type="button" variant="outline" size="xs" onClick={() => void applyOracleUpdate({ status: 'open' })} disabled={actionBusy}>
                          {t('board.reopen')}
                        </Button>
                      )}
                      {selectedOracle.status !== 'dropped' && (
                        <Button
                          type="button"
                          variant="outline"
                          size="xs"
                          onClick={() => { if (window.confirm(t('board.dropConfirm'))) void applyOracleUpdate({ status: 'dropped' }) }}
                          disabled={actionBusy}
                        >
                          {t('board.drop')}
                        </Button>
                      )}
                    </div>
                  )}
                </div>
                {selectedOracle == null ? (
                  <div className="flex flex-1 items-center justify-center px-4 text-center text-sm text-muted-foreground">{t('board.selectOracle')}</div>
                ) : (
                  <div className="min-h-0 flex-1 overflow-auto">
                    <div className="space-y-5 px-4 py-4">
                      {actionError && <Callout tone="destructive">{actionError}</Callout>}
                      <p className="whitespace-pre-wrap text-[14px] leading-[1.65] text-foreground">{selectedOracle.question}</p>

                      <dl className="grid grid-cols-[84px_minmax(0,1fr)] gap-x-3 gap-y-2 text-[13px]">
                        <dt className="text-muted-foreground">{t('board.fieldCategory')}</dt>
                        <dd className="text-foreground">{t(CATEGORY_LABEL_KEYS[selectedOracle.category] ?? 'board.categoryOther')}</dd>
                        <dt className="text-muted-foreground">{t('board.fieldPriority')}</dt>
                        <dd className="flex items-center gap-1.5 text-foreground">
                          <span aria-hidden className={cn('h-2 w-2 rounded-full', PRIORITY_DOT[selectedOracle.priority])} />
                          {t(PRIORITY_LABEL_KEYS[selectedOracle.priority])}
                        </dd>
                        <dt className="text-muted-foreground">{t('board.fieldStatus')}</dt>
                        <dd>
                          <Badge size="sm" variant={statusVariant(selectedOracle.status)} className={cn(selectedOracle.status === 'dropped' && 'line-through decoration-1')}>
                            {t(STATUS_LABEL_KEYS[selectedOracle.status] ?? 'board.statusOpen')}
                          </Badge>
                        </dd>
                        <dt className="text-muted-foreground">{t('board.fieldOrigin')}</dt>
                        <dd className="text-foreground">{selectedOracle.origin === 'user' ? t('board.originUser') : t('board.originModel')}</dd>
                        <dt className="text-muted-foreground">{t('board.fieldSession')}</dt>
                        <dd className="min-w-0">
                          {selectedOracle.claimed_session_id ? (
                            <button
                              type="button"
                              onClick={() => onOpenSession(selectedOracle.claimed_session_id)}
                              title={selectedOracle.claimed_session_id}
                              className="max-w-full truncate font-mono text-[12px] text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                            >
                              {selectedOracle.claimed_session_id}
                            </button>
                          ) : <span className="text-muted-foreground">—</span>}
                        </dd>
                        <dt className="text-muted-foreground">{t('board.fieldCreated')}</dt>
                        <dd className="text-foreground">{formatTime(selectedOracle.created_at) || '—'}</dd>
                        <dt className="text-muted-foreground">{t('board.fieldUpdated')}</dt>
                        <dd className="text-foreground">{formatTime(selectedOracle.updated_at) || '—'}</dd>
                      </dl>

                      <section>
                        <h3 className="mb-2 text-[12px] font-medium text-muted-foreground">{t('board.endpointsTitle')}</h3>
                        {selectedOracle.endpoint_keys.length === 0 ? (
                          <p className="text-[12px] text-muted-foreground">{t('board.endpointsEmpty')}</p>
                        ) : (
                          <div className="flex flex-wrap gap-1.5">
                            {selectedOracle.endpoint_keys.map((key) => (
                              <button
                                key={key}
                                type="button"
                                onClick={() => focusEndpoint(key)}
                                title={key}
                                className="max-w-full truncate rounded border border-border bg-background px-2 py-1 font-mono text-[11px] text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                              >
                                {key}
                              </button>
                            ))}
                          </div>
                        )}
                      </section>

                      <section>
                        <h3 className="mb-2 text-[12px] font-medium text-muted-foreground">{t('board.resultTitle')}</h3>
                        {selectedOracle.result_summary ? (
                          <p className="whitespace-pre-wrap text-[13px] leading-[1.65] text-foreground">{selectedOracle.result_summary}</p>
                        ) : (
                          <p className="text-[12px] text-muted-foreground">{t('board.resultEmpty')}</p>
                        )}
                        {(selectedOracle.finding_id || selectedOracle.evidence_ids.length > 0) && (
                          <div className="mt-2 flex flex-wrap gap-1.5">
                            {selectedOracle.finding_id && selectedOracle.finding_number && (
                              <Button type="button" variant="outline" size="xs" onClick={() => onViewFinding(selectedOracle.finding_id)}>
                                {t('board.viewFinding', { number: selectedOracle.finding_number })}
                              </Button>
                            )}
                            {selectedOracle.evidence_ids.map((evidenceId) => (
                              <Button
                                key={evidenceId}
                                type="button"
                                variant="ghost"
                                size="xs"
                                title={evidenceId}
                                onClick={() => onViewEvidence(evidenceId)}
                                className="max-w-[240px] truncate font-mono text-muted-foreground"
                              >
                                {t('board.viewEvidence', { id: evidenceId })}
                              </Button>
                            ))}
                          </div>
                        )}
                      </section>

                      <section>
                        <h3 className="mb-2 text-[12px] font-medium text-muted-foreground">{t('board.eventsTitle')}</h3>
                        {(selectedOracle.events?.length ?? 0) === 0 ? (
                          <p className="text-[12px] text-muted-foreground">{t('board.eventsEmpty')}</p>
                        ) : (
                          <ol className="space-y-2.5">
                            {selectedOracle.events!.map((event, index) => (
                              <li key={`${event.at}-${index}`} className="border-l-2 border-border pl-3">
                                <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px]">
                                  <span className="font-medium text-foreground">
                                    {event.from ? t(STATUS_LABEL_KEYS[event.from as WebApiBoardOracleStatus] ?? 'board.statusOpen') : t('board.eventCreated')}
                                    <span aria-hidden className="mx-1 text-muted-foreground">→</span>
                                    {t(STATUS_LABEL_KEYS[event.to as WebApiBoardOracleStatus] ?? 'board.statusOpen')}
                                  </span>
                                  <span className="text-muted-foreground">{event.actor === 'user' ? t('board.actorUser') : t('board.actorModel')}</span>
                                  <span className="ml-auto tabular-nums text-muted-foreground">{formatTime(event.at)}</span>
                                </div>
                                {event.summary && <p className="mt-0.5 whitespace-pre-wrap text-[12px] text-muted-foreground">{event.summary}</p>}
                              </li>
                            ))}
                          </ol>
                        )}
                      </section>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      ) : section === 'map' ? (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border/70 px-4 py-2">
            <div className="relative">
              <Search aria-hidden className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={mapQuery}
                onChange={(event) => setMapQuery(event.target.value)}
                placeholder={t('board.mapSearchPlaceholder')}
                aria-label={t('board.mapSearchPlaceholder')}
                className="h-8 w-[300px] pl-8 text-[12px]"
              />
            </div>
            <label className="flex cursor-pointer items-center gap-2 text-[12px] text-muted-foreground">
              <Switch checked={mapUntested} onCheckedChange={setMapUntested} className="scale-75" />
              {t('board.mapUntestedOnly')}
            </label>
            <span className="ml-auto text-[11px] tabular-nums text-muted-foreground">{endpoints.length}{endpointHasMore ? '+' : ''}</span>
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
            {endpointsLoading && endpoints.length === 0 ? (
              <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
            ) : endpointsError ? (
              <div className="px-4 py-5 text-center text-sm text-destructive">{endpointsError}</div>
            ) : endpoints.length === 0 ? (
              <div className="px-4 py-5 text-center text-[13px] text-muted-foreground">
                {mapQuery.trim() || mapUntested ? t('board.mapEmptyFilter') : t('board.mapEmpty')}
              </div>
            ) : (
              <table className="w-full min-w-[900px] table-fixed text-[12px]">
                <colgroup>
                  <col className="w-16" />
                  <col />
                  <col className="w-[180px]" />
                  <col className="w-[120px]" />
                  <col className="w-16" />
                  <col className="w-16" />
                  <col className="w-24" />
                  <col className="w-[88px]" />
                  <col className="w-11" />
                </colgroup>
                <thead className="sticky top-0 z-10 bg-card text-[11px] font-medium text-muted-foreground">
                  <tr className="h-10 border-b border-border">
                    <th className="px-3 text-left font-medium">{t('board.colMethod')}</th>
                    <th className="px-3 text-left font-medium">{t('board.colPath')}</th>
                    <th className="px-3 text-left font-medium">{t('board.colParams')}</th>
                    <th className="px-3 text-left font-medium">{t('board.colStatusCodes')}</th>
                    <th className="px-2 text-left font-medium">{t('board.colAuth')}</th>
                    <th className="px-3 text-right font-medium">{t('board.colRequests')}</th>
                    <th className="px-2 text-left font-medium">{t('board.colOracle')}</th>
                    <th className="px-2 text-left font-medium">{t('board.colSource')}</th>
                    <th className="px-2 text-left font-medium"><span className="sr-only">{t('board.colActions')}</span></th>
                  </tr>
                </thead>
                <tbody>
                  {endpoints.map((endpoint) => {
                    const params = endpoint.params.slice(0, 4)
                    const codes = endpoint.status_codes.slice(0, 4)
                    return (
                      <tr key={endpoint.id} className="border-b border-border/70 transition-colors hover:bg-muted/40">
                        <td className="px-3 py-1.5">
                          <span className={cn('font-mono text-[11px] font-semibold', METHOD_TONE[endpoint.method.toUpperCase()] ?? 'text-muted-foreground')}>{endpoint.method}</span>
                        </td>
                        <td className="min-w-0 px-3 py-1.5">
                          <div className="flex min-w-0 flex-col">
                            <span className="truncate text-[11px] text-muted-foreground" title={endpoint.origin}>{endpoint.origin || '—'}</span>
                            <span className="truncate font-mono text-[12px] text-foreground" title={endpoint.endpoint_key}>{endpoint.path}</span>
                          </div>
                        </td>
                        <td className="min-w-0 px-3 py-1.5" title={endpoint.params.join(', ')}>
                          {endpoint.params.length === 0 ? (
                            <span className="text-muted-foreground">—</span>
                          ) : (
                            <span className="block truncate font-mono text-[11px] text-foreground/80">
                              {params.join(', ')}{endpoint.params.length > params.length ? ` +${endpoint.params.length - params.length}` : ''}
                            </span>
                          )}
                        </td>
                        <td className="px-3 py-1.5">
                          <div className="flex flex-wrap items-center gap-1">
                            {codes.length === 0 ? <span className="text-muted-foreground">—</span> : codes.map((code) => (
                              <Badge key={code} size="sm" variant={code >= 500 ? 'danger' : code >= 400 ? 'warning' : code >= 300 ? 'info' : 'success'} className="tabular-nums">{code}</Badge>
                            ))}
                            {endpoint.status_codes.length > codes.length && <span className="text-[10px] text-muted-foreground">+{endpoint.status_codes.length - codes.length}</span>}
                          </div>
                        </td>
                        <td className="px-2 py-1.5 text-[11px] text-muted-foreground">{t(authLabelKey(endpoint.auth_seen))}</td>
                        <td className="px-3 py-1.5 text-right tabular-nums text-foreground">{endpoint.request_count}</td>
                        <td className="px-2 py-1.5">
                          {endpoint.oracles.length === 0 ? (
                            <span className="text-[11px] text-warning">{t('board.uncovered')}</span>
                          ) : (
                            <span className="flex flex-wrap gap-1">
                              {endpoint.oracles.slice(0, 3).map((number) => (
                                <button
                                  key={number}
                                  type="button"
                                  onClick={() => revealOracle(number)}
                                  className="font-mono text-[11px] text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                                >
                                  {number}
                                </button>
                              ))}
                              {endpoint.oracles.length > 3 && <span className="text-[10px] text-muted-foreground">+{endpoint.oracles.length - 3}</span>}
                            </span>
                          )}
                        </td>
                        <td className="px-2 py-1.5 text-[11px] text-muted-foreground">{t(sourceLabelKey(endpoint.source))}</td>
                        <td className="px-2 py-1.5">
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-xs"
                                onClick={() => openPropose([endpoint.endpoint_key])}
                                aria-label={t('board.proposeForEndpoint')}
                                className="text-muted-foreground hover:text-primary"
                              >
                                <PlusCircle className="h-3.5 w-3.5" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>{t('board.proposeForEndpoint')}</TooltipContent>
                          </Tooltip>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            )}
          </div>
          {endpointHasMore && (
            <div className="flex h-10 shrink-0 items-center justify-center border-t border-border">
              <Button type="button" variant="ghost" size="xs" onClick={() => void loadEndpoints(endpointNextCursor ?? undefined)} disabled={endpointsLoadingMore} className="text-muted-foreground">
                {endpointsLoadingMore ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('board.loadMore')}
              </Button>
            </div>
          )}
        </div>
      ) : (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-border/70 px-4 py-2">
            <Select value={factKind || 'all'} onValueChange={(value) => setFactKind(value === 'all' ? '' : value as WebApiBoardFactKind)}>
              <SelectTrigger aria-label={t('board.factKind')} className="h-8 w-[150px] text-[12px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{t('board.factKindAll')}</SelectItem>
                {FACT_KINDS.map((kind) => (
                  <SelectItem key={kind} value={kind}>{t(FACT_KIND_LABEL_KEYS[kind])}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <label className="flex cursor-pointer items-center gap-2 text-[12px] text-muted-foreground">
              <Switch checked={showRetracted} onCheckedChange={setShowRetracted} className="scale-75" />
              {t('board.factsShowRetracted')}
            </label>
            <span className="ml-auto text-[11px] tabular-nums text-muted-foreground">{facts.length}{factHasMore ? '+' : ''}</span>
          </div>
          <div className="min-h-0 flex-1 overflow-auto">
            {factsLoading && facts.length === 0 ? (
              <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
            ) : factsError ? (
              <div className="px-4 py-5 text-center text-sm text-destructive">{factsError}</div>
            ) : facts.length === 0 ? (
              <div className="px-4 py-5 text-center text-[13px] text-muted-foreground">{t('board.factsEmpty')}</div>
            ) : (
              <div>
                {facts.map((fact) => (
                  <div key={fact.id} className={cn('border-b border-border/70 px-4 py-3', fact.status === 'retracted' && 'opacity-60')}>
                    <div className="flex min-w-0 flex-wrap items-center gap-2">
                      <Badge size="sm" variant="secondary">{t(FACT_KIND_LABEL_KEYS[fact.kind] ?? 'board.factKindOther')}</Badge>
                      <span className={cn('min-w-0 truncate text-[13px] font-semibold', fact.status === 'retracted' && 'line-through decoration-1')}>{fact.subject}</span>
                      <Badge size="sm" variant={fact.confidence === 'confirmed' ? 'success' : 'muted'}>
                        {fact.confidence === 'confirmed' ? t('board.confidenceConfirmed') : t('board.confidenceInferred')}
                      </Badge>
                      {fact.status === 'retracted' && <Badge size="sm" variant="muted">{t('board.factRetracted')}</Badge>}
                      <span className="ml-auto shrink-0 text-[11px] tabular-nums text-muted-foreground">{formatTime(fact.updated_at)}</span>
                    </div>
                    <p className={cn('mt-1 whitespace-pre-wrap text-[13px] leading-relaxed', fact.status === 'retracted' ? 'text-muted-foreground line-through decoration-1' : 'text-foreground')}>
                      {fact.statement}
                    </p>
                    {fact.evidence_ids.length > 0 && (
                      <div className="mt-1.5 flex flex-wrap gap-1">
                        {fact.evidence_ids.map((evidenceId) => (
                          <Button
                            key={evidenceId}
                            type="button"
                            variant="ghost"
                            size="xs"
                            title={evidenceId}
                            onClick={() => onViewEvidence(evidenceId)}
                            className="max-w-[240px] truncate font-mono text-[11px] text-muted-foreground"
                          >
                            {t('board.viewEvidence', { id: evidenceId })}
                          </Button>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
          {factHasMore && (
            <div className="flex h-10 shrink-0 items-center justify-center border-t border-border">
              <Button type="button" variant="ghost" size="xs" onClick={() => void loadFacts(factNextCursor ?? undefined)} disabled={factsLoadingMore} className="text-muted-foreground">
                {factsLoadingMore ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('board.loadMore')}
              </Button>
            </div>
          )}
        </div>
      )}

      <ProposeOracleDialog
        open={proposeOpen}
        taskId={task.id}
        prefill={proposePrefill}
        onClose={() => setProposeOpen(false)}
        onProposed={handleOracleProposed}
      />
    </div>
  )
}

interface ProposeProps {
  open: boolean
  taskId: string
  prefill: string[]
  onClose: () => void
  onProposed: (oracle: WebApiBoardOracle, created: boolean) => void
}

function ProposeOracleDialog({ open, taskId, prefill, onClose, onProposed }: ProposeProps) {
  const { t } = useTranslation('chat')
  const [question, setQuestion] = useState('')
  const [category, setCategory] = useState<WebApiBoardCategory>('other')
  const [priority, setPriority] = useState<WebApiBoardPriority>('medium')
  const [selectedKeys, setSelectedKeys] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [options, setOptions] = useState<WebApiBoardEndpoint[]>([])
  const [manual, setManual] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  const [dedupe, setDedupe] = useState('')

  const prefillKey = prefill.join('\u0000')
  const optionSeq = useRef(0)

  useEffect(() => {
    if (!open) return
    setQuestion('')
    setCategory('other')
    setPriority('medium')
    setSelectedKeys(prefill)
    setSearch('')
    setManual('')
    setError('')
    setDedupe('')
    // prefill 以 key 比较：父级每次渲染都可能传新数组，不能直接做依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, prefillKey])

  useEffect(() => {
    if (!open) return
    const seq = ++optionSeq.current
    let cancelled = false
    const timer = setTimeout(() => {
      listWebApiBoardEndpoints(taskId, { q: search.trim() || undefined, limit: 20 })
        .then((result) => { if (!cancelled && seq === optionSeq.current) setOptions(result.items) })
        .catch(() => { if (!cancelled && seq === optionSeq.current) setOptions([]) })
    }, 200)
    return () => { cancelled = true; clearTimeout(timer) }
  }, [open, search, taskId])

  const addKey = (key: string) => {
    const value = key.trim()
    if (!value) return
    setSelectedKeys((current) => (current.includes(value) ? current : [...current, value]))
  }

  const addManual = () => {
    if (!manual.trim()) return
    addKey(manual)
    setManual('')
  }

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    if (submitting) return
    if (!question.trim()) {
      setError(t('board.requiredQuestion'))
      return
    }
    setSubmitting(true)
    setError('')
    setDedupe('')
    try {
      const result = await createWebApiBoardOracle(taskId, {
        question: question.trim(),
        category,
        priority,
        endpoint_keys: selectedKeys,
      })
      onProposed(result.oracle, result.created)
      if (result.created) onClose()
      else setDedupe(result.oracle.number)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !submitting) onClose() }}>
      <DialogContent
        closeLabel={t('board.cancel')}
        aria-describedby={undefined}
        className="flex max-h-[90dvh] w-[calc(100vw-2rem)] max-w-[560px] flex-col gap-0 overflow-hidden p-0"
      >
        <DialogHeaderRow title={t('board.proposeTitle')} />
        <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
          <Field label={t('board.question')} hint={t('board.questionHint')}>
            <Textarea
              value={question}
              onChange={(event) => setQuestion(event.target.value)}
              maxLength={500}
              rows={4}
              placeholder={t('board.questionPlaceholder')}
              disabled={submitting}
              autoFocus
            />
            <span className="block text-right text-[10px] tabular-nums text-muted-foreground">{question.length}/500</span>
          </Field>

          <div className="grid grid-cols-2 gap-3">
            <Field label={t('board.category')}>
              <Select value={category} onValueChange={(value) => setCategory(value as WebApiBoardCategory)} disabled={submitting}>
                <SelectTrigger className="h-9 text-[13px]"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {CATEGORIES.map((item) => <SelectItem key={item} value={item}>{t(CATEGORY_LABEL_KEYS[item])}</SelectItem>)}
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('board.fieldPriority')}>
              <Select value={priority} onValueChange={(value) => setPriority(value as WebApiBoardPriority)} disabled={submitting}>
                <SelectTrigger className="h-9 text-[13px]"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {(['high', 'medium', 'low'] as WebApiBoardPriority[]).map((item) => (
                    <SelectItem key={item} value={item}>{t(PRIORITY_LABEL_KEYS[item])}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          </div>

          <div className="space-y-1.5">
            <span className="block text-[13px] font-medium text-foreground/75">{t('board.endpointsLabel')}</span>
            {selectedKeys.length > 0 && (
              <div className="flex flex-wrap gap-1.5">
                {selectedKeys.map((key) => (
                  <span key={key} title={key} className="inline-flex max-w-full items-center gap-1.5 rounded-full border border-primary/40 bg-primary/15 px-2.5 py-1 text-[11px] font-medium text-primary">
                    <span className="max-w-[360px] truncate font-mono">{key}</span>
                    <button
                      type="button"
                      onClick={() => setSelectedKeys((current) => current.filter((item) => item !== key))}
                      aria-label={t('board.removeEndpoint', { key })}
                      className="shrink-0 rounded-full p-0.5 hover:bg-primary/20"
                    >
                      <X className="h-3 w-3" />
                    </button>
                  </span>
                ))}
              </div>
            )}
            <div className="relative">
              <Search aria-hidden className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('board.endpointSearch')}
                className="h-9 pl-8"
                disabled={submitting}
              />
            </div>
            {search.trim() && (
              <div className="max-h-40 overflow-auto rounded-md border border-border bg-background">
                {options.length === 0 ? (
                  <p className="px-3 py-2 text-[12px] text-muted-foreground">{t('board.endpointNoMatch')}</p>
                ) : options.map((option) => (
                  <button
                    key={option.id}
                    type="button"
                    onClick={() => addKey(option.endpoint_key)}
                    disabled={selectedKeys.includes(option.endpoint_key)}
                    className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[12px] hover:bg-muted/50 disabled:opacity-40"
                  >
                    <span className={cn('w-12 shrink-0 font-mono text-[10px] font-semibold', METHOD_TONE[option.method.toUpperCase()] ?? 'text-muted-foreground')}>{option.method}</span>
                    <span className="min-w-0 flex-1 truncate font-mono" title={option.endpoint_key}>{option.endpoint_key}</span>
                  </button>
                ))}
              </div>
            )}
            <div className="flex items-center gap-1.5">
              <Input
                value={manual}
                onChange={(event) => setManual(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') { event.preventDefault(); addManual() }
                }}
                placeholder={t('board.endpointManualPlaceholder')}
                className="h-9 flex-1 font-mono text-[12px]"
                disabled={submitting}
              />
              <Button type="button" variant="outline" size="sm" onClick={addManual} disabled={submitting || !manual.trim()}>
                {t('board.addEndpoint')}
              </Button>
            </div>
          </div>

          {dedupe && <Callout tone="warning">{t('board.dedupeNotice', { number: dedupe })}</Callout>}
          {error && <Callout tone="destructive">{error}</Callout>}

        </div>
        <DialogFooterRow>
          <Button type="button" variant="outline" onClick={onClose} disabled={submitting}>{t('board.cancel')}</Button>
          <Button type="submit" disabled={submitting}>
            {submitting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('board.submit')}
          </Button>
        </DialogFooterRow>
        </form>
      </DialogContent>
    </Dialog>
  )
}
