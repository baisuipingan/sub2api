import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TaskRecord } from '../../types'

const { discardGoneTask, ensureGalleryIdentity, uploadGalleryTask } = vi.hoisted(() => ({
  discardGoneTask: vi.fn(), ensureGalleryIdentity: vi.fn(), uploadGalleryTask: vi.fn(),
}))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  discardGoneTask,
}))
vi.mock('../../lib/galleryApi', async () => ({
  ...await vi.importActual<typeof import('../../lib/galleryApi')>('../../lib/galleryApi'),
  ensureGalleryIdentity,
}))
vi.mock('../../lib/gallerySync', async () => ({
  ...await vi.importActual<typeof import('../../lib/gallerySync')>('../../lib/gallerySync'),
  uploadGalleryTask,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { setImageStudioUserScope } from '../../lib/db'
import { GalleryApiError } from '../../lib/galleryApi'
import { storeState as state, syncGalleryTask, useStore } from '../store'

const task = { id: 'gone-task', status: 'done', inputImageIds: [], outputImages: ['image'] } as TaskRecord

beforeEach(() => {
  localStorage.setItem('auth_token', 'token')
  localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
  setImageStudioUserScope(7)
  discardGoneTask.mockReset().mockResolvedValue(undefined)
  ensureGalleryIdentity.mockReset().mockResolvedValue(undefined)
  uploadGalleryTask.mockReset()
  useStore.setState({ tasks: [task], galleryEnabled: true, galleryUserId: 7, gallerySyncStatus: { [task.id]: 'pending' } })
})

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
  })
})
