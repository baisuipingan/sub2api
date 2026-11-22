import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import WorkbenchSearchSettings from '../WorkbenchSearchSettings.vue'
import { setCurrentBillingCurrencySettings } from '@/utils/billingCurrency'

const { load, save, testSearch, stepUpRun } = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  testSearch: vi.fn(),
  stepUpRun: vi.fn((action: () => unknown) => action()),
}))
vi.mock('@/api/admin/workbench', () => ({
  getWorkbenchSearchConfig: load,
  updateWorkbenchSearchConfig: save,
  testWorkbenchSearch: testSearch,
}))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpCancelled: () => false,
  isStepUpBlocked: () => false,
  stepUpBlockReason: () => '',
}))

function mountSettings() {
  return mount(WorkbenchSearchSettings, { global: { stubs: {
    TotpStepUpDialog: true,
    Select: {
      props: ['modelValue', 'options'], emits: ['update:modelValue'],
      template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
    },
  } } })
}
const wrappers: ReturnType<typeof mountSettings>[] = []

describe('Workbench search provider configuration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    load.mockResolvedValue({ provider: 'brave', api_key_configured: true })
  })
  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    setCurrentBillingCurrencySettings()
  })

  it('requires a fresh key when changing providers and clears the input after saving', async () => {
    const wrapper = mountSettings()
    wrappers.push(wrapper)
    await flushPromises()
    expect((wrapper.get('#workbench-search-key').element as HTMLInputElement).value).toBe('')
    await wrapper.get('#workbench-search-provider').setValue('deepseek')
    const saveButton = wrapper.get('.btn-primary')
    expect(saveButton.attributes('disabled')).toBeDefined()
    await wrapper.get('#workbench-search-key').setValue('new-test-key')
    save.mockResolvedValue({ provider: 'deepseek', api_key_configured: true })
    await saveButton.trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledOnce()
    expect(save).toHaveBeenCalledWith({ provider: 'deepseek', api_key: 'new-test-key', price_per_1000_usd: 0 })
    expect((wrapper.get('#workbench-search-key').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('#workbench-search-key').attributes('type')).toBe('password')
  })

  it('updates DeepSeek price without replacing its key and permits making search free again', async () => {
    load.mockResolvedValue({ provider: 'deepseek', api_key_configured: true, price_per_1000_usd: 8 })
    const wrapper = mountSettings()
    wrappers.push(wrapper)
    await flushPromises()
    const input = wrapper.get('#workbench-search-price')
    const saveButton = wrapper.get('.btn-primary')
    expect((input.element as HTMLInputElement).value).toBe('8')
    expect(saveButton.attributes('disabled')).toBeDefined()
    await input.setValue('-1')
    expect(saveButton.attributes('disabled')).toBeDefined()
    await input.setValue('0')
    save.mockResolvedValue({ provider: 'deepseek', api_key_configured: true, price_per_1000_usd: 0 })
    await saveButton.trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith({ provider: 'deepseek', price_per_1000_usd: 0 })
    expect(saveButton.attributes('disabled')).toBeDefined()
  })

  it('edits the search price in the configured currency and saves the USD equivalent', async () => {
    setCurrentBillingCurrencySettings({ billing_display_currency: 'CNY', billing_usd_to_cny: 6.8 })
    load.mockResolvedValue({ provider: 'deepseek', api_key_configured: true, price_per_1000_usd: 5 })
    const wrapper = mountSettings()
    wrappers.push(wrapper)
    await flushPromises()
    const input = wrapper.get<HTMLInputElement>('#workbench-search-price')
    expect(input.element.value).toBe('34')
    expect(wrapper.get('.btn-primary').attributes('disabled')).toBeDefined()

    await input.setValue('40.8')
    save.mockResolvedValue({ provider: 'deepseek', api_key_configured: true, price_per_1000_usd: 6 })
    await wrapper.get('.btn-primary').trigger('click')
    await flushPromises()
    expect(save).toHaveBeenCalledWith({ provider: 'deepseek', price_per_1000_usd: 6 })
    expect(input.element.value).toBe('40.8')
  })

  it('tests the saved provider and renders results without activating unsafe links', async () => {
    const wrapper = mountSettings()
    wrappers.push(wrapper)
    await flushPromises()
    await wrapper.get('#workbench-search-query').setValue('API docs')
    testSearch.mockResolvedValue({
      provider: 'brave', query: 'API docs', results: [
        { title: 'Official docs', url: 'https://example.test/docs', snippet: 'Documentation' },
        { title: 'Invalid URL', url: 'javascript:alert(1)', snippet: '' },
      ],
    })
    await wrapper.get('.btn-secondary').trigger('click')
    await flushPromises()
    expect(stepUpRun).toHaveBeenCalledOnce()
    expect(testSearch).toHaveBeenCalledWith('API docs')
    expect(wrapper.findAll('a').map(link => link.attributes('href'))).toEqual(['https://example.test/docs'])
  })
})
