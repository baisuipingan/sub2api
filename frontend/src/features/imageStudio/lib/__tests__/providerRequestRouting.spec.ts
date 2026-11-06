import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type AppSettings, type TaskParams } from '../../types'
import { callImageApi } from '../api'
import { createImageModelCatalogFromModelList, type ImageModelDefinition, type ImageModelProfile } from '../modelRegistry'
import { normalizeParamsForSettings } from '../paramCompatibility'

vi.mock('../i18nMessage', () => ({ libT: (key: string) => key }))

const PNG = 'data:image/png;base64,iVBORw0KGgo='
const JPEG = 'data:image/jpeg;base64,/9j/AQIDBA=='
const SETTINGS: AppSettings = {
  miloRouter: { baseUrl: '', apiKey: 'sk-test', rememberApiKey: false, connectionStatus: 'connected' },
  selectedModelId: '', connectionOnboardingSeen: true, timeout: 30,
  clearInputAfterSubmit: false, persistInputOnRestart: false, taskCompletionNotification: false, enterSubmit: false,
}
const fetchMock = vi.fn<typeof fetch>()

function catalogModel(id: string, api: 'openai' | 'gemini', profiles: ImageModelProfile[]) {
  return createImageModelCatalogFromModelList({ models: [{ id, api, profiles }] })[0]
}

function requestOptions(model: ImageModelDefinition, params: Partial<TaskParams> = {}, inputImageDataUrls: string[] = []) {
  const settings = { ...SETTINGS, selectedModelId: model.id }
  return {
    settings, modelId: model.id, apiKind: model.api, modelProvider: model.provider, modelCapabilities: model.capabilities,
    prompt: 'Draw a red cup',
    params: normalizeParamsForSettings({ ...DEFAULT_PARAMS, ...params }, settings, [model]),
    inputImageDataUrls,
  }
}

async function blobBytes(blob: Blob) {
  return new Promise<number[]>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve([...new Uint8Array(reader.result as ArrayBuffer)])
    reader.onerror = () => reject(reader.error)
    reader.readAsArrayBuffer(blob)
  })
}

beforeEach(() => {
  fetchMock.mockReset().mockImplementation(async (url) => {
    if (String(url).startsWith('/v1beta/')) {
      return new Response(JSON.stringify({ candidates: [{ content: { parts: [{ inlineData: { mimeType: 'image/png', data: 'iVBORw0KGgo=' } }] } }] }))
    }
    return new Response(JSON.stringify({ data: [{ b64_json: 'iVBORw0KGgo=', mime_type: 'image/png' }] }))
  })
  vi.stubGlobal('fetch', fetchMock)
})
afterEach(() => vi.unstubAllGlobals())

describe('provider profile to image request routing', () => {
  it.each([
    { id: 'gpt-image-2', api: 'openai' as const, provider: 'openai' as const, profile: 'gpt-image-2', fields: { size: '2048x2048' } },
    { id: 'gemini-3.1-flash-image', api: 'gemini' as const, provider: 'google' as const, profile: 'gemini-3.1-flash-image', fields: { generationConfig: { imageConfig: { imageSize: '2K' } } } },
    { id: 'grok-imagine-image-2.0', api: 'openai' as const, provider: 'xai' as const, profile: 'grok-imagine-image-2.0', fields: { resolution: '2k' } },
  ])('sends the initial explicit 2K tier through the $id adapter', async ({ id, api, provider, profile, fields }) => {
    const model = catalogModel(id, api, [{ provider, profile }])
    await callImageApi(requestOptions(model))
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toMatchObject(fields)
  })

  it.each([
    { id: 'gpt-image-2', api: 'openai' as const, provider: 'openai' as const, profile: 'gpt-image-2', fields: { size: '2048x2048' } },
    { id: 'gemini-3.1-flash-image', api: 'gemini' as const, provider: 'google' as const, profile: 'gemini-3.1-flash-image', fields: { generationConfig: { imageConfig: { imageSize: '2K', aspectRatio: '1:1' } } } },
  ])('sends a resolved explicit tier when the $id studio draft previously used auto', async ({ id, api, provider, profile, fields }) => {
    const model = catalogModel(id, api, [{ provider, profile }])
    await callImageApi(requestOptions(model, { resolution: 'auto' }))
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toMatchObject(fields)
  })

  it('keeps an arbitrary xAI public alias and sends n=10 in one Grok request', async () => {
    const model = catalogModel('my-painter', 'openai', [{ provider: 'xai', profile: 'grok-imagine-image-2.0' }])
    const opts = requestOptions(model, { n: 10, resolution: '2K', aspect_ratio: '21:9', quality: 'medium' })

    expect(model.provider).toBe('xai')
    expect(opts.params.n).toBe(10)
    await callImageApi(opts)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/generations')
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({
      model: 'my-painter', prompt: 'Draw a red cup', n: 10, response_format: 'b64_json', resolution: '2k', aspect_ratio: '21:9', quality: 'medium',
    })
  })

  it('routes five xAI edit references as JSON images with native geometry and no GPT fields', async () => {
    const model = catalogModel('custom-edit-service', 'openai', [{ provider: 'xai', profile: 'grok-imagine-image-2.0' }])
    const references = [PNG, JPEG, PNG, JPEG, PNG]
    await callImageApi(requestOptions(model, { resolution: '2K', aspect_ratio: '21:9', quality: 'medium', output_format: 'webp', moderation: 'low', output_compression: 80 }, references))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/edits')
    expect(fetchMock.mock.calls[0][1]!.headers).toMatchObject({ 'Content-Type': 'application/json' })
    const body = JSON.parse(fetchMock.mock.calls[0][1]!.body as string)
    expect(body).toEqual({
      model: 'custom-edit-service', prompt: 'Draw a red cup', n: 1, response_format: 'b64_json', resolution: '2k', aspect_ratio: '21:9', quality: 'medium',
      images: references.map((url) => ({ url, type: 'image_url' })),
    })
    for (const field of ['image', 'size', 'output_format', 'output_compression', 'moderation', 'stream', 'partial_images', 'background']) expect(body).not.toHaveProperty(field)
  })

  it('routes an arbitrary Google alias through the declared native API with 512 and an ultrawide ratio', async () => {
    const model = catalogModel('my-banana', 'gemini', [{ provider: 'google', profile: 'gemini-3.1-flash-image' }])
    const result = await callImageApi(requestOptions(model, { resolution: '0.5K', aspect_ratio: '8:1' }, [JPEG]))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/v1beta/models/my-banana:generateContent')
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({
      contents: [{ role: 'user', parts: [{ text: 'Draw a red cup' }, { inlineData: { mimeType: 'image/jpeg', data: '/9j/AQIDBA==' } }] }],
      generationConfig: { responseModalities: ['IMAGE'], imageConfig: { imageSize: '512', aspectRatio: '8:1' } },
    })
    expect(result.images).toEqual([PNG])
  })

  it('keeps Google-compatible Images on Images even when the parameter provider is Google', async () => {
    const model = catalogModel('my-compatible-banana', 'openai', [{ provider: 'google', profile: 'gemini-3.1-flash-image' }])
    await callImageApi(requestOptions(model, { resolution: '2K', size: '1024x1024' }))

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/generations')
    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).toEqual({ model: model.id, prompt: 'Draw a red cup', n: 1, size: '2048x2048' })
  })

  it('sends OpenAI 2.6 alias complexity and preserves original JPEG bytes in multipart edits', async () => {
    const model = catalogModel('my-detailed-editor', 'openai', [{ provider: 'openai', profile: 'gpt-image-2.6', transport: 'images' }])
    const opts = requestOptions(model, { complexity: 'high', resolution: '2K', size: '1024x1024', quality: 'high' }, [JPEG])
    await callImageApi(opts)

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/v1/images/edits')
    const request = fetchMock.mock.calls[0][1]!
    expect(request.headers).not.toHaveProperty('Content-Type')
    const form = request.body as FormData
    expect(form.get('model')).toBe('my-detailed-editor')
    expect(form.get('complexity')).toBe('high')
    expect(form.get('size')).toBe('2048x2048')
    expect(form.get('quality')).toBe('high')
    const image = form.get('image[]') as File
    expect(image.type).toBe('image/jpeg')
    expect(image.name).toBe('reference-1.jpeg')
    expect(await blobBytes(image)).toEqual([255, 216, 255, 1, 2, 3, 4])
  })

  it('does not send new Images-only complexity through the existing subscription bridge', async () => {
    const model = catalogModel('my-subscription-editor', 'openai', [{ provider: 'openai', profile: 'gpt-image-2.6', transport: 'bridge' }])
    const opts = requestOptions(model, { complexity: 'high' })
    expect(opts.params.complexity).toBeUndefined()
    await callImageApi(opts)

    expect(JSON.parse(fetchMock.mock.calls[0][1]!.body as string)).not.toHaveProperty('complexity')
  })
})
