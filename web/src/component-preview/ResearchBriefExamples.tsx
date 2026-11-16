import { useState } from 'react'
import { Button, Space, Tag } from 'antd'
import { Example } from './examples.tsx'
import { ResearchBriefEditor, ResearchBriefSummary } from '../ui/ResearchBrief.tsx'
import { emptyResearchBrief, validateResearchBrief, type ResearchBrief, type BriefErrors } from '../ui/researchBrief.ts'

const one: ResearchBrief = { identities: [{ label: '成员A', role: '普通成员', ownership: '本人组织内自建记录' }], business_rules: '用户声明：仅允许访问本人记录。\n<script>不是HTML执行入口</script>', preconditions: '仅本机合成材料，未执行登录。', share_with_model: false }
export function ResearchBriefExamples() {
  const [value, setValue] = useState<ResearchBrief>(emptyResearchBrief()), [errors, setErrors] = useState<BriefErrors>({}), [disabled, setDisabled] = useState(false)
  return <><Example title="身份说明与业务前提 · 组件fixture"><Space wrap><Tag>仅组件样例</Tag><Button onClick={() => { setValue(emptyResearchBrief()); setErrors({}) }}>空样例</Button><Button onClick={() => { setValue(one); setErrors({}) }}>单身份样例</Button><Button onClick={() => { setValue({ ...one, identities: Array.from({ length: 8 }, (_, i) => ({ label: `成员${i + 1}`, role: '普通成员'.repeat(16), ownership: '同组织记录'.repeat(40) })) }); setErrors({}) }}>八身份长文本</Button><Button onClick={() => setErrors(validateResearchBrief(value))}>校验样例</Button><Button aria-pressed={disabled} onClick={() => setDisabled(!disabled)}>切换禁用</Button></Space><ResearchBriefEditor value={value} disabled={disabled} errors={errors} onChange={next => { setValue(next); setErrors({}) }} /></Example>
    <Example title="固定说明 · 纯文本与分享意图"><ResearchBriefSummary value={one} /><ResearchBriefSummary /></Example></>
}
