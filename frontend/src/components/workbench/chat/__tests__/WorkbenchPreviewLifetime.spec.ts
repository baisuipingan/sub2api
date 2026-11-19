import { createPinia, defineStore, setActivePinia } from 'pinia'
import { computed, defineComponent, h, onMounted, onUnmounted, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import WorkbenchChatView from '@/views/user/WorkbenchChatView.vue'

const mocks = vi.hoisted(() => ({ mounts: vi.fn(), unmounts: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { conversationId: 'conversation' }, query: {} }), useRouter: () => ({ replace: vi.fn() }) }))
vi.mock('@vueuse/core', () => ({ useMediaQuery: () => ref(false) }))
vi.mock('@/api/workbenchChat', () => ({ workbenchChatApi: {} }))
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
    wrapper = mount(WorkbenchChatView, { attachTo: document.body, global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true, ChatComposer: true, TaskProgress: true, PinnedTaskPanel: true, ResultSection: true, ResourcePreview: Preview } } })
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
})
