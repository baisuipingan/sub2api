import { libT } from './i18nMessage'
import type { StoredImage, StoredImageThumbnail, TaskRecord } from '../types'

const LEGACY_DB_NAME = 'milorouter-ai-image'
const DB_VERSION = 2
let dbName = LEGACY_DB_NAME + '-unassigned'
const STORE_TASKS = 'tasks'
const STORE_IMAGES = 'images'
const STORE_THUMBNAILS = 'thumbnails'
const STORE_SYNC = 'pending-sync'
const THUMBNAIL_MAX_SIZE = 720
const THUMBNAIL_QUALITY = 0.9
const THUMBNAIL_VERSION = 2

export interface GallerySyncRecord {
  status: 'pending' | 'syncing' | 'error'
  attempts: number
  nextRetryAt: number
  retryable: boolean
  errorCode?: string
}

const NEW_SYNC: GallerySyncRecord = { status: 'pending', attempts: 0, nextRetryAt: 0, retryable: true }

/** Legacy outbox values were boolean true. An interrupted upload is safe to re-upload. */
export function normalizeGallerySyncRecord(value: unknown): GallerySyncRecord {
  if (!value || typeof value !== 'object') return { ...NEW_SYNC }
  const raw = value as Partial<GallerySyncRecord>
  return {
    status: raw.status === 'error' ? 'error' : 'pending',
    attempts: Number.isSafeInteger(raw.attempts) && Number(raw.attempts) >= 0 ? Number(raw.attempts) : 0,
    nextRetryAt: Number.isFinite(raw.nextRetryAt) && Number(raw.nextRetryAt) > 0 ? Number(raw.nextRetryAt) : 0,
    retryable: raw.retryable !== false,
    ...(typeof raw.errorCode === 'string' ? { errorCode: raw.errorCode.slice(0, 64) } : {}),
  }
}

export const CURRENT_THUMBNAIL_VERSION = THUMBNAIL_VERSION

/** The pre-gallery database has no owner and is opened only for explicit import. */
export function setImageStudioUserScope(userId: number | null) {
  dbName = userId && Number.isSafeInteger(userId) && userId > 0
    ? LEGACY_DB_NAME + '-user-' + userId
    : LEGACY_DB_NAME + '-unassigned'
}

export function getImageStudioDbScope() { return dbName }

function openDB(name = dbName): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(name, name === LEGACY_DB_NAME ? 1 : DB_VERSION)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(STORE_TASKS)) db.createObjectStore(STORE_TASKS, { keyPath: 'id' })
      if (!db.objectStoreNames.contains(STORE_IMAGES)) db.createObjectStore(STORE_IMAGES, { keyPath: 'id' })
      if (!db.objectStoreNames.contains(STORE_THUMBNAILS)) db.createObjectStore(STORE_THUMBNAILS, { keyPath: 'id' })
      if (name !== LEGACY_DB_NAME && !db.objectStoreNames.contains(STORE_SYNC)) db.createObjectStore(STORE_SYNC)
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function requestTransaction<T>(
  stores: string | string[],
  mode: IDBTransactionMode,
  run: (transaction: IDBTransaction) => IDBRequest<T>,
  name = dbName,
) {
  return openDB(name).then((db) => new Promise<T>((resolve, reject) => {
    const transaction = db.transaction(stores, mode)
    const request = run(transaction)
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
    transaction.oncomplete = () => db.close()
    transaction.onerror = () => { db.close(); reject(transaction.error) }
    transaction.onabort = () => { db.close(); reject(transaction.error) }
  }))
}

function writeSyncRecord(id: string, record: GallerySyncRecord | null, scope: string): Promise<void> {
  return openDB(scope).then((db) => new Promise<void>((resolve, reject) => {
    const transaction = db.transaction(STORE_SYNC, 'readwrite')
    if (record) transaction.objectStore(STORE_SYNC).put(record, id)
    else transaction.objectStore(STORE_SYNC).delete(id)
    transaction.oncomplete = () => { db.close(); resolve() }
    transaction.onerror = () => { db.close(); reject(transaction.error) }
    transaction.onabort = () => { db.close(); reject(transaction.error) }
  }))
}

export function getAllTasks(scope = dbName) {
  return requestTransaction<TaskRecord[]>(STORE_TASKS, 'readonly', (transaction) => transaction.objectStore(STORE_TASKS).getAll(), scope)
}

export function getLegacyTasks() {
  return requestTransaction<TaskRecord[]>(STORE_TASKS, 'readonly', (transaction) => transaction.objectStore(STORE_TASKS).getAll(), LEGACY_DB_NAME)
}

export function getLegacyImage(id: string) {
  return requestTransaction<StoredImage | undefined>(STORE_IMAGES, 'readonly', (transaction) => transaction.objectStore(STORE_IMAGES).get(id), LEGACY_DB_NAME)
}

export function markTaskPendingSync(id: string, scope = dbName) {
  return writeSyncRecord(id, { ...NEW_SYNC }, scope)
}

export function setTaskSyncRecord(id: string, record: GallerySyncRecord, scope = dbName) {
  return writeSyncRecord(id, record, scope)
}

export async function getTaskSyncRecord(id: string, scope = dbName): Promise<GallerySyncRecord | null> {
  const value = await requestTransaction<unknown>(STORE_SYNC, 'readonly', (transaction) => transaction.objectStore(STORE_SYNC).get(id), scope)
  return value === undefined ? null : normalizeGallerySyncRecord(value)
}

export function putTaskWithPendingSync(task: TaskRecord, scope = dbName) {
  return openDB(scope).then((db) => new Promise<void>((resolve, reject) => {
    const transaction = db.transaction([STORE_TASKS, STORE_SYNC], 'readwrite')
    transaction.objectStore(STORE_TASKS).put(task)
    transaction.objectStore(STORE_SYNC).put({ ...NEW_SYNC }, task.id)
    transaction.oncomplete = () => { db.close(); resolve() }
    transaction.onerror = () => { db.close(); reject(transaction.error) }
    transaction.onabort = () => { db.close(); reject(transaction.error) }
  }))
}

export function clearTaskPendingSync(id: string, scope = dbName) {
  return writeSyncRecord(id, null, scope)
}

/** A server tombstone rejects re-uploads; remove the local task and retry marker atomically. */
export function discardGoneTask(id: string, scope = dbName) {
  return openDB(scope).then((db) => new Promise<void>((resolve, reject) => {
    const transaction = db.transaction([STORE_TASKS, STORE_SYNC], 'readwrite')
    transaction.objectStore(STORE_TASKS).delete(id)
    transaction.objectStore(STORE_SYNC).delete(id)
    transaction.oncomplete = () => { db.close(); resolve() }
    transaction.onerror = () => { db.close(); reject(transaction.error) }
    transaction.onabort = () => { db.close(); reject(transaction.error) }
  }))
}

export function getPendingSyncTaskIds(scope = dbName) {
  return requestTransaction<IDBValidKey[]>(STORE_SYNC, 'readonly', (transaction) => transaction.objectStore(STORE_SYNC).getAllKeys(), scope).then((ids) => ids.map(String))
}

export function getPendingSyncRecords(scope = dbName): Promise<Array<{ id: string; record: GallerySyncRecord }>> {
  return openDB(scope).then((db) => new Promise((resolve, reject) => {
    const result: Array<{ id: string; record: GallerySyncRecord }> = []
    const transaction = db.transaction(STORE_SYNC, 'readonly')
    const cursor = transaction.objectStore(STORE_SYNC).openCursor()
    cursor.onsuccess = () => {
      const item = cursor.result
      if (!item) return
      result.push({ id: String(item.key), record: normalizeGallerySyncRecord(item.value) })
      item.continue()
    }
    transaction.oncomplete = () => { db.close(); resolve(result) }
    transaction.onerror = () => { db.close(); reject(transaction.error) }
    transaction.onabort = () => { db.close(); reject(transaction.error) }
  }))
}

export function putTask(task: TaskRecord, scope = dbName) {
  return requestTransaction<IDBValidKey>(STORE_TASKS, 'readwrite', (transaction) => transaction.objectStore(STORE_TASKS).put(task), scope)
}

export function deleteTask(id: string) {
  return requestTransaction<undefined>(STORE_TASKS, 'readwrite', (transaction) => transaction.objectStore(STORE_TASKS).delete(id))
}

export function commitTaskDeletion(deletedTaskIds: string[], updatedTasks: TaskRecord[], scope = dbName) {
  return openDB(scope).then((db) => new Promise<void>((resolve, reject) => {
    const transaction = db.transaction(STORE_TASKS, 'readwrite')
    const store = transaction.objectStore(STORE_TASKS)
    for (const id of deletedTaskIds) store.delete(id)
    for (const task of updatedTasks) store.put(task)
    transaction.oncomplete = () => {
      db.close()
      resolve()
    }
    transaction.onerror = () => reject(transaction.error)
    transaction.onabort = () => reject(transaction.error)
  }))
}

export function clearTasks(scope = dbName) {
  return requestTransaction<undefined>(STORE_TASKS, 'readwrite', (transaction) => transaction.objectStore(STORE_TASKS).clear(), scope)
}

export function getImage(id: string, scope = dbName) {
  return requestTransaction<StoredImage | undefined>(STORE_IMAGES, 'readonly', (transaction) => transaction.objectStore(STORE_IMAGES).get(id), scope)
}

export function getStoredImageThumbnail(id: string, scope = dbName) {
  return requestTransaction<StoredImageThumbnail | undefined>(STORE_THUMBNAILS, 'readonly', (transaction) => transaction.objectStore(STORE_THUMBNAILS).get(id), scope)
}

export async function getStoredFreshImageThumbnail(id: string, scope = dbName) {
  const thumbnail = await getStoredImageThumbnail(id, scope)
  return thumbnail?.thumbnailVersion === THUMBNAIL_VERSION ? thumbnail : undefined
}

export function putImage(image: StoredImage, scope = dbName) {
  return requestTransaction<IDBValidKey>(STORE_IMAGES, 'readwrite', (transaction) => transaction.objectStore(STORE_IMAGES).put(image), scope)
}

export function putImageThumbnail(thumbnail: StoredImageThumbnail, scope = dbName) {
  return requestTransaction<IDBValidKey>(STORE_THUMBNAILS, 'readwrite', (transaction) => transaction.objectStore(STORE_THUMBNAILS).put(thumbnail), scope)
}

export async function getImageThumbnail(id: string, scope = dbName) {
  const stored = await getStoredImageThumbnail(id, scope)
  if (stored?.thumbnailVersion === THUMBNAIL_VERSION) return stored

  const image = await getImage(id, scope)
  if (!image) return undefined
  const thumbnail = await createImageThumbnail(image.dataUrl)
  const record: StoredImageThumbnail = {
    id,
    thumbnailDataUrl: thumbnail.thumbnailDataUrl,
    width: thumbnail.width,
    height: thumbnail.height,
    thumbnailVersion: THUMBNAIL_VERSION,
  }
  await putImageThumbnail(record, scope)
  if (thumbnail.width && thumbnail.height && (!image.width || !image.height)) {
    await putImage({ ...image, width: thumbnail.width, height: thumbnail.height }, scope)
  }
  return record
}

export function getAllImages() {
  return requestTransaction<StoredImage[]>(STORE_IMAGES, 'readonly', (transaction) => transaction.objectStore(STORE_IMAGES).getAll())
}

export function getAllImageIds() {
  return requestTransaction<IDBValidKey[]>(STORE_IMAGES, 'readonly', (transaction) => transaction.objectStore(STORE_IMAGES).getAllKeys())
    .then((keys) => keys.map(String))
}

export function deleteImage(id: string, scope = dbName) {
  return openDB(scope).then((db) => new Promise<void>((resolve, reject) => {
    const transaction = db.transaction([STORE_IMAGES, STORE_THUMBNAILS], 'readwrite')
    transaction.objectStore(STORE_IMAGES).delete(id)
    transaction.objectStore(STORE_THUMBNAILS).delete(id)
    transaction.oncomplete = () => {
      db.close()
      resolve()
    }
    transaction.onerror = () => reject(transaction.error)
    transaction.onabort = () => reject(transaction.error)
  }))
}

export function clearImages(scope = dbName) {
  return openDB(scope).then((db) => new Promise<void>((resolve, reject) => {
    const transaction = db.transaction([STORE_IMAGES, STORE_THUMBNAILS], 'readwrite')
    transaction.objectStore(STORE_IMAGES).clear()
    transaction.objectStore(STORE_THUMBNAILS).clear()
    transaction.oncomplete = () => {
      db.close()
      resolve()
    }
    transaction.onerror = () => reject(transaction.error)
    transaction.onabort = () => reject(transaction.error)
  }))
}

export async function storeImage(dataUrl: string, source: NonNullable<StoredImage['source']> = 'upload', scope = dbName) {
  return (await storeImageWithSize(dataUrl, source, scope)).id
}

export async function storeImageWithSize(dataUrl: string, source: NonNullable<StoredImage['source']> = 'upload', scope = dbName) {
  const id = await hashDataUrl(dataUrl)
  const existing = await getImage(id, scope)
  if (existing) return { id, width: existing.width, height: existing.height }

  const thumbnail = await safeCreateImageThumbnail(dataUrl)
  await putImage({
    id,
    dataUrl,
    createdAt: Date.now(),
    source,
    width: thumbnail.width,
    height: thumbnail.height,
  }, scope)
  if (thumbnail.thumbnailDataUrl) {
    await putImageThumbnail({
      id,
      thumbnailDataUrl: thumbnail.thumbnailDataUrl,
      width: thumbnail.width,
      height: thumbnail.height,
      thumbnailVersion: THUMBNAIL_VERSION,
    }, scope)
  }
  return { id, width: thumbnail.width, height: thumbnail.height }
}

async function hashDataUrl(dataUrl: string) {
  if (globalThis.crypto?.subtle) {
    const buffer = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(dataUrl))
    return Array.from(new Uint8Array(buffer)).map((byte) => byte.toString(16).padStart(2, '0')).join('')
  }

  let hash = 2166136261
  for (let index = 0; index < dataUrl.length; index += 1) {
    hash ^= dataUrl.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return `image-${(hash >>> 0).toString(16)}`
}

function loadImage(dataUrl: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('图片加载失败'))
    image.src = dataUrl
  })
}

async function createImageThumbnail(dataUrl: string) {
  const image = await loadImage(dataUrl)
  const scale = Math.min(1, THUMBNAIL_MAX_SIZE / Math.max(image.naturalWidth, image.naturalHeight))
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(image.naturalWidth * scale))
  canvas.height = Math.max(1, Math.round(image.naturalHeight * scale))
  const context = canvas.getContext('2d')
  if (!context) throw new Error(libT('imageStudio.lib.canvasUnsupported'))
  context.drawImage(image, 0, 0, canvas.width, canvas.height)
  return {
    thumbnailDataUrl: canvas.toDataURL('image/webp', THUMBNAIL_QUALITY),
    width: image.naturalWidth,
    height: image.naturalHeight,
  }
}

async function safeCreateImageThumbnail(dataUrl: string) {
  try {
    return await createImageThumbnail(dataUrl)
  } catch {
    return { thumbnailDataUrl: '', width: undefined, height: undefined }
  }
}
