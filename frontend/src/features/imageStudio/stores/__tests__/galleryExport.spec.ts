import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type TaskRecord } from '../../types'

const { getAllImageIds, getImage, listGalleryTasks, getGalleryAssetUrl, fetchGalleryAssetDataUrl, buildExportZip } = vi.hoisted(() => ({
  getAllImageIds: vi.fn(),
  getImage: vi.fn(),
  listGalleryTasks: vi.fn(),
  getGalleryAssetUrl: vi.fn(),
  fetchGalleryAssetDataUrl: vi.fn(),
  buildExportZip: vi.fn(),
}))

vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  getAllImageIds,
  getImage,
}))
vi.mock('../../lib/galleryApi', async () => ({
  ...await vi.importActual<typeof import('../../lib/galleryApi')>('../../lib/galleryApi'),
  listGalleryTasks,
  getGalleryAssetUrl,
  fetchGalleryAssetDataUrl,
}))
vi.mock('../../lib/exportZip', async () => ({
  ...await vi.importActual<typeof import('../../lib/exportZip')>('../../lib/exportZip'),
  buildExportZip,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { exportData, useStore } from '../store'

function task(id: string, imageId: string, createdAt: number): TaskRecord {
  return {
    id, prompt: id, params: { ...DEFAULT_PARAMS }, apiModel: 'gpt-image-2',
    inputImageIds: [], outputImages: [imageId], status: 'done', error: null,
    createdAt, finishedAt: createdAt + 1000, elapsed: 1000,
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
  getAllImageIds.mockReset().mockResolvedValue([])
  getImage.mockReset().mockResolvedValue(undefined)
  getGalleryAssetUrl.mockReset().mockResolvedValue({ url: 'https://example.invalid/image', bytes: 4, mime: 'image/png', width: 1, height: 1, expires_at: '' })
  fetchGalleryAssetDataUrl.mockReset().mockResolvedValue('data:image/png;base64,AAECAw==')
  buildExportZip.mockReset().mockResolvedValue({ bytes: new Uint8Array([1]) })
  Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: vi.fn(() => 'blob:test') })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  localStorage.setItem('auth_token', 'token-for-user-a')
  localStorage.setItem('auth_user', JSON.stringify({ id: 42 }))
  useStore.setState({ tasks: [], galleryUserId: 42, galleryEnabled: true })
})

describe('cloud gallery backup export', () => {
  it('loads every cloud page and full image before building the ZIP', async () => {
    const first = task('task-1', 'image-1', 2000)
    const second = task('task-2', 'image-2', 1000)
    listGalleryTasks.mockReset().mockImplementation(async (cursor: string | null) => cursor
      ? { items: [second], next_cursor: null }
      : { items: [first], next_cursor: 'next' })

    await exportData({ exportTasks: true })

    expect(listGalleryTasks).toHaveBeenCalledTimes(2)
    expect(getGalleryAssetUrl).toHaveBeenCalledTimes(2)
    expect(fetchGalleryAssetDataUrl).toHaveBeenCalledTimes(2)
    const zip = buildExportZip.mock.calls[0][0]
    expect(zip.tasks.map((item: TaskRecord) => item.id)).toEqual([first.id, second.id])
    expect(zip.images.map((item: { id: string }) => item.id)).toEqual(['image-1', 'image-2'])
  })

  it('stops before reading images or downloading when the account changes during pagination', async () => {
    let finishPage!: (value: { items: TaskRecord[]; next_cursor: null }) => void
    listGalleryTasks.mockReset().mockReturnValueOnce(new Promise<{ items: TaskRecord[]; next_cursor: null }>((resolve) => { finishPage = resolve }))

    const exporting = exportData({ exportTasks: true })
    useStore.setState({ galleryUserId: 99 })
    finishPage({ items: [task('task-a', 'image-a', 1000)], next_cursor: null })
    await exporting

    expect(getGalleryAssetUrl).not.toHaveBeenCalled()
    expect(fetchGalleryAssetDataUrl).not.toHaveBeenCalled()
    expect(buildExportZip).not.toHaveBeenCalled()
    expect(HTMLAnchorElement.prototype.click).not.toHaveBeenCalled()
  })

  it('stops when the token switches before the cached user profile updates', async () => {
    let finishPage!: (value: { items: TaskRecord[]; next_cursor: null }) => void
    listGalleryTasks.mockReset().mockReturnValueOnce(new Promise<{ items: TaskRecord[]; next_cursor: null }>((resolve) => { finishPage = resolve }))

    const exporting = exportData({ exportTasks: true })
    localStorage.setItem('auth_token', 'token-for-user-b')
    finishPage({ items: [task('task-b', 'image-b', 1000)], next_cursor: null })
    await exporting

    expect(getGalleryAssetUrl).not.toHaveBeenCalled()
    expect(buildExportZip).not.toHaveBeenCalled()
  })

  it('does not claim an export succeeded when a cloud original cannot be fetched', async () => {
    listGalleryTasks.mockReset().mockResolvedValue({ items: [task('task-1', 'image-1', 1000)], next_cursor: null })
    fetchGalleryAssetDataUrl.mockRejectedValueOnce(new Error('Storage CORS denied the image'))

    await exportData({ exportTasks: true })

    expect(buildExportZip).not.toHaveBeenCalled()
    expect(HTMLAnchorElement.prototype.click).not.toHaveBeenCalled()
  })
})
