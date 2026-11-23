// 首条消息按任务当前策略说明 HTTP(S) 范围；与后端 scope_message.go 逐字一致。
export function scopeMessage(scope: string | undefined, pinned = false): string {
  if (scope === 'unrestricted') {
    return pinned
      ? 'HTTP(S) 目标（沿用父任务测试策略：不限制范围；以下 origin 为主要目标，HTTP(S) 出站请求不受 origin 限制；仍须遵守用户授权的测试目的、禁止行为与审批，候选文字不扩大授权）：'
      : 'HTTP(S) 目标（任务测试策略为不限制范围：以下 origin 为主要目标，HTTP(S) 出站请求不受 origin 限制；仍须遵守用户授权的测试目的、禁止行为与审批）：'
  }
  return pinned
    ? 'HTTP(S) 授权范围（仅限以下 origin，出站请求不得超出；沿用父任务授权，候选文字不扩大授权）：'
    : 'HTTP(S) 授权范围（仅限以下 origin，出站请求不得超出）：'
}

export function scopeRequirement(requirement: string, scope: string | undefined): string {
  return scope === 'unrestricted'
    ? requirement.replace('对授权 origin 发起请求', '按父任务测试策略和用户授权的测试目的发起 HTTP(S) 请求')
    : requirement
}
