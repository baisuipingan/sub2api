import {
  clearPrivateVariantRecords,
  deletePrivateVariantRecords,
  PRIVATE_VARIANT_TTL_MS,
  readPrivateVariant,
  writePrivateVariant,
  type PrivateImageVariant,
  type PrivateVariantRecord,
} from './privateImageVariantStore'

export type { PrivateImageVariant } from './privateImageVariantStore'
export interface AuthorizedImageVariant {
  url: string
  expires_at: string
  bytes: number
  mime: string
  width: number
  height: number
  cache_version?: string
}
export interface PrivateImageVariantOptions {
  viewerId: number
  ownerId: number
  imageId: string
  variant: PrivateImageVariant
  authorize: () => Promise<AuthorizedImageVariant>
  isCurrent: () => boolean
  signal?: AbortSignal
  /** Ticket screenshots can retain legible text at 2560px; galleries keep 1600px. */
  previewMaxDimension?: 1600 | 2560
}
export interface CachedPrivateImageVariant extends AuthorizedImageVariant { cached: boolean }

const allowedMimes = new Set(['image/png', 'image/jpeg', 'image/webp'])
const inflight = new Map<string, Promise<Blob>>()
const generations = new Map<string, number>()
const MAX_GENERATIONS = 1000
let invalidationEpoch = 0
const networkBypassOrigins = new Map<string, number>()
const pendingDeletions = new Map<string, Promise<void>>()
const scopeGenerations = new Map<number, number>()
const pendingClears = new Map<number, Promise<void>>()

function identity(options: Pick<PrivateImageVariantOptions, 'viewerId' | 'ownerId' | 'imageId' | 'variant'>) {
  return JSON.stringify([options.viewerId, options.ownerId, options.imageId, options.variant])
}
function generation(key: string) { return generations.get(key) ?? 0 }
function invalidate(key: string) {
  generations.delete(key)
  generations.set(key, Date.now() + Math.random())
  while (generations.size > MAX_GENERATIONS) {
    generations.delete(generations.keys().next().value!)
    // Removing a tombstone must never make a request with the old default
    // generation current again. Rare large invalidations cancel older work.
    invalidationEpoch++
  }
}
function checkCurrent(options: PrivateImageVariantOptions) {
  if (options.signal?.aborted || !options.isCurrent()) throw new DOMException('Image request is no longer current', 'AbortError')
}
function awaitConsumer<T>(promise: Promise<T>, options: PrivateImageVariantOptions): Promise<T> {
  if (!options.signal) return promise
  const signal = options.signal
  return new Promise((resolve, reject) => {
    const abort = () => reject(new DOMException('Image request was cancelled', 'AbortError'))
    if (signal.aborted) { abort(); return }
    signal.addEventListener('abort', abort, { once: true })
    promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', abort)).catch(() => undefined)
  })
}
function variantLimit(variant: PrivateImageVariant) { return variant === 'thumbnail' ? 4 * 1024 * 1024 : 16 * 1024 * 1024 }
function validMetadata(asset: AuthorizedImageVariant, variant: PrivateImageVariant, previewMaxDimension: 1600 | 2560 = 1600) {
  const maximumDimension = variant === 'thumbnail' ? 720 : previewMaxDimension
  return allowedMimes.has(asset.mime) && Number.isSafeInteger(asset.bytes) && asset.bytes > 0 && asset.bytes <= variantLimit(variant)
    && Number.isInteger(asset.width) && Number.isInteger(asset.height) && asset.width > 0 && asset.height > 0
    && asset.width <= maximumDimension && asset.height <= maximumDimension
}
function readBlob(blob: Blob, method: 'readAsDataURL' | 'readAsArrayBuffer'): Promise<string | ArrayBuffer> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    const timer = setTimeout(() => { reader.abort(); reject(new Error('Image cache blob read timed out')) }, 2000)
    reader.onload = () => {
      clearTimeout(timer)
      if (typeof reader.result === 'string' || reader.result instanceof ArrayBuffer) resolve(reader.result)
      else reject(new Error('Image cache blob is unreadable'))
    }
    reader.onerror = () => { clearTimeout(timer); reject(reader.error ?? new Error('Image cache blob is unreadable')) }
    reader[method](blob)
  })
}
async function validBlob(blob: Blob, asset: AuthorizedImageVariant) {
  if (!blob || blob.size !== asset.bytes || blob.type !== asset.mime) return false
  const header = new Uint8Array(await readBlob(blob.slice(0, 16), 'readAsArrayBuffer') as ArrayBuffer)
  const ascii = (from: number, to: number) => String.fromCharCode(...header.subarray(from, to))
  if (asset.mime === 'image/png') return header.length >= 8 && header[0] === 137 && ascii(1, 4) === 'PNG' && header[4] === 13 && header[5] === 10 && header[6] === 26 && header[7] === 10
  if (asset.mime === 'image/jpeg') return header.length >= 3 && header[0] === 255 && header[1] === 216 && header[2] === 255
  if (asset.mime === 'image/webp') return ascii(0, 4) === 'RIFF' && ascii(8, 12) === 'WEBP'
  return false
}
function originOf(url: string) { try { return new URL(url).origin } catch { return '' } }
function bypassNetworkCache(url: string) {
  const origin = originOf(url)
  const until = networkBypassOrigins.get(origin)
  if (!until) return false
  if (until <= Date.now()) { networkBypassOrigins.delete(origin); return false }
  return true
}
function temporarilyBypassNetworkCache(url: string) {
  const origin = originOf(url)
  if (!origin) return
  networkBypassOrigins.delete(origin)
  networkBypassOrigins.set(origin, Date.now() + 60_000)
  while (networkBypassOrigins.size > 16) networkBypassOrigins.delete(networkBypassOrigins.keys().next().value!)
}
async function download(asset: AuthorizedImageVariant, variant: PrivateImageVariant) {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 12_000)
  try {
    let response: Response
    try { response = await fetch(asset.url, { cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer', signal: controller.signal }) }
    catch (error) {
      // Cross-origin img display can work when CORS does not allow fetch.
      // Avoid probing every thumbnail again; the next minute retries caching.
      if (error instanceof TypeError) temporarilyBypassNetworkCache(asset.url)
      throw error
    }
    if (!response.ok) throw new Error('Image download failed')
    const declaredLength = Number(response.headers.get('Content-Length'))
    if (declaredLength > variantLimit(variant)) throw new Error('Image is larger than cache limits')
    let blob: Blob
    if (response.body) {
      const reader = response.body.getReader()
      const chunks: Uint8Array[] = []
      let received = 0
      try {
        for (;;) {
          const part = await reader.read()
          if (part.done) break
          received += part.value.byteLength
          if (received > variantLimit(variant)) {
            await reader.cancel()
            throw new Error('Image is larger than cache limits')
          }
          chunks.push(part.value)
        }
      } finally { reader.releaseLock() }
      const mime = response.headers.get('Content-Type')?.split(';')[0]?.trim() || asset.mime
      blob = new Blob(chunks, { type: mime })
    } else blob = await response.blob()
    if (!blob.type) blob = new Blob([blob], { type: asset.mime })
    if (!await validBlob(blob, asset)) throw new Error('Image data is invalid')
    return blob
  } finally { clearTimeout(timer) }
}

/** Authorize on every read; only immutable image bytes are cached across refreshes. */
export async function loadPrivateImageVariant(options: PrivateImageVariantOptions): Promise<CachedPrivateImageVariant> {
  checkCurrent(options)
  if (!Number.isSafeInteger(options.viewerId) || options.viewerId <= 0 || !Number.isSafeInteger(options.ownerId) || options.ownerId <= 0
    || !options.imageId || !['thumbnail', 'preview'].includes(options.variant)) throw new Error('Invalid private image cache identity')
  const base = identity(options)
  const revision = generation(base)
  const scopeRevision = scopeGenerations.get(options.viewerId) ?? 0
  const epoch = invalidationEpoch
  const current = () => generation(base) === revision && (scopeGenerations.get(options.viewerId) ?? 0) === scopeRevision && invalidationEpoch === epoch && options.isCurrent() && !options.signal?.aborted
  const asset = await awaitConsumer(options.authorize(), options)
  checkCurrent(options)
  const direct = () => {
    checkCurrent(options)
    if (!current()) throw new DOMException('Image cache entry was removed', 'AbortError')
    return { ...asset, cached: false }
  }
  if (!validMetadata(asset, options.variant, options.previewMaxDimension)) return direct()
  const version = asset.cache_version || 'v1'
  const key = JSON.stringify(['schema-v1', options.viewerId, options.ownerId, options.imageId, options.variant, version])
  await pendingClears.get(options.viewerId)
  await pendingDeletions.get(base)
  checkCurrent(options)
  if (!current()) throw new DOMException('Image cache entry was removed', 'AbortError')
  let record: PrivateVariantRecord | undefined
  try { record = await readPrivateVariant(key, Date.now()) } catch { /* Browsing works without IndexedDB. */ }
  checkCurrent(options)
  if (!current()) throw new DOMException('Image cache entry was removed', 'AbortError')
  if (record) {
    try {
      if (record.key === key && record.viewerId === options.viewerId && record.ownerId === options.ownerId
        && record.imageId === options.imageId && record.variant === options.variant && record.version === version
        && record.bytes === asset.bytes && record.mime === asset.mime && await validBlob(record.blob, asset)) {
        const url = await readBlob(record.blob, 'readAsDataURL') as string
        checkCurrent(options)
        if (!current()) throw new DOMException('Image cache entry was removed', 'AbortError')
        return { ...asset, url, cached: true }
      }
    } catch (error) { if (error instanceof DOMException && error.name === 'AbortError') throw error }
    await deletePrivateVariantRecords(options.viewerId, options.ownerId, [options.imageId], [options.variant]).catch(() => undefined)
  }
  if (bypassNetworkCache(asset.url)) return direct()
  let pending = inflight.get(key)
  if (!pending) {
    // Never attach a consumer's AbortSignal to a shared fetch. A cancelled
    // thumbnail must not abort another visible component requesting the same ID.
    pending = download(asset, options.variant)
    inflight.set(key, pending)
    void pending.finally(() => { if (inflight.get(key) === pending) inflight.delete(key) }).catch(() => undefined)
  }
  let blob: Blob
  try { blob = await awaitConsumer(pending, options) }
  catch {
    checkCurrent(options)
    if (!current()) throw new DOMException('Image cache entry was removed', 'AbortError')
    return direct()
  }
  checkCurrent(options)
  if (!current()) throw new DOMException('Image cache entry was removed', 'AbortError')

  try {
    const url = await readBlob(blob, 'readAsDataURL') as string
    checkCurrent(options)
    if (!current()) throw new DOMException('Image cache entry was removed', 'AbortError')
    const now = Date.now()
    // Do not make displaying a downloaded image wait for quota/pruning or disk.
    void writePrivateVariant({ key, viewerId: options.viewerId, ownerId: options.ownerId, imageId: options.imageId,
      variant: options.variant, version, blob, bytes: blob.size, mime: blob.type,
      expiresAt: now + PRIVATE_VARIANT_TTL_MS, lastAccess: now }, now, current).catch(() => undefined)
    return { ...asset, url, cached: true }
  } catch (error) {
    checkCurrent(options)
    if (error instanceof DOMException && error.name === 'AbortError') throw error
    return direct()
  }
}

export async function deletePrivateImageVariants(viewerId: number, ownerId: number, imageIds: readonly string[], variants: readonly PrivateImageVariant[] = ['thumbnail', 'preview']): Promise<void> {
  const keys = imageIds.flatMap(imageId => variants.map(variant => identity({ viewerId, ownerId, imageId, variant })))
  for (const key of keys) invalidate(key)
  const previous = keys.map(key => pendingDeletions.get(key)).filter(Boolean)
  const pending = Promise.all(previous).then(() => deletePrivateVariantRecords(viewerId, ownerId, imageIds, variants)).catch(() => undefined)
  for (const key of keys) pendingDeletions.set(key, pending)
  await pending
  for (const key of keys) if (pendingDeletions.get(key) === pending) pendingDeletions.delete(key)
}

export async function clearPrivateImageVariants(viewerId: number, ownerId?: number): Promise<void> {
  scopeGenerations.set(viewerId, (scopeGenerations.get(viewerId) ?? 0) + 1)
  const pending = Promise.resolve(pendingClears.get(viewerId)).then(() => clearPrivateVariantRecords(viewerId, ownerId)).catch(() => undefined)
  pendingClears.set(viewerId, pending)
  await pending
  if (pendingClears.get(viewerId) === pending) pendingClears.delete(viewerId)
}
