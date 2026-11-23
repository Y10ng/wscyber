import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ArrowRight, Check, HelpCircle, MessageSquare, MoreHorizontal, Pencil, Plus, RefreshCw, Trash2, X,
} from 'lucide-react'
import {
  Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger,
  EmptyState, Input, Spinner, Tooltip, TooltipContent, TooltipTrigger, useConfirm,
} from '@cyber/ui'
import { cn } from '@cyber/theme'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { FilterPills, Page, PageHeader, TableCard } from './PageFrame'
import {
  APIError,
  deleteChatSession,
  getWebApiSessionKinds,
  getWebApiTaskBySession,
  listChatSessions,
  updateChatSession,
  type AgentView,
  type SessionRecord,
  type WebApiSessionKindItem,
} from '../api'
import { formatDateTime } from '../lib/time'

// 普通对话页（U2g）：只列不属于任何 Web/API 任务的原生会话。
// 归属判定只用既有接口，不按标题/首条消息猜：
//   1. POST /session-kinds 批量识别 任务/验证/复测/报告 会话（一次最多 500 个）；
//   2. 剩余会话按 GET /tasks?session_id= 复核：能解析出任务即子 Agent 会话
//      （taskForSession 覆盖子会话），同样排除。
// 普通对话在出口侧只记录、不受任务策略约束（native/POLICY.md §全工具出口）。

type ChatsFilter = 'active' | 'archived'

interface ChatRow {
  id: string
  title: string
  updatedAt: Date | null
  closed: boolean
  archived: boolean
  busy: boolean
}

const KINDS_BATCH = 500
const SUBAGENT_CHECK_CONCURRENCY = 6

function recordIDs(records: SessionRecord[]): string[] {
  return [...new Set(records.map((record) => record.session?.id || '').filter(Boolean))]
}

// 有限并发：子 Agent 归属复核是逐会话只读请求，避免一次打出上百个并发。
async function mapLimit<T, R>(items: T[], limit: number, fn: (item: T) => Promise<R>): Promise<R[]> {
  const results = new Array<R>(items.length)
  let next = 0
  const worker = async () => {
    while (next < items.length) {
      const index = next++
      results[index] = await fn(items[index])
    }
  }
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, worker))
  return results
}

async function loadSessionKinds(ids: string[]): Promise<Record<string, WebApiSessionKindItem>> {
  const kinds: Record<string, WebApiSessionKindItem> = {}
  for (let offset = 0; offset < ids.length; offset += KINDS_BATCH) {
    const chunk = ids.slice(offset, offset + KINDS_BATCH)
    if (chunk.length === 0) continue
    Object.assign(kinds, await getWebApiSessionKinds(chunk))
  }
  return kinds
}

// 能解析出任务 = 任务/验证/复测/子 Agent 会话（前三者已由 session-kinds 排除）。
// 404 才是「不属于任何任务」；其它错误（网络/服务端）不把会话判成业务会话，
// 下一次刷新自愈（与侧栏徽标缓存同一取舍）。
async function belongsToTask(sessionID: string): Promise<boolean> {
  try {
    await getWebApiTaskBySession(sessionID)
    return true
  } catch (err) {
    return !(err instanceof APIError) || err.status !== 404
  }
}

function toRow(record: SessionRecord): ChatRow {
  const updated = record.updatedAt ? timestampDate(record.updatedAt) : null
  return {
    id: record.session?.id || '',
    title: record.session?.title?.trim() || '',
    updatedAt: updated && !Number.isNaN(updated.getTime()) ? updated : null,
    closed: record.session?.state === 'closed',
    archived: record.archived === true,
    busy: record.session?.state === 'busy',
  }
}

interface Props {
  agents: AgentView[]
  onOpenChat: (sessionID: string) => void
  onNewChat: (nodeID: string) => void
  onOpenMenu: () => void
}

export default function ChatsPage({ agents, onOpenChat, onNewChat, onOpenMenu }: Props) {
  const { t } = useTranslation('chat')
  const confirm = useConfirm()
  const [rows, setRows] = useState<ChatRow[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [filter, setFilter] = useState<ChatsFilter>('active')
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [editingID, setEditingID] = useState<string | null>(null)
  const [titleDraft, setTitleDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [deletingID, setDeletingID] = useState<string | null>(null)
  const seq = useRef(0)

  useEffect(() => {
    const timer = window.setTimeout(() => setQuery(searchInput.trim()), 250)
    return () => window.clearTimeout(timer)
  }, [searchInput])

  const load = useCallback(async (silent = false) => {
    const current = ++seq.current
    if (silent) setRefreshing(true)
    else setLoading(true)
    setError('')
    try {
      // 原生 ListSessions 的 archived 是精确匹配：两次取「未归档」与「归档」。
      const [activeRecords, archivedRecords] = await Promise.all([
        listChatSessions({ archived: false }),
        listChatSessions({ archived: true }),
      ])
      if (current !== seq.current) return
      const archivedIDs = new Set(archivedRecords.map((record) => record.session?.id || '').filter(Boolean))
      const records = [...activeRecords, ...archivedRecords]
      const ids = recordIDs(records)
      let kinds: Record<string, WebApiSessionKindItem>
      try {
        kinds = await loadSessionKinds(ids)
      } catch (err) {
        if (current !== seq.current) return
        // 无法读取归属时不猜：整个列表不显示（避免把任务会话误当普通对话）。
        setRows([])
        setError(`${t('shell.chatsKindsFailed')}：${err instanceof Error ? err.message : String(err)}`)
        return
      }
      if (current !== seq.current) return
      const candidates = records.filter((record) => {
        const id = record.session?.id
        return !!id && !kinds[id]
      })
      const belongs = await mapLimit(candidates, SUBAGENT_CHECK_CONCURRENCY, (record) => belongsToTask(record.session!.id))
      if (current !== seq.current) return
      const next = candidates
        .filter((_, index) => !belongs[index])
        .map((record) => {
          const row = toRow(record)
          // 归档列表单独返回时记录上不一定回填 archived，按来源列表兜底。
          if (archivedIDs.has(row.id)) row.archived = true
          return row
        })
      next.sort((a, b) => (b.updatedAt?.getTime() || 0) - (a.updatedAt?.getTime() || 0))
      setRows(next)
    } catch (err) {
      if (current !== seq.current) return
      setRows([])
      setError(`${t('shell.chatsLoadFailed')}：${err instanceof Error ? err.message : String(err)}`)
    } finally {
      if (current === seq.current) {
        setLoading(false)
        setRefreshing(false)
      }
    }
  }, [t])

  useEffect(() => {
    void load()
    return () => { seq.current++ }
  }, [load])

  const activeCount = rows.filter((row) => !row.archived).length
  const archivedCount = rows.length - activeCount
  const visible = rows
    .filter((row) => (filter === 'archived' ? row.archived : !row.archived))
    .filter((row) => !query || row.title.toLowerCase().includes(query.toLowerCase()))

  const newChatNodeID = agents.find((agent) => agent.hello?.nodeId)?.hello?.nodeId || ''
  const startNewChat = () => {
    if (!newChatNodeID) return
    onNewChat(newChatNodeID)
  }

  const startRename = (row: ChatRow) => {
    setEditingID(row.id)
    setTitleDraft(row.title || t('shell.chatsUntitled'))
  }

  const saveTitle = async (row: ChatRow) => {
    if (saving) return
    const title = titleDraft.trim()
    if (!title) return
    setSaving(true)
    try {
      await updateChatSession(row.id, { title })
      setRows((prev) => prev.map((item) => (item.id === row.id ? { ...item, title } : item)))
      setEditingID(null)
    } catch (err) {
      setError(`${t('shell.chatsRenameFailed')}：${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setSaving(false)
    }
  }

  const remove = async (row: ChatRow) => {
    if (deletingID) return
    const ok = await confirm({
      title: t('shell.chatsDeleteTitle'),
      description: t('shell.chatsDeleteConfirm'),
      confirmLabel: t('shell.chatsDelete'),
      destructive: true,
    })
    if (!ok) return
    setDeletingID(row.id)
    try {
      await deleteChatSession(row.id)
      setRows((prev) => prev.filter((item) => item.id !== row.id))
    } catch (err) {
      setError(`${t('shell.chatsDeleteFailed')}：${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setDeletingID(null)
    }
  }

  const filterItems = [
    { key: 'active' as const, label: t('shell.chatsFilterActive'), count: activeCount, dot: 'bg-success' },
    { key: 'archived' as const, label: t('shell.chatsFilterArchived'), count: archivedCount, dot: undefined },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
      <Page>
        <PageHeader
          title={t('shell.navChats')}
          onOpenMenu={onOpenMenu}
          menuLabel={t('shell.openMenu')}
          actions={(
            <>
              <Button type="button" variant="outline" size="sm" className="h-9" onClick={() => void load(true)} disabled={loading || refreshing}>
                <RefreshCw className={cn('mr-1.5 h-3.5 w-3.5', refreshing && 'animate-spin')} />
                {t('shell.refresh')}
              </Button>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span tabIndex={newChatNodeID ? -1 : 0}>
                    <Button type="button" size="sm" className="h-9 gap-1.5" onClick={startNewChat} disabled={!newChatNodeID}>
                      <Plus className="h-4 w-4" />
                      {t('shell.chatsNew')}
                    </Button>
                  </span>
                </TooltipTrigger>
                {!newChatNodeID && <TooltipContent className="max-w-[260px] whitespace-normal text-left">{t('shell.chatsNoAgent')}</TooltipContent>}
              </Tooltip>
            </>
          )}
        >
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                type="button"
                aria-label={t('shell.chatsHelp')}
                className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
              >
                <HelpCircle className="h-3.5 w-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-[300px] whitespace-normal text-left leading-relaxed">
              {t('shell.chatsHelp')}
            </TooltipContent>
          </Tooltip>
        </PageHeader>

        <div className="flex flex-wrap items-center gap-3">
          <FilterPills items={filterItems} value={filter} onChange={setFilter} />
          <Input
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            placeholder={t('shell.chatsSearch')}
            aria-label={t('shell.chatsSearch')}
            className="ml-auto h-9 w-[240px] bg-card"
          />
        </div>

        {error && <p role="status" className="text-[12px] text-warning">{error}</p>}

        <TableCard
          footer={(
            <span className="text-[12px] text-muted-foreground">
              {t('shell.chatsShown', { count: visible.length })}
            </span>
          )}
        >
          {loading ? (
            <div className="flex h-40 items-center justify-center gap-2 text-[13px] text-muted-foreground">
              <Spinner className="h-5 w-5" />
              {t('shell.loading')}
            </div>
          ) : visible.length === 0 ? (
            <EmptyState
              title={query || filter === 'archived' ? t('shell.chatsEmptyFiltered') : t('shell.chatsEmpty')}
              action={(
                <Button type="button" size="sm" className="gap-1.5" onClick={startNewChat} disabled={!newChatNodeID}>
                  <Plus className="h-4 w-4" />
                  {t('shell.chatsNew')}
                </Button>
              )}
            />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[560px] table-fixed border-collapse text-[13px]">
                <colgroup>
                  <col />
                  <col className="w-[152px]" />
                  <col className="w-[96px]" />
                  <col className="w-[92px]" />
                </colgroup>
                <thead>
                  <tr className="ws-table-head h-10 border-b border-border text-[12px] font-medium text-muted-foreground">
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.chatsColTitle')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.chatsColUpdated')}</th>
                    <th className="whitespace-nowrap px-3 text-left font-medium">{t('shell.chatsColStatus')}</th>
                    <th className="whitespace-nowrap px-3 text-right font-medium">{t('shell.chatsColActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map((row) => (
                    <tr
                      key={row.id}
                      data-session-id={row.id}
                      className="cursor-pointer border-b border-border/70 transition-colors hover:bg-muted/40"
                      onClick={() => { if (editingID !== row.id) onOpenChat(row.id) }}
                    >
                      <td className="min-w-0 px-3 py-2.5">
                        {editingID === row.id ? (
                          <form
                            className="flex min-w-0 items-center gap-1"
                            onClick={(event) => event.stopPropagation()}
                            onSubmit={(event) => { event.preventDefault(); void saveTitle(row) }}
                          >
                            <input
                              autoFocus
                              aria-label={t('shell.chatsRename')}
                              maxLength={200}
                              value={titleDraft}
                              onChange={(event) => setTitleDraft(event.target.value)}
                              className="min-w-0 flex-1 rounded border border-border bg-background px-2 py-1 text-[13px]"
                            />
                            <button type="submit" disabled={saving || !titleDraft.trim()} aria-label={t('shell.chatsSave')} className="p-1 text-muted-foreground hover:text-foreground disabled:opacity-50">
                              <Check className="h-3.5 w-3.5" />
                            </button>
                            <button type="button" onClick={() => setEditingID(null)} aria-label={t('shell.chatsCancel')} className="p-1 text-muted-foreground hover:text-foreground">
                              <X className="h-3.5 w-3.5" />
                            </button>
                          </form>
                        ) : (
                          <div className="flex min-w-0 items-center gap-2">
                            <MessageSquare className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                            <span className="min-w-0 truncate text-[13px] font-medium text-foreground" title={row.title}>
                              {row.title || t('shell.chatsUntitled')}
                            </span>
                            {row.archived && (
                              <span className="shrink-0 rounded border border-border px-1 py-0.5 text-[10px] text-muted-foreground">{t('shell.chatsArchived')}</span>
                            )}
                          </div>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[12px] tabular text-muted-foreground">
                        {row.updatedAt ? formatDateTime(row.updatedAt.toISOString()) : '—'}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2.5">
                        <span className="inline-flex items-center gap-1.5 text-[12px] text-secondary-foreground">
                          <span aria-hidden className={cn('h-2 w-2 shrink-0 rounded-full', row.closed ? 'bg-muted-foreground/50' : row.busy ? 'bg-warning' : 'bg-success')} />
                          {row.closed ? t('shell.chatsStatusClosed') : row.busy ? t('shell.chatsStatusBusy') : t('shell.chatsStatusOpen')}
                        </span>
                      </td>
                      <td className="px-3 py-2.5">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            type="button"
                            variant="ghost"
                            size="icon-xs"
                            aria-label={t('shell.chatsOpen')}
                            title={t('shell.chatsOpen')}
                            onClick={(event) => { event.stopPropagation(); onOpenChat(row.id) }}
                            className="text-muted-foreground"
                          >
                            <ArrowRight className="h-3.5 w-3.5" />
                          </Button>
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-xs"
                                aria-label={t('shell.chatsColActions')}
                                title={t('shell.chatsColActions')}
                                disabled={deletingID === row.id}
                                onClick={(event) => event.stopPropagation()}
                                className="text-muted-foreground"
                              >
                                <MoreHorizontal className="h-3.5 w-3.5" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" onClick={(event) => event.stopPropagation()}>
                              <DropdownMenuItem onSelect={() => startRename(row)}>
                                <Pencil className="mr-2 h-3.5 w-3.5" />
                                {t('shell.chatsRename')}
                              </DropdownMenuItem>
                              <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => void remove(row)}>
                                <Trash2 className="mr-2 h-3.5 w-3.5" />
                                {t('shell.chatsDelete')}
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </TableCard>
      </Page>
    </div>
  )
}