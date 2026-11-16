import type {
  ExplorationInput,
  ExplorationStart,
  Mission,
  HTTPRecordPage,
  HTTPRecordDetail,
  OperationReceipt,
  Snapshot,
  SystemInfo,
  Thread,
} from './types.ts'

export type SessionChangeReason = 'unauthorized' | 'logout' | 'relogin'

let memoryToken = ''
let sessionEpoch = 1
let lastSessionChangeReason: SessionChangeReason = 'logout'
const inFlightControllers = new Set<AbortController>()
let unauthorizedHandler: (() => void) | null = null

export class ApiError extends Error {
  public readonly status: number
  public readonly code?: string
  constructor(message: string, status: number, code?: string) {
    super(message)
    this.code = code
    this.name = 'ApiError'
    this.status = status
  }
}

export class StaleSessionError extends Error {
  public readonly reason: SessionChangeReason
  constructor(reason: SessionChangeReason = lastSessionChangeReason) {
    super('会话状态已变更，旧请求响应已丢弃')
    this.name = 'StaleSessionError'
    this.reason = reason
  }
}

export function getSessionEpoch(): number {
  return sessionEpoch
}

export function hasToken(): boolean {
  return memoryToken.length > 0
}

export function setMemoryToken(nextToken: string): number {
  sessionEpoch += 1
  lastSessionChangeReason = 'relogin'
  memoryToken = nextToken
  return sessionEpoch
}

export function invalidateSession(triggerUnauthorizedCallback: boolean): number {
  sessionEpoch += 1
  lastSessionChangeReason = triggerUnauthorizedCallback ? 'unauthorized' : 'logout'
  memoryToken = ''
  for (const controller of inFlightControllers) {
    controller.abort()
  }
  inFlightControllers.clear()
  if (triggerUnauthorizedCallback && unauthorizedHandler) {
    unauthorizedHandler()
  }
  return sessionEpoch
}

export function setOnUnauthorized(handler: (() => void) | null): void {
  unauthorizedHandler = handler
}

export const idPath = (id: string): string => encodeURIComponent(id)

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  rawBody?: string
  safeErrors?: boolean
  headers?: Record<string, string>
  overrideToken?: string
  isLoginProbe?: boolean
  timeoutMs?: number
  signal?: AbortSignal
}

export async function requestApi<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? (options.body === undefined ? 'GET' : 'POST')
  const capturedEpoch = sessionEpoch
  const activeToken = options.overrideToken ?? memoryToken
  const controller = new AbortController()
  inFlightControllers.add(controller)
  const cancel = () => controller.abort()
  options.signal?.addEventListener('abort', cancel, { once: true })
  if (options.signal?.aborted) cancel()

  const timeoutMs = options.timeoutMs ?? 20_000
  let timedOut = false
  const timer = globalThis.setTimeout(() => {
    timedOut = true
    controller.abort()
  }, timeoutMs)

  try {
    const headers: Record<string, string> = {
      Authorization: `Bearer ${activeToken}`,
      ...(options.headers ?? {}),
    }
    if (options.body !== undefined || options.rawBody !== undefined) {
      headers['Content-Type'] = 'application/json'
    }

    const response = await fetch(`/api${path}`, {
      method,
      headers,
      body: options.rawBody ?? (options.body === undefined ? undefined : JSON.stringify(options.body)),
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      signal: controller.signal,
    })

    if (!options.isLoginProbe && capturedEpoch !== sessionEpoch) {
      throw new StaleSessionError()
    }

    if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')
    const data: unknown = await response.json().catch(() => null)
    if (controller.signal.aborted) throw new DOMException('Aborted', 'AbortError')

    if (!options.isLoginProbe && capturedEpoch !== sessionEpoch) {
      throw new StaleSessionError()
    }

    if (!response.ok) {
      const detail =
        !options.safeErrors && data && typeof data === 'object' && 'error' in data && typeof (data as { error?: unknown }).error === 'string'
          ? (data as { error: string }).error
          : `API 请求失败（HTTP ${response.status}）`

      if (response.status === 401 && !options.isLoginProbe) {
        invalidateSession(true)
      }
      const code = data && typeof data === 'object' && 'code' in data && typeof data.code === 'string' ? data.code : undefined
      throw new ApiError(detail, response.status, code)
    }

    if (data === null) {
      throw new ApiError('API 返回了无法解析的 JSON，请检查同源服务状态。', 0)
    }

    return data as T
  } catch (error) {
    if (error instanceof StaleSessionError) {
      throw error
    }
    if (error instanceof ApiError && error.status === 401) {
      throw error
    }
    if (!options.isLoginProbe && capturedEpoch !== sessionEpoch) {
      throw new StaleSessionError()
    }
    if (error instanceof ApiError) {
      throw error
    }
    const baseMsg = timedOut
      ? '请求超时。'
      : error instanceof Error && error.name === 'AbortError'
        ? '请求已中止。'
        : '无法连接 API。'
    const suffix =
      method === 'GET'
        ? '请检查服务连接后重试。'
        : '请求可能已被服务端接收，请保留当前提交键并先核对提交状态，不要直接重复新建。'
    throw new ApiError(`${baseMsg}${suffix}`, 0)
  } finally {
    globalThis.clearTimeout(timer)
    options.signal?.removeEventListener('abort', cancel)
    inFlightControllers.delete(controller)
  }
}

export async function verifyAndSetToken(candidateToken: string): Promise<SystemInfo> {
  const trimmed = candidateToken.trim()
  if (!trimmed) {
    throw new ApiError('请输入访问令牌', 400)
  }
  const sys = await requestApi<SystemInfo>('/system', {
    method: 'GET',
    overrideToken: trimmed,
    isLoginProbe: true,
  })
  setMemoryToken(trimmed)
  return sys
}

export const fetchSystem = (signal?: AbortSignal): Promise<SystemInfo> => requestApi<SystemInfo>('/system', { signal })

export const fetchMissions = (): Promise<Mission[]> => requestApi<Mission[]>('/missions')

export const fetchMission = (id: string): Promise<Mission> =>
  requestApi<Mission>(`/missions/${idPath(id)}`)

export const fetchMissionThreads = (missionId: string): Promise<Thread[]> =>
  requestApi<Thread[]>(`/missions/${idPath(missionId)}/threads`)

export const fetchThreadSnapshot = (threadId: string): Promise<Snapshot> =>
  requestApi<Snapshot>(`/threads/${idPath(threadId)}`)

export const fetchHTTPRecords = (missionId: string, query: string, signal?: AbortSignal): Promise<HTTPRecordPage> =>
  requestApi<HTTPRecordPage>(`/missions/${idPath(missionId)}/http-records?${query}`, { signal })

export const fetchHTTPRecordDetail = (missionId: string, actionId: string, signal?: AbortSignal): Promise<HTTPRecordDetail> =>
  requestApi<HTTPRecordDetail>(`/missions/${idPath(missionId)}/http-records/${idPath(actionId)}`, { signal })

export const postExploration = (
  operationKey: string,
  input: ExplorationInput,
): Promise<ExplorationStart> =>
  requestApi<ExplorationStart>('/explorations', {
    method: 'POST',
    headers: { 'Idempotency-Key': operationKey },
    body: input,
  })

export const fetchOperationReceipt = (operationKey: string): Promise<OperationReceipt> =>
  requestApi<OperationReceipt>(`/operations/${idPath(operationKey)}`)
