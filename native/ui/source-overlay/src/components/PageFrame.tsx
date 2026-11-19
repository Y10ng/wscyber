import type { ReactNode } from 'react'
import { Menu } from 'lucide-react'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'

/** 页面容器：正文滚动区，内容最大宽 1600px、超宽居左（docs/ui-shell.md §2）。 */
export function Page({ children, className, contentClassName }: {
  children: ReactNode
  className?: string
  contentClassName?: string
}) {
  return (
    <div className={cn('flex-1 overflow-auto px-6 py-5', className)}>
      <div className={cn('max-w-[1600px] space-y-3', contentClassName)}>{children}</div>
    </div>
  )
}

/**
 * 页头（高 40px）：标题 → 搜索/分段（children）→ ml-auto 次要操作 → 主操作（actions）。
 * 移动端左侧菜单按钮打开侧栏抽屉。
 */
export function PageHeader({ title, children, actions, onOpenMenu, menuLabel }: {
  title: string
  children?: ReactNode
  actions?: ReactNode
  onOpenMenu?: () => void
  menuLabel?: string
}) {
  return (
    <div className="flex min-h-10 flex-wrap items-center gap-3">
      {onOpenMenu && (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={onOpenMenu}
          aria-label={menuLabel || title}
          className="-ml-1 shrink-0 text-muted-foreground md:hidden"
        >
          <Menu className="h-4 w-4" />
        </Button>
      )}
      <h1 className="shrink-0 text-[20px] font-semibold tracking-tight text-foreground">{title}</h1>
      {children}
      {actions && <div className="ml-auto flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}

export interface FilterPillItem<K extends string> {
  key: K
  label: string
  count?: number
  /** 状态色点（Tailwind 背景类，如 bg-info / bg-success / bg-muted-foreground）。 */
  dot?: string
}

/** 筛选行：状态胶囊，选中 border + bg-accent；计数与色点可选。 */
export function FilterPills<K extends string>({ items, value, onChange }: {
  items: FilterPillItem<K>[]
  value: K
  onChange: (key: K) => void
}) {
  return (
    <div className="flex flex-wrap items-center gap-1.5" role="group">
      {items.map((item) => {
        const selected = item.key === value
        return (
          <button
            key={item.key}
            type="button"
            aria-pressed={selected}
            onClick={() => onChange(item.key)}
            className={cn(
              'flex h-8 items-center gap-1.5 rounded-full px-3 text-[12px] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40',
              selected ? 'border border-border bg-accent font-medium text-foreground' : 'border border-transparent text-muted-foreground hover:bg-muted/40 hover:text-foreground',
            )}
          >
            {item.dot && <span aria-hidden className={cn('h-2 w-2 shrink-0 rounded-full', item.dot)} />}
            <span>{item.label}</span>
            {item.count !== undefined && <span className={cn('tabular-nums', selected ? 'text-foreground/70' : 'text-muted-foreground')}>{item.count}</span>}
          </button>
        )
      })}
    </div>
  )
}

/** 内容容器：圆角描边卡片 + 可选底部分页条（h-12, border-t）。 */
export function TableCard({ children, footer, className }: {
  children: ReactNode
  footer?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('overflow-hidden rounded-lg border border-border bg-card', className)}>
      {children}
      {footer && <div className="flex h-12 items-center border-t border-border px-5">{footer}</div>}
    </div>
  )
}

/** 只有图标的次要操作按钮带 tooltip（按钮排放规则 2）。 */
export function IconAction({ label, onClick, children, disabled }: {
  label: string
  onClick: () => void
  children: ReactNode
  disabled?: boolean
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button type="button" variant="outline" size="icon-sm" aria-label={label} onClick={onClick} disabled={disabled}>
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}