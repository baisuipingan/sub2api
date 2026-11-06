import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS } from '../../types'

const { callImageApi, putTask, storeImageWithSize, showNotification, showToast } = vi.hoisted(() => ({
  callImageApi: vi.fn(),
  putTask: vi.fn(),
  storeImageWithSize: vi.fn(),
  showNotification: vi.fn(),
  showToast: vi.fn(),
}))
vi.mock('../../lib/api', () => ({ callImageApi }))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  putTask,
  putTaskWithPendingSync: putTask,
  storeImageWithSize,
}))
vi.mock('../../lib/browserNotification', async () => ({
  ...await vi.importActual<typeof import('../../lib/browserNotification')>('../../lib/browserNotification'),
  showBrowserNotification: showNotification,
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('../../lib/studioKeySession', async () => ({
  ...await vi.importActual<typeof import('../../lib/studioKeySession')>('../../lib/studioKeySession'),
  isCurrentStudioApiKey: () => true,
}))
vi.mock('../../lib/galleryApi', async () => ({
  ...await vi.importActual<typeof import('../../lib/galleryApi')>('../../lib/galleryApi'),
  ensureGalleryIdentity: vi.fn().mockResolvedValue(undefined),
}))

import { storeState as state, submitTask, useStore } from '../store'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.setItem('auth_token', 'session-one')
  localStorage.setItem('auth_user', JSON.stringify({ id: 100 }))
  vi.spyOn(document, 'hasFocus').mockReturnValue(false)
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
  putTask.mockResolvedValue(undefined)
  storeImageWithSize.mockResolvedValue({ id: 'generated-image', width: 1024, height: 1024 })
  callImageApi.mockReset().mockResolvedValue({ images: ['data:image/png;base64,output'] })
  useStore.setState({
    settings: {
      ...state.settings,
      selectedModelId: 'gpt-image-2',
      taskCompletionNotification: false,
      clearInputAfterSubmit: false,
      miloRouter: { ...state.settings.miloRouter, apiKey: 'studio-one', connectionStatus: 'connected' },
    },
    modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2', api: 'openai' }] }),
    catalogStatus: 'ready',
    prompt: 'A landscape', params: { ...DEFAULT_PARAMS },
    inputImages: [], maskDraft: null, tasks: [], streamPreviews: {}, showToast,
  })
})

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.removeItem('auth_token')
  localStorage.removeItem('auth_user')
})

describe('generation completion feedback', () => {
  it.each([false, true])('does not add a success toast with notification preference %s', async (enabled) => {
    state.setSettings({ taskCompletionNotification: enabled })
    await submitTask()
    expect(state.tasks[0].status).toBe('done')
    expect(showToast).not.toHaveBeenCalled()
    expect(showNotification).toHaveBeenCalledTimes(enabled ? 1 : 0)
  })

  it('does not send a system notification while the studio is visible and focused', async () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    state.setSettings({ taskCompletionNotification: true })
    await submitTask()
    expect(showNotification).not.toHaveBeenCalled()
    expect(showToast).not.toHaveBeenCalled()
  })

  it('notifies when the visible page has lost focus', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    state.setSettings({ taskCompletionNotification: true })
    await submitTask()
    expect(showNotification).toHaveBeenCalledTimes(1)
  })

  it('uses the latest preference when a running task finishes', async () => {
    state.setSettings({ taskCompletionNotification: true })
    callImageApi.mockImplementationOnce(async () => {
      state.setSettings({ taskCompletionNotification: false })
      return { images: ['data:image/png;base64,output'] }
    })
    await submitTask()
    expect(showNotification).not.toHaveBeenCalled()
  })

  it('honors a notification preference enabled while the task was running', async () => {
    callImageApi.mockImplementationOnce(async () => {
      state.setSettings({ taskCompletionNotification: true })
      return { images: ['data:image/png;base64,output'] }
    })
    await submitTask()
    expect(showNotification).toHaveBeenCalledTimes(1)
  })

  it.each(['logout', 'different-user', 'different-key'])('does not notify a stale %s session', async (change) => {
    state.setSettings({ taskCompletionNotification: true })
    callImageApi.mockImplementationOnce(async () => {
      if (change === 'logout') localStorage.removeItem('auth_token')
      if (change === 'different-user') localStorage.setItem('auth_user', JSON.stringify({ id: 200 }))
      if (change === 'different-key') state.setMiloRouterConfig({ apiKey: 'studio-two' })
      return { images: ['data:image/png;base64,output'] }
    })
    await submitTask()
    expect(showNotification).not.toHaveBeenCalled()
  })

  it('retains notifications across token refreshes for the same user', async () => {
    state.setSettings({ taskCompletionNotification: true })
    const tokenFor = (generation: number) => [
      btoa('{}'), btoa(JSON.stringify({ user_id: 100, sid: 'one-login', generation })), 'signature',
    ].join('.')
    localStorage.setItem('auth_token', tokenFor(1))
    callImageApi.mockImplementationOnce(async () => {
      localStorage.setItem('auth_token', tokenFor(2))
      return { images: ['data:image/png;base64,output'] }
    })
    await submitTask()
    expect(showNotification).toHaveBeenCalledTimes(1)
  })

  it('preserves input clearing and failure feedback', async () => {
    state.setSettings({ clearInputAfterSubmit: true })
    await submitTask()
    expect(state.prompt).toBe('')
    expect(showToast).not.toHaveBeenCalled()

    useStore.setState({ tasks: [], prompt: 'Try again' })
    callImageApi.mockRejectedValueOnce(new Error('Provider unavailable'))
    await submitTask()
    expect(state.tasks[0].status).toBe('error')
    expect(showToast).toHaveBeenCalledWith(expect.any(String), 'error')
  })

  it('retains the selected detail image while keeping old task-only calls compatible', () => {
    state.setDetailTaskId('task-one', 'image-three')
    expect(state.detailTaskId).toBe('task-one')
    expect(state.detailImageId).toBe('image-three')
    state.setDetailTaskId('task-two')
    expect(state.detailImageId).toBeNull()
    state.setDetailTaskId('task-two', 'image-two')
    state.setDetailTaskId(null)
    expect(state.detailTaskId).toBeNull()
    expect(state.detailImageId).toBeNull()
  })
})
