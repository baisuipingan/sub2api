import type { StoredImage, StoredImageThumbnail, TaskRecord } from '../types'

const DB_NAME = 'milorouter-ai-image'
const DB_VERSION = 1
const STORE_TASKS = 'tasks'
const STORE_IMAGES = 'images'
const STORE_THUMBNAILS = 'thumbnails'
const THUMBNAIL_MAX_SIZE = 720
const THUMBNAIL_QUALITY = 0.9
const THUMBNAIL_VERSION = 2

export const CURRENT_THUMBNAIL_VERSION = THUMBNAIL_VERSION

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION)
    request.onupgradeneeded = () => {
      const db = request.result
      if (!db.objectStoreNames.contains(STORE_TASKS)) db.createObjectStore(STORE_TASKS, { keyPath: 'id' })
      if (!db.objectStoreNames.contains(STORE_IMAGES)) db.createObjectStore(STORE_IMAGES, { keyPath: 'id' })
      if (!db.objectStoreNames.contains(STORE_THUMBNAILS)) db.createObjectStore(STORE_THUMBNAILS, { keyPath: 'id' })
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function requestTransaction<T>(
  stores: string | string[],
  mode: IDBTransactionMode,
  run: (transaction: IDBTransaction) => IDBRequest<T>,
) {
  return openDB().then((db) => new Promise<T>((resolve, reject) => {
    const transaction = db.transaction(stores, mode)
    const request = run(transaction)
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
    transaction.oncomplete = () => db.close()
    transaction.onerror = () => reject(transaction.error)
    transaction.onabort = () => reject(transaction.error)
  }))
}

export function getAllTasks() {
  return requestTransaction<TaskRecord[]>(STORE_TASKS, 'readonly', (transaction) => transaction.objectStore(STORE_TASKS).getAll())
}

export function putTask(task: TaskRecord) {
  return requestTransaction<IDBValidKey>(STORE_TASKS, 'readwrite', (transaction) => transaction.objectStore(STORE_TASKS).put(task))
}

export function deleteTask(id: string) {
  return requestTransaction<undefined>(STORE_TASKS, 'readwrite', (transaction) => transaction.objectStore(STORE_TASKS).delete(id))
}

export function commitTaskDeletion(deletedTaskIds: string[], updatedTasks: TaskRecord[]) {
  return openDB().then((db) => new Promise<void>((resolve, reject) => {
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

export function clearTasks() {
  return requestTransaction<undefined>(STORE_TASKS, 'readwrite', (transaction) => transaction.objectStore(STORE_TASKS).clear())
}

export function getImage(id: string) {
  return requestTransaction<StoredImage | undefined>(STORE_IMAGES, 'readonly', (transaction) => transaction.objectStore(STORE_IMAGES).get(id))
}

export function getStoredImageThumbnail(id: string) {
  return requestTransaction<StoredImageThumbnail | undefined>(STORE_THUMBNAILS, 'readonly', (transaction) => transaction.objectStore(STORE_THUMBNAILS).get(id))
}

export async function getStoredFreshImageThumbnail(id: string) {
  const thumbnail = await getStoredImageThumbnail(id)
  return thumbnail?.thumbnailVersion === THUMBNAIL_VERSION ? thumbnail : undefined
}

export function putImage(image: StoredImage) {
  return requestTransaction<IDBValidKey>(STORE_IMAGES, 'readwrite', (transaction) => transaction.objectStore(STORE_IMAGES).put(image))
}

export function putImageThumbnail(thumbnail: StoredImageThumbnail) {
  return requestTransaction<IDBValidKey>(STORE_THUMBNAILS, 'readwrite', (transaction) => transaction.objectStore(STORE_THUMBNAILS).put(thumbnail))
}

export async function getImageThumbnail(id: string) {
  const stored = await getStoredImageThumbnail(id)
  if (stored?.thumbnailVersion === THUMBNAIL_VERSION) return stored

  const image = await getImage(id)
  if (!image) return undefined
  const thumbnail = await createImageThumbnail(image.dataUrl)
  const record: StoredImageThumbnail = {
    id,
    thumbnailDataUrl: thumbnail.thumbnailDataUrl,
    width: thumbnail.width,
    height: thumbnail.height,
    thumbnailVersion: THUMBNAIL_VERSION,
  }
  await putImageThumbnail(record)
  if (thumbnail.width && thumbnail.height && (!image.width || !image.height)) {
    await putImage({ ...image, width: thumbnail.width, height: thumbnail.height })
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

export function deleteImage(id: string) {
  return openDB().then((db) => new Promise<void>((resolve, reject) => {
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

export function clearImages() {
  return openDB().then((db) => new Promise<void>((resolve, reject) => {
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

export async function storeImage(dataUrl: string, source: NonNullable<StoredImage['source']> = 'upload') {
  return (await storeImageWithSize(dataUrl, source)).id
}

export async function storeImageWithSize(dataUrl: string, source: NonNullable<StoredImage['source']> = 'upload') {
  const id = await hashDataUrl(dataUrl)
  const existing = await getImage(id)
  if (existing) return { id, width: existing.width, height: existing.height }

  const thumbnail = await safeCreateImageThumbnail(dataUrl)
  await putImage({
    id,
    dataUrl,
    createdAt: Date.now(),
    source,
    width: thumbnail.width,
    height: thumbnail.height,
  })
  if (thumbnail.thumbnailDataUrl) {
    await putImageThumbnail({
      id,
      thumbnailDataUrl: thumbnail.thumbnailDataUrl,
      width: thumbnail.width,
      height: thumbnail.height,
      thumbnailVersion: THUMBNAIL_VERSION,
    })
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
  if (!context) throw new Error('当前浏览器不支持 Canvas')
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
