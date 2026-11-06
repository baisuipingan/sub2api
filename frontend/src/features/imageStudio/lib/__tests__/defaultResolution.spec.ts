import { describe, expect, it } from 'vitest'
import { DEFAULT_PARAMS, type AppSettings } from '../../types'
import { createImageModelCatalogFromModelList } from '../modelRegistry'
import { isRequestedImageSizeAvailable, normalizeParamsForSettings } from '../paramCompatibility'

const SETTINGS = { selectedModelId: 'gpt-image-2' } as AppSettings

function normalize(modelId: string, metadata = {}, params = { ...DEFAULT_PARAMS }) {
  const catalog = createImageModelCatalogFromModelList({ models: [{ id: modelId, ...metadata }] })
  const next = normalizeParamsForSettings(params, { ...SETTINGS, selectedModelId: modelId }, catalog)
  return { next, model: catalog[0] }
}

describe('Image Studio initial resolution', () => {
  it('starts at an explicit 2K tier and produces square GPT pixel dimensions', () => {
    expect(DEFAULT_PARAMS).toMatchObject({ resolution: '2K', size: 'auto' })
    expect(normalize('gpt-image-2').next).toMatchObject({ resolution: '2K', size: '2048x2048' })
  })

  it('retains the selected default while the model catalog is unavailable', () => {
    expect(normalizeParamsForSettings({ ...DEFAULT_PARAMS }, SETTINGS, [])).toMatchObject({ resolution: '2K', size: 'auto' })
  })

  it.each(['gemini-3.1-flash-image', 'grok-imagine-image-2.0'])('uses explicit 2K and a square native ratio for %s', (id) => {
    expect(normalize(id).next).toMatchObject({ resolution: '2K', size: 'auto', aspect_ratio: '1:1' })
  })

  it.each([
    { tiers: ['1K'], resolution: '1K', size: '1024x1024' },
    { tiers: ['4K'], resolution: '4K', size: '2880x2880' },
    { tiers: ['1K', '4K'], resolution: '1K', size: '1024x1024' },
  ])('selects a concrete available fallback when the channel only offers $tiers', ({ tiers, resolution, size }) => {
    const { next, model } = normalize('gpt-image-2', { available_size_tiers: tiers })
    expect(next).toMatchObject({ resolution, size })
    expect(isRequestedImageSizeAvailable(model, next)).toBe(true)
  })

  it.each(['gemini-2.5-flash-image', 'gemini-3.1-flash-lite-image'])('uses the fixed 1K tier for %s', (id) => {
    expect(normalize(id).next).toMatchObject({ resolution: '1K', size: 'auto' })
  })

  it('prefers 1K over 0.5K when a Gemini channel lacks 2K', () => {
    const { next, model } = normalize('gemini-3.1-flash-image', { available_size_tiers: ['1K'] })
    expect(next).toMatchObject({ resolution: '1K', size: 'auto', aspect_ratio: '1:1' })
    expect(isRequestedImageSizeAvailable(model, next)).toBe(true)
  })

  it('uses supported dimensions for the first-generation GPT image profile', () => {
    expect(normalize('gpt-image-1.5').next).toMatchObject({ resolution: '1K', size: '1024x1024' })
  })

  it('does not carry GPT pixel dimensions into a native Gemini task', () => {
    const gpt = normalize('gpt-image-2').next
    const gemini = normalize('gemini-2.5-flash-image', {}, gpt).next
    expect(gemini).toMatchObject({ resolution: '1K', size: 'auto', aspect_ratio: '1:1' })
  })

  it('keeps unknown model geometry conservative', () => {
    expect(normalize('unknown-image-model').next).toMatchObject({ resolution: 'auto', size: 'auto' })
  })

  it.each(['gpt-image-2', 'gemini-3.1-flash-image', 'grok-imagine-image-2.0'])('normalizes an older auto draft to explicit 2K for %s', (id) => {
    const { next } = normalize(id, {}, { ...DEFAULT_PARAMS, resolution: 'auto' })
    expect(next.resolution).toBe('2K')
    if (id === 'gpt-image-2') expect(next.size).toBe('2048x2048')
    else expect(next).toMatchObject({ size: 'auto', aspect_ratio: '1:1' })
  })

  it('normalizes an older auto draft to a concrete available tier on a 4K-only channel', () => {
    expect(normalize('gpt-image-2', { available_size_tiers: ['4K'] }, { ...DEFAULT_PARAMS, resolution: 'auto' }).next).toMatchObject({ resolution: '4K', size: '2880x2880' })
  })

  it('makes a previously automatic native ratio explicit when resolving the draft', () => {
    expect(normalize('gemini-3.1-flash-image', {}, { ...DEFAULT_PARAMS, aspect_ratio: 'auto' }).next).toMatchObject({ resolution: '2K', aspect_ratio: '1:1' })
  })
})
