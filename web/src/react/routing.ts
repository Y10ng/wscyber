export type ParsedRoute =
  | { kind: 'login' }
  | { kind: 'projects' }
  | { kind: 'create' }
  | { kind: 'library' }
  | { kind: 'models'; configID: string | null; testID: string | null; invalid?: boolean }
  | { kind: 'usage'; tab: 'calls' | 'prices'; callID: string | null }
  | { kind: 'workspace'; missionId: string; threadId: string | null; modelCalls?: boolean; callID?: string | null }

export function parsePathname(pathname: string): ParsedRoute {
  const clean = pathname.replace(/\/+$/, '') || '/'
  if (clean === '/login') return { kind: 'login' }
  if (clean === '/library') return { kind: 'library' }
  if (clean === '/projects/new') return { kind: 'create' }
  if (clean === '/usage' || clean === '/usage/prices') return { kind: 'usage', tab: clean.endsWith('/prices') ? 'prices' : 'calls', callID: null }
  const usageCall = /^\/usage\/calls\/([0-9a-f]{32})$/.exec(clean)
  if (usageCall) return { kind: 'usage', tab: 'calls', callID: usageCall[1] }
  if (clean === '/settings/models') return { kind: 'models', configID: null, testID: null }
  const test = /^\/settings\/models\/([0-9a-f]{32})\/tests\/([0-9a-f]{32})$/.exec(clean)
  if (test) return { kind: 'models', configID: test[1], testID: test[2] }
  if (clean.startsWith('/settings/models/')) return { kind: 'models', configID: null, testID: null, invalid: true }
  try {
    const calls = /^\/projects\/([^/]+)\/model-calls(?:\/([0-9a-f]{32}))?$/.exec(clean)
    if (calls) return { kind: 'workspace', missionId: decodeURIComponent(calls[1]), threadId: null, modelCalls: true, callID: calls[2] ?? null }
    const thread = /^\/projects\/([^/]+)\/threads\/([^/]+)$/.exec(clean)
    if (thread) return { kind: 'workspace', missionId: decodeURIComponent(thread[1]), threadId: decodeURIComponent(thread[2]) }
    const mission = /^\/projects\/([^/]+)$/.exec(clean)
    if (mission) return { kind: 'workspace', missionId: decodeURIComponent(mission[1]), threadId: null }
  } catch {
    // 畸形编码不使整个入口崩溃，也不解析为可执行的外部地址。
  }
  return { kind: 'projects' }
}

export interface CreateContext {
  returnPath: string
  hasBackEntry: boolean
}
export interface RouteLocation {
  pathname: string
  create: CreateContext
}
export const defaultCreateContext: CreateContext = { returnPath: '/projects', hasBackEntry: false }

function validReturnPath(path: unknown): path is string {
  return typeof path === 'string' && !/[?#]/.test(path) &&
    (path === '/projects' || path === '/usage' || path === '/library' || parsePathname(path).kind === 'workspace')
}

/** history 只记录非秘密来源路由；草稿、令牌和提交键始终留在 App 内存。 */
export function readRouteLocation(pathname: string, state: unknown): RouteLocation {
  const context = state && typeof state === 'object' && 'wscyberCreate' in state ? state.wscyberCreate : null
  if (context && typeof context === 'object' && 'returnPath' in context && validReturnPath(context.returnPath)) {
    return { pathname, create: { returnPath: context.returnPath, hasBackEntry: 'hasBackEntry' in context && context.hasBackEntry === true } }
  }
  return { pathname, create: defaultCreateContext }
}

/** 登录替换当前历史条目时，保留 Modal 原先的返回位置及是否可后退。 */
export function nextCreateContext(sourcePath: string, preserved: CreateContext, replace: boolean): CreateContext {
  const source = parsePathname(sourcePath)
  if (source.kind === 'login' || source.kind === 'create') return preserved
  return { returnPath: validReturnPath(sourcePath) ? sourcePath : '/projects', hasBackEntry: !replace }
}

export function contentPathname(location: RouteLocation): string {
  return parsePathname(location.pathname).kind === 'create' ? location.create.returnPath : location.pathname
}

export function createCloseDestination(context: CreateContext): { back: true } | { back: false; pathname: string } {
  return context.hasBackEntry ? { back: true } : { back: false, pathname: context.returnPath }
}
