import type { ImageResolutionTier } from './modelRegistry'

export interface GenerationEstimate {
  creditsPerImage: number
  duration: string
}

export function getGenerationEstimate(resolution: ImageResolutionTier): GenerationEstimate {
  const is1K = resolution === '1K'
  const is4K = resolution === '4K'
  return {
    creditsPerImage: is1K ? 0.0268 : is4K ? 0.0536 : 0.0402,
    duration: is4K ? '60–120 秒' : '30–50 秒',
  }
}

export function getEstimatedCredits(resolution: ImageResolutionTier, imageCount: number) {
  return getGenerationEstimate(resolution).creditsPerImage * imageCount
}
