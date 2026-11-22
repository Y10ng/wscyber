// 统一时间格式：本地时区、24 小时制、手工补零（不用 toLocaleString，避免 AM/PM）。
// 无效或空值一律返回空串，由调用方决定占位符。

function two(value: number): string {
  return value < 10 ? `0${value}` : String(value)
}

function parse(value: string | null | undefined): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

/** 'YYYY-MM-DD HH:mm'。 */
export function formatDateTime(iso: string | null | undefined): string {
  const date = parse(iso)
  if (!date) return ''
  return `${date.getFullYear()}-${two(date.getMonth() + 1)}-${two(date.getDate())} ${two(date.getHours())}:${two(date.getMinutes())}`
}

/** 'MM-DD HH:mm'。 */
export function formatShortDateTime(iso: string | null | undefined): string {
  const date = parse(iso)
  if (!date) return ''
  return `${two(date.getMonth() + 1)}-${two(date.getDate())} ${two(date.getHours())}:${two(date.getMinutes())}`
}

/** 'HH:mm:ss'。 */
export function formatClock(iso: string | null | undefined): string {
  const date = parse(iso)
  if (!date) return ''
  return `${two(date.getHours())}:${two(date.getMinutes())}:${two(date.getSeconds())}`
}

/** 'MM-DD HH:mm:ss'（审计表等需要秒级时间）。 */
export function formatShortDateTimeSeconds(iso: string | null | undefined): string {
  const date = parse(iso)
  if (!date) return ''
  return `${two(date.getMonth() + 1)}-${two(date.getDate())} ${two(date.getHours())}:${two(date.getMinutes())}:${two(date.getSeconds())}`
}
