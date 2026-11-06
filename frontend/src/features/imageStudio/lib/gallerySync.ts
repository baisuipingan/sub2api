import type { TaskRecord } from '../types'
import { getImage } from './db'
import { getGalleryAssetUrl, putGalleryAsset, putGalleryTask } from './galleryApi'

/** The same original may be referenced by multiple roles or tasks. */
export function taskAssetIds(task: TaskRecord): string[] {
  return [...new Set([
    ...task.inputImageIds,
    ...(task.maskTargetImageId ? [task.maskTargetImageId] : []),
    ...(task.maskImageId ? [task.maskImageId] : []),
    ...task.outputImages,
    ...(task.transparentOriginalImages ?? []),
  ])]
}

/** Upload only local results; never call a paid image model while retrying. */
export async function uploadGalleryTask(task: TaskRecord, isCurrentSession: () => boolean = () => true, scope?: string) {
  if (task.status === 'running') throw new Error('A running task cannot be archived')
  for (const id of taskAssetIds(task)) {
    if (!isCurrentSession()) throw new Error('Gallery session changed')
    const image = await getImage(id, scope)
    if (!image?.dataUrl) {
      // Cloud history can be reused before its original has been cached on this device.
      await getGalleryAssetUrl(id, 'original')
      continue
    }
    if (!isCurrentSession()) throw new Error('Gallery session changed')
    await putGalleryAsset(id, image.dataUrl)
  }
  if (!isCurrentSession()) throw new Error('Gallery session changed')
  return putGalleryTask(task)
}
