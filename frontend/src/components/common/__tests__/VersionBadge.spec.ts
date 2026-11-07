import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import VersionBadge from '../VersionBadge.vue'
import { useAppStore, useAuthStore } from '@/stores'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

enableAutoUnmount(afterEach)

let pinia: ReturnType<typeof createPinia>

function mountBadge(buildType: string, isAdmin = true) {
  const authStore = useAuthStore()
  authStore.user = {
    id: 1,
    username: 'version-test',
    email: 'version@example.com',
    role: isAdmin ? 'admin' : 'user',
    balance: 0,
    concurrency: 1,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: false,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '',
    updated_at: ''
  }
  const appStore = useAppStore()
  appStore.versionLoaded = true
  appStore.buildType = buildType
  appStore.currentVersion = '0.2.19-ds.60'
  appStore.upstreamVersion = '0.2.19'
  appStore.latestVersion = '0.2.20'
  appStore.hasUpdate = true
  const fetchVersion = vi.spyOn(appStore, 'fetchVersion')

  const wrapper = mount(VersionBadge, {
    props: { version: '0.2.19-ds.60' },
    global: { plugins: [pinia], stubs: { Icon: true } }
  })
  return { wrapper, appStore, fetchVersion }
}

describe('VersionBadge build selection', () => {
  beforeEach(() => {
    pinia = createPinia()
    setActivePinia(pinia)
  })

  it('shows the fork baseline and external release workflow for an admin fork build', async () => {
    const { wrapper } = mountBadge('fork')
    await wrapper.get('[data-testid="fork-version-trigger"]').trigger('click')

    expect(wrapper.text()).toContain('version.forkOfficialUpdateAvailable')
    expect(wrapper.text()).toContain('version.forkOfficialBaseline')
    expect(wrapper.text()).toContain('./scripts/fork-release.sh prepare v0.2.20')
    expect(wrapper.get('[data-testid="fork-update-disabled"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).not.toContain('version.sourceModeHint')
    expect(wrapper.text()).not.toContain('version.updateNow')
  })

  it('switches to the fork panel when version detection resolves after mounting', async () => {
    const { wrapper, appStore } = mountBadge('source')
    expect(wrapper.find('[data-testid="fork-version-trigger"]').exists()).toBe(false)

    appStore.buildType = 'fork'
    await nextTick()

    expect(wrapper.get('[data-testid="fork-version-trigger"]').exists()).toBe(true)
  })

  it.each([
    ['source', 'version.sourceModeHint', 'version.updateNow'],
    ['release', 'version.updateNow', 'version.sourceModeHint']
  ])('preserves the existing %s admin update panel', async (buildType, expected, absent) => {
    const { wrapper } = mountBadge(buildType)
    await wrapper.get('button').trigger('click')

    expect(wrapper.find('[data-testid="fork-version-trigger"]').exists()).toBe(false)
    expect(wrapper.text()).toContain(expected)
    expect(wrapper.text()).not.toContain(absent)
  })

  it('keeps fork version text static for a non-admin without fetching admin metadata', () => {
    const { wrapper, fetchVersion } = mountBadge('fork', false)

    expect(wrapper.text()).toBe('v0.2.19-ds.60')
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.find('[data-testid="fork-version-trigger"]').exists()).toBe(false)
    expect(fetchVersion).not.toHaveBeenCalled()
  })
})
