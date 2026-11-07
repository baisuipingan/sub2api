import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { state, clearData, exportData, importData, importLegacyHistory, refreshGalleryStats, retryFailedGalleryTasks, setGallerySyncPaused, getLegacyTasks, requestPermission, translate } = vi.hoisted(() => ({
  state: {
    showSettings: true,
    galleryUserId: null as number | null,
    galleryEnabled: false,
    galleryInitialized: false,
    galleryStats: null as null | { tasks: number; done_tasks: number; failed_tasks: number; output_images: number; assets: number; used_bytes: number; max_tasks: number; max_assets: number; max_bytes: number },
    gallerySyncPaused: false,
    gallerySyncStatus: {} as Record<string, string>,
    galleryImportProgress: null as null | { total: number; processed: number; imported: number; skipped: number },
    settings: {
      clearInputAfterSubmit: false,
      enterSubmit: true,
      taskCompletionNotification: false,
    },
    setShowSettings: vi.fn(),
    setConfirmDialog: vi.fn(),
    setSettings: vi.fn(),
    showToast: vi.fn(),
  },
  clearData: vi.fn(),
  exportData: vi.fn(),
  importData: vi.fn(),
  importLegacyHistory: vi.fn(),
  refreshGalleryStats: vi.fn(), retryFailedGalleryTasks: vi.fn(), setGallerySyncPaused: vi.fn(),
  getLegacyTasks: vi.fn(),
  requestPermission: vi.fn(),
  translate: vi.fn((key: string) => key),
}))

vi.mock('../../stores/store', () => ({ storeState: state, clearData, exportData, importData, importLegacyHistory, refreshGalleryStats, retryFailedGalleryTasks, setGallerySyncPaused }))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  getLegacyTasks,
}))
vi.mock('../../lib/browserNotification', () => ({
  getBrowserNotificationSession: () => 'user:100',
  requestBrowserNotificationPermission: requestPermission,
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: translate }),
}))

import StudioSettingsModal from '../StudioSettingsModal.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'

enableAutoUnmount(afterEach)

function mountModal() {
  return mount(StudioSettingsModal, { global: { stubs: { StudioIcon: true, Teleport: true, Transition: false } } })
}

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  state.galleryUserId = null
  state.galleryEnabled = false
  state.galleryInitialized = false
  state.galleryStats = null
  state.gallerySyncPaused = false
  state.gallerySyncStatus = {}
  state.galleryImportProgress = null
  getLegacyTasks.mockResolvedValue([])
  requestPermission.mockReset().mockResolvedValue({ ok: true })
  state.settings.taskCompletionNotification = false
})

describe('StudioSettingsModal', () => {
  it('uses server totals and existing settings space for cloud progress and retry', async () => {
    state.galleryUserId = 7
    state.galleryInitialized = true
    state.galleryStats = { tasks: 11, done_tasks: 10, failed_tasks: 1, output_images: 12, assets: 15, used_bytes: 1024, max_tasks: 1000, max_assets: 500, max_bytes: 1 << 30 }
    state.gallerySyncStatus = { pending: 'pending', failed: 'error' }
    state.galleryImportProgress = { total: 11, processed: 11, imported: 10, skipped: 1 }
    const wrapper = mountModal()
    const section = wrapper.get('[data-testid="studio-cloud-gallery-settings"]')
    expect(translate).toHaveBeenCalledWith('imageStudio.gallery.taskCount', { count: 11, limit: 1000 })
    expect(section.text()).toContain('imageStudio.gallery.statusSummary')
    expect(section.text()).toContain('imageStudio.gallery.importProgress')
    await section.findAll('button').find((button) => button.text().includes('imageStudio.gallery.retryAll'))!.trigger('click')
    expect(retryFailedGalleryTasks).toHaveBeenCalledTimes(1)
    await section.findAll('button').find((button) => button.text().includes('imageStudio.gallery.pauseSync'))!.trigger('click')
    expect(setGallerySyncPaused).toHaveBeenCalledWith(true)
  })

  it('only offers useful creation preferences and local data controls', () => {
    const wrapper = mountModal()

    expect(wrapper.text()).toContain('imageStudio.settings.prefClearInput')
    expect(wrapper.text()).toContain('imageStudio.settings.prefNotify')
    expect(wrapper.findComponent(BaseDialog).exists()).toBe(true)
    expect(wrapper.findAllComponents(Toggle)).toHaveLength(2)
    expect(wrapper.text()).not.toContain('imageStudio.settings.prefEnterSubmit')
    expect(wrapper.text()).not.toContain('imageStudio.settings.connectionTitle')
    expect(wrapper.text()).not.toContain('imageStudio.settings.refreshKey')
  })

  it('keeps the remaining preference controls functional', async () => {
    const wrapper = mountModal()
    await wrapper.findAll('[role="switch"]')[0].trigger('click')
    await wrapper.findAll('[role="switch"]')[1].trigger('click')
    await flushPromises()

    expect(state.setSettings).toHaveBeenCalledWith({ clearInputAfterSubmit: true })
    expect(state.setSettings).toHaveBeenCalledWith({ taskCompletionNotification: true })
    expect(state.setSettings).not.toHaveBeenCalledWith(expect.objectContaining({ enterSubmit: expect.anything() }))
  })

  it.each([
    ['denied', 'notificationDenied'],
    ['unsupported', 'notificationUnsupported'],
    ['insecure', 'notificationInsecure'],
    ['default', 'notificationPermissionRequired'],
    ['error', 'notificationEnableFailed'],
  ])('keeps system notifications off after %s permission result', async (reason, messageKey) => {
    requestPermission.mockResolvedValueOnce({ ok: false, reason })
    const wrapper = mountModal()
    await wrapper.findAll('[role="switch"]')[1].trigger('click')
    await flushPromises()
    expect(requestPermission).toHaveBeenCalledTimes(1)
    expect(state.setSettings).toHaveBeenCalledWith({ taskCompletionNotification: false })
    expect(state.setSettings).not.toHaveBeenCalledWith({ taskCompletionNotification: true })
    expect(state.showToast).toHaveBeenCalledWith(`imageStudio.settings.${messageKey}`, 'error')
  })

  it('prevents duplicate permission prompts while the browser is asking', async () => {
    let finish!: (result: { ok: boolean }) => void
    requestPermission.mockReturnValueOnce(new Promise((resolve) => { finish = resolve }))
    const wrapper = mountModal()
    const toggle = wrapper.findAll('[role="switch"]')[1]
    await toggle.trigger('click')
    expect(toggle.attributes('disabled')).toBeDefined()
    await toggle.trigger('click')
    expect(requestPermission).toHaveBeenCalledTimes(1)
    finish({ ok: true })
    await flushPromises()
    expect(state.setSettings).toHaveBeenCalledWith({ taskCompletionNotification: true })
  })

  it('can switch notifications off without requesting browser permission', async () => {
    state.settings.taskCompletionNotification = true
    const wrapper = mountModal()
    await wrapper.findAll('[role="switch"]')[1].trigger('click')
    expect(requestPermission).not.toHaveBeenCalled()
    expect(state.setSettings).toHaveBeenCalledWith({ taskCompletionNotification: false })
  })

  it('keeps export and history cleanup available', async () => {
    const wrapper = mountModal()
    await wrapper.findAll('button').find((button) => button.text().includes('imageStudio.settings.exportBackup'))!.trigger('click')
    await wrapper.findAll('button').find((button) => button.text().includes('imageStudio.settings.clearHistory'))!.trigger('click')

    expect(exportData).toHaveBeenCalledWith({ exportConfig: true, exportTasks: true })
    expect(clearData).toHaveBeenCalledWith({ clearConfig: false, clearTasks: true })
  })

  it('keeps backup import available', async () => {
    const wrapper = mountModal()
    const input = wrapper.get('input[type="file"]')
    const file = new File(['backup'], 'images.zip', { type: 'application/zip' })
    Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
    await input.trigger('change')
    await flushPromises()

    expect(importData).toHaveBeenCalledWith(file, { importConfig: true, importTasks: true })
  })

  it('confirms local legacy import for the initialized account while the gallery is disabled', async () => {
    localStorage.setItem('auth_token', 'token-a')
    localStorage.setItem('auth_user', JSON.stringify({ id: 101 }))
    state.galleryUserId = 101
    state.galleryInitialized = true
    getLegacyTasks.mockResolvedValueOnce([{ id: 'legacy-one' }, { id: 'legacy-two' }])
    const wrapper = mountModal()
    const importButton = wrapper.findAll('button').find((button) => button.text().includes('imageStudio.settings.importLegacyHistory'))
    expect(importButton).toBeDefined()
    expect(importLegacyHistory).not.toHaveBeenCalled()

    await importButton!.trigger('click')
    await flushPromises()

    expect(translate).toHaveBeenCalledWith('imageStudio.settings.importLegacyHint', { count: 2, userId: 101 })
    expect(state.setShowSettings).toHaveBeenCalledWith(false)
    const confirmation = state.setConfirmDialog.mock.lastCall?.[0] as { action: () => Promise<void> | undefined }
    expect(confirmation).toEqual(expect.objectContaining({
      title: 'imageStudio.settings.importLegacyHistory',
      message: 'imageStudio.settings.importLegacyHint',
      confirmText: 'imageStudio.settings.importLegacyHistory',
      cancelText: 'imageStudio.common.cancel',
    }))
    expect(importLegacyHistory).not.toHaveBeenCalled()

    await confirmation.action()
    expect(importLegacyHistory).toHaveBeenCalledTimes(1)
  })

  it('does not offer legacy import before the account is initialized', () => {
    state.galleryUserId = 101
    const wrapper = mountModal()
    expect(wrapper.text()).not.toContain('imageStudio.settings.importLegacyHistory')
  })

  it.each([false, true])('does not import legacy history into a different account after confirmation when gallery enabled is %s', async (galleryEnabled) => {
    localStorage.setItem('auth_token', 'token-a')
    localStorage.setItem('auth_user', JSON.stringify({ id: 101 }))
    state.galleryUserId = 101
    state.galleryEnabled = galleryEnabled
    state.galleryInitialized = true
    const wrapper = mountModal()
    await wrapper.findAll('button').find((button) => button.text().includes('imageStudio.settings.importLegacyHistory'))!.trigger('click')
    await flushPromises()
    const confirmation = state.setConfirmDialog.mock.lastCall?.[0] as { action: () => Promise<void> | undefined }
    expect(confirmation).toBeDefined()

    localStorage.setItem('auth_token', 'token-b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 202 }))
    state.galleryUserId = 202
    await confirmation.action()
    expect(importLegacyHistory).not.toHaveBeenCalled()
  })

  it('uses common dialog close controls without submitting preference changes', async () => {
    const wrapper = mountModal()
    await wrapper.get('button[aria-label="Close modal"]').trigger('click')
    expect(state.setShowSettings).toHaveBeenCalledWith(false)
    expect(state.setSettings).not.toHaveBeenCalled()
  })

  it('closes on Escape through the existing studio modal stack', () => {
    const wrapper = mountModal()
    expect(wrapper.getComponent(BaseDialog).props('closeOnEscape')).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(state.setShowSettings).toHaveBeenCalledTimes(1)
    expect(state.setShowSettings).toHaveBeenCalledWith(false)
  })
})
