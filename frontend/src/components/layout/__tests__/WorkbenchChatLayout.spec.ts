import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import WorkbenchChatLayout from '../WorkbenchChatLayout.vue'
import { useWorkspaceNavigationStore } from '@/stores/workspaceNavigation'
import { withWorkspaceMeta } from '@/router/workspace'

const { app, auth, chat, tickets, flags } = vi.hoisted(() => ({
  app: { sidebarCollapsed: false, backendModeEnabled: false, siteName: 'MiloRouter', siteLogo: '', publicSettingsLoaded: true, docUrl: '', contactInfo: '', cachedPublicSettings: { custom_menu_items: [], image_studio_enabled: true }, toggleSidebar: vi.fn() },
  auth: { isAdmin: false, isSimpleMode: false, user: { id: 7, role: 'user', email: 'user@example.com', balance: 25 } },
  chat: { status: { files_enabled: true }, newConversation: vi.fn() },
  tickets: { userUnreadCount: 2, startPolling: vi.fn(), stopPolling: vi.fn() },
  flags: new Set<string>(),
}))
const appState = reactive(app), authState = reactive(auth), chatState = reactive(chat)
const mounted: VueWrapper[] = []
let desktop = true
let viewportListener: ((event: MediaQueryListEvent) => void) | undefined
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores', () => ({ useAppStore: () => appState, useAuthStore: () => authState, useOnboardingStore: () => ({ replay: vi.fn() }) }))
vi.mock('@/stores/workbenchChat', () => ({ useWorkbenchChatStore: () => chatState }))
vi.mock('@/stores/tickets', () => ({ useTicketStore: () => tickets }))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: { value: false }, refreshBatchImageAccess: vi.fn() }) }))
vi.mock('@/utils/featureFlags', async importOriginal => ({ ...await importOriginal<typeof import('@/utils/featureFlags')>(), isFeatureFlagEnabled: (flag: { key: string }) => flags.has(flag.key) }))
vi.mock('@/utils/billingCurrency', () => ({ formatCurrentBillingAmount: (value: number) => String(value) }))

async function setup() {
  const component = { template: '<div />' }
  const router = createRouter({ history: createMemoryHistory(), routes: withWorkspaceMeta([
    { path: '/workbench/chat/:id?', component, meta: { workspaceArea: 'workbench' } },
    { path: '/workbench/image', component, meta: { workspaceArea: 'workbench' } },
    { path: '/workbench/files', component, meta: { workspaceArea: 'workbench' } },
    { path: '/workbench/downloads', component, meta: { workspaceArea: 'workbench' } },
    { path: '/admin/workbench', component, meta: { requiresAdmin: true, workspaceArea: 'workbench' } },
    ...['/dashboard', '/keys', '/usage', '/profile', '/tickets', '/subscriptions', '/model-plaza', '/downloads'].map(path => ({ path, component })),
  ]) })
  await router.push('/workbench/chat/one')
  const pinia = createPinia()
  const wrapper = mount(WorkbenchChatLayout, {
    attachTo: document.body,
    slots: { default: '<button data-testid="composer">消息输入</button>' },
    global: { plugins: [router, pinia], stubs: {
      ConversationList: { props: ['showNewButton'], template: '<section data-testid="conversation-list" :data-show-new="String(showNewButton)" />' },
      AnnouncementBell: { template: '<button aria-label="公告" />' },
      LocaleSwitcher: { template: '<button aria-label="语言" />' },
      SubscriptionProgressMini: true,
      Icon: true,
    } },
  })
  mounted.push(wrapper)
  return { wrapper, router, pinia }
}

describe('personal chat shell', () => {
  beforeEach(() => {
    desktop = true
    appState.sidebarCollapsed = false
    appState.backendModeEnabled = false
    appState.docUrl = ''
    appState.cachedPublicSettings.image_studio_enabled = true
    authState.isAdmin = false
    authState.isSimpleMode = false
    chatState.status.files_enabled = true
    flags.clear()
    vi.clearAllMocks()
    chat.newConversation.mockResolvedValue(undefined)
    app.toggleSidebar.mockImplementation(() => { appState.sidebarCollapsed = !appState.sidebarCollapsed })
    document.documentElement.classList.remove('dark')
    localStorage.clear()
    sessionStorage.clear()
    window.matchMedia = vi.fn((query: string) => ({ matches: query.includes('1024px') ? desktop : false, media: query, addEventListener: vi.fn((_, callback) => { if (query.includes('1024px')) viewportListener = callback }), removeEventListener: vi.fn() })) as unknown as typeof window.matchMedia
  })
  afterEach(() => { for (const wrapper of mounted.splice(0)) wrapper.unmount(); document.body.style.overflow = ''; document.documentElement.classList.remove('dark') })

  it('uses a single history sidebar and leaves management navigation out of personal history', async () => {
    const { wrapper } = await setup()
    expect(wrapper.findAll('aside')).toHaveLength(1)
    expect(wrapper.findAll('[data-testid="conversation-list"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="conversation-list"]').attributes('data-show-new')).toBe('false')
    expect(wrapper.find('a[href="/workbench/image"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/workbench/files"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/admin/workbench"]').exists()).toBe(false)
    expect(wrapper.get('footer .chat-balance dt').text()).toBe('common.availableBalance')
    expect(wrapper.get('footer .chat-balance dd').text()).toBe('25')
    expect(wrapper.find('.glass').exists()).toBe(false)
    expect(tickets.startPolling).toHaveBeenCalledWith(false)
  })

  it('gives administrators an explicit management entry without embedding admin menus', async () => {
    authState.isAdmin = true
    const { wrapper } = await setup()
    expect(wrapper.findAll('a[href^="/admin/"]')).toHaveLength(1)
    expect(wrapper.get('a[href="/admin/workbench"]').text()).toContain('nav.workbenchManagement')
    expect(wrapper.get('a[href="/keys"]').text()).toContain('nav.console')
  })

  it('keeps file feature visibility and desktop collapse separate from mobile history', async () => {
    const { wrapper } = await setup()
    chatState.status.files_enabled = false
    appState.cachedPublicSettings.image_studio_enabled = false
    await flushPromises()
    expect(wrapper.find('a[href="/workbench/files"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/workbench/image"]').exists()).toBe(false)
    await wrapper.get('button[aria-label="收起侧栏"]').trigger('click')
    expect(wrapper.classes()).toContain('chat-sidebar-collapsed')
    expect(wrapper.find('[data-testid="conversation-list"]').exists()).toBe(false)
    expect(wrapper.find('.chat-balance').exists()).toBe(false)
    viewportListener?.({ matches: false } as MediaQueryListEvent)
    await flushPromises()
    expect(wrapper.classes()).not.toContain('chat-sidebar-collapsed')
    expect(wrapper.findAll('[data-testid="conversation-list"]')).toHaveLength(1)
    expect(wrapper.get('footer .chat-balance dd').text()).toBe('25')
  })

  it('resets the current conversation before navigating to a new one', async () => {
    const { wrapper, router } = await setup()
    await wrapper.get('.chat-new-conversation').trigger('click')
    await flushPromises()
    expect(chat.newConversation).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.path).toBe('/workbench/chat')
    expect(document.activeElement).toBe(wrapper.get('main').element)
  })

  it('restores only visible console destinations and retains a safe default', async () => {
    const { wrapper, router, pinia } = await setup()
    const memory = useWorkspaceNavigationStore(pinia)
    memory.paths = { 'personal:console': '/usage?model=gpt' }
    await flushPromises()
    expect(wrapper.get('a[href="/usage?model=gpt"]').exists()).toBe(true)
    authState.isSimpleMode = true
    await flushPromises()
    expect(wrapper.get('a[href="/dashboard"]').exists()).toBe(true)
    expect(router.currentRoute.value.path).toBe('/workbench/chat/one')
  })

  it('traps mobile navigation focus, restores it on Escape and cleans up body locking', async () => {
    desktop = false
    document.body.style.overflow = 'auto'
    const { wrapper } = await setup()
    const trigger = wrapper.get<HTMLButtonElement>('button[aria-label="打开工作台导航"]')
    trigger.element.focus()
    await trigger.trigger('click')
    await flushPromises()
    expect(wrapper.get('aside').attributes('role')).toBe('dialog')
    expect(wrapper.get('main').attributes('inert')).toBeDefined()
    expect(document.body.style.overflow).toBe('hidden')
    expect(document.activeElement?.getAttribute('aria-label')).toBe('关闭导航')
    const first = wrapper.get('aside a').element as HTMLElement
    first.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement?.getAttribute('aria-label')).toBe('common.userMenu')
    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }))
    await flushPromises()
    expect(wrapper.get('aside').attributes('aria-hidden')).toBe('true')
    expect(document.activeElement).toBe(trigger.element)
    expect(document.body.style.overflow).toBe('auto')
  })

  it('closes mobile navigation on route changes and retains existing theme persistence', async () => {
    desktop = false
    const { wrapper, router } = await setup()
    await wrapper.get('button[aria-label="打开工作台导航"]').trigger('click')
    await wrapper.get('button[aria-label="切换深色模式"]').trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
    await router.push('/workbench/image')
    await flushPromises()
    expect(wrapper.get('aside').attributes('aria-hidden')).toBe('true')
    expect(document.body.style.overflow).toBe('')
  })

  it('preserves global account entries and keyboard access in the bottom account menu', async () => {
    appState.docUrl = 'https://docs.example.com/help'
    flags.add('model_plaza_enabled')
    const { wrapper } = await setup()
    const trigger = wrapper.get<HTMLButtonElement>('button[aria-label="common.userMenu"]')
    trigger.element.focus()
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(document.activeElement?.getAttribute('href')).toBe('/workbench/downloads')
    const menu = wrapper.get('.account-menu-popover')
    expect(menu.find('a[href="https://docs.example.com/help"]').exists()).toBe(true)
    expect(menu.find('a[href="/model-plaza?embedded=1"]').exists()).toBe(true)
    expect(menu.find('a[href="/tickets"]').text()).toContain('2')
    expect(menu.find('a[href="/profile"]').exists()).toBe(true)
    expect(menu.find('a[href="/keys"]').exists()).toBe(true)
    expect(menu.text()).toContain('25')
    await menu.trigger('keydown', { key: 'End' })
    expect(document.activeElement?.textContent).toContain('nav.logout')
    await menu.trigger('keydown', { key: 'Escape' })
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)
  })
})
