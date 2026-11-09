import type { TaskRecord } from '../types'
import { blobToDataUrl, imageSourceToBlob } from './dataUrl'
import { imageDataUrlToPngBlob } from './canvasImage'
import { currentGalleryUserId, isVerifiedGalleryAuth, verifyGalleryIdentity } from './gallerySession'
import { StudioRequestTimeoutError, withStudioRequestDeadline } from './requestDeadline'
import { libT } from './i18nMessage'
import { safeMessage } from './apiErrors'

const BASE = '/api/v1/image-studio/gallery'

export type GalleryVariant = 'thumbnail' | 'preview' | 'original'

export interface GalleryAssetUrl {
  url: string
  expires_at: string
  cache_version?: string
  bytes: number
  mime: string
  width: number
  height: number
}

export interface GalleryStats {
  tasks: number
  done_tasks: number
  failed_tasks: number
  output_images: number
  assets: number
  used_bytes: number
  max_tasks: number
  max_assets: number
  max_bytes: number
}

export interface GalleryStatus {
  enabled: boolean
  user_id: number
  stats: GalleryStats
  jobs_enabled?: boolean
}

export class GalleryApiError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string, readonly metadata?: Record<string, string>) {
    super(message)
    this.name = 'GalleryApiError'
  }
}

function token() {
  const value = localStorage.getItem('auth_token')
  if (!value) throw new GalleryApiError('Gallery authentication is required', 401)
  return value
}

async function request<T>(path: string, init: RequestInit = {}, authToken = token(), timeoutMs = 20_000): Promise<T> {
  if (path !== '/status' && !isVerifiedGalleryAuth(authToken)) {
    await ensureGalleryIdentity(authToken)
  }
  try {
    return await withStudioRequestDeadline(timeoutMs, init.signal, async signal => {
      const response = await fetch(BASE + path, {
        ...init,
        signal,
        cache: 'no-store',
        headers: { Authorization: 'Bearer ' + authToken, ...init.headers },
      })
      let payload: { code?: number; message?: string; reason?: string; metadata?: Record<string, string>; data?: T; error?: { code?: string; message?: string } }
      try {
        payload = await response.json() as typeof payload
      } catch {
        throw new GalleryApiError('Invalid gallery response', response.status)
      }
      if (!response.ok || payload.code !== 0 || !payload.data) {
        throw new GalleryApiError(payload.error?.message ?? payload.message ?? 'Gallery request failed', response.status, payload.error?.code ?? payload.reason, payload.metadata)
      }
      return payload.data
    })
  } catch (error) {
    if (error instanceof StudioRequestTimeoutError) throw new GalleryApiError('Gallery request timed out', 408, timeoutMs > 20_000 ? 'GALLERY_UPLOAD_TIMEOUT' : 'STUDIO_REQUEST_TIMEOUT')
    throw error
  }
}

/** Revalidate a refreshed JWT before any cloud asset or task operation. */
export async function ensureGalleryIdentity(authToken = token()) {
  if (isVerifiedGalleryAuth(authToken)) return
  const status = await request<GalleryStatus>('/status', {}, authToken)
  if (localStorage.getItem('auth_token') !== authToken || currentGalleryUserId() !== status.user_id) {
    throw new GalleryApiError('Gallery identity changed', 401)
  }
  verifyGalleryIdentity(status.user_id, authToken)
  if (!isVerifiedGalleryAuth(authToken)) throw new GalleryApiError('Gallery scope does not match the user', 401)
}

export function getGalleryStatus() {
  return request<GalleryStatus>('/status')
}

export async function putGalleryAsset(imageId: string, dataUrl: string, authToken = token(), signal?: AbortSignal) {
  if (signal?.aborted) throw new DOMException('Screenshot upload cancelled', 'AbortError')
  const source = await imageSourceToBlob(dataUrl)
  // Preserve legacy uploads locally; archive a decodable unsupported format as PNG.
  const blob = ['image/png', 'image/jpeg', 'image/webp'].includes(source.type)
    ? source
    : await imageDataUrlToPngBlob(dataUrl)
  if (localStorage.getItem('auth_token') !== authToken) {
    throw new GalleryApiError('Gallery login changed', 401)
  }
  const data = await request<{ id: string }>('/assets/' + encodeURIComponent(imageId), {
    method: 'PUT',
    signal,
    headers: { 'Content-Type': blob.type },
    body: blob,
  }, authToken, 120_000)
  if (data.id !== imageId) throw new GalleryApiError('Gallery image ID mismatch', 502)
  return data
}

export function getGalleryErrorMessage(error: unknown): string {
  const keys: Record<string, string> = {
    GALLERY_INVALID: 'invalid', GALLERY_INVALID_ID: 'invalidId', GALLERY_IMAGE_EMPTY: 'empty',
    GALLERY_TOO_LARGE: 'tooLarge', GALLERY_BUSY: 'busy', GALLERY_IMAGE_FORMAT_UNSUPPORTED: 'format',
    GALLERY_IMAGE_TOO_MANY_PIXELS: 'pixels', GALLERY_IMAGE_MIME_MISMATCH: 'mime',
    GALLERY_IMAGE_DECODE_FAILED: 'decode', GALLERY_UPLOAD_TIMEOUT: 'uploadTimeout', STUDIO_REQUEST_TIMEOUT: 'requestTimeout',
  }
  if (!(error instanceof GalleryApiError)) return error instanceof Error ? safeMessage(error.message) : libT('imageStudio.galleryInputErrors.invalid')
  const key = error.code && keys[error.code]
  const detail = safeMessage(error.message)
  if (!key) return detail
  const hint = libT('imageStudio.galleryInputErrors.' + key)
  return error.code === 'GALLERY_INVALID' || error.code?.endsWith('TIMEOUT') ? hint : `${hint}\n${detail}`
}

export function putGalleryTask(task: TaskRecord) {
  return request<{ task: TaskRecord }>('/tasks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ task }),
  })
}

export function listGalleryTasks(cursor: string | null = null, limit = 50) {
  const query = new URLSearchParams({ limit: String(limit) })
  if (cursor) query.set('cursor', cursor)
  return request<{ items: TaskRecord[]; next_cursor: string | null }>('/tasks?' + query)
}

export function getGalleryTask(id: string) {
  return request<{ task: TaskRecord }>('/tasks/' + encodeURIComponent(id))
}

export function deleteGalleryTask(id: string) {
  return request<{ deleted: boolean }>('/tasks/' + encodeURIComponent(id), { method: 'DELETE' })
}

/** The URL is short-lived and must never be persisted as an asset identifier. */
export function getGalleryAssetUrl(imageId: string, variant: GalleryVariant): Promise<GalleryAssetUrl> {
  return request<GalleryAssetUrl>('/assets/' + encodeURIComponent(imageId) + '/url?variant=' + variant)
}

/** Requires S3 bucket CORS for the studio origin; callers can retain a local IDB fallback. */
export async function fetchGalleryAssetBlob(imageId: string, variant: GalleryVariant): Promise<Blob> {
  const asset = await getGalleryAssetUrl(imageId, variant)
  let response: Response
  try {
    response = await fetch(asset.url, { cache: 'no-store' })
  } catch {
    throw new GalleryApiError('Gallery image could not be read; check storage CORS', 0)
  }
  if (!response.ok) throw new GalleryApiError('Gallery image download failed', response.status)
  let blob = await response.blob()
  if (!blob.type && asset.mime.startsWith('image/')) {
    blob = new Blob([await blob.arrayBuffer()], { type: asset.mime })
  }
  if (!blob.type.startsWith('image/')) throw new GalleryApiError('Gallery response is not an image', 502)
  return blob
}

export async function fetchGalleryAssetDataUrl(imageId: string, variant: GalleryVariant): Promise<string> {
  return blobToDataUrl(await fetchGalleryAssetBlob(imageId, variant))
}
