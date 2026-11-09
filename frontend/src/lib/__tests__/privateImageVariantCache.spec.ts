import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AuthorizedImageVariant, PrivateImageVariantOptions } from '../privateImageVariantCache'
import type { PrivateVariantRecord } from '../privateImageVariantStore'

const state = vi.hoisted(() => ({ records: new Map<string, PrivateVariantRecord>(), readsFail: false, writesFail: false, writes: vi.fn(), deletes: vi.fn() }))
vi.mock('../privateImageVariantStore', () => ({
  PRIVATE_VARIANT_TTL_MS: 7 * 24 * 60 * 60 * 1000,
  readPrivateVariant: vi.fn(async (key: string, now: number) => {
    if (state.readsFail) throw new Error('Unavailable IndexedDB')
    const value = state.records.get(key)
    if (value && value.expiresAt <= now) { state.records.delete(key); return undefined }
    return value
  }),
  writePrivateVariant: vi.fn(async (record: PrivateVariantRecord, _now: number, current: () => boolean) => {
    state.writes(record)
    if (state.writesFail) throw new Error('QuotaExceededError')
    if (current()) state.records.set(record.key, record)
  }),
  deletePrivateVariantRecords: vi.fn(async (viewer: number, owner: number, ids: string[], variants: string[]) => {
    state.deletes()
    for (const [key, value] of state.records) if (value.viewerId === viewer && value.ownerId === owner && ids.includes(value.imageId) && variants.includes(value.variant)) state.records.delete(key)
  }),
  clearPrivateVariantRecords: vi.fn(async (viewer: number, owner?: number) => {
    for (const [key, value] of state.records) if (value.viewerId === viewer && (owner === undefined || value.ownerId === owner)) state.records.delete(key)
  }),
}))

const bytes = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82])
const asset: AuthorizedImageVariant = { url: 'https://private.test/signed?signature=secret', expires_at: '2030-01-01T00:00:00Z', bytes: bytes.length, mime: 'image/png', width: 100, height: 100, cache_version: 'digest-v1' }
const makeBlob = () => new Blob([bytes], { type: asset.mime })
function response(blob = makeBlob()) { return { ok: true, headers: new Headers(), blob: async () => blob } as Response }
function options(overrides: Partial<PrivateImageVariantOptions> = {}): PrivateImageVariantOptions {
  return { viewerId: 1, ownerId: 2, imageId: 'image-one', variant: 'thumbnail', authorize: vi.fn(async () => ({ ...asset })), isCurrent: () => true, ...overrides }
}
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(done => { resolve = done }); return { promise, resolve } }

beforeEach(() => { vi.resetModules(); state.records.clear(); state.readsFail = false; state.writesFail = false; state.writes.mockClear(); state.deletes.mockClear(); vi.stubGlobal('fetch', vi.fn(async () => response())) })
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); vi.useRealTimers() })

describe('private image variant caching', () => {
  it('authorizes every read, persists bytes instead of credentials, and reuses bytes across module reloads', async () => {
    let cache = await import('../privateImageVariantCache')
    const opts = options()
    const first = await cache.loadPrivateImageVariant(opts)
    expect(first.url).toMatch(/^data:image\/png;base64,/)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith(asset.url, expect.objectContaining({ cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer' }))
    expect(state.records.size).toBe(1)
    expect(JSON.stringify([...state.records.values()])).not.toContain('signature')
    vi.resetModules()
    cache = await import('../privateImageVariantCache')
    const second = await cache.loadPrivateImageVariant(opts)
    expect(second.url).toBe(first.url)
    expect(opts.authorize).toHaveBeenCalledTimes(2)
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('caches 2560px ticket previews only when explicitly allowed, keeping gallery limits unchanged', async () => {
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    const authorize = vi.fn(async () => ({ ...asset, width: 2560, height: 1440 }))
    const gallery = options({ variant: 'preview', authorize })
    expect((await loadPrivateImageVariant(gallery)).cached).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
    const ticket = options({ imageId: 'ticket:1:image-one', variant: 'preview', authorize, previewMaxDimension: 2560 })
    expect((await loadPrivateImageVariant(ticket)).cached).toBe(true)
    expect((await loadPrivateImageVariant(ticket)).cached).toBe(true)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(authorize).toHaveBeenCalledTimes(3)
  })

  it('separates viewers, owners, variants and content versions', async () => {
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    await loadPrivateImageVariant(options())
    await loadPrivateImageVariant(options({ viewerId: 3 }))
    await loadPrivateImageVariant(options({ ownerId: 4 }))
    await loadPrivateImageVariant(options({ variant: 'preview' }))
    await loadPrivateImageVariant(options({ authorize: async () => ({ ...asset, cache_version: 'digest-v2' }) }))
    expect(fetch).toHaveBeenCalledTimes(5)
    expect(state.records.size).toBe(5)
  })

  it('does not serve cached private bytes when authorization fails', async () => {
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    await loadPrivateImageVariant(options())
    await expect(loadPrivateImageVariant(options({ authorize: async () => { throw new Error('Forbidden') } }))).rejects.toThrow('Forbidden')
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('falls back to a signed URL if CORS or network loading prevents byte caching', async () => {
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('CORS denied'))
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    const result = await loadPrivateImageVariant(options())
    expect(result.url).toBe(asset.url)
    expect(result.cached).toBe(false)
    expect(state.records.size).toBe(0)
  })

  it('preserves display when IndexedDB is blocked or out of space', async () => {
    state.readsFail = true; state.writesFail = true
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    const result = await loadPrivateImageVariant(options())
    expect(result.url).toMatch(/^data:image\/png;base64,/)
    expect(state.records.size).toBe(0)
  })

  it('does not cache non-raster data or mismatched byte counts', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(response(new Blob(['<svg></svg>'], { type: 'image/png' })))
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    expect((await loadPrivateImageVariant(options())).url).toBe(asset.url)
    expect(state.records.size).toBe(0)
    const unsupported = options({ authorize: async () => ({ ...asset, mime: 'image/svg+xml' }) })
    expect((await loadPrivateImageVariant(unsupported)).url).toBe(asset.url)
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('discards corrupt or expired cache records and downloads fresh bytes', async () => {
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    await loadPrivateImageVariant(options())
    const [key, stored] = [...state.records.entries()][0]!
    state.records.set(key, { ...stored, blob: new Blob(['invalid'], { type: 'image/png' }) })
    await loadPrivateImageVariant(options())
    expect(state.deletes).toHaveBeenCalledTimes(1)
    state.records.set(key, { ...state.records.get(key)!, expiresAt: Date.now() - 1000 })
    await loadPrivateImageVariant(options())
    expect(fetch).toHaveBeenCalledTimes(3)
  })

  it('shares downloads without attaching the first consumer cancellation to the second', async () => {
    const waiting = deferred<Response>()
    vi.mocked(fetch).mockReturnValue(waiting.promise)
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    const controller = new AbortController()
    const first = loadPrivateImageVariant(options({ signal: controller.signal }))
    const rejected = expect(first).rejects.toMatchObject({ name: 'AbortError' })
    const second = loadPrivateImageVariant(options())
    await new Promise(resolve => setTimeout(resolve, 0))
    controller.abort()
    waiting.resolve(response())
    await rejected
    expect((await second).url).toMatch(/^data:image\/png;base64,/)
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('ignores cancelled account responses and never writes their private bytes', async () => {
    const waiting = deferred<Response>()
    vi.mocked(fetch).mockReturnValue(waiting.promise)
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    let current = true
    const result = loadPrivateImageVariant(options({ isCurrent: () => current }))
    const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    await new Promise(resolve => setTimeout(resolve, 0))
    current = false; waiting.resolve(response())
    await rejected
    expect(state.records.size).toBe(0)
  })

  it('deletes only the selected viewer/owner/variant and stops an old response from restoring it', async () => {
    const { loadPrivateImageVariant, deletePrivateImageVariants, clearPrivateImageVariants } = await import('../privateImageVariantCache')
    await loadPrivateImageVariant(options())
    await loadPrivateImageVariant(options({ variant: 'preview' }))
    await loadPrivateImageVariant(options({ viewerId: 3 }))
    await deletePrivateImageVariants(1, 2, ['image-one'], ['preview'])
    expect([...state.records.values()].map(r => [r.viewerId, r.variant])).toEqual([[1, 'thumbnail'], [3, 'thumbnail']])
    const waiting = deferred<Response>()
    vi.mocked(fetch).mockReturnValueOnce(waiting.promise)
    const pending = loadPrivateImageVariant(options({ imageId: 'deleted-during-download' }))
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    await new Promise(resolve => setTimeout(resolve, 0))
    await deletePrivateImageVariants(1, 2, ['deleted-during-download'])
    waiting.resolve(response())
    await rejected
    await clearPrivateImageVariants(1)
    expect([...state.records.values()].every(r => r.viewerId === 3)).toBe(true)
  })

  it('temporarily bypasses repeated CORS fetch probes without skipping authorization or cached reads', async () => {
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    vi.mocked(fetch).mockRejectedValueOnce(new TypeError('CORS denied'))
    const opts = options()
    expect((await loadPrivateImageVariant(opts)).cached).toBe(false)
    expect((await loadPrivateImageVariant(options({ imageId: 'second-image' }))).cached).toBe(false)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(opts.authorize).toHaveBeenCalledTimes(1)
    const later = Date.now() + 60_001
    vi.spyOn(Date, 'now').mockReturnValue(later)
    expect((await loadPrivateImageVariant(options({ imageId: 'third-image' }))).cached).toBe(true)
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('cannot revive a removed response when bounded tombstones are evicted', async () => {
    const waiting = deferred<Response>()
    vi.mocked(fetch).mockReturnValue(waiting.promise)
    const { loadPrivateImageVariant, deletePrivateImageVariants } = await import('../privateImageVariantCache')
    const pending = loadPrivateImageVariant(options())
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    await new Promise(resolve => setTimeout(resolve, 0))
    await deletePrivateImageVariants(1, 2, ['image-one', ...Array.from({ length: 1001 }, (_, i) => `other-${i}`)])
    waiting.resolve(response())
    await rejected
    expect(state.records.size).toBe(0)
  })

  it('does not return a stale direct URL after deletion during a failed download or authorization', async () => {
    const waiting = deferred<Response>()
    const failed = waiting.promise.then(() => { throw new Error('network failed') })
    vi.mocked(fetch).mockReturnValue(failed)
    const { loadPrivateImageVariant, deletePrivateImageVariants } = await import('../privateImageVariantCache')
    const pending = loadPrivateImageVariant(options())
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
    await new Promise(resolve => setTimeout(resolve, 0))
    await deletePrivateImageVariants(1, 2, ['image-one'])
    waiting.resolve(response())
    await rejected
    const auth = deferred<AuthorizedImageVariant>()
    const next = loadPrivateImageVariant(options({ authorize: () => auth.promise }))
    const nextRejected = expect(next).rejects.toMatchObject({ name: 'AbortError' })
    await deletePrivateImageVariants(1, 2, ['image-one'])
    auth.resolve({ ...asset, mime: 'image/svg+xml' })
    await nextRejected
  })

  it('cancels oversized response streams before buffering an unbounded image', async () => {
    const cancel = vi.fn(async () => undefined)
    const releaseLock = vi.fn()
    const read = vi.fn().mockResolvedValueOnce({ done: false, value: new Uint8Array(4 * 1024 * 1024 + 1) })
    vi.mocked(fetch).mockResolvedValue({ ok: true, headers: new Headers(), body: { getReader: () => ({ read, cancel, releaseLock }) } } as unknown as Response)
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    expect((await loadPrivateImageVariant(options())).url).toBe(asset.url)
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(releaseLock).toHaveBeenCalledTimes(1)
    expect(state.records.size).toBe(0)
  })

  it('times out a slow cache download and leaves normal img loading available', async () => {
    vi.useFakeTimers()
    vi.mocked(fetch).mockImplementation((_url, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(new DOMException('Timed out', 'AbortError')), { once: true })
    }))
    const { loadPrivateImageVariant } = await import('../privateImageVariantCache')
    const pending = loadPrivateImageVariant(options())
    await vi.advanceTimersByTimeAsync(12_001)
    expect((await pending).url).toBe(asset.url)
    expect(state.records.size).toBe(0)
  })

})
