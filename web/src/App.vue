<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  ArrowLeft, ArrowRight, ArrowUpRight, Check, ChevronRight, CircleHelp, Compass,
  FileText, FolderOpen, Globe2, History, KeyRound, ListChecks, LoaderCircle,
  LockKeyhole, LogOut, Pause, Play, Plus, RefreshCw, Search, Settings2,
  ShieldCheck, SquareTerminal, StickyNote, X,
} from 'lucide-vue-next'
import Modal from './components/Modal.vue'
import ActionDetail from './components/ActionDetail.vue'
import { api, ApiError, idPath, setToken } from './api'
import { actionTitle, dateTime, isUnsettled, needsResolution, observationKind, statusText, statusTone } from './types'
import type { Action, ActionInput, Mission, ResearchEvent, Snapshot, SystemInfo, Thread } from './types'

type Page = 'projects' | 'research' | 'actions' | 'process'
type DetailTab = 'observations' | 'actions' | 'attempts'
type FormModal = 'mission' | 'thread' | 'action' | 'system' | null
interface ActionDraft { kind: 'http' | 'local_command'; method: string; url: string; headers: string; body: string; command: string; reason: string }
interface ThreadDraft { title: string; question: string; rationale: string; priority: number }

const loggedIn = ref(false)
const loginInput = ref('')
const loginError = ref('')
const loginBusy = ref(false)
const system = ref<SystemInfo | null>(null)
const missions = ref<Mission[]>([])
const threadsByMission = reactive<Record<string, Thread[]>>({})
const actionLists = reactive<Record<string, Action[]>>({})
const snapshots = reactive<Record<string, Snapshot>>({})
const eventsByScope = reactive<Record<string, ResearchEvent[]>>({})
const selectedMissionId = ref('')
const selectedThreads = reactive<Record<string, string>>({})
const page = ref<Page>('projects')
const detailTab = ref<DetailTab>('observations')
const modal = ref<FormModal>(null)
const busy = reactive(new Set<string>())
const error = ref('')
const notice = ref('')
const refreshError = ref('')
const refreshing = ref(false)
const loaded = ref(false)
const lastRefreshed = ref('')
const projectSearch = ref('')
const threadSearch = ref('')
const actionFilter = ref('pending')
const selectedActionId = ref('')
const actionFallback = ref<Action | null>(null)
const actionContextLoading = ref(false)
const missionDraft = reactive({ title: '', origin: '', goal: '', origins: '' })
const threadDrafts = reactive<Record<string, ThreadDraft>>({})
const observationDrafts = reactive<Record<string, string>>({})
const actionDrafts = reactive<Record<string, ActionDraft>>({})
const actionNotes = reactive<Record<string, { decision: string; resolution: string; checked: boolean }>>({})
let session = 0
let refreshSerial = 0
let pollTimer: number | undefined

const scope = computed(() => selectedMissionId.value || 'global')
const mission = computed(() => missions.value.find((item) => item.id === selectedMissionId.value))
const threadId = computed(() => selectedThreads[selectedMissionId.value] || '')
const snapshot = computed(() => snapshots[threadId.value])
const thread = computed(() => snapshot.value?.thread)
const threads = computed(() => threadsByMission[selectedMissionId.value] || [])
const filteredThreads = computed(() => threads.value.filter((item) => `${item.title} ${item.question}`.toLowerCase().includes(threadSearch.value.toLowerCase())))
const filteredMissions = computed(() => missions.value.filter((item) => `${item.title} ${item.goal}`.toLowerCase().includes(projectSearch.value.toLowerCase())))
const scopedActions = computed(() => actionLists[scope.value] || [])
const pendingGlobal = computed(() => (actionLists.global || []).some((item) => item.status === 'pending' || needsResolution(item)))
const filteredActions = computed(() => scopedActions.value.filter((item) => {
  if (actionFilter.value === 'pending') return item.status === 'pending'
  if (actionFilter.value === 'unknown') return needsResolution(item)
  if (actionFilter.value === 'active') return ['approved', 'running'].includes(item.status)
  return true
}))
const events = computed(() => [...(eventsByScope[scope.value] || [])].sort((a, b) => b.id - a.id))
const selectedAction = computed(() => {
  const candidates = [...(actionLists.global || []), ...(actionLists[scope.value] || []), ...(snapshot.value?.actions || [])]
  if (actionFallback.value) candidates.push(actionFallback.value)
  return candidates.filter((item) => item.id === selectedActionId.value).sort((a, b) => b.updated_at.localeCompare(a.updated_at))[0]
})
const currentActionNote = computed(() => {
  const id = selectedActionId.value
  return actionNotes[id] ??= { decision: '', resolution: '', checked: false }
})
const currentThreadDraft = computed(() => threadDrafts[selectedMissionId.value] ??= { title: '', question: '', rationale: '', priority: 0 })
const currentActionDraft = computed(() => actionDrafts[threadId.value] ??= newActionDraft())
const currentObservation = computed({
  get: () => observationDrafts[threadId.value] || '',
  set: (value: string) => { observationDrafts[threadId.value] = value },
})
const unsettledActions = computed(() => (snapshot.value?.actions || []).filter(isUnsettled))
const hasUnknown = computed(() => unsettledActions.value.some(needsResolution))
const runDisabledReason = computed(() => {
  if (!system.value?.model_configured) return '尚未配置模型，自主研究不可用；仍可人工记录观察、申请与审批动作。'
  if (thread.value?.status === 'concluded') return '该方向已结束，不会隐式重开。请创建新的研究方向。'
  if (hasUnknown.value) return '先人工核对效果未知的动作，再安排新工作。'
  if (thread.value?.status === 'running') return '已有执行在途，不能再派发一轮。'
  if (thread.value?.auto_run && ['ready', 'waiting_approval'].includes(thread.value.status)) return '自主研究已启用，将按服务端状态继续；审批等待期间不会持续调用模型。'
  return ''
})
const proposalDisabledReason = computed(() => {
  if (thread.value?.status === 'running') return '研究正在执行，请等待本轮完成后再人工申请动作。'
  if (thread.value?.status === 'concluded') return '该方向已结束，请创建新方向。'
  if (unsettledActions.value.length) return '该方向已有未决动作，请先审批、等待回执或核对未知效果。'
  return ''
})

function newActionDraft(): ActionDraft { return { kind: 'http', method: 'GET', url: '', headers: '', body: '', command: '', reason: '' } }
function showForm(value: FormModal) { error.value = ''; modal.value = value }
function clearRecord(record: object) { for (const key of Object.keys(record)) delete (record as Record<string, unknown>)[key] }
function logout(message = '') {
  session++
  refreshSerial++
  setToken('')
  loggedIn.value = false
  loginInput.value = ''
  loginError.value = message
  system.value = null
  missions.value = []
  for (const record of [threadsByMission, actionLists, snapshots, eventsByScope, selectedThreads, threadDrafts, observationDrafts, actionDrafts, actionNotes]) clearRecord(record)
  Object.assign(missionDraft, { title: '', origin: '', goal: '', origins: '' })
  selectedMissionId.value = ''
  selectedActionId.value = ''
  actionFallback.value = null
  modal.value = null
  page.value = 'projects'
  error.value = ''; notice.value = ''; refreshError.value = ''; lastRefreshed.value = ''
  busy.clear()
  refreshing.value = false
  loaded.value = false
}
function messageOf(reason: unknown) { return reason instanceof Error ? reason.message : '操作失败，请核对服务状态。' }
function handleAuthError(reason: unknown) {
  if (reason instanceof ApiError && reason.status === 401) {
    logout('访问令牌无效或已失效，请重新连接。')
    return true
  }
  return false
}
async function login() {
  if (loginBusy.value || !loginInput.value.trim()) return
  loginBusy.value = true
  loginError.value = ''
  setToken(loginInput.value.trim())
  loginInput.value = ''
  const currentSession = ++session
  try {
    const info = await api<SystemInfo>('/system')
    if (currentSession !== session) return
    system.value = info
    loggedIn.value = true
    await refresh()
  } catch (reason) {
    if (currentSession !== session) return
    setToken('')
    loginError.value = messageOf(reason)
  } finally { loginBusy.value = false }
}
async function refresh() {
  if (!loggedIn.value) return
  const currentSession = session
  const serial = ++refreshSerial
  const missionId = selectedMissionId.value
  const selectedThread = threadId.value
  const key = missionId || 'global'
  refreshing.value = true
  try {
    const [info, projectList, globalActions, projectThreads, projectActions, eventList, threadSnapshot] = await Promise.all([
      api<SystemInfo>('/system'),
      api<Mission[]>('/missions'),
      api<Action[]>('/actions'),
      missionId ? api<Thread[]>(`/missions/${idPath(missionId)}/threads`) : Promise.resolve(null),
      missionId ? api<Action[]>(`/actions?mission_id=${idPath(missionId)}`) : Promise.resolve(null),
      api<ResearchEvent[]>(`/events?after=0${missionId ? `&mission_id=${idPath(missionId)}` : ''}`),
      selectedThread ? api<Snapshot>(`/threads/${idPath(selectedThread)}`) : Promise.resolve(null),
    ])
    if (currentSession !== session || serial !== refreshSerial) return
    system.value = info
    missions.value = projectList
    actionLists.global = globalActions
    if (projectThreads) threadsByMission[missionId] = projectThreads
    if (projectActions) actionLists[missionId] = projectActions
    if (threadSnapshot) snapshots[selectedThread] = threadSnapshot
    // 增量合并最近批次，保留本会话已读事件；不声称这是一份完整审计导出。
    const merged = new Map((eventsByScope[key] || []).map((item) => [item.id, item]))
    for (const event of eventList) merged.set(event.id, event)
    eventsByScope[key] = [...merged.values()].sort((a, b) => a.id - b.id).slice(-1000)
    refreshError.value = ''
    lastRefreshed.value = new Date().toISOString()
    loaded.value = true
  } catch (reason) {
    if (currentSession !== session || serial !== refreshSerial) return
    if (!handleAuthError(reason)) refreshError.value = messageOf(reason)
  } finally {
    if (serial === refreshSerial) refreshing.value = false
  }
}
async function task<T>(key: string, operation: () => Promise<T>, commit: (result: T) => void, success: string) {
  if (busy.has(key)) return
  const currentSession = session
  busy.add(key); error.value = ''; notice.value = ''
  try {
    const result = await operation()
    if (currentSession !== session) return
    commit(result)
    notice.value = success
    await refresh()
  } catch (reason) {
    if (currentSession === session && !handleAuthError(reason)) error.value = messageOf(reason)
  } finally { if (currentSession === session) busy.delete(key) }
}
async function openMission(item: Mission) {
  selectedMissionId.value = item.id
  page.value = 'research'
  threadSearch.value = ''
  detailTab.value = 'observations'
  error.value = ''; notice.value = ''
  await refresh()
  if (selectedMissionId.value === item.id && !selectedThreads[item.id] && threadsByMission[item.id]?.[0]) {
    selectedThreads[item.id] = threadsByMission[item.id][0].id
    await refresh()
  }
}
function goProjects() { selectedMissionId.value = ''; page.value = 'projects'; error.value = ''; void refresh() }
function goGlobal(target: 'actions' | 'process') {
  selectedMissionId.value = ''; page.value = target
  actionFilter.value = 'pending'; error.value = ''; void refresh()
}
function selectThread(item: Thread) {
  selectedThreads[selectedMissionId.value] = item.id
  detailTab.value = 'observations'; error.value = ''; void refresh()
}
async function showAction(action: Action) {
  error.value = ''
  actionFallback.value = action
  selectedActionId.value = action.id
  const currentSession = session
  actionContextLoading.value = true
  try {
    const data = await api<Snapshot>(`/threads/${idPath(action.thread_id)}`)
    if (currentSession !== session || selectedActionId.value !== action.id) return
    snapshots[action.thread_id] = data
  } catch (reason) {
    if (currentSession === session && selectedActionId.value === action.id && !handleAuthError(reason)) error.value = `审批上下文读取失败：${messageOf(reason)}`
  } finally {
    if (currentSession === session && selectedActionId.value === action.id) actionContextLoading.value = false
  }
}
function patchAction(action: Action) {
  actionFallback.value = action
  for (const key of Object.keys(actionLists)) actionLists[key] = actionLists[key].map((item) => item.id === action.id ? action : item)
}
async function goActionThread(id: string) {
  await task('open-thread', () => api<Snapshot>(`/threads/${idPath(id)}`), (data) => {
    snapshots[id] = data
    if (!missions.value.some((item) => item.id === data.mission.id)) missions.value.push(data.mission)
    selectedMissionId.value = data.mission.id
    selectedThreads[data.mission.id] = id
    page.value = 'research'; detailTab.value = 'actions'; selectedActionId.value = ''
  }, '')
}
function normalizedOrigins(value: string) {
  const lines = value.split(/\r?\n/).map((item) => item.trim()).filter(Boolean)
  if (!lines.length) throw new Error('请至少填写一个明确授权的 HTTP 或 HTTPS origin。')
  return [...new Set(lines.map((line) => {
    let url: URL
    try { url = new URL(line) } catch { throw new Error(`无效的 origin：${line}`) }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.pathname !== '/' || url.search || url.hash || url.hostname.includes('*')) {
      throw new Error(`origin 必须只有协议、主机与端口，不含路径、通配符、查询或凭据：${line}`)
    }
    return url.origin
  }))]
}
function createMission() {
  let origins: string[]
  try { origins = normalizedOrigins(missionDraft.origins) } catch (reason) { error.value = messageOf(reason); return }
  const payload = { title: missionDraft.title.trim(), origin: missionDraft.origin.trim(), goal: missionDraft.goal.trim(), allowed_origins: origins }
  if (!payload.title || !payload.goal) { error.value = '请填写项目名称与研究目标。'; return }
  void task('mission', () => api<Mission>('/missions', payload), (result) => {
    missions.value = [result, ...missions.value]
    selectedMissionId.value = result.id; page.value = 'research'; modal.value = null
    Object.assign(missionDraft, { title: '', origin: '', goal: '', origins: '' })
  }, '项目已创建。尚未访问目标，也未启用自动研究。')
}
function createThread() {
  const id = selectedMissionId.value
  const draft = currentThreadDraft.value
  const payload = { title: draft.title.trim(), question: draft.question.trim(), rationale: draft.rationale.trim(), priority: Number(draft.priority) }
  if (!payload.title || !payload.question || !Number.isInteger(payload.priority)) { error.value = '请填写方向标题、研究问题与整数优先级。'; return }
  void task('thread', () => api<Thread>(`/missions/${idPath(id)}/threads`, payload), (result) => {
    threadsByMission[id] = [result, ...(threadsByMission[id] || [])]
    selectedThreads[id] = result.id; modal.value = null; detailTab.value = 'observations'
    delete threadDrafts[id]
  }, '研究方向已保存，自动研究尚未启用。')
}
function addObservation() {
  const id = threadId.value
  const content = currentObservation.value.trim()
  if (!content) return
  void task('observation', () => api(`/threads/${idPath(id)}/observations`, { content }), () => {
    if (observationDrafts[id]?.trim() === content) observationDrafts[id] = ''
  }, '人工观察已追加保存，不会被标记为已验证事实。')
}
function proposeAction() {
  const id = threadId.value
  const draft = currentActionDraft.value
  let payload: ActionInput
  try {
    if (!draft.reason.trim()) throw new Error('请说明动作的验证目的。')
    if (draft.kind === 'http') {
      let headers: Record<string, string> = {}
      if (draft.headers.trim()) {
        const parsed: unknown = JSON.parse(draft.headers)
        if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object' || Object.values(parsed).some((value) => typeof value !== 'string')) throw new Error('请求头须为 JSON 对象，且每个值都是字符串。')
        headers = parsed as Record<string, string>
      }
      const url = new URL(draft.url.trim())
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.hash) throw new Error('请求 URL 只能使用 HTTP/HTTPS，不可含用户凭据或片段。')
      if (!mission.value?.allowed_origins.includes(url.origin)) throw new Error(`该 URL 的 origin 不在项目授权列表中：${url.origin}`)
      payload = { kind: 'http', method: draft.method, url: draft.url.trim(), headers, body: draft.body, reason: draft.reason.trim() }
    } else {
      if (!draft.command.trim()) throw new Error('请输入工具命令。')
      payload = { kind: 'local_command', command: draft.command, reason: draft.reason.trim() }
    }
  } catch (reason) { error.value = reason instanceof SyntaxError ? '请求头不是有效 JSON，请修正后再申请。' : messageOf(reason); return }
  void task('action', () => api<Action>(`/threads/${idPath(id)}/actions`, payload), (result) => {
    actionLists.global = [result, ...(actionLists.global || [])]
    modal.value = null; delete actionDrafts[id]; detailTab.value = 'actions'; showAction(result)
  }, '动作已保存，放行依据与执行结果见详情。')
}
function control(command: 'run' | 'resume' | 'pause') {
  const id = threadId.value
  const messages = {
    run: '已请求启用自动研究；等待审批不会持续调用模型。',
    resume: '已请求恢复可接续状态；未启用自动模型，也未重放旧动作。',
    pause: '已请求停止后续派发。在途动作或执行不会因此被撤回。',
  }
  void task('control', () => api<Thread>(`/threads/${idPath(id)}/control`, { command }), (result) => {
    if (snapshots[id]) snapshots[id].thread = result
    if (threadsByMission[result.mission_id]) threadsByMission[result.mission_id] = threadsByMission[result.mission_id].map((item) => item.id === id ? result : item)
  }, messages[command])
}
function decideAction(decision: 'approved' | 'rejected', reason: string) {
  const id = selectedActionId.value
  if (decision === 'rejected' && !reason.trim()) { error.value = '拒绝动作时请填写原因。'; return }
  void task('decision', () => api<Action>(`/actions/${idPath(id)}/decision`, { decision, reason }), patchAction,
    decision === 'approved' ? '已批准原请求，等待后台执行；若所属方向暂停，需要先恢复。' : '已拒绝该请求。')
}
function resolveAction(note: string) {
  const id = selectedActionId.value
  if (!note.trim()) { error.value = '请填写人工核对记录。'; return }
  void task('resolve', () => api<Action>(`/actions/${idPath(id)}/resolve`, { note }), patchAction, '核对记录已保存，原动作保留效果未知历史；未自动重放或启用研究。')
}
function onVisible() { if (document.visibilityState === 'visible' && loggedIn.value && !refreshing.value && !busy.size) void refresh() }
onMounted(() => {
  pollTimer = window.setInterval(onVisible, 8000)
  document.addEventListener('visibilitychange', onVisible)
})
onBeforeUnmount(() => { window.clearInterval(pollTimer); document.removeEventListener('visibilitychange', onVisible); setToken('') })
</script>

<template>
  <div v-if="!loggedIn" class="login-shell">
    <div class="login-story">
      <a class="brand" href="#main-login"><span class="brand-mark"><Compass :size="23" /></span><span>wscyber<span class="brand-dot">.</span></span></a>
      <div class="login-intro"><span class="eyebrow">证据驱动的安全研究</span><h1>从问题出发，<br />让证据带来下一步。</h1><p>保留研究的来路，审慎决定每一次行动。<br />围绕明确授权的目标，持续观察、核对与接续。</p><div class="login-principles"><span><Compass :size="17" />问题长久保留</span><span><ShieldCheck :size="17" />危险请求待审</span><span><FileText :size="17" />证据如实呈现</span></div></div>
      <p class="caption">内部授权研究 · 自托管工作空间</p>
    </div>
    <main id="main-login" class="login-main">
      <form class="login-card" @submit.prevent="login">
        <div class="login-symbol"><KeyRound :size="25" /></div>
        <span class="eyebrow">连接你的工作空间</span><h2>欢迎使用 wscyber</h2><p class="muted">使用服务端配置的访问令牌登录。</p>
        <label for="access-token">访问令牌</label><input id="access-token" v-model="loginInput" type="password" autocomplete="off" autocapitalize="none" spellcheck="false" required :disabled="loginBusy" placeholder="输入访问令牌" aria-describedby="token-note" />
        <p id="token-note" class="caption"><LockKeyhole :size="14" />令牌只保留在浏览器内存，刷新或退出即清除。不写入本地存储。</p>
        <p v-if="loginError" class="message danger" role="alert">{{ loginError }}</p>
        <button class="button primary login-submit" :disabled="loginBusy || !loginInput.trim()"><LoaderCircle v-if="loginBusy" class="spinning" :size="17" />{{ loginBusy ? '正在连接…' : '进入研究空间' }}<ArrowRight v-if="!loginBusy" :size="17" /></button>
        <p class="login-footnote">只连接同源控制面。登录不会向研究目标发送请求。</p>
      </form>
    </main>
  </div>

  <div v-else class="app-shell">
    <a href="#workspace" class="skip-link">跳到主要内容</a>
    <aside class="sidebar" aria-label="工作空间导航">
      <button class="brand" type="button" aria-label="wscyber 项目首页" @click="goProjects"><span class="brand-mark"><Compass :size="22" /></span><span>wscyber<span class="brand-dot">.</span></span></button>
      <div class="workspace-label"><span class="workspace-avatar">w</span><div><strong>研究工作空间</strong><span>明确授权 · 审慎执行</span></div><LockKeyhole :size="14" /></div>
      <p class="nav-heading">工作空间</p>
      <nav class="main-nav">
        <button type="button" :class="{ active: page === 'projects' }" @click="goProjects"><FolderOpen :size="18" />研究项目</button>
        <button type="button" :class="{ active: page === 'actions' && !selectedMissionId }" @click="goGlobal('actions')"><ListChecks :size="18" />全局动作与审批<span v-if="pendingGlobal" class="attention-dot" aria-label="有动作待处理" /></button>
        <button type="button" :class="{ active: page === 'process' && !selectedMissionId }" @click="goGlobal('process')"><History :size="18" />过程记录</button>
      </nav>
      <template v-if="mission"><p class="nav-heading">当前项目</p><div class="sidebar-project"><span class="project-marker" /><span>{{ mission.title }}</span></div><nav class="main-nav project-nav"><button type="button" :class="{ active: page === 'research' }" @click="page = 'research'"><Compass :size="17" />研究方向</button><button type="button" :class="{ active: page === 'actions' }" @click="page = 'actions'; actionFilter = 'pending'"><ListChecks :size="17" />动作与审批</button><button type="button" :class="{ active: page === 'process' }" @click="page = 'process'"><History :size="17" />项目过程</button></nav></template>
      <div class="sidebar-bottom"><div class="model-note" :class="{ unavailable: !system?.model_configured }"><span class="status-dot" /><div><strong>{{ system?.model_configured ? '自主研究可用' : '模型尚未配置' }}</strong><p>{{ system?.model_configured ? '仅在显式运行后启用' : '人工研究与审批仍可使用' }}</p></div></div><button class="sidebar-utility" type="button" @click="showForm('system')"><Settings2 :size="16" />环境与能力</button><button class="sidebar-utility" type="button" @click="logout()"><LogOut :size="16" />退出并清除令牌</button></div>
    </aside>

    <div class="workspace-shell">
      <header class="topbar"><div class="breadcrumbs"><span>wscyber</span><ChevronRight :size="14" /><span>{{ mission?.title || '工作空间' }}</span><template v-if="page !== 'projects'"><ChevronRight :size="14" /><strong>{{ page === 'research' ? '研究方向' : page === 'actions' ? '动作与审批' : '过程记录' }}</strong></template></div><div class="refresh-controls"><span class="caption refresh-time">{{ lastRefreshed ? `更新于 ${dateTime(lastRefreshed)}` : '正在读取' }}</span><button class="icon-button" type="button" :disabled="refreshing" aria-label="刷新服务端状态，不会触发研究" @click="refresh"><RefreshCw :size="17" :class="{ spinning: refreshing }" /></button></div></header>
      <main id="workspace" class="workspace" tabindex="-1">
        <div v-if="refreshError" class="message warning" role="alert"><div><strong>状态刷新失败，当前内容可能不是最新。</strong><p>{{ refreshError }} 输入与选中项已保留。</p></div><button class="text-button" type="button" :disabled="refreshing" @click="refresh">重试刷新</button></div>
        <div v-if="error" class="message danger" role="alert"><span>{{ error }}</span><button class="icon-button" type="button" aria-label="关闭错误提示" @click="error = ''"><X :size="16" /></button></div>
        <div v-if="notice" class="message success" role="status"><Check :size="17" /><span>{{ notice }}</span><button class="icon-button" type="button" aria-label="关闭操作提示" @click="notice = ''"><X :size="16" /></button></div>

        <template v-if="page === 'projects'">
          <div class="page-heading"><div><span class="eyebrow">研究，从这里接续</span><h1>研究项目</h1><p>定义背景与目标，在明确授权的范围内追问。</p></div><button class="button primary" type="button" @click="showForm('mission')"><Plus :size="18" />创建项目</button></div>
          <div class="projects-toolbar"><label class="search-field"><Search :size="17" /><span class="sr-only">搜索项目名称与目标</span><input v-model="projectSearch" type="search" placeholder="搜索项目名称与目标" /></label><span class="caption">项目保存研究上下文，不因一次执行失败而消失。</span></div>
          <div v-if="!loaded" class="empty-state"><LoaderCircle v-if="refreshing" class="spinning" :size="28" /><h2>{{ refreshError ? '还未取得项目数据' : '正在读取工作空间' }}</h2><p>只展示服务端真实记录。</p></div>
          <div v-else-if="!missions.length" class="empty-state project-empty"><span class="empty-symbol"><FolderOpen :size="31" /></span><span class="eyebrow">第一个项目，从一个问题开始</span><h2>你的研究空间已就绪</h2><p>写下背景、想回答的问题和明确授权的 origin。<br />创建项目不会访问目标，也不会自动启动模型。</p><button class="button primary" type="button" @click="showForm('mission')"><Plus :size="17" />创建第一个研究项目</button><div class="empty-footnote"><ShieldCheck :size="15" />没有预置样本，也没有虚构发现。</div></div>
          <div v-else-if="!filteredMissions.length" class="empty-state"><Search :size="28" /><h2>没有匹配的项目</h2><button class="text-button" type="button" @click="projectSearch = ''">清除搜索</button></div>
          <div v-else class="project-grid"><button v-for="item in filteredMissions" :key="item.id" type="button" class="project-card" @click="openMission(item)"><div class="project-card-top"><span class="project-icon"><FolderOpen :size="23" /></span><ArrowUpRight :size="19" /></div><h2>{{ item.title }}</h2><p class="project-goal">{{ item.goal }}</p><div class="origin-chips"><code v-for="origin in item.allowed_origins.slice(0, 3)" :key="origin">{{ origin }}</code><span v-if="item.allowed_origins.length > 3" class="caption">其余 origin 在项目详情查看</span></div><div class="project-card-footer"><span>创建于 {{ dateTime(item.created_at) }}</span><span>进入研究<ArrowRight :size="14" /></span></div></button></div>
          <p class="page-footnote">观察不是结论。没有发现，不意味着目标已被证明安全。</p>
        </template>

        <template v-else>
          <div class="page-heading contextual"><div><button v-if="mission" class="text-button back-link" type="button" @click="goProjects"><ArrowLeft :size="14" />全部项目</button><span v-else class="eyebrow">工作空间 · 全局视角</span><h1>{{ mission?.title || (page === 'actions' ? '动作与审批' : '过程记录') }}</h1><p v-if="mission" class="prose page-goal">{{ mission.goal }}</p><p v-else>{{ page === 'actions' ? '核对每一次影响；批准只绑定这一个已保存的请求。' : '沿着真实记录回看研究，不把执行状态当作发现。' }}</p></div></div>
          <details v-if="mission" class="scope-details"><summary><ShieldCheck :size="16" />项目背景与授权范围<span class="caption">仅明确列出的 origin</span></summary><div class="scope-content"><div><h3>原始背景</h3><p class="prose">{{ mission.origin || '未提供背景材料。' }}</p></div><div><h3>明确授权的 origin</h3><ul class="origin-list"><li v-for="origin in mission.allowed_origins" :key="origin"><Globe2 :size="14" /><code>{{ origin }}</code></li></ul><p class="caption">背景中的地址不会自动获得授权。不自动跟随重定向扩大范围。</p></div></div></details>
          <nav v-if="mission" class="section-nav" aria-label="项目内容"><button type="button" :class="{ active: page === 'research' }" :aria-current="page === 'research' ? 'page' : undefined" @click="page = 'research'"><Compass :size="17" />研究方向</button><button type="button" :class="{ active: page === 'actions' }" :aria-current="page === 'actions' ? 'page' : undefined" @click="page = 'actions'; actionFilter = 'pending'"><ListChecks :size="17" />动作与审批</button><button type="button" :class="{ active: page === 'process' }" :aria-current="page === 'process' ? 'page' : undefined" @click="page = 'process'"><History :size="17" />过程信息</button></nav>

          <div v-if="page === 'research'" class="research-layout">
            <section class="thread-rail" aria-label="研究方向列表"><div class="section-heading"><h2>研究方向</h2><button class="icon-button soft" type="button" aria-label="新建研究方向" @click="showForm('thread')"><Plus :size="18" /></button></div><label class="search-field compact-search"><Search :size="15" /><span class="sr-only">搜索研究方向</span><input v-model="threadSearch" type="search" placeholder="查找研究问题" /></label><div v-if="!threadsByMission[selectedMissionId]" class="empty-inline">{{ refreshError ? '方向尚未加载，请刷新。' : '正在读取研究方向…' }}</div><div v-else-if="!threads.length" class="thread-rail-empty"><p>还没有研究方向。</p><span class="caption">将目标转化成一个可持续追问的问题。</span><button class="text-button" type="button" @click="showForm('thread')"><Plus :size="14" />创建方向</button></div><p v-else-if="!filteredThreads.length" class="empty-inline">没有匹配的研究方向。</p><div class="thread-list"><button v-for="item in filteredThreads" :key="item.id" type="button" class="thread-item" :class="{ selected: item.id === threadId }" :aria-pressed="item.id === threadId" @click="selectThread(item)"><span class="thread-item-top"><span class="badge" :data-tone="statusTone(item.status)">{{ statusText(item.status) }}</span><span class="caption">优先级 {{ item.priority }}</span></span><strong>{{ item.title }}</strong><span class="thread-question-preview">{{ item.question }}</span><span class="caption">{{ item.auto_run ? '自动研究已启用' : '自动研究未启用' }}</span></button></div></section>
            <section class="thread-workspace" aria-label="研究方向详情">
              <div v-if="!threadId" class="empty-state"><span class="empty-symbol"><Compass :size="29" /></span><h2>{{ threads.length ? '选择一个问题，继续研究' : '先明确你想回答的问题' }}</h2><p>每个方向保留问题、依据与观察。<br />一次中断不会抹去未决问题。</p><button v-if="!threads.length" class="button primary" type="button" @click="showForm('thread')"><Plus :size="17" />新建研究方向</button></div>
              <div v-else-if="!snapshot || !thread" class="empty-state"><LoaderCircle v-if="refreshing" class="spinning" :size="24" /><h2>{{ refreshError ? '详情暂时不可用' : '正在读取研究详情' }}</h2><p>不会用其他方向的数据代替。</p></div>
              <template v-else>
                <div class="thread-heading"><div class="thread-title-line"><span class="badge" :data-tone="statusTone(thread.status)">{{ statusText(thread.status) }}</span><span class="caption">{{ thread.auto_run ? '自动研究已启用' : '自动研究未启用' }}</span></div><h2>{{ thread.title }}</h2><p class="caption">更新于 {{ dateTime(thread.updated_at) }} · 优先级 {{ thread.priority }}</p></div>
                <div class="control-bar"><button class="button primary small-button" type="button" :disabled="busy.has('control') || !!runDisabledReason" :title="runDisabledReason || '启用自动模型研究'" aria-describedby="control-help model-help" @click="control('run')"><Play :size="15" />运行研究</button><button class="button secondary small-button" type="button" :disabled="busy.has('control') || hasUnknown || !['paused', 'blocked', 'waiting_approval'].includes(thread.status)" aria-describedby="control-help" @click="control('resume')"><RefreshCw :size="15" />恢复可接续</button><button class="button secondary small-button" type="button" :disabled="busy.has('control') || ['paused', 'concluded'].includes(thread.status)" aria-describedby="control-help" @click="control('pause')"><Pause :size="15" />暂停派发</button></div>
                <p id="model-help" v-if="runDisabledReason" class="capability-note"><CircleHelp :size="15" />{{ runDisabledReason }}</p>
                <details id="control-help" class="control-help"><summary>运行、恢复与暂停有什么不同？</summary><ul><li><strong>运行研究</strong>：显式启用模型驱动的自主研究，需已配置模型。</li><li><strong>恢复可接续</strong>：恢复 ready，不启用自动模型，不重放未知动作；已批准的新动作可由后台派发。</li><li><strong>暂停派发</strong>：只阻止后续研究与动作派发，不撤回在途执行。</li></ul></details>
                <div class="research-question"><span class="eyebrow">当前问题</span><p>{{ thread.question }}</p><div v-if="thread.rationale" class="research-rationale"><h3>研究依据</h3><p class="prose">{{ thread.rationale }}</p></div></div>
                <div class="next-step"><ArrowRight :size="18" /><div><h3>下一步与阻塞</h3><p class="prose">{{ thread.next_step || '尚未形成下一步安排。可先添加观察，或人工申请一个明确目的的动作。' }}</p></div></div>
                <div v-if="unsettledActions.length" class="unsettled-notice"><ShieldCheck :size="17" /><div><strong>{{ hasUnknown ? '有动作效果未知，需人工核对' : '有动作尚未完结' }}</strong><p class="caption">{{ hasUnknown ? '不得因超时推断“未执行”，也不会自动重放。' : '等待审批是正常状态；未决动作完成前不会插入新的动作。' }}</p><button v-for="item in unsettledActions" :key="item.id" class="text-button" type="button" @click="showAction(item)">{{ statusText(item.status) }} · 查看请求<ArrowUpRight :size="14" /></button></div></div>
                <nav class="detail-tabs" aria-label="方向记录"><button type="button" :class="{ active: detailTab === 'observations' }" :aria-pressed="detailTab === 'observations'" @click="detailTab = 'observations'"><StickyNote :size="16" />观察记录</button><button type="button" :class="{ active: detailTab === 'actions' }" :aria-pressed="detailTab === 'actions'" @click="detailTab = 'actions'"><ListChecks :size="16" />动作</button><button type="button" :class="{ active: detailTab === 'attempts' }" :aria-pressed="detailTab === 'attempts'" @click="detailTab = 'attempts'"><History :size="16" />执行记录</button></nav>
                <section v-if="detailTab === 'observations'" class="detail-content"><form class="observation-form" @submit.prevent="addObservation"><label for="observation-note">添加人工观察</label><textarea id="observation-note" v-model="currentObservation" rows="3" :disabled="busy.has('observation')" placeholder="记录看到的现象、来源与仍不确定的部分…" required /><div class="section-heading compact"><span class="caption">追加保存为人工记录，不标记为已验证事实。</span><button class="button secondary small-button" :disabled="busy.has('observation') || !currentObservation.trim()"><Plus :size="15" />{{ busy.has('observation') ? '保存中…' : '保存观察' }}</button></div></form><p v-if="!snapshot.observations.length" class="empty-inline padded">还没有观察。这里将保留真实的人工记录与执行材料，不预填发现。</p><article v-for="item in [...snapshot.observations].reverse()" :key="item.id" class="observation-item"><div class="record-heading"><span class="record-symbol"><StickyNote :size="15" /></span><strong>{{ observationKind(item.kind) }}</strong><time :datetime="item.created_at">{{ dateTime(item.created_at) }}</time></div><p class="prose">{{ item.content }}</p><button v-if="item.action_id && snapshot.actions.find((action) => action.id === item.action_id)" class="text-button" type="button" @click="showAction(snapshot.actions.find((action) => action.id === item.action_id)!)">查看关联动作与原件<ArrowUpRight :size="14" /></button><span v-else-if="item.action_id" class="caption mono">关联动作 {{ item.action_id }}</span><p v-if="item.attempt_id" class="caption mono">来源执行 {{ item.attempt_id }}</p></article></section>
                <section v-else-if="detailTab === 'actions'" class="detail-content"><div class="section-heading"><div><h3>已保存的动作</h3><p class="caption">默认放行；危险 HTTP 请求待审。</p></div><button class="button secondary small-button" type="button" :disabled="!!proposalDisabledReason" :title="proposalDisabledReason" @click="showForm('action')"><Plus :size="15" />申请动作</button></div><p v-if="proposalDisabledReason" class="caption">{{ proposalDisabledReason }}</p><div v-if="!snapshot.actions.length" class="empty-inline padded">尚未申请动作。可提出受控 HTTP 请求或工具命令。</div><button v-for="item in [...snapshot.actions].reverse()" :key="item.id" type="button" class="action-row" @click="showAction(item)"><span class="action-icon"><Globe2 v-if="item.input.kind === 'http'" :size="18" /><SquareTerminal v-else :size="18" /></span><span class="action-row-main"><strong class="mono">{{ actionTitle(item) }}</strong><span>{{ item.input.reason }}</span><small>{{ dateTime(item.created_at) }}</small></span><span class="badge" :data-tone="statusTone(item.status)">{{ statusText(item.status) }}{{ item.resolved_at ? ' · 已核对' : '' }}</span><ChevronRight :size="16" /></button></section>
                <section v-else class="detail-content"><p class="caption">一次执行是有限计算尝试，不等于一个研究结论。失败或中断不会删除当前问题。</p><div v-if="!snapshot.attempts.length" class="empty-inline padded">还没有模型执行记录。手工动作的回执请在“动作”中查看。</div><article v-for="item in [...snapshot.attempts].reverse()" :key="item.id" class="attempt-item"><div class="record-heading"><span class="badge" :data-tone="statusTone(item.status)">{{ statusText(item.status) }}</span><time :datetime="item.started_at">{{ dateTime(item.started_at) }}</time></div><p class="prose">{{ item.summary || '本次执行尚无已保存摘要。' }}</p><p v-if="item.error" class="message danger">{{ item.error }}</p><dl class="attempt-meta"><dt>结束时间</dt><dd>{{ dateTime(item.ended_at) }}</dd><dt>执行 ID</dt><dd class="mono">{{ item.id }}</dd></dl></article></section>
              </template>
            </section>
          </div>

          <section v-else-if="page === 'actions'" class="actions-workspace"><div class="section-heading actions-heading"><div><h2>{{ mission ? '项目动作与审批' : '全局动作队列' }}</h2><p class="caption">{{ mission ? '仅此项目内的动作' : '所有项目的动作' }} · 点击请求查看参数、回执与审批记录</p></div><label class="filter-label">显示<select v-model="actionFilter"><option value="pending">待审批</option><option value="unknown">效果未知 · 待核对</option><option value="active">已放行 / 执行中</option><option value="all">全部动作与历史</option></select></label></div><div v-if="!actionLists[scope]" class="empty-state"><h3>正在读取动作队列</h3></div><div v-else-if="!filteredActions.length" class="empty-state"><span class="empty-symbol"><ListChecks :size="29" /></span><h2>{{ actionFilter === 'pending' ? '当前没有待审批的动作' : actionFilter === 'unknown' ? '当前没有待核对的未知效果' : '此视图中还没有动作' }}</h2><p>{{ actionFilter === 'pending' ? '这里只显示命中风险规则或历史遗留的待审请求。' : '只展示真实保存的请求与结果。' }}</p><button v-if="actionFilter !== 'all'" class="text-button" type="button" @click="actionFilter = 'all'">查看全部动作与历史<ArrowRight :size="15" /></button></div><div v-else class="action-table"><button v-for="item in filteredActions" :key="item.id" type="button" class="action-row" @click="showAction(item)"><span class="action-icon"><Globe2 v-if="item.input.kind === 'http'" :size="20" /><SquareTerminal v-else :size="20" /></span><span class="action-row-main"><strong class="mono">{{ actionTitle(item) }}</strong><span>{{ item.input.reason }}</span><small>{{ item.input.kind === 'http' ? '受控 HTTP' : '工具命令' }} · {{ dateTime(item.created_at) }} · 方向 {{ item.thread_id }}</small></span><span class="badge" :data-tone="statusTone(item.status)">{{ statusText(item.status) }}{{ item.resolved_at ? ' · 已核对' : '' }}</span><ChevronRight :size="17" /></button></div><p class="page-footnote">批准不意味着发现成立；未知效果必须核对，不能自动重放。</p></section>

          <section v-else-if="page === 'process'" class="events-workspace"><div class="section-heading"><div><h2>{{ mission ? '项目过程记录' : '工作空间过程记录' }}</h2><p class="caption">最近 API 批次与本会话已读事件（最多保留 1,000 条），不是完整审计导出。刷新不会调用模型。</p></div><History :size="22" class="muted" /></div><div v-if="!eventsByScope[scope]" class="empty-state"><h3>正在读取过程记录</h3></div><div v-else-if="!events.length" class="empty-state"><span class="empty-symbol"><History :size="29" /></span><h2>还没有过程记录</h2><p>真实的状态变化会在这里留痕。</p></div><ol v-else class="event-list"><li v-for="item in events" :key="item.id"><span class="event-marker" /><div class="event-content"><div class="record-heading"><strong>{{ item.kind }}</strong><time :datetime="item.created_at">{{ dateTime(item.created_at) }}</time></div><p class="prose">{{ item.message }}</p><div class="event-meta"><span class="caption mono">事件 #{{ item.id }}</span><span v-if="!mission" class="caption">{{ missions.find((project) => project.id === item.mission_id)?.title || item.mission_id }}</span><button v-if="item.thread_id" class="text-button" type="button" :disabled="busy.has('open-thread')" @click="goActionThread(item.thread_id)">查看研究方向<ArrowUpRight :size="14" /></button></div></div></li></ol></section>
        </template>
      </main>
    </div>
  </div>

  <Modal :open="modal === 'mission'" title="创建研究项目" @close="modal = null"><form class="stack-form" @submit.prevent="createMission"><p class="form-intro">自由表达背景与目标，再单独确认授权。创建项目不会自动请求目标或启用模型。</p><p v-if="error" class="message danger" role="alert">{{ error }}</p><fieldset :disabled="busy.has('mission')"><label for="mission-title">项目名称 <span class="required">必填</span></label><input id="mission-title" v-model="missionDraft.title" required placeholder="为这次研究起一个清晰的名称" /><label for="mission-origin">研究背景与已有材料</label><textarea id="mission-origin" v-model="missionDraft.origin" rows="4" placeholder="业务背景、身份与状态关系、已有资料，以及你担心的地方…" /><label for="mission-goal">研究目标 <span class="required">必填</span></label><textarea id="mission-goal" v-model="missionDraft.goal" rows="3" required placeholder="这次研究想回答什么问题？" /><label for="mission-origins">明确授权的 HTTP origins <span class="required">必填</span></label><textarea id="mission-origins" v-model="missionDraft.origins" class="mono" rows="3" required spellcheck="false" placeholder="每行一个 scheme://host[:port]" aria-describedby="origins-help" /><p id="origins-help" class="caption">仅 HTTP / HTTPS；不含路径、通配符、查询或用户凭据。将按标准 URL 规范化默认端口及大小写。背景中提到的地址不会自动成为授权范围。</p></fieldset><div class="form-actions"><button class="button secondary" type="button" @click="modal = null">暂不创建</button><button class="button primary" :disabled="busy.has('mission')">{{ busy.has('mission') ? '正在保存…' : '创建项目' }}<ArrowRight :size="16" /></button></div></form></Modal>

  <Modal :open="modal === 'thread'" title="新建研究方向" @close="modal = null"><form class="stack-form" @submit.prevent="createThread"><p class="form-intro">一个方向对应一个可接续的问题，不绑定一次执行。新方向默认不启用自动研究。</p><p v-if="error" class="message danger" role="alert">{{ error }}</p><fieldset :disabled="busy.has('thread')"><label for="thread-title">方向标题 <span class="required">必填</span></label><input id="thread-title" v-model="currentThreadDraft.title" required placeholder="简洁描述要研究的方向" /><label for="thread-question">当前研究问题 <span class="required">必填</span></label><textarea id="thread-question" v-model="currentThreadDraft.question" rows="4" required placeholder="一个具体、可以用证据推进的问题" /><label for="thread-rationale">依据与已知条件</label><textarea id="thread-rationale" v-model="currentThreadDraft.rationale" rows="3" placeholder="为什么值得研究？已有何种材料、假设或约束？" /><label for="thread-priority">优先级</label><input id="thread-priority" v-model.number="currentThreadDraft.priority" type="number" step="1" required /><p class="caption">使用整数，由服务端安排派发顺序；不会因创建方向立即调用模型。</p></fieldset><div class="form-actions"><button class="button secondary" type="button" @click="modal = null">稍后再建</button><button class="button primary" :disabled="busy.has('thread')">{{ busy.has('thread') ? '正在保存…' : '保存研究方向' }}</button></div></form></Modal>

  <Modal :open="modal === 'action'" title="申请一个动作" wide @close="modal = null"><form class="stack-form" @submit.prevent="proposeAction"><p class="form-intro">普通请求与工具命令提交后自动执行；命中危险信号的 HTTP 请求待审。</p><p v-if="error" class="message danger" role="alert">{{ error }}</p><fieldset :disabled="busy.has('action')"><label for="action-kind">动作类型</label><select id="action-kind" v-model="currentActionDraft.kind"><option value="http">受控 HTTP 请求</option><option value="local_command">工具命令</option></select><template v-if="currentActionDraft.kind === 'http'"><div class="http-input-line"><div><label for="action-method">方法</label><select id="action-method" v-model="currentActionDraft.method"><option v-for="method in ['GET', 'HEAD', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS']" :key="method">{{ method }}</option></select></div><div><label for="action-url">完整请求 URL <span class="required">必填</span></label><input id="action-url" v-model="currentActionDraft.url" type="url" class="mono" required placeholder="输入已授权 origin 下的 URL" /></div></div><div class="allowed-origins"><span class="caption">仅以下 origins 获得授权，服务端仍会核验范围：</span><code v-for="origin in mission?.allowed_origins" :key="origin">{{ origin }}</code></div><label for="action-headers">自定义请求头 <span class="muted">JSON 字符串对象，可留空</span></label><textarea id="action-headers" v-model="currentActionDraft.headers" class="mono" rows="3" spellcheck="false" placeholder="{}" /><label for="action-body">请求正文 <span class="muted">可留空，按文本提交</span></label><textarea id="action-body" v-model="currentActionDraft.body" class="mono" rows="4" spellcheck="false" /><p class="caption">不自动跟随重定向；不能通过请求正文或头字段扩大授权。</p></template><template v-else><label for="action-command">本地命令 <span class="required">必填</span></label><textarea id="action-command" v-model="currentActionDraft.command" class="mono" rows="6" required spellcheck="false" placeholder="输入希望在受限沙箱中执行的命令" /><p class="message neutral"><SquareTerminal :size="17" />命令容器默认联网，直连流量尚未拦截。需要风险检查的 HTTP 请使用 HTTP 动作。</p></template><label for="action-reason">验证目的与预期影响 <span class="required">必填</span></label><textarea id="action-reason" v-model="currentActionDraft.reason" rows="3" required placeholder="这个动作将回答什么问题？可能对目标状态产生什么影响？" /></fieldset><div class="form-actions"><button class="button secondary" type="button" @click="modal = null">保留草稿</button><button class="button primary" :disabled="busy.has('action') || !!proposalDisabledReason">{{ busy.has('action') ? '正在保存…' : '提交动作' }}</button></div><p v-if="proposalDisabledReason" class="message warning">{{ proposalDisabledReason }}</p></form></Modal>

  <Modal :open="modal === 'system'" title="环境与能力边界" @close="modal = null"><div v-if="system" class="system-details"><p class="form-intro">以下来自真实控制面配置，不代表执行能力已经完成端到端验证。</p><dl class="metadata"><dt>产品版本</dt><dd>{{ system.name }} · {{ system.version }}</dd><dt>模型</dt><dd>{{ system.model_configured ? system.model : '未配置，自主研究不可用' }}</dd><dt>本地沙箱镜像</dt><dd class="mono">{{ system.sandbox_image || '未返回配置' }}</dd><dt>沙箱运行时</dt><dd class="mono">{{ system.sandbox_runtime || '使用服务端默认配置' }}</dd><dt>网络模式</dt><dd class="mono">{{ system.network_mode }}</dd><dt>并发上限</dt><dd>{{ system.max_parallel }}（配置上限，不是运行统计）</dd></dl><div class="message neutral"><ShieldCheck :size="18" /><div>工具命令默认放行；HTTP 动作按明确危险信号分流。工具直连尚未接入流量拦截。</div></div><p class="caption">令牌只在当前页面内存中。页面关闭、刷新或退出后需要重新登录。</p></div></Modal>

  <ActionDetail v-if="selectedAction" :action="selectedAction" :context="snapshots[selectedAction.thread_id]" :context-loading="actionContextLoading" :draft="currentActionNote" :busy="busy.has('decision') || busy.has('resolve') || busy.has('open-thread')" :error="error" @close="selectedActionId = ''; error = ''" @reload="showAction(selectedAction)" @decision="decideAction" @resolve="resolveAction" @thread="goActionThread" />
</template>
