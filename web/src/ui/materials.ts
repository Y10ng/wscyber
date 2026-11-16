// 共享组合只处理受控数据与本地文本；不含请求、业务鉴权或示例数据。
export interface MaterialItem {
  id: string
  parent_id: string
  kind: 'folder' | 'file'
  classification?: 'material' | 'skill' // 旧不可变回执可能省略；只显式skill展示为Skill。
  name: string
  description: string
  format: '' | 'markdown' | 'text'
  content_sha256: string
  byte_size: number
  token: string
  created_at: string
  updated_at: string
}
export interface MaterialDetail extends MaterialItem { content: string }
export interface MaterialDraft { parent_id: string; kind: 'folder' | 'file'; classification?: 'material' | 'skill'; name: string; description: string; content: string }
export const materialBytes = (value: string) => new TextEncoder().encode(value).length
export function validUTF8Text(value: string): boolean {
  // TextEncoder 会替换孤立代理项，先拒绝，不能静默改变用户原文。
  return new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(new TextEncoder().encode(value)) === value
}
export function validateMaterial(draft: MaterialDraft): string {
  const name = draft.name.trim()
  if (!name || materialBytes(name) > 256 || /[\u0000-\u001f\u007f-\u009f/\\]/.test(name) || name === '.' || name === '..' || !validUTF8Text(name)) return '名称须为 1–256 字节，不能包含控制字符或路径分隔符。'
  if (materialBytes(draft.description) > 1024 || draft.description.includes('\0') || !validUTF8Text(draft.description)) return '说明须为不含 NUL 的有效文本且不超过 1,024 字节。'
  if (draft.classification && !['material', 'skill'].includes(draft.classification)) return '请选择普通资料或 Skill。'
  if (draft.classification === 'skill' && (draft.kind !== 'file' || !draft.description.trim())) return 'Skill 须为文本文件，并填写适用说明。'
  if (draft.kind === 'folder') return draft.content ? '文件夹不能包含正文。' : ''
  if (!/\.(md|txt)$/i.test(name)) return '资料名称须以 .md 或 .txt 结尾。'
  if (!draft.content || materialBytes(draft.content) > 32768 || draft.content.includes('\0') || !validUTF8Text(draft.content)) return '正文须为非空 UTF-8 文本，不含 NUL，且不超过 32,768 字节。'
  return ''
}
export async function importMaterialText(file: Pick<File, 'name' | 'size' | 'arrayBuffer'>): Promise<{ name: string; content: string }> {
  if (!/\.(md|txt)$/i.test(file.name)) throw new Error('请选择一个 .md 或 .txt 文件。')
  if (file.size < 1 || file.size > 32768) throw new Error('文件须为 1–32,768 字节。')
  const bytes = await file.arrayBuffer()
  if (bytes.byteLength !== file.size || bytes.byteLength > 32768) throw new Error('文件读取长度异常。')
  let content: string
  try { content = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes) } catch { throw new Error('文件不是有效 UTF-8 文本。') }
  const error = validateMaterial({ parent_id: '', kind: 'file', name: file.name, description: '', content })
  if (error) throw new Error(error)
  return { name: file.name, content }
}
export function materialSelectionError(items: MaterialItem[]): string {
  if (items.length > 16) return '每个任务最多选择 16 个资料。'
  if (new Set(items.map(item => item.id)).size !== items.length || items.some(item => item.kind !== 'file')) return '只能选择不同的文本资料。'
  if (items.reduce((sum, item) => sum + item.byte_size, 0) > 65536) return '所选资料正文合计不能超过 65,536 字节。'
  return ''
}
export function materialPath(item: Pick<MaterialItem, 'parent_id' | 'name'>, folders: MaterialItem[]): string {
  const names = [item.name], seen = new Set<string>()
  let parent = item.parent_id
  while (parent && !seen.has(parent)) {
    seen.add(parent)
    const folder = folders.find(value => value.id === parent)
    if (!folder) break
    names.unshift(folder.name); parent = folder.parent_id
  }
  return '/' + names.join('/')
}
