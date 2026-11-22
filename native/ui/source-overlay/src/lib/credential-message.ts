import type { WebApiAssetCredential } from '../api'

// 纯函数：组装与后端 credentialMessageLines（native/webapi/credential_placeholder.go）逐字
// 一致的“测试账号”块。首条消息只出现凭据 ID 与口令占位符，不含明文；空字段省略规则
// 与后端相同：名称/用户名为空时省略对应项，凭据 ID 始终显示，未设置 secret 时省略口令项。
// 无账号返回 []（调用方不应追加空块）。
export function buildCredentialMessageLines(credentials: WebApiAssetCredential[]): string[] {
  if (!credentials.length) return []
  const lines = ['测试账号（资产登记，仅用于上述授权范围内的测试）：']
  for (const credential of credentials) {
    const fields: string[] = []
    if (credential.label) fields.push(`名称：${credential.label}`)
    if (credential.username) fields.push(`用户名：${credential.username}`)
    fields.push(`凭据 ID：${credential.id}`)
    if (credential.has_secret) fields.push(`口令：用占位符 {{cred:${credential.id}.secret}}（平台发送时替换，你看不到明文）`)
    lines.push(`- ${fields.join('；')}`)
  }
  return lines
}

/** 首条消息 + 空行 + 账号块；无账号时原样返回。 */
export function appendCredentialMessage(base: string, lines: string[]): string {
  if (!lines.length) return base
  return `${base}\n\n${lines.join('\n')}`
}