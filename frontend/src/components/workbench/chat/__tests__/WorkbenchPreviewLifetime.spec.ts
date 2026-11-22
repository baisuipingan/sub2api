import { createPinia, defineStore, setActivePinia } from 'pinia'
import { computed, defineComponent, h, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkbenchChatView from '@/views/user/WorkbenchChatView.vue'
import { useWorkbenchChatStore } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import type { ChatMessage, ChatModel, ChatRun } from '@/types/workbenchChat'

const mocks = vi.hoisted(() => ({ mounts: vi.fn(), unmounts: vi.fn(), send: vi.fn(), initialize: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { conversationId: 'conversation' }, query: {} }), useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(false) }))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: defineStore('preview-view-app', () => ({ siteName: ref('MiloRouter'), siteLogo: ref('') })) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: defineStore('preview-view-auth', () => ({ user: ref({ id: 1 }) })) }))
vi.mock('@/stores/workbenchChat', () => ({
  chatError: () => '请求失败',
  useWorkbenchChatStore: defineStore('preview-view-chat', () => ({
    status: ref({ enabled: true, office_enabled: true, search_enabled: true, search_allowed: true, search_price_per_1000_usd: 0 }), models: ref<ChatModel[]>([{ id: 'gpt', supports_tools: true }, { id: 'text', supports_tools: false }]), modelCatalog: ref({ default_model: 'gpt' }),
    history: ref({ messages: [], runs: [{ id: 'run', model: 'gpt', status: 'completed', mode: 'office', created_at: '' }] }), events: ref({}),
    activeConversationId: ref('conversation'), activeRun: computed(() => null), initialize: mocks.initialize, selectConversation: vi.fn(), pause: vi.fn(), send: mocks.send,
  })),
}))
const Preview = defineComponent({ name: 'ResourcePreview', props: ['fullscreen'], emits: ['fullscreen', 'close'], setup(props, { emit }) { onMounted(mocks.mounts); onUnmounted(mocks.unmounts); return () => h('section', { 'data-preview': '' }, [h('iframe', { src: 'about:blank' }), h('button', { 'data-toggle-fullscreen': '', onClick: () => emit('fullscreen') }, props.fullscreen ? '退出全屏' : '全屏预览')]) } })
let wrapper: ReturnType<typeof mount> | undefined
describe('conversation preview lifetime', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks(); localStorage.clear() })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = ''; localStorage.clear() })
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
      { id: 'carbon', reasoning_efforts: ['low', 'medium', 'high'], default_reasoning_effort: 'medium' },
      { id: 'flash', reasoning_efforts: ['high', 'max'] },
    ]
    store.modelCatalog.default_model = 'carbon'
    store.history = {
      runs: [{ id: 'run', conversation_id: 'conversation', model: 'carbon', status: 'cancelled', created_at: '', updated_at: '' }],
      messages: [{ id: 'msg', conversation_id: 'conversation', run_id: 'run', role: 'user', sequence: 1, content: '重试这条消息', created_at: '' }],
    }
    wrapper = mount(WorkbenchChatView, { global: { stubs: {
      WorkbenchChatLayout: { template: '<div><slot /></div>' },
      ChatComposer: { name: 'ChatComposer', props: ['effort'], template: '<div />' },
      Icon: true, TaskProgress: true, ResultSection: true, ResourcePreview: true,
    } } })
    await flushPromises()
    const composer = wrapper.getComponent({ name: 'ChatComposer' })
    expect(composer.props('effort')).toBe('medium')
    composer.vm.$emit('update:modelValue', '测试推荐强度')
    composer.vm.$emit('send')
    expect(mocks.send).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'carbon', reasoning_effort: 'medium' }))

    composer.vm.$emit('update:effort', 'low')
    composer.vm.$emit('send')
    expect(mocks.send).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'carbon', reasoning_effort: 'low' }))

    composer.vm.$emit('update:model', 'flash')
    await nextTick()
    expect(composer.props('effort')).toBe('')
    await wrapper.get('[aria-label="重试任务"]').trigger('click')
    expect(mocks.send).toHaveBeenLastCalledWith({ model: 'flash', content: '重试这条消息', web_search: false })
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
