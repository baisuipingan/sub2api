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
        const nonGeminiModels = models.filter((model) => model.api !== 'gemini')
        // /v1beta/models 只是用来确认 Gemini 模型是否真的可用（后端没配 Gemini 账号时返回 503）。
        // 这里任何失败都必须静默降级为「只保留非 Gemini 模型」：
        // 之前只 catch 了 fetch，解析 503 的 {"error":...} 会抛错并让整个列表加载失败，
        // 表现就是「模型列表加载失败」+ 下拉框空 —— 非 Gemini 模型其实完全可用。
        try {
          response = await fetch(`${MILO_ROUTER_BASE_URL}/v1beta/models`, {
            headers: { Authorization: `Bearer ${key}` },
            cache: 'no-store',
            signal: controller.signal,
          })
          models = response.ok
            ? filterGeminiImageModelsByNativeModelList(models, await response.json() as unknown)
            : nonGeminiModels
        } catch {
          models = nonGeminiModels
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
