import { useLayoutEffect, useState } from 'react'
import { ApiError, StaleSessionError } from './api.ts'
// 候选及验证共用5秒串行有界读取；不会重发写入。
export function findingReadDelay(failures: number) { return failures > 3 ? null : Math.min(30000, 5000 * 2 ** failures) }
export function useFindingRead<T>(scope: string, active: boolean, read: (signal: AbortSignal) => Promise<T>, poll = false) {
  const [state, setState] = useState<{ scope: string; value: T | null; busy: boolean; error: string }>({ scope, value: null, busy: false, error: '' })
  const [reload, setReload] = useState(0)
  useLayoutEffect(() => {
    if (!active) return
    let alive = true, failures = 0
    let timer: ReturnType<typeof setTimeout> | undefined
    const controller = new AbortController()
    const load = async () => {
      if (document.hidden) { timer = setTimeout(load, 5000); return }
      setState(old => ({ scope, value: old.scope === scope ? old.value : null, error: old.scope === scope ? old.error : '', busy: true }))
      let again = poll
      try {
        const value = await read(controller.signal)
        if (!alive) return
        failures = 0; setState({ scope, value, error: '', busy: false })
      } catch (error) {
        if (!alive) return
        failures++
        again = poll && !(error instanceof StaleSessionError || error instanceof ApiError && [400, 401, 403, 404].includes(error.status))
        setState(old => ({ ...old, busy: false, error: error instanceof Error ? error.message : '读取失败' }))
      }
      const delay = findingReadDelay(failures)
      if (alive && again && delay !== null) timer = setTimeout(load, delay)
    }
    void load()
    return () => { alive = false; controller.abort(); clearTimeout(timer) }
  }, [scope, active, read, poll, reload])
  return { value: state.scope === scope ? state.value : null, busy: active && (state.scope !== scope || state.busy), error: state.scope === scope ? state.error : '', refresh: () => setReload(n => n + 1) }
}
