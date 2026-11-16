import { useMemo, useState } from 'react'
import { Alert, Button, Radio, Typography } from 'antd'
import { DownloadOutlined } from '@ant-design/icons'
import { CopyButton } from './SurfaceControls.tsx'
import { createEvidenceBlob, createEvidencePreview, decodeEvidenceBytes, EVIDENCE_PREVIEW_LIMIT, safeEvidenceFilename, type EvidenceViewMode } from './evidenceBytes.ts'
import './evidence.css'

export interface EvidenceBytesViewerProps {
  base64Value?: string
  label: string
  filename: string
  charset?: string
  /** 项目/方向/动作/流的非敏感身份；不能使用证据内容充当 key。 */
  sourceKey: string
}

export function EvidenceBytesViewer(props: EvidenceBytesViewerProps) {
  return <EvidenceBytesContent key={props.sourceKey} {...props} />
}

function EvidenceBytesContent({ base64Value, label, filename, charset }: EvidenceBytesViewerProps) {
  const [mode, setMode] = useState<EvidenceViewMode>('auto')
  const [source, setSource] = useState({ base64Value, charset, revision: 0 })
  if (source.base64Value !== base64Value || source.charset !== charset) {
    // 在提交新材料前同步重置；即使预览文本相同，旧复制完成也不能显示在新材料上。
    setSource({ base64Value, charset, revision: source.revision + 1 })
    setMode('auto')
  }
  const decoded = useMemo(() => decodeEvidenceBytes(base64Value, charset), [base64Value, charset])
  const preview = useMemo(() => decoded.status === 'available' ? createEvidencePreview(decoded, mode) : null, [decoded, mode])

  const download = () => {
    if (decoded.status !== 'available') return
    const url = URL.createObjectURL(createEvidenceBlob(decoded.bytes))
    const link = document.createElement('a')
    link.href = url
    link.download = safeEvidenceFilename(filename)
    link.click()
    // 附件是完整已保存字节；延迟释放仅服务于浏览器下载，不管理复制反馈。
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  return <section className="evidence-bytes" aria-label={label}>
    <div className="evidence-row">
      <div className="evidence-row-title">
        <Typography.Text strong>{label}</Typography.Text>
        {decoded.status === 'available' && <Typography.Text type="secondary">（已保存 {decoded.bytes.length.toLocaleString('zh-CN')} 字节）</Typography.Text>}
      </div>
      {decoded.status === 'available' && <Button icon={<DownloadOutlined />} onClick={download}>下载已保存字节</Button>}
    </div>
    {decoded.status === 'missing' ? <Typography.Paragraph type="secondary" style={{ margin: 0 }}>未提供已保存字节；可能为空，无法确定长度。</Typography.Paragraph>
      : decoded.status === 'invalid' ? <Alert type="error" showIcon title={decoded.error} />
        : preview && <>
          <Radio.Group className="evidence-modes" aria-label={`${label}显示模式`} value={mode} onChange={(event) => setMode(event.target.value as EvidenceViewMode)} optionType="button" buttonStyle="solid">
            <Radio.Button value="auto">自动（{decoded.text === null ? '十六进制' : '文本'}）</Radio.Button>
            <Radio.Button value="text" disabled={decoded.text === null}>解码文本</Radio.Button>
            <Radio.Button value="hex">十六进制</Radio.Button>
            <Radio.Button value="base64">Base64</Radio.Button>
          </Radio.Group>
          <div className="evidence-copy-action">
            <Typography.Text type="secondary">{preview.copyLabel}</Typography.Text>
            <CopyButton key={`${source.revision}:${mode}`} text={preview.text} label={preview.copyLabel} />
          </div>
          <pre className="evidence-preview" data-evidence-mode={preview.mode} tabIndex={0} aria-label={`${label}当前预览`}>{preview.text}</pre>
          {decoded.bytes.length === 0 && <Typography.Text type="secondary">已保存内容为空（0 字节）。</Typography.Text>}
          {decoded.textIssue && <Typography.Paragraph type="secondary" style={{ margin: 0 }}>{decoded.textIssue}</Typography.Paragraph>}
          {preview.limited && <Alert type="warning" showIcon title={`当前预览仅前 ${EVIDENCE_PREVIEW_LIMIT.toLocaleString('zh-CN')} 个${preview.unit}；复制仅含当前预览，下载包含全部已保存字节。`} />}
        </>}
  </section>
}
