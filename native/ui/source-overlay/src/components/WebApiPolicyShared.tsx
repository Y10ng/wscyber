import { useTranslation } from 'react-i18next'
import { useMemo, useState } from 'react'
import { ShieldAlert, ShieldCheck } from 'lucide-react'
import { Button, Callout, Spinner, ToggleGroup, ToggleGroupItem } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { saveWebApiPolicy, type WebApiTaskPolicy } from '../api'

// B1 任务级测试策略分段控件：建任务对话框与任务顶栏 Popover 共用同一组
// 分段按钮与提示。策略只由人设置；scope/approval 契约见 native/POLICY.md，
// 字段/枚举名以后端为准（scope: origins|unrestricted, approval: off|high_risk|all）。

export const DEFAULT_WEB_API_POLICY: WebApiTaskPolicy = {
  scope: 'origins',
  approval: 'high_risk',
  is_default: true,
  updated_at: null,
}

// 旧后端/旧任务响应缺 policy 字段时按默认渲染，不另造前端状态。
export function normalizeWebApiPolicy(policy: WebApiTaskPolicy | undefined | null): WebApiTaskPolicy {
  if (!policy) return { ...DEFAULT_WEB_API_POLICY }
  return {
    scope: policy.scope === 'unrestricted' ? 'unrestricted' : 'origins',
    approval: policy.approval === 'off' || policy.approval === 'all' ? policy.approval : 'high_risk',
    is_default: policy.is_default === true,
    updated_at: policy.updated_at ?? null,
  }
}

interface PolicyFieldsProps {
  scope: string
  approval: string
  onScopeChange: (scope: string) => void
  onApprovalChange: (approval: string) => void
  disabled?: boolean
  /** 紧凑版（任务顶栏 Popover）隐藏字段说明，只留分段与风险提示。 */
  compact?: boolean
}

export function WebApiPolicyFields({ scope, approval, onScopeChange, onApprovalChange, disabled, compact }: PolicyFieldsProps) {
  const { t } = useTranslation('chat')
  const risky = scope === 'unrestricted'
  return (
    <div className={cn('min-w-0 space-y-2', compact && 'space-y-1.5')}>
      <div className="min-w-0">
        {!compact && <span className="mb-1 block text-[13px] font-medium text-foreground/75">{t('webapi.policySection')}</span>}
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className={cn('shrink-0 text-muted-foreground', compact ? 'text-[12px]' : 'text-[13px]')}>{t('webapi.policyScope')}</span>
          <ToggleGroup value={scope} onValueChange={onScopeChange} disabled={disabled} ariaLabel={t('webapi.policyScope')} className="shrink-0">
            <ToggleGroupItem value="origins">{t('webapi.policyScopeOrigins')}</ToggleGroupItem>
            <ToggleGroupItem value="unrestricted">{t('webapi.policyScopeUnrestricted')}</ToggleGroupItem>
          </ToggleGroup>
        </div>
        {risky && (
          <p role="status" className="mt-1 flex items-start gap-1.5 text-xs leading-relaxed text-warning">
            <ShieldAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
            {t('webapi.policyUnrestrictedWarning')}
          </p>
        )}
      </div>
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <span className={cn('shrink-0 text-muted-foreground', compact ? 'text-[12px]' : 'text-[13px]')}>{t('webapi.policyApproval')}</span>
        <ToggleGroup value={approval} onValueChange={onApprovalChange} disabled={disabled} ariaLabel={t('webapi.policyApproval')} className="shrink-0">
          <ToggleGroupItem value="off">{t('webapi.policyApprovalOff')}</ToggleGroupItem>
          <ToggleGroupItem value="high_risk">{t('webapi.policyApprovalHighRisk')}</ToggleGroupItem>
          <ToggleGroupItem value="all">{t('webapi.policyApprovalAll')}</ToggleGroupItem>
        </ToggleGroup>
      </div>
    </div>
  )
}

// 保存按钮 + 错误显示：任务顶栏 Popover 用（保存成功即关闭由调用方处理）。
export function WebApiPolicySaver({ taskID, scope, approval, current, onSaved }: {
  taskID: string
  scope: string
  approval: string
  current: WebApiTaskPolicy
  onSaved: (policy: WebApiTaskPolicy) => void
}) {
  const { t } = useTranslation('chat')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const changed = scope !== current.scope || approval !== current.approval
  const save = async () => {
    if (saving || !changed) return
    setSaving(true)
    setError('')
    try {
      onSaved(await saveWebApiPolicy(taskID, scope, approval))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setSaving(false)
    }
  }
  return (
    <div className="flex min-w-0 flex-col gap-2">
      {error && <Callout tone="destructive">{error}</Callout>}
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] text-muted-foreground">{t('webapi.policySaveHint')}</span>
        <Button type="button" className="h-8 text-[12px]" disabled={saving || !changed} onClick={() => { void save() }}>
          {saving && <Spinner className="h-3.5 w-3.5" />}{t('webapi.policySave')}
        </Button>
      </div>
    </div>
  )
}

// 任务顶栏短文案：盾牌图标 + “仅授权 · 高危审批”；不限制/审批关用琥珀色。
export function useWebApiPolicySummary(policy: WebApiTaskPolicy) {
  const { t } = useTranslation('chat')
  return useMemo(() => {
    const scopeLabel = policy.scope === 'unrestricted' ? t('webapi.policyScopeUnrestricted') : t('webapi.policyScopeOrigins')
    const approvalLabel = policy.approval === 'off' ? t('webapi.policyApprovalOff')
      : policy.approval === 'all' ? t('webapi.policyApprovalAll') : t('webapi.policyApprovalHighRisk')
    const warn = policy.scope === 'unrestricted' || policy.approval === 'off'
    return {
      text: `${scopeLabel} · ${approvalLabel}`,
      warn,
      icon: warn ? ShieldAlert : ShieldCheck,
      title: policy.is_default ? t('webapi.policyDefaultTitle') : t('webapi.policyTitle'),
    }
  }, [policy.scope, policy.approval, policy.is_default, t])
}
