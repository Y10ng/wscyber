import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown } from 'lucide-react'
import { Button, Textarea } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { listWebApiScenarios, type WebApiScenario, type WebApiScenarioOption } from '../api'

// M1 研究场景选择（建任务、资产默认场景、任务顶栏共用）。场景与完成标准只由人设置，
// 契约见 native/SCENARIO.md；说明文案依据 docs/scenarios.md。两张并排卡片，
// 下方「完成标准」默认收起；留空＝使用场景默认文本。

export interface WebApiScenarioDraft {
  scenario: string
  doneCriteria: string
}

export const DEFAULT_SCENARIO_KEY = 'webapi_pentest'

// 旧后端/旧任务响应缺 scenario 时按默认渲染，不另造前端状态。
export function normalizeWebApiScenario(scenario: WebApiScenario | undefined | null): WebApiScenario {
  if (!scenario) {
    return { key: DEFAULT_SCENARIO_KEY, name: '', done_criteria: '', done_criteria_effective: '', is_default: true }
  }
  return {
    key: scenario.key || DEFAULT_SCENARIO_KEY,
    name: scenario.name || scenario.key || DEFAULT_SCENARIO_KEY,
    done_criteria: scenario.done_criteria ?? '',
    done_criteria_effective: scenario.done_criteria_effective ?? '',
    is_default: scenario.is_default === true,
  }
}

export function scenarioDraftOf(scenario: WebApiScenario | undefined | null): WebApiScenarioDraft {
  const normalized = normalizeWebApiScenario(scenario)
  return { scenario: normalized.key, doneCriteria: normalized.done_criteria }
}

// 场景说明固定两行；名称与默认完成标准以后端返回为准。
const DESCRIPTION_KEYS: Record<string, [string, string]> = {
  webapi_pentest: ['scenario.pentestDesc1', 'scenario.pentestDesc2'],
  autonomous: ['scenario.autonomousDesc1', 'scenario.autonomousDesc2'],
}

// 选项一次取回后在本页内复用；失败不缓存，下次挂载重试。
let optionsPromise: Promise<WebApiScenarioOption[]> | null = null
function loadScenarioOptions(): Promise<WebApiScenarioOption[]> {
  if (!optionsPromise) {
    optionsPromise = listWebApiScenarios().catch((err) => {
      optionsPromise = null
      throw err
    })
  }
  return optionsPromise
}

interface Props {
  value: WebApiScenarioDraft
  onChange: (value: WebApiScenarioDraft) => void
  disabled?: boolean
}

export default function WebApiScenarioPicker({ value, onChange, disabled }: Props) {
  const { t } = useTranslation('chat')
  const [options, setOptions] = useState<WebApiScenarioOption[]>([])
  const [expanded, setExpanded] = useState(false)
  const [loadError, setLoadError] = useState('')

  useEffect(() => {
    let cancelled = false
    loadScenarioOptions()
      .then((list) => { if (!cancelled) { setOptions(list); setLoadError('') } })
      .catch((err) => { if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err)) })
    return () => { cancelled = true }
  }, [])

  const custom = value.doneCriteria.trim() !== ''
  const selected = options.find((option) => option.key === value.scenario)
  // 完成标准 placeholder 始终是当前场景的默认文本：未自定义时随场景切换，
  // 已自定义时保留文本（placeholder 只在清空后可见）。
  const placeholder = selected?.default_done_criteria ?? ''

  return (
    <div className="min-w-0 space-y-2">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {options.map((option) => {
          const active = option.key === value.scenario
          const description = DESCRIPTION_KEYS[option.key]
          return (
            <button
              key={option.key}
              type="button"
              aria-pressed={active}
              disabled={disabled}
              onClick={() => onChange({ scenario: option.key, doneCriteria: value.doneCriteria })}
              className={cn(
                'relative rounded-lg border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 disabled:opacity-60',
                active ? 'border-primary bg-primary/5' : 'border-border/70 bg-background hover:border-border hover:bg-muted/20',
              )}
            >
              {active && (
                <span className="absolute right-2 top-2 flex h-4 w-4 items-center justify-center rounded-full bg-primary text-primary-foreground">
                  <Check className="h-3 w-3" aria-hidden="true" />
                </span>
              )}
              <span className={cn('block text-[13px] font-medium', active ? 'text-primary' : 'text-foreground', active && 'pr-5')}>{option.name}</span>
              {description && (
                <>
                  <span className="mt-1 block text-[11px] leading-relaxed text-muted-foreground">{t(description[0])}</span>
                  <span className="block text-[11px] leading-relaxed text-muted-foreground">{t(description[1])}</span>
                </>
              )}
            </button>
          )
        })}
      </div>
      {loadError && <p role="status" className="text-[11px] text-warning">{t('scenario.loadFailed')}：{loadError}</p>}

      <div className="rounded-lg border border-border/70">
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((current) => !current)}
          className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
        >
          <span className="text-[13px] font-medium text-foreground/75">{t('scenario.doneCriteria')}</span>
          <span className="flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground">
            {custom ? t('scenario.doneCriteriaCustom') : t('scenario.doneCriteriaDefault')}
            <ChevronDown className={cn('h-3.5 w-3.5 transition-transform', expanded && 'rotate-180')} aria-hidden="true" />
          </span>
        </button>
        {expanded && (
          <div className="space-y-1.5 border-t border-border/60 p-3">
            <Textarea
              value={value.doneCriteria}
              onChange={(event) => onChange({ scenario: value.scenario, doneCriteria: event.target.value })}
              maxLength={2000}
              rows={3}
              placeholder={placeholder}
              disabled={disabled}
            />
            <div className="flex items-center justify-between gap-2">
              <span className="min-w-0 text-[11px] text-muted-foreground">{t('scenario.doneCriteriaHint')}</span>
              <Button
                type="button"
                variant="ghost"
                size="xs"
                disabled={disabled || !custom}
                onClick={() => onChange({ scenario: value.scenario, doneCriteria: '' })}
                className="shrink-0 text-muted-foreground"
              >
                {t('scenario.restoreDefault')}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
