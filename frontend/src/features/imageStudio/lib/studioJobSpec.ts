import type { CallApiOptions } from './imageApiShared'
import { buildGeminiImageRequest } from './geminiImageApi'
import { buildGrokImageRequest } from './grokImageApi'
import { imageRequestParams, shouldRequestTransparentBackground } from './openaiImageApi'
import type { StudioJobSpec, StudioPostprocess } from './studioJobsApi'

/** Build the durable spec from the same request builders used by the live UI. */
export function buildStudioJobSpec(
  opts: CallApiOptions,
  ids: {
    inputImageIds: string[]
    requestInputImageIds: string[]
    maskImageId?: string
    maskTargetImageId?: string
    providerProfile?: string
    postprocess: StudioPostprocess
  },
): StudioJobSpec {
  const model = opts.modelId ?? opts.settings.selectedModelId
  if (!model) throw new Error('Image model is required')
  if (ids.inputImageIds.length !== ids.requestInputImageIds.length || ids.inputImageIds.length !== opts.inputImageDataUrls.length) {
    throw new Error('Studio image references are incomplete')
  }
  if (ids.postprocess === 'mask' && (!opts.maskDataUrl || !ids.maskImageId || !ids.maskTargetImageId
    || !ids.requestInputImageIds[0] || ids.requestInputImageIds[0] === ids.inputImageIds[0])) {
    throw new Error('Prepared mask input is required for a cloud edit')
  }

  const base = {
    ...(ids.providerProfile ? { provider_profile: ids.providerProfile } : {}),
    model,
    input_image_ids: ids.inputImageIds,
    request_input_image_ids: ids.requestInputImageIds,
    ...(ids.maskImageId ? { mask_image_id: ids.maskImageId } : {}),
    ...(ids.maskTargetImageId ? { mask_target_image_id: ids.maskTargetImageId } : {}),
    postprocess: ids.postprocess,
    ...(ids.postprocess !== 'none' ? { output_format: opts.params.output_format } : {}),
    ...(ids.postprocess !== 'none' && opts.params.output_compression != null
      ? { output_compression: opts.params.output_compression }
      : {}),
    image_count: opts.params.n,
  }

  if (opts.apiKind === 'gemini') {
    if (opts.maskDataUrl) throw new Error('Gemini image editing does not support a mask')
    const body = buildGeminiImageRequest(opts.prompt, opts.params, [], opts.modelCapabilities)
    return { ...base, protocol: 'gemini', prompt: opts.prompt, params: { generationConfig: body.generationConfig } }
  }

  if (opts.modelProvider === 'xai') {
    const body = buildGrokImageRequest(opts)
    const params: Record<string, unknown> = { ...body }
    delete params.model
    delete params.prompt
    delete params.image
    delete params.images
    return { ...base, protocol: 'grok', prompt: body.prompt, params }
  }

  const params: Record<string, unknown> = { ...imageRequestParams(opts, model) }
  if (shouldRequestTransparentBackground(opts.params, Boolean(opts.nativeTransparentOutput), Boolean(opts.maskDataUrl))) {
    params.background = 'transparent'
  }
  return { ...base, protocol: 'openai', prompt: opts.prompt, params }
}
