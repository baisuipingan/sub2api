import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put } }))

import { getDesktopAgentConfig, updateDesktopAgentConfig, type DesktopAgentConfig } from '@/api/admin/desktopAgent'

describe('independent desktop Agent admin API', () => {
  beforeEach(() => { get.mockReset(); put.mockReset() })

  it('reads only its protected config without inventing a model', async () => {
    const initial: DesktopAgentConfig = { enabled: false, group_id: 0, base_url: '' }
    get.mockResolvedValueOnce({ data: initial })
    await expect(getDesktopAgentConfig()).resolves.toBe(initial)
    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/admin/desktop/config')
    expect(put).not.toHaveBeenCalled()
  })

  it('sends the captured complete payload only to the dedicated endpoint', async () => {
    const payload = { enabled: false, group_id: 7, base_url: 'https://account.example.invalid/v1' }
    const saved = { enabled: false, group_id: 0, base_url: '' }
    put.mockResolvedValueOnce({ data: saved })
    await expect(updateDesktopAgentConfig(payload)).resolves.toBe(saved)
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/admin/desktop/config', payload)
    expect(get).not.toHaveBeenCalled()
  })

  it('supports explicit disable without serializing an invalid draft', async () => {
    put.mockResolvedValueOnce({ data: { enabled: false, group_id: 0, base_url: '' } })
    await updateDesktopAgentConfig({ enabled: false })
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/admin/desktop/config', { enabled: false })
  })

  it('leaves step-up and retry decisions to the existing controller', async () => {
    const error = { status: 403, code: 'STEP_UP_REQUIRED' }
    put.mockRejectedValueOnce(error)
    await expect(updateDesktopAgentConfig({ enabled: false })).rejects.toBe(error)
    expect(put).toHaveBeenCalledTimes(1)
  })
})
