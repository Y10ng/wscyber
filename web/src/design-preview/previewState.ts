import { createInitialDemoDataset, type InitialDemoDataset } from './demoData.ts'
import { createDemoConfiguration, type TaskConfiguration } from './TaskConfiguration.tsx'
import { createDemoTurn, INITIAL_PROGRESS, parseDemoOrigins, type DemoProgress } from './demoProgress.ts'
import type { DemoProject, DemoThread } from './types.ts'

export interface TaskDraft {
  name: string
  target: string
  objective: string
  origins: string
  authorized: boolean
  configuration: TaskConfiguration
}
export function createTaskDraft(): TaskDraft {
  return { name: '', target: '', objective: '', origins: '', authorized: false, configuration: createDemoConfiguration() }
}

export interface PreviewState {
  data: InitialDemoDataset
  configurations: Record<string, TaskConfiguration>
  progress: Record<string, DemoProgress>
}
export function createPreviewState(): PreviewState {
  const data = createInitialDemoDataset()
  return { data, configurations: Object.fromEntries(data.projects.map((project) => [project.id, createDemoConfiguration(project.id === 'PRJ-2026-01')])), progress: {} }
}
export function advancePreview(state: PreviewState, projectId: string, threadId: string): PreviewState {
  const project = state.data.projects.find((item) => item.id === projectId)
  const thread = state.data.threads.find((item) => item.id === threadId && item.projectId === projectId)
  if (!project || !thread || project.executionStatus !== '运行中' || thread.executionStatus !== '运行中') return state
  const previousProgress = state.progress[projectId] ?? INITIAL_PROGRESS
  const previousFinding = state.data.findings.find((finding) => finding.id === previousProgress.findingId)
  if (previousFinding && previousFinding.threadId !== threadId) return state
  const round = previousProgress.round + 1, stage = Math.min(previousProgress.stage + 1, 3)
  const turn = createDemoTurn(project, thread, state.configurations[projectId]!, round, stage)
  const steps = [...(previousFinding?.reproductionSteps ?? []), { ...turn.finding.reproductionSteps[0]!, stepNumber: (previousFinding?.reproductionSteps.length ?? 0) + 1 }]
  const finding = { ...turn.finding, reproductionSteps: steps, verificationRecords: turn.finding.verificationRecords.map((record) => ({ ...record, evidenceRequestIds: steps.map((step) => step.linkedRequestId) })) }
  // Earlier input/output strings, traffic, and revision events remain immutable.
  return { ...state, progress: { ...state.progress, [projectId]: { round, stage, findingId: finding.id } }, data: {
    ...state.data,
    traffic: [...state.data.traffic, turn.request], modelCalls: [...state.data.modelCalls, turn.call],
    findings: previousProgress.stage < 3 ? [finding, ...state.data.findings.filter((item) => item.id !== finding.id)] : state.data.findings,
    timelineEvents: previousProgress.stage < 3 ? [...state.data.timelineEvents, turn.event] : state.data.timelineEvents,
    threads: state.data.threads.map((item) => item.id === threadId ? { ...item, updatedAt: `示例第 ${round} 轮` } : item),
  } }
}
export function createPreviewProject(state: PreviewState, draft: TaskDraft): { state: PreviewState; projectId: string; threadId: string } | { error: string } {
  if (!draft.target.trim() || !draft.objective.trim()) return { error: '请填写目标／已知信息和希望达到的目标。' }
  const origins = parseDemoOrigins(draft.origins)
  if (!origins) return { error: '请输入明确的 HTTP(S) origin，仅接受 example.com / .org / .net 或 .test 保留测试域；不含路径、凭据或参数。' }
  if (!draft.authorized) return { error: '请确认上述明确范围的授权；背景信息和资料不能代替授权。' }
  const projectId = `PRJ-LOCAL-${state.data.projects.length + 1}`, threadId = `THR-LOCAL-${state.data.projects.length + 1}`
  const project: DemoProject = {
    id: projectId, name: draft.name.trim() || draft.target.trim().slice(0, 28),
    targetAndKnownInfo: draft.target.trim(), desiredObjective: draft.objective.trim(), allowedOrigins: origins,
    submissionStatus: '已接收', executionStatus: '运行中', currentFocusSummary: '已建立初始研究方向。',
    createdAt: '本地新建', identities: [], stepLimitPerThread: 20, concurrencyLimit: 1,
  }
  const thread: DemoThread = {
    id: threadId, projectId, title: '初始目标与权限边界研究', executionStatus: '运行中',
    questionAndBasis: `已知信息：${project.targetAndKnownInfo}\n研究目标：${project.desiredObjective}`,
    nextStepPlan: '在明确范围内整理基线，记录候选与对照证据。', blockingReason: null, blockingRequestId: null,
    executedSteps: 0, maxSteps: 20, pendingApprovedActionsOnResume: [], updatedAt: '本地新建',
  }
  const config: TaskConfiguration = { ...draft.configuration, skills: draft.configuration.skills.map((ref) => ({ ...ref, effectiveFromRound: 1 })) }
  const turn = createDemoTurn(project, thread, config, 1, 0)
  return { projectId, threadId, state: { ...state,
    configurations: { ...state.configurations, [projectId]: config }, progress: { ...state.progress, [projectId]: { ...INITIAL_PROGRESS } },
    data: { ...state.data, projects: [project, ...state.data.projects], threads: [thread, ...state.data.threads],
      traffic: [...state.data.traffic, turn.request], modelCalls: [...state.data.modelCalls, turn.call] },
  } }
}

export function supplementPreview(state: PreviewState, projectId: string, ids: string[]): PreviewState {
  const config = state.configurations[projectId]
  if (!config || state.data.projects.find((project) => project.id === projectId)?.executionStatus !== '运行中') return state
  const nextRound = (state.progress[projectId] ?? INITIAL_PROGRESS).round + 1
  return { ...state, configurations: { ...state.configurations, [projectId]: { ...config, skills: [
    ...config.skills, ...[...new Set(ids)].filter((id) => !config.skills.some((ref) => ref.versionId === id)).map((versionId) => ({ versionId, effectiveFromRound: nextRound })),
  ] } } }
}
