import {
  CURRENT_THUMBNAIL_VERSION,
  getImage,
  getImageThumbnail,
  getStoredFreshImageThumbnail,
  putImage,
} from './db'
import { ensureGalleryIdentity, fetchGalleryAssetDataUrl, getGalleryAssetUrl } from './galleryApi'
import { captureGallerySession, isGallerySessionCurrent, mayReadGalleryCache } from './gallerySession'

type ImageThumbnail = {
  dataUrl: string
  width?: number
  height?: number
  thumbnailVersion?: number
  expiresAt?: number
}

const imageCache = new Map<string, string>()
const previewCache = new Map<string, { url: string; expiresAt?: number }>()
const cloudAssetIds = new Set<string>()
const thumbnailCache = new Map<string, ImageThumbnail>()
const thumbnailBackfillIds = new Map<string, 'visible' | 'background'>()
const thumbnailBackfillRunningIds = new Set<string>()
const thumbnailSubscribers = new Map<string, Set<(thumbnail: ImageThumbnail) => void>>()
let thumbnailBackfillScheduled = false
let cacheSession = captureGallerySession()

function ensureCacheSession() {
  if (isGallerySessionCurrent(cacheSession)) return
  imageCache.clear()
  previewCache.clear()
  thumbnailCache.clear()
  thumbnailBackfillIds.clear()
  thumbnailBackfillRunningIds.clear()
  thumbnailSubscribers.clear()
  cloudAssetIds.clear()
  cacheSession = captureGallerySession()
}

async function verifiedCacheSession() {
  ensureCacheSession()
  const session = captureGallerySession()
  if (!mayReadGalleryCache() && session.token) {
    try { await ensureGalleryIdentity(session.token) } catch { return null }
  }
  return isGallerySessionCurrent(session) && mayReadGalleryCache() ? session : null
}

const MAX_IMAGE_CACHE_ENTRIES = 8
const MAX_PREVIEW_CACHE_ENTRIES = 16
const MAX_THUMBNAIL_CACHE_ENTRIES = 80
const MAX_THUMBNAIL_BACKFILL_CONCURRENT = 4

export function getCachedImage(id: string): string | undefined {
  ensureCacheSession()
  if (!mayReadGalleryCache()) return undefined
  const dataUrl = imageCache.get(id)
  if (dataUrl) {
    imageCache.delete(id)
    imageCache.set(id, dataUrl)
  }
  return dataUrl
}

export function registerCloudAssetIds(ids: Iterable<string>) {
  ensureCacheSession()
  for (const id of ids) cloudAssetIds.add(id)
}

export function getCachedPreviewUrl(id: string): string | undefined {
  ensureCacheSession()
  if (!mayReadGalleryCache()) return undefined
  const preview = previewCache.get(id)
  if (!preview || preview.expiresAt && preview.expiresAt <= Date.now() + 60_000) {
    previewCache.delete(id)
    return undefined
  }
  previewCache.delete(id)
  previewCache.set(id, preview)
  return preview.url
}

function cachePreviewUrl(id: string, url: string, expiresAt?: number) {
  ensureCacheSession()
  previewCache.delete(id)
  previewCache.set(id, { url, expiresAt })
  while (previewCache.size > MAX_PREVIEW_CACHE_ENTRIES) {
    const oldest = previewCache.keys().next().value
    if (!oldest) break
    previewCache.delete(oldest)
  }
}

function createLocalPreview(dataUrl: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onerror = () => reject(new Error('Preview could not be decoded'))
    image.onload = () => {
      const scale = Math.min(1, 1600 / Math.max(image.naturalWidth, image.naturalHeight))
      const canvas = document.createElement('canvas')
      canvas.width = Math.max(1, Math.round(image.naturalWidth * scale))
      canvas.height = Math.max(1, Math.round(image.naturalHeight * scale))
      const context = canvas.getContext('2d')
      if (!context) { reject(new Error('Canvas is unavailable')); return }
      context.drawImage(image, 0, 0, canvas.width, canvas.height)
      resolve(canvas.toDataURL('image/png'))
    }
    image.src = dataUrl
  })
}

export function cacheImage(id: string, dataUrl: string) {
  ensureCacheSession()
  if (!mayReadGalleryCache()) return
  imageCache.delete(id)
  imageCache.set(id, dataUrl)
  while (imageCache.size > MAX_IMAGE_CACHE_ENTRIES) {
    const oldestKey = imageCache.keys().next().value
    if (oldestKey == null) break
    imageCache.delete(oldestKey)
  }
}

export function deleteCachedImage(id: string) {
  imageCache.delete(id)
  previewCache.delete(id)
}

function getCachedThumbnail(id: string) {
  ensureCacheSession()
  if (!mayReadGalleryCache()) return undefined
  const thumbnail = thumbnailCache.get(id)
  if (thumbnail?.expiresAt && thumbnail.expiresAt <= Date.now() + 60_000) {
    thumbnailCache.delete(id)
    return undefined
  }
  if (thumbnail?.thumbnailVersion === CURRENT_THUMBNAIL_VERSION) {
    thumbnailCache.delete(id)
    thumbnailCache.set(id, thumbnail)
    return thumbnail
  }
  if (thumbnail) thumbnailCache.delete(id)
  return undefined
}

export function cacheThumbnail(id: string, thumbnail: ImageThumbnail) {
  ensureCacheSession()
  if (thumbnail.thumbnailVersion !== CURRENT_THUMBNAIL_VERSION) return
  thumbnailCache.delete(id)
  thumbnailCache.set(id, thumbnail)
  while (thumbnailCache.size > MAX_THUMBNAIL_CACHE_ENTRIES) {
    const oldestKey = thumbnailCache.keys().next().value
    if (oldestKey == null) break
    thumbnailCache.delete(oldestKey)
  }
}

export function deleteImageCacheEntry(id: string) {
  ensureCacheSession()
  imageCache.delete(id)
  previewCache.delete(id)
  cloudAssetIds.delete(id)
  thumbnailCache.delete(id)
  thumbnailBackfillIds.delete(id)
  thumbnailBackfillRunningIds.delete(id)
  thumbnailSubscribers.delete(id)
}

export function clearImageCaches() {
  imageCache.clear()
  previewCache.clear()
  cloudAssetIds.clear()
  thumbnailCache.clear()
  thumbnailBackfillIds.clear()
  cacheSession = captureGallerySession()
}

export async function ensureImageCached(id: string): Promise<string | undefined> {
  const session = await verifiedCacheSession()
  if (!session) return undefined
  const cached = getCachedImage(id)
  if (cached) return cached
  const rec = await getImage(id, session.scope)
  if (!isGallerySessionCurrent(session)) return undefined
  if (rec) {
    cacheImage(id, rec.dataUrl)
    return rec.dataUrl
  }
  const dataUrl = await fetchGalleryAssetDataUrl(id, 'original').catch(() => undefined)
  if (!isGallerySessionCurrent(session)) return undefined
  if (!dataUrl) return undefined
  cacheImage(id, dataUrl)
  // A cloud miss is cached locally only after the bytes have been fetched successfully.
  await putImage({ id, dataUrl, source: 'generated' }, session.scope).catch(() => undefined)
  return isGallerySessionCurrent(session) ? dataUrl : undefined
}

export async function ensureImagePreviewUrl(id: string): Promise<string | undefined> {
  const session = await verifiedCacheSession()
  if (!session) return undefined
  const cached = getCachedPreviewUrl(id)
  if (cached) return cached
  if (cloudAssetIds.has(id)) {
    const remote = await getGalleryAssetUrl(id, 'preview').catch(() => undefined)
    if (!isGallerySessionCurrent(session)) return undefined
    if (remote) {
      cachePreviewUrl(id, remote.url, Date.parse(remote.expires_at))
      return remote.url
    }
  }
  const local = getCachedImage(id) ?? (await getImage(id, session.scope))?.dataUrl
  if (!isGallerySessionCurrent(session)) return undefined
  if (local) {
    const preview = await createLocalPreview(local).catch(() => undefined)
    if (!isGallerySessionCurrent(session)) return undefined
    if (preview) cachePreviewUrl(id, preview)
    return preview
  }
  const remote = await getGalleryAssetUrl(id, 'preview').catch(() => undefined)
  if (!isGallerySessionCurrent(session)) return undefined
  if (!remote) return undefined
  cloudAssetIds.add(id)
  cachePreviewUrl(id, remote.url, Date.parse(remote.expires_at))
  return remote.url
}

export async function ensureImageThumbnailCached(id: string): Promise<ImageThumbnail | undefined> {
  const session = await verifiedCacheSession()
  if (!session) return undefined
  const cached = getCachedThumbnail(id)
  if (cached) return cached

  const rec = await getStoredFreshImageThumbnail(id, session.scope)
  if (!isGallerySessionCurrent(session)) return undefined
  if (!rec?.thumbnailDataUrl) {
    if (cloudAssetIds.has(id)) {
      const remote = await getGalleryAssetUrl(id, 'thumbnail').catch(() => undefined)
      if (!isGallerySessionCurrent(session)) return undefined
      if (remote) {
        const thumbnail = {
          dataUrl: remote.url,
          width: remote.width,
          height: remote.height,
          thumbnailVersion: CURRENT_THUMBNAIL_VERSION,
          expiresAt: Date.parse(remote.expires_at),
        }
        cacheThumbnail(id, thumbnail)
        return thumbnail
      }
    }
    if (await getImage(id, session.scope)) {
      if (!isGallerySessionCurrent(session)) return undefined
      scheduleThumbnailBackfill([id], 'visible')
      return undefined
    }
    const remote = await getGalleryAssetUrl(id, 'thumbnail').catch(() => undefined)
    if (!isGallerySessionCurrent(session)) return undefined
    if (!remote) return undefined
    const thumbnail = {
      dataUrl: remote.url,
      width: remote.width,
      height: remote.height,
      thumbnailVersion: CURRENT_THUMBNAIL_VERSION,
      expiresAt: Date.parse(remote.expires_at),
    }
    cacheThumbnail(id, thumbnail)
    return thumbnail
  }

  const thumbnail = {
    dataUrl: rec.thumbnailDataUrl,
    width: rec.width,
    height: rec.height,
    thumbnailVersion: rec.thumbnailVersion,
  }
  cacheThumbnail(id, thumbnail)
  return thumbnail
}

export function subscribeImageThumbnail(id: string, callback: (thumbnail: ImageThumbnail) => void) {
  ensureCacheSession()
  let subscribers = thumbnailSubscribers.get(id)
  if (!subscribers) {
    subscribers = new Set()
    thumbnailSubscribers.set(id, subscribers)
  }
  subscribers.add(callback)
  return () => {
    subscribers?.delete(callback)
    if (subscribers?.size === 0) thumbnailSubscribers.delete(id)
  }
}

export function scheduleThumbnailBackfill(ids: Iterable<string>, priority: 'visible' | 'background' = 'background') {
  ensureCacheSession()
  for (const id of ids) {
    if (getCachedThumbnail(id) || thumbnailBackfillRunningIds.has(id)) continue
    const currentPriority = thumbnailBackfillIds.get(id)
    if (!currentPriority || priority === 'visible') thumbnailBackfillIds.set(id, priority)
  }
  scheduleThumbnailBackfillTick()
}

function scheduleThumbnailBackfillTick() {
  if (thumbnailBackfillScheduled || thumbnailBackfillIds.size === 0) return
  thumbnailBackfillScheduled = true

  const run = () => {
    thumbnailBackfillScheduled = false
    void processNextThumbnailBackfill()
  }

  if (typeof window !== 'undefined' && 'requestIdleCallback' in window) {
    window.requestIdleCallback(run, { timeout: 2_000 })
  } else {
    globalThis.setTimeout(run, 250)
  }
}

async function processNextThumbnailBackfill() {
  if (thumbnailBackfillRunningIds.size > 0) return

  const ids = await getNextThumbnailBackfillBatch()
  for (const id of ids) void startThumbnailBackfill(id)

  if (thumbnailBackfillIds.size > 0) scheduleThumbnailBackfillTick()
}

async function getNextThumbnailBackfillBatch() {
  const session = captureGallerySession()
  const candidates = getOrderedThumbnailBackfillIds().slice(0, MAX_THUMBNAIL_BACKFILL_CONCURRENT)
  if (candidates.length === 0) return []

  const sizes = await Promise.all(candidates.map(async (id) => {
    const image = await getImage(id, session.scope)
    return { width: image?.width, height: image?.height }
  }))
  if (!isGallerySessionCurrent(session)) return []
  const concurrency = getThumbnailConcurrencyForBatch(sizes)
  const selected = candidates.slice(0, concurrency)
  for (const id of selected) thumbnailBackfillIds.delete(id)
  return selected
}

function getOrderedThumbnailBackfillIds() {
  const visible: string[] = []
  const background: string[] = []
  for (const [id, priority] of thumbnailBackfillIds) {
    if (priority === 'visible') visible.push(id)
    else background.push(id)
  }
  return [...visible, ...background]
}

function getThumbnailConcurrencyForBatch(sizes: Array<{ width?: number; height?: number }>) {
  let maxMegapixels = 0
  for (const { width, height } of sizes) {
    if (!width || !height) return 1
    maxMegapixels = Math.max(maxMegapixels, (width * height) / 1_000_000)
  }
  if (maxMegapixels >= 8) return 1
  if (maxMegapixels >= 4) return 2
  if (maxMegapixels >= 2) return 3
  return 4
}

async function startThumbnailBackfill(id: string) {
  const session = captureGallerySession()
  thumbnailBackfillRunningIds.add(id)

  try {
    if (getCachedThumbnail(id)) return

    const thumbnail = await getImageThumbnail(id, session.scope)
    if (!isGallerySessionCurrent(session)) return
    if (thumbnail?.thumbnailDataUrl) {
      cacheThumbnail(id, {
        dataUrl: thumbnail.thumbnailDataUrl,
        width: thumbnail.width,
        height: thumbnail.height,
        thumbnailVersion: thumbnail.thumbnailVersion,
      })
      thumbnailSubscribers.get(id)?.forEach((callback) => callback({
        dataUrl: thumbnail.thumbnailDataUrl,
        width: thumbnail.width,
        height: thumbnail.height,
      }))
    }
  } catch {
    // 缩略图生成失败时保留占位图，后续仍可再次补全。
  } finally {
    thumbnailBackfillRunningIds.delete(id)
    if (isGallerySessionCurrent(session)) scheduleThumbnailBackfillTick()
  }
}
