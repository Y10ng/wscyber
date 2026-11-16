export type ResearchCommand = 'pause' | 'run' | 'resume'
export const controlLabels: Record<ResearchCommand, string> = { pause: '暂停研究', run: '启用模型研究', resume: '仅恢复已批准动作' }
export const controlHelp: Record<ResearchCommand, string> = {
  pause: '停止后续认领；在途执行仍保存真实结果，不代表撤销请求。',
  run: '按任务原绑定模型继续后续研究；不重置累计步数，不批准待审动作，不重放未知结果。',
  resume: '解除暂停，只允许原已批准动作继续派发；不启用模型，不批准待审动作，不重放未知结果。',
}
export interface SupplementDraft { source: string; content: string; share_with_model: boolean }
export interface SupplementMeta { id: string; mission_id: string; thread_id: string; source: string; kind: string; byte_size: number; content_sha256: string; created_at: string; content?: string }
export const emptySupplement = (): SupplementDraft => ({ source: '', content: '', share_with_model: false })
export function supplementError(value: SupplementDraft): string {
  const valid = (s: string, max: number) => s.trim().length > 0 && !s.includes('\0') && !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(s) && new TextEncoder().encode(s).length <= max
  if (!valid(value.source, 256)) return '来源须为 1–256 字节的有效文本。'
  if (!valid(value.content, 16384)) return '补充正文须为 1–16,384 字节的有效文本。'
  if (!value.share_with_model) return '请明确确认提供给后续研究模型；保存不代表已发送。'
  return ''
}
