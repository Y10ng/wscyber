import { idPath, requestApi } from './api.ts'
import type { MaterialDetail, MaterialDraft, MaterialItem } from '../ui/materials.ts'
import { isResearchBrief, type ResearchBrief } from '../ui/researchBrief.ts'
export interface LibraryWrite extends MaterialDraft { expected_token?: string }
export interface LibraryMutation { key: string; item_id: string; deleted: boolean; item?: MaterialItem; committed_at: string }
export interface LibraryPage { items: MaterialItem[]; next_cursor: string }
export interface MaterialReference { id: string; source_token: string; name: string; path: string; description: string; format: 'markdown' | 'text'; content_sha256: string; byte_size: number }
export interface ResearchConfig { mission_id: string; materials: MaterialReference[]; safety_prompt_enabled: boolean; safety_prompt: string; research_brief?: ResearchBrief }
export const fetchLibraryFolders = (signal?: AbortSignal) => requestApi<{ items: MaterialItem[] }>('/library/folders', { signal })
export const fetchLibraryItems = (options: { parentID?: string; all?: boolean; kind?: 'file' | 'folder'; q?: string; after?: string }, signal?: AbortSignal) => {
  const query = new URLSearchParams({ limit: '25' })
  if (options.all) query.set('all', 'true'); else query.set('parent_id', options.parentID || '')
  if (options.kind) query.set('kind', options.kind)
  if (options.q) query.set('q', options.q)
  if (options.after) query.set('after', options.after)
  return requestApi<LibraryPage>(`/library/items?${query}`, { signal })
}
export const fetchLibraryItem = (id: string, signal?: AbortSignal) => requestApi<MaterialDetail>(`/library/items/${idPath(id)}`, { signal })
export const putLibraryItem = (id: string, key: string, body: LibraryWrite) => requestApi<LibraryMutation>(`/library/items/${idPath(id)}`, { method: 'PUT', headers: { 'Idempotency-Key': key }, body })
export const deleteLibraryItem = (id: string, key: string, expected_token: string) => requestApi<LibraryMutation>(`/library/items/${idPath(id)}`, { method: 'DELETE', headers: { 'Idempotency-Key': key }, body: { expected_token } })
export const fetchLibraryReceipt = (key: string) => requestApi<LibraryMutation>(`/library/operations/${idPath(key)}`)
export const fetchResearchConfig = async (missionID: string, signal?: AbortSignal): Promise<ResearchConfig> => {
  const value = await requestApi<ResearchConfig>(`/missions/${idPath(missionID)}/research-config`, { signal })
  if (!value || value.mission_id !== missionID || ('research_brief' in value && !isResearchBrief(value.research_brief))) throw new Error('任务研究配置归属或身份说明格式无效。')
  return value
}
export const fetchMissionMaterial = (missionID: string, itemID: string, signal?: AbortSignal) => requestApi<MaterialReference & { content: string }>(`/missions/${idPath(missionID)}/materials/${idPath(itemID)}`, { signal })
