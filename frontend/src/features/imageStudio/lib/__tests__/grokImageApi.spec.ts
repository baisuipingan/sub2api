import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type AppSettings } from '../../types'
import { buildGrokImageRequest, callGrokImageApi } from '../grokImageApi'
import type { CallApiOptions } from '../imageApiShared'
import { IMAGE_MODEL_REGISTRY } from '../modelRegistry'

vi.mock('../i18nMessage', () => ({ libT: (key: string) => key }))

const IMAGE = 'data:image/png;base64,iVBORw0KGgo='
const SETTINGS: AppSettings = {
  miloRouter: { baseUrl: '', apiKey: 'sk-test', rememberApiKey: false, connectionStatus: 'connected' },
  selectedModelId: 'grok-imagine-image-2.0', connectionOnboardingSeen: true, timeout: 30,
  clearInputAfterSubmit: false, persistInputOnRestart: false, taskCompletionNotification: false, enterSubmit: false,
}
const XAI_CAPABILITIES = {
  ...IMAGE_MODEL_REGISTRY[0].capabilities,
  qualities: ['auto', 'low', 'medium'] as CallApiOptions['params']['quality'][],
  resolutions: ['auto', '1K', '2K'] as CallApiOptions['params']['resolution'][],
  imageCount: { min: 1, max: 10 },
  referenceImages: 5,
}

function options(patch: Partial<CallApiOptions> = {}): CallApiOptions {
  return {
    settings: SETTINGS, modelId: SETTINGS.selectedModelId, prompt: 'Combine the references', params: { ...DEFAULT_PARAMS },
    modelCapabilities: XAI_CAPABILITIES, inputImageDataUrls: [], ...patch,
  }
}

const fetchMock = vi.fn<typeof fetch>()
beforeEach(() => {
  fetchMock.mockReset().mockResolvedValue(new Response(JSON.stringify({ data: [{ b64_json: 'iVBORw0KGgo=', mime_type: 'image/png' }] }), { headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('Grok image API adapter', () => {
  it('generates JSON with official resolution/aspect/quality fields and requests base64', async () => {
    await callGrokImageApi(options({ params: { ...DEFAULT_PARAMS, resolution: '2K', aspect_ratio: '21:9', quality: 'medium', output_format: 'webp', output_compression: 75, moderation: 'low', partial_images: 3, transparent_output: true } }))

    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/generations')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'POST', cache: 'no-store', headers: { Authorization: 'Bearer sk-test', 'Content-Type': 'application/json' } })
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({ model: SETTINGS.selectedModelId, prompt: 'Combine the references', n: 1, response_format: 'b64_json', resolution: '2k', aspect_ratio: '21:9', quality: 'medium' })
  })

  it.each(['5:2', 'auto', '19.5:9', '9:20'])('keeps the official aspect ratio %s', (aspect_ratio) => {
    expect(buildGrokImageRequest(options({ params: { ...DEFAULT_PARAMS, aspect_ratio } }))).toHaveProperty('aspect_ratio', aspect_ratio)
  })

  it('sends the documented 1k default for auto while leaving the aspect ratio automatic', () => {
    const body = buildGrokImageRequest(options({ params: { ...DEFAULT_PARAMS, resolution: 'auto' } }))
    expect(body.resolution).toBe('1k')
    expect(body).not.toHaveProperty('aspect_ratio')
  })

  it('preserves a legacy selected pixel ratio when no explicit aspect is present', () => {
    const body = buildGrokImageRequest(options({ params: { ...DEFAULT_PARAMS, size: '2000x800', resolution: '2K' } }))
    expect(body).toHaveProperty('aspect_ratio', '5:2')
    expect(body).not.toHaveProperty('size')
  })

  it('sends ten same-prompt images in one native batch request', async () => {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ data: Array.from({ length: 10 }, () => ({ b64_json: '/9j/', mime_type: 'image/jpeg' })) })))
    const result = await callGrokImageApi(options({ params: { ...DEFAULT_PARAMS, n: 10 } }))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string).n).toBe(10)
    expect(result.images).toHaveLength(10)
    expect(result.actualParams?.n).toBe(10)
    expect(result.images[0]).toBe('data:image/jpeg;base64,/9j/')
  })

  it('uses a single image object for a one-reference JSON edit', async () => {
    await callGrokImageApi(options({ inputImageDataUrls: [IMAGE] }))

    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/edits')
    const body = JSON.parse(fetchMock.mock.calls[0][1]!.body as string)
    expect(body.image).toEqual({ url: IMAGE, type: 'image_url' })
    expect(body).not.toHaveProperty('images')
  })

  it('uses only images for a five-reference 2.0 edit and preserves auto quality', async () => {
    const sources = Array.from({ length: 5 }, (_, index) => `https://images.example/${index}.png`)
    await callGrokImageApi(options({ inputImageDataUrls: sources, prompt: 'Combine [image 1] with [image 5]; leave [image 9] as text' }))

    const body = JSON.parse(fetchMock.mock.calls[0][1]!.body as string)
    expect(body.images).toEqual(sources.map((url) => ({ url, type: 'image_url' })))
    expect(body).not.toHaveProperty('image')
    expect(body.quality).toBe('auto')
    expect(body.prompt).toBe('Combine <IMAGE_0> with <IMAGE_4>; leave [image 9] as text')
  })

  it('does not add new quality controls to legacy model capabilities', () => {
    const body = buildGrokImageRequest(options({ modelId: 'grok-imagine-image', modelCapabilities: { ...XAI_CAPABILITIES, qualities: [], referenceImages: 3 } }))
    expect(body).not.toHaveProperty('quality')
  })

  it('rejects unsupported resolution, mask, image type, reference count, and batch size before fetch', async () => {
    for (const patch of [
      { params: { ...DEFAULT_PARAMS, resolution: '4K' as const } },
      { params: { ...DEFAULT_PARAMS, aspect_ratio: '7:1' } },
      { params: { ...DEFAULT_PARAMS, n: 11 } },
      { params: { ...DEFAULT_PARAMS, n: 0 } },
      { maskDataUrl: IMAGE },
      { inputImageDataUrls: ['data:image/gif;base64,R0lGODlh'] },
      { inputImageDataUrls: Array.from({ length: 6 }, () => IMAGE) },
      { modelCapabilities: { ...XAI_CAPABILITIES, referenceImages: 3 }, inputImageDataUrls: Array.from({ length: 4 }, () => IMAGE) },
    ]) {
      await expect(callGrokImageApi(options(patch))).rejects.toThrow()
    }
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('detects actual PNG data even when the upstream omits its MIME', async () => {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ data: [{ b64_json: 'iVBORw0KGgo=' }] })))
    expect((await callGrokImageApi(options())).images).toEqual([IMAGE])
  })

  it('reports a successful response without images as an invalid response', async () => {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ data: [] })))
    await expect(callGrokImageApi(options())).rejects.toMatchObject({ name: 'ApiInvalidResponseError' })
  })

  it('preserves gateway rate-limit error classification', async () => {
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify({ error: { message: 'Too many requests' } }), { status: 429 }))
    await expect(callGrokImageApi(options())).rejects.toMatchObject({ name: 'ApiRateLimitError' })
  })
})
