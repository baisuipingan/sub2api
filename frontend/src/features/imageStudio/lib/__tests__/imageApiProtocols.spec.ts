import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type AppSettings } from '../../types'
import { callImageApi } from '../api'
import { buildGeminiImageRequest, mapGeminiImageConfig } from '../geminiImageApi'
import type { CallApiOptions } from '../imageApiShared'
import { createImageModelCatalogFromModelList, IMAGE_MODEL_REGISTRY, type ImageModelDefinition } from '../modelRegistry'
import { normalizeParamsForSettings } from '../paramCompatibility'
import { dataUrlToBlob, maskDataUrlToPngBlob } from '../canvasImage'

vi.mock('../i18nMessage', () => ({ libT: (key: string) => key }))
vi.mock('../canvasImage', () => ({
  dataUrlToBlob: vi.fn(async () => new Blob(['reference'], { type: 'image/png' })),
  imageDataUrlToPngBlob: vi.fn(async () => new Blob(['reference'], { type: 'image/png' })),
  maskDataUrlToPngBlob: vi.fn(async () => new Blob(['mask'], { type: 'image/png' })),
}))

const IMAGE = 'data:image/png;base64,iVBORw0KGgo='
const SETTINGS: AppSettings = {
  miloRouter: { baseUrl: '', apiKey: 'sk-test', rememberApiKey: false, connectionStatus: 'connected' },
  selectedModelId: 'gpt-image-2',
  connectionOnboardingSeen: true,
  timeout: 30,
  clearInputAfterSubmit: false,
  persistInputOnRestart: false,
  taskCompletionNotification: false,
  enterSubmit: false,
}

function registeredModel(id: string) {
  return IMAGE_MODEL_REGISTRY.find((model) => model.id === id)!
}

function options(modelId: string, patch: Partial<CallApiOptions> = {}): CallApiOptions {
  return {
    settings: { ...SETTINGS, selectedModelId: modelId },
    modelId,
    prompt: 'Draw a red cup',
    params: { ...DEFAULT_PARAMS },
    inputImageDataUrls: [],
    ...patch,
  }
}

const fetchMock = vi.fn<typeof fetch>()

beforeEach(() => {
  fetchMock.mockReset().mockImplementation(async (url) => new Response(JSON.stringify(String(url).includes('/v1beta/')
    ? { candidates: [{ content: { parts: [{ text: 'A cup' }, { inlineData: { mimeType: 'image/png', data: 'iVBORw0KGgo=' } }] } }] }
    : { data: [{ b64_json: 'iVBORw0KGgo=' }] }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => vi.unstubAllGlobals())

describe('Image Studio protocol adapters', () => {
  it.each(['1K', '2K', '4K'] as const)('Gemini native uses imageSize=%s with a supported aspect ratio and inline reference image', async (resolution) => {
    const model = registeredModel('gemini-3.1-flash-image')
    const result = await callImageApi(options(model.id, {
      apiKind: 'gemini',
      modelCapabilities: model.capabilities,
      params: { ...DEFAULT_PARAMS, size: '2048x1152', resolution, quality: 'high', output_format: 'webp', moderation: 'low' },
      inputImageDataUrls: [IMAGE],
    }))

    expect(result.images).toEqual([IMAGE])
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/v1beta/models/gemini-3.1-flash-image:generateContent')
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({
      contents: [{ role: 'user', parts: [{ text: 'Draw a red cup' }, { inlineData: { mimeType: 'image/png', data: 'iVBORw0KGgo=' } }] }],
      generationConfig: { responseModalities: ['IMAGE'], imageConfig: { aspectRatio: '16:9', imageSize: resolution } },
    })
  })

  it('Gemini auto preserves automatic resolution while retaining a selected aspect ratio', () => {
    expect(mapGeminiImageConfig('2048x1152', 'auto')).toEqual({ aspectRatio: '16:9' })
    expect(buildGeminiImageRequest('Draw', { ...DEFAULT_PARAMS, resolution: 'auto' })).toEqual({
      contents: [{ role: 'user', parts: [{ text: 'Draw' }] }],
      generationConfig: { responseModalities: ['IMAGE'] },
    })
  })

  it('Gemini served through Images uses the selected resolution and omits unsupported GPT options', async () => {
    const template = registeredModel('gemini-3.1-flash-image')
    const model: ImageModelDefinition = { ...template, api: 'openai' }
    const settings = { ...SETTINGS, selectedModelId: model.id }
    const params = normalizeParamsForSettings({ ...DEFAULT_PARAMS, resolution: '2K' }, settings, [model])

    expect(params.size).toBe('2048x2048')
    await callImageApi(options(model.id, { apiKind: model.api, modelCapabilities: model.capabilities, params }))

    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/generations')
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({ model: model.id, prompt: 'Draw a red cup', size: '2048x2048', n: 1 })
  })

  it('Gemini native and compatible Images preserve the selected aspect ratio at 4K', () => {
    const template = registeredModel('gemini-3.1-flash-image')
    const params = { ...DEFAULT_PARAMS, resolution: '4K' as const, size: '1024x576' }
    const native = normalizeParamsForSettings(params, { ...SETTINGS, selectedModelId: template.id }, [template])
    const compatible = normalizeParamsForSettings(params, { ...SETTINGS, selectedModelId: template.id }, [{ ...template, api: 'openai' }])

    expect(native).toMatchObject({ size: 'auto', resolution: '4K', aspect_ratio: '16:9' })
    expect(mapGeminiImageConfig(native.size, native.resolution, native.aspect_ratio, template.capabilities)).toEqual({ aspectRatio: '16:9', imageSize: '4K' })
    expect(compatible.size).toBe('3840x2160')
  })

  it('Grok Images keeps size for gateway geometry conversion and omits GPT-only options', async () => {
    await callImageApi(options('grok-imagine-image', {
      apiKind: 'openai',
      params: { ...DEFAULT_PARAMS, size: '2048x1152', resolution: '2K', quality: 'high', moderation: 'low', output_compression: 75 },
    }))

    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/generations')
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({ model: 'grok-imagine-image', prompt: 'Draw a red cup', size: '2048x1152', n: 1 })
  })

  it('an unknown model defaults to minimal Images parameters', async () => {
    await callImageApi(options('provider/image-v3', { apiKind: 'openai' }))

    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({ model: 'provider/image-v3', prompt: 'Draw a red cup', n: 1 })
  })

  it('a conservative unknown model does not inherit hidden size or GPT options from the previous selection', async () => {
    const [model] = createImageModelCatalogFromModelList({ data: [{ id: 'provider-image-v3' }] })
    const params = normalizeParamsForSettings({ ...DEFAULT_PARAMS, resolution: '4K', size: '3840x2160', quality: 'high' }, { ...SETTINGS, selectedModelId: model.id }, [model])
    expect(params.resolution).toBe('auto')
    expect(params.size).toBe('auto')
    await callImageApi(options(model.id, { apiKind: model.api, modelCapabilities: model.capabilities, params }))

    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({ model: 'provider-image-v3', prompt: 'Draw a red cup', n: 1 })
  })

  it('catalog capabilities enable options for a custom provider model', async () => {
    const capabilities = registeredModel('gpt-image-2').capabilities
    await callImageApi(options('gpt-image-custom', { apiKind: 'openai', modelCapabilities: capabilities, params: { ...DEFAULT_PARAMS, size: '1024x1024', quality: 'high' } }))

    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toMatchObject({ size: '1024x1024', quality: 'high', output_format: 'png', moderation: 'auto' })
  })

  it('GPT generation retains compression and transparent background', async () => {
    await callImageApi(options('gpt-image-2', {
      apiKind: 'openai',
      nativeTransparentOutput: true,
      params: { ...DEFAULT_PARAMS, output_format: 'webp', output_compression: 80, transparent_output: true },
    }))

    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({ model: 'gpt-image-2', prompt: 'Draw a red cup', quality: 'auto', output_format: 'webp', output_compression: 80, moderation: 'auto', n: 1, background: 'transparent' })
  })

  it('compatible Gemini edits upload references without GPT-only multipart fields', async () => {
    const model = registeredModel('gemini-3.1-flash-image')
    await callImageApi(options(model.id, { apiKind: 'openai', modelCapabilities: model.capabilities, inputImageDataUrls: [IMAGE], params: { ...DEFAULT_PARAMS, size: '2048x2048', resolution: '2K' } }))

    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/edits')
    const form = fetchMock.mock.calls[0][1]!.body as FormData
    expect([...form.keys()]).toEqual(['model', 'prompt', 'size', 'n', 'image[]'])
    expect(form.get('size')).toBe('2048x2048')
    expect(form.get('image[]')).toBeInstanceOf(Blob)
  })

  it('GPT mask edits retain the mask and omit whole-image transparent background', async () => {
    await callImageApi(options('gpt-image-2', { apiKind: 'openai', inputImageDataUrls: [IMAGE], maskDataUrl: IMAGE, nativeTransparentOutput: true, params: { ...DEFAULT_PARAMS, transparent_output: true } }))

    const form = fetchMock.mock.calls[0][1]!.body as FormData
    expect(form.get('mask')).toBeInstanceOf(Blob)
    expect(form.get('background')).toBeNull()
    expect(form.get('quality')).toBe('auto')
    expect(form.get('output_format')).toBe('png')
  })

  it('rejects a 50 MB reference before sending a direct OpenAI edit', async () => {
    const blob = new Blob(['reference'], { type: 'image/png' })
    Object.defineProperty(blob, 'size', { value: 50_000_000 })
    vi.mocked(dataUrlToBlob).mockResolvedValueOnce(blob)
    await expect(callImageApi(options('gpt-image-2', { apiKind: 'openai', inputImageDataUrls: [IMAGE] }))).rejects.toThrow('openaiTooLarge')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('checks the actual PNG mask against the endpoint 4 MB limit', async () => {
    const mask = new Blob(['mask'], { type: 'image/png' })
    Object.defineProperty(mask, 'size', { value: 4_000_000 })
    vi.mocked(maskDataUrlToPngBlob).mockResolvedValueOnce(mask)
    await expect(callImageApi(options('gpt-image-2', { apiKind: 'openai', inputImageDataUrls: [IMAGE], maskDataUrl: IMAGE }))).rejects.toThrow('maskTooLarge')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('declared GPT streaming still sends streaming controls and parses the final image', async () => {
    fetchMock.mockResolvedValueOnce(new Response('data: {"type":"image_generation.completed","b64_json":"iVBORw0KGgo="}\n\n', { headers: { 'Content-Type': 'text/event-stream' } }))
    const result = await callImageApi(options('gpt-image-2.5-flare', { apiKind: 'openai', streamingGeneration: true, params: { ...DEFAULT_PARAMS, partial_images: 1 } }))

    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toMatchObject({ stream: true, partial_images: 1 })
    expect(result.images).toEqual([IMAGE])
  })

  it('multiple Gemini images use separate native requests without an unsupported n field', async () => {
    const model = registeredModel('gemini-3.1-flash-image')
    const result = await callImageApi(options(model.id, { apiKind: 'gemini', modelCapabilities: model.capabilities, params: { ...DEFAULT_PARAMS, n: 2 } }))

    expect(result.images).toEqual([IMAGE, IMAGE])
    expect(fetchMock).toHaveBeenCalledTimes(2)
    for (const [, request] of fetchMock.mock.calls) expect(JSON.parse(request!.body as string)).not.toHaveProperty('n')
  })

  it('Gemini rejects a mask before requesting the upstream', async () => {
    await expect(callImageApi(options('gemini-3.1-flash-image', { apiKind: 'gemini', inputImageDataUrls: [IMAGE], maskDataUrl: IMAGE }))).rejects.toThrow()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
