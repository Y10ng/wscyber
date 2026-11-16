import { materialBytes, validUTF8Text } from './materials.ts'

export interface ResearchIdentity { label: string; role: string; ownership?: string }
export interface ResearchBrief { identities?: ResearchIdentity[]; business_rules?: string; preconditions?: string; share_with_model: boolean }
export type BriefErrors = Record<string, string>
export const emptyResearchBrief = (): ResearchBrief => ({ share_with_model: false })
// 与Go strings.TrimSpace的Unicode White_Space一致，特意不使用JS trim（其包含FEFF、不含0085）。
export const trimBriefSpace = (s: string) => s.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
export const hasBriefText = (b?: ResearchBrief) => Boolean(b && (b.identities?.length || b.business_rules || b.preconditions))
export const hasBriefDraft = (b?: ResearchBrief) => hasBriefText(b) || Boolean(b?.share_with_model)
// 保留原文本及顺序，规范化可选空字段；只做深复制，不隐式打开分享。
export function cloneResearchBrief(b: ResearchBrief): ResearchBrief {
  return { ...(b.identities?.length ? { identities: b.identities.map(i => ({ label: i.label, role: i.role, ...(i.ownership ? { ownership: i.ownership } : {}) })) } : {}), ...(b.business_rules ? { business_rules: b.business_rules } : {}), ...(b.preconditions ? { preconditions: b.preconditions } : {}), share_with_model: b.share_with_model }
}
export const researchBriefPayload = (b?: ResearchBrief) => b && hasBriefText(b) ? cloneResearchBrief(b) : undefined
export const briefIdentity = (b?: ResearchBrief) => JSON.stringify(b ? cloneResearchBrief(b) : null)
function validText(s: string, max: number, multiline = false) {
  if (!validUTF8Text(s) || materialBytes(s) > max) return false
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i)
    if (!multiline && (c === 0x2028 || c === 0x2029)) return false
    if (c >= 32 && (c < 127 || c > 159)) continue
    if (multiline && (c === 9 || c === 10 || c === 13 && s.charCodeAt(i + 1) === 10)) continue
    return false
  }
  return true
}
export function validateResearchBrief(b: ResearchBrief): BriefErrors {
  const errors: BriefErrors = {}, identities = b.identities ?? [], seen = new Set<string>()
  if (typeof b.share_with_model !== 'boolean') errors.share = '请明确选择是否提供给研究模型。'
  if (identities.length > 8) errors.group = '身份说明最多 8 项。'
  let total = materialBytes(b.business_rules || '') + materialBytes(b.preconditions || '')
  identities.forEach((item, index) => {
    const key = trimBriefSpace(item.label)
    if (!key || !validText(item.label, 128)) errors[`${index}-label`] = '别名必填，最多 128 字节，不含换行或控制字符。'
    else if (seen.has(key)) errors[`${index}-label`] = '别名去首尾空白后不能重复。'
    seen.add(key)
    if (!trimBriefSpace(item.role) || !validText(item.role, 512)) errors[`${index}-role`] = '角色必填，最多 512 字节，不含换行或控制字符。'
    if (!validText(item.ownership || '', 1024)) errors[`${index}-ownership`] = '资源归属最多 1,024 字节，不含换行或控制字符。'
    total += materialBytes(item.label) + materialBytes(item.role) + materialBytes(item.ownership || '')
  })
  if (!validText(b.business_rules || '', 8192, true)) errors.rules = '业务规则最多 8,192 字节；控制字符仅允许 TAB、LF 和 CRLF。'
  if (!validText(b.preconditions || '', 4096, true)) errors.preconditions = '复现前提最多 4,096 字节；控制字符仅允许 TAB、LF 和 CRLF。'
  if (!identities.length && !trimBriefSpace(b.business_rules || '') && !trimBriefSpace(b.preconditions || '')) errors.group = '请填写身份说明、业务规则或复现前提。'
  if (total > 16384) errors.group = '说明正文合计最多 16,384 字节。'
  // Go encoding/json会额外转义HTML及行分隔符；容量以相同编码计，不改提交文本。
  const encoded = Array.from(JSON.stringify(cloneResearchBrief(b)), c => [60, 62, 38, 0x2028, 0x2029].includes(c.charCodeAt(0)) ? String.fromCharCode(92) + 'u' + c.charCodeAt(0).toString(16).padStart(4, '0') : c).join('')
  if (materialBytes(encoded) > 32768) errors.group = '说明编码后最多 32,768 字节。'
  return errors
}
export function isResearchBrief(value: unknown): value is ResearchBrief {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const b = value as Record<string, unknown>
  if (Object.keys(b).some(k => !['identities', 'business_rules', 'preconditions', 'share_with_model'].includes(k)) || typeof b.share_with_model !== 'boolean') return false
  if (('business_rules' in b && typeof b.business_rules !== 'string') || ('preconditions' in b && typeof b.preconditions !== 'string')) return false
  if ('identities' in b && (!Array.isArray(b.identities) || b.identities.some(i => !i || typeof i !== 'object' || Array.isArray(i) || Object.keys(i).some(k => !['label', 'role', 'ownership'].includes(k)) || typeof i.label !== 'string' || typeof i.role !== 'string' || ('ownership' in i && typeof i.ownership !== 'string')))) return false
  return Object.keys(validateResearchBrief(value as ResearchBrief)).length === 0
}
