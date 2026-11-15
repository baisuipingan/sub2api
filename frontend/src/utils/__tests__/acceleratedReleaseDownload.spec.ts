// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  downloadReleaseInRanges,
  RELEASE_DOWNLOAD_CHUNK_SIZE,
  RELEASE_DOWNLOAD_CONCURRENCY,
  ReleaseDownloadError,
  type ReleaseDownloadWriter,
} from '../acceleratedReleaseDownload'

const packageUrl = 'https://download.example.test/releases/MiloCode.exe'
const etag = '"fixed-release-etag"'

function rangeResponse(start: number, end: number, total = 16, headerOverrides: Record<string, string> = {}, bytes?: Uint8Array) {
  return new Response(bytes ?? Uint8Array.from({ length: end - start + 1 }, (_, i) => start + i), {
    status: 206,
    headers: {
      'Content-Range': `bytes ${start}-${end}/${total}`,
      'Content-Length': String(end - start + 1),
      ETag: etag,
      ...headerOverrides,
    },
  })
}

function requestedRange(options?: RequestInit) {
  const range = (options?.headers as Headers).get('Range') ?? ''
  const match = /^bytes=(\d+)-(\d+)$/.exec(range)
  if (!match) throw new Error('Expected a single explicit byte range')
  return { start: Number(match[1]), end: Number(match[2]) }
}

function createWriter(size = 16) {
  const output = new Uint8Array(size)
  const writer: ReleaseDownloadWriter = {
    truncate: vi.fn(async () => undefined),
    write: vi.fn(async command => { output.set(command.data, command.position) }),
    close: vi.fn(async () => undefined),
    abort: vi.fn(async () => undefined),
  }
  return { writer, output }
}

afterEach(() => { vi.useRealTimers() })

describe('accelerated release downloads', () => {
  it('keeps the public defaults bounded to six workers and four MiB chunks', () => {
    expect(RELEASE_DOWNLOAD_CHUNK_SIZE).toBe(4 * 1024 * 1024)
    expect(RELEASE_DOWNLOAD_CONCURRENCY).toBe(6)
  })

  it('writes verified chunks at their offsets, pins If-Range, and commits only the complete file', async () => {
    const { writer, output } = createWriter()
    const progress = vi.fn()
    const fetcher = vi.fn(async (_url: RequestInfo | URL, options?: RequestInit) => {
      const { start, end } = requestedRange(options)
      return rangeResponse(start, end)
    })
    await downloadReleaseInRanges({ url: packageUrl, size: 16, writer, signal: new AbortController().signal, chunkSize: 4, fetcher, onProgress: progress })
    expect(output).toEqual(Uint8Array.from({ length: 16 }, (_, i) => i))
    expect(writer.truncate).toHaveBeenCalledWith(16)
    expect(writer.close).toHaveBeenCalledOnce()
    expect(writer.abort).not.toHaveBeenCalled()
    expect(progress).toHaveBeenLastCalledWith({ writtenBytes: 16, totalBytes: 16 })
    expect((fetcher.mock.calls[0][1]?.headers as Headers).has('If-Range')).toBe(false)
    for (const [, options] of fetcher.mock.calls.slice(1)) {
      expect((options?.headers as Headers).get('If-Range')).toBe(etag)
      expect(options).toMatchObject({ credentials: 'omit', redirect: 'error' })
    }
  })

  it('never runs more than six segment requests concurrently', async () => {
    const { writer } = createWriter(44)
    let running = 0
    let peak = 0
    const fetcher = vi.fn(async (_url: RequestInfo | URL, options?: RequestInit) => {
      const { start, end } = requestedRange(options)
      running += 1
      peak = Math.max(peak, running)
      await new Promise(resolve => setTimeout(resolve, 1))
      running -= 1
      return rangeResponse(start, end, 44)
    })
    await downloadReleaseInRanges({ url: packageUrl, size: 44, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })
    expect(peak).toBe(6)
    expect(fetcher).toHaveBeenCalledTimes(12)
  })

  it('retries transient network errors without writing duplicate bytes', async () => {
    vi.useFakeTimers()
    const { writer } = createWriter(4)
    const fetcher = vi.fn()
      .mockResolvedValueOnce(rangeResponse(0, 0, 4))
      .mockRejectedValueOnce(new TypeError('Temporary network error'))
      .mockResolvedValueOnce(rangeResponse(0, 3, 4))
    const task = downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })
    await vi.runAllTimersAsync()
    await task
    expect(fetcher).toHaveBeenCalledTimes(3)
    expect(writer.write).toHaveBeenCalledTimes(1)
  })

  it('stops retrying inaccessible CORS resources after three attempts and aborts the file', async () => {
    vi.useFakeTimers()
    const { writer } = createWriter(4)
    const fetcher = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'))
    const task = downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })
    const assertion = expect(task).rejects.toThrow('Failed to fetch')
    await vi.runAllTimersAsync()
    await assertion
    expect(fetcher).toHaveBeenCalledTimes(3)
    expect(writer.abort).toHaveBeenCalledOnce()
    expect(writer.close).not.toHaveBeenCalled()
  })

  it('retries temporary server failures before writing a verified range', async () => {
    vi.useFakeTimers()
    const { writer } = createWriter(4)
    const fetcher = vi.fn()
      .mockResolvedValueOnce(rangeResponse(0, 0, 4))
      .mockResolvedValueOnce(new Response('temporarily unavailable', { status: 503 }))
      .mockResolvedValueOnce(rangeResponse(0, 3, 4))
    const task = downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })
    await vi.runAllTimersAsync()
    await task
    expect(fetcher).toHaveBeenCalledTimes(3)
    expect(writer.write).toHaveBeenCalledOnce()
    expect(writer.close).toHaveBeenCalledOnce()
  })

  it('times out stalled requests and stops after three attempts', async () => {
    vi.useFakeTimers()
    const { writer } = createWriter(4)
    const fetcher = vi.fn((_url: RequestInfo | URL, options?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      options?.signal?.addEventListener('abort', () => reject(options.signal?.reason), { once: true })
    }))
    const task = downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })
    const assertion = expect(task).rejects.toMatchObject({ name: 'TimeoutError' })
    await vi.runAllTimersAsync()
    await assertion
    expect(fetcher).toHaveBeenCalledTimes(3)
    expect(writer.abort).toHaveBeenCalledOnce()
    expect(writer.write).not.toHaveBeenCalled()
  })

  it.each([
    ['incorrect range', { 'Content-Range': 'bytes 1-4/4' }],
    ['incorrect total', { 'Content-Range': 'bytes 0-3/8' }],
    ['incorrect length', { 'Content-Length': '3' }],
    ['missing content range', { 'Content-Range': '' }],
    ['missing etag', { ETag: '' }],
    ['weak etag', { ETag: 'W/"weak"' }],
    ['unexpected encoding', { 'Content-Encoding': 'gzip' }],
  ])('rejects %s before writing it', async (_name, headers) => {
    const { writer } = createWriter(4)
    const fetcher = vi.fn(async (_url: RequestInfo | URL, options?: RequestInit) => {
      const chunk = requestedRange(options)
      return rangeResponse(chunk.start, chunk.end, 4, headers)
    })
    await expect(downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })).rejects.toBeInstanceOf(ReleaseDownloadError)
    expect(writer.write).not.toHaveBeenCalled()
    expect(writer.abort).toHaveBeenCalledOnce()
    expect(fetcher).toHaveBeenCalledOnce()
  })

  it('rejects a server that ignores Range and returns the whole installer', async () => {
    const { writer } = createWriter(4)
    const fetcher = vi.fn().mockResolvedValue(new Response(new Uint8Array(20), { status: 200 }))
    await expect(downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })).rejects.toThrow('Expected HTTP 206')
    expect(writer.write).not.toHaveBeenCalled()
    expect(writer.abort).toHaveBeenCalledOnce()
  })

  it.each([2, 5])('rejects a body of %i bytes for a four-byte range', async byteCount => {
    const { writer } = createWriter(4)
    const fetcher = vi.fn(async (_url: RequestInfo | URL, options?: RequestInit) => {
      const chunk = requestedRange(options)
      const bytes = chunk.start === 0 && chunk.end === 0 ? new Uint8Array([0]) : new Uint8Array(byteCount)
      return rangeResponse(chunk.start, chunk.end, 4, {}, bytes)
    })
    await expect(downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })).rejects.toBeInstanceOf(ReleaseDownloadError)
    expect(writer.write).not.toHaveBeenCalled()
    expect(writer.abort).toHaveBeenCalledOnce()
  })

  it('refuses to commit chunks from a changed release', async () => {
    const { writer } = createWriter(8)
    const fetcher = vi.fn()
      .mockResolvedValueOnce(rangeResponse(0, 0, 8))
      .mockResolvedValueOnce(rangeResponse(0, 3, 8))
      .mockResolvedValueOnce(rangeResponse(4, 7, 8, { ETag: '"different-release"' }))
    await expect(downloadReleaseInRanges({ url: packageUrl, size: 8, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })).rejects.toThrow('file changed')
    expect(writer.write).toHaveBeenCalledTimes(1)
    expect(writer.close).not.toHaveBeenCalled()
    expect(writer.abort).toHaveBeenCalledOnce()
  })

  it('aborts network work and the incomplete file when the user cancels', async () => {
    const { writer } = createWriter(8)
    const controller = new AbortController()
    const fetcher = vi.fn((_url: RequestInfo | URL, options?: RequestInit) => {
      const { start, end } = requestedRange(options)
      if (start === 0) return Promise.resolve(rangeResponse(start, end, 8))
      return new Promise<Response>((_resolve, reject) => {
        options?.signal?.addEventListener('abort', () => reject(options.signal?.reason), { once: true })
        controller.abort(new DOMException('Cancelled', 'AbortError'))
      })
    })
    await expect(downloadReleaseInRanges({ url: packageUrl, size: 8, writer, signal: controller.signal, chunkSize: 4, fetcher })).rejects.toMatchObject({ name: 'AbortError' })
    expect(writer.write).toHaveBeenCalledTimes(0)
    expect(writer.close).not.toHaveBeenCalled()
    expect(writer.abort).toHaveBeenCalledOnce()
  })

  it('rolls back the file when a disk write fails', async () => {
    const { writer } = createWriter(4)
    vi.mocked(writer.write).mockRejectedValue(new Error('Disk full'))
    const fetcher = vi.fn(async (_url: RequestInfo | URL, options?: RequestInit) => {
      const chunk = requestedRange(options)
      return rangeResponse(chunk.start, chunk.end, 4)
    })
    await expect(downloadReleaseInRanges({ url: packageUrl, size: 4, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })).rejects.toThrow('Disk full')
    expect(writer.abort).toHaveBeenCalledOnce()
    expect(writer.close).not.toHaveBeenCalled()
  })

  it('cancels sibling requests before rolling back after a segment fails validation', async () => {
    const { writer } = createWriter(12)
    let siblingSignal: AbortSignal | null | undefined
    const fetcher = vi.fn((_url: RequestInfo | URL, options?: RequestInit) => {
      const { start, end } = requestedRange(options)
      if (start === 0) return Promise.resolve(rangeResponse(start, end, 12))
      if (start === 4) return Promise.resolve(rangeResponse(start, end, 12, { ETag: '"changed"' }))
      siblingSignal = options?.signal
      return new Promise<Response>((_resolve, reject) => {
        options?.signal?.addEventListener('abort', () => reject(options.signal?.reason), { once: true })
      })
    })
    await expect(downloadReleaseInRanges({ url: packageUrl, size: 12, writer, signal: new AbortController().signal, chunkSize: 4, fetcher })).rejects.toThrow('file changed')
    expect(siblingSignal?.aborted).toBe(true)
    expect(writer.write).toHaveBeenCalledOnce()
    expect(writer.abort).toHaveBeenCalledOnce()
    expect(writer.close).not.toHaveBeenCalled()
  })
})
