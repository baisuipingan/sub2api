import { createPinia, defineStore, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
const api = vi.hoisted(() => ({ status: vi.fn(), models: vi.fn(), conversations: vi.fn(), history: vi.fn(), createConversation: vi.fn(), submit: vi.fn(), run: vi.fn(), advance: vi.fn(), cancel: vi.fn(), renameConversation: vi.fn(), deleteConversation: vi.fn(), selectAnswer: vi.fn() }))
const readEvents = vi.hoisted(() => vi.fn(async (_id: string, _after: number, _signal: AbortSignal, _accept: (event: unknown) => void) => {}))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: api, readChatEvents: readEvents }))
vi.mock('@/stores/auth', () => ({ useAuthStore: defineStore('test-workbench-auth', () => ({ user: ref<{ id: number } | null>({ id: 1 }) })) }))
import { useAuthStore } from '@/stores/auth'
import { chatError, useWorkbenchChatStore } from '@/stores/workbenchChat'
const message = (id: string, sequence: number) => ({ id, sequence, conversation_id: 'conversation', run_id: 'run', role: 'user', content: id, created_at: '' })
describe('workbench account isolation and durable submissions', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks(); readEvents.mockResolvedValue(undefined) })
  afterEach(() => { useWorkbenchChatStore().pause(); vi.useRealTimers() })
  it('does not restore private history when an older request finishes after logout', async () => {
    let resolve!: (value: unknown) => void
    api.history.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const store = useWorkbenchChatStore()
    const pending = store.selectConversation('conversation')
    useAuthStore().user = null
    resolve({ messages: [message('private', 1)], runs: [] })
    await pending
    expect(store.history.messages).toEqual([])
    expect(store.activeConversationId).toBe('')
    expect(store.events).toEqual({})
  })
  it('does not submit or activate a new conversation if navigation changes while creating it', async () => {
    let resolve!: (value: unknown) => void
    api.createConversation.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    api.history.mockResolvedValue({ messages: [], runs: [] })
    const store = useWorkbenchChatStore()
    const pending = store.send({ model: 'gpt', content: '问题', task_mode: false, web_search: false })
    await store.selectConversation('other')
    resolve({ id: 'created', title: '问题' })
    await pending
    expect(api.submit).not.toHaveBeenCalled()
    expect(store.activeConversationId).toBe('other')
  })
  it('does not restore a submitted run after logging out while its response is in flight', async () => {
    let resolve!: (value: unknown) => void
    api.history.mockResolvedValue({ messages: [], runs: [] })
    api.submit.mockImplementationOnce(() => new Promise(r => { resolve = r }))
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    const pending = store.send({ model: 'gpt', content: '问题', task_mode: false, web_search: false })
    useAuthStore().user = null
    resolve({ id: 'run', conversation_id: 'conversation', status: 'queued', created_at: '' })
    await pending
    expect(store.history.runs).toEqual([])
    expect(store.activeConversationId).toBe('')
    expect(store.submitting).toBe(false)
  })
  it('does not load model access or conversations when chat is disabled', async () => {
    api.status.mockResolvedValueOnce({ enabled: false })
    const store = useWorkbenchChatStore()
    await store.initialize()
    expect(api.models).not.toHaveBeenCalled()
    expect(api.conversations).not.toHaveBeenCalled()
    expect(store.models).toEqual([])
  })
  it('reads current provider readiness when the user returns to chat', async () => {
    api.status.mockResolvedValueOnce({ enabled: true, search_allowed: true, search_enabled: false })
    api.status.mockResolvedValueOnce({ enabled: true, search_allowed: true, search_enabled: true })
    api.models.mockResolvedValueOnce({ models: [{ id: 'gpt', supports_tools: true }] })
    api.conversations.mockResolvedValueOnce({ items: [] })
    const store = useWorkbenchChatStore()
    await store.initialize()
    expect(store.status?.search_enabled).toBe(false)
    await store.initialize()
    expect(store.status?.search_enabled).toBe(true)
    expect(api.models).toHaveBeenCalledTimes(1)
  })
  it('shows the structured workbench error returned after a provider becomes unavailable', () => {
    expect(chatError({ status: 409, message: 'Request failed with status code 409', error: { code: 'WORKBENCH_SEARCH_UNAVAILABLE', message: '联网搜索暂不可用' } })).toBe('联网搜索暂不可用')
  })
  it('loads earlier messages only when requested and merges in sequence order', async () => {
    api.history.mockResolvedValueOnce({ messages: [message('new', 50)], runs: [], has_more: true, next_before: 50 })
    api.history.mockResolvedValueOnce({ messages: [message('old', 2)], runs: [], has_more: false })
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    expect(api.history).toHaveBeenCalledTimes(1)
    expect(store.history.messages.map(m => m.id)).toEqual(['new'])
    await store.loadEarlier()
    expect(api.history).toHaveBeenLastCalledWith('conversation', 50)
    expect(store.history.messages.map(m => m.id)).toEqual(['old', 'new'])
    expect(store.hasMoreHistory).toBe(false)
  })
  it('reuses the run identity after an uncertain submission instead of charging a second invocation', async () => {
    api.history.mockResolvedValue({ messages: [], runs: [] })
    api.submit.mockRejectedValue(new Error('network interrupted'))
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    const input = { model: 'gpt', content: '问题', task_mode: false, web_search: false }
    await expect(store.send(input)).rejects.toThrow('network interrupted')
    await expect(store.send(input)).rejects.toThrow('network interrupted')
    const first = api.submit.mock.calls[0]![1]
    const second = api.submit.mock.calls[1]![1]
    expect(first.run_id).toBe(second.run_id)
    expect(first.run_id).toMatch(/^[a-f0-9-]{36}$/)
  })
  it('uses durable run event names without treating tool completion as a terminal run', async () => {
    const store = useWorkbenchChatStore()
    store.history = { messages: [], runs: [{ id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'queued', created_at: '', updated_at: '' }] }
    readEvents.mockImplementationOnce(async (_id, _after, _signal, accept) => {
      accept({ id: 'one', run_id: 'run', sequence: 1, type: 'run.running', payload: { epoch: 1 } })
      accept({ id: 'two', run_id: 'run', sequence: 2, type: 'tool.completed', payload: { operation_id: 'tool', status: 'completed' } })
    })
    await store.loadRunEvents('run')
    expect(store.activeRun?.status).toBe('running')
    readEvents.mockImplementationOnce(async (_id, _after, _signal, accept) => {
      accept({ id: 'three', run_id: 'run', sequence: 3, type: 'clarification.requested', payload: { request_id: 'input', questions: [] } })
    })
    await store.loadRunEvents('run')
    expect(store.activeRun?.status).toBe('waiting_user')
    readEvents.mockImplementationOnce(async (_id, _after, _signal, accept) => {
      accept({ id: 'four', run_id: 'run', sequence: 4, type: 'run.failed', payload: { code: 'RUN_TIME_LIMIT', message: '任务已超时' } })
    })
    await store.loadRunEvents('run')
    expect(store.activeRun).toBeUndefined()
    expect(store.history.runs[0]).toMatchObject({ status: 'failed', error_code: 'RUN_TIME_LIMIT', error_message: '任务已超时' })
  })
  it('retains a historical file version when selecting an answer omits the resource projection, but honors an explicit clear', async () => {
    const store = useWorkbenchChatStore()
    const run = { id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'completed' as const, created_at: '', updated_at: '' }
    const artifact = { id: 'document', version_id: 'original-version', name: 'weekly.docx', mime: 'application/octet-stream', bytes: 100 }
    store.history = { messages: [], runs: [{ ...run, artifacts: [artifact] }] }
    api.selectAnswer.mockResolvedValueOnce({ ...run, selected: true })
    await store.selectAnswer('run')
    expect(store.history.runs[0]?.artifacts).toEqual([artifact])
    api.selectAnswer.mockResolvedValueOnce({ ...run, selected: true, artifacts: [] })
    await store.selectAnswer('run')
    expect(store.history.runs[0]?.artifacts).toEqual([])
  })
  it('keeps a newly published file when a live run completes before its next history refresh', async () => {
    const store = useWorkbenchChatStore()
    store.history = { messages: [], runs: [{ id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'running', artifacts: [], created_at: '', updated_at: '' }] }
    const artifact = { id: 'a', version_id: 'version', name: 'weekly.docx', mime: '', bytes: 100 }
    readEvents.mockImplementationOnce(async (_id, _after, _signal, accept) => {
      accept({ id: 'one', run_id: 'run', sequence: 1, type: 'artifact.published', payload: { artifact } })
      accept({ id: 'two', run_id: 'run', sequence: 2, type: 'run.completed', payload: {} })
    })
    await store.loadRunEvents('run')
    expect(store.history.runs[0]).toMatchObject({ status: 'completed', artifacts: [artifact] })
  })
  it('retrying an uncertain browser-driven step uses the same run and follows persisted state', async () => {
    vi.useFakeTimers()
    const queued = { id: 'run', conversation_id: 'conversation', model: 'gemini', status: 'queued', browser_driven: true, created_at: '', updated_at: '' }
    const completed = { ...queued, status: 'completed' }
    api.history.mockResolvedValue({ messages: [], runs: [queued] })
    api.run.mockResolvedValue(queued)
    api.advance.mockRejectedValueOnce({ status: 0, message: '连接中断' })
      .mockRejectedValueOnce({ status: 409 })
      .mockImplementationOnce(() => {
        api.history.mockResolvedValue({ messages: [message('answer', 2)], runs: [completed] })
        return completed
      })
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    expect(api.advance).toHaveBeenCalledTimes(1)
    expect(store.connectionError).toBe('连接中断')
    await vi.advanceTimersByTimeAsync(800)
    expect(api.advance).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1600)
    expect(api.advance.mock.calls.map(([id]) => id)).toEqual(['run', 'run', 'run'])
    expect(api.submit).not.toHaveBeenCalled()
    expect(store.activeRun).toBeUndefined()
    expect(store.history.messages.map(item => item.id)).toEqual(['answer'])
    expect(store.connectionError).toBe('')
  })
  it('only advances while the conversation is open and resumes from its persisted run on reopening', async () => {
    const run = { id: 'run', conversation_id: 'conversation', model: 'gemini', status: 'running', browser_driven: true, created_at: '', updated_at: '' }
    let finishFirst!: (value: unknown) => void
    api.advance.mockImplementationOnce(() => new Promise(resolve => { finishFirst = resolve }))
    api.advance.mockResolvedValueOnce({ ...run, status: 'waiting_user' })
    api.history.mockResolvedValue({ messages: [], runs: [run] })
    api.run.mockResolvedValue(run)
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    const firstSignal = api.advance.mock.calls[0]![1] as AbortSignal
    store.pause()
    expect(firstSignal.aborted).toBe(true)
    finishFirst(run)
    await Promise.resolve()
    expect(api.advance).toHaveBeenCalledTimes(1)
    await store.selectConversation('conversation', true)
    expect(api.advance).toHaveBeenCalledTimes(2)
    expect(store.activeRun?.status).toBe('waiting_user')
  })
  it('keeps legacy runs on their event stream without invoking browser execution', async () => {
    const run = { id: 'legacy', conversation_id: 'conversation', model: 'gpt', status: 'running', created_at: '', updated_at: '' }
    api.history.mockResolvedValue({ messages: [], runs: [run] })
    api.run.mockResolvedValue(run)
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    expect(readEvents).toHaveBeenCalledWith('legacy', 0, expect.any(AbortSignal), expect.any(Function))
    expect(api.advance).not.toHaveBeenCalled()
  })

  it('resumes the same active task after a failed stop request', async () => {
    const run = { id: 'run', conversation_id: 'conversation', model: 'gemini', status: 'running', browser_driven: true, created_at: '', updated_at: '' }
    api.history.mockResolvedValue({ messages: [], runs: [run] })
    api.run.mockResolvedValue(run)
    api.advance.mockImplementation(() => new Promise(() => {}))
    api.cancel.mockRejectedValueOnce({ status: 503, message: '停止请求失败' })
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    const original = api.advance.mock.calls[0]?.[1] as AbortSignal
    await store.cancelRun()
    expect(original.aborted).toBe(true)
    expect(api.advance.mock.calls.map(([id]) => id)).toEqual(['run', 'run'])
    expect(readEvents).toHaveBeenCalledTimes(2)
    expect(store.error).toBe('停止请求失败')
  })

  it('surfaces a live MODEL_ROUTE_CHANGED error carried in a successful step response', async () => {
    const run = { id: 'run', conversation_id: 'conversation', model: 'gemini', status: 'running', browser_driven: true, created_at: '', updated_at: '' }
    const failed = { ...run, status: 'failed', error_code: 'MODEL_ROUTE_CHANGED' }
    api.history.mockResolvedValueOnce({ messages: [], runs: [run] }).mockResolvedValue({ messages: [], runs: [failed] })
    api.run.mockResolvedValue(failed)
    api.advance.mockResolvedValue(failed)
    const store = useWorkbenchChatStore()
    await store.selectConversation('conversation')
    await Promise.resolve()
    expect(store.modelRouteChangedRunID).toBe('run')
    expect(store.activeRun).toBeUndefined()
  })
})
