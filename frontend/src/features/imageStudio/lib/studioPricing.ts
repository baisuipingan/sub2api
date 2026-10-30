import { getModelResolution, type ImageModelDefinition, type ImageResolutionTier } from './modelRegistry'

/**
 * 生图工作台的单价缓存。
 *
 * 结构：map[计费模型][尺寸档位] = 单张实际成本（已含分组倍率与用户专属倍率）。
 * 由后端 /api/v1/image-studio/pricing 计算，前端只负责乘以张数。
 *
 * 为什么要从后端拿：原来前端写死 0.0268 / 0.0402 / 0.0536，
 * 那只是某一套配置下的巧合值。管理员改倍率或配自定义单价后，
 * 页面会继续显示旧数字而真实扣费已变 —— 用户会以为被多收钱。
 */
export type ImagePricingTable = Record<string, Record<string, number>>

let cache: ImagePricingTable = {}

export function getCachedPricing(): ImagePricingTable {
  return cache
}

export function resetPricingCache() {
  cache = {}
}

/** 把 'auto' 归一化成实际计费档位（后端默认按 2K 计） */
export function normalizeBillingTier(tier: ImageResolutionTier | string): string {
  if (tier === '1K' || tier === '2K' || tier === '4K') return tier
  return '2K'
}

/**
 * 拉取指定计费模型的单价表。
 *
 * 失败时静默返回空表，由调用方回退到内置估算值 ——
 * 预估金额不该因为一个可选接口失败而让整个页面不可用。
 */
export async function loadImagePricing(models: string[]): Promise<ImagePricingTable> {
  const unique = Array.from(new Set(models.filter(Boolean)))
  if (!unique.length) return {}

  const token = localStorage.getItem('auth_token')
  if (!token) return {}

  try {
    const response = await fetch('/api/v1/image-studio/pricing', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify({ models: unique }),
    })
    if (!response.ok) return {}

    const payload = (await response.json()) as {
      code?: number
      data?: { pricing?: ImagePricingTable }
    }
    const pricing = payload.data?.pricing
    if (payload.code !== 0 || !pricing) return {}

    cache = { ...cache, ...pricing }
    return cache
  } catch {
    return {}
  }
}

/** 查某个计费模型在某个档位的单张实价；没有则返回 undefined */
export function lookupUnitPrice(
  billingModel: string | undefined,
  tier: ImageResolutionTier | string,
): number | undefined {
  if (!billingModel) return undefined
  const byModel = cache[billingModel]
  if (!byModel) return undefined
  const price = byModel[normalizeBillingTier(tier)]
  return typeof price === 'number' && Number.isFinite(price) ? price : undefined
}

/**
 * 解析某模型在当前尺寸/清晰度下的单张实价。
 *
 * 注意：请求模型可能随档位变化（如 gpt-image-2 在 2K/4K 走 gpt-image-2-4K），
 * 而不同模型的单价是分别配置的，所以必须先选对计费模型再取价。
 */
export function resolveUnitPrice(
  model: ImageModelDefinition | null | undefined,
  size: string,
  resolution: ImageResolutionTier,
): number | undefined {
  if (!model) return undefined
  const tier = getModelResolution(model, size, resolution)
  const billingModel = model.requestModels[tier] ?? model.requestModels.auto ?? model.id
  return lookupUnitPrice(billingModel, tier)
}

/** 该模型可能用到的全部计费模型名（用于一次性预取价格） */
export function collectBillingModels(models: ImageModelDefinition[]): string[] {
  const names = new Set<string>()
  for (const model of models) {
    for (const name of Object.values(model.requestModels)) {
      if (name) names.add(name)
    }
    names.add(model.id)
  }
  return Array.from(names)
}
