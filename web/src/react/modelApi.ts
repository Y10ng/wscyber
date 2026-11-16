import { requestApi } from './api.ts'
import { requireTestIdentity, validModelID, type ModelConfig, type ModelTest, type ModelSettings, type MissionModel } from './models.ts'

export const fetchModelSettings = (signal?: AbortSignal) => requestApi<ModelSettings>('/model-settings', { signal, safeErrors: true })
export const putModelSettings = (settings: ModelSettings, defaultID: string, signal?: AbortSignal) => {
  if (defaultID && !validModelID(defaultID)) throw new Error('无效的配置身份')
  return requestApi<ModelSettings>('/model-settings', { method: 'PUT', body: { expected_revision: settings.revision, default_config_id: defaultID }, signal, safeErrors: true })
}
export async function fetchMissionModel(missionID: string, signal?: AbortSignal) {
  const value = await requestApi<MissionModel>(`/missions/${encodeURIComponent(missionID)}/model`, { signal, safeErrors: true })
  if (value.mission_id !== missionID || !['managed', 'environment'].includes(value.source)) throw new Error('任务模型归属不匹配')
  return value
}

function configPath(id: string) {
  if (!validModelID(id)) throw new Error('无效的配置身份')
  return `/model-configs/${id}`
}
function testPath(configID: string, testID: string) {
  if (!validModelID(testID)) throw new Error('无效的测试身份')
  return `${configPath(configID)}/tests/${testID}`
}
export const fetchModelConfigs = (signal?: AbortSignal) => requestApi<ModelConfig[]>('/model-configs', { signal, safeErrors: true })
export const fetchModelConfig = (id: string, signal?: AbortSignal) => requestApi<ModelConfig>(configPath(id), { signal, safeErrors: true })
export const patchModelConfig = (id: string, rawBody: string, signal?: AbortSignal) => requestApi<ModelConfig>(configPath(id), { method: 'PATCH', rawBody, signal, safeErrors: true })
export async function fetchModelTest(configID: string, testID: string, signal?: AbortSignal) {
  return requireTestIdentity(await requestApi<ModelTest>(testPath(configID, testID), { signal, safeErrors: true }), configID, testID)
}
/** 只能由明确确认的事件处理器调用，挂载、刷新、恢复路径不调用此方法。 */
export async function postModelTest(configID: string, testID: string, revision: number, signal?: AbortSignal) {
  if (!Number.isSafeInteger(revision) || revision < 1) throw new Error('无效的配置版本')
  return requireTestIdentity(await requestApi<ModelTest>(testPath(configID, testID), {
    method: 'POST', body: { config_revision: revision, confirm_billable: true }, signal, safeErrors: true,
  }), configID, testID)
}
