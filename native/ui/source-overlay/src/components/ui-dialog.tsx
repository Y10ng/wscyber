import type { ReactNode } from 'react'
import { DialogTitle } from '@cyber/ui'
import { cn } from '@cyber/theme'

/**
 * Dialog / Sheet 统一头部与底部（docs/ui-design.md §3 弹层规范）。
 * 只封装样式与结构；DialogContent/SheetContent 仍用 @cyber/ui 原件。
 *
 * 头部：可选 32px 品牌浅底图标方块 + 16px 600 标题；描述 12px 灰色在标题下；
 * 下方 1px 分隔线；右侧留出 DialogContent 自带关闭按钮的空间。
 */
export function DialogHeaderRow({ icon, title, description, className }: {
  icon?: ReactNode
  title: ReactNode
  description?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex shrink-0 items-start gap-2.5 border-b border-border px-5 py-4 pr-12', className)}>
      {icon && (
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-[var(--brand-soft)] text-[var(--brand-primary)]">
          {icon}
        </div>
      )}
      <div className="min-w-0 flex-1">
        <DialogTitle className="truncate text-[16px] font-semibold leading-6 text-foreground">{title}</DialogTitle>
        {description && <p className="mt-0.5 text-[12px] leading-5 text-muted-foreground">{description}</p>}
      </div>
    </div>
  )
}

/**
 * Dialog / Sheet 底部操作行：右对齐，次要 outline 在左、主操作 primary 在右；
 * 与内容之间 1px 分隔线。极窄屏允许换行但按钮本身不收缩文字。
 */
export function DialogFooterRow({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn('flex shrink-0 flex-row flex-wrap items-center justify-end gap-2 border-t border-border px-5 py-3', className)}>
      {children}
    </div>
  )
}
