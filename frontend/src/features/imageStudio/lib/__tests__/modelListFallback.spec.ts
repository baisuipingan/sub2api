import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchMiloRouterImageModels } from '../miloRouterApi'

/**
 * 回归测试：/v1beta/models 不可用时，模型列表不能整体失败。
 *
 * 线上真实场景：后端没配 Gemini 账号时 /v1beta/models 返回 503 + {"error":...}。
 * 旧实现在这条路径上只 catch 了 fetch，解析响应体抛错会冒泡，
 * 导致「模型列表加载失败」、下拉框空白——而 gpt-image 等模型其实完全可用。
 */

type Route = { status: number; body: unknown }

function mockFetch(routes: Record<string, Route>) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    const key = Object.keys(routes).find((fragment) => url.includes(fragment))
    if (!key) return new Response('not found', { status: 404 })
    const route = routes[key]
    return new Response(JSON.stringify(route.body), {
      status: route.status,
      headers: { 'Content-Type': 'application/json' }
    })
  })
}

const modelsPayload = {
  data: [
    { id: 'gpt-image-2', type: 'model' },
    { id: 'gpt-image-2.5-flare', type: 'model' },
    { id: 'gemini-2.5-flash-image', type: 'model' }
  ]
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchMiloRouterImageModels', () => {
  it('/v1beta/models 返回 503 时降级为非 Gemini 模型，而不是整体失败', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        '/v1beta/models': { status: 503, body: { error: { code: 503, message: 'No available Gemini accounts' } } },
        '/v1/models': { status: 200, body: modelsPayload }
      })
    )

    const models = await fetchMiloRouterImageModels('sk-test')
    const ids = models.map((model) => model.id)

    expect(ids).toContain('gpt-image-2')
    expect(ids).not.toContain('gemini-2.5-flash-image')
  })

  it('/v1beta/models 返回 200 但结构异常时也不应抛错', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        '/v1beta/models': { status: 200, body: { unexpected: true } },
        '/v1/models': { status: 200, body: modelsPayload }
      })
    )

    await expect(fetchMiloRouterImageModels('sk-test')).resolves.toBeTruthy()
  })

  it('/v1/models 本身失败时仍应抛出（这是真的没有模型可用）', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        '/v1/models': { status: 503, body: { error: { message: 'upstream down' } } }
      })
    )

    await expect(fetchMiloRouterImageModels('sk-test')).rejects.toBeTruthy()
  })
})
