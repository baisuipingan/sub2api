import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Blob as NodeBlob } from 'node:buffer'

const { loadImage, storeImageWithSize } = vi.hoisted(() => ({
  loadImage: vi.fn(),
  storeImageWithSize: vi.fn(),
}))
vi.mock('../../lib/canvasImage', async () => ({
  ...await vi.importActual<typeof import('../../lib/canvasImage')>('../../lib/canvasImage'),
  loadImage,
}))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  storeImageWithSize,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { addImageFromUrl, storeState as state, useStore } from '../store'

const image = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('Blob', NodeBlob)
  useStore.setState({ inputImages: [] })
  loadImage.mockReset().mockResolvedValue({ naturalWidth: 128, naturalHeight: 96 })
  storeImageWithSize.mockReset().mockResolvedValue({ id: 'reference-image' })
})

afterEach(() => vi.unstubAllGlobals())

describe('reference image imports', () => {
  it('imports in-memory data without fetch, then decodes before adding the reference', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    await addImageFromUrl(image)
    expect(fetch).not.toHaveBeenCalled()
    expect(loadImage).toHaveBeenCalledWith(image)
    expect(storeImageWithSize).toHaveBeenCalledWith(image, 'upload')
    expect(state.inputImages).toEqual([{ id: 'reference-image', dataUrl: image }])
  })

  it('rejects empty sources before fetching or saving a webpage as an image', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    await expect(addImageFromUrl('  ')).rejects.toThrow()
    expect(fetch).not.toHaveBeenCalled()
    expect(storeImageWithSize).not.toHaveBeenCalled()
    expect(state.inputImages).toEqual([])
  })

  it('rejects an HTTP 200 HTML page before persisting or displaying it', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      blob: async () => new NodeBlob(['<!doctype html><title>Studio</title>'], { type: 'text/html' }),
    })))
    loadImage.mockRejectedValueOnce(new Error('Image cannot decode HTML'))
    await expect(addImageFromUrl('/image-studio')).rejects.toThrow('imageStudio.store.imageReadFailed')
    expect(storeImageWithSize).not.toHaveBeenCalled()
    expect(state.inputImages).toEqual([])
  })

  it('does not persist corrupt bytes labelled as an image', async () => {
    loadImage.mockRejectedValueOnce(new Error('Corrupt image'))
    await expect(addImageFromUrl(image)).rejects.toThrow('imageStudio.store.imageReadFailed')
    expect(storeImageWithSize).not.toHaveBeenCalled()
    expect(state.inputImages).toEqual([])
  })
})
