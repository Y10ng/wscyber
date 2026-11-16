import { useRef, useState } from 'react'
import { Alert, Button, Form, Input, Popconfirm, Radio, Space } from 'antd'
import { TaskModal } from '../ui/TaskModal.tsx'
import { GridFold } from '../ui/SurfaceControls.tsx'
import { Example } from './examples.tsx'

type SampleState = '编辑' | '提交中' | '待核对' | '已接收'
export function ModalExamples() {
  const [open, setOpen] = useState(false), [detailOpen, setDetailOpen] = useState(false)
  const [draft, setDraft] = useState(''), [state, setState] = useState<SampleState>('编辑')
  const pending = useRef(false), trigger = useRef<HTMLButtonElement>(null)
  return <Example title="任务弹窗与提交状态"><Space wrap>
    <Button ref={trigger} type="primary" onClick={() => setOpen(true)}>打开任务样例</Button>
    <Popconfirm title="清空本地草稿？" description="仅清空当前样例，不发请求。" okText="清空" cancelText="取消" onConfirm={() => { setDraft(''); setState('编辑') }}><Button danger>清空草稿</Button></Popconfirm>
  </Space><p className="muted">状态由样例按钮切换；不创建任务、不核对服务端回执。</p>
    <TaskModal title="任务表单样例" open={open} onCancel={() => setOpen(false)} focusable={{ focusTriggerAfterClose: !pending.current }} afterClose={() => { if (pending.current) { pending.current = false; setDetailOpen(true) } }} footer={<div className="modal-footer"><Button onClick={() => setOpen(false)}>保留草稿并关闭</Button><Button onClick={() => { pending.current = true; setOpen(false) }}>查看约束</Button></div>}>
      <Form layout="vertical"><Form.Item label={<label htmlFor="sample-draft">目标／已知信息</label>}><Input.TextArea id="sample-draft" value={draft} onChange={(e) => setDraft(e.target.value)} disabled={state !== '编辑'} rows={4} placeholder="关闭后保留，刷新后清空" /></Form.Item>
        <Form.Item label="手动切换展示状态"><Radio.Group value={state} onChange={(e) => setState(e.target.value)} options={['编辑', '提交中', '待核对', '已接收']} /></Form.Item>
        {state === '提交中' && <Button loading>提交中（示例）</Button>}
        {state === '待核对' && <Alert type="warning" showIcon title="提交回执未知（示例）" description="404不证明在途提交失败；先核对，不自动重发。" />}
        {state === '已接收' && <Alert type="info" showIcon title="202已接收（示例）" description="不表示已运行。" />}
        <GridFold title="长表单样例">{Array.from({ length: 10 }, (_, i) => <Form.Item key={i} label={`扩展字段 ${i + 1}`}><Input aria-label={`扩展字段 ${i + 1}`} /></Form.Item>)}</GridFold>
      </Form>
    </TaskModal>
    <TaskModal title="样例约束" width={640} open={detailOpen} onCancel={() => setDetailOpen(false)} focusable={{ focusTriggerAfterClose: false }} afterClose={() => trigger.current?.focus({ preventScroll: true })} footer={<Button onClick={() => setDetailOpen(false)}>关闭约束</Button>}><p>草稿由调用方持有；网络请求、授权校验、幂等键和核对均不属于Modal组件。</p></TaskModal>
  </Example>
}
