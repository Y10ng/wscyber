package model

import (
	"fmt"
	"wscyber/internal/domain"
)

const verificationPrompt = `
本方向是独立候选验证，以下模式约束覆盖上方通用kind列表：不得finding、delegate或conclude；其余观察、动作、Skill、block仍可用。结束时kind必须verification。
verification_context.source是固定候选内容/引用时点；这些主张未确认，不应迎合原研究结论。原研究全文和原始流量未自动复制，未提供的内容不能声称已读。先核对当前实际前提与授权，通过新的受控动作检验候选主张并尝试反证；缺真实登录能力不得猜造或提取凭据。
结果载荷verification为{"verdict":"supported或counterevidence或inconclusive","method":"实际检验方法","preconditions":"实际可用前提，不能假称已登录","summary":"对固定候选的本次判读","limitations":"未验证部分/反证/限制","evidence":[{"kind":"action或observation","id":"本方向本次实际装配的原件ID","relation":"supporting或counterevidence或limitation"}]}。每段最多4096 UTF-8字节，证据0–16，总载荷24KiB。summary/next_step各最多1024字节，结束next_step可空。
supported和counterevidence至少需要同关系的新完成且完整捕获的HTTP证据。程序只验证真实来源与完整性，不以HTTP状态码判漏洞：500错误或302重定向可以是证据，200也不证明漏洞。原候选引用、仅模型观察、命令exit0、待审/未发送、unknown或截断不能单独支撑结论；失败/不完整尝试仍可作为limitation保留，不能删掉限制材料以迎合结论。实际登录失效、权限/环境前提不足、不可达或未复现不能证明主张成立/不成立，应使用inconclusive并写明实际理由；401/403须结合真实前提和内容判断，不能仅凭状态码。supported只是新证据支持的模型判读，不代表自动confirmed；counterevidence不代表所有环境都不成立。不得给fixed/confirmed或修复结论。未知动作不可重放。结果不覆盖原候选。有限步数包括结果保存决策，应在预算内收尾，不要求用尽预算。`

func ValidateDecisionForSnapshot(d domain.Decision, s domain.Snapshot) error {
	if err := ValidateDecision(d); err != nil {
		return err
	}
	if s.Verification == nil {
		if d.Kind == "verification" {
			return fmt.Errorf("普通研究不能提交独立验证结果")
		}
		return nil
	}
	if d.Kind == "finding" || d.Kind == "delegate" || d.Kind == "conclude" {
		return fmt.Errorf("独立验证不能改写候选、派生方向或无结构化结论结束")
	}
	return nil
}
