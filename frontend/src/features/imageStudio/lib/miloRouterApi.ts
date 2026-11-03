import { libT } from './i18nMessage'
import type { MiloRouterBillingInfo } from '../types'
import { ApiTimeoutError, createApiError } from './apiErrors'
import { MILO_ROUTER_BASE_URL } from './miloRouterConfig'
import { getApiErrorMessage } from './imageApiShared'
import { createImageModelCatalogFromModelList, filterGeminiImageModelsByNativeModelList, parseImageModelCatalog } from './modelRegistry'

function getNumber(value: unknown, keys: string[]): number | undefined {
  if (!value || typeof value !== 'object') return undefined
  const record = value as Record<string, unknown>
  for (const key of keys) {
    const candidate = record[key]
    if (typeof candidate === 'number' && Number.isFinite(candidate)) return candidate
    if (typeof candidate === 'string' && candidate.trim() && Number.isFinite(Number(candidate))) return Number(candidate)
  }
  for (const child of Object.values(record)) {
    const nested = getNumber(child, keys)
    if (nested !== undefined) return nested
  }
  return undefined
}

export async function validateMiloRouterApiKey(apiKey: string, timeoutMs = 15_000): Promise<MiloRouterBillingInfo> {
  const key = apiKey.trim()
  if (!key) throw createApiError(401)

  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(`${MILO_ROUTER_BASE_URL}/v1/usage`, {
      headers: { Authorization: `Bearer ${key}` },
      cache: 'no-store',
      signal: controller.signal,
    })
    if (!response.ok) throw createApiError(response.status, await getApiErrorMessage(response), key)

    const payload = await response.json() as unknown
    if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
      throw new Error(libT('imageStudio.lib.connectionPayloadUnknown'))
    }

    const record = payload as Record<string, unknown>
    const data = record.data && typeof record.data === 'object' && !Array.isArray(record.data)
      ? record.data as Record<string, unknown>
      : record
    return {
      balance: getNumber(data, ['balance', 'remaining', 'available_balance', 'availableBalance', 'remaining_balance', 'remainingBalance', 'total_balance', 'totalBalance', 'credits']),
      effectiveRateMultiplier: getNumber(data, ['effective_rate_multiplier', 'effectiveRateMultiplier']),
      observedAt: typeof data.observed_at === 'string'
        ? data.observed_at
        : typeof data.observedAt === 'string' ? data.observedAt : undefined,
    }
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw new ApiTimeoutError('连接 MiloRouter 超时，请稍后重试')
    }
    throw error
  } finally {
    clearTimeout(timeoutId)
  }
}

export async function fetchMiloRouterImageModels(apiKey: string, timeoutMs = 15_000) {
  const key = apiKey.trim()
  if (!key) throw createApiError(401)

  const controller = new AbortController()
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs)
  try {
    let response = await fetch(`${MILO_ROUTER_BASE_URL}/v1/image-models`, {
      headers: { Authorization: `Bearer ${key}` },
      cache: 'no-store',
      signal: controller.signal,
    })
    if (response.status === 404) {
      response = await fetch(`${MILO_ROUTER_BASE_URL}/v1/models`, {
        headers: { Authorization: `Bearer ${key}` },
        cache: 'no-store',
        signal: controller.signal,
      })
      if (!response.ok) throw createApiError(response.status, await getApiErrorMessage(response), key)
      let models = createImageModelCatalogFromModelList(await response.json() as unknown)
      if (models.some((model) => model.api === 'gemini')) {
        // /v1beta/models 只用于「确认 Gemini 模型当前是否真的可用」。
        //
        // 关键原则：**这只是可选优化，任何一个模型探测失败都不该影响其它模型**。
        //   - 探测请求失败（网络/超时）→ 保留全部模型，交给真正调用时再报错
        //   - 探测返回非 2xx（如没配 Gemini 账号的 503）→ 同样保留全部模型
        //   - 只有探测成功时才用它做过滤
        // 之前这里一旦解析非 2xx 响应体就抛错并冒泡，导致整个列表加载失败，
        // 而实际原因可能只是余额不足或某个渠道没配置——与模型本身是否可用无关。
        //
        // 另外：不在前端把 gemini 模型「猜」成不可用。用户以后配好了 Gemini，
        // 这里不需要改代码就能自动出现。
        try {
          const probe = await fetch(`${MILO_ROUTER_BASE_URL}/v1beta/models`, {
            headers: { Authorization: `Bearer ${key}` },
            cache: 'no-store',
            signal: controller.signal,
          })
          if (probe.ok) {
            models = filterGeminiImageModelsByNativeModelList(models, await probe.json() as unknown)
          }
        } catch {
          // 探测失败就保持原样，不影响列表
        }
      }
      if (!models.length) throw new Error(libT('imageStudio.lib.noImageModels'))
      return models
    }
    if (!response.ok) throw createApiError(response.status, await getApiErrorMessage(response), key)
    const models = parseImageModelCatalog(await response.json() as unknown)
    if (!models.length) throw new Error(libT('imageStudio.lib.noImageModels'))
    return models
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw new ApiTimeoutError('模型列表加载超时，请稍后重试')
    }
    throw error
  } finally {
    clearTimeout(timeoutId)
  }
}
