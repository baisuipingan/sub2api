import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ForkVersionPanel from '../ForkVersionPanel.vue'
import { useAppStore } from '@/stores'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

enableAutoUnmount(afterEach)

let pinia: ReturnType<typeof createPinia>

function configureStore(overrides: {
  currentVersion?: string
  upstreamVersion?: string
  latestVersion?: string
  hasUpdate?: boolean
  versionWarning?: string
  releaseInfo?: ReturnType<typeof useAppStore>['releaseInfo']
}) {
  const store = useAppStore()
  store.versionLoaded = true
  store.currentVersion = overrides.currentVersion ?? '0.2.17-ds.1'
  store.upstreamVersion = overrides.upstreamVersion ?? '0.2.17'
  store.latestVersion = overrides.latestVersion ?? '0.2.18'
  store.hasUpdate = overrides.hasUpdate ?? true
  store.versionWarning = overrides.versionWarning ?? ''
  store.buildType = 'fork'
  store.updateMode = 'external'
  store.canSelfUpdate = false
  store.releaseInfo = overrides.releaseInfo ?? {
    name: 'v0.2.18',
    body: 'Upstream changes',
    published_at: '2026-10-19T00:00:00Z',
    html_url: 'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.18'
  }
}

async function mountOpenPanel() {
  const wrapper = mount(ForkVersionPanel, {
    props: { version: 'fallback' },
    global: { plugins: [pinia], stubs: { Icon: true } }
  })
  await wrapper.get('[data-testid="fork-version-trigger"]').trigger('click')
  return wrapper
}

describe('ForkVersionPanel', () => {
  beforeEach(() => {
    pinia = createPinia()
    setActivePinia(pinia)
  })

  it('keeps the upstream notification and exposes release notes while disabling update', async () => {
    configureStore({})
    const wrapper = await mountOpenPanel()

    expect(wrapper.text()).toContain('v0.2.17-ds.1')
    expect(wrapper.text()).toContain('v0.2.17')
    expect(wrapper.text()).toContain('v0.2.18')
    expect(wrapper.text()).toContain('version.forkOfficialUpdateAvailable')
    expect(wrapper.get('[data-testid="fork-update-disabled"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('./scripts/fork-release.sh prepare v0.2.18')
    expect(wrapper.get('a').attributes('href')).toBe(
      'https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.18'
    )
  })

  it('clears the highlight when the official baseline is current', async () => {
    configureStore({ latestVersion: '0.2.17', hasUpdate: false, releaseInfo: null })
    const wrapper = await mountOpenPanel()

    expect(wrapper.get('[data-testid="fork-version-synced"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('version.forkBaselineSynced')
    expect(wrapper.find('[data-testid="fork-update-disabled"]').exists()).toBe(false)
  })

  it('shows invalid build metadata instead of a success check', async () => {
    configureStore({
      upstreamVersion: '',
      hasUpdate: false,
      versionWarning: 'fork build metadata is invalid: upstream_version is required'
    })
    const wrapper = await mountOpenPanel()

    expect(wrapper.get('[data-testid="fork-version-warning"]').text()).toContain(
      'upstream_version is required'
    )
    expect(wrapper.find('[data-testid="fork-version-synced"]').exists()).toBe(false)
  })

  it('shows detection failures without claiming the fork is current', async () => {
    configureStore({
      latestVersion: '',
      hasUpdate: false,
      releaseInfo: null,
      versionWarning: 'GitHub unavailable'
    })
    const wrapper = await mountOpenPanel()

    expect(wrapper.get('[data-testid="fork-version-warning"]').text()).toContain(
      'GitHub unavailable'
    )
    expect(wrapper.text()).toContain('version.forkVersionUnknown')
    expect(wrapper.find('[data-testid="fork-version-synced"]').exists()).toBe(false)
  })
})
