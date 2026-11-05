import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { state, clearData, exportData, importData } = vi.hoisted(() => ({
  state: {
    showSettings: true,
    settings: {
      clearInputAfterSubmit: false,
      enterSubmit: true,
      taskCompletionNotification: false,
    },
    setShowSettings: vi.fn(),
    setSettings: vi.fn(),
  },
  clearData: vi.fn(),
  exportData: vi.fn(),
  importData: vi.fn(),
}))

vi.mock('../../stores/store', () => ({ storeState: state, clearData, exportData, importData }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import StudioSettingsModal from '../StudioSettingsModal.vue'
import StudioCheckbox from '../StudioCheckbox.vue'

enableAutoUnmount(afterEach)

function mountModal() {
  return mount(StudioSettingsModal, { global: { stubs: { StudioIcon: true } } })
}

beforeEach(() => vi.clearAllMocks())

describe('StudioSettingsModal', () => {
  it('only offers useful creation preferences and local data controls', () => {
    const wrapper = mountModal()

    expect(wrapper.text()).toContain('imageStudio.settings.prefClearInput')
    expect(wrapper.text()).toContain('imageStudio.settings.prefNotify')
    expect(wrapper.findAllComponents(StudioCheckbox)).toHaveLength(2)
    expect(wrapper.text()).not.toContain('imageStudio.settings.prefEnterSubmit')
    expect(wrapper.text()).not.toContain('imageStudio.settings.connectionTitle')
    expect(wrapper.text()).not.toContain('imageStudio.settings.refreshKey')
  })

  it('keeps the remaining preference controls functional', async () => {
    const wrapper = mountModal()
    await wrapper.findAll('input[type="checkbox"]')[0].setValue(true)
    await wrapper.findAll('input[type="checkbox"]')[1].setValue(true)

    expect(state.setSettings).toHaveBeenCalledWith({ clearInputAfterSubmit: true })
    expect(state.setSettings).toHaveBeenCalledWith({ taskCompletionNotification: true })
    expect(state.setSettings).not.toHaveBeenCalledWith(expect.objectContaining({ enterSubmit: expect.anything() }))
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
})
