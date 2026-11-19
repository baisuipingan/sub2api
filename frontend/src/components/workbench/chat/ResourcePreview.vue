<template>
  <section class="bs-chat-workspace bso-workspace" :class="{ fullscreen }" :aria-label="file ? file.name : '文件工作区'">
    <header class="bso-workspace-toolbar">
      <button v-if="file" type="button" class="bs-chat-icon" aria-label="返回文件列表" title="返回文件列表" @click="emit('preview', null)"><Icon name="arrowLeft" size="sm" /></button>
      <strong><span class="bso-workspace-title" :title="file?.name">{{ file?.name || '文件工作区' }}</span><span v-if="!file && files.length" class="bso-workspace-count">{{ files.length }}</span></strong>
      <button v-if="file && !readOnly && editable(file)" type="button" class="bso-continue-edit" title="基于当前文件版本继续修改" @click="emit('edit', file)"><Icon name="edit" size="sm" /><span>继续修改</span></button>
      <button v-if="file" type="button" class="bs-chat-icon" :disabled="downloading" aria-label="下载文件" title="下载原文件" @click="download(file)"><Icon :name="downloading ? 'refresh' : 'download'" :class="{ 'bs-chat-spin': downloading }" size="sm" /></button>
      <button v-if="!hideFullscreen" type="button" class="bs-chat-icon" :aria-label="fullscreen ? '退出全屏' : '全屏预览'" :title="fullscreen ? '退出全屏' : '全屏预览'" @click="emit('fullscreen')"><Icon :name="fullscreen ? 'x' : 'externalLink'" size="sm" /></button>
      <button type="button" class="bs-chat-icon" aria-label="关闭文件工作区" title="关闭文件工作区" @click="emit('close')"><Icon name="x" size="sm" /></button>
    </header>
    <div v-if="file && ['markdown', 'text'].includes(kind) && !loading && !error" class="bso-preview-mode-toolbar"><div class="bso-segmented" role="tablist" aria-label="文件显示方式"><button type="button" role="tab" :aria-selected="!showSource" @click="showSource = false">预览</button><button type="button" role="tab" :aria-selected="showSource" @click="showSource = true">源码</button></div></div>
    <p v-if="error" class="bs-chat-preview-error" role="alert"><Icon name="exclamationCircle" size="sm" /><span>{{ error }}</span><button type="button" class="bs-chat-text-button" @click="loadPreview(true)">重试</button></p>
    <WorkspaceFileList v-if="!file" :files="files" :loading="filesLoading" :load-failed="filesLoadFailed" :downloading="downloading" @preview="emit('preview', $event)" @download="download" />
    <div v-else-if="loading" class="bs-chat-preview-loading bso-preview-loading" role="status"><Icon class="bs-chat-spin" name="refresh" size="md" />正在加载预览…</div>
    <div v-else-if="kind === 'image' && previewURL" class="bs-chat-image-preview"><img :src="previewURL" :alt="file.name" @error="imageFailed" /></div>
    <!-- The authenticated bounded/signature-checked PDF blob is kept intact.
         Toolbar/fullscreen changes never create a second viewer or object GET. -->
    <iframe v-else-if="kind === 'pdf' && previewURL" :src="previewURL" :title="file.name" class="bs-chat-pdf-preview" referrerpolicy="no-referrer"></iframe>
    <div v-else-if="kind === 'markdown' && !showSource" class="bs-chat-document-preview"><ChatMarkdown :content="text" :files="files" @preview="emit('preview', $event)" /></div>
    <pre v-else-if="['text', 'markdown'].includes(kind)" class="bs-chat-text-preview" :class="{ 'bso-source-preview': showSource }">{{ text }}</pre>
    <div v-else-if="!error" class="bs-chat-preview-unsupported"><WorkspaceFileIcon :file="file" /><strong>{{ file.name }}</strong><p>此格式暂不支持在线预览，可以下载查看。</p><button type="button" class="bs-chat-text-button" :disabled="downloading" @click="download(file)"><Icon name="download" size="sm" />下载文件</button></div>
  </section>
</template>
<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { saveAs } from 'file-saver'
import Icon from '@/components/icons/Icon.vue'
import ChatMarkdown from './ChatMarkdown.vue'
import WorkspaceFileList from './WorkspaceFileList.vue'
import WorkspaceFileIcon from './WorkspaceFileIcon.vue'
import './workbenchArtifacts.css'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatError } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import type { ChatArtifact, ChatResourceAccess } from '@/types/workbenchChat'
const props = defineProps<{ files: ChatArtifact[]; file: ChatArtifact | null; filesLoading?: boolean; filesLoadFailed?: boolean; fullscreen?: boolean; hideFullscreen?: boolean; readOnly?: boolean; loadAccess?: (versionId: string) => Promise<ChatResourceAccess> }>()
const emit = defineEmits<{ preview: [file: ChatArtifact | null]; close: []; fullscreen: []; edit: [file: ChatArtifact]; error: [message: string] }>()
const loading = ref(false), downloading = ref(false), error = ref(''), previewURL = ref(''), text = ref(''), kind = ref('unsupported')
const auth = useAuthStore()
const showSource = ref(false)
let epoch = 0, ownerEpoch = 0, fetchController: AbortController | undefined, downloadController: AbortController | undefined
const maxAccessEntries = 24, maxPreviewEntries = 8, maxPreviewBytes = 64 * 1024 * 1024
const maxPDFBytes = 24 * 1024 * 1024, maxTextBytes = 2 * 1024 * 1024
const accessCache = new Map<string, ChatResourceAccess>()
type LoadedPreview = { kind: string; url: string; text: string; bytes: number }
const previewCache = new Map<string, LoadedPreview>()
function discardPreview(id: string) { const value = previewCache.get(id); if (value?.url) URL.revokeObjectURL(value.url); previewCache.delete(id) }
function clearCaches() { accessCache.clear(); for (const id of previewCache.keys()) discardPreview(id) }
function cachePreview(id: string, value: LoadedPreview) {
  discardPreview(id); previewCache.set(id, value)
  let bytes = [...previewCache.values()].reduce((total, item) => total + item.bytes, 0)
  while (previewCache.size > maxPreviewEntries || bytes > maxPreviewBytes) { const first = previewCache.keys().next().value; if (!first) break; bytes -= previewCache.get(first)!.bytes; discardPreview(first) }
}
function showPreview(value: LoadedPreview) { kind.value = value.kind; previewURL.value = value.url; text.value = value.text }
function validURL(value: string): string { const url = new URL(value, window.location.origin); if (!['https:', 'http:'].includes(url.protocol)) throw new Error('文件链接无效'); return url.href }
async function getAccess(file: ChatArtifact, force = false) {
  const cached = accessCache.get(file.version_id)
  if (!force && cached?.expires_at && Date.parse(cached.expires_at) > Date.now() + 60000) return cached
  const owner = ownerEpoch
  const access = await (props.loadAccess ? props.loadAccess(file.version_id) : workbenchChatApi.access(file.version_id))
  if (owner !== ownerEpoch) throw new Error('登录账号已变更')
  accessCache.delete(file.version_id); accessCache.set(file.version_id, access)
  while (accessCache.size > maxAccessEntries) accessCache.delete(accessCache.keys().next().value!)
  return access
}
async function previewResponse(file: ChatArtifact, access: ChatResourceAccess, controller: AbortController) {
  const request = (value: ChatResourceAccess) => fetch(validURL(value.preview_url || value.url), { signal: controller.signal, referrerPolicy: 'no-referrer' })
  let response = await request(access)
  if (response.status === 403 && !controller.signal.aborted) response = await request(await getAccess(file, true))
  if (!response.ok) throw new Error('文件预览加载失败，请重试')
  return response
}
async function readPreview(response: Response, limit: number, truncate: boolean) {
  if (!truncate && Number(response.headers.get('content-length')) > limit) throw new Error('预览文件超过 24 MB，请下载原文件查看')
  const reader = response.body?.getReader()
  if (!reader) throw new Error('文件预览内容为空，请下载原文件查看')
  const chunks: Uint8Array[] = []; let bytes = 0, truncated = false
  try {
    while (true) {
      const part = await reader.read(); if (part.done) break
      if (bytes + part.value.length > limit) {
        if (!truncate) throw new Error('预览文件超过 24 MB，请下载原文件查看')
        chunks.push(part.value.subarray(0, limit - bytes)); bytes = limit; truncated = true; break
      }
      chunks.push(part.value); bytes += part.value.length
    }
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
  const content = new Uint8Array(bytes); let offset = 0
  for (const chunk of chunks) { content.set(chunk, offset); offset += chunk.length }
  return { content, truncated }
}
async function loadPreview(force = false) {
  const ownEpoch = ++epoch; fetchController?.abort(); const controller = new AbortController(); fetchController = controller
  const file = props.file; error.value = ''; previewURL.value = ''; text.value = ''; kind.value = 'unsupported'
  if (!file) { loading.value = false; return }
  const cached = previewCache.get(file.version_id)
  if (cached && !force) { previewCache.delete(file.version_id); previewCache.set(file.version_id, cached); showPreview(cached); loading.value = false; return }
  if (force) discardPreview(file.version_id)
  loading.value = true
  try {
    const access = await getAccess(file, force)
    if (ownEpoch !== epoch) return
    const mime = file.mime || ''
    kind.value = access.preview_kind || file.preview_kind || (access.preview_url ? 'pdf' : mime.startsWith('image/') ? 'image' : mime === 'application/pdf' ? 'pdf' : /\.(md|markdown)$/i.test(file.name) ? 'markdown' : mime.startsWith('text/') ? 'text' : 'unsupported')
    if (kind.value === 'image') previewURL.value = validURL(access.preview_url || access.url)
    else if (kind.value === 'pdf') {
      const response = await previewResponse(file, access, controller)
      if (response.headers.get('content-type')?.split(';')[0]?.trim().toLowerCase() !== 'application/pdf') throw new Error('文件不是有效的 PDF 预览，请下载原文件查看')
      const { content } = await readPreview(response, maxPDFBytes, false)
      if (ownEpoch !== epoch) return
      if (new TextDecoder().decode(content.subarray(0, 5)) !== '%PDF-') throw new Error('文件不是有效的 PDF 预览，请下载原文件查看')
      const value = { kind: 'pdf', url: URL.createObjectURL(new Blob([content], { type: 'application/pdf' })), text: '', bytes: content.byteLength }
      cachePreview(file.version_id, value); showPreview(value)
    }
    else if (['text', 'markdown'].includes(kind.value)) {
      const { content, truncated } = await readPreview(await previewResponse(file, access, controller), maxTextBytes, true)
      if (ownEpoch !== epoch) return
      const value = { kind: kind.value, url: '', text: new TextDecoder().decode(content) + (truncated ? '\n\n（内容较长，完整内容请下载原文件查看。）' : ''), bytes: content.byteLength }
      cachePreview(file.version_id, value); showPreview(value)
    }
  } catch (e) { if (ownEpoch === epoch && !controller.signal.aborted) error.value = chatError(e) }
  finally { if (ownEpoch === epoch) loading.value = false }
}
async function download(file: ChatArtifact) {
  if (downloading.value) return
  const owner = ownerEpoch, controller = new AbortController(); downloadController = controller
  downloading.value = true; error.value = ''
  try {
    let access = await getAccess(file)
    let response = await fetch(validURL(access.url), { signal: controller.signal, referrerPolicy: 'no-referrer' })
    if (response.status === 403) { access = await getAccess(file, true); response = await fetch(validURL(access.url), { signal: controller.signal, referrerPolicy: 'no-referrer' }) }
    if (!response.ok) throw new Error('文件下载失败，请稍后重试')
    const blob = await response.blob()
    if (owner === ownerEpoch) saveAs(blob, file.name)
  } catch (e) { if (owner === ownerEpoch && !controller.signal.aborted) { error.value = chatError(e); emit('error', error.value) } }
  finally { if (owner === ownerEpoch) downloading.value = false; if (downloadController === controller) downloadController = undefined }
}
function editable(file: ChatArtifact) { return /\.(docx|xlsx|pptx)$/i.test(file.name) }
function imageFailed() { error.value = '图片加载失败，请重试'; if (props.file) accessCache.delete(props.file.version_id) }
watch(() => props.file?.version_id, () => { showSource.value = false; void loadPreview() }, { immediate: true })
watch(() => auth.user?.id, () => { ++ownerEpoch; ++epoch; fetchController?.abort(); downloadController?.abort(); clearCaches(); previewURL.value = ''; text.value = ''; error.value = ''; loading.value = false; downloading.value = false }, { flush: 'sync' })
onBeforeUnmount(() => { ++ownerEpoch; ++epoch; fetchController?.abort(); downloadController?.abort(); clearCaches() })
defineExpose({ download })
</script>
