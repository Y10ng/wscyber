import { useState, type ReactNode } from 'react'
import { App, Alert, Badge, Breadcrumb, Button, Descriptions, Empty, Input, List, Popover, Radio, Result, Skeleton, Space, Spin, Steps, Table, Tabs, Tag, Timeline, Tooltip, Tree, Typography } from 'antd'
import { ExperimentOutlined, FolderOpenOutlined } from '@ant-design/icons'
import { CopyButton, GridFold, StreamText } from '../ui/SurfaceControls.tsx'
import { EvidenceExamples } from './EvidenceExamples.tsx'

export function Example({ title, children }: { title: string; children: ReactNode }) {
  return <section className="example"><h2>{title}</h2>{children}</section>
}
export function Basics() {
  const [clicks, setClicks] = useState(0)
  return <div className="example-grid">
    <Example title="文字与语义"><h3>当前问题与证据</h3><p>正文 14px，长内容自然换行。</p><p className="muted">次要信息不靠缩小字号区分。</p><p><ExperimentOutlined /> 研究方向 <code>REQ-2026-001</code></p><Space wrap><Tag>待验证</Tag><Tag color="success">已核对</Tag><Badge status="warning" text="效果未知" /></Space></Example>
    <Example title="按钮与链接"><Space wrap><Button type="primary" onClick={() => setClicks(clicks + 1)}>执行本地交互</Button><Button onClick={() => setClicks(0)}>重置</Button><Button disabled>不可用</Button><Button loading>处理中</Button><Button danger onClick={() => setClicks(0)}>清空计数</Button></Space><p role="status">已点击 {clicks} 次</p><Typography.Link href="#基础" onClick={(event) => { event.preventDefault(); setClicks(clicks + 1) }}>本页链接操作</Typography.Link></Example>
    <Example title="浮层与复制"><Space wrap><Tooltip title="说明只在需要时出现"><Button>聚焦查看提示</Button></Tooltip><Popover title="组件约束" content={<p>内容来自本地样例，不加载外链。</p>} trigger="click"><Button>查看浮层</Button></Popover><CopyButton text="wscyber · 本地组件样例" label="复制样例文本" /></Space></Example>
  </div>
}
export function LayoutExamples() {
  const [crumb, setCrumb] = useState('组件'), [revision, setRevision] = useState(0)
  return <div className="example-grid">
    <Example title="面包屑与页签"><Breadcrumb items={[{ title: <Button type="link" onClick={() => setCrumb('组件')}>组件</Button> }, { title: <Button type="link" onClick={() => setCrumb('布局')}>布局</Button> }]} /><p role="status">当前位置：{crumb}</p><Tabs items={[{ key: 'one', label: '概览', children: <p>页签由官方 Ant Design 管理键盘行为。</p> }, { key: 'two', label: '详情', children: <p>切换不提交任何业务操作。</p> }]} /></Example>
    <Example title="折叠与局部滚动"><GridFold title="展开高级选项"><Input aria-label="折叠内输入" placeholder="收起后不会进入Tab序列" /></GridFold><div className="example-scroll" tabIndex={0} aria-label="局部滚动样例">{Array.from({ length: 12 }, (_, i) => <p key={i}>第 {i + 1} 行 · 只滚动当前区域，长文本保持可读。</p>)}</div></Example>
    <Example title="新增短文字"><Button onClick={() => setRevision(revision + 1)}>插入新样例</Button>{revision > 0 && <StreamText key={revision} text={`新增本地样例 ${revision}，不是模型返回。`} />}<p className="muted">减少动效时直接显示全部文字。</p></Example>
  </div>
}
const rows = Array.from({ length: 17 }, (_, i) => ({ key: String(i), name: `示例方向 ${String(i + 1).padStart(2, '0')}`, state: i % 2 ? '待验证' : '已核对', count: i + 1 }))
export function DataExamples() {
  const [query, setQuery] = useState(''), [selected, setSelected] = useState('尚未选择')
  return <div className="example-grid">
    <Example title="表格与查询"><Input.Search aria-label="搜索示例方向" placeholder="搜索示例方向" allowClear value={query} onChange={(e) => setQuery(e.target.value)} /><Table size="small" rowSelection={{}} dataSource={rows.filter((row) => row.name.includes(query))} pagination={{ pageSize: 5, showSizeChanger: false }} scroll={{ x: 440 }} columns={[
      { title: '方向', dataIndex: 'name', sorter: (a, b) => a.name.localeCompare(b.name) },
      { title: '状态', dataIndex: 'state', filters: [{ text: '待验证', value: '待验证' }, { text: '已核对', value: '已核对' }], onFilter: (value, row) => row.state === value },
      { title: '序号', dataIndex: 'count', sorter: (a, b) => a.count - b.count },
    ]} /></Example>
    <Example title="资料树与详情"><Tree showIcon defaultExpandAll onSelect={(_keys, info) => setSelected(String(info.node.title))} treeData={[{ key: 'root', title: '示例资料', icon: <FolderOpenOutlined />, children: [{ key: 'v1', title: '双身份对照 · v1' }, { key: 'v2', title: '较长的固定资料版本名称，用来检查换行而不是隐藏重要内容 · v2' }] }]} /><Descriptions column={1} items={[{ key: 'selection', label: '已选资料', children: selected }, { key: 'source', label: '来源', children: '本地展示数据' }]} /></Example>
    <Example title="列表与过程"><List dataSource={['候选不等于确认', '编号不证明有效']} renderItem={(item) => <List.Item>{item}</List.Item>} /><Steps size="small" current={1} items={[{ title: '接收' }, { title: '核对' }, { title: '交回' }]} /><Timeline items={[{ content: '示例：记录观察' }, { content: '示例：保留反证' }]} /></Example>
    <EvidenceExamples />
    <Example title="不可信内容只作文本"><pre className="example-code">{'<img src="https://untrusted.invalid/pixel" onerror="alert(1)">\n' + JSON.stringify({ state: 'unknown', note: '不自动重放', path: '/very-long-example-path/'.repeat(8) }, null, 2)}</pre></Example>
  </div>
}
export function FeedbackExamples() {
  const { message } = App.useApp()
  const [state, setState] = useState('空'), [retry, setRetry] = useState(0)
  return <div className="example-grid">
    <Example title="局部状态"><Radio.Group aria-label="切换状态样例" value={state} onChange={(e) => setState(e.target.value)} optionType="button" options={['空', '加载', '错误', '无权限']} />
      <div className="state-sample">{state === '空' ? <Empty description="暂无材料" /> : state === '加载' ? <><Spin aria-label="加载中" /><Skeleton active paragraph={{ rows: 2 }} /></> : state === '错误' ? <Alert type="error" showIcon title="读取失败（示例）" description={`本地重试计数：${retry}`} action={<Button onClick={() => setRetry(retry + 1)}>重试读取</Button>} /> : <Result status="403" title="无权查看此材料" subTitle="请联系项目管理员" />}</div>
    </Example>
    <Example title="短通知"><Space wrap><Button onClick={() => { void message.info('本地样例已切换') }}>显示通知</Button><Button onClick={() => { void message.error('本地样例：读取失败') }}>显示错误</Button></Space><Alert type="warning" showIcon title="请求效果未知" description="核对结果前不自动重放。" /></Example>
  </div>
}
