import { useState } from 'react'
import { Button, Checkbox, DatePicker, Form, Input, InputNumber, Radio, Select, Space, Switch, TimePicker, Upload } from 'antd'
import { UploadOutlined } from '@ant-design/icons'
import { Example } from './examples.tsx'

export function FormExamples() {
  const [form] = Form.useForm()
  const [disabled, setDisabled] = useState(false), [saved, setSaved] = useState(false)
  return <Example title="表单与配置"><div className="form-toolbar"><Switch aria-label="禁用示例表单" checked={disabled} onChange={setDisabled} /> 禁用控件</div>
    <Form form={form} layout="vertical" disabled={disabled} requiredMark={false} scrollToFirstError={{ focus: true }} onValuesChange={() => setSaved(false)} onFinish={() => setSaved(true)} initialValues={{ name: '', kind: 'http', mode: 'manual', retain: false, number: 2 }}>
      <div className="form-grid">
        <Form.Item label="名称" name="name" rules={[{ required: true, message: '请输入名称' }]}><Input placeholder="本地样例名称" /></Form.Item>
        <Form.Item label="临时口令" name="password"><Input.Password autoComplete="off" placeholder="仅内存，不持久化" /></Form.Item>
        <Form.Item label="备注" name="note"><Input.TextArea rows={3} placeholder="长文本自动换行" /></Form.Item>
        <Form.Item label="数量" name="number"><InputNumber min={1} max={20} /></Form.Item>
        <Form.Item label="搜索类型" name="kind"><Select showSearch options={[{ label: '平台HTTP', value: 'http' }, { label: '本地命令', value: 'command' }, { label: '不可用类型', value: 'disabled', disabled: true }]} /></Form.Item>
        <Form.Item label="多选资料" name="materials"><Select mode="multiple" showSearch options={[{ label: '双身份对照.md', value: 'identity' }, { label: '较长的资料名称用于检查多选布局.txt', value: 'long' }]} /></Form.Item>
        <Form.Item label="选择模式" name="mode"><Radio.Group options={[{ label: '手动', value: 'manual' }, { label: '自动', value: 'auto' }]} /></Form.Item>
        <Form.Item label="保留选项" name="retain" valuePropName="checked"><Checkbox>本地状态开关</Checkbox></Form.Item>
        <Form.Item label="日期" name="date"><DatePicker /></Form.Item>
        <Form.Item label="时间" name="time"><TimePicker /></Form.Item>
        <Form.Item label="本地文件（不上传）"><Upload beforeUpload={() => false} maxCount={1}><Button icon={<UploadOutlined />}>选择文件</Button></Upload></Form.Item>
      </div>
      <Space wrap><Button type="primary" htmlType="submit">校验表单</Button><Button onClick={() => { form.resetFields(); setSaved(false) }}>重置示例</Button>{saved && <span role="status">校验通过，未发送任何请求。</span>}</Space>
    </Form>
  </Example>
}
