import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  EmptyState,
  Input,
  SkeletonList,
  useConfirm,
} from '@cyber/ui'
import { ChevronDown, ChevronRight, GitMerge, Loader2, Pencil, RefreshCw } from 'lucide-react'
import { cn } from '@cyber/theme'
import { newID } from '@cyber/aop'
import {
  listWebApiAssetVulns,
  mergeWebApiVulns,
  moveWebApiVulnMember,
  saveWebApiVulnTitle,
  type WebApiVuln,
  type WebApiVulnList,
  type WebApiVulnMember,
  type WebApiVulnStatus,
} from '../api'
import { formatTime } from './WebApiEvidenceReference'
import { RetestBadge, StatusBadge, VerdictBadge } from './ui-badges'

interface Props {
  assetId: string
  onOpenTask: (sessionID: string) => void
  onCounts: (counts: Record<WebApiVulnStatus, number>) => void
  /** 从全局漏洞页跳入时定位并展开该条目；消费后回调清空。 */
  focusVulnId?: string | null
  onFocusConsumed?: () => void
}

type Filter = 'all' | WebApiVulnStatus

const EMPTY_COUNTS: Record<WebApiVulnStatus, number> = { new: 0, present: 0, pending: 0, unconfirmed: 0, fixed: 0 }

const STATUS_ORDER: WebApiVulnStatus[] = ['new', 'present', 'pending', 'unconfirmed', 'fixed']

const STATUS_META: Record<WebApiVulnStatus, { key: string }> = {
  new: { key: 'webapi.vulnStatusNew' },
  present: { key: 'webapi.vulnStatusPresent' },
  pending: { key: 'webapi.vulnStatusPending' },
  unconfirmed: { key: 'webapi.vulnStatusUnconfirmed' },
  fixed: { key: 'webapi.vulnStatusFixed' },
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

const VERDICT_KEYS: Record<string, string> = {
  supported: 'webapi.verdict_supported',
  counterevidence: 'webapi.verdict_counterevidence',
  inconclusive: 'webapi.verdict_inconclusive',
}

const RETEST_KEYS: Record<string, string> = {
  reproducible: 'webapi.retestReproducible',
  fixed_in_scope: 'webapi.retestFixedInScope',
  inconclusive: 'webapi.retestInconclusive',
}

function VulnStatusBadge({ status }: { status: WebApiVulnStatus }) {
  return <StatusBadge status={status} className="shrink-0" />
}

export default function WebApiAssetVulns({ assetId, onOpenTask, onCounts, focusVulnId, onFocusConsumed }: Props) {
  const { t } = useTranslation('chat')
  const confirm = useConfirm()
  const [list, setList] = useState<WebApiVulnList | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [titleSaving, setTitleSaving] = useState(false)
  const [titleError, setTitleError] = useState('')
  const [actionError, setActionError] = useState('')
  const [acting, setActing] = useState(false)
  const loadSeq = useRef(0)
  const containerRef = useRef<HTMLDivElement>(null)

  // nextExpand：undefined 保留当前展开（条目已不存在则收起）；null 全部收起；字符串展开该条目。
  const load = useCallback(async (nextExpand?: string | null) => {
    const seq = ++loadSeq.current
    setLoading(true)
    setError('')
    try {
      const next = await listWebApiAssetVulns(assetId)
      if (seq !== loadSeq.current) return
      setList(next)
      onCounts(next.counts)
      const wanted = nextExpand === undefined
        ? expandedId
        : nextExpand
      setExpandedId(wanted && next.vulns.some((item) => item.id === wanted) ? wanted : null)
    } catch (err) {
      if (seq !== loadSeq.current) return
      setError(errorText(err))
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [assetId, expandedId, onCounts])

  // 只在挂载与显式刷新/操作后加载，不轮询。
  const loadRef = useRef(load)
  loadRef.current = load
  useEffect(() => {
    setList(null)
    setExpandedId(null)
    setFilter('all')
    setRenaming(false)
    void loadRef.current(null)
  }, [assetId])

  // 外部定位（全局漏洞页 → 资产漏洞库）：列表就绪后展开该条目并滚到可见区。
  // 条目已不存在时也消费定位，避免父级状态悬留。
  useEffect(() => {
    if (!focusVulnId || !list) return
    if (!list.vulns.some((item) => item.id === focusVulnId)) {
      onFocusConsumed?.()
      return
    }
    setFilter('all')
    setExpandedId(focusVulnId)
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const element = containerRef.current?.querySelector<HTMLElement>(`[data-vuln-id="${CSS.escape(focusVulnId)}"]`)
        element?.scrollIntoView({ block: 'nearest' })
      })
    })
    onFocusConsumed?.()
  }, [focusVulnId, list, onFocusConsumed])

  const counts = list?.counts ?? EMPTY_COUNTS
  const total = STATUS_ORDER.reduce((sum, status) => sum + counts[status], 0)
  const visible = list ? list.vulns.filter((item) => filter === 'all' || item.status === filter) : []

  const refresh = () => {
    setRenaming(false)
    void load()
  }

  const toggleExpanded = (id: string) => {
    setRenaming(false)
    setTitleError('')
    setExpandedId((current) => (current === id ? null : id))
  }

  const startRename = (vuln: WebApiVuln) => {
    setRenaming(true)
    setTitleDraft(vuln.title)
    setTitleError('')
  }

  const saveTitle = async (vuln: WebApiVuln) => {
    if (titleSaving) return
    setTitleSaving(true)
    setTitleError('')
    try {
      await saveWebApiVulnTitle(assetId, vuln.id, titleDraft.trim())
      setRenaming(false)
      setActionError('')
      await load(vuln.id)
    } catch (err) {
      setTitleError(errorText(err))
    } finally {
      setTitleSaving(false)
    }
  }

  const moveMember = async (from: WebApiVuln, member: WebApiVulnMember, targetId: string | null) => {
    if (acting) return
    setActing(true)
    setActionError('')
    try {
      await moveWebApiVulnMember(assetId, targetId ?? newID(), member.finding_id)
      await load(from.id)
    } catch (err) {
      setActionError(errorText(err))
    } finally {
      setActing(false)
    }
  }

  const mergeVuln = async (from: WebApiVuln, into: WebApiVuln) => {
    if (acting) return
    const ok = await confirm({
      description: t('webapi.vulnMergeConfirm', { from: from.title, to: into.title }),
    })
    if (!ok) return
    setActing(true)
    setActionError('')
    try {
      await mergeWebApiVulns(assetId, into.id, from.id)
      setRenaming(false)
      await load(into.id)
    } catch (err) {
      setActionError(errorText(err))
    } finally {
      setActing(false)
    }
  }

  const filterLabel = (value: Filter) => (value === 'all' ? t('webapi.vulnFilterAll') : t(STATUS_META[value].key))
  const filterCount = (value: Filter) => (value === 'all' ? total : counts[value])

  return (
    <div className="vuln-detail-container" ref={containerRef}>
      <div className="mb-3 flex items-center gap-2">
        <div className="min-w-0 flex-1 overflow-x-auto">
          <div role="group" aria-label={t('webapi.vulnTab')} className="flex w-max shrink-0 items-center gap-0.5 rounded-md border border-border/60 p-0.5">
            {(['all', ...STATUS_ORDER] as Filter[]).map((value) => (
              <button
                key={value}
                type="button"
                aria-pressed={filter === value}
                onClick={() => setFilter(value)}
                className={cn(
                  'flex h-6 shrink-0 items-center whitespace-nowrap rounded px-2 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                  filter === value ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
                )}
              >
                {filterLabel(value)}
                <span className="ml-1 font-mono text-[11px] tabular-nums">{filterCount(value)}</span>
              </button>
            ))}
          </div>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={refresh}
          disabled={loading}
          aria-label={t('webapi.vulnRefresh')}
          className="shrink-0 text-muted-foreground"
        >
          <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
        </Button>
      </div>

      {error && (
        <div className="mb-2 flex items-center gap-2 text-[12px] text-destructive">
          <span className="min-w-0 truncate" title={error}>{error}</span>
          <Button type="button" variant="ghost" size="xs" onClick={refresh} className="shrink-0">{t('webapi.vulnRetry')}</Button>
        </div>
      )}
      {actionError && <p role="alert" className="mb-2 text-[12px] text-destructive">{actionError}</p>}

      {list === null ? (
        loading ? <SkeletonList count={5} className="p-3" /> : null
      ) : list.vulns.length === 0 ? (
        <EmptyState title={t('webapi.vulnEmptyTitle')} compact />
      ) : visible.length === 0 ? (
        <p className="px-3 py-2 text-[12px] text-muted-foreground">{t('webapi.vulnFilterEmpty')}</p>
      ) : (
        <div>
          {visible.map((vuln) => {
            const expanded = expandedId === vuln.id
            const endpoint = !vuln.endpoint_key || vuln.endpoint_key.startsWith('finding:')
              ? t('webapi.vulnNoEndpoint')
              : vuln.endpoint_key
            const meta = [endpoint, t('webapi.vulnFirstSeen', { time: formatTime(vuln.first_seen_at) })]
            if (vuln.status === 'fixed' && vuln.fixed_at) {
              meta.push(t('webapi.vulnFixedAt', { time: formatTime(vuln.fixed_at) }))
            } else if (vuln.last_confirmed_at) {
              meta.push(t('webapi.vulnLastConfirmed', { time: formatTime(vuln.last_confirmed_at) }))
            }
            return (
              <div key={vuln.id} data-vuln-id={vuln.id}>
                <button
                  type="button"
                  onClick={() => toggleExpanded(vuln.id)}
                  aria-expanded={expanded}
                  className={cn(
                    'flex w-full min-w-0 flex-col gap-1 border-b border-border/60 px-3 py-2 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                    expanded && 'bg-muted/30',
                  )}
                >
                  <span className="flex w-full min-w-0 items-center gap-2">
                    <ChevronRight className={cn('h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform', expanded && 'rotate-90')} aria-hidden="true" />
                    <VulnStatusBadge status={vuln.status} />
                    <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-foreground" title={vuln.title}>{vuln.title}</span>
                    <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
                      {t('webapi.vulnSummary', { members: vuln.member_count, tasks: vuln.task_count })}
                    </span>
                  </span>
                  <span className="mt-0.5 truncate pl-[22px] font-mono text-[11px] text-muted-foreground">
                    {meta.join(' · ')}
                  </span>
                </button>

                {expanded && (
                  <div className="border-b border-border/60 bg-muted/30 pb-3 pl-[34px] pr-3 pt-1">
                    <div className="flex flex-wrap items-center gap-2 text-[12px]">
                      <Button type="button" variant="ghost" size="xs" onClick={() => startRename(vuln)} className="gap-1 text-[12px] text-muted-foreground">
                        <Pencil className="h-3.5 w-3.5" />
                        {t('webapi.vulnRename')}
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button type="button" variant="ghost" size="xs" disabled={acting || list.vulns.length <= 1} className="gap-1 text-[12px] text-muted-foreground">
                            <GitMerge className="h-3.5 w-3.5" />
                            {t('webapi.vulnMergeInto')}
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start">
                          {list.vulns.filter((item) => item.id !== vuln.id).map((target) => (
                            <DropdownMenuItem key={target.id} onSelect={() => void mergeVuln(vuln, target)}>
                              <VulnStatusBadge status={target.status} />
                              <span className="ml-2 max-w-[280px] truncate">{target.title}</span>
                            </DropdownMenuItem>
                          ))}
                        </DropdownMenuContent>
                      </DropdownMenu>
                      {vuln.last_event && (
                        <span className="ml-auto text-[11px] text-muted-foreground">
                          {t('webapi.vulnLastResult')}
                          {vuln.last_event.kind === 'verify'
                            ? `${t('webapi.vulnVerify')} ${VERDICT_KEYS[vuln.last_event.value] ? t(VERDICT_KEYS[vuln.last_event.value]) : vuln.last_event.value}`
                            : `${t('webapi.vulnRetest')} ${RETEST_KEYS[vuln.last_event.value] ? t(RETEST_KEYS[vuln.last_event.value]) : vuln.last_event.value}`}
                          {' · '}{formatTime(vuln.last_event.at)}
                        </span>
                      )}
                    </div>

                    {renaming && (
                      <div className="mt-2 flex flex-wrap items-center gap-2">
                        <Input
                          value={titleDraft}
                          onChange={(event) => setTitleDraft(event.target.value)}
                          maxLength={200}
                          className="h-7 max-w-[480px] text-[13px]"
                          disabled={titleSaving}
                        />
                        <Button type="button" size="xs" onClick={() => void saveTitle(vuln)} disabled={titleSaving}>
                          {titleSaving && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                          {t('webapi.vulnSave')}
                        </Button>
                        <Button type="button" variant="ghost" size="xs" onClick={() => setRenaming(false)} disabled={titleSaving} className="text-muted-foreground">{t('webapi.vulnCancel')}</Button>
                        <span className="text-[11px] text-muted-foreground">{t('webapi.vulnTitleAutoHint')}</span>
                        {titleError && <span role="alert" className="text-[12px] text-destructive">{titleError}</span>}
                      </div>
                    )}

                    <table className="mt-2 w-full table-fixed text-[12px]">
                      <thead>
                        <tr className="text-left text-muted-foreground">
                          <th className="py-1 pr-2 font-normal">{t('webapi.vulnColCandidate')}</th>
                          <th className="vuln-narrow-hide w-[120px] py-1 pr-2 font-normal">{t('webapi.vulnColFound')}</th>
                          <th className="w-[80px] py-1 pr-2 font-normal">{t('webapi.vulnColVerify')}</th>
                          <th className="w-[120px] py-1 pr-2 font-normal">{t('webapi.vulnColRetest')}</th>
                          <th className="vuln-narrow-hide w-[48px] py-1 pr-2 font-normal">{t('webapi.vulnColLink')}</th>
                          <th className="w-[96px] py-1 text-right font-normal">{t('webapi.vulnColActions')}</th>
                        </tr>
                      </thead>
                      <tbody>
                        {vuln.members.map((member) => (
                          <tr key={member.finding_id} className="h-8 border-t border-border/40">
                            <td className="min-w-0 py-1 pr-2">
                              <div className="flex min-w-0 items-baseline gap-1">
                                <span className="shrink-0 font-mono text-[11px] text-muted-foreground">{member.number}</span>
                                <span className="min-w-0 truncate" title={member.title}>{member.title}</span>
                              </div>
                            </td>
                            <td className="vuln-narrow-hide truncate py-1 pr-2 text-muted-foreground">{formatTime(member.created_at)}</td>
                            <td className="truncate py-1 pr-2">
                              <VerdictBadge verdict={member.latest_verdict} showModelLabel={false} />
                            </td>
                            <td className="truncate py-1 pr-2">
                              <RetestBadge conclusion={member.latest_retest} />
                            </td>
                            <td className="vuln-narrow-hide py-1 pr-2 text-muted-foreground">
                              {member.link === 'manual' ? t('webapi.vulnLinkManual') : t('webapi.vulnLinkAuto')}
                            </td>
                            <td className="whitespace-nowrap py-1 text-right">
                              <Button type="button" variant="link" size="xs" onClick={() => onOpenTask(member.initial_session_id)} className="h-6 px-1 text-[12px]">
                                {t('webapi.vulnView')}
                              </Button>
                              <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                  <Button type="button" variant="ghost" size="xs" disabled={acting} className="h-6 gap-0.5 px-1 text-[12px] text-muted-foreground">
                                    {t('webapi.vulnMove')}
                                    <ChevronDown className="h-3 w-3" />
                                  </Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent align="end">
                                  {list.vulns.filter((item) => item.id !== vuln.id).map((target) => (
                                    <DropdownMenuItem key={target.id} disabled={acting} onSelect={() => void moveMember(vuln, member, target.id)}>
                                      <VulnStatusBadge status={target.status} />
                                      <span className="ml-2 max-w-[280px] truncate">{target.title}</span>
                                    </DropdownMenuItem>
                                  ))}
                                  {list.vulns.length > 1 && <DropdownMenuSeparator />}
                                  <DropdownMenuItem disabled={acting} onSelect={() => void moveMember(vuln, member, null)}>
                                    {t('webapi.vulnSplit')}
                                  </DropdownMenuItem>
                                </DropdownMenuContent>
                              </DropdownMenu>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
