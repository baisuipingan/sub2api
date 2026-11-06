import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { DEFAULT_PARAMS, type TaskRecord } from '../../types'

const { commitTaskDeletion, deleteImage, deleteImageCacheEntry, putTask, storeImageWithSize, callImageApi } = vi.hoisted(() => ({
  commitTaskDeletion: vi.fn(),
  deleteImage: vi.fn(),
  deleteImageCacheEntry: vi.fn(),
  putTask: vi.fn(),
  storeImageWithSize: vi.fn(),
  callImageApi: vi.fn(),
}))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  commitTaskDeletion, deleteImage, putTask, storeImageWithSize,
}))
vi.mock('../../lib/imageCache', async () => ({
  ...await vi.importActual<typeof import('../../lib/imageCache')>('../../lib/imageCache'),
  deleteImageCacheEntry,
}))
vi.mock('../../lib/api', () => ({ callImageApi }))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'
import { deleteImageIfUnreferenced, removeMultipleTasks, removeTask, storeState as state, submitTask, useStore } from '../store'

function task(id: string, patch: Partial<TaskRecord> = {}): TaskRecord {
  return {
    id, prompt: 'A landscape', params: { ...DEFAULT_PARAMS }, inputImageIds: [], outputImages: [id + '-output'],
    status: 'done', error: null, createdAt: 1, finishedAt: 2, elapsed: 1, ...patch,
  }
}

function deferred() {
  let resolve!: () => void
  let reject!: (reason: Error) => void
  const promise = new Promise<void>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

beforeEach(() => {
  vi.clearAllMocks()
  commitTaskDeletion.mockReset().mockResolvedValue(undefined)
  deleteImage.mockReset().mockResolvedValue(undefined)
  putTask.mockReset().mockResolvedValue(undefined)
  storeImageWithSize.mockReset().mockResolvedValue({ id: 'generated', width: 1024, height: 1024 })
  callImageApi.mockReset().mockResolvedValue({ images: ['data:image/png;base64,image'] })
  useStore.setState({
    settings: { ...state.settings, selectedModelId: 'gpt-image-2', clearInputAfterSubmit: false, taskCompletionNotification: false,
      miloRouter: { ...state.settings.miloRouter, apiKey: 'test-key', connectionStatus: 'connected' } },
    modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2', api: 'openai' }] }),
    catalogStatus: 'ready', prompt: 'A landscape', params: { ...DEFAULT_PARAMS },
    tasks: [], streamPreviews: {}, inputImages: [], maskDraft: null, maskEditorImageId: null, imageEditSession: null,
    detailTaskId: null, detailImageId: null, lightboxImageId: null, lightboxImageList: [], latestSubmittedTaskId: null, toast: null,
  })
})

describe('task removal', () => {
  it('rejects a batch containing a running task without deleting completed tasks either', async () => {
    const running = task('running', { status: 'running', finishedAt: null })
    const done = task('done')
    state.tasks = [running, done]
    await removeMultipleTasks([running.id, done.id])

    expect(state.tasks).toEqual([running, done])
    expect(commitTaskDeletion).not.toHaveBeenCalled()
    expect(deleteImage).not.toHaveBeenCalled()
    expect(state.toast?.message).toBe('imageStudio.store.waitGeneration')
  })

  it('also rejects a finished task while its generation result is still being persisted', async () => {
    const persistence = deferred()
    putTask.mockResolvedValueOnce(undefined).mockReturnValueOnce(persistence.promise)
    const generation = submitTask()
    await flushPromises()
    const finished = state.tasks[0]
    expect(finished.status).toBe('done')
    await removeTask(finished)
    expect(commitTaskDeletion).not.toHaveBeenCalled()
    expect(state.tasks).toHaveLength(1)

    persistence.resolve()
    await generation
    await removeTask(finished)
    expect(state.tasks).toHaveLength(0)
    expect(commitTaskDeletion).toHaveBeenCalledWith([finished.id], [])
  })

  it('keeps the UI and image caches intact until the task transaction commits', async () => {
    const persistence = deferred()
    commitTaskDeletion.mockReturnValueOnce(persistence.promise)
    const removed = task('removed')
    state.tasks = [removed]
    state.detailTaskId = removed.id
    state.detailImageId = removed.outputImages[0]
    const removal = removeTask(removed)

    expect(state.tasks).toEqual([removed])
    expect(state.detailTaskId).toBe(removed.id)
    expect(deleteImage).not.toHaveBeenCalled()
    persistence.resolve()
    await removal
    expect(state.tasks).toEqual([])
    expect(state.detailTaskId).toBeNull()
    expect(state.detailImageId).toBeNull()
    expect(deleteImage).toHaveBeenCalledWith(removed.outputImages[0])
  })

  it('preserves the task and open views when persistence fails', async () => {
    commitTaskDeletion.mockRejectedValueOnce(new Error('Storage failed'))
    const removed = task('removed')
    state.tasks = [removed]
    state.detailTaskId = removed.id
    state.detailImageId = removed.outputImages[0]
    state.lightboxImageId = removed.outputImages[0]
    state.lightboxImageList = removed.outputImages
    await expect(removeTask(removed)).rejects.toThrow('Storage failed')

    expect(state.tasks).toEqual([removed])
    expect(state.detailTaskId).toBe(removed.id)
    expect(state.lightboxImageId).toBe(removed.outputImages[0])
    expect(deleteImage).not.toHaveBeenCalled()
    expect(deleteImageCacheEntry).not.toHaveBeenCalled()
  })

  it('retains all references owned by other tasks, drafts, masks, or an edit session', async () => {
    const referencedIds = ['shared', 'draft', 'draft-mask', 'mask-editor', 'edit-target', 'edit-input', 'edit-mask', 'task-mask-target']
    state.tasks = [
      task('removed', { inputImageIds: referencedIds, outputImages: ['unused-output'], maskImageId: 'unused-mask', transparentOriginalImages: ['unused-original'] }),
      task('remaining', { outputImages: ['shared'], maskTargetImageId: 'task-mask-target' }),
    ]
    state.inputImages = [{ id: 'draft', dataUrl: 'draft' }]
    state.maskDraft = { targetImageId: 'draft-mask', maskDataUrl: 'mask', updatedAt: 1 }
    state.maskEditorImageId = 'mask-editor'
    state.imageEditSession = {
      targetImageId: 'edit-target', inputImages: [{ id: 'edit-input', dataUrl: 'input' }],
      maskDraft: { targetImageId: 'edit-mask', maskDataUrl: 'mask', updatedAt: 1 },
      sourceDataUrl: 'source', prompt: '', params: { ...DEFAULT_PARAMS }, selectedModelId: 'gpt-image-2',
    }
    await removeMultipleTasks(['removed'])

    expect(deleteImage.mock.calls.map(([id]) => id).sort()).toEqual(['unused-mask', 'unused-original', 'unused-output'])
    expect(deleteImageCacheEntry.mock.calls.map(([id]) => id).sort()).toEqual(['unused-mask', 'unused-original', 'unused-output'])
  })

  it('uses the latest task and draft references after an asynchronous commit', async () => {
    const persistence = deferred()
    commitTaskDeletion.mockReturnValueOnce(persistence.promise)
    const removed = task('removed', { outputImages: ['new-draft', 'new-task'] })
    state.tasks = [removed]
    const removal = removeTask(removed)
    const added = task('new', { inputImageIds: ['new-task'] })
    state.tasks = [...state.tasks, added]
    state.inputImages = [{ id: 'new-draft', dataUrl: 'draft' }]
    persistence.resolve()
    await removal

    expect(state.tasks).toEqual([added])
    expect(deleteImage).not.toHaveBeenCalled()
  })

  it('prunes deleted images from the lightbox list while retaining a valid current image', async () => {
    state.tasks = [task('removed'), task('remaining')]
    state.lightboxImageId = 'remaining-output'
    state.lightboxImageList = ['removed-output', 'remaining-output']
    await removeMultipleTasks(['removed'])

    expect(state.lightboxImageId).toBe('remaining-output')
    expect(state.lightboxImageList).toEqual(['remaining-output'])
  })

  it('closes a deleted current image and clears task-specific detail and preview state', async () => {
    state.tasks = [task('removed')]
    state.detailTaskId = 'removed'
    state.detailImageId = 'removed-output'
    state.latestSubmittedTaskId = 'removed'
    state.streamPreviews = { removed: { image: 'preview', partialIndex: 1 } }
    state.lightboxImageId = 'removed-output'
    state.lightboxImageList = ['removed-output']
    await removeMultipleTasks(['removed'])

    expect(state.detailTaskId).toBeNull()
    expect(state.detailImageId).toBeNull()
    expect(state.latestSubmittedTaskId).toBeNull()
    expect(state.streamPreviews).toEqual({})
    expect(state.lightboxImageId).toBeNull()
    expect(state.lightboxImageList).toEqual([])
  })

  it('does not persist an empty removal or unknown task IDs', async () => {
    await removeMultipleTasks([])
    await removeMultipleTasks(['missing'])
    expect(commitTaskDeletion).not.toHaveBeenCalled()
  })

  it('also protects edit-session image references from direct orphan cleanup', async () => {
    state.imageEditSession = {
      targetImageId: 'edit-target', inputImages: [], sourceDataUrl: 'source',
      prompt: '', params: { ...DEFAULT_PARAMS }, selectedModelId: 'gpt-image-2', maskDraft: null,
    }
    await deleteImageIfUnreferenced('edit-target')
    expect(deleteImage).not.toHaveBeenCalled()
  })
})
