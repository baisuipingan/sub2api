import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

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
  return vi.fn(async (input: RequestInfo | URL, _init?: RequestInit) => {
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

beforeEach(() => localStorage.removeItem('auth_token'))

afterEach(() => {
  localStorage.removeItem('auth_token')
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


describe('fresh studio catalog', () => {
  it('使用工作台鉴权目录的真实名称和协议，不拼入预定义模型', async () => {
    localStorage.setItem('auth_token', 'session-test')
    const mocked = mockFetch({
      '/api/v1/image-studio/models': { status: 200, body: { code: 0, data: { models: [
        { id: 'gpt-image-2', api: 'openai' },
        { id: 'gpt-image-2.5', api: 'openai' },
        { id: 'public-banana', api: 'gemini' },
      ] } } },
    })
    vi.stubGlobal('fetch', mocked)
    const models = await fetchMiloRouterImageModels('sk-test')
    expect(models.map(({ id, name, api }) => ({ id, name, api }))).toEqual([
      { id: 'gpt-image-2', name: 'gpt-image-2', api: 'openai' },
      { id: 'gpt-image-2.5', name: 'gpt-image-2.5', api: 'openai' },
      { id: 'public-banana', name: 'public-banana', api: 'gemini' },
    ])
    expect(mocked).toHaveBeenCalledTimes(1)
    expect(mocked.mock.calls[0][1]).toMatchObject({ cache: 'no-store', headers: { Authorization: 'Bearer session-test' } })
  })

  it('空的工作台目录不退回通用列表补齐旧路由', async () => {
    localStorage.setItem('auth_token', 'session-test')
    const mocked = mockFetch({
      '/api/v1/image-studio/models': { status: 200, body: { code: 0, data: { models: [] } } },
      '/v1/models': { status: 200, body: modelsPayload },
    })
    vi.stubGlobal('fetch', mocked)
    await expect(fetchMiloRouterImageModels('sk-test')).rejects.toBeTruthy()
    expect(mocked).toHaveBeenCalledTimes(1)
  })

  it('工作台目录失败时保留真实错误，不回退到可能陈旧的通用列表', async () => {
    localStorage.setItem('auth_token', 'session-test')
    const mocked = mockFetch({ '/api/v1/image-studio/models': { status: 403, body: { message: 'Image Studio disabled' } } })
    vi.stubGlobal('fetch', mocked)
    await expect(fetchMiloRouterImageModels('sk-test')).rejects.toBeTruthy()
    expect(mocked).toHaveBeenCalledTimes(1)
  })

  it('旧服务没有专用目录时可兼容原模型接口', async () => {
    localStorage.setItem('auth_token', 'session-test')
    vi.stubGlobal('fetch', mockFetch({ '/v1/models': { status: 200, body: { data: [{ id: 'gpt-image-2.5' }] } } }))
    expect((await fetchMiloRouterImageModels('sk-test')).map(({ id }) => id)).toEqual(['gpt-image-2.5'])
  })

  it('原生 Gemini 探测不包含的模型继续走兼容 Images，不删除或替换型号', async () => {
    vi.stubGlobal('fetch', mockFetch({
      '/v1/models': { status: 200, body: { data: [
        { id: 'gemini-3.1-flash-image' }, { id: 'gemini-3-pro-image-preview' },
      ] } },
      '/v1beta/models': { status: 200, body: { models: [{ name: 'models/gemini-3-pro-image-preview' }] } },
    }))
    expect((await fetchMiloRouterImageModels('sk-test')).map(({ id, api }) => ({ id, api }))).toEqual([
      { id: 'gemini-3.1-flash-image', api: 'openai' },
      { id: 'gemini-3-pro-image-preview', api: 'gemini' },
    ])
  })

  it('原生探测失败时使用兼容 Images，不猜测存在原生 Gemini 链路', async () => {
    vi.stubGlobal('fetch', mockFetch({
      '/v1/models': { status: 200, body: { data: [{ id: 'gemini-3.1-flash-image' }] } },
      '/v1beta/models': { status: 503, body: {} },
    }))
    expect((await fetchMiloRouterImageModels('sk-test'))[0].api).toBe('openai')
  })
})
