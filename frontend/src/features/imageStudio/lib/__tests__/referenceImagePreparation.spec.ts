import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
const { loadImage, canvasToBlob, blobToDataUrl } = vi.hoisted(() => ({ loadImage: vi.fn(), canvasToBlob: vi.fn(), blobToDataUrl: vi.fn() }))
vi.mock('../canvasImage', () => ({ loadImage, canvasToBlob }))
vi.mock('../dataUrl', () => ({ blobToDataUrl, imageSourceToBlob: vi.fn() }))
vi.mock('../imageApiShared', async () => ({
  ...await vi.importActual<typeof import('../imageApiShared')>('../imageApiShared'),
  getDataUrlDecodedByteSize: (value: string) => value.includes('oversized-mask') ? 33 * 1024 * 1024 : 100,
}))
vi.mock('../i18nMessage', () => ({ libT: (key: string) => key }))
import { GALLERY_REFERENCE_MAX_BYTES, prepareStudioReference, referencePreparationSize } from '../referenceImagePreparation'

beforeEach(() => {
  vi.clearAllMocks()
  canvasToBlob.mockImplementation(async (_canvas: HTMLCanvasElement, mime: string) => new Blob(['image'], { type: mime }))
  blobToDataUrl.mockImplementation(async (blob: Blob) => `data:${blob.type};base64,cHJlcGFyZWQ=`)
})
afterEach(() => vi.restoreAllMocks())

describe('oversized cloud reference copies', () => {
  it('keeps ordinary image dimensions and bytes completely unchanged', async () => {
    loadImage.mockResolvedValue({ naturalWidth: 2048, naturalHeight: 2048, src: '' })
    const original = 'data:image/jpeg;base64,/9j/'
    expect(await prepareStudioReference(original)).toEqual({ dataUrl: original, maskDataUrl: undefined, width: 2048, height: 2048, changed: false })
    expect(canvasToBlob).not.toHaveBeenCalled()
    expect(referencePreparationSize(4000, 5000, 100)).toBeNull()
  })

  it('fits the reported 9000×4367 image proportionally within 4096px', () => {
    const size = referencePreparationSize(9000, 4367, 1_741_565)
    expect(size).toEqual({ width: 4096, height: 1987 })
    expect(size!.width * size!.height).toBeLessThan(20_000_000)
    expect(referencePreparationSize(1000, 1000, GALLERY_REFERENCE_MAX_BYTES + 1)).toEqual({ width: 1000, height: 1000 })
  })

  it('resizes a source and its mask to exactly the same dimensions without JPEG mask alpha', async () => {
    loadImage.mockResolvedValue({ naturalWidth: 9000, naturalHeight: 4367, src: '' })
    const draws = vi.fn()
    const canvas = { width: 0, height: 0, getContext: () => ({ drawImage: draws }) }
    vi.spyOn(document, 'createElement').mockReturnValue(canvas as unknown as HTMLCanvasElement)
    const prepared = await prepareStudioReference('data:image/jpeg;base64,/9j/', 'data:image/png;base64,bWFzaw==')
    expect(prepared).toMatchObject({ changed: true, width: 4096, height: 1987, dataUrl: 'data:image/jpeg;base64,cHJlcGFyZWQ=', maskDataUrl: 'data:image/png;base64,cHJlcGFyZWQ=' })
    expect(draws).toHaveBeenCalledTimes(2)
    for (const call of draws.mock.calls) expect(call.slice(1)).toEqual([0, 0, 4096, 1987])
    expect(canvasToBlob.mock.calls.map(call => call[1])).toEqual(['image/png', 'image/jpeg'])
  })

  it('rejects a mismatched original mask rather than stretching it onto the source', async () => {
    loadImage.mockResolvedValueOnce({ naturalWidth: 9000, naturalHeight: 4367, src: '' }).mockResolvedValueOnce({ naturalWidth: 8000, naturalHeight: 4367, src: '' })
    await expect(prepareStudioReference('data:image/jpeg;base64,/9j/', 'mask')).rejects.toThrow('maskSizeMismatchRedraw')
    expect(canvasToBlob).not.toHaveBeenCalled()
  })

  it('normalizes an oversized mask even when its source is within the normal limits', async () => {
    loadImage.mockResolvedValue({ naturalWidth: 2048, naturalHeight: 2048, src: '' })
    vi.spyOn(document, 'createElement').mockReturnValue({ width: 0, height: 0, getContext: () => ({ drawImage: vi.fn() }) } as unknown as HTMLCanvasElement)
    const prepared = await prepareStudioReference('data:image/jpeg;base64,/9j/', 'oversized-mask')
    expect(prepared).toMatchObject({ changed: true, width: 2048, height: 2048 })
    expect(canvasToBlob).toHaveBeenCalledTimes(2)
  })
})
