import { useMemo, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Space, Tag, Typography } from 'antd'
import { CloseOutlined, EyeInvisibleOutlined, EyeOutlined, ReloadOutlined } from '@ant-design/icons'
import type { Action, Mission, Thread } from '../types.ts'
import { actionSummaryTitle, formatDateTime, statusBadgeColor, statusText } from '../types.ts'
import { formatHeadersForDisplay } from '../workspace.ts'
import { EvidenceBytesViewer } from './EvidenceBytesViewer.tsx'
import { CopyButton } from '../../ui/SurfaceControls.tsx'

interface ActionEvidenceProps {
  action: Action | null
  mission?: Mission
  thread?: Thread
  onClose: () => void
  readError?: string
  refreshing?: boolean
  onRefresh?: () => void
}

export function ActionEvidenceDrawer({ action, mission, thread, onClose, readError, refreshing, onRefresh }: ActionEvidenceProps) {
  // Drawer 保持挂载以保留关闭/焦点归还；材料子树在身份变化或关闭时立即卸载。
  const sourceKey = JSON.stringify([mission?.id ?? '', thread?.id ?? action?.thread_id ?? '', action?.id ?? null])
  return <Drawer title="动作回执与证据详情" placement="right" size="large" open={Boolean(action)} onClose={onClose} destroyOnHidden
    extra={onRefresh ? <Button loading={refreshing} onClick={onRefresh}>刷新详情</Button> : undefined}>
    {action && <ActionEvidenceContent key={sourceKey} action={action} mission={mission} thread={thread} sourceKey={sourceKey} readError={readError} />}
  </Drawer>
}

/** 研究活动与 HTTP 表格共用的就地证据；身份键不包含任何材料正文或秘密。 */
export function ActionEvidencePanel({ action, mission, thread, onClose, readError, refreshing, onRefresh }: ActionEvidenceProps) {
  const sourceKey = JSON.stringify([mission?.id ?? '', thread?.id ?? action?.thread_id ?? '', action?.id ?? null])
  if (!action) return null
  return <section className="ws-inline-evidence" aria-label={action.input.kind === 'http' ? 'HTTP 请求与响应证据' : '命令输出与执行证据'} tabIndex={-1}>
    <div className="ws-inline-evidence-heading">
      <div className="ws-inline-evidence-title">
        <strong>{action.input.kind === 'http' ? 'HTTP 请求与响应' : '命令输出'}</strong>
        <code>{actionSummaryTitle(action)}</code>
      </div>
      <Space size={4}>
        {onRefresh && <Button type="text" icon={<ReloadOutlined />} loading={refreshing} aria-label="刷新证据详情" onClick={onRefresh} />}
        <Button type="text" icon={<CloseOutlined />} aria-label="关闭证据" onClick={onClose} />
      </Space>
    </div>
    <ActionEvidenceContent key={sourceKey} action={action} mission={mission} thread={thread} sourceKey={sourceKey} readError={readError} presentation="inline" />
  </section>
}

function VisibleTextCopy({ text, label, sourceKey, mode }: { text: string; label: string; sourceKey: string; mode: string }) {
  return <div className="evidence-copy-action">
    <Typography.Text type="secondary">{label}</Typography.Text>
    <CopyButton key={JSON.stringify([sourceKey, mode])} text={text} label={label} />
  </div>
}

export function ActionEvidenceContent({ action, mission, thread, sourceKey, readError, presentation = 'drawer' }: Omit<ActionEvidenceProps, 'action' | 'onClose'> & { action: Action; sourceKey: string; presentation?: 'drawer' | 'inline' }) {
  const [revealSensitiveHeaders, setRevealSensitiveHeaders] = useState(false)
  const headerMode = revealSensitiveHeaders ? 'revealed' : 'masked'
  const inline = presentation === 'inline'

  const charset = useMemo(() => {
    const headers = action?.result?.headers || {}
    const contentType = Object.entries(headers).find(([key]) => key.toLowerCase() === 'content-type')?.[1]?.join(';') || ''
    return /charset\s*=\s*["']?([^;\s"']+)/i.exec(contentType)?.[1]
  }, [action])

  const reqHeadersDisplay = useMemo(
    () => formatHeadersForDisplay(action?.input.headers, revealSensitiveHeaders),
    [action?.input.headers, revealSensitiveHeaders],
  )
  const respHeadersDisplay = useMemo(
    () => formatHeadersForDisplay(action?.result?.headers, revealSensitiveHeaders),
    [action?.result?.headers, revealSensitiveHeaders],
  )

  const purpose = <details className="ws-evidence-details">
    <summary>申请目的与决策说明</summary>
    <Descriptions column={1} size="small">
      <Descriptions.Item label="申请目的">{action.input.reason || '未提供目的'}</Descriptions.Item>
      {action.decision_reason && <Descriptions.Item label="决策说明">{action.decision_reason}</Descriptions.Item>}
      {action.resolved_at && <Descriptions.Item label={`人工核对（${formatDateTime(action.resolved_at)}）`}>{action.resolution_note}</Descriptions.Item>}
    </Descriptions>
  </details>

  const capture = <div className="ws-capture-grid">
    <section className="ws-capture-pane" aria-label={action.input.kind === 'http' ? 'HTTP 请求' : '命令'}>
      <div className="ws-capture-head">
        <span className="ws-capture-title">{action.input.kind === 'http' ? `REQUEST · ${action.input.method || 'GET'}` : 'COMMAND'}</span>
        {action.input.kind === 'http' && <div className="ws-capture-tools">
          {reqHeadersDisplay.text && <VisibleTextCopy sourceKey={`${sourceKey}:request-headers`} mode={headerMode} text={reqHeadersDisplay.text} label="复制请求头" />}
          {action.input.body !== undefined && <VisibleTextCopy sourceKey={`${sourceKey}:request-body`} mode="text" text={action.input.body} label="复制请求正文" />}
        </div>}
      </div>
      <div className="ws-capture-raw">
        {action.input.kind === 'http' ? (
          <pre className="ws-code-pre">{action.input.url}{reqHeadersDisplay.text ? `\n\n${reqHeadersDisplay.text}` : '\n\n无自定义请求头'}{action.input.body ? `\n\n${action.input.body}` : '\n\n请求正文为空'}</pre>
        ) : (
          <pre className="ws-code-pre">{action.input.command}</pre>
        )}
      </div>
    </section>

    <section className="ws-capture-pane" aria-label="响应或输出">
      <div className="ws-capture-head">
        <span className="ws-capture-title">{action.input.kind === 'http' ? 'RESPONSE' : 'OUTPUT'}{action.result?.status_code ? ` · ${action.result.status_code}` : ''}{action.result ? ` · ${action.result.duration_ms} ms` : ''}</span>
        {action.input.kind === 'http' && respHeadersDisplay.text && <div className="ws-capture-tools"><VisibleTextCopy sourceKey={`${sourceKey}:response-headers`} mode={headerMode} text={respHeadersDisplay.text} label="复制响应头" /></div>}
      </div>
      <div className="ws-capture-raw">
        {action.result ? (action.input.kind === 'http' ? <>
          <pre className="ws-code-pre">{action.result.status_code ? `HTTP ${action.result.status_code}` : '未收到状态码'}{action.result.resolved_ip ? ` · IP ${action.result.resolved_ip}` : ''}{respHeadersDisplay.text ? `\n\n${respHeadersDisplay.text}` : '\n\n未保存响应头'}</pre>
          <EvidenceBytesViewer sourceKey={`${sourceKey}:response-body`} base64Value={action.result.body_base64} label="响应正文" charset={charset} filename={`${action.id}-response.bin`} />
        </> : <>
          <pre className="ws-code-pre">退出码：{action.result.exit_code}</pre>
          <EvidenceBytesViewer sourceKey={`${sourceKey}:stdout`} base64Value={action.result.stdout_base64} label="标准输出 (stdout)" filename={`${action.id}-stdout.bin`} />
          <EvidenceBytesViewer sourceKey={`${sourceKey}:stderr`} base64Value={action.result.stderr_base64} label="标准错误 (stderr)" filename={`${action.id}-stderr.bin`} />
        </>) : <pre className="ws-code-pre">{action.status === 'rejected' ? '此请求已拒绝，无执行回执。' : '尚无已保存的执行回执。'}</pre>}
      </div>
    </section>
  </div>

  const identity = <details className="ws-evidence-details"><summary>动作与请求标识</summary><Descriptions column={1} size="small">
    <Descriptions.Item label="动作 ID"><code>{action.id}</code></Descriptions.Item>
    <Descriptions.Item label="请求 SHA-256"><code className="ws-inline-code">{action.request_hash}</code></Descriptions.Item>
    <Descriptions.Item label="最近更新">{formatDateTime(action.updated_at)}</Descriptions.Item>
  </Descriptions></details>

  return <Space orientation="vertical" size="small" className={inline ? 'ws-action-evidence-inline' : 'ws-action-evidence-drawer'} style={{ width: '100%' }}>
    {readError && <Alert type="error" showIcon title={`详情未更新：${readError}（保留上次读取详情）`} />}
    <div className="ws-section-row">
      <Space size="small" wrap>
        <Tag color={statusBadgeColor(action.status)}>{statusText(action.status)}{action.resolved_at ? ' · 已人工核对' : ''}</Tag>
        <Tag>{action.input.kind === 'http' ? '受控 HTTP' : '工具命令'}</Tag>
        <Typography.Text type="secondary">{formatDateTime(action.created_at)}</Typography.Text>
      </Space>
      {(reqHeadersDisplay.hasSensitive || respHeadersDisplay.hasSensitive) && <Button icon={revealSensitiveHeaders ? <EyeInvisibleOutlined /> : <EyeOutlined />} onClick={() => setRevealSensitiveHeaders((prev) => !prev)}>{revealSensitiveHeaders ? '隐藏敏感头原值' : '展开敏感头原值'}</Button>}
    </div>

    {!inline && mission && thread && <Card size="small"><Descriptions column={1} size="small">
      <Descriptions.Item label="所属项目">{mission.title}</Descriptions.Item>
      <Descriptions.Item label="研究方向">{thread.title}</Descriptions.Item>
      <Descriptions.Item label="授权范围"><Space size={[6, 6]} wrap>{mission.allowed_origins.map((origin) => <Tag key={origin} className="ws-mono-tag">{origin}</Tag>)}</Space></Descriptions.Item>
    </Descriptions></Card>}

    {action.error && <Alert type="error" showIcon title={`执行说明：${action.error}`} />}
    {action.status === 'approved' && <Alert type="info" showIcon title="已放行，等待后台认领执行；若所属方向处于暂停状态，恢复前不会派发。" />}
    {action.result?.truncated && <Alert type="warning" showIcon title="证据已被截断，下方仅包含已保存部分。" />}
    {(action.result?.effect_unknown || action.status === 'outcome_unknown') && <Alert type="warning" showIcon title="外部效果未知，不代表未执行；禁止自动重放。" />}

    {inline ? <>{capture}{purpose}{identity}</> : <>{purpose}{capture}{identity}</>}
  </Space>
}
