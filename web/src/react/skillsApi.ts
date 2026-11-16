import { idPath, requestApi } from './api.ts'
import type { SkillActivityRow, SkillUsageRow } from '../ui/SkillRecords.tsx'
import type { MaterialReference } from './libraryApi.ts'
import type { ModelCall } from './modelUsage.ts'
export interface SkillPage<T> { mission_id: string; items: T[]; next_cursor: string }
export interface SkillUsagePage extends SkillPage<SkillUsageRow> { total_successful_loads: string }
export interface SkillCallsPage extends SkillPage<ModelCall> { load_id: string }
export interface SkillLoad extends MaterialReference { load_id: string; mission_id: string; thread_id: string; attempt_id: string; purpose: string; created_at: string; content: string }
const base = (mission: string) => `/missions/${idPath(mission)}`
const query = (after: string) => new URLSearchParams({ after, limit: '25' })
function checkMission<T extends { mission_id: string }>(value: T, mission: string): T {
  if (value.mission_id !== mission) throw new Error('Skill记录不属于当前项目。')
  return value
}
export async function fetchSkillUsage(mission: string, after: string, signal: AbortSignal) {
  return checkMission(await requestApi<SkillUsagePage>(`${base(mission)}/skill-usage?${query(after)}`, { signal }), mission)
}
export async function fetchSkillActivities(mission: string, after: string, signal: AbortSignal) {
  const value = checkMission(await requestApi<SkillPage<SkillActivityRow>>(`${base(mission)}/skill-activities?${query(after)}`, { signal }), mission)
  if (value.items.some(row => row.mission_id !== mission)) throw new Error('活动不属于当前项目。')
  return value
}
export async function fetchSkillLoad(mission: string, loadID: string, signal: AbortSignal) {
  const value = checkMission(await requestApi<SkillLoad>(`${base(mission)}/skill-loads/${idPath(loadID)}`, { signal }), mission)
  if (value.load_id !== loadID) throw new Error('固定原件不属于当前加载。')
  return value
}
export async function fetchSkillCalls(mission: string, loadID: string, after: string, signal: AbortSignal) {
  const value = checkMission(await requestApi<SkillCallsPage>(`${base(mission)}/skill-loads/${idPath(loadID)}/calls?${query(after)}`, { signal }), mission)
  if (value.load_id !== loadID || value.items.some(call => call.mission_id !== mission)) throw new Error('调用不属于当前项目或加载。')
  return value
}
