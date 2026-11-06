import type { TaskRecord } from '../types'
import { blobToDataUrl, imageSourceToBlob } from './dataUrl'
import { imageDataUrlToPngBlob } from './canvasImage'
import { currentGalleryUserId, isVerifiedGalleryAuth, verifyGalleryIdentity } from './gallerySession'

const BASE = '/api/v1/image-studio/gallery'

export type GalleryVariant = 'thumbnail' | 'preview' | 'original'

export interface GalleryAssetUrl {
  url: string
  expires_at: string
  bytes: number
  mime: string
  width: number
  height: number
}

export class GalleryApiError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string) {
    super(message)
    this.name = 'GalleryApiError'
  }
}

function token() {
  const value = localStorage.getItem('auth_token')
  if (!value) throw new GalleryApiError('Gallery authentication is required', 401)
  return value
}

async function request<T>(path: string, init: RequestInit = {}, authToken = token()): Promise<T> {
  if (path !== '/status' && !isVerifiedGalleryAuth(authToken)) {
    await ensureGalleryIdentity(authToken)
  }
  const response = await fetch(BASE + path, {
    ...init,
    cache: 'no-store',
    headers: { Authorization: 'Bearer ' + authToken, ...init.headers },
  })
  let payload: { code?: number; message?: string; reason?: string; data?: T; error?: { code?: string; message?: string } }
  try {
    payload = await response.json() as typeof payload
  } catch {
    throw new GalleryApiError('Invalid gallery response', response.status)
  }
  if (!response.ok || payload.code !== 0 || !payload.data) {
    throw new GalleryApiError(payload.error?.message ?? payload.message ?? 'Gallery request failed', response.status, payload.error?.code ?? payload.reason)
  }
  return payload.data
}

/** Revalidate a refreshed JWT before any cloud asset or task operation. */
export async function ensureGalleryIdentity(authToken = token()) {
  if (isVerifiedGalleryAuth(authToken)) return
  const status = await request<{ enabled: boolean; user_id: number }>('/status', {}, authToken)
  if (localStorage.getItem('auth_token') !== authToken || currentGalleryUserId() !== status.user_id) {
    throw new GalleryApiError('Gallery identity changed', 401)
  }
  verifyGalleryIdentity(status.user_id, authToken)
  if (!isVerifiedGalleryAuth(authToken)) throw new GalleryApiError('Gallery scope does not match the user', 401)
}

export function getGalleryStatus() {
  return request<{ enabled: boolean; user_id: number }>('/status')
}

export async function putGalleryAsset(imageId: string, dataUrl: string) {
  const authToken = token()
  const source = await imageSourceToBlob(dataUrl)
  // Preserve legacy uploads locally; archive a decodable unsupported format as PNG.
  const blob = ['image/png', 'image/jpeg', 'image/webp'].includes(source.type)
    ? source
    : await imageDataUrlToPngBlob(dataUrl)
  const data = await request<{ id: string }>('/assets/' + encodeURIComponent(imageId), {
    method: 'PUT',
    headers: { 'Content-Type': blob.type },
    body: blob,
  }, authToken)
  if (data.id !== imageId) throw new GalleryApiError('Gallery image ID mismatch', 502)
  return data
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
