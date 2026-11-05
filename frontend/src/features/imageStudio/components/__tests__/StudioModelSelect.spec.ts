import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import type { ImageModelDefinition } from '../../lib/modelRegistry'
import StudioModelSelect from '../StudioModelSelect.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }),
}))

enableAutoUnmount(afterEach)

function model(id: string, name = 'Preset display name'): ImageModelDefinition {
  return { id, name } as ImageModelDefinition
}

function mountSelect(overrides: Record<string, unknown> = {}) {
  return mount(StudioModelSelect, {
    attachTo: document.body,
    props: {
      models: [model('gpt-image-2'), model('gpt-image-2.5')],
      selectedModelId: 'gpt-image-2',
      disabled: false,
      placeholder: 'Select a model',
      ...overrides,
    },
    global: { stubs: { Teleport: true, Transition: false } },
  })
}

describe('StudioModelSelect', () => {
  it('displays only the raw model IDs supplied by the catalog', async () => {
    const wrapper = mountSelect()
    expect(wrapper.get('button[aria-haspopup]').text()).toBe('gpt-image-2')
    await wrapper.get('button[aria-haspopup]').trigger('click')

    expect(wrapper.findAll('[role="option"]').map((option) => option.text())).toEqual(['gpt-image-2', 'gpt-image-2.5'])
    expect(wrapper.text()).not.toContain('Preset display name')
    expect(wrapper.text()).not.toContain('flare')
    expect(wrapper.text()).not.toContain('sunburst')
  })

  it('refreshes from the icon button without changing the selected model', async () => {
    const wrapper = mountSelect()
    await wrapper.get('button[aria-haspopup]').trigger('click')
    const refresh = wrapper.get('[data-testid="refresh-models"]')
    expect(refresh.attributes('type')).toBe('button')
    expect(refresh.attributes('aria-label')).toBe('imageStudio.modelSelect.refresh')
    expect(refresh.attributes('title')).toBe('imageStudio.modelSelect.refresh')
    await refresh.trigger('click')

    expect(wrapper.emitted('refresh')).toHaveLength(1)
    expect(wrapper.emitted('change')).toBeUndefined()
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
  })

  it('does not apply a highlighted option when the refresh button receives Enter', async () => {
    const wrapper = mountSelect()
    await wrapper.get('button[aria-haspopup]').trigger('click')
    const refresh = wrapper.get('[data-testid="refresh-models"]')
    await refresh.trigger('keydown', { key: 'Enter' })
    await refresh.trigger('click')

    expect(wrapper.emitted('refresh')).toHaveLength(1)
    expect(wrapper.emitted('change')).toBeUndefined()
  })

  it('keeps refresh available for an empty or failed catalog', async () => {
    const wrapper = mountSelect({ models: [], disabled: true })
    expect(wrapper.get('button[aria-haspopup]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="refresh-models"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="refresh-models"]').trigger('click')

    expect(wrapper.emitted('refresh')).toHaveLength(1)
  })

  it('shows progress and prevents repeated refreshes while loading', async () => {
    const wrapper = mountSelect()
    await wrapper.get('button[aria-haspopup]').trigger('click')
    await wrapper.setProps({ refreshing: true })
    const refresh = wrapper.get('[data-testid="refresh-models"]')

    expect(refresh.attributes('disabled')).toBeDefined()
    expect(refresh.attributes('aria-busy')).toBe('true')
    expect(refresh.attributes('aria-label')).toBe('imageStudio.modelSelect.refreshing')
    expect(refresh.get('svg').classes()).toContain('animate-spin')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    await refresh.trigger('click')
    expect(wrapper.emitted('refresh')).toBeUndefined()
  })

  it('disables refresh when no connection key is available', async () => {
    const wrapper = mountSelect({ refreshDisabled: true })
    await wrapper.get('[data-testid="refresh-models"]').trigger('click')

    expect(wrapper.get('[data-testid="refresh-models"]').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('refresh')).toBeUndefined()
  })

  it('searches raw IDs instead of predefined display names', async () => {
    const wrapper = mountSelect({
      models: Array.from({ length: 9 }, (_, index) => model(`provider-image-${index}`, 'Friendly display name')),
    })
    await wrapper.get('button[aria-haspopup]').trigger('click')
    await wrapper.get('input').setValue('IMAGE-7')
    expect(wrapper.findAll('[role="option"]').map((option) => option.text())).toEqual(['provider-image-7'])
    await wrapper.get('input').setValue('Friendly')
    expect(wrapper.findAll('[role="option"]')).toHaveLength(0)
  })

  it('retains keyboard selection for the catalog models', async () => {
    const wrapper = mountSelect()
    const trigger = wrapper.get('button[aria-haspopup]')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await wrapper.get('input').trigger('keydown', { key: 'ArrowDown' })
    await wrapper.get('input').trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('change')).toEqual([['gpt-image-2.5']])
  })
})
