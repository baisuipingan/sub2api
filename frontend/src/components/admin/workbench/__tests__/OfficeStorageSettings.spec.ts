import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import type { OfficeStorageConfigResponse } from '@/api/admin/backup'
import OfficeStorageSettings from '../OfficeStorageSettings.vue'

const { getConfig, saveConfig } = vi.hoisted(() => ({
  getConfig: vi.fn(),
  saveConfig: vi.fn(),
}))
vi.mock('@/api', () => ({ adminAPI: { backup: { getOfficeStorageConfig: getConfig, updateOfficeStorageConfig: saveConfig } } }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: (action: () => unknown) => action() }),
  isStepUpCancelled: () => false,
  isStepUpBlocked: () => false,
}))

let wrapper: ReturnType<typeof mount> | undefined
afterEach(() => { wrapper?.unmount() })

it('uses the shared keyboard-accessible source menu and saves the selected storage source', async () => {
  const config: OfficeStorageConfigResponse = {
    config: { source: 'backup', bucket: 'private-office', endpoint: '', region: '', access_key_id: '', force_path_style: false },
    secret_configured: false,
    configured: true,
    target_in_use: true,
  }
  getConfig.mockResolvedValue(config)
  saveConfig.mockResolvedValue({ ...config, config: { ...config.config, source: 'images' } })
  wrapper = mount(OfficeStorageSettings, { attachTo: document.body, global: { stubs: { TotpStepUpDialog: true } } })
  const trigger = wrapper.get<HTMLButtonElement>('#office-storage-source').element
  expect(trigger.disabled).toBe(true)
  await flushPromises()
  expect(trigger.disabled).toBe(false)

  async function press(key: string) {
    document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
    await nextTick()
  }
  trigger.focus()
  await press('ArrowDown')
  await nextTick()
  const listbox = document.querySelector('[role="listbox"]')
  expect(document.activeElement).toBe(listbox)
  expect(listbox?.querySelectorAll('[role="option"]')).toHaveLength(3)
  await press('ArrowDown')
  await press('Enter')

  expect(trigger.textContent).toContain('officeStorage.images')
  expect(document.activeElement).toBe(trigger)
  await wrapper.get('[data-testid="office-storage-save"]').trigger('click')
  await flushPromises()
  expect(saveConfig).toHaveBeenCalledWith({ ...config.config, source: 'images' })
})
