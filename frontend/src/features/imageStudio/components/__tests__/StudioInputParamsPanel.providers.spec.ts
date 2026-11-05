import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AppSettings, TaskParams } from '../../types'
import { DEFAULT_PARAMS } from '../../types'
import { createImageModelCatalogFromModelList, type ImageModelDefinition } from '../../lib/modelRegistry'
import { normalizeParamsForSettings } from '../../lib/paramCompatibility'
import { buildGeminiImageRequest } from '../../lib/geminiImageApi'
import StudioInputParamsPanel from '../StudioInputParamsPanel.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

enableAutoUnmount(afterEach)

function model(id: string, metadata: Record<string, unknown> = {}): ImageModelDefinition {
  return createImageModelCatalogFromModelList({ models: [{ id, ...metadata }] })[0]!
}

function settings(selectedModelId: string): AppSettings {
  return { selectedModelId } as AppSettings
}

function mountPanel(current: ImageModelDefinition, initial: Partial<TaskParams> = {}) {
  const wrapper = mount(StudioInputParamsPanel, {
    props: {
      model: current,
      params: normalizeParamsForSettings({ ...DEFAULT_PARAMS, ...initial }, settings(current.id), [current]),
      onSetParams: (patch: Partial<TaskParams>) => {
        const selected = wrapper.props('model')!
        void wrapper.setProps({ params: normalizeParamsForSettings({ ...wrapper.props('params'), ...patch }, settings(selected.id), [selected]) })
      },
    },
    global: { stubs: { StudioIcon: true, teleport: true } },
  })
  return wrapper
}

type Panel = ReturnType<typeof mountPanel>

async function clickText(wrapper: Panel, text: string) {
  const button = wrapper.findAll('button').find((item) => item.text() === text)
  expect(button, `Missing button ${text}`).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

describe('StudioInputParamsPanel provider controls', () => {
  it('greys out 4K when account routing only admits 1K and 2K, and blocks clicks', async () => {
    const current = model('gpt-image-2.6-sunburst', { api: 'openai', available_size_tiers: ['1K', '2K'] })
    const wrapper = mountPanel(current)
    const fourK = wrapper.findAll('button').find((button) => button.text() === '4K')!
    expect(fourK.attributes('disabled')).toBeDefined()
    expect(fourK.attributes('title')).toBe('imageStudio.params.channelSizeUnavailable')
    await fourK.trigger('click')
    expect(wrapper.props('params').resolution).toBe('auto')
    await clickText(wrapper, '2K')
    expect(wrapper.props('params').resolution).toBe('2K')
    expect(wrapper.find('[role="status"]').text()).toBe('imageStudio.params.availableSizes')
  })
  it.each(['gpt-image-2.6-flare', 'gpt-image-2.6-sunburst-2026-11-01'])('shows resolution, quality, and independent complexity controls for %s', async (id) => {
    const current = model(id)
    const wrapper = mountPanel(current)
    expect(wrapper.get('[data-testid="provider-parameters-title"]').text()).toBe('imageStudio.params.providers.openai')
    expect(wrapper.text()).toContain('imageStudio.params.quality')
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(true)
    await clickText(wrapper, '4K')
    await clickText(wrapper, 'imageStudio.params.qualityOptions.high')
    await wrapper.get('[data-testid="image-complexity"]').findAll('button').find((button) => button.text() === 'imageStudio.params.complexityOptions.low')!.trigger('click')
    await flushPromises()

    expect(wrapper.props('params')).toMatchObject({ resolution: '4K', quality: 'high', complexity: 'low' })
  })

  it('uses server profiles rather than the public GPT-looking name to choose controls', () => {
    const current = model('gpt-image-2.6-flare', {
      api: 'gemini', profiles: [{ provider: 'google', profile: 'gemini-3.1-flash-image' }],
    })
    const wrapper = mountPanel(current)

    expect(wrapper.get('[data-testid="provider-parameters-title"]').text()).toBe('imageStudio.params.providers.google')
    expect(wrapper.findAll('button').some((button) => button.text() === '512')).toBe(true)
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('imageStudio.params.quality')
  })

  it('selects Gemini 512 and 1:8 through the real panel without generating GPT pixel sizes', async () => {
    const current = model('gemini-3.1-flash-image', { api: 'gemini' })
    const wrapper = mountPanel(current)
    await clickText(wrapper, '512')
    await clickText(wrapper, 'imageStudio.params.moreRatios')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
    await clickText(wrapper, '1:8')
    await clickText(wrapper, 'imageStudio.ratio.apply')

    expect(wrapper.props('params')).toMatchObject({ resolution: '0.5K', aspect_ratio: '1:8', size: 'auto' })
    expect(buildGeminiImageRequest('Draw', wrapper.props('params'), [], current.capabilities).generationConfig.imageConfig)
      .toEqual({ imageSize: '512', aspectRatio: '1:8' })
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('imageStudio.params.quality')
    expect(wrapper.text()).not.toContain('imageStudio.params.moreParams')
  })

  it.each(['gemini-2.5-flash-image', 'gemini-3.1-flash-lite-image'])('limits %s to its fixed 1K controls', (id) => {
    const wrapper = mountPanel(model(id))
    expect(wrapper.findAll('button').filter((button) => ['512', '2K', '4K'].includes(button.text()))).toHaveLength(0)
  })

  it('limits Grok 2.0 to 1K/2K and low/medium quality, with up to ten images', async () => {
    const current = model('grok-imagine-image-2.0', { api: 'openai' })
    const wrapper = mountPanel(current)
    expect(wrapper.get('[data-testid="provider-parameters-title"]').text()).toBe('imageStudio.params.providers.xai')
    expect(wrapper.findAll('button').filter((button) => ['512', '4K'].includes(button.text()))).toHaveLength(0)
    expect(wrapper.text()).not.toContain('imageStudio.params.qualityOptions.high')
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('imageStudio.params.moreParams')
    await clickText(wrapper, '2K')
    await clickText(wrapper, 'imageStudio.params.qualityOptions.medium')
    const count = wrapper.get('input#image-count')
    expect(count.attributes('max')).toBe('10')
    await count.setValue('10')
    await count.trigger('change')
    await flushPromises()

    expect(wrapper.props('params')).toMatchObject({ resolution: '2K', quality: 'medium', n: 10, size: 'auto' })
  })

  it('shows a clear upstream-default hint for unknown model capabilities', () => {
    const wrapper = mountPanel(model('private-image-v9', { api: 'openai' }))

    expect(wrapper.get('[role="status"]').text()).toBe('imageStudio.params.unknownCapabilities')
    expect(wrapper.get('[data-testid="provider-parameters-title"]').text()).toBe('imageStudio.params.title')
    expect(wrapper.findAll('button')).toHaveLength(0)
  })

  it('does not expose inferred GPT controls when server metadata marks the mapped model unknown', () => {
    const wrapper = mountPanel(model('gpt-image-2.6-flare', {
      api: 'openai', profiles: [{ provider: 'unknown', profile: 'unknown' }],
    }))

    expect(wrapper.get('[role="status"]').text()).toBe('imageStudio.params.unknownCapabilities')
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(false)
    expect(wrapper.findAll('button')).toHaveLength(0)
  })

  it('shows the shared-capabilities hint and hides controls outside a mixed channel intersection', () => {
    const current = model('shared-image', {
      api: 'gemini',
      profiles: [
        { provider: 'google', profile: 'gemini-2.5-flash-image' },
        { provider: 'google', profile: 'gemini-3.1-flash-image' },
      ],
    })
    const wrapper = mountPanel(current)

    expect(wrapper.get('[role="status"]').text()).toBe('imageStudio.params.sharedCapabilities')
    expect(current.capabilities.resolutions).toEqual(['auto', '1K'])
    expect(wrapper.findAll('button').filter((button) => ['512', '2K', '4K'].includes(button.text()))).toHaveLength(0)
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(false)
  })

  it('drops provider-specific advanced parameters when switching from GPT to Gemini and back', async () => {
    const gpt = model('gpt-image-2.6-flare')
    const gemini = model('gemini-3.1-flash-image', { api: 'gemini' })
    const wrapper = mountPanel(gpt, {
      resolution: '4K', quality: 'high', complexity: 'high', output_format: 'webp', output_compression: 80,
      transparent_output: true, moderation: 'low', partial_images: 3,
    })
    const normalized = normalizeParamsForSettings(wrapper.props('params'), settings(gemini.id), [gemini])
    await wrapper.setProps({ model: gemini, params: normalized })

    expect(wrapper.props('params')).toMatchObject({ quality: 'auto', complexity: undefined, output_compression: null, moderation: 'auto', transparent_output: false, partial_images: 0 })
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('imageStudio.params.quality')
    const fromGemini = { ...normalized, aspect_ratio: '1:8', resolution: '0.5K' as const }
    const returned = normalizeParamsForSettings(fromGemini, settings(gpt.id), [gpt])
    await wrapper.setProps({ model: gpt, params: returned })
    expect(wrapper.props('params').aspect_ratio).toBeUndefined()
    expect(wrapper.props('params').resolution).toBe('auto')
    expect(wrapper.find('[data-testid="image-complexity"]').exists()).toBe(true)
  })
})
