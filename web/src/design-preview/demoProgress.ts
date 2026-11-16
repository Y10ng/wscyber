import type { DemoFinding, DemoModelCall, DemoProject, DemoThread, DemoTimelineEvent, DemoTrafficRequest } from './types.ts'
import type { TaskConfiguration } from './TaskConfiguration.tsx'

export interface DemoProgress {
  round: number
  stage: number
  findingId?: string
}

export const INITIAL_PROGRESS: DemoProgress = { round: 1, stage: 0 }

// 每次仅由点击推进。所有内容都是本地 fixture，不发送目标或模型请求。
export function createDemoTurn(project: DemoProject, thread: DemoThread, config: TaskConfiguration, round: number, stage: number) {
  const suffix = `${project.id}-R${round}`
  const requestId = `REQ-DEMO-${suffix}`
  const callId = `LLM-DEMO-${suffix}`
  const findingId = `F-DEMO-${project.id}`
  const origin = project.allowedOrigins[0]!
  const confirmed = stage === 3
  const compared = stage >= 2
  const description = stage <= 1
    ? '订单响应包含归属字段；尚缺另一身份对照与业务权限说明，暂不能确认越权。'
    : confirmed
      ? '在本演示的非共享订单规则下，身份 B 仍能读取身份 A 的订单摘要。对照与复核支持跨身份读取问题；只确认本次示例范围。'
      : '补充身份 B 对同一订单的响应，仍返回身份 A 的归属字段。正文已更新，业务权限与共享规则尚待复核。'
  const request: DemoTrafficRequest = {
    id: requestId, projectId: project.id, projectName: project.name,
    threadId: thread.id, threadTitle: thread.title, toolActionId: `ACT-DEMO-${suffix}`,
    usageStage: confirmed ? '独立验证' : '探索采样',
    verificationPurpose: confirmed ? '复核双身份差异与非共享业务规则（演示）' : '订单归属字段与身份对照（演示）',
    method: 'GET', url: `${origin}/demo/orders/DEMO-01`, normalizedOrigin: origin,
    identityId: compared ? 'demo-b' : 'demo-a', identityLabel: compared ? '演示身份 B' : '演示身份 A',
    sendStatus: '已有响应', statusCode: 200, statusText: 'OK（演示）',
    capturedAt: `演示第 ${round} 轮`, captureSource: '本地演示 fixture · 非实际捕获',
    truncated: false, truncationNote: null, scopeValidity: `演示范围：${origin}`,
    immutableFingerprint: `demo-reference:${suffix}（非真实指纹）`,
    requestHeaders: { Accept: 'application/json', 'X-Demo-Identity': compared ? 'B' : 'A' },
    requestBody: '', responseHeaders: { 'Content-Type': 'application/json' },
    responseBody: JSON.stringify({ demo: true, order: 'DEMO-01', owner: 'A', shared: false }, null, 2),
    unknownOutcomeNote: null, reconciliationNote: null,
    linkedFindingIds: stage > 0 ? [findingId] : [], linkedModelCallId: callId,
  }
  const call: DemoModelCall = {
    id: callId, projectId: project.id, projectName: project.name,
    threadId: thread.id, threadTitle: thread.title, purpose: confirmed ? '验证' : '探索',
    turnLabel: `演示第 ${round} 轮`, sequenceCursor: `demo:${suffix}`,
    modelName: '本地模型记录示例（未调用模型）', durationMs: 0,
    status: '已完成', recordedAt: `演示第 ${round} 轮`,
    contextIsolationNote: '仅内存演示；耗时 0 表示未执行。',
    inputSummary: '任务配置与证据引用示例，不代表完整输入。',
    outputSummary: stage ? description : '开始整理目标与授权范围（演示）。',
    recordingEnabled: config.saveModelIo,
    actualInputText: config.saveModelIo ? JSON.stringify({
      demo: true, project: project.id, round, scope: project.allowedOrigins,
      target: project.targetAndKnownInfo, objective: project.desiredObjective,
      selected_skill_versions: config.skills.filter((ref) => ref.effectiveFromRound <= round),
      ...(config.prohibitedPromptEnabled ? { prohibited_behaviors: config.prohibitedBehaviors } : {}),
    }, null, 2) : null,
    actualOutputText: config.saveModelIo ? JSON.stringify({ demo: true, observation: description }, null, 2) : null,
    linkedRequestIds: [requestId], ...(stage ? { linkedFindingId: findingId } : {}),
  }
  const finding: DemoFinding = {
    id: findingId, projectId: project.id, threadId: thread.id,
    title: '订单摘要跨身份读取（增量演示）', verificationStatus: confirmed ? '验证成立' : '待验证',
    severityLabel: confirmed ? '中危' : '待评估',
    severityBasis: confirmed ? '中危（演示评估）：非共享订单摘要向另一身份暴露。' : '候选不等于确认；严重度尚待评估。',
    remediationStatus: '未处理', affectedTarget: request.url,
    currentRevision: `rev-${stage}（演示）`, descriptionPrincipleAndHarm: description,
    reproductionSteps: [{ stepNumber: 1, instruction: request.verificationPurpose,
      identityPrerequisite: request.identityLabel, linkedRequestId: requestId,
      expectedObservation: compared ? '身份 B 仍收到 owner=A 的订单摘要（演示）。' : '自有身份读取成功，尚不证明越权（演示）。' }],
    remediationAdvice: confirmed ? '按认证主体校验订单归属和共享权限；加入双身份回归用例。' : '先补齐权限规则与对照证据，再确定修复建议。',
    verificationRecords: confirmed ? [{
      id: `VER-DEMO-${suffix}`, findingId, executedAt: `演示第 ${round} 轮`,
      executionContextNote: '本地验证状态示例，未执行实际验证。', verdictLabel: '验证成立',
      severityIndependenceNote: '验证结论与严重度分开。', evidenceRequestIds: [requestId], modelCallId: callId,
      summary: '演示非共享规则与身份对照一致，排除本示例中的正常共享解释。',
    }] : [], updatedAt: `演示第 ${round} 轮`,
  }
  const event: DemoTimelineEvent = {
    id: `TL-DEMO-${suffix}`, findingId, timestamp: `演示第 ${round} 轮`,
    stageLabel: confirmed ? '独立验证记录' : stage === 2 ? '补充对比流量' : '分配候选编号',
    actorSource: confirmed ? '独立验证上下文（主稿推荐）' : '工具捕获事实',
    summary: `${finding.currentRevision} · ${finding.verificationStatus}：${description}`,
    linkedRequestId: requestId, linkedModelCallId: callId,
  }
  return { request, call, finding, event }
}

export function parseDemoOrigins(text: string): string[] | null {
  const entries = text.split(/[\n,]/).map((entry) => entry.trim()).filter(Boolean)
  if (!entries.length) return null
  try {
    const origins = entries.map((entry) => {
      const url = new URL(entry)
      const reserved = /(^|\.)(example\.(com|org|net)|test)$/.test(url.hostname)
      if (!reserved || !['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash || url.pathname !== '/') {
        throw new Error('仅接受明确的保留测试域 origin')
      }
      return url.origin
    })
    return [...new Set(origins)]
  } catch {
    return null
  }
}
