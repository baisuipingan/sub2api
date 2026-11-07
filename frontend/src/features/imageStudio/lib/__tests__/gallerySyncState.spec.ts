import { describe, expect, it } from 'vitest'
import { normalizeGallerySyncRecord } from '../db'
import { gallerySyncReasonKey, galleryTaskSyncStatus } from '../gallerySyncDisplay'

describe('gallery sync state compatibility', () => {
  it('recovers old boolean outbox entries and interrupted uploads as pending', () => {
    expect(normalizeGallerySyncRecord(true)).toEqual({
      status: 'pending', attempts: 0, nextRetryAt: 0, retryable: true,
    })
    expect(normalizeGallerySyncRecord({ status: 'syncing', attempts: 2, nextRetryAt: 99, retryable: true }).status).toBe('pending')
  })

  it('keeps a stopped retry reason across page reloads without storing a raw server message', () => {
    expect(normalizeGallerySyncRecord({ status: 'error', attempts: 3, retryable: false, errorCode: 'GALLERY_QUOTA_EXCEEDED' })).toMatchObject({
      status: 'error', attempts: 3, retryable: false, errorCode: 'GALLERY_QUOTA_EXCEEDED',
    })
    expect(gallerySyncReasonKey('GALLERY_QUOTA_EXCEEDED')).toBe('errorQuota')
  })

  it('marks a user-scoped task without an outbox marker as archived', () => {
    expect(galleryTaskSyncStatus(7, {}, 'task')).toBe('synced')
    expect(galleryTaskSyncStatus(null, {}, 'task')).toBe('local')
  })
})
