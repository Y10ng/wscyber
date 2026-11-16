import { useState } from 'react'
import { Button, Segmented } from 'antd'
import type { DemoTrafficRequest } from './types.ts'
import { MessageViewer } from './MessageViewer.tsx'
import { CopyButton, GridFold } from './SurfaceControls.tsx'

// Both project evidence and global history render the exact same referenced record.
export function TrafficDetail({ request, onResearch, onFinding, onModel }: {
  request: DemoTrafficRequest; onResearch?: (id: string) => void; onFinding?: (id: string) => void; onModel?: (id: string) => void
}) {
  const [layout, setLayout] = useState('双栏')
  return <div className="traffic-detail">
    <div className="detail-context"><span className="method-label">{request.method}</span><span className="selected-url" title={request.url}>{request.url}</span><CopyButton label="复制完整 URL" text={request.url} /></div>
    <div className="detail-options"><Segmented aria-label="报文布局" options={['双栏', '仅请求', '仅响应']} value={layout} onChange={(value) => setLayout(String(value))} /><span className="muted">{request.id}</span></div>
    <div className="message-metadata"><span>根据保存字段重建 · 本地 fixture</span><span>{request.truncated ? '捕获已截断' : request.statusCode === null ? request.sendStatus : '已保存请求与响应'}</span></div>
    <div className="traffic-relations"><GridFold title="流量上下文与关联信息"><div className="detail-document">
      <p>{request.threadTitle} · {request.identityLabel}</p><p>{request.verificationPurpose}</p><p>{request.scopeValidity} · {request.sendStatus}</p>
      {request.unknownOutcomeNote && <p className="warning-note">{request.unknownOutcomeNote} 不自动重放。</p>}
      {request.truncationNote && <p className="warning-note">{request.truncationNote} 仅提供保存的示例字段，不提供完整字节附件或指纹校验。</p>}
      <div className="link-list">{onResearch && <Button aria-label="跳转关联研究方向" onClick={() => onResearch(request.threadId)}>研究方向</Button>}{onFinding && request.linkedFindingIds.map((id) => <Button key={id} aria-label={`查看发现 ${id}`} onClick={() => onFinding(id)}>{id}</Button>)}{onModel && request.linkedModelCallId && <Button aria-label="查看关联模型记录" onClick={() => onModel(request.linkedModelCallId!)}>模型记录</Button>}</div>
      <p className="muted">{request.capturedAt} · {request.captureSource}</p>
    </div></GridFold></div>
    <div className={`message-panes ${layout === '双栏' ? 'paired' : 'single'}`}>
      {layout !== '仅响应' && <MessageViewer key={`${request.id}-request`} request={request} side="request" />}
      {layout !== '仅请求' && <MessageViewer key={`${request.id}-response`} request={request} side="response" />}
    </div>
  </div>
}
