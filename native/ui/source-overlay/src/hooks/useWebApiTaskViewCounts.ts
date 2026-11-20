import { useCallback, useEffect, useRef, useState } from 'react'
import { getWebApiBoard, listWebApiEvidence, listWebApiFindings, listWebApiReports } from '../api'
import { usePolling } from './usePolling'

/** 计数形态：数字或 “N+”（接口无 total，首页满页 + has_more）。null = 取不到不显示。 */
export type WebApiViewCount = string | null

export interface WebApiTaskViewCounts {
  traffic: WebApiViewCount
  /** 黑板：待走 + 进行中的 Oracle 数；为 0 时不显示。 */
  board: WebApiViewCount
  findings: WebApiViewCount
  reports: WebApiViewCount
}

/**
 * 任务视图计数（原 WebApiTaskNav 的逻辑抽出复用）：流量 / 候选 / 报告各取列表第一页，
 * has_more 用 “N+” 表示；每类独立落地，失败只留空，不显示假 0。
 * countsRevision 由新建验证/报告成功后 bump，触发静默重取。
 */
export function useWebApiTaskViewCounts(taskId: string, countsRevision = 0): WebApiTaskViewCounts {
  const [counts, setCounts] = useState<WebApiTaskViewCounts>({ traffic: null, board: null, findings: null, reports: null })
  const seq = useRef(0)

  const load = useCallback(async () => {
    const current = ++seq.current
    const [evidence, board, findings, reports] = await Promise.all([
      listWebApiEvidence(taskId, 100).catch(() => null),
      getWebApiBoard(taskId).catch(() => null),
      listWebApiFindings(taskId, 25).catch(() => null),
      listWebApiReports(taskId, 25).catch(() => null),
    ])
    if (current !== seq.current) return
    const pending = board ? (board.counts.oracles.open || 0) + (board.counts.oracles.in_progress || 0) : 0
    setCounts({
      traffic: evidence ? `${evidence.items.length}${evidence.has_more ? '+' : ''}` : null,
      board: board ? (pending > 0 ? String(pending) : null) : null,
      findings: findings ? `${findings.items.length}${findings.has_more ? '+' : ''}` : null,
      reports: reports ? `${reports.items.length}${reports.has_more ? '+' : ''}` : null,
    })
  }, [taskId])

  useEffect(() => {
    setCounts({ traffic: null, board: null, findings: null, reports: null })
    void load()
    return () => { seq.current++ }
  }, [load])

  useEffect(() => { void load() }, [countsRevision, load])

  // 模型在研究会话里也可能提出/推进 Oracle；15 秒静默刷新让页签计数跟随黑板。
  usePolling(() => { void load() }, 15000)

  return counts
}