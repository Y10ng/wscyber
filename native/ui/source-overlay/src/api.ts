import type { SCONode } from '@cyber/cstx-easm';
import { Code, ConnectError } from '@connectrpc/connect'
import { requireAuth, requireConnection, aopClient } from './runtime/api-bridge'
import { AuthError } from './runtime/auth'
import { create, type MessageInitShape } from '@bufbuild/protobuf'
import { anyPack } from '@bufbuild/protobuf/wkt'
import {
  newID as newRPCID,
  AOPProtocolMessageSchema,
  FileProtocolMessageSchema,
  type AOPPayload,
  type AOPProtocolMessage,
  type Event as AOPEvent,
  type EventDelivery,
  type FileResult,
  type Session,
  type TurnReceipt,
} from '@cyber/aop';
import {
  CommandProtocolMessageSchema,
  GuardrailProtocolMessageSchema,
  JEVProtocolMessageSchema,
  type JEVProtocolMessage,
  type JEVLibrary,
  type GuardrailProtocolMessage,
  type Review,
  DistributeConfigSchema,
  LLMProbeRequestSchema,
  ReloadProtocolMessageSchema,
  AgentRunOptionsSchema,
  ScanStatus,
  SessionBindingSchema,
  type AgentView,
  type AgentListMetadata,
  type CommandProtocolMessage,
  type CommandSpec,
  type ConfigView,
  type DistributeConfig,
  type LLMProbeRequest,
  type LLMProbeResult,
  type ListModelsResult,
  type TestConnectionResponse,
  type ConnectionCheck,
  type LLMProviderView,
  type Scan,
  type ScanOptions,
  type SessionRecord,
  type SystemStatus as ServerStatus,
} from './cyber-proto'

export type { SCONode };
export type { AOPEvent };

// Generated proto types are the single source of truth for the API surface.
// Re-exported here so views keep a single import site for API types.
export type { AgentListMetadata, AgentView, CommandSpec, ConfigView, DistributeConfig, EventDelivery, LLMProviderView, LLMProbeRequest, LLMProbeResult, ListModelsResult, TestConnectionResponse, ConnectionCheck, Scan, ScanOptions, SessionRecord, ServerStatus, TurnReceipt };
export type { Session as AOPSession } from '@cyber/aop';
export { ScanStatus };

// Binary framing, matching the AOP channel. The events these services return
// carry google.protobuf.Any extensions whose type set is open — SessionHistory,
// SessionScanEvent, the observe payloads, and whatever a future agent attaches.
// JSON framing expands an Any by looking its type up in a client-side registry,
// so one unregistered extension makes the entire response undecodable; binary
// framing leaves the Any as {typeUrl, value} for the caller's own anyUnpack.
// Resolve clients at call time. Only the authenticated Cordis Hub owns them.
const cyberRPC = new Proxy({} as import('./runtime/connection').ConnectionService['rpc'], {
  get: (_target, name) => requireConnection().rpc[name as keyof import('./runtime/connection').ConnectionService['rpc']],
})

export const CONFIG_CHANGED_EVENT = 'cyber:config-changed'

export class APIError extends Error {
  constructor(message: string, public readonly status: number) {
    super(message)
    this.name = 'APIError'
  }
}

export async function getAuthSession(): Promise<boolean> { return requireAuth().check() }
export async function login(token: string): Promise<void> {
  try { await requireAuth().login(token) }
  catch (error) { if (error instanceof AuthError) throw new APIError(error.message, error.status); throw error }
}
export async function logout(): Promise<void> { await requireAuth().logout() }

export async function getAgentConnectToken(): Promise<string> {
  const body = await apiJSON<{ token?: string }>(
    '/api/auth/agent-token',
    'Failed to configure the agent token',
    { cache: 'no-store' },
  )
  return body.token ?? ''
}

export interface IOAIdentityBinding {
  namespace: string
  subject: string
  claims?: Record<string, unknown>
}

export interface IOANode {
  id: string
  name: string
  description?: string
  meta?: Record<string, unknown>
  identities?: IOAIdentityBinding[]
}

export interface IOASpace {
  id: string
  name: string
  tags?: string[]
  nodes?: IOANode[]
  message_count: number
}

export interface IOARef {
  messages?: string[]
  nodes?: string[]
}

export interface IOAMessage {
  id: string
  space_id: string
  sender: string
  created_at: string
  content_type?: string
  content: Record<string, unknown>
  refs?: IOARef
  meta?: Record<string, unknown>
  content_schema?: Record<string, unknown>
}

export interface IOAOverview {
  nodes: IOANode[]
  spaces: IOASpace[]
  messages: IOAMessage[]
}

export async function getStatus(): Promise<ServerStatus> {
  try {
    const response = await cyberRPC.system.getStatus({})
    if (!response.status) throw new Error('Status is unavailable')
    return response.status
  } catch (error) {
    throw connectFailure(error, 'Failed to load status')
  }
}

export async function listAgents(): Promise<AgentView[]> {
  try {
    const response = await cyberRPC.agents.listAgents({})
    return response.agents
  } catch (error) {
    throw connectFailure(error, 'Failed to list agents')
  }
}

// The Hub may intentionally have no local provider client: in the profile-
// neutral deployment the connected scan/audit node owns the provider. The
// status endpoint still exposes the saved provider/model, so configuration is
// present when a model and provider are available even if `llm_available` is
// false on the Hub itself.
export function llmConfigured(status: ServerStatus | null | undefined): boolean {
  return !!(
    status?.llmModel?.trim() &&
    (status.llmAvailable || status.llmApiKeyConfigured || status.llmProvider?.trim())
  )
}

export async function getConfigStatus(): Promise<ConfigView> {
  try {
    const response = await cyberRPC.config.getConfig({})
    if (!response.config) throw new Error('Config is unavailable')
    return response.config
  } catch (error) {
    throw connectFailure(error, 'Failed to load config')
  }
}

export async function saveConfig(config: DistributeConfig): Promise<ConfigView> {
  try {
    const response = await cyberRPC.config.updateConfig({ config })
    if (!response.config) throw new Error('Config update returned no view')
    window.dispatchEvent(new Event(CONFIG_CHANGED_EVENT))
    return response.config
  } catch (error) {
    throw connectFailure(error, 'Failed to save config')
  }
}

export async function activateLLMProfile(id: string): Promise<ConfigView> {
  try {
    const response = await cyberRPC.config.activateProfile({ profileId: id })
    if (!response.config) throw new Error('Profile activation returned no view')
    return response.config
  } catch (error) {
    throw connectFailure(error, 'Failed to switch LLM profile')
  }
}

// Keep the installed policy warm: interaction-mode edits do not reload agents.
export async function setGuardrailMode(mode: 'safe' | 'auto'): Promise<ConfigView> {
  const current = await getConfigStatus()
  return saveConfig(create(DistributeConfigSchema, {
    extensions: {
      guardrail: { ...current.extensions.guardrail?.values, mode },
    },
  }))
}

// Blank api_key asks the server to reuse the stored secret.
export async function testLLM(req: MessageInitShape<typeof LLMProbeRequestSchema>): Promise<LLMProbeResult> {
  return cyberRPC.config.testLLM(req)
}

export async function listLLMModels(req: MessageInitShape<typeof LLMProbeRequestSchema>): Promise<ListModelsResult> {
  return cyberRPC.config.listModels(req)
}

// testConn probes the external dependencies of a settings section
// (cyberhub | recon | search | ioa.client). The current form is
// sent so edits are tested; blank secrets fall back to stored values server-side.
export async function testConn(section: string, config: DistributeConfig): Promise<TestConnectionResponse> {
  return cyberRPC.config.testConnection({ section, config })
}

// --- IOA overview ---

export async function getIOAOverview(): Promise<IOAOverview> {
  const signal = AbortSignal.timeout(10_000)
  const [nodes, spaces, messages] = await Promise.all([
    apiJSON<IOANode[]>('/ioa/nodes', 'Failed to load IOA nodes', { signal }),
    apiJSON<IOASpace[]>('/ioa/spaces', 'Failed to load IOA spaces', { signal }),
    apiJSON<IOAMessage[]>('/ioa/messages', 'Failed to load IOA messages', { signal }),
  ])
  return { nodes, spaces, messages }
}

// --- Chat session API ---

export async function createChatSession(nodeID: string, title?: string, scanID?: string, opts?: { sessionID: string; requestID: string }): Promise<Session> {
  try {
	const extensions = scanID
		? [anyPack(SessionBindingSchema, create(SessionBindingSchema, { scanId: scanID }))]
		: []
	const response = await requestCore(create(AOPProtocolMessageSchema, { message: { case: 'openSessionRequest', value: { sessionId: opts?.sessionID || newRPCID(), nodeId: nodeID, title: title || '', extensions } } }), 'openSessionResponse', opts?.requestID)
    if (response.outcome.case !== 'accepted') throw rejectionError(response.outcome.value, 'Failed to create session')
    return response.outcome.value
  } catch (error) {
    throw connectFailure(error, 'Failed to create session')
  }
}

export type SessionFilters = { search: string; nodeId: string; archived: boolean }
export async function listChatSessions(filters?: Partial<SessionFilters>): Promise<SessionRecord[]> {
  const sessions: SessionRecord[] = []
  let afterCursor = ''
  do {
    const response = await cyberRPC.sessions.listSessions({ ...filters, afterCursor, limit: 100, includeClosed: true })
    sessions.push(...response.sessions)
    afterCursor = response.nextCursor
  } while (afterCursor)
  return [...new Map(sessions.map((record) => [record.session?.id, record])).values()]
}

export async function updateChatSession(sessionId: string, patch: { title?: string; archived?: boolean }): Promise<SessionRecord> {
  const response = await cyberRPC.sessions.updateSession({ requestId: newRPCID(), sessionId, ...patch })
  if (response.outcome.case !== 'accepted') throw rejectionError(response.outcome.value, 'Failed to update session')
  return response.outcome.value
}

export async function getChatSession(id: string): Promise<SessionRecord> {
  try {
    const response = await cyberRPC.sessions.getSession({ sessionId: id })
    if (!response.session) throw new Error('Session not found')
    return response.session
  } catch (error) {
    throw connectFailure(error, 'Session not found')
  }
}

// 只读核对（不重试/不写）：真正 Code.NotFound 才视为会话不存在；其它错误在 connectFailure
// 抹掉 ConnectError.code 之前原样抛出。record 存在但缺原生快照属于「存在但暂不能核对」，
// 不当作不存在。
export async function lookupChatSession(id: string): Promise<Session | null> {
  try {
    const response = await cyberRPC.sessions.getSession({ sessionId: id })
    const record = response.session
    if (!record || !record.session) throw new Error('Session exists but native snapshot is missing')
    return record.session
  } catch (error) {
    if (ConnectError.from(error).code === Code.NotFound) return null
    throw error
  }
}

export async function deleteChatSession(id: string): Promise<void> {
  try {
    const response = await cyberRPC.sessions.deleteSession({ requestId: newRPCID(), sessionId: id })
    if (response.outcome.case !== 'accepted') throw rejectionError(response.outcome.value, 'Failed to delete session')
  } catch (error) {
    throw connectFailure(error, 'Failed to delete session')
  }
}

export async function resetChatSession(id: string): Promise<SessionRecord> {
  try {
    const response = await cyberRPC.sessions.resetSession({ requestId: newRPCID(), sessionId: id })
    if (response.outcome.case !== 'accepted' || !response.outcome.value.current) {
      throw rejectionError(response.outcome.case === 'rejected' ? response.outcome.value : undefined, 'Failed to reset session')
    }
    return response.outcome.value.current
  } catch (error) {
    throw connectFailure(error, 'Failed to reset session')
  }
}

// The web "/" menu is built from the cyber.chat.CommandSpec list returned by
// SessionService/ListCommands (hub-scope commands merged with the bound
// agent's reported commands), so it always reflects the real command set
// instead of a hardcoded list.
export async function fetchSessionCommands(sessionID: string): Promise<CommandSpec[]> {
  try {
    const response = await cyberRPC.sessions.listCommands({ sessionId: sessionID })
    return response.commands
  } catch (error) {
    throw connectFailure(error, 'Failed to load commands')
  }
}

export type ChatSendOptions = {
  persist?: boolean
  evalCriteria?: string
  evalRounds?: string
  images?: { data: Uint8Array; mediaType: string; filename: string }[]
  // Optional turn budget forwarded verbatim to the native RunTurn field. Leave
  // unset for non-local nodes to preserve the existing protobuf default (0).
  maxTurns?: number
}

export async function sendChatMessage(
  sessionID: string,
  content: string,
  opts?: ChatSendOptions & {
    messageID?: string
    turnID?: string
    requestID?: string
    continueSession?: boolean
  },
): Promise<TurnReceipt> {
  const messageID = opts?.messageID || newRPCID()
  const turnID = opts?.turnID || newRPCID()
  const extensions = []
  const criteria = opts?.persist ? opts.evalCriteria?.trim() : ''
  if (criteria) {
    const value = create(AgentRunOptionsSchema, { evalCriteria: criteria, evalRounds: opts?.evalRounds?.trim() || '' })
    extensions.push(anyPack(AgentRunOptionsSchema, value))
  }
  try {
	const request = create(AOPProtocolMessageSchema, { message: { case: 'runTurnRequest', value: {
	  sessionId: sessionID, turnId: turnID, continueSession: opts?.continueSession === true,
	  maxTurns: opts?.maxTurns ?? 0,
	  input: { id: messageID, role: 'user', content: opts?.continueSession ? [] : [
      ...(content ? [{ value: { case: 'text' as const, value: { text: content } } }] : []),
      ...(opts?.images || []).map(image => ({ value: { case: 'media' as const, value: {
        kind: 'image', resource: { mediaType: image.mediaType, filename: image.filename, source: { case: 'data' as const, value: image.data } },
      } } })),
    ] }, extensions,
	} } })
	const response = await requestCore(request, 'runTurnResponse', opts?.requestID)
    if (response.outcome.case !== 'accepted') throw rejectionError(response.outcome.value, 'Failed to send message')
    return response.outcome.value
  } catch (error) {
    throw connectFailure(error, 'Failed to send message')
  }
}

export async function executeChatCommand(sessionID: string, line: string, requestID?: string): Promise<void> {
  try {
	const request = create(CommandProtocolMessageSchema, { message: { case: 'request', value: { sessionId: sessionID, line } } })
	const response = await aopClient.request(CommandProtocolMessageSchema, request, { id: requestID })
	if (response.$typeName === 'aop.ProtocolMessage') {
		const core = response as AOPProtocolMessage
		if (core.message.case === 'protocolError') throw new Error(core.message.value.message)
	}
  } catch (error) {
	throw error instanceof Error ? error : new Error('Failed to execute command')
  }
}

async function requestGuardrail(request: GuardrailProtocolMessage): Promise<GuardrailProtocolMessage> {
  const response = await aopClient.request(GuardrailProtocolMessageSchema, request, { timeoutMs: 12_000, requireConnected: request.message.case === 'resolve' })
  if (response.$typeName === 'aop.ProtocolMessage') {
    const core = response as AOPProtocolMessage
    if (core.message.case === 'protocolError') throw new Error(core.message.value.message)
  }
  if (response.$typeName !== 'cyber.guardrail.ProtocolMessage') throw new Error('Unexpected guardrail response')
  return response as GuardrailProtocolMessage
}

export async function pendingGuardrailReviews(sessionId: string): Promise<Review[]> {
  const response = await requestGuardrail(create(GuardrailProtocolMessageSchema, { message: { case: 'pending', value: { sessionId } } }))
  if (response.message.case !== 'pendingResult') throw new Error('Expected pending guardrail reviews')
  return response.message.value.reviews
}

export async function getJEVLibrary(sessionId: string): Promise<JEVLibrary> {
  const response = await aopClient.request(JEVProtocolMessageSchema,
    create(JEVProtocolMessageSchema, { message: { case: 'request', value: { sessionId } } }), { timeoutMs: 12_000 })
  if (response.$typeName === 'aop.ProtocolMessage') {
    const core = response as AOPProtocolMessage
    if (core.message.case === 'protocolError') throw new Error(core.message.value.message)
  }
  if (response.$typeName !== 'cyber.jev.ProtocolMessage') throw new Error('Unexpected JEV response')
  const value = response as JEVProtocolMessage
  if (value.message.case !== 'library') throw new Error('Expected JEV library')
  return value.message.value
}

export async function resolveGuardrailReview(sessionId: string, operationId: string, approve: boolean): Promise<void> {
  const response = await requestGuardrail(create(GuardrailProtocolMessageSchema, { message: { case: 'resolve', value: { sessionId, operationId, approve } } }))
  if (response.message.case !== 'resolved') throw new Error('Expected guardrail resolution')
}

export async function cancelChatSession(sessionID: string, turnID: string): Promise<void> {
  if (!turnID) throw new Error('No active turn')
  try {
	const response = await requestCore(create(AOPProtocolMessageSchema, { message: { case: 'cancelTurnRequest', value: { sessionId: sessionID, turnId: turnID, reason: 'user_requested' } } }), 'cancelTurnResponse')
    if (response.outcome.case !== 'accepted') throw rejectionError(response.outcome.value, 'Failed to pause response')
  } catch (error) {
    throw connectFailure(error, 'Failed to pause response')
  }
}

export async function closeChatSession(sessionID: string): Promise<void> {
  try {
	const response = await requestCore(create(AOPProtocolMessageSchema, { message: { case: 'closeSessionRequest', value: { sessionId: sessionID, reason: 'completed' } } }), 'closeSessionResponse')
    if (response.outcome.case !== 'accepted') throw rejectionError(response.outcome.value, 'Failed to close session')
  } catch (error) {
    throw connectFailure(error, 'Failed to close session')
  }
}

export type FileUploadResult = FileResult

export async function uploadChatFile(sessionID: string, file: File): Promise<FileUploadResult> {
  try {
	const request = create(FileProtocolMessageSchema, { message: { case: 'uploadRequest', value: { sessionId: sessionID, filename: file.name, mediaType: file.type || 'application/octet-stream', data: new Uint8Array(await file.arrayBuffer()) } } })
	const payload = await aopClient.request(FileProtocolMessageSchema, request)
	if (payload.$typeName === 'aop.file.ProtocolMessage') {
		const filePayload = payload as import('@cyber/aop').FileProtocolMessage
		if (filePayload.message.case === 'result') return filePayload.message.value
	}
	if (payload.$typeName === 'aop.ProtocolMessage') {
		const core = payload as AOPProtocolMessage
		if (core.message.case === 'protocolError') throw new Error(core.message.value.message)
	}
	throw new Error('Upload returned an unexpected response')
  } catch (error) {
    throw connectFailure(error, 'Upload failed')
  }
}

// Chat history is the raw AOP event log; views project EventDelivery records
// directly into the existing AOP reducer.
export async function listChatEvents(sessionID: string, afterCursor = ''): Promise<EventDelivery[]> {
  try {
    const events: EventDelivery[] = []
    do {
      const response = await cyberRPC.sessions.listEvents({ sessionId: sessionID, limit: 500, afterCursor })
      events.push(...response.events)
      if (response.nextCursor === afterCursor && afterCursor) throw new Error('History cursor did not advance')
      afterCursor = response.nextCursor
    } while (afterCursor)
    return events
  } catch (error) {
    throw connectFailure(error, 'Failed to list messages')
  }
}

export function subscribeAOPEvents(
  sessionID: string,
  onEvent: (event: AOPEvent, cursor: string) => void,
  afterCursor = '',
): () => void {
	const watch = (cursor: string) => create(AOPProtocolMessageSchema, { message: { case: 'watchEventsRequest', value: { sessionId: sessionID, afterCursor: cursor } } })
	return aopClient.subscribe(AOPProtocolMessageSchema, watch(afterCursor), (payload, envelope) => {
		if (payload.$typeName !== 'aop.ProtocolMessage') return
		const core = payload as AOPProtocolMessage
		if (core.message.case === 'event') onEvent(core.message.value, envelope.deliveryCursor)
	}, { durable: true, resume: (cursor) => watch(cursor || afterCursor) })
}

function rejectionError(value: { code?: string; message?: string } | undefined, fallback: string): Error {
  return Object.assign(new Error(value?.message || value?.code || fallback), { rejected: true, code: value?.code })
}

function connectFailure(error: unknown, fallback: string): Error {
  if ((error as { rejected?: boolean })?.rejected && error instanceof Error) return error
  const failure = ConnectError.from(error)
  return new Error(failure.rawMessage || failure.message || fallback)
}

type CoreMessage = AOPProtocolMessage['message']
type CoreCase = Exclude<CoreMessage['case'], undefined>
type CoreValue<C extends CoreCase> = Extract<CoreMessage, { case: C }>['value']

async function requestCore<C extends CoreCase>(request: AOPProtocolMessage, expected: C, id?: string): Promise<CoreValue<C>> {
	const payload = await aopClient.request(AOPProtocolMessageSchema, request, { id })
	if (payload.$typeName !== 'aop.ProtocolMessage') throw new Error(`Unexpected AOP response ${payload.$typeName}`)
	const core = payload as AOPProtocolMessage
	if (core.message.case === 'protocolError') throw new Error(core.message.value.message)
	if (core.message.case !== expected) throw new Error(`Expected ${expected}, received ${core.message.case || 'empty'}`)
	return core.message.value as CoreValue<C>
}

export { aopClient }

export async function syncArtifactEvents(afterCursor = '', artifacts: AOPEvent[] = []): Promise<EventDelivery[]> {
  try {
    const response = await cyberRPC.artifacts.syncArtifacts({ afterCursor, artifacts })
    return response.artifacts
  } catch (error) {
    throw connectFailure(error, 'Failed to sync artifacts')
  }
}

async function apiJSON<T>(path: string, fallbackMessage: string, init?: RequestInit): Promise<T> {
  const res = await authenticatedFetch(path, init)
  if (!res.ok) {
    throw new APIError(await errorMessage(res, fallbackMessage), res.status)
  }
  return res.json();
}

async function authenticatedFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  return requireConnection().fetch(input, init)
}


async function errorMessage(res: Response, fallback: string) {
  try {
    const body = await res.json();
    return body?.error || fallback;
  } catch {
    return fallback;
  }
}

export async function getScan(id: string): Promise<Scan> {
  const response = await cyberRPC.scans.getScan({ scanId: id })
  if (!response.scan) throw new Error('Scan not found')
  return response.scan
}

// --- Web/API 任务与流量原件（原生 webapi capability 的冻结业务接口） ---
// 保存/读取走同源原生鉴权内的 apiJSON，仅声明后端已冻结的 JSON 形状；
// Flow 原件在展示组件中用 @cyber/aop 生成类型解码，不在本层猜字段。

export interface WebApiTask {
  id: string
  known_info: string
  objective: string
  origins: string[]
  initial_session_id: string
  created_at: string
  // B1 任务级测试策略；旧后端缺省 undefined 时按默认（origins + high_risk）渲染。
  policy?: WebApiTaskPolicy
  // M1 研究场景与完成标准（native/SCENARIO.md）；旧后端缺省时按默认场景渲染。
  scenario?: WebApiScenario
  // 从资产发起的任务带资产引用；普通任务为 null/缺省。
  asset?: { id: string; name: string } | null
}

// M1 研究场景：任务/资产视图里的 scenario。done_criteria 为保存的自定义文本
// （空＝用场景默认），done_criteria_effective 为实际生效文本，is_default＝无保存记录。
export interface WebApiScenario {
  key: string
  name: string
  done_criteria: string
  done_criteria_effective: string
  is_default: boolean
  // M3 持续探索：Goal 评估循环最大轮数生效值（1..20；保存值或场景默认）。旧后端缺省。
  max_rounds?: number
}

// 新建任务/资产表单用的场景选项（GET /api/webapi/scenarios，按展示顺序，第一项默认）。
export interface WebApiScenarioOption {
  key: string
  name: string
  default_done_criteria: string
  default_max_rounds?: number
  is_default: boolean
}

export async function listWebApiScenarios(): Promise<WebApiScenarioOption[]> {
  const body = await apiJSON<{ scenarios: WebApiScenarioOption[] }>('/api/webapi/scenarios', 'Failed to load scenarios')
  return body.scenarios
}

// 未提供的字段沿用当前值；done_criteria 传空串即恢复场景默认。下一次 Run 生效。
export async function putWebApiTaskScenario(taskID: string, input: { scenario?: string; done_criteria?: string }): Promise<WebApiScenario> {
  const body = await apiJSON<{ task_id: string; scenario: WebApiScenario }>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/scenario`,
    'Failed to save task scenario',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    },
  )
  return body.scenario
}

// scope: 'origins'（默认）| 'unrestricted'；approval: 'off' | 'high_risk'（默认）| 'all'。
export interface WebApiTaskPolicy {
  scope: string
  approval: string
  is_default: boolean
  updated_at?: string | null
}

// 只由人经鉴权 API 设置；枚举校验在后端，保存即生效（仅影响之后的调用）。
export async function saveWebApiPolicy(taskID: string, scope: string, approval: string): Promise<WebApiTaskPolicy> {
  const body = await apiJSON<{ task_id: string; policy: WebApiTaskPolicy }>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/policy`,
    'Failed to save task policy',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ scope, approval }),
    },
  )
  return body.policy
}

export interface WebApiSaveResult {
  task: WebApiTask
  conflict: boolean
  created: boolean
  idempotent: boolean
}

export interface WebApiEvidence {
  evidence_id: string
  task_id: string
  session_id: string
  turn_id: string
  call_id: string
  request_fingerprint: string
  method: string
  url: string
  status: string
  reason: string
  sent: boolean | null
  http_status?: number
  preview_truncated: boolean
  capture_truncated: boolean
  created_at: string
  completed_at?: string
  // M5b：证据来源（replay=一键重放，authz_compare=多账号身份对比变体）；普通请求省略。
  source?: 'replay' | 'authz_compare' | string
}

export type WebApiAuthzCompareStatus = 'sent' | 'review_required' | 'denied' | 'unknown' | 'error'
export type WebApiAuthzHint = 'same_as_original' | 'denied' | 'not_found' | 'different' | 'not_sent'

export interface WebApiAuthzCompare {
  id: string
  task_id: string
  session_id: string
  original_evidence_id: string
  label: string
  variant_evidence_id: string
  status: WebApiAuthzCompareStatus
  hint: WebApiAuthzHint
  http_status: number | null
  created_at: string
}

export interface WebApiEvidenceList {
  task_id: string
  items: WebApiEvidence[]
  limit: number
  has_more: boolean
  next_cursor: string | null
}

export interface WebApiEvidenceDetail {
  metadata: WebApiEvidence
  preview: string
  /** 遮盖响应（mask=1）省略该字段，改由 `flow`（protojson）提供对照内容。 */
  flow_protobuf_base64?: string
  flow: unknown
  /** 遮盖响应中替换的口令次数；未遮盖或缺省为 0。 */
  masked_secrets?: number
  capture: {
    request: string
    response_body_limit: number
    preview_limit: number
    capture_truncated: boolean
    preview_truncated: boolean
    auto_decompressed: boolean
  }
}

// 相同 ID 同规范内容由后端幂等返回同一 task；不同内容 409 会以 APIError 抛出，
// 前端不自行覆盖授权。客户端只生成一次 ID，重试复用同一 ID。
// M1：scenario/done_criteria 只在本次新建时随任务保存；幂等重放不改已保存值。
export async function saveWebApiTask(
  id: string,
  knownInfo: string,
  objective: string,
  origins: string[],
  scenario?: string,
  doneCriteria?: string,
): Promise<WebApiSaveResult> {
  const body: Record<string, unknown> = { known_info: knownInfo, objective, authorization: { confirmed: true, origins } }
  if (scenario) body.scenario = scenario
  if (doneCriteria) body.done_criteria = doneCriteria
  return apiJSON<WebApiSaveResult>(`/api/webapi/tasks/${encodeURIComponent(id)}`, 'Failed to save task', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export async function getWebApiTask(id: string): Promise<WebApiTask> {
  return apiJSON<WebApiTask>(`/api/webapi/tasks/${encodeURIComponent(id)}`, 'Failed to load task')
}

// 选中/刷新会话后按原生 session 恢复业务 Task；不根据聊天文本/标题猜 TaskID。
export async function getWebApiTaskBySession(sessionId: string): Promise<WebApiTask> {
  return apiJSON<WebApiTask>(`/api/webapi/tasks?session_id=${encodeURIComponent(sessionId)}`, 'Failed to load task')
}

// --- 全局任务列表（U2a，只读）：任务页表格与侧栏计数 ---
export type WebApiTaskListStatus = 'running' | 'idle' | 'not_started' | 'unknown'

export interface WebApiTaskListItem {
  task_id: string
  title: string
  origins: string[]
  asset: { id: string; name: string } | null
  scheduled: boolean
  created_at: string
  initial_session_id: string
  status: WebApiTaskListStatus
  archived: boolean
  last_activity_at: string
  finding_count: number
  verification_count: number
  supported_count: number
  report_count: number
  retest_count: number
  total_tokens: number
  policy: { scope: string; approval: string }
  // M1 研究场景；旧响应缺省时按默认场景处理。
  scenario?: WebApiScenario
}

export interface WebApiTaskListCounts {
  all: number
  running: number
  idle: number
  not_started: number
  unknown: number
  archived: number
}

export interface WebApiTaskList {
  items: WebApiTaskListItem[]
  has_more: boolean
  next_cursor: string | null
  counts: WebApiTaskListCounts
}

export async function listWebApiTaskList(params: {
  q?: string
  status?: WebApiTaskListStatus
  asset_id?: string
  archived?: boolean
  limit?: number
  cursor?: string
} = {}): Promise<WebApiTaskList> {
  const query = new URLSearchParams()
  if (params.q) query.set('q', params.q)
  if (params.status) query.set('status', params.status)
  if (params.asset_id) query.set('asset_id', params.asset_id)
  if (params.archived) query.set('archived', '1')
  if (params.limit) query.set('limit', String(params.limit))
  if (params.cursor) query.set('cursor', params.cursor)
  const suffix = query.toString()
  return apiJSON<WebApiTaskList>(`/api/webapi/task-list${suffix ? `?${suffix}` : ''}`, 'Failed to load tasks')
}

// --- 全局漏洞（U2a，只读）：GET /vulns，字段见 native/OVERVIEW.md ---
// 全局漏洞 = 未归档资产的漏洞条目 + 未关联资产任务的候选（kind=task_finding，
// id=finding:<finding_id>）。状态与排序由后端按资产漏洞库同一规则派生。
// 漏洞处置（V1）：严重度、处置、负责人、修复期限。空串严重度 = 未定级；
// owner/due_date 为空时由后端读时派生 owner_effective/due_effective。
export type WebApiVulnSeverity = 'critical' | 'high' | 'medium' | 'low' | 'info' | ''
export type WebApiVulnDisposition = 'open' | 'fixing' | 'risk_accepted' | 'false_positive'

export interface WebApiVulnTriage {
  vuln_key: string
  severity: WebApiVulnSeverity
  disposition: WebApiVulnDisposition
  owner: string
  owner_effective: string
  due_date: string
  due_effective: string | null
  overdue: boolean
  note: string
  updated_at: string | null
}

export interface WebApiVulnTriageInput {
  severity?: WebApiVulnSeverity
  disposition?: WebApiVulnDisposition
  owner?: string
  due_date?: string
  note?: string
}

export interface WebApiVulnSLA {
  critical_days: number
  high_days: number
  medium_days: number
  low_days: number
  updated_at: string | null
}

export interface WebApiGlobalVuln {
  id: string
  /** K1：稳定编号 V-XXXXXXXX。 */
  code: string
  kind: 'asset_vuln' | 'task_finding'
  title: string
  status: WebApiVulnStatus
  endpoint_key: string
  asset: { id: string; name: string } | null
  task_id: string
  first_seen_at: string
  last_confirmed_at: string | null
  fixed_at: string | null
  last_event: WebApiVulnEvent | null
  member_count: number
  task_count: number
  triage: WebApiVulnTriage
  /** K1：成员最新带建议等级的 supported 验证；空串 = 无建议。 */
  suggested_severity: WebApiVulnSeverity
}

// 严重度计数（by_severity）：none = 未定级。
export interface WebApiVulnSeverityCounts {
  critical: number
  high: number
  medium: number
  low: number
  info: number
  none: number
}

export interface WebApiGlobalVulnCounts {
  new: number
  present: number
  pending: number
  unconfirmed: number
  fixed: number
  overdue: number
  by_severity: WebApiVulnSeverityCounts
}

export interface WebApiGlobalVulnList {
  items: WebApiGlobalVuln[]
  has_more: boolean
  next_cursor: string | null
  counts: WebApiGlobalVulnCounts
}

export async function listWebApiGlobalVulns(params: {
  status?: WebApiVulnStatus
  severity?: string
  disposition?: string
  overdue?: boolean
  asset_id?: string
  q?: string
  limit?: number
  cursor?: string
} = {}): Promise<WebApiGlobalVulnList> {
  const query = new URLSearchParams()
  if (params.status) query.set('status', params.status)
  if (params.severity) query.set('severity', params.severity)
  if (params.disposition) query.set('disposition', params.disposition)
  if (params.overdue) query.set('overdue', '1')
  if (params.asset_id) query.set('asset_id', params.asset_id)
  if (params.q) query.set('q', params.q)
  if (params.limit) query.set('limit', String(params.limit))
  if (params.cursor) query.set('cursor', params.cursor)
  const suffix = query.toString()
  return apiJSON<WebApiGlobalVulnList>(`/api/webapi/vulns${suffix ? `?${suffix}` : ''}`, 'Failed to load vulnerabilities')
}

// 保存漏洞人工处置（V1）：PUT /vulns/{key}/triage，返回 Triage View。
// key 来自全局漏洞 id（资产条目 vid 或 finding:<id>），路径段整体编码。
export async function saveWebApiVulnTriage(key: string, body: WebApiVulnTriageInput): Promise<WebApiVulnTriage> {
  return apiJSON<WebApiVulnTriage>(
    `/api/webapi/vulns/${encodeURIComponent(key)}/triage`,
    'Failed to save triage',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
}

// --- K1 漏洞卡（只读聚合，字段见 native/VULN_CARD.md）-------------------
export interface WebApiVulnCardAsset {
  id: string
  name: string
  env: string
  owner: string
  origins: string[]
}

export interface WebApiVulnCardHeader {
  key: string
  code: string
  title_template: string
  title: string
  status: WebApiVulnStatus
  severity: WebApiVulnSeverity
  suggested_severity: WebApiVulnSeverity
  severity_reason: string
  disposition: WebApiVulnDisposition
  due_effective: string | null
  overdue: boolean
  first_seen_at: string
  asset: WebApiVulnCardAsset | null
  endpoint_key: string
}

export interface WebApiVulnCardStep {
  title: string
  evidence_id: string
  method: string
  url: string
  status_code: number | null
  request_marks_found: string[]
  response_marks_found: string[]
  mark_missing: boolean
}

export interface WebApiVulnCardEvidenceRef {
  evidence_id: string
  method: string
  url: string
  status_code: number | null
}

export interface WebApiVulnCardContrast {
  normal_evidence_id: string
  abnormal_evidence_id: string
  note: string
  normal: WebApiVulnCardEvidenceRef | null
  abnormal: WebApiVulnCardEvidenceRef | null
}

export interface WebApiVulnCardRepro {
  verification_id: string
  task_id: string
  finding_id: string
  session_id: string
  impact: string
  preconditions: string
  steps: WebApiVulnCardStep[]
  contrast: WebApiVulnCardContrast | null
  remediation: string[]
  retest_method: string
  saved_at: string
}

export interface WebApiVulnCardRetest {
  retest_id: string
  task_id: string
  finding_id: string
  session_id: string
  conclusion: string
  summary: string
  at: string
}

/** V3：该键最近一次成功的“申请复测”及其会话状态（native/VULN_CARD.md）。 */
export interface WebApiVulnRetestRequest {
  retest_id: string
  session_id: string
  task_id: string
  finding_id: string
  started_at: string
  status: 'running' | 'done'
}

export type WebApiVulnCardLifecycleKind = 'candidate' | 'verification' | 'triage' | 'notify' | 'replay' | 'retest_request' | 'retest' | 'fixed'
export type WebApiVulnCardActor = 'ai' | 'user' | 'system'

export interface WebApiVulnCardEvent {
  at: string
  kind: WebApiVulnCardLifecycleKind
  title: string
  detail: string
  actor: WebApiVulnCardActor
}

export interface WebApiVulnCardVerification {
  verification_id: string
  task_id: string
  finding_id: string
  finding_number: string
  session_id: string
  created_at: string
  verdict: string | null
  method: string
  preconditions: string
  summary: string
  limitations: string
  suggested_severity: WebApiVulnSeverity
  severity_reason: string
  has_repro: boolean
  evidence_count: number
  saved_at: string | null
}

export interface WebApiVulnCardMember {
  task_id: string
  finding_id: string
  number: string
  title: string
  link: string
  latest_verdict: string | null
  latest_retest: string | null
  created_at: string
  initial_session_id: string
}

export interface WebApiVulnCardEvidence {
  evidence_id: string
  task_id: string
  source: 'candidate' | 'verification' | 'retest'
  relation: string
  method: string
  url: string
  status_code: number | null
  created_at: string
}

export interface WebApiVulnCardChange {
  id: string
  at: string
  actor: string
  action: string
  target_kind: string
  target_id: string
  task_id: string
  asset_id: string
  summary: string
  detail: unknown
}

export interface WebApiVulnCardAppendix {
  verifications: WebApiVulnCardVerification[]
  members: WebApiVulnCardMember[]
  evidence_count: number
  evidence: WebApiVulnCardEvidence[]
  changes: WebApiVulnCardChange[]
}

export interface WebApiVulnCardRelated {
  key: string
  code: string
  title: string
  severity: WebApiVulnSeverity
  /** K1/2d0afe0：同资产其他条目也带 AI 建议等级。 */
  suggested_severity: WebApiVulnSeverity
  status: WebApiVulnStatus
}

export interface WebApiVulnCardNav {
  prev_key: string | null
  next_key: string | null
}

/** K2：漏洞卡的推送段（native/NOTIFY.md“漏洞卡人工推送”）。 */
export interface WebApiVulnCardNotify {
  configured: boolean
  require_confirm: boolean
  items: WebApiNotification[]
}

/** 漏洞卡人工推送的逐字预览；image 为 null 表示不发图。 */
export interface WebApiVulnNotifyPreview {
  verification_id: string
  markdown: string
  image: { task_id: string; evidence_id: string; marks: string[] } | null
  image_note: string
}

/** 推送/预览错误体；status 为平台语义状态。 */
export interface WebApiVulnNotifyError {
  error?: string
  code?: 'not_configured' | 'false_positive' | 'no_supported' | 'in_flight'
  notification_id?: string
}

/** 带 HTTP 状态与已解析错误体的推送失败；供 UI 区分 409 原因。 */
export class VulnNotifyAPIError extends APIError {
  constructor(message: string, public readonly httpStatus: number, public readonly body: WebApiVulnNotifyError | null) {
    super(message, httpStatus)
    this.name = 'VulnNotifyAPIError'
  }
}

// --- K2 一键重放（native/REPLAY.md）------------------------------------
export type WebApiReplayVerdictHint = 'still_reproducible' | 'changed' | 'unknown'

export interface WebApiReplayMarksFound {
  request: string[]
  response: string[]
}

/** 重放请求 200 响应体（字段照抄 native/REPLAY.md）。 */
export interface WebApiReplayResult {
  replay_id: string
  step: number
  evidence_id: string
  task_id: string
  at: string
  request: { method: string; url: string }
  original: {
    evidence_id: string
    status_code: number | null
    body_bytes: number
    marks_found: WebApiReplayMarksFound
  }
  replayed: {
    status_code: number | null
    body_bytes: number
    capture_truncated: boolean
    marks_found: WebApiReplayMarksFound
  }
  comparison: {
    same_status: boolean
    response_marks_still_present: boolean
    verdict_hint: WebApiReplayVerdictHint
  }
  note: string
}

/** 重放请求非 2xx 的错误体；status 为平台语义状态，不是 HTTP 状态码。 */
export interface WebApiReplayError {
  error?: string
  status?: 'denied' | 'review_required' | 'archived' | 'replay_in_progress' | 'unresolved' | 'unknown'
  reason?: string
  evidence_id?: string
  retry_after?: number
}

/** 带 HTTP 状态与已解析错误体的重放失败；供 UI 区分 403/409/429/502。 */
export class ReplayAPIError extends APIError {
  constructor(message: string, public readonly httpStatus: number, public readonly body: WebApiReplayError | null) {
    super(message, httpStatus)
    this.name = 'ReplayAPIError'
  }
}

/** 漏洞卡 `replays[]` 项。 */
export interface WebApiVulnCardReplay {
  replay_id: string
  step: number
  /** 重放证据所属任务（K2）。 */
  task_id: string
  evidence_id: string
  at: string
  status_code: number | null
  verdict_hint: WebApiReplayVerdictHint
}

export type WebApiVulnCardMissingReason = 'no_verification' | 'no_supported' | 'no_key_steps'

export interface WebApiVulnCard {
  header: WebApiVulnCardHeader
  triage: WebApiVulnTriage | null
  repro: WebApiVulnCardRepro | null
  repro_missing_reason: WebApiVulnCardMissingReason | null
  retest: WebApiVulnCardRetest | null
  /** V3：最近一次成功的申请复测；无则 null。 */
  retest_request: WebApiVulnRetestRequest | null
  lifecycle: WebApiVulnCardEvent[]
  appendix: WebApiVulnCardAppendix
  related: WebApiVulnCardRelated[]
  nav: WebApiVulnCardNav
  notify: WebApiVulnCardNotify
  replays: WebApiVulnCardReplay[]
}

export async function getWebApiVulnCard(key: string): Promise<WebApiVulnCard> {
  return apiJSON<WebApiVulnCard>(
    `/api/webapi/vulns/${encodeURIComponent(key)}/card`,
    'Failed to load vulnerability card',
    { cache: 'no-store' },
  )
}

/** 卡片的 curl 命令（默认遮盖敏感值；本片只有 mask=1）。 */
export async function getWebApiVulnCardCurl(key: string, step: number): Promise<string> {
  const res = await authenticatedFetch(
    `/api/webapi/vulns/${encodeURIComponent(key)}/card/curl?step=${step}&mask=1`,
    { cache: 'no-store' },
  )
  if (!res.ok) throw new APIError(await errorMessage(res, 'Failed to generate curl'), res.status)
  return res.text()
}

/** 一键重放卡片关键请求的第 step 步（native/REPLAY.md）。错误带 httpStatus 与解析后的错误体。 */
export async function replayWebApiVulnStep(key: string, step: number): Promise<WebApiReplayResult> {
  const res = await authenticatedFetch(`/api/webapi/vulns/${encodeURIComponent(key)}/card/replay`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ step }),
    cache: 'no-store',
  })
  if (!res.ok) {
    let body: WebApiReplayError | null = null
    try {
      const parsed = await res.json()
      if (parsed && typeof parsed === 'object') body = parsed as WebApiReplayError
    } catch {
      body = null
    }
    throw new ReplayAPIError(body?.error || 'Failed to replay step', res.status, body)
  }
  return res.json() as Promise<WebApiReplayResult>
}

// --- V3 漏洞卡“申请复测”（native/VULN_CARD.md「申请复测」）---
export interface WebApiVulnRetestError {
  error?: string
  code?: 'archived' | 'false_positive' | 'no_member' | 'in_flight' | 'no_model'
  retest_id?: string
  session_id?: string
  stage?: string
}

/** 带 HTTP 状态与已解析错误体的申请复测失败；供 UI 区分 409 原因。 */
export class VulnRetestAPIError extends APIError {
  constructor(message: string, public readonly httpStatus: number, public readonly body: WebApiVulnRetestError | null) {
    super(message, httpStatus)
    this.name = 'VulnRetestAPIError'
  }
}

/** 申请复测：后端在代表候选的原任务下新建复测并启动会话（无需打开原任务）。 */
export async function requestWebApiVulnRetest(key: string): Promise<{
  retest_id: string
  session_id: string
  task_id: string
  finding_id: string
  revision_id: string
}> {
  const res = await authenticatedFetch(`/api/webapi/vulns/${encodeURIComponent(key)}/card/retest`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
    cache: 'no-store',
  })
  if (!res.ok) {
    let body: WebApiVulnRetestError | null = null
    try {
      const parsed = await res.json()
      if (parsed && typeof parsed === 'object') body = parsed as WebApiVulnRetestError
    } catch {
      body = null
    }
    throw new VulnRetestAPIError(body?.error || 'Failed to request retest', res.status, body)
  }
  return res.json() as Promise<{ retest_id: string; session_id: string; task_id: string; finding_id: string; revision_id: string }>
}

// --- K2 漏洞卡人工推送与漏洞报告导出（native/NOTIFY.md、VULN_CARD.md） ---
async function notifyErrorResponse(res: Response, fallback: string): Promise<VulnNotifyAPIError> {
  let body: WebApiVulnNotifyError | null = null
  try {
    const parsed = await res.json()
    if (parsed && typeof parsed === 'object') body = parsed as WebApiVulnNotifyError
  } catch {
    body = null
  }
  return new VulnNotifyAPIError(body?.error || fallback, res.status, body)
}

/** 人工推送将发送的逐字 markdown；未配置 webhook 也可预览。 */
export async function getWebApiVulnNotifyPreview(key: string): Promise<WebApiVulnNotifyPreview> {
  const res = await authenticatedFetch(
    `/api/webapi/vulns/${encodeURIComponent(key)}/card/notify/preview`,
    { cache: 'no-store' },
  )
  if (!res.ok) throw await notifyErrorResponse(res, 'Failed to load notify preview')
  return res.json() as Promise<WebApiVulnNotifyPreview>
}

/** 人工推送：直接入队（不受 require_confirm 影响）。 */
export async function notifyWebApiVulnCard(key: string): Promise<WebApiNotification> {
  const res = await authenticatedFetch(`/api/webapi/vulns/${encodeURIComponent(key)}/card/notify`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
    cache: 'no-store',
  })
  if (!res.ok) throw await notifyErrorResponse(res, 'Failed to push vulnerability card')
  return res.json() as Promise<WebApiNotification>
}

/** 单条漏洞报告导出（遮盖版）；取文本由调用方落文件。 */
export async function exportWebApiVulnCard(key: string, format: 'markdown' | 'json'): Promise<string> {
  const res = await authenticatedFetch(
    `/api/webapi/vulns/${encodeURIComponent(key)}/card/export?format=${format}`,
    { cache: 'no-store' },
  )
  if (!res.ok) throw new APIError(await errorMessage(res, 'Failed to export vulnerability report'), res.status)
  return res.text()
}

/** 多条漏洞报告导出（按 keys 顺序输出，1–50 条，遮盖版）。 */
export async function exportWebApiVulns(keys: string[], format: 'markdown' | 'json'): Promise<string> {
  const res = await authenticatedFetch('/api/webapi/vulns/export', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ keys, format }),
    cache: 'no-store',
  })
  if (!res.ok) throw new APIError(await errorMessage(res, 'Failed to export vulnerability reports'), res.status)
  return res.text()
}

/** 导出文件名：单条 `vuln-<code>.<ext>`，多条（code 为 null）`vulns-report.<ext>`。 */
export function webApiVulnExportFilename(code: string | null, format: 'markdown' | 'json'): string {
  const base = code ? `vuln-${code}` : 'vulns-report'
  return `${base}.${format === 'markdown' ? 'md' : 'json'}`
}

export async function getWebApiVulnSLA(): Promise<WebApiVulnSLA> {
  return apiJSON<WebApiVulnSLA>('/api/webapi/vuln-sla', 'Failed to load SLA')
}

export async function saveWebApiVulnSLA(body: Partial<Omit<WebApiVulnSLA, 'updated_at'>>): Promise<WebApiVulnSLA> {
  return apiJSON<WebApiVulnSLA>(
    '/api/webapi/vuln-sla',
    'Failed to save SLA',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  )
}

// --- 全局报告（U2a，只读）：GET /report-list，字段见 native/OVERVIEW.md ---
export interface WebApiGlobalReport {
  report_id: string
  task_id: string
  task_title: string
  asset: { id: string; name: string } | null
  session_id: string
  title: string
  has_draft: boolean
  item_count: number
  created_at: string
  updated_at: string
}

export interface WebApiGlobalReportList {
  items: WebApiGlobalReport[]
  has_more: boolean
  next_cursor: string | null
  total: number
}

export async function listWebApiGlobalReports(params: {
  q?: string
  asset_id?: string
  limit?: number
  cursor?: string
} = {}): Promise<WebApiGlobalReportList> {
  const query = new URLSearchParams()
  if (params.q) query.set('q', params.q)
  if (params.asset_id) query.set('asset_id', params.asset_id)
  if (params.limit) query.set('limit', String(params.limit))
  if (params.cursor) query.set('cursor', params.cursor)
  const suffix = query.toString()
  return apiJSON<WebApiGlobalReportList>(`/api/webapi/report-list${suffix ? `?${suffix}` : ''}`, 'Failed to load reports')
}

// --- 总览聚合（U2a，只读）：GET /overview，字段见 native/OVERVIEW.md ---
export interface WebApiOverviewDayCount {
  date: string
  count: number
}

export interface WebApiOverviewVulnDay {
  date: string
  new: number
  present: number
  fixed: number
}

export interface WebApiOverviewTokenDay {
  date: string
  total_tokens: number
}

export interface WebApiOverviewNextSchedule {
  asset_id: string
  asset_name: string
  next_run_at: string
}

export interface WebApiOverviewRecentRun {
  asset_id: string
  asset_name: string
  scheduled_for: string
  status: string
  task_id: string
  followups_state: string | null
}

export interface WebApiOverviewRecentSupported {
  vuln_id: string
  title: string
  asset: { id: string; name: string } | null
  task_id: string
  at: string
  /** OV2：triage 严重度，空串 = 未定级。 */
  severity: WebApiVulnSeverity
  /** K1：AI 建议等级，空串 = 无建议。 */
  suggested_severity: WebApiVulnSeverity
}

// --- OV2 总览字段（见 native/OVERVIEW.md 的 overview 节）---
export interface WebApiOverviewTodo {
  unrated: number
  pending_push: number
  overdue: number
}

export interface WebApiOverviewOverdueVuln {
  vuln_key: string
  title: string
  asset: { id: string; name: string } | null
  owner_effective: string
  overdue_days: number
}

export interface WebApiOverviewSLA {
  /** 0..1；区间内无已修复样本时为 null。 */
  rate: number | null
  closed_on_time: number
  closed_total: number
  avg_fix_days: Record<string, number | null>
  overdue_top: WebApiOverviewOverdueVuln[]
}

export interface WebApiOverviewStaleAsset {
  asset_id: string
  name: string
  last_tested_at: string | null
  days_since: number | null
}

export interface WebApiOverviewCoverage {
  window_days: number
  tested: number
  total: number
  stale_top: WebApiOverviewStaleAsset[]
}

export interface WebApiOverviewRunningTask {
  task_id: string
  title: string
  asset: { id: string; name: string } | null
  started_at: string
  elapsed_seconds: number
  board: { endpoints: number; covered_endpoints: number }
}

export interface WebApiOverviewFailedRun {
  asset_id: string
  asset_name: string
  scheduled_for: string
  status: string
  error: string
}

export interface WebApiOverview {
  generated_at: string
  timezone: string
  days: number
  assets: { total: number; archived: number; scheduled_enabled: number }
  tasks: { total: number; running: number; created_in_range: number; by_day: WebApiOverviewDayCount[] }
  vulns: {
    counts: Record<WebApiVulnStatus, number>
    open_by_severity: WebApiVulnSeverityCounts
    overdue: number
    confirmed_by_day: WebApiOverviewVulnDay[]
  }
  tokens: { total: number; by_day: WebApiOverviewTokenDay[] }
  schedules: { next: WebApiOverviewNextSchedule[]; recent_runs: WebApiOverviewRecentRun[] }
  recent_supported: WebApiOverviewRecentSupported[]
  todo: WebApiOverviewTodo
  sla: WebApiOverviewSLA
  coverage: WebApiOverviewCoverage
  running: WebApiOverviewRunningTask[]
  recent_failed_runs: WebApiOverviewFailedRun[]
}

export async function getWebApiOverview(days: 7 | 30 | 90): Promise<WebApiOverview> {
  return apiJSON<WebApiOverview>(`/api/webapi/overview?days=${days}`, 'Failed to load overview')
}

// --- 任务黑板（M2 后端，字段见 native/BOARD.md）--------------------------------
// 地图 = 流量/模型补充的接口清单；事实 = 有效或已撤回的客观信息；
// Oracle = 具体可证伪的待证明问题。成立 ≠ 漏洞确认，仍走独立验证。
export type WebApiBoardOracleStatus = 'open' | 'in_progress' | 'holds' | 'refuted' | 'blocked' | 'dropped'
export type WebApiBoardCategory = 'authz' | 'authn' | 'injection' | 'ssrf' | 'file' | 'logic' | 'info_disclosure' | 'config' | 'other'
export type WebApiBoardPriority = 'high' | 'medium' | 'low'
export type WebApiBoardFactKind = 'endpoint' | 'auth' | 'role' | 'tech' | 'config' | 'data' | 'other'
export type WebApiBoardSource = 'traffic' | 'model'

export interface WebApiBoardCounts {
  endpoints: number
  traffic_endpoints: number
  model_endpoints: number
  untested_endpoints: number
  /** 有效（active）事实数。 */
  facts: number
  confirmed_facts: number
  oracles: Record<WebApiBoardOracleStatus, number>
}

export interface WebApiBoardDigest {
  task_id: string
  digest: string
  counts: WebApiBoardCounts
}

export interface WebApiBoardEndpoint {
  id: string
  endpoint_key: string
  method: string
  origin: string
  path: string
  params: string[]
  status_codes: number[]
  /** none / with / both；模型补充且从未请求为空串。 */
  auth_seen: string
  request_count: number
  first_seen_at: string
  last_seen_at: string
  source: WebApiBoardSource
  note: string
  /** 引用该端点的 Oracle 编号（任意状态）。 */
  oracles: string[]
}

export interface WebApiBoardFact {
  id: string
  kind: WebApiBoardFactKind
  subject: string
  statement: string
  confidence: 'confirmed' | 'inferred'
  evidence_ids: string[]
  status: 'active' | 'retracted'
  session_id: string
  turn_id: string
  call_id: string
  created_at: string
  updated_at: string
}

export interface WebApiBoardOracleEvent {
  /** 创建事件为空串。 */
  from: string
  to: string
  actor: string
  session_id: string
  at: string
  summary: string
}

export interface WebApiBoardOracle {
  id: string
  number: string
  question: string
  category: WebApiBoardCategory
  endpoint_keys: string[]
  priority: WebApiBoardPriority
  status: WebApiBoardOracleStatus
  origin: 'model' | 'user' | string
  claimed_session_id: string
  result_summary: string
  evidence_ids: string[]
  finding_id: string
  /** WSC-000001；无关联为空串。 */
  finding_number: string
  created_at: string
  updated_at: string
  events?: WebApiBoardOracleEvent[]
}

export interface WebApiBoardList<T> {
  items: T[]
  has_more: boolean
  next_cursor: string | null
}

export interface WebApiBoardOracleInput {
  question: string
  category: WebApiBoardCategory
  endpoint_keys: string[]
  priority: WebApiBoardPriority
}

export async function getWebApiBoard(taskId: string): Promise<WebApiBoardDigest> {
  return apiJSON<WebApiBoardDigest>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/board`, 'Failed to load board')
}

export async function listWebApiBoardEndpoints(taskId: string, params: {
  q?: string
  untested?: boolean
  limit?: number
  cursor?: string
} = {}): Promise<WebApiBoardList<WebApiBoardEndpoint>> {
  const query = new URLSearchParams()
  if (params.q) query.set('q', params.q)
  if (params.untested) query.set('untested', '1')
  query.set('limit', String(params.limit ?? 50))
  if (params.cursor) query.set('cursor', params.cursor)
  return apiJSON<WebApiBoardList<WebApiBoardEndpoint>>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/board/endpoints?${query.toString()}`, 'Failed to load board endpoints')
}

export async function listWebApiBoardFacts(taskId: string, params: {
  kind?: WebApiBoardFactKind
  status?: 'active' | 'retracted'
  limit?: number
  cursor?: string
} = {}): Promise<WebApiBoardList<WebApiBoardFact>> {
  const query = new URLSearchParams()
  if (params.kind) query.set('kind', params.kind)
  if (params.status) query.set('status', params.status)
  query.set('limit', String(params.limit ?? 50))
  if (params.cursor) query.set('cursor', params.cursor)
  return apiJSON<WebApiBoardList<WebApiBoardFact>>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/board/facts?${query.toString()}`, 'Failed to load board facts')
}

export async function listWebApiBoardOracles(taskId: string, params: {
  status?: WebApiBoardOracleStatus
  category?: WebApiBoardCategory
  limit?: number
  cursor?: string
} = {}): Promise<WebApiBoardList<WebApiBoardOracle>> {
  const query = new URLSearchParams()
  if (params.status) query.set('status', params.status)
  if (params.category) query.set('category', params.category)
  query.set('limit', String(params.limit ?? 50))
  if (params.cursor) query.set('cursor', params.cursor)
  return apiJSON<WebApiBoardList<WebApiBoardOracle>>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/board/oracles?${query.toString()}`, 'Failed to load board oracles')
}

// --- 子 Agent 并行探索（M5a 后端，契约 native/SUBAGENT.md）--------------------
// 研究会话派出的命名子 Agent oracle_explorer 认领一个 Oracle 并行探索；子会话
// 转录不在 Hub 保存，平台可追溯的是 /subsessions、请求、候选与黑板事件。
export interface WebApiSubsessionOracle {
  id: string
  /** 后端为 "O-001" 形态的字符串。 */
  number: string
  question: string
  status: WebApiBoardOracleStatus
}

export interface WebApiSubsession {
  session_id: string
  oracle: WebApiSubsessionOracle | null
  status: 'running' | 'ended'
  /** 上游 StopReason：completed/canceled/error/interrupted… */
  stop_reason: string
  error: string
  created_at: string
  ended_at: string | null
  request_count: number
  finding_count: number
}

export async function listWebApiSubsessions(taskId: string): Promise<{ items: WebApiSubsession[] }> {
  return apiJSON<{ items: WebApiSubsession[] }>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/subsessions`, 'Failed to load subagents', { cache: 'no-store' })
}

export async function createWebApiBoardOracle(taskId: string, body: WebApiBoardOracleInput): Promise<{ oracle: WebApiBoardOracle; created: boolean }> {
  return apiJSON<{ oracle: WebApiBoardOracle; created: boolean }>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/board/oracles`, 'Failed to propose oracle', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export async function updateWebApiBoardOracle(taskId: string, number: string, body: {
  priority?: WebApiBoardPriority
  status?: 'open' | 'dropped'
}): Promise<{ oracle: WebApiBoardOracle; changed: boolean }> {
  return apiJSON<{ oracle: WebApiBoardOracle; changed: boolean }>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/board/oracles/${encodeURIComponent(number)}`, 'Failed to update oracle', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export async function listWebApiEvidence(
  taskId: string,
  limit = 100,
  cursor?: string,
  opts?: { mask?: boolean },
): Promise<WebApiEvidenceList> {
  const query = new URLSearchParams()
  query.set('limit', String(limit))
  if (cursor) query.set('cursor', cursor)
  if (opts?.mask) query.set('mask', '1')
  return apiJSON<WebApiEvidenceList>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/evidence?${query.toString()}`, 'Failed to load traffic')
}

export async function getWebApiEvidence(
  taskId: string,
  evidenceId: string,
  opts?: { mask?: boolean },
): Promise<WebApiEvidenceDetail> {
  const suffix = opts?.mask ? '?mask=1' : ''
  return apiJSON<WebApiEvidenceDetail>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/evidence/${encodeURIComponent(evidenceId)}${suffix}`, 'Failed to load evidence')
}

// M5b：身份对比记录（新到旧）。带 evidenceId 时按原请求过滤；不带时返回任务全部（变体反查用）。
export async function listWebApiAuthzCompares(taskId: string, evidenceId?: string): Promise<{ items: WebApiAuthzCompare[] }> {
  const query = new URLSearchParams()
  if (evidenceId) query.set('evidence_id', evidenceId)
  const suffix = query.toString()
  return apiJSON<{ items: WebApiAuthzCompare[] }>(
    `/api/webapi/tasks/${encodeURIComponent(taskId)}/authz-compares${suffix ? `?${suffix}` : ''}`,
    'Failed to load identity comparisons',
    { cache: 'no-store' },
  )
}

// --- Web/API 候选（finding）只读接口 ---
// 全部只读；候选是未确认主张，前端不提供确认/编辑/报告/复测入口。

export interface WebApiFindingSummary {
  finding_id: string
  number: string
  title: string
  status: string
  revision: number
  revision_id: string
  created_at: string
  updated_at: string
}

export interface WebApiFindingSource {
  session_id: string
  turn_id: string
  call_id: string
}

export interface WebApiFindingStep {
  text: string
  evidence_indices: number[]
}

export interface WebApiFindingEvidenceInput {
  evidence_id: string
  relation: string
}

export interface WebApiFindingContent {
  title: string
  description: string
  steps: WebApiFindingStep[]
  remediation?: string
  limitations?: string
  evidence: WebApiFindingEvidenceInput[]
}

export interface WebApiFindingEvidenceSnapshot {
  evidence_id: string
  relation: string
  session_id: string
  turn_id: string
  call_id: string
  method: string
  url: string
  request_fingerprint: string
  status: string
  reason: string
  sent: boolean | null
  http_status?: number
  preview_truncated: boolean
  capture_truncated: boolean
  created_at: string
  completed_at?: string
  flow_sha256?: string
}

export interface WebApiFindingDetail {
  finding_id: string
  task_id: string
  number: string
  status: string
  revision: number
  revision_id: string
  revision_created_at: string
  created_at?: string
  updated_at?: string
  content: WebApiFindingContent
  evidence: WebApiFindingEvidenceSnapshot[]
  source: WebApiFindingSource
}

export interface WebApiFindingList {
  task_id: string
  items: WebApiFindingSummary[]
  limit: number
  has_more: boolean
  next_cursor: string | null
}

// limit/cursor 分页同 evidence；finding 默认 limit 为 25、上限 50。
export async function listWebApiFindings(taskId: string, limit = 25, cursor?: string): Promise<WebApiFindingList> {
  const query = new URLSearchParams()
  query.set('limit', String(limit))
  if (cursor) query.set('cursor', cursor)
  return apiJSON<WebApiFindingList>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/findings?${query.toString()}`, 'Failed to load findings')
}

export async function getWebApiFinding(taskId: string, findingId: string): Promise<WebApiFindingDetail> {
  return apiJSON<WebApiFindingDetail>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/findings/${encodeURIComponent(findingId)}`, 'Failed to load finding')
}

// --- Web/API 独立验证（候选的模型判读，不确认候选、不覆盖原候选） ---
// 保存只写本地状态；后续 Open/Run 仍由原生 SessionService 驱动。摘要 verdict 可空，
// 结果为空不代表执行失败，也不等于尚未启动 —— 执行状态只用原生 session/事件。

export interface WebApiVerificationSummary {
  verification_id: string
  task_id: string
  finding_id: string
  source_revision_id: string
  session_id: string
  created_at: string
  verdict?: string
}

export interface WebApiVerificationEvidenceInput {
  evidence_id: string
  relation: string
}

export interface WebApiVerificationPayload {
  verdict: string
  method: string
  preconditions?: string
  summary: string
  limitations?: string
  evidence?: WebApiVerificationEvidenceInput[]
}

export interface WebApiVerificationResult {
  session_id: string
  turn_id: string
  call_id: string
  payload: WebApiVerificationPayload
  evidence: WebApiFindingEvidenceSnapshot[]
  created_at: string
}

export interface WebApiVerificationDetail {
  verification_id: string
  task_id: string
  finding_id: string
  source_revision_id: string
  session_id: string
  created_at: string
  source: WebApiFindingDetail
  result: WebApiVerificationResult | null
}

export interface WebApiVerificationList {
  task_id: string
  items: WebApiVerificationSummary[]
  limit: number
  has_more: boolean
  next_cursor: string | null
}

export interface WebApiVerificationSaveResult {
  verification: WebApiVerificationSummary
  created: boolean
  idempotent: boolean
}

// 同 ID 同来源由后端幂等返回同一验证；异来源 409、错归属 404、新提交过期来源 409。
// 客户端只生成一次 verification ID，重试复用同一 ID，不自行换 revision/换 ID 提交。
export async function saveWebApiVerification(taskID: string, findingID: string, verificationID: string, sourceRevisionID: string): Promise<WebApiVerificationSaveResult> {
  return apiJSON<WebApiVerificationSaveResult>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/findings/${encodeURIComponent(findingID)}/verifications/${encodeURIComponent(verificationID)}`,
    'Failed to save verification',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ source_revision_id: sourceRevisionID }),
    },
  )
}

export async function getWebApiVerification(taskID: string, findingID: string, verificationID: string): Promise<WebApiVerificationDetail> {
  return apiJSON<WebApiVerificationDetail>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/findings/${encodeURIComponent(findingID)}/verifications/${encodeURIComponent(verificationID)}`,
    'Failed to load verification',
  )
}

export async function listWebApiVerifications(taskID: string, findingID: string, limit = 25, cursor?: string): Promise<WebApiVerificationList> {
  const query = new URLSearchParams()
  query.set('limit', String(limit))
  if (cursor) query.set('cursor', cursor)
  return apiJSON<WebApiVerificationList>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/findings/${encodeURIComponent(findingID)}/verifications?${query.toString()}`,
    'Failed to load verifications',
  )
}

// 切换/刷新原生验证 session 时恢复其固定来源与结果；不存在 404。声明为冻结接口，
// 当前前端仍把验证结果放在父任务候选工作面内，不在原生执行页另造验证壳。
export async function getWebApiVerificationBySession(sessionID: string): Promise<WebApiVerificationDetail> {
  return apiJSON<WebApiVerificationDetail>(
    `/api/webapi/verifications?session_id=${encodeURIComponent(sessionID)}`,
    'Failed to load verification by session',
  )
}

// --- Web/API 复测（固定修订复测是否仍可复现；结论不可变，不覆盖原候选/验证/报告） ---
// 类型与验证一一对应；conclusion 词汇不同（reproducible/fixed_in_scope/inconclusive）。

export interface WebApiRetestSummary {
  retest_id: string
  task_id: string
  finding_id: string
  source_revision_id: string
  session_id: string
  created_at: string
  conclusion?: string
}

export interface WebApiRetestPayload {
  conclusion: string
  method: string
  preconditions?: string
  summary: string
  limitations?: string
  evidence?: WebApiVerificationEvidenceInput[]
}

export interface WebApiRetestResult {
  session_id: string
  turn_id: string
  call_id: string
  payload: WebApiRetestPayload
  evidence: WebApiFindingEvidenceSnapshot[]
  created_at: string
}

export interface WebApiRetestDetail {
  retest_id: string
  task_id: string
  finding_id: string
  source_revision_id: string
  session_id: string
  created_at: string
  conclusion?: string
  source: WebApiFindingDetail
  result: WebApiRetestResult | null
}

export interface WebApiRetestList {
  task_id: string
  items: WebApiRetestSummary[]
  limit: number
  has_more: boolean
  next_cursor: string | null
}

export interface WebApiRetestSaveResult {
  retest: WebApiRetestSummary
  created: boolean
  idempotent: boolean
}

// 同 ID 同来源由后端幂等返回同一复测；异来源 409、固定非当前修订 409、错归属 404。
// 客户端只生成一次 retest ID，重试复用同一 ID，不自行换 revision/换 ID 提交。
export async function saveWebApiRetest(taskID: string, findingID: string, retestID: string, sourceRevisionID: string): Promise<WebApiRetestSaveResult> {
  return apiJSON<WebApiRetestSaveResult>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/findings/${encodeURIComponent(findingID)}/retests/${encodeURIComponent(retestID)}`,
    'Failed to save retest',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ source_revision_id: sourceRevisionID }),
    },
  )
}

export async function getWebApiRetest(taskID: string, findingID: string, retestID: string): Promise<WebApiRetestDetail> {
  return apiJSON<WebApiRetestDetail>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/findings/${encodeURIComponent(findingID)}/retests/${encodeURIComponent(retestID)}`,
    'Failed to load retest',
  )
}

export async function listWebApiRetests(taskID: string, findingID: string, limit = 25, cursor?: string): Promise<WebApiRetestList> {
  const query = new URLSearchParams()
  query.set('limit', String(limit))
  if (cursor) query.set('cursor', cursor)
  return apiJSON<WebApiRetestList>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/findings/${encodeURIComponent(findingID)}/retests?${query.toString()}`,
    'Failed to load retests',
  )
}

// 复测 session 反查详情；不存在 404。App 按 session 恢复复测条只用真实 404 判无复测。
export async function getWebApiRetestBySession(sessionID: string): Promise<WebApiRetestDetail> {
  return apiJSON<WebApiRetestDetail>(
    `/api/webapi/retests?session_id=${encodeURIComponent(sessionID)}`,
    'Failed to load retest by session',
  )
}

// --- Web/API 报告（固定若干候选修订 + 可附验证；草稿由报告会话模型写入，不可变） ---
// 报告 session 不被 taskForSession 解析，只能经 GET /api/webapi/reports?session_id= 反查。

export interface WebApiReportItem {
  finding_id: string
  revision_id: string
  verification_ids: string[]
}

export interface WebApiReportSummary {
  report_id: string
  task_id: string
  session_id: string
  created_at: string
  items: WebApiReportItem[]
  has_draft: boolean
  // 后端另片补齐：有草稿=草稿标题，无草稿=空串；旧后端缺省 undefined。
  title?: string
}

export interface WebApiReportVerification {
  verification_id: string
  task_id: string
  finding_id: string
  source_revision_id: string
  session_id: string
  created_at: string
  verdict?: string
  result: WebApiVerificationResult | null
}

export interface WebApiReportItemDetail extends WebApiReportItem {
  revision: WebApiFindingDetail | null
  verifications: WebApiReportVerification[]
}

export interface WebApiReportDraftItem {
  finding_id: string
  description: string
  reproduction: string
  remediation?: string
  evidence_ids: string[]
}

export interface WebApiReportDraftPayload {
  title: string
  overview: string
  items: WebApiReportDraftItem[]
  limitations?: string
}

export interface WebApiReportDraftView {
  session_id: string
  turn_id: string
  call_id: string
  payload: WebApiReportDraftPayload
  created_at: string
}

export interface WebApiReportDetail {
  report_id: string
  task_id: string
  session_id: string
  created_at: string
  has_draft: boolean
  items: WebApiReportItemDetail[]
  draft: WebApiReportDraftView | null
}

export interface WebApiReportSaveResult {
  report: WebApiReportSummary
  created: boolean
  idempotent: boolean
}

export interface WebApiReportList {
  task_id: string
  items: WebApiReportSummary[]
  limit: number
  has_more: boolean
  next_cursor: string | null
}

// 客户端一次生成 report ID，重试复用；同 ID 同 items 后端幂等回放（201/200），
// 同 ID 异 items 409、新建固定非当前修订 409（区分提示重读），错归属 404。
// 前端不自行覆盖 items 或换 ID 重提交。
export async function saveWebApiReport(taskID: string, reportID: string, items: WebApiReportItem[]): Promise<WebApiReportSaveResult> {
  return apiJSON<WebApiReportSaveResult>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/reports/${encodeURIComponent(reportID)}`,
    'Failed to save report',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ items }),
    },
  )
}

export async function listWebApiReports(taskID: string, limit = 25, cursor?: string): Promise<WebApiReportList> {
  const query = new URLSearchParams()
  query.set('limit', String(limit))
  if (cursor) query.set('cursor', cursor)
  return apiJSON<WebApiReportList>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/reports?${query.toString()}`,
    'Failed to load reports',
  )
}

export async function getWebApiReport(taskID: string, reportID: string): Promise<WebApiReportDetail> {
  return apiJSON<WebApiReportDetail>(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/reports/${encodeURIComponent(reportID)}`,
    'Failed to load report',
  )
}

// 报告 session 反查详情；不存在 404。App 按 session 恢复报告条只用真实 404 判无报告。
export async function getWebApiReportBySession(sessionID: string): Promise<WebApiReportDetail> {
  return apiJSON<WebApiReportDetail>(
    `/api/webapi/reports?session_id=${encodeURIComponent(sessionID)}`,
    'Failed to load report by session',
  )
}

// 导出由已存草稿+固定来源确定性渲染；无草稿 409。取文本由调用方落 Blob 下载。
export async function exportWebApiReport(taskID: string, reportID: string, format: 'markdown' | 'json'): Promise<string> {
  const res = await authenticatedFetch(
    `/api/webapi/tasks/${encodeURIComponent(taskID)}/reports/${encodeURIComponent(reportID)}/export?format=${format}`,
  )
  if (!res.ok) throw new APIError(await errorMessage(res, 'Failed to export report'), res.status)
  return res.text()
}

// --- Web/API 用量（只读 token 汇总；上游无价格配置，不含费用） ---
// 契约见 native/USAGE.md：task_id 省略/空 = 全局汇总（返回 by_task）；
// 指定 task 返回 by_session；from/to 为 Hub 本地时区日期闭区间（YYYY-MM-DD）。
// 损坏事件行计入 skipped_events，不进任何汇总；日期按响应 timezone 字段。

export interface WebApiUsageTotals {
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  calls: number
}

export interface WebApiUsageDay extends WebApiUsageTotals {
  day: string
}

export interface WebApiUsageModel extends WebApiUsageTotals {
  model: string
}

export interface WebApiUsageTask extends WebApiUsageTotals {
  task_id: string
  title: string
}

export interface WebApiUsageSession extends WebApiUsageTotals {
  session_id: string
  role: string
  finding_id: string
  report_id: string
}

export interface WebApiUsageReport {
  timezone: string
  totals: WebApiUsageTotals
  by_day: WebApiUsageDay[]
  by_model: WebApiUsageModel[]
  by_task?: WebApiUsageTask[]
  by_session?: WebApiUsageSession[]
  skipped_events: number
}

export async function getWebApiUsage(options?: { taskId?: string; from?: string; to?: string }): Promise<WebApiUsageReport> {
  const query = new URLSearchParams()
  if (options?.taskId) query.set('task_id', options.taskId)
  if (options?.from) query.set('from', options.from)
  if (options?.to) query.set('to', options.to)
  const suffix = query.toString()
  return apiJSON<WebApiUsageReport>(`/api/webapi/usage${suffix ? `?${suffix}` : ''}`, 'Failed to load usage')
}

export function webApiReportExportFilename(reportID: string, format: 'markdown' | 'json'): string {
  return `report-${reportID.slice(0, 8)}.${format === 'markdown' ? 'md' : 'json'}`
}

// --- 资产登记与周期计划（C1/C2a/C2b 契约见 native/ASSETS.md） ---
// 资产是服务端实体；任何响应都不含 secret，密码只在保存时单向写入。

export interface WebApiAssetCredential {
  id: string
  label: string
  username: string
  has_secret: boolean
  note: string
  updated_at: string
}

export interface WebApiAsset {
  id: string
  name: string
  owner: string
  environment: string
  known_info: string
  objective: string
  origins: string[]
  forbidden: string
  scope: string
  approval: string
  archived: boolean
  created_at: string
  updated_at: string
  credentials: WebApiAssetCredential[]
  // M1 默认研究场景；旧响应缺省时按默认场景处理。
  scenario?: WebApiScenario
}

export interface WebApiAssetTaskItem {
  task_id: string
  objective: string
  initial_session_id: string
  created_at: string
  finding_count: number
}

export interface WebApiFollowupCounts {
  pending: number
  launched: number
  failed: number
  skipped: number
}

export interface WebApiRunFollowups {
  state: 'waiting' | 'not_planned' | 'in_progress' | 'completed'
  completed: boolean
  auto_verify: boolean | null
  auto_retest: boolean | null
  planned_at: string | null
  completed_at: string | null
  verify: WebApiFollowupCounts
  retest: WebApiFollowupCounts
}

export interface WebApiScheduleRun {
  id: string
  asset_id: string
  scheduled_for: string
  task_id: string
  status: 'launched' | 'failed' | 'skipped'
  error: string
  created_at: string
  followups: WebApiRunFollowups | null
}

export interface WebApiSchedule {
  asset_id: string
  cron: string
  enabled: boolean
  auto_verify: boolean
  auto_retest: boolean
  next_run_at: string | null
  upcoming: string[]
  timezone: string
  updated_at: string
  last_run: WebApiScheduleRun | null
}

export interface WebApiFollowupItem {
  id: string
  kind: 'verify' | 'retest'
  task_id: string
  finding_id: string
  finding_number: string
  finding_title: string
  target_id: string
  session_id: string | null
  status: 'pending' | 'launched' | 'failed' | 'skipped'
  error: string
  verdict: string | null
  conclusion: string | null
  created_at: string
}

export interface WebApiAssetSaveInput {
  name: string
  owner: string
  environment: string
  known_info: string
  objective: string
  origins: string[]
  forbidden: string
  scope?: string
  approval?: string
  archived?: boolean
  // M1 资产默认研究场景；未提供＝保留当前值，done_criteria 空串＝场景默认。
  scenario?: string
  done_criteria?: string
}

export interface WebApiAssetCredentialInput {
  label: string
  username: string
  secret?: string
  note: string
}

export interface WebApiLaunchProgress {
  task_created: boolean
  session_opened: boolean
  turn_started: boolean
}

export interface WebApiAssetLaunchResult {
  asset_id: string
  task: WebApiTask
  session_id: string
  node_id: string
  turn_id: string
  progress: WebApiLaunchProgress
  created: boolean
  idempotent: boolean
}

// 发起失败带阶段与已完成进度；重试必须复用同一 task_id 才可续完。
export class AssetLaunchError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly stage: string,
    public readonly progress: WebApiLaunchProgress | null,
  ) {
    super(message)
    this.name = 'AssetLaunchError'
  }
}

export interface WebApiScheduleRunList {
  items: WebApiScheduleRun[]
  has_more: boolean
  next_cursor: string | null
}

export async function listWebApiAssets(archived: boolean): Promise<WebApiAsset[]> {
  const body = await apiJSON<{ assets: WebApiAsset[] }>(
    `/api/webapi/assets?archived=${archived ? 1 : 0}`,
    'Failed to load assets',
  )
  return body.assets
}

export async function getWebApiAsset(id: string): Promise<WebApiAsset> {
  return apiJSON<WebApiAsset>(`/api/webapi/assets/${encodeURIComponent(id)}`, 'Failed to load asset')
}

export async function saveWebApiAsset(id: string, input: WebApiAssetSaveInput): Promise<WebApiAsset> {
  return apiJSON<WebApiAsset>(`/api/webapi/assets/${encodeURIComponent(id)}`, 'Failed to save asset', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
}

// secret 为空串/未提供时不放进 body：后端按“保留原值”处理，前端永不多写空密码。
export async function saveWebApiCredential(assetId: string, credentialId: string, input: WebApiAssetCredentialInput): Promise<WebApiAsset> {
  const body: WebApiAssetCredentialInput = { label: input.label, username: input.username, note: input.note }
  if (input.secret) body.secret = input.secret
  return apiJSON<WebApiAsset>(`/api/webapi/assets/${encodeURIComponent(assetId)}/credentials/${encodeURIComponent(credentialId)}`, 'Failed to save credential', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export async function deleteWebApiCredential(assetId: string, credentialId: string): Promise<WebApiAsset> {
  return apiJSON<WebApiAsset>(`/api/webapi/assets/${encodeURIComponent(assetId)}/credentials/${encodeURIComponent(credentialId)}`, 'Failed to delete credential', {
    method: 'DELETE',
  })
}

export async function listWebApiAssetTasks(assetId: string): Promise<WebApiAssetTaskItem[]> {
  const body = await apiJSON<{ tasks: WebApiAssetTaskItem[] }>(
    `/api/webapi/assets/${encodeURIComponent(assetId)}/tasks`,
    'Failed to load asset tasks',
  )
  return body.tasks
}

export async function launchWebApiAssetTask(assetId: string, taskId: string, options: { objective?: string; scope?: string; approval?: string }): Promise<WebApiAssetLaunchResult> {
  const res = await authenticatedFetch(`/api/webapi/assets/${encodeURIComponent(assetId)}/tasks/${encodeURIComponent(taskId)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(options),
  })
  if (!res.ok) {
    let body: { error?: string; stage?: string; progress?: WebApiLaunchProgress } | null = null
    try { body = await res.json() } catch { /* 非 JSON 错误体按通用文案 */ }
    throw new AssetLaunchError(body?.error || 'Failed to launch task', res.status, body?.stage || '', body?.progress ?? null)
  }
  return res.json()
}

// 未设置计划返回 null（404），其余错误照常抛出。
export async function getWebApiSchedule(assetId: string): Promise<WebApiSchedule | null> {
  try {
    return await apiJSON<WebApiSchedule>(`/api/webapi/assets/${encodeURIComponent(assetId)}/schedule`, 'Failed to load schedule')
  } catch (err) {
    if (err instanceof APIError && err.status === 404) return null
    throw err
  }
}

export async function saveWebApiSchedule(assetId: string, input: { cron: string; enabled: boolean; auto_verify: boolean; auto_retest: boolean }): Promise<WebApiSchedule> {
  return apiJSON<WebApiSchedule>(`/api/webapi/assets/${encodeURIComponent(assetId)}/schedule`, 'Failed to save schedule', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
}

export async function deleteWebApiSchedule(assetId: string): Promise<void> {
  await apiJSON<{ asset_id: string; deleted: boolean }>(`/api/webapi/assets/${encodeURIComponent(assetId)}/schedule`, 'Failed to delete schedule', {
    method: 'DELETE',
  })
}

export async function listWebApiSchedules(): Promise<WebApiSchedule[]> {
  const body = await apiJSON<{ schedules: WebApiSchedule[] }>('/api/webapi/schedules', 'Failed to load schedules')
  return body.schedules
}

export async function listWebApiScheduleRuns(assetId: string, cursor?: string): Promise<WebApiScheduleRunList> {
  const query = new URLSearchParams()
  query.set('limit', '20')
  if (cursor) query.set('cursor', cursor)
  return apiJSON<WebApiScheduleRunList>(`/api/webapi/assets/${encodeURIComponent(assetId)}/schedule/runs?${query.toString()}`, 'Failed to load schedule runs')
}

export async function listWebApiScheduleFollowups(runId: string): Promise<WebApiFollowupItem[]> {
  const body = await apiJSON<{ items: WebApiFollowupItem[] }>(
    `/api/webapi/schedule-runs/${encodeURIComponent(runId)}/followups`,
    'Failed to load followups',
  )
  return body.items
}

// --- 资产漏洞库（C3）：状态读时派生，前端一次取全量后本地筛选 ---
export type WebApiVulnStatus = 'new' | 'present' | 'pending' | 'unconfirmed' | 'fixed'

export interface WebApiVulnMember {
  task_id: string
  finding_id: string
  number: string
  title: string
  link: 'auto' | 'manual'
  latest_verdict: string | null
  latest_retest: string | null
  created_at: string
  initial_session_id: string
}

export interface WebApiVulnEvent {
  kind: 'verify' | 'retest'
  value: string
  at: string
  task_id: string
  finding_id: string
}

export interface WebApiVuln {
  id: string
  /** K1：稳定编号 V-XXXXXXXX。 */
  code: string
  title: string
  status: WebApiVulnStatus
  endpoint_key: string
  first_seen_at: string
  last_confirmed_at: string | null
  fixed_at: string | null
  last_event: WebApiVulnEvent | null
  member_count: number
  task_count: number
  members: WebApiVulnMember[]
  /** V1 人工处置视图（后端 HTTP 层填入）。 */
  triage?: WebApiVulnTriage | null
  /** K1：成员中最新带建议等级的 supported 验证；空串 = 无建议。 */
  suggested_severity?: WebApiVulnSeverity
}

export interface WebApiVulnList {
  asset_id: string
  latest_task_id: string | null
  counts: Record<WebApiVulnStatus, number>
  vulns: WebApiVuln[]
}

export async function listWebApiAssetVulns(assetId: string): Promise<WebApiVulnList> {
  return apiJSON<WebApiVulnList>(
    `/api/webapi/assets/${encodeURIComponent(assetId)}/vulns`,
    'Failed to load vulnerabilities',
  )
}

export async function saveWebApiVulnTitle(assetId: string, vulnId: string, title: string): Promise<WebApiVuln> {
  return apiJSON<WebApiVuln>(
    `/api/webapi/assets/${encodeURIComponent(assetId)}/vulns/${encodeURIComponent(vulnId)}`,
    'Failed to save title',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title }),
    },
  )
}

export async function moveWebApiVulnMember(assetId: string, vulnId: string, findingId: string): Promise<WebApiVuln> {
  return apiJSON<WebApiVuln>(
    `/api/webapi/assets/${encodeURIComponent(assetId)}/vulns/${encodeURIComponent(vulnId)}/members/${encodeURIComponent(findingId)}`,
    'Failed to move candidate',
    {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    },
  )
}

export async function mergeWebApiVulns(assetId: string, intoId: string, fromId: string): Promise<WebApiVuln> {
  return apiJSON<WebApiVuln>(
    `/api/webapi/assets/${encodeURIComponent(assetId)}/vulns/${encodeURIComponent(intoId)}/merge`,
    'Failed to merge vulnerabilities',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from: fromId }),
    },
  )
}

/** 会话归属批量查询结果条目；只含后端能识别的会话。 */
export interface WebApiSessionKindItem {
  kind: 'task' | 'verification' | 'report' | 'retest'
  task_id: string
  task_session_id: string
  task_objective: string
  number: string
  title: string
  created_at: string
}

/** 一次查询 1–500 个会话，调用方保证分批；返回仅含能识别的会话。 */
export async function getWebApiSessionKinds(sessionIds: string[]): Promise<Record<string, WebApiSessionKindItem>> {
  const body = await apiJSON<{ items?: Record<string, WebApiSessionKindItem> }>(
    '/api/webapi/session-kinds',
    'Failed to load session kinds',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_ids: sessionIds }),
    },
  )
  return body.items || {}
}

// --- 全工具出口（B2/M5c）：终端、浏览器与爬虫经代理的请求记录（只读） ---
// 与 webapi_requests 的结构化证据不同：这是工具进程的实际出口流量（HTTPS 为
// MITM 解密后的内层请求，CONNECT 本身不单独记录）；拒绝记录在连接目标前写入。

export interface WebApiEgressItem {
  id: string
  session_id: string
  turn_id: string
  call_id: string
  tool: string
  method: string
  url: string
  decision: 'allowed' | 'denied'
  sent: boolean | null
  http_status: number | null
  error: string
  created_at: string
  completed_at: string
}

export interface WebApiEgressList {
  items: WebApiEgressItem[]
  has_more: boolean
  next_cursor: string | null
}

export interface WebApiEgressDetail {
  summary: WebApiEgressItem
  host: string
  flow: unknown
  flow_protobuf_base64: string
  capture: {
    request_bytes: number
    response_bytes: number
    preview_limit: number
    preview_truncated: boolean
    note: string
  }
}

export async function listWebApiEgress(taskId: string, limit = 100, cursor?: string, decision?: 'allowed' | 'denied'): Promise<WebApiEgressList> {
  const query = new URLSearchParams()
  query.set('limit', String(limit))
  if (cursor) query.set('cursor', cursor)
  if (decision) query.set('decision', decision)
  return apiJSON<WebApiEgressList>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/egress?${query.toString()}`, 'Failed to load tool egress')
}

export async function getWebApiEgress(taskId: string, flowId: string): Promise<WebApiEgressDetail> {
  return apiJSON<WebApiEgressDetail>(`/api/webapi/tasks/${encodeURIComponent(taskId)}/egress/${encodeURIComponent(flowId)}`, 'Failed to load egress record')
}

// 证据图片：后端按已保存的 Flow 原件确定性渲染 PNG。返回同源相对路径，
// 浏览器以 Cookie 鉴权，可直接用于 <img src> 与下载链接。mask 默认开启。
export function webApiEvidenceImageURL(taskId: string, evidenceId: string, opts?: { mask?: boolean; marks?: string[] }): string {
  const mask = opts?.mask === false ? '0' : '1'
  const params = new URLSearchParams({ mask })
  for (const mark of opts?.marks || []) {
    if (mark) params.append('mark', mark)
  }
  return `/api/webapi/tasks/${encodeURIComponent(taskId)}/evidence/${encodeURIComponent(evidenceId)}/image.png?${params.toString()}`
}

export function webApiEgressImageURL(taskId: string, flowId: string, opts?: { mask?: boolean }): string {
  const mask = opts?.mask === false ? '0' : '1'
  return `/api/webapi/tasks/${encodeURIComponent(taskId)}/egress/${encodeURIComponent(flowId)}/image.png?mask=${mask}`
}

// --- 企业微信推送（D2）：配置、测试与通知记录（契约 native/NOTIFY.md） ---
export interface WebApiWecomSettings {
  enabled: boolean
  has_webhook: boolean
  webhook_hint: string
  require_confirm: boolean
  /** 运营通知：待审批提醒 / 计划运行失败 / 每周摘要（不受 require_confirm 影响）。 */
  notify_approval: boolean
  notify_schedule_failed: boolean
  notify_weekly_digest: boolean
  base_url: string
  updated_at: string | null
}

/** webhook 省略或 "" 保留原值，"-" 清除；运营通知三项省略保留原值。 */
export interface WebApiWecomSaveInput {
  webhook?: string
  enabled: boolean
  require_confirm: boolean
  notify_approval?: boolean
  notify_schedule_failed?: boolean
  notify_weekly_digest?: boolean
  base_url: string
}

export interface WebApiWecomTestResult {
  ok: boolean
  errcode: number
  errmsg: string
}

export async function getWebApiWecomSettings(): Promise<WebApiWecomSettings> {
  return apiJSON<WebApiWecomSettings>('/api/webapi/notify/wecom', 'Failed to load WeCom settings', { cache: 'no-store' })
}

export async function saveWebApiWecomSettings(body: WebApiWecomSaveInput): Promise<WebApiWecomSettings> {
  return apiJSON<WebApiWecomSettings>('/api/webapi/notify/wecom', 'Failed to save WeCom settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export async function testWebApiWecom(): Promise<WebApiWecomTestResult> {
  return apiJSON<WebApiWecomTestResult>('/api/webapi/notify/wecom/test', 'Failed to send test message', { method: 'POST' })
}

export type WebApiNotificationStatus = 'pending_confirm' | 'queued' | 'sending' | 'sent' | 'failed' | 'dismissed'

export type WebApiNotificationKind =
  | 'new' | 'regression' | 'manual'
  | 'approval_pending' | 'schedule_failed' | 'weekly_digest'

export interface WebApiNotification {
  id: string
  kind: WebApiNotificationKind
  status: WebApiNotificationStatus
  dedupe_key: string
  task_id: string
  finding_id: string
  finding_number: string
  verification_id: string
  verification_session_id: string
  /** 仅 approval_pending 有值（链接所用研究会话）；其余为空串。 */
  session_id?: string
  vuln_id: string | null
  title: string
  asset: { id: string; name: string } | null
  attempts: number
  last_error: string
  next_attempt_at: string | null
  created_at: string
  sent_at: string | null
}

export interface WebApiNotificationList {
  items: WebApiNotification[]
  has_more: boolean
  next_cursor: string | null
}

export async function listWebApiNotifications(params: {
  status?: WebApiNotificationStatus
  limit?: number
  cursor?: string
} = {}): Promise<WebApiNotificationList> {
  const query = new URLSearchParams()
  if (params.status) query.set('status', params.status)
  if (params.limit) query.set('limit', String(params.limit))
  if (params.cursor) query.set('cursor', params.cursor)
  const suffix = query.toString()
  return apiJSON<WebApiNotificationList>(`/api/webapi/notifications${suffix ? `?${suffix}` : ''}`, 'Failed to load push records')
}

export async function actWebApiNotification(id: string, action: 'confirm' | 'dismiss' | 'retry'): Promise<WebApiNotification> {
  return apiJSON<WebApiNotification>(
    `/api/webapi/notifications/${encodeURIComponent(id)}/${action}`,
    'Failed to update push record',
    { method: 'POST' },
  )
}
