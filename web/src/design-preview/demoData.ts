import type {
  DemoFinding,
  DemoGraphEdge,
  DemoGraphNode,
  DemoModelCall,
  DemoObservation,
  DemoProject,
  DemoReportVersion,
  DemoRetestRecord,
  DemoThread,
  DemoTimelineEvent,
  DemoTrafficRequest,
  PendingDecisionItem,
} from './types.ts'

export interface InitialDemoDataset {
  projects: DemoProject[]
  threads: DemoThread[]
  observations: DemoObservation[]
  traffic: DemoTrafficRequest[]
  modelCalls: DemoModelCall[]
  graphNodes: DemoGraphNode[]
  graphEdges: DemoGraphEdge[]
  findings: DemoFinding[]
  timelineEvents: DemoTimelineEvent[]
  retests: DemoRetestRecord[]
  reports: DemoReportVersion[]
  pendingDecisions: PendingDecisionItem[]
}

export function createInitialDemoDataset(): InitialDemoDataset {
  const projects: DemoProject[] = [
    {
      id: 'PRJ-2026-01',
      name: '商城多租户订单与结算接口研究',
      targetAndKnownInfo:
        '商户订单中心 https://api.shop.example.com 与统一鉴权网关 https://auth.shop.example.com；已知包含租户买家订单查询、电子发票导出及售后退款冲正流水接口。',
      desiredObjective:
        '验证多租户买家在订单查询、发票导出及售后退款状态流转中的水平越权与幂等性边界。',
      allowedOrigins: [
        'https://api.shop.example.com',
        'https://auth.shop.example.com',
      ],
      submissionStatus: '已接收',
      executionStatus: '运行中',
      currentFocusSummary:
        '方向 THR-101 已完成双身份对比实证（F-2026-001 高危越权、F-2026-002 低危异常栈泄露、F-2026-003 正常权限功能排除）；方向 THR-102 存在连接重置未知请求 REQ-205 待核对。',
      createdAt: '2026-11-11 09:15',
      identities: [
        {
          id: 'id-buyer-a',
          label: '测试租户A买家 (id-buyer-a)',
          roleSummary: '租户 tenant_alpha 普通买家账号',
          credentialType: 'Bearer Token + Cookie',
          maskedPreview: 'Authorization: [已隐藏] / Cookie: [已隐藏]',
          status: '有效（演示）',
        },
        {
          id: 'id-buyer-b',
          label: '测试租户B买家 (id-buyer-b)',
          roleSummary: '租户 tenant_beta 普通买家账号（用于双身份越权对比）',
          credentialType: 'Bearer Token + Cookie',
          maskedPreview: 'Authorization: [已隐藏] / Cookie: [已隐藏]',
          status: '有效（演示）',
        },
      ],
      stepLimitPerThread: 20,
      concurrencyLimit: 2,
    },
    {
      id: 'PRJ-2026-02',
      name: '合作伙伴开放网关 Webhook 与签名校验研究',
      targetAndKnownInfo:
        '开放平台沙箱网关 https://partner.api.example.org；提供商户回调地址注册、事件验签与账单拉取接口。',
      desiredObjective:
        '核查回调目标地址校验边界、签名时间戳重放窗口及跨商户账单凭据隔离。',
      allowedOrigins: ['https://partner.api.example.org'],
      submissionStatus: '已接收',
      executionStatus: '运行中',
      currentFocusSummary:
        '方向 THR-201 正在对比不同时间戳偏移下的签名重放响应（REQ-208 / LLM-504）。',
      createdAt: '2026-11-11 10:05',
      identities: [
        {
          id: 'id-partner-sandbox',
          label: '合作商户沙箱密钥 (id-partner-sandbox)',
          roleSummary: '开放平台测试商户 API Key',
          credentialType: 'X-API-Key Header',
          maskedPreview: 'X-API-Key: [已隐藏]',
          status: '有效（演示）',
        },
      ],
      stepLimitPerThread: 20,
      concurrencyLimit: 1,
    },
    {
      id: 'PRJ-2026-03',
      name: '清结算批处理服务管理接口评估',
      targetAndKnownInfo:
        '结算测试环境 https://settle.sandbox.example.net；提供只读对账单查询与结算批次状态查询。',
      desiredObjective: '检查结算批次查询接口的租户参数隔离与异常输入回显。',
      allowedOrigins: ['https://settle.sandbox.example.net'],
      submissionStatus: '已接收',
      executionStatus: '已结束',
      currentFocusSummary:
        '初始研究方向 THR-301 已完成预设步数并正常收束（不代表已证明目标绝对安全）。',
      createdAt: '2026-11-10 16:40',
      identities: [],
      stepLimitPerThread: 15,
      concurrencyLimit: 1,
    },
  ]

  const threads: DemoThread[] = [
    {
      id: 'THR-101',
      projectId: 'PRJ-2026-01',
      title: '跨租户订单详情与发票导出越权核验',
      executionStatus: '运行中',
      questionAndBasis:
        '对比测试租户A买家（id-buyer-a）与测试租户B买家（id-buyer-b）访问同一订单 /v2/orders/ORD-90021 及其发票导出接口时，服务端是否校验订单所属 tenant_id。已由 REQ-201、REQ-202、REQ-203 获得跨租户实证，并由独立验证记录（LLM-502）确认 F-2026-001 成立。',
      nextStepPlan:
        '继续核查订单子资源（物流拆单与发票冲红查询）是否共享同一权限校验缺陷。',
      blockingReason: null,
      blockingRequestId: null,
      executedSteps: 6,
      maxSteps: 20,
      pendingApprovedActionsOnResume: ['ACT-303: 继续核查订单物流拆单子接口'],
      updatedAt: '2026-11-11 09:42',
    },
    {
      id: 'THR-102',
      projectId: 'PRJ-2026-01',
      title: '售后退款冲正接口状态机与幂等核验',
      executionStatus: '待核对',
      questionAndBasis:
        '验证售后退款单 /v2/refunds/RF-8801 在确认冲正阶段是否具备幂等锁；工具动作 ACT-304 发送请求 REQ-205 后连接被对端重置，尚未收到 HTTP 响应码。',
      nextStepPlan:
        '遵守恢复不变量：发送效果未知的外部请求 REQ-205 禁止自动重放。需先通过只读查询或人工核对记录确认目标状态，再决定后续步骤。',
      blockingReason:
        '请求 REQ-205 发送字节已写出但连接中断（发送效果未知），平台已停止自动重放，等待人工核对记录。',
      blockingRequestId: 'REQ-205',
      executedSteps: 4,
      maxSteps: 20,
      pendingApprovedActionsOnResume: [],
      updatedAt: '2026-11-11 09:38',
    },
    {
      id: 'THR-103',
      projectId: 'PRJ-2026-01',
      title: '响应内联内部管理域名越界访问核查',
      executionStatus: '已结束',
      questionAndBasis:
        '订单接口调试响应头曾出现内部地址 https://admin.internal.example.org/v1/system/config；模型尝试探测该地址时被平台出口范围边界直接拦截未发送（REQ-206）。',
      nextStepPlan:
        '目标 origin 不在项目明确授权范围 allowed_origins 内，平台不发送越界请求，本方向收束结束。',
      blockingReason: null,
      blockingRequestId: null,
      executedSteps: 2,
      maxSteps: 20,
      pendingApprovedActionsOnResume: [],
      updatedAt: '2026-11-11 09:26',
    },
    {
      id: 'THR-201',
      projectId: 'PRJ-2026-02',
      title: 'Webhook 验签时间戳窗口与重放边界验证',
      executionStatus: '运行中',
      questionAndBasis:
        '测试 /v1/webhooks/verify 对超过 300 秒时间戳与相同 nonce 的验签响应差异（已记录 REQ-208 与模型调用 LLM-504）。',
      nextStepPlan: '继续采样 600 秒偏移与重复事件 ID 下的服务端拒绝行为。',
      blockingReason: null,
      blockingRequestId: null,
      executedSteps: 3,
      maxSteps: 20,
      pendingApprovedActionsOnResume: [],
      updatedAt: '2026-11-11 10:18',
    },
    {
      id: 'THR-301',
      projectId: 'PRJ-2026-03',
      title: '结算批次只读查询接口租户过滤检查',
      executionStatus: '已结束',
      questionAndBasis:
        '已完成 15 步只读参数对比，所有跨租户批次号查询均返回 403 且无敏感字段回显。',
      nextStepPlan: '步数达到方向上限且无新增异常观察，方向已结束。',
      blockingReason: null,
      blockingRequestId: null,
      executedSteps: 15,
      maxSteps: 15,
      pendingApprovedActionsOnResume: [],
      updatedAt: '2026-11-10 17:30',
    },
  ]

  const observations: DemoObservation[] = [
    {
      id: 'OBS-401',
      projectId: 'PRJ-2026-01',
      threadId: 'THR-101',
      sourceType: '工具实证',
      title: '双身份订单详情对比确认跨租户读取（REQ-201 vs REQ-202）',
      summary:
        '使用 id-buyer-a 读取自有订单 ORD-90021 返回 200；切换为跨租户身份 id-buyer-b 请求同一 URL（REQ-202）仍返回 200 及完整收货人与税号字节，响应体与原件一致。',
      linkedRequestId: 'REQ-202',
      linkedModelCallId: 'LLM-501',
      linkedFindingId: 'F-2026-001',
      recordedAt: '2026-11-11 09:22',
    },
    {
      id: 'OBS-402',
      projectId: 'PRJ-2026-01',
      threadId: 'THR-101',
      sourceType: '工具实证',
      title: '跨租户电子发票导出与畸形参数分表栈回显（REQ-203 / REQ-204）',
      summary:
        'REQ-203 证实跨租户可导出发票（高危 F-2026-001）；REQ-204 传入畸形 format 参数触发服务端返回内部表名与 SQL 绑定栈（低危但真实成立 F-2026-002，低危不等于误报）。',
      linkedRequestId: 'REQ-203',
      linkedModelCallId: 'LLM-502',
      linkedFindingId: 'F-2026-001',
      recordedAt: '2026-11-11 09:29',
    },
    {
      id: 'OBS-403',
      projectId: 'PRJ-2026-01',
      threadId: 'THR-101',
      sourceType: '模型观察',
      title: '自有历史归档订单导出核查（已排除：属正常权限内功能 F-2026-003）',
      summary:
        '探索阶段曾怀疑买家导出自身历史归档订单接口存在越权，但经独立验证核对（REQ-201），仅返回当前登录租户自身数据，属正常权限功能，不冒充漏洞。',
      linkedRequestId: 'REQ-201',
      linkedModelCallId: 'LLM-502',
      linkedFindingId: 'F-2026-003',
      recordedAt: '2026-11-11 09:34',
    },
    {
      id: 'OBS-404',
      projectId: 'PRJ-2026-01',
      threadId: 'THR-102',
      sourceType: '工具实证',
      title: '退款冲正确认请求连接中断（REQ-205 发送效果未知）',
      summary:
        '动作 ACT-304 发送 POST /v2/refunds/RF-8801/confirm 后遭遇 TCP RST，平台标记发送结果为“发送效果未知”并禁止自动重放。',
      linkedRequestId: 'REQ-205',
      linkedFindingId: 'F-2026-004',
      recordedAt: '2026-11-11 09:38',
    },
  ]

  const traffic: DemoTrafficRequest[] = [
    {
      id: 'REQ-201',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验',
      toolActionId: 'ACT-301',
      usageStage: '探索采样',
      verificationPurpose: '获取租户 A 自有订单 ORD-90021 的基线响应结构与租户归属字段',
      method: 'GET',
      url: 'https://api.shop.example.com/v2/orders/ORD-90021',
      normalizedOrigin: 'https://api.shop.example.com',
      identityId: 'id-buyer-a',
      identityLabel: '测试租户A买家 (id-buyer-a)',
      sendStatus: '已有响应',
      statusCode: 200,
      statusText: 'OK',
      capturedAt: '2026-11-11 09:20:11',
      captureSource: '平台受控 HTTP 出口记录 · 重建报文展示',
      truncated: false,
      truncationNote: null,
      scopeValidity: '在项目明确授权范围 https://api.shop.example.com 内',
      immutableFingerprint: 'sha256:8a41f0c9e2174a01b92d31f80019c201a7e29001',
      requestHeaders: {
        Accept: 'application/json',
        Authorization:
          'Bearer demo_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.tenantA_buyer_fiction',
        Cookie: 'shop_session=demo_cookie_tenant_a_99102_fiction',
        'X-Request-Trace': 'trace-req-201',
      },
      requestBody: '',
      responseHeaders: {
        'Content-Type': 'application/json; charset=utf-8',
        'X-Tenant-Scope': 'tenant_alpha',
        'Set-Cookie': 'shop_session=demo_cookie_tenant_a_refresh_fiction; HttpOnly',
      },
      responseBody: JSON.stringify(
        {
          order_id: 'ORD-90021',
          owner_tenant: 'tenant_alpha',
          buyer_account: 'buyer_a@example.com',
          tax_identifier: '91310000DEMO0001X',
          total_cents: 128000,
          status: 'PAID',
        },
        null,
        2,
      ),
      unknownOutcomeNote: null,
      reconciliationNote: null,
      linkedFindingIds: ['F-2026-001', 'F-2026-003'],
      linkedModelCallId: 'LLM-501',
    },
    {
      id: 'REQ-202',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验',
      toolActionId: 'ACT-301',
      usageStage: '探索采样',
      verificationPurpose: '切换测试租户B买家身份访问租户A订单 ORD-90021，验证水平越权控制',
      method: 'GET',
      url: 'https://api.shop.example.com/v2/orders/ORD-90021',
      normalizedOrigin: 'https://api.shop.example.com',
      identityId: 'id-buyer-b',
      identityLabel: '测试租户B买家 (id-buyer-b)',
      sendStatus: '已有响应',
      statusCode: 200,
      statusText: 'OK',
      capturedAt: '2026-11-11 09:21:45',
      captureSource: '平台受控 HTTP 出口记录 · 重建报文展示',
      truncated: false,
      truncationNote: null,
      scopeValidity: '在项目明确授权范围 https://api.shop.example.com 内',
      immutableFingerprint: 'sha256:3c92b77d19044e88a1209f8341c09982b1d42002',
      requestHeaders: {
        Accept: 'application/json',
        Authorization:
          'Bearer demo_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.tenantB_buyer_fiction',
        Cookie: 'shop_session=demo_cookie_tenant_b_55319_fiction',
        'X-Request-Trace': 'trace-req-202',
      },
      requestBody: '',
      responseHeaders: {
        'Content-Type': 'application/json; charset=utf-8',
        'X-Tenant-Scope': 'tenant_alpha',
      },
      responseBody: JSON.stringify(
        {
          order_id: 'ORD-90021',
          owner_tenant: 'tenant_alpha',
          buyer_account: 'buyer_a@example.com',
          tax_identifier: '91310000DEMO0001X',
          total_cents: 128000,
          status: 'PAID',
        },
        null,
        2,
      ),
      unknownOutcomeNote: null,
      reconciliationNote: null,
      linkedFindingIds: ['F-2026-001'],
      linkedModelCallId: 'LLM-501',
    },
    {
      id: 'REQ-203',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验',
      toolActionId: 'ACT-302',
      usageStage: '独立验证',
      verificationPurpose: '验证测试租户B买家能否触发租户A订单 ORD-90021 的电子发票导出记录生成',
      method: 'POST',
      url: 'https://api.shop.example.com/v2/orders/ORD-90021/invoice-export',
      normalizedOrigin: 'https://api.shop.example.com',
      identityId: 'id-buyer-b',
      identityLabel: '测试租户B买家 (id-buyer-b)',
      sendStatus: '已有响应',
      statusCode: 200,
      statusText: 'OK',
      capturedAt: '2026-11-11 09:28:50',
      captureSource: '平台受控 HTTP 出口记录 · 重建报文展示',
      truncated: true,
      truncationNote:
        '捕获截断说明：响应正文超过单次保存上限（64 KB），平台已保存前 64 KB 原始字节并记录完整 SHA-256 指纹；界面预览展示核心字段。',
      scopeValidity: '在项目明确授权范围 https://api.shop.example.com 内',
      immutableFingerprint: 'sha256:9f01e442c8110b72d519a003812f6610c4a82003',
      requestHeaders: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        Authorization:
          'Bearer demo_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.tenantB_buyer_fiction',
      },
      requestBody: JSON.stringify(
        {
          format: 'json_summary',
          include_tax_detail: true,
        },
        null,
        2,
      ),
      responseHeaders: {
        'Content-Type': 'application/json; charset=utf-8',
        'X-Capture-Truncated': 'true; saved_bytes=65536',
      },
      responseBody: JSON.stringify(
        {
          invoice_serial: 'INV-20261111-0089',
          order_id: 'ORD-90021',
          owner_tenant: 'tenant_alpha',
          exported_by_actor: 'tenant_beta:buyer_b',
          buyer_tax_no: '91310000DEMO0001X',
          download_preview_url:
            'https://api.shop.example.com/v2/invoices/INV-20261111-0089/preview',
          _capture_notice: '后续明细行已在 65536 字节边界截断，原件指纹已固定',
        },
        null,
        2,
      ),
      unknownOutcomeNote: null,
      reconciliationNote: null,
      linkedFindingIds: ['F-2026-001'],
      linkedModelCallId: 'LLM-502',
    },
    {
      id: 'REQ-204',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验',
      toolActionId: 'ACT-303',
      usageStage: '独立验证',
      verificationPurpose:
        '验证发票导出接口对未知 format 枚举值的异常处理，确认是否存在内部表结构与栈信息泄露（F-2026-002 低危实证）',
      method: 'POST',
      url: 'https://api.shop.example.com/v2/orders/ORD-90021/invoice-export',
      normalizedOrigin: 'https://api.shop.example.com',
      identityId: 'id-buyer-a',
      identityLabel: '测试租户A买家 (id-buyer-a)',
      sendStatus: '已有响应',
      statusCode: 500,
      statusText: 'Internal Server Error',
      capturedAt: '2026-11-11 09:31:10',
      captureSource: '平台受控 HTTP 出口记录 · 重建报文展示',
      truncated: false,
      truncationNote: null,
      scopeValidity: '在项目明确授权范围 https://api.shop.example.com 内',
      immutableFingerprint: 'sha256:4d77a901e332c618b004219f773e1108e9b12004',
      requestHeaders: {
        'Content-Type': 'application/json',
        Authorization:
          'Bearer demo_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.tenantA_buyer_fiction',
      },
      requestBody: JSON.stringify(
        {
          format: ' INVALID_FORMAT_PROBE ',
          include_tax_detail: true,
        },
        null,
        2,
      ),
      responseHeaders: {
        'Content-Type': 'application/json; charset=utf-8',
      },
      responseBody: JSON.stringify(
        {
          error: 'UNHANDLED_RENDERER_EXCEPTION',
          internal_table: 't_order_invoice_shard_04',
          sql_fragment:
            'SELECT inv_blob FROM t_order_invoice_shard_04 WHERE order_id = $1',
          trace: 'at InvoiceExportService.render (/srv/shop-order/invoice.go:184)',
        },
        null,
        2,
      ),
      unknownOutcomeNote: null,
      reconciliationNote: null,
      linkedFindingIds: ['F-2026-002'],
      linkedModelCallId: 'LLM-502',
    },
    {
      id: 'REQ-205',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-102',
      threadTitle: '售后退款冲正接口状态机与幂等核验',
      toolActionId: 'ACT-304',
      usageStage: '探索采样',
      verificationPurpose: '验证退款单 RF-8801 确认冲正接口在并发/异常下的状态转换',
      method: 'POST',
      url: 'https://api.shop.example.com/v2/refunds/RF-8801/confirm',
      normalizedOrigin: 'https://api.shop.example.com',
      identityId: 'id-buyer-a',
      identityLabel: '测试租户A买家 (id-buyer-a)',
      sendStatus: '发送效果未知',
      statusCode: null,
      statusText: '连接重置（目标实际处理效果未知）',
      capturedAt: '2026-11-11 09:37:52',
      captureSource: '平台受控 HTTP 出口记录 · 发送异常记录',
      truncated: false,
      truncationNote: null,
      scopeValidity: '在项目明确授权范围 https://api.shop.example.com 内',
      immutableFingerprint: 'sha256:6e12c883a440f912d7710c23901a4421f8c52005',
      requestHeaders: {
        'Content-Type': 'application/json',
        Authorization:
          'Bearer demo_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.tenantA_buyer_fiction',
        Cookie: 'shop_session=demo_cookie_tenant_a_99102_fiction',
      },
      requestBody: JSON.stringify(
        {
          refund_id: 'RF-8801',
          confirm_action: 'COMMIT_REVERSAL',
          client_seq: 1,
        },
        null,
        2,
      ),
      responseHeaders: null,
      responseBody: null,
      unknownOutcomeNote:
        '请求报文已写入传输层套接字，但在等待响应头时收到 TCP Connection Reset by Peer。平台无法断言目标服务端是否已完成冲正写入，严格禁止自动重放。',
      reconciliationNote: null,
      linkedFindingIds: ['F-2026-004'],
    },
    {
      id: 'REQ-206',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-103',
      threadTitle: '响应内联内部管理域名越界访问核查',
      toolActionId: 'ACT-305',
      usageStage: '探索采样',
      verificationPurpose: '尝试探测响应头中出现的内部管理域 https://admin.internal.example.org',
      method: 'GET',
      url: 'https://admin.internal.example.org/v1/system/config',
      normalizedOrigin: 'https://admin.internal.example.org',
      identityId: 'id-buyer-a',
      identityLabel: '测试租户A买家 (id-buyer-a)',
      sendStatus: '越界未发送',
      statusCode: null,
      statusText: '超出授权范围未发送（零字节出站）',
      capturedAt: '2026-11-11 09:25:14',
      captureSource: '平台授权范围校验 · 越界拦截记录',
      truncated: false,
      truncationNote: null,
      scopeValidity:
        '越出项目明确授权范围 [https://api.shop.example.com, https://auth.shop.example.com]；目标响应不能扩大授权，未向目标发送。',
      immutableFingerprint: 'sha256:0b88d112f553e701a9928c41200f3319a1d72006',
      requestHeaders: {
        Accept: 'application/json',
        Authorization:
          'Bearer demo_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.tenantA_buyer_fiction',
      },
      requestBody: '',
      responseHeaders: null,
      responseBody: null,
      unknownOutcomeNote: null,
      reconciliationNote: null,
      linkedFindingIds: [],
    },
    {
      id: 'REQ-207',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验（复测 RT-2026-001-1）',
      toolActionId: 'ACT-RT-01',
      usageStage: '单漏洞复测',
      verificationPurpose:
        '单漏洞复测 RT-2026-001-1：沿用原复现逻辑验证修复补丁上线后跨租户读取 ORD-90021 是否被拦截',
      method: 'GET',
      url: 'https://api.shop.example.com/v2/orders/ORD-90021',
      normalizedOrigin: 'https://api.shop.example.com',
      identityId: 'id-buyer-b',
      identityLabel: '测试租户B买家 (id-buyer-b)',
      sendStatus: '已有响应',
      statusCode: 403,
      statusText: 'Forbidden',
      capturedAt: '2026-11-11 10:30:05',
      captureSource: '单漏洞复测任务 RT-2026-001-1 · 重建报文展示',
      truncated: false,
      truncationNote: null,
      scopeValidity: '在项目明确授权范围 https://api.shop.example.com 内',
      immutableFingerprint: 'sha256:7f33c109b882d415e6601a90432b8812d0e92007',
      requestHeaders: {
        Accept: 'application/json',
        Authorization:
          'Bearer demo_eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.tenantB_buyer_fiction',
        'X-Retest-Task': 'RT-2026-001-1',
      },
      requestBody: '',
      responseHeaders: {
        'Content-Type': 'application/json; charset=utf-8',
        'X-Tenant-Check': 'enforced',
      },
      responseBody: JSON.stringify(
        {
          error_code: 'TENANT_OWNERSHIP_MISMATCH',
          message: '当前登录租户 tenant_beta 无权访问归属 tenant_alpha 的订单资源',
          request_id: 'req-retest-207',
        },
        null,
        2,
      ),
      unknownOutcomeNote: null,
      reconciliationNote: null,
      linkedFindingIds: ['F-2026-001'],
      linkedModelCallId: 'LLM-503',
    },
    {
      id: 'REQ-208',
      projectId: 'PRJ-2026-02',
      projectName: '合作伙伴开放网关 Webhook 与签名校验研究',
      threadId: 'THR-201',
      threadTitle: 'Webhook 验签时间戳窗口与重放边界验证',
      toolActionId: 'ACT-201',
      usageStage: '探索采样',
      verificationPurpose: '验证开放平台 Webhook 验签接口对过期 450 秒时间戳的处理行为',
      method: 'POST',
      url: 'https://partner.api.example.org/v1/webhooks/verify',
      normalizedOrigin: 'https://partner.api.example.org',
      identityId: 'id-partner-sandbox',
      identityLabel: '合作商户沙箱密钥 (id-partner-sandbox)',
      sendStatus: '已有响应',
      statusCode: 200,
      statusText: 'OK',
      capturedAt: '2026-11-11 10:16:20',
      captureSource: '平台受控 HTTP 出口记录 · 重建报文展示',
      truncated: false,
      truncationNote: null,
      scopeValidity: '在项目 PRJ-2026-02 明确授权范围 https://partner.api.example.org 内',
      immutableFingerprint: 'sha256:5a90e221d774c310f8812b60541c7703e2f12008',
      requestHeaders: {
        'Content-Type': 'application/json',
        'X-API-Key': 'demo_partner_key_77102_sandbox_fiction',
        'X-Webhook-Timestamp': '1762826900',
        'X-Webhook-Signature': 'sha256=demo_sig_882190a_fiction',
      },
      requestBody: JSON.stringify(
        {
          event_id: 'evt_demo_9910',
          event_type: 'settlement.completed',
          merchant_id: 'mch_sandbox_01',
        },
        null,
        2,
      ),
      responseHeaders: {
        'Content-Type': 'application/json; charset=utf-8',
      },
      responseBody: JSON.stringify(
        {
          verified: true,
          accepted_skew_seconds: 450,
          warning: 'timestamp skew exceeded recommended 300s window',
        },
        null,
        2,
      ),
      unknownOutcomeNote: null,
      reconciliationNote: null,
      linkedFindingIds: [],
      linkedModelCallId: 'LLM-504',
    },
  ]

  const modelCalls: Omit<DemoModelCall, 'recordingEnabled'>[] = [
    {
      id: 'LLM-501',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验',
      purpose: '探索',
      turnLabel: '方向 THR-101 · 第 2 轮调用 (Attempt #2)',
      sequenceCursor: 'cursor:llm-seq-1001',
      modelName: 'claude-sonnet-5-5（演示记录 · 不触发真实调用）',
      durationMs: 2420,
      status: '已完成',
      recordedAt: '2026-11-11 09:21:10',
      contextIsolationNote:
        '探索上下文：读取已保存的租户 A 基线响应 REQ-201，规划租户 B 同资源对比请求 REQ-202。',
      inputSummary:
        '输入：授权范围 [https://api.shop.example.com]、身份列表 [id-buyer-a, id-buyer-b] 及基线响应 REQ-201（owner_tenant=tenant_alpha）。',
      outputSummary:
        '返回：建议保持 URL /v2/orders/ORD-90021 不变，切换身份为 id-buyer-b 发起只读 GET 对照请求（生成 ACT-301 / REQ-202）。',
      actualInputText: JSON.stringify(
        {
          system_role: 'authorized_web_api_research_loop',
          mission_scope: ['https://api.shop.example.com', 'https://auth.shop.example.com'],
          thread_id: 'THR-101',
          turn_index: 2,
          observed_evidence: [
            {
              request_id: 'REQ-201',
              identity: 'id-buyer-a',
              status_code: 200,
              extracted_fields: {
                order_id: 'ORD-90021',
                owner_tenant: 'tenant_alpha',
                tax_identifier: '91310000DEMO0001X',
              },
            },
          ],
          available_identities: ['id-buyer-a', 'id-buyer-b'],
        },
        null,
        2,
      ),
      actualOutputText: JSON.stringify(
        {
          hypothesis:
            'GET /v2/orders/ORD-90021 返回了 tenant_alpha 的私有税号字段，需用跨租户身份 id-buyer-b 请求同一资源核验服务端是否绑定 tenant_id。',
          proposed_tool_action: {
            action_id: 'ACT-301',
            method: 'GET',
            url: 'https://api.shop.example.com/v2/orders/ORD-90021',
            identity_id: 'id-buyer-b',
          },
          candidate_finding_draft: null,
        },
        null,
        2,
      ),
      linkedRequestIds: ['REQ-201', 'REQ-202'],
      linkedFindingId: 'F-2026-001',
    },
    {
      id: 'LLM-502',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验',
      purpose: '验证',
      turnLabel: '漏洞验证记录 · 第 1 轮调用 (Verify #1)',
      sequenceCursor: 'cursor:llm-seq-1002',
      modelName: 'claude-sonnet-5-5（演示记录 · 不触发真实调用）',
      durationMs: 3180,
      status: '已完成',
      recordedAt: '2026-11-11 09:31:45',
      contextIsolationNote:
        '主稿推荐（建议 · 待确认）：使用独立验证执行上下文，仅输入候选复现步骤与实证报文指纹，不继承探索阶段长对话历史，不是已批准的固定多专家组织。',
      inputSummary:
        '输入：候选漏洞 F-2026-001、F-2026-002、F-2026-003 的复现步骤及实证流量 REQ-201~REQ-204。',
      outputSummary:
        '返回：1) F-2026-001 验证成立（高危）；2) F-2026-002 验证成立（低危信息泄露，低危不等于误报）；3) F-2026-003 验证不成立（属租户自身正常权限功能，非漏洞）。',
      actualInputText: JSON.stringify(
        {
          purpose: 'INDEPENDENT_FINDING_VERIFICATION_DEMO',
          candidates: [
            {
              finding_id: 'F-2026-001',
              claim: '跨租户读取订单详情与导出电子发票',
              evidence_requests: ['REQ-201', 'REQ-202', 'REQ-203'],
            },
            {
              finding_id: 'F-2026-002',
              claim: '畸形 format 参数触发内部表名与代码行号回显',
              evidence_requests: ['REQ-204'],
            },
            {
              finding_id: 'F-2026-003',
              claim: '买家导出自身历史归档订单接口返回订单摘要',
              evidence_requests: ['REQ-201'],
            },
          ],
        },
        null,
        2,
      ),
      actualOutputText: JSON.stringify(
        {
          verdicts: [
            {
              finding_id: 'F-2026-001',
              verification_status: 'VERIFIED_TRUE',
              severity: 'HIGH',
              reason:
                'REQ-201 证明 ORD-90021 归属 tenant_alpha，REQ-202 与 REQ-203 证明 tenant_beta 买家可读取并导出该订单发票，实证闭环。',
            },
            {
              finding_id: 'F-2026-002',
              verification_status: 'VERIFIED_TRUE',
              severity: 'LOW',
              reason:
                'REQ-204 稳定返回 HTTP 500 及内部表名 t_order_invoice_shard_04 与 SQL 片段。验证成立与严重度分开：虽危害为低危，但实证成立，不归为误报。',
            },
            {
              finding_id: 'F-2026-003',
              verification_status: 'VERIFIED_FALSE_NORMAL_FEATURE',
              severity: 'NONE',
              reason:
                'id-buyer-a 读取自身归档订单属业务授权范围内的正常功能，未跨越租户或角色边界，不能冒充漏洞。',
            },
          ],
        },
        null,
        2,
      ),
      linkedRequestIds: ['REQ-201', 'REQ-202', 'REQ-203', 'REQ-204'],
      linkedFindingId: 'F-2026-001',
    },
    {
      id: 'LLM-503',
      projectId: 'PRJ-2026-01',
      projectName: '商城多租户订单与结算接口研究',
      threadId: 'THR-101',
      threadTitle: '跨租户订单详情与发票导出越权核验（复测 RT-2026-001-1）',
      purpose: '复测',
      turnLabel: '单漏洞复测 RT-2026-001-1 · 第 1 轮调用',
      sequenceCursor: 'cursor:llm-seq-1003',
      modelName: 'claude-sonnet-5-5（演示记录 · 不触发真实调用）',
      durationMs: 1950,
      status: '已完成',
      recordedAt: '2026-11-11 10:30:12',
      contextIsolationNote:
        '复测上下文：沿用 F-2026-001 (rev-2) 原复现步骤与身份 id-buyer-b，对比新采集流量 REQ-207（403 Forbidden），不覆盖原证据。',
      inputSummary:
        '输入：原漏洞版本 F-2026-001 (rev-2) 复现步骤、原证据 REQ-202（200 OK）及本次复测新流量 REQ-207（403 Forbidden）。',
      outputSummary:
        '返回：判定结论为“本次范围修复已验证”，生成独立复测报告 RPT-RT-001-1。',
      actualInputText: JSON.stringify(
        {
          purpose: 'SINGLE_FINDING_RETEST_EVALUATION',
          retest_id: 'RT-2026-001-1',
          bound_finding: { id: 'F-2026-001', revision: 'rev-2' },
          original_evidence: { request_id: 'REQ-202', status_code: 200 },
          new_retest_evidence: {
            request_id: 'REQ-207',
            status_code: 403,
            error_code: 'TENANT_OWNERSHIP_MISMATCH',
          },
        },
        null,
        2,
      ),
      actualOutputText: JSON.stringify(
        {
          retest_conclusion: '本次范围修复已验证',
          preserve_original_evidence: true,
          report_summary:
            '相同跨租户身份 id-buyer-b 请求 GET /v2/orders/ORD-90021 已返回 403 TENANT_OWNERSHIP_MISMATCH，原证据 REQ-201~REQ-203 保持不变。',
        },
        null,
        2,
      ),
      linkedRequestIds: ['REQ-202', 'REQ-207'],
      linkedFindingId: 'F-2026-001',
    },
    {
      id: 'LLM-504',
      projectId: 'PRJ-2026-02',
      projectName: '合作伙伴开放网关 Webhook 与签名校验研究',
      threadId: 'THR-201',
      threadTitle: 'Webhook 验签时间戳窗口与重放边界验证',
      purpose: '探索',
      turnLabel: '方向 THR-201 · 第 1 轮调用 (Attempt #1)',
      sequenceCursor: 'cursor:llm-seq-1004',
      modelName: 'claude-sonnet-5-5（演示记录 · 不触发真实调用）',
      durationMs: 2110,
      status: '已完成',
      recordedAt: '2026-11-11 10:16:30',
      contextIsolationNote:
        '项目 PRJ-2026-02 独立探索上下文：分析 REQ-208 验签响应中的 accepted_skew_seconds=450。',
      inputSummary:
        '输入：项目 PRJ-2026-02 范围 [https://partner.api.example.org] 与流量记录 REQ-208。',
      outputSummary:
        '返回：记录服务端允许 450 秒时间戳偏移的观察，下一步构造相同 event_id 的二次请求验证是否具备去重表。',
      actualInputText: JSON.stringify(
        {
          project_id: 'PRJ-2026-02',
          thread_id: 'THR-201',
          observed_request: 'REQ-208',
          response_excerpt: { verified: true, accepted_skew_seconds: 450 },
        },
        null,
        2,
      ),
      actualOutputText: JSON.stringify(
        {
          observation: '450s 时间戳偏移仍通过验签，需结合 nonce/event_id 去重检查才能判断是否存在重放风险。',
          next_step: '使用相同 event_id=evt_demo_9910 发送第二次请求，验证服务端是否拒绝重复事件。',
        },
        null,
        2,
      ),
      linkedRequestIds: ['REQ-208'],
    },
  ]

  const graphNodes: DemoGraphNode[] = [
    {
      id: 'THR-101',
      projectId: 'PRJ-2026-01',
      kind: 'thread',
      kindLabel: '研究方向',
      code: 'THR-101',
      title: '跨租户订单与发票越权核验',
      statusLabel: '运行中',
      summary: '长寿命研究问题：核验订单详情與电子发票导出接口的租户归属隔离。',
      col: 0,
      row: 0,
      targetTab: 'research',
      targetObjectId: 'THR-101',
    },
    {
      id: 'THR-102',
      projectId: 'PRJ-2026-01',
      kind: 'thread',
      kindLabel: '研究方向',
      code: 'THR-102',
      title: '退款冲正状态机与幂等核验',
      statusLabel: '待核对',
      summary: '长寿命研究问题：核验售后退款冲正确认接口的状态机与连接异常恢复。',
      col: 0,
      row: 1,
      targetTab: 'research',
      targetObjectId: 'THR-102',
    },
    {
      id: 'ACT-301',
      projectId: 'PRJ-2026-01',
      kind: 'activity',
      kindLabel: '工具活动',
      code: 'ACT-301',
      title: '双身份订单读取对比',
      statusLabel: '已完成',
      summary: '分别使用 id-buyer-a 与 id-buyer-b 请求同一订单详情接口。',
      col: 1,
      row: 0,
      targetTab: 'traffic',
      targetObjectId: 'REQ-202',
    },
    {
      id: 'ACT-302',
      projectId: 'PRJ-2026-01',
      kind: 'activity',
      kindLabel: '工具活动',
      code: 'ACT-302',
      title: '跨租户电子发票导出',
      statusLabel: '已完成',
      summary: '使用 id-buyer-b 调用订单 ORD-90021 的发票导出动作。',
      col: 1,
      row: 1,
      targetTab: 'traffic',
      targetObjectId: 'REQ-203',
    },
    {
      id: 'ACT-303',
      projectId: 'PRJ-2026-01',
      kind: 'activity',
      kindLabel: '工具活动',
      code: 'ACT-303',
      title: '畸形导出参数异常栈探测',
      statusLabel: '已完成',
      summary: '验证发票导出参数异常处理，捕获内部表名泄露响应 REQ-204。',
      col: 1,
      row: 2,
      targetTab: 'traffic',
      targetObjectId: 'REQ-204',
    },
    {
      id: 'ACT-304',
      projectId: 'PRJ-2026-01',
      kind: 'activity',
      kindLabel: '工具活动',
      code: 'ACT-304',
      title: '退款冲正提交动作',
      statusLabel: '发送效果未知',
      summary: '请求写出后遭遇 TCP 连接重置，禁止自动重放。',
      col: 1,
      row: 3,
      targetTab: 'traffic',
      targetObjectId: 'REQ-205',
    },
    {
      id: 'REQ-201',
      projectId: 'PRJ-2026-01',
      kind: 'evidence',
      kindLabel: '流量证据',
      code: 'REQ-201',
      title: 'GET /v2/orders/ORD-90021 (租户A基线)',
      statusLabel: '200 OK',
      summary: '租户 A 读取自身订单返回 200 OK，提供字段对比基线。',
      col: 2,
      row: 0,
      targetTab: 'traffic',
      targetObjectId: 'REQ-201',
    },
    {
      id: 'REQ-202',
      projectId: 'PRJ-2026-01',
      kind: 'evidence',
      kindLabel: '流量证据',
      code: 'REQ-202',
      title: 'GET /v2/orders/ORD-90021 (租户B越权)',
      statusLabel: '200 OK',
      summary: '租户 B 读取租户 A 订单返回 200 OK 及完整税号信息。',
      col: 2,
      row: 1,
      targetTab: 'traffic',
      targetObjectId: 'REQ-202',
    },
    {
      id: 'REQ-203',
      projectId: 'PRJ-2026-01',
      kind: 'evidence',
      kindLabel: '流量证据',
      code: 'REQ-203',
      title: 'POST .../invoice-export (租户B导出)',
      statusLabel: '200 OK (含截断说明)',
      summary: '租户 B 成功触发租户 A 订单的发票导出并获得预览凭据。',
      col: 2,
      row: 2,
      targetTab: 'traffic',
      targetObjectId: 'REQ-203',
    },
    {
      id: 'REQ-204',
      projectId: 'PRJ-2026-01',
      kind: 'evidence',
      kindLabel: '流量证据',
      code: 'REQ-204',
      title: 'POST .../invoice-export (畸形format)',
      statusLabel: '500 栈与分表泄露',
      summary: '返回内部表名 t_order_invoice_shard_04 与 SQL 片段。',
      col: 2,
      row: 3,
      targetTab: 'traffic',
      targetObjectId: 'REQ-204',
    },
    {
      id: 'REQ-205',
      projectId: 'PRJ-2026-01',
      kind: 'evidence',
      kindLabel: '流量证据',
      code: 'REQ-205',
      title: 'POST /v2/refunds/RF-8801/confirm',
      statusLabel: '发送效果未知',
      summary: '连接重置导致目标是否入账未知，等待核对记录。',
      col: 2,
      row: 4,
      targetTab: 'traffic',
      targetObjectId: 'REQ-205',
    },
    {
      id: 'F-2026-001',
      projectId: 'PRJ-2026-01',
      kind: 'finding',
      kindLabel: '漏洞记录',
      code: 'F-2026-001',
      title: '订单详情与发票导出跨租户水平越权',
      statusLabel: '验证成立 · 高危',
      summary: '由 REQ-201、REQ-202、REQ-203 双身份实证确认；含独立复测记录 RT-2026-001-1。',
      col: 3,
      row: 0,
      targetTab: 'findings',
      targetObjectId: 'F-2026-001',
    },
    {
      id: 'F-2026-002',
      projectId: 'PRJ-2026-01',
      kind: 'finding',
      kindLabel: '漏洞记录',
      code: 'F-2026-002',
      title: '发票导出异常回显内部数据库分表名与代码行',
      statusLabel: '验证成立 · 低危',
      summary: '由 REQ-204 实证确认：验证成立与严重度分开，低危不等于误报。',
      col: 3,
      row: 1,
      targetTab: 'findings',
      targetObjectId: 'F-2026-002',
    },
  ]

  const graphEdges: DemoGraphEdge[] = [
    {
      id: 'EDG-01',
      projectId: 'PRJ-2026-01',
      fromId: 'THR-101',
      toId: 'ACT-301',
      relationLabel: '派发工具活动',
      evidenceBasis: '方向 THR-101 双身份基线对比任务（关联模型调用 LLM-501）',
    },
    {
      id: 'EDG-02',
      projectId: 'PRJ-2026-01',
      fromId: 'THR-101',
      toId: 'ACT-302',
      relationLabel: '派发工具活动',
      evidenceBasis: '方向 THR-101 发票导出验证任务',
    },
    {
      id: 'EDG-03',
      projectId: 'PRJ-2026-01',
      fromId: 'THR-101',
      toId: 'ACT-303',
      relationLabel: '派发工具活动',
      evidenceBasis: '方向 THR-101 异常格式参数探测任务',
    },
    {
      id: 'EDG-04',
      projectId: 'PRJ-2026-01',
      fromId: 'THR-102',
      toId: 'ACT-304',
      relationLabel: '派发工具活动',
      evidenceBasis: '方向 THR-102 退款冲正验证任务',
    },
    {
      id: 'EDG-05',
      projectId: 'PRJ-2026-01',
      fromId: 'ACT-301',
      toId: 'REQ-201',
      relationLabel: '产生出站流量',
      evidenceBasis: '动作 ACT-301 第 1 条请求（id-buyer-a 基线）',
    },
    {
      id: 'EDG-06',
      projectId: 'PRJ-2026-01',
      fromId: 'ACT-301',
      toId: 'REQ-202',
      relationLabel: '产生出站流量',
      evidenceBasis: '动作 ACT-301 第 2 条请求（id-buyer-b 跨租户读取）',
    },
    {
      id: 'EDG-07',
      projectId: 'PRJ-2026-01',
      fromId: 'ACT-302',
      toId: 'REQ-203',
      relationLabel: '产生出站流量',
      evidenceBasis: '动作 ACT-302 出站请求（id-buyer-b 发票导出）',
    },
    {
      id: 'EDG-08',
      projectId: 'PRJ-2026-01',
      fromId: 'ACT-303',
      toId: 'REQ-204',
      relationLabel: '产生出站流量',
      evidenceBasis: '动作 ACT-303 出站请求（触发 500 分表名与栈泄露）',
    },
    {
      id: 'EDG-09',
      projectId: 'PRJ-2026-01',
      fromId: 'ACT-304',
      toId: 'REQ-205',
      relationLabel: '产生未知结果请求',
      evidenceBasis: '字节发出后 TCP RST，目标状态待核对',
    },
    {
      id: 'EDG-10',
      projectId: 'PRJ-2026-01',
      fromId: 'REQ-201',
      toId: 'F-2026-001',
      relationLabel: '提供归属基线证据',
      evidenceBasis: '证明订单 ORD-90021 真实归属于 tenant_alpha',
    },
    {
      id: 'EDG-11',
      projectId: 'PRJ-2026-01',
      fromId: 'REQ-202',
      toId: 'F-2026-001',
      relationLabel: '提供跨租户实证',
      evidenceBasis: '证明 tenant_beta 买家可越权读取 ORD-90021 敏感字段',
    },
    {
      id: 'EDG-12',
      projectId: 'PRJ-2026-01',
      fromId: 'REQ-203',
      toId: 'F-2026-001',
      relationLabel: '提供跨租户实证',
      evidenceBasis: '证明 tenant_beta 买家可越权导出 ORD-90021 电子发票',
    },
    {
      id: 'EDG-13',
      projectId: 'PRJ-2026-01',
      fromId: 'REQ-204',
      toId: 'F-2026-002',
      relationLabel: '提供实证响应',
      evidenceBasis: '证明畸形参数稳定触发内部表名与 SQL 片段泄露（低危但成立）',
    },
  ]

  const findings: DemoFinding[] = [
    {
      id: 'F-2026-001',
      projectId: 'PRJ-2026-01',
      title: '订单详情与发票导出接口缺失租户归属校验导致跨租户水平越权',
      verificationStatus: '验证成立',
      severityLabel: '高危',
      severityBasis:
        '高危 · 任意已登录租户买家可跨租户读取他人订单详情、纳税人识别号并导出电子发票（验证成立与严重度分开评估）',
      remediationStatus: '声称已修复（已有复测结论）',
      affectedTarget:
        'https://api.shop.example.com/v2/orders/:orderId 及 /v2/orders/:orderId/invoice-export',
      currentRevision: 'rev-2（固定双身份流量实证版）',
      threadId: 'THR-101',
      descriptionPrincipleAndHarm:
        '订单中心服务在处理 /v2/orders/:orderId 与 /v2/orders/:orderId/invoice-export 请求时，仅验证了请求方持有合法买家 JWT，未将令牌中的 tenant_id 与订单记录的 owner_tenant 进行绑定比对。攻击者使用任意普通租户买家身份（如 tenant_beta），即可通过订单号读取其他租户（tenant_alpha）的收货账户、税号及金额，并触发电子发票导出。',
      reproductionSteps: [
        {
          stepNumber: 1,
          instruction:
            '使用租户 A 买家身份（id-buyer-a）请求自有订单详情，确认订单 ORD-90021 归属 owner_tenant 为 tenant_alpha。',
          identityPrerequisite: '测试租户A买家 (id-buyer-a · tenant_alpha)',
          linkedRequestId: 'REQ-201',
          expectedObservation:
            '返回 HTTP 200，响应体 owner_tenant 为 tenant_alpha，含完整纳税人识别号。',
        },
        {
          stepNumber: 2,
          instruction:
            '保持目标 URL 不变，切换为租户 B 买家身份（id-buyer-b）发送 GET /v2/orders/ORD-90021。',
          identityPrerequisite: '测试租户B买家 (id-buyer-b · tenant_beta)',
          linkedRequestId: 'REQ-202',
          expectedObservation:
            '服务端未拦截跨租户访问，仍返回 HTTP 200 及属于 tenant_alpha 的完整订单数据。',
        },
        {
          stepNumber: 3,
          instruction:
            '继续使用租户 B 买家身份（id-buyer-b）调用 POST /v2/orders/ORD-90021/invoice-export 请求导出电子发票。',
          identityPrerequisite: '测试租户B买家 (id-buyer-b · tenant_beta)',
          linkedRequestId: 'REQ-203',
          expectedObservation:
            '服务端返回 HTTP 200 并生成发票流水 INV-20261111-0089 及预览地址。',
        },
      ],
      remediationAdvice:
        '1. 在订单查询与发票导出服务层强制从认证上下文提取当前会话的 tenant_id，并在数据访问层查询条件中附加 WHERE order_id = :orderId AND owner_tenant = :currentTenantId；2. 对跨租户访问统一返回 HTTP 403 Forbidden（错误码 TENANT_OWNERSHIP_MISMATCH），避免泄露目标订单是否存在；3. 将双身份跨租户回归用例纳入发版复测流水线。',
      verificationRecords: [
        {
          id: 'VER-001',
          findingId: 'F-2026-001',
          executedAt: '2026-11-11 09:31:45',
          executionContextNote:
            '主稿推荐（建议 · 待确认）：使用独立验证执行上下文复核 REQ-201~REQ-203，不继承探索对话偏见；非固定多专家组织已获批。',
          verdictLabel: '验证成立',
          severityIndependenceNote:
            '验证成立（由双身份对比流量证实跨租户越权）与严重度（高危：涉及敏感财务税号与发票导出）分别独立记录。',
          evidenceRequestIds: ['REQ-201', 'REQ-202', 'REQ-203'],
          modelCallId: 'LLM-502',
          summary:
            '自有身份 REQ-201 与跨租户身份 REQ-202、REQ-203 形成完整实证闭环，排除公开资源可能性，验证成立。',
        },
      ],
      updatedAt: '2026-11-11 09:32',
    },
    {
      id: 'F-2026-002',
      projectId: 'PRJ-2026-01',
      title: '发票导出接口异常分支未脱敏导致内部数据库分表名与代码路径泄露',
      verificationStatus: '验证成立',
      severityLabel: '低危',
      severityBasis:
        '低危 · 仅泄露内部物理分表名（t_order_invoice_shard_04）与服务端源码路径，不直接暴露业务数据；验证成立与严重度分开，低危不等于误报。',
      remediationStatus: '未处理',
      affectedTarget: 'https://api.shop.example.com/v2/orders/:orderId/invoice-export',
      currentRevision: 'rev-1（已验证实证版）',
      threadId: 'THR-101',
      descriptionPrincipleAndHarm:
        '当向 POST /v2/orders/:orderId/invoice-export 传入非预期的 format 字段值时，服务端渲染器抛出未捕获异常，并在 HTTP 500 响应体中直接回显内部数据库物理分表名 t_order_invoice_shard_04、SQL 查询片段及服务端源码文件路径。该问题危害等级为低危，但现象100%可稳定复现（低危不等于误报）。',
      reproductionSteps: [
        {
          stepNumber: 1,
          instruction:
            '使用合法买家身份（id-buyer-a）向自有订单发票导出接口发送包含非常规 format 值的 POST 请求（REQ-204）。',
          identityPrerequisite: '测试租户A买家 (id-buyer-a · tenant_alpha)',
          linkedRequestId: 'REQ-204',
          expectedObservation:
            '返回 HTTP 500，响应 JSON 包含 internal_table、sql_fragment 与 Go 源码行号。',
        },
      ],
      remediationAdvice:
        '1. 在接口入参校验层对 format 枚举值执行白名单校验，非法值返回 HTTP 400 Bad Request；2. 全局错误中间件统一屏蔽内部 SQL 片段与堆栈路径，仅向客户端返回 trace_id。',
      verificationRecords: [
        {
          id: 'VER-002',
          findingId: 'F-2026-002',
          executedAt: '2026-11-11 09:31:45',
          executionContextNote:
            '主稿推荐（建议 · 待确认）：独立验证上下文核对 REQ-204 响应字节。',
          verdictLabel: '验证成立',
          severityIndependenceNote:
            '关键原则：验证成立（确有内部表名与 SQL 片段泄露）与严重度（低危）分开记录；低危漏洞同样是真实成立的安全缺陷，绝不混同为“误报”。',
          evidenceRequestIds: ['REQ-204'],
          modelCallId: 'LLM-502',
          summary:
            'REQ-204 响应体真实包含物理表名 t_order_invoice_shard_04 与源码行号，复现稳定，判定为“验证成立（低危）”。',
        },
      ],
      updatedAt: '2026-11-11 09:33',
    },
    {
      id: 'F-2026-003',
      projectId: 'PRJ-2026-01',
      title: '租户买家读取自身名下历史订单摘要（经核实属正常权限内功能）',
      verificationStatus: '验证不成立（正常权限功能）',
      severityLabel: '无危害（非漏洞）',
      severityBasis:
        '无危害 · 当前登录买家读取自身 tenant_alpha 名下订单属业务正常授权功能，正常权限功能不冒充漏洞。',
      remediationStatus: '无需修复（正常功能）',
      affectedTarget: 'https://api.shop.example.com/v2/orders/ORD-90021',
      currentRevision: 'rev-1（验证排除记录）',
      threadId: 'THR-101',
      descriptionPrincipleAndHarm:
        '探索初期曾因响应包含纳税人识别号而生成候选记录 F-2026-003。经核对请求身份上下文（REQ-201 使用 id-buyer-a 访问其本人所属 tenant_alpha 的订单），该行为完全处于当前身份的预期业务权限范围内，未跨越任何权限边界。平台保留此验证记录以备审计，但明确标为“验证不成立（正常权限功能）”，不计入有效漏洞。',
      reproductionSteps: [
        {
          stepNumber: 1,
          instruction:
            '使用 id-buyer-a 请求其本人所属租户的订单 GET /v2/orders/ORD-90021（REQ-201）。',
          identityPrerequisite: '测试租户A买家 (id-buyer-a · 自有资源)',
          linkedRequestId: 'REQ-201',
          expectedObservation:
            '返回当前买家自有订单信息，属正常业务功能，非安全漏洞。',
        },
      ],
      remediationAdvice:
        '属正常权限内业务功能，无需修复；报告生成时默认排除在有效漏洞正文之外。',
      verificationRecords: [
        {
          id: 'VER-003',
          findingId: 'F-2026-003',
          executedAt: '2026-11-11 09:31:45',
          executionContextNote:
            '主稿推荐（建议 · 待确认）：独立验证上下文核对主体身份与资源归属关系。',
          verdictLabel: '验证不成立（正常权限功能）',
          severityIndependenceNote:
            '关键原则：正常权限范围内的业务功能（买家查看自有订单）不冒充安全漏洞。',
          evidenceRequestIds: ['REQ-201'],
          modelCallId: 'LLM-502',
          summary:
            '请求主体 id-buyer-a 与资源归属 tenant_alpha 完全一致，无越权行为，判定验证不成立。',
        },
      ],
      updatedAt: '2026-11-11 09:35',
    },
    {
      id: 'F-2026-004',
      projectId: 'PRJ-2026-01',
      title: '售后退款冲正确认接口在连接异常下疑似缺乏幂等锁保护',
      verificationStatus: '待验证',
      severityLabel: '中危',
      severityBasis:
        '中危（候选评估） · 若缺失服务端幂等键校验，客户端超时重试可能导致重复冲正记账；当前尚待核对 REQ-205 实际落库状态。',
      remediationStatus: '未处理',
      affectedTarget: 'https://api.shop.example.com/v2/refunds/:refundId/confirm',
      currentRevision: 'rev-1（候选记录版）',
      threadId: 'THR-102',
      descriptionPrincipleAndHarm:
        '售后退款冲正确认接口 POST /v2/refunds/:refundId/confirm 接收 client_seq 参数，但在动作 ACT-304 执行期间（REQ-205）发生连接重置，尚未取得明确响应。在确认目标数据库是否已消费该冲正请求前，该问题保持为“待验证”候选状态，不能直接标为验证成立。',
      reproductionSteps: [
        {
          stepNumber: 1,
          instruction:
            '使用租户 A 买家身份（id-buyer-a）提交退款单 RF-8801 的冲正确认请求 POST /v2/refunds/RF-8801/confirm。',
          identityPrerequisite: '测试租户A买家 (id-buyer-a · tenant_alpha)',
          linkedRequestId: 'REQ-205',
          expectedObservation:
            '请求字节发出后遭遇连接重置（发送效果未知），需先核对目标退款单状态，禁止直接自动重放。',
        },
      ],
      remediationAdvice:
        '1. 服务端对退款冲正操作强制要求唯一幂等键（Idempotency-Key 或 refund_id + 状态乐观锁 WHERE status = PENDING_CONFIRM）；2. 对重复提交返回幂等回执而非重复执行资金记账。',
      verificationRecords: [
        {
          id: 'VER-004',
          findingId: 'F-2026-004',
          executedAt: '2026-11-11 09:39:00',
          executionContextNote: '等待 REQ-205 发送结果核对后再启动独立验证。',
          verdictLabel: '待验证（效果未知待核对）',
          severityIndependenceNote:
            '关联请求 REQ-205 发送效果未知，禁止盲目重放，当前保持待验证。',
          evidenceRequestIds: ['REQ-205'],
          modelCallId: 'LLM-501',
          summary:
            '尚缺目标端落库核对结果；上次复测 RT-2026-004-1 因凭据过期返回 401，按规则判定为“无法判断”而非“已修复”。',
        },
      ],
      updatedAt: '2026-11-11 09:40',
    },
  ]

  const timelineEvents: DemoTimelineEvent[] = [
    {
      id: 'TL-101',
      findingId: 'F-2026-001',
      timestamp: '2026-11-11 09:20:11',
      stageLabel: '产生观察',
      actorSource: '工具捕获事实',
      summary:
        '方向 THR-101 执行动作 ACT-301，使用 id-buyer-a 获取订单 ORD-90021 基线响应（REQ-201，owner_tenant=tenant_alpha）。',
      linkedRequestId: 'REQ-201',
      linkedModelCallId: 'LLM-501',
    },
    {
      id: 'TL-102',
      findingId: 'F-2026-001',
      timestamp: '2026-11-11 09:21:48',
      stageLabel: '分配候选编号',
      actorSource: '平台编号服务',
      summary:
        '切换身份 id-buyer-b 请求同一订单返回 200 OK（REQ-202），平台分配稳定漏洞编号 F-2026-001（初始状态：待验证）。',
      linkedRequestId: 'REQ-202',
      linkedModelCallId: 'LLM-501',
    },
    {
      id: 'TL-103',
      findingId: 'F-2026-001',
      timestamp: '2026-11-11 09:28:50',
      stageLabel: '补充对比流量',
      actorSource: '工具捕获事实',
      summary:
        '动作 ACT-302 使用 id-buyer-b 成功调用电子发票导出接口（REQ-203），固定原件指纹并更新发现版本至 rev-2。',
      linkedRequestId: 'REQ-203',
    },
    {
      id: 'TL-104',
      findingId: 'F-2026-001',
      timestamp: '2026-11-11 09:31:45',
      stageLabel: '独立验证记录',
      actorSource: '独立验证上下文（主稿推荐）',
      summary:
        '关联模型调用 LLM-502 复核 REQ-201（归属基线）与 REQ-202/REQ-203（跨租户实证），判定“验证成立 · 高危”，纳入报告 RPT-2026-01-V2。',
      linkedRequestId: 'REQ-203',
      linkedModelCallId: 'LLM-502',
      linkedReportId: 'RPT-2026-01-V2',
    },
    {
      id: 'TL-105',
      findingId: 'F-2026-001',
      timestamp: '2026-11-11 10:30:05',
      stageLabel: '复测结论记录',
      actorSource: '复测任务执行',
      summary:
        '针对 F-2026-001 (rev-2) 沿用原复现逻辑发起独立复测 RT-2026-001-1，捕获新流量 REQ-207（403 Forbidden），结论为“本次范围修复已验证”，生成独立报告 RPT-RT-001-1（不覆盖原证据）。',
      linkedRequestId: 'REQ-207',
      linkedModelCallId: 'LLM-503',
      linkedRetestId: 'RT-2026-001-1',
      linkedReportId: 'RPT-RT-001-1',
    },
    {
      id: 'TL-201',
      findingId: 'F-2026-002',
      timestamp: '2026-11-11 09:31:10',
      stageLabel: '产生观察',
      actorSource: '工具捕获事实',
      summary:
        '动作 ACT-303 发送畸形 format 参数（REQ-204），捕获 HTTP 500 内部数据库分表名与代码行号回显。',
      linkedRequestId: 'REQ-204',
    },
    {
      id: 'TL-202',
      findingId: 'F-2026-002',
      timestamp: '2026-11-11 09:31:45',
      stageLabel: '独立验证记录',
      actorSource: '独立验证上下文（主稿推荐）',
      summary:
        '独立验证记录 VER-002（LLM-502）确认泄露事实稳定存在：判定“验证成立 · 低危”（明确低危不等于误报）。',
      linkedRequestId: 'REQ-204',
      linkedModelCallId: 'LLM-502',
    },
    {
      id: 'TL-301',
      findingId: 'F-2026-003',
      timestamp: '2026-11-11 09:31:45',
      stageLabel: '独立验证记录',
      actorSource: '独立验证上下文（主稿推荐）',
      summary:
        '核对 REQ-201 身份与资源归属一致，判定为“验证不成立（正常权限功能）”，排除出有效漏洞列表。',
      linkedRequestId: 'REQ-201',
      linkedModelCallId: 'LLM-502',
    },
    {
      id: 'TL-401',
      findingId: 'F-2026-004',
      timestamp: '2026-11-11 09:37:52',
      stageLabel: '分配候选编号',
      actorSource: '平台编号服务',
      summary:
        '退款冲正请求 REQ-205 发生连接重置（发送效果未知），平台登记候选编号 F-2026-004（状态：待验证）并阻止自动重放。',
      linkedRequestId: 'REQ-205',
    },
  ]

  const retests: DemoRetestRecord[] = [
    {
      id: 'RT-2026-001-1',
      findingId: 'F-2026-001',
      projectId: 'PRJ-2026-01',
      boundFindingRevision: 'F-2026-001 · rev-2',
      reusedReproductionLogic:
        '沿用原漏洞第 2 步复现逻辑：使用跨租户身份 id-buyer-b 请求租户 A 订单 GET /v2/orders/ORD-90021',
      targetIdentityLabel: '测试租户B买家 (id-buyer-b)',
      authorizedScope: 'https://api.shop.example.com（沿用项目明确授权范围）',
      executionContextNote:
        '主稿推荐（建议 · 待确认）：使用独立复测执行上下文，采集新证据 REQ-207，不覆盖原始发现证据 REQ-201~REQ-203。',
      executionStatus: '已完成（演示）',
      conclusion: '本次范围修复已验证',
      conclusionBasis:
        '使用与原复现步骤一致的跨租户身份 id-buyer-b 重新请求 GET /v2/orders/ORD-90021（新证据 REQ-207），服务端稳定返回 HTTP 403 Forbidden 及 TENANT_OWNERSHIP_MISMATCH，原越权响应不再出现；原发现证据 REQ-201~REQ-203 完整保留未覆盖。',
      linkedRequestId: 'REQ-207',
      linkedModelCallId: 'LLM-503',
      independentReportId: 'RPT-RT-001-1',
      executedAt: '2026-11-11 10:30',
    },
    {
      id: 'RT-2026-002-1',
      findingId: 'F-2026-002',
      projectId: 'PRJ-2026-01',
      boundFindingRevision: 'F-2026-002 · rev-1',
      reusedReproductionLogic:
        '沿用原漏洞第 1 步复现逻辑：向 /v2/orders/ORD-90021/invoice-export 发送畸形 format 参数',
      targetIdentityLabel: '测试租户A买家 (id-buyer-a)',
      authorizedScope: 'https://api.shop.example.com',
      executionContextNote: '独立复测执行上下文（演示记录）',
      executionStatus: '已完成（演示）',
      conclusion: '仍存在',
      conclusionBasis:
        '重新发送畸形 format 参数后，服务端仍返回 HTTP 500 及内部物理分表名 t_order_invoice_shard_04，确认该低危漏洞“仍存在”。',
      linkedRequestId: 'REQ-204',
      independentReportId: 'RPT-2026-01-V2',
      executedAt: '2026-11-11 10:20',
    },
    {
      id: 'RT-2026-004-1',
      findingId: 'F-2026-004',
      projectId: 'PRJ-2026-01',
      boundFindingRevision: 'F-2026-004 · rev-1',
      reusedReproductionLogic:
        '沿用原复现逻辑检查 /v2/refunds/RF-8801 状态与冲正幂等边界',
      targetIdentityLabel: '测试租户A买家（过期会话演示）',
      authorizedScope: 'https://api.shop.example.com',
      executionContextNote: '独立复测执行上下文（演示记录）',
      executionStatus: '前置鉴权受阻（演示）',
      conclusion: '无法判断',
      conclusionBasis:
        '复测前置检查时目标返回 401 Token Expired（登录态失效），未能到达退款冲正业务逻辑。遵守核心不变量：目标不可达、登录失败、材料不足或仅未复现，绝不自动标记为“本次范围修复已验证”，结论记为“无法判断”。',
      linkedRequestId: 'REQ-205',
      independentReportId: 'RPT-2026-01-V2',
      executedAt: '2026-11-11 10:12',
    },
  ]

  const reports: DemoReportVersion[] = [
    {
      id: 'RPT-2026-01-V2',
      projectId: 'PRJ-2026-01',
      versionTag: 'v1.1（固定证据版）',
      reportKind: '项目漏洞报告',
      title: '商城多租户订单与结算接口安全研究报告（固定证据版）',
      boundFindingIds: ['F-2026-001', 'F-2026-002'],
      boundEvidenceIds: ['REQ-201', 'REQ-202', 'REQ-203', 'REQ-204'],
      generationStatus: '已固定版本',
      publishStatus: '本地已审阅版本',
      excludedFindingNote:
        '过滤与不编造说明：F-2026-003 已核实属“正常权限功能（非漏洞）”，已排除出报告正文；F-2026-002 虽为低危但验证成立（低危不等于误报），如实纳入报告；F-2026-004 因关联请求 REQ-205 发送效果未知保持待验证。',
      createdAt: '2026-11-11 09:45',
      markdownContent: `# 商城多租户订单与结算接口安全研究报告

- **报告版本**：RPT-2026-01-V2（v1.1 固定证据版 · 演示数据）
- **所属项目**：PRJ-2026-01（商城多租户订单与结算接口研究）
- **明确授权范围**：\`https://api.shop.example.com\`、\`https://auth.shop.example.com\`
- **状态区分**：报告已固定版本 | 本地已审阅（未对外发布）

---

## 一、验证成立漏洞：F-2026-001（验证成立 · 高危 · rev-2）

### 1. 漏洞描述（原理与危害）
订单中心服务在处理 \`/v2/orders/:orderId\` 与 \`/v2/orders/:orderId/invoice-export\` 请求时，未校验当前买家令牌所属 \`tenant_id\` 与订单 \`owner_tenant\` 的绑定关系，导致任意租户买家可跨租户读取他人订单详情并导出电子发票。

### 2. 复现步骤（绑定真实流量引用）
1. **租户 A 基线采样（REQ-201）**：使用 \`id-buyer-a\` 请求 \`GET https://api.shop.example.com/v2/orders/ORD-90021\`，返回 \`200 OK\`（归属 \`tenant_alpha\`）。
2. **租户 B 跨租户读取（REQ-202）**：切换 \`id-buyer-b\` 请求同一地址，仍返回 \`200 OK\` 与完整税号字节（指纹 \`sha256:3c92b77d...\`）。
3. **租户 B 跨租户发票导出（REQ-203）**：使用 \`id-buyer-b\` 调用 \`POST /v2/orders/ORD-90021/invoice-export\`，返回 \`200 OK\` 及发票预览地址（指纹 \`sha256:9f01e442...\`）。

### 3. 修复建议
在订单查询与发票导出服务层强制校验 \`owner_tenant == current_tenant_id\`，不匹配时统一返回 \`403 Forbidden\`。

---

## 二、验证成立漏洞：F-2026-002（验证成立 · 低危 · rev-1）
- **验证结论**：验证成立（低危不等于误报）
- **关联流量**：\`REQ-204\`（传入非常规 \`format\` 参数触发 HTTP 500，泄露物理分表名 \`t_order_invoice_shard_04\` 与 SQL 片段）
- **修复建议**：入参枚举白名单校验 + 全局错误响应脱敏。

---

## 三、已排除与待核对记录说明
- **F-2026-003（已排除）**：验证不成立（买家读取自有订单属正常权限内业务功能，不冒充漏洞）。
- **F-2026-004（待验证）**：关联请求 \`REQ-205\` 发送效果未知（连接重置），禁止自动重放，尚待状态核对。`,
      jsonContent: JSON.stringify(
        {
          report_id: 'RPT-2026-01-V2',
          version: 'v1.1',
          demo_notice: '设计预览演示数据 · 未触发真实网络或外部投递',
          project_id: 'PRJ-2026-01',
          authorized_origins: [
            'https://api.shop.example.com',
            'https://auth.shop.example.com',
          ],
          verified_findings: [
            {
              finding_id: 'F-2026-001',
              revision: 'rev-2',
              verification_status: 'VERIFIED_TRUE',
              severity: 'HIGH',
              evidence_request_ids: ['REQ-201', 'REQ-202', 'REQ-203'],
            },
            {
              finding_id: 'F-2026-002',
              revision: 'rev-1',
              verification_status: 'VERIFIED_TRUE',
              severity: 'LOW',
              note: '验证成立与严重度分开，低危不等于误报',
              evidence_request_ids: ['REQ-204'],
            },
          ],
          excluded_normal_feature: ['F-2026-003'],
          pending_unknown_outcome: ['F-2026-004'],
        },
        null,
        2,
      ),
    },
    {
      id: 'RPT-RT-001-1',
      projectId: 'PRJ-2026-01',
      versionTag: 'retest-v1.0',
      reportKind: '单漏洞复测报告',
      title: 'F-2026-001 跨租户越权漏洞独立复测报告（RT-2026-001-1）',
      boundFindingIds: ['F-2026-001'],
      boundRetestId: 'RT-2026-001-1',
      boundEvidenceIds: ['REQ-201', 'REQ-202', 'REQ-203', 'REQ-207'],
      generationStatus: '已固定版本',
      publishStatus: '仅本地保存（未发布）',
      excludedFindingNote:
        '独立复测报告说明：本报告沿用原漏洞版本 F-2026-001 (rev-2) 复现逻辑并绑定新增复测流量 REQ-207，不覆盖原始发现报告 RPT-2026-01-V2。',
      createdAt: '2026-11-11 10:31',
      markdownContent: `# 单漏洞独立复测报告：RT-2026-001-1

- **复测任务编号**：RT-2026-001-1
- **关联原漏洞版本**：F-2026-001（rev-2 · 订单详情与发票导出跨租户水平越权）
- **本次复测身份与范围**：测试租户B买家 (\`id-buyer-b\`) / \`https://api.shop.example.com\`
- **复测结论**：**本次范围修复已验证**（枚举：仍存在 / 本次范围修复已验证 / 无法判断）

---

## 1. 原复现逻辑与原证据保留
原漏洞版本 \`F-2026-001 (rev-2)\` 中，\`id-buyer-b\` 访问 \`GET /v2/orders/ORD-90021\`（\`REQ-202\`）与 \`POST /v2/orders/ORD-90021/invoice-export\`（\`REQ-203\`）返回 \`200 OK\`。原证据完整保留，不被复测覆盖。

## 2. 本次复测新流量证据
1. 沿用原复现逻辑，使用相同跨租户身份 \`id-buyer-b\` 发起请求 \`GET https://api.shop.example.com/v2/orders/ORD-90021\`（新流量证据 \`REQ-207\`）。
2. 服务端返回 \`HTTP 403 Forbidden\`，响应体明确返回 \`{"error_code": "TENANT_OWNERSHIP_MISMATCH"}\`（指纹 \`sha256:7f33c109...\`）。

## 3. 复测结论
在本次授权范围与 \`id-buyer-b\` 身份前提下，结论为 **本次范围修复已验证**。`,
      jsonContent: JSON.stringify(
        {
          report_id: 'RPT-RT-001-1',
          report_kind: 'INDEPENDENT_RETEST_REPORT',
          retest_id: 'RT-2026-001-1',
          bound_finding_id: 'F-2026-001',
          bound_finding_revision: 'rev-2',
          conclusion: '本次范围修复已验证',
          original_evidence_preserved: ['REQ-201', 'REQ-202', 'REQ-203'],
          new_retest_evidence: ['REQ-207'],
        },
        null,
        2,
      ),
    },
    {
      id: 'RPT-2026-01-V1',
      projectId: 'PRJ-2026-01',
      versionTag: 'v1.0（历史草稿）',
      reportKind: '项目漏洞报告',
      title: '商城多租户订单与结算接口阶段草稿（仅含初步订单读取证据）',
      boundFindingIds: ['F-2026-001'],
      boundEvidenceIds: ['REQ-201', 'REQ-202'],
      generationStatus: '草稿',
      publishStatus: '仅本地保存（未发布）',
      excludedFindingNote: '历史快照：生成于发票导出证据 REQ-203 捕获之前，仅供版本回溯对比。',
      createdAt: '2026-11-11 09:24',
      markdownContent: `# 商城多租户订单接口研究阶段草稿 (v1.0)\n\n- 绑定初始证据：REQ-201、REQ-202\n- 后续证据补充后已生成新版本 v1.1（RPT-2026-01-V2），本草稿保持原样不静默改写。`,
      jsonContent: JSON.stringify(
        {
          report_id: 'RPT-2026-01-V1',
          version: 'v1.0-draft',
          bound_finding_ids: ['F-2026-001'],
          bound_evidence_ids: ['REQ-201', 'REQ-202'],
        },
        null,
        2,
      ),
    },
  ]

  const pendingDecisions: PendingDecisionItem[] = [
    {
      id: 'D01',
      topic: '首版范围与延期项（D1 已更新）',
      recommendation:
        '首版聚焦本地 Web/API 自主挖掘闭环；请求风险审批/待放行控制及外部平台真实对接明确延期至后续阶段（不删除现有 G0 后端保护）。',
      implicationIfDeferred: '首版不以待放行审批队列或外部连接器作为主流程。',
      statusLabel: '已确认调整（D1）',
    },
    {
      id: 'D02',
      topic: '流量中心分区与 LLM 调用保存（D1 已更新）',
      recommendation:
        '目标流量独立保留。模型输入与返回按任务开关记录；关闭时明确未启用记录，仍展示调用元数据。默认值与接口待冻结。',
      implicationIfDeferred: '便于复盘探索、验证与复测各轮模型的实际上下文与输出质量。',
      statusLabel: '已确认调整（D1）',
    },
    {
      id: 'D03',
      topic: '挖掘 Agent 架构与验证/复测上下文（优先评审）',
      recommendation:
        '当前用户优先评审 docs/research-engine-design.md 的挖掘架构（质量/效率/深度/覆盖）。模板中展示的独立验证/复测执行上下文仅为主稿推荐，不代表固定多专家架构已获批准。',
      implicationIfDeferred: '以架构文档评审结论为准，模板不倒逼或锁定引擎架构。',
      statusLabel: '待确认（主稿建议）',
    },
    {
      id: 'D04',
      topic: '漏洞验证判定与复测结论规则（D1 已更新）',
      recommendation:
        '验证成立与严重度分开（低危不等于误报，正常权限功能不冒充漏洞）；复测沿用原复现逻辑、采集新证据且不覆盖历史，结论枚举为“仍存在 / 本次范围修复已验证 / 无法判断”。',
      implicationIfDeferred: '防止把低危实证丢弃为误报，或把鉴权失败误判为已修复。',
      statusLabel: '已确认调整（D1）',
    },
    {
      id: 'D05',
      topic: '目标登录态与多身份对比',
      recommendation:
        '首版支持人工配置项目内具名 Token / Cookie / 请求头身份（如租户 A 与租户 B），支持双身份对比；不承诺任意站点自动登录。',
      implicationIfDeferred: '凭据存储与生命周期仍需在业务契约阶段冻结。',
      statusLabel: '待确认（主稿建议）',
    },
    {
      id: 'D06',
      topic: '研究预算与本地报告格式',
      recommendation:
        '先明确时间/步数/并发硬限制；本地报告首版优先交付 Markdown + 结构化 JSON，PDF 随后评估。',
      implicationIfDeferred: '不展示虚假金额或假进度条。',
      statusLabel: '待确认（主稿建议）',
    },
    {
      id: 'D07',
      topic: '请求风险审批与外部复测/SDL 真实对接',
      recommendation:
        '已按用户指示从首版主流程延期；现有代码中的 G0 基础保护保持不动，待核心挖掘架构与首版闭环稳定后再立批次。',
      implicationIfDeferred: '本轮不实现审批中心或外部平台投递连接器。',
      statusLabel: '已延期（后续阶段）',
    },
  ]

  return {
    projects,
    threads,
    observations,
    traffic,
    modelCalls: modelCalls.map((call) => ({
      ...call,
      recordingEnabled: call.projectId === 'PRJ-2026-01',
      actualInputText: call.projectId === 'PRJ-2026-01' ? call.actualInputText : null,
      actualOutputText: call.projectId === 'PRJ-2026-01' ? call.actualOutputText : null,
    })),
    graphNodes,
    graphEdges,
    findings,
    timelineEvents,
    retests,
    reports,
    pendingDecisions,
  }
}
