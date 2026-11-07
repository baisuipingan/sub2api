export type GalleryTaskSyncStatus = 'local' | 'pending' | 'syncing' | 'synced' | 'error'

export function galleryTaskSyncStatus(
  userId: number | null | undefined,
  states: Record<string, GalleryTaskSyncStatus> | undefined,
  taskId: string,
): GalleryTaskSyncStatus {
  if (!userId) return 'local'
  // User-scoped tasks without an outbox marker were already archived. The
  // explicit old-history import always creates one before showing the task.
  return states?.[taskId] ?? 'synced'
}

export function gallerySyncLabelKey(status: GalleryTaskSyncStatus): string {
  return {
    local: 'localOnly',
    pending: 'syncPending',
    syncing: 'syncing',
    synced: 'synced',
    error: 'syncFailed',
  }[status]
}

export function gallerySyncReasonKey(code: string | undefined): string {
  switch (code) {
    case 'GALLERY_QUOTA_EXCEEDED': return 'errorQuota'
    case 'GALLERY_DISABLED': return 'errorDisabled'
    case 'GALLERY_CONFLICT': return 'errorConflict'
    case 'GALLERY_INVALID': return 'errorInvalid'
    case 'GALLERY_PUBLIC_STORAGE': return 'errorStorageConfig'
    case 'GALLERY_NOT_FOUND': return 'errorImageMissing'
    case 'AUTH': return 'errorAuth'
    case 'NETWORK': return 'errorNetwork'
    default: return 'errorTemporary'
  }
}
