/** Token 数：≥1e6 显示 1.2M，≥1e3 显示 12.3K（docs/ui-shell.md §5）。 */
export function formatTokens(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0'
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return String(Math.round(value))
}
