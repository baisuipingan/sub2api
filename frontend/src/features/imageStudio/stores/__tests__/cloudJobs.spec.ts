import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type TaskRecord } from '../../types'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'
import { GalleryApiError } from '../../lib/galleryApi'

const { prepareStudioReference, validateMaskMatchesImage, createMaskedInputImage, loadImage, callImageApi, createStudioJob, getStudioJob, listStudioJobs, deleteStudioJob, getGalleryStatus, listGalleryTasks, putGalleryAsset, ensureGalleryIdentity, showBrowserNotification, getAllTasks, getPendingSyncRecords, getPendingSyncTaskIds, putTask, putTaskWithPendingSync, commitTaskDeletion, clearTaskPendingSync, storeImage, storeImageWithSize } = vi.hoisted(() => ({
  prepareStudioReference: vi.fn(), validateMaskMatchesImage: vi.fn(), createMaskedInputImage: vi.fn(), loadImage: vi.fn(),
  callImageApi: vi.fn(), createStudioJob: vi.fn(), getStudioJob: vi.fn(), listStudioJobs: vi.fn(), deleteStudioJob: vi.fn(),
  getGalleryStatus: vi.fn(), listGalleryTasks: vi.fn(), putGalleryAsset: vi.fn(), ensureGalleryIdentity: vi.fn(), showBrowserNotification: vi.fn(), getAllTasks: vi.fn(), getPendingSyncRecords: vi.fn(),
  getPendingSyncTaskIds: vi.fn(), putTask: vi.fn(), putTaskWithPendingSync: vi.fn(), commitTaskDeletion: vi.fn(), clearTaskPendingSync: vi.fn(), storeImage: vi.fn(), storeImageWithSize: vi.fn(),
}))

vi.mock('../../lib/imageApiShared', async () => ({
  ...await vi.importActual<typeof import('../../lib/imageApiShared')>('../../lib/imageApiShared'),
  getDataUrlDecodedByteSize: (value: string) => value.includes('oversized-mask-request') ? 33 * 1024 * 1024 : 100,
}))

vi.mock('../../lib/referenceImagePreparation', () => ({ prepareStudioReference, GALLERY_REFERENCE_MAX_BYTES: 32 * 1024 * 1024 }))
vi.mock('../../lib/canvasImage', async () => ({
  ...await vi.importActual<typeof import('../../lib/canvasImage')>('../../lib/canvasImage'),
  validateMaskMatchesImage, createMaskedInputImage, loadImage,
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
  putTask, putTaskWithPendingSync, commitTaskDeletion, clearTaskPendingSync, storeImage, storeImageWithSize, getAllTasks,
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
  prepareStudioReference.mockImplementation(async (dataUrl: string, maskDataUrl?: string) => ({ dataUrl, maskDataUrl, width: 2048, height: 2048, changed: false }))
  validateMaskMatchesImage.mockResolvedValue(undefined)
  createMaskedInputImage.mockResolvedValue('data:image/png;base64,masked-request')
  loadImage.mockResolvedValue({ naturalWidth: 4096, naturalHeight: 1987 })
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
  storeImage.mockResolvedValue('normalized-mask')
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
    studioSubmissionStage: {},
  })
})

afterEach(() => {
  localStorage.clear()
  setImageStudioUserScope(null)
  vi.restoreAllMocks()
})

describe('durable Studio generation handoff', () => {
  it('submits an oversized reference copy under a new ID while preserving the original draft', async () => {
    const original = { id: 'original-large', dataUrl: 'data:image/jpeg;base64,large' }
    prepareStudioReference.mockResolvedValueOnce({ dataUrl: 'data:image/jpeg;base64,small', width: 4096, height: 1987, changed: true })
    storeImageWithSize.mockResolvedValueOnce({ id: 'normalized-reference', width: 4096, height: 1987 })
    useStore.setState({ inputImages: [original] })
    await submitTask()
    expect(state.inputImages).toEqual([original])
    expect(putGalleryAsset).toHaveBeenCalledWith('normalized-reference', 'data:image/jpeg;base64,small', expect.any(String), expect.any(AbortSignal))
    expect(createStudioJob.mock.calls[0][1]).toMatchObject({ input_image_ids: ['normalized-reference'], request_input_image_ids: ['normalized-reference'] })
    expect(createStudioJob.mock.calls[0][0].inputImageIds).toEqual(['normalized-reference'])
  })

  it('resizes the mask and source together for the submission and retains the original mask draft', async () => {
    const original = { id: 'original-large', dataUrl: 'data:image/jpeg;base64,large' }
    const maskDraft = { targetImageId: original.id, maskDataUrl: 'data:image/png;base64,original-mask', updatedAt: 1, mode: 'region' as const }
    prepareStudioReference.mockResolvedValueOnce({ dataUrl: 'data:image/jpeg;base64,small', maskDataUrl: 'data:image/png;base64,small-mask', width: 4096, height: 1987, changed: true })
    storeImageWithSize.mockImplementation(async (data: string) => ({ id: data.includes('small-mask') ? 'normalized-mask' : data.includes('masked-request') ? 'masked-request' : 'normalized-reference', width: 4096, height: 1987 }))
    useStore.setState({ inputImages: [original], maskDraft })
    await submitTask()
    expect(state.inputImages).toEqual([original])
    expect(state.maskDraft).toEqual(maskDraft)
    expect(validateMaskMatchesImage).toHaveBeenNthCalledWith(1, maskDraft.maskDataUrl, original.dataUrl)
    expect(validateMaskMatchesImage).toHaveBeenNthCalledWith(2, 'data:image/png;base64,small-mask', 'data:image/jpeg;base64,small')
    expect(createMaskedInputImage).toHaveBeenCalledWith('data:image/jpeg;base64,small', 'data:image/png;base64,small-mask')
    expect(createStudioJob.mock.calls[0][1]).toMatchObject({ input_image_ids: ['normalized-reference'], request_input_image_ids: ['masked-request'], mask_image_id: 'normalized-mask', mask_target_image_id: 'normalized-reference' })
  })

  it('does not create any task or call the model when preparing an oversized source fails', async () => {
    prepareStudioReference.mockRejectedValueOnce(new Error('Reference could not be decoded'))
    useStore.setState({ inputImages: [{ id: 'bad-source', dataUrl: 'data:image/jpeg;base64,bad' }] })
    await submitTask()
    expect(state.tasks).toHaveLength(0)
    expect(putGalleryAsset).not.toHaveBeenCalled()
    expect(createStudioJob).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('rebuilds an oversized masked PNG from a smaller synchronized source and mask', async () => {
    const original = { id: 'source', dataUrl: 'data:image/jpeg;base64,ordinary' }
    const maskDraft = { targetImageId: 'source', maskDataUrl: 'data:image/png;base64,original-mask', updatedAt: 1, mode: 'region' as const }
    prepareStudioReference.mockResolvedValueOnce({ dataUrl: original.dataUrl, maskDataUrl: maskDraft.maskDataUrl, width: 4096, height: 4096, changed: false })
      .mockResolvedValueOnce({ dataUrl: 'data:image/jpeg;base64,smaller', maskDataUrl: 'data:image/png;base64,smaller-mask', width: 2560, height: 2560, changed: true })
    createMaskedInputImage.mockResolvedValueOnce('data:image/png;base64,oversized-mask-request').mockResolvedValueOnce('data:image/png;base64,final-masked-request')
    storeImageWithSize.mockImplementation(async (data: string) => ({ id: data.includes('final-masked') ? 'final-request' : 'final-source', width: 2560, height: 2560 }))
    useStore.setState({ inputImages: [original], maskDraft })
    await submitTask()
    expect(prepareStudioReference).toHaveBeenNthCalledWith(2, original.dataUrl, maskDraft.maskDataUrl, 2560)
    expect(createMaskedInputImage).toHaveBeenLastCalledWith('data:image/jpeg;base64,smaller', 'data:image/png;base64,smaller-mask')
    expect(createStudioJob.mock.calls[0][1]).toMatchObject({ input_image_ids: ['final-source'], request_input_image_ids: ['final-request'], mask_target_image_id: 'final-source' })
    expect(state.inputImages).toEqual([original])
    expect(state.maskDraft).toEqual(maskDraft)
  })
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
    expect(putGalleryAsset).toHaveBeenCalledWith('reference-1', 'data:image/png;base64,AQID', localStorage.getItem('auth_token'), expect.any(AbortSignal))
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

  it('marks reference preparation separately and never starts a job after an upload failure', async () => {
    let fail!: (error: Error) => void
    putGalleryAsset.mockImplementationOnce(() => new Promise((_resolve, reject) => { fail = reject }))
    useStore.setState({ inputImages: [{ id: 'reference-1', dataUrl: 'data:image/png;base64,AQID' }] })
    const submit = submitTask()
    await vi.waitFor(() => expect(putGalleryAsset).toHaveBeenCalledOnce())
    expect(state.studioSubmissionStage[state.tasks[0].id]).toBe('uploading')
    expect(createStudioJob).not.toHaveBeenCalled()
    fail(new GalleryApiError('image exceeds the supported pixel count', 400, 'GALLERY_IMAGE_TOO_MANY_PIXELS'))
    await submit
    expect(state.tasks[0].status).toBe('error')
    expect(state.tasks[0].error).toContain('imageStudio.store.referenceUploadFailed')
    expect(state.tasks[0].error).toContain('image exceeds the supported pixel count')
    expect(state.studioSubmissionStage[state.tasks[0].id]).toBeUndefined()
    expect(createStudioJob).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('stops an in-flight reference upload without submitting or polling a paid job', async () => {
    let signal!: AbortSignal
    putGalleryAsset.mockImplementationOnce((_id: string, _image: string, _token: string, input: AbortSignal) => new Promise((_resolve, reject) => {
      signal = input
      input.addEventListener('abort', () => reject(new DOMException('cancelled', 'AbortError')), { once: true })
    }))
    useStore.setState({ inputImages: [{ id: 'reference-1', dataUrl: 'data:image/png;base64,AQID' }] })
    const submit = submitTask()
    await vi.waitFor(() => expect(putGalleryAsset).toHaveBeenCalledOnce())
    stopTaskGeneration(state.tasks[0].id)
    await submit
    await vi.waitFor(() => expect(state.tasks[0].cloudJobStatus).toBe('cancelled'))
    expect(signal.aborted).toBe(true)
    expect(createStudioJob).not.toHaveBeenCalled()
    expect(deleteStudioJob).not.toHaveBeenCalled()
    expect(getStudioJob).not.toHaveBeenCalled()
    expect(callImageApi).not.toHaveBeenCalled()
  })

  it('continues only safe GET polling after a control request times out', async () => {
    vi.useFakeTimers()
    getStudioJob.mockRejectedValueOnce(new GalleryApiError('timed out', 408, 'STUDIO_REQUEST_TIMEOUT'))
    try {
      await submitTask()
      await vi.advanceTimersByTimeAsync(10_000)
      expect(getStudioJob).toHaveBeenCalledTimes(2)
      expect(createStudioJob).toHaveBeenCalledOnce()
      expect(callImageApi).not.toHaveBeenCalled()
      expect(state.tasks[0].status).toBe('done')
    } finally { vi.useRealTimers() }
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
