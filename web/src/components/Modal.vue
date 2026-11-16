<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { X } from 'lucide-vue-next'
const props = defineProps<{ open: boolean; title: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement>()
const headingId = useId()
watch(() => props.open, async (open) => {
  await nextTick()
  if (open && !dialog.value?.open) dialog.value?.showModal()
  if (!open && dialog.value?.open) dialog.value?.close()
}, { immediate: true })
onBeforeUnmount(() => { dialog.value?.close() })
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" :aria-labelledby="headingId" :class="['modal', { 'modal-wide': wide }]" @cancel.prevent="emit('close')">
      <div class="modal-heading">
        <h2 :id="headingId">{{ title }}</h2>
        <button class="icon-button" type="button" aria-label="关闭对话框（保留未提交输入）" @click="emit('close')"><X :size="20" /></button>
      </div>
      <div class="modal-content"><slot /></div>
    </dialog>
  </Teleport>
</template>
