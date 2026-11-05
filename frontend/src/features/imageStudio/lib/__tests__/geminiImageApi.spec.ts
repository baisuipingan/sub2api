import { afterEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type AppSettings, type TaskParams } from '../../types'
import { IMAGE_MODEL_REGISTRY, type ImageModelCapabilities } from '../modelRegistry'
import { buildGeminiImageRequest, callGeminiImageApi, mapGeminiImageConfig, parseGeminiImageResponse } from '../geminiImageApi'

vi.mock('../i18nMessage', () => ({ libT: (key: string) => key }))

const IMAGE = 'data:image/png;base64,iVBORw0KGgo='
const RATIOS = ['1:1', '2:3', '3:2', '3:4', '4:3', '4:5', '5:4', '9:16', '16:9', '21:9']
const WIDE_RATIOS = [...RATIOS, '1:4', '4:1', '1:8', '8:1']

function capabilities(patch: Partial<ImageModelCapabilities> = {}): ImageModelCapabilities {
  const template = IMAGE_MODEL_REGISTRY.find((model) => model.id === 'gemini-3.1-flash-image')!
  return { ...template.capabilities, resolutions: ['auto', '0.5K', '1K', '2K', '4K'], aspectRatios: WIDE_RATIOS, ...patch }
}

afterEach(() => vi.unstubAllGlobals())

describe('Gemini native image parameters', () => {
  it.each(['1:4', '4:1', '1:8', '8:1'])('preserves explicit %s instead of inferring a clamped pixel ratio', (aspectRatio) => {
    const request = buildGeminiImageRequest('Draw', {
      ...DEFAULT_PARAMS, size: '1536x512', resolution: '2K', aspect_ratio: aspectRatio,
    }, [], capabilities())

    expect(request.generationConfig.imageConfig).toEqual({ aspectRatio, imageSize: '2K' })
  })

  it('maps a supported half-K resolution to the official imageSize 512 value', () => {
    const request = buildGeminiImageRequest('Draw', {
      ...DEFAULT_PARAMS, resolution: '0.5K', aspect_ratio: '16:9',
    }, [], capabilities())

    expect(request.generationConfig.imageConfig).toEqual({ aspectRatio: '16:9', imageSize: '512' })
  })

  it('does not send 512 unless the model profile declares support', () => {
    expect(mapGeminiImageConfig('auto', '0.5K')).toBeUndefined()
    expect(mapGeminiImageConfig('auto', '0.5K', undefined, capabilities({ resolutions: ['auto', '1K', '2K', '4K'] }))).toBeUndefined()
  })

  it.each(['auto', '1K', '2K', '4K'] as const)('omits imageSize for a fixed-1K profile even with a stale %s selection', (resolution) => {
    const request = buildGeminiImageRequest('Draw', {
      ...DEFAULT_PARAMS, size: '4096x4096', resolution, aspect_ratio: '4:5',
    }, [], capabilities({ resolutions: ['auto', '1K'], aspectRatios: RATIOS }))

    expect(request.generationConfig.imageConfig).toEqual({ aspectRatio: '4:5' })
  })

  it('does not infer a larger size for a fixed-1K model when resolution is absent', () => {
    expect(mapGeminiImageConfig('4096x4096', undefined, undefined, capabilities({ resolutions: ['auto', '1K'] }))).toEqual({ aspectRatio: '1:1' })
  })

  it('does not forward an explicit ratio unsupported by the selected model', () => {
    const request = buildGeminiImageRequest('Draw', {
      ...DEFAULT_PARAMS, size: '1024x1024', resolution: '2K', aspect_ratio: '8:1',
    }, [], capabilities({ aspectRatios: RATIOS }))

    expect(request.generationConfig.imageConfig).toEqual({ imageSize: '2K' })
  })

  it('keeps auto resolution automatic while sending a supported explicit ratio', () => {
    expect(mapGeminiImageConfig('4096x4096', 'auto', '1:8', capabilities())).toEqual({ aspectRatio: '1:8' })
  })

  it('continues to infer a supported ratio for an older request without aspect_ratio', () => {
    expect(mapGeminiImageConfig('2048x1152', '2K', undefined, capabilities({ aspectRatios: RATIOS }))).toEqual({ aspectRatio: '16:9', imageSize: '2K' })
  })

  it('keeps inline reference images and omits unsupported GPT image options', () => {
    const request = buildGeminiImageRequest('Edit this', {
      ...DEFAULT_PARAMS, resolution: '1K', aspect_ratio: '1:1', quality: 'high', output_format: 'jpeg', output_compression: 80, moderation: 'low', transparent_output: true,
    }, [IMAGE], capabilities())

    expect(request).toEqual({
      contents: [{ role: 'user', parts: [{ text: 'Edit this' }, { inlineData: { mimeType: 'image/png', data: 'iVBORw0KGgo=' } }] }],
      generationConfig: { responseModalities: ['IMAGE'], imageConfig: { aspectRatio: '1:1', imageSize: '1K' } },
    })
  })

  it('passes model capabilities to the request builder on the live adapter path', async () => {
    const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(new Response(JSON.stringify({
      candidates: [{ content: { parts: [{ inlineData: { mimeType: 'image/png', data: 'iVBORw0KGgo=' } }] } }],
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    const settings = { miloRouter: { apiKey: 'test-key' }, selectedModelId: 'public-image-alias', timeout: 30 } as AppSettings
    await callGeminiImageApi({
      settings,
      modelId: 'public-image-alias',
      modelCapabilities: capabilities({ resolutions: ['auto', '1K'], aspectRatios: RATIOS }),
      prompt: 'Draw',
      params: { ...DEFAULT_PARAMS, resolution: '4K', aspect_ratio: '16:9' } as TaskParams,
      inputImageDataUrls: [],
    })

    expect(fetchMock.mock.calls[0][0]).toBe('/v1beta/models/public-image-alias:generateContent')
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string).generationConfig.imageConfig).toEqual({ aspectRatio: '16:9' })
  })
})

describe('Gemini native final image parsing', () => {
  it('ignores intermediate thought images and returns only final image parts', () => {
    const result = parseGeminiImageResponse({ candidates: [{ content: { parts: [
      { thought: true, inlineData: { mimeType: 'image/png', data: 'dGhvdWdodA==' } },
      { text: 'Final image' },
      { inlineData: { mimeType: 'image/png', data: 'iVBORw0KGgo=' } },
    ] } }] })

    expect(result.images).toEqual([IMAGE])
    expect(result.actualParams).toEqual({ n: 1 })
  })

  it('does not treat an all-thought response as a successful generated image', () => {
    expect(() => parseGeminiImageResponse({ candidates: [{ content: { parts: [
      { thought: true, inlineData: { mimeType: 'image/png', data: 'dGhvdWdodA==' } },
    ] } }] })).toThrow()
  })
})
