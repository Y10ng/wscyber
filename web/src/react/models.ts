import { generateOperationKey } from './idempotency.ts'
import { encodeSecretEntries, validateSecretEntries, type SecretEntry, type SecretEntryErrors } from '../ui/secretEntries.ts'
import type { SystemInfo } from './types.ts'

export interface ModelSettings { revision: number; default_config_id: string }
export interface MissionModel { mission_id: string; source: 'managed' | 'environment'; config_id?: string; name: string; model: string; protocol: string; enabled: boolean }
export function canStartExploration(system: SystemInfo | null, selectedID?: string, configs?: ModelConfig[] | null): boolean {
  return selectedID ? Boolean(system?.model_management_available && configs?.some(c => c.id === selectedID && c.enabled)) : Boolean(system?.model_configured)
}

export interface ModelConnection {
  name: string
  protocol: 'openai_chat' | 'anthropic_messages'
  base_url: string
  model: string
  auth: 'none' | 'bearer' | 'api_key'
  timeout_seconds: number
  max_output_tokens: number
}
export interface ModelConfig extends ModelConnection {
  id: string
  revision: number
  enabled: boolean
  api_key_set: boolean
  header_names: string[]
  parameter_keys: string[]
  created_at: string
}
export interface ModelDraft extends ModelConnection {
  id: string
  expected_revision: number
  enabled: boolean
  original: ModelConfig | null
  api_key: string
  headers: SecretEntry[]
  parameters: SecretEntry[]
}
export interface ModelTest {
  id: string
  config_id: string
  config_revision: number
  status: 'pending' | 'running' | 'completed'
  created_at: string
  started_at?: string
  completed_at?: string
  outcome?: 'not_sent' | 'transport_unknown' | 'http_error' | 'invalid_response' | 'valid_response'
  http_status?: number
  attempted?: boolean
}
export interface TestIdentity { configID: string; testID: string; receipt?: ModelTest }
/** 只保存本会话已知身份。401去掉回执，显式退出全部清空；不提供持久存储。 */
export class ModelTestSession {
  readonly known = new Map<string, TestIdentity>()
  defaultPending?: { targetID: string; expectedRevision: number; rejected?: boolean }
  defaultGeneration = 0
  defaultChecked?: { generation: number; settings: ModelSettings }
  beginDefault(settings: ModelSettings, targetID: string) {
    this.defaultPending = { targetID, expectedRevision: settings.revision }
    delete this.defaultChecked
    return ++this.defaultGeneration
  }
  acceptDefaultRead(settings: ModelSettings, generation: number) {
    if (generation !== this.defaultGeneration) return false
    this.defaultChecked = { generation, settings }
    return true
  }
  acceptDefaultWrite(generation: number) {
    if (generation !== this.defaultGeneration) return false
    delete this.defaultPending; delete this.defaultChecked
    return true
  }
  rejectDefaultWrite(generation: number) {
    if (generation !== this.defaultGeneration || !this.defaultPending) return false
    this.defaultPending.rejected = true
    return true
  }
  continueDefaultFromRead(): ModelSettings | null {
    if (!this.defaultPending || !this.defaultChecked || this.defaultChecked.generation !== this.defaultGeneration) return null
    const current = this.defaultChecked.settings
    this.defaultGeneration++
    delete this.defaultPending; delete this.defaultChecked
    return current
  }
  constructor(restored?: unknown) {
    if (!Array.isArray(restored)) return
    for (const item of restored) {
      if (item && typeof item.configID === 'string' && typeof item.testID === 'string' && validModelID(item.configID) && validModelID(item.testID)) this.remember(item.configID, item.testID)
    }
  }
  identities() {
    return [...this.known.values()].filter(value => value.receipt?.status !== 'completed').map(({ configID, testID }) => ({ configID, testID }))
  }
  remember(configID: string, testID: string): TestIdentity {
    const previous = this.known.get(testID)
    const identity = previous?.configID === configID ? previous : { configID, testID }
    this.known.set(testID, identity)
    return identity
  }
  forConfig(configID: string) {
    const entries = [...this.known.values()].filter(value => value.configID === configID)
    return entries.find(value => value.receipt?.status !== 'completed') ?? entries.at(-1)
  }
  accept(receipt: ModelTest) {
    const current = this.known.get(receipt.id)
    if (current?.configID === receipt.config_id) current.receipt = receipt
  }
  mask() { for (const identity of this.known.values()) delete identity.receipt; this.defaultGeneration++; delete this.defaultChecked }
  clear() { this.known.clear(); delete this.defaultPending; delete this.defaultChecked; this.defaultGeneration++ }
}
export const validModelID = (id: string) => /^[0-9a-f]{32}$/.test(id)
export const modelTestPath = (configID: string, testID: string) => {
  if (!validModelID(configID) || !validModelID(testID)) throw new Error('无效的测试身份')
  return `/settings/models/${configID}/tests/${testID}`
}
export function modelDraft(config?: ModelConfig): ModelDraft {
  return {
    id: config?.id ?? generateOperationKey(), expected_revision: config?.revision ?? 0,
    original: config ?? null, name: config?.name ?? '', protocol: config?.protocol ?? 'openai_chat',
    base_url: config?.base_url ?? '', model: config?.model ?? '', auth: config?.auth ?? 'bearer',
    timeout_seconds: config?.timeout_seconds ?? 30, max_output_tokens: config?.max_output_tokens ?? 1024,
    enabled: config?.enabled ?? true,
    api_key: '',
    headers: (config?.header_names ?? []).map((name, i) => ({ id: `header-${i}`, name })),
    parameters: (config?.parameter_keys ?? []).map((name, i) => ({ id: `parameter-${i}`, name })),
  }
}
export type DraftErrors = Partial<Record<keyof ModelDraft, string>>
export const modelRecipientChanged = (d: ModelDraft) => Boolean(d.original && (d.protocol !== d.original.protocol || d.base_url !== d.original.base_url || d.auth !== d.original.auth))
export function modelEntryErrors(d: ModelDraft, key: 'headers' | 'parameters'): SecretEntryErrors {
  return validateSecretEntries(d[key], { savedNames: key === 'headers' ? d.original?.header_names : d.original?.parameter_keys, caseInsensitive: key === 'headers', json: key === 'parameters', requireValues: modelRecipientChanged(d) })
}
export function validateModelDraft(d: ModelDraft): DraftErrors {
  const errors: DraftErrors = {}
  for (const key of ['name', 'base_url', 'model'] as const) if (!d[key].trim()) errors[key] = '此项不能为空'
  if (!Number.isInteger(d.timeout_seconds) || d.timeout_seconds < 1 || d.timeout_seconds > 600) errors.timeout_seconds = '请输入 1–600 的整数'
  if (!Number.isInteger(d.max_output_tokens) || d.max_output_tokens < 1 || d.max_output_tokens > 32768) errors.max_output_tokens = '请输入 1–32768 的整数'
  if (d.protocol === 'openai_chat' && d.auth === 'api_key') errors.auth = 'OpenAI Chat 不支持 API Key Header 认证'
  if (d.auth !== 'none' && ((!d.api_key && (!d.original?.api_key_set || modelRecipientChanged(d))) || (d.api_key !== '' && !d.api_key.trim()))) errors.api_key = modelRecipientChanged(d) ? '接收端或认证已变更，请重新输入 API Key' : '请输入 API Key'
  for (const key of ['headers', 'parameters'] as const) if (Object.keys(modelEntryErrors(d, key)).length) errors[key] = modelRecipientChanged(d) ? '接收端或认证已变更，请填写条目值或删除条目' : '请检查条目名称和值'
  return errors
}
/** JSON只做语法检查，发送原片段，重复键交给服务端严格解码，绝不静默折叠。 */
export function encodeModelDraft(d: ModelDraft): string {
  if (Object.keys(validateModelDraft(d)).length) throw new Error('请检查配置字段')
  const { name, protocol, base_url, model, auth, timeout_seconds, max_output_tokens, enabled, expected_revision } = d
  const connection = JSON.stringify({ name, protocol, base_url, model, auth, timeout_seconds, max_output_tokens, enabled, expected_revision, ...(auth !== 'none' && d.api_key ? { api_key: d.api_key } : {}) })
  return `${connection.slice(0, -1)},"headers":${encodeSecretEntries(d.headers)},"parameters":${encodeSecretEntries(d.parameters, true)}}`
}
export function modelEndpoint(config: ModelConnection): string {
  return `${config.base_url.replace(/\/+$/, '')}/${config.protocol === 'openai_chat' ? 'chat/completions' : 'v1/messages'}`
}
export function testStatus(receipt?: ModelTest): string {
  if (!receipt) return '受理情况待核对'
  if (receipt.status === 'pending') return '排队中'
  if (receipt.status === 'running') return '运行中'
  return { not_sent: '未发送', transport_unknown: '效果未知', http_error: 'HTTP 错误', invalid_response: '无效响应', valid_response: '有效响应' }[receipt.outcome!] ?? '结果待核对'
}
export function testAttempt(receipt: ModelTest): string {
  return receipt.attempted === true ? '已进入传输' : receipt.attempted === false ? '未进入传输' : '发送阶段未知'
}
export function requireTestIdentity(receipt: ModelTest, configID: string, testID: string): ModelTest {
  if (receipt.config_id !== configID || receipt.id !== testID || !Number.isSafeInteger(receipt.config_revision) || receipt.config_revision < 1 || !['pending', 'running', 'completed'].includes(receipt.status)) throw new Error('测试回执身份不匹配')
  return receipt
}
