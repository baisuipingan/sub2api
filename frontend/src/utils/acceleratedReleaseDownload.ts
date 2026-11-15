export const RELEASE_DOWNLOAD_CHUNK_SIZE = 4 * 1024 * 1024
export const RELEASE_DOWNLOAD_CONCURRENCY = 6
const MAX_ATTEMPTS = 3
const RETRY_DELAY_MS = 250
const REQUEST_TIMEOUT_MS = 60_000

export interface ReleaseDownloadWriter {
  truncate(size: number): Promise<void>
  write(data: { type: 'write'; position: number; data: Uint8Array }): Promise<void>
  close(): Promise<void>
  abort(reason?: unknown): Promise<void>
}

export interface ReleaseDownloadProgress {
  writtenBytes: number
  totalBytes: number
}

export interface AcceleratedReleaseDownloadOptions {
  url: string
  size: number
  writer: ReleaseDownloadWriter
  signal: AbortSignal
  onProgress?: (progress: ReleaseDownloadProgress) => void
  fetcher?: typeof fetch
  chunkSize?: number
  concurrency?: number
}

export class ReleaseDownloadError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'ReleaseDownloadError'
  }
}

interface RangeChunk {
  start: number
  end: number
}

function abortError(): DOMException {
  return new DOMException('The download was cancelled.', 'AbortError')
}

function assertNotAborted(signal: AbortSignal): void {
  if (signal.aborted) throw signal.reason ?? abortError()
}

function expectedChunkSize(chunk: RangeChunk): number {
  return chunk.end - chunk.start + 1
}

function parseContentRange(value: string | null): { start: number; end: number; total: number } | null {
  if (!value) return null
  const match = /^bytes (\d+)-(\d+)\/(\d+)$/i.exec(value.trim())
  if (!match) return null
  const [start, end, total] = match.slice(1).map(Number)
  if (![start, end, total].every(Number.isSafeInteger)) return null
  return { start, end, total }
}

function validateHeaders(response: Response, chunk: RangeChunk, totalSize: number, etag?: string): string {
  if (response.status !== 206) {
    throw new ReleaseDownloadError(`Expected HTTP 206, received ${response.status}.`)
  }

  const contentRange = parseContentRange(response.headers.get('Content-Range'))
  if (
    !contentRange ||
    contentRange.start !== chunk.start ||
    contentRange.end !== chunk.end ||
    contentRange.total !== totalSize
  ) {
    throw new ReleaseDownloadError('The server returned an unexpected Content-Range.')
  }

  const expectedSize = expectedChunkSize(chunk)
  const contentLength = response.headers.get('Content-Length')
  if (contentLength === null || !/^\d+$/.test(contentLength) || Number(contentLength) !== expectedSize) {
    throw new ReleaseDownloadError('The server returned an unexpected chunk size.')
  }

  const responseEtag = response.headers.get('ETag')
  if (!responseEtag || !/^"[\x21\x23-\x7E\x80-\xFF]*"$/.test(responseEtag)) {
    throw new ReleaseDownloadError('The server did not provide a strong ETag for this download.')
  }
  if (etag !== undefined && responseEtag !== etag) {
    throw new ReleaseDownloadError('The file changed while it was being downloaded.')
  }

  const contentEncoding = response.headers.get('Content-Encoding')
  if (contentEncoding && contentEncoding.toLowerCase() !== 'identity') {
    throw new ReleaseDownloadError('The server encoded a byte-range response unexpectedly.')
  }

  return responseEtag
}

function waitBeforeRetry(signal: AbortSignal, attempt: number): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort)
      resolve()
    }, RETRY_DELAY_MS * attempt)
    const onAbort = () => {
      clearTimeout(timer)
      signal.removeEventListener('abort', onAbort)
      reject(signal.reason ?? abortError())
    }
    signal.addEventListener('abort', onAbort, { once: true })
  })
}

async function discardResponse(response: Response): Promise<void> {
  try {
    await response.body?.cancel()
  } catch {
    // A failed or already-consumed response body needs no further handling.
  }
}

async function readExactChunk(response: Response, size: number, signal: AbortSignal): Promise<Uint8Array> {
  if (!response.body) throw new ReleaseDownloadError('The downloaded chunk had no body.')
  const reader = response.body.getReader()
  const bytes = new Uint8Array(size)
  const cancelReader = () => { void reader.cancel(signal.reason).catch(() => undefined) }
  signal.addEventListener('abort', cancelReader, { once: true })
  let offset = 0
  try {
    while (true) {
      assertNotAborted(signal)
      const { value, done } = await reader.read()
      assertNotAborted(signal)
      if (done) break
      if (offset + value.byteLength > size) {
        throw new ReleaseDownloadError('The downloaded chunk exceeded its byte range.')
      }
      bytes.set(value, offset)
      offset += value.byteLength
    }
    if (offset !== size) throw new ReleaseDownloadError('The downloaded chunk length did not match its byte range.')
    return bytes
  } catch (error) {
    await reader.cancel().catch(() => undefined)
    throw error
  } finally {
    signal.removeEventListener('abort', cancelReader)
    reader.releaseLock()
  }
}

async function fetchRange(
  fetcher: typeof fetch,
  url: string,
  chunk: RangeChunk,
  totalSize: number,
  signal: AbortSignal,
  etag?: string,
): Promise<{ bytes: Uint8Array; etag: string }> {
  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt += 1) {
    assertNotAborted(signal)
    const requestController = new AbortController()
    const abortRequest = () => requestController.abort(signal.reason ?? abortError())
    signal.addEventListener('abort', abortRequest, { once: true })
    const timeout = setTimeout(() => requestController.abort(new DOMException('The download request timed out.', 'TimeoutError')), REQUEST_TIMEOUT_MS)
    try {
      const headers = new Headers({ Range: `bytes=${chunk.start}-${chunk.end}` })
      if (etag) headers.set('If-Range', etag)
      const response = await fetcher(url, {
        method: 'GET',
        headers,
        signal: requestController.signal,
        credentials: 'omit',
        redirect: 'error',
      })

      if ([408, 429].includes(response.status) || response.status >= 500) {
        await discardResponse(response)
        throw new Error(`The download server returned HTTP ${response.status}.`)
      }

      let responseEtag: string
      try {
        responseEtag = validateHeaders(response, chunk, totalSize, etag)
      } catch (error) {
        await discardResponse(response)
        throw error
      }
      const bytes = await readExactChunk(response, expectedChunkSize(chunk), requestController.signal)
      return { bytes, etag: responseEtag }
    } catch (error) {
      if (signal.aborted) throw signal.reason ?? abortError()
      if (error instanceof ReleaseDownloadError || attempt === MAX_ATTEMPTS) throw error
    } finally {
      clearTimeout(timeout)
      signal.removeEventListener('abort', abortRequest)
    }
    await waitBeforeRetry(signal, attempt)
  }

  throw new ReleaseDownloadError('The download could not be completed.')
}

function chunksFor(size: number, chunkSize: number): RangeChunk[] {
  const chunks: RangeChunk[] = []
  for (let start = 0; start < size; start += chunkSize) {
    chunks.push({ start, end: Math.min(start + chunkSize, size) - 1 })
  }
  return chunks
}

/** Downloads verified ranges directly to a file writer without assembling the package in memory. */
export async function downloadReleaseInRanges({
  url,
  size,
  writer,
  signal,
  onProgress,
  fetcher = fetch,
  chunkSize = RELEASE_DOWNLOAD_CHUNK_SIZE,
  concurrency = RELEASE_DOWNLOAD_CONCURRENCY,
}: AcceleratedReleaseDownloadOptions): Promise<void> {
  const controller = new AbortController()
  const abortFromCaller = () => controller.abort(signal.reason ?? abortError())
  signal.addEventListener('abort', abortFromCaller, { once: true })
  if (signal.aborted) abortFromCaller()

  let didClose = false
  try {
    if (!Number.isSafeInteger(size) || size <= 0) throw new ReleaseDownloadError('Invalid package size.')
    if (!Number.isSafeInteger(chunkSize) || chunkSize <= 0 || chunkSize > RELEASE_DOWNLOAD_CHUNK_SIZE) throw new ReleaseDownloadError('Invalid chunk size.')
    if (!Number.isSafeInteger(concurrency) || concurrency <= 0 || concurrency > RELEASE_DOWNLOAD_CONCURRENCY) throw new ReleaseDownloadError('Invalid download concurrency.')
    const chunks = chunksFor(size, chunkSize)
    assertNotAborted(controller.signal)
    await writer.truncate(size)
    // A one-byte probe authenticates the immutable object before any segment is written,
    // while keeping the full first chunk eligible for the worker pool below.
    const probe = await fetchRange(fetcher, url, { start: 0, end: 0 }, size, controller.signal)
    assertNotAborted(controller.signal)
    let writtenBytes = 0
    onProgress?.({ writtenBytes, totalBytes: size })

    let nextChunk = 0
    const worker = async () => {
      while (true) {
        assertNotAborted(controller.signal)
        const chunkIndex = nextChunk
        nextChunk += 1
        if (chunkIndex >= chunks.length) return

        const chunk = chunks[chunkIndex]
        const result = await fetchRange(fetcher, url, chunk, size, controller.signal, probe.etag)
        assertNotAborted(controller.signal)
        await writer.write({ type: 'write', position: chunk.start, data: result.bytes })
        writtenBytes += result.bytes.byteLength
        onProgress?.({ writtenBytes, totalBytes: size })
      }
    }

    const workers = Array.from({ length: Math.min(concurrency, chunks.length) }, () => worker().catch(error => {
      // Stop sibling fetches before rolling back the writer, so no task can write to an aborted file.
      controller.abort(error)
      throw error
    }))
    const results = await Promise.allSettled(workers)
    const failedWorker = results.find((result): result is PromiseRejectedResult => result.status === 'rejected')
    if (failedWorker) throw failedWorker.reason
    assertNotAborted(controller.signal)
    if (writtenBytes !== size) throw new ReleaseDownloadError('The completed file size did not match the published package size.')
    await writer.close()
    didClose = true
  } catch (error) {
    controller.abort(error)
    if (!didClose) {
      try {
        await writer.abort(error)
      } catch {
        // The browser can reject abort after the save stream has already closed.
      }
    }
    throw error
  } finally {
    signal.removeEventListener('abort', abortFromCaller)
  }
}
