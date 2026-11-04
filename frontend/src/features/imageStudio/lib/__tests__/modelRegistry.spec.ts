import { describe, expect, it } from 'vitest'
import { createImageModelCatalogFromModelList } from '../modelRegistry'

/**
 * 这些用例锁住「模型列表 = 分组真实提供的生图模型」这一行为。
 *
 * 背景：原实现只遍历写死的注册表，分组新增的生图模型一律看不到；
 * 而分组里同时挂着文本模型（grok-4.5）和视频模型（grok-imagine-video），
 * 若一并列出，用户选中后请求必然失败。
 */
function payloadOf(ids: string[]) {
  return { data: ids.map((id) => ({ id, object: 'model' })) }
}

const GROUP_MODELS = [
  'gpt-image-2',
  'gpt-image-2-4K',
  'gpt-image-2.5',
  'gpt-image-2.5-flare',
  'gpt-image-2.5-sunburst',
  'grok-4.5',
  'grok-4.6',
  'grok-imagine-image',
  'grok-imagine-image-2.0',
  'grok-imagine-video',
  'grok-imagine-video-1.5',
]

describe('createImageModelCatalogFromModelList', () => {
  const catalog = createImageModelCatalogFromModelList(payloadOf(GROUP_MODELS))
  const ids = catalog.map((m) => m.id)

  it('列出分组里的全部生图模型', () => {
    expect(ids).toEqual([
      'gpt-image-2',
      'gpt-image-2.5-flare',
      'gpt-image-2.5-sunburst',
      'gpt-image-2.5',
      'grok-imagine-image',
      'grok-imagine-image-2.0',
    ])
  })

  it('排除视频与文本模型', () => {
    expect(ids).not.toContain('grok-imagine-video')
    expect(ids).not.toContain('grok-imagine-video-1.5')
    expect(ids).not.toContain('grok-4.5')
    expect(ids).not.toContain('grok-4.6')
  })

  it('不把 -4K 变体当成独立模型，而是并入基础模型', () => {
    expect(ids).not.toContain('gpt-image-2-4K')

    const base = catalog.find((m) => m.id === 'gpt-image-2')!
    // fork: image-size-routing
    // 所有档位都发基础模型名，档位由 size 参数表达、由路由层解析。
    // 曾经 2K 也指向 gpt-image-2-4K，导致选 2K 实际按 4K 处理。
    expect(base.requestModels['1K']).toBe('gpt-image-2')
    expect(base.requestModels['2K']).toBe('gpt-image-2')
    expect(base.requestModels['4K']).toBe('gpt-image-2')
  })

  it('2K 与 4K 不共用同一个上游模型名（防止退回按档位换模型的旧行为）', () => {
    const base = catalog.find((m) => m.id === 'gpt-image-2')!
    // 关键不变量：2K 请求必须能区分出 2K，不能被并到 4K
    expect(base.requestModels['2K']).not.toBe('gpt-image-2-4K')
    // 所有档位同名的唯一合法取值就是基础名
    const tiers = ['1K', '2K', '4K'] as const
    for (const tier of tiers) {
      expect(base.requestModels[tier]).toBe('gpt-image-2')
    }
  })

  it('注册表已知模型保留完整能力', () => {
    const base = catalog.find((m) => m.id === 'gpt-image-2')!
    expect(base.capabilities.resolutions).toContain('4K')
    expect(base.capabilities.mask).toBe(true)
  })

  it('未收录模型给保守能力，避免上游拒绝', () => {
    const fallback = catalog.find((m) => m.id === 'grok-imagine-image')!
    expect(fallback.capabilities.resolutions).toEqual(['auto'])
    expect(fallback.capabilities.imageCount.max).toBe(1)
    expect(fallback.capabilities.referenceImages).toBe(0)
    expect(fallback.capabilities.mask).toBe(false)
    // 名称要做成可读形式，不能直接回显 id
    expect(fallback.name).toBe('Grok Imagine Image')
  })

  it('gpt-image-2.5 这类未收录模型仍按 openai 兼容接口调用', () => {
    const model = catalog.find((m) => m.id === 'gpt-image-2.5')!
    expect(model.api).toBe('openai')
    expect(model.name).toBe('GPT Image 2.5')
  })

  it('分组没有生图模型时返回空列表', () => {
    expect(createImageModelCatalogFromModelList(payloadOf(['grok-4.5']))).toEqual([])
  })

  it('兼容 { models: [...] } 与 { name } 两种返回格式', () => {
    const alt = { models: [{ name: 'models/gpt-image-2' }] }
    const result = createImageModelCatalogFromModelList(alt)
    expect(result.map((m) => m.id)).toContain('gpt-image-2')
  })
})
