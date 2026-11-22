<template>
  <section class="bs-chat-workspace bso-workspace" :class="{ fullscreen }" :aria-label="file ? file.name : '文件工作区'">
    <header class="bso-workspace-toolbar">
      <button v-if="file" type="button" class="bs-chat-icon" aria-label="返回文件列表" title="返回文件列表" @click="emit('preview', null)"><Icon name="arrowLeft" size="sm" /></button>
      <strong><span class="bso-workspace-title" :title="file?.name">{{ file?.name || '文件工作区' }}</span><span v-if="!file && files.length" class="bso-workspace-count">{{ files.length }}</span></strong>
      <button v-if="file && !readOnly && editable(file)" type="button" class="bso-continue-edit" title="基于当前文件版本继续修改" @click="emit('edit', file)"><Icon name="edit" size="sm" /><span>继续修改</span></button>
      <button
        v-if="file && canPrintPreview"
        type="button"
        class="bs-chat-icon bso-print-preview"
        :disabled="printLoading"
        :aria-label="printLabel"
        :title="printLabel"
        @click="togglePrintPreview(file)"
      >
        <Icon :name="printLoading ? 'refresh' : printURL ? 'eye' : 'document'" :class="{ 'bs-chat-spin': printLoading }" size="sm" />
        <span>{{ printLoading ? '生成中' : printURL ? '快速版' : '打印版' }}</span>
      </button>
      <button v-if="file" type="button" class="bs-chat-icon" :disabled="downloading" aria-label="下载文件" title="下载原文件" @click="download(file)"><Icon :name="downloading ? 'refresh' : 'download'" :class="{ 'bs-chat-spin': downloading }" size="sm" /></button>
      <button v-if="!hideFullscreen" type="button" class="bs-chat-icon" :aria-label="fullscreen ? '退出全屏' : '全屏预览'" :title="fullscreen ? '退出全屏' : '全屏预览'" @click="emit('fullscreen')"><Icon :name="fullscreen ? 'x' : 'externalLink'" size="sm" /></button>
      <button type="button" class="bs-chat-icon" aria-label="关闭文件工作区" title="关闭文件工作区" @click="emit('close')"><Icon name="x" size="sm" /></button>
    </header>
    <div v-if="file && ['markdown', 'text'].includes(kind) && !loading && !error" class="bso-preview-mode-toolbar"><div class="bso-segmented" role="tablist" aria-label="文件显示方式"><button type="button" role="tab" :aria-selected="!showSource" @click="showSource = false">预览</button><button type="button" role="tab" :aria-selected="showSource" @click="showSource = true">源码</button></div></div>
    <p v-if="printLoading" class="sr-only" role="status">正在准备打印版预览</p>
    <p v-if="printError" class="bs-chat-preview-error" role="alert"><Icon name="exclamationCircle" size="sm" /><span>{{ printError }}</span></p>
    <p v-if="error && !printURL" class="bs-chat-preview-error" role="alert"><Icon name="exclamationCircle" size="sm" /><span>{{ error }}</span><button type="button" class="bs-chat-text-button" @click="loadPreview(true)">重试</button></p>
    <WorkspaceFileList v-if="!file" :files="files" :loading="filesLoading" :load-failed="filesLoadFailed" :downloading="downloading" @preview="emit('preview', $event)" @download="download" />
    <iframe v-else-if="printURL" :src="printURL" :title="`${file.name} · 打印版预览`" class="bs-chat-pdf-preview" referrerpolicy="no-referrer"></iframe>
    <div v-else-if="loading" class="bs-chat-preview-loading bso-preview-loading" role="status"><Icon class="bs-chat-spin" name="refresh" size="md" />正在加载预览…</div>
    <div v-else-if="kind === 'image' && previewURL" class="bs-chat-image-preview"><img :src="previewURL" :alt="file.name" @error="imageFailed" /></div>
    <!-- The authenticated bounded/signature-checked PDF blob is kept intact.
         Toolbar/fullscreen changes never create a second viewer or object GET. -->
    <iframe v-else-if="kind === 'pdf' && previewURL" :src="previewURL" :title="file.name" class="bs-chat-pdf-preview" referrerpolicy="no-referrer"></iframe>
    <iframe v-else-if="kind === 'docx' && docxMarkup" :srcdoc="docxMarkup" :title="file.name" class="bs-chat-docx-preview" sandbox="" referrerpolicy="no-referrer"></iframe>
    <DrawioPreview v-else-if="kind === 'drawio' && text" :xml="text" :title="file.name" />
    <div v-else-if="kind === 'markdown' && !showSource" class="bs-chat-document-preview"><ChatMarkdown :content="text" :files="files" @preview="emit('preview', $event)" /></div>
    <pre v-else-if="['text', 'markdown'].includes(kind)" class="bs-chat-text-preview" :class="{ 'bso-source-preview': showSource }">{{ text }}</pre>
    <div v-else-if="!error" class="bs-chat-preview-unsupported"><WorkspaceFileIcon :file="file" /><strong>{{ file.name }}</strong><p>此格式暂不支持在线预览，可以下载查看。</p><button type="button" class="bs-chat-text-button" :disabled="downloading" @click="download(file)"><Icon name="download" size="sm" />下载文件</button></div>
  </section>
</template>
<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue'
import { saveAs } from 'file-saver'
import Icon from '@/components/icons/Icon.vue'
import ChatMarkdown from './ChatMarkdown.vue'
import WorkspaceFileList from './WorkspaceFileList.vue'
import WorkspaceFileIcon from './WorkspaceFileIcon.vue'
import './workbenchArtifacts.css'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatError } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import { isChatDocx, type ChatArtifact, type ChatResourceAccess } from '@/types/workbenchChat'
const DrawioPreview = defineAsyncComponent(() => import('./DrawioPreview.vue'))
const props = defineProps<{ files: ChatArtifact[]; file: ChatArtifact | null; filesLoading?: boolean; filesLoadFailed?: boolean; fullscreen?: boolean; hideFullscreen?: boolean; readOnly?: boolean; docxOnly?: boolean; editableExtensions?: string[]; loadAccess?: (versionId: string) => Promise<ChatResourceAccess> }>()
const emit = defineEmits<{ preview: [file: ChatArtifact | null]; close: []; fullscreen: []; edit: [file: ChatArtifact]; error: [message: string] }>()
const loading = ref(false), downloading = ref(false), error = ref(''), previewURL = ref(''), text = ref(''), kind = ref('unsupported')
const docxMarkup = ref('')
const canPrintPreview = ref(false)
const printLoading = ref(false), printURL = ref(''), printError = ref('')
const printLabel = computed(() => {
  if (printLoading.value) return '正在准备打印版预览'
  return printURL.value ? '返回快速预览' : '打印版预览'
})
const auth = useAuthStore()
const showSource = ref(false)
let epoch = 0, ownerEpoch = 0, fetchController: AbortController | undefined, downloadController: AbortController | undefined
let printController: AbortController | undefined
const maxAccessEntries = 24, maxPreviewEntries = 8, maxPreviewBytes = 64 * 1024 * 1024
const maxPDFBytes = 24 * 1024 * 1024, maxTextBytes = 2 * 1024 * 1024
const accessCache = new Map<string, ChatResourceAccess>()
type LoadedPreview = { kind: string; url: string; text: string; bytes: number; docx?: { markup: string; original: Blob } }
const previewCache = new Map<string, LoadedPreview>()
function discardPreview(id: string) { const value = previewCache.get(id); if (value?.url) URL.revokeObjectURL(value.url); previewCache.delete(id) }
function clearCaches() { accessCache.clear(); for (const id of previewCache.keys()) discardPreview(id) }
function cachePreview(id: string, value: LoadedPreview) {
  discardPreview(id); previewCache.set(id, value)
  let bytes = [...previewCache.values()].reduce((total, item) => total + item.bytes, 0)
  while (previewCache.size > maxPreviewEntries || bytes > maxPreviewBytes) { const first = previewCache.keys().next().value; if (!first) break; bytes -= previewCache.get(first)!.bytes; discardPreview(first) }
}
function showPreview(value: LoadedPreview) { kind.value = value.kind; previewURL.value = value.url; text.value = value.text; docxMarkup.value = value.docx?.markup || '' }
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
async function previewResponse(file: ChatArtifact, access: ChatResourceAccess, controller: AbortController, original = false) {
  const request = (value: ChatResourceAccess) => fetch(validURL((!original && value.preview_url) || value.url), { signal: controller.signal, referrerPolicy: 'no-referrer' })
  let response = await request(access)
  if (response.status === 403 && !controller.signal.aborted) response = await request(await getAccess(file, true))
  if (!response.ok) throw new Error('文件预览加载失败，请重试')
  return response
}
async function readPreview(response: Response, limit: number, truncate: boolean) {
  const sizeError = `预览文件超过 ${limit / (1024 * 1024)} MB，请下载原文件查看`
  if (!truncate && Number(response.headers.get('content-length')) > limit) {
    await response.body?.cancel().catch(() => {})
    throw new Error(sizeError)
  }
  const reader = response.body?.getReader()
  if (!reader) throw new Error('文件预览内容为空，请下载原文件查看')
  const chunks: Uint8Array[] = []; let bytes = 0, truncated = false
  try {
    while (true) {
      const part = await reader.read(); if (part.done) break
      if (bytes + part.value.length > limit) {
        if (!truncate) throw new Error(sizeError)
        chunks.push(part.value.subarray(0, limit - bytes)); bytes = limit; truncated = true; break
      }
      chunks.push(part.value); bytes += part.value.length
    }
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock() }
  const content = new Uint8Array(bytes); let offset = 0
  for (const chunk of chunks) { content.set(chunk, offset); offset += chunk.length }
  return { content, truncated }
}
const docxMime = 'application/vnd.openxmlformats-officedocument.wordprocessingml.document'
function previewKind(file: ChatArtifact, access: ChatResourceAccess) {
  const mime = file.mime || ''
  if (access.preview_url) return (access.preview_kind || file.preview_kind) === 'image' ? 'image' : 'pdf'
  if (mime === docxMime || isChatDocx(file)) return 'docx'
  if (/\.drawio$/i.test(file.name)) return 'drawio'
  return access.preview_kind || file.preview_kind || (mime.startsWith('image/') ? 'image' : mime === 'application/pdf' ? 'pdf' : /\.(md|markdown)$/i.test(file.name) ? 'markdown' : mime.startsWith('text/') ? 'text' : 'unsupported')
}
function printPreviewAllowed(file: ChatArtifact, access?: ChatResourceAccess) {
  return !props.loadAccess && isChatDocx(file) && access?.can_render_print_preview === true && !access.preview_url
}

async function pdfPreviewBlob(response: Response) {
  if (response.headers.get('content-type')?.split(';')[0]?.trim().toLowerCase() !== 'application/pdf') {
    await response.body?.cancel().catch(() => {})
    throw new Error('文件不是有效的 PDF 预览，请下载原文件查看')
  }
  const { content } = await readPreview(response, maxPDFBytes, false)
  if (new TextDecoder().decode(content.subarray(0, 5)) !== '%PDF-') throw new Error('文件不是有效的 PDF 预览，请下载原文件查看')
  return new Blob([content], { type: 'application/pdf' })
}

function resetPrintPreview() {
  printController?.abort()
  printController = undefined
  printLoading.value = false
  printError.value = ''
  printURL.value = ''
  canPrintPreview.value = false
}

function waitForPrintPreview(ms: number, signal: AbortSignal) {
  return new Promise<void>(resolve => {
    const finish = () => {
      clearTimeout(timeout)
      signal.removeEventListener('abort', finish)
      resolve()
    }
    const timeout = setTimeout(finish, ms)
    signal.addEventListener('abort', finish, { once: true })
  })
}

async function fetchPrintPreview(versionId: string, signal: AbortSignal) {
  const deadline = Date.now() + 180_000
  let refreshed = false
  let retry = true
  while (Date.now() < deadline) {
    signal.throwIfAborted()
    const result = await workbenchChatApi.printPreview(versionId, signal, retry)
    retry = false
    signal.throwIfAborted()
    if (result.status === 'ready') {
      const response = await fetch(validURL(result.preview_url), { signal, referrerPolicy: 'no-referrer' })
      if (response.status === 403 && !refreshed) {
        refreshed = true
        await response.body?.cancel().catch(() => {})
        continue
      }
      if (!response.ok) throw new Error('打印版预览加载失败，请重试')
      return pdfPreviewBlob(response)
    }
    if (result.status !== 'pending') throw new Error('打印版预览返回了无效状态')
    await waitForPrintPreview(Math.min(5_000, Math.max(500, result.retry_after_ms || 1_000)), signal)
  }
  throw new Error('打印版仍在准备中，稍后可以继续查看')
}

async function togglePrintPreview(file: ChatArtifact) {
  if (printLoading.value) return
  printError.value = ''
  if (printURL.value) {
    printURL.value = ''
    return
  }
  const cacheKey = `print:${file.version_id}`
  const cached = previewCache.get(cacheKey)
  if (cached) {
    previewCache.delete(cacheKey)
    previewCache.set(cacheKey, cached)
    printURL.value = cached.url
    return
  }
  const controller = new AbortController()
  printController = controller
  printLoading.value = true
  try {
    const blob = await fetchPrintPreview(file.version_id, controller.signal)
    if (printController !== controller || controller.signal.aborted) return
    const url = URL.createObjectURL(blob)
    cachePreview(cacheKey, { kind: 'pdf', url, text: '', bytes: blob.size })
    printURL.value = url
  } catch (e) {
    if (printController === controller && !controller.signal.aborted) printError.value = chatError(e)
  } finally {
    if (printController === controller) {
      printController = undefined
      printLoading.value = false
    }
  }
}

async function loadPreview(force = false) {
  const ownEpoch = ++epoch; fetchController?.abort(); const controller = new AbortController(); fetchController = controller
  const file = props.file; error.value = ''; previewURL.value = ''; text.value = ''; docxMarkup.value = ''; kind.value = 'unsupported'
  if (!file) { loading.value = false; return }
  const cached = previewCache.get(file.version_id)
  if (cached && !force) { previewCache.delete(file.version_id); previewCache.set(file.version_id, cached); showPreview(cached); canPrintPreview.value = printPreviewAllowed(file, accessCache.get(file.version_id)); loading.value = false; return }
  if (force) discardPreview(file.version_id)
  loading.value = true
  try {
    const access = await getAccess(file, force)
    if (ownEpoch !== epoch) return
    canPrintPreview.value = printPreviewAllowed(file, access)
    kind.value = previewKind(file, access)
    if (kind.value === 'image') previewURL.value = validURL(access.preview_url || access.url)
    else if (kind.value === 'pdf') {
      const blob = await pdfPreviewBlob(await previewResponse(file, access, controller))
      if (ownEpoch !== epoch) return
      const value = { kind: 'pdf', url: URL.createObjectURL(blob), text: '', bytes: blob.size }
      cachePreview(file.version_id, value); showPreview(value)
    }
    else if (kind.value === 'docx') {
      const { maxDOCXBytes, renderDOCXPreview } = await import('./docxPreview')
      if (ownEpoch !== epoch) return
      if (file.bytes > maxDOCXBytes) throw new Error('预览文件超过 24 MB，请下载原文件查看')
      const { content } = await readPreview(await previewResponse(file, access, controller, true), maxDOCXBytes, false)
      if (ownEpoch !== epoch) return
      const markup = await renderDOCXPreview(content, controller.signal)
      if (ownEpoch !== epoch) return
      const original = new Blob([content], { type: docxMime })
      const value = { kind: 'docx', url: '', text: '', bytes: original.size + markup.length * 2, docx: { markup, original } }
      cachePreview(file.version_id, value)
      showPreview(value)
    }
    else if (['text', 'markdown', 'drawio'].includes(kind.value)) {
      const { content, truncated } = await readPreview(await previewResponse(file, access, controller), maxTextBytes, kind.value !== 'drawio')
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
    const original = previewCache.get(file.version_id)?.docx?.original
    if (original) {
      if (owner === ownerEpoch) saveAs(original, file.name)
      return
    }
    let response = await fetch(validURL(access.url), { signal: controller.signal, referrerPolicy: 'no-referrer' })
    if (response.status === 403) { access = await getAccess(file, true); response = await fetch(validURL(access.url), { signal: controller.signal, referrerPolicy: 'no-referrer' }) }
    if (!response.ok) throw new Error('文件下载失败，请稍后重试')
    const blob = await response.blob()
    if (owner === ownerEpoch) saveAs(blob, file.name)
  } catch (e) { if (owner === ownerEpoch && !controller.signal.aborted) { error.value = chatError(e); emit('error', error.value) } }
  finally { if (owner === ownerEpoch) downloading.value = false; if (downloadController === controller) downloadController = undefined }
}
function editable(file: ChatArtifact) {
  if (props.editableExtensions) return props.editableExtensions.some(extension => file.name.toLowerCase().endsWith(extension.toLowerCase()))
  return props.docxOnly ? isChatDocx(file) : /\.(docx|xlsx|pptx)$/i.test(file.name)
}
function imageFailed() { error.value = '图片加载失败，请重试'; if (props.file) accessCache.delete(props.file.version_id) }
watch(() => props.file?.version_id, () => { resetPrintPreview(); showSource.value = false; void loadPreview() }, { immediate: true })
watch(() => auth.user?.id, () => { ++ownerEpoch; ++epoch; fetchController?.abort(); downloadController?.abort(); resetPrintPreview(); clearCaches(); previewURL.value = ''; text.value = ''; docxMarkup.value = ''; error.value = ''; loading.value = false; downloading.value = false }, { flush: 'sync' })
onBeforeUnmount(() => { ++ownerEpoch; ++epoch; fetchController?.abort(); downloadController?.abort(); resetPrintPreview(); clearCaches() })
defineExpose({ download })
</script>
