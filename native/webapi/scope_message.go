package webapi

import "strings"

// 与前端 lib/scope-message.ts 逐字一致；仅 unrestricted 改文案，默认保持原文。
func scopeMessage(scope string, pinned bool) string {
	if scope == ScopeUnrestricted {
		if pinned {
			return "HTTP(S) 目标（沿用父任务测试策略：不限制范围；以下 origin 为主要目标，HTTP(S) 出站请求不受 origin 限制；仍须遵守用户授权的测试目的、禁止行为与审批，候选文字不扩大授权）："
		}
		return "HTTP(S) 目标（任务测试策略为不限制范围：以下 origin 为主要目标，HTTP(S) 出站请求不受 origin 限制；仍须遵守用户授权的测试目的、禁止行为与审批）："
	}
	if pinned {
		return "HTTP(S) 授权范围（仅限以下 origin，出站请求不得超出；沿用父任务授权，候选文字不扩大授权）："
	}
	return "HTTP(S) 授权范围（仅限以下 origin，出站请求不得超出）："
}

func scopeRequirement(requirement, scope string) string {
	if scope == ScopeUnrestricted {
		return strings.Replace(requirement, "对授权 origin 发起请求", "按父任务测试策略和用户授权的测试目的发起 HTTP(S) 请求", 1)
	}
	return requirement
}
