import { createPinia, defineStore, setActivePinia } from 'pinia'
import { computed, defineComponent, h, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkbenchChatView from '@/views/user/WorkbenchChatView.vue'
import { useWorkbenchChatStore } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import type { ChatEvent, ChatMessage, ChatModel, ChatRun } from '@/types/workbenchChat'

const mocks = vi.hoisted(() => ({ mounts: vi.fn(), unmounts: vi.fn(), send: vi.fn(), initialize: vi.fn() }))
const route = reactive({ params: { conversationId: 'conversation' as string | undefined }, query: {} })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(false) }))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: { toolDetail: vi.fn() } }))
vi.mock('@/stores/app', () => ({ useAppStore: defineStore('preview-view-app', () => ({ siteName: ref('MiloRouter'), siteLogo: ref('') })) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: defineStore('preview-view-auth', () => ({ user: ref({ id: 1 }) })) }))
vi.mock('@/stores/workbenchChat', () => ({
  chatError: () => '请求失败',
  useWorkbenchChatStore: defineStore('preview-view-chat', () => ({
    status: ref({ enabled: true, office_enabled: true, search_enabled: true, search_allowed: true, search_price_per_1000_usd: 0 }), models: ref<ChatModel[]>([{ id: 'gpt', supports_tools: true }, { id: 'text', supports_tools: false }]), modelCatalog: ref({ default_model: 'gpt' }),
    history: ref({ messages: [], runs: [{ id: 'run', model: 'gpt', status: 'completed', mode: 'office', created_at: '' }] }), events: ref<Record<string, ChatEvent[]>>({}),
    modelRouteChangedRunID: ref(''),
    activeConversationId: ref('conversation'), activeRun: computed(() => null), initialize: mocks.initialize, selectConversation: vi.fn(), pause: vi.fn(), send: mocks.send,
  })),
}))
const Preview = defineComponent({ name: 'ResourcePreview', props: ['fullscreen'], emits: ['fullscreen', 'close'], setup(props, { emit }) { onMounted(mocks.mounts); onUnmounted(mocks.unmounts); return () => h('section', { 'data-preview': '' }, [h('iframe', { src: 'about:blank' }), h('button', { 'data-toggle-fullscreen': '', onClick: () => emit('fullscreen') }, props.fullscreen ? '退出全屏' : '全屏预览')]) } })
let wrapper: ReturnType<typeof mount> | undefined
describe('conversation preview lifetime', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks(); route.params.conversationId = 'conversation'; localStorage.clear() })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = ''; localStorage.clear() })

  it('renders a zero-tool model completion once, then replaces the stream with the saved answer', async () => {
    const store = useWorkbenchChatStore()
    const run: ChatRun = { id: 'run', conversation_id: 'conversation', model: 'gpt', status: 'running', browser_driven: true, created_at: '', updated_at: '' }
    store.history.runs = [run]
    const event = (sequence: number, type: string, payload: Record<string, unknown>): ChatEvent => ({ id: `event-${sequence}`, run_id: run.id, sequence, type, payload, created_at: '' })
    const opener = event(1, 'assistant.delta', { operation_id: 'model-1', text: '先查资料。' })
    store.events[run.id] = [opener]
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: true, Icon: true, ResourcePreview: true, ToolOperationDetails: true,
    } } })
    await flushPromises()
    const process = wrapper.get<HTMLDetailsElement>('.bs-chat-turn-process')
    expect(process.element.open).toBe(true)
    expect(process.text()).toContain('先查资料。')
    expect(wrapper.find('.bs-chat-result').exists()).toBe(false)
    store.events[run.id] = [
      opener,
      event(2, 'model.completed', { operation_id: 'model-1', tool_count: 1 }),
      event(3, 'tool.completed', { operation_id: 'tool-1', title: '联网搜索' }),
      event(4, 'model.started', { operation_id: 'model-2' }),
      event(5, 'assistant.delta', { operation_id: 'model-2', text: '实时回答。' }),
      event(6, 'model.completed', { operation_id: 'model-2', tool_count: 0 }),
    ]
    await nextTick()
    expect(process.element.open).toBe(true)
    expect(wrapper.get('.bs-chat-step-commentary').text()).toBe('先查资料。')
    expect(wrapper.findAll('.bs-chat-step-commentary')).toHaveLength(1)
    expect(wrapper.get('.bs-chat-result').text()).toBe('实时回答。')
    expect(wrapper.get('.bso-breathing').text()).toBe('正在整理回答…')
    store.history.runs = [{ ...run, status: 'completed' }]
    store.history.messages = [{ id: 'answer', conversation_id: 'conversation', run_id: run.id, role: 'assistant', sequence: 7, content: '保存后的回答。', created_at: '' }]
    await nextTick()
    expect(process.element.open).toBe(false)
    expect(process.find('.bs-chat-result').exists()).toBe(false)
    expect(wrapper.get('.bs-chat-step-commentary').text()).toBe('先查资料。')
    expect(wrapper.get('.bs-chat-result').text()).toBe('保存后的回答。')
    expect(wrapper.text()).not.toContain('实时回答。')
    expect(wrapper.find('.bso-breathing').exists()).toBe(false)
  })
  it('moves one preview between dock and full screen without remounting its iframe', async () => {
    const store = useWorkbenchChatStore()
    store.history.runs[0]!.artifacts = [{ id: 'report', version_id: 'report', name: '报告.docx', mime: '', bytes: 1 }]
    wrapper = mount(WorkbenchChatView, { attachTo: document.body, global: { stubs: { WorkbenchChatLayout: { template: '<div><slot /></div>' }, Icon: true, ChatComposer: { template: '<div />', methods: { focus() {} } }, TaskProgress: true, ResultSection: true, ChatExportToolbar: true, ResourcePreview: Preview } } })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('对话文件'))!.trigger('click')
    const frame = document.querySelector('[data-preview] iframe')
    expect(frame).not.toBeNull(); expect(mocks.mounts).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-toggle-fullscreen]').trigger('click'); await flushPromises()
    expect(document.querySelector('[role="dialog"] iframe')).toBe(frame)
    await document.querySelector<HTMLButtonElement>('[data-toggle-fullscreen]')!.click(); await flushPromises()
    expect(wrapper.get('.bs-chat-preview-dock iframe').element).toBe(frame)
    expect(mocks.mounts).toHaveBeenCalledTimes(1); expect(mocks.unmounts).not.toHaveBeenCalled()
  })
  it('exports the displayed regenerated answer with the original question and clears selection on account change', async () => {
    const store = useWorkbenchChatStore(), auth = useAuthStore()
    const run = (id: string, extra = {}): ChatRun => ({ id, conversation_id: 'conversation', model: 'gpt', status: 'completed', created_at: '', updated_at: '', ...extra })
    const message = (id: string, run_id: string, role: ChatMessage['role'], sequence: number): ChatMessage => ({ id, run_id, role, sequence, conversation_id: 'conversation', content: id, created_at: '' })
    store.history = { runs: [run('root'), run('regenerated', { regeneration_of: 'root', selected: true })], messages: [message('question', 'root', 'user', 1), message('old-answer', 'root', 'assistant', 2), message('new-answer', 'regenerated', 'assistant', 3), message('internal', 'regenerated', 'system', 4)] }
    wrapper = mount(WorkbenchChatView, { attachTo: document.body, global: { stubs: { WorkbenchChatLayout: { template: '<div><slot /></div>' }, Icon: true, ChatComposer: { template: '<div />', methods: { focus() {} } }, TaskProgress: true, ResultSection: true, ChatExportToolbar: true, ResourcePreview: Preview } } })
    await flushPromises()
    await wrapper.get('[aria-label="导出回答"]').trigger('click')
    expect(wrapper.findAll('.bs-chat-message-check')).toHaveLength(2)
    expect(wrapper.findAll<HTMLInputElement>('.bs-chat-message-check').every(input => input.element.checked)).toBe(true)
    expect(wrapper.findComponent({ name: 'ChatExportToolbar' }).props('messages')).toHaveLength(2)
    expect(wrapper.findComponent({ name: 'ChatExportToolbar' }).props('messages').map((item: ChatMessage) => item.id)).toEqual(['question', 'new-answer'])
    auth.user = { ...auth.user!, id: 2 }
    await nextTick()
    expect(wrapper.findComponent({ name: 'ChatExportToolbar' }).exists()).toBe(false)
  })

  it('defaults to search where supported and honors a user opt-out in the submitted turn', async () => {
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['webSearch'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    expect(composer.props('webSearch')).toBe(true)
    composer.vm.$emit('update:model', 'text')
    await nextTick()
    expect(composer.props('webSearch')).toBe(false)
    composer.vm.$emit('update:model', 'gpt')
    await nextTick()
    expect(composer.props('webSearch')).toBe(true)
    composer.vm.$emit('update:webSearch', false)
    composer.vm.$emit('update:modelValue', '只使用已有资料')
    await nextTick()
    composer.vm.$emit('send')
    expect(mocks.send).toHaveBeenCalledWith(expect.objectContaining({ web_search: false }))
    expect(localStorage.getItem('workbench-chat-search:1')).toBe('off')
  })

  it('uses the model recommendation until changed and retries a stopped turn with the selected model', async () => {
    const store = useWorkbenchChatStore()
    store.models = [
      { id: 'carbon', reasoning_efforts: ['none', 'low', 'medium', 'high'], default_reasoning_effort: 'medium' },
      { id: 'flash', reasoning_efforts: ['none', 'high', 'max'], default_reasoning_effort: 'high' },
    ]
    store.modelCatalog.default_model = 'carbon'
    store.history = {
      runs: [{ id: 'run', conversation_id: 'conversation', model: 'carbon', status: 'cancelled', created_at: '', updated_at: '' }],
      messages: [{ id: 'msg', conversation_id: 'conversation', run_id: 'run', role: 'user', sequence: 1, content: '重试这条消息', created_at: '' }],
    }
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['effort', 'modelValue'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    expect(composer.props('effort')).toBe('medium')
    composer.vm.$emit('update:modelValue', '测试推荐强度')
    composer.vm.$emit('send')
    expect(mocks.send).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'carbon', reasoning_effort: 'medium' }))

    composer.vm.$emit('update:effort', 'none')
    composer.vm.$emit('send')
    expect(mocks.send).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'carbon', reasoning_effort: 'none' }))

    composer.vm.$emit('update:model', 'flash')
    await nextTick()
    expect(composer.props('effort')).toBe('high')
    composer.vm.$emit('update:modelValue', '还没有发送的新稿')
    mocks.send.mockResolvedValueOnce('conversation')
    await wrapper.get('[aria-label="重试任务"]').trigger('click')
    await flushPromises()
    expect(mocks.send).toHaveBeenLastCalledWith({ model: 'flash', content: '重试这条消息', web_search: false, regenerate_run_id: 'run', reasoning_effort: 'high' })
    expect(composer.props('modelValue')).toBe('还没有发送的新稿')
  })

  it('clears the old conversation draft when the sidebar already reset the store selection', async () => {
    const store = useWorkbenchChatStore()
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['modelValue'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    composer.vm.$emit('update:modelValue', '上一场会话的草稿')
    await nextTick()
    expect(composer.props('modelValue')).toBe('上一场会话的草稿')
    store.activeConversationId = ''
    route.params.conversationId = undefined
    await flushPromises()
    expect(composer.props('modelValue')).toBe('')
  })

  it('refreshes the catalog when a live task reports a changed model route', async () => {
    const store = useWorkbenchChatStore()
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['modelValue', 'model'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    composer.vm.$emit('update:modelValue', '保留我的草稿')
    mocks.initialize.mockImplementationOnce(async () => {
      store.models = [{ id: 'gemini' }]
      store.modelCatalog.default_model = 'gemini'
    })
    store.modelRouteChangedRunID = 'run'
    await flushPromises()
    expect(mocks.initialize).toHaveBeenCalledTimes(2)
    expect(composer.props('model')).toBe('gemini')
    expect(composer.props('modelValue')).toBe('保留我的草稿')
    expect(mocks.send).not.toHaveBeenCalled()
  })

  it('blocks sending and retrying when the shared catalog is empty and reloads it on request', async () => {
    const store = useWorkbenchChatStore()
    const preference = 'workbench-chat-model:1'
    localStorage.setItem(preference, 'retired')
    store.models = []
    store.modelCatalog.default_model = 'retired'
    store.history = {
      runs: [{ id: 'run', conversation_id: 'conversation', model: 'retired', status: 'cancelled', created_at: '', updated_at: '' }],
      messages: [{ id: 'msg', conversation_id: 'conversation', run_id: 'run', role: 'user', sequence: 1, content: '历史消息', created_at: '' }],
    }
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['disabled', 'model'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    expect(composer.props('disabled')).toBe(true)
    expect(composer.props('model')).toBe('')
    expect(wrapper.get('[role="status"]').text()).toContain('对话助手暂无可用模型')
    composer.vm.$emit('update:modelValue', '测试消息')
    composer.vm.$emit('send')
    const retry = wrapper.get<HTMLButtonElement>('[aria-label="重试任务"]')
    expect(retry.element.disabled).toBe(true)
    await retry.trigger('click')
    expect(mocks.send).not.toHaveBeenCalled()

    mocks.initialize.mockImplementationOnce(async () => {
      store.models = [{ id: 'gpt' }, { id: 'claude' }]
      store.modelCatalog.default_model = 'claude'
    })
    const refresh = wrapper.get('.bs-chat-page-error button')
    await refresh.trigger('click')
    await flushPromises()
    expect(mocks.initialize).toHaveBeenLastCalledWith(true)
    expect(composer.props('model')).toBe('claude')
    expect(composer.props('disabled')).toBe(false)
    expect(localStorage.getItem(preference)).toBe('retired')
    composer.vm.$emit('update:model', 'gpt')
    expect(localStorage.getItem(preference)).toBe('gpt')
  })

  it('refreshes a changed model list without resending or losing the draft', async () => {
    const store = useWorkbenchChatStore()
    mocks.send.mockRejectedValueOnce({ status: 409, error: { code: 'MODEL_ROUTE_CHANGED' } })
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['modelValue', 'model'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    mocks.initialize.mockImplementationOnce(async () => {
      store.models = [{ id: 'claude' }]
      store.modelCatalog.default_model = 'claude'
    })
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    composer.vm.$emit('update:modelValue', '测试目录刷新')
    composer.vm.$emit('send')
    await flushPromises()
    expect(mocks.send).toHaveBeenCalledOnce()
    expect(mocks.initialize).toHaveBeenLastCalledWith(true)
    expect(composer.props('model')).toBe('claude')
    expect(composer.props('modelValue')).toBe('测试目录刷新')
    expect(wrapper.get('[role="alert"]').text()).toContain('工作台模型配置已更新')
  })

  it('sends the displayed search quote and refreshes a stale quote without resubmitting', async () => {
    const store = useWorkbenchChatStore()
    store.status!.search_price_per_1000_usd = 4
    mocks.send.mockRejectedValueOnce({ status: 409, error: { code: 'WORKBENCH_SEARCH_PRICE_CHANGED' } })
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['modelValue', 'searchPricePer1000Usd'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    mocks.initialize.mockImplementationOnce(async () => {
      store.status!.search_price_per_1000_usd = 5
    })
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    expect(composer.props('searchPricePer1000Usd')).toBe(4)
    composer.vm.$emit('update:modelValue', '查一下资料')
    composer.vm.$emit('send')
    await flushPromises()
    expect(mocks.send).toHaveBeenCalledOnce()
    expect(mocks.send).toHaveBeenCalledWith(expect.objectContaining({ web_search: true, search_price_per_1000_usd: 4 }))
    expect(composer.props('searchPricePer1000Usd')).toBe(5)
    expect(composer.props('modelValue')).toBe('查一下资料')
    expect(wrapper.get('[role="alert"]').text()).toContain('联网搜索价格已变更')
  })
})
