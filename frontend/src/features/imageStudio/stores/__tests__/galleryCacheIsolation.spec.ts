import { beforeEach, describe, expect, it, vi } from 'vitest'

const { clearTasks, clearImages, getPendingSyncTaskIds, deleteImage, deleteImageCacheEntry, clearImageVariantCaches } = vi.hoisted(() => ({
  clearTasks: vi.fn(),
  clearImages: vi.fn(),
  getPendingSyncTaskIds: vi.fn(),
  deleteImage: vi.fn(),
  deleteImageCacheEntry: vi.fn(),
  clearImageVariantCaches: vi.fn(),
}))

vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  clearTasks, clearImages, getPendingSyncTaskIds, deleteImage,
}))
vi.mock('../../lib/imageCache', async () => ({
  ...await vi.importActual<typeof import('../../lib/imageCache')>('../../lib/imageCache'),
  deleteImageCacheEntry, clearImageVariantCaches,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { setImageStudioUserScope } from '../../lib/db'
import { verifyGalleryIdentity } from '../../lib/gallerySession'
import { clearData, deleteImageIfUnreferenced, submitTask, useStore } from '../store'

beforeEach(() => {
  for (const mock of [clearTasks, clearImages, getPendingSyncTaskIds, deleteImage, deleteImageCacheEntry, clearImageVariantCaches]) mock.mockReset()
  clearTasks.mockResolvedValue(undefined)
  clearImages.mockResolvedValue(undefined)
  deleteImage.mockResolvedValue(undefined)
  clearImageVariantCaches.mockResolvedValue(undefined)
  localStorage.setItem('auth_token', 'token-a')
  localStorage.setItem('auth_user', JSON.stringify({ id: 42 }))
  setImageStudioUserScope(42)
  verifyGalleryIdentity(42, 'token-a')
  useStore.setState({ tasks: [], inputImages: [], maskDraft: null, imageEditSession: null, galleryInitialized: true, galleryUserId: 42 })
})

describe('gallery local-cache account isolation', () => {
  it('blocks new generation while a local-cache clear is in progress', async () => {
    let finish!: (ids: string[]) => void
    getPendingSyncTaskIds.mockReturnValueOnce(new Promise<string[]>((resolve) => { finish = resolve }))
    const clearing = clearData({ clearTasks: true })
    await submitTask()
    expect(useStore.getState().toast?.message).toBe('imageStudio.gallery.cacheClearing')
    finish([])
    await clearing
    expect(clearImageVariantCaches).toHaveBeenCalledWith(expect.objectContaining({ userId: 42, scope: 'milorouter-ai-image-user-42' }))
  })

  it('does not clear the next account after switching during pending-sync lookup', async () => {
    let finish!: (ids: string[]) => void
    getPendingSyncTaskIds.mockReturnValueOnce(new Promise<string[]>((resolve) => { finish = resolve }))

    const clearing = clearData({ clearTasks: true })
    localStorage.setItem('auth_token', 'token-b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 99 }))
    setImageStudioUserScope(99)
    useStore.setState({ galleryUserId: 99 })
    finish([])
    await clearing

    expect(clearTasks).not.toHaveBeenCalled()
    expect(clearImages).not.toHaveBeenCalled()
    expect(clearImageVariantCaches).not.toHaveBeenCalled()
  })

  it('deletes only the captured account image and never clears the next account cache', async () => {
    let finish!: () => void
    deleteImage.mockReturnValueOnce(new Promise<void>((resolve) => { finish = resolve }))

    const deleting = deleteImageIfUnreferenced('old-image')
    expect(deleteImage).toHaveBeenCalledWith('old-image', 'milorouter-ai-image-user-42')
    localStorage.setItem('auth_token', 'token-b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 99 }))
    setImageStudioUserScope(99)
    useStore.setState({ galleryUserId: 99 })
    finish()
    await deleting

    expect(deleteImageCacheEntry).not.toHaveBeenCalled()
  })
})
