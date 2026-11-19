<template>
  <AppLayout immersive>
    <div ref="surface" class="bs-chat bs-chat-surface" :class="{ 'has-workspace': panelOpen, 'preview-fullscreen': fullscreen }">
      <div class="bs-chat-column">
        <template v-if="initializing"><div class="bs-chat-startup" role="status"><Icon name="refresh" class="bs-chat-spin" size="md" />正在打开工作台…</div></template>
        <div v-else-if="startupError || !store.status?.enabled" class="bs-chat-startup"><Icon name="chat" size="lg" /><p role="alert">{{ startupError || '对话助手尚未启用' }}</p><button v-if="startupError" type="button" class="bs-chat-text-button" @click="initialize">重新加载</button></div>
        <template v-else>
          <header v-if="!landing" class="bs-chat-header"><h1 :title="store.currentConversation?.title">{{ store.currentConversation?.title || '对话助手' }}</h1><button v-if="allFiles.length || store.history.runs.some(r => r.task_mode || r.mode === 'office')" type="button" class="bs-chat-text-button" :aria-pressed="panelOpen" @click="panelOpen = !panelOpen; fullscreen = false"><Icon name="document" size="sm" />文件工作区<span v-if="allFiles.length" class="bs-chat-file-count">{{ allFiles.length }}</span></button></header>
          <div v-if="store.loading" class="bs-chat-startup" role="status"><Icon name="refresh" class="bs-chat-spin" size="md" />正在加载会话…</div>
          <div v-else-if="landing" class="bs-chat-landing">
            <div class="bs-chat-hero"><div class="bs-chat-welcome"><Icon name="sparkles" size="lg" /><h1>有什么可以帮你？</h1></div><p class="bs-chat-welcome-description">问问题、查资料、处理文件</p><ChatComposer ref="composer" v-model="draft" v-model:model="model" v-model:effort="effort" v-model:task-mode="taskMode" v-model:web-search="webSearch" :models="store.models" :attachments="attachments" :edit-target="editTarget" :disabled="!store.models.length || (!!editTarget || attachments.length > 0) && !officeEnabled" :busy="store.submitting" :uploading="uploading" :error="uploadError || ((editTarget || attachments.length > 0) && !officeEnabled ? '处理文件需要选择支持任务模式的模型，并启用办公功能' : '')" :office-enabled="officeEnabled" :search-enabled="store.status.search_enabled && !!store.models.find(m => m.id === model)?.supports_tools" :uploads-enabled="store.status.uploads_enabled && officeEnabled" elevated @send="send" @stop="store.cancelRun" @files="uploadFiles" @remove-attachment="removeAttachment" @clear-edit="editTarget = null" /><div class="bs-chat-starters"><button v-for="starter in starters" :key="starter" type="button" @click="draft = starter; composer?.focus()">{{ starter }}</button></div></div>
          </div>
          <template v-else>
            <div ref="scroll" class="bs-chat-message-scroll" @scroll.passive="onScroll">
              <div class="bs-chat-content"><button v-if="store.hasMoreHistory" type="button" class="bs-chat-earlier" :disabled="store.loadingEarlier" @click="loadEarlier">{{ store.loadingEarlier ? '正在加载…' : '加载更早消息' }}</button>
                <section v-for="turn in turns" :key="turn.run.id" class="bs-chat-turn">
                  <div v-for="message in turn.users" :key="message.id" class="bs-chat-user-message"><div class="bs-chat-user-bubble">{{ chatText(message.content) }}</div><div v-if="chatArtifacts(message.content).length" class="bs-chat-user-files"><button v-for="file in chatArtifacts(message.content)" :key="file.version_id" type="button" @click="openFile(file)"><Icon name="document" size="sm" />{{ file.name }}</button></div></div>
                  <article class="bs-chat-assistant-message" :aria-busy="isWorking(turn.run)">
                    <TaskProgress :run="turn.run" :events="eventsFor(turn.run.id)" @stop="store.cancelRun" @updated="refreshCurrent" />
                    <ResultSection v-if="turn.text || turn.files.length" :text="turn.text" :files="turn.files" @preview="openFile" @download="download" />
                    <nav v-if="turn.citations.length" class="bs-chat-citations" aria-label="引用来源"><a v-for="citation in turn.citations" :key="citation.id" :href="safeLink(citation.url)" target="_blank" rel="noopener noreferrer" :title="citation.excerpt">{{ citation.title }}</a></nav>
                    <div v-if="!isWorking(turn.run) && turn.run.status !== 'waiting_user'" class="bs-chat-message-actions"><button v-if="turn.text" type="button" class="bs-chat-icon" :aria-label="copied === turn.run.id ? '已复制' : '复制回答'" :title="copied === turn.run.id ? '已复制' : '复制回答'" @click="copy(turn.run.id, turn.text)"><Icon :name="copied === turn.run.id ? 'check' : 'copy'" size="sm" /></button><button v-if="turn.users.length && !store.activeRun" type="button" class="bs-chat-icon" :aria-label="['failed', 'cancelled'].includes(turn.run.status) ? '重试任务' : '重新生成'" :title="['failed', 'cancelled'].includes(turn.run.status) ? '重试任务' : '重新生成'" :disabled="store.submitting" @click="regenerate(turn.run, turn.users[0])"><Icon name="refresh" size="sm" /></button><span v-if="turn.versions.length > 1" class="bs-chat-answer-versions"><button type="button" class="bs-chat-icon" aria-label="上一个回答" :disabled="turn.versionIndex === 0" @click="showVersion(turn.root, turn.versions[turn.versionIndex - 1]!.run.id)"><Icon name="chevronLeft" size="sm" /></button>{{ turn.versionIndex + 1 }}/{{ turn.versions.length }}<button type="button" class="bs-chat-icon" aria-label="下一个回答" :disabled="turn.versionIndex === turn.versions.length - 1" @click="showVersion(turn.root, turn.versions[turn.versionIndex + 1]!.run.id)"><Icon name="chevronRight" size="sm" /></button></span><button v-if="turn.versions.length > 1 && !turn.run.selected && ['completed', 'partially_completed'].includes(turn.run.status)" type="button" class="bs-chat-text-button" :disabled="!!store.activeRun" @click="useAnswer(turn.run.id)">使用此回答继续</button><span>{{ formatTime(turn.run.created_at) }}</span><span v-if="turn.run.model_calls">{{ turn.run.model_calls }} 次模型调用</span></div>
                  </article>
                </section>
                <div v-if="!turns.length && !store.error" class="bs-chat-empty-conversation">这段对话还没有消息</div>
              </div>
            </div>
            <div class="bs-chat-composer-seat"><button v-if="scrolledUp" type="button" class="bs-chat-return-bottom" @click="scrollBottom(true)"><Icon name="arrowDown" size="sm" />回到底部</button><PinnedTaskPanel v-if="latestRun" :key="latestRun.id" :run="latestRun" :events="eventsFor(latestRun.id)" /><ChatComposer ref="composer" v-model="draft" v-model:model="model" v-model:effort="effort" v-model:task-mode="taskMode" v-model:web-search="webSearch" :models="store.models" :attachments="attachments" :edit-target="editTarget" :disabled="!store.models.length || store.loading || (!!editTarget || attachments.length > 0) && !officeEnabled" :busy="!!store.activeRun || store.submitting" :waiting="store.activeRun?.status === 'waiting_user'" :uploading="uploading" :error="uploadError || ((editTarget || attachments.length > 0) && !officeEnabled ? '处理文件需要选择支持任务模式的模型，并启用办公功能' : '')" :office-enabled="officeEnabled" :search-enabled="store.status.search_enabled && !!store.models.find(m => m.id === model)?.supports_tools" :uploads-enabled="store.status.uploads_enabled && officeEnabled" @send="send" @stop="store.cancelRun" @files="uploadFiles" @remove-attachment="removeAttachment" @clear-edit="editTarget = null" /></div>
          </template>
          <div v-if="store.error || store.connectionError || actionError" class="bs-chat-page-error" role="alert">{{ actionError || store.error || store.connectionError }}<button v-if="store.error && !store.submitting" type="button" class="bs-chat-text-button" @click="refreshCurrent">同步会话</button></div>
          <div v-if="!store.models.length" class="bs-chat-page-error">当前工作台分组没有可用的文本模型。</div>
        </template>
      </div>
      <div class="bs-chat-preview-dock" :class="{ open: panelOpen && !previewIsOverlay }">
        <Teleport to="body" :disabled="!previewIsOverlay">
          <div v-show="panelOpen" ref="previewOverlay" class="bs-chat bs-chat-preview-host" tabindex="-1" :class="{ 'bs-chat-preview-overlay': previewIsOverlay, 'is-fullscreen': fullscreen, 'is-phone': phone }" :role="previewIsOverlay ? 'dialog' : undefined" :aria-modal="previewIsOverlay ? true : undefined" aria-label="文件预览" @keydown.esc="closePanel">
            <ResourcePreview :files="allFiles" :file="previewFile" :fullscreen="fullscreen || phone" :hide-fullscreen="mobile" @edit="editFile" @preview="previewFile = $event" @close="closePanel" @fullscreen="fullscreen = !fullscreen" />
          </div>
        </Teleport>
      </div>
    </div>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useMediaQuery } from '@vueuse/core'
import { saveAs } from 'file-saver'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import ChatComposer from '@/components/workbench/chat/ChatComposer.vue'
import TaskProgress from '@/components/workbench/chat/TaskProgress.vue'
import PinnedTaskPanel from '@/components/workbench/chat/PinnedTaskPanel.vue'
import ResultSection from '@/components/workbench/chat/ResultSection.vue'
import ResourcePreview from '@/components/workbench/chat/ResourcePreview.vue'
import { runArtifacts } from '@/components/workbench/chat/chatPresentation'
import { useWorkbenchChatStore, chatError } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import { workbenchChatApi } from '@/api/workbenchChat'
import { chatArtifacts, chatText } from '@/types/workbenchChat'
import type { ChatArtifact, ChatCitation, ChatMessage, ChatResource, ChatRun, ChatSubmit } from '@/types/workbenchChat'
import '@/components/workbench/chat/chat.css'
const store = useWorkbenchChatStore(), auth = useAuthStore(), route = useRoute(), router = useRouter()
const initializing = ref(true), startupError = ref(''), draft = ref(''), model = ref(''), effort = ref(''), taskMode = ref(false), webSearch = ref(false)
const editTarget = ref<{ version_id: string; name: string } | null>(null)
const attachments = ref<ChatResource[]>([]), uploading = ref(false), uploadError = ref(''), actionError = ref(''), copied = ref('')
const panelOpen = ref(false), fullscreen = ref(false), previewFile = ref<ChatArtifact | null>(null)
const composer = ref<InstanceType<typeof ChatComposer> | null>(null), surface = ref<HTMLElement | null>(null), scroll = ref<HTMLElement | null>(null), scrolledUp = ref(false)
const previewOverlay = ref<HTMLElement | null>(null)
let pageEpoch = 0, previousFocus: HTMLElement | null = null
const mobile = useMediaQuery('(max-width: 1023px)'), phone = useMediaQuery('(max-width: 576px)')
const previewIsOverlay = computed(() => panelOpen.value && (mobile.value || fullscreen.value))
const conversationId = computed(() => typeof route.params.conversationId === 'string' ? route.params.conversationId : '')
const officeEnabled = computed(() => !!store.status?.office_enabled && !!store.models.find(m => m.id === model.value)?.supports_tools)
const landing = computed(() => !conversationId.value && !store.activeConversationId && !store.history.messages.length)
const latestRun = computed(() => store.history.runs.at(-1))
const starters = ['帮我梳理一份工作计划', '总结文档中的关键内容', '分析表格并制作汇报']
const eventsFor = (id: string) => store.events[id] || []
const allTurns = computed(() => store.history.runs.map(run => {
  const messages = store.history.messages.filter(m => m.run_id === run.id)
  const assistants = messages.filter(m => m.role === 'assistant')
  const persisted = assistants.map(m => chatText(m.content)).join('\n\n')
  const text = persisted || eventsFor(run.id).filter(e => e.type === 'assistant.delta' && typeof e.payload.text === 'string').map(e => e.payload.text).join('')
  const files = new Map<string, ChatArtifact>(runArtifacts(eventsFor(run.id), run).map(f => [f.version_id, f]))
  const citations: ChatCitation[] = []
  for (const message of assistants) { for (const file of chatArtifacts(message.content)) files.set(file.version_id, file); if (typeof message.content !== 'string') citations.push(...message.content.citations || []) }
  return { run, users: messages.filter(m => m.role === 'user'), text, files: [...files.values()], citations }
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
function showVersion(root: string, id: string) { visibleVersions.value[root] = id; void store.loadRunEvents(id).catch(() => {}) }
async function useAnswer(id: string) { actionError.value = ''; try { await store.selectAnswer(id) } catch(e) { actionError.value = chatError(e) } }
const allFiles = computed(() => { const files = new Map<string, ChatArtifact>(); for (const message of store.history.messages) for (const file of chatArtifacts(message.content)) files.set(file.version_id, file); for (const turn of allTurns.value) for (const file of turn.files) files.set(file.version_id, file); return [...files.values()] })
const preferredKey = computed(() => `workbench-chat-model:${auth.user?.id || ''}`)
async function initialize() { initializing.value = true; startupError.value = ''; try { await store.initialize(); const preferred = localStorage.getItem(preferredKey.value); model.value = store.models.some(m => m.id === preferred) ? preferred! : store.modelCatalog.default_model || store.models[0]?.id || ''; await loadRoute() } catch (e) { startupError.value = chatError(e) } finally { initializing.value = false } }
function readEditTarget() { const version = route.query.edit_version, name = route.query.edit_name; if (typeof version === 'string' && /^[a-f0-9-]{36}$/i.test(version)) { editTarget.value = { version_id: version, name: typeof name === 'string' ? name.slice(0, 240) : '选中的文件' }; if (officeEnabled.value) taskMode.value = true; void router.replace({ path: route.path, query: {} }) } }
async function loadRoute() { const changed = store.activeConversationId !== conversationId.value; if (changed) { ++pageEpoch; uploading.value = false; draft.value = ''; attachments.value = []; uploadError.value = ''; closePanel(); scrolledUp.value = false; editTarget.value = null; visibleVersions.value = {} } await store.selectConversation(conversationId.value); readEditTarget(); await nextTick(); scrollBottom(true) }
watch(conversationId, () => { if (!initializing.value) void loadRoute() })
watch(() => auth.user?.id, () => { ++pageEpoch; uploading.value = false; uploadError.value = ''; actionError.value = ''; draft.value = ''; attachments.value = []; editTarget.value = null; closePanel(); startupError.value = ''; model.value = ''; }, { flush: 'sync' })
watch(() => route.query.edit_version, () => { if (!initializing.value) readEditTarget() })
watch(model, value => { effort.value = ''; if (value) localStorage.setItem(preferredKey.value, value); if (!officeEnabled.value) taskMode.value = false; else if (editTarget.value || attachments.value.length) taskMode.value = true; if (!store.models.find(m => m.id === value)?.supports_tools) webSearch.value = false })
watch(taskMode, value => { if (!value && editTarget.value && officeEnabled.value) editTarget.value = null })
watch(mobile, () => { fullscreen.value = false })
watch(previewIsOverlay, async value => {
  if (value) { previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null; await nextTick(); previewOverlay.value?.querySelector<HTMLElement>('button')?.focus() }
  else { previousFocus?.focus(); previousFocus = null }
})
watch(() => store.history.messages.length + Object.values(store.events).reduce((n, e) => n + e.length, 0), () => nextTick(() => scrollBottom()))
watch(() => store.events, () => nextTick(() => scrollBottom()), { deep: true })
async function submit(input: Omit<ChatSubmit, 'run_id'>) { actionError.value = ''; try { const owner = auth.user?.id; const id = await store.send(input); if (id && owner === auth.user?.id && store.activeConversationId === id) { draft.value = ''; attachments.value = []; editTarget.value = null; scrolledUp.value = false; if (conversationId.value !== id) await router.replace(`/workbench/chat/${id}`); await nextTick(); scrollBottom(true) } } catch { if (store.activeConversationId && conversationId.value !== store.activeConversationId) await router.replace(`/workbench/chat/${store.activeConversationId}`) } }
function send() { if (new TextEncoder().encode(draft.value).byteLength > 32768) { uploadError.value = '消息过长，请精简后发送（最多 32 KB）'; return } if ((editTarget.value || attachments.value.length) && !officeEnabled.value) return; if (!draft.value.trim() || !model.value || uploading.value) return; void submit({ model: model.value, content: draft.value, task_mode: taskMode.value || !!editTarget.value || attachments.value.length > 0, ...(editTarget.value ? { target_resource_version_id: editTarget.value.version_id } : {}), web_search: webSearch.value, ...(effort.value ? { reasoning_effort: effort.value } : {}), ...(attachments.value.length ? { resource_version_ids: attachments.value.map(f => f.version_id) } : {}) }) }
async function regenerate(run: ChatRun, message: ChatMessage) { const oldFiles = chatArtifacts(message.content); await submit({ model: model.value || run.model, content: chatText(message.content), task_mode: run.task_mode || run.mode === 'office', web_search: run.search_enabled || false, ...(['completed', 'partially_completed'].includes(run.status) ? { regenerate_run_id: run.regeneration_of || run.id } : {}), ...(effort.value ? { reasoning_effort: effort.value } : {}), ...(oldFiles.length ? { resource_version_ids: oldFiles.map(f => f.version_id) } : {}) }) }
async function uploadFiles(files: File[]) { if (uploading.value || !store.status?.uploads_enabled) return; if (!officeEnabled.value) { uploadError.value = '请选择支持任务模式的模型后再添加文件'; return } taskMode.value = true; const maxFiles = store.status.max_files || 10, maxBytes = store.status.max_file_bytes || 24 * 1024 * 1024; uploadError.value = ''; if (files.length + attachments.value.length > maxFiles) { uploadError.value = `每次最多添加 ${maxFiles} 个文件`; return } const oversized = files.find(f => f.size > maxBytes); if (oversized) { uploadError.value = `「${oversized.name}」超过 ${Math.floor(maxBytes / 1024 / 1024)} MB 限制`; return } uploading.value = true; const id = store.activeConversationId, owner = auth.user?.id, own = pageEpoch; try { for (const file of files) { if (owner !== auth.user?.id || own !== pageEpoch || id !== store.activeConversationId) return; const resource = await workbenchChatApi.upload(file); if (owner === auth.user?.id && own === pageEpoch && id === store.activeConversationId) attachments.value.push(resource) } } catch (e) { if (owner === auth.user?.id && own === pageEpoch) uploadError.value = chatError(e) } finally { if (own === pageEpoch) uploading.value = false } }
function removeAttachment(id: string) { attachments.value = attachments.value.filter(f => f.version_id !== id) }
function openFile(file: ChatArtifact) { previewFile.value = file; panelOpen.value = true }
function editFile(file: ChatArtifact) { editTarget.value = { version_id: file.version_id, name: file.name }; if (officeEnabled.value) taskMode.value = true; if (mobile.value || fullscreen.value) closePanel(); void nextTick(() => composer.value?.focus()) }
function closePanel() { panelOpen.value = false; fullscreen.value = false; previewFile.value = null }
async function download(file: ChatArtifact) { actionError.value = ''; try { let access = await workbenchChatApi.access(file.version_id); const request = (url: string) => { const value = new URL(url, window.location.origin); if (!['http:', 'https:'].includes(value.protocol)) throw new Error('文件链接无效'); return fetch(value.href, { referrerPolicy: 'no-referrer' }) }; let response = await request(access.url); if (response.status === 403) { access = await workbenchChatApi.access(file.version_id); response = await request(access.url) } if (!response.ok) throw new Error('文件下载失败，请重试'); saveAs(await response.blob(), file.name) } catch (e) { actionError.value = chatError(e) } }
async function copy(id: string, value: string) { try { await navigator.clipboard.writeText(value); copied.value = id } catch { actionError.value = '复制失败，请手动选择内容复制' } }
async function refreshCurrent() { const id = store.activeConversationId; store.error = ''; await store.selectConversation(''); await store.selectConversation(id) }
async function loadEarlier() { const el = scroll.value; const height = el?.scrollHeight || 0; const top = el?.scrollTop || 0; scrolledUp.value = true; await store.loadEarlier(); await nextTick(); if (el) el.scrollTop = top + el.scrollHeight - height }
function onScroll() { const el = scroll.value; if (el) scrolledUp.value = el.scrollHeight - el.scrollTop - el.clientHeight > 100 }
function scrollBottom(force = false) { const el = scroll.value; if (el && (force || !scrolledUp.value)) { el.scrollTop = el.scrollHeight; scrolledUp.value = false } }
function isWorking(run: ChatRun) { return ['queued', 'running', 'recovering', 'needs_confirmation'].includes(run.status) }
function formatTime(value: string) { return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) }
function safeLink(value?: string) { try { const url = new URL(value || ''); return ['https:', 'http:'].includes(url.protocol) ? url.href : undefined } catch { return undefined } }
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
onBeforeUnmount(() => { ++pageEpoch; previousFocus?.focus(); store.stopWatching(); window.removeEventListener('keydown', escapePreview) })
</script>
