import { useEffect, useId, useMemo, useState, type CSSProperties, type ReactNode } from 'react'
import { Button, Tooltip } from 'antd'
import { CheckOutlined, CopyOutlined, DownOutlined } from '@ant-design/icons'
import { copyText } from './clipboard.ts'
import { createCopyLifecycle, STREAM } from './surface.ts'

export function CopyButton({ text, label }: { text: string; label: string }) {
  const [state, setState] = useState<'idle' | 'success' | 'error'>('idle')
  const lifecycle = useMemo(() => createCopyLifecycle(setState), [])
  useEffect(() => { lifecycle.reset(); setState('idle'); return lifecycle.reset }, [text, lifecycle])
  return <span className="copy-control" data-copy-state={state}>
    <Tooltip title={state === 'error' ? '复制失败，请手动选择文本' : label}><Button type="text" aria-label={label} icon={<span className="copy-icon"><CopyOutlined className="copy-default" /><CheckOutlined className="copy-check" /></span>} onClick={() => { void lifecycle.run(async () => copyText(text)) }} /></Tooltip>
    <span className="sr-only" role="status">{state === 'success' ? '已复制' : state === 'error' ? '复制失败，请手动选择文本' : ''}</span>
    {state === 'error' && <span className="copy-error">复制失败</span>}
  </span>
}

export function GridFold({ title, children, defaultOpen = false, open: controlledOpen, onOpenChange }: { title: string; children: ReactNode; defaultOpen?: boolean; open?: boolean; onOpenChange?: (open: boolean) => void }) {
  const [localOpen, setLocalOpen] = useState(defaultOpen)
  const open = controlledOpen ?? localOpen
  const setOpen = (value: boolean) => { setLocalOpen(value); onOpenChange?.(value) }
  const id = useId()
  return <section className="grid-fold" data-open={open}>
    <button type="button" className="fold-trigger" aria-label={title} aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}><span>{title}</span><DownOutlined /></button>
    <div className="fold-grid" id={id} aria-hidden={!open} inert={!open}><div className="fold-clip"><div className="fold-body">{children}</div></div></div>
  </section>
}

// 仅用于明确新增的短文本；历史正文和HTTP原件不伪装成实时流。
export function StreamText({ text }: { text: string }) {
  return <p className="stream-text" style={{ '--stream-duration': `${STREAM.duration}ms`, '--stream-easing': STREAM.easing, '--stream-blur': `${STREAM.blur}px` } as CSSProperties}>
    <span className="sr-only">{text}</span><span aria-hidden="true">{Array.from(text).map((character, i) => <span className="stream-character" key={i} style={{ animationDelay: `${i * STREAM.stagger}ms` }}>{character}</span>)}</span>
  </p>
}
