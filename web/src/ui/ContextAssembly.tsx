import { Alert, Descriptions, Empty, Skeleton, Space, Table, Tag, Tooltip } from 'antd'
import { GridFold } from './SurfaceControls.tsx'
import { SkillPager } from './SkillRecords.tsx'
import './materials.css'

export interface ContextSource {
  kind: string; id: string; state?: string; at: string; original_bytes: number; included_bytes: number
  included: boolean; required: boolean; reason: string; content_sha256?: string
}
export interface ContextWindow { kind: string; limit: number; older_exists: boolean; cutoff_at?: string; cutoff_id?: string }
export interface ContextAssembly {
  policy: string; mission_id: string; thread_id: string; attempt_id: string; assembled_at: string
  input_bytes: number; max_input_bytes: number; sources: ContextSource[]; windows: ContextWindow[]
}
export interface ModelCallContext { call_id: string; mission_id: string; thread_id: string; recorded: boolean; assembly: ContextAssembly | null }
export const contextKindLabels: Record<string, string> = { mission: '项目', thread: '方向', observation: '观察', action: '动作', attempt: '执行尝试', material: '固定资料', research_brief: '身份与业务前提', safety_prompt: '安全清单', skill: 'Skill原件', skill_activity: 'Skill活动', supplement: '人工补充', finding: '候选发现（固定内容）', verification_source: '独立验证的固定候选' }
const stateLabels: Record<string, string> = { counterevidence: '反证', contradiction: '矛盾', refutation: '反驳', action_result: '动作结果', model_observation: '模型观察', other: '其他观察', pending: '待审批', approved: '已批准', running: '进行中', succeeded: '成功', failed: '失败', rejected: '已拒绝', outcome_unknown: '效果未知', interrupted: '中断', waiting_approval: '等待审批', ready: '待运行', blocked: '受阻', paused: '已暂停', concluded: '已结束', shared: '明确分享', enabled: '启用', active: '已加载', user_statement: '用户声明', discovered: '目录返回', loaded: '加载', released: '释放', unavailable: '不可用', stale: '已变化', capacity: '已达容量', not_active: '未加载', candidate: '候选 · 未验证' }
export const CONTEXT_PAGE_SIZE = 20
// 不读取正文、不请求网络；调用事实/鉴权及过期响应由业务层负责。
export function ContextAssemblyView({ value, attempted, page = 0, onPage, busy, error }: { value?: ModelCallContext | null; attempted: boolean | null; page?: number; onPage: (page: number) => void; busy?: boolean; error?: string }) {
  if (error) return <Alert type="error" showIcon title={error} />
  if (busy || !value) return <Skeleton active />
  const a = value.assembly
  if (!value.recorded || !a) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="本次未记录上下文装配" />
  const current = Math.min(Math.max(0, page), Math.max(0, Math.ceil(a.sources.length / CONTEXT_PAGE_SIZE) - 1))
  return <section className="material-list-pane" aria-label="本次上下文装配">
    <Space wrap><Tag>已装配</Tag><Tooltip title="装配记录不是发送回执；尝试发送不证明供应商完整接收、理解或采用。"><Tag>{attempted === null ? '发送阶段未知' : attempted ? '已尝试发送' : '未尝试发送'}</Tag></Tooltip><Tag>模型理解未证实</Tag></Space>
    <Descriptions size="small" column={1} items={[
      { key: 'at', label: '装配时间', children: a.assembled_at },
      { key: 'input', label: '系统与用户输入', children: `${a.input_bytes.toLocaleString()} / ${a.max_input_bytes.toLocaleString()} 字节` },
    ]} />
    <GridFold title="历史读取范围"><div className="material-list-pane">{a.windows.map(w => <div key={w.kind} style={{ overflowWrap: 'anywhere' }}><Space wrap><strong>{contextKindLabels[w.kind]}</strong><span>最近 {w.limit} 条窗口</span><Tag color={w.older_exists ? 'warning' : undefined}>{w.older_exists ? '更早历史未全读' : '窗口覆盖当时历史'}</Tag></Space>{w.cutoff_id && <div><time>{w.cutoff_at}</time> · <code>{w.cutoff_id}</code></div>}{w.older_exists && <Tooltip title={w.kind === 'finding' ? '更早候选未全部读取，可能含支持、反证和未验证限制；不能据未见判定不存在。' : '窗口外的显式反证与未决/效果未知动作另作必需项；其他更早历史未读取，可能含未分类反证。'}><Tag>未读内容不能视为不存在</Tag></Tooltip>}</div>)}</div></GridFold>
    <Table<ContextSource> size="small" rowKey={row => `${row.kind}:${row.id}`} pagination={false} dataSource={a.sources.slice(current * CONTEXT_PAGE_SIZE, (current + 1) * CONTEXT_PAGE_SIZE)} scroll={{ x: 540 }} columns={[
      { title: '来源 / 引用', render: (_, row) => <div style={{ overflowWrap: 'anywhere' }}><Space wrap size={4}><strong>{contextKindLabels[row.kind] ?? row.kind}</strong>{row.required && <Tag>必需</Tag>}</Space><div><code>{row.id}</code></div>{row.state && <span>{stateLabels[row.state] ?? '未知状态'}</span>}</div> },
      { title: '本次选择', width: 116, render: (_, row) => <Tag color={row.included ? undefined : 'warning'}>{row.included ? '已装配' : row.reason === 'item_limit' ? '单项超限' : '历史预算未选'}</Tag> },
      { title: '装配字节', width: 100, render: (_, row) => <Tooltip title={`原字段/原件 ${row.original_bytes} 字节；JSON转义可能增加大小。`}>{row.included_bytes.toLocaleString()}</Tooltip> },
    ]} expandable={{ expandedRowRender: row => <Descriptions size="small" column={1} items={[{ key: 'time', label: '来源 / 读取时间', children: row.at }, ...(row.content_sha256 ? [{ key: 'hash', label: '原件 SHA256', children: <code style={{ overflowWrap: 'anywhere' }}>{row.content_sha256}</code> }] : [])]} /> }} />
    <Space wrap><span>第 {current + 1} / {Math.max(1, Math.ceil(a.sources.length / CONTEXT_PAGE_SIZE))} 页 · {a.sources.length} 条引用</span><SkillPager previous={current > 0} next={(current + 1) * CONTEXT_PAGE_SIZE < a.sources.length} onPrevious={() => onPage(current - 1)} onNext={() => onPage(current + 1)} /></Space>
  </section>
}
