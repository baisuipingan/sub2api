import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { TaskRecord } from '../../types'

const { discardGoneTask, clearTaskPendingSync, ensureGalleryIdentity, uploadGalleryTask, getTaskSyncRecord, setTaskSyncRecord, getGalleryStatus } = vi.hoisted(() => ({
  discardGoneTask: vi.fn(), ensureGalleryIdentity: vi.fn(), uploadGalleryTask: vi.fn(),
  getTaskSyncRecord: vi.fn(), setTaskSyncRecord: vi.fn(), getGalleryStatus: vi.fn(), clearTaskPendingSync: vi.fn(),
}))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  discardGoneTask, clearTaskPendingSync, getTaskSyncRecord, setTaskSyncRecord,
}))
vi.mock('../../lib/galleryApi', async () => ({
  ...await vi.importActual<typeof import('../../lib/galleryApi')>('../../lib/galleryApi'),
  ensureGalleryIdentity, getGalleryStatus,
}))
vi.mock('../../lib/gallerySync', async () => ({
  ...await vi.importActual<typeof import('../../lib/gallerySync')>('../../lib/gallerySync'),
  uploadGalleryTask,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { setImageStudioUserScope } from '../../lib/db'
import { GalleryApiError } from '../../lib/galleryApi'
import { storeState as state, syncGalleryTask, retryGalleryTask, setGallerySyncPaused, useStore } from '../store'

const task = { id: 'gone-task', status: 'done', inputImageIds: [], outputImages: ['image'] } as TaskRecord
let syncRecord = { status: 'pending', attempts: 0, nextRetryAt: 0, retryable: true, errorCode: undefined as string | undefined }

beforeEach(() => {
  localStorage.setItem('auth_token', 'token')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
  setImageStudioUserScope(7)
  discardGoneTask.mockReset().mockResolvedValue(undefined)
  ensureGalleryIdentity.mockReset().mockResolvedValue(undefined)
  uploadGalleryTask.mockReset()
  syncRecord = { status: 'pending', attempts: 0, nextRetryAt: 0, retryable: true, errorCode: undefined }
  getTaskSyncRecord.mockReset().mockImplementation(async () => syncRecord)
  setTaskSyncRecord.mockReset().mockImplementation(async (_id, record) => { syncRecord = record })
  clearTaskPendingSync.mockReset().mockResolvedValue(undefined)
  getGalleryStatus.mockReset().mockResolvedValue({ enabled: true, user_id: 7, stats: null })
  useStore.setState({ tasks: [task], galleryEnabled: true, galleryUserId: 7, gallerySyncPaused: false, gallerySyncStatus: { [task.id]: 'pending' } })
})
afterEach(() => setGallerySyncPaused(true))

describe('gallery tombstone retry', () => {
  it('drops the local retry marker and task after GALLERY_GONE', async () => {
    uploadGalleryTask.mockRejectedValueOnce(new GalleryApiError('deleted', 410, 'GALLERY_GONE'))
    await syncGalleryTask(task.id)
    expect(discardGoneTask).toHaveBeenCalledWith(task.id, 'milorouter-ai-image-user-7')
    expect(state.tasks).toEqual([])
    expect(state.gallerySyncStatus).toEqual({})
  })

  it('keeps a temporarily failed task available for retry', async () => {
    uploadGalleryTask.mockRejectedValueOnce(new GalleryApiError('unavailable', 503, 'GALLERY_UNAVAILABLE'))
    await syncGalleryTask(task.id)
    expect(discardGoneTask).not.toHaveBeenCalled()
    expect(state.tasks).toEqual([task])
    expect(state.gallerySyncStatus[task.id]).toBe('error')
    expect(syncRecord.retryable).toBe(true)
    expect(syncRecord.nextRetryAt).toBeGreaterThan(Date.now())
    await syncGalleryTask(task.id)
    expect(uploadGalleryTask).toHaveBeenCalledTimes(1)
    uploadGalleryTask.mockResolvedValueOnce(undefined)
    await retryGalleryTask(task.id)
    expect(uploadGalleryTask).toHaveBeenCalledTimes(2)
    expect(clearTaskPendingSync).toHaveBeenCalledWith(task.id, 'milorouter-ai-image-user-7')
  })
})
