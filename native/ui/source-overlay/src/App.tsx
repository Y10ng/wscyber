import { useState, useEffect, useLayoutEffect, useCallback, useMemo, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, ArrowLeft } from 'lucide-react'
import AppSidebar from './components/AppSidebar'
import OverviewPage from './components/OverviewPage'
import TasksPage from './components/TasksPage'
import VulnsPage from './components/VulnsPage'
import ReportsPage from './components/ReportsPage'
import { Page, PageHeader } from './components/PageFrame'
import ConfigPanel from './components/ConfigPanel'
import AgentPanel from './components/AgentPanel'
import ChatPanel from './components/ChatPanel'
import WebApiTaskDialog from './components/WebApiTaskDialog'
import WebApiEvidencePanel from './components/WebApiEvidencePanel'
import WebApiBoardPanel from './components/WebApiBoardPanel'
import WebApiFindingsPanel from './components/WebApiFindingsPanel'
import WebApiTaskBar, { type WebApiView } from './components/WebApiTaskBar'
import WebApiExecutionTimeline, { ExecutionViewHeader, type ExecutionViewMode } from './components/WebApiExecutionTimeline'
import { buildExecutionTimeline } from './lib/execution-timeline'
import type { ExecutionApproval } from './lib/execution-timeline'
import { WebApiVerificationBar } from './components/WebApiVerification'
import { WebApiRetestBar } from './components/WebApiRetest'
import WebApiReportsPanel from './components/WebApiReportsPanel'
import WebApiReportBar from './components/WebApiReportBar'
import WebApiAssetsPanel from './components/WebApiAssetsPanel'
import WebApiUsagePanel from './components/WebApiUsagePanel'
import WebApiUsageDialog from './components/WebApiUsageDialog'
import IntegrationsPage from './components/IntegrationsPage'
import { assetMentionables } from './components/AssetPanel'
import MentionPicker from './components/MentionPicker'
import { useObservations } from '@/viewer'
import { Button, TooltipProvider } from '@cyber/ui'
import { ThemeProvider } from '@cyber/theme'
import { activateLLMProfile, APIError, getConfigStatus, getIOAOverview, getStatus, getWebApiReportBySession, getWebApiRetestBySession, getWebApiTask, getWebApiTaskBySession, getWebApiSessionKinds, getWebApiVerificationBySession } from './api'
import type { IOAMessage, IOANode, LLMProviderView, ServerStatus, WebApiReportDetail, WebApiRetestDetail, WebApiTask, WebApiVerificationDetail } from './api'
import type { SCONode } from '@cyber/cstx-easm'
import type { MentionPopupApi } from './viewer'
import { useChatSession } from './hooks/useChatSession'
import { useGuardrailReviews } from './hooks/useGuardrailReviews'
import type { PendingItem } from './components/WebApiPendingApprovals'
import { cachedWebApiBadgeLabel } from './components/SessionList'
import { usePolling } from './hooks/usePolling'
import { isLocalModelAgent, isSessionAgentOnline } from './lib/session-agent'
import type { IOAConsoleTarget } from './lib/ioa-navigation'
import { listSCONodes, subscribeCSTXChanges, syncCSTXArtifacts } from './lib/cstx-runtime'

import { useObservable, WorkbenchSlots } from './runtime/react'
import type { HubServices } from './runtime/services'
import type { CapabilityManifest } from './runtime/capabilities'

export type AppPage = 'overview' | 'tasks' | 'assets' | 'vulns' | 'reports' | 'models' | 'nodes' | 'integrations'

// 已实现页面：总览/任务/资产/漏洞/报告/集成/模型/节点；URL 里出现未实现的占位入口时归一为总览。
const IMPLEMENTED_PAGES: AppPage[] = ['overview', 'tasks', 'assets', 'vulns', 'reports', 'models', 'nodes', 'integrations']
const sidebarCollapsedStorageKey = 'wscyber.sidebar.collapsed'

const EMPTY_SEED = { text: '', nonce: 0 }

// Respect a previously-chosen theme on boot. ThemeProvider's own initializer is
// short-circuited by the `initial` prop (it returns `initial` before ever reading
// storage), so we read the persisted value here and feed it in as the initial —
// otherwise every reload snaps back to the light default.
function getInitialTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'light'
  const v = window.localStorage.getItem('cyber-theme')
  return v === 'dark' || v === 'light' ? v : 'light'
}

// 移动端侧栏是抽屉，默认关闭；桌面始终显示侧栏（宽度由折叠状态决定）。
function getInitialSidebarOpen() {
  if (typeof window === 'undefined') return true
  return !window.matchMedia('(max-width: 767px)').matches
}

function getInitialPage(): AppPage {
  if (typeof window === 'undefined') return 'overview'
  const value = new URLSearchParams(window.location.search).get('page')
  return IMPLEMENTED_PAGES.includes(value as AppPage) ? value as AppPage : 'overview'
}

function getInitialSidebarCollapsed(): boolean {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(sidebarCollapsedStorageKey) === 'true'
}

export default function App({ services }: { services: HubServices }) {
  const { t } = useTranslation('app')
  const { t: tc } = useTranslation('chat')
  const chat = useChatSession()
  const observations = useObservations(chat.aopEvents)
  const guardrailSessions = useMemo(() => {
    const online = new Set(chat.agents.map(agent => agent.hello?.nodeId))
    const ids = chat.sessions.filter(record => online.has(record.session?.nodeId) && record.session?.state !== 'closed')
      .map(record => record.session?.id || '').filter(Boolean)
    if (chat.activeSessionID && !ids.includes(chat.activeSessionID)) ids.push(chat.activeSessionID)
    return ids
  }, [chat.agents, chat.sessions, chat.activeSessionID])
  const guardrails = useGuardrailReviews(guardrailSessions, chat.activeSessionID, chat.aopEvents)
  // 顶栏「待审批 N」：全部会话的 live pending 聚合。会话名取会话标题；demo 等
  // 无标题会话回退到侧栏已解析的业务徽标（任务目标/验证/报告/复测），再回退短 ID。
  const pendingApprovalItems = useMemo<PendingItem[]>(() => {
    const items: PendingItem[] = []
    const nameOf = (sessionId: string) => {
      const record = chat.sessions.find((item) => item.session?.id === sessionId)
      return record?.session?.title?.trim()
        || cachedWebApiBadgeLabel(sessionId, tc)
        || sessionId.slice(0, 8)
    }
    for (const [sessionId, reviews] of Object.entries(guardrails.bySession)) {
      for (const review of reviews) {
        items.push({ sessionId, review, sessionName: nameOf(sessionId), unavailable: guardrails.unavailable[sessionId] === true })
      }
    }
    return items
  }, [guardrails.bySession, guardrails.unavailable, chat.sessions, tc])
  const resolvePendingApproval = useCallback(async (item: PendingItem, approve: boolean) => {
    await guardrails.resolve(item.sessionId, item.review, approve)
  }, [guardrails])
  const [serverStatus, setServerStatus] = useState<ServerStatus | null>(null)
  const [llmProfiles, setLLMProfiles] = useState<LLMProviderView[]>([])
  const [activeLLMProfile, setActiveLLMProfile] = useState('')
  const [switchingLLM, setSwitchingLLM] = useState(false)
  const panelSelection = useObservable(services.panels)
  const activeToolPanel = panelSelection?.id ?? null
  const [sidebarOpen, setSidebarOpen] = useState(getInitialSidebarOpen)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(getInitialSidebarCollapsed)
  // 进入任务工作区前的展开状态：离开工作区时恢复（手动折叠的保持折叠）。
  const collapsedBeforeWorkspaceRef = useRef(false)
  const [page, setPage] = useState<AppPage>(getInitialPage)
  const [taskDialogOpen, setTaskDialogOpen] = useState(false)
  const [usageDialogOpen, setUsageDialogOpen] = useState(false)
  const [focusAssetId, setFocusAssetId] = useState<string | null>(null)
  const [focusVulnId, setFocusVulnId] = useState<string | null>(null)
  // 总览“逾期 N” → 漏洞页：一次性请求，漏洞页消费后清空。
  const [vulnsOverdueRequested, setVulnsOverdueRequested] = useState(false)
  const [activeWebApiTask, setActiveWebApiTask] = useState<WebApiTask | null>(null)
  const [workspaceView, setWorkspaceView] = useState<WebApiView>('execution')
  // 任务导航计数失效信号：新建验证/报告成功时 bump，TaskNav 感知后重取计数。
  const [webApiCountsRevision, setWebApiCountsRevision] = useState(0)
  const bumpWebApiCountsRevision = useCallback(() => setWebApiCountsRevision((n) => n + 1), [])
  const [webApiTaskError, setWebApiTaskError] = useState('')
  const [activeWebApiVerification, setActiveWebApiVerification] = useState<WebApiVerificationDetail | null>(null)
  const [activeWebApiReport, setActiveWebApiReport] = useState<WebApiReportDetail | null>(null)
  const [activeWebApiRetest, setActiveWebApiRetest] = useState<WebApiRetestDetail | null>(null)
  const [focusFindingId, setFocusFindingId] = useState<string | null>(null)
  const [focusReportId, setFocusReportId] = useState<string | null>(null)
  const [focusEvidenceId, setFocusEvidenceId] = useState<string | null>(null)
  const pendingFindingIdRef = useRef<{ sessionID: string; findingID: string } | null>(null)
  const pendingReportIdRef = useRef<{ sessionID: string; reportID: string } | null>(null)
  // 执行视图内部：时间线（默认）| 控制流。不在 WebApiView 枚举里 —— 切到流量/候选
  // 再回来仍落回时间线，与任务导航语义一致。
  const [executionMode, setExecutionMode] = useState<ExecutionViewMode>('timeline')
  const activeSessionIDRef = useRef(chat.activeSessionID)
  activeSessionIDRef.current = chat.activeSessionID
  const extensionRevision = useObservable(services.slots.revision)
  const capabilityState = useObservable(services.capabilities)
  const capabilityManifest = capabilityState.manifest
  useLayoutEffect(() => {
    services.capabilities.setNodes(Object.fromEntries(chat.agents.map(agent => [agent.hello?.nodeId || '',
      agent.hello?.capabilities || []])))
  }, [services, chat.agents])
  const capabilityIDs = capabilityState.advertised
  const effectiveManifest = useMemo<CapabilityManifest | null>(() => capabilityManifest ? {
    ...capabilityManifest, capabilities: capabilityIDs.map(id => capabilityManifest.capabilities.find(item => item.id === id) || { id }),
  } : null, [capabilityManifest, capabilityIDs])
  const scanEnabled = capabilityIDs.includes('scan')
  const webapiEnabled = capabilityIDs.includes('webapi')
  // 选中/刷新已有原生会话后按 session 查询恢复业务 Task；非 Web/API 会话不显示
  // 假流量，也不根据聊天文本/标题猜 TaskID。
  useEffect(() => {
    // 切 session 立刻清旧 Task/验证/报告/复测/视图，渲染再由 initial_session_id / session_id 兜底。
    setActiveWebApiTask(null)
    setActiveWebApiVerification(null)
    setActiveWebApiReport(null)
    setActiveWebApiRetest(null)
    setFocusFindingId(null)
    setFocusReportId(null)
    setFocusEvidenceId(null)
    setWorkspaceView('execution')
    setWebApiTaskError('')
    if (!webapiEnabled) return
    const id = chat.activeSessionID
    if (!id) return
    if (pendingFindingIdRef.current && pendingFindingIdRef.current.sessionID !== id) pendingFindingIdRef.current = null
    if (pendingReportIdRef.current && pendingReportIdRef.current.sessionID !== id) pendingReportIdRef.current = null
    let cancelled = false
    // 报告 session：taskForSession 与验证/复测反查都不解析，只能经 reports?session_id= 反查；
    // 只有真实 404 算无报告，网络/服务端错误可见。
    const resolveReport = async () => {
      try {
        const report = await getWebApiReportBySession(id)
        if (cancelled) return
        if (report.session_id === id) setActiveWebApiReport(report)
      } catch (rerr: unknown) {
        if (cancelled) return
        if (!(rerr instanceof APIError) || rerr.status !== 404) {
          setWebApiTaskError(rerr instanceof Error ? rerr.message : String(rerr))
        }
      }
    }
    // 复测 session：taskForSession 回父任务 200，验证反查 404 后先试复测再落报告。
    const resolveRetest = async () => {
      try {
        const retest = await getWebApiRetestBySession(id)
        if (cancelled) return
        if (retest.session_id === id) {
          setActiveWebApiRetest(retest)
          setWorkspaceView('execution')
        }
      } catch (rterr: unknown) {
        if (cancelled) return
        setActiveWebApiRetest(null)
        setWorkspaceView('execution')
        // 只有真实 404 算无复测；网络/服务端错误可见，不静默吞掉。无复测时再试报告。
        if (!(rterr instanceof APIError) || rterr.status !== 404) {
          setWebApiTaskError(rterr instanceof Error ? rterr.message : String(rterr))
          return
        }
        await resolveReport()
      }
    }
    const resolveVerification = async () => {
      try {
        const verification = await getWebApiVerificationBySession(id)
        if (cancelled) return
        if (verification.session_id === id) {
          setActiveWebApiVerification(verification)
          setWorkspaceView('execution')
        }
      } catch (verr: unknown) {
        if (cancelled) return
        setActiveWebApiVerification(null)
        setWorkspaceView('execution')
        // 只有真实 404 算无验证；网络/服务端错误可见，不静默吞掉。无验证时再试复测/报告。
        if (!(verr instanceof APIError) || verr.status !== 404) {
          setWebApiTaskError(verr instanceof Error ? verr.message : String(verr))
          return
        }
        await resolveRetest()
      }
    }
    getWebApiTaskBySession(id)
      .then((task) => {
        if (cancelled) return
        setWorkspaceView('execution')
        if (task.initial_session_id === id) {
          setActiveWebApiTask(task)
          if (pendingFindingIdRef.current?.sessionID === id) {
            setFocusFindingId(pendingFindingIdRef.current.findingID)
            setWorkspaceView('findings')
            pendingFindingIdRef.current = null
          } else if (pendingReportIdRef.current?.sessionID === id) {
            setFocusReportId(pendingReportIdRef.current.reportID)
            setWorkspaceView('reports')
            pendingReportIdRef.current = null
          }
        } else {
          // 验证/复测 session 被后端 taskForSession 回父 Task 200：只读查回该记录显示 Bar，不再等 404。
          void resolveVerification()
        }
      })
      .catch(async (err: unknown) => {
        if (cancelled) return
        // 只有真实 404 才当普通会话/验证/复测/报告会话；其它错误可见，不全 catch 静默隐藏。
        if (err instanceof APIError && err.status !== 404) {
          setWebApiTaskError(err instanceof Error ? err.message : String(err))
          return
        }
        if (err instanceof APIError && err.status === 404) {
          // 非任务会话：按验证 session 只读查回固定来源/结果，404 再试复测、再试报告反查。
          await resolveVerification()
          return
        }
        // 网络/超时等非 APIError 不吞掉，保持可见。
        setWebApiTaskError(err instanceof Error ? err.message : String(err))
      })
    return () => { cancelled = true }
  }, [webapiEnabled, chat.activeSessionID])

  // 验证 Bar 首次 result 可能为 null：沿原生执行结束变化做只读轮询，直到出现真实判读。
  const refreshActiveVerification = useCallback(async () => {
    const verification = activeWebApiVerification
    if (!verification || verification.session_id !== chat.activeSessionID || verification.result) return
    try {
      const fresh = await getWebApiVerificationBySession(verification.session_id)
      if (fresh.session_id !== chat.activeSessionID) return
      setActiveWebApiVerification((current) =>
        current && current.verification_id === fresh.verification_id ? fresh : current)
    } catch { /* 轮询失败不覆盖已有；下一次再试 */ }
  }, [activeWebApiVerification, chat.activeSessionID])
  usePolling(refreshActiveVerification, 5000,
    webapiEnabled && !!activeWebApiVerification && activeWebApiVerification.session_id === chat.activeSessionID && !activeWebApiVerification.result)

  // 报告 Bar 首次可能尚无草稿：只读轮询详情，直到报告会话写入草稿（has_draft）。
  const refreshActiveReport = useCallback(async () => {
    const report = activeWebApiReport
    if (!report || report.session_id !== chat.activeSessionID || report.has_draft) return
    try {
      const fresh = await getWebApiReportBySession(report.session_id)
      if (fresh.session_id !== chat.activeSessionID) return
      setActiveWebApiReport((current) => current && current.report_id === fresh.report_id ? fresh : current)
    } catch { /* 轮询失败不覆盖已有；下一次再试 */ }
  }, [activeWebApiReport, chat.activeSessionID])
  usePolling(refreshActiveReport, 8000,
    webapiEnabled && !!activeWebApiReport && activeWebApiReport.session_id === chat.activeSessionID && !activeWebApiReport.has_draft)

  // 复测 Bar 首次 result 可能为 null：沿原生执行结束变化做只读轮询，直到出现真实结论。
  const refreshActiveRetest = useCallback(async () => {
    const retest = activeWebApiRetest
    if (!retest || retest.session_id !== chat.activeSessionID || retest.result) return
    try {
      const fresh = await getWebApiRetestBySession(retest.session_id)
      if (fresh.session_id !== chat.activeSessionID) return
      setActiveWebApiRetest((current) =>
        current && current.retest_id === fresh.retest_id ? fresh : current)
    } catch { /* 轮询失败不覆盖已有；下一次再试 */ }
  }, [activeWebApiRetest, chat.activeSessionID])
  usePolling(refreshActiveRetest, 5000,
    webapiEnabled && !!activeWebApiRetest && activeWebApiRetest.session_id === chat.activeSessionID && !activeWebApiRetest.result)
  const openSettings = useCallback((section: 'llm' | 'jev' = 'llm') => services.panels.open('settings', { section }), [services])
  const openIOAConsole = useCallback((target?: IOAConsoleTarget) => services.panels.open('ioa', { target }), [services])

  const refreshStatus = useCallback(async () => {
    const [statusResult, configResult] = await Promise.allSettled([getStatus(), getConfigStatus()])
    if (statusResult.status === 'fulfilled') setServerStatus(statusResult.value)
    if (configResult.status === 'fulfilled') {
      const profiles = configResult.value.llm?.providers ?? []
      setLLMProfiles(profiles)
      setActiveLLMProfile(configResult.value.llm?.activeProfile || profiles[0]?.id || '')
    }
  }, [])

  useEffect(() => {
    refreshStatus()
  }, [refreshStatus])

  // Keep the header (model + agent count + health base) fresh without a reload.
  usePolling(refreshStatus, 30000)

  // 页面导航：离开当前会话、写 URL（?page=）、切页并关闭移动端抽屉。
  // leaveSession 不改 URL，统一由这里 pushState；工作区沿用 /sessions/<id>。
  const navigate = useCallback((next: AppPage) => {
    chat.leaveSession()
    window.history.pushState({}, '', '/?page=' + next)
    setPage(next)
    if (window.matchMedia('(max-width: 767px)').matches) setSidebarOpen(false)
  }, [chat])

  // 浏览器后退/前进：根路径按 ?page 恢复页面（会话路由由 useChatSession 的监听处理）。
  useEffect(() => {
    const applyPage = () => {
      if (window.location.pathname !== '/') return
      const value = new URLSearchParams(window.location.search).get('page')
      setPage(IMPLEMENTED_PAGES.includes(value as AppPage) ? value as AppPage : 'overview')
    }
    window.addEventListener('popstate', applyPage)
    return () => window.removeEventListener('popstate', applyPage)
  }, [])

  // 用户手动折叠/展开：持久化到 localStorage；自动折叠不进持久化值。
  const toggleSidebarCollapsed = useCallback(() => {
    setSidebarCollapsed((prev) => {
      const next = !prev
      window.localStorage.setItem(sidebarCollapsedStorageKey, String(next))
      return next
    })
  }, [])

  // 进入任务工作区自动折叠，离开时恢复进入前状态。
  const workspaceActive = !!chat.activeSessionID
  const prevWorkspaceRef = useRef(workspaceActive)
  useEffect(() => {
    if (workspaceActive === prevWorkspaceRef.current) return
    prevWorkspaceRef.current = workspaceActive
    if (workspaceActive) {
      collapsedBeforeWorkspaceRef.current = sidebarCollapsed
      setSidebarCollapsed(true)
    } else {
      setSidebarCollapsed(collapsedBeforeWorkspaceRef.current)
    }
  }, [workspaceActive, sidebarCollapsed])

  // Sources for the chat composer's "@" picker: CSTX assets, plus IOA
  // nodes/messages. Both refresh when the timeline advances — a finished scan or
  // agent turn often means new assets landed or new IOA traffic was exchanged.
  const [scoNodes, setScoNodes] = useState<SCONode[]>([])
  const [ioaAvailable, setIoaAvailable] = useState(false)
  const ioaEnabled = capabilityManifest?.capabilities.some(item => item.id === 'ioa') || ioaAvailable
  const [ioaNodes, setIoaNodes] = useState<IOANode[]>([])
  const [ioaMessages, setIoaMessages] = useState<IOAMessage[]>([])
  const [composerSeed, setComposerSeed] = useState(EMPTY_SEED)

  const refreshSCONodes = useCallback(async () => {
    if (!scanEnabled) { setScoNodes([]); return }
    try {
      const { items: data } = await listSCONodes()
      setScoNodes(data)
    } catch { /* non-critical */ }
  }, [scanEnabled])

  const refreshIOA = useCallback(async () => {
    try {
      const overview = await getIOAOverview()
      setIoaAvailable(true)
      setIoaNodes(overview.nodes)
      setIoaMessages(overview.messages)
    } catch { setIoaAvailable(false) }
  }, [])

  useEffect(() => {
    if (!scanEnabled) { setScoNodes([]); return }
    void refreshSCONodes()
    const unsubscribe = subscribeCSTXChanges(() => { void refreshSCONodes() })
    return unsubscribe
  }, [refreshSCONodes, scanEnabled])
  // Refresh mentionables when scans finish (timeline changes often signal new results)
  useEffect(() => {
    if (scanEnabled) void syncCSTXArtifacts().catch(() => {})
    void refreshIOA()
  }, [chat.timeline.length, refreshIOA, scanEnabled])

  const mentionables = useMemo(() => assetMentionables(scoNodes), [scoNodes])

  const handleAssetSendToChat = useCallback((text: string) => {
    setComposerSeed({ text, nonce: Date.now() })
  }, [])

  // Always render the picker: even with no assets or IOA traffic yet, the File
  // category (attach) is available inside a live session, so "@" is never a
  // dead key. The picker itself decides which category tabs to show.
  const renderMentionPopup = useCallback(
    (api: MentionPopupApi) => (
      <MentionPicker {...api} nodes={scoNodes} ioaNodes={ioaNodes} ioaMessages={ioaMessages} />
    ),
    [scoNodes, ioaNodes, ioaMessages],
  )

  const model = serverStatus?.llmModel || ''
  const handleSwitchLLM = useCallback(async (profileID: string) => {
    if (!profileID || profileID === activeLLMProfile) return
    setSwitchingLLM(true)
    try {
      const next = await activateLLMProfile(profileID)
      setLLMProfiles(next.llm?.providers ?? [])
      setActiveLLMProfile(next.llm?.activeProfile || profileID)
      await refreshStatus()
    } catch {
      openSettings()
    } finally {
      setSwitchingLLM(false)
    }
  }, [activeLLMProfile, refreshStatus, openSettings])
  const activeSession = chat.activeSessionRecord?.session?.id === chat.activeSessionID ? chat.activeSessionRecord : chat.sessions.find((s) => s.session?.id === chat.activeSessionID) || null
  // The open session's bound agent has dropped off the live roster (its node
  // exited / the hub restarted). The transcript still shows, but a new turn
  // can't be dispatched until it reconnects — surface that in the chat panel.
  const activeAgentOffline = !!activeSession && !isSessionAgentOnline(activeSession, chat.agents)
  const activeNodeIsLocalModel = isLocalModelAgent(chat.agents.find((a) => a.hello?.nodeId === activeSession?.session?.nodeId))

  // On phones the sidebar is an overlay drawer (see SessionList); entering a
  // conversation or terminal should dismiss it so the content isn't left covered.
  // No-op at md+ where the sidebar is a docked rail that shares the row.
  function closeSidebarOnMobile() {
    if (typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches) {
      setSidebarOpen(false)
    }
  }

  function handleOpenTerminal(nodeID: string) {
    services.panels.open('agents', { focusNodeID: nodeID })
    chat.selectNode(nodeID)
    closeSidebarOnMobile()
  }

  function handleSelectSession(id: string) {
    if (activeToolPanel === 'agents') services.panels.close()
    chat.selectSession(id)
    closeSidebarOnMobile()
  }

  function handleCreateSession(nodeID: string) {
    if (activeToolPanel === 'agents') services.panels.close()
    chat.createSession(nodeID)
    closeSidebarOnMobile()
  }

  // 资产视图 → 任务：离开资产视图并打开任务初始会话。
  function openTaskSession(sessionID: string) {
    // 服务端刚发起的会话尚未进入本地列表；先拉取，侧栏即时可见并高亮。
    void chat.refreshSessions()
    handleSelectSession(sessionID)
  }

  // 验证/报告/复测会话顶栏的返回：回到其所属任务（S1 session-kinds 的
  // task_session_id → 打开任务初始会话）；取不到归属时回任务页。
  const handleBackToParentTask = useCallback(async () => {
    const id = activeSessionIDRef.current
    if (!id) { navigate('tasks'); return }
    try {
      const items = await getWebApiSessionKinds([id])
      const target = items[id]?.task_session_id
      if (activeSessionIDRef.current !== id) return
      if (target) openTaskSession(target)
      else navigate('tasks')
    } catch {
      navigate('tasks')
    }
  }, [navigate])

  // 从验证 session 返回原候选：定位到父任务 finding，而不是只给侧栏说明。
  async function handleReturnToFinding() {    if (!activeWebApiVerification) return
    const verification = activeWebApiVerification
    try {
      const parent = await getWebApiTask(verification.task_id)
      if (activeSessionIDRef.current !== verification.session_id) return
      pendingFindingIdRef.current = { sessionID: parent.initial_session_id, findingID: verification.finding_id }
      setActiveWebApiVerification(null)
      void chat.selectSession(parent.initial_session_id)
    } catch (err) {
      if (activeSessionIDRef.current === verification.session_id) setWebApiTaskError(err instanceof Error ? err.message : String(err))
    }
  }

  // 从报告 session 返回报告视图：定位到父任务报告并选中该报告，而不是只给侧栏说明。
  async function handleReturnToReport() {
    if (!activeWebApiReport) return
    const report = activeWebApiReport
    try {
      const parent = await getWebApiTask(report.task_id)
      if (activeSessionIDRef.current !== report.session_id) return
      pendingReportIdRef.current = { sessionID: parent.initial_session_id, reportID: report.report_id }
      setActiveWebApiReport(null)
      void chat.selectSession(parent.initial_session_id)
    } catch (err) {
      if (activeSessionIDRef.current === report.session_id) setWebApiTaskError(err instanceof Error ? err.message : String(err))
    }
  }

  // 从复测 session 返回原候选：定位到父任务 finding，而不是只给侧栏说明。
  async function handleReturnToFindingFromRetest() {
    if (!activeWebApiRetest) return
    const retest = activeWebApiRetest
    try {
      const parent = await getWebApiTask(retest.task_id)
      if (activeSessionIDRef.current !== retest.session_id) return
      pendingFindingIdRef.current = { sessionID: parent.initial_session_id, findingID: retest.finding_id }
      setActiveWebApiRetest(null)
      void chat.selectSession(parent.initial_session_id)
    } catch (err) {
      if (activeSessionIDRef.current === retest.session_id) setWebApiTaskError(err instanceof Error ? err.message : String(err))
    }
  }

  useLayoutEffect(() => {
    services.workbench.publish({ chat, serverStatus, capabilityIDs, ioaEnabled, observationCount: observations.length,
      assetCount: scoNodes.length, refreshStatus, refreshAssets: refreshSCONodes, sendToChat: handleAssetSendToChat })
  })
  useLayoutEffect(() => () => { services.workbench.set(null) }, [services])

  // 渲染兜底：即使 effect 尚未执行，也绝不把旧 Task/流量挂到新 session 下。
  const webApiTaskActive = webapiEnabled && !!activeWebApiTask && activeWebApiTask.initial_session_id === chat.activeSessionID
  const webApiVerificationActive = webapiEnabled && !!activeWebApiVerification && activeWebApiVerification.session_id === chat.activeSessionID
  const webApiReportActive = webapiEnabled && !!activeWebApiReport && activeWebApiReport.session_id === chat.activeSessionID
  const webApiRetestActive = webapiEnabled && !!activeWebApiRetest && activeWebApiRetest.session_id === chat.activeSessionID
  // 执行时间线数据：验证/报告/复测/任务会话的「执行」视图都显示（纯函数派生，不新增接口）。
  const executionSessionActive = webApiTaskActive || webApiVerificationActive || webApiReportActive || webApiRetestActive
  const executionData = useMemo(
    () => (executionSessionActive
      ? buildExecutionTimeline(
        chat.aopEvents,
        Object.entries(guardrails.bySession).flatMap(([sessionId, reviews]) => reviews.map((review) => (
          review.sessionId === sessionId ? review : { ...review, sessionId }
        ))),
        guardrails.unavailable[chat.activeSessionID || ''] !== true,
      )
      : null),
    [executionSessionActive, chat.aopEvents, guardrails.bySession, guardrails.unavailable, chat.activeSessionID],
  )
  // 执行视图置顶待审批卡的批准/拒绝：从 live reviews 找回原 Review 走 guardrails.resolve。
  const resolveExecutionApproval = useCallback(async (approval: ExecutionApproval, approve: boolean) => {
    const reviews = guardrails.bySession[approval.sessionId] || []
    const review = reviews.find((item) => item.operation?.operationId === approval.id)
    if (!review) throw new Error('审批已不在待处理列表，请刷新后查看最新状态。')
    await guardrails.resolve(approval.sessionId, review, approve)
  }, [guardrails])

  // 时间线「在流量中查看」：切换流量视图并定位该 evidence（模式同 focusFindingId）。
  const handleViewEvidence = useCallback((taskId: string, evidenceId: string) => {
    if (!activeWebApiTask || activeWebApiTask.id !== taskId) return
    setFocusEvidenceId(evidenceId)
    setWorkspaceView('traffic')
  }, [activeWebApiTask])

  // 时间线「查看候选」：切换候选视图并定位该 finding。
  const handleViewFinding = useCallback((findingId: string) => {
    setFocusFindingId(findingId)
    setWorkspaceView('findings')
  }, [])

  // 全局用量 Dialog 点击任务行：关闭 Dialog，按 task_id 解析出任务初始会话后打开该任务。
  const handleOpenUsageTask = useCallback(async (taskID: string) => {
    if (!taskID) return
    setUsageDialogOpen(false)
    try {
      const task = await getWebApiTask(taskID)
      void chat.selectSession(task.initial_session_id)
    } catch (err) {
      setWebApiTaskError(err instanceof Error ? err.message : String(err))
    }
  }, [chat.selectSession])

  // 全局漏洞页打开任务候选：按 task_id 解析初始会话，再经 pendingFindingIdRef
  // 在任务工作区切到候选视图并定位（同 handleReturnToFinding）。
  const handleOpenTaskFinding = useCallback(async (taskID: string, findingID: string) => {
    try {
      const task = await getWebApiTask(taskID)
      pendingFindingIdRef.current = { sessionID: task.initial_session_id, findingID }
      void chat.selectSession(task.initial_session_id)
    } catch (err) {
      setWebApiTaskError(err instanceof Error ? err.message : String(err))
    }
  }, [chat.selectSession])

  // 全局报告页打开报告：按 task_id 解析初始会话，再经 pendingReportIdRef
  // 在任务工作区切到报告视图并选中该报告（同 handleReturnToReport）。
  const handleOpenTaskReport = useCallback(async (taskID: string, reportID: string) => {
    try {
      const task = await getWebApiTask(taskID)
      pendingReportIdRef.current = { sessionID: task.initial_session_id, reportID }
      void chat.selectSession(task.initial_session_id)
    } catch (err) {
      setWebApiTaskError(err instanceof Error ? err.message : String(err))
    }
  }, [chat.selectSession])

  // 全局漏洞页点资产条目：打开资产页并定位漏洞库中该条（自动展开）。
  const handleOpenAssetVuln = useCallback((assetID: string, vulnID: string) => {
    setFocusAssetId(assetID)
    setFocusVulnId(vulnID)
    navigate('assets')
  }, [navigate])

  return (
    <ThemeProvider initial={getInitialTheme()} storageKey="cyber-theme" className="aspect-theme-root h-full text-foreground font-sans antialiased">
    <TooltipProvider delayDuration={300}>
      <div className="flex h-[100dvh] overflow-hidden" data-cyber-product={effectiveManifest?.product || 'cyber-harness'} data-cyber-capabilities={effectiveManifest?.capabilities.map(item => item.id).join(',') || 'core'}>
        <AppSidebar
          open={sidebarOpen}
          collapsed={sidebarCollapsed}
          page={page}
          webapiEnabled={webapiEnabled}
          agents={chat.agents}
          services={services}
          llmProfiles={llmProfiles}
          activeLLMProfile={activeLLMProfile}
          fallbackModel={model}
          switchingLLM={switchingLLM}
          onSwitchLLM={handleSwitchLLM}
          serverURL={serverStatus?.serverUrl}
          version={serverStatus?.version}
          connectionProfiles={effectiveManifest?.profiles}
          pendingApprovalItems={pendingApprovalItems}
          onResolvePending={resolvePendingApproval}
          onViewSession={handleSelectSession}
          onNavigate={navigate}
          onToggleCollapsed={toggleSidebarCollapsed}
          onCloseDrawer={() => setSidebarOpen(false)}
          onOpenUsage={() => setUsageDialogOpen(true)}
        />

        <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
          {chat.activeSessionID ? (
            <>
            {webApiTaskError && (
              <div className="flex shrink-0 items-center gap-2 border-b border-warning/40 bg-warning/10 px-4 py-1.5 text-xs text-warning">
                <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                <span className="min-w-0 truncate">{tc('webapi.loadTaskFailed')}：{webApiTaskError}</span>
              </div>
            )}
            {webApiTaskActive && activeWebApiTask && (
              <WebApiTaskBar
                task={activeWebApiTask}
                onPolicySaved={setActiveWebApiTask}
                onScenarioSaved={setActiveWebApiTask}
                onOpenAsset={webapiEnabled ? (assetId) => { setFocusAssetId(assetId); navigate('assets') } : undefined}
                onBack={() => navigate('tasks')}
                view={workspaceView}
                onViewChange={setWorkspaceView}
                countsRevision={webApiCountsRevision}
              />
            )}
            {webApiVerificationActive && activeWebApiVerification && (
              <WebApiVerificationBar verification={activeWebApiVerification} onReturnToFinding={handleReturnToFinding} onBack={handleBackToParentTask} />
            )}
            {webApiReportActive && activeWebApiReport && (
              <WebApiReportBar report={activeWebApiReport} onReturnToReport={handleReturnToReport} onBack={handleBackToParentTask} />
            )}
            {webApiRetestActive && activeWebApiRetest && (
              <WebApiRetestBar retest={activeWebApiRetest} onReturnToFinding={handleReturnToFindingFromRetest} onBack={handleBackToParentTask} />
            )}
            {!executionSessionActive && (
              <div className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-background px-3">
                <Button type="button" variant="ghost" size="icon-sm" onClick={() => navigate('tasks')} aria-label={tc('shell.backToTasks')} title={tc('shell.backToTasks')} className="shrink-0 text-muted-foreground">
                  <ArrowLeft className="h-4 w-4" />
                </Button>
                <span className="min-w-0 truncate text-[13px] font-medium text-foreground">{activeSession?.session?.title || tc('shell.sessionFallback')}</span>
              </div>
            )}
            {/* 视图行：左侧任务导航列已取消，面板独占整行（纯列方向）。 */}
            <div className="webapi-task-row flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
            {webApiTaskActive && activeWebApiTask && workspaceView === 'traffic' ? (
              <WebApiEvidencePanel
                task={activeWebApiTask}
                focusEvidenceId={focusEvidenceId}
                onFocusConsumed={() => setFocusEvidenceId(null)}
              />
            ) : webApiTaskActive && activeWebApiTask && workspaceView === 'board' ? (
              <WebApiBoardPanel
                task={activeWebApiTask}
                onViewFinding={handleViewFinding}
                onViewEvidence={(evidenceId) => handleViewEvidence(activeWebApiTask.id, evidenceId)}
                onOpenSession={(sessionID) => { void chat.selectSession(sessionID) }}
                onCountsChanged={bumpWebApiCountsRevision}
              />
            ) : webApiTaskActive && activeWebApiTask && workspaceView === 'findings' ? (
              <WebApiFindingsPanel
                task={activeWebApiTask}
                onStartVerification={chat.startWebApiVerification}
                onStartRetest={chat.startWebApiRetest}
                onViewExecution={chat.selectSession}
                verificationLaunch={chat.verificationLaunch}
                onDismissVerificationLaunch={chat.dismissVerificationLaunch}
                onResetVerificationLaunch={chat.resetVerificationLaunch}
                retestLaunch={chat.retestLaunch}
                onDismissRetestLaunch={chat.dismissRetestLaunch}
                onResetRetestLaunch={chat.resetRetestLaunch}
                focusFindingId={focusFindingId}
                onFocusConsumed={() => setFocusFindingId(null)}
                onVerificationStarted={bumpWebApiCountsRevision}
                onRetestStarted={bumpWebApiCountsRevision}
              />
            ) : webApiTaskActive && activeWebApiTask && workspaceView === 'reports' ? (
              <WebApiReportsPanel
                task={activeWebApiTask}
                onStartReport={chat.startWebApiReport}
                onViewExecution={chat.selectSession}
                reportLaunch={chat.reportLaunch[activeWebApiTask.id] ?? null}
                onDismissReportLaunch={() => chat.dismissReportLaunch(activeWebApiTask.id)}
                onResetReportLaunch={() => chat.resetReportLaunch(activeWebApiTask.id)}
                onRetryReportLaunch={(items) => chat.startWebApiReport({ taskID: activeWebApiTask.id, items })}
                focusReportId={focusReportId}
                onFocusConsumed={() => setFocusReportId(null)}
                onReportStarted={bumpWebApiCountsRevision}
              />
            ) : webApiTaskActive && activeWebApiTask && workspaceView === 'usage' ? (
              <WebApiUsagePanel task={activeWebApiTask} onViewExecution={chat.selectSession} />
            ) : executionSessionActive ? (
              <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
                <ExecutionViewHeader mode={executionMode} onModeChange={setExecutionMode} summary={executionData?.summary ?? null} />
                {executionMode === 'timeline' ? (
                  executionData && executionData.entries.length > 0 ? (
                    <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
                      <WebApiExecutionTimeline
                        data={executionData}
                        onViewEvidence={handleViewEvidence}
                        onViewFinding={handleViewFinding}
                        onResolveApproval={resolveExecutionApproval}
                      />
                      {/* 时间线视图保留原生 composer：与控制流模式同一组件与发送路径，
                          发送后新事件追加到时间线（executionData 由会话事件驱动）。 */}
                      <ChatPanel
                        composerOnly
                        resolveExtension={services.slots.resolve}
                        extensionRevision={extensionRevision}
                        timeline={chat.timeline}
                        guardrailUnavailable={guardrails.unavailable[chat.activeSessionID || ''] === true}
                        guardrailReviews={guardrails.bySession[chat.activeSessionID || ''] || []}
                        onResolveGuardrail={(review, approve) => guardrails.resolve(chat.activeSessionID!, review, approve)}
                        aopEvents={chat.aopEvents}
                        isThinking={chat.isThinking}
                        isBusy={chat.busy}
                        canPause={chat.canPause}
                        error={chat.error}
                        activeSessionID={chat.activeSessionID}
                        hasActiveSession={chat.activeSessionID !== null}
                        agentOffline={activeAgentOffline}
                        agentName={activeSession?.agentName}
                        agents={chat.agents.map((a) => ({ nodeID: a.hello?.nodeId || '', name: a.hello?.name || '' }))}
                        onCreateSession={handleCreateSession}
                        onOpenTerminal={handleOpenTerminal}
                        onOpenIOA={openIOAConsole}
                        mentionables={mentionables}
                        renderMentionPopup={renderMentionPopup}
                        injectText={composerSeed}
                        onSend={chat.sendMessage}
                        ensureSession={chat.ensureSession}
                        onPause={chat.cancelMessage}
                        onClearError={chat.clearError}
                        localModelActive={activeNodeIsLocalModel}
                      />
                    </div>
                  ) : (
                    <div className="flex flex-1 flex-col overflow-hidden">
                      <div className="flex flex-1 items-center justify-center px-4 text-center text-sm text-muted-foreground">{tc('webapi.execNoEntries')}</div>
                      <ChatPanel
                        composerOnly
                        resolveExtension={services.slots.resolve}
                        extensionRevision={extensionRevision}
                        timeline={chat.timeline}
                        guardrailUnavailable={guardrails.unavailable[chat.activeSessionID || ''] === true}
                        guardrailReviews={guardrails.bySession[chat.activeSessionID || ''] || []}
                        onResolveGuardrail={(review, approve) => guardrails.resolve(chat.activeSessionID!, review, approve)}
                        aopEvents={chat.aopEvents}
                        isThinking={chat.isThinking}
                        isBusy={chat.busy}
                        canPause={chat.canPause}
                        error={chat.error}
                        activeSessionID={chat.activeSessionID}
                        hasActiveSession={chat.activeSessionID !== null}
                        agentOffline={activeAgentOffline}
                        agentName={activeSession?.agentName}
                        agents={chat.agents.map((a) => ({ nodeID: a.hello?.nodeId || '', name: a.hello?.name || '' }))}
                        onCreateSession={handleCreateSession}
                        onOpenTerminal={handleOpenTerminal}
                        onOpenIOA={openIOAConsole}
                        mentionables={mentionables}
                        renderMentionPopup={renderMentionPopup}
                        injectText={composerSeed}
                        onSend={chat.sendMessage}
                        ensureSession={chat.ensureSession}
                        onPause={chat.cancelMessage}
                        onClearError={chat.clearError}
                        localModelActive={activeNodeIsLocalModel}
                      />
                    </div>
                  )
                ) : (
                  <ChatPanel
                    resolveExtension={services.slots.resolve}
                    extensionRevision={extensionRevision}
                    timeline={chat.timeline}
                    guardrailUnavailable={guardrails.unavailable[chat.activeSessionID || ''] === true}
                    guardrailReviews={guardrails.bySession[chat.activeSessionID || ''] || []}
                    onResolveGuardrail={(review, approve) => guardrails.resolve(chat.activeSessionID!, review, approve)}
                    aopEvents={chat.aopEvents}
                    isThinking={chat.isThinking}
                    isBusy={chat.busy}
                    canPause={chat.canPause}
                    error={chat.error}
                    activeSessionID={chat.activeSessionID}
                    hasActiveSession={chat.activeSessionID !== null}
                    agentOffline={activeAgentOffline}
                    agentName={activeSession?.agentName}
                    agents={chat.agents.map((a) => ({ nodeID: a.hello?.nodeId || '', name: a.hello?.name || '' }))}
                    onCreateSession={handleCreateSession}
                    onOpenTerminal={handleOpenTerminal}
                    onOpenIOA={openIOAConsole}
                    mentionables={mentionables}
                    renderMentionPopup={renderMentionPopup}
                    injectText={composerSeed}
                    onSend={chat.sendMessage}
                    ensureSession={chat.ensureSession}
                    onPause={chat.cancelMessage}
                    onClearError={chat.clearError}
                    localModelActive={activeNodeIsLocalModel}
                  />
                )}
              </div>
            ) : (
            <ChatPanel
              resolveExtension={services.slots.resolve}
              extensionRevision={extensionRevision}
              timeline={chat.timeline}
              guardrailUnavailable={guardrails.unavailable[chat.activeSessionID || ''] === true}
              guardrailReviews={guardrails.bySession[chat.activeSessionID || ''] || []}
              onResolveGuardrail={(review, approve) => guardrails.resolve(chat.activeSessionID!, review, approve)}
              aopEvents={chat.aopEvents}
              isThinking={chat.isThinking}
              isBusy={chat.busy}
              canPause={chat.canPause}
              error={chat.error}
              activeSessionID={chat.activeSessionID}
              hasActiveSession={chat.activeSessionID !== null}
              agentOffline={activeAgentOffline}
              agentName={activeSession?.agentName}
              agents={chat.agents.map((a) => ({ nodeID: a.hello?.nodeId || '', name: a.hello?.name || '' }))}
              onCreateSession={handleCreateSession}
              onOpenTerminal={handleOpenTerminal}
              onOpenIOA={openIOAConsole}
              mentionables={mentionables}
              renderMentionPopup={renderMentionPopup}
              injectText={composerSeed}
              onSend={chat.sendMessage}
              ensureSession={chat.ensureSession}
              onPause={chat.cancelMessage}
              onClearError={chat.clearError}
              localModelActive={activeNodeIsLocalModel}
            />
            )}
            </div>
            </>
          ) : (
            <>
              {page === 'overview' && (
                <OverviewPage
                  onNavigate={navigate}
                  onOpenTask={openTaskSession}
                  onOpenAsset={(assetId) => { setFocusAssetId(assetId); navigate('assets') }}
                  onOpenUsage={() => setUsageDialogOpen(true)}
                  onOpenVulnsOverdue={() => { setVulnsOverdueRequested(true); navigate('vulns') }}
                  onOpenMenu={() => setSidebarOpen(true)}
                />
              )}
              {page === 'tasks' && (
                <TasksPage
                  webapiEnabled={webapiEnabled}
                  onOpenTask={openTaskSession}
                  onCreateTask={() => setTaskDialogOpen(true)}
                  onOpenAsset={(assetId) => { setFocusAssetId(assetId); navigate('assets') }}
                  onOpenMenu={() => setSidebarOpen(true)}
                  onArchive={(id, archived) => chat.updateSession(id, { archived })}
                />
              )}
              {page === 'vulns' && (
                webapiEnabled ? (
                  <VulnsPage
                    webapiEnabled={webapiEnabled}
                    onOpenAssetVuln={handleOpenAssetVuln}
                    onOpenTaskFinding={handleOpenTaskFinding}
                    onOpenMenu={() => setSidebarOpen(true)}
                    overdueRequested={vulnsOverdueRequested}
                    onOverdueRequestedConsumed={() => setVulnsOverdueRequested(false)}
                  />
                ) : (
                  <Page>
                    <PageHeader title={tc('shell.navVulns')} onOpenMenu={() => setSidebarOpen(true)} menuLabel={tc('shell.openMenu')} />
                  </Page>
                )
              )}
              {page === 'reports' && (
                webapiEnabled ? (
                  <ReportsPage
                    webapiEnabled={webapiEnabled}
                    onOpenTaskReport={handleOpenTaskReport}
                    onOpenMenu={() => setSidebarOpen(true)}
                  />
                ) : (
                  <Page>
                    <PageHeader title={tc('shell.navReports')} onOpenMenu={() => setSidebarOpen(true)} menuLabel={tc('shell.openMenu')} />
                  </Page>
                )
              )}
              {page === 'assets' && (
                webapiEnabled ? (
                  <WebApiAssetsPanel
                    focusAssetId={focusAssetId}
                    focusVulnId={focusVulnId}
                    onFocusConsumed={() => setFocusAssetId(null)}
                    onFocusVulnConsumed={() => setFocusVulnId(null)}
                    onOpenTask={openTaskSession}
                  />
                ) : (
                  <Page>
                    <PageHeader title={tc('shell.navAssets')} onOpenMenu={() => setSidebarOpen(true)} menuLabel={tc('shell.openMenu')} />
                  </Page>
                )
              )}
              {page === 'models' && (
                <Page>
                  <PageHeader title={tc('shell.modelsTitle')} onOpenMenu={() => setSidebarOpen(true)} menuLabel={tc('shell.openMenu')} />
                  {/* 设置面板以页面模式内嵌：同一份内容 JSX，不渲染 Dialog/遮罩。 */}
                  <ConfigPanel
                    variant="page"
                    open
                    status={serverStatus}
                    capabilities={capabilityIDs}
                    initialSection="llm"
                    onClose={() => {}}
                    onSaved={() => { void refreshStatus() }}
                  />
                </Page>
              )}
              {page === 'nodes' && (
                <Page className="flex flex-col" contentClassName="flex min-h-0 flex-1 flex-col space-y-3">
                  <PageHeader title={tc('shell.nodesTitle')} onOpenMenu={() => setSidebarOpen(true)} menuLabel={tc('shell.openMenu')} />
                  {/* Agent 控制台以页面模式内嵌：同一份内容 JSX，不渲染 ToolDrawer。 */}
                  <AgentPanel variant="page" open agents={chat.agents} onClose={() => {}} />
                </Page>
              )}
              {page === 'integrations' && (
                webapiEnabled ? (
                  <IntegrationsPage
                    onOpenSession={openTaskSession}
                    onOpenMenu={() => setSidebarOpen(true)}
                  />
                ) : (
                  <Page>
                    <PageHeader title={tc('shell.integrationsTitle')} onOpenMenu={() => setSidebarOpen(true)} menuLabel={tc('shell.openMenu')} />
                  </Page>
                )
              )}
            </>
          )}
        </main>
      </div>

      <WebApiTaskDialog open={taskDialogOpen} onClose={() => setTaskDialogOpen(false)} onSubmit={chat.startWebApiTask} />

      {webapiEnabled && (
        <WebApiUsageDialog open={usageDialogOpen} onClose={() => setUsageDialogOpen(false)} onOpenTask={handleOpenUsageTask} />
      )}

      <WorkbenchSlots services={services} name="shell.panels" />
    </TooltipProvider>
    </ThemeProvider>
  )
}

