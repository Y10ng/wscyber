export type SurfaceTheme = 'light' | 'dark'
export const surfaceTokens = {
  light: { base: '#f4f6f9', plane: '#ffffff', inset: '#f5f7fa', raised: '#ffffff', ink: '#20242d', muted: '#697384', accent: '#315ee7', 'series-input': '#2a78d6', 'series-output': '#eb6834', selected: '#edf3ff', regular: 400, medium: 500, strong: 600, heading: 650, positive: '#247c58', warning: '#8a5e16', 'focus-inner': '#dce7ff', 'focus-outer': '#7098e8', 'line': '#e2e7ef', 'muted-surface': '#f0f3f8' },
  dark: { base: '#10131a', plane: '#171c26', inset: '#1d2430', raised: '#202836', ink: '#e3e8f0', muted: '#a0acbc', accent: '#87a8ff', 'series-input': '#3987e5', 'series-output': '#d95926', selected: '#24344f', regular: 360, medium: 460, strong: 560, heading: 610, positive: '#a6c4ae', warning: '#d5bb84', 'focus-inner': '#293854', 'focus-outer': '#87a8ff', 'line': '#303a49', 'muted-surface': '#242b39' },
} as const
export const STREAM = { duration: 90, stagger: 30, blur: 6, easing: 'cubic-bezier(.215,.61,.355,1)' } as const
export const SPRINGS = { drag: { stiffness: 1200, damping: 80, mass: 1 }, check: { stiffness: 380, damping: 18, mass: 1 } } as const
// 解析单位阶跃响应，保留已验收弹簧参数，不用任意Bezier代替。
export function springPosition(seconds: number, { stiffness: k, damping: c, mass: m }: { stiffness: number; damping: number; mass: number }) {
  const a = c / (2 * m), w2 = k / m
  if (a * a > w2) {
    const d = Math.sqrt(a * a - w2), r1 = -a + d, r2 = -a - d
    return 1 + (r2 * Math.exp(r1 * seconds) - r1 * Math.exp(r2 * seconds)) / (r1 - r2)
  }
  const w = Math.sqrt(w2 - a * a)
  return 1 - Math.exp(-a * seconds) * (Math.cos(w * seconds) + a / w * Math.sin(w * seconds))
}
export function springCurve(kind: keyof typeof SPRINGS) {
  const duration = kind === 'drag' ? 0.6 : 0.8
  const samples = Array.from({ length: 81 }, (_, i) => i === 80 ? 1 : springPosition(i / 80 * duration, SPRINGS[kind]))
  return `linear(${samples.map((x) => x.toFixed(5)).join(',')})`
}
export const COPY_HOLD_MS = 2500
// 重复复制、记录变化与卸载使旧异步完成和计时器失效。
export function createCopyLifecycle(publish: (state: 'idle' | 'success' | 'error') => void,
  schedule: (callback: () => void, ms: number) => ReturnType<typeof setTimeout> = setTimeout,
  cancel: (id: ReturnType<typeof setTimeout>) => void = clearTimeout) {
  let generation = 0, timer: ReturnType<typeof setTimeout> | undefined
  const reset = () => { generation++; if (timer !== undefined) cancel(timer); timer = undefined }
  return {
    reset,
    async run(copy: () => Promise<boolean>) {
      reset(); const attempt = generation; publish('idle')
      let ok = false
      try { ok = await copy() } catch { /* 权限失败不能显示成功。 */ }
      if (attempt !== generation) return
      publish(ok ? 'success' : 'error')
      timer = schedule(() => { if (attempt === generation) publish('idle'); timer = undefined }, COPY_HOLD_MS)
    },
  }
}
