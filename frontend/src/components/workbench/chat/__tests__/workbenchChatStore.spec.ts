import { createPinia, defineStore, setActivePinia } from 'pinia'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const api = vi.hoisted(() => ({ status: vi.fn(), models: vi.fn(), conversations: vi.fn(), history: vi.fn(), createConversation: vi.fn(), submit: vi.fn(), run: vi.fn(), cancel: vi.fn(), renameConversation: vi.fn(), deleteConversation: vi.fn(), selectAnswer: vi.fn() }))
const readEvents = vi.hoisted(() => vi.fn(async (_id: string, _after: number, _signal: AbortSignal, _accept: (event: unknown) => void) => {}))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: api, readChatEvents: readEvents }))
vi.mock('@/stores/auth', () => ({ useAuthStore: defineStore('test-workbench-auth', () => ({ user: ref<{ id: number } | null>({ id: 1 }) })) }))
import { useAuthStore } from '@/stores/auth'
import { useWorkbenchChatStore } from '@/stores/workbenchChat'
const message = (id: string, sequence: number) => ({ id, sequence, conversation_id: 'conversation', run_id: 'run', role: 'user', content: id, created_at: '' })
describe('workbench account isolation and durable submissions', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks() })
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
})
