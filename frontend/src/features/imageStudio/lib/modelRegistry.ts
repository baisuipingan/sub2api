import { libT } from './i18nMessage'
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
    // 客户端统一使用公开模型名；官方调度器按 size 选择账号，
    // 再通过该账号的普通模型映射使用供应商型号。
    requestModels: { auto: GPT_IMAGE_2_MODEL_ID, '1K': GPT_IMAGE_2_MODEL_ID, '2K': GPT_IMAGE_2_MODEL_ID, '4K': GPT_IMAGE_2_MODEL_ID },
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
    // 兼容旧目录中的 auto / 1K 别名；其他已声明档位保持原样。
    if (!requestModels.auto && requestModels['1K']) requestModels.auto = requestModels['1K']
    if (!requestModels['1K'] && requestModels.auto) requestModels['1K'] = requestModels.auto
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
  if (!isRecord(payload) || payload.version !== 1 || !Array.isArray(payload.data)) throw new Error(libT('imageStudio.lib.modelListFormatUnsupported'))
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

/**
 * 从 /v1/models 的返回里取出模型 id 集合。
 *
 * 兼容三种常见形状，避免因为上游格式差异导致整个工作台「模型列表加载失败」：
 *   { data:   [{ id }] }        OpenAI 风格
 *   { models: [{ name }] }      Gemini 原生风格（name 带 models/ 前缀）
 *   [ "id", ... ]               裸数组
 */
function extractModelItems(payload: unknown): unknown[] | null {
  if (Array.isArray(payload)) return payload
  if (!isRecord(payload)) return null
  for (const key of ['data', 'models', 'items']) {
    if (Array.isArray(payload[key])) return payload[key] as unknown[]
  }
  return null
}

function getAvailableModelIds(payload: unknown) {
  const items = extractModelItems(payload)
  if (!items) throw new Error(libT('imageStudio.lib.modelListFormatUnsupported'))

  const ids = new Set<string>()
  for (const item of items) {
    if (typeof item === 'string') {
      const trimmed = item.trim().replace(/^models\//, '')
      if (trimmed) ids.add(trimmed)
      continue
    }
    if (!isRecord(item)) continue

    const raw =
      typeof item.id === 'string'
        ? item.id
        : typeof item.name === 'string'
          ? item.name
          : ''
    const normalized = raw.trim().replace(/^models\//, '')
    if (normalized) ids.add(normalized)
  }
  return ids
}

export function filterGeminiImageModelsByNativeModelList(catalog: readonly ImageModelDefinition[], payload: unknown) {
  if (!isRecord(payload) || !Array.isArray(payload.models)) throw new Error(libT('imageStudio.lib.geminiModelListFormatUnsupported'))

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

/**
 * 判断某个模型名是否「看起来是生图模型」。
 *
 * /v1/models 只返回 id，没有能力字段，只能按命名推断。
 * 分组里常混着文本模型与视频模型（如 grok-4.5 / grok-imagine-video），
 * 若一并塞进生图模型列表，用户选中后会直接报错，因此要显式排除。
 */
const IMAGE_MODEL_HINT = /(?:^|[-_.])(?:image|imagine-image|imagen|dall-e|flux|seedream|ideogram|recraft)(?:[-_.]|$)/i
const NON_IMAGE_HINT = /video|tts|whisper|embed|rerank|moderation|transcri/i

function isImageModelId(modelId: string) {
  return IMAGE_MODEL_HINT.test(modelId) && !NON_IMAGE_HINT.test(modelId)
}

/** 把 gpt-image-2-4K 这类分辨率变体归并到基础模型名下 */
function getBaseModelId(modelId: string) {
  return modelId.replace(/[-_.](1k|2k|4k|hd)$/i, '')
}

/**
 * 为「注册表里没有、但分组确实提供」的生图模型生成兜底定义。
 *
 * 能力取最保守的一组：只开放 auto 分辨率、单张、无参考图、无遮罩。
 * 猜错能力的代价是请求被上游拒绝，比少给几个选项严重得多；
 * 等确认该模型的实际能力后，再把它加进 IMAGE_MODEL_REGISTRY 即可获得完整选项。
 */
/** 已知缩写保持大写，其余首字母大写，如 grok-imagine-image-2.0 → Grok Imagine Image 2.0 */
const ACRONYMS: Record<string, string> = {
  gpt: 'GPT',
  ai: 'AI',
  hd: 'HD',
  sdxl: 'SDXL',
  api: 'API',
}

function humanizeModelId(modelId: string) {
  return modelId
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => {
      const lower = part.toLowerCase()
      if (ACRONYMS[lower]) return ACRONYMS[lower]
      // 纯版本号（2.5 / 2）原样保留
      if (/^[0-9.]+$/.test(part)) return part
      return part.charAt(0).toUpperCase() + part.slice(1)
    })
    .join(' ')
}

function createFallbackImageModel(modelId: string): ImageModelDefinition {
  const isGemini = /^(?:gemini|imagen)/i.test(modelId)

  return {
    id: modelId,
    name: humanizeModelId(modelId),
    api: isGemini ? 'gemini' : 'openai',
    requestModels: Object.fromEntries(
      RESOLUTIONS.map((tier) => [tier, modelId]),
    ) as ImageModelDefinition['requestModels'],
    capabilities: {
      resolutions: ['auto'],
      customSize: false,
      aspectRatios: ['1:1'],
      qualities: ['auto'],
      outputFormats: ['png'],
      outputCompression: false,
      moderation: ['auto'],
      imageCount: { min: 1, max: 1 },
      referenceImages: 0,
      mask: false,
      transparentOutput: false,
      nativeTransparentOutput: false,
      streamingGeneration: false,
      streamingEdit: false,
      maxPartialImages: 0,
    },
    defaults: { ...DEFAULTS },
  }
}

/**
 * 从 /v1/models 的返回构建生图模型列表。
 *
 * 与原实现的区别：不再「只遍历写死的注册表」，而是以分组真实提供的模型为准 ——
 * 注册表里有精细配置的沿用配置，没有的按命名补齐并给保守能力。
 * 这样管理员在分组里加了生图模型，工作台不用改代码就能看到。
 */
export function createImageModelCatalogFromModelList(payload: unknown) {
  const availableIds = getAvailableModelIds(payload)
  const findAvailable = (candidates: string[]) => candidates.find((id) => availableIds.has(id))
  const consumed = new Set<string>()
  const catalog: ImageModelDefinition[] = []

  const consume = (model: ImageModelDefinition) => {
    for (const id of Object.values(model.requestModels)) consumed.add(id)
    consumed.add(model.id)
  }

  // ① 先处理注册表里的已知模型，保留它们精细的能力配置
  for (const model of IMAGE_MODEL_REGISTRY) {
    if (model.id === GPT_IMAGE_2_MODEL_ID) {
      // fork: image-size-routing
      // 不再探测 gpt-image-2-4K 并把它绑到 2K/4K。
      //
      // 旧逻辑会看分组里有没有 gpt-image-2-4K：有就让 2K 和 4K 都发这个名字。
      // 现在档位由 size 参数表达、路由层按 size 选供应商，
      // 客户端只发基础模型名即可。继续绑 4K 会让 2K 请求也走 4K 档，
      // 表现为「2K/4K 轮着调但都算 4K」。
      const base = findAvailable(['gpt-image-2'])
      if (!base) continue

      const requestModels: ImageModelDefinition['requestModels'] = {
        auto: base,
        '1K': base,
        '2K': base,
        '4K': base,
      }
      const built: ImageModelDefinition = {
        ...model,
        requestModels,
        defaults: { ...model.defaults, resolution: 'auto' },
        capabilities: {
          ...model.capabilities,
          resolutions: RESOLUTIONS.filter((tier) => Boolean(requestModels[tier])),
        },
      }
      consume(built)
      catalog.push(built)
      continue
    }

    const candidates = model.id === GPT_IMAGE_2_5_FLARE_MODEL_ID || model.id === GPT_IMAGE_2_5_SUNBURST_MODEL_ID
      ? [model.id]
      : [model.id, `${model.id}-preview`]
    const requestModel = findAvailable(candidates)
    if (!requestModel) continue

    const built: ImageModelDefinition = {
      ...model,
      requestModels: Object.fromEntries(
        RESOLUTIONS.map((tier) => [tier, requestModel]),
      ) as ImageModelDefinition['requestModels'],
    }
    consume(built)
    catalog.push(built)
  }

  // ② 补齐注册表未收录、但分组确实提供的生图模型
  const extras = new Set<string>()
  for (const modelId of availableIds) {
    if (consumed.has(modelId)) continue
    if (!isImageModelId(modelId)) continue

    const baseId = getBaseModelId(modelId)
    // 分辨率变体已在①里并进基础模型；这里只收基础模型本身
    if (baseId !== modelId) {
      consumed.add(modelId)
      continue
    }
    extras.add(modelId)
  }

  for (const modelId of Array.from(extras).sort()) {
    catalog.push(createFallbackImageModel(modelId))
  }

  return catalog
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
