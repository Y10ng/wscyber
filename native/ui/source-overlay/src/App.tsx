import { useState, useEffect, useLayoutEffect, useCallback, useMemo, useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, Globe, LogOut, Menu, Monitor } from 'lucide-react'
import SessionList from './components/SessionList'
import ChatPanel from './components/ChatPanel'
import WebApiTaskDialog from './components/WebApiTaskDialog'
import WebApiEvidencePanel from './components/WebApiEvidencePanel'
import WebApiFindingsPanel from './components/WebApiFindingsPanel'
import WebApiTaskBar, { type WebApiView } from './components/WebApiTaskBar'
import WebApiTaskNav from './components/WebApiTaskNav'
import WebApiExecutionTimeline, { ExecutionViewHeader, type ExecutionViewMode } from './components/WebApiExecutionTimeline'
import { buildExecutionTimeline } from './lib/execution-timeline'
import { WebApiVerificationBar } from './components/WebApiVerification'
import { WebApiRetestBar } from './components/WebApiRetest'
import WebApiReportsPanel from './components/WebApiReportsPanel'
import WebApiReportBar from './components/WebApiReportBar'
import WebApiUsagePanel from './components/WebApiUsagePanel'
import WebApiUsageDialog from './components/WebApiUsageDialog'
import { assetMentionables } from './components/AssetPanel'
import MentionPicker from './components/MentionPicker'
import QuickConnect from './components/QuickConnect'
import BrandLogo from './components/brand/BrandLogo'
import { useObservations } from '@/viewer'
import { Button, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Tooltip, TooltipContent, TooltipProvider, TooltipTrigger, useConfirm } from '@cyber/ui'
import { ThemeProvider, cn } from '@cyber/theme'
import { activateLLMProfile, APIError, getConfigStatus, getIOAOverview, getStatus, getWebApiReportBySession, getWebApiRetestBySession, getWebApiTask, getWebApiTaskBySession, getWebApiVerificationBySession, logout } from './api'
import type { IOAMessage, IOANode, LLMProviderView, ServerStatus, WebApiReportDetail, WebApiRetestDetail, WebApiTask, WebApiVerificationDetail } from './api'
import type { SCONode } from '@cyber/cstx-easm'
import type { MentionPopupApi } from './viewer'
import { useChatSession } from './hooks/useChatSession'
import { useGuardrailReviews } from './hooks/useGuardrailReviews'
import { GuardrailToggle } from './components/GuardrailToggle'
import { usePolling } from './hooks/usePolling'
import { isLocalModelAgent, isSessionAgentOnline } from './lib/session-agent'
import type { IOAConsoleTarget } from './lib/ioa-navigation'
import { listSCONodes, subscribeCSTXChanges, syncCSTXArtifacts } from './lib/cstx-runtime'

import { useObservable, WorkbenchSlots } from './runtime/react'
import type { HubServices } from './runtime/services'
import type { CapabilityManifest } from './runtime/capabilities'

const sidebarStorageKey = 'cyber-sidebar-open'

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

function getInitialSidebarOpen() {
  if (typeof window === 'undefined') return true
  if (window.matchMedia('(max-width: 767px)').matches) return false
  const stored = window.localStorage.getItem(sidebarStorageKey)
  if (stored === 'true' || stored === 'false') return stored === 'true'
  return window.matchMedia('(min-width: 1024px)').matches
}

export default function App({ services }: { services: HubServices }) {
  const { t } = useTranslation('app')
  const { t: tc } = useTranslation('chat')
  const confirm = useConfirm()
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
  const pendingReviewCounts = useMemo(() => Object.fromEntries(Object.entries(guardrails.bySession).map(([id, reviews]) => [id, reviews.length])), [guardrails.bySession])
  const [serverStatus, setServerStatus] = useState<ServerStatus | null>(null)
  const [llmProfiles, setLLMProfiles] = useState<LLMProviderView[]>([])
  const [activeLLMProfile, setActiveLLMProfile] = useState('')
  const [switchingLLM, setSwitchingLLM] = useState(false)
  const panelSelection = useObservable(services.panels)
  const activeToolPanel = panelSelection?.id ?? null
  const [sidebarOpen, setSidebarOpen] = useState(getInitialSidebarOpen)
  const [taskDialogOpen, setTaskDialogOpen] = useState(false)
  const [usageDialogOpen, setUsageDialogOpen] = useState(false)
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

  useEffect(() => {
    window.localStorage.setItem(sidebarStorageKey, String(sidebarOpen))
  }, [sidebarOpen])

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

  // Deleting a session also tears down its live subscription, so confirm first —
  // matches every other destructive action in the app (node / config).
  async function handleDeleteSession(id: string) {
    if (!(await confirm({ description: tc('deleteSessionConfirm'), destructive: true }))) return
    void chat.deleteSession(id)
  }

  // Agent node clicked (roster / terminal open) → open the agent console focused
  // on that node.
  function handleOpenNode(nodeID: string) {
    services.panels.open('agents', { focusNodeID: nodeID })
  }

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
    () => (executionSessionActive ? buildExecutionTimeline(chat.aopEvents) : null),
    [executionSessionActive, chat.aopEvents],
  )

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

  return (
    <ThemeProvider initial={getInitialTheme()} storageKey="cyber-theme" className="aspect-theme-root h-full text-foreground font-sans antialiased">
    <TooltipProvider delayDuration={300}>
      <div className="flex h-[100dvh] flex-col overflow-hidden" data-cyber-product={effectiveManifest?.product || 'cyber-harness'} data-cyber-capabilities={effectiveManifest?.capabilities.map(item => item.id).join(',') || 'core'}>
        <header className={cn('relative z-[60] flex min-h-12 shrink-0 flex-wrap items-center justify-between gap-1 border-b border-border/60 bg-background px-2 py-1 pt-safe sm:flex-nowrap sm:gap-2 sm:px-4 sm:py-0', (activeToolPanel === 'settings' || taskDialogOpen) && 'z-40')}>
          <div className="flex min-w-0 shrink-0 items-center gap-1 sm:shrink sm:gap-2">
            {/* Phone-only drawer opener — the collapsed sidebar is hidden below md,
                so the session history opens from here (Doubao-style). */}
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => setSidebarOpen(true)}
              aria-label={t('openSessions')}
              className="-ml-1 shrink-0 text-muted-foreground md:hidden"
            >
              <Menu className="h-4 w-4" />
            </Button>
            <BrandLogo size={22} className="hidden shrink-0 sm:block" />
            <h1 className="shrink-0 text-sm font-semibold tracking-tight text-foreground">Cyber</h1>
            <span className="text-[10px] text-muted-foreground">Hub</span>
            <LLMProfileSwitcher
              profiles={llmProfiles}
              activeProfileID={activeLLMProfile}
              fallbackModel={model}
              disabled={switchingLLM}
              onChange={handleSwitchLLM}
            />
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-0.5 sm:gap-2">
            {webapiEnabled && (
              <Button type="button" variant="soft" size="xs" onClick={() => setTaskDialogOpen(true)} aria-label={tc('webapi.newTask')} className="h-7 gap-1 text-[12px]">
                <Globe className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">{tc('webapi.newTask')}</span>
              </Button>
            )}
            <GuardrailToggle disabled={activeToolPanel === 'settings'} onConfigure={() => openSettings('jev')} />
            <WorkbenchSlots services={services} name="shell.header.actions" />
            <QuickConnect serverURL={serverStatus?.serverUrl} version={serverStatus?.version} profiles={effectiveManifest?.profiles} />
            <HeaderIconButton label={t('logout')} onClick={() => { void logout() }}>
              <LogOut className="h-3.5 w-3.5" />
            </HeaderIconButton>
          </div>
        </header>

        <div className="flex min-h-0 flex-1 overflow-hidden">
          <SessionList
            open={sidebarOpen}
            onToggle={() => setSidebarOpen(!sidebarOpen)}
            agents={chat.agents}
            sessions={chat.sessions}
            pendingReviewCounts={pendingReviewCounts}
            filters={chat.sessionFilters}
            onFilter={chat.filterSessions}
            onUpdateSession={chat.updateSession}
            activeSessionID={chat.activeSessionID}
            activeSessionNodeID={activeSession?.session?.nodeId || null}
            activeSessionBusy={chat.busy}
            selectedNodeID={chat.selectedNodeID}
            onSelectSession={handleSelectSession}
            onCreateSession={handleCreateSession}
            onDeleteSession={handleDeleteSession}
            onOpenUsage={webapiEnabled ? () => setUsageDialogOpen(true) : undefined}
          />

          <main className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
            {webApiTaskError && (
              <div className="flex shrink-0 items-center gap-2 border-b border-warning/40 bg-warning/10 px-4 py-1.5 text-xs text-warning">
                <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
                <span className="min-w-0 truncate">{tc('webapi.loadTaskFailed')}：{webApiTaskError}</span>
              </div>
            )}
            {webApiTaskActive && activeWebApiTask && (
              <WebApiTaskBar task={activeWebApiTask} />
            )}
            {webApiVerificationActive && activeWebApiVerification && (
              <WebApiVerificationBar verification={activeWebApiVerification} onReturnToFinding={handleReturnToFinding} />
            )}
            {webApiReportActive && activeWebApiReport && (
              <WebApiReportBar report={activeWebApiReport} onReturnToReport={handleReturnToReport} />
            )}
            {webApiRetestActive && activeWebApiRetest && (
              <WebApiRetestBar retest={activeWebApiRetest} onReturnToFinding={handleReturnToFindingFromRetest} />
            )}
            {/* 任务视图行：≥1100px 为 导航168px | 面板 的水平行；<1100px 改为
                垂直堆叠（导航横向分段在上、面板占满在下），主内容不再被挤出视口。
                断点与 index.css 的 .webapi-task-nav 媒体查询（1099px）保持一致；
                仅任务视图改列向，普通会话（无导航）保持行向不受影响。 */}
            <div className={cn('webapi-task-row flex min-h-0 min-w-0 flex-1 overflow-hidden', webApiTaskActive ? 'flex-col min-[1100px]:flex-row' : 'flex-row')}>
            {webApiTaskActive && activeWebApiTask && (
              <WebApiTaskNav task={activeWebApiTask} view={workspaceView} onViewChange={setWorkspaceView} countsRevision={webApiCountsRevision} />
            )}
            {webApiTaskActive && activeWebApiTask && workspaceView === 'traffic' ? (
              <WebApiEvidencePanel
                task={activeWebApiTask}
                focusEvidenceId={focusEvidenceId}
                onFocusConsumed={() => setFocusEvidenceId(null)}
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
          </main>
        </div>
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

function LLMProfileSwitcher({
  profiles,
  activeProfileID,
  fallbackModel,
  disabled,
  onChange,
}: {
  profiles: LLMProviderView[]
  activeProfileID: string
  fallbackModel: string
  disabled: boolean
  onChange: (profileID: string) => void
}) {
  const { t } = useTranslation('app')

  if (profiles.length === 0) {
    return <span className="ml-1 hidden font-mono text-[10px] uppercase tracking-wider text-muted-foreground sm:inline">{fallbackModel}</span>
  }

  return (
    <Select value={activeProfileID || profiles[0].id} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label={t('switchLLMProfile')}
        className="ml-1 hidden h-7 w-auto min-w-[120px] max-w-[230px] gap-1 border-0 bg-transparent px-2 font-mono text-[10px] text-muted-foreground shadow-none hover:bg-muted/60 hover:text-foreground sm:flex"
      >
        <SelectValue placeholder={fallbackModel} />
      </SelectTrigger>
      <SelectContent align="start">
        {profiles.map(profile => (
          <SelectItem key={profile.id} value={profile.id}>
            {profile.name || profile.model || profile.provider}
            {profile.model && profile.name !== profile.model ? ` · ${profile.model}` : ''}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function HeaderIconButton({ children, label, onClick, active, toolDrawerTrigger }: { children: ReactNode; label: string; onClick: () => void; active?: boolean; toolDrawerTrigger?: boolean }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          active={active}
          data-tool-drawer-trigger={toolDrawerTrigger ? '' : undefined}
          aria-label={label}
          onClick={onClick}
          className={cn('hover:text-foreground', !active && 'text-muted-foreground')}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
