import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PRIVATE_VARIANT_DB, PRIVATE_VARIANT_MAX_BYTES, PRIVATE_VARIANT_MAX_ENTRIES, clearPrivateVariantRecords, deletePrivateVariantRecords, readPrivateVariant, writePrivateVariant, type PrivateVariantRecord } from '../privateImageVariantStore'

/** Minimal event-driven IndexedDB adapter; the production transaction and cursor
 * policy runs unchanged, without adding a browser database polyfill dependency. */
function storage(records: Map<string, PrivateVariantRecord>) {
  const opened = vi.fn()
  const close = vi.fn()
  function transaction() {
    let completed = false
    const tx = { oncomplete: null as null | (() => void), onabort: null as null | (() => void), onerror: null as null | (() => void), error: null,
      abort: () => { completed = true; tx.onabort?.() }, objectStore: () => store }
    const finish = () => queueMicrotask(() => { if (!completed) { completed = true; tx.oncomplete?.() } })
    const store = {
      get: (key: string) => {
        const req = { result: undefined as PrivateVariantRecord | undefined, onsuccess: null as null | (() => void) }
        queueMicrotask(() => { req.result = records.has(key) ? { ...records.get(key)! } : undefined; req.onsuccess?.(); finish() })
        return req
      },
      put: (record: PrivateVariantRecord) => { records.set(record.key, { ...record }) },
      delete: (key: string) => { records.delete(key) },
      openCursor: () => {
        const values = [...records.values()]
        let index = 0
        const req = { result: null as null | { value: PrivateVariantRecord; delete: () => void; continue: () => void }, onsuccess: null as null | (() => void) }
        const next = () => queueMicrotask(() => {
          const record = values[index++]
          req.result = record ? { value: { ...record }, delete: () => { records.delete(record.key) }, continue: next } : null
          req.onsuccess?.()
          if (!record) finish()
        })
        next()
        return req
      },
    }
    return tx
  }
  vi.stubGlobal('indexedDB', { open: (name: string, version: number) => {
    opened(name, version)
    const db = { objectStoreNames: { contains: () => true }, close, transaction }
    const req = { result: db, error: null, onupgradeneeded: null as null | (() => void), onerror: null as null | (() => void), onsuccess: null as null | (() => void) }
    queueMicrotask(() => req.onsuccess?.())
    return req
  } })
  return { opened, close }
}
const record = (key: string, overrides: Partial<PrivateVariantRecord> = {}): PrivateVariantRecord => ({ key, viewerId: 1, ownerId: 2, imageId: key, variant: 'thumbnail', version: 'v1', blob: new Blob(['image']), bytes: 5, mime: 'image/png', expiresAt: 10000, lastAccess: 100, ...overrides })
const records = new Map<string, PrivateVariantRecord>()
beforeEach(() => records.clear())
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

describe('bounded disposable image variant storage', () => {
  it('uses its own database, expires stale bytes and touches an existing cache entry', async () => {
    const adapter = storage(records)
    records.set('expired', record('expired', { expiresAt: 100 }))
    records.set('fresh', record('fresh'))
    expect(await readPrivateVariant('expired', 200)).toBeUndefined()
    expect(records.has('expired')).toBe(false)
    const fresh = await readPrivateVariant('fresh', 200)
    expect(fresh?.lastAccess).toBe(200)
    expect(adapter.opened.mock.calls).toEqual([[PRIVATE_VARIANT_DB, 1], [PRIVATE_VARIANT_DB, 1]])
    expect(adapter.close).toHaveBeenCalledTimes(2)
  })

  it('evicts the least recently used entry at the entry limit, regardless of account', async () => {
    storage(records)
    for (let i = 0; i < PRIVATE_VARIANT_MAX_ENTRIES; i++) records.set(String(i), record(String(i), { viewerId: i % 3 + 1, lastAccess: i }))
    await writePrivateVariant(record('new', { lastAccess: 999 }), 100)
    expect(records.size).toBe(PRIVATE_VARIANT_MAX_ENTRIES)
    expect(records.has('0')).toBe(false)
    expect(records.has('1')).toBe(true)
    expect(records.has('new')).toBe(true)
  })

  it('enforces the combined byte budget and removes expired entries first', async () => {
    storage(records)
    records.set('old', record('old', { bytes: PRIVATE_VARIANT_MAX_BYTES / 2, lastAccess: 1 }))
    records.set('recent', record('recent', { bytes: PRIVATE_VARIANT_MAX_BYTES / 2, lastAccess: 2 }))
    records.set('expired', record('expired', { bytes: 1000, expiresAt: 10 }))
    await writePrivateVariant(record('new', { bytes: 1024, lastAccess: 3 }), 100)
    expect([...records.keys()]).toEqual(['recent', 'new'])
    expect([...records.values()].reduce((sum, item) => sum + item.bytes, 0)).toBeLessThanOrEqual(PRIVATE_VARIANT_MAX_BYTES)
    await expect(writePrivateVariant(record('too-big', { bytes: PRIVATE_VARIANT_MAX_BYTES + 1 }), 100)).rejects.toThrow()
    expect(records.has('too-big')).toBe(false)
  })

  it('isolates deletes by viewer, owner and variant and guards late writes', async () => {
    storage(records)
    records.set('thumb', record('thumb', { imageId: 'same-image' }))
    records.set('preview', record('preview', { imageId: 'same-image', variant: 'preview' }))
    records.set('other-viewer', record('other-viewer', { viewerId: 3, imageId: 'same-image' }))
    records.set('other-owner', record('other-owner', { ownerId: 4, imageId: 'same-image' }))
    await deletePrivateVariantRecords(1, 2, ['same-image'], ['preview'])
    expect([...records.keys()]).toEqual(['thumb', 'other-viewer', 'other-owner'])
    await writePrivateVariant(record('late'), 100, () => false)
    expect(records.has('late')).toBe(false)
    await clearPrivateVariantRecords(1, 2)
    expect([...records.keys()]).toEqual(['other-viewer', 'other-owner'])
  })

  it('times out a blocked database without touching any other database', async () => {
    vi.useFakeTimers()
    const open = vi.fn(() => ({}))
    vi.stubGlobal('indexedDB', { open })
    const pending = readPrivateVariant('blocked', 100)
    const rejected = expect(pending).rejects.toThrow('blocked')
    await vi.advanceTimersByTimeAsync(1600)
    await rejected
    expect(open).toHaveBeenCalledWith(PRIVATE_VARIANT_DB, 1)
  })
})
