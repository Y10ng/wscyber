import type { DemoTrafficRequest } from './types.ts'
export * from '../ui/surface.ts'

export function clampSplitRatio(value: number, height = 700) {
  const usable = Math.max(530, height - 8)
  const minimum = Math.max(22, 150 / usable * 100)
  const maximum = Math.max(minimum, Math.min(65, (usable - 380) / usable * 100))
  return Math.max(minimum, Math.min(maximum, value))
}
export function projectEvidence(requests: DemoTrafficRequest[], projectId: string, id: string) {
  return requests.find((request) => request.id === id && request.projectId === projectId) ?? null
}
