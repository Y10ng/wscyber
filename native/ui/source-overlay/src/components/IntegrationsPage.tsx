import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ExternalLink, HelpCircle, Inbox, Loader2, RefreshCw } from 'lucide-react'
import {
  Button, Callout, Field, Input, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Spinner, Switch,
  Tooltip, TooltipContent, TooltipTrigger,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { FilterPills, Page, PageHeader, type FilterPillItem } from './PageFrame'
import { IntegrationStatusBadge, NotificationStatusBadge } from './ui-badges'
import { formatShortDateTime } from '../lib/time'
import { usePolling } from '../hooks/usePolling'
import {
  actWebApiNotification, APIError, getWebApiWecomSettings, listWebApiNotifications,
  saveWebApiWecomSettings, testWebApiWecom,
  type WebApiNotification, type WebApiNotificationKind, type WebApiNotificationStatus, type WebApiNotificationStatusCounts, type WebApiWecomSettings,
} from '../api'

const PAGE_LIMIT = 50

type NotificationFilter = 'all' | WebApiNotificationStatus
type NotificationKindFilter = 'all' | 'vuln' | 'approval_pending' | 'schedule_failed' | 'weekly_digest'

const KIND_LABEL_KEYS: Record<WebApiNotificationKind, string> = {
  new: 'shell.integrationsKindNew',
  regression: 'shell.integrationsKindRegression',
  manual: 'shell.integrationsKindManual',
  approval_pending: 'shell.integrationsKindApprovalPending',
  schedule_failed: 'shell.integrationsKindScheduleFailed',
  weekly_digest: 'shell.integrationsKindWeeklyDigest',
}

const VULN_KINDS: WebApiNotificationKind[] = ['new', 'regression', 'manual']

function matchesKindFilter(kind: WebApiNotificationKind, filter: NotificationKindFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'vuln') return VULN_KINDS.includes(kind)
  return kind === filter
}

interface IntegrationsPageProps {
  onOpenSession: (sessionID: string) => void
  onOpenMenu: () => void
  /** 总览“待确认推送”跳转：进入页面时预置推送记录筛选。 */
  presetFilter?: NotificationFilter | null
  onPresetConsumed?: () => void
}

export default function IntegrationsPage({ onOpenSession, onOpenMenu, presetFilter, onPresetConsumed }: IntegrationsPageProps) {
  const { t } = useTranslation('chat')
  const [settings, setSettings] = useState<WebApiWecomSettings | null>(null)
  const [loadingSettings, setLoadingSettings] = useState(true)
  const [settingsError, setSettingsError] = useState('')

  // 表单模型：webhook 只保存新输入；已存值仅在 placeholder 里显示 hint，不回显。
  const [enabled, setEnabled] = useState(false)
  const [requireConfirm, setRequireConfirm] = useState(false)
  const [notifyApproval, setNotifyApproval] = useState(false)
  const [notifyScheduleFailed, setNotifyScheduleFailed] = useState(false)
  const [notifyWeeklyDigest, setNotifyWeeklyDigest] = useState(false)
  const [baseURL, setBaseURL] = useState('')
  const [webhook, setWebhook] = useState('')
  const [clearWebhook, setClearWebhook] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)
  const savedTimer = useRef<number | null>(null)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ tone: 'success' | 'warning'; text: string } | null>(null)

  const [items, setItems] = useState<WebApiNotification[]>([])
  const [statusCounts, setStatusCounts] = useState<WebApiNotificationStatusCounts | undefined>(undefined)
  const [filter, setFilter] = useState<NotificationFilter>(presetFilter || 'all')
  const [kindFilter, setKindFilter] = useState<NotificationKindFilter>('all')
  const [hasMore, setHasMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [listError, setListError] = useState('')
  const [acting, setActing] = useState<string | null>(null)
  const [rowError, setRowError] = useState<{ id: string; message: string } | null>(null)
  const seq = useRef(0)
  const cursorRef = useRef<string | null>(null)

  useEffect(() => () => {
    if (savedTimer.current !== null) window.clearTimeout(savedTimer.current)
  }, [])

  // 总览跳转的预置筛选只消费一次（进入页面时由 useState 初始化）。
  useEffect(() => {
    if (presetFilter) onPresetConsumed?.()
  }, [presetFilter, onPresetConsumed])

  const applySettings = useCallback((next: WebApiWecomSettings) => {
    setSettings(next)
    setEnabled(next.enabled)
    setRequireConfirm(next.require_confirm)
    setNotifyApproval(next.notify_approval)
    setNotifyScheduleFailed(next.notify_schedule_failed)
    setNotifyWeeklyDigest(next.notify_weekly_digest)
    setBaseURL(next.base_url)
    setWebhook('')
    setClearWebhook(false)
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoadingSettings(true)
    getWebApiWecomSettings()
      .then((next) => {
        if (cancelled) return
        applySettings(next)
        setSettingsError('')
      })
      .catch((err) => {
        if (cancelled) return
        setSettingsError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setLoadingSettings(false)
      })
    return () => { cancelled = true }
  }, [applySettings])

  const save = async () => {
    setSaving(true)
    setSaveError('')
    setSaved(false)
    try {
      const body: Parameters<typeof saveWebApiWecomSettings>[0] = {
        enabled,
        require_confirm: requireConfirm,
        // 集成页始终显式带上三项当前值（契约：省略即保留）。
        notify_approval: notifyApproval,
        notify_schedule_failed: notifyScheduleFailed,
        notify_weekly_digest: notifyWeeklyDigest,
        base_url: baseURL.trim(),
      }
      if (clearWebhook) body.webhook = '-'
      else if (webhook.trim() !== '') body.webhook = webhook.trim()
      const next = await saveWebApiWecomSettings(body)
      applySettings(next)
      setTestResult(null)
      setSaved(true)
      if (savedTimer.current !== null) window.clearTimeout(savedTimer.current)
      savedTimer.current = window.setTimeout(() => setSaved(false), 3000)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  const runTest = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const result = await testWebApiWecom()
      if (result.ok) {
        setTestResult({ tone: 'success', text: t('shell.integrationsTestOk') })
      } else {
        setTestResult({
          tone: 'warning',
          text: t('shell.integrationsTestFailed', { errcode: result.errcode, errmsg: result.errmsg }),
        })
      }
    } catch (err) {
      // 409（未配置）/429（限流）等直接显示后端文案。
      setTestResult({ tone: 'warning', text: err instanceof Error ? err.message : String(err) })
    } finally {
      setTesting(false)
    }
  }

  const loadNotifications = useCallback(async (opts: { reset: boolean; silent?: boolean }) => {
    const nextCursor = opts.reset ? undefined : cursorRef.current || undefined
    if (!opts.reset && !nextCursor) return
    const current = ++seq.current
    if (!opts.silent) {
      if (opts.reset) setLoading(true)
      else setLoadingMore(true)
    }
    try {
      const data = await listWebApiNotifications({
        status: filter === 'all' ? undefined : filter,
        limit: PAGE_LIMIT,
        cursor: nextCursor,
      })
      if (current !== seq.current) return
      cursorRef.current = data.next_cursor
      setHasMore(data.has_more)
      setItems((prev) => (opts.reset ? data.items : [...prev, ...data.items]))
      if (data.status_counts) setStatusCounts(data.status_counts)
      setListError('')
    } catch (err) {
      if (current !== seq.current) return
      setListError(err instanceof Error ? err.message : String(err))
    } finally {
      if (current === seq.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    }
  }, [filter])

  // 条件变化时回到第一页。
  useEffect(() => {
    cursorRef.current = null
    void loadNotifications({ reset: true })
  }, [loadNotifications])

  // 15 秒静默刷新第一页；usePolling 在标签页隐藏时暂停。
  usePolling(() => { void loadNotifications({ reset: true, silent: true }) }, 15000)

  const refresh = () => {
    cursorRef.current = null
    void loadNotifications({ reset: true })
  }

  const runAction = async (item: WebApiNotification, action: 'confirm' | 'dismiss' | 'retry') => {
    setActing(`${item.id}:${action}`)
    setRowError(null)
    try {
      await actWebApiNotification(item.id, action)
      cursorRef.current = null
      await loadNotifications({ reset: true, silent: true })
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setRowError({ id: item.id, message })
      if (err instanceof APIError && err.status === 409) {
        cursorRef.current = null
        void loadNotifications({ reset: true, silent: true })
      }
    } finally {
      setActing(null)
    }
  }

  const filterItems: FilterPillItem<NotificationFilter>[] = [
    { key: 'all', label: t('shell.integrationsFilterAll'), count: statusCounts?.all },
    { key: 'pending_confirm', label: t('shell.integrationsFilterPending'), count: statusCounts?.pending_confirm },
    { key: 'queued', label: t('shell.integrationsFilterQueued'), count: statusCounts?.queued },
    { key: 'sending', label: t('shell.integrationsFilterSending'), count: statusCounts?.sending },
    { key: 'sent', label: t('shell.integrationsFilterSent'), count: statusCounts?.sent },
    { key: 'failed', label: t('shell.integrationsFilterFailed'), count: statusCounts?.failed },
    { key: 'dismissed', label: t('shell.integrationsFilterDismissed'), count: statusCounts?.dismissed },
  ]

  const kindFilterItems: Array<{ key: NotificationKindFilter; label: string }> = [
    { key: 'all', label: t('shell.integrationsKindFilterAll') },
    { key: 'vuln', label: t('shell.integrationsKindFilterVuln') },
    { key: 'approval_pending', label: t('shell.integrationsKindApprovalPending') },
    { key: 'schedule_failed', label: t('shell.integrationsKindScheduleFailed') },
    { key: 'weekly_digest', label: t('shell.integrationsKindWeeklyDigest') },
  ]
  // 类别只过滤已加载的数据，不改后端查询。
  const visibleItems = kindFilter === 'all' ? items : items.filter((item) => matchesKindFilter(item.kind, kindFilter))

  // 运营通知没有验证会话：待审批用 session_id，计划失败/每周摘要不显示“打开会话”。
  const sessionIDOf = (item: WebApiNotification): string => {
    if (item.kind === 'approval_pending') return item.session_id || ''
    if (item.kind === 'schedule_failed' || item.kind === 'weekly_digest') return ''
    return item.verification_session_id
  }
  const showSessionButton = (item: WebApiNotification): boolean =>
    item.kind !== 'schedule_failed' && item.kind !== 'weekly_digest'

  const configured = !!settings?.has_webhook

  // 说明列：发送时间 / 下次尝试 / 错误。
  const noteOf = (item: WebApiNotification): string => {
    if (item.status === 'sent' && item.sent_at) return formatShortDateTime(item.sent_at)
    if (item.status === 'queued' && item.next_attempt_at) {
      return t('shell.integrationsNextAttempt', { time: formatShortDateTime(item.next_attempt_at) })
    }
    return item.last_error || '—'
  }

  return (
    <Page contentClassName="max-w-[1600px] space-y-4">
      <PageHeader
        title={t('shell.integrationsTitle')}
        onOpenMenu={onOpenMenu}
        menuLabel={t('shell.openMenu')}
      >
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              aria-label={t('shell.integrationsHelp')}
              className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            >
              <HelpCircle className="h-3.5 w-3.5" />
            </button>
          </TooltipTrigger>
          <TooltipContent className="max-w-[300px] whitespace-normal text-left leading-relaxed">
            {t('shell.integrationsHelp')}
          </TooltipContent>
        </Tooltip>
      </PageHeader>

      <div className="flex flex-col gap-4 min-[1280px]:flex-row min-[1280px]:items-start">
        {/* 左：企业微信群机器人配置卡 */}
        <div className="min-w-0 flex-1 rounded-lg border border-border bg-card">
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
            <h2 className="text-[14px] font-semibold text-foreground">{t('shell.integrationsWecomTitle')}</h2>
            <IntegrationStatusBadge configured={configured} enabled={!!settings?.enabled} />
            <div className="ml-auto">
              <Tooltip>
                <TooltipTrigger asChild>
                  <span tabIndex={configured ? -1 : 0} className="inline-flex">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="h-9"
                      disabled={!configured || testing}
                      onClick={() => { void runTest() }}
                    >
                      {testing && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
                      {t('shell.integrationsSendTest')}
                    </Button>
                  </span>
                </TooltipTrigger>
                <TooltipContent>{configured ? t('shell.integrationsSendTest') : t('shell.integrationsSendTestHint')}</TooltipContent>
              </Tooltip>
            </div>
          </div>

          {loadingSettings ? (
            <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('shell.loading')}
            </div>
          ) : (
            <div className="space-y-4 p-4">
              {settingsError && <Callout tone="destructive">{t('shell.integrationsLoadFailed', { error: settingsError })}</Callout>}
              {testResult && <Callout tone={testResult.tone}>{testResult.text}</Callout>}

              {/* 设置行：标签 + 说明在左，开关在右。 */}
              <div className="divide-y divide-border border-y border-border">
                <SettingRow label={t('shell.integrationsEnabledLabel')} hint={t('shell.integrationsEnabledHint')}>
                  <Switch checked={enabled} onCheckedChange={setEnabled} disabled={saving} aria-label={t('shell.integrationsEnabledLabel')} />
                </SettingRow>
                <SettingRow label={t('shell.integrationsRequireConfirmLabel')} hint={t('shell.integrationsRequireConfirmHint')}>
                  <Switch checked={requireConfirm} onCheckedChange={setRequireConfirm} disabled={saving} aria-label={t('shell.integrationsRequireConfirmLabel')} />
                </SettingRow>
              </div>

              {/* 运营通知：独立分组，不受“需人工确认”影响；未启用/无 webhook 时仍可编辑。 */}
              <div className="border-t border-border pt-3">
                <div className="text-[12px] font-semibold text-foreground">{t('shell.integrationsOpsTitle')}</div>
                <div className="mt-2 divide-y divide-border border-y border-border">
                  <SettingRow
                    label={t('shell.integrationsOpsApprovalLabel')}
                    hint={t('shell.integrationsOpsApprovalHint')}
                    hintClassName="text-[11px]"
                  >
                    <Switch checked={notifyApproval} onCheckedChange={setNotifyApproval} disabled={saving} aria-label={t('shell.integrationsOpsApprovalLabel')} />
                  </SettingRow>
                  <SettingRow
                    label={t('shell.integrationsOpsScheduleFailedLabel')}
                    hint={t('shell.integrationsOpsScheduleFailedHint')}
                    hintClassName="text-[11px]"
                  >
                    <Switch checked={notifyScheduleFailed} onCheckedChange={setNotifyScheduleFailed} disabled={saving} aria-label={t('shell.integrationsOpsScheduleFailedLabel')} />
                  </SettingRow>
                  <SettingRow
                    label={t('shell.integrationsOpsWeeklyDigestLabel')}
                    hint={t('shell.integrationsOpsWeeklyDigestHint')}
                    hintClassName="text-[11px]"
                  >
                    <Switch checked={notifyWeeklyDigest} onCheckedChange={setNotifyWeeklyDigest} disabled={saving} aria-label={t('shell.integrationsOpsWeeklyDigestLabel')} />
                  </SettingRow>
                </div>
                <p className="mt-2 text-[11px] text-muted-foreground">{t('shell.integrationsOpsNote')}</p>
              </div>

              <Field label={t('shell.integrationsWebhookLabel')}>
                <div className="flex items-center gap-1.5">
                  <Input
                    type="password"
                    autoComplete="off"
                    value={webhook}
                    disabled={clearWebhook || saving}
                    onChange={(event) => setWebhook(event.target.value)}
                    placeholder={clearWebhook
                      ? t('shell.integrationsWebhookWillClear')
                      : configured
                        ? t('shell.integrationsWebhookConfigured', { hint: settings?.webhook_hint || '' })
                        : t('shell.integrationsWebhookPlaceholder')}
                    className="h-9 min-w-0 flex-1 font-mono text-[12px]"
                    aria-label={t('shell.integrationsWebhookLabel')}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className={cn('h-9 shrink-0', !configured && !clearWebhook && 'hidden')}
                    disabled={saving || (!configured && !clearWebhook)}
                    onClick={() => setClearWebhook((prev) => !prev)}
                  >
                    {clearWebhook ? t('shell.integrationsWebhookUndo') : t('shell.integrationsWebhookClear')}
                  </Button>
                </div>
              </Field>

              <Field label={t('shell.integrationsBaseUrlLabel')} hint={t('shell.integrationsBaseUrlHint')}>
                <div className="flex items-center gap-1.5">
                  <Input
                    value={baseURL}
                    onChange={(event) => setBaseURL(event.target.value)}
                    placeholder="https://wscyber.example.test"
                    disabled={saving}
                    className="h-9 min-w-0 flex-1"
                    aria-label={t('shell.integrationsBaseUrlLabel')}
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-9 shrink-0"
                    disabled={saving}
                    onClick={() => setBaseURL(window.location.origin)}
                  >
                    {t('shell.integrationsUseCurrent')}
                  </Button>
                </div>
              </Field>

              <div className="flex items-center justify-end gap-3 border-t border-border pt-3">
                {saveError && <span className="text-[12px] text-destructive">{saveError}</span>}
                {saved && <span className="text-[12px] text-success">{t('shell.integrationsSaved')}</span>}
                <Button type="button" className="h-9" onClick={() => { void save() }} disabled={saving}>
                  {saving && <Spinner className="mr-1.5 h-4 w-4" />}
                  {t('shell.integrationsSave')}
                </Button>
              </div>
            </div>
          )}
        </div>

        {/* 右：推送记录卡 */}
        <div className="min-w-0 flex-1 rounded-lg border border-border bg-card">
          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3">
            <h2 className="text-[14px] font-semibold text-foreground">{t('shell.integrationsRecordsTitle')}</h2>
            <div className="ml-auto">
              <Button type="button" variant="outline" size="sm" className="h-9" onClick={refresh} disabled={loading}>
                <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')} />
                {t('shell.refresh')}
              </Button>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2.5">
            <FilterPills items={filterItems} value={filter} onChange={setFilter} />
            <Select value={kindFilter} onValueChange={(value) => setKindFilter(value as NotificationKindFilter)}>
              <SelectTrigger aria-label={t('shell.integrationsKindFilterLabel')} className="ml-auto h-8 w-[128px] shrink-0 bg-card text-[12px]">
                <SelectValue placeholder={t('shell.integrationsKindFilterAll')} />
              </SelectTrigger>
              <SelectContent>
                {kindFilterItems.map((item) => (
                  <SelectItem key={item.key} value={item.key}>{item.label}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {listError && <p role="status" className="border-b border-border px-4 py-2 text-[12px] text-warning">{t('shell.integrationsLoadFailed', { error: listError })}</p>}

          {loading ? (
            <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('shell.loading')}
            </div>
          ) : visibleItems.length === 0 ? (
            <div className="flex h-40 flex-col items-center justify-center gap-1.5 px-6 text-center text-[13px] text-muted-foreground">
              <Inbox className="mb-1 h-6 w-6 text-muted-foreground/60" aria-hidden />
              <span>{t('shell.integrationsEmpty')}</span>
              {!settings?.enabled && <span className="text-[12px]">{t('shell.integrationsEmptyHint')}</span>}
            </div>
          ) : (
            <>
              <ul className="divide-y divide-border/70">
                {visibleItems.map((item) => {
                  const busy = acting !== null && acting.startsWith(`${item.id}:`)
                  const rowConflict = rowError?.id === item.id ? rowError.message : ''
                  const sessionID = sessionIDOf(item)
                  return (
                    <li key={item.id} className="px-4 py-2.5">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <NotificationStatusBadge status={item.status} />
                        <span className="shrink-0 text-[12px] text-muted-foreground">{t(KIND_LABEL_KEYS[item.kind])}</span>
                        <span className="min-w-0 flex-1 truncate text-[13px] text-foreground" title={item.title}>
                          {item.title || item.finding_number || '—'}
                        </span>
                        <span className="shrink-0 text-[12px] text-muted-foreground">{item.asset?.name || '—'}</span>
                        <span className="shrink-0 font-mono text-[11px] tabular text-muted-foreground">{formatShortDateTime(item.created_at)}</span>
                        <span className="flex shrink-0 items-center gap-1">
                          {item.status === 'pending_confirm' && (
                            <>
                              <Button type="button" variant="soft" size="xs" disabled={busy} onClick={() => { void runAction(item, 'confirm') }}>
                                {t('shell.integrationsConfirm')}
                              </Button>
                              <Button type="button" variant="ghost" size="xs" disabled={busy} onClick={() => { void runAction(item, 'dismiss') }}>
                                {t('shell.integrationsDismiss')}
                              </Button>
                            </>
                          )}
                          {item.status === 'failed' && (
                            <>
                              <Button type="button" variant="soft" size="xs" disabled={busy} onClick={() => { void runAction(item, 'retry') }}>
                                {t('shell.integrationsRetry')}
                              </Button>
                              <Button type="button" variant="ghost" size="xs" disabled={busy} onClick={() => { void runAction(item, 'dismiss') }}>
                                {t('shell.integrationsDismiss')}
                              </Button>
                            </>
                          )}
                          {showSessionButton(item) && (
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="inline-flex">
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon-xs"
                                    aria-label={t('shell.integrationsOpenSession')}
                                    disabled={!sessionID || busy}
                                    onClick={() => onOpenSession(sessionID)}
                                    className="text-muted-foreground"
                                  >
                                    <ExternalLink className="h-3.5 w-3.5" />
                                  </Button>
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>{t('shell.integrationsOpenSession')}</TooltipContent>
                            </Tooltip>
                          )}
                        </span>
                      </div>
                      {(rowConflict || item.status === 'queued' || item.last_error) && (
                        <p className={cn('mt-1 truncate text-[11px]', rowConflict ? 'text-destructive' : 'text-muted-foreground')} title={rowConflict || noteOf(item)}>
                          {rowConflict || noteOf(item)}
                        </p>
                      )}
                    </li>
                  )
                })}
              </ul>
              <div className="flex h-12 items-center border-t border-border px-4">
                <span className="text-[12px] text-muted-foreground">{t('shell.integrationsShown', { count: visibleItems.length })}</span>
                {hasMore && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="ml-auto h-8"
                    disabled={loadingMore}
                    onClick={() => { void loadNotifications({ reset: false }) }}
                  >
                    {loadingMore && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
                    {t('shell.loadMore')}
                  </Button>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </Page>
  )
}

/** 设置行：标签 + 说明左、控件右。 */
function SettingRow({ label, hint, hintClassName, children }: { label: ReactNode; hint?: ReactNode; hintClassName?: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 py-2.5">
      <div className="min-w-0">
        <div className="text-[13px] text-foreground">{label}</div>
        {hint && <div className={cn('mt-0.5 text-[12px] leading-relaxed text-muted-foreground', hintClassName)}>{hint}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}
