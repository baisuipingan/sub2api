import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_PARAMS, type TaskRecord } from '../../types'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'

const { getAllTasks, callImageApi, putTask } = vi.hoisted(() => ({ getAllTasks: vi.fn(), callImageApi: vi.fn(), putTask: vi.fn() }))
vi.mock('../../lib/api', () => ({ callImageApi }))
vi.mock('../../lib/db', async () => ({
  ...await vi.importActual<typeof import('../../lib/db')>('../../lib/db'),
  getAllTasks,
  putTask,
  putTaskWithPendingSync: putTask,
  storeImageWithSize: vi.fn(async () => ({ id: 'generated-image', width: 2048, height: 2048 })),
}))
vi.mock('@/i18n', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('../../lib/studioKeySession', async () => ({
  ...await vi.importActual<typeof import('../../lib/studioKeySession')>('../../lib/studioKeySession'),
  isCurrentStudioApiKey: () => true,
}))

beforeEach(() => {
  vi.resetModules()
  getAllTasks.mockReset().mockResolvedValue([])
  callImageApi.mockReset().mockResolvedValue({ images: ['data:image/png;base64,output'] })
})

describe('Image Studio default resolution state', () => {
  it('keeps the initial 2K choice through local initialization before the catalog arrives', async () => {
    const { initStore, storeState: state } = await import('../store')
    expect(state.params).toMatchObject({ resolution: '2K', size: 'auto' })
    await initStore()
    expect(state.params).toMatchObject({ resolution: '2K', size: 'auto' })
  })

  it('normalizes against the current channel capabilities during local initialization', async () => {
    const { initStore, storeState: state, useStore } = await import('../store')
    useStore.setState({
      modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2', available_size_tiers: ['4K'] }] }),
    })
    await initStore()
    expect(state.params).toMatchObject({ resolution: '4K', size: '2880x2880' })
  })

  it('keeps authoritative unknown-model metadata during local initialization', async () => {
    const { initStore, storeState: state, useStore } = await import('../store')
    useStore.setState({
      modelCatalog: createImageModelCatalogFromModelList({ models: [{
        id: 'gpt-image-2', profiles: [{ provider: 'unknown', profile: 'unknown' }],
      }] }),
    })
    await initStore()
    expect(state.params).toMatchObject({ resolution: 'auto', size: 'auto' })
  })

  it.each(['auto', undefined])('preserves history but makes reused automatic geometry explicit with stored resolution %s', async (resolution) => {
    getAllTasks.mockResolvedValueOnce([{
      id: 'old-task', prompt: 'Keep this automatic', status: 'done', apiModel: 'gpt-image-2',
      params: { size: 'auto', resolution }, inputImageIds: [], outputImages: [],
    }])
    const { initStore, reuseConfig, storeState: state, useStore } = await import('../store')
    useStore.setState({ modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2' }] }) })
    await initStore()

    expect(state.tasks[0].params).toMatchObject({ resolution: 'auto', size: 'auto' })
    await reuseConfig(state.tasks[0])
    expect(state.params).toMatchObject({ resolution: '2K', size: '2048x2048' })
    expect(state.tasks[0].params).toMatchObject({ resolution: 'auto', size: 'auto' })
  })

  it('makes a Grok auto task explicit when reusing it on a channel that only offers 2K', async () => {
    const { reuseConfig, storeState: state, useStore } = await import('../store')
    useStore.setState({
      modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'grok-imagine-image-2.0', available_size_tiers: ['2K'] }] }),
      settings: { ...state.settings, selectedModelId: 'grok-imagine-image-2.0' },
    })
    await reuseConfig({
      id: 'old-task', modelId: 'grok-imagine-image-2.0', prompt: 'Keep this automatic',
      params: { ...DEFAULT_PARAMS, resolution: 'auto' }, inputImageIds: [],
    } as TaskRecord)
    expect(state.params).toMatchObject({ resolution: '2K', size: 'auto', aspect_ratio: '1:1' })
  })

  it('submits an older Grok auto draft at explicit 2K on a 2K-only channel', async () => {
    const { submitTask, storeState: state, useStore } = await import('../store')
    useStore.setState({
      modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'grok-imagine-image-2.0', available_size_tiers: ['2K'] }] }),
      catalogStatus: 'ready',
      settings: {
        ...state.settings, selectedModelId: 'grok-imagine-image-2.0',
        miloRouter: { ...state.settings.miloRouter, apiKey: 'test-key', connectionStatus: 'connected' },
      },
      params: { ...DEFAULT_PARAMS, resolution: 'auto', aspect_ratio: 'auto' },
      prompt: 'A square landscape',
    })
    await submitTask()
    expect(callImageApi).toHaveBeenCalledWith(expect.objectContaining({
      params: expect.objectContaining({ resolution: '2K', aspect_ratio: '1:1' }),
    }))
    expect(state.tasks[0].params).toMatchObject({ resolution: '2K', aspect_ratio: '1:1' })
  })

  it('still blocks a concrete selection that became unavailable before submitting', async () => {
    const { submitTask, storeState: state, useStore } = await import('../store')
    useStore.setState({
      modelCatalog: createImageModelCatalogFromModelList({ models: [{ id: 'gpt-image-2', available_size_tiers: ['2K'] }] }),
      catalogStatus: 'ready',
      settings: {
        ...state.settings, selectedModelId: 'gpt-image-2',
        miloRouter: { ...state.settings.miloRouter, apiKey: 'test-key', connectionStatus: 'connected' },
      },
      params: { ...DEFAULT_PARAMS, resolution: '4K', size: '2880x2880' },
      prompt: 'A landscape',
    })
    await submitTask()
    expect(callImageApi).not.toHaveBeenCalled()
    expect(state.tasks).toEqual([])
    expect(state.toast?.message).toBe('imageStudio.store.channelSizeUnavailable')
  })
})
