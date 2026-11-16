import { exactDecimal } from '../ui/exactDecimal.ts'
import type { ModelConfig } from './models.ts'
export type Decimal = string
export type ModelSource = 'managed' | 'environment'
export type ModelProtocol = 'openai_chat' | 'anthropic_messages'
export type CallOutcome = 'pending' | 'not_sent' | 'transport_unknown' | 'http_error' | 'invalid_response' | 'valid_response' | 'interrupted'
export interface PriceTerms { currency: string; input_per_million: Decimal; output_per_million: Decimal; cache_read_per_million: Decimal | null; cache_write_5m_per_million: Decimal | null; cache_write_1h_per_million: Decimal | null }
export interface PriceTarget { source: ModelSource; model_config_id: string; protocol: ModelProtocol; model: string }
export interface ModelPrice extends PriceTarget, PriceTerms { id: string; revision: number; enabled: boolean; created_at: string; updated_at: string }
export interface PriceWrite extends PriceTarget, PriceTerms { expected_revision: number; enabled: boolean }
export interface ModelUsage { status: 'reported' | 'partial' | 'missing' | 'invalid'; input_tokens: Decimal | null; output_tokens: Decimal | null; total_tokens: Decimal | null; cache_read_input_tokens: Decimal | null; cache_creation_input_tokens: Decimal | null; cache_creation_5m_input_tokens: Decimal | null; cache_creation_1h_input_tokens: Decimal | null; reasoning_output_tokens: Decimal | null; provider_usage: Record<string, string> }
export interface ModelCost { status: 'estimated' | 'unpriced' | 'usage_missing' | 'unsupported'; currency: string | null; amount: Decimal | null; price_id: string | null; price_snapshot: PriceTerms | null }
export interface ModelCall extends PriceTarget { id: string; config_name: string; purpose: 'research' | 'verification' | 'connectivity_test'; mission_id: string; thread_id: string; attempt_id: string; test_id: string; started_at: string; finished_at: string | null; duration_ms: number | null; state: 'running' | 'completed' | 'interrupted'; outcome: CallOutcome; attempted: boolean | null; http_status: number | null; record_model_content: boolean; content_state: 'disabled' | 'pending' | 'available' | 'partial' | 'missing'; usage: ModelUsage; cost: ModelCost }
export interface ContentPart { state: 'available' | 'missing' | 'pending'; body_base64: string | null; media_type: string; complete: boolean; redacted: boolean }
export interface ModelCallContent { call_id: string; recorded: boolean; input: ContentPart | null; response: ContentPart | null }
export interface UsageRange { from: string; to: string; time_zone: string }
export interface UsageSummary { calls: Decimal; running_calls: Decimal; interrupted_calls: Decimal; input_tokens: Decimal; output_tokens: Decimal; total_tokens: Decimal; missing_usage_calls: Decimal; invalid_usage_calls: Decimal; estimated_costs: { currency: string; amount: Decimal; priced_calls: Decimal }[]; unpriced_calls: Decimal }
export interface UsageResponse { coverage_started_at: string; range: UsageRange; summary: UsageSummary; summary_scope?: 'all_time' | 'range'; daily: (UsageSummary & { date: string })[] }
export interface ModelCallPage { items: ModelCall[]; next_cursor: string | null; coverage_started_at: string; range: UsageRange }
export interface UsageFilters { from: string; to: string; time_zone: string; model_config_id: string; purpose: string; outcome: string }
export const emptyUsageFilters: UsageFilters = { from: '', to: '', time_zone: 'UTC', model_config_id: '', purpose: '', outcome: '' }
export function usageQuery(filters: UsageFilters, missionID?: string, cursor?: string): string {
  const q = new URLSearchParams()
  if (missionID) q.set('mission_id', missionID)
  for (const [key, value] of Object.entries(filters)) if (value) q.set(key, value)
  if (cursor) q.set('cursor', cursor)
  return q.toString()
}
export function filterError(filters: UsageFilters): string {
  try { new Intl.DateTimeFormat('en', { timeZone: filters.time_zone }).format() } catch { return '请输入有效的 IANA 时区。' }
  if (Boolean(filters.from) !== Boolean(filters.to)) return '请同时选择开始日期与结束日期。'
  if (!filters.from) return ''
  for (const date of [filters.from, filters.to]) if (!/^\d{4}-\d{2}-\d{2}$/.test(date) || !Number.isFinite(Date.parse(date)) || new Date(date).toISOString().slice(0, 10) !== date) return '日期格式无效。'
  const days = (Date.parse(filters.to) - Date.parse(filters.from)) / 86400000
  return days <= 0 ? '结束日期（不含）须晚于开始日期。' : days > 93 ? '单次日期范围不能超过 93 个日历日。' : ''
}
/** 只操作日历标签；真正时区/DST边界由服务器根据IANA时区计算。 */
export function recentCalendarRange(days: number, timeZone: string, now = new Date()): Pick<UsageFilters, 'from' | 'to'> {
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).formatToParts(now)
  const p = (name: string) => parts.find(part => part.type === name)!.value
  const date = new Date(`${p('year')}-${p('month')}-${p('day')}T00:00:00Z`)
  date.setUTCDate(date.getUTCDate() + 1)
  const to = date.toISOString().slice(0, 10)
  date.setUTCDate(date.getUTCDate() - days)
  return { from: date.toISOString().slice(0, 10), to }
}
export const outcomeLabels: Record<CallOutcome, string> = { pending: '执行中', not_sent: '未发送', transport_unknown: '发送结果未知', http_error: 'HTTP 错误', invalid_response: '返回无效', valid_response: '有效返回', interrupted: '执行中断' }
export const costLabels: Record<ModelCost['status'], string> = { estimated: '已估算', unpriced: '未配置价格', usage_missing: '用量不完整', unsupported: '计费分类不支持' }
export const usageLabels: Record<ModelUsage['status'], string> = { reported: '已上报', partial: '部分上报', missing: '未上报', invalid: '计数无效' }
export const contentLabels: Record<ModelCall['content_state'], string> = { disabled: '未启用记录', pending: '尚在执行', available: '可读取', partial: '部分内容', missing: '内容缺失' }
export const priceKey = (p: PriceTarget) => JSON.stringify([p.source, p.model_config_id, p.protocol, p.model])
export function knownPriceTargets(configs: ModelConfig[], calls: ModelCall[], prices: ModelPrice[]): { value: string; label: string; target: PriceTarget }[] {
  const found = new Map<string, { value: string; label: string; target: PriceTarget }>()
  for (const p of [...prices, ...calls, ...configs.map(c => ({ source: 'managed' as const, model_config_id: c.id, protocol: c.protocol, model: c.model, config_name: c.name }))]) {
    const target: PriceTarget = { source: p.source, model_config_id: p.model_config_id, protocol: p.protocol, model: p.model }
    const key = priceKey(target)
    found.set(key, { value: key, label: `${p.source === 'environment' ? '环境模型' : ('config_name' in p && p.config_name) || configs.find(c => c.id === p.model_config_id)?.name || p.model_config_id} · ${p.model} · ${p.protocol === 'openai_chat' ? 'OpenAI Chat' : 'Anthropic Messages'}`, target })
  }
  return [...found.values()]
}
export function validPrice(value: string): boolean { return /^(?:0|[1-9]\d*)(?:\.\d{1,6})?$/.test(value) && BigInt(value.split('.')[0]) <= 1_000_000n && (BigInt(value.split('.')[0]) < 1_000_000n || !/[1-9]/.test(value.split('.')[1] ?? '')) }
export const priceFields = ['input_per_million', 'output_per_million', 'cache_read_per_million', 'cache_write_5m_per_million', 'cache_write_1h_per_million'] as const
export const priceFieldLabels: Record<typeof priceFields[number], string> = { input_per_million: '普通输入', output_per_million: '输出（含推理）', cache_read_per_million: '缓存读取', cache_write_5m_per_million: '缓存写入 · 5 分钟', cache_write_1h_per_million: '缓存写入 · 1 小时' }
function normalizedDecimal(v: string | null): string | null { return v === null ? null : v.includes('.') ? v.replace(/0+$/, '').replace(/\.$/, '') : v }
export function matchesPriceWrite(price: ModelPrice, write: PriceWrite): boolean {
  return priceKey(price) === priceKey(write) && price.enabled === write.enabled && price.currency === write.currency && priceFields.every(f => normalizedDecimal(price[f]) === normalizedDecimal(write[f]))
}
export function displayCost(cost: ModelCost): string { return cost.status === 'estimated' && cost.amount !== null && cost.currency ? `${cost.currency} ${exactDecimal(cost.amount)}` : costLabels[cost.status] ?? '未知' }
export interface PriceDraft { id: string; original: ModelPrice | null; target: PriceTarget | null; currency: string; enabled: boolean; input_per_million: string; output_per_million: string; cache_read_per_million: string; cache_write_5m_per_million: string; cache_write_1h_per_million: string }
/** App内存持有，401与导航不丢失不确定写身份；明确退出才清空。 */
export class PriceEditSession {
  draft: PriceDraft | null = null
  pending: { id: string; body: PriceWrite } | null = null
  clear() { this.draft = null; this.pending = null }
}
