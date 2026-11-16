import { ApiError, idPath, requestApi } from './api.ts'
import type { ModelCall, ModelCallContent, ModelCallPage, ModelPrice, PriceWrite, UsageResponse } from './modelUsage.ts'
const owned = (call: ModelCall, missionID?: string, callID?: string) => {
  if ((missionID && call.mission_id !== missionID) || (callID && call.id !== callID)) throw new ApiError('调用记录归属不匹配，已拒绝显示。', 404)
  return call
}
export async function fetchModelCalls(query: string, missionID?: string, signal?: AbortSignal) {
  const page = await requestApi<ModelCallPage>(`/model-calls?${query}`, { signal, safeErrors: true })
  if (!Array.isArray(page.items) || new Set(page.items.map(c => c.id)).size !== page.items.length) throw new ApiError('调用列表格式无效。', 0)
  page.items.forEach(c => owned(c, missionID))
  return page
}
const missionQuery = (missionID?: string) => missionID ? `?mission_id=${idPath(missionID)}` : ''
export async function fetchModelCall(id: string, missionID?: string, signal?: AbortSignal) {
  return owned(await requestApi<ModelCall>(`/model-calls/${idPath(id)}${missionQuery(missionID)}`, { signal, safeErrors: true }), missionID, id)
}
export async function fetchModelCallContent(id: string, missionID?: string, signal?: AbortSignal) {
  const content = await requestApi<ModelCallContent>(`/model-calls/${idPath(id)}/content${missionQuery(missionID)}`, { signal, safeErrors: true })
  if (content.call_id !== id) throw new ApiError('原文归属不匹配，已拒绝显示。', 404)
  return content
}
export const fetchModelUsage = (query: string, missionID?: string, signal?: AbortSignal) => requestApi<UsageResponse>(`${missionID ? `/missions/${idPath(missionID)}/model-usage` : '/model-usage'}?${query}`, { signal, safeErrors: true })
export async function fetchModelRecording(missionID: string, signal?: AbortSignal) {
  const value = await requestApi<{ mission_id: string; record_model_content: boolean }>(`/missions/${idPath(missionID)}/model-recording`, { signal, safeErrors: true })
  if (value.mission_id !== missionID) throw new ApiError('记录设置不属于当前项目。', 404)
  return value.record_model_content
}
export const fetchModelPrices = (signal?: AbortSignal) => requestApi<{ items: ModelPrice[] }>('/model-prices', { signal, safeErrors: true })
export const putModelPrice = (id: string, body: PriceWrite, signal?: AbortSignal) => requestApi<ModelPrice>(`/model-prices/${idPath(id)}`, { method: 'PUT', body, signal, safeErrors: true })
