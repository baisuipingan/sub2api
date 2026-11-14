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
    expect(wrapper.get('[data-testid="milocode-download-badge-icon"]').attributes('src')).toContain('milocode-icon.png')
    expect(wrapper.get('[data-testid="milocode-download-product-icon"]').attributes('src')).toContain('milocode-icon.png')
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
    expect(wrapper.findAll('[data-platform] a')).toHaveLength(0)
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
    expect(wrapper.find('[data-platform] a').exists()).toBe(true)
    expect(getDownloads).toHaveBeenCalledTimes(2)
  })

  it('never creates a link for a non-HTTPS response', async () => {
    getDownloads.mockResolvedValue({ platforms: [{ ...downloads.platforms[0], url: 'javascript:alert(1)' }] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('[data-platform] a')).toHaveLength(0)
  })

  it('keeps the Mac installation helper available independently of release metadata', async () => {
    getDownloads.mockRejectedValue(new Error('offline'))
    const wrapper = render()
    await flushPromises()
    const helper = wrapper.get('[data-testid="mac-helper-download"]')
    expect(helper.attributes('href')).toBe('/downloads/MiloCode-macOS-Helper.command')
    expect(helper.attributes('download')).toBe('MiloCode-macOS-Helper.command')
    const help = wrapper.get('[data-testid="mac-install-help"]')
    expect(help.text()).toContain('desktop.downloads.macUnsigned')
    expect(help.text()).toContain('bash ~/Downloads/MiloCode-macOS-Helper.command')
    expect(help.text()).toContain('desktop.downloads.macHelperScope')
  })

  it('shows the real Mac release without suggesting an Intel package', async () => {
    getDownloads.mockResolvedValue({ platforms: [
      { platform: 'darwin-arm64', available: true, version: '2.10.4', url: 'https://download.example.test/milocode/releases/2.10.4/darwin-arm64/MiloCode.zip', format: 'zip' },
    ] })
    const wrapper = render()
    await flushPromises()
    const mac = wrapper.get('[data-platform="darwin-arm64"]')
    expect(mac.text()).toContain('v2.10.4')
    expect(mac.text()).toContain('ZIP')
    expect(mac.text()).not.toContain('ZIP ·')
    expect(mac.get('a').attributes('href')).toContain('/2.10.4/darwin-arm64/')
    expect(wrapper.text()).not.toContain('Intel')
    expect(wrapper.findAll('[data-platform]')).toHaveLength(2)
  })

})
