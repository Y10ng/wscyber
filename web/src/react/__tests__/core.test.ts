import './httpRecords.test.ts'
import './models.test.ts'
import './modelUsage.test.ts'
import './materials.test.tsx'
import './skills.test.ts'
import './researchBrief.test.ts'
import './researchControls.test.ts'
import './context.test.ts'
import './findings.test.ts'
import './verification.test.ts'
import './workspaceLayout.test.tsx'
import './activity.test.tsx'
import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { normalizeOrigin, parseOriginInputText } from '../origin.ts'
import {
  beginSubmission,
  canonicalizeExplorationInput,
  generateOperationKey,
  handleOperationCheck404,
  handlePostFailure,
  handleSessionUnauthorizedDuringSubmission,
  initialSubmissionState,
  isSubmissionLocked,
  isValidOperationKey,
  utf8ByteLength,
} from '../idempotency.ts'
import {
  ApiError,
  StaleSessionError,
  getSessionEpoch,
  invalidateSession,
  requestApi,
  setMemoryToken,
  setOnUnauthorized,
} from '../api.ts'
import {
  ACTIVE_POLL_INTERVAL_MS,
  IDLE_POLL_INTERVAL_MS,
  WorkspaceRequestTracker,
  computeWorkspacePollDelayMs,
  createObservationActionSelection,
  formatHeadersForDisplay,
  isSensitiveHeaderName,
  resolveInspectingAction,
  resolveWorkspaceThreadTarget,
  selectMatchedSnapshot,
} from '../workspace.ts'
import type { Action, Attempt, Mission, Observation, Snapshot, Thread } from '../types.ts'
import type { InspectingActionSelection } from '../workspace.ts'
import type { RawObservationContext, RawObservationSelection } from '../observations.ts'
import { actionReceiptSummary, createRawObservationSelection, isCurrentRawObservationEvent, resolveRawObservation } from '../observations.ts'
import { ObservationRecords, ProjectWorkspaceView } from '../components/ProjectWorkspaceView.tsx'
import { ExplorationCreateActions, ExplorationCreateView } from '../components/ExplorationCreateView.tsx'
import { contentPathname, createCloseDestination, defaultCreateContext, nextCreateContext, parsePathname, readRouteLocation } from '../routing.ts'
import { LoginView } from '../components/LoginView.tsx'
import { EvidenceBytesViewer } from '../components/EvidenceBytesViewer.tsx'
import { ActionEvidenceDrawer } from '../components/ActionEvidenceDrawer.tsx'
import { createEvidenceBlob, createEvidencePreview, decodeEvidenceBytes, EVIDENCE_PREVIEW_LIMIT, safeEvidenceFilename } from '../../ui/evidenceBytes.ts'

describe('normalizeOrigin (Go execution.NormalizeOrigin parity)', () => {
  it('normalizes scheme, host case, default ports, trailing dot, and IPv6', () => {
    assert.deepEqual(normalizeOrigin('HTTP://Example.COM.:80'), {
      ok: true,
      normalized: 'http://example.com',
    })
    assert.deepEqual(normalizeOrigin('HTTPS://API.EXAMPLE.COM:443'), {
      ok: true,
      normalized: 'https://api.example.com',
    })
    assert.deepEqual(normalizeOrigin('https://example.com:8443'), {
      ok: true,
      normalized: 'https://example.com:8443',
    })
    assert.deepEqual(normalizeOrigin('http://127.0.0.1:08080'), {
      ok: true,
      normalized: 'http://127.0.0.1:8080',
    })
    assert.deepEqual(normalizeOrigin('http://[::1]:80'), {
      ok: true,
      normalized: 'http://[::1]',
    })
    assert.deepEqual(normalizeOrigin('http://[::ffff:192.0.2.1]:8080'), {
      ok: true,
      normalized: 'http://192.0.2.1:8080',
    })
  })

  it('strictly rejects paths, trailing slash, query, fragment, userinfo, whitespace, non-standard IP', () => {
    assert.equal(normalizeOrigin('https://example.com/').ok, false)
    assert.equal(normalizeOrigin('https://example.com/api').ok, false)
    assert.equal(normalizeOrigin('https://example.com?a=1').ok, false)
    assert.equal(normalizeOrigin('https://example.com#top').ok, false)
    assert.equal(normalizeOrigin('https://user:pass@example.com').ok, false)
    assert.equal(normalizeOrigin(' https://example.com').ok, false)
    assert.equal(normalizeOrigin('https://example.com ').ok, false)
    assert.equal(normalizeOrigin('example.com').ok, false)
    assert.equal(normalizeOrigin('ftp://example.com').ok, false)
    assert.equal(normalizeOrigin('https://*.example.com').ok, false)
    assert.equal(normalizeOrigin('https://example.com:').ok, false)
    assert.equal(normalizeOrigin('https://example.com:65536').ok, false)
    assert.equal(normalizeOrigin('http://127.1').ok, false)
    assert.equal(normalizeOrigin('http://0177.0.0.1').ok, false)
    assert.equal(normalizeOrigin('http://-bad.example.com').ok, false)
    assert.equal(normalizeOrigin('http://测试.example.com').ok, false)
  })

  it('sorts and deduplicates valid origins and reports line errors', () => {
    const parsed = parseOriginInputText(
      'https://b.example.com:443\nhttp://a.example.com:80, https://b.example.com.\nhttps://bad.example.com/path',
    )
    assert.deepEqual(parsed.origins, ['http://a.example.com', 'https://b.example.com'])
    assert.equal(parsed.errors.length, 1)
    assert.equal(parsed.errors[0].line, 3)
  })
})

describe('Idempotency key & reliable submission state machine', () => {
  it('generates 128-bit (32 hex chars) valid operation keys', () => {
    const k1 = generateOperationKey()
    const k2 = generateOperationKey()
    assert.equal(k1.length, 32)
    assert.match(k1, /^[0-9a-f]{32}$/)
    assert.notEqual(k1, k2)
    assert.equal(isValidOperationKey(k1), true)
    assert.equal(isValidOperationKey('short'), false)
  })

  it('freezes key and payload on first submit and blocks payload modification while unverified', () => {
    const payload = canonicalizeExplorationInput('目标说明', '发现越权', [
      'https://b.example.com',
      'https://a.example.com',
    ])
    assert.deepEqual(payload.allowed_origins, ['https://a.example.com', 'https://b.example.com'])

    const s1 = beginSubmission(initialSubmissionState, payload)
    assert.equal(s1.phase, 'submitting')
    assert.equal(isSubmissionLocked(s1), true)
    const originalKey = s1.operationKey
    assert.ok(originalKey)

    // Network loss / timeout (status 0) retains key and frozen payload
    const s2 = handlePostFailure(s1, 0, '请求超时。')
    assert.equal(s2.phase, 'unverified')
    assert.equal(s2.operationKey, originalKey)
    assert.deepEqual(s2.frozenPayload, payload)

    // Operations 404 retains key and does not unlock for new key creation
    const s3 = handleOperationCheck404(s2)
    assert.equal(s3.phase, 'unverified')
    assert.equal(s3.operationKey, originalKey)
    assert.equal(s3.operationsChecked404, true)

    // Attempting to submit with modified payload while unverified throws
    const modified = canonicalizeExplorationInput('修改后的说明', '发现越权', [
      'https://a.example.com',
    ])
    assert.throws(() => beginSubmission(s3, modified), /不能修改请求内容或更换提交键/)

    // Retrying with the exact same payload reuses the exact same key
    const s4 = beginSubmission(s3, payload)
    assert.equal(s4.operationKey, originalKey)
    assert.equal(s4.phase, 'submitting')

    // 409 and 503 also retain the original key
    const s5 = handlePostFailure(s4, 409, '状态已变化')
    assert.equal(s5.operationKey, originalKey)
    const s6 = handlePostFailure(s5, 503, '能力未配置')
    assert.equal(s6.operationKey, originalKey)

    // 400 validation error releases the lock so user can fix input
    const s7 = handlePostFailure(s1, 400, '输入无效')
    assert.equal(s7.phase, 'idle')
    assert.equal(s7.operationKey, null)
  })

  it('transitions in-flight submitting/verifying to unverified on 401 and never resurrects after explicit logout', () => {
    const payload = canonicalizeExplorationInput('目标说明', '发现越权', [
      'https://a.example.com',
    ])
    const submittingState = beginSubmission(initialSubmissionState, payload)
    const key = submittingState.operationKey

    // Parallel 401 while POST is in-flight transitions phase from submitting to unverified
    const after401 = handleSessionUnauthorizedDuringSubmission(submittingState)
    assert.equal(after401.phase, 'unverified')
    assert.equal(after401.operationKey, key)
    assert.deepEqual(after401.frozenPayload, payload)

    // Explicit logout resets state to initialSubmissionState; late errors cannot resurrect it
    assert.deepEqual(
      handlePostFailure(initialSubmissionState, 500, '晚到错误'),
      initialSubmissionState,
    )
    assert.deepEqual(
      handleOperationCheck404(initialSubmissionState),
      initialSubmissionState,
    )
    assert.deepEqual(
      handleSessionUnauthorizedDuringSubmission(initialSubmissionState),
      initialSubmissionState,
    )
  })

  it('computes UTF-8 byte lengths accurately for Chinese text', () => {
    assert.equal(utf8ByteLength('安全研究'), 12)
  })
})

describe('Session epoch & 401 late-response protection', () => {
  it('invalidates session on 401 with reason=unauthorized and discards late responses', async () => {
    const origFetch = globalThis.fetch
    let unauthorizedTriggered = 0
    setOnUnauthorized(() => {
      unauthorizedTriggered += 1
    })

    try {
      setMemoryToken('token-1')
      const startEpoch = getSessionEpoch()

      let resolveSlowFetch!: (res: Response) => void
      const slowFetchPromise = new Promise<Response>((resolve) => {
        resolveSlowFetch = resolve
      })

      globalThis.fetch = (async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/missions')) {
          return slowFetchPromise
        }
        return new Response(JSON.stringify({ error: '需要有效的访问令牌' }), {
          status: 401,
          headers: { 'Content-Type': 'application/json' },
        })
      }) as typeof fetch

      const slowCall = requestApi('/missions')
      const unauthorizedCall = requestApi('/system')

      await assert.rejects(unauthorizedCall, (err: unknown) => {
        return err instanceof ApiError && err.status === 401
      })
      assert.equal(unauthorizedTriggered, 1)
      assert.ok(getSessionEpoch() > startEpoch)

      resolveSlowFetch(
        new Response(JSON.stringify([{ id: 'secret-mission' }]), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )

      await assert.rejects(slowCall, (err: unknown) => {
        return err instanceof StaleSessionError && err.reason === 'unauthorized'
      })
    } finally {
      globalThis.fetch = origFetch
      setOnUnauthorized(null)
      invalidateSession(false)
    }
  })

  it('marks explicit logout invalidation with reason=logout', async () => {
    const origFetch = globalThis.fetch
    try {
      setMemoryToken('token-2')
      let resolveSlowFetch!: (res: Response) => void
      const slowFetchPromise = new Promise<Response>((resolve) => {
        resolveSlowFetch = resolve
      })
      globalThis.fetch = (async () => slowFetchPromise) as typeof fetch

      const inFlight = requestApi('/missions')
      invalidateSession(false)
      resolveSlowFetch(new Response('[]', { status: 200 }))

      await assert.rejects(inFlight, (err: unknown) => {
        return err instanceof StaleSessionError && err.reason === 'logout'
      })
    } finally {
      globalThis.fetch = origFetch
      invalidateSession(false)
    }
  })
})

describe('Workspace route generation, non-fallback 404, Drawer isolation, polling, and header masking', () => {
  const sampleMission: Mission = {
    id: 'm-1',
    title: '测试项目',
    goal: '验证接口鉴权',
    origin: '背景信息',
    allowed_origins: ['https://api.example.com'],
    created_at: '2026-11-11T00:00:00Z',
  }

  const threadA: Thread = {
    id: 't-a',
    mission_id: 'm-1',
    title: '方向 A',
    question: '问题 A',
    status: 'ready',
    priority: 1,
    auto_run: true,
    step_count: 0,
    next_step: '',
    rationale: '',
    created_at: '2026-11-11T00:00:00Z',
    updated_at: '2026-11-11T00:00:00Z',
  }

  const threadB: Thread = {
    ...threadA,
    id: 't-b',
    title: '方向 B',
    status: 'concluded',
    auto_run: false,
  }

  const actionA: Action = {
    id: 'act-1',
    thread_id: 't-a',
    status: 'executed',
    decision_reason: '允许',
    request_hash: 'abc',
    input: {
      kind: 'http',
      method: 'GET',
      url: 'https://api.example.com/v1',
      reason: '探测',
    },
    error: '',
    created_at: '2026-11-11T00:00:00Z',
    updated_at: '2026-11-11T00:00:00Z',
  }

  const snapshotA: Snapshot = {
    mission: sampleMission,
    thread: threadA,
    attempts: [],
    observations: [],
    actions: [actionA],
  }

  it('guards against stale responses when switching threads or missions', () => {
    const tracker = new WorkspaceRequestTracker()
    const genA = tracker.begin('m-1', 't-a')
    const genB = tracker.begin('m-1', 't-b')

    assert.equal(tracker.isCurrent(genA, 'm-1', 't-a'), false)
    assert.equal(tracker.isCurrent(genB, 'm-1', 't-b'), true)

    tracker.invalidate()
    assert.equal(tracker.isCurrent(genB, 'm-1', 't-b'), false)
  })

  it('does not fallback to another thread when an explicit threadId is requested and validates snapshot thread.id', () => {
    const explicitTarget = resolveWorkspaceThreadTarget('t-missing', [threadA, threadB])
    assert.equal(explicitTarget.isExplicit, true)
    assert.equal(explicitTarget.targetThreadId, 't-missing')

    const implicitTarget = resolveWorkspaceThreadTarget(null, [threadA, threadB])
    assert.equal(implicitTarget.isExplicit, false)
    assert.equal(implicitTarget.targetThreadId, 't-a')

    // Snapshot for t-a must NOT match when effectiveThreadId is t-b or t-missing
    assert.equal(selectMatchedSnapshot(snapshotA, 'm-1', 't-b'), null)
    assert.equal(selectMatchedSnapshot(snapshotA, 'm-1', 't-missing'), null)
    assert.equal(selectMatchedSnapshot(snapshotA, 'm-2', 't-a'), null)
    assert.equal(selectMatchedSnapshot(snapshotA, 'm-1', 't-a'), snapshotA)
  })

  it('clears Drawer action if mission or thread changes', () => {
    const selection = { missionId: 'm-1', threadId: 't-a', actionId: 'act-1' }
    assert.equal(
      resolveInspectingAction(selection, sampleMission, 't-a', snapshotA),
      actionA,
    )
    // Switching thread to t-b immediately invalidates inspectingAction
    assert.equal(
      resolveInspectingAction(selection, sampleMission, 't-b', snapshotA),
      null,
    )
    // Switching mission to m-2 immediately invalidates inspectingAction
    assert.equal(
      resolveInspectingAction(
        selection,
        { ...sampleMission, id: 'm-2' },
        't-a',
        snapshotA,
      ),
      null,
    )
  })

  it('schedules bounded serial polling for active threads and stops on 404 or completed', () => {
    // Active ready + auto_run thread polls at ACTIVE_POLL_INTERVAL_MS
    assert.equal(
      computeWorkspacePollDelayMs({
        authenticated: true,
        isWorkspaceRoute: true,
        documentHidden: false,
        pollCount: 0,
        terminalErrorStatus: null,
        consecutiveFailures: 0,
        snapshot: snapshotA,
        threads: [threadA],
      }),
      ACTIVE_POLL_INTERVAL_MS,
    )

    // Completed thread with no running actions stops polling
    const completedSnapshot: Snapshot = {
      ...snapshotA,
      thread: threadB,
    }
    assert.equal(
      computeWorkspacePollDelayMs({
        authenticated: true,
        isWorkspaceRoute: true,
        documentHidden: false,
        pollCount: 1,
        terminalErrorStatus: null,
        consecutiveFailures: 0,
        snapshot: completedSnapshot,
        threads: [threadB],
      }),
      null,
    )

    // Paused thread demotes to IDLE_POLL_INTERVAL_MS then stops at pollCount >= 20
    const pausedSnapshot: Snapshot = {
      ...snapshotA,
      thread: { ...threadA, status: 'paused', auto_run: false },
    }
    assert.equal(
      computeWorkspacePollDelayMs({
        authenticated: true,
        isWorkspaceRoute: true,
        documentHidden: false,
        pollCount: 3,
        terminalErrorStatus: null,
        consecutiveFailures: 0,
        snapshot: pausedSnapshot,
        threads: [pausedSnapshot.thread],
      }),
      IDLE_POLL_INTERVAL_MS,
    )
    assert.equal(
      computeWorkspacePollDelayMs({
        authenticated: true,
        isWorkspaceRoute: true,
        documentHidden: false,
        pollCount: 20,
        terminalErrorStatus: null,
        consecutiveFailures: 0,
        snapshot: pausedSnapshot,
        threads: [pausedSnapshot.thread],
      }),
      null,
    )

    // 404 / 403 error or hidden document stops polling immediately
    assert.equal(
      computeWorkspacePollDelayMs({
        authenticated: true,
        isWorkspaceRoute: true,
        documentHidden: false,
        pollCount: 0,
        terminalErrorStatus: 404,
        consecutiveFailures: 1,
        snapshot: null,
        threads: [threadA],
      }),
      null,
    )
    assert.equal(
      computeWorkspacePollDelayMs({
        authenticated: true,
        isWorkspaceRoute: true,
        documentHidden: false,
        pollCount: 0,
        terminalErrorStatus: 403,
        consecutiveFailures: 1,
        snapshot: null,
        threads: [threadA],
      }),
      null,
    )
    assert.equal(
      computeWorkspacePollDelayMs({
        authenticated: true,
        isWorkspaceRoute: true,
        documentHidden: true,
        pollCount: 0,
        terminalErrorStatus: null,
        consecutiveFailures: 0,
        snapshot: snapshotA,
        threads: [threadA],
      }),
      null,
    )
  })

  it('masks sensitive HTTP headers by default and reveals when requested', () => {
    assert.equal(isSensitiveHeaderName('Authorization'), true)
    assert.equal(isSensitiveHeaderName('proxy-authorization'), true)
    assert.equal(isSensitiveHeaderName('COOKIE'), true)
    assert.equal(isSensitiveHeaderName('Set-Cookie'), true)
    assert.equal(isSensitiveHeaderName('X-API-Key'), true)
    assert.equal(isSensitiveHeaderName('Content-Type'), false)

    const headers = {
      'Content-Type': 'application/json',
      Authorization: 'Bearer secret-jwt-123',
      'Set-Cookie': ['sid=abc123; HttpOnly', 'theme=light'],
      'x-api-key': 'k-secret',
    }

    const masked = formatHeadersForDisplay(headers, false)
    assert.equal(masked.hasSensitive, true)
    assert.match(masked.text, /Content-Type: application\/json/)
    assert.match(masked.text, /Authorization: \[已隐藏\]/)
    assert.match(masked.text, /Set-Cookie: \[已隐藏\]/)
    assert.match(masked.text, /x-api-key: \[已隐藏\]/)
    assert.doesNotMatch(masked.text, /secret-jwt-123/)
    assert.doesNotMatch(masked.text, /sid=abc123/)
    assert.doesNotMatch(masked.text, /k-secret/)

    const revealed = formatHeadersForDisplay(headers, true)
    assert.equal(revealed.hasSensitive, true)
    assert.match(revealed.text, /Authorization: Bearer secret-jwt-123/)
    assert.match(revealed.text, /Set-Cookie: sid=abc123; HttpOnly/)
    assert.match(revealed.text, /x-api-key: k-secret/)
  })
})

describe('AF-1 最近一次尝试的失败记录', () => {
  const mission: Mission = { id: 'm-af', title: '失败提示核验', goal: '只按真实尝试记录显示', origin: '本地受控材料', allowed_origins: ['https://example.com'], created_at: '2026-11-11T00:00:00Z' }
  const thread: Thread = { id: 't-af', mission_id: mission.id, title: '方向', question: '当前问题', rationale: '', next_step: '下一步以服务端事实为准', status: 'blocked', priority: 1, auto_run: false, step_count: 2, created_at: mission.created_at, updated_at: mission.created_at }
  const attempt = (id: string, status: string, error: string, startedAt = '2026-11-11T00:00:00Z'): Attempt => ({ id, thread_id: thread.id, status, summary: '', error, started_at: startedAt })
  const render = (attempts: Attempt[], currentThread = thread) => renderToStaticMarkup(
    React.createElement(ProjectWorkspaceView, {
      mission, threads: [currentThread], selectedThreadId: currentThread.id,
      snapshot: { mission, thread: currentThread, attempts, observations: [], actions: [] },
      loadingMission: false, loadingSnapshot: false, error: '', threadError: '', acceptedBanner: '',
      onDismissAcceptedBanner() {}, onBackToList() {}, onRefresh() {}, onSelectThread() {},
    }),
  )
  const failureNotice = /最近尝试失败/

  it('移除常驻能力说明但保留真实任务背景和授权范围', () => {
    const html = render([])
    assert.match(html, /本地受控材料/)
    assert.match(html, /https:\/\/example\.com/)
    assert.doesNotMatch(html, /平台 HTTP 强制校验|工具容器 bridge 直连尚未限制|未提供额外背景/)
  })

  it('显示唯一且最新失败尝试的错误，不宣称它是当前阻塞原因', () => {
    const html = render([attempt('a1', 'failed', '本次公开错误')])
    assert.match(html, /本次公开错误/)
    assert.doesNotMatch(html, /当前阻塞原因/)
  })

  it('按后端升序集合取最后一次，而不是第一条历史失败，且不改原集合', () => {
    const attempts = [attempt('a1', 'failed', '旧错误-A'), attempt('a2', 'failed', '新错误-B', '2026-11-11T00:01:00Z')]
    const before = structuredClone(attempts)
    const html = render(attempts)
    assert.match(html, /新错误-B/)
    assert.doesNotMatch(html, /旧错误-A/)
    assert.deepEqual(attempts, before)
  })

  it('后续成功尝试遇模型block或预算阻塞时，不回退旧失败或解析next_step', () => {
    for (const next_step of ['等待补充材料后再研究', '已达到研究步数预算（2/2）；问题保持未决。']) {
      const html = render([attempt('a1', 'failed', '早先模型错误'), attempt('a2', 'succeeded', '', '2026-11-11T00:01:00Z')], { ...thread, next_step })
      assert.doesNotMatch(html, failureNotice)
      assert.doesNotMatch(html, /早先模型错误/)
      assert.ok(html.includes(next_step))
    }
  })

  for (const status of ['interrupted', 'waiting_approval', 'running']) {
    it(`最后一次为${status}时，不越过该记录寻找旧失败`, () => {
      const html = render([attempt('a1', 'failed', '早先失败'), attempt('a2', status, '本次非failed记录', '2026-11-11T00:01:00Z')])
      assert.doesNotMatch(html, failureNotice)
      assert.doesNotMatch(html, /早先失败/)
    })
  }

  it('空集合不制造失败提示', () => {
    assert.doesNotMatch(render([]), failureNotice)
  })

  it('最后一次失败没有error时不回退更早的有错记录', () => {
    assert.doesNotMatch(render([attempt('a1', 'failed', '更早错误'), attempt('a2', 'failed', '', '2026-11-11T00:01:00Z')]), failureNotice)
  })

  it('非blocked方向不新添失败提示，不改变状态语义', () => {
    for (const status of ['ready', 'running', 'paused', 'concluded', 'waiting_approval']) {
      assert.doesNotMatch(render([attempt('a1', 'failed', '历史错误')], { ...thread, status }), failureNotice)
    }
  })

  it('相同started_at仍遵循后端id排序后的末条，不在客户端重新排序', () => {
    const html = render([attempt('a1', 'failed', '同刻旧错误'), attempt('a2', 'failed', '同刻新错误')])
    assert.match(html, /同刻新错误/)
    assert.doesNotMatch(html, /同刻旧错误/)
  })

  it('错误仍为未脱敏纯文本，不执行HTML，也不将它解析成状态', () => {
    const html = render([attempt('a1', 'failed', '<img src="https://example.invalid/af1">公开fixture错误')])
    assert.match(html, /&lt;img src=&quot;https:\/\/example\.invalid\/af1&quot;&gt;公开fixture错误/)
    assert.doesNotMatch(html, /<img[^>]+example\.invalid/)
  })
})

describe('OL-1 观察就地关联回执', () => {
  const mission: Mission = { id: 'm', title: '关联项目', goal: '', origin: '', allowed_origins: [], created_at: '' }
  const thread: Thread = { id: 't', mission_id: 'm', title: '关联方向', question: '', rationale: '', next_step: '', status: 'ready', priority: 1, auto_run: false, step_count: 0, created_at: '', updated_at: '' }
  const action: Action = { id: 'a', thread_id: 't', status: 'succeeded', request_hash: 'hash', decision_reason: '', error: '', created_at: '', updated_at: '', input: { kind: 'http', method: 'GET', url: 'https://fixture.invalid', reason: '受控测试' } }
  const observation: Observation = { id: 'o', thread_id: 't', action_id: 'a', kind: 'action_result', content: '动作回执，不是漏洞确认', created_at: '' }
  const snapshot: Snapshot = { mission, thread, actions: [action], observations: [observation], attempts: [] }
  const selection: InspectingActionSelection = { missionId: 'm', threadId: 't', actionId: 'a', observationId: 'o' }
  const renderRecords = (value: Snapshot, project: Mission | null = mission, threadId: string | null = 't') => renderToStaticMarkup(React.createElement(ObservationRecords, {
    mission: project, selectedThreadId: threadId, snapshot: value, onInspect: () => { throw new Error('渲染不能打开回执') },
  }))

  it('无结构化action_id不从正文、kind或数组位置猜关联，也无常驻解释', () => {
    for (const actionId of [undefined, '']) {
      const note = { ...observation, action_id: actionId, content: 'action_id=a，查看 a 的回执' }
      const value = { ...snapshot, observations: [note] }
      assert.equal(createObservationActionSelection(note, mission, 't', value), null)
      assert.doesNotMatch(renderRecords(value), /查看关联回执|关联回执不可用|<pre/)
      assert.match(renderRecords(value), /展开观察原文（可能含敏感值）/)
    }
  })

  it('只按结构化ID定位HTTP或命令；已放行未执行和unknown也只读取现有动作', () => {
    const command: Action = { ...action, input: { kind: 'local_command', command: 'fixture', reason: '' } }
    for (const linkedAction of [action, command, { ...action, status: 'approved' }, { ...action, status: 'outcome_unknown' }]) {
      const value = { ...snapshot, actions: [{ ...action, id: 'not-linked' }, linkedAction] }
      const chosen = createObservationActionSelection(observation, mission, 't', value)
      assert.deepEqual(chosen, selection)
      assert.equal(resolveInspectingAction(chosen, mission, 't', value), linkedAction)
      const html = renderRecords(value)
      assert.equal((html.match(/查看关联回执/g) || []).length, 1)
      assert.doesNotMatch(html, /关联回执不可用|href=|已确认漏洞/)
    }
    const note = { ...observation, kind: 'operator_note' }
    assert.deepEqual(createObservationActionSelection(note, mission, 't', { ...snapshot, observations: [note] }), selection)
  })

  it('关联缺失不回退其他动作，仅在该观察局部提示不可用', () => {
    for (const actions of [[], [{ ...action, id: 'another' }]]) {
      const value = { ...snapshot, actions }
      assert.equal(createObservationActionSelection(observation, mission, 't', value), null)
      assert.equal(resolveInspectingAction(selection, mission, 't', value), null)
      const html = renderRecords(value)
      assert.match(html, /关联回执不可用/)
      assert.doesNotMatch(html, /查看关联回执|已删除|<pre/)
      assert.match(html, /展开观察原文（可能含敏感值）/)
    }
  })

  it('同时核验当前项目、所选方向、快照项目与方向及方向mission_id', () => {
    const cases: [Mission, string, Snapshot][] = [
      [{ ...mission, id: 'other-m' }, 't', snapshot],
      [mission, 'other-t', snapshot],
      [mission, 't', { ...snapshot, mission: { ...mission, id: 'other-m' } }],
      [mission, 't', { ...snapshot, thread: { ...thread, id: 'other-t' } }],
      [mission, 't', { ...snapshot, thread: { ...thread, mission_id: 'other-m' } }],
    ]
    for (const [project, threadId, value] of cases) {
      assert.equal(createObservationActionSelection(observation, project, threadId, value), null)
      assert.equal(resolveInspectingAction(selection, project, threadId, value), null)
      assert.match(renderRecords(value, project, threadId), /关联回执不可用/)
      assert.doesNotMatch(renderRecords(value, project, threadId), /查看关联回执/)
    }
  })

  it('即使ID相同也拒绝跨方向的观察和动作', () => {
    const foreignObservation = { ...observation, thread_id: 'foreign-t' }
    const foreignAction = { ...action, thread_id: 'foreign-t' }
    for (const value of [
      { ...snapshot, observations: [foreignObservation] },
      { ...snapshot, actions: [foreignAction] },
    ]) {
      assert.equal(createObservationActionSelection(value.observations[0]!, mission, 't', value), null)
      assert.equal(resolveInspectingAction(selection, mission, 't', value), null)
      assert.match(renderRecords(value), /关联回执不可用/)
    }
  })

  it('拒绝不属于当前快照的旧观察对象，不借同ID的新观察打开', () => {
    const refreshedObservation = { ...observation }
    const refreshed = { ...snapshot, observations: [refreshedObservation] }
    assert.equal(createObservationActionSelection(observation, mission, 't', refreshed), null)
    assert.equal(createObservationActionSelection({ ...observation }, mission, 't', snapshot), null)
    assert.deepEqual(createObservationActionSelection(refreshedObservation, mission, 't', refreshed), selection)
  })

  it('旧选择不能借相同观察/动作ID进入新项目或方向', () => {
    const otherMission = { ...mission, id: 'other-m' }
    const otherThread = { ...thread, mission_id: 'other-m' }
    const otherSnapshot = { ...snapshot, mission: otherMission, thread: otherThread }
    assert.equal(resolveInspectingAction(selection, otherMission, 't', otherSnapshot), null)
    const nextThread = { ...thread, id: 'next-t' }
    const nextSnapshot = { ...snapshot, thread: nextThread, observations: [{ ...observation, thread_id: 'next-t' }], actions: [{ ...action, thread_id: 'next-t' }] }
    assert.equal(resolveInspectingAction(selection, mission, 'next-t', nextSnapshot), null)
  })

  it('重复观察或动作ID均拒绝，包括重复项只有一条归属正确', () => {
    for (const value of [
      { ...snapshot, observations: [observation, observation] },
      { ...snapshot, observations: [observation, { ...observation, thread_id: 'foreign-t' }] },
      { ...snapshot, actions: [action, action] },
      { ...snapshot, actions: [action, { ...action, thread_id: 'foreign-t' }] },
    ]) {
      assert.equal(createObservationActionSelection(observation, mission, 't', value), null)
      assert.equal(resolveInspectingAction(selection, mission, 't', value), null)
      assert.doesNotMatch(renderRecords(value), /查看关联回执/)
      assert.match(renderRecords(value), /关联回执不可用/)
    }
  })

  it('刷新仅取同一关联的最新回执，顺序变化不影响；关联变更、消失或跨归属立即失效', () => {
    const updated = { ...action, updated_at: '2026-11-11T01:00:00Z', status: 'outcome_unknown' }
    const other = { ...action, id: 'new-action' }
    const refreshed = { ...snapshot, actions: [other, updated], observations: [{ ...observation, id: 'another-observation' }, { ...observation }] }
    assert.equal(resolveInspectingAction(selection, mission, 't', refreshed), updated)
    for (const observations of [
      [],
      [{ ...observation, action_id: 'new-action' }],
      [{ ...observation, action_id: undefined }],
      [{ ...observation, thread_id: 'foreign-t' }],
    ]) {
      assert.equal(resolveInspectingAction(selection, mission, 't', { ...refreshed, observations }), null)
    }
    const changed = { ...refreshed, observations: [{ ...observation, action_id: 'new-action' }] }
    const next = createObservationActionSelection(changed.observations[0]!, mission, 't', changed)
    assert.deepEqual(next, { ...selection, actionId: 'new-action' })
    assert.equal(resolveInspectingAction(next, mission, 't', changed), other)
  })

  it('原动作入口仍可独立读取，无观察也不回退；同样拒绝动作归属异常与重复ID', () => {
    const direct = { missionId: 'm', threadId: 't', actionId: 'a' }
    assert.equal(resolveInspectingAction(direct, mission, 't', { ...snapshot, observations: [] }), action)
    for (const value of [
      { ...snapshot, actions: [] },
      { ...snapshot, actions: [{ ...action, id: 'another' }] },
      { ...snapshot, actions: [{ ...action, thread_id: 'foreign-t' }] },
      { ...snapshot, actions: [action, action] },
      { ...snapshot, thread: { ...thread, mission_id: 'other-m' } },
    ]) assert.equal(resolveInspectingAction(direct, mission, 't', value), null)
  })

  it('关闭或401清空上下文后没有可读取动作；不接受空来源标识', () => {
    assert.equal(resolveInspectingAction(null, mission, 't', snapshot), null)
    assert.equal(resolveInspectingAction(selection, null, 't', snapshot), null)
    assert.equal(resolveInspectingAction(selection, mission, null, snapshot), null)
    assert.equal(resolveInspectingAction(selection, mission, 't', null), null)
    assert.equal(createObservationActionSelection(observation, null, 't', snapshot), null)
    assert.equal(createObservationActionSelection(observation, mission, null, snapshot), null)
    assert.equal(createObservationActionSelection(observation, mission, 't', null), null)
    assert.equal(resolveInspectingAction({ ...selection, observationId: '' }, mission, 't', snapshot), null)
    assert.equal(resolveInspectingAction({ ...selection, actionId: '' }, mission, 't', snapshot), null)
  })

  it('SSR保留非动作观察正文为文本；无关联、有效、缺失三种记录仅提供对应局部入口', () => {
    const text = '<img src="https://untrusted.invalid/pixel"><script>bad()</script>'
    const value = { ...snapshot, observations: [
      { ...observation, id: 'no-link', action_id: undefined, kind: 'operator_note', content: text },
      observation,
      { ...observation, id: 'missing', action_id: 'missing' },
    ] }
    const html = renderRecords(value)
    assert.match(html, /&lt;img src=/)
    assert.doesNotMatch(html, /<img src=|<script>|href=|已确认漏洞|重放/)
    assert.equal((html.match(/查看关联回执/g) || []).length, 1)
    assert.equal((html.match(/关联回执不可用/g) || []).length, 1)
    assert.equal((html.match(/<pre\b/g) || []).length, 1)
    assert.equal(html.includes(observation.content), false)
    assert.match(renderRecords({ ...snapshot, observations: [] }), /尚无观察记录/)
  })

  it('观察点击仅交回绑定来源的选择和原快照，不调用网络或路由', () => {
    const originalFetch = globalThis.fetch
    let requests = 0
    let received: [InspectingActionSelection, Snapshot] | null = null
    globalThis.fetch = (async () => { requests += 1; throw new Error('不应调用网络') }) as typeof fetch
    const findClick = (node: React.ReactNode): (() => void) | undefined => {
      if (!React.isValidElement<{ onClick?: () => void; children?: React.ReactNode }>(node)) return undefined
      if (node.props.onClick) return node.props.onClick
      for (const child of React.Children.toArray(node.props.children)) {
        const click = findClick(child)
        if (click) return click
      }
      return undefined
    }
    try {
      const tree = ObservationRecords({ mission, selectedThreadId: 't', snapshot, onInspect: (chosen, source) => { received = [chosen, source] } })
      assert.equal(received, null)
      const click = findClick(tree)
      assert.ok(click)
      click()
      assert.deepEqual(received, [selection, snapshot])
      assert.equal(requests, 0)
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})

describe('OS-1 关联回执白名单摘要与显式未脱敏观察原文', () => {
  const mission: Mission = { id: 'm', title: '摘要项目', goal: '', origin: '', allowed_origins: [], created_at: '' }
  const thread: Thread = { id: 't', mission_id: 'm', title: '摘要方向', question: '', rationale: '', next_step: '', status: 'ready', priority: 1, auto_run: false, step_count: 0, created_at: '', updated_at: '' }
  const secret = 'OS1_RAW_SECRET'
  const content = `\n {"headers":{"Set-Cookie":["${secret}"]},"status_code":599,"extra":"OS1_UNKNOWN_FIELD","error":"OS1_RAW_ERROR"} \r\n`
  const action: Action = { id: 'a', thread_id: 't', status: 'succeeded', request_hash: 'OS1_HASH', decision_reason: 'OS1_DECISION', error: 'OS1_ERROR', resolution_note: 'OS1_NOTE', created_at: '', updated_at: '', input: { kind: 'http', method: 'OS1_METHOD', url: 'https://OS1_URL.invalid', reason: 'OS1_REASON', body: 'OS1_BODY', headers: { Authorization: 'OS1_AUTH' } }, result: { status_code: 204, exit_code: 39, duration_ms: 17, truncated: true, effect_unknown: false, headers: { 'Set-Cookie': ['OS1_COOKIE'] }, body_base64: 'OS1_BASE64', stdout_base64: 'OS1_STDOUT', stderr_base64: 'OS1_STDERR', resolved_ip: 'OS1_IP' } }
  const observation: Observation = { id: 'o', thread_id: 't', action_id: 'a', kind: 'action_result', content, created_at: '' }
  const snapshot: Snapshot = { mission, thread, observations: [observation], actions: [action], attempts: [] }
  const rawSelection = createRawObservationSelection(observation, mission, 't', snapshot)!
  const context: RawObservationContext = { mission, selectedThreadId: 't', snapshot, available: true }
  const renderRecords = (value = snapshot, selected: RawObservationSelection | null = null, project: Mission | null = mission, threadId: string | null = 't') => renderToStaticMarkup(React.createElement(ObservationRecords, {
    mission: project, selectedThreadId: threadId, snapshot: value, rawSelection: selected,
    onInspect: () => { throw new Error('渲染不能打开回执') },
    onToggleRaw: () => { throw new Error('渲染不能展开原文') },
  }))
  const elements = (node: React.ReactNode): React.ReactElement<Record<string, unknown>>[] => {
    if (!React.isValidElement<Record<string, unknown>>(node)) return []
    return [node, ...React.Children.toArray(node.props.children as React.ReactNode).flatMap(elements)]
  }

  it('默认HTML没有原文、未知字段及关联动作敏感值，摘要明确来自关联回执而非content', () => {
    const before = JSON.stringify(snapshot)
    const html = renderRecords()
    assert.match(html, /关联回执摘要/)
    for (const field of ['执行成功', '受控 HTTP', 'HTTP 204', '耗时 17 ms', '已截断', '查看关联回执', '展开观察原文（可能含敏感值）']) assert.ok(html.includes(field))
    assert.doesNotMatch(html, /OS1_|<pre\b|HTTP 599|退出码|未脱敏观察原文|收起观察原文/)
    assert.equal(JSON.stringify(snapshot), before)
  })

  it('白名单仅接受已知动作状态和类型，不输出任意未知原值或原型属性名', () => {
    for (const [status, text] of [['pending', '待审批'], ['approved', '已放行 · 待执行'], ['running', '执行中'], ['succeeded', '执行成功'], ['failed', '执行失败'], ['rejected', '已拒绝'], ['outcome_unknown', '效果未知']]) {
      assert.deepEqual(actionReceiptSummary({ ...action, status: status!, result: undefined }), [text, '受控 HTTP'])
    }
    for (const status of ['OS1_STATUS', '__proto__', 'constructor', 'toString', 'ready', 'executed', '']) {
      const value = { ...action, status, input: { ...action.input, kind: 'OS1_KIND' }, result: undefined } as unknown as Action
      assert.deepEqual(actionReceiptSummary(value), [])
      assert.doesNotMatch(renderRecords({ ...snapshot, actions: [value] }), /OS1_|__proto__|constructor|toString|executed/)
    }
  })

  it('HTTP状态码仅100–599整数，缺失和异常值不伪造0、不字符串化', () => {
    for (const code of [100, 200, 599]) {
      assert.deepEqual(actionReceiptSummary({ ...action, result: { status_code: code } as Action['result'] }), ['执行成功', '受控 HTTP', `HTTP ${code}`])
    }
    for (const code of [undefined, null, 0, 99, 600, -1, 204.5, NaN, Infinity, 'OS1_CODE', '200', true, {}, []]) {
      const value = { ...action, result: { status_code: code } } as unknown as Action
      assert.deepEqual(actionReceiptSummary(value), ['执行成功', '受控 HTTP'])
      assert.doesNotMatch(renderRecords({ ...snapshot, actions: [value] }), /OS1_|HTTP \d|NaN|Infinity|\[object Object\]/)
    }
  })

  it('命令只显示安全整数退出码，HTTP不显示exit_code，未知类型不猜命令', () => {
    for (const exitCode of [-1, 0, 1, 127, Number.MAX_SAFE_INTEGER]) {
      const command: Action = { ...action, input: { kind: 'local_command', command: 'OS1_COMMAND', reason: 'OS1_REASON' }, result: { exit_code: exitCode, status_code: 200 } as Action['result'] }
      assert.deepEqual(actionReceiptSummary(command), ['执行成功', '工具命令', `退出码 ${exitCode}`])
      assert.doesNotMatch(renderRecords({ ...snapshot, actions: [command] }), /OS1_|HTTP 200/)
    }
    for (const exitCode of [undefined, null, NaN, Infinity, 1.5, '0', 'OS1_EXIT', Number.MAX_SAFE_INTEGER + 1, {}, []]) {
      const command = { ...action, input: { kind: 'local_command' }, result: { exit_code: exitCode } } as unknown as Action
      assert.deepEqual(actionReceiptSummary(command), ['执行成功', '工具命令'])
    }
  })

  it('耗时只接受非负有限安全整数；缺失不是0，异常值在HTML中没有泄露', () => {
    for (const duration of [0, 1, Number.MAX_SAFE_INTEGER]) {
      assert.deepEqual(actionReceiptSummary({ ...action, result: { duration_ms: duration } as Action['result'] }), ['执行成功', '受控 HTTP', `耗时 ${duration} ms`])
    }
    for (const duration of [undefined, null, -1, 0.1, NaN, Infinity, -Infinity, Number.MAX_SAFE_INTEGER + 1, 'OS1_DURATION', '17', true, {}, []]) {
      const value = { ...action, result: { duration_ms: duration } } as unknown as Action
      assert.deepEqual(actionReceiptSummary(value), ['执行成功', '受控 HTTP'])
      assert.doesNotMatch(renderRecords({ ...snapshot, actions: [value] }), /OS1_|耗时|NaN|Infinity|\[object Object\]/)
    }
  })

  it('截断和未知效果仅精确true触发；outcome_unknown无结果仍标明且不重复', () => {
    for (const flag of [undefined, null, false, 0, 1, 'true', 'OS1_FLAG', {}, []]) {
      assert.deepEqual(actionReceiptSummary({ ...action, result: { truncated: flag, effect_unknown: flag } } as unknown as Action), ['执行成功', '受控 HTTP'])
    }
    assert.deepEqual(actionReceiptSummary({ ...action, result: { truncated: true, effect_unknown: true } as Action['result'] }), ['执行成功', '受控 HTTP', '已截断', '效果未知'])
    assert.deepEqual(actionReceiptSummary({ ...action, status: 'outcome_unknown', result: undefined }), ['效果未知', '受控 HTTP'])
    assert.deepEqual(actionReceiptSummary({ ...action, status: 'outcome_unknown', result: { effect_unknown: true } as Action['result'] }), ['效果未知', '受控 HTTP'])
    const hostile = { toString: () => { throw new Error('不应转换异常值') } }
    assert.deepEqual(actionReceiptSummary({ ...action, status: hostile, input: { kind: hostile }, result: { status_code: hostile, exit_code: hostile, duration_ms: hostile, truncated: hostile, effect_unknown: hostile } } as unknown as Action), [])
  })

  it('空、非JSON及任意JSON原文都不默认挂载，不猜关联或回退明文', () => {
    for (const text of ['', 'OS1_NOT_JSON action_id=a', 'null', '[]', '{"extra":"OS1_EXTRA","status":"OS1_RAW_STATUS"}', '<img src="https://OS1_REMOTE.invalid"><script>OS1_SCRIPT</script>']) {
      for (const actionId of [undefined, '', 'missing', 'a']) {
        const value = { ...snapshot, observations: [{ ...observation, content: text, action_id: actionId }] }
        const html = renderRecords(value)
        assert.doesNotMatch(html, /OS1_|<pre\b|<img\b|<script\b/)
        assert.match(html, /展开观察原文（可能含敏感值）/)
        if (actionId === 'a') assert.match(html, /关联回执摘要/)
        else assert.doesNotMatch(html, /关联回执摘要|查看关联回执/)
        if (actionId === 'missing') assert.match(html, /关联回执不可用/)
        if (!actionId) assert.doesNotMatch(html, /关联回执不可用/)
      }
    }
  })

  it('非法或缺失动作关联不展示摘要，但合法所属观察原文独立可读', () => {
    for (const actions of [[], [{ ...action, id: 'other' }], [{ ...action, thread_id: 'other-t' }], [action, action], [action, { ...action, thread_id: 'other-t' }]]) {
      const value = { ...snapshot, actions }
      assert.deepEqual(createRawObservationSelection(observation, mission, 't', value), rawSelection)
      assert.equal(resolveRawObservation(rawSelection, mission, 't', value), observation)
      const html = renderRecords(value)
      assert.match(html, /关联回执不可用/)
      assert.doesNotMatch(html, /OS1_|关联回执摘要|查看关联回执|<pre/)
      assert.match(renderRecords(value, rawSelection), /OS1_RAW_SECRET/)
    }
    for (const actionId of [undefined, '']) {
      const note = { ...observation, action_id: actionId }
      const value = { ...snapshot, observations: [note], actions: [] }
      const selected = createRawObservationSelection(note, mission, 't', value)
      assert.ok(selected)
      assert.equal(resolveRawObservation(selected, mission, 't', value), note)
      assert.match(renderRecords(value, selected), /OS1_RAW_SECRET/)
    }
  })

  it('仅精确action_result默认折叠，其他kind继续保留原字符串纯文本语义', () => {
    const text = '<img src="https://OS1_REMOTE.invalid"><script>bad()</script>\n{"action_id":"a"}'
    for (const kind of ['operator_note', 'model_observation', 'http_response', 'local_command', 'action_result ', 'ACTION_RESULT', 'custom']) {
      const note = { ...observation, kind, content: text }
      const value = { ...snapshot, observations: [note] }
      const html = renderRecords(value)
      assert.equal(createRawObservationSelection(note, mission, 't', value), null)
      assert.equal(resolveRawObservation(rawSelection, mission, 't', value), null)
      assert.match(html, /&lt;img src=/)
      assert.doesNotMatch(html, /<img\b|<script\b|关联回执摘要|展开观察原文/)
      const expected = renderToStaticMarkup(React.createElement('pre', null, text))
      assert.equal(/<pre\b[^>]*>([\s\S]*?)<\/pre>/.exec(html)?.[1], expected.slice(5, -6))
    }
  })

  it('显式原文pre接收API字符串逐字符不变，含空白、Unicode、HTML及超长内容，不JSON重排或截断', () => {
    for (const text of ['', content, '\n\r\n\t中文😀\u0000  é <img src="https://fixture.invalid/pixel"><script>bad()</script>', '长'.repeat(20000) + 'END']) {
      const note = { ...observation, content: text }
      const value = { ...snapshot, observations: [note] }
      const selected = createRawObservationSelection(note, mission, 't', value)
      assert.ok(selected)
      assert.equal(resolveRawObservation(selected, mission, 't', value)?.content, text)
      const tree = ObservationRecords({ mission, selectedThreadId: 't', snapshot: value, rawSelection: selected, onInspect: () => {} })
      const pre = elements(tree).find((element) => element.type === 'pre')
      assert.equal(pre?.props.children, text)
      const html = renderRecords(value, selected)
      const expected = renderToStaticMarkup(React.createElement('pre', null, text))
      assert.equal(/<pre\b[^>]*>([\s\S]*?)<\/pre>/.exec(html)?.[1], expected.slice(5, -6))
      assert.match(html, /收起观察原文/)
      assert.match(html, /未脱敏观察原文；与关联回执独立。/)
      assert.doesNotMatch(html, /<img\b|<script\b|下载|复制|编辑/)
    }
  })

  it('仅所选记录挂载原文；收起移除pre，原文不出现在key或任何DOM属性', () => {
    const other = { ...observation, id: 'o2', content: 'OS1_SECOND_SECRET' }
    const value = { ...snapshot, observations: [observation, other] }
    const selectedOther = createRawObservationSelection(other, mission, 't', value)!
    assert.doesNotMatch(renderRecords(value), /OS1_|<pre/)
    assert.match(renderRecords(value, rawSelection), /OS1_RAW_SECRET/)
    assert.doesNotMatch(renderRecords(value, rawSelection), /OS1_SECOND_SECRET/)
    assert.match(renderRecords(value, selectedOther), /OS1_SECOND_SECRET/)
    assert.doesNotMatch(renderRecords(value, selectedOther), /OS1_RAW_SECRET/)
    assert.equal((renderRecords(value, selectedOther).match(/<pre\b/g) || []).length, 1)
    assert.doesNotMatch(renderRecords(value, null), /OS1_|<pre/)
    for (const selected of [null, rawSelection]) {
      const tree = ObservationRecords({ mission, selectedThreadId: 't', snapshot: value, rawSelection: selected, onInspect: () => {} })
      for (const element of elements(tree)) {
        assert.doesNotMatch(String(element.key), /OS1_/)
        for (const [name, prop] of Object.entries(element.props)) {
          if (name !== 'children' && typeof prop === 'string') assert.doesNotMatch(prop, /OS1_/)
        }
      }
    }
  })

  it('原文同样拒绝跨项目、方向、错误快照和空上下文，不用raw入口绕过归属', () => {
    const cases: [Mission | null, string | null, Snapshot | null][] = [
      [null, 't', snapshot], [mission, null, snapshot], [mission, 't', null],
      [{ ...mission, id: 'other-m' }, 't', snapshot], [mission, 'other-t', snapshot],
      [mission, 't', { ...snapshot, mission: { ...mission, id: 'other-m' } }],
      [mission, 't', { ...snapshot, thread: { ...thread, id: 'other-t' } }],
      [mission, 't', { ...snapshot, thread: { ...thread, mission_id: 'other-m' } }],
      [mission, 't', { ...snapshot, observations: [{ ...observation, thread_id: 'other-t' }] }],
    ]
    for (const [project, threadId, value] of cases) {
      assert.equal(createRawObservationSelection(value?.observations[0] ?? observation, project, threadId, value), null)
      assert.equal(resolveRawObservation(rawSelection, project, threadId, value), null)
      if (value) assert.doesNotMatch(renderRecords(value, rawSelection, project, threadId), /OS1_|展开观察原文|收起观察原文|<pre/)
    }
  })

  it('记录消失、重复ID（含仅一条归属正确）、空ID均不可展开', () => {
    for (const observations of [[], [observation, observation], [observation, { ...observation, thread_id: 'other-t' }], [{ ...observation, id: '' }]]) {
      const value = { ...snapshot, observations }
      assert.equal(resolveRawObservation(rawSelection, mission, 't', value), null)
      for (const note of observations) assert.equal(createRawObservationSelection(note, mission, 't', value), null)
      assert.doesNotMatch(renderRecords(value, rawSelection), /OS1_|展开观察原文|收起观察原文|<pre/)
    }
    assert.equal(resolveRawObservation({ ...rawSelection, observationId: '' }, mission, 't', snapshot), null)
  })

  it('同身份同内容普通刷新可保持；观察ID/kind/action_id/content变化立即失效', () => {
    const refreshedNote = { ...observation, created_at: '2026-11-11T01:00:00Z' }
    const refreshed = { ...snapshot, observations: [{ ...observation, id: 'other' }, refreshedNote], actions: [{ ...action, status: 'failed' }] }
    assert.equal(resolveRawObservation(rawSelection, mission, 't', refreshed), refreshedNote)
    assert.equal(createRawObservationSelection(observation, mission, 't', refreshed), null)
    assert.equal(createRawObservationSelection({ ...observation }, mission, 't', snapshot), null)
    for (const changed of [{ id: 'other' }, { kind: 'operator_note' }, { action_id: 'other' }, { action_id: undefined }, { action_id: '' }, { content: content + ' ' }]) {
      assert.equal(resolveRawObservation(rawSelection, mission, 't', { ...snapshot, observations: [{ ...observation, ...changed }] }), null)
    }
    assert.equal(resolveRawObservation(null, mission, 't', snapshot), null)
  })

  it('事件代次拒绝旧快照和旧闭包；切页签/Drawer/Modal关闭后旧事件也不复用', () => {
    assert.equal(isCurrentRawObservationEvent(rawSelection, context, context), true)
    assert.equal(isCurrentRawObservationEvent(rawSelection, context, null), false)
    // 即使数据相等，刷新/切走再返回/展开再收起都是新事件上下文。
    for (const current of [{ ...context }, { ...context, snapshot: { ...snapshot } }, { ...context, available: false }, { ...context, mission: { ...mission } }]) {
      assert.equal(isCurrentRawObservationEvent(rawSelection, context, current), false)
    }
    const blocked = { ...context, available: false }
    assert.equal(isCurrentRawObservationEvent(rawSelection, blocked, blocked), false)
    const returned = { ...context }
    assert.equal(isCurrentRawObservationEvent(rawSelection, blocked, returned), false)
    assert.equal(isCurrentRawObservationEvent(rawSelection, returned, returned), true)
    const changed = { ...context, snapshot: { ...snapshot, observations: [{ ...observation, content: content + 'new' }] } }
    assert.equal(isCurrentRawObservationEvent(rawSelection, changed, changed), false)
    const invalid = { ...context, selectedThreadId: 'other-t' }
    assert.equal(isCurrentRawObservationEvent(rawSelection, invalid, invalid), false)
  })

  it('原文按钮只交回所选记录，渲染和点击不请求网络或改变源数据', () => {
    const originalFetch = globalThis.fetch
    let requests = 0
    let received: RawObservationSelection | null = null
    const before = JSON.stringify(snapshot)
    globalThis.fetch = (async () => { requests += 1; throw new Error('不应调用网络') }) as typeof fetch
    try {
      const tree = ObservationRecords({ mission, selectedThreadId: 't', snapshot, onInspect: () => { throw new Error('不应打开Drawer') }, onToggleRaw: (selection) => { received = selection } })
      const button = elements(tree).find((element) => element.props.children === '展开观察原文（可能含敏感值）')
      assert.ok(button)
      assert.equal(received, null)
      assert.equal(button.props['aria-expanded'], false)
      ;(button.props.onClick as () => void)()
      assert.deepEqual(received, rawSelection)
      assert.equal(requests, 0)
      assert.equal(JSON.stringify(snapshot), before)
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})

describe('路由驱动的创建 Modal', () => {
  it('深链无需来源历史，关闭替换到项目列表，畸形编码不崩溃', () => {
    const location = readRouteLocation('/projects/new', null)
    assert.equal(parsePathname(location.pathname).kind, 'create')
    assert.equal(contentPathname(location), '/projects')
    assert.deepEqual(createCloseDestination(location.create), { back: false, pathname: '/projects' })
    assert.deepEqual(parsePathname('/projects/%E0%A4%A'), { kind: 'projects' })
    assert.deepEqual(parsePathname('/projects/m%201/threads/t%201'), { kind: 'workspace', missionId: 'm 1', threadId: 't 1' })
  })

  it('从来源工作面打开，关闭用历史后退，前进仍还原同一来源', () => {
    const source = '/projects/m-1/threads/t-2'
    const context = nextCreateContext(source, defaultCreateContext, false)
    assert.deepEqual(context, { returnPath: source, hasBackEntry: true })
    const state = { wscyberCreate: context }
    assert.equal(contentPathname(readRouteLocation('/projects/new', state)), source)
    assert.deepEqual(createCloseDestination(context), { back: true })
    assert.equal(contentPathname(readRouteLocation(source, null)), source)
    assert.deepEqual(readRouteLocation('/projects/new', state).create, context)
    assert.deepEqual(nextCreateContext('/projects/new', context, false), context)
  })

  it('401替换登录后仍保留来源，首次登录深链及显式退出使用安全默认', () => {
    const context = nextCreateContext('/projects/m-2', defaultCreateContext, false)
    const afterLogin = nextCreateContext('/login', context, true)
    assert.deepEqual(afterLogin, context)
    assert.equal(contentPathname(readRouteLocation('/projects/new', { wscyberCreate: afterLogin })), '/projects/m-2')
    assert.deepEqual(nextCreateContext('/login', defaultCreateContext, true), defaultCreateContext)
  })

  it('history来源只接受内部工作面，不携带草稿、令牌或外部跳转', () => {
    for (const returnPath of ['https://example.com', '//example.com', '/login', '/projects/new', '/projects/m?token=secret', '/projects/m#secret']) {
      assert.deepEqual(readRouteLocation('/projects/new', { wscyberCreate: { returnPath, hasBackEntry: true } }).create, defaultCreateContext)
    }
    const context = nextCreateContext('/projects', defaultCreateContext, false)
    assert.deepEqual(Object.keys(context).sort(), ['hasBackEntry', 'returnPath'])
  })

  it('在途、失联、404、503、401状态关闭重开仍使用同一键和冻结内容', () => {
    const payload = canonicalizeExplorationInput('原目标', '原目标结果', ['https://target.example.com'])
    const submitting = beginSubmission(initialSubmissionState, payload)
    const states = [submitting, handlePostFailure(submitting, 0, '断线'), handleOperationCheck404(submitting), handlePostFailure(submitting, 503, '不可用'), handleSessionUnauthorizedDuringSubmission(submitting)]
    for (const state of states) {
      const frozen = JSON.stringify(state)
      const context = nextCreateContext('/projects/m-1', defaultCreateContext, false)
      createCloseDestination(context)
      readRouteLocation('/projects/new', { wscyberCreate: context })
      assert.equal(JSON.stringify(state), frozen)
      assert.equal(state.operationKey, submitting.operationKey)
      assert.deepEqual(state.frozenPayload, payload)
      assert.equal(isSubmissionLocked(state), true)
    }
  })
})

function encodedBytes(bytes: Uint8Array): string {
  return btoa(Array.from(bytes, (byte) => String.fromCharCode(byte)).join(''))
}
function encodedText(value: string): string { return encodedBytes(new TextEncoder().encode(value)) }
function availableEvidence(value: string, charset?: string) {
  const decoded = decodeEvidenceBytes(value, charset)
  assert.ok(decoded.status === 'available')
  return decoded
}

describe('EC-1 已保存字节与明确预览复制', () => {
  it('所有字节原样解码并构造 octet-stream 附件，不从文本/预览重建', async () => {
    const original = Uint8Array.from({ length: 256 }, (_, index) => index)
    const decoded = availableEvidence(encodedBytes(original))
    assert.deepEqual(decoded.bytes, original)
    const blob = createEvidenceBlob(decoded.bytes)
    assert.equal(blob.type, 'application/octet-stream')
    assert.deepEqual(new Uint8Array(await blob.arrayBuffer()), original)
    assert.equal(createEvidenceBlob(new Uint8Array()).size, 0)
    for (const filename of ['../../秘密.html', '\\r\\n/bad\\\\file.bin', '', '..', 'a'.repeat(300)]) {
      const safe = safeEvidenceFilename(filename)
      assert.match(safe, /^[a-zA-Z0-9_-][a-zA-Z0-9._-]*$/)
      assert.ok(safe.length <= 160)
    }
  })

  it('缺失不等于零字节，显式空字符串才是已知空内容', () => {
    assert.deepEqual(decodeEvidenceBytes(undefined), { status: 'missing' })
    const empty = availableEvidence('')
    assert.equal(empty.bytes.length, 0)
    assert.equal(empty.text, '')
    assert.equal(createEvidencePreview(empty, 'auto').text, '')
  })

  it('严格拒绝非标准 Base64、非法 padding/尾位和 atob 容忍的空白或短编码', () => {
    for (const invalid of ['Zg', 'Zg=', 'Zg===', 'Zg==\n', ' Zg==', 'AA?=', '-_8=', 'AB==', 'AAB=', '=AAA', 'A===', 'A', '====']) {
      const decoded = decodeEvidenceBytes(invalid)
      assert.equal(decoded.status, 'invalid', invalid)
      assert.equal('bytes' in decoded, false)
    }
    assert.equal(availableEvidence('Zg==').text, 'f')
    assert.equal(availableEvidence('Zm8=').text, 'fo')
    assert.equal(availableEvidence('Zm9v').text, 'foo')
  })

  it('文本/二进制/非法与未知编码分开，BOM处理只属于文本视图', () => {
    assert.equal(availableEvidence(encodedText('中文 😀')).text, '中文 😀')
    assert.equal(availableEvidence('6Q==', 'windows-1252').text, 'é')
    for (const [value, charset] of [['AA==', 'utf-8'], ['/w==', 'utf-8'], ['YQ==', 'x-unknown'], ['YQ==', 'utf-16le']]) {
      const decoded = availableEvidence(value!, charset)
      assert.equal(decoded.text, null)
      assert.equal(createEvidencePreview(decoded, 'text').mode, 'hex')
      assert.equal(createEvidencePreview(decoded, 'base64').text, value)
    }
    const bom = availableEvidence('77u/YQ==')
    assert.equal(bom.text, 'a')
    assert.deepEqual(Array.from(bom.bytes), [239, 187, 191, 97])
  })

  it('文本按码点截取16384字符，不切半个代理对，边界不误报截断', () => {
    const full = 'A'.repeat(EVIDENCE_PREVIEW_LIMIT - 1) + '😀尾部'
    const preview = createEvidencePreview(availableEvidence(encodedText(full)), 'text')
    assert.equal(preview.text, full.slice(0, -2))
    assert.equal(Array.from(preview.text).length, EVIDENCE_PREVIEW_LIMIT)
    assert.equal(preview.text.endsWith('😀'), true)
    assert.equal(preview.limited, true)
    assert.equal(preview.unit, '字符')
    assert.equal(preview.copyLabel, '复制当前文本预览')
    assert.equal(createEvidencePreview(availableEvidence(encodedText(preview.text)), 'text').limited, false)
  })

  it('hex上限以源字节计，Base64上限以字符计，不改变下载字节', async () => {
    const bytes = Uint8Array.from({ length: EVIDENCE_PREVIEW_LIMIT + 1 }, (_, index) => index % 256)
    const decoded = availableEvidence(encodedBytes(bytes))
    const hex = createEvidencePreview(decoded, 'hex')
    assert.equal(hex.unit, '源字节')
    assert.equal(hex.text.split('\n').length, EVIDENCE_PREVIEW_LIMIT / 16)
    assert.match(hex.text, /00003ff0  f0 f1 f2 f3 f4 f5 f6 f7 f8 f9 fa fb fc fd fe ff$/)
    assert.equal(hex.limited, true)
    const base64 = createEvidencePreview(decoded, 'base64')
    assert.equal(base64.text, encodedBytes(bytes).slice(0, EVIDENCE_PREVIEW_LIMIT))
    assert.equal(base64.limited, true)
    assert.equal(base64.unit, '字符')
    assert.deepEqual(new Uint8Array(await createEvidenceBlob(decoded.bytes).arrayBuffer()), bytes)
  })

  it('SSR将HTML只作文本，pre内容和复制语义来自同一个当前预览', () => {
    const text = '<img src="https://untrusted.invalid/pixel">\n<script>bad()</script>'
    const html = renderToStaticMarkup(React.createElement(EvidenceBytesViewer, { sourceKey: 'm:t:a:body', base64Value: encodedText(text), label: '正文', filename: 'a.bin' }))
    const expected = renderToStaticMarkup(React.createElement('pre', null, createEvidencePreview(availableEvidence(encodedText(text)), 'auto').text))
    const pre = /<pre\b[^>]*>([\s\S]*?)<\/pre>/.exec(html)?.[1]
    assert.equal(pre, expected.slice(5, -6))
    assert.match(html, /aria-label="复制当前文本预览"/)
    assert.match(html, /下载已保存字节/)
    assert.doesNotMatch(html, /<img src=|<script>/)
  })

  it('SSR缺失/非法不伪造0字节、空附件或复制，显式空允许真实空附件', () => {
    const render = (base64Value?: string) => renderToStaticMarkup(React.createElement(EvidenceBytesViewer, { sourceKey: 'a', base64Value, label: '正文', filename: 'a.bin' }))
    assert.match(render(), /未提供已保存字节；可能为空，无法确定长度/)
    assert.match(render('AA?='), /Base64 数据无效/)
    for (const html of [render(), render('AA?=')]) assert.doesNotMatch(html, /0 字节|下载已保存字节|复制当前|<pre/)
    assert.match(render(''), /已保存内容为空（0 字节）/)
    assert.match(render(''), /下载已保存字节/)
    assert.match(render(''), /aria-label="复制当前文本预览"/)
  })

  it('SSR长预览明确单位与局限，不把预览截取称为捕获截断', () => {
    const html = renderToStaticMarkup(React.createElement(EvidenceBytesViewer, { sourceKey: 'long', base64Value: encodedText('A'.repeat(EVIDENCE_PREVIEW_LIMIT + 1)), label: '正文', filename: 'a.bin' }))
    assert.equal(/<pre\b[^>]*>([\s\S]*?)<\/pre>/.exec(html)?.[1]?.length, EVIDENCE_PREVIEW_LIMIT)
    assert.match(html, /16,384 个字符/)
    assert.match(html, /复制仅含当前预览，下载包含全部已保存字节/)
    assert.doesNotMatch(html, /证据已被截断/)
  })

  it('共享查看器以非敏感sourceKey重挂载，不以材料字符串为key', () => {
    const props = { label: '正文', filename: 'a.bin', base64Value: encodedText('相同内容') }
    const first = EvidenceBytesViewer({ ...props, sourceKey: 'm:t:a:body' })
    const next = EvidenceBytesViewer({ ...props, sourceKey: 'm:t:b:body' })
    assert.equal(first.key, 'm:t:a:body')
    assert.notEqual(first.key, next.key)
    assert.notEqual(first.key, props.base64Value)
  })
})

describe('EC-1 正式检查器身份与可见材料', () => {
  const action: Action = { id: 'a', thread_id: 't', status: 'outcome_unknown', request_hash: 'hash', decision_reason: '', error: '', created_at: '', updated_at: '', input: { kind: 'http', method: 'POST', url: 'https://example.invalid', reason: '本地fixture', headers: { Authorization: 'Bearer request-secret', 'X-Visible': 'public' }, body: '<script>请求正文</script>' }, result: { headers: { 'Set-Cookie': ['response-secret'] }, body_base64: encodedText('<img src="https://untrusted.invalid/pixel">'), exit_code: 0, truncated: true, effect_unknown: true, duration_ms: 1 } }
  const mission: Mission = { id: 'm', title: '项目', goal: '', origin: '', allowed_origins: [], created_at: '' }
  const thread: Thread = { id: 't', mission_id: 'm', title: '方向', question: '', status: 'ready', priority: 1, auto_run: false, step_count: 0, next_step: '', rationale: '', created_at: '', updated_at: '' }
  const child = (value: Action | null = action, project = mission, direction = thread) => ActionEvidenceDrawer({ action: value, mission: project, thread: direction, onClose: () => {} }).props.children as React.ReactElement | null

  it('项目/方向/动作任一变化重挂载敏感子树；关闭立即移除而不是等待effect', () => {
    const first = child()!
    assert.notEqual(first.key, child(action, { ...mission, id: 'm2' })!.key)
    assert.notEqual(first.key, child(action, mission, { ...thread, id: 't2' })!.key)
    assert.notEqual(first.key, child({ ...action, id: 'a2' })!.key)
    assert.equal(child(null), null)
    assert.doesNotMatch(String(first.key), /request-secret|response-secret/)
  })

  it('初始可复制头仅为遮蔽输出，HTML和请求正文只作文本，保留unknown与捕获截断', () => {
    const html = renderToStaticMarkup(child()!)
    assert.doesNotMatch(html, /request-secret|response-secret|<script>|<img src=/)
    assert.match(html, /Authorization: \[已隐藏\]/)
    assert.match(html, /Set-Cookie: \[已隐藏\]/)
    assert.match(html, /X-Visible: public/)
    for (const label of ['复制请求头', '复制响应头', '复制请求正文', '复制当前文本预览']) assert.ok(html.includes(`aria-label="${label}"`))
    assert.match(html, /&lt;script&gt;请求正文&lt;\/script&gt;/)
    assert.match(html, /证据已被截断/)
    assert.match(html, /外部效果未知，不代表未执行；禁止自动重放/)
  })

  it('result缺失不是空响应；命令缺失流分别如实展示', () => {
    const missing = renderToStaticMarkup(child({ ...action, result: undefined })!)
    assert.match(missing, /尚无已保存的执行回执/)
    assert.doesNotMatch(missing, /响应正文|复制当前响应头|下载已保存字节/)
    const command = renderToStaticMarkup(child({ ...action, input: { kind: 'local_command', command: 'fixture', reason: '' }, result: { exit_code: 0, duration_ms: 1, truncated: false, effect_unknown: false, stdout_base64: '' } })!)
    assert.match(command, /标准输出 \(stdout\)/)
    assert.match(command, /标准错误 \(stderr\)/)
    assert.match(command, /已保存内容为空（0 字节）/)
    assert.match(command, /未提供已保存字节；可能为空，无法确定长度/)
    assert.equal((command.match(/下载已保存字节/g) || []).length, 1)
  })
})

describe('DR-1 动作决策说明不冒充放行依据', () => {
  const mission: Mission = { id: 'm-dr', title: 'DR-1项目', goal: '核对已有决策说明', origin: '受控fixture', allowed_origins: [], created_at: '' }
  const thread: Thread = { id: 't-dr', mission_id: mission.id, title: 'DR-1方向', question: '', status: 'ready', priority: 1, auto_run: false, step_count: 0, next_step: '', rationale: '', created_at: '', updated_at: '' }
  const action: Action = { id: 'a-dr', thread_id: thread.id, status: 'pending', request_hash: 'dr-hash', decision_reason: '', error: '', created_at: '', updated_at: '', input: { kind: 'http', method: 'DELETE', url: 'https://fixture.invalid/item', reason: '核对动作展示，不发出请求' } }
  const render = (value: Action) => {
    const snapshot: Snapshot = { mission, thread, actions: [value], observations: [], attempts: [] }
    const before = JSON.stringify(snapshot)
    const list = renderToStaticMarkup(React.createElement(ProjectWorkspaceView, {
      mission, threads: [thread], selectedThreadId: thread.id, snapshot,
      loadingMission: false, loadingSnapshot: false, error: '', threadError: '', acceptedBanner: '',
      onDismissAcceptedBanner: () => {}, onBackToList: () => {}, onRefresh: () => {}, onSelectThread: () => {},
    }))
    // 渲染真实Drawer材料子树；不把SSR不挂载portal当作抽屉内容覆盖。
    const child = ActionEvidenceDrawer({ action: value, mission, thread, onClose: () => {} }).props.children as React.ReactElement
    const drawer = renderToStaticMarkup(child)
    assert.equal(JSON.stringify(snapshot), before)
    return { list, drawer }
  }

  for (const [status, reason, badge] of [
    ['pending', 'request-signals-v1：命中DELETE，等待人工决定', '待审批'],
    ['rejected', '人工拒绝：不执行该请求', '已拒绝'],
    ['approved', '策略或人工允许；原文不作为来源证明', '已放行 · 待执行'],
  ]) {
    it(`${status}在列表和Drawer均显示中性标签及原理由，不改真实状态`, () => {
      const { list, drawer } = render({ ...action, status, decision_reason: reason })
      assert.match(list, /决策说明：/)
      assert.match(drawer, /申请目的与决策说明/)
      assert.equal((drawer.match(/决策说明/g) || []).length, 2)
      for (const html of [list, drawer]) {
        assert.ok(html.includes(reason))
        assert.ok(html.includes(badge))
        assert.doesNotMatch(html, /放行依据|人工已批准/)
      }
      if (status === 'rejected') assert.match(drawer, /此请求已拒绝，无执行回执/)
      if (status === 'approved') assert.match(drawer, /已放行，等待后台认领执行/)
      if (status !== 'approved') assert.doesNotMatch(drawer, /已放行，等待后台认领执行/)
      assert.doesNotMatch(drawer, /下载已保存字节/)
    })
  }

  it('运行、成功、失败和未知状态保留已有理由，不根据状态改写其含义', () => {
    for (const status of ['running', 'succeeded', 'failed', 'outcome_unknown']) {
      const { list, drawer } = render({ ...action, status, decision_reason: 'DR1_原有理由完整保留' })
      for (const html of [list, drawer]) {
        assert.match(html, /决策说明/)
        assert.match(html, /DR1_原有理由完整保留/)
        assert.doesNotMatch(html, /放行依据/)
      }
      if (status === 'outcome_unknown') assert.match(drawer, /外部效果未知，不代表未执行；禁止自动重放/)
    }
  })

  it('空理由不补虚构原因，列表无该字段、Drawer仅保留原有卡片标题', () => {
    const { list, drawer } = render(action)
    assert.doesNotMatch(list, /决策说明：|放行依据/)
    assert.match(drawer, /申请目的与决策说明/)
    assert.equal((drawer.match(/决策说明/g) || []).length, 1)
    assert.doesNotMatch(drawer, /放行依据/)
  })

  it('理由按纯文本保留HTML与来源措辞，渲染不修改输入或发网络请求', () => {
    const previousFetch = globalThis.fetch
    let requests = 0
    globalThis.fetch = async () => { requests += 1; throw new Error('不得发网络请求') }
    try {
      const reason = 'DR1_TEXT <img src="https://must-not-load.invalid/dr1"> & 不从此原文推断人工或策略来源'
      const { list, drawer } = render({ ...action, status: 'rejected', decision_reason: reason })
      for (const html of [list, drawer]) {
        assert.match(html, /DR1_TEXT &lt;img src=&quot;https:\/\/must-not-load\.invalid\/dr1&quot;&gt; &amp; 不从此原文推断人工或策略来源/)
        assert.doesNotMatch(html, /<img|<script|<iframe/)
      }
      assert.equal(requests, 0)
    } finally { globalThis.fetch = previousFetch }
  })
})

describe('React component rendering & field labels', () => {
  it('Modal重挂载只展示原草稿，固定底栏明确关联表单且渲染不发送POST', () => {
    const draft = { description: '关闭后保留 <script>bad()</script>', goal: '保持证据', originsText: 'https://target.example.com' }
    let writes = 0
    const form = () => renderToStaticMarkup(React.createElement(ExplorationCreateView, {
      systemInfo: null, draft, submissionState: initialSubmissionState,
      onChangeDraft: () => {}, onSubmitExploration: async () => { writes += 1 },
    }))
    assert.equal(form(), form())
    assert.match(form(), /id="exploration-create-form"/)
    assert.match(form(), /&lt;script&gt;/)
    const actions = renderToStaticMarkup(React.createElement(ExplorationCreateActions, {
      systemInfo: null, submissionState: initialSubmissionState, hasDraftContent: true,
      onClearDraft: () => {}, onClose: () => {}, onVerifyOperation: async () => {}, onRetrySameKey: async () => { writes += 1 },
    }))
    assert.match(actions, /form="exploration-create-form"/)
    assert.match(actions, /暂时关闭/)
    assert.equal(writes, 0)
  })

  it('提交/核对进行中底栏禁止重复网络动作，待核对只提供显式原键重试', () => {
    const submitting = beginSubmission(initialSubmissionState, canonicalizeExplorationInput('目标', '期望', ['https://target.example.com']))
    let writes = 0
    for (const phase of ['submitting', 'verifying', 'unverified'] as const) {
      const html = renderToStaticMarkup(React.createElement(ExplorationCreateActions, {
        systemInfo: null, submissionState: { ...submitting, phase }, hasDraftContent: true,
        onClearDraft: () => { throw new Error('不应清空在途草稿') }, onClose: () => {},
        onVerifyOperation: async () => { writes += 1 }, onRetrySameKey: async () => { writes += 1 },
      }))
      assert.doesNotMatch(html, /开始探索|清空草稿/)
      assert.match(html, /核对提交状态/)
      assert.match(html, /使用原提交键重试/)
      assert.equal((html.match(/disabled=""/g) || []).length, phase === 'unverified' ? 0 : 2)
    }
    assert.equal(writes, 0)
  })

  it('renders streamlined LoginView with stable label and reauth notice', () => {
    const html = renderToStaticMarkup(
      React.createElement(LoginView, {
        reauthNotice: '访问令牌已失效',
        hasPreservedDraftOrKey: true,
        onLogin: async () => {},
      }),
    )
    assert.match(html, /访问令牌/)
    assert.match(html, /id="auth-token-input"/)
    assert.match(html, /进入工作台/)
    assert.match(html, /当前内存草稿与待核对提交已保留/)
  })

  it('renders ExplorationCreateView with stable labels, origin preview and model state, without persistent helper copy', () => {
    const html = renderToStaticMarkup(
      React.createElement(ExplorationCreateView, {
        systemInfo: {
          name: 'wscyber',
          version: '0.1.0-dev',
          model_configured: false,
          model: '',
          sandbox_image: 'wscyber/runtime:local',
          sandbox_runtime: 'runc',
          network_mode: 'bridge（HTTP 动作风险分流，工具直连未拦截）',
          max_parallel: 2,
        },
        draft: {
          description: '测试网关接口',
          goal: '验证未授权访问',
          originsText: 'HTTPS://Target.Example.com:443',
        },
        submissionState: initialSubmissionState,
        onChangeDraft: () => {},
        onSubmitExploration: async () => {},
      }),
    )
    assert.match(html, /id="exploration-description"/)
    assert.match(html, /目标／已知信息/)
    assert.match(html, /id="exploration-goal"/)
    assert.match(html, /希望达到的目标/)
    assert.match(html, /id="exploration-origins"/)
    assert.match(html, /授权范围（精确 HTTP\/HTTPS Origin）/)
    assert.match(html, /https:\/\/target\.example\.com/)
    assert.doesNotMatch(html, /平台 HTTP 强制校验|工具容器 bridge 直连|exploration-origin-boundary/)
    assert.match(html, /模型未配置，当前不可启动新的探索任务/)
  })
})
