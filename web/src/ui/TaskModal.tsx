import type { KeyboardEvent } from 'react'
import { Modal, type ModalProps } from 'antd'

// 补Ant6末项Tab进入浏览器工具栏的问题，不接管下拉组件内部的键盘行为。
function keepModalTabFocus(event: KeyboardEvent<HTMLDivElement>) {
  if (event.key !== 'Tab' || event.altKey || event.ctrlKey || event.metaKey) return
  const items = [...event.currentTarget.querySelectorAll<HTMLElement>('button, input, textarea, select, a[href], [tabindex]')]
    .filter((item) => item.tabIndex >= 0 && !item.matches(':disabled, [type="hidden"]') && !item.closest('[inert], [hidden]') && item.checkVisibility({ checkVisibilityCSS: true }))
  const first = items[0], last = items.at(-1)
  const next = event.shiftKey && document.activeElement === first ? last : !event.shiftKey && document.activeElement === last ? first : null
  if (next) { event.preventDefault(); next.focus({ preventScroll: true }) }
}

/** 草稿、提交、串行切换由调用方持有；关闭本身不丢弃草稿，也不执行网络动作。 */
export function TaskModal({ className = '', width = 880, ...props }: Omit<ModalProps, 'centered' | 'modalRender'>) {
  // 任务弹窗与遮罩有意不使用进出场动画，避免减少动效的零时长使关闭卡在等待动画结束。
  return <Modal {...props} width={width} centered className={`task-modal ${className}`}
    transitionName="" maskTransitionName=""
    modalRender={(node) => <div style={{ display: 'contents' }} onKeyDownCapture={keepModalTabFocus}>{node}</div>} />
}
