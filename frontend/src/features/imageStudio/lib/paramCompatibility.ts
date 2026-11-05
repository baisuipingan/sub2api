import { DEFAULT_PARAMS, type AppSettings, type TaskParams } from '../types'
import type { ImageModelDefinition } from './modelRegistry'
import { getImageModel, getModelResolution } from './modelRegistry'
import { calculateImageSize, GPT_IMAGE_MIN_PIXELS, normalizeImageSize, parseRatio } from './size'

export const MAX_OUTPUT_IMAGES = 10

export function isResolutionAvailable(model: ImageModelDefinition | null, resolution: TaskParams['resolution']) {
  if (!model?.availableSizeTiers) return true
  // The Grok adapter explicitly sends its documented 1K default for auto.
  if (resolution === 'auto') return model.provider !== 'xai' || model.availableSizeTiers.includes('1K')
  return model.availableSizeTiers.includes(resolution === '0.5K' ? '1K' : resolution)
}

/** Account tier routing classifies the actual requested pixels by longest edge. */
export function isRequestedImageSizeAvailable(model: ImageModelDefinition, params: TaskParams) {
  if (!model.availableSizeTiers) return true
  if (usesNativeGeometry(model)) return isResolutionAvailable(model, params.resolution)
  const dimensions = params.size.match(/^(\d+)x(\d+)$/i)
  if (!dimensions) return isResolutionAvailable(model, params.resolution)
  const edge = Math.max(Number(dimensions[1]), Number(dimensions[2]))
  return model.availableSizeTiers.includes(edge <= 1024 ? '1K' : edge <= 2048 ? '2K' : '4K')
}

export function usesNativeGeometry(model: ImageModelDefinition | null | undefined) {
  return model?.api === 'gemini' || model?.provider === 'xai'
}

export function closestSupportedRatio(size: string, ratios: readonly string[]) {
  const dimensions = parseRatio(size)
  if (!dimensions) return undefined
  const ratio = dimensions.width / dimensions.height
  const nearest = ratios.map((value) => {
    const parsed = parseRatio(value)
    return { value, delta: parsed ? Math.abs(Math.log(ratio / (parsed.width / parsed.height))) : Infinity }
  }).sort((a, b) => a.delta - b.delta)[0]
  return nearest && nearest.delta < 0.02 ? nearest.value : undefined
}

export function parameterImageSize(model: ImageModelDefinition, resolution: TaskParams['resolution'], ratio: string) {
  const fixed = model.capabilities.fixedSizes
  if (fixed?.length) return fixed.find((size) => closestSupportedRatio(size, [ratio])) ?? fixed[0]
  const tier = resolution === 'auto' || resolution === '0.5K' ? '1K' : resolution
  return calculateImageSize(tier, ratio, model.capabilities.customSize ? { minPixels: GPT_IMAGE_MIN_PIXELS } : undefined)
}

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
  const nativeGeometry = usesNativeGeometry(model)
  const aspectRatio = nativeGeometry
    ? capabilities?.aspectRatios.includes(params.aspect_ratio ?? '') ? params.aspect_ratio : params.aspect_ratio === 'auto' ? 'auto' : closestSupportedRatio(params.size, capabilities?.aspectRatios ?? []) ?? 'auto'
    : undefined
  let size = normalizedSize
  if (!nativeGeometry) {
    if (capabilities && !supportsGeometry) size = 'auto'
    else if (model && (resolution !== 'auto' || capabilities?.fixedSizes?.length) && (capabilities?.customSize || model.api === 'openai')) {
      size = params.size === 'auto' && resolution === 'auto' ? 'auto' : parameterImageSize(model, resolution, params.size === 'auto' ? model.defaults.aspectRatio : params.size) ?? normalizedSize
    }
  }
  const outputFormat = capabilities?.outputFormats.includes(params.output_format)
    ? params.output_format
    : model?.defaults.outputFormat ?? DEFAULT_PARAMS.output_format
  const next: TaskParams = {
    ...params,
    aspect_ratio: aspectRatio,
    complexity: capabilities?.complexities?.includes(params.complexity ?? 'auto') ? params.complexity ?? 'auto' : undefined,
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
  if (model && !isRequestedImageSizeAvailable(model, next)) {
    next.resolution = capabilities?.resolutions.find((tier) => isResolutionAvailable(model, tier)) ?? 'auto'
    next.size = nativeGeometry || next.resolution === 'auto'
      ? 'auto'
      : parameterImageSize(model, next.resolution, model.defaults.aspectRatio) ?? 'auto'
  }
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
