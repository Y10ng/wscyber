import React, { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { Button } from 'antd'
import { UIProvider } from '../ui/UIProvider.tsx'
import { AppShell } from '../ui/AppShell.tsx'
import { TaskModal } from '../ui/TaskModal.tsx'
import type { SurfaceTheme } from '../ui/surface.ts'
import { contentPathname, createCloseDestination, defaultCreateContext, nextCreateContext, parsePathname, readRouteLocation } from './routing.ts'
import {
  LogoutOutlined,
  ProjectOutlined,
  MoonOutlined,
  SunOutlined,
  SettingOutlined,
  BarChartOutlined,
  FolderOutlined,
} from '@ant-design/icons'
import {
  ApiError,
  StaleSessionError,
  fetchMission,
  fetchMissionThreads,
  fetchMissions,
  fetchOperationReceipt,
  fetchSystem,
  fetchThreadSnapshot,
  hasToken,
  invalidateSession,
  postExploration,
  setOnUnauthorized,
  verifyAndSetToken,
} from './api.ts'
import type { ExplorationDraft, ExplorationSubmissionState } from './idempotency.ts'
import {
  beginSubmission,
  handleOperationCheck404,
  handlePostFailure,
  handleSessionUnauthorizedDuringSubmission,
  initialSubmissionState,
} from './idempotency.ts'
import type {
  ExplorationInput,
  Mission,
  Snapshot,
  SystemInfo,
  Thread,
} from './types.ts'
import {
  WorkspaceRequestTracker,
  computeWorkspacePollDelayMs,
  resolveWorkspaceThreadTarget,
  selectMatchedSnapshot,
} from './workspace.ts'
import { LoginView } from './components/LoginView.tsx'
import { ProjectListView } from './components/ProjectListView.tsx'
import { ExplorationCreateActions, ExplorationCreateView } from './components/ExplorationCreateView.tsx'
import { ProjectWorkspaceView } from './components/ProjectWorkspaceView.tsx'
import { ModelSettingsView } from './components/ModelSettingsView.tsx'
import { ModelTestSession, canStartExploration, type ModelConfig } from './models.ts'
import { fetchModelConfigs } from './modelApi.ts'
import { UsageView } from './components/UsageView.tsx'
import { PriceEditSession } from './modelUsage.ts'
import { LibrarySession } from './library.ts'
import { ResearchControlSession } from './researchControls.ts'
import { VerificationSession } from './verificationSession.ts'
import { VerificationContext, VerificationPending } from './components/VerificationWorkspace.tsx'
import { LibraryView } from './components/LibraryView.tsx'
import { hasBriefDraft } from '../ui/researchBrief.ts'

const emptyDraft: ExplorationDraft = {
  description: '',
  goal: '',
  originsText: '',
}

export const App: React.FC = () => {
  const verificationSession = useRef(new VerificationSession())
  const verificationState = useSyncExternalStore(verificationSession.current.subscribe, verificationSession.current.snapshot)
  const researchSession = useRef(new ResearchControlSession())
  const researchState = useSyncExternalStore(researchSession.current.subscribe, researchSession.current.snapshot)
  const researchOwner = researchState.pending ?? researchState.draft
  const librarySession = useRef(new LibrarySession())
  const libraryState = useSyncExternalStore(librarySession.current.subscribe, librarySession.current.snapshot)
  const priceSession = useRef(new PriceEditSession())
  const modelTests = useRef(new ModelTestSession(window.history.state?.wscyberModelTests))
  const [authenticated, setAuthenticated] = useState<boolean>(() => hasToken())
  const [reauthNotice, setReauthNotice] = useState<string>('')
  const [systemInfo, setSystemInfo] = useState<SystemInfo | null>(null)
  const [modelChoices, setModelChoices] = useState<ModelConfig[] | null>(null)
  const [modelChoicesLoading, setModelChoicesLoading] = useState(false)
  const [modelChoicesError, setModelChoicesError] = useState('')
  const [modelRefresh, setModelRefresh] = useState(0)
  const refreshModels = useCallback(() => setModelRefresh(n => n + 1), [])

  const [appearance, setAppearance] = useState<SurfaceTheme>('light')
  const [location, setLocation] = useState(() => readRouteLocation(window.location.pathname, window.history.state))
  const pathname = location.pathname
  const createContextRef = useRef(location.create)
  const createTriggerRef = useRef<HTMLElement | null>(null)
  const createButtonRef = useRef<React.ComponentRef<typeof Button>>(null)
  const [returnPathAfterLogin, setReturnPathAfterLogin] = useState<string>(() => {
    const p = window.location.pathname
    return p && p !== '/login' && p !== '/' ? p : '/projects'
  })

  // 项目列表状态
  const [missions, setMissions] = useState<Mission[]>([])
  const [missionsInitialLoading, setMissionsInitialLoading] = useState<boolean>(false)
  const [missionsRefreshing, setMissionsRefreshing] = useState<boolean>(false)
  const [missionsError, setMissionsError] = useState<string>('')

  // 项目与研究方向快照状态
  const [activeMission, setActiveMission] = useState<Mission | null>(null)
  const [missionThreads, setMissionThreads] = useState<Thread[]>([])
  const [activeSnapshot, setActiveSnapshot] = useState<Snapshot | null>(null)
  const [loadingMission, setLoadingMission] = useState<boolean>(false)
  const [loadingSnapshot, setLoadingSnapshot] = useState<boolean>(false)
  const [workspaceError, setWorkspaceError] = useState<string>('')
  const [workspaceThreadError, setWorkspaceThreadError] = useState<string>('')
  const [acceptedBanner, setAcceptedBanner] = useState<string>('')

  // 工作区请求代次与串行有界轮询状态
  const workspaceTrackerRef = useRef<WorkspaceRequestTracker>(new WorkspaceRequestTracker())
  const logoutEpochRef = useRef<number>(0)
  const [workspaceFetchInFlight, setWorkspaceFetchInFlight] = useState<boolean>(false)
  const [workspacePollCount, setWorkspacePollCount] = useState<number>(0)
  const [workspaceConsecutiveFailures, setWorkspaceConsecutiveFailures] = useState<number>(0)
  const [workspaceTerminalStatus, setWorkspaceTerminalStatus] = useState<number | null>(null)
  const [documentHidden, setDocumentHidden] = useState<boolean>(() =>
    typeof document !== 'undefined' ? document.visibilityState === 'hidden' : false,
  )

  // 内存探索草稿与可靠提交状态（跨应用内导航与 401 重认证保留，退出清空）
  const [draft, setDraft] = useState<ExplorationDraft>(emptyDraft)
  const [submissionState, setSubmissionState] =
    useState<ExplorationSubmissionState>(initialSubmissionState)

  const navigate = useCallback((nextPath: string, replace = false) => {
    // 跨设置/项目刷新只携带待核对对象ID，不携带版本、回执或任何配置/秘密。
    let historyState: Record<string, unknown> = { wscyberModelTests: modelTests.current.identities() }
    if (parsePathname(nextPath).kind === 'create') {
      createContextRef.current = nextCreateContext(window.location.pathname, createContextRef.current, replace)
      historyState = { ...historyState, wscyberCreate: createContextRef.current }
    }
    if (window.location.pathname !== nextPath) {
      if (replace) {
        window.history.replaceState(historyState, '', nextPath)
      } else {
        window.history.pushState(historyState, '', nextPath)
      }
    }
    setLocation(readRouteLocation(nextPath, historyState))
  }, [])

  const openCreate = () => {
    createTriggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    navigate('/projects/new')
  }
  const closeCreate = () => {
    const destination = createCloseDestination(createContextRef.current)
    if (destination.back) window.history.back()
    else navigate(destination.pathname, true)
  }
  const restoreCreateFocus = () => {
    // 仅关闭回来源时归还焦点；提交成功跳转、401、退出或立即重开不抢焦点。
    if (!hasToken() || window.location.pathname !== createContextRef.current.returnPath) return
    const trigger = createTriggerRef.current
    if (trigger?.isConnected && trigger.checkVisibility({ checkVisibilityCSS: true })) trigger.focus({ preventScroll: true })
    else createButtonRef.current?.focus({ preventScroll: true })
  }

  // 立即清空/遮蔽服务端敏感数据（用于 401 或退出）
  const maskSensitiveServerData = useCallback(() => {
    modelTests.current.mask()
    librarySession.current.mask()
    researchSession.current.mask()
    verificationSession.current.mask()
    workspaceTrackerRef.current.invalidate()
    setWorkspaceFetchInFlight(false)
    setWorkspacePollCount(0)
    setWorkspaceConsecutiveFailures(0)
    setWorkspaceTerminalStatus(null)
    setSystemInfo(null)
    setModelChoices(null)
    setModelChoicesError('')
    setModelChoicesLoading(false)
    setMissions([])
    setMissionsError('')
    setActiveMission(null)
    setMissionThreads([])
    setActiveSnapshot(null)
    setLoadingMission(false)
    setLoadingSnapshot(false)
    setWorkspaceError('')
    setWorkspaceThreadError('')
    setAcceptedBanner('')
  }, [])

  // 注册全局 401 处理：立即遮蔽旧敏感数据、将在途提交/核对转为 unverified 并保留草稿与提交键
  useEffect(() => {
    setOnUnauthorized(() => {
      maskSensitiveServerData()
      setSubmissionState((prev) => handleSessionUnauthorizedDuringSubmission(prev))
      setAuthenticated(false)
      setReauthNotice('访问令牌已失效，已遮蔽工作台数据，请重新输入访问令牌。')
      const currentPath = window.location.pathname
      if (currentPath && currentPath !== '/login' && currentPath !== '/') {
        setReturnPathAfterLogin(currentPath)
      }
      navigate('/login', true)
    })
    return () => setOnUnauthorized(null)
  }, [maskSensitiveServerData, navigate])

  // 监听浏览器前进/后退与标签页可见性
  useEffect(() => {
    const onPopState = () => {
      const nextLocation = readRouteLocation(window.location.pathname, window.history.state)
      if (parsePathname(nextLocation.pathname).kind === 'create') createContextRef.current = nextLocation.create
      setLocation(nextLocation)
    }
    const onVisibilityChange = () => {
      setDocumentHidden(document.visibilityState === 'hidden')
    }
    window.addEventListener('popstate', onPopState)
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      window.removeEventListener('popstate', onPopState)
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [])

  const hasPreservedDraftOrKey = useMemo(() => {
    return (
      Boolean(verificationState.pending) ||
      Boolean(libraryState.draft || libraryState.pending) ||
      Boolean(submissionState.operationKey) ||
      Boolean(draft.description.trim()) ||
      Boolean(draft.goal.trim()) ||
      Boolean(draft.originsText.trim()) || Boolean(draft.modelConfigID) || Boolean(draft.recordModelContent) || Boolean(draft.materials?.length || draft.safetyPromptEnabled || draft.safetyPrompt) || hasBriefDraft(draft.researchBrief)
    )
  }, [draft, submissionState.operationKey, libraryState.draft, libraryState.pending, verificationState.pending])

  // 刷新/关闭提醒：有草稿或待核对提交键时提示，不承诺恢复
  useEffect(() => {
    if (!hasPreservedDraftOrKey) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [hasPreservedDraftOrKey])

  const loadMissionsList = useCallback(async (isInitial: boolean) => {
    if (isInitial) {
      setMissionsInitialLoading(true)
    } else {
      setMissionsRefreshing(true)
    }
    setMissionsError('')
    try {
      const [sys, list] = await Promise.all([fetchSystem(), fetchMissions()])
      setSystemInfo(sys)
      setMissions(list)
    } catch (err) {
      if (err instanceof StaleSessionError) return
      if (err instanceof ApiError && err.status === 401) return
      setMissionsError(err instanceof Error ? err.message : '读取项目列表失败')
    } finally {
      setMissionsInitialLoading(false)
      setMissionsRefreshing(false)
    }
  }, [])

  const loadWorkspaceData = useCallback(
    async (
      missionId: string,
      preferredThreadId: string | null,
      options: { silent?: boolean; resetPoll?: boolean } = {},
    ) => {
      const gen = workspaceTrackerRef.current.begin(missionId, preferredThreadId)
      const silent = Boolean(options.silent)

      if (options.resetPoll) {
        setWorkspacePollCount(0)
        setWorkspaceConsecutiveFailures(0)
        setWorkspaceTerminalStatus(null)
      }

      setWorkspaceFetchInFlight(true)
      if (!silent) {
        setLoadingMission(true)
        setLoadingSnapshot(true)
        setWorkspaceError('')
        setWorkspaceThreadError('')
      }

      try {
        const [sys, missionData, threadsData] = await Promise.all([
          fetchSystem(),
          fetchMission(missionId),
          fetchMissionThreads(missionId),
        ])

        if (!workspaceTrackerRef.current.isCurrent(gen, missionId, preferredThreadId)) {
          return
        }

        setSystemInfo(sys)
        setActiveMission(missionData)
        setMissionThreads(threadsData)
        setWorkspaceError('')

        const { targetThreadId, isExplicit } = resolveWorkspaceThreadTarget(
          preferredThreadId,
          threadsData,
        )

        if (!targetThreadId) {
          setActiveSnapshot(null)
          setWorkspaceThreadError('')
          setWorkspaceConsecutiveFailures(0)
          return
        }

        try {
          const snap = await fetchThreadSnapshot(targetThreadId)
          if (!workspaceTrackerRef.current.isCurrent(gen, missionId, preferredThreadId)) {
            return
          }
          if (snap.mission.id !== missionId) {
            setActiveSnapshot(null)
            setWorkspaceThreadError('指定的研究方向不属于当前项目，请从左侧列表选择有效方向。')
            setWorkspaceTerminalStatus(404)
            return
          }
          setActiveSnapshot(snap)
          setWorkspaceThreadError('')
          setWorkspaceConsecutiveFailures(0)
          setWorkspaceTerminalStatus(null)
        } catch (threadErr) {
          if (threadErr instanceof StaleSessionError) return
          if (threadErr instanceof ApiError && threadErr.status === 401) return
          if (!workspaceTrackerRef.current.isCurrent(gen, missionId, preferredThreadId)) {
            return
          }
          if (
            threadErr instanceof ApiError &&
            (threadErr.status === 404 || threadErr.status === 403)
          ) {
            // 显式指定或目标方向 404/403 时禁止静默回退到其他研究方向，并设置终止状态码停止轮询
            setActiveSnapshot(null)
            setWorkspaceThreadError(
              threadErr.status === 403
                ? threadErr.message || '无权访问该研究方向。'
                : isExplicit
                  ? `未找到指定的研究方向（ID: ${targetThreadId}），请从左侧列表选择有效方向。`
                  : '研究方向不存在或已被移除。',
            )
            setWorkspaceTerminalStatus(threadErr.status)
            return
          }
          setWorkspaceConsecutiveFailures((prev) => prev + 1)
          setWorkspaceThreadError(
            threadErr instanceof Error ? threadErr.message : '读取研究方向快照失败',
          )
        }
      } catch (err) {
        if (err instanceof StaleSessionError) return
        if (err instanceof ApiError && err.status === 401) return
        if (!workspaceTrackerRef.current.isCurrent(gen, missionId, preferredThreadId)) {
          return
        }
        if (err instanceof ApiError && (err.status === 404 || err.status === 403)) {
          setActiveMission(null)
          setMissionThreads([])
          setActiveSnapshot(null)
          setWorkspaceTerminalStatus(err.status)
        } else {
          setWorkspaceConsecutiveFailures((prev) => prev + 1)
        }
        setWorkspaceError(err instanceof Error ? err.message : '读取研究状态失败')
      } finally {
        if (workspaceTrackerRef.current.isCurrent(gen, missionId, preferredThreadId)) {
          setWorkspaceFetchInFlight(false)
          setLoadingMission(false)
          setLoadingSnapshot(false)
        }
      }
    },
    [],
  )

  const route = useMemo(() => parsePathname(pathname), [pathname])
  const contentPath = contentPathname(location)
  const contentRoute = useMemo(() => parsePathname(contentPath), [contentPath])
  const routeMissionId = contentRoute.kind === 'workspace' ? contentRoute.missionId : null
  const routeThreadId = contentRoute.kind === 'workspace' ? contentRoute.threadId : null

  // 根据认证状态与当前路径同步数据
  useEffect(() => {
    if (!authenticated) {
      workspaceTrackerRef.current.invalidate()
      if (route.kind !== 'login') {
        if (pathname !== '/' && pathname !== '/login') {
          setReturnPathAfterLogin(pathname)
        }
        navigate('/login', true)
      }
      return
    }

    if (route.kind === 'login' || pathname === '/') {
      navigate('/projects', true)
      return
    }

  }, [authenticated, route.kind, pathname, navigate])

  // Modal 不替换来源工作面；相同来源开关时不清空快照/选中页签或重复读取。
  useEffect(() => {
    if (!authenticated) return
    if (contentRoute.kind === 'projects') {
      workspaceTrackerRef.current.invalidate()
      setWorkspaceFetchInFlight(false)
      void loadMissionsList(missions.length === 0)
    } else if (contentRoute.kind === 'workspace') {
      void loadWorkspaceData(contentRoute.missionId, contentRoute.threadId, { resetPoll: true })
    } else {
      workspaceTrackerRef.current.invalidate()
      setWorkspaceFetchInFlight(false)
    }
  }, [authenticated, contentRoute.kind, routeMissionId, routeThreadId, loadMissionsList, loadWorkspaceData])

  useEffect(() => {
    if (!authenticated) return
    const controller = new AbortController()
    setModelChoices(null); setModelChoicesLoading(true); setModelChoicesError('')
    void (async () => {
      try {
        const system = await fetchSystem(controller.signal)
        if (controller.signal.aborted) return
        setSystemInfo(system)
        if (system.model_management_available) {
          const choices = await fetchModelConfigs(controller.signal)
          if (!controller.signal.aborted) setModelChoices(choices)
        }
      } catch (error) {
        if (!controller.signal.aborted && !(error instanceof StaleSessionError) && !(error instanceof ApiError && error.status === 401)) setModelChoicesError('模型选项读取失败，请重新读取。')
      } finally { if (!controller.signal.aborted) setModelChoicesLoading(false) }
    })()
    return () => controller.abort()
  }, [authenticated, route.kind, modelRefresh])

  const effectiveWorkspaceThreadId = useMemo(() => {
    if (contentRoute.kind !== 'workspace') return null
    const { targetThreadId } = resolveWorkspaceThreadTarget(
      contentRoute.threadId,
      activeMission && activeMission.id === contentRoute.missionId ? missionThreads : [],
    )
    return targetThreadId
  }, [contentRoute, activeMission, missionThreads])

  const matchedWorkspaceSnapshot = useMemo(() => {
    if (contentRoute.kind !== 'workspace') return null
    return selectMatchedSnapshot(
      activeSnapshot,
      contentRoute.missionId,
      effectiveWorkspaceThreadId,
    )
  }, [contentRoute, activeSnapshot, effectiveWorkspaceThreadId])

  // 有界串行只读轮询：仅在工作区可见、未报错终止且未在请求中时按状态调度下一次只读 GET
  useEffect(() => {
    if (route.kind !== 'workspace' || workspaceFetchInFlight) {
      return
    }
    const delayMs = computeWorkspacePollDelayMs({
      authenticated,
      isWorkspaceRoute: true,
      documentHidden,
      pollCount: workspacePollCount,
      terminalErrorStatus: workspaceTerminalStatus,
      consecutiveFailures: workspaceConsecutiveFailures,
      snapshot: matchedWorkspaceSnapshot,
      threads:
        activeMission && activeMission.id === route.missionId ? missionThreads : [],
    })
    if (delayMs === null) {
      return
    }
    const timer = window.setTimeout(() => {
      setWorkspacePollCount((prev) => prev + 1)
      void loadWorkspaceData(route.missionId, route.threadId, { silent: true })
    }, delayMs)
    return () => window.clearTimeout(timer)
  }, [
    authenticated,
    route,
    documentHidden,
    workspaceFetchInFlight,
    workspacePollCount,
    workspaceTerminalStatus,
    workspaceConsecutiveFailures,
    matchedWorkspaceSnapshot,
    activeMission,
    missionThreads,
    loadWorkspaceData,
  ])

  const handleLogin = async (tokenValue: string) => {
    const sys = await verifyAndSetToken(tokenValue)
    setSystemInfo(sys)
    setAuthenticated(true)
    setReauthNotice('')
    const dest =
      returnPathAfterLogin && returnPathAfterLogin !== '/login'
        ? returnPathAfterLogin
        : '/projects'
    setReturnPathAfterLogin('/projects')
    navigate(dest, true)
  }

  const handleLogout = () => {
    logoutEpochRef.current += 1
    invalidateSession(false)
    modelTests.current.clear()
    priceSession.current.clear()
    librarySession.current.clear()
    researchSession.current.clear()
    verificationSession.current.clear()
    maskSensitiveServerData()
    setDraft(emptyDraft)
    setSubmissionState(initialSubmissionState)
    createContextRef.current = defaultCreateContext
    createTriggerRef.current = null
    setAuthenticated(false)
    setReauthNotice('')
    setReturnPathAfterLogin('/projects')
    navigate('/login', true)
  }

  const executePostWithState = async (nextState: ExplorationSubmissionState) => {
    if (!nextState.operationKey || !nextState.frozenPayload) return
    const capturedLogoutEpoch = logoutEpochRef.current
    setSubmissionState(nextState)
    try {
      const started = await postExploration(
        nextState.operationKey,
        nextState.frozenPayload,
      )
      if (capturedLogoutEpoch !== logoutEpochRef.current) return
      setDraft(emptyDraft)
      setSubmissionState(initialSubmissionState)
      setAcceptedBanner('启动已接收')
      navigate(
        `/projects/${encodeURIComponent(started.mission.id)}/threads/${encodeURIComponent(started.thread.id)}`,
      )
    } catch (err) {
      if (capturedLogoutEpoch !== logoutEpochRef.current) return
      if (err instanceof StaleSessionError) {
        if (err.reason === 'unauthorized') {
          setSubmissionState((prev) => handleSessionUnauthorizedDuringSubmission(prev))
        }
        return
      }
      const status = err instanceof ApiError ? err.status : 0
      const msg = err instanceof Error ? err.message : '提交探索请求失败'
      setSubmissionState((prev) => handlePostFailure(prev, status, msg, err instanceof ApiError ? err.code : undefined))
    }
  }

  const handleSubmitExploration = async (payload: ExplorationInput) => {
    const nextState = beginSubmission(submissionState, payload)
    await executePostWithState(nextState)
  }

  const handleRetrySameKey = async () => {
    if (!submissionState.operationKey || !submissionState.frozenPayload) return
    const nextState = beginSubmission(submissionState, submissionState.frozenPayload)
    await executePostWithState(nextState)
  }

  const handleVerifyOperation = async () => {
    const key = submissionState.operationKey
    if (!key) return
    const capturedLogoutEpoch = logoutEpochRef.current
    setSubmissionState((prev) => ({
      ...prev,
      phase: 'verifying',
      message: '正在查询操作回执…',
    }))
    try {
      const receipt = await fetchOperationReceipt(key)
      if (capturedLogoutEpoch !== logoutEpochRef.current) return
      const missionId = receipt.resource_id
      const [missionData, threadsData] = await Promise.all([
        fetchMission(missionId),
        fetchMissionThreads(missionId),
      ])
      if (capturedLogoutEpoch !== logoutEpochRef.current) return
      setActiveMission(missionData)
      setMissionThreads(threadsData)
      setDraft(emptyDraft)
      setSubmissionState(initialSubmissionState)
      setAcceptedBanner('启动已接收')
      if (threadsData.length > 0) {
        navigate(
          `/projects/${encodeURIComponent(missionId)}/threads/${encodeURIComponent(threadsData[0].id)}`,
        )
      } else {
        navigate(`/projects/${encodeURIComponent(missionId)}`)
      }
    } catch (err) {
      if (capturedLogoutEpoch !== logoutEpochRef.current) return
      if (err instanceof StaleSessionError) {
        if (err.reason === 'unauthorized') {
          setSubmissionState((prev) => handleSessionUnauthorizedDuringSubmission(prev))
        }
        return
      }
      if (err instanceof ApiError && err.status === 404) {
        setSubmissionState((prev) => handleOperationCheck404(prev))
        return
      }
      if (err instanceof ApiError && err.status === 401) {
        setSubmissionState((prev) => handlePostFailure(prev, 401, err.message))
        return
      }
      const msg = err instanceof Error ? err.message : '核对操作回执失败'
      setSubmissionState((prev) => ({
        ...prev,
        phase: 'unverified',
        message: `核对操作回执未能完成：${msg}。已保留原提交键，请稍后继续核对。`,
      }))
    }
  }

  const themeButton = (
    <Button
      type="text"
      aria-label={appearance === 'light' ? '切换到暗色主题' : '切换到浅色主题'}
      title={appearance === 'light' ? '切换到暗色主题' : '切换到浅色主题'}
      icon={appearance === 'light' ? <MoonOutlined /> : <SunOutlined />}
      onClick={() => setAppearance((prev) => prev === 'light' ? 'dark' : 'light')}
    />
  )
  const hasUnsavedDraft = Boolean(draft.description.trim() || draft.goal.trim() || draft.originsText.trim() || draft.modelConfigID || draft.recordModelContent || draft.materials?.length || draft.safetyPromptEnabled || draft.safetyPrompt) || hasBriefDraft(draft.researchBrief)

  return <UIProvider appearance={appearance}><VerificationContext.Provider value={{ session: verificationSession.current, control: researchSession.current, maxSteps: systemInfo?.max_steps ?? 0 }}>
    {!authenticated || route.kind === 'login' ? (
      <LoginView
        reauthNotice={reauthNotice}
        hasPreservedDraftOrKey={hasPreservedDraftOrKey}
        onLogin={handleLogin}
        themeControl={themeButton}
      />
    ) : <>
      <AppShell
        navigationLabel="主导航"
        items={[{ key: 'projects', label: '研究项目', icon: <ProjectOutlined />, group: '研究' }, { key: 'library', label: '资料库', icon: <FolderOutlined />, group: '研究' }, { key: 'usage', label: '模型用量', icon: <BarChartOutlined />, group: '配置' }, { key: 'models', label: '模型配置', icon: <SettingOutlined />, group: '配置' }]}
        activeKey={contentRoute.kind === 'models' ? 'models' : contentRoute.kind === 'usage' ? 'usage' : contentRoute.kind === 'library' ? 'library' : 'projects'}
        onNavigate={(key) => navigate(key === 'models' ? '/settings/models' : key === 'usage' ? '/usage' : key === 'library' ? '/library' : '/projects')}
        footer={<>
          {themeButton}
          <Button type="text" icon={<LogoutOutlined />} onClick={handleLogout}>退出</Button>
        </>}
      >
        <div className="ws-content-container" inert={route.kind === 'create'}>
          <VerificationPending session={verificationSession.current} />
          {researchOwner && <div className="research-write-feedback"><Button onClick={() => navigate(`/projects/${encodeURIComponent(researchOwner.mission)}/threads/${encodeURIComponent(researchOwner.thread)}`)}>{researchState.pending ? '返回原提交方向 · 待核对' : '返回人工补充草稿方向'}</Button></div>}
          {contentRoute.kind === 'library' && <LibraryView session={librarySession.current} />}
          <UsageView active={route.kind === 'usage'} tab={contentRoute.kind === 'usage' ? contentRoute.tab : 'calls'} callID={route.kind === 'usage' ? route.callID : null} session={priceSession.current} navigate={navigate} />
          <ModelSettingsView active={route.kind === 'models'} configID={route.kind === 'models' ? route.configID : null} testID={route.kind === 'models' ? route.testID : null} invalid={route.kind === 'models' && route.invalid} session={modelTests.current} navigate={navigate} onModelsChanged={refreshModels} />
          {contentRoute.kind === 'projects' && (
            <ProjectListView
              missions={missions}
              initialLoading={missionsInitialLoading}
              refreshing={missionsRefreshing}
              error={missionsError}
              hasActiveSubmission={Boolean(submissionState.operationKey)}
              hasUnsavedDraft={hasUnsavedDraft}
              createLabel={submissionState.operationKey ? '核对探索提交' : '新建探索'}
              createButtonRef={createButtonRef}
              onRefresh={() => void loadMissionsList(false)}
              onCreateNew={openCreate}
              onOpenMission={(id) => navigate(`/projects/${encodeURIComponent(id)}`)}
            />
          )}
          {contentRoute.kind === 'workspace' && (
            <ProjectWorkspaceView
              researchSession={researchSession.current}
              maxSteps={systemInfo?.max_steps}
              creationOpen={route.kind === 'create'}
              modelCallsRoute={contentRoute.modelCalls}
              modelCallID={contentRoute.callID ?? null}
              onModelCallsNavigate={(id) => navigate(`/projects/${encodeURIComponent(contentRoute.missionId)}/model-calls${id ? `/${id}` : ''}`)}
              onResearchNavigate={() => navigate(`/projects/${encodeURIComponent(contentRoute.missionId)}`)}
              mission={activeMission?.id === contentRoute.missionId ? activeMission : null}
              threads={activeMission?.id === contentRoute.missionId ? missionThreads : []}
              selectedThreadId={effectiveWorkspaceThreadId}
              snapshot={matchedWorkspaceSnapshot}
              loadingMission={loadingMission}
              loadingSnapshot={loadingSnapshot}
              error={workspaceError}
              threadError={workspaceThreadError}
              acceptedBanner={acceptedBanner}
              onDismissAcceptedBanner={() => setAcceptedBanner('')}
              onBackToList={() => {
                setAcceptedBanner('')
                navigate('/projects')
              }}
              onRefresh={() => void loadWorkspaceData(contentRoute.missionId, contentRoute.threadId, { resetPoll: true })}
              onSelectThread={(threadId) => navigate(`/projects/${encodeURIComponent(contentRoute.missionId)}/threads/${encodeURIComponent(threadId)}`)}
            />
          )}
        </div>
      </AppShell>
      <TaskModal
        title="新建探索"
        open={route.kind === 'create'}
        onCancel={closeCreate}
        closable={{ 'aria-label': '关闭新建探索' }}
        focusable={{ focusTriggerAfterClose: false }}
        afterClose={restoreCreateFocus}
        destroyOnHidden
        footer={<ExplorationCreateActions
          systemInfo={systemInfo}
          canStart={canStartExploration(systemInfo, draft.modelConfigID, modelChoices)}
          submissionState={submissionState}
          hasDraftContent={hasUnsavedDraft}
          onClearDraft={() => setDraft(emptyDraft)}
          onVerifyOperation={handleVerifyOperation}
          onRetrySameKey={handleRetrySameKey}
          onClose={closeCreate}
        />}
      >
        <ExplorationCreateView
          systemInfo={systemInfo}
          models={modelChoices}
          modelsLoading={modelChoicesLoading}
          modelsError={modelChoicesError}
          onRefreshModels={refreshModels}
          draft={draft}
          submissionState={submissionState}
          onChangeDraft={setDraft}
          onSubmitExploration={handleSubmitExploration}
        />
      </TaskModal>
    </>}
  </VerificationContext.Provider></UIProvider>
}
