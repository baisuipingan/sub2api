import { beforeEach, describe, expect, it, vi } from 'vitest'

const { state } = vi.hoisted(() => ({
  state: {
    settings: { miloRouter: { apiKey: '', connectionStatus: 'idle' } },
    setMiloRouterConfig: vi.fn(),
    refreshImageModelCatalog: vi.fn(),
  },
}))
vi.mock('../../stores/store', () => ({ storeState: state }))

import { ensureStudioApiKey } from '../studioAuth'
import { isCurrentStudioApiKey } from '../studioKeySession'

function keyResponse(key: string): Response {
  return { ok: true, json: async () => ({ code: 0, data: { key, group_id: 1 } }) } as Response
}

beforeEach(() => {
  localStorage.clear()
  state.settings.miloRouter.apiKey = ''
  state.settings.miloRouter.connectionStatus = 'idle'
  state.setMiloRouterConfig.mockReset().mockImplementation((patch: { apiKey?: string; connectionStatus?: string }) => {
    Object.assign(state.settings.miloRouter, patch)
  })
  state.refreshImageModelCatalog.mockReset().mockResolvedValue(true)
  vi.unstubAllGlobals()
})

describe('studio key session binding', () => {
  it('does not inject A key into B while A response is delayed', async () => {
    const responses = new Map<string, (response: Response) => void>()
    const fetchMock = vi.fn((_url: string, init: RequestInit) =>
      new Promise<Response>((resolve) => {
        responses.set(String((init.headers as Record<string, string>).Authorization), resolve)
      }))
    vi.stubGlobal('fetch', fetchMock)

    localStorage.setItem('auth_token', 'token-a')
    localStorage.setItem('auth_user', JSON.stringify({ id: 101 }))
    const requestA = ensureStudioApiKey()
    localStorage.setItem('auth_token', 'token-b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 202 }))
    const requestB = ensureStudioApiKey()

    responses.get('Bearer token-a')!(keyResponse('key-a'))
    expect(await requestA).toBeNull()
    expect(state.setMiloRouterConfig).not.toHaveBeenCalled()

    responses.get('Bearer token-b')!(keyResponse('key-b'))
    expect(await requestB).toBe('key-b')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(state.settings.miloRouter.apiKey).toBe('key-b')
    expect(isCurrentStudioApiKey('key-b')).toBe(true)
    expect(isCurrentStudioApiKey('key-a')).toBe(false)
  })

  it('reuses one in-flight request for the same account', async () => {
    let finish!: (response: Response) => void
    const fetchMock = vi.fn(() => new Promise<Response>((resolve) => { finish = resolve }))
    vi.stubGlobal('fetch', fetchMock)
    localStorage.setItem('auth_token', 'same-token')
    localStorage.setItem('auth_user', JSON.stringify({ id: 303 }))

    const first = ensureStudioApiKey()
    const second = ensureStudioApiKey()
    finish(keyResponse('same-key'))
    expect(await first).toBe('same-key')
    expect(await second).toBe('same-key')
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
