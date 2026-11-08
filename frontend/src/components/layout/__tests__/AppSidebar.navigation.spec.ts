import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { reactive, ref } from 'vue'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import AppSidebar from '../AppSidebar.vue'
import { withWorkspaceMeta } from '@/router/workspace'
import { workbenchRoutes } from '@/router/workbenchRoutes'
import { useWorkspaceNavigationStore } from '@/stores/workspaceNavigation'

const appState = reactive({
  sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0, backendModeEnabled: false,
  siteName: 'MiloRouter', siteLogo: '', siteVersion: 'test', publicSettingsLoaded: true,
  cachedPublicSettings: { custom_menu_items: [], payment_enabled: true } as Record<string, any>,
  toggleSidebar: () => { appState.sidebarCollapsed = !appState.sidebarCollapsed },
  setMobileOpen: (open: boolean) => { appState.mobileOpen = open },
})
const authState = reactive({ isAdmin: true, isSimpleMode: false, user: { id: 7 } as { id: number } | null })
const adminState = reactive({ customMenuItems: [] as Array<{ id: string; label: string; visibility: string; sort_order: number; icon_svg: string }>, opsMonitoringEnabled: true, paymentEnabled: true, fetch: vi.fn() })
const mounted: VueWrapper[] = []

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string, args?: Record<string, string>) => args?.area ? key + args.area : key }) }))
vi.mock('@/stores', () => ({
  useAppStore: () => appState,
  useAuthStore: () => authState,
  useAdminSettingsStore: () => adminState,
  useOnboardingStore: () => ({ isCurrentStep: () => false, nextStep: vi.fn() }),
  useTicketStore: () => ({ userUnreadCount: 0, adminUnreadCount: 0 }),
}))
vi.mock('@/utils/featureFlags', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/utils/featureFlags')>(),
  makeSidebarFlag: (flag: { key: string }) => () => appState.cachedPublicSettings[flag.key] !== false,
}))
vi.mock('@/composables/useBatchImageAccess', () => ({ useBatchImageAccess: () => ({ canUseBatchImage: ref(true), refreshBatchImageAccess: vi.fn() }) }))

async function setup(path: string) {
  const pinia = createPinia(); setActivePinia(pinia)
  const component = { template: '<div />' }
  const router = createRouter({ history: createMemoryHistory(), routes: withWorkspaceMeta([
    ...workbenchRoutes.map(route => route.redirect ? route : { ...route, component }),
    ...['/dashboard', '/keys', '/usage', '/profile', '/tickets', '/custom/:id', '/batch-image', '/available-channels', '/monitor', '/subscriptions', '/purchase', '/orders', '/redeem', '/affiliate', '/settings/mcp'].map(path => ({ path, component })),
    ...['/admin/dashboard', '/admin/accounts', '/admin/settings', '/admin/ops', '/admin/users', '/admin/groups', '/admin/subscriptions', '/admin/plugins', '/admin/announcements', '/admin/proxies', '/admin/redeem', '/admin/promo-codes', '/admin/usage', '/admin/tickets', '/admin/audit-logs'].map(path => ({ path, component, meta: { requiresAdmin: true } })),
  ]) })
  const nav = useWorkspaceNavigationStore(); nav.syncUser(authState.user?.id ?? null)
  router.afterEach(to => nav.remember(to))
  await router.push(path)
  const wrapper = mount(AppSidebar, { attachTo: document.body, global: { plugins: [pinia, router], stubs: { VersionBadge: true } } })
  mounted.push(wrapper)
  await flushPromises()
  return { wrapper, router, nav }
}

describe('workspace sidebar interactions', () => {
  afterEach(() => { for (const wrapper of mounted.splice(0)) wrapper.unmount() })
  beforeEach(() => {
    document.body.innerHTML = ''; sessionStorage.clear()
    appState.sidebarCollapsed = false; appState.mobileOpen = false; appState.backendModeEnabled = false
    appState.cachedPublicSettings = { custom_menu_items: [], payment_enabled: true }
    adminState.customMenuItems = []
    authState.isAdmin = true; authState.isSimpleMode = false; authState.user = { id: 7 }
  })

  it('shows workbench management above personal generation, with only the exact active management entry selected', async () => {
    const { wrapper } = await setup('/admin/workbench/tasks')
    expect(wrapper.text()).toContain('nav.workbenchManagement')
    expect(wrapper.text()).toContain('nav.myCreations')
    expect(wrapper.find('.sidebar-nav a[href="/admin/workbench/tasks"]').classes()).toContain('sidebar-link-active')
    expect(wrapper.find('.sidebar-nav a[href="/admin/workbench"]').classes()).not.toContain('sidebar-link-active')
    expect(wrapper.find('a[href="/workbench/image"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/admin/accounts"]').exists()).toBe(false)
    expect(wrapper.find('a[href="/keys"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('preserves personal context when an administrator switches areas and returns to the last personal console page', async () => {
    const { wrapper, router } = await setup('/usage?period=month')
    await wrapper.find('.user-area-switcher a[href="/workbench/image"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/workbench/image')
    const consoleLink = wrapper.find('.user-area-switcher a[href="/usage?period=month"]')
    expect(consoleLink.exists()).toBe(true)
    await consoleLink.trigger('click'); await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/usage?period=month')
    wrapper.unmount()
  })

  it('uses management defaults while retaining the existing console menu', async () => {
    const { wrapper, router } = await setup('/admin/dashboard')
    expect(wrapper.find('a[href="/admin/accounts"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/keys"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/image-studio"]').exists()).toBe(false)
    await wrapper.find('.user-area-switcher a[href="/admin/workbench"]').trigger('click'); await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/workbench')
    wrapper.unmount()
  })

  it('shows only image generation to ordinary workbench users', async () => {
    authState.isAdmin = false
    const { wrapper } = await setup('/workbench/image')
    expect(wrapper.find('a[href="/workbench/image"]').exists()).toBe(true)
    expect(wrapper.find('a[href^="/admin/"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('nav.workbenchManagement')
    wrapper.unmount()
  })

  it('keeps modified link clicks native and clears the same-user return memory on logout', async () => {
    const { wrapper, router, nav } = await setup('/admin/dashboard')
    expect(nav.paths['admin:console']).toBe('/admin/dashboard')
    const link = wrapper.find('.user-area-switcher a[href="/admin/workbench"]')
    // Suppress jsdom's unsupported native new-tab navigation after Vue's link
    // handler has handled the modifier; the current router must stay put.
    link.element.addEventListener('click', event => event.preventDefault(), { once: true })
    await link.trigger('click', { ctrlKey: true })
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
    authState.user = null
    await flushPromises()
    expect(nav.paths).toEqual({})
    authState.user = { id: 7 }
    await flushPromises()
    expect(nav.paths).toEqual({})
    wrapper.unmount()
  })

  it('hands off sidebar scroll ownership when custom menu context changes without unmounting', async () => {
    adminState.customMenuItems = [{ id: 'ops', label: 'Operations', visibility: 'admin', sort_order: 0, icon_svg: '' }]
    appState.cachedPublicSettings.custom_menu_items = [{ id: 'help', label: 'Help', visibility: 'user', sort_order: 0, icon_svg: '' }]
    const { wrapper, router, nav } = await setup('/custom/ops')
    const element = wrapper.get('.sidebar-nav').element as HTMLElement
    element.scrollTop = 180
    nav.saveScroll('personal:console', 32)
    await router.push('/custom/help'); await flushPromises()
    expect(wrapper.get('.sidebar-nav').element).toBe(element)
    expect(nav.scrollPositions['admin:console']).toBe(180)
    expect(element.scrollTop).toBe(32)
    element.scrollTop = 66
    await router.push('/custom/ops'); await flushPromises()
    expect(nav.scrollPositions['personal:console']).toBe(66)
    expect(element.scrollTop).toBe(180)
    element.scrollTop = 210
    wrapper.unmount()
    expect(nav.scrollPositions['admin:console']).toBe(210)
    expect(nav.scrollPositions['personal:console']).toBe(66)
  })

  it('reuses the shared keyboard-accessible picker on a collapsed desktop rail', async () => {
    appState.sidebarCollapsed = true
    const { wrapper, router } = await setup('/admin/dashboard')
    const trigger = wrapper.find('.user-area-switcher button')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('keydown', { key: 'ArrowDown' }); await flushPromises()
    const listbox = document.querySelector('[role="listbox"]')
    expect(listbox).not.toBeNull()
    listbox!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    listbox!.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/workbench')
    await vi.waitFor(() => expect(document.querySelector('[role="listbox"]')).toBeNull())
    wrapper.unmount()
  })

  it('shows full labels in the mobile drawer despite a collapsed desktop preference', async () => {
    const matchMedia = window.matchMedia
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ ...matchMedia(query), matches: false }))
    appState.sidebarCollapsed = true; appState.mobileOpen = true
    const { wrapper } = await setup('/workbench/image')
    expect(wrapper.find('aside').classes()).toContain('w-[220px]')
    expect(wrapper.findAll('.user-area-switcher a')).toHaveLength(2)
    wrapper.unmount()
    vi.restoreAllMocks()
  })
})
