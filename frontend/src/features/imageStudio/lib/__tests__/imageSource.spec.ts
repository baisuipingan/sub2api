import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Blob as NodeBlob } from 'node:buffer'
import { dataUrlToBlob, maskDataUrlToPngBlob } from '../canvasImage'
import { imageSourceToBlob } from '../dataUrl'
import { downloadImageSource } from '../downloadImage'

const PNG = 'data:image/png;base64,iVBORw0KGgo='
const fetchMock = vi.fn<typeof fetch>()
const createObjectURLMock = vi.fn<(blob: Blob) => string>()
const revokeObjectURLMock = vi.fn<(url: string) => void>()

beforeEach(() => {
  fetchMock.mockReset().mockRejectedValue(new Error('CSP blocks fetch(data:)'))
  createObjectURLMock.mockReset().mockReturnValue('blob:download-test')
  revokeObjectURLMock.mockReset()
  vi.stubGlobal('fetch', fetchMock)
  vi.stubGlobal('Blob', NodeBlob)
  vi.stubGlobal('URL', { createObjectURL: createObjectURLMock, revokeObjectURL: revokeObjectURLMock })
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function bytes(blob: Blob) {
  return [...new Uint8Array(await blob.arrayBuffer())]
}

describe('imageSourceToBlob', () => {
  it('decodes base64 locally with the declared image MIME', async () => {
    const blob = await imageSourceToBlob(PNG)

    expect(blob.type).toBe('image/png')
    expect(await bytes(blob)).toEqual([137, 80, 78, 71, 13, 10, 26, 10])
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('preserves binary percent-encoded bytes without decoding them as UTF-8 text', async () => {
    const blob = await imageSourceToBlob('data:image/png,%89PNG%0D%0A%1A%0A%00%FF+')

    expect(blob.type).toBe('image/png')
    expect(await bytes(blob)).toEqual([137, 80, 78, 71, 13, 10, 26, 10, 0, 255, 43])
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('decodes percent-escaped base64 and respects case-insensitive metadata', async () => {
    const blob = await imageSourceToBlob('DATA:IMAGE/JPEG;BASE64,%2F9j%2F')

    expect(blob.type).toBe('image/jpeg')
    expect(await bytes(blob)).toEqual([255, 216, 255])
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('preserves Unicode text and percent-encoded bytes in SVG payloads', async () => {
    const blob = await imageSourceToBlob('data:image/svg+xml;charset=utf-8,%3Csvg%3E猫%3C%2Fsvg%3E')

    expect(blob.type).toBe('image/svg+xml')
    expect(new TextDecoder().decode(await blob.arrayBuffer())).toBe('<svg>猫</svg>')
  })

  it('uses the fallback MIME when the data URL has none', async () => {
    const blob = await imageSourceToBlob('data:;base64,AQID', 'image/webp')

    expect(blob.type).toBe('image/webp')
    expect(await bytes(blob)).toEqual([1, 2, 3])
  })

  it.each(['', '   '])('rejects an empty source before fetch: %j', async (source) => {
    await expect(imageSourceToBlob(source)).rejects.toThrow('Image source is not available')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it.each(['data:image/png;base64', 'data:image/png,%GG', 'data:image/png,%8', 'data:image/png;base64,%%%'])('rejects malformed local data: %s', async (source) => {
    await expect(imageSourceToBlob(source)).rejects.toThrow()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it.each(['https://images.example/image.webp', 'blob:existing-image'])('reads a remote/blob source and keeps its MIME: %s', async (source) => {
    const expected = new Blob([Uint8Array.of(1, 2)], { type: 'image/webp' })
    fetchMock.mockResolvedValueOnce({ ok: true, status: 200, blob: async () => expected } as Response)

    expect(await imageSourceToBlob(source)).toBe(expected)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith(source)
  })

  it('rejects HTTP errors before reading or accepting the response body', async () => {
    const readBlob = vi.fn()
    fetchMock.mockResolvedValueOnce({ ok: false, status: 404, blob: readBlob } as unknown as Response)

    await expect(imageSourceToBlob('https://images.example/missing')).rejects.toThrow('HTTP 404')
    expect(readBlob).not.toHaveBeenCalled()
  })

  it('adds the fallback MIME when a successful response blob has none', async () => {
    fetchMock.mockResolvedValueOnce({ ok: true, status: 200, blob: async () => new Blob([Uint8Array.of(1, 2)]) } as Response)
    const blob = await imageSourceToBlob('https://images.example/raw', 'image/jpeg')

    expect(blob.type).toBe('image/jpeg')
    expect(await bytes(blob)).toEqual([1, 2])
  })
})

describe('reference upload image conversion', () => {
  it('converts reference data to the upload Blob without a CSP-blocked fetch', async () => {
    const blob = await dataUrlToBlob(PNG)
    expect(blob.type).toBe('image/png')
    expect(await bytes(blob)).toEqual([137, 80, 78, 71, 13, 10, 26, 10])
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('keeps PNG masks without a network fetch or canvas re-encoding', async () => {
    const blob = await maskDataUrlToPngBlob(PNG)
    expect(blob.type).toBe('image/png')
    expect(await bytes(blob)).toEqual([137, 80, 78, 71, 13, 10, 26, 10])
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

describe('downloadImageSource', () => {
  it.each([
    ['image/png', 'png'],
    ['image/jpeg', 'jpg'],
    ['image/webp', 'webp'],
    ['image/gif', 'gif'],
    ['image/avif', 'avif'],
  ])('downloads %s locally with .%s and releases the object URL after the click', async (mime, extension) => {
    vi.useFakeTimers()
    const clickedLinks: HTMLAnchorElement[] = []
    let connectedAtClick = false
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clickedLinks.push(this)
      connectedAtClick = this.isConnected
    })

    await downloadImageSource(Promise.resolve(`data:${mime};base64,AQID`), 'generated-image')
    const clickedLink = clickedLinks[0]

    expect(fetchMock).not.toHaveBeenCalled()
    expect(createObjectURLMock).toHaveBeenCalledTimes(1)
    expect(createObjectURLMock.mock.calls[0][0].type).toBe(mime)
    expect(connectedAtClick).toBe(true)
    expect(clickedLink?.getAttribute('href')).toBe('blob:download-test')
    expect(clickedLink?.download).toBe(`generated-image.${extension}`)
    expect(clickedLink?.rel).toBe('noopener')
    expect(clickedLink?.isConnected).toBe(false)
    expect(revokeObjectURLMock).not.toHaveBeenCalled()
    await vi.runOnlyPendingTimersAsync()
    expect(revokeObjectURLMock).toHaveBeenCalledTimes(1)
    expect(revokeObjectURLMock).toHaveBeenCalledWith('blob:download-test')
  })

  it('downloads HTTP images after checking response status', async () => {
    vi.useFakeTimers()
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    fetchMock.mockResolvedValueOnce({ ok: true, status: 200, blob: async () => new Blob(['image'], { type: 'image/jpeg' }) } as Response)

    await downloadImageSource('https://images.example/result', 'result')

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('https://images.example/result')
    expect(click).toHaveBeenCalledTimes(1)
    expect((click.mock.instances[0] as HTMLAnchorElement).download).toBe('result.jpg')
    await vi.runOnlyPendingTimersAsync()
  })

  it.each([undefined, '', '   '])('rejects unavailable download sources without creating a link: %j', async (source) => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await expect(downloadImageSource(Promise.resolve(source))).rejects.toThrow('Image source is not available')
    expect(fetchMock).not.toHaveBeenCalled()
    expect(createObjectURLMock).not.toHaveBeenCalled()
    expect(click).not.toHaveBeenCalled()
  })

  it('does not trigger a download when the HTTP source fails', async () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    fetchMock.mockResolvedValueOnce(new Response('missing', { status: 404 }))

    await expect(downloadImageSource('https://images.example/missing')).rejects.toThrow('HTTP 404')
    expect(createObjectURLMock).not.toHaveBeenCalled()
    expect(click).not.toHaveBeenCalled()
  })

  it('removes the link and releases its object URL even if triggering the download fails', async () => {
    vi.useFakeTimers()
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => { throw new Error('Download blocked') })

    await expect(downloadImageSource(PNG, 'failed')).rejects.toThrow('Download blocked')
    expect(document.querySelector('a[download="failed.png"]')).toBeNull()
    await vi.runOnlyPendingTimersAsync()
    expect(revokeObjectURLMock).toHaveBeenCalledWith('blob:download-test')
  })
})
