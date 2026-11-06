import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TaskRecord } from '../../types'

const { getImage, getGalleryAssetUrl, putGalleryAsset, putGalleryTask } = vi.hoisted(() => ({
  getImage: vi.fn(), getGalleryAssetUrl: vi.fn(), putGalleryAsset: vi.fn(), putGalleryTask: vi.fn(),
}))
vi.mock('../db', () => ({ getImage }))
vi.mock('../galleryApi', () => ({ getGalleryAssetUrl, putGalleryAsset, putGalleryTask }))

import { taskAssetIds, uploadGalleryTask } from '../gallerySync'

const task = {
  id: 'task-1', status: 'done', inputImageIds: ['reference', 'reference'],
  maskTargetImageId: 'reference', maskImageId: 'mask',
  outputImages: ['output-1', 'output-2'], transparentOriginalImages: ['original'],
} as TaskRecord

beforeEach(() => {
  vi.clearAllMocks()
  getImage.mockImplementation(async (id: string) => ({ id, dataUrl: 'data:image/png;base64,aW1hZ2U=' }))
  getGalleryAssetUrl.mockResolvedValue({ url: 'https://storage.test/original' })
  putGalleryAsset.mockImplementation(async (id: string) => ({ id }))
  putGalleryTask.mockResolvedValue({ task })
})

describe('gallery task archival', () => {
  it('includes every image role exactly once before saving metadata', async () => {
    expect(taskAssetIds(task)).toEqual(['reference', 'mask', 'output-1', 'output-2', 'original'])
    await uploadGalleryTask(task, () => true, 'user-scope')
    expect(getImage).toHaveBeenCalledTimes(5)
    expect(getImage).toHaveBeenCalledWith('reference', 'user-scope')
    expect(putGalleryAsset.mock.calls.map((call) => call[0])).toEqual(taskAssetIds(task))
    expect(putGalleryTask).toHaveBeenCalledOnce()
  })

  it('reuses a cloud-only reference without re-uploading it', async () => {
    getImage.mockImplementation(async (id: string) => id === 'reference' ? undefined : { id, dataUrl: 'data:image/png;base64,aW1hZ2U=' })
    await uploadGalleryTask(task)
    expect(getGalleryAssetUrl).toHaveBeenCalledWith('reference', 'original')
    expect(putGalleryAsset).not.toHaveBeenCalledWith('reference', expect.anything())
    expect(putGalleryTask).toHaveBeenCalledOnce()
  })

  it('does not publish incomplete metadata after one asset fails', async () => {
    putGalleryAsset.mockImplementation(async (id: string) => {
      if (id === 'output-2') throw new Error('storage failed')
      return { id }
    })
    await expect(uploadGalleryTask(task)).rejects.toThrow('storage failed')
    expect(putGalleryTask).not.toHaveBeenCalled()
  })

  it('stops uploading when the account changes during an asset write', async () => {
    let current = true
    putGalleryAsset.mockImplementationOnce(async (id: string) => { current = false; return { id } })
    await expect(uploadGalleryTask(task, () => current)).rejects.toThrow('Gallery session changed')
    expect(putGalleryAsset).toHaveBeenCalledTimes(1)
    expect(putGalleryTask).not.toHaveBeenCalled()
  })
})
