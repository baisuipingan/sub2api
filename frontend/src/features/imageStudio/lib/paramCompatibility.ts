import { DEFAULT_PARAMS, type AppSettings, type TaskParams } from '../types'
import type { ImageModelDefinition } from './modelRegistry'
import { getImageModel, getModelResolution } from './modelRegistry'
import { calculateImageSize, GPT_IMAGE_MIN_PIXELS, normalizeImageSize } from './size'

export const MAX_OUTPUT_IMAGES = 5

export function normalizeParamsForSettings(params: TaskParams, settings: AppSettings, catalog?: readonly ImageModelDefinition[]) {
  const model = getImageModel(settings.selectedModelId, catalog)
  const capabilities = model?.capabilities
  const minPixels = capabilities?.customSize ? GPT_IMAGE_MIN_PIXELS : undefined
  const normalizedSize = normalizeImageSize(params.size, { minPixels }) || DEFAULT_PARAMS.size
  const requestedResolution = getModelResolution(model, normalizedSize, params.resolution)
  const resolution = capabilities?.resolutions.includes(requestedResolution)
    ? requestedResolution
    : model?.defaults.resolution ?? requestedResolution
  const supportsGeometry = capabilities?.customSize
    || capabilities?.resolutions.some((tier) => tier !== 'auto')
    || (capabilities?.aspectRatios.length ?? 0) > 1
  const size = capabilities && !supportsGeometry
    ? 'auto'
    : resolution !== 'auto' && (capabilities?.customSize || model?.api === 'openai')
    ? calculateImageSize(resolution, params.size === 'auto' ? model?.defaults.aspectRatio ?? '1:1' : params.size, { minPixels })
      ?? calculateImageSize(resolution, model?.defaults.aspectRatio ?? '1:1', { minPixels })
      ?? normalizedSize
    : normalizedSize
  const outputFormat = capabilities?.outputFormats.includes(params.output_format)
    ? params.output_format
    : model?.defaults.outputFormat ?? DEFAULT_PARAMS.output_format
  const next: TaskParams = {
    ...params,
    resolution,
    size,
    n: capabilities
      ? Math.min(MAX_OUTPUT_IMAGES, capabilities.imageCount.max, Math.max(capabilities.imageCount.min, params.n || model.defaults.imageCount))
      : DEFAULT_PARAMS.n,
    quality: capabilities?.qualities.includes(params.quality) ? params.quality : model?.defaults.quality ?? DEFAULT_PARAMS.quality,
    output_format: outputFormat,
    output_compression: capabilities?.outputCompression ? params.output_compression : null,
    moderation: capabilities?.moderation.includes(params.moderation) ? params.moderation : DEFAULT_PARAMS.moderation,
    transparent_output: Boolean(capabilities?.transparentOutput) && outputFormat !== 'jpeg'
      ? params.transparent_output
      : false,
    partial_images: capabilities
      ? Math.min(capabilities.maxPartialImages, Math.max(0, Math.floor(params.partial_images ?? 0))) as TaskParams['partial_images']
      : DEFAULT_PARAMS.partial_images,
  }

  if (next.output_format === 'png') next.output_compression = null
  if (next.output_format === 'jpeg') next.transparent_output = false
  return next
}

export function getChangedParams(current: TaskParams, next: TaskParams) {
  const patch: Partial<TaskParams> = {}
  for (const key of Object.keys(next) as Array<keyof TaskParams>) {
    if (current[key] !== next[key]) {
      (patch as Record<keyof TaskParams, TaskParams[keyof TaskParams]>)[key] = next[key]
    }
  }
  return patch
}
