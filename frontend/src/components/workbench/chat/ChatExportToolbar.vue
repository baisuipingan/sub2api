<template>
  <div class="bs-chat bs-chat-export-toolbar" role="toolbar" aria-label="导出对话">
    <label class="bs-chat-export-all" title="全选"><input type="checkbox" :checked="allSelected" :indeterminate="!!selectedCount && !allSelected" :disabled="!!busy" @change="emit('toggleAll')" /><span>全选</span></label>
    <span class="bs-chat-export-selected">已选 {{ selectedCount }} 条</span>
    <div class="bs-chat-export-spacer"></div>
    <div class="bs-chat-export-desktop-formats"><button v-for="format in chatExportFormats" :key="format.value" type="button" :disabled="!!busy || !selectedCount" :title="format.hint || format.label" :aria-label="format.label" @click="download(format.value)"><Icon :name="busy === format.value ? 'refresh' : 'document'" :class="{ 'bs-chat-spin': busy === format.value }" size="sm" :style="{ color: format.color }" /><span>{{ format.label }}</span></button></div>
    <button type="button" class="bs-chat-export-mobile" :disabled="!selectedCount" title="导出到本地" aria-label="导出到本地" @click="sheetOpen = true"><Icon name="download" size="sm" /><span>导出到本地</span></button>
    <button type="button" class="bs-chat-export-exit" :title="busy === 'pdf' ? '取消打印并退出选择' : '退出选择'" aria-label="退出选择" @click="close"><Icon name="x" size="sm" /></button>
    <p v-if="error" role="alert" class="bs-chat-small-error">{{ error }}</p>
    <ChatExportDialog :show="sheetOpen" :messages="messages" :title="title" @close="sheetOpen = false" @exported="emit('exported')" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { useAuthStore } from '@/stores/auth'
import ChatExportDialog from './ChatExportDialog.vue'
import { chatExportFormats, createChatExporter, exportableChatMessages } from './chatExport'
import type { ChatExportFormat } from './chatExport'
import type { ChatMessage } from '@/types/workbenchChat'
const props = defineProps<{ messages: ChatMessage[]; title?: string; allSelected?: boolean }>()
const emit = defineEmits<{ close: []; exported: []; toggleAll: [] }>()
const selectedCount = computed(() => exportableChatMessages(props.messages).length), busy = ref<ChatExportFormat | null>(null), error = ref(''), sheetOpen = ref(false)
const auth = useAuthStore()
let exporter = createChatExporter(), epoch = 0
function reset() { ++epoch; exporter.dispose(); exporter = createChatExporter(); busy.value = null; error.value = ''; sheetOpen.value = false }
function close() { reset(); emit('close') }
async function download(format: ChatExportFormat) {
  if (busy.value || !selectedCount.value) return
  const own = epoch
  busy.value = format; error.value = ''
  try { await exporter.download(format, { messages: props.messages, title: props.title }); if (own === epoch) emit('exported') } catch (reason) { if (own === epoch && (!(reason instanceof DOMException) || reason.name !== 'AbortError')) error.value = reason instanceof Error ? reason.message : '导出失败，请重试' } finally { if (own === epoch) busy.value = null }
}
watch(() => auth.user?.id, () => { reset(); emit('close') })
onBeforeUnmount(() => { ++epoch; exporter.dispose() })
</script>

<style scoped>
.bs-chat-export-toolbar { display:flex; align-items:center; flex-wrap:wrap; gap:6px; width:100%; padding:8px 12px 8px 10px; border:1px solid var(--bc-border); border-radius:12px; background:var(--bc-page); box-shadow:0 6px 20px rgb(0 0 0 / 8%); }
.bs-chat-export-all { display:flex; flex:none; align-items:center; gap:8px; color:var(--bc-ink); font-size:13px; cursor:pointer; }.bs-chat-export-all input { width:16px; height:16px; accent-color:var(--bc-blue); }.bs-chat-export-selected { color:var(--bc-muted); font-size:12px; white-space:nowrap; }.bs-chat-export-spacer { flex:1; }
.bs-chat-export-desktop-formats { display:flex; flex-wrap:wrap; gap:6px; }.bs-chat-export-toolbar button { display:inline-flex; flex:none; align-items:center; justify-content:center; gap:4px; min-height:28px; padding:3px 8px; border:1px solid var(--bc-border); border-radius:6px; color:var(--bc-ink); background:transparent; font-size:12px; cursor:pointer; }
.bs-chat-export-toolbar button:hover:enabled { background:var(--bc-fill-2); }.bs-chat-export-toolbar button:focus-visible { outline:2px solid var(--bc-blue); outline-offset:2px; }
.bs-chat-export-toolbar .bs-chat-export-exit { width:28px; padding:4px; border:0; color:var(--bc-muted); }.bs-chat-export-toolbar .bs-chat-export-mobile { display:none; }.bs-chat-export-toolbar .bs-chat-small-error { width:100%; margin:0; }
@media(max-width:1023px) and (min-width:768px) { .bs-chat-export-desktop-formats button span { display:none; } }
@media(max-width:767px) { .bs-chat-export-desktop-formats { display:none; }.bs-chat-export-toolbar .bs-chat-export-mobile { display:inline-flex; }.bs-chat-export-toolbar { padding-right:10px; } }
@media(max-width:399px) { .bs-chat-export-all>span,.bs-chat-export-mobile>span { display:none; } }
</style>
