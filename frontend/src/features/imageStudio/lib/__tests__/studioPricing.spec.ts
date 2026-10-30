import { describe, expect, it, vi } from 'vitest'
import {
  collectBillingModels,
  loadImagePricing,
  lookupUnitPrice,
  normalizeBillingTier,
  resolveUnitPrice,
  type ImagePricingTable,
} from '../studioPricing'
import type { ImageModelDefinition } from '../modelRegistry'

/**
 * 这些用例锁住「页面显示的单价必须等于后端实价」的关键环节。
 * 背景：gpt-image-2 在 2K/4K 会切换到 gpt-image-2-4K 计费，两者单价不同，
 * 取错模型会让预估金额静默偏高/偏低。
 */

function makeModel(overrides: Partial<ImageModelDefinition> = {}): ImageModelDefinition {
  return {
    id: 'gpt-image-2',
    name: 'GPT Image 2',
    api: 'openai',
    requestModels: { auto: 'gpt-image-2', '1K': 'gpt-image-2', '2K': 'gpt-image-2-4K', '4K': 'gpt-image-2-4K' },
    capabilities: {
      resolutions: ['auto', '1K', '2K', '4K'],
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
    defaults: { resolution: 'auto', aspectRatio: '1:1', quality: 'auto', outputFormat: 'png', imageCount: 1 },
    ...overrides,
  }
}

const TABLE: ImagePricingTable = {
  'gpt-image-2': { '1K': 0.0268, '2K': 0.0402, '4K': 0.0536 },
  'gpt-image-2-4K': { '1K': 0.5, '2K': 0.6, '4K': 0.7 },
}

describe('normalizeBillingTier', () => {
  it('把 auto 归一成后端默认的 2K', () => {
    expect(normalizeBillingTier('auto')).toBe('2K')
  })

  it('保留明确的档位', () => {
    expect(normalizeBillingTier('1K')).toBe('1K')
    expect(normalizeBillingTier('4K')).toBe('4K')
  })
})

describe('lookupUnitPrice', () => {
  it('取指定模型与档位的价格', () => {
    expect(lookupUnitPrice(TABLE, 'gpt-image-2', '1K')).toBe(0.0268)
  })

  it('表格缺失或模型未知时返回 undefined，交由调用方回退', () => {
    expect(lookupUnitPrice(undefined, 'gpt-image-2', '1K')).toBeUndefined()
    expect(lookupUnitPrice(TABLE, 'unknown', '1K')).toBeUndefined()
    expect(lookupUnitPrice({}, 'gpt-image-2', '1K')).toBeUndefined()
  })

  it('非法数值不算有效价格', () => {
    expect(lookupUnitPrice({ m: { '1K': Number.NaN } }, 'm', '1K')).toBeUndefined()
  })
})

describe('resolveUnitPrice', () => {
  it('1K 用基础模型的价格', () => {
    expect(resolveUnitPrice(TABLE, makeModel(), '1024x1024', '1K')).toBe(0.0268)
  })

  it('2K/4K 必须切到 -4K 计费模型的价格', () => {
    // 若误用 gpt-image-2 的价（0.0536）会少报一半以上
    expect(resolveUnitPrice(TABLE, makeModel(), 'auto', '2K')).toBe(0.6)
    expect(resolveUnitPrice(TABLE, makeModel(), 'auto', '4K')).toBe(0.7)
  })

  it('auto 走基础模型 + 2K 档位（已用真实扣费核对）', () => {
    // 实测：gpt-image-2 不传 size → usage_logs.image_size=2K、actual_cost=0.0402，
    // 即请求打基础模型、按 2K 档位计价，而不是切到 -4K 变体。
    expect(resolveUnitPrice(TABLE, makeModel(), 'auto', 'auto')).toBe(0.0402)
  })

  it('模型为空时返回 undefined', () => {
    expect(resolveUnitPrice(TABLE, null, 'auto', '1K')).toBeUndefined()
  })
})

describe('collectBillingModels', () => {
  it('覆盖所有档位映射与模型 id，去重', () => {
    const models = collectBillingModels([makeModel()])
    expect(models.sort()).toEqual(['gpt-image-2', 'gpt-image-2-4K'])
  })
})

describe('loadImagePricing', () => {
  it('按 20 个一批分批请求，避免超过后端上限被拒', async () => {
    const models = Array.from({ length: 45 }, (_, i) => `m-${i}`)
    const fetchMock = vi.fn(async (_url: string, init: RequestInit) => {
      const body = JSON.parse(String(init.body)) as { models: string[] }
      const pricing: ImagePricingTable = {}
      for (const m of body.models) pricing[m] = { '1K': 1 }
      return { ok: true, json: async () => ({ code: 0, data: { pricing } }) } as unknown as Response
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('localStorage', { getItem: () => 'token' })

    const table = await loadImagePricing(models)

    expect(fetchMock).toHaveBeenCalledTimes(3) // 20 + 20 + 5
    expect(Object.keys(table)).toHaveLength(45)

    vi.unstubAllGlobals()
  })

  it('单批失败不影响其它批次', async () => {
    let call = 0
    vi.stubGlobal('fetch', async (_url: string, init: RequestInit) => {
      call += 1
      // 第一批模拟失败，后续批次按请求内容回包
      if (call === 1) return { ok: false } as unknown as Response
      const body = JSON.parse(String(init.body)) as { models: string[] }
      const pricing: ImagePricingTable = {}
      for (const m of body.models) pricing[m] = { '1K': 1 }
      return { ok: true, json: async () => ({ code: 0, data: { pricing } }) } as unknown as Response
    })
    vi.stubGlobal('localStorage', { getItem: () => 'token' })

    // 需要超过一批（20 个）才会触发第二次请求
    const models = Array.from({ length: 25 }, (_, i) => `m-${i}`)
    const table = await loadImagePricing(models)

    expect(Object.keys(table)).toEqual(['m-20', 'm-21', 'm-22', 'm-23', 'm-24'])
    vi.unstubAllGlobals()
  })

  it('拿不到 token 时不发请求', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('localStorage', { getItem: () => null })

    expect(await loadImagePricing(['a'])).toEqual({})
    expect(fetchMock).not.toHaveBeenCalled()
    vi.unstubAllGlobals()
  })
})
