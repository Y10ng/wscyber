/** 不做execCommand回退，不把权限失败当成功；调用方负责显示局部反馈。 */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (!globalThis.navigator?.clipboard?.writeText) return false
    await navigator.clipboard.writeText(text)
    return true
  } catch { return false }
}
