import type { AppSettings, MiloRouterConfig } from '../types'

/**
 * API 基址。
 *
 * 原实现是 '/api/milorouter'（独立部署时由 nginx 反代到 milorouter.com）。
 * 集成进 sub2api 后改为同源直连：请求直接打到本站的 /v1/*，
 * 由现有的网关中间件完成鉴权与计费。
 */
export const MILO_ROUTER_BASE_URL = ''

const SESSION_API_KEY_STORAGE_KEY = 'milo-router-image-api-key.session'
const LOCAL_API_KEY_STORAGE_KEY = 'milo-router-image-api-key.local'

function getStorage(type: 'session' | 'local') {
  if (typeof window === 'undefined') return null
  try {
    return type === 'session' ? window.sessionStorage : window.localStorage
  } catch {
    return null
  }
}

export function createMiloRouterConfig(patch: Partial<MiloRouterConfig> = {}): MiloRouterConfig {
  return {
    apiKey: '',
    rememberApiKey: false,
    connectionStatus: 'idle',
    ...patch,
    baseUrl: MILO_ROUTER_BASE_URL,
  }
}

/**
 * 读取持久化的连接配置。
 *
 * 集成后 API Key 由后端按用户下发（生图工作台专用密钥），
 * 不再从 localStorage 恢复用户手填的 key —— 旧数据会覆盖系统凭据。
 */
export function readStoredMiloRouterConfig() {
  return createMiloRouterConfig({})
}

export function saveMiloRouterApiKey(apiKey: string, rememberApiKey: boolean) {
  const key = apiKey.trim()
  const session = getStorage('session')
  const local = getStorage('local')

  if (!key) {
    session?.removeItem(SESSION_API_KEY_STORAGE_KEY)
    local?.removeItem(LOCAL_API_KEY_STORAGE_KEY)
    return
  }

  if (rememberApiKey) {
    local?.setItem(LOCAL_API_KEY_STORAGE_KEY, key)
    session?.removeItem(SESSION_API_KEY_STORAGE_KEY)
    return
  }

  session?.setItem(SESSION_API_KEY_STORAGE_KEY, key)
  local?.removeItem(LOCAL_API_KEY_STORAGE_KEY)
}

export function stripApiKeysFromSettings(settings: AppSettings): AppSettings {
  return {
    ...settings,
    miloRouter: createMiloRouterConfig({
      ...settings.miloRouter,
      apiKey: '',
    }),
  }
}
