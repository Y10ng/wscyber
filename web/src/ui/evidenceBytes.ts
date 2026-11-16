/** 已保存字节的本地视图；不读取网络，不以文本预览重建原件。 */
export const EVIDENCE_PREVIEW_LIMIT = 16_384
export type EvidenceViewMode = 'auto' | 'text' | 'hex' | 'base64'
export type DecodedEvidence =
  | { status: 'missing' }
  | { status: 'invalid'; error: string }
  | { status: 'available'; bytes: Uint8Array; base64: string; text: string | null; textIssue: string }

export function decodeEvidenceBytes(base64Value?: string, charset?: string): DecodedEvidence {
  // Go 的 []byte + omitempty 会省略空值；缺字段不是已知的零长度。
  if (base64Value === undefined) return { status: 'missing' }
  let binary: string
  try {
    // atob 会容忍空白和缺失 padding；API 使用标准、带 padding 的 Base64。
    if (typeof base64Value !== 'string' || base64Value.length % 4 !== 0 || !/^[A-Za-z0-9+/]*={0,2}$/.test(base64Value)) throw new Error('invalid')
    binary = globalThis.atob(base64Value)
    // 同时拒绝非零 padding bits，避免默默接受另一份编码内容。
    if (globalThis.btoa(binary) !== base64Value) throw new Error('invalid')
  } catch {
    return { status: 'invalid', error: 'Base64 数据无效，无法解码已保存字节。' }
  }
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0))
  let text: string | null = null
  let textIssue = ''
  try {
    const value = new TextDecoder(charset ?? 'utf-8', { fatal: true }).decode(bytes)
    if (/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/.test(value)) {
      textIssue = '内容含非文本控制字符；可查看十六进制或 Base64。'
    } else {
      // TextDecoder 的 BOM 处理属于解码文本视图，不宣称文本复制等于原字节。
      text = value
    }
  } catch {
    textIssue = '编码不受支持或字节无法按其解码；可查看十六进制或 Base64。'
  }
  return { status: 'available', bytes, base64: base64Value, text, textIssue }
}

export interface EvidencePreview {
  mode: Exclude<EvidenceViewMode, 'auto'>
  text: string
  limited: boolean
  unit: '源字节' | '字符'
  copyLabel: string
}

export function createEvidencePreview(evidence: Extract<DecodedEvidence, { status: 'available' }>, requestedMode: EvidenceViewMode): EvidencePreview {
  const mode = requestedMode === 'auto' || (requestedMode === 'text' && evidence.text === null)
    ? evidence.text === null ? 'hex' : 'text'
    : requestedMode
  const copyLabel = mode === 'text' ? '复制当前文本预览' : mode === 'hex' ? '复制当前十六进制预览' : '复制当前Base64预览'
  if (mode === 'base64') return { mode, text: evidence.base64.slice(0, EVIDENCE_PREVIEW_LIMIT), limited: evidence.base64.length > EVIDENCE_PREVIEW_LIMIT, unit: '字符', copyLabel }
  if (mode === 'text') {
    const value = evidence.text ?? ''
    let end = 0, characters = 0
    // 按 Unicode 码点计字符，避免在预览边界留下半个代理对。
    for (const character of value) {
      if (characters === EVIDENCE_PREVIEW_LIMIT) break
      end += character.length
      characters += 1
    }
    return { mode, text: value.slice(0, end), limited: end < value.length, unit: '字符', copyLabel }
  }
  const bytes = evidence.bytes.subarray(0, EVIDENCE_PREVIEW_LIMIT)
  const rows: string[] = []
  for (let offset = 0; offset < bytes.length; offset += 16) {
    const columns = Array.from(bytes.subarray(offset, offset + 16), (byte) => byte.toString(16).padStart(2, '0')).join(' ')
    rows.push(`${offset.toString(16).padStart(8, '0')}  ${columns}`)
  }
  return { mode, text: rows.join('\n'), limited: evidence.bytes.length > EVIDENCE_PREVIEW_LIMIT, unit: '源字节', copyLabel }
}

export function safeEvidenceFilename(filename: string): string {
  const safe = filename.replace(/[^a-zA-Z0-9._-]/g, '_').replace(/^\.+/, '').slice(0, 160)
  return safe || 'evidence.bin'
}

export function createEvidenceBlob(bytes: Uint8Array): Blob {
  return new Blob([new Uint8Array(bytes)], { type: 'application/octet-stream' })
}
