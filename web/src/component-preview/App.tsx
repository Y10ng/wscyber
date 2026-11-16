import { useState } from 'react'
import { Button, Input, Table, Tag } from 'antd'
import { AppstoreOutlined, BarsOutlined, FileTextOutlined, FormOutlined, LayoutOutlined, NotificationOutlined, ExperimentOutlined } from '@ant-design/icons'
import { UIProvider } from '../ui/UIProvider.tsx'
import { AppShell } from '../ui/AppShell.tsx'
import type { SurfaceTheme } from '../ui/surface.ts'
import { Basics, DataExamples, Example, FeedbackExamples, LayoutExamples } from './examples.tsx'
import { FormExamples } from './FormExamples.tsx'
import { SecretEntriesExamples } from './SecretEntriesExamples.tsx'
import { DailyUsageExamples } from './DailyUsageExamples.tsx'
import { ModalExamples } from './ModalExamples.tsx'
import { coverage } from './coverage.ts'
import { MaterialExamples } from './MaterialExamples.tsx'
import { SkillExamples } from './SkillExamples.tsx'
import { ResearchControlsExamples } from './ResearchControlsExamples.tsx'
import { ResearchBriefExamples } from './ResearchBriefExamples.tsx'
import { ContextAssemblyExamples } from './ContextAssemblyExamples.tsx'
import { FindingExamples } from './FindingExamples.tsx'
import { VerificationExamples } from './VerificationExamples.tsx'
import './styles.css'

const pages = [
  { key: '基础', label: '主题与基础', icon: <AppstoreOutlined /> },
  { key: '布局', label: '布局与导航', icon: <LayoutOutlined /> },
  { key: '表单', label: '表单与配置', icon: <FormOutlined /> },
  { key: '数据', label: '数据与详情', icon: <FileTextOutlined /> },
  { key: '反馈', label: '弹层与反馈', icon: <NotificationOutlined /> },
  { key: '研究组合', label: '研究业务组合', icon: <ExperimentOutlined /> },
  { key: '矩阵', label: '规划覆盖矩阵', icon: <BarsOutlined /> },
]
export function App() {
  const [appearance, setAppearance] = useState<SurfaceTheme>('light'), [page, setPage] = useState('基础'), [query, setQuery] = useState('')
  const items = coverage.filter((row) => (page === '矩阵' || row.category === page) && `${row.name} ${row.status} ${row.boundary}`.includes(query))
  return <UIProvider appearance={appearance}><AppShell items={pages} activeKey={page} onNavigate={(key) => { setPage(key); setQuery('') }} title={<h1>{pages.find((item) => item.key === page)?.label}</h1>}
    actions={<Tag>UI-1 · 组件样例 · 非业务入口</Tag>} footer={<Button aria-label="切换浅暗主题" aria-pressed={appearance === 'dark'} onClick={() => setAppearance(appearance === 'light' ? 'dark' : 'light')}>{appearance === 'light' ? '浅色' : '暗色'}</Button>}>
    <div className="catalog-content" key={page}>
      {page === '基础' && <Basics />}{page === '布局' && <LayoutExamples />}{page === '表单' && <><FormExamples /><SecretEntriesExamples /></>}{page === '数据' && <><DailyUsageExamples /><DataExamples /></>}{page === '反馈' && <><ModalExamples /><FeedbackExamples /></>}
      {page === '研究组合' && <><MaterialExamples /><SkillExamples /><ResearchBriefExamples /><ResearchControlsExamples /><ContextAssemblyExamples /><FindingExamples /><VerificationExamples /><Example title="其他组合的设计样例"><Button href="http://127.0.0.1:4175/" target="_blank" rel="noopener noreferrer">打开本地设计模板</Button></Example></>}
      <Example title={page === '矩阵' ? '完整规划覆盖矩阵' : '本类覆盖与边界'}><Input.Search aria-label="搜索覆盖矩阵" placeholder="搜索组件、状态或约束" allowClear value={query} onChange={(e) => setQuery(e.target.value)} /><Table rowKey="key" size="small" dataSource={items} pagination={false} scroll={{ x: 800 }} columns={[
        { title: '组件 / 组合', dataIndex: 'name', width: 240 },
        { title: '状态', dataIndex: 'status', width: 120, filters: ['可复用', '仅展示', '待补', '本期不适用'].map((value) => ({ text: value, value })), onFilter: (value, row) => row.status === value, render: (text: string) => <Tag>{text}</Tag> },
        { title: '实现落点', dataIndex: 'path', width: 260, render: (text: string) => <code className="path-text">{text}</code> },
        { title: '使用约束', dataIndex: 'boundary' },
      ]} /></Example>
    </div>
  </AppShell></UIProvider>
}
