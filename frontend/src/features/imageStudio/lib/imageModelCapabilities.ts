import type { ImageModelDefinition } from './modelRegistry'
import { getImageModel } from './modelRegistry'

export interface ImageModelCapabilities {
  referenceImages: boolean
  mask: boolean
  quality: boolean
  outputFormat: boolean
  outputCompression: boolean
  moderation: boolean
  imageCount: boolean
  transparentOutput: boolean
}

export function getImageModelCapabilities(modelId: string, catalog?: readonly ImageModelDefinition[]): ImageModelCapabilities {
  const capabilities = getImageModel(modelId, catalog)?.capabilities
  return {
    referenceImages: Boolean(capabilities?.referenceImages),
    mask: Boolean(capabilities?.mask),
    quality: Boolean(capabilities?.qualities.length),
    outputFormat: Boolean(capabilities?.outputFormats.length),
    outputCompression: Boolean(capabilities?.outputCompression),
    moderation: Boolean(capabilities?.moderation.length),
    imageCount: Boolean(capabilities && capabilities.imageCount.max > 1),
    transparentOutput: Boolean(capabilities?.transparentOutput),
  }
}
