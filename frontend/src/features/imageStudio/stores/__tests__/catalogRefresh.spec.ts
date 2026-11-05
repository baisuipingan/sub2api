import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ImageModelDefinition } from '../../lib/modelRegistry'

const { fetchModels, callImageApi } = vi.hoisted(() => ({
  fetchModels: vi.fn<() => Promise<ImageModelDefinition[]>>(),
  callImageApi: vi.fn(),
}))

vi.mock('../../lib/miloRouterApi', () => ({ fetchMiloRouterImageModels: fetchModels }))
vi.mock('../../lib/api', () => ({ callImageApi }))
vi.mock('@/i18n', () => ({
  default: { global: { t: (key: string) => key } },
}))

import { createImageModelCatalogFromModelList, getImageModel } from '../../lib/modelRegistry'
import { DEFAULT_PARAMS } from '../../types'
import { storeState as state, useStore } from '../store'

function catalog(...ids: string[]) {
  return createImageModelCatalogFromModelList({ data: ids.map((id) => ({ id })) })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

beforeEach(() => {
  fetchModels.mockReset()
  callImageApi.mockReset()
  state.setMiloRouterConfig({ apiKey: 'sk-catalog', connectionStatus: 'connected' })
  useStore.setState({
    settings: { ...state.settings, selectedModelId: 'gpt-image-2' },
    params: { ...DEFAULT_PARAMS },
    modelCatalog: catalog('gpt-image-2'),
    catalogStatus: 'ready',
    catalogError: undefined,
    maskDraft: null,
  })
})

describe('Image Studio catalog refresh', () => {
  it('replaces stale models and selects the first returned model when flare was removed', async () => {
    useStore.setState({
      modelCatalog: catalog('gpt-image-2.5-flare', 'gpt-image-2.5-sunburst'),
      settings: { ...state.settings, selectedModelId: 'gpt-image-2.5-flare' },
    })
    const latest = catalog('gpt-image-2', 'gpt-image-2.5-sunburst', 'gpt-image-2.5')
    fetchModels.mockResolvedValueOnce(latest)

    await expect(state.refreshImageModelCatalog()).resolves.toBe(true)

    expect(state.modelCatalog.map((model) => model.id)).toEqual(latest.map((model) => model.id))
    expect(state.modelCatalog.some((model) => model.id === 'gpt-image-2.5-flare')).toBe(false)
    expect(state.settings.selectedModelId).toBe(latest[0].id)
    expect(state.catalogStatus).toBe('ready')
  })

  it('preserves an existing selection that is still returned by the interface', async () => {
    useStore.setState({ settings: { ...state.settings, selectedModelId: 'gpt-image-2.5' } })
    fetchModels.mockResolvedValueOnce(catalog('gpt-image-2', 'gpt-image-2.5'))

    await state.refreshImageModelCatalog()

    expect(state.settings.selectedModelId).toBe('gpt-image-2.5')
    expect(state.modelCatalog.map((model) => model.name)).toEqual(['gpt-image-2', 'gpt-image-2.5'])
  })

  it('keeps a raw gpt-image-2.5 settings value instead of migrating it to flare', () => {
    state.setSettings({ selectedModelId: ' gpt-image-2.5 ' })

    expect(state.settings.selectedModelId).toBe('gpt-image-2.5')
  })

  it('ignores an older refresh response that arrives after the newer response', async () => {
    const oldRequest = deferred<ImageModelDefinition[]>()
    const newRequest = deferred<ImageModelDefinition[]>()
    fetchModels.mockReturnValueOnce(oldRequest.promise).mockReturnValueOnce(newRequest.promise)
    const oldRefresh = state.refreshImageModelCatalog()
    const newRefresh = state.refreshImageModelCatalog()
    expect(state.catalogStatus).toBe('loading')

    newRequest.resolve(catalog('gpt-image-2.5'))
    await expect(newRefresh).resolves.toBe(true)
    oldRequest.resolve(catalog('gpt-image-2.5-flare'))
    await expect(oldRefresh).resolves.toBe(false)

    expect(state.modelCatalog.map((model) => model.id)).toEqual(['gpt-image-2.5'])
    expect(state.settings.selectedModelId).toBe('gpt-image-2.5')
    expect(state.catalogStatus).toBe('ready')
  })

  it('ignores an older failure after a newer refresh succeeded', async () => {
    const oldRequest = deferred<ImageModelDefinition[]>()
    fetchModels.mockReturnValueOnce(oldRequest.promise).mockResolvedValueOnce(catalog('gpt-image-2.5'))
    const oldRefresh = state.refreshImageModelCatalog()
    await state.refreshImageModelCatalog()
    oldRequest.reject(new Error('503 old catalog request failed'))
    await expect(oldRefresh).resolves.toBe(false)

    expect(state.modelCatalog.map((model) => model.id)).toEqual(['gpt-image-2.5'])
    expect(state.catalogStatus).toBe('ready')
    expect(state.catalogError).toBeUndefined()
  })

  it('discards in-flight results when the API key changes', async () => {
    const oldRequest = deferred<ImageModelDefinition[]>()
    fetchModels.mockReturnValueOnce(oldRequest.promise)
    const oldRefresh = state.refreshImageModelCatalog()
    state.setMiloRouterConfig({ apiKey: 'sk-other-user' })
    expect(state.modelCatalog).toEqual([])
    expect(state.catalogStatus).toBe('idle')
    oldRequest.resolve(catalog('gpt-image-2.5-flare'))
    await expect(oldRefresh).resolves.toBe(false)

    expect(state.settings.miloRouter.apiKey).toBe('sk-other-user')
    expect(state.modelCatalog).toEqual([])
    expect(state.catalogStatus).toBe('idle')
  })

  it('invalidates old requests even if the key changes away and then back', async () => {
    const oldRequest = deferred<ImageModelDefinition[]>()
    fetchModels.mockReturnValueOnce(oldRequest.promise)
    const oldRefresh = state.refreshImageModelCatalog()
    state.setMiloRouterConfig({ apiKey: 'sk-other-user' })
    state.setMiloRouterConfig({ apiKey: 'sk-catalog' })
    fetchModels.mockResolvedValueOnce(catalog('gpt-image-2.5'))
    await state.refreshImageModelCatalog()
    oldRequest.resolve(catalog('gpt-image-2.5-flare'))
    await expect(oldRefresh).resolves.toBe(false)

    expect(state.modelCatalog.map((model) => model.id)).toEqual(['gpt-image-2.5'])
    expect(state.catalogStatus).toBe('ready')
  })

  it('clears stale models after failure and leaves no ready model to generate with', async () => {
    fetchModels.mockRejectedValueOnce(new Error('503 catalog unavailable'))

    await expect(state.refreshImageModelCatalog()).resolves.toBe(false)

    expect(state.catalogStatus).toBe('error')
    expect(state.catalogError).toBe('imageStudio.errors.unavailable')
    expect(state.modelCatalog).toEqual([])
    expect(getImageModel(state.settings.selectedModelId, state.modelCatalog)).toBeNull()
    expect(callImageApi).not.toHaveBeenCalled()
  })
})
