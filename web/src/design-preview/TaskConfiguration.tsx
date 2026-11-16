import { Button, Form, Input, Select, Space, Switch, Tag, Tree, Typography } from 'antd'
import { useState } from 'react'
import { GridFold } from './SurfaceControls.tsx'

const { Text } = Typography

// 固定本地文本；资料不会触发文件读取、上传、脚本或链接加载。
export const DEMO_SKILLS = [
  { id: 'tenant-check@1', folder: 'Web/API / 权限核验', name: '双身份对照', version: 'v1', body: '# 双身份对照\n\n先核对业务权限与资源归属，再比较自有身份和另一获授权身份的响应。保留正常权限反例；响应成功不直接等于漏洞。' },
  { id: 'tenant-check@2', folder: 'Web/API / 权限核验', name: '双身份对照', version: 'v2', body: '# 双身份对照\n\n固定资源、环境与身份前提，记录自有访问和跨身份访问的差异。补查公开资源、共享授权等正常功能解释。证据不足时保留候选。' },
  { id: 'evidence-note@1', folder: 'Web/API / 证据整理', name: '简洁证据记录', version: 'v1', body: '# 简洁证据记录\n\n描述观察、关联请求与响应、列出未验证前提。正文只保留问题描述、复现步骤和修复建议。未知效果不自动重放。' },
] as const

export interface SkillReference {
  versionId: string
  effectiveFromRound: number
}

export interface TaskConfiguration {
  saveModelIo: boolean
  prohibitedPromptEnabled: boolean
  prohibitedBehaviors: string
  skills: SkillReference[]
}

export function createDemoConfiguration(saveModelIo = false): TaskConfiguration {
  return {
    saveModelIo,
    prohibitedPromptEnabled: true,
    prohibitedBehaviors: '不删除或覆盖业务数据\n不干扰服务可用性\n不向第三方发送目标材料',
    skills: [],
  }
}

export function skillLabel(id: string) {
  const skill = DEMO_SKILLS.find((item) => item.id === id)
  return skill ? `${skill.name} · ${skill.version}` : id
}

export function SkillSelect({ value, onChange, excluded = [] }: {
  value: string[]
  onChange: (ids: string[]) => void
  excluded?: string[]
}) {
  return <Select
    mode="multiple"
    aria-label="选择固定版本 skill"
    placeholder="选择固定版本 skill"
    value={value}
    onChange={onChange}
    style={{ width: '100%' }}
    options={DEMO_SKILLS.map((skill) => ({
      value: skill.id,
      label: `${skill.folder} / ${skillLabel(skill.id)}`,
      disabled: excluded.includes(skill.id),
    }))}
  />
}

export function AdvancedConfiguration({ value, onChange, name, onNameChange }: {
  value: TaskConfiguration
  onChange: (value: TaskConfiguration) => void
  name: string
  onNameChange: (value: string) => void
}) {
  return <GridFold title="高级设置">
      <Form.Item label="项目名称（选填）">
        <Input value={name} onChange={(e) => onNameChange(e.target.value)} placeholder="留空按目标自动命名" />
      </Form.Item>
      <Form.Item label="资料选用 · 固定版本">
        <SkillSelect value={value.skills.map((ref) => ref.versionId)} onChange={(ids) => onChange({
          ...value, skills: ids.map((versionId) => ({ versionId, effectiveFromRound: 1 })),
        })} />
      </Form.Item>
      <Form.Item label="保存模型输入与返回">
        <Space wrap>
          <Switch aria-label="保存模型输入与返回" checked={value.saveModelIo} onChange={(saveModelIo) => onChange({ ...value, saveModelIo })} />
          <Text>{value.saveModelIo ? '开启' : '关闭'}</Text>
          <Text type="secondary">创建时固定；关闭后不保存原文。</Text>
        </Space>
      </Form.Item>
      <section className="dp-safety-settings" aria-label="测试安全">
        <h3>测试安全</h3>
        <Form.Item label="禁止行为提示词">
          <Space wrap>
            <Switch aria-label="禁止行为提示词" checked={value.prohibitedPromptEnabled} onChange={(prohibitedPromptEnabled) => onChange({ ...value, prohibitedPromptEnabled })} />
            <Text>提示词约束，不保证拦截</Text>
          </Space>
        </Form.Item>
        <Form.Item label="禁止行为清单（示例可编辑）">
          <Input.TextArea aria-label="禁止行为清单" rows={3} value={value.prohibitedBehaviors} disabled={!value.prohibitedPromptEnabled}
            onChange={(e) => onChange({ ...value, prohibitedBehaviors: e.target.value })} />
        </Form.Item>
        <Text type="secondary">关闭不扩大授权、不撤销现有程序保护。</Text>
      </section>
  </GridFold>
}

export function LibraryPreview({ selected, onSelect }: {
  selected: string[]
  onSelect: (ids: string[]) => void
}) {
  const [activeId, setActiveId] = useState<string>(DEMO_SKILLS[0].id)
  const active = DEMO_SKILLS.find((skill) => skill.id === activeId)!
  const folders = [...new Set(DEMO_SKILLS.map((skill) => skill.folder))]
  return <div className="library-layout">
    <aside className="library-tree" aria-label="资料文件夹树"><div className="pane-heading"><strong>方法与资料</strong></div>
      <Tree.DirectoryTree aria-label="资料版本目录" defaultExpandAll selectedKeys={[activeId]} onSelect={(keys) => {
        const id = String(keys[0])
        if (DEMO_SKILLS.some((skill) => skill.id === id)) setActiveId(id)
      }} treeData={[{ key: 'root', title: 'Web/API', children: folders.map((folder) => ({
        key: folder, title: folder.split(' / ')[1], children: DEMO_SKILLS.filter((skill) => skill.folder === folder).map((skill) => ({
          key: skill.id, title: `${skill.name} · ${skill.version}`, isLeaf: true,
        })),
      })) }]} />
    </aside>
    <article className="library-document scroll-pane"><header className="section-heading"><h2>{active.name}</h2><Tag>{active.version} · 固定版本</Tag></header>
      <pre className="library-body">{active.body}</pre>
      <div className="action-line"><Button type="primary" aria-label={selected.includes(active.id) ? '移出创建草稿' : '选入创建草稿'} onClick={() => onSelect(selected.includes(active.id)
        ? selected.filter((id) => id !== active.id) : [...selected, active.id])}>
        {selected.includes(active.id) ? '移出创建草稿' : '选入创建草稿'}
      </Button></div><p className="muted">仅作为研究方法参考，不授予目标访问权限。</p>
      <section className="library-selection"><h3>创建草稿已选</h3><div className="link-list">{selected.length ? selected.map((id) => <Tag key={id}>{skillLabel(id)}</Tag>) : <span className="muted">尚未选择资料</span>}</div></section>
    </article>
  </div>
}
