/** API十进制字符串直接格式化；金额、计数不经Number或浮点累计。 */
export function exactDecimal(value: string | null | undefined): string {
  if (value == null || !/^\d+(?:\.\d+)?$/.test(value)) return '未知'
  const [integer, fraction] = value.split('.')
  return integer.replace(/\B(?=(\d{3})+(?!\d))/g, ',') + (fraction ? `.${fraction}` : '')
}
export function integerValue(value: string | null | undefined): bigint | null {
  return typeof value === 'string' && /^\d+$/.test(value) ? BigInt(value) : null
}
/** 只把0..1,000,000的有界几何比例转换为Number，不转换原始计数。 */
export function plotRatio(value: string | null, maximum: bigint): number | null {
  const n = integerValue(value)
  if (n === null || maximum <= 0n || n > maximum) return null
  return Number(n * 1_000_000n / maximum) / 1_000_000
}
export function compactInteger(value: bigint): string {
  if (value < 10_000n) return exactDecimal(String(value))
  // 极大计数的轴刻度用有界科学记数标签；完整精度留在tooltip和表格。
  if (value >= 1_000_000_000_000n) {
    const digits = value.toString()
    return `${digits[0]}.${digits[1]}e${digits.length - 1}`
  }
  const unit = value >= 1_000_000_000n ? 1_000_000_000n : value >= 1_000_000n ? 1_000_000n : 1_000n
  const suffix = unit === 1_000_000_000n ? 'B' : unit === 1_000_000n ? 'M' : 'K'
  const tenths = value * 10n / unit
  return `${tenths / 10n}${tenths % 10n ? `.${tenths % 10n}` : ''}${suffix}`
}
