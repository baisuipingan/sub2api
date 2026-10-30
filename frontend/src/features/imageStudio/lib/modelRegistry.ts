import type { ImageApiKind, ImageResolutionTier, TaskParams } from '../types'
import { getImageSizeTier } from './size'

export type { ImageResolutionTier } from '../types'

export const GPT_IMAGE_2_MODEL_ID = 'gpt-image-2'
export const GPT_IMAGE_2_5_LEGACY_MODEL_ID = 'gpt-image-2.5'
export const GPT_IMAGE_2_5_FLARE_MODEL_ID = 'gpt-image-2.5-flare'
export const GPT_IMAGE_2_5_SUNBURST_MODEL_ID = 'gpt-image-2.5-sunburst'
export const GEMINI_FLASH_IMAGE_MODEL_ID = 'gemini-3.1-flash-image'
export const GEMINI_PRO_IMAGE_MODEL_ID = 'gemini-3-pro-image'

export interface ImageModelCapabilities {
  resolutions: ImageResolutionTier[]
  customSize: boolean
  aspectRatios: string[]
  qualities: TaskParams['quality'][]
  outputFormats: TaskParams['output_format'][]
  outputCompression: boolean
  moderation: TaskParams['moderation'][]
  imageCount: { min: number; max: number }
  referenceImages: number
  mask: boolean
  transparentOutput: boolean
  nativeTransparentOutput: boolean
  streamingGeneration: boolean
  streamingEdit: boolean
  maxPartialImages: 0 | 1 | 2 | 3
}

export interface ImageModelDefaults {
  resolution: ImageResolutionTier
  aspectRatio: string
  quality: TaskParams['quality']
  outputFormat: TaskParams['output_format']
  imageCount: number
}

export interface ImageModelDefinition {
  id: string
  name: string
  api: ImageApiKind
  featured?: boolean
  requestModels: Partial<Record<ImageResolutionTier, string>>
  capabilities: ImageModelCapabilities
  defaults: ImageModelDefaults
}

const GPT_CAPABILITIES: ImageModelCapabilities = {
  resolutions: ['auto', '1K', '2K', '4K'],
  customSize: true,
  aspectRatios: ['1:1', '3:2', '2:3', '16:9', '9:16', '4:3', '3:4', '21:9'],
  qualities: ['auto', 'low', 'medium', 'high'],
  outputFormats: ['png', 'jpeg', 'webp'],
  outputCompression: true,
  moderation: ['auto', 'low'],
  imageCount: { min: 1, max: 5 },
  referenceImages: 16,
  mask: true,
  transparentOutput: true,
  nativeTransparentOutput: true,
  streamingGeneration: false,
  streamingEdit: false,
  maxPartialImages: 0,
}

export type ImageEditProfile = 'standard' | 'fast' | 'precision'

export function getImageEditProfile(model: ImageModelDefinition): ImageEditProfile {
  if (model.id === GPT_IMAGE_2_5_FLARE_MODEL_ID) return 'fast'
  if (model.id === GPT_IMAGE_2_5_SUNBURST_MODEL_ID) return 'precision'
  return 'standard'
}

export function getImageEditProfileLabel(model: ImageModelDefinition): string {
  const profile = getImageEditProfile(model)
  if (profile === 'fast') return '快速编辑'
  if (profile === 'precision') return '精确编辑'
  return '标准编辑'
}

const GPT_IMAGE_2_5_CAPABILITIES: ImageModelCapabilities = {
  ...GPT_CAPABILITIES,
  qualities: ['auto', 'low', 'medium', 'high', 'xhigh', 'max'],
  outputFormats: ['png'],
  outputCompression: false,
  moderation: ['auto'],
  streamingGeneration: true,
  streamingEdit: true,
  maxPartialImages: 0,
}

const GEMINI_CAPABILITIES: ImageModelCapabilities = {
  resolutions: ['auto', '1K', '2K', '4K'],
  customSize: false,
  aspectRatios: ['1:1', '3:2', '2:3', '16:9', '9:16', '4:3', '3:4', '21:9'],
  qualities: [],
  outputFormats: [],
  outputCompression: false,
  moderation: [],
  imageCount: { min: 1, max: 1 },
  referenceImages: 14,
  mask: false,
  transparentOutput: false,
  nativeTransparentOutput: false,
  streamingGeneration: false,
  streamingEdit: false,
  maxPartialImages: 0,
}

const DEFAULTS: ImageModelDefaults = {
  resolution: 'auto',
  aspectRatio: '1:1',
  quality: 'auto',
  outputFormat: 'png',
  imageCount: 1,
}

export const IMAGE_MODEL_REGISTRY: readonly ImageModelDefinition[] = [
  {
    id: GPT_IMAGE_2_MODEL_ID,
    name: 'GPT Image 2',
    api: 'openai',
    featured: true,
    requestModels: { auto: GPT_IMAGE_2_MODEL_ID, '1K': GPT_IMAGE_2_MODEL_ID, '2K': 'gpt-image-2-4K', '4K': 'gpt-image-2-4K' },
    capabilities: GPT_CAPABILITIES,
    defaults: DEFAULTS,
  },
  {
    id: GEMINI_FLASH_IMAGE_MODEL_ID,
    name: 'Nano Banana 2',
    api: 'gemini',
    featured: true,
    requestModels: { auto: GEMINI_FLASH_IMAGE_MODEL_ID, '1K': GEMINI_FLASH_IMAGE_MODEL_ID, '2K': GEMINI_FLASH_IMAGE_MODEL_ID, '4K': GEMINI_FLASH_IMAGE_MODEL_ID },
    capabilities: GEMINI_CAPABILITIES,
    defaults: DEFAULTS,
  },
  {
    id: GEMINI_PRO_IMAGE_MODEL_ID,
    name: 'Nano Banana Pro',
    api: 'gemini',
    requestModels: { auto: GEMINI_PRO_IMAGE_MODEL_ID, '1K': GEMINI_PRO_IMAGE_MODEL_ID, '2K': GEMINI_PRO_IMAGE_MODEL_ID, '4K': GEMINI_PRO_IMAGE_MODEL_ID },
    capabilities: GEMINI_CAPABILITIES,
    defaults: DEFAULTS,
  },
  {
    id: GPT_IMAGE_2_5_FLARE_MODEL_ID,
    name: 'GPT Image 2.5 Flare（快速）',
    api: 'openai',
    featured: true,
    requestModels: { auto: GPT_IMAGE_2_5_FLARE_MODEL_ID, '1K': GPT_IMAGE_2_5_FLARE_MODEL_ID, '2K': GPT_IMAGE_2_5_FLARE_MODEL_ID, '4K': GPT_IMAGE_2_5_FLARE_MODEL_ID },
    capabilities: GPT_IMAGE_2_5_CAPABILITIES,
    defaults: DEFAULTS,
  },
  {
    id: GPT_IMAGE_2_5_SUNBURST_MODEL_ID,
    name: 'GPT Image 2.5 Sunburst（精确编辑）',
    api: 'openai',
    featured: true,
    requestModels: { auto: GPT_IMAGE_2_5_SUNBURST_MODEL_ID, '1K': GPT_IMAGE_2_5_SUNBURST_MODEL_ID, '2K': GPT_IMAGE_2_5_SUNBURST_MODEL_ID, '4K': GPT_IMAGE_2_5_SUNBURST_MODEL_ID },
    capabilities: GPT_IMAGE_2_5_CAPABILITIES,
    defaults: DEFAULTS,
  },
]

const RESOLUTIONS: ImageResolutionTier[] = ['auto', '1K', '2K', '4K']
const QUALITIES: TaskParams['quality'][] = ['auto', 'low', 'medium', 'high', 'xhigh', 'max']
const OUTPUT_FORMATS: TaskParams['output_format'][] = ['png', 'jpeg', 'webp']
const MODERATION: TaskParams['moderation'][] = ['auto', 'low']

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function stringArray<T extends string>(value: unknown, allowed?: readonly T[]) {
  if (!Array.isArray(value) || value.some((item) => typeof item !== 'string')) return null
  const items = [...new Set(value.map((item) => item.trim()).filter(Boolean))]
  if (allowed && items.some((item) => !allowed.includes(item as T))) return null
  return items as T[]
}

function parseCatalogItem(value: unknown): ImageModelDefinition | null {
  if (!isRecord(value) || typeof value.id !== 'string' || !value.id.trim() || typeof value.name !== 'string' || !value.name.trim()) return null
  if (value.api !== 'openai' && value.api !== 'gemini') return null
  if (!isRecord(value.requestModels) || !isRecord(value.capabilities) || !isRecord(value.defaults)) return null

  const requestModels: Partial<Record<ImageResolutionTier, string>> = {}
  for (const tier of RESOLUTIONS) {
    const modelId = value.requestModels[tier]
    if (typeof modelId === 'string' && modelId.trim()) requestModels[tier] = modelId.trim()
  }
  if (!Object.keys(requestModels).length) return null

  const resolutions = stringArray(value.capabilities.resolutions, RESOLUTIONS)
  const aspectRatios = stringArray(value.capabilities.aspectRatios)
  const qualities = stringArray(value.capabilities.qualities, QUALITIES)
  const outputFormats = stringArray(value.capabilities.outputFormats, OUTPUT_FORMATS)
  const moderation = stringArray(value.capabilities.moderation, MODERATION)
  const imageCount = value.capabilities.imageCount
  if (!resolutions?.length || !aspectRatios?.length || !qualities || !outputFormats || !moderation || !isRecord(imageCount)) return null
  if (typeof imageCount.min !== 'number' || typeof imageCount.max !== 'number' || imageCount.min < 1 || imageCount.max < imageCount.min) return null
  if (typeof value.capabilities.referenceImages !== 'number' || value.capabilities.referenceImages < 0) return null
  for (const field of ['customSize', 'outputCompression', 'mask', 'transparentOutput'] as const) {
    if (typeof value.capabilities[field] !== 'boolean') return null
  }
  const nativeTransparentOutput = value.capabilities.nativeTransparentOutput === true
  const streamingGeneration = value.capabilities.streamingGeneration === true
  const streamingEdit = value.capabilities.streamingEdit === true
  const maxPartialImages = Number.isInteger(value.capabilities.maxPartialImages)
    ? Math.min(3, Math.max(0, Number(value.capabilities.maxPartialImages))) as 0 | 1 | 2 | 3
    : 0

  const resolution = value.defaults.resolution
  const aspectRatio = value.defaults.aspectRatio
  const quality = value.defaults.quality
  const outputFormat = value.defaults.outputFormat
  const imageCountDefault = value.defaults.imageCount
  if (!RESOLUTIONS.includes(resolution as ImageResolutionTier) || !resolutions.includes(resolution as ImageResolutionTier)) return null
  if (typeof aspectRatio !== 'string' || !aspectRatio.trim()) return null
  if (!QUALITIES.includes(quality as TaskParams['quality']) || !OUTPUT_FORMATS.includes(outputFormat as TaskParams['output_format'])) return null
  if (typeof imageCountDefault !== 'number' || imageCountDefault < imageCount.min || imageCountDefault > imageCount.max) return null

  const isGptImage2 = value.id.trim() === GPT_IMAGE_2_MODEL_ID
  if (isGptImage2) {
    if (!requestModels.auto && requestModels['1K']) requestModels.auto = requestModels['1K']
    if (!requestModels['1K'] && requestModels.auto) requestModels['1K'] = requestModels.auto
    if (requestModels['4K']) requestModels['2K'] = requestModels['4K']
    else delete requestModels['2K']
  }
  const availableResolutions = isGptImage2
    ? RESOLUTIONS.filter((tier) => Boolean(requestModels[tier]) && (
      resolutions.includes(tier)
      || tier === 'auto' && resolutions.includes('1K')
      || tier === '1K' && resolutions.includes('auto')
    ))
    : resolutions
  if (!availableResolutions.length) return null
  const defaultResolution = resolution as ImageResolutionTier

  return {
    id: value.id.trim(),
    name: value.name.trim(),
    api: value.api,
    featured: value.featured === true,
    requestModels,
    capabilities: {
      resolutions: availableResolutions,
      customSize: value.capabilities.customSize === true,
      aspectRatios,
      qualities,
      outputFormats,
      outputCompression: value.capabilities.outputCompression === true,
      moderation,
      imageCount: { min: Math.floor(imageCount.min), max: Math.floor(imageCount.max) },
      referenceImages: Math.floor(value.capabilities.referenceImages),
      mask: value.capabilities.mask === true,
      transparentOutput: value.capabilities.transparentOutput === true,
      nativeTransparentOutput,
      streamingGeneration,
      streamingEdit,
      maxPartialImages,
    },
    defaults: {
      resolution: availableResolutions.includes(defaultResolution) ? defaultResolution : availableResolutions[0],
      aspectRatio: aspectRatio.trim(),
      quality: quality as TaskParams['quality'],
      outputFormat: outputFormat as TaskParams['output_format'],
      imageCount: Math.floor(imageCountDefault),
    },
  }
}

export function parseImageModelCatalog(payload: unknown) {
  if (!isRecord(payload) || payload.version !== 1 || !Array.isArray(payload.data)) throw new Error('模型列表格式不受支持')
  const seen = new Set<string>()
  return payload.data
    .map(parseCatalogItem)
    .filter((item): item is ImageModelDefinition => Boolean(item))
    .filter((item) => {
      if (seen.has(item.id)) return false
      seen.add(item.id)
      return true
    })
    .sort((a, b) => Number(Boolean(b.featured)) - Number(Boolean(a.featured)))
}

function getAvailableModelIds(payload: unknown) {
  if (!isRecord(payload) || !Array.isArray(payload.data)) throw new Error('模型列表格式不受支持')

  return new Set(payload.data.flatMap((item) => {
    if (!isRecord(item) || typeof item.id !== 'string' || !item.id.trim()) return []
    return [item.id.trim()]
  }))
}

export function filterGeminiImageModelsByNativeModelList(catalog: readonly ImageModelDefinition[], payload: unknown) {
  if (!isRecord(payload) || !Array.isArray(payload.models)) throw new Error('Gemini 模型列表格式不受支持')

  const availableIds = new Set(payload.models.flatMap((item) => {
    if (!isRecord(item)) return []
    const modelId = typeof item.name === 'string' ? item.name : typeof item.id === 'string' ? item.id : ''
    return modelId.trim() ? [modelId.trim().replace(/^models\//, '')] : []
  }))

  return catalog.flatMap<ImageModelDefinition>((model) => {
    if (model.api !== 'gemini') return [{ ...model }]

    const requestModel = [...new Set([
      ...Object.values(model.requestModels),
      model.id,
      `${model.id}-preview`,
    ])].find((modelId) => availableIds.has(modelId))
    if (!requestModel) return []

    return [{
      ...model,
      requestModels: Object.fromEntries(model.capabilities.resolutions.map((tier) => [tier, requestModel])) as ImageModelDefinition['requestModels'],
    }]
  })
}

export function createImageModelCatalogFromModelList(payload: unknown) {
  const availableIds = getAvailableModelIds(payload)
  const findAvailable = (candidates: string[]) => candidates.find((id) => availableIds.has(id))

  return IMAGE_MODEL_REGISTRY.flatMap<ImageModelDefinition>((model) => {
    if (model.id === GPT_IMAGE_2_MODEL_ID) {
      const base = findAvailable(['gpt-image-2'])
      const model4K = findAvailable(['gpt-image-2-4K', 'gpt-image-2-4k'])
      const fallback = base ?? model4K
      if (!fallback) return []

      const requestModels: ImageModelDefinition['requestModels'] = {}
      if (base) {
        requestModels.auto = base
        requestModels['1K'] = base
      }
      if (model4K) {
        requestModels['2K'] = model4K
        requestModels['4K'] = model4K
      }
      return [{
        ...model,
        requestModels,
        defaults: { ...model.defaults, resolution: base ? 'auto' : '4K' },
        capabilities: {
          ...model.capabilities,
          resolutions: RESOLUTIONS.filter((tier) => Boolean(requestModels[tier])),
        },
      }]
    }

    if (model.id === GPT_IMAGE_2_5_FLARE_MODEL_ID || model.id === GPT_IMAGE_2_5_SUNBURST_MODEL_ID) {
      const requestModel = findAvailable([model.id])
      if (!requestModel) return []
      return [{
        ...model,
        requestModels: Object.fromEntries(RESOLUTIONS.map((tier) => [tier, requestModel])) as ImageModelDefinition['requestModels'],
      }]
    }

    const requestModel = findAvailable([
      model.id,
      `${model.id}-preview`,
    ])
    if (!requestModel) return []
    return [{
      ...model,
      requestModels: Object.fromEntries(RESOLUTIONS.map((tier) => [tier, requestModel])) as ImageModelDefinition['requestModels'],
    }]
  })
}

export function getImageModel(modelId: string, catalog?: readonly ImageModelDefinition[]) {
  const id = modelId.trim()
  const source = catalog ?? IMAGE_MODEL_REGISTRY
  return source.find((model) => model.id === id || Object.values(model.requestModels).includes(id)) ?? null
}

export function migrateLegacyImageModelId(modelId: string) {
  return modelId.trim() === GPT_IMAGE_2_5_LEGACY_MODEL_ID ? GPT_IMAGE_2_5_FLARE_MODEL_ID : modelId.trim()
}

export function getModelResolution(_model: ImageModelDefinition | null, size: string, resolution?: ImageResolutionTier): ImageResolutionTier {
  return resolution ?? (size === 'auto' ? 'auto' : getImageSizeTier(size))
}

export function getRequestModelId(model: ImageModelDefinition, size: string, resolution?: ImageResolutionTier) {
  const tier = getModelResolution(model, size, resolution)
  return model.requestModels[tier]
    ?? model.requestModels.auto
    ?? Object.values(model.requestModels)[0]
    ?? ''
}
