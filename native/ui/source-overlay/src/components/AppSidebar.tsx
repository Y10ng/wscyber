import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Boxes, Cpu, FileText, Gauge, LayoutDashboard, ListChecks, LogOut,
  PanelLeftClose, PanelLeftOpen, ScrollText, Server, ShieldAlert, ShieldCheck, Webhook,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import {
  Button, Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
  ThemeToggle, Tooltip, TooltipContent, TooltipTrigger,
} from '@cyber/ui'
import { cn, useTheme } from '@cyber/theme'
// 品牌区按设计稿使用蓝底盾牌图标；BrandLogo 组件保留给原生会话页，品牌位置不再出现 “Cyber” 字样。
import LanguageToggle from './LanguageToggle'
import QuickConnect from './QuickConnect'
import { PendingApprovalsButton, type PendingItem } from './WebApiPendingApprovals'
import { listWebApiAssets, listWebApiGlobalVulns, listWebApiTaskList, logout, type AgentView, type LLMProviderView } from '../api'
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
  /** 计数来源：tasks = 任务列表 counts.all；assets = 未归档资产数；vulns = 新发现+仍存在；agents = 在线 agent 数。 */
  count?: 'tasks' | 'assets' | 'agents' | 'vulns'
}

// 漏洞 / 报告依赖 webapi capability；未启用时在渲染前过滤（下方 operationsNav）。
const OPERATIONS_NAV: NavDef[] = [
  { key: 'overview', icon: LayoutDashboard, labelKey: 'shell.navOverview', enabled: true },
  { key: 'tasks', icon: ListChecks, labelKey: 'shell.navTasks', enabled: true, count: 'tasks' },
  { key: 'assets', icon: Boxes, labelKey: 'shell.navAssets', enabled: true, count: 'assets' },
  { key: 'vulns', icon: ShieldAlert, labelKey: 'shell.navVulns', enabled: true, count: 'vulns' },
  { key: 'reports', icon: FileText, labelKey: 'shell.navReports', enabled: true },
]

const CONFIG_NAV: NavDef[] = [
  { key: 'models', icon: Cpu, labelKey: 'shell.navModels', enabled: true },
  { key: 'nodes', icon: Server, labelKey: 'shell.navNodes', enabled: true, count: 'agents' },
  { key: 'integrations', icon: Webhook, labelKey: 'shell.navIntegrations', enabled: true },
  { key: 'audit', icon: ScrollText, labelKey: 'shell.navAudit', enabled: true },
]

interface AppSidebarProps {
  open: boolean
  collapsed: boolean
  page: AppPage
  /** 导航高亮用的页面；缺省同 page（任务工作区里 App 传 tasks）。 */
  activePage?: AppPage
  /** 外部数据变化信号（如任务归档）：变化时立即重取导航计数。 */
  countsRevision?: number
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
  /** 总览“待审批操作”一次性请求：打开侧栏待审批 Popover。 */
  approvalsRequested: boolean
  onApprovalsRequestedConsumed: () => void
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
  open, collapsed, page, activePage, countsRevision = 0, webapiEnabled, agents, services,
  llmProfiles, activeLLMProfile, fallbackModel, switchingLLM, onSwitchLLM,
  serverURL, version, connectionProfiles,
  pendingApprovalItems, approvalsRequested, onApprovalsRequestedConsumed, onResolvePending, onViewSession,
  onNavigate, onToggleCollapsed, onCloseDrawer, onOpenUsage,
}: AppSidebarProps) {
  const { t } = useTranslation('chat')
  const { t: ta } = useTranslation('app')
  const { t: ts } = useTranslation('sidebar')
  const { isDark, toggle } = useTheme()
  const [taskCount, setTaskCount] = useState<number | null>(null)
  const [assetCount, setAssetCount] = useState<number | null>(null)
  const [vulnCounts, setVulnCounts] = useState<{ total: number; new: number } | null>(null)
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
    const [tasks, assets, vulns] = await Promise.allSettled([
      listWebApiTaskList({ limit: 1 }),
      listWebApiAssets(false),
      listWebApiGlobalVulns({ limit: 1 }),
    ])
    if (tasks.status === 'fulfilled') setTaskCount(tasks.value.counts.all)
    if (assets.status === 'fulfilled') setAssetCount(assets.value.length)
    if (vulns.status === 'fulfilled') {
      const counts = vulns.value.counts
      setVulnCounts({ total: counts.new + counts.present, new: counts.new })
    }
  }, [webapiEnabled])

  useEffect(() => { void loadCounts() }, [loadCounts, page, countsRevision])

  // 从工作区返回时导航计数可能已变化：在离开工作区后由 page 变化触发；同一页内
  // 的刷新（新建任务等）由侧栏本身低频重取兜底。
  useEffect(() => {
    const timer = window.setInterval(() => { void loadCounts() }, 60000)
    return () => window.clearInterval(timer)
  }, [loadCounts])

  const countOf = (source: NavDef['count']): number | null => {
    if (source === 'tasks') return taskCount
    if (source === 'assets') return assetCount
    if (source === 'vulns') return vulnCounts && vulnCounts.total > 0 ? vulnCounts.total : null
    if (source === 'agents') return agents.length > 0 ? agents.length : null
    return null
  }
  // 漏洞计数在有新发现时用红色圆形徽章（NavGroup 内渲染）。
  // 集成（企业微信推送）与审计依赖 webapi capability；未启用时不显示入口。
  const configNav = webapiEnabled ? CONFIG_NAV : CONFIG_NAV.filter(item => item.key !== 'integrations' && item.key !== 'audit')
  // 漏洞 / 报告依赖 webapi capability；未启用时不显示入口。
  const operationsNav = webapiEnabled
    ? OPERATIONS_NAV
    : OPERATIONS_NAV.filter(item => item.key !== 'vulns' && item.key !== 'reports')

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
        {/* 品牌区：图标方块 + 两行（产品名 / 副标题），折叠为图标栏时只留图标 */}
        <div className={cn('flex shrink-0', rail ? 'flex-col items-center justify-center gap-0.5 py-2' : 'items-center gap-2 px-3 py-2.5')}>
          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground shadow-sm">
            <ShieldCheck className="h-4 w-4" aria-hidden />
          </div>
          {!rail && (
            <div className="min-w-0 flex-1 truncate">
              <div className="truncate text-[13px] font-semibold leading-tight text-foreground">WScyber</div>
              <div className="truncate text-[10.5px] text-muted-foreground">{t('shell.brandSubtitle')}</div>
            </div>
          )}
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

        {/* 模型配置切换（整行，带状态圆点） */}
        {!rail && (
          <div className="shrink-0 px-2 pt-3">
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
          <NavGroup title={t('shell.groupOperations')} items={operationsNav} collapsed={rail}
            activePage={activePage ?? page} countOf={countOf} vulnNew={vulnCounts?.new ?? 0} onNavigate={onNavigate} />
          <NavGroup title={t('shell.groupConfig')} items={configNav} collapsed={rail}
            activePage={activePage ?? page} countOf={countOf} vulnNew={vulnCounts?.new ?? 0} onNavigate={onNavigate} />
        </nav>

        {/* 底部：原生工具图标行（弱化）→ 待审批整行警示卡 → 主题 · 中/EN · 退出 */}
        <div className="mt-auto shrink-0 border-t border-border pt-2">
          <div className={cn('flex gap-0.5 px-2 pb-1 opacity-70', rail ? 'flex-col items-center [&_button]:!w-9 [&_button]:!px-0 [&_button>span]:hidden' : 'flex-row flex-wrap items-center')}>
            <WorkbenchSlots services={services} name="shell.header.actions" />
            <QuickConnect serverURL={serverURL} version={version} profiles={connectionProfiles} />
          </div>
          <div className={cn('px-2 pb-2', rail && 'flex flex-col items-center')}>
            <div className={cn(rail && '[&_button]:w-9 [&_button]:justify-center [&_button]:px-0 [&_button>span]:hidden')}>
              <PendingApprovalsButton items={pendingApprovalItems} onResolve={onResolvePending} onViewSession={onViewSession}
                openRequested={approvalsRequested} onOpenRequestedConsumed={onApprovalsRequestedConsumed} />
            </div>
          </div>
          <div className={cn(
            'flex items-center justify-between gap-0.5 px-2 py-1 pb-[max(0.5rem,env(safe-area-inset-bottom))]',
            rail && 'flex-col',
          )}>
            <div data-sidebar-theme-toggle>
              <ThemeToggle isDark={isDark} onToggle={toggle} size="sm" toLightLabel={ts('switchToLight')} toDarkLabel={ts('switchToDark')} />
            </div>
            <LanguageToggle />
            <div className={cn('flex items-center gap-0.5', rail && 'flex-col')}>
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
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button type="button" variant="ghost" size="icon-xs" aria-label={ta('logout')} onClick={() => { void logout() }} className="text-muted-foreground hover:text-foreground">
                    <LogOut className="h-3.5 w-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{ta('logout')}</TooltipContent>
              </Tooltip>
            </div>
          </div>
        </div>
      </aside>
    </>
  )
}

function NavGroup({ title, items, collapsed, activePage, countOf, vulnNew, onNavigate }: {
  title: string
  items: NavDef[]
  collapsed: boolean
  activePage: AppPage
  countOf: (source: NavDef['count']) => number | null
  /** 漏洞新发现数：>0 时在总数旁显示红色圆形徽章。 */
  vulnNew: number
  onNavigate: (page: AppPage) => void
}) {
  const { t } = useTranslation('chat')
  const visible = items.filter((item) => item.enabled)
  if (visible.length === 0) return null
  return (
    <div className="pt-2">
      {!collapsed && <div className="px-2 pb-1 pt-3 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">{title}</div>}
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
                'group flex h-8 w-full items-center gap-2.5 rounded-md px-2.5 text-left text-[13px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
                active
                  ? 'bg-[var(--brand-soft)] text-[var(--brand-primary)]'
                  : 'text-secondary-foreground hover:bg-muted/60 hover:text-foreground',
                collapsed && 'justify-center px-0',
              )}
            >
              <Icon className="h-4 w-4 shrink-0" />
              {!collapsed && <span className="min-w-0 flex-1 truncate">{label}</span>}
              {!collapsed && count !== null && (
                item.count === 'vulns' && vulnNew > 0 ? (
                  <span className="ml-auto flex shrink-0 items-center gap-1">
                    <span className="text-[11px] tabular text-muted-foreground">{count}</span>
                    <span
                      className="rounded-full bg-[var(--st-new-dot)] px-1.5 text-[10.5px] font-semibold leading-4 tabular text-white"
                      title={t('shell.vulnStatusNew')}
                    >{vulnNew}</span>
                  </span>
                ) : (
                  <span className="ml-auto shrink-0 text-[11px] tabular text-muted-foreground">{count}</span>
                )
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
        className="h-8 w-full gap-1.5 rounded-md border-border bg-muted/60 px-2.5 text-xs text-foreground shadow-none transition-colors hover:border-primary/50"
      >
        <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-emerald-500" />
        <SelectValue placeholder={fallbackModel} className="min-w-0 flex-1 truncate text-left font-medium" />
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