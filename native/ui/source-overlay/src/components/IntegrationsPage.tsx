import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ExternalLink, Loader2, RefreshCw } from 'lucide-react'
import {
  Badge, Button, Callout, Field, Input, Spinner, Switch,
  Tooltip, TooltipContent, TooltipTrigger,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { FilterPills, IconAction, Page, PageHeader } from './PageFrame'
import { usePolling } from '../hooks/usePolling'
import {
  actWebApiNotification, APIError, getWebApiWecomSettings, listWebApiNotifications,
  saveWebApiWecomSettings, testWebApiWecom,
  type WebApiNotification, type WebApiNotificationStatus, type WebApiWecomSettings,
} from '../api'

const PAGE_LIMIT = 50

type NotificationFilter = 'all' | WebApiNotificationStatus

interface IntegrationsPageProps {
  onOpenSession: (sessionID: string) => void
  onOpenMenu: () => void
}

function notificationVariant(status: WebApiNotificationStatus): 'warning' | 'info' | 'success' | 'danger' | 'muted' {
  switch (status) {
    case 'pending_confirm': return 'warning'
    case 'queued': return 'info'
    case 'sending': return 'info'
    case 'sent': return 'success'
    case 'failed': return 'danger'
    default: return 'muted'
  }
}

export default function IntegrationsPage({ onOpenSession, onOpenMenu }: IntegrationsPageProps) {
  const { t, i18n } = useTranslation('chat')
  const [settings, setSettings] = useState<WebApiWecomSettings | null>(null)
  const [loadingSettings, setLoadingSettings] = useState(true)
  const [settingsError, setSettingsError] = useState('')

  // 表单模型：webhook 只保存新输入；已存值仅在 placeholder 里显示 hint，不回显。
  const [enabled, setEnabled] = useState(false)
  const [requireConfirm, setRequireConfirm] = useState(false)
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
  const [filter, setFilter] = useState<NotificationFilter>('all')
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

  const applySettings = useCallback((next: WebApiWecomSettings) => {
    setSettings(next)
    setEnabled(next.enabled)
    setRequireConfirm(next.require_confirm)
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

  const formatCreated = (iso: string) => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return { day: iso, time: '' }
    return {
      day: date.toLocaleDateString(i18n.language, { year: 'numeric', month: '2-digit', day: '2-digit' }),
      time: date.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit', hour12: false }),
    }
  }
  const formatDateTime = (iso: string) => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    return date.toLocaleString(i18n.language, {
      year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
    })
  }
  const formatHM = (iso: string) => {
    const date = new Date(iso)
    if (Number.isNaN(date.getTime())) return iso
    return date.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit', hour12: false })
  }

  const statusLabel = (status: WebApiNotificationStatus) => {
    switch (status) {
      case 'pending_confirm': return t('shell.integrationsStatusPending')
      case 'queued': return t('shell.integrationsStatusQueued')
      case 'sending': return t('shell.integrationsStatusSending')
      case 'sent': return t('shell.integrationsStatusSent')
      case 'failed': return t('shell.integrationsStatusFailed')
      default: return t('shell.integrationsStatusDismissed')
    }
  }

  const filterItems: Array<{ key: NotificationFilter; label: string }> = [
    { key: 'all', label: t('shell.integrationsFilterAll') },
    { key: 'pending_confirm', label: t('shell.integrationsFilterPending') },
    { key: 'queued', label: t('shell.integrationsFilterQueued') },
    { key: 'sending', label: t('shell.integrationsFilterSending') },
    { key: 'sent', label: t('shell.integrationsFilterSent') },
    { key: 'failed', label: t('shell.integrationsFilterFailed') },
    { key: 'dismissed', label: t('shell.integrationsFilterDismissed') },
  ]

  const configured = !!settings?.has_webhook
  const headerBadge = !configured
    ? { variant: 'muted' as const, label: t('shell.integrationsStatusUnconfigured') }
    : settings?.enabled
      ? { variant: 'success' as const, label: t('shell.integrationsStatusEnabled') }
      : { variant: 'muted' as const, label: t('shell.integrationsStatusDisabled') }

  const noteOf = (item: WebApiNotification): string => {
    if (item.status === 'sent' && item.sent_at) return formatDateTime(item.sent_at)
    if (item.status === 'queued' && item.next_attempt_at) {
      return t('shell.integrationsNextAttempt', { time: formatHM(item.next_attempt_at) })
    }
    return item.last_error || '—'
  }

  return (
    <Page contentClassName="max-w-[1100px] space-y-4">
      <PageHeader
        title={t('shell.integrationsTitle')}
        onOpenMenu={onOpenMenu}
        menuLabel={t('shell.openMenu')}
      />

      {/* 卡片 A：企业微信群机器人 */}
      <div className="rounded-lg border border-border bg-card">
        <div className="flex h-12 items-center gap-2 border-b border-border px-5">
          <h2 className="text-[14px] font-semibold text-foreground">{t('shell.integrationsWecomTitle')}</h2>
          <Badge variant={headerBadge.variant} size="sm">{headerBadge.label}</Badge>
          <div className="ml-auto">
            <Tooltip>
              <TooltipTrigger asChild>
                <span tabIndex={configured ? -1 : 0} className="inline-flex">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
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
          <div className="space-y-4 p-5">
            {settingsError && <Callout tone="destructive">{t('shell.integrationsLoadFailed', { error: settingsError })}</Callout>}
            {testResult && <Callout tone={testResult.tone}>{testResult.text}</Callout>}

            <Field label={t('shell.integrationsEnabledLabel')} hint={t('shell.integrationsEnabledHint')}>
              <Switch checked={enabled} onCheckedChange={setEnabled} disabled={saving} />
            </Field>

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
                  className="min-w-0 flex-1"
                  aria-label={t('shell.integrationsWebhookLabel')}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={saving || (!configured && !clearWebhook)}
                  className={cn(!configured && !clearWebhook && 'hidden')}
                  onClick={() => setClearWebhook((prev) => !prev)}
                >
                  {clearWebhook ? t('shell.integrationsWebhookUndo') : t('shell.integrationsWebhookClear')}
                </Button>
              </div>
            </Field>

            <Field label={t('shell.integrationsRequireConfirmLabel')} hint={t('shell.integrationsRequireConfirmHint')}>
              <Switch checked={requireConfirm} onCheckedChange={setRequireConfirm} disabled={saving} />
            </Field>

            <Field label={t('shell.integrationsBaseUrlLabel')} hint={t('shell.integrationsBaseUrlHint')}>
              <div className="flex items-center gap-1.5">
                <Input
                  value={baseURL}
                  onChange={(event) => setBaseURL(event.target.value)}
                  placeholder="https://wscyber.example.test"
                  disabled={saving}
                  className="min-w-0 flex-1"
                  aria-label={t('shell.integrationsBaseUrlLabel')}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={saving}
                  onClick={() => setBaseURL(window.location.origin)}
                >
                  {t('shell.integrationsUseCurrent')}
                </Button>
              </div>
            </Field>

            <div className="flex items-center justify-end gap-3">
              {saveError && <span className="text-[12px] text-destructive">{saveError}</span>}
              {saved && <span className="text-[12px] text-success">{t('shell.integrationsSaved')}</span>}
              <Button type="button" onClick={() => { void save() }} disabled={saving}>
                {saving && <Spinner className="mr-1.5 h-4 w-4" />}
                {t('shell.integrationsSave')}
              </Button>
            </div>
          </div>
        )}
      </div>

      {/* 卡片 B：推送记录 */}
      <div className="rounded-lg border border-border bg-card">
        <div className="flex h-12 items-center gap-2 border-b border-border px-5">
          <h2 className="text-[14px] font-semibold text-foreground">{t('shell.integrationsRecordsTitle')}</h2>
          <div className="ml-auto">
            <IconAction label={t('shell.refresh')} onClick={refresh} disabled={loading}>
              <RefreshCw className={cn('h-3.5 w-3.5', loading && 'animate-spin')} />
            </IconAction>
          </div>
        </div>

        <div className="border-b border-border px-5 py-3">
          <FilterPills items={filterItems} value={filter} onChange={setFilter} />
        </div>

        {listError && <p role="status" className="border-b border-border px-5 py-2 text-[12px] text-warning">{t('shell.integrationsLoadFailed', { error: listError })}</p>}

        {loading ? (
          <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
            <Spinner className="h-5 w-5" />
            {t('shell.loading')}
          </div>
        ) : items.length === 0 ? (
          <div className="flex h-32 flex-col items-center justify-center gap-1 px-6 text-center text-[13px] text-muted-foreground">
            <span>{t('shell.integrationsEmpty')}</span>
            {!settings?.enabled && <span className="text-[12px]">{t('shell.integrationsEmptyHint')}</span>}
          </div>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[860px] table-fixed border-collapse text-[13px]">
                <colgroup>
                  <col className="w-[120px]" />
                  <col className="w-20" />
                  <col />
                  <col className="w-[140px]" />
                  <col className="w-[100px]" />
                  <col className="w-[60px]" />
                  <col />
                  <col className="w-[150px]" />
                </colgroup>
                <thead>
                  <tr className="h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colCreated')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.integrationsColKind')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.integrationsColVuln')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.integrationsColAsset')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.colStatus')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.integrationsColAttempts')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.integrationsColNote')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {items.map((item) => {
                    const created = formatCreated(item.created_at)
                    const busy = acting !== null && acting.startsWith(`${item.id}:`)
                    const rowConflict = rowError?.id === item.id ? rowError.message : ''
                    return (
                      <tr key={item.id} className="h-12 border-b border-border/70 transition-colors hover:bg-muted/40">
                        <td className="px-3 leading-tight">
                          <div className="text-[12px] text-foreground">{created.day}</div>
                          <div className="font-mono text-[11px] text-muted-foreground">{created.time}</div>
                        </td>
                        <td className="px-3">
                          {item.kind === 'regression'
                            ? <Badge variant="warning" size="sm">{t('shell.integrationsKindRegression')}</Badge>
                            : <Badge variant="muted" size="sm">{t('shell.integrationsKindNew')}</Badge>}
                        </td>
                        <td className="min-w-0 px-3 leading-tight">
                          <div className="truncate font-mono text-[11px] text-muted-foreground">
                            {item.finding_number || '—'}
                          </div>
                          <div className="truncate text-[13px] text-foreground" title={item.title}>{item.title || '—'}</div>
                        </td>
                        <td className="truncate px-3 text-[12px] text-muted-foreground" title={item.asset?.name}>
                          {item.asset?.name || '—'}
                        </td>
                        <td className="px-3">
                          <Badge variant={notificationVariant(item.status)} size="sm">{statusLabel(item.status)}</Badge>
                        </td>
                        <td className="px-3 text-right tabular-nums text-muted-foreground">{item.attempts}</td>
                        <td className="min-w-0 px-3">
                          {rowConflict
                            ? <span className="block truncate text-[12px] text-destructive" title={rowConflict}>{rowConflict}</span>
                            : <span className="block truncate text-[12px] text-muted-foreground" title={noteOf(item)}>{noteOf(item)}</span>}
                        </td>
                        <td className="px-3">
                          <div className="flex items-center justify-end gap-1">
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
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="inline-flex">
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon-xs"
                                    aria-label={t('shell.integrationsOpenSession')}
                                    disabled={!item.verification_session_id || busy}
                                    onClick={() => onOpenSession(item.verification_session_id)}
                                    className="text-muted-foreground"
                                  >
                                    <ExternalLink className="h-3.5 w-3.5" />
                                  </Button>
                                </span>
                              </TooltipTrigger>
                              <TooltipContent>{t('shell.integrationsOpenSession')}</TooltipContent>
                            </Tooltip>
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
            <div className="flex h-12 items-center border-t border-border px-5">
              <span className="text-[12px] text-muted-foreground">{t('shell.integrationsShown', { count: items.length })}</span>
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
    </Page>
  )
}
