import { beforeEach, describe, expect, it, vi } from 'vitest'

const { loadPrivateImageVariant, deletePrivateImageVariants, clearPrivateImageVariants, identity } = vi.hoisted(() => ({
  loadPrivateImageVariant: vi.fn(), deletePrivateImageVariants: vi.fn(), clearPrivateImageVariants: vi.fn(),
  identity: { token: 'token', userId: 1, scope: 'user-scope' },
}))
vi.mock('@/lib/privateImageVariantCache', () => ({ loadPrivateImageVariant, deletePrivateImageVariants, clearPrivateImageVariants }))

const { getImage, getStoredFreshImageThumbnail, getImageThumbnail, putImage, ensureGalleryIdentity, fetchGalleryAssetDataUrl, getGalleryAssetUrl } = vi.hoisted(() => ({
  getImage: vi.fn(), getStoredFreshImageThumbnail: vi.fn(), getImageThumbnail: vi.fn(), putImage: vi.fn(),
  ensureGalleryIdentity: vi.fn(), fetchGalleryAssetDataUrl: vi.fn(), getGalleryAssetUrl: vi.fn(),
}))
vi.mock('../db', () => ({
  CURRENT_THUMBNAIL_VERSION: 2, getImage, getStoredFreshImageThumbnail, getImageThumbnail, putImage,
}))
vi.mock('../galleryApi', () => ({ ensureGalleryIdentity, fetchGalleryAssetDataUrl, getGalleryAssetUrl }))
vi.mock('../gallerySession', () => ({
  captureGallerySession: () => ({ ...identity }),
  isGallerySessionCurrent: (session: typeof identity) => session.token === identity.token && session.userId === identity.userId && session.scope === identity.scope,
  mayReadGalleryCache: () => true,
}))

import { clearImageCaches, clearImageVariantCaches, deleteCachedImage, deleteImageCacheEntry, ensureImageCached, ensureImagePreviewUrl, ensureImageThumbnailCached, registerCloudAssetIds } from '../imageCache'

beforeEach(() => {
  vi.clearAllMocks()
  Object.assign(identity, { token: 'token', userId: 1, scope: 'user-scope' })
  clearImageCaches()
  deletePrivateImageVariants.mockResolvedValue(undefined)
  clearPrivateImageVariants.mockResolvedValue(undefined)
  loadPrivateImageVariant.mockImplementation(async (options: { authorize: () => Promise<Record<string, unknown>>; isCurrent: () => boolean }) => {
    const result = await options.authorize()
    if (!options.isCurrent()) throw new Error('identity changed')
    return { ...result, cached: false }
  })
  getImage.mockResolvedValue(undefined)
  getStoredFreshImageThumbnail.mockResolvedValue(undefined)
  getImageThumbnail.mockResolvedValue(undefined)
  putImage.mockResolvedValue(undefined)
  const expires_at = new Date(Date.now() + 600_000).toISOString()
  getGalleryAssetUrl.mockImplementation(async (_id: string, variant: string) => ({ url: 'https://storage.test/' + variant, expires_at, width: 480, height: 480, bytes: 100, mime: 'image/webp', cache_version: 'asset-sha-v1' }))
})

describe('cloud gallery image cache', () => {
  it('uses the 1600px preview URL without reading full-resolution bytes', async () => {
    registerCloudAssetIds(['cloud-image'])
    expect(await ensureImagePreviewUrl('cloud-image')).toBe('https://storage.test/preview')
    expect(getGalleryAssetUrl).toHaveBeenCalledWith('cloud-image', 'preview')
    expect(fetchGalleryAssetDataUrl).not.toHaveBeenCalled()
    expect(getImage).not.toHaveBeenCalled()
  })

  it('uses the thumbnail URL without fetching the original image', async () => {
    registerCloudAssetIds(['cloud-image'])
    const thumbnail = await ensureImageThumbnailCached('cloud-image')
    expect(thumbnail?.dataUrl).toBe('https://storage.test/thumbnail')
    expect(getGalleryAssetUrl).toHaveBeenCalledWith('cloud-image', 'thumbnail')
    expect(fetchGalleryAssetDataUrl).not.toHaveBeenCalled()
  })

  it('fetches original bytes only when explicitly requested', async () => {
    fetchGalleryAssetDataUrl.mockResolvedValue('data:image/png;base64,aW1hZ2U=')
    expect(await ensureImageCached('cloud-image')).toBe('data:image/png;base64,aW1hZ2U=')
    expect(fetchGalleryAssetDataUrl).toHaveBeenCalledWith('cloud-image', 'original')
    expect(putImage).toHaveBeenCalledWith(expect.objectContaining({ id: 'cloud-image' }), 'user-scope')
  })
})


describe('persistent cloud variants integration', () => {
  it('uses the private cache with the verified viewer, owner, immutable image ID and exact variant', async () => {
    loadPrivateImageVariant.mockImplementation(async (options) => ({ ...await options.authorize(), url: 'data:image/webp;base64,cached-preview', cached: true }))
    registerCloudAssetIds(['cloud-image'])
    expect(await ensureImagePreviewUrl('cloud-image')).toBe('data:image/webp;base64,cached-preview')
    expect(loadPrivateImageVariant).toHaveBeenLastCalledWith(expect.objectContaining({ viewerId: 1, ownerId: 1, imageId: 'cloud-image', variant: 'preview' }))
    expect(getGalleryAssetUrl).toHaveBeenCalledWith('cloud-image', 'preview')
    expect(fetchGalleryAssetDataUrl).not.toHaveBeenCalled()
    expect(putImage).not.toHaveBeenCalled()
    expect(await ensureImagePreviewUrl('cloud-image')).toBe('data:image/webp;base64,cached-preview')
    expect(loadPrivateImageVariant).toHaveBeenCalledTimes(1)
  })

  it('reauthorizes a cloud variant after memory is cleared without changing the original-image store', async () => {
    loadPrivateImageVariant.mockImplementation(async (options) => ({ ...await options.authorize(), url: 'data:image/webp;base64,cached-thumbnail', cached: true }))
    registerCloudAssetIds(['cloud-image'])
    expect((await ensureImageThumbnailCached('cloud-image'))?.dataUrl).toBe('data:image/webp;base64,cached-thumbnail')
    clearImageCaches()
    registerCloudAssetIds(['cloud-image'])
    expect((await ensureImageThumbnailCached('cloud-image'))?.dataUrl).toBe('data:image/webp;base64,cached-thumbnail')
    expect(getGalleryAssetUrl).toHaveBeenCalledTimes(2)
    expect(clearPrivateImageVariants).not.toHaveBeenCalled()
    expect(putImage).not.toHaveBeenCalled()
  })

  it('keeps existing local thumbnails ahead of cloud reads', async () => {
    getStoredFreshImageThumbnail.mockResolvedValue({ thumbnailDataUrl: 'data:image/png;base64,local', width: 60, height: 40, thumbnailVersion: 2 })
    registerCloudAssetIds(['cloud-image'])
    expect((await ensureImageThumbnailCached('cloud-image'))?.dataUrl).toBe('data:image/png;base64,local')
    expect(loadPrivateImageVariant).not.toHaveBeenCalled()
    expect(getGalleryAssetUrl).not.toHaveBeenCalled()
  })

  it('does not deliver or cache an old account cloud result after an account switch', async () => {
    let resolve!: (value: Record<string, unknown>) => void
    getGalleryAssetUrl.mockReturnValueOnce(new Promise(done => { resolve = done }))
    registerCloudAssetIds(['same-image'])
    const result = ensureImagePreviewUrl('same-image')
    await vi.waitFor(() => expect(getGalleryAssetUrl).toHaveBeenCalled())
    Object.assign(identity, { token: 'next-token', userId: 2, scope: 'next-user-scope' })
    resolve({ url: 'https://storage.test/old-preview', expires_at: new Date(Date.now() + 600_000).toISOString(), width: 100, height: 100, bytes: 100, mime: 'image/webp' })
    expect(await result).toBeUndefined()
    clearImageCaches()
    registerCloudAssetIds(['same-image'])
    await ensureImagePreviewUrl('same-image')
    expect(loadPrivateImageVariant).toHaveBeenLastCalledWith(expect.objectContaining({ viewerId: 2, ownerId: 2, imageId: 'same-image' }))
  })

  it('makes preview retry wait for targeted invalidation instead of rereading a corrupt preview', async () => {
    let finish!: () => void
    deletePrivateImageVariants.mockReturnValueOnce(new Promise<void>(resolve => { finish = resolve }))
    registerCloudAssetIds(['cloud-image'])
    deleteCachedImage('cloud-image')
    const loading = ensureImagePreviewUrl('cloud-image')
    await vi.waitFor(() => expect(deletePrivateImageVariants).toHaveBeenCalledWith(1, 1, ['cloud-image'], ['preview']))
    expect(loadPrivateImageVariant).not.toHaveBeenCalled()
    finish()
    await loading
    expect(loadPrivateImageVariant).toHaveBeenCalledTimes(1)
  })

  it('invalidates both reduced variants when an unreferenced image is deleted', async () => {
    deleteImageCacheEntry('unused-image')
    await vi.waitFor(() => expect(deletePrivateImageVariants).toHaveBeenCalledWith(1, 1, ['unused-image'], ['thumbnail', 'preview']))
    expect(fetchGalleryAssetDataUrl).not.toHaveBeenCalled()
  })

  it('clears only the captured viewer and owner variant cache from settings', async () => {
    await clearImageVariantCaches({ ...identity })
    expect(clearPrivateImageVariants).toHaveBeenCalledWith(1, 1)
    expect(fetchGalleryAssetDataUrl).not.toHaveBeenCalled()
  })
})
