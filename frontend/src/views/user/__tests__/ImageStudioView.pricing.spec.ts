import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const { state, authState, initStore, retryPendingGalleryTasks, resumeStudioJobsForCurrentUser, ensureStudioApiKey, ensureGalleryIdentity, loadImagePricing, collectBillingModels } = vi.hoisted(() => ({
  state: {
    catalogStatus: 'idle',
    modelCatalog: [] as Array<{ id: string }>,
    settings: { miloRouter: { apiKey: '' } },
    setImagePricing: vi.fn(),
  },
  authState: { token: null as string | null, user: null as { id: number } | null },
  initStore: vi.fn(),
  retryPendingGalleryTasks: vi.fn(),
  resumeStudioJobsForCurrentUser: vi.fn(),
  ensureStudioApiKey: vi.fn().mockResolvedValue('key'),
  ensureGalleryIdentity: vi.fn().mockResolvedValue(undefined),
  loadImagePricing: vi.fn(),
  collectBillingModels: vi.fn((models: Array<{ id: string }>) => models.map((model) => model.id)),
}))
vi.mock('@/features/imageStudio/stores/store', async () => ({
  storeState: (await import('vue')).reactive(state),
  initStore,
  retryPendingGalleryTasks,
  resumeStudioJobsForCurrentUser,
}))
vi.mock('@/features/imageStudio/lib/studioAuth', () => ({ ensureStudioApiKey }))
vi.mock('@/features/imageStudio/lib/studioPricing', () => ({ loadImagePricing, collectBillingModels }))
vi.mock('@/features/imageStudio/lib/galleryApi', () => ({ ensureGalleryIdentity }))
vi.mock('@/stores/auth', async () => {
  const { reactive } = await import('vue')
  return { useAuthStore: () => reactive(authState) }
})

import { storeState } from '@/features/imageStudio/stores/store'
import { useAuthStore } from '@/stores/auth'
import ImageStudioView from '../ImageStudioView.vue'

enableAutoUnmount(afterEach)

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

beforeEach(() => {
  localStorage.clear()
  state.setImagePricing.mockClear()
  initStore.mockReset()
  retryPendingGalleryTasks.mockReset()
  resumeStudioJobsForCurrentUser.mockReset()
  ensureStudioApiKey.mockReset().mockResolvedValue('key')
  ensureGalleryIdentity.mockReset().mockResolvedValue(undefined)
  loadImagePricing.mockReset()
  const auth = useAuthStore() as unknown as typeof authState
  auth.token = null
  auth.user = null
  storeState.catalogStatus = 'idle'
  storeState.modelCatalog = [] as typeof storeState.modelCatalog
  storeState.settings.miloRouter.apiKey = ''
})

function mountView() {
  return mount(ImageStudioView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    StudioCreationPanel: true, StudioResultPanel: true, StudioDetailModal: true,
    StudioLightbox: true, StudioMaskEditorModal: true, StudioSettingsModal: true,
    StudioConfirmDialog: true, StudioImageContextMenu: true, StudioToast: true,
  } } })
}

function tokenFor(id: number, generation: number) {
  return [btoa('{}'), btoa(JSON.stringify({ user_id: id, sid: 'session-' + id, generation })), 'signature'].join('.')
}

describe('Image Studio pricing session', () => {
  it('discards a late A price response after account and studio key switch', async () => {
    const a = deferred<Record<string, Record<string, number>>>()
    const b = deferred<Record<string, Record<string, number>>>()
    loadImagePricing.mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    localStorage.setItem('auth_token', 'token-a')
    localStorage.setItem('auth_user', JSON.stringify({ id: 101 }))
    storeState.settings.miloRouter.apiKey = 'key-a'
    storeState.modelCatalog = [{ id: 'model-a' }] as typeof storeState.modelCatalog
    storeState.catalogStatus = 'ready'
    mountView()
    expect(loadImagePricing).toHaveBeenCalledWith(['model-a'])

    localStorage.setItem('auth_token', 'token-b')
    localStorage.setItem('auth_user', JSON.stringify({ id: 202 }))
    storeState.settings.miloRouter.apiKey = 'key-b'
    storeState.modelCatalog = [{ id: 'model-b' }] as typeof storeState.modelCatalog
    await nextTick()
    expect(loadImagePricing).toHaveBeenCalledWith(['model-b'])

    b.resolve({ 'model-b': { '2K': 2 } })
    await flushPromises()
    a.resolve({ 'model-a': { '2K': 1 } })
    await flushPromises()
    expect(state.setImagePricing).toHaveBeenCalledTimes(1)
    expect(state.setImagePricing).toHaveBeenCalledWith({ 'model-b': { '2K': 2 } })
  })
})

describe('Image Studio auth lifecycle', () => {
  it('revalidates and retries on token refresh but ignores an A/B transition', async () => {
    const auth = useAuthStore() as unknown as typeof authState
    const first = tokenFor(101, 1)
    localStorage.setItem('auth_token', first)
    localStorage.setItem('auth_user', JSON.stringify({ id: 101 }))
    auth.token = first
    auth.user = { id: 101 }
    mountView()
    await flushPromises()
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(1)

    const refreshed = tokenFor(101, 2)
    localStorage.setItem('auth_token', refreshed)
    auth.token = refreshed
    await flushPromises()
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(2)
    expect(ensureGalleryIdentity).toHaveBeenCalledWith(refreshed)
    expect(retryPendingGalleryTasks).toHaveBeenCalledTimes(1)

    const other = tokenFor(202, 1)
    localStorage.setItem('auth_token', other)
    auth.token = other
    await flushPromises()
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(2)
    expect(ensureGalleryIdentity).toHaveBeenCalledTimes(1)

    localStorage.setItem('auth_user', JSON.stringify({ id: 202 }))
    auth.user = { id: 202 }
    await flushPromises()
    expect(initStore).toHaveBeenCalledTimes(2)
    expect(ensureStudioApiKey).toHaveBeenCalledTimes(3)
    expect(ensureGalleryIdentity).toHaveBeenCalledWith(other)
  })
})
