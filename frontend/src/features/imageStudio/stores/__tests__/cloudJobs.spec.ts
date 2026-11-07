import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type TaskRecord } from '../../types'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'
import { GalleryApiError } from '../../lib/galleryApi'

const { callImageApi, createStudioJob, getStudioJob, listStudioJobs, deleteStudioJob, getGalleryStatus, listGalleryTasks, putGalleryAsset, ensureGalleryIdentity, showBrowserNotification, getAllTasks, getPendingSyncRecords, getPendingSyncTaskIds, putTask, putTaskWithPendingSync, commitTaskDeletion, clearTaskPendingSync, storeImageWithSize } = vi.hoisted(() => ({
  callImageApi: vi.fn(), createStudioJob: vi.fn(), getStudioJob: vi.fn(), listStudioJobs: vi.fn(), deleteStudioJob: vi.fn(),
  getGalleryStatus: vi.fn(), listGalleryTasks: vi.fn(), putGalleryAsset: vi.fn(), ensureGalleryIdentity: vi.fn(), showBrowserNotification: vi.fn(), getAllTasks: vi.fn(), getPendingSyncRecords: vi.fn(),
  getPendingSyncTaskIds: vi.fn(), putTask: vi.fn(), putTaskWithPendingSync: vi.fn(), commitTaskDeletion: vi.fn(), clearTaskPendingSync: vi.fn(), storeImageWithSize: vi.fn(),
}))

vi.mock('../../lib/api', () => ({ callImageApi }))
vi.mock('../../lib/browserNotification', async () => ({
  ...await vi.importActual<typeof import('../../lib/browserNotification')>('../../lib/browserNotification'),
  showBrowserNotification,
}))
vi.mock('../../lib/studioJobsApi', async () => ({
  ...await vi.importActual<typeof import('../../lib/studioJobsApi')>('../../lib/studioJobsApi'),
  createStudioJob, getStudioJob, listStudioJobs, deleteStudioJob,
}))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  putTask, putTaskWithPendingSync, commitTaskDeletion, clearTaskPendingSync, storeImageWithSize, getAllTasks,
  getTaskSyncRecord: vi.fn().mockResolvedValue(null),
  getPendingSyncRecords,
  getPendingSyncTaskIds,
}))
vi.mock('../../lib/galleryApi', async () => ({
  ...await vi.importActual<typeof import('../../lib/galleryApi')>('../../lib/galleryApi'),
  getGalleryStatus, listGalleryTasks, putGalleryAsset,
  ensureGalleryIdentity,
}))
vi.mock('../../lib/studioKeySession', async () => ({
  ...await vi.importActual<typeof import('../../lib/studioKeySession')>('../../lib/studioKeySession'),
  isCurrentStudioApiKey: () => true,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))

import { setImageStudioUserScope } from '../../lib/db'
import { verifyGalleryIdentity } from '../../lib/gallerySession'
import { initStore, loadNextGalleryPage, removeMultipleTasks, resumeStudioJobsForCurrentUser, retryTask, stopTaskGeneration, storeState as state, submitTask, useStore } from '../store'

function tokenFor(id: number) {
  return [btoa('{}'), btoa(JSON.stringify({ user_id: id, sid: 'cloud-test-' + id })), 'signature'].join('.')
}

beforeEach(() => {
  vi.clearAllMocks()
  const token = tokenFor(100)
  localStorage.setItem('auth_token', token)
  localStorage.setItem('auth_user', JSON.stringify({ id: 100 }))
  setImageStudioUserScope(100)
  verifyGalleryIdentity(100, token)
  putTask.mockResolvedValue(undefined)
  putTaskWithPendingSync.mockResolvedValue(undefined)
  commitTaskDeletion.mockResolvedValue(undefined)
  clearTaskPendingSync.mockResolvedValue(undefined)
  deleteStudioJob.mockResolvedValue({ deleted: true })
  getAllTasks.mockResolvedValue([])
  getPendingSyncRecords.mockResolvedValue([])
  getPendingSyncTaskIds.mockResolvedValue([])
  getGalleryStatus.mockResolvedValue({ enabled: true, jobs_enabled: true, user_id: 100, stats: null })
  listGalleryTasks.mockResolvedValue({ items: [], next_cursor: null })
  putGalleryAsset.mockResolvedValue({ id: 'reference-1' })
  ensureGalleryIdentity.mockResolvedValue(undefined)
  storeImageWithSize.mockResolvedValue({ id: 'local-result', width: 2048, height: 2048 })
  callImageApi.mockResolvedValue({ images: ['data:image/png;base64,output'] })
  listStudioJobs.mockResolvedValue({ items: [], next_cursor: null })
  getStudioJob.mockImplementation(async (taskId: string) => ({ job: {
    task_id: taskId,
    status: 'completed',
    task: { ...state.tasks.find((task) => task.id === taskId), status: 'done', outputImages: ['cloud-result'] },
    calls: [], created_at: '', updated_at: '',
  } }))
  createStudioJob.mockImplementation(async (task: TaskRecord) => ({ job: {
    task_id: task.id, status: 'queued', task, calls: [], created_at: '', updated_at: '',
  } }))
  useStore.setState({
    settings: {
      ...state.settings,
      selectedModelId: 'gpt-image-2',
      miloRouter: { ...state.settings.miloRouter, apiKey: 'studio-key', connectionStatus: 'connected' },
    },
    catalogStatus: 'ready',
    modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2', api: 'openai' }] }),
    galleryInitialized: true,
    galleryUserId: 100,
    galleryEnabled: true,
    galleryStatusKnown: true,
    studioJobsSupported: true,
    studioJobsEnabled: true,
    tasks: [], prompt: 'A landscape', inputImages: [], maskDraft: null,
    params: { ...DEFAULT_PARAMS, partial_images: 0 },
    studioJobStatus: {}, gallerySyncStatus: {}, gallerySyncErrorCode: {},
  })
})

afterEach(() => {
  localStorage.clear()
  setImageStudioUserScope(null)
  vi.restoreAllMocks()
})

describe('durable Studio generation handoff', () => {
  it('accepts a server job and polls it without invoking the browser model API', async () => {
    await submitTask()
    expect(createStudioJob).toHaveBeenCalledOnce()
    expect(callImageApi).not.toHaveBeenCalled()
    expect(createStudioJob.mock.calls[0]?.[1]).toMatchObject({
      protocol: 'openai', image_count: 1, input_image_ids: [], request_input_image_ids: [],
    })
    await vi.waitFor(() => expect(state.tasks[0]?.status).toBe('done'))
    expect(state.tasks[0]).toMatchObject({ execution: 'cloud', outputImages: ['cloud-result'] })
    expect(state.gallerySyncStatus[state.tasks[0].id]).toBe('synced')
  })

  it('retains the current browser path when progressive previews are requested', async () => {
    useStore.setState({
      settings: { ...state.settings, selectedModelId: 'gpt-image-2.5-flare' },
      modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2.5-flare', api: 'openai' }] }),
      params: { ...DEFAULT_PARAMS, partial_images: 1 },
    })
    await submitTask()
    expect(createStudioJob).not.toHaveBeenCalled()
    expect(callImageApi).toHaveBeenCalledOnce()
    expect(state.tasks[0]?.execution).toBeUndefined()
  })

  it('uploads reference images under the captured login before the paid job can start', async () => {
    const order: string[] = []
    putGalleryAsset.mockImplementation(async (id: string) => {
      order.push('asset')
      return { id }
    })
    createStudioJob.mockImplementationOnce(async (task: TaskRecord) => {
      order.push('job')
      return { job: { task_id: task.id, status: 'queued', task, calls: [], created_at: '', updated_at: '' } }
    })
    useStore.setState({ inputImages: [{ id: 'reference-1', dataUrl: 'data:image/png;base64,AQID' }] })
    await submitTask()
    expect(order).toEqual(['asset', 'job'])
    expect(putGalleryAsset).toHaveBeenCalledWith('reference-1', 'data:image/png;base64,AQID', localStorage.getItem('auth_token'))
    expect(createStudioJob.mock.calls[0]?.[1]).toMatchObject({ input_image_ids: ['reference-1'], request_input_image_ids: ['reference-1'] })
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('treats rapid double submit as one task and one paid request', async () => {
    let releaseUpload!: () => void
    putGalleryAsset.mockImplementationOnce(() => new Promise((resolve) => {
      releaseUpload = () => resolve({ id: 'reference-1' })
    }))
    useStore.setState({ inputImages: [{ id: 'reference-1', dataUrl: 'data:image/png;base64,AQID' }] })
    const first = submitTask()
    const second = submitTask()
    await vi.waitFor(() => expect(putGalleryAsset).toHaveBeenCalledOnce())
    releaseUpload()
    await Promise.all([first, second])
    expect(createStudioJob).toHaveBeenCalledOnce()
    expect(callImageApi).not.toHaveBeenCalled()
    expect(state.tasks).toHaveLength(1)
  })

  it('recovers an accepted POST after the same user refreshes JWT without another model call', async () => {
    createStudioJob.mockImplementationOnce(async () => {
      const refreshed = tokenFor(100).replace('signature', 'refreshed-signature')
      localStorage.setItem('auth_token', refreshed)
      verifyGalleryIdentity(100, refreshed)
      throw new GalleryApiError('Studio login changed', 401, 'STUDIO_LOGIN_CHANGED_AFTER_SEND')
    })
    await submitTask()
    await vi.waitFor(() => expect(state.tasks[0]?.status).toBe('done'))
    expect(state.tasks[0]?.execution).toBe('cloud')
    expect(createStudioJob).toHaveBeenCalledOnce()
    expect(getStudioJob).toHaveBeenCalledWith(state.tasks[0].id, localStorage.getItem('auth_token'), expect.anything())
    expect(callImageApi).not.toHaveBeenCalled()
    expect(putTaskWithPendingSync).not.toHaveBeenCalled()
  })

  it('rebinds an in-progress cloud poll when the same user JWT refreshes', async () => {
    getStudioJob.mockImplementationOnce(async () => {
      const refreshed = tokenFor(100).replace('signature', 'poll-refreshed-signature')
      localStorage.setItem('auth_token', refreshed)
      verifyGalleryIdentity(100, refreshed)
      throw new GalleryApiError('Studio login changed', 401, 'STUDIO_LOGIN_CHANGED_AFTER_SEND')
    })
    await submitTask()
    await vi.waitFor(() => expect(state.tasks[0]?.status).toBe('done'))
    expect(getStudioJob).toHaveBeenCalledTimes(2)
    expect(getStudioJob.mock.calls[1]?.[1]).toBe(localStorage.getItem('auth_token'))
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('notifies only for a live cloud completion when the notification setting is enabled', async () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(false)
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    useStore.setState({ settings: { ...state.settings, taskCompletionNotification: true } })
    await submitTask()
    await vi.waitFor(() => expect(state.tasks[0]?.status).toBe('done'))
    expect(showBrowserNotification).toHaveBeenCalledOnce()
    expect(showBrowserNotification).toHaveBeenCalledWith('imageStudio.store.notificationTitle', { body: 'A landscape' })
  })

  it('recovers an accepted cloud task after reopening without marking it interrupted or calling a model', async () => {
    const running: TaskRecord = {
      id: 'accepted-cloud-task', execution: 'cloud', prompt: 'An orange bird', params: DEFAULT_PARAMS,
      inputImageIds: [], outputImages: [], status: 'running', error: null,
      createdAt: 10, finishedAt: null, elapsed: null,
    }
    getAllTasks.mockResolvedValueOnce([running])
    // The administrator may stop accepting new jobs while this one is active.
    getGalleryStatus.mockResolvedValueOnce({ enabled: true, jobs_enabled: false, user_id: 100, stats: null })
    listStudioJobs.mockResolvedValueOnce({ items: [{
      task_id: running.id, status: 'generating', task: running, calls: [], created_at: '', updated_at: '',
    }], next_cursor: null })
    getStudioJob.mockResolvedValueOnce({ job: {
      task_id: running.id, status: 'completed',
      task: { ...running, status: 'done', outputImages: ['recovered-cloud-image'] },
      calls: [], created_at: '', updated_at: '',
    } })

    await initStore()
    await vi.waitFor(() => expect(state.tasks.find((task) => task.id === running.id)?.status).toBe('done'))
    expect(state.tasks.find((task) => task.id === running.id)).toMatchObject({ execution: 'cloud', outputImages: ['recovered-cloud-image'] })
    expect(putTaskWithPendingSync).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('blocks a new submission when cloud status cannot be checked instead of silently using the browser', async () => {
    useStore.setState({ galleryStatusKnown: false })
    getGalleryStatus.mockRejectedValueOnce(new Error('network unavailable'))
    await submitTask()
    expect(createStudioJob).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
    expect(state.toast?.message).toBe('imageStudio.store.cloudStatusUnavailable')
  })

  it('keeps an ambiguous POST 404 result local and accepts a job that appears later', async () => {
    createStudioJob.mockRejectedValueOnce(new GalleryApiError('network failed', 0, 'NETWORK'))
    getStudioJob.mockRejectedValue(new GalleryApiError('not found', 404, 'NOT_FOUND'))
    vi.useFakeTimers()
    let failed!: TaskRecord
    try {
      await submitTask()
      await vi.advanceTimersByTimeAsync(6000)
      expect(getStudioJob).toHaveBeenCalledTimes(3)
      failed = state.tasks[0]
      expect(failed).toMatchObject({ execution: 'cloud', status: 'error', cloudJobStatus: 'unknown' })
      expect(putTaskWithPendingSync).not.toHaveBeenCalled()
      expect(putTask).toHaveBeenLastCalledWith(expect.objectContaining({ id: failed.id, cloudJobStatus: 'unknown' }), expect.any(String))
    } finally { vi.useRealTimers() }

    listStudioJobs.mockResolvedValueOnce({ items: [{
      task_id: failed.id, status: 'queued', task: { ...failed, status: 'running', error: null },
      calls: [], created_at: '', updated_at: '',
    }], next_cursor: null })
    getStudioJob.mockResolvedValueOnce({ job: {
      task_id: failed.id, status: 'completed',
      task: { ...failed, status: 'done', outputImages: ['late-result'] },
      calls: [], created_at: '', updated_at: '',
    } })
    await resumeStudioJobsForCurrentUser()
    await vi.waitFor(() => expect(state.tasks.find((task) => task.id === failed.id)?.status).toBe('done'))
    expect(state.tasks.find((task) => task.id === failed.id)?.outputImages).toEqual(['late-result'])
    expect(putTaskWithPendingSync).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('retains stop intent through transient 404s until a slow POST is accepted and deleted', async () => {
    let accept!: () => void
    createStudioJob.mockImplementationOnce((task: TaskRecord) => new Promise((resolve) => {
      accept = () => resolve({ job: { task_id: task.id, status: 'queued', task, calls: [], created_at: '', updated_at: '' } })
    }))
    deleteStudioJob.mockReset().mockRejectedValueOnce(new GalleryApiError('not found', 404, 'NOT_FOUND')).mockResolvedValue({ deleted: true })
    getStudioJob.mockRejectedValue(new GalleryApiError('not found', 404, 'NOT_FOUND'))
    vi.useFakeTimers()
    try {
      const submitting = submitTask()
      await vi.waitFor(() => expect(createStudioJob).toHaveBeenCalledOnce())
      const taskId = state.tasks[0].id
      stopTaskGeneration(taskId)
      await vi.advanceTimersByTimeAsync(6000)
      expect(getStudioJob).toHaveBeenCalledTimes(3)
      expect(state.tasks[0]).toMatchObject({ status: 'error', cloudCancelRequested: true })
      expect(putTaskWithPendingSync).not.toHaveBeenCalled()
      accept()
      await submitting
      await vi.waitFor(() => expect(state.tasks[0]?.cloudJobStatus).toBe('cancelled'))
      expect(deleteStudioJob).toHaveBeenCalledTimes(2)
      expect(state.tasks[0]?.cloudCancelRequested).toBeUndefined()
      expect(callImageApi).not.toHaveBeenCalled()
    } finally { vi.useRealTimers() }
  })

  it('cancels a listed active job before applying a persisted stop intent after reload', async () => {
    const stopped: TaskRecord = {
      id: 'stopped-cloud-task', execution: 'cloud', cloudCancelRequested: true,
      prompt: 'Do not generate', params: DEFAULT_PARAMS, inputImageIds: [], outputImages: [],
      status: 'error', error: 'Stopped', createdAt: 1, finishedAt: 2, elapsed: 1,
    }
    useStore.setState({ tasks: [stopped] })
    const job = { task_id: stopped.id, status: 'queued' as const, task: { ...stopped, status: 'running' as const },
      calls: [], created_at: '', updated_at: '' }
    listStudioJobs.mockResolvedValueOnce({ items: [job], next_cursor: null })
    getStudioJob.mockResolvedValue({ job })
    await resumeStudioJobsForCurrentUser()
    await vi.waitFor(() => expect(state.tasks[0]?.cloudJobStatus).toBe('cancelled'))
    expect(deleteStudioJob).toHaveBeenCalledWith(stopped.id, localStorage.getItem('auth_token'))
    expect(state.tasks[0]).toMatchObject({ status: 'error', outputImages: [] })
    expect(putTaskWithPendingSync).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('does not let a gallery page erase a persisted stop intent before job recovery', async () => {
    const stopped: TaskRecord = {
      id: 'late-gallery-task', execution: 'cloud', cloudCancelRequested: true,
      prompt: 'Do not generate', params: DEFAULT_PARAMS, inputImageIds: [], outputImages: [],
      status: 'error', error: 'Stopped', createdAt: 1, finishedAt: 2, elapsed: 1,
    }
    useStore.setState({ tasks: [stopped], galleryHasMore: true, galleryNextCursor: null })
    listGalleryTasks.mockResolvedValueOnce({ items: [{ ...stopped, cloudCancelRequested: false, status: 'done', outputImages: ['unexpected'] }], next_cursor: null })
    await loadNextGalleryPage()
    expect(state.tasks[0]).toMatchObject({ id: stopped.id, cloudCancelRequested: true, status: 'error', outputImages: [] })
    expect(putTask).not.toHaveBeenCalled()
  })

  it('settles a persisted stop intent when the server already reports cancelled', async () => {
    const stopped: TaskRecord = {
      id: 'already-cancelled-task', execution: 'cloud', cloudCancelRequested: true,
      prompt: 'Do not generate', params: DEFAULT_PARAMS, inputImageIds: [], outputImages: [],
      status: 'error', error: 'Stopped', createdAt: 1, finishedAt: 2, elapsed: 1,
    }
    useStore.setState({ tasks: [stopped] })
    getStudioJob.mockResolvedValueOnce({ job: {
      task_id: stopped.id, status: 'cancelled', task: stopped, calls: [], created_at: '', updated_at: '',
    } })
    await resumeStudioJobsForCurrentUser()
    await vi.waitFor(() => expect(state.tasks[0]?.cloudJobStatus).toBe('cancelled'))
    expect(state.tasks[0]?.cloudCancelRequested).toBeUndefined()
    expect(deleteStudioJob).not.toHaveBeenCalled()
  })

  it('requires explicit confirmation before regenerating an unknown billed result', async () => {
    const unknown: TaskRecord = {
      id: 'unknown-cloud-task', execution: 'cloud', cloudJobStatus: 'unknown', prompt: 'A bird',
      params: DEFAULT_PARAMS, modelId: 'gpt-image-2', apiModel: 'gpt-image-2',
      inputImageIds: [], outputImages: [], status: 'error', error: 'The result is uncertain',
      createdAt: 1, finishedAt: 2, elapsed: 1,
    }
    useStore.setState({ tasks: [unknown], studioJobStatus: {} })
    await retryTask(unknown)
    expect(state.confirmDialog?.title).toBe('imageStudio.store.unknownRetryTitle')
    expect(createStudioJob).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
    state.setConfirmDialog(null)
    expect(createStudioJob).not.toHaveBeenCalled()
  })

  it('waits for an in-flight job cache write before deleting its local record', async () => {
    const unknown: TaskRecord = {
      id: 'deleted-cloud-task', execution: 'cloud', prompt: 'An orange bird', params: DEFAULT_PARAMS,
      inputImageIds: [], outputImages: [], status: 'running', error: null,
      createdAt: 10, finishedAt: null, elapsed: null,
    }
    let finishCacheWrite!: () => void
    putTask.mockImplementationOnce(() => new Promise<void>((resolve) => { finishCacheWrite = resolve }))
    listStudioJobs.mockResolvedValueOnce({ items: [{
      task_id: unknown.id, status: 'unknown', task: unknown, calls: [], created_at: '', updated_at: '',
    }], next_cursor: null })
    const restoring = resumeStudioJobsForCurrentUser()
    await vi.waitFor(() => expect(putTask).toHaveBeenCalledOnce())
    expect(state.tasks.find((task) => task.id === unknown.id)?.status).toBe('error')
    const removing = removeMultipleTasks([unknown.id])
    await Promise.resolve()
    expect(commitTaskDeletion).not.toHaveBeenCalled()
    finishCacheWrite()
    await Promise.all([restoring, removing])
    expect(commitTaskDeletion).toHaveBeenCalledWith([unknown.id], [], expect.any(String))
    expect(state.tasks.find((task) => task.id === unknown.id)).toBeUndefined()
  })
})
