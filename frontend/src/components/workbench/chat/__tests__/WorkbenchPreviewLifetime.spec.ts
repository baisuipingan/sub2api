import { createPinia, defineStore, setActivePinia } from 'pinia'
import { computed, defineComponent, h, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkbenchChatView from '@/views/user/WorkbenchChatView.vue'
import { useWorkbenchChatStore } from '@/stores/workbenchChat'
import { useAuthStore } from '@/stores/auth'
import type { ChatMessage, ChatRun } from '@/types/workbenchChat'

const mocks = vi.hoisted(() => ({ mounts: vi.fn(), unmounts: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { conversationId: 'conversation' }, query: {} }), useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(false) }))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: {} }))
vi.mock('@/stores/app', () => ({ useAppStore: defineStore('preview-view-app', () => ({ siteName: ref('MiloRouter'), siteLogo: ref('') })) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: defineStore('preview-view-auth', () => ({ user: ref({ id: 1 }) })) }))
vi.mock('@/stores/workbenchChat', () => ({
  chatError: () => '请求失败',
  useWorkbenchChatStore: defineStore('preview-view-chat', () => ({
    status: ref({ enabled: true, office_enabled: true }), models: ref([{ id: 'gpt', supports_tools: true }]), modelCatalog: ref({ default_model: 'gpt' }),
    history: ref({ messages: [], runs: [{ id: 'run', model: 'gpt', status: 'completed', mode: 'office', created_at: '' }] }), events: ref({}),
    activeConversationId: ref('conversation'), activeRun: computed(() => null), initialize: vi.fn(), selectConversation: vi.fn(), stopWatching: vi.fn(),
  })),
}))
const Preview = defineComponent({ name: 'ResourcePreview', props: ['fullscreen'], emits: ['fullscreen', 'close'], setup(props, { emit }) { onMounted(mocks.mounts); onUnmounted(mocks.unmounts); return () => h('section', { 'data-preview': '' }, [h('iframe', { src: 'about:blank' }), h('button', { 'data-toggle-fullscreen': '', onClick: () => emit('fullscreen') }, props.fullscreen ? '退出全屏' : '全屏预览')]) } })
let wrapper: ReturnType<typeof mount> | undefined
describe('conversation preview lifetime', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks() })
  afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = '' })
  it('moves one preview between dock and full screen without remounting its iframe', async () => {
    wrapper = mount(WorkbenchChatView, { attachTo: document.body, global: { stubs: { WorkbenchChatLayout: { template: '<div><slot /></div>' }, Icon: true, ChatComposer: { template: '<div />', methods: { focus() {} } }, TaskProgress: true, PinnedTaskPanel: true, ResultSection: true, ChatExportToolbar: true, ResourcePreview: Preview } } })
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text().includes('文件工作区'))!.trigger('click')
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
    wrapper = mount(WorkbenchChatView, { attachTo: document.body, global: { stubs: { WorkbenchChatLayout: { template: '<div><slot /></div>' }, Icon: true, ChatComposer: { template: '<div />', methods: { focus() {} } }, TaskProgress: true, PinnedTaskPanel: true, ResultSection: true, ChatExportToolbar: true, ResourcePreview: Preview } } })
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
})
