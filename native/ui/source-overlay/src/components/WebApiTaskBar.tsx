import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Boxes, Globe, ShieldCheck } from 'lucide-react'
import { Button, Popover, PopoverContent, PopoverTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { getWebApiTask, type WebApiTask, type WebApiTaskPolicy } from '../api'
import { normalizeWebApiPolicy, useWebApiPolicySummary, WebApiPolicyFields, WebApiPolicySaver } from './WebApiPolicyShared'

export type WebApiView = 'execution' | 'traffic' | 'findings' | 'reports' | 'usage'

interface Props {
  task: WebApiTask
  /** 策略保存成功后回写 App 的任务对象（App 持有 activeWebApiTask 状态）。 */
  onPolicySaved: (task: WebApiTask) => void
  /** 任务来自资产时，点击资产标签回到资产详情。 */
  onOpenAsset?: (assetId: string) => void
}

// 真正 Web/API 任务才显示的范围摘要栏；普通会话不渲染。
// 视图切换已上移到任务局部导航（WebApiTaskNav），这里保留目标、授权 origin 与
// B1 测试策略指示（盾牌 + 短文案，点击 Popover 修改，仅对之后的调用生效）。
export default function WebApiTaskBar({ task, onPolicySaved, onOpenAsset }: Props) {
  const { t, i18n } = useTranslation('chat')
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

  return (
    <div className="flex shrink-0 items-center gap-2 border-b border-border/60 bg-background px-4 py-1.5">
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
          <PopoverContent align="end" className="w-80 space-y-3 p-3">
            <div className="space-y-0.5">
              <p className="text-[13px] font-medium text-foreground">{t('webapi.policyTitle')}</p>
              <p className="text-[11px] text-muted-foreground">
                {draft.updated_at
                  ? t('webapi.policyUpdatedAt', { time: new Date(draft.updated_at).toLocaleString(i18n.language) })
                  : t('webapi.policyDefaultSince')}
              </p>
            </div>
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
          </PopoverContent>
        </Popover>
      </div>
    </div>
  )
}
