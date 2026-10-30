import type { ImageResolutionTier } from './modelRegistry'

export interface GenerationEstimate {
  creditsPerImage: number
  duration: string
}

/**
 * 内置兜底单价（USD/张）。
 *
 * 仅在拿不到后端真实单价时使用（接口失败 / 离线）。正常情况下
 * 应传入 unitPrice，由后端按分组单价与倍率计算，保证显示 = 实扣。
 */
const FALLBACK_UNIT_PRICE: Record<string, number> = {
  '1K': 0.0268,
  '2K': 0.0402,
  '4K': 0.0536,
}

export function getGenerationEstimate(
  resolution: ImageResolutionTier,
  unitPrice?: number,
): GenerationEstimate {
  const is4K = resolution === '4K'
  const fallback = FALLBACK_UNIT_PRICE[resolution] ?? FALLBACK_UNIT_PRICE['2K']
  return {
    creditsPerImage:
      typeof unitPrice === 'number' && Number.isFinite(unitPrice) ? unitPrice : fallback,
    duration: is4K ? '60–120 秒' : '30–50 秒',
  }
}

export function getEstimatedCredits(
  resolution: ImageResolutionTier,
  imageCount: number,
  unitPrice?: number,
) {
  return getGenerationEstimate(resolution, unitPrice).creditsPerImage * imageCount
}
