import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Boxes, Compass, Globe, ShieldCheck } from 'lucide-react'
import { Button, Callout, Popover, PopoverContent, PopoverTrigger, Spinner } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { getWebApiTask, putWebApiTaskScenario, type WebApiScenario, type WebApiTask, type WebApiTaskPolicy } from '../api'
import { formatDateTime } from '../lib/time'
import { useWebApiTaskViewCounts } from '../hooks/useWebApiTaskViewCounts'
import { normalizeWebApiPolicy, useWebApiPolicySummary, WebApiPolicyFields, WebApiPolicySaver } from './WebApiPolicyShared'
import WebApiScenarioPicker, { normalizeWebApiScenario, scenarioDraftOf, type WebApiScenarioDraft } from './WebApiScenarioPicker'

export type WebApiView = 'execution' | 'traffic' | 'board' | 'findings' | 'reports' | 'usage'

interface Props {
  task: WebApiTask
  /** 策略保存成功后回写 App 的任务对象（App 持有 activeWebApiTask 状态）。 */
  onPolicySaved: (task: WebApiTask) => void
  /** 场景保存成功后回写 App 的任务对象。 */
  onScenarioSaved: (task: WebApiTask) => void
  /** 任务来自资产时，点击资产标签回到资产详情。 */
  onOpenAsset?: (assetId: string) => void
  /** 返回任务列表（离开任务工作区，由 App 导航并离开会话）。 */
  onBack: () => void
  view: WebApiView
  onViewChange: (view: WebApiView) => void
  /** 失效信号：新建验证/报告成功后 App bump，页签计数重取。 */
  countsRevision?: number
}

const VIEW_TABS: { view: WebApiView; labelKey: string; countKey: 'traffic' | 'board' | 'findings' | 'reports' | null }[] = [
  { view: 'execution', labelKey: 'webapi.execution', countKey: null },
  { view: 'traffic', labelKey: 'webapi.traffic', countKey: 'traffic' },
  { view: 'board', labelKey: 'board.tab', countKey: 'board' },
  { view: 'findings', labelKey: 'webapi.findings', countKey: 'findings' },
  { view: 'reports', labelKey: 'webapi.reports', countKey: 'reports' },
  { view: 'usage', labelKey: 'webapi.usageView', countKey: null },
]

// 任务工作区顶栏（高 48px）：返回 · 资产标签 · 任务标题 · origin · 策略指示
// …… 右侧视图页签（替代原左侧任务导航列；计数取数逻辑抽到 useWebApiTaskViewCounts）。
export default function WebApiTaskBar({ task, onPolicySaved, onScenarioSaved, onOpenAsset, onBack, view, onViewChange, countsRevision = 0 }: Props) {
  const { t } = useTranslation('chat')
  const counts = useWebApiTaskViewCounts(task.id, countsRevision)
  const summary = `${task.known_info}\n\n${task.objective}`.trim()
  const policy = normalizeWebApiPolicy(task.policy)
  const summaryView = useWebApiPolicySummary(policy)
  const PolicyIcon = summaryView.icon
  // Popover 打开时重新拉取任务（策略可能在别处被改过），以最新值为编辑基线；
  // saved 是服务端当前值（判断「有改动」的基准），draft 是编辑中的值。
  const [draft, setDraft] = useState<WebApiTaskPolicy>(policy)
  const [saved, setSaved] = useState<WebApiTaskPolicy>(policy)
  const [open, setOpen] = useState(false)
  const [reloadError, setReloadError] = useState('')
  // 场景指示：独立 Popover（策略指示左侧）。打开时同样重新拉取任务，以最新值为基线。
  const [scenarioOpen, setScenarioOpen] = useState(false)
  const [scenarioSaved, setScenarioSaved] = useState<WebApiScenario>(normalizeWebApiScenario(task.scenario))
  const [scenarioDraft, setScenarioDraft] = useState<WebApiScenarioDraft>(scenarioDraftOf(task.scenario))
  const [scenarioSaving, setScenarioSaving] = useState(false)
  const [scenarioError, setScenarioError] = useState('')
  const scenarioName = normalizeWebApiScenario(task.scenario).name || t('scenario.pentestName')
  const scenarioChanged = scenarioDraft.scenario !== scenarioSaved.key
    || scenarioDraft.doneCriteria.trim() !== (scenarioSaved.done_criteria ?? '')
  useEffect(() => {
    if (!open) return
    let cancelled = false
    setDraft(policy)
    setSaved(policy)
    setReloadError('')
    getWebApiTask(task.id)
      .then((fresh) => {
        if (cancelled) return
        const freshPolicy = normalizeWebApiPolicy(fresh.policy)
        setDraft(freshPolicy)
        setSaved(freshPolicy)
      })
      .catch((err) => { if (!cancelled) setReloadError(err instanceof Error ? err.message : String(err)) })
    return () => { cancelled = true }
    // policy 不进依赖：打开瞬间以上次渲染值为基线，异步再校正，避免闭包旧值。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, task.id])

  useEffect(() => {
    if (!scenarioOpen) return
    let cancelled = false
    setScenarioDraft(scenarioDraftOf(task.scenario))
    setScenarioSaved(normalizeWebApiScenario(task.scenario))
    setScenarioError('')
    getWebApiTask(task.id)
      .then((fresh) => {
        if (cancelled) return
        setScenarioDraft(scenarioDraftOf(fresh.scenario))
        setScenarioSaved(normalizeWebApiScenario(fresh.scenario))
      })
      .catch((err) => { if (!cancelled) setScenarioError(err instanceof Error ? err.message : String(err)) })
    return () => { cancelled = true }
    // 与策略同一模式：打开瞬间用当前渲染值，异步再校正。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scenarioOpen, task.id])

  const saveScenario = async () => {
    if (scenarioSaving || !scenarioChanged) return
    setScenarioSaving(true)
    setScenarioError('')
    try {
      // 下一次 Run 生效；空完成标准传空串＝恢复场景默认。
      const next = await putWebApiTaskScenario(task.id, {
        scenario: scenarioDraft.scenario,
        done_criteria: scenarioDraft.doneCriteria.trim(),
      })
      setScenarioSaved(next)
      setScenarioDraft(scenarioDraftOf(next))
      setScenarioOpen(false)
      onScenarioSaved({ ...task, scenario: next })
    } catch (err) {
      setScenarioError(err instanceof Error ? err.message : String(err))
    } finally {
      setScenarioSaving(false)
    }
  }

  return (
    <div className="flex h-12 shrink-0 items-center gap-2 overflow-hidden border-b border-border bg-background px-3">
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        onClick={onBack}
        aria-label={t('shell.backToTasks')}
        title={t('shell.backToTasks')}
        className="shrink-0 text-muted-foreground"
      >
        <ArrowLeft className="h-4 w-4" />
      </Button>
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {task.asset && onOpenAsset && (
          <Button
            type="button"
            variant="soft"
            size="xs"
            onClick={() => onOpenAsset(task.asset!.id)}
            title={task.asset.name}
            className="h-6 shrink-0 gap-1 px-1.5 text-[12px]"
          >
            <Boxes className="h-3 w-3" />
            <span className="max-w-[8rem] truncate">{task.asset.name.length > 12 ? `${task.asset.name.slice(0, 12)}…` : task.asset.name}</span>
          </Button>
        )}
        <Globe className="h-3.5 w-3.5 shrink-0 text-primary" />
        <span className="min-w-0 truncate text-[13px] font-medium text-foreground" title={summary}>{task.objective}</span>
        <span className="hidden shrink-0 items-center gap-1 text-muted-foreground sm:flex">
          <ShieldCheck className="h-3 w-3 shrink-0" />
          {task.origins.map((origin) => (
            <span key={origin} className="font-mono text-[12px]" title={origin}>{origin}</span>
          ))}
        </span>
        {/* 场景指示：策略指示左侧；点击打开场景与完成标准编辑 Popover。 */}
        <Popover open={scenarioOpen} onOpenChange={setScenarioOpen}>
          <PopoverTrigger asChild>
            <button
              type="button"
              title={t('scenario.title')}
              className="flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[12px] text-muted-foreground transition-colors hover:bg-muted/40 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
            >
              <Compass className="h-3 w-3 shrink-0" aria-hidden="true" />
              <span>{scenarioName}</span>
            </button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-[min(22.5rem,calc(100vw-2rem))] rounded-lg border border-border p-0 shadow-md">
            <div className="border-b border-border px-3 py-2.5">
              <p className="text-[14px] font-semibold text-foreground">{t('scenario.title')}</p>
              <p className="mt-0.5 text-[11px] text-muted-foreground">{t('scenario.hint')}</p>
            </div>
            <div className="space-y-3 px-3 py-3">
              <WebApiScenarioPicker value={scenarioDraft} onChange={setScenarioDraft} disabled={scenarioSaving} />
              {scenarioError && <Callout tone="destructive">{scenarioError}</Callout>}
              <div className="flex justify-end">
                <Button type="button" className="h-8 text-[12px]" disabled={scenarioSaving || !scenarioChanged} onClick={() => { void saveScenario() }}>
                  {scenarioSaving && <Spinner className="h-3.5 w-3.5" />}{t('scenario.save')}
                </Button>
              </div>
            </div>
          </PopoverContent>
        </Popover>
        {/* 策略指示：origin 之后；不限制/审批关用琥珀色文字。点击打开编辑 Popover。 */}
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button
              type="button"
              title={summaryView.title}
              className={cn(
                'flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                summaryView.warn ? 'text-warning hover:bg-warning/10' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
              )}
            >
              <PolicyIcon className="h-3 w-3 shrink-0" aria-hidden="true" />
              <span>{summaryView.text}</span>
            </button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-[min(22.5rem,calc(100vw-2rem))] rounded-lg border border-border p-0 shadow-md">
            <div className="border-b border-border px-3 py-2.5">
              <p className="text-[14px] font-semibold text-foreground">{t('webapi.policyTitle')}</p>
              <p className="mt-0.5 text-[11px] text-muted-foreground">
                {draft.updated_at
                  ? t('webapi.policyUpdatedAt', { time: formatDateTime(draft.updated_at) })
                  : t('webapi.policyDefaultSince')}
              </p>
            </div>
            <div className="space-y-3 px-3 py-3">
              {reloadError && <p role="status" className="text-[11px] text-warning">{t('webapi.policyReloadFailed')}：{reloadError}</p>}
              <WebApiPolicyFields
                scope={draft.scope}
                approval={draft.approval}
                onScopeChange={(scope) => setDraft((current) => ({ ...current, scope }))}
                onApprovalChange={(approval) => setDraft((current) => ({ ...current, approval }))}
                compact
              />
              {/* 终端出口的边界如实写明：scope 决定终端经代理的请求是否被拦截。 */}
              <p className="text-[11px] text-muted-foreground">
                {draft.scope === 'unrestricted' ? t('webapi.egressScopeNoteUnrestricted') : t('webapi.egressScopeNoteOrigins')}
              </p>
              {/* 保存仅对之后的调用生效；保存成功即关闭并回写 App 任务状态。 */}
              <WebApiPolicySaver
                taskID={task.id}
                scope={draft.scope}
                approval={draft.approval}
                current={saved}
                onSaved={(next) => {
                  setSaved(next)
                  setDraft(next)
                  setOpen(false)
                  onPolicySaved({ ...task, policy: next })
                }}
              />
            </div>
          </PopoverContent>
        </Popover>
      </div>
      {/* 视图页签：执行 / 流量 / 候选 / 报告 / 用量（原左侧任务导航列）。 */}
      <div role="group" aria-label={t('webapi.taskNav')} className="flex shrink-0 items-center gap-0.5 overflow-x-auto rounded-md border border-border/60 p-0.5">
        {VIEW_TABS.map((item) => {
          const active = view === item.view
          const count = item.countKey ? counts[item.countKey] : null
          return (
            <button
              key={item.view}
              type="button"
              aria-pressed={active}
              onClick={() => onViewChange(item.view)}
              className={cn(
                'flex h-6 shrink-0 items-center gap-1 rounded px-2 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                active ? 'bg-primary/10 font-medium text-primary' : 'text-muted-foreground hover:bg-muted/40 hover:text-foreground',
              )}
            >
              <span>{t(item.labelKey)}</span>
              {count !== null && <span className="font-mono text-[11px] tabular-nums">{count}</span>}
            </button>
          )
        })}
      </div>
    </div>
  )
}
