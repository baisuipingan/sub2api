import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DesktopDownloadsView from '../DesktopDownloadsView.vue'

const { getDownloads, rangeDownload } = vi.hoisted(() => ({ getDownloads: vi.fn(), rangeDownload: vi.fn() }))
vi.mock('@/api/desktopDownloads', () => ({ getDesktopDownloads: getDownloads }))
vi.mock('@/utils/acceleratedReleaseDownload', () => ({ downloadReleaseInRanges: rangeDownload }))
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

beforeEach(() => {
  vi.clearAllMocks()
  getDownloads.mockResolvedValue(downloads)
  rangeDownload.mockResolvedValue(undefined)
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(window, 'showSaveFilePicker', { configurable: true, value: undefined })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })

function enableSavePicker() {
  const writer = { truncate: vi.fn(), write: vi.fn(), close: vi.fn(), abort: vi.fn() }
  const createWritable = vi.fn().mockResolvedValue(writer)
  const picker = vi.fn().mockResolvedValue({ createWritable })
  Object.defineProperty(window, 'showSaveFilePicker', { configurable: true, value: picker })
  return { writer, picker, createWritable }
}

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
    expect(wrapper.findAll('[data-platform] button[disabled]')).toHaveLength(3)
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

  it('shows a published ARM Mac release and leaves unpublished Intel downloads disabled', async () => {
    getDownloads.mockResolvedValue({ platforms: [
      { platform: 'darwin-arm64', available: true, version: '2.10.4', url: 'https://download.example.test/milocode/releases/2.10.4/darwin-arm64/MiloCode-2.10.4-mac-arm64.dmg', size: 23456789, format: 'dmg' },
    ] })
    const wrapper = render()
    await flushPromises()
    const mac = wrapper.get('[data-platform="darwin-arm64"]')
    expect(mac.text()).toContain('v2.10.4')
    expect(mac.text()).toContain('DMG · 22.4 MB')
    expect(mac.get('a').attributes('href')).toContain('/2.10.4/darwin-arm64/MiloCode-2.10.4-mac-arm64.dmg')
    expect(mac.get('[data-testid="mac-package-install-hint"]').text()).toBe('desktop.downloads.macDmgInstallHint')
    expect(mac.text()).not.toContain('desktop.downloads.macZipInstallHint')
    const intel = wrapper.get('[data-platform="darwin-x64"]')
    expect(intel.text()).toContain('Intel · x64')
    expect(intel.find('a').exists()).toBe(false)
    expect(intel.get('button').attributes()).toHaveProperty('disabled')
    expect(wrapper.findAll('[data-platform]')).toHaveLength(3)
  })

  it('shows an Intel Mac package only after the API publishes it', async () => {
    getDownloads.mockResolvedValue({ platforms: [
      { platform: 'darwin-x64', available: true, version: '2.10.5', url: 'https://download.example.test/milocode/releases/2.10.5/darwin-x64/MiloCode-2.10.5-mac-x64.dmg', size: 22345678, format: 'dmg' },
    ] })
    const wrapper = render()
    await flushPromises()
    const intel = wrapper.get('[data-platform="darwin-x64"]')
    expect(intel.text()).toContain('Intel · x64')
    expect(intel.text()).toContain('v2.10.5')
    expect(intel.get('a').attributes('href')).toContain('/darwin-x64/MiloCode-2.10.5-mac-x64.dmg')
    expect(intel.get('[data-testid="mac-package-install-hint"]').text()).toBe('desktop.downloads.macDmgInstallHint')
    expect(wrapper.get('[data-platform="darwin-arm64"]').find('a').exists()).toBe(false)
  })

  it('keeps ordinary download available when the browser cannot stream to a chosen file', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-platform="win32-x64"] a').attributes('href')).toBe(downloads.platforms[0].url)
    expect(wrapper.find('[data-testid="accelerated-download-win32-x64"]').exists()).toBe(false)
    expect(rangeDownload).not.toHaveBeenCalled()
  })

  it('offers acceleration as a separate user action and opens the save picker on click', async () => {
    const { picker, writer } = enableSavePicker()
    const wrapper = render()
    await flushPromises()
    const windows = wrapper.get('[data-platform="win32-x64"]')
    expect(windows.get('a').attributes('href')).toBe(downloads.platforms[0].url)
    expect(windows.get('a').text()).toContain('desktop.downloads.ordinaryDownload')
    expect(windows.get('a').classes()).toContain('btn-secondary')
    expect(windows.get('[data-testid="accelerated-download-win32-x64"]').classes()).toContain('btn-primary')
    expect(picker).not.toHaveBeenCalled()
    expect(rangeDownload).not.toHaveBeenCalled()
    await windows.get('[data-testid="accelerated-download-win32-x64"]').trigger('click')
    await flushPromises()
    expect(picker).toHaveBeenCalledWith({ suggestedName: 'MiloCode.exe' })
    expect(rangeDownload).toHaveBeenCalledWith(expect.objectContaining({ url: downloads.platforms[0].url, size: 1024 * 1024, writer }))
    expect(wrapper.get('[data-testid="accelerated-download-complete"]').text()).toBe('desktop.downloads.downloadSaved')
  })

  it('shows progress and lets the user cancel without starting another download', async () => {
    enableSavePicker()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    rangeDownload.mockImplementation(({ signal, onProgress }) => new Promise((_resolve, reject) => {
      onProgress({ writtenBytes: 512 * 1024, totalBytes: 1024 * 1024 })
      signal.addEventListener('abort', () => reject(signal.reason), { once: true })
    }))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="accelerated-download-win32-x64"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="accelerated-download-progress"]').text()).toContain('50% · 0.5 / 1.0 MB')
    expect(wrapper.get('progress').attributes('value')).toBe(String(512 * 1024))
    expect(wrapper.get('[data-testid="accelerated-download-win32-x64"]').attributes()).toHaveProperty('disabled')
    await wrapper.get('[data-testid="cancel-accelerated-download"]').trigger('click')
    await flushPromises()
    expect(rangeDownload.mock.calls[0][0].signal.aborted).toBe(true)
    expect(wrapper.find('[data-testid="accelerated-download-progress"]').exists()).toBe(false)
    expect(click).not.toHaveBeenCalled()
  })

  it('silently starts the ordinary direct download after acceleration fails', async () => {
    enableSavePicker()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    rangeDownload.mockRejectedValue(new Error('Private diagnostic CORS detail'))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="accelerated-download-win32-x64"]').trigger('click')
    await flushPromises()
    expect(click).toHaveBeenCalledOnce()
    expect(wrapper.find('[data-testid="accelerated-download-progress"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Private diagnostic')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('does not start a download if the user cancels the save picker', async () => {
    const { picker } = enableSavePicker()
    picker.mockRejectedValue(new DOMException('User cancelled', 'AbortError'))
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="accelerated-download-win32-x64"]').trigger('click')
    await flushPromises()
    expect(rangeDownload).not.toHaveBeenCalled()
    expect(click).not.toHaveBeenCalled()
  })

  it('cancels active acceleration when leaving the page without starting ordinary download', async () => {
    enableSavePicker()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
    rangeDownload.mockImplementation(({ signal }) => new Promise((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(signal.reason), { once: true })
    }))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="accelerated-download-win32-x64"]').trigger('click')
    await flushPromises()
    wrapper.unmount()
    await flushPromises()
    expect(rangeDownload.mock.calls[0][0].signal.aborted).toBe(true)
    expect(click).not.toHaveBeenCalled()
  })

  it('explains how to install older ZIP-only releases without calling them a DMG', async () => {
    getDownloads.mockResolvedValue({ platforms: [
      { platform: 'darwin-arm64', available: true, version: '2.10.3', url: 'https://download.example.test/milocode/releases/2.10.3/darwin-arm64/MiloCode.zip', format: 'zip' },
    ] })
    const wrapper = render()
    await flushPromises()
    const mac = wrapper.get('[data-platform="darwin-arm64"]')
    expect(mac.text()).toContain('ZIP')
    expect(mac.text()).not.toContain('ZIP ·')
    expect(mac.get('a').attributes('href')).toContain('/2.10.3/darwin-arm64/MiloCode.zip')
    expect(mac.get('[data-testid="mac-package-install-hint"]').text()).toBe('desktop.downloads.macZipInstallHint')
    expect(mac.text()).not.toContain('desktop.downloads.macDmgInstallHint')
  })

})
