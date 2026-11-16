import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Card, Descriptions, Empty, Select, Skeleton, Space, Tag, Typography } from 'antd'
import { ArrowLeftOutlined, CheckCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import type { Mission, Snapshot, Thread } from '../types.ts'
import type { Action } from '../types.ts'
import { formatDateTime, needsUnknownResolution, statusBadgeColor, statusText } from '../types.ts'
import type { InspectingActionSelection } from '../workspace.ts'
import { resolveInspectingAction } from '../workspace.ts'
import type { RawObservationContext, RawObservationSelection } from '../observations.ts'
import { isCurrentRawObservationEvent, resolveRawObservation } from '../observations.ts'
import { ActionEvidenceDrawer } from './ActionEvidenceDrawer.tsx'
import { HTTPRecordsWorkspace } from './HTTPRecordsWorkspace.tsx'
import { FindingsWorkspace } from './FindingsWorkspace.tsx'
import { MissionResearchConfig } from './MissionResearchConfig.tsx'
import { ModelUsageWorkspace } from './ModelUsageWorkspace.tsx'
import { ResearchControlsPanel } from './ResearchControlsPanel.tsx'
import { ExecutionActivity } from './ExecutionActivity.tsx'
import { ObservationList as ObservationListForRecords } from './ObservationReader.tsx'
import type { ResearchControlSession } from '../researchControls.ts'

interface ProjectWorkspaceViewProps {
  mission: Mission | null
  threads: Thread[]
  selectedThreadId: string | null
  snapshot: Snapshot | null
  loadingMission: boolean
  loadingSnapshot: boolean
  error: string
  threadError: string
  acceptedBanner: string
  onDismissAcceptedBanner: () => void
  onBackToList: () => void
  onRefresh: () => void
  onSelectThread: (threadId: string) => void
  researchSession?: ResearchControlSession
  maxSteps?: number
  creationOpen?: boolean
  modelCallsRoute?: boolean
  modelCallID?: string | null
  onModelCallsNavigate?: (id: string | null) => void
  onResearchNavigate?: () => void
}

export function ObservationRecords({ mission, selectedThreadId, snapshot, onInspect, rawSelection = null, onToggleRaw }: {
  mission: Mission | null
  selectedThreadId: string | null
  snapshot: Snapshot
  onInspect: (selection: InspectingActionSelection, sourceSnapshot: Snapshot) => void
  rawSelection?: RawObservationSelection | null
  onToggleRaw?: (selection: RawObservationSelection) => void
}) {
  if (!mission || selectedThreadId !== snapshot.thread.id || snapshot.thread.mission_id !== snapshot.mission.id || snapshot.mission.id !== mission.id) {
    return <div className="ws-record-card"><Typography.Text type="secondary">关联回执不可用</Typography.Text></div>
  }
  return ObservationListForRecords({ mission, snapshot, observations: snapshot.observations, rawSelection, onToggleRaw: onToggleRaw ?? (() => {}), onInspect })
}

const OBJECT_NAV = [
  { key: 'research', label: '执行记录' },
  { key: 'http', label: 'HTTP 流量' },
  { key: 'findings', label: '候选发现' },
  { key: 'calls', label: '模型调用' },
  { key: 'info', label: '任务信息' },
] as const

export const ProjectWorkspaceView: React.FC<ProjectWorkspaceViewProps> = ({
  mission, threads, selectedThreadId, snapshot, loadingMission, loadingSnapshot, error, threadError,
  acceptedBanner, onDismissAcceptedBanner, onBackToList, onRefresh, onSelectThread,
  researchSession, maxSteps, creationOpen = false, modelCallsRoute = false, modelCallID = null,
  onModelCallsNavigate, onResearchNavigate,
}) => {
  const [inspectingSelection, setInspectingSelection] = useState<InspectingActionSelection | null>(null)
  const [localWorkspaceTab, setWorkspaceTab] = useState('research')
  const workspaceTab = modelCallsRoute ? 'calls' : localWorkspaceTab
  useEffect(() => { if (!modelCallsRoute && localWorkspaceTab === 'calls') setWorkspaceTab('research') }, [modelCallsRoute, localWorkspaceTab])
  const [rawSelection, setRawSelection] = useState<RawObservationSelection | null>(null)
  const rawContext = useMemo<RawObservationContext>(() => ({
    mission, selectedThreadId, snapshot,
    available: workspaceTab === 'research' && !creationOpen && !inspectingSelection,
  }), [mission, selectedThreadId, snapshot, workspaceTab, creationOpen, inspectingSelection])
  const currentRawContext = useRef<RawObservationContext | null>(null)
  useLayoutEffect(() => {
    currentRawContext.current = rawContext
    return () => { currentRawContext.current = null }
  }, [rawContext])
  const visibleRawSelection = rawContext.available && resolveRawObservation(rawSelection, mission, selectedThreadId, snapshot) ? rawSelection : null
  useLayoutEffect(() => { if (rawSelection && !visibleRawSelection) setRawSelection(null) }, [rawSelection, visibleRawSelection])
  const toggleRaw = (selection: RawObservationSelection) => {
    if (!isCurrentRawObservationEvent(selection, rawContext, currentRawContext.current) || !rawContext.mission) return
    setRawSelection(visibleRawSelection?.observationId === selection.observationId ? null : selection)
  }
  useEffect(() => { setInspectingSelection(null) }, [mission?.id, selectedThreadId, creationOpen, workspaceTab])
  const inspectingAction = useMemo(() => creationOpen || workspaceTab !== 'research' || !matchedSnapshot(snapshot, mission, selectedThreadId) ? null : resolveInspectingAction(inspectingSelection, mission, selectedThreadId, snapshot),
    [inspectingSelection, mission, selectedThreadId, snapshot, creationOpen, workspaceTab])
  useEffect(() => { if (inspectingSelection && !inspectingAction) setInspectingSelection(current => current === inspectingSelection ? null : current) }, [inspectingSelection, inspectingAction])

  const currentEvidenceContext = useRef<{ mission: Mission | null; selectedThreadId: string | null; snapshot: Snapshot | null; creationOpen: boolean } | null>(null)
  useLayoutEffect(() => {
    currentEvidenceContext.current = { mission, selectedThreadId, snapshot, creationOpen: creationOpen || workspaceTab !== 'research' }
    return () => { currentEvidenceContext.current = null }
  }, [mission, selectedThreadId, snapshot, creationOpen, workspaceTab])
  const inspectAction = (selection: InspectingActionSelection, sourceSnapshot: Snapshot) => {
    const current = currentEvidenceContext.current
    if (!current || current.creationOpen || sourceSnapshot !== current.snapshot || !matchedSnapshot(current.snapshot, current.mission, current.selectedThreadId)) return
    if (resolveInspectingAction(selection, current.mission, current.selectedThreadId, current.snapshot)) {
      setRawSelection(null)
      setInspectingSelection(selection)
    }
  }

  const activeThread = snapshot?.thread ?? threads.find(thread => thread.id === selectedThreadId) ?? null
  const hasUnknownAction = Boolean(snapshot?.actions.some(action => needsUnknownResolution(action)))
  const pendingActionsCount = snapshot?.actions.filter(action => action.status === 'pending').length ?? 0
  const latestAttempt = snapshot?.attempts[snapshot.attempts.length - 1]
  const latestFailedAttempt = latestAttempt?.status === 'failed' && latestAttempt.error ? latestAttempt : null
  const validSnapshot = matchedSnapshot(snapshot, mission, selectedThreadId) ? snapshot : null
  const navThreads = mission ? threads.filter(thread => thread.mission_id === mission.id) : []

  const viewSwitch = (key: string) => {
    setWorkspaceTab(key)
    if (key === 'calls') onModelCallsNavigate?.(null)
    else if (modelCallsRoute) onResearchNavigate?.()
  }

  const riskCount = pendingActionsCount + (hasUnknownAction ? 1 : 0) + (validSnapshot?.thread.status === 'blocked' && latestFailedAttempt ? 1 : 0)

  return <div className="ws-task-workspace">
    <div className="ws-task-head">
      <Button type="text" icon={<ArrowLeftOutlined />} aria-label="返回项目列表" onClick={onBackToList} />
      <div className="ws-task-head-name" title={mission?.title}>{mission?.title ?? '项目研究'}</div>
      <Button className="ws-task-head-refresh" icon={<ReloadOutlined />} loading={loadingMission || loadingSnapshot} onClick={onRefresh}>刷新</Button>
    </div>

    {acceptedBanner && <Alert type="success" showIcon icon={<CheckCircleOutlined />} closable onClose={onDismissAcceptedBanner} title={acceptedBanner} />}
    {error && <Alert type="error" showIcon title={mission ? `未能刷新最新研究状态：${error}（下方仍保留上次已读取的内容）` : `读取项目详情失败：${error}`} action={<Button onClick={onRefresh}>重试读取</Button>} />}
    {loadingMission && !mission ? <Skeleton active paragraph={{ rows: 6 }} /> : !mission ? <Card><Empty description="未找到该项目或暂时无法读取"><Button onClick={onBackToList}>返回项目列表</Button></Empty></Card> : (
      <div className="ws-task-body">
        <aside className="ws-task-nav" aria-label="任务内导航">
          <nav className="ws-object-nav" aria-label="任务对象">
            {OBJECT_NAV.map(item => <button key={item.key} type="button" className="ws-object-nav-item" aria-current={workspaceTab === item.key ? 'page' : undefined} onClick={() => viewSwitch(item.key)}>{item.label}</button>)}
          </nav>
        </aside>

        <div className="ws-task-detail">
          <div hidden={workspaceTab !== 'research'} className="ws-research-view">
            {threadError && <Alert type="error" showIcon title={threadError} />}
            {loadingSnapshot && !validSnapshot ? <Skeleton active paragraph={{ rows: 8 }} /> : !validSnapshot ? <Empty description={threadError ? '未能加载所选研究方向，请从上方选择有效方向' : '请选择研究方向'} /> : <>
              <div className="ws-direction-toolbar">
                <div className="ws-direction-identity">
                  <Select aria-label="选择研究方向" className="ws-direction-select" value={selectedThreadId || undefined} options={navThreads.map(thread => ({ value: thread.id, label: thread.title }))} onChange={onSelectThread} />
                  <Tag color={statusBadgeColor(validSnapshot.thread.status)}>{statusText(validSnapshot.thread.status)}</Tag>
                  <span className="ws-direction-meta">{validSnapshot.thread.step_count} 步 · 更新于 {formatDateTime(validSnapshot.thread.updated_at)}</span>
                </div>
                {!creationOpen && researchSession && <ResearchControlsPanel compact key={mission.id + validSnapshot.thread.id} snapshot={validSnapshot} session={researchSession} maxSteps={maxSteps} onRefresh={onRefresh} />}
              </div>

              {riskCount > 0 && <div className="ws-risk-strip" role="status">
                {pendingActionsCount > 0 && <Tag color="warning">{pendingActionsCount} 项待审批</Tag>}
                {hasUnknownAction && <Tag color="warning">存在效果未知动作</Tag>}
                {validSnapshot.thread.status === 'blocked' && latestFailedAttempt && <Tag color="error">最近尝试失败</Tag>}
                {latestFailedAttempt?.error && <span className="ws-risk-error" title={latestFailedAttempt.error}>{latestFailedAttempt.error}</span>}
                <span className="ws-risk-hint">在下方执行记录中查看与处理</span>
              </div>}

              <details className="ws-direction-rationale"><summary>当前问题与下一步</summary><div className="ws-direction-brief"><section><h3>当前问题</h3><p className="ws-pre-wrap">{validSnapshot.thread.question}</p></section><section><h3>下一步</h3><p className="ws-pre-wrap">{validSnapshot.thread.next_step || '尚无下一步计划记录'}</p></section>{validSnapshot.thread.rationale && <section><h3>研究依据</h3><p className="ws-pre-wrap">{validSnapshot.thread.rationale}</p></section>}</div></details>

              <ExecutionActivity key={mission.id + validSnapshot.thread.id} mission={mission} snapshot={validSnapshot} rawSelection={visibleRawSelection} onToggleRaw={toggleRaw} onInspect={inspectAction} />
            </>}
          </div>

          <div hidden={workspaceTab !== 'http'}><HTTPRecordsWorkspace key={mission.id} mission={mission} threads={threads} active={!creationOpen && workspaceTab === 'http'} /></div>
          <FindingsWorkspace key={mission.id} mission={mission} threads={threads} active={!creationOpen && workspaceTab === 'findings'} />
          <div hidden={workspaceTab !== 'calls'}><ModelUsageWorkspace key={mission.id} missionID={mission.id} active={!creationOpen && workspaceTab === 'calls'} selectedCallID={modelCallID} onSelectCall={onModelCallsNavigate} /></div>

          <div hidden={workspaceTab !== 'info'} className="ws-task-info">
            <Card title="项目范围与目标" extra={!creationOpen && workspaceTab === 'info' && <MissionResearchConfig key={mission.id} missionID={mission.id} />}>
              <Descriptions column={{ xs: 1, sm: 1, md: 2, lg: 2, xl: 2 }} size="middle">
                <Descriptions.Item label="希望达到的目标" span={2}><div className="ws-pre-wrap">{mission.goal}</div></Descriptions.Item>
                <Descriptions.Item label="明确授权范围" span={2}><Space size={[8, 8]} wrap>{mission.allowed_origins.map(origin => <Tag key={origin} color="processing" className="ws-mono-tag">{origin}</Tag>)}</Space></Descriptions.Item>
                {mission.origin && <Descriptions.Item label="目标／已知信息" span={2}><div className="ws-pre-wrap ws-scrollable-box">{mission.origin}</div></Descriptions.Item>}
                <Descriptions.Item label="项目 ID"><code>{mission.id}</code></Descriptions.Item>
                <Descriptions.Item label="创建时间">{formatDateTime(mission.created_at)}</Descriptions.Item>
              </Descriptions>
            </Card>
          </div>
        </div>
      </div>
    )}

    {!creationOpen && <ActionEvidenceDrawer action={inspectingAction} mission={inspectingAction ? snapshot?.mission ?? undefined : undefined} thread={inspectingAction ? snapshot?.thread ?? activeThread ?? undefined : undefined} onClose={() => setInspectingSelection(null)} />}
  </div>
}

function matchedSnapshot(snapshot: Snapshot | null, mission: Mission | null, selectedThreadId: string | null): snapshot is Snapshot {
  return Boolean(snapshot && mission && snapshot.mission.id === mission.id && snapshot.thread.id === selectedThreadId && snapshot.thread.mission_id === mission.id)
}

export type { Action }
