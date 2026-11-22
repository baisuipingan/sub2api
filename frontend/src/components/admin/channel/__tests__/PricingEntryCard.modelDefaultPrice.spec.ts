import { flushPromises, mount, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { PricingFormEntry } from '../types'
import { formIntervalsToAPI, mTokToPerToken } from '../types'
import channelsAPI from '@/api/admin/channels'
import { setCurrentBillingCurrencySettings } from '@/utils/billingCurrency'

vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing: vi.fn() },
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const getModelDefaultPricing = vi.mocked(channelsAPI.getModelDefaultPricing)

function createEntry(models: string[] = []): PricingFormEntry {
  return {
    models,
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    ultrafast_multiplier: null,
    reasoning_effort_multipliers: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: {
      timezone: 'Asia/Shanghai',
      weekdays_only: false,
      periods: [],
    },
  }
}

function addModels(entry: PricingFormEntry, models: string[]) {
  const wrapper = shallowMount(PricingEntryCard, { props: { entry } })
  wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', models)
  return wrapper
}

const emittedEntries = (wrapper: ReturnType<typeof shallowMount>) =>
  (wrapper.emitted('update') ?? []).map(e => e[0] as PricingFormEntry)

describe('PricingEntryCard default price auto-fill', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getModelDefaultPricing.mockResolvedValue({ found: true, input_price: 3e-6, output_price: 15e-6 })
  })

  afterEach(() => setCurrentBillingCurrencySettings())

  it('auto-fills the default price when a model is added to an empty entry', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).toHaveBeenCalledTimes(1)
    expect(getModelDefaultPricing).toHaveBeenCalledWith('claude-opus-4-8')
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-8'],
      input_price: 3,
      output_price: 15,
    })
  })

  it('does not overwrite prices when appending a model to an entry that already has one', async () => {
    const entry = createEntry(['claude-opus-4-7'])
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
    })
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBeNull()
      expect(emitted.output_price).toBeNull()
      expect(emitted.cache_write_price).toBeNull()
      expect(emitted.cache_read_price).toBeNull()
    }
  })

  it('does not auto-fill a shared price when several models are added to an empty entry at once', async () => {
    const wrapper = addModels(createEntry(), ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(emittedEntries(wrapper).at(-1)).toMatchObject({
      models: ['claude-opus-4-7', 'claude-opus-4-8'],
      input_price: null,
      output_price: null,
    })
  })

  it('keeps manual prices untouched when a model is appended', async () => {
    const entry = { ...createEntry(['claude-opus-4-7']), input_price: '1.5' }
    const wrapper = addModels(entry, ['claude-opus-4-7', 'claude-opus-4-8'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    for (const emitted of emittedEntries(wrapper)) {
      expect(emitted.input_price).toBe('1.5')
    }
  })

  it('displays official and interval prices in CNY, then emits API-compatible USD values', async () => {
    setCurrentBillingCurrencySettings({ billing_display_currency: 'CNY', billing_usd_to_cny: 7.2 })
    const entry = createEntry()
    entry.intervals = [{
      min_tokens: 128000, max_tokens: null, tier_label: '', sort_order: 0,
      input_price: 2, output_price: null, cache_write_price: null, cache_read_price: null,
      input_multiplier: null, output_multiplier: null, cache_write_multiplier: null,
      cache_read_multiplier: null, per_request_price: null,
    }]
    const wrapper = mount(PricingEntryCard, { props: { entry, useSiteCurrency: true }, global: { stubs: { ModelTagInput: true } } })
    wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', ['example-model'])
    await flushPromises()
    const filled = emittedEntries(wrapper).at(-1)!
    await wrapper.setProps({ entry: filled })
    expect(wrapper.text()).toContain('¥/MTok')
    expect(wrapper.get<HTMLInputElement>('.pricing-default-grid input').element.value).toBe('21.6')
    expect(wrapper.findAll<HTMLInputElement>('.pricing-interval-grid input').at(2)!.element.value).toBe('14.4')

    await wrapper.get('.pricing-default-grid input').setValue('36')
    const edited = emittedEntries(wrapper).at(-1)!
    expect(mTokToPerToken(edited.input_price)).toBe(5e-6)
    await wrapper.setProps({ entry: edited })
    await wrapper.findAll('.pricing-interval-grid input').at(2)!.setValue('25.2')
    expect(formIntervalsToAPI(emittedEntries(wrapper).at(-1)!.intervals)[0].input_price).toBe(3.5e-6)
    wrapper.unmount()
  })

  it('converts per-image amounts and preserves blank and zero', async () => {
    setCurrentBillingCurrencySettings({ billing_display_currency: 'CNY', billing_usd_to_cny: 7.2 })
    const entry = { ...createEntry(), billing_mode: 'image' as const, per_request_price: .025 }
    const wrapper = mount(PricingEntryCard, { props: { entry, useSiteCurrency: true } })
    const amount = wrapper.get<HTMLInputElement>('input[type="number"]')
    expect(amount.element.value).toBe('0.18')
    expect(wrapper.text()).toContain('¥/admin.channels.form.priceUnitImage')
    await amount.setValue('0')
    expect(emittedEntries(wrapper).at(-1)?.per_request_price).toBe(0)
    await amount.setValue('')
    expect(emittedEntries(wrapper).at(-1)?.per_request_price).toBeNull()
    wrapper.unmount()
  })
})
