<template>
  <div class="bs-chat-composer-shell bs-native-composer" @dragenter.prevent="dragEnter" @dragover.prevent="dragOver" @dragleave.prevent="dragLeave" @drop.prevent="dropFiles">
    <div v-if="dragging && uploadsEnabled && !busy" class="bs-composer-drop" aria-hidden="true"><Icon name="upload" size="xl" /><span>将文件拖到这里</span></div>
    <div class="bs-chat-composer" :class="{ elevated, 'has-resources': attachments.length || editTarget || uploading || references.length }">
      <ComposerAttachmentBar :attachments="attachments" :edit-target="editTarget" :uploading="uploading" :disabled="busy" @remove="emit('removeAttachment', $event)" @clear-edit="emit('clearEdit')" />
      <div class="bs-composer-writing"><ComposerEditor ref="editor" :model-value="modelValue" :references="references" :disabled="disabled" :placeholder="waiting ? '请先回答上方的问题' : taskMode ? '描述你的任务，或添加需要处理的文件…' : '有什么可以帮你？'" @update:model-value="emit('update:modelValue', $event)" @references-change="updateReferences" @submit="send" @paste-files="pasteFiles" /></div>
      <div class="bs-chat-composer-toolbar">
        <div class="bs-chat-composer-leading"><ComposerResourceMenu :disabled="disabled || busy" :uploading="uploading" :office-enabled="officeEnabled" :search-enabled="searchEnabled" :uploads-enabled="uploadsEnabled" :resources-enabled="resourcesEnabled" :task-mode="taskMode" :web-search="webSearch" :task-locked="taskMode && !!(attachments.length || editTarget)" @update:task-mode="emit('update:taskMode', $event)" @update:web-search="emit('update:webSearch', $event)" @upload="fileInput?.click()" @resources="pickerOpen = true" @focus-editor="editor?.focus()" /></div>
        <div class="bs-chat-composer-trailing">
          <Select class="bs-chat-model-select" :model-value="model" :options="modelOptions" :disabled="busy" searchable aria-label="选择模型" @update:model-value="emit('update:model', String($event))" />
          <Select v-if="effortOptions.length" class="bs-chat-effort-select" :model-value="effort" :options="effortOptions" :disabled="busy" aria-label="推理强度" @update:model-value="emit('update:effort', String($event))" />
          <button v-if="busy && !waiting" type="button" class="bs-chat-submit" title="停止生成" aria-label="停止生成" @click="emit('stop')"><span class="bs-chat-stop-square"></span></button>
          <button v-else type="button" class="bs-chat-submit" title="发送" aria-label="发送消息" :disabled="!canSend" @click="send"><Icon name="arrowUp" size="md" /></button>
        </div>
      </div>
    </div>
    <p v-if="error" class="bs-chat-small-error" role="alert">{{ error }}</p>
    <input ref="fileInput" type="file" multiple hidden :accept="accept" @change="pickFiles" />
    <ComposerFilePicker v-if="resourcesEnabled" :show="pickerOpen" :attachments="attachments" :max-files="maxFiles" @select="selectResources" @close="closePicker" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ComposerEditor from './ComposerEditor.vue'
import ComposerAttachmentBar from './ComposerAttachmentBar.vue'
import ComposerResourceMenu from './ComposerResourceMenu.vue'
import ComposerFilePicker from './ComposerFilePicker.vue'
import type { ComposerReference } from './composerDocument'
import type { ChatModel, ChatResource } from '@/types/workbenchChat'
import './composer.css'

const props = withDefaults(defineProps<{ modelValue: string; model: string; models: ChatModel[]; effort: string; taskMode: boolean; webSearch: boolean; attachments: ChatResource[]; editTarget?: { version_id: string; name: string } | null; disabled?: boolean; busy?: boolean; waiting?: boolean; elevated?: boolean; uploading?: boolean; error?: string; officeEnabled?: boolean; searchEnabled?: boolean; uploadsEnabled?: boolean; resourcesEnabled?: boolean; maxFiles?: number }>(), { maxFiles: 10 })
const emit = defineEmits<{ 'update:modelValue': [value: string]; 'update:model': [value: string]; 'update:effort': [value: string]; 'update:taskMode': [value: boolean]; 'update:webSearch': [value: boolean]; send: []; stop: []; files: [files: File[]]; selectResources: [files: ChatResource[]]; removeAttachment: [id: string]; clearEdit: [] }>()
const editor = ref<InstanceType<typeof ComposerEditor> | null>(null), fileInput = ref<HTMLInputElement | null>(null)
const dragging = ref(false), pickerOpen = ref(false)
let dragDepth = 0
const accept = '.pdf,.docx,.xlsx,.pptx,.csv,.json,.txt,.md,.png,.jpg,.jpeg,.webp'
const modelOptions = computed(() => props.models.map(model => ({ value: model.id, label: model.display_name || model.id })))
const labels: Record<string, string> = { minimal: '极低', low: '低', medium: '中', high: '高', xhigh: '极高', none: '关闭', auto: '默认' }
const effortOptions = computed(() => { const list = props.models.find(model => model.id === props.model)?.reasoning_efforts || []; return list.length ? [{ value: '', label: '推理强度' }, ...list.map(value => ({ value, label: labels[value] || value }))] : [] })
const references = computed<ComposerReference[]>(() => [
  ...(props.taskMode ? [{ key: 'task', kind: 'task' as const, label: '任务模式' }] : []),
  ...(props.webSearch ? [{ key: 'search', kind: 'tool' as const, label: '联网搜索' }] : []),
])
const canSend = computed(() => !props.disabled && !props.busy && !props.waiting && !props.uploading && !!props.modelValue.trim() && !!props.model)
function send() { if (canSend.value) emit('send') }
function updateReferences(keys: string[]) { if (props.disabled || props.busy) return; if (props.taskMode && !keys.includes('task') && !props.attachments.length && !props.editTarget) emit('update:taskMode', false); if (props.webSearch && !keys.includes('search')) emit('update:webSearch', false) }
function pickFiles(event: Event) { const input = event.target as HTMLInputElement; if (props.uploadsEnabled && !props.disabled && !props.busy && !props.uploading && input.files?.length) emit('files', Array.from(input.files)); input.value = '' }
function dragEnter(event: DragEvent) { if (!event.dataTransfer?.types.includes('Files') || !props.uploadsEnabled || props.disabled || props.busy) return; dragDepth++; dragging.value = true }
function dragOver(event: DragEvent) { if (event.dataTransfer) event.dataTransfer.dropEffect = props.uploadsEnabled && !props.disabled && !props.busy && !props.uploading ? 'copy' : 'none' }
function dragLeave() { dragDepth = Math.max(0, dragDepth - 1); if (!dragDepth) dragging.value = false }
function dropFiles(event: DragEvent) { dragDepth = 0; dragging.value = false; if (props.uploadsEnabled && !props.disabled && !props.busy && !props.uploading && event.dataTransfer?.files.length) emit('files', Array.from(event.dataTransfer.files)) }
function pasteFiles(event: ClipboardEvent) { const files = Array.from(event.clipboardData?.files || []); if (files.length && props.uploadsEnabled && !props.disabled && !props.busy && !props.uploading) { event.preventDefault(); emit('files', files) } }
function selectResources(files: ChatResource[]) { if (props.resourcesEnabled && !props.disabled && !props.busy && !props.uploading) emit('selectResources', files) }
function closePicker() { pickerOpen.value = false; editor.value?.focus() }
watch(() => [props.disabled, props.busy, props.resourcesEnabled], () => { if (props.disabled || props.busy || !props.resourcesEnabled) pickerOpen.value = false })
defineExpose({ focus: () => editor.value?.focus() })
</script>
