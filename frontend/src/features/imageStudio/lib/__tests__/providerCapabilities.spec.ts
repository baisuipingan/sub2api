import { describe, expect, it } from 'vitest'
import { createImageModelCatalogFromModelList, getRequestModelId } from '../modelRegistry'
import { normalizeParamsForSettings, parameterImageSize, isRequestedImageSizeAvailable, isResolutionAvailable } from '../paramCompatibility'
import { DEFAULT_PARAMS, type AppSettings } from '../../types'
import { normalizeBillingTier } from '../studioPricing'

function model(id: string, metadata = {}) {
  return createImageModelCatalogFromModelList({ models: [{ id, ...metadata }] })[0]!
}
function settings(id: string): AppSettings {
  return { miloRouter: { baseUrl: '', apiKey: 'test', connectionStatus: 'connected', rememberApiKey: false }, selectedModelId: id, timeout: 30, connectionOnboardingSeen: true, clearInputAfterSubmit: false, persistInputOnRestart: false, taskCompletionNotification: false, enterSubmit: false }
}

describe('Image Studio documented provider capabilities', () => {
  it('keeps documented 4K visible but disables it when serving accounts allow only 1K and 2K', () => {
    const current = model('gpt-image-2.6-sunburst', { api: 'openai', available_size_tiers: ['1K', '2K'] })
    expect(current.capabilities.resolutions).toContain('4K')
    expect(isResolutionAvailable(current, '4K')).toBe(false)
    expect(isResolutionAvailable(current, '2K')).toBe(true)
    expect(isResolutionAvailable(current, 'auto')).toBe(true)
    expect(isRequestedImageSizeAvailable(current, { ...DEFAULT_PARAMS, resolution: '4K', size: '2880x2880' })).toBe(false)
    expect(isRequestedImageSizeAvailable(current, { ...DEFAULT_PARAMS, resolution: '2K', size: '2560x1440' })).toBe(false)
    const normalized = normalizeParamsForSettings({ ...DEFAULT_PARAMS, resolution: '4K', size: '2880x2880' }, settings(current.id), [current])
    expect(normalized).toMatchObject({ resolution: '2K', size: '2048x2048' })
    expect(normalizeParamsForSettings({ ...DEFAULT_PARAMS, resolution: '2K' }, settings(current.id), [current])).toMatchObject({ resolution: '2K', size: '2048x2048' })
  })

  it('does not invent an account restriction when the server omits tier metadata', () => {
    const current = model('gpt-image-2.6-flare')
    expect(current.availableSizeTiers).toBeUndefined()
    expect(isResolutionAvailable(current, '4K')).toBe(true)
    expect(isRequestedImageSizeAvailable(current, { ...DEFAULT_PARAMS, resolution: '4K', size: '2880x2880' })).toBe(true)
  })

  it('treats the Grok auto request as its actual 1K default on an Images account restricted to 2K', () => {
    const current = model('grok-imagine-image-2.0', { api: 'openai', available_size_tiers: ['2K'] })
    expect(isResolutionAvailable(current, 'auto')).toBe(false)
    const params = { ...DEFAULT_PARAMS, resolution: 'auto' as const }
    expect(isRequestedImageSizeAvailable(current, params)).toBe(false)
    const normalized = normalizeParamsForSettings(params, settings(current.id), [current])
    expect(normalized).toMatchObject({ resolution: '2K', size: 'auto' })
    expect(isRequestedImageSizeAvailable(current, normalized)).toBe(true)
  })
  it.each(['gpt-image-2.6-flare', 'gpt-image-2.6-sunburst', 'gpt-image-2.6-flare-2026-11-01', 'gpt-image-2.6-flare-4K'])('offers current OpenAI controls for %s without changing its public name', (id) => {
    const current = model(id)
    expect(current.provider).toBe('openai')
    expect(current.capabilities.resolutions).toEqual(['auto', '1K', '2K', '4K'])
    expect(current.capabilities.complexities).toEqual(['auto', 'low', 'medium', 'high'])
    expect(current.capabilities.outputFormats).toEqual(['png', 'jpeg', 'webp'])
    expect(getRequestModelId(current, 'auto', '4K')).toBe(id)
    expect(current.name).toBe(id)
  })

  it('does not infer a documented 2.6 profile for a different version or arbitrary suffix', () => {
    expect(model('gpt-image-2.60-flare').capabilityStatus).toBe('unknown')
    expect(model('gpt-image-2.6-flare-private').capabilityStatus).toBe('unknown')
  })

  it('uses route metadata for a public alias and separates Gemini capabilities from Images protocol', () => {
    const current = model('my-private-banana', { api: 'openai', profiles: [{ provider: 'google', profile: 'gemini-3.1-flash-image' }] })
    expect(current.provider).toBe('google')
    expect(current.api).toBe('openai')
    expect(current.capabilities.qualities).toEqual([])
    expect(current.capabilities.resolutions).toEqual(['auto', '1K', '2K', '4K'])
    expect(current.capabilities.aspectRatios).not.toContain('1:8')
    expect(current.name).toBe('my-private-banana')
  })

  it('does not let a public GPT-looking alias override authoritative unknown mapping metadata', () => {
    const current = model('gpt-image-2.6-flare', { api: 'openai', profiles: [{ provider: 'unknown', profile: 'unknown' }] })
    expect(current.capabilityStatus).toBe('unknown')
    expect(current.capabilities.complexities).toEqual([])
    expect(current.capabilities.resolutions).toEqual(['auto'])
  })

  it('intersects all serving profiles, including unknown and bridge constraints', () => {
    const metadata = { api: 'openai', profiles: [{ provider: 'openai', profile: 'gpt-image-2.6' }, { provider: 'openai', profile: 'gpt-image-2.6', transport: 'bridge' }] }
    const mixed = model('paint', metadata)
    expect(mixed.capabilityStatus).toBe('mixed')
    expect(mixed.capabilities.complexities).toEqual([])
    expect(mixed.capabilities.resolutions).toContain('4K')
    const unknown = model('paint', { ...metadata, profiles: [...metadata.profiles, { provider: 'unknown', profile: 'unknown' }] })
    expect(unknown.capabilities.referenceImages).toBe(0)
    expect(unknown.capabilities.resolutions).toEqual(['auto'])
  })

  it('uses separate Google model profiles rather than giving every Gemini model 4K', () => {
    expect(model('gemini-2.5-flash-image').capabilities.resolutions).toEqual(['auto', '1K'])
    expect(model('gemini-3.1-flash-lite-image').capabilities.resolutions).toEqual(['auto', '1K'])
    expect(model('gemini-3-pro-image-preview').capabilities.resolutions).toEqual(['auto', '1K', '2K', '4K'])
    expect(model('gemini-3.1-flash-image').capabilities.resolutions).toContain('0.5K')
    expect(model('gemini-nano-banana-2.1').capabilities.resolutions).not.toContain('0.5K')
  })

  it('does not advertise incompatible geometry fields when one public ID mixes Images vendors', () => {
    const current = model('mixed-painter', { api: 'openai', profiles: [{ provider: 'openai', profile: 'gpt-image-2.6' }, { provider: 'xai', profile: 'grok-imagine-image-2.0' }] })
    expect(current.capabilityStatus).toBe('mixed')
    expect(current.provider).toBe('unknown')
    expect(current.capabilities.resolutions).toEqual(['auto'])
    expect(current.capabilities.referenceImages).toBe(0)
    expect(current.capabilities.qualities).toEqual([])
  })

  it('preserves native 512 and ultrawide ratios without GPT pixel clamping', () => {
    const current = model('gemini-3.1-flash-image')
    const params = normalizeParamsForSettings({ ...DEFAULT_PARAMS, resolution: '0.5K', aspect_ratio: '1:8' }, settings(current.id), [current])
    expect(params).toMatchObject({ resolution: '0.5K', aspect_ratio: '1:8', size: 'auto' })
    expect(normalizeBillingTier('0.5K')).toBe('1K')
  })

  it('clears parameters from the previous provider when switching models', () => {
    const current = model('grok-imagine-image-2.0')
    const params = normalizeParamsForSettings({ ...DEFAULT_PARAMS, resolution: '4K', quality: 'high', complexity: 'high', output_format: 'webp', output_compression: 50, transparent_output: true, partial_images: 3, aspect_ratio: '1:8' }, settings(current.id), [current])
    expect(params).toMatchObject({ resolution: '2K', quality: 'auto', complexity: undefined, output_compression: null, transparent_output: false, partial_images: 0, aspect_ratio: '1:1' })
    expect(current.capabilities.imageCount.max).toBe(10)
    expect(current.capabilities.referenceImages).toBe(5)
    const gpt = model('gpt-image-2')
    expect(normalizeParamsForSettings({ ...params, aspect_ratio: '21:9' }, settings(gpt.id), [gpt]).aspect_ratio).toBeUndefined()
  })

  it('uses supported fixed dimensions for first-generation GPT image models', () => {
    const current = model('gpt-image-1.5')
    expect(parameterImageSize(current, '1K', '3:2')).toBe('1536x1024')
    expect(parameterImageSize(current, '1K', '2:3')).toBe('1024x1536')
  })
})
