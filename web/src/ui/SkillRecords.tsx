import type { ReactNode } from 'react'
import { Alert, Button, Descriptions, Empty, Space, Spin, Table, Tag, Tooltip } from 'antd'
import './materials.css'

// 纯受控组合；API、鉴权、分页身份和正文读取由业务入口负责。
export interface SkillUsageRow { item_id: string; name: string; successful_loads: string }
export interface SkillActivityRow {
  sequence: string; attempt_id: string; mission_id: string; thread_id: string; kind: string; created_at: string
  request: { query?: string; item_id?: string; source_token?: string; purpose?: string; load_id?: string }
  result: { status: string; load_id?: string; material?: { name: string }; items?: { id: string; name: string; description: string }[]; next_cursor?: string }
}
export const skillStatusLabels: Record<string, string> = { discovered: '目录已返回', loaded: '成功加载', released: '已退出上下文', unavailable: '不可用', stale: '来源已变化', capacity: '上下文已满', not_active: '不在当前上下文' }
export const skillKindLabels: Record<string, string> = { skill_discover: '发现', skill_load: '加载', skill_release: '释放' }
export interface SkillPagerProps { busy?: boolean; previous?: boolean; next?: boolean; onPrevious: () => void; onNext: () => void }
export function SkillPager({ busy, previous, next, onPrevious, onNext }: SkillPagerProps) {
  return <Space wrap><Button disabled={busy || !previous} onClick={onPrevious}>上一页</Button><Button disabled={busy || !next} onClick={onNext}>下一页</Button></Space>
}
export function SkillUsageList({ items, total, busy, error, pager }: { items: SkillUsageRow[]; total?: string; busy?: boolean; error?: string; pager: SkillPagerProps }) {
  return <section className="material-list-pane" aria-label="按Skill成功加载次数">
    <Space wrap><h3>成功加载记录</h3>{total !== undefined && <Tooltip title="计数来自不可变成功加载明细。目录曝光、失败、重复携带和浏览器读取不计入；不代表模型执行了指导。"><span>合计 {total} 次</span></Tooltip>}</Space>
    {error && <Alert type="error" showIcon title={error} />}
    <Table<SkillUsageRow> rowKey="item_id" size="small" pagination={false} loading={busy} dataSource={items} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={error ? '记录暂不可用' : '暂无成功加载'} /> }} columns={[
      { title: 'Skill（最近加载名称）', dataIndex: 'name', render: (name: string) => <span style={{ overflowWrap: 'anywhere' }}>{name}</span> },
      { title: '成功加载', dataIndex: 'successful_loads', width: 120, render: (value: string) => `${value} 次` },
    ]} />
    <SkillPager {...pager} busy={busy} />
  </section>
}
export function SkillActivityList({ items, busy, error, pager, onOpen }: { items: SkillActivityRow[]; busy?: boolean; error?: string; pager: SkillPagerProps; onOpen: (row: SkillActivityRow) => void }) {
  return <section className="material-list-pane" aria-label="Skill活动记录">{error && <Alert type="error" showIcon title={error} />}
    <Table<SkillActivityRow> rowKey="attempt_id" size="small" loading={busy} dataSource={items} pagination={false} scroll={{ x: 620 }} locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={error ? '活动暂不可用' : '暂无Skill活动'} /> }} columns={[
      { title: '时间 / 活动', width: 210, render: (_, row) => <Space wrap size={4}><span>{row.created_at}</span><Tag>{skillKindLabels[row.kind] || row.kind}</Tag></Space> },
      { title: '对象 / 目的', render: (_, row) => <div style={{ overflowWrap: 'anywhere' }}>{row.result.material?.name || row.request.query || row.request.item_id || row.request.load_id || '全部Skill目录'}{row.request.purpose && <div>{row.request.purpose}</div>}</div> },
      { title: '结果', width: 150, render: (_, row) => <Button type="link" onClick={() => onOpen(row)}>{skillStatusLabels[row.result.status] || row.result.status}</Button> },
    ]} /><SkillPager {...pager} busy={busy} />
  </section>
}
export function SkillActivityDetail({ activity, children, onClose }: { activity: SkillActivityRow; children?: ReactNode; onClose: () => void }) {
  return <section className="material-list-pane" aria-label="Skill活动详情"><Space wrap><h3>{skillKindLabels[activity.kind] || activity.kind}详情</h3><Tag>{skillStatusLabels[activity.result.status] || activity.result.status}</Tag><Button onClick={onClose}>返回记录</Button></Space>
    <Descriptions size="small" column={1} items={[
      { key: 'thread', label: '方向', children: activity.thread_id }, { key: 'attempt', label: '执行尝试', children: activity.attempt_id },
      { key: 'created', label: '时间', children: activity.created_at },
      ...(activity.request.purpose ? [{ key: 'purpose', label: '加载目的', children: activity.request.purpose }] : []),
    ]} />
    {activity.kind === 'skill_discover' && <><h3>返回的目录</h3>{!activity.result.items?.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="目录无匹配Skill" />}{activity.result.items?.map(item => <div className="material-row" key={item.id}><div className="material-row-text"><strong>{item.name}</strong><span>{item.description}</span></div></div>)}{activity.result.next_cursor && <Tag>当次目录还有下一页</Tag>}</>}
    {children}
  </section>
}
export interface SkillCallFact { id: string; model: string; outcome: string; attempted: boolean | null; started_at: string }
export function SkillCallFacts({ items, busy, error, pager, kind = 'skill' }: { items: SkillCallFact[]; busy?: boolean; error?: string; pager: SkillPagerProps; kind?: 'skill' | 'supplement' }) {
  return <section className="material-list-pane" aria-label={kind === 'skill' ? '固定Skill的模型调用关联' : '人工补充的模型调用关联'}><h3>模型调用关联</h3>{error && <Alert type="error" showIcon title={error} />}<Spin spinning={Boolean(busy)}>
    {!items.length && !busy && !error && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚无关联调用" />}
    {items.map(call => <div className="material-row" key={call.id}><div className="material-row-text"><strong>{call.model}</strong><span>{call.id}</span><span>{call.started_at}</span></div><Space wrap><Tag>{call.outcome}</Tag><Tooltip title={kind === 'skill' ? '已尝试发送不证明供应商完整接收；存在关联也不证明已执行Skill指导。' : '关联只证明Begin时装配了补充。已尝试发送不证明完整接收、理解或采纳；中断时发送阶段可能未知。'}><span>{call.attempted === null ? '发送阶段未知' : call.attempted ? '已尝试发送' : '未尝试发送'}</span></Tooltip></Space></div>)}
    </Spin><SkillPager {...pager} busy={busy} /></section>
}
