/** A disposable cache, deliberately separate from Studio tasks and originals. */
export const PRIVATE_VARIANT_DB = 'milorouter-private-image-variants-v1'
const STORE = 'variants'
export const PRIVATE_VARIANT_MAX_BYTES = 80 * 1024 * 1024
export const PRIVATE_VARIANT_MAX_ENTRIES = 500
export const PRIVATE_VARIANT_TTL_MS = 7 * 24 * 60 * 60 * 1000
const DATABASE_TIMEOUT_MS = 1500

export type PrivateImageVariant = 'thumbnail' | 'preview'
export interface PrivateVariantRecord {
  key: string
  viewerId: number
  ownerId: number
  imageId: string
  variant: PrivateImageVariant
  version: string
  blob: Blob
  bytes: number
  mime: string
  expiresAt: number
  lastAccess: number
}

async function database(): Promise<IDBDatabase> {
  if (typeof indexedDB === 'undefined') throw new Error('Image cache storage is unavailable')
  return new Promise((resolve, reject) => {
    let settled = false
    const request = indexedDB.open(PRIVATE_VARIANT_DB, 1)
    const timer = setTimeout(() => {
      settled = true
      reject(new Error('Image cache storage is blocked'))
    }, DATABASE_TIMEOUT_MS)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(STORE)) request.result.createObjectStore(STORE, { keyPath: 'key' })
    }
    request.onerror = () => { clearTimeout(timer); settled = true; reject(request.error) }
    request.onsuccess = () => {
      clearTimeout(timer)
      if (settled) { request.result.close(); return }
      settled = true
      resolve(request.result)
    }
  })
}

async function transaction<T>(run: (store: IDBObjectStore, done: (value: T) => void) => void): Promise<T> {
  const db = await database()
  return new Promise((resolve, reject) => {
    let value: T
    let tx: IDBTransaction
    try { tx = db.transaction(STORE, 'readwrite') }
    catch (error) { db.close(); reject(error); return }
    const timer = setTimeout(() => {
      try { tx.abort() } catch { /* Already complete or unavailable. */ }
      db.close()
      reject(new Error('Image cache transaction timed out'))
    }, DATABASE_TIMEOUT_MS)
    const finish = () => { clearTimeout(timer); db.close() }
    tx.oncomplete = () => { finish(); resolve(value) }
    tx.onabort = () => { finish(); reject(tx.error ?? new Error('Image cache transaction aborted')) }
    tx.onerror = () => { /* onabort settles failed requests. */ }
    try { run(tx.objectStore(STORE), result => { value = result }) }
    catch (error) { finish(); try { tx.abort() } catch { /* Invalid transaction. */ } reject(error) }
  })
}

export function readPrivateVariant(key: string, now: number): Promise<PrivateVariantRecord | undefined> {
  return transaction((store, done) => {
    const request = store.get(key)
    request.onsuccess = () => {
      const record = request.result as PrivateVariantRecord | undefined
      if (!record || !Number.isFinite(record.expiresAt) || record.expiresAt <= now) {
        if (record) store.delete(key)
        done(undefined)
        return
      }
      record.lastAccess = now
      store.put(record)
      done(record)
    }
  })
}

export function writePrivateVariant(record: PrivateVariantRecord, now: number, canWrite: () => boolean = () => true): Promise<void> {
  if (!Number.isFinite(record.bytes) || record.bytes <= 0 || record.bytes > PRIVATE_VARIANT_MAX_BYTES) return Promise.reject(new Error('Image cache entry exceeds its budget'))
  return transaction((store, done) => {
    const candidates: Array<Pick<PrivateVariantRecord, 'key' | 'bytes' | 'lastAccess'>> = []
    let total = 0
    const cursor = store.openCursor()
    cursor.onsuccess = () => {
      const item = cursor.result
      if (item) {
        const value = item.value as PrivateVariantRecord
        if (value.key === record.key || !Number.isFinite(value.bytes) || value.bytes <= 0 || !Number.isFinite(value.expiresAt) || value.expiresAt <= now) item.delete()
        else { total += value.bytes; candidates.push({ key: value.key, bytes: value.bytes, lastAccess: value.lastAccess }) }
        item.continue()
        return
      }
      candidates.sort((a, b) => a.lastAccess - b.lastAccess)
      while (candidates.length && (total + record.bytes > PRIVATE_VARIANT_MAX_BYTES || candidates.length >= PRIVATE_VARIANT_MAX_ENTRIES)) {
        const oldest = candidates.shift()!
        store.delete(oldest.key)
        total -= oldest.bytes
      }
      if (canWrite()) store.put(record)
      done(undefined)
    }
  })
}

export function deletePrivateVariantRecords(viewerId: number, ownerId: number, imageIds: readonly string[], variants: readonly PrivateImageVariant[]): Promise<void> {
  const ids = new Set(imageIds)
  const selected = new Set(variants)
  return transaction((store, done) => {
    const cursor = store.openCursor()
    cursor.onsuccess = () => {
      const item = cursor.result
      if (!item) { done(undefined); return }
      const record = item.value as PrivateVariantRecord
      if (record.viewerId === viewerId && record.ownerId === ownerId && ids.has(record.imageId) && selected.has(record.variant)) item.delete()
      item.continue()
    }
  })
}

export function clearPrivateVariantRecords(viewerId: number, ownerId?: number): Promise<void> {
  return transaction((store, done) => {
    const cursor = store.openCursor()
    cursor.onsuccess = () => {
      const item = cursor.result
      if (!item) { done(undefined); return }
      const record = item.value as PrivateVariantRecord
      if (record.viewerId === viewerId && (ownerId === undefined || record.ownerId === ownerId)) item.delete()
      item.continue()
    }
  })
}
