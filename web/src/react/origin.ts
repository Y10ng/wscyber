export interface OriginValidationResult {
  ok: boolean
  normalized?: string
  error?: string
}

export interface OriginListParseResult {
  origins: string[]
  errors: Array<{ line: number; raw: string; error: string }>
}

function parseStrictIPv4(raw: string): number[] | null {
  const parts = raw.split('.')
  if (parts.length !== 4) return null
  const octets: number[] = []
  for (const part of parts) {
    if (!/^(0|[1-9][0-9]{0,2})$/.test(part)) return null
    const n = Number(part)
    if (!Number.isInteger(n) || n < 0 || n > 255) return null
    octets.push(n)
  }
  return octets
}

function parseStrictIPv6(raw: string): Uint16Array | null {
  if (!raw || raw.includes('%') || raw.includes('.')) {
    // Handle IPv4-embedded IPv6 below
  }
  if (!raw || raw.includes('%')) return null
  const doubleColonParts = raw.split('::')
  if (doubleColonParts.length > 2) return null

  const parseSegmentList = (segmentStr: string, allowEmbeddedIPv4: boolean): number[] | null => {
    if (segmentStr === '') return []
    const tokens = segmentStr.split(':')
    const words: number[] = []
    for (let i = 0; i < tokens.length; i++) {
      const token = tokens[i]
      if (allowEmbeddedIPv4 && i === tokens.length - 1 && token.includes('.')) {
        const ipv4 = parseStrictIPv4(token)
        if (!ipv4) return null
        words.push((ipv4[0] << 8) | ipv4[1], (ipv4[2] << 8) | ipv4[3])
      } else {
        if (!/^[0-9a-fA-F]{1,4}$/.test(token)) return null
        words.push(parseInt(token, 16))
      }
    }
    return words
  }

  if (doubleColonParts.length === 1) {
    const words = parseSegmentList(doubleColonParts[0], true)
    if (!words || words.length !== 8) return null
    return Uint16Array.from(words)
  }

  const left = parseSegmentList(doubleColonParts[0], false)
  const right = parseSegmentList(doubleColonParts[1], true)
  if (!left || !right) return null
  const missing = 8 - (left.length + right.length)
  if (missing < 1) return null
  const full = [...left, ...new Array<number>(missing).fill(0), ...right]
  if (full.length !== 8) return null
  return Uint16Array.from(full)
}

function formatIPv6OrUnmapped(words: Uint16Array): { host: string; isIPv6: boolean } {
  const isIPv4Mapped =
    words[0] === 0 &&
    words[1] === 0 &&
    words[2] === 0 &&
    words[3] === 0 &&
    words[4] === 0 &&
    words[5] === 0xffff
  if (isIPv4Mapped) {
    const a = (words[6] >> 8) & 0xff
    const b = words[6] & 0xff
    const c = (words[7] >> 8) & 0xff
    const d = words[7] & 0xff
    return { host: `${a}.${b}.${c}.${d}`, isIPv6: false }
  }

  let bestStart = -1
  let bestLen = 0
  let curStart = -1
  let curLen = 0
  for (let i = 0; i < 8; i++) {
    if (words[i] === 0) {
      if (curStart === -1) {
        curStart = i
        curLen = 1
      } else {
        curLen++
      }
      if (curLen > bestLen && curLen >= 2) {
        bestStart = curStart
        bestLen = curLen
      }
    } else {
      curStart = -1
      curLen = 0
    }
  }

  if (bestStart === -1) {
    return {
      host: Array.from(words, (w) => w.toString(16)).join(':'),
      isIPv6: true,
    }
  }

  const left = Array.from(words.slice(0, bestStart), (w) => w.toString(16)).join(':')
  const right = Array.from(words.slice(bestStart + bestLen), (w) => w.toString(16)).join(':')
  return {
    host: `${left}::${right}`,
    isIPv6: true,
  }
}

export function normalizeOrigin(raw: string): OriginValidationResult {
  if (raw === '' || raw.length > 16384 || raw.trim() !== raw || /[\r\n\t\\# ]/.test(raw)) {
    return {
      ok: false,
      error: 'HTTP 地址为空、过长或含有空白、反斜杠、片段',
    }
  }

  const schemeMatch = /^([a-zA-Z][a-zA-Z0-9+.-]*):\/\/(.*)$/.exec(raw)
  if (!schemeMatch) {
    return {
      ok: false,
      error: '仅支持没有内嵌凭据的绝对 HTTP/HTTPS 地址（如 https://example.com）',
    }
  }

  const scheme = schemeMatch[1].toLowerCase()
  const rest = schemeMatch[2]
  if (scheme !== 'http' && scheme !== 'https') {
    return {
      ok: false,
      error: '仅支持没有内嵌凭据的绝对 HTTP/HTTPS 地址',
    }
  }

  if (!rest || rest.includes('@')) {
    return {
      ok: false,
      error: '仅支持没有内嵌凭据的绝对 HTTP/HTTPS 地址',
    }
  }

  if (rest.includes('/') || rest.includes('?')) {
    return {
      ok: false,
      error: 'origin 只能包含协议、主机和端口，不能包含路径或查询',
    }
  }

  if (rest.endsWith(':')) {
    return {
      ok: false,
      error: '端口不能为空',
    }
  }

  let rawHost = ''
  let rawPort = ''
  let bracketedIPv6 = false

  if (rest.startsWith('[')) {
    const closeIdx = rest.indexOf(']')
    if (closeIdx === -1) {
      return { ok: false, error: 'IPv6 主机地址缺少右括号' }
    }
    rawHost = rest.slice(1, closeIdx)
    const afterBracket = rest.slice(closeIdx + 1)
    if (afterBracket !== '') {
      if (!afterBracket.startsWith(':')) {
        return { ok: false, error: 'HTTP 地址格式无效' }
      }
      rawPort = afterBracket.slice(1)
    }
    bracketedIPv6 = true
  } else {
    const firstColon = rest.indexOf(':')
    if (firstColon !== -1) {
      if (rest.indexOf(':', firstColon + 1) !== -1) {
        return { ok: false, error: 'IPv6 地址须使用方括号包裹，或端口格式无效' }
      }
      rawHost = rest.slice(0, firstColon)
      rawPort = rest.slice(firstColon + 1)
    } else {
      rawHost = rest
    }
  }

  if (rawHost === '' || /[%* \t\r\n]/.test(rawHost)) {
    return {
      ok: false,
      error: '主机不能为空或包含通配符、空白、IPv6 zone',
    }
  }

  let port = ''
  if (rawPort !== '') {
    if (!/^[0-9]+$/.test(rawPort)) {
      return { ok: false, error: '端口必须介于 1 与 65535 之间' }
    }
    const portNum = Number(rawPort)
    if (!Number.isInteger(portNum) || portNum < 1 || portNum > 65535) {
      return { ok: false, error: '端口必须介于 1 与 65535 之间' }
    }
    if (!((scheme === 'http' && portNum === 80) || (scheme === 'https' && portNum === 443))) {
      port = String(portNum)
    }
  }

  let canonicalHostStr = ''
  let isIPv6Host = false

  if (bracketedIPv6) {
    const lowered = rawHost.toLowerCase().endsWith('.') ? rawHost.toLowerCase().slice(0, -1) : rawHost.toLowerCase()
    const ipv6Words = parseStrictIPv6(lowered)
    if (!ipv6Words) {
      return { ok: false, error: 'IPv6 地址格式无效' }
    }
    const formatted = formatIPv6OrUnmapped(ipv6Words)
    canonicalHostStr = formatted.host
    isIPv6Host = formatted.isIPv6
  } else {
    const lowered = rawHost.toLowerCase().endsWith('.') ? rawHost.toLowerCase().slice(0, -1) : rawHost.toLowerCase()
    const ipv4 = parseStrictIPv4(lowered)
    if (ipv4) {
      canonicalHostStr = ipv4.join('.')
    } else if (/^[0-9.]+$/.test(lowered)) {
      return {
        ok: false,
        error: 'IPv4 地址须为标准四段十进制（0–255），不支持简写或八进制格式',
      }
    } else {
      if (lowered.length > 253) {
        return { ok: false, error: 'DNS 名称过长' }
      }
      const labels = lowered.split('.')
      for (const label of labels) {
        if (label.length === 0 || label.length > 63 || label.startsWith('-') || label.endsWith('-')) {
          return { ok: false, error: 'DNS 名称格式无效' }
        }
        for (let i = 0; i < label.length; i++) {
          const c = label.charCodeAt(i)
          const isAlphaNum = (c >= 97 && c <= 122) || (c >= 48 && c <= 57)
          if (!isAlphaNum && c !== 45) {
            return { ok: false, error: 'DNS 名称须为 ASCII；国际化域名请使用 punycode' }
          }
        }
      }
      canonicalHostStr = lowered
    }
  }

  const hostWithBrackets = isIPv6Host ? `[${canonicalHostStr}]` : canonicalHostStr
  const authority = port !== '' ? `${hostWithBrackets}:${port}` : hostWithBrackets
  return {
    ok: true,
    normalized: `${scheme}://${authority}`,
  }
}

export function parseOriginInputText(rawText: string): OriginListParseResult {
  const lines = rawText.split(/\r?\n/)
  const seen = new Set<string>()
  const origins: string[] = []
  const errors: Array<{ line: number; raw: string; error: string }> = []

  lines.forEach((lineText, idx) => {
    const segments = lineText.split(',')
    for (const seg of segments) {
      if (seg.trim() === '') continue
      const res = normalizeOrigin(seg.trim())
      if (!res.ok || !res.normalized) {
        errors.push({
          line: idx + 1,
          raw: seg.trim(),
          error: res.error || 'Origin 格式无效',
        })
      } else if (!seen.has(res.normalized)) {
        seen.add(res.normalized)
        origins.push(res.normalized)
      }
    }
  })

  origins.sort()
  return { origins, errors }
}
