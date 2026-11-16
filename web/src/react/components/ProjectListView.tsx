import React, { useState } from 'react'
import { Alert, Button, Empty, Input, Skeleton, Table, Tag, Typography } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { PlusOutlined, ReloadOutlined, SearchOutlined } from '@ant-design/icons'
import type { Mission } from '../types.ts'
import { formatDateTime } from '../types.ts'

interface ProjectListViewProps {
  missions: Mission[]
  initialLoading: boolean
  refreshing: boolean
  error: string
  hasActiveSubmission: boolean
  hasUnsavedDraft: boolean
  createLabel: string
  createButtonRef?: React.Ref<HTMLButtonElement | HTMLAnchorElement>
  onRefresh: () => void
  onCreateNew: () => void
  onOpenMission: (missionId: string) => void
}

export const ProjectListView: React.FC<ProjectListViewProps> = ({
  missions,
  initialLoading,
  refreshing,
  error,
  hasActiveSubmission,
  hasUnsavedDraft,
  createLabel,
  createButtonRef,
  onRefresh,
  onCreateNew,
  onOpenMission,
}) => {
  const [query, setQuery] = useState('')
  const normalized = query.trim().toLocaleLowerCase()
  const visibleMissions = normalized ? missions.filter(mission => `${mission.title}\n${mission.goal}\n${mission.allowed_origins.join('\n')}`.toLocaleLowerCase().includes(normalized)) : missions

  const columns: ColumnsType<Mission> = [
    {
      title: '研究项目',
      key: 'title_goal',
      render: (_val, record) => (
        <div className="ws-project-cell">
          <Button type="link" className="ws-project-title-link" onClick={() => onOpenMission(record.id)}>{record.title}</Button>
          <Typography.Paragraph className="ws-project-goal" ellipsis={{ rows: 1 }} style={{ marginBottom: 0 }}>{record.goal}</Typography.Paragraph>
        </div>
      ),
    },
    {
      title: '授权范围',
      dataIndex: 'allowed_origins',
      key: 'allowed_origins',
      width: 240,
      render: (origins: string[]) => (
        <div className="ws-origin-list">
          {origins.map((origin) => <Tag key={origin} className="ws-mono-tag">{origin}</Tag>)}
        </div>
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 180,
      render: (val: string) => <Typography.Text type="secondary" className="ws-project-date">{formatDateTime(val)}</Typography.Text>,
    },
  ]

  return (
    <div className="ws-page">
      <div className="ws-page-toolbar">
        <div className="ws-page-toolbar__main">
          <h1 className="ws-page-title">研究项目</h1>
          <Input className="ws-page-search" prefix={<SearchOutlined />} allowClear aria-label="搜索研究项目" placeholder="搜索项目、目标或授权范围" value={query} onChange={event => setQuery(event.target.value)} />
          <div className="ws-page-toolbar__actions">
            <Button ref={createButtonRef} type="primary" icon={<PlusOutlined />} onClick={onCreateNew}>{createLabel}</Button>
            <Button icon={<ReloadOutlined />} loading={refreshing} onClick={onRefresh}>刷新</Button>
          </div>
        </div>
      </div>

      {hasActiveSubmission && <Alert className="ws-page-alert" type="warning" showIcon title="已有提交键，先核对回执，不要重复创建。" action={<Button type="primary" onClick={onCreateNew}>前往核对提交</Button>} />}
      {!hasActiveSubmission && hasUnsavedDraft && <Alert className="ws-page-alert" type="info" showIcon title="内存中保留了尚未提交的探索草稿。" action={<Button onClick={onCreateNew}>继续编辑草稿</Button>} />}
      {error && <Alert className="ws-page-alert" type="error" showIcon title={missions.length > 0 ? `未能更新项目列表：${error}（下方仍保留上次已读取的内容）` : `读取项目列表失败：${error}`} action={<Button onClick={onRefresh}>重试读取</Button>} />}

      {initialLoading && missions.length === 0 ? (
        <div className="ws-table-frame"><div className="ws-table-frame-body"><Skeleton active paragraph={{ rows: 5 }} /></div></div>
      ) : visibleMissions.length === 0 ? (
        <div className="ws-table-frame"><div className="ws-table-frame-body"><Empty description={query.trim() ? '没有匹配的研究项目' : '暂无研究项目'} /></div></div>
      ) : (
        <div className="ws-table-frame">
          <Table<Mission>
            rowKey="id"
            columns={columns}
            dataSource={visibleMissions}
            pagination={false}
            tableLayout="fixed"
            onRow={(record) => ({ onClick: () => onOpenMission(record.id), style: { cursor: 'pointer' } })}
            className="ws-project-table"
          />
          <div className="ws-table-footer">
            <Typography.Text type="secondary">共 {visibleMissions.length} 个项目</Typography.Text>
          </div>
        </div>
      )}
    </div>
  )
}
