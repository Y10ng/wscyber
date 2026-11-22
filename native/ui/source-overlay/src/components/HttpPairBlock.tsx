import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { RotateCw } from 'lucide-react'
import { aopFlowToHttpView, type TrafficHttpView } from '@cyber/traffic'
import { Button } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { getWebApiEvidence } from '../api'
import { decodeEvidenceFlow } from './WebApiEvidenceReference'

// 敏感请求/响应头：遮盖开关开启时只替换值，头名保留。
const SENSITIVE_HEADERS = new Set([
  'authorization',
  'proxy-authorization',
  'cookie',
  'set-cookie',
  'x-api-key',
  'x-auth-token',
])

const BODY_FOLD_LINES = 12
const BODY_LINE_CHARS = 400
const BODY_MAX_CHARS = 200_000

/** 把证据解码成 HTTP 视图；masked 时请求后端遮盖资产口令，404/解析失败由调用方展示重试。 */
export function useEvidenceHttpView(taskId: string, evidenceId: string, masked = true) {
  const [view, setView] = useState<TrafficHttpView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const seq = useRef(0)

  const load = useCallback(async () => {
    const current = ++seq.current
    setLoading(true)
    setError('')
    try {
      const detail = await getWebApiEvidence(taskId, evidenceId, { mask: masked })
      const next = aopFlowToHttpView(decodeEvidenceFlow(detail))
      if (current !== seq.current) return
      setView(next)
    } catch (err) {
      if (current !== seq.current) return
      setView(null)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (current === seq.current) setLoading(false)
    }
  }, [taskId, evidenceId, masked])

  useEffect(() => { void load() }, [load])

  return { view, loading, error, reload: load }
}

/** 去掉 origin，仅保留路径与查询串；非法 URL 原样返回。 */
export function displayPath(url: string): string {
  if (!url) return ''
  try {
    const parsed = new URL(url)
    return `${parsed.pathname}${parsed.search}` || '/'
  } catch {
    return url
  }
}

function maskHeader(name: string, value: string, masked: boolean): string {
  return masked && SENSITIVE_HEADERS.has(name.toLowerCase()) ? '******' : value
}

/** 复制用请求原文：不折叠、不省略、不格式化，遮盖开关生效。 */
export function rawRequestText(view: TrafficHttpView | null, masked: boolean): string {
  if (!view) return ''
  const { request } = view
  const start = `${request.method} ${request.requestTarget} ${request.httpVersion}`.trim()
  const headers = request.headers.map(([name, value]) => `${name}: ${maskHeader(name, value, masked)}`)
  const lines = [start, ...headers]
  if (request.body) lines.push('', request.body)
  return lines.join('\n')
}

/**
 * 仅显示用的 JSON 美化：正文 trim 后以 { 或 [ 开头且可解析时，输出两空格缩进。
 * 复制、截图与 curl 都仍用原件，不走这里。
 */
export function formatBodyText(body: string): string {
  const trimmed = body.trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return body
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return body
  }
}

// 美化后若原来能命中的 mark 在格式化文本里全部找得到才用美化文本；
// 任一丢失就退回原件，避免空格/转义差异让行高亮失效。
function bodyTextForDisplay(body: string, marks: string[]): string {
  const formatted = formatBodyText(body)
  if (formatted === body) return formatted
  const originalLines = body.split('\n')
  const matched = marks.filter((mark) => mark !== '' && originalLines.some((line) => line.includes(mark)))
  if (matched.length === 0) return formatted
  const formattedLines = formatted.split('\n')
  return matched.every((mark) => formattedLines.some((line) => line.includes(mark))) ? formatted : body
}

/** 响应正文按显示规则格式化后的行（对照区复用）。 */
export function responseBodyLines(view: TrafficHttpView | null): string[] {
  const body = view?.response?.body
  if (!body) return []
  return formatBodyText(body).split('\n')
}

function lineMatches(text: string, marks: string[]): boolean {
  return marks.some((mark) => mark !== '' && text.includes(mark))
}

interface Line {
  num: number
  text: string
  highlight: boolean
  /** 折叠显示时该行原始字符数（超过 400 才设置），用于灰字提示。 */
  truncatedCount?: number
  /** 折叠空隙提示行：不显示行号。 */
  omitted?: boolean
}

function LineRow({ line }: { line: Line }) {
  const { t } = useTranslation('chat')
  if (line.omitted) {
    return (
      <div className="px-[10px] py-0.5 font-sans text-[11px] italic text-muted-foreground opacity-80">
        {line.text}
      </div>
    )
  }
  return (
    <div
      className={cn('flex items-baseline whitespace-pre-wrap break-all px-[10px]', line.highlight && 'pl-[7.5px] font-medium')}
      style={line.highlight ? {
        background: 'var(--code-hl-bg)',
        borderLeft: '2.5px solid var(--code-hl-border)',
      } : undefined}
    >
      <span aria-hidden className="w-6 shrink-0 select-none text-[10.5px] text-muted-foreground opacity-65">{line.num}</span>
      <span className="min-w-0">
        {line.text}
        {line.truncatedCount !== undefined && (
          <span className="text-muted-foreground"> {t('vulnCard.lineTruncated', { count: line.truncatedCount })}</span>
        )}
      </span>
    </div>
  )
}

function CollapseButton({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return (
    <div className="px-3 pt-1.5 font-sans">
      <button
        type="button"
        onClick={onClick}
        className="text-left text-[11px] text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        {children}
      </button>
    </div>
  )
}

function HttpBox({ title, header, headerLines, headerCollapse, bodyLines, bodyCollapse, emptyText }: {
  title: 'Request' | 'Response'
  header: ReactNode
  headerLines: Line[]
  headerCollapse?: ReactNode
  bodyLines: Line[]
  bodyCollapse?: ReactNode
  emptyText?: string
}) {
  return (
    <div className="flex min-w-0 flex-col overflow-hidden rounded-md border border-border bg-[var(--bg-code-light)]">
      <div className="flex items-center justify-between gap-2 border-b border-border bg-[var(--bg-subtle)] px-3 py-1.5 text-[11px]">
        <div className="flex min-w-0 items-center gap-1.5 font-mono font-semibold">{header}</div>
        <span className="shrink-0 text-muted-foreground">{title}</span>
      </div>
      <div className="min-w-0 flex-1 overflow-x-auto py-2 font-mono text-[12px] leading-[1.65]">
        {headerLines.length === 0 && bodyLines.length === 0 ? (
          <div className="px-3 py-1.5 text-[11.5px] text-muted-foreground">{emptyText}</div>
        ) : (
          <>
            {headerLines.map((line) => <LineRow key={line.num} line={line} />)}
            {headerCollapse}
            {bodyLines.map((line, index) => <LineRow key={`body-${index}`} line={line} />)}
            {bodyCollapse}
          </>
        )}
      </div>
    </div>
  )
}

/** 漏洞卡“关键请求”的请求/响应并排代码块（行号、高亮、折叠与遮盖）。 */
export default function HttpPairBlock({
  view, loading, error, retry, requestMarks, responseMarks, masked, foldHeaders = true, stacked = false,
}: {
  view: TrafficHttpView | null
  loading: boolean
  error: string
  retry: () => void
  requestMarks: string[]
  responseMarks: string[]
  masked: boolean
  /** 默认折叠非关键请求头；false 时全部显示且不出现“展开其余请求头”。 */
  foldHeaders?: boolean
  /** 窄栏内强制上下堆叠（默认按视口宽度在 ≥769px 并排）。 */
  stacked?: boolean
}) {
  const { t } = useTranslation('chat')
  const [headersExpanded, setHeadersExpanded] = useState(false)
  const [requestBodyExpanded, setRequestBodyExpanded] = useState(false)
  const [responseBodyExpanded, setResponseBodyExpanded] = useState(false)

  // 证据切换时重置折叠态。
  useEffect(() => {
    setHeadersExpanded(false)
    setRequestBodyExpanded(false)
    setResponseBodyExpanded(false)
  }, [view?.id])

  if (loading) {
    return (
      <div className={cn('grid min-h-[150px] grid-cols-1 gap-3', !stacked && 'min-[769px]:grid-cols-2')} aria-busy>
        {[0, 1].map((side) => (
          <div key={side} className="space-y-2 rounded-md border border-border bg-[var(--bg-code-light)] p-3">
            <div className="h-3 w-1/3 animate-pulse rounded bg-muted" />
            {[0, 1, 2, 3, 4].map((row) => (
              <div key={row} className="h-2.5 animate-pulse rounded bg-muted/70" style={{ width: `${88 - row * 7}%` }} />
            ))}
          </div>
        ))}
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex min-h-[120px] flex-col items-center justify-center gap-2 rounded-md border border-border bg-[var(--bg-code-light)] text-[12px] text-destructive">
        <span>{t('vulnCard.evidenceLoadFailed')}</span>
        <Button type="button" variant="outline" size="sm" className="h-7 gap-1" onClick={retry}>
          <RotateCw className="h-3 w-3" />
          {t('vulnCard.retry')}
        </Button>
      </div>
    )
  }

  if (!view) return null

  const { request, response } = view
  const requestStart = `${request.method} ${request.requestTarget} ${request.httpVersion}`.trim()

  const clipLine = (line: Line): Line => {
    const chars = Array.from(line.text)
    if (chars.length <= BODY_LINE_CHARS) return line
    return { ...line, text: chars.slice(0, BODY_LINE_CHARS).join(''), truncatedCount: chars.length }
  }

  const bodyCharCount = (content: Line[]): number =>
    content.reduce((sum, line) => sum + Array.from(line.text).length, 0)

  // 行数超过 12 或任一行超过 400 字都进入折叠态（截断捕获的单行 JSON 也受控）。
  const needsFold = (content: Line[]): boolean => content.length > BODY_FOLD_LINES
    || content.some((line) => Array.from(line.text).length > BODY_LINE_CHARS)

  // 展开后最多渲染 200000 字，剩余字数以灰字提示（不改变复制/下载原件）。
  const capBody = (content: Line[]): Line[] => {
    const total = bodyCharCount(content)
    if (total <= BODY_MAX_CHARS) return content
    const result: Line[] = []
    let used = 0
    for (const line of content) {
      if (used >= BODY_MAX_CHARS) break
      const chars = Array.from(line.text)
      const take = Math.min(chars.length, BODY_MAX_CHARS - used)
      result.push(take < chars.length ? { ...line, text: chars.slice(0, take).join('') } : line)
      used += take
    }
    result.push({
      num: 0,
      text: t('vulnCard.bodyTruncated', { count: total - used }),
      highlight: false,
      omitted: true,
    })
    return result
  }

  const omitLine = (count: number): Line => ({
    num: 0,
    text: t('vulnCard.omitLines', { count }),
    highlight: false,
    omitted: true,
  })

  // 默认前 12 行；12 行之后的高亮行各带上下 1 行，空隙合并为“省略 N 行”。
  // 长行在折叠态裁到 400 字；展开态保留完整行，但总字符数有上限。
  const foldBody = (content: Line[], expanded: boolean): Line[] => {
    if (expanded) return capBody(content)
    if (!needsFold(content)) return content
    if (content.length <= BODY_FOLD_LINES) return content.map(clipLine)
    const ranges: Array<[number, number]> = [[0, BODY_FOLD_LINES - 1]]
    content.forEach((line, index) => {
      if (index >= BODY_FOLD_LINES && line.highlight) {
        ranges.push([Math.max(0, index - 1), Math.min(content.length - 1, index + 1)])
      }
    })
    ranges.sort((a, b) => a[0] - b[0])
    const merged: Array<[number, number]> = []
    for (const range of ranges) {
      const last = merged[merged.length - 1]
      if (last && range[0] <= last[1] + 1) last[1] = Math.max(last[1], range[1])
      else merged.push([range[0], range[1]])
    }
    const result: Line[] = []
    let cursor = 0
    for (const [start, end] of merged) {
      if (start > cursor) result.push(omitLine(start - cursor))
      for (let index = start; index <= end; index++) result.push(clipLine(content[index]))
      cursor = end + 1
    }
    if (cursor < content.length) result.push(omitLine(content.length - cursor))
    return result
  }

  const buildBody = (body: string, marks: string[], blankNum: number) => {
    const text = bodyTextForDisplay(body, marks)
    const content: Line[] = text.split('\n').map((line, index) => ({
      num: blankNum + 1 + index,
      text: line,
      highlight: lineMatches(line, marks),
    }))
    return { blank: { num: blankNum, text: '', highlight: false } as Line, content }
  }

  const fullRequestHeaders: Line[] = request.headers.map(([name, value], index) => ({
    num: index + 2,
    text: `${name}: ${maskHeader(name, value, masked)}`,
    highlight: lineMatches(`${name}: ${value}`, requestMarks),
  }))
  const visibleRequestHeaders = headersExpanded || !foldHeaders
    ? fullRequestHeaders
    : fullRequestHeaders.filter((line, index) => {
      const [name, value] = request.headers[index]
      return name.toLowerCase() === 'host' || lineMatches(`${name}: ${value}`, requestMarks)
    })
  const foldedHeaderCount = fullRequestHeaders.filter((line, index) => {
    const [name, value] = request.headers[index]
    return name.toLowerCase() === 'host' || lineMatches(`${name}: ${value}`, requestMarks)
  }).length
  const hiddenHeaderCount = foldHeaders ? fullRequestHeaders.length - foldedHeaderCount : 0

  const requestHeaderLines: Line[] = [
    { num: 1, text: requestStart, highlight: lineMatches(requestStart, requestMarks) },
    ...visibleRequestHeaders,
  ]

  const requestBody = request.body
    ? buildBody(request.body, requestMarks, fullRequestHeaders.length + 2)
    : null
  const requestBodyTotal = requestBody?.content.length || 0
  const requestBodyChars = requestBody ? bodyCharCount(requestBody.content) : 0
  const requestBodyFolded = requestBody ? needsFold(requestBody.content) : false
  const requestBodyRows = requestBody
    ? [requestBody.blank, ...foldBody(requestBody.content, requestBodyExpanded)]
    : []
  const requestBodyToggle = requestBodyFolded ? (
    <CollapseButton onClick={() => setRequestBodyExpanded((value) => !value)}>
      {requestBodyExpanded
        ? t('vulnCard.collapseRequestBody')
        : requestBodyTotal > BODY_FOLD_LINES
          ? t('vulnCard.expandRequestBody', { count: requestBodyTotal })
          : t('vulnCard.expandLongBody', { count: requestBodyChars })}
    </CollapseButton>
  ) : null

  const headerToggle = hiddenHeaderCount > 0 ? (
    <CollapseButton onClick={() => setHeadersExpanded((value) => !value)}>
      {headersExpanded
        ? t('vulnCard.collapseHeaders')
        : t('vulnCard.expandHeaders', { count: hiddenHeaderCount })}
    </CollapseButton>
  ) : null

  const responseHeaderLines: Line[] = []
  if (response) {
    const statusLine = `${response.httpVersion || 'HTTP/1.1'} ${response.status} ${response.reason}`.trim()
    responseHeaderLines.push({ num: 1, text: statusLine, highlight: lineMatches(statusLine, responseMarks) })
    response.headers.forEach(([name, value], index) => {
      responseHeaderLines.push({
        num: index + 2,
        text: `${name}: ${maskHeader(name, value, masked)}`,
        highlight: lineMatches(`${name}: ${value}`, responseMarks),
      })
    })
  }

  const responseBody = response?.body
    ? buildBody(response.body, responseMarks, responseHeaderLines.length + 1)
    : null
  const responseBodyTotal = responseBody?.content.length || 0
  const responseBodyChars = responseBody ? bodyCharCount(responseBody.content) : 0
  const responseBodyFolded = responseBody ? needsFold(responseBody.content) : false
  const responseBodyRows = responseBody
    ? [responseBody.blank, ...foldBody(responseBody.content, responseBodyExpanded)]
    : []
  const responseBodyToggle = responseBodyFolded ? (
    <CollapseButton onClick={() => setResponseBodyExpanded((value) => !value)}>
      {responseBodyExpanded
        ? t('vulnCard.collapseBody')
        : responseBodyTotal > BODY_FOLD_LINES
          ? t('vulnCard.expandBody', { count: responseBodyTotal })
          : t('vulnCard.expandLongBody', { count: responseBodyChars })}
    </CollapseButton>
  ) : null

  const requestHeader = (
    <>
      <span className="shrink-0 rounded bg-blue-600 px-1.5 py-px text-[10px] font-semibold text-white">
        {request.method || 'REQ'}
      </span>
      <span className="min-w-0 truncate text-foreground" title={request.requestTarget}>{request.requestTarget}</span>
    </>
  )

  const responseHeader = response ? (
    <span className={cn(
      'shrink-0 rounded px-1.5 py-px text-[10px] font-semibold text-white',
      response.status >= 400 ? 'bg-rose-600' : 'bg-emerald-600',
    )}>
      {response.status}{response.reason ? ` ${response.reason}` : ''}
    </span>
  ) : null

  return (
    <div className={cn('grid grid-cols-1 items-start gap-3', !stacked && 'min-[769px]:grid-cols-2')}>
      <HttpBox
        title="Request"
        header={requestHeader}
        headerLines={requestHeaderLines}
        headerCollapse={headerToggle}
        bodyLines={requestBodyRows}
        bodyCollapse={requestBodyToggle}
      />
      <HttpBox
        title="Response"
        header={responseHeader}
        headerLines={responseHeaderLines}
        bodyLines={responseBodyRows}
        bodyCollapse={responseBodyToggle}
        emptyText={t('vulnCard.noResponse')}
      />
    </div>
  )
}
