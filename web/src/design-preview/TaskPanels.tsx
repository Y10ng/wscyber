import { useEffect, useRef } from 'react'
import { Alert, Button, Checkbox, Form, Input } from 'antd'
import { AdvancedConfiguration, SkillSelect, skillLabel, type TaskConfiguration } from './TaskConfiguration.tsx'
import { parseDemoOrigins } from './demoProgress.ts'
import type { TaskDraft } from './previewState.ts'
import type { DemoProject } from './types.ts'
import { TaskModal } from '../ui/TaskModal.tsx'

export function CreateTaskPanel({ open, draft, onChange, error, onClose, onCreate }: {
  open: boolean; draft: TaskDraft; onChange: (draft: TaskDraft) => void; error: string | null; onClose: () => void; onCreate: () => void
}) {
  const origins = parseDemoOrigins(draft.origins)
  const errorRef = useRef<HTMLDivElement>(null)
  useEffect(() => { if (open && error) errorRef.current?.scrollIntoView({ block: 'nearest' }) }, [open, error])
  return <TaskModal title="新建项目" width={880} open={open} onCancel={onClose} footer={<div className="modal-footer"><Button aria-label="保留创建草稿并关闭" onClick={onClose}>取消</Button><Button type="primary" aria-label="开始探索" onClick={onCreate}>开始探索</Button></div>}>
    <Form layout="vertical" requiredMark={false}>
      <div className="primary-inputs">
        <Form.Item label={<label htmlFor="task-target">目标／已知信息</label>} required>
          <Input.TextArea id="task-target" aria-label="目标／已知信息" value={draft.target} onChange={(e) => onChange({ ...draft, target: e.target.value })} rows={4} placeholder="测试接口、业务规则与可用身份" />
        </Form.Item>
        <Form.Item label={<label htmlFor="task-objective">希望达到的目标</label>} required>
          <Input.TextArea id="task-objective" aria-label="希望达到的目标" value={draft.objective} onChange={(e) => onChange({ ...draft, objective: e.target.value })} rows={4} placeholder="例如：验证不同租户之间的订单访问边界" />
        </Form.Item>
      </div>
      <section className="authorization-section"><h3>明确授权范围</h3><Form.Item label={<label htmlFor="task-origins">允许的 HTTP(S) origins</label>}>
        <Input.TextArea id="task-origins" aria-label="允许的 HTTP(S) origins" value={draft.origins} onChange={(e) => onChange({ ...draft, origins: e.target.value, authorized: false })} rows={2} placeholder="https://api.shop.example.com" />
      </Form.Item><p className="muted">仅接受保留测试域，不实际访问目标。</p>
        {origins && <div className="scope-preview" aria-label="规范化授权范围">{origins.map((origin) => <div key={origin}>{origin}</div>)}</div>}
        <Checkbox aria-label="确认明确目标授权" checked={draft.authorized} onChange={(e) => onChange({ ...draft, authorized: e.target.checked })}>我确认上述明确范围的授权；资料不授予访问权限。</Checkbox>
        {error && <div ref={errorRef} className="form-error" role="alert"><Alert type="error" showIcon title={error} /></div>}
      </section>
      <AdvancedConfiguration value={draft.configuration} onChange={(configuration) => onChange({ ...draft, configuration })} name={draft.name} onNameChange={(name) => onChange({ ...draft, name })} />
    </Form>
  </TaskModal>
}

export function ConfigurationPanel({ open, onClose, afterClose, restoreFocus = true, project, config, round, onSupplement }: {
  open: boolean; onClose: () => void; afterClose?: () => void; restoreFocus?: boolean; project: DemoProject; config: TaskConfiguration; round: number; onSupplement: () => void
}) {
  return <TaskModal title="任务配置" width={920} open={open} onCancel={onClose} afterClose={afterClose} focusable={{ focusTriggerAfterClose: restoreFocus }} footer={<div className="modal-footer"><Button aria-label="关闭任务配置" onClick={onClose}>关闭</Button><Button type="primary" aria-label="从配置补充资料" disabled={project.executionStatus !== '运行中'} onClick={onSupplement}>补充资料</Button></div>}>
    <h2 className="configuration-title">{project.name}</h2>
    <div className="configuration-grid">
      <section className="detail-document"><h3>研究目标</h3><label>目标与已知信息</label><p>{project.targetAndKnownInfo}</p><label>希望达到的目标</label><p>{project.desiredObjective}</p><label>明确授权范围</label><div className="plain-code">{project.allowedOrigins.join('\n')}</div></section>
      <section className="detail-document"><h3>运行配置</h3><label>模型原文记录</label><p>{config.saveModelIo ? '开启' : '关闭'} · 启动时固定，历史不可补录</p>
        <label>固定版本资料</label>{config.skills.length ? <ul className="configured-skills">{config.skills.map((ref) => <li key={ref.versionId}><strong>{skillLabel(ref.versionId)}</strong><span>第 {ref.effectiveFromRound} 轮起生效{ref.effectiveFromRound > round ? ' · 等待下一轮' : ''}</span></li>)}</ul> : <p className="muted">未选用资料</p>}
        <label>测试安全</label><p>禁止行为提示词：{config.prohibitedPromptEnabled ? '开启' : '关闭'}</p><pre className="plain-code">{config.prohibitedBehaviors || '未设置清单'}</pre><p className="muted">软约束；关闭不扩大授权、不撤销程序保护。</p>
      </section>
    </div>
  </TaskModal>
}

export function SupplementPanel({ open, onClose, afterClose, config, round, selected, onSelect, onSave }: {
  open: boolean; onClose: () => void; afterClose?: () => void; config: TaskConfiguration; round: number; selected: string[]; onSelect: (ids: string[]) => void; onSave: () => void
}) {
  return <TaskModal title="补充研究资料" width={800} open={open} onCancel={onClose} afterClose={afterClose} focusable={{ focusTriggerAfterClose: false }} footer={<div className="modal-footer"><Button aria-label="取消补充资料" onClick={onClose}>取消</Button><Button aria-label="确认补充资料" type="primary" disabled={!selected.length} onClick={onSave}>补充资料</Button></div>}><div className="detail-document">
    <h3>从第 {round + 1} 轮生效</h3><p>选用固定版本，历史输入不回写；资料不授予目标访问权限。</p>
    <SkillSelect value={selected} onChange={onSelect} excluded={config.skills.map((ref) => ref.versionId)} />
    <label>已选用版本</label>{config.skills.length ? <ul className="configured-skills">{config.skills.map((ref) => <li key={ref.versionId}><strong>{skillLabel(ref.versionId)}</strong><span>第 {ref.effectiveFromRound} 轮起生效</span></li>)}</ul> : <p className="muted">当前任务尚未选用资料</p>}
  </div></TaskModal>
}
