import { useState } from 'react'
import { CopyButton, GridFold, StreamText } from './SurfaceControls.tsx'
import { Button, Empty, Segmented, Tag } from 'antd'
import { ArrowRightOutlined, FileTextOutlined, PauseOutlined, PlayCircleOutlined } from '@ant-design/icons'
import type { InitialDemoDataset } from './demoData.ts'
import type { DemoFinding, DemoModelCall, DemoThread } from './types.ts'
import type { DemoProgress } from './demoProgress.ts'

export function StatusLabel({ children }: { children: string }) {
  return <span className={`status-label ${children === '运行中' || children === '验证成立' ? 'positive' : children.includes('未知') || children === '待核对' || children === '待验证' ? 'caution' : ''}`}><i />{children}</span>
}

export function ResearchWorkspace({ data, projectId, selectedThreadId, onThread, progress, onAdvance, onPause, onSupplement, onFinding, onTraffic, streamRound = null }: {
  data: InitialDemoDataset; projectId: string; selectedThreadId: string; onThread: (id: string) => void
  streamRound?: number | null; progress: DemoProgress; onAdvance: () => void; onPause: (thread: DemoThread) => void; onSupplement: () => void
  onFinding: (id: string) => void; onTraffic: (id: string) => void
}) {
  const threads = data.threads.filter((thread) => thread.projectId === projectId)
  const thread = threads.find((item) => item.id === selectedThreadId) ?? threads[0]
  const findings = data.findings.filter((item) => item.projectId === projectId && item.threadId === thread?.id)
  const observations = data.observations.filter((item) => item.projectId === projectId && item.threadId === thread?.id)
  const incremental = data.timelineEvents.filter((event) => findings.some((finding) => finding.id === event.findingId) && event.id.startsWith('TL-DEMO')).slice().reverse()
  const boundFinding = data.findings.find((finding) => finding.id === progress.findingId)
  const canAdvance = thread?.executionStatus === '运行中' && (!boundFinding || boundFinding.threadId === thread.id)
  const stageLabel = ['下一轮：记录候选', '下一轮：补充对照证据', '下一轮：记录验证结论', '验证已记录 · 继续积累材料'][progress.stage]
  return <div className="research-layout">
    <aside className="object-list" aria-label="研究方向列表"><div className="pane-heading"><strong>研究方向</strong></div>{threads.map((item) => <button key={item.id} className={`object-item ${item.id === thread?.id ? 'active' : ''}`} aria-label={`研究方向 ${item.title}`} aria-pressed={item.id === thread?.id} onClick={() => onThread(item.id)}><span className="object-kicker">{item.id}</span><strong>{item.title}</strong><StatusLabel>{item.executionStatus}</StatusLabel></button>)}</aside>
    {thread ? <>
      <section className="research-document scroll-pane" aria-label="当前研究方向">
        <div className={`research-grid ${incremental.length || observations.length ? 'has-activity' : ''}`}><div className="research-main">
        <div className="document-heading"><h2>{thread.title}</h2><StatusLabel>{thread.executionStatus}</StatusLabel></div>
        <p>{thread.questionAndBasis}</p>
        <section className="next-step"><h3>下一步</h3><p>{thread.nextStepPlan}</p>
          {thread.blockingReason ? <div className="warning-note"><strong>等待核对</strong><p>{thread.blockingReason}</p>{thread.blockingRequestId && <Button aria-label="查看未知请求" onClick={() => onTraffic(thread.blockingRequestId!)}>查看请求</Button>}</div> : <><div className="action-line"><Button type="primary" aria-label="推进一轮" icon={<PlayCircleOutlined />} disabled={!canAdvance} onClick={onAdvance}>推进一轮</Button>{['运行中', '已暂停后续派发'].includes(thread.executionStatus) && <Button aria-label={thread.executionStatus === '运行中' ? '暂停后续派发' : '恢复后续派发'} icon={thread.executionStatus === '运行中' ? <PauseOutlined /> : <PlayCircleOutlined />} onClick={() => onPause(thread)}>{thread.executionStatus === '运行中' ? '暂停' : '恢复'}</Button>}</div><div className="muted stage-label">{boundFinding && boundFinding.threadId !== thread.id ? '增量候选属于另一研究方向，请切换后继续。' : stageLabel}</div></>}
        </section>
        {streamRound !== null && <div className="local-stream" aria-label="本地研究增量示例"><span className="eyebrow">本地演示 · 非真实模型调用</span><StreamText key={streamRound} text={`第 ${streamRound} 轮材料已保存，继续核对身份前提与证据。`} /></div>}
        <GridFold title="候选与证据" defaultOpen><div className="evidence-panel" aria-label="候选与证据">{findings.length ? findings.map((finding) => <button className="finding-peek" key={finding.id} aria-label={`打开发现 ${finding.id}`} onClick={() => onFinding(finding.id)}><span className="object-kicker">{finding.id}</span><strong>{finding.title}</strong><StatusLabel>{finding.verificationStatus}</StatusLabel><span className="muted">{finding.severityLabel} · {finding.currentRevision}</span></button>) : <p className="muted">尚无候选；观察不自动成为已确认漏洞。</p>}<div className="supplement-block"><FileTextOutlined /><span>固定版本研究资料</span><Button aria-label="补充研究资料" onClick={onSupplement}>补充资料</Button></div></div></GridFold>
        </div><section className="research-activity"><div className="section-heading"><h3>最近活动</h3><span className="muted">第 {progress.round} 轮</span></div>
        <ol className="activity-list">{incremental.map((event) => <li key={event.id}><div className="activity-meta"><strong>{event.stageLabel}</strong><span>{event.timestamp}</span></div><p>{event.summary}</p>{event.linkedRequestId && <Button type="link" aria-label={`查看流量 ${event.linkedRequestId}`} onClick={() => onTraffic(event.linkedRequestId!)}>{event.linkedRequestId} <ArrowRightOutlined /></Button>}</li>)}{observations.map((item) => <li key={item.id}><div className="activity-meta"><strong>{item.sourceType}</strong><span>{item.recordedAt}</span></div><p>{item.title}</p><GridFold title="观察正文"><p>{item.summary}</p></GridFold>{item.linkedRequestId && <Button type="link" aria-label={`查看流量 ${item.linkedRequestId}`} onClick={() => onTraffic(item.linkedRequestId!)}>{item.linkedRequestId} <ArrowRightOutlined /></Button>}</li>)}</ol>
        {!incremental.length && !observations.length && <p className="muted">尚无已保存活动。</p>}
        </section></div>
      </section>

    </> : <Empty description="暂无研究方向" />}
  </div>
}

export function FindingsWorkspace({ data, projectId, selectedId, onSelect, onTraffic }: { data: InitialDemoDataset; projectId: string; selectedId: string | null; onSelect: (id: string) => void; onTraffic: (id: string) => void }) {
  const findings = data.findings.filter((finding) => finding.projectId === projectId)
  const finding = findings.find((item) => item.id === selectedId) ?? findings[0]
  return <div className="records-layout"><aside className="object-list" aria-label="发现列表"><div className="pane-heading"><strong>发现记录</strong></div>{findings.map((item) => <button className={`object-item ${item.id === finding?.id ? 'active' : ''}`} aria-label={`选择发现 ${item.id}`} key={item.id} onClick={() => onSelect(item.id)}><span className="object-kicker">{item.id}</span><strong>{item.title}</strong><StatusLabel>{item.verificationStatus}</StatusLabel></button>)}</aside>{finding ? <FindingDocument key={finding.id} finding={finding} data={data} onTraffic={onTraffic} /> : <div className="empty-state"><strong>尚无发现</strong><span>候选会在研究过程中出现，无需等待任务结束。</span></div>}</div>
}
function FindingDocument({ finding, data, onTraffic }: { finding: DemoFinding; data: InitialDemoDataset; onTraffic: (id: string) => void }) {
  const [tab, setTab] = useState('正文')
  const events = data.timelineEvents.filter((event) => event.findingId === finding.id)
  return <article className="finding-document scroll-pane"><header><div className="eyebrow">{finding.id} · {finding.currentRevision}</div><h2>{finding.title}</h2><div className="action-line"><StatusLabel>{finding.verificationStatus}</StatusLabel><Tag>{finding.severityLabel}</Tag></div><p className="muted">{finding.severityBasis}</p></header><Segmented aria-label="发现详情视图" value={tab} onChange={(value) => setTab(String(value))} options={['正文', '证据与历史']} />
    {tab === '正文' ? <div className="finding-body"><section><h3>漏洞描述</h3><p>{finding.descriptionPrincipleAndHarm}</p></section><section><h3>复现步骤</h3><ol className="reproduction-list">{finding.reproductionSteps.map((step) => <li key={`${step.stepNumber}-${step.linkedRequestId}`}><p>{step.instruction}</p><p className="muted">前提：{step.identityPrerequisite}</p><p>{step.expectedObservation}</p><Button aria-label={`查看证据 ${step.linkedRequestId}`} onClick={() => onTraffic(step.linkedRequestId)}>{step.linkedRequestId} <ArrowRightOutlined /></Button></li>)}</ol></section><section><h3>修复建议</h3><p>{finding.remediationAdvice}</p></section></div> : <div className="finding-history">
      <section><h3>验证记录</h3>{finding.verificationRecords.length ? finding.verificationRecords.map((record) => <div className="history-entry" key={record.id}><StatusLabel>{record.verdictLabel}</StatusLabel><p>{record.summary}</p><p className="muted">{record.executionContextNote}</p><div className="link-list">{record.evidenceRequestIds.map((id) => <Button key={id} aria-label={`查看验证流量 ${id}`} onClick={() => onTraffic(id)}>{id}</Button>)}</div></div>) : <p className="muted">尚无验证结论。候选不等于确认。</p>}</section>
      <section><h3>正文与证据演进</h3><ol className="activity-list">{events.map((event) => <li key={event.id}><div className="activity-meta"><strong>{event.stageLabel}</strong><span>{event.timestamp}</span></div><p>{event.summary}</p>{event.linkedRequestId && <Button aria-label={`查看历史流量 ${event.linkedRequestId}`} onClick={() => onTraffic(event.linkedRequestId!)}>{event.linkedRequestId}</Button>}</li>)}</ol></section>
      {data.retests.some((item) => item.findingId === finding.id) && <section><h3>已保存的复测记录</h3>{data.retests.filter((item) => item.findingId === finding.id).map((item) => <div className="history-entry" key={item.id}><strong>{item.conclusion}</strong><p>{item.conclusionBasis}</p><p className="muted">{item.executedAt} · {item.boundFindingRevision}</p><Button aria-label={`查看复测证据 ${item.linkedRequestId}`} onClick={() => onTraffic(item.linkedRequestId)}>{item.linkedRequestId}</Button></div>)}</section>}
    </div>}
  </article>
}

export function ModelWorkspace({ calls, selectedId, onSelect, onTraffic }: { calls: DemoModelCall[]; selectedId: string | null; onSelect: (id: string) => void; onTraffic: (id: string) => void }) {
  const call = calls.find((item) => item.id === selectedId) ?? calls[calls.length - 1]
  return <div className="records-layout"><aside className="object-list" aria-label="模型运行列表"><div className="pane-heading"><strong>运行与步骤</strong></div>{calls.slice().reverse().map((item) => <button key={item.id} aria-label={`选择模型记录 ${item.id}`} className={`object-item ${item.id === call?.id ? 'active' : ''}`} onClick={() => onSelect(item.id)}><span className="object-kicker">{item.id}</span><strong>{item.purpose} · {item.turnLabel}</strong><span>{item.status}</span><span className="muted">{item.recordingEnabled ? '已保存输入与返回' : '原文未记录'}</span></button>)}</aside>{call ? <ModelDocument key={call.id} call={call} onTraffic={onTraffic} /> : <div className="empty-state"><strong>暂无模型记录</strong><span>目标流量与模型记录独立保存。</span></div>}</div>
}
function ModelDocument({ call, onTraffic }: { call: DemoModelCall; onTraffic: (id: string) => void }) {
  const [tab, setTab] = useState('输入')
  return <section className="model-document"><header><span className="eyebrow">{call.id} · {call.recordedAt}</span><h2>{call.threadTitle}</h2><div className="model-meta"><span>{call.modelName}</span><span>{call.status}</span><span>{call.durationMs === 0 ? '未执行模型调用' : `记录耗时 ${call.durationMs} ms`}</span></div></header>
    <div className="model-context"><p>{tab === '输入' ? call.inputSummary : call.outputSummary}</p><GridFold title="上下文与关联流量"><p>{call.contextIsolationNote}</p><div className="link-list">{call.linkedRequestIds.map((id) => <Button key={id} aria-label={`查看模型关联流量 ${id}`} onClick={() => onTraffic(id)}>{id}</Button>)}</div></GridFold></div>
    <div className="model-tabs"><Segmented aria-label="模型输入输出视图" value={tab} onChange={(value) => setTab(String(value))} options={['输入', '返回']} /><span className="muted">{call.recordingEnabled ? '保存的示例原文' : '原文未记录 · 启动时关闭'}</span>{call.recordingEnabled && <CopyButton label={`复制模型${tab}原文`} text={(tab === '输入' ? call.actualInputText : call.actualOutputText) ?? '没有保存的原文'} />}</div>
    {call.recordingEnabled ? <pre className="model-code" tabIndex={0} aria-label={`模型${tab}原文`}>{(tab === '输入' ? call.actualInputText : call.actualOutputText) ?? '没有保存的原文'}</pre> : <div className="empty-state"><strong>未记录模型原文</strong><span>本次任务创建时关闭记录。调用元数据与目标流量仍保留，历史原文无法补录。</span></div>}
  </section>
}
