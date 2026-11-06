import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getImage, putImage, putTask } = vi.hoisted(() => ({
  getImage: vi.fn(),
  putImage: vi.fn(),
  putTask: vi.fn(),
}))

vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  getImage,
  putImage,
  putTask,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { buildExportZip } from '../../lib/exportZip'
import { setImageStudioUserScope } from '../../lib/db'
import { verifyGalleryIdentity } from '../../lib/gallerySession'
import { importData, storeState, useStore } from '../store'

beforeEach(() => {
  getImage.mockReset().mockResolvedValue(undefined)
  putImage.mockReset().mockResolvedValue(undefined)
  putTask.mockReset().mockResolvedValue(undefined)
  localStorage.setItem('auth_token', 'token-a')
  localStorage.setItem('auth_user', JSON.stringify({ id: 42 }))
  setImageStudioUserScope(42)
  verifyGalleryIdentity(42, 'token-a')
  useStore.setState({ tasks: [], galleryInitialized: true, galleryUserId: 42, galleryEnabled: false })
})

describe('image-only backup parts', () => {
  it('imports images even when this shard has no task manifest', async () => {
    const image = { id: 'image-in-later-shard', dataUrl: 'data:image/png;base64,AAECAw==' }
    const { manifest, bytes } = await buildExportZip({
      options: { exportTasks: true },
      exportedAt: Date.UTC(2026, 10, 7),
      settings: storeState.settings,
      tasks: [],
      images: [image],
      thumbnailsByImageId: new Map(),
      includeManifestData: false,
      backupPart: { id: 'split-backup', index: 2, total: 2 },
    })
    expect(manifest.tasks).toBeUndefined()

    const file = {
      arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
    } as File
    await importData(file, { importTasks: true })

    expect(putImage).toHaveBeenCalledWith(expect.objectContaining(image), expect.any(String))
    expect(putTask).not.toHaveBeenCalled()
  })

  it('never imports old images into a newly selected account', async () => {
    const { bytes } = await buildExportZip({
      options: { exportTasks: true }, exportedAt: Date.UTC(2026, 10, 7),
      settings: storeState.settings, tasks: [],
      images: [{ id: 'old-image', dataUrl: 'data:image/png;base64,AAECAw==' }],
      thumbnailsByImageId: new Map(), includeManifestData: false,
    })
    let finishRead!: (value: ArrayBuffer) => void
    const file = { arrayBuffer: () => new Promise<ArrayBuffer>((resolve) => { finishRead = resolve }) } as File
    const importing = importData(file, { importTasks: true })
    localStorage.setItem('auth_token', 'token-b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 99 }))
    useStore.setState({ galleryUserId: 99 })
    finishRead(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength))
    await importing

    expect(putImage).not.toHaveBeenCalled()
    expect(putTask).not.toHaveBeenCalled()
  })
})
