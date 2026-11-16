/** 只包含调用方当前输入；undefined表示未输入，不承载已保存的秘密。 */
export interface SecretEntry { id: string; name: string; value?: string }
export interface SecretEntryOptions { savedNames?: string[]; caseInsensitive?: boolean; json?: boolean; requireValues?: boolean }
export type SecretEntryErrors = Record<string, { name?: string; value?: string }>
export const entryName = (name: string, caseInsensitive = false) => caseInsensitive ? name.toLowerCase() : name
export function validateSecretEntries(entries: SecretEntry[], options: SecretEntryOptions = {}): SecretEntryErrors {
  const errors: SecretEntryErrors = {}
  const seen = new Set<string>()
  const saved = new Set((options.savedNames ?? []).map(name => entryName(name, options.caseInsensitive)))
  for (const entry of entries) {
    const error: { name?: string; value?: string } = {}
    const name = entryName(entry.name, options.caseInsensitive)
    if (!entry.name.trim()) error.name = '请输入名称'
    else if (entry.name !== entry.name.trim()) error.name = '名称首尾不能有空白'
    else if (seen.has(name)) error.name = '名称不能重复'
    seen.add(name)
    if (entry.value === undefined) {
      if (options.requireValues || !saved.has(name)) error.value = '请输入值'
    } else if (options.json) {
      try { JSON.parse(entry.value) } catch { error.value = '请输入合法 JSON 值' }
    }
    if (error.name || error.value) errors[entry.id] = error
  }
  return errors
}
/** 语法检查与编码分离；JSON参数必须保留原文，不能消掉嵌套重复键。 */
export function encodeSecretEntries(entries: SecretEntry[], json = false): string {
  return `[${entries.map(entry => `{"name":${JSON.stringify(entry.name)}${entry.value === undefined ? '' : `,"value":${json ? entry.value : JSON.stringify(entry.value)}`}}`).join(',')}]`
}
