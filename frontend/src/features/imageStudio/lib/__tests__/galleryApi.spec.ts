import { beforeEach, describe, expect, it, vi } from 'vitest'

const { imageSourceToBlob, imageDataUrlToPngBlob } = vi.hoisted(() => ({
  imageSourceToBlob: vi.fn(), imageDataUrlToPngBlob: vi.fn(),
}))
vi.mock('../dataUrl', () => ({ imageSourceToBlob, blobToDataUrl: vi.fn() }))
vi.mock('../canvasImage', () => ({ imageDataUrlToPngBlob }))
vi.mock('../gallerySession', () => ({
  currentGalleryUserId: () => 1,
  isVerifiedGalleryAuth: () => true,
  verifyGalleryIdentity: vi.fn(),
}))

import { fetchGalleryAssetBlob, GalleryApiError, getGalleryStatus, listGalleryTasks, putGalleryAsset } from '../galleryApi'

function apiResponse(data: unknown, code = 0) {
  return new Response(JSON.stringify({ code, data }), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  })
}

beforeEach(() => {
  localStorage.setItem('auth_token', 'token')
  vi.unstubAllGlobals()
  imageSourceToBlob.mockReset()
  imageDataUrlToPngBlob.mockReset()
})

describe('gallery API contract', () => {
  it('reads canonical status and cursor pagination envelopes', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(apiResponse({ enabled: true, user_id: 1 }))
      .mockResolvedValueOnce(apiResponse({ items: [], next_cursor: 'next' }))
    vi.stubGlobal('fetch', fetchMock)
    expect(await getGalleryStatus()).toEqual({ enabled: true, user_id: 1 })
    expect(await listGalleryTasks('previous', 12)).toEqual({ items: [], next_cursor: 'next' })
    expect(fetchMock.mock.calls[1][0]).toContain('/tasks?limit=12&cursor=previous')
  })

  it('converts unsupported source formats to PNG before uploading', async () => {
    const source = new Blob(['avif'], { type: 'image/avif' })
    const png = new Blob(['png'], { type: 'image/png' })
    imageSourceToBlob.mockResolvedValue(source)
    imageDataUrlToPngBlob.mockResolvedValue(png)
    const fetchMock = vi.fn().mockResolvedValue(apiResponse({ id: 'image-1' }))
    vi.stubGlobal('fetch', fetchMock)
    expect(await putGalleryAsset('image-1', 'data:image/avif;base64,aW1hZ2U=')).toEqual({ id: 'image-1' })
    expect(imageDataUrlToPngBlob).toHaveBeenCalledOnce()
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: 'PUT', body: png, headers: expect.objectContaining({ 'Content-Type': 'image/png' }) })
  })

  it('does not upload a prepared image after the login changes', async () => {
    imageSourceToBlob.mockImplementation(async () => {
      localStorage.setItem('auth_token', 'another-user')
      return new Blob(['png'], { type: 'image/png' })
    })
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await expect(putGalleryAsset('image-1', 'data:image/png;base64,aW1hZ2U=', 'token'))
      .rejects.toMatchObject({ status: 401, message: 'Gallery login changed' })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('reports cross-origin storage failures as a CORS-readable error', async () => {
    const asset = { url: 'https://storage.test/original', expires_at: 'future', mime: 'image/png', bytes: 3, width: 1, height: 1 }
    const fetchMock = vi.fn().mockResolvedValueOnce(apiResponse(asset)).mockRejectedValueOnce(new TypeError('Failed to fetch'))
    vi.stubGlobal('fetch', fetchMock)
    await expect(fetchGalleryAssetBlob('image-1', 'original')).rejects.toMatchObject({
      name: 'GalleryApiError', status: 0, message: expect.stringContaining('CORS'),
    })
  })

  it('rejects application errors even when HTTP status is 200', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(apiResponse(null, 1)))
    await expect(getGalleryStatus()).rejects.toBeInstanceOf(GalleryApiError)
  })

  it('preserves the backend top-level reason for tombstones', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      code: 410, message: 'gallery task was deleted', reason: 'GALLERY_GONE',
    }), { status: 410 })))
    await expect(getGalleryStatus()).rejects.toMatchObject({ status: 410, code: 'GALLERY_GONE' })
  })
})
