import { defineStore } from 'pinia'
import { computed, ref, watch } from 'vue'
import { useAuthStore } from './auth'
import { readChatEvents, workbenchChatApi } from '@/api/workbenchChat'
import { CHAT_ACTIVE_STATUSES } from '@/types/workbenchChat'
import type { ChatArtifact, ChatConversation, ChatEvent, ChatHistory, ChatModels, ChatRun, ChatRunStatus, ChatStatus, ChatSubmit } from '@/types/workbenchChat'

const RUN_STATUSES = new Set<ChatRunStatus>(['queued', 'running', 'waiting_user', 'recovering', 'completed', 'partially_completed', 'failed', 'cancelled', 'needs_confirmation'])

function waitForRetry(ms: number, signal: AbortSignal) {
  return new Promise<void>(resolve => {
    if (signal.aborted) { resolve(); return }
    const timer = setTimeout(done, ms)
    signal.addEventListener('abort', done, { once: true })
    function done() {
      clearTimeout(timer)
      signal.removeEventListener('abort', done)
      resolve()
    }
  })
}
function canRetryChatRequest(error: unknown): boolean {
  const status = (error as { status?: number } | null)?.status
  return !status || status >= 500 || status === 408 || status === 409 || status === 429
}

export function chatError(error: unknown): string {
  if (error && typeof error === 'object' && 'error' in error) {
    const detail = error.error
    if (detail && typeof detail === 'object' && 'message' in detail && typeof detail.message === 'string' && detail.message) return detail.message
  }
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
  const streamError = ref('')
  const advanceError = ref('')
  const connectionError = computed(() => advanceError.value || streamError.value)
  const nextCursor = ref<string | undefined>()
  const initialized = ref(false)
  let selectionEpoch = 0
  let authEpoch = 0
  const auth = useAuthStore()
  let controller: AbortController | undefined
  const historyControllers = new Set<AbortController>()
  let pending: { conversationId: string; body: ChatSubmit } | undefined
  const activeRun = computed(() => [...history.value.runs].reverse().find(r => CHAT_ACTIVE_STATUSES.has(r.status)))
  const currentConversation = computed(() => conversations.value.find(c => c.id === activeConversationId.value))
  const models = computed(() => modelCatalog.value.models)

  function stopWatching() {
    controller?.abort(); controller = undefined
    for (const own of historyControllers) own.abort()
    historyControllers.clear()
    streamError.value = ''
    advanceError.value = ''
  }
  function pause() {
    stopWatching()
    ++selectionEpoch
    loading.value = false
    loadingEarlier.value = false
    submitting.value = false
  }
  function updateRun(run: ChatRun) {
    const i = history.value.runs.findIndex(r => r.id === run.id)
    if (i >= 0) {
      // Single-run responses omit resource projections; a history response can
      // explicitly clear removed files with artifacts: [].
      const previous = history.value.runs[i]!
      if (!CHAT_ACTIVE_STATUSES.has(previous.status) && CHAT_ACTIVE_STATUSES.has(run.status)) return
      history.value.runs.splice(i, 1, run.artifacts === undefined && previous.artifacts !== undefined ? { ...run, artifacts: previous.artifacts } : run)
    }
    else history.value.runs.push(run)
  }
  function acceptEvent(event: ChatEvent) {
    const list = events.value[event.run_id] ||= []
    if (event.sequence <= (list.at(-1)?.sequence || 0)) return
    const last = list.at(-1)
    if ((event.type === 'assistant.delta' || event.type === 'reasoning.delta') && last?.type === event.type && last.payload.operation_id === event.payload.operation_id && typeof event.payload.text === 'string' && typeof last.payload.text === 'string') {
      last.payload.text += event.payload.text; last.sequence = event.sequence; last.id = event.id
    } else { list.push(event); list.sort((a, b) => a.sequence - b.sequence) }
    if (list.length > 2000) {
      const disposable = list.findIndex(e => !['assistant.delta', 'assistant.partial', 'reasoning.delta', 'clarification.requested', 'artifact.published', 'task.updated'].includes(e.type))
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
    // A delayed live event can arrive after a terminal snapshot from advance().
    if (!CHAT_ACTIVE_STATUSES.has(run.status)) return
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
    for (const message of value.messages || []) messages.set(message.id, message)
    for (const run of value.runs || []) updateRun(run)
    history.value.messages = [...messages.values()].sort((a,b) => a.sequence-b.sequence)
    history.value.runs.sort((a,b) => a.created_at.localeCompare(b.created_at))
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
  function watchRun(run: ChatRun, id: string, epoch: number) {
    if (epoch !== selectionEpoch || id !== activeConversationId.value) return
    controller?.abort()
    const own = new AbortController()
    controller = own
    void streamRun(run.id, id, epoch, own.signal)
    if (run.browser_driven) void driveRun(run.id, id, epoch, own.signal)
  }
  async function streamRun(runId: string, id: string, epoch: number, signal: AbortSignal) {
    const watching = () => !signal.aborted && epoch === selectionEpoch
    while (watching()) {
      try {
        const last = events.value[runId]?.at(-1)?.sequence || 0
        await readChatEvents(runId, last, signal, event => {
          if (watching() && event.run_id === runId) { streamError.value = ''; acceptEvent(event) }
        })
      } catch (e) {
        if (!watching()) return
        streamError.value = chatError(e)
        if (!canRetryChatRequest(e)) return
      }
      if (!watching()) return
      try {
        // Reconcile durable messages if the stream closed before a terminal event arrived.
        const before = history.value.runs.find(item => item.id === runId)
        const beforeStatus = before?.status
        const received = await workbenchChatApi.run(runId)
        if (!watching()) return
        const current = history.value.runs.find(item => item.id === runId)
        const changed = current && (current !== before || current.status !== beforeStatus)
        const run = changed && !(Date.parse(received.updated_at) > Date.parse(current.updated_at)) ? current : received
        if (run !== current) updateRun(run)
        if (!CHAT_ACTIVE_STATUSES.has(run.status) || run.status === 'needs_confirmation') {
          await refreshHistory(id, epoch)
          if (watching()) streamError.value = ''
          return
        }
      } catch (e) {
        if (!watching()) return
        streamError.value = chatError(e)
        if (!canRetryChatRequest(e)) return
      }
      await waitForRetry(activeRun.value?.status === 'waiting_user' ? 8000 : 1800, signal)
    }
  }
  async function driveRun(runId: string, id: string, epoch: number, signal: AbortSignal) {
    const watching = () => !signal.aborted && epoch === selectionEpoch
    let delay = 800
    while (watching()) {
      const run = activeRun.value
      if (run?.id !== runId || run.status === 'needs_confirmation' || run.status === 'waiting_user') return
      try {
        const updated = await workbenchChatApi.advance(runId, signal)
        if (!watching()) return
        updateRun(updated)
        advanceError.value = ''
        delay = 800
        if (!CHAT_ACTIVE_STATUSES.has(updated.status) || updated.status === 'needs_confirmation') {
          try { await refreshHistory(id, epoch) } catch { /* The event stream also reconciles terminal messages. */ }
          return
        }
      } catch (e) {
        if (!watching()) return
        const http = e as { status?: number; retry_after_seconds?: number } | null
        const busy = http?.status === 409 || http?.status === 429
        if (!busy) advanceError.value = chatError(e)
        if (!canRetryChatRequest(e)) return
        await waitForRetry(http?.retry_after_seconds ? http.retry_after_seconds * 1000 : delay, signal)
        delay = Math.min(delay * 2, 12000)
      }
    }
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
    const owner = authEpoch
    const wasEnabled = status.value?.enabled
    const flags = await workbenchChatApi.status()
    if (owner !== authEpoch) return
    status.value = flags
    if (initialized.value && wasEnabled === flags.enabled) return
    if (flags.enabled) {
      const catalog = await workbenchChatApi.models()
      if (owner !== authEpoch) return
      modelCatalog.value = catalog
      await loadConversations()
    }
    if (owner === authEpoch) initialized.value = true
  }
  async function selectConversation(id: string, refresh = false) {
    if (activeConversationId.value === id && !refresh && !error.value) return
    const changed = activeConversationId.value !== id
    stopWatching(); const epoch = ++selectionEpoch
    loadingEarlier.value = false
    error.value = ''
    if (changed) {
      nextBefore.value = undefined
      hasMoreHistory.value = false
      activeConversationId.value = id
      history.value = { messages: [], runs: [] }
      pending = undefined
      submitting.value = false
    }
    if (!id) { loading.value = false; return }
    loading.value = true
    try {
      await refreshHistory(id, epoch)
      if (epoch !== selectionEpoch) return
      const run = activeRun.value
      if (run) watchRun(run, id, epoch)
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
      try { await refreshHistory(id, epoch) }
      catch (e) { if (epoch === selectionEpoch) error.value = chatError(e) }
      if (epoch !== selectionEpoch) return
      watchRun(run, id, epoch)
      void loadConversations().catch(() => {})
      return id
    } catch (e) { if (owner === authEpoch && selected === selectionEpoch) error.value = chatError(e); throw e }
    finally { if (owner === authEpoch && selected === selectionEpoch) submitting.value = false }
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
    stopWatching()
    try {
      const result = await workbenchChatApi.cancel(run.id)
      if (epoch !== selectionEpoch) return
      updateRun(result)
      await refreshHistory(id, epoch)
      void loadRunEvents(run.id).catch(() => {})
    } catch (e) {
      if (epoch !== selectionEpoch) return
      error.value = chatError(e)
    }
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
  return { status, modelCatalog, models, conversations, activeConversationId, currentConversation, history, events, activeRun, loading, loadingEarlier, hasMoreHistory, loadEarlier, loadRunEvents, loadingConversations, submitting, error, connectionError, nextCursor, initialized, initialize, loadConversations, loadMore: () => loadConversations(true), selectConversation, createConversation, renameConversation, deleteConversation, newConversation, send, selectAnswer, cancelRun, pause, reset }
})
