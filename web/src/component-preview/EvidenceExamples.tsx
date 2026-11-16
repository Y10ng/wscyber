import { useState } from 'react'
import { Button, Select, Space, Typography } from 'antd'
import { EvidenceBytesViewer } from '../ui/EvidenceBytesViewer.tsx'

function textBase64(value: string) {
  return btoa(Array.from(new TextEncoder().encode(value), (byte) => String.fromCharCode(byte)).join(''))
}

// 仅组件入口内存 fixture，不连接目标或正式业务状态。
const samples: { key: string; name: string; base64Value?: string; charset?: string }[] = [
  { key: 'text', name: 'UTF-8 文本', base64Value: textBase64('已保存的本地样例\n中文与 emoji 😀\n') },
  { key: 'binary', name: '二进制', base64Value: 'AAECA3+A//4=' },
  { key: 'long', name: '超长预览', base64Value: textBase64('A'.repeat(16_383) + '😀后续已保存内容'.repeat(50)) },
  { key: 'empty', name: '显式空字节', base64Value: '' },
  { key: 'missing', name: '未提供字节' },
  { key: 'invalid', name: '非法 Base64', base64Value: 'AA?=' },
  { key: 'charset', name: '未知 charset', base64Value: 'c2FtcGxl', charset: 'x-unknown-charset' },
  { key: 'invalid-utf8', name: '非法 UTF-8', base64Value: '/w==' },
  { key: 'html', name: 'HTML 仅文本', base64Value: textBase64('<img src="https://untrusted.invalid/evidence-pixel" onerror="alert(1)">\n<script>alert(1)</script>') },
  { key: 'bom', name: '带 BOM 的文本', base64Value: textBase64('﻿文本视图不含 BOM；下载保留原字节。') },
]

export function EvidenceExamples() {
  const [selected, setSelected] = useState('text')
  const [record, setRecord] = useState(1)
  const sample = samples.find((item) => item.key === selected)!
  return <section className="example" style={{ gridColumn: '1 / -1' }}>
    <h2>已保存字节查看与当前预览复制</h2>
    <Space wrap>
      <Select aria-label="字节样例" value={selected} style={{ width: 220 }} options={samples.map((item) => ({ value: item.key, label: item.name }))} onChange={setSelected} />
      <Button onClick={() => setRecord((value) => value + 1)}>同内容换记录</Button>
      <Typography.Text type="secondary">本地样例 · 记录 {record}</Typography.Text>
    </Space>
    <EvidenceBytesViewer sourceKey={`component-example:${selected}:${record}`} base64Value={sample.base64Value} charset={sample.charset} label={sample.name} filename={`example-${selected}.bin`} />
  </section>
}
