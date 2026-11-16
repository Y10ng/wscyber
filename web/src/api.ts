let token = ''
export const setToken = (value: string) => { token = value }

export class ApiError extends Error {
  constructor(message: string, public status: number) { super(message) }
}

// 凭据仅在此模块内存；所有请求均走同源控制面，不直接访问研究目标。
export async function api<T>(path: string, body?: unknown): Promise<T> {
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 20_000)
  try {
    const response = await fetch(`/api${path}`, {
      method: body === undefined ? 'GET' : 'POST',
      headers: {
        Authorization: `Bearer ${token}`,
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      signal: controller.signal,
    })
    const data: unknown = await response.json().catch(() => null)
    if (!response.ok) {
      const detail = data && typeof data === 'object' && 'error' in data && typeof data.error === 'string'
        ? data.error : `API 请求失败（HTTP ${response.status}）`
      throw new ApiError(detail, response.status)
    }
    if (data === null) throw new ApiError('API 返回了无法解析的 JSON，请检查同源服务或开发代理。', 0)
    return data as T
  } catch (error) {
    if (error instanceof ApiError) throw error
    const message = error instanceof Error && error.name === 'AbortError'
      ? '请求超时。' : '无法连接 API。'
    throw new ApiError(`${message}${body === undefined ? '请检查服务后刷新。' : '操作可能已被服务端接收，请先刷新核对，不要直接重复提交。'}`, 0)
  } finally {
    window.clearTimeout(timeout)
  }
}

export const idPath = (id: string) => encodeURIComponent(id)
