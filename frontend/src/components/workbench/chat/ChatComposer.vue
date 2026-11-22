<template>
  <div class="bs-chat-composer-shell bs-native-composer" @dragenter.prevent="dragEnter" @dragover.prevent="dragOver" @dragleave.prevent="dragLeave" @drop.prevent="dropFiles">
    <div v-if="dragging && uploadsEnabled && !busy" class="bs-composer-drop" aria-hidden="true"><Icon name="upload" size="xl" /><span>将文件拖到这里</span></div>
    <div class="bs-chat-composer" :class="{ elevated, 'has-resources': attachments.length || editTarget || uploading }">
      <ComposerAttachmentBar :attachments="attachments" :edit-target="editTarget" :uploading="uploading" :disabled="busy" @remove="emit('removeAttachment', $event)" @clear-edit="emit('clearEdit')" />
      <div class="bs-composer-writing"><ComposerEditor ref="editor" :model-value="modelValue" :disabled="disabled" :placeholder="placeholder" @update:model-value="emit('update:modelValue', $event)" @submit="send" @paste-files="pasteFiles" /></div>
      <div class="bs-chat-composer-toolbar">
        <div class="bs-chat-composer-leading">
          <ComposerResourceMenu v-if="uploadsEnabled || resourcesEnabled" :disabled="disabled || busy || waiting" :uploading="uploading" :uploads-enabled="uploadsEnabled" :resources-enabled="resourcesEnabled" @upload="fileInput?.click()" @resources="pickerOpen = true" @focus-editor="editor?.focus()" />
          <ComposerToolTag
            v-if="webSearch || searchEnabled || searchUnavailableReason"
            label="联网搜索"
            icon="globe"
            :active="webSearch"
            :disabled="busy || waiting || disabled"
            :unavailable="!webSearch && !searchEnabled"
            :hint="!webSearch && !searchEnabled ? searchUnavailableReason : undefined"
            :description-id="searchPriceLabel ? searchPriceId : !webSearch && searchUnavailableReason ? searchReasonId : undefined"
            @toggle="toggleSearch"
          />
          <span v-if="searchPriceLabel" :id="searchPriceId" class="bs-composer-search-price">{{ searchPriceLabel }}</span>
        </div>
        <div class="bs-chat-composer-trailing">
          <Select class="bs-chat-model-select" :model-value="model" :options="modelOptions" :disabled="busy || !modelOptions.length" placeholder="暂无可用模型" searchable aria-label="选择模型" @update:model-value="emit('update:model', String($event))" />
          <Select v-if="effortOptions.length" class="bs-chat-effort-select" :model-value="effort" :options="effortOptions" :disabled="busy" aria-label="推理强度" @update:model-value="emit('update:effort', String($event))">
            <template #selected>{{ labels[effort] || effort || '模型默认' }}</template>
          </Select>
          <button v-if="busy && !waiting" type="button" class="bs-chat-submit" title="停止生成" aria-label="停止生成" @click="emit('stop')"><span class="bs-chat-stop-square"></span></button>
          <button v-else type="button" class="bs-chat-submit" title="发送" aria-label="发送消息" :disabled="!canSend" @click="send"><Icon name="arrowUp" size="md" /></button>
        </div>
      </div>
    </div>
    <p v-if="error" class="bs-chat-small-error" role="alert">{{ error }}</p>
    <p v-if="!webSearch && searchUnavailableReason" :id="searchReasonId" :class="showSearchReason ? 'bs-composer-tool-reason' : 'sr-only'">{{ searchUnavailableReason }}</p>
    <input ref="fileInput" type="file" multiple hidden :accept="accept" @change="pickFiles" />
    <ComposerFilePicker v-if="resourcesEnabled" :show="pickerOpen" :attachments="attachments" :max-files="maxFiles" :attachment-extensions="attachmentExtensions" @select="selectResources" @close="closePicker" />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ComposerEditor from './ComposerEditor.vue'
import ComposerAttachmentBar from './ComposerAttachmentBar.vue'
import ComposerResourceMenu from './ComposerResourceMenu.vue'
import ComposerFilePicker from './ComposerFilePicker.vue'
import ComposerToolTag from './ComposerToolTag.vue'
import type { ChatModel, ChatResource } from '@/types/workbenchChat'
import { formatScaled } from '@/utils/pricing'
import './composer.css'

const props = withDefaults(defineProps<{ modelValue: string; model: string; models: ChatModel[]; effort: string; webSearch: boolean; attachments: ChatResource[]; editTarget?: { version_id: string; name: string } | null; disabled?: boolean; busy?: boolean; waiting?: boolean; elevated?: boolean; uploading?: boolean; error?: string; searchEnabled?: boolean; searchUnavailableReason?: string; searchPricePer1000Usd?: number; uploadsEnabled?: boolean; resourcesEnabled?: boolean; maxFiles?: number; attachmentExtensions?: string[] }>(), { maxFiles: 10 })
const emit = defineEmits<{ 'update:modelValue': [value: string]; 'update:model': [value: string]; 'update:effort': [value: string]; 'update:webSearch': [value: boolean]; send: []; stop: []; files: [files: File[]]; selectResources: [files: ChatResource[]]; removeAttachment: [id: string]; clearEdit: [] }>()
const editor = ref<InstanceType<typeof ComposerEditor> | null>(null), fileInput = ref<HTMLInputElement | null>(null)
const dragging = ref(false), pickerOpen = ref(false)
const searchReasonId = `bs-composer-search-${useId()}`
const searchPriceId = `bs-composer-search-price-${useId()}`
const searchPriceLabel = computed(() => {
  const price = props.searchPricePer1000Usd ?? 0
  return props.webSearch && props.searchEnabled && Number.isFinite(price) && price > 0 ? `${formatScaled(price, 1)} / 千次搜索` : ''
})
const showSearchReason = ref(false)
let dragDepth = 0
const accept = computed(() => props.attachmentExtensions?.join(',') ?? '.pdf,.docx,.xlsx,.pptx,.csv,.json,.txt,.md,.png,.jpg,.jpeg,.webp')
const placeholder = computed(() => {
  if (props.waiting) return '请先回答上方的问题'
  if (props.uploadsEnabled || props.resourcesEnabled) return '发送消息或添加附件'
  return '发送消息'
})
const modelOptions = computed(() => props.models.map(model => ({ value: model.id, label: model.id })))
const labels: Record<string, string> = { minimal: '极低', low: '低', medium: '中', high: '高', xhigh: '极高', max: '最高', none: '关闭', auto: '默认' }
const effortOptions = computed(() => {
  const model = props.models.find(item => item.id === props.model)
  const levels = model?.reasoning_efforts ?? []
  if (!levels.length) return []
  const recommended = model?.default_reasoning_effort
  const options = levels.map(value => ({
    value,
    label: (labels[value] || value) + (value === recommended ? '（推荐）' : ''),
  }))
  return recommended && levels.includes(recommended) ? options : [{ value: '', label: '模型默认' }, ...options]
})
const canSend = computed(() => !props.disabled && !props.busy && !props.waiting && !props.uploading && !(props.webSearch && !props.searchEnabled) && !!props.modelValue.trim() && modelOptions.value.some(option => option.value === props.model))
function send() { if (canSend.value) emit('send') }
function toggleSearch() {
  if (props.disabled || props.busy || props.waiting) return
  if (!props.webSearch && !props.searchEnabled) {
    showSearchReason.value = !showSearchReason.value
    return
  }
  showSearchReason.value = false
  emit('update:webSearch', !props.webSearch)
  void nextTick(() => editor.value?.focus())
}
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
