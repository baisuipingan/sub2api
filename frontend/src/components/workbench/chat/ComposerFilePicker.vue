<template>
  <BaseDialog :show="show" title="选择附件" width="reading" :z-index="190" @close="emit('close')">
    <div class="bs-chat bs-composer-file-picker">
      <div v-if="canUseGallery" role="group" aria-label="文件来源" class="mb-2 flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary" :aria-pressed="source === 'files'" :disabled="adding" @click="changeSource('files')">我的文件</button>
        <button type="button" class="btn btn-secondary" :aria-pressed="source === 'gallery'" :disabled="adding" @click="changeSource('gallery')">云端图库</button>
      </div>
      <SearchInput v-model="query" placeholder="搜索文件名称" :debounce-ms="0" />
      <div class="bs-composer-file-columns"><span>文件名称</span><span>上传或生成时间</span></div>
      <div class="bs-composer-file-list" :aria-busy="loading">
        <p v-if="!files.length && loading" class="bs-composer-picker-status" role="status"><Icon name="refresh" size="sm" class="bs-chat-spin" />正在加载文件…</p>
        <p v-else-if="!visibleFiles.length && !error" class="bs-composer-picker-status">{{ query ? '没有找到匹配的文件' : '还没有可以选取的文件' }}</p>
        <label v-for="file in visibleFiles" :key="file.version_id" class="bs-composer-file-row" :class="{ selected: selected.has(file.version_id) || attachedIds.has(file.version_id), unavailable: !canSelect(file) }">
          <input type="checkbox" :checked="selected.has(file.version_id) || attachedIds.has(file.version_id)" :disabled="!canSelect(file) || loading" :aria-label="`选择${file.name}`" @change="toggle(file)" />
          <span class="bs-composer-file-icon"><Icon name="document" size="md" /></span>
          <span class="bs-composer-resource-body"><span class="bs-composer-resource-name" :title="file.name">{{ file.name }}</span><span class="bs-composer-resource-meta">{{ attachedIds.has(file.version_id) ? '已添加到对话' : file.source === 'generated' ? '生成的文件' : file.source === 'gallery' ? '云端图片' : '上传的文件' }} · {{ size(file.bytes) }}</span></span>
          <time>{{ date(file.created_at) }}</time>
        </label>
        <button v-if="cursor" type="button" class="bs-composer-picker-more" :disabled="loading || adding" @click="loadMore">{{ loading ? '正在加载…' : '加载更多' }}</button>
      </div>
      <p v-if="error" class="bs-chat-small-error" role="alert">{{ error }}<button v-if="listFailed" type="button" class="bs-chat-text-button" :disabled="loading || adding" @click="loadMore">重试</button></p>
    </div>
    <template #footer><div class="bs-composer-picker-footer"><span>{{ selected.size + attachedIds.size }} / {{ maxFiles }} 个文件</span><button type="button" class="btn btn-secondary" @click="emit('close')">取消</button><button type="button" class="btn btn-primary" :disabled="!selected.size || loading || adding" @click="confirm">{{ adding ? '正在添加…' : '添加到对话' }}</button></div></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Icon from '@/components/icons/Icon.vue'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatError } from '@/stores/workbenchChat'
import { acceptsChatAttachment, CHAT_IMAGE_EXTENSIONS, type ChatArtifact, type ChatResource } from '@/types/workbenchChat'
const props = withDefaults(defineProps<{ show: boolean; attachments: ChatResource[]; maxFiles?: number; attachmentExtensions?: string[] }>(), { maxFiles: 10 })
const emit = defineEmits<{ close: []; select: [files: ChatResource[]] }>()
const files = ref<ChatArtifact[]>([]), query = ref(''), loading = ref(false), error = ref(''), cursor = ref<string | undefined>(), selected = ref(new Map<string, ChatArtifact>())
const source = ref<'files' | 'gallery'>('files'), adding = ref(false), listFailed = ref(false)
const canUseGallery = computed(() => props.attachmentExtensions?.some(ext => CHAT_IMAGE_EXTENSIONS.includes(ext)) ?? false)
const attachedIds = computed(() => new Set(props.attachments.map(file => file.version_id)))
const visibleFiles = computed(() => {
  const keyword = query.value.trim().toLocaleLowerCase()
  return files.value.filter(file => acceptsChatAttachment(file, props.attachmentExtensions) && file.name.toLocaleLowerCase().includes(keyword))
})
let epoch = 0
function canSelect(file: ChatArtifact) { return !adding.value && !attachedIds.value.has(file.version_id) && (selected.value.has(file.version_id) || selected.value.size + attachedIds.value.size < props.maxFiles) }
function toggle(file: ChatArtifact) { if (!canSelect(file)) return; const next = new Map(selected.value); if (next.has(file.version_id)) next.delete(file.version_id); else next.set(file.version_id, file); selected.value = next }
async function loadMore() {
  if (loading.value || adding.value || !props.show) return
  const own = epoch
  loading.value = true
  error.value = ''
  listFailed.value = false
  try {
    const page = source.value === 'gallery' ? await workbenchChatApi.resources(cursor.value, 'gallery') : await workbenchChatApi.resources(cursor.value)
    if (own !== epoch) return
    const unique = new Map(files.value.map(file => [file.version_id, file]))
    for (const file of page.items) unique.set(file.version_id, file)
    files.value = [...unique.values()]
    cursor.value = page.next_cursor
  } catch (e) {
    if (own === epoch) { error.value = chatError(e); listFailed.value = true }
  } finally { if (own === epoch) loading.value = false }
}

function changeSource(next: 'files' | 'gallery') {
  if (adding.value || next === source.value) return
  ++epoch
  source.value = next
  files.value = []
  cursor.value = undefined
  query.value = ''
  loading.value = false
  void loadMore()
}
function size(bytes: number) { return bytes >= 1024 * 1024 ? `${(bytes / 1024 / 1024).toFixed(1)} MB` : `${Math.max(1, Math.ceil(bytes / 1024))} KB` }
function date(value?: string) { if (!value) return ''; const parsed = new Date(value); return Number.isNaN(parsed.valueOf()) ? '' : parsed.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric' }) }
async function confirm() {
  if (!props.show || loading.value || adding.value || !selected.value.size) return
  const own = epoch, chosen: ChatResource[] = []
  adding.value = true
  error.value = ''
  listFailed.value = false
  try {
    for (const file of selected.value.values()) {
      if (own !== epoch) return
      chosen.push(file.source === 'gallery' ? await workbenchChatApi.attachGalleryImage(file.id) : file)
    }
    if (own !== epoch) return
    emit('select', chosen)
    emit('close')
  } catch (e) {
    if (own === epoch) error.value = chatError(e)
  } finally { if (own === epoch) adding.value = false }
}
watch(() => props.show, show => { ++epoch; loading.value = false; adding.value = false; if (show) { source.value = 'files'; files.value = []; cursor.value = undefined; query.value = ''; error.value = ''; listFailed.value = false; selected.value = new Map(); void loadMore() } }, { immediate: true })
onBeforeUnmount(() => { ++epoch })
</script>
