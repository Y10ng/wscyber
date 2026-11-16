import { useEffect, useMemo, useRef, useState } from 'react'
import { Button, Input, Segmented, Tooltip } from 'antd'
import { ArrowDownOutlined, EyeInvisibleOutlined, EyeOutlined } from '@ant-design/icons'
import { CopyButton } from './SurfaceControls.tsx'
import { formatDemoHeaders, type DemoTrafficRequest } from './types.ts'
import { messageText, searchParts, type MessageMode } from './trafficUtils.ts'

export function MessageViewer({ request, side }: { request: DemoTrafficRequest; side: 'request' | 'response' }) {
  const label = side === 'request' ? '请求' : '响应'
  const [mode, setMode] = useState<MessageMode>('Raw')
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const [reveal, setReveal] = useState(false)
  const [wrap, setWrap] = useState(true)
  const codeRef = useRef<HTMLPreElement>(null)
  const text = messageText(request, side, mode, reveal)
  const parts = useMemo(() => searchParts(text, query), [text, query])
  const matches = parts.filter((part) => part.match !== null).length
  const sensitive = formatDemoHeaders(side === 'request' ? request.requestHeaders : request.responseHeaders, false).hasSensitive
  useEffect(() => {
    const pane = codeRef.current, match = pane?.querySelector<HTMLElement>('[data-active="true"]')
    if (!pane || !match) return
    const bounds = pane.getBoundingClientRect(), item = match.getBoundingClientRect()
    if (item.top < bounds.top) pane.scrollTop += item.top - bounds.top - 12
    else if (item.bottom > bounds.bottom) pane.scrollTop += item.bottom - bounds.bottom + 12
    if (item.left < bounds.left) pane.scrollLeft += item.left - bounds.left - 12
    else if (item.right > bounds.right) pane.scrollLeft += item.right - bounds.right + 12
  }, [active, query, mode, reveal])
  // The parent keys both viewers by record id: reveal, search and clipboard feedback never leak to another record.
  return <section className="message-viewer" aria-label={`${label}报文查看器`}>
    <header className="message-title"><strong>{label}</strong><span className="truncate muted">{side === 'request' ? request.method : request.statusCode === null ? request.sendStatus : `${request.statusCode} ${request.statusText}`}</span>
      <CopyButton label={`复制${label}当前视图`} text={text} />
    </header>
    <div className="message-controls">
      <Segmented aria-label={`${label}视图`} options={['Raw', '格式化', 'Headers']} value={mode} onChange={(value) => { setMode(value as MessageMode); setActive(0) }} />
      <div className="row-actions"><Tooltip title="自动换行"><Button type={wrap ? 'default' : 'text'} aria-label={`${label}自动换行`} aria-pressed={wrap} onClick={() => setWrap(!wrap)}>换行</Button></Tooltip>
      {sensitive && <Tooltip title={reveal ? '遮蔽敏感头' : '查看敏感头'}><Button type="text" aria-label={`${label}${reveal ? '遮蔽' : '查看'}敏感头`} icon={reveal ? <EyeOutlined /> : <EyeInvisibleOutlined />} onClick={() => { setReveal(!reveal); setActive(0) }} /></Tooltip>}</div>
    </div>
    <div className="message-search"><Input aria-label={`搜索${label}报文`} placeholder={`搜索${label}报文`} value={query} allowClear onChange={(e) => { setQuery(e.target.value); setActive(0) }} onPressEnter={() => setActive((value) => matches ? (value + 1) % matches : 0)} />
      <span className="match-count" aria-live="polite">{query ? matches ? `${active + 1}/${matches}` : '无匹配' : ''}</span>
      <Tooltip title="下一个匹配"><Button aria-label={`${label}下一个匹配`} type="text" icon={<ArrowDownOutlined />} disabled={!matches} onClick={() => setActive((value) => (value + 1) % matches)} /></Tooltip>
    </div>
    <pre ref={codeRef} tabIndex={0} aria-label={`${label}报文正文`} className={`message-code ${wrap ? 'wrap' : ''}`}><code>{parts.map((part, i) => part.match === null ? part.text : <mark key={i} data-active={part.match === active}>{part.text}</mark>)}</code></pre>
  </section>
}
