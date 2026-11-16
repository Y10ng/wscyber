import type { WebApiFindingDetail } from '../api'

// 所有候选及证据字段都是不可信文本；围栏比内容中最长反引号串更长，
// 避免 Markdown、HTML 或远程资源被当作活动内容。正文不截断。
function textBlock(value: unknown, missing = '未提供'): string {
  const text = value == null || value === '' ? missing : String(value)
  const longest = (text.match(/`+/g) ?? []).reduce((max, run) => Math.max(max, run.length), 0)
  const fence = '`'.repeat(Math.max(3, longest + 1))
  return `${fence}text\n${text}\n${fence}`
}

function field(label: string, value: unknown, missing?: string): string {
  return `### ${label}\n\n${textBlock(value, missing)}\n`
}

function flag(value: boolean | null | undefined): string {
  return value === true ? '是' : value === false ? '否' : '未知'
}

/** 仅使用当前详情的固定修订和证据快照，绝不读取原件或请求最新版本。 */
export function buildFindingDraft(detail: WebApiFindingDetail): { filename: string; markdown: string } {
  if (!/^WSC-\d{6,}$/.test(detail.number) || !/[1-9]/.test(detail.number) ||
      !Number.isSafeInteger(detail.revision) || detail.revision < 1 ||
      !detail.task_id?.trim() || !detail.finding_id?.trim() || !detail.revision_id?.trim()) {
    throw new Error('Invalid finding identity')
  }

  const evidence = detail.evidence ?? []
  const content = detail.content
  const parts = [
    '# 候选草稿（未确认，非正式发布）\n',
    '正文可能含敏感信息，分享前请检查。本文件是当前阅读修订的确定性草稿，不代表最新状态。\n',
    field('编号', detail.number),
    field('标题', content.title),
    '## 来源\n',
    field('任务 ID', detail.task_id),
    field('发现 ID', detail.finding_id),
    field('修订号', detail.revision),
    field('修订 ID', detail.revision_id),
    field('修订创建时间', detail.revision_created_at),
    field('来源 session', detail.source?.session_id),
    field('来源 turn', detail.source?.turn_id),
    field('来源 call', detail.source?.call_id),
    '## 描述\n',
    `${textBlock(content.description)}\n`,
    '## 记录的复现步骤\n',
    '以下步骤是原候选主张，不是本次执行或复现的证明。\n',
  ]

  if (!content.steps?.length) parts.push('未提供\n')
  for (const [index, step] of (content.steps ?? []).entries()) {
    parts.push(`### 步骤 ${index + 1}\n`, `${textBlock(step.text)}\n`)
    for (const ref of step.evidence_indices ?? []) {
      if (Number.isInteger(ref) && ref >= 0 && ref < evidence.length) {
        parts.push(`证据引用：[${ref + 1}]\n`)
      } else {
        parts.push(`证据索引未解析（零基）：\n\n${textBlock(ref)}\n`)
      }
    }
  }

  parts.push(
    '## 修复建议\n',
    `${textBlock(content.remediation)}\n`,
    '## 附录：限制与证据快照\n',
    '证据快照为候选保存时点的记录，并非当前证据状态。Flow hash 对应已保存的 Flow，不证明线上报文逐字节一致。\n',
    field('原有限制', content.limitations),
  )
  if (!evidence.length) parts.push('未提供证据快照。\n')
  for (const [index, item] of evidence.entries()) {
    parts.push(
      `### 证据 [${index + 1}]\n`,
      field('evidence_id', item.evidence_id),
      field('relation', item.relation),
      field('method', item.method),
      field('status', item.status),
      field('sent', flag(item.sent)),
      field('http_status', item.http_status, '未取得'),
      field('created_at', item.created_at),
      field('completed_at', item.completed_at, '未取得'),
      field('preview_truncated（预览截断）', flag(item.preview_truncated)),
      field('capture_truncated（捕获截断）', flag(item.capture_truncated)),
      field('flow_sha256', item.flow_sha256, '未取得'),
    )
  }

  return { filename: `${detail.number}-r${detail.revision}-draft.md`, markdown: parts.join('\n') }
}
