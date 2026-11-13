import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put, post } }))

import { disableDesktopAgentConfig, getDesktopAgentConfig, getDesktopAgentModels, getDesktopAgentProfiles, updateDesktopAgentConfig, type DesktopAgentConfig } from '@/api/admin/desktopAgent'

const initial = (): DesktopAgentConfig => ({ enabled: false, group_id: 0, base_url: '', models: [], default_model_id: '' })

describe('independent desktop Agent admin API', () => {
  beforeEach(() => { get.mockReset(); put.mockReset(); post.mockReset() })

  it('reads only its protected config without inventing a model', async () => {
    const value = initial()
    get.mockResolvedValueOnce({ data: value })
    await expect(getDesktopAgentConfig()).resolves.toBe(value)
    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/admin/desktop/config')
    expect(put).not.toHaveBeenCalled()
  })

  it('sends the captured complete payload only to the dedicated endpoint', async () => {
    const payload = { ...initial(), group_id: 7, base_url: 'https://account.example.invalid/v1' }
    const saved = initial()
    put.mockResolvedValueOnce({ data: saved })
    await expect(updateDesktopAgentConfig(payload)).resolves.toBe(saved)
    expect(put).toHaveBeenCalledTimes(1)
    expect(put).toHaveBeenCalledWith('/admin/desktop/config', payload)
    expect(get).not.toHaveBeenCalled()
  })

  it('disables through a separate action without sending draft fields', async () => {
    const saved = initial()
    post.mockResolvedValueOnce({ data: saved })
    await expect(disableDesktopAgentConfig()).resolves.toBe(saved)
    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/admin/desktop/config/disable')
    expect(put).not.toHaveBeenCalled()
    expect(get).not.toHaveBeenCalled()
  })

  it('loads the selected group catalog from the dedicated read-only endpoint', async () => {
    const catalog = { group_id: 7, models: ['configured-model-a', 'configured-model-b'] }
    get.mockResolvedValueOnce({ data: catalog })
    await expect(getDesktopAgentModels(7)).resolves.toBe(catalog)
    expect(get).toHaveBeenCalledTimes(1)
    expect(get).toHaveBeenCalledWith('/admin/desktop/models', { params: { group_id: 7 } })
    expect(post).not.toHaveBeenCalled()
    expect(put).not.toHaveBeenCalled()
  })

  it('keeps empty catalogs authoritative and propagates read failure without fallback', async () => {
    const empty = { group_id: 7, models: [] }
    get.mockResolvedValueOnce({ data: empty })
    await expect(getDesktopAgentModels(7)).resolves.toBe(empty)
    const error = { status: 503, code: 'DESKTOP_AGENT_UNAVAILABLE' }
    get.mockRejectedValueOnce(error)
    await expect(getDesktopAgentModels(8)).rejects.toBe(error)
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('leaves step-up and retry decisions to the existing controller', async () => {
    const error = { status: 403, code: 'STEP_UP_REQUIRED' }
    put.mockRejectedValueOnce(error)
    await expect(updateDesktopAgentConfig(initial())).rejects.toBe(error)
    expect(put).toHaveBeenCalledTimes(1)
  })

  it('leaves disable step-up and retry decisions to the controller', async () => {
    const error = { status: 403, code: 'STEP_UP_REQUIRED' }
    post.mockRejectedValueOnce(error)
    await expect(disableDesktopAgentConfig()).rejects.toBe(error)
    expect(post).toHaveBeenCalledTimes(1)
    expect(put).not.toHaveBeenCalled()
  })
  it('loads saved capability profiles without creating credentials or probing the upstream', async () => {
    const profiles = { profiles: [] }
    get.mockResolvedValueOnce({ data: profiles })
    await expect(getDesktopAgentProfiles()).resolves.toBe(profiles)
    expect(get).toHaveBeenCalledWith('/admin/desktop/profiles')
    expect(post).not.toHaveBeenCalled()
  })

})
