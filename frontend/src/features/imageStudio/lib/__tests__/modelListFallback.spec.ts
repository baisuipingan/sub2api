import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchMiloRouterImageModels } from '../miloRouterApi'
import { ApiInsufficientBalanceError } from '../apiErrors'

/**
 * 回归测试：模型列表里的「附加探测」失败，绝不能拖垮整个列表。
 *
 * 线上真实故障：用户余额为 0，/v1/models 返回
 *   403 {"code":"INSUFFICIENT_BALANCE",...}
 * 旧实现把它吞成「模型列表加载失败」，用户重试无效也不知道要去充值。
 *
 * 另一类相关故障：/v1beta/models 探测失败（没配 Gemini 账号时 503），
 * 旧实现解析错误体抛错并冒泡，同样导致整个列表空白。
 *
 * 设计原则：
 *   1. 探测失败 → 保留模型原样，不猜哪个模型不可用（用户以后配好 Gemini 也不用改代码）
 *   2. /v1/models 本身失败 → 必须把真实原因（余额/鉴权）传出去，不能一律说"加载失败"
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
  it('Gemini 探测返回 503 时保留全部模型，不因探测失败影响列表', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        '/v1beta/models': { status: 503, body: { error: { code: 503, message: 'No available Gemini accounts' } } },
        '/v1/models': { status: 200, body: modelsPayload }
      })
    )

    const ids = (await fetchMiloRouterImageModels('sk-test')).map((model) => model.id)

    expect(ids).toContain('gpt-image-2')
    // 关键：不在前端猜 gemini 不可用。用户以后配好 Gemini 账号，这里无需改代码即可生效。
    expect(ids).toContain('gemini-2.5-flash-image')
  })

  it('Gemini 探测返回 200 但结构异常时也不应抛错', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        '/v1beta/models': { status: 200, body: { unexpected: true } },
        '/v1/models': { status: 200, body: modelsPayload }
      })
    )

    await expect(fetchMiloRouterImageModels('sk-test')).resolves.toBeTruthy()
  })

  it('余额不足时必须抛出可识别的错误，而不是笼统的"加载失败"', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        '/v1/models': {
          status: 403,
          body: { code: 'INSUFFICIENT_BALANCE', message: 'Insufficient account balance' }
        }
      })
    )

    // 断言「抛的是余额不足类型」而不是「消息里含原始 code」——
    // 用户看到的是本地化文案，不该暴露内部 code。
    await expect(fetchMiloRouterImageModels('sk-test')).rejects.toBeInstanceOf(
      ApiInsufficientBalanceError
    )
  })

  it('/v1/models 5xx 时仍应抛出（这是真的取不到列表）', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetch({
        '/v1/models': { status: 503, body: { error: { message: 'upstream down' } } }
      })
    )

    await expect(fetchMiloRouterImageModels('sk-test')).rejects.toBeTruthy()
  })
})
