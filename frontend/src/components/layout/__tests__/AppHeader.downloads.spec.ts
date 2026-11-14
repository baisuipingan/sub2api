import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import AppHeader from '../AppHeader.vue'
import { desktopDownloadRoutes } from '@/router/desktopDownloadRoutes'
import { withWorkspaceMeta } from '@/router/workspace'

const appState = reactive({ backendModeEnabled: false, docUrl: '', contactInfo: '', cachedPublicSettings: {}, toggleMobileSidebar: vi.fn() })
const authState = reactive({ isAdmin: false, isSimpleMode: false, user: { id: 7, role: 'user', email: 'test@example.com' } as { id: number; role: string; email: string } | null })
const mounted: VueWrapper[] = []

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores', () => ({
  useAppStore: () => appState,
  useAuthStore: () => authState,
  useOnboardingStore: () => ({ replay: vi.fn() }),
}))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => ({ customMenuItems: [] }) }))
vi.mock('@/utils/featureFlags', async (importOriginal) => ({ ...await importOriginal<typeof import('@/utils/featureFlags')>(), isFeatureFlagEnabled: () => false }))
vi.mock('@/utils/billingCurrency', () => ({ formatCurrentBillingAmount: () => '$0.00' }))

async function setup(path: string) {
  const component = { template: '<div />' }
  const router = createRouter({ history: createMemoryHistory(), routes: withWorkspaceMeta([
    ...desktopDownloadRoutes.map(route => ({ ...route, component })),
    { path: '/dashboard', component },
    { path: '/workbench/image', component, meta: { workspaceArea: 'workbench' } },
    { path: '/admin/dashboard', component, meta: { requiresAdmin: true } },
    { path: '/admin/workbench', component, meta: { requiresAdmin: true, workspaceArea: 'workbench' } },
    { path: '/keys', component },
    { path: '/profile', component },
  ]) })
  await router.push(path)
  const wrapper = mount(AppHeader, { global: { plugins: [router], stubs: { LocaleSwitcher: true, AnnouncementBell: { template: '<button data-testid="announcement-bell" />' }, SubscriptionProgressMini: true, Icon: true } } })
  mounted.push(wrapper)
  return { wrapper, router }
}

describe('header MiloCode download entry', () => {
  beforeEach(() => {
    appState.backendModeEnabled = false
    authState.isAdmin = false
    authState.user = { id: 7, role: 'user', email: 'test@example.com' }
  })
  afterEach(() => { for (const wrapper of mounted.splice(0)) wrapper.unmount() })

  it.each([
    ['/dashboard', false, '/downloads'],
    ['/workbench/image', false, '/workbench/downloads'],
    ['/admin/dashboard', true, '/admin/downloads'],
    ['/admin/workbench', true, '/admin/workbench/downloads'],
    ['/dashboard', true, '/downloads'],
  ] as const)('opens downloads in the current area and role from %s (admin=%s)', async (path, admin, destination) => {
    authState.isAdmin = admin
    const { wrapper, router } = await setup(path)
    const link = wrapper.get('[data-testid="milocode-download-header"]')
    expect(link.attributes('href')).toBe(destination)
    expect(link.text()).toContain('MiloCode')
    expect(link.attributes('aria-label')).toBe('nav.milocodeDownload')
    expect(link.get('[data-testid="milocode-download-icon"]').attributes('src')).toContain('milocode-icon.png')
    expect(link.element.nextElementSibling?.getAttribute('data-testid')).toBe('announcement-bell')
    await link.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(destination)
    expect(router.currentRoute.value.meta.requiresAdmin).toBe(destination.startsWith('/admin/'))
  })

  it('keeps the entry available to administrators in backend-only mode', async () => {
    appState.backendModeEnabled = true
    authState.isAdmin = true
    const { wrapper } = await setup('/admin/dashboard')
    expect(wrapper.find('[data-testid="milocode-download-header"]').exists()).toBe(true)
  })

  it('preserves backend-only access restrictions for ordinary users', async () => {
    appState.backendModeEnabled = true
    const { wrapper } = await setup('/dashboard')
    expect(wrapper.find('[data-testid="milocode-download-header"]').exists()).toBe(false)
  })

  it('hides the entry without a signed-in user', async () => {
    authState.user = null
    const { wrapper } = await setup('/dashboard')
    expect(wrapper.find('[data-testid="milocode-download-header"]').exists()).toBe(false)
  })
})
