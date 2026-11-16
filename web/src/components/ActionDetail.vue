<script setup lang="ts">
import { computed } from 'vue'
import { ArrowUpRight, Check, ShieldAlert, X } from 'lucide-vue-next'
import Modal from './Modal.vue'
import EvidenceBody from './EvidenceBody.vue'
import { dateTime, needsResolution, statusText, statusTone } from '../types'
import type { Action, Snapshot } from '../types'
const props = defineProps<{
  action: Action
  context?: Snapshot
  contextLoading: boolean
  draft: { decision: string; resolution: string; checked: boolean }
  busy: boolean
  error: string
}>()
const emit = defineEmits<{
  close: []
  reload: []
  decision: [decision: 'approved' | 'rejected', reason: string]
  resolve: [note: string]
  thread: [threadId: string]
}>()
const charset = computed(() => {
  const headers = props.action.result?.headers || {}
  const contentType = Object.entries(headers).find(([key]) => key.toLowerCase() === 'content-type')?.[1]?.join(';') || ''
  return /charset\s*=\s*["']?([^;\s"']+)/i.exec(contentType)?.[1]
})
const headersText = (headers: Record<string, string | string[]> | undefined) => Object.entries(headers || {})
  .flatMap(([key, value]) => (Array.isArray(value) ? value : [value]).map((item) => `${key}: ${item}`)).join('\n')
</script>

<template>
  <Modal :open="true" title="动作详情" wide @close="emit('close')">
    <div class="action-detail-top">
      <span class="badge" :data-tone="statusTone(action.status)">{{ statusText(action.status) }}{{ action.resolved_at ? ' · 已人工核对' : '' }}</span>
      <span class="caption">{{ action.input.kind === 'http' ? '受控 HTTP' : '工具命令' }} · {{ dateTime(action.created_at) }}</span>
      <button type="button" class="text-button" :disabled="busy" @click="emit('thread', action.thread_id)">查看所属研究方向<ArrowUpRight :size="15" /></button>
    </div>
    <div v-if="context" class="action-context">
      <p><strong>{{ context.mission.title }}</strong><span class="muted"> / {{ context.thread.title }}</span></p>
      <div class="origin-chips"><span class="caption">授权 origins</span><code v-for="origin in context.mission.allowed_origins" :key="origin">{{ origin }}</code></div>
      <p v-if="context.thread.status === 'paused'" class="caption">所属方向已暂停：批准后也不会派发，需先到研究方向恢复。</p>
    </div>
    <p v-if="contextLoading" class="caption context-loading" role="status">正在核对所属项目与授权范围…</p>
    <button v-else-if="!context" type="button" class="text-button context-loading" @click="emit('reload')">重新读取审批上下文</button>
    <div class="detail-purpose"><span class="eyebrow">申请目的</span><p class="prose">{{ action.input.reason || '未提供目的' }}</p></div>
    <p v-if="error" class="message danger" role="alert">{{ error }}</p>
    <p v-if="action.error" class="message danger">执行说明：{{ action.error }}</p>
    <p v-if="action.status === 'approved'" class="message neutral">已放行，等待后台执行；暂停的方向需先恢复。</p>
    <div v-if="action.result?.truncated" class="message warning" role="alert"><ShieldAlert :size="18" /><div><strong>证据已被服务端截断，不完整。</strong><p>下方预览和下载均仅包含已保存部分，不能据此宣称完整响应或完整输出。</p></div></div>
    <p v-if="action.result?.effect_unknown || action.status === 'outcome_unknown'" class="message warning">外部效果未知，不代表动作未执行。禁止直接重放；人工核对只解除新工作的阻塞，不会把原动作改为成功。</p>

    <div class="request-response">
      <section class="capture-panel">
        <div class="capture-heading"><span class="eyebrow">已保存的请求</span><span class="caption">参数不可变</span></div>
        <template v-if="action.input.kind === 'http'">
          <h3 class="request-method">{{ action.input.method || 'GET' }}</h3>
          <pre class="code-block request-url">{{ action.input.url }}</pre>
          <h4>请求头</h4><pre v-if="Object.keys(action.input.headers || {}).length" class="code-block">{{ headersText(action.input.headers) }}</pre><p v-else class="empty-inline">没有自定义请求头。</p>
          <h4>请求正文</h4><pre v-if="action.input.body" class="code-block evidence-preview">{{ action.input.body }}</pre><p v-else class="empty-inline">请求正文为空。</p>
        </template>
        <template v-else>
          <h4>命令</h4><pre class="code-block evidence-preview">{{ action.input.command }}</pre>
          <p class="caption">在命令容器中运行；网络模式见环境配置，直连流量尚未接入请求拦截。</p>
        </template>
      </section>
      <section class="capture-panel">
        <div class="capture-heading"><span class="eyebrow">已保存的{{ action.input.kind === 'http' ? '响应' : '输出' }}</span><span class="caption">{{ action.result ? `${action.result.duration_ms} ms` : '暂无回执' }}</span></div>
        <template v-if="action.result">
          <template v-if="action.input.kind === 'http'">
            <h3 class="request-method">{{ action.result.status_code ? `HTTP ${action.result.status_code}` : '未收到 HTTP 状态码' }}</h3>
            <p v-if="action.result.resolved_ip" class="caption mono">连接 IP · {{ action.result.resolved_ip }}</p>
            <h4>响应头</h4><pre v-if="Object.keys(action.result.headers || {}).length" class="code-block">{{ headersText(action.result.headers) }}</pre><p v-else class="empty-inline">未保存响应头。</p>
            <EvidenceBody :key="`${action.id}-body`" :value="action.result.body_base64" label="响应正文" :charset="charset" :filename="`${action.id}-response.bin`" />
          </template>
          <template v-else>
            <p class="caption">退出码：<code>{{ action.result.exit_code }}</code>（是否完成以动作状态为准）</p>
            <EvidenceBody :key="`${action.id}-stdout`" :value="action.result.stdout_base64" label="标准输出" :filename="`${action.id}-stdout.bin`" />
            <EvidenceBody :key="`${action.id}-stderr`" :value="action.result.stderr_base64" label="标准错误" :filename="`${action.id}-stderr.bin`" />
          </template>
        </template>
        <div v-else class="capture-empty"><p>{{ action.status === 'rejected' ? '此请求已拒绝，没有执行回执。' : '尚无已保存的执行回执。' }}</p><span class="caption">未执行、执行中或回执缺失不能被当作空响应。</span></div>
      </section>
    </div>
    <p class="caption capture-disclaimer">HTTP 内容为结构化捕获，不是原始线上报文。响应与命令输出是观察材料，不等于已确认漏洞。</p>
    <details class="technical-details"><summary>请求标识与完整性</summary><dl class="metadata"><dt>动作 ID</dt><dd class="mono">{{ action.id }}</dd><dt>方向 ID</dt><dd class="mono">{{ action.thread_id }}</dd><dt>来源</dt><dd>{{ action.attempt_id ? '研究执行提出' : '人工提出' }}</dd><dt v-if="action.attempt_id">执行 ID</dt><dd v-if="action.attempt_id" class="mono">{{ action.attempt_id }}</dd><dt>请求 SHA-256</dt><dd class="mono">{{ action.request_hash }}</dd><dt>最近更新</dt><dd>{{ dateTime(action.updated_at) }}</dd></dl></details>

    <section v-if="action.status === 'pending'" class="decision-panel">
      <h3>审批这一个请求</h3>
      <p class="caption">批准绑定上方不可变参数，后台将异步执行。请确认目的、授权范围与可能影响；本次批准不扩大项目授权。</p>
      <label :for="`decision-${action.id}`">审批说明 <span class="muted">（拒绝时必填）</span></label>
      <textarea :id="`decision-${action.id}`" v-model="draft.decision" :disabled="busy" rows="3" placeholder="说明批准依据，或为何不应执行此请求" />
      <div class="form-actions">
        <button type="button" class="button danger-button" :disabled="busy || !draft.decision.trim()" @click="emit('decision', 'rejected', draft.decision.trim())"><X :size="16" />拒绝请求</button>
        <button type="button" class="button primary" :disabled="busy || contextLoading || !context" @click="emit('decision', 'approved', draft.decision.trim())"><Check :size="16" />{{ busy ? '正在提交…' : '批准此请求' }}</button>
      </div>
    </section>
    <form v-if="needsResolution(action)" class="decision-panel" @submit.prevent="draft.checked && draft.resolution.trim() && emit('resolve', draft.resolution.trim())">
      <h3>人工核对未知效果</h3>
      <p class="caption">先在独立证据中核对是否产生效果，再记录核对依据和后续安排。提交不会自动重放、批准动作或启用自动研究。</p>
      <label :for="`resolve-${action.id}`">核对记录 <span class="required">必填</span></label>
      <textarea :id="`resolve-${action.id}`" v-model="draft.resolution" :disabled="busy" rows="4" required placeholder="记录实际核对的证据、仍存在的不确定性与后续安排" />
      <label class="checkbox-label"><input v-model="draft.checked" type="checkbox" :disabled="busy" required />我已人工核对，理解原动作仍保留“效果未知”历史。</label>
      <div class="form-actions"><button class="button primary" :disabled="busy || !draft.checked || !draft.resolution.trim()">{{ busy ? '正在提交…' : '保存核对记录' }}</button></div>
    </form>
    <section v-if="action.decision_reason || action.resolved_at" class="decision-history">
      <template v-if="action.decision_reason"><h4>放行／待审依据</h4><p class="prose">{{ action.decision_reason }}</p></template>
      <template v-if="action.resolved_at"><h4>人工核对 · {{ dateTime(action.resolved_at) }}</h4><p class="prose">{{ action.resolution_note }}</p><p class="caption">已允许后续新工作；原动作仍为效果未知，未被重放或改判成功。</p></template>
    </section>
  </Modal>
</template>
