import {
  getModelResolution,
  type ImageModelDefinition,
  type ImageResolutionTier,
} from './modelRegistry'

/**
 * 生图工作台的单价表。
 *
 * 结构：map[计费模型][尺寸档位] = 单张实际成本（已含分组倍率与用户专属倍率）。
 * 由后端 /api/v1/image-studio/pricing 用真实计费链路算出，前端只负责乘以张数。
 *
 * 为什么不放模块级可变缓存：单价是页面状态的一部分，存在 store 里才能
 * 让视图随价格到达自动刷新；模块级变量不参与响应式，容易读到旧值。
 */
export type ImagePricingTable = Record<string, Record<string, number>>

/** 把 'auto' 归一化成实际计费档位（后端对未知尺寸默认按 2K 计） */
export function normalizeBillingTier(tier: ImageResolutionTier | string): string {
  if (tier === '0.5K') return '1K'
  if (tier === '1K' || tier === '2K' || tier === '4K') return tier
  return '2K'
}

/** 查某个计费模型在某个档位的单张实价 */
export function lookupUnitPrice(
  table: ImagePricingTable | undefined,
  billingModel: string | undefined,
  tier: ImageResolutionTier | string,
): number | undefined {
  if (!table || !billingModel) return undefined
  const price = table[billingModel]?.[normalizeBillingTier(tier)]
  return typeof price === 'number' && Number.isFinite(price) ? price : undefined
}

/**
 * 解析某模型在当前尺寸/清晰度下的单张实价。
 *
 * 请求模型可能随档位变化（如 gpt-image-2 在 2K/4K 走 gpt-image-2-4K），
 * 而不同模型的单价分开配置，所以必须先按档位选对计费模型再取价。
 */
export function resolveUnitPrice(
  table: ImagePricingTable | undefined,
  model: ImageModelDefinition | null | undefined,
  size: string,
  resolution: ImageResolutionTier,
): number | undefined {
  if (!model) return undefined
  const selectedTier = getModelResolution(model, size, resolution)
  const tier = model.provider === 'xai' && selectedTier === 'auto' ? '1K' : selectedTier
  const billingModel = model.requestModels[tier] ?? model.requestModels.auto ?? model.id
  return lookupUnitPrice(table, billingModel, tier)
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

const PRICING_BATCH_SIZE = 20

/**
 * 拉取指定计费模型的单价表。
 *
 * 分批请求：后端对单次模型数有上限（20），分组模型多时一次性发送会被拒，
 * 而这种失败是静默的（回退到内置价），等于价格悄悄变错 —— 必须分批。
 *
 * 某批失败不影响其它批次；整体为空时由调用方决定是否回退内置价。
 */
export async function loadImagePricing(models: string[]): Promise<ImagePricingTable> {
  const unique = Array.from(new Set(models.filter(Boolean)))
  if (!unique.length) return {}

  const token = localStorage.getItem('auth_token')
  if (!token) return {}

  const table: ImagePricingTable = {}

  for (let i = 0; i < unique.length; i += PRICING_BATCH_SIZE) {
    const batch = unique.slice(i, i + PRICING_BATCH_SIZE)
    try {
      const response = await fetch('/api/v1/image-studio/pricing', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Accept: 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: JSON.stringify({ models: batch }),
      })
      if (!response.ok) continue

      const payload = (await response.json()) as {
        code?: number
        data?: { pricing?: ImagePricingTable }
      }
      if (payload.code !== 0 || !payload.data?.pricing) continue
      Object.assign(table, payload.data.pricing)
    } catch {
      // 单批失败继续下一批
    }
  }

  return table
}
