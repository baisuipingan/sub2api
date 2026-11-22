<template>
  <BaseDialog :show="show" title="选择导出格式" width="reading" :z-index="190" :show-close-button="false" :close-on-click-outside="!busy" @close="close">
    <div class="bs-chat bs-chat-export-sheet">
      <button v-for="format in chatExportFormats" :key="format.value" type="button" :disabled="!!busy || !selectedCount" :title="format.hint || format.label" @click="download(format.value)">
        <Icon name="document" size="md" :style="{ color: format.color }" /><span>{{ format.label }}<small v-if="format.value === 'pdf'">使用系统打印，选择“另存为 PDF”</small></span><span v-if="busy === format.value" class="bs-chat-spin"><Icon name="refresh" size="sm" /></span>
      </button>
      <p v-if="error" role="alert" class="bs-chat-small-error">{{ error }}</p>
      <button type="button" class="bs-chat-export-cancel" @click="close">{{ busy === 'pdf' ? '取消打印' : '取消' }}</button>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAuthStore } from '@/stores/auth'
import { chatExportFormats, createChatExporter, exportableChatMessages } from './chatExport'
import type { ChatExportFormat } from './chatExport'
import type { ChatMessage } from '@/types/workbenchChat'
const props = defineProps<{ show: boolean; messages: ChatMessage[]; title?: string }>()
const emit = defineEmits<{ close: []; exported: [] }>()
const selectedCount = computed(() => exportableChatMessages(props.messages).length), busy = ref<ChatExportFormat | null>(null), error = ref('')
const auth = useAuthStore()
let exporter = createChatExporter(), epoch = 0
function reset() { ++epoch; exporter.dispose(); exporter = createChatExporter(); busy.value = null; error.value = '' }
function close() { reset(); emit('close') }
async function download(format: ChatExportFormat) {
  if (!props.show || busy.value || !selectedCount.value) return
  const own = epoch
  busy.value = format; error.value = ''
  try { await exporter.download(format, { messages: props.messages, title: props.title }); if (own === epoch) { emit('exported'); emit('close') } } catch (reason) { if (own === epoch && (!(reason instanceof DOMException) || reason.name !== 'AbortError')) error.value = reason instanceof Error ? reason.message : '导出失败，请重试' } finally { if (own === epoch) busy.value = null }
}
watch(() => props.show, reset)
watch(() => auth.user?.id, () => { reset(); emit('close') })
onBeforeUnmount(() => { ++epoch; exporter.dispose() })
</script>

<style scoped>
.bs-chat-export-sheet { display:flex; flex-direction:column; gap:6px; background:transparent; }
.bs-chat-export-sheet>button { display:flex; align-items:center; gap:12px; width:100%; padding:10px 12px; border:0; border-radius:8px; color:var(--bc-ink); background:transparent; text-align:left; font-size:14px; cursor:pointer; }
.bs-chat-export-sheet>button:hover:enabled { background:var(--bc-fill-2); }.bs-chat-export-sheet>button:focus-visible { outline:2px solid var(--bc-blue); outline-offset:2px; }
.bs-chat-export-sheet>button>span:first-of-type { flex:1; }.bs-chat-export-sheet small { display:block; margin-top:3px; color:var(--bc-muted); font-size:12px; }
.bs-chat-export-sheet .bs-chat-export-cancel { justify-content:center; margin-top:8px; border-top:1px solid var(--bc-border); border-radius:0; color:var(--bc-muted); }
@media(max-width:767px) { :global(.modal-overlay:has(.bs-chat-export-sheet)) { align-items:flex-end; padding:0; }:global(.modal-content:has(.bs-chat-export-sheet)) { width:100%; max-width:100%; margin:0; border-radius:16px 16px 0 0; }:global(.modal-content:has(.bs-chat-export-sheet) .modal-header) { justify-content:center; padding:12px 16px 8px; }:global(.modal-content:has(.bs-chat-export-sheet) .modal-title) { font-size:14px; font-weight:500; }:global(.modal-content:has(.bs-chat-export-sheet) .modal-body) { padding:0 16px max(24px,env(safe-area-inset-bottom)); } }
</style>
