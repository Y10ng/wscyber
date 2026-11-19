import { useCallback, useEffect, useRef, useState } from 'react'
import { listWebApiEvidence, listWebApiFindings, listWebApiReports } from '../api'

/** 计数形态：数字或 “N+”（接口无 total，首页满页 + has_more）。null = 取不到不显示。 */
export type WebApiViewCount = string | null

export interface WebApiTaskViewCounts {
  traffic: WebApiViewCount
  findings: WebApiViewCount
  reports: WebApiViewCount
}

/**
 * 任务视图计数（原 WebApiTaskNav 的逻辑抽出复用）：流量 / 候选 / 报告各取列表第一页，
 * has_more 用 “N+” 表示；每类独立落地，失败只留空，不显示假 0。
 * countsRevision 由新建验证/报告成功后 bump，触发静默重取。
 */
export function useWebApiTaskViewCounts(taskId: string, countsRevision = 0): WebApiTaskViewCounts {
  const [counts, setCounts] = useState<WebApiTaskViewCounts>({ traffic: null, findings: null, reports: null })
  const seq = useRef(0)

  const load = useCallback(async () => {
    const current = ++seq.current
    const [evidence, findings, reports] = await Promise.all([
      listWebApiEvidence(taskId, 100).catch(() => null),
      listWebApiFindings(taskId, 25).catch(() => null),
      listWebApiReports(taskId, 25).catch(() => null),
    ])
    if (current !== seq.current) return
    setCounts({
      traffic: evidence ? `${evidence.items.length}${evidence.has_more ? '+' : ''}` : null,
      findings: findings ? `${findings.items.length}${findings.has_more ? '+' : ''}` : null,
      reports: reports ? `${reports.items.length}${reports.has_more ? '+' : ''}` : null,
    })
  }, [taskId])

  useEffect(() => {
    setCounts({ traffic: null, findings: null, reports: null })
    void load()
    return () => { seq.current++ }
  }, [load])

  useEffect(() => { void load() }, [countsRevision, load])

  return counts
}