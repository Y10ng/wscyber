import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  FileText, Gauge, Globe, PanelLeftClose, PanelLeft, ChevronDown, ChevronRight, Monitor,
  Boxes, List, MessageSquare, MoreHorizontal, Plus, RefreshCw, Trash2,
  Archive, ArchiveRestore, Pencil, Check, X, ShieldAlert, ShieldCheck,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import {
  Button, Tooltip, TooltipTrigger, TooltipContent,
  ThemeToggle, DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator,
} from '@cyber/ui'
import { cn, useTheme } from '@cyber/theme'
import LanguageToggle from './LanguageToggle'
import {
  APIError,
  getWebApiReportBySession,
  getWebApiRetestBySession,
  getWebApiTaskBySession,
  getWebApiVerificationBySession,
  type AgentView, type SessionRecord, type SessionFilters,
} from '../api'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import i18n from '../i18n'

function recordID(record: SessionRecord): string {
  return record.session?.id || ''
}

// ── 侧栏会话业务标题（追加 11 / fix-1 #6；R2 改为结构化缓存）────────────────
// 已识别为 Web/API 业务会话的条目显示「类型图标 + 业务标题」，未识别的保持原样。
// 仅前端展示：识别用已有 by-session 反查接口，结果按 session_id 缓存（模块级）。
// 缓存只存结构化数据（kind + 编号 + 标题），前缀文案在渲染期用 t() 组装 ——
// 识别时把语言字符串固化进缓存会让切换语言后仍显示旧前缀。
type WebApiSessionKind = 'task' | 'verification' | 'report' | 'retest'

interface WebApiSessionBadge {
  kind: WebApiSessionKind
  /** 任务/验证/复测：候选编号（WSC-xxxx）；报告无编号。 */
  number: string
  /** 标题（任务为 objective 首行；报告为草稿标题）。 */
  title: string
}

const webApiBadgeCache = new Map<string, WebApiSessionBadge | null>()
const webApiBadgeCacheAt = new Map<string, number>()
const webApiBadgeInflight = new Set<string>()
// 「非业务会话」的 null 也缓存以免重复反查，但缓存会过期（30s）：识别链依赖三条
// by-session 接口与报告保存的先后，早期（如报告行尚未落库）被误判 null 的会话能在
// 下次列表刷新时自愈，而不是永久显示首条消息原文。
const WEB_API_BADGE_NULL_TTL_MS = 30_000

// 渲染期组装徽标文案（语言随当前界面）。zh：任务 / 验证 · / 报告 · / 复测 ·；
// en：Verification · / Report · / Retest ·。编号与标题保留空格分隔。
// 供顶栏「待审批」等外部组件复用已解析的业务标题缓存（渲染期用当前语言组装）。
export function cachedWebApiBadgeLabel(sessionID: string, t: (key: string) => string): string | null {
  const badge = webApiBadgeCache.get(sessionID)
  return badge ? badgeLabel(badge, t) : null
}

function badgeLabel(badge: WebApiSessionBadge, t: (key: string) => string): string {
  const body = [badge.number, badge.title].filter(Boolean).join(' ')
  if (badge.kind === 'task') return body || badge.title
  const prefixKey = badge.kind === 'verification' ? 'webapi.verification'
    : badge.kind === 'report' ? 'webapi.reportSingular' : 'webapi.retestTitle'
  const prefix = t(prefixKey)
  return body ? `${prefix} · ${body}` : prefix
}

function badgeIcon(kind: WebApiSessionKind): LucideIcon {
  if (kind === 'verification') return ShieldCheck
  if (kind === 'report') return FileText
  if (kind === 'retest') return RefreshCw
  return Globe
}

function firstLine(text: string): string {
  const line = text.split('\n').map((part) => part.trim()).find(Boolean)
  return line || text.trim()
}

interface BadgeResolution {
  badge: WebApiSessionBadge | null
  /** true = 存在非 404（网络/服务端）失败，结果不确定，不缓存。 */
  transient: boolean
}

// 反查顺序：任务 → 验证 → 报告 → 复测。任务接口对验证/复测 session 也 200 回父任务，
// 必须比对 initial_session_id；真实 404 视为「非业务会话」，网络错误标记 transient。
// 只缓存结构化数据（kind + 编号 + 标题）；前缀文案渲染期用 t() 组装。
async function resolveWebApiBadge(sessionID: string): Promise<BadgeResolution> {
  let transient = false
  try {
    const task = await getWebApiTaskBySession(sessionID)
    if (task.initial_session_id === sessionID) {
      const objective = firstLine(task.objective)
      if (objective) return { badge: { kind: 'task', number: '', title: objective }, transient: false }
    }
  } catch (err) {
    transient = transient || !isNotFound(err)
  }
  try {
    const verification = await getWebApiVerificationBySession(sessionID)
    if (verification.session_id === sessionID) {
      return {
        badge: {
          kind: 'verification',
          number: verification.source?.number || '',
          title: verification.source?.content?.title || '',
        },
        transient: false,
      }
    }
  } catch (err) {
    transient = transient || !isNotFound(err)
  }
  try {
    const report = await getWebApiReportBySession(sessionID)
    if (report.session_id === sessionID) {
      return {
        badge: {
          kind: 'report',
          number: '',
          title: report.draft?.payload?.title?.trim() || '',
        },
        transient: false,
      }
    }
  } catch (err) {
    transient = transient || !isNotFound(err)
  }
  try {
    const retest = await getWebApiRetestBySession(sessionID)
    if (retest.session_id === sessionID) {
      return {
        badge: {
          kind: 'retest',
          number: retest.source?.number || '',
          title: retest.source?.content?.title || '',
        },
        transient: false,
      }
    }
  } catch (err) {
    transient = transient || !isNotFound(err)
  }
  return { badge: null, transient }
}

function isNotFound(err: unknown): boolean {
  return err instanceof APIError && err.status === 404
}

function useWebApiSessionBadges(sessions: SessionRecord[]): Record<string, WebApiSessionBadge> {
  const [badges, setBadges] = useState<Record<string, WebApiSessionBadge>>({})
  useEffect(() => {
    const now = Date.now()
    for (const record of sessions) {
      const id = record.session?.id
      if (!id || webApiBadgeInflight.has(id)) continue
      if (webApiBadgeCache.has(id)) {
        // null 缓存过期后允许重试；徽标缓存不设 TTL（一旦识别即业务行已存在）。
        if (webApiBadgeCache.get(id) || now - (webApiBadgeCacheAt.get(id) || 0) < WEB_API_BADGE_NULL_TTL_MS) continue
        webApiBadgeCache.delete(id)
      }
      webApiBadgeInflight.add(id)
      void (async () => {
        let resolution: BadgeResolution = { badge: null, transient: true }
        try {
          resolution = await resolveWebApiBadge(id)
        } catch { /* 兜底：视为不确定结果 */ }
        webApiBadgeInflight.delete(id)
        // 确定「非业务会话」的 null 也缓存，避免重复反查；transient 不缓存。
        if (!resolution.transient) {
          webApiBadgeCache.set(id, resolution.badge)
          webApiBadgeCacheAt.set(id, Date.now())
        }
        // 不看 effect 的 cancelled：解析写的是模块级缓存（幂等回填），sessions 变化只影响
        // 「本组件挂载期」的语义；丢弃已完成结果会让报告会话创建后（sessions 不再变化）永久
        // 显示首条消息原文（fix-2 #2 实测根因）。
        if (resolution.badge) setBadges((prev) => ({ ...prev, [id]: resolution.badge! }))
      })()
    }
    // 已缓存条目同步进 state（组件重挂载不丢徽标）。
    const cached: Record<string, WebApiSessionBadge> = {}
    for (const record of sessions) {
      const id = record.session?.id
      const badge = id ? webApiBadgeCache.get(id) : undefined
      if (id && badge) cached[id] = badge
    }
    setBadges((prev) => ({ ...cached, ...prev }))
  }, [sessions])
  return badges
}

interface Props {
  open: boolean
  onToggle: () => void
  agents?: AgentView[]
  sessions?: SessionRecord[]
  pendingReviewCounts?: Record<string, number>
  filters: SessionFilters
  onFilter: (patch: Partial<SessionFilters>) => void
  onUpdateSession: (id: string, patch: { title?: string; archived?: boolean }) => Promise<void>
  activeSessionID: string | null
  activeSessionNodeID: string | null
  activeSessionBusy: boolean
  selectedNodeID: string | null
  onSelectSession: (id: string) => void
  onCreateSession: (nodeID: string) => void
  onDeleteSession: (id: string) => void
  /** 全局用量入口（webapi capability 可用时提供；标题行「用量」按钮）。 */
  onOpenUsage?: () => void
  /** 资产视图入口（webapi capability 可用时提供；在「用量」左侧）。 */
  onOpenAssets?: () => void
}

export default function SessionList({
  open, onToggle, agents = [], sessions = [], pendingReviewCounts = {}, filters, onFilter,
  activeSessionID, activeSessionNodeID, activeSessionBusy, selectedNodeID,
  onSelectSession, onCreateSession, onDeleteSession, onUpdateSession, onOpenUsage, onOpenAssets,
}: Props) {
  const { t } = useTranslation('sidebar')
  const { t: tc } = useTranslation('chat')
  const webApiBadges = useWebApiSessionBadges(sessions)
  const closeButtonRef = useRef<HTMLButtonElement>(null)
  const toggleRef = useRef(onToggle)
  useEffect(() => { toggleRef.current = onToggle }, [onToggle])
  useEffect(() => {
    if (!open || !window.matchMedia('(max-width: 767px)').matches) return
    const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') toggleRef.current()
    }
    closeButtonRef.current?.focus()
    window.addEventListener('keydown', onKeyDown)
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      previouslyFocused?.focus()
    }
  }, [open])
  const revealedSessionRef = useRef<string | null>(null)
  useEffect(() => {
    if (!activeSessionID || !activeSessionNodeID || revealedSessionRef.current === activeSessionID) return
    const visible = sessions.some((session) => recordID(session) === activeSessionID)
    if (!visible) return
    revealedSessionRef.current = activeSessionID
    if (filters.nodeId !== activeSessionNodeID) onFilter({ nodeId: activeSessionNodeID })
  }, [activeSessionID, activeSessionNodeID, sessions, filters.nodeId, onFilter])
  const newTaskNodeID = filters.nodeId
    ? agents.find((agent) => agent.hello?.nodeId === filters.nodeId)?.hello?.nodeId
    : agents.find((agent) => agent.hello?.nodeId === selectedNodeID)?.hello?.nodeId || agents[0]?.hello?.nodeId
  const groups = new Map<string, { name: string; online: boolean; sessions: SessionRecord[] }>()
  for (const agent of agents) {
    const id = agent.hello?.nodeId
    if (id) groups.set(id, { name: agent.hello?.name || id, online: true, sessions: [] })
  }
  for (const session of sessions) {
    const id = session.session?.nodeId
    if (!id) continue
    const group = groups.get(id) || { name: session.agentName || id, online: false, sessions: [] }
    group.sessions.push(session)
    groups.set(id, group)
  }
  if (filters.nodeId && !groups.has(filters.nodeId)) groups.set(filters.nodeId, { name: filters.nodeId, online: false, sessions: [] })
  const visibleGroups = [...groups].filter(([id, group]) => (!filters.search && !filters.archived) || group.sessions.length > 0 || id === filters.nodeId)
  const visibleTaskCount = filters.nodeId ? groups.get(filters.nodeId)?.sessions.length || 0 : sessions.length
  const emptyMessage = filters.search ? 'noMatchingTasks' : filters.archived ? 'noArchivedTasks' : filters.nodeId ? 'noNodeTasks' : 'noTasksYet'

  return (
    <>
      {open && (
        <button
          type="button"
          aria-label={t('closeSidebarOverlay')}
          onClick={onToggle}
          className="fixed inset-0 z-30 bg-background/60 backdrop-blur-[1px] md:hidden"
        />
      )}
      <aside
        className={cn(
          'surface-raised flex flex-col border-r border-border bg-card/95 backdrop-blur-sm transition-all duration-200 ease-in-out shrink-0 md:bg-card/50',
          open
            ? 'fixed bottom-0 left-0 top-[calc(env(safe-area-inset-top)+3rem)] z-40 w-72 shadow-elevated md:relative md:inset-auto md:z-auto md:shadow-none'
            // Collapsed: a 48px icon rail on desktop, but fully hidden on phones —
            // there the drawer opens from the header's menu button, so the chat
            // gets the full width instead of a stub rail down the left edge.
            : 'w-12 max-md:hidden',
        )}
      >
        <div className={cn('flex border-b border-border/60', open ? 'items-center gap-2 px-3 py-2.5' : 'flex-col items-center gap-2 p-2')}>
          {open ? (
            <>
              <span className="min-w-0 flex-1 truncate text-xs font-semibold text-foreground">{t('tasks')}</span>
              {onOpenAssets && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button variant="ghost" size="icon" onClick={onOpenAssets} className="h-7 w-7 text-muted-foreground" aria-label={tc('webapi.assetsView')}>
                      <Boxes className="h-4 w-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{tc('webapi.assetsView')}</TooltipContent>
                </Tooltip>
              )}
              {onOpenUsage && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button variant="ghost" size="icon" onClick={onOpenUsage} className="h-7 w-7 text-muted-foreground" aria-label={tc('webapi.usageView')}>
                      <Gauge className="h-4 w-4" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>{tc('webapi.usageView')}</TooltipContent>
                </Tooltip>
              )}
              <Button ref={closeButtonRef} variant="ghost" size="icon" onClick={onToggle} className="h-7 w-7 text-muted-foreground" aria-label={t('collapseSidebar')}>
                <PanelLeftClose className="w-4 h-4" />
              </Button>
            </>
          ) : (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon-sm" onClick={onToggle} aria-label={t('expandSidebar')} className="rounded-lg text-muted-foreground">
                  <PanelLeft className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="right">{t('expandSidebar')}</TooltipContent>
            </Tooltip>
          )}
        </div>

        {open && <div className="flex items-center gap-1 border-b border-border/60 p-2.5">
          <input aria-label={t('searchTasks')} placeholder={t('searchTasks')} value={filters.search} onChange={(event) => onFilter({ search: event.target.value })} className="w-full rounded-md border border-border bg-background px-2 py-1.5 text-xs" />
          <Button size="icon-xs" variant={filters.archived ? 'secondary' : 'ghost'} aria-label={t('archived')} title={t('archived')} aria-pressed={filters.archived} onClick={() => onFilter({ archived: !filters.archived })}><Archive className="h-3.5 w-3.5" /></Button>
        </div>}

        {open ? (
          <div className="flex-1 overflow-auto p-2.5 animate-fade-in">
            <div className="space-y-1">
              <button type="button" aria-pressed={!filters.nodeId} onClick={() => onFilter({ nodeId: '' })} className={cn('flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-xs', !filters.nodeId ? 'bg-accent font-medium text-foreground' : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground')}>
                <List className="h-3.5 w-3.5 shrink-0" />
                <span className="min-w-0 flex-1 truncate">{t('allTasks')}</span>
                <span className="tabular-nums text-[10px] text-muted-foreground">{sessions.length}</span>
              </button>
              {visibleGroups.map(([id, group]) => {
                const expanded = !filters.nodeId || filters.nodeId === id
                return <div key={id} data-node-id={id}>
                  <div className={cn('group flex h-9 items-center gap-1 rounded-md px-1.5', filters.nodeId === id ? 'bg-accent' : 'hover:bg-accent/50')}>
                    <button type="button" aria-expanded={expanded} aria-pressed={filters.nodeId === id} onClick={() => onFilter({ nodeId: id })} className="flex min-w-0 flex-1 items-center gap-2 text-left">
                      {expanded ? <ChevronDown className="h-3.5 w-3.5 shrink-0" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0" />}
                      <Monitor className={cn('h-3.5 w-3.5 shrink-0', group.online ? 'text-primary' : 'text-muted-foreground/50')} />
                      <span className="truncate text-xs font-medium">{group.name}</span>
                      {!group.online && <span className="shrink-0 text-[9px] text-warning">{t('agentOffline')}</span>}
                      <span className="ml-auto shrink-0 text-[10px] tabular-nums text-muted-foreground">{group.sessions.length}</span>
                    </button>
                    {group.online && <Button size="icon-xs" variant="ghost" aria-label={t('newTaskForNode', { name: group.name })} title={t('newTask')} onClick={() => { onFilter({ nodeId: id, search: '', archived: false }); onCreateSession(id) }}><Plus className="h-3.5 w-3.5" /></Button>}
                  </div>
                  {expanded && group.sessions.length > 0 && <div className="ml-4 space-y-0.5 border-l border-border/70 pl-1.5">
                    {group.sessions.map((session) => <SessionItem
                      key={recordID(session)}
                      session={session}
                      badge={webApiBadges[recordID(session)] || null}
                      active={recordID(session) === activeSessionID}
                      busy={recordID(session) === activeSessionID && activeSessionBusy}
                      pendingReviews={pendingReviewCounts[recordID(session)] || 0}
                      onSelect={() => onSelectSession(recordID(session))}
                      onDelete={() => onDeleteSession(recordID(session))}
                      onUpdate={(patch) => onUpdateSession(recordID(session), patch)}
                    />)}
                  </div>}
                </div>
              })}
              {visibleTaskCount === 0 && <div className="space-y-1 px-2 py-3 text-xs text-muted-foreground">
                <p>{t(emptyMessage)}</p>
                {filters.search ? <button type="button" className="text-primary hover:underline" onClick={() => onFilter({ search: '' })}>{t('clearSearch')}</button>
                  : filters.archived ? <button type="button" className="text-primary hover:underline" onClick={() => onFilter({ archived: false })}>{t('showActiveTasks')}</button>
                    : filters.nodeId ? <button type="button" className="text-primary hover:underline" onClick={() => onFilter({ nodeId: '' })}>{t('allTasks')}</button>
                      : null}
              </div>}
            </div>
          </div>
        ) : (
          <div className="flex flex-col items-center pt-3">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon-sm" disabled={!newTaskNodeID} onClick={() => newTaskNodeID && onCreateSession(newTaskNodeID)} aria-label={t('newTask')}>
                  <Plus className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="right">{t('newTask')}</TooltipContent>
            </Tooltip>
          </div>
        )}

        <SidebarPreferences expanded={open} />
      </aside>
    </>
  )
}

function SidebarPreferences({ expanded }: { expanded: boolean }) {
  const { isDark, toggle } = useTheme()
  const { t } = useTranslation('sidebar')

  return (
    <div className={cn(
      'mt-auto shrink-0 border-t border-border/60',
      expanded
        ? 'flex items-center justify-end gap-1 px-3 py-2 pb-[max(0.5rem,env(safe-area-inset-bottom))]'
        : 'flex flex-col items-center gap-1 p-2',
    )}>
      <LanguageToggle />
      <div data-sidebar-theme-toggle>
        <ThemeToggle
          isDark={isDark}
          onToggle={toggle}
          size="sm"
          toLightLabel={t('switchToLight')}
          toDarkLabel={t('switchToDark')}
        />
      </div>
    </div>
  )
}

function SessionItem({
  session, badge, active, busy, pendingReviews, onSelect, onDelete, onUpdate,
}: {
  session: SessionRecord
  badge: WebApiSessionBadge | null
  active: boolean
  busy: boolean
  pendingReviews: number
  onSelect: () => void
  onDelete: () => void
  onUpdate: (patch: { title?: string; archived?: boolean }) => Promise<void>
}) {
  const { t } = useTranslation('sidebar')
  // 业务徽标前缀文案用 chat 命名空间渲染期组装（结构化缓存 + t()，语言随界面切换）。
  const { t: tc } = useTranslation('chat')
  const [editing, setEditing] = useState(false)
  const [titleDraft, setTitleDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const itemRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (active) itemRef.current?.scrollIntoView({ block: 'nearest' })
  }, [active])
  const save = async (patch: { title?: string; archived?: boolean }) => {
    if (saving) return
    setSaving(true)
    try { await onUpdate(patch); setEditing(false) } catch { /* parent displays error */ }
    finally { setSaving(false) }
  }

  const title = session.session?.title || t('newSession')
  const updatedAt = session.updatedAt ? timestampDate(session.updatedAt) : null
  const time = (updatedAt || new Date(0)).toLocaleDateString(i18n.language, { month: 'short', day: 'numeric' })
  const closed = session.session?.state === 'closed'
  const badgeText = badge ? badgeLabel(badge, tc) : ''
  const BadgeIcon = badge ? badgeIcon(badge.kind) : null

  return (
    <div
      ref={itemRef}
      data-session-id={recordID(session)}
      className={cn(
        'group flex min-h-10 items-center gap-1 rounded-md px-2 py-1 transition-colors',
        active ? 'bg-primary/10 text-foreground' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
      )}
    >
      {editing ? <form className="flex min-w-0 flex-1 items-center" onSubmit={(event) => { event.preventDefault(); void save({ title: titleDraft }) }}>
        <input autoFocus aria-label={t('renameTask')} maxLength={200} value={titleDraft} onChange={(event) => setTitleDraft(event.target.value)} className="min-w-0 flex-1 rounded border border-border bg-background px-1 text-xs" />
        <button disabled={saving || !titleDraft.trim()} aria-label={t('saveTitle')}><Check className="h-3 w-3" /></button>
        <button type="button" onClick={() => setEditing(false)} aria-label={t('cancelEdit')}><X className="h-3 w-3" /></button>
      </form> : <>
      <button type="button" onClick={onSelect} className="flex-1 min-w-0 text-left">
        <div className="flex items-center gap-1.5">
          {badge && BadgeIcon ? (
            // 业务会话：12px 类型图标（任务=Globe，验证=ShieldCheck，报告=FileText，复测=RefreshCw）+ 业务标题。
            <BadgeIcon className="h-3 w-3 shrink-0" />
          ) : busy ? <span className="h-2 w-2 shrink-0 animate-pulse rounded-full bg-warning" role="status" aria-label={t('runningTask')} title={t('runningTask')} />
            : closed ? <span className="h-2 w-2 shrink-0 rounded-full bg-muted-foreground/50" role="status" aria-label={t('closedTask')} title={t('closedTask')} />
              : <MessageSquare className="h-2.5 w-2.5 shrink-0" />}
          <span className="truncate text-[11px] font-medium" title={badge ? badgeText : undefined}>{badge ? badgeText : title}</span>
          {pendingReviews > 0 && <span className="ml-auto inline-flex items-center gap-0.5 text-warning" title={t('pendingApprovals', { count: pendingReviews })} data-pending-reviews={pendingReviews}><ShieldAlert className="h-3 w-3" /><span className="text-[9px] tabular-nums">{pendingReviews}</span></span>}
        </div>
        <div className="mt-0.5 text-[9px] text-muted-foreground">{time}</div>
      </button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button size="icon-xs" variant="ghost" aria-label={t('taskActions', { title })} className="h-7 w-7 shrink-0 text-muted-foreground"><MoreHorizontal className="h-3.5 w-3.5" /></Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => { setTitleDraft(title); setEditing(true) }}><Pencil className="mr-2 h-3.5 w-3.5" />{t('renameTask')}</DropdownMenuItem>
          <DropdownMenuItem disabled={saving} onSelect={() => void save({ archived: !session.archived })}>{session.archived ? <ArchiveRestore className="mr-2 h-3.5 w-3.5" /> : <Archive className="mr-2 h-3.5 w-3.5" />}{t(session.archived ? 'restoreTask' : 'archiveTask')}</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={onDelete}><Trash2 className="mr-2 h-3.5 w-3.5" />{t('deleteSession')}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      </>}
    </div>
  )
}
