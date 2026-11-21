import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Archive, ArchiveRestore, HelpCircle, Loader2, Pencil, Plus, RefreshCw, Rocket, Trash2 } from 'lucide-react'
import { newID } from '@cyber/aop'
import { cn } from '@cyber/theme'
import {
  Button, Chip, EmptyState, Field, Input, SkeletonList, Spinner,
  Switch, Textarea, Tooltip, TooltipContent, TooltipTrigger, useConfirm,
} from '@cyber/ui'
import { Page, PageHeader } from './PageFrame'
import { EnvBadge, RunStateBadge, ScheduleStateBadge } from './ui-badges'
import {
  deleteWebApiCredential,
  deleteWebApiSchedule,
  getWebApiAsset,
  getWebApiTask,
  listWebApiAssetTasks,
  listWebApiAssetVulns,
  listWebApiAssets,
  listWebApiScheduleRuns,
  listWebApiSchedules,
  saveWebApiAsset,
  saveWebApiCredential,
  saveWebApiSchedule,
  type WebApiAsset,
  type WebApiAssetCredential,
  type WebApiAssetCredentialInput,
  type WebApiAssetSaveInput,
  type WebApiAssetTaskItem,
  type WebApiSchedule,
  type WebApiScheduleRun,
  type WebApiVulnStatus,
} from '../api'
import { usePolling } from '../hooks/usePolling'
import { formatDateTime, formatShortDateTime } from '../lib/time'
import { normalizeWebApiPolicy, useWebApiPolicySummary, WebApiPolicyFields } from './WebApiPolicyShared'
import WebApiScenarioPicker, { scenarioDraftOf } from './WebApiScenarioPicker'
import WebApiAssetLaunchDialog from './WebApiAssetLaunchDialog'
import WebApiAssetVulns from './WebApiAssetVulns'
import './WebApiAssetsPanel.css'

interface Props {
  focusAssetId: string | null
  /** 从全局漏洞页跳入时要展开的漏洞条目；随 focusAssetId 一起消费。 */
  focusVulnId?: string | null
  onFocusConsumed: () => void
  onFocusVulnConsumed?: () => void
  onOpenTask: (sessionID: string) => void
  /** 资产漏洞库行点击打开漏洞卡（?page=vulns&vuln=）；带回资产 ID 以便后退时重新选中。 */
  onOpenAssetVulnCard?: (assetId: string, key: string) => void
  onOpenMenu?: () => void
}

const EMPTY_CREDENTIAL: WebApiAssetCredentialInput & { secret: string } = { label: '', username: '', secret: '', note: '' }

const CRON_PRESETS = [
  { value: '0 2 * * *', key: 'webapi.schedulePresetDaily' },
  { value: '0 2 * * 1', key: 'webapi.schedulePresetWeekly' },
  { value: '0 */6 * * *', key: 'webapi.schedulePreset6h' },
] as const

interface AssetForm {
  name: string
  owner: string
  environment: string
  known_info: string
  objective: string
  origins: string[]
  forbidden: string
  scope: string
  approval: string
  scenario: string
  doneCriteria: string
}

function formFromAsset(asset: WebApiAsset | null): AssetForm {
  const policy = normalizeWebApiPolicy(asset ? {
    scope: asset.scope,
    approval: asset.approval,
    is_default: !asset.scope && !asset.approval,
  } : null)
  const scenario = scenarioDraftOf(asset?.scenario)
  return {
    name: asset?.name ?? '',
    owner: asset?.owner ?? '',
    environment: asset?.environment ?? '',
    known_info: asset?.known_info ?? '',
    objective: asset?.objective ?? '',
    origins: asset?.origins && asset.origins.length > 0 ? [...asset.origins] : [''],
    forbidden: asset?.forbidden ?? '',
    scope: policy.scope,
    approval: policy.approval,
    scenario: scenario.scenario,
    doneCriteria: scenario.doneCriteria,
  }
}

// 已保存资产的持久化字段（归档切换时整份提交，避免覆盖丢失）。
function assetInput(asset: WebApiAsset, archived: boolean): WebApiAssetSaveInput {
  return {
    name: asset.name,
    owner: asset.owner,
    environment: asset.environment,
    known_info: asset.known_info,
    objective: asset.objective,
    origins: asset.origins,
    forbidden: asset.forbidden,
    scope: asset.scope,
    approval: asset.approval,
    archived,
  }
}

function normalizeForm(form: AssetForm) {
  return JSON.stringify({
    ...form,
    name: form.name.trim(),
    owner: form.owner.trim(),
    environment: form.environment.trim(),
    origins: form.origins.map((origin) => origin.trim()).filter(Boolean),
  })
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

type AssetDetailTab = 'info' | 'vulns'

// 资产视图：左列表（360px @≥1100px）+ 右详情。详情内为基本信息表单、测试账号、
// 周期计划与关联任务，均为原生 /api/webapi 接口，不读其它来源。
export default function WebApiAssetsPanel({ focusAssetId, focusVulnId, onFocusConsumed, onFocusVulnConsumed, onOpenTask, onOpenAssetVulnCard, onOpenMenu }: Props) {
  const { t } = useTranslation('chat')
  const [assets, setAssets] = useState<WebApiAsset[]>([])
  const [schedules, setSchedules] = useState<Record<string, WebApiSchedule>>({})
  const [showArchived, setShowArchived] = useState(false)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const [listError, setListError] = useState('')
  const [selected, setSelected] = useState<WebApiAsset | null>(null)
  const [creatingId, setCreatingId] = useState('')
  const [showDetail, setShowDetail] = useState(false)
  // 切换资产时保留当前分段（资产信息 / 漏洞库）。
  const [detailTab, setDetailTab] = useState<AssetDetailTab>('info')
  const loadSeq = useRef(0)
  const focusHandled = useRef<string | null>(null)

  const load = useCallback(async (archived: boolean) => {
    const seq = ++loadSeq.current
    setLoading(true)
    setListError('')
    try {
      const [nextAssets, nextSchedules] = await Promise.all([listWebApiAssets(archived), listWebApiSchedules()])
      if (seq !== loadSeq.current) return
      setAssets(nextAssets)
      setSchedules(Object.fromEntries(nextSchedules.map((item) => [item.asset_id, item])))
      // 选中项以列表最新视图为准；列表不含它（归档过滤）时保留当前对象以便取消归档。
      setSelected((prev) => (prev ? nextAssets.find((item) => item.id === prev.id) ?? prev : prev))
    } catch (err) {
      if (seq !== loadSeq.current) return
      setListError(errorText(err))
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [])

  useEffect(() => { void load(showArchived) }, [load, showArchived])
  useEffect(() => () => { loadSeq.current++ }, [])

  // 外部定位资产（任务顶栏标签 / 用量等）：找不到时先打开“显示已归档”再按 ID 拉取。
  useEffect(() => {
    if (!focusAssetId) return
    const found = assets.find((item) => item.id === focusAssetId)
    if (found) {
      setCreatingId('')
      setSelected(found)
      setShowDetail(true)
      if (focusVulnId) setDetailTab('vulns')
      onFocusConsumed()
      return
    }
    if (loading) return
    if (!showArchived) {
      setShowArchived(true)
      return
    }
    if (focusHandled.current === focusAssetId) return
    focusHandled.current = focusAssetId
    void (async () => {
      try {
        const asset = await getWebApiAsset(focusAssetId)
        setCreatingId('')
        setSelected(asset)
        setShowDetail(true)
        if (focusVulnId) setDetailTab('vulns')
        if (asset.archived !== showArchived) setShowArchived(asset.archived)
      } catch (err) {
        setListError(errorText(err))
      } finally {
        onFocusConsumed()
      }
    })()
  }, [focusAssetId, focusVulnId, assets, loading, showArchived, onFocusConsumed])

  const visibleAssets = useMemo(() => {
    const query = search.trim().toLowerCase()
    if (!query) return assets
    return assets.filter((asset) => asset.name.toLowerCase().includes(query)
      || asset.owner.toLowerCase().includes(query)
      || asset.origins.some((origin) => origin.toLowerCase().includes(query)))
  }, [assets, search])

  const selectAsset = useCallback((asset: WebApiAsset) => {
    setSelected(asset)
    setCreatingId('')
    setShowDetail(true)
  }, [])

  const startCreate = useCallback(() => {
    setCreatingId(newID())
    setSelected(null)
    setShowDetail(true)
  }, [])

  const handleSaved = useCallback((saved: WebApiAsset) => {
    setCreatingId('')
    setSelected(saved)
    setShowDetail(true)
    setAssets((prev) => {
      const rest = prev.filter((item) => item.id !== saved.id)
      return saved.archived === showArchived ? [saved, ...rest] : rest
    })
    void load(showArchived)
  }, [load, showArchived])

  const updateAsset = useCallback((saved: WebApiAsset) => {
    setSelected((prev) => (prev && prev.id === saved.id ? saved : prev))
    setAssets((prev) => prev.map((item) => (item.id === saved.id ? saved : item)))
  }, [])

  const handleScheduleChange = useCallback((assetId: string, next: WebApiSchedule | null) => {
    setSchedules((prev) => {
      const copy = { ...prev }
      if (next) copy[assetId] = next
      else delete copy[assetId]
      return copy
    })
  }, [])

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page className="flex flex-col overflow-hidden" contentClassName="flex min-h-0 flex-1 flex-col space-y-3">
        <PageHeader
          title={t('shell.navAssets')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <>
              <label className="flex shrink-0 cursor-pointer items-center gap-1.5 text-[12px] text-muted-foreground">
                <Switch
                  checked={showArchived}
                  onCheckedChange={setShowArchived}
                  className="scale-75"
                  aria-label={t('webapi.assetShowArchived')}
                />
                {t('webapi.assetShowArchived')}
              </label>
              <Button type="button" variant="outline" size="sm" className="h-9" onClick={() => void load(showArchived)} disabled={loading}>
                <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')} />
                {t('shell.refresh')}
              </Button>
              <Button type="button" size="sm" className="h-9 gap-1.5" onClick={startCreate}>
                <Plus className="h-4 w-4" />
                {t('webapi.assetNew')}
              </Button>
            </>
          )}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label={t('shell.assetsHelp')}
                className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <HelpCircle className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px] whitespace-normal text-left leading-relaxed">
              {t('shell.assetsHelp')}
            </TooltipContent>
          </Tooltip>
        </PageHeader>

        <div className="assets-container min-h-0 min-w-0 flex-1">
          <div className={cn('assets-workspace', showDetail && 'assets-show-detail')}>
            <div className="assets-list flex min-h-0 min-w-0 flex-col border-border">
              <div className="shrink-0 border-b border-border/60 px-3 py-2.5">
                <Input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder={t('webapi.assetSearchPlaceholder')}
                  className="h-9 bg-card text-[13px]"
                  aria-label={t('webapi.assetSearchPlaceholder')}
                />
              </div>

              <div className="min-h-0 flex-1 overflow-auto">
                {loading && assets.length === 0 ? (
                  <SkeletonList count={5} className="p-3" />
                ) : listError ? (
                  <div className="flex flex-col items-center gap-2 px-3 py-4 text-center text-sm">
                    <span className="text-destructive">{listError}</span>
                    <Button type="button" variant="outline" size="xs" onClick={() => void load(showArchived)}>{t('shell.refresh')}</Button>
                  </div>
                ) : visibleAssets.length === 0 ? (
                  <EmptyState
                    icon={Archive}
                    title={t('webapi.assetEmptyTitle')}
                    action={<Button type="button" size="xs" onClick={startCreate} className="gap-1"><Plus className="h-3.5 w-3.5" />{t('webapi.assetNew')}</Button>}
                    compact
                  />
                ) : (
                  <div className="divide-y divide-border/40">
                    {visibleAssets.map((asset) => (
                      <AssetRow
                        key={asset.id}
                        asset={asset}
                        selected={!creatingId && selected?.id === asset.id}
                        onSelect={() => selectAsset(asset)}
                      />
                    ))}
                  </div>
                )}
              </div>
            </div>

            <div className="assets-detail flex min-h-0 min-w-0 flex-col overflow-hidden">
              {creatingId ? (
                <AssetDetail
                  key={`new-${creatingId}`}
                  asset={null}
                  creatingId={creatingId}
                  schedule={null}
                  tab={detailTab}
                  onTabChange={setDetailTab}
                  focusVulnId={focusVulnId}
                  onFocusVulnConsumed={onFocusVulnConsumed}
                  onSaved={handleSaved}
                  onAssetUpdated={updateAsset}
                  onScheduleChange={handleScheduleChange}
                  onOpenTask={onOpenTask}
                  onOpenAssetVulnCard={onOpenAssetVulnCard}
                  onBack={() => setShowDetail(false)}
                />
              ) : selected ? (
                <AssetDetail
                  key={selected.id}
                  asset={selected}
                  creatingId={null}
                  schedule={schedules[selected.id] ?? null}
                  tab={detailTab}
                  onTabChange={setDetailTab}
                  focusVulnId={focusVulnId}
                  onFocusVulnConsumed={onFocusVulnConsumed}
                  onSaved={handleSaved}
                  onAssetUpdated={updateAsset}
                  onScheduleChange={handleScheduleChange}
                  onOpenTask={onOpenTask}
                  onOpenAssetVulnCard={onOpenAssetVulnCard}
                  onBack={() => setShowDetail(false)}
                />
              ) : (
                <div className="flex flex-1 items-center justify-center px-4 text-sm text-muted-foreground">{t('webapi.assetSelect')}</div>
              )}
            </div>
          </div>
        </div>
      </Page>
    </div>
  )
}

function AssetRow({ asset, selected, onSelect }: {
  asset: WebApiAsset
  selected: boolean
  onSelect: () => void
}) {
  const { t } = useTranslation('chat')
  const policy = useMemo(() => normalizeWebApiPolicy({
    scope: asset.scope,
    approval: asset.approval,
    is_default: !asset.scope && !asset.approval,
  }), [asset.scope, asset.approval])
  const policySummary = useWebApiPolicySummary(policy)
  // 次行：负责人 · N 个 origin · 策略摘要（逾期漏洞计数暂无数据，不做）。
  const meta = [asset.owner, t('webapi.assetOriginsCount', { count: asset.origins.length }), policySummary.text]
    .filter(Boolean)
    .join(' · ')

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        'flex w-full min-w-0 flex-col gap-1 px-3 py-2.5 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
        selected && 'bg-[var(--brand-soft)]',
        asset.archived && 'opacity-60',
      )}
    >
      <span className="flex w-full min-w-0 items-center gap-2">
        <span className="assets-list-title min-w-0 flex-1 text-[14px] font-medium text-foreground">{asset.name}</span>
        {asset.archived && <span className="shrink-0 text-[11px] text-muted-foreground">{t('webapi.assetArchivedBadge')}</span>}
        <EnvBadge env={asset.environment} className="shrink-0" />
      </span>
      <span className="block w-full truncate text-[12px] text-muted-foreground">{meta || '—'}</span>
    </button>
  )
}

function DetailSection({ title, first, actions, children }: {
  title?: ReactNode
  first?: boolean
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className={cn('space-y-3', !first && 'border-t border-border pt-4')}>
      {(title || actions) && (
        <div className="flex flex-wrap items-center justify-between gap-2">
          {title ? <h3 className="text-[14px] font-semibold text-foreground">{title}</h3> : <span />}
          {actions}
        </div>
      )}
      {children}
    </section>
  )
}

/** 右栏卡片标题行：14px 600 + 右侧操作。 */
function RailCard({ title, actions, children }: { title: ReactNode; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-[14px] font-semibold text-foreground">{title}</h3>
        {actions}
      </div>
      {children}
    </div>
  )
}

/** 设置行：标签（+ 可选说明）在左、开关在右。 */
function SwitchRow({ label, hint, children }: { label: ReactNode; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3 py-2">
      <div className="min-w-0">
        <div className="text-[13px] text-foreground">{label}</div>
        {hint && <div className="mt-0.5 text-[12px] text-muted-foreground">{hint}</div>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

function AssetDetail({
  asset, creatingId, schedule, tab, onTabChange, focusVulnId, onFocusVulnConsumed,
  onSaved, onAssetUpdated, onScheduleChange, onOpenTask, onOpenAssetVulnCard, onBack,
}: {
  asset: WebApiAsset | null
  creatingId: string | null
  schedule: WebApiSchedule | null
  tab: AssetDetailTab
  onTabChange: (tab: AssetDetailTab) => void
  focusVulnId?: string | null
  onFocusVulnConsumed?: () => void
  onSaved: (asset: WebApiAsset) => void
  onAssetUpdated: (asset: WebApiAsset) => void
  onScheduleChange: (assetId: string, schedule: WebApiSchedule | null) => void
  onOpenTask: (sessionID: string) => void
  onOpenAssetVulnCard?: (assetId: string, key: string) => void
  onBack: () => void
}) {
  const { t } = useTranslation('chat')
  const confirm = useConfirm()
  const creating = asset === null
  const assetId = asset?.id ?? creatingId ?? ''
  const baseline = useMemo(() => formFromAsset(asset), [asset])
  const [form, setForm] = useState<AssetForm>(baseline)
  const previousBaseline = useRef(baseline)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [savedFlash, setSavedFlash] = useState(false)
  const [archiving, setArchiving] = useState(false)
  const [launchOpen, setLaunchOpen] = useState(false)
  // 漏洞库标签数字：切换资产时先取一次 counts，之后由漏洞库视图加载后回传。
  const [vulnCounts, setVulnCounts] = useState<Record<WebApiVulnStatus, number> | null>(null)
  // 计划运行刷新后的首条运行 ID：变化即让右栏运行记录与关联任务静默重取。
  const [tasksKey, setTasksKey] = useState('')
  const savedTimer = useRef<number | null>(null)

  useEffect(() => {
    if (creating || !asset) return
    let cancelled = false
    listWebApiAssetVulns(asset.id)
      .then((result) => { if (!cancelled) setVulnCounts(result.counts) })
      .catch(() => { /* 数字取不到就不显示，不阻塞资产信息 */ })
    return () => { cancelled = true }
  }, [creating, asset?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  // 资产视图被外部刷新（如保存测试账号）时同步服务端值；仅在表单没有未保存
  // 编辑时覆盖，避免丢掉用户正在改的基本信息。
  useEffect(() => {
    const previous = previousBaseline.current
    previousBaseline.current = baseline
    setForm((current) => (normalizeForm(current) === normalizeForm(previous) ? formFromAsset(asset) : current))
  }, [baseline, asset])
  useEffect(() => () => { if (savedTimer.current !== null) window.clearTimeout(savedTimer.current) }, [])

  const dirty = normalizeForm(form) !== normalizeForm(baseline)
  const patch = (value: Partial<AssetForm>) => setForm((current) => ({ ...current, ...value }))
  const patchOrigin = (index: number, value: string) => setForm((current) => ({
    ...current,
    origins: current.origins.map((origin, i) => (i === index ? value : origin)),
  }))
  const addOrigin = () => setForm((current) => ({ ...current, origins: [...current.origins, ''] }))
  const removeOrigin = (index: number) => setForm((current) => (current.origins.length <= 1
    ? current
    : { ...current, origins: current.origins.filter((_, i) => i !== index) }))

  const save = async () => {
    if (saving) return
    const origins = form.origins.map((origin) => origin.trim()).filter(Boolean)
    if (!form.name.trim()) { setSaveError(t('webapi.assetRequiredName')); return }
    if (origins.length === 0) { setSaveError(t('webapi.assetRequiredOrigins')); return }
    setSaving(true)
    setSaveError('')
    try {
      const saved = await saveWebApiAsset(assetId, {
        name: form.name,
        owner: form.owner,
        environment: form.environment,
        known_info: form.known_info,
        objective: form.objective,
        origins,
        forbidden: form.forbidden,
        scope: form.scope,
        approval: form.approval,
        scenario: form.scenario,
        done_criteria: form.doneCriteria.trim(),
        archived: asset?.archived ?? false,
      })
      onSaved(saved)
      setSavedFlash(true)
      if (savedTimer.current !== null) window.clearTimeout(savedTimer.current)
      savedTimer.current = window.setTimeout(() => setSavedFlash(false), 2000)
    } catch (err) {
      setSaveError(errorText(err))
    } finally {
      setSaving(false)
    }
  }

  const toggleArchive = async () => {
    if (!asset || archiving) return
    const ok = await confirm({
      description: asset.archived ? t('webapi.assetUnarchiveConfirm') : t('webapi.assetArchiveConfirm'),
      destructive: !asset.archived,
    })
    if (!ok) return
    setArchiving(true)
    setSaveError('')
    try {
      onSaved(await saveWebApiAsset(asset.id, assetInput(asset, !asset.archived)))
    } catch (err) {
      setSaveError(errorText(err))
    } finally {
      setArchiving(false)
    }
  }

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="space-y-4 px-5 py-4">
          {/* 详情头：资产名 18px + EnvBadge；右侧 归档 outline · 发起测试 primary（h-9）。 */}
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="ghost" size="xs" onClick={onBack} className="assets-back -ml-2 shrink-0 text-muted-foreground">
              {t('webapi.assetBack')}
            </Button>
            <h2 className="min-w-0 truncate text-[18px] font-semibold text-foreground" title={creating ? undefined : asset.name}>
              {creating ? t('webapi.assetNewTitle') : asset.name}
            </h2>
            {!creating && <EnvBadge env={asset.environment} />}
            {!creating && asset.archived && <span className="ws-st-badge ws-st-unconfirmed">{t('webapi.assetArchivedBadge')}</span>}
            {!creating && (
              <div className="ml-auto flex shrink-0 items-center gap-2">
                <Button type="button" variant="outline" size="sm" className="h-9" onClick={() => void toggleArchive()} disabled={archiving}>
                  {archiving ? <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" /> : asset.archived ? <ArchiveRestore className="mr-1.5 h-3.5 w-3.5" /> : <Archive className="mr-1.5 h-3.5 w-3.5" />}
                  {asset.archived ? t('webapi.assetUnarchive') : t('webapi.assetArchive')}
                </Button>
                <Button type="button" size="sm" className="h-9 gap-1.5" onClick={() => setLaunchOpen(true)}>
                  <Rocket className="h-4 w-4" />
                  {t('webapi.assetLaunch')}
                </Button>
              </div>
            )}
          </div>

          {!creating && (
            <div role="group" aria-label={t('webapi.vulnTab')} className="flex flex-wrap items-center gap-1.5">
              <button
                type="button"
                aria-pressed={tab === 'info'}
                onClick={() => onTabChange('info')}
                className={cn(
                  'flex h-8 items-center gap-1.5 rounded-full border px-2.5 text-[12px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                  tab === 'info'
                    ? 'border-primary bg-primary text-primary-foreground'
                    : 'border-border bg-card text-muted-foreground hover:text-foreground',
                )}
              >
                {t('webapi.vulnTabInfo')}
              </button>
              <button
                type="button"
                aria-pressed={tab === 'vulns'}
                onClick={() => onTabChange('vulns')}
                className={cn(
                  'flex h-8 items-center gap-1.5 rounded-full border px-2.5 text-[12px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                  tab === 'vulns'
                    ? 'border-primary bg-primary text-primary-foreground'
                    : 'border-border bg-card text-muted-foreground hover:text-foreground',
                )}
              >
                {t('webapi.vulnTab')}
                {!!vulnCounts && (
                  <span className={cn('tabular', tab === 'vulns' ? 'text-primary-foreground/80' : 'text-muted-foreground')}>
                    {vulnCounts.new + vulnCounts.present + vulnCounts.pending + vulnCounts.unconfirmed + vulnCounts.fixed}
                  </span>
                )}
              </button>
            </div>
          )}

          {tab === 'vulns' && !creating ? (
            <WebApiAssetVulns
              assetId={asset.id}
              onCounts={setVulnCounts}
              focusVulnId={focusVulnId}
              onFocusConsumed={onFocusVulnConsumed}
              onOpenVuln={(key) => onOpenAssetVulnCard?.(asset.id, key)}
            />
          ) : (
            <div className="flex flex-col gap-4 min-[1280px]:flex-row min-[1280px]:items-start">
              {/* 主栏：基本信息 + 研究场景与策略，底部共用一个保存。 */}
              <div className="min-w-0 flex-1 space-y-4">
                <section className="rounded-lg border border-border bg-card p-4">
                  <DetailSection title={t('webapi.assetBasicInfo')} first>
                    <div className="assets-form-grid">
                      <Field label={`${t('webapi.assetName')}*`}>
                        <Input value={form.name} onChange={(event) => patch({ name: event.target.value })} disabled={saving} className="h-9" />
                      </Field>
                      <Field label={t('webapi.assetOwner')}>
                        <Input value={form.owner} onChange={(event) => patch({ owner: event.target.value })} disabled={saving} className="h-9" />
                      </Field>
                      <Field label={t('webapi.assetEnvironment')}>
                        <Input value={form.environment} onChange={(event) => patch({ environment: event.target.value })} placeholder={t('webapi.assetEnvironmentPlaceholder')} disabled={saving} className="h-9" />
                      </Field>
                      <div className="assets-form-col space-y-1.5">
                        <span className="block text-[12px] text-muted-foreground">{t('webapi.assetOrigins')}*</span>
                        {form.origins.map((origin, index) => (
                          <div key={index} className="flex items-center gap-1.5">
                            <Input value={origin} onChange={(event) => patchOrigin(index, event.target.value)} placeholder={t('webapi.originPlaceholder')} disabled={saving} className="h-9 flex-1" />
                            <Button type="button" variant="ghost" size="icon-xs" onClick={() => removeOrigin(index)} disabled={form.origins.length <= 1 || saving} aria-label={t('webapi.removeOrigin')} className="shrink-0 text-muted-foreground hover:text-destructive">
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          </div>
                        ))}
                        <Button type="button" variant="ghost" size="xs" onClick={addOrigin} disabled={saving} className="gap-1 text-muted-foreground">
                          <Plus className="h-3.5 w-3.5" />
                          {t('webapi.addOrigin')}
                        </Button>
                      </div>
                      <Field label={t('webapi.assetKnownInfo')} className="assets-form-col">
                        <Textarea value={form.known_info} onChange={(event) => patch({ known_info: event.target.value })} rows={4} disabled={saving} />
                      </Field>
                      <Field label={t('webapi.assetObjective')} className="assets-form-col">
                        <Textarea value={form.objective} onChange={(event) => patch({ objective: event.target.value })} rows={3} placeholder={t('webapi.objectivePlaceholder')} disabled={saving} />
                      </Field>
                      <Field
                        label={(
                          <span className="flex flex-wrap items-center gap-2">
                            {t('webapi.assetForbidden')}
                            <span className="text-[11px] font-normal text-muted-foreground">{t('webapi.assetForbiddenHint')}</span>
                          </span>
                        )}
                        className="assets-form-col"
                      >
                        <Textarea value={form.forbidden} onChange={(event) => patch({ forbidden: event.target.value })} rows={3} disabled={saving} />
                      </Field>
                    </div>
                  </DetailSection>
                </section>

                <section className="rounded-lg border border-border bg-card p-4">
                  <DetailSection title={t('webapi.assetScenarioSection')} first>
                    <div className="space-y-3">
                      <div className="space-y-1.5">
                        <p className="text-[12px] text-muted-foreground">{t('scenario.assetHint')}</p>
                        <WebApiScenarioPicker
                          value={{ scenario: form.scenario, doneCriteria: form.doneCriteria }}
                          onChange={(next) => patch({ scenario: next.scenario, doneCriteria: next.doneCriteria })}
                          disabled={saving}
                        />
                      </div>
                      <WebApiPolicyFields
                        scope={form.scope}
                        approval={form.approval}
                        onScopeChange={(scope) => patch({ scope })}
                        onApprovalChange={(approval) => patch({ approval })}
                        disabled={saving}
                      />
                    </div>
                  </DetailSection>

                  <div className="mt-4 flex min-h-7 items-center justify-end gap-2 border-t border-border pt-3">
                    {saveError && <span role="alert" className="mr-auto min-w-0 text-xs text-destructive">{saveError}</span>}
                    {savedFlash && <span className="text-xs text-muted-foreground">{t('webapi.assetSaved')}</span>}
                    <Button type="button" className="h-9" onClick={() => void save()} disabled={saving || !dirty}>
                      {saving && <Spinner className="mr-1.5 h-3.5 w-3.5" />}
                      {t('webapi.assetSave')}
                    </Button>
                  </div>
                </section>
              </div>

              {/* 右栏：测试账号、周期计划、运行记录、关联任务。 */}
              {!creating && (
                <aside className="w-full shrink-0 space-y-3 min-[1280px]:w-[360px]">
                  <CredentialsSection asset={asset} onAsset={onAssetUpdated} />
                  <ScheduleSection
                    asset={asset}
                    schedule={schedule}
                    onScheduleChange={(next) => onScheduleChange(asset.id, next)}
                    onOpenTask={onOpenTask}
                    onRunsChanged={setTasksKey}
                  />
                  <AssetTasks assetId={asset.id} refreshKey={tasksKey} onOpenTask={onOpenTask} />
                </aside>
              )}
            </div>
          )}
        </div>
      </div>

      {!creating && (
        <WebApiAssetLaunchDialog
          open={launchOpen}
          asset={asset}
          onClose={() => setLaunchOpen(false)}
          onLaunched={(sessionID) => { setLaunchOpen(false); onOpenTask(sessionID) }}
        />
      )}
    </div>
  )
}

function CredentialsSection({ asset, onAsset }: { asset: WebApiAsset; onAsset: (asset: WebApiAsset) => void }) {
  const { t } = useTranslation('chat')
  const confirm = useConfirm()
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState<WebApiAssetCredentialInput & { secret: string }>(EMPTY_CREDENTIAL)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const beginEdit = (credential: WebApiAssetCredential) => {
    setEditingId(credential.id)
    setDraft({ label: credential.label, username: credential.username, secret: '', note: credential.note })
    setError('')
  }
  const beginNew = () => {
    setEditingId('new')
    setDraft({ ...EMPTY_CREDENTIAL })
    setError('')
  }
  const cancel = () => {
    setEditingId(null)
    setDraft({ ...EMPTY_CREDENTIAL })
    setError('')
  }
  const save = async () => {
    if (saving || !editingId) return
    setSaving(true)
    setError('')
    try {
      const next = await saveWebApiCredential(asset.id, editingId === 'new' ? newID() : editingId, draft)
      onAsset(next)
      cancel()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setSaving(false)
    }
  }
  const remove = async (credential: WebApiAssetCredential) => {
    const ok = await confirm({
      description: t('webapi.assetCredDeleteConfirm', { label: credential.label || credential.username || '—' }),
      destructive: true,
    })
    if (!ok) return
    setError('')
    try {
      onAsset(await deleteWebApiCredential(asset.id, credential.id))
    } catch (err) {
      setError(errorText(err))
    }
  }

  const editor = (
    <div className="grid grid-cols-2 gap-2 border-t border-border/60 bg-muted/20 px-2 py-2.5">
      <Field label={t('webapi.assetCredLabel')}>
        <Input value={draft.label} onChange={(event) => setDraft((current) => ({ ...current, label: event.target.value }))} className="h-9" disabled={saving} />
      </Field>
      <Field label={t('webapi.assetCredUsername')}>
        <Input value={draft.username} onChange={(event) => setDraft((current) => ({ ...current, username: event.target.value }))} className="h-9" disabled={saving} />
      </Field>
      <Field label={t('webapi.assetCredSecret')}>
        <Input type="password" value={draft.secret} onChange={(event) => setDraft((current) => ({ ...current, secret: event.target.value }))} className="h-9" placeholder={t('webapi.assetCredKeepSecret')} autoComplete="new-password" disabled={saving} />
      </Field>
      <Field label={t('webapi.assetCredNote')}>
        <Input value={draft.note} onChange={(event) => setDraft((current) => ({ ...current, note: event.target.value }))} className="h-9" disabled={saving} />
      </Field>
      <div className="col-span-2 flex items-center justify-end gap-1">
        <Button type="button" variant="ghost" size="xs" onClick={cancel} disabled={saving} className="text-muted-foreground">{t('webapi.assetCredCancel')}</Button>
        <Button type="button" size="xs" onClick={() => void save()} disabled={saving}>
          {saving && <Spinner className="mr-1 h-3 w-3" />}
          {t('webapi.assetCredSave')}
        </Button>
      </div>
    </div>
  )

  return (
    <RailCard
      title={t('webapi.assetCredentials')}
      actions={(
        <Button type="button" variant="outline" size="xs" onClick={beginNew} disabled={editingId !== null} className="gap-1">
          <Plus className="h-3.5 w-3.5" />
          {t('webapi.assetCredAdd')}
        </Button>
      )}
    >
      {asset.credentials.length === 0 && editingId !== 'new' ? (
        <p className="py-2 text-[12px] text-muted-foreground">{t('webapi.assetCredEmpty')}</p>
      ) : (
        <ul className="divide-y divide-border/60">
          {asset.credentials.map((credential) => (
            editingId === credential.id ? (
              <li key={credential.id}>{editor}</li>
            ) : (
              <li key={credential.id} className="flex min-w-0 items-center gap-2 py-2">
                <div className="min-w-0 flex-1">
                  <div className="truncate text-[13px] font-medium text-foreground" title={credential.note || undefined}>
                    {credential.label || '—'}
                  </div>
                  <div className="flex min-w-0 items-center gap-1.5 text-[12px] text-muted-foreground">
                    <span className="truncate font-mono text-[11px] text-secondary-foreground">{credential.username || '—'}</span>
                    <span aria-hidden>·</span>
                    <span>{credential.has_secret ? t('webapi.assetCredSecretMasked') : t('webapi.assetCredUnset')}</span>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-0.5">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button type="button" variant="ghost" size="icon-xs" onClick={() => beginEdit(credential)} disabled={editingId !== null} aria-label={t('webapi.assetCredEdit')} className="text-muted-foreground">
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t('webapi.assetCredEdit')}</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button type="button" variant="ghost" size="icon-xs" onClick={() => void remove(credential)} disabled={editingId !== null} aria-label={t('webapi.assetCredDelete')} className="text-muted-foreground hover:text-destructive">
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>{t('webapi.assetCredDelete')}</TooltipContent>
                  </Tooltip>
                </div>
              </li>
            )
          ))}
          {editingId === 'new' && <li>{editor}</li>}
        </ul>
      )}
      {error && <p role="alert" className="mt-1 text-xs text-destructive">{error}</p>}
    </RailCard>
  )
}

function ScheduleSection({ asset, schedule, onScheduleChange, onOpenTask, onRunsChanged }: {
  asset: WebApiAsset
  schedule: WebApiSchedule | null
  onScheduleChange: (schedule: WebApiSchedule | null) => void
  onOpenTask: (sessionID: string) => void
  /** 第一页运行记录刷新后回调首条运行 ID（无运行传空串）。 */
  onRunsChanged?: (key: string) => void
}) {
  const { t } = useTranslation('chat')
  const confirm = useConfirm()
  const [cron, setCron] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [autoVerify, setAutoVerify] = useState(true)
  const [autoRetest, setAutoRetest] = useState(true)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState('')

  const [runs, setRuns] = useState<WebApiScheduleRun[]>([])
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsError, setRunsError] = useState('')
  const [openingTask, setOpeningTask] = useState('')
  const runSeq = useRef(0)

  useEffect(() => {
    if (!schedule) return
    setCron(schedule.cron)
    setEnabled(schedule.enabled)
    setAutoVerify(schedule.auto_verify)
    setAutoRetest(schedule.auto_retest)
  }, [schedule?.updated_at]) // eslint-disable-line react-hooks/exhaustive-deps

  const loadRuns = useCallback(async (mode: 'replace' | 'append' | 'refresh', cursor?: string) => {
    const seq = ++runSeq.current
    if (mode !== 'refresh') setRunsLoading(true)
    setRunsError('')
    try {
      const result = await listWebApiScheduleRuns(asset.id, cursor)
      if (seq !== runSeq.current) return
      setRuns((prev) => {
        if (mode === 'append') {
          const seen = new Set(prev.map((run) => run.id))
          return [...prev, ...result.items.filter((run) => !seen.has(run.id))]
        }
        if (mode === 'refresh') {
          const seen = new Set(result.items.map((run) => run.id))
          return [...result.items, ...prev.filter((run) => !seen.has(run.id))]
        }
        return result.items
      })
      // 只要拿到第一页（replace/refresh，或首屏 append）就通知关联任务侧；
      // 值相同（同一首条运行）时 React 不会重复触发下游 effect。
      if (!cursor) onRunsChanged?.(result.items[0]?.id ?? '')
    } catch (err) {
      if (seq !== runSeq.current) return
      if (mode !== 'refresh') setRunsError(errorText(err))
    } finally {
      if (seq === runSeq.current && mode !== 'refresh') setRunsLoading(false)
    }
  }, [asset.id, onRunsChanged])

  useEffect(() => { void loadRuns('replace') }, [loadRuns])

  const urgent = !!schedule?.enabled && !!schedule.next_run_at && Date.parse(schedule.next_run_at) - Date.now() <= 2 * 60 * 1000
  const followupsRunning = runs.some((run) => run.followups?.state === 'waiting' || run.followups?.state === 'in_progress')
  usePolling(() => { void loadRuns('refresh') }, 15000, urgent || followupsRunning)

  const save = async () => {
    if (saving) return
    if (!cron.trim()) { setError(t('webapi.scheduleCronRequired')); return }
    setSaving(true)
    setError('')
    try {
      onScheduleChange(await saveWebApiSchedule(asset.id, {
        cron: cron.trim(),
        enabled,
        auto_verify: autoVerify,
        auto_retest: autoRetest,
      }))
      setEditing(false)
    } catch (err) {
      setError(errorText(err))
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (deleting) return
    if (!(await confirm({ description: t('webapi.scheduleDeleteConfirm'), destructive: true }))) return
    setDeleting(true)
    setError('')
    try {
      await deleteWebApiSchedule(asset.id)
      onScheduleChange(null)
      setCron('')
    } catch (err) {
      setError(errorText(err))
    } finally {
      setDeleting(false)
    }
  }

  const openTask = async (taskId: string) => {
    if (!taskId || openingTask) return
    setOpeningTask(taskId)
    try {
      const task = await getWebApiTask(taskId)
      onOpenTask(task.initial_session_id)
    } catch (err) {
      setRunsError(errorText(err))
    } finally {
      setOpeningTask('')
    }
  }

  // 卡标题右侧：无计划为「设置计划」，有则为「修改计划」；表单点开才显示。
  const [editing, setEditing] = useState(false)
  const showForm = editing
  const restUpcoming = schedule?.upcoming?.slice(1) ?? []
  // 运行记录时间线：最新 5 条；圆点颜色 launched 蓝 / failed 红 / skipped 灰。
  const timelineRuns = runs.slice(0, 5)

  return (
    <>
      <RailCard
        title={t('webapi.scheduleSection')}
        actions={(
          <Button type="button" variant="outline" size="xs" onClick={() => setEditing((current) => !current)}>
            {schedule ? (showForm ? t('webapi.scheduleCancelEdit') : t('webapi.scheduleEdit')) : t('webapi.scheduleSet')}
          </Button>
        )}
      >
        {schedule ? (
          <div className="space-y-1 text-[12px]">
            <div className="flex flex-wrap items-center gap-1.5">
              <ScheduleStateBadge enabled={schedule.enabled} />
              {schedule.enabled && schedule.next_run_at ? (
                <>
                  <span className="text-muted-foreground">{t('shell.scheduleNext')}</span>
                  <span className="font-mono tabular text-foreground">{formatDateTime(schedule.next_run_at)}</span>
                </>
              ) : (
                <span className="text-muted-foreground">{t('webapi.scheduleDisabled')}</span>
              )}
            </div>
            {schedule.enabled && restUpcoming.length > 0 && (
              <span className="block text-[11px] text-muted-foreground/70">{restUpcoming.map((item) => formatDateTime(item)).join(', ')} · {schedule.timezone}</span>
            )}
          </div>
        ) : !showForm ? (
          <p className="text-[12px] text-muted-foreground">{t('webapi.scheduleNone')}</p>
        ) : null}

        {showForm && (
          <div className="mt-3 space-y-3 border-t border-border pt-3">
            <div className="flex flex-wrap items-center gap-2">
              <Input
                value={cron}
                onChange={(event) => setCron(event.target.value)}
                placeholder="0 3 * * *"
                className="h-9 w-[180px] font-mono text-[12px]"
                aria-label={t('webapi.scheduleCron')}
                disabled={saving || deleting}
              />
              {CRON_PRESETS.map((preset) => (
                <Chip key={preset.value} type="button" active={cron === preset.value} onClick={() => setCron(preset.value)} disabled={saving || deleting}>
                  {t(preset.key)}
                </Chip>
              ))}
            </div>
            <div className="divide-y divide-border/60 border-y border-border/60">
              <SwitchRow label={t('webapi.scheduleEnabled')}>
                <Switch checked={enabled} onCheckedChange={setEnabled} disabled={saving || deleting} aria-label={t('webapi.scheduleEnabled')} />
              </SwitchRow>
              <SwitchRow label={t('webapi.scheduleAutoVerify')}>
                <Switch checked={autoVerify} onCheckedChange={setAutoVerify} disabled={saving || deleting} aria-label={t('webapi.scheduleAutoVerify')} />
              </SwitchRow>
              <SwitchRow label={t('webapi.scheduleAutoRetest')}>
                <Switch checked={autoRetest} onCheckedChange={setAutoRetest} disabled={saving || deleting} aria-label={t('webapi.scheduleAutoRetest')} />
              </SwitchRow>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button type="button" size="xs" onClick={() => { void save(); }} disabled={saving || deleting}>
                {saving && <Spinner className="mr-1 h-3 w-3" />}
                {t('webapi.scheduleSave')}
              </Button>
              {schedule && (
                <Button type="button" variant="ghost" size="xs" onClick={() => void remove()} disabled={saving || deleting} className="text-muted-foreground hover:text-destructive">
                  {deleting && <Spinner className="mr-1 h-3 w-3" />}
                  {t('webapi.scheduleDelete')}
                </Button>
              )}
              {error && <span role="alert" className="min-w-0 text-xs text-destructive">{error}</span>}
            </div>
          </div>
        )}
      </RailCard>

      <RailCard title={t('webapi.scheduleRuns')}>
        {runsLoading && runs.length === 0 ? (
          <div className="flex items-center gap-2 py-2 text-[12px] text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" />{t('webapi.loading')}</div>
        ) : runsError ? (
          <div className="flex items-center gap-2 py-2 text-[12px] text-destructive"><span className="min-w-0 truncate" title={runsError}>{runsError}</span><Button type="button" variant="ghost" size="xs" onClick={() => void loadRuns('replace')}>{t('webapi.retry')}</Button></div>
        ) : timelineRuns.length === 0 ? (
          <p className="py-2 text-[12px] text-muted-foreground">{t('webapi.scheduleRunsEmpty')}</p>
        ) : (
          <ol className="relative ml-2 space-y-3 border-l border-border">
            {timelineRuns.map((run) => (
              <li key={run.id} className="relative pl-4">
                <span
                  aria-hidden
                  className="absolute -left-[4.5px] top-1.5 h-2 w-2 rounded-full"
                  style={{ background: run.status === 'launched' ? '#2563EB' : run.status === 'failed' ? 'var(--st-new-dot)' : 'var(--st-unsup-dot)' }}
                />
                <div className="flex justify-between gap-2">
                  <RunStateBadge status={run.status} />
                  <span className="shrink-0 font-mono text-[11px] tabular text-muted-foreground">{formatShortDateTime(run.scheduled_for)}</span>
                </div>
                <div className="mt-0.5 flex items-start gap-1.5 text-[11px] text-muted-foreground">
                  <span className="line-clamp-2 min-w-0" title={run.error || run.task_id}>
                    {run.error || (run.task_id ? t('webapi.scheduleRunTask') : '—')}
                  </span>
                </div>
                {run.task_id && (
                  <button
                    type="button"
                    onClick={() => void openTask(run.task_id)}
                    disabled={!!openingTask}
                    className="mt-0.5 inline-flex items-center gap-1 text-[11px] text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                  >
                    {openingTask === run.task_id && <Loader2 className="h-3 w-3 animate-spin" />}
                    {t('webapi.scheduleOpenTask')}
                  </button>
                )}
              </li>
            ))}
          </ol>
        )}
      </RailCard>
    </>
  )
}


function AssetTasks({ assetId, refreshKey, onOpenTask }: { assetId: string; refreshKey: string; onOpenTask: (sessionID: string) => void }) {
  const { t } = useTranslation('chat')
  const [tasks, setTasks] = useState<WebApiAssetTaskItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const loadedAssetRef = useRef('')

  useEffect(() => {
    let cancelled = false
    // 只有首次加载/切换资产显示 loading；计划运行触发的静默刷新保留现有列表。
    if (loadedAssetRef.current !== assetId) setLoading(true)
    setError('')
    listWebApiAssetTasks(assetId)
      .then((items) => { if (!cancelled) setTasks(items) })
      .catch((err) => { if (!cancelled) setError(errorText(err)) })
      .finally(() => {
        if (cancelled) return
        loadedAssetRef.current = assetId
        setLoading(false)
      })
    return () => { cancelled = true }
  }, [assetId, refreshKey])

  return (
    <RailCard title={t('webapi.assetTasks')}>
      {loading ? (
        <div className="flex items-center gap-2 py-2 text-[12px] text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" />{t('webapi.loading')}</div>
      ) : error ? (
        <p className="py-2 text-[12px] text-destructive">{error}</p>
      ) : tasks.length === 0 ? (
        <p className="py-2 text-[12px] text-muted-foreground">{t('webapi.assetTasksEmpty')}</p>
      ) : (
        <ul className="-my-1 divide-y divide-border/60">
          {tasks.map((task) => (
            <li key={task.task_id}>
              <button
                type="button"
                onClick={() => onOpenTask(task.initial_session_id)}
                className="flex w-full min-w-0 items-center gap-2 py-2 text-left transition-colors hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <span className="shrink-0 font-mono text-[11px] tabular text-muted-foreground">{formatShortDateTime(task.created_at)}</span>
                <span className="min-w-0 flex-1 truncate text-[12px] text-foreground" title={task.objective}>{task.objective}</span>
                <span className="shrink-0 text-[12px] tabular text-secondary-foreground">{task.finding_count}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </RailCard>
  )
}
