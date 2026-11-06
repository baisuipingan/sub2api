import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getImage, getStoredFreshImageThumbnail, getImageThumbnail, putImage, ensureGalleryIdentity, fetchGalleryAssetDataUrl, getGalleryAssetUrl } = vi.hoisted(() => ({
  getImage: vi.fn(), getStoredFreshImageThumbnail: vi.fn(), getImageThumbnail: vi.fn(), putImage: vi.fn(),
  ensureGalleryIdentity: vi.fn(), fetchGalleryAssetDataUrl: vi.fn(), getGalleryAssetUrl: vi.fn(),
}))
vi.mock('../db', () => ({
  CURRENT_THUMBNAIL_VERSION: 2, getImage, getStoredFreshImageThumbnail, getImageThumbnail, putImage,
}))
vi.mock('../galleryApi', () => ({ ensureGalleryIdentity, fetchGalleryAssetDataUrl, getGalleryAssetUrl }))
vi.mock('../gallerySession', () => ({
  captureGallerySession: () => ({ token: 'token', userId: 1, scope: 'user-scope' }),
  isGallerySessionCurrent: () => true,
  mayReadGalleryCache: () => true,
}))

import { clearImageCaches, ensureImageCached, ensureImagePreviewUrl, ensureImageThumbnailCached, registerCloudAssetIds } from '../imageCache'

beforeEach(() => {
  vi.clearAllMocks()
  clearImageCaches()
  getImage.mockResolvedValue(undefined)
  getStoredFreshImageThumbnail.mockResolvedValue(undefined)
  getImageThumbnail.mockResolvedValue(undefined)
  putImage.mockResolvedValue(undefined)
  const expires_at = new Date(Date.now() + 600_000).toISOString()
  getGalleryAssetUrl.mockImplementation(async (_id: string, variant: string) => ({ url: 'https://storage.test/' + variant, expires_at, width: 480, height: 480 }))
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
