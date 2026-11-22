<template>
  <WorkbenchChatLayout>
    <div ref="surface" class="bs-chat bs-chat-surface" :class="{ 'has-workspace': panelOpen, 'preview-fullscreen': fullscreen, 'is-resizing': resize.dragging.value, 'is-selecting': selectionActive }">
      <div class="bs-chat-column">
        <template v-if="initializing"><div class="bs-chat-startup" role="status"><Icon name="refresh" class="bs-chat-spin" size="md" />正在打开工作台…</div></template>
        <div v-else-if="startupError || !store.status?.enabled" class="bs-chat-startup"><Icon name="chat" size="lg" /><p role="alert">{{ startupError || '对话助手尚未启用' }}</p><button v-if="startupError" type="button" class="bs-chat-text-button" @click="initialize">重新加载</button></div>
        <template v-else>
          <header v-if="!landing" class="bs-chat-header"><h1 :title="store.currentConversation?.title">{{ store.currentConversation?.title || '对话助手' }}</h1><button v-if="store.history.messages.length" type="button" class="bs-chat-icon" :title="store.hasMoreHistory ? '导出已加载的消息' : '导出对话'" aria-label="导出对话" @click="openExport()"><Icon name="download" size="sm" /></button><button v-if="allFiles.length" type="button" class="bs-chat-text-button" :aria-pressed="panelOpen && !referenceRunId" @click="toggleWorkspace"><Icon name="document" size="sm" />对话文件<span class="bs-chat-file-count">{{ allFiles.length }}</span></button></header>
          <div v-if="store.loading" class="bs-chat-startup" role="status"><Icon name="refresh" class="bs-chat-spin" size="md" />正在加载会话…</div>
          <template v-else-if="!landing">
            <div ref="scroll" class="bs-chat-message-scroll" @scroll.passive="onScroll">
              <div class="bs-chat-content"><button v-if="store.hasMoreHistory" type="button" class="bs-chat-earlier" :disabled="store.loadingEarlier" @click="loadEarlier">{{ store.loadingEarlier ? '正在加载…' : '加载更早消息' }}</button>
                <section v-for="turn in turns" :key="turn.run.id" class="bs-chat-turn">
                  <div v-for="message in turn.users" :key="message.id" class="bs-chat-user-message bs-chat-selectable"><input v-if="selectionActive" type="checkbox" class="bs-chat-message-check" :aria-label="`选择问题：${chatText(message.content).slice(0, 30)}`" :checked="selectedExportIds.has(message.id)" @change="toggleExportMessages([message.id])" /><div class="bs-chat-user-bubble">{{ chatText(message.content) }}</div><div class="bs-chat-user-actions"><time>{{ formatTime(message.created_at) }}</time><button type="button" class="bs-chat-icon" :aria-label="copied === message.id ? '已复制' : '复制问题'" @click="copy(message.id, chatText(message.content))"><Icon :name="copied === message.id ? 'check' : 'copy'" size="sm" /></button></div><div v-if="chatArtifacts(message.content).length" class="bs-chat-user-files"><button v-for="file in chatArtifacts(message.content)" :key="file.version_id" type="button" @click="openFile(file)"><Icon name="document" size="sm" />{{ file.name }}</button></div></div>
                  <article class="bs-chat-assistant-message bs-chat-selectable" :aria-busy="isWorking(turn.run)">
                    <input v-if="selectionActive && assistantMessageIds(turn.run.id).length" type="checkbox" class="bs-chat-message-check" aria-label="选择这条回答" :checked="assistantMessageIds(turn.run.id).every(id => selectedExportIds.has(id))" @change="toggleExportMessages(assistantMessageIds(turn.run.id))" />
                    <TaskProgress :run="turn.run" :events="eventsFor(turn.run.id)" :has-text="!!turn.text" @stop="store.cancelRun" @preview="openFile" @updated="refreshCurrent" />
                    <details v-if="turn.reasoningSummary" class="bs-chat-process">
                      <summary><Icon name="chevronRight" size="sm" />推理摘要</summary>
                      <p class="bs-chat-reasoning-summary">{{ turn.reasoningSummary }}</p>
                    </details>
                    <ResultSection v-if="turn.text || turn.files.length" :text="turn.text" :files="turn.files" :citations="turn.citations" @preview="openFile" @download="download" @reference="openReferences(turn.run.id, $event.id)" />

                    <div v-if="!isWorking(turn.run) && turn.run.status !== 'waiting_user'" class="bs-chat-message-actions"><button v-if="turn.text" type="button" class="bs-chat-icon" :aria-label="copied === turn.run.id ? '已复制' : '复制回答'" :title="copied === turn.run.id ? '已复制' : '复制回答'" @click="copy(turn.run.id, turn.text)"><Icon :name="copied === turn.run.id ? 'check' : 'copy'" size="sm" /></button><button v-if="turn.users.length && !store.activeRun" type="button" class="bs-chat-icon" :aria-label="['failed', 'cancelled'].includes(turn.run.status) ? '重试任务' : '重新生成'" :title="['failed', 'cancelled'].includes(turn.run.status) ? '重试任务' : '重新生成'" :disabled="store.submitting" @click="regenerate(turn.run, turn.users[0])"><Icon name="refresh" size="sm" /></button><span v-if="turn.versions.length > 1" class="bs-chat-answer-versions"><button type="button" class="bs-chat-icon" aria-label="上一个回答" :disabled="selectionActive || turn.versionIndex === 0" @click="showVersion(turn.root, turn.versions[turn.versionIndex - 1]!.run.id)"><Icon name="chevronLeft" size="sm" /></button>{{ turn.versionIndex + 1 }}/{{ turn.versions.length }}<button type="button" class="bs-chat-icon" aria-label="下一个回答" :disabled="selectionActive || turn.versionIndex === turn.versions.length - 1" @click="showVersion(turn.root, turn.versions[turn.versionIndex + 1]!.run.id)"><Icon name="chevronRight" size="sm" /></button></span><button v-if="turn.versions.length > 1 && !turn.run.selected && ['completed', 'partially_completed'].includes(turn.run.status)" type="button" class="bs-chat-text-button" :disabled="!!store.activeRun" @click="useAnswer(turn.run.id)">使用此回答继续</button><button v-if="turn.text" type="button" class="bs-chat-icon" aria-label="导出回答" title="导出" @click="openExport(turn.run.id)"><Icon name="download" size="sm" /></button><button v-if="turn.citations.length" type="button" class="bs-chat-reference-entry" @click="openReferences(turn.run.id)"><Icon name="globe" size="sm" />参考资料 <span>{{ turn.citations.length }}</span><Icon name="chevronRight" size="sm" /></button><time>{{ formatTime(turn.run.finished_at || turn.run.created_at) }}</time></div>
                  </article>
                </section>
                <div v-if="!turns.length && !store.error" class="bs-chat-empty-conversation">这段对话还没有消息</div>
              </div>
            </div>
          </template>
          <div v-if="selectionActive" class="bs-chat-composer-seat"><ChatExportToolbar :messages="selectedExportMessages" :title="store.currentConversation?.title" :all-selected="selectedExportMessages.length === exportMessages.length && !!exportMessages.length" @toggle-all="toggleAllExportMessages" @close="closeExportSelection" @exported="closeExportSelection" /><p v-if="store.hasMoreHistory" class="bs-chat-export-history-note">当前选择包含已加载消息；可向上加载更早消息后继续选择。</p></div>
          <div v-else class="bs-chat-composer-seat" :class="{ 'is-landing': landing }">
            <div v-if="landing" class="bs-chat-welcome">
              <img :src="app.siteLogo || '/logo.svg'" alt="" class="bs-chat-hero-mark" />
              <h1>今天想做什么？</h1>
            </div>
            <button v-else-if="scrolledUp" type="button" class="bs-chat-return-bottom" @click="scrollBottom(true)"><Icon name="arrowDown" size="sm" />回到底部</button>
            <ChatComposer
              ref="composer"
              v-model="draft"
              v-model:model="model"
              v-model:effort="effort"
              v-model:web-search="webSearch"
              :models="store.models"
              :attachments="attachments"
              :edit-target="editTarget"
              :disabled="!store.models.length || store.loading || hasIncompatibleFiles"
              :busy="!!store.activeRun || store.submitting"
              :waiting="store.activeRun?.status === 'waiting_user'"
              :uploading="uploading"
              :error="composerError"
              :elevated="landing"
              :search-enabled="searchEnabled"
              :search-price-per1000-usd="store.status.search_price_per_1000_usd"
              :search-unavailable-reason="searchUnavailableReason"
              :uploads-enabled="store.status.uploads_enabled && canAttach"
              :resources-enabled="store.status.files_enabled && canAttach"
              :max-files="store.status.max_files || 10"
              :attachment-extensions="attachmentExtensions"
              @send="send"
              @stop="store.cancelRun"
              @files="uploadFiles"
              @select-resources="selectResources"
              @remove-attachment="removeAttachment"
              @clear-edit="editTarget = null"
            />
          </div>
          <div v-if="store.error || store.connectionError || actionError" class="bs-chat-page-error" role="alert">{{ actionError || store.error || store.connectionError }}<button v-if="(store.error || store.connectionError) && !store.submitting" type="button" class="bs-chat-text-button" @click="refreshCurrent">同步会话</button></div>
          <div v-if="!store.models.length" class="bs-chat-page-error">当前工作台分组没有可用的文本模型。</div>
        </template>
      </div>
      <div class="bs-chat-preview-dock" :class="{ open: panelOpen && !previewIsOverlay }" :style="panelOpen && !previewIsOverlay ? resize.panelStyle.value : undefined">
        <div v-if="panelOpen && !previewIsOverlay" class="bs-chat-resize" role="separator" aria-label="调整文件与资料面板宽度" aria-orientation="vertical" :aria-valuemin="resize.limits.value.min" :aria-valuemax="resize.limits.value.max" :aria-valuenow="Number.parseInt(resize.panelStyle.value.width)" tabindex="0" @pointerdown="resize.start" @keydown="resize.keydown" @dblclick="resize.reset" />
        <Teleport to="body" :disabled="!previewIsOverlay">
          <div v-show="panelOpen" ref="previewOverlay" class="bs-chat bs-chat-preview-host" tabindex="-1" :class="{ 'bs-chat-preview-overlay': previewIsOverlay, 'is-fullscreen': fullscreen, 'is-mobile': mobile }" :style="fullscreen && !mobile ? resize.fullscreenStyle.value : undefined" :role="previewIsOverlay ? 'dialog' : undefined" :aria-modal="previewIsOverlay ? true : undefined" aria-label="文件预览" @keydown.esc="closePanel">
            <ChatReferencesPanel v-show="!!referenceRunId" :citations="activeCitations" :active-id="activeReferenceId" @close="closePanel" />
            <ResourcePreview ref="resourcePreview" v-show="!referenceRunId" @error="actionError = $event" :files="allFiles" :file="previewFile" :fullscreen="fullscreen || mobile" :hide-fullscreen="mobile" :docx-only="nativeChat" :editable-extensions="nativeChat ? officeExtensions : undefined" :read-only="!store.status?.office_enabled" @edit="editFile" @preview="previewFile = $event" @close="closePanel" @fullscreen="fullscreen = !fullscreen" />
          </div>
        </Teleport>
      </div>
    </div>
  </WorkbenchChatLayout>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import WorkbenchChatLayout from '@/components/layout/WorkbenchChatLayout.vue'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import ChatComposer from '@/components/workbench/chat/ChatComposer.vue'
import TaskProgress from '@/components/workbench/chat/TaskProgress.vue'
import ResultSection from '@/components/workbench/chat/ResultSection.vue'
import ResourcePreview from '@/components/workbench/chat/ResourcePreview.vue'
import ChatReferencesPanel from '@/components/workbench/chat/ChatReferencesPanel.vue'
import ChatExportToolbar from '@/components/workbench/chat/ChatExportToolbar.vue'
import { useWorkbenchResize } from '@/components/workbench/chat/useWorkbenchResize'
import { runArtifacts } from '@/components/workbench/chat/chatPresentation'
import { useWorkbenchChatStore, chatError } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import { workbenchChatApi } from '@/api/workbenchChat'
import { acceptsChatAttachment, CHAT_IMAGE_EXTENSIONS, chatArtifacts, chatText } from '@/types/workbenchChat'
import type { ChatArtifact, ChatCitation, ChatMessage, ChatResource, ChatRun, ChatSubmit } from '@/types/workbenchChat'
import '@/components/workbench/chat/chat.css'
const store = useWorkbenchChatStore(), auth = useAuthStore(), app = useAppStore(), route = useRoute(), router = useRouter()
const initializing = ref(true), startupError = ref(''), draft = ref(''), model = ref('')
const effortOverride = ref('')
const searchPreference = ref(true)
const editTarget = ref<{ version_id: string; name: string } | null>(null)
const attachments = ref<ChatResource[]>([]), uploading = ref(false), uploadError = ref(''), actionError = ref(''), copied = ref('')
const panelOpen = ref(false), fullscreen = ref(false), previewFile = ref<ChatArtifact | null>(null), referenceRunId = ref(''), activeReferenceId = ref('')
const selectionActive = ref(false), selectedExportIds = ref(new Set<string>())
const composer = ref<InstanceType<typeof ChatComposer> | null>(null), surface = ref<HTMLElement | null>(null), scroll = ref<HTMLElement | null>(null), scrolledUp = ref(false)
const resize = useWorkbenchResize(surface)
const previewOverlay = ref<HTMLElement | null>(null), resourcePreview = ref<InstanceType<typeof ResourcePreview> | null>(null)
let pageEpoch = 0, previousFocus: HTMLElement | null = null, overlayBackground: HTMLElement | null = null
let previousInert = false
const mobile = useMediaQuery('(max-width: 1023px)')
const previewIsOverlay = computed(() => panelOpen.value && (mobile.value || fullscreen.value))
const conversationId = computed(() => typeof route.params.conversationId === 'string' ? route.params.conversationId : '')
const selectedModel = computed(() => store.models.find(item => item.id === model.value))
const effort = computed({
  get() {
    const options = selectedModel.value?.reasoning_efforts ?? []
    if (options.includes(effortOverride.value)) return effortOverride.value
    const recommended = selectedModel.value?.default_reasoning_effort
    return recommended && options.includes(recommended) ? recommended : ''
  },
  set(value: string) {
    effortOverride.value = value
  },
})
const supportsTools = computed(() => !!selectedModel.value?.supports_tools)
const officeEnabled = computed(() => !!store.status?.office_enabled && supportsTools.value)
const nativeChat = computed(() => store.status?.browser_driven === true)
const officeExtensions = computed(() => store.status?.office_file_extensions ?? ['.docx'])
const attachmentExtensions = computed(() => nativeChat.value ? [
  ...(officeEnabled.value ? officeExtensions.value : []),
  ...(selectedModel.value?.supports_vision ? CHAT_IMAGE_EXTENSIONS : []),
] : undefined)
const canAttach = computed(() => attachmentExtensions.value ? attachmentExtensions.value.length > 0 : officeEnabled.value)
const hasIncompatibleFiles = computed(() => !!editTarget.value && !officeEnabled.value || attachments.value.some(file =>
  !canAttach.value || !acceptsChatAttachment(file, attachmentExtensions.value)))
const searchEnabled = computed(() => !!store.status?.search_enabled && supportsTools.value)
const searchPreferenceKey = computed(() => auth.user?.id ? `workbench-chat-search:${auth.user.id}` : '')
const webSearch = computed({
  get: () => searchEnabled.value && searchPreference.value,
  set(enabled: boolean) {
    searchPreference.value = enabled
    if (!searchPreferenceKey.value) return
    try { localStorage.setItem(searchPreferenceKey.value, enabled ? 'on' : 'off') }
    catch { /* Keep the selection in memory when browser storage is unavailable. */ }
  },
})
watch(searchPreferenceKey, key => {
  searchPreference.value = true
  if (!key) return
  try { searchPreference.value = localStorage.getItem(key) !== 'off' }
  catch { /* Default to search when browser storage is unavailable. */ }
}, { immediate: true, flush: 'sync' })
const searchUnavailableReason = computed(() => {
  if (!store.status?.search_enabled) return store.status?.search_allowed ? '工作台搜索供应商尚未就绪，请联系管理员。' : ''
  return supportsTools.value ? '' : '当前模型未确认支持工具调用，请更换模型。'
})
const composerError = computed(() => {
  if (uploadError.value) return uploadError.value
  if (hasIncompatibleFiles.value) return '请选择支持当前附件的模型，或移除附件后继续。'
  return ''
})
const landing = computed(() => !conversationId.value && !store.activeConversationId && !store.history.messages.length)
const eventsFor = (id: string) => store.events[id] || []
const allTurns = computed(() => store.history.runs.map(run => {
  const messages = store.history.messages.filter(m => m.run_id === run.id)
  const assistants = messages.filter(m => m.role === 'assistant')
  const persisted = assistants.map(m => chatText(m.content)).join('\n\n')
  const runEvents = eventsFor(run.id)
  const partial = runEvents.find(event => event.type === 'assistant.partial')?.payload
  const text = persisted || (typeof partial?.text === 'string' ? partial.text : '')
    || runEvents.filter(event => event.type === 'assistant.delta' && typeof event.payload.text === 'string').map(event => event.payload.text).join('')
  const reasoningSummary = assistants.flatMap(message => typeof message.content === 'string' ? [] : message.content.reasoning_summary || []).join('\n\n')
    || (typeof partial?.reasoning_summary === 'string' ? partial.reasoning_summary : '')
    || runEvents.filter(event => event.type === 'reasoning.delta' && typeof event.payload.text === 'string').map(event => event.payload.text).join('')
  const files = new Map<string, ChatArtifact>(runArtifacts(runEvents, run).map(file => [file.version_id, file]))
  const citations: ChatCitation[] = []
  for (const message of assistants) { for (const file of chatArtifacts(message.content)) files.set(file.version_id, file); if (typeof message.content !== 'string') citations.push(...message.content.citations || []) }
  return { run, users: messages.filter(m => m.role === 'user'), text, reasoningSummary, files: [...files.values()], citations }
}))
const visibleVersions = ref<Record<string, string>>({})
const turns = computed(() => {
  const groups = new Map<string, typeof allTurns.value>()
  for (const turn of allTurns.value) { const root = turn.run.regeneration_of || turn.run.id; const list = groups.get(root) || []; list.push(turn); groups.set(root, list) }
  return [...groups.entries()].map(([root, versions]) => {
    const active = versions.find(v => ['running', 'queued', 'recovering', 'waiting_user', 'needs_confirmation'].includes(v.run.status))
    const chosen = active || versions.find(v => v.run.id === visibleVersions.value[root]) || [...versions].reverse().find(v => v.run.selected) || versions.at(-1)!
    return { ...chosen, users: versions[0]?.users.length ? versions[0].users : chosen.users, root, versions, versionIndex: versions.findIndex(v => v.run.id === chosen.run.id) }
  })
})
const activeCitations = computed(() => allTurns.value.find(turn => turn.run.id === referenceRunId.value)?.citations || [])
function showVersion(root: string, id: string) { visibleVersions.value[root] = id; void store.loadRunEvents(id).catch(() => {}) }
async function useAnswer(id: string) { actionError.value = ''; try { await store.selectAnswer(id) } catch(e) { actionError.value = chatError(e) } }
// Export exactly the displayed user/answer pairs, preserving turn order even
// when the selected regeneration was produced after a later turn.
const exportMessages = computed(() => [...new Map(turns.value.flatMap(turn => [...turn.users, ...store.history.messages.filter(message => message.run_id === turn.run.id && message.role === 'assistant')]).map(message => [message.id, message])).values()])
const selectedExportMessages = computed(() => exportMessages.value.filter(message => selectedExportIds.value.has(message.id)))
function assistantMessageIds(runId: string) { return exportMessages.value.filter(message => message.run_id === runId && message.role === 'assistant').map(message => message.id) }
function openExport(runId?: string) {
  if (selectionActive.value) { closeExportSelection(); return }
  const turn = turns.value.find(item => item.run.id === runId)
  selectedExportIds.value = new Set(turn ? [...turn.users.map(message => message.id), ...assistantMessageIds(turn.run.id)] : exportMessages.value.map(message => message.id))
  selectionActive.value = true
}
function toggleExportMessages(ids: string[]) { const next = new Set(selectedExportIds.value), remove = ids.every(id => next.has(id)); for (const id of ids) { if (remove) next.delete(id); else next.add(id) } selectedExportIds.value = next }
function toggleAllExportMessages() { selectedExportIds.value = selectedExportMessages.value.length === exportMessages.value.length ? new Set() : new Set(exportMessages.value.map(message => message.id)) }
function closeExportSelection() { selectionActive.value = false; selectedExportIds.value = new Set() }
const allFiles = computed(() => { const files = new Map<string, ChatArtifact>(); for (const message of store.history.messages) for (const file of chatArtifacts(message.content)) files.set(file.version_id, file); for (const turn of allTurns.value) for (const file of turn.files) files.set(file.version_id, file); return [...files.values()] })
const preferredKey = computed(() => `workbench-chat-model:${auth.user?.id || ''}`)
async function initialize() { initializing.value = true; startupError.value = ''; try { await store.initialize(); const preferred = localStorage.getItem(preferredKey.value); model.value = store.models.some(m => m.id === preferred) ? preferred! : store.modelCatalog.default_model || store.models[0]?.id || ''; await loadRoute(true) } catch (e) { startupError.value = chatError(e) } finally { initializing.value = false } }
function readEditTarget() {
  const version = route.query.edit_version, name = route.query.edit_name
  if (typeof version !== 'string' || !/^[a-f0-9-]{36}$/i.test(version)) return
  const target = { version_id: version, name: typeof name === 'string' ? name.slice(0, 240) : '选中的文件' }
  if (nativeChat.value && !acceptsChatAttachment(target, officeExtensions.value)) uploadError.value = '当前文档工具不支持修改这个文件格式'
  else editTarget.value = target
  void router.replace({ path: route.path, query: {} })
}
async function loadRoute(refresh = false) { const changed = store.activeConversationId !== conversationId.value; if (changed) { ++pageEpoch; uploading.value = false; draft.value = ''; attachments.value = []; uploadError.value = ''; closePanel(); closeExportSelection(); scrolledUp.value = false; editTarget.value = null; visibleVersions.value = {} } await store.selectConversation(conversationId.value, refresh); readEditTarget(); await nextTick(); scrollBottom(true) }
watch(conversationId, () => { if (!initializing.value) void loadRoute() })
watch(() => auth.user?.id, () => { ++pageEpoch; uploading.value = false; uploadError.value = ''; actionError.value = ''; draft.value = ''; attachments.value = []; editTarget.value = null; closePanel(); closeExportSelection(); startupError.value = ''; model.value = '' }, { flush: 'sync' })
watch(() => route.query.edit_version, () => { if (!initializing.value) readEditTarget() })
watch(model, value => { effortOverride.value = ''; if (value) localStorage.setItem(preferredKey.value, value) }, { flush: 'sync' })
watch(mobile, () => { resize.stop(); fullscreen.value = false })
watch(previewIsOverlay, async value => {
  if (value) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    overlayBackground = surface.value?.closest<HTMLElement>('.workbench-chat-layout') || null
    if (overlayBackground) { previousInert = overlayBackground.inert; overlayBackground.inert = true }
    await nextTick()
    const first = [...previewOverlay.value?.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], [tabindex="0"]') || []].find(element => element.getClientRects().length)
    ;(first || previewOverlay.value)?.focus()
  } else {
    if (overlayBackground) overlayBackground.inert = previousInert
    overlayBackground = null
    if (previousFocus?.isConnected && previousFocus.matches('button, a[href], input, textarea, [tabindex], [contenteditable=true]') && previousFocus.getClientRects().length) previousFocus.focus()
    else composer.value?.focus()
    previousFocus = null
  }
})
watch(() => store.history.messages.length + Object.values(store.events).reduce((n, e) => n + e.length, 0), () => nextTick(() => scrollBottom()))
watch(() => store.events, () => nextTick(() => scrollBottom()), { deep: true })
async function submit(input: Omit<ChatSubmit, 'run_id'>) {
  actionError.value = ''
  const owner = auth.user?.id
  try {
    const searchPrice = store.status?.search_price_per_1000_usd ?? 0
    const id = await store.send(input.web_search ? { ...input, search_price_per_1000_usd: searchPrice } : input)
    if (id && owner === auth.user?.id && store.activeConversationId === id) {
      draft.value = ''
      attachments.value = []
      editTarget.value = null
      scrolledUp.value = false
      if (conversationId.value !== id) await router.replace(`/workbench/chat/${id}`)
      await nextTick()
      scrollBottom(true)
    }
  } catch (error) {
    if (owner !== auth.user?.id) return
    if (store.activeConversationId && conversationId.value !== store.activeConversationId) {
      await router.replace(`/workbench/chat/${store.activeConversationId}`)
    }
    const failure = error as { code?: string; reason?: string; error?: { code?: string } } | null
    if ([failure?.code, failure?.reason, failure?.error?.code].includes('WORKBENCH_SEARCH_PRICE_CHANGED')) {
      actionError.value = '联网搜索价格已变更，请确认输入栏显示的新单价后再发送。'
      try {
        await store.initialize()
      } catch {
        if (owner === auth.user?.id) actionError.value = '联网搜索价格已变更，请刷新页面获取新单价。'
      }
    }
  }
}
function send() {
  if (!draft.value.trim() || !model.value || uploading.value || hasIncompatibleFiles.value) return
  if (new TextEncoder().encode(draft.value).byteLength > 32768) {
    uploadError.value = '消息过长，请精简后发送（最多 32 KB）'
    return
  }
  uploadError.value = ''
  void submit({
    model: model.value, content: draft.value, web_search: webSearch.value,
    ...(editTarget.value ? { target_resource_version_id: editTarget.value.version_id } : {}),
    ...(effort.value ? { reasoning_effort: effort.value } : {}),
    ...(attachments.value.length ? { resource_version_ids: attachments.value.map(file => file.version_id) } : {}),
  })
}
async function regenerate(run: ChatRun, message: ChatMessage) {
  const oldFiles = chatArtifacts(message.content)
  if (oldFiles.some(file => !acceptsChatAttachment(file, attachmentExtensions.value))) {
    actionError.value = '当前模型无法处理这条消息中的附件，请切换模型后重试。'
    return
  }
  await submit({
    model: model.value || run.model, content: chatText(message.content), web_search: webSearch.value,
    ...(['completed', 'partially_completed'].includes(run.status) ? { regenerate_run_id: run.regeneration_of || run.id } : {}),
    ...(effort.value ? { reasoning_effort: effort.value } : {}),
    ...(oldFiles.length ? { resource_version_ids: oldFiles.map(file => file.version_id) } : {}),
  })
}
async function uploadFiles(files: File[]) {
  if (uploading.value || !store.status?.uploads_enabled || !canAttach.value) return
  if (files.some(file => !acceptsChatAttachment(file, attachmentExtensions.value))) { uploadError.value = '当前模型支持添加 ' + attachmentExtensions.value?.join('、'); return }
  const maxFiles = store.status.max_files || 10
  const maxBytes = store.status.max_file_bytes || 24 * 1024 * 1024
  uploadError.value = ''
  if (files.length + attachments.value.length > maxFiles) { uploadError.value = `每次最多添加 ${maxFiles} 个文件`; return }
  const oversized = files.find(file => file.size > maxBytes)
  if (oversized) { uploadError.value = `「${oversized.name}」超过 ${Math.floor(maxBytes / 1024 / 1024)} MB 限制`; return }
  uploading.value = true
  const id = store.activeConversationId, owner = auth.user?.id, own = pageEpoch
  const isCurrent = () => owner === auth.user?.id && own === pageEpoch && id === store.activeConversationId
  try {
    for (const file of files) {
      if (!isCurrent()) return
      const resource = await workbenchChatApi.upload(file)
      if (isCurrent()) attachments.value.push(resource)
    }
  } catch (e) {
    if (isCurrent()) uploadError.value = chatError(e)
  } finally {
    if (isCurrent()) uploading.value = false
  }
}
function selectResources(files: ChatResource[]) {
  if (!canAttach.value || !store.status?.files_enabled || uploading.value || store.activeRun) return
  if (files.some(file => !acceptsChatAttachment(file, attachmentExtensions.value))) { uploadError.value = '当前模型支持添加 ' + attachmentExtensions.value?.join('、'); return }
  const selected = new Map(attachments.value.map(file => [file.version_id, file]))
  for (const file of files) selected.set(file.version_id, file)
  if (selected.size > (store.status.max_files || 10)) { uploadError.value = `每次最多添加 ${store.status.max_files || 10} 个文件`; return }
  attachments.value = [...selected.values()]
  uploadError.value = ''
}
function removeAttachment(id: string) { attachments.value = attachments.value.filter(f => f.version_id !== id) }
function toggleWorkspace() { const showingFiles = panelOpen.value && !referenceRunId.value; referenceRunId.value = ''; previewFile.value = null; panelOpen.value = !showingFiles; fullscreen.value = false }
function openReferences(runId: string, sourceId = '') { activeReferenceId.value = sourceId; referenceRunId.value = runId; panelOpen.value = true; fullscreen.value = false }
function openFile(file: ChatArtifact) { referenceRunId.value = ''; previewFile.value = file; panelOpen.value = true }
function editFile(file: ChatArtifact) { editTarget.value = { version_id: file.version_id, name: file.name }; if (mobile.value || fullscreen.value) closePanel(); void nextTick(() => composer.value?.focus()) }
function closePanel() { resize.stop(); panelOpen.value = false; fullscreen.value = false; previewFile.value = null; referenceRunId.value = ''; activeReferenceId.value = '' }
function download(file: ChatArtifact) { actionError.value = ''; void resourcePreview.value?.download(file) }
async function copy(id: string, value: string) { try { await navigator.clipboard.writeText(value); copied.value = id } catch { actionError.value = '复制失败，请手动选择内容复制' } }
async function refreshCurrent() { await store.selectConversation(store.activeConversationId, true) }
async function loadEarlier() { const el = scroll.value; const height = el?.scrollHeight || 0; const top = el?.scrollTop || 0; scrolledUp.value = true; await store.loadEarlier(); await nextTick(); if (el) el.scrollTop = top + el.scrollHeight - height }
function onScroll() { const el = scroll.value; if (el) scrolledUp.value = el.scrollHeight - el.scrollTop - el.clientHeight > 100 }
function scrollBottom(force = false) { const el = scroll.value; if (el && (force || !scrolledUp.value)) { el.scrollTop = el.scrollHeight; scrolledUp.value = false } }
function isWorking(run: ChatRun) { return ['queued', 'running', 'recovering', 'needs_confirmation'].includes(run.status) }
function formatTime(value: string) { const date = new Date(value); return Number.isNaN(date.getTime()) ? '' : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }
function escapePreview(event: KeyboardEvent) {
  if (event.key === 'Escape' && panelOpen.value) { event.preventDefault(); closePanel(); return }
  if (event.key !== 'Tab' || !previewIsOverlay.value || !previewOverlay.value) return
  const focusable = [...previewOverlay.value.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], iframe, [tabindex="0"]')].filter(el => el.offsetParent !== null)
  const first = focusable[0], last = focusable.at(-1)
  if (!first || !last) { event.preventDefault(); previewOverlay.value.focus(); return }
  if (event.shiftKey && (document.activeElement === first || !previewOverlay.value.contains(document.activeElement))) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && (document.activeElement === last || !previewOverlay.value.contains(document.activeElement))) { event.preventDefault(); first.focus() }
}
onMounted(() => { void initialize(); window.addEventListener('keydown', escapePreview) })
onBeforeUnmount(() => { ++pageEpoch; if (overlayBackground) overlayBackground.inert = previousInert; previousFocus?.focus(); store.pause(); window.removeEventListener('keydown', escapePreview) })
</script>
