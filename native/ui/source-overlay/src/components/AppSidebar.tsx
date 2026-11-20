import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Boxes, Cpu, FileText, Gauge, LayoutDashboard, ListChecks, LogOut,
  PanelLeftClose, PanelLeftOpen, Server, ShieldAlert, Webhook,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import {
  Button, Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
  ThemeToggle, Tooltip, TooltipContent, TooltipTrigger,
} from '@cyber/ui'
import { cn, useTheme } from '@cyber/theme'
import BrandLogo from './brand/BrandLogo'
import LanguageToggle from './LanguageToggle'
import QuickConnect from './QuickConnect'
import { PendingApprovalsButton, type PendingItem } from './WebApiPendingApprovals'
import { listWebApiAssets, listWebApiTaskList, logout, type AgentView, type LLMProviderView } from '../api'
import { WorkbenchSlots } from '../runtime/react'
import type { HubServices } from '../runtime/services'
import type { AppPage } from '../App'

function useIsMobile(): boolean {
  const [mobile, setMobile] = useState(() => typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches)
  useEffect(() => {
    const media = window.matchMedia('(max-width: 767px)')
    const apply = () => setMobile(media.matches)
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [])
  return mobile
}

interface NavDef {
  key: AppPage
  icon: LucideIcon
  labelKey: string
  enabled: boolean
  /** 计数来源：tasks = 任务列表 counts.all；assets = 未归档资产数；agents = 在线 agent 数。 */
  count?: 'tasks' | 'assets' | 'agents'
}

// 漏洞 / 报告页面后续片实现；保留数组项但用 enabled: false 过滤，避免占位入口。
const OPERATIONS_NAV: NavDef[] = [
  { key: 'overview', icon: LayoutDashboard, labelKey: 'shell.navOverview', enabled: true },
  { key: 'tasks', icon: ListChecks, labelKey: 'shell.navTasks', enabled: true, count: 'tasks' },
  { key: 'assets', icon: Boxes, labelKey: 'shell.navAssets', enabled: true, count: 'assets' },
  { key: 'vulns', icon: ShieldAlert, labelKey: 'shell.navVulns', enabled: false },
  { key: 'reports', icon: FileText, labelKey: 'shell.navReports', enabled: false },
]

const CONFIG_NAV: NavDef[] = [
  { key: 'models', icon: Cpu, labelKey: 'shell.navModels', enabled: true },
  { key: 'nodes', icon: Server, labelKey: 'shell.navNodes', enabled: true, count: 'agents' },
  { key: 'integrations', icon: Webhook, labelKey: 'shell.navIntegrations', enabled: true },
]

interface AppSidebarProps {
  open: boolean
  collapsed: boolean
  page: AppPage
  webapiEnabled: boolean
  agents: AgentView[]
  services: HubServices
  llmProfiles: LLMProviderView[]
  activeLLMProfile: string
  fallbackModel: string
  switchingLLM: boolean
  onSwitchLLM: (profileID: string) => void
  serverURL: string | undefined
  version: string | undefined
  connectionProfiles: Array<{ id: string; title?: string }> | undefined
  pendingApprovalItems: PendingItem[]
  onResolvePending: (item: PendingItem, approve: boolean) => Promise<void>
  onViewSession: (sessionID: string) => void
  onNavigate: (page: AppPage) => void
  onToggleCollapsed: () => void
  onCloseDrawer: () => void
  onOpenUsage: () => void
}

/**
 * 左侧固定菜单（docs/ui-shell.md §1/§2）：240px 展开 / 56px 图标栏；
 * 移动端为抽屉（<768px），由页面页头菜单按钮打开。
 */
export default function AppSidebar({
  open, collapsed, page, webapiEnabled, agents, services,
  llmProfiles, activeLLMProfile, fallbackModel, switchingLLM, onSwitchLLM,
  serverURL, version, connectionProfiles,
  pendingApprovalItems, onResolvePending, onViewSession,
  onNavigate, onToggleCollapsed, onCloseDrawer, onOpenUsage,
}: AppSidebarProps) {
  const { t } = useTranslation('chat')
  const { t: ta } = useTranslation('app')
  const { t: ts } = useTranslation('sidebar')
  const { isDark, toggle } = useTheme()
  const [taskCount, setTaskCount] = useState<number | null>(null)
  const [assetCount, setAssetCount] = useState<number | null>(null)
  // 移动端是抽屉（不受折叠状态影响），桌面是固定栏（折叠为 56px 图标栏）。
  const mobile = useIsMobile()
  const rail = !mobile && collapsed
  const drawer = mobile && open
  // 视口切到移动端时收起抽屉，避免从桌面宽度缩小后遮罩卡在打开状态。
  useEffect(() => {
    if (mobile) onCloseDrawer()
    // 只在断点变化时触发；抽屉的打开/关闭由页头菜单按钮控制。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mobile])

  // 计数：挂载与页面变化时各请求一次；失败不显示数字（不显示假 0）。
  const loadCounts = useCallback(async () => {
    if (!webapiEnabled) return
    const [tasks, assets] = await Promise.allSettled([
      listWebApiTaskList({ limit: 1 }),
      listWebApiAssets(false),
    ])
    if (tasks.status === 'fulfilled') setTaskCount(tasks.value.counts.all)
    if (assets.status === 'fulfilled') setAssetCount(assets.value.length)
  }, [webapiEnabled])

  useEffect(() => { void loadCounts() }, [loadCounts, page])

  // 从工作区返回时导航计数可能已变化：在离开工作区后由 page 变化触发；同一页内
  // 的刷新（新建任务等）由侧栏本身低频重取兜底。
  useEffect(() => {
    const timer = window.setInterval(() => { void loadCounts() }, 60000)
    return () => window.clearInterval(timer)
  }, [loadCounts])

  const countOf = (source: NavDef['count']): number | null => {
    if (source === 'tasks') return taskCount
    if (source === 'assets') return assetCount
    if (source === 'agents') return agents.length > 0 ? agents.length : null
    return null
  }
  // 集成（企业微信推送）依赖 webapi capability；未启用时不显示入口。
  const configNav = webapiEnabled ? CONFIG_NAV : CONFIG_NAV.filter(item => item.key !== 'integrations')

  return (
    <>
      {open && (
        <button
          type="button"
          aria-label={ts('closeSidebarOverlay')}
          onClick={onCloseDrawer}
          className="fixed inset-0 z-30 bg-background/60 backdrop-blur-[1px] md:hidden"
        />
      )}
      <aside
        data-sidebar-collapsed={collapsed ? '' : undefined}
        className={cn(
          'shrink-0 flex-col border-r border-border bg-card transition-[width] duration-200 ease-in-out',
          mobile
            ? (drawer ? 'fixed inset-y-0 left-0 z-40 flex w-60 shadow-elevated' : 'hidden')
            : 'relative flex',
          !mobile && (collapsed ? 'w-14' : 'w-60'),
        )}
      >
        {/* Logo + 折叠按钮 */}
        <div className={cn('flex shrink-0 border-b border-border', rail ? 'flex-col items-center justify-center gap-0.5 py-2' : 'h-12 items-center gap-2 px-3')}>
          <BrandLogo size={22} className="shrink-0" />
          {!rail && <span className="min-w-0 flex-1 truncate text-[13px] font-semibold tracking-tight text-foreground">Cyber</span>}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                onClick={onToggleCollapsed}
                aria-label={rail ? ts('expandSidebar') : ts('collapseSidebar')}
                className="shrink-0 text-muted-foreground"
              >
                {rail ? <PanelLeftOpen className="h-3.5 w-3.5" /> : <PanelLeftClose className="h-3.5 w-3.5" />}
              </Button>
            </TooltipTrigger>
            <TooltipContent side={rail ? 'right' : 'bottom'}>{rail ? ts('expandSidebar') : ts('collapseSidebar')}</TooltipContent>
          </Tooltip>
        </div>

        {/* 模型配置切换（原顶栏） */}
        {!rail && (
          <div className="shrink-0 border-b border-border px-2 py-2">
            <LLMProfileSwitcher
              profiles={llmProfiles}
              activeProfileID={activeLLMProfile}
              fallbackModel={fallbackModel}
              disabled={switchingLLM}
              onChange={onSwitchLLM}
            />
          </div>
        )}

        <nav aria-label={t('shell.navLabel')} className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
          <NavGroup title={t('shell.groupOperations')} items={OPERATIONS_NAV} collapsed={rail}
            activePage={page} countOf={countOf} onNavigate={onNavigate} />
          <NavGroup title={t('shell.groupConfig')} items={configNav} collapsed={rail}
            activePage={page} countOf={countOf} onNavigate={onNavigate} />
        </nav>

        {/* 底部：待审批 → 工具图标行 → 语言/主题/退出 → 用量 */}
        <div className="mt-auto shrink-0 border-t border-border pt-2">
          <div className={cn('px-2 pb-1', rail && 'flex flex-col items-center')}>
            <div className={cn(rail && '[&_button]:w-9 [&_button]:justify-center [&_button]:px-0 [&_button>span]:hidden')}>
              <PendingApprovalsButton items={pendingApprovalItems} onResolve={onResolvePending} onViewSession={onViewSession} />
            </div>
          </div>
          <div className={cn('flex gap-0.5 px-2 py-1', rail ? 'flex-col items-center [&_button]:!w-9 [&_button]:!px-0 [&_button>span]:hidden' : 'flex-row flex-wrap items-center')}>
            <WorkbenchSlots services={services} name="shell.header.actions" />
            <QuickConnect serverURL={serverURL} version={version} profiles={connectionProfiles} />
          </div>
          <div className={cn('flex items-center gap-0.5 px-2 py-1 pb-[max(0.5rem,env(safe-area-inset-bottom))]', rail && 'flex-col')}>
            <LanguageToggle />
            <div data-sidebar-theme-toggle>
              <ThemeToggle isDark={isDark} onToggle={toggle} size="sm" toLightLabel={ts('switchToLight')} toDarkLabel={ts('switchToDark')} />
            </div>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button type="button" variant="ghost" size="icon-xs" aria-label={ta('logout')} onClick={() => { void logout() }} className="text-muted-foreground hover:text-foreground">
                  <LogOut className="h-3.5 w-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{ta('logout')}</TooltipContent>
            </Tooltip>
            {webapiEnabled && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button type="button" variant="ghost" size="icon-xs" aria-label={t('shell.usage')} onClick={onOpenUsage} className="text-muted-foreground hover:text-foreground">
                    <Gauge className="h-3.5 w-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{t('shell.usage')}</TooltipContent>
              </Tooltip>
            )}
          </div>
        </div>
      </aside>
    </>
  )
}

function NavGroup({ title, items, collapsed, activePage, countOf, onNavigate }: {
  title: string
  items: NavDef[]
  collapsed: boolean
  activePage: AppPage
  countOf: (source: NavDef['count']) => number | null
  onNavigate: (page: AppPage) => void
}) {
  const { t } = useTranslation('chat')
  const visible = items.filter((item) => item.enabled)
  if (visible.length === 0) return null
  return (
    <div className="pt-2">
      {!collapsed && <div className="px-3 pb-1 pt-2 text-[11px] uppercase tracking-wide text-muted-foreground">{title}</div>}
      <div className="space-y-0.5">
        {visible.map((item) => {
          const Icon = item.icon
          const active = activePage === item.key
          const count = countOf(item.count)
          const label = t(item.labelKey)
          const button = (
            <button
              type="button"
              aria-current={active ? 'page' : undefined}
              onClick={() => onNavigate(item.key)}
              className={cn(
                'group relative flex h-9 w-full items-center gap-2.5 rounded-md px-3 text-left text-[13px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                active ? 'bg-accent font-medium text-foreground' : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground',
                collapsed && 'justify-center px-0',
              )}
            >
              {active && <span aria-hidden className="absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-primary" />}
              <Icon className="h-4 w-4 shrink-0" />
              {!collapsed && <span className="min-w-0 flex-1 truncate">{label}</span>}
              {!collapsed && count !== null && (
                <span className="ml-auto rounded bg-muted px-1.5 text-[11px] tabular-nums text-muted-foreground">{count}</span>
              )}
            </button>
          )
          return collapsed ? (
            <Tooltip key={item.key}>
              <TooltipTrigger asChild>{button}</TooltipTrigger>
              <TooltipContent side="right">{count !== null ? `${label} · ${count}` : label}</TooltipContent>
            </Tooltip>
          ) : <div key={item.key}>{button}</div>
        })}
      </div>
    </div>
  )
}

export function LLMProfileSwitcher({
  profiles,
  activeProfileID,
  fallbackModel,
  disabled,
  onChange,
}: {
  profiles: LLMProviderView[]
  activeProfileID: string
  fallbackModel: string
  disabled: boolean
  onChange: (profileID: string) => void
}) {
  const { t } = useTranslation('app')

  if (profiles.length === 0) {
    return <span className="block truncate px-1 font-mono text-[10px] uppercase tracking-wider text-muted-foreground">{fallbackModel}</span>
  }

  return (
    <Select value={activeProfileID || profiles[0].id} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label={t('switchLLMProfile')}
        className="h-8 w-full gap-1 border-border bg-transparent px-2 font-mono text-[10px] text-muted-foreground shadow-none hover:bg-muted/60 hover:text-foreground"
      >
        <SelectValue placeholder={fallbackModel} />
      </SelectTrigger>
      <SelectContent align="start">
        {profiles.map(profile => (
          <SelectItem key={profile.id} value={profile.id}>
            {profile.name || profile.model || profile.provider}
            {profile.model && profile.name !== profile.model ? ` · ${profile.model}` : ''}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}