import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getGalleryStatus, listGalleryTasks, getAllTasks, getPendingSyncTaskIds, getPendingSyncRecords, putTaskWithPendingSync } = vi.hoisted(() => ({
  getGalleryStatus: vi.fn(), listGalleryTasks: vi.fn(), getAllTasks: vi.fn(), getPendingSyncTaskIds: vi.fn(), getPendingSyncRecords: vi.fn(), putTaskWithPendingSync: vi.fn(),
}))
vi.mock('../../lib/galleryApi', async () => ({
  ...await vi.importActual<typeof import('../../lib/galleryApi')>('../../lib/galleryApi'),
  getGalleryStatus, listGalleryTasks, ensureGalleryIdentity: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  getAllTasks, getPendingSyncTaskIds, getPendingSyncRecords, putTaskWithPendingSync,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { getImageStudioDbScope, setImageStudioUserScope } from '../../lib/db'
import { initStore, storeState as state, useStore } from '../store'

function tokenFor(id: number) {
  return [btoa('{}'), btoa(JSON.stringify({ user_id: id, sid: 'session-' + id })), 'signature'].join('.')
}

beforeEach(() => {
  localStorage.clear()
  setImageStudioUserScope(null)
  getGalleryStatus.mockReset()
  listGalleryTasks.mockReset().mockResolvedValue({ items: [], next_cursor: null })
  getAllTasks.mockReset().mockResolvedValue([])
  getPendingSyncTaskIds.mockReset().mockResolvedValue([])
  getPendingSyncRecords.mockReset().mockResolvedValue([])
  putTaskWithPendingSync.mockReset().mockResolvedValue(undefined)
})

describe('Image Studio account initialization', () => {
  it('switches DB scope and clears the prior key before waiting for B status', async () => {
    localStorage.setItem('auth_token', tokenFor(101))
    localStorage.setItem('auth_user', JSON.stringify({ id: 101 }))
    getGalleryStatus.mockResolvedValueOnce({ enabled: false, user_id: 101 })
    await initStore()
    state.setMiloRouterConfig({ apiKey: 'key-a' })
    state.setMiloRouterConfig({ connectionStatus: 'connected' })
    state.setImagePricing({ model: { '2K': 1 } })
    state.setConfirmDialog({ title: 'A', message: 'A', action: vi.fn() })

    let finish!: (status: { enabled: boolean; user_id: number }) => void
    getGalleryStatus.mockReturnValueOnce(new Promise((resolve) => { finish = resolve }))
    localStorage.setItem('auth_token', tokenFor(202))
    localStorage.setItem('auth_user', JSON.stringify({ id: 202 }))
    const initialization = initStore()
    expect(getImageStudioDbScope()).toBe('milorouter-ai-image-user-202')
    expect(state.settings.miloRouter.apiKey).toBe('')
    expect(state.settings.miloRouter.connectionStatus).toBe('idle')
    expect(state.imagePricing).toEqual({})
    expect(state.confirmDialog).toBeNull()
    finish({ enabled: false, user_id: 202 })
    await initialization
    expect(state.galleryUserId).toBe(202)
  })

  it('quarantines B token while the cached user record still belongs to A', async () => {
    localStorage.setItem('auth_token', tokenFor(202))
    localStorage.setItem('auth_user', JSON.stringify({ id: 101 }))
    getGalleryStatus.mockResolvedValueOnce({ enabled: true, user_id: 202 })
    const initialization = initStore()
    expect(getImageStudioDbScope()).toBe('milorouter-ai-image-unassigned')
    await initialization
    expect(state.galleryInitialized).toBe(false)
    expect(state.galleryUserId).toBeNull()
    expect(getAllTasks).not.toHaveBeenCalled()
    useStore.setState({ galleryEnabled: false })
  })

  it('marks an interrupted local task for eventual cloud retry', async () => {
    localStorage.setItem('auth_token', tokenFor(303))
    localStorage.setItem('auth_user', JSON.stringify({ id: 303 }))
    getGalleryStatus.mockResolvedValueOnce({ enabled: false, user_id: 303 })
    getAllTasks.mockResolvedValueOnce([{
      id: 'interrupted', status: 'running', prompt: 'A landscape',
      params: { size: 'auto', resolution: '2K', n: 1 }, inputImageIds: [], outputImages: [], createdAt: 1,
    }])
    await initStore()
    expect(putTaskWithPendingSync).toHaveBeenCalledWith(expect.objectContaining({
      id: 'interrupted', status: 'error',
    }), 'milorouter-ai-image-user-303')
  })
})
