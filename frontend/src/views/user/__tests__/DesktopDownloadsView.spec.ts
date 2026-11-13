import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DesktopDownloadsView from '../DesktopDownloadsView.vue'

const { getDownloads } = vi.hoisted(() => ({ getDownloads: vi.fn() }))
vi.mock('@/api/desktopDownloads', () => ({ getDesktopDownloads: getDownloads }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
const wrappers: ReturnType<typeof mount>[] = []
function render() {
  const wrapper = mount(DesktopDownloadsView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' } } } })
  wrappers.push(wrapper)
  return wrapper
}
const downloads = { platforms: [
  { platform: 'win32-x64', available: true, version: '2.10.3', url: 'https://download.example.test/milocode/releases/2.10.3/win32-x64/MiloCode.exe', size: 1024 * 1024, format: 'exe' },
  { platform: 'darwin-arm64', available: false },
] }

beforeEach(() => { vi.clearAllMocks(); getDownloads.mockResolvedValue(downloads) })
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()) })

describe('MiloCode download page', () => {
  it('shows published platform versions and links directly to the release host', async () => {
    const wrapper = render()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    await flushPromises()
    const windows = wrapper.get('[data-platform="win32-x64"]')
    expect(windows.text()).toContain('v2.10.3')
    expect(windows.text()).toContain('EXE · 1.0 MB')
    expect(windows.get('a').attributes('href')).toBe(downloads.platforms[0].url)
    expect(windows.get('a').attributes()).toHaveProperty('download')
    const mac = wrapper.get('[data-platform="darwin-arm64"]')
    expect(mac.text()).toContain('Apple Silicon · ARM64')
    expect(mac.find('a').exists()).toBe(false)
    expect(mac.get('button').attributes()).toHaveProperty('disabled')
  })

  it('does not guess a package URL before a release exists', async () => {
    getDownloads.mockResolvedValue({ platforms: [] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('a')).toHaveLength(0)
    expect(wrapper.findAll('[data-platform] button[disabled]')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('undefined')
  })

  it('reports a safe load error and lets the user retry', async () => {
    getDownloads.mockRejectedValueOnce(new Error('internal secret details'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('desktop.downloads.loadFailed')
    expect(wrapper.text()).not.toContain('secret')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('a').exists()).toBe(true)
    expect(getDownloads).toHaveBeenCalledTimes(2)
  })

  it('never creates a link for a non-HTTPS response', async () => {
    getDownloads.mockResolvedValue({ platforms: [{ ...downloads.platforms[0], url: 'javascript:alert(1)' }] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('a')).toHaveLength(0)
  })
})
