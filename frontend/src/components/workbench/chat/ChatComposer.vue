<template>
  <div class="bs-chat-composer-shell" @dragover.prevent="dragging = true" @dragleave.self="dragging = false" @drop.prevent="dropFiles">
    <div v-if="dragging" class="bs-chat-drop" @dragleave="dragging = false">将文件拖到这里</div>
    <div class="bs-chat-composer" :class="{ elevated }">
      <div v-if="attachments.length || editTarget" class="bs-chat-attachment-strip"><div v-if="editTarget" class="bs-chat-attachment bs-chat-edit-target"><Icon name="edit" size="md" /><span :title="editTarget.name">继续修改：{{ editTarget.name }}</span><button type="button" class="bs-chat-icon" :disabled="busy" aria-label="取消修改此文件" @click="emit('clearEdit')"><Icon name="x" size="sm" /></button></div>
        <div v-for="file in attachments" :key="file.version_id" class="bs-chat-attachment"><Icon name="document" size="md" /><span :title="file.name">{{ file.name }}</span><button type="button" class="bs-chat-icon" :disabled="busy" :aria-label="`移除${file.name}`" @click="emit('removeAttachment', file.version_id)"><Icon name="x" size="sm" /></button></div>
      </div>
      <TextArea ref="editor" :model-value="modelValue" :disabled="disabled" :placeholder="waiting ? '请先回答上方的问题' : taskMode ? '描述你的任务，或添加需要处理的文件…' : '有什么可以帮你？'" :rows="1" :maxlength="32768" aria-label="消息" @update:model-value="emit('update:modelValue', $event)" @keydown="keyDown" @input="resize" @paste="pasteFiles" />
      <div class="bs-chat-composer-toolbar">
        <div class="bs-chat-composer-leading">
          <button ref="menuButton" type="button" class="bs-chat-add" aria-label="添加附件和工具" :aria-expanded="menuOpen" :disabled="disabled" @click="menuOpen = !menuOpen"><Icon name="plus" size="sm" /></button>
          <button v-if="taskMode" type="button" class="bs-chat-capability" :disabled="busy || attachments.length > 0" :title="attachments.length ? '移除附件后可退出任务模式' : '退出任务模式'" aria-pressed="true" @click="emit('update:taskMode', false)"><Icon name="clipboard" size="sm" />任务模式<Icon name="x" size="sm" /></button>
          <button v-if="webSearch" type="button" class="bs-chat-capability" :disabled="busy" aria-pressed="true" @click="emit('update:webSearch', false)"><Icon name="globe" size="sm" />联网搜索<Icon name="x" size="sm" /></button>
          <span v-if="uploading" class="bs-chat-muted"><Icon class="bs-chat-spin" name="refresh" size="sm" />正在上传…</span>
        </div>
        <div class="bs-chat-composer-trailing">
          <Select class="bs-chat-model-select" :model-value="model" :options="modelOptions" :disabled="busy" searchable aria-label="选择模型" @update:model-value="emit('update:model', String($event))" />
          <Select v-if="effortOptions.length" class="bs-chat-effort-select" :model-value="effort" :options="effortOptions" :disabled="busy" aria-label="推理强度" @update:model-value="emit('update:effort', String($event))" />
          <button v-if="busy && !waiting" type="button" class="bs-chat-submit" title="停止生成" aria-label="停止生成" @click="emit('stop')"><span class="bs-chat-stop-square"></span></button>
          <button v-else type="button" class="bs-chat-submit" title="发送" aria-label="发送消息" :disabled="disabled || busy || uploading || !modelValue.trim() || !model" @click="emit('send')"><Icon name="arrowUp" size="md" /></button>
        </div>
      </div>
    </div>
    <p v-if="error" class="bs-chat-small-error" role="alert">{{ error }}</p>
    <input ref="fileInput" type="file" multiple hidden :accept="accept" @change="pickFiles" />
    <Teleport to="body">
      <div v-if="menuOpen" class="bs-chat-popup-backdrop" @click="menuOpen = false"></div>
      <div v-if="menuOpen" class="bs-chat bs-chat-command-menu" :style="menuStyle" role="menu" @keydown.esc="menuOpen = false">
        <button v-if="officeEnabled" type="button" role="menuitemcheckbox" :aria-checked="taskMode" :disabled="busy || taskMode && attachments.length > 0" @click="emit('update:taskMode', !taskMode); menuOpen = false"><Icon name="clipboard" size="sm" /><span>任务模式<small>规划并处理办公任务</small></span><Icon v-if="taskMode" name="check" size="sm" /></button>
        <button v-if="searchEnabled" type="button" role="menuitemcheckbox" :aria-checked="webSearch" :disabled="busy" @click="emit('update:webSearch', !webSearch); menuOpen = false"><Icon name="globe" size="sm" /><span>联网搜索<small>查找并引用互联网资料</small></span><Icon v-if="webSearch" name="check" size="sm" /></button>
        <button v-if="uploadsEnabled" type="button" role="menuitem" :disabled="busy || uploading" @click="fileInput?.click(); menuOpen = false"><Icon name="upload" size="sm" /><span>上传文件<small>添加需要阅读或处理的文件</small></span></button>
        <p v-if="!officeEnabled && !searchEnabled && !uploadsEnabled" class="bs-chat-muted">当前未启用其他工具</p>
      </div>
    </Teleport>
  </div>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import Icon from '@/components/icons/Icon.vue'
import type { ChatModel, ChatResource } from '@/types/workbenchChat'
const props = defineProps<{ modelValue: string; model: string; models: ChatModel[]; effort: string; taskMode: boolean; webSearch: boolean; attachments: ChatResource[]; editTarget?: { version_id: string; name: string } | null; disabled?: boolean; busy?: boolean; waiting?: boolean; elevated?: boolean; uploading?: boolean; error?: string; officeEnabled?: boolean; searchEnabled?: boolean; uploadsEnabled?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; 'update:model': [value: string]; 'update:effort': [value: string]; 'update:taskMode': [value: boolean]; 'update:webSearch': [value: boolean]; send: []; stop: []; files: [files: File[]]; removeAttachment: [id: string]; clearEdit: [] }>()
const editor = ref<InstanceType<typeof TextArea> | null>(null), fileInput = ref<HTMLInputElement | null>(null), menuButton = ref<HTMLButtonElement | null>(null)
const menuOpen = ref(false), dragging = ref(false)
const accept = '.pdf,.docx,.xlsx,.pptx,.csv,.json,.txt,.md,.png,.jpg,.jpeg,.webp'
const modelOptions = computed(() => props.models.map(m => ({ value: m.id, label: m.display_name || m.id })))
const labels: Record<string, string> = { minimal: '极低', low: '低', medium: '中', high: '高', xhigh: '极高', none: '关闭', auto: '默认' }
const effortOptions = computed(() => { const list = props.models.find(m => m.id === props.model)?.reasoning_efforts || []; return list.length ? [{ value: '', label: '推理强度' }, ...list.map(v => ({ value: v, label: labels[v] || v }))] : [] })
const menuStyle = ref<Record<string, string>>({})
function positionMenu() { const rect = menuButton.value?.getBoundingClientRect(); if (rect) menuStyle.value = { left: `${Math.max(12, Math.min(rect.left, window.innerWidth - 300))}px`, bottom: `${window.innerHeight - rect.top + 10}px`, maxHeight: `${Math.max(150, rect.top - 24)}px` } }
watch(menuOpen, value => { if (value) positionMenu() })
function closeMenu() { menuOpen.value = false }
window.addEventListener('resize', closeMenu)
onBeforeUnmount(() => window.removeEventListener('resize', closeMenu))
function keyDown(event: KeyboardEvent) { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && event.keyCode !== 229) { event.preventDefault(); if (!props.disabled && !props.busy && !props.uploading && props.modelValue.trim()) emit('send') } }
function resize(event?: Event) { const area = event?.target instanceof HTMLTextAreaElement ? event.target : editor.value?.$el?.querySelector('textarea'); if (area) { area.style.height = 'auto'; area.style.height = `${Math.min(area.scrollHeight, 220)}px` } }
watch(() => props.modelValue, () => nextTick(() => resize()))
function pickFiles(event: Event) { const input = event.target as HTMLInputElement; if (input.files?.length) emit('files', Array.from(input.files)); input.value = '' }
function dropFiles(event: DragEvent) { dragging.value = false; if (props.uploadsEnabled && !props.busy && event.dataTransfer?.files.length) emit('files', Array.from(event.dataTransfer.files)) }
function pasteFiles(event: ClipboardEvent) { const files = Array.from(event.clipboardData?.files || []); if (files.length && props.uploadsEnabled && !props.busy) { event.preventDefault(); emit('files', files) } }
defineExpose({ focus: () => editor.value?.focus() })
</script>
