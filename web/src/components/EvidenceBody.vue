<script setup lang="ts">
import { computed, ref } from 'vue'
import { Download } from 'lucide-vue-next'
const props = defineProps<{ value?: string; label: string; filename: string; charset?: string }>()
const mode = ref('auto')
const limit = 16_384
const decoded = computed(() => {
  try {
    const binary = atob(props.value || '')
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
    let text: string | null = null
    try {
      const value = new TextDecoder(props.charset || 'utf-8', { fatal: true }).decode(bytes)
      if (!/[\u0000-\u0008\u000b\u000c\u000e-\u001f]/.test(value)) text = value
    } catch { /* 非文本原件以十六进制展示，不替换坏字节。 */ }
    return { bytes, text, error: '' }
  } catch {
    return { bytes: new Uint8Array(), text: null, error: 'Base64 数据无效，无法解码；请检查服务端保存的原件。' }
  }
})
const displayMode = computed(() => mode.value === 'auto' ? (decoded.value.text === null ? 'hex' : 'text') : mode.value)
const preview = computed(() => {
  if (displayMode.value === 'base64') return (props.value || '').slice(0, limit)
  if (displayMode.value === 'text') return (decoded.value.text ?? '').slice(0, limit)
  const bytes = decoded.value.bytes.subarray(0, limit)
  const rows: string[] = []
  for (let i = 0; i < bytes.length; i += 16) {
    rows.push(`${i.toString(16).padStart(8, '0')}  ${Array.from(bytes.subarray(i, i + 16), (b) => b.toString(16).padStart(2, '0')).join(' ')}`)
  }
  return rows.join('\n')
})
const previewLimited = computed(() => displayMode.value === 'text'
  ? (decoded.value.text?.length || 0) > limit
  : displayMode.value === 'base64' ? (props.value?.length || 0) > limit : decoded.value.bytes.length > limit)
function download() {
  const blob = new Blob([decoded.value.bytes as BlobPart], { type: 'application/octet-stream' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = props.filename.replace(/[^a-zA-Z0-9._-]/g, '_')
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
</script>

<template>
  <section class="evidence-body">
    <div class="section-heading compact">
      <h4>{{ label }} <span class="muted weight-normal">{{ decoded.bytes.length.toLocaleString('zh-CN') }} 字节</span></h4>
      <button type="button" class="text-button" :disabled="!!decoded.error" @click="download"><Download :size="14" />下载已保存字节</button>
    </div>
    <p v-if="decoded.error" class="message danger" role="alert">{{ decoded.error }}</p>
    <template v-else-if="decoded.bytes.length">
      <div class="body-format">
        <label>显示格式
          <select v-model="mode">
            <option value="auto">自动（{{ decoded.text === null ? '二进制' : charset || 'UTF-8' }}）</option>
            <option value="text" :disabled="decoded.text === null">解码文本</option>
            <option value="hex">十六进制</option>
            <option value="base64">Base64 原编码</option>
          </select>
        </label>
      </div>
      <pre class="code-block evidence-preview">{{ preview }}</pre>
      <p v-if="decoded.text === null" class="caption">不是可无损显示的文本（或字符集不受支持），默认使用十六进制保留字节含义。</p>
      <p v-if="previewLimited" class="message warning">仅预览前 {{ limit.toLocaleString('zh-CN') }} 个{{ displayMode === 'hex' ? '字节' : '字符' }}；下载可获得全部已保存字节，不代表服务端证据未被截断。</p>
    </template>
    <p v-else class="empty-inline">已保存内容为空（0 字节）。</p>
  </section>
</template>
