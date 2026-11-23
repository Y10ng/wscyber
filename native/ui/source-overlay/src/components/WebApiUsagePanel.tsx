import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { Button } from '@cyber/ui'
import {
  getWebApiFinding,
  getWebApiReport,
  getWebApiReportBySession,
  getWebApiRetestBySession,
  getWebApiUsage,
  getWebApiVerificationBySession,
  listWebApiSubsessions,
  type WebApiTask,
  type WebApiUsageReport,
} from '../api'
import {
  UsageCompactTable,
  UsageRoleBadge,
  UsageTotalsRow,
  compactRowFrom,
  formatUsageTokens,
} from './WebApiUsageShared'

interface Props {
  task: WebApiTask
  /** 行点击 → 跳到该会话执行视图（沿用「查看执行」跳转方式）。 */
  onViewExecution: (sessionID: string) => void
}

// 角色固定顺序：研究 → 子 Agent → 验证 → 报告 → 复测。
const ROLE_ORDER: Record<string, number> = { research: 0, subagent: 1, verification: 2, report: 3, retest: 4 }

// 任务「用量」视图：合计一行 + 按会话表 +（按模型 / 按日期 并排小表）。
// 只读，进入视图与手动刷新时加载；不轮询。不显示费用（无价格依据）。
export default function WebApiUsagePanel({ task, onViewExecution }: Props) {
  const { t } = useTranslation('chat')
  const [report, setReport] = useState<WebApiUsageReport | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [links, setLinks] = useState<Record<string, string>>({})
  // 按会话表的时间序：接口只给会话/角色/用量，这里只读反查各会话创建时间。
  const [sessionTimes, setSessionTimes] = useState<Record<string, string>>({})
  const seq = useRef(0)
  const linkSeq = useRef(0)
  const timeSeq = useRef(0)

  const load = useCallback(async () => {
    const current = ++seq.current
    setLoading(true)
    setError('')
    try {
      const result = await getWebApiUsage({ taskId: task.id })
      if (current !== seq.current) return
      setReport(result)
    } catch (err) {
      if (current !== seq.current) return
      setReport(null)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (current === seq.current) setLoading(false)
    }
  }, [task.id])

  // 切任务即清旧数据并重载；卸载使迟到响应失效。
  useEffect(() => {
    seq.current++
    linkSeq.current++
    setReport(null)
    setError('')
    setLinks({})
    setSessionTimes({})
    void load()
    return () => { seq.current++; linkSeq.current++; timeSeq.current++ }
  }, [load])

  // 「关联」列：验证/复测行显示候选编号（WSC-xxxx），报告行显示报告草稿标题；
  // 接口只回 finding_id/report_id，这里按 ID 只读反查一次并缓存，失败显示 —。
  useEffect(() => {
    if (!report?.by_session) return
    const wanted = new Map<string, 'finding' | 'report'>()
    for (const session of report.by_session) {
      if (session.role === 'report' && session.report_id) wanted.set(session.report_id, 'report')
      else if ((session.role === 'verification' || session.role === 'retest') && session.finding_id) {
        wanted.set(session.finding_id, 'finding')
      }
    }
    if (wanted.size === 0) return
    const current = ++linkSeq.current
    void (async () => {
      const entries = await Promise.all([...wanted].map(async ([id, kind]) => {
        try {
          if (kind === 'finding') {
            const finding = await getWebApiFinding(task.id, id)
            return [id, finding.number] as const
          }
          const detail = await getWebApiReport(task.id, id)
          return [id, detail.draft?.payload?.title?.trim() || ''] as const
        } catch {
          return [id, ''] as const
        }
      }))
      if (current !== linkSeq.current) return
      setLinks(Object.fromEntries(entries))
    })()
  }, [report, task.id])

  // 同角色按时间排序所需：研究会话用任务创建时间，子 Agent 用 /subsessions.created_at，
  // 验证/复测/报告按会话反查。失败或缺字段时不写入，排序回退到接口顺序。
  useEffect(() => {
    if (!report?.by_session) { setSessionTimes({}); return }
    const current = ++timeSeq.current
    const times: Record<string, string> = {}
    const list = report.by_session
    for (const session of list) {
      if (session.role === 'research' && session.session_id === task.initial_session_id) times[session.session_id] = task.created_at
    }
    void (async () => {
      const direct = await Promise.all(list.map(async (session) => {
        try {
          if (session.role === 'verification') return [session.session_id, (await getWebApiVerificationBySession(session.session_id)).created_at] as const
          if (session.role === 'retest') return [session.session_id, (await getWebApiRetestBySession(session.session_id)).created_at] as const
          if (session.role === 'report') return [session.session_id, (await getWebApiReportBySession(session.session_id)).created_at] as const
        } catch { /* 反查失败不阻塞排序 */ }
        return null
      }))
      for (const entry of direct) if (entry) times[entry[0]] = entry[1]
      if (list.some((session) => session.role === 'subagent')) {
        try {
          const subs = await listWebApiSubsessions(task.id)
          for (const sub of subs.items) times[sub.session_id] = sub.created_at
        } catch { /* 同上 */ }
      }
      if (current !== timeSeq.current) return
      setSessionTimes(times)
    })()
  }, [report, task.id, task.initial_session_id, task.created_at])

  // 角色固定顺序：研究 → 子 Agent → 验证 → 报告 → 复测；同角色按创建时间升序。
  const sessions = useMemo(() => {
    const list = report?.by_session ?? []
    return [...list].sort((a, b) => {
      const orderA = ROLE_ORDER[a.role] ?? 99
      const orderB = ROLE_ORDER[b.role] ?? 99
      if (orderA !== orderB) return orderA - orderB
      const timeA = sessionTimes[a.session_id]
      const timeB = sessionTimes[b.session_id]
      if (timeA && timeB && timeA !== timeB) return timeA < timeB ? -1 : 1
      if (timeA && !timeB) return -1
      if (!timeA && timeB) return 1
      return 0
    })
  }, [report, sessionTimes])
  const models = report?.by_model ?? []
  const days = report?.by_day ?? []
  const empty = !!report && report.totals.calls === 0 && sessions.length === 0 && models.length === 0 && days.length === 0

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
      {report && (
        <UsageTotalsRow
          totals={report.totals}
          skippedEvents={report.skipped_events}
          onRefresh={() => void load()}
          refreshing={loading}
        />
      )}
      <div className="min-h-0 flex-1 overflow-y-auto">
        {loading && !report ? (
          <div className="flex h-full items-center justify-center text-muted-foreground"><Loader2 className="h-5 w-5 animate-spin" /></div>
        ) : error ? (
          <div className="flex flex-col items-center gap-2 px-3 py-6 text-center">
            <span className="text-sm text-destructive">{error}</span>
            <Button type="button" variant="outline" size="xs" onClick={() => void load()}>{t('webapi.usageRetry')}</Button>
          </div>
        ) : empty ? (
          <div className="px-3 py-6 text-center text-sm text-muted-foreground">{t('webapi.usageEmpty')}</div>
        ) : report ? (
          <div className="space-y-5 p-3">
            <section aria-label={t('webapi.usageBySession')} className="space-y-1.5">
              <h2 className="text-[13px] font-medium text-foreground">{t('webapi.usageBySession')}</h2>
              <table className="w-full border-separate border-spacing-0 text-left">
                <thead>
                  <tr>
                    <th scope="col" className="border-b border-border/60 bg-background px-2 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.usageRole')}</th>
                    <th scope="col" className="border-b border-border/60 bg-background px-2 py-1 text-[12px] font-medium text-muted-foreground">{t('webapi.usageRelated')}</th>
                    <th scope="col" className="w-[90px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageInput')}</th>
                    <th scope="col" className="w-[90px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageOutput')}</th>
                    <th scope="col" className="w-[90px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageCacheRead')}</th>
                    <th scope="col" className="w-[100px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageTotal')}</th>
                    <th scope="col" className="w-[70px] border-b border-border/60 bg-background px-2 py-1 text-right text-[12px] font-medium text-muted-foreground">{t('webapi.usageCalls')}</th>
                  </tr>
                </thead>
                <tbody>
                  {sessions.map((session) => {
                    const related = session.role === 'report'
                      ? (session.report_id ? (links[session.report_id] || '') : '')
                      : (session.finding_id ? (links[session.finding_id] || '') : '')
                    return (
                      <tr
                        key={session.session_id}
                        tabIndex={0}
                        title={t('webapi.verificationViewExecution')}
                        onClick={() => onViewExecution(session.session_id)}
                        onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onViewExecution(session.session_id) } }}
                        className="h-[30px] cursor-pointer transition-colors hover:bg-muted/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary/40"
                      >
                        <td className="border-b border-border/40 px-2">
                          <UsageRoleBadge role={session.role} />
                        </td>
                        <td className="max-w-0 truncate border-b border-border/40 px-2 font-mono text-[12px] text-foreground" title={related || undefined}>
                          {related || '—'}
                        </td>
                        <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-foreground">{formatUsageTokens(session.input_tokens)}</td>
                        <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-foreground">{formatUsageTokens(session.output_tokens)}</td>
                        <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-foreground">{formatUsageTokens(session.cache_read_tokens)}</td>
                        <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-foreground">{formatUsageTokens(session.total_tokens)}</td>
                        <td className="border-b border-border/40 px-2 text-right font-mono text-[12px] tabular-nums text-muted-foreground">{formatUsageTokens(session.calls)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </section>
            <div className="grid grid-cols-1 gap-4 min-[900px]:grid-cols-2">
              <section aria-label={t('webapi.usageByModel')} className="space-y-1.5">
                <h2 className="text-[13px] font-medium text-foreground">{t('webapi.usageByModel')}</h2>
                <UsageCompactTable
                  nameKey="webapi.usageModel"
                  rows={models.map((model) => compactRowFrom(model.model, model.model, model))}
                />
              </section>
              <section aria-label={t('webapi.usageByDate')} className="space-y-1.5">
                <h2 className="text-[13px] font-medium text-foreground">{t('webapi.usageByDate')}</h2>
                <UsageCompactTable
                  nameKey="webapi.usageDay"
                  rows={days.map((day) => compactRowFrom(day.day, day.day, day))}
                />
              </section>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  )
}
