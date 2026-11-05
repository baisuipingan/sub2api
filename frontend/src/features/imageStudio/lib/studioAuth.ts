import { storeState as state } from '../stores/store'

/**
 * 生图工作台的鉴权引导。
 *
 * 背景：原实现要求用户手动填写 API Key。集成到 sub2api 后改为由后端
 * 按「生图工作台分组」为每个用户创建一把专用密钥（名称：生图工作台专用），
 * 前端通过面板接口拿回来注入 store，用户全程无感。
 *
 * 计费不受影响：请求仍打到 /v1/images/*，走完整网关链路，
 * usage_logs 归属这把 key，扣费落到用户余额。
 */
export interface StudioKeyResponse {
  key: string
  group_id: number | null
  group_name?: string
}

let pending: Promise<string | null> | null = null

/** 向后端取（或按需创建）当前用户的生图工作台密钥 */
async function fetchStudioKey(): Promise<StudioKeyResponse | null> {
  // 面板接口走 JWT，不是 cookie：必须带上 localStorage 里的 access token，
  // 否则会被鉴权中间件拒绝（401）。
  const token = localStorage.getItem('auth_token')
  if (!token) return null

  const response = await fetch('/api/v1/image-studio/key', {
    headers: {
      Accept: 'application/json',
      Authorization: `Bearer ${token}`,
    },
  })

  if (!response.ok) return null

  const payload = (await response.json()) as {
    code?: number
    data?: StudioKeyResponse
  }
  if (payload.code !== 0 || !payload.data?.key) return null
  return payload.data
}

/**
 * 确保 store 里已注入可用密钥。
 *
 * 幂等：重复调用只发一次请求。失败时静默降级 —— 页面仍可打开，
 * 由生成按钮提示用户（不弹阻塞式对话框）。
 */
export async function ensureStudioApiKey(): Promise<string | null> {
  if (pending) return pending

  pending = (async () => {
    try {
      const data = await fetchStudioKey()
      if (!data) return null

      // 分两步设置：setMiloRouterConfig 在检测到 apiKey 变化时会把
      // connectionStatus 强制重置为 idle（用于清理旧连接），
      // 同一批 patch 里带 connected 会被覆盖掉。
      state.setMiloRouterConfig({ apiKey: data.key })
      state.setMiloRouterConfig({ connectionStatus: 'connected' })

      // 拿到密钥后拉取可用模型列表。必须 await：模型目录就绪前
      // 生成按钮不可用，用户会以为功能坏了。
      await state.refreshImageModelCatalog()

      return data.key
    } catch {
      return null
    } finally {
      // 允许下次进入页面重试
      pending = null
    }
  })()

  return pending
}
