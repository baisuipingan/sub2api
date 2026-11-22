<template>
  <AppLayout immersive>
    <div class="bs-chat bs-chat-files-page">
      <header class="bs-chat-files-header"><div><h1>我的文件</h1><p>工作台上传的资料与生成的成果</p></div><button type="button" class="btn btn-secondary" :disabled="loading" @click="load()"><Icon name="refresh" size="sm" :class="{ 'bs-chat-spin': loading }" />刷新</button><button v-if="uploadsEnabled" type="button" class="btn btn-primary" :disabled="uploading" @click="picker?.click()"><Icon name="upload" size="sm" />{{ uploading ? '正在上传…' : '上传文件' }}</button><input ref="picker" type="file" hidden multiple :accept="nativeChat ? nativeAttachmentExtensions.join(',') : undefined" @change="upload" /></header>
      <div v-if="error" class="bs-chat-page-error" role="alert">{{ error }}</div>
      <div class="bs-chat-files-body">
        <section class="bs-chat-files-grid" aria-label="文件列表">
          <div v-for="file in files" :key="file.version_id" class="bs-chat-library-file"><button type="button" class="bs-chat-library-preview" @click="selected = file; panelOpen = true"><Icon name="document" size="lg" /><strong :title="file.name">{{ file.name }}</strong><small>{{ formatBytes(file.bytes) }} · {{ file.source === 'gallery' ? '图片生成' : file.source === 'generated' || file.role === 'output' ? '办公成果' : '上传资料' }}</small></button><div class="bs-chat-library-actions"><button type="button" class="bs-chat-text-button" @click="download(file)"><Icon name="download" size="sm" />下载</button><button type="button" class="bs-chat-icon" :aria-label="`删除${file.name}`" @click="toDelete = file"><Icon name="trash" size="sm" /></button></div></div>
          <p v-if="!files.length && !loading" class="bs-chat-empty-library"><Icon name="document" size="lg" />还没有文件。你可以在对话中上传资料或生成办公文档。</p>
          <p v-if="loading" class="bs-chat-muted" role="status">正在加载文件…</p>
          <button v-if="cursor" type="button" class="bs-chat-text-button" :disabled="loading" @click="load(true)">加载更多</button>
        </section>
        <div v-if="panelOpen" class="bs-chat-files-preview"><ResourcePreview ref="preview" :file="selected" :files="files" :docx-only="nativeChat" :editable-extensions="nativeChat ? officeExtensions : undefined" :read-only="!officeEnabled" hide-fullscreen @edit="editFile" @preview="selected = $event" @close="panelOpen = false" /></div>
      </div>
      <ConfirmDialog :show="!!toDelete" title="删除文件" :message="`删除「${toDelete?.name || ''}」？已有消息会保留，但将无法继续访问此文件。`" danger :loading="deleting" @confirm="remove" @cancel="toDelete = null" />
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ResourcePreview from '@/components/workbench/chat/ResourcePreview.vue'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatError } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import { acceptsChatAttachment, CHAT_IMAGE_EXTENSIONS, type ChatArtifact } from '@/types/workbenchChat'
import '@/components/workbench/chat/chat.css'
const router = useRouter(), auth = useAuthStore()
let epoch = 0, request = 0
const limits = ref({ max_files: 8, max_file_bytes: 24 * 1024 * 1024 })
const files = ref<ChatArtifact[]>([]), cursor = ref<string | undefined>(), loading = ref(false), error = ref(''), uploading = ref(false), uploadsEnabled = ref(false)
const nativeChat = ref(false), officeEnabled = ref(false)
const officeExtensions = ref(['.docx'])
const nativeAttachmentExtensions = computed(() => [...officeExtensions.value, ...CHAT_IMAGE_EXTENSIONS])
const picker = ref<HTMLInputElement | null>(null), selected = ref<ChatArtifact | null>(null), panelOpen = ref(false), preview = ref<InstanceType<typeof ResourcePreview> | null>(null)
const toDelete = ref<ChatArtifact | null>(null), deleting = ref(false)
async function load(append = false) {
  if (loading.value) return
  const owner = epoch, own = ++request
  loading.value = true; error.value = ''
  try {
    const value = await workbenchChatApi.resources(append ? cursor.value : undefined)
    if (owner !== epoch || own !== request) return
    files.value = append ? [...files.value, ...value.items.filter(item => !files.value.some(old => old.version_id === item.version_id))] : value.items
    cursor.value = value.next_cursor
  } catch (e) { if (owner === epoch && own === request) error.value = chatError(e) }
  finally { if (owner === epoch && own === request) loading.value = false }
}
async function upload(event: Event) {
  const input = event.target as HTMLInputElement, list = Array.from(input.files || [])
  input.value = ''
  if (uploading.value || !uploadsEnabled.value || !list.length) return
  if (nativeChat.value && list.some(file => !acceptsChatAttachment(file, nativeAttachmentExtensions.value))) { error.value = '请选择支持的文件格式'; return }
  if (list.length > limits.value.max_files) { error.value = `每次最多上传 ${limits.value.max_files} 个文件`; return }
  const tooLarge = list.find(file => file.size > limits.value.max_file_bytes)
  if (tooLarge) { error.value = `「${tooLarge.name}」超过 ${Math.floor(limits.value.max_file_bytes / 1024 / 1024)} MB 限制`; return }
  const owner = epoch
  uploading.value = true; error.value = ''
  try {
    for (const file of list) { if (owner !== epoch) return; await workbenchChatApi.upload(file) }
    if (owner === epoch) await load()
  } catch (e) { if (owner === epoch) error.value = chatError(e) }
  finally { if (owner === epoch) uploading.value = false }
}
async function download(file: ChatArtifact) { selected.value = file; panelOpen.value = true; await nextTick(); await preview.value?.download(file) }
function editFile(file: ChatArtifact) { void router.push({ path: '/workbench/chat', query: { edit_version: file.version_id, edit_name: file.name } }) }
async function remove() {
  const file = toDelete.value, owner = epoch
  if (!file || deleting.value) return
  deleting.value = true
  try {
    await workbenchChatApi.deleteResource(file.id)
    if (owner !== epoch) return
    if (selected.value?.id === file.id) { panelOpen.value = false; selected.value = null }
    toDelete.value = null; await load()
  } catch (e) { if (owner === epoch) error.value = chatError(e) }
  finally { if (owner === epoch) deleting.value = false }
}
watch(() => auth.user?.id, () => { ++epoch; ++request; files.value = []; selected.value = null; toDelete.value = null; panelOpen.value = false; loading.value = false; uploading.value = false; deleting.value = false; uploadsEnabled.value = false; error.value = ''; cursor.value = undefined }, { flush: 'sync' })
function formatBytes(bytes: number) { return bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(0)} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB` }
onMounted(() => { const owner = epoch; void load(); void workbenchChatApi.status().then(value => { if (owner === epoch) { uploadsEnabled.value = !!value.uploads_enabled; nativeChat.value = value.browser_driven === true; officeExtensions.value = value.office_file_extensions ?? ['.docx']; officeEnabled.value = !!value.office_enabled; limits.value = { max_files: value.max_files || 8, max_file_bytes: value.max_file_bytes || 24 * 1024 * 1024 } } }).catch(() => {}) })
onBeforeUnmount(() => { ++epoch; ++request })
</script>
