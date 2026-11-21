import { lazy, Suspense, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { LoaderCircle, Monitor, Search, Info } from 'lucide-react'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import type { AgentView } from '../api'
const terminalChunkReloadKey = 'cyber-terminal-chunk-reload'
const loadAgentTerminal = () => import('./terminal')
async function loadAgentTerminalWithRecovery() {
  try {
    const module = await loadAgentTerminal()
    window.sessionStorage.removeItem(terminalChunkReloadKey)
    return module
  } catch (error) {
    if (!window.sessionStorage.getItem(terminalChunkReloadKey)) {
      window.sessionStorage.setItem(terminalChunkReloadKey, '1')
      window.location.reload()
      return new Promise<Awaited<ReturnType<typeof loadAgentTerminal>>>(() => {})
    }
    window.sessionStorage.removeItem(terminalChunkReloadKey)
    throw error
  }
}
const AgentTerminal = lazy(loadAgentTerminalWithRecovery)
import {
  Badge,
  EmptyState,
  Input,
  ListRow,
  StatusDot,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@cyber/ui'
import { ToolDrawer } from './layout/ToolDrawer'
import { isLocalModelAgent } from '../lib/session-agent'
import { NodeStateBadge } from './ui-badges'
import { formatDateTime } from '../lib/time'

interface AgentPanelProps {
  open: boolean
  /** 'page' 内嵌为页面卡片（不渲染 ToolDrawer）；缺省 'drawer' 保持原抽屉行为。 */
  variant?: 'drawer' | 'page'
  /** The roster from useChatSession — avoids a redundant listAgents poll. */
  agents: AgentView[]
  /** When opened from a deck node click, focus this agent's console. */
  focusNodeID?: string
  onClose: () => void
}

export default function AgentPanel({ open, variant = 'drawer', agents: rosterAgents, focusNodeID, onClose }: AgentPanelProps) {
  const { t } = useTranslation('agent')
  const { agents, selected, selectedNodeID, setSelectedNodeID } = useAgentDirectory(open, rosterAgents, focusNodeID)
  const showAgentList = agents.length > 1
  const description = selected ? `${selected.hello?.name || ''} · ${selected.busy ? t('busy') : t('idle')}` : t('noAgentSelected')
  const titleMeta = (
    <Badge variant="secondary" size="sm" className="py-0 font-mono font-normal">
      {agents.length}
    </Badge>
  )
  // 抽屉与页面共用同一份内容：空状态 / 列表 + 终端。
  const content = agents.length === 0 ? (
    <div className="flex h-full items-center justify-center">
      <EmptyState icon={Monitor} title={t('noAgentsConnected')} />
    </div>
  ) : (
    <div className="flex h-full min-h-0 flex-col lg:flex-row">
      {showAgentList && (
        <AgentList
          agents={agents}
          selectedNodeID={selectedNodeID}
          onSelect={setSelectedNodeID}
        />
      )}
      <section className="flex min-h-0 min-w-0 flex-1 flex-col">
        {selected && (isLocalModelAgent(selected) ? (
          <LocalModelAgentStatus agent={selected} />
        ) : (
          <Suspense fallback={(
            <div className="flex flex-1 items-center justify-center gap-2 text-xs text-muted-foreground" role="status">
              <LoaderCircle className="h-4 w-4 animate-spin text-primary" aria-hidden="true" />
              <span>{t('openingTerminal')}</span>
            </div>
          )}>
            <AgentTerminal agent={selected} />
          </Suspense>
        ))}
      </section>
    </div>
  )

  if (variant === 'page') {
    // 页面模式：每台节点一张卡（名称 + NodeStateBadge + 右侧连接时间），卡内两列信息。
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto">
        {agents.length === 0 ? (
          <div className="flex min-h-[320px] items-center justify-center rounded-lg border border-border bg-card">
            <EmptyState icon={Monitor} title={t('noAgentsConnected')} />
          </div>
        ) : (
          [...agents]
            .sort((a, b) => Number(b.busy) - Number(a.busy) || (a.hello?.name || '').localeCompare(b.hello?.name || ''))
            .map((agent) => (
              <NodeCard key={agent.hello?.nodeId} agent={agent} />
            ))
        )}
      </div>
    )
  }

  return (
    <ToolDrawer
      open={open}
      onClose={onClose}
      icon={Monitor}
      title={t('agentConsole')}
      description={description}
      titleMeta={titleMeta}
      contentProps={{
        // The console advertises "Esc 中断". Escape from inside the terminal must
        // interrupt the running task only — otherwise the same keypress also
        // dismisses the drawer and takes the output with it.
        onEscapeKeyDown: (event: KeyboardEvent) => {
          if (document.activeElement?.closest('.xterm')) event.preventDefault()
        },
      }}
    >
      {content}
    </ToolDrawer>
  )
}

// Derives selection state from the parent-provided roster (useChatSession
// already polls listAgents every 5s). No internal fetch or polling — the agent
// list is a prop, so the panel never double-polls.
function useAgentDirectory(open: boolean, roster: AgentView[], focusNodeID?: string) {
  const [selectedNodeID, setSelectedNodeID] = useState('')

  // Keep selection valid as the roster changes (agents disconnect / reconnect).
  useEffect(() => {
    setSelectedNodeID((current) => roster.some((a) => a.hello?.nodeId === current) ? current : roster[0]?.hello?.nodeId || '')
  }, [roster])

  // Focus a specific node when the panel is opened from a deck node click.
  // Apply it exactly ONCE per open/focus request: `roster` is in the deps only
  // so we can wait for the roster to load, but the 5s poll replaces `roster`
  // every tick — without this guard the effect would re-fire and yank the
  // selection back to the focused node, defeating any manual agent switch.
  const focusAppliedRef = useRef(false)
  useEffect(() => {
    focusAppliedRef.current = false
  }, [open, focusNodeID])
  useEffect(() => {
    if (focusAppliedRef.current) return
    if (open && focusNodeID && roster.some((a) => a.hello?.nodeId === focusNodeID)) {
      setSelectedNodeID(focusNodeID)
      focusAppliedRef.current = true
    }
  }, [open, focusNodeID, roster])

  const selected = roster.find((agent) => agent.hello?.nodeId === selectedNodeID) || roster[0] || null

  return { agents: roster, selected, selectedNodeID, setSelectedNodeID }
}

// Local controlled model node: there is no PTY/console to mount (mounting
// AgentTerminal would issue PTY requests). Show the node's real reported status
// instead; connectedAt is the connection moment, not a per-save apply ack.
function LocalModelAgentStatus({ agent }: { agent: AgentView }) {
  const { t } = useTranslation('agent')
  const status = agent.status
  const reported = [status?.provider || '—', status?.model || '—'].join(' / ')
  const notConfigured = !status?.provider?.trim() || !status?.model?.trim()
  const connectedAt = agent.connectedAt ? formatDateTime(timestampDate(agent.connectedAt).toISOString()) : '—'
  return (
    <div className="flex h-full min-h-0 flex-col gap-3 overflow-auto p-4" data-testid="local-model-status">
      {/* 本机受控模式提示：卡内顶部提示条（--brand-soft 底、12px）。 */}
      <p className="rounded-md px-3 py-2 text-[12px] leading-relaxed text-foreground" style={{ background: 'var(--brand-soft)' }}>
        {t('localModelNote')}
      </p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div className="min-w-0">
          <div className="text-[12px] text-muted-foreground">{t('nodeReportedStatus')}</div>
          <div className="mt-1 break-words text-[13px] text-foreground">{reported}</div>
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-1 text-[12px] text-muted-foreground">
            <span>{t('statusConnectedAt')}</span>
            <Tooltip>
              <TooltipTrigger asChild>
                <button type="button" aria-label={t('statusConnectedAtHint')} className="inline-flex text-muted-foreground/70 hover:text-foreground">
                  <Info className="h-3 w-3" />
                </button>
              </TooltipTrigger>
              <TooltipContent>{t('statusConnectedAtHint')}</TooltipContent>
            </Tooltip>
          </div>
          <div className="mt-1 font-mono text-[13px] tabular text-foreground">{connectedAt}</div>
        </div>
      </div>
      {status?.configError ? (
        <div className="text-[12px] text-warning">{t('statusApplyUncertain')}</div>
      ) : notConfigured ? (
        <div className="text-[12px] text-muted-foreground">{t('statusNotConfigured')}</div>
      ) : null}
      {status?.configError ? <p className="rounded-md bg-muted/60 px-3 py-2 text-[12px] text-warning">{status.configError}</p> : null}
    </div>
  )
}

function NodeCard({ agent }: { agent: AgentView }) {
  const { t } = useTranslation('agent')
  const runtime = agent.hello?.runtime
  const status = agent.status
  const connectedAt = agent.connectedAt ? formatDateTime(timestampDate(agent.connectedAt).toISOString()) : '—'
  const isLocal = isLocalModelAgent(agent)
  const info: Array<{ label: string; value: string }> = [
    { label: t('nodeModel'), value: [status?.provider, status?.model].filter(Boolean).join(' / ') || '—' },
    { label: t('nodeCapabilities'), value: String(agent.hello?.capabilities.length || 0) },
    { label: t('nodeTools'), value: String(agent.commands.length) },
    { label: t('nodeHost'), value: runtime?.hostname || '—' },
  ]
  return (
    <section className="rounded-lg border border-border bg-card p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Monitor className="h-4 w-4 shrink-0 text-primary" aria-hidden="true" />
        <h3 className="min-w-0 truncate text-[14px] font-semibold text-foreground" title={agent.hello?.name}>{agent.hello?.name || agent.hello?.nodeId}</h3>
        <NodeStateBadge state={agent.busy ? 'busy' : 'idle'} />
        {isLocal && <span className="rounded-full border border-border px-2 py-0.5 text-[11px] text-muted-foreground">{t('localModelNode')}</span>}
        <span className="ml-auto shrink-0 text-[12px] text-muted-foreground">
          {t('statusConnectedAt')} <span className="font-mono tabular text-foreground">{connectedAt}</span>
        </span>
      </div>
      {isLocal && (
        <p className="mt-3 rounded-md px-3 py-2 text-[12px] leading-relaxed text-foreground" style={{ background: 'var(--brand-soft)' }}>
          {t('localModelNote')}
        </p>
      )}
      <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-border pt-3 text-[12px]">
        {info.map((item) => (
          <div key={item.label} className="min-w-0">
            <span className="block text-muted-foreground">{item.label}</span>
            <span className="mt-0.5 block truncate text-foreground" title={item.value}>{item.value}</span>
          </div>
        ))}
      </div>
      {status?.configError ? <p className="mt-2 text-[12px] text-warning">{status.configError}</p> : null}
    </section>
  )
}

function AgentList({
  agents,
  onSelect,
  selectedNodeID,
}: {
  agents: AgentView[]
  onSelect: (nodeID: string) => void
  selectedNodeID: string
}) {
  const { t } = useTranslation('agent')
  const [query, setQuery] = useState('')

  // Busy agents first, then alphabetical — keeps active nodes at the top.
  const sorted = useMemo(
    () =>
      [...agents].sort((a, b) => {
        if (a.busy !== b.busy) return a.busy ? -1 : 1
        return (a.hello?.name || '').localeCompare(b.hello?.name || '')
      }),
    [agents],
  )
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return sorted
    return sorted.filter((a) =>
      `${a.hello?.name || ''} ${a.status?.model || ''} ${a.status?.provider || ''} ${a.hello?.runtime?.hostname || ''}`
        .toLowerCase()
        .includes(q),
    )
  }, [sorted, query])
  const busy = agents.filter((a) => a.busy).length
  const showFilter = agents.length > 6

  return (
    <aside className="flex max-h-52 w-full shrink-0 flex-col border-b border-border lg:max-h-none lg:w-64 lg:border-b-0 lg:border-r">
      <div className="flex h-10 items-center border-b border-border px-3">
        <span className="text-xs font-medium uppercase text-muted-foreground">
          {t('agents')}
          <span className="ml-1.5 font-mono text-[10px] normal-case text-muted-foreground/60">
            {busy}/{agents.length}
          </span>
        </span>
      </div>
      {showFilter && (
        <div className="border-b border-border p-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground/60" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('filterAgents')}
              aria-label={t('filterAgents')}
              className="h-8 pl-7 text-xs"
            />
          </div>
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {filtered.length === 0 ? (
          <EmptyState compact title={t('noMatchingAgents')} />
        ) : (
          filtered.map((agent) => (
            <ListRow
              key={agent.hello?.nodeId}
              active={selectedNodeID === agent.hello?.nodeId}
              leading={<StatusDot status={agent.busy ? 'warning' : 'info'} className="mt-1" />}
              onClick={() => onSelect(agent.hello?.nodeId || '')}
              title={agentDetails(agent)}
              className="mb-1"
            >
              <span className="block truncate text-sm font-medium">{agent.hello?.name}</span>
              <span className="mt-0.5 block truncate text-xs">
                {agent.busy ? t('busy') : t('idle')} · {formatRelativeTime(agent.connectedAt ? timestampDate(agent.connectedAt).toISOString() : '', t)}
              </span>
            </ListRow>
          ))
        )}
      </div>
    </aside>
  )
}

function agentDetails(agent: AgentView) {
  const runtime = agent.hello?.runtime
  const status = agent.status
  const stats = agent.stats
  const parts = [
    `name: ${agent.hello?.name || ''}`,
    `id: ${agent.hello?.nodeId || ''}`,
    `state: ${agent.busy ? 'busy' : 'idle'}`,
    `connected: ${agent.connectedAt ? formatDateTime(timestampDate(agent.connectedAt).toISOString()) : ''}`,
    runtime?.hostname ? `host: ${runtime.hostname}` : '',
    runtime?.username ? `user: ${runtime.username}` : '',
    runtime?.workingDir ? `cwd: ${runtime.workingDir}` : '',
    runtime?.os || runtime?.arch ? `runtime: ${[runtime.os, runtime.arch].filter(Boolean).join('/')}` : '',
    runtime?.pid ? `pid: ${runtime.pid}` : '',
    status?.provider || status?.model ? `llm: ${[status.provider, status.model].filter(Boolean).join(' / ')}` : '',
    agent.commands.length ? `commands: ${agent.commands.map((command) => command.name).join(', ')}` : '',
    agent.hello?.capabilities.length ? `capabilities: ${agent.hello.capabilities.join(', ')}` : '',
    stats ? `turns: ${stats.turns}` : '',
    stats ? `tool calls: ${stats.toolCalls}` : '',
    stats ? `tokens: ${stats.totalTokens}` : '',
  ]
  return parts.filter(Boolean).join('\n')
}

function formatRelativeTime(iso: string, t: TFunction<'agent'>): string {
  try {
    const diff = Date.now() - new Date(iso).getTime()
    const mins = Math.floor(diff / 60000)
    if (mins < 1) return t('justNow')
    if (mins < 60) return t('minutesAgo', { count: mins })
    const hours = Math.floor(mins / 60)
    if (hours < 24) return t('hoursAgo', { count: hours })
    return t('daysAgo', { count: Math.floor(hours / 24) })
  } catch {
    return ''
  }
}
