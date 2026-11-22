import { Fragment, useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight, ExternalLink, HelpCircle, Loader2, RefreshCw } from 'lucide-react'
import {
  Button, EmptyState, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
  Spinner, Tooltip, TooltipContent, TooltipTrigger,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { FilterPills, Page, PageHeader, TableCard, type FilterPillItem } from './PageFrame'
import { listWebApiAssets, listWebApiAudit, type WebApiAsset, type WebApiAuditEvent } from '../api'
import { formatShortDateTimeSeconds } from '../lib/time'

interface AuditPageProps {
  webapiEnabled: boolean
  onOpenTask: (taskId: string) => void
  onOpenAsset: (assetId: string) => void
  onOpenMenu: () => void
}

type ActorFilter = '' | 'user' | 'system'

const PAGE_LIMIT = 50
const ALL_OPTION = '__all__'
const ACTOR_ALL = '__all__'

// 动作类别：全部 + 后端 action 前缀（native/AUDIT.md）。
const ACTION_CATEGORIES = [
  { value: '', labelKey: 'shell.auditCategoryAll' },
  { value: 'task.', labelKey: 'shell.auditCategoryTask' },
  { value: 'verification.', labelKey: 'shell.auditCategoryVerification' },
  { value: 'retest.', labelKey: 'shell.auditCategoryRetest' },
  { value: 'report.', labelKey: 'shell.auditCategoryReport' },
  { value: 'approval.', labelKey: 'shell.auditCategoryApproval' },
  { value: 'asset.', labelKey: 'shell.auditCategoryAsset' },
  { value: 'credential.', labelKey: 'shell.auditCategoryCredential' },
  { value: 'schedule.', labelKey: 'shell.auditCategorySchedule' },
  { value: 'vuln.', labelKey: 'shell.auditCategoryVuln' },
  { value: 'notify.', labelKey: 'shell.auditCategoryNotify' },
  { value: 'notification.', labelKey: 'shell.auditCategoryNotification' },
]

// 每个登记动作的中文/英文名（覆盖 native/AUDIT.md 登记点表；另含后端已有但文档表外的 task.scenario / board.oracle.*）。
const ACTION_LABELS: Record<string, string> = {
  'task.create': 'shell.auditActionTaskCreate',
  'task.policy': 'shell.auditActionTaskPolicy',
  'task.scenario': 'shell.auditActionTaskScenario',
  'verification.start': 'shell.auditActionVerificationStart',
  'retest.start': 'shell.auditActionRetestStart',
  'report.start': 'shell.auditActionReportStart',
  'approval.approve': 'shell.auditActionApprovalApprove',
  'approval.deny': 'shell.auditActionApprovalDeny',
  'approval.expire': 'shell.auditActionApprovalExpire',
  'asset.create': 'shell.auditActionAssetCreate',
  'asset.update': 'shell.auditActionAssetUpdate',
  'asset.archive': 'shell.auditActionAssetArchive',
  'asset.unarchive': 'shell.auditActionAssetUnarchive',
  'credential.save': 'shell.auditActionCredentialSave',
  'credential.delete': 'shell.auditActionCredentialDelete',
  'schedule.save': 'shell.auditActionScheduleSave',
  'schedule.delete': 'shell.auditActionScheduleDelete',
  'schedule.run': 'shell.auditActionScheduleRun',
  'vuln.rename': 'shell.auditActionVulnRename',
  'vuln.move': 'shell.auditActionVulnMove',
  'vuln.merge': 'shell.auditActionVulnMerge',
  'vuln.triage': 'shell.auditActionVulnTriage',
  'vuln.sla': 'shell.auditActionVulnSla',
  'vuln.curl_unmasked': 'shell.auditActionVulnCurlUnmasked',
  'vuln.replay': 'shell.auditActionVulnReplay',
  'vuln.retest_request': 'shell.auditActionVulnRetestRequest',
  'vuln.notify': 'shell.auditActionVulnNotify',
  'notify.settings': 'shell.auditActionNotifySettings',
  'notify.test': 'shell.auditActionNotifyTest',
  'notification.confirm': 'shell.auditActionNotificationConfirm',
  'notification.dismiss': 'shell.auditActionNotificationDismiss',
  'notification.retry': 'shell.auditActionNotificationRetry',
  'notification.sent': 'shell.auditActionNotificationSent',
  'notification.failed': 'shell.auditActionNotificationFailed',
  'board.oracle.create': 'shell.auditActionBoardOracleCreate',
  'board.oracle.update': 'shell.auditActionBoardOracleUpdate',
}

const TARGET_KIND_LABELS: Record<string, string> = {
  task: 'shell.auditTargetTask',
  operation: 'shell.auditTargetOperation',
  verification: 'shell.auditTargetVerification',
  retest: 'shell.auditTargetRetest',
  report: 'shell.auditTargetReport',
  asset: 'shell.auditTargetAsset',
  credential: 'shell.auditTargetCredential',
  schedule: 'shell.auditTargetSchedule',
  vuln: 'shell.auditTargetVuln',
  vuln_sla: 'shell.auditTargetVulnSla',
  notify: 'shell.auditTargetNotify',
  notification: 'shell.auditTargetNotification',
  board_oracle: 'shell.auditTargetBoardOracle',
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** 本地日期字符串（YYYY-MM-DD）可直接按字典序比较。 */
function dateRangeInvalid(from: string, to: string): boolean {
  return Boolean(from && to && to < from)
}

function ActorBadge({ actor }: { actor: WebApiAuditEvent['actor'] }) {
  const { t } = useTranslation('chat')
  const isUser = actor === 'user'
  return (
    <span className={cn('ws-st-badge', isUser ? 'bg-primary/10 text-primary' : 'ws-st-unconfirmed')}>
      {!isUser && <span aria-hidden className="ws-st-dot" />}
      {t(isUser ? 'shell.auditActorUser' : 'shell.auditActorSystem')}
    </span>
  )
}

export default function AuditPage({ webapiEnabled, onOpenTask, onOpenAsset, onOpenMenu }: AuditPageProps) {
  const { t } = useTranslation('chat')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [actor, setActor] = useState<ActorFilter>('')
  const [category, setCategory] = useState('')
  const [assetId, setAssetId] = useState('')
  const [assets, setAssets] = useState<WebApiAsset[]>([])
  const [assetError, setAssetError] = useState('')
  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')
  const [items, setItems] = useState<WebApiAuditEvent[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const seq = useRef(0)
  const cursorRef = useRef<string | null>(null)
  const invalidRange = dateRangeInvalid(fromDate, toDate)

  // 搜索防抖 300ms：输入停顿后才传 q（同任务/漏洞页）。
  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchInput.trim()), 300)
    return () => window.clearTimeout(timer)
  }, [searchInput])

  const loadAssets = useCallback(async () => {
    if (!webapiEnabled) return
    try {
      const next = await listWebApiAssets(false)
      setAssets(next)
      setAssetError('')
    } catch (err) {
      setAssetError(errorText(err))
    }
  }, [webapiEnabled])

  useEffect(() => { void loadAssets() }, [loadAssets])

  const loadAudit = useCallback(async (opts: { reset: boolean }) => {
    if (!webapiEnabled) return
    if (dateRangeInvalid(fromDate, toDate)) {
      ++seq.current
      setLoading(false)
      setLoadingMore(false)
      return
    }
    const nextCursor = opts.reset ? undefined : cursorRef.current || undefined
    if (!opts.reset && !nextCursor) return
    const current = ++seq.current
    if (opts.reset) setLoading(true)
    else setLoadingMore(true)
    try {
      const data = await listWebApiAudit({
        action: category || undefined,
        actor: actor || undefined,
        asset_id: assetId || undefined,
        q: query || undefined,
        from: fromDate || undefined,
        to: toDate || undefined,
        limit: PAGE_LIMIT,
        cursor: nextCursor,
      })
      if (current !== seq.current) return
      cursorRef.current = data.next_cursor
      setHasMore(data.has_more)
      setItems((prev) => (opts.reset ? data.items : [...prev, ...data.items]))
      setError('')
    } catch (err) {
      if (current !== seq.current) return
      setError(errorText(err))
    } finally {
      if (current === seq.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    }
  }, [webapiEnabled, actor, category, assetId, query, fromDate, toDate])

  // 任一筛选变化重置游标并回到第一页。
  useEffect(() => {
    if (!webapiEnabled) { setLoading(false); return }
    cursorRef.current = null
    setExpandedId(null)
    void loadAudit({ reset: true })
  }, [loadAudit, webapiEnabled])

  const refresh = () => {
    void loadAssets()
    cursorRef.current = null
    setExpandedId(null)
    void loadAudit({ reset: true })
  }

  const actorItems: FilterPillItem<ActorFilter>[] = [
    { key: '', label: t('shell.filterAll') },
    { key: 'user', label: t('shell.auditActorUser') },
    { key: 'system', label: t('shell.auditActorSystem') },
  ]
  const filtered = actor !== '' || category !== '' || assetId !== '' || fromDate !== '' || toDate !== '' || query !== ''

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page>
        <PageHeader
          title={t('shell.navAudit')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <Button type="button" variant="outline" size="sm" className="h-9" onClick={refresh} disabled={loading}>
              <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')} />
              {t('shell.refresh')}
            </Button>
          )}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label={t('shell.auditHelp')}
                className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <HelpCircle className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px] whitespace-normal text-left leading-relaxed">
              {t('shell.auditHelp')}
            </TooltipContent>
          </Tooltip>
        </PageHeader>

        {/* 筛选行：操作者胶囊 + 动作类别；右侧资产、日期、搜索。 */}
        <div className="flex flex-wrap items-center gap-3">
          <FilterPills items={actorItems} value={actor} onChange={setActor} />
          <Select
            value={category || ALL_OPTION}
            onValueChange={(value) => setCategory(value === ALL_OPTION ? '' : value)}
          >
            <SelectTrigger aria-label={t('shell.auditCategoryFilter')} className="h-9 w-[140px] shrink-0 bg-card">
              <SelectValue placeholder={t('shell.auditCategoryAll')} />
            </SelectTrigger>
            <SelectContent>
              {ACTION_CATEGORIES.map((item) => (
                <SelectItem key={item.value || ALL_OPTION} value={item.value || ALL_OPTION}>
                  {t(item.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <Select value={assetId || ALL_OPTION} onValueChange={(value) => setAssetId(value === ALL_OPTION ? '' : value)}>
              <SelectTrigger aria-label={t('shell.auditAssetFilter')} className="h-9 w-[160px] shrink-0 bg-card">
                <SelectValue placeholder={t('shell.auditAssetAll')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_OPTION}>{t('shell.auditAssetAll')}</SelectItem>
                {assets.map((asset) => (
                  <SelectItem key={asset.id} value={asset.id}>{asset.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <label className="flex items-center gap-1.5 text-[12px] text-muted-foreground">
              <span className="shrink-0">{t('shell.auditFromDate')}</span>
              <Input
                type="date"
                value={fromDate}
                max={toDate || undefined}
                onChange={(event) => setFromDate(event.target.value)}
                aria-label={t('shell.auditFromDate')}
                className="h-9 w-[150px] shrink-0 bg-card px-2"
              />
            </label>
            <label className="flex items-center gap-1.5 text-[12px] text-muted-foreground">
              <span className="shrink-0">{t('shell.auditToDate')}</span>
              <Input
                type="date"
                value={toDate}
                min={fromDate || undefined}
                onChange={(event) => setToDate(event.target.value)}
                aria-label={t('shell.auditToDate')}
                className="h-9 w-[150px] shrink-0 bg-card px-2"
              />
            </label>
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              placeholder={t('shell.auditSearch')}
              aria-label={t('shell.auditSearch')}
              className="h-9 w-[200px] bg-card"
            />
          </div>
        </div>
        {(error || assetError) && (
          <p role="status" className="text-[12px] text-warning">{t('shell.loadFailed')}：{error || assetError}</p>
        )}
        {invalidRange && (
          <p role="status" className="text-[12px] text-warning">{t('shell.auditDateInvalid')}</p>
        )}
        <TableCard
          footer={(
            <>
              <span className="text-[12px] text-muted-foreground">{t('shell.auditShown', { count: items.length })}</span>
              {hasMore && !invalidRange && (
                <Button type="button" variant="outline" size="sm" className="ml-auto h-8" disabled={loadingMore} onClick={() => void loadAudit({ reset: false })}>
                  {loadingMore && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
                  {t('shell.loadMore')}
                </Button>
              )}
            </>
          )}
        >
          {loading ? (
            <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('shell.loading')}
            </div>
          ) : items.length === 0 ? (
            <EmptyState title={invalidRange ? t('shell.auditDateInvalid') : filtered ? t('shell.auditFilterEmpty') : t('shell.auditEmpty')} />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[960px] table-fixed border-collapse text-[13px] min-[1440px]:min-w-0">
                <colgroup>
                  <col className="w-[132px]" />
                  <col className="w-[76px]" />
                  <col className="w-[170px]" />
                  <col />
                  <col className="w-[240px]" />
                  <col className="w-[56px]" />
                </colgroup>
                <thead>
                  <tr className="ws-table-head h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.auditColTime')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.auditColActor')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.auditColAction')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.auditColSummary')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.auditColTarget')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.auditColActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => {
                    const expanded = expandedId === item.id
                    const actionLabel = ACTION_LABELS[item.action]
                    const targetKindLabel = TARGET_KIND_LABELS[item.target_kind]
                    return (
                      <Fragment key={item.id}>
                        <tr className="border-b border-border/70 transition-colors hover:bg-muted/40">
                          <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[12px] tabular text-muted-foreground">
                            {formatShortDateTimeSeconds(item.at) || '—'}
                          </td>
                          <td className="px-3 py-2.5">
                            <ActorBadge actor={item.actor} />
                          </td>
                          <td className="min-w-0 px-3 py-2.5">
                            {actionLabel
                              ? <span className="block truncate text-[12px] text-foreground" title={item.action}>{t(actionLabel)}</span>
                              : <span className="block truncate font-mono text-[11px] text-secondary-foreground" title={item.action}>{item.action}</span>}
                          </td>
                          <td className="min-w-0 px-3 py-2.5">
                            <span className="block truncate text-[13px] text-foreground" title={item.summary}>{item.summary}</span>
                          </td>
                          <td className="min-w-0 px-3 py-2.5">
                            <span className="block truncate text-[12px] text-secondary-foreground" title={`${item.target_kind} ${item.target_id}`}>
                              {targetKindLabel ? t(targetKindLabel) : (item.target_kind || '—')}
                              {item.target_id && (
                                <span className="ml-1.5 font-mono text-[11px] text-muted-foreground">{item.target_id}</span>
                              )}
                            </span>
                          </td>
                          <td className="px-3 py-2.5">
                            <div className="flex justify-end">
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-xs"
                                aria-label={expanded ? t('shell.auditCollapse') : t('shell.auditExpand')}
                                aria-expanded={expanded}
                                onClick={() => setExpandedId(expanded ? null : item.id)}
                                className="text-muted-foreground"
                              >
                                {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
                              </Button>
                            </div>
                          </td>
                        </tr>
                        {expanded && (
                          <tr key={`${item.id}-detail`} className="border-b border-border/70 bg-muted/20">                            <td colSpan={6} className="px-4 py-3">
                              <div className="space-y-2">
                                <div className="flex flex-wrap items-center gap-2">
                                  {item.task_id && (
                                    <Button type="button" variant="outline" size="sm" className="h-8 gap-1" onClick={() => onOpenTask(item.task_id)}>
                                      <ExternalLink className="h-3.5 w-3.5" />
                                      {t('shell.auditOpenTask')}
                                    </Button>
                                  )}
                                  {item.asset_id && (
                                    <Button type="button" variant="outline" size="sm" className="h-8 gap-1" onClick={() => onOpenAsset(item.asset_id)}>
                                      <ExternalLink className="h-3.5 w-3.5" />
                                      {t('shell.auditOpenAsset')}
                                    </Button>
                                  )}
                                  <span className="font-mono text-[11px] text-muted-foreground" title={item.id}>{item.id}</span>
                                </div>
                                <pre className="max-h-[320px] overflow-auto whitespace-pre-wrap break-all rounded-md border border-border/60 bg-[var(--bg-code-light)] px-2 py-1.5 font-mono text-[12px] leading-[1.5] text-foreground">
                                  {JSON.stringify(item.detail, null, 2)}
                                </pre>
                              </div>
                            </td>
                          </tr>
                        )}
                      </Fragment>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </TableCard>
      </Page>
    </div>
  )
}
