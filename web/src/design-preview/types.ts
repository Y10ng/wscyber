export type PreviewScenarioState =
  | 'running'
  | 'empty'
  | 'failed'
  | 'unknown'

export type GlobalNavSection = 'projects' | 'traffic' | 'library' | 'settings'

export type ProjectTabKey = 'research' | 'traffic' | 'findings' | 'reports'

export type TrafficCenterTabKey = 'target_traffic' | 'model_calls'

export type FindingDetailTabKey = 'body' | 'timeline' | 'retests'

export interface DemoIdentity {
  id: string
  label: string
  roleSummary: string
  credentialType: string
  maskedPreview: string
  status: '有效（演示）' | '已过期（演示）'
}

export interface DemoProject {
  id: string
  name: string
  targetAndKnownInfo: string
  desiredObjective: string
  allowedOrigins: string[]
  submissionStatus: '已接收' | '提交结果待核对'
  executionStatus: '运行中' | '待核对' | '已暂停后续派发' | '已结束' | '能力受阻'
  currentFocusSummary: string
  createdAt: string
  identities: DemoIdentity[]
  stepLimitPerThread: number
  concurrencyLimit: number
}

export interface DemoObservation {
  id: string
  projectId: string
  threadId: string
  sourceType: '工具实证' | '模型观察' | '人工补充（演示）'
  title: string
  summary: string
  linkedRequestId?: string
  linkedModelCallId?: string
  linkedFindingId?: string
  recordedAt: string
}

export interface DemoThread {
  id: string
  projectId: string
  title: string
  executionStatus: '运行中' | '待核对' | '已暂停后续派发' | '已结束'
  questionAndBasis: string
  nextStepPlan: string
  blockingReason: string | null
  blockingRequestId: string | null
  executedSteps: number
  maxSteps: number
  pendingApprovedActionsOnResume: string[]
  updatedAt: string
}

export type RequestSendStatus =
  | '已有响应'
  | '发送效果未知'
  | '越界未发送'

export interface DemoTrafficRequest {
  id: string
  projectId: string
  projectName: string
  threadId: string
  threadTitle: string
  toolActionId: string
  usageStage: '探索采样' | '独立验证' | '单漏洞复测'
  verificationPurpose: string
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  url: string
  normalizedOrigin: string
  identityId: string
  identityLabel: string
  sendStatus: RequestSendStatus
  statusCode: number | null
  statusText: string
  capturedAt: string
  captureSource: string
  truncated: boolean
  truncationNote: string | null
  scopeValidity: string
  immutableFingerprint: string
  requestHeaders: Record<string, string>
  requestBody: string
  responseHeaders: Record<string, string> | null
  responseBody: string | null
  unknownOutcomeNote: string | null
  reconciliationNote: string | null
  linkedFindingIds: string[]
  linkedModelCallId?: string
}

export type ModelCallPurpose = '探索' | '验证' | '复测'

export interface DemoModelCall {
  id: string
  projectId: string
  projectName: string
  threadId: string
  threadTitle: string
  purpose: ModelCallPurpose
  turnLabel: string
  sequenceCursor: string
  modelName: string
  durationMs: number
  status: '已完成' | '响应截断' | '调用失败'
  recordedAt: string
  contextIsolationNote: string
  inputSummary: string
  outputSummary: string
  recordingEnabled: boolean
  actualInputText: string | null
  actualOutputText: string | null
  linkedRequestIds: string[]
  linkedFindingId?: string
}

export type GraphNodeKind = 'thread' | 'activity' | 'evidence' | 'finding'

export interface DemoGraphNode {
  id: string
  projectId: string
  kind: GraphNodeKind
  kindLabel: '研究方向' | '工具活动' | '流量证据' | '漏洞记录'
  code: string
  title: string
  statusLabel: string
  summary: string
  col: 0 | 1 | 2 | 3
  row: number
  targetTab: ProjectTabKey
  targetObjectId: string
}

export interface DemoGraphEdge {
  id: string
  projectId: string
  fromId: string
  toId: string
  relationLabel: string
  evidenceBasis: string
}

export interface DemoReproductionStep {
  stepNumber: number
  instruction: string
  identityPrerequisite: string
  linkedRequestId: string
  expectedObservation: string
}

export interface DemoVerificationRecord {
  id: string
  findingId: string
  executedAt: string
  executionContextNote: string
  verdictLabel: '验证成立' | '验证不成立（正常权限功能）' | '待验证（效果未知待核对）'
  severityIndependenceNote: string
  evidenceRequestIds: string[]
  modelCallId: string
  summary: string
}

export interface DemoTimelineEvent {
  id: string
  findingId: string
  timestamp: string
  stageLabel:
    | '产生观察'
    | '分配候选编号'
    | '补充对比流量'
    | '独立验证记录'
    | '发起复测'
    | '复测结论记录'
  actorSource: '工具捕获事实' | '平台编号服务' | '独立验证上下文（主稿推荐）' | '复测任务执行'
  summary: string
  linkedRequestId?: string
  linkedModelCallId?: string
  linkedRetestId?: string
  linkedReportId?: string
}

export type RetestConclusionStatus =
  | '仍存在'
  | '本次范围修复已验证'
  | '无法判断'

export interface DemoRetestRecord {
  id: string
  findingId: string
  projectId: string
  boundFindingRevision: string
  reusedReproductionLogic: string
  targetIdentityLabel: string
  authorizedScope: string
  executionContextNote: string
  executionStatus: '已完成（演示）' | '前置鉴权受阻（演示）'
  conclusion: RetestConclusionStatus
  conclusionBasis: string
  linkedRequestId: string
  linkedModelCallId?: string
  independentReportId: string
  executedAt: string
}

export type FindingVerificationStatus =
  | '验证成立'
  | '验证不成立（正常权限功能）'
  | '待验证'

export interface DemoFinding {
  id: string
  projectId: string
  title: string
  verificationStatus: FindingVerificationStatus
  severityLabel: '高危' | '中危' | '低危' | '待评估' | '无危害（非漏洞）'
  severityBasis: string
  remediationStatus: '未处理' | '声称已修复（已有复测结论）' | '无需修复（正常功能）'
  affectedTarget: string
  currentRevision: string
  threadId: string
  descriptionPrincipleAndHarm: string
  reproductionSteps: DemoReproductionStep[]
  remediationAdvice: string
  verificationRecords: DemoVerificationRecord[]
  updatedAt: string
}

export interface DemoReportVersion {
  id: string
  projectId: string
  versionTag: string
  reportKind: '项目漏洞报告' | '单漏洞复测报告'
  title: string
  boundFindingIds: string[]
  boundRetestId?: string
  boundEvidenceIds: string[]
  generationStatus: '已固定版本' | '草稿' | '生成失败（已保留上一版本）'
  publishStatus: '仅本地保存（未发布）' | '本地已审阅版本'
  excludedFindingNote: string | null
  createdAt: string
  markdownContent: string
  jsonContent: string
}

export interface PendingDecisionItem {
  id: string
  topic: string
  recommendation: string
  implicationIfDeferred: string
  statusLabel: '已确认调整（D1）' | '待确认（主稿建议）' | '已延期（后续阶段）'
}

const SENSITIVE_HEADER_NAMES = new Set([
  'authorization',
  'proxy-authorization',
  'cookie',
  'set-cookie',
  'x-api-key',
  'x-auth-token',
  'api-key',
])

export function isSensitiveHeaderName(name: string): boolean {
  return SENSITIVE_HEADER_NAMES.has(name.trim().toLowerCase())
}

export function formatDemoHeaders(
  headers: Record<string, string> | null,
  revealSensitive: boolean,
): { lines: string[]; hasSensitive: boolean } {
  if (!headers) {
    return { lines: [], hasSensitive: false }
  }
  let hasSensitive = false
  const lines = Object.entries(headers).map(([key, value]) => {
    const sensitive = isSensitiveHeaderName(key)
    if (sensitive) {
      hasSensitive = true
    }
    return sensitive && !revealSensitive
      ? `${key}: [已隐藏]`
      : `${key}: ${value}`
  })
  return { lines, hasSensitive }
}

export function buildRawHttpRequestText(
  req: DemoTrafficRequest,
  revealSensitive: boolean,
): string {
  const parsedUrl = new URL(req.url)
  const pathWithQuery = `${parsedUrl.pathname}${parsedUrl.search}` || '/'
  const { lines } = formatDemoHeaders(req.requestHeaders, revealSensitive)
  const headerBlock = lines.join('\n')
  if (!req.requestBody) {
    return `${req.method} ${pathWithQuery} HTTP/1.1\nHost: ${parsedUrl.host}\n${headerBlock}`
  }
  return `${req.method} ${pathWithQuery} HTTP/1.1\nHost: ${parsedUrl.host}\n${headerBlock}\n\n${req.requestBody}`
}

export function buildRawHttpResponseText(
  req: DemoTrafficRequest,
  revealSensitive: boolean,
): string {
  if (req.statusCode === null || !req.responseHeaders) {
    return '（未发送至目标或连接中断，无目标 HTTP 响应报文）'
  }
  const { lines } = formatDemoHeaders(req.responseHeaders, revealSensitive)
  const headerBlock = lines.join('\n')
  const body = req.responseBody ?? ''
  return `HTTP/1.1 ${req.statusCode} ${req.statusText}\n${headerBlock}\n\n${body}`
}
