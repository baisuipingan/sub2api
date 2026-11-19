import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { useAuthStore } from './auth'
import { readChatEvents, workbenchChatApi } from '@/api/workbenchChat'
import { CHAT_ACTIVE_STATUSES } from '@/types/workbenchChat'
import type { ChatArtifact, ChatConversation, ChatEvent, ChatHistory, ChatModels, ChatRun, ChatRunStatus, ChatStatus, ChatSubmit } from '@/types/workbenchChat'

const RUN_STATUSES = new Set<ChatRunStatus>(['queued', 'running', 'waiting_user', 'recovering', 'completed', 'partially_completed', 'failed', 'cancelled', 'needs_confirmation'])

export function chatError(error: unknown): string {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') return error.message
  return '请求失败，请稍后重试'
}
export const useWorkbenchChatStore = defineStore('workbenchChat', () => {
  const status = ref<ChatStatus | null>(null)
  const modelCatalog = ref<ChatModels>({ models: [] })
  const conversations = ref<ChatConversation[]>([])
  const activeConversationId = ref('')
  const history = ref<ChatHistory>({ messages: [], runs: [] })
  const events = ref<Record<string, ChatEvent[]>>({})
  const loading = ref(false)
  const loadingEarlier = ref(false)
  const nextBefore = ref<number | undefined>()
  const hasMoreHistory = ref(false)
  const loadingConversations = ref(false)
  const submitting = ref(false)
  const error = ref('')
  const connectionError = ref('')
  const nextCursor = ref<string | undefined>()
  const initialized = ref(false)
  let selectionEpoch = 0
  let authEpoch = 0
  const auth = useAuthStore()
  let controller: AbortController | undefined
  const historyControllers = new Set<AbortController>()
  let retryTimer: ReturnType<typeof setTimeout> | undefined
  let pending: { conversationId: string; body: ChatSubmit } | undefined
  const activeRun = computed(() => [...history.value.runs].reverse().find(r => CHAT_ACTIVE_STATUSES.has(r.status)))
  const currentConversation = computed(() => conversations.value.find(c => c.id === activeConversationId.value))
  const models = computed(() => modelCatalog.value.models)

  function stopWatching() {
    controller?.abort(); controller = undefined
    for (const own of historyControllers) own.abort()
    historyControllers.clear()
    if (retryTimer) clearTimeout(retryTimer)
    retryTimer = undefined
    connectionError.value = ''
  }
  function updateRun(run: ChatRun) {
    const i = history.value.runs.findIndex(r => r.id === run.id)
    if (i >= 0) {
      // Single-run responses omit resource projections; a history response can
      // explicitly clear removed files with artifacts: [].
      const previous = history.value.runs[i]!
      history.value.runs.splice(i, 1, run.artifacts === undefined && previous.artifacts !== undefined ? { ...run, artifacts: previous.artifacts } : run)
    }
    else history.value.runs.push(run)
  }
  function acceptEvent(event: ChatEvent) {
    const list = events.value[event.run_id] ||= []
    if (event.sequence <= (list.at(-1)?.sequence || 0)) return
    const last = list.at(-1)
    if (event.type === 'assistant.delta' && last?.type === event.type && last.payload.operation_id === event.payload.operation_id && typeof event.payload.text === 'string' && typeof last.payload.text === 'string' && event.sequence > last.sequence) {
      last.payload.text += event.payload.text; last.sequence = event.sequence; last.id = event.id
    } else { list.push(event); list.sort((a, b) => a.sequence - b.sequence) }
    if (list.length > 2000) {
      const disposable = list.findIndex(e => e.type !== 'assistant.delta' && e.type !== 'clarification.requested' && e.type !== 'artifact.published' && e.type !== 'task.updated')
      if (disposable >= 0) list.splice(disposable, 1)
      else list.splice(0, 1)
    }
    const runIds = Object.keys(events.value)
    if (runIds.length > 24) for (const id of runIds.slice(0, runIds.length - 24)) if (id !== activeRun.value?.id && !history.value.runs.slice(-10).some(r => r.id === id)) delete events.value[id]
    const run = history.value.runs.find(r => r.id === event.run_id)
    if (!run) return
    if (event.type === 'artifact.published' && CHAT_ACTIVE_STATUSES.has(run.status)) {
      const artifact = event.payload.artifact as ChatArtifact | undefined
      if (artifact && typeof artifact.version_id === 'string' && typeof artifact.name === 'string') {
        const files = new Map((run.artifacts || []).map(file => [file.version_id, file]))
        files.set(artifact.version_id, artifact); run.artifacts = [...files.values()]
      }
    }
    if (event.type === 'clarification.requested') {
      run.status = 'waiting_user'
    } else if (event.type.startsWith('run.')) {
      if (event.payload.run && typeof event.payload.run === 'object') {
        const updated = event.payload.run as ChatRun
        if (updated.id === run.id && RUN_STATUSES.has(updated.status)) updateRun({ ...run, ...updated })
      } else {
        const status = typeof event.payload.status === 'string' ? event.payload.status : event.type.slice(4)
        if (RUN_STATUSES.has(status as ChatRunStatus)) run.status = status as ChatRunStatus
        const code = event.payload.error_code ?? event.payload.code
        const message = event.payload.error_message ?? event.payload.message
        if (typeof code === 'string') run.error_code = code
        if (typeof message === 'string') run.error_message = message
      }
    }
  }
  function mergeHistory(value: ChatHistory) {
    const messages = new Map(history.value.messages.map(m => [m.id, m]))
    const runs = new Map(history.value.runs.map(r => [r.id, r]))
    for (const message of value.messages || []) messages.set(message.id, message)
    for (const run of value.runs || []) runs.set(run.id, run)
    history.value = { messages: [...messages.values()].sort((a,b) => a.sequence-b.sequence), runs: [...runs.values()].sort((a,b) => a.created_at.localeCompare(b.created_at)) }
  }
  async function refreshHistory(id: string, epoch: number) {
    const value = await workbenchChatApi.history(id)
    if (epoch === selectionEpoch && id === activeConversationId.value) {
      const initial = !history.value.messages.length
      mergeHistory(value)
      if (initial) { nextBefore.value = value.next_before; hasMoreHistory.value = !!value.has_more }
    }
  }
  async function loadEarlier() {
    if (!hasMoreHistory.value || loadingEarlier.value || !nextBefore.value) return
    loadingEarlier.value = true
    const epoch = selectionEpoch, id = activeConversationId.value
    try {
      const value = await workbenchChatApi.history(id, nextBefore.value)
      if (epoch !== selectionEpoch) return
      mergeHistory(value); nextBefore.value = value.next_before; hasMoreHistory.value = !!value.has_more
    } catch (e) { if (epoch === selectionEpoch) error.value = chatError(e) }
    finally { if (epoch === selectionEpoch) loadingEarlier.value = false }
  }
  async function watchRun(runId: string, id: string, epoch: number) {
    if (epoch !== selectionEpoch || id !== activeConversationId.value) return
    controller?.abort()
    const own = new AbortController(); controller = own
    const last = events.value[runId]?.at(-1)?.sequence || 0
    try {
      await readChatEvents(runId, last, own.signal, event => {
        if (epoch === selectionEpoch && event.run_id === runId) { connectionError.value = ''; acceptEvent(event) }
      })
    } catch (e) {
      if (own.signal.aborted || epoch !== selectionEpoch) return
      connectionError.value = chatError(e)
    }
    if (own.signal.aborted || epoch !== selectionEpoch) return
    // Always reconcile persistent messages after a stream closes. Never replay a model submission.
    try {
      const run = await workbenchChatApi.run(runId)
      if (epoch !== selectionEpoch) return
      updateRun(run)
      if (!CHAT_ACTIVE_STATUSES.has(run.status) || run.status === 'needs_confirmation') { await refreshHistory(id, epoch); connectionError.value = ''; return }
    } catch (e) { if (epoch === selectionEpoch) connectionError.value = chatError(e) }
    if (epoch === selectionEpoch) retryTimer = setTimeout(() => { void watchRun(runId, id, epoch) }, activeRun.value?.status === 'waiting_user' ? 8000 : 1800)
  }
  async function loadConversations(append = false) {
    if (loadingConversations.value || (append && !nextCursor.value)) return
    loadingConversations.value = true
    const owner = authEpoch
    try {
      const page = await workbenchChatApi.conversations(append ? nextCursor.value : undefined)
      if (owner !== authEpoch) return
      const items = page.items || []
      conversations.value = append ? [...conversations.value, ...items.filter(c => !conversations.value.some(old => old.id === c.id))] : items
      nextCursor.value = page.next_cursor
    } finally { if (owner === authEpoch) loadingConversations.value = false }
  }
  async function initialize() {
    if (initialized.value) return
    const owner = authEpoch
    const flags = await workbenchChatApi.status()
    if (owner !== authEpoch) return
    status.value = flags
    if (flags.enabled) {
      const catalog = await workbenchChatApi.models()
      if (owner !== authEpoch) return
      modelCatalog.value = catalog
      await loadConversations()
    }
    if (owner === authEpoch) initialized.value = true
  }
  async function selectConversation(id: string) {
    if (activeConversationId.value === id && !error.value) return
    stopWatching(); const epoch = ++selectionEpoch
    nextBefore.value = undefined; hasMoreHistory.value = false; loadingEarlier.value = false
    activeConversationId.value = id; history.value = { messages: [], runs: [] }; error.value = ''; pending = undefined
    if (!id) return
    loading.value = true
    try {
      await refreshHistory(id, epoch)
      if (epoch !== selectionEpoch) return
      const run = activeRun.value
      if (run) void watchRun(run.id, id, epoch)
      // Load the newest task's process; older turns can be expanded on demand.
      const terminal = [...history.value.runs].reverse().find(r => !CHAT_ACTIVE_STATUSES.has(r.status))
      if (terminal) void loadRunEvents(terminal.id).catch(() => {})

    } catch (e) { if (epoch === selectionEpoch) error.value = chatError(e) }
    finally { if (epoch === selectionEpoch) loading.value = false }
  }
  async function loadRunEvents(runId: string) {
    const after = events.value[runId]?.at(-1)?.sequence || 0
    const epoch = selectionEpoch
    const own = new AbortController(); historyControllers.add(own)
    const timeout = setTimeout(() => own.abort(), 20000)
    try { await readChatEvents(runId, after, own.signal, event => { if (epoch === selectionEpoch) acceptEvent(event) }) }
    finally { clearTimeout(timeout); historyControllers.delete(own) }
  }
  async function createConversation(title = '新对话') {
    const owner = authEpoch
    const conversation = await workbenchChatApi.createConversation(crypto.randomUUID(), title)
    if (owner !== authEpoch) throw new Error('登录账号已变更，请重新打开工作台')
    conversations.value.unshift(conversation)
    return conversation
  }
  async function send(input: Omit<ChatSubmit, 'run_id'>) {
    if (submitting.value || activeRun.value) return
    submitting.value = true; error.value = ''
    const owner = authEpoch, selected = selectionEpoch
    try {
      let id = activeConversationId.value
      if (!id) {
        const conversation = await createConversation(input.content.trim().slice(0, 60))
        if (owner !== authEpoch || selected !== selectionEpoch) return
        id = conversation.id; activeConversationId.value = id
      }
      // A failed transport may have reached the server. Reuse the same invocation identity for an unchanged retry.
      const same = pending?.conversationId === id && JSON.stringify({ ...pending.body, run_id: undefined }) === JSON.stringify({ ...input, run_id: undefined })
      const body: ChatSubmit = same ? pending!.body : { ...input, run_id: crypto.randomUUID() }
      pending = { conversationId: id, body }
      const run = await workbenchChatApi.submit(id, body)
      if (owner !== authEpoch || selected !== selectionEpoch) return
      pending = undefined
      updateRun(run)
      const epoch = selectionEpoch
      await refreshHistory(id, epoch)
      void watchRun(run.id, id, epoch)
      void loadConversations().catch(() => {})
      return id
    } catch (e) { if (owner === authEpoch && selected === selectionEpoch) error.value = chatError(e); throw e }
    finally { if (owner === authEpoch) submitting.value = false }
  }
  async function selectAnswer(runId: string) {
    const epoch = selectionEpoch
    const updated = await workbenchChatApi.selectAnswer(runId)
    if (epoch !== selectionEpoch) return
    const root = updated.regeneration_of || updated.id
    for (const run of history.value.runs) if (run.id === root || run.regeneration_of === root) run.selected = run.id === updated.id
    updateRun(updated)
  }
  async function cancelRun() {
    const run = activeRun.value
    if (!run) return
    const epoch = selectionEpoch, id = activeConversationId.value
    try { const result = await workbenchChatApi.cancel(run.id); if (epoch !== selectionEpoch) return; updateRun(result); await refreshHistory(id, epoch) }
    catch (e) { if (epoch === selectionEpoch) error.value = chatError(e) }
  }
  async function renameConversation(id: string, title: string) {
    const name = title.trim()
    if (!name) return
    const owner = authEpoch
    const updated = await workbenchChatApi.renameConversation(id, name)
    if (owner !== authEpoch) return
    const i = conversations.value.findIndex(c => c.id === id)
    if (i >= 0) conversations.value.splice(i, 1, updated)
  }
  async function deleteConversation(id: string) {
    const owner = authEpoch
    await workbenchChatApi.deleteConversation(id)
    if (owner !== authEpoch) return
    conversations.value = conversations.value.filter(c => c.id !== id)
    for (const run of history.value.runs) if (run.conversation_id === id) delete events.value[run.id]
    if (activeConversationId.value === id) await selectConversation('')
  }
  function newConversation() { return selectConversation('') }
  function reset() { stopWatching(); ++selectionEpoch; ++authEpoch; loading.value = false; submitting.value = false; loadingConversations.value = false; loadingEarlier.value = false; nextBefore.value = undefined; hasMoreHistory.value = false; error.value = ''; nextCursor.value = undefined; initialized.value = false; conversations.value = []; history.value = { messages: [], runs: [] }; events.value = {}; activeConversationId.value = ''; status.value = null; modelCatalog.value = { models: [] }; pending = undefined }
  watch(() => auth.user?.id, () => reset(), { flush: 'sync' })
  return { status, modelCatalog, models, conversations, activeConversationId, currentConversation, history, events, activeRun, loading, loadingEarlier, hasMoreHistory, loadEarlier, loadRunEvents, loadingConversations, submitting, error, connectionError, nextCursor, initialized, initialize, loadConversations, loadMore: () => loadConversations(true), selectConversation, createConversation, renameConversation, deleteConversation, newConversation, send, selectAnswer, cancelRun, stopWatching, reset }
})
