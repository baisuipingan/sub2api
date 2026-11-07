import { describe, expect, it } from 'vitest'
import { DEFAULT_PARAMS, type AppSettings } from '../../types'
import { getImageModel } from '../modelRegistry'
import { buildStudioJobSpec } from '../studioJobSpec'

const settings: AppSettings = {
  miloRouter: { baseUrl: '', apiKey: 'secret-must-never-appear', rememberApiKey: false, connectionStatus: 'connected' },
  selectedModelId: 'gpt-image-2',
  connectionOnboardingSeen: true,
  timeout: 180,
  clearInputAfterSubmit: false,
  persistInputOnRestart: true,
  taskCompletionNotification: false,
  enterSubmit: false,
}

describe('Studio durable job spec parity', () => {
  it('copies OpenAI size/quality/background fields without a browser key or image bytes', () => {
    const model = getImageModel('gpt-image-2')!
    const spec = buildStudioJobSpec({
      settings,
      modelId: model.id,
      apiKind: model.api,
      modelProvider: model.provider,
      modelCapabilities: model.capabilities,
      prompt: 'A red figure',
      params: { ...DEFAULT_PARAMS, size: '2048x2048', resolution: '2K', quality: 'high', transparent_output: true },
      inputImageDataUrls: [],
      nativeTransparentOutput: true,
    }, { inputImageIds: [], requestInputImageIds: [], postprocess: 'none' })

    expect(spec.protocol).toBe('openai')
    expect(spec.params).toMatchObject({ size: '2048x2048', quality: 'high', background: 'transparent', n: 1 })
    expect(JSON.stringify(spec)).not.toContain('secret-must-never-appear')
    expect(JSON.stringify(spec)).not.toContain('data:image/')
  })

  it('preserves Grok native n and mention conversion while keeping references as asset IDs', () => {
    const model = getImageModel('grok-imagine-image')!
    const spec = buildStudioJobSpec({
      settings,
      modelId: model.id,
      apiKind: model.api,
      modelProvider: 'xai',
      modelCapabilities: model.capabilities,
      prompt: 'Use [image 1] as the subject',
      params: { ...DEFAULT_PARAMS, resolution: '2K', size: '2048x2048', n: 3 },
      inputImageDataUrls: ['data:image/png;base64,AQID'],
    }, {
      inputImageIds: ['input-1'],
      requestInputImageIds: ['input-1'],
      postprocess: 'none',
    })

    expect(spec.protocol).toBe('grok')
    expect(spec.prompt).toBe('Use <IMAGE_0> as the subject')
    expect(spec.params).toMatchObject({ n: 3, response_format: 'b64_json', resolution: '2k' })
    expect(spec.request_input_image_ids).toEqual(['input-1'])
    expect(JSON.stringify(spec)).not.toContain('data:image/')
  })

  it('uses the Gemini imageConfig builder and rejects missing input IDs', () => {
    const model = getImageModel('gemini-3-pro-image')!
    const opts = {
      settings,
      modelId: model.id,
      apiKind: model.api,
      modelProvider: 'google' as const,
      modelCapabilities: model.capabilities,
      prompt: 'A landscape',
      params: { ...DEFAULT_PARAMS, resolution: '2K' as const, size: '2048x1152', aspect_ratio: '16:9' },
      inputImageDataUrls: [],
    }
    const spec = buildStudioJobSpec(opts, { inputImageIds: [], requestInputImageIds: [], postprocess: 'none' })
    expect(spec.protocol).toBe('gemini')
    expect(spec.params).toMatchObject({ generationConfig: { responseModalities: ['IMAGE'], imageConfig: { aspectRatio: '16:9', imageSize: '2K' } } })
    expect(() => buildStudioJobSpec({ ...opts, inputImageDataUrls: ['data:image/png;base64,AQID'] }, {
      inputImageIds: ['input-1'], requestInputImageIds: [], postprocess: 'none',
    })).toThrow('Studio image references are incomplete')
  })

  it('requires a separately uploaded masked request image for region edits', () => {
    const model = getImageModel('gpt-image-2')!
    const opts = {
      settings,
      modelId: model.id,
      apiKind: model.api,
      modelProvider: model.provider,
      modelCapabilities: model.capabilities,
      prompt: 'Replace a small region',
      params: DEFAULT_PARAMS,
      inputImageDataUrls: ['data:image/png;base64,AQID'],
      maskDataUrl: 'data:image/png;base64,BAUG',
    }
    const ids = {
      inputImageIds: ['original-1'],
      requestInputImageIds: ['original-1'],
      maskImageId: 'mask-1',
      maskTargetImageId: 'original-1',
      postprocess: 'mask' as const,
    }
    expect(() => buildStudioJobSpec(opts, ids)).toThrow('Prepared mask input is required')
    const spec = buildStudioJobSpec(opts, { ...ids, requestInputImageIds: ['prepared-1'] })
    expect(spec.request_input_image_ids).toEqual(['prepared-1'])
    expect(spec.input_image_ids).toEqual(['original-1'])
    expect(spec.mask_image_id).toBe('mask-1')
    expect(spec.postprocess).toBe('mask')
  })
})
