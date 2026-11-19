import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Archive, ArchiveRestore, ChevronDown, ChevronRight, Loader2, Plus, RefreshCw, Rocket, Trash2 } from 'lucide-react'
import { newID } from '@cyber/aop'
import { cn } from '@cyber/theme'
import {
  Badge, Button, Chip, EmptyState, Field, Input, SkeletonList, Spinner,
  Switch, Textarea, Tooltip, TooltipContent, TooltipTrigger, useConfirm,
} from '@cyber/ui'
import {
  deleteWebApiCredential,
  deleteWebApiSchedule,
  getWebApiAsset,
  getWebApiTask,
  listWebApiAssetTasks,
  listWebApiAssets,
  listWebApiScheduleFollowups,
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
  type WebApiFollowupItem,
  type WebApiRunFollowups,
  type WebApiSchedule,
  type WebApiScheduleRun,
} from '../api'
import { usePolling } from '../hooks/usePolling'
import { formatTime } from './WebApiEvidenceReference'
import { normalizeWebApiPolicy, useWebApiPolicySummary, WebApiPolicyFields } from './WebApiPolicyShared'
import WebApiAssetLaunchDialog from './WebApiAssetLaunchDialog'
import './WebApiAssetsPanel.css'

interface Props {
  focusAssetId: string | null
  onFocusConsumed: () => void
  onOpenTask: (sessionID: string) => void
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
}

function formFromAsset(asset: WebApiAsset | null): AssetForm {
  const policy = normalizeWebApiPolicy(asset ? {
    scope: asset.scope,
    approval: asset.approval,
    is_default: !asset.scope && !asset.approval,
  } : null)
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

// 资产视图：左列表（360px @≥1100px）+ 右详情。详情内为基本信息表单、测试账号、
// 周期计划与关联任务，均为原生 /api/webapi 接口，不读其它来源。
export default function WebApiAssetsPanel({ focusAssetId, onFocusConsumed, onOpenTask }: Props) {
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
        if (asset.archived !== showArchived) setShowArchived(asset.archived)
      } catch (err) {
        setListError(errorText(err))
      } finally {
        onFocusConsumed()
      }
    })()
  }, [focusAssetId, assets, loading, showArchived, onFocusConsumed])

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
    <div className="assets-container min-h-0 min-w-0 flex-1">
      <div className={cn('assets-workspace', showDetail && 'assets-show-detail')}>
        <div className="assets-list flex min-h-0 min-w-0 flex-col border-border/60">
          <div className="flex h-10 shrink-0 items-center justify-between gap-2 border-b border-border/60 px-3">
            <span className="flex min-w-0 items-center gap-2 text-[13px] font-medium text-foreground">
              {t('webapi.assetsView')}
              <span className="font-mono text-[11px] tabular-nums text-muted-foreground">{visibleAssets.length}</span>
            </span>
            <span className="flex shrink-0 items-center gap-1.5">
              <label className="flex cursor-pointer items-center gap-1 text-[12px] text-muted-foreground">
                <Switch
                  checked={showArchived}
                  onCheckedChange={setShowArchived}
                  className="scale-75"
                  aria-label={t('webapi.assetShowArchived')}
                />
                {t('webapi.assetShowArchived')}
              </label>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button type="button" variant="ghost" size="icon-xs" onClick={() => void load(showArchived)} disabled={loading} aria-label={t('webapi.refresh')} className="text-muted-foreground">
                    {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t('webapi.refresh')}</TooltipContent>
              </Tooltip>
              <Button type="button" size="xs" onClick={startCreate} className="gap-1 text-[12px]">
                <Plus className="h-3.5 w-3.5" />
                {t('webapi.assetNew')}
              </Button>
            </span>
          </div>

          <div className="shrink-0 border-b border-border/60 px-3 py-2">
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('webapi.assetSearchPlaceholder')}
              className="h-7 text-[12px]"
              aria-label={t('webapi.assetSearchPlaceholder')}
            />
          </div>

          <div className="min-h-0 flex-1 overflow-auto">
            {loading && assets.length === 0 ? (
              <SkeletonList count={5} className="p-3" />
            ) : listError ? (
              <div className="flex flex-col items-center gap-2 px-3 py-4 text-center text-sm">
                <span className="text-destructive">{listError}</span>
                <Button type="button" variant="outline" size="xs" onClick={() => void load(showArchived)}>{t('webapi.refresh')}</Button>
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
                    schedule={schedules[asset.id] ?? null}
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
              onSaved={handleSaved}
              onAssetUpdated={updateAsset}
              onScheduleChange={handleScheduleChange}
              onOpenTask={onOpenTask}
              onBack={() => setShowDetail(false)}
            />
          ) : selected ? (
            <AssetDetail
              key={selected.id}
              asset={selected}
              creatingId={null}
              schedule={schedules[selected.id] ?? null}
              onSaved={handleSaved}
              onAssetUpdated={updateAsset}
              onScheduleChange={handleScheduleChange}
              onOpenTask={onOpenTask}
              onBack={() => setShowDetail(false)}
            />
          ) : (
            <div className="flex flex-1 items-center justify-center px-4 text-sm text-muted-foreground">{t('webapi.assetSelect')}</div>
          )}
        </div>
      </div>
    </div>
  )
}

function AssetRow({ asset, schedule, selected, onSelect }: {
  asset: WebApiAsset
  schedule: WebApiSchedule | null
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
  const originText = asset.origins.length > 0
    ? `${asset.origins[0]}${asset.origins.length > 1 ? ` +${asset.origins.length - 1}` : ''}`
    : ''
  const scheduleText = schedule
    ? (schedule.enabled && schedule.next_run_at
      ? t('webapi.scheduleNextShort', { time: formatTime(schedule.next_run_at) })
      : t('webapi.scheduleDisabled'))
    : ''
  const meta = [asset.owner, originText, policySummary.text, scheduleText].filter(Boolean).join(' · ')

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-pressed={selected}
      className={cn(
        'flex w-full min-w-0 flex-col gap-1 px-3 py-2 text-left transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
        selected && 'bg-accent',
        asset.archived && 'opacity-60',
      )}
    >
      <span className="flex w-full min-w-0 items-center gap-2">
        <span className="assets-list-title min-w-0 flex-1 text-[13px] font-medium text-foreground">{asset.name}</span>
        {asset.archived && <span className="shrink-0 text-[11px] text-muted-foreground">{t('webapi.assetArchivedBadge')}</span>}
        {asset.environment && <Badge variant="muted" size="sm" className="shrink-0">{asset.environment}</Badge>}
      </span>
      <span className="block w-full truncate text-[12px] text-muted-foreground">{meta || '—'}</span>
    </button>
  )
}

function DetailSection({ title, first, children }: { title?: ReactNode; first?: boolean; children: ReactNode }) {
  return (
    <section className={cn(!first && 'mt-4 border-t border-border/60 pt-4')}>
      {title && <h3 className="mb-2 text-[13px] font-medium text-foreground">{title}</h3>}
      {children}
    </section>
  )
}

function AssetDetail({ asset, creatingId, schedule, onSaved, onAssetUpdated, onScheduleChange, onOpenTask, onBack }: {
  asset: WebApiAsset | null
  creatingId: string | null
  schedule: WebApiSchedule | null
  onSaved: (asset: WebApiAsset) => void
  onAssetUpdated: (asset: WebApiAsset) => void
  onScheduleChange: (assetId: string, schedule: WebApiSchedule | null) => void
  onOpenTask: (sessionID: string) => void
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
  // 计划运行刷新后的首条运行 ID：变化即让关联任务静默重取，无需整页刷新。
  const [tasksKey, setTasksKey] = useState('')
  const savedTimer = useRef<number | null>(null)

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
        <div className="assets-detail-column px-5 py-4">
          <div className="mb-4 flex flex-wrap items-center gap-2">
            <Button type="button" variant="ghost" size="xs" onClick={onBack} className="assets-back -ml-2 shrink-0 text-muted-foreground">
              {t('webapi.assetBack')}
            </Button>
            <h2 className="min-w-0 truncate text-[15px] font-semibold text-foreground" title={creating ? undefined : asset.name}>
              {creating ? t('webapi.assetNewTitle') : asset.name}
            </h2>
            {!creating && asset.environment && <Badge variant="muted" size="sm">{asset.environment}</Badge>}
            {!creating && asset.archived && <Badge variant="muted" size="sm">{t('webapi.assetArchivedBadge')}</Badge>}
            {!creating && (
              <div className="ml-auto flex shrink-0 items-center gap-2">
                <Button type="button" size="xs" onClick={() => setLaunchOpen(true)} className="gap-1">
                  <Rocket className="h-3.5 w-3.5" />
                  {t('webapi.assetLaunch')}
                </Button>
                <Button type="button" variant="outline" size="xs" onClick={() => void toggleArchive()} disabled={archiving} className="gap-1">
                  {archiving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : asset.archived ? <ArchiveRestore className="h-3.5 w-3.5" /> : <Archive className="h-3.5 w-3.5" />}
                  {asset.archived ? t('webapi.assetUnarchive') : t('webapi.assetArchive')}
                </Button>
              </div>
            )}
          </div>

          <DetailSection title={t('webapi.assetBasicInfo')} first>
            <div className="assets-form-grid">
              <Field label={`${t('webapi.assetName')}*`}>
                <Input value={form.name} onChange={(event) => patch({ name: event.target.value })} disabled={saving} />
              </Field>
              <Field label={t('webapi.assetOwner')}>
                <Input value={form.owner} onChange={(event) => patch({ owner: event.target.value })} disabled={saving} />
              </Field>
              <Field label={t('webapi.assetEnvironment')}>
                <Input value={form.environment} onChange={(event) => patch({ environment: event.target.value })} placeholder={t('webapi.assetEnvironmentPlaceholder')} disabled={saving} />
              </Field>
              <div className="assets-form-col space-y-1.5">
                <span className="block text-[13px] font-medium text-foreground/75">{t('webapi.assetOrigins')}*</span>
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
              <div className="assets-form-col">
                <WebApiPolicyFields
                  scope={form.scope}
                  approval={form.approval}
                  onScopeChange={(scope) => patch({ scope })}
                  onApprovalChange={(approval) => patch({ approval })}
                  disabled={saving}
                />
              </div>
            </div>

            <div className="mt-3 flex min-h-7 items-center justify-end gap-2">
              {saveError && <span role="alert" className="mr-auto min-w-0 text-xs text-destructive">{saveError}</span>}
              {savedFlash && <span className="text-xs text-muted-foreground">{t('webapi.assetSaved')}</span>}
              <Button type="button" size="xs" onClick={() => void save()} disabled={saving || !dirty}>
                {saving && <Spinner className="h-3.5 w-3.5" />}
                {t('webapi.assetSave')}
              </Button>
            </div>
          </DetailSection>

          {!creating && (
            <DetailSection title={t('webapi.assetCredentials')}>
              <CredentialsSection asset={asset} onAsset={onAssetUpdated} />
            </DetailSection>
          )}

          {!creating && (
            <DetailSection title={t('webapi.scheduleSection')}>
              <ScheduleSection
                asset={asset}
                schedule={schedule}
                onScheduleChange={(next) => onScheduleChange(asset.id, next)}
                onOpenTask={onOpenTask}
                onRunsChanged={setTasksKey}
              />
            </DetailSection>
          )}

          {!creating && (
            <DetailSection title={t('webapi.assetTasks')}>
              <AssetTasks assetId={asset.id} refreshKey={tasksKey} onOpenTask={onOpenTask} />
            </DetailSection>
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
    <div className="flex flex-wrap items-end gap-2 py-2">
      <Field label={t('webapi.assetCredLabel')} className="min-w-[8rem] flex-1">
        <Input value={draft.label} onChange={(event) => setDraft((current) => ({ ...current, label: event.target.value }))} className="h-8" disabled={saving} />
      </Field>
      <Field label={t('webapi.assetCredUsername')} className="min-w-[8rem] flex-1">
        <Input value={draft.username} onChange={(event) => setDraft((current) => ({ ...current, username: event.target.value }))} className="h-8" disabled={saving} />
      </Field>
      <Field label={t('webapi.assetCredSecret')} className="min-w-[8rem] flex-1">
        <Input type="password" value={draft.secret} onChange={(event) => setDraft((current) => ({ ...current, secret: event.target.value }))} className="h-8" placeholder={t('webapi.assetCredKeepSecret')} autoComplete="new-password" disabled={saving} />
      </Field>
      <Field label={t('webapi.assetCredNote')} className="min-w-[8rem] flex-1">
        <Input value={draft.note} onChange={(event) => setDraft((current) => ({ ...current, note: event.target.value }))} className="h-8" disabled={saving} />
      </Field>
      <div className="flex items-center gap-1 pb-0.5">
        <Button type="button" size="xs" onClick={() => void save()} disabled={saving}>
          {saving && <Spinner className="h-3.5 w-3.5" />}
          {t('webapi.assetCredSave')}
        </Button>
        <Button type="button" variant="ghost" size="xs" onClick={cancel} disabled={saving} className="text-muted-foreground">{t('webapi.assetCredCancel')}</Button>
      </div>
    </div>
  )

  return (
    <div>
      <table className="w-full table-fixed text-[12px]">
        <thead>
          <tr className="text-left text-muted-foreground">
            <th className="w-[18%] py-1 pr-2 font-normal">{t('webapi.assetCredLabel')}</th>
            <th className="w-[18%] py-1 pr-2 font-normal">{t('webapi.assetCredUsername')}</th>
            <th className="w-[14%] py-1 pr-2 font-normal">{t('webapi.assetCredSecret')}</th>
            <th className="py-1 pr-2 font-normal">{t('webapi.assetCredNote')}</th>
            <th className="w-[6rem] py-1 text-right font-normal">{t('webapi.assetCredActions')}</th>
          </tr>
        </thead>
        <tbody>
          {asset.credentials.map((credential) => (
            editingId === credential.id ? (
              <tr key={credential.id} className="border-t border-border/40">
                <td colSpan={5}>{editor}</td>
              </tr>
            ) : (
              <tr key={credential.id} className="border-t border-border/40">
                <td className="truncate py-1.5 pr-2">{credential.label || '—'}</td>
                <td className="truncate py-1.5 pr-2 font-mono">{credential.username || '—'}</td>
                <td className="py-1.5 pr-2 text-muted-foreground">{credential.has_secret ? t('webapi.assetCredSet') : t('webapi.assetCredUnset')}</td>
                <td className="truncate py-1.5 pr-2 text-muted-foreground" title={credential.note}>{credential.note || ''}</td>
                <td className="whitespace-nowrap py-1.5 text-right">
                  <Button type="button" variant="ghost" size="xs" onClick={() => beginEdit(credential)} disabled={editingId !== null} className="h-6 px-1 text-[12px] text-muted-foreground">{t('webapi.assetCredEdit')}</Button>
                  <Button type="button" variant="ghost" size="xs" onClick={() => void remove(credential)} disabled={editingId !== null} className="h-6 px-1 text-[12px] text-muted-foreground hover:text-destructive">{t('webapi.assetCredDelete')}</Button>
                </td>
              </tr>
            )
          ))}
          {editingId === 'new' && (
            <tr className="border-t border-border/40">
              <td colSpan={5}>{editor}</td>
            </tr>
          )}
        </tbody>
      </table>
      <Button type="button" variant="ghost" size="xs" onClick={beginNew} disabled={editingId !== null} className="mt-2 gap-1 text-muted-foreground">
        <Plus className="h-3.5 w-3.5" />
        {t('webapi.assetCredAdd')}
      </Button>
      {error && <p role="alert" className="mt-1 text-xs text-destructive">{error}</p>}
    </div>
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
  const [editing, setEditing] = useState(false)
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
  const [hasMore, setHasMore] = useState(false)
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [expandedRun, setExpandedRun] = useState<string | null>(null)
  const [followups, setFollowups] = useState<Record<string, WebApiFollowupItem[]>>({})
  const [followupLoading, setFollowupLoading] = useState<string | null>(null)
  const [followupError, setFollowupError] = useState('')
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
      setHasMore(result.has_more)
      setNextCursor(result.next_cursor)
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
      setEditing(false)
      setCron('')
    } catch (err) {
      setError(errorText(err))
    } finally {
      setDeleting(false)
    }
  }

  const toggleFollowups = async (run: WebApiScheduleRun) => {
    if (expandedRun === run.id) {
      setExpandedRun(null)
      return
    }
    setExpandedRun(run.id)
    setFollowupError('')
    if (followups[run.id]) return
    setFollowupLoading(run.id)
    try {
      const items = await listWebApiScheduleFollowups(run.id)
      setFollowups((prev) => ({ ...prev, [run.id]: items }))
    } catch (err) {
      setFollowupError(errorText(err))
    } finally {
      setFollowupLoading((current) => (current === run.id ? null : current))
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

  const showForm = !!schedule || editing
  const restUpcoming = schedule?.upcoming?.slice(1) ?? []

  return (
    <div>
      {schedule ? (
        <p className="mb-2 flex flex-wrap items-baseline gap-x-2 text-[12px] text-muted-foreground">
          <span>{schedule.enabled && schedule.next_run_at
            ? t('webapi.scheduleNextRun', { time: formatTime(schedule.next_run_at) })
            : t('webapi.scheduleDisabled')}</span>
          {restUpcoming.length > 0 && <span className="text-muted-foreground/70">{restUpcoming.map((item) => formatTime(item)).join(', ')}</span>}
          <span className="text-muted-foreground/70">{schedule.timezone}</span>
        </p>
      ) : !showForm ? (
        <div className="flex items-center gap-2 text-[12px] text-muted-foreground">
          <span>{t('webapi.scheduleNone')}</span>
          <Button type="button" variant="outline" size="xs" onClick={() => setEditing(true)}>{t('webapi.scheduleSet')}</Button>
        </div>
      ) : null}

      {showForm && (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <Input
              value={cron}
              onChange={(event) => setCron(event.target.value)}
              placeholder="0 2 * * *"
              className="h-8 w-[200px] font-mono text-[12px]"
              aria-label={t('webapi.scheduleCron')}
              disabled={saving || deleting}
            />
            {CRON_PRESETS.map((preset) => (
              <Chip key={preset.value} type="button" active={cron === preset.value} onClick={() => setCron(preset.value)} disabled={saving || deleting}>
                {t(preset.key)}
              </Chip>
            ))}
          </div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-[12px]">
            <label className="flex cursor-pointer items-center gap-1.5">
              <Switch checked={enabled} onCheckedChange={setEnabled} className="scale-75" disabled={saving || deleting} />
              {t('webapi.scheduleEnabled')}
            </label>
            <label className="flex cursor-pointer items-center gap-1.5">
              <Switch checked={autoVerify} onCheckedChange={setAutoVerify} className="scale-75" disabled={saving || deleting} />
              {t('webapi.scheduleAutoVerify')}
            </label>
            <label className="flex cursor-pointer items-center gap-1.5">
              <Switch checked={autoRetest} onCheckedChange={setAutoRetest} className="scale-75" disabled={saving || deleting} />
              {t('webapi.scheduleAutoRetest')}
            </label>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" size="xs" onClick={() => void save()} disabled={saving || deleting}>
              {saving && <Spinner className="h-3.5 w-3.5" />}
              {t('webapi.scheduleSave')}
            </Button>
            {schedule && (
              <Button type="button" variant="ghost" size="xs" onClick={() => void remove()} disabled={saving || deleting} className="text-muted-foreground hover:text-destructive">
                {deleting && <Spinner className="h-3.5 w-3.5" />}
                {t('webapi.scheduleDelete')}
              </Button>
            )}
            {error && <span role="alert" className="min-w-0 text-xs text-destructive">{error}</span>}
          </div>
        </div>
      )}

      <div className="mt-3 border-t border-border/60 pt-3">
        <h4 className="mb-1.5 text-[13px] font-medium text-foreground">{t('webapi.scheduleRuns')}</h4>
        {runsLoading && runs.length === 0 ? (
          <div className="flex items-center gap-2 py-2 text-[12px] text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" />{t('webapi.loading')}</div>
        ) : runsError ? (
          <div className="flex items-center gap-2 py-2 text-[12px] text-destructive"><span className="min-w-0 truncate" title={runsError}>{runsError}</span><Button type="button" variant="ghost" size="xs" onClick={() => void loadRuns('replace')}>{t('webapi.retry')}</Button></div>
        ) : runs.length === 0 ? (
          <p className="py-2 text-[12px] text-muted-foreground">{t('webapi.scheduleRunsEmpty')}</p>
        ) : (
          <>
            <table className="w-full table-fixed text-[12px]">
              <thead>
                <tr className="text-left text-muted-foreground">
                  <th className="w-[7.5rem] py-1 pr-2 font-normal">{t('webapi.scheduleRunTime')}</th>
                  <th className="w-[4rem] py-1 pr-2 font-normal">{t('webapi.scheduleRunStatus')}</th>
                  <th className="w-[4rem] py-1 pr-2 font-normal">{t('webapi.scheduleRunTask')}</th>
                  <th className="w-[40%] py-1 pr-2 font-normal">{t('webapi.scheduleRunFollowups')}</th>
                  <th className="py-1 font-normal">{t('webapi.scheduleRunReason')}</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => (
                  <Fragment key={run.id}>
                    <tr className="border-t border-border/40 align-top">
                      <td className="whitespace-nowrap py-1.5 pr-2 text-muted-foreground">{formatTime(run.scheduled_for)}</td>
                      <td className="py-1.5 pr-2"><RunStatusBadge status={run.status} /></td>
                      <td className="py-1.5 pr-2">
                        {run.task_id ? (
                          <Button type="button" variant="ghost" size="xs" onClick={() => void openTask(run.task_id)} disabled={!!openingTask} className="h-6 px-1 text-[12px] text-muted-foreground">
                            {openingTask === run.task_id ? <Loader2 className="h-3 w-3 animate-spin" /> : t('webapi.scheduleOpenTask')}
                          </Button>
                        ) : '—'}
                      </td>
                      <td className="py-1.5 pr-2">
                        {run.followups ? (
                          <button
                            type="button"
                            onClick={() => void toggleFollowups(run)}
                            aria-expanded={expandedRun === run.id}
                            className="flex max-w-full items-center gap-1 text-left text-[12px] text-foreground hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
                          >
                            <span className="min-w-0 truncate">{followupSummaryText(run.followups, t)}</span>
                            {expandedRun === run.id
                              ? <ChevronDown className="h-3 w-3 shrink-0" aria-hidden="true" />
                              : <ChevronRight className="h-3 w-3 shrink-0" aria-hidden="true" />}
                          </button>
                        ) : '—'}
                      </td>
                      <td className="truncate py-1.5 text-muted-foreground" title={run.error}>{run.error || ''}</td>
                    </tr>
                    {expandedRun === run.id && (
                      <tr className="border-t border-border/30 bg-muted/20">
                        <td colSpan={5} className="px-2 py-2">
                          {followupLoading === run.id ? (
                            <div className="flex items-center gap-2 text-[12px] text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" />{t('webapi.loading')}</div>
                          ) : followupError ? (
                            <p className="text-[12px] text-destructive">{followupError}</p>
                          ) : (followups[run.id]?.length ?? 0) === 0 ? (
                            <p className="text-[12px] text-muted-foreground">{t('webapi.followupEmpty')}</p>
                          ) : (
                            <ul className="space-y-1">
                              {followups[run.id].map((item) => (
                                <li key={item.id} className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-[12px]">
                                  <span className="font-medium text-foreground">{item.kind === 'verify' ? t('webapi.followupVerify') : t('webapi.followupRetest')}</span>
                                  <span className="text-muted-foreground">·</span>
                                  <span className="min-w-0 truncate">#{item.finding_number} {item.finding_title}</span>
                                  <span className="text-muted-foreground">·</span>
                                  <FollowupStatus status={item.status} />
                                  {(item.verdict || item.conclusion) && (
                                    <>
                                      <span className="text-muted-foreground">·</span>
                                      <span className="text-muted-foreground">{item.verdict || item.conclusion}</span>
                                    </>
                                  )}
                                  {item.error && (
                                    <>
                                      <span className="text-muted-foreground">·</span>
                                      <span className="text-muted-foreground">{item.error}</span>
                                    </>
                                  )}
                                  {item.session_id && (
                                    <Button type="button" variant="ghost" size="xs" onClick={() => onOpenTask(item.session_id!)} className="h-5 px-1 text-[12px] text-muted-foreground">
                                      {t('webapi.followupViewExecution')}
                                    </Button>
                                  )}
                                </li>
                              ))}
                            </ul>
                          )}
                        </td>
                      </tr>
                    )}
                  </Fragment>
                ))}
              </tbody>
            </table>
            {hasMore && (
              <div className="mt-1.5 text-center">
                <Button type="button" variant="ghost" size="xs" onClick={() => void loadRuns('append', nextCursor ?? undefined)} disabled={runsLoading} className="text-muted-foreground">
                  {runsLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : t('webapi.loadMore')}
                </Button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function RunStatusBadge({ status }: { status: WebApiScheduleRun['status'] }) {
  const { t } = useTranslation('chat')
  if (status === 'launched') return <Badge variant="success" size="sm">{t('webapi.scheduleStatusLaunched')}</Badge>
  if (status === 'failed') return <Badge variant="danger" size="sm">{t('webapi.scheduleStatusFailed')}</Badge>
  return <Badge variant="muted" size="sm">{t('webapi.scheduleStatusSkipped')}</Badge>
}

function FollowupStatus({ status }: { status: WebApiFollowupItem['status'] }) {
  const { t } = useTranslation('chat')
  const label = status === 'launched' ? t('webapi.followupStatusLaunched')
    : status === 'failed' ? t('webapi.followupStatusFailed')
      : status === 'skipped' ? t('webapi.followupStatusSkipped')
        : t('webapi.followupStatusPending')
  const tone = status === 'failed' ? 'text-destructive' : status === 'launched' ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground'
  return <span className={tone}>{label}</span>
}

function followupSummaryText(followups: WebApiRunFollowups, t: (key: string, options?: Record<string, unknown>) => string): string {
  const state = followups.state === 'waiting' ? t('webapi.followupWaiting')
    : followups.state === 'not_planned' ? t('webapi.followupNotPlanned')
      : followups.state === 'completed' ? t('webapi.followupCompleted')
        : t('webapi.followupInProgress')
  const parts = [state]
  const verifyTotal = followups.verify.pending + followups.verify.launched + followups.verify.failed + followups.verify.skipped
  const retestTotal = followups.retest.pending + followups.retest.launched + followups.retest.failed + followups.retest.skipped
  if (verifyTotal > 0) parts.push(t('webapi.followupVerifyProgress', { launched: followups.verify.launched, total: verifyTotal }))
  if (retestTotal > 0) parts.push(t('webapi.followupRetestProgress', { launched: followups.retest.launched, total: retestTotal }))
  return parts.join(' · ')
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

  if (loading) {
    return <div className="flex items-center gap-2 py-2 text-[12px] text-muted-foreground"><Loader2 className="h-3.5 w-3.5 animate-spin" />{t('webapi.loading')}</div>
  }
  if (error) return <p className="py-2 text-[12px] text-destructive">{error}</p>
  if (tasks.length === 0) return <p className="py-2 text-[12px] text-muted-foreground">{t('webapi.assetTasksEmpty')}</p>
  return (
    <table className="w-full table-fixed text-[12px]">
      <thead>
        <tr className="text-left text-muted-foreground">
          <th className="w-[7.5rem] py-1 pr-2 font-normal">{t('webapi.assetTasksTime')}</th>
          <th className="py-1 pr-2 font-normal">{t('webapi.assetTasksObjective')}</th>
          <th className="w-[4.5rem] py-1 text-right font-normal">{t('webapi.assetTasksFindings')}</th>
        </tr>
      </thead>
      <tbody>
        {tasks.map((task) => (
          <tr
            key={task.task_id}
            role="button"
            tabIndex={0}
            onClick={() => onOpenTask(task.initial_session_id)}
            onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onOpenTask(task.initial_session_id) } }}
            className="cursor-pointer border-t border-border/40 transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
          >
            <td className="whitespace-nowrap py-1.5 pr-2 text-muted-foreground">{formatTime(task.created_at)}</td>
            <td className="truncate py-1.5 pr-2 text-foreground" title={task.objective}>{task.objective}</td>
            <td className="py-1.5 text-right font-mono tabular-nums text-muted-foreground">{task.finding_count}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
